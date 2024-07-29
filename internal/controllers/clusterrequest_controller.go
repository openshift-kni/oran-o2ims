/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// clusterRequestReconciler reconciles a ClusterRequest object
type ClusterRequestReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterRequestReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *oranv1alpha1.ClusterRequest
}

type clusterDetails struct {
	name          string `default:""`
	version       string `default:""`
	policyVersion string `default:""`
}

func newClusterDetails() *clusterDetails {
	cd := &clusterDetails{}
	return cd
}

func (cd *clusterDetails) setName(name string) *clusterDetails {
	cd.name = name
	return cd
}

func (cd *clusterDetails) setVersion(version string) *clusterDetails {
	cd.version = version
	return cd
}

func (cd *clusterDetails) setPolicyVersion(policyVersion string) *clusterDetails {
	cd.policyVersion = policyVersion
	return cd
}

const (
	clusterRequestFinalizer = "clusterrequest.oran.openshift.io/finalizer"
	ztpDoneLabel            = "ztp-done"
)

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ClusterRequestReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// Fetch the object:
	object := &oranv1alpha1.ClusterRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch Cluster Request",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile ClusterRequest]",
		"name", object.Name, "namespace", object.Namespace)

	if res, stop, err := r.handleFinalizer(ctx, object); !res.IsZero() || stop || err != nil {
		if err != nil {
			r.Logger.ErrorContext(
				ctx,
				"Encountered error while handling the ClusterRequest finalizer",
				slog.String("err", err.Error()))
		}
		return res, err
	}

	// Create and run the task:
	task := &clusterRequestReconcilerTask{
		logger: r.Logger,
		client: r.Client,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterRequestReconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// ### CLUSTERINSTANCE TEMPLATE RENDERING ###
	renderedClusterInstance, renderErr := t.renderClusterInstanceTemplate(ctx)
	if renderErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the ClusterInstance template for ClusterInstance",
			slog.String("name", t.object.Name),
			slog.String("error", renderErr.Error()),
		)
		renderErr = fmt.Errorf("failed to render the ClusterInstance template: %s", renderErr.Error())
	}

	err = t.updateRenderTemplateStatus(ctx, renderErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the RenderTemplate status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}
	if renderErr != nil {
		return ctrl.Result{}, nil
	}

	// ### HARDWARE TEMPLATE RENDERING ###
	renderedNodePool, renderErr := t.renderHardwareTemplate(ctx, renderedClusterInstance)
	if renderErr != nil {
		return ctrl.Result{}, nil
	}

	// ### CREATE/UPDATE NODE POOL CR ###
	createErr := t.createNodePoolResources(ctx, renderedNodePool)
	if createErr != nil {
		return ctrl.Result{}, nil
	}

	// wait for the NodePool to be provisioned and update BMC details in ClusterInstance
	if !t.waitForHardwareData(ctx, renderedClusterInstance, renderedNodePool) {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Waiting for NodePool %s in the namespace %s to be provisioned",
				renderedNodePool.GetName(),
				renderedNodePool.GetNamespace(),
			),
		)
		//TODO: enable Requeue after the hardware plugin is ready
		//return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// ### CREATION OF RESOURCES NEEDED BY THE CLUSTER INSTANCE ###
	createErr = t.createClusterInstanceResources(ctx, renderedClusterInstance)
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create/update the resources for ClusterInstance",
			slog.String("name", t.object.Name),
			slog.String("error", createErr.Error()),
		)
		createErr = fmt.Errorf("failed to render the ClusterInstance template: %s", createErr.Error())
	}
	// Update the ClusterRequest status.
	err = t.updateInstallationResourcesStatus(ctx, createErr)
	if err != nil {
		errString := fmt.Sprintf(
			"Failed to update the clusterInstallationResources status for ClusterRequest %s",
			slog.String("name", t.object.Name),
		)
		t.logger.ErrorContext(
			ctx,
			errString,
		)
		return
	}
	if createErr != nil {
		return ctrl.Result{}, nil
	}

	// ### Create/Update the ClusterInstance CR
	createErr = utils.CreateK8sCR(ctx, t.client, renderedClusterInstance, nil, utils.UPDATE)
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create/update the rendered ClusterInstance %s in the namespace %s",
				renderedClusterInstance.GetName(),
				renderedClusterInstance.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		createErr = fmt.Errorf("failed to create/update the rendered ClusterInstance: %s", createErr.Error())
	}
	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created/Updated clusterInstance %s in the namespace %s",
			renderedClusterInstance.GetName(),
			renderedClusterInstance.GetNamespace(),
		),
	)

	err = t.updateRenderTemplateAppliedStatus(ctx, createErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the RenderTemplateApplied status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}
	if createErr != nil {
		return ctrl.Result{}, nil
	}

	// Update the Status with status from the ClusterInstance object.
	err = t.updateClusterInstanceStatus(ctx, renderedClusterInstance.GetName(), true)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the ClusterInstanceStatus status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}

	return ctrl.Result{}, nil
}

func (t *clusterRequestReconcilerTask) renderClusterInstanceTemplate(
	ctx context.Context) (*unstructured.Unstructured, error) {
	t.logger.InfoContext(
		ctx,
		"[RenderClusterInstance] Rendering the ClusterInstance template for ClusterRequest",
		slog.String("name", t.object.Name),
	)

	// Convert the clusterTemplateInput JSON string into a map
	t.logger.InfoContext(
		ctx,
		"[RenderClusterInstance] Getting the input data(clusterTemplateInput) from ClusterRequest",
		slog.String("name", t.object.Name),
	)
	clusterTemplateInputMap := make(map[string]any)
	err := json.Unmarshal(t.object.Spec.ClusterTemplateInput.ClusterInstanceInput.Raw, &clusterTemplateInputMap)
	if err != nil {
		return nil, fmt.Errorf("the clusterTemplateInput is not in a valid JSON format, err: %w", err)
	}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)
	}

	ctClusterInstanceDefaultsCmName := clusterTemplate.Spec.Templates.ClusterInstanceDefaults
	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"[RenderClusterInstance] Getting the default data(clusterinstance-defaults) "+
				" from Configmap %s in the namespace %s",
			ctClusterInstanceDefaultsCmName,
			t.object.Namespace,
		),
		slog.String("name", t.object.Name),
	)

	// Retrieve the map that holds the default data for clusterInstance
	ctClusterInstanceDefaultsMap, err :=
		t.getClusterTemplateDefaultsFromConfigMap(
			ctx,
			ctClusterInstanceDefaultsCmName,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
		)
	if err != nil {
		return nil, fmt.Errorf("failed to get the default data for ClusterInstace: %w ", err)
	}

	// Create the data object that is consumed by the clusterInstance template
	mergedClusterInstanceData, err := t.buildClusterData(
		clusterTemplateInputMap, ctClusterInstanceDefaultsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build ClusterInstance data, err: %w", err)
	}

	// Validate that the mergedClusterInstanceMap matches the ClusterTemplate referenced
	// in the ClusterRequest.
	err = t.validateClusterTemplateInputMatchesSchema(
		ctx,
		clusterTemplate,
		mergedClusterInstanceData["Cluster"].(map[string]any),
		utils.ClusterInstanceSchema,
	)

	if err != nil {
		return nil, fmt.Errorf(
			fmt.Sprintf(
				"errors matching the merged ClusterInstance data against the referenced "+
					"ClusterTemplate %s: %s", t.object.Name, err.Error()))
	}

	renderedClusterInstance, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, fmt.Errorf("failed to render the ClusterInstance template, err: %w", err)
	}

	t.logger.InfoContext(
		ctx,
		"[RenderClusterInstance] ClusterInstance template is rendered successfully for ClusterRequest",
		slog.String("name", t.object.Name),
	)
	return renderedClusterInstance, nil
}

