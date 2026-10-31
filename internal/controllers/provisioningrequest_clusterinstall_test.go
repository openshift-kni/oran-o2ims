/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Overview for provisioningrequest_clusterinstall_test.go:

1. handleRenderClusterInstance Tests:
   - Tests successful rendering and validation of ClusterInstance with dry-run
   - Verifies disable-auto-import annotation behavior for ManagedCluster:
     * Added when cluster provisioning has not started
     * Not added when cluster provisioning has completed
   - Tests failure scenarios with invalid input data (empty clusterName)
   - Validates status condition updates for ClusterInstanceRendered

2. handleClusterInstallation Tests:
   - Validates that ClusterInstance is applied with correct parameters
   - Tests that ClusterDetails.Name is set in status when applying ClusterInstance

3. TestExtractNodeDetails Tests:
   - Tests extraction of node details from existing ClusterInstance unstructured data
   - Covers various scenarios:
     * Valid input with complete node information (hostname, BMC, MAC addresses)
     * Missing hostname (nodes should be skipped)
     * Missing BMC address and boot MAC address
     * Missing BMC credentials name or nested name field
     * Missing node network interfaces
     * Invalid interface data (missing name or MAC address fields)
   - Validates proper handling of nodeInfo structure population

4. TestAssignNodeDetails Tests:
   - Tests assignment of node details to rendered ClusterInstance
   - Covers scenarios:
     * Valid assignment of BMC details, MAC addresses, and credentials
     * Missing spec.nodes section (should not panic)
     * Nodes with missing hostname (should be skipped)
     * Missing interface MAC address information in nodeInfo
     * Proper setting of hostRef fields (name and namespace)
   - Validates that unstructured.Unstructured objects are modified correctly

5. checkNodesAndMCPsReady tests:
   - Checks requested node readiness and intended MachineConfigPool membership
   - Waits for desired worker pool counts and pool-wide configuration convergence
   - Covers default and custom worker pools
*/

package controllers

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/openshift-kni/oran-o2ims/internal/spokeclient"
	"github.com/openshift-kni/oran-o2ims/internal/validation"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
)

// getClusterInstanceDefaultsConfigMap returns a ConfigMap with ClusterInstance defaults
func getClusterInstanceDefaultsConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
holdInstallation: false
templateRefs:
  - name: "ai-cluster-templates-v1"
    namespace: "siteconfig-operator"
nodeGroups:
- name: master
  role: master
  ironicInspect: ""
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
    config:
      interfaces:
      - name: eno1
        type: ethernet
        state: up
        ipv4:
          enabled: true
        ipv6:
          enabled: false
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"`,
		},
	}
}

var _ = Describe("handleRenderClusterInstance", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ProvisioningRequestReconciler
		task         *provisioningRequestReconcilerTask
		cr           *provisioningv1alpha1.ProvisioningRequest
		ciDefaultsCm = "clusterinstance-defaults-v1"
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		// Define the cluster template.
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		// Use helper function to get ConfigMap with ClusterInstance defaults
		cm := getClusterInstanceDefaultsConfigMap(ciDefaultsCm, ctNamespace)

		c = fakeclient.GetFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
			},
		}

		clusterInstanceInputParams, err := validation.ExtractMatchingInput(
			cr.Spec.TemplateParameters.Raw, constants.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		mergedClusterInstanceData, err := task.getMergedClusterInstanceData(
			ctx, ciDefaultsCm, clusterInstanceInputParams.(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		task.clusterInput.clusterInstanceData = mergedClusterInstanceData
	})

	It("should successfully render and validate ClusterInstance with dry-run", func() {
		renderedClusterInstance, err := task.handleRenderClusterInstance(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedClusterInstance).ToNot(BeNil())

		// Verify the disable-auto-import annotation is added to the ManagedCluster
		// when cluster provisioning has not started yet.
		Expect(renderedClusterInstance.Spec.ExtraAnnotations).To(HaveKey("ManagedCluster"))
		Expect(renderedClusterInstance.Spec.ExtraAnnotations["ManagedCluster"]).To(HaveKey(disableAutoImportAnnotation))

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		testutils.VerifyStatusCondition(*cond, metav1.Condition{
			Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionTrue,
			Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
			Message: "ClusterInstance rendered and passed dry-run validation",
		})
	})

	It("should not contain disable-auto-import annotation for ManagedCluster in the "+
		"rendered ClusterInstance if cluster provisioning has completed", func() {
		// Simulate that the ClusterInstance has started provisioning
		task.object.Status.Conditions = []metav1.Condition{
			{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "Provisioned cluster",
			},
		}
		renderedClusterInstance, err := task.handleRenderClusterInstance(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedClusterInstance).ToNot(BeNil())

		// Verify the disable-auto-import annotation is not added to the ManagedCluster
		// when cluster provisioning has completed.
		Expect(renderedClusterInstance.Spec.ExtraAnnotations).ToNot(HaveKey("ManagedCluster"))

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		testutils.VerifyStatusCondition(*cond, metav1.Condition{
			Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionTrue,
			Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
			Message: "ClusterInstance rendered and passed dry-run validation",
		})
	})

	It("should fail to render ClusterInstance due to invalid input", func() {
		// Modify input data to be invalid
		task.clusterInput.clusterInstanceData["clusterName"] = ""
		_, err := task.handleRenderClusterInstance(ctx)
		Expect(err).To(HaveOccurred())

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		testutils.VerifyStatusCondition(*cond, metav1.Condition{
			Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionFalse,
			Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
			Message: "spec.clusterName cannot be empty",
		})
	})

})

