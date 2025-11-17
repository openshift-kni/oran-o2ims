/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	narcallbackclient "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/nar-callback"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

const (
	// maxCallbackRetries defines the maximum number of callback retry attempts
	maxCallbackRetries = 3

	// maxConcurrentCallbacks limits the number of concurrent callback goroutines
	// to prevent resource exhaustion
	maxConcurrentCallbacks = 20
)

// mapConditionToCallbackStatus maps hardware management condition types and reasons to callback status values
func mapConditionToCallbackStatus(conditionType hwmgmtv1alpha1.ConditionType, conditionReason hwmgmtv1alpha1.ConditionReason) narcallbackclient.CallbackPayloadStatus {
	switch conditionType {
	case hwmgmtv1alpha1.Provisioned:
		switch conditionReason {
		case hwmgmtv1alpha1.InProgress:
			return narcallbackclient.InProgress
		case hwmgmtv1alpha1.Completed:
			return narcallbackclient.Completed
		case hwmgmtv1alpha1.Failed:
			return narcallbackclient.Failed
		case hwmgmtv1alpha1.TimedOut:
			return narcallbackclient.TimedOut
		case hwmgmtv1alpha1.Unprovisioned:
			return narcallbackclient.Unprovisioned
		case hwmgmtv1alpha1.NotInitialized:
			return narcallbackclient.NotInitialized
		case hwmgmtv1alpha1.InvalidInput:
			return narcallbackclient.InvalidInput
		}
	case hwmgmtv1alpha1.Configured:
		switch conditionReason {
		case hwmgmtv1alpha1.InProgress:
			return narcallbackclient.InProgress
		case hwmgmtv1alpha1.Completed, hwmgmtv1alpha1.ConfigApplied:
			return narcallbackclient.ConfigurationApplied
		case hwmgmtv1alpha1.Failed:
			return narcallbackclient.Failed
		case hwmgmtv1alpha1.TimedOut:
			return narcallbackclient.TimedOut
		case hwmgmtv1alpha1.ConfigUpdate:
			return narcallbackclient.ConfigurationUpdateRequested
		case hwmgmtv1alpha1.InvalidInput:
			return narcallbackclient.InvalidInput
		}
	case hwmgmtv1alpha1.Validation:
		switch conditionReason {
		case hwmgmtv1alpha1.InProgress:
			return narcallbackclient.InProgress
		case hwmgmtv1alpha1.Completed:
			return narcallbackclient.Completed
		case hwmgmtv1alpha1.Failed:
			return narcallbackclient.Failed
		case hwmgmtv1alpha1.InvalidInput:
			return narcallbackclient.InvalidInput
		}
	case hwmgmtv1alpha1.Unknown:
		return narcallbackclient.Pending
	}

	// Default fallback
	return narcallbackclient.Pending
}

// updateConditionAndSendCallback updates the NodeAllocationRequest condition and sends a callback notification
func (r *NodeAllocationRequestReconciler) updateConditionAndSendCallback(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	conditionType hwmgmtv1alpha1.ConditionType,
	conditionReason hwmgmtv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string) error {

	// Update the condition
	if err := hwmgrutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
		conditionType, conditionReason, conditionStatus, message); err != nil {
		return err //nolint:wrapcheck
	}

	// Send callback notification asynchronously (non-blocking)
	callbackStatus := mapConditionToCallbackStatus(conditionType, conditionReason)
	errorMsg := ""
	if conditionStatus == metav1.ConditionFalse && (conditionReason == hwmgmtv1alpha1.Failed ||
		conditionReason == hwmgmtv1alpha1.TimedOut || conditionReason == hwmgmtv1alpha1.InvalidInput) {
		errorMsg = message
	}

	// Send callback with async retries to avoid blocking the controller
	callbackCtx := r.callbackCtx
	if callbackCtx == nil {
		// Fallback to background context if not initialized (shouldn't happen in normal operation)
		r.Logger.WarnContext(ctx, "Callback context not initialized, using background context")
		callbackCtx = context.Background()
	}

	// Launch callback goroutine with semaphore-based rate limiting
	r.activeCallbacks.Add(1)
	go r.sendCallbackWithAsyncRetryRateLimited(callbackCtx, nodeAllocationRequest, callbackStatus, errorMsg)

	return nil
}

// NodeAllocationRequestReconciler reconciles NodeAllocationRequest objects associated with the Metal3 H/W plugin
type NodeAllocationRequestReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	indexerEnabled  bool
	PluginNamespace string

	// Goroutine management for callback retries
	callbackCtx       context.Context
	callbackCancel    context.CancelFunc
	activeCallbacks   sync.WaitGroup
	callbackSemaphore chan struct{} // Limits concurrent callback goroutines
}

