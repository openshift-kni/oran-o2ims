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
	"sigs.k8s.io/controller-runtime/pkg/log"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// Label constants for BareMetalHost resources
const (
	BMHLabelSiteID = "resources.clcm.openshift.io/siteId"
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
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch

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
		slog.String("siteId", site.Spec.SiteID),
		slog.String("globalLocationId", site.Spec.GlobalLocationID))

	// Handle finalizer logic
	if result, stop, err := r.handleFinalizer(ctx, site); stop || err != nil {
		return result, err
	}

	// Validate Location reference and set Ready condition if not being deleted
	if site.DeletionTimestamp.IsZero() {
		if err := r.validateAndSetConditions(ctx, site); err != nil {
			return requeueWithShortInterval(), err
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
				slog.String("siteId", site.Spec.SiteID))
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
			slog.String("siteId", site.Spec.SiteID))

		// Check for dependent ResourcePools
		poolDependents, err := r.findDependentResourcePools(ctx, site.Spec.SiteID)
		if err != nil {
			return requeueWithShortInterval(), true, fmt.Errorf("failed to check for dependent ResourcePools: %w", err)
		}

		// Check for dependent BareMetalHosts
		bmhDependents, err := r.findDependentBMHs(ctx, site.Spec.SiteID)
		if err != nil {
			return requeueWithShortInterval(), true, fmt.Errorf("failed to check for dependent BareMetalHosts: %w", err)
		}

		totalDependents := len(poolDependents) + len(bmhDependents)
		if totalDependents > 0 {
			// Update status to indicate deletion is blocked
			r.Logger.InfoContext(ctx, "OCloudSite deletion blocked by dependents",
				slog.String("siteId", site.Spec.SiteID),
				slog.Int("resourcePoolCount", len(poolDependents)),
				slog.Int("bmhCount", len(bmhDependents)))

			if err := r.setDeletionBlockedCondition(ctx, site, len(poolDependents), len(bmhDependents)); err != nil {
				r.Logger.WarnContext(ctx, "Failed to update status", slog.String("error", err.Error()))
			}

			// Requeue to check again later
			return requeueWithCustomInterval(10 * time.Second), true, nil
		}

		// No dependents, safe to remove finalizer and allow k8s deletion
		r.Logger.InfoContext(ctx, "No dependents found, removing finalizer from OCloudSite",
			slog.String("siteId", site.Spec.SiteID))

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

// findDependentResourcePools returns all ResourcePools that reference this OCloudSite
func (r *OCloudSiteReconciler) findDependentResourcePools(ctx context.Context, siteID string) ([]inventoryv1alpha1.ResourcePool, error) {
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := r.List(ctx, &poolList); err != nil {
		return nil, fmt.Errorf("failed to list ResourcePools: %w", err)
	}

	var dependents []inventoryv1alpha1.ResourcePool
	for _, pool := range poolList.Items {
		if pool.Spec.OCloudSiteId == siteID {
			dependents = append(dependents, pool)
		}
	}

	return dependents, nil
}

// findDependentBMHs returns all BareMetalHosts that have the siteId label matching this OCloudSite
func (r *OCloudSiteReconciler) findDependentBMHs(ctx context.Context, siteID string) ([]bmhv1alpha1.BareMetalHost, error) {
	var bmhList bmhv1alpha1.BareMetalHostList
	if err := r.List(ctx, &bmhList, client.MatchingLabels{BMHLabelSiteID: siteID}); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	return bmhList.Items, nil
}

// validateAndSetConditions validates the Location reference and sets appropriate conditions
func (r *OCloudSiteReconciler) validateAndSetConditions(ctx context.Context, site *inventoryv1alpha1.OCloudSite) error {
	// Validate that the referenced Location exists
	locationExists, err := r.validateLocationReference(ctx, site.Spec.GlobalLocationID)
	if err != nil {
		return fmt.Errorf("failed to validate Location reference: %w", err)
	}

	var condition metav1.Condition
	if !locationExists {
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonInvalidReference,
			Message:            fmt.Sprintf("Referenced Location with globalLocationId '%s' does not exist", site.Spec.GlobalLocationID),
			ObservedGeneration: site.Generation,
		}
	} else {
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
			return fmt.Errorf("failed to update status: %w", err)
		}
	}

	return nil
}

// validateLocationReference checks if a Location with the given globalLocationId exists
func (r *OCloudSiteReconciler) validateLocationReference(ctx context.Context, globalLocationID string) (bool, error) {
	var locationList inventoryv1alpha1.LocationList
	if err := r.List(ctx, &locationList); err != nil {
		return false, fmt.Errorf("failed to list Locations: %w", err)
	}

	for _, location := range locationList.Items {
		if location.Spec.GlobalLocationID == globalLocationID {
			return true, nil
		}
	}

	return false, nil
}

// setDeletionBlockedCondition sets the Deleting condition indicating deletion is blocked
func (r *OCloudSiteReconciler) setDeletionBlockedCondition(ctx context.Context, site *inventoryv1alpha1.OCloudSite, poolCount, bmhCount int) error {
	message := fmt.Sprintf("Cannot delete: %d ResourcePool(s) and %d BareMetalHost(s) reference this OCloudSite", poolCount, bmhCount)
	condition := metav1.Condition{
		Type:               inventoryv1alpha1.ConditionTypeDeleting,
		Status:             metav1.ConditionFalse,
		Reason:             inventoryv1alpha1.ReasonDependentsExist,
		Message:            message,
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
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup ocloudsite controller: %w", err)
	}
	return nil
}
