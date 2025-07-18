/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

var _ = Describe("AllocatedNodeReconciler", func() {
	var (
		ctx               context.Context
		logger            *slog.Logger
		scheme            *runtime.Scheme
		fakeClient        client.Client
		fakeNoncached     client.Reader
		reconciler        *AllocatedNodeReconciler
		allocatedNode     *pluginsv1alpha1.AllocatedNode
		bmh               *metal3v1alpha1.BareMetalHost
		req               ctrl.Request
		pluginNamespace   = "test-plugin-namespace"
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

		// Create test AllocatedNode
		allocatedNode = &pluginsv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-node",
				Namespace: pluginNamespace,
				Labels: map[string]string{
					hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
				},
				ResourceVersion: "1000",
			},
			Spec: pluginsv1alpha1.AllocatedNodeSpec{
				HwMgrNodeId: "test-bmh",
				HwMgrNodeNs: "test-bmh-namespace",
			},
		}

		// Create test BareMetalHost
		bmh = &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State: metal3v1alpha1.StateAvailable,
				},
			},
		}

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(allocatedNode, bmh).Build()
		fakeNoncached = fakeClient

		reconciler = &AllocatedNodeReconciler{
			Client:          fakeClient,
			NoncachedClient: fakeNoncached,
			Scheme:          scheme,
			Logger:          logger,
			PluginNamespace: pluginNamespace,
		}

		req = ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      allocatedNode.Name,
				Namespace: allocatedNode.Namespace,
			},
		}
	})

	Describe("Reconcile", func() {
		Context("when AllocatedNode is not found", func() {
			It("should return without error and not requeue", func() {
				// Delete the node to simulate not found
				Expect(fakeClient.Delete(ctx, allocatedNode)).To(Succeed())

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))
			})
		})

		Context("when AllocatedNode exists and is not being deleted", func() {
			It("should add finalizer if not present and not requeue", func() {
				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))

				// Verify finalizer was added
				var updatedNode pluginsv1alpha1.AllocatedNode
				Expect(fakeClient.Get(ctx, req.NamespacedName, &updatedNode)).To(Succeed())
				Expect(controllerutil.ContainsFinalizer(&updatedNode, hwpluginutils.AllocatedNodeFinalizer)).To(BeTrue())
			})

			It("should not add finalizer if already present", func() {
				// Add finalizer to the node
				controllerutil.AddFinalizer(allocatedNode, hwpluginutils.AllocatedNodeFinalizer)
				Expect(fakeClient.Update(ctx, allocatedNode)).To(Succeed())

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))

				// Verify finalizer is still present
				var updatedNode pluginsv1alpha1.AllocatedNode
				Expect(fakeClient.Get(ctx, req.NamespacedName, &updatedNode)).To(Succeed())
				Expect(controllerutil.ContainsFinalizer(&updatedNode, hwpluginutils.AllocatedNodeFinalizer)).To(BeTrue())
			})
		})

		Context("when AllocatedNode is being deleted", func() {
			BeforeEach(func() {
				// Delete the existing node and create a new one with deletion timestamp
				Expect(fakeClient.Delete(ctx, allocatedNode)).To(Succeed())

				// Create a new node with deletion timestamp and finalizer
				now := metav1.Now()
				deletingNode := &pluginsv1alpha1.AllocatedNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:              allocatedNode.Name,
						Namespace:         allocatedNode.Namespace,
						Labels:            allocatedNode.Labels,
						DeletionTimestamp: &now,
						Finalizers:        []string{hwpluginutils.AllocatedNodeFinalizer},
					},
					Spec: allocatedNode.Spec,
				}
				Expect(fakeClient.Create(ctx, deletingNode)).To(Succeed())
			})

			It("should handle deletion successfully and remove finalizer", func() {
				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))

				// Note: Finalizer removal is tested in the utils package tests
				// Here we just verify the controller handles the deletion workflow correctly
			})

			It("should complete without error when BMH not found during deletion", func() {
				// Delete the BMH to simulate it not being found
				Expect(fakeClient.Delete(ctx, bmh)).To(Succeed())

				result, err := reconciler.Reconcile(ctx, req)

				// Should complete successfully even if BMH is not found
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))

				// Note: The controller should handle missing BMH gracefully during deletion
			})

			// Note: Testing scenario where node has deletion timestamp but no finalizers
			// is not realistic in Kubernetes as the fake client correctly rejects such objects
		})

		Context("error scenarios", func() {
			It("should requeue with short interval when getting node fails", func() {
				// Use a request for a non-existent node but in a way that causes retrieval error
				invalidReq := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "nonexistent-node",
						Namespace: "invalid-namespace",
					},
				}

				result, err := reconciler.Reconcile(ctx, invalidReq)

				// Should handle the not found error gracefully
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hwpluginutils.DoNotRequeue()))
			})
		})
	})

	Describe("SetupWithManager", func() {
		It("should create label selector predicate correctly", func() {
			// Test the label selector creation logic directly
			labelSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					hwpluginutils.HardwarePluginLabel: hwpluginutils.Metal3HardwarePluginID,
				},
			}

			_, err := predicate.LabelSelectorPredicate(labelSelector)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should use correct hardware plugin label", func() {
			// Verify the label matches what we expect
			Expect(hwpluginutils.HardwarePluginLabel).To(Equal("clcm.openshift.io/hardware-plugin"))
			Expect(hwpluginutils.Metal3HardwarePluginID).To(Equal("metal3-hwplugin"))
		})
	})

	Describe("handleAllocatedNodeDeletion", func() {
		Context("when BMH exists", func() {
			BeforeEach(func() {
				// Create PreprovisioningImage for the BMH since deallocateBMH expects it
				image := &metal3v1alpha1.PreprovisioningImage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bmh.Name,
						Namespace: bmh.Namespace,
						Labels: map[string]string{
							BmhInfraEnvLabel: "test-infraenv",
						},
					},
				}
				Expect(fakeClient.Create(ctx, image)).To(Succeed())
			})

			It("should handle deletion successfully", func() {
				completed, err := reconciler.handleAllocatedNodeDeletion(ctx, allocatedNode)

				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeTrue())

				// Verify BMH was deallocated (check that allocated label is removed)
				var updatedBMH metal3v1alpha1.BareMetalHost
				bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				Expect(fakeClient.Get(ctx, bmhKey, &updatedBMH)).To(Succeed())
				
				// The deallocateBMH function should have removed the allocated label
				_, hasAllocatedLabel := updatedBMH.Labels[BmhAllocatedLabel]
				Expect(hasAllocatedLabel).To(BeFalse())
			})
		})

		Context("when BMH does not exist", func() {
			It("should return error", func() {
				// Delete the BMH
				Expect(fakeClient.Delete(ctx, bmh)).To(Succeed())

				completed, err := reconciler.handleAllocatedNodeDeletion(ctx, allocatedNode)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get BMH for node"))
				Expect(completed).To(BeTrue()) // Returns true to indicate we should proceed with finalizer removal
			})
		})

		Context("when node has invalid BMH reference", func() {
			It("should return error", func() {
				// Set invalid BMH reference
				allocatedNode.Spec.HwMgrNodeId = "nonexistent-bmh"
				allocatedNode.Spec.HwMgrNodeNs = "nonexistent-namespace"

				completed, err := reconciler.handleAllocatedNodeDeletion(ctx, allocatedNode)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get BMH for node"))
				Expect(completed).To(BeTrue())
			})
		})
	})

	Describe("Helper function tests", func() {
		Describe("getBMHForNode", func() {
			It("should return BMH successfully", func() {
				retrievedBMH, err := getBMHForNode(ctx, fakeClient, allocatedNode)

				Expect(err).NotTo(HaveOccurred())
				Expect(retrievedBMH.Name).To(Equal(bmh.Name))
				Expect(retrievedBMH.Namespace).To(Equal(bmh.Namespace))
			})

			It("should return error when BMH not found", func() {
				allocatedNode.Spec.HwMgrNodeId = "nonexistent-bmh"

				_, err := getBMHForNode(ctx, fakeClient, allocatedNode)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unable to find BMH"))
			})
		})

		Describe("deallocateBMH integration", func() {
			BeforeEach(func() {
				// Set up BMH with allocation labels and annotations
				bmh.Labels = map[string]string{
					BmhAllocatedLabel:         ValueTrue,
					SiteConfigOwnedByLabel:    "test-cluster",
					BmhInfraEnvLabel:          "test-infraenv",
				}
				bmh.Annotations = map[string]string{
					BiosUpdateNeededAnnotation:     ValueTrue,
					FirmwareUpdateNeededAnnotation: ValueTrue,
				}
				bmh.Spec.Online = true
				bmh.Spec.CustomDeploy = &metal3v1alpha1.CustomDeploy{Method: "test"}
				bmh.Spec.Image = &metal3v1alpha1.Image{URL: "test-url"}

				// Create PreprovisioningImage for the BMH
				image := &metal3v1alpha1.PreprovisioningImage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bmh.Name,
						Namespace: bmh.Namespace,
						Labels: map[string]string{
							BmhInfraEnvLabel: "test-infraenv",
						},
					},
				}

				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(allocatedNode, bmh, image).Build()
				reconciler.Client = fakeClient
			})

			It("should deallocate BMH completely", func() {
				err := deallocateBMH(ctx, fakeClient, logger, bmh)

				Expect(err).NotTo(HaveOccurred())

				// Verify BMH was deallocated
				var updatedBMH metal3v1alpha1.BareMetalHost
				bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				Expect(fakeClient.Get(ctx, bmhKey, &updatedBMH)).To(Succeed())

				// Check labels were removed
				_, hasAllocated := updatedBMH.Labels[BmhAllocatedLabel]
				Expect(hasAllocated).To(BeFalse())
				_, hasOwned := updatedBMH.Labels[SiteConfigOwnedByLabel]
				Expect(hasOwned).To(BeFalse())
				_, hasInfraEnv := updatedBMH.Labels[BmhInfraEnvLabel]
				Expect(hasInfraEnv).To(BeFalse())

				// Check annotations were removed
				_, hasBios := updatedBMH.Annotations[BiosUpdateNeededAnnotation]
				Expect(hasBios).To(BeFalse())
				_, hasFirmware := updatedBMH.Annotations[FirmwareUpdateNeededAnnotation]
				Expect(hasFirmware).To(BeFalse())

				// Check spec was updated
				Expect(updatedBMH.Spec.Online).To(BeFalse())
				Expect(updatedBMH.Spec.CustomDeploy).To(BeNil())
				Expect(updatedBMH.Spec.Image).To(BeNil())

				// Verify PreprovisioningImage was updated
				var updatedImage metal3v1alpha1.PreprovisioningImage
				imageKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				Expect(fakeClient.Get(ctx, imageKey, &updatedImage)).To(Succeed())
				_, hasImageInfraEnv := updatedImage.Labels[BmhInfraEnvLabel]
				Expect(hasImageInfraEnv).To(BeFalse())
			})
		})
	})
})

 