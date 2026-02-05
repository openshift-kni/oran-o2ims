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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

// validateFirmwareUpdateSpec checks that the BIOS and firmware URLs are valid
func validateFirmwareUpdateSpec(spec hwmgmtv1alpha1.HardwareProfileSpec) error {

	if spec.BiosFirmware.Version != "" {
		if spec.BiosFirmware.URL == "" {
			return typederrors.NewInputError("missing BIOS firmware URL for version: %v", spec.BiosFirmware.Version)
		}
		if !ctlrutils.IsValidURL(spec.BiosFirmware.URL) {
			return typederrors.NewInputError("invalid BIOS firmware URL: %v", spec.BiosFirmware.URL)
		}
	}
	if spec.BmcFirmware.Version != "" {
		if spec.BmcFirmware.URL == "" {
			return typederrors.NewInputError("missing BMC firmware URL for version: %v", spec.BmcFirmware.Version)
		}
		if !ctlrutils.IsValidURL(spec.BmcFirmware.URL) {
			return typederrors.NewInputError("invalid BMC firmware URL: %v", spec.BmcFirmware.URL)
		}
	}

	for i, nic := range spec.NicFirmware {
		if nic.Version != "" {
			if nic.URL == "" {
				return typederrors.NewInputError("missing NIC firmware URL for NIC at index %v, version: %v", i, nic.Version)
			}
			if !ctlrutils.IsValidURL(nic.URL) {
				return typederrors.NewInputError("invalid NIC firmware URL for NIC at index %v: %v", i, nic.URL)
			}
		}
	}

	return nil
}

func convertToFirmwareUpdates(spec hwmgmtv1alpha1.HardwareProfileSpec) []metal3v1alpha1.FirmwareUpdate {
	var updates []metal3v1alpha1.FirmwareUpdate

	if spec.BiosFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: componentBIOS,
			URL:       spec.BiosFirmware.URL,
		})
	}

	if spec.BmcFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: componentBMC,
			URL:       spec.BmcFirmware.URL,
		})
	}

	// NIC firmware updates are handled by isVersionChangeDetected function
	// since we need to match against actual HFC status components

	return updates
}

func isHostFirmwareComponentsChangeDetectedAndValid(ctx context.Context,
	c client.Client,
	bmh *metal3v1alpha1.BareMetalHost) (bool, error) {
	// Extract logger from context
	logger := slog.Default()
	if ctxLogger, ok := ctx.Value(any("logger")).(*slog.Logger); ok && ctxLogger != nil {
		logger = ctxLogger
	}

	hfc, err := getHostFirmwareComponents(ctx, c, bmh.Name, bmh.Namespace)

	if err != nil {
		return false, fmt.Errorf("failed to get HostFirmwareComponents %s/%s: %w", bmh.Namespace, bmh.Name, err)
	}

	changeDetectedCond := meta.FindStatusCondition(hfc.Status.Conditions, string(metal3v1alpha1.HostFirmwareComponentsChangeDetected))
	if changeDetectedCond == nil {
		return false, fmt.Errorf("failed to get HostFirmwareComponents %s condition %s/%s: %w",
			metal3v1alpha1.FirmwareSettingsChangeDetected, bmh.Namespace, bmh.Name, err)
	}

	changeDetected := changeDetectedCond.Status == metav1.ConditionTrue
	valid := meta.IsStatusConditionTrue(hfc.Status.Conditions, string(metal3v1alpha1.HostFirmwareComponentsValid))
	observed := changeDetectedCond.ObservedGeneration == hfc.Generation

	logger.DebugContext(ctx, "Evaluating HostFirmwareComponents change",
		slog.String("bmh", bmh.Name),
		slog.String("hfc", hfc.Name),
		slog.Int64("hfcGeneration", hfc.Generation),
		slog.String("changeDetectedStatus", string(changeDetectedCond.Status)),
		slog.Int64("changeDetectedObservedGen", changeDetectedCond.ObservedGeneration),
		slog.Bool("changeDetected", changeDetected),
		slog.Bool("valid", valid),
		slog.Bool("observed", observed),
		slog.Bool("result", changeDetected && valid && observed))

	return changeDetected && valid && observed, nil
}

