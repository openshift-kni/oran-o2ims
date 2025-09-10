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
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

			It("should return node when no provisioned condition exists", func() {
				// Add node without provisioned condition (considered in progress)
				nodeWithoutCondition := pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{Name: "no-condition-node"},
					Status:     pluginsv1alpha1.AllocatedNodeStatus{},
				}
				nodeList.Items = append(nodeList.Items, nodeWithoutCondition)

				result := findNodeInProgress(nodeList)
				Expect(result).NotTo(BeNil())
				Expect(result.Name).To(Equal("no-condition-node"))
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
			var (
				testBMH    *metal3v1alpha1.BareMetalHost
				testNode   *pluginsv1alpha1.AllocatedNode
				testClient client.Client
				ctx        context.Context
				logger     *slog.Logger
			)

			BeforeEach(func() {
				ctx = context.Background()
				logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))

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
					},
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

				// Create fake client with the test objects
				scheme := runtime.NewScheme()
				Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
				Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

				testClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(testBMH, testNode).
					Build()
			})

			It("should clean up config annotation when BMH is in error state", func() {
				nodeList := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{*testNode},
				}

				// Call handleInProgressUpdate
				result, handled, err := handleInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", nodeList)

				// Verify the function handled the error case
				Expect(handled).To(BeTrue()) // Should return true when BMH error is processed
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to update AllocatedNode status"))

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

				nodeList := &pluginsv1alpha1.AllocatedNodeList{
					Items: []pluginsv1alpha1.AllocatedNode{*testNode},
				}

				// Call handleInProgressUpdate
				result, handled, err := handleInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", nodeList)

				// Verify the function continues waiting
				Expect(handled).To(BeTrue()) // Should return true when still processing
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
			It("should be tested in integration tests", func() {
				Skip("Requires complex configuration workflow - test in integration suite")
			})
		})
	})
})
