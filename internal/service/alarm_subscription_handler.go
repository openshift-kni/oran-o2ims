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
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/search"

	"github.com/openshift-kni/oran-o2ims/internal/persiststorage"
)

const (
	TestNamespace     = "orantest"
	TestConfigmapName = "orantestconfigmapalarmsub"
	FieldOwner        = "oran-o2ims"
)

// alarmSubscriptionHandlerBuilder contains the data and logic needed to create a new deployment
// manager collection handler. Don't create instances of this type directly, use the
// NewAlarmSubscriptionHandler function instead.
type alarmSubscriptionHandlerBuilder struct {
	logger         *slog.Logger
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	cloudID        string
	extensions     []string
	kubeClient     *k8s.Client
}

// alarmSubscriptionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewAlarmSubscriptionHandler function
// instead.
type alarmSubscriptionHandler struct {
	logger                   *slog.Logger
	loggingWrapper           func(http.RoundTripper) http.RoundTripper
	cloudID                  string
	extensions               []string
	kubeClient               *k8s.Client
	jsonAPI                  jsoniter.API
	selectorEvaluator        *search.SelectorEvaluator
	jqTool                   *jq.Tool
	subscritionMapMemoryLock *sync.Mutex
	subscriptionMap          *map[string]data.Object
	persistStore             *persiststorage.KubeConfigMapStore
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

// SetExtensions sets the fields that will be added to the extensions.
func (b *alarmSubscriptionHandlerBuilder) SetKubeClient(
	kubeClient *k8s.Client) *alarmSubscriptionHandlerBuilder {
	b.kubeClient = kubeClient
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *alarmSubscriptionHandlerBuilder) Build(ctx context.Context) (
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

	if b.kubeClient == nil {
		err = errors.New("kubeClient is mandatory")
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

	// create persist storeage option
	persistStore := persiststorage.NewKubeConfigMapStore().
		SetNameSpace(TestNamespace).
		SetName(TestConfigmapName).
		SetFieldOwnder(FieldOwner).
		SetJsonAPI(&jsonAPI).
		SetClient(b.kubeClient)

	// Create and populate the object:
	result = &alarmSubscriptionHandler{
		logger:                   b.logger,
		loggingWrapper:           b.loggingWrapper,
		cloudID:                  b.cloudID,
		kubeClient:               b.kubeClient,
		extensions:               slices.Clone(b.extensions),
		selectorEvaluator:        selectorEvaluator,
		jsonAPI:                  jsonAPI,
		jqTool:                   jqTool,
		subscritionMapMemoryLock: &sync.Mutex{},
		subscriptionMap:          &map[string]data.Object{},
		persistStore:             persistStore,
	}

	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"CloudID", b.cloudID,
	)

	err = result.recoveryFromPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"alarmSubscriptionHandler failed to recovery from persistStore ", err,
		)
	}

	err = result.watchPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"alarmSubscriptionHandler failed to watch persist store changes ", err,
		)
	}

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

	h.logger.DebugContext(
		ctx,
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

	h.logger.DebugContext(
		ctx,
		"alarmSubscriptionHandler Add:",
	)
	id, err := h.addItem(ctx, *request)

	if err != nil {
		h.logger.Debug(
			"alarmSubscriptionHandler Add:",
			"err", err.Error(),
		)
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

	h.logger.DebugContext(
		ctx,
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
	obj, ok := (*h.subscriptionMap)[id]
	if !ok {
		err = ErrNotFound
		return
	}

	result, _ = h.encodeSubId(ctx, id, obj)
	return
}

func (h *alarmSubscriptionHandler) fetchItems(ctx context.Context) (result data.Stream, err error) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()

	ar := make([]data.Object, 0, len(*h.subscriptionMap))

	for key, value := range *h.subscriptionMap {
		obj, _ := h.encodeSubId(ctx, key, value)
		ar = append(ar, obj)
	}
	h.logger.DebugContext(
		ctx,
		"alarmSubscriptionHandler fetchItems:",
	)
	result = data.Pour(ar...)
	return
}

func (h *alarmSubscriptionHandler) addItem(
	ctx context.Context, input_data AddRequest) (subId string, err error) {

	subId = h.getSubcriptionId()

	//save the subscription in configuration map
	//value, err := jsoniter.MarshalIndent(&input_data.Object, "", " ")
	value, err := h.jsonAPI.MarshalIndent(&input_data.Object, "", " ")
	if err != nil {
		return
	}
	err = h.persistStoreAddEntry(ctx, subId, string(value))
	if err != nil {
		h.logger.Debug(
			"alarmSubscriptionHandler addItem:",
			"err", err.Error(),
		)
		return
	}

	h.addToSubscriptionMap(subId, input_data.Object)

	return
}

func (h *alarmSubscriptionHandler) deleteItem(
	ctx context.Context, delete_req DeleteRequest) (err error) {

	err = h.persistStoreDeleteEntry(ctx, delete_req.Variables[0])
	if err != nil {
		return
	}

	h.deleteToSubscriptionMap(delete_req.Variables[0])

	return
}

func (h *alarmSubscriptionHandler) mapItem(ctx context.Context,
	input data.Object) (output data.Object, err error) {

	//TBD only save related attributes in the future
	return input, nil
}
func (h *alarmSubscriptionHandler) addToSubscriptionMap(key string, value data.Object) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	(*h.subscriptionMap)[key] = value
}
func (h *alarmSubscriptionHandler) deleteToSubscriptionMap(key string) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	//test if the key in the map
	_, ok := (*h.subscriptionMap)[key]

	if !ok {
		return
	}

	delete(*h.subscriptionMap, key)
}

func (h *alarmSubscriptionHandler) assignSubscriptionMap(newMap map[string]data.Object) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	h.subscriptionMap = &newMap
}

func (h *alarmSubscriptionHandler) getSubcriptionId() (subId string) {
	subId = uuid.New().String()
	return
}

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

func (h *alarmSubscriptionHandler) persistStoreAddEntry(
	ctx context.Context, entryKey string, value string) (err error) {
	return persiststorage.Add(h.persistStore, ctx, entryKey, value)
}

func (h *alarmSubscriptionHandler) persistStoreDeleteEntry(
	ctx context.Context, entryKey string) (err error) {
	err = persiststorage.Delete(h.persistStore, ctx, entryKey)
	return
}

func (h *alarmSubscriptionHandler) recoveryFromPersistStore(ctx context.Context) (err error) {
	newMap, err := persiststorage.GetAll(h.persistStore, ctx)
	if err != nil {
		return
	}
	h.assignSubscriptionMap(newMap)
	return
}

func (h *alarmSubscriptionHandler) watchPersistStore(ctx context.Context) (err error) {
	err = persiststorage.ProcessChanges(h.persistStore, ctx, &h.subscriptionMap, h.subscritionMapMemoryLock)

	if err != nil {
		panic("failed to launch watcher")
	}
	return
}
