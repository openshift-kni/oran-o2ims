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
	"github.com/openshift-kni/oran-o2ims/internal/search"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

var _ = Describe("Alarm handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewAlarmHandler().
				SetCloudID("123").
				SetBackendURL("https://my-backend:6443").
				SetBackendToken("my-token").
				SetResourceServerURL("https://resource-server:8003").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewAlarmHandler().
				SetLogger(logger).
				SetBackendURL("https://my-backend:6443").
				SetBackendToken("my-token").
				SetResourceServerURL("https://resource-server:8003").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("cloud identifier"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend URL", func() {
			handler, err := NewAlarmHandler().
				SetLogger(logger).
				SetCloudID("123").
				SetBackendToken("my-token").
				SetResourceServerURL("https://resource-server:8003").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("backend URL"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a backend token", func() {
			handler, err := NewAlarmHandler().
				SetLogger(logger).
				SetCloudID("123").
				SetBackendURL("https://my-backend:6443").
				SetResourceServerURL("https://resource-server:8003").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("backend token"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a resource server URL", func() {
			handler, err := NewAlarmHandler().
				SetLogger(logger).
				SetCloudID("123").
				SetBackendToken("my-token").
				SetBackendURL("https://my-backend:6443").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("resource server URL"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx            context.Context
			backend        *Server
			resourceServer *Server
			handler        *AlarmHandler
		)

		BeforeEach(func() {
			var err error

			// Create a context:
			ctx = context.Background()

			// Create the backend server:
			backend = MakeTCPServer()

			// Create the resource server:
			resourceServer = MakeTCPServer()

			// Create the handler:
			handler, err = NewAlarmHandler().
				SetLogger(logger).
				SetCloudID("123").
				SetBackendURL(backend.URL()).
				SetBackendToken("my-token").
				SetResourceServerURL(resourceServer.URL()).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(handler).ToNot(BeNil())
		})

		AfterEach(func() {
			backend.Close()
			resourceServer.Close()
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

		// RespondWithList creates a handler that responds with the given search results.
		var RespondWithList = func(items ...data.Object) http.HandlerFunc {
			return ghttp.RespondWithJSONEncoded(http.StatusOK, items)
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
							"labels": data.Object{
								"alertname":       "alert1",
								"severity":        "warning",
								"managed_cluster": "spoke0",
							},
							"annotations": data.Object{
								"summary":     "alert1_summary",
								"description": "alert1_description",
							},
							"startsAt":  "00:00",
							"updatedAt": "01:00",
						},
						data.Object{
							"labels": data.Object{
								"alertname":       "alert2",
								"severity":        "info",
								"managed_cluster": "spoke0",
								"instance":        "host0",
							},
							"annotations": data.Object{
								"summary":     "alert2_summary",
								"description": "alert2_description",
							},
							"startsAt":  "00:00",
							"updatedAt": "01:00",
						},
					),
				)

				// Prepare the resource server:
				resourceServer.RouteToHandler(http.MethodGet, "/resourcePools",
					RespondWithList(
						data.Object{
							"name":           "spoke0",
							"resourcePoolId": "spoke0",
						},
					),
				)
				resourceServer.RouteToHandler(http.MethodGet, "/resourcePools/spoke0/resources",
					RespondWithList(
						data.Object{
							"resourceID":     "host0",
							"resourceTypeID": "type0",
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
				Expect(items[0]).To(MatchJQ(`.alarmEventRecordId`, "alert1_spoke0"))
				Expect(items[0]).To(MatchJQ(`.resourceID`, "spoke0"))
				Expect(items[0]).To(MatchJQ(`.resourceTypeID`, ""))
				Expect(items[0]).To(MatchJQ(`.alarmRaisedTime`, "00:00"))
				Expect(items[0]).To(MatchJQ(`.alarmChangedTime`, "01:00"))
				Expect(items[0]).To(MatchJQ(`.alarmDefinitionID`, "alert1"))
				Expect(items[0]).To(MatchJQ(`.probableCauseID`, "alert1"))
				Expect(items[0]).To(MatchJQ(`.perceivedSeverity`, "WARNING"))
				Expect(items[0]).To(MatchJQ(`.extensions.summary`, "alert1_summary"))
				Expect(items[0]).To(MatchJQ(`.extensions.description`, "alert1_description"))

				// Verify seconds result:
				Expect(items[1]).To(MatchJQ(`.alarmEventRecordId`, "alert2_spoke0_host0"))
				Expect(items[1]).To(MatchJQ(`.resourceID`, "host0"))
				Expect(items[1]).To(MatchJQ(`.resourceTypeID`, "type0"))
				Expect(items[1]).To(MatchJQ(`.alarmRaisedTime`, "00:00"))
				Expect(items[1]).To(MatchJQ(`.alarmChangedTime`, "01:00"))
				Expect(items[1]).To(MatchJQ(`.alarmDefinitionID`, "alert2"))
				Expect(items[1]).To(MatchJQ(`.probableCauseID`, "alert2"))
				Expect(items[1]).To(MatchJQ(`.perceivedSeverity`, "MINOR"))
				Expect(items[1]).To(MatchJQ(`.extensions.summary`, "alert2_summary"))
				Expect(items[1]).To(MatchJQ(`.extensions.description`, "alert2_description"))
			})

			It("Accepts a filter", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Selector: &search.Selector{
						Terms: []*search.Term{{
							Operator: search.Eq,
							Path: []string{
								"alarmDefinitionID",
							},
							Values: []any{
								"ClusterNotUpgradeable",
							},
						}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify filters
				Expect(handler.alarmFetcher.filters).To(ContainElement(
					"alertname=ClusterNotUpgradeable",
				))
			})

			It("Accepts multiple filters", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Selector: &search.Selector{
						Terms: []*search.Term{
							{
								Operator: search.Eq,
								Path: []string{
									"alarmDefinitionID",
								},
								Values: []any{
									"ClusterNotUpgradeable",
								},
							},
							{
								Operator: search.Neq,
								Path: []string{
									"probableCauseID",
								},
								Values: []any{
									"NodeClockNotSynchronising",
								},
							},
						},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify filters
				Expect(handler.alarmFetcher.filters).To(ContainElement(
					"alertname=ClusterNotUpgradeable",
				))
				Expect(handler.alarmFetcher.filters).To(ContainElement(
					"alertname!=NodeClockNotSynchronising",
				))
			})

			It("Accepts a filter by extension", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Selector: &search.Selector{
						Terms: []*search.Term{{
							Operator: search.Eq,
							Path: []string{
								"extensions",
								"severity",
							},
							Values: []any{
								"critical",
							},
						}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify filters
				Expect(handler.alarmFetcher.filters).To(ContainElement(
					"severity=critical",
				))
			})

			It("No filter for unknown property", func() {
				// Prepare the backend:
				backend.AppendHandlers(
					RespondWithItems(),
				)

				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Selector: &search.Selector{
						Terms: []*search.Term{{
							Operator: search.Eq,
							Path: []string{
								"unknown",
							},
							Values: []any{
								"value",
							},
						}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify filters
				Expect(handler.alarmFetcher.filters).To(HaveLen(0))
			})

			It("Adds configurable extensions", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithItems(
							data.Object{
								"labels": data.Object{
									"alertname":       "alert",
									"severity":        "warning",
									"managed_cluster": "spoke0",
								},
								"annotations": data.Object{
									"summary":     "alert_summary",
									"description": "alert_description",
								},
								"startsAt":  "00:00",
								"updatedAt": "01:00",
							},
						),
					),
				)

				// Prepare the resource server:
				resourceServer.AppendHandlers(
					CombineHandlers(
						RespondWithList(
							data.Object{
								"name":           "spoke0",
								"resourcePoolId": "spoke0",
							},
						),
					),
				)

				// Create the handler:
				handler, err := NewAlarmHandler().
					SetLogger(logger).
					SetCloudID("123").
					SetBackendURL(backend.URL()).
					SetBackendToken("my-token").
					SetResourceServerURL(resourceServer.URL()).
					SetExtensions(
						`{
							"cluster": .labels.managed_cluster,
						}`,
						`{
							"fixed": 123
						}`).
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request and verify the result:
				response, err := handler.Get(ctx, &GetRequest{
					Variables: []string{"alert_spoke0"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Object).To(MatchJQ(`.extensions.cluster`, "spoke0"))
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
					Variables: []string{"alert_spoke0"},
				})
			})

			It("Translates result", func() {
				// Prepare a backend:
				backend.AppendHandlers(
					CombineHandlers(
						RespondWithItems(
							data.Object{
								"labels": data.Object{
									"alertname":       "alert",
									"severity":        "warning",
									"managed_cluster": "spoke0",
								},
								"annotations": data.Object{
									"summary":     "alert_summary",
									"description": "alert_description",
								},
								"startsAt":  "00:00",
								"updatedAt": "01:00",
							},
						),
					),
				)

				// Prepare the resource server:
				resourceServer.AppendHandlers(
					CombineHandlers(
						RespondWithList(
							data.Object{
								"name":           "spoke0",
								"resourcePoolId": "spoke0",
							},
						),
					),
				)

				// Send the request:
				response, err := handler.Get(ctx, &GetRequest{
					Variables: []string{"alert_spoke0"},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				// Verify the result:
				Expect(response.Object).To(MatchJQ(`.alarmEventRecordId`, "alert_spoke0"))
				Expect(response.Object).To(MatchJQ(`.resourceID`, "spoke0"))
				Expect(response.Object).To(MatchJQ(`.resourceTypeID`, ""))
				Expect(response.Object).To(MatchJQ(`.alarmRaisedTime`, "00:00"))
				Expect(response.Object).To(MatchJQ(`.alarmChangedTime`, "01:00"))
				Expect(response.Object).To(MatchJQ(`.alarmDefinitionID`, "alert"))
				Expect(response.Object).To(MatchJQ(`.probableCauseID`, "alert"))
				Expect(response.Object).To(MatchJQ(`.perceivedSeverity`, "WARNING"))
			})
		})
	})
})
