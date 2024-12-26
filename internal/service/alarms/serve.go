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

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/clusterserver"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/dictionary_definition"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/notifier_provider"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
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
	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

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
	pool, err := db.NewPgxPool(ctx, db.GetPgConfig(username, password, database))
	if err != nil {
		return fmt.Errorf("failed to connected to DB: %w", err)
	}
	defer func() {
		slog.Info("Closing DB connection")
		pool.Close()
	}()

	// Get client for hub
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return fmt.Errorf("error creating client for hub: %w", err)
	}

	// Init alarm repository
	alarmRepository := &repo.AlarmsRepository{
		Db: pool,
	}

	// TODO: Launch k8s job for DB remove resolved data

	// Parse global cloud id
	var globalCloudID uuid.UUID
	if config.GlobalCloudID != utils.DefaultOCloudID {
		globalCloudID, err = uuid.Parse(config.GlobalCloudID)
		if err != nil {
			return fmt.Errorf("failed to parse global cloud id: %w", err)
		}
	}
	// Add Alarm Service Configuration to the database
	serviceConfig, err := alarmRepository.CreateServiceConfiguration(ctx, api.DefaultRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to create alarm service configuration: %w", err)
	}
	slog.Info("Alarm Service configuration created/found", "retentionPeriod", serviceConfig.RetentionPeriod, "extensions", serviceConfig.Extensions)

	// Init server
	// Create the handler
	alarmServer := api.AlarmsServer{
		GlobalCloudID:    globalCloudID,
		AlarmsRepository: alarmRepository,
	}

	if err := UpdateAlarmDictionaryAndAlarmsDefinitionData(ctx, hubClient, &alarmServer); err != nil {
		return fmt.Errorf("error updating alarms definition data: %w", err)
	}

	// start a new notifier
	if err := startSubscriptionNotifier(ctx, *config, &alarmServer); err != nil {
		return fmt.Errorf("error starting alarms notifier: %w", err)
	}

	alarmServerStrictHandler := generated.NewStrictHandlerWithOptions(&alarmServer, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  common.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: common.GetOranRespErrFunc(),
		},
	)

	r := http.NewServeMux()
	// Register a default handler that replies with 404 so that we can override the response format
	r.HandleFunc("/", common.NotFoundFunc())

	// This also validates the spec file
	swagger, err := generated.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	filterAdapter, err := common.NewFilterAdapter(logger, swagger)
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
	if err := alertmanager.Setup(ctx, hubClient); err != nil {
		return fmt.Errorf("error configuring alert manager: %w", err)
	}

	// Start server
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

// UpdateAlarmDictionaryAndAlarmsDefinitionData reach out to cluster server and sync DB with alarm dict and def
func UpdateAlarmDictionaryAndAlarmsDefinitionData(ctx context.Context, hubClient client.Client, a *api.AlarmsServer) error {
	// Initialize cluster server client
	cs, err := clusterserver.New()
	if err != nil {
		return fmt.Errorf("error creating cluster server client: %w", err)
	}

	// Get all needed objects from the cluster server
	err = cs.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("error getting objects from the cluster server: %w", err)
	}

	// set cluster server data to alarms server
	a.ClusterServer = cs

	// Get all needed resources from the resource server
	if err = cs.GetAll(ctx); err != nil {
		slog.Warn("error getting resources from the resource server", "error", err)
	}

	// Load dictionary and definition
	alarmsDictDef := dictionary_definition.New(hubClient, a.AlarmsRepository)
	// todo Handle me
	if err = alarmsDictDef.Load(ctx, cs.NodeClusterTypes); err != nil {
		slog.Warn("error loading dictionary and definition data", "error", err)
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
	oauthConfig, err := config.CommonServerConfig.CreateOAuthConfig()
	if err != nil {
		return fmt.Errorf("failed to create oauth client configuration for alarms subscribers: %w", err)
	}

	notificationsProvider := notifier_provider.NewNotificationStorageProvider(a.AlarmsRepository)
	subscriptionsProvider := notifier_provider.NewSubscriptionStorageProvider(a.AlarmsRepository)
	newNotifier := notifier.NewNotifier(subscriptionsProvider, notificationsProvider, oauthConfig)

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
