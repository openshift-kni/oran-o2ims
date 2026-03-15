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

// OCloudSiteReconciler reconciles an OCloudSite object
type OCloudSiteReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites/finalizers,verbs=update
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations,verbs=get;list;watch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OCloudSiteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = doNotRequeue()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "OCloudSite")

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

	// Fetch the OCloudSite object
	site := &inventoryv1alpha1.OCloudSite{}
	if err = r.Client.Get(ctx, req.NamespacedName, site); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "OCloudSite not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch OCloudSite", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, site)
	r.Logger.InfoContext(ctx, "Fetched OCloudSite successfully",
		slog.String("name", site.Name),
		slog.String("globalLocationName", site.Spec.GlobalLocationName))

	// Handle finalizer logic
	if result, stop, err := r.handleFinalizer(ctx, site); stop || err != nil {
		return result, err
	}

	// Validate Location reference and set Ready condition if not being deleted
	if site.DeletionTimestamp.IsZero() {
		parentValid, err := r.validateAndSetConditions(ctx, site)
		if err != nil {
			return requeueWithShortInterval(), err
		}
		// Requeue if parent doesn't exist or is not ready, it may be created/ready later
		if !parentValid {
			return requeueWithMediumInterval(), nil
		}
	}

	return result, nil
}

