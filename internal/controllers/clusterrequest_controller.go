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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ClusterRequestReconciler reconciles a ClusterRequest object
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
	interfaces     []*hwv1alpha1.Interface
}

type deleteOrUpdateEvent interface {
	event.UpdateEvent | event.DeleteEvent
}

const (
	clusterRequestFinalizer      = "clusterrequest.oran.openshift.io/finalizer"
	clusterRequestNameLabel      = "clusterrequest.oran.openshift.io/name"
	clusterRequestNamespaceLabel = "clusterrequest.oran.openshift.io/namespace"
	ztpDoneLabel                 = "ztp-done"
)

//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=clusterrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=list;watch

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
			return t.checkClusterDeployConfigState(ctx)
		}
		// internal error that might recover
		return requeueWithError(err)
	}

	// Render and validate ClusterInstance
	renderedClusterInstance, err := t.handleRenderClusterInstance(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		return requeueWithError(err)
	}

	// Handle the creation of resources required for cluster deployment
	err = t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			_, err = t.checkClusterDeployConfigState(ctx)
			if err != nil {
				return requeueWithError(err)
			}
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
			return t.checkClusterDeployConfigState(ctx)
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
		// TODO: Remove this check once hwmgr plugin(s) are fully utilized
		if renderedNodePool.ObjectMeta.Namespace != utils.TempDellPluginNamespace && renderedNodePool.ObjectMeta.Namespace != utils.UnitTestHwmgrNamespace {
			return ctrl.Result{RequeueAfter: time.Second * 30}, nil
		}
	}

	hwProvisionedCond := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.CRconditionTypes.HardwareProvisioned))
	if hwProvisionedCond != nil {
		// TODO: check hwProvisionedCond.Status == metav1.ConditionTrue
		// after hw plugin is ready

		// Handle the cluster install with ClusterInstance
		err := t.handleClusterInstallation(ctx, renderedClusterInstance)
		if err != nil {
			return requeueWithError(err)
		}
	}

	// Handle policy configuration only after the cluster provisioning
	// has started, and not failed or timedout (completed, in-progress or unknown)
	if utils.IsClusterProvisionPresent(t.object) &&
		!utils.IsClusterProvisionTimedOutOrFailed(t.object) {

		// Handle configuration through policies.
		requeue, err := t.handleClusterPolicyConfiguration(ctx)
		if err != nil {
			return requeueWithError(err)
		}

		// Requeue if cluster provisioning is not completed (in-progress or unknown)
		// or there are enforce policies that are not Compliant
		if !utils.IsClusterProvisionCompleted(t.object) || requeue {
			return requeueWithLongInterval(), nil
		}
	}

	return doNotRequeue(), nil
}

// checkClusterDeployConfigState checks the current deployment and configuration state of
// the cluster by evaluating the statuses of related resources like NodePool, ClusterInstance
// and policy configuration when applicable, and update the corresponding ClusterRequest
// status conditions
func (t *clusterRequestReconcilerTask) checkClusterDeployConfigState(ctx context.Context) (ctrl.Result, error) {
	// Check the NodePool status if exists
	if t.object.Status.NodePoolRef == nil {
		return doNotRequeue(), nil
	}
	nodePool := &hwv1alpha1.NodePool{}
	nodePool.SetName(t.object.Status.NodePoolRef.Name)
	nodePool.SetNamespace(t.object.Status.NodePoolRef.Namespace)
	t.checkNodePoolProvisionStatus(ctx, nodePool)

	hwProvisionedCond := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.CRconditionTypes.HardwareProvisioned))
	if hwProvisionedCond != nil {
		// Check the ClusterInstance status if exists
		if t.object.Status.ClusterDetails == nil {
			return doNotRequeue(), nil
		}
		err := t.checkClusterProvisionStatus(
			ctx, t.object.Status.ClusterDetails.Name)
		if err != nil {
			return requeueWithError(err)
		}
	}

	// Check the policy configuration status only after the cluster provisioning
	// has started, and not failed or timedout
	if utils.IsClusterProvisionPresent(t.object) &&
		!utils.IsClusterProvisionTimedOutOrFailed(t.object) {
		requeue, err := t.handleClusterPolicyConfiguration(ctx)
		if err != nil {
			return requeueWithError(err)
		}
		// Requeue if Cluster Provisioned is not completed (in-progress or unknown)
		// or there are enforce policies that are not Compliant
		if !utils.IsClusterProvisionCompleted(t.object) || requeue {
			return requeueWithLongInterval(), nil
		}
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

	// Validate the clusterinstance input from ClusterRequest against the schema
	clusterTemplateInputMap, err := t.getClusterTemplateInputFromClusterRequest(
		&t.object.Spec.ClusterTemplateInput.ClusterInstanceInput)
	if err != nil {
		return utils.NewInputError("failed to get the ClusterTemplate input for ClusterInstance: %s", err.Error())
	}
	err = t.validateClusterTemplateInputMatchesSchema(
		&clusterTemplate.Spec.InputDataSchema.ClusterInstanceSchema,
		clusterTemplateInputMap,
		utils.ClusterInstanceDataType)
	if err != nil {
		return utils.NewInputError("failed to validate ClusterTemplate input matches schema: %s", err.Error())
	}
	// Get the merged clusterinstance input data
	mergedClusterInstanceData, err := t.getMergedClusterInputData(ctx, clusterTemplate, utils.ClusterInstanceDataType)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	// Validate the merged policytemplate input data matches the schema
	mergedPolicyTemplateData, err := t.getMergedClusterInputData(ctx, clusterTemplate, utils.PolicyTemplateDataType)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}
	err = t.validateClusterTemplateInputMatchesSchema(
		&clusterTemplate.Spec.InputDataSchema.PolicyTemplateSchema,
		mergedPolicyTemplateData,
		utils.PolicyTemplateDataType)
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

	if dataType == utils.ClusterInstanceDataType {
		// Special handling for overrides of ClusterInstance's extraLabels and extraAnnotations.
		// The clusterTemplateInputMap will be overridden with the values from defaut configmap
		// if same labels/annotations exist in both.
		if err := utils.OverrideClusterInstanceLabelsOrAnnotations(
			clusterTemplateInputMap, clusterTemplateDefaultsMap); err != nil {
			return nil, utils.NewInputError(err.Error())
		}
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

// handleRenderClusterInstance handles the ClusterInstance rendering and validation.
func (t *clusterRequestReconcilerTask) handleRenderClusterInstance(ctx context.Context) (*siteconfig.ClusterInstance, error) {
	renderedClusterInstance, err := t.renderClusterInstanceTemplate(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render and validate the ClusterInstance for ClusterRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render and validate ClusterInstance: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered the ClusterInstance and validated it with dry-run",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"ClusterInstance rendered and passed dry-run validation",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to handle ClusterInstance rendering and validation: %w", err)
	}
	return renderedClusterInstance, nil
}

