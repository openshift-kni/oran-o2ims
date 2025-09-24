/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package narcallback

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/auth"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second

	// Validation constants for callback payloads
	maxNodeAllocationRequestIdLength = 253 // Kubernetes object name max length
	maxErrorMessageLength            = 1000
	maxProvisioningRequestNameLength = 253
)

// HardwarePluginServer implements StrictServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ StrictServerInterface = (*NodeAllocationRequestCallbackServer)(nil)

var baseURL = constants.NarCallbackBaseURL
var currentVersion = "1.0.0"

type NodeAllocationRequestCallbackServer struct {
	utils.CommonServerConfig
	HubClient client.Client
	Logger    *slog.Logger
}

func NewNodeAllocationRequestCallbackServer(client client.Client, logger *slog.Logger) *NodeAllocationRequestCallbackServer {
	return &NodeAllocationRequestCallbackServer{
		HubClient: client,
		Logger:    logger,
	}
}

// Serve starts the NodeAllocationRequest callback server using the generated OpenAPI interface
func (s *NodeAllocationRequestCallbackServer) Serve(ctx context.Context, config utils.CommonServerConfig) error {
	s.Logger.Info("Initializing the NAR Callback server")

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sig := <-shutdown
		s.Logger.InfoContext(ctx, "Shutdown signal received", slog.String("signal", sig.String()))
		cancel()
	}()

	// Retrieve the OpenAPI spec file for validation
	narCallbackAPIswagger, err := GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get NAR Callback swagger: %w", err)
	}

	// Create authn/authz middleware
	authn, err := auth.GetAuthenticator(ctx, &config)
	if err != nil {
		return fmt.Errorf("error setting up NAR callback server authenticator middleware: %w", err)
	}

	authz, err := auth.GetAuthorizer()
	if err != nil {
		return fmt.Errorf("error setting up NAR callback server authorizer middleware: %w", err)
	}

	// Create the NodeAllocationRequest Callback strict handler using the generated interface
	narCallbackServerStrictHandler := NewStrictHandlerWithOptions(s, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  api.GetRequestErrorFunc(),
		ResponseErrorHandlerFunc: api.GetResponseErrorFunc(),
	})

	// Create base router
	baseRouter := http.NewServeMux()

	// Register a default handler that replies with 404 to override the response format
	baseRouter.HandleFunc("/", api.GetNotFoundFunc())

	// Register the generated handlers with middleware
	apiHandler := HandlerWithOptions(narCallbackServerStrictHandler, StdHTTPServerOptions{
		BaseRouter: baseRouter,
		Middlewares: []MiddlewareFunc{
			api.GetOpenAPIValidationFunc(narCallbackAPIswagger),
			authz,
			authn,
			api.GetLogDurationFunc(),
		},
		ErrorHandlerFunc: api.GetRequestErrorFunc(),
	})

	// Apply global middleware chain
	handler := middleware.ChainHandlers(
		apiHandler,
		middleware.ErrorJsonifier(),
		middleware.TrailingSlashStripper(),
	)

	serverTLSConfig, err := ctlrutils.GetServerTLSConfig(ctx, config.TLS.CertFile, config.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to get NAR Callback server TLS config: %w", err)
	}

	port, err := ctlrutils.ExtractPortFromAddress(config.Listener.Address)
	if err != nil {
		return fmt.Errorf("failed to extract port from address: %w", err)
	}

	server := &http.Server{
		Handler:      handler,
		Addr:         fmt.Sprintf(":%d", port),
		TLSConfig:    serverTLSConfig,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog:     slog.NewLogLogger(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{AddSource: true}), slog.LevelError),
	}

	s.Logger.Info("Starting NodeAllocationRequest Callback Server", "Address", server.Addr)

	// Start server
	serverErrors := make(chan error, 1)
	go func() {
		s.Logger.Info(fmt.Sprintf("Listening on %s", server.Addr))
		// Cert/Key files aren't needed here since they've been added to the tls.Config above.
		if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Handle graceful shutdown when context is cancelled
	go func() {
		<-ctx.Done()
		s.Logger.Info("Context cancelled, initiating graceful shutdown of NAR Callback server")

		// Perform graceful shutdown
		if err := s.performGracefulShutdown(server); err != nil {
			s.Logger.Error("Error during graceful shutdown", "error", err)
		}
	}()

	defer func() {
		// Ensure cleanup occurs even if shutdown didn't happen via context cancellation
		if ctx.Err() == nil {
			cancel()
			s.Logger.Info("Performing final cleanup of NAR Callback server")
			if err := s.performGracefulShutdown(server); err != nil {
				s.Logger.Error("Error during final cleanup", "error", err)
			}
		}
	}()

	// Blocking select
	select {
	case err := <-serverErrors:
		s.Logger.Error("NAR Callback server encountered an error", "error", err)
		return fmt.Errorf("error starting NAR Callback server: %w", err)
	case <-ctx.Done():
		s.Logger.Info("NAR Callback server shutdown initiated")
	}

	return nil
}