// validateHFCHasRequiredComponents checks that all firmware components specified in the HardwareProfile
// have corresponding component data in the HostFirmwareComponents status. This prevents attempting
// updates on components that don't have firmware data available.
func validateHFCHasRequiredComponents(status *metal3v1alpha1.HostFirmwareComponentsStatus,
	spec hwmgmtv1alpha1.HardwareProfileSpec) error {

	// Build a map of available components from HFC status
	availableComponents := make(map[string]bool)
	nicCount := 0
	for _, component := range status.Components {
		availableComponents[component.Component] = true
		if strings.HasPrefix(component.Component, componentNIC) {
			nicCount++
		}
	}

	// Check if BIOS firmware is required but not available
	if !spec.BiosFirmware.IsEmpty() && !availableComponents[componentBIOS] {
		return typederrors.NewInputError("BIOS firmware update requested but BIOS component not found in HostFirmwareComponents")
	}

	// Check if BMC firmware is required but not available
	if !spec.BmcFirmware.IsEmpty() && !availableComponents[componentBMC] {
		return typederrors.NewInputError("BMC firmware update requested but BMC component not found in HostFirmwareComponents")
	}

	// Check if NIC firmware is required but insufficient NICs available
	requiredNicCount := 0
	for _, nic := range spec.NicFirmware {
		if nic.Version != "" && nic.URL != "" {
			requiredNicCount++
		}
	}
	if requiredNicCount > 0 && nicCount == 0 {
		return typederrors.NewInputError("NIC firmware update requested but no NIC components found in HostFirmwareComponents")
	}
	if requiredNicCount > nicCount {
		return typederrors.NewInputError("NIC firmware update requested for %d NICs but only %d NIC components found in HostFirmwareComponents",
			requiredNicCount, nicCount)
	}

	return nil
}

func isVersionChangeDetected(ctx context.Context, logger *slog.Logger, status *metal3v1alpha1.HostFirmwareComponentsStatus,
	spec hwmgmtv1alpha1.HardwareProfileSpec) ([]metal3v1alpha1.FirmwareUpdate, bool) {

	firmwareMap := map[string]hwmgmtv1alpha1.Firmware{
		componentBIOS: spec.BiosFirmware,
		componentBMC:  spec.BmcFirmware,
	}

	var updates []metal3v1alpha1.FirmwareUpdate
	updateRequired := false

	// Handle BIOS and BMC firmware
	for _, component := range status.Components {
		if fw, exists := firmwareMap[component.Component]; exists {
			// Skip if firmware spec is empty
			if fw.IsEmpty() {
				logger.InfoContext(ctx, "Skipping firmware update due to empty firmware spec",
					slog.String("component", component.Component))
				continue
			}

			// If version differs, append update
			if component.CurrentVersion != fw.Version {
				updates = append(updates, metal3v1alpha1.FirmwareUpdate{
					Component: component.Component,
					URL:       fw.URL,
				})
				logger.InfoContext(ctx, "Add firmware update",
					slog.String("component", component.Component),
					slog.String("url", fw.URL))
				updateRequired = true
			} else {
				logger.InfoContext(ctx, "No version change detected",
					slog.String("current", component.CurrentVersion),
					slog.String("desired", fw.Version),
					slog.Any("spec", spec),
					slog.Any("hfc_status", status))
			}
		}
	}

	// Handle NIC firmware - match versions regardless of component name
	usedComponents := make(map[string]bool)
	for i, nic := range spec.NicFirmware {
		if nic.Version == "" || nic.URL == "" {
			continue // Skip if no version or URL specified
		}

		// Check if this version already exists in any nic: component
		versionFound := false
		for _, component := range status.Components {
			if strings.HasPrefix(component.Component, componentNIC) && component.CurrentVersion == nic.Version {
				versionFound = true
				usedComponents[component.Component] = true
				logger.InfoContext(ctx, "NIC firmware version already matches",
					slog.Int("nicIndex", i),
					slog.String("version", nic.Version),
					slog.String("component", component.Component))
				break
			}
		}

		if !versionFound {
			// Find the first available NIC component that hasn't been used yet
			for _, component := range status.Components {
				if !strings.HasPrefix(component.Component, componentNIC) || usedComponents[component.Component] {
					continue
				}
				updates = append(updates, metal3v1alpha1.FirmwareUpdate{
					Component: component.Component,
					URL:       nic.URL,
				})
				usedComponents[component.Component] = true
				logger.InfoContext(ctx, "Add NIC firmware update",
					slog.Int("nicIndex", i),
					slog.String("component", component.Component),
					slog.String("url", nic.URL),
					slog.String("targetVersion", nic.Version))
				updateRequired = true
				break
			}
		}
	}

	return updates, updateRequired
}

