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

// firmware holds a resolved firmware URL and version pair.
type firmware struct {
	Version string
	URL     string
}

func (f firmware) isEmpty() bool {
	return f.Version == "" && f.URL == ""
}

// resolvedFirmware holds the url/version pairs resolved from catalog entry names.
type resolvedFirmware struct {
	BiosFirmware firmware
	BmcFirmware  firmware
	NicFirmware  []firmware
}

// resolveFirmwareFromCatalog looks up firmware entry names from the HardwareProfile
// in the singleton FirmwareCatalog and returns resolved url/version pairs.
func resolveFirmwareFromCatalog(ctx context.Context, c client.Client,
	namespace string, spec hwmgmtv1alpha1.HardwareProfileSpec) (resolvedFirmware, error) {

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

	if spec.BiosFirmware != "" {
		img, ok := imageMap[spec.BiosFirmware]
		if !ok {
			return resolvedFirmware{},
				fmt.Errorf("biosFirmware entry %q not found in FirmwareCatalog", spec.BiosFirmware)
		}
		if img.Component != "bios" {
			return resolvedFirmware{},
				fmt.Errorf("biosFirmware entry %q has component %q, expected bios", spec.BiosFirmware, img.Component)
		}
		resolved.BiosFirmware = firmware{URL: img.URL, Version: img.Version}
	}

	if spec.BmcFirmware != "" {
		img, ok := imageMap[spec.BmcFirmware]
		if !ok {
			return resolvedFirmware{},
				fmt.Errorf("bmcFirmware entry %q not found in FirmwareCatalog", spec.BmcFirmware)
		}
		if img.Component != "bmc" {
			return resolvedFirmware{},
				fmt.Errorf("bmcFirmware entry %q has component %q, expected bmc", spec.BmcFirmware, img.Component)
		}
		resolved.BmcFirmware = firmware{URL: img.URL, Version: img.Version}
	}

	for _, name := range spec.NicFirmware {
		img, ok := imageMap[name]
		if !ok {
			return resolvedFirmware{},
				fmt.Errorf("nicFirmware entry %q not found in FirmwareCatalog", name)
		}
		if img.Component != "nic" {
			return resolvedFirmware{},
				fmt.Errorf("nicFirmware entry %q has component %q, expected nic", name, img.Component)
		}
		resolved.NicFirmware = append(resolved.NicFirmware, firmware{URL: img.URL, Version: img.Version})
	}

	return resolved, nil
}
