/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

var config api.AlarmsServerConfig

// alarmsServe represents start alarms command
var alarmsServe = &cobra.Command{
	Use:   "serve",
	Short: "Start alarms server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", "err", err)
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", "err", err)
			os.Exit(1)
		}
		if err := alarms.Serve(&config); err != nil {
			slog.Error("failed to start alarms server", "err", err)
			os.Exit(1)
		}
	},
}

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) error {
	flags := cmd.Flags()
	if err := utils2.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
		return fmt.Errorf("could not set common server flags: %w", err)
	}
	flags.StringVar(
		&config.GlobalCloudID,
		server.GlobalCloudIDFlagName,
		constants.DefaultOCloudID,
		"The global O-Cloud identifier.",
	)
	return nil
}

func init() {
	if err := setServerFlags(alarmsServe); err != nil {
		panic(err)
	}
	AlarmRootCmd.AddCommand(alarmsServe)
}
