/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	generated "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/server"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// LoopbackPluginServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ generated.StrictServerInterface = (*LoopbackPluginServer)(nil)

type LoopbackPluginServer struct {
	utils.CommonServerConfig
	HubClient client.Client
}

var baseURL = "/hardware-manager/provisioning/v1"
var currentVerion = "1.0.0"

// GetAllVersions handles an API request to fetch all versions
func (s *LoopbackPluginServer) GetAllVersions(_ context.Context, _ generated.GetAllVersionsRequestObject,
) (generated.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVerion,
		},
	}
	return generated.GetAllVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (s *LoopbackPluginServer) GetMinorVersions(_ context.Context, _ generated.GetMinorVersionsRequestObject,
) (generated.GetMinorVersionsResponseObject, error) {
	// We currently support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVerion,
		},
	}
	return generated.GetMinorVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

func (s *LoopbackPluginServer) GetNodeAllocationRequests(
	ctx context.Context,
	request generated.GetNodeAllocationRequestsRequestObject,
) (generated.GetNodeAllocationRequestsResponseObject, error) {

	// TODO

	return generated.GetNodeAllocationRequests200JSONResponse{}, nil
}

func (s *LoopbackPluginServer) GetNodeAllocationRequest(
	ctx context.Context,
	request generated.GetNodeAllocationRequestRequestObject,
) (generated.GetNodeAllocationRequestResponseObject, error) {

	// TODO

	return generated.GetNodeAllocationRequest200JSONResponse{}, nil
}

// CreateNodeAllocationRequest creates a NodeAllocationRequest object
func (s *LoopbackPluginServer) CreateNodeAllocationRequest(
	ctx context.Context,
	request generated.CreateNodeAllocationRequestRequestObject,
) (generated.CreateNodeAllocationRequestResponseObject, error) {

	// TODO

	return generated.CreateNodeAllocationRequest202JSONResponse(""), nil
}

func (s *LoopbackPluginServer) UpdateNodeAllocationRequest(
	ctx context.Context,
	request generated.UpdateNodeAllocationRequestRequestObject,
) (generated.UpdateNodeAllocationRequestResponseObject, error) {

	// TODO

	return generated.UpdateNodeAllocationRequest202JSONResponse{}, nil
}

func (s *LoopbackPluginServer) DeleteNodeAllocationRequest(
	ctx context.Context,
	request generated.DeleteNodeAllocationRequestRequestObject,
) (generated.DeleteNodeAllocationRequestResponseObject, error) {

	// TODO

	return generated.DeleteNodeAllocationRequest202JSONResponse{}, nil
}

func (s *LoopbackPluginServer) GetAllocatedNodes(
	ctx context.Context,
	request generated.GetAllocatedNodesRequestObject,
) (generated.GetAllocatedNodesResponseObject, error) {

	// TODO

	return generated.GetAllocatedNodes200JSONResponse{}, nil
}

func (s *LoopbackPluginServer) GetAllocatedNode(
	ctx context.Context,
	request generated.GetAllocatedNodeRequestObject,
) (generated.GetAllocatedNodeResponseObject, error) {

	// TODO

	return generated.GetAllocatedNode200JSONResponse{}, nil
}

func (s *LoopbackPluginServer) GetAllocatedNodesFromNodeAllocationRequest(
	ctx context.Context,
	request generated.GetAllocatedNodesFromNodeAllocationRequestRequestObject,
) (generated.GetAllocatedNodesFromNodeAllocationRequestResponseObject, error) {

	// TODO

	return generated.GetAllocatedNodesFromNodeAllocationRequest200JSONResponse{}, nil
}