var _ = Describe("handleClusterInstallation", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ProvisioningRequestReconciler
		task         *provisioningRequestReconcilerTask
		cr           *provisioningv1alpha1.ProvisioningRequest
		ciDefaultsCm = "clusterinstance-defaults-v1"
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		// Define the cluster template.
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		// Use helper function to get ConfigMap with ClusterInstance defaults
		cm := getClusterInstanceDefaultsConfigMap(ciDefaultsCm, ctNamespace)

		c = fakeclient.GetFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
			},
		}

		clusterInstanceInputParams, err := validation.ExtractMatchingInput(
			cr.Spec.TemplateParameters.Raw, constants.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		mergedClusterInstanceData, err := task.getMergedClusterInstanceData(
			ctx, ciDefaultsCm, clusterInstanceInputParams.(map[string]any))
		Expect(err).ToNot(HaveOccurred())
		task.clusterInput.clusterInstanceData = mergedClusterInstanceData
	})

	It("should apply ClusterInstance with correct parameters", func() {
		// Build unstructured ClusterInstance
		renderedCIUnstructured, err := task.buildClusterInstanceUnstructured()
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedCIUnstructured).ToNot(BeNil())

		clusterName := renderedCIUnstructured.GetName()

		// Apply the ClusterInstance
		_ = task.handleClusterInstallation(ctx, renderedCIUnstructured)

		// Verify ClusterInstance was applied by checking if it exists
		appliedCI := &unstructured.Unstructured{}
		appliedCI.SetGroupVersionKind(renderedCIUnstructured.GroupVersionKind())
		err = c.Get(ctx, client.ObjectKey{Name: clusterName, Namespace: clusterName}, appliedCI)
		Expect(err).ToNot(HaveOccurred())
		Expect(appliedCI.GetName()).To(Equal(clusterName))
	})

	It("should set ClusterDetails.Name in status when applying ClusterInstance", func() {
		// Build unstructured ClusterInstance
		renderedCIUnstructured, err := task.buildClusterInstanceUnstructured()
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedCIUnstructured).ToNot(BeNil())

		clusterName := task.clusterInput.clusterInstanceData["clusterName"].(string)

		// Verify ClusterDetails is not set before applying
		Expect(task.object.Status.Extensions.ClusterDetails).To(BeNil())

		// Apply the ClusterInstance (this will set ClusterDetails)
		_ = task.handleClusterInstallation(ctx, renderedCIUnstructured)

		// Fetch the ProvisioningRequest from the client and verify ClusterDetails.Name is set
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
		err = c.Get(ctx, client.ObjectKey{Name: crName}, provisioningRequest)
		Expect(err).ToNot(HaveOccurred())
		Expect(provisioningRequest.Status.Extensions.ClusterDetails).ToNot(BeNil())
		Expect(provisioningRequest.Status.Extensions.ClusterDetails.Name).To(Equal(clusterName))
	})
})

// A spec-level cpuArchitecture set in the ProvisioningRequest input or the
// clusterInstanceDefaults ConfigMap must reach the rendered ClusterInstance
// spec. Otherwise siteconfig applies its x86_64 CRD default and ARM (aarch64)
// deployments fail with an image-architecture mismatch.
var _ = Describe("cpuArchitecture propagation into rendered ClusterInstance", func() {
	var (
		ctx          context.Context
		ciDefaultsCm = "clusterinstance-defaults-v1"
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		crName       = "cluster-1"
	)

	// buildTask wires a task with the given ClusterInstance defaults ConfigMap so
	// the merge/render path can be exercised end-to-end.
	buildTask := func(cm *corev1.ConfigMap) *provisioningRequestReconcilerTask {
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: crName},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		c := fakeclient.GetFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		return &provisioningRequestReconcilerTask{
			logger:       logger,
			client:       c,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails:    &clusterTemplateDetails{namespace: ctNamespace},
		}
	}

	// prInput returns the clusterInstanceParameters supplied by the sample
	// ProvisioningRequest.
	prInput := func() map[string]any {
		input, err := validation.ExtractMatchingInput(
			[]byte(testutils.TestFullTemplateParameters), constants.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		return input.(map[string]any)
	}

	// renderSpec merges the input with the defaults and returns the rendered
	// ClusterInstance spec.
	renderSpec := func(task *provisioningRequestReconcilerTask, input map[string]any) map[string]any {
		merged, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, input)
		Expect(err).ToNot(HaveOccurred())
		task.clusterInput.clusterInstanceData = merged
		rendered, err := task.buildClusterInstanceUnstructured()
		Expect(err).ToNot(HaveOccurred())
		spec, ok := rendered.Object["spec"].(map[string]any)
		Expect(ok).To(BeTrue())
		return spec
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("renders spec.cpuArchitecture from the ProvisioningRequest input", func() {
		task := buildTask(getClusterInstanceDefaultsConfigMap(ciDefaultsCm, ctNamespace))
		input := prInput()
		input["cpuArchitecture"] = "aarch64"
		spec := renderSpec(task, input)
		Expect(spec["cpuArchitecture"]).To(Equal("aarch64"))
	})

	It("renders spec.cpuArchitecture from the clusterInstance defaults ConfigMap", func() {
		cm := getClusterInstanceDefaultsConfigMap(ciDefaultsCm, ctNamespace)
		cm.Data[utils.ClusterInstanceTemplateDefaultsConfigmapKey] =
			"cpuArchitecture: aarch64\n" + cm.Data[utils.ClusterInstanceTemplateDefaultsConfigmapKey]
		task := buildTask(cm)
		spec := renderSpec(task, prInput())
		Expect(spec["cpuArchitecture"]).To(Equal("aarch64"))
	})

	It("leaves cpuArchitecture unset when neither input nor defaults provide it", func() {
		// Documents the root cause: with nothing to pass through, siteconfig's CRD
		// default (x86_64) applies, which breaks ARM deployments.
		task := buildTask(getClusterInstanceDefaultsConfigMap(ciDefaultsCm, ctNamespace))
		spec := renderSpec(task, prInput())
		_, present := spec["cpuArchitecture"]
		Expect(present).To(BeFalse())
	})
})