func (t *clusterRequestReconcilerTask) renderClusterInstanceTemplate(
	ctx context.Context) (*siteconfig.ClusterInstance, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering the ClusterInstance template for ClusterRequest",
		slog.String("name", t.object.Name),
	)

	// Wrap the merged ClusterInstance data in a map with key "Cluster"
	// This data object will be consumed by the clusterInstance template
	mergedClusterInstanceData := map[string]any{
		"Cluster": t.clusterInput.clusterInstanceData,
	}

	renderedClusterInstance := &siteconfig.ClusterInstance{}
	renderedClusterInstanceUnstructure, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, utils.NewInputError("failed to render the ClusterInstance template for ClusterRequest: %w", err)
	} else {
		// Add ClusterRequest labels to the generated ClusterInstance
		labels := make(map[string]string)
		labels[clusterRequestNameLabel] = t.object.Name
		labels[clusterRequestNamespaceLabel] = t.object.Namespace
		renderedClusterInstanceUnstructure.SetLabels(labels)

		// Create the ClusterInstance namespace if not exist.
		ciName := renderedClusterInstanceUnstructure.GetName()
		err = t.createClusterInstanceNamespace(ctx, ciName)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster namespace %s: %w", ciName, err)
		}

		// Check for updates to immutable fields in the ClusterInstance, if it exists.
		// Once provisioning has started or reached a final state (Completed or Failed),
		// updates to immutable fields in the ClusterInstance spec are disallowed,
		// with the exception of scaling up/down when Cluster provisioning is completed.
		crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions,
			string(utils.CRconditionTypes.ClusterProvisioned))
		if crProvisionedCond != nil && crProvisionedCond.Reason != string(utils.CRconditionReasons.Unknown) {
			existingClusterInstance := &unstructured.Unstructured{}
			existingClusterInstance.SetGroupVersionKind(
				renderedClusterInstanceUnstructure.GroupVersionKind())
			ciExists, err := utils.DoesK8SResourceExist(
				ctx, t.client, ciName, ciName, existingClusterInstance,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get ClusterInstance (%s): %w",
					ciName, err)
			}
			if ciExists {
				updatedFields, scalingNodes, err := utils.FindClusterInstanceImmutableFieldUpdates(
					existingClusterInstance, renderedClusterInstanceUnstructure)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to find immutable field updates for ClusterInstance (%s): %w", ciName, err)
				}

				var disallowedChanges []string
				if len(updatedFields) != 0 {
					disallowedChanges = append(disallowedChanges, updatedFields...)
				}
				if len(scalingNodes) != 0 &&
					crProvisionedCond.Reason != string(utils.CRconditionReasons.Completed) {
					// In-progress || Failed
					disallowedChanges = append(disallowedChanges, scalingNodes...)
				}

				if len(disallowedChanges) != 0 {
					return nil, utils.NewInputError(fmt.Sprintf(
						"detected changes in immutable fields: %s", strings.Join(disallowedChanges, ", ")))
				}
			}
		}

		// Validate the rendered ClusterInstance with dry-run
		isDryRun := true
		err = t.applyClusterInstance(ctx, renderedClusterInstanceUnstructure, isDryRun)
		if err != nil {
			return nil, fmt.Errorf("failed to validate the rendered ClusterInstance with dry-run: %w", err)
		}

		// Convert unstructured to siteconfig.ClusterInstance type
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(
			renderedClusterInstanceUnstructure.Object, renderedClusterInstance); err != nil {
			// Unlikely to happen since dry-run validation has passed
			return nil, utils.NewInputError("failed to convert to siteconfig.ClusterInstance type: %w", err)
		}
	}

	return renderedClusterInstance, nil
}

