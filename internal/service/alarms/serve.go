package main

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

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/dictionary"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/k8s_client"
)

func main() {
	Serve()
}

// Alarm server config values
const (
	host         = "127.0.0.1"
	port         = "8080"
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second
)

// Serve TODO: Call this func using cobra-cli from inside deployment CR.
func Serve() {
	slog.Info("Starting Alarm server")

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-shutdown
		cancel()
	}()

	// Get client for hub
	hubClient, err := k8s_client.NewClientForHub()
	if err != nil {
		slog.Error("error creating client for hub", "error", err)
		os.Exit(1)
	}

	alarmsDict := dictionary.New(hubClient)
	alarmsDict.Load(ctx)

	// TODO: Init DB client

	// TODO: Audit and Insert data database

	// TODO: Launch k8s job for DB remove archived data

	// Init server
	// Create the handler
	alarmServer := internal.AlarmsServer{}

	alarmServerStrictHandler := api.NewStrictHandlerWithOptions(&alarmServer, nil,
		api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  getOranReqErrFunc(),
			ResponseErrorHandlerFunc: getOranRespErrFunc(),
		})

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
		slog.Error(fmt.Sprintf("error starting server: %s", err))
	case sig := <-ctx.Done():
		slog.Info(fmt.Sprintf("Shutdown signal received: %v", sig))
		if err := gracefulShutdown(srv); err != nil {
			slog.Error(fmt.Sprintf("graceful shutdown failed: %v", err))
		}
	}
}

// gracefulShutdown allow graceful shutdown with timeout
func gracefulShutdown(srv *http.Server) error {
	// Create shutdown context with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		// TODO: handle this error
		return fmt.Errorf("failed graceful shutdown: %w", err)
	}
	slog.Info("Server gracefully stopped")
	return nil
}

// getOranReqErrFunc override default validation errors to allow for O-RAN specific struct
func getOranReqErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(api.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		})
		http.Error(w, string(out), http.StatusBadRequest)
	}
}

// getOranRespErrFunc override default internal server error to allow for O-RAN specific struct
func getOranRespErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(api.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		})
		http.Error(w, string(out), http.StatusInternalServerError)
	}
}
