/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

var _ = Describe("Object adapter", func() {
	var ctrl *gomock.Controller

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(func() {
			ctrl.Finish()
		})
	})

	Describe("Creation", func() {
		It("Can be created with a logger and a handler", func() {
			handler := NewMockObjectHandler(ctrl)
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(adapter).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			handler := NewMockObjectHandler(ctrl)
			adapter, err := NewObjectAdapter().
				SetHandler(handler).
				Build()
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
			Expect(adapter).To(BeNil())
		})

		It("Can't be created without a handler", func() {
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("handler"))
			Expect(msg).To(ContainSubstring("mandatory"))
			Expect(adapter).To(BeNil())
		})
	})

	Describe("Projection", func() {
		It("Accepts projector with one field", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr"},
				}))
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mypath?fields=myattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue"
			}`))
		})

		It("Accepts projector with two fields", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr"},
					{"yourattr"},
				}))
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mypath?fields=myattr,yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue",
				"yourattr": "yourvalue"
			}`))
		})

		It("Accepts projector with two path segments", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr", "yourattr"},
				}))
				response = &GetResponse{
					Object: data.Object{
						"myattr": data.Object{
							"yourattr":  "yourvalue",
							"theirattr": "theirvalue",
						},
						"morestuff": 123,
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mypath?fields=myattr/yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": {
					"yourattr": "yourvalue"
				}
			}`))
		})

		It("Accepts request without projector", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue",
				"yourattr": "yourvalue"
			}`))
		})

		It("Removes default excluded fields when no query parameter is used", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				SetDefaultExclude("yourattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue"
			}`))
		})

		It("Removes explicitly excluded parameter", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath?exclude_fields=yourattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue"
			}`))
		})

		It("Doesn't remove default excluded parameter if others are explicitly excluded", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr":   "myvalue",
						"yourattr": "yourvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath?exclude_fields=yourattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				SetDefaultExclude("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{
				"myattr": "myvalue"
			}`))
		})

		It("Removes default excluded field even if it is also included by default", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr": "myvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				SetDefaultInclude("myattr").
				SetDefaultExclude("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{}`))
		})

		It("Removes explicitly excluded field even if it is also explicitly included", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *GetRequest) (response *GetResponse, err error) {
				response = &GetResponse{
					Object: data.Object{
						"myattr": "myvalue",
					},
				}
				return
			}
			handler := NewMockObjectHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mypath?fields=myattr&exclude_fields=myattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewObjectAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{}`))
		})
	})
})
