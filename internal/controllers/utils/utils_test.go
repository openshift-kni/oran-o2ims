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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
)

// Scheme used for the tests:
var suitescheme = clientgoscheme.Scheme

func TestORANO2IMSControllerUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Utils Suite")
}

func getFakeClientFromObjects(objs ...client.Object) (client.WithWatch, error) {
	return fake.NewClientBuilder().WithScheme(suitescheme).WithObjects(objs...).WithStatusSubresource(&oranv1alpha1.ORANO2IMS{}).Build(), nil
}

var _ = Describe("ExtensionUtils", func() {
	It("The container args contain all the extensions args", func() {

		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{
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
			},
		}
		objs := []client.Object{orano2ims}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		actualArgs, err := GetServerArgs(context.TODO(), fakeClient, orano2ims, ORANO2IMSDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", defaultBackendURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
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
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
			},
		}

		objs := []client.Object{orano2ims}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		actualArgs, err := GetServerArgs(context.TODO(), fakeClient, orano2ims, ORANO2IMSDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", defaultBackendURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
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
			Spec: oranv1alpha1.ORANO2IMSSpec{},
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
		err = CreateK8sCR(context.TODO(), fakeClient,
			deployment, orano2ims, UPDATE)
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
			Spec: oranv1alpha1.ORANO2IMSSpec{},
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
		err = CreateK8sCR(context.TODO(), fakeClient,
			deployment, orano2ims, UPDATE)
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
		err = CreateK8sCR(context.TODO(), fakeClient,
			newDeployment, orano2ims, UPDATE)
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

var _ = Describe("getACMNamespace", func() {

	It("If multiclusterengine does not exist, return error", func() {
		objs := []client.Object{}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		acmNamespace, err := getACMNamespace(context.TODO(), fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("multiclusterengine object not found"))
		Expect(acmNamespace).To(Equal(""))
	})

	It("If multiclusterengine exists without the expected labels, return error", func() {
		u := &unstructured.Unstructured{}
		u.Object = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "multiclusterengine",
				"labels": map[string]interface{}{
					"installer.name": "multiclusterhub",
				},
			},
			"spec": map[string]interface{}{
				"targetNamespace": "multicluster-engine",
			},
		}

		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "multicluster.openshift.io",
			Kind:    "MultiClusterEngine",
			Version: "v1",
		})

		objs := []client.Object{u}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		acmNamespace, err := getACMNamespace(context.TODO(), fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("multiclusterengine labels do not contain the installer.namespace key"))
		Expect(acmNamespace).To(Equal(""))
	})

	It("If multiclusterengine exists with the expected labels, return the ACM namespace", func() {
		mce := &unstructured.Unstructured{}
		mce.Object = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "multiclusterengine",
				"labels": map[string]interface{}{
					"installer.name":      "multiclusterhub",
					"installer.namespace": "open-cluster-management",
				},
			},
			"spec": map[string]interface{}{
				"targetNamespace": "multicluster-engine",
			},
		}

		mce.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "multicluster.openshift.io",
			Kind:    "MultiClusterEngine",
			Version: "v1",
		})

		objs := []client.Object{mce}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		acmNamespace, err := getACMNamespace(context.TODO(), fakeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(acmNamespace).To(Equal("open-cluster-management"))
	})
})

var _ = Describe("searchAPI", func() {
	It("If there is an error in getACMNamespace, that error is returned", func() {
		mce := &unstructured.Unstructured{}
		mce.Object = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "multiclusterengine",
				"labels": map[string]interface{}{
					"installer.name": "multiclusterhub",
				},
			},
			"spec": map[string]interface{}{
				"targetNamespace": "multicluster-engine",
			},
		}

		mce.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "multicluster.openshift.io",
			Kind:    "MultiClusterEngine",
			Version: "v1",
		})

		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.apps.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, orano2ims}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, orano2ims)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("multiclusterengine labels do not contain the installer.namespace key"))
		Expect(searchAPI).To(Equal(""))
	})

	It("If the ingress host does not have the expected format (containing .apps), error is returned", func() {
		mce := &unstructured.Unstructured{}
		mce.Object = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "multiclusterengine",
				"labels": map[string]interface{}{
					"installer.name":      "multiclusterhub",
					"installer.namespace": "open-cluster-management",
				},
			},
			"spec": map[string]interface{}{
				"targetNamespace": "multicluster-engine",
			},
		}

		mce.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "multicluster.openshift.io",
			Kind:    "MultiClusterEngine",
			Version: "v1",
		})

		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.app.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, orano2ims}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, orano2ims)
		Expect(searchAPI).To(BeEmpty())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"the searchAPIBackendURL could not be obtained from the IngressHost. " +
				"Directly specify the searchAPIBackendURL in the ORANO2IMS CR or update the IngressHost"))
	})

	It("The ingress host has the expected format (containing .apps) and the searchAPI is returned", func() {
		mce := &unstructured.Unstructured{}
		mce.Object = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "multiclusterengine",
				"labels": map[string]interface{}{
					"installer.name":      "multiclusterhub",
					"installer.namespace": "open-cluster-management",
				},
			},
			"spec": map[string]interface{}{
				"targetNamespace": "multicluster-engine",
			},
		}

		mce.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "multicluster.openshift.io",
			Kind:    "MultiClusterEngine",
			Version: "v1",
		})

		orano2ims := &oranv1alpha1.ORANO2IMS{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: ORANO2IMSNamespace,
			},
			Spec: oranv1alpha1.ORANO2IMSSpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.apps.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, orano2ims}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, orano2ims)
		Expect(err).ToNot(HaveOccurred())
		Expect(searchAPI).To(Equal("https://search-api-open-cluster-management.apps.lab.karmalabs.corp"))
	})
})

