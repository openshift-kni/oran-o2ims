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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/mux"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

var _ = Describe("Adapter", func() {
	var ctrl *gomock.Controller

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(func() {
			ctrl.Finish()
		})
	})

	Describe("Creation", func() {
		It("Can be created with a logger, a variable and a handler", func() {
			handler := NewMockHandler(ctrl)
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(adapter).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			handler := NewMockHandler(ctrl)
			adapter, err := NewAdapter().
				SetHandler(handler).
				SetPathVariables("id").
				Build()
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
			Expect(adapter).To(BeNil())
		})

		It("Can't be created without at least one path variable", func() {
			handler := NewMockHandler(ctrl)
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetHandler(handler).
				Build()
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("variable"))
			Expect(msg).To(ContainSubstring("required"))
			Expect(adapter).To(BeNil())
		})

		It("Can't be created without a handler", func() {
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				Build()
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("handler"))
			Expect(msg).To(ContainSubstring("mandatory"))
			Expect(adapter).To(BeNil())
		})
	})

	It("Serves collection or item according to path variables", func() {
		// Prepare the handler:
		handler := NewMockHandler(ctrl)
		collectionDo := func(ctx context.Context,
			request *ListRequest) (response *ListResponse, err error) {
			Expect(request.Variables).To(BeEmpty())
			response = &ListResponse{
				Items: data.Null(),
			}
			return
		}
		handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(collectionDo)
		itemDo := func(ctx context.Context,
			request *GetRequest) (response *GetResponse, err error) {
			Expect(request.Variables[0]).To(Equal("123"))
			response = &GetResponse{
				Object: data.Object{},
			}
			return
		}
		handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(itemDo)

		// Create the adapter:
		adapter, err := NewAdapter().
			SetLogger(logger).
			SetPathVariables("id").
			SetHandler(handler).
			Build()
		Expect(err).ToNot(HaveOccurred())
		router := mux.NewRouter()
		router.Handle("/mycollection", adapter)
		router.Handle("/mycollection/{id}", adapter)

		By("Serving collection request")
		request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		By("Serving item request")
		request = httptest.NewRequest(http.MethodGet, "/mycollection/123", nil)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
	})

	It("Supports sub-collections", func() {
		// Prepare the handler:
		handler := NewMockHandler(ctrl)
		collectionDo := func(ctx context.Context,
			request *ListRequest) (response *ListResponse, err error) {
			Expect(request.Variables).To(Equal([]string{"123"}))
			response = &ListResponse{
				Items: data.Null(),
			}
			return
		}
		handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(collectionDo)
		itemDo := func(ctx context.Context,
			request *GetRequest) (response *GetResponse, err error) {
			Expect(request.Variables).To(Equal([]string{"456", "123"}))
			response = &GetResponse{
				Object: data.Object{},
			}
			return
		}
		handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(itemDo)

		// Create the adapter:
		adapter, err := NewAdapter().
			SetLogger(logger).
			SetPathVariables("parentID", "childID").
			SetHandler(handler).
			Build()
		Expect(err).ToNot(HaveOccurred())
		router := mux.NewRouter()
		router.Handle("/myparent/{parentID}/mychild", adapter)
		router.Handle("/myparent/{parentID}/mychild/{childID}", adapter)

		By("Serving collection request")
		request := httptest.NewRequest(http.MethodGet, "/myparent/123/mychild", nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		By("Serving item request")
		request = httptest.NewRequest(http.MethodGet, "/myparent/123/mychild/456", nil)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
	})

	Describe("Collection selection", func() {
		It("Accepts simple selector", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Selector).To(Equal(&search.Selector{
					Terms: []*search.Term{{
						Operator: search.Eq,
						Path: []string{
							"myattr",
						},
						Values: []any{
							"myvalue",
						},
					}},
				}))
				response = &ListResponse{
					Items: data.Null(),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection?filter=(eq,myattr,myvalue)",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
		})

		It("Accepts multiple filters", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Selector).To(Equal(&search.Selector{
					Terms: []*search.Term{
						{
							Operator: search.Eq,
							Path: []string{
								"myattr",
							},
							Values: []any{
								"myvalue",
							},
						},
						{
							Operator: search.Neq,
							Path: []string{
								"yourattr",
							},
							Values: []any{
								"yourvalue",
							},
						},
					},
				}))
				response = &ListResponse{
					Items: data.Null(),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection?filter=(eq,myattr,myvalue)&filter=(neq,yourattr,yourvalue)",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
		})

		It("Accepts no filter", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Selector).To(BeNil())
				response = &ListResponse{
					Items: data.Null(),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Run the adapter:
			request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
		})

		It("Rejects incorrect filter", func() {
			// Prepare the handler, but don't expect any call as the filter error will
			// be detected before calling the handler.
			handler := NewMockHandler(ctrl)

			// Prepare the request and response:
			request := httptest.NewRequest(http.MethodGet, "/mycollection?filter=junk", nil)
			recorder := httptest.NewRecorder()

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Run the adapter:
			adapter.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Collection projection", func() {
		It("Accepts projector with one field", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr"},
				}))
				Expect(request.Projector.Exclude).To(BeEmpty())
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection?fields=myattr",
				nil,
			)
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Send the request:
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue"
			}]`))
		})

		It("Accepts projector with two fields", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr"},
					{"yourattr"},
				}))
				Expect(request.Projector.Exclude).To(BeEmpty())
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection?fields=myattr,yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue",
				"yourattr": "yourvalue"
			}]`))
		})

		It("Accepts projector with two path segments", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				Expect(request.Projector.Include).To(Equal([]search.Path{
					{"myattr", "yourattr"},
				}))
				Expect(request.Projector.Exclude).To(BeEmpty())
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr": data.Object{
								"yourattr":  "yourvalue",
								"theirattr": "theirvalue",
							},
							"morestuff": 123,
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection?fields=myattr/yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": {
					"yourattr": "yourvalue"
				}
			}]`))
		})

		It("Accepts request without projector", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue",
				"yourattr": "yourvalue"
			}]`))
		})

		It("Removes default excluded fields when no query parameter is used", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetExcludeFields("yourattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue"
			}]`))
		})

		It("Removes explicitly excluded parameter", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection?exclude_fields=yourattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue"
			}]`))
		})

		It("Doesn't remove default excluded parameter if others are explicitly excluded", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr":   "myvalue",
							"yourattr": "yourvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection?exclude_fields=yourattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetExcludeFields("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{
				"myattr": "myvalue"
			}]`))
		})

		It("Removes default excluded field even if it is also included by default", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr": "myvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetIncludeFields("myattr").
				SetExcludeFields("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{}]`))
		})

		It("Removes explicitly excluded field even if it is also explicitly included", func() {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: data.Pour(
						data.Object{
							"myattr": "myvalue",
						},
					),
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection?fields=myattr&exclude_fields=myattr", nil)
			recorder := httptest.NewRecorder()
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			adapter.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`[{}]`))
		})
	})

	Describe("Object projection", func() {
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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Send the request:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?fields=myattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?fields=myattr,yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?fields=myattr/yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection/123", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetExcludeFields("yourattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection/123", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?exclude_fields=yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetExcludeFields("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?exclude_fields=yourattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				SetIncludeFields("myattr").
				SetExcludeFields("myattr").
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(http.MethodGet, "/mycollection/123", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

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
			handler := NewMockHandler(ctrl)
			handler.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection/{id}", adapter)

			// Send the request:
			request := httptest.NewRequest(
				http.MethodGet,
				"/mycollection/123?fields=myattr&exclude_fields=myattr",
				nil,
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			// Verify the response:
			Expect(recorder.Body).To(MatchJSON(`{}`))
		})
	})

	DescribeTable(
		"JSON generation",
		func(items data.Stream, expected string) {
			// Prepare the handler:
			body := func(ctx context.Context,
				request *ListRequest) (response *ListResponse, err error) {
				response = &ListResponse{
					Items: items,
				}
				return
			}
			handler := NewMockHandler(ctrl)
			handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

			// Prepare the request and response:
			request := httptest.NewRequest(http.MethodGet, "/mycollection", nil)
			recorder := httptest.NewRecorder()

			// Create the adapter:
			adapter, err := NewAdapter().
				SetLogger(logger).
				SetPathVariables("id").
				SetHandler(handler).
				Build()
			Expect(err).ToNot(HaveOccurred())
			router := mux.NewRouter()
			router.Handle("/mycollection", adapter)

			// Run the adapter:
			router.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))
			Expect(recorder.Body).To(MatchJSON(expected))
		},
		Entry(
			"No items",
			data.Null(),
			`[]`,
		),
		Entry(
			"One item with one field",
			data.Pour(data.Object{
				"myattr": "myvalue",
			}),
			`[{
				"myattr": "myvalue"
			}]`,
		),
		Entry(
			"One item with two fields",
			data.Pour(data.Object{
				"myattr":   "myvalue",
				"yourattr": 123,
			}),
			`[{
				"myattr": "myvalue",
				"yourattr": 123
			}]`,
		),
		Entry(
			"Two items with one field each",
			data.Pour(
				data.Object{
					"myattr": "myvalue1",
				},
				data.Object{
					"myattr": "myvalue2",
				},
			),
			`[
				{ "myattr": "myvalue1" },
				{ "myattr": "myvalue2" }
			]`,
		),
		Entry(
			"Two items with two fields each",
			data.Pour(
				data.Object{
					"myattr":   "myvalue1",
					"yourattr": 123,
				},
				data.Object{
					"myattr":   "myvalue2",
					"yourattr": 456,
				},
			),
			`[
				{
					"myattr": "myvalue1",
					"yourattr": 123
				},
				{
					"myattr": "myvalue2",
					"yourattr": 456
				}
			]`,
		),
	)

	// The intent of this test is to ensure that generating large number of items doesn't
	// exhaust memory, thanks to the streaming approach. But it is too slow to run it by
	// default: it takes approxitely 15 minutes.
	It("Supports large number of items", func() {
		Skip("Too slow to run by default")

		// Create an object results in approximately 300 bytes of JSON:
		object := data.Object{}
		for i := 0; i < 10; i++ {
			name := fmt.Sprintf("my_attr_%d", i)
			value := fmt.Sprintf("my_value_%d", i)
			object[name] = value
		}

		// Prepare the handler that will return one billion copies of the object.
		// That will be roughtly 280 GiB.
		body := func(ctx context.Context,
			request *ListRequest) (response *ListResponse, err error) {
			response = &ListResponse{
				Items: data.Repeat(object, 1_000_000_000),
			}
			return
		}
		handler := NewMockHandler(ctrl)
		handler.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(body)

		// To avoid flooding the Ginkgo output we need to create a logger that
		// discards messages:
		logger, err := logging.NewLogger().
			SetWriter(io.Discard).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the adapter:
		adapter, err := NewAdapter().
			SetLogger(logger).
			SetPathVariables("id").
			SetHandler(handler).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Prepare the server. Note that we can't use an HTTP test recorder for this
		// because it saves the response body in memory, and that would exhaust. Instead
		// of that we start an HTTP server and will consume the response body explicitly.
		server := httptest.NewServer(adapter)
		defer server.Close()

		// Send the request:
		response, err := http.Get(server.URL)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := response.Body.Close()
			Expect(err).ToNot(HaveOccurred())
		}()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		Expect(response.Header.Get("Content-Type")).To(Equal("application/json"))
		written, err := io.Copy(io.Discard, response.Body)
		Expect(err).ToNot(HaveOccurred())

		// Check that this resulted in at least 208 GiB:
		Expect(written).To(BeNumerically(">", 280*1<<30))
	})
})
