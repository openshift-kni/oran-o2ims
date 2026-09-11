/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testCatalog returns a FirmwareCatalog with one bios, one bmc, one nic entry,
// plus a second bios entry and an unsupported-component entry used by the
// negative test cases.
func testCatalog() *FirmwareCatalog {
	return &FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      FirmwareCatalogName,
			Namespace: "test-ns",
		},
		Spec: FirmwareCatalogSpec{
			Images: []FirmwareImage{
				{Name: "bios-entry", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bios-entry-2", Component: "bios", URL: "https://example.com/bios2.bin", Version: "1.1"},
				{Name: "bmc-entry", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
				{Name: "nic-entry", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
				{Name: "gpu-entry", Component: "gpu", URL: "https://example.com/gpu.bin", Version: "4.0"},
			},
		},
	}
}

func TestHardwareProfileWebhookValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	tests := []struct {
		name      string
		hp        *HardwareProfile
		noCatalog bool
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid firmwareImages with single BIOS entry",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"bios-entry"}},
			},
		},
		{
			name: "valid firmwareImages with bios, bmc and nic",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					FirmwareImages: []string{"bios-entry", "bmc-entry", "nic-entry"},
				},
			},
		},
		{
			name: "empty firmware fields allowed",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{},
			},
		},
		{
			name: "deprecated inline fields require no catalog",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware: Firmware{Version: "1.0", URL: "https://example.com/bios.bin"},
					BmcFirmware:  Firmware{Version: "2.0", URL: "https://example.com/bmc.bin"},
					NicFirmware:  []Nic{{Version: "3.0", URL: "https://example.com/nic.bin"}},
				},
			},
			noCatalog: true,
		},
		{
			name: "mutually exclusive approaches rejected",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware:   Firmware{Version: "1.0", URL: "https://example.com/bios.bin"},
					FirmwareImages: []string{"bios-entry"},
				},
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "nonexistent firmwareImages entry",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"missing-entry"}},
			},
			wantErr: true,
			errMsg:  "not found in FirmwareCatalog",
		},
		{
			name: "more than one BIOS entry rejected",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"bios-entry", "bios-entry-2"}},
			},
			wantErr: true,
			errMsg:  "at most one",
		},
		{
			name: "unsupported component rejected",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"gpu-entry"}},
			},
			wantErr: true,
			errMsg:  "unsupported component",
		},
		{
			name: "duplicate firmwareImages entry rejected",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"nic-entry", "nic-entry"}},
			},
			wantErr: true,
			errMsg:  `firmwareImages contains duplicate entry "nic-entry"`,
		},
		{
			name: "missing FirmwareCatalog",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"bios-entry"}},
			},
			noCatalog: true,
			wantErr:   true,
			errMsg:    "failed to get FirmwareCatalog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if !tt.noCatalog {
				builder = builder.WithObjects(testCatalog())
			}
			fakeClient := builder.Build()

			v := &hardwareProfileValidator{Client: fakeClient}
			_, err := v.ValidateCreate(context.Background(), tt.hp)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestHardwareProfileWebhookValidateUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	oldHP := &HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
		Spec:       HardwareProfileSpec{FirmwareImages: []string{"bios-entry"}},
	}

	tests := []struct {
		name    string
		newHP   *HardwareProfile
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid firmwareImages references",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					FirmwareImages: []string{"bios-entry", "bmc-entry"},
				},
			},
		},
		{
			name: "nonexistent firmwareImages entry",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{FirmwareImages: []string{"missing-entry"}},
			},
			wantErr: true,
			errMsg:  "not found in FirmwareCatalog",
		},
		{
			name: "switch to deprecated inline fields",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware: Firmware{Version: "1.0", URL: "https://example.com/bios.bin"},
				},
			},
		},
		{
			name: "mutually exclusive approaches rejected",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware:   Firmware{Version: "1.0", URL: "https://example.com/bios.bin"},
					FirmwareImages: []string{"bios-entry"},
				},
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "removing all firmware references",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testCatalog()).
				Build()

			v := &hardwareProfileValidator{Client: fakeClient}
			_, err := v.ValidateUpdate(context.Background(), oldHP, tt.newHP)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsEntryReferencedByAnyProfile(t *testing.T) {
	profiles := []HardwareProfile{
		{
			Spec: HardwareProfileSpec{
				FirmwareImages: []string{"bios-entry-1", "bmc-entry-1", "nic-entry-1", "nic-entry-2"},
			},
		},
		{
			Spec: HardwareProfileSpec{
				FirmwareImages: []string{"bios-entry-2"},
			},
		},
		{
			// A profile using the deprecated inline fields creates no catalog
			// dependency because those fields carry their own URL/version.
			Spec: HardwareProfileSpec{
				BiosFirmware: Firmware{Version: "9.9", URL: "https://example.com/inline.bin"},
			},
		},
	}

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
			got := isEntryReferencedByAnyProfile(tt.entryName, profiles)
			if got != tt.want {
				t.Errorf("isEntryReferencedByAnyProfile(%q) = %v, want %v", tt.entryName, got, tt.want)
			}
		})
	}
}
