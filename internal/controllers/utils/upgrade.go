/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coreos/go-semver/semver"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CV condition types not exported by the configv1 package.
const (
	CVConditionInvalid         = configv1.ClusterStatusConditionType("Invalid")
	CVConditionFailing         = configv1.ClusterStatusConditionType("Failing")
	CVConditionReleaseAccepted = configv1.ClusterStatusConditionType("ReleaseAccepted")
)

// upgradeConfig holds the parsed upgrade configuration extracted from
// ProvisioningRequest upgradeParameters and ClusterTemplate upgradeDefaults.
type UpgradeConfig struct {
	UpgradeType         string
	Timeout             time.Duration
	IntermediateVersion string
}

// UpgradePhase represents the current phase of a ClusterVersion upgrade.
type UpgradePhase int

const (
	PhasePreStart UpgradePhase = iota
	PhaseInProgress
	PhaseCompleted
)

// CVUpgradeAction holds the resolved upgrade state for a ClusterVersion upgrade.
type CVUpgradeAction struct {
	UpgradeToVersion  string
	Phase             UpgradePhase
	IsEUS             bool
	IsEUSIntermediate bool
}

// VersionLabel returns "intermediate" for EUS intermediate upgrades,
// "desired" otherwise. Used in condition messages.
func (a *CVUpgradeAction) VersionLabel() string {
	if a.IsEUSIntermediate {
		return "intermediate"
	}
	return "desired"
}

// TriggerCVUpgrade patches the spoke ClusterVersion's desiredUpdate if it
// differs from the desired state. Returns true if a patch was applied.
func TriggerCVUpgrade(ctx context.Context, spokeClient client.Client, logger *slog.Logger,
	cv *configv1.ClusterVersion, desiredUpdate *configv1.Update,
) (bool, error) {
	if cv.Spec.DesiredUpdate != nil && equality.Semantic.DeepEqual(*cv.Spec.DesiredUpdate, *desiredUpdate) {
		return false, nil
	}

	patch := client.MergeFrom(cv.DeepCopy())
	cv.Spec.DesiredUpdate = desiredUpdate
	if err := spokeClient.Patch(ctx, cv, patch); err != nil {
		return false, fmt.Errorf("failed to patch ClusterVersion desiredUpdate: %w", err)
	}
	logger.InfoContext(ctx, "Upgrade triggered on spoke",
		slog.String("targetVersion", desiredUpdate.Version))
	return true, nil
}

// PatchCVChannelUpstream patches channel and upstream on the spoke ClusterVersion
// if they differ from the desired values. Returns true if a patch was applied.
func PatchCVChannelUpstream(ctx context.Context, spokeClient client.Client, logger *slog.Logger,
	cv *configv1.ClusterVersion, cvSpec *configv1.ClusterVersionSpec,
) (bool, error) {
	changed := false
	patch := client.MergeFrom(cv.DeepCopy())
	// CVO sets a default channel in cv.spec.channel after installation. If
	// channel is not provided in the upgrade config, preserve the existing one.
	if cvSpec.Channel != "" && cv.Spec.Channel != cvSpec.Channel {
		cv.Spec.Channel = cvSpec.Channel
		changed = true
	}
	// Unlike channel, there is no default upstream in cv.spec.upstream (CVO
	// uses the default graph internally when upstream is empty). The upgrade
	// config is the source of truth — if a cluster has a custom upstream set
	// outside this controller, the upgrade config should explicitly include it.
	if cv.Spec.Upstream != cvSpec.Upstream {
		cv.Spec.Upstream = cvSpec.Upstream
		changed = true
	}

	if changed {
		if err := spokeClient.Patch(ctx, cv, patch); err != nil {
			return false, fmt.Errorf("failed to patch ClusterVersion channel/upstream: %w", err)
		}
		logger.InfoContext(ctx, "Patched channel/upstream on spoke ClusterVersion",
			slog.String("channel", cvSpec.Channel), slog.String("upstream", string(cvSpec.Upstream)))
	}
	return changed, nil
}

// FindCVHistoryEntry searches the ClusterVersion history for a specific version.
func FindCVHistoryEntry(cv *configv1.ClusterVersion, version string) *configv1.UpdateHistory {
	for i := range cv.Status.History {
		if cv.Status.History[i].Version == version {
			return &cv.Status.History[i]
		}
	}
	return nil
}

