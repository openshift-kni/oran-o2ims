/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
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

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var ServerTestImage = "controller-manager:test"

var _ = DescribeTable(
	"Reconciler",
	func(objs []client.Object, request reconcile.Request, validate func(result ctrl.Result, reconciler Reconciler)) {
		// Declare the Namespace for the O-RAN O2IMS resource.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "oran-o2ims",
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
				Labels:    map[string]string{utils.SearchApiLabelKey: utils.SearchApiLabelValue},
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

		cv := &openshiftv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
		}

		// Set up necessary env variables
		err := os.Setenv(utils.KubeRbacProxyImageName, "kube-rbac-proxy:test")
		Expect(err).NotTo(HaveOccurred())
		err = os.Setenv(utils.PostgresImageName, "postgres:test")
		Expect(err).NotTo(HaveOccurred())

		// Update the testcase objects to include the Namespace.
		objs = append(objs, ns, ingress, search, cv)

		// Get the fake client.
		fakeClient := getFakeClientFromObjects(objs...)

		// Initialize the O-RAN O2IMS reconciler.
		r := &Reconciler{
			Client: fakeClient,
			Logger: logger,
		}

		// Reconcile.
		result, err := r.Reconcile(context.TODO(), request)
		Expect(err).ToNot(HaveOccurred())

		validate(result, *r)
	},
	Entry(
		"Resource server deployment is updated after edit",
		[]client.Object{
			&inventoryv1alpha1.Inventory{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "oran-o2ims-sample-1",
					Namespace:         utils.InventoryNamespace,
					CreationTimestamp: metav1.Now(),
				},
				Spec: inventoryv1alpha1.InventorySpec{
					Image: &ServerTestImage,
					ResourceServerConfig: inventoryv1alpha1.ResourceServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: true,
						},
					},
					AlarmServerConfig: inventoryv1alpha1.AlarmServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: false,
						},
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: utils.InventoryNamespace,
				Name:      "oran-o2ims-sample-1",
			},
		},
		func(result ctrl.Result, reconciler Reconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Check that the metadata server deployment exists.
			deployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
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
					Namespace: utils.InventoryNamespace,
					Name:      "oran-o2ims-sample-1",
				},
			}
			_, err = reconciler.Reconcile(context.TODO(), req)
			Expect(err).ToNot(HaveOccurred())

			// Check that the fields edited above were restored to their previous value.
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).To(Equal(savedSpecTemplateVolumeSecret))
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal(savedContainersArgsValue))
		},
	),
	Entry(
		"Only the resource server is required",
		[]client.Object{
			&inventoryv1alpha1.Inventory{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "oran-o2ims-sample-1",
					Namespace:         utils.InventoryNamespace,
					CreationTimestamp: metav1.Now(),
				},
				Spec: inventoryv1alpha1.InventorySpec{
					Image: &ServerTestImage,
					ResourceServerConfig: inventoryv1alpha1.ResourceServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: true,
						},
					},
					AlarmServerConfig: inventoryv1alpha1.AlarmServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: false,
						},
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: utils.InventoryNamespace,
				Name:      "oran-o2ims-sample-1",
			},
		},
		func(result ctrl.Result, reconciler Reconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Check the metadata server deployment exists.
			resourceDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				resourceDeployment)
			Expect(err).ToNot(HaveOccurred())

			// Check that the Ingress exists.
			ingress := &networkingv1.Ingress{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryIngressName,
					Namespace: utils.InventoryNamespace,
				},
				ingress)
			Expect(err).ToNot(HaveOccurred())

			// Check that the ServiceAccount exists.
			serviceAccount := &corev1.ServiceAccount{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				serviceAccount)
			Expect(err).ToNot(HaveOccurred())

			// Check that the Service exists.
			service := &corev1.Service{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				service)
			Expect(err).ToNot(HaveOccurred())

			// Check the deployment manager server does not exist.
			alarmsDeployment := &appsv1.Deployment{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryAlarmServerName,
					Namespace: utils.InventoryNamespace,
				},
				alarmsDeployment)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(fmt.Sprintf("deployments.apps \"%s\" not found", utils.InventoryAlarmServerName)))
		},
	),
	Entry(
		"Resource, alarms and artifacts servers required",
		[]client.Object{
			&inventoryv1alpha1.Inventory{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "oran-o2ims-sample-1",
					Namespace:         "oran-o2ims",
					CreationTimestamp: metav1.Now(),
				},
				Spec: inventoryv1alpha1.InventorySpec{
					Image: &ServerTestImage,
					ResourceServerConfig: inventoryv1alpha1.ResourceServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: true,
						},
					},
					AlarmServerConfig: inventoryv1alpha1.AlarmServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: true,
						},
					},
					ArtifactsServerConfig: inventoryv1alpha1.ArtifactsServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: true,
						},
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: utils.InventoryNamespace,
				Name:      "oran-o2ims-sample-1",
			},
		},
		func(result ctrl.Result, reconciler Reconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Check that the resource server exists.
			resourceDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				resourceDeployment)
			Expect(err).ToNot(HaveOccurred())

			// Check that the alarms server exists.
			alarmsDeployment := &appsv1.Deployment{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryAlarmServerName,
					Namespace: utils.InventoryNamespace,
				},
				alarmsDeployment)
			Expect(err).ToNot(HaveOccurred())

			// Check that the artifacts server exists.
			artifactsDeployment := &appsv1.Deployment{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryArtifactsServerName,
					Namespace: utils.InventoryNamespace,
				},
				artifactsDeployment)
			Expect(err).ToNot(HaveOccurred())
		},
	),
	Entry(
		"No O-RAN O2IMS server required",
		[]client.Object{
			&inventoryv1alpha1.Inventory{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "oran-o2ims-sample-1",
					Namespace:         "oran-o2ims",
					CreationTimestamp: metav1.Now(),
				},
				Spec: inventoryv1alpha1.InventorySpec{
					Image: &ServerTestImage,
					ResourceServerConfig: inventoryv1alpha1.ResourceServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: false,
						},
					},
					AlarmServerConfig: inventoryv1alpha1.AlarmServerConfig{
						ServerConfig: inventoryv1alpha1.ServerConfig{
							Enabled: false,
						},
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: utils.InventoryNamespace,
				Name:      "oran-o2ims-sample-1",
			},
		},
		func(result ctrl.Result, reconciler Reconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))
			// Check the metadata server deployment does not exist.
			resourceDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryResourceServerName,
					Namespace: utils.InventoryNamespace,
				},
				resourceDeployment)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(fmt.Sprintf("deployments.apps \"%s\" not found", utils.InventoryResourceServerName)))

			// Check the deployment manager server does not exist.
			alarmsDeployment := &appsv1.Deployment{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      utils.InventoryAlarmServerName,
					Namespace: utils.InventoryNamespace,
				},
				alarmsDeployment)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(fmt.Sprintf("deployments.apps \"%s\" not found", utils.InventoryAlarmServerName)))
		},
	),
)
