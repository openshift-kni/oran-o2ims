package alarms

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/serviceconfig"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/notifier_provider"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
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

	go func() {
		sig := <-shutdown
		slog.Info("Shutdown signal received", "signal", sig)
		cancel()
	}()

	password, exists := os.LookupEnv(utils.AlarmsPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.AlarmsPasswordEnvName)
	}

	// Init DB client
	pgConfig := db.GetPgConfig(username, password, database)
	pool, err := db.NewPgxPool(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connected to DB: %w", err)
	}
	defer func() {
		slog.Info("Closing DB connection")
		pool.Close()
	}()

	// Init infrastructure clients
	infrastructureClients, err := infrastructure.Init(ctx)
	if err != nil {
		return fmt.Errorf("error setting up and collecting objects from infrastructure servers: %w", err)
	}

	// Init alarm repository
	alarmRepository := &repo.AlarmsRepository{
		Db: pool,
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

	// Start a new notifier
	if err := startSubscriptionNotifier(ctx, *config, &alarmServer); err != nil {
		return fmt.Errorf("error starting alarms notifier: %w", err)
	}

	// Configure server and start alarms cleanup cronjob
	if err := ConfigAlarmServerCleanup(ctx, &alarmServer, pgConfig); err != nil {
		return fmt.Errorf("failed configure and start cleanup cronjob: %w", err)
	}

	alarmServerStrictHandler := generated.NewStrictHandlerWithOptions(&alarmServer, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  common.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: common.GetOranRespErrFunc(),
		},
	)

	r := common.NewErrorJsonifier(http.NewServeMux())

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	filterAdapter, err := common.NewFilterAdapter(logger)
	if err != nil {
		return fmt.Errorf("error creating filter filterAdapter: %w", err)
	}

	opt := generated.StdHTTPServerOptions{
		BaseRouter: r,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			common.OpenAPIValidation(swagger),
			common.ResponseFilter(filterAdapter),
			common.LogDuration(),
		},
		ErrorHandlerFunc: common.GetOranReqErrFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(alarmServerStrictHandler, opt)

	// Server config
	srv := &http.Server{
		Handler:      r,
		Addr:         config.Listener.Address,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog: slog.NewLogLogger(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
		}), slog.LevelError),
	}

	// Channel to listen for errors coming from the listener.
	serverErrors := make(chan error, 1)

	// Configure AM right before the server starts listening
	if err := alertmanager.Setup(ctx); err != nil {
		return fmt.Errorf("error configuring alert manager: %w", err)
	}

	// Init server
	go func() {
		slog.Info(fmt.Sprintf("Listening on %s", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

// startSubscriptionNotifier set a new startSubscriptionNotifier for alarms server
func startSubscriptionNotifier(ctx context.Context, config api.AlarmsServerConfig, a *api.AlarmsServer) error {
	// Create the OAuth client config
	oauthConfig, err := config.CommonServerConfig.CreateOAuthConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to create oauth client configuration for alarms subscribers: %w", err)
	}

	notificationsProvider := notifier_provider.NewNotificationStorageProvider(a.AlarmsRepository, a.GlobalCloudID)
	subscriptionsProvider := notifier_provider.NewSubscriptionStorageProvider(a.AlarmsRepository)
	clientFactory := notifier.NewClientFactory(oauthConfig, utils.DefaultBackendTokenFile)
	newNotifier := notifier.NewNotifier(subscriptionsProvider, notificationsProvider, clientFactory)

	a.NotificationProvider = notificationsProvider
	a.Notifier = newNotifier

	// Start alarms notifier
	go func() {
		slog.Info("Starting alarms subs notifier")
		if err := newNotifier.Run(ctx); err != nil {
			slog.Error("notifier error", "error", err)
		}
	}()

	return nil
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
