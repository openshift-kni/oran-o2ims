/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package inventory

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type InventoryServer struct {
	HubClient client.Client
	Logger    *slog.Logger
}

// InventoryServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ StrictServerInterface = (*InventoryServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = "/hardware-manager/inventory/v1"
var currentVersion = "1.0.0"

// GetAllVersions handles an API request to fetch all versions
func (i *InventoryServer) GetAllVersions(_ context.Context, _ GetAllVersionsRequestObject) (GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return GetAllVersions200JSONResponse(APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (i *InventoryServer) GetMinorVersions(_ context.Context, _ GetMinorVersionsRequestObject) (GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return GetMinorVersions200JSONResponse(APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

func (i *InventoryServer) GetResourcePools(ctx context.Context, request GetResourcePoolsRequestObject) (GetResourcePoolsResponseObject, error) {
	// TODO implement me
	return GetResourcePools200JSONResponse{}, nil
}

func (i *InventoryServer) GetResourcePool(ctx context.Context, request GetResourcePoolRequestObject) (GetResourcePoolResponseObject, error) {
	// TODO implement me
	return GetResourcePool200JSONResponse{}, nil
}

func (i *InventoryServer) GetResourcePoolResources(ctx context.Context, request GetResourcePoolResourcesRequestObject) (GetResourcePoolResourcesResponseObject, error) {
	// TODO implement me
	return GetResourcePoolResources200JSONResponse{}, nil
}

func (i *InventoryServer) GetResources(ctx context.Context, request GetResourcesRequestObject) (GetResourcesResponseObject, error) {
	// TODO implement me
	return GetResources200JSONResponse{}, nil
}

func (i *InventoryServer) GetResource(ctx context.Context, request GetResourceRequestObject) (GetResourceResponseObject, error) {
	// TODO implement me
	return GetResource200JSONResponse{}, nil
}

// GetSubscriptions receives the API request to this endpoint, executes the request, and responds appropriately
func (i *InventoryServer) GetSubscriptions(ctx context.Context, request GetSubscriptionsRequestObject,
) (GetSubscriptionsResponseObject, error) {
	// TODO
	objects := make([]Subscription, 1)
	return GetSubscriptions200JSONResponse(objects), nil
}

// CreateSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (i *InventoryServer) CreateSubscription(ctx context.Context, request CreateSubscriptionRequestObject,
) (CreateSubscriptionResponseObject, error) {
	// TODO
	return CreateSubscription201JSONResponse(Subscription{}), nil
}

// GetSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (i *InventoryServer) GetSubscription(ctx context.Context, request GetSubscriptionRequestObject,
) (GetSubscriptionResponseObject, error) {
	// TODO
	return GetSubscription200JSONResponse(Subscription{}), nil
}

// DeleteSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (i *InventoryServer) DeleteSubscription(ctx context.Context, request DeleteSubscriptionRequestObject,
) (DeleteSubscriptionResponseObject, error) {
	// TODO
	return DeleteSubscription200Response{}, nil
}
