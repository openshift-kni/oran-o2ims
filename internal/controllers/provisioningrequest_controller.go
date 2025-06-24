/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginclient "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/client"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ProvisioningRequestReconciler reconciles a ProvisioningRequest object
type ProvisioningRequestReconciler struct {
	client.Client
	Logger *slog.Logger
}

type provisioningRequestReconcilerTask struct {
	logger         *slog.Logger
	client         client.Client
	hwpluginClient *hwmgrpluginclient.HardwarePluginClient
	object         *provisioningv1alpha1.ProvisioningRequest
	clusterInput   *clusterInput
	ctDetails      *clusterTemplateDetails
	timeouts       *timeouts
}

// clusterInput holds the merged input data for a cluster
type clusterInput struct {
	clusterInstanceData map[string]any
	policyTemplateData  map[string]any
}

// clusterTemplateDetails holds the details for the referenced ClusterTemplate
type clusterTemplateDetails struct {
	namespace string
	templates provisioningv1alpha1.Templates
}

// timeouts holds the timeout values, in minutes,
// for hardware provisioning, cluster provisioning
// and cluster configuration.
type timeouts struct {
	hardwareProvisioning time.Duration
	clusterProvisioning  time.Duration
	clusterConfiguration time.Duration
}

func GetClusterTemplateRefName(name, version string) string {
	return fmt.Sprintf("%s.%s", name, version)
}

//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwaretemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwaretemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodeallocationrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodeallocationrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=list;watch
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/status,verbs=get
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:urls="/hardware-manager/provisioning/*",verbs=get;list;create;post;put;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ProvisioningRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ProvisioningRequestReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = doNotRequeue()

	// Reconciliation loop can be triggered multiple times for the same resource
	// due to changes in related resources, events or conditions.
	// Wait a bit so that API server/etcd syncs up and this reconcile has a
	// better chance of getting the latest resources.
	time.Sleep(100 * time.Millisecond)

	// Fetch the object:
	object := &provisioningv1alpha1.ProvisioningRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The provisioning request could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ProvisioningRequest",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile ProvisioningRequest]",
		"name", object.Name, "namespace", object.Namespace)

	if res, stop, err := r.handleFinalizer(ctx, object); !res.IsZero() || stop || err != nil {
		if err != nil {
			r.Logger.ErrorContext(
				ctx,
				"Encountered error while handling the ProvisioningRequest finalizer",
				slog.String("err", err.Error()))
		}
		return res, err
	}

	// Create and run the task:
	task := &provisioningRequestReconcilerTask{
		logger:       r.Logger,
		client:       r.Client,
		object:       object,
		clusterInput: &clusterInput{},
		ctDetails:    &clusterTemplateDetails{},
		timeouts:     &timeouts{},
	}
	result, err = task.run(ctx)
	return
}