func (t *clusterRequestReconcilerTask) handleClusterResources(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
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
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {
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
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {

	nodePool := &hwv1alpha1.NodePool{}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ClusterRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateCmName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplateCm, err := utils.GetConfigmap(ctx, t.client, hwTemplateCmName, utils.InventoryNamespace)
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

	roleCounts := make(map[string]int)
	err = utils.ProcessClusterNodeGroups(clusterInstance, nodeGroup, roleCounts)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the process node spec err: %w", err)
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

	// Add boot interface label to the generated nodePool
	annotation := make(map[string]string)
	annotation[utils.HwTemplateBootIfaceLabel] = hwTemplateCm.Data[utils.HwTemplateBootIfaceLabel]
	nodePool.SetAnnotations(annotation)

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

		checkType := false
		err := utils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, checkType) // clusterTemplateInput overrides the defaults
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

func (t *clusterRequestReconcilerTask) applyClusterInstance(ctx context.Context, clusterInstance client.Object, isDryRun bool) error {
	var operationType string

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
			return fmt.Errorf("failed to get ClusterInstance: %w", err)
		}

		operationType = utils.OperationTypeCreated
		opts := []client.CreateOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}

		// Create the ClusterInstance
		err = t.client.Create(ctx, clusterInstance, opts...)
		if err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to create ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError(err.Error())
		}
	} else {
		if _, ok := clusterInstance.(*siteconfig.ClusterInstance); ok {
			// No update needed, return
			if equality.Semantic.DeepEqual(existingClusterInstance.Spec,
				clusterInstance.(*siteconfig.ClusterInstance).Spec) {
				return nil
			}
		}

		// Make sure these fields from existing object are copied
		clusterInstance.SetResourceVersion(existingClusterInstance.GetResourceVersion())
		clusterInstance.SetFinalizers(existingClusterInstance.GetFinalizers())
		clusterInstance.SetLabels(existingClusterInstance.GetLabels())
		clusterInstance.SetAnnotations(existingClusterInstance.GetAnnotations())

		operationType = utils.OperationTypeUpdated
		opts := []client.PatchOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}
		patch := client.MergeFrom(existingClusterInstance.DeepCopy())
		if err := t.client.Patch(ctx, clusterInstance, patch, opts...); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to patch ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError(err.Error())
		}
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Rendered ClusterInstance %s in the namespace %s %s",
			clusterInstance.GetName(),
			clusterInstance.GetNamespace(),
			operationType,
		),
	)
	return nil
}

// checkClusterProvisionStatus checks the status of cluster provisioning
func (t *clusterRequestReconcilerTask) checkClusterProvisionStatus(
	ctx context.Context, clusterInstanceName string) error {

	clusterInstance := &siteconfig.ClusterInstance{}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstanceName, clusterInstanceName, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get ClusterInstance %s: %w", clusterInstanceName, err)
	}
	if !exists {
		return nil
	}
	// Check ClusterInstance status and update the corresponding ClusterRequest status conditions.
	t.updateClusterInstanceProcessedStatus(clusterInstance)
	t.updateClusterProvisionStatus(clusterInstance)

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
	}

	return nil
}

// handleClusterInstallation creates/updates the ClusterInstance to handle the cluster provisioning.
func (t *clusterRequestReconcilerTask) handleClusterInstallation(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	isDryRun := false
	err := t.applyClusterInstance(ctx, clusterInstance, isDryRun)
	if err != nil {
		if !utils.IsInputError(err) {
			return fmt.Errorf("failed to apply the rendered ClusterInstance (%s): %s", clusterInstance.Name, err.Error())
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			fmt.Sprintf(
				"Failed to apply the rendered ClusterInstance (%s): %s",
				clusterInstance.Name, err.Error()),
		)
	} else {
		// Set ClusterDetails
		if t.object.Status.ClusterDetails == nil {
			t.object.Status.ClusterDetails = &oranv1alpha1.ClusterDetails{}
		}
		t.object.Status.ClusterDetails.Name = clusterInstance.GetName()
	}

	// Continue checking the existing ClusterInstance provision status
	if err := t.checkClusterProvisionStatus(ctx, clusterInstance.Name); err != nil {
		return err
	}
	return nil
}

// handleClusterPolicyConfiguration updates the ClusterRequest status to reflect the status
// of the policies that match the managed cluster created through the ClusterRequest.
func (t *clusterRequestReconcilerTask) handleClusterPolicyConfiguration(ctx context.Context) (
	requeue bool, err error) {
	if t.object.Status.ClusterDetails == nil {
		return false, fmt.Errorf("status.clusterDetails is empty")
	}

	// Get all the child policies in the namespace of the managed cluster created through
	// the ClusterRequest.
	policies := &policiesv1.PolicyList{}
	listOpts := []client.ListOption{
		client.HasLabels{utils.ChildPolicyRootPolicyLabel},
		client.InNamespace(t.object.Status.ClusterDetails.Name),
	}

	err = t.client.List(ctx, policies, listOpts...)
	if err != nil {
		return false, fmt.Errorf("failed to list Policies: %w", err)
	}

	allPoliciesCompliant := true
	nonCompliantPolicyInEnforce := false
	var targetPolicies []oranv1alpha1.PolicyDetails
	// Go through all the policies and get those that are matched with the managed cluster created
	// by the current cluster request.
	for _, policy := range policies.Items {
		if policy.Status.ComplianceState != policiesv1.Compliant {
			allPoliciesCompliant = false
			if strings.EqualFold(string(policy.Spec.RemediationAction), string(policiesv1.Enforce)) {
				nonCompliantPolicyInEnforce = true
			}
		}
		// Child policy name = parent_policy_namespace.parent_policy_name
		policyNameArr := strings.Split(policy.Name, ".")
		targetPolicy := &oranv1alpha1.PolicyDetails{
			Compliant:         string(policy.Status.ComplianceState),
			PolicyName:        policyNameArr[1],
			PolicyNamespace:   policyNameArr[0],
			RemediationAction: string(policy.Spec.RemediationAction),
		}
		targetPolicies = append(targetPolicies, *targetPolicy)
	}
	err = t.updateConfigurationAppliedStatus(
		ctx, targetPolicies, allPoliciesCompliant, nonCompliantPolicyInEnforce)
	if err != nil {
		return false, err
	}
	err = t.updateZTPStatus(ctx, allPoliciesCompliant)
	if err != nil {
		return false, err
	}

	// If there are policies that are not Compliant, we need to requeue and see if they
	// time out or complete.
	return nonCompliantPolicyInEnforce, nil
}

