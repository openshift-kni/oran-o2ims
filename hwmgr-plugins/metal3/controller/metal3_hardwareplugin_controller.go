/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

// Metal3PluginReconciler reconciles NodeAllocationRequest objects associated with the Metal3 H/W plugin
type Metal3PluginReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	indexerEnabled  bool
	PluginNamespace string
}

func (r *Metal3PluginReconciler) SetupIndexer(ctx context.Context) error {
	// Setup AllocatedNode CRD indexer. This field indexer allows us to query a list of AllocatedNode CRs, filtered by the spec.nodeAllocationRequest field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
	}

	if err := r.Manager.GetFieldIndexer().IndexField(ctx, &hwmgmtv1alpha1.AllocatedNode{}, hwpluginutils.AllocatedNodeSpecNodeAllocationRequestKey, nodeIndexFunc); err != nil {
		return fmt.Errorf("failed to setup node indexer: %w", err)
	}

	return nil
}

//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodeallocationrequests,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodeallocationrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodeallocationrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=allocatednodes,verbs=get;create;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=allocatednodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=allocatednodes/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareprofiles,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwaresettings,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwarecomponents,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostupdatepolicies,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=firmwareschemas,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch;delete

func (r *Metal3PluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// Add logging context with the NodeAllocationRequest name
	ctx = logging.AppendCtx(ctx, slog.String("NodeAllocationRequest", req.Name))

	if !r.indexerEnabled {
		if err := r.SetupIndexer(ctx); err != nil {
			return hwpluginutils.DoNotRequeue(), fmt.Errorf("failed to setup indexer: %w", err)
		}
		r.Logger.InfoContext(ctx, "NodeAllocationRequest field indexer initialized")
		r.indexerEnabled = true
	}

	// Fetch the nodeAllocationRequest, using non-caching client
	nodeAllocationRequest := &hwmgmtv1alpha1.NodeAllocationRequest{}
	if err := hwpluginutils.GetNodeAllocationRequest(ctx, r.NoncachedClient, req.NamespacedName, nodeAllocationRequest); err != nil {
		if errors.IsNotFound(err) {
			// The NodeAllocationRequest object has likely been deleted
			return hwpluginutils.DoNotRequeue(), nil
		}
		r.Logger.InfoContext(ctx, "Unable to fetch NodeAllocationRequest. Requeuing", slog.String("error", err.Error()))
		return hwpluginutils.RequeueWithShortInterval(), nil
	}

	// Add logging context with data from the CR
	ctx = logging.AppendCtx(ctx, slog.String("CloudID", nodeAllocationRequest.Spec.CloudID))
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", nodeAllocationRequest.ResourceVersion))

	r.Logger.InfoContext(ctx, "Reconciling NodeAllocationRequest")

	if nodeAllocationRequest.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "NodeAllocationRequest is being deleted")
		if controllerutil.ContainsFinalizer(nodeAllocationRequest, hwpluginutils.NodeAllocationRequestFinalizer) {
			completed, deleteErr := r.handleNodeAllocationRequestDeletion(ctx, nodeAllocationRequest)
			if deleteErr != nil {
				return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed HandleNodeAllocationRequestDeletion: %w", deleteErr)
			}

			if !completed {
				r.Logger.InfoContext(ctx, "Deletion handling in progress, requeueing")
				return hwpluginutils.RequeueWithShortInterval(), nil
			}

			if finalizerErr := hwpluginutils.NodeAllocationRequestRemoveFinalizer(ctx, r.Client, nodeAllocationRequest); finalizerErr != nil {
				r.Logger.InfoContext(ctx, "Failed to remove finalizer, requeueing", slog.String("error", finalizerErr.Error()))
				return hwpluginutils.RequeueWithShortInterval(), nil
			}

			r.Logger.InfoContext(ctx, "Deletion handling complete, finalizer removed")
			return hwpluginutils.DoNotRequeue(), nil
		}

		r.Logger.InfoContext(ctx, "No finalizer, deletion handling complete")
		return hwpluginutils.DoNotRequeue(), nil
	}

	// Handle NodeAllocationRequest
	result, err = r.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
	if err != nil {
		return result, fmt.Errorf("failed to handle NodeAllocationRequest: %w", err)
	}

	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Metal3PluginReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Create a label selector for filtering NodeAllocationRequests pertaining to the Metal3 HardwarePlugin
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
		},
	}

	// Create a predicate to filter NodeAllocationRequests with the specified metal3 H/W plugin label
	pred, err := predicate.LabelSelectorPredicate(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to create label selector predicate: %w", err)
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&hwmgmtv1alpha1.NodeAllocationRequest{}).
		WithEventFilter(pred).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	return nil
}

