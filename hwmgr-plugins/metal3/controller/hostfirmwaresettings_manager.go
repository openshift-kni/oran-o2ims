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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

// convertBiosSettingsToHostFirmware converts BiosSettings to HostFirmwareSettings CR
func convertBiosSettingsToHostFirmware(bmh metal3v1alpha1.BareMetalHost, biosSettings hwmgmtv1alpha1.Bios) metal3v1alpha1.HostFirmwareSettings {
	return metal3v1alpha1.HostFirmwareSettings{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		},
		Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
			Settings: biosSettings.Attributes, // Copy attributes directly
		},
	}
}

func createHostFirmwareSettings(ctx context.Context, c client.Client, logger *slog.Logger, hfs *metal3v1alpha1.HostFirmwareSettings) error {
	if err := c.Create(ctx, hfs); err != nil {
		logger.InfoContext(ctx, "Failed to create HostFirmwareSettings", slog.String("HFS", hfs.Name))
		return fmt.Errorf("failed to create HostFirmwareSettings: %w", err)
	}
	return nil
}

func updateHostFirmwareSettings(ctx context.Context, c client.Client, name types.NamespacedName, settings metal3v1alpha1.HostFirmwareSettings) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		existingHFS, err := getHostFirmwareSettings(ctx, c, name.Name, name.Namespace)

		if err != nil {
			return fmt.Errorf("failed to fetch BMH %s/%s: %w", name.Namespace, name.Name, err)
		}
		existingHFS.Spec.Settings = settings.Spec.Settings
		return c.Update(ctx, existingHFS)
	})
}

func IsBiosUpdateRequired(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost, biosSettings hwmgmtv1alpha1.Bios) (bool, error) {
	hfs := convertBiosSettingsToHostFirmware(*bmh, biosSettings)

	existingHFS, err := getOrCreateHostFirmwareSettings(ctx, c, logger, &hfs)
	if err != nil {
		return false, err
	}

	if err := validateBiosSettings(ctx, c, existingHFS, hfs.Spec.Settings); err != nil {
		if !typederrors.IsInputError(err) {
			return false, fmt.Errorf("hfs %s/%s: %w", existingHFS.Namespace, existingHFS.Name, err)
		}
		return false, err
	}

	return checkAndUpdateFirmwareSettings(ctx, c, logger, existingHFS, &hfs)
}

func isFirmwareSettingsChangeDetectedAndValid(ctx context.Context,
	c client.Client,
	bmh *metal3v1alpha1.BareMetalHost) (bool, error) {
	hfs, err := getHostFirmwareSettings(ctx, c, bmh.Name, bmh.Namespace)

	if err != nil {
		return false, fmt.Errorf("failed to get HostFirmwareSettings %s/%s: %w", bmh.Namespace, bmh.Name, err)
	}
	changeDetectedCond := meta.FindStatusCondition(hfs.Status.Conditions, string(metal3v1alpha1.FirmwareSettingsChangeDetected))
	if changeDetectedCond == nil {
		return false, fmt.Errorf("failed to get HostFirmwareSettings %s condition %s/%s: %w",
			metal3v1alpha1.FirmwareSettingsChangeDetected, bmh.Namespace, bmh.Name, err)
	}

	changeDetected := changeDetectedCond.Status == metav1.ConditionTrue
	valid := meta.IsStatusConditionTrue(hfs.Status.Conditions, string(metal3v1alpha1.FirmwareSettingsValid))
	observed := changeDetectedCond.ObservedGeneration == hfs.Generation

	return changeDetected && valid && observed, nil
}

// Retrieves existing HostFirmwareSettings or creates a new one if not found.
func getOrCreateHostFirmwareSettings(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	hfs *metal3v1alpha1.HostFirmwareSettings) (*metal3v1alpha1.HostFirmwareSettings, error) {
	existingHFS, err := getHostFirmwareSettings(ctx, c, hfs.Name, hfs.Namespace)

	if err != nil {
		if errors.IsNotFound(err) {
			if err := createHostFirmwareSettings(ctx, c, logger, hfs); err != nil {
				logger.InfoContext(ctx, "Failed to create HostFirmwareSettings", slog.String("HFS", hfs.Name))
				return nil, fmt.Errorf("failed to create HostFirmwareSettings: %w", err)
			}
			logger.InfoContext(ctx, "Successfully created HostFirmwareSettings", slog.String("HFS", hfs.Name))
			return hfs.DeepCopy(), nil
		}
		logger.InfoContext(ctx, "Failed to get HostFirmwareSettings", slog.String("HFS", hfs.Name))
		return nil, err
	}

	return existingHFS, nil
}