// ptr is a helper function to create pointers
func ptr[T any](v T) *T {
	return &v
}

func TestExtractNodeDetails(t *testing.T) {
	tests := []struct {
		name       string
		existingCI *unstructured.Unstructured
		expected   map[string]nodeInfo
	}{
		{
			name: "Valid Input - Extract Node Details",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName":       "node1",
								"bmcAddress":     "192.168.1.1",
								"bootMACAddress": "AA:BB:CC:DD:EE:FF",
								"bmcCredentialsName": map[string]any{
									"name": "bmc-secret",
								},
								"nodeNetwork": map[string]any{
									"interfaces": []any{
										map[string]any{
											"name":       "eth0",
											"macAddress": "00:11:22:33:44:55",
										},
										map[string]any{
											"name":       "eth1",
											"macAddress": "00:11:22:33:44:66",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{
				"node1": {
					bmcAddress:         ptr("192.168.1.1"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcCredentialsName: ptr("bmc-secret"),
					interfaceMACAddress: map[string]string{
						"eth0": "00:11:22:33:44:55",
						"eth1": "00:11:22:33:44:66",
					},
				},
			},
		},
		{
			name: "Missing hostName - Should be skipped",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{ // No hostName
								"bmcAddress": "192.168.1.1",
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{}, // No nodes extracted
		},
		{
			name: "Missing bmcAddress and bootMACAddress",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName": "node1",
								"bmcCredentialsName": map[string]any{
									"name": "bmc-secret",
								},
								"nodeNetwork": map[string]any{
									"interfaces": []any{
										map[string]any{
											"name":       "eth0",
											"macAddress": "00:11:22:33:44:55",
										},
										map[string]any{
											"name":       "eth1",
											"macAddress": "00:11:22:33:44:66",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{
				"node1": {
					bmcCredentialsName: ptr("bmc-secret"),
					interfaceMACAddress: map[string]string{
						"eth0": "00:11:22:33:44:55",
						"eth1": "00:11:22:33:44:66",
					},
				},
			},
		},
		{
			name: "Missing bmcCredentialsName or bmcCredentialsName.name",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName":           "node1",
								"bmcCredentialsName": map[string]any{
									// "name" key is missing
								},
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{
				"node1": {},
			},
		},
		{
			name: "Missing nodeNetwork.interfaces",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName":       "node1",
								"bmcAddress":     "192.168.1.1",
								"bootMACAddress": "AA:BB:CC:DD:EE:FF",
								"bmcCredentialsName": map[string]any{
									"name": "bmc-secret",
								},
								"nodeNetwork": map[string]any{
									// "interfaces" key is missing
								},
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{
				"node1": {
					bmcAddress:         ptr("192.168.1.1"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcCredentialsName: ptr("bmc-secret"),
				},
			},
		},
		{
			name: "Invalid Interface Data (missing name or macAddress)",
			existingCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName": "node1",
								"nodeNetwork": map[string]any{
									"interfaces": []any{
										map[string]any{ // Missing name
											"macAddress": "00:11:22:33:44:55",
										},
										map[string]any{ // Missing macAddress
											"name": "eth1",
										},
										map[string]any{ // Valid entry
											"name":       "eth2",
											"macAddress": "00:11:22:33:44:77",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string]nodeInfo{
				"node1": {
					interfaceMACAddress: map[string]string{
						"eth2": "00:11:22:33:44:77", // Only the valid entry should be included
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractNodeDetails(tt.existingCI)

			// Compare the extracted result with expected output
			if !reflect.DeepEqual(tt.expected, result) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestAssignNodeDetails(t *testing.T) {
	tests := []struct {
		name       string
		renderedCI *unstructured.Unstructured
		nodesInfo  map[string]nodeInfo
		expected   map[string]any
	}{
		{
			name: "Valid Input - Assign Node Details",
			renderedCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName": "node1",
								"nodeNetwork": map[string]any{
									"interfaces": []any{
										map[string]any{
											"name":       "eth0",
											"macAddress": "old-mac",
										},
									},
								},
							},
						},
					},
				},
			},
			nodesInfo: map[string]nodeInfo{
				"node1": {
					bmcAddress:         ptr("192.168.1.1"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcCredentialsName: ptr("bmc-secret"),
					interfaceMACAddress: map[string]string{
						"eth0": "00:11:22:33:44:55",
					},
				},
			},
			expected: map[string]any{
				"spec": map[string]any{
					"nodes": []any{
						map[string]any{
							"hostName":       "node1",
							"bmcAddress":     "192.168.1.1",
							"bootMACAddress": "AA:BB:CC:DD:EE:FF",
							"bmcCredentialsName": map[string]any{
								"name": "bmc-secret",
							},
							"nodeNetwork": map[string]any{
								"interfaces": []any{
									map[string]any{
										"name":       "eth0",
										"macAddress": "00:11:22:33:44:55", // Updated MAC
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Missing spec.nodes - Should not panic",
			renderedCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{},
				},
			},
			nodesInfo: map[string]nodeInfo{
				"node1": {
					bmcAddress:         ptr("192.168.1.1"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcCredentialsName: ptr("bmc-secret"),
					interfaceMACAddress: map[string]string{
						"eth0": "00:11:22:33:44:55",
					},
				},
			},
			expected: map[string]any{
				"spec": map[string]any{}, // No changes
			},
		},
		{
			name: "Node with missing hostName - Should be skipped",
			renderedCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{ // No hostName
								"nodeNetwork": map[string]any{
									"interfaces": []any{
										map[string]any{
											"name":       "eth0",
											"macAddress": "old-mac",
										},
									},
								},
							},
						},
					},
				},
			},
			nodesInfo: map[string]nodeInfo{
				"node1": {
					bmcAddress:         ptr("192.168.1.1"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcCredentialsName: ptr("bmc-secret"),
					interfaceMACAddress: map[string]string{
						"eth0": "00:11:22:33:44:55",
					},
				},
			},
			expected: map[string]any{
				"spec": map[string]any{
					"nodes": []any{ // No changes, since hostName is missing
						map[string]any{
							"nodeNetwork": map[string]any{
								"interfaces": []any{
									map[string]any{
										"name":       "eth0",
										"macAddress": "old-mac",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Missing interfacesMACAddress in the nodeInfo",
			renderedCI: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"nodes": []any{
							map[string]any{
								"hostName": "node1",
							},
						},
					},
				},
			},
			nodesInfo: map[string]nodeInfo{
				"node1": {
					bmcCredentialsName: ptr("bmc-secret"),
					bootMACAddress:     ptr("AA:BB:CC:DD:EE:FF"),
					bmcAddress:         ptr("192.168.1.1"),
					HwMgrNodeId:        "test",
					HwMgrNodeNs:        "test",
				},
			},
			expected: map[string]any{
				"spec": map[string]any{
					"nodes": []any{
						map[string]any{
							"hostName":       "node1",
							"bootMACAddress": "AA:BB:CC:DD:EE:FF",
							"bmcAddress":     "192.168.1.1",
							"bmcCredentialsName": map[string]any{
								"name": "bmc-secret",
							},
							"hostRef": map[string]any{
								"name":      "test",
								"namespace": "test",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignNodeDetails(tt.renderedCI, tt.nodesInfo)

			// Compare modified renderedCI with expected output
			if !reflect.DeepEqual(tt.expected, tt.renderedCI.Object) {
				t.Errorf("expected %v, got %v", tt.expected, tt.renderedCI.Object)
			}
		})
	}
}

var _ = Describe("validateScaleWorkerOnly", func() {
	makeCI := func(nodes []map[string]any) *unstructured.Unstructured {
		ci := &unstructured.Unstructured{}
		ci.Object = map[string]any{
			"spec": map[string]any{
				"nodes": func() []any {
					result := make([]any, len(nodes))
					for i, n := range nodes {
						result[i] = n
					}
					return result
				}(),
			},
		}
		return ci
	}

	It("should allow adding worker nodes", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker2", "role": "worker"},
		})
		Expect(validateScaleWorkerOnly(existingCI, renderedCI)).To(Succeed())
	})

	It("should allow removing worker nodes", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker2", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		Expect(validateScaleWorkerOnly(existingCI, renderedCI)).To(Succeed())
	})

	It("should allow replacing a worker node (simultaneous add and remove)", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker2", "role": "worker"},
		})
		Expect(validateScaleWorkerOnly(existingCI, renderedCI)).To(Succeed())
	})

	It("should reject adding master nodes", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "master2", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		err := validateScaleWorkerOnly(existingCI, renderedCI)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot add master node"))
	})

	It("should reject removing master nodes", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "master2", "role": "master"},
			{"hostName": "master3", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "master2", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		err := validateScaleWorkerOnly(existingCI, renderedCI)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot remove master node"))
		Expect(err.Error()).To(ContainSubstring("master3"))
	})

	It("should reject scaling on single-node clusters", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		err := validateScaleWorkerOnly(existingCI, renderedCI)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not supported on single-node clusters"))
	})

	It("should pass when no scaling occurred", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		Expect(validateScaleWorkerOnly(existingCI, renderedCI)).To(Succeed())
	})

	It("should reject in-place role change from worker to master", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "master"},
		})
		err := validateScaleWorkerOnly(existingCI, renderedCI)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("in-place role change is not supported"))
		Expect(err.Error()).To(ContainSubstring("worker1"))
	})

	It("should reject in-place role change from master to worker", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "worker"},
			{"hostName": "worker1", "role": "worker"},
		})
		err := validateScaleWorkerOnly(existingCI, renderedCI)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("in-place role change is not supported"))
		Expect(err.Error()).To(ContainSubstring("master1"))
	})
})

var _ = Describe("scale-out detection via getNodeRolesByHostname", func() {
	makeCI := func(nodes []map[string]any) *unstructured.Unstructured {
		ci := &unstructured.Unstructured{}
		ci.Object = map[string]any{
			"spec": map[string]any{
				"nodes": func() []any {
					result := make([]any, len(nodes))
					for i, n := range nodes {
						result[i] = n
					}
					return result
				}(),
			},
		}
		return ci
	}

	It("should detect scale-out when rendered CI has more nodes than existing CI", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "master2", "role": "master"},
			{"hostName": "master3", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "master2", "role": "master"},
			{"hostName": "master3", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker2", "role": "worker"},
		})
		existingCount := len(getNodeRolesByHostname(existingCI))
		renderedCount := len(getNodeRolesByHostname(renderedCI))
		Expect(renderedCount).To(BeNumerically(">", existingCount))
	})

	It("should not detect scale-out when node counts are equal", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		existingCount := len(getNodeRolesByHostname(existingCI))
		renderedCount := len(getNodeRolesByHostname(renderedCI))
		Expect(renderedCount).To(Equal(existingCount))
	})
})