func (t *provisioningRequestReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	if t.shouldStopReconciliation() {
		return doNotRequeue(), nil
	}

	// Handle validation, rendering and creation of required resources
	renderedClusterInstance, res, err := t.handlePreProvisioning(ctx)
	if renderedClusterInstance == nil {
		return res, err
	}

	// TODO: the handlePreProvisioning function should be updated to return an unstructured ClusterInstance
	unstructuredClusterInstance, err := utils.ConvertToUnstructured(*renderedClusterInstance)
	if err != nil {
		return requeueWithError(err)
	}

	// Handle hardware template and NodeAllocationRequest provisioning/configuring
	if !t.isHardwareProvisionSkipped() {

		// Get hwplugin client for the HardwarePlugin
		hwclient, err := getHardwarePluginClient(ctx, t.client, t.logger, t.object)
		if err != nil {
			return requeueWithError(err)
		}
		t.hwpluginClient = hwclient

		res, proceed, err := t.handleNodeAllocationRequestProvisioning(ctx, unstructuredClusterInstance)
		if err != nil || (res == doNotRequeue() && !proceed) || res.RequeueAfter > 0 {
			return res, err
		}
	}

	// Handle the cluster install with ClusterInstance
	err = t.handleClusterInstallation(ctx, unstructuredClusterInstance)
	if err != nil {
		return requeueWithError(err)
	}
	if !utils.IsClusterProvisionPresent(t.object) ||
		utils.IsClusterProvisionTimedOutOrFailed(t.object) {
		// If the cluster installation has not started due to
		// processing issue, failed or timed out, do not requeue.
		return doNotRequeue(), nil
	}

	// Handle policy configuration
	requeueForConfig, err := t.handleClusterPolicyConfiguration(ctx)
	if err != nil {
		return requeueWithError(err)
	}

	if utils.IsClusterZtpDone(t.object) {
		// If the initial provisioning is completed, check if an upgrade is requested
		shouldUpgrade, err := t.IsUpgradeRequested(ctx, renderedClusterInstance.GetName())
		if err != nil {
			return requeueWithError(err)
		}

		// An upgrade is requested or upgrade has started but not completed
		if shouldUpgrade ||
			(utils.IsClusterUpgradeInitiated(t.object) &&
				!utils.IsClusterUpgradeCompleted(t.object)) {
			upgradeCtrlResult, proceed, err := t.handleUpgrade(ctx, renderedClusterInstance.GetName())
			if upgradeCtrlResult.RequeueAfter > 0 || !proceed || err != nil {
				// Requeue if the upgrade is in progress or an error occurs.
				// Stop reconciliation if the upgrade has failed.
				// Proceed if the upgrade is completed.
				return upgradeCtrlResult, err
			}
		}
	}

	// Requeue if cluster provisioning is not completed (in-progress or unknown)
	// or there are enforce policies that are not Compliant but the configuration
	// has not timed out.
	if !utils.IsClusterProvisionCompleted(t.object) || requeueForConfig {
		return requeueWithLongInterval(), nil
	}

	err = t.finalizeProvisioningIfComplete(ctx)
	if err != nil {
		return requeueWithError(err)
	}

	return doNotRequeue(), nil
}

// shouldStopReconciliation checks if the reconciliation should stop.
func (t *provisioningRequestReconcilerTask) shouldStopReconciliation() bool {
	if t.object.Status.ObservedGeneration == t.object.Generation &&
		t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFailed &&
		utils.HasFatalProvisioningFailure(t.object.Status.Conditions) {
		// If the provisioning has failed with a fatal error and no spec changes,
		// stop reconciliation.
		return true
	}

	return false
}

// handlePreProvisioning handles the validation, rendering and creation of required resources.
// It returns a rendered ClusterInstance for successful pre-provisioning, a ctrl.Result to
// indicate if/when to requeue, and an error if any issues occur.
func (t *provisioningRequestReconcilerTask) handlePreProvisioning(ctx context.Context) (*siteconfig.ClusterInstance, ctrl.Result, error) {
	// Set the provisioning state to pending if spec changes are observed
	if t.object.Status.ObservedGeneration != t.object.Generation {
		utils.SetProvisioningStatePending(t.object, "Validating and preparing resources")
		if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			return nil, doNotRequeue(), fmt.Errorf(
				"failed to update status for ProvisioningRequest %s: %w",
				t.object.Name, updateErr,
			)
		}
	}

	// Validate the ProvisioningRequest
	err := t.handleValidation(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return nil, res, err
		}
		// internal error that might recover
		return nil, doNotRequeue(), err
	}

	// Render and validate ClusterInstance
	renderedClusterInstance, err := t.handleRenderClusterInstance(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return nil, res, err
		}
		return nil, doNotRequeue(), err
	}

	// Handle the creation of resources required for cluster deployment
	err = t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			_, err = t.checkClusterDeployConfigState(ctx)
			if err != nil {
				return nil, doNotRequeue(), err
			}
			// Requeue since we are not watching for updates to required resources
			// if they are missing
			return nil, requeueWithMediumInterval(), nil
		}
		return nil, doNotRequeue(), err
	}

	return renderedClusterInstance, doNotRequeue(), nil
}

