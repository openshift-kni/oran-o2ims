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
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	"golang.org/x/exp/maps"
)

// DeploymentManagerHandlerBuilder contains the data and logic needed to create a new deployment
// manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerHandler function instead.
type alarmSubscriptionHandlerBuilder struct {
	logger         *slog.Logger
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	cloudID        string
	extensions     []string
}

// alarmSubscriptionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewAlarmSubscriptionHandler function
// instead.
type alarmSubscriptionHandler struct {
	logger                   *slog.Logger
	loggingWrapper           func(http.RoundTripper) http.RoundTripper
	cloudID                  string
	extensions               []string
	jsonAPI                  jsoniter.API
	selectorEvaluator        *search.SelectorEvaluator
	jqTool                   *jq.Tool
	subscritionMapMemoryLock *sync.Mutex
	subscriptionMap          map[string]data.Object
}

// NewAlarmSubscriptionHandler creates a builder that can then be used to configure and create a
// handler for the collection of deployment managers.
func NewAlarmSubscriptionHandler() *alarmSubscriptionHandlerBuilder {
	return &alarmSubscriptionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *alarmSubscriptionHandlerBuilder) SetLogger(
	value *slog.Logger) *alarmSubscriptionHandlerBuilder {
	b.logger = value
	return b
}

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *alarmSubscriptionHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *alarmSubscriptionHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *alarmSubscriptionHandlerBuilder) SetCloudID(
	value string) *alarmSubscriptionHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *alarmSubscriptionHandlerBuilder) SetExtensions(
	values ...string) *alarmSubscriptionHandlerBuilder {
	b.extensions = values
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *alarmSubscriptionHandlerBuilder) Build() (
	result *alarmSubscriptionHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.cloudID == "" {
		err = errors.New("cloud identifier is mandatory")
		return
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

	// Create the jq tool:
	jqTool, err := jq.NewTool().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Check that extensions are at least syntactically valid:
	for _, extension := range b.extensions {
		_, err = jqTool.Compile(extension)
		if err != nil {
			return
		}
	}

	// Create and populate the object:
	result = &alarmSubscriptionHandler{
		logger:                   b.logger,
		loggingWrapper:           b.loggingWrapper,
		cloudID:                  b.cloudID,
		extensions:               slices.Clone(b.extensions),
		selectorEvaluator:        selectorEvaluator,
		jsonAPI:                  jsonAPI,
		jqTool:                   jqTool,
		subscritionMapMemoryLock: &sync.Mutex{},
		subscriptionMap:          map[string]data.Object{},
	}

	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"CloudID", b.cloudID,
	)

	return
}

// List is the implementation of the collection handler interface.
func (h *alarmSubscriptionHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {
	// Create the stream that will fetch the items:
	var items data.Stream

	items, err = h.fetchItems(ctx)

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
func (h *alarmSubscriptionHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	h.logger.Debug(
		"alarmSubscriptionHandler Get:",
	)
	item, err := h.fetchItem(ctx, request.Variables[0])

	// Return the result:
	response = &GetResponse{
		Object: item,
	}
	return
}

// Add is the implementation of the object handler ADD interface.
func (h *alarmSubscriptionHandler) Add(ctx context.Context,
	request *AddRequest) (response *AddResponse, err error) {

	h.logger.Debug(
		"alarmSubscriptionHandler Add:",
	)
	id, err := h.addItem(ctx, *request)

	if err != nil {
		return
	}

	//add subscription Id in the response
	obj := request.Object

	obj, err = h.encodeSubId(ctx, id, obj)

	if err != nil {
		return
	}

	// Return the result:
	response = &AddResponse{
		Object: obj,
	}
	return
}

// Delete is the implementation of the object handler delete interface.
func (h *alarmSubscriptionHandler) Delete(ctx context.Context,
	request *DeleteRequest) (response *DeleteResponse, err error) {

	h.logger.Debug(
		"alarmSubscriptionHandler delete:",
	)
	err = h.deleteItem(ctx, *request)

	// Return the result:
	response = &DeleteResponse{}

	return
}
func (h *alarmSubscriptionHandler) fetchItem(ctx context.Context,
	id string) (result data.Object, err error) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	result, ok := h.subscriptionMap[id]
	if !ok {
		err = ErrNotFound
		return
	}
	return
}

func (h *alarmSubscriptionHandler) fetchItems(
	ctx context.Context) (result data.Stream, err error) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	ar := maps.Values(h.subscriptionMap)
	h.logger.Debug(
		"alarmSubscriptionHandler fetchItems:",
	)
	result = data.Pour(ar...)
	return
}

func (h *alarmSubscriptionHandler) getSubcriptionId() (subId string) {
	subId = uuid.New().String()
	return
}

// Not sure if we need this in the future
// add it for now for the test purpose
func (h *alarmSubscriptionHandler) encodeSubId(ctx context.Context,
	subId string, input data.Object) (output data.Object, err error) {
	//get consumer name, subscriptions
	err = h.jqTool.Evaluate(
		`{
			"alarmSubscriptionId": $alarmSubId,
			"consumerSubscriptionId": .consumerSubscriptionId,
			"callback": .callback,
			"filter": .filter
		}`,
		input, &output,
		jq.String("$alarmSubId", subId),
	)
	if err != nil {
		return
	}
	return
}

func (h *alarmSubscriptionHandler) decodeSubId(ctx context.Context,
	input data.Object) (output string, err error) {
	//get cluster name, subscriptions
	err = h.jqTool.Evaluate(
		`.alarmSubscriptionId`, input, &output)
	if err != nil {
		return
	}
	return
}
func (h *alarmSubscriptionHandler) addItem(
	ctx context.Context, input_data AddRequest) (subId string, err error) {

	subId = h.getSubcriptionId()
	object, err := h.encodeSubId(ctx, subId, input_data.Object)
	if err != nil {
		return
	}

	object, err = h.mapItem(ctx, object)
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	h.subscriptionMap[subId] = object

	return
}

func (h *alarmSubscriptionHandler) deleteItem(
	ctx context.Context, delete_req DeleteRequest) (err error) {

	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()

	//test if the key in the map
	_, ok := h.subscriptionMap[delete_req.Variables[0]]

	if !ok {
		err = ErrNotFound
		return
	}

	delete(h.subscriptionMap, delete_req.Variables[0])

	return
}

func (h *alarmSubscriptionHandler) mapItem(ctx context.Context,
	input data.Object) (output data.Object, err error) {

	//TBD only save related attributes in the future
	return input, nil
}
