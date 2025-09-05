/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ProvisioningRequestReconciler reconciles a ProvisioningRequest object
type ProvisioningRequestReconciler struct {
	client.Client
	Logger         *slog.Logger
	CallbackConfig *ctlrutils.NarCallbackConfig
}

type provisioningRequestReconcilerTask struct {
	logger         *slog.Logger
	client         client.Client
	hwpluginClient hwmgrpluginapi.HardwarePluginClientInterface
	object         *provisioningv1alpha1.ProvisioningRequest
	clusterInput   *clusterInput
	ctDetails      *clusterTemplateDetails
	timeouts       *timeouts
	callbackConfig *ctlrutils.NarCallbackConfig
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

// Hardware plugin client retry configuration constants
const (
	maxHardwareClientRetries = 3
	baseRetryDelay           = 5 * time.Second
	timedOutMessage          = "timed out"
)

func GetClusterTemplateRefName(name, version string) string {
	return fmt.Sprintf("%s.%s", name, version)
}

//+kubebuilder:rbac:groups=clcm.openshift.io,resources=provisioningrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=provisioningrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=provisioningrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwaretemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwaretemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=nodeallocationrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=nodeallocationrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=list;watch
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/status,verbs=get
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:urls="/hardware-manager/provisioning/*",verbs=get;list;create;update;delete

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
	startTime := time.Now()
	result = doNotRequeue()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ProvisioningRequest")

	defer func() {
		duration := time.Since(startTime)
		if err != nil {
			r.Logger.ErrorContext(ctx, "Reconciliation failed",
				slog.Duration("duration", duration),
				slog.String("error", err.Error()))
		} else {
			r.Logger.InfoContext(ctx, "Reconciliation completed",
				slog.Duration("duration", duration),
				slog.Bool("requeue", result.Requeue),
				slog.Duration("requeueAfter", result.RequeueAfter))
		}
	}()

	// Reconciliation loop can be triggered multiple times for the same resource
	// due to changes in related resources, events or conditions.
	// Wait a bit so that API server/etcd syncs up and this reconcile has a
	// better chance of getting the latest resources.
	time.Sleep(100 * time.Millisecond)

	// Fetch the object:
	object := &provisioningv1alpha1.ProvisioningRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if k8serrors.IsNotFound(err) {
			// The provisioning request could have been deleted
			r.Logger.InfoContext(ctx, "ProvisioningRequest not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch ProvisioningRequest", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, object)
	r.Logger.InfoContext(ctx, "Fetched ProvisioningRequest successfully")

	if res, stop, err := r.handleFinalizer(ctx, object); !res.IsZero() || stop || err != nil {
		if err != nil {
			ctlrutils.LogError(ctx, r.Logger, "Encountered error while handling the ProvisioningRequest finalizer", err)
		}
		return res, err
	}

	// Create and run the task:
	task := &provisioningRequestReconcilerTask{
		logger:         r.Logger,
		client:         r.Client,
		object:         object,
		clusterInput:   &clusterInput{},
		ctDetails:      &clusterTemplateDetails{},
		timeouts:       &timeouts{},
		callbackConfig: r.CallbackConfig,
	}
	result, err = task.run(ctx)
	return
}

func (t *provisioningRequestReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	if t.shouldStopReconciliation() {
		t.logger.InfoContext(ctx, "Stopping reconciliation due to fatal failure")
		return doNotRequeue(), nil
	}

	// Execute the main reconciliation phases
	renderedClusterInstance, result, err := t.executeProvisioningPhases(ctx)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		// Check for overall provisioning timeout before returning error/requeue
		if timeoutResult := t.checkOverallProvisioningTimeout(ctx); timeoutResult.RequeueAfter > 0 {
			return timeoutResult, nil
		}
		return result, err
	}

	// Handle post-provisioning logic
	return t.handlePostProvisioning(ctx, renderedClusterInstance)
}

// executeProvisioningPhases handles the main provisioning phases and returns the cluster instance if successful
func (t *provisioningRequestReconcilerTask) executeProvisioningPhases(ctx context.Context) (
	*unstructured.Unstructured, ctrl.Result, error) {

	// Phase 1: Pre-provisioning
	renderedClusterInstance, unstructuredClusterInstance, result, err := t.executePreProvisioningPhase(ctx)
	if err != nil || renderedClusterInstance == nil {
		return nil, result, err
	}

	// Phase 2: Hardware provisioning
	result, err = t.executeHardwareProvisioningPhase(ctx, unstructuredClusterInstance)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return nil, result, err
	}

	// Phase 3: Cluster resources
	result, err = t.executeClusterResourcesPhase(ctx, renderedClusterInstance)
	if err != nil {
		return nil, result, err
	}

	// Phase 4: Cluster installation
	result, err = t.executeClusterInstallationPhase(ctx, unstructuredClusterInstance)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return nil, result, err
	}

	return unstructuredClusterInstance, ctrl.Result{}, nil
}

