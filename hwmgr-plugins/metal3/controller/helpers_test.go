/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

package controller

/*
Test Cases Covered in this File:

1. Config Annotation Functions
   - setConfigAnnotation: Tests setting configuration annotations on AllocatedNode objects
     * Setting annotation when annotations map is nil
     * Setting annotation when annotations map already exists
     * Overwriting existing config annotation
   - getConfigAnnotation: Tests retrieving configuration annotations from AllocatedNode objects
     * Handling nil annotations map
     * Handling missing config annotation
     * Retrieving existing config annotation value
   - removeConfigAnnotation: Tests removing configuration annotations from AllocatedNode objects
     * Handling nil annotations map gracefully
     * Removing existing config annotation while preserving other annotations
   - clearConfigAnnotationWithPatch: Tests the wrapper function for removing config annotations and patching
     * Successfully removes annotation and persists changes via patch operation
     * Handles nodes without config annotation gracefully

2. Node Finding Functions
   - findNodesInProgress: Tests finding all nodes that are currently in progress state or have no condition
     * Returns nil when no nodes are in progress
     * Returns first node with InProgress status
     * Handles nodes without provisioned condition
   - findNextNodeToUpdate: Tests finding the next node that needs hardware profile updates
     * Returns nil when all nodes are up-to-date
     * Returns nodes with different hardware profiles
     * Returns nodes with invalid input conditions
     * Skips nodes from different groups
   - findNodeConfigInProgress: Tests finding nodes with configuration annotations
     * Returns nil when no nodes have config annotations
     * Returns first node with config annotation

3. Utility Functions
   - contains: Tests checking if a value exists in a string slice
     * Returns true when value exists
     * Returns false when value doesn't exist
     * Handles empty and nil slices

4. Node Creation
   - createNode: Tests creating new AllocatedNode objects
     * Successfully creates new AllocatedNode with proper specifications
     * Skips creation if AllocatedNode already exists

5. Status Derivation
   - deriveNodeAllocationRequestStatusFromNodes: Tests deriving NodeAllocationRequest status from node conditions
     * Returns InProgress when nodes are missing Configured condition
     * Returns ConfigApplied when all nodes are successfully configured
     * Returns failure status from first failed node

6. Node Allocation Processing
   - processNewNodeAllocationRequest: Tests processing new node allocation requests
     * Succeeds when enough resources are available
     * Skips groups with size 0
     * Returns error when insufficient resources are available

7. Allocation Status Checking
   - isNodeAllocationRequestFullyAllocated: Tests checking if allocation requests are fully satisfied
     * Returns true when all groups are fully allocated
     * Returns false when groups are not fully allocated

8. Integration Test Placeholders
   - Lists complex functions that require extensive mocking and should be tested in integration tests:
     * setAwaitConfigCondition
     * releaseNodeAllocationRequest
     * getNodeAllocationRequestBMHNamespace
     * allocateBMHToNodeAllocationRequest
     * processNodeAllocationRequestAllocation
     * handleNodeInProgressUpdate
     * initiateNodeUpdate
     * handleNodeAllocationRequestConfiguring
*/

