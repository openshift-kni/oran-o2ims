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
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/openapi"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// MetadataServer creates and returns the `start metadata-server` command.
func MetadataServer() *cobra.Command {
	c := NewMetadataServer()
	result := &cobra.Command{
		Use:   "metadata-server",
		Short: "Starts the metadata server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	_ = flags.String(
		cloudIDFlagName,
		"",
		"O-Cloud identifier.",
	)
	_ = flags.String(
		externalAddressFlagName,
		"",
		"External address.",
	)
	return result
}

// MetadataServerCommand contains the data and logic needed to run the `start
// deployment-manager-server` command.
type MetadataServerCommand struct {
}

// NewMetadataServer creates a new runner that knows how to execute the `start
// deployment-manager-server` command.
func NewMetadataServer() *MetadataServerCommand {
	return &MetadataServerCommand{}
}

// run executes the `start deployment-manager-server` command.
func (c *MetadataServerCommand) run(cmd *cobra.Command, argv []string) error {
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

	// Create the metrics wrapper:
	metricsWrapper, err := metrics.NewHandlerWrapper().
		AddPaths(
			"/o2ims-infrastructureInventory/api_versions",
			"/o2ims-infrastructureInventory/-/api_versions",
			"/o2ims-infrastructureInventory/-/openapi",
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

	// Create the handler that serves the OpenAPI metadata:
	openapiHandler, err := openapi.NewHandler().
		SetLogger(logger).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create OpenAPI handler",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	router.PathPrefix(
		"/o2ims-infrastructureInventory/v1/openapi",
	).Handler(openapiHandler)

	// Create the handler that servers the information about the versions of the API:
	versionsHandler, err := service.NewVersionsHandler().
		SetLogger(logger).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create versions handler",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	versionsAdapter, err := service.NewAdapter().
		SetLogger(logger).
		SetPathVariables("version").
		SetHandler(versionsHandler).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create versions adapter",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/api_versions",
		versionsAdapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/api_versions",
		versionsAdapter,
	).Methods(http.MethodGet)

	// Create the handler that serves the information about the cloud:
	cloudInfoHandler, err := service.NewCloudInfoHandler().
		SetLogger(logger).
		SetCloudID(cloudID).
		SetExternalAddress(externalAddress).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create cloud info handler",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	cloudInfoAdapter, err := service.NewAdapter().
		SetLogger(logger).
		SetHandler(cloudInfoHandler).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create cloud info adapter",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/v1",
		cloudInfoAdapter,
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
		"API server listening",
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
