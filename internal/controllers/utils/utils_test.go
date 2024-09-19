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
	"reflect"
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
	"sigs.k8s.io/yaml"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
)

// Scheme used for the tests:
var suitescheme = clientgoscheme.Scheme

func TestInventoryControllerUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Utils Suite")
}

//nolint:unparam
func getFakeClientFromObjects(objs ...client.Object) (client.WithWatch, error) {
	return fake.NewClientBuilder().WithScheme(suitescheme).WithObjects(objs...).WithStatusSubresource(&oranv1alpha1.Inventory{}).Build(), nil
}

var _ = Describe("ExtensionUtils", func() {
	It("The container args contain all the extensions args", func() {

		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{
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
		objs := []client.Object{Inventory}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		actualArgs, err := GetServerArgs(context.TODO(), fakeClient, Inventory, InventoryDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", defaultBackendURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
		)
		expectedArgs = append(expectedArgs,
			"--extensions=.metadata.labels[\"name\"] as $name |\n{\n  name: $name,\n  alias: $name\n}\n",
			"--extensions={\"my\": {memory: .status.capacity.memory, k8s_version: .status.version.kubernetes}}")
		Expect(actualArgs).To(Equal(expectedArgs))
	})

	It("No extension args", func() {
		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
			},
		}

		objs := []client.Object{Inventory}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		actualArgs, err := GetServerArgs(context.TODO(), fakeClient, Inventory, InventoryDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", defaultBackendURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
		)
		Expect(actualArgs).To(Equal(expectedArgs))
	})
})

var _ = Describe("DoesK8SResourceExist", func() {

	suitescheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.Inventory{})
	suitescheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.InventoryList{})
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
		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{},
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
			deployment, Inventory, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		// Check that the deployment has been created.
		k8sResourceExists, err = DoesK8SResourceExist(context.TODO(), fakeClient,
			deployment.Name, deployment.Namespace, &appsv1.Deployment{})
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sResourceExists).To(Equal(true))
	})

	It("If deployment does exist, it will be updated", func() {
		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-server-2",
				Namespace: "oran-o2ims",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: InventoryDeploymentManagerServerName,
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
			deployment, Inventory, UPDATE)
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
			newDeployment, Inventory, UPDATE)
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

		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.apps.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, Inventory}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, Inventory)
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

		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.app.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, Inventory}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, Inventory)
		Expect(searchAPI).To(BeEmpty())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"the searchAPIBackendURL could not be obtained from the IngressHost. " +
				"Directly specify the searchAPIBackendURL in the Inventory CR or update the IngressHost"))
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

		Inventory := &oranv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: oranv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: oranv1alpha1.DeploymentManagerServerConfig{},
				IngressHost:                   "o2ims.apps.lab.karmalabs.corp",
			},
		}

		objs := []client.Object{mce, Inventory}
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())
		searchAPI, err := getSearchAPI(context.TODO(), fakeClient, Inventory)
		Expect(err).ToNot(HaveOccurred())
		Expect(searchAPI).To(Equal("https://search-api-open-cluster-management.apps.lab.karmalabs.corp"))
	})
})

var testSchema = `
properties:
  additionalNTPSources:
    items:
      type: string
    type: array
  apiVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  baseDomain:
    type: string
  clusterName:
    description: ClusterName is the name of the cluster.
    type: string
  extraLabels:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  extraAnnotations:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  ingressVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  machineNetwork:
    description: MachineNetwork is the list of IP address pools for machines.
    items:
      description: MachineNetworkEntry is a single IP address block for
        node IP blocks.
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  nodes:
    items:
      description: NodeSpec
      properties:
        extraAnnotations:
          additionalProperties:
            additionalProperties:
              type: string
            type: object
          description: Additional node-level annotations to be applied
            to the rendered templates
          type: object
        hostName:
          description: Hostname is the desired hostname for the host
          type: string
        nodeLabels:
          additionalProperties:
            type: string
          type: object
        nodeNetwork:
          properties:
            config:
              type: object
              x-kubernetes-preserve-unknown-fields: true
            interfaces:
              items:
                properties:
                  macAddress:
                    type: string
                  name:
                    type: string
                required:
                - macAddress
                type: object
              minItems: 1
              type: array
          type: object
      required:
      - hostName
      type: object
    type: array
  serviceNetwork:
    items:
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  sshPublicKey:
    type: string
required:
- clusterName
- nodes
type: object
`

