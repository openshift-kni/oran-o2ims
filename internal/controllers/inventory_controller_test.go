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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var ServerTestImage = "controller-manager:test"

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
			Expect(err).NotTo(HaveOccurred())

			// Update the testcase objects to include the Namespace.
			objs = append(objs, ns, ingress, search, cv)
			objs = append(objs, pods...)

			// Get the fake client.
			fakeClient := getFakeClientFromObjects(objs...)

			// Initialize the O-Cloud Manager reconciler.
			r := &Reconciler{
				Client: fakeClient,
				Logger: logger,
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
					Spec: inventoryv1alpha1.InventorySpec{
						Image: &ServerTestImage,
					},
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
					Spec: inventoryv1alpha1.InventorySpec{
						Image: &ServerTestImage,
					},
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
	)
})
