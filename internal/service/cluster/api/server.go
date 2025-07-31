/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	api "github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo"
	svcclusterutils "github.com/openshift-kni/oran-o2ims/internal/service/cluster/utils"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// ClusterServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ClusterServer)(nil)

// ClusterServerConfig defines the configuration attributes for the resource server
type ClusterServerConfig struct {
	svcutils.CommonServerConfig
	CloudID         string
	GlobalCloudID   string
	Extensions      []string
	ExternalAddress string
}

// ClusterServer defines the instance attributes for an instance of a cluster server
type ClusterServer struct {
	Config                   *ClusterServerConfig
	Repo                     repo.RepositoryInterface
	SubscriptionEventHandler notifier.SubscriptionEventHandler
}

// GetClusterResourceTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResourceTypes(ctx context.Context, request api.GetClusterResourceTypesRequestObject) (api.GetClusterResourceTypesResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ClusterResourceType{}); err != nil {
		return api.GetClusterResourceTypes400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetClusterResourceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resource types: %w", err)
	}

	objects := make([]api.ClusterResourceType, len(records))
	for i, record := range records {
		objects[i] = models.ClusterResourceTypeToModel(&record, options)
	}

	return api.GetClusterResourceTypes200JSONResponse(objects), nil
}

// GetClusterResourceType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResourceType(ctx context.Context, request api.GetClusterResourceTypeRequestObject) (api.GetClusterResourceTypeResponseObject, error) {
	record, err := r.Repo.GetClusterResourceType(ctx, request.ClusterResourceTypeId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	object := models.ClusterResourceTypeToModel(record, commonapi.NewDefaultFieldOptions())
	return api.GetClusterResourceType200JSONResponse(object), nil
}

// GetClusterResources receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResources(ctx context.Context, request api.GetClusterResourcesRequestObject) (api.GetClusterResourcesResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ClusterResource{}); err != nil {
		return api.GetClusterResources400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetClusterResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	objects := make([]api.ClusterResource, len(records))
	for i, record := range records {
		objects[i] = models.ClusterResourceToModel(&record, options)
	}

	return api.GetClusterResources200JSONResponse(objects), nil
}

// GetClusterResource receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetClusterResource(ctx context.Context, request api.GetClusterResourceRequestObject) (api.GetClusterResourceResponseObject, error) {
	record, err := r.Repo.GetClusterResource(ctx, request.ClusterResourceId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	object := models.ClusterResourceToModel(record, commonapi.NewDefaultFieldOptions())
	return api.GetClusterResource200JSONResponse(object), nil
}

// GetNodeClusterTypes receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusterTypes(ctx context.Context, request api.GetNodeClusterTypesRequestObject) (api.GetNodeClusterTypesResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.NodeClusterType{}); err != nil {
		return api.GetNodeClusterTypes400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetNodeClusterTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resource types: %w", err)
	}

	objects := make([]api.NodeClusterType, len(records))
	for i, record := range records {
		objects[i] = models.NodeClusterTypeToModel(&record, options)
	}

	return api.GetNodeClusterTypes200JSONResponse(objects), nil
}

// GetNodeClusterType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusterType(ctx context.Context, request api.GetNodeClusterTypeRequestObject) (api.GetNodeClusterTypeResponseObject, error) {
	record, err := r.Repo.GetNodeClusterType(ctx, request.NodeClusterTypeId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	object := models.NodeClusterTypeToModel(record, commonapi.NewDefaultFieldOptions())
	return api.GetNodeClusterType200JSONResponse(object), nil
}

// GetNodeClusterTypeAlarmDictionary receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusterTypeAlarmDictionary(ctx context.Context, request api.GetNodeClusterTypeAlarmDictionaryRequestObject) (api.GetNodeClusterTypeAlarmDictionaryResponseObject, error) {
	records, err := r.Repo.GetNodeClusterTypeAlarmDictionary(ctx, request.NodeClusterTypeId)
	if err != nil {
		return api.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterTypeId": request.NodeClusterTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(records) == 0 {
		return api.GetNodeClusterTypeAlarmDictionary404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterTypeId": request.NodeClusterTypeId.String(),
			},
			Detail: "requested NodeClusterType not found",
			Status: http.StatusNotFound,
		}, nil
	}

	// Safe to assume there is a single record since node_cluster_type_id is unique in the db
	dictionary := records[0]

	// Get alarm definitions
	definitions, err := r.Repo.GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictionary.AlarmDictionaryID)
	if err != nil {
		return api.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterTypeId": request.NodeClusterTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.AlarmDictionaryToModel(&dictionary, definitions)
	return api.GetNodeClusterTypeAlarmDictionary200JSONResponse(object), nil
}