// performGracefulShutdown handles the graceful shutdown of the server with proper logging
func (s *NodeAllocationRequestCallbackServer) performGracefulShutdown(server *http.Server) error {
	s.Logger.Info("Shutting down NAR Callback server gracefully")

	// Use the common graceful shutdown utility with timeout
	if err := common.GracefulShutdown(server); err != nil {
		s.Logger.Error("Failed to shutdown NAR Callback server gracefully", "error", err)
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	s.Logger.Info("NAR Callback server shutdown completed successfully")
	return nil
}

// validateCallbackPayload validates the incoming callback payload
func (n *NodeAllocationRequestCallbackServer) validateCallbackPayload(
	provisioningRequestName string,
	payload ProvisioningRequestCallbackJSONRequestBody) error {

	// Validate provisioning request name
	if strings.TrimSpace(provisioningRequestName) == "" {
		return fmt.Errorf("provisioningRequestName cannot be empty")
	}
	if len(provisioningRequestName) > maxProvisioningRequestNameLength {
		return fmt.Errorf("provisioningRequestName exceeds maximum length of %d characters", maxProvisioningRequestNameLength)
	}

	// Validate NodeAllocationRequestId (required field)
	if strings.TrimSpace(payload.NodeAllocationRequestId) == "" {
		return fmt.Errorf("nodeAllocationRequestId cannot be empty")
	}
	if len(payload.NodeAllocationRequestId) > maxNodeAllocationRequestIdLength {
		return fmt.Errorf("nodeAllocationRequestId exceeds maximum length of %d characters", maxNodeAllocationRequestIdLength)
	}

	// Validate Status (required field with specific enum values)
	switch payload.Status {
	case Pending, InProgress, Completed, Failed, TimedOut, Unprovisioned, NotInitialized, ConfigurationUpdateRequested, ConfigurationApplied, InvalidInput:
		// Valid status
	default:
		return fmt.Errorf("invalid status value: %s, must be one of: pending, inProgress, completed, failed, timedOut, unprovisioned, notInitialized, configurationUpdateRequested, configurationApplied, invalidInput", payload.Status)
	}

	// Validate Timestamp (required field)
	if payload.Timestamp.IsZero() {
		return fmt.Errorf("timestamp cannot be zero")
	}

	// Validate Error field (optional, but if provided should not be too long)
	if payload.Error != nil {
		if len(*payload.Error) > maxErrorMessageLength {
			return fmt.Errorf("error message exceeds maximum length of %d characters", maxErrorMessageLength)
		}
	}

	// Additional business logic validations
	// Error field should only be present when status indicates a failure or error condition
	errorStatuses := []CallbackPayloadStatus{Failed, TimedOut, InvalidInput}
	isErrorStatus := false
	for _, errorStatus := range errorStatuses {
		if payload.Status == errorStatus {
			isErrorStatus = true
			break
		}
	}

	if !isErrorStatus && payload.Error != nil && strings.TrimSpace(*payload.Error) != "" {
		return fmt.Errorf("error message should only be provided when status indicates a failure (failed, timedOut, invalidInput)")
	}

	// Error field should be present when status indicates a failure condition
	if isErrorStatus && (payload.Error == nil || strings.TrimSpace(*payload.Error) == "") {
		return fmt.Errorf("error message is required when status indicates a failure (%s)", payload.Status)
	}

	return nil
}

// GetAllVersions handles an API request to fetch all versions
func (n *NodeAllocationRequestCallbackServer) GetAllVersions(_ context.Context, _ GetAllVersionsRequestObject,
) (GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return GetAllVersions200JSONResponse(APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (n *NodeAllocationRequestCallbackServer) GetMinorVersions(_ context.Context, _ GetMinorVersionsRequestObject,
) (GetMinorVersionsResponseObject, error) {
	// We currently support a single version
	versions := []APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return GetMinorVersions200JSONResponse(APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// ProvisioningRequestCallback handles an API request to receive a callback
func (n *NodeAllocationRequestCallbackServer) ProvisioningRequestCallback(ctx context.Context, request ProvisioningRequestCallbackRequestObject) (ProvisioningRequestCallbackResponseObject, error) {
	n.Logger.Info("Received callback request",
		"provisioningRequestName", request.ProvisioningRequestName,
		"nodeAllocationRequestId", request.Body.NodeAllocationRequestId,
		"status", request.Body.Status)

	// Validate the callback payload
	if err := n.validateCallbackPayload(request.ProvisioningRequestName, *request.Body); err != nil {
		n.Logger.WarnContext(ctx, "Callback payload validation failed",
			"provisioningRequestName", request.ProvisioningRequestName,
			"nodeAllocationRequestId", request.Body.NodeAllocationRequestId,
			"error", err.Error())
		detail := err.Error()
		return ProvisioningRequestCallback400ApplicationProblemPlusJSONResponse{
			Detail: detail,
			Status: 400,
			Title:  stringPtr("Invalid callback payload"),
		}, nil
	}

	// Get the ProvisioningRequest
	var pr provisioningv1alpha1.ProvisioningRequest
	if err := n.HubClient.Get(ctx, client.ObjectKey{Name: request.ProvisioningRequestName}, &pr); err != nil {
		if client.IgnoreNotFound(err) == nil {
			n.Logger.WarnContext(ctx, "ProvisioningRequest not found",
				"provisioningRequestName", request.ProvisioningRequestName,
				"nodeAllocationRequestId", request.Body.NodeAllocationRequestId)
			detail := fmt.Sprintf("ProvisioningRequest %s not found", request.ProvisioningRequestName)
			return ProvisioningRequestCallback404ApplicationProblemPlusJSONResponse{
				Detail: detail,
				Status: 404,
				Title:  stringPtr("ProvisioningRequest not found"),
			}, nil
		}
		n.Logger.ErrorContext(ctx, "Failed to get ProvisioningRequest",
			"provisioningRequestName", request.ProvisioningRequestName,
			"nodeAllocationRequestId", request.Body.NodeAllocationRequestId,
			"error", err.Error())
		detail := fmt.Sprintf("Failed to retrieve ProvisioningRequest: %s", err.Error())
		return ProvisioningRequestCallback500ApplicationProblemPlusJSONResponse{
			Detail: detail,
			Status: 500,
			Title:  stringPtr("Internal server error"),
		}, nil
	}

	// Dedupe on (status, NAR id) so InProgress is handled once per NAR and
	// same-status updates for the same NAR don't retrigger reconciliation.
	prevStatus := ""
	prevNar := ""
	if pr.Annotations != nil {
		prevStatus = pr.Annotations[ctlrutils.CallbackStatusAnnotation]
		prevNar = pr.Annotations[ctlrutils.CallbackNodeAllocationRequestIdAnnotation]
	}
	newStatus := string(request.Body.Status)
	newNar := request.Body.NodeAllocationRequestId

	if prevStatus == newStatus && prevNar == newNar {
		n.Logger.Debug("Duplicate callback (same status & NAR) — ignoring",
			"provisioningRequestName", request.ProvisioningRequestName,
			"nodeAllocationRequestId", newNar,
			"status", newStatus)
		// Return success but don't trigger reconciliation
		return ProvisioningRequestCallback200Response{}, nil
	}

	// Log when ProvisioningRequest receives a new NodeAllocationRequestID
	if newNar != prevNar && newNar != "" {
		n.Logger.Info("ProvisioningRequest received new NodeAllocationRequestID",
			"provisioningRequestName", request.ProvisioningRequestName,
			"nodeAllocationRequestId", newNar,
			"status", newStatus)
	}

	// Update annotations to trigger reconciliation only for a meaningful change
	if pr.Annotations == nil {
		pr.Annotations = make(map[string]string)
	}
	pr.Annotations[ctlrutils.CallbackReceivedAnnotation] = fmt.Sprintf("%d", time.Now().Unix())
	pr.Annotations[ctlrutils.CallbackStatusAnnotation] = newStatus
	pr.Annotations[ctlrutils.CallbackNodeAllocationRequestIdAnnotation] = newNar

	if err := n.HubClient.Update(ctx, &pr); err != nil {
		n.Logger.ErrorContext(ctx, "Failed to update ProvisioningRequest",
			"provisioningRequestName", request.ProvisioningRequestName,
			"nodeAllocationRequestId", newNar,
			"status", newStatus,
			"error", err.Error())
		detail := fmt.Sprintf("Failed to update ProvisioningRequest: %s", err.Error())
		return ProvisioningRequestCallback500ApplicationProblemPlusJSONResponse{
			Detail: detail,
			Status: 500,
			Title:  stringPtr("Internal server error"),
		}, nil
	}

	n.Logger.Info("Successfully processed meaningful callback",
		"provisioningRequestName", request.ProvisioningRequestName,
		"nodeAllocationRequestId", newNar,
		"status", newStatus,
		"previousStatus", prevStatus)

	return ProvisioningRequestCallback200Response{}, nil
}

// stringPtr is a helper function to create a pointer to a string
func stringPtr(s string) *string {
	return &s
}
