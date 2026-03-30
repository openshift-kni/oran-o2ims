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

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

// This test validates the deletion behavior of the hierarchy controllers.
//
// CRs can be deleted in any order. Child CRs will transition to Ready=False
//  (ParentNotFound) when their parent is deleted, but deletion itself is not blocked.
//
// The undeploy sequence is now simpler:
//  1. Delete all resources via kustomize (any order works)
//  2. Children will transition to Ready=False when parents are deleted

var _ = Describe("Undeploy Scenario", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When deleting hierarchy in correct order (children first)", func() {
		It("should allow clean deletion in any order", func() {

			// STEP 1: Create a complete hierarchy (Location -> OCloudSite -> ResourcePool -> BMH)

			// Create Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-location",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Location for undeploy test",
					Address:     ptrString("Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready
			waitForLocationReady(location)

			// Create OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "undeploy-test-location",
					Description:        "OCloudSite for undeploy test",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready
			waitForOCloudSiteReady(site)

			// Create ResourcePool
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-pool",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "undeploy-test-site",
					Description:    "ResourcePool for undeploy test",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for ResourcePool to be Ready
			waitForResourcePoolReady(pool)

			// Create BareMetalHost (associated via labels)
			bmh := &bmhv1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-test-bmh",
					Namespace: testNamespace,
					Labels:    map[string]string{},
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

			// STEP 2: Delete in children-first order (cleanest approach)

			// 2a: Delete BareMetalHost first
			By("Deleting BareMetalHost first")
			Expect(k8sClient.Delete(ctx, bmh)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bmh), &bmhv1alpha1.BareMetalHost{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "BareMetalHost should be deleted")

			// 2b: Delete ResourcePool
			By("Deleting ResourcePool")
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), &inventoryv1alpha1.ResourcePool{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted")

			// 2c: Delete OCloudSite
			By("Deleting OCloudSite")
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), &inventoryv1alpha1.OCloudSite{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted")

			// 2d: Delete Location
			By("Deleting Location")
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

		It("should allow deleting parent before children (children become not ready)", func() {
			// This test shows that parents can be deleted first
			// and children will gracefully transition to Ready=False

			// Create hierarchy
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-parent-first-location",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Location for parent-first deletion test",
					Address:     ptrString("Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			waitForLocationReady(location)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undeploy-parent-first-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "undeploy-parent-first-location",
					Description:        "OCloudSite for parent-first deletion test",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})
			waitForOCloudSiteReady(site)

			// Delete Location FIRST (parent before child)
			By("Deleting Location while OCloudSite still references it")
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Location should be deleted immediately (no blocking)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), &inventoryv1alpha1.Location{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted immediately")

			// OCloudSite should transition to Ready=False (ParentNotFound)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched); err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonParentNotFound {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "OCloudSite should transition to Ready=False (ParentNotFound)")

			// Now delete OCloudSite
			By("Deleting orphaned OCloudSite")
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), &inventoryv1alpha1.OCloudSite{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted")
		})
	})
})
