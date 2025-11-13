/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package listener

import (
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	svccommon "github.com/openshift-kni/oran-o2ims/internal/service/common"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

var _ = Describe("Alarms Sync", func() {
	Describe("getIronicPrometheusRules", func() {
		var (
			ctx    context.Context
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			_ = monitoringv1.AddToScheme(scheme)

			ctx = context.Background()
		})

		It("includes only hardware monitoring groups with ironic labels", func() {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create PrometheusRules with both hardware and non-hardware groups
			rules := []*monitoringv1.PrometheusRule{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mixed-rules",
						Namespace: "monitoring",
					},
					Spec: monitoringv1.PrometheusRuleSpec{
						Groups: []monitoringv1.RuleGroup{
							{
								Name: "hardware-alerts",
								Labels: map[string]string{
									svccommon.HardwareAlertTypeLabel:      svccommon.HardwareAlertTypeValue,
									svccommon.HardwareAlertComponentLabel: svccommon.HardwareAlertComponentValue,
								},
								Rules: []monitoringv1.Rule{
									{
										Alert: "IronicHardwareTemperatureHigh",
										Annotations: map[string]string{
											"summary":     "Hardware temperature is high",
											"description": "Sensor temperature above threshold",
										},
										Expr: intstr.IntOrString{
											Type:   intstr.String,
											StrVal: "hardware_temp > 80",
										},
										Labels: map[string]string{
											"severity": "warning",
										},
									},
								},
							},
							{
								Name: "cluster-alerts",
								Rules: []monitoringv1.Rule{
									{
										Alert: "ClusterMemoryHigh",
										Annotations: map[string]string{
											"summary":     "Cluster memory usage is high",
											"description": "Memory usage above 85%",
										},
										Expr: intstr.IntOrString{
											Type:   intstr.String,
											StrVal: "cluster_memory > 85",
										},
										Labels: map[string]string{
											"severity": "critical",
										},
									},
								},
							},
						},
					},
				},
			}

			for _, rule := range rules {
				err := hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			// Call getIronicPrometheusRules and verify only hardware alerts are returned
			result, err := getIronicPrometheusRules(ctx, hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Alert).To(Equal("IronicHardwareTemperatureHigh"))
		})

		It("excludes all rules when no hardware groups are present", func() {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			rules := []*monitoringv1.PrometheusRule{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-rules",
						Namespace: "monitoring",
					},
					Spec: monitoringv1.PrometheusRuleSpec{
						Groups: []monitoringv1.RuleGroup{
							{
								Name: "cluster-alerts-1",
								Rules: []monitoringv1.Rule{
									{
										Alert: "Alert1",
										Expr:  intstr.FromString("up == 1"),
									},
								},
							},
							{
								Name: "cluster-alerts-2",
								Rules: []monitoringv1.Rule{
									{
										Alert: "Alert2",
										Expr:  intstr.FromString("up == 0"),
									},
								},
							},
						},
					},
				},
			}

			for _, rule := range rules {
				err := hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := getIronicPrometheusRules(ctx, hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(0))
		})

		It("includes only groups with both type and component hardware labels", func() {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			rules := []*monitoringv1.PrometheusRule{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "partial-hardware-labels",
						Namespace: "monitoring",
					},
					Spec: monitoringv1.PrometheusRuleSpec{
						Groups: []monitoringv1.RuleGroup{
							{
								Name: "only-type-label",
								Labels: map[string]string{
									svccommon.HardwareAlertTypeLabel: svccommon.HardwareAlertTypeValue,
									// Missing component label
								},
								Rules: []monitoringv1.Rule{
									{
										Alert: "PartialHardwareAlert",
										Expr:  intstr.FromString("up == 1"),
									},
								},
							},
							{
								Name: "only-component-label",
								Labels: map[string]string{
									svccommon.HardwareAlertComponentLabel: svccommon.HardwareAlertComponentValue,
									// Missing type label
								},
								Rules: []monitoringv1.Rule{
									{
										Alert: "AnotherPartialAlert",
										Expr:  intstr.FromString("up == 0"),
									},
								},
							},
							{
								Name: "both-labels",
								Labels: map[string]string{
									svccommon.HardwareAlertTypeLabel:      svccommon.HardwareAlertTypeValue,
									svccommon.HardwareAlertComponentLabel: svccommon.HardwareAlertComponentValue,
								},
								Rules: []monitoringv1.Rule{
									{
										Alert: "ValidHardwareAlert",
										Expr:  intstr.FromString("hardware_error == 1"),
									},
								},
							},
						},
					},
				},
			}

			for _, rule := range rules {
				err := hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			// Only the alert from group with BOTH labels should be included
			result, err := getIronicPrometheusRules(ctx, hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Alert).To(Equal("ValidHardwareAlert"))
		})
	})

	Describe("makeAlarmDictionary", func() {
		It("creates alarm dictionary from resource type", func() {
			resourceTypeID := uuid.New()
			resourceType := models.ResourceType{
				ResourceTypeID: resourceTypeID,
				Model:          "Dell-R750",
				Vendor:         "Dell",
				Version:        "v1.0",
			}

			result := makeAlarmDictionary(resourceType)

			Expect(result.ResourceTypeID).To(Equal(resourceTypeID))
			Expect(result.Vendor).To(Equal("Dell"))
			Expect(result.AlarmDictionaryVersion).To(Equal("v1.0"))
			Expect(result.AlarmDictionarySchemaVersion).To(Equal(AlarmDictionarySchemaVersion))
			Expect(result.EntityType).To(Equal("Dell-R750-v1.0"))
			Expect(result.ManagementInterfaceID).To(Equal([]string{string(common.AlarmDefinitionManagementInterfaceIdO2IMS)}))
			Expect(result.PKNotificationField).To(Equal([]string{"alarmDefinitionID"}))
		})
	})

	Describe("makeAlarmDefinitions", func() {
		It("transforms prometheus rules to alarm definitions", func() {
			alarmDictID := uuid.New()
			forDuration := monitoringv1.Duration("5m")
			keepFiringDuration := monitoringv1.NonEmptyDuration("10m")

			rules := []monitoringv1.Rule{
				{
					Alert: "HardwareTemperatureHigh",
					Expr: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "hardware_temp > 80",
					},
					For:           &forDuration,
					KeepFiringFor: &keepFiringDuration,
					Labels: map[string]string{
						"severity": "warning",
					},
					Annotations: map[string]string{
						"summary":     "Hardware temperature is high",
						"description": "Temperature sensor above threshold",
						"runbook_url": "https://example.com/runbook",
					},
				},
				{
					Alert: "HardwareFanFailure",
					Expr: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "hardware_fan_status == 0",
					},
					Labels: map[string]string{
						"severity": "critical",
					},
					Annotations: map[string]string{
						"summary":     "Fan failure detected",
						"description": "Hardware fan not operational",
					},
				},
			}

			result := makeAlarmDefinitions(rules, alarmDictID)

			Expect(result).To(HaveLen(2))

			// Verify first alarm definition
			Expect(result[0].AlarmName).To(Equal("HardwareTemperatureHigh"))
			Expect(result[0].AlarmDictionaryID).To(Equal(&alarmDictID))
			Expect(result[0].Severity).To(Equal("warning"))
			Expect(result[0].AlarmDescription).To(ContainSubstring("Hardware temperature is high"))
			Expect(result[0].AlarmDescription).To(ContainSubstring("Temperature sensor above threshold"))
			Expect(result[0].ProposedRepairActions).To(Equal("https://example.com/runbook"))
			Expect(result[0].ClearingType).To(Equal(string(common.AUTOMATIC)))
			Expect(result[0].AlarmChangeType).To(Equal(string(common.ADDED)))
			Expect(result[0].ManagementInterfaceID).To(Equal([]string{string(common.AlarmDefinitionManagementInterfaceIdO2IMS)}))
			Expect(result[0].PKNotificationField).To(Equal([]string{"alarmDefinitionID"}))

			// Verify additional fields include Prometheus rule details
			additionalFields := *result[0].AlarmAdditionalFields
			Expect(additionalFields["Expr"]).To(Equal("hardware_temp > 80"))
			Expect(additionalFields["For"]).To(Equal("5m"))
			Expect(additionalFields["KeepFiringFor"]).To(Equal("10m"))
			Expect(additionalFields["severity"]).To(Equal("warning"))

			// Verify second alarm definition
			Expect(result[1].AlarmName).To(Equal("HardwareFanFailure"))
			Expect(result[1].Severity).To(Equal("critical"))
			Expect(result[1].ProposedRepairActions).To(BeEmpty()) // No runbook_url provided
		})

		It("handles rules without optional fields", func() {
			alarmDictID := uuid.New()

			rules := []monitoringv1.Rule{
				{
					Alert: "BasicAlert",
					Expr:  intstr.FromString("up == 0"),
					Labels: map[string]string{
						"severity": "info",
					},
					Annotations: map[string]string{
						"summary": "Basic summary",
					},
					// No For, KeepFiringFor, description, or runbook_url
				},
			}

			result := makeAlarmDefinitions(rules, alarmDictID)

			Expect(result).To(HaveLen(1))
			Expect(result[0].AlarmName).To(Equal("BasicAlert"))
			Expect(result[0].Severity).To(Equal("info"))
			Expect(result[0].ProposedRepairActions).To(BeEmpty())

			additionalFields := *result[0].AlarmAdditionalFields
			Expect(additionalFields["Expr"]).To(Equal("up == 0"))
			Expect(additionalFields).NotTo(HaveKey("For"))
			Expect(additionalFields).NotTo(HaveKey("KeepFiringFor"))
		})
	})
})
