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
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
	logger       *slog.Logger
	client       client.Client
	object       *oranv1alpha1.ClusterRequest
	clusterInput *clusterInput
}

// clusterInput holds the merged input data for a cluster
type clusterInput struct {
	clusterInstanceData map[string]any
	policyTemplateData  map[string]any
}

type nodeInfo struct {
	bmcAddress     string
	bmcCredentials string
	nodeName       string
}

const (
	clusterRequestFinalizer      = "clusterrequest.oran.openshift.io/finalizer"
	clusterRequestNameLabel      = "clusterrequest.oran.openshift.io/name"
	clusterRequestNamespaceLabel = "clusterrequest.oran.openshift.io/namespace"
	ztpDoneLabel                 = "ztp-done"
)

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=hardwaremanagement.oran.openshift.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
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
	result = doNotRequeue()

	// Reconciliation loop can be triggered multiple times for the same resource
	// due to changes in related resources, events or conditions.
	// Wait a bit so that API server/etcd syncs up and this reconcile has a
	// better chance of getting the latest resources.
	time.Sleep(100 * time.Millisecond)

	// Fetch the object:
	object := &oranv1alpha1.ClusterRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The cluster request could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ClusterRequest",
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
		logger:       r.Logger,
		client:       r.Client,
		object:       object,
		clusterInput: &clusterInput{},
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterRequestReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	// Validate the ClusterRequest
	err := t.handleValidation(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return doNotRequeue(), nil
		}
		// internal error that might recover
		return requeueWithError(err)
	}

	// Render the ClusterInstance template
	renderedClusterInstance, err := t.renderClusterInstanceTemplate(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return doNotRequeue(), nil
		}
		return requeueWithError(err)
	}

	// Handle the creation of resources required for cluster deployment
	err = t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			// Requeue since we are not watching for updates to required resources
			// if they are missing
			return requeueWithMediumInterval(), nil
		}
		return requeueWithError(err)
	}

	// Render the hardware template for NodePool
	renderedNodePool, err := t.renderHardwareTemplate(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			return doNotRequeue(), nil
		}
		return requeueWithError(err)
	}

	// Create/Update the NodePool
	err = t.createNodePoolResources(ctx, renderedNodePool)
	if err != nil {
		return requeueWithError(err)
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
		// TODO: enable Requeue after the hardware plugin is ready
		// return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Handle the cluster install with ClusterInstance
	err = t.handleClusterInstallation(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			return doNotRequeue(), nil
		}
		return requeueWithError(err)
	}

	return doNotRequeue(), nil
}

func (t *clusterRequestReconcilerTask) handleValidation(ctx context.Context) error {
	// Validate cluster request CR
	err := t.validateClusterRequestCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate the ClusterRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.Validated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to validate the ClusterRequest: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Validated the ClusterRequest CR",
			slog.String("name", t.object.Name),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.Validated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The cluster request validation succeeded",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

// validateClusterRequestCR validates the ClusterRequest CR
func (t *clusterRequestReconcilerTask) validateClusterRequestCR(ctx context.Context) error {
	// Check the referenced cluster template is present and valid
	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)
	}
	ctValidatedCondition := meta.FindStatusCondition(clusterTemplate.Status.Conditions, string(utils.CTconditionTypes.Validated))
	if ctValidatedCondition == nil || ctValidatedCondition.Status == metav1.ConditionFalse {
		return utils.NewInputError("the clustertemplate validation has failed")
	}

	// Validate the merged clusterinstance input data matches the schema
	mergedClusterInstanceData, err := t.getMergedClusterInputData(ctx, clusterTemplate, utils.ClusterInstanceDataType)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}
	err = t.validateClusterTemplateInputMatchesSchema(
		&clusterTemplate.Spec.InputDataSchema.ClusterInstanceSchema,
		mergedClusterInstanceData)
	if err != nil {
		return utils.NewInputError("failed to validate ClusterTemplate input matches schema: %s", err.Error())
	}

	// Validate the merged policytemplate input data matches the schema
	mergedPolicyTemplateData, err := t.getMergedClusterInputData(ctx, clusterTemplate, utils.PolicyTemplateDataType)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}
	err = t.validateClusterTemplateInputMatchesSchema(
		&clusterTemplate.Spec.InputDataSchema.PolicyTemplateSchema,
		mergedPolicyTemplateData)
	if err != nil {
		return utils.NewInputError("failed to validate ClusterTemplate input matches schema: %s", err.Error())
	}

	// TODO: Verify that ClusterInstance is per ClusterRequest basis.
	//       There should not be multiple ClusterRequests for the same ClusterInstance.

	// The ClusterRequest is valid
	// Set the clusterInput with the merged clusterinstance and policy template data
	t.clusterInput.clusterInstanceData = mergedClusterInstanceData
	t.clusterInput.policyTemplateData = mergedPolicyTemplateData
	return nil
}