func (t *clusterRequestReconcilerTask) renderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*oranv1alpha1.NodePool, error) {
	renderedNodePool, renderErr := t.handleRenderHardwareTemplate(ctx, clusterInstance)
	if renderErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the Hardware template for NodePool",
			slog.String("name", t.object.Name),
			slog.String("error", renderErr.Error()),
		)
		renderErr = fmt.Errorf("failed to render the Hardware template: %s", renderErr.Error())
		err := t.updateRenderTemplateStatus(ctx, renderErr)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to update the RenderTemplate status for ClusterRequest",
				slog.String("name", t.object.Name),
			)
		}
		return &oranv1alpha1.NodePool{}, renderErr
	}
	return renderedNodePool, nil
}

func (t *clusterRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*oranv1alpha1.NodePool, error) {

	nodePool := &oranv1alpha1.NodePool{}
	hwTemplateCm := &corev1.ConfigMap{}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)

	}
	hwTemplateCmName := clusterTemplate.Spec.Templates.HwTemplate
	cmExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, hwTemplateCmName, utils.ORANO2IMSNamespace, hwTemplateCm)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for Hardware Template, err: %w", hwTemplateCmName, err)
	}
	if !cmExists {
		t.logger.InfoContext(
			ctx,
			"[renderHardwareTemplate] The Configmap for Hardware Template is not found",
			fmt.Sprintf("[renderHardwareTemplate] The Configmap %s in the namespace %s for "+
				"Hardware Template is not found",
				hwTemplateCmName,
				utils.ORANO2IMSNamespace,
			),
			slog.String("name", t.object.Name),
		)
		return nil, fmt.Errorf("configmap for Hardware Template is not found %s: %w ", hwTemplateCmName, err)
	}

	nodeGroup := []oranv1alpha1.NodeGroup{}
	poolData, exists := hwTemplateCm.Data[utils.HwTemplateNodePool]
	if !exists {
		return nil, fmt.Errorf(
			"%s does not exist in the %s configmap data",
			utils.HwTemplateNodePool,
			hwTemplateCmName,
		)
	}
	if err := k8syaml.Unmarshal([]byte(poolData), &nodeGroup); err != nil {
		return nil, fmt.Errorf(
			"error unmarshaling JSON data from configmap %s for Hardware Template, err: %w", hwTemplateCmName, err)
	}

	// count the nodes  per group
	roleCounts := make(map[string]int)
	specInterface, specExists := clusterInstance.Object["spec"].(map[string]any)
	if !specExists {
		return nil, fmt.Errorf(
			"\"spec\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it's missing",
			t.object.Name,
		)
	}
	if nodes, ok := specInterface["nodes"].([]interface{}); ok {
		for _, node := range nodes {
			if nodeMap, ok := node.(map[string]interface{}); ok {
				if role, ok := nodeMap["role"].(string); ok {
					roleCounts[role]++
				}
			}
		}
	} else {
		return nil, fmt.Errorf("nodes field is not a list")
	}

	for i, group := range nodeGroup {
		if count, ok := roleCounts[group.Name]; ok {
			nodeGroup[i].Size = count
		}
	}
	nodePool.Spec.CloudID = clusterInstance.GetName()
	nodePool.Spec.LocationSpec = t.object.Spec.LocationSpec
	nodePool.Spec.Site = t.object.Spec.Site
	nodePool.Spec.NodeGroup = nodeGroup
	nodePool.ObjectMeta.Name = clusterInstance.GetName()
	nodePool.ObjectMeta.Namespace = hwTemplateCm.Data[utils.HwTemplatePluginMgr]

	return nodePool, nil
}

// getClusterTemplateDefaultsFromConfigMap retrieves the default data for a configmap.
func (t *clusterRequestReconcilerTask) getClusterTemplateDefaultsFromConfigMap(
	ctx context.Context, configmapName string, configMapSuffix string) (map[string]any, error) {

	ctDefaultsMap := make(map[string]any)
	ctDefaultsCm := &corev1.ConfigMap{}

	cmExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, configmapName, t.object.Namespace, ctDefaultsCm)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for ClusterTemplate defaults, err: %w",
			configmapName, err)
	}
	if !cmExists {
		t.logger.InfoContext(
			ctx,
			"[getClusterTemplateDefaultsFromConfigMap] The Configmap for ClusterTemplate defaults is not found",
			fmt.Sprintf(
				"The Configmap %s in the namespace %s for ClusterTemplate defaults is not found",
				configmapName,
				t.object.Namespace,
			),
			slog.String("name", t.object.Name),
		)
	} else {
		defaults, exists := ctDefaultsCm.Data[configMapSuffix]
		if !exists {
			return nil, fmt.Errorf(
				"%s does not exist in the %s configmap data",
				configMapSuffix,
				configmapName,
			)
		}

		// Unmarshal the defaults JSON string into a map.
		err = utils.UnmarshalYAMLOrJSONString(defaults, &ctDefaultsMap)
		if err != nil {
			return nil, fmt.Errorf(
				"the %s from configmap %s is not in a valid JSON format, err: %w",
				configMapSuffix, configmapName, err,
			)
		}
	}

	return ctDefaultsMap, nil
}

