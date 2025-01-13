package cmd

import (
	"fmt"
	"log/slog"
	"os"

	artifacts "github.com/openshift-kni/oran-o2ims/internal/service/artifacts"
	"github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api"
	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/spf13/cobra"
)

var config api.ArtifactsServerConfig

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

// setServerFlags creates the flag instances for the server
func setServerFlags(cmd *cobra.Command) error {
	if err := utils2.SetCommonServerFlags(cmd, &config.CommonServerConfig); err != nil {
		return fmt.Errorf("could not set common server flags: %w", err)
	}

	return nil
}

func init() {
	if err := setServerFlags(artifactsServer); err != nil {
		panic(err)
	}
	artifactsRootCmd.AddCommand(artifactsServer)
}
