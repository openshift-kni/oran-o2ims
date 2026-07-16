/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	authenticationv1 "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// KubernetesAuthenticatorConfig defines the attributes required to instantiate a Kubernetes-based authenticator.Request
type KubernetesAuthenticatorConfig struct {
	RESTConfig     *rest.Config
	ClientCABundle string
	Audiences      []string
	// AudienceExemptPaths lists URL paths that are exempt from the
	// service-specific audience check. Requests to these paths are still
	// authenticated via TokenReview and authorized via RBAC, and the
	// TokenReview validates the token against the default Kubernetes API
	// server audience rather than the service-specific audience. This
	// accepts callers using default-audience SA tokens (e.g., ACM
	// alertmanager). Populated at startup from OpenAPI endpoints marked
	// with x-skip-audience-validation.
	AudienceExemptPaths []string
}

type kubernetesAuthenticator struct {
	authenticator       authenticator.Request
	audiences           authenticator.Audiences
	defaultAudiences    authenticator.Audiences
	audienceExemptPaths map[string]bool
}

// New instantiates an authenticator.Request based on the supplied attributes which delegates control to a Kubernetes
// delegated authenticator.
func (c *KubernetesAuthenticatorConfig) New() (authenticator.Request, error) {
	authenticationV1Client, err := authenticationv1.NewForConfig(c.RESTConfig)
	if err != nil {
		return nil, err // nolint: wrapcheck
	}

	apiAudiences := authenticator.Audiences(c.Audiences)
	if len(c.Audiences) > 0 && len(c.AudienceExemptPaths) > 0 {
		apiAudiences = append(apiAudiences, constants.DefaultKubernetesAudience)
	}

	authenticatorConfig := authenticatorfactory.DelegatingAuthenticatorConfig{
		Anonymous:                &apiserver.AnonymousAuthConfig{Enabled: false}, // Require authentication.
		CacheTTL:                 1 * time.Minute,
		TokenAccessReviewClient:  authenticationV1Client,
		TokenAccessReviewTimeout: 10 * time.Second,
		APIAudiences:             apiAudiences,
		// wait.Backoff is copied from: https://github.com/kubernetes/apiserver/blob/v0.29.0/pkg/server/options/authentication.go#L43-L50
		// options.DefaultAuthWebhookRetryBackoff is not used to avoid a dependency on "k8s.io/apiserver/pkg/server/options".
		WebhookRetryBackoff: &wait.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.2,
			Steps:    5,
		},
	}

	if c.ClientCABundle != "" {
		// This would only become needed if we had to support authentication by client certificate.  The only expected
		// clients that would come through the Kubernetes authentication path are our service accounts for our peer
		// servers, therefore, they would be using access tokens rather than client certificates so this code would be
		// reached. It remains here for test purposes.
		p, err := dynamiccertificates.NewDynamicCAContentFromFile("client-ca", c.ClientCABundle)
		if err != nil {
			return nil, fmt.Errorf("failed to create dynamic CA bundle loader: %w", err)
		}
		authenticatorConfig.ClientCertificateCAContentProvider = p
	}

	delegatingAuthenticator, _, err := authenticatorConfig.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes delegated authenticator: %w", err)
	}

	exemptSet := make(map[string]bool, len(c.AudienceExemptPaths))
	for _, p := range c.AudienceExemptPaths {
		exemptSet[p] = true
	}

	return &kubernetesAuthenticator{
		authenticator:       delegatingAuthenticator,
		audiences:           authenticator.Audiences(c.Audiences),
		defaultAudiences:    authenticator.Audiences{constants.DefaultKubernetesAudience},
		audienceExemptPaths: exemptSet,
	}, nil
}

// AuthenticateRequest delegates the authentication request to the Kubernetes authenticator.
// When audiences are configured, they are injected into the request context so that the
// delegating authenticator includes them in the TokenReview spec and validates them in
// the response — mirroring what the standard Kubernetes API server does in its
// WithAuthentication filter. Paths in audienceExemptPaths use the default Kubernetes API
// server audience instead of the service-specific audience so that callers with default
// SA tokens are accepted while still requiring a valid audience.
func (h *kubernetesAuthenticator) AuthenticateRequest(req *http.Request) (*authenticator.Response, bool, error) {
	if len(h.audiences) > 0 {
		audiences := h.audiences
		if h.audienceExemptPaths[req.URL.Path] {
			audiences = h.defaultAudiences
		}
		ctx := authenticator.WithAudiences(req.Context(), audiences)
		req = req.WithContext(ctx)
	}
	return h.authenticator.AuthenticateRequest(req) // nolint: wrapcheck
}

// KubernetesAuthorizerConfig defines the attributes required to instantiate a Kubernetes-based authorizer.Authorizer
type KubernetesAuthorizerConfig struct {
	RESTConfig *rest.Config
}

type kubernetesAuthorizer struct {
	authorizer authorizer.Authorizer
}

// New instantiates an authorizer.Authorizer which delegates control to a Kubernetes delegated authorizer.
func (c *KubernetesAuthorizerConfig) New() (authorizer.Authorizer, error) {
	authorizationV1Client, err := authorizationv1.NewForConfig(c.RESTConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes authorizer: %w", err)
	}

	authorizerConfig := authorizerfactory.DelegatingAuthorizerConfig{
		SubjectAccessReviewClient: authorizationV1Client,
		AllowCacheTTL:             5 * time.Minute,
		DenyCacheTTL:              30 * time.Second,
		// wait.Backoff is copied from: https://github.com/kubernetes/apiserver/blob/v0.29.0/pkg/server/options/authentication.go#L43-L50
		// options.DefaultAuthWebhookRetryBackoff is not used to avoid a dependency on "k8s.io/apiserver/pkg/server/options".
		WebhookRetryBackoff: &wait.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.2,
			Steps:    5,
		},
	}
	delegatingAuthorizer, err := authorizerConfig.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create authorizer: %w", err)
	}

	return &kubernetesAuthorizer{
		authorizer: delegatingAuthorizer,
	}, nil
}

// Authorize delegates the authorization request to the Kubernetes authorizer.
func (a *kubernetesAuthorizer) Authorize(ctx context.Context, attributes authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	return a.authorizer.Authorize(ctx, attributes) // nolint: wrapcheck
}