// handleNodeAllocationRequestProvisioning handles the rendering, creation, and provisioning of the NodeAllocationRequest.
// It first renders the hardware template for the NodeAllocationRequest based on the provided ClusterInstance,
// then creates or updates the NodeAllocationRequest resource, and finally waits for the NodeAllocationRequest to be provisioned.
// The function returns a ctrl.Result to indicate if/when to requeue, the rendered NodeAllocationRequest, a bool
// to indicate whether to process with further processing and an error if any issues occur.
func (t *provisioningRequestReconcilerTask) handleNodeAllocationRequestProvisioning(ctx context.Context,
	renderedClusterInstance *unstructured.Unstructured) (ctrl.Result, bool, error) {

	// Render the hardware template for NodeAllocationRequest
	renderedNodeAllocationRequest, err := t.renderHardwareTemplate(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return res, false, err
		}
		return doNotRequeue(), false, err
	}

	// Create/Update the NodeAllocationRequest
	if err := t.createOrUpdateNodeAllocationRequest(ctx, renderedClusterInstance.GetNamespace(), renderedNodeAllocationRequest); err != nil {
		return doNotRequeue(), false, err
	}

	nodeAllocationRequestID := t.getNodeAllocationRequestID()
	if nodeAllocationRequestID == "" {
		return doNotRequeue(), false, fmt.Errorf("missing nodeAllocationRequest identifier")
	}

	nodeAllocationRequestResponse, exists, err := t.getNodeAllocationRequestResponse(ctx)
	if err != nil {
		return doNotRequeue(), false, err
	}
	if !exists {
		return requeueWithMediumInterval(), false, nil
	}

	// Wait for the NodeAllocationRequest to be provisioned and update BMC details if necessary
	provisioned, configured, timedOutOrFailed, err := t.waitForHardwareData(ctx, renderedClusterInstance, nodeAllocationRequestResponse)
	if err != nil {
		return doNotRequeue(), false, err
	}
	if timedOutOrFailed {
		return doNotRequeue(), false, nil
	}
	if !provisioned {
		t.logger.InfoContext(ctx, fmt.Sprintf("Waiting for NodeAllocationRequest %s to be provisioned", nodeAllocationRequestID))
		return requeueWithMediumInterval(), false, nil
	}

	// If the NodeAllocationRequest was updated but the configuration hasn’t been set yet,
	// or if the configuration is not yet complete, requeue and wait for completion.
	// If configuration is not set and no configuration update is requested, do nothing.
	configuringStarted := t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart
	if (configured == nil && !configuringStarted.IsZero()) || (configured != nil && !*configured) {
		t.logger.InfoContext(ctx, fmt.Sprintf("Waiting for NodeAllocationRequest %s to be configured", nodeAllocationRequestID))
		return requeueWithMediumInterval(), false, nil
	}

	// Provisioning completed successfully; proceed with further processing
	return doNotRequeue(), true, nil
}

