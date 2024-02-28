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
	"slices"

	jsoniter "github.com/json-iterator/go"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// AlarmHandlerBuilder contains the data and logic needed to create a new alarm
// collection handler. Don't create instances of this type directly, use the NewAlarmHandler
// function instead.
type AlarmHandlerBuilder struct {
	logger              *slog.Logger
	transportWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID             string
	extensions          []string
	backendURL          string
	backendToken        string
	resourceServerURL   string
	resourceServerToken string
}

// AlarmHandler knows how to respond to requests to list alarms. Don't create
// instances of this type directly, use the NewAlarmHandler function instead.
type AlarmHandler struct {
	logger              *slog.Logger
	transportWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID             string
	extensions          []string
	backendURL          string
	backendToken        string
	backendClient       *http.Client
	resourceServerURL   string
	resourceServerToken string
	jsonAPI             jsoniter.API
	selectorEvaluator   *search.SelectorEvaluator
	alarmFetcher        *AlarmFetcher
}

// NewAlarmHandler creates a builder that can then be used to configure and create a
// handler for the collection of alarms.
func NewAlarmHandler() *AlarmHandlerBuilder {
	return &AlarmHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmHandlerBuilder) SetLogger(
	value *slog.Logger) *AlarmHandlerBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *AlarmHandlerBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *AlarmHandlerBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *AlarmHandlerBuilder) SetCloudID(
	value string) *AlarmHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *AlarmHandlerBuilder) SetExtensions(values ...string) *AlarmHandlerBuilder {
	b.extensions = values
	return b
}

// SetBackendURL sets the URL of the backend server. This is mandatory.
func (b *AlarmHandlerBuilder) SetBackendURL(
	value string) *AlarmHandlerBuilder {
	b.backendURL = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate
// with the backend server. This is mandatory.
func (b *AlarmHandlerBuilder) SetBackendToken(
	value string) *AlarmHandlerBuilder {
	b.backendToken = value
	return b
}

// SetResourceServerURL sets the URL of the resource server. This is mandatory.
// The resource server is used for mapping Alarms to Resources.
func (b *AlarmHandlerBuilder) SetResourceServerURL(
	value string) *AlarmHandlerBuilder {
	b.resourceServerURL = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate
// with to the resource server. This is mandatory.
func (b *AlarmHandlerBuilder) SetResourceServerToken(
	value string) *AlarmHandlerBuilder {
	b.resourceServerToken = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *AlarmHandlerBuilder) Build() (
	result *AlarmHandler, err error) {
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
	if b.resourceServerURL == "" {
		err = errors.New("resource server URL is mandatory")
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
	result = &AlarmHandler{
		logger:              b.logger,
		transportWrapper:    b.transportWrapper,
		cloudID:             b.cloudID,
		extensions:          slices.Clone(b.extensions),
		backendClient:       backendClient,
		backendURL:          b.backendURL,
		backendToken:        b.backendToken,
		resourceServerURL:   b.resourceServerURL,
		resourceServerToken: b.resourceServerToken,
		selectorEvaluator:   selectorEvaluator,
		jsonAPI:             jsonAPI,
	}
	return
}

// List is part of the implementation of the collection handler interface.
func (h *AlarmHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {

	// Transform the items into what we need:
	alarms, err := h.fetchItems(ctx, request.Selector)
	if err != nil {
		return
	}

	// Select only the items that satisfy the filter:
	if request.Selector != nil {
		alarms = data.Select(
			alarms,
			func(ctx context.Context, item data.Object) (result bool, err error) {
				result, err = h.selectorEvaluator.Evaluate(ctx, request.Selector, item)
				return
			},
		)
	}

	// Return the result:
	response = &ListResponse{
		Items: alarms,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *AlarmHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	h.alarmFetcher, err = NewAlarmFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetExtensions(h.extensions...).
		Build()
	if err != nil {
		return
	}

	// Fetch the object:
	alarm, err := h.fetchItem(ctx, request.Variables[0])
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: alarm,
	}

	return
}

func (h *AlarmHandler) fetchItems(
	ctx context.Context, selector *search.Selector) (result data.Stream, err error) {
	h.alarmFetcher, err = NewAlarmFetcher().
		SetLogger(h.logger).
		SetTransportWrapper(h.transportWrapper).
		SetCloudID(h.cloudID).
		SetBackendURL(h.backendURL).
		SetBackendToken(h.backendToken).
		SetExtensions(h.extensions...).
		Build()
	if err != nil {
		return
	}
	return h.alarmFetcher.FetchItems(ctx)
}

func (h *AlarmHandler) fetchItem(ctx context.Context,
	id string) (alarm data.Object, err error) {
	// Fetch alarms
	alarms, err := h.alarmFetcher.FetchItems(ctx)
	if err != nil {
		return
	}

	// Get first result
	alarm, err = alarms.Next(ctx)

	return
}
