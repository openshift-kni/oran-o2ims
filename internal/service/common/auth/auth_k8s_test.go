/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/client-go/rest"
)

var _ = Describe("KubernetesAuthenticatorConfig", func() {
	It("should propagate audiences through the authenticator to TokenReview requests", func() {
		var capturedAudiences []string

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())

			var review authenticationv1.TokenReview
			err = json.Unmarshal(body, &review)
			Expect(err).ToNot(HaveOccurred())

			capturedAudiences = review.Spec.Audiences

			review.Status = authenticationv1.TokenReviewStatus{
				Authenticated: true,
				User: authenticationv1.UserInfo{
					Username: "test-user",
					Groups:   []string{"system:authenticated"},
				},
			}

			respBytes, err := json.Marshal(review)
			Expect(err).ToNot(HaveOccurred())
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write(respBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer server.Close()

		config := KubernetesAuthenticatorConfig{
			RESTConfig: &rest.Config{
				Host: server.URL,
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: true,
				},
				ContentConfig: rest.ContentConfig{
					ContentType: "application/json",
				},
			},
			Audiences: []string{"test-audience"},
		}

		auth, err := config.New()
		Expect(err).ToNot(HaveOccurred())
		Expect(auth).ToNot(BeNil())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		resp, ok, err := auth.AuthenticateRequest(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(resp.User.GetName()).To(Equal("test-user"))
		Expect(capturedAudiences).To(ContainElement("test-audience"))
	})

	It("should authenticate requests without audience filtering when audiences are empty", func() {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())

			var review authenticationv1.TokenReview
			err = json.Unmarshal(body, &review)
			Expect(err).ToNot(HaveOccurred())

			review.Status = authenticationv1.TokenReviewStatus{
				Authenticated: true,
				User: authenticationv1.UserInfo{
					Username: "test-user",
				},
			}

			respBytes, err := json.Marshal(review)
			Expect(err).ToNot(HaveOccurred())
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write(respBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer server.Close()

		config := KubernetesAuthenticatorConfig{
			RESTConfig: &rest.Config{
				Host: server.URL,
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: true,
				},
				ContentConfig: rest.ContentConfig{
					ContentType: "application/json",
				},
			},
			Audiences: []string{},
		}

		auth, err := config.New()
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		resp, ok, err := auth.AuthenticateRequest(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(resp.User.GetName()).To(Equal("test-user"))
	})
})
