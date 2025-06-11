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

	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
)

var config api.ProvisioningServerConfig

// provisioningServe represents start provisioning command
var provisioningServe = &cobra.Command{
	Use:   "serve",
	Short: "Start provisioning server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", "err", err)
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", "err", err)
			os.Exit(1)
		}
		if err := provisioning.Serve(&config); err != nil {
			slog.Error("failed to start provisioning server", "err", err)
			os.Exit(1)
		}
	},
}

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) error {
	if err := utils2.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
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
