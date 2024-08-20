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
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/persiststorage"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// AlarmNotificationManagerHandlerBuilder contains the data and logic needed to construct
// alarm notification handler. Don't create instances of this type directly, use the
// NewAlarmNotificationHandler function instead.
type AlarmNotificationHandlerBuilder struct {
	logger                     *slog.Logger
	loggingWrapper             func(http.RoundTripper) http.RoundTripper
	cloudID                    string
	kubeClient                 *k8s.Client
	o2imsNamespace             string
	subscriptionsConfigmapName string
	resourceServerURL          string
	resourceServerToken        string
}

// key string is uuid
type alarmSubIdSet map[string]struct{}

// AlarmNotificationHander will receive the alarts from openshift alert manager,
// match alarm subscription filter rules with the alerts/alarm, and send out matched alarm notifications
// Don't create instances of this type directly, use the NewAlarmNotificationHandler function
// instead.
type AlarmNotificationHandler struct {
	logger            *slog.Logger
	loggingWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID           string
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
	jqTool            *jq.Tool
	alarmMapper       *AlarmMapper

	//structures for notification
	subscriptionMapMemoryLock *sync.RWMutex
	subscriptionMap           *map[string]data.Object
	persistStore              *persiststorage.KubeConfigMapStore
	subscriptionSearcher      *alarmSubscriptionSearcher
	httpClient                http.Client
}

// NewAlarmNotificationHandler creates a builder that can then be used to configure and create a
// handler for AlarmNotificationHandler.
func NewAlarmNotificationHandler() *AlarmNotificationHandlerBuilder {
	return &AlarmNotificationHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmNotificationHandlerBuilder) SetLogger(
	value *slog.Logger) *AlarmNotificationHandlerBuilder {
	b.logger = value
	return b
}

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *AlarmNotificationHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *AlarmNotificationHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *AlarmNotificationHandlerBuilder) SetCloudID(
	value string) *AlarmNotificationHandlerBuilder {
	b.cloudID = value
	return b
}

// SetKubeClient sets the kubeClient.
func (b *AlarmNotificationHandlerBuilder) SetKubeClient(
	kubeClient *k8s.Client) *AlarmNotificationHandlerBuilder {
	b.kubeClient = kubeClient
	return b
}

// SetNamespace sets the namespace.
func (b *AlarmNotificationHandlerBuilder) SetNamespace(
	value string) *AlarmNotificationHandlerBuilder {
	b.o2imsNamespace = value
	return b
}

// SetNamespace sets the namespace.
func (b *AlarmNotificationHandlerBuilder) SetConfigmapName(
	value string) *AlarmNotificationHandlerBuilder {
	b.subscriptionsConfigmapName = value
	return b
}

// SetResourceServerURL sets the URL of the resource server. This is mandatory.
// The resource server is used for mapping Alarms to Resources.
func (b *AlarmNotificationHandlerBuilder) SetResourceServerURL(
	value string) *AlarmNotificationHandlerBuilder {
	b.resourceServerURL = value
	return b
}

