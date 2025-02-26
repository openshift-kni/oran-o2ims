package utils

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// OAuthConfig defines the attributes used to communicate with the OAuth server
type OAuthConfig struct {
	IssuerURL          string
	TokenEndpoint      string
	ClientID           string `envconfig:"SMO_OAUTH_CLIENT_ID"`
	ClientSecret       string `envconfig:"SMO_OAUTH_CLIENT_SECRET"`
	Scopes             []string
	UsernameClaim      string
	GroupsClaim        string
	ClientBindingClaim string
}

// TLSConfig defines the attributes used to establish an mTLS session
type TLSConfig struct {
	CertFile       string
	KeyFile        string
	ClientCertFile string
	ClientKeyFile  string
	CABundleFile   string
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
	ListenerFlagName                = "api-listener-address"
	OAuthIssuerURLFlagName          = "oauth-issuer-url"     // nolint: gosec
	OAuthTokenEndpointFlagName      = "oauth-token-endpoint" // nolint: gosec
	OAuthScopesFlagName             = "oauth-scopes"
	OAuthUsernameClaimFlagName      = "oauth-username-claim"
	OAuthGroupsClaimFlagName        = "oauth-groups-claim"
	OAuthClientBindingClaimFlagName = "oauth-client-binding-claim"
	ServerCertFileFlagName          = "tls-server-cert"
	ServerKeyFileFlagName           = "tls-server-key"
	ClientCertFileFlagName          = "tls-client-cert"
	ClientKeyFileFlagName           = "tls-client-key"
	CABundleFileFlagName            = "ca-bundle-file"
)

// SetCommonServerFlags creates the flag instances for the server
func SetCommonServerFlags(cmd *cobra.Command, config *CommonServerConfig) error {
	flags := cmd.Flags()
	flags.StringVar(
		&config.Listener.Address,
		ListenerFlagName,
		fmt.Sprintf("127.0.0.1:%d", utils.DefaultContainerPort),
		"API listener address",
	)
	flags.StringVar(
		&config.OAuth.TokenEndpoint,
		OAuthTokenEndpointFlagName,
		"",
		"OAuth server token URL",
	)
	flags.StringVar(
		&config.OAuth.IssuerURL,
		OAuthIssuerURLFlagName,
		"",
		"OAuth server URL",
	)
	flags.StringSliceVar(
		&config.OAuth.Scopes,
		OAuthScopesFlagName,
		[]string{},
		"OAuth client scopes",
	)
	flags.StringVar(
		&config.OAuth.UsernameClaim,
		OAuthUsernameClaimFlagName,
		"",
		"OAuth username claim",
	)
	flags.StringVar(
		&config.OAuth.GroupsClaim,
		OAuthGroupsClaimFlagName,
		"",
		"OAuth groups claim",
	)
	flags.StringVar(
		&config.OAuth.ClientBindingClaim,
		OAuthClientBindingClaimFlagName,
		"",
		"OAuth client binding claim",
	)
	flags.StringVar(
		&config.TLS.CertFile,
		ServerCertFileFlagName,
		fmt.Sprintf("%s/tls.crt", utils.TLSServerMountPath),
		"Server certificate file",
	)
	flags.StringVar(
		&config.TLS.KeyFile,
		ServerKeyFileFlagName,
		fmt.Sprintf("%s/tls.key", utils.TLSServerMountPath),
		"Server private key file",
	)
	flags.StringVar(
		&config.TLS.ClientCertFile,
		ClientCertFileFlagName,
		"",
		"Client certificate file for mTLS authentication",
	)
	flags.StringVar(
		&config.TLS.ClientKeyFile,
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

	if c.OAuth.ClientID != "" && (c.OAuth.TokenEndpoint == "" || c.OAuth.IssuerURL == "") {
		return fmt.Errorf("token endpoint and issuer URL are required if client ID is specified")
	}

	if c.OAuth.TokenEndpoint != "" && c.OAuth.ClientID == "" {
		return fmt.Errorf("client ID is required if token URL is specified")
	}

	if (c.TLS.ClientCertFile != "" && c.TLS.ClientKeyFile == "") ||
		(c.TLS.ClientCertFile == "" && c.TLS.ClientKeyFile != "") {
		return fmt.Errorf("both TLS cert file and key file are required")
	}

	return nil
}

// CreateOAuthConfig builds an OAuthClientConfig from the specified parameters
func (c *CommonServerConfig) CreateOAuthConfig(ctx context.Context) (*utils.OAuthClientConfig, error) {
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

	if c.TLS.ClientCertFile != "" && c.TLS.ClientKeyFile != "" {
		// Load the mTLS client cert/key dynamic to support certificate renewals
		dynamicClientCert, err := dynamiccertificates.NewDynamicServingContentFromFiles("client-oauth", c.TLS.ClientCertFile, c.TLS.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create dynamic client certificate loader: %w", err)
		}
		if config.TLSConfig == nil {
			config.TLSConfig = &utils.TLSConfig{}
		}
		config.TLSConfig.ClientCert = dynamicClientCert
		slog.Debug("using TLS client config", "cert", c.TLS.ClientCertFile, ",key", c.TLS.ClientKeyFile)

		// Run the controller so that it monitors for file changes
		go dynamicClientCert.Run(ctx, 1)
	}

	config.OAuthConfig = &utils.OAuthConfig{
		TokenURL:     fmt.Sprintf("%s/%s", c.OAuth.IssuerURL, c.OAuth.TokenEndpoint),
		ClientID:     c.OAuth.ClientID,
		ClientSecret: c.OAuth.ClientSecret,
		Scopes:       c.OAuth.Scopes,
	}

	return &config, nil
}
