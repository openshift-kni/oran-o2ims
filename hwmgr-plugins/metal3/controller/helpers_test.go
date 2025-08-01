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

2. Node Finding Functions
   - findNodeInProgress: Tests finding nodes that are currently in progress state
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
     * handleInProgressUpdate
     * initiateNodeUpdate
     * handleNodeAllocationRequestConfiguring
*/

import (
	"context"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

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
	})

	Describe("Node Finding Functions", func() {
		var nodeList *pluginsv1alpha1.AllocatedNodeList

		BeforeEach(func() {
			nodeList = &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{},
			}
		})

		Describe("findNodeInProgress", func() {
			It("should return nil when no nodes are in progress", func() {
				// Add node with completed status
				completedNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "completed-node"},
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(hwmgmtv1alpha1.Provisioned),
								Status: metav1.ConditionTrue,
								Reason: string(hwmgmtv1alpha1.Completed),
							},
						},
					},
				}
				nodeList.Items = append(nodeList.Items, completedNode)

				result := findNodeInProgress(nodeList)
				Expect(result).To(BeNil())
			})

			It("should return first node with InProgress status", func() {
				// Add node with in-progress status
				inProgressNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "in-progress-node"},
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(hwmgmtv1alpha1.Provisioned),
								Status: metav1.ConditionFalse,
								Reason: string(hwmgmtv1alpha1.InProgress),
							},
						},
					},
				}
				// Add another completed node
				completedNode := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "completed-node"},
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(hwmgmtv1alpha1.Provisioned),
								Status: metav1.ConditionTrue,
								Reason: string(hwmgmtv1alpha1.Completed),
							},
						},
					},
				}
				nodeList.Items = append(nodeList.Items, inProgressNode, completedNode)

				result := findNodeInProgress(nodeList)
				Expect(result).NotTo(BeNil())
				Expect(result.Name).To(Equal("in-progress-node"))
			})

			It("should return nil when no provisioned condition exists", func() {
				// Add node without provisioned condition
				nodeWithoutCondition := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "no-condition-node"},
					Status:     pluginsv1alpha1.AllocatedNodeStatus{},
				}
				nodeList.Items = append(nodeList.Items, nodeWithoutCondition)

				result := findNodeInProgress(nodeList)
				Expect(result).To(BeNil())
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

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(nodeAllocationRequest, bmh1, bmh2).
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

	// Skip complex integration tests that require extensive mocking
	Describe("Complex Integration Functions - Skipped", func() {
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

		Context("handleInProgressUpdate", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex BMH status handling - test in integration suite")
			})
		})

		Context("initiateNodeUpdate", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex hardware profile processing - test in integration suite")
			})
		})

		Context("handleNodeAllocationRequestConfiguring", func() {
			It("should be tested in integration tests", func() {
				Skip("Requires complex configuration workflow - test in integration suite")
			})
		})
	})
})
