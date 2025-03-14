// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package alertmanager_test

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	mockinfrastructure "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/generated"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Alertmanager Functions", func() {
	var (
		ctrl            *gomock.Controller
		mockInfraClient *mockinfrastructure.MockClient
		ctx             context.Context
		commonFP        string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockInfraClient = mockinfrastructure.NewMockClient(ctrl)
		ctx = context.Background()
		commonFP = "abcdef123456"
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("ConvertAmToAlarmEventRecordModels", func() {
		It("should convert a standard alert correctly", func() {
			// Setup
			now := time.Now().UTC()
			clusterUUID := uuid.New()
			fingerprint := commonFP
			objectTypeIDUUID := uuid.New()
			alarmDefUUID := uuid.New()

			// Setup mock expectations
			mockInfraClient.EXPECT().
				GetObjectTypeID(gomock.Any(), clusterUUID).
				Return(objectTypeIDUUID, nil)

			mockInfraClient.EXPECT().
				GetAlarmDefinitionID(gomock.Any(), objectTypeIDUUID, "TestAlert", "critical").
				Return(alarmDefUUID, nil)

			// Create the alert
			firing := api.Firing
			labels := map[string]string{
				"alertname":       "TestAlert",
				"severity":        "critical",
				"managed_cluster": clusterUUID.String(),
			}
			annotations := map[string]string{
				"summary": "Test alert summary",
			}
			alerts := []api.Alert{
				{
					Status:      &firing,
					StartsAt:    &now,
					EndsAt:      nil,
					Fingerprint: &fingerprint,
					Labels:      &labels,
					Annotations: &annotations,
				},
			}

			records := alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts, mockInfraClient)

			// Assert
			Expect(records).To(HaveLen(1))
			record := records[0]

			Expect(record.AlarmStatus).To(Equal("firing"))
			Expect(record.Fingerprint).To(Equal(fingerprint))
			Expect(record.PerceivedSeverity).To(Equal(api.CRITICAL))
			Expect(record.AlarmRaisedTime).To(Equal(now))

			// Check cluster ID and alarm definition ID
			Expect(record.ObjectID).NotTo(BeNil())
			Expect(*record.ObjectID).To(Equal(clusterUUID))
			Expect(record.AlarmDefinitionID).NotTo(BeNil())
			Expect(*record.AlarmDefinitionID).To(Equal(alarmDefUUID))
		})

		It("should handle resolved alerts correctly", func() {
			// Setup
			now := time.Now().UTC()
			clusterUUID := uuid.New()
			fingerprint := commonFP
			endTime := now.Add(1 * time.Hour)
			objectTypeIDUUID := uuid.New()
			alarmDefUUID := uuid.New()

			// Create the alert
			resolved := api.Resolved
			labels := map[string]string{
				"alertname":       "TestAlert",
				"severity":        "critical",
				"managed_cluster": clusterUUID.String(),
			}
			annotations := map[string]string{
				"summary": "Test alert summary",
			}
			alerts := []api.Alert{
				{
					Status:      &resolved,
					StartsAt:    &now,
					EndsAt:      &endTime,
					Fingerprint: &fingerprint,
					Labels:      &labels,
					Annotations: &annotations,
				},
			}

			mockInfraClient.EXPECT().
				GetObjectTypeID(gomock.Any(), clusterUUID).
				Return(objectTypeIDUUID, nil)

			mockInfraClient.EXPECT().
				GetAlarmDefinitionID(gomock.Any(), objectTypeIDUUID, "TestAlert", "critical").
				Return(alarmDefUUID, nil)

			records := alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts, mockInfraClient)

			// Assert
			Expect(records).To(HaveLen(1))
			record := records[0]

			Expect(record.AlarmStatus).To(Equal("resolved"))
			Expect(record.PerceivedSeverity).To(Equal(api.CLEARED))
			Expect(record.AlarmClearedTime).NotTo(BeNil())
			Expect(*record.AlarmClearedTime).To(Equal(endTime))
		})

		It("should skip alerts with missing required fields", func() {
			clusterUUID := uuid.New()
			now := time.Now().UTC()
			firing := api.Firing
			fingerprint := commonFP

			// No StartsAt
			alerts1 := []api.Alert{
				{
					Status:      &firing,
					StartsAt:    nil, // Missing StartsAt
					Fingerprint: &fingerprint,
					Labels: &map[string]string{
						"alertname":       "TestAlert",
						"managed_cluster": clusterUUID.String(),
					},
				},
			}

			// No Status
			alerts2 := []api.Alert{
				{
					Status:      nil, // Missing Status
					StartsAt:    &now,
					Fingerprint: &fingerprint,
					Labels: &map[string]string{
						"alertname":       "TestAlert",
						"managed_cluster": clusterUUID.String(),
					},
				},
			}

			// No Fingerprint
			alerts3 := []api.Alert{
				{
					Status:      &firing,
					StartsAt:    &now,
					Fingerprint: nil, // Missing Fingerprint
					Labels: &map[string]string{
						"alertname":       "TestAlert",
						"managed_cluster": clusterUUID.String(),
					},
				},
			}

			Expect(alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts1, mockInfraClient)).To(BeEmpty())
			Expect(alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts2, mockInfraClient)).To(BeEmpty())
			Expect(alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts3, mockInfraClient)).To(BeEmpty())
		})

		It("should handle infrastructure client errors gracefully", func() {
			// Setup
			now := time.Now().UTC()
			clusterUUID := uuid.New()
			fingerprint := commonFP

			// Mock error response
			mockInfraClient.EXPECT().
				GetObjectTypeID(gomock.Any(), clusterUUID).
				Return(uuid.Nil, errors.New("infrastructure error"))

			// Create alert
			firing := api.Firing
			labels := map[string]string{
				"alertname":       "TestAlert",
				"severity":        "critical",
				"managed_cluster": clusterUUID.String(),
			}
			alerts := []api.Alert{
				{
					Status:      &firing,
					StartsAt:    &now,
					Fingerprint: &fingerprint,
					Labels:      &labels,
				},
			}

			records := alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts, mockInfraClient)

			// Assert - should still create record but without ObjectTypeID
			Expect(records).To(HaveLen(1))
			record := records[0]
			Expect(record.ObjectTypeID).To(BeNil())
			Expect(record.AlarmDefinitionID).To(BeNil()) // Can't get definition without object type
		})

		DescribeTable("severity mapping",
			func(inputSeverity string, expectedSeverity api.PerceivedSeverity) {
				// Setup
				now := time.Now().UTC()
				clusterUUID := uuid.New()
				fingerprint := "severity-test"
				objectTypeIDUUID := uuid.New()
				alarmDefUUID := uuid.New()

				// Setup mock expectations
				mockInfraClient.EXPECT().
					GetObjectTypeID(gomock.Any(), clusterUUID).
					Return(objectTypeIDUUID, nil)

				mockInfraClient.EXPECT().
					GetAlarmDefinitionID(gomock.Any(), objectTypeIDUUID, "TestAlert", inputSeverity).
					Return(alarmDefUUID, nil)

				// Create alert with this severity
				firing := api.Firing
				labels := map[string]string{
					"alertname":       "TestAlert",
					"severity":        inputSeverity,
					"managed_cluster": clusterUUID.String(),
				}
				annotations := map[string]string{}
				alerts := []api.Alert{
					{
						Status:      &firing,
						StartsAt:    &now,
						Fingerprint: &fingerprint,
						Labels:      &labels,
						Annotations: &annotations,
					},
				}

				records := alertmanager.ConvertAmToAlarmEventRecordModels(ctx, &alerts, mockInfraClient)

				// Assert
				Expect(records).To(HaveLen(1))
				Expect(records[0].PerceivedSeverity).To(Equal(expectedSeverity))
			},
			Entry("critical severity", "critical", api.CRITICAL),
			Entry("major severity", "major", api.MAJOR),
			Entry("minor severity", "minor", api.MINOR),
			Entry("low severity", "low", api.MINOR),
			Entry("warning severity", "warning", api.WARNING),
			Entry("info severity", "info", api.WARNING),
			Entry("unknown severity", "unknown", api.INDETERMINATE),
		)
	})

	Context("ConvertAPIAlertsToWebhook", func() {
		It("should convert empty array", func() {
			webhookAlerts := alertmanager.ConvertAPIAlertsToWebhook(&[]alertmanager.APIAlert{})
			Expect(webhookAlerts).To(BeEmpty())
		})

		It("should convert firing alerts correctly", func() {
			// Setup
			now := time.Now().UTC()

			// Create a firing API alert (with future EndAt)
			apiAlerts := []alertmanager.APIAlert{
				{
					Annotations: &map[string]string{
						"summary": "Test alert summary",
					},
					Labels: &map[string]string{
						"alertname":       "TestAlert",
						"severity":        "critical",
						"managed_cluster": uuid.New().String(),
					},
					StartsAt:     TimePtr(now.Add(-1 * time.Hour)),
					EndsAt:       TimePtr(now.Add(1 * time.Hour)), // Future time = still firing
					GeneratorURL: StringPtr("http://test-url"),
					Fingerprint:  &commonFP,
				},
			}

			webhookAlerts := alertmanager.ConvertAPIAlertsToWebhook(&apiAlerts)

			// Assert
			Expect(webhookAlerts).To(HaveLen(1))

			alert := webhookAlerts[0]
			Expect(*alert.Status).To(Equal(api.Firing))
			Expect(*alert.StartsAt).To(Equal(*apiAlerts[0].StartsAt))
			Expect(alert.EndsAt).To(BeNil()) // EndsAt should be nil for firing alerts
			Expect(*alert.Fingerprint).To(Equal(*apiAlerts[0].Fingerprint))
			Expect(*alert.GeneratorURL).To(Equal(*apiAlerts[0].GeneratorURL))
			Expect(*alert.Labels).To(Equal(*apiAlerts[0].Labels))
			Expect(*alert.Annotations).To(Equal(*apiAlerts[0].Annotations))
		})

		It("should convert resolved alerts correctly", func() {
			// Setup
			now := time.Now().UTC()
			pastTime := now.Add(-2 * time.Hour)

			// Create a resolved API alert (with past EndAt)
			apiAlerts := []alertmanager.APIAlert{
				{
					Annotations: &map[string]string{
						"summary": "Test alert summary",
					},
					Labels: &map[string]string{
						"alertname":       "TestAlert",
						"severity":        "critical",
						"managed_cluster": uuid.New().String(),
					},
					StartsAt:     TimePtr(now.Add(-3 * time.Hour)),
					EndsAt:       TimePtr(pastTime), // Past time = resolved
					GeneratorURL: StringPtr("http://test-url"),
					Fingerprint:  &commonFP,
				},
			}

			webhookAlerts := alertmanager.ConvertAPIAlertsToWebhook(&apiAlerts)

			// Assert
			Expect(webhookAlerts).To(HaveLen(1))

			alert := webhookAlerts[0]
			Expect(*alert.Status).To(Equal(api.Resolved))
			Expect(*alert.StartsAt).To(Equal(*apiAlerts[0].StartsAt))
			Expect(*alert.EndsAt).To(Equal(pastTime)) // EndsAt should be set for resolved alerts
		})
	})
})
