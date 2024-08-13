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
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/persiststorage"
)

// AlarmSubscriptionHandlerBuilder contains the data and logic needed to create a new
// alarm subscription handler. Don't create instance of this type directly, use the
// NewAlarmSubscriptionHandler function instead.
type SubscriptionHandlerBuilder struct {
	logger                     *slog.Logger
	loggingWrapper             func(http.RoundTripper) http.RoundTripper
	cloudID                    string
	extensions                 []string
	kubeClient                 *k8s.Client
	o2imsNamespace             string
	subscriptionsConfigmapName string
	subscriptionIdString       string
}

// alarmSubscriptionHander knows how to respond to requests to list alarm subscriptions.
// Don't create instances of this type directly, use the NewAlarmSubscriptionHandler function
// instead.
type SubscriptionHandler struct {
	logger               *slog.Logger
	loggingWrapper       func(http.RoundTripper) http.RoundTripper
	cloudID              string
	extensions           []string
	kubeClient           *k8s.Client
	jsonAPI              jsoniter.API
	jqTool               *jq.Tool
	subscriptionIdString string
	subscriptionMapLock  *sync.Mutex
	subscriptionMap      *map[string]data.Object
	persistStore         *persiststorage.KubeConfigMapStore
}

// NewSubscriptionHandler creates a builder that can then be used to configure and create a
// handler for the collection of deployment managers.
func NewSubscriptionHandler() *SubscriptionHandlerBuilder {
	return &SubscriptionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *SubscriptionHandlerBuilder) SetLogger(
	value *slog.Logger) *SubscriptionHandlerBuilder {
	b.logger = value
	return b
}

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *SubscriptionHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *SubscriptionHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *SubscriptionHandlerBuilder) SetCloudID(
	value string) *SubscriptionHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *SubscriptionHandlerBuilder) SetExtensions(
	values ...string) *SubscriptionHandlerBuilder {
	b.extensions = values
	return b
}

// SetKubeClient sets the K8S client.
func (b *SubscriptionHandlerBuilder) SetKubeClient(
	kubeClient *k8s.Client) *SubscriptionHandlerBuilder {
	b.kubeClient = kubeClient
	return b
}

// SetSubscriptionType sets the purpose of the subscription.
// So far alarm and infrastructure-inventory as supported.
func (b *SubscriptionHandlerBuilder) SetSubscriptionIdString(
	subscriptionIdString string) *SubscriptionHandlerBuilder {
	b.subscriptionIdString = subscriptionIdString
	return b
}

// SetNamespace sets the namespace.
func (b *SubscriptionHandlerBuilder) SetNamespace(
	value string) *SubscriptionHandlerBuilder {
	b.o2imsNamespace = value
	return b
}

// SetNamespace sets the namespace.
func (b *SubscriptionHandlerBuilder) SetConfigmapName(
	value string) *SubscriptionHandlerBuilder {
	b.subscriptionsConfigmapName = value
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *SubscriptionHandlerBuilder) Build(ctx context.Context) (
	result *SubscriptionHandler, err error) {
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

	if b.subscriptionIdString != SubscriptionIdAlarm &&
		b.subscriptionIdString != SubscriptionIdInfrastructureInventory {
		err = fmt.Errorf(
			fmt.Sprintf(
				"subscription type can only be %s or %s",
				SubscriptionIdAlarm, SubscriptionIdInfrastructureInventory,
			),
		)
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

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

	// Setup persistent storage:
	persistStore, err := persiststorage.NewKubeConfigMapStoreBuilder().
		SetNamespace(b.o2imsNamespace).
		SetName(b.subscriptionsConfigmapName).
		SetFieldOwner(FieldOwner).
		SetJsonAPI(jsonAPI).
		SetClient(b.kubeClient).Build()
	if err != nil {
		return
	}

	// Create and populate the object:

	handler := &SubscriptionHandler{
		logger:               b.logger,
		loggingWrapper:       b.loggingWrapper,
		cloudID:              b.cloudID,
		kubeClient:           b.kubeClient,
		extensions:           slices.Clone(b.extensions),
		jsonAPI:              jsonAPI,
		jqTool:               jqTool,
		subscriptionIdString: b.subscriptionIdString,
		subscriptionMapLock:  &sync.Mutex{},
		subscriptionMap:      &map[string]data.Object{},
		persistStore:         persistStore,
	}

	b.logger.Debug(
		"SubscriptionHandler build:",
		"CloudID", b.cloudID,
	)

	err = handler.getFromPersistentStorage(ctx)
	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"persistStorage namespace", b.o2imsNamespace,
	)

	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"persistStorage configmap name", b.subscriptionsConfigmapName,
	)

	if err != nil {
		b.logger.Error(
			"alarmSubscriptionHandler failed to recovery from persistStore ",
			slog.String("error", err.Error()),
		)
		return
	}

	err = handler.watchPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"alarmSubscriptionHandler failed to watch persist store changes ",
			slog.String("error", err.Error()),
		)
		return
	}

	result = handler
	return
}

