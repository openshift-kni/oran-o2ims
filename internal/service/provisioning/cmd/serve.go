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

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
)

var config api.ProvisioningServerConfig

// provisioningServe represents start provisioning command
var provisioningServe = &cobra.Command{
	Use:   "serve",
	Short: "Start provisioning server",
	Run: func(cmd *cobra.Command, args []string) {
		// Configure structured logging for this service with context support
		baseHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
		logger := slog.New(logging.NewContextHandler(baseHandler, slog.LevelInfo))
		slog.SetDefault(logger)
		klog.SetSlogLogger(logger)

		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", slog.Any("err", err))
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", slog.Any("err", err))
			os.Exit(1)
		}
		if err := provisioning.Serve(&config); err != nil {
			slog.Error("failed to start provisioning server", slog.Any("err", err))
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
	if err := setServerFlags(provisioningServe); err != nil {
		panic(err)
	}
	provisioningRootCmd.AddCommand(provisioningServe)
}
