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
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/cache"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
	svcresourceutils "github.com/openshift-kni/oran-o2ims/internal/service/resources/utils"
)

// ResourceServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ResourceServer)(nil)

// ResourceServerConfig defines the configuration attributes for the resource server
type ResourceServerConfig struct {
	svcutils.CommonServerConfig
	CloudID         string
	GlobalCloudID   string
	Extensions      []string
	ExternalAddress string
}

// AlarmDictData holds pre-built alarm dictionary API models with lookup
// indexes for serving from memory.
type AlarmDictData struct {
	All          []generated.AlarmDictionary
	ByID         map[uuid.UUID]*generated.AlarmDictionary
	ByResourceID map[uuid.UUID]*generated.AlarmDictionary
}

// ResourceServer defines the instance attributes for an instance of a resource server
type ResourceServer struct {
	Config                   *ResourceServerConfig
	Info                     api.OCloudInfo
	Repo                     repo.ResourcesRepositoryInterface
	SubscriptionEventHandler notifier.SubscriptionEventHandler
	AlarmDicts               *cache.Entry[AlarmDictData]
}

// InitAlarmDictCache initializes the alarm dictionary cache with the
// appropriate loader. Must be called before serving API requests.
// TTL is 0 (no expiration) because the PG listener's resource_type_changed
// handler and its 15-minute catch-up sync both call InvalidateAlarmDictCache,
// providing the staleness bound externally.
func (r *ResourceServer) InitAlarmDictCache() {
	r.AlarmDicts = cache.NewEntry("alarm-dictionaries", 0, func(ctx context.Context) (AlarmDictData, error) {
		records, err := r.Repo.GetAlarmDictionaries(ctx)
		if err != nil {
			return AlarmDictData{}, fmt.Errorf("failed to get alarm dictionaries: %w", err)
		}

		data := AlarmDictData{
			All:          make([]generated.AlarmDictionary, len(records)),
			ByID:         make(map[uuid.UUID]*generated.AlarmDictionary, len(records)),
			ByResourceID: make(map[uuid.UUID]*generated.AlarmDictionary, len(records)),
		}

		for i, record := range records {
			definitions, err := r.Repo.GetAlarmDefinitionsByAlarmDictionaryID(ctx, record.AlarmDictionaryID)
			if err != nil {
				return AlarmDictData{}, fmt.Errorf("failed to get alarm definitions for dictionary %s: %w", record.AlarmDictionaryID, err)
			}

			data.All[i] = models.AlarmDictionaryToModel(&record, definitions)
			data.ByID[record.AlarmDictionaryID] = &data.All[i]
			data.ByResourceID[record.ResourceTypeID] = &data.All[i]
		}

		return data, nil
	})
}

// InvalidateAlarmDictCache clears the cached alarm dictionaries so they
// will be reloaded from the database on the next API request.
func (r *ResourceServer) InvalidateAlarmDictCache() {
	if r.AlarmDicts != nil {
		r.AlarmDicts.Invalidate()
	}
}

func (r *ResourceServer) GetAlarmDictionaries(ctx context.Context, _ api.GetAlarmDictionariesRequestObject) (api.GetAlarmDictionariesResponseObject, error) {
	data, err := r.AlarmDicts.Get(ctx)
	if err != nil {
		return api.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	return api.GetAlarmDictionaries200JSONResponse(data.All), nil
}

func (r *ResourceServer) GetAlarmDictionary(ctx context.Context, request api.GetAlarmDictionaryRequestObject) (api.GetAlarmDictionaryResponseObject, error) {
	data, err := r.AlarmDicts.Get(ctx)
	if err != nil {
		return api.GetAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"alarmDictionaryId": request.AlarmDictionaryId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	dict, found := data.ByID[request.AlarmDictionaryId]
	if !found {
		return api.GetAlarmDictionary404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"alarmDictionaryId": request.AlarmDictionaryId.String(),
			},
			Detail: "requested AlarmDictionary not found",
			Status: http.StatusNotFound,
		}, nil
	}

	return api.GetAlarmDictionary200JSONResponse(*dict), nil
}

func (r *ResourceServer) GetResourceTypeAlarmDictionary(ctx context.Context, request api.GetResourceTypeAlarmDictionaryRequestObject) (api.GetResourceTypeAlarmDictionaryResponseObject, error) {
	data, err := r.AlarmDicts.Get(ctx)
	if err != nil {
		return api.GetResourceTypeAlarmDictionary500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	dict, found := data.ByResourceID[request.ResourceTypeId]
	if !found {
		return api.GetResourceTypeAlarmDictionary404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: "requested ResourceType not found",
			Status: http.StatusNotFound,
		}, nil
	}

	return api.GetResourceTypeAlarmDictionary200JSONResponse(*dict), nil
}

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	version := svcresourceutils.CurrentInventoryVersion
	baseURL := constants.O2IMSInventoryBaseURL
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
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.OCloudInfo{}); err != nil {
		return api.GetCloudInfo400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	return api.GetCloudInfo200JSONResponse(r.Info), nil
}

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	version := svcresourceutils.CurrentInventoryVersion
	baseURL := constants.O2IMSInventoryBaseURL
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
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.DeploymentManager{}); err != nil {
		return api.GetDeploymentManagers400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetDeploymentManagers(ctx)
	if err != nil {
		return api.GetDeploymentManagers500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get deployment managers: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.DeploymentManager, len(records))
	for i, record := range records {
		objects[i] = models.DeploymentManagerToModel(&record, options)
	}

	return api.GetDeploymentManagers200JSONResponse(objects), nil
}