var _ = Describe("ValidateJsonAgainstJsonSchema", func() {

	It("Return error if required field is missing", func() {
		schema := `
		{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type": "object",
			"properties": {
				"name": {
					"type": "string"
				},
				"age": {
					"type": "integer"
				},
				"email": {
					"type": "string",
					"format": "email"
				},
				"address": {
					"type": "object",
					"properties": {
						"street": {
							"type": "string"
						},
						"city": {
							"type": "string"
						},
						"zipcode": {
							"type": "string"
						},
						"capital": {
							"type": "boolean"
						}
					},
					"required": ["street", "city", "capital"]
				},
				"phoneNumbers": {
					"type": "array",
					"items": {
						"type": "string"
					}
				}
			},
			"required": ["name", "age"]
		}
		`
		input := `
		{
			"name": "Bob",
			"age": 35,
			"email": "bob@example.com",
			"address": {
				"street": "123 Main St",
				"city": "Springfield",
				"zipcode": "12345"
			},
			"phoneNumbers": ["123-456-7890", "987-654-3210"]
		}
		`
		err := ValidateJsonAgainstJsonSchema(schema, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("The JSON input does not match the JSON schema:  address: capital is required"))
	})

	It("Return error if field is of different type", func() {
		schema := `
		{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type": "object",
			"properties": {
				"name": {
					"type": "string"
				},
				"age": {
					"type": "integer"
				},
				"email": {
					"type": "string",
					"format": "email"
				},
				"address": {
					"type": "object",
					"properties": {
						"street": {
							"type": "string"
						},
						"city": {
							"type": "string"
						},
						"zipcode": {
							"type": "string"
						},
						"capital": {
							"type": "boolean"
						}
					},
					"required": ["street", "city"]
				},
				"phoneNumbers": {
					"type": "array",
					"items": {
						"type": "string"
					}
				}
			},
			"required": ["name", "age"]
		}
		`
		// Age is a string instead of integer.
		input := `
		{
			"name": "Bob",
			"age": "35",
			"email": "bob@example.com",
			"address": {
				"street": "123 Main St",
				"city": "Springfield",
				"zipcode": "12345"
			},
			"phoneNumbers": ["123-456-7890", "987-654-3210"]
		}
		`
		err := ValidateJsonAgainstJsonSchema(schema, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("The JSON input does not match the JSON schema:  age: Invalid type. Expected: integer, given: string"))
	})

	It("Returns success if optional field with required fields is missing", func() {
		schema := `
		{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type": "object",
			"properties": {
				"name": {
					"type": "string"
				},
				"age": {
					"type": "integer"
				},
				"email": {
					"type": "string",
					"format": "email"
				},
				"address": {
					"type": "object",
					"properties": {
						"street": {
							"type": "string"
						},
						"city": {
							"type": "string"
						},
						"zipcode": {
							"type": "string"
						},
						"capital": {
							"type": "boolean"
						}
					},
					"required": ["street", "city"]
				},
				"phoneNumbers": {
					"type": "array",
					"items": {
						"type": "string"
					}
				}
			},
			"required": ["name", "age"]
		}
		`
		// Address has required fields, but it's missing completely.
		input := `
		{
			"name": "Bob",
			"age": 35,
			"email": "bob@example.com",
			"phoneNumbers": ["123-456-7890", "987-654-3210"]
		}
		`
		err := ValidateJsonAgainstJsonSchema(schema, input)
		Expect(err).ToNot(HaveOccurred())
	})
})
