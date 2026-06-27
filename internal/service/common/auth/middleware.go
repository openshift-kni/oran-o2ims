/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/client-go/rest"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const skipAudienceValidationExtension = "x-skip-audience-validation"

// GetAudienceExemptPaths extracts API paths marked with x-skip-audience-validation
// from the OpenAPI spec. These paths are exempt from audience-scoped token
// validation while still requiring authentication (TokenReview) and
// authorization (RBAC). This is used at startup to build the exempt path
// list — no per-request spec traversal is needed.
func GetAudienceExemptPaths(spec *openapi3.T) []string {
	if spec == nil || spec.Paths == nil {
		return nil
	}
	var paths []string
	for _, path := range spec.Paths.InMatchingOrder() {
		pathItem := spec.Paths.Find(path)
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if v, ok := op.Extensions[skipAudienceValidationExtension].(bool); ok && v {
				paths = append(paths, path)
				break
			}
		}
	}
	return paths
}

// GetAuthenticator builds authentication middleware to be used to extract user/group identity from incoming requests
func GetAuthenticator(ctx context.Context, config *svcutils.CommonServerConfig) (middleware.Middleware, error) {
	// Setup kubernetes config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	// Create a client TLS config suitable to be used with entities outside the cluster
	clientTLSConfig, err := ctlrutils.GetClientTLSConfig(ctx, config.TLS.ClientCertFile, config.TLS.ClientKeyFile, config.TLS.CABundleFile)
	if err != nil {
		return nil, fmt.Errorf("failed to get client TLS config: %w", err)
	}

	client := &http.Client{
		Transport: net.SetTransportDefaults(&http.Transport{TLSClientConfig: clientTLSConfig}),
	}

	var oauthAuthenticator authenticator.Request
	if config.OAuth.IssuerURL != "" {
		authenticatorConfig := OAuthAuthenticatorConfig{
			IssuerURL:            config.OAuth.IssuerURL,
			ClientID:             config.OAuth.ClientID,
			UsernameClaim:        config.OAuth.UsernameClaim,
			GroupsClaim:          config.OAuth.GroupsClaim,
			ClientBindingClaim:   config.OAuth.ClientBindingClaim,
			SupportedSigningAlgs: DefaultSigningAlgorithms,
			Client:               client,
		}
		oauthAuthenticator, err = authenticatorConfig.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create oauth authenticator: %w", err)
		}
	}

	authenticatorConfig := KubernetesAuthenticatorConfig{
		RESTConfig:          restConfig,
		AudienceExemptPaths: config.AudienceExemptPaths,
	}
	if config.Audience != "" {
		authenticatorConfig.Audiences = []string{config.Audience}
	}
	k8sAuthenticator, err := authenticatorConfig.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s authenticator: %w", err)
	}

	return Authenticator(oauthAuthenticator, k8sAuthenticator), nil
}

// GetAuthorizer builds authorization middleware to be used authorize incoming requests
func GetAuthorizer() (middleware.Middleware, error) {
	// Setup kubernetes config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	// Setup authorizer
	c := KubernetesAuthorizerConfig{RESTConfig: restConfig}
	k8sAuthorizer, err := c.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s authorizer: %w", err)
	}

	return Authorizer(k8sAuthorizer), nil
}
