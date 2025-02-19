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

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/auth"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api/generated"
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
func Serve(config *api.ArtifactsServerConfig) error {
	slog.Info("Starting artifacts server")

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
			RequestErrorHandlerFunc:  middleware.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: middleware.GetOranRespErrFunc(),
		},
	)

	// Create a new logger to be passed where a logger is needed.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug, // TODO: set with server args
	}))

	// Create a response filter filterAdapter that can support the 'filter' and '*fields' query parameters.
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
	generated.HandlerWithOptions(serverStrictHandler, opt)

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

	// Start server
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
		if err := common.GracefulShutdown(srv); err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
	}

	return nil
}
