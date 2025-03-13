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

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api"
)

// config defines the configuration attributes for the resource server
var config api.ResourceServerConfig

// resourceServer represents start command for the resource server
var resourceServer = &cobra.Command{
	Use:   "serve",
	Short: "Start resource server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", "err", err)
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", "err", err)
			os.Exit(1)
		}
		if err := resources.Serve(&config); err != nil {
			slog.Error("failed to start resource server", "err", err)
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
		&config.CloudID,
		server.CloudIDFlagName,
		"",
		"The local O-Cloud identifier.",
	)
	flags.StringArrayVar(
		&config.Extensions,
		server.ExtensionsFlagName,
		[]string{},
		"Extensions to add to resources and resource pools.",
	)
	flags.StringVar(
		&config.ExternalAddress,
		server.ExternalAddressFlagName,
		"",
		"URL used to access the O-Cloud Manager from outside of the cluster.",
	)
	flags.StringVar(
		&config.GlobalCloudID,
		server.GlobalCloudIDFlagName,
		utils.DefaultOCloudID,
		"The global O-Cloud identifier.",
	)

	// The O-Cloud ID and External address arguments are mandatory while all other arguments are optional.  The Global
	// O-Cloud ID is special in that it is not strictly mandatory to start the server, but it is mandatory to enable
	// some API endpoints (e.g., subscriptions).
	requiredFlags := []string{server.CloudIDFlagName, server.ExternalAddressFlagName}
	for _, flag := range requiredFlags {
		err := cmd.MarkFlagRequired(flag)
		if err != nil {
			return fmt.Errorf("failed to mark required flag %s: %w", flag, err)
		}
	}
	return nil
}

func init() {
	if err := setServerFlags(resourceServer); err != nil {
		panic(err)
	}
	resourcesRootCmd.AddCommand(resourceServer)
}