// updateZTPStatus updates status.ClusterDetails.ZtpStatus.
func (t *clusterRequestReconcilerTask) updateZTPStatus(ctx context.Context, allPoliciesCompliant bool) error {
	// Check if the cluster provision has started.
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(utils.CRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil {
		// If the provisioning has started, and the ZTP status is empty or not done.
		if t.object.Status.ClusterDetails.ZtpStatus != utils.ClusterZtpDone {
			t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpNotDone
			// If the provisioning finished and all the policies are compliant, then ZTP is done.
			if crProvisionedCond.Status == metav1.ConditionTrue && allPoliciesCompliant {
				// Once the ZTPStatus reaches ZTP Done, it will stay that way.
				t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			}
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update the ZTP status for ClusterRequest %s: %w", t.object.Name, err)
	}
	return nil
}

// hasPolicyConfigurationTimedOut determines if the policy configuration for the
// ClusterRequest has timed out.
func (t *clusterRequestReconcilerTask) hasPolicyConfigurationTimedOut(ctx context.Context) bool {
	policyTimedOut := false
	// Get the ConfigurationApplied condition.
	configurationAppliedCondition := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.CRconditionTypes.ConfigurationApplied))

	// If the condition does not exist, set the non compliant timestamp since we
	// get here just for policies that have a status different from Compliant.
	if configurationAppliedCondition == nil {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		return policyTimedOut
	}

	// If the current status of the Condition is false.
	if configurationAppliedCondition.Status == metav1.ConditionFalse {
		switch configurationAppliedCondition.Reason {
		case string(utils.CRconditionReasons.InProgress):
			// Check if the configuration application has timed out.
			if t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
			} else {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(t.object)
			}
		case string(utils.CRconditionReasons.TimedOut):
			policyTimedOut = true
		case string(utils.CRconditionReasons.Missing):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.OutOfDate):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.ClusterNotReady):
			// The cluster might not be ready because its being initially provisioned or
			// there are problems after provisionion, so it might be that NonCompliantAt
			// has been previously set.
			if !t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(t.object)
			}
		default:
			t.logger.InfoContext(ctx,
				fmt.Sprintf("Unexpected Reason for condition type %s",
					utils.CRconditionTypes.ConfigurationApplied,
				),
			)
		}
	} else if configurationAppliedCondition.Reason == string(utils.CRconditionReasons.Completed) {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
	}

	return policyTimedOut
}

// updateConfigurationAppliedStatus updates the ClusterRequest ConfigurationApplied condition
// based on the state of the policies matched with the managed cluster.
func (t *clusterRequestReconcilerTask) updateConfigurationAppliedStatus(
	ctx context.Context, targetPolicies []oranv1alpha1.PolicyDetails, allPoliciesCompliant bool,
	nonCompliantPolicyInEnforce bool) (err error) {
	err = nil

	defer func() {
		t.object.Status.Policies = targetPolicies
		// Update the current policy status.
		if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			err = fmt.Errorf("failed to update status for ClusterRequest %s: %w", t.object.Name, updateErr)
		} else {
			err = nil
		}
	}()

	if len(targetPolicies) == 0 {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Missing,
			metav1.ConditionFalse,
			"No configuration present",
		)
		return
	}

	// Update the ConfigurationApplied condition.
	if allPoliciesCompliant {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The configuration is up to date",
		)
		return
	}

	clusterIsReadyForPolicyConfig, err := utils.ClusterIsReadyForPolicyConfig(
		ctx, t.client, t.object.Status.ClusterDetails.Name,
	)
	if err != nil {
		return fmt.Errorf(
			"error determining if the cluster is ready for policy configuration: %w", err)
	}

	if !clusterIsReadyForPolicyConfig {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster %s (%s) is not ready for policy configuration",
				t.object.Status.ClusterDetails.Name,
				t.object.Status.ClusterDetails.Name,
			),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.ClusterNotReady,
			metav1.ConditionFalse,
			"The Cluster is not yet ready",
		)
		return
	}

	if nonCompliantPolicyInEnforce {
		policyTimedOut := t.hasPolicyConfigurationTimedOut(ctx)

		message := "The configuration is still being applied"
		reason := utils.CRconditionReasons.InProgress
		if policyTimedOut {
			message += ", but it timed out"
			reason = utils.CRconditionReasons.TimedOut
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ConfigurationApplied,
			reason,
			metav1.ConditionFalse,
			message,
		)
	} else {
		// No timeout is reported if all policies are in inform, just out of date.
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			"The configuration is out of date",
		)
	}

	return
}

