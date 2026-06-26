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
	"time"

	"golang.org/x/oauth2"

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
	oauthConfig        *ctlrutils.OAuthClientConfig
	serviceTokenSource oauth2.TokenSource
}

// ClientProvider defines the interface which any client factory must implement.  This exists for
// future unit test purposes so that the ClientFactory can be swapped out as needed.
type ClientProvider interface {
	NewClient(ctx context.Context, authType commonapi.AuthType) (*http.Client, error)
}

// NewClientFactory creates a new factory
func NewClientFactory(oauthConfig *ctlrutils.OAuthClientConfig, serviceTokenSource oauth2.TokenSource) ClientProvider {
	return &ClientFactory{
		oauthConfig:        oauthConfig,
		serviceTokenSource: serviceTokenSource,
	}
}

func (f *ClientFactory) newClusterClient(ctx context.Context) (*http.Client, error) {
	tlsConfig, err := ctlrutils.GetDefaultTLSConfig(nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 30 * time.Second,
	}
	if f.serviceTokenSource != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
		client = oauth2.NewClient(ctx, f.serviceTokenSource)
	}
	BlockCrossHostRedirects(client)
	return client, nil
}

func (f *ClientFactory) newOAuthClient(ctx context.Context) (*http.Client, error) {
	client, err := ctlrutils.SetupOAuthClient(ctx, nil, f.oauthConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup oauth client")
	}
	BlockCrossHostRedirects(client)
	return client, nil
}

// NewClient creates a new Client based on the auth type. For ServiceAccount auth, a cluster-internal
// client is created; when serviceTokenSource is non-nil it wraps the client with bearer token
// authorization, otherwise callbacks are sent without authorization. For OAuth auth, the client is
// configured with the OAuth credentials for public endpoint callbacks.
func (f *ClientFactory) NewClient(ctx context.Context, authType commonapi.AuthType) (*http.Client, error) {
	if authType == commonapi.ServiceAccount {
		return f.newClusterClient(ctx)
	}
	return f.newOAuthClient(ctx)
}
