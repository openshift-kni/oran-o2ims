/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

/*
import (
	"context"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
)

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
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		// Configmap for ClusterInstance defaults
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCm,
				Namespace: ctNamespace,
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
nodes:
- hostname: "node1"
  role: master
  ironicInspect: ""
  nodeNetwork:
    interfaces:
    - name: eno1
      label: bootable-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"`,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct, cm}...)
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

		clusterInstanceInputParams, err := provisioningv1alpha1.ExtractMatchingInput(
			cr.Spec.TemplateParameters.Raw, utils.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		mergedClusterInstanceData, err := task.getMergedClusterInputData(
			ctx, ciDefaultsCm, clusterInstanceInputParams.(map[string]any), utils.TemplateParamClusterInstance)
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
							"hostRef": map[string]string{
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
*/
