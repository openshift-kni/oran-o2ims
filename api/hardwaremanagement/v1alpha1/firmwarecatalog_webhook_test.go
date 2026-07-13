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

func TestFindRemovedEntries(t *testing.T) {
	tests := []struct {
		name      string
		oldImages []FirmwareImage
		newImages []FirmwareImage
		expected  []string
	}{
		{
			name:      "no changes",
			oldImages: []FirmwareImage{{Name: "a"}, {Name: "b"}},
			newImages: []FirmwareImage{{Name: "a"}, {Name: "b"}},
			expected:  nil,
		},
		{
			name:      "entry removed",
			oldImages: []FirmwareImage{{Name: "a"}, {Name: "b"}},
			newImages: []FirmwareImage{{Name: "a"}},
			expected:  []string{"b"},
		},
		{
			name:      "all entries removed",
			oldImages: []FirmwareImage{{Name: "a"}, {Name: "b"}},
			newImages: nil,
			expected:  []string{"a", "b"},
		},
		{
			name:      "entry added",
			oldImages: []FirmwareImage{{Name: "a"}},
			newImages: []FirmwareImage{{Name: "a"}, {Name: "b"}},
			expected:  nil,
		},
		{
			name:      "empty old",
			oldImages: nil,
			newImages: []FirmwareImage{{Name: "a"}},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findRemovedEntries(tt.oldImages, tt.newImages)
			if len(got) != len(tt.expected) {
				t.Errorf("findRemovedEntries() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFirmwareCatalogValidatorValidateUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	oldCatalog := &FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "firmware-catalog",
			Namespace: "test-ns",
		},
		Spec: FirmwareCatalogSpec{
			Images: []FirmwareImage{
				{Name: "bios-1", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-1", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
			},
		},
	}

	t.Run("allows adding new entries", func(t *testing.T) {
		newCatalog := oldCatalog.DeepCopy()
		newCatalog.Spec.Images = append(newCatalog.Spec.Images, FirmwareImage{
			Name: "nic-1", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0",
		})

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		v := &firmwareCatalogValidator{Client: c}

		_, err := v.ValidateUpdate(context.Background(), oldCatalog, newCatalog)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("allows removing unreferenced entries", func(t *testing.T) {
		newCatalog := oldCatalog.DeepCopy()
		newCatalog.Spec.Images = newCatalog.Spec.Images[:1]

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		v := &firmwareCatalogValidator{Client: c}

		_, err := v.ValidateUpdate(context.Background(), oldCatalog, newCatalog)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("allows no changes", func(t *testing.T) {
		newCatalog := oldCatalog.DeepCopy()
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		v := &firmwareCatalogValidator{Client: c}

		_, err := v.ValidateUpdate(context.Background(), oldCatalog, newCatalog)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("blocks removing a referenced entry", func(t *testing.T) {
		newCatalog := oldCatalog.DeepCopy()
		newCatalog.Spec.Images = newCatalog.Spec.Images[1:]

		profile := &HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "hwprofile-1", Namespace: "test-ns"},
			Spec:       HardwareProfileSpec{BiosFirmware: "bios-1"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(profile).Build()
		v := &firmwareCatalogValidator{Client: c}

		_, err := v.ValidateUpdate(context.Background(), oldCatalog, newCatalog)
		if err == nil {
			t.Error("expected error when removing a referenced entry, got nil")
		}
	})
}
