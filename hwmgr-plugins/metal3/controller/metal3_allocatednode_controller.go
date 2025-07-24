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

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
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

	// Add logging context with the resource name
	ctx = logging.AppendCtx(ctx, slog.String("ReconcileRequest", req.Name))

	allocatedNode, err := hwpluginutils.GetNode(ctx, r.Logger, r.NoncachedClient, req.Namespace, req.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			// The AllocatedNode object has likely been deleted
			r.Logger.InfoContext(ctx, "Node no longer exists.")
			return hwpluginutils.DoNotRequeue(), nil
		}
		r.Logger.InfoContext(ctx, "Unable to fetch AllocatedNode. Requeuing", slog.String("error", err.Error()))
		return hwpluginutils.RequeueWithShortInterval(), nil
	}

	// Add logging context with data from the CR
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", allocatedNode.ResourceVersion))

	r.Logger.InfoContext(ctx, "Reconciling AllocatedNode")

	if allocatedNode.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "AllocatedNode is being deleted")
		if controllerutil.ContainsFinalizer(allocatedNode, hwpluginutils.AllocatedNodeFinalizer) {
			completed, deleteErr := r.handleAllocatedNodeDeletion(ctx, allocatedNode)
			if deleteErr != nil {
				return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed CleanupForDeletedNode: %w", deleteErr)
			}

			if !completed {
				r.Logger.InfoContext(ctx, "Node deletion handling in progress, requeueing")
				return hwpluginutils.RequeueWithShortInterval(), nil
			}

			if finalizerErr := hwpluginutils.AllocatedNodeRemoveFinalizer(ctx, r.NoncachedClient, r.Client, allocatedNode); finalizerErr != nil {
				r.Logger.InfoContext(ctx, "Failed to remove finalizer, requeueing", slog.String("error", finalizerErr.Error()))
				return hwpluginutils.RequeueWithShortInterval(), nil
			}

			r.Logger.InfoContext(ctx, "Deletion handling complete, finalizer removed")
			return hwpluginutils.DoNotRequeue(), nil
		}

		r.Logger.InfoContext(ctx, "No finalizer, deletion handling complete")
		return hwpluginutils.DoNotRequeue(), nil
	}

	if !controllerutil.ContainsFinalizer(allocatedNode, hwpluginutils.AllocatedNodeFinalizer) {
		if finalizerErr := hwpluginutils.AllocatedNodeAddFinalizer(ctx, r.NoncachedClient, r.Client, allocatedNode); finalizerErr != nil {
			r.Logger.InfoContext(ctx, "Failed to add node finalizer, requeueing", slog.String("error", finalizerErr.Error()))
			return hwpluginutils.RequeueWithShortInterval(), nil
		}
	}

	return hwpluginutils.DoNotRequeue(), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AllocatedNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Create a label selector for filtering AllocatedNode pertaining to the Metal3 HardwarePlugin
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
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
	bmh, err := getBMHForNode(ctx, r.Client, allocatednode)
	if err != nil {
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
		if err := patchOnlineFalse(ctx, r.Client, bmh); err != nil {
			return false, fmt.Errorf("failed to patchOnlineFalse for BMH %s: %w", bmh.Name, err)
		}
	}

	return true, clearBMHDeallocationAnnotation(ctx, r.Client, r.Logger, bmh)
}
