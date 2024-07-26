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

	// ### CREATION OF RESOURCES NEEDED BY THE CLUSTER INSTANCE ###
	createErr := t.createClusterInstanceResources(ctx, renderedClusterInstance)
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
	ctClusterInstanceDefaultsMap, err := t.getCtClusterInstanceDefaults(ctx, ctClusterInstanceDefaultsCmName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the default data for ClusterInstace: %w ", err)
	}

	// Create the data object that is consumed by the clusterInstance template
	mergedClusterInstanceData, err := t.buildClusterInstanceData(
		clusterTemplateInputMap, ctClusterInstanceDefaultsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build ClusterInstance data, err: %w", err)
	}

	// Validate that the mergedClusterInstanceMap matches the ClusterTemplate referenced
	// in the ClusterRequest.
	err = t.validateClusterTemplateInputMatchesClusterTemplate(
		ctx, clusterTemplate, mergedClusterInstanceData["Cluster"].(map[string]any))
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

// getCtClusterInstanceDefaults retrieves the dafault data for ClusterInstance for a configmap
func (t *clusterRequestReconcilerTask) getCtClusterInstanceDefaults(
	ctx context.Context, configmapName string) (map[string]any, error) {

	ctClusterInstanceDefaultsMap := make(map[string]any)
	ctClusterInstanceDefaultsCm := &corev1.ConfigMap{}

	cmExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, configmapName, t.object.Namespace, ctClusterInstanceDefaultsCm)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for ClusterInstance defaults, err: %w", configmapName, err)
	}
	if !cmExists {
		t.logger.InfoContext(
			ctx,
			"[RenderClusterInstance] The Configmap for ClusterInstance defaults is not found",
			fmt.Sprintf(
				"[RenderClusterInstance] The Configmap %s in the namespace %s for "+
					"ClusterInstance defaults is not found",
				configmapName,
				t.object.Namespace,
			),
			slog.String("name", t.object.Name),
		)
	} else {
		defaults, exists := ctClusterInstanceDefaultsCm.Data[utils.ClusterInstanceTemplateDefaultsConfigmapSuffix]
		if !exists {
			return nil, fmt.Errorf(
				"%s does not exist in the %s configmap data",
				utils.ClusterInstanceTemplateDefaultsConfigmapSuffix,
				configmapName,
			)
		}

		// Unmarshal the clusterinstance defaults JSON string into a map
		err = utils.UnmarshalYAMLOrJSONString(defaults, &ctClusterInstanceDefaultsMap)
		if err != nil {
			return nil, fmt.Errorf(
				"the %s from configmap %s is not in a valid JSON format, err: %w",
				utils.ClusterInstanceTemplateDefaultsConfigmapSuffix, configmapName, err,
			)
		}
	}

	return ctClusterInstanceDefaultsMap, nil
}

// buildClusterInstanceData returns an object that is consumed for rendering clusterinstance template
func (t *clusterRequestReconcilerTask) buildClusterInstanceData(
	clusterTemplateInput, ctClusterInstanceDefaults map[string]any) (map[string]any, error) {

	// Initialize a map to hold the merged data
	var mergedClusterInstanceMap map[string]any

	switch {
	case len(ctClusterInstanceDefaults) != 0 && len(clusterTemplateInput) != 0:
		t.logger.Info(
			"[RenderClusterInstance] Merging the default data with the clusterTemplateInput data for ClusterInstance",
			slog.String("name", t.object.Name),
		)
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterInstanceMap = maps.Clone(ctClusterInstanceDefaults)
		err := utils.DeepMergeMaps(mergedClusterInstanceMap, clusterTemplateInput, true) // clusterTemplateInput overrides the defaults
		if err != nil {
			return nil, fmt.Errorf("failed to merge the clusterTemplateInput(src) with the defaults(dst): %w", err)
		}
	case len(ctClusterInstanceDefaults) == 0 && len(clusterTemplateInput) != 0:
		mergedClusterInstanceMap = maps.Clone(clusterTemplateInput)
	case len(clusterTemplateInput) == 0 && len(ctClusterInstanceDefaults) != 0:
		mergedClusterInstanceMap = maps.Clone(ctClusterInstanceDefaults)
	default:
		return nil, fmt.Errorf("no ClusterInstance data provided in either ClusterRequest or Configmap")
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

	// Create the clusterInstance namespace.
	err := t.createClusterInstanceNamespace(ctx, clusterInstance)
	if err != nil {
		return err
	}

	clusterName := clusterInstance.GetName()

	// Create the BMC secrets.
	err = t.createClusterInstanceBMCSecrets(ctx, clusterName)
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
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef, configMap)
	if err != nil {
		return err
	}
	if !configMapExists {
		return fmt.Errorf(
			"extra-manifests configmap %s expected to exist in the %s namespace, but it's missing",
			clusterName, t.object.Spec.ClusterTemplateRef)
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
		ctx, t.client, pullSecretName, t.object.Spec.ClusterTemplateRef, pullSecret)
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
	ctx context.Context, clusterInstance *unstructured.Unstructured) error {

	// If we got to this point, we can assume that all the keys exist, including
	// clusterName
	clusterName := clusterInstance.GetName()
	t.logger.InfoContext(
		ctx,
		"nodes: "+fmt.Sprintf("%v", clusterName),
	)

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

// validateClusterTemplateInput validates if the clusterTemplateInput matches the
// inputDataSchema of the ClusterTemplate
func (t *clusterRequestReconcilerTask) validateClusterTemplateInputMatchesClusterTemplate(
	ctx context.Context, clusterTemplateRef *oranv1alpha1.ClusterTemplate, clusterInstanceData map[string]any) error {

	// Get the input data in string format.
	jsonString, err := json.Marshal(clusterInstanceData)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Error Marshaling the provided clusterInstance data from "+
					"ClusterTemplate %s for ClusterRequest %s",
				t.object.Spec.ClusterTemplateRef, jsonString))
		return err
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	err = utils.ValidateJsonAgainstJsonSchema(
		string(clusterTemplateRef.Spec.InputDataSchema.ClusterInstanceSchema.Raw), string(jsonString))

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
