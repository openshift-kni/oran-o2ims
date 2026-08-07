/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases in this file:

1. "Resource server deployment is updated after edit"
   - Creates an Inventory resource with test image
   - Verifies that when a deployment's spec is manually modified, the reconciler
     restores the original values during the next reconciliation cycle
   - Tests the reconciler's ability to maintain desired state and handle drift

2. "Check for presence of all servers"
   - Creates an Inventory resource and verifies all required server deployments are created
   - Checks for the existence of all inventory microservices:
     * Resource server
     * Cluster server
     * Alarms server
     * Artifacts server
     * Provisioning server
   - Validates complete inventory service deployment

3. "NetworkPolicy scopes: API servers allow ingress-controller, database does not"
   - Verifies API server and hardware manager NetworkPolicies allow ingress-controller,
     monitoring, and same-namespace traffic
   - Verifies alarms-server additionally allows alertmanager ingress
   - Verifies database NetworkPolicy blocks ingress-controller and monitoring but
     allows same-namespace traffic

4. "Postgres ConfigMap contains TLS profile settings and deployment has tls-profile-hash"
   - Verifies base ConfigMap (no TLS profile set) has only static PG18 TLS controls
   - Re-reconciles with a mixed cipher list (Intermediate-style) and verifies:
     * ssl_min_protocol_version is mapped from the cluster profile
     * ssl_ciphers contains only TLS 1.2 ciphers (TLS 1.3 filtered out)
     * ssl_tls13_ciphers excludes CCM and ssl_groups enables ML-KEM
   - Re-reconciles with Modern profile (TLS 1.3 only, no TLS 1.2 ciphers) and verifies:
     * ssl_min_protocol_version = TLSv1.3
     * ssl_ciphers is omitted (no TLS 1.2 ciphers in Modern)
     * ssl_tls13_ciphers and ssl_groups remain present
   - Verifies the postgres-server Deployment has the tls-profile-hash annotation
*/

package controllers

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	openshiftv1 "github.com/openshift/api/config/v1"
	openshiftoperatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var serverTestImage = "controller-manager:test"