// GetDeploymentManager receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetDeploymentManager(ctx context.Context, request api.GetDeploymentManagerRequestObject) (api.GetDeploymentManagerResponseObject, error) {
	record, err := r.Repo.GetDeploymentManager(ctx, request.DeploymentManagerId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	object := models.DeploymentManagerToModel(record, commonapi.NewDefaultFieldOptions())
	return api.GetDeploymentManager200JSONResponse(object), nil
}

// GetSubscriptions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.Subscription{}); err != nil {
		return api.GetSubscriptions400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetSubscriptions(ctx)
	if err != nil {
		return api.GetSubscriptions500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get subscriptions: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.Subscription, len(records))
	for i, record := range records {
		objects[i] = models.SubscriptionToModel(&record)
	}

	return api.GetSubscriptions200JSONResponse(objects), nil
}

// validateSubscription validates a subscription before accepting the request
func (r *ResourceServer) validateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) error {
	if err := commonapi.ValidateCallbackURL(ctx, r.SubscriptionEventHandler.GetClientFactory(), request.Body.Callback); err != nil {
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
func (r *ResourceServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
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
	r.SubscriptionEventHandler.SubscriptionEvent(ctx, &notifier.SubscriptionEvent{
		Removed: true,
		Subscription: models.SubscriptionToInfo(&models2.Subscription{
			SubscriptionID: &request.SubscriptionId,
		}),
	})

	return api.DeleteSubscription200Response{}, nil
}

// GetResourcePools receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePools(ctx context.Context, request api.GetResourcePoolsRequestObject) (api.GetResourcePoolsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ResourcePool{}); err != nil {
		return api.GetResourcePools400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetResourcePools(ctx)
	if err != nil {
		return api.GetResourcePools500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to retrieve resource pools: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.ResourcePool, len(records))
	for i, record := range records {
		objects[i] = models.ResourcePoolToModel(&record, options)
	}

	return api.GetResourcePools200JSONResponse(objects), nil
}

// GetResourcePool receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourcePool(ctx context.Context, request api.GetResourcePoolRequestObject) (api.GetResourcePoolResponseObject, error) {
	record, err := r.Repo.GetResourcePool(ctx, request.ResourcePoolId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	object := models.ResourcePoolToModel(record, commonapi.NewDefaultFieldOptions())
	return api.GetResourcePool200JSONResponse(object), nil
}

// GetResources receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResources(ctx context.Context, request api.GetResourcesRequestObject) (api.GetResourcesResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.Resource{}); err != nil {
		return api.GetResources400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

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
	if errors.Is(err, svcutils.ErrNotFound) {
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
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ResourceType{}); err != nil {
		return api.GetResourceTypes400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetResourceTypes(ctx)
	if err != nil {
		return api.GetResourceTypes500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get resource types: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Fetch all alarm dictionaries and build a map of resourceTypeId -> alarmDictionaryId
	alarmDictionaries, err := r.Repo.GetAlarmDictionaries(ctx)
	if err != nil {
		return api.GetResourceTypes500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get alarm dictionaries: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	alarmDictionaryIDMap := make(map[string]*uuid.UUID)
	for i := range alarmDictionaries {
		alarmDictionaryIDMap[alarmDictionaries[i].ResourceTypeID.String()] = &alarmDictionaries[i].AlarmDictionaryID
	}

	objects := make([]api.ResourceType, len(records))
	for i, record := range records {
		alarmDictionaryID := alarmDictionaryIDMap[record.ResourceTypeID.String()]
		objects[i] = models.ResourceTypeToModel(&record, alarmDictionaryID)
	}

	return api.GetResourceTypes200JSONResponse(objects), nil
}

// GetResourceType receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetResourceType(ctx context.Context, request api.GetResourceTypeRequestObject) (api.GetResourceTypeResponseObject, error) {
	record, err := r.Repo.GetResourceType(ctx, request.ResourceTypeId)
	if errors.Is(err, svcutils.ErrNotFound) {
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

	// Fetch alarm dictionary ID for this resource type
	var alarmDictionaryID *uuid.UUID
	dictionaries, err := r.Repo.GetResourceTypeAlarmDictionary(ctx, request.ResourceTypeId)
	if err != nil {
		return api.GetResourceType500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceTypeId": request.ResourceTypeId.String(),
			},
			Detail: fmt.Sprintf("failed to get alarm dictionary: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	if len(dictionaries) > 0 {
		alarmDictionaryID = &dictionaries[0].AlarmDictionaryID
	}

	object := models.ResourceTypeToModel(record, alarmDictionaryID)
	return api.GetResourceType200JSONResponse(object), nil
}

// GetInternalResourceById receives the API request to this endpoint, executes the request, and responds appropriately
// This internal endpoint returns a resource by ID without needing to know the resource pool
func (r *ResourceServer) GetInternalResourceById(ctx context.Context, request api.GetInternalResourceByIdRequestObject) (api.GetInternalResourceByIdResponseObject, error) {
	record, err := r.Repo.GetResource(ctx, request.ResourceId)
	if errors.Is(err, svcutils.ErrNotFound) {
		return api.GetInternalResourceById404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceId": request.ResourceId.String(),
			},
			Detail: "requested resource not found",
			Status: http.StatusNotFound,
		}, nil
	}
	if err != nil {
		return api.GetInternalResourceById500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"resourceId": request.ResourceId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.ResourceToModel(record, nil)
	return api.GetInternalResourceById200JSONResponse(object), nil
}

// GetLocations receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetLocations(ctx context.Context, request api.GetLocationsRequestObject) (api.GetLocationsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.LocationInfo{}); err != nil {
		return api.GetLocations400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetLocations(ctx)
	if err != nil {
		return api.GetLocations500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get locations: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	siteIDsByLocation, err := r.Repo.GetAllOCloudSiteIDsByLocation(ctx)
	if err != nil {
		return api.GetLocations500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get O-Cloud sites: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.LocationInfo, len(records))
	for i, record := range records {
		siteIDs := siteIDsByLocation[record.GlobalLocationID] // nil if no sites, which is fine
		objects[i] = models.LocationToModel(&record, siteIDs)
	}

	return api.GetLocations200JSONResponse(objects), nil
}

// GetLocation receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetLocation(ctx context.Context, request api.GetLocationRequestObject) (api.GetLocationResponseObject, error) {
	record, err := r.Repo.GetLocation(ctx, request.GlobalLocationId)
	if errors.Is(err, svcutils.ErrNotFound) {
		return api.GetLocation404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"globalLocationId": request.GlobalLocationId,
			},
			Detail: "requested location not found",
			Status: http.StatusNotFound,
		}, nil
	}
	if err != nil {
		return api.GetLocation500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"globalLocationId": request.GlobalLocationId,
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Fetch O-Cloud Site IDs for this location
	siteIDs, err := r.Repo.GetOCloudSiteIDsForLocation(ctx, record.GlobalLocationID)
	if err != nil {
		return api.GetLocation500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"globalLocationId": request.GlobalLocationId,
			},
			Detail: fmt.Sprintf("failed to get O-Cloud sites for location: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.LocationToModel(record, siteIDs)
	return api.GetLocation200JSONResponse(object), nil
}

// GetOCloudSites receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetOCloudSites(ctx context.Context, request api.GetOCloudSitesRequestObject) (api.GetOCloudSitesResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.OCloudSiteInfo{}); err != nil {
		return api.GetOCloudSites400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	records, err := r.Repo.GetOCloudSites(ctx)
	if err != nil {
		return api.GetOCloudSites500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get O-Cloud sites: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	poolIDsBySite, err := r.Repo.GetAllResourcePoolIDsBySite(ctx)
	if err != nil {
		return api.GetOCloudSites500ApplicationProblemPlusJSONResponse{
			Detail: fmt.Sprintf("failed to get resource pools: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	objects := make([]api.OCloudSiteInfo, len(records))
	for i, record := range records {
		poolIDs := poolIDsBySite[record.OCloudSiteID] // nil if no pools, which is fine
		objects[i] = models.OCloudSiteToModel(&record, poolIDs)
	}

	return api.GetOCloudSites200JSONResponse(objects), nil
}

// GetOCloudSite receives the API request to this endpoint, executes the request, and responds appropriately
func (r *ResourceServer) GetOCloudSite(ctx context.Context, request api.GetOCloudSiteRequestObject) (api.GetOCloudSiteResponseObject, error) {
	record, err := r.Repo.GetOCloudSite(ctx, request.OCloudSiteId)
	if errors.Is(err, svcutils.ErrNotFound) {
		return api.GetOCloudSite404ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"oCloudSiteId": request.OCloudSiteId.String(),
			},
			Detail: "requested O-Cloud site not found",
			Status: http.StatusNotFound,
		}, nil
	}
	if err != nil {
		return api.GetOCloudSite500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"oCloudSiteId": request.OCloudSiteId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}, nil
	}

	// Fetch Resource Pool IDs for this site
	poolIDs, err := r.Repo.GetResourcePoolIDsForSite(ctx, record.OCloudSiteID)
	if err != nil {
		return api.GetOCloudSite500ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"oCloudSiteId": request.OCloudSiteId.String(),
			},
			Detail: fmt.Sprintf("failed to get resource pools for site: %s", err.Error()),
			Status: http.StatusInternalServerError,
		}, nil
	}

	object := models.OCloudSiteToModel(record, poolIDs)
	return api.GetOCloudSite200JSONResponse(object), nil
}
