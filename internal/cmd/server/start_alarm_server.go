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

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

const (
	alartmanagerApiUrlPrefix    = "alertmanager-open-cluster-management-observability"
	resourceServerURLFlagName   = "resource-server-url"
	resourceServerTokenFlagName = "resource-server-token"
)

// Server creates and returns the `start alarm-server` command.
func AlarmServer() *cobra.Command {
	c := NewAlarmServer()
	result := &cobra.Command{
		Use:   "alarm-server",
		Short: "Starts the alarm server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	_ = flags.String(
		cloudIDFlagName,
		"",
		"O-Cloud identifier.",
	)
	_ = flags.String(
		backendURLFlagName,
		"",
		"URL of the backend server.",
	)
	_ = flags.String(
		backendTokenFlagName,
		"",
		"Token for authenticating to the backend server.",
	)
	_ = flags.String(
		resourceServerURLFlagName,
		"",
		"URL of the resource server.",
	)
	_ = flags.String(
		resourceServerTokenFlagName,
		"",
		"Token for authenticating to the resource server.",
	)
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to alarms.",
	)
	return result
}

// AlarmServerCommand contains the data and logic needed to run the `start
// alarm-server` command.
type AlarmServerCommand struct {
	logger *slog.Logger
}

// NewAlarmServer creates a new runner that knows how to execute the `start
// alarm-server` command.
func NewAlarmServer() *AlarmServerCommand {
	return &AlarmServerCommand{}
}

// run executes the `start alarm-server` command.
func (c *AlarmServerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)

	// Get the flags:
	flags := cmd.Flags()

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		c.logger.Error(
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the backend details:
	backendURL, err := flags.GetString(backendURLFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get backend URL flag",
			"flag", backendURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendURL == "" {
		c.logger.Error(
			"Backend URL is empty",
			"flag", backendURLFlagName,
		)
		return exit.Error(1)
	}
	backendToken, err := flags.GetString(backendTokenFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get backend token flag",
			"flag", backendTokenFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendToken == "" {
		c.logger.Error(
			"Backend token is empty",
			"flag", backendTokenFlagName,
		)
		return exit.Error(1)
	}

	// Get the resource server details:
	resourceServerURL, err := flags.GetString(resourceServerURLFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get resource server URL flag",
			"flag", resourceServerURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if resourceServerURL == "" {
		c.logger.Error(
			"Resource server URL is empty",
			"flag", resourceServerURLFlagName,
		)
		return exit.Error(1)
	}
	resourceServerToken, err := flags.GetString(resourceServerTokenFlagName)
	if err != nil || resourceServerToken == "" {
		// Fallbacks to backend token
		c.logger.Info("Resource server token wasn't specified, using backend token instead.")
		resourceServerToken = backendToken
	}

	extensions, err := flags.GetStringArray(extensionsFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to extension flag",
			"flag", extensionsFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"Backend details",
		slog.String("url", backendURL),
		slog.String("!token", backendToken),
		slog.Any("extensions", extensions),
	)

	// Create the transport wrapper:
	transportWrapper, err := logging.NewTransportWrapper().
		SetLogger(c.logger).
		SetFlags(flags).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
	}

	// Create the router:
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusNotFound, "Not found")
	})
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	})

	// Generate the search API URL according the backend URL
	backendURL, err = c.generateAlarmmanagerApiUrl(backendURL)
	if err != nil {
		c.logger.Error(
			"Failed to generate search API URL",
			"error", err.Error(),
		)
	}

	// Create the handler for alarms:
	if err := c.createAlarmHandler(
		transportWrapper, router,
		cloudID, backendURL, backendToken, resourceServerURL, resourceServerToken, extensions); err != nil {
		return err
	}

	// Create the handler for alarms probable causes:
	if err := c.createAlarmProbableCausesHandler(router); err != nil {
		return err
	}

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"API listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := http.Server{
		Addr:    apiListener.Addr().String(),
		Handler: router,
	}
	err = apiServer.Serve(apiListener)
	if err != nil {
		c.logger.Error(
			"API server finished with error",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	return nil
}

func (c *AlarmServerCommand) createAlarmHandler(
	transportWrapper func(http.RoundTripper) http.RoundTripper,
	router *mux.Router,
	cloudID, backendURL, backendToken, resourceServerUrl, resourceServerToken string,
	extensions []string) error {

	// Create the handler:
	handler, err := service.NewAlarmHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetResourceServerURL(resourceServerUrl).
		SetResourceServerToken(resourceServerToken).
		SetExtensions(extensions...).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(c.logger).
		SetPathVariables("alarmEventRecordId").
		SetHandler(handler).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarms",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarms/{alarmEventRecordId}",
		adapter,
	).Methods(http.MethodGet)

	return nil
}

// This API is not defined by O2ims Interface Specification.
// It is used for exposing the custom list of alarm probable causes.
func (c *AlarmServerCommand) createAlarmProbableCausesHandler(router *mux.Router) error {

	// This API is not defined by

	// Create the handler:
	handler, err := service.NewAlarmProbableCauseHandler().
		SetLogger(c.logger).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(c.logger).
		SetPathVariables("probableCauseID").
		SetHandler(handler).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarmProbableCauses",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarmProbableCauses/{probableCauseID}",
		adapter,
	).Methods(http.MethodGet)

	return nil
}

func (c *AlarmServerCommand) generateAlarmmanagerApiUrl(backendURL string) (string, error) {
	u, err := url.Parse(backendURL)
	if err != nil {
		return "", err
	}

	// Split URL address
	hostArr := strings.Split(u.Host, ".")

	// Replace with Alarmmanager API prefix
	hostArr[0] = alartmanagerApiUrlPrefix

	// Generate search API URL
	alertmanagerUri := strings.Join(hostArr, ".")
	return fmt.Sprintf("%s://%s/api/v2", u.Scheme, alertmanagerUri), nil
}
