/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// HardwarePluginReconciler reconciles a HardwarePlugin object
type HardwarePluginReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins/finalizers,verbs=update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HardwarePluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = utils.DoNotRequeue()

	// Fetch the CR:
	hwplugin := &pluginv1alpha1.HardwarePlugin{}
	if err = r.Client.Get(ctx, req.NamespacedName, hwplugin); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch HardwarePlugin",
			slog.String("error", err.Error()),
		)
		return
	}

	// Make sure this is an instance for this adaptor and that this generation hasn't already been handled
	if hwplugin.Status.ObservedGeneration == hwplugin.Generation {
		// Nothing to do
		return
	}

	ctx = logging.AppendCtx(ctx, slog.String("HardwarePlugin", hwplugin.Name))

	hwplugin.Status.ObservedGeneration = hwplugin.Generation

	// Validate the HardwarePlugin
	condReason := pluginv1alpha1.ConditionReasons.InProgress
	condStatus := metav1.ConditionFalse
	condMessage := ""

	isValid, err := r.validateHardwarePlugin(ctx, hwplugin)

	if err != nil {
		err = fmt.Errorf("encountered an error while attempting to validate HardwarePlugin (%s): %w", hwplugin.Name, err)
		condMessage = err.Error()

	} else {
		if isValid {
			condReason = pluginv1alpha1.ConditionReasons.Completed
			condStatus = metav1.ConditionTrue
			condMessage = fmt.Sprintf("Validated connection to %s", hwplugin.Spec.ApiRoot)
		} else {
			condReason = pluginv1alpha1.ConditionReasons.Failed
			condStatus = metav1.ConditionFalse
			condMessage = fmt.Sprintf("Failed to validate connection to %s", hwplugin.Spec.ApiRoot)
		}
	}

	if updateErr := utils.UpdateHardwarePluginStatusCondition(ctx, r.Client, hwplugin,
		pluginv1alpha1.ConditionTypes.Registration, condReason, condStatus, condMessage); updateErr != nil {
		err = fmt.Errorf("failed to update status for HardwarePlugin (%s) with validation success: %w", hwplugin.Name, updateErr)
	}

	return
}

// validateHardwarePlugin verifies secure connectivity to the HardwarePlugin's apiRoot using mTLS.
func (r *HardwarePluginReconciler) validateHardwarePlugin(ctx context.Context, hwplugin *pluginv1alpha1.HardwarePlugin) (bool, error) {
	// TODO - complete
	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HardwarePluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up HardwarePlugin controller")
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pluginv1alpha1.HardwarePlugin{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup HardwarePlugin controller: %w", err)
	}

	return nil
}
