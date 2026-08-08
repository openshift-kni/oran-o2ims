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

var _ = Describe("Firmware Resolution", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	createCatalog := func(ns string, images []hwmgmtv1alpha1.FirmwareImage) *hwmgmtv1alpha1.FirmwareCatalog {
		return &hwmgmtv1alpha1.FirmwareCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwmgmtv1alpha1.FirmwareCatalogName,
				Namespace: ns,
			},
			Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{
				Images: images,
			},
		}
	}

	Describe("resolveFirmwareFromCatalog", func() {
		It("should resolve all firmware types correctly", func() {
			catalog := createCatalog("test-ns", []hwmgmtv1alpha1.FirmwareImage{
				{Name: "bios-entry", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-entry", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
				{Name: "nic-entry", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
			})
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()

			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: "bios-entry",
				BmcFirmware:  "bmc-entry",
				NicFirmware:  []string{"nic-entry"},
			}

			resolved, err := resolveFirmwareFromCatalog(ctx, c, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.URL).To(Equal("https://example.com/bios.bin"))
			Expect(resolved.BiosFirmware.Version).To(Equal("1.0"))
			Expect(resolved.BmcFirmware.URL).To(Equal("https://example.com/bmc.bin"))
			Expect(resolved.BmcFirmware.Version).To(Equal("2.0"))
			Expect(resolved.NicFirmware).To(HaveLen(1))
			Expect(resolved.NicFirmware[0].URL).To(Equal("https://example.com/nic.bin"))
			Expect(resolved.NicFirmware[0].Version).To(Equal("3.0"))
		})

		It("should return empty resolved firmware for empty spec", func() {
			catalog := createCatalog("test-ns", nil)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()

			spec := hwmgmtv1alpha1.HardwareProfileSpec{}
			resolved, err := resolveFirmwareFromCatalog(ctx, c, "test-ns", spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.BiosFirmware.isEmpty()).To(BeTrue())
			Expect(resolved.BmcFirmware.isEmpty()).To(BeTrue())
			Expect(resolved.NicFirmware).To(BeNil())
		})

		It("should return error when entry not found in catalog", func() {
			catalog := createCatalog("test-ns", nil)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()

			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: "nonexistent",
			}
			_, err := resolveFirmwareFromCatalog(ctx, c, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found in FirmwareCatalog"))
		})

		It("should return error when component type mismatches", func() {
			catalog := createCatalog("test-ns", []hwmgmtv1alpha1.FirmwareImage{
				{Name: "bmc-entry", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
			})
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()

			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: "bmc-entry",
			}
			_, err := resolveFirmwareFromCatalog(ctx, c, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected bios"))
		})

		It("should return error when catalog does not exist", func() {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: "any-entry",
			}
			_, err := resolveFirmwareFromCatalog(ctx, c, "test-ns", spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get FirmwareCatalog"))
		})
	})

	Describe("firmware.isEmpty", func() {
		It("should return true for empty firmware", func() {
			f := firmware{}
			Expect(f.isEmpty()).To(BeTrue())
		})

		It("should return false when version is set", func() {
			f := firmware{Version: "1.0"}
			Expect(f.isEmpty()).To(BeFalse())
		})

		It("should return false when URL is set", func() {
			f := firmware{URL: "https://example.com/fw.bin"}
			Expect(f.isEmpty()).To(BeFalse())
		})
	})
})
