/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

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

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api"
	hwpluginserver "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/server"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/auth"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// Metal3 HardwarePlugin Server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second
)

// Serve starts the Metal3 HardwarePlugin API server and blocks until it terminates or context is canceled.
func Serve(ctx context.Context, config svcutils.CommonServerConfig, hubClient client.Client) error {
	slog.Info("Initializing the Metal3 HardwarePlugin server")

	// Retrieve the OpenAPI spec file
	swagger, err := hwpluginserver.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sig := <-shutdown
		slog.InfoContext(ctx, "Shutdown signal received", slog.String("signal", sig.String()))
		cancel()
	}()

	// Init metal3Server
	metal3Server, err := NewMetal3PluginServer(
		config,
		hubClient,
		slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		})))
	if err != nil {
		return fmt.Errorf("failed to build Metal3 HardwarePlugin server: %w", err)
	}

	serverStrictHandler := hwpluginserver.NewStrictHandlerWithOptions(metal3Server, nil,
		hwpluginserver.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  api.GetRequestErrorFunc(),
			ResponseErrorHandlerFunc: api.GetResponseErrorFunc(),
		},
	)

	baseRouter := http.NewServeMux()
	// Register a default handler that replies with 404 so that we can override the response format
	baseRouter.HandleFunc("/", api.GetNotFoundFunc())

	// Create authn/authz middleware
	authn, err := auth.GetAuthenticator(ctx, &config)
	if err != nil {
		return fmt.Errorf("error setting up Metal3 HardwarePlugin authenticator middleware: %w", err)
	}

	authz, err := auth.GetAuthorizer()
	if err != nil {
		return fmt.Errorf("error setting up Metal3 HardwarePlugin authorizer middleware: %w", err)
	}

	opt := hwpluginserver.StdHTTPServerOptions{
		BaseRouter: baseRouter,
		Middlewares: []hwpluginserver.MiddlewareFunc{
			api.GetOpenAPIValidationFunc(swagger),
			authz,
			authn,
			api.GetLogDurationFunc(),
		},
		ErrorHandlerFunc: api.GetRequestErrorFunc(),
	}

	// Register the handler
	hwpluginserver.HandlerWithOptions(serverStrictHandler, opt)

	handler := middleware.ChainHandlers(
		baseRouter,
		middleware.ErrorJsonifier(),
		middleware.TrailingSlashStripper(),
	)

	serverTLSConfig, err := utils.GetServerTLSConfig(ctx, config.TLS.CertFile, config.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to get Metal3 HardwarePlugin server TLS config: %w", err)
	}

	srv := &http.Server{
		Handler:      handler,
		Addr:         config.Listener.Address,
		TLSConfig:    serverTLSConfig,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog:     slog.NewLogLogger(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{AddSource: true}), slog.LevelError),
	}

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
		// Shutdown the Metal3 HardwarePlugin server
		slog.Info("Shutting down Metal3 HardwarePlugin server")
		if err := common.GracefulShutdown(srv); err != nil {
			slog.Error("error shutting down Metal3 HardwarePlugin server", "error", err)
		}
	}()

	// Blocking select
	select {
	case err := <-serverErrors:
		return fmt.Errorf("error starting Metal3 HardwarePlugin server: %w", err)
	case <-ctx.Done():
		slog.Info("Process shutting down Metal3 HardwarePlugin server")
	}

	return nil
}