// buildClusterData returns an object that is consumed for rendering clusterinstance template
// or templated policies.
func (t *clusterRequestReconcilerTask) buildClusterData(
	clusterTemplateInput, ctClusterInputDefaults map[string]any) (map[string]any, error) {

	// Initialize a map to hold the merged data
	var mergedClusterInstanceMap map[string]any

	switch {
	case len(ctClusterInputDefaults) != 0 && len(clusterTemplateInput) != 0:
		t.logger.Info(
			"[buildClusterData] Merging default data with the clusterTemplateInput data for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterInstanceMap = maps.Clone(ctClusterInputDefaults)
		err := utils.DeepMergeMaps(mergedClusterInstanceMap, clusterTemplateInput, true) // clusterTemplateInput overrides the defaults
		if err != nil {
			return nil, fmt.Errorf("failed to merge the clusterTemplateInput(src) with the defaults(dst): %w", err)
		}
	case len(ctClusterInputDefaults) == 0 && len(clusterTemplateInput) != 0:
		mergedClusterInstanceMap = maps.Clone(clusterTemplateInput)
	case len(clusterTemplateInput) == 0 && len(ctClusterInputDefaults) != 0:
		mergedClusterInstanceMap = maps.Clone(ctClusterInputDefaults)
	default:
		return nil, fmt.Errorf("expected clusterTemplateInput data not provided in either ClusterRequest or Configmap")
	}

	// Wrap the data in a map with key "Cluster"
	mergedClusterInstanceData := map[string]any{
		"Cluster": mergedClusterInstanceMap,
	}

	return mergedClusterInstanceData, nil
}

// createClusterInstanceResources creates all the resources needed for the
// ClusterInstance object to perform a successful installation.
func (t *clusterRequestReconcilerTask) createClusterInstanceResources(
	ctx context.Context, clusterInstance *unstructured.Unstructured) error {

	clusterName := clusterInstance.GetName()

	// Create the BMC secrets.
	err := t.createClusterInstanceBMCSecrets(ctx, clusterName)
	if err != nil {
		return err
	}

	// If we got to this point, we can assume that all the keys up to the BMC details
	// exists since ClusterInstance has nodes mandatory.
	specInterface, specExists := clusterInstance.Object["spec"]
	if !specExists {
		return fmt.Errorf(
			"\"spec\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it's missing",
			t.object.Name,
		)
	}
	spec := specInterface.(map[string]interface{})

	// Copy the pull secret from the cluster template namespace to the
	// clusterInstance namespace.
	err = t.createPullSecret(ctx, clusterName, spec)
	if err != nil {
		return err
	}

	// Copy the extra-manifests ConfigMaps from the cluster template namespace
	// to the clusterInstance namespace.
	err = t.createExtraManifestsConfigMap(ctx, clusterName, spec)
	if err != nil {
		return err
	}

	// Create the cluster ConfigMap which will be used by ACM policies.
	err = t.createPoliciesConfigMap(ctx, clusterName, spec)
	if err != nil {
		return err
	}

	return nil
}

// createExtraManifestsConfigMap copies the extra-manifests ConfigMaps from the
// cluster template namespace to the clusterInstance namespace.
func (t *clusterRequestReconcilerTask) createExtraManifestsConfigMap(
	ctx context.Context, clusterName string, spec map[string]interface{}) error {

	configMapRefInterface, configMapRefExists := spec["extraManifestsRefs"]
	// If no extra manifests ConfigMap is referenced, there's nothing to do. Return.
	if !configMapRefExists {
		return nil
	}

	// Make sure the extra-manifests ConfigMap exists in the cluster template
	// namespace. The name of the extra-manifests ConfigMap is the same as the cluster
	// template.
	configMap := &corev1.ConfigMap{}
	configMapExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Namespace, configMap)
	if err != nil {
		return err
	}
	if !configMapExists {
		return fmt.Errorf(
			"extra-manifests configmap %s expected to exist in the %s namespace, but it's missing",
			t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef)
	}

	// Get the name of the extra-manifests ConfigMap for the ClusterInstance namespace.
	configMapInterfaceArr := configMapRefInterface.([]interface{})
	configMapNameInterface, configMapNameExists :=
		configMapInterfaceArr[0].(map[string]interface{})["name"]
	if !configMapNameExists {
		return fmt.Errorf(
			"\"spec.extraManifestsRefs[%d].name\" expected to exist in the rendered of "+
				"ClusterInstance for ClusterRequest %s, but it's missing",
			0, t.object.Name,
		)
	}
	newConfigMapName := configMapNameInterface.(string)
	newExtraManifestsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newConfigMapName,
			Namespace: clusterName,
		},
		Data: configMap.Data,
	}
	return utils.CreateK8sCR(ctx, t.client, newExtraManifestsConfigMap, t.object, utils.UPDATE)
}

// createPullSecret copies the pull secret from the cluster template namespace
// to the clusterInstanceNamespace
func (t *clusterRequestReconcilerTask) createPullSecret(
	ctx context.Context, clusterName string, spec map[string]interface{}) error {

	// If we got to this point, we can assume that all the keys exist, including
	// clusterName
	pullSecretRefInterface, pullSecretRefExists := spec["pullSecretRef"]
	if !pullSecretRefExists {
		return fmt.Errorf(
			"\"spec.pullSecretRef\" key expected to exist in the rendered ClusterInstance "+
				"for ClusterRequest %s, but it's missing",
			t.object.Name,
		)
	}
	pullSecretInterface := pullSecretRefInterface.(map[string]interface{})
	pullSecretNameInterface, pullSecretNameExists := pullSecretInterface["name"]
	if !pullSecretNameExists {
		return fmt.Errorf(
			"\"spec.pullSecretRef.name\" key expected to exist in the rendered ClusterInstance "+
				"for ClusterRequest %s, but it's missing",
			t.object.Name,
		)
	}
	pullSecretName := pullSecretNameInterface.(string)
	t.logger.InfoContext(
		ctx,
		"pullSecretName: "+fmt.Sprintf("%v", pullSecretName),
	)
	// Check the pull secret already exists in the clusterTemplate namespace.
	// We assume the ClusterTemplate name and ClusterTemplate namespace are the same.
	pullSecret := &corev1.Secret{}
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef, pullSecret)
	if err != nil {
		return err
	}
	if !pullSecretExistsInTemplateNamespace {
		return fmt.Errorf(
			"pull secret %s expected to exist in the %s namespace, but it's missing",
			pullSecretName, t.object.Spec.ClusterTemplateRef)
	}

	newClusterInstancePullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pullSecretName,
			Namespace: clusterName,
		},
		Data: pullSecret.Data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	return utils.CreateK8sCR(ctx, t.client, newClusterInstancePullSecret, nil, utils.UPDATE)
}

// createClusterInstanceNamespace creates the namespace of the ClusterInstance
// where all the other resources needed for installation will exist.
func (t *clusterRequestReconcilerTask) createClusterInstanceNamespace(
	ctx context.Context, clusterName string) error {

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
	err := utils.CreateK8sCR(ctx, t.client, namespace, nil, utils.UPDATE)
	if err != nil {
		return err
	}

	return nil
}

