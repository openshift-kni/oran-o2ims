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
	"sigs.k8s.io/controller-runtime/pkg/log"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// LocationReconciler reconciles a Location object
type LocationReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations,verbs=get;list;watch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=locations/status,verbs=get;update;patch

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
		slog.String("name", location.Name))

	// Skip validation if being deleted - let Kubernetes handle deletion directly
	if !location.DeletionTimestamp.IsZero() {
		r.Logger.InfoContext(ctx, "Location is being deleted, skipping validation")
		return result, nil
	}

	// Validate and set Ready condition
	if err := r.validateAndSetConditions(ctx, location); err != nil {
		return requeueWithShortInterval(), err
	}

	return result, nil
}

// validateAndSetConditions sets the Ready condition.
func (r *LocationReconciler) validateAndSetConditions(ctx context.Context, location *inventoryv1alpha1.Location) error {
	condition := metav1.Condition{
		Type:               inventoryv1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             inventoryv1alpha1.ReasonReady,
		Message:            "Location is ready",
		ObservedGeneration: location.Generation,
	}

	// Only update if the condition is missing, has changed, or ObservedGeneration is stale
	existingCondition := meta.FindStatusCondition(location.Status.Conditions, inventoryv1alpha1.ConditionTypeReady)
	conditionMissing := existingCondition == nil
	conditionChanged := !conditionMissing && (existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason)
	generationStale := !conditionMissing && existingCondition.ObservedGeneration != location.Generation

	if conditionMissing || conditionChanged || generationStale {
		meta.SetStatusCondition(&location.Status.Conditions, condition)
		if err := r.Status().Update(ctx, location); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
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