// checkClusterDeployConfigState checks the current deployment and configuration state of
// the cluster by evaluating the statuses of related resources like NodeAllocationRequest, ClusterInstance
// and policy configuration when applicable, and update the corresponding ProvisioningRequest
// status conditions
func (t *provisioningRequestReconcilerTask) checkClusterDeployConfigState(ctx context.Context) (result ctrl.Result, err error) {
	if !t.isHardwareProvisionSkipped() {
		// Check the NodeAllocationRequest status if exists
		nodeAllocationRequestResponse, exists, err := t.getNodeAllocationRequestResponse(ctx)
		if err != nil || !exists {
			return requeueWithError(err)
		}

		hwProvisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwv1alpha1.Provisioned)
		if err != nil {
			return requeueWithError(err)
		}
		if timedOutOrFailed {
			// Timeout occurred or failed, stop requeuing
			return doNotRequeue(), nil
		}
		if !hwProvisioned {
			return requeueWithMediumInterval(), nil
		}
	}

	// Check the ClusterInstance status if exists
	if t.object.Status.Extensions.ClusterDetails == nil {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
		return doNotRequeue(), nil
	}
	err = t.checkClusterProvisionStatus(
		ctx, t.object.Status.Extensions.ClusterDetails.Name)
	if err != nil {
		return requeueWithError(err)
	}
	if !utils.IsClusterProvisionPresent(t.object) ||
		utils.IsClusterProvisionTimedOutOrFailed(t.object) {
		// If the cluster installation has not started due to
		// processing issue, failed or timed out, do not requeue.
		return doNotRequeue(), nil
	}

	// Check the policy configuration status
	requeueForConfig, err := t.handleClusterPolicyConfiguration(ctx)
	if err != nil {
		return requeueWithError(err)
	}
	// Requeue if Cluster Provisioned is not completed (in-progress or unknown)
	// or there are enforce policies that are not Compliant and configuration
	// has not timed out
	if !utils.IsClusterProvisionCompleted(t.object) || requeueForConfig {
		return requeueWithLongInterval(), nil
	}

	err = t.finalizeProvisioningIfComplete(ctx)
	if err != nil {
		return requeueWithError(err)
	}

	// If the existing provisioning has been fulfilled, check if there are any issues
	// with the validation, rendering, or creation of resources due to updates to the
	// ProvisioningRequest. If there are issues, transition the provisioningPhase to failed.
	if utils.IsProvisioningStateFulfilled(t.object) {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
	}
	return doNotRequeue(), nil
}

// checkResourcePreparationStatus checks for validation and preparation failures, setting the
// provisioningPhase to failed if issues are found.
func (t *provisioningRequestReconcilerTask) checkResourcePreparationStatus(ctx context.Context) error {
	conditionTypes := []provisioningv1alpha1.ConditionType{
		provisioningv1alpha1.PRconditionTypes.Validated,
		provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered,
		provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated,
		provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered,
	}

	for _, condType := range conditionTypes {
		cond := meta.FindStatusCondition(t.object.Status.Conditions, string(condType))
		if cond != nil && cond.Status == metav1.ConditionFalse {
			// Set the provisioning state to failed if any condition is false
			utils.SetProvisioningStateFailed(t.object, cond.Message)
			break
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) handleValidation(ctx context.Context) error {
	// Validate provisioning request CR
	err := t.validateProvisioningRequestCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate the ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.Validated,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to validate the ProvisioningRequest: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Validated the ProvisioningRequest CR",
			slog.String("name", t.object.Name),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.Validated,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The provisioning request validation succeeded",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

// handleRenderClusterInstance handles the ClusterInstance rendering and validation.
func (t *provisioningRequestReconcilerTask) handleRenderClusterInstance(ctx context.Context) (*siteconfig.ClusterInstance, error) {
	renderedClusterInstance, err := t.buildClusterInstance(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render and validate the ClusterInstance for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered,
			provisioningv1alpha1.CRconditionReasons.Failed,
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
			provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"ClusterInstance rendered and passed dry-run validation",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to handle ClusterInstance rendering and validation: %w", err)
	}
	return renderedClusterInstance, nil
}

func (t *provisioningRequestReconcilerTask) handleClusterResources(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	err := t.createOrUpdateClusterResources(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to apply the required cluster resource for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to apply the required cluster resource: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Applied the required cluster resources for ProvisioningRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Cluster resources applied",
		)
	}
	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

func (t *provisioningRequestReconcilerTask) renderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwmgrpluginapi.NodeAllocationRequest, error) {
	renderedNodeAllocationRequest, err := t.handleRenderHardwareTemplate(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the Hardware template for NodeAllocationRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render the Hardware template: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered Hardware template for NodeAllocationRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered Hardware template successfully",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return renderedNodeAllocationRequest, err
}

func (r *ProvisioningRequestReconciler) handleFinalizer(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) (ctrl.Result, bool, error) {

	// Check if the ProvisioningRequest is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if provisioningRequest.DeletionTimestamp.IsZero() {
		// Check and add finalizer for this CR.
		if !controllerutil.ContainsFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer) {
			controllerutil.AddFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer)
			if err := r.Update(ctx, provisioningRequest); err != nil {
				return doNotRequeue(), true, fmt.Errorf("failed to update ProvisioningRequest with finalizer: %w", err)
			}
			// Requeue since the finalizer has been added.
			return requeueImmediately(), false, nil
		}
	} else if controllerutil.ContainsFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer) {
		r.Logger.Info(fmt.Sprintf("ProvisioningRequest (%s) is being deleted", provisioningRequest.Name))
		deleteComplete, err := r.handleProvisioningRequestDeletion(ctx, provisioningRequest)
		if !deleteComplete {
			// No need to requeue here, deletion of dependents(including their finalizer removal) will
			// automatically trigger reconciliation.
			return doNotRequeue(), true, err
		}

		// Deletion has completed. Remove provisioningRequestFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Dependents have been deleted. Removing provisioningRequest finalizer", "name", provisioningRequest.Name)
		patch := client.MergeFrom(provisioningRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer) {
			if err := r.Patch(ctx, provisioningRequest, patch); err != nil {
				return doNotRequeue(), true, fmt.Errorf("failed to patch ProvisioningRequest: %w", err)
			}
			return doNotRequeue(), true, nil
		}
	}

	return doNotRequeue(), false, nil
}

