package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	api "github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/cluster/utils"
	api2 "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// ClusterServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ClusterServer)(nil)

// ClusterServerConfig defines the configuration attributes for the resource server
type ClusterServerConfig struct {
	Address         string
	CloudID         string
	GlobalCloudID   string
	Extensions      []string
	ExternalAddress string
}

// ClusterServer defines the instance attributes for an instance of a cluster server
type ClusterServer struct {
	Config                   *ClusterServerConfig
	Repo                     *repo.ClusterRepository
	SubscriptionEventHandler notifier.SubscriptionEventHandler
}

// GetClusterResourceTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResourceTypes(ctx context.Context, request api.GetClusterResourceTypesRequestObject) (api.GetClusterResourceTypesResponseObject, error) {
	records, err := r.Repo.GetClusterResourceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resource types: %w", err)
	}

	objects := make([]api.ClusterResourceType, len(records))
	for i, record := range records {
		objects[i] = models.ClusterResourceTypeToModel(&record)
	}

	return api.GetClusterResourceTypes200JSONResponse(objects), nil
}

// GetClusterResourceType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResourceType(ctx context.Context, request api.GetClusterResourceTypeRequestObject) (api.GetClusterResourceTypeResponseObject, error) {
	record, err := r.Repo.GetClusterResourceType(ctx, request.ClusterResourceTypeId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetClusterResourceType404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"ClusterResourceTypeId": request.ClusterResourceTypeId.String(),
			},
			Detail: "requested ClusterResourceType not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetClusterResourceType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"ClusterResourceTypeId": request.ClusterResourceTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.ClusterResourceTypeToModel(record)
	return api.GetClusterResourceType200JSONResponse(object), nil
}

// GetClusterResources receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResources(ctx context.Context, request api.GetClusterResourcesRequestObject) (api.GetClusterResourcesResponseObject, error) {
	records, err := r.Repo.GetClusterResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	objects := make([]api.ClusterResource, len(records))
	for i, record := range records {
		objects[i] = models.ClusterResourceToModel(&record)
	}

	return api.GetClusterResources200JSONResponse(objects), nil
}

// GetClusterResource receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResource(ctx context.Context, request api.GetClusterResourceRequestObject) (api.GetClusterResourceResponseObject, error) {
	record, err := r.Repo.GetClusterResource(ctx, request.ClusterResourceId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetClusterResource404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"ClusterResourceId": request.ClusterResourceId.String(),
			},
			Detail: "requested ClusterResource not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetClusterResource500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"ClusterResourceId": request.ClusterResourceId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.ClusterResourceToModel(record)
	return api.GetClusterResource200JSONResponse(object), nil
}

// GetNodeClusterTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusterTypes(ctx context.Context, request api.GetNodeClusterTypesRequestObject) (api.GetNodeClusterTypesResponseObject, error) {
	records, err := r.Repo.GetNodeClusterTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resource types: %w", err)
	}

	objects := make([]api.NodeClusterType, len(records))
	for i, record := range records {
		objects[i] = models.NodeClusterTypeToModel(&record)
	}

	return api.GetNodeClusterTypes200JSONResponse(objects), nil
}

// GetNodeClusterType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusterType(ctx context.Context, request api.GetNodeClusterTypeRequestObject) (api.GetNodeClusterTypeResponseObject, error) {
	record, err := r.Repo.GetNodeClusterType(ctx, request.NodeClusterTypeId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetNodeClusterType404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterTypeId": request.NodeClusterTypeId.String(),
			},
			Detail: "requested NodeClusterType not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetNodeClusterType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterTypeId": request.NodeClusterTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.NodeClusterTypeToModel(record)
	return api.GetNodeClusterType200JSONResponse(object), nil
}

// GetNodeClusters receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusters(ctx context.Context, request api.GetNodeClustersRequestObject) (api.GetNodeClustersResponseObject, error) {
	records, err := r.Repo.GetNodeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node clusters: %w", err)
	}

	objects := make([]api.NodeCluster, len(records))
	for i, record := range records {
		objects[i] = models.NodeClusterToModel(&record)
	}

	return api.GetNodeClusters200JSONResponse(objects), nil
}

// GetNodeCluster receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeCluster(ctx context.Context, request api.GetNodeClusterRequestObject) (api.GetNodeClusterResponseObject, error) {
	record, err := r.Repo.GetNodeCluster(ctx, request.NodeClusterId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetNodeCluster404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterId": request.NodeClusterId.String(),
			},
			Detail: "requested NodeCluster not found",
			Status: http.StatusNotFound,
		}, nil
	} else if err != nil {
		return api.GetNodeCluster500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterId": request.NodeClusterId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.NodeClusterToModel(record)
	return api.GetNodeCluster200JSONResponse(object), nil
}

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	version := utils2.CurrentVersion
	baseURL := utils2.BaseURL
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

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	version := utils2.CurrentVersion
	baseURL := utils2.BaseURL
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

// GetSubscriptions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
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
func (r *ClusterServer) validateSubscription(request api.CreateSubscriptionRequestObject) error {
	err := api2.ValidateCallbackURL(request.Body.Callback)
	if err != nil {
		return fmt.Errorf("invalid callback url: %w", err)
	}
	// TODO: add validation of filter and move to common if filter syntax is the same for all servers
	return nil
}

// CreateSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
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
func (r *ClusterServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
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
func (r *ClusterServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
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
