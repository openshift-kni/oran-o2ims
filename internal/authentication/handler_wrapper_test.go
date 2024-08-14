/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

// This file contains tests for the authentication handler.

package authentication

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v4"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/ghttp"

	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

const bearerPrefix = "Bearer "

var _ = Describe("Handler wapper", func() {
	It("Can't be built without a logger", func() {
		_, err := NewHandlerWrapper().
			AddKeysFile(keysFile).
			Build()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("logger"))
		Expect(err.Error()).To(ContainSubstring("mandatory"))
	})

	It("Can't be built with a keys file that doesn't exist", func() {
		_, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile("/does/not/exist").
			Build()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("/does/not/exist"))
	})

	It("Can't be built with a malformed keys URL", func() {
		_, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL("junk").
			Build()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("junk"))
	})

	It("Can't be built with a URL that isn't HTTPS", func() {
		_, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL("http://example.com/.well-known/jwks.json").
			Build()
		Expect(err).To(HaveOccurred())
	})

	It("Can be built with one keys file", func() {
		_, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Can be built with one keys URL", func() {
		_, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL("https://example.com/.well-known/jwks.json").
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Rejects request without authorization header for private path", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Wrap the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Request doesn't contain the authorization header"
		}`))
	})

	It("Rejects bad authorization type", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Wrap the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with a bad type and a good token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", "Bad "+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Authentication type 'Bad' isn't supported"
		}`))
	})

	It("Rejects bad bearer token", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Wrap the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with a bad type and a good token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", "Bearer bad")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token is malformed"
		}`))
	})

	It("Rejects expired bearer token", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the expired token:
		bearer := MakeTokenString("Bearer", -1*time.Hour)

		// Wrap the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the expired token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token is expired"
		}`))
	})

	It("Accepts token without `typ` claim", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare a token without the 'typ' claim:
		bearer := MakeTokenObject(jwt.MapClaims{
			"typ": nil,
		}).Raw

		// Wrap the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is accepted:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Rejects `typ` claim with incorrect type", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare a refresh token:
		bearer := MakeTokenObject(jwt.MapClaims{
			"typ": 123,
		}).Raw

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token type claim contains incorrect string value '123'"
		}`))
	})

	It("Rejects refresh tokens", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare a refresh token:
		bearer := MakeTokenString("Refresh", 1*time.Hour)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token type 'Refresh' isn't allowed"
		}`))
	})

	It("Rejects offline tokens", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare an offline access token:
		bearer := MakeTokenString("Offline", 0)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token type 'Offline' isn't allowed"
		}`))
	})

	It("Rejects token without issue date", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the token without the 'iat' claim:
		token := MakeTokenObject(jwt.MapClaims{
			"typ": "Bearer",
			"iat": nil,
		})
		bearer := token.Raw

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token doesn't contain required claim 'iat'"
		}`))
	})

	It("Rejects token without expiration date", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the token without the 'exp' claim:
		token := MakeTokenObject(jwt.MapClaims{
			"typ": "Bearer",
			"exp": nil,
		})
		bearer := token.Raw

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token doesn't contain required claim 'exp'"
		}`))
	})

	It("Rejects token issued in the future", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare a token issued in the future:
		now := time.Now()
		iat := now.Add(1 * time.Minute)
		exp := iat.Add(1 * time.Minute)
		token := MakeTokenObject(jwt.MapClaims{
			"typ": "Bearer",
			"iat": iat.Unix(),
			"exp": exp.Unix(),
		})
		bearer := token.Raw

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with the bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token was issued in the future"
		}`))
	})

	It("Rejects token that isn't valid yet", func() {
		// Prepare the next handler, which should not be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the not yet valid token:
		iat := time.Now()
		nbf := iat.Add(1 * time.Minute)
		exp := nbf.Add(1 * time.Minute)
		token := MakeTokenObject(jwt.MapClaims{
			"typ": "Bearer",
			"iat": iat.Unix(),
			"nbf": nbf.Unix(),
			"exp": exp.Unix(),
		})
		bearer := token.Raw

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request with a bad token:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token isn't valid yet"
		}`))
	})

	It("Loads keys from file", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Adds token to the request context", func() {
		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).ToNot(BeNil())
			Expect(subject.Token).To(Equal(bearer))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Adds subject to the request context", func() {
		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).ToNot(BeNil())
			Expect(subject.Name).To(Equal("mysubject"))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Doesn't require authorization header for public URL", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			AddPublicPath("^/public(/.*)?$").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request without the authorization header:
		request := httptest.NewRequest(http.MethodGet, "/public", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Ignores malformed authorization header for public URL", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			AddPublicPath("^/public(/.*)?$").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/public", nil)
		request.Header.Set("Authorization", "Bad junk")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
	})

	It("Ignores expired token for public URL", func() {
		// Prepare the expired token:
		bearer := MakeTokenString("Bearer", -1*time.Minute)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			AddPublicPath("^/public(/.*)?$").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/public", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
	})

	It("Combines multiple public URLs", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			AddPublicPath("^/public(/.*)?$").
			AddPublicPath("^/open(/.*)?$").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send a request for one of the public URLs:
		request := httptest.NewRequest(http.MethodGet, "/public", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusOK))

		// Send a request for another of the public URLs:
		request = httptest.NewRequest(http.MethodGet, "/open", nil)
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Passes authenticated subject to next handler for public path", func() {
		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).ToNot(BeNil())
			Expect(subject.Name).To(Equal("mysubject"))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			AddPublicPath("^/public(/.*)?$").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/public", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Doesn't load insecure keys by default", func() {
		var err error

		// Prepare the server:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err = os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(
			RespondWith(http.StatusOK, keysBytes),
		)
		server.SetAllowUnhandledRequests(true)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
	})

	It("Loads insecure keys in insecure mode", func() {
		var err error

		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err = os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(
			RespondWith(http.StatusOK, keysBytes),
		)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysInsecure(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Returns the response of the next handler", func() {
		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{
				"myfield": "myvalue"
			}`))
			Expect(err).ToNot(HaveOccurred())
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send a request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
		Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(recorder.Body).To(MatchJSON(`{
			"myfield": "myvalue"
		}`))
	})

	It("Returns expected headers", func() {
		// Prepare the next handler, which should never be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", "Bearer junk")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		header := recorder.Header().Get("WWW-Authenticate")
		Expect(header).To(Equal("Bearer realm=\"O2IMS\""))
	})

	It("Honours explicit realm", func() {
		// Prepare the next handler, which should never be called:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(true).To(BeFalse())
			w.WriteHeader(http.StatusBadRequest)
		})

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			SetRealm("my_realm").
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Check that the response code contains the realm:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", "Bearer junk")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Header().Get("WWW-Authenticate")).To(Equal(`Bearer realm="my_realm"`))
	})

	It("Accepts token expired within the configured tolerance", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare a token that expired 5 minutes ago:
		bearer := MakeTokenString("Bearer", -5*time.Minute)

		// Prepare a handler that tolerates tokens that expired up to 10 minutes ago:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			SetTolerance(10 * time.Minute).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Rejects token expired outside the configured tolerance", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare a token that expired 15 minutes ago:
		bearer := MakeTokenString("Bearer", -15*time.Minute)

		// Prepare a handler that tolerates tokens that have expired up to 10 minutes ago:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysFile(keysFile).
			SetTolerance(10 * time.Minute).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
	})

	It("Accepts all requests if no keys are configured", func() {
		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := SubjectFromContext(r.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
			w.WriteHeader(http.StatusOK)
		})

		// Prepare a handler that has no keys, so will not perform accept all requests:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify the response:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Uses token to load keys", func() {
		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err := os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(CombineHandlers(
			VerifyHeaderKV("Authorization", "Bearer mykeystoken"),
			RespondWith(http.StatusOK, keysBytes),
		))

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysToken("mykeystoken").
			SetKeysInsecure(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Uses token file to load keys", func() {
		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err := os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(CombineHandlers(
			VerifyHeaderKV("Authorization", "Bearer mykeystoken"),
			RespondWith(http.StatusOK, keysBytes),
		))

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the token file:
		fd, err := os.CreateTemp("", "*.token")
		Expect(err).ToNot(HaveOccurred())
		file := fd.Name()
		defer func() {
			err := os.Remove(file)
			Expect(err).ToNot(HaveOccurred())
		}()
		err = fd.Close()
		Expect(err).ToNot(HaveOccurred())
		err = os.WriteFile(file, []byte("mykeystoken"), 0o600)
		Expect(err).ToNot(HaveOccurred())

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysTokenFile(file).
			SetKeysInsecure(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Uses token to load keys if token file doesn't exist", func() {
		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err := os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(CombineHandlers(
			VerifyHeaderKV("Authorization", "Bearer mykeystoken"),
			RespondWith(http.StatusOK, keysBytes),
		))

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare a token file that doesn't exist:
		dir, err := os.MkdirTemp("", "*.tokens")
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := os.RemoveAll(dir)
			Expect(err).ToNot(HaveOccurred())
		}()
		file := filepath.Join(dir, "bad.token")

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysToken("mykeystoken").
			SetKeysTokenFile(file).
			SetKeysInsecure(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusOK))
	})

	It("Rejects if token is required for loading keys and none has been configured", func() {
		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err := os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(CombineHandlers(
			RespondWith(http.StatusUnauthorized, nil),
		))

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("Shouldn't be called")
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysInsecure(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token can't be verified"
		}`))
	})

	It("Rejects if token is required for loading keys token file doesn't exist", func() {
		// Prepare the server that will return the keys:
		server, ca := MakeTCPTLSServer()
		defer func() {
			server.Close()
			err := os.Remove(ca)
			Expect(err).ToNot(HaveOccurred())
		}()
		server.AppendHandlers(
			RespondWith(http.StatusUnauthorized, nil),
		)

		// Prepare the next handler:
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Fail("Shouldn't be called")
		})

		// Prepare the token:
		bearer := MakeTokenString("Bearer", 1*time.Minute)

		// Prepare a token file that doesn't exist:
		dir, err := os.MkdirTemp("", "*.tokens")
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := os.RemoveAll(dir)
			Expect(err).ToNot(HaveOccurred())
		}()
		file := filepath.Join(dir, "bad.token")

		// Prepare the handler:
		wrapper, err := NewHandlerWrapper().
			SetLogger(logger).
			AddKeysURL(server.URL()).
			SetKeysInsecure(true).
			SetKeysTokenFile(file).
			Build()
		Expect(err).ToNot(HaveOccurred())
		handler = wrapper(handler)

		// Send the request:
		request := httptest.NewRequest(http.MethodGet, "/private", nil)
		request.Header.Set("Authorization", bearerPrefix+bearer)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		// Verify that the request is rejected:
		Expect(recorder.Code).To(Equal(http.StatusUnauthorized))
		Expect(recorder.Body).To(MatchJSON(`{
			"status": 401,
			"detail": "Bearer token can't be verified"
		}`))
	})
})
