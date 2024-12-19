package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster"

	"github.com/spf13/cobra"
)

// migrate represents the migrate command
var migrate = &cobra.Command{
	Use:   "migrate",
	Short: "Run migrations all the way up",
	Long:  `This will run from k8s job before the server starts.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := cluster.StartMigration(); err != nil {
			slog.Error("failed to do migration", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	clusterRootCmd.AddCommand(migrate)
}