func (t *clusterRequestReconcilerTask) getMergedClusterInputData(
	ctx context.Context, clusterTemplate *oranv1alpha1.ClusterTemplate, dataType string) (map[string]any, error) {

	var clusterTemplateInput runtime.RawExtension
	var templateDefaultsCm string
	var templateDefaultsCmKey string

	switch dataType {
	case utils.ClusterInstanceDataType:
		clusterTemplateInput = t.object.Spec.ClusterTemplateInput.ClusterInstanceInput
		templateDefaultsCm = clusterTemplate.Spec.Templates.ClusterInstanceDefaults
		templateDefaultsCmKey = utils.ClusterInstanceTemplateDefaultsConfigmapKey
	case utils.PolicyTemplateDataType:
		clusterTemplateInput = t.object.Spec.ClusterTemplateInput.PolicyTemplateInput
		templateDefaultsCm = clusterTemplate.Spec.Templates.PolicyTemplateDefaults
		templateDefaultsCmKey = utils.PolicyTemplateDefaultsConfigmapKey
	default:
		return nil, utils.NewInputError("unsupported data type")
	}

	// Get the clustertemplate input from cluster request
	clusterTemplateInputMap, err := t.getClusterTemplateInputFromClusterRequest(&clusterTemplateInput)
	if err != nil {
		return nil, utils.NewInputError("failed to get the ClusterTemplate input for %s: %s", dataType, err.Error())
	}

	// Retrieve the configmap that holds the default data
	templateCm, err := utils.GetConfigmap(ctx, t.client, templateDefaultsCm, t.object.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", templateDefaultsCm, err)
	}
	clusterTemplateDefaultsMap, err := utils.ExtractTemplateDataFromConfigMap[map[string]any](
		ctx, t.client, templateCm, templateDefaultsCmKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", templateDefaultsCm, err)
	}

	// Get the merged cluster data
	mergedClusterDataMap, err := mergeClusterTemplateInputWithDefaults(clusterTemplateInputMap, clusterTemplateDefaultsMap)
	if err != nil {
		return nil, utils.NewInputError("failed to merge data for %s: %s", dataType, err.Error())
	}

	t.logger.Info(
		fmt.Sprintf("Merged the %s default data with the clusterTemplateInput data for ClusterRequest", dataType),
		slog.String("name", t.object.Name),
	)
	return mergedClusterDataMap, nil
}

func (t *clusterRequestReconcilerTask) renderClusterInstanceTemplate(
	ctx context.Context) (*unstructured.Unstructured, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering the ClusterInstance template for ClusterRequest",
		slog.String("name", t.object.Name),
	)

	// Wrap the merged clusterinstance data in a map with key "Cluster"
	// This data object will be consumed by the clusterInstance template
	mergedClusterInstanceData := map[string]any{
		"Cluster": t.clusterInput.clusterInstanceData,
	}

	// TODO: Consider unsharmalling into siteconfig.ClusterInstance type
	// to catch any type issue in advance
	renderedClusterInstance, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the ClusterInstance template for ClusterRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		// Something went wrong with rendering, and it's unlikely to be recoverable
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render the ClusterInstance template: "+err.Error(),
		)
		err = utils.NewInputError(err.Error())
	} else {
		// Add ClusterRequest labels to the generated ClusterInstance
		labels := make(map[string]string)
		labels[clusterRequestNameLabel] = t.object.Name
		labels[clusterRequestNamespaceLabel] = t.object.Namespace
		renderedClusterInstance.SetLabels(labels)

		t.logger.InfoContext(
			ctx,
			"Successfully rendered ClusterInstance template for ClusterRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered ClusterInstance template successfully",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return renderedClusterInstance, err
}

func (t *clusterRequestReconcilerTask) handleClusterResources(ctx context.Context, clusterInstance *unstructured.Unstructured) error {
	err := t.createOrUpdateClusterResources(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to apply the required cluster resource for ClusterRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to apply the required cluster resource: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Applied the required cluster resources for ClusterRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Cluster resources applied",
		)
	}
	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

func (t *clusterRequestReconcilerTask) renderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwv1alpha1.NodePool, error) {
	renderedNodePool, err := t.handleRenderHardwareTemplate(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the Hardware template for NodePool",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render the Hardware template: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered Hardware template for NodePool",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered Hardware template successfully",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return renderedNodePool, err
}

func (t *clusterRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwv1alpha1.NodePool, error) {

	nodePool := &hwv1alpha1.NodePool{}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateCmName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplateCm, err := utils.GetConfigmap(ctx, t.client, hwTemplateCmName, utils.ORANO2IMSNamespace)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for Hardware Template, err: %w", hwTemplateCmName, err)
	}

	nodeGroup, err := utils.ExtractTemplateDataFromConfigMap[[]hwv1alpha1.NodeGroup](
		ctx, t.client, hwTemplateCm, utils.HwTemplateNodePool)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the Hardware template from ConfigMap %s, err: %w", hwTemplateCmName, err)
	}

	// count the nodes  per group
	roleCounts := make(map[string]int)
	specInterface, specExists := clusterInstance.Object["spec"].(map[string]any)
	if !specExists {
		// Unlikely to happen
		return nil, utils.NewInputError(
			"\"spec\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it is missing",
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
		// Unlikely to happen
		return nil, utils.NewInputError("nodes field is not a list")
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

	// Add ClusterRequest labels to the generated nodePool
	labels := make(map[string]string)
	labels[clusterRequestNameLabel] = t.object.Name
	labels[clusterRequestNamespaceLabel] = t.object.Namespace
	nodePool.SetLabels(labels)
	return nodePool, nil
}

func (t *clusterRequestReconcilerTask) getClusterTemplateInputFromClusterRequest(clusterTemplateInput *runtime.RawExtension) (map[string]any, error) {
	clusterTemplateInputMap := make(map[string]any)
	err := json.Unmarshal(clusterTemplateInput.Raw, &clusterTemplateInputMap)
	if err != nil {
		// Unlikely to happen since it has been validated by API server
		return nil, fmt.Errorf("error unmarshaling the cluster template input from ClusterRequest: %w", err)
	}
	return clusterTemplateInputMap, nil
}

// mergeClusterTemplateInputWithDefaults merges the cluster template input with the default data
func mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateInputDefaults map[string]any) (map[string]any, error) {
	// Initialize a map to hold the merged data
	var mergedClusterData map[string]any

	switch {
	case len(clusterTemplateInputDefaults) != 0 && len(clusterTemplateInput) != 0:
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)
		err := utils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, true) // clusterTemplateInput overrides the defaults
		if err != nil {
			return nil, fmt.Errorf("failed to merge the clusterTemplateInput(src) with the defaults(dst): %w", err)
		}
	case len(clusterTemplateInputDefaults) == 0 && len(clusterTemplateInput) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInput)
	case len(clusterTemplateInput) == 0 && len(clusterTemplateInputDefaults) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)
	default:
		return nil, fmt.Errorf("expected clusterTemplateInput data not provided in either ClusterRequest or Configmap")
	}

	return mergedClusterData, nil
}

