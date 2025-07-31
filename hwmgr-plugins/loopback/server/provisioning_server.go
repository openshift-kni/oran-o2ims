/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const LoopbackResourcePrefix = "loopback"

// LoopbackPluginServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ provisioning.StrictServerInterface = (*LoopbackPluginServer)(nil)

type LoopbackPluginServer struct {
	provisioning.HardwarePluginServer
}

// NewLoopbackPluginServer creates a Loopback HardwarePlugin server
func NewLoopbackPluginServer(
	config svcutils.CommonServerConfig,
	hubClient client.Client,
	logger *slog.Logger,
) (*LoopbackPluginServer, error) {
	return &LoopbackPluginServer{
		HardwarePluginServer: provisioning.HardwarePluginServer{
			CommonServerConfig: config,
			HubClient:          hubClient,
			Logger:             logger,
			Namespace:          provisioning.GetLoopbackHWPluginNamespace(),
			HardwarePluginID:   hwmgrutils.LoopbackHardwarePluginID,
			ResourcePrefix:     LoopbackResourcePrefix,
		},
	}, nil
}
