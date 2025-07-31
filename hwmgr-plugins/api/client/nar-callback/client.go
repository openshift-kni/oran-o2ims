/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package narcallback

import (
	"context"
	"fmt"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/api/common"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// NarCallbackClient provides functions for calling the NarCallback APIs
type NarCallbackClient struct {
	Client *ClientWithResponses
	logger *slog.Logger
}

// NewHardwarePluginClient creates an authenticated client connected to the HardwarePlugin server.
func NewNarCallbackClient(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	callback *pluginsv1alpha1.Callback,
) (*NarCallbackClient, error) {
	// Construct OAuth client configuration
	config, err := utils.SetupOAuthClientConfig(
		ctx, c,
		callback.CaBundleName,
		callback.AuthClientConfig,
		constants.DefaultNamespace,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup OAuth client config: %w", err)
	}

	// Build OAuth client information if type is not ServiceAccount
	cf := notifier.NewClientFactory(config, constants.DefaultBackendTokenFile)

	// Default to ServiceAccount if no auth config is provided
	authType := common.ServiceAccount
	if callback.AuthClientConfig != nil {
		authType = callback.AuthClientConfig.Type
	}

	httpClient, err := cf.NewClient(ctx, authType)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	hClient, err := NewClientWithResponses(callback.CallbackURL, WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create client with responses: %w", err)
	}

	return &NarCallbackClient{
		Client: hClient,
		logger: logger,
	}, nil
}
