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

func TestHardwareProfileWebhookValidateCreate(t *testing.T) {
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
				{Name: "bios-entry", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-entry", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
				{Name: "nic-entry", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
			},
		},
	}

	tests := []struct {
		name    string
		hp      *HardwareProfile
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid BIOS reference",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BiosFirmware: "bios-entry"},
			},
		},
		{
			name: "valid all references",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware: "bios-entry",
					BmcFirmware:  "bmc-entry",
					NicFirmware:  []string{"nic-entry"},
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
			name: "nonexistent BIOS entry",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BiosFirmware: "missing-entry"},
			},
			wantErr: true,
			errMsg:  "not found in FirmwareCatalog",
		},
		{
			name: "wrong component type for BIOS",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BiosFirmware: "bmc-entry"},
			},
			wantErr: true,
			errMsg:  "expected bios",
		},
		{
			name: "wrong component type for BMC",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BmcFirmware: "bios-entry"},
			},
			wantErr: true,
			errMsg:  "expected bmc",
		},
		{
			name: "wrong component type for NIC",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{NicFirmware: []string{"bios-entry"}},
			},
			wantErr: true,
			errMsg:  "expected nic",
		},
		{
			name: "nonexistent NIC entry",
			hp: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{NicFirmware: []string{"missing-nic"}},
			},
			wantErr: true,
			errMsg:  "not found in FirmwareCatalog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(catalog.DeepCopy()).
				Build()

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

	catalog := &FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      FirmwareCatalogName,
			Namespace: "test-ns",
		},
		Spec: FirmwareCatalogSpec{
			Images: []FirmwareImage{
				{Name: "bios-entry", Component: "bios", URL: "https://example.com/bios.bin", Version: "1.0"},
				{Name: "bmc-entry", Component: "bmc", URL: "https://example.com/bmc.bin", Version: "2.0"},
				{Name: "nic-entry", Component: "nic", URL: "https://example.com/nic.bin", Version: "3.0"},
			},
		},
	}

	oldHP := &HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
		Spec:       HardwareProfileSpec{BiosFirmware: "bios-entry"},
	}

	tests := []struct {
		name    string
		newHP   *HardwareProfile
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid firmware references",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec: HardwareProfileSpec{
					BiosFirmware: "bios-entry",
					BmcFirmware:  "bmc-entry",
				},
			},
		},
		{
			name: "nonexistent BIOS entry",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BiosFirmware: "missing-entry"},
			},
			wantErr: true,
			errMsg:  "not found in FirmwareCatalog",
		},
		{
			name: "wrong component type for BMC",
			newHP: &HardwareProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns"},
				Spec:       HardwareProfileSpec{BmcFirmware: "bios-entry"},
			},
			wantErr: true,
			errMsg:  "expected bmc",
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
				WithObjects(catalog.DeepCopy()).
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
				BiosFirmware: "bios-entry-1",
				BmcFirmware:  "bmc-entry-1",
				NicFirmware:  []string{"nic-entry-1", "nic-entry-2"},
			},
		},
		{
			Spec: HardwareProfileSpec{
				BiosFirmware: "bios-entry-2",
			},
		},
	}

	tests := []struct {
		name      string
		entryName string
		want      bool
	}{
		{"referenced by biosFirmware", "bios-entry-1", true},
		{"referenced by bmcFirmware", "bmc-entry-1", true},
		{"referenced by nicFirmware", "nic-entry-1", true},
		{"referenced by second NIC", "nic-entry-2", true},
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
