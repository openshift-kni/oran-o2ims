/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alarms

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/listener"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/notifier_provider"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/serviceconfig"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/auth"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// Alarm server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	username = "alarms"
	database = "alarms"
)

// Serve start alarms server
func Serve(config *api.AlarmsServerConfig) error {
	slog.Info("Starting Alarm server")

	// Get and validate the openapi spec file
	swagger, err := generated.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}
	if err := swagger.Validate(context.Background(),
		openapi3.EnableSchemaDefaultsValidation(), // Validate default values
		openapi3.EnableSchemaFormatValidation(),   // Validate standard formats
		openapi3.EnableSchemaPatternValidation(),  // Validate regex patterns
		openapi3.EnableExamplesValidation(),       // Validate examples
		openapi3.ProhibitExtensionsWithRef(),      // Prevent x- extension fields
	); err != nil {
		return fmt.Errorf("failed validate swagger: %w", err)
	}

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Recovery defer to print panic strace
	defer func() {
		if r := recover(); r != nil {
			slog.Error("something went wrong", "stacktrace", string(debug.Stack()))
		}
	}()

	go func() {
		sig := <-shutdown
		slog.Info("Shutdown signal received", "signal", sig)
		cancel()
	}()

	password, exists := os.LookupEnv(utils.AlarmsPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.AlarmsPasswordEnvName)
	}

	// Init DB client and alarm repository
	pgConfig := db.GetPgConfig(username, password, database)
	pool, err := db.NewPgxPool(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connected to DB: %w", err)
	}
	defer func() {
		// Try to close the pool with a timeout
		closeComplete := make(chan struct{})
		go func() {
			pool.Close()
			close(closeComplete)
		}()

		// If there's panic during server init, DB pool.Close() may deadlock - handling this with timeout
		// Wait for either close completion or timeout
		select {
		case <-closeComplete:
			slog.Info("Closed DB connection")
		case <-time.After(5 * time.Second):
			slog.Warn("DB connection close timed out")
		}
	}()
	alarmRepository := &repo.AlarmsRepository{
		Db: pool,
	}

	// Init infrastructure clients
	infrastructureClients, err := infrastructure.Init(ctx)
	if err != nil {
		return fmt.Errorf("error setting up and collecting objects from infrastructure servers: %w", err)
	}

	// Parse global cloud id
	var globalCloudID uuid.UUID
	if config.GlobalCloudID != utils.DefaultOCloudID {
		globalCloudID, err = uuid.Parse(config.GlobalCloudID)
		if err != nil {
			return fmt.Errorf("failed to parse global cloud id: %w", err)
		}
	}

	// Init server
	// Create the handler
	alarmServer := api.AlarmsServer{
		GlobalCloudID:    globalCloudID,
		AlarmsRepository: alarmRepository,
		Infrastructure:   infrastructureClients,
	}

	// Create the OAuth client config
	oauthConfig, err := config.CommonServerConfig.CreateOAuthConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to create oauth client configuration for alarms subscribers: %w", err)
	}

	newNotifier := notifier.NewNotifier(
		notifier_provider.NewSubscriptionStorageProvider(alarmRepository),
		notifier_provider.NewNotificationStorageProvider(alarmRepository, globalCloudID),
		notifier.NewClientFactory(oauthConfig, utils.DefaultBackendTokenFile),
	)

	// Attribute needed when subscription event happens
	alarmServer.SubscriptionEventHandler = newNotifier

	// Start alarms notifier
	alarmServer.Wg.Add(1)
	go func() {
		defer alarmServer.Wg.Done()
		slog.Info("Starting alarms subs notifier")
		if err := newNotifier.Run(ctx); err != nil {
			slog.Error("notifier error", "error", err)
		}
	}()

	// Start listening for alarms events using pg listen/notify
	alarmServer.Wg.Add(1)
	go func() {
		defer alarmServer.Wg.Done()
		listener.ListenForAlarmsPgChannels(ctx, pool, newNotifier, globalCloudID)
		slog.Info("Done listening to alarms pg channels")
	}()

	// Configure server and start alarms cleanup cronjob
	if err := ConfigAlarmServerCleanup(ctx, &alarmServer, pgConfig); err != nil {
		return fmt.Errorf("failed configure and start cleanup cronjob: %w", err)
	}

	alarmServerStrictHandler := generated.NewStrictHandlerWithOptions(&alarmServer, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  middleware.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: middleware.GetOranRespErrFunc(),
		},
	)

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	filterAdapter, err := middleware.NewFilterAdapter(logger)
	if err != nil {
		return fmt.Errorf("error creating filter filterAdapter: %w", err)
	}

	// Create authn/authz middleware
	authn, err := auth.GetAuthenticator(ctx, &config.CommonServerConfig)
	if err != nil {
		return fmt.Errorf("error setting up authenticator middleware: %w", err)
	}

	authz, err := auth.GetAuthorizer()
	if err != nil {
		return fmt.Errorf("error setting up authorizer middleware: %w", err)
	}

	baseRouter := http.NewServeMux()
	opt := generated.StdHTTPServerOptions{
		BaseRouter: baseRouter,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			middleware.OpenAPIValidation(swagger),
			middleware.ResponseFilter(filterAdapter),
			authz,
			authn,
			middleware.LogDuration(),
		},
		ErrorHandlerFunc: middleware.GetOranReqErrFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(alarmServerStrictHandler, opt)

	// Server config
	// Wrap base router with additional middlewares
	handler := middleware.ChainHandlers(baseRouter,
		middleware.ErrorJsonifier(),
		middleware.TrailingSlashStripper(),
	)

	serverTLSConfig, err := utils.GetServerTLSConfig(ctx, config.TLS.CertFile, config.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to get server TLS config: %w", err)
	}

	srv := &http.Server{
		Handler:      handler,
		Addr:         config.Listener.Address,
		TLSConfig:    serverTLSConfig,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog: slog.NewLogLogger(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
		}), slog.LevelError),
	}

	// Channel to listen for errors coming from the listener.
	serverErrors := make(chan error, 1)

	// Configure webhook and init DB with CaaS alerts using AM right before the server starts listening
	// Also start alert sync scheduler
	if err := startAlertmanager(ctx, &alarmServer); err != nil {
		return fmt.Errorf("failed to start alartmanager: %w", err)

	}
	// Init server
	go func() {
		slog.Info(fmt.Sprintf("Listening on %s", srv.Addr))
		// Cert/Key files aren't needed here since they've been added to the tls.Config above.
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Blocking select
	select {
	case err := <-serverErrors:
		return fmt.Errorf("error starting server: %w", err)
	case <-ctx.Done():
		slog.Info("Shutting down server")
		if err := gracefulShutdownWithTasks(srv, &alarmServer); err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
	}

	return nil
}