func (t *clusterRequestReconcilerTask) updateClusterInstanceProcessedStatus(ci *siteconfig.ClusterInstance) {
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
		fmt.Sprintf("Applied and processed ClusterInstance (%s) successfully", ci.Name),
	)
}

func (t *clusterRequestReconcilerTask) updateClusterProvisionStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	// Search for ClusterInstance Provisioned condition
	ciProvisionedCondition := meta.FindStatusCondition(
		ci.Status.Conditions, string(hwv1alpha1.Provisioned))

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

	if utils.IsClusterProvisionPresent(t.object) {
		// Set the start timestamp if it's already set
		if t.object.Status.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
			t.object.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
		}

		// If it's not failed or completed, check if it has timed out
		if !utils.IsClusterProvisionCompletedOrFailed(t.object) {
			if time.Since(t.object.Status.ClusterDetails.ClusterProvisionStartedAt.Time) >
				time.Duration(t.object.Spec.Timeout.ClusterProvisioning)*time.Minute {
				// timed out
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.CRconditionTypes.ClusterProvisioned,
					utils.CRconditionReasons.TimedOut,
					metav1.ConditionFalse,
					"Cluster provisioning timed out",
				)
			}
		}
	}
}

// createOrUpdateClusterResources creates/updates all the resources needed for cluster deployment
func (t *clusterRequestReconcilerTask) createOrUpdateClusterResources(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterName := clusterInstance.GetName()

	// TODO: remove the BMC secrets creation when hw plugin is ready
	err := t.createClusterInstanceBMCSecrets(ctx, clusterName)
	if err != nil {
		return err
	}

	// Copy the pull secret from the cluster template namespace to the
	// clusterInstance namespace.
	err = t.createPullSecret(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create pull Secret for cluster %s: %w", clusterName, err)
	}

	// Copy the extra-manifests ConfigMaps from the cluster template namespace
	// to the clusterInstance namespace.
	err = t.createExtraManifestsConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create extraManifests ConfigMap for cluster %s: %w", clusterName, err)
	}

	// Create the cluster ConfigMap which will be used by ACM policies.
	err = t.createPoliciesConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create policy template ConfigMap for cluster %s: %w", clusterName, err)
	}

	return nil
}

// createExtraManifestsConfigMap copies the extra-manifests ConfigMaps from the
// cluster template namespace to the clusterInstance namespace.
func (t *clusterRequestReconcilerTask) createExtraManifestsConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	for _, extraManifestsRef := range clusterInstance.Spec.ExtraManifestsRefs {
		// Make sure the extra-manifests ConfigMap exists in the clusterTemplate namespace.
		// The clusterRequest namespace is the same as the clusterTemplate namespace.
		configMap := &corev1.ConfigMap{}
		extraManifestCmName := extraManifestsRef.Name
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, t.client, extraManifestCmName, t.object.Namespace, configMap)
		if err != nil {
			return fmt.Errorf("failed to check if ConfigMap exists: %w", err)
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
				Namespace: clusterInstance.Name,
			},
			Data: configMap.Data,
		}
		if err := utils.CreateK8sCR(ctx, t.client, newExtraManifestsConfigMap, t.object, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create extra-manifests ConfigMap: %w", err)
		}
	}

	return nil
}

// createPullSecret copies the pull secret from the cluster template namespace
// to the clusterInstance namespace
func (t *clusterRequestReconcilerTask) createPullSecret(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	// If we got to this point, we can assume that all the keys exist, including
	// clusterName

	// Check the pull secret already exists in the clusterTemplate namespace.
	// The clusterRequest namespace is the same as the clusterTemplate namespace.
	pullSecret := &corev1.Secret{}
	pullSecretName := clusterInstance.Spec.PullSecretRef.Name
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, pullSecretName, t.object.Namespace, pullSecret)
	if err != nil {
		return fmt.Errorf(
			"failed to check if pull secret %s exists in namespace %s: %w",
			t.object.Spec.ClusterTemplateRef, t.object.Spec.ClusterTemplateRef, err,
		)
	}
	if !pullSecretExistsInTemplateNamespace {
		return utils.NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
			pullSecretName, t.object.Spec.ClusterTemplateRef)
	}

	newClusterInstancePullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pullSecretName,
			Namespace: clusterInstance.Name,
		},
		Data: pullSecret.Data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	if err := utils.CreateK8sCR(ctx, t.client, newClusterInstancePullSecret, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for ClusterInstancePullSecret: %w", err)
	}

	return nil
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

	err := utils.CreateK8sCR(ctx, t.client, namespace, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create or update namespace %s: %w", clusterName, err)
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return utils.NewInputError("the namespace %s is terminating", clusterName)
	}

	return nil
}

