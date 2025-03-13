/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"log/slog"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms"
	"github.com/spf13/cobra"
)

// alarmsMigrate represents the migrate command
var alarmsMigrate = &cobra.Command{
	Use:   "migrate",
	Short: "Run migrations all the way up",
	Long:  `This is will run from k8s job before the server starts.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := alarms.StartAlarmsMigration(); err != nil {
			slog.Error("failed to do migration", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	AlarmRootCmd.AddCommand(alarmsMigrate)
}
