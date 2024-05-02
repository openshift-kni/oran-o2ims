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
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/authentication"
	"github.com/openshift-kni/oran-o2ims/internal/authorization"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start alarm-subscription-server` command.
func AlarmSubscriptionServer() *cobra.Command {
	c := NewAlarmSubscriptionServer()
	result := &cobra.Command{
		Use:   "alarm-subscription-server",
		Short: "Starts the alarm Subscription Server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()

	authentication.AddFlags(flags)
	authorization.AddFlags(flags)

	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	_ = flags.String(
		cloudIDFlagName,
		"",
		"O-Cloud identifier.",
	)
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to alarm subscriptions.",
	)
	return result
}

// alarmSubscriptionServerCommand contains the data and logic needed to run the `start
// alarm-subscription-server` command.
type AlarmSubscriptionServerCommand struct {
}

// NewAlarmSubscriptionServer creates a new runner that knows how to execute the `start
// alarm-subscription-server` command.
func NewAlarmSubscriptionServer() *AlarmSubscriptionServerCommand {
	return &AlarmSubscriptionServerCommand{}
}

// run executes the `start alarm-subscription-server` command.
func (c *AlarmSubscriptionServerCommand) run(cmd *cobra.Command, argv []string) error {
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

	// Get the extensions details:
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
	logger.InfoContext(
		ctx,
		"alarm subscription extensions details",
		slog.Any("extensions", extensions),
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

	// Create the authentication and authorization wrappers:
	authenticationWrapper, err := authentication.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create authentication wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	authorizationWrapper, err := authorization.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create authorization wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Create the metrics wrapper:
	metricsWrapper, err := metrics.NewHandlerWrapper().
		AddPaths(
			"/o2ims-infrastructureMonitoring/-/alarmSubscriptions/-",
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
	router.Use(metricsWrapper, authenticationWrapper, authorizationWrapper)

	// Create the handler:
	handler, err := service.NewAlarmSubscriptionHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetCloudID(cloudID).
		SetExtensions(extensions...).
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
		SetPathVariables("alarmSubscriptionID").
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
		"/o2ims-infrastructureMonitoring/{version}/alarmSubscriptions",
		adapter,
	).Methods(http.MethodGet, http.MethodPost)

	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarmSubscriptions/{alarmSubscriptionID}",
		adapter,
	).Methods(http.MethodGet, http.MethodDelete)

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
		Addr:    apiListener.Addr().String(),
		Handler: router,
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
		Addr:    metricsListener.Addr().String(),
		Handler: metricsHandler,
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

	// Wait for exit signals:
	return exitHandler.Wait(ctx)
}
