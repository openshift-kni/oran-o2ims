/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// Label constants for BareMetalHost resources
const (
	BMHLabelResourcePoolID = "resources.clcm.openshift.io/resourcePoolId"
)

// ResourcePoolReconciler reconciles a ResourcePool object
type ResourcePoolReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools/finalizers,verbs=update
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites,verbs=get;list;watch
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ResourcePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = doNotRequeue()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ResourcePool")

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

	// Fetch the ResourcePool object
	pool := &inventoryv1alpha1.ResourcePool{}
	if err = r.Client.Get(ctx, req.NamespacedName, pool); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "ResourcePool not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch ResourcePool", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, pool)
	r.Logger.InfoContext(ctx, "Fetched ResourcePool successfully",
		slog.String("resourcePoolId", pool.Spec.ResourcePoolId),
		slog.String("oCloudSiteId", pool.Spec.OCloudSiteId))

	// Handle finalizer logic
	if result, stop, err := r.handleFinalizer(ctx, pool); stop || err != nil {
		return result, err
	}

	// Validate OCloudSite reference and set Ready condition if not being deleted
	if pool.DeletionTimestamp.IsZero() {
		siteExists, err := r.validateAndSetConditions(ctx, pool)
		if err != nil {
			return requeueWithShortInterval(), err
		}
		// Requeue if site doesn't exist - it may be created later
		if !siteExists {
			return requeueWithMediumInterval(), nil
		}
	}

	return result, nil
}

