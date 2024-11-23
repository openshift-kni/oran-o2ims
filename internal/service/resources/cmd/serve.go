package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/resources"
	"github.com/spf13/cobra"
)

// resourceServer represents start command for the resource server
var resourceServer = &cobra.Command{
	Use:   "serve",
	Short: "Start resource server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := resources.Serve(); err != nil {
			slog.Error("failed to start resource server", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	resourcesRootCmd.AddCommand(resourceServer)
}
