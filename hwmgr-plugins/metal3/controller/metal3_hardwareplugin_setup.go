/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Metal3Controllers holds references to the metal3 controllers for lifecycle management
type Metal3Controllers struct {
	NodeAllocationReconciler *NodeAllocationRequestReconciler
	AllocatedNodeReconciler  *AllocatedNodeReconciler
}

func SetupMetal3Controllers(mgr ctrl.Manager, namespace string) (*Metal3Controllers, error) {
	baseLogger := slog.New(logging.NewLoggingContextHandler(slog.LevelInfo))

	nodeAllocationReconciler := &NodeAllocationRequestReconciler{
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          baseLogger.With(slog.String("controller", "metal3_nodeallocationrequest_controller")),
		PluginNamespace: namespace,
		Manager:         mgr,
	}

	if err := nodeAllocationReconciler.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to setup NodeAllocationRequest controller: %w", err)
	}

	allocatedReconciler := &AllocatedNodeReconciler{
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          baseLogger.With(slog.String("controller", "metal3_allocatednode_controller")),
		PluginNamespace: namespace,
		Manager:         mgr,
	}

	if err := allocatedReconciler.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to setup AllocatedNode controller: %w", err)
	}

	return &Metal3Controllers{
		NodeAllocationReconciler: nodeAllocationReconciler,
		AllocatedNodeReconciler:  allocatedReconciler,
	}, nil
}
