/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
)

type NoopAuthenticator struct {
	called   bool
	Response *authenticator.Response
	Ok       bool
	Error    error
}

func (a *NoopAuthenticator) AuthenticateRequest(_ *http.Request) (*authenticator.Response, bool, error) {
	a.called = true
	return a.Response, a.Ok, a.Error
}

type NoopAuthorizer struct {
	called   bool
	Decision authorizer.Decision
	Reason   string
	Error    error
}

func (a *NoopAuthorizer) Authorize(_ context.Context, _ authorizer.Attributes) (authorizer.Decision, string, error) {
	a.called = true
	return a.Decision, a.Reason, a.Error
}

type NoopHandler struct {
	called  bool
	request http.Request
}

func (h *NoopHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.request = *r
	w.WriteHeader(http.StatusOK)
}

var _ = Describe("Authenticator", func() {
	var next http.Handler
	var req http.Request
	var oauthAuthenticator, k8sAuthenticator NoopAuthenticator
	var recorder *httptest.ResponseRecorder
	var handler http.Handler

	BeforeEach(func() {
		oauthAuthenticator = NoopAuthenticator{
			Response: &authenticator.Response{
				User: &user.DefaultInfo{
					Name:   "test1",
					UID:    "1234",
					Groups: nil,
				},
			},
			Ok:    true,
			Error: nil,
		}
		k8sAuthenticator = NoopAuthenticator{
			Response: &authenticator.Response{
				User: &user.DefaultInfo{
					Name:   "test2",
					UID:    "5678",
					Groups: nil,
				},
			},
			Ok:    true,
			Error: nil,
		}
		req = http.Request{Header: http.Header{}}
		next = &NoopHandler{}
		recorder = httptest.NewRecorder()
		handler = Authenticator(&oauthAuthenticator, &k8sAuthenticator)(next)
	})

	It("Authorizes the request using OAuth behind proxy", func() {
		req.Header.Set("x-forwarded-for", "127.0.0.1")
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeTrue())
		Expect(k8sAuthenticator.called).To(BeFalse())
		Expect(next.(*NoopHandler).called).To(BeTrue())
		Expect(recorder.Code).To(Equal(http.StatusOK))
		user, ok := request.UserFrom(next.(*NoopHandler).request.Context())
		Expect(ok).To(BeTrue())
		Expect(user.GetName()).To(Equal("test1"))
	})

	It("Authorizes the request using Kubernetes behind proxy when no OAuth handler provided", func() {
		req.Header.Set("x-forwarded-for", "127.0.0.1")
		handler = Authenticator(nil, &k8sAuthenticator)(next)
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeFalse())
		Expect(k8sAuthenticator.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeTrue())
		Expect(recorder.Code).To(Equal(http.StatusOK))
		user, ok := request.UserFrom(next.(*NoopHandler).request.Context())
		Expect(ok).To(BeTrue())
		Expect(user.GetName()).To(Equal("test2"))
	})

	It("Authorizes the request using Kubernetes when not behind proxy", func() {
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeFalse())
		Expect(k8sAuthenticator.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeTrue())
		Expect(recorder.Code).To(Equal(http.StatusOK))
		user, ok := request.UserFrom(next.(*NoopHandler).request.Context())
		Expect(ok).To(BeTrue())
		Expect(user.GetName()).To(Equal("test2"))
	})

	It("Fails the request when the handler returns an error", func() {
		k8sAuthenticator.Error = errors.New("some error")
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeFalse())
		Expect(k8sAuthenticator.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeFalse())
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body.String()).To(ContainSubstring("some error"))
		Expect(recorder.Body.String()).To(ContainSubstring("failed to authenticate request"))
	})

	It("Rejects the request when the handler returns false", func() {
		k8sAuthenticator.Ok = false
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeFalse())
		Expect(k8sAuthenticator.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeFalse())
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body.String()).To(ContainSubstring("unable to authenticate request"))
	})
})

var _ = Describe("Authorizer", func() {
	var next http.Handler
	var req *http.Request
	var k8sAuthorizer NoopAuthorizer
	var recorder *httptest.ResponseRecorder
	var handler http.Handler

	BeforeEach(func() {
		k8sAuthorizer = NoopAuthorizer{
			Decision: authorizer.DecisionAllow,
			Reason:   "foo",
			Error:    nil,
		}
		req = &http.Request{Header: http.Header{}, Method: http.MethodGet, URL: &url.URL{Path: "/some/path"}}
		req = req.WithContext(request.WithUser(req.Context(), &user.DefaultInfo{Name: "test"}))
		next = &NoopHandler{}
		recorder = httptest.NewRecorder()
		handler = Authorizer(&k8sAuthorizer)(next)
	})

	It("Authorizes the request", func() {
		handler.ServeHTTP(recorder, req)
		Expect(k8sAuthorizer.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeTrue())
	})

	It("Fails the request if User not in context", func() {
		req = &http.Request{Header: http.Header{}, Method: http.MethodGet, URL: &url.URL{Path: "/some/path"}}
		handler.ServeHTTP(recorder, req)
		Expect(k8sAuthorizer.called).To(BeFalse())
		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		Expect(recorder.Body.String()).To(ContainSubstring("user not in context"))
		Expect(next.(*NoopHandler).called).To(BeFalse())
	})

	It("Fails the request if handler returns an error", func() {
		k8sAuthorizer.Error = errors.New("some error")
		handler.ServeHTTP(recorder, req)
		Expect(k8sAuthorizer.called).To(BeTrue())
		Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
		Expect(recorder.Body.String()).To(ContainSubstring("Authorization for user 'test' failed"))
		Expect(next.(*NoopHandler).called).To(BeFalse())
	})

	It("Rejects the request if handler returns an error", func() {
		k8sAuthorizer.Decision = authorizer.DecisionNoOpinion
		handler.ServeHTTP(recorder, req)
		Expect(k8sAuthorizer.called).To(BeTrue())
		Expect(recorder.Code).To(Equal(http.StatusForbidden))
		Expect(recorder.Body.String()).To(ContainSubstring("Authorization not allowed for user 'test'"))
		Expect(next.(*NoopHandler).called).To(BeFalse())
	})
})
