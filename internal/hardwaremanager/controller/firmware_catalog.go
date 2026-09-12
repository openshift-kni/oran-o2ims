/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// Firmware holds the url and version resolved from a catalog entry.
type Firmware struct {
	Version string
	URL     string
}

// IsEmpty returns true when both Version and URL are empty.
func (fm Firmware) IsEmpty() bool {
	return fm.Version == "" && fm.URL == ""
}

// Nic holds the url and version for a NIC firmware entry resolved from the catalog.
type Nic struct {
	Version string
	URL     string
}

// resolvedFirmware holds the url/version pairs resolved from catalog entry names.
type resolvedFirmware struct {
	BiosFirmware Firmware
	BmcFirmware  Firmware
	NicFirmware  []Nic
}

// resolveFirmwareFromCatalog resolves the firmware url/version pairs for a
// HardwareProfile using one of two mutually exclusive approaches:
//
//   - FirmwareImages (recommended): each name is looked up in the singleton
//     FirmwareCatalog and auto-classified by the entry's component type.
//   - The deprecated inline BiosFirmware/BmcFirmware/NicFirmware fields: their
//     URL/version values are used directly, without consulting the catalog.
//
// If neither approach is configured, an empty resolvedFirmware is returned.
func resolveFirmwareFromCatalog(ctx context.Context, c client.Client,
	namespace string, spec hwmgmtv1alpha1.HardwareProfileSpec) (resolvedFirmware, error) {

	if len(spec.FirmwareImages) > 0 {
		return resolveFirmwareImages(ctx, c, namespace, spec.FirmwareImages)
	}

	return resolveInlineFirmware(spec), nil
}

// resolveFirmwareImages looks up each firmwareImages entry in the singleton
// FirmwareCatalog and auto-classifies it by the catalog entry's component type.
func resolveFirmwareImages(ctx context.Context, c client.Client,
	namespace string, images []string) (resolvedFirmware, error) {

	catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
	if err := c.Get(ctx, types.NamespacedName{
		Name: hwmgmtv1alpha1.FirmwareCatalogName, Namespace: namespace,
	}, catalog); err != nil {
		return resolvedFirmware{}, fmt.Errorf("failed to get FirmwareCatalog: %w", err)
	}

	imageMap := make(map[string]hwmgmtv1alpha1.FirmwareImage, len(catalog.Spec.Images))
	for _, img := range catalog.Spec.Images {
		imageMap[img.Name] = img
	}

	var resolved resolvedFirmware
	for _, name := range images {
		img, ok := imageMap[name]
		if !ok {
			return resolvedFirmware{},
				fmt.Errorf("firmwareImages entry %q not found in FirmwareCatalog", name)
		}
		switch img.Component {
		case hwmgmtv1alpha1.ComponentBIOS:
			resolved.BiosFirmware = Firmware{URL: img.URL, Version: img.Version}
		case hwmgmtv1alpha1.ComponentBMC:
			resolved.BmcFirmware = Firmware{URL: img.URL, Version: img.Version}
		case hwmgmtv1alpha1.ComponentNIC:
			resolved.NicFirmware = append(resolved.NicFirmware, Nic{URL: img.URL, Version: img.Version})
		default:
			return resolvedFirmware{},
				fmt.Errorf("firmwareImages entry %q has unsupported component %q", name, img.Component)
		}
	}

	return resolved, nil
}

// resolveInlineFirmware builds the resolved firmware directly from the legacy
// inline BiosFirmware/BmcFirmware/NicFirmware fields, which already carry their
// own URL and version.
func resolveInlineFirmware(spec hwmgmtv1alpha1.HardwareProfileSpec) resolvedFirmware {
	var resolved resolvedFirmware
	resolved.BiosFirmware = Firmware{URL: spec.BiosFirmware.URL, Version: spec.BiosFirmware.Version}
	resolved.BmcFirmware = Firmware{URL: spec.BmcFirmware.URL, Version: spec.BmcFirmware.Version}
	for _, nic := range spec.NicFirmware {
		resolved.NicFirmware = append(resolved.NicFirmware, Nic{URL: nic.URL, Version: nic.Version})
	}
	return resolved
}
