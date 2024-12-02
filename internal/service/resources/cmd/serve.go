package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
		if err := resources.Serve(&config); err != nil {
			slog.Error("failed to start resource server", "err", err)
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
	flags.StringVar(
		&config.BackendURL,
		server.BackendURLFlagName,
		"",
		"URL of the backend search-api component.",
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
			panic(fmt.Sprintf("failed to mark required flag: %s", flag))
		}
	}
}

func init() {
	setServerFlags(resourceServer)
	resourcesRootCmd.AddCommand(resourceServer)
}
