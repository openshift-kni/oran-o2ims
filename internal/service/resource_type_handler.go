/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"

	jsoniter "github.com/json-iterator/go"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/files"
	"github.com/openshift-kni/oran-o2ims/internal/model"
)

const (
	ResourceClassCompute    = "COMPUTE"
	ResourceClassNetworking = "NETWORKING"
	ResourceClassStorage    = "STORAGE"
	ResourceClassUndefined  = "UNDEFINED"

	ResourceKindPhysical  = "PHYSICAL"
	ResourceKindLogical   = "LOGICAL"
	ResourcekindUndefined = "UNDEFINED"
)

// ResourceTypeHandlerBuilder contains the data and logic needed to create a new resource type
// collection handler. Don't create instances of this type directly, use the NewResourceTypeHandler
// function instead.
type ResourceTypeHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
	graphqlQuery     string
	graphqlVars      *model.SearchInput
}

// ResourceTypeHandler knows how to respond to requests to list resource types. Don't create
// instances of this type directly, use the NewResourceTypeHandler function instead.
type ResourceTypeHandler struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
	backendClient    *http.Client
	jsonAPI          jsoniter.API
	graphqlQuery     string
	graphqlVars      *model.SearchInput
	resourceFetcher  *ResourceFetcher
}

// NewResourceTypeHandler creates a builder that can then be used to configure and create a
// handler for the collection of resource types.
func NewResourceTypeHandler() *ResourceTypeHandlerBuilder {
	return &ResourceTypeHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *ResourceTypeHandlerBuilder) SetLogger(
	value *slog.Logger) *ResourceTypeHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *ResourceTypeHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *ResourceTypeHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *ResourceTypeHandlerBuilder) SetCloudID(
	value string) *ResourceTypeHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *ResourceTypeHandlerBuilder) SetBackendToken(
	value string) *ResourceTypeHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory.
func (b *ResourceTypeHandlerBuilder) SetBackendURL(
	value string) *ResourceTypeHandlerBuilder {
	b.backendURL = value
	return b
}

// SetGraphqlQuery sets the query to send to the search API server.
func (b *ResourceTypeHandlerBuilder) SetGraphqlQuery(
	value string) *ResourceTypeHandlerBuilder {
	b.graphqlQuery = value
	return b
}