// handleProvisioningRequestDeletion ensures that specific dependents with potential long-running finalizers
// are deleted before the ProvisioningRequest itself is finalized. It returns true if all dependents have been
// deleted; otherwise, it returns false.
func (r *ProvisioningRequestReconciler) handleProvisioningRequestDeletion(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) (bool, error) {
	// Set the provisioningState to deleting
	if provisioningRequest.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateDeleting {
		utils.SetProvisioningStateDeleting(provisioningRequest)
		if err := utils.UpdateK8sCRStatus(ctx, r.Client, provisioningRequest); err != nil {
			return false, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", provisioningRequest.Name, err)
		}
	}

	deleteCompleted := true

	if provisioningRequest.Status.Extensions.NodeAllocationRequestRef != nil {
		// Get hwplugin client for the HardwarePlugin
		hwpluginClient, err := getHardwarePluginClient(ctx, r.Client, r.Logger, provisioningRequest)
		if err != nil {
			return false, fmt.Errorf("failed to get HardwarePlugin client: %w", err)
		}

		nodeAllocationRequestID := provisioningRequest.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
		if nodeAllocationRequestID != "" {
			r.Logger.Info(fmt.Sprintf("Deleting NodeAllocationRequest (%s)", nodeAllocationRequestID))
			resp, narExists, err := hwpluginClient.DeleteNodeAllocationRequest(ctx, nodeAllocationRequestID)
			if err != nil {
				return false, fmt.Errorf("failed to delete NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
			}

			if resp == nodeAllocationRequestID {
				r.Logger.Info(fmt.Sprintf("Deletion request for nodeAllocationRequest '%s' is successful", nodeAllocationRequestID))
			}

			if narExists {
				r.Logger.Info(fmt.Sprintf("Waiting for NodeAllocationRequest (%s) to be deleted", nodeAllocationRequestID))
				deleteCompleted = false
			} else {
				deleteCompleted = true
			}
		}
	}

	// List resources by label
	var labels = map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: provisioningRequest.Name,
	}
	listOpts := []client.ListOption{
		client.MatchingLabels(labels),
	}

	namespaceList := &corev1.NamespaceList{}
	if err := r.Client.List(ctx, namespaceList, listOpts...); err != nil {
		return false, fmt.Errorf("failed to list namespaces: %w", err)
	}
	for _, ns := range namespaceList.Items {
		// Delete cluster namespace if not already.
		// Deleting cluster namespace will delete all resources within it, including
		// ClusterInstance and ImageBasedGroupUpgrade.
		if ns.DeletionTimestamp.IsZero() {
			r.Logger.Info(fmt.Sprintf("Deleting Cluster Namespace (%s)", ns.Name))
			copiedNamespace := ns
			if err := r.Client.Delete(ctx, &copiedNamespace); client.IgnoreNotFound(err) != nil {
				return false, fmt.Errorf("failed to delete namespace: %w", err)
			}
		}
		r.Logger.Info(fmt.Sprintf("Waiting for Cluster Namespace (%s) to be deleted", ns.Name))
		deleteCompleted = false
	}
	return deleteCompleted, nil
}

