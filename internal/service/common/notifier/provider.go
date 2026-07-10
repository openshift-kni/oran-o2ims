/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// ErrCrossHostRedirect is returned when an HTTP redirect targets a different host.
var ErrCrossHostRedirect = errors.New("cross-host redirect blocked")

// BlockCrossHostRedirects sets a CheckRedirect policy that prevents the client from following
// redirects to a different host or port, which would leak credentials to untrusted endpoints.
func BlockCrossHostRedirects(client *http.Client) {
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		originalHost := via[0].URL.Host
		redirectHost := req.URL.Host
		if redirectHost != originalHost {
			return fmt.Errorf("%w: %s -> %s", ErrCrossHostRedirect, originalHost, redirectHost)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}

// ClientFactory is a utility used to abstract building an HTTP client based on the type of callback
// URL supplied.
type ClientFactory struct {
	oauthConfig *ctlrutils.OAuthClientConfig
}

// ClientProvider defines the interface which any client factory must implement.  This exists for
// future unit test purposes so that the ClientFactory can be swapped out as needed.
type ClientProvider interface {
	NewClient(ctx context.Context, authType commonapi.AuthType) (*http.Client, error)
}

// NewClientFactory creates a new factory
func NewClientFactory(oauthConfig *ctlrutils.OAuthClientConfig) ClientProvider {
	return &ClientFactory{
		oauthConfig: oauthConfig,
	}
}

// NewClient creates an OAuth-authenticated HTTP client for callback delivery.
func (f *ClientFactory) NewClient(ctx context.Context, _ commonapi.AuthType) (*http.Client, error) {
	client, err := ctlrutils.SetupOAuthClient(ctx, nil, f.oauthConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup oauth client")
	}
	BlockCrossHostRedirects(client)
	return client, nil
}
