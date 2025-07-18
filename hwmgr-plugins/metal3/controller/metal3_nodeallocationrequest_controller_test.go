/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

var _ = Describe("NodeAllocationRequestReconciler", func() {
	var (
		ctx                     context.Context
		logger                  *slog.Logger
		scheme                  *runtime.Scheme
		fakeClient              client.Client
		fakeNoncached           client.Reader
		reconciler              *NodeAllocationRequestReconciler
		nodeAllocationRequest   *pluginsv1alpha1.NodeAllocationRequest
		allocatedNode           *pluginsv1alpha1.AllocatedNode
		bmh                     *metal3v1alpha1.BareMetalHost
		req                     ctrl.Request
		pluginNamespace         = "test-plugin-namespace"
		nodeAllocationRequestNS = "test-nar-namespace"
		nodeName                = "test-node-123"
		bmhName                 = "test-bmh"
		bmhNamespace            = "test-bmh-namespace"
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwpluginutils.InitNodeAllocationRequestUtils(scheme)).To(Succeed())

		// Create test NodeAllocationRequest
		nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nar",
				Namespace: nodeAllocationRequestNS,
				Labels: map[string]string{
					hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
				},
				Generation: 1,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId: "test-cluster",
				LocationSpec: pluginsv1alpha1.LocationSpec{
					Site: "test-site",
				},
				HardwarePluginRef: hwpluginutils.Metal3HardwarePluginID,
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "test-group",
							Role:      "worker",
							HwProfile: "test-profile",
						},
						Size: 1,
					},
				},
				BootInterfaceLabel: "boot-interface",
			},
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				HwMgrPlugin: pluginsv1alpha1.GenerationStatus{
					ObservedGeneration: 1,
				},
			},
		}

		// Create test AllocatedNode
		allocatedNode = &pluginsv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName,
				Namespace: pluginNamespace,
			},
			Spec: pluginsv1alpha1.AllocatedNodeSpec{
				NodeAllocationRequest: nodeAllocationRequest.Name,
				GroupName:            "test-group",
				HwProfile:            "test-profile",
				HardwarePluginRef:    hwpluginutils.Metal3HardwarePluginID,
				HwMgrNodeId:          bmhName,
				HwMgrNodeNs:          bmhNamespace,
			},
		}

		// Create test BareMetalHost
		bmh = &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmhName,
				Namespace: bmhNamespace,
			},
			Spec: metal3v1alpha1.BareMetalHostSpec{
				Online: true,
			},
			Status: metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State: metal3v1alpha1.StateAvailable,
				},
			},
		}

		// Initialize fake client with test objects and indexer
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(nodeAllocationRequest, allocatedNode, bmh).
			WithIndex(&pluginsv1alpha1.AllocatedNode{}, hwpluginutils.AllocatedNodeSpecNodeAllocationRequestKey, func(obj client.Object) []string {
				return []string{obj.(*pluginsv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
			}).
			Build()
		fakeNoncached = fakeClient

		// Setup reconciler
		reconciler = &NodeAllocationRequestReconciler{
			Client:          fakeClient,
			NoncachedClient: fakeNoncached,
			Scheme:          scheme,
			Logger:          logger,
			PluginNamespace: pluginNamespace,
			indexerEnabled:  true, // Skip the manager-based indexer setup
		}

		req = ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      nodeAllocationRequest.Name,
				Namespace: nodeAllocationRequest.Namespace,
			},
		}
	})



	Describe("Reconcile", func() {
		It("should reconcile new NodeAllocationRequest successfully", func() {
			// Remove any existing conditions to simulate new request
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{}
			Expect(fakeClient.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify finalizer was added
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedNAR)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedNAR, hwpluginutils.NodeAllocationRequestFinalizer)).To(BeTrue())
		})

		It("should handle non-existent NodeAllocationRequest", func() {
			// Use non-existent request
			nonExistentReq := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent",
					Namespace: nodeAllocationRequestNS,
				},
			}

			result, err := reconciler.Reconcile(ctx, nonExistentReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})

		It("should handle NodeAllocationRequest deletion", func() {
			// Create a new NodeAllocationRequest with deletion timestamp and finalizer
			now := metav1.Now()
			deletingNAR := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-nar",
					Namespace:         nodeAllocationRequestNS,
					DeletionTimestamp: &now,
					Finalizers:        []string{hwpluginutils.NodeAllocationRequestFinalizer},
					Labels: map[string]string{
						hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
					},
				},
				Spec: nodeAllocationRequest.Spec,
			}
			Expect(fakeClient.Create(ctx, deletingNAR)).To(Succeed())

			deletingReq := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      deletingNAR.Name,
					Namespace: deletingNAR.Namespace,
				},
			}

			result, err := reconciler.Reconcile(ctx, deletingReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify finalizer was removed
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, deletingReq.NamespacedName, updatedNAR)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedNAR, hwpluginutils.NodeAllocationRequestFinalizer)).To(BeFalse())
		})

		It("should handle deletion without finalizer", func() {
			// Create a new NodeAllocationRequest with deletion timestamp but no finalizer
			now := metav1.Now()
			deletingNAR := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-nar-no-finalizer",
					Namespace:         nodeAllocationRequestNS,
					DeletionTimestamp: &now,
					Labels: map[string]string{
						hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
					},
				},
				Spec: nodeAllocationRequest.Spec,
			}
			Expect(fakeClient.Create(ctx, deletingNAR)).To(Succeed())

			deletingReq := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      deletingNAR.Name,
					Namespace: deletingNAR.Namespace,
				},
			}

			result, err := reconciler.Reconcile(ctx, deletingReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})

	Describe("HandleNodeAllocationRequest", func() {
		It("should add finalizer to new NodeAllocationRequest", func() {
			// Remove finalizer
			controllerutil.RemoveFinalizer(nodeAllocationRequest, hwpluginutils.NodeAllocationRequestFinalizer)
			Expect(fakeClient.Update(ctx, nodeAllocationRequest)).To(Succeed())

			_, err := reconciler.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())

			// Verify finalizer was added
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedNAR, hwpluginutils.NodeAllocationRequestFinalizer)).To(BeTrue())
		})

		It("should handle create action for new request", func() {
			// Create a fresh NodeAllocationRequest without conditions
			freshNAR := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fresh-nar-create",
					Namespace: nodeAllocationRequestNS,
					Labels: map[string]string{
						hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
					},
					Generation: 1,
				},
				Spec: nodeAllocationRequest.Spec,
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					HwMgrPlugin: pluginsv1alpha1.GenerationStatus{
						ObservedGeneration: 1,
					},
				},
			}
			Expect(fakeClient.Create(ctx, freshNAR)).To(Succeed())

			// This should trigger create action since there are no conditions
			_, err := reconciler.HandleNodeAllocationRequest(ctx, freshNAR)
			// Expect error due to insufficient resources
			Expect(err).To(HaveOccurred())
		})

		It("should handle processing action", func() {
			// Set up processing state
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionFalse,
					Reason: string(hwmgmtv1alpha1.InProgress),
				},
			}
			Expect(fakeClient.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			result, err := reconciler.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			// Processing should requeue with short interval
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))
		})

		It("should handle spec changed action", func() {
			// Set up spec changed state (generation mismatch)
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionTrue,
					Reason: string(hwmgmtv1alpha1.Completed),
				},
			}
			nodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration = 0 // Different from current generation (1)
			Expect(fakeClient.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			_, err := reconciler.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle noop action", func() {
			// Set up completed state
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionTrue,
					Reason: string(hwmgmtv1alpha1.Completed),
				},
			}
			nodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration = 1 // Same as current generation
			Expect(fakeClient.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			result, err := reconciler.HandleNodeAllocationRequest(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})

	Describe("handleNewNodeAllocationRequestCreate", func() {
		BeforeEach(func() {
			// Remove conditions to simulate new request
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{}
		})

		It("should handle successful creation", func() {
			// Create enough available BMHs for the request
			additionalBMH := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "available-bmh",
					Namespace: bmhNamespace,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						State: metal3v1alpha1.StateAvailable,
					},
				},
			}
			Expect(fakeClient.Create(ctx, additionalBMH)).To(Succeed())

			result, err := reconciler.handleNewNodeAllocationRequestCreate(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify condition was set to InProgress
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
			condition := meta.FindStatusCondition(updatedNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
		})

		It("should handle creation failure", func() {
			// No available BMHs should cause creation to fail
			Expect(fakeClient.Delete(ctx, bmh)).To(Succeed())

			result, err := reconciler.handleNewNodeAllocationRequestCreate(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify condition was set to Failed
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
			condition := meta.FindStatusCondition(updatedNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
		})
	})

	Describe("handleNodeAllocationRequestProcessing", func() {
		BeforeEach(func() {
			// Set up processing state
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionFalse,
					Reason: string(hwmgmtv1alpha1.InProgress),
				},
			}
		})

		It("should handle fully allocated request", func() {
			// Make sure allocated node is properly configured to represent full allocation
			allocatedNode.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionTrue,
					Reason: string(hwmgmtv1alpha1.Completed),
				},
			}
			Expect(fakeClient.Status().Update(ctx, allocatedNode)).To(Succeed())

			result, err := reconciler.handleNodeAllocationRequestProcessing(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify condition was set to Completed
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
			condition := meta.FindStatusCondition(updatedNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.Completed)))
		})

		It("should handle in-progress request", func() {
			// Allocated node is not yet fully provisioned
			allocatedNode.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionFalse,
					Reason: string(hwmgmtv1alpha1.InProgress),
				},
			}
			Expect(fakeClient.Status().Update(ctx, allocatedNode)).To(Succeed())

			result, err := reconciler.handleNodeAllocationRequestProcessing(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			// Verify condition remains InProgress
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
			condition := meta.FindStatusCondition(updatedNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
		})
	})

	Describe("handleNodeAllocationRequestSpecChanged", func() {
		BeforeEach(func() {
			// Set up completed state with generation mismatch
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{
					Type:   string(hwmgmtv1alpha1.Provisioned),
					Status: metav1.ConditionTrue,
					Reason: string(hwmgmtv1alpha1.Completed),
				},
			}
			nodeAllocationRequest.Generation = 2
			nodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration = 1
		})

		It("should handle spec change successfully", func() {
			_, err := reconciler.handleNodeAllocationRequestSpecChanged(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
			
			// Should set configured condition if needed
			updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), updatedNAR)).To(Succeed())
		})

		It("should set await config condition when configured condition is true", func() {
			// Add configured condition as true
			nodeAllocationRequest.Status.Conditions = append(nodeAllocationRequest.Status.Conditions,
				metav1.Condition{
					Type:   string(hwmgmtv1alpha1.Configured),
					Status: metav1.ConditionTrue,
					Reason: string(hwmgmtv1alpha1.ConfigApplied),
				})
			Expect(fakeClient.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			_, err := reconciler.handleNodeAllocationRequestSpecChanged(ctx, nodeAllocationRequest)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("handleNodeAllocationRequestDeletion", func() {
		var deletingNAR *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			// Create a deletion request object
			now := metav1.Now()
			deletingNAR = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-test-nar",
					Namespace:         nodeAllocationRequestNS,
					DeletionTimestamp: &now,
				},
				Spec: nodeAllocationRequest.Spec,
			}
		})

		It("should handle deletion successfully", func() {
			completed, err := reconciler.handleNodeAllocationRequestDeletion(ctx, deletingNAR)
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeTrue())
		})
	})


}) 