// handleFinalizer manages the finalizer for ResourcePool CRs
func (r *ResourcePoolReconciler) handleFinalizer(
	ctx context.Context, pool *inventoryv1alpha1.ResourcePool) (ctrl.Result, bool, error) {

	// Check if the ResourcePool is marked to be deleted
	if pool.DeletionTimestamp.IsZero() {
		// Object is not being deleted, add finalizer if not present
		if !controllerutil.ContainsFinalizer(pool, inventoryv1alpha1.ResourcePoolFinalizer) {
			r.Logger.InfoContext(ctx, "Adding finalizer to ResourcePool",
				slog.String("resourcePoolId", pool.Spec.ResourcePoolId))
			controllerutil.AddFinalizer(pool, inventoryv1alpha1.ResourcePoolFinalizer)
			if err := r.Update(ctx, pool); err != nil {
				r.Logger.WarnContext(ctx, "Failed to add finalizer, will retry",
					slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to add finalizer: %w", err)
			}
			// Requeue since the finalizer has been added
			return requeueImmediately(), false, nil
		}
		return doNotRequeue(), false, nil
	}

	// Object is being deleted
	if controllerutil.ContainsFinalizer(pool, inventoryv1alpha1.ResourcePoolFinalizer) {
		r.Logger.InfoContext(ctx, "ResourcePool is being deleted, checking for dependents",
			slog.String("resourcePoolId", pool.Spec.ResourcePoolId))

		// Check for dependent BareMetalHosts
		bmhDependents, err := r.findDependentBMHs(ctx, pool.Spec.ResourcePoolId)
		if err != nil {
			return requeueWithShortInterval(), true, fmt.Errorf("failed to check for dependent BareMetalHosts: %w", err)
		}

		if len(bmhDependents) > 0 {
			// Update status to indicate deletion is blocked
			r.Logger.InfoContext(ctx, "ResourcePool deletion blocked by dependent BareMetalHosts",
				slog.String("resourcePoolId", pool.Spec.ResourcePoolId),
				slog.Int("bmhCount", len(bmhDependents)))

			if err := r.setDeletionBlockedCondition(ctx, pool, len(bmhDependents)); err != nil {
				r.Logger.WarnContext(ctx, "Failed to update status", slog.String("error", err.Error()))
			}

			// Requeue to check again later
			return requeueWithCustomInterval(10 * time.Second), true, nil
		}

		// No dependents, safe to remove finalizer and allow k8s deletion
		r.Logger.InfoContext(ctx, "No dependents found, removing finalizer from ResourcePool",
			slog.String("resourcePoolId", pool.Spec.ResourcePoolId))

		patch := client.MergeFrom(pool.DeepCopy())
		if controllerutil.RemoveFinalizer(pool, inventoryv1alpha1.ResourcePoolFinalizer) {
			if err := r.Patch(ctx, pool, patch); err != nil {
				r.Logger.WarnContext(ctx, "Failed to remove finalizer, will retry",
					slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}
		return doNotRequeue(), true, nil
	}

	return doNotRequeue(), false, nil
}

// findDependentBMHs returns all BareMetalHosts that have the resourcePoolId label matching this ResourcePool
func (r *ResourcePoolReconciler) findDependentBMHs(ctx context.Context, resourcePoolID string) ([]bmhv1alpha1.BareMetalHost, error) {
	var bmhList bmhv1alpha1.BareMetalHostList
	if err := r.List(ctx, &bmhList, client.MatchingLabels{BMHLabelResourcePoolID: resourcePoolID}); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	return bmhList.Items, nil
}

// validateAndSetConditions validates the OCloudSite reference and sets appropriate conditions.
// Returns (siteExists, error) so caller can decide whether to requeue.
func (r *ResourcePoolReconciler) validateAndSetConditions(ctx context.Context, pool *inventoryv1alpha1.ResourcePool) (bool, error) {
	// Validate that the referenced OCloudSite exists
	siteExists, err := r.validateSiteReference(ctx, pool.Spec.OCloudSiteId)
	if err != nil {
		return false, fmt.Errorf("failed to validate OCloudSite reference: %w", err)
	}

	var condition metav1.Condition
	if !siteExists {
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonInvalidReference,
			Message:            fmt.Sprintf("Referenced OCloudSite with siteId '%s' does not exist", pool.Spec.OCloudSiteId),
			ObservedGeneration: pool.Generation,
		}
	} else {
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             inventoryv1alpha1.ReasonReady,
			Message:            "ResourcePool is ready",
			ObservedGeneration: pool.Generation,
		}
	}

	// Only update if the condition has changed
	existingCondition := meta.FindStatusCondition(pool.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	if existingCondition == nil || existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason {
		meta.SetStatusCondition(&pool.Status.Conditions, condition)
		if err := r.Status().Update(ctx, pool); err != nil {
			return siteExists, fmt.Errorf("failed to update status: %w", err)
		}
	}

	return siteExists, nil
}

// validateSiteReference checks if an OCloudSite with the given siteId exists
func (r *ResourcePoolReconciler) validateSiteReference(ctx context.Context, siteID string) (bool, error) {
	var siteList inventoryv1alpha1.OCloudSiteList
	if err := r.List(ctx, &siteList); err != nil {
		return false, fmt.Errorf("failed to list OCloudSites: %w", err)
	}

	for _, site := range siteList.Items {
		if site.Spec.SiteID == siteID {
			return true, nil
		}
	}

	return false, nil
}

// setDeletionBlockedCondition sets the Deleting condition indicating deletion is blocked
func (r *ResourcePoolReconciler) setDeletionBlockedCondition(ctx context.Context, pool *inventoryv1alpha1.ResourcePool, bmhCount int) error {
	condition := metav1.Condition{
		Type:               inventoryv1alpha1.ConditionTypeDeleting,
		Status:             metav1.ConditionFalse,
		Reason:             inventoryv1alpha1.ReasonDependentsExist,
		Message:            fmt.Sprintf("Cannot delete: %d BareMetalHost(s) reference this ResourcePool", bmhCount),
		ObservedGeneration: pool.Generation,
	}

	meta.SetStatusCondition(&pool.Status.Conditions, condition)
	if err := r.Status().Update(ctx, pool); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourcePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&inventoryv1alpha1.ResourcePool{}).
		// Watch OCloudSite changes to re-reconcile ResourcePools that reference them
		Watches(
			&inventoryv1alpha1.OCloudSite{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueResourcePoolsForOCloudSite),
		).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup resourcepool controller: %w", err)
	}
	return nil
}

// enqueueResourcePoolsForOCloudSite maps OCloudSite changes to ResourcePools that reference them.
func (r *ResourcePoolReconciler) enqueueResourcePoolsForOCloudSite(ctx context.Context, obj client.Object) []reconcile.Request {
	site, ok := obj.(*inventoryv1alpha1.OCloudSite)
	if !ok {
		return nil
	}

	// Find all ResourcePools that reference this OCloudSite's siteId
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := r.List(ctx, &poolList); err != nil {
		r.Logger.ErrorContext(ctx, "Failed to list ResourcePools for OCloudSite watch",
			slog.String("error", err.Error()))
		return nil
	}

	var requests []reconcile.Request
	for _, pool := range poolList.Items {
		if pool.Spec.OCloudSiteId == site.Spec.SiteID {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&pool),
			})
		}
	}

	if len(requests) > 0 {
		r.Logger.InfoContext(ctx, "OCloudSite change triggering ResourcePool reconciliation",
			slog.String("siteId", site.Spec.SiteID),
			slog.Int("resourcePoolCount", len(requests)))
	}

	return requests
}
