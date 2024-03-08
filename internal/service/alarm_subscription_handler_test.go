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

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/data"
)

var _ = Describe("alarm Subscription handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewAlarmSubscriptionHandler().
				SetCloudID("123").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewAlarmSubscriptionHandler().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("cloud identifier"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			// Create a context:
			ctx = context.Background()

		})

		Describe("List", func() {

			It("Translates empty list of results", func() {

				// Create the handler:
				handler, err := NewAlarmSubscriptionHandler().
					SetLogger(logger).
					SetCloudID("123").
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
				// Create the handler:
				handler, err := NewAlarmSubscriptionHandler().
					SetLogger(logger).
					SetCloudID("123").
					Build()
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// pre-populate the subscription map
				obj_1 := data.Object{
					"customerId": "test_customer_id_prime",
				}
				obj_2 := data.Object{
					"customerId": "test_custer_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}
				req_1 := AddRequest{nil, obj_1}
				req_2 := AddRequest{nil, obj_2}

				subId_1, err := handler.addItem(ctx, req_1)
				Expect(err).ToNot(HaveOccurred())

				subId_2, err := handler.addItem(ctx, req_2)
				Expect(err).ToNot(HaveOccurred())

				obj_1, err = handler.encodeSubId(ctx, subId_1, obj_1)
				Expect(err).ToNot(HaveOccurred())
				obj_2, err = handler.encodeSubId(ctx, subId_2, obj_2)
				Expect(err).ToNot(HaveOccurred())

				subIdMap := map[string]data.Object{}
				subIdMap[subId_2] = obj_2
				subIdMap[subId_1] = obj_1

				//subIdArray := maps.Keys(subIdMap)

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(2))
				id, err := handler.decodeSubId(ctx, items[0])
				Expect(err).ToNot(HaveOccurred())
				Expect(items[0]).To(Equal(subIdMap[id]))
				id, err = handler.decodeSubId(ctx, items[1])
				Expect(err).ToNot(HaveOccurred())
				Expect(items[1]).To(Equal(subIdMap[id]))
			})

			/* tbd
			It("Adds configurable extensions", func() {
			}) */
		})

		Describe("Get", func() {
			It("Test Get functions", func() {
				// Create the handler:
				handler, err := NewAlarmSubscriptionHandler().
					SetLogger(logger).
					SetCloudID("123").
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it sends the token, no
				// matter what is the response.
				// Send fake/wrong Id
				resp, err := handler.Get(ctx, &GetRequest{
					Variables: []string{"negtive_test"},
				})
				msg := err.Error()
				Expect(msg).To(Equal("not found"))
				Expect(resp.Object).To(BeEmpty())

			})

			It("Uses the right search id ", func() {
				// Create the handler:
				handler, err := NewAlarmSubscriptionHandler().
					SetLogger(logger).
					SetCloudID("123").
					Build()
				Expect(err).ToNot(HaveOccurred())
				obj_1 := data.Object{
					"customerId": "test_custer_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}
				req_1 := AddRequest{nil, obj_1}

				subId_1, err := handler.addItem(ctx, req_1)
				Expect(err).ToNot(HaveOccurred())
				obj_1, err = handler.encodeSubId(ctx, subId_1, obj_1)
				Expect(err).ToNot(HaveOccurred())

				// Send the request. Note that we ignore the error here because
				// all we care about in this test is that it uses the right URL
				// path, no matter what is the response.
				resp, err := handler.Get(ctx, &GetRequest{
					Variables: []string{subId_1},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Object).To(Equal(obj_1))
			})

		})

		Describe("Add + Delete", func() {
			It("Create the alart subscription and add a subscription", func() {
				// Create the handler:
				handler, err := NewAlarmSubscriptionHandler().
					SetLogger(logger).
					SetCloudID("123").
					Build()
				Expect(err).ToNot(HaveOccurred())
				obj := data.Object{
					"customerId": "test_custer_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}

				//add the request
				add_req := AddRequest{nil, obj}
				resp, err := handler.Add(ctx, &add_req)
				Expect(err).ToNot(HaveOccurred())

				//decode the subId
				sub_id, err := handler.decodeSubId(ctx, resp.Object)
				Expect(err).ToNot(HaveOccurred())

				//use Get to verify the addrequest
				get_resp, err := handler.Get(ctx, &GetRequest{
					Variables: []string{sub_id},
				})
				Expect(err).ToNot(HaveOccurred())
				//extract sub_id and verify
				sub_id_get, err := handler.decodeSubId(ctx, get_resp.Object)
				Expect(err).ToNot(HaveOccurred())
				Expect(sub_id).To(Equal(sub_id_get))

				//use Delete
				_, err = handler.Delete(ctx, &DeleteRequest{
					Variables: []string{sub_id}})
				Expect(err).ToNot(HaveOccurred())

				//use Get to verify the entry was deleted
				get_resp, err = handler.Get(ctx, &GetRequest{
					Variables: []string{sub_id},
				})

				msg := err.Error()
				Expect(msg).To(Equal("not found"))
				Expect(get_resp.Object).To(BeEmpty())
			})

		})
	})
})
