package cmd

import (
	"log/slog"
	"os"

	artifacts "github.com/openshift-kni/oran-o2ims/internal/service/artifacts"
	"github.com/spf13/cobra"
)

// artifactsServer represents start command for the artifacts server
var artifactsServer = &cobra.Command{
	Use:   "serve",
	Short: "Start artifacts server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := artifacts.Serve(); err != nil {
			slog.Error("failed to start artifacts server", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	artifactsRootCmd.AddCommand(artifactsServer)
}
