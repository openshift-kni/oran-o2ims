/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/authentication"
	"github.com/openshift-kni/oran-o2ims/internal/authorization"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start deployment-manager-server` command.
func DeploymentManagerServer() *cobra.Command {
	c := NewDeploymentManagerServer()
	result := &cobra.Command{
		Use:   "deployment-manager-server",
		Short: "Starts the deployment manager server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	authentication.AddFlags(flags)
	authorization.AddFlags(flags)
	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	_ = flags.String(
		cloudIDFlagName,
		"",
		"O-Cloud identifier.",
	)
	_ = flags.String(
		backendURLFlagName,
		"",
		"URL of the backend server.",
	)
	_ = flags.String(
		backendTypeFlagName,
		string(service.DeploymentManagerBackendTypeRegularHub),
		fmt.Sprintf(
			"Type of backend. Possible values are '%s' and '%s'.",
			service.DeploymentManagerBackendTypeGlobalHub,
			service.DeploymentManagerBackendTypeRegularHub,
		),
	)
	_ = flags.String(
		backendTokenFlagName,
		"",
		"Token for authenticating to the backend server.",
	)
	_ = flags.String(
		backendTokenFileFlagName,
		"",
		"File containing the token for authenticating to the backend server.",
	)
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to deployment managers.",
	)
	return result
}

// DeploymentManagerServerCommand contains the data and logic needed to run the `start
// deployment-manager-server` command.
type DeploymentManagerServerCommand struct {
}

// NewDeploymentManagerServer creates a new runner that knows how to execute the `start
// deployment-manager-server` command.
func NewDeploymentManagerServer() *DeploymentManagerServerCommand {
	return &DeploymentManagerServerCommand{}
}

// run executes the `start deployment-manager-server` command.
func (c *DeploymentManagerServerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	logger := internal.LoggerFromContext(ctx)

	// Get the flags:
	flags := cmd.Flags()

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		logger.Error(
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		logger.Error(
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the backend details:
	backendTypeText, err := flags.GetString(backendTypeFlagName)
	if err != nil {
		logger.Error(
			"Failed to get backend type flag",
			"flag", backendTypeFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	backendType := service.DeploymentManagerBackendType(backendTypeText)
	switch backendType {
	case service.DeploymentManagerBackendTypeGlobalHub:
	case service.DeploymentManagerBackendTypeRegularHub:
	default:
		logger.Error(
			"Unknown backend type",
			slog.String("type", backendTypeText),
		)
		return exit.Error(1)
	}
	backendURL, err := flags.GetString(backendURLFlagName)
	if err != nil {
		logger.Error(
			"Failed to get backend URL flag",
			"flag", backendURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendURL == "" {
		logger.Error(
			"Backend URL is empty",
			"flag", backendURLFlagName,
		)
		return exit.Error(1)
	}
	backendToken, err := flags.GetString(backendTokenFlagName)
	if err != nil {
		logger.Error(
			"Failed to get backend token flag",
			"flag", backendTokenFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendToken == "" {
		logger.Error(
			"Backend token is empty",
			"flag", backendTokenFlagName,
		)
		return exit.Error(1)
	}
	backendTokenFile, err := flags.GetString(backendTokenFileFlagName)
	if err != nil {
		logger.Error(
			"Failed to get backend token file flag",
			slog.String("flag", backendTokenFileFlagName),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	extensions, err := flags.GetStringArray(extensionsFlagName)
	if err != nil {
		logger.Error(
			"Failed to extension flag",
			"flag", extensionsFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	// Check that the backend token and token file haven't been simultaneously provided:
	if backendToken != "" && backendTokenFile != "" {
		logger.Error(
			"Backend token and token file have both been provided, but they are incompatible",
			slog.Any(
				"flags",
				[]string{
					backendTokenFlagName,
					backendTokenFileFlagName,
				},
			),
			slog.String("!token", backendToken),
			slog.String("token_file", backendTokenFile),
		)
		return exit.Error(1)
	}

	// Read the backend token file if needed:
	if backendToken == "" && backendTokenFile != "" {
		backendTokenData, err := os.ReadFile(backendTokenFile)
		if err != nil {
			logger.Error(
				"Failed to read backend token file",
				slog.String("file", backendTokenFile),
				slog.String("error", err.Error()),
			)
			return exit.Error(1)
		}
		backendToken = strings.TrimSpace(string(backendTokenData))
		logger.Info(
			"Loaded backend token from file",
			slog.String("file", backendTokenFile),
			slog.String("!token", backendToken),
		)
	}

	// Check that we have a token:
	if backendToken == "" {
		logger.Error("Backend token or token file must be provided")
		return exit.Error(1)
	}

	// Write the backend details to the log:
	logger.Info(
		"Backend details",
		slog.String("type", string(backendType)),
		slog.String("url", backendURL),
		slog.String("!token", backendToken),
		slog.String("token_file", backendTokenFile),
		slog.Any("extensions", extensions),
	)

	// Create the logging wrapper:
	loggingWrapper, err := logging.NewTransportWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	// Create the authentication and authorization wrappers:
	authenticationWrapper, err := authentication.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create authentication wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	authorizationWrapper, err := authorization.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create authorization wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Create the router:
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusNotFound, "Not found")
	})
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	})
	router.Use(authenticationWrapper, authorizationWrapper)

	// Create the handler:
	handler, err := service.NewDeploymentManagerHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetCloudID(cloudID).
		SetExtensions(extensions...).
		SetBackendType(backendType).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetEnableHack(true).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(logger).
		SetPathVariables("deploymentManagerID").
		SetHandler(handler).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/deploymentManagers",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/deploymentManagers/{deploymentManagerID}",
		adapter,
	).Methods(http.MethodGet)

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		logger.Error(
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	logger.Info(
		"API listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := http.Server{
		Addr:    apiListener.Addr().String(),
		Handler: router,
	}
	err = apiServer.Serve(apiListener)
	if err != nil {
		logger.Error(
			"API server finished with error",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	return nil
}

// Names of command line flags:
const (
	backendTypeFlagName      = "backend-type"
	backendTokenFlagName     = "backend-token"
	backendTokenFileFlagName = "backend-token-file"
	backendURLFlagName       = "backend-url"
)
