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

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/ghttp"
	"k8s.io/utils/ptr"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
	"github.com/openshift-kni/oran-o2ims/internal/text"
)

var _ = Describe("Resource handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewResourceHandler().
				SetBackendURL("https://my-backend:6443").
				SetBackendToken("my-token").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend URL", func() {
			handler, err := NewResourceHandler().
				SetLogger(logger).
				SetBackendToken("my-token").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("backend URL"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend token", func() {
			handler, err := NewResourceHandler().
				SetLogger(logger).
				SetBackendURL("https://my-backend:6443").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("backend token"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx     context.Context
			backend *Server
			handler *ResourceHandler
		)

		BeforeEach(func() {
			var err error

			// Create a context:
			ctx = context.Background()

			// Create the backend server:
			backend = MakeTCPServer()

			// Create the handler:
			handler, err = NewResourceHandler().
				SetLogger(logger).
				SetBackendURL(backend.URL()).
				SetBackendToken("my-token").
				SetGraphqlQuery(text.Dedent(`
					query ($input: [SearchInput]) {
						searchResult: search(input: $input) {
							items,
						}
					}
				`)).
				SetGraphqlVars(&model.SearchInput{
					Filters: []*model.SearchFilter{
						{
							Property: "kind",
							Values: []*string{
								ptr.To("node"),
							},
						},
					},
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(handler).ToNot(BeNil())
		})

		AfterEach(func() {
			backend.Close()
		})

		// RespondWithItems creates a handler that responds with the given search results.
		var RespondWithItems = func(items ...data.Object) http.HandlerFunc {
			return RespondWithObject(data.Object{
				"data": data.Object{
					"searchResult": data.Array{
						data.Object{
							"items": items,
						},
					},
				},
			})
		}

		Describe("List", func() {
			It("Uses the configured token", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyHeaderKV("Authorization", "Bearer my-token"),
						RespondWithItems(),
					),
				)

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it sends the token, no
				// matter what is the result.
				_, _ = handler.List(ctx, &ListRequest{
					Variables: []string{"0", "123"},
				})
			})

			It("Translates empty list of results", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{
					Variables: []string{"0", "123"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(BeEmpty())
			})

			It("Translates non empty list of results", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(
						data.Object{
							"cluster":      "0",
							"label":        "a=b; c=d",
							"name":         "my-node-0",
							"_systemUUID":  "node-0-system-uuid",
							"_uid":         "node-0-global-uuid",
							"architecture": "amd64",
							"cpu":          "8",
							"kind":         "Node",
						},
						data.Object{
							"cluster":      "1",
							"label":        "a=b; c=d",
							"name":         "my-node-1",
							"_systemUUID":  "node-1-system-uuid",
							"_uid":         "node-1-global-uuid",
							"architecture": "amd64",
							"cpu":          "8",
							"kind":         "Node",
						},
					),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Variables: []string{"0", "123"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(2))

				// Verify first result:
				Expect(items[0]).To(MatchJQ(`.resourceId`, "node-0-system-uuid"))
				Expect(items[0]).To(MatchJQ(`.description`, "my-node-0"))
				Expect(items[0]).To(MatchJQ(`.globalAssetId`, "node-0-global-uuid"))
				Expect(items[0]).To(MatchJQ(`.description`, "my-node-0"))
				Expect(items[0]).To(MatchJQ(`.resourcePoolId`, "0"))

				// Verify second result:
				Expect(items[1]).To(MatchJQ(`.resourceId`, "node-1-system-uuid"))
				Expect(items[1]).To(MatchJQ(`.description`, "my-node-1"))
				Expect(items[1]).To(MatchJQ(`.globalAssetId`, "node-1-global-uuid"))
				Expect(items[1]).To(MatchJQ(`.description`, "my-node-1"))
				Expect(items[1]).To(MatchJQ(`.resourcePoolId`, "1"))
			})

			It("Accepts a filter", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Variables: []string{
						"my-pool",
					},
					Selector: &search.Selector{
						Terms: []*search.Term{{
							Operator: search.Eq,
							Path: []string{
								"resourcePoolId",
							},
							Values: []any{
								"spoke0",
							},
						}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify GraphQL filters:
				Expect(handler.resourceFetcher.graphqlVars.Filters).To(HaveLen(4))
				Expect(handler.resourceFetcher.graphqlVars.Filters).To(ContainElement(
					&model.SearchFilter{
						Property: "cluster",
						Values:   []*string{ptr.To("=spoke0")},
					},
				))
			})

			It("Accepts multiple filters", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Variables: []string{
						"my-pool",
					},
					Selector: &search.Selector{
						Terms: []*search.Term{
							{
								Operator: search.Eq,
								Path: []string{
									"resourcePoolId",
								},
								Values: []any{
									"spoke0",
								},
							},
							{
								Operator: search.Neq,
								Path: []string{
									"description",
								},
								Values: []any{
									"node0",
								},
							},
						},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify GraphQL filters:
				Expect(handler.resourceFetcher.graphqlVars.Filters).To(HaveLen(5))
				Expect(handler.resourceFetcher.graphqlVars.Filters).To(ContainElements(
					&model.SearchFilter{
						Property: "cluster",
						Values:   []*string{ptr.To("=spoke0")},
					},
					&model.SearchFilter{
						Property: "name",
						Values:   []*string{ptr.To("!=node0")},
					},
				))
			})

			It("Ignore invalid filters", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Variables: []string{
						"my-pool",
					},
					Selector: &search.Selector{
						Terms: []*search.Term{
							{
								Operator: search.Cont,
								Path: []string{
									"resourcePoolId",
								},
								Values: []any{
									"spoke0",
								},
							},
							{
								Operator: search.In,
								Path: []string{
									"description",
								},
								Values: []any{
									"node0",
								},
							},
						},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify GraphQL filters:
				// (3 filters are added by default)
				Expect(handler.resourceFetcher.graphqlVars.Filters).To(HaveLen(3))
			})

			It("Adds configurable extensions", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithItems(
							data.Object{
								"cluster":      "0",
								"label":        "os=linux; arch=amd64",
								"name":         "my-node-0",
								"_uid":         "node-0-uuid",
								"_systemUUID":  "node-0-system-uuid",
								"architecture": "amd64",
								"cpu":          "8",
								"kind":         "Node",
							},
						),
					),
				)

				// Create the handler:
				handler, err := NewResourceHandler().
					SetLogger(logger).
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					SetGraphqlQuery(text.Dedent(`
						query ($input: [SearchInput]) {
							searchResult: search(input: $input) {
								items,
							}
						}
					`)).
					SetGraphqlVars(&model.SearchInput{
						Filters: []*model.SearchFilter{
							{
								Property: "kind",
								Values: []*string{
									ptr.To("node"),
								},
							},
						},
					}).
					SetExtensions(
						`{
							"operation_system": .label|parse_labels|.os,
							"architecture": .label|parse_labels|.arch
						}`,
						`{
							"fixed": 123
						}`).
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request and verify the result:
				response, err := handler.Get(ctx, &GetRequest{
					Variables: []string{"0", "1"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Object).To(MatchJQ(`.extensions.operation_system`, "linux"))
				Expect(response.Object).To(MatchJQ(`.extensions.architecture`, "amd64"))
				Expect(response.Object).To(MatchJQ(`.extensions.fixed`, 123))
			})
		})

		Describe("Get", func() {
			It("Uses the configured token", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyHeaderKV("Authorization", "Bearer my-token"),
						RespondWithItems(),
					),
				)

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it sends the token, no
				// matter what is the response.
				_, _ = handler.Get(ctx, &GetRequest{
					Variables: []string{"0", "123"},
				})
			})

			It("Translates result", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithItems(
							data.Object{
								"cluster":      "0",
								"label":        "a=b; c=d",
								"name":         "my-node-0",
								"_uid":         "node-0-uuid",
								"_systemUUID":  "node-0-system-uuid",
								"architecture": "amd64",
								"cpu":          "8",
								"kind":         "Node",
							},
						),
					),
				)

				// Send the request:
				response, err := handler.Get(ctx, &GetRequest{
					Variables: []string{"0", "123"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify the result:
				Expect(response.Object).To(MatchJQ(`.resourceId`, "node-0-system-uuid"))
				Expect(response.Object).To(MatchJQ(`.description`, "my-node-0"))
				Expect(response.Object).To(MatchJQ(`.resourcePoolId`, "0"))
				Expect(response.Object).To(MatchJQ(`.globalAssetId`, "node-0-uuid"))
			})
		})
	})
})
