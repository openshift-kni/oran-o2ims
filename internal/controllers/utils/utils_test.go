/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	openshiftv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

// Scheme used for the tests:
var suitescheme = clientgoscheme.Scheme

func TestInventoryControllerUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Utils Suite")
}

//nolint:unparam
func getFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	suitescheme.AddKnownTypes(openshiftv1.SchemeGroupVersion, &openshiftv1.IngressController{})

	return fake.NewClientBuilder().
		WithScheme(suitescheme).
		WithObjects(objs...).
		WithStatusSubresource(&inventoryv1alpha1.Inventory{}).
		WithStatusSubresource(&openshiftv1.IngressController{}).
		Build()
}

var _ = Describe("ExtensionUtils", func() {
	It("The container args contain all the extensions args", func() {

		Inventory := &inventoryv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: inventoryv1alpha1.InventorySpec{},
		}

		actualArgs, err := GetServerArgs(Inventory, InventoryResourceServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := ResourceServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Status.ClusterID),
			"--global-cloud-id=undefined",
			fmt.Sprintf("--external-address=https://%s", Inventory.Status.IngressHost),
		)
		Expect(actualArgs).To(Equal(expectedArgs))
	})

	It("No extension args", func() {
		Inventory := &inventoryv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: inventoryv1alpha1.InventorySpec{
				ResourceServerConfig: &inventoryv1alpha1.ResourceServerConfig{},
			},
		}

		actualArgs, err := GetServerArgs(Inventory, InventoryResourceServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := ResourceServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Status.ClusterID),
			"--global-cloud-id=undefined",
			fmt.Sprintf("--external-address=https://%s", Inventory.Status.IngressHost),
		)
		Expect(actualArgs).To(Equal(expectedArgs))
	})
})

var _ = Describe("DoesK8SResourceExist", func() {

	suitescheme.AddKnownTypes(inventoryv1alpha1.GroupVersion, &inventoryv1alpha1.Inventory{})
	suitescheme.AddKnownTypes(inventoryv1alpha1.GroupVersion, &inventoryv1alpha1.InventoryList{})
	suitescheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.Deployment{})
	suitescheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DeploymentList{})

	objs := []client.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultNamespace,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "metadata-server",
				Namespace: constants.DefaultNamespace,
			},
		},
	}

	// Get a fake client.
	fakeClient := getFakeClientFromObjects(objs...)

	It("If deployment does not exist, it will be created", func() {
		Inventory := &inventoryv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: inventoryv1alpha1.InventorySpec{},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-server",
				Namespace: constants.DefaultNamespace,
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
		Inventory := &inventoryv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: inventoryv1alpha1.InventorySpec{},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-server-2",
				Namespace: constants.DefaultNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: InventoryResourceServerName,
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
				Namespace: constants.DefaultNamespace,
			},
		}
		k8sResourceExists, err := DoesK8SResourceExist(context.TODO(), fakeClient, "metadata-server", constants.DefaultNamespace, obj)
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

