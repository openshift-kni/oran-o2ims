/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
)

func testJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	return fmt.Sprintf("%s.%s.%s", header, body, sig)
}

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
	var logBuffer bytes.Buffer
	var origLogger *slog.Logger

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
		req = http.Request{
			Header: http.Header{},
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/api/test"},
		}
		next = &NoopHandler{}
		recorder = httptest.NewRecorder()
		handler = Authenticator(&oauthAuthenticator, &k8sAuthenticator)(next)

		logBuffer.Reset()
		origLogger = slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	})

	AfterEach(func() {
		slog.SetDefault(origLogger)
	})

	It("Authorizes the request using OAuth behind proxy", func() {
		req.Header.Set("x-forwarded-for", constants.Localhost)
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
		req.Header.Set("x-forwarded-for", constants.Localhost)
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

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring("authentication failed"))
		Expect(logOutput).To(ContainSubstring(`"level":"WARN"`))
		Expect(logOutput).To(ContainSubstring(`"method":"GET"`))
		Expect(logOutput).To(ContainSubstring(`"/api/test"`))
		Expect(logOutput).To(ContainSubstring("some error"))
	})

	It("Rejects the request when the handler returns false", func() {
		k8sAuthenticator.Ok = false
		handler.ServeHTTP(recorder, &req)
		Expect(oauthAuthenticator.called).To(BeFalse())
		Expect(k8sAuthenticator.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeFalse())
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body.String()).To(ContainSubstring("unable to authenticate request"))

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring("authentication rejected"))
		Expect(logOutput).To(ContainSubstring(`"level":"WARN"`))
		Expect(logOutput).To(ContainSubstring(`"method":"GET"`))
		Expect(logOutput).To(ContainSubstring(`"/api/test"`))
	})

	It("Logs clientIp from x-forwarded-for on auth failure", func() {
		req.Header.Set("x-forwarded-for", "10.0.0.1, 192.168.1.1")
		oauthAuthenticator.Error = errors.New("bad token")
		handler.ServeHTTP(recorder, &req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"clientIp":"10.0.0.1"`))
	})

	It("Logs clientIp from RemoteAddr when no x-forwarded-for on auth failure", func() {
		req.RemoteAddr = "172.16.0.5:43210"
		k8sAuthenticator.Error = errors.New("bad token")
		handler.ServeHTTP(recorder, &req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"clientIp":"172.16.0.5:43210"`))
	})

	It("Logs container field on auth failure", func() {
		k8sAuthenticator.Error = errors.New("bad token")
		handler.ServeHTTP(recorder, &req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"container"`))
	})

	It("Logs JWT claims on auth failure with bearer token", func() {
		req.Header.Set("Authorization",
			"Bearer "+testJWT(`{"iss":"https://sso.example.com","aud":"o2ims","exp":1700000000}`))
		k8sAuthenticator.Error = errors.New("expired")
		handler.ServeHTTP(recorder, &req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"issuer":"https://sso.example.com"`))
		Expect(logOutput).To(ContainSubstring(`"audience":"o2ims"`))
		Expect(logOutput).To(ContainSubstring(`"expiration"`))
	})

	It("Logs container and clientIp on auth rejection", func() {
		req.RemoteAddr = "10.20.30.40:9999"
		k8sAuthenticator.Ok = false
		handler.ServeHTTP(recorder, &req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring("authentication rejected"))
		Expect(logOutput).To(ContainSubstring(`"container"`))
		Expect(logOutput).To(ContainSubstring(`"clientIp":"10.20.30.40:9999"`))
	})
})

var _ = Describe("Authorizer", func() {
	var next http.Handler
	var req *http.Request
	var k8sAuthorizer NoopAuthorizer
	var recorder *httptest.ResponseRecorder
	var handler http.Handler
	var logBuffer bytes.Buffer
	var origLogger *slog.Logger

	BeforeEach(func() {
		k8sAuthorizer = NoopAuthorizer{
			Decision: authorizer.DecisionAllow,
			Reason:   "foo",
			Error:    nil,
		}
		req = &http.Request{Header: http.Header{}, Method: http.MethodGet, URL: &url.URL{Path: "/some/path"}}
		req = req.WithContext(request.WithUser(req.Context(), &user.DefaultInfo{
			Name:   "test",
			Groups: []string{"o2ims-admin", "o2ims-reader"},
		}))
		next = &NoopHandler{}
		recorder = httptest.NewRecorder()
		handler = Authorizer(&k8sAuthorizer)(next)

		logBuffer.Reset()
		origLogger = slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	})

	AfterEach(func() {
		slog.SetDefault(origLogger)
	})

	It("Authorizes the request", func() {
		handler.ServeHTTP(recorder, req)
		Expect(k8sAuthorizer.called).To(BeTrue())
		Expect(next.(*NoopHandler).called).To(BeTrue())
	})

	It("Logs groups on successful authorization", func() {
		handler.ServeHTTP(recorder, req)
		Expect(recorder.Code).To(Equal(http.StatusOK))

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring("authorization allowed"))
		Expect(logOutput).To(ContainSubstring(`"level":"DEBUG"`))
		Expect(logOutput).To(ContainSubstring("test"))
		Expect(logOutput).To(ContainSubstring("o2ims-admin"))
		Expect(logOutput).To(ContainSubstring("o2ims-reader"))
		Expect(logOutput).To(ContainSubstring("/some/path"))
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

	It("Logs container and clientIp on authorization error", func() {
		req.RemoteAddr = "10.0.0.50:8080"
		k8sAuthorizer.Error = errors.New("sar failed")
		handler.ServeHTTP(recorder, req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"container"`))
		Expect(logOutput).To(ContainSubstring(`"clientIp":"10.0.0.50:8080"`))
	})

	It("Logs container and clientIp on authorization denied", func() {
		req.RemoteAddr = "10.0.0.60:7070"
		k8sAuthorizer.Decision = authorizer.DecisionNoOpinion
		handler.ServeHTTP(recorder, req)

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"container"`))
		Expect(logOutput).To(ContainSubstring(`"clientIp":"10.0.0.60:7070"`))
	})
})