// InitializeCallbackContext sets up the long-lived context for callback goroutines
func (r *NodeAllocationRequestReconciler) InitializeCallbackContext(ctx context.Context) {
	r.callbackCtx, r.callbackCancel = context.WithCancel(ctx)
	r.callbackSemaphore = make(chan struct{}, maxConcurrentCallbacks)
	r.Logger.Info("Callback context initialized",
		slog.Int("maxConcurrentCallbacks", maxConcurrentCallbacks))
}

// ShutdownCallbacks gracefully shuts down all active callback goroutines
func (r *NodeAllocationRequestReconciler) ShutdownCallbacks(timeout time.Duration) {
	r.Logger.Info("Shutting down callback goroutines...")

	// Cancel the callback context to signal all goroutines to stop
	if r.callbackCancel != nil {
		r.callbackCancel()
	}

	// Wait for all active callbacks to complete with timeout
	done := make(chan struct{})
	go func() {
		r.activeCallbacks.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.Logger.Info("All callback goroutines terminated successfully")
	case <-time.After(timeout):
		r.Logger.Warn("Timeout waiting for callback goroutines to terminate",
			slog.Duration("timeout", timeout))
	}
}

func (r *NodeAllocationRequestReconciler) SetupIndexer(ctx context.Context) error {
	r.Logger.Info("SetupIndexer Start")
	// Setup AllocatedNode CRD indexer. This field indexer allows us to query a list of AllocatedNode CRs, filtered by the spec.nodeAllocationRequest field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*pluginsv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
	}

	if err := r.Manager.GetFieldIndexer().IndexField(ctx, &pluginsv1alpha1.AllocatedNode{}, hwmgrutils.AllocatedNodeSpecNodeAllocationRequestKey, nodeIndexFunc); err != nil {
		return fmt.Errorf("failed to setup node indexer: %w", err)
	}
	return nil
}

//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=nodeallocationrequests,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=nodeallocationrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=nodeallocationrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes,verbs=get;create;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes/finalizers,verbs=update
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareprofiles,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwaresettings,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwarecomponents,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostupdatepolicies,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=firmwareschemas,verbs=get;list;watch
//+kubebuilder:rbac:groups=metal3.io,resources=hardwaredata,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch;delete

func (r *NodeAllocationRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "NodeAllocationRequest")

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

	// Add logging context with the NodeAllocationRequest name
	ctx = logging.AppendCtx(ctx, slog.String("NodeAllocationRequest", req.Name))

	if !r.indexerEnabled {
		if err := r.SetupIndexer(ctx); err != nil {
			return hwmgrutils.DoNotRequeue(), fmt.Errorf("failed to setup indexer: %w", err)
		}
		r.Logger.InfoContext(ctx, "NodeAllocationRequest field indexer initialized")
		r.indexerEnabled = true
	}

	// Fetch the nodeAllocationRequest, using non-caching client
	nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{}
	if err := hwmgrutils.GetNodeAllocationRequest(ctx, r.NoncachedClient, req.NamespacedName, nodeAllocationRequest); err != nil {
		if errors.IsNotFound(err) {
			// The NodeAllocationRequest object has likely been deleted
			r.Logger.InfoContext(ctx, "NodeAllocationRequest not found, assuming deleted")
			return hwmgrutils.DoNotRequeue(), nil
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch NodeAllocationRequest", err)
		return hwmgrutils.RequeueWithShortInterval(), nil
	}

	// Add object-specific context and hardware-specific context
	ctx = ctlrutils.AddObjectContext(ctx, nodeAllocationRequest)
	ctx = logging.AppendCtx(ctx, slog.String("ClusterID", nodeAllocationRequest.Spec.ClusterId))
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", nodeAllocationRequest.ResourceVersion))

	r.Logger.InfoContext(ctx, "Fetched NodeAllocationRequest successfully")

	if nodeAllocationRequest.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "NodeAllocationRequest is being deleted")
		if controllerutil.ContainsFinalizer(nodeAllocationRequest, hwmgrutils.NodeAllocationRequestFinalizer) {
			completed, deleteErr := r.handleNodeAllocationRequestDeletion(ctx, nodeAllocationRequest)
			if deleteErr != nil {
				return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed HandleNodeAllocationRequestDeletion: %w", deleteErr)
			}

			if !completed {
				r.Logger.InfoContext(ctx, "Deletion handling in progress, requeueing")
				return hwmgrutils.RequeueWithShortInterval(), nil
			}

			if finalizerErr := hwmgrutils.NodeAllocationRequestRemoveFinalizer(ctx, r.Client, nodeAllocationRequest); finalizerErr != nil {
				r.Logger.InfoContext(ctx, "Failed to remove finalizer, requeueing", slog.String("error", finalizerErr.Error()))
				return hwmgrutils.RequeueWithShortInterval(), nil
			}

			r.Logger.InfoContext(ctx, "Deletion handling complete, finalizer removed")
			return hwmgrutils.DoNotRequeue(), nil
		}

		r.Logger.InfoContext(ctx, "No finalizer, deletion handling complete")
		return hwmgrutils.DoNotRequeue(), nil
	}

	// Handle NodeAllocationRequest
	result, err = r.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
	if err != nil {
		return result, fmt.Errorf("failed to handle NodeAllocationRequest: %w", err)
	}

	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeAllocationRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Create a label selector for filtering NodeAllocationRequests pertaining to the Metal3 HardwarePlugin
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			hwmgrutils.HardwarePluginLabel: hwmgrutils.Metal3HardwarePluginID,
		},
	}

	// Create a predicate to filter NodeAllocationRequests with the specified metal3 H/W plugin label
	pred, err := predicate.LabelSelectorPredicate(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to create label selector predicate: %w", err)
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pluginsv1alpha1.NodeAllocationRequest{}).
		WithEventFilter(pred).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	return nil
}

