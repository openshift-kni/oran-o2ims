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
	"net/http"

	"github.com/spf13/cobra"

	"github.com/jhernand/o2ims/internal"
	"github.com/jhernand/o2ims/internal/exit"
	"github.com/jhernand/o2ims/internal/service"
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
	_ = flags.String(
		"cloud-id",
		"",
		"O-Cloud identifier.",
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
		logger.Error("Cloud identifier is empty")
		return exit.Error(1)
	}
	logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Create the handlers and adapters:
	handler, err := service.NewDeploymentManagerCollectionHandler().
		SetLogger(logger).
		SetCloudID(cloudID).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}
	adapter, err := service.NewCollectionAdapter().
		SetLogger(logger).
		SetHandler(handler).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}

	// Start the server:
	err = http.ListenAndServe(":8080", adapter)
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
	cloudIDFlagName = "cloud-id"
)
