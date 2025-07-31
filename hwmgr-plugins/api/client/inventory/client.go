/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package inventory

import (
	"context"
	"fmt"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrclientutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// InventoryClient provides functions for calling the HardwarePlugin APIs
type InventoryClient struct {
	client   *ClientWithResponses
	hwPlugin *hwmgmtv1alpha1.HardwarePlugin
}

// NewInventoryClient creates an authenticated client connected to the Hardware plugin Inventory server.
func NewInventoryClient(
	ctx context.Context,
	c rtclient.Client,
	hwPlugin *hwmgmtv1alpha1.HardwarePlugin,
) (*InventoryClient, error) {
	// Construct OAuth client configuration
	config, err := hwmgrclientutils.SetupOAuthClientConfig(ctx, c, hwPlugin)
	if err != nil {
		return nil, fmt.Errorf("failed to setup OAuth client config: %w", err)
	}

	// Build OAuth client information if type is not ServiceAccount
	cf := notifier.NewClientFactory(config, constants.DefaultBackendTokenFile)
	httpClient, err := cf.NewClient(ctx, hwPlugin.Spec.AuthClientConfig.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	hClient, err := NewClientWithResponses(
		hwPlugin.Spec.ApiRoot,
		WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client with responses: %w", err)
	}

	return &InventoryClient{
		client:   hClient,
		hwPlugin: hwPlugin,
	}, nil
}

// GetResourcesWithResponse request returning *GetResourcesResponse
func (i *InventoryClient) GetResourcesWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*GetResourcesResponse, error) {
	rsp, err := i.client.GetResources(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseGetResourcesResponse(rsp)
}

// GetResourcePoolsWithResponse request returning *GetResourcePoolsResponse
func (i *InventoryClient) GetResourcePoolsWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*GetResourcePoolsResponse, error) {
	rsp, err := i.client.GetResourcePools(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseGetResourcePoolsResponse(rsp)
}
