/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clusterserver

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

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

var _ = Describe("NewClient", func() {
	const callerSA = "test-caller"

	It("calls the cluster server with a token for the caller", func(ctx SpecContext) {
		var gotAudiences []string
		var gotAccount string
		fakeClientset := k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				gotAccount = action.(k8stesting.CreateActionImpl).Name
				gotAudiences = action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenRequest).Spec.Audiences
				return true, &authenticationv1.TokenRequest{
					Status: authenticationv1.TokenRequestStatus{
						Token:               "caller-token",
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(10 * time.Minute)),
					},
				}, nil
			})

		var gotPath, gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		DeferCleanup(server.Close)
		GinkgoT().Setenv(URLEnvName, server.URL)

		c, err := NewClient(fakeClientset, callerSA, clients.WithTransport(http.DefaultTransport))
		Expect(err).ToNot(HaveOccurred())

		resp, err := c.GetNodeClustersWithResponse(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))
		Expect(gotPath).To(Equal("/O2ims_infrastructureCluster/v1/nodeClusters"))
		Expect(gotAuth).To(Equal("Bearer caller-token"))
		Expect(gotAccount).To(Equal(callerSA))
		Expect(gotAudiences).To(ConsistOf(ctlrutils.InventoryClusterServerName))
	})

	It("returns an error when the cluster server connection cannot be prepared", func() {
		failingTransport := clients.WithTransportFactory(func() (http.RoundTripper, error) {
			return nil, errors.New("no CA bundle")
		})

		c, err := NewClient(k8sfake.NewSimpleClientset(), callerSA, failingTransport)
		Expect(err).To(MatchError(ContainSubstring("failed to prepare cluster server connection")))
		Expect(err).To(MatchError(ContainSubstring("no CA bundle")))
		Expect(c).To(BeNil())
	})
})
