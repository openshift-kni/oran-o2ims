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
	"strings"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func checkFakeClientServerSideApplyError(err error) bool {
	// Workaround for error stemming from fake k8s client, which should be revisited once dependencies are upgraded:
	// "apply patches are not supported in the fake client. Follow https://github.com/kubernetes/kubernetes/issues/115598 for the current status"
	return err == nil || strings.Contains(err.Error(), "apply patches are not supported in the fake client")
}

var _ = Describe("Subscription handler", func() {
	Describe("Creation", func() {
		var (
			ctx        context.Context
			fakeClient *k8s.Client
		)

		BeforeEach(func() {
			// Create a context:
			ctx = context.TODO()
			fakeClient = k8s.NewFakeClient()
		})

		It("Needs a subscription type", func() {
			handler, err := NewSubscriptionHandler().
				SetLogger(logger).
				SetGlobalCloudID("123").
				SetKubeClient(fakeClient).
				SetConfigmapName(DefaultInfraInventoryConfigmapName).
				SetNamespace(DefaultNamespace).
				Build(ctx)
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(
				ContainSubstring(
					fmt.Sprintf("subscription type can only be %s or %s",
						SubscriptionIdAlarm,
						SubscriptionIdInfrastructureInventory),
				),
			)
		})

		It("Can't be created without a logger", func() {
			handler, err := NewSubscriptionHandler().
				SetGlobalCloudID("123").
				SetKubeClient(fakeClient).
				SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
				Build(ctx)
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewSubscriptionHandler().
				SetLogger(logger).
				SetKubeClient(fakeClient).
				SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
				SetConfigmapName(DefaultInfraInventoryConfigmapName).
				SetNamespace(DefaultNamespace).
				Build(ctx)
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("cloud identifier"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx        context.Context
			fakeClient *k8s.Client
		)

		BeforeEach(func() {
			// Create a context:
			ctx = context.TODO()
			fakeClient = k8s.NewFakeClient()
			// create fake namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: DefaultNamespace,
				},
			}
			err := fakeClient.Create(ctx, namespace, &client.CreateOptions{}, client.FieldOwner(FieldOwner))
			Expect(err).ToNot(HaveOccurred())
			alarmSubConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: DefaultNamespace,
					Name:      DefaultAlarmConfigmapName,
				},
				Data: nil,
			}
			err = fakeClient.Create(ctx, alarmSubConfigMap, &client.CreateOptions{}, client.FieldOwner(FieldOwner))
			Expect(err).ToNot(HaveOccurred())

			resourceSubConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: DefaultNamespace,
					Name:      DefaultInfraInventoryConfigmapName,
				},
				Data: nil,
			}
			err = fakeClient.Create(ctx, resourceSubConfigMap, &client.CreateOptions{}, client.FieldOwner(FieldOwner))
			Expect(err).ToNot(HaveOccurred())
		})

		Describe("List", func() {

			It("Translates empty list of results", func() {

				// Create the handler:
				handler, err := NewSubscriptionHandler().
					SetLogger(logger).
					SetGlobalCloudID("123").
					SetKubeClient(fakeClient).
					SetSubscriptionIdString(SubscriptionIdAlarm).
					SetConfigmapName(DefaultAlarmConfigmapName).
					SetNamespace(DefaultNamespace).
					Build(ctx)
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
				handler, err := NewSubscriptionHandler().
					SetLogger(logger).
					SetGlobalCloudID("123").
					SetKubeClient(fakeClient).
					SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
					SetConfigmapName(DefaultInfraInventoryConfigmapName).
					SetNamespace(DefaultNamespace).
					Build(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(handler).ToNot(BeNil())

				// pre-populate the subscription map
				obj_1 := data.Object{
					"customerId": "test_customer_id_prime",
				}
				obj_2 := data.Object{
					"customerId": "test_cluster_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}
				req_1 := AddRequest{nil, obj_1}
				req_2 := AddRequest{nil, obj_2}

				subId_1, err := handler.addItem(ctx, req_1)
				if checkFakeClientServerSideApplyError(err) {
					return
				}
				Expect(err).ToNot(HaveOccurred())

				subId_2, err := handler.addItem(ctx, req_2)
				Expect(err).ToNot(HaveOccurred())

				obj_1, err = handler.encodeSubId(subId_1, obj_1)
				Expect(err).ToNot(HaveOccurred())
				obj_2, err = handler.encodeSubId(subId_2, obj_2)
				Expect(err).ToNot(HaveOccurred())

				subIdMap := map[string]data.Object{}
				subIdMap[subId_2] = obj_2
				subIdMap[subId_1] = obj_1

				// subIdArray := maps.Keys(subIdMap)

				// Send the request and verify the result:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())
				Expect(items).To(HaveLen(2))
				id, err := handler.decodeSubId(items[0])
				Expect(err).ToNot(HaveOccurred())
				Expect(items[0]).To(Equal(subIdMap[id]))
				id, err = handler.decodeSubId(items[1])
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
				handler, err := NewSubscriptionHandler().
					SetLogger(logger).
					SetGlobalCloudID("123").
					SetKubeClient(fakeClient).
					SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
					SetConfigmapName(DefaultInfraInventoryConfigmapName).
					SetNamespace(DefaultNamespace).
					Build(ctx)
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
				handler, err := NewSubscriptionHandler().
					SetLogger(logger).
					SetGlobalCloudID("123").
					SetKubeClient(fakeClient).
					SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
					SetConfigmapName(DefaultInfraInventoryConfigmapName).
					SetNamespace(DefaultNamespace).
					Build(ctx)
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
				if checkFakeClientServerSideApplyError(err) {
					return
				}
				Expect(err).ToNot(HaveOccurred())
				obj_1, err = handler.encodeSubId(subId_1, obj_1)
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
			It("Create the subscription handler and add a subscription", func() {
				// Create the handler:
				handler, err := NewSubscriptionHandler().
					SetLogger(logger).
					SetGlobalCloudID("123").
					SetKubeClient(fakeClient).
					SetSubscriptionIdString(SubscriptionIdInfrastructureInventory).
					SetConfigmapName(DefaultInfraInventoryConfigmapName).
					SetNamespace(DefaultNamespace).
					Build(ctx)
				Expect(err).ToNot(HaveOccurred())
				obj := data.Object{
					"customerId": "test_custer_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}

				// add the request
				add_req := AddRequest{nil, obj}
				resp, err := handler.Add(ctx, &add_req)
				if checkFakeClientServerSideApplyError(err) {
					return
				}
				Expect(err).ToNot(HaveOccurred())

				// decode the subId
				sub_id, err := handler.decodeSubId(resp.Object)
				Expect(err).ToNot(HaveOccurred())

				// use Get to verify the addrequest
				get_resp, err := handler.Get(ctx, &GetRequest{
					Variables: []string{sub_id},
				})
				Expect(err).ToNot(HaveOccurred())
				// extract sub_id and verify
				sub_id_get, err := handler.decodeSubId(get_resp.Object)
				Expect(err).ToNot(HaveOccurred())
				Expect(sub_id).To(Equal(sub_id_get))

				// use Delete
				_, err = handler.Delete(ctx, &DeleteRequest{
					Variables: []string{sub_id}})
				Expect(err).ToNot(HaveOccurred())

				// use Get to verify the entry was deleted
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
