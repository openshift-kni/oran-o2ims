/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("FirmwareCatalogReconciler", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		reconciler *FirmwareCatalogReconciler
		catalog    *hwmgmtv1alpha1.FirmwareCatalog
		testNs     string
	)

	BeforeEach(func() {
		ctx = context.TODO()
		testNs = "oran-o2ims"

		catalog = &hwmgmtv1alpha1.FirmwareCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "firmware-catalog",
				Namespace:  testNs,
				Generation: 1,
			},
			Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{},
		}
	})

	setupReconciler := func(objs ...client.Object) {
		fakeClient = fakeclient.GetFakeClientFromObjects(objs...)
		reconciler = &FirmwareCatalogReconciler{
			Client: fakeClient,
			Logger: logger.With(slog.String("controller", "FirmwareCatalog")),
		}
	}

	reconcile := func() (ctrl.Result, error) {
		return reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      catalog.Name,
				Namespace: catalog.Namespace,
			},
		})
	}

	fetchCatalog := func() *hwmgmtv1alpha1.FirmwareCatalog {
		fetched := &hwmgmtv1alpha1.FirmwareCatalog{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{
			Name:      catalog.Name,
			Namespace: catalog.Namespace,
		}, fetched)).To(Succeed())
		return fetched
	}

	Describe("Reconcile", func() {
		Context("when the FirmwareCatalog does not exist", func() {
			It("should not return an error", func() {
				setupReconciler()
				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
			})
		})

		Context("when the catalog has valid entries", func() {
			It("should set Validation condition to True", func() {
				catalog.Spec.Images = []hwmgmtv1alpha1.FirmwareImage{
					{
						Name:      "dell-bios-2.3.5",
						Component: "bios",
						URL:       "https://example.com/bios.exe",
						Version:   "2.3.5",
						Vendor:    "Dell",
					},
					{
						Name:      "dell-bmc-7.10",
						Component: "bmc",
						URL:       "https://example.com/bmc.exe",
						Version:   "7.10",
					},
					{
						Name:      "broadcom-nic-25.2",
						Component: "nic",
						URL:       "http://example.com/nic.bin",
						Version:   "25.2",
						Vendor:    "Broadcom",
					},
				}
				setupReconciler(catalog)

				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				fetched := fetchCatalog()
				Expect(fetched.Status.ObservedGeneration).To(Equal(int64(1)))
				Expect(fetched.Status.ImageStatuses).To(HaveLen(3))
				for _, s := range fetched.Status.ImageStatuses {
					Expect(s.Valid).To(BeTrue())
				}

				condition := meta.FindStatusCondition(fetched.Status.Conditions, string(hwmgmtv1alpha1.Validation))
				Expect(condition).ToNot(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.Completed)))
			})
		})

		Context("when a catalog entry has an invalid URL", func() {
			It("should set Validation condition to False", func() {
				catalog.Spec.Images = []hwmgmtv1alpha1.FirmwareImage{
					{
						Name:      "dell-bios-bad",
						Component: "bios",
						URL:       "ftp://example.com/bios.exe",
						Version:   "2.3.5",
					},
				}
				setupReconciler(catalog)

				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				fetched := fetchCatalog()
				Expect(fetched.Status.ImageStatuses).To(HaveLen(1))
				Expect(fetched.Status.ImageStatuses[0].Valid).To(BeFalse())
				Expect(fetched.Status.ImageStatuses[0].Reason).To(Equal("InvalidURL"))

				condition := meta.FindStatusCondition(fetched.Status.Conditions, string(hwmgmtv1alpha1.Validation))
				Expect(condition).ToNot(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			})
		})

		Context("when a catalog entry has an invalid component", func() {
			It("should set Validation condition to False", func() {
				catalog.Spec.Images = []hwmgmtv1alpha1.FirmwareImage{
					{
						Name:      "bad-component",
						Component: "gpu",
						URL:       "https://example.com/gpu.bin",
						Version:   "1.0",
					},
				}
				setupReconciler(catalog)

				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				fetched := fetchCatalog()
				Expect(fetched.Status.ImageStatuses).To(HaveLen(1))
				Expect(fetched.Status.ImageStatuses[0].Valid).To(BeFalse())
				Expect(fetched.Status.ImageStatuses[0].Reason).To(Equal("InvalidComponent"))

				condition := meta.FindStatusCondition(fetched.Status.Conditions, string(hwmgmtv1alpha1.Validation))
				Expect(condition).ToNot(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			})
		})

		Context("when the catalog has a mix of valid and invalid entries", func() {
			It("should set Validation condition to False and report per-entry results", func() {
				catalog.Spec.Images = []hwmgmtv1alpha1.FirmwareImage{
					{
						Name:      "valid-bios",
						Component: "bios",
						URL:       "https://example.com/bios.exe",
						Version:   "2.3.5",
					},
					{
						Name:      "invalid-url",
						Component: "bmc",
						URL:       "not-a-url",
						Version:   "1.0",
					},
				}
				setupReconciler(catalog)

				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				fetched := fetchCatalog()
				Expect(fetched.Status.ImageStatuses).To(HaveLen(2))
				Expect(fetched.Status.ImageStatuses[0].Valid).To(BeTrue())
				Expect(fetched.Status.ImageStatuses[1].Valid).To(BeFalse())

				condition := meta.FindStatusCondition(fetched.Status.Conditions, string(hwmgmtv1alpha1.Validation))
				Expect(condition).ToNot(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			})
		})

		Context("when the catalog has an empty images list", func() {
			It("should set Validation condition to True", func() {
				setupReconciler(catalog)

				result, err := reconcile()
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				fetched := fetchCatalog()
				condition := meta.FindStatusCondition(fetched.Status.Conditions, string(hwmgmtv1alpha1.Validation))
				Expect(condition).ToNot(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			})
		})
	})
})
