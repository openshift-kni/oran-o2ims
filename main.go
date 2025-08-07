/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

//go:debug http2server=0

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	hwpluginscmd "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/cmd"
	metal3plugincmd "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/cmd"
	alarmscmd "github.com/openshift-kni/oran-o2ims/internal/service/alarms/cmd"
	artifactscmd "github.com/openshift-kni/oran-o2ims/internal/service/artifacts/cmd"
	clustercmd "github.com/openshift-kni/oran-o2ims/internal/service/cluster/cmd"
	provisioningcmd "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/cmd"
	inventorycmd "github.com/openshift-kni/oran-o2ims/internal/service/resources/cmd"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/cmd"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
)

func main() {
	// Create a context:
	ctx := context.Background()

	// Create the toolbuilder:
	toolBuilder := internal.NewTool().
		AddArgs(os.Args...).
		SetIn(os.Stdin).
		SetOut(os.Stdout).
		SetErr(os.Stderr).
		AddCommand(cmd.Start).
		AddCommand(cmd.Version).
		AddCommand(hwpluginscmd.Start).
		AddCommand(metal3plugincmd.Start).
		AddCommand(alarmscmd.GetAlarmRootCmd).             // TODO: all server should have same root to share init info
		AddCommand(clustercmd.GetClusterRootCmd).          // TODO: all server should have same root to share init info
		AddCommand(inventorycmd.GetResourcesRootCmd).      // TODO: all server should have same root to share init info
		AddCommand(artifactscmd.GetArtifactsRootCmd).      // TODO: all server should have same root to share init info
		AddCommand(provisioningcmd.GetProvisioningRootCmd) // TODO all server should have same root to share init info

	// Build out the tool:
	tool, err := toolBuilder.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}

	// Run the tool:
	err = tool.Run(ctx)
	if err != nil {
		var exitError exit.Error
		ok := errors.As(err, &exitError)
		if ok {
			os.Exit(exitError.Code())
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", err.Error())
			os.Exit(1)
		}
	}
}
