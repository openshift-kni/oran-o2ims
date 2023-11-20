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

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/searchapi"
)

// ResourcePoolObjectHandlerBuilder contains the data and logic needed to create a new
// deployment manager object handler. Don't create instances of this type directly, use the
// NewResourcePoolObjectHandler function instead.
type ResourcePoolObjectHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
	graphqlQuery     string
	graphqlVars      *searchapi.SearchInput
}

// NewResourcePoolObjectHandler knows how to respond to requests to list resource pool.
// Don't create instances of this type directly, use the NewResourcePoolObjectHandler
// function instead.
type ResourcePoolObjectHandler struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
	backendClient    *http.Client
	graphqlQuery     string
	graphqlVars      *searchapi.SearchInput
}

// NewResourcePoolObjectHandler creates a builder that can then be used to configure and
// create a handler for an individual resource pool.
func NewResourcePoolObjectHandler() *ResourcePoolObjectHandlerBuilder {
	return &ResourcePoolObjectHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *ResourcePoolObjectHandlerBuilder) SetLogger(
	value *slog.Logger) *ResourcePoolObjectHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *ResourcePoolObjectHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *ResourcePoolObjectHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *ResourcePoolObjectHandlerBuilder) SetCloudID(
	value string) *ResourcePoolObjectHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory..
func (b *ResourcePoolObjectHandlerBuilder) SetBackendToken(
	value string) *ResourcePoolObjectHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *ResourcePoolObjectHandlerBuilder) SetBackendURL(
	value string) *ResourcePoolObjectHandlerBuilder {
	b.backendURL = value
	return b
}

// SetGraphqlQuery sets the query to send to the search API server.
func (b *ResourcePoolObjectHandlerBuilder) SetGraphqlQuery(
	value string) *ResourcePoolObjectHandlerBuilder {
	b.graphqlQuery = value
	return b
}

// SetGraphqlVars sets the query vars to send to the search API server.
func (b *ResourcePoolObjectHandlerBuilder) SetGraphqlVars(
	value *searchapi.SearchInput) *ResourcePoolObjectHandlerBuilder {
	b.graphqlVars = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *ResourcePoolObjectHandlerBuilder) Build() (
	result *ResourcePoolObjectHandler, err error) {
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

	// Create and populate the object:
	result = &ResourcePoolObjectHandler{
		logger:           b.logger,
		transportWrapper: b.transportWrapper,
		cloudID:          b.cloudID,
		backendURL:       b.backendURL,
		backendToken:     b.backendToken,
		backendClient:    backendClient,
		graphqlQuery:     b.graphqlQuery,
		graphqlVars:      b.graphqlVars,
	}
	return
}

// Get is part of the implementation of the collection handler interface.
func (h *ResourcePoolObjectHandler) Get(ctx context.Context,
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
	resourcePool, err := h.fetchObject(ctx, request.ID, resourcePoolFetcher)
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: resourcePool,
	}
	return
}

func (h *ResourcePoolObjectHandler) fetchObject(ctx context.Context,
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
