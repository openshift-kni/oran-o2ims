/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

func createOrUpdateHostUpdatePolicy(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
	firmwareUpdateRequired, biosUpdateRequired bool) error {

	// Validate input parameters
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if c == nil {
		return fmt.Errorf("client cannot be nil")
	}
	if bmh == nil {
		return fmt.Errorf("bmh cannot be nil")
	}

	logger.DebugContext(ctx, "Creating or updating HostUpdatePolicy",
		slog.String("bmh", bmh.Name),
		slog.Bool("firmwareUpdateRequired", firmwareUpdateRequired),
		slog.Bool("biosUpdateRequired", biosUpdateRequired))

	hup := &metal3v1alpha1.HostUpdatePolicy{}
	key := types.NamespacedName{
		Name:      bmh.Name,
		Namespace: bmh.Namespace,
	}

	// Try to get existing policy
	err := c.Get(ctx, key, hup)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get HostUpdatePolicy: %w", err)
	}

	existingFirmwareUpdates := ""
	existingFirmwareSettings := ""
	if err == nil {
		existingFirmwareUpdates = string(hup.Spec.FirmwareUpdates)
		existingFirmwareSettings = string(hup.Spec.FirmwareSettings)
		logger.DebugContext(ctx, "Found existing HostUpdatePolicy",
			slog.String("name", hup.Name),
			slog.String("existingFirmwareUpdates", existingFirmwareUpdates),
			slog.String("existingFirmwareSettings", existingFirmwareSettings))
	}

	desiredSpec := metal3v1alpha1.HostUpdatePolicySpec{}

	if firmwareUpdateRequired {
		desiredSpec.FirmwareUpdates = metal3v1alpha1.HostUpdatePolicyOnReboot
	}
	if biosUpdateRequired {
		desiredSpec.FirmwareSettings = metal3v1alpha1.HostUpdatePolicyOnReboot
	}

	logger.DebugContext(ctx, "Desired HostUpdatePolicy spec",
		slog.String("bmh", bmh.Name),
		slog.String("desiredFirmwareUpdates", string(desiredSpec.FirmwareUpdates)),
		slog.String("desiredFirmwareSettings", string(desiredSpec.FirmwareSettings)))

	if errors.IsNotFound(err) {
		// Not found: create a new HostUpdatePolicy
		newPolicy := &metal3v1alpha1.HostUpdatePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmh.Name,
				Namespace: bmh.Namespace,
			},
			Spec: desiredSpec,
		}

		logger.DebugContext(ctx, "Creating new HostUpdatePolicy",
			slog.String("name", newPolicy.Name))

		if err := c.Create(ctx, newPolicy); err != nil {
			return fmt.Errorf("failed to create HostUpdatePolicy: %w", err)
		}
		logger.InfoContext(ctx, "Created HostUpdatePolicy",
			slog.String("name", newPolicy.Name),
			slog.String("firmwareUpdates", string(desiredSpec.FirmwareUpdates)),
			slog.String("firmwareSettings", string(desiredSpec.FirmwareSettings)))
	} else {
		// Exists: check if update is needed
		if !reflect.DeepEqual(hup.Spec, desiredSpec) {
			logger.DebugContext(ctx, "HostUpdatePolicy spec differs, updating",
				slog.String("name", hup.Name),
				slog.String("oldFirmwareUpdates", existingFirmwareUpdates),
				slog.String("newFirmwareUpdates", string(desiredSpec.FirmwareUpdates)),
				slog.String("oldFirmwareSettings", existingFirmwareSettings),
				slog.String("newFirmwareSettings", string(desiredSpec.FirmwareSettings)))

			hup.Spec = desiredSpec
			if err := c.Update(ctx, hup); err != nil {
				return fmt.Errorf("failed to update existing HostUpdatePolicy: %w", err)
			}
			logger.InfoContext(ctx, "Updated HostUpdatePolicy",
				slog.String("name", hup.Name),
				slog.String("firmwareUpdates", string(desiredSpec.FirmwareUpdates)),
				slog.String("firmwareSettings", string(desiredSpec.FirmwareSettings)))
		} else {
			logger.DebugContext(ctx, "HostUpdatePolicy already up to date",
				slog.String("name", hup.Name),
				slog.String("firmwareUpdates", string(desiredSpec.FirmwareUpdates)),
				slog.String("firmwareSettings", string(desiredSpec.FirmwareSettings)))
		}
	}

	return nil
}
