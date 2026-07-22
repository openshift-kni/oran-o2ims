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
	It("sets EndpointParams with audience when provided", func() {
		audience := "https://api.example.com"

		var endpointParams url.Values
		if audience != "" {
			endpointParams = url.Values{"audience": {audience}}
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
		Expect(oauthConfig.EndpointParams["audience"]).To(Equal([]string{"https://api.example.com"}))
	})

	It("leaves EndpointParams nil when no audience provided", func() {
		audience := ""

		var endpointParams url.Values
		if audience != "" {
			endpointParams = url.Values{"audience": {audience}}
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

	It("OAuthConfig struct stores audience correctly", func() {
		config := OAuthConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     "https://oauth.example.com/token",
			Scopes:       []string{"openid"},
			Audience:     "https://api.example.com",
		}

		Expect(config.Audience).To(Equal("https://api.example.com"))
	})

	It("OAuthConfig struct defaults to empty audience", func() {
		config := OAuthConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     "https://oauth.example.com/token",
			Scopes:       []string{"openid"},
		}

		Expect(config.Audience).To(BeEmpty())
	})
})
