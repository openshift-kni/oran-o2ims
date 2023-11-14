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
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/searchapi"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

const (
	SearchApiUrlPrefix = "search-api-open-cluster-management"
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

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		c.logger.Error(
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the backend details:
	backendURL, err := flags.GetString(backendURLFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get backend URL flag",
			"flag", backendURLFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendURL == "" {
		c.logger.Error(
			"Backend URL is empty",
			"flag", backendURLFlagName,
		)
		return exit.Error(1)
	}
	backendToken, err := flags.GetString(backendTokenFlagName)
	if err != nil {
		c.logger.Error(
			"Failed to get backend token flag",
			"flag", backendTokenFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if backendToken == "" {
		c.logger.Error(
			"Backend token is empty",
			"flag", backendTokenFlagName,
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"Backend details",
		"url", backendURL,
		"!token", backendToken,
	)

	// Create the transport wrapper:
	transportWrapper, err := logging.NewTransportWrapper().
		SetLogger(c.logger).
		SetFlags(flags).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
	}

	// Create the router:
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusNotFound, "Not found")
	})
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	})

	// Generate the search API URL according the backend URL
	backendURL, err = c.generateSearchApiUrl(backendURL)
	if err != nil {
		c.logger.Error(
			"Failed to generate search API URL",
			"error", err.Error(),
		)
	}

	// Create the handlers for resource pools:
	if err := c.createResourcePoolsHandlers(
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
		c.logger.Error(
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	c.logger.Info(
		"API listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := http.Server{
		Addr:    apiListener.Addr().String(),
		Handler: router,
	}
	err = apiServer.Serve(apiListener)
	if err != nil {
		c.logger.Error(
			"API server finished with error",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	return nil
}

func (c *ResourceServerCommand) createResourcePoolsHandlers(
	transportWrapper func(http.RoundTripper) http.RoundTripper,
	router *mux.Router,
	cloudID, backendURL, backendToken string) error {

	// Create the collection handler for resource pools:
	collectionHandler, err := service.NewResourcePoolCollectionHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetGraphqlQuery(c.getGraphqlQuery()).
		SetGraphqlVars(c.getGraphqlVars()).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}
	collectionAdapter, err := service.NewCollectionAdapter().
		SetLogger(c.logger).
		SetHandler(collectionHandler).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/O2ims_infrastructureInventory/{version}/resourcePools",
		collectionAdapter,
	).Methods(http.MethodGet)

	// Create the object handler:
	objectHandler, err := service.NewResourcePoolObjectHandler().
		SetLogger(c.logger).
		SetTransportWrapper(transportWrapper).
		SetCloudID(cloudID).
		SetBackendURL(backendURL).
		SetBackendToken(backendToken).
		SetGraphqlQuery(c.getGraphqlQuery()).
		SetGraphqlVars(c.getGraphqlVars()).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}
	objectAdapter, err := service.NewObjectAdapter().
		SetLogger(c.logger).
		SetHandler(objectHandler).
		SetIDVariable("resourcePoolID").
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/O2ims_infrastructureInventory/{version}/resourcePools/{resourcePoolID}",
		objectAdapter,
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

	// Replace with search API prefix
	hostArr[0] = SearchApiUrlPrefix

	// Generate search API URL
	searchUri := strings.Join(hostArr, ".")
	return fmt.Sprintf("%s://%s/searchapi/graphql", u.Scheme, searchUri), nil
}

func (c *ResourceServerCommand) getGraphqlQuery() string {
	return `query ($input: [SearchInput]) {
				searchResult: search(input: $input) {
						items,
						related {
							items
						},       
					}
			}`
}

func (c *ResourceServerCommand) getGraphqlVars() *searchapi.SearchInput {
	input := searchapi.SearchInput{}
	itemKind := "Cluster"
	relatedItemKind := "Node"
	input.Filters = []*searchapi.SearchFilter{
		{
			Property: "kind",
			Values:   []*string{&itemKind},
		},
	}
	input.RelatedKinds = []*string{&relatedItemKind}
	return &input
}