func createHostFirmwareComponents(ctx context.Context,
	c client.Client,
	bmh *metal3v1alpha1.BareMetalHost,
	spec hwmgmtv1alpha1.HardwareProfileSpec) (*metal3v1alpha1.HostFirmwareComponents, error) {

	updates := convertToFirmwareUpdates(spec)

	hfc := metal3v1alpha1.HostFirmwareComponents{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		},
		Spec: metal3v1alpha1.HostFirmwareComponentsSpec{
			Updates: updates,
		},
	}

	if err := c.Create(ctx, &hfc); err != nil {
		return nil, fmt.Errorf("failed to create HostFirmwareComponents: %w", err)
	}

	return hfc.DeepCopy(), nil
}

func updateHostFirmwareComponents(ctx context.Context,
	c client.Client,
	name types.NamespacedName, updates []metal3v1alpha1.FirmwareUpdate) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		hfc, err := getHostFirmwareComponents(ctx, c, name.Name, name.Namespace)
		if err != nil {
			return fmt.Errorf("failed to fetch HostFirmwareComponents %s/%s: %w", name.Namespace, name.Name, err)
		}
		hfc.Spec.Updates = updates
		return c.Update(ctx, hfc)
	})
}

func IsFirmwareUpdateRequired(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost, spec hwmgmtv1alpha1.HardwareProfileSpec) (bool, error) {
	if err := validateFirmwareUpdateSpec(spec); err != nil {
		return false, err
	}

	existingHFC, created, err := getOrCreateHostFirmwareComponents(ctx, c, logger, bmh, spec)
	if err != nil {
		return false, err
	}
	// If the resource was just created, we assume an update is needed
	if created {
		return true, nil
	}

	// Validate that HFC has all required components before proceeding
	if err := validateHFCHasRequiredComponents(&existingHFC.Status, spec); err != nil {
		return false, err
	}

	updates, updateRequired := isVersionChangeDetected(ctx, logger, &existingHFC.Status, spec)

	// No update needed if already up-to-date
	if !updateRequired {
		return false, nil
	}

	if err := updateHostFirmwareComponents(ctx, c, types.NamespacedName{
		Name:      existingHFC.Name,
		Namespace: existingHFC.Namespace,
	}, updates); err != nil {
		return false, fmt.Errorf("failed to update HostFirmwareComponents: %w", err)
	}

	return true, nil
}

// Retrieves existing HostFirmwareComponents or creates a new one if not found.
func getOrCreateHostFirmwareComponents(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
	spec hwmgmtv1alpha1.HardwareProfileSpec) (*metal3v1alpha1.HostFirmwareComponents, bool, error) {

	hfc, err := getHostFirmwareComponents(ctx, c, bmh.Name, bmh.Namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			newHFC, err := createHostFirmwareComponents(ctx, c, bmh, spec)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create HostFirmwareComponents: %w", err)
			}
			logger.InfoContext(ctx, "Successfully created HostFirmwareComponents", slog.String("HFC", bmh.Name))
			return newHFC, true, nil
		}
		return nil, false, err
	}

	return hfc, false, nil
}

func getHostFirmwareComponents(ctx context.Context,
	c client.Reader,
	name, namespace string) (*metal3v1alpha1.HostFirmwareComponents, error) {
	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, hfc)
	if err != nil {
		return nil, fmt.Errorf("failed to get HostFirmwareComponents %s/%s: %w", namespace, name, err)
	}

	return hfc, nil
}