// HandleNodeAllocationRequest processes the NodeAllocationRequest CR
func (r *Metal3PluginReconciler) HandleNodeAllocationRequest(
	ctx context.Context, nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {
	result := hwpluginutils.DoNotRequeue()

	if !controllerutil.ContainsFinalizer(nodeAllocationRequest, hwpluginutils.NodeAllocationRequestFinalizer) {
		r.Logger.InfoContext(ctx, "Adding finalizer to NodeAllocationRequest")
		if err := hwpluginutils.NodeAllocationRequestAddFinalizer(ctx, r.Client, nodeAllocationRequest); err != nil {
			return hwpluginutils.RequeueImmediately(), fmt.Errorf("failed to add finalizer to NodeAllocationRequest: %w", err)
		}
	}

	switch hwpluginutils.DetermineAction(ctx, r.Logger, nodeAllocationRequest) {
	case hwpluginutils.NodeAllocationRequestFSMCreate:
		return r.handleNewNodeAllocationRequestCreate(ctx, nodeAllocationRequest)
	case hwpluginutils.NodeAllocationRequestFSMProcessing:
		return r.handleNodeAllocationRequestProcessing(ctx, nodeAllocationRequest)
	case hwpluginutils.NodeAllocationRequestFSMSpecChanged:
		return r.handleNodeAllocationRequestSpecChanged(ctx, nodeAllocationRequest)
	case hwpluginutils.NodeAllocationRequestFSMNoop:
		// Nothing to do
		return result, nil
	}

	return result, nil
}

func (r *Metal3PluginReconciler) handleNewNodeAllocationRequestCreate(
	ctx context.Context,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	if err := processNewNodeAllocationRequest(ctx, r.Client, r.Logger, nodeAllocationRequest); err != nil {
		r.Logger.ErrorContext(ctx, "failed processNewNodeAllocationRequest", slog.String("error", err.Error()))
		conditionReason = hwmgmtv1alpha1.Failed
		conditionStatus = metav1.ConditionFalse
		message = "Creation request failed: " + err.Error()
	} else {
		conditionReason = hwmgmtv1alpha1.InProgress
		conditionStatus = metav1.ConditionFalse
		message = "Handling creation"
	}

	if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
		conditionType, conditionReason, conditionStatus, message); err != nil {
		return hwpluginutils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodeAllocationRequest.Name, err)
	}
	// Update the NodeAllocationRequest hwMgrPlugin status
	if err := hwpluginutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); err != nil {
		return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return hwpluginutils.DoNotRequeue(), nil
}

func (r *Metal3PluginReconciler) handleNodeAllocationRequestSpecChanged(
	ctx context.Context,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	configuredCondition := meta.FindStatusCondition(
		nodeAllocationRequest.Status.Conditions,
		string(hwmgmtv1alpha1.Configured))
	// Set a default status that will be updated during the configuration process
	if configuredCondition == nil {
		if result, err := setAwaitConfigCondition(ctx, r.Client, nodeAllocationRequest); err != nil {
			return result, err
		}
	}

	result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, r.Client, r.NoncachedClient, r.Logger, r.PluginNamespace, nodeAllocationRequest)
	if nodelist != nil {
		status, reason, message := deriveNodeAllocationRequestStatusFromNodes(ctx, r.NoncachedClient, r.Logger, nodelist)

		if updateErr := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConditionReason(reason), status, message); updateErr != nil {

			r.Logger.ErrorContext(ctx, "Failed to update aggregated NodeAllocationRequest status",
				slog.String("NodeAllocationRequest", nodeAllocationRequest.Name),
				slog.String("error", updateErr.Error()))

			if err == nil {
				err = updateErr
			}
		}
		if status == metav1.ConditionTrue && reason == string(hwmgmtv1alpha1.ConfigApplied) {
			if err := hwpluginutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); err != nil {
				return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
			}
		}
	}

	return result, err
}

func (r *Metal3PluginReconciler) handleNodeAllocationRequestProcessing(
	ctx context.Context,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	var result ctrl.Result

	full, err := checkNodeAllocationRequestProgress(ctx, r.Client, r.NoncachedClient, r.Logger, r.PluginNamespace,
		nodeAllocationRequest)
	if err != nil {
		reason := hwmgmtv1alpha1.Failed
		if typederrors.IsInputError(err) {
			reason = hwmgmtv1alpha1.InvalidInput
		}
		if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest, hwmgmtv1alpha1.Provisioned,
			reason, metav1.ConditionFalse, err.Error()); err != nil {
			return hwpluginutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
		}
		return hwpluginutils.DoNotRequeue(), fmt.Errorf("failed to check NodeAllocationRequest progress %s: %w", nodeAllocationRequest.Name, err)
	}

	if full {
		r.Logger.InfoContext(ctx, "NodePool request is fully allocated")

		if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, "Created"); err != nil {
			return hwpluginutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodeAllocationRequest.Name, err)
		}
		result = hwpluginutils.DoNotRequeue()
	} else {
		r.Logger.InfoContext(ctx, "NodeAllocationRequest request in progress")
		if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.InProgress, metav1.ConditionFalse,
			string(hwmgmtv1alpha1.AwaitConfig)); err != nil {
			return hwpluginutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodeAllocationRequest.Name, err)
		}
		result = hwpluginutils.RequeueWithShortInterval()
	}

	return result, nil
}

// handleNodeAllocationRequestDeletion processes the NodeAllocationRequest CR deletion
func (r *Metal3PluginReconciler) handleNodeAllocationRequestDeletion(ctx context.Context, nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (bool, error) {

	r.Logger.InfoContext(ctx, "Finalizing NodeAllocationRequest")

	if err := releaseNodeAllocationRequest(ctx, r.Client, r.Logger, nodeAllocationRequest); err != nil {
		return false, fmt.Errorf("failed to release NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	return true, nil
}
