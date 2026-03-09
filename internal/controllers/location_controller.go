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
	"sigs.k8s.io/controller-runtime/pkg/log"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// LocationReconciler reconciles a Location object
type LocationReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations/finalizers,verbs=update
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *LocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = doNotRequeue()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "Location")

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

	// Fetch the Location object
	location := &inventoryv1alpha1.Location{}
	if err = r.Client.Get(ctx, req.NamespacedName, location); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "Location not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch Location", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, location)
	r.Logger.InfoContext(ctx, "Fetched Location successfully",
		slog.String("globalLocationId", location.Spec.GlobalLocationID))

	// Handle finalizer logic
	if result, stop, err := r.handleFinalizer(ctx, location); stop || err != nil {
		return result, err
	}

	// Validate and set Ready condition if not being deleted
	if location.DeletionTimestamp.IsZero() {
		if err := r.validateAndSetConditions(ctx, location); err != nil {
			return requeueWithShortInterval(), err
		}
	}

	return result, nil
}

// handleFinalizer manages the finalizer for Location CRs
func (r *LocationReconciler) handleFinalizer(
	ctx context.Context, location *inventoryv1alpha1.Location) (ctrl.Result, bool, error) {

	// Check if the Location is marked to be deleted
	if location.DeletionTimestamp.IsZero() {
		// Object is not being deleted, add finalizer if not present
		if !controllerutil.ContainsFinalizer(location, inventoryv1alpha1.LocationFinalizer) {
			r.Logger.InfoContext(ctx, "Adding finalizer to Location",
				slog.String("globalLocationId", location.Spec.GlobalLocationID))
			controllerutil.AddFinalizer(location, inventoryv1alpha1.LocationFinalizer)
			if err := r.Update(ctx, location); err != nil {
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
	if controllerutil.ContainsFinalizer(location, inventoryv1alpha1.LocationFinalizer) {
		r.Logger.InfoContext(ctx, "Location is being deleted, checking for dependents",
			slog.String("globalLocationId", location.Spec.GlobalLocationID))

		// A Location that is not Ready (e.g., DuplicateID) can always be deleted
		// because it was never serving any dependents - skip dependent check
		readyCondition := meta.FindStatusCondition(location.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
		isReady := readyCondition != nil && readyCondition.Status == metav1.ConditionTrue

		if isReady {
			// Only check for dependents if Location is Ready
			dependents, err := r.findDependentOCloudSites(ctx, location.Spec.GlobalLocationID)
			if err != nil {
				return requeueWithShortInterval(), true, fmt.Errorf("failed to check for dependents: %w", err)
			}

			if len(dependents) > 0 {
				// Update status to indicate deletion is blocked
				r.Logger.InfoContext(ctx, "Location deletion blocked by dependent OCloudSites",
					slog.String("globalLocationId", location.Spec.GlobalLocationID),
					slog.Int("dependentCount", len(dependents)))

				if err := r.setDeletionBlockedCondition(ctx, location, len(dependents)); err != nil {
					r.Logger.WarnContext(ctx, "Failed to update status", slog.String("error", err.Error()))
				}

				// Requeue to check again later
				return requeueWithCustomInterval(10 * time.Second), true, nil
			}
		} else {
			r.Logger.InfoContext(ctx, "Location is not Ready, skipping dependent check",
				slog.String("globalLocationId", location.Spec.GlobalLocationID))
		}

		// No dependents (or not Ready), safe to remove finalizer and allow k8s deletion
		r.Logger.InfoContext(ctx, "Removing finalizer from Location",
			slog.String("globalLocationId", location.Spec.GlobalLocationID))

		patch := client.MergeFrom(location.DeepCopy())
		if controllerutil.RemoveFinalizer(location, inventoryv1alpha1.LocationFinalizer) {
			if err := r.Patch(ctx, location, patch); err != nil {
				r.Logger.WarnContext(ctx, "Failed to remove finalizer, will retry",
					slog.String("error", err.Error()))
				return requeueWithShortInterval(), true, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}
		return doNotRequeue(), true, nil
	}

	return doNotRequeue(), false, nil
}

// findDependentOCloudSites returns all OCloudSites that reference this Location
func (r *LocationReconciler) findDependentOCloudSites(ctx context.Context, globalLocationID string) ([]inventoryv1alpha1.OCloudSite, error) {
	var siteList inventoryv1alpha1.OCloudSiteList
	if err := r.List(ctx, &siteList); err != nil {
		return nil, fmt.Errorf("failed to list OCloudSites: %w", err)
	}

	var dependents []inventoryv1alpha1.OCloudSite
	for _, site := range siteList.Items {
		if site.Spec.GlobalLocationID == globalLocationID {
			dependents = append(dependents, site)
		}
	}

	return dependents, nil
}

// validateAndSetConditions validates uniqueness and sets appropriate conditions.
func (r *LocationReconciler) validateAndSetConditions(ctx context.Context, location *inventoryv1alpha1.Location) error {
	// Check for duplicate globalLocationId
	duplicateName, err := r.findDuplicateLocation(ctx, location)
	if err != nil {
		return fmt.Errorf("failed to check for duplicates: %w", err)
	}

	var condition metav1.Condition
	if duplicateName != "" {
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonDuplicateID,
			Message:            fmt.Sprintf("globalLocationId '%s' is already used by Location '%s'", location.Spec.GlobalLocationID, duplicateName),
			ObservedGeneration: location.Generation,
		}
		r.Logger.WarnContext(ctx, "Duplicate globalLocationId detected",
			slog.String("globalLocationId", location.Spec.GlobalLocationID),
			slog.String("conflictingLocation", duplicateName))
	} else {
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             inventoryv1alpha1.ReasonReady,
			Message:            "Location is ready",
			ObservedGeneration: location.Generation,
		}
	}

	// Only update if the condition has changed
	existingCondition := meta.FindStatusCondition(location.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	if existingCondition == nil || existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason {
		meta.SetStatusCondition(&location.Status.Conditions, condition)
		if err := r.Status().Update(ctx, location); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}

	return nil
}

// findDuplicateLocation checks if another Location CR already uses the same globalLocationId.
// Returns the name of the conflicting Location if found, or empty string if no duplicate exists.
func (r *LocationReconciler) findDuplicateLocation(ctx context.Context, location *inventoryv1alpha1.Location) (string, error) {
	var locationList inventoryv1alpha1.LocationList
	if err := r.List(ctx, &locationList); err != nil {
		return "", fmt.Errorf("failed to list Locations: %w", err)
	}

	for _, other := range locationList.Items {
		// Skip self
		if other.Name == location.Name && other.Namespace == location.Namespace {
			continue
		}
		// Check for duplicate globalLocationId
		if other.Spec.GlobalLocationID == location.Spec.GlobalLocationID {
			return other.Name, nil
		}
	}

	return "", nil
}

// setDeletionBlockedCondition sets the Deleting condition indicating deletion is blocked
func (r *LocationReconciler) setDeletionBlockedCondition(ctx context.Context, location *inventoryv1alpha1.Location, dependentCount int) error {
	condition := metav1.Condition{
		Type:               inventoryv1alpha1.ConditionTypeDeleting,
		Status:             metav1.ConditionFalse,
		Reason:             inventoryv1alpha1.ReasonDependentsExist,
		Message:            fmt.Sprintf("Cannot delete: %d OCloudSite(s) reference this Location", dependentCount),
		ObservedGeneration: location.Generation,
	}

	meta.SetStatusCondition(&location.Status.Conditions, condition)
	if err := r.Status().Update(ctx, location); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&inventoryv1alpha1.Location{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup location controller: %w", err)
	}
	return nil
}
