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

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/dictionary"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/resourceserver"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Alarm server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	username = "alarms"
	database = "alarms"
)

type AlarmsServerConfig struct {
	Address       string
	GlobalCloudID string
}

// Serve start alarms server
func Serve(config *AlarmsServerConfig) error {
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

	// Initialize resource server client
	rs, err := resourceserver.New()
	if err != nil {
		return fmt.Errorf("error creating resource server client: %w", err)
	}

	// Get all needed resources from the resource server
	err = rs.GetAll(ctx)
	if err != nil {
		slog.Warn("error getting resources from the resource server", "error", err)
	}

	// Init alarm repository
	alarmRepository := &repo.AlarmsRepository{
		Db: pool,
	}

	// Load dictionary
	alarmsDict := dictionary.New(hubClient, alarmRepository)
	alarmsDict.Load(ctx, rs.ResourceTypes)

	// TODO: Audit and Insert data database

	// TODO: Launch k8s job for DB remove archived data

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
	alarmServer := internal.AlarmsServer{
		GlobalCloudID:    globalCloudID,
		AlarmsRepository: alarmRepository,
		ResourceServer:   rs,
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

	opt := generated.StdHTTPServerOptions{
		BaseRouter: r,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			common.OpenAPIValidation(swagger),
			common.LogDuration(),
		},
		ErrorHandlerFunc: common.GetOranReqErrFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(alarmServerStrictHandler, opt)

	// Server config
	srv := &http.Server{
		Handler:      r,
		Addr:         config.Address,
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
		if err := common.GracefulShutdown(srv); err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
	}

	return nil
}