// handleClusterInstallation creates/updates the ClusterInstance to handle the cluster provisioning.
func (t *clusterRequestReconcilerTask) handleClusterInstallation(ctx context.Context, clusterInstance *unstructured.Unstructured) error {
	// Query the ClusterInstance and its status.
	existingClusterInstance := &siteconfig.ClusterInstance{}
	err := t.client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterInstance.GetName(),
			Namespace: clusterInstance.GetNamespace()},
		existingClusterInstance)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// Create the ClusterInstance
		err = t.client.Create(ctx, clusterInstance)
		if err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return err
			}
			// Invalid or webhook error
			err = utils.NewInputError(err.Error())
			t.updateClusterInstanceProcessedStatus(nil, err)
		} else {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"Created ClusterInstance %s in the namespace %s",
					clusterInstance.GetName(),
					clusterInstance.GetNamespace(),
				),
			)
		}
	} else {
		// TODO: only update the existing clusterInstance when a list of allowed fields are changed
		// TODO: What about if the ClusterInstance is not generated by a ClusterRequest?
		t.updateClusterInstanceProcessedStatus(existingClusterInstance, nil)
		t.updateClusterProvisionStatus(existingClusterInstance)

		// Make sure these fields from existing object are copied
		clusterInstance.SetResourceVersion(existingClusterInstance.GetResourceVersion())
		clusterInstance.SetFinalizers(existingClusterInstance.GetFinalizers())
		clusterInstance.SetLabels(existingClusterInstance.GetLabels())
		clusterInstance.SetAnnotations(existingClusterInstance.GetAnnotations())

		patch := client.MergeFrom(existingClusterInstance.DeepCopy())
		if err := t.client.Patch(ctx, clusterInstance, patch); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return err
			}
			// Invalid or webhook error
			err = utils.NewInputError(err.Error())
			t.updateClusterInstanceProcessedStatus(nil, err)
		} else {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"Updated ClusterInstance %s in the namespace %s",
					clusterInstance.GetName(),
					clusterInstance.GetNamespace(),
				),
			)
		}
	}

	if t.object.Status.ClusterInstanceRef == nil {
		t.object.Status.ClusterInstanceRef = &oranv1alpha1.ClusterInstanceRef{}
		t.object.Status.ClusterInstanceRef.Name = clusterInstance.GetName()
	}

	// Check if the cluster provision has completed
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(utils.CRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil && crProvisionedCond.Status == metav1.ConditionTrue {
		// Check the managed cluster for updating the ztpDone status.
		managedCluster := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, t.client,
			clusterInstance.GetName(),
			clusterInstance.GetName(),
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
				t.object.Status.ClusterInstanceRef.ZtpStatus = utils.ClusterZtpDone
			} else {
				t.object.Status.ClusterInstanceRef.ZtpStatus = utils.ClusterZtpNotDone
			}
		}
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return nil
}

