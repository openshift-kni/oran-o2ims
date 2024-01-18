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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	neturl "net/url"
	"slices"
	"sync"

	"github.com/imdario/mergo"
	jsoniter "github.com/json-iterator/go"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// DeploymentManagerHandlerBuilder contains the data and logic needed to create a new deployment
// manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerHandler function instead.
type DeploymentManagerHandlerBuilder struct {
	logger         *slog.Logger
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	cloudID        string
	extensions     []string
	backendURL     string
	backendToken   string
	enableHack     bool
}

// DeploymentManagerCollectionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewDeploymentManagerHandler function
// instead.
type DeploymentManagerHandler struct {
	logger            *slog.Logger
	loggingWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID           string
	extensions        []string
	backendURL        string
	backendToken      string
	backendClient     *http.Client
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
	jqTool            *jq.Tool
	globalHubClient   *k8s.Client
	enableHack        bool
	profileCacheLock  *sync.Mutex
	profileCache      map[string]data.Object
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

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *DeploymentManagerHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *DeploymentManagerHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *DeploymentManagerHandlerBuilder) SetCloudID(
	value string) *DeploymentManagerHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *DeploymentManagerHandlerBuilder) SetExtensions(values ...string) *DeploymentManagerHandlerBuilder {
	b.extensions = values
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

// SetEnableHack sets or clears the flag that indicates if the hack used to fetch authentication
// details from clusters should be enabled. This is intended for unit tests, where we don't currently
// have a way to test that hack. By the default the hack is disabled.
func (b *DeploymentManagerHandlerBuilder) SetEnableHack(
	value bool) *DeploymentManagerHandlerBuilder {
	b.enableHack = value
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
	if b.loggingWrapper != nil {
		backendTransport = b.loggingWrapper(backendTransport)
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

	// Create the Kubernetes API client only if the hack is enabled:
	var globalHubClient *k8s.Client
	if b.enableHack {
		globalHubClient, err = k8s.NewClient().
			SetLogger(b.logger).
			SetLoggingWrapper(b.loggingWrapper).
			Build()
		if err != nil {
			return
		}
	}

	// Create and populate the object:
	result = &DeploymentManagerHandler{
		logger:            b.logger,
		loggingWrapper:    b.loggingWrapper,
		cloudID:           b.cloudID,
		extensions:        slices.Clone(b.extensions),
		backendURL:        b.backendURL,
		backendToken:      b.backendToken,
		backendClient:     backendClient,
		selectorEvaluator: selectorEvaluator,
		jsonAPI:           jsonAPI,
		jqTool:            jqTool,
		globalHubClient:   globalHubClient,
		enableHack:        b.enableHack,
		profileCacheLock:  &sync.Mutex{},
		profileCache:      map[string]data.Object{},
	}
	return
}

// List is the implementation of the collection handler interface.
func (h *DeploymentManagerHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {
	// Create the stream that will fetch the items:
	items, err := h.fetchItems(ctx, nil)
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
	// Fetch the item:
	item, err := h.fetchItem(ctx, request.Variables[0], nil)
	if err != nil {
		return
	}

	// Transform the object into what we need:
	item, err = h.mapItem(ctx, item)
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: item,
	}
	return
}

func (h *DeploymentManagerHandler) fetchItems(ctx context.Context,
	query neturl.Values) (result data.Stream, err error) {
	url := fmt.Sprintf("%s/global-hub-api/v1/managedclusters", h.backendURL)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if query != nil {
		request.URL.RawQuery = query.Encode()
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

func (h *DeploymentManagerHandler) fetchItem(ctx context.Context, id string,
	query neturl.Values) (result data.Object, err error) {
	// Currently the ACM API that we use doesn't have a specific endpoint for retrieving a
	// specific object, instead of that we need to fetch a list filtering with a label
	// selector.
	if query == nil {
		query = neturl.Values{}
	} else {
		query = maps.Clone(query)
	}
	query.Set("labelSelector", fmt.Sprintf("clusterID=%s", id))
	query.Set("limit", "1")
	stream, err := h.fetchItems(ctx, query)
	if err != nil {
		return
	}
	items, err := data.Collect(ctx, stream)
	if err != nil {
		return
	}
	if len(items) == 0 {
		err = ErrNotFound
		return
	}
	result = items[0]
	return
}

func (h *DeploymentManagerHandler) mapItem(ctx context.Context,
	input data.Object) (output data.Object, err error) {
	// Get the name of the hub and the name of the cluster:
	var hub string
	err = h.jqTool.Evaluate(
		`.metadata.annotations["global-hub.open-cluster-management.io/managed-by"]`,
		input, &hub,
	)
	if err != nil {
		return
	}
	var cluster string
	err = h.jqTool.Evaluate(`.metadata.name`, input, &cluster)
	if err != nil {
		return
	}

	// Get the basic attributes:
	err = h.jqTool.Evaluate(
		`{
			"deploymentManagerId": .metadata.labels["clusterID"],
			"description": $cluster,
			"name": $cluster,
			"oCloudId": $cloud,
			"serviceUri": .spec.managedClusterClientConfigs[0].url
		}`,
		input, &output,
		jq.String("$cloud", h.cloudID),
		jq.String("$cluster", cluster),
	)
	if err != nil {
		return
	}

	// Add the extensions:
	for _, extension := range h.extensions {
		var value any
		err = h.jqTool.Evaluate(extension, input, &value)
		if err != nil {
			h.logger.Error(
				"Failed to evaluate extension",
				slog.String("cluster", cluster),
				slog.String("extension", extension),
				slog.String("error", err.Error()),
			)
		}
		if value != nil {
			err = mergo.Merge(
				&output,
				data.Object{
					"extensions": value,
				},
				mergo.WithOverride,
			)
			if err != nil {
				h.logger.Warn(
					"Failed to merge extension",
					slog.String("cluster", cluster),
					slog.String("extension", extension),
					slog.String("error", err.Error()),
				)
				err = nil
			}
		}
	}

	// Add the profile:
	profile, err := h.getProfile(ctx, hub, cluster)
	if err != nil {
		h.logger.Warn(
			"Failed to fetch profile",
			slog.String("hub", hub),
			slog.String("cluster", cluster),
			slog.String("error", err.Error()),
		)
		err = nil
	}
	if profile != nil {
		err = mergo.Merge(
			&output,
			data.Object{
				"extensions": data.Object{
					"profileName": "k8s",
					"profileData": profile,
				},
			},
			mergo.WithOverride,
		)
		if err != nil {
			h.logger.Warn(
				"Failed to merge profile",
				slog.String("hub", hub),
				slog.String("cluster", cluster),
				slog.String("error", err.Error()),
			)
			err = nil
		}
	}

	return
}

func (h *DeploymentManagerHandler) getProfile(ctx context.Context,
	hub, cluster string) (result data.Object, err error) {
	h.profileCacheLock.Lock()
	defer h.profileCacheLock.Unlock()
	result, ok := h.profileCache[cluster]
	if !ok {
		result, err = h.fetchProfile(ctx, hub, cluster)
		if err != nil {
			return
		}
		if result != nil {
			h.profileCache[cluster] = result
		}
	}
	return
}

func (h *DeploymentManagerHandler) fetchProfile(ctx context.Context,
	hub, cluster string) (result data.Object, err error) {
	// What we do here is slow and fragile, we are doing it only temporarely because there
	// is no way to get the authentiation details from the backend server. In addition there
	// is no simple way to test it, so we will only do it when enabled.
	if !h.enableHack {
		return
	}

	// Fetch the kubeconfig that was used to register the hub, and then use it to fetch the
	// admin kubeconfig of the hub:
	hubRegKC, err := h.fetchRegKC(ctx, h.globalHubClient, hub)
	if err != nil {
		return
	}
	hubRegClient, err := k8s.NewClient().
		SetLogger(h.logger).
		SetLoggingWrapper(h.loggingWrapper).
		SetKubeconfig(hubRegKC).
		Build()
	if err != nil {
		return
	}
	hubAdminKC, err := h.fetchAdminKC(ctx, hubRegClient)
	if err != nil {
		return
	}

	// Use the admin kubeconfig of the hub to fetch the kubeconfig that was used to register
	// the cluster, and then use it to fetch the admin kubeconfig of the cluster:
	hubAdminClient, err := k8s.NewClient().
		SetLogger(h.logger).
		SetLoggingWrapper(h.loggingWrapper).
		SetKubeconfig(hubAdminKC).
		Build()
	if err != nil {
		return
	}
	clusteRegKC, err := h.fetchRegKC(ctx, hubAdminClient, cluster)
	if err != nil {
		return
	}
	clusterRegClient, err := k8s.NewClient().
		SetLogger(h.logger).
		SetLoggingWrapper(h.loggingWrapper).
		SetKubeconfig(clusteRegKC).
		Build()
	if err != nil {
		return
	}
	clusterAdminKC, err := h.fetchAdminKC(ctx, clusterRegClient)
	if err != nil {
		return
	}

	// Make the profile data from the cluster admin kubeconfig:
	result, err = h.makeProfile(clusterAdminKC)
	return
}

// fetchRegKC uses the given Kubernetes API client to fetch the kubeconfig that was used to
// register a cluster. Returns the serialized kubeconfig, or nil if there is no such kubeconfig.
func (h *DeploymentManagerHandler) fetchRegKC(ctx context.Context,
	client clnt.Client, clusterName string) (result []byte, err error) {
	// Try to fetch the secret that contains the credentials that were used when the cluster
	// was registered.
	secret := &corev1.Secret{}
	key := clnt.ObjectKey{
		Namespace: clusterName,
		Name:      fmt.Sprintf("%s-cluster-secret", clusterName),
	}
	err = client.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		h.logger.Info(
			"Cluster secret doesn't exist",
			slog.String("cluster", clusterName),
			slog.String("namespace", key.Namespace),
			slog.String("secret", key.Name),
		)
		err = nil
		return
	}
	if err != nil {
		return
	}

	// The secret should contain a 'server' entry with the URL of the API server:
	content, ok := secret.Data["server"]
	if !ok {
		h.logger.Warn(
			"Cluster secret exists but doesn't contain the server URL",
			slog.String("cluster", clusterName),
			slog.String("namespace", key.Namespace),
			slog.String("secret", key.Name),
		)
		return
	}
	server := string(content)

	// The secret should contain a 'config' entry that contains a JSON document containing the
	// credentials, something like this:
	//
	//	{
	//		"bearerToken": "ey...",
	//		"tlsClientConfig": {
	//			"insecure": true
	//		},
	//	}
	//
	// We need to parse and extract the values.
	content, ok = secret.Data["config"]
	if !ok {
		h.logger.Warn(
			"Cluster secret exists but doesn't contain the configuration",
			slog.String("cluster", clusterName),
			slog.String("namespace", key.Namespace),
			slog.String("secret", key.Name),
		)
		return
	}
	type Config struct {
		BearerToken string `json:"bearerToken"`
	}
	var config Config
	err = json.Unmarshal(content, &config)
	if err != nil {
		return
	}
	kubeConfigObject := data.Object{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": data.Array{
			data.Object{
				"name": "default",
				"cluster": data.Object{
					"server":                   server,
					"insecure-skip-tls-verify": true,
				},
			},
		},
		"users": data.Array{
			data.Object{
				"name": "default",
				"user": data.Object{
					"token": config.BearerToken,
				},
			},
		},
		"contexts": data.Array{
			data.Object{
				"name": "default",
				"context": data.Object{
					"cluster": "default",
					"user":    "default",
				},
			},
		},
		"current-context": "default",
	}
	result, err = yaml.Marshal(kubeConfigObject)
	return
}

// fetchAdminKC uses the given Kubernetes API client to fetch the administrator kubeconfig. Returns
// the serialized kubeconfig, or nil if it doesn't exist.
func (h *DeploymentManagerHandler) fetchAdminKC(ctx context.Context,
	client clnt.Client) (result []byte, err error) {
	secret := &corev1.Secret{}
	key := clnt.ObjectKey{
		Namespace: "openshift-kube-apiserver",
		Name:      "node-kubeconfigs",
	}
	err = client.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	result = secret.Data["lb-ext.kubeconfig"]
	return
}

func (h *DeploymentManagerHandler) makeProfile(kubeconfigBytes []byte) (result data.Object,
	err error) {
	var kubeconfig data.Object
	err = yaml.Unmarshal(kubeconfigBytes, &kubeconfig)
	if err != nil {
		return
	}
	err = h.jqTool.Evaluate(
		`
		."current-context" as $current |
		(.contexts[] | select(.name == $current) | .context) as $context |
		(.clusters[] | select(.name == $context.cluster) | .cluster) as $cluster |
		(.users[] | select(.name == $context.user) | .user) as $user |
		{
			"cluster_api_endpoint": $cluster."server",
			"cluster_ca_cert": $cluster."certificate-authority-data",
			"admin_user": $context."user",
			"admin_client_cert": $user."client-certificate-data",
			"admin_client_key": $user."client-key-data"
		}
		`,
		kubeconfig, &result,
	)
	return
}
