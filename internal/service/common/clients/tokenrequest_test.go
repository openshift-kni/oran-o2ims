/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clients

import (
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("TokenRequestTokenSource", func() {
	var (
		fakeClientset *k8sfake.Clientset
		callCount     int
	)

	BeforeEach(func() {
		callCount = 0
		fakeClientset = k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				callCount++
				createAction := action.(k8stesting.CreateAction)
				tokenReq := createAction.GetObject().(*authenticationv1.TokenRequest)

				Expect(tokenReq.Spec.Audiences).To(ContainElement("test-audience"))

				return true, &authenticationv1.TokenRequest{
					Status: authenticationv1.TokenRequestStatus{
						Token:               fmt.Sprintf("token-%d", callCount),
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(10 * time.Minute)),
					},
				}, nil
			})
	})

	It("should create a token with the specified audience", func() {
		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")
		token, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())
		Expect(token.AccessToken).To(Equal("token-1"))
		Expect(token.TokenType).To(Equal("Bearer"))
		Expect(callCount).To(Equal(1))
	})

	It("should cache tokens and return the cached version on subsequent calls", func() {
		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")

		token1, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())

		token2, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())

		Expect(token1.AccessToken).To(Equal(token2.AccessToken))
		Expect(callCount).To(Equal(1))
	})

	It("should refresh expired tokens", func() {
		fakeClientset = k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				callCount++
				return true, &authenticationv1.TokenRequest{
					Status: authenticationv1.TokenRequestStatus{
						Token:               fmt.Sprintf("token-%d", callCount),
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(1 * time.Second)),
					},
				}, nil
			})

		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")

		token1, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())
		Expect(token1.AccessToken).To(Equal("token-1"))

		// The token should be expired almost immediately due to short TTL + 80% refresh
		time.Sleep(1 * time.Second)

		token2, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())
		Expect(token2.AccessToken).To(Equal("token-2"))
		Expect(callCount).To(Equal(2))
	})

	It("should return an error when the API call fails", func() {
		fakeClientset = k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(_ k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("permission denied")
			})

		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")
		_, err := ts.Token()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("permission denied"))
	})

	It("should be safe for concurrent access", func() {
		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")

		var wg sync.WaitGroup
		errs := make([]error, 10)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, errs[idx] = ts.Token()
			}(i)
		}
		wg.Wait()

		for _, err := range errs {
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(callCount).To(Equal(1))
	})

	It("should set ExpirationSeconds in the token request", func() {
		var requestedExpiration *int64
		fakeClientset = k8sfake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "serviceaccounts/token",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				createAction := action.(k8stesting.CreateAction)
				tokenReq := createAction.GetObject().(*authenticationv1.TokenRequest)
				requestedExpiration = tokenReq.Spec.ExpirationSeconds
				return true, &authenticationv1.TokenRequest{
					Status: authenticationv1.TokenRequestStatus{
						Token:               "token-exp",
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(10 * time.Minute)),
					},
				}, nil
			})

		ts := NewTokenRequestTokenSource(fakeClientset, "test-ns", "test-sa", "test-audience")
		_, err := ts.Token()
		Expect(err).ToNot(HaveOccurred())
		Expect(requestedExpiration).ToNot(BeNil())
		Expect(*requestedExpiration).To(Equal(int64(600)))
	})
})
