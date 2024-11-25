package api

import (
	"context"
	"fmt"

	api "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ResourceServer)(nil)

type ResourceServer struct {
	Repository *repo.ResourceRepository
}

func (r ResourceServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetCloudInfo(ctx context.Context, request api.GetCloudInfoRequestObject) (api.GetCloudInfoResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetDeploymentManagers(ctx context.Context, request api.GetDeploymentManagersRequestObject) (api.GetDeploymentManagersResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetDeploymentManager(ctx context.Context, request api.GetDeploymentManagerRequestObject) (api.GetDeploymentManagerResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResourcePools(ctx context.Context, request api.GetResourcePoolsRequestObject) (api.GetResourcePoolsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResourcePool(ctx context.Context, request api.GetResourcePoolRequestObject) (api.GetResourcePoolResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResources(ctx context.Context, request api.GetResourcesRequestObject) (api.GetResourcesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResource(ctx context.Context, request api.GetResourceRequestObject) (api.GetResourceResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResourceTypes(ctx context.Context, request api.GetResourceTypesRequestObject) (api.GetResourceTypesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (r ResourceServer) GetResourceType(ctx context.Context, request api.GetResourceTypeRequestObject) (api.GetResourceTypeResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}