// executePreProvisioningPhase handles pre-provisioning validation and setup
func (t *provisioningRequestReconcilerTask) executePreProvisioningPhase(ctx context.Context) (
	*siteconfig.ClusterInstance, *unstructured.Unstructured, ctrl.Result, error) {

	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "pre_provisioning")
	phaseStartTime := time.Now()

	// Initialize hardware plugin client if needed
	if err := t.initializeHardwarePluginIfNeeded(ctx); err != nil {
		result, _ := requeueWithError(err)
		return nil, nil, result, err
	}

	// Handle validation, rendering and creation of required resources
	renderedClusterInstance, res, err := t.handlePreProvisioning(ctx)
	if renderedClusterInstance == nil {
		if err != nil {
			ctlrutils.LogError(ctx, t.logger, "Pre-provisioning phase failed", err)
		}
		return nil, nil, res, err
	}

	ctlrutils.LogPhaseComplete(ctx, t.logger, "pre_provisioning", time.Since(phaseStartTime))

	// Convert to unstructured for hardware phase
	unstructuredClusterInstance, err := ctlrutils.ConvertToUnstructured(*renderedClusterInstance)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to convert ClusterInstance to unstructured: %w", err)
		result, _ := requeueWithError(wrappedErr)
		return nil, nil, result, wrappedErr
	}

	return renderedClusterInstance, unstructuredClusterInstance, ctrl.Result{}, nil
}

// executeHardwareProvisioningPhase handles hardware provisioning
func (t *provisioningRequestReconcilerTask) executeHardwareProvisioningPhase(ctx context.Context,
	unstructuredClusterInstance *unstructured.Unstructured) (ctrl.Result, error) {

	if t.isHardwareProvisionSkipped() {
		t.logger.InfoContext(ctx, "Hardware provisioning skipped")
		return ctrl.Result{}, nil
	}

	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "hardware_provisioning")
	phaseStartTime := time.Now()

	if t.hwpluginClient == nil {
		result, _ := requeueWithError(errors.New("hwpluginClient is not initialized"))
		return result, errors.New("hwpluginClient is not initialized")
	}

	res, proceed, err := t.handleNodeAllocationRequestProvisioning(ctx, unstructuredClusterInstance)
	if err != nil || (res == doNotRequeue() && !proceed) || res.RequeueAfter > 0 {
		if err != nil {
			ctlrutils.LogError(ctx, t.logger, "Hardware provisioning phase failed", err)
		} else if res.RequeueAfter > 0 {
			t.logger.InfoContext(ctx, "Hardware provisioning in progress, requeueing",
				slog.Duration("requeueAfter", res.RequeueAfter))
		}
		return res, err
	}

	ctlrutils.LogPhaseComplete(ctx, t.logger, "hardware_provisioning", time.Since(phaseStartTime))
	return ctrl.Result{}, nil
}

// executeClusterResourcesPhase handles cluster resources creation
func (t *provisioningRequestReconcilerTask) executeClusterResourcesPhase(ctx context.Context,
	renderedClusterInstance *siteconfig.ClusterInstance) (ctrl.Result, error) {

	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "cluster_resources")
	phaseStartTime := time.Now()

	err := t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Cluster resources phase failed", err)
		result, _ := requeueWithError(err)
		return result, err
	}

	ctlrutils.LogPhaseComplete(ctx, t.logger, "cluster_resources", time.Since(phaseStartTime))
	return ctrl.Result{}, nil
}

// executeClusterInstallationPhase handles cluster installation
func (t *provisioningRequestReconcilerTask) executeClusterInstallationPhase(ctx context.Context,
	unstructuredClusterInstance *unstructured.Unstructured) (ctrl.Result, error) {

	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "cluster_installation")
	phaseStartTime := time.Now()

	err := t.handleClusterInstallation(ctx, unstructuredClusterInstance)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Cluster installation phase failed", err)
		result, _ := requeueWithError(err)
		return result, err
	}

	ctlrutils.LogPhaseComplete(ctx, t.logger, "cluster_installation", time.Since(phaseStartTime))

	// Check cluster provision status
	if !ctlrutils.IsClusterProvisionPresent(t.object) {
		t.logger.InfoContext(ctx, "ClusterProvision not present, requeueing", slog.String("name", t.object.Name))
		return requeueWithShortInterval(), nil
	} else if ctlrutils.IsClusterProvisionTimedOutOrFailed(t.object) {
		// Even after timeout/failure, continue monitoring for cleanup completion
		// This allows detection of manual recovery or cleanup progress
		t.logger.InfoContext(ctx, "ClusterProvision timed out or failed, monitoring cleanup", slog.String("name", t.object.Name))
		return requeueWithLongInterval(), nil
	}

	return ctrl.Result{}, nil
}

