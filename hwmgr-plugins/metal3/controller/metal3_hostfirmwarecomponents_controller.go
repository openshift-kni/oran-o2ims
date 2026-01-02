/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// Component names for firmware validation
	componentBIOS = "bios"
	componentBMC  = "bmc"
	componentNIC  = "nic:"
)

// HostFirmwareComponentsReconciler reconciles HostFirmwareComponents objects
type HostFirmwareComponentsReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	PluginNamespace string
}

// +kubebuilder:rbac:groups=metal3.io,resources=hostfirmwarecomponents,verbs=get;list;watch
// +kubebuilder:rbac:groups=metal3.io,resources=hostfirmwarecomponents/status,verbs=get
// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;update;patch
// +kubebuilder:rbac:groups=metal3.io,resources=hardwaredata,verbs=get;list
func (r *HostFirmwareComponentsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "HostFirmwareComponents")

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

	// Add logging context with the resource name
	ctx = logging.AppendCtx(ctx, slog.String("ReconcileRequest", req.Name))

	// Fetch the HostFirmwareComponents
	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	if err := r.NoncachedClient.Get(ctx, req.NamespacedName, hfc); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "HostFirmwareComponents not found, assuming deleted")
			return hwmgrutils.DoNotRequeue(), nil
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch HostFirmwareComponents", err)
		return hwmgrutils.RequeueWithShortInterval(), nil
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, hfc)
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", hfc.ResourceVersion))
	r.Logger.InfoContext(ctx, "Fetched HostFirmwareComponents successfully")

	// Get the corresponding BMH (same name and namespace)
	bmh := &metal3v1alpha1.BareMetalHost{}
	bmhKey := client.ObjectKey{Namespace: req.Namespace, Name: req.Name}
	if err := r.NoncachedClient.Get(ctx, bmhKey, bmh); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "Corresponding BareMetalHost not found")
			return hwmgrutils.DoNotRequeue(), nil
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch BareMetalHost", err)
		return hwmgrutils.RequeueWithShortInterval(), nil
	}

	// Check if this BMH is managed by O-Cloud Manager
	if !IsOCloudManaged(bmh) {
		r.Logger.InfoContext(ctx, "BareMetalHost is not O-Cloud managed, skipping validation")
		// Ensure label is removed if it exists
		if _, exists := bmh.Labels[ValidationUnavailableLabelKey]; exists {
			return r.removeLabelFromBMH(ctx, bmh)
		}
		return hwmgrutils.DoNotRequeue(), nil
	}

	// Check if this is an HPE or Dell system via HardwareData
	if !r.isHPEOrDell(ctx, req.Namespace, req.Name) {
		r.Logger.InfoContext(ctx, "System is not HPE or Dell, skipping validation")
		// Ensure label is removed if it exists
		if _, exists := bmh.Labels[ValidationUnavailableLabelKey]; exists {
			return r.removeLabelFromBMH(ctx, bmh)
		}
		return hwmgrutils.DoNotRequeue(), nil
	}

	// Check what firmware components are missing
	missingComponents := r.checkMissingComponents(ctx, hfc)

	if missingComponents == "" {
		// All components present, remove label if it exists
		if _, exists := bmh.Labels[ValidationUnavailableLabelKey]; exists {
			r.Logger.InfoContext(ctx, "All firmware components present, removing validation label")
			return r.removeLabelFromBMH(ctx, bmh)
		}
		r.Logger.InfoContext(ctx, "All firmware components present, no action needed")
		return hwmgrutils.DoNotRequeue(), nil
	}

	// Missing components detected, add or update label
	r.Logger.InfoContext(ctx, "Missing firmware components detected",
		slog.String("missingType", missingComponents))
	return r.addLabelToBMH(ctx, bmh, missingComponents)
}

// isHPEOrDell checks if the system is HPE or Dell by examining the HardwareData CR
func (r *HostFirmwareComponentsReconciler) isHPEOrDell(ctx context.Context, namespace, name string) bool {
	hardwareData := &metal3v1alpha1.HardwareData{}
	hdKey := client.ObjectKey{Namespace: namespace, Name: name}

	if err := r.NoncachedClient.Get(ctx, hdKey, hardwareData); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "HardwareData not found for validation check")
		} else {
			r.Logger.WarnContext(ctx, "Failed to get HardwareData for vendor check",
				slog.String("error", err.Error()))
		}
		return false
	}

	if hardwareData.Spec.HardwareDetails == nil {
		r.Logger.InfoContext(ctx, "HardwareDetails is nil, cannot check vendor")
		return false
	}

	manufacturer := hardwareData.Spec.HardwareDetails.SystemVendor.Manufacturer
	r.Logger.InfoContext(ctx, "Checking system manufacturer",
		slog.String("manufacturer", manufacturer))

	return manufacturer == "HPE" || manufacturer == "Dell Inc."
}

