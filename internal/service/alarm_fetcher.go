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
	neturl "net/url"

	"github.com/imdario/mergo"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"

	"github.com/itchyny/gojq"
)

type AlarmFetcher struct {
	logger              *slog.Logger
	cloudID             string
	backendURL          string
	backendToken        string
	backendClient       *http.Client
	resourceServerURL   string
	resourceServerToken string
	extensions          []string
	jqTool              *jq.Tool
}

// AlarmFetcherBuilder contains the data and logic needed to create a new AlarmFetcher.
type AlarmFetcherBuilder struct {
	logger              *slog.Logger
	transportWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID             string
	backendURL          string
	backendToken        string
	resourceServerURL   string
	resourceServerToken string
	extensions          []string
}

// NewAlarmFetcher creates a builder that can then be used to configure
// and create a handler for the AlarmFetcher.
func NewAlarmFetcher() *AlarmFetcherBuilder {
	return &AlarmFetcherBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmFetcherBuilder) SetLogger(
	value *slog.Logger) *AlarmFetcherBuilder {
	b.logger = value
	return b
}

// SetTransportWrapper sets the wrapper that will be used to configure the HTTP clients used to
// connect to other servers, including the backend server. This is optional.
func (b *AlarmFetcherBuilder) SetTransportWrapper(
	value func(http.RoundTripper) http.RoundTripper) *AlarmFetcherBuilder {
	b.transportWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *AlarmFetcherBuilder) SetCloudID(
	value string) *AlarmFetcherBuilder {
	b.cloudID = value
	return b
}

// SetBackendURL sets the URL of the backend server This is mandatory.
func (b *AlarmFetcherBuilder) SetBackendURL(
	value string) *AlarmFetcherBuilder {
	b.backendURL = value
	return b
}

// SetBackendToken sets the authentication token that will be used to authenticate to the backend
// server. This is mandatory.
func (b *AlarmFetcherBuilder) SetBackendToken(
	value string) *AlarmFetcherBuilder {
	b.backendToken = value
	return b
}

// SetResourceServerURL sets the URL of the resource server. This is mandatory.
// The resource server is used for mapping Alarms to Resources.
func (b *AlarmFetcherBuilder) SetResourceServerURL(
	value string) *AlarmFetcherBuilder {
	b.resourceServerURL = value
	return b
}

// SetResourceServerToken sets the authentication token that will be used to authenticate
// with to the resource server. This is mandatory.
func (b *AlarmFetcherBuilder) SetResourceServerToken(
	value string) *AlarmFetcherBuilder {
	b.resourceServerToken = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *AlarmFetcherBuilder) SetExtensions(values ...string) *AlarmFetcherBuilder {
	b.extensions = values
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *AlarmFetcherBuilder) Build() (
	result *AlarmFetcher, err error) {
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

	// Create a jq compiler function for parsing labels
	compilerFunc := gojq.WithFunction("parse_labels", 0, 1, func(x any, _ []any) any {
		if labels, ok := x.(string); ok {
			return data.GetLabelsMap(labels)
		}
		return nil
	})

	// Create the jq tool:
	jqTool, err := jq.NewTool().
		SetLogger(b.logger).
		SetCompilerOption(&compilerFunc).
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
	result = &AlarmFetcher{
		logger:              b.logger,
		cloudID:             b.cloudID,
		backendURL:          b.backendURL,
		backendToken:        b.backendToken,
		backendClient:       backendClient,
		resourceServerURL:   b.resourceServerURL,
		resourceServerToken: b.resourceServerToken,
		extensions:          b.extensions,
		jqTool:              jqTool,
	}
	return
}

// FetchItems returns a data stream of O2 Alarms.
// The items are converted from Alerts fetched from the Alertmanager API.
func (r *AlarmFetcher) FetchItems(
	ctx context.Context) (alarms data.Stream, err error) {
	query := neturl.Values{}
	url := r.backendURL + "/alerts"
	response, err := r.doGet(ctx, url, r.backendToken, query)
	if err != nil {
		return
	}

	// Create a reader for Alerts
	alerts, err := k8s.NewStream().
		SetLogger(r.logger).
		SetReader(response.Body).
		Build()
	if err != nil {
		return
	}

	// Transform Alerts to Alarms
	alarms = data.Map(alerts, r.mapAlertItem)

	return
}

func (r *AlarmFetcher) doGet(ctx context.Context, url, token string,
	query neturl.Values) (response *http.Response, err error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if query != nil {
		request.URL.RawQuery = query.Encode()
	}
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	request.Header.Set("Accept", "application/json")
	response, err = r.backendClient.Do(request)
	if err != nil {
		return
	}
	if response.StatusCode != http.StatusOK {
		r.logger.ErrorContext(
			ctx,
			"Received unexpected status code",
			"code", response.StatusCode,
			"url", request.URL,
		)
		err = fmt.Errorf(
			"received unexpected status code %d from '%s'",
			response.StatusCode, request.URL,
		)
		return
	}
	return
}

