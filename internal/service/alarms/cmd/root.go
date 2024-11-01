package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// AlarmRootCmd represents the root command for working alarms server
var AlarmRootCmd = &cobra.Command{
	Use:   "alarms-server",
	Short: "All things needed for alarms server",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nothing to do. Use sub-commands instead.")
	},
}

func GetAlarmRootCmd() *cobra.Command {
	return AlarmRootCmd
}

func init() {
	configureAlarmLogger()
}

func configureAlarmLogger() {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	slog.SetDefault(l)
	slog.Info("Alarm server global logger configured")
}
