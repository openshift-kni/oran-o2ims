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
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	openshiftv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
			Spec: inventoryv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: inventoryv1alpha1.DeploymentManagerServerConfig{
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

		actualArgs, err := GetServerArgs(Inventory, InventoryDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Status.ClusterID),
			fmt.Sprintf("--backend-url=%s", defaultApiServerURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
		)
		expectedArgs = append(expectedArgs,
			"--extensions=.metadata.labels[\"name\"] as $name |\n{\n  name: $name,\n  alias: $name\n}\n",
			"--extensions={\"my\": {memory: .status.capacity.memory, k8s_version: .status.version.kubernetes}}")
		Expect(actualArgs).To(Equal(expectedArgs))
	})

	It("No extension args", func() {
		Inventory := &inventoryv1alpha1.Inventory{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oran-o2ims-sample-1",
				Namespace: InventoryNamespace,
			},
			Spec: inventoryv1alpha1.InventorySpec{
				DeploymentManagerServerConfig: inventoryv1alpha1.DeploymentManagerServerConfig{},
			},
		}

		actualArgs, err := GetServerArgs(Inventory, InventoryDeploymentManagerServerName)
		Expect(err).ToNot(HaveOccurred())
		expectedArgs := DeploymentManagerServerArgs
		expectedArgs = append(expectedArgs,
			fmt.Sprintf("--cloud-id=%s", Inventory.Status.ClusterID),
			fmt.Sprintf("--backend-url=%s", defaultApiServerURL),
			fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile),
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
			Spec: openshiftv1.IngressControllerSpec{
				Domain: "apps.example.com"}}

		objs := []client.Object{ingress}
		fakeClient := getFakeClientFromObjects(objs...)
		domain, err := GetIngressDomain(context.TODO(), fakeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(domain).To(Equal("apps.example.com"))
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
