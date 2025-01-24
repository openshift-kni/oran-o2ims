package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
)

// StaticKeyPairLoader provides a means by which we can use a static certificate as input into the dynamic content
// provider interface.  We limit the use of this to clients that are a one-shot implementation that would get re-created
// periodically and wouldn't need to worry about certificates getting renewed as part of a long lived client.
type StaticKeyPairLoader struct {
	cert []byte
	key  []byte
}

// AddListener is a no-op for static providers.
func (s *StaticKeyPairLoader) AddListener(_ dynamiccertificates.Listener) {
	// NOOP
}

// Name is a simple name to identify this provider
func (s *StaticKeyPairLoader) Name() string {
	return "StaticKeyPairLoader"
}

// CurrentCertKeyContent loads the key pair from the dynamic source.  In this case it is static data so we always load
// the same data.
func (s *StaticKeyPairLoader) CurrentCertKeyContent() ([]byte, []byte) {
	return s.cert, s.key
}

func NewStaticKeyPairLoader(cert, key []byte) *StaticKeyPairLoader {
	return &StaticKeyPairLoader{cert: cert, key: key}
}

// TLSConfig defines the TLS config attributes related to a OAuth client configuration
type TLSConfig struct {
	// The client certificate to be used when initiating connection to the server.
	ClientCert dynamiccertificates.CertKeyContentProvider
	// Defines a PEM encoded set of CA certificates used to validate server certificates.  If not provided then the
	// default root CA bundle will be used.
	CaBundle []byte
}

// OAuthConfig defines the OAuth config attributes related to an OAuth client configuration
type OAuthConfig struct {
	// Defines the OAuth client-id attribute to be used when acquiring a token.  If not provided (for debug/testing)
	// then a normal HTTP client without OAuth capabilities will be created
	ClientID string
	// Defines the OAuth client-secret attribute to be used when acquiring a token.
	ClientSecret string
	// The absolute URL of the API endpoint to be used to acquire a token
	// (e.g., http://example.com/realms/oran/protocol/openid-connect/token)
	TokenURL string
	// The list of OAuth scopes requested by the client.  These will be dictated by what the SMO is expecting to see in
	// the token.
	Scopes []string
}

// OAuthClientConfig defines the parameters required to establish an HTTP Client capable of acquiring an OAuth Token
// from an OAuth capable authorization server.
type OAuthClientConfig struct {
	OAuthConfig *OAuthConfig
	// The TLS related configuration attributes
	TLSConfig *TLSConfig
}

// SetupOAuthClient creates an HTTP client capable of acquiring an OAuth token used to authorize client requests.  If
// the config excludes the OAuth specific sections then the client produced is a simple HTTP client without OAuth
// capabilities.
func SetupOAuthClient(ctx context.Context, config *OAuthClientConfig) (*http.Client, error) {
	tlsConfig, _ := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})

	// Adjust the TLS config with the related options passed in
	if config.TLSConfig != nil {
		err := setupTLSConfig(config.TLSConfig, tlsConfig)
		if err != nil {
			return nil, err
		}
		slog.Info("Configured TLS client")
	}

	baseClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 30 * time.Second,
	}

	if config.OAuthConfig != nil && config.OAuthConfig.ClientID != "" {
		oauthConfig := clientcredentials.Config{
			ClientID:       config.OAuthConfig.ClientID,
			ClientSecret:   config.OAuthConfig.ClientSecret,
			TokenURL:       config.OAuthConfig.TokenURL,
			Scopes:         config.OAuthConfig.Scopes,
			EndpointParams: nil,
			AuthStyle:      oauth2.AuthStyleInParams,
		}

		ctx = context.WithValue(ctx, oauth2.HTTPClient, baseClient)
		oauthClient := oauthConfig.Client(ctx)

		slog.Info("Successfully created oauth client")
		return oauthClient, nil
	}

	slog.Info("Successfully created base client")
	return baseClient, nil
}

// setupTLSConfig updates the TLS config with the related options from the OAuth configuration
func setupTLSConfig(config *TLSConfig, tlsConfig *tls.Config) error {
	if config.ClientCert != nil {
		// Enable mTLS if a client certificate was provided.  The client CA is expected to be recognized by the server.
		// We are using a dynamic client certificate approach so that we can support renewing our certificate without
		// restarting the server.
		tlsConfig.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.X509KeyPair(config.ClientCert.CurrentCertKeyContent())
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %w", err)
			}
			return &cert, nil
		}
	}

	if len(config.CaBundle) != 0 {
		// If the user has provided a CA bundle then we must use it to build our client so that we can verify the
		// identity of remote servers.
		if tlsConfig.RootCAs == nil {
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(config.CaBundle) {
				return fmt.Errorf("failed to append certificate bundle to pool")
			}
			tlsConfig.RootCAs = certPool
		} else {
			// We may not need the default CA bundles in this case but there's no harm in keeping them in the pool
			// to handle cases where they may be needed.
			tlsConfig.RootCAs.AppendCertsFromPEM(config.CaBundle)
		}
	}
	return nil
}
