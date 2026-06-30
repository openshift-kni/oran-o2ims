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
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/spf13/cobra"
)

// artifactsRootCmd represents the root command for working artifacts server
var artifactsRootCmd = &cobra.Command{
	Use:   constants.ArtifactsServerCmd,
	Short: "All things needed for the artifacts server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureArtifactsLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetArtifactsRootCmd() *cobra.Command {
	return artifactsRootCmd
}

func configureArtifactsLogger() {
	baseHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	l := slog.New(logging.NewContextHandler(baseHandler, slog.LevelDebug))
	slog.SetDefault(l)
	slog.Info("Artifacts server global logger configured")
}
