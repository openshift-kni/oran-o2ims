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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
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

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
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
	if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch Cluster Request",
			slog.String("error", err.Error()),
		)
	}

	r.Logger.InfoContext(ctx, "[Reconcile Cluster Request] "+object.Name)

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
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// ### JSON VALIDATION ###

	// Check if the clusterTemplateInput is in a JSON format; the schema itself is not of importance.
	validationErr := utils.ValidateInputDataSchema(t.object.Spec.ClusterTemplateInput)
	if validationErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate the ClusterTemplateInput format",
			slog.String("name", t.object.Name),
			slog.String("error", validationErr.Error()),
		)
		validationErr = fmt.Errorf("failed to validate the ClusterTemplateInput format: %s", validationErr.Error())
	}
	// Update the ClusterRequest status.
	err = t.updateClusterTemplateInputValidationStatus(ctx, validationErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the clusterTemplateInputValidation for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}
	if validationErr != nil {
		return ctrl.Result{}, nil
	}

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

	return
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
	err := utils.UnmarshalYAMLOrJSONString(t.object.Spec.ClusterTemplateInput, &clusterTemplateInputMap)
	if err != nil {
		return nil, fmt.Errorf("the clusterTemplateInput is not in a valid JSON format, err: %w", err)
	}

	ctClusterInstanceDefaultsCmName := fmt.Sprintf(
		"%s-%s", t.object.Spec.ClusterTemplateRef, utils.ClusterInstanceTemplateDefaultsConfigmapSuffix)
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
		ctx, mergedClusterInstanceData["Cluster"].(map[string]any))
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
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef, pullSecret)
	if err != nil {
		return err
	}
	if !pullSecretExistsInTemplateNamespace {
		return fmt.Errorf(
			"pull secret %s expected to exist in the %s namespace, but it's missing",
			t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef)
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
	err := json.Unmarshal([]byte(t.object.Spec.ClusterTemplateInput), &inputData)
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

// validateClusterTemplateInput validates if the clusterTemplateInput matches the
// inputDataSchema of the ClusterTemplate
func (t *clusterRequestReconcilerTask) validateClusterTemplateInputMatchesClusterTemplate(
	ctx context.Context, clusterInstanceData map[string]any) error {

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
		return err
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
		return err
	}

	// Get the input data in string format.
	jsonString, err := json.Marshal(clusterInstanceData)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Error Marshaling the provided clusterInstance data from "+
					"ClusterTemplate %s for ClusterRequest %s",
				t.object.Spec.ClusterTemplateRef, t.object.Name))
		return err
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	err = utils.ValidateJsonAgainstJsonSchema(
		clusterTemplateRef.Spec.InputDataSchema, string(jsonString))

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

// updateClusterTemplateInputValidationStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateClusterTemplateInputValidationStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterTemplateInputValidation.InputIsValid = true
	t.object.Status.ClusterTemplateInputValidation.InputError = ""

	if inputError != nil {
		t.object.Status.ClusterTemplateInputValidation.InputIsValid = false
		t.object.Status.ClusterTemplateInputValidation.InputError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

/*
// updateClusterTemplateMatchStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateClusterTemplateMatchStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplate = true
	t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplateError = ""

	if inputError != nil {
		t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplate = false
		t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplateError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}
*/

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

// updateClusterTemplateMatchStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateInstallationResourcesStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterInstallationResources.ResourcesCreatedSuccessfully = true
	t.object.Status.ClusterInstallationResources.ErrorCreatingResources = ""

	if inputError != nil {
		t.object.Status.ClusterInstallationResources.ResourcesCreatedSuccessfully = false
		t.object.Status.ClusterInstallationResources.ErrorCreatingResources = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// findClusterRequestsForClusterTemplate maps the ClusterTemplates used by ClusterRequests
// to reconciling requests for those ClusterRequests.
func (r *ClusterRequestReconciler) findClusterRequestsForClusterTemplate(
	ctx context.Context, clusterTemplate client.Object) []reconcile.Request {

	// Empty array of reconciling requests.
	reqs := make([]reconcile.Request, 0)
	// Get all the clusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests, client.InNamespace(clusterTemplate.GetNamespace()))
	if err != nil {
		return reqs
	}

	// Create reconciling requests only for the clusterRequests that are using the
	// current clusterTemplate.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Spec.ClusterTemplateRef == clusterTemplate.GetName() {
			reqs = append(
				reqs,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterRequest.Namespace,
						Name:      clusterRequest.Name,
					},
				},
			)
		}
	}

	return reqs
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
			handler.EnqueueRequestsFromMapFunc(r.findClusterRequestsForClusterTemplate),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Complete(r)
}
