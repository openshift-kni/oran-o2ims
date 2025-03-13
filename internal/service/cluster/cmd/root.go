/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// clusterRootCmd represents the root command for working resource server
var clusterRootCmd = &cobra.Command{
	Use:   "cluster-server",
	Short: "All things needed for the cluster server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureDefaultLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetClusterRootCmd() *cobra.Command {
	return clusterRootCmd
}

func configureDefaultLogger() {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	slog.SetDefault(l)
	slog.Info("Cluster server global logger configured")
}