var _ = Describe("buildIntermediateCIWithPruneManifests", func() {
	makeCI := func(nodes []map[string]any) *unstructured.Unstructured {
		ci := &unstructured.Unstructured{}
		ci.Object = map[string]any{
			"spec": map[string]any{
				"nodes": func() []any {
					result := make([]any, len(nodes))
					for i, n := range nodes {
						result[i] = n
					}
					return result
				}(),
			},
		}
		return ci
	}

	It("should add removed node entries with pruneManifests", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master", "bmcAddress": "redfish://1.2.3.4"},
			{"hostName": "worker1", "role": "worker", "bmcAddress": "redfish://1.2.3.5"},
			{"hostName": "worker2", "role": "worker", "bmcAddress": "redfish://1.2.3.6"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master", "bmcAddress": "redfish://1.2.3.4"},
			{"hostName": "worker1", "role": "worker", "bmcAddress": "redfish://1.2.3.5"},
		})

		result := buildIntermediateCIWithPruneManifests(renderedCI, existingCI, []string{"worker2"})

		spec := result.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		Expect(nodes).To(HaveLen(3))

		// The third node (worker2) should have pruneManifests
		worker2 := nodes[2].(map[string]any)
		Expect(worker2["hostName"]).To(Equal("worker2"))
		Expect(worker2["pruneManifests"]).ToNot(BeNil())

		prune := worker2["pruneManifests"].([]any)
		Expect(prune).To(HaveLen(3))
		Expect(prune[0].(map[string]any)["kind"]).To(Equal("InfraEnv"))
		Expect(prune[1].(map[string]any)["kind"]).To(Equal("NMStateConfig"))
		Expect(prune[2].(map[string]any)["kind"]).To(Equal("BareMetalHost"))
	})

	It("should not modify the original rendered CI", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
		})

		result := buildIntermediateCIWithPruneManifests(renderedCI, existingCI, []string{"worker1"})

		// Original rendered CI should still have 1 node
		origSpec := renderedCI.Object["spec"].(map[string]any)
		origNodes := origSpec["nodes"].([]any)
		Expect(origNodes).To(HaveLen(1))

		// Result should have 2 nodes
		resultSpec := result.Object["spec"].(map[string]any)
		resultNodes := resultSpec["nodes"].([]any)
		Expect(resultNodes).To(HaveLen(2))
	})

	It("should handle multiple removed nodes", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker2", "role": "worker"},
			{"hostName": "worker3", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})

		result := buildIntermediateCIWithPruneManifests(renderedCI, existingCI, []string{"worker2", "worker3"})

		spec := result.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		Expect(nodes).To(HaveLen(4))

		// Both removed nodes should have pruneManifests
		for _, node := range nodes[2:] {
			nodeMap := node.(map[string]any)
			Expect(nodeMap["pruneManifests"]).ToNot(BeNil())
		}
	})

	It("should exclude new nodes not in existing CI (swap case)", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker2", "role": "worker"},
		})
		// Swap: remove worker2, add worker3
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
			{"hostName": "worker3", "role": "worker"},
		})

		result := buildIntermediateCIWithPruneManifests(renderedCI, existingCI, []string{"worker2"})

		spec := result.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		// Should have: master1, worker1 (kept), worker2 (pruned) = 3 nodes
		// worker3 should be excluded (not in existing CI)
		Expect(nodes).To(HaveLen(3))

		hostnames := make([]string, len(nodes))
		for i, n := range nodes {
			hostnames[i] = n.(map[string]any)["hostName"].(string)
		}
		Expect(hostnames).To(ContainElements("master1", "worker1", "worker2"))
		Expect(hostnames).ToNot(ContainElement("worker3"))

		// worker2 should have pruneManifests
		worker2 := nodes[2].(map[string]any)
		Expect(worker2["hostName"]).To(Equal("worker2"))
		Expect(worker2["pruneManifests"]).ToNot(BeNil())
	})

	It("should include BMH in pruneManifests to clear stale manifestsRendered", func() {
		existingCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
			{"hostName": "worker1", "role": "worker"},
		})
		renderedCI := makeCI([]map[string]any{
			{"hostName": "master1", "role": "master"},
		})

		result := buildIntermediateCIWithPruneManifests(renderedCI, existingCI, []string{"worker1"})

		spec := result.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		worker1 := nodes[1].(map[string]any)
		prune := worker1["pruneManifests"].([]any)

		Expect(prune).To(HaveLen(3))
		kinds := make([]string, len(prune))
		for i, entry := range prune {
			kinds[i] = entry.(map[string]any)["kind"].(string)
		}
		Expect(kinds).To(ContainElements("InfraEnv", "NMStateConfig", "BareMetalHost"))
	})
})

