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
	"github.com/onsi/gomega/ghttp"
	. "github.com/onsi/gomega/ghttp"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

var _ = Describe("Deployment manager handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewDeploymentManagerHandler().
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
			handler, err := NewDeploymentManagerHandler().
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
			handler, err := NewDeploymentManagerHandler().
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
			handler, err := NewDeploymentManagerHandler().
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
		)

		BeforeEach(func() {
			// Create a context:
			ctx = context.Background()

			// Create the backend server:
			backend = MakeTCPServer()
		})

		AfterEach(func() {
			backend.Close()
		})

		// RespondWithList creates a handler that responds with the given search results.
		var RespondWithList = func(items ...data.Object) http.HandlerFunc {
			return ghttp.RespondWithJSONEncoded(http.StatusOK, data.Object{
				"apiVersion": "v1",
				"kind":       "List",
				"items":      items,
			})
		}

		Describe("List", func() {
			It("Uses the configured token", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyHeaderKV("Authorization", "Bearer my-token"),
						RespondWithList(),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it sends the token, no
				// matter what is the result.
				_, _ = handler.List(ctx, &ListRequest{})
			})

			It("Uses the right path", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyRequest(http.MethodGet, "/global-hub-api/v1/managedclusters"),
						RespondWithList(),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// Send the request. Note that we ignore the error here because
				// all we care about here in this test is that it uses the right
				// URL path, no matter what is the result.
				_, _ = handler.List(ctx, &ListRequest{})
			})

			It("Translates empty list of results", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					RespondWithList(),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(BeEmpty())
			})

			It("Translates non empty list of results", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithList(
							data.Object{
								"metadata": data.Object{
									"name": "my-cluster",
									"labels": data.Object{
										"clusterID": "123",
									},
								},
								"spec": data.Object{
									"managedClusterClientConfigs": data.Array{
										data.Object{
											"url": "https://my-cluster:6443",
										},
									},
								},
							},
							data.Object{
								"metadata": data.Object{
									"name": "your-cluster",
									"labels": data.Object{
										"clusterID": "456",
									},
								},
								"spec": data.Object{
									"managedClusterClientConfigs": data.Array{
										data.Object{
											"url": "https://your-cluster:6443",
										},
									},
								},
							},
						),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(2))
				Expect(items[0]).To(Equal(data.Object{
					"deploymentManagerId": "123",
					"description":         "my-cluster",
					"name":                "my-cluster",
					"oCloudId":            "123",
					"serviceUri":          "https://my-cluster:6443",
				}))
				Expect(items[1]).To(Equal(data.Object{
					"deploymentManagerId": "456",
					"description":         "your-cluster",
					"name":                "your-cluster",
					"oCloudId":            "123",
					"serviceUri":          "https://your-cluster:6443",
				}))
			})

			It("Adds configurable extensions", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithList(data.Object{
							"metadata": data.Object{
								"name": "my-cluster",
								"labels": data.Object{
									"clusterID": "123",
									"country":   "ES",
								},
								"annotations": data.Object{
									"region": "Madrid",
								},
							},
							"spec": data.Object{
								"managedClusterClientConfigs": data.Array{
									data.Object{
										"url": "https://my-cluster:6443",
									},
								},
							},
						}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					SetExtensions(
						`{
							"country": .metadata.labels["country"],
							"region": .metadata.annotations["region"]
						}`,
						`{
							"fixed": 123
						}`).
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0]).To(MatchJQ(`.extensions.country`, "ES"))
				Expect(items[0]).To(MatchJQ(`.extensions.region`, "Madrid"))
				Expect(items[0]).To(MatchJQ(`.extensions.fixed`, 123))
			})
		})

		Describe("Get", func() {
			It("Uses the configured token", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyHeaderKV("Authorization", "Bearer my-token"),
						RespondWithObject(data.Object{}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it sends the token, no
				// matter what is the response.
				_, _ = handler.Get(ctx, &GetRequest{
					ID: "123",
				})
			})

			It("Uses the right path", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyRequest(http.MethodGet, "/global-hub-api/v1/managedclusters"),
						RespondWithObject(data.Object{}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it uses the right URL
				// path, no matter what is the response.
				_, _ = handler.Get(ctx, &GetRequest{
					ID: "123",
				})
			})

			It("Uses the right label selector", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyFormKV("labelSelector", "clusterID=123"),
						RespondWithObject(data.Object{}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it uses the right URL
				// path, no matter what is the response.
				_, _ = handler.Get(ctx, &GetRequest{
					ID: "123",
				})
			})

			It("Uses the right label limit", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						VerifyFormKV("limit", "1"),
						RespondWithObject(data.Object{}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it uses the right URL
				// path, no matter what is the response.
				_, _ = handler.Get(ctx, &GetRequest{
					ID: "123",
				})
			})

			It("Translates result", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithList(data.Object{
							"metadata": data.Object{
								"name": "my-cluster",
								"labels": data.Object{
									"clusterID": "123",
								},
							},
							"spec": data.Object{
								"managedClusterClientConfigs": data.Array{
									data.Object{
										"url": "https://my-cluster:6443",
									},
								},
							},
						}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request and verify the result:
				response, err := handler.Get(ctx, &GetRequest{
					ID: "123",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Object).To(Equal(data.Object{
					"deploymentManagerId": "123",
					"description":         "my-cluster",
					"name":                "my-cluster",
					"oCloudId":            "123",
					"serviceUri":          "https://my-cluster:6443",
				}))
			})

			It("Adds configurable extensions", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithList(data.Object{
							"metadata": data.Object{
								"name": "my-cluster",
								"labels": data.Object{
									"clusterID": "123",
									"country":   "ES",
								},
								"annotations": data.Object{
									"region": "Madrid",
								},
							},
							"spec": data.Object{
								"managedClusterClientConfigs": data.Array{
									data.Object{
										"url": "https://my-cluster:6443",
									},
								},
							},
						}),
					),
				)

				// Create the handler:
				handler, err := NewDeploymentManagerHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					SetExtensions(
						`{
							"country": .metadata.labels["country"],
							"region": .metadata.annotations["region"]
						}`,
						`{
							"fixed": 123
						}`).
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request and verify the result:
				response, err := handler.Get(ctx, &GetRequest{
					ID: "123",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Object).To(MatchJQ(`.extensions.country`, "ES"))
				Expect(response.Object).To(MatchJQ(`.extensions.region`, "Madrid"))
				Expect(response.Object).To(MatchJQ(`.extensions.fixed`, 123))
			})
		})
	})
})
