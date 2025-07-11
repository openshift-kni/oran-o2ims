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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

// LoopbackPluginReconciler reconciles NodeAllocationRequest objects associated with the loopback H/W plugin
type LoopbackPluginReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	indexerEnabled  bool
}

func (r *LoopbackPluginReconciler) SetupIndexer(ctx context.Context) error {
	// Setup AllocatedNode CRD indexer. This field indexer allows us to query a list of AllocatedNode CRs, filtered by the spec.nodeAllocationRequest field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*pluginsv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
	}

	if err := r.Manager.GetFieldIndexer().IndexField(ctx, &pluginsv1alpha1.AllocatedNode{}, hwpluginutils.AllocatedNodeSpecNodeAllocationRequestKey, nodeIndexFunc); err != nil {
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
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch;delete

func (r *LoopbackPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
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
	nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{}
	if err := hwpluginutils.GetNodeAllocationRequest(ctx, r.NoncachedClient, req.NamespacedName, nodeAllocationRequest); err != nil {
		if errors.IsNotFound(err) {
			// The NodeAllocationRequest object has likely been deleted
			return hwpluginutils.DoNotRequeue(), nil
		}
		r.Logger.InfoContext(ctx, "Unable to fetch NodeAllocationRequest. Requeuing", slog.String("error", err.Error()))
		return hwpluginutils.RequeueWithShortInterval(), nil
	}

	// Add logging context with data from the CR
	ctx = logging.AppendCtx(ctx, slog.String("ClusterID", nodeAllocationRequest.Spec.ClusterId))
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
func (r *LoopbackPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Create a label selector for filtering NodeAllocationRequests pertaining to the Loopback HardwarePlugin
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			hwpluginutils.HardwarePluginLabel: hwpluginutils.LoopbackHardwarePluginID,
		},
	}

	// Create a predicate to filter NodeAllocationRequests with the specified label
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

// HandleNodeAllocationRequest processes the NodeAllocationRequest CR
func (r *LoopbackPluginReconciler) HandleNodeAllocationRequest(
	ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {
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

func (r *LoopbackPluginReconciler) handleNewNodeAllocationRequestCreate(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	if err := processNewNodeAllocationRequest(ctx, r.Client, r.Logger, nodeAllocationRequest); err != nil {
		r.Logger.InfoContext(ctx, "failed processNewNodeAllocationRequest", slog.String("err", err.Error()))
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
			fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}
	// Update the NodeAllocationRequest hwMgrPlugin status
	if err := hwpluginutils.UpdateNodeAllocationRequestPluginStatus(ctx, r.Client, nodeAllocationRequest); err != nil {
		return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return hwpluginutils.DoNotRequeue(), nil
}

func (r *LoopbackPluginReconciler) handleNodeAllocationRequestProcessing(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (ctrl.Result, error) {

	full, err := checkNodeAllocationRequestProgress(ctx, r.Client, r.Logger, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed checkNodeAllocationRequestProgress: %w", err)
	}

	allocatedNodes, err := getAllocatedNodes(ctx, r.Client, r.Logger, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for %s: %w", nodeAllocationRequest.Name, err)
	}
	nodeAllocationRequest.Status.Properties.NodeNames = allocatedNodes

	if err := hwpluginutils.UpdateNodeAllocationRequestProperties(ctx, r.Client, nodeAllocationRequest); err != nil {
		return hwpluginutils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	var result ctrl.Result

	if full {
		r.Logger.InfoContext(ctx, "NodeAllocationRequest request is fully allocated")

		if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, r.Client, nodeAllocationRequest,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, "Created"); err != nil {
			return hwpluginutils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
		}

		result = hwpluginutils.DoNotRequeue()
	} else {
		r.Logger.InfoContext(ctx, "NodeAllocationRequest request in progress")
		result = hwpluginutils.RequeueWithShortInterval()
	}

	return result, nil
}

func (r *LoopbackPluginReconciler) handleNodeAllocationRequestSpecChanged(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(
		ctx,
		r.Client,
		nodeAllocationRequest,
		hwmgmtv1alpha1.Configured,
		hwmgmtv1alpha1.ConfigUpdate,
		metav1.ConditionFalse,
		string(hwmgmtv1alpha1.AwaitConfig)); err != nil {
		return hwpluginutils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	return handleNodeAllocationRequestConfiguring(ctx, r.Client, r.Logger, nodeAllocationRequest)
}

// handleNodeAllocationRequestDeletion processes the NodeAllocationRequest CR deletion
func (r *LoopbackPluginReconciler) handleNodeAllocationRequestDeletion(ctx context.Context, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (bool, error) {
	r.Logger.InfoContext(ctx, "Finalizing NodeAllocationRequest")

	if err := releaseNodeAllocationRequest(ctx, r.Client, r.Logger, nodeAllocationRequest); err != nil {
		return false, fmt.Errorf("failed to release NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	return true, nil
}
