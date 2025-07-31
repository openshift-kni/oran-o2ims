/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/api/common"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// SetupOAuthClientConfig constructs an OAuth client configuration from generic parameters.
// This is the common implementation used by both HardwarePlugin and Callback configurations.
func SetupOAuthClientConfig(
	ctx context.Context,
	c client.Client,
	caBundleName *string,
	authClientConfig *common.AuthClientConfig,
	namespace string,
) (*sharedutils.OAuthClientConfig, error) {
	config := &sharedutils.OAuthClientConfig{
		TLSConfig: &sharedutils.TLSConfig{},
	}

	// Set up CA bundle if specified
	if err := SetupCABundle(ctx, c, caBundleName, namespace, config); err != nil {
		return nil, err
	}

	// Set up TLS client certificate if specified
	if err := SetupTLSClientCert(ctx, c, authClientConfig, namespace, config); err != nil {
		return nil, err
	}

	// Set up OAuth configuration if specified
	if err := SetupOAuthConfig(ctx, c, authClientConfig, namespace, config); err != nil {
		return nil, err
	}

	// TODO: process authClientConfig.BasicAuthSecret when `Basic` authType is supported

	return config, nil
}

// SetupCABundle configures the CA bundle for TLS verification using generic parameters
func SetupCABundle(ctx context.Context, c client.Client, caBundleName *string, namespace string, config *sharedutils.OAuthClientConfig) error {
	if caBundleName == nil {
		return nil
	}

	cm, err := sharedutils.GetConfigmap(ctx, c, *caBundleName, namespace)
	if err != nil {
		return fmt.Errorf("failed to get CA bundle configmap: %w", err)
	}

	caBundle, err := sharedutils.GetConfigMapField(cm, constants.CABundleFilename)
	if err != nil {
		return fmt.Errorf("failed to get certificate bundle from configmap: %w", err)
	}

	config.TLSConfig.CaBundle = []byte(caBundle)
	return nil
}

// SetupTLSClientCert configures the TLS client certificate for mutual TLS using generic parameters
func SetupTLSClientCert(ctx context.Context, c client.Client, authClientConfig *common.AuthClientConfig, namespace string, config *sharedutils.OAuthClientConfig) error {
	if authClientConfig == nil ||
		authClientConfig.TLSConfig == nil ||
		authClientConfig.TLSConfig.SecretName == nil {
		return nil
	}

	secretName := *authClientConfig.TLSConfig.SecretName
	cert, key, err := sharedutils.GetKeyPairFromSecret(ctx, c, secretName, namespace)
	if err != nil {
		return fmt.Errorf("failed to get certificate and key from secret: %w", err)
	}

	config.TLSConfig.ClientCert = sharedutils.NewStaticKeyPairLoader(cert, key)
	return nil
}

// SetupOAuthConfig configures OAuth client credentials using generic parameters
func SetupOAuthConfig(ctx context.Context, c client.Client, authClientConfig *common.AuthClientConfig, namespace string, config *sharedutils.OAuthClientConfig) error {
	if authClientConfig == nil ||
		authClientConfig.OAuthClientConfig == nil {
		return nil
	}

	oauthConf := authClientConfig.OAuthClientConfig
	secret, err := sharedutils.GetSecret(ctx, c, oauthConf.ClientSecretName, namespace)
	if err != nil {
		return fmt.Errorf("failed to get OAuth secret '%s': %w", oauthConf.ClientSecretName, err)
	}

	clientID, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientIDField)
	if err != nil {
		return fmt.Errorf("failed to get '%s' from OAuth secret: %w", sharedutils.OAuthClientIDField, err)
	}

	clientSecret, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientSecretField)
	if err != nil {
		return fmt.Errorf("failed to get '%s' from OAuth secret: %w", sharedutils.OAuthClientSecretField, err)
	}

	config.OAuthConfig = &sharedutils.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     BuildTokenURL(oauthConf.URL, oauthConf.TokenEndpoint),
		Scopes:       oauthConf.Scopes,
	}

	return nil
}

// BuildTokenURL constructs the token URL from base URL and token endpoint
func BuildTokenURL(baseURL, tokenEndpoint string) string {
	return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(tokenEndpoint, "/")
}