// createClusterInstanceBMCSecrets creates all the BMC secrets needed by the nodes included
// in the ClusterRequest.
func (t *clusterRequestReconcilerTask) createClusterInstanceBMCSecrets(
	ctx context.Context, clusterName string) error {

	// The BMC credential details are for now obtained from the ClusterRequest.
	var inputData map[string]interface{}
	err := json.Unmarshal(t.object.Spec.ClusterTemplateInput.ClusterInstanceInput.Raw, &inputData)
	if err != nil {
		return err
	}

	// If we got to this point, we can assume that all the keys up to the BMC details
	// exists since ClusterInstance has nodes mandatory.
	nodesInterface, nodesExist := inputData["nodes"]
	if !nodesExist {
		return fmt.Errorf(
			"\"spec.nodes\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it's missing",
			t.object.Name,
		)
	}

	nodes := nodesInterface.([]interface{})
	// Go through all the nodes.
	for _, nodeInterface := range nodes {
		node := nodeInterface.(map[string]interface{})

		username, password, secretName, err :=
			utils.GetBMCDetailsForClusterInstance(node, t.object.Name)
		if err != nil {
			return err
		}

		// Create the node's BMC secret.
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: clusterName,
			},
			Data: map[string][]byte{
				"username": []byte(username),
				"password": []byte(password),
			},
		}

		err = utils.CreateK8sCR(ctx, t.client, bmcSecret, nil, utils.UPDATE)
		if err != nil {
			return err
		}
	}

	return nil
}

// copyHwMgrPluginBMCSecret copies the BMC secret from the plugin namespace to the cluster namespace
func (t *clusterRequestReconcilerTask) copyHwMgrPluginBMCSecret(ctx context.Context, name string, sourceNamespace string, targetNamespace string) error {

	// if the secret already exists in the target namespace, do nothing
	secret := &corev1.Secret{}
	exists, err := utils.DoesK8SResourceExist(
		ctx, t.client, name, targetNamespace, secret)
	if err != nil {
		return err
	}
	if exists {
		t.logger.Info(
			"BMC secret already exists in the cluster namespace",
			slog.String("name", name),
			slog.String("name", targetNamespace),
		)
		return nil
	}
	return utils.CopyK8sSecret(ctx, t.client, name, sourceNamespace, targetNamespace)
}

// createHwMgrPluginNamespace creates the namespace of the hardware manager plugin
// where the node pools resource resides
func (t *clusterRequestReconcilerTask) createHwMgrPluginNamespace(
	ctx context.Context, name string) error {

	t.logger.InfoContext(
		ctx,
		"Plugin: "+fmt.Sprintf("%v", name),
	)

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := utils.CreateK8sCR(ctx, t.client, namespace, nil, utils.UPDATE)
	if err != nil {
		return err
	}

	return nil
}

func (t *clusterRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *oranv1alpha1.NodePool) error {

	// Create the hardware plugin namespace.
	pluginNameSpace := nodePool.ObjectMeta.Namespace
	createErr := t.createHwMgrPluginNamespace(ctx, pluginNameSpace)
	if createErr != nil {
		return fmt.Errorf(
			"failed to create hardware manager plugin namespace %s, err: %w", pluginNameSpace, createErr)
	}
	// Create the clusterInstance namespace.
	err := t.createClusterInstanceNamespace(ctx, nodePool.GetName())
	if err != nil {
		return err
	}

	// Create/update the node pool resource
	createErr = utils.CreateK8sCR(ctx, t.client, nodePool, t.object, utils.UPDATE)
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create/update the NodePool %s in the namespace %s",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		createErr = fmt.Errorf("failed to create/update the NodePool: %s", createErr.Error())
	}
	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created/Updated NodePool %s in the namespace %s",
			nodePool.GetName(),
			nodePool.GetNamespace(),
		),
	)
	err = t.updateRenderTemplateAppliedStatus(ctx, createErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the RenderTemplateApplied status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
	}
	return createErr
}

func (t *clusterRequestReconcilerTask) getCrClusterTemplateRef(ctx context.Context) (*oranv1alpha1.ClusterTemplate, error) {
	// Check the clusterTemplateRef references an existing template in the same namespace
	// as the current clusterRequest.
	clusterTemplateRef := &oranv1alpha1.ClusterTemplate{}
	clusterTemplateRefExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Namespace, clusterTemplateRef)

	// If there was an error in trying to get the ClusterTemplate, return it.
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Error obtaining the ClusterTemplate referenced by ClusterTemplateRef",
			slog.String("clusterTemplateRef", t.object.Spec.ClusterTemplateRef),
		)
		return nil, err
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	if !clusterTemplateRefExists {
		err := fmt.Errorf(
			fmt.Sprintf(
				"The referenced ClusterTemplate (%s) does not exist in the %s namespace",
				t.object.Spec.ClusterTemplateRef, t.object.Namespace))

		t.logger.ErrorContext(
			ctx,
			err.Error())
		return nil, err
	}
	return clusterTemplateRef, nil
}

// validateClusterTemplateInputMatchesSchema validates if the clusterTemplateInput matches the
// inputDataSchema of the ClusterTemplate
func (t *clusterRequestReconcilerTask) validateClusterTemplateInputMatchesSchema(
	ctx context.Context,
	clusterTemplateRef *oranv1alpha1.ClusterTemplate,
	clusterData map[string]any,
	schemaType string) error {

	// Get the input data in string format.
	jsonString, err := json.Marshal(clusterData)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Error Marshaling the provided cluster data from "+
					"ClusterTemplate %s for ClusterRequest %s",
				t.object.Spec.ClusterTemplateRef, jsonString))
		return err
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	if schemaType == utils.ClusterInstanceSchema {
		err = utils.ValidateJsonAgainstJsonSchema(
			string(clusterTemplateRef.Spec.InputDataSchema.ClusterInstanceSchema.Raw), string(jsonString))
	} else if schemaType == utils.PolicyTemplateSchema {
		err = utils.ValidateJsonAgainstJsonSchema(
			string(clusterTemplateRef.Spec.InputDataSchema.PolicyTemplateSchema.Raw), string(jsonString))
	}

	if err != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"The provided clusterTemplateInput does not match "+
					"the inputDataSchema from the ClusterTemplateRef (%s)",
				t.object.Spec.ClusterTemplateRef))

		return err
	}

	return nil
}

