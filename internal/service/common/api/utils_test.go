// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package api_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateCallbackURL", func() {
	var (
		ctx           context.Context
		callback      string
		client        *http.Client
		server        *httptest.Server
		savedResolver api.Resolver
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

		// Replace the default resolver so the loopback test server address passes target validation
		savedResolver = api.DefaultResolver
		api.DefaultResolver = &mockResolver{addrs: map[string][]string{
			"127.0.0.1": {"203.0.113.1"},
		}}
	})

	AfterEach(func() {
		server.Close()
		api.DefaultResolver = savedResolver
	})

	Context("when the callback URL is valid and reachable", func() {
		It("should not return an error", func() {
			stub := &stubClientProvider{client: client}
			err := api.ValidateCallbackURL(ctx, stub, callback, callback)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when the callback URL has an invalid scheme", func() {
		It("should return an error", func() {
			invalidURL := "http://example.com"
			stub := &stubClientProvider{client: client}
			err := api.ValidateCallbackURL(ctx, stub, invalidURL, "")
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
			err := api.ValidateCallbackURL(ctx, stub, callback, callback)
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
		Expect(err).ToNot(HaveOccurred())
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

var _ = Describe("ValidateCallbackTarget", func() {
	var (
		ctx      context.Context
		resolver *mockResolver
	)

	BeforeEach(func() {
		ctx = context.Background()
		resolver = &mockResolver{addrs: map[string][]string{}}
	})

	Context("cluster-local callbacks", func() {
		It("should allow cluster-local URLs regardless of SMO URL", func() {
			resolver.addrs["my-svc.my-ns.svc.cluster.local"] = []string{"10.0.0.1"}
			err := api.ValidateCallbackTarget(ctx, "https://my-svc.my-ns.svc.cluster.local/cb", "", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow cluster-local URLs when SMO is configured", func() {
			resolver.addrs["my-svc.my-ns.svc.cluster.local"] = []string{"10.0.0.1"}
			err := api.ValidateCallbackTarget(ctx, "https://my-svc.my-ns.svc.cluster.local/cb", "https://smo.example.com", resolver)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("external callbacks", func() {
		It("should allow callback matching SMO host", func() {
			resolver.addrs["smo.example.com"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/callback", "https://smo.example.com", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow callback matching SMO host with different path", func() {
			resolver.addrs["smo.example.com"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/other/path", "https://smo.example.com/api", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow callback matching SMO host with different case (RFC 4343)", func() {
			resolver.addrs["SMO.Example.COM"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://SMO.Example.COM/callback", "https://smo.example.com", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject callback to a different host than SMO", func() {
			resolver.addrs["attacker.com"] = []string{"198.51.100.1"}
			err := api.ValidateCallbackTarget(ctx, "https://attacker.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not match SMO host"))
		})

		It("should reject external callbacks when SMO URL is not configured", func() {
			err := api.ValidateCallbackTarget(ctx, "https://external.example.com/cb", "", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not allowed when SMO URL is not configured"))
		})
	})

	Context("blocked IP ranges", func() {
		It("should reject loopback addresses", func() {
			resolver.addrs["smo.example.com"] = []string{"127.0.0.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked address"))
		})

		It("should reject IPv6 loopback", func() {
			resolver.addrs["smo.example.com"] = []string{"::1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked address"))
		})

		It("should reject link-local addresses (cloud metadata)", func() {
			resolver.addrs["smo.example.com"] = []string{"169.254.169.254"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked address"))
		})

		It("should reject 0.0.0.0/8 addresses", func() {
			resolver.addrs["smo.example.com"] = []string{"0.0.0.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked address"))
		})

		It("should allow public IP addresses", func() {
			resolver.addrs["smo.example.com"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow private IPs for cluster-local callbacks", func() {
			resolver.addrs["svc.ns.svc.cluster.local"] = []string{"10.96.0.1"}
			err := api.ValidateCallbackTarget(ctx, "https://svc.ns.svc.cluster.local/cb", "", resolver)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("edge cases", func() {
		It("should reject callback with no hostname", func() {
			err := api.ValidateCallbackTarget(ctx, "https:///path", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no hostname"))
		})

		It("should reject unresolvable hostname", func() {
			resolver.err = fmt.Errorf("no such host")
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to resolve"))
		})

		It("should reject non-https scheme", func() {
			err := api.ValidateCallbackTarget(ctx, "http://smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only https is supported"))
		})

		It("should handle IPv6 bracket notation in callback URL", func() {
			resolver.addrs["::1"] = []string{"::1"}
			err := api.ValidateCallbackTarget(ctx, "https://[::1]/cb", "https://[::1]", resolver)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked address"))
		})

		It("should allow callback with different port than SMO", func() {
			resolver.addrs["smo.example.com"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://smo.example.com:8443/cb", "https://smo.example.com:443", resolver)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should strip userinfo before comparing hostnames", func() {
			resolver.addrs["smo.example.com"] = []string{"203.0.113.1"}
			err := api.ValidateCallbackTarget(ctx, "https://user:pass@smo.example.com/cb", "https://smo.example.com", resolver)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("DeniedCIDRStrings", func() {
	It("should all be valid CIDR notation", func() {
		for _, cidr := range api.DeniedCIDRStrings {
			_, _, err := net.ParseCIDR(cidr)
			Expect(err).ToNot(HaveOccurred(), "invalid denied CIDR: %s", cidr)
		}
	})
})

// stubClientProvider is a simple stub that implements the notifier.ClientProvider interface.
type stubClientProvider struct {
	client *http.Client
}

func (s *stubClientProvider) NewClient(_ context.Context, _ commonapi.AuthType) (*http.Client, error) {
	return s.client, nil
}

// mockResolver implements api.Resolver for testing.
type mockResolver struct {
	addrs map[string][]string
	err   error
}

func (m *mockResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	addrs, ok := m.addrs[host]
	if !ok {
		return nil, fmt.Errorf("no such host: %s", host)
	}
	return addrs, nil
}