func (t *provisioningRequestReconcilerTask) isHardwareProvisionSkipped() bool {
	return t.ctDetails.templates.HwTemplate == ""
}

// finalizeProvisioningIfComplete checks if the provisioning/upgrade process is completed.
// If so, it sets the provisioning state to "fulfilled" and updates the provisioned
// resources in the status.
func (t *provisioningRequestReconcilerTask) finalizeProvisioningIfComplete(ctx context.Context) error {
	if utils.IsClusterProvisionCompleted(t.object) && utils.IsClusterConfigCompleted(t.object) &&
		(!utils.IsClusterUpgradeInitiated(t.object) || utils.IsClusterUpgradeCompleted(t.object)) {

		utils.SetProvisioningStateFulfilled(t.object)
		mcl, err := t.updateOCloudNodeClusterId(ctx)
		if err != nil {
			return err
		}

		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
		}

		// Make sure extra labels are added to the needed CRs.
		err = t.addPostProvisioningLabels(ctx, mcl)
		if err != nil {
			return err
		}
	}

	return nil
}

// getHardwarePluginClient is a convenience wrapper function to get the HardwarePluginClient object
func getHardwarePluginClient(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pr *provisioningv1alpha1.ProvisioningRequest,
) (*hwmgrpluginclient.HardwarePluginClient, error) {
	// Get the HardwarePlugin CR
	hwplugin, err := utils.GetHardwarePluginFromProvisioningRequest(ctx, c, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve HardwarePlugin: %w", err)
	}

	// Validate that the HardwarePlugin CR is registered successfully
	validated := meta.FindStatusCondition(hwplugin.Status.Conditions, string(hwv1alpha1.ConditionTypes.Registration))
	if validated == nil || validated.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("hardwarePlugin '%s' is not registered", hwplugin.Name)
	}

	// Get hwplugin client for the HardwarePlugin
	// nolint: wrapcheck
	return hwmgrpluginclient.NewHardwarePluginClient(ctx, c, logger, hwplugin)
}

// getNodeAllocationRequestID returns the NodeAllocationRequest identifier associated with the ProvisioningRequest CR.
// If no identifier is set, a null string is returned.
func (t *provisioningRequestReconcilerTask) getNodeAllocationRequestID() string {
	if t.object.Status.Extensions.NodeAllocationRequestRef != nil {
		return t.object.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
	}
	return ""
}

// getNodeAllocationRequestResponse retrieves the NodeAllocationRequest from the HardwarePlugin server.
// It returns the NodeAllocationRequestResponse, boolean value indicating whether the NodeAllocationRequest object was found (exists),
// and any error encountered while attempting to fetch the NodeAllocationRequest.
func (t *provisioningRequestReconcilerTask) getNodeAllocationRequestResponse(ctx context.Context) (*hwmgrpluginapi.NodeAllocationRequestResponse, bool, error) {
	nodeAllocationRequestID := t.getNodeAllocationRequestID()
	if nodeAllocationRequestID == "" {
		return nil, false, fmt.Errorf("missing status.nodeAllocationRequestRef.NodeAllocationRequestID")
	}
	var (
		nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse
		exists                        bool
		err                           error
	)
	// Get the generated NodeAllocationRequest and its status.
	if err = utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		nodeAllocationRequestResponse, exists, err = t.hwpluginClient.GetNodeAllocationRequest(ctx, nodeAllocationRequestID)
		if err != nil {
			return fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
		}
		if !exists {
			return fmt.Errorf("nodeAllocationRequest '%s' does not exist", nodeAllocationRequestID)
		}
		return nil
	}); err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, false, nil
		}
		// nolint: wrapcheck
		return nil, false, err
	}

	return nodeAllocationRequestResponse, true, nil
}
