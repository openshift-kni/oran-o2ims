package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// config defines the configuration attributes for the cluster server
var config api.ClusterServerConfig

// clusterServer represents start command for the cluster server
var clusterServer = &cobra.Command{
	Use:   "serve",
	Short: "Start cluster server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.LoadFromEnv(); err != nil {
			slog.Error("failed to load environment variables", "err", err)
			os.Exit(1)
		}
		if err := config.Validate(); err != nil {
			slog.Error("failed to validate common server configuration", "err", err)
			os.Exit(1)
		}
		if err := cluster.Serve(&config); err != nil {
			slog.Error("failed to start cluster server", "err", err)
			os.Exit(1)
		}
	},
}

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) error {
	flags := cmd.Flags()
	if err := utils.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
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
	return nil
}

func init() {
	if err := setServerFlags(clusterServer); err != nil {
		panic(err)
	}
	clusterRootCmd.AddCommand(clusterServer)
}