// gracefulShutdownWithTasks Server may have background tasks running when SIGTERM is received. Let them continue.
func gracefulShutdownWithTasks(srv *http.Server, alarmsServer *api.AlarmsServer) error {
	done := make(chan struct{})
	go func() {
		alarmsServer.Wg.Wait()
		close(done)
	}()

	serverErr := common.GracefulShutdown(srv)

	slog.Info("Waiting for alarms server background tasks to finish")
	select {
	case <-done:
		slog.Info("Successfully finished all alarms server background tasks")
		return serverErr //nolint:wrapcheck
	case <-time.After(29 * time.Second): // TODO: Set timeout to match terminationGracePeriodSeconds (default 30s) minus buffer for remaining defers
		if serverErr != nil {
			return fmt.Errorf("shutdown failed and tasks timed out: %w", serverErr)
		}
		return fmt.Errorf("alarms server background tasks timed out")
	}
}

// ConfigAlarmServerCleanup configure server and launch the cleanup cronjob for resolved alarm events
func ConfigAlarmServerCleanup(ctx context.Context, alarmServer *api.AlarmsServer, pgConfig db.PgConfig) error {
	// Add Alarm Service Configuration to the database
	serviceConfig, err := alarmServer.AlarmsRepository.CreateServiceConfiguration(ctx, api.DefaultRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to create alarm service configuration: %w", err)
	}
	slog.Info("Alarm Service configuration created/found", "retentionPeriod", serviceConfig.RetentionPeriod, "extensions", serviceConfig.Extensions)

	// Init ServiceConfig and start cronjob
	alarmServer.ServiceConfig, err = serviceconfig.LoadEnvConfig()
	if err != nil {
		return fmt.Errorf("failed to load alarm service configuration: %w", err)
	}
	alarmServer.ServiceConfig.PgConnConfig = pgConfig
	clientForHub, err := k8s.NewClientForHub()
	if err != nil {
		return fmt.Errorf("failed to create k8s client for hub: %w", err)
	}
	alarmServer.ServiceConfig.HubClient = clientForHub

	if err := alarmServer.ServiceConfig.EnsureCleanupCronJob(ctx, serviceConfig); err != nil {
		return fmt.Errorf("failed to start alarms cleanup cron job: %w", err)
	}
	slog.Info("Successfully created initial set of cronjob and resources")

	return nil
}

// Init everything needed to start using ACM's alertmanager
// Collect the full set of alerts with API, start a background sync and finally open up webhook
func startAlertmanager(ctx context.Context, alarmServer *api.AlarmsServer) error {
	hubclient, err := k8s.NewClientForHub()
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	// Run the first sync - running it outside also makes sure connectivity is good before server starts listening
	c := alertmanager.NewAlertManagerClient(hubclient, alarmServer.AlarmsRepository, alarmServer.Infrastructure)
	slog.Info("Running initial alert sync")
	if err := c.SyncAlerts(ctx); err != nil {
		return fmt.Errorf("failed to run initial alert sync: %w", err)
	}

	// Start alert sync scheduler with a go routine
	alarmServer.Wg.Add(1)
	go func() {
		defer alarmServer.Wg.Done()
		if err := c.RunAlertSyncScheduler(ctx, 1*time.Hour); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("failed to run alert sync scheduler", "error", err)
			}
		}
	}()

	// Configure webhook by programmatically merging with the existing config
	if err := alertmanager.Setup(ctx); err != nil {
		return fmt.Errorf("error configuring alert manager: %w", err)
	}

	slog.Info("Successfully did the first sync and configured alertmanager")
	return nil
}
