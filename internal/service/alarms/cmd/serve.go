package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"

	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/server"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
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
func setServerFlags(cmd *cobra.Command) error {
	flags := cmd.Flags()
	if err := utils2.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
		return fmt.Errorf("could not set common server flags: %w", err)
	}
	flags.StringVar(
		&config.GlobalCloudID,
		server.GlobalCloudIDFlagName,
		utils.DefaultOCloudID,
		"The global O-Cloud identifier.",
	)
	return nil
}

func init() {
	if err := setServerFlags(alarmsServe); err != nil {
		panic(err)
	}
	AlarmRootCmd.AddCommand(alarmsServe)
}
