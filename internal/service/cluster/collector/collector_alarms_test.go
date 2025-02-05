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

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
							utils.OpenshiftVersionLabelName: version4167,
							utils.LocalClusterLabelName:     "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-2",
						Namespace: "default",
						Labels: map[string]string{
							utils.OpenshiftVersionLabelName: version4167,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-3",
						Namespace: "default",
						Labels: map[string]string{
							utils.OpenshiftVersionLabelName: version4167,
							utils.LocalClusterLabelName:     "true",
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

			Expect(cluster.Labels[utils.OpenshiftVersionLabelName]).To(Equal(version4167))
			Expect(cluster.Labels).ToNot(HaveKey(utils.LocalClusterLabelName))
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
						utils.OpenshiftVersionLabelName: version4167,
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
})