// List is the implementation of the collection handler interface.
func (h *SubscriptionHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {
	// Create the stream that will fetch the items:
	var items data.Stream

	items, err = h.fetchItems(ctx)

	if err != nil {
		return
	}

	// Transform the items into what we need:
	items = data.Map(items, h.mapItem)

	// Return the result:
	response = &ListResponse{
		Items: items,
	}
	return
}

// Get is the implementation of the object handler interface.
func (h *SubscriptionHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	h.logger.DebugContext(
		ctx,
		"SubscriptionHandler Get:",
	)
	item, err := h.fetchItem(ctx, request.Variables[0])

	// Return the result:
	response = &GetResponse{
		Object: item,
	}
	return
}

// Add is the implementation of the object handler ADD interface.
func (h *SubscriptionHandler) Add(ctx context.Context,
	request *AddRequest) (response *AddResponse, err error) {

	h.logger.DebugContext(
		ctx,
		"SubscriptionHandler Add:",
	)
	id, err := h.addItem(ctx, *request)

	if err != nil {
		h.logger.Debug(
			"SubscriptionHandler Add:",
			"err", err.Error(),
		)
		return
	}

	//add subscription Id in the response
	obj := request.Object

	obj, err = h.encodeSubId(id, obj)

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
func (h *SubscriptionHandler) Delete(ctx context.Context,
	request *DeleteRequest) (response *DeleteResponse, err error) {

	h.logger.DebugContext(
		ctx,
		"SubscriptionHandler delete:",
	)

	err = h.deleteItem(ctx, *request)

	// Return the result:
	response = &DeleteResponse{}

	return
}
func (h *SubscriptionHandler) fetchItem(ctx context.Context, // nolint: unparam
	id string) (result data.Object, err error) {
	h.subscriptionMapLock.Lock()
	defer h.subscriptionMapLock.Unlock()
	obj, ok := (*h.subscriptionMap)[id]
	if !ok {
		err = ErrNotFound
		return
	}

	result, _ = h.encodeSubId(id, obj)
	return
}

func (h *SubscriptionHandler) fetchItems(ctx context.Context) (result data.Stream, err error) {
	h.subscriptionMapLock.Lock()
	defer h.subscriptionMapLock.Unlock()

	ar := make([]data.Object, 0, len(*h.subscriptionMap))

	for key, value := range *h.subscriptionMap {
		obj, _ := h.encodeSubId(key, value)
		ar = append(ar, obj)
	}
	h.logger.DebugContext(
		ctx,
		"SubscriptionHandler fetchItems:",
	)
	result = data.Pour(ar...)
	return
}

func (h *SubscriptionHandler) addItem(
	ctx context.Context, input_data AddRequest) (subId string, err error) {

	subId = h.getSubcriptionId()

	//save the subscription in configuration map
	value, err := h.jsonAPI.MarshalIndent(&input_data.Object, "", " ")
	if err != nil {
		return
	}
	err = persiststorage.Add(h.persistStore, ctx, subId, string(value))
	if err != nil {
		h.logger.Debug(
			"SubscriptionHandler addItem:",
			"err", err.Error(),
		)
		return
	}

	h.addToSubscriptionMap(subId, input_data.Object)

	return
}

func (h *SubscriptionHandler) deleteItem(
	ctx context.Context, delete_req DeleteRequest) (err error) {

	err = persiststorage.Delete(h.persistStore, ctx, delete_req.Variables[0])
	if err != nil {
		return
	}

	h.deleteToSubscriptionMap(delete_req.Variables[0])

	return
}

func (h *SubscriptionHandler) mapItem(ctx context.Context,
	input data.Object) (output data.Object, err error) {

	//TBD only save related attributes in the future
	return input, nil
}
func (h *SubscriptionHandler) addToSubscriptionMap(key string, value data.Object) {
	h.subscriptionMapLock.Lock()
	defer h.subscriptionMapLock.Unlock()
	(*h.subscriptionMap)[key] = value
}
func (h *SubscriptionHandler) deleteToSubscriptionMap(key string) {
	h.subscriptionMapLock.Lock()
	defer h.subscriptionMapLock.Unlock()
	//test if the key in the map
	_, ok := (*h.subscriptionMap)[key]

	if !ok {
		return
	}

	delete(*h.subscriptionMap, key)
}

func (h *SubscriptionHandler) assignSubscriptionMap(newMap map[string]data.Object) {
	h.subscriptionMapLock.Lock()
	defer h.subscriptionMapLock.Unlock()
	h.subscriptionMap = &newMap
}

func (h *SubscriptionHandler) getSubcriptionId() (subId string) {
	subId = uuid.New().String()
	return
}

func (h *SubscriptionHandler) encodeSubId(
	subId string, input data.Object) (output data.Object, err error) {

	// Get consumer name, subscriptions.
	err = h.jqTool.Evaluate(
		`{
			$subscriptionIdString: $subId,
			"consumerSubscriptionId": .consumerSubscriptionId,
			"callback": .callback,
			"filter": .filter
		}`,
		input, &output,
		jq.String("$subscriptionIdString", h.subscriptionIdString),
		jq.String("$subId", subId),
	)

	return
}

func (h *SubscriptionHandler) decodeSubId(
	input data.Object) (output string, err error) {

	// get cluster name, subscriptions
	err = h.jqTool.Evaluate(
		"."+h.subscriptionIdString, input, &output)

	return
}

func (h *SubscriptionHandler) getFromPersistentStorage(ctx context.Context) (err error) {
	newMap, err := persiststorage.GetAll(h.persistStore, ctx)
	if err != nil {
		return
	}
	h.assignSubscriptionMap(newMap)
	return
}

func (h *SubscriptionHandler) watchPersistStore(ctx context.Context) (err error) {
	err = persiststorage.ProcessChanges(h.persistStore, ctx, &h.subscriptionMap, h.subscriptionMapLock)

	if err != nil {
		panic("failed to launch watcher")
	}
	return
}