// handlePostProvisioning handles policy configuration, upgrades, and finalization
func (t *provisioningRequestReconcilerTask) handlePostProvisioning(ctx context.Context,
	renderedClusterInstance *unstructured.Unstructured) (ctrl.Result, error) {

	// Handle policy configuration
	requeueForConfig, err := t.handleClusterPolicyConfiguration(ctx)
	if err != nil {
		result, _ := requeueWithError(err)
		return result, err
	}

	// Handle upgrades if ZTP is done
	if ctlrutils.IsClusterZtpDone(t.object) {
		result, err := t.handleClusterUpgrades(ctx, renderedClusterInstance.GetName())
		if err != nil || result.Requeue || result.RequeueAfter > 0 {
			return result, err
		}
	}

	// Check if we need to requeue for ongoing operations
	if !ctlrutils.IsClusterProvisionCompleted(t.object) || requeueForConfig {
		return requeueWithLongInterval(), nil
	}

	// Finalize if everything is complete
	err = t.finalizeProvisioningIfComplete(ctx)
	if err != nil {
		result, _ := requeueWithError(err)
		return result, err
	}

	return doNotRequeue(), nil
}

// checkOverallProvisioningTimeout checks if the overall provisioning process has exceeded any timeout
// and sets the appropriate failed state if so. Returns a non-zero RequeueAfter to indicate timeout detected.
func (t *provisioningRequestReconcilerTask) checkOverallProvisioningTimeout(ctx context.Context) ctrl.Result {
	// Skip timeout checks for fulfilled states
	if ctlrutils.IsProvisioningStateFulfilled(t.object) {
		return ctrl.Result{}
	}

	now := time.Now()

	// Check overall provisioning timeout to catch early failures
	// Use ProvisioningStatus.UpdateTime when provisioning state changes to InProgress for current generation
	overallTimeout := t.timeouts.clusterProvisioning + t.timeouts.hardwareProvisioning

	// Only check timeout if we're in a provisioning state (not fulfilled/failed)
	// and if we have a start time for the current generation
	if !t.object.Status.ProvisioningStatus.UpdateTime.IsZero() &&
		t.object.Status.ObservedGeneration == t.object.Generation &&
		(t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StatePending ||
			t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateProgressing) {

		provisioningStartTime := t.object.Status.ProvisioningStatus.UpdateTime.Time
		if ctlrutils.TimeoutExceeded(provisioningStartTime, overallTimeout) {
			t.logger.ErrorContext(ctx, "Overall provisioning timeout exceeded",
				slog.Duration("elapsed", now.Sub(provisioningStartTime)),
				slog.Duration("timeout", overallTimeout))

			ctlrutils.SetProvisioningStateFailed(t.object,
				fmt.Sprintf("Overall provisioning timed out after %v", overallTimeout))

			if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				t.logger.WarnContext(ctx, "Failed to update status for overall provisioning timeout", slog.String("error", err.Error()))
			}
			return requeueWithMediumInterval()
		}
	}

	// Check hardware provisioning timeout
	// Check hardware provisioning timeout (with callback awareness)
	if result := t.checkHardwareProvisioningTimeout(ctx, now); result.RequeueAfter > 0 {
		return result
	}

	// Check cluster installation timeout
	if t.object.Status.Extensions.ClusterDetails != nil {
		clusterProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
		if clusterProvisionedCond != nil && clusterProvisionedCond.Status == metav1.ConditionFalse &&
			clusterProvisionedCond.Reason != string(provisioningv1alpha1.CRconditionReasons.Failed) {
			clusterStartTime := t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt
			if !clusterStartTime.IsZero() && ctlrutils.TimeoutExceeded(clusterStartTime.Time, t.timeouts.clusterProvisioning) {
				t.logger.ErrorContext(ctx, "Cluster installation timeout exceeded",
					slog.Duration("elapsed", now.Sub(clusterStartTime.Time)),
					slog.Duration("timeout", t.timeouts.clusterProvisioning))

				ctlrutils.SetProvisioningStateFailed(t.object,
					fmt.Sprintf("Cluster installation timed out after %v", t.timeouts.clusterProvisioning))

				if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
					t.logger.WarnContext(ctx, "Failed to update status for cluster installation timeout", slog.String("error", err.Error()))
				}
				return requeueWithMediumInterval()
			}
		}
	}

	// Check cluster configuration timeout
	if t.object.Status.Extensions.ClusterDetails != nil {
		configAppliedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		if configAppliedCond != nil && configAppliedCond.Status == metav1.ConditionFalse &&
			configAppliedCond.Reason != string(provisioningv1alpha1.CRconditionReasons.Failed) {
			configStartTime := t.object.Status.Extensions.ClusterDetails.NonCompliantAt
			if !configStartTime.IsZero() && ctlrutils.TimeoutExceeded(configStartTime.Time, t.timeouts.clusterConfiguration) {
				t.logger.ErrorContext(ctx, "Cluster configuration timeout exceeded",
					slog.Duration("elapsed", now.Sub(configStartTime.Time)),
					slog.Duration("timeout", t.timeouts.clusterConfiguration))

				ctlrutils.SetProvisioningStateFailed(t.object,
					fmt.Sprintf("Cluster configuration timed out after %v", t.timeouts.clusterConfiguration))

				if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
					t.logger.WarnContext(ctx, "Failed to update status for cluster configuration timeout", slog.String("error", err.Error()))
				}
				return requeueWithMediumInterval()
			}
		}
	}

	// No timeout detected
	return ctrl.Result{}
}