// GetNodeClusters receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeClusters(ctx context.Context, request api.GetNodeClustersRequestObject) (api.GetNodeClustersResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.NodeCluster{}); err != nil {
		return api.GetNodeClusters400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetNodeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node clusters: %w", err)
	}

	// Retrieve the list of ClusterResourceID values per NodeCluster in one query so that we don't have to look it up
	// on a per NodeCluster basis.
	nodeClusterResourceIDs, err := r.Repo.GetNodeClusterResourceIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node cluster resource ids: %w", err)
	}

	// Store them in a map so they are easier to access for the next operation.
	mapper := make(map[uuid.UUID][]uuid.UUID)
	for _, entry := range nodeClusterResourceIDs {
		mapper[entry.NodeClusterID] = entry.ClusterResourceIDs
	}

	objects := make([]api.NodeCluster, len(records))
	for i, record := range records {
		objects[i] = models.NodeClusterToModel(&record, mapper[record.NodeClusterID], options)
	}

	return api.GetNodeClusters200JSONResponse(objects), nil
}

// GetNodeCluster receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetNodeCluster(ctx context.Context, request api.GetNodeClusterRequestObject) (api.GetNodeClusterResponseObject, error) {
	record, err := r.Repo.GetNodeCluster(ctx, request.NodeClusterId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	resources, err := r.Repo.GetNodeClusterResourceIDs(ctx, []any{record.NodeClusterID}...)
	if err != nil {
		return nil, fmt.Errorf("failed to get node cluster resource ids: %w", err)
	}

	var ids []uuid.UUID
	if len(resources) == 1 {
		ids = resources[0].ClusterResourceIDs
	} else if len(resources) > 1 {
		return api.GetNodeCluster500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"NodeClusterId": request.NodeClusterId.String(),
			},
			Detail: "Unexpected number of NodeCluster resources",
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.NodeClusterToModel(record, ids, commonapi.NewDefaultFieldOptions())
	return api.GetNodeCluster200JSONResponse(object), nil
}

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	version := svcclusterutils.CurrentVersion
	baseURL := constants.O2IMSClusterBaseURL
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
	version := svcclusterutils.CurrentVersion
	baseURL := constants.O2IMSClusterBaseURL
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
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.Subscription{}); err != nil {
		return api.GetSubscriptions400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

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
func (r *ClusterServer) validateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) error {
	if err := commonapi.ValidateCallbackURL(ctx, r.SubscriptionEventHandler.GetClientFactory(), request.Body.Callback); err != nil {
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
	if err := r.validateSubscription(ctx, request); err != nil {
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
	r.SubscriptionEventHandler.SubscriptionEvent(ctx, &notifier.SubscriptionEvent{
		Removed:      false,
		Subscription: models.SubscriptionToInfo(result),
	})

	response := models.SubscriptionToModel(result)
	return api.CreateSubscription201JSONResponse(response), nil
}

// GetSubscription receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	record, err := r.Repo.GetSubscription(ctx, request.SubscriptionId)
	if errors.Is(err, svcutils.ErrNotFound) {
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
	r.SubscriptionEventHandler.SubscriptionEvent(ctx, &notifier.SubscriptionEvent{
		Removed: true,
		Subscription: models.SubscriptionToInfo(&models2.Subscription{
			SubscriptionID: &request.SubscriptionId,
		}),
	})

	return api.DeleteSubscription200Response{}, nil
}

// GetAlarmDictionaries receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetAlarmDictionaries(ctx context.Context, request api.GetAlarmDictionariesRequestObject) (api.GetAlarmDictionariesResponseObject, error) {
	records, err := r.Repo.GetAlarmDictionaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm dictionaries: %w", err)
	}

	objects := make([]generated.AlarmDictionary, len(records))
	for i, record := range records {
		definitions, err := r.Repo.GetAlarmDefinitionsByAlarmDictionaryID(ctx, record.AlarmDictionaryID)
		if err != nil {
			return api.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{
				AdditionalAttributes: &map[string]string{
					"alarmDictionaryId": record.AlarmDictionaryID.String(),
				},
				Detail: err.Error(),
				Status: http.StatusInternalServerError,
			}, nil
		}

		objects[i] = models.AlarmDictionaryToModel(&record, definitions)
	}

	return api.GetAlarmDictionaries200JSONResponse(objects), nil
}

// GetAlarmDictionary receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ClusterServer) GetAlarmDictionary(ctx context.Context, request api.GetAlarmDictionaryRequestObject) (api.GetAlarmDictionaryResponseObject, error) {
	record, err := r.Repo.GetAlarmDictionary(ctx, request.AlarmDictionaryId)
	if errors.Is(err, svcutils.ErrNotFound) {
		return api.GetAlarmDictionary404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"alarmDictionaryId": request.AlarmDictionaryId.String(),
			},
			Detail: "requested AlarmDictionary not found",
			Status: http.StatusNotFound,
		}, nil
	}
	if err != nil {
		return api.GetAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"alarmDictionaryId": request.AlarmDictionaryId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	definitions, err := r.Repo.GetAlarmDefinitionsByAlarmDictionaryID(ctx, record.AlarmDictionaryID)
	if err != nil {
		return api.GetAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"alarmDictionaryId": request.AlarmDictionaryId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.AlarmDictionaryToModel(record, definitions)

	return api.GetAlarmDictionary200JSONResponse(object), nil
}
