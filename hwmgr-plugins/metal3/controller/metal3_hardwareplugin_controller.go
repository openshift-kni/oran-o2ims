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

	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
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
}

func (r *Metal3PluginReconciler) SetupIndexer(ctx context.Context) error {
	// Setup AllocatedNode CRD indexer. This field indexer allows us to query a list of AllocatedNode CRs, filtered by the spec.nodeAllocationRequest field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*pluginv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
	}

	if err := r.Manager.GetFieldIndexer().IndexField(ctx, &pluginv1alpha1.AllocatedNode{}, hwpluginutils.AllocatedNodeSpecNodeAllocationRequestKey, nodeIndexFunc); err != nil {
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
	nodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
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
		For(&pluginv1alpha1.NodeAllocationRequest{}).
		WithEventFilter(pred).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	return nil
}

// HandleNodeAllocationRequest processes the NodeAllocationRequest CR
func (r *Metal3PluginReconciler) HandleNodeAllocationRequest(
	ctx context.Context, nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {
	result := hwpluginutils.DoNotRequeue()

	if !controllerutil.ContainsFinalizer(nodeAllocationRequest, hwpluginutils.NodeAllocationRequestFinalizer) {
		r.Logger.InfoContext(ctx, "Adding finalizer to NodeAllocationRequest")
		if err := hwpluginutils.NodeAllocationRequestAddFinalizer(ctx, r.Client, nodeAllocationRequest); err != nil {
			return hwpluginutils.RequeueImmediately(), fmt.Errorf("failed to add finalizer to NodeAllocationRequest: %w", err)
		}
	}

	// TODO

	return result, nil
}

// handleNodeAllocationRequestDeletion processes the NodeAllocationRequest CR deletion
func (r *Metal3PluginReconciler) handleNodeAllocationRequestDeletion(ctx context.Context, nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) (bool, error) {

	r.Logger.InfoContext(ctx, "Finalizing NodeAllocationRequest")

	//  TODO: remove this conditional which is added to to appease the linter Gods
	if nodeAllocationRequest.Name == "" {
		return false, fmt.Errorf("nodeAllocationRequest.name is empty: resource version: %s", nodeAllocationRequest.ResourceVersion)
	}

	// TODO

	return true, nil
}