func getHostFirmwareSettings(ctx context.Context, c client.Client, name, namespace string) (*metal3v1alpha1.HostFirmwareSettings, error) {
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, hfs)
	if err != nil {
		return nil, fmt.Errorf("failed to get HostFirmwareSettings %s/%s: %w", namespace, name, err)
	}
	return hfs, nil
}

// Validates the BIOS settings against the firmware schema.
func validateBiosSettings(ctx context.Context,
	c client.Client,
	existingHFS *metal3v1alpha1.HostFirmwareSettings, newSettings map[string]intstr.IntOrString) error {
	if existingHFS.Status.FirmwareSchema == nil {
		return fmt.Errorf("failed to get FirmwareSchema from HFS: %+v", existingHFS)
	}
	if existingHFS.Status.FirmwareSchema.Name == "" || existingHFS.Status.FirmwareSchema.Namespace == "" {
		return fmt.Errorf("firmwareSchema name or namespace is nil: %+v", existingHFS.Status.FirmwareSchema)
	}

	firmwareSchema := &metal3v1alpha1.FirmwareSchema{}
	if err := c.Get(ctx, client.ObjectKey{Name: existingHFS.Status.FirmwareSchema.Name,
		Namespace: existingHFS.Status.FirmwareSchema.Namespace}, firmwareSchema); err != nil {
		return fmt.Errorf("failed to get FirmwareSchema %s/%s: %w", existingHFS.Status.FirmwareSchema,
			existingHFS.Status.FirmwareSchema.Name, err)
	}

	validationErrors := validSettings(existingHFS, firmwareSchema, newSettings)
	if len(validationErrors) != 0 {
		return typederrors.NewInputError("invalid BIOS settings: %+v", validationErrors)
	}

	return nil
}

// Checks if BIOS settings have changed and updates if necessary.
func checkAndUpdateFirmwareSettings(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	existingHFS, hfs *metal3v1alpha1.HostFirmwareSettings) (bool, error) {
	if isChangeDetected(ctx, logger, hfs.Spec.Settings, existingHFS.Status.Settings) {
		logger.InfoContext(ctx, "Updating existing HostFirmwareSettings", slog.String("HFS", hfs.Name))

		if err := updateHostFirmwareSettings(ctx, c, types.NamespacedName{Name: hfs.Name, Namespace: hfs.Namespace}, *hfs); err != nil {
			logger.InfoContext(ctx, "Failed to update HostFirmwareSettings", slog.String("HFS", hfs.Name))
			return false, fmt.Errorf("failed to update HostFirmwareSettings: %w", err)
		}

		logger.InfoContext(ctx, "Successfully updated HostFirmwareSetting", slog.String("HFS", hfs.Name))
		return true, nil
	}

	logger.InfoContext(ctx, "No changes detected in HostFirmwareSettings", slog.String("HFS", hfs.Name))
	return false, nil
}

func validSettings(hfs *metal3v1alpha1.HostFirmwareSettings, schema *metal3v1alpha1.FirmwareSchema,
	newSettings map[string]intstr.IntOrString) []error {

	var validationErrors []error

	for name, val := range newSettings {

		// The setting must be in the Status
		if _, ok := hfs.Status.Settings[name]; !ok {
			validationErrors = append(validationErrors, fmt.Errorf("setting %s is not in the Status field", name))
			continue
		}

		// check validity of updated value
		if schema != nil {
			if err := schema.ValidateSetting(name, val, schema.Spec.Schema); err != nil {
				validationErrors = append(validationErrors, err)
			}
		}
	}

	return validationErrors
}

// isChangeDetected compares two maps (used to detect changes in BIOS attributes)
func isChangeDetected(ctx context.Context, logger *slog.Logger, a map[string]intstr.IntOrString, b map[string]string) bool {
	// Check if any Spec settings are different than Status
	changed := false
	for k, v := range a {
		if statusVal, ok := b[k]; ok {
			if v.String() != statusVal {
				logger.InfoContext(ctx, "spec value different than status", slog.String("name", k),
					slog.String("specvalue", v.String()), slog.String("statusvalue", statusVal))
				changed = true
				break
			}
		} else {
			// Spec setting is not in Status, this will be handled by validateHostFirmwareSettings
			changed = true
			break
		}
	}

	return changed
}
