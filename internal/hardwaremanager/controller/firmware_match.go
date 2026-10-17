/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"strings"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

// firmwareMatchResult captures the outcome of comparing the firmware versions
// resolved from a HardwareProfile against the components reported in a
// HostFirmwareComponents (HFC) status.
type firmwareMatchResult struct {
	// Updates lists the firmware updates required to bring the HFC components to
	// the resolved versions. It is empty when every component already reports
	// the desired version.
	Updates []metal3v1alpha1.FirmwareUpdate
	// BiosMatched reports whether the resolved BIOS version is already present.
	// It is true when no BIOS version is resolved.
	BiosMatched bool
	// BmcMatched reports whether the resolved BMC version is already present.
	// It is true when no BMC version is resolved.
	BmcMatched bool
	// NicMatched reports whether every resolved NIC version is already present,
	// each on a distinct nic: component. It is true when no NIC version is
	// resolved.
	NicMatched bool
}

// UpdateRequired reports whether any firmware update is needed to converge.
func (r firmwareMatchResult) UpdateRequired() bool {
	return len(r.Updates) > 0
}

// matchFirmwareComponents is the single source of truth for deciding whether a
// set of resolved BIOS/BMC/NIC firmware versions match the components reported
// in a HostFirmwareComponents status. Both the update-detection path
// (isVersionChangeDetected, which decides what to flash) and the
// convergence-validation path (validateFirmwareVersions / validateNicFirmware,
// which decides whether the host has reached the target) use it, so the two
// can never disagree on the same match invariant.
//
// Version comparisons are normalized with normalizeVersion for BIOS, BMC, and
// NIC, so cosmetic differences such as a leading "v" or surrounding whitespace
// do not cause a spurious update on one path while the other reports the device
// already converged. NIC components are matched by version with each nic:
// component consumed at most once: two NIC requirements at the same version
// therefore need two components at that version to both be considered matched,
// rather than both matching the same component.
func matchFirmwareComponents(status *metal3v1alpha1.HostFirmwareComponentsStatus,
	resolved resolvedFirmware) firmwareMatchResult {

	result := firmwareMatchResult{
		BiosMatched: true,
		BmcMatched:  true,
		NicMatched:  true,
	}

	// matchSingle handles a single-instance component (BIOS or BMC), identified
	// by its HFC component name. When a version is resolved it records whether
	// the matching component already reports it and, if not, appends an update.
	matchSingle := func(componentName string, fw Firmware, matched *bool) {
		if strings.TrimSpace(fw.Version) == "" {
			return // No version resolved => nothing to match.
		}
		*matched = false
		want := normalizeVersion(fw.Version)
		for _, component := range status.Components {
			if strings.ToLower(strings.TrimSpace(component.Component)) != componentName {
				continue
			}
			if normalizeVersion(component.CurrentVersion) == want {
				*matched = true
			} else {
				result.Updates = append(result.Updates, metal3v1alpha1.FirmwareUpdate{
					Component: component.Component,
					URL:       fw.URL,
				})
			}
			break
		}
	}

	matchSingle(componentBIOS, resolved.BiosFirmware, &result.BiosMatched)
	matchSingle(componentBMC, resolved.BmcFirmware, &result.BmcMatched)

	// NIC firmware: first consume, for each resolved requirement, a distinct
	// nic: component that already reports the target version. Requirements that
	// cannot be matched then consume the remaining nic: components and are
	// scheduled for an update. Tracking consumed components across both passes
	// ensures a single component never satisfies more than one requirement.
	used := make(map[string]bool)
	matchedReq := make(map[int]bool, len(resolved.NicFirmware))
	for i, nic := range resolved.NicFirmware {
		if strings.TrimSpace(nic.Version) == "" {
			matchedReq[i] = true // No version => not a pending update.
			continue
		}
		want := normalizeVersion(nic.Version)
		for _, component := range status.Components {
			if !strings.HasPrefix(component.Component, componentNIC) || used[component.Component] {
				continue
			}
			if normalizeVersion(component.CurrentVersion) == want {
				used[component.Component] = true
				matchedReq[i] = true
				break
			}
		}
	}
	for i, nic := range resolved.NicFirmware {
		if matchedReq[i] {
			continue
		}
		result.NicMatched = false
		for _, component := range status.Components {
			if !strings.HasPrefix(component.Component, componentNIC) || used[component.Component] {
				continue
			}
			used[component.Component] = true
			result.Updates = append(result.Updates, metal3v1alpha1.FirmwareUpdate{
				Component: component.Component,
				URL:       nic.URL,
			})
			break
		}
	}

	return result
}
