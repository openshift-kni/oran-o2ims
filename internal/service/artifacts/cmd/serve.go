/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	artifacts "github.com/openshift-kni/oran-o2ims/internal/service/artifacts"
	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

var config api.ArtifactsServerConfig

// artifactsServer represents start command for the artifacts server
var artifactsServer = &cobra.Command{
	Use:   "serve",
	Short: "Start artifacts server",
	Run: func(cmd *cobra.Command, args []string) {
		// Configure structured logging for this service
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)
		klog.SetSlogLogger(logger)

		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", "err", err)
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", "err", err)
			os.Exit(1)
		}
		if err := artifacts.Serve(&config); err != nil {
			slog.Error("failed to start artifacts server", "err", err)
			os.Exit(1)
		}
	},
}

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) error {
	if err := svcutils.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
		return fmt.Errorf("could not set common server flags: %w", err)
	}

	return nil
}

func init() {
	if err := setServerFlags(artifactsServer); err != nil {
		panic(err)
	}
	artifactsRootCmd.AddCommand(artifactsServer)
}