// calculateBackoffDuration calculates exponential backoff duration for the given attempt
func calculateBackoffDuration(attempt int) time.Duration {
	shift := attempt - 1
	if shift > 31 { // Prevent overflow for very large attempt values
		shift = 31
	}
	if shift < 0 { // Safety check for negative values
		shift = 0
	}
	// Convert to uint safely after bounds checking
	uintShift := uint(shift) // #nosec G115 -- shift is bounds-checked above
	return time.Duration(1<<uintShift) * time.Second
}

// sendCallbackWithAsyncRetry sends a callback notification with retry logic running asynchronously
func (r *NodeAllocationRequestReconciler) sendCallbackWithAsyncRetry(ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest, status narcallbackclient.CallbackPayloadStatus, errorMsg string) {
	if nodeAllocationRequest.Spec.Callback == nil || nodeAllocationRequest.Spec.Callback.CallbackURL == "" {
		r.Logger.DebugContext(ctx, "No callback configuration provided, skipping callback")
		return
	}

	callback := nodeAllocationRequest.Spec.Callback
	callbackURLStr := callback.CallbackURL

	// Parse the callback URL to extract the provisioning request name
	callbackURL, err := url.Parse(callbackURLStr)
	if err != nil {
		r.Logger.WarnContext(ctx, "Failed to parse callback URL, skipping callback",
			slog.String("callbackURL", callbackURLStr),
			slog.String("error", err.Error()))
		return
	}

	// Extract provisioning request name from the URL path pattern: /nar-callback/v1/provisioning-requests/{provisioningRequestName}
	if !strings.HasPrefix(callbackURL.Path, constants.NarCallbackServicePath) {
		r.Logger.WarnContext(ctx, "Callback URL does not match expected pattern, skipping callback",
			slog.String("callbackURL", callbackURLStr),
			slog.String("expectedPath", constants.NarCallbackServicePath+"/{provisioningRequestName}"),
			slog.String("actualPath", callbackURL.Path))
		return
	}

	provisioningRequestName := strings.TrimPrefix(callbackURL.Path, constants.NarCallbackServicePath+"/")
	if provisioningRequestName == "" {
		r.Logger.WarnContext(ctx, "Could not extract provisioning request name from callback URL, skipping callback",
			slog.String("callbackURL", callbackURLStr))
		return
	}

	// Create base URL for the callback client (without the path)
	baseURL := fmt.Sprintf("%s://%s", callbackURL.Scheme, callbackURL.Host)
	if callbackURL.Port() != "" {
		baseURL = fmt.Sprintf("%s://%s:%s", callbackURL.Scheme, callbackURL.Hostname(), callbackURL.Port())
	}

	// Create a modified callback config with the base URL instead of the full URL
	callbackForClient := &pluginsv1alpha1.Callback{
		CallbackURL:      baseURL,
		CaBundleName:     callback.CaBundleName,
		AuthClientConfig: callback.AuthClientConfig,
	}

	narCallbackClient, err := narcallbackclient.NewNarCallbackClient(ctx, r.Client, r.Logger, callbackForClient)
	if err != nil {
		r.Logger.ErrorContext(ctx, "Unable to create NAR callback client",
			slog.String("baseURL", baseURL),
			slog.String("error", err.Error()))
		return
	}

	// Create callback payload using the generated types
	payload := narcallbackclient.CallbackPayload{
		NodeAllocationRequestId: nodeAllocationRequest.Name,
		Status:                  status,
		Timestamp:               time.Now().UTC(),
	}
	if errorMsg != "" {
		payload.Error = &errorMsg
	}

	// Execute retry logic asynchronously (doesn't block the controller)
	r.executeAsyncRetry(ctx, narCallbackClient, provisioningRequestName, payload, nodeAllocationRequest.Name, status)
}

