/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clients

import (
	"fmt"
	"net/http"
	"os"

	"k8s.io/client-go/kubernetes"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// Backend holds what a generated oapi-codegen client needs to call another O2IMS service: the service URL, an HTTP
// client with the internal TLS configuration, and an editor that adds an audience-scoped bearer token.
type Backend struct {
	URL        string
	HTTPClient *http.Client
	Auth       *AuthorizationEditor
}

// BackendOption customizes how NewBackend builds the connection settings.
type BackendOption func(*backendOptions)

type backendOptions struct {
	newTransport func() (http.RoundTripper, error)
}

// WithTransport makes NewBackend use rt instead of the default internal TLS transport.
func WithTransport(rt http.RoundTripper) BackendOption {
	return WithTransportFactory(func() (http.RoundTripper, error) { return rt, nil })
}

// WithTransportFactory makes NewBackend build its transport with f instead of the default internal TLS transport,
// which loads CA bundles from in-cluster paths. An error from f is returned by NewBackend.
func WithTransportFactory(f func() (http.RoundTripper, error)) BackendOption {
	return func(o *backendOptions) {
		o.newTransport = f
	}
}

// NewBackend prepares the connection settings for calling the O2IMS service serverName on behalf of the service
// account callerSA. The URL defaults to the in-cluster service URL and can be overridden with the urlEnvName
// environment variable for local development. Tokens are minted for callerSA, in the operator namespace, with
// serverName as the audience.
func NewBackend(clientset kubernetes.Interface, serverName, urlEnvName, callerSA string, opts ...BackendOption) (*Backend, error) {
	o := backendOptions{newTransport: ctlrutils.GetDefaultBackendTransport}
	for _, opt := range opts {
		opt(&o)
	}

	// GetServiceURL uses the same namespace, so the URL and the caller's service account always agree
	namespace := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	url := ctlrutils.GetServiceURL(serverName)
	if override := os.Getenv(urlEnvName); override != "" {
		url = override
	}

	tr, err := o.newTransport()
	if err != nil {
		return nil, fmt.Errorf("failed to create http transport: %w", err)
	}

	return &Backend{
		URL:        url,
		HTTPClient: &http.Client{Transport: tr},
		Auth: &AuthorizationEditor{
			Source: NewTokenRequestTokenSource(clientset, namespace, callerSA, serverName),
		},
	}, nil
}
