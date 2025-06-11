/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/request/bearertoken"
	"k8s.io/apiserver/plugin/pkg/authenticator/token/oidc"
)

var DefaultSigningAlgorithms = []string{"RS256"}

// OAuthAuthenticatorConfig defines the attributes required to instantiate an OAuth-based authenticator.Request.
type OAuthAuthenticatorConfig struct {
	IssuerURL            string
	ClientID             string
	UsernameClaim        string
	GroupsClaim          string
	SupportedSigningAlgs []string
	ClientBindingClaim   string
	Client               *http.Client
}

type oAuthAuthenticator struct {
	authenticator authenticator.Request
	clientID      string
}

// New instantiates an OAuth-based authenticator.Request which delegates control to an OIDC token authenticator.
func (c *OAuthAuthenticatorConfig) New(ctx context.Context) (authenticator.Request, error) {
	noPrefix := ""
	options := oidc.Options{
		JWTAuthenticator: apiserver.JWTAuthenticator{
			Issuer: apiserver.Issuer{
				URL:       c.IssuerURL,
				Audiences: []string{c.ClientID},
			},
			ClaimMappings: apiserver.ClaimMappings{
				Username: apiserver.PrefixedClaimOrExpression{
					Prefix: &noPrefix,
					Claim:  c.UsernameClaim,
				},
				Groups: apiserver.PrefixedClaimOrExpression{
					Prefix: &noPrefix,
					Claim:  c.GroupsClaim,
				},
				Extra: []apiserver.ExtraMapping{
					{
						Key:             fingerprintKey,
						ValueExpression: c.ClientBindingClaim,
					},
				},
			},
		},
		Client:               c.Client,
		SupportedSigningAlgs: c.SupportedSigningAlgs,
	}

	jwtAuthenticator, err := oidc.New(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC authenticator: %w", err)
	}

	return &oAuthAuthenticator{
		// Wrap the bearer token authenticate with one that can verify the client certificate binding if it is present
		// in the incoming token.
		authenticator: WithClientVerification(bearertoken.New(jwtAuthenticator)),
		clientID:      c.ClientID,
	}, nil
}

func (h *oAuthAuthenticator) AuthenticateRequest(req *http.Request) (*authenticator.Response, bool, error) {
	return h.authenticator.AuthenticateRequest(req) // nolint: wrapcheck
}
