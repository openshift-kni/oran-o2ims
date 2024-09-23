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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start deployment-manager-server` command.
func DeploymentManagerServer() *cobra.Command {
	c := NewDeploymentManagerServer()
	result := &cobra.Command{
		Use:   "deployment-manager-server",
		Short: "Starts the deployment manager server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	AddTokenFlags(flags)
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
		backendTypeFlagName,
		string(service.DeploymentManagerBackendTypeRegularHub),
		fmt.Sprintf(
			"Type of backend. Possible values are '%s' and '%s'.",
			service.DeploymentManagerBackendTypeGlobalHub,
			service.DeploymentManagerBackendTypeRegularHub,
		),
	)
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to deployment managers.",
	)
	_ = flags.String(
		externalAddressFlagName,
		"",
		"External address.",
	)
	return result
}

// DeploymentManagerServerCommand contains the data and logic needed to run the `start
// deployment-manager-server` command.
type DeploymentManagerServerCommand struct {
}

// NewDeploymentManagerServer creates a new runner that knows how to execute the `start
// deployment-manager-server` command.
func NewDeploymentManagerServer() *DeploymentManagerServerCommand {
	return &DeploymentManagerServerCommand{}
}

// run executes the `start deployment-manager-server` command.
func (c *DeploymentManagerServerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	logger := internal.LoggerFromContext(ctx)

	// Get the flags:
	flags := cmd.Flags()

	// Create the exit handler:
	exitHandler, err := exit.NewHandler().
		SetLogger(logger).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create exit handler",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		logger.ErrorContext(
			ctx,
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	logger.InfoContext(
		ctx,
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the backend details:
	backendTypeText, err := flags.GetString(backendTypeFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get backend type flag",
			"flag", backendTypeFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	backendType := service.DeploymentManagerBackendType(backendTypeText)
	switch backendType {
	case service.DeploymentManagerBackendTypeGlobalHub:
	case service.DeploymentManagerBackendTypeRegularHub:
	default:
		logger.ErrorContext(
			ctx,
			"Unknown backend type",
			slog.String("type", backendTypeText),
		)
		return exit.Error(1)
	}
	backendURL, err := flags.GetString(backendURLFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get backend URL flag",
			"flag", backendURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendURL == "" {
		logger.ErrorContext(
			ctx,
			"Backend URL is empty",
			"flag", backendURLFlagName,
		)
		return exit.Error(1)
	}
	extensions, err := flags.GetStringArray(extensionsFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to extension flag",
			"flag", extensionsFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	backendToken, err := GetTokenFlag(ctx, flags, logger)
	if err != nil {
		return exit.Error(1)
	}

	// Write the backend details to the log:
	logger.InfoContext(
		ctx,
		"Backend details",
		slog.String("type", string(backendType)),
		slog.String("url", backendURL),
		slog.String("!token", backendToken),
		slog.Any("extensions", extensions),
	)

	// Get the external address:
	externalAddress, err := flags.GetString(externalAddressFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get external address flag",
			slog.String("flag", externalAddressFlagName),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	logger.InfoContext(
		ctx,
		"External address",
		slog.String("value", externalAddress),
	)

	// Create the logging wrapper:
	loggingWrapper, err := logging.NewTransportWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	// Create the metrics wrapper:
	metricsWrapper, err := metrics.NewHandlerWrapper().
		AddPaths(
			"/o2ims-infrastructureInventory/-/deploymentManagers/-",
		).
		SetSubsystem("inbound").
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create metrics wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Create the router:
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusNotFound, "Not found")
	})
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	})
	router.Use(metricsWrapper)

	// Create the handler:
	handler, err := service.NewDeploymentManagerHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetCloudID(cloudID).
		SetExtensions(extensions...).
		SetBackendType(backendType).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetEnableHack(true).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(logger).
		SetPathVariables("deploymentManagerID").
		SetHandler(handler).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/deploymentManagers",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/deploymentManagers/{deploymentManagerID}",
		adapter,
	).Methods(http.MethodGet)

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	logger.InfoContext(
		ctx,
		"API listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := &http.Server{
		Addr:              apiListener.Addr().String(),
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	exitHandler.AddServer(apiServer)
	go func() {
		err = apiServer.Serve(apiListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(
				ctx,
				"API server finished with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Start the metrics server:
	metricsListener, err := network.NewListener().
		SetLogger(logger).
		SetFlags(flags, network.MetricsListener).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create metrics listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	logger.InfoContext(
		ctx,
		"Metrics server listening",
		slog.String("address", metricsListener.Addr().String()),
	)
	metricsHandler := promhttp.Handler()
	metricsServer := &http.Server{
		Addr:              metricsListener.Addr().String(),
		Handler:           metricsHandler,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	exitHandler.AddServer(metricsServer)
	go func() {
		err = metricsServer.Serve(metricsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(
				ctx,
				"Metrics server finished with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Wait for exit signals
	if err := exitHandler.Wait(ctx); err != nil {
		return fmt.Errorf("failed to wait for exit signals: %w", err)
	}
	return nil
}
