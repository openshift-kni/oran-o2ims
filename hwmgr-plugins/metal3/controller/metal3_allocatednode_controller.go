/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

// AllocatedNodeReconciler reconciles NodeAllocationRequest objects associated with the Metal3 H/W plugin
type AllocatedNodeReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	PluginNamespace string
}

// +kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes,verbs=get;create;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=plugins.clcm.openshift.io,resources=allocatednodes/finalizers,verbs=update
func (r *AllocatedNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "AllocatedNode")

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

	// Add logging context with the resource name
	ctx = logging.AppendCtx(ctx, slog.String("ReconcileRequest", req.Name))

	allocatedNode, err := hwmgrutils.GetNode(ctx, r.Logger, r.NoncachedClient, req.Namespace, req.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "AllocatedNode not found, assuming deleted")
			return hwmgrutils.DoNotRequeue(), nil
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch AllocatedNode", err)
		return hwmgrutils.RequeueWithShortInterval(), nil
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, allocatedNode)
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", allocatedNode.ResourceVersion))
	r.Logger.InfoContext(ctx, "Fetched AllocatedNode successfully")

	if allocatedNode.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "AllocatedNode is being deleted")
		if controllerutil.ContainsFinalizer(allocatedNode, hwmgrutils.AllocatedNodeFinalizer) {
			completed, deleteErr := r.handleAllocatedNodeDeletion(ctx, allocatedNode)
			if deleteErr != nil {
				return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed CleanupForDeletedNode: %w", deleteErr)
			}

			if !completed {
				r.Logger.InfoContext(ctx, "Node deletion handling in progress, requeueing")
				return hwmgrutils.RequeueWithShortInterval(), nil
			}

			if finalizerErr := hwmgrutils.AllocatedNodeRemoveFinalizer(ctx, r.NoncachedClient, r.Client, allocatedNode); finalizerErr != nil {
				r.Logger.InfoContext(ctx, "Failed to remove finalizer, requeueing", slog.String("error", finalizerErr.Error()))
				return hwmgrutils.RequeueWithShortInterval(), nil
			}

			r.Logger.InfoContext(ctx, "Deletion handling complete, finalizer removed")
			return hwmgrutils.DoNotRequeue(), nil
		}

		r.Logger.InfoContext(ctx, "No finalizer, deletion handling complete")
		return hwmgrutils.DoNotRequeue(), nil
	}

	if !controllerutil.ContainsFinalizer(allocatedNode, hwmgrutils.AllocatedNodeFinalizer) {
		if finalizerErr := hwmgrutils.AllocatedNodeAddFinalizer(ctx, r.NoncachedClient, r.Client, allocatedNode); finalizerErr != nil {
			r.Logger.InfoContext(ctx, "Failed to add node finalizer, requeueing", slog.String("error", finalizerErr.Error()))
			return hwmgrutils.RequeueWithShortInterval(), nil
		}
	}

	return hwmgrutils.DoNotRequeue(), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AllocatedNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Create a label selector for filtering AllocatedNode pertaining to the Metal3 HardwarePlugin
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			hwmgrutils.HardwarePluginLabel: hwmgrutils.Metal3HardwarePluginID,
		},
	}

	// Create a predicate to filter AllocatedNode with the specified metal3 H/W plugin label
	pred, err := predicate.LabelSelectorPredicate(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to create label selector predicate: %w", err)
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pluginsv1alpha1.AllocatedNode{}).
		WithEventFilter(pred).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create allocated node controller: %w", err)
	}

	return nil
}

// CleanupForDeletedNode
func (r *AllocatedNodeReconciler) handleAllocatedNodeDeletion(ctx context.Context, allocatednode *pluginsv1alpha1.AllocatedNode) (bool, error) {

	r.Logger.InfoContext(ctx, "handleAllocatedNodeDeletion", slog.String("node", allocatednode.Name))
	bmh, err := getBMHForNode(ctx, r.NoncachedClient, allocatednode)
	if err != nil {
		// If BMH is not found (e.g., manually deleted), allow the AllocatedNode deletion to proceed
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "BMH not found, assuming manually deleted — proceeding with AllocatedNode deletion",
				slog.String("node", allocatednode.Name),
				slog.String("bmh", allocatednode.Spec.HwMgrNodeId))
			return true, nil
		}
		return true, fmt.Errorf("failed to get BMH for node %s: %w", allocatednode.Name, err)
	}

	if !isBMHDeallocated(bmh) {
		if err = deallocateBMH(ctx, r.Client, r.Logger, bmh); err != nil {
			return false, fmt.Errorf("failed to deallocate BMH: %w", err)
		}
		return false, nil
	}

	if isNodeProvisioningInProgress(allocatednode) {
		// Wait for BMH to transition to Available before powering off
		if bmh.Status.Provisioning.State != metal3v1alpha1.StateAvailable {
			r.Logger.InfoContext(ctx, "BMH not yet Available — waiting before powering off", slog.String("bmh", bmh.Name))
			return false, nil
		}
	}

	if bmh.Spec.Online {
		// Skip power-off if skip-cleanup is requested
		if _, present := bmh.Annotations[SkipCleanupAnnotation]; !present {
			if err := patchOnlineFalse(ctx, r.Client, bmh); err != nil {
				return false, fmt.Errorf("failed to patchOnlineFalse for BMH %s: %w", bmh.Name, err)
			}
		}
	}

	return true, clearBMHAnnotation(ctx, r.Client, r.Logger, bmh)
}