var _ = Describe("getAgentHostname", func() {
	It("should extract hostname from spec.hostname", func() {
		agent := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"hostname": "worker1.cluster.example.com",
				},
			},
		}
		Expect(getAgentHostname(agent)).To(Equal("worker1.cluster.example.com"))
	})

	It("should fall back to status.inventory.hostname", func() {
		agent := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{},
				"status": map[string]any{
					"inventory": map[string]any{
						"hostname": "worker2.cluster.example.com",
					},
				},
			},
		}
		Expect(getAgentHostname(agent)).To(Equal("worker2.cluster.example.com"))
	})

	It("should prefer spec.hostname over status.inventory.hostname", func() {
		agent := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"hostname": "from-spec",
				},
				"status": map[string]any{
					"inventory": map[string]any{
						"hostname": "from-status",
					},
				},
			},
		}
		Expect(getAgentHostname(agent)).To(Equal("from-spec"))
	})

	It("should return empty string when no hostname is found", func() {
		agent := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{},
			},
		}
		Expect(getAgentHostname(agent)).To(BeEmpty())
	})
})

var _ = Describe("handleScaleInAbort", func() {
	var (
		ctx  context.Context
		task *provisioningRequestReconcilerTask
		nar  *hwmgmtv1alpha1.NodeAllocationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		nar = &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pr",
				Namespace: "oran-o2ims",
			},
		}
	})

	It("should remove scale-in annotation from NAR when aborting", func() {
		nar.Annotations = map[string]string{
			hwmgrcontroller.ScaleInNodesAnnotation: "node-1",
		}

		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
		}

		c := fakeclient.GetFakeClientFromObjects(pr, nar)
		task = &provisioningRequestReconcilerTask{
			logger: logger,
			client: c,
			object: pr,
		}

		Expect(task.handleScaleInAbort(ctx)).To(Succeed())

		// Verify annotation was removed
		updatedNAR := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updatedNAR)).To(Succeed())
		Expect(updatedNAR.GetAnnotations()).ToNot(HaveKey(hwmgrcontroller.ScaleInNodesAnnotation))
	})

	It("should be a no-op when no annotation is present", func() {
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
		}

		c := fakeclient.GetFakeClientFromObjects(pr, nar)
		task = &provisioningRequestReconcilerTask{
			logger: logger,
			client: c,
			object: pr,
		}

		Expect(task.handleScaleInAbort(ctx)).To(Succeed())
	})

	It("should be a no-op when NAR does not exist", func() {
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
		}

		c := fakeclient.GetFakeClientFromObjects(pr)
		task = &provisioningRequestReconcilerTask{
			logger: logger,
			client: c,
			object: pr,
		}

		Expect(task.handleScaleInAbort(ctx)).To(Succeed())
	})

	It("should remove annotation when hostmap is populated but spoke is gone", func() {
		nar.Annotations = map[string]string{
			hwmgrcontroller.ScaleInNodesAnnotation: "node-1,node-2",
		}

		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{
						Name: "test-cluster",
					},
					AllocatedNodeHostMap: map[string]string{
						"node-1": "worker-1.example.com",
						"node-2": "worker-2.example.com",
					},
				},
			},
		}

		// No kubeconfig secret — spoke cluster is gone
		c := fakeclient.GetFakeClientFromObjects(pr, nar)
		task = &provisioningRequestReconcilerTask{
			logger: logger,
			client: c,
			object: pr,
		}

		Expect(task.handleScaleInAbort(ctx)).To(Succeed())

		// Verify annotation was still removed even though spoke is unavailable
		updatedNAR := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updatedNAR)).To(Succeed())
		Expect(updatedNAR.GetAnnotations()).ToNot(HaveKey(hwmgrcontroller.ScaleInNodesAnnotation))
	})
})

