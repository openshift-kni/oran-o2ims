/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginclient "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// HardwarePluginReconciler reconciles a HardwarePlugin object
type HardwarePluginReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareplugins/finalizers,verbs=update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HardwarePluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = hwmgrutils.RequeueWithLongInterval()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "HardwarePlugin")

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

	// Fetch the CR:
	hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
	if err = r.Client.Get(ctx, req.NamespacedName, hwplugin); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "HardwarePlugin not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch HardwarePlugin", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, hwplugin)
	ctx = logging.AppendCtx(ctx, slog.String("HardwarePlugin", hwplugin.Name))
	r.Logger.InfoContext(ctx, "Fetched HardwarePlugin successfully")

	hwplugin.Status.ObservedGeneration = hwplugin.Generation

	// Phase 1: Validate the HardwarePlugin
	ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "validation")
	phaseStartTime := time.Now()

	condReason := hwmgmtv1alpha1.ConditionReasons.InProgress
	condStatus := metav1.ConditionFalse
	condMessage := ""

	var isValid bool
	isValid, err = r.validateHardwarePlugin(ctx, hwplugin)
	if err != nil {
		err = fmt.Errorf("encountered an error while attempting to validate HardwarePlugin (%s): %w", hwplugin.Name, err)
		condMessage = err.Error()
		ctlrutils.LogError(ctx, r.Logger, "HardwarePlugin validation failed", err)
		result = hwmgrutils.RequeueWithMediumInterval()
	} else {
		if isValid {
			condReason = hwmgmtv1alpha1.ConditionReasons.Completed
			condStatus = metav1.ConditionTrue
			condMessage = fmt.Sprintf("Validated connection to %s", hwplugin.Spec.ApiRoot)
		} else {
			condReason = hwmgmtv1alpha1.ConditionReasons.Failed
			condStatus = metav1.ConditionFalse
			condMessage = fmt.Sprintf("Failed to validate connection to %s", hwplugin.Spec.ApiRoot)
			r.Logger.InfoContext(ctx, "Failed to validate connection to HardwarePlugin",
				slog.String("apiRoot", hwplugin.Spec.ApiRoot))
			result = hwmgrutils.RequeueWithMediumInterval()
		}
	}
	ctlrutils.LogPhaseComplete(ctx, r.Logger, "validation", time.Since(phaseStartTime))

	if updateErr := hwmgrutils.UpdateHardwarePluginStatusCondition(ctx, r.Client, hwplugin,
		hwmgmtv1alpha1.ConditionTypes.Registration, condReason, condStatus, condMessage); updateErr != nil {
		err = fmt.Errorf("failed to update status for HardwarePlugin (%s) with validation success: %w", hwplugin.Name, updateErr)
	}

	return
}

// validateHardwarePlugin verifies secure connectivity to the HardwarePlugin's apiRoot using mTLS.
func (r *HardwarePluginReconciler) validateHardwarePlugin(ctx context.Context, hwplugin *hwmgmtv1alpha1.HardwarePlugin) (bool, error) {

	if hwplugin.Spec.AuthClientConfig == nil {
		return false, fmt.Errorf("missing authClientConfig configuration")
	}

	// Validate apiRoot URL
	apiRoot, err := url.Parse(hwplugin.Spec.ApiRoot)
	if err != nil {
		return false, fmt.Errorf("invalid apiRoot URL '%s': %w", hwplugin.Spec.ApiRoot, err)
	}

	// Get HardwarePlugin client
	hwpclient, err := hwpluginclient.NewHardwarePluginClient(ctx, r.Client, r.Logger, hwplugin)
	if err != nil {
		return false, fmt.Errorf("failed to get HardwarePlugin client: %w", err)
	}

	// Validate the connection by fetch api-versions
	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		_, err = hwpclient.GetAllVersions(ctx)
		if err != nil {
			return fmt.Errorf("validation attempt to '%s' failed, err: %w", apiRoot, err)
		}
		return nil
	}); err != nil {
		r.Logger.ErrorContext(ctx, fmt.Sprintf("validation attempt to '%s' failed, err: %s", apiRoot, err.Error()))
		return false, nil
	}

	r.Logger.InfoContext(ctx, fmt.Sprintf("validation attempt to '%s' succeeded", apiRoot))
	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HardwarePluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up HardwarePlugin controller")
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&hwmgmtv1alpha1.HardwarePlugin{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup HardwarePlugin controller: %w", err)
	}

	return nil
}
