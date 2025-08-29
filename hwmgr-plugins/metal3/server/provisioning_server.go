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

const Metal3ResourcePrefix = "metal3"

// Metal3PluginServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ provisioning.StrictServerInterface = (*Metal3PluginServer)(nil)

type Metal3PluginServer struct {
	provisioning.HardwarePluginServer
}

// NewMetal3PluginServer creates a Metal3 HardwarePlugin server
func NewMetal3PluginServer(
	config svcutils.CommonServerConfig,
	hubClient client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
) (*Metal3PluginServer, error) {
	return &Metal3PluginServer{
		HardwarePluginServer: provisioning.HardwarePluginServer{
			CommonServerConfig: config,
			HubClient:          hubClient,
			NoncachedClient:    noncachedClient,
			Logger:             logger,
			Namespace:          provisioning.GetMetal3HWPluginNamespace(),
			HardwarePluginID:   hwmgrutils.Metal3HardwarePluginID,
			ResourcePrefix:     Metal3ResourcePrefix,
		},
	}, nil
}
