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

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jhernand/o2ims/internal"
	"github.com/jhernand/o2ims/internal/service"
)

// Server creates and returns the `start server` command.
func Start() *cobra.Command {
	c := NewServerCommand()
	result := &cobra.Command{
		Use:   "server",
		Short: "Starts the server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	return result
}

// ServerCommand contains the data and logic needed to run the `start server` command.
type ServerCommand struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
}

// NewServerCommand creates a new runner that knows how to execute the `start server` command.
func NewServerCommand() *ServerCommand {
	return &ServerCommand{}
}

// run executes the `start server` command.
func (c *ServerCommand) run(cmd *cobra.Command, argv []string) (err error) {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)

	// Save the flags:
	c.flags = cmd.Flags()

	// Create the server:
	server, err := service.NewServer().
		SetLogger(c.logger).
		SetHandler(&echoHandler{}).
		Build()
	if err != nil {
		c.logger.Error(
			"Failed to create server",
			"error", err,
		)
		os.Exit(1)
	}

	// Start the server:
	err = http.ListenAndServe(":8080", server)
	if err != nil {
		c.logger.Error(
			"server finished with error",
			"error", err,
		)
	}
	return nil
}

type echoHandler struct {
}

func (h *echoHandler) Serve(w http.ResponseWriter, r *service.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "filter=%s", r.Filter)
}
