/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("FirmwareCatalogValidator", func() {
	var (
		ctx        context.Context
		validator  *firmwareCatalogValidator
		fakeClient client.Client
		oldCatalog *hwmgmtv1alpha1.FirmwareCatalog
		newCatalog *hwmgmtv1alpha1.FirmwareCatalog
	)

	BeforeEach(func() {
		ctx = context.TODO()
		oldCatalog = &hwmgmtv1alpha1.FirmwareCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwmgmtv1alpha1.FirmwareCatalogName,
				Namespace: "oran-o2ims",
			},
			Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{
				Images: []hwmgmtv1alpha1.FirmwareImage{
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
						Vendor:    "Dell",
					},
				},
			},
		}
		newCatalog = oldCatalog.DeepCopy()
	})

	setupValidator := func(objs ...client.Object) {
		fakeClient = fake.NewClientBuilder().WithScheme(s).
			WithObjects(objs...).
			Build()
		validator = &firmwareCatalogValidator{
			Client: fakeClient,
			Reader: fakeClient,
		}
	}

	Describe("ValidateCreate", func() {
		It("should allow creation", func() {
			setupValidator()
			warnings, err := validator.ValidateCreate(ctx, oldCatalog)
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("ValidateUpdate", func() {
		Context("when no entries are removed", func() {
			It("should allow adding new entries", func() {
				setupValidator()
				newCatalog.Spec.Images = append(newCatalog.Spec.Images, hwmgmtv1alpha1.FirmwareImage{
					Name:      "broadcom-nic-25.2",
					Component: "nic",
					URL:       "https://example.com/nic.bin",
					Version:   "25.2",
				})

				warnings, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})

		Context("when an unreferenced entry is removed", func() {
			It("should allow the removal", func() {
				setupValidator()
				newCatalog.Spec.Images = []hwmgmtv1alpha1.FirmwareImage{oldCatalog.Spec.Images[0]}

				warnings, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})

		Context("when updating entry descriptions only", func() {
			It("should allow the update", func() {
				setupValidator()
				newCatalog.Spec.Images[0].Description = "Updated description"

				warnings, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})

		Context("when modifying an immutable field", func() {
			It("should reject a component change", func() {
				setupValidator()
				newCatalog.Spec.Images[0].Component = "bmc"

				_, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("component is immutable"))
			})

			It("should reject a url change", func() {
				setupValidator()
				newCatalog.Spec.Images[0].URL = "https://other.com/bios.exe"

				_, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("url is immutable"))
			})

			It("should reject a version change", func() {
				setupValidator()
				newCatalog.Spec.Images[0].Version = "9.9.9"

				_, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("version is immutable"))
			})

			It("should reject a vendor change", func() {
				setupValidator()
				newCatalog.Spec.Images[0].Vendor = "HP"

				_, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("vendor is immutable"))
			})

			It("should report multiple violations", func() {
				setupValidator()
				newCatalog.Spec.Images[0].Component = "nic"
				newCatalog.Spec.Images[0].Version = "9.9.9"

				_, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("component is immutable"))
				Expect(err.Error()).To(ContainSubstring("version is immutable"))
			})
		})

		Context("when all entries are removed and none are referenced", func() {
			It("should allow the removal", func() {
				setupValidator()
				newCatalog.Spec.Images = nil

				warnings, err := validator.ValidateUpdate(ctx, oldCatalog, newCatalog)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})
	})

	Describe("ValidateDelete", func() {
		It("should allow deletion when no HardwareProfiles reference entries", func() {
			setupValidator()
			warnings, err := validator.ValidateDelete(ctx, oldCatalog)
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should reject deletion when a HardwareProfile references a BIOS entry via firmwareImages", func() {
			hp := &hwmgmtv1alpha1.HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test-profile", Namespace: "oran-o2ims"},
				Spec:       hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"dell-bios-2.3.5"}},
			}
			setupValidator(hp)
			_, err := validator.ValidateDelete(ctx, oldCatalog)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot delete FirmwareCatalog"))
			Expect(err.Error()).To(ContainSubstring("dell-bios-2.3.5"))
		})

		It("should reject deletion when a HardwareProfile references a NIC entry via firmwareImages", func() {
			catalog := oldCatalog.DeepCopy()
			catalog.Spec.Images = append(catalog.Spec.Images, hwmgmtv1alpha1.FirmwareImage{
				Name:      "broadcom-nic-25.2",
				Component: "nic",
				URL:       "https://example.com/nic.bin",
				Version:   "25.2",
			})
			hp := &hwmgmtv1alpha1.HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test-profile", Namespace: "oran-o2ims"},
				Spec:       hwmgmtv1alpha1.HardwareProfileSpec{FirmwareImages: []string{"broadcom-nic-25.2"}},
			}
			setupValidator(hp)
			_, err := validator.ValidateDelete(ctx, catalog)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot delete FirmwareCatalog"))
			Expect(err.Error()).To(ContainSubstring("broadcom-nic-25.2"))
		})

		It("should allow deletion when HardwareProfiles exist but have no firmware references", func() {
			hp := &hwmgmtv1alpha1.HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test-profile", Namespace: "oran-o2ims"},
				Spec:       hwmgmtv1alpha1.HardwareProfileSpec{},
			}
			setupValidator(hp)
			warnings, err := validator.ValidateDelete(ctx, oldCatalog)
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("findRemovedEntries", func() {
		It("should return empty when no entries are removed", func() {
			removed := findRemovedEntries(oldCatalog.Spec.Images, oldCatalog.Spec.Images)
			Expect(removed).To(BeEmpty())
		})

		It("should detect removed entries", func() {
			removed := findRemovedEntries(oldCatalog.Spec.Images, []hwmgmtv1alpha1.FirmwareImage{oldCatalog.Spec.Images[0]})
			Expect(removed).To(ConsistOf("dell-bmc-7.10"))
		})

		It("should detect all removed entries when new list is empty", func() {
			removed := findRemovedEntries(oldCatalog.Spec.Images, nil)
			Expect(removed).To(ConsistOf("dell-bios-2.3.5", "dell-bmc-7.10"))
		})
	})
})

func TestBuildReferencedEntryNames(t *testing.T) {
	profiles := []hwmgmtv1alpha1.HardwareProfile{
		{
			Spec: hwmgmtv1alpha1.HardwareProfileSpec{
				FirmwareImages: []string{"bios-entry-1", "bmc-entry-1", "nic-entry-1", "nic-entry-2"},
			},
		},
		{
			Spec: hwmgmtv1alpha1.HardwareProfileSpec{
				FirmwareImages: []string{"bios-entry-2"},
			},
		},
		{
			// A profile using the deprecated inline fields creates no catalog
			// dependency because those fields carry their own URL/version.
			Spec: hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{Version: "9.9", URL: "https://example.com/inline.bin"},
			},
		},
	}

	referenced := buildReferencedEntryNames(profiles)

	tests := []struct {
		name      string
		entryName string
		want      bool
	}{
		{"referenced BIOS entry", "bios-entry-1", true},
		{"referenced BMC entry", "bmc-entry-1", true},
		{"referenced NIC entry", "nic-entry-1", true},
		{"referenced second NIC", "nic-entry-2", true},
		{"referenced by second profile", "bios-entry-2", true},
		{"not referenced", "missing-entry", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := referenced[tt.entryName]
			if got != tt.want {
				t.Errorf("buildReferencedEntryNames()[%q] present = %v, want %v", tt.entryName, got, tt.want)
			}
		})
	}
}