import (
	"context"
	"log/slog"
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

// Helper functions
// nolint:unparam
func createNodeWithCondition(name, namespace, conditionType, reason string, status metav1.ConditionStatus) *pluginsv1alpha1.AllocatedNode {
	node := createAllocatedNode(name, namespace, "bmh-"+name, namespace)
	node.Status.Conditions = []metav1.Condition{
		{
			Type:   conditionType,
			Status: status,
			Reason: reason,
		},
	}
	return node
}

var _ = Describe("Helpers", func() {
	var (
		ctx             context.Context
		logger          *slog.Logger
		scheme          *runtime.Scheme
		fakeClient      client.Client
		fakeNoncached   client.Reader
		pluginNamespace = "test-plugin-namespace"
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	Describe("Config Annotation Functions", func() {
		var testNode *pluginsv1alpha1.AllocatedNode

		BeforeEach(func() {
			testNode = &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: pluginNamespace,
				},
			}
		})

		Describe("setConfigAnnotation", func() {
			It("should set config annotation when annotations are nil", func() {
				setConfigAnnotation(testNode, "test-reason")

				annotations := testNode.GetAnnotations()
				Expect(annotations).NotTo(BeNil())
				Expect(annotations[ConfigAnnotation]).To(Equal("test-reason"))
			})

			It("should set config annotation when annotations exist", func() {
				testNode.SetAnnotations(map[string]string{"existing": "value"})
				setConfigAnnotation(testNode, "new-reason")

				annotations := testNode.GetAnnotations()
				Expect(annotations["existing"]).To(Equal("value"))
				Expect(annotations[ConfigAnnotation]).To(Equal("new-reason"))
			})

			It("should overwrite existing config annotation", func() {
				testNode.SetAnnotations(map[string]string{ConfigAnnotation: "old-reason"})
				setConfigAnnotation(testNode, "new-reason")

				annotations := testNode.GetAnnotations()
				Expect(annotations[ConfigAnnotation]).To(Equal("new-reason"))
			})
		})

		Describe("getConfigAnnotation", func() {
			It("should return empty string when annotations are nil", func() {
				result := getConfigAnnotation(testNode)
				Expect(result).To(Equal(""))
			})

			It("should return empty string when config annotation doesn't exist", func() {
				testNode.SetAnnotations(map[string]string{"other": "value"})
				result := getConfigAnnotation(testNode)
				Expect(result).To(Equal(""))
			})

			It("should return config annotation value when it exists", func() {
				testNode.SetAnnotations(map[string]string{ConfigAnnotation: "test-reason"})
				result := getConfigAnnotation(testNode)
				Expect(result).To(Equal("test-reason"))
			})
		})

		Describe("removeConfigAnnotation", func() {
			It("should handle nil annotations gracefully", func() {
				removeConfigAnnotation(testNode)
				// Should not panic
			})

			It("should remove config annotation when it exists", func() {
				testNode.SetAnnotations(map[string]string{
					ConfigAnnotation: "test-reason",
					"other":          "value",
				})
				removeConfigAnnotation(testNode)

				annotations := testNode.GetAnnotations()
				Expect(annotations).NotTo(HaveKey(ConfigAnnotation))
				Expect(annotations["other"]).To(Equal("value"))
			})
		})

		Describe("clearConfigAnnotationWithPatch", func() {
			var (
				ctx        context.Context
				testClient client.Client
				allocNode  *pluginsv1alpha1.AllocatedNode
			)

			BeforeEach(func() {
				ctx = context.Background()
				allocNode = &pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-node",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							ConfigAnnotation: "firmware-update",
							"other":          "value",
						},
					},
				}

				scheme := runtime.NewScheme()
				Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
				testClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(allocNode).
					Build()
			})

			It("should remove config annotation and patch the AllocatedNode", func() {
				err := clearConfigAnnotationWithPatch(ctx, testClient, allocNode)
				Expect(err).ToNot(HaveOccurred())

				// Verify annotation was removed from in-memory object
				Expect(allocNode.Annotations).NotTo(HaveKey(ConfigAnnotation))
				Expect(allocNode.Annotations["other"]).To(Equal("value"))

				// Verify the change was persisted
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				err = testClient.Get(ctx, types.NamespacedName{Name: "test-node", Namespace: "test-namespace"}, updatedNode)
				Expect(err).ToNot(HaveOccurred())
				Expect(updatedNode.Annotations).NotTo(HaveKey(ConfigAnnotation))
				Expect(updatedNode.Annotations["other"]).To(Equal("value"))
			})

			It("should handle nodes without config annotation gracefully", func() {
				// Remove the config annotation first
				delete(allocNode.Annotations, ConfigAnnotation)

				err := clearConfigAnnotationWithPatch(ctx, testClient, allocNode)
				Expect(err).ToNot(HaveOccurred())

				// Should still have other annotations
				Expect(allocNode.Annotations["other"]).To(Equal("value"))
			})
		})
	})

	Describe("Node Finding Functions", func() {
		var nodeList *pluginsv1alpha1.AllocatedNodeList

		BeforeEach(func() {
			nodeList = &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{},
			}
		})

		Describe("findNodesInProgress", func() {
			It("should return empty slice when no nodes are in progress", func() {
				nodeList := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						*createNodeWithCondition("node1", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
						*createNodeWithCondition("node2", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse),
					},
				}

				result := findNodesInProgress(nodeList)
				Expect(result).To(BeEmpty())
			})

			It("should return all nodes with InProgress status or no condition", func() {
				nodeList := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						*createNodeWithCondition("node1", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
						*createNodeWithCondition("node2", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse),
						*createAllocatedNode("node3", "test-ns", "bmh-node3", "test-ns"),
					},
				}

				result := findNodesInProgress(nodeList)
				Expect(result).To(HaveLen(2))
				Expect(result[0].Name).To(Equal("node2"))
				Expect(result[1].Name).To(Equal("node3"))
			})

			It("should handle empty node list", func() {
				result := findNodesInProgress(nodeList)
				Expect(result).To(BeEmpty())
			})
		})

		Describe("findNextNodeToUpdate", func() {
			It("should return nil when no nodes need update", func() {
				// Add node with matching profile
				upToDateNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "up-to-date-node"},
					Spec: pluginsv1alpha1.AllocatedNodeSpec{
						GroupName: "test-group",
						HwProfile: "target-profile",
					},
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(hwmgmtv1alpha1.Configured),
								Status: metav1.ConditionTrue,
								Reason: string(hwmgmtv1alpha1.ConfigApplied),
							},
						},
					},
				}
				nodeList.Items = append(nodeList.Items, upToDateNode)

				result := findNextNodeToUpdate(nodeList, "test-group", "target-profile")
				Expect(result).To(BeNil())
			})

			It("should return node with different hw profile", func() {
				// Add node with different profile
				outdatedNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "outdated-node"},
					Spec: pluginsv1alpha1.AllocatedNodeSpec{
						GroupName: "test-group",
						HwProfile: "old-profile",
					},
				}
				nodeList.Items = append(nodeList.Items, outdatedNode)

				result := findNextNodeToUpdate(nodeList, "test-group", "new-profile")
				Expect(result).NotTo(BeNil())
				Expect(result.Name).To(Equal("outdated-node"))
			})

			It("should return node with invalid input condition even if profile matches", func() {
				// Add node with matching profile but invalid input condition
				invalidNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "invalid-node"},
					Spec: pluginsv1alpha1.AllocatedNodeSpec{
						GroupName: "test-group",
						HwProfile: "target-profile",
					},
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(hwmgmtv1alpha1.Configured),
								Status: metav1.ConditionFalse,
								Reason: string(hwmgmtv1alpha1.InvalidInput),
							},
						},
					},
				}
				nodeList.Items = append(nodeList.Items, invalidNode)

				result := findNextNodeToUpdate(nodeList, "test-group", "target-profile")
				Expect(result).NotTo(BeNil())
				Expect(result.Name).To(Equal("invalid-node"))
			})

			It("should skip nodes from different groups", func() {
				// Add node from different group
				differentGroupNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "different-group-node"},
					Spec: pluginsv1alpha1.AllocatedNodeSpec{
						GroupName: "other-group",
						HwProfile: "old-profile",
					},
				}
				nodeList.Items = append(nodeList.Items, differentGroupNode)

				result := findNextNodeToUpdate(nodeList, "test-group", "new-profile")
				Expect(result).To(BeNil())
			})
		})

		Describe("findNodeConfigInProgress", func() {
			It("should return nil when no nodes have config annotation", func() {
				// Add node without config annotation
				normalNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "normal-node"},
				}
				nodeList.Items = append(nodeList.Items, normalNode)

				result := findNodeConfigInProgress(nodeList)
				Expect(result).To(BeNil())
			})

			It("should return first node with config annotation", func() {
				// Add node with config annotation
				configInProgressNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: "config-in-progress-node",
						Annotations: map[string]string{
							ConfigAnnotation: "config-update",
						},
					},
				}
				// Add normal node
				normalNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "normal-node"},
				}
				nodeList.Items = append(nodeList.Items, configInProgressNode, normalNode)

				result := findNodeConfigInProgress(nodeList)
				Expect(result).NotTo(BeNil())
				Expect(result.Name).To(Equal("config-in-progress-node"))
			})
		})
	})

	Describe("contains function", func() {
		It("should return true when value exists in slice", func() {
			slice := []string{"apple", "banana", "cherry"}
			result := contains(slice, "banana")
			Expect(result).To(BeTrue())
		})

		It("should return false when value doesn't exist in slice", func() {
			slice := []string{"apple", "banana", "cherry"}
			result := contains(slice, "orange")
			Expect(result).To(BeFalse())
		})

		It("should return false for empty slice", func() {
			slice := []string{}
			result := contains(slice, "apple")
			Expect(result).To(BeFalse())
		})

		It("should return false for nil slice", func() {
			var slice []string
			result := contains(slice, "apple")
			Expect(result).To(BeFalse())
		})
	})

	Describe("createNode", func() {
		var nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "plugins.hardwaremanagement.oran.openshift.io/v1alpha1",
					Kind:       "NodeAllocationRequest",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: pluginNamespace,
					UID:       "test-uid",
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					HardwarePluginRef: "test-plugin",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(nodeAllocationRequest).
				Build()
		})

		It("should create a new AllocatedNode successfully", func() {
			err := createNode(ctx, fakeClient, logger, pluginNamespace, nodeAllocationRequest,
				"test-node", "test-node-id", "test-node-ns", "test-group", "test-profile")
			Expect(err).NotTo(HaveOccurred())

			// Verify node was created
			createdNode := &pluginsv1alpha1.AllocatedNode{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-node",
				Namespace: pluginNamespace,
			}, createdNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdNode.Spec.GroupName).To(Equal("test-group"))
			Expect(createdNode.Spec.HwProfile).To(Equal("test-profile"))
			Expect(createdNode.Spec.HwMgrNodeId).To(Equal("test-node-id"))
			Expect(createdNode.Spec.HwMgrNodeNs).To(Equal("test-node-ns"))
			Expect(createdNode.Labels[hwmgrutils.HardwarePluginLabel]).To(Equal(hwmgrutils.Metal3HardwarePluginID))
		})

		It("should skip creation if AllocatedNode already exists", func() {
			// Create existing node
			existingNode := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-node",
					Namespace: pluginNamespace,
				},
			}
			Expect(fakeClient.Create(ctx, existingNode)).To(Succeed())

			err := createNode(ctx, fakeClient, logger, pluginNamespace, nodeAllocationRequest,
				"existing-node", "test-node-id", "test-node-ns", "test-group", "test-profile")
			Expect(err).NotTo(HaveOccurred())

			// Verify original node wasn't modified
			retrievedNode := &pluginsv1alpha1.AllocatedNode{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "existing-node",
				Namespace: pluginNamespace,
			}, retrievedNode)
			Expect(err).NotTo(HaveOccurred())
			// Original spec should be empty since we didn't set it
			Expect(retrievedNode.Spec.GroupName).To(Equal(""))
		})
	})

	Describe("deriveNodeAllocationRequestStatusFromNodes", func() {
		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			fakeNoncached = fakeClient
		})

		It("should return InProgress when a node is missing Configured condition", func() {
			// Create node without configured condition
			nodeWithoutCondition := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-without-condition",
					Namespace: pluginNamespace,
				},
			}
			Expect(fakeClient.Create(ctx, nodeWithoutCondition)).To(Succeed())

			nodeList := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*nodeWithoutCondition},
			}

			status, reason, message := deriveNodeAllocationRequestStatusFromNodes(ctx, fakeNoncached, logger, nodeList)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(message).To(ContainSubstring("missing Configured condition"))
		})

		It("should return ConfigApplied when all nodes are successfully configured", func() {
			// Create node with successful configuration
			configuredNode := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "configured-node",
					Namespace: pluginNamespace,
				},
				Status: pluginsv1alpha1.AllocatedNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(hwmgmtv1alpha1.Configured),
							Status: metav1.ConditionTrue,
							Reason: string(hwmgmtv1alpha1.ConfigApplied),
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, configuredNode)).To(Succeed())

			nodeList := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*configuredNode},
			}

			status, reason, message := deriveNodeAllocationRequestStatusFromNodes(ctx, fakeNoncached, logger, nodeList)
			Expect(status).To(Equal(metav1.ConditionTrue))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
			Expect(message).To(Equal(string(hwmgmtv1alpha1.ConfigSuccess)))
		})

		It("should return first failed node's condition", func() {
			// Create node with failed configuration
			failedNode := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-node",
					Namespace: pluginNamespace,
				},
				Status: pluginsv1alpha1.AllocatedNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(hwmgmtv1alpha1.Configured),
							Status:  metav1.ConditionFalse,
							Reason:  string(hwmgmtv1alpha1.Failed),
							Message: "Configuration failed",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, failedNode)).To(Succeed())

			nodeList := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*failedNode},
			}

			status, reason, message := deriveNodeAllocationRequestStatusFromNodes(ctx, fakeNoncached, logger, nodeList)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(message).To(ContainSubstring("failed-node"))
			Expect(message).To(ContainSubstring("Configuration failed"))
		})
	})

	Describe("processNewNodeAllocationRequest", func() {
		var nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					LocationSpec: pluginsv1alpha1.LocationSpec{
						Site: "test-site",
					},
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{
							Size: 2,
							NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
								Name:           "test-group",
								ResourcePoolId: "test-pool",
								HwProfile:      "test-profile",
							},
						},
					},
				},
			}

			// Create BMHs for testing
			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh-1",
					Namespace: "test-bmh-ns",
					Labels: map[string]string{
						LabelSiteID:         "test-site",
						LabelResourcePoolID: "test-pool",
					},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						State: metal3v1alpha1.StateAvailable,
					},
				},
			}
			bmh2 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh-2",
					Namespace: "test-bmh-ns",
					Labels: map[string]string{
						LabelSiteID:         "test-site",
						LabelResourcePoolID: "test-pool",
					},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						State: metal3v1alpha1.StateAvailable,
					},
				},
			}

			// Create corresponding HardwareData for each BMH
			hwData1 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh-1",
					Namespace: "test-bmh-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}
			hwData2 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh-2",
					Namespace: "test-bmh-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(nodeAllocationRequest, bmh1, bmh2, hwData1, hwData2).
				Build()
		})

		It("should succeed when enough resources are available", func() {
			err := processNewNodeAllocationRequest(ctx, fakeClient, logger, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should skip groups with size 0", func() {
			nodeAllocationRequest.Spec.NodeGroup[0].Size = 0
			err := processNewNodeAllocationRequest(ctx, fakeClient, logger, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when not enough resources are available", func() {
			// Increase required nodes to more than available
			nodeAllocationRequest.Spec.NodeGroup[0].Size = 5
			err := processNewNodeAllocationRequest(ctx, fakeClient, logger, nodeAllocationRequest)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enough free resources"))
		})
	})

	Describe("isNodeAllocationRequestFullyAllocated", func() {
		var nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{
							Size: 2,
							NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
								Name: "test-group",
							},
						},
					},
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					Properties: pluginsv1alpha1.Properties{
						NodeNames: []string{"node-1", "node-2"},
					},
				},
			}

			// Create allocated nodes
			node1 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-1",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.AllocatedNodeSpec{
					GroupName: "test-group",
				},
			}
			node2 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-2",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.AllocatedNodeSpec{
					GroupName: "test-group",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(node1, node2).
				Build()
			fakeNoncached = fakeClient
		})

		It("should return true when all groups are fully allocated", func() {
			result := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(result).To(BeTrue())
		})

		It("should return false when a group is not fully allocated", func() {
			// Increase the required size
			nodeAllocationRequest.Spec.NodeGroup[0].Size = 3
			result := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(result).To(BeFalse())
		})
	})

	Describe("Validation Functions", func() {
		var (
			testBMH       *metal3v1alpha1.BareMetalHost
			testHwProfile *hwmgmtv1alpha1.HardwareProfile
			testHFC       *metal3v1alpha1.HostFirmwareComponents
			testHFS       *metal3v1alpha1.HostFirmwareSettings
			testClient    client.Client
		)

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.Default()

			// Create test BMH
			testBMH = &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh",
					Namespace: "test-namespace",
				},
			}

			// Create test HardwareProfile with firmware and BIOS settings
			testHwProfile = &hwmgmtv1alpha1.HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-profile",
					Namespace: pluginNamespace,
				},
				Spec: hwmgmtv1alpha1.HardwareProfileSpec{
					BiosFirmware: hwmgmtv1alpha1.Firmware{
						Version: "1.2.3",
						URL:     "http://example.com/bios.bin",
					},
					BmcFirmware: hwmgmtv1alpha1.Firmware{
						Version: "4.5.6",
						URL:     "http://example.com/bmc.bin",
					},
					NicFirmware: []hwmgmtv1alpha1.Nic{
						{
							Version: "7.8.9",
							URL:     "http://example.com/nic1.bin",
						},
						{
							Version: "10.11.12",
							URL:     "http://example.com/nic2.bin",
						},
					},
					Bios: hwmgmtv1alpha1.Bios{
						Attributes: map[string]intstr.IntOrString{
							"VirtualizationTechnology": intstr.FromString("Enabled"),
							"HyperThreading":           intstr.FromString("Disabled"),
							"BootOrder":                intstr.FromInt(1),
						},
					},
				},
			}

			// Create test HostFirmwareComponents
			testHFC = &metal3v1alpha1.HostFirmwareComponents{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh",
					Namespace: "test-namespace",
				},
				Status: metal3v1alpha1.HostFirmwareComponentsStatus{
					Components: []metal3v1alpha1.FirmwareComponentStatus{
						{
							Component:      "bios",
							CurrentVersion: "1.2.3",
						},
						{
							Component:      "bmc",
							CurrentVersion: "4.5.6",
						},
						{
							Component:      "nic:pci-0000:01:00.0",
							CurrentVersion: "7.8.9",
						},
						{
							Component:      "nic:pci-0000:02:00.0",
							CurrentVersion: "10.11.12",
						},
					},
				},
			}

			// Create test HostFirmwareSettings
			testHFS = &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh",
					Namespace: "test-namespace",
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: map[string]string{
						"VirtualizationTechnology": "Enabled",
						"HyperThreading":           "Disabled",
						"BootOrder":                "1",
					},
				},
			}

			scheme = runtime.NewScheme()
			Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

			testClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testBMH, testHwProfile, testHFC, testHFS).
				Build()
		})

		Describe("validateFirmwareVersions", func() {
			It("should return true when firmware versions match", func() {
				valid, err := validateFirmwareVersions(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return true when no firmware versions are specified", func() {
				// Update profile to have no firmware versions
				testHwProfile.Spec.BiosFirmware.Version = ""
				testHwProfile.Spec.BmcFirmware.Version = ""
				Expect(testClient.Update(ctx, testHwProfile)).To(Succeed())

				valid, err := validateFirmwareVersions(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return false when firmware versions don't match", func() {
				// Update HFC to have different versions
				testHFC.Status.Components[0].CurrentVersion = "1.0.0" // Different BIOS version
				Expect(testClient.Update(ctx, testHFC)).To(Succeed())

				valid, err := validateFirmwareVersions(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when HostFirmwareComponents is missing", func() {
				// Delete HFC
				Expect(testClient.Delete(ctx, testHFC)).To(Succeed())

				valid, err := validateFirmwareVersions(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return error when HardwareProfile is missing", func() {
				valid, err := validateFirmwareVersions(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "nonexistent-profile")
				Expect(err).To(HaveOccurred())
				Expect(valid).To(BeFalse())
			})
		})

		Describe("validateAppliedBiosSettings", func() {
			It("should return true when BIOS settings match", func() {
				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return true when no BIOS settings are specified", func() {
				// Update profile to have no BIOS settings
				testHwProfile.Spec.Bios.Attributes = map[string]intstr.IntOrString{}
				Expect(testClient.Update(ctx, testHwProfile)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return false when BIOS setting values don't match", func() {
				// Update HFS to have different values
				testHFS.Status.Settings["VirtualizationTechnology"] = "Disabled" // Different value
				Expect(testClient.Update(ctx, testHFS)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when BIOS setting is missing from HFS", func() {
				// Remove a setting from HFS
				delete(testHFS.Status.Settings, "HyperThreading")
				Expect(testClient.Update(ctx, testHFS)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when HostFirmwareSettings is missing", func() {
				// Delete HFS
				Expect(testClient.Delete(ctx, testHFS)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return true when NIC firmware versions match", func() {
				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return true when no NIC firmware is specified", func() {
				// Update profile to have no NIC firmware
				testHwProfile.Spec.NicFirmware = []hwmgmtv1alpha1.Nic{}
				Expect(testClient.Update(ctx, testHwProfile)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return false when NIC firmware version doesn't match", func() {
				// Update HFC to have different NIC version
				testHFC.Status.Components[2].CurrentVersion = "7.0.0" // Different version for nic1
				Expect(testClient.Update(ctx, testHFC)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when NIC component is missing from HFC", func() {
				// Remove a NIC component from HFC
				testHFC.Status.Components = testHFC.Status.Components[:3] // Remove the second NIC
				Expect(testClient.Update(ctx, testHFC)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when HostFirmwareComponents is missing and NIC firmware is specified", func() {
				// Delete HFC
				Expect(testClient.Delete(ctx, testHFC)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should skip NIC validation when NIC version is empty", func() {
				// Update profile to have empty NIC version
				testHwProfile.Spec.NicFirmware = []hwmgmtv1alpha1.Nic{
					{
						Version: "", // Empty version should be skipped
						URL:     "http://example.com/nic1.bin",
					},
				}
				Expect(testClient.Update(ctx, testHwProfile)).To(Succeed())

				valid, err := validateAppliedBiosSettings(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})
		})

		Describe("validateNodeConfiguration", func() {
			It("should return true when both firmware and BIOS validation pass", func() {
				valid, err := validateNodeConfiguration(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return false when firmware validation fails", func() {
				// Make firmware validation fail
				testHFC.Status.Components[0].CurrentVersion = "1.0.0" // Different BIOS version
				Expect(testClient.Update(ctx, testHFC)).To(Succeed())

				valid, err := validateNodeConfiguration(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return false when BIOS validation fails", func() {
				// Make BIOS validation fail
				testHFS.Status.Settings["VirtualizationTechnology"] = "Disabled" // Different value
				Expect(testClient.Update(ctx, testHFS)).To(Succeed())

				valid, err := validateNodeConfiguration(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeFalse())
			})

			It("should return true when no firmware or BIOS settings are specified", func() {
				// Update profile to have no firmware or BIOS settings
				testHwProfile.Spec.BiosFirmware.Version = ""
				testHwProfile.Spec.BmcFirmware.Version = ""
				testHwProfile.Spec.Bios.Attributes = map[string]intstr.IntOrString{}
				Expect(testClient.Update(ctx, testHwProfile)).To(Succeed())

				valid, err := validateNodeConfiguration(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "test-profile")
				Expect(err).NotTo(HaveOccurred())
				Expect(valid).To(BeTrue())
			})

			It("should return error when HardwareProfile is missing", func() {
				valid, err := validateNodeConfiguration(ctx, testClient, testClient, logger, testBMH, pluginNamespace, "nonexistent-profile")
				Expect(err).To(HaveOccurred())
				Expect(valid).To(BeFalse())
			})
		})

		Describe("equalIntOrStringWithString", func() {
			It("should compare string values correctly", func() {
				result := equalIntOrStringWithString(intstr.FromString("Enabled"), "Enabled")
				Expect(result).To(BeTrue())

				result = equalIntOrStringWithString(intstr.FromString("Enabled"), "Disabled")
				Expect(result).To(BeFalse())
			})

			It("should compare integer values correctly", func() {
				result := equalIntOrStringWithString(intstr.FromInt(42), "42")
				Expect(result).To(BeTrue())

				result = equalIntOrStringWithString(intstr.FromInt(42), "24")
				Expect(result).To(BeFalse())
			})

			It("should handle case insensitive comparison", func() {
				result := equalIntOrStringWithString(intstr.FromString("ENABLED"), "enabled")
				Expect(result).To(BeTrue())

				result = equalIntOrStringWithString(intstr.FromString("Enabled"), "ENABLED")
				Expect(result).To(BeTrue())
			})

			It("should handle whitespace trimming", func() {
				result := equalIntOrStringWithString(intstr.FromString(" Enabled "), "Enabled")
				Expect(result).To(BeTrue())

				result = equalIntOrStringWithString(intstr.FromString("Enabled"), " Enabled ")
				Expect(result).To(BeTrue())
			})
		})

		Describe("findNodeConfigRequested", func() {
			It("should return nil for empty nodelist", func() {
				nodelist := &pluginsv1alpha1.AllocatedNodeList{}
				result := findNodeConfigRequested(nodelist)
				Expect(result).To(BeNil())
			})

			It("should return node when spec.HwProfile != status.HwProfile and no Configured condition", func() {
				nodelist := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node1"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status:     pluginsv1alpha1.AllocatedNodeStatus{HwProfile: "profile-v1"},
						},
					},
				}
				result := findNodeConfigRequested(nodelist)
				Expect(result).ToNot(BeNil())
				Expect(result.Name).To(Equal("node1"))
			})

			It("should return node when Configured condition is False with ConfigUpdate reason", func() {
				nodelist := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node1"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status: pluginsv1alpha1.AllocatedNodeStatus{
								HwProfile: "profile-v1",
								Conditions: []metav1.Condition{
									{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
								},
							},
						},
					},
				}
				result := findNodeConfigRequested(nodelist)
				Expect(result).ToNot(BeNil())
				Expect(result.Name).To(Equal("node1"))
			})

			It("should return nil when node is already up to date", func() {
				nodelist := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node1"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status: pluginsv1alpha1.AllocatedNodeStatus{
								HwProfile: "profile-v2",
								Conditions: []metav1.Condition{
									{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
								},
							},
						},
					},
				}
				result := findNodeConfigRequested(nodelist)
				Expect(result).To(BeNil())
			})

			It("should return first matching node from multiple nodes", func() {
				nodelist := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node1"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status:     pluginsv1alpha1.AllocatedNodeStatus{HwProfile: "profile-v2"},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node2"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status:     pluginsv1alpha1.AllocatedNodeStatus{HwProfile: "profile-v1"}, // Needs update
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "node3"},
							Spec:       pluginsv1alpha1.AllocatedNodeSpec{HwProfile: "profile-v2"},
							Status:     pluginsv1alpha1.AllocatedNodeStatus{HwProfile: "profile-v1"}, // Also needs update
						},
					},
				}
				result := findNodeConfigRequested(nodelist)
				Expect(result).ToNot(BeNil())
				Expect(result.Name).To(Equal("node2")) // First matching
			})
		})

		Describe("getGroupsSortedByRole", func() {
			It("should return empty slice for NAR with no groups", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{},
					},
				}
				result := getGroupsSortedByRole(nar)
				Expect(result).To(BeEmpty())
			})

			It("should sort master group before worker group", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "workers", Role: hwmgmtv1alpha1.NodeRoleWorker}},
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "masters", Role: hwmgmtv1alpha1.NodeRoleMaster}},
						},
					},
				}
				result := getGroupsSortedByRole(nar)
				Expect(result).To(HaveLen(2))
				Expect(result[0].NodeGroupData.Role).To(Equal(hwmgmtv1alpha1.NodeRoleMaster))
				Expect(result[1].NodeGroupData.Role).To(Equal(hwmgmtv1alpha1.NodeRoleWorker))
			})

			It("should preserve order when all groups have same role", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "group-a", Role: hwmgmtv1alpha1.NodeRoleWorker}},
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "group-b", Role: hwmgmtv1alpha1.NodeRoleWorker}},
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "group-c", Role: hwmgmtv1alpha1.NodeRoleWorker}},
						},
					},
				}
				result := getGroupsSortedByRole(nar)
				Expect(result).To(HaveLen(3))
				Expect(result[0].NodeGroupData.Name).To(Equal("group-a"))
				Expect(result[1].NodeGroupData.Name).To(Equal("group-b"))
				Expect(result[2].NodeGroupData.Name).To(Equal("group-c"))
			})

			It("should handle case-insensitive master role", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "workers", Role: hwmgmtv1alpha1.NodeRoleWorker}},
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "masters", Role: "MASTER"}},
						},
					},
				}
				result := getGroupsSortedByRole(nar)
				Expect(result).To(HaveLen(2))
				Expect(result[0].NodeGroupData.Role).To(Equal("MASTER"))
				Expect(result[1].NodeGroupData.Role).To(Equal(hwmgmtv1alpha1.NodeRoleWorker))
			})

			It("should handle single group", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "controller", Role: hwmgmtv1alpha1.NodeRoleMaster}},
						},
					},
				}
				result := getGroupsSortedByRole(nar)
				Expect(result).To(HaveLen(1))
				Expect(result[0].NodeGroupData.Name).To(Equal("controller"))
			})
		})
	})

	// Skip complex integration tests that require extensive mocking
	Describe("Complex Integration Functions", func() {
		Context("setAwaitConfigCondition", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex external dependencies - test in integration suite")
			})
		})

		Context("releaseNodeAllocationRequest", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires field indexing setup - test in integration suite")
			})
		})

		Context("getNodeAllocationRequestBMHNamespace", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex BMH filtering - test in integration suite")
			})
		})

		Context("allocateBMHToNodeAllocationRequest", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex BMH and Node management - test in integration suite")
			})
		})

		Context("processNodeAllocationRequestAllocation", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex allocation logic - test in integration suite")
			})
		})

		Context("handleNodeInProgressUpdate", func() {
			var (
				testBMH            *metal3v1alpha1.BareMetalHost
				testNode           *pluginsv1alpha1.AllocatedNode
				testNAR            *pluginsv1alpha1.NodeAllocationRequest
				testClient         client.Client
				spokeClient        client.Client
				ctx                context.Context
				logger             *slog.Logger
				originalClientFunc func(context.Context, client.Client, string) (client.Client, error)
			)

			BeforeEach(func() {
				ctx = context.Background()
				logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))

				// Save original function and restore in AfterEach
				originalClientFunc = newClientForClusterFunc

				// Create a test AllocatedNode with config-in-progress annotation
				testNode = &pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-node",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							ConfigAnnotation: "firmware-update", // Node has config in progress
						},
					},
					Spec: pluginsv1alpha1.AllocatedNodeSpec{
						HwMgrNodeId: "test-bmh",
						HwMgrNodeNs: "test-namespace",
						HwProfile:   "test-hw-profile",
					},
				}

				// Create a test NAR with callback URL pointing to test PR
				testNAR = &pluginsv1alpha1.NodeAllocationRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nar",
						Namespace: "test-namespace",
					},
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						ClusterId: "test-cluster",
						Callback: &pluginsv1alpha1.Callback{
							CallbackURL: "http://localhost/nar-callback/v1/provisioning-requests/test-pr",
						},
					},
				}

				// Create a test ProvisioningRequest with AllocatedNodeHostMap
				testPR := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pr",
					},
					Status: provisioningv1alpha1.ProvisioningRequestStatus{
						Extensions: provisioningv1alpha1.Extensions{
							AllocatedNodeHostMap: map[string]string{
								"test-node": "test-node.example.com",
							},
						},
					},
				}
				// Create HardwareProfile with empty BIOS/firmware settings to pass validation checks
				testHwProfile := &hwmgmtv1alpha1.HardwareProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-hw-profile",
						Namespace: "test-plugin-namespace",
					},
					Spec: hwmgmtv1alpha1.HardwareProfileSpec{},
				}

				// Create a test BMH in error state with old timestamp to make it non-transient
				oldTimestamp := time.Now().Add(-10 * time.Minute).Format(time.RFC3339) // 10 minutes ago
				testBMH = &metal3v1alpha1.BareMetalHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-bmh",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							BmhErrorTimestampAnnotation: oldTimestamp,
						},
					},
					Status: metal3v1alpha1.BareMetalHostStatus{
						OperationalStatus: metal3v1alpha1.OperationalStatusError,
						ErrorMessage:      "Servicing failed",
						ErrorType:         metal3v1alpha1.ServicingError,
					},
				}

				// Create fake hub client with the test objects
				scheme := runtime.NewScheme()
				Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(corev1.AddToScheme(scheme)).To(Succeed())

				testClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(testBMH, testNode, testHwProfile, testPR).
					WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
					Build()
			})

			AfterEach(func() {
				// Restore the original function
				newClientForClusterFunc = originalClientFunc
			})

			It("should clean up config annotation when BMH is in error state", func() {
				// Call handleNodeInProgressUpdate
				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, testNAR)

				// Verify the function handled the error case
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("bmh test-namespace/test-bmh is in error state, node status updated to Failed"))

				// Verify that the config annotation was removed
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				nodeKey := types.NamespacedName{Name: testNode.Name, Namespace: testNode.Namespace}
				Expect(testClient.Get(ctx, nodeKey, updatedNode)).To(Succeed())

				// The annotation should be removed
				Expect(updatedNode.Annotations).NotTo(HaveKey(ConfigAnnotation))

				// Verify requeue result (should not requeue on terminal error)
				Expect(result.Requeue).To(BeFalse())
			})

			It("should continue waiting when BMH is in progress (not error)", func() {
				// Update BMH to be in servicing state (not error)
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""

				// Update the client with the modified BMH
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				// Call handleNodeInProgressUpdate
				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, testNAR)

				// Verify the function continues waiting
				Expect(err).ToNot(HaveOccurred())

				// Verify that the config annotation was NOT removed (still in progress)
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				nodeKey := types.NamespacedName{Name: testNode.Name, Namespace: testNode.Namespace}
				Expect(testClient.Get(ctx, nodeKey, updatedNode)).To(Succeed())

				// The annotation should still be there
				Expect(updatedNode.Annotations).To(HaveKey(ConfigAnnotation))

				// Verify requeue with medium interval
				Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			})

			It("should clear config annotation and return no requeue when BMH is in OK state and node is ready", func() {
				// Update BMH to be in OK state
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""
				// Update the client with the modified BMH
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				// Create a ready K8s node for the spoke cluster
				readyK8sNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node.example.com",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionFalse},
						},
					},
				}

				// Create fake spoke client with the ready K8s node
				spokeScheme := runtime.NewScheme()
				Expect(corev1.AddToScheme(spokeScheme)).To(Succeed())
				spokeClient = fake.NewClientBuilder().
					WithScheme(spokeScheme).
					WithObjects(readyK8sNode).
					Build()

				// Mock the spoke client creation function
				newClientForClusterFunc = func(_ context.Context, _ client.Client, _ string) (client.Client, error) {
					return spokeClient, nil
				}

				// Call handleNodeInProgressUpdate
				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, testNAR)

				// Verify the function clears the config annotation and returns no requeue
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
				Expect(result.RequeueAfter).To(BeZero())

				// Verify that the config annotation was removed
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				nodeKey := types.NamespacedName{Name: testNode.Name, Namespace: testNode.Namespace}
				Expect(testClient.Get(ctx, nodeKey, updatedNode)).To(Succeed())
				Expect(updatedNode.Annotations).NotTo(HaveKey(ConfigAnnotation))

				// Verify node status was updated to ConfigApplied
				Expect(updatedNode.Status.HwProfile).To(Equal("test-hw-profile"))
			})

			It("should wait and requeue when BMH is in OK state but node is not ready", func() {
				// Update BMH to be in OK state
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				// Create a not-ready K8s node for the spoke cluster
				notReadyK8sNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node.example.com",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
							{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionTrue},
						},
					},
				}

				// Create spoke client with not-ready node
				spokeScheme := runtime.NewScheme()
				Expect(corev1.AddToScheme(spokeScheme)).To(Succeed())
				notReadySpokeClient := fake.NewClientBuilder().
					WithScheme(spokeScheme).
					WithObjects(notReadyK8sNode).
					Build()

				// Override the mock to return the not-ready spoke client
				newClientForClusterFunc = func(_ context.Context, _ client.Client, _ string) (client.Client, error) {
					return notReadySpokeClient, nil
				}

				// Call handleNodeInProgressUpdate
				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, testNAR)

				// Verify no error but should requeue
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(hwmgrutils.RequeueWithMediumInterval()))

				// Verify that the config annotation was NOT removed (still waiting)
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				nodeKey := types.NamespacedName{Name: testNode.Name, Namespace: testNode.Namespace}
				Expect(testClient.Get(ctx, nodeKey, updatedNode)).To(Succeed())
				Expect(updatedNode.Annotations).To(HaveKey(ConfigAnnotation))

				// Verify node status was NOT updated to the new profile
				Expect(updatedNode.Status.HwProfile).ToNot(Equal(testNode.Spec.HwProfile))
			})

			Context("Integration test scenarios", func() {
				It("should be tested for complex BMH status transitions", func() {
					Skip("Full BMH lifecycle testing requires integration test environment")
				})
			})
		})

		Context("initiateNodeUpdate", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex hardware profile processing - test in integration suite")
			})
		})

		Context("handleNodeAllocationRequestConfiguring", func() {
			var (
				ctx        context.Context
				logger     *slog.Logger
				testClient client.Client
				scheme     *runtime.Scheme

				testNamespace    string = "test-namespace"
				newHwProfile     *hwmgmtv1alpha1.HardwareProfile
				newHwProfileName string = "profile-v2"
				nar              *pluginsv1alpha1.NodeAllocationRequest
			)

			// Helper to create node with all fields needed for handleNodeAllocationRequestConfiguring tests
			// Extends createAllocatedNodeWithGroup by adding NodeAllocationRequest, status profile, and conditions
			createNode := func(name, narName, groupName, specProfile, statusProfile string,
				configuredCondition *metav1.Condition, annotations map[string]string) *pluginsv1alpha1.AllocatedNode {
				node := createAllocatedNodeWithGroup(name, testNamespace, name+"-bmh", testNamespace, groupName, specProfile)
				node.Spec.NodeAllocationRequest = narName
				node.Status.HwProfile = statusProfile
				if annotations != nil {
					node.Annotations = annotations
				}
				if configuredCondition != nil {
					node.Status.Conditions = []metav1.Condition{*configuredCondition}
				}
				return node
			}

			// Helper to create BMH with operational status for config tests
			// Extends createBMH by setting OperationalStatus instead of provisioning state
			createBMH := func(name string, opStatus metal3v1alpha1.OperationalStatus) *metal3v1alpha1.BareMetalHost {
				bmh := createBMH(name+"-bmh", testNamespace, nil, nil, metal3v1alpha1.StateAvailable)
				bmh.Status.OperationalStatus = opStatus
				return bmh
			}

			// Helper to create NAR with node groups for config tests
			createNAR := func(nodeGroups []pluginsv1alpha1.NodeGroup) *pluginsv1alpha1.NodeAllocationRequest {
				nar := createNodeAllocationRequest("test-nar", testNamespace)
				nar.Spec.NodeGroup = nodeGroups
				return nar
			}

			buildClientWithIndex := func(scheme *runtime.Scheme, objs ...client.Object) client.Client {
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objs...).
					WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
					WithStatusSubresource(&metal3v1alpha1.BareMetalHost{}).
					WithIndex(&pluginsv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
						node := obj.(*pluginsv1alpha1.AllocatedNode)
						return []string{node.Spec.NodeAllocationRequest}
					}).
					Build()
			}

			BeforeEach(func() {
				ctx = context.Background()
				logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))

				scheme = runtime.NewScheme()
				Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

				// New HardwareProfile
				newHwProfile = &hwmgmtv1alpha1.HardwareProfile{
					ObjectMeta: metav1.ObjectMeta{Name: newHwProfileName, Namespace: testNamespace},
					Spec: hwmgmtv1alpha1.HardwareProfileSpec{
						BiosFirmware: hwmgmtv1alpha1.Firmware{
							Version: "1.0.0",
							URL:     "https://example.com/firmware.bin",
						},
					},
				}
			})

			Context("SNO cluster", func() {
				BeforeEach(func() {
					// Create Node with old profile
					node := createNode("node1", "test-nar", "controller", "profile-v1", "profile-v1", nil, nil)

					// Create NodeAllocationRequest with new profile
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "controller", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, node, nar, newHwProfile)
				})

				It("should return no-op when node is already up to date", func() {
					// Override BeforeEach: Create node that is already up to date (profile-v2 in both spec and status)
					upToDateNode := createNode("node1", "test-nar", "controller", newHwProfileName, newHwProfileName,
						&metav1.Condition{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)}, nil)
					bmh := createBMH("node1", metal3v1alpha1.OperationalStatusOK)

					// Rebuild the client with the up-to-date node
					testClient = buildClientWithIndex(scheme, upToDateNode, bmh, nar, newHwProfile)

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)

					Expect(err).ToNot(HaveOccurred())
					Expect(result.Requeue).To(BeFalse())
					Expect(result.RequeueAfter).To(BeZero())
					Expect(nodelist.Items).To(HaveLen(1))
				})

				It("should initiate update when node needs new profile", func() {
					bmh := createBMH("node1", metal3v1alpha1.OperationalStatusOK)
					Expect(testClient.Create(ctx, bmh)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(1))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// Verify node spec was updated to new profile
					updatedNode := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node1", Namespace: testNamespace}, updatedNode)).To(Succeed())
					Expect(updatedNode.Spec.HwProfile).To(Equal(newHwProfileName))
					// Verify node status condition was updated to ConfigurationUpdateRequested
					Expect(updatedNode.Status.Conditions[0].Type).To(Equal(string(hwmgmtv1alpha1.Configured)))
					Expect(updatedNode.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
					Expect(updatedNode.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))
				})
			})

			Context("3-node compact cluster", func() {
				var (
					node1 *pluginsv1alpha1.AllocatedNode
					node2 *pluginsv1alpha1.AllocatedNode
					node3 *pluginsv1alpha1.AllocatedNode
					bmh1  *metal3v1alpha1.BareMetalHost
					bmh2  *metal3v1alpha1.BareMetalHost
					bmh3  *metal3v1alpha1.BareMetalHost
				)
				BeforeEach(func() {
					// Create nodes with old profile
					node1 = createNode("node1", "test-nar", "controller", "profile-v1", "profile-v1", nil, nil)
					node2 = createNode("node2", "test-nar", "controller", "profile-v1", "profile-v1", nil, nil)
					node3 = createNode("node3", "test-nar", "controller", "profile-v1", "profile-v1", nil, nil)
					// BMHs are OK
					bmh1 = createBMH("node1", metal3v1alpha1.OperationalStatusOK)
					bmh2 = createBMH("node2", metal3v1alpha1.OperationalStatusOK)
					bmh3 = createBMH("node3", metal3v1alpha1.OperationalStatusOK)
					// NAR with new profile
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "controller", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, node1, node2, node3, bmh1, bmh2, bmh3, nar, newHwProfile)
				})

				It("should initiate update for first node only when all need update", func() {
					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(3))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// Only node1 (first alphabetically) should have its spec updated
					updatedNode1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node1", Namespace: testNamespace}, updatedNode1)).To(Succeed())
					Expect(updatedNode1.Spec.HwProfile).To(Equal(newHwProfileName))
					// Verify node status condition was updated to ConfigurationUpdateRequested
					Expect(updatedNode1.Status.Conditions[0].Type).To(Equal(string(hwmgmtv1alpha1.Configured)))
					Expect(updatedNode1.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
					Expect(updatedNode1.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))

					// node2 and node3 should still have old profile
					updatedNode2 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node2", Namespace: testNamespace}, updatedNode2)).To(Succeed())
					Expect(updatedNode2.Spec.HwProfile).To(Equal("profile-v1"))
					// Verify node status condition was not updated
					Expect(updatedNode2.Status.Conditions).To(BeEmpty())

					updatedNode3 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node3", Namespace: testNamespace}, updatedNode3)).To(Succeed())
					Expect(updatedNode3.Spec.HwProfile).To(Equal("profile-v1"))
					// Verify node status condition was not updated
					Expect(updatedNode3.Status.Conditions).To(BeEmpty())
				})

				It("should handle node with config-in-progress annotation", func() {
					// node1 is in progress (has config-in-progress annotation)
					node1.Spec.HwProfile = newHwProfileName
					node1.Annotations = map[string]string{ConfigAnnotation: "bios-update"}
					Expect(testClient.Update(ctx, node1)).To(Succeed())
					node1.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
					}
					Expect(testClient.Status().Update(ctx, node1)).To(Succeed())

					// Update bmh1 to servicing status (update in progress)
					bmh1.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
					Expect(testClient.Status().Update(ctx, bmh1)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(3))
					// Should requeue to continue monitoring the in-progress node
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))
				})

				It("should handle node with config requested but not in progress", func() {
					// node1 has config update requested
					node1.Spec.HwProfile = newHwProfileName
					Expect(testClient.Update(ctx, node1)).To(Succeed())
					node1.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
					}
					Expect(testClient.Status().Update(ctx, node1)).To(Succeed())

					// Add update-needed annotation to bmh1 that has been initiated
					bmh1.Annotations = map[string]string{BiosUpdateNeededAnnotation: ValueTrue}
					Expect(testClient.Update(ctx, bmh1)).To(Succeed())

					// HostFirmwareSettings CR is required for BIOS update validation
					hfs1 := &metal3v1alpha1.HostFirmwareSettings{
						ObjectMeta: metav1.ObjectMeta{Name: "node1-bmh", Namespace: testNamespace, Generation: 1},
						Status: metal3v1alpha1.HostFirmwareSettingsStatus{
							Conditions: []metav1.Condition{
								{
									Type:               string(metal3v1alpha1.FirmwareSettingsChangeDetected),
									Status:             metav1.ConditionTrue,
									Reason:             "Success",
									LastTransitionTime: metav1.Now(),
									ObservedGeneration: 1,
								},
								{
									Type:               string(metal3v1alpha1.FirmwareSettingsValid),
									Status:             metav1.ConditionTrue,
									Reason:             "Success",
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					}
					Expect(testClient.Create(ctx, hfs1)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(3))
					// Should requeue to continue monitoring BMH operational status
					Expect(result.RequeueAfter).To(Equal(15 * time.Second))

					// Verify BMH has reboot annotation to trigger reboot
					updatedBMH := &metal3v1alpha1.BareMetalHost{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node1-bmh", Namespace: testNamespace}, updatedBMH)).To(Succeed())
					Expect(updatedBMH.Annotations[BmhRebootAnnotation]).To(Equal(""))
				})

				It("should initiate update for the next node in the group", func() {
					// node1 has updated to new profile
					node1.Spec.HwProfile = newHwProfileName
					Expect(testClient.Update(ctx, node1)).To(Succeed())
					node1.Status.HwProfile = newHwProfileName
					node1.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					Expect(testClient.Status().Update(ctx, node1)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(3))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// node2 should be updated next
					updatedNode2 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node2", Namespace: testNamespace}, updatedNode2)).To(Succeed())
					Expect(updatedNode2.Spec.HwProfile).To(Equal(newHwProfileName))
					// Verify node status condition was updated to ConfigurationUpdateRequested
					Expect(updatedNode2.Status.Conditions[0].Type).To(Equal(string(hwmgmtv1alpha1.Configured)))
					Expect(updatedNode2.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
					Expect(updatedNode2.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))
				})
			})

			Context("Standard cluster (masters + workers)", func() {
				var (
					master1    *pluginsv1alpha1.AllocatedNode
					master2    *pluginsv1alpha1.AllocatedNode
					master3    *pluginsv1alpha1.AllocatedNode
					worker1    *pluginsv1alpha1.AllocatedNode
					worker2    *pluginsv1alpha1.AllocatedNode
					bmhMaster1 *metal3v1alpha1.BareMetalHost
					bmhMaster2 *metal3v1alpha1.BareMetalHost
					bmhMaster3 *metal3v1alpha1.BareMetalHost
					bmhWorker1 *metal3v1alpha1.BareMetalHost
					bmhWorker2 *metal3v1alpha1.BareMetalHost
				)
				BeforeEach(func() {
					// 3 masters and 2 workers, all need update
					master1 = createNode("master1", "test-nar", "masters", "profile-v1", "profile-v1", nil, nil)
					master2 = createNode("master2", "test-nar", "masters", "profile-v1", "profile-v1", nil, nil)
					master3 = createNode("master3", "test-nar", "masters", "profile-v1", "profile-v1", nil, nil)
					worker1 = createNode("worker1", "test-nar", "workers", "profile-v1", "profile-v1", nil, nil)
					worker2 = createNode("worker2", "test-nar", "workers", "profile-v1", "profile-v1", nil, nil)
					// BMHs are OK
					bmhMaster1 = createBMH("master1", metal3v1alpha1.OperationalStatusOK)
					bmhMaster2 = createBMH("master2", metal3v1alpha1.OperationalStatusOK)
					bmhMaster3 = createBMH("master3", metal3v1alpha1.OperationalStatusOK)
					bmhWorker1 = createBMH("worker1", metal3v1alpha1.OperationalStatusOK)
					bmhWorker2 = createBMH("worker2", metal3v1alpha1.OperationalStatusOK)

					// Define workers BEFORE masters in spec to test that masters are still processed first
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "workers", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newHwProfileName}},
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "masters", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, master1, master2, master3, worker1, worker2,
						bmhMaster1, bmhMaster2, bmhMaster3, bmhWorker1, bmhWorker2, nar, newHwProfile)
				})

				It("should process master group before worker group", func() {
					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(5))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// Master1 (first master alphabetically) should be updated first
					updatedMaster1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "master1", Namespace: testNamespace}, updatedMaster1)).To(Succeed())
					Expect(updatedMaster1.Spec.HwProfile).To(Equal(newHwProfileName))

					// Workers should NOT be updated yet
					updatedWorker1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker1", Namespace: testNamespace}, updatedWorker1)).To(Succeed())
					Expect(updatedWorker1.Spec.HwProfile).To(Equal("profile-v1"))
				})

				It("should not process worker group when master group is not done", func() {
					// master1 and master2 are done
					master1.Spec.HwProfile = newHwProfileName
					master2.Spec.HwProfile = newHwProfileName
					Expect(testClient.Update(ctx, master1)).To(Succeed())
					Expect(testClient.Update(ctx, master2)).To(Succeed())

					master1.Status.HwProfile = newHwProfileName
					master1.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					master2.Status.HwProfile = newHwProfileName
					master2.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					Expect(testClient.Status().Update(ctx, master1)).To(Succeed())
					Expect(testClient.Status().Update(ctx, master2)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(5))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// master3 should be updated next
					updatedMaster3 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "master3", Namespace: testNamespace}, updatedMaster3)).To(Succeed())
					Expect(updatedMaster3.Spec.HwProfile).To(Equal(newHwProfileName))
					// Verify node status condition was updated to ConfigurationUpdateRequested
					Expect(updatedMaster3.Status.Conditions[0].Type).To(Equal(string(hwmgmtv1alpha1.Configured)))
					Expect(updatedMaster3.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
					Expect(updatedMaster3.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))

					// Workers should NOT be updated yet
					updatedWorker1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker1", Namespace: testNamespace}, updatedWorker1)).To(Succeed())
					Expect(updatedWorker1.Spec.HwProfile).To(Equal("profile-v1"))
				})

				It("should process workers after all masters are done", func() {
					// All masters are done
					master1.Spec.HwProfile = newHwProfileName
					master2.Spec.HwProfile = newHwProfileName
					master3.Spec.HwProfile = newHwProfileName
					Expect(testClient.Update(ctx, master1)).To(Succeed())
					Expect(testClient.Update(ctx, master2)).To(Succeed())
					Expect(testClient.Update(ctx, master3)).To(Succeed())

					// Update status for all masters (marks them as truly "done")
					master1.Status.HwProfile = newHwProfileName
					master1.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					master2.Status.HwProfile = newHwProfileName
					master2.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					master3.Status.HwProfile = newHwProfileName
					master3.Status.Conditions = []metav1.Condition{
						{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)},
					}
					Expect(testClient.Status().Update(ctx, master1)).To(Succeed())
					Expect(testClient.Status().Update(ctx, master2)).To(Succeed())
					Expect(testClient.Status().Update(ctx, master3)).To(Succeed())

					result, nodelist, err := handleNodeAllocationRequestConfiguring(ctx, testClient, testClient, logger, testNamespace, nar)
					Expect(err).ToNot(HaveOccurred())
					Expect(nodelist.Items).To(HaveLen(5))
					Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

					// Worker1 (first worker alphabetically) should be updated
					updatedWorker1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker1", Namespace: testNamespace}, updatedWorker1)).To(Succeed())
					Expect(updatedWorker1.Spec.HwProfile).To(Equal(newHwProfileName))

					// Worker2 is not updated yet
					updatedWorker2 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker2", Namespace: testNamespace}, updatedWorker2)).To(Succeed())
					Expect(updatedWorker2.Spec.HwProfile).To(Equal("profile-v1"))
				})
			})
		})
	})
})
