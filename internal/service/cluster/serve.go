/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

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

	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/auth"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/collector"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo"
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

	password, exists := os.LookupEnv(ctlrutils.ClustersPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", ctlrutils.ClustersPasswordEnvName)
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
	oauthConfig, err := config.CommonServerConfig.CreateOAuthConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to create oauth client configuration: %w", err)
	}

	// Create the built-in data sources
	k8s, err := collector.NewK8SDataSource(cloudID, config.Extensions)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes data source: %w", err)
	}

	alarms, err := collector.NewAlarmsDataSource()
	if err != nil {
		return fmt.Errorf("failed to create Alarms data source: %w", err)
	}

	// Create the notifier with our resource-specific subscription and notification providers.
	notificationsProvider := repo2.NewNotificationStorageProvider(commonRepository)
	subscriptionsProvider := repo2.NewSubscriptionStorageProvider(commonRepository, collector.NewNotificationTransformer())
	clientFactory := notifier.NewClientFactory(oauthConfig, constants.DefaultBackendTokenFile)
	clusterNotifier := notifier.NewNotifier(subscriptionsProvider, notificationsProvider, clientFactory)

	// Create the collector
	clusterCollector := collector.NewCollector(repository, clusterNotifier, []collector.DataSource{k8s, alarms})

	// Init server
	// Create the handler
	server := api.ClusterServer{
		Config:                   config,
		Repo:                     repository,
		SubscriptionEventHandler: clusterNotifier,
	}

	serverStrictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  middleware.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: middleware.GetOranRespErrFunc(),
		},
	)

	// Create a new logger to be passed to things that need a logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug, // TODO: set with server args
	}))

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters
	filterAdapter, err := middleware.NewFilterAdapterFromSwagger(logger, swagger)
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
	generated.HandlerWithOptions(serverStrictHandler, opt)

	// Server config
	// Wrap base router with additional middlewares
	handler := middleware.ChainHandlers(baseRouter,
		middleware.ErrorJsonifier(),
		middleware.TrailingSlashStripper(),
	)

	serverTLSConfig, err := ctlrutils.GetServerTLSConfig(ctx, config.TLS.CertFile, config.TLS.KeyFile)
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
		// Cert/Key files aren't needed here since they've been added to the tls.Config above.
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
