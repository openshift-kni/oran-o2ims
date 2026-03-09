/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

var _ = Describe("OCloudSite Controller", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When creating an OCloudSite", func() {
		It("should automatically add the finalizer", func() {
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-finalizer",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-FINALIZER-001",
					GlobalLocationID: "LOC-NONEXISTENT", // Location doesn't exist
					Description:      "Testing finalizer addition",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for the finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.OCloudSiteFinalizer)
			}, timeout, interval).Should(BeTrue())
		})

		It("should set Ready=True when Location is valid and ready", func() {
			// First create a valid Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-FOR-SITE-001",
					Description:      "Testing valid reference",
					Address:          ptrString("Valid Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			// Now create the OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-valid-ref",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-VALID-REF-001",
					GlobalLocationID: "LOC-FOR-SITE-001", // References the location above
					Description:      "Testing valid Location reference",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for the Ready=True condition
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionTrue &&
						cond.Reason == inventoryv1alpha1.ReasonReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should set Ready=False with ParentNotFound when Location does not exist", func() {
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-invalid-ref",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-INVALID-REF-001",
					GlobalLocationID: "LOC-DOES-NOT-EXIST",
					Description:      "Testing invalid Location reference",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for the Ready=False condition with ParentNotFound reason
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
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
			}, timeout, interval).Should(BeTrue())
		})

		// Note: We cannot test "ParentNotReady" for OCloudSite because Location
		// has no parent and is always Ready=True.
	})

	Context("When creating an OCloudSite with duplicate siteId", func() {
		It("should set Ready=False with DuplicateID reason on the second OCloudSite", func() {
			// Create a Location first (required for valid OCloudSite)
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-dup-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-DUP-SITE-001",
					Description:      "Location for duplicate site test",
					Address:          ptrString("Duplicate Site Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})
			waitForLocationReady(location)

			// Create the first OCloudSite
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-first",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUPLICATE-001",
					GlobalLocationID: "LOC-DUP-SITE-001",
					Description:      "First site with this ID",
				},
			}
			Expect(k8sClient.Create(ctx, site1)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site1)
			})

			// Wait for first OCloudSite to be Ready=True
			waitForOCloudSiteReady(site1)

			// Create the second OCloudSite with the same siteId
			site2 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-second",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUPLICATE-001", // Same as first!
					GlobalLocationID: "LOC-DUP-SITE-001",
					Description:      "Second site with same ID",
				},
			}
			Expect(k8sClient.Create(ctx, site2)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site2)
			})

			// Wait for second OCloudSite to have Ready=False with DuplicateID reason
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonDuplicateID {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Second OCloudSite should have Ready=False with DuplicateID reason")

			// Verify the message mentions the first site
			Eventually(func() string {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				if err != nil {
					return ""
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Reason == inventoryv1alpha1.ReasonDuplicateID {
						return cond.Message
					}
				}
				return ""
			}, timeout, interval).Should(ContainSubstring("test-site-dup-first"), "Message should reference the existing OCloudSite")
		})

		It("should allow deleting a duplicate OCloudSite even when ResourcePools reference the same siteId", func() {
			// This test verifies that a duplicate OCloudSite (Ready=False, DuplicateID)
			// can be deleted even when ResourcePools reference the siteId,
			// because those ResourcePools are served by the original Ready OCloudSite.

			// Create a Location first
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-dup-site-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-DUP-SITE-DELETE-001",
					Description:      "Location for duplicate site deletion test",
					Address:          ptrString("Site Delete Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})
			waitForLocationReady(location)

			// Create the first OCloudSite (will be Ready=True)
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-delete-first",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUP-DELETE-001",
					GlobalLocationID: "LOC-DUP-SITE-DELETE-001",
					Description:      "Original site",
				},
			}
			Expect(k8sClient.Create(ctx, site1)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site1)
			})
			waitForOCloudSiteReady(site1)

			// Create a ResourcePool that references this siteId
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-for-dup-site-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-DUP-SITE-DELETE-001",
					OCloudSiteId:   "SITE-DUP-DELETE-001",
					Description:    "Pool referencing the site",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for ResourcePool to be Ready
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be Ready")

			// Create the second OCloudSite with the same siteId (will be Ready=False, DuplicateID)
			site2 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-delete-second",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUP-DELETE-001", // Same as first!
					GlobalLocationID: "LOC-DUP-SITE-DELETE-001",
					Description:      "Duplicate site to be deleted",
				},
			}
			Expect(k8sClient.Create(ctx, site2)).To(Succeed())

			// Wait for second OCloudSite to have Ready=False with DuplicateID
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonDuplicateID {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Second OCloudSite should have DuplicateID")

			// Delete the duplicate OCloudSite - this should succeed because it was never Ready
			Expect(k8sClient.Delete(ctx, site2)).To(Succeed())

			// The duplicate OCloudSite should be deleted successfully (not blocked by dependents)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Duplicate OCloudSite should be deleted (not blocked by ResourcePool)")

			// Verify the original OCloudSite and ResourcePool are still intact and Ready
			fetchedSite := &inventoryv1alpha1.OCloudSite{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site1), fetchedSite)).To(Succeed())
			Expect(fetchedSite.Status.Conditions).To(ContainElement(
				SatisfyAll(
					HaveField("Type", inventoryv1alpha1.ConditionTypeReady),
					HaveField("Status", metav1.ConditionTrue),
				),
			), "Original OCloudSite should still be Ready")

			fetchedPool := &inventoryv1alpha1.ResourcePool{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetchedPool)).To(Succeed())
			Expect(fetchedPool.Status.Conditions).To(ContainElement(
				SatisfyAll(
					HaveField("Type", inventoryv1alpha1.ConditionTypeReady),
					HaveField("Status", metav1.ConditionTrue),
				),
			), "ResourcePool should still be Ready")
		})

		It("should transition to Ready=True when the conflicting OCloudSite is deleted", func() {
			// This test verifies that when the original OCloudSite is deleted,
			// the duplicate OCloudSite automatically transitions to Ready=True
			// via the watch mechanism.

			// First create a valid parent Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-dup-resolve",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-DUP-RESOLVE-SITE-001",
					Description:      "Location for duplicate OCloudSite resolution test",
					Address:          ptrString("Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})
			waitForLocationReady(location)

			// Create the first OCloudSite (will be Ready=True)
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-resolve-first",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUP-RESOLVE-001",
					GlobalLocationID: "LOC-DUP-RESOLVE-SITE-001",
					Description:      "First site to be deleted",
				},
			}
			Expect(k8sClient.Create(ctx, site1)).To(Succeed())

			// Wait for first OCloudSite to be Ready=True
			waitForOCloudSiteReady(site1)

			// Create the second OCloudSite with the same siteId (will be Ready=False, DuplicateID)
			site2 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dup-resolve-second",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DUP-RESOLVE-001", // Same as first!
					GlobalLocationID: "LOC-DUP-RESOLVE-SITE-001",
					Description:      "Second site that will become valid",
				},
			}
			Expect(k8sClient.Create(ctx, site2)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site2)
			})

			// Wait for second OCloudSite to have Ready=False with DuplicateID
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonDuplicateID {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Second OCloudSite should start with DuplicateID")

			// Delete the first OCloudSite (the original)
			Expect(k8sClient.Delete(ctx, site1)).To(Succeed())

			// Wait for first OCloudSite to be fully deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site1), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "First OCloudSite should be deleted")

			// The watch should trigger re-reconciliation of the second OCloudSite.
			// It should now transition to Ready=True since there's no more conflict.
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site2), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionTrue &&
						cond.Reason == inventoryv1alpha1.ReasonReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Second OCloudSite should transition to Ready=True after first is deleted")
		})
	})

	Context("When parent Location is created later", func() {
		It("should transition from ParentNotFound to Ready=True when Location is created and ready", func() {
			// Create the OCloudSite FIRST, before Location exists
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-late-location",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-LATE-LOC-001",
					GlobalLocationID: "LOC-LATE-002", // Location doesn't exist yet
					Description:      "Testing watch on Location creation",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for Ready=False (Location doesn't exist)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
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
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be Ready=False (ParentNotFound) initially")

			// Now create the Location AFTER OCloudSite
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-late-for-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-LATE-002", // Matches OCloudSite's reference
					Description:      "Testing watch triggers re-reconciliation",
					Address:          ptrString("Late Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// The watch should trigger re-reconciliation of OCloudSite
			// It should now transition to Ready=True
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionTrue &&
						cond.Reason == inventoryv1alpha1.ReasonReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "OCloudSite should transition to Ready=True after Location is created")
		})
	})

	Context("When deleting an OCloudSite with dependent ResourcePools", func() {
		It("should block deletion until dependents are removed", func() {
			// Create a Location first (for valid reference)
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-site-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-SITE-DELETE-001",
					Description:      "Testing deletion",
					Address:          ptrString("Delete Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			// Create the OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-delete-blocked",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DELETE-BLOCKED-001",
					GlobalLocationID: "LOC-SITE-DELETE-001",
					Description:      "Testing deletion blocking",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())

			// Wait for OCloudSite to be Ready before creating dependent ResourcePool
			waitForOCloudSiteReady(site)

			// Create a dependent ResourcePool
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-dependent",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-DEP-001",
					OCloudSiteId:   "SITE-DELETE-BLOCKED-001", // References the site above
					Description:    "Pool that depends on site",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for pool finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Try to delete the OCloudSite
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())

			// Verify the OCloudSite still exists (deletion blocked by finalizer)
			Consistently(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				return err == nil && fetched.DeletionTimestamp != nil
			}, 4*time.Second, interval).Should(BeTrue(), "OCloudSite should still exist with DeletionTimestamp set")

			// Verify the Deleting condition is set
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeDeleting &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonDependentsExist {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Delete the dependent ResourcePool
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			// Wait for ResourcePool to be fully deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted (NotFound)")

			// Now the OCloudSite should be deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted (NotFound)")
		})
	})

	Context("When deleting an OCloudSite without dependents", func() {
		It("should delete successfully", func() {
			// Create a Location first
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-site-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-SITE-DELETE-OK-001",
					Description:      "Testing successful deletion",
					Address:          ptrString("OK Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DELETE-OK-001",
					GlobalLocationID: "LOC-SITE-DELETE-OK-001",
					Description:      "Testing successful deletion",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.OCloudSiteFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Delete the OCloudSite
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())

			// OCloudSite should be deleted since there are no dependents
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted (NotFound)")
		})
	})
})
