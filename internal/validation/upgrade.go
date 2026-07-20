/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"time"

	"github.com/coreos/go-semver/semver"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

// ValidateCVUpgradeData validates the semantic business rules for
// clusterVersion upgrade parameters. It checks:
//   - desiredUpdate.version, if set, matches releaseVersion
//   - clusterUpgradeTimeout, if set, is a valid positive Go duration
//   - intermediateVersion, if set, is valid semver, same major, and
//     exactly one minor below releaseVersion
//
// upgradeData is the top-level upgrade config map (containing keys like
// "clusterVersion", "clusterUpgradeTimeout", "intermediateVersion").
// releaseVersion is the ClusterTemplate spec.release value.
// contextLabel identifies the caller context for error messages (e.g.
// "upgradeDefaults" or "upgradeParameters").
func ValidateCVUpgradeData(upgradeData map[string]any, releaseVersion, contextLabel string) error {
	if cvRaw, ok := upgradeData["clusterVersion"]; ok {
		cvMap, ok := cvRaw.(map[string]any)
		if !ok {
			return typederrors.NewInputError("%s %q value must be an object", contextLabel, "clusterVersion")
		}

		if desiredUpdate, ok := cvMap["desiredUpdate"].(map[string]any); ok {
			if version, ok := desiredUpdate["version"].(string); ok && version != "" {
				if version != releaseVersion {
					return typederrors.NewInputError(
						"the clusterVersion desiredUpdate.version (%s) does not match the ClusterTemplate spec.release (%s)",
						version, releaseVersion)
				}
			}
		}
	}

	if timeoutStr, ok := upgradeData["clusterUpgradeTimeout"].(string); ok {
		dur, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return typederrors.NewInputError(
				"invalid clusterUpgradeTimeout %q in %s: %s",
				timeoutStr, contextLabel, err.Error())
		}
		if dur <= 0 {
			return typederrors.NewInputError(
				"invalid clusterUpgradeTimeout %q in %s: must be a positive duration",
				timeoutStr, contextLabel)
		}
	}

	if intermediateVersionStr, ok := upgradeData["intermediateVersion"].(string); ok && intermediateVersionStr != "" {
		intermediateVer, err := semver.NewVersion(intermediateVersionStr)
		if err != nil {
			return typederrors.NewInputError(
				"invalid intermediateVersion %q in %s: %s",
				intermediateVersionStr, contextLabel, err.Error())
		}
		releaseVer, err := semver.NewVersion(releaseVersion)
		if err != nil {
			return typederrors.NewInputError(
				"cannot validate intermediateVersion: spec.release %q is not valid semver: %s",
				releaseVersion, err.Error())
		}
		if intermediateVer.Major != releaseVer.Major {
			return typederrors.NewInputError(
				"intermediateVersion major version (%d) must equal spec.release major version (%d)",
				intermediateVer.Major, releaseVer.Major)
		}
		if intermediateVer.Minor+1 != releaseVer.Minor {
			return typederrors.NewInputError(
				"intermediateVersion %s must be exactly one minor version below ClusterTemplate's release version %s",
				intermediateVer, releaseVer)
		}
	}

	return nil
}
