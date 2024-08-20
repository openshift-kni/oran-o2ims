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
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
)

var _ = Describe("alarm Notification handler", func() {
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

		It("Can't be created without a logger", func() {
			handler, err := NewAlarmNotificationHandler().
				SetCloudID("123").
				SetKubeClient(fakeClient).
				SetConfigmapName(DefaultAlarmConfigmapName).
				SetNamespace(DefaultNamespace).
				Build(ctx)
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewAlarmNotificationHandler().
				SetLogger(logger).
				SetKubeClient(fakeClient).
				SetConfigmapName(DefaultAlarmConfigmapName).
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
		})

		Describe("Synch from persistent storage and test filter ", func() {
			It("Build alarm notification server searcher and verify subId set size", func() {
				// Create the handler:
				handler, err := NewAlarmNotificationHandler().
					SetLogger(logger).
					SetKubeClient(fakeClient).
					SetConfigmapName(DefaultAlarmConfigmapName).
					SetNamespace(DefaultNamespace).
					SetCloudID("123").
					SetResourceServerToken("testToken").
					SetResourceServerURL("testURL").
					Build(ctx)
				Expect(err).ToNot(HaveOccurred())

				obj1 := data.Object{
					"consumerSubscriptionId": "ef74487e-81ea-40ce-8b48-899b6310c3a7",
					"filter":                 "(eq,resourceID,my-host)",
					"callback":               "https://my-smo.example.com/host-alarms",
				}
				obj2 := data.Object{
					"consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
					"filter":                 "(eq,extensions/country,US);(in,perceivedSeverity,CRITICAL,MAJOR)",
					"callback":               "https://my-smo.example.com/country-alarms",
				}

				newMap := map[string]data.Object{
					"subId-1": obj1,
					"subId-2": obj2,
				}

				//validate the prebuild indexes go through
				Expect(handler.assignSubscriptionMap(&newMap)).ToNot(HaveOccurred())

				requestObj := data.Object{
					"alarmEventRecordId": "a267bbd0-57aa-4ea1-b030-a300d420ef19",
					"resourceTypeID":     "c1fe0c43-28e3-4b61-aac5-84bea67551ea",
					"resourceID":         "my-host",
					"alarmDefinitionID":  "4db97698-e612-430a-9520-c00e214c39e1",
					"probableCauseID":    "4a02fdab-e135-4919-b60c-96af08bd088b",
					"alarmRaisedTime":    "2012-04-23T18:25:43.511Z",
					"perceivedSeverity":  "CRITICAL",
					"extensions": data.Object{
						"country": "US",
					},
				}

				//validate the subId set size(expected filter matched number correct)
				subIdSet := handler.getSubscriptionIdsFromAlarm(ctx, requestObj)
				Expect(len(subIdSet) == 2)

				//validate add/post request go through
				add_req := AddRequest{nil, requestObj}
				_, err = handler.add(ctx, &add_req)
				Expect(err).To(HaveOccurred())

				//packet does not match any subscription filters
				requestObj2 := data.Object{
					"alarmRaisedTime":   "2024-06-11T11:08:56.160Z",
					"alarmChangedTime":  "2024-06-11T11:13:26.937Z",
					"alarmDefinitionID": "NodeClockNotSynchronising",
					"perceivedSeverity": "CRITICAL",
					"extensions": data.Object{
						"namespace":                 "openshift-monitoring",
						"pod":                       "node-exporter-r7vfl",
						"container":                 "kube-rbac-proxy",
						"instance":                  "ostest-extraworker-1",
						"summary":                   "Clock not synchronising.",
						"runbook_url":               "https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/NodeClockNotSynchronising.md",
						"service":                   "node-exporter",
						"severity":                  "critical",
						"alertname":                 "NodeClockNotSynchronising",
						"openshift_io_alert_source": "platform",
						"prometheus":                "openshift-monitoring/k8s",
						"managed_cluster":           "e98f27b4-b09b-47e1-8d4a-d2b3630a0b37",
						"description":               "Clock at ostest-extraworker-1 is not synchronising. Ensure NTP is configured on this host.",
						"job":                       "node-exporter",
						"endpoint":                  "https",
					},
					"resourceID":         "1cf16c13-f5cb-497a-97d5-e909cba396a3",
					"resourceTypeID":     "node_8_cores_amd64",
					"alarmEventRecordId": "NodeClockNotSynchronising_spoke1_ostest-extraworker-1",
					"probableCauseID":    "NodeClockNotSynchronising",
				}

				//validate the subId set size(expected filter matched number correct)
				subIdSet2 := handler.getSubscriptionIdsFromAlarm(ctx, requestObj)
				Expect(len(subIdSet2) == 0)

				//validate add/post request go through
				add_req = AddRequest{nil, requestObj2}
				_, err = handler.add(ctx, &add_req)
				Expect(err).To(HaveOccurred())
			})

		})
	})
})
