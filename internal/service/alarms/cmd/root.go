/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/spf13/cobra"
)

// AlarmRootCmd represents the root command for working alarms server
var AlarmRootCmd = &cobra.Command{
	Use:   constants.AlarmsServerCmd,
	Short: "All things needed for alarms server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureAlarmLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetAlarmRootCmd() *cobra.Command {
	return AlarmRootCmd
}

func configureAlarmLogger() {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	slog.SetDefault(l)
	slog.Info("Alarm server global logger configured")
}
