/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

// Package resourceserver provides a client for the O2IMS resource server API that any O2IMS service can use.
package resourceserver

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/resourceserver/generated"
)

// URLEnvName overrides the resource server URL, for local development.
const URLEnvName = "RESOURCE_SERVER_URL"

// NewClient creates a resource server client that authenticates as the service account callerSA.
// Options are passed to clients.NewBackend.
func NewClient(clientset kubernetes.Interface, callerSA string, opts ...clients.BackendOption) (*generated.ClientWithResponses, error) {
	backend, err := clients.NewBackend(clientset, ctlrutils.InventoryResourceServerName, URLEnvName, callerSA, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare resource server connection: %w", err)
	}

	c, err := generated.NewClientWithResponses(backend.URL,
		generated.WithHTTPClient(backend.HTTPClient),
		generated.WithRequestEditorFn(backend.Auth.Editor))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource server client: %w", err)
	}

	return c, nil
}