// createPoliciesConfigMap creates the cluster ConfigMap which will be used
// by the ACM policies.
func (t *clusterRequestReconcilerTask) createPoliciesConfigMap(
	ctx context.Context, clusterName string, spec map[string]interface{}) error {

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)
	}

	// Obtain the cluster version for the cluster-version label.
	clusterVersion, policyVersion, err := utils.GetLabelsForPolicies(
		t.object, spec, clusterName, clusterTemplate.Namespace)
	if err != nil {
		return err
	}
	clusterDetails := newClusterDetails()
	clusterDetails.setName(clusterName).setVersion(clusterVersion).setPolicyVersion(policyVersion)

	// Merge the defaults and the input values for the policies.
	mergedPolicyTemplateData, err := t.createMergedPolicyTemplateData(ctx, clusterTemplate)

	if err != nil {
		return nil
	}

	// Validate that the mergedPolicyTemplateData matches the policyTemplateSchema referenced
	// in the ClusterRequest.
	err = t.validateClusterTemplateInputMatchesSchema(
		ctx,
		clusterTemplate,
		mergedPolicyTemplateData["Cluster"].(map[string]any),
		utils.PolicyTemplateSchema,
	)
	if err != nil {
		return fmt.Errorf(
			fmt.Sprintf(
				"errors matching the merged policyTemplate data against the referenced "+
					"ClusterTemplate %s: %s", t.object.Name, err.Error()))
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"the merged policyTemplate data matches the policyTemplateSchema referenced in "+
				"ClusterTemplate  %s", t.object.Name),
	)

	return t.createPolicyTemplateConfigMap(
		ctx, mergedPolicyTemplateData, clusterTemplate, clusterDetails)
}

// createMergedPolicyTemplateData merges the defaults and the input values for the config policies
func (t *clusterRequestReconcilerTask) createMergedPolicyTemplateData(
	ctx context.Context, clusterTemplate *oranv1alpha1.ClusterTemplate) (map[string]any, error) {

	// Convert the policyTemplateInput JSON string into a map.
	t.logger.InfoContext(
		ctx,
		"Getting the input data(policyTemplateInput) from ClusterRequest",
		slog.String("name", t.object.Name),
	)
	clusterTemplateInputMap := make(map[string]any)
	err := json.Unmarshal(t.object.Spec.ClusterTemplateInput.PolicyTemplateInput.Raw, &clusterTemplateInputMap)
	if err != nil {
		return nil, fmt.Errorf("the PolicyTemplateInput is not in a valid JSON format, err: %w", err)
	}

	ctPolicyTemplateDefaultsCmName := clusterTemplate.Spec.Templates.PolicyTemplateDefaults
	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Getting the default data(policytemplate-defaults) "+
				" from Configmap %s in the namespace %s",
			ctPolicyTemplateDefaultsCmName,
			t.object.Namespace,
		),
		slog.String("name", t.object.Name),
	)

	// Retrieve the map that holds the default data for policy templates.
	ctClusterInstanceDefaultsMap, err :=
		t.getClusterTemplateDefaultsFromConfigMap(
			ctx,
			ctPolicyTemplateDefaultsCmName,
			utils.PolicyTemplateDefaultsConfigmapKey)

	if err != nil {
		return nil, fmt.Errorf("failed to get the default data for PolicyTemplates: %w ", err)
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf("policytemplate-defaults: %s", ctClusterInstanceDefaultsMap),
		slog.String("name", t.object.Name),
	)

	// Create the data object that is consumed by the templated policies.
	mergedPolicyTemplateData, err := t.buildClusterData(
		clusterTemplateInputMap, ctClusterInstanceDefaultsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build ClusterInstance data, err: %w", err)
	}

	return mergedPolicyTemplateData, nil
}

// createPolicyTemplateConfigMap updates the keys of the default ConfigMap to match the
// clusterTemplate and the cluster version and creates/updates the ConfigMap for the
// required version of the policy template.
func (t *clusterRequestReconcilerTask) createPolicyTemplateConfigMap(
	ctx context.Context,
	mergedPolicyTemplateData map[string]any,
	clusterTemplate *oranv1alpha1.ClusterTemplate,
	clusterDetails *clusterDetails) error {

	// Update the keys to match the ClusterTemplate name and the version.
	finalPolicyTemplateData := make(map[string]string)
	for key, value := range mergedPolicyTemplateData["Cluster"].(map[string]interface{}) {
		newKey := fmt.Sprintf(
			"policy-%s-%s",
			clusterDetails.policyVersion,
			key,
		)
		finalPolicyTemplateData[newKey] = value.(string)
	}

	// Put all the data from the mergedPolicyTemplateData in a configMap in the same
	// namespace as the templated ACM policies.
	// The namespace is: ztp + <clustertemplate-name> + <cluster-version>
	policyTemplateConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-pg", clusterDetails.name),
			Namespace: fmt.Sprintf(
				"ztp-%s-%s",
				clusterTemplate.Namespace, clusterDetails.version,
			),
		},
		Data: finalPolicyTemplateData,
	}

	return utils.CreateK8sCR(ctx, t.client, policyTemplateConfigMap, t.object, utils.UPDATE)
}