func (t *clusterRequestReconcilerTask) updateClusterInstanceProcessedStatus(ci *siteconfig.ClusterInstance, createOrPatchErr error) {
	if createOrPatchErr != nil {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			fmt.Sprintf(
				"Failed to apply the rendered ClusterInstance (%s): %s",
				ci.Name, createOrPatchErr.Error()),
		)
		return
	}

	if ci == nil {
		return
	}

	clusterInstanceConditionTypes := []string{
		"ClusterInstanceValidated",
		"RenderedTemplates",
		"RenderedTemplatesValidated",
		"RenderedTemplatesApplied",
	}

	if len(ci.Status.Conditions) == 0 {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.Unknown,
			metav1.ConditionUnknown,
			fmt.Sprintf("Waiting for ClusterInstance (%s) to be processed", ci.Name),
		)
		return
	}

	for _, condType := range clusterInstanceConditionTypes {
		ciCondition := meta.FindStatusCondition(ci.Status.Conditions, condType)
		if ciCondition != nil && ciCondition.Status != metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.CRconditionTypes.ClusterInstanceProcessed,
				utils.ConditionReason(ciCondition.Reason),
				ciCondition.Status,
				ciCondition.Message,
			)

			return
		}
	}

	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.CRconditionTypes.ClusterInstanceProcessed,
		utils.CRconditionReasons.Completed,
		metav1.ConditionTrue,
		fmt.Sprintf("Processed ClusterInstance (%s) successfully", ci.Name),
	)
}

func (t *clusterRequestReconcilerTask) updateClusterProvisionStatus(ci *siteconfig.ClusterInstance) {
	// Search for ClusterInstance Provisioned condition
	ciProvisionedCondition := meta.FindStatusCondition(
		ci.Status.Conditions, "Provisioned")

	if ciProvisionedCondition == nil {
		crClusterInstanceProcessedCond := meta.FindStatusCondition(
			t.object.Status.Conditions, string(utils.CRconditionTypes.ClusterInstanceProcessed))
		if crClusterInstanceProcessedCond != nil && crClusterInstanceProcessedCond.Status == metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.CRconditionTypes.ClusterProvisioned,
				utils.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				"Waiting for cluster provisioning to start",
			)
		}
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterProvisioned,
			utils.ConditionReason(ciProvisionedCondition.Reason),
			ciProvisionedCondition.Status,
			ciProvisionedCondition.Message,
		)
	}
}

// createOrUpdateClusterResources creates/updates all the resources needed for cluster deployment
func (t *clusterRequestReconcilerTask) createOrUpdateClusterResources(
	ctx context.Context, clusterInstance *unstructured.Unstructured) error {

	clusterName := clusterInstance.GetName()

	// Create the clusterInstance namespace.
	err := t.createClusterInstanceNamespace(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create cluster namespace %s: %w", clusterName, err)
	}

	// TODO: remove the BMC secrets creation when hw plugin is ready
	err = t.createClusterInstanceBMCSecrets(ctx, clusterName)
	if err != nil {
		return err
	}

	// If we got to this point, we can assume that all the keys up to the BMC details
	// exists since ClusterInstance has nodes mandatory.
	specInterface, specExists := clusterInstance.Object["spec"]
	if !specExists {
		// Unlikely to happen
		return utils.NewInputError(
			"\"spec\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it is missing",
			t.object.Name,
		)
	}
	spec := specInterface.(map[string]interface{})

	// Copy the pull secret from the cluster template namespace to the
	// clusterInstance namespace.
	err = t.createPullSecret(ctx, clusterName, spec)
	if err != nil {
		return fmt.Errorf("failed to create pull Secret for cluster %s: %w", clusterName, err)
	}

	// Copy the extra-manifests ConfigMaps from the cluster template namespace
	// to the clusterInstance namespace.
	err = t.createExtraManifestsConfigMap(ctx, clusterName, spec)
	if err != nil {
		return fmt.Errorf("failed to create extraManifests ConfigMap for cluster %s: %w", clusterName, err)
	}

	// Create the cluster ConfigMap which will be used by ACM policies.
	err = t.createPoliciesConfigMap(ctx, clusterName, spec)
	if err != nil {
		return fmt.Errorf("failed to create policy template ConfigMap for cluster %s: %w", clusterName, err)
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

	configMapInterfaceArr := configMapRefInterface.([]interface{})
	for index, configMap := range configMapInterfaceArr {

		configMapNameInterface, configMapNameExists := configMap.(map[string]interface{})["name"]
		if !configMapNameExists {
			return utils.NewInputError(
				"\"spec.extraManifestsRefs[%d].name\" expected to exist in the rendered of "+
					"ClusterInstance for ClusterRequest %s, but it is missing",
				index, t.object.Name,
			)
		}

		// Make sure the extra-manifests ConfigMap exists in the clusterTemplate namespace.
		// The clusterRequest namespace is the same as the clusterTemplate namespace.
		configMap := &corev1.ConfigMap{}
		extraManifestCmName := configMapNameInterface.(string)
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, t.client, extraManifestCmName, t.object.Namespace, configMap)
		if err != nil {
			return err
		}
		if !configMapExists {
			return utils.NewInputError(
				"extra-manifests configmap %s expected to exist in the %s namespace, but it is missing",
				extraManifestCmName, t.object.Namespace)
		}

		// Create the extra-manifests ConfigMap in the clusterInstance namespace
		newExtraManifestsConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      extraManifestCmName,
				Namespace: clusterName,
			},
			Data: configMap.Data,
		}
		if err := utils.CreateK8sCR(ctx, t.client, newExtraManifestsConfigMap, t.object, utils.UPDATE); err != nil {
			return err
		}
	}

	return nil
}

