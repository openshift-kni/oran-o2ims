/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
	metal3ctrl "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
)

// Metal3PluginInventoryServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ inventory.StrictServerInterface = (*Metal3PluginInventoryServer)(nil)

type Metal3PluginInventoryServer struct {
	inventory.InventoryServer
}

// NewMetal3PluginServer creates a Metal3 HardwarePlugin inventory server
func NewMetal3PluginInventoryServer(
	hubClient client.Client,
	logger *slog.Logger,
) (*Metal3PluginInventoryServer, error) {
	return &Metal3PluginInventoryServer{
		InventoryServer: inventory.InventoryServer{
			HubClient: hubClient,
			Logger:    logger,
		},
	}, nil
}

func (m *Metal3PluginInventoryServer) GetResourcePools(ctx context.Context, request inventory.GetResourcePoolsRequestObject) (inventory.GetResourcePoolsResponseObject, error) {
	// nolint: wrapcheck
	return metal3ctrl.GetResourcePools(ctx, m.HubClient)
}

func (m *Metal3PluginInventoryServer) GetResources(ctx context.Context, request inventory.GetResourcesRequestObject) (inventory.GetResourcesResponseObject, error) {
	// nolint: wrapcheck
	return metal3ctrl.GetResources(ctx, m.HubClient)
}
