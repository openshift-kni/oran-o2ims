/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/cmd/operator"
)

// Create creates and returns the `start` command.
func Start() *cobra.Command {
	result := &cobra.Command{
		Use:   "start",
		Short: "Starts components",
		Args:  cobra.NoArgs,
	}
	result.AddCommand(operator.ControllerManager())
	return result
}
