/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

var _ = Describe("OAuth audience configuration", func() {
	It("sets EndpointParams with audiences when provided", func() {
		audiences := []string{"https://api.example.com", "my-api"}

		var endpointParams url.Values
		if len(audiences) > 0 {
			endpointParams = url.Values{"audience": audiences}
		}

		oauthConfig := clientcredentials.Config{
			ClientID:       "test-client",
			ClientSecret:   "test-secret",
			TokenURL:       "https://oauth.example.com/token",
			Scopes:         []string{"openid"},
			EndpointParams: endpointParams,
			AuthStyle:      oauth2.AuthStyleInParams,
		}

		Expect(oauthConfig.EndpointParams).ToNot(BeNil())
		Expect(oauthConfig.EndpointParams["audience"]).To(Equal([]string{"https://api.example.com", "my-api"}))
	})

	It("leaves EndpointParams nil when no audiences provided", func() {
		var audiences []string

		var endpointParams url.Values
		if len(audiences) > 0 {
			endpointParams = url.Values{"audience": audiences}
		}

		oauthConfig := clientcredentials.Config{
			ClientID:       "test-client",
			ClientSecret:   "test-secret",
			TokenURL:       "https://oauth.example.com/token",
			Scopes:         []string{"openid"},
			EndpointParams: endpointParams,
			AuthStyle:      oauth2.AuthStyleInParams,
		}

		Expect(oauthConfig.EndpointParams).To(BeNil())
	})

	It("OAuthConfig struct stores audiences correctly", func() {
		config := OAuthConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     "https://oauth.example.com/token",
			Scopes:       []string{"openid"},
			Audiences:    []string{"https://api.example.com"},
		}

		Expect(config.Audiences).To(HaveLen(1))
		Expect(config.Audiences[0]).To(Equal("https://api.example.com"))
	})

	It("OAuthConfig struct defaults to nil audiences", func() {
		config := OAuthConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     "https://oauth.example.com/token",
			Scopes:       []string{"openid"},
		}

		Expect(config.Audiences).To(BeNil())
	})
})
