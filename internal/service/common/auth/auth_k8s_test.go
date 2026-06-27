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

	"github.com/getkin/kin-openapi/openapi3"
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

	It("should skip audience injection for exempt paths", func() {
		var capturedAudiences []string

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())

			var review authenticationv1.TokenReview
			err = json.Unmarshal(body, &review)
			Expect(err).ToNot(HaveOccurred())

			capturedAudiences = review.Spec.Audiences

			review.Status = authenticationv1.TokenReviewStatus{
				Authenticated: true,
				User: authenticationv1.UserInfo{
					Username: "alertmanager-sa",
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
			Audiences:           []string{"alarms-server"},
			AudienceExemptPaths: []string{"/internal/v1/caas-alerts/alertmanager"},
		}

		auth, err := config.New()
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest(http.MethodPost, "/internal/v1/caas-alerts/alertmanager", nil)
		req.Header.Set("Authorization", "Bearer exempt-token")

		resp, ok, err := auth.AuthenticateRequest(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(resp.User.GetName()).To(Equal("alertmanager-sa"))
		Expect(capturedAudiences).To(BeEmpty())
	})

	It("should still inject audiences for non-exempt paths when exempt paths are configured", func() {
		var capturedAudiences []string

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			Audiences:           []string{"alarms-server"},
			AudienceExemptPaths: []string{"/internal/v1/caas-alerts/alertmanager"},
		}

		auth, err := config.New()
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest(http.MethodGet, "/o2ims-infrastructureMonitoring/v1/alarms", nil)
		req.Header.Set("Authorization", "Bearer scoped-token")

		resp, ok, err := auth.AuthenticateRequest(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(resp.User.GetName()).To(Equal("test-user"))
		Expect(capturedAudiences).To(ContainElement("alarms-server"))
	})
})

var _ = Describe("GetAudienceExemptPaths", func() {
	It("should return nil for a nil spec", func() {
		paths := GetAudienceExemptPaths(nil)
		Expect(paths).To(BeNil())
	})

	It("should return nil when no paths have the extension", func() {
		spec := &openapi3.T{
			OpenAPI: "3.0.0",
			Info:    &openapi3.Info{Title: "test", Version: "1.0"},
			Paths:   openapi3.NewPaths(),
		}
		spec.Paths.Set("/v1/alarms", &openapi3.PathItem{
			Get: &openapi3.Operation{
				OperationID: "listAlarms",
				Responses:   openapi3.NewResponses(),
			},
		})
		paths := GetAudienceExemptPaths(spec)
		Expect(paths).To(BeNil())
	})

	It("should extract paths with x-skip-audience-validation set to true", func() {
		spec := &openapi3.T{
			OpenAPI: "3.0.0",
			Info:    &openapi3.Info{Title: "test", Version: "1.0"},
			Paths:   openapi3.NewPaths(),
		}
		spec.Paths.Set("/internal/v1/caas-alerts/alertmanager", &openapi3.PathItem{
			Post: &openapi3.Operation{
				OperationID: "AmNotification",
				Extensions: map[string]interface{}{
					skipAudienceValidationExtension: true,
				},
				Responses: openapi3.NewResponses(),
			},
		})
		spec.Paths.Set("/v1/alarms", &openapi3.PathItem{
			Get: &openapi3.Operation{
				OperationID: "listAlarms",
				Responses:   openapi3.NewResponses(),
			},
		})
		paths := GetAudienceExemptPaths(spec)
		Expect(paths).To(ConsistOf("/internal/v1/caas-alerts/alertmanager"))
	})

	It("should ignore paths with x-skip-audience-validation set to false", func() {
		spec := &openapi3.T{
			OpenAPI: "3.0.0",
			Info:    &openapi3.Info{Title: "test", Version: "1.0"},
			Paths:   openapi3.NewPaths(),
		}
		spec.Paths.Set("/v1/alarms", &openapi3.PathItem{
			Get: &openapi3.Operation{
				OperationID: "listAlarms",
				Extensions: map[string]interface{}{
					skipAudienceValidationExtension: false,
				},
				Responses: openapi3.NewResponses(),
			},
		})
		paths := GetAudienceExemptPaths(spec)
		Expect(paths).To(BeNil())
	})
})