var _ = Describe("GetIngressDomain", func() {

	It("If ingress controller does not exist, return error", func() {
		objs := []client.Object{}
		fakeClient := getFakeClientFromObjects(objs...)
		domain, err := GetIngressDomain(context.TODO(), fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ingress controller object not found"))
		Expect(domain).To(Equal(""))
	})

	It("If ingress controller with proper name", func() {
		ingress := &openshiftv1.IngressController{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: "openshift-ingress-operator"},
			Status: openshiftv1.IngressControllerStatus{
				Domain: "apps.example.com"}}

		objs := []client.Object{ingress}
		fakeClient := getFakeClientFromObjects(objs...)
		domain, err := GetIngressDomain(context.TODO(), fakeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(domain).To(Equal("apps.example.com"))
	})
})

var _ = Describe("GetSearchURL", func() {

	It("If search-api service does not exist, return error", func() {
		objs := []client.Object{}
		fakeClient := getFakeClientFromObjects(objs...)
		searchURL, err := GetSearchURL(context.TODO(), fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no services found"))
		Expect(searchURL).To(Equal(""))
	})

	It("If search-api service with proper labels", func() {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "bar",
				Labels:    map[string]string{SearchApiLabelKey: SearchApiLabelValue},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{
						Port: 9999,
						Name: "test",
					},
				},
			},
		}

		objs := []client.Object{service}
		fakeClient := getFakeClientFromObjects(objs...)
		searchURL, err := GetSearchURL(context.TODO(), fakeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(searchURL).To(Equal("https://foo.bar.svc.cluster.local:9999"))
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

func Test_mapKeysToSlice(t *testing.T) {
	type args struct {
		inputMap map[string]bool
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "ok",
			args: args{
				inputMap: map[string]bool{"banana": true, "apple": false, "grape": true},
			},
			want: []string{"apple", "banana", "grape"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapKeysToSlice(tt.args.inputMap); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapKeysToSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}

var _ = Describe("GetDefaultsFromConfigMap", func() {
	var (
		c                        client.Client
		ctx                      context.Context
		configMapNamespace       = "sno-ran-du"
		configMapName            = "defaults"
		configMapKey             string
		clusterInstanceConfigMap = &corev1.ConfigMap{}
		schemaKey                string
	)
	const schema = `{
		"properties": {
		  "policyTemplateParameters": {
			"properties": {
			  "sriov-network-vlan-1": {
				"type": "string"
			  },
			  "install-plan-approval": {
				"type": "string",
				"default": "Automatic"
			  }
			}
		  },
		  "clusterInstanceParameters": {
			"properties": {
			  "additionalNTPSources": {
				"items": {
				  "type": "string"
				},
				"type": "array"
			  }
			}
		  }
		},
		"type": "object"
	  }`
	BeforeEach(func() {
		// Define the namespace.
		objs := []client.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "",
				},
			},
		}
		// Get a fake client.
		c = getFakeClientFromObjects(objs...)
		clusterInstanceConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: configMapNamespace,
			},
			Data: map[string]string{
				ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
additionalNTPSources:
- 192.168.1.1
- 192.168.1.2
templateRefs:
- name: "ai-node-templates-v1"
`,
			},
		}
	})
	It("fails when the ConfigMap is missing", func() {
		configMapKey = ClusterInstanceTemplateDefaultsConfigmapKey
		schemaKey = provisioningv1alpha1.TemplateParamClusterInstance
		result, err := GetDefaultsFromConfigMap(
			ctx, c, configMapName, configMapNamespace, configMapKey, []byte(schema), schemaKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get ConfigMap"))
		Expect(result).To(BeNil())
	})
	It("fails when it cannot obtain the expected data from the ConfigMap", func() {
		configMapKey = "other-configmap-key-than-expected"
		schemaKey = provisioningv1alpha1.TemplateParamClusterInstance
		// Create the ConfigMap.
		Expect(c.Create(ctx, clusterInstanceConfigMap)).To(Succeed())
		result, err := GetDefaultsFromConfigMap(
			ctx, c, configMapName, configMapNamespace, configMapKey, []byte(schema), schemaKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not obtain the requested field from default ConfigMap"))
		Expect(result).To(BeNil())
	})
	It("fails when it cannot obtain the expected subschema", func() {
		configMapKey = ClusterInstanceTemplateDefaultsConfigmapKey
		schemaKey = "other-subschema-key-than-expected"
		// Create the ConfigMap.
		Expect(c.Create(ctx, clusterInstanceConfigMap)).To(Succeed())
		result, err := GetDefaultsFromConfigMap(
			ctx, c, configMapName, configMapNamespace, configMapKey, []byte(schema), schemaKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("could not extract subSchema: subSchema '%s' does not exist", schemaKey)))
		Expect(result).To(BeNil())
	})
	It("return the editable and immutable fields when no errors happen", func() {
		configMapKey = ClusterInstanceTemplateDefaultsConfigmapKey
		schemaKey = provisioningv1alpha1.TemplateParamClusterInstance
		// Create the ConfigMap.
		Expect(c.Create(ctx, clusterInstanceConfigMap)).To(Succeed())
		result, err := GetDefaultsFromConfigMap(
			ctx, c, configMapName, configMapNamespace, configMapKey, []byte(schema), schemaKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result).To(Equal(
			map[string]interface{}{
				"editable": map[string]interface{}{
					"additionalNTPSources": []interface{}{
						"192.168.1.1",
						"192.168.1.2",
					},
				},
				"immutable": map[string]interface{}{
					"clusterImageSetNameRef": "4.15",
					"templateRefs": []interface{}{
						map[string]interface{}{
							"name": "ai-node-templates-v1",
						},
					},
				},
			}))
	})
})
var _ = Describe("GetDefaultsFromMaps and GetDefaultsFromSlices", func() {
	var schema string
	const basicSchema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
`
	It("handles simple maps", func() {
		schema = basicSchema
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("handles arrays in editable", func() {
		schema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
  additionalNTPSources:
    items:
      type: string
    type: array
`
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"additionalNTPSources": []interface{}{
				"192.168.10.10",
				"192.168.10.12",
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
			"additionalNTPSources": []interface{}{
				"192.168.10.10",
				"192.168.10.12",
			},
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("handles arrays in immutable", func() {
		schema = basicSchema
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"additionalNTPSources": []interface{}{
				"192.168.10.10",
				"192.168.10.12",
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
			"additionalNTPSources": []interface{}{
				"192.168.10.10",
				"192.168.10.12",
			},
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("handles arrays in bad format", func() {
		schema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
  additionalNTPSources:
    type: array
`
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"additionalNTPSources": []interface{}{
				"192.168.10.10",
				"192.168.10.12",
			},
		}
		_, _, err = GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("array type schema is missing its expected \"items\" component"))
	})
	It("handles array of maps for editable", func() {
		schema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
  serviceNetwork:
    items:
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
`
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"serviceNetwork": []interface{}{
				map[string]interface{}{
					"cidr": "172.30.0.0/1",
				},
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
			"serviceNetwork": []interface{}{
				map[string]interface{}{
					"cidr": "172.30.0.0/1",
				},
			},
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("handles array of maps for immutable", func() {
		schema = basicSchema
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"serviceNetwork": []interface{}{
				map[string]interface{}{
					"cidr": "172.30.0.0/1",
				},
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
			"serviceNetwork": []interface{}{
				map[string]interface{}{
					"cidr": "172.30.0.0/1",
				},
			},
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("splits the same map in immutable and editable", func() {
		schema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
  nodes:
    items:
      nodeNetwork:
        properties:
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
`
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"label":      constants.BootInterfaceLabel,
								"macAddress": "aa:aa:aa:aa:aa:aa",
							},
							map[string]interface{}{
								"name":       "eth1",
								"label":      "data-interface",
								"macAddress": "bb:aa:aa:aa:aa:aa",
							},
						},
					},
				},
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"macAddress": "aa:aa:aa:aa:aa:aa",
							},
							map[string]interface{}{
								"name":       "eth1",
								"macAddress": "bb:aa:aa:aa:aa:aa",
							},
						},
					},
				},
			},
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"label": constants.BootInterfaceLabel,
							},
							map[string]interface{}{
								"label": "data-interface",
							},
						},
					},
				},
			},
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
	It("handles objects that do no have the properties key", func() {
		schema = `
properties:
  cluster-logfwd-output-url:
    type: string
  cpu-isolated:
    type: string
  cpu-reserved:
    type: string
  nodes:
    items:
      nodeNetwork:
        properties:
          config:
            type: object
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
`
		yamlSchema := make(map[string]interface{})
		err := yaml.Unmarshal([]byte(schema), &yamlSchema)
		Expect(err).ToNot(HaveOccurred())
		defaultValues := map[string]interface{}{
			"cpu-isolated":         "0-1,64-65",
			"sriov-network-vlan-2": "111",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"config": map[string]interface{}{
							"routes": map[string]interface{}{
								"config": []interface{}{
									map[string]interface{}{
										"destination":        "0.0.0.0/0",
										"next-hop-interface": "eth0",
									},
								},
							},
							"interface": []interface{}{
								map[string]interface{}{
									"name": "eth0",
								},
							},
						},
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"label":      constants.BootInterfaceLabel,
								"macAddress": "aa:aa:aa:aa:aa:aa",
							},
							map[string]interface{}{
								"name":       "eth1",
								"label":      "data-interface",
								"macAddress": "bb:aa:aa:aa:aa:aa",
							},
						},
					},
				},
			},
		}
		editableExpected := map[string]interface{}{
			"cpu-isolated": "0-1,64-65",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"config": map[string]interface{}{
							"routes": map[string]interface{}{
								"config": []interface{}{
									map[string]interface{}{
										"destination":        "0.0.0.0/0",
										"next-hop-interface": "eth0",
									},
								},
							},
							"interface": []interface{}{
								map[string]interface{}{
									"name": "eth0",
								},
							},
						},
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"macAddress": "aa:aa:aa:aa:aa:aa",
							},
							map[string]interface{}{
								"name":       "eth1",
								"macAddress": "bb:aa:aa:aa:aa:aa",
							},
						},
					},
				},
			},
		}
		immutableExpected := map[string]interface{}{
			"sriov-network-vlan-2": "111",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"label": constants.BootInterfaceLabel,
							},
							map[string]interface{}{
								"label": "data-interface",
							},
						},
					},
				},
			},
		}
		editableResult, immutableResult, err := GetDefaultsFromMaps(defaultValues, yamlSchema["properties"].(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		Expect(editableResult).To(Equal(editableExpected))
		Expect(immutableResult).To(Equal(immutableExpected))
	})
})

