/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	svccommon "github.com/openshift-kni/oran-o2ims/internal/service/common"
)

const (
	version4167 = "4.16.7"
	version4152 = "4.15.2"
)

var _ = Describe("Alarms Collector", func() {
	Describe("getManagedCluster", func() {
		var (
			r      *AlarmsDataSource
			ctx    context.Context
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			_ = clusterv1.AddToScheme(scheme)

			withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &AlarmsDataSource{
				hubClient: withWatch,
			}

			ctx = context.Background()

			managedClusters := []*clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "default",
						Labels: map[string]string{
							ctlrutils.OpenshiftVersionLabelName: version4167,
							ctlrutils.LocalClusterLabelName:     "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-2",
						Namespace: "default",
						Labels: map[string]string{
							ctlrutils.OpenshiftVersionLabelName: version4167,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-3",
						Namespace: "default",
						Labels: map[string]string{
							ctlrutils.OpenshiftVersionLabelName: version4167,
							ctlrutils.LocalClusterLabelName:     "true",
						},
					},
				},
			}

			for _, cluster := range managedClusters {
				err := r.hubClient.Create(ctx, cluster)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("returns a cluster with the correct version", func() {
			cluster, err := r.getManagedCluster(ctx, version4167)
			Expect(err).NotTo(HaveOccurred())

			Expect(cluster.Labels[ctlrutils.OpenshiftVersionLabelName]).To(Equal(version4167))
			Expect(cluster.Labels).ToNot(HaveKey(ctlrutils.LocalClusterLabelName))
		})
		It("returns an error when no cluster is found", func() {
			_, err := r.getManagedCluster(ctx, version4152)
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("processManagedCluster", func() {
		var (
			r      *AlarmsDataSource
			ctx    context.Context
			scheme *runtime.Scheme

			temp func(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error)
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			_ = clusterv1.AddToScheme(scheme)

			withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &AlarmsDataSource{
				hubClient: withWatch,
			}

			ctx = context.Background()

			managedCluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: "default",
					Labels: map[string]string{
						ctlrutils.OpenshiftVersionLabelName: version4167,
					},
				},
			}

			err := r.hubClient.Create(ctx, managedCluster)
			Expect(err).NotTo(HaveOccurred())

			temp = getClientForCluster
			getClientForCluster = func(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error) {
				scheme := runtime.NewScheme()
				_ = monitoringv1.AddToScheme(scheme)
				withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()

				rules := []*monitoringv1.PrometheusRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "acm-metrics-collector-alerting-rules",
							Namespace: "monitoring",
						},
						Spec: monitoringv1.PrometheusRuleSpec{
							Groups: []monitoringv1.RuleGroup{
								{
									Name: "metrics-collector-rules",
									Rules: []monitoringv1.Rule{
										{
											Alert: "ACMMetricsCollectorFederationError",
											Annotations: map[string]string{
												"summary":     "Error federating from in-cluster Prometheus.",
												"description": "There are errors when federating from platform Prometheus",
											},
											Expr: intstr.IntOrString{
												Type:   intstr.String,
												IntVal: 0,
												StrVal: "(sum by (status_code, type) (rate(acm_metrics_collector_federate_requests_total{status_code!~\"2.*\"}[10m]))) > 10",
											},
											For: func() *monitoringv1.Duration {
												d := monitoringv1.Duration("10m")
												return &d
											}(),
											Labels: map[string]string{
												"severity": "critical",
											},
										},
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "machineapprover-rules",
							Namespace: "monitoring",
						},
						Spec: monitoringv1.PrometheusRuleSpec{
							Groups: []monitoringv1.RuleGroup{
								{
									Name: "memory-alerts",
									Rules: []monitoringv1.Rule{
										{
											Alert: "HighMemoryUsage",
											Annotations: map[string]string{
												"summary":     "High Memory Usage detected",
												"description": "Memory usage is above 85% for more than 5 minutes on instance {{ $labels.instance }}",
											},
											Expr: intstr.IntOrString{
												Type:   intstr.String,
												IntVal: 0,
												StrVal: "mapi_current_pending_csr > mapi_max_pending_csr",
											},
											For: func() *monitoringv1.Duration {
												d := monitoringv1.Duration("5m")
												return &d
											}(),
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
					err := withWatch.Create(ctx, rule)
					Expect(err).NotTo(HaveOccurred())
				}

				return withWatch, nil
			}
		})

		AfterEach(func() {
			getClientForCluster = temp
		})

		It("returns prometheus rules associated with a cluster", func() {
			rules, err := r.processManagedCluster(ctx, version4167)
			Expect(err).NotTo(HaveOccurred())

			Expect(rules).To(HaveLen(2))
		})
	})

	Describe("getRules", func() {
		var (
			r      *AlarmsDataSource
			ctx    context.Context
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			_ = monitoringv1.AddToScheme(scheme)

			ctx = context.Background()
		})

		It("excludes hardware monitoring groups from results", func() {
			withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &AlarmsDataSource{
				hubClient: withWatch,
			}

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
										Alert: "HardwareTemperatureHigh",
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
				err := r.hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			// Call getRules and verify only non-hardware alerts are returned
			result, err := r.getRules(ctx, r.hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Alert).To(Equal("ClusterMemoryHigh"))
		})

		It("includes all rules when no hardware groups are present", func() {
			withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &AlarmsDataSource{
				hubClient: withWatch,
			}

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
				err := r.hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := r.getRules(ctx, r.hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))
		})

		It("excludes only groups with both type and component hardware labels", func() {
			withWatch := fake.NewClientBuilder().WithScheme(scheme).Build()
			r = &AlarmsDataSource{
				hubClient: withWatch,
			}

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
						},
					},
				},
			}

			for _, rule := range rules {
				err := r.hubClient.Create(ctx, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			// Both alerts should be included since neither group has BOTH labels
			result, err := r.getRules(ctx, r.hubClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))
		})
	})

	Describe("isTemplated", func() {
		DescribeTable("correctly identifies templated strings",
			func(input string, expected bool) {
				Expect(IsTemplated(input)).To(Equal(expected))
			},
			Entry("templated severity from ViolatedPolicyReport", "{{ $labels.severity }}", true),
			Entry("templated with spaces", "{{$labels.severity}}", true),
			Entry("simple static value", "critical", false),
			Entry("empty string", "", false),
			Entry("partial template open only", "{{something", false),
			Entry("partial template close only", "something}}", false),
			Entry("multiple templates", "{{ $labels.foo }} and {{ $labels.bar }}", true),
		)
	})

	Describe("expandTemplatedSeverity", func() {
		var r *AlarmsDataSource

		BeforeEach(func() {
			r = &AlarmsDataSource{}
		})

		It("expands rules with templated severity into 5 static rules", func() {
			// ViolatedPolicyReport rule with templated severity
			rules := []monitoringv1.Rule{
				{
					Alert: "ViolatedPolicyReport",
					Expr:  intstr.FromString("sum(policyreport_info) > 0"),
					For: func() *monitoringv1.Duration {
						d := monitoringv1.Duration("1m")
						return &d
					}(),
					Labels: map[string]string{
						"severity": "{{ $labels.severity }}",
					},
					Annotations: map[string]string{
						"summary":     "Policy violation detected",
						"description": "The policy has a violation",
					},
				},
			}

			result := r.expandTemplatedSeverity(rules)

			// Should expand into 5 rules (critical, important, moderate, low, unknown)
			Expect(result).To(HaveLen(5))

			// Verify each expanded rule has a static severity
			severities := make(map[string]bool)
			for _, rule := range result {
				Expect(rule.Alert).To(Equal("ViolatedPolicyReport"))
				severity := rule.Labels["severity"]
				Expect(severity).NotTo(ContainSubstring("{{"))
				severities[severity] = true
			}

			// Verify all expected severities are present
			Expect(severities).To(HaveKey("critical"))
			Expect(severities).To(HaveKey("important"))
			Expect(severities).To(HaveKey("moderate"))
			Expect(severities).To(HaveKey("low"))
			Expect(severities).To(HaveKey("unknown"))
		})

		It("preserves non-templated rules unchanged", func() {
			rules := []monitoringv1.Rule{
				{
					Alert: "KubePersistentVolumeFillingUp",
					Expr:  intstr.FromString("kubelet_volume_stats < 0.03"),
					Labels: map[string]string{
						"severity": "info",
					},
				},
			}

			result := r.expandTemplatedSeverity(rules)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Alert).To(Equal("KubePersistentVolumeFillingUp"))
			Expect(result[0].Labels["severity"]).To(Equal("info"))
		})

		It("handles mixed templated and non-templated rules", func() {
			rules := []monitoringv1.Rule{
				{
					Alert: "ViolatedPolicyReport",
					Expr:  intstr.FromString("sum(policyreport_info) > 0"),
					Labels: map[string]string{
						"severity": "{{ $labels.severity }}",
					},
				},
				{
					Alert: "KubePersistentVolumeFillingUp",
					Expr:  intstr.FromString("kubelet_volume_stats < 0.03"),
					Labels: map[string]string{
						"severity": "info",
					},
				},
			}

			result := r.expandTemplatedSeverity(rules)

			// 5 from ViolatedPolicyReport + 1 from KubePersistentVolumeFillingUp
			Expect(result).To(HaveLen(6))
		})

		It("handles rules without severity label", func() {
			rules := []monitoringv1.Rule{
				{
					Alert: "SomeAlert",
					Expr:  intstr.FromString("up == 0"),
					Labels: map[string]string{
						"team": "platform",
					},
				},
			}

			result := r.expandTemplatedSeverity(rules)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Alert).To(Equal("SomeAlert"))
		})

		It("preserves other labels and annotations during expansion", func() {
			rules := []monitoringv1.Rule{
				{
					Alert: "ViolatedPolicyReport",
					Expr:  intstr.FromString("sum(policyreport_info) > 0"),
					Labels: map[string]string{
						"severity": "{{ $labels.severity }}",
						"team":     "security",
					},
					Annotations: map[string]string{
						"summary":     "Policy violation",
						"runbook_url": "https://example.com/runbook",
					},
				},
			}

			result := r.expandTemplatedSeverity(rules)

			Expect(result).To(HaveLen(5))
			for _, rule := range result {
				// Other labels preserved
				Expect(rule.Labels["team"]).To(Equal("security"))
				// Annotations preserved
				Expect(rule.Annotations["summary"]).To(Equal("Policy violation"))
				Expect(rule.Annotations["runbook_url"]).To(Equal("https://example.com/runbook"))
			}
		})
	})
})