// sendCallbackWithAsyncRetryRateLimited wraps the callback with rate limiting
func (r *NodeAllocationRequestReconciler) sendCallbackWithAsyncRetryRateLimited(ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest, status narcallbackclient.CallbackPayloadStatus, errorMsg string) {
	defer r.activeCallbacks.Done()

	// Acquire semaphore to limit concurrent callbacks
	select {
	case r.callbackSemaphore <- struct{}{}:
		// Successfully acquired semaphore slot
		defer func() { <-r.callbackSemaphore }() // Release when done
	case <-ctx.Done():
		// Context cancelled while waiting for semaphore
		r.Logger.WarnContext(ctx, "Context cancelled while waiting for callback semaphore",
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))
		return
	}

	// Execute the actual callback logic
	r.sendCallbackWithAsyncRetry(ctx, nodeAllocationRequest, status, errorMsg)
}

// executeAsyncRetry runs the callback retry logic with exponential backoff in a goroutine
func (r *NodeAllocationRequestReconciler) executeAsyncRetry(
	ctx context.Context,
	narCallbackClient *narcallbackclient.NarCallbackClient,
	provisioningRequestName string,
	payload narcallbackclient.CallbackPayload,
	nodeAllocationRequestName string,
	status narcallbackclient.CallbackPayloadStatus) {

	var lastErr error

	for attempt := 1; attempt <= maxCallbackRetries; attempt++ {
		// Create a context with timeout for this specific attempt
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		// Try to send the callback
		resp, err := narCallbackClient.Client.ProvisioningRequestCallback(attemptCtx, provisioningRequestName, payload)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt, err)
			r.Logger.WarnContext(ctx, "Callback attempt failed (async)",
				slog.Int("attempt", attempt),
				slog.Int("maxRetries", maxCallbackRetries),
				slog.String("provisioningRequest", provisioningRequestName),
				slog.String("nodeAllocationRequest", nodeAllocationRequestName),
				slog.String("error", err.Error()))

			if attempt < maxCallbackRetries {
				// Calculate exponential backoff duration
				backoffDuration := calculateBackoffDuration(attempt)
				r.Logger.InfoContext(ctx, "Retrying callback after backoff (async)",
					slog.Int("attempt", attempt+1),
					slog.Duration("backoff", backoffDuration))

				// Wait for backoff duration (safe since we're in a goroutine)
				select {
				case <-ctx.Done():
					r.Logger.WarnContext(ctx, "Context cancelled during async callback retry",
						slog.String("provisioningRequest", provisioningRequestName),
						slog.String("nodeAllocationRequest", nodeAllocationRequestName))
					return
				case <-time.After(backoffDuration):
					// Continue to next attempt
				}
				continue
			}
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("attempt %d: received non-success status code %d", attempt, resp.StatusCode)
			r.Logger.WarnContext(ctx, "Callback returned non-success status (async)",
				slog.Int("attempt", attempt),
				slog.Int("maxRetries", maxCallbackRetries),
				slog.String("provisioningRequest", provisioningRequestName),
				slog.String("nodeAllocationRequest", nodeAllocationRequestName),
				slog.Int("statusCode", resp.StatusCode))

			if attempt < maxCallbackRetries {
				// Calculate exponential backoff duration
				backoffDuration := calculateBackoffDuration(attempt)
				r.Logger.InfoContext(ctx, "Retrying callback after backoff (async)",
					slog.Int("attempt", attempt+1),
					slog.Duration("backoff", backoffDuration))

				// Wait for backoff duration (safe since we're in a goroutine)
				select {
				case <-ctx.Done():
					r.Logger.WarnContext(ctx, "Context cancelled during async callback retry",
						slog.String("provisioningRequest", provisioningRequestName),
						slog.String("nodeAllocationRequest", nodeAllocationRequestName))
					return
				case <-time.After(backoffDuration):
					// Continue to next attempt
				}
				continue
			}
			continue
		}

		// Success
		r.Logger.InfoContext(ctx, "Callback sent successfully (async)",
			slog.Int("attempt", attempt),
			slog.String("provisioningRequest", provisioningRequestName),
			slog.String("nodeAllocationRequest", nodeAllocationRequestName),
			slog.String("status", string(status)))
		return
	}

	// All attempts failed
	r.Logger.ErrorContext(ctx, "Callback failed after all async retry attempts",
		slog.Int("maxRetries", maxCallbackRetries),
		slog.String("provisioningRequest", provisioningRequestName),
		slog.String("nodeAllocationRequest", nodeAllocationRequestName),
		slog.String("status", string(status)),
		slog.String("lastError", lastErr.Error()))
}