// createClusterInstanceBMCSecrets creates all the BMC secrets needed by the nodes included
// in the ClusterRequest.
func (t *clusterRequestReconcilerTask) createClusterInstanceBMCSecrets( // nolint: unused
	ctx context.Context, clusterName string) error {

	// The BMC credential details are for now obtained from the ClusterRequest.
	inputData, err := t.getClusterTemplateInputFromClusterRequest(&t.object.Spec.ClusterTemplateInput.ClusterInstanceInput)
	if err != nil {
		return fmt.Errorf("failed to unmarshal ClusterInstanceInput raw data: %w", err)
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
			// If a hwmgr plugin is being used, BMC details will not be in the cluster request
			t.logger.InfoContext(ctx, "BMC details not present in cluster request", "name", t.object.Name)
			continue
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

		if err = utils.CreateK8sCR(ctx, t.client, bmcSecret, nil, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create Kubernetes CR: %w", err)
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
		return fmt.Errorf("failed to check if secret exists in namespace %s: %w", targetNamespace, err)
	}
	if exists {
		t.logger.Info(
			"BMC secret already exists in the cluster namespace",
			slog.String("name", name),
			slog.String("name", targetNamespace),
		)
		return nil
	}

	if err := utils.CopyK8sSecret(ctx, t.client, name, sourceNamespace, targetNamespace); err != nil {
		return fmt.Errorf("failed to copy Kubernetes secret: %w", err)
	}

	return nil
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
	if err := utils.CreateK8sCR(ctx, t.client, namespace, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for namespace %s: %w", namespace, err)
	}

	return nil
}

func (t *clusterRequestReconcilerTask) hwMgrPluginNamespaceExists(
	ctx context.Context, name string) (bool, error) {

	t.logger.InfoContext(
		ctx,
		"Plugin: "+fmt.Sprintf("%v", name),
	)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, name, "", namespace)
	if err != nil {
		return false, fmt.Errorf("failed check if namespace exists %s: %w", name, err)
	}

	return exists, nil
}

func (t *clusterRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {
	// Create the hardware plugin namespace.
	pluginNameSpace := nodePool.ObjectMeta.Namespace
	if exists, err := t.hwMgrPluginNamespaceExists(ctx, pluginNameSpace); err != nil {
		return fmt.Errorf("failed check if hardware manager plugin namespace exists %s, err: %w", pluginNameSpace, err)
	} else if !exists && (pluginNameSpace == utils.TempDellPluginNamespace || pluginNameSpace == utils.UnitTestHwmgrNamespace) {
		// TODO: For test purposes only. Code to be removed once hwmgr plugin(s) are fully utilized
		createErr := t.createHwMgrPluginNamespace(ctx, pluginNameSpace)
		if createErr != nil {
			return fmt.Errorf(
				"failed to create hardware manager plugin namespace %s, err: %w", pluginNameSpace, createErr)
		}
	} else if !exists {
		return fmt.Errorf("specified hardware manager plugin namespace does not exist: %s", pluginNameSpace)
	}

	// Create/update the clusterInstance namespace, adding ClusterRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, nodePool.GetName())
	if err != nil {
		return err
	}

	// Create the node pool resource
	createErr := utils.CreateK8sCR(ctx, t.client, nodePool, t.object, "")
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create the NodePool %s in the namespace %s",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		return fmt.Errorf("failed to create/update the NodePool: %s", createErr.Error())
	}

	// Set NodePoolRef
	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &oranv1alpha1.NodePoolRef{}
	}
	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created NodePool %s in the namespace %s, if not already exist",
			nodePool.GetName(),
			nodePool.GetNamespace(),
		),
	)
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
		return nil, fmt.Errorf("failed to get ClusterTemplate: %w", err)
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
	clusterTemplateInputSchema *runtime.RawExtension, clusterTemplateInput map[string]any, dataType string) error {
	// Get the schema
	schemaMap := make(map[string]any)
	err := json.Unmarshal(clusterTemplateInputSchema.Raw, &schemaMap)
	if err != nil {
		return fmt.Errorf("error marshaling the ClusterTemplate schema data: %w", err)
	}
	if dataType == utils.ClusterInstanceDataType {
		utils.DisallowUnknownFieldsInSchema(schemaMap)
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	err = utils.ValidateJsonAgainstJsonSchema(
		schemaMap, clusterTemplateInput)
	if err != nil {
		return fmt.Errorf("the provided clusterTemplateInput for %s does not "+
			"match the schema from the ClusterTemplate (%s): %w", dataType, t.object.Spec.ClusterTemplateRef, err)
	}

	return nil
}

// createPoliciesConfigMap creates the cluster ConfigMap which will be used
// by the ACM policies.
func (t *clusterRequestReconcilerTask) createPoliciesConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	// Check the cluster version for the cluster-version label.
	clusterLabels := clusterInstance.Spec.ExtraLabels["ManagedCluster"]
	if err := utils.CheckClusterLabelsForPolicies(clusterInstance.Name, clusterLabels); err != nil {
		return fmt.Errorf("failed to check cluster labels: %w", err)
	}

	return t.createPolicyTemplateConfigMap(ctx, clusterInstance.Name)
}

