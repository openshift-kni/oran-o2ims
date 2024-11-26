package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms"
)

var config api.AlarmsServerConfig

// alarmsServe represents start alarms command
var alarmsServe = &cobra.Command{
	Use:   "serve",
	Short: "Start alarms server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := alarms.Serve(&config); err != nil {
			slog.Error("failed to start alarms server", "err", err)
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
		&config.GlobalCloudID,
		server.GlobalCloudIDFlagName,
		utils.DefaultOCloudID,
		"The global O-Cloud identifier.",
	)
}

func init() {
	setServerFlags(alarmsServe)
	AlarmRootCmd.AddCommand(alarmsServe)
}
