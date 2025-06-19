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

	desiredSpec := metal3v1alpha1.HostUpdatePolicySpec{}

	if firmwareUpdateRequired {
		desiredSpec.FirmwareUpdates = "onReboot"
	}
	if biosUpdateRequired {
		desiredSpec.FirmwareSettings = "onReboot"
	}

	if errors.IsNotFound(err) {
		// Not found: create a new HostUpdatePolicy
		newPolicy := &metal3v1alpha1.HostUpdatePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmh.Name,
				Namespace: bmh.Namespace,
			},
			Spec: desiredSpec,
		}

		if err := c.Create(ctx, newPolicy); err != nil {
			return fmt.Errorf("failed to create HostUpdatePolicy: %w", err)
		}
		logger.InfoContext(ctx, "Created HostUpdatePolicy", slog.String("name", newPolicy.Name))
	} else {
		// Exists: check if update is needed
		if !reflect.DeepEqual(hup.Spec, desiredSpec) {
			hup.Spec = desiredSpec
			if err := c.Update(ctx, hup); err != nil {
				return fmt.Errorf("failed to update existing HostUpdatePolicy: %w", err)
			}
			logger.InfoContext(ctx, "Updated HostUpdatePolicy", slog.String("name", hup.Name))
		} else {
			logger.InfoContext(ctx, "HostUpdatePolicy already up to date", slog.String("name", hup.Name))
		}
	}

	return nil
}