var _ = Describe("checkNodesAndMCPsReady", func() {
	const (
		clusterName     = "readiness-cluster"
		customPoolName  = "worker-custom"
		customRoleLabel = "node-role.kubernetes.io/" + customPoolName
	)
	var (
		ctx        context.Context
		task       *provisioningRequestReconcilerTask
		ci         *unstructured.Unstructured
		pool       *mcfgv1.MachineConfigPool
		spoke      client.Client
		workerNode *corev1.Node
	)

	BeforeEach(func() {
		ctx = context.Background()
		spokeclient.ClearCache()
		DeferCleanup(spokeclient.ClearCache)
		pool = &mcfgv1.MachineConfigPool{
			ObjectMeta: metav1.ObjectMeta{Name: customPoolName, Generation: 1},
			Spec: mcfgv1.MachineConfigPoolSpec{
				NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					customRoleLabel: "",
				}},
				Configuration: mcfgv1.MachineConfigPoolStatusConfiguration{
					ObjectReference: corev1.ObjectReference{Name: "rendered-worker-custom"},
				},
			},
			Status: mcfgv1.MachineConfigPoolStatus{
				ObservedGeneration: 1,
				MachineCount:       2, ReadyMachineCount: 2, UpdatedMachineCount: 2,
				Configuration: mcfgv1.MachineConfigPoolStatusConfiguration{
					ObjectReference: corev1.ObjectReference{Name: "rendered-worker-custom"},
				},
				Conditions: []mcfgv1.MachineConfigPoolCondition{
					{Type: mcfgv1.MachineConfigPoolUpdated, Status: corev1.ConditionTrue},
					{Type: mcfgv1.MachineConfigPoolUpdating, Status: corev1.ConditionFalse},
					{Type: mcfgv1.MachineConfigPoolDegraded, Status: corev1.ConditionFalse},
				},
			},
		}
		clusterInstance := &siteconfig.ClusterInstance{
			TypeMeta:   metav1.TypeMeta{APIVersion: siteconfig.GroupVersion.String(), Kind: "ClusterInstance"},
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: clusterName},
			Spec: siteconfig.ClusterInstanceSpec{Nodes: []siteconfig.NodeSpec{
				{HostName: "master-1", Role: "master"},
				{HostName: "worker-1", Role: "worker"},
				{HostName: "worker-2", Role: "worker"},
			}},
		}
		var err error
		ci, err = utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "readiness-pr"},
		}
		rbacWork, err := spokeclient.BuildRBACManifestWork(pr.Name+"-scaleout-rbac", clusterName,
			pr.Name+"-scaleout", "open-cluster-management-agent-addon", scaleOutCSRRBACRules)
		Expect(err).ToNot(HaveOccurred())
		rbacWork.Generation = 1
		rbacWork.Status.Conditions = []metav1.Condition{
			{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 1},
		}
		hub := fakeclient.GetFakeClientFromObjects(
			&addonv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
				Name: "managed-serviceaccount", Namespace: clusterName,
			}},
			&msav1beta1.ManagedServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: pr.Name + "-scaleout", Namespace: clusterName},
				Status: msav1beta1.ManagedServiceAccountStatus{
					TokenSecretRef: &msav1beta1.SecretRef{Name: "readiness-token"},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "readiness-token", Namespace: clusterName},
				Data:       map[string][]byte{"token": []byte("test-token"), "ca.crt": []byte("test-ca")},
			},
			rbacWork,
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName},
				Spec: clusterv1.ManagedClusterSpec{ManagedClusterClientConfigs: []clusterv1.ClientConfig{
					{URL: "https://readiness.example.com"},
				}},
			})
		task = &provisioningRequestReconcilerTask{
			logger: logger, client: hub, object: pr,
			clusterInput: &clusterInput{hostToGroupName: map[string]string{
				"master-1": "controller", "worker-1": pool.Name, "worker-2": pool.Name,
			}},
		}
		objects := []client.Object{pool}
		for _, name := range []string{"master-1", "worker-1", "worker-2"} {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"node-role.kubernetes.io/worker": "", customRoleLabel: ""},
				},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				}},
			}
			objects = append(objects, node)
			if name == "master-1" {
				node.Labels = map[string]string{"node-role.kubernetes.io/master": ""}
			}
			if name == "worker-2" {
				workerNode = node
			}
		}
		spoke = fake.NewClientBuilder().WithScheme(scaleOutSpokeScheme).WithObjects(objects...).Build()
		DeferCleanup(spokeclient.SetTestSpokeClientCreator(func(_ string, _ string, _ []byte, _ *runtime.Scheme) (client.Client, error) {
			return spoke, nil
		}))
	})

	It("waits for worker membership, expected pool count, and pool convergence", func() {
		pool.Status.MachineCount = 1
		pool.Status.ReadyMachineCount = 1
		pool.Status.UpdatedMachineCount = 1
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		delete(workerNode.Labels, customRoleLabel)
		Expect(spoke.Update(ctx, workerNode)).To(Succeed())

		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		// The worker label may appear before the MCP notices the added node.
		workerNode.Labels[customRoleLabel] = ""
		Expect(spoke.Update(ctx, workerNode)).To(Succeed())
		ready, err = task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		pool.Status.MachineCount = 2
		pool.Status.Conditions[0].Status = corev1.ConditionFalse
		pool.Status.Conditions[1].Status = corev1.ConditionTrue
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		ready, err = task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		pool.Status.ReadyMachineCount = 2
		pool.Status.UpdatedMachineCount = 2
		pool.Status.Conditions[0].Status = corev1.ConditionTrue
		pool.Status.Conditions[1].Status = corev1.ConditionFalse
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		ready, err = task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("supports the default worker group without ClusterInstance nodeLabels", func() {
		Expect(spoke.Delete(ctx, pool)).To(Succeed())
		pool.Name = "worker"
		pool.ResourceVersion = ""
		pool.Spec.NodeSelector.MatchLabels = map[string]string{"node-role.kubernetes.io/worker": ""}
		Expect(spoke.Create(ctx, pool)).To(Succeed())
		task.clusterInput.hostToGroupName["worker-1"] = "worker"
		task.clusterInput.hostToGroupName["worker-2"] = "worker"
		for _, hostname := range []string{"worker-1", "worker-2"} {
			node := &corev1.Node{}
			Expect(spoke.Get(ctx, client.ObjectKey{Name: hostname}, node)).To(Succeed())
			delete(node.Labels, customRoleLabel)
			Expect(spoke.Update(ctx, node)).To(Succeed())
		}
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	DescribeTable("waits for pool convergence", func(mutate func(*mcfgv1.MachineConfigPool)) {
		mutate(pool)
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	},
		Entry("unobserved generation", func(p *mcfgv1.MachineConfigPool) { p.Status.ObservedGeneration = 0 }),
		Entry("old machine count", func(p *mcfgv1.MachineConfigPool) { p.Status.MachineCount = 1 }),
		Entry("not all machines ready", func(p *mcfgv1.MachineConfigPool) { p.Status.ReadyMachineCount = 1 }),
		Entry("not all machines updated", func(p *mcfgv1.MachineConfigPool) { p.Status.UpdatedMachineCount = 1 }),
		Entry("unavailable machine", func(p *mcfgv1.MachineConfigPool) { p.Status.UnavailableMachineCount = 1 }),
		Entry("degraded machine", func(p *mcfgv1.MachineConfigPool) { p.Status.DegradedMachineCount = 1 }),
		Entry("missing conditions", func(p *mcfgv1.MachineConfigPool) { p.Status.Conditions = nil }),
		Entry("not Updated", func(p *mcfgv1.MachineConfigPool) { p.Status.Conditions[0].Status = corev1.ConditionFalse }),
		Entry("Updating", func(p *mcfgv1.MachineConfigPool) { p.Status.Conditions[1].Status = corev1.ConditionTrue }),
		Entry("Degraded", func(p *mcfgv1.MachineConfigPool) { p.Status.Conditions[2].Status = corev1.ConditionTrue }),
		Entry("old rendered config", func(p *mcfgv1.MachineConfigPool) { p.Status.Configuration.Name = "old-config" }),
	)

	It("checks the requested node's custom role even when pool counts look ready", func() {
		delete(workerNode.Labels, customRoleLabel)
		Expect(spoke.Update(ctx, workerNode)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("waits for a missing pool node selector to be configured even when pool counts look ready", func() {
		selector := pool.Spec.NodeSelector
		pool.Spec.NodeSelector = nil
		Expect(spoke.Update(ctx, pool)).To(Succeed())

		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		pool.Spec.NodeSelector = selector
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		ready, err = task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("accepts Ready workers matching a converged pool without daemon annotations", func() {
		Expect(workerNode.Annotations).To(BeEmpty())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("waits for a missing requested node", func() {
		Expect(spoke.Delete(ctx, workerNode)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	DescribeTable("waits for a requested node without Ready=True", func(conditions []corev1.NodeCondition) {
		workerNode.Status.Conditions = conditions
		Expect(spoke.Status().Update(ctx, workerNode)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	},
		Entry("Ready=False", []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}),
		Entry("Ready=Unknown", []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionUnknown}}),
		Entry("no conditions", []corev1.NodeCondition{}),
		Entry("only other conditions", []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse}}),
	)

	It("ignores an unrelated spoke node that is not Ready", func() {
		unrelatedNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated-node"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			}},
		}
		Expect(spoke.Create(ctx, unrelatedNode)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("returns node list failures for retry", func() {
		spoke = interceptor.NewClient(spoke.(client.WithWatch), interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return errors.New("node list failed")
				}
				return c.List(ctx, list, opts...)
			},
		})
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).To(MatchError(ContainSubstring("node list failed")))
		Expect(ready).To(BeFalse())
	})

	DescribeTable("keeps the existing all-nodes-Ready requirement", func(hostname string) {
		node := &corev1.Node{}
		Expect(spoke.Get(ctx, client.ObjectKey{Name: hostname}, node)).To(Succeed())
		node.Status.Conditions[0].Status = corev1.ConditionFalse
		Expect(spoke.Status().Update(ctx, node)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	},
		Entry("an existing worker is not Ready", "worker-1"),
		Entry("an existing master is not Ready", "master-1"),
	)

	It("waits when a requested node is being deleted", func() {
		workerNode.Finalizers = []string{"test.example.com/hold"}
		Expect(spoke.Update(ctx, workerNode)).To(Succeed())
		Expect(spoke.Delete(ctx, workerNode)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("waits for a missing pool", func() {
		Expect(spoke.Delete(ctx, pool)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("waits when the target pool is being deleted", func() {
		pool.Finalizers = []string{"test.example.com/hold"}
		Expect(spoke.Update(ctx, pool)).To(Succeed())
		Expect(spoke.Delete(ctx, pool)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("returns MCP read failures for retry", func() {
		spoke = interceptor.NewClient(spoke.(client.WithWatch), interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*mcfgv1.MachineConfigPool); ok {
					return errors.New("MCP read failed")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).To(MatchError(ContainSubstring("MCP read failed")))
		Expect(ready).To(BeFalse())
	})

	It("skips the MCP check when no host-to-group mapping is available", func() {
		task.clusterInput.hostToGroupName = nil
		Expect(spoke.Delete(ctx, pool)).To(Succeed())
		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("counts default and custom worker groups separately", func() {
		defaultPool := pool.DeepCopy()
		defaultPool.Name = "worker"
		defaultPool.ResourceVersion = ""
		defaultPool.Spec.NodeSelector.MatchLabels = map[string]string{"node-role.kubernetes.io/worker": ""}
		defaultPool.Spec.Configuration.Name = "rendered-worker"
		defaultPool.Status.Configuration.Name = "rendered-worker"
		defaultPool.Status.MachineCount = 1
		defaultPool.Status.ReadyMachineCount = 1
		defaultPool.Status.UpdatedMachineCount = 1
		Expect(spoke.Create(ctx, defaultPool)).To(Succeed())
		defaultWorker := workerNode.DeepCopy()
		defaultWorker.Name = "default-worker"
		defaultWorker.ResourceVersion = ""
		delete(defaultWorker.Labels, customRoleLabel)
		Expect(spoke.Create(ctx, defaultWorker)).To(Succeed())
		task.clusterInput.hostToGroupName[defaultWorker.Name] = defaultPool.Name
		nodes, _, err := unstructured.NestedSlice(ci.Object, "spec", "nodes")
		Expect(err).ToNot(HaveOccurred())
		nodes = append(nodes, map[string]any{"hostName": defaultWorker.Name, "role": "worker"})
		Expect(unstructured.SetNestedSlice(ci.Object, nodes, "spec", "nodes")).To(Succeed())

		ready, err := task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())

		// A healthy custom pool cannot compensate for a stale default worker pool.
		defaultPool.Status.MachineCount = 0
		defaultPool.Status.ReadyMachineCount = 0
		defaultPool.Status.UpdatedMachineCount = 0
		Expect(spoke.Update(ctx, defaultPool)).To(Succeed())
		ready, err = task.checkNodesAndMCPsReady(ctx, ci)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})
})