var _ = Describe("DisallowUnknownFieldsInSchema", func() {
	var schemaMap map[string]any

	BeforeEach(func() {
		err := yaml.Unmarshal([]byte(testSchema), &schemaMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should add 'additionalProperties': false to all objects with 'properties'", func() {
		var expected = `
additionalProperties: false
properties:
  additionalNTPSources:
    items:
      type: string
    type: array
  apiVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  baseDomain:
    type: string
  clusterName:
    description: ClusterName is the name of the cluster.
    type: string
  extraLabels:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  extraAnnotations:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  ingressVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  machineNetwork:
    description: MachineNetwork is the list of IP address pools for machines.
    items:
      description: MachineNetworkEntry is a single IP address block for
        node IP blocks.
      additionalProperties: false
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  nodes:
    items:
      description: NodeSpec
      additionalProperties: false
      properties:
        extraAnnotations:
          additionalProperties:
            additionalProperties:
              type: string
            type: object
          description: Additional node-level annotations to be applied
            to the rendered templates
          type: object
        hostName:
          description: Hostname is the desired hostname for the host
          type: string
        nodeLabels:
          additionalProperties:
            type: string
          type: object
        nodeNetwork:
          additionalProperties: false
          properties:
            config:
              type: object
              x-kubernetes-preserve-unknown-fields: true
            interfaces:
              items:
                additionalProperties: false
                properties:
                  macAddress:
                    type: string
                  name:
                    type: string
                required:
                - macAddress
                type: object
              minItems: 1
              type: array
          type: object
      required:
      - hostName
      type: object
    type: array
  serviceNetwork:
    items:
      additionalProperties: false
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  sshPublicKey:
    type: string
required:
- clusterName
- nodes
type: object
`
		// Call the function
		DisallowUnknownFieldsInSchema(schemaMap)

		var expectedSchema map[string]any
		err := yaml.Unmarshal([]byte(expected), &expectedSchema)
		Expect(err).ToNot(HaveOccurred())
		Expect(schemaMap).To(Equal(expectedSchema))
	})
})

var _ = Describe("ValidateJsonAgainstJsonSchema", func() {

	var schemaMap map[string]any

	BeforeEach(func() {
		err := yaml.Unmarshal([]byte(testSchema), &schemaMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Return error if required field is missing", func() {
		// The required field nodes[0].hostName is missing.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
serviceNetwork:
  - cidr: 172.30.0.0/16
nodes:
  - nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`
		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("invalid input: nodes.0: hostName is required"))
	})

	It("Return error if field is of different type", func() {
		// ExtraLabels - ManagedCluster is a map instead of list.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
serviceNetwork:
  - cidr: 172.30.0.0/16
extraLabels:
  ManagedCluster:
    - label1
    - label2
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("invalid input: extraLabels.ManagedCluster: Invalid type. Expected: object, given: array"))
	})

	It("Returns success if optional field with required fields is missing", func() {
		// The optional field serviceNetwork has required field - cidr, but it's missing completely.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Return error if unknown field is provided", func() {
		// clusterType is not in the schema
		input := `
clusterType: SNO
clusterName: sno1
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		schemaMap["additionalProperties"] = false
		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("Additional property clusterType is not allowed"))
	})
})

var _ = Describe("RenderTemplateForK8sCR", func() {
	var (
		clusterInstanceObj   map[string]interface{}
		expectedRenderedYaml string
	)

	BeforeEach(func() {
		clusterInstanceObj = map[string]interface{}{
			"Cluster": map[string]interface{}{
				"clusterName":            "site-sno-du-1",
				"baseDomain":             "example.com",
				"clusterImageSetNameRef": "4.16",
				"pullSecretRef":          map[string]interface{}{"name": "pullSecretName"},
				"templateRefs":           []map[string]interface{}{{"name": "aci-cluster-crs-v1", "namespace": "siteconfig-system"}},
				"additionalNTPSources":   []string{"NTP.server1", "10.16.231.22"},
				"apiVIPs":                []string{"10.0.0.1", "10.0.0.2"},
				"caBundleRef":            map[string]interface{}{"name": "my-bundle-ref"},
				"extraLabels":            map[string]map[string]string{"ManagedCluster": {"common": "true", "group-du-sno": "test", "sites": "site-sno-du-1"}},
				"clusterType":            "SNO",
				"clusterNetwork":         []map[string]interface{}{{"cidr": "10.128.0.0/14", "hostPrefix": 23}},
				"machineNetwork":         []map[string]interface{}{{"cidr": "10.16.231.0/24"}},
				"networkType":            "OVNKubernetes",
				"cpuPartitioningMode":    "AllNodes",
				"diskEncryption":         map[string]interface{}{"tang": []map[string]interface{}{{"thumbprint": "1234567890", "url": "http://10.0.0.1:7500"}}, "type": "nbde"},
				"extraManifestsRefs":     []map[string]interface{}{{"name": "foobar1"}, {"name": "foobar2"}},
				"ignitionConfigOverride": "igen",
				"installConfigOverrides": "{\"capabilities\":{\"baselineCapabilitySet\": \"None\", \"additionalEnabledCapabilities\": [ \"marketplace\", \"NodeTuning\" ] }}",
				"proxy":                  map[string]interface{}{"noProxy": "foobar"},
				"serviceNetwork":         []map[string]interface{}{{"cidr": "172.30.0.0/16"}},
				"sshPublicKey":           "ssh-rsa",
				"nodes": []map[string]interface{}{
					{
						"bmcAddress":             "idrac-virtualmedia+https://10.16.231.87/redfish/v1/Systems/System.Embedded.1",
						"bmcCredentialsName":     map[string]interface{}{"name": "node1-bmc-secret"},
						"bootMACAddress":         "00:00:00:01:20:30",
						"bootMode":               "UEFI",
						"hostName":               "node1.baseDomain.com",
						"ignitionConfigOverride": "{\"ignition\": {\"version\": \"3.1.0\"}, \"storage\": {\"files\": [{\"path\": \"/etc/containers/registries.conf\", \"overwrite\": true, \"contents\": {\"source\": \"data:text/plain;base64,aGVsbG8gZnJvbSB6dHAgcG9saWN5IGdlbmVyYXRvcg==\"}}]}}",
						"installerArgs":          "[\"--append-karg\", \"nameserver=8.8.8.8\", \"-n\"]",
						"ironicInspect":          "",
						"role":                   "master",
						"rootDeviceHint":         map[string]interface{}{"hctl": "1:2:0:0"},
						"automatedCleaningMode":  "disabled",
						"templateRefs":           []map[string]interface{}{{"name": "aci-node-crs-v1", "namespace": "siteconfig-system"}},
						"nodeNetwork": map[string]interface{}{
							"config": map[string]interface{}{
								"dns-resolver": map[string]interface{}{
									"config": map[string]interface{}{
										"server": []string{"10.19.42.41"},
									},
								},
								"interfaces": []map[string]interface{}{
									{
										"ipv4": map[string]interface{}{
											"address": []map[string]interface{}{
												{"ip": "10.16.231.3", "prefix-length": 24},
												{"ip": "10.16.231.28", "prefix-length": 24},
												{"ip": "10.16.231.31", "prefix-length": 24},
											},
											"dhcp":    false,
											"enabled": true,
										},
										"ipv6": map[string]interface{}{
											"address": []map[string]interface{}{
												{"ip": "2620:52:0:10e7:e42:a1ff:fe8a:601", "prefix-length": 64},
												{"ip": "2620:52:0:10e7:e42:a1ff:fe8a:602", "prefix-length": 64},
												{"ip": "2620:52:0:10e7:e42:a1ff:fe8a:603", "prefix-length": 64},
											},
											"dhcp":    false,
											"enabled": true,
										},
										"name": "eno1",
										"type": "ethernet",
									},
									{
										"ipv6": map[string]interface{}{
											"address": []map[string]interface{}{
												{"ip": "2620:52:0:1302::100"},
											},
											"enabled": true,
											"link-aggregation": map[string]interface{}{
												"mode": "balance-rr",
												"options": map[string]interface{}{
													"miimon": "140",
												},
												"slaves": []string{"eth0", "eth1"},
											},
											"prefix-length": 64,
										},
										"name":  "bond99",
										"state": "up",
										"type":  "bond",
									},
								},
								"routes": map[string]interface{}{
									"config": []map[string]interface{}{
										{
											"destination":        "0.0.0.0/0",
											"next-hop-address":   "10.16.231.254",
											"next-hop-interface": "eno1",
											"table":              "",
										},
									},
								},
							},
							"interfaces": []map[string]interface{}{
								{"macAddress": "00:00:00:01:20:30", "name": "eno1"},
								{"macAddress": "02:00:00:80:12:14", "name": "eth0"},
								{"macAddress": "02:00:00:80:12:15", "name": "eth1"},
							},
						},
					},
				},
			},
		}

		expectedRenderedYaml = `
apiVersion: siteconfig.open-cluster-management.io/v1alpha1
kind: ClusterInstance
metadata:
  name: site-sno-du-1
  namespace: site-sno-du-1
spec:
  additionalNTPSources:
  - NTP.server1
  - 10.16.231.22
  apiVIPs:
  - 10.0.0.1
  - 10.0.0.2
  baseDomain: example.com
  caBundleRef:
    name: my-bundle-ref
  clusterImageSetNameRef: "4.16"
  extraLabels:
    ManagedCluster:
      common: "true"
      group-du-sno: test
      sites: site-sno-du-1
  clusterName: site-sno-du-1
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
  clusterType: SNO
  cpuPartitioningMode: AllNodes
  diskEncryption:
    tang:
    - thumbprint: "1234567890"
      url: http://10.0.0.1:7500
    type: nbde
  extraManifestsRefs:
  - name: foobar1
  - name: foobar2
  holdInstallation: false
  ignitionConfigOverride: igen
  installConfigOverrides: '{"capabilities":{"baselineCapabilitySet": "None", "additionalEnabledCapabilities":
    [ "marketplace", "NodeTuning" ] }}'
  machineNetwork:
  - cidr: 10.16.231.0/24
  networkType: OVNKubernetes
  nodes:
  - automatedCleaningMode: disabled
    bmcAddress: idrac-virtualmedia+https://10.16.231.87/redfish/v1/Systems/System.Embedded.1
    bmcCredentialsName:
      name: node1-bmc-secret
    bootMACAddress: "00:00:00:01:20:30"
    bootMode: UEFI
    hostName: node1.baseDomain.com
    ignitionConfigOverride: '{"ignition": {"version": "3.1.0"}, "storage": {"files":
      [{"path": "/etc/containers/registries.conf", "overwrite": true, "contents":
      {"source": "data:text/plain;base64,aGVsbG8gZnJvbSB6dHAgcG9saWN5IGdlbmVyYXRvcg=="}}]}}'
    installerArgs: '["--append-karg", "nameserver=8.8.8.8", "-n"]'
    ironicInspect: ""
    nodeNetwork:
      config:
        dns-resolver:
          config:
            server:
            - 10.19.42.41
        interfaces:
        - ipv4:
            address:
            - ip: 10.16.231.3
              prefix-length: 24
            - ip: 10.16.231.28
              prefix-length: 24
            - ip: 10.16.231.31
              prefix-length: 24
            dhcp: false
            enabled: true
          ipv6:
            address:
            - ip: 2620:52:0:10e7:e42:a1ff:fe8a:601
              prefix-length: 64
            - ip: 2620:52:0:10e7:e42:a1ff:fe8a:602
              prefix-length: 64
            - ip: 2620:52:0:10e7:e42:a1ff:fe8a:603
              prefix-length: 64
            dhcp: false
            enabled: true
          name: eno1
          type: ethernet
        - ipv6:
            address:
            - ip: 2620:52:0:1302::100
            enabled: true
            link-aggregation:
              mode: balance-rr
              options:
                miimon: "140"
              slaves:
              - eth0
              - eth1
            prefix-length: 64
          name: bond99
          state: up
          type: bond
        routes:
          config:
          - destination: 0.0.0.0/0
            next-hop-address: 10.16.231.254
            next-hop-interface: eno1
            table: ""
      interfaces:
      - macAddress: 00:00:00:01:20:30
        name: eno1
      - macAddress: 02:00:00:80:12:14
        name: eth0
      - macAddress: 02:00:00:80:12:15
        name: eth1
    role: master
    templateRefs:
    - name: aci-node-crs-v1
      namespace: siteconfig-system
  proxy:
    noProxy: foobar
  pullSecretRef:
    name: pullSecretName
  serviceNetwork:
  - cidr: 172.30.0.0/16
  sshPublicKey: ssh-rsa
  templateRefs:
  - name: aci-cluster-crs-v1
    namespace: siteconfig-system
    `
	})

	It("Renders the cluster instance template successfully", func() {
		expectedRenderedClusterInstance := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(expectedRenderedYaml), expectedRenderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())

		renderedClusterInstance, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).ToNot(HaveOccurred())

		yamlString, err := yaml.Marshal(renderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		fmt.Println(string(yamlString))

		if !reflect.DeepEqual(renderedClusterInstance, expectedRenderedClusterInstance) {
			err = fmt.Errorf("renderedClusterInstance not equal, expected = %v, got = %v",
				renderedClusterInstance, expectedRenderedClusterInstance)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Return error if a required string field is empty", func() {
		// Update the required field baseDomain to empty string
		clusterInstanceObj["Cluster"].(map[string]any)["baseDomain"] = ""
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.baseDomain cannot be empty"))
	})

	It("Return error if a required array field is empty", func() {
		// Update the required field templateRefs to empty slice
		clusterInstanceObj["Cluster"].(map[string]any)["templateRefs"] = []string{}
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.templateRefs cannot be empty"))
	})

	It("Return error if a required map field is empty", func() {
		// Update the required field pullSecretRef to empty map
		clusterInstanceObj["Cluster"].(map[string]any)["pullSecretRef"] = map[string]any{}
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.pullSecretRef cannot be empty"))
	})

	It("Return error if a required field is not provided", func() {
		// Remove the required field hostName
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]map[string]any)[0]
		delete(node1, "hostName")

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].hostName must be provided"))
	})

	It("Return error if expected array field is not an array", func() {
		// Change the nodes.nodeNetwork.interfaces to a map
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]map[string]any)[0]
		delete(node1["nodeNetwork"].(map[string]any), "interfaces")
		node1["nodeNetwork"].(map[string]any)["interfaces"] = map[string]any{"macAddress": "00:00:00:01:20:30", "name": "eno1"}

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].nodeNetwork.interfaces must be of type array"))
	})

	It("Return error if expected map field is not a map", func() {
		// Change the nodes.nodeNetwork to string
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]map[string]any)[0]
		delete(node1, "nodeNetwork")
		node1["nodeNetwork"] = "string"

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].nodeNetwork must be of type map"))
	})
})