// initializeHardwarePluginIfNeeded initializes the hardware plugin client if hardware template is present
func (t *provisioningRequestReconcilerTask) initializeHardwarePluginIfNeeded(ctx context.Context) error {
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err == nil && clusterTemplate.Spec.Templates.HwTemplate != "" {
		if t.hwpluginClient == nil {
			if err := t.initializeHardwarePluginClientWithRetry(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleClusterUpgrades handles cluster upgrade logic
func (t *provisioningRequestReconcilerTask) handleClusterUpgrades(ctx context.Context, clusterName string) (ctrl.Result, error) {
	shouldUpgrade, err := t.IsUpgradeRequested(ctx, clusterName)
	if err != nil {
		result, _ := requeueWithError(err)
		return result, err
	}

	// An upgrade is requested or upgrade has started but not completed
	if shouldUpgrade ||
		(ctlrutils.IsClusterUpgradeInitiated(t.object) &&
			!ctlrutils.IsClusterUpgradeCompleted(t.object)) {
		upgradeCtrlResult, proceed, err := t.handleUpgrade(ctx, clusterName)
		if upgradeCtrlResult.RequeueAfter > 0 || !proceed || err != nil {
			// Requeue if the upgrade is in progress or an error occurs.
			// Stop reconciliation if the upgrade has failed.
			// Proceed if the upgrade is completed.
			return upgradeCtrlResult, err
		}
	}

	return ctrl.Result{}, nil
}

// shouldStopReconciliation checks if the reconciliation should stop.
func (t *provisioningRequestReconcilerTask) shouldStopReconciliation() bool {
	// Only stop reconciliation if we've timed out
	return t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFailed &&
		strings.Contains(t.object.Status.ProvisioningStatus.ProvisioningDetails, timedOutMessage) &&
		t.object.Status.ObservedGeneration == t.object.Generation
}

// checkHardwareProvisioningTimeout checks for hardware provisioning timeout with callback awareness
func (t *provisioningRequestReconcilerTask) checkHardwareProvisioningTimeout(ctx context.Context, now time.Time) ctrl.Result {
	if t.isHardwareProvisionSkipped() || t.object.Status.Extensions.NodeAllocationRequestRef == nil {
		return ctrl.Result{}
	}

	hwProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
	if hwProvisionedCond == nil || hwProvisionedCond.Status != metav1.ConditionFalse ||
		hwProvisionedCond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) {
		return ctrl.Result{}
	}

	hwStartTime := t.object.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart
	if hwStartTime.IsZero() || !ctlrutils.TimeoutExceeded(hwStartTime.Time, t.timeouts.hardwareProvisioning) {
		return ctrl.Result{}
	}

	// Hardware provisioning timeout detected
	t.logger.ErrorContext(ctx, "Hardware provisioning timeout exceeded",
		slog.Duration("elapsed", now.Sub(hwStartTime.Time)),
		slog.Duration("timeout", t.timeouts.hardwareProvisioning))

	ctlrutils.SetProvisioningStateFailed(t.object,
		fmt.Sprintf("Hardware provisioning timed out after %v", t.timeouts.hardwareProvisioning))

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		t.logger.WarnContext(ctx, "Failed to update status for hardware provisioning timeout", slog.String("error", err.Error()))
	}

	return requeueWithMediumInterval()
}

// handlePreProvisioning handles the validation, rendering and creation of required resources.
// It returns a rendered ClusterInstance for successful pre-provisioning, a ctrl.Result to
// indicate if/when to requeue, and an error if any issues occur.
func (t *provisioningRequestReconcilerTask) handlePreProvisioning(ctx context.Context) (*siteconfig.ClusterInstance, ctrl.Result, error) {
	// Set the provisioning state to pending if spec changes are observed
	if t.object.Status.ObservedGeneration != t.object.Generation {
		ctlrutils.SetProvisioningStatePending(t.object, ctlrutils.ValidationMessage)
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			t.logger.WarnContext(ctx, "Status update failed, will retry", slog.String("error", updateErr.Error()))
			return nil, requeueWithShortInterval(), fmt.Errorf(
				"failed to update status for ProvisioningRequest %s: %w",
				t.object.Name, updateErr,
			)
		}
	}

	// Validate the ProvisioningRequest
	err := t.handleValidation(ctx)
	if err != nil {
		if ctlrutils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return nil, res, err
		}
		// internal error that might recover - requeue to allow recovery
		t.logger.WarnContext(ctx, "Internal validation error, will retry", slog.String("error", err.Error()))
		return nil, requeueWithMediumInterval(), err
	}

	// Render and validate ClusterInstance
	renderedClusterInstance, err := t.handleRenderClusterInstance(ctx)
	if err != nil {
		if ctlrutils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return nil, res, err
		}
		// internal error that might recover - requeue to allow recovery
		t.logger.WarnContext(ctx, "Internal ClusterInstance rendering error, will retry", slog.String("error", err.Error()))
		return nil, requeueWithMediumInterval(), err
	}

	// Handle the creation of resources required for cluster deployment
	// Only create ClusterInstance resources if hardware provisioning is skipped
	// For hardware provisioning scenarios, cluster resources are created after hardware completion
	if t.isHardwareProvisionSkipped() {
		err = t.handleClusterResources(ctx, renderedClusterInstance)
		if err != nil {
			if ctlrutils.IsInputError(err) {
				_, err = t.checkClusterDeployConfigState(ctx)
				if err != nil {
					t.logger.WarnContext(ctx, "Cluster deploy config state check failed, will retry", slog.String("error", err.Error()))
					return nil, requeueWithMediumInterval(), err
				}
				// Requeue since we are not watching for updates to required resources
				// if they are missing
				return nil, requeueWithMediumInterval(), nil
			}
			// internal error that might recover - requeue to allow recovery
			t.logger.WarnContext(ctx, "Internal cluster resources error, will retry", slog.String("error", err.Error()))
			return nil, requeueWithMediumInterval(), err
		}
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
		if ctlrutils.IsInputError(err) {
			res, err := t.checkClusterDeployConfigState(ctx)
			return res, false, err
		}
		t.logger.ErrorContext(ctx, "Hardware template rendering error", slog.String("error", err.Error()))
		return doNotRequeue(), false, err
	}

	// Create/Update the NodeAllocationRequest
	if err := t.createOrUpdateNodeAllocationRequest(ctx, renderedClusterInstance.GetNamespace(), renderedNodeAllocationRequest); err != nil {
		t.logger.WarnContext(ctx, "NodeAllocationRequest create/update error, will retry", slog.String("error", err.Error()))
		return requeueWithMediumInterval(), false, err
	}

	nodeAllocationRequestID := t.getNodeAllocationRequestID()
	if nodeAllocationRequestID == "" {
		t.logger.WarnContext(ctx, "Missing NodeAllocationRequest identifier, will retry")
		return requeueWithShortInterval(), false, fmt.Errorf("missing nodeAllocationRequest identifier")
	}

	nodeAllocationRequestResponse, exists, err := t.getNodeAllocationRequestResponse(ctx)
	if err != nil {
		t.logger.WarnContext(ctx, "NodeAllocationRequest response error, will retry", slog.String("error", err.Error()))
		return requeueWithMediumInterval(), false, err
	}
	if !exists {
		return requeueWithShortInterval(), false, nil
	}

	// Wait for the NodeAllocationRequest to be provisioned and update BMC details if necessary
	provisioned, configured, timedOutOrFailed, err := t.waitForHardwareData(ctx, renderedClusterInstance, nodeAllocationRequestResponse)
	if err != nil {
		t.logger.WarnContext(ctx, "Hardware data wait error, will retry", slog.String("error", err.Error()))
		return requeueWithMediumInterval(), false, err
	}
	if timedOutOrFailed {
		err = t.resetHardwareTimersAndPersist(ctx)
		return requeueWithMediumInterval(), false, err
	}
	if !provisioned {

		t.logger.InfoContext(ctx, "Waiting for NodeAllocationRequest to be provisioned",
			slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
		return requeueWithMediumInterval(), false, nil
	}

	// Provisioning is done. Evaluate configuration succinctly.
	configStart := t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart
	switch {
	case configured != nil && *configured:
		// Config completed → clear timers and proceed
		err = t.resetHardwareTimersAndPersist(ctx)
		if err != nil {
			return requeueWithShortInterval(), false, err
		}
		return doNotRequeue(), true, nil
	case configured != nil && !*configured:
		// Config explicitly not done yet → wait
		t.logger.InfoContext(ctx, "Waiting for NodeAllocationRequest to be configured",
			slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
		return requeueWithMediumInterval(), false, nil
	case !configStart.IsZero() && configured == nil:
		// Config has started but not reported yet → wait
		t.logger.InfoContext(ctx, "Waiting for NodeAllocationRequest to be configured",
			slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
		return requeueWithMediumInterval(), false, nil
	default:
		// configured == nil and configStart.IsZero() → no configuration needed; proceed
		return doNotRequeue(), true, nil
	}
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
			// If NodeAllocationRequest doesn't exist and this is likely initial validation phase,
			// skip hardware status checks and proceed to resource preparation status check
			if t.object.Status.Extensions.NodeAllocationRequestRef == nil {
				// No NodeAllocationRequestRef means this is initial phase, skip hardware checks
			} else {
				// NodeAllocationRequestRef exists but getNodeAllocationRequestResponse failed,
				// this indicates a real hardware plugin error
				return requeueWithError(err)
			}
		} else {
			hwProvisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwmgmtv1alpha1.Provisioned)
			if err != nil {
				return requeueWithError(err)
			}
			if timedOutOrFailed {
				// Continue requeuing to allow overall timeout or spec changes to potentially recover
				return requeueWithMediumInterval(), nil
			}
			if !hwProvisioned {
				return requeueWithMediumInterval(), nil
			}
		}
	}

	// Check the ClusterInstance status if exists
	if t.object.Status.Extensions.ClusterDetails == nil {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
		// For fulfilled state, skip early return and continue to final fulfilled check
		// For non-fulfilled state, continue monitoring for resource preparation completion
		if !ctlrutils.IsProvisioningStateFulfilled(t.object) {
			t.logger.InfoContext(ctx, "ClusterDetails not yet available, monitoring resource preparation")
			return requeueWithMediumInterval(), nil
		}
		// If fulfilled state, continue to the end of function for proper fulfilled handling
	}

	// Always check timeout even if other checks fail
	// This ensures we never miss timeout detection due to transient errors
	timeoutDetected := t.checkClusterInstallationTimeout(ctx)
	if timeoutDetected {
		t.logger.WarnContext(ctx, "Cluster installation timeout detected",
			slog.String("name", t.object.Name),
			slog.Duration("timeout", t.timeouts.clusterProvisioning))
		// Continue to monitor even after timeout to detect cleanup completion
		return requeueWithMediumInterval(), nil
	}

	// Always check resource preparation status to detect any failed conditions
	// (e.g., ClusterInstanceRendered failures after hardware provisioning succeeds)
	if err = t.checkResourcePreparationStatus(ctx); err != nil {
		return requeueWithError(err)
	}

	// If resource preparation check set the state to failed due to validation errors, stop processing
	// Only stop for persistent validation failures, not temporary cluster installation issues
	if t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFailed {
		// Check if this is a validation failure (persistent) vs installation issue (temporary)
		if cond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered)); cond != nil && cond.Status == metav1.ConditionFalse && strings.Contains(cond.Message, "Required value") {
			// This is a persistent validation error that won't resolve by itself
			return doNotRequeue(), nil
		}
		// For other failures (like cluster installation restrictions), continue processing
	}

	// Check cluster provision status if ClusterDetails exists
	if t.object.Status.Extensions.ClusterDetails != nil {
		err = t.checkClusterProvisionStatus(
			ctx, t.object.Status.Extensions.ClusterDetails.Name)
		if err != nil {
			// Don't stop on status check errors - timeout monitoring must continue
			t.logger.WarnContext(ctx, "Failed to check cluster provision status, continuing monitoring",
				slog.String("error", err.Error()),
				slog.String("name", t.object.Name))
			// Continue with timeout monitoring even if status check fails
		}
	}
	if !ctlrutils.IsClusterProvisionPresent(t.object) ||
		ctlrutils.IsClusterProvisionTimedOutOrFailed(t.object) {
		// Even for timeout/failure cases, continue monitoring unless in fulfilled state
		// This allows detection of cleanup progress and manual recovery
		if !ctlrutils.IsProvisioningStateFulfilled(t.object) {
			// Continue monitoring timeout/failed states for cleanup completion
			return requeueWithLongInterval(), nil
		}
	}

	// Check the policy configuration status
	requeueForConfig, err := t.handleClusterPolicyConfiguration(ctx)
	if err != nil {
		return requeueWithError(err)
	}
	// Requeue if Cluster Provisioned is not completed (in-progress or unknown)
	// or there are enforce policies that are not Compliant and configuration
	// has not timed out
	if !ctlrutils.IsClusterProvisionCompleted(t.object) || requeueForConfig {
		return requeueWithLongInterval(), nil
	}

	err = t.finalizeProvisioningIfComplete(ctx)
	if err != nil {
		return requeueWithError(err)
	}

	// If the existing provisioning has been fulfilled, check if there are any issues
	// with the validation, rendering, or creation of resources due to updates to the
	// ProvisioningRequest. If there are issues, transition the provisioningPhase to failed.
	if ctlrutils.IsProvisioningStateFulfilled(t.object) {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
		// Continue monitoring even after fulfillment for spec changes
		t.logger.DebugContext(ctx, "Fulfilled provisioning check complete, continuing monitoring")
		return requeueWithLongInterval(), nil
	}
	return doNotRequeue(), nil
}

