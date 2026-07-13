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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
)

const (
	conditionTypeValidation = "Validation"
	reasonAllValid          = "AllImagesValid"
	reasonValidationFailed  = "ValidationFailed"
)

// FirmwareCatalogReconciler reconciles FirmwareCatalog objects
type FirmwareCatalogReconciler struct {
	ctrl.Manager
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
}

// +kubebuilder:rbac:groups=clcm.openshift.io,resources=firmwarecatalogs,verbs=get;create;list;watch;update;patch
// +kubebuilder:rbac:groups=clcm.openshift.io,resources=firmwarecatalogs/status,verbs=get;update;patch
func (r *FirmwareCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()

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

	ctx = logging.AppendCtx(ctx, slog.String("reconcileRequest", req.Name))

	catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, catalog); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "FirmwareCatalog not found, assuming deleted")
			return hwmgrutils.DoNotRequeue(), nil
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch FirmwareCatalog", err)
		return hwmgrutils.RequeueWithShortInterval(), nil
	}

	if catalog.Status.ObservedGeneration == catalog.Generation {
		return hwmgrutils.DoNotRequeue(), nil
	}

	imageStatuses := validateCatalogImages(catalog.Spec.Images)

	allValid := true
	for i := range imageStatuses {
		if !imageStatuses[i].Valid {
			allValid = false
			break
		}
	}

	catalog.Status.ObservedGeneration = catalog.Generation
	catalog.Status.ImageStatuses = imageStatuses

	if allValid {
		meta.SetStatusCondition(&catalog.Status.Conditions, metav1.Condition{
			Type:               conditionTypeValidation,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: catalog.Generation,
			Reason:             reasonAllValid,
			Message:            "All firmware images passed validation",
		})
	} else {
		meta.SetStatusCondition(&catalog.Status.Conditions, metav1.Condition{
			Type:               conditionTypeValidation,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: catalog.Generation,
			Reason:             reasonValidationFailed,
			Message:            "One or more firmware images failed validation",
		})
	}

	if err := r.Client.Status().Update(ctx, catalog); err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to update FirmwareCatalog status: %w", err)
	}

	return hwmgrutils.DoNotRequeue(), nil
}

func validateCatalogImages(images []hwmgmtv1alpha1.FirmwareImage) []hwmgmtv1alpha1.ImageValidationStatus {
	statuses := make([]hwmgmtv1alpha1.ImageValidationStatus, 0, len(images))
	for _, img := range images {
		status := hwmgmtv1alpha1.ImageValidationStatus{
			Name:  img.Name,
			Valid: true,
		}

		if !ctlrutils.IsValidURL(img.URL) {
			status.Valid = false
			status.Reason = "InvalidURL"
			status.Message = fmt.Sprintf("URL %q is not a valid HTTP(S) URL", img.URL)
		}

		statuses = append(statuses, status)
	}
	return statuses
}

// EnsureFirmwareCatalogSingleton creates the singleton FirmwareCatalog CR if it
// does not already exist. It never overwrites user content.
// This is safe to call before mgr.Start() because Create bypasses the cache.
func EnsureFirmwareCatalogSingleton(ctx context.Context, c client.Client, namespace string) error {
	catalog := &hwmgmtv1alpha1.FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hwmgmtv1alpha1.FirmwareCatalogName,
			Namespace: namespace,
		},
		Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{},
	}

	if err := c.Create(ctx, catalog); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create FirmwareCatalog singleton: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *FirmwareCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&hwmgmtv1alpha1.FirmwareCatalog{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create FirmwareCatalog controller: %w", err)
	}
	return nil
}