// SetGraphqlVars sets the query vars to send to the search API server.
func (b *ResourceTypeHandlerBuilder) SetGraphqlVars(
	value *model.SearchInput) *ResourceTypeHandlerBuilder {
	b.graphqlVars = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *ResourceTypeHandlerBuilder) Build() (
	result *ResourceTypeHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.cloudID == "" {
		err = errors.New("cloud identifier is mandatory")
		return
	}
	if b.backendURL == "" {
		err = errors.New("backend URL is mandatory")
		return
	}
	if b.backendToken == "" {
		err = errors.New("backend token is mandatory")
		return
	}

	// Create the HTTP client that we will use to connect to the backend:
	var backendTransport http.RoundTripper
	backendTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	if b.transportWrapper != nil {
		backendTransport = b.transportWrapper(backendTransport)
	}
	backendClient := &http.Client{
		Transport: backendTransport,
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	result = &ResourceTypeHandler{
		logger:           b.logger,
		transportWrapper: b.transportWrapper,
		cloudID:          b.cloudID,
		backendURL:       b.backendURL,
		backendToken:     b.backendToken,
		backendClient:    backendClient,
		jsonAPI:          jsonAPI,
		graphqlQuery:     b.graphqlQuery,
		graphqlVars:      b.graphqlVars,
	}
	return
}

// List is part of the implementation of the collection handler interface.
func (h *ResourceTypeHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {

	// Transform the items into what we need:
	resourceTypes, err := h.fetchItems(ctx)
	if err != nil {
		return
	}

	// Return the result:
	response = &ListResponse{
		Items: resourceTypes,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *ResourceTypeHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	resourceFetcher, err := NewResourceFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetGraphqlQuery(h.graphqlQuery).
		SetGraphqlVars(h.getGraphqlVars()).
		Build()
	if err != nil {
		return
	}

	// Fetch the object:
	resourceType, err := h.fetchItem(ctx, request.Variables[0], resourceFetcher)
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: resourceType,
	}

	return
}

func (h *ResourceTypeHandler) fetchItems(
	ctx context.Context) (result data.Stream, err error) {
	h.resourceFetcher, err = NewResourceFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetGraphqlQuery(h.graphqlQuery).
		SetGraphqlVars(h.getGraphqlVars()).
		Build()
	if err != nil {
		return
	}

	items, err := h.resourceFetcher.FetchItems(ctx)
	if err != nil {
		return
	}

	// Transform Items to Resources
	resources := data.Map(items, h.mapItem)

	// TODO: find a better solution to remove duplicates
	resSlice, err := data.Collect(ctx, resources)
	result = data.Pour(h.getUniqueSlice(resSlice)...)

	return
}

func (h *ResourceTypeHandler) getUniqueSlice(items []data.Object) []data.Object {
	allKeys := make(map[string]bool)
	list := []data.Object{}
	for _, item := range items {
		id := item["resourceTypeID"].(string)
		if _, value := allKeys[id]; !value {
			allKeys[id] = true
			list = append(list, item)
		}
	}
	return list
}

func (h *ResourceTypeHandler) fetchItem(ctx context.Context,
	id string, resourceFetcher *ResourceFetcher) (resourceType data.Object, err error) {
	// Fetch items
	items, err := resourceFetcher.FetchItems(ctx)
	if err != nil {
		return
	}

	// Transform Items to ResourcesTypes
	resourceTypes := data.Map(items, h.mapItem)

	// Filter by ID
	resourceTypes = data.Select(
		resourceTypes,
		func(ctx context.Context, item data.Object) (result bool, err error) {
			result = item["resourceTypeID"] == id
			return
		},
	)

	// Get first result
	resourceType, err = resourceTypes.Next(ctx)

	return
}

func (h *ResourceTypeHandler) getGraphqlVars() (graphqlVars *model.SearchInput) {
	graphqlVars = &model.SearchInput{}
	graphqlVars.Keywords = h.graphqlVars.Keywords
	graphqlVars.Filters = h.graphqlVars.Filters

	// Filter results without '_systemUUID' property (could happen with stale objects)
	nonEmpty := "!"
	graphqlVars.Filters = append(graphqlVars.Filters, &model.SearchFilter{
		Property: "_systemUUID",
		Values:   []*string{&nonEmpty},
	})

	return
}

func (h *ResourceTypeHandler) getAlarmDictionary(ctx context.Context, resourceClass string) (result data.Object, err error) {
	handler, err := NewAlarmDefinitionHandler().
		SetLogger(h.logger).
		Build()
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to create handler",
			"error", err,
		)
		return
	}

	response, err := handler.List(ctx, nil)
	if err != nil {
		return
	}

	// Filter by 'resourceClass'
	definitions := data.Select(
		response.Items,
		func(ctx context.Context, item data.Object) (result bool, err error) {
			itemResourceClass, err := data.GetString(item, "alarmAdditionalFields.resourceClass")
			if err != nil {
				// Missing optional 'resourceClass' property in item
				return false, nil
			}
			result = itemResourceClass == resourceClass

			return
		},
	)

	alarmDefinitions, err := data.Collect(ctx, definitions)
	if err != nil {
		return
	}

	result = data.Object{
		"alarmDefinition":        alarmDefinitions,
		"pkNotificationField":    "alarmDefinitionID",
		"managementInterfaceId":  "O2IMS",
		"alarmDictionaryVersion": files.AlarmDictionaryVersion,
		// Constant string value - not yet defined by O2IMS spec
		"alarmDictionarySchemaVersion": "",
		// TODO: no direct mapping
		"entityType": "",
		"vendor":     "",
	}

	return
}

// Map an item to an O2 Resource object.
func (h *ResourceTypeHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {
	kind, err := data.GetString(from, "kind")
	if err != nil {
		return
	}

	var resourceClass, resourceKind, resourceTypeID string
	switch kind {
	case KindNode:
		resourceClass = ResourceClassCompute
		resourceKind = ResourceKindPhysical
		resourceTypeID, err = h.resourceFetcher.GetResourceTypeID(from)
		if err != nil {
			return
		}
	}

	alarmDictionary, err := h.getAlarmDictionary(ctx, resourceClass)
	if err != nil {
		return
	}

	to = data.Object{
		"resourceTypeID":  resourceTypeID,
		"name":            resourceTypeID,
		"resourceKind":    resourceKind,
		"resourceClass":   resourceClass,
		"alarmDictionary": alarmDictionary,
		// TODO: no direct mapping
		"extensions": "",
		"vendor":     "",
		"model":      "",
		"version":    "",
	}
	return
}