func makePod(namespace, serverName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": serverName,
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

var _ = Describe("Inventory Controller", func() {
	DescribeTable(
		"Reconciler",
		func(objs []client.Object, request reconcile.Request, validate func(result ctrl.Result, reconciler *Reconciler)) {
			// Declare the Namespace for the O-Cloud Manager resource.
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: constants.DefaultNamespace,
				},
			}

			ingress := &openshiftoperatorv1.IngressController{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "openshift-ingress-operator"},
				Spec: openshiftoperatorv1.IngressControllerSpec{
					Domain: "apps.example.com"}}

			search := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "search-search-api",
					Namespace: "open-cluster-management",
					Labels:    map[string]string{ctlrutils.SearchApiLabelKey: ctlrutils.SearchApiLabelValue},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: 4010,
							Name: "search-api",
						},
					},
				},
			}

			pods := []client.Object{
				makePod(ns.Name, ctlrutils.InventoryDatabaseServerName),
				makePod(ns.Name, ctlrutils.InventoryResourceServerName),
				makePod(ns.Name, ctlrutils.InventoryClusterServerName),
				makePod(ns.Name, ctlrutils.InventoryAlarmServerName),
				makePod(ns.Name, ctlrutils.InventoryArtifactsServerName),
				makePod(ns.Name, ctlrutils.InventoryProvisioningServerName),
			}

			cv := &openshiftv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "version",
				},
			}

			// Set up necessary env variables
			err := os.Setenv(constants.PostgresImageName, "postgres:test")
			Expect(err).ToNot(HaveOccurred())

			// Update the testcase objects to include the Namespace.
			objs = append(objs, ns, ingress, search, cv)
			objs = append(objs, pods...)

			// Get the fake client.
			fakeClient := fakeclient.GetFakeClientFromObjects(objs...)

			// Initialize the O-Cloud Manager reconciler.
			r := &Reconciler{
				Client: fakeClient,
				Logger: logger,
				Image:  serverTestImage,
			}

			// Reconcile.
			result, err := r.Reconcile(context.TODO(), request)
			Expect(err).ToNot(HaveOccurred())

			validate(result, r)
		},
		Entry(
			"Resource server deployment is updated after edit",
			[]client.Object{
				&inventoryv1alpha1.Inventory{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "oran-o2ims-sample-1",
						Namespace:         ctlrutils.InventoryNamespace,
						CreationTimestamp: metav1.Now(),
					},
					Spec: inventoryv1alpha1.InventorySpec{},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ctlrutils.InventoryNamespace,
					Name:      "oran-o2ims-sample-1",
				},
			},
			func(result ctrl.Result, reconciler *Reconciler) {
				Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

				// Check that the metadata server deployment exists.
				deployment := &appsv1.Deployment{}
				err := reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryResourceServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					deployment)
				Expect(err).ToNot(HaveOccurred())

				// Update one of the deployment's Spec values to something random.
				savedSpecTemplateVolumeSecret := deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName
				savedContainersArgsValue := deployment.Spec.Template.Spec.Containers[0].Args
				deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName = "made-up-name"
				deployment.Spec.Template.Spec.Containers[0].Args = []string{"a", "b"}

				// Run the reconciliation again.
				req := reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: ctlrutils.InventoryNamespace,
						Name:      "oran-o2ims-sample-1",
					},
				}
				_, err = reconciler.Reconcile(context.TODO(), req)
				Expect(err).ToNot(HaveOccurred())

				// Check that the fields edited above were restored to their previous value.
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryResourceServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					deployment)
				Expect(err).ToNot(HaveOccurred())
				Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).To(Equal(savedSpecTemplateVolumeSecret))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal(savedContainersArgsValue))
			},
		),
		Entry(
			"Check for presence of all servers",
			[]client.Object{
				&inventoryv1alpha1.Inventory{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "oran-o2ims-sample-1",
						Namespace:         constants.DefaultNamespace,
						CreationTimestamp: metav1.Now(),
					},
					Spec: inventoryv1alpha1.InventorySpec{},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ctlrutils.InventoryNamespace,
					Name:      "oran-o2ims-sample-1",
				},
			},
			func(result ctrl.Result, reconciler *Reconciler) {
				Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

				// Check that the resource server exists.
				resourceDeployment := &appsv1.Deployment{}
				err := reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryResourceServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					resourceDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Check that the cluster server exists.
				clusterDeployment := &appsv1.Deployment{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryClusterServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					clusterDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Check that the alarms server exists.
				alarmsDeployment := &appsv1.Deployment{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryAlarmServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					alarmsDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Check that the artifacts server exists.
				artifactsDeployment := &appsv1.Deployment{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryArtifactsServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					artifactsDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Check that the provisioning server exists.
				provisioningDeployment := &appsv1.Deployment{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      ctlrutils.InventoryProvisioningServerName,
						Namespace: ctlrutils.InventoryNamespace,
					},
					provisioningDeployment)
				Expect(err).ToNot(HaveOccurred())
			},
		),
		Entry(
			"NetworkPolicy scopes: API servers allow ingress-controller, database does not",
			[]client.Object{
				&inventoryv1alpha1.Inventory{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "oran-o2ims-sample-1",
						Namespace:         constants.DefaultNamespace,
						CreationTimestamp: metav1.Now(),
					},
					Spec: inventoryv1alpha1.InventorySpec{},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ctlrutils.InventoryNamespace,
					Name:      "oran-o2ims-sample-1",
				},
			},
			func(result ctrl.Result, reconciler *Reconciler) {
				Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

				hasIngressControllerRule := func(np *networkingv1.NetworkPolicy) bool {
					for _, rule := range np.Spec.Ingress {
						for _, peer := range rule.From {
							if peer.NamespaceSelector != nil {
								if peer.NamespaceSelector.MatchLabels["network.openshift.io/policy-group"] == "ingress" {
									return true
								}
							}
						}
					}
					return false
				}

				hasSameNamespaceRule := func(np *networkingv1.NetworkPolicy) bool {
					for _, rule := range np.Spec.Ingress {
						for _, peer := range rule.From {
							if peer.PodSelector != nil && peer.NamespaceSelector == nil {
								if len(peer.PodSelector.MatchLabels) == 0 && len(peer.PodSelector.MatchExpressions) == 0 {
									return true
								}
							}
						}
					}
					return false
				}

				hasAlertmanagerRule := func(np *networkingv1.NetworkPolicy) bool {
					for _, rule := range np.Spec.Ingress {
						for _, peer := range rule.From {
							if peer.NamespaceSelector != nil && peer.PodSelector != nil {
								nsMatch := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] ==
									ctlrutils.OpenClusterManagementObservabilityNamespace
								podMatch := peer.PodSelector.MatchLabels[ctlrutils.AlertmanagerObjectName] == "observability"
								if nsMatch && podMatch {
									return true
								}
							}
						}
					}
					return false
				}

				// API servers and hardware manager should allow ingress-controller traffic.
				for _, serverName := range []string{
					ctlrutils.InventoryResourceServerName,
					ctlrutils.InventoryClusterServerName,
					ctlrutils.InventoryAlarmServerName,
					ctlrutils.InventoryArtifactsServerName,
					ctlrutils.InventoryProvisioningServerName,
					ctlrutils.HardwareManagerServerName,
				} {
					np := &networkingv1.NetworkPolicy{}
					err := reconciler.Client.Get(context.TODO(), types.NamespacedName{
						Name:      serverName,
						Namespace: ctlrutils.InventoryNamespace,
					}, np)
					Expect(err).ToNot(HaveOccurred())
					Expect(hasIngressControllerRule(np)).To(BeTrue(),
						"expected ingress-controller rule on %s NetworkPolicy", serverName)
					Expect(hasSameNamespaceRule(np)).To(BeTrue(),
						"expected same-namespace rule on %s NetworkPolicy", serverName)
				}

				// Alarm server should additionally allow alertmanager traffic.
				alarmNP := &networkingv1.NetworkPolicy{}
				err := reconciler.Client.Get(context.TODO(), types.NamespacedName{
					Name:      ctlrutils.InventoryAlarmServerName,
					Namespace: ctlrutils.InventoryNamespace,
				}, alarmNP)
				Expect(err).ToNot(HaveOccurred())
				Expect(hasAlertmanagerRule(alarmNP)).To(BeTrue(),
					"expected alertmanager ingress rule on alarm server NetworkPolicy")

				// Database should NOT allow ingress-controller traffic but SHOULD allow same-namespace traffic.
				dbNP := &networkingv1.NetworkPolicy{}
				err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
					Name:      ctlrutils.InventoryDatabaseServerName,
					Namespace: ctlrutils.InventoryNamespace,
				}, dbNP)
				Expect(err).ToNot(HaveOccurred())
				Expect(hasIngressControllerRule(dbNP)).To(BeFalse(),
					"database NetworkPolicy should not allow ingress-controller traffic")
				Expect(hasSameNamespaceRule(dbNP)).To(BeTrue(),
					"database NetworkPolicy should allow same-namespace traffic")
			},
		),
		Entry(
			"Postgres ConfigMap contains TLS profile settings and deployment has tls-profile-hash",
			[]client.Object{
				&inventoryv1alpha1.Inventory{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "oran-o2ims-sample-1",
						Namespace:         constants.DefaultNamespace,
						CreationTimestamp: metav1.Now(),
					},
					Spec: inventoryv1alpha1.InventorySpec{},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ctlrutils.InventoryNamespace,
					Name:      "oran-o2ims-sample-1",
				},
			},
			func(result ctrl.Result, reconciler *Reconciler) {
				Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

				pgConfKey := types.NamespacedName{
					Name:      ctlrutils.InventoryDatabaseServerName + "-config",
					Namespace: ctlrutils.InventoryNamespace,
				}
				pgDeployKey := types.NamespacedName{
					Name:      ctlrutils.InventoryDatabaseServerName,
					Namespace: ctlrutils.InventoryNamespace,
				}
				req := reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: ctlrutils.InventoryNamespace,
						Name:      "oran-o2ims-sample-1",
					},
				}

				// --- Phase 1: Base reconcile (no TLS profile set) ---
				// Static PG18 TLS controls are always appended; dynamic settings are not.
				cm := &corev1.ConfigMap{}
				err := reconciler.Client.Get(context.TODO(), pgConfKey, cm)
				Expect(err).ToNot(HaveOccurred())
				Expect(cm.Data).To(HaveKey("postgresql.conf"))
				pgConf := cm.Data["postgresql.conf"]
				Expect(pgConf).ToNot(ContainSubstring("ssl_min_protocol_version"))
				Expect(pgConf).ToNot(ContainSubstring("ssl_ciphers"))
				Expect(pgConf).To(ContainSubstring("ssl_tls13_ciphers"))
				Expect(pgConf).To(ContainSubstring("ssl_groups"))

				// --- Phase 2: Mixed cipher list (Intermediate-style with TLS 1.2 + 1.3 ciphers) ---
				reconciler.TLSMinVersion = "VersionTLS12"
				reconciler.TLSProfileHash = "intermediate-hash"
				reconciler.TLSCiphers = "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,ECDHE-RSA-AES128-GCM-SHA256,ECDHE-RSA-AES256-GCM-SHA384"
				_, err = reconciler.Reconcile(context.TODO(), req)
				Expect(err).ToNot(HaveOccurred())

				err = reconciler.Client.Get(context.TODO(), pgConfKey, cm)
				Expect(err).ToNot(HaveOccurred())
				pgConf = cm.Data["postgresql.conf"]
				Expect(pgConf).To(ContainSubstring("ssl_min_protocol_version = 'TLSv1.2'"))
				Expect(pgConf).To(ContainSubstring(
					"ssl_ciphers = 'ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384'"))
				Expect(pgConf).To(ContainSubstring(
					"ssl_tls13_ciphers = 'TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256:TLS_AES_128_GCM_SHA256'"))
				Expect(pgConf).ToNot(ContainSubstring("TLS_AES_128_CCM_SHA256"))
				Expect(pgConf).To(ContainSubstring(
					"ssl_groups = 'X25519MLKEM768:X25519:prime256v1'"))

				deployment := &appsv1.Deployment{}
				err = reconciler.Client.Get(context.TODO(), pgDeployKey, deployment)
				Expect(err).ToNot(HaveOccurred())
				Expect(deployment.Spec.Template.Annotations).To(
					HaveKeyWithValue("ocloud.openshift.io/tls-profile-hash", "intermediate-hash"))

				// --- Phase 3: Modern profile (TLS 1.3 only — no TLS 1.2 ciphers) ---
				reconciler.TLSMinVersion = "VersionTLS13"
				reconciler.TLSProfileHash = "modern-hash"
				reconciler.TLSCiphers = "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256"
				_, err = reconciler.Reconcile(context.TODO(), req)
				Expect(err).ToNot(HaveOccurred())

				err = reconciler.Client.Get(context.TODO(), pgConfKey, cm)
				Expect(err).ToNot(HaveOccurred())
				pgConf = cm.Data["postgresql.conf"]
				Expect(pgConf).To(ContainSubstring("ssl_min_protocol_version = 'TLSv1.3'"))
				Expect(pgConf).ToNot(ContainSubstring("ssl_ciphers ="),
					"Modern profile has no TLS 1.2 ciphers; ssl_ciphers should not be set")
				Expect(pgConf).To(ContainSubstring("ssl_tls13_ciphers"))
				Expect(pgConf).To(ContainSubstring("ssl_groups"))

				err = reconciler.Client.Get(context.TODO(), pgDeployKey, deployment)
				Expect(err).ToNot(HaveOccurred())
				Expect(deployment.Spec.Template.Annotations).To(
					HaveKeyWithValue("ocloud.openshift.io/tls-profile-hash", "modern-hash"),
					"annotation should update on profile change to trigger rolling restart")
			},
		),
	)
})