var _ = Describe("DeepMergeMaps and DeepMergeMapsSlices", func() {
	var (
		dst map[string]interface{}
		src map[string]interface{}
	)

	BeforeEach(func() {
		dst = make(map[string]interface{})
		src = make(map[string]interface{})
	})

	It("should merge non-conflicting keys", func() {
		dst = map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}

		src = map[string]interface{}{
			"key3": "value3",
			"key4": "value4",
		}

		expected := map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
			"key4": "value4",
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should override conflicting keys with src values", func() {
		dst = map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}

		src = map[string]interface{}{
			"key2": "new_value2",
			"key3": "value3",
		}

		expected := map[string]interface{}{
			"key1": "value1",
			"key2": "new_value2",
			"key3": "value3",
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should recursively merge nested maps", func() {
		dst = map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey1": "subvalue1",
				"subkey2": "subvalue2",
			},
		}

		src = map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey2": "new_subvalue2",
				"subkey3": "subvalue3",
			},
		}

		expected := map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey1": "subvalue1",
				"subkey2": "new_subvalue2",
				"subkey3": "subvalue3",
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should deeply merge slices of maps", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
		}

		expected := map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should append elements when src slice is longer than dst slice", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		expected := map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should preserve elements when dst slice is longer than src slice", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
		}

		expected := map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}
		err := DeepMergeMaps(dst, src, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should return error on type mismatch when checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": "value1",
		}

		src = map[string]interface{}{
			"key1": 10,
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("type mismatch for key: key1"))

		err = DeepMergeMaps(dst, src, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error if type do not match in maps when checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": map[string]interface{}{
				"subKey1": "test",
			},
		}

		src = map[string]interface{}{
			"key1": map[string]interface{}{
				"subKey1": 10,
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging maps for key: key1: type mismatch for key: subKey1 (dst: string, src: int)"))

		err = DeepMergeMaps(dst, src, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error if types do not match in slices and checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{"value1"},
		}

		src = map[string]interface{}{
			"key1": []interface{}{10},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging slices for key: key1: type mismatch at index: 0 (dst: string, src: int)"))

		err = DeepMergeMaps(dst, src, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})
	It("should return error when merging slices for key with mismatched types, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				"string_value",
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging slices for key: key1: type mismatch at index: 0 (dst: map[string]interface {}, src: string)"))

		err = DeepMergeMaps(dst, src, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error when merging maps at index with mismatched types, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": 123, // Type mismatch here
				},
			},
		}

		err := DeepMergeMaps(dst, src, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging maps at slice index: 0: type mismatch for key: subkey1"))

		err = DeepMergeMaps(dst, src, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})
})