// HandleNodeAllocationRequest processes the NodeAllocationRequest CR
func (r *NodeAllocationRequestReconciler) HandleNodeAllocationRequest(
	ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {
	result := hwmgrutils.DoNotRequeue()

	// Check for hardware timeout at the top level
	if timeoutExceeded, conditionType, err := r.checkHardwareTimeout(nodeAllocationRequest); err != nil {
		r.Logger.ErrorContext(ctx, "Failed to check hardware timeout",
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
			slog.String("error", err.Error()))
		return hwmgrutils.RequeueWithMediumInterval(), fmt.Errorf("failed to check hardware timeout: %w", err)
	} else if timeoutExceeded {
		var timeoutMessage string
		var startTimeStr string
		switch conditionType {
		case hwmgmtv1alpha1.Provisioned:
			timeoutMessage = "Hardware provisioning timed out"
			if nodeAllocationRequest.Status.HardwareOperationStartTime != nil {
				startTimeStr = nodeAllocationRequest.Status.HardwareOperationStartTime.Format(time.RFC3339)
			}
		case hwmgmtv1alpha1.Configured:
			timeoutMessage = "Hardware configuration timed out"
			if nodeAllocationRequest.Status.HardwareOperationStartTime != nil {
				startTimeStr = nodeAllocationRequest.Status.HardwareOperationStartTime.Format(time.RFC3339)
			}
		default:
			timeoutMessage = "Hardware operation timed out"
		}

		r.Logger.ErrorContext(ctx, "Timeout detected: "+timeoutMessage,
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
			slog.String("conditionType", string(conditionType)),
			slog.String("timeoutDuration", nodeAllocationRequest.Spec.HardwareProvisioningTimeout),
			slog.String("startTime", startTimeStr))

		if updateErr := r.updateConditionAndSendCallback(
			ctx, nodeAllocationRequest,
			conditionType, hwmgmtv1alpha1.TimedOut,
			metav1.ConditionFalse, timeoutMessage,
		); updateErr != nil {
			r.Logger.ErrorContext(ctx, "Failed to update status for timeout",
				slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
				slog.String("error", updateErr.Error()))
			return hwmgrutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest timeout %s: %w",
					nodeAllocationRequest.Name, updateErr)
		}

		// Update ObservedGeneration to mark this generation as processed
		// This prevents FSM from treating this as a spec change on next reconciliation
		if updateErr := hwmgrutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); updateErr != nil {
			r.Logger.ErrorContext(ctx, "Failed to update ObservedGeneration after timeout",
				slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
				slog.String("error", updateErr.Error()))
			// Don't return error, timeout condition is already set
		}

		if conditionType == hwmgmtv1alpha1.Configured {
			if err := clearConfigAnnotationForAllocatedNodes(ctx, r.Client, r.NoncachedClient, r.Logger, nodeAllocationRequest); err != nil {
				r.Logger.ErrorContext(ctx, "Failed to clear config in progress annotations after configuration timeout",
					slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
					slog.String("error", err.Error()))
				return hwmgrutils.RequeueWithMediumInterval(),
					fmt.Errorf("failed to clear config in progress annotations after configuration timeout %s: %w",
						nodeAllocationRequest.Name, err)
			}

			// Also clear BMH update annotations for configuration timeouts
			if err := clearBMHUpdateAnnotationsForNAR(ctx, r.Client, r.Logger, nodeAllocationRequest); err != nil {
				r.Logger.ErrorContext(ctx, "Failed to clear BMH update annotations after configuration timeout",
					slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
					slog.String("error", err.Error()))
				return hwmgrutils.RequeueWithMediumInterval(),
					fmt.Errorf("failed to clear BMH update annotations after configuration timeout %s: %w",
						nodeAllocationRequest.Name, err)
			}
		}

		return hwmgrutils.DoNotRequeue(), nil
	}

	if !controllerutil.ContainsFinalizer(nodeAllocationRequest, hwmgrutils.NodeAllocationRequestFinalizer) {
		r.Logger.InfoContext(ctx, "Adding finalizer to NodeAllocationRequest")
		if err := hwmgrutils.NodeAllocationRequestAddFinalizer(ctx, r.Client, nodeAllocationRequest); err != nil {
			return hwmgrutils.RequeueImmediately(), fmt.Errorf("failed to add finalizer to NodeAllocationRequest: %w", err)
		}
	}

	switch hwmgrutils.DetermineAction(ctx, r.Logger, nodeAllocationRequest) {
	case hwmgrutils.NodeAllocationRequestFSMCreate:
		return r.handleNewNodeAllocationRequestCreate(ctx, nodeAllocationRequest)
	case hwmgrutils.NodeAllocationRequestFSMProcessing:
		return r.handleNodeAllocationRequestProcessing(ctx, nodeAllocationRequest)
	case hwmgrutils.NodeAllocationRequestFSMSpecChanged:
		return r.handleNodeAllocationRequestSpecChanged(ctx, nodeAllocationRequest)
	case hwmgrutils.NodeAllocationRequestFSMNoop:
		// Nothing to do
		return result, nil
	}

	return result, nil
}

