package utils

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// OAuthConfig defines the attributes used to communicate with the OAuth server
type OAuthConfig struct {
	TokenURL     string
	ClientID     string `envconfig:"SMO_OAUTH_CLIENT_ID"`
	ClientSecret string `envconfig:"SMO_OAUTH_CLIENT_SECRET"`
	Scopes       []string
}

// TLSConfig defines the attributes used to establish an mTLS session
type TLSConfig struct {
	CertFile     string
	KeyFile      string
	CABundleFile string
}

// ListenerConfig defines the attributes used to start an HTTPS server
type ListenerConfig struct {
	Address string
}

type CommonServerConfig struct {
	// Listener defines the attributes to set up the HTTPS listener
	Listener ListenerConfig
	// OAuth defines the attributes required to communicate with the OAuth server
	OAuth OAuthConfig
	// TLS defines the attributes used to start mTLS sessions to the SMO/OAuth servers
	TLS TLSConfig
}

const (
	ListenerFlagName       = "api-listener-address"
	OAuthTokenURLFlagName  = "oauth-token-url" // nolint: gosec
	OAuthScopesFlagName    = "oauth-scopes"
	ClientCertFileFlagName = "tls-client-cert"
	ClientKeyFileFlagName  = "tls-client-key"
	CABundleFileFlagName   = "ca-bundle-file"
)

// SetCommonServerFlags creates the flag instances for the server
func SetCommonServerFlags(cmd *cobra.Command, config *CommonServerConfig) error {
	flags := cmd.Flags()
	flags.StringVar(
		&config.Listener.Address,
		ListenerFlagName,
		"127.0.0.1:8000",
		"API listener address",
	)
	flags.StringVar(
		&config.OAuth.TokenURL,
		OAuthTokenURLFlagName,
		"",
		"OAuth server token URL",
	)
	flags.StringSliceVar(
		&config.OAuth.Scopes,
		OAuthScopesFlagName,
		[]string{},
		"OAuth client scopes",
	)
	flags.StringVar(
		&config.TLS.CertFile,
		ClientCertFileFlagName,
		"",
		"Client certificate file for mTLS authentication",
	)
	flags.StringVar(
		&config.TLS.KeyFile,
		ClientKeyFileFlagName,
		"",
		"Client private key file for mTLS authentication",
	)
	flags.StringVar(
		&config.TLS.CABundleFile,
		CABundleFileFlagName,
		"",
		"Custom CA certificate bundle file",
	)

	return nil
}

// LoadFromEnv loads config values from the environment
func (c *CommonServerConfig) LoadFromEnv() error {
	err := envconfig.Process("common", c)
	if err != nil {
		return fmt.Errorf("failed to process environment variables: %w", err)
	}
	return nil
}

// Validate checks the configuration attribute to ensure they are semantically correct
func (c *CommonServerConfig) Validate() error {
	if c.Listener.Address == "" {
		return fmt.Errorf("listener address is required")
	}

	if (c.OAuth.ClientID == "" && c.OAuth.ClientSecret != "") ||
		(c.OAuth.ClientID != "" && c.OAuth.ClientSecret == "") {
		return fmt.Errorf("both OAuth client ID and OAuth client secret are required")
	}

	if c.OAuth.ClientID != "" && c.OAuth.TokenURL == "" {
		return fmt.Errorf("token URL is required if client ID is specified")
	}

	if c.OAuth.TokenURL != "" && c.OAuth.ClientID == "" {
		return fmt.Errorf("client ID is required if token URL is specified")
	}

	if (c.TLS.CertFile != "" && c.TLS.KeyFile == "") ||
		(c.TLS.CertFile == "" && c.TLS.KeyFile != "") {
		return fmt.Errorf("both TLS cert file and key file are required")
	}

	return nil
}

// CreateOAuthConfig builds an OAuthClientConfig from the specified parameters
func (c *CommonServerConfig) CreateOAuthConfig() (*utils.OAuthClientConfig, error) {
	config := utils.OAuthClientConfig{}
	if c.TLS.CABundleFile != "" {
		// Load the bundle
		bytes, err := os.ReadFile(c.TLS.CABundleFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA bundle file '%s': %w", c.TLS.CABundleFile, err)
		}
		config.TLSConfig = &utils.TLSConfig{CaBundle: bytes}
		slog.Debug("using CA bundle", "path", c.TLS.CABundleFile)
	}

	if c.TLS.CertFile != "" && c.TLS.KeyFile != "" {
		// Load the mTLS client cert/key
		cert, err := tls.LoadX509KeyPair(c.TLS.CertFile, c.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate and key pair: %w", err)
		}
		if config.TLSConfig == nil {
			config.TLSConfig = &utils.TLSConfig{}
		}
		config.TLSConfig.ClientCert = &cert
		slog.Debug("using TLS client config", "cert", c.TLS.CertFile, ",key", c.TLS.KeyFile)
	}

	config.OAuthConfig = &utils.OAuthConfig{
		TokenURL:     c.OAuth.TokenURL,
		ClientID:     c.OAuth.ClientID,
		ClientSecret: c.OAuth.ClientSecret,
		Scopes:       c.OAuth.Scopes,
	}

	return &config, nil
}