// IsCVUpdateAvailable checks if a version is in the ClusterVersion's available updates.
func IsCVUpdateAvailable(cv *configv1.ClusterVersion, version string) bool {
	for i := range cv.Status.AvailableUpdates {
		if cv.Status.AvailableUpdates[i].Version == version {
			return true
		}
	}
	return false
}

// GetCVCondition finds a condition by type in the ClusterVersion status.
// Returns nil if cv is nil or the condition is not found.
func GetCVCondition(cv *configv1.ClusterVersion, condType configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	if cv == nil {
		return nil
	}
	for i := range cv.Status.Conditions {
		if cv.Status.Conditions[i].Type == condType {
			return &cv.Status.Conditions[i]
		}
	}
	return nil
}

// GetCurrentCVVersion returns the current completed version from CV history.
func GetCurrentCVVersion(cv *configv1.ClusterVersion) string {
	for _, h := range cv.Status.History {
		if h.State == configv1.CompletedUpdate {
			return h.Version
		}
	}
	return ""
}

// IsMinorUpgrade returns true if the target version has a higher minor version
// than the current version. Returns an error if either version is not valid semver.
func IsMinorUpgrade(currentVersion, targetVersion string) (bool, error) {
	if currentVersion == "" || targetVersion == "" {
		return false, nil
	}

	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse current version %q: %w", currentVersion, err)
	}
	target, err := semver.NewVersion(targetVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse target version %q: %w", targetVersion, err)
	}
	return target.Minor > current.Minor, nil
}

// ResolveCVUpgradeAction determines the current phase and version from CV
// history. The caller is responsible for EUS detection via IsEUSUpgrade and
// passes the result as isEUS.
func ResolveCVUpgradeAction(
	cv *configv1.ClusterVersion,
	targetVersion, intermediateVersion string,
	isEUS bool,
) *CVUpgradeAction {
	var upgradeToVersion string
	var phase UpgradePhase

	if isEUS {
		intEntry := FindCVHistoryEntry(cv, intermediateVersion)
		switch {
		case intEntry == nil:
			upgradeToVersion, phase = intermediateVersion, PhasePreStart
		case intEntry.State == configv1.CompletedUpdate:
			tgtEntry := FindCVHistoryEntry(cv, targetVersion)
			switch {
			case tgtEntry == nil:
				upgradeToVersion, phase = targetVersion, PhasePreStart
			case tgtEntry.State == configv1.CompletedUpdate:
				upgradeToVersion, phase = targetVersion, PhaseCompleted
			default:
				upgradeToVersion, phase = targetVersion, PhaseInProgress
			}
		default:
			upgradeToVersion, phase = intermediateVersion, PhaseInProgress
		}
	} else {
		entry := FindCVHistoryEntry(cv, targetVersion)
		switch {
		case entry == nil:
			upgradeToVersion, phase = targetVersion, PhasePreStart
		case entry.State == configv1.CompletedUpdate:
			upgradeToVersion, phase = targetVersion, PhaseCompleted
		default:
			upgradeToVersion, phase = targetVersion, PhaseInProgress
		}
	}

	return &CVUpgradeAction{
		UpgradeToVersion:  upgradeToVersion,
		Phase:             phase,
		IsEUS:             isEUS,
		IsEUSIntermediate: isEUS && upgradeToVersion == intermediateVersion,
	}
}

// IsEUSUpgrade determines whether the upgrade is EUS-to-EUS based on the
// start and target versions (both even minor, exactly 2 apart). When EUS is
// detected, it validates that intermediateVersion is provided and exactly one
// minor version below the target. Returns an error if intermediateVersion is
// provided for a non-EUS upgrade, or if the version chain is invalid.
// Returns false with no error for empty start/target versions.
func IsEUSUpgrade(startVersion, intermediateVersion, targetVersion string) (bool, error) {
	if startVersion == "" || targetVersion == "" {
		return false, nil
	}

	start, err := semver.NewVersion(startVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse start version %q: %w", startVersion, err)
	}
	target, err := semver.NewVersion(targetVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse target version %q: %w", targetVersion, err)
	}

	isEUS := start.Major == target.Major &&
		start.Minor%2 == 0 && target.Minor%2 == 0 &&
		target.Minor-start.Minor == 2

	if !isEUS {
		if intermediateVersion != "" {
			return false, fmt.Errorf(
				"intermediateVersion %s provided but upgrade from %s to %s is not EUS-to-EUS",
				intermediateVersion, startVersion, targetVersion)
		}
		return false, nil
	}

	if intermediateVersion == "" {
		return false, fmt.Errorf(
			"intermediateVersion is required for EUS-to-EUS upgrades (current: %s, target: %s)",
			startVersion, targetVersion)
	}

	intermediate, err := semver.NewVersion(intermediateVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse intermediateVersion %q: %w", intermediateVersion, err)
	}
	if intermediate.Major != target.Major {
		return false, fmt.Errorf(
			"intermediateVersion %s major version must match targetVersion %s",
			intermediateVersion, targetVersion)
	}
	if target.Minor-intermediate.Minor != 1 {
		return false, fmt.Errorf(
			"intermediateVersion %s must be exactly one minor version below targetVersion %s",
			intermediateVersion, targetVersion)
	}

	return true, nil
}

