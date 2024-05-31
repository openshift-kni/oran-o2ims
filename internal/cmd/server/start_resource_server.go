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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/metrics"
	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start resource-server` command.
func ResourceServer() *cobra.Command {
	c := NewResourceServer()
	result := &cobra.Command{
		Use:   "resource-server",
		Short: "Starts the resource server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	AddTokenFlags(flags)
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
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to resources and resource pools.",
	)
	return result
}

// ResourceServerCommand contains the data and logic needed to run the `start
// resource-server` command.
type ResourceServerCommand struct {
	logger *slog.Logger
}

// NewResourceServer creates a new runner that knows how to execute the `start
// resource-server` command.
func NewResourceServer() *ResourceServerCommand {
	return &ResourceServerCommand{}
}

// run executes the `start resource-server` command.
func (c *ResourceServerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)

	// Get the flags:
	flags := cmd.Flags()

	// Create the exit handler:
	exitHandler, err := exit.NewHandler().
		SetLogger(c.logger).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create exit handler",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		c.logger.ErrorContext(
			ctx,
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	c.logger.InfoContext(
		ctx,
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the backend details:
	backendURL, err := flags.GetString(backendURLFlagName)
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to get backend URL flag",
			"flag", backendURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendURL == "" {
		c.logger.ErrorContext(
			ctx,
			"Backend URL is empty",
			"flag", backendURLFlagName,
		)
		return exit.Error(1)
	}

	extensions, err := flags.GetStringArray(extensionsFlagName)
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to extension flag",
			"flag", extensionsFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	backendToken, err := GetTokenFlag(ctx, flags, c.logger)
	if err != nil {
		return exit.Error(1)
	}

	c.logger.InfoContext(
		ctx,
		"Backend details",
		slog.String("url", backendURL),
		slog.String("!token", backendToken),
		slog.Any("extensions", extensions),
	)

	// Create the transport wrapper:
	transportWrapper, err := logging.NewTransportWrapper().
		SetLogger(c.logger).
		SetFlags(flags).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
	}

	// Create the metrics wrapper:
	metricsWrapper, err := metrics.NewHandlerWrapper().
		AddPaths(
			"/o2ims-infrastructureInventory/-/resourceTypes/-",
			"/o2ims-infrastructureInventory/-/resourcePools/-/resources/-",
		).
		SetSubsystem("inbound").
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create metrics wrapper",
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
	router.Use(metricsWrapper)

	// Generate the search API URL according the backend URL
	backendURL, err = c.generateSearchApiUrl(backendURL)
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to generate search API URL",
			"error", err.Error(),
		)
	}

	// Create the handler for resource pools:
	if err := c.createResourcePoolHandler(
		ctx,
		transportWrapper, router,
		cloudID, backendURL, backendToken, extensions); err != nil {
		return err
	}

	// Create the handler for resources:
	if err := c.createResourceHandler(
		ctx,
		transportWrapper, router,
		cloudID, backendURL, backendToken, extensions); err != nil {
		return err
	}

	// Create the handlers for resource types:
	if err := c.createResourceTypeHandler(
		ctx,
		transportWrapper, router,
		cloudID, backendURL, backendToken); err != nil {
		return err
	}

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	c.logger.InfoContext(
		ctx,
		"API server listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := &http.Server{
		Addr:    apiListener.Addr().String(),
		Handler: router,
	}
	exitHandler.AddServer(apiServer)
	go func() {
		err = apiServer.Serve(apiListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.ErrorContext(
				ctx,
				"API server finished with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Start the metrics server:
	metricsListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(flags, network.MetricsListener).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create metrics listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	c.logger.InfoContext(
		ctx,
		"Metrics server listening",
		slog.String("address", metricsListener.Addr().String()),
	)
	metricsHandler := promhttp.Handler()
	metricsServer := &http.Server{
		Addr:    metricsListener.Addr().String(),
		Handler: metricsHandler,
	}
	exitHandler.AddServer(metricsServer)
	go func() {
		err = metricsServer.Serve(metricsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.ErrorContext(
				ctx,
				"Metrics server finished with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Wait for exit signals:
	return exitHandler.Wait(ctx)
}

func (c *ResourceServerCommand) createResourcePoolHandler(
	ctx context.Context,
	transportWrapper func(http.RoundTripper) http.RoundTripper,
	router *mux.Router,
	cloudID, backendURL, backendToken string, extensions []string) error {

	// Create the handler:
	handler, err := service.NewResourcePoolHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetExtensions(extensions...).
		SetGraphqlQuery(c.getGraphqlQuery()).
		SetGraphqlVars(c.getClusterGraphqlVars()).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(c.logger).
		SetPathVariables("resourcePoolID").
		SetHandler(handler).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourcePools",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourcePools/{resourcePoolID}",
		adapter,
	).Methods(http.MethodGet)

	return nil
}

func (c *ResourceServerCommand) createResourceHandler(
	ctx context.Context,
	transportWrapper func(http.RoundTripper) http.RoundTripper,
	router *mux.Router,
	cloudID, backendURL, backendToken string, extensions []string) error {

	// Create the handler:
	handler, err := service.NewResourceHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetExtensions(extensions...).
		SetGraphqlQuery(c.getGraphqlQuery()).
		SetGraphqlVars(c.getResourceGraphqlVars()).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(c.logger).
		SetPathVariables("resourcePoolID", "resourceID").
		SetHandler(handler).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourcePools/{resourcePoolID}/resources",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourcePools/{resourcePoolID}/resources/{resourceID}",
		adapter,
	).Methods(http.MethodGet)

	return nil
}

func (c *ResourceServerCommand) createResourceTypeHandler(
	ctx context.Context,
	transportWrapper func(http.RoundTripper) http.RoundTripper,
	router *mux.Router,
	cloudID, backendURL, backendToken string) error {

	// Create the handler:
	handler, err := service.NewResourceTypeHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetGraphqlQuery(c.getGraphqlQuery()).
		SetGraphqlVars(c.getResourceGraphqlVars()).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	// Create the collection adapter:
	adapter, err := service.NewAdapter().
		SetLogger(c.logger).
		SetPathVariables("resourceTypeID").
		SetHandler(handler).
		Build()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourceTypes",
		adapter,
	).Methods(http.MethodGet)
	router.Handle(
		"/o2ims-infrastructureInventory/{version}/resourceTypes/{resourceTypeID}",
		adapter,
	).Methods(http.MethodGet)

	return nil
}

func (c *ResourceServerCommand) generateSearchApiUrl(backendURL string) (string, error) {
	u, err := url.Parse(backendURL)
	if err != nil {
		return "", err
	}

	// Split URL address
	hostArr := strings.Split(u.Host, ".")

	// Generate search API URL
	searchUri := strings.Join(hostArr, ".")
	return fmt.Sprintf("%s://%s/searchapi/graphql", u.Scheme, searchUri), nil
}

func (c *ResourceServerCommand) getGraphqlQuery() string {
	return `query ($input: [SearchInput]) {
				searchResult: search(input: $input) {
						items,    
					}
			}`
}

func (c *ResourceServerCommand) getClusterGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	itemKind := "Cluster"
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values:   []*string{&itemKind},
		},
	}
	return &input
}

func (c *ResourceServerCommand) getResourceGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	kindNode := service.KindNode
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values: []*string{
				&kindNode,
				// Add more kinds here if required
			},
		},
	}
	return &input
}
