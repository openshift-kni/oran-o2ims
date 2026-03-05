/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"time"

	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

// This test validates the deletion behavior of the hierarchy controllers.
//
// Note: In a real `make undeploy`, the  controller is  stopped first, then
// finalizers are removed. In envtest, controllers keep running, so we test
// the controller's deletion behavior with dependents instead.
//
// The undeploy sequence in Makefile is:
//  1. Remove finalizers from ResourcePools (children first)
//  2. Remove finalizers from OCloudSites (middle layer)
//  3. Remove finalizers from Locations (parents)
//  4. Delete all resources via kustomize

var _ = Describe("Undeploy Scenario", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When deleting hierarchy in correct order (children first)", func() {
		It("should allow clean deletion when dependents are removed first", func() {

			// STEP 1: Create a complete hierarchy (Location -> OCloudSite -> ResourcePool -> BMH)

			// Create Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-location",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-UNDEPLOY-001",
					Description:      "Location for undeploy test",
					Address:          ptrString("Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready with finalizer
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched); err != nil {
					return false
				}
				hasFinalizer := controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.LocationFinalizer)
				isReady := false
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady && cond.Status == metav1.ConditionTrue {
						isReady = true
						break
					}
				}
				return hasFinalizer && isReady
			}, timeout, interval).Should(BeTrue(), "Location should have finalizer and be Ready")

			// Create OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-UNDEPLOY-001",
					GlobalLocationID: "LOC-UNDEPLOY-001",
					Description:      "OCloudSite for undeploy test",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready with finalizer
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched); err != nil {
					return false
				}
				hasFinalizer := controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.OCloudSiteFinalizer)
				isReady := false
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady && cond.Status == metav1.ConditionTrue {
						isReady = true
						break
					}
				}
				return hasFinalizer && isReady
			}, timeout, interval).Should(BeTrue(), "OCloudSite should have finalizer and be Ready")

			// Create ResourcePool
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-pool",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-UNDEPLOY-001",
					OCloudSiteId:   "SITE-UNDEPLOY-001",
					Description:    "ResourcePool for undeploy test",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for ResourcePool to be Ready with finalizer
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched); err != nil {
					return false
				}
				hasFinalizer := controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
				isReady := false
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady && cond.Status == metav1.ConditionTrue {
						isReady = true
						break
					}
				}
				return hasFinalizer && isReady
			}, timeout, interval).Should(BeTrue(), "ResourcePool should have finalizer and be Ready")

			// Create BareMetalHost (associated via labels)
			bmh := &bmhv1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-bmh",
					Namespace: testNamespace,
					Labels: map[string]string{
						"resources.clcm.openshift.io/siteId":         "SITE-UNDEPLOY-001",
						"resources.clcm.openshift.io/resourcePoolId": "POOL-UNDEPLOY-001",
					},
				},
				Spec: bmhv1alpha1.BareMetalHostSpec{
					BMC: bmhv1alpha1.BMCDetails{
						Address: "redfish://example.com/redfish/v1/Systems/1",
					},
					BootMACAddress: "00:11:22:33:44:55",
				},
			}
			Expect(k8sClient.Create(ctx, bmh)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bmh)
			})

			// Verify BMH was created
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(bmh), &bmhv1alpha1.BareMetalHost{})
			}, timeout, interval).Should(Succeed(), "BareMetalHost should be created")

			// STEP 2: Delete in correct order (children first)
			// This simulates the natural cleanup order needed for undeploy

			// 2a: Delete BareMetalHost first ---
			By("Deleting BareMetalHost first")
			Expect(k8sClient.Delete(ctx, bmh)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bmh), &bmhv1alpha1.BareMetalHost{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "BareMetalHost should be deleted")

			// 2b: Delete ResourcePool (no BMH dependents now) ---
			By("Deleting ResourcePool (no dependents)")
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), &inventoryv1alpha1.ResourcePool{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted")

			// 2c: Delete OCloudSite (no ResourcePool dependents now) ---
			By("Deleting OCloudSite (no dependents)")
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), &inventoryv1alpha1.OCloudSite{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted")

			// 2d: Delete Location (no OCloudSite dependents now) ---
			By("Deleting Location (no dependents)")
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), &inventoryv1alpha1.Location{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted")

			// STEP 3: Verify complete cleanup

			By("Verifying complete cleanup")

			// All hierarchy CRs should be gone
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(location), &inventoryv1alpha1.Location{})).
				To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), &inventoryv1alpha1.OCloudSite{})).
				To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), &inventoryv1alpha1.ResourcePool{})).
				To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bmh), &bmhv1alpha1.BareMetalHost{})).
				To(MatchError(ContainSubstring("not found")))
		})

		It("should demonstrate that deletion WITHOUT removing finalizers would hang on dependents", func() {
			// This test shows why the undeploy order matters

			// Create hierarchy
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-hang-test-location",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-HANG-001",
					Description:      "Location for hang test",
					Address:          ptrString("Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			waitForLocationReady(location)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-hang-test-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-HANG-001",
					GlobalLocationID: "LOC-HANG-001",
					Description:      "OCloudSite for hang test",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			waitForOCloudSiteReady(site)

			// Try to delete Location while OCloudSite still references it
			// The controller should block this with a "deletion blocked" condition
			By("Attempting to delete Location while dependent OCloudSite exists")
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Location should NOT be deleted immediately - it should have deletion blocked condition
			Eventually(func() string {
				fetched := &inventoryv1alpha1.Location{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched); err != nil {
					return "not found"
				}
				// Check if deletion is blocked
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeDeleting {
						return cond.Reason
					}
				}
				return "no deleting condition"
			}, timeout, interval).Should(Equal(inventoryv1alpha1.ReasonDependentsExist),
				"Location should have Deleting condition with DependentsExist reason")

			// Location should still exist (finalizer prevents deletion)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(location), &inventoryv1alpha1.Location{})).To(Succeed(),
				"Location should still exist because of finalizer and dependents")

			// Cleanup: Remove finalizers and delete (undeploy pattern)
			By("Cleaning up using undeploy pattern")

			// Delete site first (remove finalizer, then delete)
			fetchedSite := &inventoryv1alpha1.OCloudSite{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetchedSite)).To(Succeed())
			patch := client.MergeFrom(fetchedSite.DeepCopy())
			fetchedSite.Finalizers = nil
			Expect(k8sClient.Patch(ctx, fetchedSite, patch)).To(Succeed())
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), &inventoryv1alpha1.OCloudSite{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			// Now location can be deleted (remove finalizer first)
			fetchedLocation := &inventoryv1alpha1.Location{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetchedLocation)).To(Succeed())
			patch = client.MergeFrom(fetchedLocation.DeepCopy())
			fetchedLocation.Finalizers = nil
			Expect(k8sClient.Patch(ctx, fetchedLocation, patch)).To(Succeed())

			// Location should now be deleted (delete was already requested)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), &inventoryv1alpha1.Location{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted after finalizer removal")
		})
	})
})
