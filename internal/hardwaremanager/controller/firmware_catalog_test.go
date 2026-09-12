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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("resolveFirmwareFromCatalog", func() {
	var (
		ctx     context.Context
		scheme  *runtime.Scheme
		catalog *hwmgmtv1alpha1.FirmwareCatalog
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

		catalog = &hwmgmtv1alpha1.FirmwareCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwmgmtv1alpha1.FirmwareCatalogName,
				Namespace: "test-ns",
			},
			Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{
				Images: []hwmgmtv1alpha1.FirmwareImage{
					{Name: "bios-v1", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0.0"},
					{Name: "bmc-v2", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0.0"},
					{Name: "nic-v3", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0.0"},
				},
			},
		}
	})

	Context("firmwareImages approach", func() {
		It("should resolve a single BIOS entry auto-classified by component", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog.DeepCopy()).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"bios-v1"}}

			resolved, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.Version).To(Equal("1.0.0"))
			Expect(resolved.BiosFirmware.URL).To(Equal("https://example.com/bios.bin"))
			Expect(resolved.BmcFirmware.IsEmpty()).To(BeTrue())
			Expect(resolved.NicFirmware).To(BeEmpty())
		})

		It("should resolve all firmware types auto-classified by component", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog.DeepCopy()).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				FirmwareImages: []string{"bios-v1", "bmc-v2", "nic-v3"},
			}

			resolved, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.Version).To(Equal("1.0.0"))
			Expect(resolved.BmcFirmware.Version).To(Equal("2.0.0"))
			Expect(resolved.NicFirmware).To(HaveLen(1))
			Expect(resolved.NicFirmware[0].Version).To(Equal("3.0.0"))
		})

		It("should resolve multiple NIC entries", func() {
			catalog.Spec.Images = append(catalog.Spec.Images,
				hwmgmtv1alpha1.FirmwareImage{Name: "nic-v4", Component: "nic", URL: "https://example.com/nic2.bin", Version: "4.0.0"})
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog.DeepCopy()).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"nic-v3", "nic-v4"}}

			resolved, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.NicFirmware).To(HaveLen(2))
			Expect(resolved.NicFirmware[0].Version).To(Equal("3.0.0"))
			Expect(resolved.NicFirmware[1].Version).To(Equal("4.0.0"))
		})

		It("should return error for missing catalog entry", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog.DeepCopy()).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"nonexistent"}}

			_, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found in FirmwareCatalog"))
		})

		It("should return error for unsupported component type", func() {
			catalog.Spec.Images = append(catalog.Spec.Images,
				hwmgmtv1alpha1.FirmwareImage{Name: "gpu-v1", Component: "gpu", URL: "https://example.com/gpu.bin", Version: "1.0.0"})
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog.DeepCopy()).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"gpu-v1"}}

			_, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported component"))
		})

		It("should return error when FirmwareCatalog does not exist", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"bios-v1"}}

			_, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get FirmwareCatalog"))
		})
	})

	Context("deprecated inline firmware approach", func() {
		It("should resolve inline BIOS/BMC/NIC firmware without consulting the catalog", func() {
			// No catalog objects registered: the inline path must not require one.
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{Version: "1.0.0", URL: "https://example.com/bios.bin"},
				BmcFirmware:  hwmgmtv1alpha1.Firmware{Version: "2.0.0", URL: "https://example.com/bmc.bin"},
				NicFirmware: []hwmgmtv1alpha1.Nic{
					{Version: "3.0.0", URL: "https://example.com/nic.bin"},
				},
			}

			resolved, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.Version).To(Equal("1.0.0"))
			Expect(resolved.BiosFirmware.URL).To(Equal("https://example.com/bios.bin"))
			Expect(resolved.BmcFirmware.Version).To(Equal("2.0.0"))
			Expect(resolved.NicFirmware).To(HaveLen(1))
			Expect(resolved.NicFirmware[0].Version).To(Equal("3.0.0"))
		})
	})

	Context("no firmware configured", func() {
		It("should return empty resolved firmware for empty spec without requiring a catalog", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			spec := hwmgmtv1alpha1.HardwareProfileSpec{}

			resolved, err := resolveFirmwareFromCatalog(ctx, fakeClient, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.IsEmpty()).To(BeTrue())
			Expect(resolved.BmcFirmware.IsEmpty()).To(BeTrue())
			Expect(resolved.NicFirmware).To(BeEmpty())
		})
	})
})
