/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("FirmwareCatalog Controller", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	Describe("validateCatalogImages", func() {
		It("should return empty statuses for empty images list", func() {
			statuses := validateCatalogImages(nil)
			Expect(statuses).To(BeEmpty())
		})

		It("should validate all images as valid with correct URLs", func() {
			images := []hwmgmtv1alpha1.FirmwareImage{
				{Name: "bios-1", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-1", Component: "bmc", URL: "http://example.com/bmc.bin", Version: "2.0"},
			}
			statuses := validateCatalogImages(images)
			Expect(statuses).To(HaveLen(2))
			Expect(statuses[0].Valid).To(BeTrue())
			Expect(statuses[1].Valid).To(BeTrue())
		})

		It("should mark image with invalid URL as invalid", func() {
			images := []hwmgmtv1alpha1.FirmwareImage{
				{Name: "bad-url", Component: "bios", URL: "not-a-url", Version: "1.0"},
			}
			statuses := validateCatalogImages(images)
			Expect(statuses).To(HaveLen(1))
			Expect(statuses[0].Valid).To(BeFalse())
			Expect(statuses[0].Reason).To(Equal("InvalidURL"))
		})

		It("should validate each image independently", func() {
			images := []hwmgmtv1alpha1.FirmwareImage{
				{Name: "good", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bad", Component: "bmc", URL: "not-a-url", Version: "2.0"},
				{Name: "also-good", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
			}
			statuses := validateCatalogImages(images)
			Expect(statuses).To(HaveLen(3))
			Expect(statuses[0].Valid).To(BeTrue())
			Expect(statuses[1].Valid).To(BeFalse())
			Expect(statuses[2].Valid).To(BeTrue())
		})
	})

	Describe("EnsureFirmwareCatalogSingleton", func() {
		It("should create the singleton when it does not exist", func() {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			err := EnsureFirmwareCatalogSingleton(ctx, c, "test-ns")
			Expect(err).ToNot(HaveOccurred())

			catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      hwmgmtv1alpha1.FirmwareCatalogName,
				Namespace: "test-ns",
			}, catalog)
			Expect(err).ToNot(HaveOccurred())
			Expect(catalog.Name).To(Equal(hwmgmtv1alpha1.FirmwareCatalogName))
			Expect(catalog.Spec.Images).To(BeNil())
		})

		It("should not overwrite an existing singleton", func() {
			existing := &hwmgmtv1alpha1.FirmwareCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwmgmtv1alpha1.FirmwareCatalogName,
					Namespace: "test-ns",
				},
				Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{
					Images: []hwmgmtv1alpha1.FirmwareImage{
						{Name: "user-entry", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
					},
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

			err := EnsureFirmwareCatalogSingleton(ctx, c, "test-ns")
			Expect(err).ToNot(HaveOccurred())

			catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      hwmgmtv1alpha1.FirmwareCatalogName,
				Namespace: "test-ns",
			}, catalog)
			Expect(err).ToNot(HaveOccurred())
			Expect(catalog.Spec.Images).To(HaveLen(1))
			Expect(catalog.Spec.Images[0].Name).To(Equal("user-entry"))
		})
	})
})