// checkClusterInstallationTimeout ensures reliable timeout detection even when other checks fail.
// This function provides a backup timeout mechanism that works independently of ClusterInstance status.
// It only applies to active cluster installation (not day 2 operations or fulfilled states).
func (t *provisioningRequestReconcilerTask) checkClusterInstallationTimeout(ctx context.Context) bool {
	// Only check timeout if we have ClusterDetails and a valid start timestamp
	if t.object.Status.Extensions.ClusterDetails == nil ||
		t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
		return false
	}

	// Skip timeout check for fulfilled states - they represent completed clusters
	// that may be undergoing day 2 operations, not initial provisioning
	if ctlrutils.IsProvisioningStateFulfilled(t.object) {
		return false
	}

	// Skip if cluster is already completed, failed, or timed out
	if ctlrutils.IsClusterProvisionCompleted(t.object) ||
		ctlrutils.IsClusterProvisionTimedOutOrFailed(t.object) {
		return false
	}

	// Check if timeout has been exceeded
	startTime := t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.Time
	if ctlrutils.TimeoutExceeded(startTime, t.timeouts.clusterProvisioning) {
		// Set timeout condition and failed state
		message := fmt.Sprintf("Cluster installation timed out after %s", t.timeouts.clusterProvisioning)
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
			provisioningv1alpha1.CRconditionReasons.TimedOut,
			metav1.ConditionFalse,
			message,
		)
		ctlrutils.SetProvisioningStateFailed(t.object, message)

		// Attempt to persist the timeout status, but don't fail if it doesn't work
		// The next reconciliation will detect the timeout again if status update fails
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			t.logger.WarnContext(ctx, "Failed to update timeout status, will retry next reconciliation",
				slog.String("error", updateErr.Error()),
				slog.String("name", t.object.Name))
		}

		return true
	}

	return false
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
			ctlrutils.SetProvisioningStateFailed(t.object, cond.Message)
			break
		}
	}

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) handleValidation(ctx context.Context) error {
	// Validate provisioning request CR
	err := t.validateProvisioningRequestCR(ctx)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to validate the ProvisioningRequest", err,
			slog.String("name", t.object.Name))
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.Validated,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to validate the ProvisioningRequest: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(ctx, "Validated the ProvisioningRequest CR",
			slog.String("name", t.object.Name))
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.Validated,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The provisioning request validation succeeded",
		)
	}

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
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
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
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

		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"ClusterInstance rendered and passed dry-run validation",
		)
	}

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
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

		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
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

		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Cluster resources applied",
		)
	}
	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
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

		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
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

		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered Hardware template successfully",
		)
	}

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
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
				r.Logger.WarnContext(ctx, "Failed to add finalizer, will retry", slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to update ProvisioningRequest with finalizer: %w", err)
			}
			// Requeue since the finalizer has been added.
			return requeueImmediately(), false, nil
		}
	} else if controllerutil.ContainsFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer) {
		r.Logger.InfoContext(ctx, "ProvisioningRequest is being deleted",
			slog.String("name", provisioningRequest.Name))
		deleteComplete, err := r.handleProvisioningRequestDeletion(ctx, provisioningRequest)
		if !deleteComplete {
			return requeueWithShortInterval(), true, err
		}

		// Deletion has completed. Remove provisioningRequestFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Dependents have been deleted. Removing provisioningRequest finalizer", "name", provisioningRequest.Name)
		patch := client.MergeFrom(provisioningRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(provisioningRequest, provisioningv1alpha1.ProvisioningRequestFinalizer) {
			if err := r.Patch(ctx, provisioningRequest, patch); err != nil {
				r.Logger.WarnContext(ctx, "Failed to remove finalizer, will retry", slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to patch ProvisioningRequest: %w", err)
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
		ctlrutils.SetProvisioningStateDeleting(provisioningRequest)
		if err := ctlrutils.UpdateK8sCRStatus(ctx, r.Client, provisioningRequest); err != nil {
			return false, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", provisioningRequest.Name, err)
		}
	}

	// Delete the NodeAllocationRequest CR first
	if provisioningRequest.Status.Extensions.NodeAllocationRequestRef != nil {
		// Get hwplugin client for the HardwarePlugin
		hwpluginClient, err := getHardwarePluginClient(ctx, r.Client, r.Logger, provisioningRequest)
		if err != nil {
			return false, fmt.Errorf("failed to get HardwarePlugin client: %w", err)
		}

		nodeAllocationRequestID := provisioningRequest.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
		if nodeAllocationRequestID != "" {
			r.Logger.InfoContext(ctx, "Deleting NodeAllocationRequest",
				slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
			// Create adapter for the hardware plugin client
			clientAdapter := hwmgrpluginapi.NewHardwarePluginClientAdapter(hwpluginClient)
			resp, narExists, err := clientAdapter.DeleteNodeAllocationRequest(ctx, nodeAllocationRequestID)
			if err != nil {
				return false, fmt.Errorf("failed to delete NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
			}

			if resp == nodeAllocationRequestID {
				r.Logger.InfoContext(ctx, "Deletion request for NodeAllocationRequest successful",
					slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
			}

			if narExists {
				r.Logger.InfoContext(ctx, "Waiting for NodeAllocationRequest to be deleted",
					slog.String("nodeAllocationRequestID", nodeAllocationRequestID))
				return false, nil
			}
		}
	}

	// Delete the ClusterInstance CR next
	if provisioningRequest.Status.Extensions.ClusterDetails != nil {
		clusterInstanceName := provisioningRequest.Status.Extensions.ClusterDetails.Name

		r.Logger.InfoContext(ctx, fmt.Sprintf("Checking ClusterInstance (%s)", clusterInstanceName))
		clusterInstance := &siteconfig.ClusterInstance{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: clusterInstanceName, Namespace: clusterInstanceName}, clusterInstance); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return false, fmt.Errorf("failed to get ClusterInstance %s: %w", clusterInstanceName, err)
			}
			r.Logger.InfoContext(ctx, fmt.Sprintf("ClusterInstance (%s) not found, so it is already deleted", clusterInstanceName))
		} else {
			if clusterInstance.DeletionTimestamp.IsZero() {
				r.Logger.InfoContext(ctx, fmt.Sprintf("Deleting ClusterInstance (%s)", clusterInstanceName))
				if err := r.Client.Delete(ctx, clusterInstance); err != nil {
					return false, fmt.Errorf("failed to delete ClusterInstance %s: %w", clusterInstanceName, err)
				}
			}
			r.Logger.InfoContext(ctx, fmt.Sprintf("Waiting for ClusterInstance (%s) to be deleted", clusterInstanceName))
			return false, nil
		}
	} else {
		r.Logger.InfoContext(ctx, "provisioningRequest.Status.Extensions.ClusterDetails is nil")
	}

	// Delete the ClusterNamespace CR last

	// List resources by label
	var labels = map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: provisioningRequest.Name,
	}
	listOpts := []client.ListOption{
		client.MatchingLabels(labels),
	}

	deleteCompleted := true

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
	if ctlrutils.IsClusterProvisionCompleted(t.object) && ctlrutils.IsClusterConfigCompleted(t.object) &&
		(t.isHardwareProvisionSkipped() || ctlrutils.IsHardwareConfigCompleted(t.object)) &&
		(!ctlrutils.IsClusterUpgradeInitiated(t.object) || ctlrutils.IsClusterUpgradeCompleted(t.object)) {

		ctlrutils.SetProvisioningStateFulfilled(t.object)
		mcl, err := t.updateOCloudNodeClusterId(ctx)
		if err != nil {
			return err
		}

		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
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

// initializeHardwarePluginClientWithRetry attempts to initialize the hardware plugin client
// with synchronous exponential backoff retry logic to handle temporary failures.
func (t *provisioningRequestReconcilerTask) initializeHardwarePluginClientWithRetry(ctx context.Context) error {
	var lastErr error

	for attempt := 1; attempt <= maxHardwareClientRetries; attempt++ {
		hwclient, err := getHardwarePluginClient(ctx, t.client, t.logger, t.object)
		if err == nil {
			// Success - initialize client and return
			t.hwpluginClient = hwmgrpluginapi.NewHardwarePluginClientAdapter(hwclient)

			if attempt > 1 {
				t.logger.InfoContext(ctx,
					"Hardware plugin client initialized successfully after retries",
					slog.String("provisioningRequest", t.object.Name),
					slog.Int("attempts", attempt))
			} else {
				t.logger.InfoContext(ctx,
					"Hardware plugin client initialized successfully",
					slog.String("provisioningRequest", t.object.Name))
			}

			return nil
		}

		lastErr = err

		if attempt == maxHardwareClientRetries {
			// Maximum retries exceeded, fail permanently
			t.logger.ErrorContext(ctx,
				"Failed to initialize hardware plugin client after maximum retries",
				slog.String("provisioningRequest", t.object.Name),
				slog.Int("maxRetries", maxHardwareClientRetries),
				slog.String("error", err.Error()))
			break
		}

		// Calculate exponential backoff delay: baseRetryDelay * 2^(attempt-1)
		delay := baseRetryDelay * time.Duration(1<<(attempt-1))

		t.logger.WarnContext(ctx,
			"Hardware plugin client initialization failed, retrying",
			slog.String("provisioningRequest", t.object.Name),
			slog.Int("attempt", attempt),
			slog.Int("maxRetries", maxHardwareClientRetries),
			slog.Duration("retryDelay", delay),
			slog.String("error", err.Error()))

		// Sleep before retry (except for the last attempt)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("hardware plugin client initialization failed after %d retries: %w",
		maxHardwareClientRetries, lastErr)
}

// getHardwarePluginClient is a convenience wrapper function to get the HardwarePluginClient object
func getHardwarePluginClient(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pr *provisioningv1alpha1.ProvisioningRequest,
) (*hwmgrpluginapi.HardwarePluginClient, error) {
	// Get the HardwarePlugin CR
	hwplugin, err := ctlrutils.GetHardwarePluginFromProvisioningRequest(ctx, c, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve HardwarePlugin: %w", err)
	}

	// Validate that the HardwarePlugin CR is registered successfully
	validated := meta.FindStatusCondition(hwplugin.Status.Conditions, string(hwmgmtv1alpha1.ConditionTypes.Registration))
	if validated == nil || validated.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("hardwarePlugin '%s' is not registered", hwplugin.Name)
	}

	// Get hwplugin client for the HardwarePlugin
	// nolint: wrapcheck
	return hwmgrpluginapi.NewHardwarePluginClient(ctx, c, logger, hwplugin)
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

	// Check if hardware plugin client is available
	if t.hwpluginClient == nil {
		return nil, false, fmt.Errorf("hardware plugin client is not available")
	}

	var (
		nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse
		exists                        bool
		err                           error
	)
	// Get the generated NodeAllocationRequest and its status.
	if err = ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
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

func (t *provisioningRequestReconcilerTask) resetHardwareTimers() bool {
	ref := t.object.Status.Extensions.NodeAllocationRequestRef
	if ref == nil {
		return false
	}
	changed := false
	if !ref.HardwareProvisioningCheckStart.IsZero() {
		ref.HardwareProvisioningCheckStart = &metav1.Time{}
		changed = true
	}
	if !ref.HardwareConfiguringCheckStart.IsZero() {
		ref.HardwareConfiguringCheckStart = &metav1.Time{}
		changed = true
	}
	return changed
}

func (t *provisioningRequestReconcilerTask) resetHardwareTimersAndPersist(ctx context.Context) error {
	if t.resetHardwareTimers() {
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return fmt.Errorf("failed to persist NAR timer reset: %w", err)
		}
	}
	return nil
}