// handleFinalizer manages the finalizer for OCloudSite CRs
func (r *OCloudSiteReconciler) handleFinalizer(
	ctx context.Context, site *inventoryv1alpha1.OCloudSite) (ctrl.Result, bool, error) {

	// Check if the OCloudSite is marked to be deleted
	if site.DeletionTimestamp.IsZero() {
		// Object is not being deleted, add finalizer if not present
		if !controllerutil.ContainsFinalizer(site, inventoryv1alpha1.OCloudSiteFinalizer) {
			r.Logger.InfoContext(ctx, "Adding finalizer to OCloudSite",
				slog.String("name", site.Name))
			controllerutil.AddFinalizer(site, inventoryv1alpha1.OCloudSiteFinalizer)
			if err := r.Update(ctx, site); err != nil {
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
	if controllerutil.ContainsFinalizer(site, inventoryv1alpha1.OCloudSiteFinalizer) {
		r.Logger.InfoContext(ctx, "OCloudSite is being deleted, checking for dependents",
			slog.String("name", site.Name))

		// Check for dependent ResourcePools (those that reference this OCloudSite by name)
		// Note: ResourcePools handle their own BMH dependents transitively
		dependents, err := r.findDependentResourcePools(ctx, site.Name)
		if err != nil {
			return requeueWithShortInterval(), true, fmt.Errorf("failed to check for dependent ResourcePools: %w", err)
		}

		// dependents exist, block deletion
		if len(dependents) > 0 {
			// Update status to indicate deletion is blocked
			r.Logger.InfoContext(ctx, "OCloudSite deletion blocked by dependent ResourcePools",
				slog.String("name", site.Name),
				slog.Int("resourcePoolCount", len(dependents)))

			if err := r.setDeletionBlockedCondition(ctx, site, len(dependents)); err != nil {
				r.Logger.WarnContext(ctx, "Failed to update status", slog.String("error", err.Error()))
			}

			// Requeue to check again later
			return requeueWithCustomInterval(10 * time.Second), true, nil
		}

		// No dependents, safe to remove finalizer and allow k8s deletion
		r.Logger.InfoContext(ctx, "Removing finalizer from OCloudSite",
			slog.String("name", site.Name))

		patch := client.MergeFrom(site.DeepCopy())
		if controllerutil.RemoveFinalizer(site, inventoryv1alpha1.OCloudSiteFinalizer) {
			if err := r.Patch(ctx, site, patch); err != nil {
				r.Logger.WarnContext(ctx, "Failed to remove finalizer, will retry",
					slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}
		return doNotRequeue(), true, nil
	}

	return doNotRequeue(), false, nil
}

// findDependentResourcePools returns all ResourcePools that reference this OCloudSite by name.
func (r *OCloudSiteReconciler) findDependentResourcePools(ctx context.Context, siteName string) ([]inventoryv1alpha1.ResourcePool, error) {
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := r.List(ctx, &poolList, client.MatchingFields{
		ctlrutils.ResourcePoolOCloudSiteNameIndex: siteName,
	}); err != nil {
		return nil, fmt.Errorf("failed to list ResourcePools: %w", err)
	}

	return poolList.Items, nil
}

// validateAndSetConditions validates the Location reference and sets appropriate conditions.
// Returns (parentValid, error) where parentValid is true only when parent exists AND is ready.
func (r *OCloudSiteReconciler) validateAndSetConditions(ctx context.Context, site *inventoryv1alpha1.OCloudSite) (bool, error) {
	// Validate that the referenced Location exists and is ready (lookup by name)
	result, err := r.validateLocationReference(ctx, site.Spec.GlobalLocationName, site.Namespace)
	if err != nil {
		return false, fmt.Errorf("failed to validate Location reference: %w", err)
	}

	var condition metav1.Condition
	switch {
	case !result.Exists:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonParentNotFound,
			Message:            fmt.Sprintf("Referenced Location '%s' does not exist", site.Spec.GlobalLocationName),
			ObservedGeneration: site.Generation,
		}
	case !result.Ready:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonParentNotReady,
			Message:            fmt.Sprintf("Referenced Location '%s' is not ready", site.Spec.GlobalLocationName),
			ObservedGeneration: site.Generation,
		}
	default:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             inventoryv1alpha1.ReasonReady,
			Message:            "OCloudSite is ready",
			ObservedGeneration: site.Generation,
		}
	}

	// Only update if the condition has changed
	existingCondition := meta.FindStatusCondition(site.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	if existingCondition == nil || existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason {
		meta.SetStatusCondition(&site.Status.Conditions, condition)
		if err := r.Status().Update(ctx, site); err != nil {
			return result.Exists && result.Ready, fmt.Errorf("failed to update status: %w", err)
		}
	}

	// Return true only when parent exists AND is ready
	return result.Exists && result.Ready, nil
}

// validateLocationReference checks if a Location with the given name exists and is ready.
func (r *OCloudSiteReconciler) validateLocationReference(ctx context.Context, locationName, namespace string) (ctlrutils.ParentValidationResult, error) {
	location := &inventoryv1alpha1.Location{}
	if err := r.Get(ctx, client.ObjectKey{Name: locationName, Namespace: namespace}, location); err != nil {
		if errors.IsNotFound(err) {
			return ctlrutils.ParentValidationResult{Exists: false, Ready: false}, nil
		}
		return ctlrutils.ParentValidationResult{}, fmt.Errorf("failed to get Location: %w", err)
	}

	// Location exists, check if it's ready
	readyCondition := meta.FindStatusCondition(location.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		return ctlrutils.ParentValidationResult{Exists: true, Ready: true}, nil
	}

	// Exists but not ready
	return ctlrutils.ParentValidationResult{Exists: true, Ready: false}, nil
}

// setDeletionBlockedCondition sets the Deleting condition indicating deletion is blocked
func (r *OCloudSiteReconciler) setDeletionBlockedCondition(ctx context.Context, site *inventoryv1alpha1.OCloudSite, dependentCount int) error {
	condition := metav1.Condition{
		Type:               inventoryv1alpha1.ConditionTypeDeleting,
		Status:             metav1.ConditionFalse,
		Reason:             inventoryv1alpha1.ReasonDependentsExist,
		Message:            fmt.Sprintf("Cannot delete: %d ResourcePool(s) reference this OCloudSite", dependentCount),
		ObservedGeneration: site.Generation,
	}

	meta.SetStatusCondition(&site.Status.Conditions, condition)
	if err := r.Status().Update(ctx, site); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OCloudSiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&inventoryv1alpha1.OCloudSite{}).
		// Watch Location changes to re-reconcile OCloudSites that reference them
		Watches(
			&inventoryv1alpha1.Location{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueOCloudSitesForLocation),
		).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup ocloudsite controller: %w", err)
	}
	return nil
}

// enqueueOCloudSitesForLocation maps Location changes to OCloudSites that reference them.
func (r *OCloudSiteReconciler) enqueueOCloudSitesForLocation(ctx context.Context, obj client.Object) []reconcile.Request {
	location, ok := obj.(*inventoryv1alpha1.Location)
	if !ok {
		return nil
	}

	// Find all OCloudSites that reference this Location by name (spec.globalLocationName)
	var siteList inventoryv1alpha1.OCloudSiteList
	if err := r.List(ctx, &siteList, client.MatchingFields{
		ctlrutils.OCloudSiteGlobalLocationNameIndex: location.Name,
	}); err != nil {
		r.Logger.ErrorContext(ctx, "Failed to list OCloudSites for Location watch",
			slog.String("error", err.Error()))
		return nil
	}

	var requests []reconcile.Request
	for _, site := range siteList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&site),
		})
	}

	if len(requests) > 0 {
		r.Logger.InfoContext(ctx, "Location change triggering OCloudSite reconciliation",
			slog.String("locationName", location.Name),
			slog.Int("oCloudSiteCount", len(requests)))
	}

	return requests
}
