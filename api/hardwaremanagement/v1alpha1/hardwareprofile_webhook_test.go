/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHardwareProfileValidatorValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	catalog := &FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      FirmwareCatalogName,
			Namespace: "test-ns",
		},
		Spec: FirmwareCatalogSpec{
			Images: []FirmwareImage{
				{Name: "bios-1", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-1", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
				{Name: "nic-1", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
			},
		},
	}

	t.Run("allows valid references", func(t *testing.T) {
		hp := &HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
			Spec: HardwareProfileSpec{
				BiosFirmware: "bios-1",
				BmcFirmware:  "bmc-1",
				NicFirmware:  []string{"nic-1"},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()
		v := &hardwareProfileValidator{Client: c}

		_, err := v.ValidateCreate(context.Background(), hp)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("allows empty firmware references", func(t *testing.T) {
		hp := &HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
			Spec:       HardwareProfileSpec{},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		v := &hardwareProfileValidator{Client: c}

		_, err := v.ValidateCreate(context.Background(), hp)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("rejects nonexistent entry", func(t *testing.T) {
		hp := &HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
			Spec: HardwareProfileSpec{
				BiosFirmware: "nonexistent",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()
		v := &hardwareProfileValidator{Client: c}

		_, err := v.ValidateCreate(context.Background(), hp)
		if err == nil {
			t.Error("expected error for nonexistent entry")
		}
	})

	t.Run("rejects component type mismatch", func(t *testing.T) {
		hp := &HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
			Spec: HardwareProfileSpec{
				BiosFirmware: "bmc-1",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(catalog).Build()
		v := &hardwareProfileValidator{Client: c}

		_, err := v.ValidateCreate(context.Background(), hp)
		if err == nil {
			t.Error("expected error for component mismatch")
		}
	})
}

func TestIsEntryReferencedByProfile(t *testing.T) {
	profile := &HardwareProfile{
		Spec: HardwareProfileSpec{
			BiosFirmware: "bios-entry",
			BmcFirmware:  "bmc-entry",
			NicFirmware:  []string{"nic-entry-1", "nic-entry-2"},
		},
	}

	tests := []struct {
		name     string
		entry    string
		expected bool
	}{
		{"finds bios reference", "bios-entry", true},
		{"finds bmc reference", "bmc-entry", true},
		{"finds nic reference", "nic-entry-1", true},
		{"finds second nic reference", "nic-entry-2", true},
		{"returns false for unreferenced", "other-entry", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEntryReferencedByProfile(tt.entry, profile)
			if got != tt.expected {
				t.Errorf("isEntryReferencedByProfile(%q) = %v, want %v", tt.entry, got, tt.expected)
			}
		})
	}
}
