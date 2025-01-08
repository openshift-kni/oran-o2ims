package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// provisioningRootCmd represents the root command for working provisioning server
var provisioningRootCmd = &cobra.Command{
	Use:   "provisioning-server",
	Short: "All things needed for the provisioning server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureProvisioningLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetProvisioningRootCmd() *cobra.Command {
	return provisioningRootCmd
}

func configureProvisioningLogger() {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	slog.SetDefault(l)
	slog.Info("Provisioning server global logger configured")
}
