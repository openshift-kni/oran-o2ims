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

var _ = Describe("Location Controller", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When creating a Location", func() {
		It("should automatically add the finalizer", func() {
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-finalizer",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-FINALIZER-001",
					Description:      "Testing finalizer addition",
					Address:          ptrString("123 Test Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for the finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.LocationFinalizer)
			}, timeout, interval).Should(BeTrue())
		})

		It("should set Ready condition to True", func() {
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-READY-001",
					Description:      "Testing Ready condition",
					Address:          ptrString("456 Ready Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, location)
			})

			// Wait for the Ready condition
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
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

	Context("When deleting a Location with dependent OCloudSites", func() {
		It("should block deletion until dependents are removed", func() {
			// Create the Location first
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-delete-blocked",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-DELETE-BLOCKED",
					Description:      "Testing deletion blocking",
					Address:          ptrString("789 Block Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			// Create a dependent OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dependent",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "SITE-DEP-001",
					GlobalLocationID: "LOC-DELETE-BLOCKED", // References the location above
					Description:      "Site that depends on location",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())

			// Wait for site finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.OCloudSiteFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Try to delete the Location
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Verify the Location still exists (deletion blocked by finalizer)
			Consistently(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				return err == nil && fetched.DeletionTimestamp != nil
			}, 4*time.Second, interval).Should(BeTrue(), "Location should still exist with DeletionTimestamp set")

			// Verify the Deleting condition is set
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
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

			// Delete the dependent OCloudSite
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())

			// Wait for OCloudSite to be fully deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.OCloudSite{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "OCloudSite should be deleted (NotFound)")

			// Now the Location should be deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted (NotFound)")
		})
	})

	Context("When deleting a Location without dependents", func() {
		It("should delete successfully", func() {
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-DELETE-OK",
					Description:      "Testing successful deletion",
					Address:          ptrString("111 Success Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(fetched, inventoryv1alpha1.LocationFinalizer)
			}, timeout, interval).Should(BeTrue())

			// Delete the Location
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Location should be deleted since there are no dependents
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted (NotFound)")
		})
	})
})

// ptrString is a helper function to create a pointer to a string
func ptrString(s string) *string {
	return &s
}
