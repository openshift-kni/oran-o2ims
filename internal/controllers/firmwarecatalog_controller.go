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

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// FirmwareCatalogReconciler reconciles a FirmwareCatalog object
type FirmwareCatalogReconciler struct {
	client.Client
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=clcm.openshift.io,resources=firmwarecatalogs,verbs=get;list;watch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=firmwarecatalogs/status,verbs=get;update;patch

// Reconcile validates image entries and writes validation results to status.
func (r *FirmwareCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = doNotRequeue()

	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "FirmwareCatalog")

	defer func() {
		duration := time.Since(startTime)
		if err != nil {
			r.Logger.ErrorContext(ctx, "Reconciliation failed",
				slog.Duration("duration", duration),
				slog.Any("error", err))
		} else {
			r.Logger.InfoContext(ctx, "Reconciliation completed",
				slog.Duration("duration", duration),
				slog.Bool("requeue", result.Requeue),
				slog.Duration("requeueAfter", result.RequeueAfter))
		}
	}()

	catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
	if err = r.Client.Get(ctx, req.NamespacedName, catalog); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "FirmwareCatalog not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch FirmwareCatalog", err)
		return
	}

	ctx = ctlrutils.AddObjectContext(ctx, catalog)
	r.Logger.InfoContext(ctx, "Fetched FirmwareCatalog successfully",
		slog.String("name", catalog.Name))

	if !catalog.DeletionTimestamp.IsZero() {
		r.Logger.InfoContext(ctx, "FirmwareCatalog is being deleted, skipping validation")
		return result, nil
	}

	if err := r.validateAndSetStatus(ctx, catalog); err != nil {
		return requeueWithShortInterval(), err
	}

	return result, nil
}

// validateAndSetStatus builds image statuses and writes results to status.
// Field-level validation (component enum, URL pattern) is enforced by CRD markers
// at admission time, so the controller only records each accepted entry as valid.
func (r *FirmwareCatalogReconciler) validateAndSetStatus(ctx context.Context, catalog *hwmgmtv1alpha1.FirmwareCatalog) error {
	imageStatuses := make([]hwmgmtv1alpha1.ImageValidationStatus, 0, len(catalog.Spec.Images))
	for _, img := range catalog.Spec.Images {
		imageStatuses = append(imageStatuses, hwmgmtv1alpha1.ImageValidationStatus{
			Name:    img.Name,
			Valid:   true,
			Reason:  "Valid",
			Message: "Firmware image entry is valid",
		})
	}

	condition := metav1.Condition{
		Type:               string(hwmgmtv1alpha1.Validation),
		ObservedGeneration: catalog.Generation,
		Status:             metav1.ConditionTrue,
		Reason:             string(hwmgmtv1alpha1.Completed),
		Message:            "All firmware catalog entries are valid",
	}

	existingCondition := meta.FindStatusCondition(catalog.Status.Conditions, string(hwmgmtv1alpha1.Validation))
	conditionMissing := existingCondition == nil
	conditionChanged := !conditionMissing && (existingCondition.Status != condition.Status || existingCondition.Reason != condition.Reason)
	generationStale := !conditionMissing && existingCondition.ObservedGeneration != catalog.Generation

	if conditionMissing || conditionChanged || generationStale {
		catalog.Status.ObservedGeneration = catalog.Generation
		catalog.Status.ImageStatuses = imageStatuses
		meta.SetStatusCondition(&catalog.Status.Conditions, condition)
		if err := r.Status().Update(ctx, catalog); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FirmwareCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&hwmgmtv1alpha1.FirmwareCatalog{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup FirmwareCatalog controller: %w", err)
	}
	return nil
}