// Map Alert to an O2 Alarm object.
func (r *AlarmFetcher) mapAlertItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {

	alertName, err := data.GetString(from, "labels.alertname")
	if err != nil {
		return
	}

	alarmRaisedTime, err := data.GetString(from, "startsAt")
	if err != nil {
		return
	}

	alarmChangedTime, err := data.GetString(from, "updatedAt")
	if err != nil {
		return
	}

	severity, err := data.GetString(from, "labels.severity")
	if err != nil {
		return
	}

	clusterId, err := data.GetString(from, "labels.managed_cluster")
	if err != nil {
		return
	}

	resourcePool, err := r.fetchResourcePool(ctx, clusterId)
	if err != nil {
		return
	}
	resourcePoolName, err := data.GetString(resourcePool, "name")
	if err != nil {
		return
	}

	var alarmEventRecordId, resourceID, resourceTypeID string
	alertInstance, err := data.GetString(from, "labels.instance")
	if err != nil {
		// Instance is not available for cluster global alerts
		alarmEventRecordId = fmt.Sprintf("%s_%s", alertName, resourcePoolName)
		resourceID = resourcePool["resourcePoolID"].(string)
	} else {
		alarmEventRecordId = fmt.Sprintf("%s_%s_%s", alertName, resourcePoolName, alertInstance)
		resource, err := r.fetchResource(ctx, resourcePoolName, alertInstance)
		if err == nil {
			resourceID = resource["resourceID"].(string)
			resourceTypeID = resource["resourceTypeID"].(string)
		}
	}

	// Add the extensions:
	extensionsMap, err := data.GetExtensions(from, r.extensions, r.jqTool)
	if err != nil {
		return
	}
	if len(extensionsMap) == 0 {
		// Fallback to all labels and annotations
		var labels, annotations data.Object
		labels, err = data.GetObj(from, "labels")
		if err != nil {
			return
		}
		annotations, err = data.GetObj(from, "annotations")
		if err != nil {
			return
		}
		err = mergo.Map(&extensionsMap, labels, mergo.WithOverride)
		if err != nil {
			return
		}
		err = mergo.Map(&extensionsMap, annotations, mergo.WithOverride)
		if err != nil {
			return
		}
	}

	to = data.Object{
		"alarmEventRecordId": alarmEventRecordId,
		"resourceID":         resourceID,
		"resourceTypeID":     resourceTypeID,
		"alarmRaisedTime":    alarmRaisedTime,
		"alarmChangedTime":   alarmChangedTime,
		"alarmDefinitionID":  alertName,
		"probableCauseID":    alertName,
		"perceivedSeverity":  AlarmSeverity(severity).mapProperty(),
		"extensions":         extensionsMap,
	}

	return
}

func (r *AlarmFetcher) fetchResourcePool(ctx context.Context, clusterId string) (resourcePool data.Object, err error) {
	query := neturl.Values{}
	query.Add("filter", fmt.Sprintf("(eq,description,%s)", clusterId))
	url := r.resourceServerURL + "/resourcePools"
	response, err := r.doGet(ctx, url, r.resourceServerToken, query)
	if err != nil {
		return
	}

	// Create a reader for Resource pools
	resourcePools, err := k8s.NewStream().
		SetLogger(r.logger).
		SetReader(response.Body).
		Build()
	if err != nil {
		return
	}

	resourcePool, err = resourcePools.Next(ctx)

	return
}

func (r *AlarmFetcher) fetchResource(ctx context.Context, clusterName, resourceName string) (resource data.Object, err error) {
	query := neturl.Values{}
	query.Add("filter", fmt.Sprintf("(eq,description,%s)", resourceName))
	path := fmt.Sprintf("/resourcePools/%s/resources", clusterName)
	url := r.resourceServerURL + path
	response, err := r.doGet(ctx, url, r.resourceServerToken, query)
	if err != nil {
		return
	}

	// Create a reader for Resources
	resources, err := k8s.NewStream().
		SetLogger(r.logger).
		SetReader(response.Body).
		Build()
	if err != nil {
		return
	}

	resource, err = resources.Next(ctx)

	return
}

type AlarmSeverity string

func (p AlarmSeverity) mapProperty() string {
	switch p {
	case "critical":
		return "CRITICAL"
	case "info":
		return "MINOR"
	case "warning":
		return "WARNING"
	case "none":
		return "INDETERMINATE"
	default:
		// unknown property
		return "CLEARED"
	}
}