func (r *NodeAllocationRequestReconciler) handleNewNodeAllocationRequestCreate(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	if err := processNewNodeAllocationRequest(ctx, r.NoncachedClient, r.Logger, nodeAllocationRequest); err != nil {
		r.Logger.ErrorContext(ctx, "failed processNewNodeAllocationRequest", slog.String("error", err.Error()))
		conditionReason = hwmgmtv1alpha1.Failed
		conditionStatus = metav1.ConditionFalse
		message = "Creation request failed: " + err.Error()
	} else {
		conditionReason = hwmgmtv1alpha1.InProgress
		conditionStatus = metav1.ConditionFalse
		message = "Handling creation"
	}

	if err := r.updateConditionAndSendCallback(ctx, nodeAllocationRequest,
		conditionType, conditionReason, conditionStatus, message); err != nil {
		return hwmgrutils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}
	// Update the NodeAllocationRequest hwMgrPlugin status
	if err := hwmgrutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return hwmgrutils.DoNotRequeue(), nil
}

func (r *NodeAllocationRequestReconciler) handleNodeAllocationRequestSpecChanged(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	configuredCondition := meta.FindStatusCondition(
		nodeAllocationRequest.Status.Conditions,
		string(hwmgmtv1alpha1.Configured))

	// Set AwaitConfig condition when handling spec changes for retry scenarios
	// Only update when the condition exists and is in a state that allows retry:
	// - Status is True (completed configuration, now needs reconfiguration)
	// - Reason is TimedOut or Failed (terminal states that can be retried on spec change)
	// Do NOT set during initial provisioning when configuredCondition doesn't exist yet
	if configuredCondition != nil && (configuredCondition.Status == metav1.ConditionTrue ||
		configuredCondition.Reason == string(hwmgmtv1alpha1.TimedOut) ||
		configuredCondition.Reason == string(hwmgmtv1alpha1.Failed)) {
		if configuredCondition.Reason == string(hwmgmtv1alpha1.TimedOut) ||
			configuredCondition.Reason == string(hwmgmtv1alpha1.Failed) {
			r.Logger.InfoContext(ctx, "NodeAllocationRequest configuration is in terminal state, but config change detected - allowing retry",
				slog.String("reason", configuredCondition.Reason))
		}
		if result, err := setAwaitConfigCondition(ctx, r.Client, nodeAllocationRequest); err != nil {
			return result, err
		}
	}

	result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, r.Client, r.NoncachedClient, r.Logger, r.PluginNamespace, nodeAllocationRequest)

	if nodelist != nil {
		// Check if NAR already has a terminal condition (Failed or TimedOut) - if so, skip aggregation
		// to preserve the terminal status. These terminal states are detected at NAR level, not node level.
		configuredCondition := meta.FindStatusCondition(nodeAllocationRequest.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		if configuredCondition != nil &&
			(configuredCondition.Reason == string(hwmgmtv1alpha1.Failed) ||
				configuredCondition.Reason == string(hwmgmtv1alpha1.TimedOut)) {
			r.Logger.InfoContext(ctx, "Skipping status aggregation - NAR already has terminal condition",
				slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
				slog.String("reason", configuredCondition.Reason))

			// Update observedGeneration to acknowledge the spec change was processed
			// This prevents the FSM from re-triggering spec change handling
			if updateErr := hwmgrutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); updateErr != nil {
				r.Logger.ErrorContext(ctx, "Failed to update hwMgrPlugin observedGeneration Status",
					slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
					slog.String("error", updateErr.Error()))
				// Return error to trigger requeue
				return hwmgrutils.RequeueWithShortInterval(),
					fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", updateErr)
			}

			return result, err
		}

		status, reason, message := deriveNodeAllocationRequestStatusFromNodes(ctx, r.NoncachedClient, r.Logger, nodelist)

		if updateErr := r.updateConditionAndSendCallback(ctx, nodeAllocationRequest,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConditionReason(reason), status, message); updateErr != nil {

			r.Logger.ErrorContext(ctx, "Failed to update aggregated NodeAllocationRequest status",
				slog.String("NodeAllocationRequest", nodeAllocationRequest.Name),
				slog.String("error", updateErr.Error()))

			if err == nil {
				err = updateErr
			}
		}

		// Update observedGeneration when configuration reaches a terminal state (success, failure, or timeout).
		if status == metav1.ConditionTrue ||
			reason == string(hwmgmtv1alpha1.Failed) ||
			reason == string(hwmgmtv1alpha1.TimedOut) {
			if updateErr := hwmgrutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); updateErr != nil {
				r.Logger.ErrorContext(ctx, "Failed to update hwMgrPlugin observedGeneration Status",
					slog.String("nodeAllocationRequest", nodeAllocationRequest.Name),
					slog.String("error", updateErr.Error()))
				// Return error to trigger requeue
				return hwmgrutils.RequeueWithShortInterval(),
					fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", updateErr)
			}
		}
	}

	return result, err
}

