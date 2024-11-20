package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// resourcesRootCmd represents the root command for working resource server
var resourcesRootCmd = &cobra.Command{
	Use:   "resource-server",
	Short: "All things needed for the resource server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureResourcesLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetResourcesRootCmd() *cobra.Command {
	return resourcesRootCmd
}

func configureResourcesLogger() {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	slog.SetDefault(l)
	slog.Info("Resource server global logger configured")
}
