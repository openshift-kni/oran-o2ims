package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api"
)

// config defines the configuration attributes for the cluster server
var config api.ClusterServerConfig

// clusterServer represents start command for the cluster server
var clusterServer = &cobra.Command{
	Use:   "serve",
	Short: "Start cluster server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cluster.Serve(&config); err != nil {
			slog.Error("failed to start cluster server", "err", err)
			os.Exit(1)
		}
	},
}

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(
		&config.Address,
		server.APIListenerAddressFlagName,
		"127.0.0.1:8000",
		"API listener address.",
	)
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
}

func init() {
	setServerFlags(clusterServer)
	clusterRootCmd.AddCommand(clusterServer)
}
