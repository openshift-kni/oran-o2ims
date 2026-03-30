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

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

var _ = Describe("ResourcePool Controller", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When creating a ResourcePool", func() {
		It("should set Ready=True when OCloudSite is valid and ready", func() {
			// First create a valid Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing valid reference",
					Address:     ptrString("Pool Test Address"),
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
					GlobalLocationName: "test-location-for-pool", // References the location by metadata.name
					Description:        "Testing valid reference",
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
					OCloudSiteName: "test-site-for-pool", // References the site by metadata.name
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
						cond.Status == metav1.ConditionTrue &&
						cond.Reason == inventoryv1alpha1.ReasonReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should set Ready=False with ParentNotFound when OCloudSite does not exist", func() {
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-invalid-ref",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "site-does-not-exist",
					Description:    "Testing invalid OCloudSite reference",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for the Ready=False condition with ParentNotFound reason
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
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

		It("should set Ready=False with ParentNotReady when OCloudSite exists but is not ready", func() {
			// Create an OCloudSite that references a non-existent Location
			// This makes OCloudSite exist but be Ready=False
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "loc-nonexistent", // Location doesn't exist
					Description:        "Site with invalid Location reference",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready=False (ParentNotFound)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be Ready=False")

			// Create ResourcePool referencing the not-ready OCloudSite
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-parent-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site-not-ready", // References the not-ready site by metadata.name
					Description:    "Testing ParentNotReady condition",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for the Ready=False condition with ParentNotReady reason
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonParentNotReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "ResourcePool should have Ready=False with ParentNotReady reason")
		})
	})

	Context("When parent OCloudSite is created or becomes ready later", func() {
		It("should transition from ParentNotFound to Ready=True when OCloudSite is created and ready", func() {
			// Create the ResourcePool FIRST, before OCloudSite exists
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-late-site",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site-late", // Site doesn't exist yet
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
						cond.Reason == inventoryv1alpha1.ReasonParentNotFound {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be Ready=False (ParentNotFound) initially")

			// Now create the Location (required for OCloudSite)
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-late",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing late creation",
					Address:     ptrString("Late Location Address"),
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
					Name:      "test-site-late", // Matches ResourcePool's oCloudSiteName reference
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location-late",
					Description:        "Testing watch triggers re-reconciliation",
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

		It("should transition from ParentNotReady to Ready=True when OCloudSite becomes ready", func() {
			// Create OCloudSite without its parent Location (will be NotReady)
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-becomes-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location-makes-site-ready", // Location doesn't exist yet
					Description:        "Testing watch on parent status change",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for OCloudSite to be Ready=False
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be Ready=False initially")

			// Create ResourcePool referencing the not-ready OCloudSite
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-waits-for-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site-becomes-ready", // References site by metadata.name
					Description:    "Testing transition when parent becomes ready",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for ResourcePool to be Ready=False (ParentNotReady)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonParentNotReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be Ready=False (ParentNotReady)")

			// Now create the Location, making OCloudSite become Ready=True
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-makes-site-ready", // Matches OCloudSite's globalLocationName reference
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Creating this makes OCloudSite ready",
					Address:     ptrString("Ready Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// ResourcePool should transition to Ready=True via watch cascade
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
			}, timeout, interval).Should(BeTrue(), "ResourcePool should transition to Ready=True when OCloudSite becomes ready")
		})

		It("should cascade Ready status through entire hierarchy when Location is created last", func() {
			// This test verifies the full cascade:
			// 1. Create ResourcePool → Ready=False (ParentNotFound, OCloudSite doesn't exist)
			// 2. Create OCloudSite → ResourcePool becomes Ready=False (ParentNotReady, OCloudSite not ready)
			// 3. Create Location → OCloudSite becomes Ready=True → ResourcePool becomes Ready=True

			// Step 1: Create ResourcePool first
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool-cascade",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site-cascade", // Site doesn't exist yet
					Description:    "Testing full hierarchy cascade",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pool)
			})

			// Wait for Ready=False (ParentNotFound)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
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
			}, timeout, interval).Should(BeTrue(), "Step 1: ResourcePool should start with Ready=False (ParentNotFound)")

			// Step 2: Create OCloudSite (without Location)
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-cascade", // Matches ResourcePool's oCloudSiteName
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location-cascade", // Location doesn't exist yet
					Description:        "Testing full hierarchy cascade",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// ResourcePool should transition to ParentNotReady (OCloudSite exists but not ready)
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				if err != nil {
					return false
				}
				for _, cond := range fetched.Status.Conditions {
					if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
						cond.Status == metav1.ConditionFalse &&
						cond.Reason == inventoryv1alpha1.ReasonParentNotReady {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Step 2: ResourcePool should transition to Ready=False (ParentNotReady)")

			// Step 3: Create Location
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-cascade", // Matches OCloudSite's globalLocationName
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing full hierarchy cascade",
					Address:     ptrString("Cascade Test Address"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Both OCloudSite and ResourcePool should now be Ready=True
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
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
			}, timeout, interval).Should(BeTrue(), "Step 3a: OCloudSite should become Ready=True")

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
			}, timeout, interval).Should(BeTrue(), "Step 3b: ResourcePool should become Ready=True through cascade")
		})
	})

	Context("When deleting a ResourcePool", func() {
		It("should delete successfully (no finalizer blocking)", func() {
			// Create the hierarchy: Location -> OCloudSite -> ResourcePool
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-for-pool-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing successful deletion",
					Address:     ptrString("OK Address"),
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
					GlobalLocationName: "test-location-for-pool-delete-ok",
					Description:        "Testing successful deletion",
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
					OCloudSiteName: "test-site-for-pool-delete-ok",
					Description:    "Testing successful deletion",
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for Ready condition
			waitForResourcePoolReady(pool)

			// Delete the ResourcePool
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			// ResourcePool should be deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.ResourcePool{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "ResourcePool should be deleted (NotFound)")
		})
	})
})
