package resources

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

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/collector"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

// Resource server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	username = "resources"
	database = "resources"
)

// Serve start alarms server
func Serve(config *api.ResourceServerConfig) error {
	slog.Info("Starting resource server")
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

	password, exists := os.LookupEnv(utils.ResourcesPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.ResourcesPasswordEnvName)
	}

	// Init DB client
	pool, err := db.NewPgxPool(ctx, db.GetPgConfig(username, password, database))
	if err != nil {
		return fmt.Errorf("failed to connected to DB: %w", err)
	}
	defer func() {
		slog.Info("Closing DB connection")
		pool.Close()
	}()

	// Init the repositories
	repository := &repo.ResourcesRepository{
		Db: pool,
	}

	// Convert arguments
	var globalCloudID uuid.UUID
	if config.GlobalCloudID != utils.DefaultOCloudID {
		value, err := uuid.Parse(config.GlobalCloudID)
		if err != nil {
			return fmt.Errorf("failed to parse global cloud NotificationID '%s': %w", config.GlobalCloudID, err)
		}
		globalCloudID = value
	}

	cloudID, err := uuid.Parse(config.CloudID)
	if err != nil {
		return fmt.Errorf("failed to parse cloud NotificationID '%s': %w", config.CloudID, err)
	}

	// Create the build-in data sources
	acm, err := collector.NewACMDataSource(cloudID, globalCloudID, config.BackendURL, config.Extensions)
	if err != nil {
		return fmt.Errorf("failed to create ACM data source: %w", err)
	}

	// Create the notifier with our resource specific subscription and notification providers.
	notificationsProvider := repo.NewNotificationStorageProvider(repository)
	subscriptionsProvider := repo.NewSubscriptionStorageProvider(repository)
	resourceNotifier := notifier.NewNotifier(subscriptionsProvider, notificationsProvider)

	// Create the collector
	resourceCollector := collector.NewCollector(repository, resourceNotifier, []collector.DataSource{acm})

	// Init server
	// Create the handler
	server := api.ResourceServer{
		Config: config,
		Repo:   repository,
		Info: generated.OCloudInfo{
			Description:   "OpenShift O-Cloud Manager",
			GlobalCloudId: globalCloudID,
			Name:          "OpenShift O-Cloud Manager",
			OCloudId:      cloudID,
			ServiceUri:    config.ExternalAddress,
		},
		SubscriptionEventHandler: resourceNotifier,
	}

	serverStrictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  common.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: common.GetOranRespErrFunc(),
		},
	)

	router := http.NewServeMux()

	// This also validates the spec file
	swagger, err := generated.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}

	opt := generated.StdHTTPServerOptions{
		BaseRouter: router,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			common.OpenAPIValidation(swagger),
			common.LogDuration(),
		},
		ErrorHandlerFunc: common.GetOranReqErrFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(serverStrictHandler, opt)

	// Server config
	srv := &http.Server{
		Handler:      router,
		Addr:         config.Address,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog: slog.NewLogLogger(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
		}), slog.LevelError),
	}

	// Start resource notifier
	notifierErrors := make(chan error, 1)
	go func() {
		slog.Info("Starting resource notifier")
		if err := resourceNotifier.Run(ctx); err != nil {
			notifierErrors <- err
		}
	}()

	// Start resource collector
	collectorErrors := make(chan error, 1)
	go func() {
		slog.Info("Starting resource collector")
		if err := resourceCollector.Run(ctx); err != nil {
			collectorErrors <- err
		}
	}()

	// Start server
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info(fmt.Sprintf("Listening on %s", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	defer func() {
		// Cancel the context in case it wasn't already canceled
		cancel()
		// Shutdown the http server
		slog.Info("Shutting down server")
		if err := common.GracefulShutdown(srv); err != nil {
			slog.Error("error shutting down server", "error", err)
		}
	}()

	// Blocking select
	select {
	case err := <-serverErrors:
		return fmt.Errorf("error starting server: %w", err)
	case err := <-collectorErrors:
		return fmt.Errorf("error starting collector: %w", err)
	case err := <-notifierErrors:
		return fmt.Errorf("error starting notifier: %w", err)
	case <-ctx.Done():
		slog.Info("Process shutting down")
	}

	return nil
}