// createPullSecret copies the pull secret from the cluster template namespace
// to the clusterInstance namespace
func (t *clusterRequestReconcilerTask) createPullSecret(
	ctx context.Context, clusterName string, spec map[string]interface{}) error {

	// If we got to this point, we can assume that all the keys exist, including
	// clusterName
	pullSecretRefInterface, pullSecretRefExists := spec["pullSecretRef"]
	if !pullSecretRefExists {
		return utils.NewInputError(
			"\"spec.pullSecretRef\" key expected to exist in the rendered ClusterInstance "+
				"for ClusterRequest %s, but it is missing",
			t.object.Name,
		)
	}
	pullSecretInterface := pullSecretRefInterface.(map[string]interface{})
	pullSecretNameInterface, pullSecretNameExists := pullSecretInterface["name"]
	if !pullSecretNameExists {
		return utils.NewInputError(
			"\"spec.pullSecretRef.name\" key expected to exist in the rendered ClusterInstance "+
				"for ClusterRequest %s, but it is missing",
			t.object.Name,
		)
	}

	// Check the pull secret already exists in the clusterTemplate namespace.
	// The clusterRequest namespace is same as the clusterTemplate namespace.
	pullSecret := &corev1.Secret{}
	pullSecretName := pullSecretNameInterface.(string)
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, pullSecretName, t.object.Namespace, pullSecret)
	if err != nil {
		return err
	}
	if !pullSecretExistsInTemplateNamespace {
		return utils.NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
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

	// Add ClusterRequest labels to the namespace
	labels := make(map[string]string)
	labels[clusterRequestNameLabel] = t.object.Name
	labels[clusterRequestNamespaceLabel] = t.object.Namespace
	namespace.SetLabels(labels)

	err := utils.CreateK8sCR(ctx, t.client, namespace, nil, utils.UPDATE)
	if err != nil {
		return err
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return utils.NewInputError("the namespace %s is terminating", clusterName)
	}

	return nil
}