func (r *NodeAllocationRequestReconciler) handleNodeAllocationRequestProcessing(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (ctrl.Result, error) {

	r.Logger.InfoContext(ctx, "Handling NodeAllocationRequest Processing")

	// New API: returns (ctrl.Result, full bool, error)
	res, full, err := checkNodeAllocationRequestProgress(
		ctx, r.Client, r.NoncachedClient, r.Logger, r.PluginNamespace, nodeAllocationRequest,
	)

	// If the checker asked for a specific requeue or returned an error, handle that first.
	if err != nil {
		reason := hwmgmtv1alpha1.Failed
		if typederrors.IsInputError(err) {
			reason = hwmgmtv1alpha1.InvalidInput
		}
		if updateErr := r.updateConditionAndSendCallback(
			ctx, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, reason,
			metav1.ConditionFalse, err.Error(),
		); updateErr != nil {
			return hwmgrutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w",
					nodeAllocationRequest.Name, updateErr)
		}
		// Bubble the original error (no forced requeue here; the caller can decide)
		return hwmgrutils.DoNotRequeue(),
			fmt.Errorf("failed to check NodeAllocationRequest progress %s: %w",
				nodeAllocationRequest.Name, err)
	}

	if res.Requeue || res.RequeueAfter > 0 {
		if res.RequeueAfter > 0 {
			r.Logger.InfoContext(ctx, "Progress detected; requeueing",
				slog.Duration("after", res.RequeueAfter))
		} else {
			r.Logger.InfoContext(ctx, "Progress detected; requeueing immediately")
		}

		if err := r.updateConditionAndSendCallback(
			ctx, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.InProgress,
			metav1.ConditionFalse, string(hwmgmtv1alpha1.AwaitConfig),
		); err != nil {
			return hwmgrutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w",
					nodeAllocationRequest.Name, err)
		}
		return res, nil
	}

	// No explicit requeue requested by the checker: decide based on "full".
	if full {
		r.Logger.InfoContext(ctx, "NodeAllocationRequest is fully allocated")
		if err := r.updateConditionAndSendCallback(
			ctx, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed,
			metav1.ConditionTrue, "Created",
		); err != nil {
			return hwmgrutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w",
					nodeAllocationRequest.Name, err)
		}
		return hwmgrutils.DoNotRequeue(), nil
	}

	// Not full yet and no specific backoff requested — keep it moving with a short retry.
	r.Logger.InfoContext(ctx, "NodeAllocationRequest processing in progress")
	if err := r.updateConditionAndSendCallback(
		ctx, nodeAllocationRequest,
		hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.InProgress,
		metav1.ConditionFalse, string(hwmgmtv1alpha1.AwaitConfig),
	); err != nil {
		return hwmgrutils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w",
				nodeAllocationRequest.Name, err)
	}
	return hwmgrutils.RequeueWithShortInterval(), nil
}

