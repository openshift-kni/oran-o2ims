package cluster

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
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/collector"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	repo2 "github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
)

// Resource server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	username = "clusters"
	database = "clusters"
)

// Serve start alarms server
func Serve(config *api.ClusterServerConfig) error {
	slog.Info("Starting cluster server")
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

	password, exists := os.LookupEnv(utils.ClustersPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.ClustersPasswordEnvName)
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
	commonRepository := &repo2.CommonRepository{
		Db: pool,
	}
	repository := &repo.ClusterRepository{
		CommonRepository: *commonRepository,
	}

	cloudID, err := uuid.Parse(config.CloudID)
	if err != nil {
		return fmt.Errorf("failed to parse cloud NotificationID '%s': %w", config.CloudID, err)
	}

	// Create the OAuth client config
	oauthConfig, err := config.CommonServerConfig.CreateOAuthConfig()
	if err != nil {
		return fmt.Errorf("failed to create oauth client configuration: %w", err)
	}

	// Create the built-in data sources
	k8s, err := collector.NewK8SDataSource(cloudID, config.Extensions)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes data source: %w", err)
	}

	// Create the notifier with our resource specific subscription and notification providers.
	notificationsProvider := repo2.NewNotificationStorageProvider(commonRepository)
	subscriptionsProvider := repo2.NewSubscriptionStorageProvider(commonRepository)
	clusterNotifier := notifier.NewNotifier(subscriptionsProvider, notificationsProvider, oauthConfig)

	// Create the collector
	clusterCollector := collector.NewCollector(repository, clusterNotifier, []collector.DataSource{k8s})

	// Init server
	// Create the handler
	server := api.ClusterServer{
		Config:                   config,
		Repo:                     repository,
		SubscriptionEventHandler: clusterNotifier,
	}

	serverStrictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  common.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: common.GetOranRespErrFunc(),
		},
	)

	router := http.NewServeMux()
	// Register a default handler that replies with 404 so that we can override the response format
	router.HandleFunc("/", common.NotFoundFunc())

	// Create a new logger to be passed to things that need a logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug, // TODO: set with server args
	}))

	// This also validates the spec file
	swagger, err := generated.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters
	filterAdapter, err := common.NewFilterAdapter(logger, swagger)
	if err != nil {
		return fmt.Errorf("error creating filter filterAdapter: %w", err)
	}

	opt := generated.StdHTTPServerOptions{
		BaseRouter: router,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			common.OpenAPIValidation(swagger),
			common.ResponseFilter(filterAdapter),
			common.LogDuration(),
		},
		ErrorHandlerFunc: common.GetOranReqErrFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(serverStrictHandler, opt)

	// Server config
	srv := &http.Server{
		Handler:      router,
		Addr:         config.Listener.Address,
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
		slog.Info("Starting cluster notifier")
		if err := clusterNotifier.Run(ctx); err != nil {
			notifierErrors <- err
		}
	}()

	// Start resource collector
	collectorErrors := make(chan error, 1)
	go func() {
		slog.Info("Starting cluster collector")
		if err := clusterCollector.Run(ctx); err != nil {
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
