package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	api "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ResourceServer)(nil)

type ResourceServer struct {
	Repo *repo.ResourcesRepository
}

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetCloudInfo receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetCloudInfo(ctx context.Context, request api.GetCloudInfoRequestObject) (api.GetCloudInfoResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetDeploymentManagers receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetDeploymentManagers(ctx context.Context, request api.GetDeploymentManagersRequestObject) (api.GetDeploymentManagersResponseObject, error) {
	records, err := r.Repo.GetDeploymentManagers(ctx)
	if err != nil {
		return api.GetDeploymentManagers500ApplicationProblemPlusJSONResponse{
			Detail:   err.Error(),
			Instance: nil,
			Status:   http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.DeploymentManager, len(records))
	for i, record := range records {
		objects[i], err = models.DeploymentManagerToModel(&record)
		if err != nil {
			slog.Error("error converting database record", "source", record)
			return api.GetDeploymentManagers500ApplicationProblemPlusJSONResponse{
				AdditionalAttributes: &map[string]string{
					"deploymentManagerId": record.ClusterID.String(),
				},
				Detail:   err.Error(),
				Instance: nil,
				Status:   http.StatusInternalServerError,
			}, nil
		}
	}

	return api.GetDeploymentManagers200JSONResponse(objects), nil
}

// GetDeploymentManager receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetDeploymentManager(ctx context.Context, request api.GetDeploymentManagerRequestObject) (api.GetDeploymentManagerResponseObject, error) {
	records, err := r.Repo.GetDeploymentManager(ctx, request.DeploymentManagerId)
	if err != nil {
		return api.GetDeploymentManager500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"deploymentManagerId": request.DeploymentManagerId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(records) == 0 {
		return api.GetDeploymentManager404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"deploymentManagerId": request.DeploymentManagerId.String(),
			},
			Detail: "requested deploymentManager not found",
			Status: http.StatusNotFound,
		}, nil
	}

	object, err := models.DeploymentManagerToModel(&records[0])
	if err != nil {
		slog.Error("error converting database records", "source", records[0])
		return api.GetDeploymentManager500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"deploymentManagerId": request.DeploymentManagerId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	return api.GetDeploymentManager200JSONResponse(object), nil
}

// GetSubscriptions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	records, err := r.Repo.GetSubscriptions(ctx)
	if err != nil {
		return api.GetSubscriptions500ApplicationProblemPlusJSONResponse{
			Detail:   err.Error(),
			Instance: nil,
			Status:   http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.Subscription, len(records))
	for i, record := range records {
		objects[i], err = models.SubscriptionToModel(&record)
		if err != nil {
			slog.Error("error converting database record", "source", record)
			return api.GetSubscriptions500ApplicationProblemPlusJSONResponse{
				AdditionalAttributes: &map[string]string{
					"subscriptionId": record.SubscriptionID.String(),
				},
				Detail:   err.Error(),
				Instance: nil,
				Status:   http.StatusInternalServerError,
			}, nil
		}
	}

	return api.GetSubscriptions200JSONResponse(objects), nil
}

// CreateSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
	consumerSubscriptionId := "<null>"
	if request.Body.ConsumerSubscriptionId != nil {
		consumerSubscriptionId = *request.Body.ConsumerSubscriptionId
	}

	// Convert from Model -> DB
	record, err := models.SubscriptionFromModel(request.Body)
	if err != nil {
		slog.Error("error converting to database record", "source", request)
		return api.CreateSubscription400ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Set internal fields
	record.CreatedAt = time.Now()
	record.EventCursor = 0

	err = r.Repo.CreateSubscription(ctx, record)
	if err != nil {
		slog.Error("error writing database record", "target", record)
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: err.Error(),
			// TODO: map errors to 400 if possible; else 500
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Re-query the DB to get the stored copy
	results, err := r.Repo.GetSubscription(ctx, *record.SubscriptionID)
	if err != nil {
		slog.Error("error re-reading created database record", "target", record)
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(results) == 0 {
		slog.Error("unable to retrieve newly created database record", "target", record)
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: "unable to retrieve newly created database record",
			Status: http.StatusInternalServerError,
		}, nil
	}

	response, err := models.SubscriptionToModel(&results[0])
	if err != nil {
		slog.Error("error converting database record", "source", results[0])
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"consumerSubscriptionId": consumerSubscriptionId,
			},
			Detail: err.Error(),
			// TODO: map errors to 400 if possible; else 500
			Status: http.StatusInternalServerError,
		}, nil

	}

	return api.CreateSubscription201JSONResponse(response), nil
}

// GetSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	records, err := r.Repo.GetSubscription(ctx, request.SubscriptionId)
	if err != nil {
		return api.GetSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(records) == 0 {
		return api.GetSubscription404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: "requested subscription not found",
			Status: http.StatusNotFound,
		}, nil
	}

	object, err := models.SubscriptionToModel(&records[0])
	if err != nil {
		slog.Error("error converting database records", "source", records[0])
		return api.GetSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	return api.GetSubscription200JSONResponse(object), nil
}

// DeleteSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
	records, err := r.Repo.GetSubscription(ctx, request.SubscriptionId)
	if err != nil {
		return api.DeleteSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(records) == 0 {
		return api.DeleteSubscription404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: "requested subscription not found",
			Status: http.StatusNotFound,
		}, nil
	}

	err = r.Repo.DeleteSubscription(ctx, *records[0].SubscriptionID)
	if err != nil {
		return api.DeleteSubscription500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"subscriptionId": request.SubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	return api.DeleteSubscription200Response{}, nil
}

// GetResourcePools receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePools(ctx context.Context, request api.GetResourcePoolsRequestObject) (api.GetResourcePoolsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetResourcePool receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePool(ctx context.Context, request api.GetResourcePoolRequestObject) (api.GetResourcePoolResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetResources receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResources(ctx context.Context, request api.GetResourcesRequestObject) (api.GetResourcesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetResource receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResource(ctx context.Context, request api.GetResourceRequestObject) (api.GetResourceResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetResourceTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourceTypes(ctx context.Context, request api.GetResourceTypesRequestObject) (api.GetResourceTypesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// GetResourceType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourceType(ctx context.Context, request api.GetResourceTypeRequestObject) (api.GetResourceTypeResponseObject, error) {
	records, err := r.Repo.GetResourceType(ctx, request.ResourceTypeId)
	if err != nil {
		return api.GetResourceType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(records) == 0 {
		return api.GetResourceType404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: "requested resourceType not found",
			Status: http.StatusNotFound,
		}, nil
	}

	object, err := models.ResourceTypeToModel(&records[0])
	if err != nil {
		slog.Error("error converting database records", "source", records[0])
		return api.GetResourceType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	return api.GetResourceType200JSONResponse(object), nil
}