// checkHardwareTimeout returns (timedOut, whichCondition, err)
// It times out Day 0 (Provisioned) and Day 2 (Configured) operations using HardwareOperationStartTime.
// The active operation is determined from the conditions.
func (r *NodeAllocationRequestReconciler) checkHardwareTimeout(
	nar *pluginsv1alpha1.NodeAllocationRequest,
) (bool, hwmgmtv1alpha1.ConditionType, error) {

	// 1) Resolve timeout
	timeoutStr := nar.Spec.HardwareProvisioningTimeout
	if timeoutStr == "" {
		timeoutStr = ctlrutils.DefaultHardwareProvisioningTimeout.String()
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return false, hwmgmtv1alpha1.ConditionType(""),
			fmt.Errorf("invalid hardware provisioning timeout %q: %w", timeoutStr, err)
	}
	if timeout <= 0 {
		return false, hwmgmtv1alpha1.ConditionType(""),
			fmt.Errorf("hardware provisioning timeout must be > 0 (got %q)", timeoutStr)
	}

	// 2) Read conditions once
	prov := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
	cfg := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))

	// Consider "in progress" as anything not True; Reason may be empty or "InProgress".
	inProgress := func(c *metav1.Condition) bool {
		return c != nil && c.Status != metav1.ConditionTrue
	}

	now := time.Now()

	// 3) Phase precedence:
	// If provisioning is in-progress, that's the active phase (ignore config).
	if inProgress(prov) {
		if nar.Status.HardwareOperationStartTime == nil || nar.Status.HardwareOperationStartTime.Time.IsZero() {
			// Inconsistent state: provisioning says in-progress but no start time.
			r.Logger.WarnContext(context.Background(), "Provisioning in progress but no start time set",
				slog.String("nar", nar.Name),
				slog.String("provisionedStatus", string(prov.Status)),
				slog.String("provisionedReason", prov.Reason))
			return false, hwmgmtv1alpha1.ConditionType(""), nil
		}
		deadline := nar.Status.HardwareOperationStartTime.Time.Add(timeout)
		elapsed := now.Sub(nar.Status.HardwareOperationStartTime.Time)
		r.Logger.InfoContext(context.Background(), "Checking provisioning timeout",
			slog.String("nar", nar.Name),
			slog.Time("now", now),
			slog.Time("startTime", nar.Status.HardwareOperationStartTime.Time),
			slog.Time("deadline", deadline),
			slog.Duration("timeout", timeout),
			slog.Duration("elapsed", elapsed),
			slog.Bool("exceeded", !now.Before(deadline)))
		if !now.Before(deadline) { // inclusive: >=
			return true, hwmgmtv1alpha1.Provisioned, nil
		}
		return false, hwmgmtv1alpha1.ConditionType(""), nil
	}

	// If provisioning is complete (or not in-progress) and configuring is in-progress, use HardwareOperationStartTime.
	if inProgress(cfg) {
		// For Day 2 retry: if there's a spec change (ConfigTransactionId mismatch),
		// skip timeout checking as this is a new configuration attempt
		// Note: ObservedConfigTransactionId is 0 when not set, so we check for 0 or mismatch
		if nar.Spec.ConfigTransactionId != 0 &&
			(nar.Status.ObservedConfigTransactionId == 0 ||
				nar.Status.ObservedConfigTransactionId != nar.Spec.ConfigTransactionId) {
			r.Logger.InfoContext(context.Background(), "Configuration spec change detected, skipping timeout check for retry",
				slog.String("nar", nar.Name),
				slog.Int64("specConfigTransactionId", nar.Spec.ConfigTransactionId),
				slog.Int64("observedConfigTransactionId", nar.Status.ObservedConfigTransactionId))
			return false, hwmgmtv1alpha1.ConditionType(""), nil
		}

		if nar.Status.HardwareOperationStartTime == nil || nar.Status.HardwareOperationStartTime.Time.IsZero() {
			// Inconsistent state: configuring says in-progress but no start time.
			r.Logger.WarnContext(context.Background(), "Configuration in progress but no start time set",
				slog.String("nar", nar.Name),
				slog.String("configuredStatus", string(cfg.Status)),
				slog.String("configuredReason", cfg.Reason))
			return false, hwmgmtv1alpha1.ConditionType(""), nil
		}
		deadline := nar.Status.HardwareOperationStartTime.Time.Add(timeout)
		elapsed := now.Sub(nar.Status.HardwareOperationStartTime.Time)
		r.Logger.InfoContext(context.Background(), "Checking configuration timeout",
			slog.String("nar", nar.Name),
			slog.Time("now", now),
			slog.Time("startTime", nar.Status.HardwareOperationStartTime.Time),
			slog.Time("deadline", deadline),
			slog.Duration("timeout", timeout),
			slog.Duration("elapsed", elapsed),
			slog.Bool("exceeded", !now.Before(deadline)))
		if !now.Before(deadline) { // inclusive: >=
			return true, hwmgmtv1alpha1.Configured, nil
		}
		return false, hwmgmtv1alpha1.ConditionType(""), nil
	}

	// Neither phase is in progress → no timeout.
	return false, hwmgmtv1alpha1.ConditionType(""), nil
}

// handleNodeAllocationRequestDeletion processes the NodeAllocationRequest CR deletion
func (r *NodeAllocationRequestReconciler) handleNodeAllocationRequestDeletion(ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (bool, error) {

	r.Logger.InfoContext(ctx, "Finalizing NodeAllocationRequest")

	return releaseNodeAllocationRequest(ctx, r.Client, r.Logger, nodeAllocationRequest)
}
