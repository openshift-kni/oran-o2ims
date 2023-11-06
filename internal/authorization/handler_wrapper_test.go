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

// This file contains tests for the authorization handler.

package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/authentication"
	"github.com/openshift-kni/oran-o2ims/internal/text"
)

var _ = Describe("Handler wapper", func() {
	Describe("Creation", func() {
		It("Can't be built without a logger", func() {
			_, err := NewHandlerWrapper().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
			Expect(err.Error()).To(ContainSubstring("mandatory"))
		})

		It("Can't be built with an ACL file that doesn't exist", func() {
			// Create a file name that doesn't exist:
			dir, err := os.MkdirTemp("", "*.acls")
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.RemoveAll(dir)
				Expect(err).ToNot(HaveOccurred())
			}()
			file := filepath.Join(dir, "bad.yaml")

			// Try to create the wrapper:
			_, err = NewHandlerWrapper().
				AddACLFile(file).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
			Expect(err.Error()).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("Accepts request that matches ACL", func() {
			// Prepare the ACL:
			acl, err := os.CreateTemp("", "acl-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, err = acl.WriteString(text.Dedent(`
				- claim: sub
				  pattern: ^mysubject$
			`))
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.Remove(acl.Name())
				Expect(err).ToNot(HaveOccurred())
			}()
			err = acl.Close()
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddACLFile(acl.Name()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "mysubject",
				Claims: map[string]any{
					"sub": "mysubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is accepted:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Accepts request that matches second of two ACL items", func() {
			// Prepare the ACL:
			acl, err := os.CreateTemp("", "acl-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, err = acl.WriteString(text.Dedent(`
				- claim: sub
				  pattern: ^mysubject$
				- claim: sub
				  pattern: ^yoursubject$
			`))
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.Remove(acl.Name())
				Expect(err).ToNot(HaveOccurred())
			}()
			err = acl.Close()
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddACLFile(acl.Name()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "yoursubject",
				Claims: map[string]any{
					"sub": "yoursubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is rejected:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Accepts request that matches second of two ACL files", func() {
			// Prepare the ACL files:
			dir, err := os.MkdirTemp("", "acls.*")
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.RemoveAll(dir)
				Expect(err).ToNot(HaveOccurred())
			}()
			file1 := filepath.Join(dir, "acl1.yaml")
			err = os.WriteFile(
				file1,
				[]byte(text.Dedent(`
					- claim: sub
					  pattern: ^mysubject$
				`)),
				0600,
			)
			Expect(err).ToNot(HaveOccurred())
			file2 := filepath.Join(dir, "acl2.yaml")
			err = os.WriteFile(
				file2,
				[]byte(text.Dedent(`
					- claim: sub
					  pattern: ^yoursubject$
				`)),
				0600,
			)
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddACLFile(file1).
				AddACLFile(file2).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "yoursubject",
				Claims: map[string]any{
					"sub": "yoursubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is accepted:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Accepts request for private path if there is no ACL", func() {
			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "mysubject",
				Claims: map[string]any{
					"sub": "mysubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is accepted:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Accepts request for public path if there are ACL items but none matches", func() {
			// Prepare the ACL:
			acl, err := os.CreateTemp("", "acl-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, err = acl.WriteString(text.Dedent(`
				- claim: sub
				  pattern: ^mysubject$
			`))
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.Remove(acl.Name())
				Expect(err).ToNot(HaveOccurred())
			}()
			err = acl.Close()
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddPublicPath("^/public(/.*)?$").
				AddACLFile(acl.Name()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "yoursubject",
				Claims: map[string]any{
					"sub": "yoursubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/public", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is rejected:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Doesn't need a subject in the context for public paths", func() {
			// Prepare the ACL:
			acl, err := os.CreateTemp("", "acl-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, err = acl.WriteString(text.Dedent(`
				- claim: sub
				  pattern: ^mysubject$
			`))
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.Remove(acl.Name())
				Expect(err).ToNot(HaveOccurred())
			}()
			err = acl.Close()
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddPublicPath("^/public(/.*)?$").
				AddACLFile(acl.Name()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/public", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is accepted:
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("Rejects request for private path if there are ACL items but none matches", func() {
			// Prepare the ACL:
			acl, err := os.CreateTemp("", "acl-*.yaml")
			Expect(err).ToNot(HaveOccurred())
			_, err = acl.WriteString(text.Dedent(`
				- claim: sub
				  pattern: ^mysubject$
			`))
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := os.Remove(acl.Name())
				Expect(err).ToNot(HaveOccurred())
			}()
			err = acl.Close()
			Expect(err).ToNot(HaveOccurred())

			// Prepare the next handler:
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap the handler:
			wrapper, err := NewHandlerWrapper().
				SetLogger(logger).
				AddACLFile(acl.Name()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			handler = wrapper(handler)

			// Send the request:
			subject := &authentication.Subject{
				Name: "yoursubject",
				Claims: map[string]any{
					"sub": "yoursubject",
				},
			}
			ctx := authentication.ContextWithSubject(ctx, subject)
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Verify that the request is rejected:
			Expect(recorder.Code).To(Equal(http.StatusForbidden))
		})
	})
})
