package auth

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/client-go/rest"

	utils2 "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// GetAuthenticator builds authentication middleware to be used to extract user/group identity from incoming requests
func GetAuthenticator(ctx context.Context, config *utils.CommonServerConfig) (middleware.Middleware, error) {
	// Setup kubernetes config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	// Create a client TLS config suitable to be used with entities outside the cluster
	clientTLSConfig, err := utils2.GetClientTLSConfig(ctx, config.TLS.ClientCertFile, config.TLS.ClientKeyFile, config.TLS.CABundleFile)
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
		RESTConfig: restConfig,
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
