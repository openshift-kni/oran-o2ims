/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clients

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

var _ = Describe("NewBackend", func() {
	const (
		serverName = "test-server"
		urlEnvName = "TEST_SERVER_URL"
		callerSA   = "test-caller"
	)

	var (
		fakeClientset *k8sfake.Clientset
		wantNamespace string
	)

	BeforeEach(func() {
		wantNamespace = constants.DefaultNamespace
		fakeClientset = k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				createAction := action.(k8stesting.CreateAction)
				Expect(createAction.GetNamespace()).To(Equal(wantNamespace))
				Expect(action.(k8stesting.CreateActionImpl).Name).To(Equal(callerSA))
				tokenReq := createAction.GetObject().(*authenticationv1.TokenRequest)
				Expect(tokenReq.Spec.Audiences).To(ConsistOf(serverName))

				return true, &authenticationv1.TokenRequest{
					Status: authenticationv1.TokenRequestStatus{
						Token:               "caller-token",
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(10 * time.Minute)),
					},
				}, nil
			})
	})

	It("defaults to the in-cluster service URL", func() {
		GinkgoT().Setenv(urlEnvName, "")

		backend, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, WithTransport(http.DefaultTransport))
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.URL).To(Equal(ctlrutils.GetServiceURL(serverName)))
	})

	It("uses the URL from the environment variable when it is set", func() {
		GinkgoT().Setenv(urlEnvName, "https://localhost:9000")

		backend, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, WithTransport(http.DefaultTransport))
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.URL).To(Equal("https://localhost:9000"))
	})

	It("sends a token minted for the caller with the server as audience", func(ctx SpecContext) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(server.Close)
		GinkgoT().Setenv(urlEnvName, server.URL)

		backend, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, WithTransport(http.DefaultTransport))
		Expect(err).ToNot(HaveOccurred())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.Auth.Editor(ctx, req)).To(Succeed())

		resp, err := backend.HTTPClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		Expect(gotAuth).To(Equal("Bearer caller-token"))
	})

	It("mints the token in the operator namespace when it is not the default", func(ctx SpecContext) {
		GinkgoT().Setenv(constants.DefaultNamespaceEnvName, "custom-ns")
		wantNamespace = "custom-ns"

		backend, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, WithTransport(http.DefaultTransport))
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.URL).To(ContainSubstring(serverName + ".custom-ns."))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.Auth.Editor(ctx, req)).To(Succeed())
		Expect(req.Header.Get("Authorization")).To(Equal("Bearer caller-token"))
	})

	It("uses the transport passed with WithTransport", func() {
		rt := &http.Transport{}

		backend, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, WithTransport(rt))
		Expect(err).ToNot(HaveOccurred())
		Expect(backend.HTTPClient.Transport).To(BeIdenticalTo(rt))
	})

	It("returns an error when the transport cannot be created", func() {
		failingTransport := WithTransportFactory(func() (http.RoundTripper, error) { return nil, errors.New("no CA bundle") })

		_, err := NewBackend(fakeClientset, serverName, urlEnvName, callerSA, failingTransport)
		Expect(err).To(MatchError(ContainSubstring("no CA bundle")))
	})
})