// createPolicyTemplateConfigMap updates the keys of the default ConfigMap to match the
// clusterTemplate and the cluster version and creates/updates the ConfigMap for the
// required version of the policy template.
func (t *clusterRequestReconcilerTask) createPolicyTemplateConfigMap(
	ctx context.Context, clusterName string) error {

	// If there is no policy configuration data, log a message and return without an error.
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

	if err := utils.CreateK8sCR(ctx, t.client, policyTemplateConfigMap, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR: %w", err)
	}

	return nil
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
		return fmt.Errorf("failed to list node pools: %w", err)
	}
	for _, nodePool := range nodePoolList.Items {
		copiedNodePool := nodePool
		if err := r.Client.Delete(ctx, &copiedNodePool); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete node pool: %w", err)
		}
	}

	// If the ClusterInstance has been created by this ClusterRequest, delete it.
	// The SiteConfig operator will also delete the namespace.
	clusterInstanceList := &siteconfig.ClusterInstanceList{}
	if err := r.Client.List(ctx, clusterInstanceList, listOpts...); err != nil {
		return fmt.Errorf("failed to list cluster instances: %w", err)
	}
	for _, clusterInstance := range clusterInstanceList.Items {
		copiedClusterInstance := clusterInstance
		if err := r.Client.Delete(ctx, &copiedClusterInstance); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete cluster instance: %w", err)
		}
	}

	if len(clusterInstanceList.Items) == 0 {
		// If the ClusterInstance has not been created. Query the namespace created by
		// this ClusterRequest. Delete it if exists.
		namespaceList := &corev1.NamespaceList{}
		if err := r.Client.List(ctx, namespaceList, listOpts...); err != nil {
			return fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range namespaceList.Items {
			copiedNamespace := ns
			if err := r.Client.Delete(ctx, &copiedNamespace); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete namespace: %w", err)
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
			if err := r.Update(ctx, clusterRequest); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to update ClusterRequest with finalizer: %w", err)
			}
			return ctrl.Result{Requeue: true}, true, nil
		}
		return ctrl.Result{}, false, nil
	} else if controllerutil.ContainsFinalizer(clusterRequest, clusterRequestFinalizer) {
		// Run finalization logic for clusterRequestFinalizer. If the finalization logic
		// fails, don't remove the finalizer so that we can retry during the next reconciliation.
		if err := r.finalizeClusterRequest(ctx, clusterRequest); err != nil {
			return ctrl.Result{}, true, err
		}

		// Remove clusterRequestFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Removing ClusterRequest finalizer", "name", clusterRequest.Name)
		patch := client.MergeFrom(clusterRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(clusterRequest, clusterRequestFinalizer) {
			if err := r.Patch(ctx, clusterRequest, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to patch ClusterRequest: %w", err)
			}
			return ctrl.Result{}, true, nil
		}
	}
	return ctrl.Result{}, false, nil
}

// checkNodePoolProvisionStatus checks for the NodePool status to be in the provisioned state.
func (t *clusterRequestReconcilerTask) checkNodePoolProvisionStatus(ctx context.Context,
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
	provisionedCondition := meta.FindStatusCondition(nodePool.Status.Conditions, string(hwv1alpha1.Provisioned))
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

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodePool.
func (t *clusterRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) bool {

	hwNodes := t.collectNodeDetails(ctx, nodePool)
	if hwNodes == nil {
		return false
	}

	if !t.copyBMCSecrets(ctx, hwNodes, nodePool) {
		return false
	}

	if !t.applyNodeConfiguration(ctx, hwNodes, nodePool, clusterInstance) {
		return false
	}

	return true
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *clusterRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) bool {

	provisioned := t.checkNodePoolProvisionStatus(ctx, nodePool)
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

// collectNodeDetails collects BMC and node interfaces details
func (t *clusterRequestReconcilerTask) collectNodeDetails(ctx context.Context,
	nodePool *hwv1alpha1.NodePool) map[string][]nodeInfo {

	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]nodeInfo)

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
			return nil
		}
		if !exists {
			t.logger.ErrorContext(
				ctx,
				"Node object does not exist",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.Bool("exists", exists),
			)
			return nil
		}
		// Verify the node object is generated from the expected pool
		if node.Spec.NodePool != nodePool.GetName() {
			t.logger.ErrorContext(
				ctx,
				"Node object is not from the expected NodePool",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil
		}

		if node.Status.BMC == nil {
			t.logger.ErrorContext(
				ctx,
				"Node status does not have BMC details",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil
		}
		// Store the nodeInfo per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], nodeInfo{
			bmcAddress:     node.Status.BMC.Address,
			bmcCredentials: node.Status.BMC.CredentialsName,
			nodeName:       node.Name,
			interfaces:     node.Status.Interfaces,
		})
	}

	return hwNodes
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

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *clusterRequestReconcilerTask) applyNodeConfiguration(ctx context.Context, hwNodes map[string][]nodeInfo,
	nodePool *hwv1alpha1.NodePool, clusterInstance *siteconfig.ClusterInstance) bool {

	for i, node := range clusterInstance.Spec.Nodes {
		// Check if the node's role matches any key in hwNodes
		nodeInfos, exists := hwNodes[node.Role]
		if !exists || len(nodeInfos) == 0 {
			continue
		}

		clusterInstance.Spec.Nodes[i].BmcAddress = nodeInfos[0].bmcAddress
		clusterInstance.Spec.Nodes[i].BmcCredentialsName = siteconfig.BmcCredentialsName{Name: nodeInfos[0].bmcCredentials}
		// Get the boot MAC address based on the interface label
		bootMAC, err := utils.GetBootMacAddress(nodeInfos[0].interfaces, nodePool)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Fail to get the node boot MAC address",
				slog.String("name", node.HostName),
				slog.String("error", err.Error()),
			)
			return false
		}
		clusterInstance.Spec.Nodes[i].BootMACAddress = bootMAC

		// Populate the MAC address for each interface
		for j, iface := range clusterInstance.Spec.Nodes[i].NodeNetwork.Interfaces {
			for _, nodeIface := range nodeInfos[0].interfaces {
				if nodeIface.Name == iface.Name {
					clusterInstance.Spec.Nodes[i].NodeNetwork.Interfaces[j].MacAddress = nodeIface.MACAddress
				}
			}
		}

		// Indicates which host has been assigned to the node
		if !t.updateNodeStatusWithHostname(ctx, nodeInfos[0].nodeName, node.HostName,
			nodePool.Namespace) {
			return false
		}
		hwNodes[node.Role] = nodeInfos[1:]
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

	if len(nodePool.Status.Conditions) > 0 {
		provisionedCondition := meta.FindStatusCondition(
			nodePool.Status.Conditions, string(hwv1alpha1.Provisioned))
		if provisionedCondition != nil {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.CRconditionTypes.HardwareProvisioned,
				utils.ConditionReason(provisionedCondition.Reason),
				provisionedCondition.Status,
				provisionedCondition.Message)
		} else {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.CRconditionTypes.HardwareProvisioned,
				utils.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				"Unknown state of hardware provisioning",
			)
		}

		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to update the HardwareProvisioning status for ClusterRequest",
				slog.String("name", t.object.Name),
				slog.Any("specificError", err),
			)
			return fmt.Errorf("failed to update HardwareProvisioning status: %w", err)
		}
	} else if nodePool.ObjectMeta.Namespace == utils.TempDellPluginNamespace || nodePool.ObjectMeta.Namespace == utils.UnitTestHwmgrNamespace {
		// TODO: For test purposes only. Code to be removed once hwmgr plugin(s) are fully utilized
		meta.SetStatusCondition(
			&nodePool.Status.Conditions,
			metav1.Condition{
				Type:   string(hwv1alpha1.Unknown),
				Status: metav1.ConditionUnknown,
				Reason: string(hwv1alpha1.NotInitialized),
			},
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, nodePool); err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to update the NodePool status",
				slog.String("name", nodePool.Name),
				slog.Any("specificError", err),
			)
			return fmt.Errorf("failed to update NodePool status: %w", err)
		}

	}
	return nil
}

