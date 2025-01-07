package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	api2 "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/resources/utils"
)

// ResourceServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ResourceServer)(nil)

// ResourceServerConfig defines the configuration attributes for the resource server
type ResourceServerConfig struct {
	utils.CommonServerConfig
	CloudID         string
	GlobalCloudID   string
	BackendURL      string
	Extensions      []string
	ExternalAddress string
}

// ResourceServer defines the instance attributes for an instance of a resource server
type ResourceServer struct {
	Config                   *ResourceServerConfig
	Info                     api.OCloudInfo
	Repo                     *repo.ResourcesRepository
	SubscriptionEventHandler notifier.SubscriptionEventHandler
}

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	version := utils2.CurrentInventoryVersion
	baseURL := utils2.BaseInventoryURL
	versions := []generated.APIVersion{
		{
			Version: &version,
		},
	}

	return api.GetAllVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetCloudInfo receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetCloudInfo(ctx context.Context, request api.GetCloudInfoRequestObject) (api.GetCloudInfoResponseObject, error) {
	return api.GetCloudInfo200JSONResponse(r.Info), nil
}

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	version := utils2.CurrentInventoryVersion
	baseURL := utils2.BaseInventoryURL
	versions := []generated.APIVersion{
		{
			Version: &version,
		},
	}

	return api.GetMinorVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetDeploymentManagers receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetDeploymentManagers(ctx context.Context, request api.GetDeploymentManagersRequestObject) (api.GetDeploymentManagersResponseObject, error) {
	records, err := r.Repo.GetDeploymentManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment managers: %w", err)
	}

	objects := make([]api.DeploymentManager, len(records))
	for i, record := range records {
		objects[i] = models.DeploymentManagerToModel(&record)
	}

	return api.GetDeploymentManagers200JSONResponse(objects), nil
}

// GetDeploymentManager receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetDeploymentManager(ctx context.Context, request api.GetDeploymentManagerRequestObject) (api.GetDeploymentManagerResponseObject, error) {
	record, err := r.Repo.GetDeploymentManager(ctx, request.DeploymentManagerId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetDeploymentManager404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"deploymentManagerId": request.DeploymentManagerId.String(),
			},
			Detail: "requested deploymentManager not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetDeploymentManager500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"deploymentManagerId": request.DeploymentManagerId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.DeploymentManagerToModel(record)
	return api.GetDeploymentManager200JSONResponse(object), nil
}

// GetSubscriptions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	records, err := r.Repo.GetSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	objects := make([]api.Subscription, len(records))
	for i, record := range records {
		objects[i] = models.SubscriptionToModel(&record)
	}

	return api.GetSubscriptions200JSONResponse(objects), nil
}

// validateSubscription validates a subscription before accepting the request
func (r *ResourceServer) validateSubscription(request api.CreateSubscriptionRequestObject) error {
	err := api2.ValidateCallbackURL(request.Body.Callback)
	if err != nil {
		return fmt.Errorf("invalid callback url: %w", err)
	}
	// TODO: add validation of filter and move to common if filter syntax is the same for all servers
	return nil
}

// CreateSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
	consumerSubscriptionId := "<null>"
	if request.Body.ConsumerSubscriptionId != nil {
		consumerSubscriptionId = request.Body.ConsumerSubscriptionId.String()
	}

	// Validate the subscription
	if err := r.validateSubscription(request); err != nil {
		filter := "<null>"
		if request.Body.Filter != nil {
			filter = *request.Body.Filter
		}
		return api.CreateSubscription400ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
				"callback":               request.Body.Callback,
				"filter":                 filter,
			},
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	// Convert from Model -> DB
	record := models.SubscriptionFromModel(request.Body)

	// Set internal fields
	record.EventCursor = 0

	result, err := r.Repo.CreateSubscription(ctx, record)
	if err != nil {
		if strings.Contains(err.Error(), "unique_callback") {
			// 409 is a more common choice for a duplicate entry, but the conformance tests expect a 400
			return api.CreateSubscription400ApplicationProblemPlusJSONResponse{
				AdditionalAttributes: &map[string]string{
					"consumerSubscriptionId": consumerSubscriptionId,
					"callback":               request.Body.Callback,
				},
				Detail: "callback value must be unique",
				Status: http.StatusBadRequest,
			}, nil
		}
		slog.Error("error writing database record", "target", record, "error", err.Error())
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: err.Error(),
			// TODO: map errors to 400 if possible; else 500
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Signal the notifier to handle this new subscription
	r.SubscriptionEventHandler.SubscriptionEvent(&notifier.SubscriptionEvent{
		Removed:      false,
		Subscription: models.SubscriptionToInfo(result),
	})

	response := models.SubscriptionToModel(result)
	return api.CreateSubscription201JSONResponse(response), nil
}

// GetSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	record, err := r.Repo.GetSubscription(ctx, request.SubscriptionId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetSubscription404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: "requested subscription not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.SubscriptionToModel(record)
	return api.GetSubscription200JSONResponse(object), nil
}

// DeleteSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
	count, err := r.Repo.DeleteSubscription(ctx, request.SubscriptionId)
	if err != nil {
		return api.DeleteSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if count == 0 {
		return api.DeleteSubscription404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: "requested subscription not found",
			Status: http.StatusNotFound,
		}, nil
	}

	// Signal the notifier to handle this subscription change
	r.SubscriptionEventHandler.SubscriptionEvent(&notifier.SubscriptionEvent{
		Removed: true,
		Subscription: models.SubscriptionToInfo(&models2.Subscription{
			SubscriptionID: &request.SubscriptionId,
		}),
	})

	return api.DeleteSubscription200Response{}, nil
}

// GetResourcePools receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePools(ctx context.Context, request api.GetResourcePoolsRequestObject) (api.GetResourcePoolsResponseObject, error) {
	records, err := r.Repo.GetResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve resource pools: %w", err)
	}

	objects := make([]api.ResourcePool, len(records))
	for i, record := range records {
		objects[i] = models.ResourcePoolToModel(&record)
	}

	return api.GetResourcePools200JSONResponse(objects), nil
}

// GetResourcePool receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePool(ctx context.Context, request api.GetResourcePoolRequestObject) (api.GetResourcePoolResponseObject, error) {
	record, err := r.Repo.GetResourcePool(ctx, request.ResourcePoolId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetResourcePool404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: "requested resourcePool not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetResourcePool500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.ResourcePoolToModel(record)
	return api.GetResourcePool200JSONResponse(object), nil
}

// GetResources receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResources(ctx context.Context, request api.GetResourcesRequestObject) (api.GetResourcesResponseObject, error) {
	// First, find the pool
	if exists, err := r.Repo.ResourcePoolExists(ctx, request.ResourcePoolId); err == nil && !exists {
		return api.GetResources404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: "requested resource pool not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetResources500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Next, get the resources
	records, err := r.Repo.GetResourcePoolResources(ctx, request.ResourcePoolId)
	if err != nil {
		return api.GetResources500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Convert from DB -> API
	objects := make([]api.Resource, len(records))
	for i, record := range records {
		// TODO: include child resources (not sure that's required unless directly querying resource)
		objects[i] = models.ResourceToModel(&record, nil)
	}

	return api.GetResources200JSONResponse(objects), nil
}

// GetResource receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResource(ctx context.Context, request api.GetResourceRequestObject) (api.GetResourceResponseObject, error) {
	// First, find the pool
	if exists, err := r.Repo.ResourcePoolExists(ctx, request.ResourcePoolId); err == nil && !exists {
		return api.GetResource404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: "requested resource pool not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetResource500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Next, get the resources
	record, err := r.Repo.GetResource(ctx, request.ResourceId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetResource404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
				"resourceId":     request.ResourceId.String(),
			},
			Detail: "requested resource not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetResource500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourcePoolId": request.ResourcePoolId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// TODO: include child resources (note sure we'll have that use case)

	object := models.ResourceToModel(record, nil)
	return api.GetResource200JSONResponse(object), nil
}

// GetResourceTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourceTypes(ctx context.Context, request api.GetResourceTypesRequestObject) (api.GetResourceTypesResponseObject, error) {
	records, err := r.Repo.GetResourceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource types: %w", err)
	}

	objects := make([]api.ResourceType, len(records))
	for i, record := range records {
		objects[i] = models.ResourceTypeToModel(&record)
	}

	return api.GetResourceTypes200JSONResponse(objects), nil
}

// GetResourceType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourceType(ctx context.Context, request api.GetResourceTypeRequestObject) (api.GetResourceTypeResponseObject, error) {
	record, err := r.Repo.GetResourceType(ctx, request.ResourceTypeId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetResourceType404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: "requested resourceType not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetResourceType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.ResourceTypeToModel(record)
	return api.GetResourceType200JSONResponse(object), nil
}
