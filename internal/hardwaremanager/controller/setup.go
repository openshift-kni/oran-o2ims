/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"fmt"
	"log/slog"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler references, populated by SetupControllers for test access.
var (
	nodeAllocationReconciler         *NodeAllocationRequestReconciler
	allocatedNodeReconciler          *AllocatedNodeReconciler
	hostFirmwareComponentsReconciler *HostFirmwareComponentsReconciler
)

func SetupControllers(mgr ctrl.Manager, namespace string, baseLogger *slog.Logger) error {
	if mgr == nil {
		return fmt.Errorf("manager is required")
	}
	if baseLogger == nil {
		return fmt.Errorf("base logger is required")
	}

	nodeAllocationReconciler = &NodeAllocationRequestReconciler{
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          baseLogger.With("controller", "hwmgr_nodeallocationrequest_controller"),
		Namespace:       namespace,
		Manager:         mgr,
	}

	if err := nodeAllocationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup NodeAllocationRequest controller: %w", err)
	}

	allocatedNodeReconciler = &AllocatedNodeReconciler{
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          baseLogger.With("controller", "hwmgr_allocatednode_controller"),
		Namespace:       namespace,
		Manager:         mgr,
	}

	if err := allocatedNodeReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup AllocatedNode controller: %w", err)
	}

	hostFirmwareComponentsReconciler = &HostFirmwareComponentsReconciler{
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          baseLogger.With("controller", "hwmgr_hostfirmwarecomponents_controller"),
		Namespace:       namespace,
		Manager:         mgr,
	}

	if err := hostFirmwareComponentsReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup HostFirmwareComponents controller: %w", err)
	}

	return nil
}

// OverrideNoncachedClient replaces the NoncachedClient on all hardware manager
// reconcilers. This is intended for e2e tests that need to use a shared
// client to avoid envtest watchcache timing discrepancies.
func OverrideNoncachedClient(c client.Reader) {
	nodeAllocationReconciler.NoncachedClient = c
	allocatedNodeReconciler.NoncachedClient = c
	hostFirmwareComponentsReconciler.NoncachedClient = c
}
