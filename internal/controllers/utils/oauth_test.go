/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OAuth audience configuration", func() {
	It("includes audience parameter in token request when configured", func() {
		var capturedForm url.Values
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			capturedForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
		}))
		defer tokenServer.Close()

		resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer resourceServer.Close()

		client, err := SetupOAuthClient(context.Background(), nil, &OAuthClientConfig{
			OAuthConfig: &OAuthConfig{
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				TokenURL:     tokenServer.URL,
				Scopes:       []string{"openid"},
				Audience:     "https://api.example.com",
			},
		})
		Expect(err).ToNot(HaveOccurred())

		resp, err := client.Get(resourceServer.URL)
		Expect(err).ToNot(HaveOccurred())
		resp.Body.Close()

		Expect(capturedForm).To(HaveKey("audience"))
		Expect(capturedForm["audience"]).To(Equal([]string{"https://api.example.com"}))
	})

	It("omits audience parameter in token request when not configured", func() {
		var capturedForm url.Values
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			capturedForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
		}))
		defer tokenServer.Close()

		resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer resourceServer.Close()

		client, err := SetupOAuthClient(context.Background(), nil, &OAuthClientConfig{
			OAuthConfig: &OAuthConfig{
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				TokenURL:     tokenServer.URL,
				Scopes:       []string{"openid"},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		resp, err := client.Get(resourceServer.URL)
		Expect(err).ToNot(HaveOccurred())
		resp.Body.Close()

		Expect(capturedForm).ToNot(HaveKey("audience"))
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