// createClusterInstanceBMCSecrets creates all the BMC secrets needed by the nodes included
// in the ClusterRequest.
func (t *clusterRequestReconcilerTask) createClusterInstanceBMCSecrets(
	ctx context.Context, clusterName string) error {

	// The BMC credential details are for now obtained from the ClusterRequest.
	inputData, err := t.getClusterTemplateInputFromClusterRequest(&t.object.Spec.ClusterTemplateInput.ClusterInstanceInput)
	if err != nil {
		return err
	}

	// If we got to this point, we can assume that all the keys up to the BMC details
	// exists since ClusterInstance has nodes mandatory.
	nodesInterface, nodesExist := inputData["nodes"]
	if !nodesExist {
		// Unlikely to happen
		return utils.NewInputError(
			"\"spec.nodes\" expected to exist in the rendered ClusterInstance for ClusterRequest %s, but it is missing",
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
func (t *clusterRequestReconcilerTask) copyHwMgrPluginBMCSecret(ctx context.Context, name, sourceNamespace, targetNamespace string) error {

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

func (t *clusterRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {

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
		return nil, err
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	if !clusterTemplateRefExists {
		return nil, utils.NewInputError(
			fmt.Sprintf(
				"the referenced ClusterTemplate (%s) does not exist in the %s namespace",
				t.object.Spec.ClusterTemplateRef, t.object.Namespace))
	}
	return clusterTemplateRef, nil
}

// validateClusterTemplateInputMatchesSchema validates if the given clusterTemplateInput matches the
// provided inputDataSchema of the ClusterTemplate
func (t *clusterRequestReconcilerTask) validateClusterTemplateInputMatchesSchema(
	clusterTemplateInputSchema *runtime.RawExtension, clusterTemplateInput map[string]any) error {

	// Get the input data in string format.
	jsonString, err := json.Marshal(clusterTemplateInput)
	if err != nil {
		return fmt.Errorf("error marshaling the ClusterTemplate input data: %w", err)
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	err = utils.ValidateJsonAgainstJsonSchema(
		string(clusterTemplateInputSchema.Raw), string(jsonString))
	if err != nil {
		return fmt.Errorf("the provided clusterTemplateInput does not "+
			"match the schema from the ClusterTemplate (%s): %w", t.object.Spec.ClusterTemplateRef, err)
	}

	return nil
}

// createPoliciesConfigMap creates the cluster ConfigMap which will be used
// by the ACM policies.
func (t *clusterRequestReconcilerTask) createPoliciesConfigMap(
	ctx context.Context, clusterName string, spec map[string]interface{}) error {

	// Check the cluster version for the cluster-version label.
	err := utils.CheckClusterLabelsForPolicies(spec, clusterName)
	if err != nil {
		return err
	}

	return t.createPolicyTemplateConfigMap(ctx, clusterName)
}

// createPolicyTemplateConfigMap updates the keys of the default ConfigMap to match the
// clusterTemplate and the cluster version and creates/updates the ConfigMap for the
// required version of the policy template.
func (t *clusterRequestReconcilerTask) createPolicyTemplateConfigMap(
	ctx context.Context, clusterName string) error {

	// If there is no policy configuration data, log a message and return without error.
	if len(t.clusterInput.policyTemplateData) == 0 {
		t.logger.InfoContext(ctx, "Policy template data is empty")
		return nil
	}

	// Update the keys to match the ClusterTemplate name and the version.
	finalPolicyTemplateData := make(map[string]string)
	for key, value := range t.clusterInput.policyTemplateData {
		finalPolicyTemplateData[key] = value.(string)
	}

	// Put all the data from the mergedPolicyTemplateData in a configMap in the same
	// namespace as the templated ACM policies.
	// The namespace is: ztp + <clustertemplate-namespace>
	policyTemplateConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pg", clusterName),
			Namespace: fmt.Sprintf("ztp-%s", t.object.Namespace),
		},
		Data: finalPolicyTemplateData,
	}

	return utils.CreateK8sCR(ctx, t.client, policyTemplateConfigMap, t.object, utils.UPDATE)
}

func (r *ClusterRequestReconciler) finalizeClusterRequest(
	ctx context.Context, clusterRequest *oranv1alpha1.ClusterRequest) error {

	var labels = map[string]string{
		clusterRequestNameLabel:      clusterRequest.Name,
		clusterRequestNamespaceLabel: clusterRequest.Namespace,
	}
	listOpts := []client.ListOption{
		client.MatchingLabels(labels),
	}

	// Query the NodePool created by this ClusterRequest. Delete it if exists.
	nodePoolList := &hwv1alpha1.NodePoolList{}
	if err := r.Client.List(ctx, nodePoolList, listOpts...); err != nil {
		return err
	}
	for _, nodePool := range nodePoolList.Items {
		if err := r.Client.Delete(ctx, &nodePool); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	// If the ClusterInstance has been created by this ClusterRequest, delete it.
	// The SiteConfig operator will also delete the namespace.
	clusterInstanceList := &siteconfig.ClusterInstanceList{}
	if err := r.Client.List(ctx, clusterInstanceList, listOpts...); err != nil {
		return err
	}
	for _, clusterInstance := range clusterInstanceList.Items {
		if err := r.Client.Delete(ctx, &clusterInstance); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	if len(clusterInstanceList.Items) == 0 {
		// If the ClusterInstance has not been created. Query the namespace created by
		// this ClusterRequest. Delete it if exists.
		namespaceList := &corev1.NamespaceList{}
		if err := r.Client.List(ctx, namespaceList, listOpts...); err != nil {
			return err
		}
		for _, ns := range namespaceList.Items {
			if err := r.Client.Delete(ctx, &ns); client.IgnoreNotFound(err) != nil {
				return err
			}
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
	nodePool *hwv1alpha1.NodePool) bool {

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

	// Update the Cluster Request Status with status from the NodePool object.
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
	clusterInstance *unstructured.Unstructured, nodePool *hwv1alpha1.NodePool) bool {

	hwNodes, macAddresses := t.collectNodeDetails(ctx, nodePool)
	if hwNodes == nil {
		return false
	}

	if !t.copyBMCSecrets(ctx, hwNodes, nodePool) {
		return false
	}

	if !t.applyNodeConfiguration(ctx, hwNodes, macAddresses, nodePool, clusterInstance) {
		return false
	}

	return true
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *clusterRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodePool *hwv1alpha1.NodePool) bool {

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

// collectNodeDetails collects BMC details and boot MAC addresses.
func (t *clusterRequestReconcilerTask) collectNodeDetails(ctx context.Context,
	nodePool *hwv1alpha1.NodePool) (map[string][]nodeInfo, map[string]string) {

	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]nodeInfo)
	macAddresses := make(map[string]string)

	for _, nodeName := range nodePool.Status.Properties.NodeNames {
		node := &hwv1alpha1.Node{}
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
			return nil, nil
		}
		if !exists {
			t.logger.ErrorContext(
				ctx,
				"Node object does not exist",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.Bool("exists", exists),
			)
			return nil, nil
		}
		// Verify the node object is generated from the expected pool
		if node.Spec.NodePool != nodePool.GetName() {
			t.logger.ErrorContext(
				ctx,
				"Node object is not from the expected NodePool",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil, nil
		}

		if node.Status.BMC == nil {
			t.logger.ErrorContext(
				ctx,
				"Node status does not have BMC details",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil, nil
		}
		// Store the nodeInfo per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], nodeInfo{
			bmcAddress:     node.Status.BMC.Address,
			bmcCredentials: node.Status.BMC.CredentialsName,
			nodeName:       node.Name,
		})

		if node.Status.BootMACAddress != "" {
			macAddresses[node.Status.BMC.Address] = node.Status.BootMACAddress
		}
	}

	return hwNodes, macAddresses
}

// copyBMCSecrets copies BMC secrets from the plugin namespace to the cluster namespace.
func (t *clusterRequestReconcilerTask) copyBMCSecrets(ctx context.Context, hwNodes map[string][]nodeInfo,
	nodePool *hwv1alpha1.NodePool) bool {

	for _, nodeInfos := range hwNodes {
		for _, node := range nodeInfos {
			err := t.copyHwMgrPluginBMCSecret(ctx, node.bmcCredentials, nodePool.GetNamespace(), nodePool.GetName())
			if err != nil {
				t.logger.ErrorContext(
					ctx,
					"Failed to copy BMC secret from the plugin namespace to the cluster namespace",
					slog.String("name", node.bmcCredentials),
					slog.String("plugin", nodePool.GetNamespace()),
					slog.String("cluster", nodePool.GetName()),
				)
				return false
			}
		}
	}
	return true
}

// applyNodeConfiguration updates the clusterInstance with BMC details and bootMacAddress.
func (t *clusterRequestReconcilerTask) applyNodeConfiguration(ctx context.Context, hwNodes map[string][]nodeInfo,
	macAddresses map[string]string, nodePool *hwv1alpha1.NodePool, clusterInstance *unstructured.Unstructured) bool {

	if nodes, ok := clusterInstance.Object["spec"].(map[string]any)["nodes"].([]interface{}); ok {
		for _, node := range nodes {
			if nodeMap, ok := node.(map[string]interface{}); ok {
				if role, ok := nodeMap["role"].(string); ok {
					// Check if the node's role matches any key in hwNodes
					if nodeInfos, exists := hwNodes[role]; exists && len(nodeInfos) > 0 {
						nodeMap["bmcAddress"] = nodeInfos[0].bmcAddress
						nodeMap["bmcCredentialsName"] = map[string]interface{}{"name": nodeInfos[0].bmcCredentials}
						if mac, macExists := macAddresses[nodeInfos[0].bmcAddress]; macExists {
							nodeMap["bootMACAddress"] = mac
						}
						// indicates which host has been assigned to the node
						if !t.updateNodeStatusWithHostname(ctx, nodeInfos[0].nodeName, nodeMap["hostName"].(string),
							nodePool.Namespace) {
							return false
						}
						hwNodes[role] = nodeInfos[1:]
					}
				}
			}
		}
	}
	return true
}

// Updates the Node status with the hostname after BMC information has been assigned.
func (t *clusterRequestReconcilerTask) updateNodeStatusWithHostname(ctx context.Context, nodeName, hostname, namespace string) bool {
	node := &hwv1alpha1.Node{}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodeName, namespace, node)
	if err != nil || !exists {
		t.logger.ErrorContext(
			ctx,
			"Failed to get the Node object for updating hostname",
			slog.String("name", nodeName),
			slog.String("namespace", namespace),
			slog.String("error", err.Error()),
			slog.Bool("exists", exists),
		)
		return false
	}

	node.Status.Hostname = hostname
	err = t.client.Status().Update(ctx, node)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update Node status with hostname",
			slog.String("name", node.GetName()),
			slog.String("hostname", hostname),
			slog.String("namespace", namespace),
			slog.String("error", err.Error()),
		)
		return false
	}
	return true
}

// updateHardwareProvisioningStatus updates the status for the created ClusterInstance
func (t *clusterRequestReconcilerTask) updateHardwareProvisioningStatus(
	ctx context.Context, nodePool *hwv1alpha1.NodePool) error {

	var err error
	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &oranv1alpha1.NodePoolRef{}
	}

	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()

	if len(nodePool.Status.Conditions) > 0 {
		provisionedCondition := meta.FindStatusCondition(
			nodePool.Status.Conditions, "Provisioned")
		if provisionedCondition != nil && provisionedCondition.Status == metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.CRconditionTypes.HardwareProvisioned,
				utils.CRconditionReasons.Completed,
				metav1.ConditionTrue,
				"Hareware provisioning completed",
			)
		} else {
			provisioningCondition := meta.FindStatusCondition(
				nodePool.Status.Conditions, "Provisioning")
			if provisioningCondition != nil && provisioningCondition.Status == metav1.ConditionTrue {
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.CRconditionTypes.HardwareProvisioned,
					utils.CRconditionReasons.InProgress,
					metav1.ConditionFalse,
					"Hareware provisioning is in progress",
				)
			} else {
				failedCondition := meta.FindStatusCondition(
					nodePool.Status.Conditions, "Failed")
				if failedCondition != nil && failedCondition.Status == metav1.ConditionTrue {
					utils.SetStatusCondition(&t.object.Status.Conditions,
						utils.CRconditionTypes.HardwareProvisioned,
						utils.CRconditionReasons.Failed,
						metav1.ConditionFalse,
						"Hareware provisioning failed",
					)
				} else {
					utils.SetStatusCondition(&t.object.Status.Conditions,
						utils.CRconditionTypes.HardwareProvisioned,
						utils.CRconditionReasons.Unknown,
						metav1.ConditionUnknown,
						"Unknown state of hardware provisioning",
					)
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

// findClusterInstanceForClusterRequest maps the ClusterInstance created by a
// a ClusterRequest to a reconciliation request.
func (r *ClusterRequestReconciler) findClusterInstanceForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newClusterInstance := event.ObjectNew.(*siteconfig.ClusterInstance)
	crName, nameExists := newClusterInstance.GetLabels()[clusterRequestNameLabel]
	crNamespace, namespaceExists := newClusterInstance.GetLabels()[clusterRequestNamespaceLabel]
	if nameExists && namespaceExists {
		// Create reconciling requests only for the ClusterRequest that has generated
		// the current ClusterInstance.
		r.Logger.Info(
			"[findClusterInstanceForClusterRequest] Add new reconcile request for ClusterRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: crNamespace,
					Name:      crName,
				},
			},
		)
	}
}

// findNodePoolForClusterRequest maps the NodePool created by a
// a ClusterRequest to a reconciliation request.
func (r *ClusterRequestReconciler) findNodePoolForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newNodePool := event.ObjectNew.(*hwv1alpha1.NodePool)

	crName, nameExists := newNodePool.GetLabels()[clusterRequestNameLabel]
	crNamespace, namespaceExists := newNodePool.GetLabels()[clusterRequestNamespaceLabel]
	if nameExists && namespaceExists {
		// Create reconciling requests only for the ClusterRequest that has generated
		// the current NodePool.
		r.Logger.Info(
			"[findNodePoolForClusterRequest] Add new reconcile request for ClusterRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: crNamespace,
					Name:      crName,
				},
			},
		)
	}
}