// ListMCPs returns all MachineConfigPools.
func ListMCPs(ctx context.Context, spokeClient client.Client) ([]mcfgv1.MachineConfigPool, error) {
	mcpList := &mcfgv1.MachineConfigPoolList{}
	if err := spokeClient.List(ctx, mcpList); err != nil {
		return nil, fmt.Errorf("failed to list MachineConfigPools: %w", err)
	}
	return mcpList.Items, nil
}

// ListNonMasterMCPs returns all MachineConfigPools except the master pool.
func ListNonMasterMCPs(ctx context.Context, spokeClient client.Client) ([]mcfgv1.MachineConfigPool, error) {
	mcps, err := ListMCPs(ctx, spokeClient)
	if err != nil {
		return nil, err
	}
	var nonMaster []mcfgv1.MachineConfigPool
	for i := range mcps {
		if mcps[i].Name != "master" {
			nonMaster = append(nonMaster, mcps[i])
		}
	}
	return nonMaster, nil
}

// PauseMCPs patches each MCP with spec.paused: true.
func PauseMCPs(ctx context.Context, spokeClient client.Client, logger *slog.Logger,
	mcps []mcfgv1.MachineConfigPool,
) error {
	for i := range mcps {
		if mcps[i].Spec.Paused {
			continue
		}
		patch := client.MergeFrom(mcps[i].DeepCopy())
		mcps[i].Spec.Paused = true
		if err := spokeClient.Patch(ctx, &mcps[i], patch); err != nil {
			return fmt.Errorf("failed to pause MachineConfigPool %s: %w", mcps[i].Name, err)
		}
		logger.InfoContext(ctx, "Paused MachineConfigPool",
			slog.String("mcpName", mcps[i].Name))
	}
	return nil
}

// UnpauseMCPs unpauses any paused MCPs in the given list.
// Returns true if any MCPs were unpaused.
func UnpauseMCPs(ctx context.Context, spokeClient client.Client, logger *slog.Logger,
	mcps []mcfgv1.MachineConfigPool,
) (bool, error) {
	unpaused := false
	for i := range mcps {
		if !mcps[i].Spec.Paused {
			continue
		}
		patch := client.MergeFrom(mcps[i].DeepCopy())
		mcps[i].Spec.Paused = false
		if err := spokeClient.Patch(ctx, &mcps[i], patch); err != nil {
			return false, fmt.Errorf("failed to unpause MachineConfigPool %s: %w", mcps[i].Name, err)
		}
		logger.InfoContext(ctx, "Unpaused MachineConfigPool",
			slog.String("mcpName", mcps[i].Name))
		unpaused = true
	}
	return unpaused, nil
}

// GetPausedMCPs returns the names of MCPs that have spec.paused set to true.
func GetPausedMCPs(mcps []mcfgv1.MachineConfigPool) []string {
	var names []string
	for i := range mcps {
		if mcps[i].Spec.Paused {
			names = append(names, mcps[i].Name)
		}
	}
	return names
}

// GetNonUpdatedMCPs returns the names of MCPs that do not have Updated=True.
func GetNonUpdatedMCPs(mcps []mcfgv1.MachineConfigPool) []string {
	var names []string
	for i := range mcps {
		updated := false
		for _, c := range mcps[i].Status.Conditions {
			if c.Type == mcfgv1.MachineConfigPoolUpdated &&
				c.Status == corev1.ConditionTrue {
				updated = true
				break
			}
		}
		if !updated {
			names = append(names, mcps[i].Name)
		}
	}
	return names
}
