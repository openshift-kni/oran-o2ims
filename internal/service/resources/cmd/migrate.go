package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/resources"

	"github.com/spf13/cobra"
)

// resourcesMigrate represents the migrate command
var resourcesMigrate = &cobra.Command{
	Use:   "migrate",
	Short: "Run migrations all the way up",
	Long:  `This will run from k8s job before the server starts.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := resources.StartResourcesMigration(); err != nil {
			slog.Error("failed to do migration", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	resourcesRootCmd.AddCommand(resourcesMigrate)
}