// SetResourceServerToken sets the authentication token that will be used to authenticate
// with to the resource server. This is mandatory.
func (b *AlarmNotificationHandlerBuilder) SetResourceServerToken(
	value string) *AlarmNotificationHandlerBuilder {
	b.resourceServerToken = value
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *AlarmNotificationHandlerBuilder) Build(ctx context.Context) (
	result *AlarmNotificationHandler, err error) {
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

	if b.resourceServerURL == "" {
		err = errors.New("resource server URL is mandatory")
		return
	}

	if b.resourceServerToken == "" {
		err = errors.New("resource server token is mandatory")
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

	alarmSubscriptionSearcher, err := newAlarmSubscriptionSearcherBuilder().
		SetLogger(b.logger).
		SetJqTool(jqTool).build()
	if err != nil {
		return
	}

	// create persist storeage option
	persistStore, err := persiststorage.NewKubeConfigMapStoreBuilder().
		SetNamespace(b.o2imsNamespace).
		SetName(b.subscriptionsConfigmapName).
		SetFieldOwner(FieldOwner).
		SetJsonAPI(jsonAPI).
		SetClient(b.kubeClient).
		Build()
	if err != nil {
		return
	}

	// http client to send out notification
	// use 2 sec first
	httpClient := http.Client{Timeout: 2 * time.Second}

	alarmMapper, err := NewAlarmMapper().
		SetLogger(b.logger).
		SetBackendClient(&httpClient).
		SetResourceServerURL(b.resourceServerURL).
		SetResourceServerToken(b.resourceServerToken).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	handler := &AlarmNotificationHandler{
		logger:                    b.logger,
		loggingWrapper:            b.loggingWrapper,
		cloudID:                   b.cloudID,
		selectorEvaluator:         selectorEvaluator,
		jsonAPI:                   jsonAPI,
		jqTool:                    jqTool,
		subscriptionMapMemoryLock: &sync.RWMutex{},
		subscriptionMap:           &map[string]data.Object{},
		persistStore:              persistStore,
		subscriptionSearcher:      alarmSubscriptionSearcher,
		httpClient:                httpClient,
		alarmMapper:               alarmMapper,
	}

	b.logger.Debug(
		"AlarmNotificationHandler build:",
		"CloudID", b.cloudID,
	)

	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"persistStorage namespace", b.o2imsNamespace,
	)

	b.logger.Debug(
		"alarmSubscriptionHandler build:",
		"persistStorage configmap name", b.subscriptionsConfigmapName,
	)

	err = handler.recoveryFromPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"AlarmNotificationHandler failed to recovery from persistStore ",
			slog.String("error", err.Error()),
		)
		return
	}

	err = handler.watchPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"AlarmNotificationHandler failed to watch persist store changes ",
			slog.String("error", err.Error()),
		)
		return
	}
	result = handler
	return
}

func (h *AlarmNotificationHandler) recoveryFromPersistStore(ctx context.Context) (err error) {
	newMap, err := persiststorage.GetAll(h.persistStore, ctx)
	if err != nil {
		return
	}
	err = h.assignSubscriptionMap(&newMap)
	if err != nil {
		h.logger.Error(
			"AlarmNotificationHandler failed building the indexes ",
			slog.String("error", err.Error()),
		)

	}
	return
}

func (h *AlarmNotificationHandler) watchPersistStore(ctx context.Context) (err error) {
	err = persiststorage.ProcessChangesWithFunction(h.persistStore, ctx, h.processStorageChanges)

	if err != nil {
		h.logger.Error(
			"AlarmNotificationHandler watchPersistStore failed ",
			slog.String("error", err.Error()),
		)
	}
	return
}

// Following function is called during daemon start to update alarm subscription as well as
// alarm subscription got updated. The write lock is need to update subscription kept in memory
func (h *AlarmNotificationHandler) assignSubscriptionMap(newMap *map[string]data.Object) (err error) {
	h.subscriptionMapMemoryLock.Lock()
	defer h.subscriptionMapMemoryLock.Unlock()
	h.subscriptionMap = newMap

	//clear existing search index and build new one for now
	h.subscriptionSearcher.subscriptionInfoMap = map[string]subscriptionInfo{}

	err = h.subscriptionSearcher.pocessSubscriptionMapForSearcher(h.subscriptionMap, h.jqTool)

	if err != nil {
		h.logger.Error(
			"pocessSubscriptionMapForSearcher ",
			slog.String("error", err.Error()),
		)
	}
	return
}

func (h *AlarmNotificationHandler) processStorageChanges(newMap *map[string]data.Object) {

	err := h.assignSubscriptionMap(newMap)

	if err != nil {
		h.logger.Error(
			"AlarmNotificationHandler failed to watch persist store changes ",
			slog.String("error", err.Error()),
		)
	}

}
