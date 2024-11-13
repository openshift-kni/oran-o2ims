package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms"

	"github.com/spf13/cobra"
)

// alarmsServe represents start alarms command
var alarmsServe = &cobra.Command{
	Use:   "serve",
	Short: "Start alarms server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := alarms.Serve(); err != nil {
			slog.Error("failed to start alarms server", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	AlarmRootCmd.AddCommand(alarmsServe)
}
