/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package utils

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
)

var suitescheme = scheme.Scheme

func getFakeClientFromObjects(objs ...client.Object) (client.WithWatch, error) {
	return fake.NewClientBuilder().WithScheme(suitescheme).WithObjects(objs...).WithStatusSubresource(&oranv1alpha1.ORANO2IMS{}).Build(), nil
}

func TestORANO2IMSControllerUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Utils Suite")
}

var _ = Describe("ExtensionUtils", func() {
	It("The container args contain all the extensions args", func() {
		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				MetadataServer:          true,
				DeploymentManagerServer: true,
				// The below extension matches the following CRD extensions entry:
				//
				// extensions:
				// - "{\"my\": {memory: .status.capacity.memory, k8s_version: .status.version.kubernetes}}"
				// - |
				//     .metadata.labels["name"] as $name |
				// 	   {
				// 	     name: $name,
				// 	     alias: $name
				// 	   }
				Extensions: []string{
					fmt.Sprintf(
						".metadata.labels[\"name\"] as $name |\n" +
							"{\n" +
							"  name: $name,\n" +
							"  alias: $name\n" +
							"}\n"),
					"{\"my\": {memory: .status.capacity.memory, k8s_version: .status.version.kubernetes}}",
				},
			},
		}
		actualArgs := BuildDeploymentManagerServerContainerArgs(orano2ims)
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", orano2ims.Spec.BackendURL),
			fmt.Sprintf("--backend-token=%s", orano2ims.Spec.BackendToken),
			fmt.Sprintf("--backend-type=%s", orano2ims.Spec.BackendType),
		)
		expectedArgs = append(expectedArgs,
			"--extensions=.metadata.labels[\"name\"] as $name |\n{\n  name: $name,\n  alias: $name\n}\n",
			"--extensions={\"my\": {memory: .status.capacity.memory, k8s_version: .status.version.kubernetes}}")
		Expect(actualArgs).To(Equal(expectedArgs))
	})

	It("No extension args", func() {
		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				MetadataServer:          true,
				DeploymentManagerServer: true,
			},
		}

		actualArgs := BuildDeploymentManagerServerContainerArgs(orano2ims)
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", orano2ims.Spec.BackendURL),
			fmt.Sprintf("--backend-token=%s", orano2ims.Spec.BackendToken),
			fmt.Sprintf("--backend-type=%s", orano2ims.Spec.BackendType),
		)
		Expect(actualArgs).To(Equal(expectedArgs))
	})
})

var _ = Describe("DoesK8SResourceExist", func() {

	suitescheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ORANO2IMS{})
	suitescheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ORANO2IMSList{})
	suitescheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.Deployment{})
	suitescheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DeploymentList{})

	objs := []client.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "oran-o2ims",
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "metadata-server",
				Namespace: "oran-o2ims",
			},
		},
	}

	// Get a fake client.
	fakeClient, err := getFakeClientFromObjects(objs...)
	Expect(err).ToNot(HaveOccurred())

	It("If deployment does not exist, it will be created", func() {
		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				MetadataServer:          true,
				DeploymentManagerServer: true,
			},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-server",
				Namespace: "oran-o2ims",
			},
		}
		// Check that the deployment does not exist.
		k8sResourceExists, err := DoesK8SResourceExist(context.TODO(), fakeClient,
			deployment.Name, deployment.Namespace, &appsv1.Deployment{})
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sResourceExists).To(Equal(false))

		// Create the deployment.
		err = CreateK8sCR(context.TODO(), fakeClient, deployment.Name,
			deployment.Namespace, deployment, orano2ims, &appsv1.Deployment{}, suitescheme, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		// Check that the deployment has been created.
		k8sResourceExists, err = DoesK8SResourceExist(context.TODO(), fakeClient,
			deployment.Name, deployment.Namespace, &appsv1.Deployment{})
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sResourceExists).To(Equal(true))
	})

	It("If deployment does exist, it will be updated", func() {
		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				MetadataServer:          true,
				DeploymentManagerServer: true,
			},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-server-2",
				Namespace: "oran-o2ims",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: ORANO2IMSDeploymentManagerServerName,
					},
				},
			},
		}
		newDeployment := &appsv1.Deployment{}
		err := fakeClient.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			},
			newDeployment)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("deployments.apps \"deployment-server-2\" not found"))

		// Create the deployment.
		err = CreateK8sCR(context.TODO(), fakeClient, deployment.Name,
			deployment.Namespace, deployment, orano2ims, &appsv1.Deployment{}, suitescheme, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		// Check that the deployment has been created.
		err = fakeClient.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			},
			newDeployment)
		Expect(err).ToNot(HaveOccurred())

		// Update the SA Name.
		newDeployment.Spec.Template.Spec.ServiceAccountName = "new-sa-name"
		err = CreateK8sCR(context.TODO(), fakeClient, newDeployment.Name,
			newDeployment.Namespace, newDeployment, orano2ims, &appsv1.Deployment{}, suitescheme, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		// Get the deployment and check that the SA Name has been updated.
		checkDeployment := &appsv1.Deployment{}
		err = fakeClient.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      newDeployment.Name,
				Namespace: newDeployment.Namespace,
			},
			checkDeployment)
		Expect(err).ToNot(HaveOccurred())
		Expect(checkDeployment.Spec.Template.Spec.ServiceAccountName).To(Equal("new-sa-name"))
	})

	It("If resource exists, returns true", func() {
		obj := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "metadata-server",
				Namespace: "oran-o2ims",
			},
		}
		k8sResourceExists, err := DoesK8SResourceExist(context.TODO(), fakeClient, "metadata-server", "oran-o2ims", obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sResourceExists).To(Equal(true))
	})

	It("If resource does not exist, returns false", func() {
		obj := &appsv1.Deployment{}
		k8sResourceExists, err := DoesK8SResourceExist(context.TODO(), fakeClient, "metadata-server", "oran", obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sResourceExists).To(Equal(false))
	})
})
