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
	"fmt"
	"log/slog"
	"net/http"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// DeploymentManagerCollectionHandlerBuilder contains the data and logic needed to create a new
// deployment manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerCollectionHandler function instead.
type DeploymentManagerCollectionHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
}

// DeploymentManagerCollectionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewDeploymentManagerCollectionHandler
// function instead.
type DeploymentManagerCollectionHandler struct {
	logger            *slog.Logger
	cloudID           string
	backendURL        string
	backendToken      string
	backendClient     *http.Client
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
}

// NewDeploymentManagerCollectionHandler creates a builder that can then be used to configure
// and create a handler for the collection of deployment managers.
func NewDeploymentManagerCollectionHandler() *DeploymentManagerCollectionHandlerBuilder {
	return &DeploymentManagerCollectionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *DeploymentManagerCollectionHandlerBuilder) SetLogger(
	value *slog.Logger) *DeploymentManagerCollectionHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *DeploymentManagerCollectionHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *DeploymentManagerCollectionHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *DeploymentManagerCollectionHandlerBuilder) SetCloudID(
	value string) *DeploymentManagerCollectionHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory..
func (b *DeploymentManagerCollectionHandlerBuilder) SetBackendToken(
	value string) *DeploymentManagerCollectionHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *DeploymentManagerCollectionHandlerBuilder) SetBackendURL(
	value string) *DeploymentManagerCollectionHandlerBuilder {
	b.backendURL = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *DeploymentManagerCollectionHandlerBuilder) Build() (
	result *DeploymentManagerCollectionHandler, err error) {
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
	result = &DeploymentManagerCollectionHandler{
		logger:            b.logger,
		cloudID:           b.cloudID,
		backendURL:        b.backendURL,
		backendToken:      b.backendToken,
		backendClient:     backendClient,
		selectorEvaluator: selectorEvaluator,
		jsonAPI:           jsonAPI,
	}
	return
}

// Get is part of the implementation of the collection handler interface.
func (h *DeploymentManagerCollectionHandler) Get(ctx context.Context,
	request *CollectionRequest) (response *CollectionResponse, err error) {
	// Create the stream that will fetch the items:
	items, err := h.fetchItems(ctx)
	if err != nil {
		return
	}

	// Transform the items into what we need:
	items = data.Map(items, h.mapItem)

	// Select only the items that satisfy the filter:
	if request.Selector != nil {
		items = data.Select(
			items,
			func(ctx context.Context, item data.Object) (result bool, err error) {
				result, err = h.selectorEvaluator.Evaluate(ctx, request.Selector, item)
				return
			},
		)
	}

	// Return the result:
	response = &CollectionResponse{
		Items: items,
	}
	return
}

func (h *DeploymentManagerCollectionHandler) fetchItems(
	ctx context.Context) (result data.Stream, err error) {
	url := fmt.Sprintf("%s/global-hub-api/v1/managedclusters", h.backendURL)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", h.backendToken))
	request.Header.Set("Accept", "application/json")
	response, err := h.backendClient.Do(request)
	if err != nil {
		return
	}
	if response.StatusCode != http.StatusOK {
		h.logger.Error(
			"Received unexpected status code",
			"code", response.StatusCode,
			"url", url,
		)
		err = fmt.Errorf(
			"received unexpected status code %d from '%s'",
			response.StatusCode, url,
		)
		return
	}
	result, err = k8s.NewStream().
		SetLogger(h.logger).
		SetReader(response.Body).
		Build()
	return
}

func (h *DeploymentManagerCollectionHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {
	fromID, err := data.GetString(from, `$.metadata.labels["clusterID"]`)
	if err != nil {
		return
	}
	fromName, err := data.GetString(from, `$.metadata.name`)
	if err != nil {
		return
	}
	fromURL, err := data.GetString(from, `$.spec.managedClusterClientConfigs[0].url`)
	if err != nil {
		return
	}
	to = data.Object{
		"deploymentManagerId": fromID,
		"name":                fromName,
		"description":         fromName,
		"oCloudId":            h.cloudID,
		"serviceUri":          fromURL,
	}
	return
}
