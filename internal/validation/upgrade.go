/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"fmt"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
)

// WorkerPoolUpgrade is the worker MCP rollout configuration decoded from
// upgradeParameters or upgradeDefaults.
type WorkerPoolUpgrade struct {
	Strategy              string                   `json:"strategy,omitempty"`
	PoolsWithControlPlane []string                 `json:"poolsWithControlPlane,omitempty"`
	Stages                []WorkerPoolUpgradeStage `json:"stages,omitempty"`
	UpgradeThrough        string                   `json:"upgradeThrough,omitempty"`
}

// WorkerPoolUpgradeStage is one ordered wave in a Custom rollout.
type WorkerPoolUpgradeStage struct {
	Name  string   `json:"name"`
	Pools []string `json:"pools"`
}

// ValidateCVUpgradeData validates the semantic business rules for
// clusterVersion upgrade parameters. It checks:
//   - desiredUpdate.version, if set, matches releaseVersion
//   - clusterUpgradeTimeout, if set, is a valid positive Go duration
//   - intermediateVersion, if set, is valid semver, same major, and
//     exactly one minor below releaseVersion
//
// upgradeData is the top-level upgrade config map containing clusterVersion.
// releaseVersion is the ClusterTemplate spec.release value.
// contextLabel identifies the caller context for error messages (e.g.
// "upgradeDefaults" or "upgradeParameters").
func ValidateCVUpgradeData(upgradeData map[string]any, releaseVersion, contextLabel string) error {
	if cvRaw, ok := upgradeData["clusterVersion"]; ok {
		cvMap, ok := cvRaw.(map[string]any)
		if !ok {
			return typederrors.NewInputError("%s %q value must be an object", contextLabel, "clusterVersion")
		}
		cvSpecRaw, found := cvMap["cvSpec"]
		cvSpec, ok := cvSpecRaw.(map[string]any)
		if found && !ok {
			return typederrors.NewInputError("%s.clusterVersion.cvSpec must be an object", contextLabel)
		}
		if desiredUpdate, ok := cvSpec["desiredUpdate"].(map[string]any); ok {
			if version, ok := desiredUpdate["version"].(string); ok && version != "" {
				if version != releaseVersion {
					return typederrors.NewInputError(
						"the clusterVersion.cvSpec.desiredUpdate.version (%s) does not match the ClusterTemplate spec.release (%s)",
						version, releaseVersion)
				}
			}
		}
		if timeoutStr, ok := cvMap["clusterUpgradeTimeout"].(string); ok {
			dur, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return typederrors.NewInputError(
					"invalid clusterVersion.clusterUpgradeTimeout %q in %s: %s",
					timeoutStr, contextLabel, err.Error())
			}
			if dur <= 0 {
				return typederrors.NewInputError(
					"invalid clusterVersion.clusterUpgradeTimeout %q in %s: must be a positive duration",
					timeoutStr, contextLabel)
			}
		}
		if intermediateVersionStr, ok := cvMap["intermediateVersion"].(string); ok && intermediateVersionStr != "" {
			if err := ValidateEUSIntermediate(intermediateVersionStr, releaseVersion); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateEUSIntermediate checks that intermediateVersion is valid semver and
// exactly one minor version below targetVersion with the same major.
func ValidateEUSIntermediate(intermediateVersion, targetVersion string) error {
	intermediateVer, err := semver.NewVersion(intermediateVersion)
	if err != nil {
		return typederrors.NewInputError(
			"clusterVersion.intermediateVersion %q is not valid semver: %s",
			intermediateVersion, err.Error())
	}
	targetVer, err := semver.NewVersion(targetVersion)
	if err != nil {
		return typederrors.NewInputError(
			"cannot validate clusterVersion.intermediateVersion: ClusterTemplate's spec.release %q is not valid semver: %s",
			targetVersion, err.Error())
	}
	if intermediateVer.Major != targetVer.Major {
		return typederrors.NewInputError(
			"clusterVersion.intermediateVersion major version (%d) must equal ClusterTemplate's spec.release major version (%d)",
			intermediateVer.Major, targetVer.Major)
	}
	if intermediateVer.Minor+1 != targetVer.Minor {
		return typederrors.NewInputError(
			"clusterVersion.intermediateVersion %s must be exactly one minor version below ClusterTemplate's spec.release version %s",
			intermediateVer, targetVer)
	}
	return nil
}

// ValidateWorkerPoolUpgrade validates strategy-specific worker MCP rollout rules
// that do not require access to the spoke cluster.
func ValidateWorkerPoolUpgrade(isEUS bool, config WorkerPoolUpgrade) error {
	seenPools := make(map[string]string)
	for _, pool := range config.PoolsWithControlPlane {
		if pool == "" {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade.poolsWithControlPlane contains an empty pool name")
		}
		if pool == "master" {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade.poolsWithControlPlane must not include %q", pool)
		}
		if _, duplicate := seenPools[pool]; duplicate {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade.poolsWithControlPlane contains duplicate pool name %q", pool)
		}
		seenPools[pool] = "poolsWithControlPlane"
	}

	switch config.Strategy {
	case constants.WorkerPoolUpgradeStrategyOpenShiftDefault:
		if isEUS {
			return fmt.Errorf(
				"clusterVersion.workerPoolUpgrade.strategy %s is not applicable to EUS upgrades",
				constants.WorkerPoolUpgradeStrategyOpenShiftDefault)
		}
		if len(config.PoolsWithControlPlane) > 0 {
			return fmt.Errorf(
				"clusterVersion.workerPoolUpgrade.poolsWithControlPlane is not supported with strategy %s",
				constants.WorkerPoolUpgradeStrategyOpenShiftDefault)
		}
	case constants.WorkerPoolUpgradeStrategySerial, constants.WorkerPoolUpgradeStrategyParallel:
		if isEUS && len(config.PoolsWithControlPlane) > 0 {
			return fmt.Errorf(
				"clusterVersion.workerPoolUpgrade.poolsWithControlPlane is not supported for EUS upgrades")
		}
	case constants.WorkerPoolUpgradeStrategyCustom:
		if isEUS && len(config.PoolsWithControlPlane) > 0 {
			return fmt.Errorf(
				"clusterVersion.workerPoolUpgrade.poolsWithControlPlane is not supported for EUS upgrades")
		}
		if len(config.Stages) == 0 {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade.stages must not be empty with strategy Custom")
		}
		seenStages := make(map[string]struct{}, len(config.Stages))
		for _, stage := range config.Stages {
			if stage.Name == "" {
				return fmt.Errorf("clusterVersion.workerPoolUpgrade.stages contains an empty stage name")
			}
			if _, duplicate := seenStages[stage.Name]; duplicate {
				return fmt.Errorf("clusterVersion.workerPoolUpgrade.stages contains duplicate stage name %q", stage.Name)
			}
			seenStages[stage.Name] = struct{}{}
			if len(stage.Pools) == 0 {
				return fmt.Errorf("clusterVersion.workerPoolUpgrade stage %q must contain at least one pool", stage.Name)
			}
			for _, pool := range stage.Pools {
				if pool == "" {
					return fmt.Errorf("clusterVersion.workerPoolUpgrade stage %q contains an empty pool name", stage.Name)
				}
				if pool == "master" {
					return fmt.Errorf("clusterVersion.workerPoolUpgrade stage must not include %q", pool)
				}
				stageLabel := fmt.Sprintf("stage %q", stage.Name)
				if previous, duplicate := seenPools[pool]; duplicate {
					if previous == stageLabel {
						return fmt.Errorf("clusterVersion.workerPoolUpgrade pool %q appears more than once in %s", pool, stageLabel)
					}
					return fmt.Errorf("clusterVersion.workerPoolUpgrade pool %q appears more than once (%s and stage %q)",
						pool, previous, stage.Name)
				}
				seenPools[pool] = stageLabel
			}
		}
		if config.UpgradeThrough != "" {
			if _, found := seenStages[config.UpgradeThrough]; !found {
				return fmt.Errorf("clusterVersion.workerPoolUpgrade.upgradeThrough refers to unknown stage %q", config.UpgradeThrough)
			}
		}
	default:
		return fmt.Errorf(
			"unsupported clusterVersion.workerPoolUpgrade.strategy %q; must be %s, %s, %s, or %s",
			config.Strategy,
			constants.WorkerPoolUpgradeStrategyOpenShiftDefault,
			constants.WorkerPoolUpgradeStrategySerial,
			constants.WorkerPoolUpgradeStrategyParallel,
			constants.WorkerPoolUpgradeStrategyCustom)
	}

	if config.Strategy != constants.WorkerPoolUpgradeStrategyCustom &&
		(len(config.Stages) > 0 || config.UpgradeThrough != "") {
		return fmt.Errorf("clusterVersion.workerPoolUpgrade.stages and clusterVersion.workerPoolUpgrade.upgradeThrough are supported only with strategy Custom")
	}
	return nil
}

// ValidateWorkerPoolUpgradeMCPs checks configured pools against live worker MCPs.
// Call ValidateWorkerPoolUpgrade first for name, uniqueness, and strategy rules.
func ValidateWorkerPoolUpgradeMCPs(
	mcps []mcfgv1.MachineConfigPool, config WorkerPoolUpgrade,
) error {
	known := make(map[string]struct{}, len(mcps))
	for i := range mcps {
		known[mcps[i].Name] = struct{}{}
	}

	configured := make([]string, 0, len(config.PoolsWithControlPlane))
	configured = append(configured, config.PoolsWithControlPlane...)
	for _, stage := range config.Stages {
		configured = append(configured, stage.Pools...)
	}
	seen := make(map[string]struct{}, len(configured))
	for _, name := range configured {
		seen[name] = struct{}{}
		if _, ok := known[name]; !ok {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade refers to unknown MachineConfigPool %q", name)
		}
	}

	if config.Strategy == constants.WorkerPoolUpgradeStrategyCustom {
		var missing []string
		for i := range mcps {
			if _, ok := seen[mcps[i].Name]; !ok {
				missing = append(missing, mcps[i].Name)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade Custom plan does not include worker MachineConfigPools %v", missing)
		}
	}
	return nil
}