var _ = Describe("GetLabelsForPolicies", func() {
	var (
		clusterLabels = map[string]string{}
		clusterName   = "cluster-1"
	)

	It("returns error if the clusterInstance does not have any labels", func() {

		err := CheckClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("No cluster labels configured by the ClusterInstance %s(%s)",
				clusterName, clusterName)))
	})

	It("returns error if the clusterInstance does not have the cluster-version label", func() {

		clusterLabels["clustertemplate-a-policy"] = "v1"
		err := CheckClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("Managed cluster %s is missing the cluster-version label.",
				clusterName)))
	})

	It("returns no error if the cluster-version label exists", func() {

		clusterLabels["cluster-version"] = "v4.17"
		err := CheckClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).ToNot(HaveOccurred())
	})

})

var _ = Describe("OverrideClusterInstanceLabelsOrAnnotations", func() {
	var (
		dstClusterRequestInput map[string]any
		srcConfigmap           map[string]any
	)

	BeforeEach(func() {
		dstClusterRequestInput = make(map[string]any)
		srcConfigmap = make(map[string]any)
	})

	It("should override only existing keys", func() {
		dstClusterRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "new_value1", // Existing key in dst
					"label2": "value2",     // New key, should be ignored
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation2": "value2", // New key, should be ignored
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "new_value1", // Overridden
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		err := OverrideClusterInstanceLabelsOrAnnotations(dstClusterRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstClusterRequestInput).To(Equal(expected))
	})

	It("should not add new resource types to dstClusterRequestInput", func() {
		dstClusterRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraLabels": map[string]any{
				"AgentClusterInstall": map[string]any{
					"label1": "value1", // New resource type, should be ignored
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1", // Should remain unchanged
				},
			},
			"clusterName": "cluster-1",
		}

		err := OverrideClusterInstanceLabelsOrAnnotations(dstClusterRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstClusterRequestInput).To(Equal(expected))
	})

	It("should not add extraLabels/extraAnnotations field if not found in ClusterRequestInput", func() {
		dstClusterRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraAnnotations": map[string]any{ // Field does not exist in dstClusterRequestInput
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{ // Should remain unchanged
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		err := OverrideClusterInstanceLabelsOrAnnotations(dstClusterRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstClusterRequestInput).To(Equal(expected))
	})

	It("should merge nodes and handle nested labels/annotations", func() {
		dstClusterRequestInput = map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1",
						},
					},
				},
				map[string]any{
					"hostName": "node2",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label2": "value2",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2",
						},
					},
				},
			},
		}

		srcConfigmap = map[string]any{
			"nodes": []any{
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Existing label, should be overridden
							"label2": "value2",     // New label, should be ignored
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2", // New annotation, should be ignored
						},
					},
				},
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",     // New label, should be ignored
							"label2": "new_value2", // Existing label, should be overridden
						},
					},
				},
			},
		}

		expected := map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1", // no change
						},
					},
				},
				map[string]any{
					"hostName": "node2",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label2": "new_value2", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2",
						},
					},
				},
			},
		}

		err := OverrideClusterInstanceLabelsOrAnnotations(dstClusterRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstClusterRequestInput).To(Equal(expected))
	})

	It("should not add the new node to dstClusterRequestInput", func() {
		dstClusterRequestInput = map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1",
						},
					},
				},
			},
		}

		srcConfigmap = map[string]any{
			"nodes": []any{
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Existing label, should be overridden
							"label2": "value2",     // New label, should be ignored
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2", // New annotation, should be ignored
						},
					},
				},
				// New node, should be ignored
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
							"label2": "value2",
						},
					},
				},
			},
		}

		expected := map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1", // no change
						},
					},
				},
			},
		}

		err := OverrideClusterInstanceLabelsOrAnnotations(dstClusterRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstClusterRequestInput).To(Equal(expected))
	})
})
