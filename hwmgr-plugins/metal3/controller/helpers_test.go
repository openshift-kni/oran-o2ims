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
   - deriveNARStatusFromSingleNode / deriveNARStatusFromMultipleNodes: Tests deriving NAR status from node conditions

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
	"fmt"
	"log/slog"
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
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
			_, err := createNode(ctx, fakeClient, logger, pluginNamespace, nodeAllocationRequest,
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

			_, err := createNode(ctx, fakeClient, logger, pluginNamespace, nodeAllocationRequest,
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

	Describe("deriveNARStatusFromSingleNode", func() {
		configured := string(hwmgmtv1alpha1.Configured)

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			fakeNoncached = fakeClient
		})

		It("should return InProgress when node is missing Configured condition", func() {
			node := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: pluginNamespace},
			}
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(message).To(Equal("Configuration update in progress (AllocatedNode n1)"))
		})

		It("should return ConfigApplied when node is successfully configured", func() {
			node := createNodeWithCondition("n1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionTrue))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
			Expect(message).To(Equal(string(hwmgmtv1alpha1.ConfigSuccess)))
		})

		It("should return InProgress for ConfigUpdatePending node", func() {
			node := createNodeWithCondition("n1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(message).To(Equal("Configuration update in progress (AllocatedNode n1)"))
		})

		It("should return InProgress for ConfigUpdate node", func() {
			node := createNodeWithCondition("n1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(message).To(Equal("Configuration update in progress (AllocatedNode n1)"))
		})

		It("should return Failed with error message for Failed node", func() {
			node := createNodeWithCondition("n1", pluginNamespace, configured, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
			node.Status.Conditions[0].Message = "BIOS update failed"
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(message).To(Equal("Configuration update failed (AllocatedNode n1: BIOS update failed)"))
		})

		It("should return Failed with error message for InvalidInput node", func() {
			node := createNodeWithCondition("n1", pluginNamespace, configured, string(hwmgmtv1alpha1.InvalidInput), metav1.ConditionFalse)
			node.Status.Conditions[0].Message = "Invalid BIOS setting"
			Expect(fakeClient.Create(ctx, node)).To(Succeed())

			status, reason, message := deriveNARStatusFromSingleNode(ctx, fakeNoncached, logger, node)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(message).To(Equal("Configuration update failed (AllocatedNode n1: Invalid BIOS setting)"))
		})
	})

	Describe("deriveNARStatusFromMultipleNodes", func() {
		var mnoNAR *pluginsv1alpha1.NodeAllocationRequest
		configured := string(hwmgmtv1alpha1.Configured)

		const testGroupMaster = "master"
		const testGroupWorker = "worker"

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			fakeNoncached = fakeClient
			mnoNAR = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", HwProfile: "profile-v2", Role: "master"}, Size: 3},
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", HwProfile: "profile-v2", Role: "worker"}, Size: 2},
					},
				},
			}
		})

		It("should report in-progress with per-group completed counts", func() {
			m1 := createNodeWithCondition("m1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m1.Spec.GroupName = testGroupMaster
			m2 := createNodeWithCondition("m2", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionTrue)
			m2.Spec.GroupName = testGroupMaster
			m3 := createNodeWithCondition("m3", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			m3.Spec.GroupName = testGroupMaster
			w1 := createNodeWithCondition("w1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			w1.Spec.GroupName = testGroupWorker
			w2 := createNodeWithCondition("w2", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			w2.Spec.GroupName = testGroupWorker

			nodes := []pluginsv1alpha1.AllocatedNode{*m1, *m2, *m3, *w1, *w2}
			for i := range nodes {
				Expect(fakeClient.Create(ctx, &nodes[i])).To(Succeed())
			}

			nodeList := &pluginsv1alpha1.AllocatedNodeList{Items: nodes}
			status, reason, message := deriveNARStatusFromMultipleNodes(ctx, fakeNoncached, logger, nodeList, mnoNAR)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(message).To(Equal(fmt.Sprintf("%s (group master: 1/3 completed, group worker: 0/2 completed)", string(hwmgmtv1alpha1.ConfigInProgress))))
		})

		It("should report failed with per-group failed counts", func() {
			m1 := createNodeWithCondition("m1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m1.Spec.GroupName = testGroupMaster
			m2 := createNodeWithCondition("m2", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m2.Spec.GroupName = testGroupMaster
			m3 := createNodeWithCondition("m3", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m3.Spec.GroupName = testGroupMaster
			w1 := createNodeWithCondition("w1", pluginNamespace, configured, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
			w1.Spec.GroupName = testGroupWorker
			w2 := createNodeWithCondition("w2", pluginNamespace, configured, string(hwmgmtv1alpha1.InvalidInput), metav1.ConditionFalse)
			w2.Spec.GroupName = testGroupWorker

			nodes := []pluginsv1alpha1.AllocatedNode{*m1, *m2, *m3, *w1, *w2}
			for i := range nodes {
				Expect(fakeClient.Create(ctx, &nodes[i])).To(Succeed())
			}

			// NAR reports failed because of the failed node in the worker group.
			nodeList := &pluginsv1alpha1.AllocatedNodeList{Items: nodes}
			status, reason, message := deriveNARStatusFromMultipleNodes(ctx, fakeNoncached, logger, nodeList, mnoNAR)
			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(message).To(Equal(fmt.Sprintf("%s (group master: 3/3 completed, group worker: 2/2 failed)", string(hwmgmtv1alpha1.ConfigFailed))))
		})

		It("should return ConfigApplied when all nodes are configured", func() {
			m1 := createNodeWithCondition("m1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m1.Spec.GroupName = testGroupMaster
			m2 := createNodeWithCondition("m2", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m2.Spec.GroupName = testGroupMaster
			m3 := createNodeWithCondition("m3", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m3.Spec.GroupName = testGroupMaster
			w1 := createNodeWithCondition("w1", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			w1.Spec.GroupName = testGroupWorker
			w2 := createNodeWithCondition("w2", pluginNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			w2.Spec.GroupName = testGroupWorker

			nodes := []pluginsv1alpha1.AllocatedNode{*m1, *m2, *m3, *w1, *w2}
			for i := range nodes {
				Expect(fakeClient.Create(ctx, &nodes[i])).To(Succeed())
			}

			nodeList := &pluginsv1alpha1.AllocatedNodeList{Items: nodes}
			status, reason, message := deriveNARStatusFromMultipleNodes(ctx, fakeNoncached, logger, nodeList, mnoNAR)
			Expect(status).To(Equal(metav1.ConditionTrue))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
			Expect(message).To(Equal(string(hwmgmtv1alpha1.ConfigSuccess)))
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

	Describe("filterNodesByGroup", func() {
		It("should return only nodes matching the specified group", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "master"}},
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker"}},
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "master"}},
				},
			}
			result := filterNodesByGroup(nodelist, "master")
			Expect(result).To(HaveLen(2))
			for _, node := range result {
				Expect(node.Spec.GroupName).To(Equal("master"))
			}
		})

		It("should return empty slice when no nodes match", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker"}},
				},
			}
			result := filterNodesByGroup(nodelist, "master")
			Expect(result).To(BeEmpty())
		})

		It("should return empty slice for empty node list", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{}
			result := filterNodesByGroup(nodelist, "master")
			Expect(result).To(BeEmpty())
		})

		It("should return all nodes when all match the group", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker"}},
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker"}},
					{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker"}},
				},
			}
			result := filterNodesByGroup(nodelist, "worker")
			Expect(result).To(HaveLen(3))
		})

		It("should return pointers to the original slice elements", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
						Spec:       pluginsv1alpha1.AllocatedNodeSpec{GroupName: "master"},
					},
				},
			}
			result := filterNodesByGroup(nodelist, "master")
			Expect(result).To(HaveLen(1))
			Expect(result[0]).To(BeIdenticalTo(&nodelist.Items[0]))
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
			}

			// Create allocated nodes linked to the NAR
			node1 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-1",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.AllocatedNodeSpec{
					GroupName:             "test-group",
					NodeAllocationRequest: "test-nar",
				},
			}
			node2 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-2",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.AllocatedNodeSpec{
					GroupName:             "test-group",
					NodeAllocationRequest: "test-nar",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(node1, node2).
				Build()
			fakeNoncached = fakeClient
		})

		It("should return true when all groups are fully allocated", func() {
			result, err := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return false when a group is not fully allocated", func() {
			nodeAllocationRequest.Spec.NodeGroup[0].Size = 3
			result, err := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should return true even when NodeNames in status is empty (counts CRs, not status)", func() {
			nodeAllocationRequest.Status.Properties.NodeNames = nil
			result, err := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should not count nodes belonging to a different NAR", func() {
			otherNode := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-node",
					Namespace: pluginNamespace,
				},
				Spec: pluginsv1alpha1.AllocatedNodeSpec{
					GroupName:             "test-group",
					NodeAllocationRequest: "other-nar",
				},
			}
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(otherNode).
				Build()
			fakeNoncached = fakeClient

			result, err := isNodeAllocationRequestFullyAllocated(ctx, fakeNoncached, logger, pluginNamespace, nodeAllocationRequest)
			Expect(err).NotTo(HaveOccurred())
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

		Describe("getNewHwProfileForNode", func() {
			It("should return the hwProfile for a matching group name", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "controller", HwProfile: "profile-A"}},
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", HwProfile: "profile-B"}},
						},
					},
				}
				Expect(getNewHwProfileForNode(nar, &pluginsv1alpha1.AllocatedNode{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "controller", HwProfile: "profile-A"}})).To(Equal("profile-A"))
				Expect(getNewHwProfileForNode(nar, &pluginsv1alpha1.AllocatedNode{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "worker", HwProfile: "profile-A"}})).To(Equal("profile-B"))
			})

			It("should return the current node's hwProfile when group is not found", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{
							{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "controller", HwProfile: "profile-A"}},
						},
					},
				}
				Expect(getNewHwProfileForNode(nar, &pluginsv1alpha1.AllocatedNode{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "nonexistent", HwProfile: "profile-A"}})).To(Equal("profile-A"))
			})

			It("should return the current node's hwProfile for empty node groups", func() {
				nar := &pluginsv1alpha1.NodeAllocationRequest{
					Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
						NodeGroup: []pluginsv1alpha1.NodeGroup{},
					},
				}
				Expect(getNewHwProfileForNode(nar, &pluginsv1alpha1.AllocatedNode{Spec: pluginsv1alpha1.AllocatedNodeSpec{GroupName: "controller", HwProfile: "profile-A"}})).To(Equal("profile-A"))
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
				testClient         client.Client
				mockOps            *MockNodeOps
				mockCtrl           *gomock.Controller
				ctx                context.Context
				logger             *slog.Logger
				originalClientFunc func(context.Context, client.Client, string) (client.Client, error)
			)

			BeforeEach(func() {
				ctx = context.Background()
				logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))
				mockCtrl = gomock.NewController(GinkgoT())
				mockOps = NewMockNodeOps(mockCtrl)

				// Save original function and restore in AfterEach
				originalClientFunc = newClientForClusterFunc

				// Create a test AllocatedNode with config-in-progress annotation and pre-populated hostname
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
					Status: pluginsv1alpha1.AllocatedNodeStatus{
						Hostname: "test-node.example.com",
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
				Expect(corev1.AddToScheme(scheme)).To(Succeed())

				testClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(testBMH, testNode, testHwProfile).
					WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
					Build()
			})

			AfterEach(func() {
				mockCtrl.Finish()
				newClientForClusterFunc = originalClientFunc
			})

			It("should clean up config annotation when BMH is in error state", func() {
				mockOps.EXPECT().UncordonNode(gomock.Any(), "test-node.example.com").Return(nil)
				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, mockOps)

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

				// Verify node condition was updated to Failed
				cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
				Expect(cond.Message).To(Equal(BmhServicingErr))
			})

			It("should continue waiting when BMH is in progress (not error)", func() {
				// Update BMH to be in servicing state (not error)
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""

				// Update the client with the modified BMH
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, mockOps)

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

				// Verify node condition message was updated to NodeWaitingBMHComplete
				cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))
				Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeWaitingBMHComplete)))
			})

			It("should clear config annotation and requeue immediately when BMH is in OK state and node is ready", func() {
				// Update BMH to be in OK state
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				mockOps.EXPECT().IsNodeReady(gomock.Any(), "test-node.example.com").Return(true, nil)
				mockOps.EXPECT().UncordonNode(gomock.Any(), "test-node.example.com").Return(nil)

				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, mockOps)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(hwmgrutils.RequeueImmediately()))

				// Verify that the config annotation was removed
				updatedNode := &pluginsv1alpha1.AllocatedNode{}
				nodeKey := types.NamespacedName{Name: testNode.Name, Namespace: testNode.Namespace}
				Expect(testClient.Get(ctx, nodeKey, updatedNode)).To(Succeed())
				Expect(updatedNode.Annotations).NotTo(HaveKey(ConfigAnnotation))

				Expect(updatedNode.Status.HwProfile).To(Equal("test-hw-profile"))
				// Verify node condition was updated to ConfigApplied
				cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
				Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.ConfigSuccess)))
			})

			It("should wait and requeue when BMH is in OK state but node is not ready", func() {
				// Update BMH to be in OK state
				testBMH.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
				testBMH.Status.ErrorMessage = ""
				testBMH.Status.ErrorType = ""
				Expect(testClient.Update(ctx, testBMH)).To(Succeed())

				mockOps.EXPECT().IsNodeReady(gomock.Any(), "test-node.example.com").Return(false, nil)

				result, err := handleNodeInProgressUpdate(ctx, testClient, testClient, logger, "test-plugin-namespace", testNode, mockOps)

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

				// Verify node condition message was updated to NodeWaitingReady
				cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))
				Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeWaitingReady)))
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

				testNamespace        string = "test-namespace"
				newHwProfile         *hwmgmtv1alpha1.HardwareProfile
				currentHwProfileName string = "profile-v1"
				newHwProfileName     string = "profile-v2"
				nar                  *pluginsv1alpha1.NodeAllocationRequest

				savedClientForClusterFunc    func(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error)
				savedClientsetForClusterFunc func(ctx context.Context, hubClient client.Client, clusterName string) (kubernetes.Interface, error)

				spokeNodeNames []string // populated by each context's BeforeEach
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

			// Helper to create NAR with node groups and callback for config tests
			createNAR := func(nodeGroups []pluginsv1alpha1.NodeGroup) *pluginsv1alpha1.NodeAllocationRequest {
				nar := createNodeAllocationRequest("test-nar", testNamespace)
				nar.Spec.NodeGroup = nodeGroups
				nar.Spec.Callback = &pluginsv1alpha1.Callback{
					CallbackURL: "/nar-callback/v1/provisioning-requests/test-pr",
				}
				return nar
			}

			// createPRWithHostMap creates a ProvisioningRequest and sets up its status with the
			// node-to-hostname mapping after the client is built (status is a subresource).
			createPRWithHostMap := func(c client.Client, nodeHostMap map[string]string) {
				pr := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
				}
				Expect(c.Create(ctx, pr)).To(Succeed())
				pr.Status.Extensions.AllocatedNodeHostMap = nodeHostMap
				Expect(c.Status().Update(ctx, pr)).To(Succeed())
			}

			buildClientWithIndex := func(scheme *runtime.Scheme, objs ...client.Object) client.Client {
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objs...).
					WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
					WithStatusSubresource(&metal3v1alpha1.BareMetalHost{}).
					WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).
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
				Expect(corev1.AddToScheme(scheme)).To(Succeed())
				Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())

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

				// Mock spoke client creation to return a fake spoke client with a default MCP.
				// Tests needing custom spoke setup can override these after BeforeEach.
				savedClientForClusterFunc = newClientForClusterFunc
				savedClientsetForClusterFunc = newClientsetForClusterFunc

				newClientForClusterFunc = func(_ context.Context, _ client.Client, _ string) (client.Client, error) {
					spokeScheme := runtime.NewScheme()
					Expect(corev1.AddToScheme(spokeScheme)).To(Succeed())
					Expect(machineconfigv1.Install(spokeScheme)).To(Succeed())
					var objs []client.Object
					for _, name := range spokeNodeNames {
						objs = append(objs, &corev1.Node{
							ObjectMeta: metav1.ObjectMeta{Name: name},
							Status: corev1.NodeStatus{
								Conditions: []corev1.NodeCondition{
									{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
								},
							},
						})
					}
					// Explicitly set master MCP maxUnavailable to 1.
					// For MNO clusters, the workers are updated sequentially if not setting the maxUnavailable.
					maxUnavailable := intstr.FromInt32(1)
					objs = append(objs,
						&machineconfigv1.MachineConfigPool{
							ObjectMeta: metav1.ObjectMeta{Name: "master"},
							Spec:       machineconfigv1.MachineConfigPoolSpec{MaxUnavailable: &maxUnavailable},
						},
					)
					return fake.NewClientBuilder().WithScheme(spokeScheme).WithObjects(objs...).Build(), nil
				}
				newClientsetForClusterFunc = func(_ context.Context, _ client.Client, _ string) (kubernetes.Interface, error) {
					var objs []runtime.Object
					for _, name := range spokeNodeNames {
						objs = append(objs, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
					}
					return kubefake.NewSimpleClientset(objs...), nil
				}
			})

			AfterEach(func() {
				newClientForClusterFunc = savedClientForClusterFunc
				newClientsetForClusterFunc = savedClientsetForClusterFunc
			})

			Context("SNO cluster", func() {
				BeforeEach(func() {
					spokeNodeNames = []string{"node1"}
					// Create Node with old profile
					node := createNode("node1", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)

					// Create NodeAllocationRequest with new profile
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, node, nar, newHwProfile)
					createPRWithHostMap(testClient, map[string]string{"node1": "node1"})
				})

				It("should return no-op when node is already up to date", func() {
					// Override BeforeEach: Create node that is already up to date (profile-v2 in both spec and status)
					upToDateNode := createNode("node1", "test-nar", "master", newHwProfileName, newHwProfileName,
						&metav1.Condition{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.ConfigApplied)}, nil)
					bmh := createBMH("node1", metal3v1alpha1.OperationalStatusOK)

					// Rebuild the client with the up-to-date node
					testClient = buildClientWithIndex(scheme, upToDateNode, bmh, nar, newHwProfile)
					createPRWithHostMap(testClient, map[string]string{"node1": "node1"})

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
					Expect(updatedNode.Status.Conditions[0].Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdateRequested)))
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
					spokeNodeNames = []string{"node1", "node2", "node3"}
					// Create nodes with old profile
					node1 = createNode("node1", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					node2 = createNode("node2", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					node3 = createNode("node3", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					// BMHs are OK
					bmh1 = createBMH("node1", metal3v1alpha1.OperationalStatusOK)
					bmh2 = createBMH("node2", metal3v1alpha1.OperationalStatusOK)
					bmh3 = createBMH("node3", metal3v1alpha1.OperationalStatusOK)
					// NAR with new profile
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, node1, node2, node3, bmh1, bmh2, bmh3, nar, newHwProfile)
					createPRWithHostMap(testClient, map[string]string{"node1": "node1", "node2": "node2", "node3": "node3"})
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
					Expect(updatedNode1.Status.Conditions[0].Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdateRequested)))

					// node2 and node3 should have new profile and be marked as ConfigUpdatePending
					updatedNode2 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node2", Namespace: testNamespace}, updatedNode2)).To(Succeed())
					Expect(updatedNode2.Spec.HwProfile).To(Equal(newHwProfileName))
					Expect(updatedNode2.Status.Conditions).To(HaveLen(1))
					Expect(updatedNode2.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
					Expect(updatedNode2.Status.Conditions[0].Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))

					updatedNode3 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "node3", Namespace: testNamespace}, updatedNode3)).To(Succeed())
					Expect(updatedNode3.Spec.HwProfile).To(Equal(newHwProfileName))
					Expect(updatedNode3.Status.Conditions).To(HaveLen(1))
					Expect(updatedNode3.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
					Expect(updatedNode3.Status.Conditions[0].Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))
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
					spokeNodeNames = []string{"master1", "master2", "master3", "worker1", "worker2"}
					// 3 masters and 2 workers, all need update
					master1 = createNode("master1", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					master2 = createNode("master2", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					master3 = createNode("master3", "test-nar", "master", currentHwProfileName, currentHwProfileName, nil, nil)
					worker1 = createNode("worker1", "test-nar", "worker", currentHwProfileName, currentHwProfileName, nil, nil)
					worker2 = createNode("worker2", "test-nar", "worker", currentHwProfileName, currentHwProfileName, nil, nil)
					// BMHs are OK
					bmhMaster1 = createBMH("master1", metal3v1alpha1.OperationalStatusOK)
					bmhMaster2 = createBMH("master2", metal3v1alpha1.OperationalStatusOK)
					bmhMaster3 = createBMH("master3", metal3v1alpha1.OperationalStatusOK)
					bmhWorker1 = createBMH("worker1", metal3v1alpha1.OperationalStatusOK)
					bmhWorker2 = createBMH("worker2", metal3v1alpha1.OperationalStatusOK)

					// Define worker BEFORE master in spec to test that master is still processed first
					nar = createNAR([]pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newHwProfileName}},
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newHwProfileName}},
					})

					testClient = buildClientWithIndex(scheme, master1, master2, master3, worker1, worker2,
						bmhMaster1, bmhMaster2, bmhMaster3, bmhWorker1, bmhWorker2, nar, newHwProfile)
					createPRWithHostMap(testClient, map[string]string{
						"master1": "master1", "master2": "master2", "master3": "master3",
						"worker1": "worker1", "worker2": "worker2",
					})
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
					Expect(updatedMaster1.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))

					// Workers should have new profile set with ConfigUpdatePending
					updatedWorker1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker1", Namespace: testNamespace}, updatedWorker1)).To(Succeed())
					Expect(updatedWorker1.Spec.HwProfile).To(Equal(newHwProfileName))
					Expect(updatedWorker1.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
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

					// Workers should have new profile but still be pending (not actively processed)
					updatedWorker1 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker1", Namespace: testNamespace}, updatedWorker1)).To(Succeed())
					Expect(updatedWorker1.Spec.HwProfile).To(Equal(newHwProfileName))
					Expect(updatedWorker1.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
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

					// Worker2 should have new profile but still be pending
					updatedWorker2 := &pluginsv1alpha1.AllocatedNode{}
					Expect(testClient.Get(ctx, types.NamespacedName{Name: "worker2", Namespace: testNamespace}, updatedWorker2)).To(Succeed())
					Expect(updatedWorker2.Spec.HwProfile).To(Equal(newHwProfileName))
					Expect(updatedWorker2.Status.Conditions[0].Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
				})
			})
		})
	})

	Describe("classifyNodes", func() {
		var (
			ctx      context.Context
			logger   *slog.Logger
			mockCtrl *gomock.Controller
			mockOps  *MockNodeOps
		)

		const (
			newProfile     = "profile-v2"
			currentProfile = "profile-v1"
			testNamespace  = "ns"
			configured     = string(hwmgmtv1alpha1.Configured)
		)

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.Default()
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)
			mockOps.EXPECT().IsNodeReady(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should correctly classify all node states", func() {
			// hwDone: ConfigApplied + matching profile
			done1 := createNodeWithCondition("done1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			done1.Spec.HwProfile = newProfile
			// hwPending: ConfigApplied but old profile (profile mismatch)
			stale := createNodeWithCondition("stale", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			stale.Spec.HwProfile = currentProfile
			// hwInProgress: ConfigUpdate
			ip1 := createNodeWithCondition("ip1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			ip1.Spec.HwProfile = newProfile
			// hwFailed: Failed
			fail1 := createNodeWithCondition("fail1", testNamespace, configured, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
			fail1.Spec.HwProfile = newProfile
			// hwPending: ConfigUpdatePending
			pend1 := createNodeWithCondition("pend1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend1.Spec.HwProfile = newProfile
			// hwFailed: InvalidInput
			invalidInput1 := createNodeWithCondition("invalidInput", testNamespace, configured, string(hwmgmtv1alpha1.InvalidInput), metav1.ConditionFalse)
			invalidInput1.Spec.HwProfile = newProfile
			// hwPending: no condition
			noCond := createAllocatedNodeWithGroup("noCond", testNamespace, "bmh-noCond", testNamespace, "g1", newProfile)

			nodes := []*pluginsv1alpha1.AllocatedNode{done1, stale, ip1, fail1, noCond, pend1, invalidInput1}
			nc := classifyNodes(ctx, logger, mockOps, nodes, newProfile)
			Expect(nc.DoneNodes).To(HaveLen(1))
			Expect(nc.DoneNodes[0].Name).To(Equal("done1"))
			Expect(nc.InProgressNodes).To(HaveLen(1))
			Expect(nc.InProgressNodes[0].Name).To(Equal("ip1"))
			Expect(nc.FailedNodes).To(HaveLen(2))
			Expect(nc.FailedNodes[0].Name).To(Equal("fail1"))
			Expect(nc.FailedNodes[1].Name).To(Equal("invalidInput"))
			Expect(nc.PendingNodes).To(HaveLen(3))
			Expect(nc.PendingNodes[0].Name).To(Equal("stale"))
			Expect(nc.PendingNodes[1].Name).To(Equal("noCond"))
			Expect(nc.PendingNodes[2].Name).To(Equal("pend1"))
			Expect(nc.PriorityNodes).To(BeNil())
		})

		It("should classify abandoned and not-ready nodes into PriorityNodes bucket", func() {
			mockCtrl.Finish()
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)

			abandonedNode := createNodeWithCondition("abandoned1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			abandonedNode.Spec.HwProfile = "old-profile"
			abandonedNode.Status.Hostname = "worker-abandoned1"
			abandonedNode.Annotations = map[string]string{UpdateAbandonedAnnotation: "true"}

			notReadyNode := createNodeWithCondition("nr1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			notReadyNode.Spec.HwProfile = newProfile
			notReadyNode.Status.Hostname = "worker-nr1"

			readyNode := createNodeWithCondition("r1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			readyNode.Spec.HwProfile = newProfile
			readyNode.Status.Hostname = "worker-r1"

			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-nr1").Return(false, nil)
			// Abandoned node: readiness is checked but doesn't matter — abandoned annotation qualifies it
			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-abandoned1").Return(true, nil)
			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-r1").Return(true, nil)

			nodes := []*pluginsv1alpha1.AllocatedNode{notReadyNode, abandonedNode, readyNode}
			nc := classifyNodes(ctx, logger, mockOps, nodes, newProfile)
			Expect(nc.PriorityNodes).To(HaveLen(2))
			Expect(nc.PriorityNodes[0].Name).To(Equal("nr1"))
			Expect(nc.PriorityNodes[1].Name).To(Equal("abandoned1"))
			Expect(nc.PendingNodes).To(HaveLen(1))
			Expect(nc.PendingNodes[0].Name).To(Equal("r1"))
		})

		It("should return all empty slices for empty input", func() {
			nc := classifyNodes(ctx, logger, mockOps, nil, newProfile)
			Expect(nc.DoneNodes).To(BeNil())
			Expect(nc.InProgressNodes).To(BeNil())
			Expect(nc.FailedNodes).To(BeNil())
			Expect(nc.PendingNodes).To(BeNil())
			Expect(nc.PriorityNodes).To(BeNil())
		})
	})

	Describe("selectNodesToProcess", func() {
		var (
			ctx      context.Context
			logger   *slog.Logger
			mockCtrl *gomock.Controller
			mockOps  *MockNodeOps
		)

		const (
			newProfile      = "profile-v2"
			testNamespace   = "ns"
			testGroupMaster = "master"
			testGroupWorker = "worker"
			configured      = string(hwmgmtv1alpha1.Configured)
		)

		createNAR := func(groups []pluginsv1alpha1.NodeGroup) *pluginsv1alpha1.NodeAllocationRequest {
			return &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: testNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: groups,
				},
			}
		}

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.Default()
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)
			// Default: all nodes are ready unless overridden in a specific test
			mockOps.EXPECT().IsNodeReady(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should select the single pending node for SNO", func() {
			n1 := createNodeWithCondition("n1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			n1.Spec.GroupName = testGroupMaster
			n1.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*n1}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newProfile}},
			})
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "master", 1).Return(1, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodesToProcess).To(HaveLen(1))
			Expect(nodesToProcess[0].node.Name).To(Equal("n1"))
			Expect(nodesToProcess[0].actionType).To(Equal(actionInitiate))
		})

		It("should return empty when all nodes in group are done", func() {
			n1 := createNodeWithCondition("n1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			n1.Spec.GroupName = testGroupMaster
			n1.Spec.HwProfile = newProfile
			n2 := createNodeWithCondition("n2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			n2.Spec.GroupName = testGroupMaster
			n2.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*n1, *n2}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newProfile}},
			})

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodesToProcess).To(BeEmpty())
		})

		It("should respect maxUnavailable, dispatch correct action types, and count failed toward unavailable", func() {
			// in-progress with config annotation -> actionInProgressUpdate, counts as unavailable
			ip1 := createNodeWithCondition("ip1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			ip1.Spec.GroupName = testGroupWorker
			ip1.Spec.HwProfile = newProfile
			ip1.Annotations = map[string]string{ConfigAnnotation: "fw-update"}
			// another in-progress with config annotation -> actionInProgressUpdate, counts as unavailable
			ip2 := createNodeWithCondition("ip2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			ip2.Spec.GroupName = testGroupWorker
			ip2.Spec.HwProfile = newProfile
			ip2.Annotations = map[string]string{ConfigAnnotation: "bios-update"}
			// in-progress without config annotation -> actionTransition, counts as unavailable
			trans1 := createNodeWithCondition("trans1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			trans1.Spec.GroupName = testGroupWorker
			trans1.Spec.HwProfile = newProfile
			// failed -> counts as unavailable (no action dispatched)
			fail1 := createNodeWithCondition("fail1", testNamespace, configured, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
			fail1.Spec.GroupName = testGroupWorker
			fail1.Spec.HwProfile = newProfile
			// 3 pending nodes waiting to be initiated
			pend1 := createNodeWithCondition("pend1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend1.Spec.GroupName = testGroupWorker
			pend1.Spec.HwProfile = newProfile
			pend2 := createNodeWithCondition("pend2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend2.Spec.GroupName = testGroupWorker
			pend2.Spec.HwProfile = newProfile
			pend3 := createNodeWithCondition("pend3", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend3.Spec.GroupName = testGroupWorker
			pend3.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*ip1, *ip2, *trans1, *fail1, *pend1, *pend2, *pend3},
			}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})
			// total=7, maxUnavailable=5, unavailable = inProgress(3) + failed(1) = 4, capacity = 1
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 7).Return(5, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodesToProcess).To(HaveLen(4))

			actionCounts := map[nodeActionType]int{}
			for _, r := range nodesToProcess {
				actionCounts[r.actionType]++
			}
			// 1 pending selected (capacity=1), 2 in-progress updates, 1 transition
			Expect(actionCounts[actionInitiate]).To(Equal(1))
			Expect(actionCounts[actionInProgressUpdate]).To(Equal(2))
			Expect(actionCounts[actionTransition]).To(Equal(1))
		})

		It("should process master group before worker group", func() {
			m1 := createNodeWithCondition("m1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			m1.Spec.GroupName = testGroupMaster
			m1.Spec.HwProfile = newProfile

			w1 := createNodeWithCondition("w1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			w1.Spec.GroupName = testGroupWorker
			w1.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*m1, *w1}}
			// Workers listed before masters in spec to test sorting
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newProfile}},
			})
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "master", 1).Return(1, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			// Only master should be selected; workers are blocked until masters complete
			Expect(nodesToProcess).To(HaveLen(1))
			Expect(nodesToProcess[0].node.Name).To(Equal("m1"))
		})

		It("should move to worker group when master group is fully done", func() {
			var items []pluginsv1alpha1.AllocatedNode
			for _, name := range []string{"m1", "m2", "m3"} {
				n := createNodeWithCondition(name, testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
				n.Spec.GroupName = testGroupMaster
				n.Spec.HwProfile = newProfile
				items = append(items, *n)
			}
			for _, name := range []string{"w1", "w2", "w3"} {
				n := createNodeWithCondition(name, testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
				n.Spec.GroupName = testGroupWorker
				n.Spec.HwProfile = newProfile
				items = append(items, *n)
			}

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: items}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: newProfile}},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})
			// total worker nodes = 3, MCP maxUnavailable=2, unavailable = 0, capacity = 2
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 3).Return(2, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodesToProcess).To(HaveLen(2))
			Expect(nodesToProcess[0].actionType).To(Equal(actionInitiate))
			Expect(nodesToProcess[1].actionType).To(Equal(actionInitiate))
		})

		It("should return error when GetMaxUnavailable fails", func() {
			n1 := createNodeWithCondition("n1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			n1.Spec.GroupName = testGroupWorker
			n1.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*n1}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 1).Return(0, fmt.Errorf("failed to parse maxUnavailable for MCP"))

			_, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse maxUnavailable for MCP"))
		})

		It("should select zero pending nodes when capacity is exhausted by in-progress nodes", func() {
			ip1 := createNodeWithCondition("ip1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			ip1.Spec.GroupName = testGroupWorker
			ip1.Spec.HwProfile = newProfile
			ip1.Annotations = map[string]string{ConfigAnnotation: "fw-update"}

			pend1 := createNodeWithCondition("pend1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend1.Spec.GroupName = testGroupWorker
			pend1.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*ip1, *pend1}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})
			// maxUnavailable=1, in-progress=1, capacity=0
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 2).Return(1, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			var initiateCount int
			for _, r := range nodesToProcess {
				if r.actionType == actionInitiate {
					initiateCount++
				}
			}
			// No new initiations, but in-progress node is still included
			Expect(initiateCount).To(Equal(0))
			Expect(nodesToProcess).To(HaveLen(1))
			Expect(nodesToProcess[0].actionType).To(Equal(actionInProgressUpdate))
		})

		It("should prioritize abandoned nodes over pending nodes and consume capacity", func() {
			// 1 abandoned node, 2 pending nodes, maxUnavailable=2
			ab1 := createNodeWithCondition("ab1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			ab1.Spec.GroupName = testGroupWorker
			ab1.Spec.HwProfile = "old-profile"
			ab1.Annotations = map[string]string{UpdateAbandonedAnnotation: "true"}

			pend1 := createNodeWithCondition("pend1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend1.Spec.GroupName = testGroupWorker
			pend1.Spec.HwProfile = newProfile

			pend2 := createNodeWithCondition("pend2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			pend2.Spec.GroupName = testGroupWorker
			pend2.Spec.HwProfile = newProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*ab1, *pend1, *pend2}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})
			// maxUnavailable=2, unavailable=0, capacity=2
			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 3).Return(2, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			// Abandoned takes 1 slot, 1 remaining for pending -> total 2
			Expect(nodesToProcess).To(HaveLen(2))
			// Abandoned should be first
			Expect(nodesToProcess[0].node.Name).To(Equal("ab1"))
			Expect(nodesToProcess[0].actionType).To(Equal(actionInitiate))
			Expect(nodesToProcess[1].node.Name).To(Equal("pend1"))
			Expect(nodesToProcess[1].actionType).To(Equal(actionInitiate))
		})

		It("should prioritize not-ready pending nodes over ready pending nodes", func() {
			// 3 pending nodes, 1 not ready. maxUnavailable=2.
			// Not-ready node is prioritized and consumes capacity like any other.
			// capacity=2: 1 not-ready + 1 ready = 2 selected, ready2 excluded.
			notReady := createNodeWithCondition("nr1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			notReady.Spec.GroupName = testGroupWorker
			notReady.Spec.HwProfile = newProfile
			notReady.Status.Hostname = "worker-nr1"

			ready1 := createNodeWithCondition("r1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			ready1.Spec.GroupName = testGroupWorker
			ready1.Spec.HwProfile = newProfile
			ready1.Status.Hostname = "worker-r1"

			ready2 := createNodeWithCondition("r2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdatePending), metav1.ConditionFalse)
			ready2.Spec.GroupName = testGroupWorker
			ready2.Spec.HwProfile = newProfile
			ready2.Status.Hostname = "worker-r2"

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*notReady, *ready1, *ready2}}
			nar := createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
			})

			// Override the default IsNodeReady mock for this test
			mockCtrl.Finish()
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)

			mockOps.EXPECT().GetMaxUnavailable(gomock.Any(), "worker", 3).Return(2, nil)
			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-nr1").Return(false, nil)
			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-r1").Return(true, nil)
			mockOps.EXPECT().IsNodeReady(gomock.Any(), "worker-r2").Return(true, nil)

			nodesToProcess, err := selectNodesToProcess(ctx, logger, mockOps, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodesToProcess).To(HaveLen(2))
			Expect(nodesToProcess[0].node.Name).To(Equal("nr1"))
			Expect(nodesToProcess[1].node.Name).To(Equal("r1"))
		})
	})

	Describe("abandonNodeUpdate", func() {
		var (
			ctx             context.Context
			logger          *slog.Logger
			scheme          *runtime.Scheme
			testClient      client.Client
			mockCtrl        *gomock.Controller
			mockOps         *MockNodeOps
			pluginNamespace string
		)

		const (
			oldProfile = "profile-v1"
			newProfile = "profile-v2"
			hostname   = "worker-1"
			testNS     = "test-ns"
		)

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))
			pluginNamespace = testNS
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)

			scheme = runtime.NewScheme()
			Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should requeue with medium interval when BMH is servicing", func() {
			node := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", oldProfile)
			node.Status.Hostname = hostname
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusServicing},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node, bmh).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			result, err := abandonNodeUpdate(ctx, testClient, testClient, logger, newProfile, node, mockOps)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(hwmgrutils.RequeueWithMediumInterval()))

			// Verify node was NOT abandoned
			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).ToNot(HaveKey(UpdateAbandonedAnnotation))
		})

		It("should requeue with medium interval when BMH is preparing", func() {
			node := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", oldProfile)
			node.Status.Hostname = hostname
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{State: metal3v1alpha1.StatePreparing},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node, bmh).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			result, err := abandonNodeUpdate(ctx, testClient, testClient, logger, newProfile, node, mockOps)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(hwmgrutils.RequeueWithMediumInterval()))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).ToNot(HaveKey(UpdateAbandonedAnnotation))
		})

		It("should abandon node, uncordon, clear annotations, and set abandoned annotation when BMH is OK", func() {
			node := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", oldProfile)
			node.Status.Hostname = hostname

			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "n1-bmh",
					Namespace:   pluginNamespace,
					Annotations: map[string]string{BiosUpdateNeededAnnotation: "true", FirmwareUpdateNeededAnnotation: "true"},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusOK},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node, bmh).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().UncordonNode(gomock.Any(), hostname).Return(nil)

			result, err := abandonNodeUpdate(ctx, testClient, testClient, logger, newProfile, node, mockOps)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(hwmgrutils.RequeueImmediately()))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).To(HaveKey(UpdateAbandonedAnnotation))
			Expect(updatedNode.Annotations).ToNot(HaveKey(ConfigAnnotation))

			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1-bmh", Namespace: pluginNamespace}, updatedBMH)).To(Succeed())
			Expect(updatedBMH.Annotations).ToNot(HaveKey(BiosUpdateNeededAnnotation))
			Expect(updatedBMH.Annotations).ToNot(HaveKey(FirmwareUpdateNeededAnnotation))
		})

		It("should abandon node, uncordon, clear annotations, and set abandoned annotation when BMH is error", func() {
			node := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", oldProfile)
			node.Status.Hostname = hostname
			node.Annotations = map[string]string{ConfigAnnotation: "bios-update"}

			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "n1-bmh",
					Namespace:   pluginNamespace,
					Annotations: map[string]string{BiosUpdateNeededAnnotation: "true", FirmwareUpdateNeededAnnotation: "true"},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusError},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node, bmh).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().UncordonNode(gomock.Any(), hostname).Return(nil)

			result, err := abandonNodeUpdate(ctx, testClient, testClient, logger, newProfile, node, mockOps)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(hwmgrutils.RequeueImmediately()))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).To(HaveKey(UpdateAbandonedAnnotation))
			Expect(updatedNode.Annotations).ToNot(HaveKey(ConfigAnnotation))

			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1-bmh", Namespace: pluginNamespace}, updatedBMH)).To(Succeed())
			Expect(updatedBMH.Annotations).ToNot(HaveKey(BiosUpdateNeededAnnotation))
			Expect(updatedBMH.Annotations).ToNot(HaveKey(FirmwareUpdateNeededAnnotation))
		})
	})

	Describe("executeNodeUpdates", func() {
		var (
			ctx             context.Context
			logger          *slog.Logger
			scheme          *runtime.Scheme
			testClient      client.Client
			mockCtrl        *gomock.Controller
			mockOps         *MockNodeOps
			pluginNamespace string
		)

		const (
			newProfile      = "profile-v2"
			currentProfile  = "profile-v1"
			workerHostname1 = "worker-1"
			workerHostname2 = "worker-2"
			testNARName     = "test-nar"
			testNamespace   = "test-ns"
		)

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{}))
			pluginNamespace = "test-ns"
			mockCtrl = gomock.NewController(GinkgoT())
			mockOps = NewMockNodeOps(mockCtrl)

			scheme = runtime.NewScheme()
			Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should return no requeue when nodesToProcess is empty", func() {
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: testNamespace},
			}
			testClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should dispatch parallel actionInitiate for multiple nodes and return requeue", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", currentProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusOK},
			}

			node2 := createAllocatedNodeWithGroup("n2", pluginNamespace, "n2-bmh", pluginNamespace, "worker", currentProfile)
			node2.Spec.NodeAllocationRequest = testNARName
			node2.Status.Hostname = workerHostname2
			bmh2 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n2-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusOK},
			}

			hwProfile := &hwmgmtv1alpha1.HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: newProfile, Namespace: pluginNamespace},
				Spec:       hwmgmtv1alpha1.HardwareProfileSpec{},
			}
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, node2, bmh1, bmh2, hwProfile).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().SkipDrain().Return(false).AnyTimes()
			mockOps.EXPECT().DrainNode(gomock.Any(), workerHostname1).Return(nil)
			mockOps.EXPECT().DrainNode(gomock.Any(), workerHostname2).Return(nil)

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionInitiate},
				{node: node2, actionType: actionInitiate},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		})

		It("should dispatch actionTransition and return requeue when BMH is in Servicing state", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", newProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
			}

			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusServicing},
			}
			bmh1.Annotations = map[string]string{
				BiosUpdateNeededAnnotation: "true",
			}

			// HFS with ChangeDetected=True and Valid=True so evaluateCRForReboot succeeds
			hfs := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "n1-bmh",
					Namespace:  pluginNamespace,
					Generation: 2,
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(metal3v1alpha1.FirmwareSettingsChangeDetected),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
						{
							Type:   string(metal3v1alpha1.FirmwareSettingsValid),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}

			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, bmh1, hfs).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}, &metal3v1alpha1.HostFirmwareSettings{}).
				Build()

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionTransition},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Verify config-in-progress annotation was set on the node
			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(getConfigAnnotation(updatedNode)).To(Equal(UpdateReasonBIOSSettings))

			// Verify BiosUpdateNeeded annotation was removed from BMH
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1-bmh", Namespace: pluginNamespace}, updatedBMH)).To(Succeed())
			Expect(updatedBMH.Annotations).ToNot(HaveKey(BiosUpdateNeededAnnotation))
		})

		It("should abandon actionTransition node when profile changed and BMH is OK", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", currentProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.ConfigUpdate),
				Message: string(hwmgmtv1alpha1.NodeUpdateRequested)},
			}

			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusOK},
			}

			// New profile is requested mid-flight
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, bmh1).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().UncordonNode(gomock.Any(), workerHostname1).Return(nil)

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionTransition},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).To(HaveKey(UpdateAbandonedAnnotation))
		})

		It("should abandon actionInProgressUpdate node when profile changed and BMH is OK", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", currentProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
			}
			node1.Annotations = map[string]string{ConfigAnnotation: "bios-update"}

			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusOK},
			}

			// New profile is requested mid-flight
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, bmh1).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().UncordonNode(gomock.Any(), workerHostname1).Return(nil)

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionInProgressUpdate},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).To(HaveKey(UpdateAbandonedAnnotation))
			Expect(updatedNode.Annotations).ToNot(HaveKey(ConfigAnnotation))
		})

		It("should abandon actionInProgressUpdate node when profile changed and BMH is ERROR", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", currentProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.ConfigUpdate),
				Message: string(hwmgmtv1alpha1.NodeWaitingBMHComplete)},
			}
			node1.Annotations = map[string]string{ConfigAnnotation: "bios-update"}

			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusError},
			}

			// New profile is requested mid-flight
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, bmh1).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			mockOps.EXPECT().UncordonNode(gomock.Any(), workerHostname1).Return(nil)

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionInProgressUpdate},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).To(HaveKey(UpdateAbandonedAnnotation))
			Expect(updatedNode.Annotations).ToNot(HaveKey(ConfigAnnotation))
		})

		It("should not abandon actionInProgressUpdate node when profile changed but BMH is servicing", func() {
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", currentProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.ConfigUpdate),
				Message: string(hwmgmtv1alpha1.NodeWaitingBMHComplete)},
			}

			bmh1 := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: "n1-bmh", Namespace: pluginNamespace},
				Status:     metal3v1alpha1.BareMetalHostStatus{OperationalStatus: metal3v1alpha1.OperationalStatusServicing},
			}

			// New profile is requested mid-flight
			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, bmh1).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}, &metal3v1alpha1.BareMetalHost{}).
				Build()

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionInProgressUpdate},
			}

			result, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Verify node was NOT abandoned
			updatedNode := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "n1", Namespace: pluginNamespace}, updatedNode)).To(Succeed())
			Expect(updatedNode.Annotations).ToNot(HaveKey(UpdateAbandonedAnnotation))
		})

		It("should aggregate errors from multiple nodes", func() {
			// Nodes reference BMHs that do not exist, causing getBMHForNode to fail
			node1 := createAllocatedNodeWithGroup("n1", pluginNamespace, "n1-bmh", pluginNamespace, "worker", newProfile)
			node1.Spec.NodeAllocationRequest = testNARName
			node1.Status.Hostname = workerHostname1
			node1.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
			}
			node1.Annotations = map[string]string{ConfigAnnotation: "bios-update"}

			node2 := createAllocatedNodeWithGroup("n2", pluginNamespace, "n2-bmh", pluginNamespace, "worker", newProfile)
			node2.Spec.NodeAllocationRequest = testNARName
			node2.Status.Hostname = workerHostname2
			node2.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Configured), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.ConfigUpdate)},
			}
			node2.Annotations = map[string]string{ConfigAnnotation: "bios-update"}

			nar := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: testNARName, Namespace: pluginNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: []pluginsv1alpha1.NodeGroup{
						{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: hwmgmtv1alpha1.NodeRoleWorker, HwProfile: newProfile}},
					},
				},
			}

			// No BMH objects in the client — both nodes will fail on getBMHForNode
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node1, node2).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			nodesToProcess := []nodeAction{
				{node: node1, actionType: actionInProgressUpdate},
				{node: node2, actionType: actionInProgressUpdate},
			}

			_, err := executeNodeUpdates(ctx, testClient, testClient, logger, pluginNamespace, nar, mockOps, nodesToProcess)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to process nodes"))
		})
	})

	Describe("markPendingNodesForUpdate", func() {
		var (
			ctx        context.Context
			logger     *slog.Logger
			scheme     *runtime.Scheme
			testClient client.Client
			nar        *pluginsv1alpha1.NodeAllocationRequest
			configured = string(hwmgmtv1alpha1.Configured)
		)
		const (
			testNamespace   = "test-ns"
			testGroupMaster = "master"
			testGroupWorker = "worker"
			newProfile      = "profile-v2"
			currentProfile  = "profile-v1"
		)

		createNAR := func(groups []pluginsv1alpha1.NodeGroup) *pluginsv1alpha1.NodeAllocationRequest {
			return &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: testNamespace},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					NodeGroup: groups,
				},
			}
		}

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.Default()
			scheme = runtime.NewScheme()
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
			nar = createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", HwProfile: newProfile}},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", HwProfile: newProfile}},
			})
		})

		It("should only update nodes in the group with old profile and skip group with no change", func() {
			// master group already at target profile with ConfigApplied — should be skipped
			m1 := createNodeWithCondition("m1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m1.Spec.GroupName = testGroupMaster
			m1.Spec.HwProfile = newProfile
			// worker group with old profile and ConfigApplied — should be updated to ConfigUpdatePending
			w1 := createNodeWithCondition("w1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			w1.Spec.GroupName = testGroupWorker
			w1.Spec.HwProfile = currentProfile
			w2 := createNodeWithCondition("w2", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			w2.Spec.GroupName = testGroupWorker
			w2.Spec.HwProfile = currentProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*m1, *w1, *w2}}
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(m1, w1, w2).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			err := markPendingNodesForUpdate(ctx, testClient, testClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			updated := &pluginsv1alpha1.AllocatedNode{}

			// master already at target — condition should remain ConfigApplied
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "m1", Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Spec.HwProfile).To(Equal(newProfile))
			cond := meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			// workers should have new profile and ConfigUpdatePending
			for _, name := range []string{"w1", "w2"} {
				Expect(testClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, updated)).To(Succeed())
				Expect(updated.Spec.HwProfile).To(Equal(newProfile))
				cond = meta.FindStatusCondition(updated.Status.Conditions, configured)
				Expect(cond).ToNot(BeNil())
				Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
				Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))
			}
		})

		It("should handle groups with different target profiles", func() {
			nar = createNAR([]pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "master", HwProfile: newProfile}},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", HwProfile: "profile-v3"}},
			})

			m1 := createNodeWithCondition("m1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			m1.Spec.GroupName = testGroupMaster
			m1.Spec.HwProfile = currentProfile
			w1 := createNodeWithCondition("w1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
			w1.Spec.GroupName = testGroupWorker
			w1.Spec.HwProfile = currentProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*m1, *w1}}
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(m1, w1).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			err := markPendingNodesForUpdate(ctx, testClient, testClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			updated := &pluginsv1alpha1.AllocatedNode{}

			// master should get profile-v2 with ConfigUpdatePending
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "m1", Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Spec.HwProfile).To(Equal(newProfile))
			cond := meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
			Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))

			// worker should get profile-v3 with ConfigUpdatePending
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "w1", Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Spec.HwProfile).To(Equal("profile-v3"))
			cond = meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
			Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))
		})

		It("should clear failed condition and set ConfigUpdatePending on profile change", func() {
			// node previously failed with old profile — should be reset to ConfigUpdatePending
			failed := createNodeWithCondition("f1", testNamespace, configured, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
			failed.Spec.GroupName = testGroupWorker
			failed.Spec.HwProfile = currentProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*failed}}
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(failed).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			err := markPendingNodesForUpdate(ctx, testClient, testClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			updated := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "f1", Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Spec.HwProfile).To(Equal(newProfile))
			cond := meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(Equal(string(hwmgmtv1alpha1.NodeUpdatePending)))
		})

		It("should skip ConfigUpdate nodes without abandoned annotation on mid-flight profile change", func() {
			// Profile changed from v1 -> v2 (NAR target). Node is in ConfigUpdate with old profile v1
			// and NOT abandoned -> should be skipped.
			inProgress := createNodeWithCondition("ip1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			inProgress.Spec.GroupName = testGroupWorker
			inProgress.Spec.HwProfile = currentProfile

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*inProgress}}
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(inProgress).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			err := markPendingNodesForUpdate(ctx, testClient, testClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			updated := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "ip1", Namespace: testNamespace}, updated)).To(Succeed())
			// Should remain ConfigUpdate with old profile — not touched
			Expect(updated.Spec.HwProfile).To(Equal(currentProfile))
			cond := meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)))
		})

		It("should reset ConfigUpdate node with abandoned annotation to ConfigUpdatePending", func() {
			// Node was previously abandoned (ConfigUpdate + abandoned annotation) — should be
			// reset to ConfigUpdatePending with the new profile.
			abandoned := createNodeWithCondition("ab1", testNamespace, configured, string(hwmgmtv1alpha1.ConfigUpdate), metav1.ConditionFalse)
			abandoned.Spec.GroupName = testGroupWorker
			abandoned.Spec.HwProfile = currentProfile
			abandoned.Annotations = map[string]string{UpdateAbandonedAnnotation: "true"}

			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*abandoned}}
			testClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(abandoned).
				WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
				Build()

			err := markPendingNodesForUpdate(ctx, testClient, testClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			updated := &pluginsv1alpha1.AllocatedNode{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: "ab1", Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Spec.HwProfile).To(Equal(newProfile))
			cond := meta.FindStatusCondition(updated.Status.Conditions, configured)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdatePending)))
		})
	})

	Describe("extractPRNameFromCallback", func() {
		It("should return error when callback is nil", func() {
			result, err := extractPRNameFromCallback(nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no callback configured"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when callback URL is empty", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no callback configured"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when URL is malformed", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "://invalid-url",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse callback URL"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when URL path doesn't match expected pattern", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost/wrong-path/my-pr",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("callback URL does not match expected pattern"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when PR name is empty in URL", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not extract provisioning request name"))
			Expect(result).To(BeEmpty())
		})

		It("should extract PR name from valid callback URL", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/my-provisioning-request",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("my-provisioning-request"))
		})

		It("should extract PR name with complex characters", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost:8080" + constants.NarCallbackServicePath + "/cluster-123-pr",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("cluster-123-pr"))
		})
	})

	Describe("populateNodeHostnames", func() {
		var (
			ctx       context.Context
			logger    *slog.Logger
			scheme    *runtime.Scheme
			hubClient client.Client
			nar       *pluginsv1alpha1.NodeAllocationRequest
		)

		BeforeEach(func() {
			ctx = context.Background()
			logger = slog.Default()
			scheme = runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())

			nar = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: "default",
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ClusterId: "test-cluster",
				},
			}
		})

		It("should skip when all nodes already have hostnames", func() {
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
						Status:     pluginsv1alpha1.AllocatedNodeStatus{Hostname: "host-1.example.com"},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-2", Namespace: "default"},
						Status:     pluginsv1alpha1.AllocatedNodeStatus{Hostname: "host-2.example.com"},
					},
				},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when callback is nil", func() {
			node := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
			}
			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*node}}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node).WithStatusSubresource(node).Build()
			nar.Spec.Callback = nil

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to extract provisioning request name"))
		})

		It("should return error when ProvisioningRequest doesn't exist", func() {
			node := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
			}
			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*node}}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(node).WithStatusSubresource(node).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/non-existent-pr",
			}

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get ProvisioningRequest"))
		})

		It("should return error when node is not in host map", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: map[string]string{
							"other-node": "other-hostname",
						},
					},
				},
			}
			node := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
			}
			nodelist := &pluginsv1alpha1.AllocatedNodeList{Items: []pluginsv1alpha1.AllocatedNode{*node}}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(pr, node).WithStatusSubresource(node).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hostname not found for AllocatedNode node-1"))
		})

		It("should populate hostnames and persist to status", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: map[string]string{
							"node-1": "worker-1.example.com",
							"node-2": "worker-2.example.com",
						},
					},
				},
			}
			node1 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
			}
			node2 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-2", Namespace: "default"},
			}
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*node1, *node2},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(pr, node1, node2).WithStatusSubresource(node1, node2).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			// Verify in-memory items updated
			Expect(nodelist.Items[0].Status.Hostname).To(Equal("worker-1.example.com"))
			Expect(nodelist.Items[1].Status.Hostname).To(Equal("worker-2.example.com"))

			// Verify persisted in API
			updated := &pluginsv1alpha1.AllocatedNode{}
			Expect(hubClient.Get(ctx, client.ObjectKeyFromObject(node1), updated)).To(Succeed())
			Expect(updated.Status.Hostname).To(Equal("worker-1.example.com"))
			Expect(hubClient.Get(ctx, client.ObjectKeyFromObject(node2), updated)).To(Succeed())
			Expect(updated.Status.Hostname).To(Equal("worker-2.example.com"))
		})

		It("should only patch nodes missing hostname", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: map[string]string{
							"node-1": "worker-1.example.com",
							"node-2": "worker-2.example.com",
						},
					},
				},
			}
			node1 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
				Status:     pluginsv1alpha1.AllocatedNodeStatus{Hostname: "worker-1.example.com"},
			}
			node2 := &pluginsv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{Name: "node-2", Namespace: "default"},
			}
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{*node1, *node2},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(pr, node1, node2).WithStatusSubresource(node1, node2).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			err := populateNodeHostnames(ctx, hubClient, logger, nodelist, nar)
			Expect(err).ToNot(HaveOccurred())

			// node-1 already had hostname, node-2 should be populated
			Expect(nodelist.Items[0].Status.Hostname).To(Equal("worker-1.example.com"))
			Expect(nodelist.Items[1].Status.Hostname).To(Equal("worker-2.example.com"))
		})
	})
})
