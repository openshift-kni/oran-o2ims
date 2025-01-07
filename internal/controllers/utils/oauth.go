package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// OAuthClientConfig defines the parameters required to establish an HTTP Client capable of acquiring an OAuth Token
// from an OAuth capable authorization server.
type OAuthClientConfig struct {
	// Defines a PEM encoded set of CA certificates used to validate server certificates.  If not provided then the
	// default root CA bundle will be used.
	CaBundle []byte
	// Defines the OAuth client-id attribute to be used when acquiring a token.  If not provided (for debug/testing)
	// then a normal HTTP client without OAuth capabilities will be created
	ClientId     string
	ClientSecret string
	// The absolute URL of the API endpoint to be used to acquire a token
	// (e.g., http://example.com/realms/oran/protocol/openid-connect/token)
	TokenUrl string
	// The list of OAuth scopes requested by the client.  These will be dictated by what the SMO is expecting to see in
	// the token.
	Scopes []string
	// The client certificate to be used when initiating connection to the server.
	ClientCert *tls.Certificate
}

// SetupOAuthClient creates an HTTP client capable of acquiring an OAuth token used to authorize client requests.  If
// the config excludes the OAuth specific sections then the client produced is a simple HTTP client without OAuth
// capabilities.
func SetupOAuthClient(ctx context.Context, config *OAuthClientConfig) (*http.Client, error) {
	tlsConfig, _ := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})

	if config.ClientCert != nil {
		// Enable mTLS if a client certificate was provided.  The client CA is expected to be recognized by the server.
		tlsConfig.Certificates = []tls.Certificate{*config.ClientCert}
	}

	if len(config.CaBundle) != 0 {
		// If the user has provided a CA bundle then we must use it to build our client so that we can verify the
		// identity of remote servers.
		if tlsConfig.RootCAs == nil {
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(config.CaBundle) {
				return nil, fmt.Errorf("failed to append certificate bundle to pool")
			}
			tlsConfig.RootCAs = certPool
		} else {
			// We may not need the default CA bundles in this case but there's no harm in keeping them in the pool
			// to handle cases where they may be needed.
			tlsConfig.RootCAs.AppendCertsFromPEM(config.CaBundle)
		}
	}

	c := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig}}

	if config.ClientId != "" {
		config := clientcredentials.Config{
			ClientID:       config.ClientId,
			ClientSecret:   config.ClientSecret,
			TokenURL:       config.TokenUrl,
			Scopes:         config.Scopes,
			EndpointParams: nil,
			AuthStyle:      oauth2.AuthStyleInParams,
		}

		ctx = context.WithValue(ctx, oauth2.HTTPClient, c)

		c = config.Client(ctx)
	}

	return c, nil
}
