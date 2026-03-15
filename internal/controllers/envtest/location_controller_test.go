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

var _ = Describe("Location Controller", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("When creating a Location", func() {
		It("should set Ready condition to True", func() {
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing Ready condition",
					Address:     ptrString("456 Ready Street"),
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

	Context("When deleting a Location", func() {
		It("should delete successfully even with dependent OCloudSites", func() {
			// Create the Location first
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-delete-with-deps",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing deletion without blocking",
					Address:     ptrString("789 NoBlock Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())

			// Wait for Location to be Ready before creating OCloudSite
			waitForLocationReady(location)

			// Create a dependent OCloudSite
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-dependent-delete",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location-delete-with-deps",
					Description:        "Site that depends on location",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, site)
			})

			// Wait for site to be Ready
			waitForOCloudSiteReady(site)

			// Delete the Location: it should succeed immediately (no blocking)
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Location should be deleted
			Eventually(func() bool {
				fetched := &inventoryv1alpha1.Location{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Location should be deleted (NotFound)")

			// OCloudSite should transition to Ready=False (parent not found)
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
			}, timeout, interval).Should(BeTrue(), "OCloudSite should transition to Ready=False (ParentNotFound)")
		})

		It("should delete successfully without dependents", func() {
			location := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-location-delete-ok",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Testing successful deletion",
					Address:     ptrString("111 Success Street"),
				},
			}
			Expect(k8sClient.Create(ctx, location)).To(Succeed())

			// Wait for Ready condition
			waitForLocationReady(location)

			// Delete the Location
			Expect(k8sClient.Delete(ctx, location)).To(Succeed())

			// Location should be deleted
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
