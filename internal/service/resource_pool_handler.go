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
	"github.com/openshift-kni/oran-o2ims/internal/search"
	"github.com/openshift-kni/oran-o2ims/internal/searchapi"
)

// ResourcePoolHandlerBuilder contains the data and logic needed to create a new resource pool
// collection handler. Don't create instances of this type directly, use the NewResourcePoolHandler
// function instead.
type ResourcePoolHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
	graphqlQuery     string
	graphqlVars      *searchapi.SearchInput
}

// ResourcePoolHandler knows how to respond to requests to list resource pools. Don't create
// instances of this type directly, use the NewResourcePoolHandler function instead.
type ResourcePoolHandler struct {
	logger            *slog.Logger
	transportWrapper  func(http.RoundTripper) http.RoundTripper
	cloudID           string
	backendURL        string
	backendToken      string
	backendClient     *http.Client
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
	graphqlQuery      string
	graphqlVars       *searchapi.SearchInput
}

// NewResourcePoolHandler creates a builder that can then be used to configure and create a
// handler for the collection of resource pools.
func NewResourcePoolHandler() *ResourcePoolHandlerBuilder {
	return &ResourcePoolHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *ResourcePoolHandlerBuilder) SetLogger(
	value *slog.Logger) *ResourcePoolHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *ResourcePoolHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *ResourcePoolHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *ResourcePoolHandlerBuilder) SetCloudID(
	value string) *ResourcePoolHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory.
func (b *ResourcePoolHandlerBuilder) SetBackendToken(
	value string) *ResourcePoolHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *ResourcePoolHandlerBuilder) SetBackendURL(
	value string) *ResourcePoolHandlerBuilder {
	b.backendURL = value
	return b
}

// SetGraphqlQuery sets the query to send to the search API server.
func (b *ResourcePoolHandlerBuilder) SetGraphqlQuery(
	value string) *ResourcePoolHandlerBuilder {
	b.graphqlQuery = value
	return b
}

// SetGraphqlVars sets the query vars to send to the search API server.
func (b *ResourcePoolHandlerBuilder) SetGraphqlVars(
	value *searchapi.SearchInput) *ResourcePoolHandlerBuilder {
	b.graphqlVars = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *ResourcePoolHandlerBuilder) Build() (
	result *ResourcePoolHandler, err error) {
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

	// Create the filter expression evaluator:
	pathEvaluator, err := search.NewPathEvaluator().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}
	selectorEvaluator, err := search.NewSelectorEvaluator().
		SetLogger(b.logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &ResourcePoolHandler{
		logger:            b.logger,
		transportWrapper:  b.transportWrapper,
		cloudID:           b.cloudID,
		backendURL:        b.backendURL,
		backendToken:      b.backendToken,
		backendClient:     backendClient,
		selectorEvaluator: selectorEvaluator,
		jsonAPI:           jsonAPI,
		graphqlQuery:      b.graphqlQuery,
		graphqlVars:       b.graphqlVars,
	}
	return
}

// List is part of the implementation of the collection handler interface.
func (h *ResourcePoolHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {

	// Transform the items into what we need:
	resourcePools, err := h.fetchItems(ctx)
	if err != nil {
		return
	}

	// Select only the items that satisfy the filter:
	if request.Selector != nil {
		resourcePools = data.Select(
			resourcePools,
			func(ctx context.Context, item data.Object) (result bool, err error) {
				result, err = h.selectorEvaluator.Evaluate(ctx, request.Selector, item)
				return
			},
		)
	}

	// Return the result:
	response = &ListResponse{
		Items: resourcePools,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *ResourcePoolHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {
	resourcePoolFetcher, err := NewResourcePoolFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetGraphqlQuery(h.graphqlQuery).
		SetGraphqlVars(h.graphqlVars).
		Build()
	if err != nil {
		return
	}

	// Fetch the object:
	resourcePool, err := h.fetchItem(ctx, request.ID, resourcePoolFetcher)
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: resourcePool,
	}
	return
}

func (h *ResourcePoolHandler) fetchItems(
	ctx context.Context) (result data.Stream, err error) {
	resourcePoolFetcher, err := NewResourcePoolFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetGraphqlQuery(h.graphqlQuery).
		SetGraphqlVars(h.graphqlVars).
		Build()
	if err != nil {
		return
	}
	return resourcePoolFetcher.FetchItems(ctx)
}

func (h *ResourcePoolHandler) fetchItem(ctx context.Context,
	id string, resourcePoolFetcher *ResourcePoolFetcher) (resourcePool data.Object, err error) {
	// Filter results by 'cluster' property
	h.graphqlVars.Filters = append(h.graphqlVars.Filters, &searchapi.SearchFilter{
		Property: "cluster",
		Values:   []*string{&id},
	})

	// Fetch resource pools
	resourcePools, err := resourcePoolFetcher.FetchItems(ctx)
	if err != nil {
		return
	}

	// Get first result
	resourcePool, err = resourcePools.Next(ctx)

	return
}
