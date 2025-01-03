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
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// AlarmSubscriptionServer Server creates and returns the `start alarm-subscription-server` command.
func AlarmSubscriptionServer() *cobra.Command {
	c := NewAlarmSubscriptionServer()
	result := &cobra.Command{
		Use:   "alarm-subscription-server",
		Short: "Starts the alarm Subscription Server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()

	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	_ = flags.String(
		GlobalCloudIDFlagName,
		"",
		"Global O-Cloud identifier.",
	)
	_ = flags.StringArray(
		ExtensionsFlagName,
		[]string{},
		"Extension to add to alarm subscriptions.",
	)
	_ = flags.String(
		namespaceFlagName,
		"",
		"The namespace the server is running",
	)
	_ = flags.String(
		subscriptionConfigmapNameFlagName,
		"",
		"The configmap name used by alarm subscriptions ",
	)
	return result
}

// AlarmSubscriptionServerCommand contains the data and logic needed to run the `start
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
	globalCloudID, err := flags.GetString(GlobalCloudIDFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get global cloud identifier flag",
			"flag", GlobalCloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if globalCloudID == "" {
		logger.ErrorContext(
			ctx,
			"Global cloud identifier is empty",
			"flag", GlobalCloudIDFlagName,
		)
		return exit.Error(1)
	}
	logger.InfoContext(
		ctx,
		"Global cloud identifier",
		"value", globalCloudID,
	)

	// Get the extensions details:
	extensions, err := flags.GetStringArray(ExtensionsFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to extension flag",
			"flag", ExtensionsFlagName,
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
	router.Use(metricsWrapper)

	// create k8s client with kube(from env first)
	kubeClient, err := k8s.NewClient().SetLogger(logger).SetLoggingWrapper(loggingWrapper).Build()

	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create kubeClient",
			"error", err,
		)
		return exit.Error(1)
	}

	// Get the namespace:
	namespace, err := flags.GetString(namespaceFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get o2ims namespace flag",
			slog.String("flag", namespaceFlagName),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	if namespace == "" {
		namespace = service.DefaultNamespace
	}

	// Get the configmapName:
	subscriptionsConfigmapName, err := flags.GetString(subscriptionConfigmapNameFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get alarm subscription configmap name flag",
			slog.String("flag", subscriptionConfigmapNameFlagName),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	if subscriptionsConfigmapName == "" {
		subscriptionsConfigmapName = service.DefaultAlarmConfigmapName
	}

	handler, err := service.NewSubscriptionHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetGlobalCloudID(globalCloudID).
		SetExtensions(extensions...).
		SetKubeClient(kubeClient).
		SetSubscriptionIdString(service.SubscriptionIdAlarm).
		SetNamespace(namespace).
		SetConfigmapName(subscriptionsConfigmapName).
		Build(ctx)

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
			"Failed to create API listener",
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