// checkMissingComponents analyzes the HFC status and returns a label value indicating what's missing
func (r *HostFirmwareComponentsReconciler) checkMissingComponents(ctx context.Context, hfc *metal3v1alpha1.HostFirmwareComponents) string {
	components := hfc.Status.Components

	// No components at all
	if len(components) == 0 {
		r.Logger.InfoContext(ctx, "No firmware components found in status")
		return LabelValueMissingFirmwareData
	}

	hasBIOS := false
	hasBMC := false
	hasNIC := false

	for _, comp := range components {
		componentName := strings.ToLower(comp.Component)

		switch {
		case componentName == componentBIOS:
			hasBIOS = true
		case componentName == componentBMC:
			hasBMC = true
		case strings.HasPrefix(componentName, componentNIC):
			hasNIC = true
		}
	}

	r.Logger.InfoContext(ctx, "Component presence check",
		slog.Bool("hasBIOS", hasBIOS),
		slog.Bool("hasBMC", hasBMC),
		slog.Bool("hasNIC", hasNIC))

	// Count missing components
	missingCount := 0
	if !hasBIOS {
		missingCount++
	}
	if !hasBMC {
		missingCount++
	}
	if !hasNIC {
		missingCount++
	}

	// Multiple components missing (2 or more)
	if missingCount >= 2 {
		return LabelValueMissingFirmwareData
	}

	// Single component missing
	if !hasNIC {
		return LabelValueMissingNICData
	}
	if !hasBMC {
		return LabelValueMissingBMCData
	}
	if !hasBIOS {
		return LabelValueMissingBIOSData
	}

	// All components present
	return ""
}

// addLabelToBMH adds or updates the validation label on the BMH
func (r *HostFirmwareComponentsReconciler) addLabelToBMH(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, labelValue string) (ctrl.Result, error) {
	if bmh.Labels == nil {
		bmh.Labels = make(map[string]string)
	}

	// Check if label already has the correct value
	if currentValue, exists := bmh.Labels[ValidationUnavailableLabelKey]; exists && currentValue == labelValue {
		r.Logger.InfoContext(ctx, "Validation label already set correctly",
			slog.String("labelValue", labelValue))
		return hwmgrutils.DoNotRequeue(), nil
	}

	bmh.Labels[ValidationUnavailableLabelKey] = labelValue

	if err := r.Client.Update(ctx, bmh); err != nil {
		ctlrutils.LogError(ctx, r.Logger, "Failed to add validation label to BareMetalHost", err)
		return hwmgrutils.RequeueWithShortInterval(), err
	}

	r.Logger.InfoContext(ctx, "Successfully added validation label to BareMetalHost",
		slog.String("labelKey", ValidationUnavailableLabelKey),
		slog.String("labelValue", labelValue))
	return hwmgrutils.DoNotRequeue(), nil
}

// removeLabelFromBMH removes the validation label from the BMH
func (r *HostFirmwareComponentsReconciler) removeLabelFromBMH(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost) (ctrl.Result, error) {
	delete(bmh.Labels, ValidationUnavailableLabelKey)

	if err := r.Client.Update(ctx, bmh); err != nil {
		ctlrutils.LogError(ctx, r.Logger, "Failed to remove validation label from BareMetalHost", err)
		return hwmgrutils.RequeueWithShortInterval(), err
	}

	r.Logger.InfoContext(ctx, "Successfully removed validation label from BareMetalHost",
		slog.String("labelKey", ValidationUnavailableLabelKey))
	return hwmgrutils.DoNotRequeue(), nil
}

// SetupWithManager sets up the controller with the Manager
func (r *HostFirmwareComponentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch for status updates on HostFirmwareComponents
	statusUpdatePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Process creates to establish initial state
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Only process if status has changed
			oldHFC, oldOK := e.ObjectOld.(*metal3v1alpha1.HostFirmwareComponents)
			newHFC, newOK := e.ObjectNew.(*metal3v1alpha1.HostFirmwareComponents)

			if !oldOK || !newOK {
				return false
			}

			// Check if status.components changed
			oldComponents := len(oldHFC.Status.Components)
			newComponents := len(newHFC.Status.Components)

			if oldComponents != newComponents {
				return true
			}

			// Check if lastUpdated timestamp changed
			return oldHFC.Status.LastUpdated != newHFC.Status.LastUpdated
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Don't need to process deletes - BMH will be deleted too
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&metal3v1alpha1.HostFirmwareComponents{}).
		WithEventFilter(statusUpdatePredicate).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create HostFirmwareComponents controller: %w", err)
	}

	return nil
}
