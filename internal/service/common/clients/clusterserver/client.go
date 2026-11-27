/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

// Package clusterserver provides a client for the O2IMS cluster server API that any O2IMS service can use.
package clusterserver

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/clusterserver/generated"
)

// URLEnvName overrides the cluster server URL, for local development.
const URLEnvName = "CLUSTER_SERVER_URL"

// NewClient creates a cluster server client that authenticates as the service account callerSA.
// Options are passed to clients.NewBackend.
func NewClient(clientset kubernetes.Interface, callerSA string, opts ...clients.BackendOption) (*generated.ClientWithResponses, error) {
	backend, err := clients.NewBackend(clientset, ctlrutils.InventoryClusterServerName, URLEnvName, callerSA, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare cluster server connection: %w", err)
	}

	c, err := generated.NewClientWithResponses(backend.URL,
		generated.WithHTTPClient(backend.HTTPClient),
		generated.WithRequestEditorFn(backend.Auth.Editor))
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster server client: %w", err)
	}

	return c, nil
}
