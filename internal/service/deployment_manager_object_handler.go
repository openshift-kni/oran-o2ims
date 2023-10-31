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
)

// DeploymentManagerObjectHandlerBuilder contains the data and logic needed to create a new
// deployment manager object handler. Don't create instances of this type directly, use the
// NewDeploymentManagerObjectHandler function instead.
type DeploymentManagerObjectHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
}

// DeploymentManagerObjectHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewDeploymentManagerObjectHandler
// function instead.
type DeploymentManagerObjectHandler struct {
	logger        *slog.Logger
	cloudID       string
	backendURL    string
	backendToken  string
	backendClient *http.Client
	jsonAPI       jsoniter.API
}

// NewDeploymentManagerObjectHandler creates a builder that can then be used to configure and
// create a handler for an individual deployment manager.
func NewDeploymentManagerObjectHandler() *DeploymentManagerObjectHandlerBuilder {
	return &DeploymentManagerObjectHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *DeploymentManagerObjectHandlerBuilder) SetLogger(
	value *slog.Logger) *DeploymentManagerObjectHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *DeploymentManagerObjectHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *DeploymentManagerObjectHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *DeploymentManagerObjectHandlerBuilder) SetCloudID(
	value string) *DeploymentManagerObjectHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory..
func (b *DeploymentManagerObjectHandlerBuilder) SetBackendToken(
	value string) *DeploymentManagerObjectHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *DeploymentManagerObjectHandlerBuilder) SetBackendURL(
	value string) *DeploymentManagerObjectHandlerBuilder {
	b.backendURL = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *DeploymentManagerObjectHandlerBuilder) Build() (
	result *DeploymentManagerObjectHandler, err error) {
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
	result = &DeploymentManagerObjectHandler{
		logger:        b.logger,
		cloudID:       b.cloudID,
		backendURL:    b.backendURL,
		backendToken:  b.backendToken,
		backendClient: backendClient,
		jsonAPI:       jsonAPI,
	}
	return
}

// Get is part of the implementation of the collection handler interface.
func (h *DeploymentManagerObjectHandler) Get(ctx context.Context,
	request *ObjectRequest) (response *ObjectResponse, err error) {
	// Fetch the object:
	object, err := h.fetchObject(ctx, request.ID)
	if err != nil {
		return
	}

	// Transform the object into what we need:
	object, err = h.mapItem(ctx, object)

	// Return the result:
	response = &ObjectResponse{
		Object: object,
	}
	return
}

func (h *DeploymentManagerObjectHandler) fetchObject(ctx context.Context,
	id string) (result data.Object, err error) {
	url := fmt.Sprintf("%s/global-hub-api/v1/managedcluster/%s", h.backendURL, id)
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
	defer func() {
		err := response.Body.Close()
		if err != nil {
			h.logger.Error(
				"Failed to close response body",
				"error", err.Error(),
			)
		}
	}()
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
	reader := jsoniter.Parse(h.jsonAPI, response.Body, 4096)
	value := reader.Read()
	err = reader.Error
	if err != nil {
		return
	}
	switch typed := value.(type) {
	case data.Object:
		result = typed
	default:
		h.logger.Error(
			"Unexpected object type",
			"expected", fmt.Sprintf("%T", data.Object{}),
			"actual", fmt.Sprintf("%T", value),
		)
		err = fmt.Errorf(
			"expected object of type '%T' but received '%T'",
			data.Object{}, value,
		)
	}
	return
}

func (h *DeploymentManagerObjectHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {
	fromName, err := data.GetString(from, `$.metadata.name`)
	if err != nil {
		return
	}
	fromURL, err := data.GetString(from, `$.spec.managedClusterClientConfigs[0].url`)
	if err != nil {
		return
	}
	to = data.Object{
		"deploymentManagerId": fromName,
		"name":                fromName,
		"description":         fromName,
		"oCloudId":            h.cloudID,
		"serviceUri":          fromURL,
	}
	return
}
