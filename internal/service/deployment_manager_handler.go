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

	"github.com/itchyny/gojq"
	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// DeploymentManagerHandlerBuilder contains the data and logic needed to create a new deployment
// manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerHandler function instead.
type DeploymentManagerHandlerBuilder struct {
	logger           *slog.Logger
	transportWrapper func(http.RoundTripper) http.RoundTripper
	cloudID          string
	backendURL       string
	backendToken     string
}

// DeploymentManagerCollectionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewDeploymentManagerHandler function
// instead.
type DeploymentManagerHandler struct {
	logger            *slog.Logger
	cloudID           string
	backendURL        string
	backendToken      string
	backendClient     *http.Client
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
	fieldMappers      map[string]*gojq.Code
}

// NewDeploymentManagerHandler creates a builder that can then be used to configure and create a
// handler for the collection of deployment managers.
func NewDeploymentManagerHandler() *DeploymentManagerHandlerBuilder {
	return &DeploymentManagerHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *DeploymentManagerHandlerBuilder) SetLogger(
	value *slog.Logger) *DeploymentManagerHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *DeploymentManagerHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *DeploymentManagerHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *DeploymentManagerHandlerBuilder) SetCloudID(
	value string) *DeploymentManagerHandlerBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory..
func (b *DeploymentManagerHandlerBuilder) SetBackendToken(
	value string) *DeploymentManagerHandlerBuilder {
	b.backendToken = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *DeploymentManagerHandlerBuilder) SetBackendURL(
	value string) *DeploymentManagerHandlerBuilder {
	b.backendURL = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *DeploymentManagerHandlerBuilder) Build() (
	result *DeploymentManagerHandler, err error) {
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

	// Compile the field mappers:
	fieldMappers, err := b.compileFieldMappers()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &DeploymentManagerHandler{
		logger:            b.logger,
		cloudID:           b.cloudID,
		backendURL:        b.backendURL,
		backendToken:      b.backendToken,
		backendClient:     backendClient,
		selectorEvaluator: selectorEvaluator,
		jsonAPI:           jsonAPI,
		fieldMappers:      fieldMappers,
	}
	return
}

// compileFieldMappers compiles the jq queries that are used to calculate the values of fields. This
// compilation happens when the object is created so that we don't need to compile those queries
// with every request.
func (b *DeploymentManagerHandlerBuilder) compileFieldMappers() (result map[string]*gojq.Code,
	err error) {
	result = map[string]*gojq.Code{}
	for field, source := range deploymentManagerFieldMappers {
		var query *gojq.Query
		query, err = gojq.Parse(source)
		if err != nil {
			b.logger.Error(
				"Failed to parse mapping",
				slog.String("field", field),
				slog.String("source", source),
				slog.String("error", err.Error()),
			)
			return
		}
		var code *gojq.Code
		code, err = gojq.Compile(query, gojq.WithVariables([]string{"$cloud_id"}))
		if err != nil {
			b.logger.Error(
				"Failed to compile mapping",
				slog.String("field", field),
				slog.String("source", source),
				slog.String("error", err.Error()),
			)
			return
		}
		result[field] = code
	}
	return
}

// List is the implementation of the collection handler interface.
func (h *DeploymentManagerHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {
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
	response = &ListResponse{
		Items: items,
	}
	return
}

// Get is the implementation of the object handler interface.
func (h *DeploymentManagerHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {
	// Fetch the object:
	object, err := h.fetchItem(ctx, request.ID)
	if err != nil {
		return
	}

	// Transform the object into what we need:
	object, err = h.mapItem(ctx, object)

	// Return the result:
	response = &GetResponse{
		Object: object,
	}
	return
}

func (h *DeploymentManagerHandler) fetchItems(ctx context.Context) (result data.Stream,
	err error) {
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

func (h *DeploymentManagerHandler) fetchItem(ctx context.Context, id string) (result data.Object,
	err error) {
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

func (h *DeploymentManagerHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {
	to = data.Object{}
	for name, code := range h.fieldMappers {
		iterator := code.RunWithContext(ctx, from, h.cloudID)
		value, ok := iterator.Next()
		if !ok {
			continue
		}
		to[name] = value
	}
	return
}

// deploymentManagerFieldMappers contains the correspondence between fields of the output objects
// and the jq queries that are used to extract their value from the input object.
var deploymentManagerFieldMappers = map[string]string{
	"deploymentManagerId": `.metadata.labels["clusterID"]`,
	"name":                `.metadata.name`,
	"description":         `.metadata.name`,
	"serviceUri":          `.spec.managedClusterClientConfigs[0].url`,
	"oCloudId":            `$cloud_id`,
}
