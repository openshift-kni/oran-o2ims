package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	utils2 "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
)

var config api.ProvisioningServerConfig

// provisioningServe represents start provisioning command
var provisioningServe = &cobra.Command{
	Use:   "serve",
	Short: "Start provisioning server",
	Run: func(cmd *cobra.Command, args []string) {
		if err := provisioning.Serve(&config); err != nil {
			slog.Error("failed to start provisioning server", "err", err)
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
	if err := setServerFlags(provisioningServe); err != nil {
		panic(err)
	}
	provisioningRootCmd.AddCommand(provisioningServe)
}