// findClusterInstanceForClusterRequest maps the ClusterInstance created by a
// ClusterRequest to a reconciliation request.
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
// ClusterRequest to a reconciliation request.
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

	// For this case, we can use either new or old object.
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

	// For this case, we can use either new or old object.
	newManagedCluster := event.ObjectNew.(*clusterv1.ManagedCluster)

	// Get the ClusterInstance
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newManagedCluster.Name,
		Name:      newManagedCluster.Name,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as this ManagedCluster is not deployed/managed by ClusterInstance
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

// findPoliciesForClusterRequests creates reconciliation requests for the ClusterRequests
// whose associated ManagedClusters have matched policies Updated or Deleted.
func findPoliciesForClusterRequests[T deleteOrUpdateEvent](
	ctx context.Context, c client.Client, e T, q workqueue.RateLimitingInterface) {

	policy := &policiesv1.Policy{}
	switch evt := any(e).(type) {
	case event.UpdateEvent:
		policy = evt.ObjectOld.(*policiesv1.Policy)
	case event.DeleteEvent:
		policy = evt.Object.(*policiesv1.Policy)
	default:
		// Only Update and Delete events are supported
		return
	}

	// Get the ClusterInstance.
	clusterInstance := &siteconfig.ClusterInstance{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: policy.Namespace,
		Name:      policy.Namespace,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as the ManagedCluster for this Namespace is not deployed/managed by ClusterInstance.
			return
		}
		return
	}

	clusterRequest, okCR := clusterInstance.GetLabels()[clusterRequestNameLabel]
	clusterRequestNs, okCRNs := clusterInstance.GetLabels()[clusterRequestNamespaceLabel]
	if okCR && okCRNs {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      clusterRequest,
			Namespace: clusterRequestNs,
		}})
	}
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ClusterRequestReconciler) handlePolicyEventDelete(
	ctx context.Context, e event.DeleteEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function for determining the corresponding ClusterRequest.
	findPoliciesForClusterRequests(ctx, r.Client, e, q)
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ClusterRequestReconciler) handlePolicyEventUpdate(
	ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function.
	findPoliciesForClusterRequests(ctx, r.Client, e, q)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-request").
		For(
			&oranv1alpha1.ClusterRequest{},
			// Watch for create and update event for ClusterRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&oranv1alpha1.ClusterTemplate{},
			handler.Funcs{UpdateFunc: r.findClusterTemplateForClusterRequest},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
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
			&policiesv1.Policy{},
			handler.Funcs{
				UpdateFunc: r.handlePolicyEventUpdate,
				DeleteFunc: r.handlePolicyEventDelete,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := e.ObjectNew.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}

					policyNew := e.ObjectNew.(*policiesv1.Policy)
					policyOld := e.ObjectOld.(*policiesv1.Policy)

					// Process status.status and remediation action changes.
					return policyOld.Status.ComplianceState != policyNew.Status.ComplianceState ||
						(policyOld.Spec.RemediationAction != policyNew.Spec.RemediationAction)
				},
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc: func(de event.DeleteEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := de.Object.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}
					return true
				},
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