// updateRenderTemplateStatus update the status of the ClusterRequest object (CR).
func (t *clusterRequestReconcilerTask) updateRenderTemplateStatus(
	ctx context.Context, inputError error) error {

	if t.object.Status.RenderedTemplateStatus == nil {
		t.object.Status.RenderedTemplateStatus = &oranv1alpha1.RenderedTemplateStatus{}
	}
	t.object.Status.RenderedTemplateStatus.RenderedTemplate = true
	t.object.Status.RenderedTemplateStatus.RenderedTemplateError = ""

	if inputError != nil {
		t.object.Status.RenderedTemplateStatus.RenderedTemplate = false
		t.object.Status.RenderedTemplateStatus.RenderedTemplateError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// updateRenderTemplateAppliedStatus update the status of the ClusterRequest object (CR).
func (t *clusterRequestReconcilerTask) updateRenderTemplateAppliedStatus(
	ctx context.Context, inputError error) error {
	if t.object.Status.RenderedTemplateStatus == nil {
		t.object.Status.RenderedTemplateStatus = &oranv1alpha1.RenderedTemplateStatus{}
	}

	t.object.Status.RenderedTemplateStatus.RenderedTemplateApplied = true
	t.object.Status.RenderedTemplateStatus.RenderedTemplateAppliedError = ""

	if inputError != nil {
		t.object.Status.RenderedTemplateStatus.RenderedTemplateApplied = false
		t.object.Status.RenderedTemplateStatus.RenderedTemplateAppliedError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// updateInstallationResourcesStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateInstallationResourcesStatus(
	ctx context.Context, inputError error) error {

	if t.object.Status.ClusterInstallationResources == nil {
		t.object.Status.ClusterInstallationResources = &oranv1alpha1.ClusterInstallationResources{}
	}

	t.object.Status.ClusterInstallationResources.ResourcesCreatedSuccessfully = true
	t.object.Status.ClusterInstallationResources.ErrorCreatingResources = ""

	if inputError != nil {
		t.object.Status.ClusterInstallationResources.ResourcesCreatedSuccessfully = false
		t.object.Status.ClusterInstallationResources.ErrorCreatingResources = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

func (r *ClusterRequestReconciler) finalizeClusterRequest(
	ctx context.Context, clusterRequest *oranv1alpha1.ClusterRequest) error {

	// If the ClusterInstance has been created, delete it. The SiteConfig operator
	// will also delete the namespace.
	if clusterRequest.Status.RenderedTemplateStatus != nil {
		if clusterRequest.Status.RenderedTemplateStatus.RenderedTemplateApplied {
			if clusterRequest.Status.ClusterInstanceStatus != nil {
				clusterInstanceName := clusterRequest.Status.ClusterInstanceStatus.Name

				clusterInstance := &unstructured.Unstructured{}
				clusterInstance.SetGroupVersionKind(schema.GroupVersionKind{
					Kind:    "ClusterInstance",
					Group:   "siteconfig.open-cluster-management.io",
					Version: "v1alpha1",
				})
				clusterInstance.SetName(clusterInstanceName)
				clusterInstance.SetNamespace(clusterInstanceName)

				exists, err := utils.DoesK8SResourceExist(
					ctx,
					r.Client,
					clusterRequest.Status.ClusterInstanceStatus.Name,
					clusterRequest.Status.ClusterInstanceStatus.Name,
					clusterInstance,
				)

				if err != nil {
					return err
				}

				if exists {
					err := r.Client.Delete(ctx, clusterInstance)
					if err != nil {
						return err
					}
				}
			}

			if clusterRequest.Status.HardwareProvisioningStatus != nil {
				// delete the node pool created by this request if it exists
				nodePool := &oranv1alpha1.NodePool{}
				exists, err := utils.DoesK8SResourceExist(
					ctx,
					r.Client,
					clusterRequest.Status.HardwareProvisioningStatus.ClusterName,
					clusterRequest.Status.HardwareProvisioningStatus.Namespace,
					nodePool,
				)

				if err != nil {
					return err
				}

				if exists {
					err := r.Client.Delete(ctx, nodePool)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// If the namespace exists, delete it.
	if clusterRequest.Status.ClusterInstanceStatus != nil {
		namespace := &corev1.Namespace{}
		exists, err := utils.DoesK8SResourceExist(
			ctx,
			r.Client,
			clusterRequest.Status.ClusterInstanceStatus.Name,
			clusterRequest.Status.ClusterInstanceStatus.Name,
			namespace,
		)
		if err != nil {
			return err
		}

		if exists {
			return r.Client.Delete(ctx, namespace)
		}
	}
	return nil
}

func (r *ClusterRequestReconciler) handleFinalizer(
	ctx context.Context, clusterRequest *oranv1alpha1.ClusterRequest) (ctrl.Result, bool, error) {

	// Check if the ClusterRequest is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if clusterRequest.DeletionTimestamp.IsZero() {
		// Check and add finalizer for this CR.
		if !controllerutil.ContainsFinalizer(clusterRequest, clusterRequestFinalizer) {
			controllerutil.AddFinalizer(clusterRequest, clusterRequestFinalizer)
			// Update and requeue since the finalizer has been added.
			return ctrl.Result{Requeue: true}, true, r.Update(ctx, clusterRequest)
		}
		return ctrl.Result{}, false, nil
	} else if controllerutil.ContainsFinalizer(clusterRequest, clusterRequestFinalizer) {
		// Run finalization logic for clusterRequestFinalizer. If the finalization logic
		// fails, don't remove the finalizer so that we can retry during the next reconciliation.
		if err := r.finalizeClusterRequest(ctx, clusterRequest); err != nil {
			return ctrl.Result{}, true, err
		}

		// Remove clusterInstanceFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Removing ClusterRequest finalizer", "name", clusterRequest.Name)
		patch := client.MergeFrom(clusterRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(clusterRequest, clusterRequestFinalizer) {
			return ctrl.Result{}, true, r.Patch(ctx, clusterRequest, patch)
		}
	}
	return ctrl.Result{}, false, nil
}

// waitForNodePoolProvision waits for the NodePool status to be in the provisioned state.
func (t *clusterRequestReconcilerTask) waitForNodePoolProvision(ctx context.Context,
	nodePool *oranv1alpha1.NodePool) bool {

	// Get the generated NodePool and its status.
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodePool.GetName(),
		nodePool.GetNamespace(), nodePool)

	if err != nil || !exists {
		t.logger.ErrorContext(
			ctx,
			"Failed to get the NodePools",
			slog.String("name", nodePool.GetName()),
			slog.String("namespace", nodePool.GetNamespace()),
		)
		return false
	}

	//Update the Cluster Request Status with status from the NodePool object.
	err = t.updateHardwareProvisioningStatus(ctx, nodePool)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodePool status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
	}
	// Check if provisioning is completed
	provisionedCondition := meta.FindStatusCondition(nodePool.Status.Conditions, "Provisioned")
	if provisionedCondition != nil && provisionedCondition.Status == metav1.ConditionTrue {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodePool %s in the namespace %s is provisioned",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
		)
		return true
	}
	return false
}

// updateClusterInstance updates the given clusterinstance object based on the provisioned nodePool.
func (t *clusterRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodePool *oranv1alpha1.NodePool) bool {

	hwNodes := make(map[string][]oranv1alpha1.BMC)
	for _, nodeName := range nodePool.Status.Properties.NodeNames {
		node := &oranv1alpha1.Node{}
		exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodeName, nodePool.Namespace, node)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to get the Node object",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.String("error", err.Error()),
				slog.Bool("exists", exists),
			)
			return false
		}
		if !exists {
			t.logger.ErrorContext(
				ctx,
				"Node object does not exist",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.Bool("exists", exists),
			)
			return false
		}
		// Verify the node object is generated from the expected pool
		if node.Spec.NodePool != nodePool.GetName() {
			t.logger.ErrorContext(
				ctx,
				"Node object is not from the expected NodePool",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return false
		}
		if node.Status.BMC == nil {
			t.logger.ErrorContext(
				ctx,
				"Node status does not have BMC details",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return false

		}
		// Store the BMC details per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], oranv1alpha1.BMC{
			Address:         node.Status.BMC.Address,
			CredentialsName: node.Status.BMC.CredentialsName,
		})
	}
	// Copy the BMC secret from the plugin namespace to the cluster namespace
	for _, bmcs := range hwNodes {
		for _, bmc := range bmcs {
			// nodePool namespace is the plugin namespace, and nodePool name is the cluster name
			err := t.copyHwMgrPluginBMCSecret(ctx, bmc.CredentialsName, nodePool.GetNamespace(), nodePool.GetName())
			if err != nil {
				t.logger.ErrorContext(
					ctx,
					"Failed to copy BMC secret from the plugin namespace to the cluster namespaceNode",
					slog.String("name", bmc.CredentialsName),
					slog.String("plugin", nodePool.GetNamespace()),
					slog.String("cluster", nodePool.GetName()),
				)
				return false
			}
		}
	}
	// Populate the BMC endpoint and credential in the cluster instance
	specInterface, specExists := clusterInstance.Object["spec"].(map[string]any)
	if !specExists {
		return false
	}
	if nodes, ok := specInterface["nodes"].([]interface{}); ok {
		for _, node := range nodes {
			if nodeMap, ok := node.(map[string]interface{}); ok {
				if role, ok := nodeMap["role"].(string); ok {
					// Check if the node's role matches any key in hwNodes
					if bmcs, exists := hwNodes[role]; exists && len(bmcs) > 0 {
						// Assign the first BMC item to the node
						nodeMap["bmcAddress"] = bmcs[0].Address
						nodeMap["bmcCredentialsName"] = map[string]interface{}{"name": bmcs[0].CredentialsName}
						// Remove the first BMC item from the list
						hwNodes[role] = bmcs[1:]
					}
				}
			}
		}
	}
	return true
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details in ClusterInstance.
func (t *clusterRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodePool *oranv1alpha1.NodePool) bool {

	provisioned := t.waitForNodePoolProvision(ctx, nodePool)
	if provisioned {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodePool %s in the namespace %s is provisioned",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
		)
		return t.updateClusterInstance(ctx, clusterInstance, nodePool)
	}
	return false
}

// updateHardwareProvisioningStatus updates the status for the created ClusterInstance
func (t *clusterRequestReconcilerTask) updateHardwareProvisioningStatus(
	ctx context.Context, nodePool *oranv1alpha1.NodePool) error {

	var err error
	if t.object.Status.HardwareProvisioningStatus == nil {
		t.object.Status.HardwareProvisioningStatus = &oranv1alpha1.HardwareProvisioningStatus{}
	}

	t.object.Status.HardwareProvisioningStatus.ClusterName = nodePool.GetName()
	t.object.Status.HardwareProvisioningStatus.Namespace = nodePool.GetNamespace()

	if len(nodePool.Status.Conditions) > 0 {
		provisionedCondition := meta.FindStatusCondition(
			nodePool.Status.Conditions, "Provisioned")
		if provisionedCondition != nil && provisionedCondition.Status == metav1.ConditionTrue {
			t.object.Status.HardwareProvisioningStatus.HardwareStatus = utils.HardwareProvisioningCompleted
		} else {
			provisioningCondition := meta.FindStatusCondition(
				nodePool.Status.Conditions, "Provisioning")
			if provisioningCondition != nil && provisioningCondition.Status == metav1.ConditionTrue {
				t.object.Status.HardwareProvisioningStatus.HardwareStatus = utils.HardwareProvisioningInProgress
			} else {
				failedCondition := meta.FindStatusCondition(
					nodePool.Status.Conditions, "Failed")
				if failedCondition != nil && failedCondition.Status == metav1.ConditionTrue {
					t.object.Status.HardwareProvisioningStatus.HardwareStatus = utils.HardwareProvisioningFailed
				} else {
					t.object.Status.HardwareProvisioningStatus.HardwareStatus = utils.HardwareProvisioningUnknown
				}
			}
		}

		err = utils.UpdateK8sCRStatus(ctx, t.client, t.object)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to update the HardwareProvisioning status for ClusterRequest",
				slog.String("name", t.object.Name),
			)
		}
	} else {
		meta.SetStatusCondition(
			&nodePool.Status.Conditions,
			metav1.Condition{
				Type:   "Unknown",
				Status: metav1.ConditionUnknown,
				Reason: "NotInitialized",
			},
		)
		err = utils.UpdateK8sCRStatus(ctx, t.client, nodePool)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to update the NodePool status",
				slog.String("name", nodePool.Name),
			)
		}
	}
	return err
}

