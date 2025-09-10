/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"

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

	return nil
}

func convertToFirmwareUpdates(spec hwmgmtv1alpha1.HardwareProfileSpec) []metal3v1alpha1.FirmwareUpdate {
	var updates []metal3v1alpha1.FirmwareUpdate

	if spec.BiosFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: "bios",
			URL:       spec.BiosFirmware.URL,
		})
	}

	if spec.BmcFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: "bmc",
			URL:       spec.BmcFirmware.URL,
		})
	}

	return updates
}

func isHostFirmwareComponentsChangeDetectedAndValid(ctx context.Context,
	c client.Client,
	bmh *metal3v1alpha1.BareMetalHost) (bool, error) {
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

	return changeDetected && valid && observed, nil
}

func isVersionChangeDetected(ctx context.Context, logger *slog.Logger, status *metal3v1alpha1.HostFirmwareComponentsStatus,
	spec hwmgmtv1alpha1.HardwareProfileSpec) ([]metal3v1alpha1.FirmwareUpdate, bool) {

	firmwareMap := map[string]hwmgmtv1alpha1.Firmware{
		"bios": spec.BiosFirmware,
		"bmc":  spec.BmcFirmware,
	}

	var updates []metal3v1alpha1.FirmwareUpdate
	updateRequired := false

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
