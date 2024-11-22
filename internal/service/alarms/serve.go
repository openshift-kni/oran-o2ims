package alarms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/dictionary"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/k8s_client"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Alarm server config values
const (
	host         = "127.0.0.1"
	port         = "8080"
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	username = "alarms"
	password = "alarms"
	database = "alarms"
)

// Serve start alarms server
func Serve() error {
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
	hubClient, err := k8s_client.NewClientForHub()
	if err != nil {
		return fmt.Errorf("error creating client for hub: %w", err)
	}

	alarmsDict := dictionary.New(hubClient)
	alarmsDict.Load(ctx)

	// TODO: Audit and Insert data database

	// TODO: Launch k8s job for DB remove archived data

	// Init server
	// Create the handler
	alarmServer := internal.AlarmsServer{
		AlarmsRepository: &repo.AlarmsRepository{
			Db: pool,
		},
	}

	alarmServerStrictHandler := api.NewStrictHandlerWithOptions(&alarmServer, nil,
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  getOranReqErrFunc(),
			ResponseErrorHandlerFunc: getOranRespErrFunc(),
		},
	)

	r := http.NewServeMux()

	opt := api.StdHTTPServerOptions{
		BaseRouter: r,
		Middlewares: []api.MiddlewareFunc{ // Add middlewares here
			internal.AlarmsOapiValidation(),
			internal.LogDuration(),
		},
		ErrorHandlerFunc: getOranReqErrFunc(),
	}

	// Register the handler
	api.HandlerWithOptions(alarmServerStrictHandler, opt)

	// Server config
	srv := &http.Server{
		Handler:      r,
		Addr:         net.JoinHostPort(host, port),
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
		if err := gracefulShutdown(srv); err != nil {
			return err
		}
	}

	return nil
}

// gracefulShutdown allow graceful shutdown with timeout
func gracefulShutdown(srv *http.Server) error {
	// Create shutdown context with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed graceful shutdown: %w", err)
	}

	slog.Info("Server gracefully stopped")
	return nil
}

// getOranReqErrFunc override default validation errors to allow for O-RAN specific struct
func getOranReqErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		})
		http.Error(w, string(out), http.StatusBadRequest)
	}
}

// getOranRespErrFunc override default internal server error to allow for O-RAN specific struct
func getOranRespErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		})
		http.Error(w, string(out), http.StatusInternalServerError)
	}
}
