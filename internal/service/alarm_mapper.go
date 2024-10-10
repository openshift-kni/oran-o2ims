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
	neturl "net/url"

	"github.com/imdario/mergo"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"

	"github.com/itchyny/gojq"
)

type AlarmMapper struct {
	logger              *slog.Logger
	backendClient       *http.Client
	resourceServerURL   string
	resourceServerToken string
	extensions          []string
	jqTool              *jq.Tool
}

// AlarmMapperBuilder contains the data and logic needed to create a new AlarmMapper.
type AlarmMapperBuilder struct {
	logger        *slog.Logger
	backendClient *http.Client
	// transportWrapper    func(http.RoundTripper) http.RoundTripper
	resourceServerURL   string
	resourceServerToken string
	extensions          []string
}

// AlarmMapper creates a builder that can then be used to configure
// and create a handler for the AlarmMapper.
func NewAlarmMapper() *AlarmMapperBuilder {
	return &AlarmMapperBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmMapperBuilder) SetLogger(
	value *slog.Logger) *AlarmMapperBuilder {
	b.logger = value
	return b
}

// SetBackendClient sets the backend client used to connect to other servers. This is mandatory.
func (b *AlarmMapperBuilder) SetBackendClient(value *http.Client) *AlarmMapperBuilder {
	b.backendClient = value
	return b
}

// SetResourceServerURL sets the URL of the resource server. This is mandatory.
// The resource server is used for mapping Alarms to Resources.
func (b *AlarmMapperBuilder) SetResourceServerURL(
	value string) *AlarmMapperBuilder {
	b.resourceServerURL = value
	return b
}

// SetResourceServerToken sets the authentication token that will be used to authenticate
// with to the resource server. This is mandatory.
func (b *AlarmMapperBuilder) SetResourceServerToken(
	value string) *AlarmMapperBuilder {
	b.resourceServerToken = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions. This is optional.
func (b *AlarmMapperBuilder) SetExtensions(values ...string) *AlarmMapperBuilder {
	b.extensions = values
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *AlarmMapperBuilder) Build() (
	result *AlarmMapper, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.backendClient == nil {
		err = errors.New("backend client is mandatory")
		return
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
	result = &AlarmMapper{
		logger:              b.logger,
		backendClient:       b.backendClient,
		resourceServerURL:   b.resourceServerURL,
		resourceServerToken: b.resourceServerToken,
		extensions:          b.extensions,
		jqTool:              jqTool,
	}
	return
}

// Map an Alert to an O2 Alarm object.
func (r *AlarmMapper) MapItem(ctx context.Context,
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
		resourceID = resourcePool["resourcePoolId"].(string)
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

func (r *AlarmMapper) doGet(ctx context.Context, url, token string,
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

func (r *AlarmMapper) fetchResourcePool(ctx context.Context, clusterId string) (resourcePool data.Object, err error) {
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

func (r *AlarmMapper) fetchResource(ctx context.Context, clusterName, resourceName string) (resource data.Object, err error) {
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
