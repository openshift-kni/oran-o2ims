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
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
	"github.com/openshift-kni/oran-o2ims/internal/text"
)

var _ = Describe("Resource type handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewResourceTypeHandler().
				SetCloudID("123").
				SetBackendURL("https://my-backend:6443").
				SetBackendToken("my-token").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewResourceTypeHandler().
				SetLogger(logger).
				SetBackendURL("https://my-backend:6443").
				SetBackendToken("my-token").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("cloud identifier"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend URL", func() {
			handler, err := NewResourceTypeHandler().
				SetLogger(logger).
				SetCloudID("123").
				SetBackendToken("my-token").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("backend URL"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend token", func() {
			handler, err := NewResourceTypeHandler().
				SetLogger(logger).
				SetCloudID("123").
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
			handler *ResourceTypeHandler
		)

		BeforeEach(func() {
			var err error

			// Create a context:
			ctx = context.Background()

			// Create the backend server:
			backend = MakeTCPServer()

			// Create the handler:
			handler, err = NewResourceTypeHandler().
				SetLogger(logger).
				SetCloudID("123").
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
								ptr.To("cluster"),
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
				_, _ = handler.List(ctx, &ListRequest{})
			})

			It("Translates empty list of results", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
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
							"architecture": "amd64",
							"cpu":          "8",
							"kind":         "Node",
						},
						data.Object{
							"architecture": "arm64",
							"cpu":          "4",
							"kind":         "Node",
						},
					),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(2))

				// Verify first result:
				Expect(items[0]).To(MatchJQ(`.resourceTypeID`, "node_8_cores_amd64"))
				Expect(items[0]).To(MatchJQ(`.name`, "node_8_cores_amd64"))
				Expect(items[0]).To(MatchJQ(`.resourceKind`, "PHYSICAL"))
				Expect(items[0]).To(MatchJQ(`.resourceClass`, "COMPUTE"))

				// Verify second result:
				Expect(items[1]).To(MatchJQ(`.resourceTypeID`, "node_4_cores_arm64"))
				Expect(items[1]).To(MatchJQ(`.name`, "node_4_cores_arm64"))
				Expect(items[1]).To(MatchJQ(`.resourceKind`, "PHYSICAL"))
				Expect(items[1]).To(MatchJQ(`.resourceClass`, "COMPUTE"))
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
					ID: "123",
				})
			})

			It("Translates result", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithItems(
							data.Object{
								"architecture": "amd64",
								"cpu":          "8",
								"kind":         "Node",
							},
						),
					),
				)

				// Send the request:
				response, err := handler.Get(ctx, &GetRequest{
					ID: "node_8_cores_amd64",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify the result:
				Expect(response.Object).To(MatchJQ(`.resourceTypeID`, "node_8_cores_amd64"))
				Expect(response.Object).To(MatchJQ(`.name`, "node_8_cores_amd64"))
				Expect(response.Object).To(MatchJQ(`.resourceKind`, "PHYSICAL"))
				Expect(response.Object).To(MatchJQ(`.resourceClass`, "COMPUTE"))
			})
		})
	})
})
