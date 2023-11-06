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
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/authentication"
	"github.com/openshift-kni/oran-o2ims/internal/authorization"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
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
		backendTokenFlagName,
		"",
		"Token for authenticating to the backend server.",
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
	logger.Info(
		"Backend details",
		"url", backendURL,
		"!token", backendToken,
	)

	// Create the transport wrapper:
	transportWrapper, err := logging.NewTransportWrapper().
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

	// Create the collection handler:
	collectionHandler, err := service.NewDeploymentManagerCollectionHandler().
		SetLogger(logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}
	collectionAdapter, err := service.NewCollectionAdapter().
		SetLogger(logger).
		SetHandler(collectionHandler).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/O2ims_infrastructureInventory/{version}/deploymentManagers",
		collectionAdapter,
	).Methods(http.MethodGet)

	// Create the object handler:
	objectHandler, err := service.NewDeploymentManagerObjectHandler().
		SetLogger(logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}
	objectAdapter, err := service.NewObjectAdapter().
		SetLogger(logger).
		SetHandler(objectHandler).
		SetID("deploymentManagerID").
		Build()
	if err != nil {
		logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/O2ims_infrastructureInventory/{version}/deploymentManagers/{deploymentManagerID}",
		objectAdapter,
	).Methods(http.MethodGet)

	// Start the server:
	err = http.ListenAndServe(":8080", router)
	if err != nil {
		logger.Error(
			"server finished with error",
			"error", err,
		)
		return exit.Error(1)
	}

	return nil
}

// Names of command line flags:
const (
	backendTokenFlagName = "backend-token"
	backendURLFlagName   = "backend-url"
	cloudIDFlagName      = "cloud-id"
)