// findClusterTemplateForClusterRequest maps the ClusterTemplates used by ClusterRequests
// to reconciling requests for those ClusterRequests.
func (r *ClusterRequestReconciler) findClusterTemplateForClusterRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case we can use either new or old object.
	newClusterTemplate := event.ObjectNew.(*oranv1alpha1.ClusterTemplate)

	// Get all the clusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests, client.InNamespace(newClusterTemplate.GetNamespace()))
	if err != nil {
		r.Logger.Error("[findClusterRequestsForClusterTemplate] Error listing ClusterRequests. ", "Error: ", err)
		return
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

	// Get the ClusterInstance
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newManagedCluster.Name,
		Name:      newManagedCluster.Name,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as this managedcluster is not deployed/managed by ClusterInstance
			return
		}
		r.Logger.Error("[findManagedClusterForClusterRequest] Error getting ClusterInstance. ", "Error: ", err)
		return
	}

	crName, nameExists := clusterInstance.GetLabels()[clusterRequestNameLabel]
	crNamespace, namespaceExists := clusterInstance.GetLabels()[clusterRequestNamespaceLabel]
	if nameExists && namespaceExists {
		r.Logger.Info(
			"[findManagedClusterForClusterRequest] Add new reconcile request for ClusterRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: crNamespace,
					Name:      crName,
				},
			},
		)
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
			handler.Funcs{UpdateFunc: r.findClusterTemplateForClusterRequest},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return true },
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
					// Watch on ClusterInstance status conditions changes only
					ciOld := e.ObjectOld.(*siteconfig.ClusterInstance)
					ciNew := e.ObjectNew.(*siteconfig.ClusterInstance)

					if ciOld.GetGeneration() == ciNew.GetGeneration() {
						if !equality.Semantic.DeepEqual(ciOld.Status.Conditions, ciNew.Status.Conditions) {
							return true
						}
					}
					return false
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&hwv1alpha1.NodePool{},
			handler.Funcs{
				UpdateFunc: r.findNodePoolForClusterRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes.
					// TODO: Filter on further conditions that the ClusterRequest is interested in
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
