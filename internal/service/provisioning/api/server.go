package api

import (
	"context"
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProvisioningServer struct {
	HubClient client.Client
}

type ProvisioningServerConfig struct {
	utils.CommonServerConfig
}

// ProvisioningServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ProvisioningServer)(nil)

// GetProvisioningRequests handles an API request to fetch provisioning requests
func (r *ProvisioningServer) GetProvisioningRequests(ctx context.Context, request api.GetProvisioningRequestsRequestObject) (api.GetProvisioningRequestsResponseObject, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetProvisioningRequest handles an API request to retrieve a provisioning request
func (r *ProvisioningServer) GetProvisioningRequest(ctx context.Context, request api.GetProvisioningRequestRequestObject) (api.GetProvisioningRequestResponseObject, error) {
	return nil, fmt.Errorf("not implemented")
}

// CreateProvisioningRequest handles an API request to create provisioning requests
func (r *ProvisioningServer) CreateProvisioningRequest(ctx context.Context, request api.CreateProvisioningRequestRequestObject) (api.CreateProvisioningRequestResponseObject, error) {
	return nil, fmt.Errorf("not implemented")
}

// UpdateProvisioningRequest handles an API request to update a provisioning request
func (r *ProvisioningServer) UpdateProvisioningRequest(ctx context.Context, request api.UpdateProvisioningRequestRequestObject) (api.UpdateProvisioningRequestResponseObject, error) {
	return nil, fmt.Errorf("not implemented")
}

// DeleteProvisioningRequest handles an API request to delete a provisioning request
func (r *ProvisioningServer) DeleteProvisioningRequest(ctx context.Context, request api.DeleteProvisioningRequestRequestObject) (api.DeleteProvisioningRequestResponseObject, error) {
	return nil, fmt.Errorf("not implemented")
}
