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
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start alarm-notification-server` command.
func AlarmNotificationServer() *cobra.Command {
	c := NewAlarmNotificationServer()
	result := &cobra.Command{
		Use:   "alarm-notification-server",
		Short: "Starts the alarm Notification Server",
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
	_ = flags.String(
		resourceServerURLFlagName,
		"",
		"The resource server URL used by alarm subscriptions ",
	)
	_ = flags.String(
		resourceServerTokenFlagName,
		"",
		"The resource server token used by alarm subscriptions ",
	)
	return result
}

// alarmNotificationServerCommand contains the data and logic needed to run the `start
// alarm-notification-server` command.
type AlarmNotificationServerCommand struct {
}

// NewAlarmNotificationServer creates a new runner that knows how to execute the `start
// alarm-notification-server` command.
func NewAlarmNotificationServer() *AlarmNotificationServerCommand {
	return &AlarmNotificationServerCommand{}
}

// run executes the `start alarm-notification-server` command.
func (c *AlarmNotificationServerCommand) run(cmd *cobra.Command, argv []string) error {
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
		logger.Error(
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		logger.Error(
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Create the logging wrapper:
	loggingWrapper, err := logging.NewTransportWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
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
		logger.Error(
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
		logger.Error(
			"Failed to create authorization wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Create the metrics wrapper:
	metricsWrapper, err := metrics.NewHandlerWrapper().
		AddPaths(
			"/o2ims-infrastructureMonitoring/-/alarmNotifications/-",
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

	// create k8s client with kube(from env first)
	//var config *rest.Config
	kubeClient, err := k8s.NewClient().SetLogger(logger).SetLoggingWrapper(loggingWrapper).Build()

	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create kubeClient",
			"error", err,
		)
		return exit.Error(1)
	}

	// Get the resource server details:
	resourceServerURL, err := flags.GetString(resourceServerURLFlagName)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to get resource server URL flag",
			"flag", resourceServerURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if resourceServerURL == "" {
		logger.ErrorContext(
			ctx,
			"Resource server URL is empty",
			slog.String("flag", resourceServerURLFlagName),
		)
		return exit.Error(1)
	}
	resourceServerToken, err := flags.GetString(resourceServerTokenFlagName)
	if err != nil || resourceServerToken == "" {
		logger.ErrorContext(
			ctx,
			"Resource server URL is empty",
			slog.String("flag", resourceServerTokenFlagName),
		)
		return exit.Error(1)
	}

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

	//create handler
	handler, err := service.NewAlarmNotificationHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetCloudID(cloudID).
		SetKubeClient(kubeClient).
		SetNamespace(namespace).
		SetConfigmapName(subscriptionsConfigmapName).
		SetResourceServerURL(resourceServerURL).
		SetResourceServerToken(resourceServerToken).
		Build(ctx)

	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	router.Handle(
		"/",
		handler,
	).Methods(http.MethodPost)

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		logger.Error(
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
