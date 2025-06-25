/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	inventory "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
	hwpluginserver "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/provisioning"

	loopbackctrl "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/loopback/controller"
)

// LoopbackPluginInventoryServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ inventory.StrictServerInterface = (*LoopbackPluginInventoryServer)(nil)

type LoopbackPluginInventoryServer struct {
	inventory.InventoryServer
}

// NewLoopbackPluginInventoryServer creates a Loopback HardwarePlugin Inventory server
func NewLoopbackPluginInventoryServer(
	hubClient client.Client,
	logger *slog.Logger,
) (*LoopbackPluginInventoryServer, error) {
	return &LoopbackPluginInventoryServer{
		InventoryServer: inventory.InventoryServer{
			HubClient: hubClient,
			Logger:    logger,
		},
	}, nil
}

func (l *LoopbackPluginInventoryServer) GetResourcePools(ctx context.Context, request inventory.GetResourcePoolsRequestObject) (inventory.GetResourcePoolsResponseObject, error) {
	// nolint: wrapcheck
	return loopbackctrl.GetResourcePools(ctx, l.HubClient, l.Logger, hwpluginserver.GetLoopbackHWPluginNamespace())
}

func (l *LoopbackPluginInventoryServer) GetResources(ctx context.Context, request inventory.GetResourcesRequestObject) (inventory.GetResourcesResponseObject, error) {
	// nolint: wrapcheck
	return loopbackctrl.GetResources(ctx, l.HubClient, l.Logger, hwpluginserver.GetLoopbackHWPluginNamespace())
}
