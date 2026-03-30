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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// ResourcePoolReconciler reconciles a ResourcePool object
type ResourcePoolReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools,verbs=get;list;watch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=resourcepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=ocloudsites,verbs=get;list;watch

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
		slog.String("name", pool.Name),
		slog.String("oCloudSiteName", pool.Spec.OCloudSiteName))

	// Skip validation if being deleted - let Kubernetes handle deletion directly
	if !pool.DeletionTimestamp.IsZero() {
		r.Logger.InfoContext(ctx, "ResourcePool is being deleted, skipping validation")
		return result, nil
	}

	// Validate OCloudSite reference and set Ready condition
	parentValid, err := r.validateAndSetConditions(ctx, pool)
	if err != nil {
		return requeueWithShortInterval(), err
	}

	// If parent is not valid, stop without requeue: the OCloudSite watch will
	// trigger reconciliation when the parent is created or becomes ready.
	if !parentValid {
		return doNotRequeue(), nil
	}

	return result, nil
}

// validateAndSetConditions validates the OCloudSite reference and sets appropriate conditions.
// Returns (parentValid, error) where parentValid is true only when parent exists AND is ready.
// When parent is ready, stores the parent's metadata.uid in status.resolvedOCloudSiteUID.
func (r *ResourcePoolReconciler) validateAndSetConditions(ctx context.Context, pool *inventoryv1alpha1.ResourcePool) (bool, error) {
	// Validate that the referenced OCloudSite exists and is ready (lookup by name)
	result, parentUID, err := r.validateSiteReference(ctx, pool.Spec.OCloudSiteName, pool.Namespace)
	if err != nil {
		return false, fmt.Errorf("failed to validate OCloudSite reference: %w", err)
	}

	var condition metav1.Condition
	var resolvedUID string

	switch {
	case !result.Exists:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonParentNotFound,
			Message:            fmt.Sprintf("Referenced OCloudSite '%s' does not exist", pool.Spec.OCloudSiteName),
			ObservedGeneration: pool.Generation,
		}
		resolvedUID = "" // Clear stale UID
	case !result.Ready:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             inventoryv1alpha1.ReasonParentNotReady,
			Message:            fmt.Sprintf("Referenced OCloudSite '%s' is not ready", pool.Spec.OCloudSiteName),
			ObservedGeneration: pool.Generation,
		}
		resolvedUID = "" // Clear stale UID
	default:
		condition = metav1.Condition{
			Type:               inventoryv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             inventoryv1alpha1.ReasonReady,
			Message:            "ResourcePool is ready",
			ObservedGeneration: pool.Generation,
		}
		resolvedUID = parentUID // Store parent's UID for collector use
	}

	// Check if status needs updating
	existingCondition := meta.FindStatusCondition(pool.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	conditionMissing := existingCondition == nil
	conditionChanged := !conditionMissing && (existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason)
	generationStale := !conditionMissing && existingCondition.ObservedGeneration != pool.Generation
	uidChanged := pool.Status.ResolvedOCloudSiteUID != resolvedUID

	if conditionMissing || conditionChanged || generationStale || uidChanged {
		meta.SetStatusCondition(&pool.Status.Conditions, condition)
		pool.Status.ResolvedOCloudSiteUID = resolvedUID
		if err := r.Status().Update(ctx, pool); err != nil {
			return result.Exists && result.Ready, fmt.Errorf("failed to update status: %w", err)
		}
	}

	// Return true only when parent exists AND is ready
	return result.Exists && result.Ready, nil
}

// validateSiteReference checks if an OCloudSite with the given name exists and is ready.
// Returns (result, parentUID, error) where parentUID is the OCloudSite's metadata.uid when ready.
func (r *ResourcePoolReconciler) validateSiteReference(ctx context.Context, siteName, namespace string) (ctlrutils.ParentValidationResult, string, error) {
	site := &inventoryv1alpha1.OCloudSite{}
	if err := r.Get(ctx, client.ObjectKey{Name: siteName, Namespace: namespace}, site); err != nil {
		if errors.IsNotFound(err) {
			return ctlrutils.ParentValidationResult{Exists: false, Ready: false}, "", nil
		}
		return ctlrutils.ParentValidationResult{}, "", fmt.Errorf("failed to get OCloudSite: %w", err)
	}

	// OCloudSite exists, check if it's ready
	readyCondition := meta.FindStatusCondition(site.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		// Return parent's metadata.uid for storage in status
		return ctlrutils.ParentValidationResult{Exists: true, Ready: true}, string(site.UID), nil
	}

	// Exists but not ready
	return ctlrutils.ParentValidationResult{Exists: true, Ready: false}, "", nil
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

	// Find all ResourcePools in the same namespace that reference this OCloudSite by name
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := r.List(ctx, &poolList,
		client.InNamespace(site.Namespace),
		client.MatchingFields{
			ctlrutils.ResourcePoolOCloudSiteNameIndex: site.Name,
		},
	); err != nil {
		r.Logger.ErrorContext(ctx, "Failed to list ResourcePools for OCloudSite watch",
			slog.String("error", err.Error()))
		return nil
	}

	var requests []reconcile.Request
	for _, pool := range poolList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&pool),
		})
	}

	if len(requests) > 0 {
		r.Logger.InfoContext(ctx, "OCloudSite change triggering ResourcePool reconciliation",
			slog.String("siteName", site.Name),
			slog.Int("resourcePoolCount", len(requests)))
	}

	return requests
}
