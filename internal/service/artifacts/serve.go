package resources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api/generated"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

// Resource server config values
const (
	host         = "127.0.0.1"
	port         = "8000"
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second
)

// Serve start artifacts server
func Serve() error {
	slog.Info("Starting artifacts server")
	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		sig := <-shutdown
		slog.Info("Shutdown signal received", "signal", sig)
		cancel()
	}()

	// Get client for hub
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return fmt.Errorf("error creating client for hub: %w", err)
	}

	// Init server
	// Create the handler
	server := api.ArtifactsServer{
		HubClient: hubClient,
	}

	serverStrictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  common.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: common.GetOranRespErrFunc(),
		},
	)

	router := http.NewServeMux()
	// Register a default handler that replies with 404 so that we can override the response format.
	router.HandleFunc("/", common.NotFoundFunc())

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
		return fmt.Errorf("error starting server: %w", err)
	case <-ctx.Done():
		slog.Info("Shutting down server")
		if err := common.GracefulShutdown(srv); err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
	}

	return nil
}