// updateClusterInstanceStatus updates the status for the created ClusterInstance
func (t *clusterRequestReconcilerTask) updateClusterInstanceStatus(
	ctx context.Context, clusterInstanceName string,
	checkClusterInstance bool) error {

	if t.object.Status.ClusterInstanceStatus == nil {
		t.object.Status.ClusterInstanceStatus = &oranv1alpha1.ClusterInstanceStatus{}
	}

	t.object.Status.ClusterInstanceStatus.Name = clusterInstanceName

	if !checkClusterInstance {
		return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	}

	// Get the generated ClusterInstance and its status.
	clusterInstance := &siteconfig.ClusterInstance{}
	err := t.client.Get(
		ctx,
		types.NamespacedName{
			Name:      t.object.Status.ClusterInstanceStatus.Name,
			Namespace: t.object.Status.ClusterInstanceStatus.Name},
		clusterInstance)
	if err != nil {
		return err
	}

	// Update the Provisioned condition type with the content of the Provisioned
	// condition from the ClusterInstance, if it has been added.
	provisionedCondition := meta.FindStatusCondition(
		clusterInstance.Status.Conditions, "Provisioned")

	if provisionedCondition != nil {
		meta.SetStatusCondition(
			&t.object.Status.ClusterInstanceStatus.Conditions,
			*provisionedCondition,
		)
	} else {
		meta.SetStatusCondition(
			&t.object.Status.ClusterInstanceStatus.Conditions,
			metav1.Condition{
				Type:    "Provisioned",
				Status:  "Unknown",
				Reason:  "ProvisionedStatusMissing",
				Message: "ClusterInstance is missing its Provisioned type condition",
			},
		)
	}

	err = utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return err
	}

	// Combine the Hive ClusterInstall conditions to determine if the install has
	// failed. Look at the stopped, completed and failed condition to make a
	// decision.
	// For more details: https://github.com/openshift/hive/blob/master/pkg/controller/clusterdeployment/clusterinstalls.go
	var deploymentCompletedCondition *hivev1.ClusterDeploymentCondition
	var deploymentFailedCondition *hivev1.ClusterDeploymentCondition
	var deploymentStoppedCondition *hivev1.ClusterDeploymentCondition

	for _, deploymentCondition := range clusterInstance.Status.DeploymentConditions {
		switch deploymentCondition.Type {
		case hivev1.ClusterDeploymentConditionType(hivev1.ClusterInstallCompleted):
			deploymentCompletedCondition = &deploymentCondition
		case hivev1.ClusterDeploymentConditionType(hivev1.ClusterInstallFailed):
			deploymentFailedCondition = &deploymentCondition
		case hivev1.ClusterDeploymentConditionType(hivev1.ClusterInstallStopped):
			deploymentStoppedCondition = &deploymentCondition
		default:
			continue
		}
	}

	if deploymentCompletedCondition != nil &&
		deploymentFailedCondition != nil &&
		deploymentStoppedCondition != nil {
		if
		// Stopped + Failed = Final failure.
		deploymentStoppedCondition.Status == corev1.ConditionTrue &&
			deploymentFailedCondition.Status == corev1.ConditionTrue {
			t.object.Status.ClusterInstanceStatus.ClusterInstallStatus = utils.ClusterFailed
		} else if
		// Stopped + Completed = Success.
		deploymentStoppedCondition.Status == corev1.ConditionTrue &&
			deploymentCompletedCondition.Status == corev1.ConditionTrue {
			t.object.Status.ClusterInstanceStatus.ClusterInstallStatus = utils.ClusterCompleted
		} else if
		// Not Stopped + Not Completed + Not Failed = In progress.
		deploymentStoppedCondition.Status != corev1.ConditionTrue &&
			deploymentCompletedCondition.Status != corev1.ConditionTrue &&
			deploymentFailedCondition.Status != corev1.ConditionTrue {
			t.object.Status.ClusterInstanceStatus.ClusterInstallStatus = utils.ClusterInstalling
		}
	}

	err = utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return err
	}

	// Check the managed cluster for updating the ztpDone status.
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := utils.DoesK8SResourceExist(
		ctx, t.client,
		t.object.Status.ClusterInstanceStatus.Name,
		t.object.Status.ClusterInstanceStatus.Name,
		managedCluster,
	)
	if err != nil {
		return err
	}

	if managedClusterExists {
		// If the ztp-done label exists, update the status to completed.
		labels := managedCluster.GetLabels()
		_, hasZtpDone := labels[ztpDoneLabel]
		if hasZtpDone {
			t.object.Status.ClusterInstanceStatus.ZtpStatus = utils.ClusterZtpDone
		} else {
			t.object.Status.ClusterInstanceStatus.ZtpStatus = utils.ClusterZtpNotDone
		}
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// findClusterInstanceForClusterRequest maps the ClusterInstance created by a
// a ClusterRequest to a reconciliation request.
func (r *ClusterRequestReconciler) findClusterInstanceForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newClusterInstance := event.ObjectNew.(*siteconfig.ClusterInstance)

	// Get all the ClusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests)
	if err != nil {
		r.Logger.Error("Error listing ClusterRequests. ", "Error: ", err)
	}

	// Create reconciling requests only for the ClusterRequest that has generated
	// the current ClusterInstance.
	// ToDo: Filter on future conditions that the ClusterRequest is interested in.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Status.ClusterInstanceStatus != nil {
			if clusterRequest.Status.ClusterInstanceStatus.Name == newClusterInstance.Name {
				r.Logger.Info(
					"[findClusterInstanceForClusterRequest] Add new reconcile request for ClusterRequest",
					"name", clusterRequest.Name)
				queue.Add(
					reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: clusterRequest.Namespace,
							Name:      clusterRequest.Name,
						},
					},
				)
			}
		}
	}
}

