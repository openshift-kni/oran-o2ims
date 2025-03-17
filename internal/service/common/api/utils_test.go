// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package api_test

import (
	"context"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateCallbackURL", func() {
	var (
		ctx      context.Context
		callback string
		client   *http.Client
		server   *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Use NewTLSServer to create an HTTPS server that returns 204 for GET requests.
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		callback = server.URL // This URL uses "https"
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	Context("when the callback URL is valid and reachable", func() {
		It("should not return an error", func() {
			stub := &stubClientProvider{client: client}
			err := api.ValidateCallbackURL(ctx, stub, callback)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the callback URL has an invalid scheme", func() {
		It("should return an error", func() {
			invalidURL := "http://example.com"
			stub := &stubClientProvider{client: client}
			err := api.ValidateCallbackURL(ctx, stub, invalidURL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid callback scheme \"http\", only https is supported"))
		})
	})

	Context("when the server does not return 204 on GET", func() {
		BeforeEach(func() {
			// Override the test server to return 200 for GET requests.
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
		})

		It("should return an error on reachability check", func() {
			stub := &stubClientProvider{client: client}
			err := api.ValidateCallbackURL(ctx, stub, callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unexpected status code"))
		})
	})
})

var _ = Describe("CheckCallbackReachabilityGET", func() {
	var (
		ctx      context.Context
		callback string
		client   *http.Client
		server   *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Create an HTTPS server that returns 204 for GET requests.
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		callback = server.URL
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	It("should succeed when the server returns 204", func() {
		err := api.CheckCallbackReachabilityGET(ctx, client, callback)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail when the server returns a status other than 204", func() {
		// Update the server handler to return 404 for GET requests.
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		err := api.CheckCallbackReachabilityGET(ctx, client, callback)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unexpected status code"))
	})
})

// stubClientProvider is a simple stub that implements the notifier.ClientProvider interface.
type stubClientProvider struct {
	client *http.Client
}

func (s *stubClientProvider) NewClient(ctx context.Context, callback string) (*http.Client, error) {
	return s.client, nil
}