var _ = Describe("IsHardwareConfigCompleted", func() {
	var pr *provisioningv1alpha1.ProvisioningRequest

	BeforeEach(func() {
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pr",
				Namespace: "test-namespace",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Conditions: []metav1.Condition{},
			},
		}
	})

	Context("when HardwareConfigured condition does not exist", func() {
		It("should return true (considered completed for initial provisioning)", func() {
			result := IsHardwareConfigCompleted(pr)
			Expect(result).To(BeTrue())
		})
	})

	Context("when HardwareConfigured condition exists with status True", func() {
		BeforeEach(func() {
			pr.Status.Conditions = []metav1.Condition{
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
					Status: metav1.ConditionTrue,
					Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
				},
			}
		})

		It("should return true", func() {
			result := IsHardwareConfigCompleted(pr)
			Expect(result).To(BeTrue())
		})
	})

	Context("when HardwareConfigured condition exists with status False", func() {
		BeforeEach(func() {
			pr.Status.Conditions = []metav1.Condition{
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
					Status: metav1.ConditionFalse,
					Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				},
			}
		})

		It("should return false", func() {
			result := IsHardwareConfigCompleted(pr)
			Expect(result).To(BeFalse())
		})
	})

	Context("when HardwareConfigured condition exists with status Unknown", func() {
		BeforeEach(func() {
			pr.Status.Conditions = []metav1.Condition{
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
					Status: metav1.ConditionUnknown,
					Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				},
			}
		})

		It("should return false", func() {
			result := IsHardwareConfigCompleted(pr)
			Expect(result).To(BeFalse())
		})
	})

	Context("when multiple conditions exist including HardwareConfigured", func() {
		BeforeEach(func() {
			pr.Status.Conditions = []metav1.Condition{
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
					Status: metav1.ConditionTrue,
					Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
				},
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
					Status: metav1.ConditionTrue,
					Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
				},
				{
					Type:   string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
					Status: metav1.ConditionFalse,
					Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				},
			}
		})

		It("should return true when HardwareConfigured is True", func() {
			result := IsHardwareConfigCompleted(pr)
			Expect(result).To(BeTrue())
		})
	})
})