// findNodePoolForClusterRequest maps the NodePool created by a
// a ClusterRequest to a reconciliation request.
func (r *ClusterRequestReconciler) findNodePoolForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newNodePool := event.ObjectNew.(*oranv1alpha1.NodePool)

	// Get all the ClusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests)
	if err != nil {
		r.Logger.Error("Error listing ClusterRequests. ", "Error: ", err)
	}

	// Create reconciling requests only for the ClusterRequest that has generated
	// the current NodePool.
	// TODO: Filter on future conditions that the ClusterRequest is interested in.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Status.HardwareProvisioningStatus != nil {
			if clusterRequest.Status.HardwareProvisioningStatus.ClusterName == newNodePool.Name {
				r.Logger.Info(
					"[findNodePoolForClusterRequest] Add new reconcile request for ClusterRequest",
					"name", clusterRequest.Name)
				queue.Add(
					reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: clusterRequest.Namespace,
							Name:      clusterRequest.Name,
						},
					},
				)
			}
		}
	}
}

// findClusterRequestsForClusterTemplate maps the ClusterTemplates used by ClusterRequests
// to reconciling requests for those ClusterRequests.
func (r *ClusterRequestReconciler) findClusterRequestsForClusterTemplate(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case we can use either new or old object.
	newClusterTemplate := event.ObjectNew.(*oranv1alpha1.ClusterTemplate)

	// Get all the clusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests, client.InNamespace(newClusterTemplate.GetNamespace()))
	if err != nil {
		r.Logger.Error("Error listing ClusterRequests. ", "Error: ", err)
	}

	// Create reconciling requests only for the clusterRequests that are using the
	// current clusterTemplate.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Spec.ClusterTemplateRef == newClusterTemplate.GetName() {
			r.Logger.Info(
				"[findClusterRequestsForClusterTemplate] Add new reconcile request for ClusterRequest",
				"name", clusterRequest.Name)
			queue.Add(
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterRequest.Namespace,
						Name:      clusterRequest.Name,
					},
				},
			)
		}
	}
}

// findManagedClusterForClusterRequest maps the ManagedClusters created
// by ClusterInstances through ClusterRequests.
func (r *ClusterRequestReconciler) findManagedClusterForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case we can use either new or old object.
	newManagedCluster := event.ObjectNew.(*clusterv1.ManagedCluster)

	// Get all the clusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests)
	if err != nil {
		r.Logger.Error("Error listing ClusterRequests. ", "Error: ", err)
	}

	// Create reconciling requests only for the clusterRequests that have
	// the same name as the ManagedCluster.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Spec.ClusterTemplateRef == newManagedCluster.GetName() {
			r.Logger.Info(
				"[findManagedClusterForClusterRequest] Add new reconcile request for ClusterRequest",
				"name", clusterRequest.Name)
			queue.Add(
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterRequest.Namespace,
						Name:      clusterRequest.Name,
					},
				},
			)
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("orano2ims-cluster-request").
		For(
			&oranv1alpha1.ClusterRequest{},
			// Watch for create and update event for ClusterRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&oranv1alpha1.ClusterTemplate{},
			handler.Funcs{UpdateFunc: r.findClusterRequestsForClusterTemplate},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on spec changes.
					return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&siteconfig.ClusterInstance{},
			handler.Funcs{
				UpdateFunc: r.findClusterInstanceForClusterRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&oranv1alpha1.NodePool{},
			handler.Funcs{
				UpdateFunc: r.findNodePoolForClusterRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.Funcs{
				UpdateFunc: r.findManagedClusterForClusterRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Check if the event was adding the label "ztp-done".
					// Return true for that event only, and false for everything else.
					_, doneLabelExistsInOld := e.ObjectOld.GetLabels()[ztpDoneLabel]
					_, doneLabelExistsInNew := e.ObjectNew.GetLabels()[ztpDoneLabel]

					doneLabelAdded := !doneLabelExistsInOld && doneLabelExistsInNew

					var availableInNew, availableInOld bool
					availableCondition := meta.FindStatusCondition(
						e.ObjectOld.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInOld = true
					}
					availableCondition = meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInNew = true
					}

					var hubAccepted bool
					acceptedCondition := meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionHubAccepted)
					if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
						hubAccepted = true
					}

					return (doneLabelAdded && availableInNew && hubAccepted) ||
						(!availableInOld && availableInNew && doneLabelExistsInNew && hubAccepted)
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Complete(r)
}
