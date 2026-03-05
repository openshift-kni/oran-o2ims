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
	"github.com/openshift-kni/oran-o2ims/internal/controllers"
)

var _ = Describe("ResourcePool Controller", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When creating a ResourcePool", func() {
		It("should automatically add the finalizer", func() {
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-finalizer",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-FINALIZER-001",
					OCloudSiteId:   "SITE-NONEXISTENT", // Site doesn't exist
					Name:           "Test Pool for Finalizer",
					Description:    "Testing finalizer addition",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for the finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
			}, timeout, interval).Should(BeTrue())
		})

		It("should set Ready=False when OCloudSite reference is invalid", func() {
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-invalid-ref",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-INVALID-REF-001",
					OCloudSiteId:   "SITE-DOES-NOT-EXIST",
					Name:           "Test Pool Invalid Ref",
					Description:    "Testing invalid OCloudSite reference",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for the Ready=False condition with InvalidReference reason
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonInvalidReference {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should transition from Ready=False to Ready=True when OCloudSite is created after ResourcePool", func() {
			// Create the ResourcePool FIRST, before OCloudSite exists
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-late-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-LATE-SITE-001",
					OCloudSiteId:   "SITE-LATE-001", // Site doesn't exist yet
					Name:           "Pool Created Before Site",
					Description:    "Testing watch on OCloudSite creation",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for Ready=False (OCloudSite doesn't exist)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonInvalidReference {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be Ready=False initially")

			// Now create the Location (required for OCloudSite)
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-late",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-LATE-001",
					Name:             "Location Created Late",
					Description:      "Testing late creation",
					Address:          ptrString("Late Location Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})
			waitForLocationReady(location)

			// Now create the OCloudSite AFTER ResourcePool
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-late",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-LATE-001", // Matches ResourcePool's reference
					GlobalLocationID: "LOC-LATE-001",
					Name:             "Site Created Late",
					Description:      "Testing watch triggers re-reconciliation",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// The watch should trigger re-reconciliation of ResourcePool
			// It should now transition to Ready=True
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
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
			}, timeout, interval).Should(BeTrue(), "ResourcePool should transition to Ready=True after OCloudSite is created")
		})

		It("should set Ready=True when OCloudSite reference is valid", func() {
			// First create a valid Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-FOR-POOL-001",
					Name:             "Location for Pool Test",
					Description:      "Testing valid reference",
					Address:          ptrString("Pool Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			// Create a valid OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-for-pool",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-FOR-POOL-001",
					GlobalLocationID: "LOC-FOR-POOL-001", // References the location above
					Name:             "Site for Pool Test",
					Description:      "Testing valid reference",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready before creating ResourcePool
			waitForOCloudSiteReady(site)

			// Create the ResourcePool
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-valid-ref",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-VALID-REF-001",
					OCloudSiteId:   "SITE-FOR-POOL-001", // References the site above
					Name:           "Test Pool Valid Ref",
					Description:    "Testing valid OCloudSite reference",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for the Ready=True condition
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
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When deleting a ResourcePool with dependent BareMetalHosts", func() {
		It("should block deletion until dependents are removed", func() {
			// Create the hierarchy: Location -> OCloudSite -> ResourcePool -> BareMetalHost
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-POOL-DELETE-001",
					Name:             "Location for Pool Delete Test",
					Description:      "Testing deletion",
					Address:          ptrString("Pool Delete Test Address"),
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
					Name:      "test-site-for-pool-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-POOL-DELETE-001",
					GlobalLocationID: "LOC-POOL-DELETE-001", // References location above
					Name:             "Site for Pool Delete Test",
					Description:      "Testing deletion",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready before creating ResourcePool
			waitForOCloudSiteReady(site)

			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-delete-blocked",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-DELETE-BLOCKED-001",
					OCloudSiteId:   "SITE-POOL-DELETE-001", // References site above
					Name:           "Pool with Dependents",
					Description:    "Testing deletion blocking",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Create a dependent BareMetalHost with the resourcePoolId label
			bmh := &bmhv1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh-dependent",
					Namespace: testNamespace,
					Labels: map[string]string{
						controllers.BMHLabelResourcePoolID: "POOL-DELETE-BLOCKED-001", // References pool above
					},
				},
				Spec: bmhv1alpha1.BareMetalHostSpec{
					Online: false,
				},
			}
			Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

			// Try to delete the ResourcePool
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			// Verify the ResourcePool still exists (deletion blocked by finalizer)
			Consistently(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return err == nil && fetched.DeletionTimestamp != nil
			}, 4*time.Second, interval).Should(BeTrue(), "ResourcePool should still exist with DeletionTimestamp set")

			// Verify the Deleting condition is set
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
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

			// Delete the dependent BareMetalHost
			Expect(k8sClient.Delete(ctx, bmh)).To(Succeed())

			// Wait for BareMetalHost to be fully deleted
			Eventually(func() bool {
				fetched := &bmhv1alpha1.BareMetalHost{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(bmh), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "BareMetalHost should be deleted (NotFound)")

			// Now the ResourcePool should be deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted (NotFound)")
		})
	})

	Context("When deleting a ResourcePool with BMH referencing a different pool", func() {
		It("should delete successfully since the BMH is not a dependent", func() {
			// Create the hierarchy: Location -> OCloudSite -> ResourcePool
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool-mismatch",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-POOL-MISMATCH-001",
					Name:             "Location for Pool Mismatch Test",
					Description:      "Testing deletion with mismatched BMH",
					Address:          ptrString("Mismatch Test Address"),
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
					Name:      "test-site-for-pool-mismatch",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-POOL-MISMATCH-001",
					GlobalLocationID: "LOC-POOL-MISMATCH-001",
					Name:             "Site for Pool Mismatch Test",
					Description:      "Testing deletion with mismatched BMH",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready before creating ResourcePool
			waitForOCloudSiteReady(site)

			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-mismatch",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-MISMATCH-001",
					OCloudSiteId:   "SITE-POOL-MISMATCH-001",
					Name:           "Pool with Mismatched BMH",
					Description:    "Testing deletion when BMH references different pool",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Create a BareMetalHost with a different resourcePoolId label (not matching our pool)
			bmh := &bmhv1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh-different-pool",
					Namespace: testNamespace,
					Labels: map[string]string{
						controllers.BMHLabelResourcePoolID: "SOME-OTHER-POOL-ID", // Does NOT match pool above
					},
				},
				Spec: bmhv1alpha1.BareMetalHostSpec{
					Online: false,
				},
			}
			Expect(k8sClient.Create(ctx, bmh)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bmh)
			})

			// Delete the ResourcePool
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			// ResourcePool should be deleted since the BMH references a different pool
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted (NotFound): BMH references different pool")
		})
	})

	Context("When deleting a ResourcePool without dependents", func() {
		It("should delete successfully", func() {
			// Create the hierarchy: Location -> OCloudSite -> ResourcePool
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-POOL-DELETE-OK-001",
					Name:             "Location for Pool Delete OK",
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
					Name:      "test-site-for-pool-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-POOL-DELETE-OK-001",
					GlobalLocationID: "LOC-POOL-DELETE-OK-001",
					Name:             "Site for Pool Delete OK",
					Description:      "Testing successful deletion",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready before creating ResourcePool
			waitForOCloudSiteReady(site)

			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "POOL-DELETE-OK-001",
					OCloudSiteId:   "SITE-POOL-DELETE-OK-001",
					Name:           "Pool without Dependents",
					Description:    "Testing successful deletion",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.ResourcePoolFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Delete the ResourcePool
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			// ResourcePool should be deleted since there are no dependents
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted (NotFound)")
		})
	})
})
