/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils/cincinnati"
	"github.com/openshift-kni/oran-o2ims/internal/spokeclient"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	"github.com/openshift-kni/oran-o2ims/internal/upgrade"
	upgradevalidation "github.com/openshift-kni/oran-o2ims/internal/validation"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsUpgradeRequested determines if a cluster upgrade is requested by comparing whether the ClusterTemplate release version
// is higher than the ManagedCluster's OpenShift release version.
// Returns:
//   - bool: true if an upgrade is requested (template version > managed cluster version), false otherwise
//   - ctrl.Result: requeue result with 30s delay if openshiftVersion label is not yet available, empty otherwise
//   - error: any error encountered during processing (ClusterTemplate fetch, ManagedCluster fetch, or version parsing)
func (t *provisioningRequestReconcilerTask) IsUpgradeRequested(
	ctx context.Context, managedClusterName string,
) (bool, ctrl.Result, error) {
	template, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return false, ctrl.Result{}, fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}

	if template.Spec.Release == "" {
		return false, ctrl.Result{}, nil
	}

	// Parse template version first to fail fast on invalid versions
	templateReleaseVersion, err := semver.NewVersion(template.Spec.Release)
	if err != nil {
		return false, ctrl.Result{}, fmt.Errorf("failed to parse ClusterTemplate release version %s: %w", template.Spec.Release, err)
	}

	managedCluster := &clusterv1.ManagedCluster{}
	if err := t.client.Get(ctx, types.NamespacedName{Name: managedClusterName}, managedCluster); err != nil {
		return false, ctrl.Result{}, fmt.Errorf("failed to get ManagedCluster: %w", err)
	}

	openshiftVersion, ok := managedCluster.GetLabels()["openshiftVersion"]
	if !ok {
		t.logger.InfoContext(ctx, "openshiftVersion label not found in ManagedCluster, requeueing",
			slog.String("managedCluster", managedClusterName))
		return false, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	managedClusterVersion, err := semver.NewVersion(openshiftVersion)
	if err != nil {
		return false, ctrl.Result{}, fmt.Errorf("failed to parse ManagedCluster version %q: %w", openshiftVersion, err)
	}

	cmp := templateReleaseVersion.Compare(*managedClusterVersion)
	switch cmp {
	case 1:
		t.logger.InfoContext(ctx, "Upgrade requested: template version is higher than ManagedCluster version",
			slog.String("templateVersion", templateReleaseVersion.String()), slog.String("managedClusterVersion", managedClusterVersion.String()))
		return true, ctrl.Result{}, nil
	case -1:
		t.logger.InfoContext(ctx, "Template version is lower than ManagedCluster version, no upgrade requested",
			slog.String("templateVersion", templateReleaseVersion.String()), slog.String("managedClusterVersion", managedClusterVersion.String()))
	case 0:
		t.logger.InfoContext(ctx, "Template version equals ManagedCluster version, no upgrade requested",
			slog.String("version", templateReleaseVersion.String()))
	}
	return false, ctrl.Result{}, nil
}

// handleUpgrade dispatches to the appropriate upgrade handler based on the
// upgrade type detected from the ClusterTemplate defaults and ProvisioningRequest
// parameters. Returns a ctrl.Result, a bool indicating whether to proceed with
// further processing, and an error.
func (t *provisioningRequestReconcilerTask) handleUpgrade(ctx context.Context, clusterName string) (ctrl.Result, bool, error) {
	t.logger.InfoContext(
		ctx,
		"Start handling upgrade",
	)
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to get clusterTemplate: %w", err)
	}

	upgradeCfg, err := parseUpgradeConfig(clusterTemplate, t.object)
	if err != nil {
		ctlrutils.SetProvisioningStateFailed(t.object, fmt.Sprintf("Upgrade precondition check failed: %s", err.Error()))
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
			metav1.ConditionFalse,
			err.Error(),
		)
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", updateErr)
		}
		return ctrl.Result{}, false, nil
	}
	switch upgradeCfg.UpgradeType {
	case ctlrutils.UpgradeDefaultsClusterVersionKey:
		return t.handleClusterVersionUpgrade(ctx, clusterTemplate, clusterName, upgradeCfg)
	case ctlrutils.UpgradeDefaultsIBGUKey:
		return t.handleIBGUUpgrade(ctx, clusterTemplate, clusterName)
	default:
		t.logger.ErrorContext(ctx, "Unexpected upgrade type from parseUpgradeConfig",
			slog.String("upgradeType", upgradeCfg.UpgradeType))
		return doNotRequeue(), false, nil
	}
}

// parseUpgradeConfig inspects the ProvisioningRequest's upgradeParameters and
// the ClusterTemplate's upgradeDefaults to determine the upgrade type, extract
// the operation-specific timeout (PR overrides CT). Returns an error if
// multiple operation types are found, or if the timeout is invalid.
func parseUpgradeConfig(
	ct *provisioningv1alpha1.ClusterTemplate,
	pr *provisioningv1alpha1.ProvisioningRequest,
) (*ctlrutils.UpgradeConfig, error) {
	hasCV, hasIBGU, hasSeed := false, false, false
	var upgradeParams, defaults map[string]any

	// Check ProvisioningRequest upgradeParameters first (takes precedence).
	if pr.Spec.TemplateParameters.Size() > 0 {
		var templateParams map[string]any
		if err := json.Unmarshal(pr.Spec.TemplateParameters.Raw, &templateParams); err != nil {
			return nil, fmt.Errorf("failed to parse templateParameters: %w", err)
		}
		if upgradeParamsRaw, ok := templateParams[constants.TemplateParamUpgrade]; ok {
			upgradeParams, ok = upgradeParamsRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s is not a map", constants.TemplateParamUpgrade)
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
				hasCV = true
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
				hasIBGU = true
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsSeedGenerationKey]; ok {
				hasSeed = true
			}
		}
	}

	// Check ClusterTemplate upgradeDefaults.
	if ct.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0 {
		if err := json.Unmarshal(ct.Spec.TemplateDefaults.UpgradeDefaults.Raw, &defaults); err != nil {
			return nil, fmt.Errorf("failed to parse upgradeDefaults: %w", err)
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
			hasCV = true
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
			hasIBGU = true
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsSeedGenerationKey]; ok {
			hasSeed = true
		}
	}

	if hasCV && hasIBGU {
		return nil, fmt.Errorf(
			"upgrade configuration contains both %q and %q keys; only one upgrade type is allowed",
			ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey)
	}
	if hasSeed && (hasCV || hasIBGU) {
		return nil, fmt.Errorf(
			"upgrade configuration contains %q and another operation key; only one upgrade type is allowed",
			ctlrutils.UpgradeDefaultsSeedGenerationKey)
	}

	var operation string
	switch {
	case hasCV:
		operation = ctlrutils.UpgradeDefaultsClusterVersionKey
	case hasIBGU:
		operation = ctlrutils.UpgradeDefaultsIBGUKey
	case hasSeed:
		operation = ctlrutils.UpgradeDefaultsSeedGenerationKey
	default:
		return nil, fmt.Errorf(
			"no upgrade configuration found: upgradeDefaults or upgradeParameters must contain %q, %q, or %q",
			ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey,
			ctlrutils.UpgradeDefaultsSeedGenerationKey)
	}
	timeout, err := parseOperationTimeout(operation, upgradeParams, defaults)
	if err != nil {
		return nil, err
	}
	return &ctlrutils.UpgradeConfig{UpgradeType: operation, Timeout: timeout}, nil
}

func parseOperationTimeout(operation string, upgradeParams, defaults map[string]any) (time.Duration, error) {
	seedOperation := operation == ctlrutils.UpgradeDefaultsSeedGenerationKey
	key := ctlrutils.ClusterUpgradeTimeoutConfigKey
	if seedOperation {
		key = ctlrutils.SeedGenerationTimeoutConfigKey
	}
	var rawTimeout any
	var found bool
	for _, source := range []struct {
		name   string
		values map[string]any
	}{
		{"upgradeParameters", upgradeParams},
		{"upgradeDefaults", defaults},
	} {
		if seedOperation {
			if _, ok := source.values[ctlrutils.ClusterUpgradeTimeoutConfigKey]; ok {
				return 0, fmt.Errorf("%s is not valid for %s in %s",
					ctlrutils.ClusterUpgradeTimeoutConfigKey, operation, source.name)
			}
			if _, ok := source.values[ctlrutils.SeedGenerationTimeoutConfigKey]; ok {
				return 0, fmt.Errorf("%s must be nested under %s in %s",
					ctlrutils.SeedGenerationTimeoutConfigKey, ctlrutils.UpgradeDefaultsSeedGenerationKey, source.name)
			}
			seedRaw, ok := source.values[ctlrutils.UpgradeDefaultsSeedGenerationKey]
			if !ok {
				continue
			}
			seed, ok := seedRaw.(map[string]any)
			if !ok {
				return 0, fmt.Errorf("%s in %s must be an object",
					ctlrutils.UpgradeDefaultsSeedGenerationKey, source.name)
			}
			rawTimeout, found = seed[key]
		} else {
			if _, ok := source.values[ctlrutils.SeedGenerationTimeoutConfigKey]; ok {
				return 0, fmt.Errorf("%s is not valid for %s in %s",
					ctlrutils.SeedGenerationTimeoutConfigKey, operation, source.name)
			}
			rawTimeout, found = source.values[key]
		}
		if found {
			timeout, err := parseOperationTimeoutValue(key, source.name, rawTimeout)
			if err != nil {
				return 0, err
			}
			return timeout, nil
		}
	}
	return 0, nil
}

func parseOperationTimeoutValue(key, source string, raw any) (time.Duration, error) {
	value, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("%s in %s must be a duration string", key, source)
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("invalid %s %q in %s: must be a positive duration", key, value, source)
	}
	return timeout, nil
}

// handleIBGUUpgrade handles the upgrade of the cluster through IBGU.
// It checks if an IBGU CR already exists (monitoring mode) or creates one
// (merge+validate mode). Returns a ctrl.Result, a bool indicating whether
// to proceed with further processing, and an error.
func (t *provisioningRequestReconcilerTask) handleIBGUUpgrade(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	clusterName string,
) (ctrl.Result, bool, error) {
	nextReconcile := ctrl.Result{}
	proceed := false

	ibgu := &ibgu.ImageBasedGroupUpgrade{}
	err := t.client.Get(ctx, types.NamespacedName{Name: t.object.Name, Namespace: clusterName}, ibgu)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nextReconcile, proceed, fmt.Errorf("failed to get IBGU: %w", err)
		}

		// Merge, validate, and build the IBGU
		ibgu, err = t.prepareIBGU(ctx, clusterTemplate, clusterName)
		if err != nil {
			if typederrors.IsInputError(err) {
				ctlrutils.LogError(ctx, t.logger, "Upgrade precondition check failed", err)
				ctlrutils.SetProvisioningStateFailed(t.object, fmt.Sprintf("Upgrade precondition check failed: %s", err.Error()))
				ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
					provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
					provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
					metav1.ConditionFalse,
					err.Error(),
				)
				if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
					return nextReconcile, proceed, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", updateErr)
				}
				return nextReconcile, proceed, nil
			}
			return nextReconcile, proceed, fmt.Errorf("failed to prepare IBGU for cluster: %w", err)
		}

		// Create the IBGU
		if err := ctlrutils.CreateK8sCR(ctx, t.logger, t.client, ibgu, t.object, ctlrutils.UPDATE); err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to create IBGU: %w", err)
		}

		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Upgrade initiated. Created IBGU %s in the namespace %s",
				ibgu.GetName(),
				ibgu.GetNamespace(),
			),
		)

		ctlrutils.SetProvisioningStateInProgress(t.object, "Cluster upgrade is initiated")
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is initiated",
		)
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
		}
	}

	if isIBGUProgressing(ibgu) {
		t.logger.InfoContext(
			ctx,
			"Wait for upgrade to be completed",
		)

		ctlrutils.SetProvisioningStateInProgress(t.object, "Cluster upgrade is in progress")
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is in progress",
		)
		nextReconcile = requeueWithMediumInterval()
	} else if failed, message := isIBGUFailed(ibgu); failed {
		ctlrutils.SetProvisioningStateFailed(t.object, "Cluster upgrade is failed")
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			message,
		)
	} else {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Upgrade is completed",
		)
		err := t.client.Delete(ctx, ibgu)
		if err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to cleanup IBGU: %w", err)
		}
		// Proceed to further processing only when IBGU is completed
		proceed = true
	}

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return nextReconcile, proceed, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
	}

	return nextReconcile, proceed, nil
}

func isIBGUFailed(cr *ibgu.ImageBasedGroupUpgrade) (bool, string) {
	for _, cluster := range cr.Status.Clusters {
		if len(cluster.FailedActions) == 0 {
			continue
		}
		message := "Upgrade Failed: "
		for _, action := range cluster.FailedActions {
			message += fmt.Sprintf("Action %s failed: %s\n", action.Action, action.Message)
		}
		return true, message
	}
	return false, ""
}

// prepareIBGU merges upgrade data, performs IBGU-specific validation, and returns the IBGU CR.
func (t *provisioningRequestReconcilerTask) prepareIBGU(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	clusterName string,
) (*ibgu.ImageBasedGroupUpgrade, error) {

	// Merge and validate upgrade data against the schema
	mergedUpgradeData, err := t.mergeAndValidateUpgradeData(clusterTemplate)
	if err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
	}

	// Extract the imageBasedGroupUpgrade data from the merged result
	ibguRaw, ok := mergedUpgradeData[ctlrutils.UpgradeDefaultsIBGUKey]
	if !ok {
		return nil, typederrors.NewInputError("key %q not found in merged upgrade data", ctlrutils.UpgradeDefaultsIBGUKey)
	}
	ibguBytes, err := json.Marshal(ibguRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s data: %w", ctlrutils.UpgradeDefaultsIBGUKey, err)
	}

	// Build the IBGU from the extracted spec
	ibguCR, err := ctlrutils.GetIBGUFromUpgradeData(ibguBytes, clusterName, t.object.Name, clusterName)
	if err != nil {
		return nil, typederrors.NewInputError("failed to build IBGU from merged upgrade data: %s", err.Error())
	}

	if clusterTemplate.Spec.Release != ibguCR.Spec.IBUSpec.SeedImageRef.Version {
		return nil, typederrors.NewInputError(
			"the imageBasedGroupUpgrade seedImageRef version (%s) does not match the ClusterTemplate spec.release (%s)",
			ibguCR.Spec.IBUSpec.SeedImageRef.Version, clusterTemplate.Spec.Release)
	}

	// Dry-run create the IBGU to validate against the API server
	if err := t.client.Create(ctx, ibguCR, client.DryRunAll); err != nil {
		if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
			return nil, fmt.Errorf("failed to dry-run create IBGU: %w", err)
		}
		return nil, typederrors.NewInputError("IBGU dry-run validation failed: %s", err.Error())
	}

	return ibguCR, nil
}

// prepareCVSpec merges and validates upgrade data, prepares the ClusterVersion spec,
// validates the worker-pool rollout configuration, and stores the resolved rollout
// in ClusterUpgradeStatus. For EUS upgrades, it also resolves from configuration or
// the Cincinnati update graph, and persists the intermediate version so subsequent
// reconciliations can drive the EUS state machine.
func (t *provisioningRequestReconcilerTask) prepareCVSpec(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	cv *configv1.ClusterVersion,
	action *ctlrutils.CVUpgradeAction,
) (*configv1.ClusterVersionSpec, error) {
	statusBefore := t.object.DeepCopy()
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus

	// Merge and validate upgrade data against the schema.
	mergedUpgradeData, err := t.mergeAndValidateUpgradeData(clusterTemplate)
	if err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
	}

	// Extract the workerPoolUpgrade data from the merged result
	workerPoolUpgrade, err := extractWorkerPoolUpgrade(mergedUpgradeData, action.IsEUS)
	if err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
	}
	if err := upgradevalidation.ValidateWorkerPoolUpgrade(action.IsEUS, workerPoolUpgrade); err != nil {
		return nil, typederrors.NewInputError("invalid workerPoolUpgrade: %s", err.Error())
	}

	// Extract the clusterVersion data from the merged result
	cvSpecRaw, ok := mergedUpgradeData[ctlrutils.UpgradeDefaultsClusterVersionKey]
	if !ok {
		return nil, typederrors.NewInputError("key %q not found in merged upgrade data",
			ctlrutils.UpgradeDefaultsClusterVersionKey)
	}
	cvSpecBytes, err := json.Marshal(cvSpecRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s spec: %w", ctlrutils.UpgradeDefaultsClusterVersionKey, err)
	}
	cvSpec := configv1.ClusterVersionSpec{}
	if err := json.Unmarshal(cvSpecBytes, &cvSpec); err != nil {
		return nil, typederrors.NewInputError("invalid clusterVersion spec format: %s", err.Error())
	}

	if cvSpec.DesiredUpdate == nil {
		cvSpec.DesiredUpdate = &configv1.Update{}
	}
	// Verify the user provided upgrade version matches the ClusterTemplate release (the final target).
	if cvSpec.DesiredUpdate.Version != "" && cvSpec.DesiredUpdate.Version != clusterTemplate.Spec.Release {
		return nil, typederrors.NewInputError(
			"the clusterVersion desiredUpdate version (%s) does not match the ClusterTemplate spec.release (%s)",
			cvSpec.DesiredUpdate.Version, clusterTemplate.Spec.Release)
	}

	// For EUS upgrades, resolve the intermediate version on the intermediate hop.
	// If intermediateVersion is explicitly configured, use it directly.
	// Otherwise, auto-select a valid intermediate version from the Cincinnati
	// update graph.
	// On the first reconciliation both action.UpgradeToVersion and
	// upgradeStatus.IntermediateVersion are "" — the match is intentional:
	// it enters this block to resolve the intermediate version, which then
	// populates both fields for subsequent reconciliations.
	isIntermediateHop := action.IsEUS && action.UpgradeToVersion == upgradeStatus.IntermediateVersion
	if isIntermediateHop {
		targetVersion := clusterTemplate.Spec.Release

		configIntermediate := ""
		if iv, ok := mergedUpgradeData[ctlrutils.UpgradeIntermediateVersionConfigKey].(string); ok {
			configIntermediate = iv
		}

		// Update action.UpgradeToVersion to the resolved version so the
		// upgrade continues in the same reconciliation.
		if configIntermediate != "" {
			if err := upgradevalidation.ValidateEUSIntermediate(
				configIntermediate, targetVersion,
			); err != nil {
				return nil, fmt.Errorf("failed to validate intermediateVersion: %w", err)
			}
			action.UpgradeToVersion = configIntermediate
		} else {
			resolved, err := t.resolveEUSIntermediateVersion(
				ctx, upgradeStatus.StartVersion, targetVersion, cv, &cvSpec)
			if err != nil {
				return nil, err
			}
			action.UpgradeToVersion = resolved
		}
		// Intermediate upgrade is version-based; clear any user-configured
		// image for target version.
		cvSpec.DesiredUpdate.Image = ""
	}

	// Set the version to the actual upgrade step target (which may be the intermediate version for EUS).
	cvSpec.DesiredUpdate.Version = action.UpgradeToVersion

	// Validate target version is valid semver.
	if _, err := semver.NewVersion(cvSpec.DesiredUpdate.Version); err != nil {
		return nil, typederrors.NewInputError("invalid target version %q: %s", cvSpec.DesiredUpdate.Version, err.Error())
	}

	// Update the upgrade status with the resolved values after validation.
	pauseStateManaged := upgradeStatus.WorkerPoolUpgrade != nil &&
		upgradeStatus.WorkerPoolUpgrade.PauseStateManaged

	stageStatuses := make([]provisioningv1alpha1.WorkerPoolUpgradeStage, 0, len(workerPoolUpgrade.Stages))
	for _, stage := range workerPoolUpgrade.Stages {
		stageStatuses = append(stageStatuses, provisioningv1alpha1.WorkerPoolUpgradeStage{
			Name: stage.Name, Pools: append([]string(nil), stage.Pools...),
		})
	}
	workerStatus := &provisioningv1alpha1.WorkerPoolUpgradeStatus{
		Strategy:              workerPoolUpgrade.Strategy,
		PoolsWithControlPlane: append([]string(nil), workerPoolUpgrade.PoolsWithControlPlane...),
		Stages:                stageStatuses,
		UpgradeThrough:        workerPoolUpgrade.UpgradeThrough,
		PauseStateManaged:     pauseStateManaged,
	}
	authorizedThrough := workerStatus.StageIndex(workerStatus.UpgradeThrough)
	for i := range workerStatus.Stages {
		state := provisioningv1alpha1.WorkerPoolUpgradeStageStateAwaitingAuthorization
		if i <= authorizedThrough {
			state = provisioningv1alpha1.WorkerPoolUpgradeStageStatePending
		}
		workerStatus.Stages[i].State = state
	}
	upgradeStatus.WorkerPoolUpgrade = workerStatus
	if isIntermediateHop {
		upgradeStatus.IntermediateVersion = action.UpgradeToVersion
	}
	if !equality.Semantic.DeepEqual(statusBefore.Status, t.object.Status) {
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return nil, fmt.Errorf("failed to persist cluster upgrade configuration status: %w", err)
		}
	}

	return &cvSpec, nil
}

// extractWorkerPoolUpgrade decodes the merged worker-pool rollout configuration,
// applying OpenShiftDefault for non-EUS upgrades and Parallel for EUS upgrades
// as default when the configuration does not specify a strategy.
func extractWorkerPoolUpgrade(
	mergedUpgradeData map[string]any, isEUS bool,
) (upgradevalidation.WorkerPoolUpgrade, error) {
	// Set the default strategy.
	strategy := constants.WorkerPoolUpgradeStrategyOpenShiftDefault
	if isEUS {
		strategy = constants.WorkerPoolUpgradeStrategyParallel
	}
	workerPoolUpgrade := upgradevalidation.WorkerPoolUpgrade{
		Strategy: strategy,
	}

	raw, ok := mergedUpgradeData[ctlrutils.UpgradeWorkerPoolUpgradeKey]
	if !ok {
		return workerPoolUpgrade, nil
	}
	workerPoolUpgradeData, err := json.Marshal(raw)
	if err != nil {
		return upgradevalidation.WorkerPoolUpgrade{}, fmt.Errorf(
			"failed to marshal %s: %w", ctlrutils.UpgradeWorkerPoolUpgradeKey, err)
	}
	if err := json.Unmarshal(workerPoolUpgradeData, &workerPoolUpgrade); err != nil {
		return upgradevalidation.WorkerPoolUpgrade{}, fmt.Errorf(
			"invalid %s: %w", ctlrutils.UpgradeWorkerPoolUpgradeKey, err)
	}
	return workerPoolUpgrade, nil
}

// resolveEUSIntermediateVersion auto-selects the intermediate version from the Cincinnati update graph.
func (t *provisioningRequestReconcilerTask) resolveEUSIntermediateVersion(
	ctx context.Context,
	startVersion, targetVersion string,
	cv *configv1.ClusterVersion,
	cvSpec *configv1.ClusterVersionSpec,
) (string, error) {
	if cvSpec.Channel == "" {
		return "", typederrors.NewInputError(
			"channel is required to auto-select intermediateVersion for EUS-to-EUS upgrades; " +
				"set clusterVersion.channel in the upgrade configuration or specify intermediateVersion explicitly")
	}

	upstream := string(cvSpec.Upstream)
	if upstream == "" {
		upstream = cincinnati.DefaultGraphURL
	}
	upstreamURI, err := url.Parse(upstream)
	if err != nil {
		return "", typederrors.NewInputError("invalid upstream URL %q: %s", upstream, err.Error())
	}
	if upstreamURI.Host == "" || (upstreamURI.Scheme != "http" && upstreamURI.Scheme != "https") {
		return "", typederrors.NewInputError(
			"invalid upstream URL %q: must be an absolute HTTP or HTTPS URL", upstream)
	}

	arch, err := t.getClusterArchitecture(ctx, cv)
	if err != nil {
		return "", fmt.Errorf("failed to determine cluster architecture: %w", err)
	}

	selected, err := cincinnati.SelectIntermediateVersion(
		ctx, t.client, t.logger, upstreamURI, cvSpec.Channel, arch, startVersion, targetVersion)
	if err != nil {
		return "", fmt.Errorf("unable to auto-select intermediateVersion: %w", err)
	}

	t.logger.InfoContext(ctx, "Auto-selected intermediateVersion from Cincinnati graph",
		slog.String("intermediateVersion", selected),
		slog.String("targetVersion", targetVersion),
		slog.String("channel", cvSpec.Channel),
		slog.String("upstream", upstream))
	return selected, nil
}

// getClusterArchitecture determines the cluster architecture for Cincinnati
// graph queries. It checks (in order):
//  1. spec.desiredUpdate.architecture on the spoke CV (explicit multi-arch transition)
//  2. cpuArchitecture from the ClusterInstance on the hub
//  3. Falls back to amd64
func (t *provisioningRequestReconcilerTask) getClusterArchitecture(
	ctx context.Context, cv *configv1.ClusterVersion,
) (string, error) {
	if cv.Spec.DesiredUpdate != nil && cv.Spec.DesiredUpdate.Architecture != "" {
		return strings.ToLower(string(cv.Spec.DesiredUpdate.Architecture)), nil
	}

	ci, err := t.getExistingClusterInstance(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read ClusterInstance for architecture: %w", err)
	}

	switch ci.Spec.CPUArchitecture {
	case siteconfig.CPUArchitectureX86_64:
		return "amd64", nil
	case siteconfig.CPUArchitectureAarch64:
		return "arm64", nil
	case siteconfig.CPUArchitectureMulti:
		return "multi", nil
	default:
		return "amd64", nil
	}
}

// mergeAndValidateUpgradeData merges upgrade defaults from the ClusterTemplate with
// upgrade parameters from the ProvisioningRequest, and validates the merged result
// against the schema defined in the ClusterTemplate's templateParameterSchema.
func (t *provisioningRequestReconcilerTask) mergeAndValidateUpgradeData(
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) (map[string]any, error) {

	// Extract upgrade defaults from the ClusterTemplate if present.
	var upgradeDefaultsMap map[string]any
	if clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0 {
		if err := json.Unmarshal(clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults.Raw, &upgradeDefaultsMap); err != nil {
			return nil, fmt.Errorf("upgradeDefaults is not a map: %w", err)
		}
	}

	// Extract upgrade parameters from the ProvisioningRequest if present.
	var upgradeParamsMap map[string]any
	var templateParams map[string]any
	if err := json.Unmarshal(t.object.Spec.TemplateParameters.Raw, &templateParams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal templateParameters: %w", err)
	}
	if upgradeParamsRaw, ok := templateParams[constants.TemplateParamUpgrade]; ok {
		upgradeParamsMap, ok = upgradeParamsRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("upgradeParameters is not a map")
		}
	}

	// When both are empty, return an empty map — the caller will detect
	// the missing imageBasedGroupUpgrade key and report a clear error.
	if len(upgradeDefaultsMap) == 0 && len(upgradeParamsMap) == 0 {
		return map[string]any{}, nil
	}
	// Merge PR overrides on top of CT defaults
	mergedUpgradeData, err := mergeClusterTemplateInputWithDefaults(upgradeParamsMap, upgradeDefaultsMap, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to merge upgrade parameters with defaults: %w", err)
	}

	// Seed generation uses a complete internal schema after merge. The
	// templateParameterSchema only describes fields a PR author may override.
	if _, hasSeed := mergedUpgradeData[ctlrutils.UpgradeDefaultsSeedGenerationKey]; hasSeed {
		if err := provisioningv1alpha1.ValidateSeedGenerationUpgradeData(mergedUpgradeData); err != nil {
			return nil, fmt.Errorf("merged seed generation parameters: %w", err)
		}
		return mergedUpgradeData, nil
	}

	// Existing upgrade types validate the merged data against their public schema.
	upgradeSchema, err := provisioningv1alpha1.ExtractSubSchema(
		clusterTemplate.Spec.TemplateParameterSchema.Raw, constants.TemplateParamUpgrade)
	if err != nil {
		return nil, fmt.Errorf("failed to extract %s schema: %w", constants.TemplateParamUpgrade, err)
	}
	if err := provisioningv1alpha1.ValidateJsonAgainstJsonSchema(upgradeSchema, mergedUpgradeData); err != nil {
		return nil, fmt.Errorf(
			"merged upgrade parameters do not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema.%s: %s",
			clusterTemplate.Name, constants.TemplateParamUpgrade, err.Error())
	}

	return mergedUpgradeData, nil
}

func isIBGUProgressing(cr *ibgu.ImageBasedGroupUpgrade) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, "Progressing")
	if condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return true
}

// upgradeRBACRules defines the RBAC permissions delivered to the spoke cluster
// for ClusterVersion upgrade operations.
var upgradeRBACRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{"config.openshift.io"},
		Resources: []string{"clusterversions"},
		Verbs:     []string{"get", "list", "watch", "update", "patch"},
	},
	{
		APIGroups: []string{"machineconfiguration.openshift.io"},
		Resources: []string{"machineconfigpools"},
		Verbs:     []string{"get", "list", "watch", "update", "patch"},
	},
}

// upgradeSpokeScheme is the scheme used by the spoke client for upgrade operations.
var upgradeSpokeScheme = spokeclient.NewSpokeScheme(configv1.Install, mcfgv1.Install)

// handleClusterVersionUpgrade manages the lifecycle of a ClusterVersion upgrade
// on the spoke cluster. It supports both standard (z/y-stream) and EUS-to-EUS
// upgrades. For EUS, it drives a two-phase upgrade (intermediate then target)
// with worker MCP pause/unpause lifecycle.
//
// The state machine is driven by spoke CV history:
//   - No history entry (pre-start): validates input, checks preconditions,
//     patches channel/upstream, verifies the update graph, and triggers the upgrade
//   - History entry, not yet completed (in-progress): monitors Progressing/Failing
//     conditions
//   - History entry with Completed: cleans up spoke resources and signals
//     completion
//
// A timeout check runs after the state machine to catch upgrades that exceed the
// configured clusterUpgradeTimeout.
func (t *provisioningRequestReconcilerTask) handleClusterVersionUpgrade(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	clusterName string,
	upgradeCfg *ctlrutils.UpgradeConfig,
) (ctrl.Result, bool, error) {
	nextReconcile := ctrl.Result{}
	proceed := false
	var err error

	targetVersion := clusterTemplate.Spec.Release
	msaName := t.object.Name + "-upgrade"
	mwName := t.object.Name + "-upgrade-rbac"

	if err := t.initUpgradeStatus(ctx, clusterName); err != nil {
		// IsUpgradeRequested already guards against a missing openshiftVersion
		// label (requeues until it appears), so the label-missing path here is
		// defensive — it should not be reached in normal operation.
		if typederrors.IsInputError(err) {
			if updateErr := t.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
			); updateErr != nil {
				return ctrl.Result{}, false, updateErr
			}
			return ctrl.Result{}, false, nil
		}
		return ctrl.Result{}, false, err
	}

	// Check if the upgrade is an EUS upgrade.
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	startVersion := upgradeStatus.StartVersion
	isEUS, err := ctlrutils.IsEUSUpgrade(startVersion, targetVersion)
	if err != nil {
		if updateErr := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
		); updateErr != nil {
			return ctrl.Result{}, false, updateErr
		}
		return ctrl.Result{}, false, nil
	}

	// Set the timeout.
	switch {
	case upgradeCfg.Timeout > 0:
		t.timeouts.clusterUpgrade = upgradeCfg.Timeout
	case isEUS:
		t.timeouts.clusterUpgrade = ctlrutils.DefaultClusterEUSUpgradeTimeout
	default:
		t.timeouts.clusterUpgrade = ctlrutils.DefaultClusterUpgradeTimeout
	}

	// Ensure spoke client is ready.
	clients, ready, err := spokeclient.EnsureSpokeClient(
		ctx, t.client, t.logger, clusterName,
		msaName, mwName,
		upgradeRBACRules, upgradeSpokeScheme, spokeclient.RuntimeClientOnly)
	if err != nil {
		if !typederrors.IsInputError(err) {
			return ctrl.Result{}, false, fmt.Errorf("failed to setup spoke client: %w", err)
		}
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, false, nil
	}
	if !ready {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.Pending, "Preparing upgrade resources",
		); err != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
		}
		if timedOut, err := t.isCVUpgradeTimedOut(ctx, clusterName, nil); timedOut || err != nil {
			return ctrl.Result{}, false, err
		}
		return requeueWithShortInterval(), false, nil
	}
	spokeClient := clients.Client

	// Get ClusterVersion from spoke.
	cv := &configv1.ClusterVersion{}
	if err := spokeClient.Get(ctx, types.NamespacedName{Name: ctlrutils.ClusterVersionName}, cv); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to get spoke ClusterVersion: %w", err)
	}

	// Intermediate comes from status only; IntermediateVersion is resolved
	// inside prepareCVSpec for EUS upgrades and stored in status.
	intermediateVersion := upgradeStatus.IntermediateVersion
	action := ctlrutils.ResolveCVUpgradeAction(
		cv, targetVersion, intermediateVersion, isEUS)

	// observedGeneration guard — don't act on stale conditions.
	if cv.Status.ObservedGeneration == cv.Generation {
		switch action.Phase {
		case ctlrutils.PhasePreStart:
			nextReconcile, err = t.handleCVUpgradePreStart(
				ctx, spokeClient, cv, clusterTemplate, action)
		case ctlrutils.PhaseCompleted:
			nextReconcile, err = t.handleCVUpgradeCompleted(
				ctx, spokeClient, cv, clusterTemplate, clusterName, msaName, mwName, action)
			if nextReconcile.RequeueAfter == 0 && err == nil {
				proceed = true
			}
		case ctlrutils.PhaseInProgress:
			nextReconcile, err = t.handleCVUpgradeInProgress(ctx, cv, action)
		}
	} else {
		t.logger.InfoContext(ctx, "ClusterVersion observedGeneration does not match generation, requeueing",
			slog.Int64("observedGeneration", cv.Status.ObservedGeneration),
			slog.Int64("generation", cv.Generation))
		nextReconcile = requeueWithShortInterval()
		err = nil
	}

	// Timeout check runs AFTER the state machine so that terminal states
	// (Completed, PreconditionChecksFailed) are set first. isCVUpgradeTimedOut
	// skips terminal states, avoiding a false timeout when the upgrade just completed.
	if timedOut, timeoutErr := t.isCVUpgradeTimedOut(ctx, clusterName, cv); timedOut || timeoutErr != nil {
		return ctrl.Result{}, false, timeoutErr
	}

	return nextReconcile, proceed, err
}

// handleCVUpgradeCompleted handles a completed ClusterVersion update. It first
// reconciles any controller-managed worker-pool rollout, then cleans up spoke
// access and marks the overall upgrade completed.
func (t *provisioningRequestReconcilerTask) handleCVUpgradeCompleted(
	ctx context.Context, spokeClient client.Client, cv *configv1.ClusterVersion,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	clusterName, msaName, mwName string, action *ctlrutils.CVUpgradeAction,
) (ctrl.Result, error) {
	// Record when CVO completed the target ClusterVersion update.
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	if upgradeStatus.ClusterVersionCompletedAt == nil {
		historyEntry := ctlrutils.FindCVHistoryEntry(cv, action.UpgradeToVersion)
		if historyEntry != nil && historyEntry.CompletionTime != nil {
			upgradeStatus.ClusterVersionCompletedAt = historyEntry.CompletionTime
		} else {
			now := metav1.Now()
			upgradeStatus.ClusterVersionCompletedAt = &now
		}
	}
	result, completed, err := t.reconcileWorkerPoolRollout(ctx, spokeClient, clusterTemplate)
	if err != nil || !completed {
		return result, err
	}

	t.logger.InfoContext(ctx, "Cluster upgrade completed",
		slog.String("clusterName", clusterName), slog.String("upgradeToVersion", action.UpgradeToVersion))

	if err := spokeclient.CleanupSpokeAccess(ctx, t.client, clusterName, msaName, mwName); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to cleanup spoke access: %w", err)
	}

	if err := t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.Completed,
		fmt.Sprintf("Upgrade to version %s completed", action.UpgradeToVersion),
	); err != nil {
		return ctrl.Result{}, err
	}
	return doNotRequeue(), nil
}

// refreshCustomStageAuthorization reads the latest workerPoolUpgrade.upgradeThrough
// configuration and persists any forward authorization.
func (t *provisioningRequestReconcilerTask) refreshCustomStageAuthorization(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) error {
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	status := upgradeStatus.WorkerPoolUpgrade
	requestedThrough, err := upgrade.RequestedWorkerPoolUpgradeThrough(
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults.Raw,
		t.object.Spec.TemplateParameters.Raw,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve requested Custom stage authorization: %w", err)
	}
	currentIndex := status.StageIndex(status.UpgradeThrough)
	if status.UpgradeThrough != "" && currentIndex < 0 {
		t.logger.WarnContext(ctx, "Ignoring Custom stage authorization from unknown persisted stage",
			slog.String("currentUpgradeThrough", status.UpgradeThrough))
		return nil
	}
	requestedIndex := status.StageIndex(requestedThrough)
	if requestedThrough != "" && requestedIndex < 0 {
		t.logger.WarnContext(ctx, "Ignoring Custom stage authorization for unknown stage",
			slog.String("requestedUpgradeThrough", requestedThrough))
		return nil
	}
	if requestedIndex < currentIndex {
		t.logger.WarnContext(ctx, "Ignoring backward Custom stage authorization",
			slog.String("currentUpgradeThrough", status.UpgradeThrough),
			slog.String("requestedUpgradeThrough", requestedThrough))
		return nil
	}
	if requestedIndex == currentIndex {
		return nil
	}

	// Resume the timeout when authorization advances after a user wait.
	if upgradeStatus.TimeoutSuspendedAt != nil {
		waitingDuration := metav1.Now().Sub(upgradeStatus.TimeoutSuspendedAt.Time)
		if waitingDuration > 0 {
			if upgradeStatus.AccumulatedSuspendedDuration == nil {
				upgradeStatus.AccumulatedSuspendedDuration = &metav1.Duration{}
			}
			upgradeStatus.AccumulatedSuspendedDuration.Duration += waitingDuration
		}
		upgradeStatus.TimeoutSuspendedAt = nil
	}

	status.UpgradeThrough = requestedThrough
	// Preserve observed progress and mark newly authorized stages Pending;
	// later stages continue waiting for authorization.
	for i := range status.Stages {
		stage := &status.Stages[i]
		if stage.State == provisioningv1alpha1.WorkerPoolUpgradeStageStateCompleted ||
			stage.State == provisioningv1alpha1.WorkerPoolUpgradeStageStateInProgress {
			continue
		}
		if i <= requestedIndex {
			stage.State = provisioningv1alpha1.WorkerPoolUpgradeStageStatePending
		} else {
			stage.State = provisioningv1alpha1.WorkerPoolUpgradeStageStateAwaitingAuthorization
		}
	}

	// Persist authorization and timeout resumption before unpausing a stage.
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to persist workerPoolUpgrade.upgradeThrough: %w", err)
	}
	return nil
}

// reconcileWorkerPoolRollout handles worker MCPs after the ClusterVersion update.
// It skips the default strategy and delegates Serial, Parallel, and Custom
// strategies to their rollout handlers, returning whether all worker work is done.
func (t *provisioningRequestReconcilerTask) reconcileWorkerPoolRollout(
	ctx context.Context, spokeClient client.Client,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) (ctrl.Result, bool, error) {
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	if upgradeStatus.WorkerPoolUpgrade == nil {
		return ctrl.Result{}, false, fmt.Errorf(
			"workerPoolUpgrade status is missing for completed ClusterVersion upgrade")
	}
	workerPoolUpgrade := upgradeStatus.WorkerPoolUpgrade

	if workerPoolUpgrade.Strategy == constants.WorkerPoolUpgradeStrategyOpenShiftDefault {
		// All worker pools were upgraded with control plane together, so skip the rollout.
		return ctrl.Result{}, true, nil
	}

	mcps, err := ctlrutils.ListNonMasterMCPs(ctx, spokeClient)
	if err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to list MCPs: %w", err)
	}

	switch workerPoolUpgrade.Strategy {
	case constants.WorkerPoolUpgradeStrategySerial, constants.WorkerPoolUpgradeStrategyParallel:
		return t.reconcileSerialOrParallelWorkerPoolRollout(ctx, spokeClient, mcps)
	case constants.WorkerPoolUpgradeStrategyCustom:
		return t.reconcileCustomWorkerPoolRollout(ctx, spokeClient, mcps, clusterTemplate)
	default:
		return ctrl.Result{}, false, fmt.Errorf(
			"unsupported workerPoolUpgrade strategy %q", workerPoolUpgrade.Strategy)
	}
}

// waitForControlPlaneWorkerPools blocks later rollout waves until every pool
// upgrading with the control plane has reported Updated.
func (t *provisioningRequestReconcilerTask) waitForControlPlaneWorkerPools(
	ctx context.Context, mcps []mcfgv1.MachineConfigPool,
) (ctrl.Result, bool, error) {
	workerPoolUpgrade := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.WorkerPoolUpgrade
	pools := ctlrutils.FilterMCPsByNames(mcps, workerPoolUpgrade.PoolsWithControlPlane)
	if notUpdated := ctlrutils.GetNonUpdatedMCPs(ctx, t.logger, pools); len(notUpdated) > 0 {
		updatedPools, waitingPools := workerPoolWaveProgress(pools, notUpdated)
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			workerPoolRolloutProgressMessage(workerPoolUpgrade.Strategy,
				"pools upgrading with the control plane", updatedPools, waitingPools),
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return requeueWithMediumInterval(), false, nil
	}
	return ctrl.Result{}, true, nil
}

// reconcileSerialOrParallelWorkerPoolRollout runs Serial or Parallel worker waves. It
// starts at most one new wave per reconciliation and waits for it to report Updated=true
// before proceeding to the next wave.
func (t *provisioningRequestReconcilerTask) reconcileSerialOrParallelWorkerPoolRollout(
	ctx context.Context, spokeClient client.Client, mcps []mcfgv1.MachineConfigPool,
) (ctrl.Result, bool, error) {
	if result, ready, err := t.waitForControlPlaneWorkerPools(ctx, mcps); err != nil || !ready {
		return result, false, err
	}
	workerPoolUpgrade := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.WorkerPoolUpgrade

	// Get the remaining pools sorted alphabetically by name.
	remaining := ctlrutils.ExcludeMCPsByNames(mcps, workerPoolUpgrade.PoolsWithControlPlane)
	if len(remaining) == 0 {
		return ctrl.Result{}, true, nil
	}

	var waves [][]mcfgv1.MachineConfigPool
	switch workerPoolUpgrade.Strategy {
	case constants.WorkerPoolUpgradeStrategySerial:
		// Pools are upgraded one at a time in serial, so there is one wave per remaining pool.
		waves = make([][]mcfgv1.MachineConfigPool, 0, len(remaining))
		for i := range remaining {
			waves = append(waves, []mcfgv1.MachineConfigPool{remaining[i]})
		}
	case constants.WorkerPoolUpgradeStrategyParallel:
		// Pools are upgraded in parallel, so there is one wave for all remaining pools.
		waves = [][]mcfgv1.MachineConfigPool{remaining}
	default:
		return ctrl.Result{}, false, fmt.Errorf(
			"unsupported persisted workerPoolUpgrade strategy %q", workerPoolUpgrade.Strategy)
	}

	completedPools := append([]string(nil), workerPoolUpgrade.PoolsWithControlPlane...)
	// Unpause the pools in the waves one by one. Wait for each wave to report Updated=true
	// before proceeding to the next wave.
	for _, wave := range waves {
		notUpdated := ctlrutils.GetNonUpdatedMCPs(ctx, t.logger, wave)
		updatedInWave, waitingPools := workerPoolWaveProgress(wave, notUpdated)
		unpaused, err := ctlrutils.UnpauseMCPs(ctx, spokeClient, t.logger, wave)
		if err != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to unpause MCPs: %w", err)
		}
		if unpaused || len(notUpdated) > 0 {
			updatedPools := append(append([]string(nil), completedPools...), updatedInWave...)
			message := workerPoolRolloutProgressMessage(workerPoolUpgrade.Strategy,
				"", updatedPools, waitingPools)
			if err := t.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.InProgress, message,
			); err != nil {
				return ctrl.Result{}, false, err
			}
			return requeueWithMediumInterval(), false, nil
		}
		for i := range wave {
			completedPools = append(completedPools, wave[i].Name)
		}
	}
	return ctrl.Result{}, true, nil
}

// reconcileCustomWorkerPoolRollout refreshes authorization, waits for pools
// upgrading with the control plane, then unpauses Custom stages in order through
// upgradeThrough. It suspends the timeout after all authorized stages finish if
// another stage still awaits authorization.
func (t *provisioningRequestReconcilerTask) reconcileCustomWorkerPoolRollout(
	ctx context.Context, spokeClient client.Client, mcps []mcfgv1.MachineConfigPool,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) (ctrl.Result, bool, error) {
	// Record new authorization if changed and resume the timeout before checking live pools.
	if err := t.refreshCustomStageAuthorization(ctx, clusterTemplate); err != nil {
		return ctrl.Result{}, false, err
	}
	if result, ready, err := t.waitForControlPlaneWorkerPools(ctx, mcps); err != nil || !ready {
		return result, false, err
	}

	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	workerPoolUpgrade := upgradeStatus.WorkerPoolUpgrade
	lastAuthorizedStageIndex := workerPoolUpgrade.StageIndex(workerPoolUpgrade.UpgradeThrough)

	completedPools := append([]string(nil), workerPoolUpgrade.PoolsWithControlPlane...)
	// Unpause the authorized stages one by one. Wait for each stage to report Updated=true before
	// proceeding to the next authorized stage.
	for i := 0; i <= lastAuthorizedStageIndex; i++ {
		stage := &workerPoolUpgrade.Stages[i]
		if stage.State != provisioningv1alpha1.WorkerPoolUpgradeStageStateCompleted {
			if stage.StartedAt == nil {
				// Record stage start time if not already set.
				now := metav1.Now()
				stage.StartedAt = &now
			}
			// Mark the stage as in progress.
			stage.State = provisioningv1alpha1.WorkerPoolUpgradeStageStateInProgress

			wave := ctlrutils.FilterMCPsByNames(mcps, stage.Pools)
			notUpdated := ctlrutils.GetNonUpdatedMCPs(ctx, t.logger, wave)
			updatedInWave, waitingPools := workerPoolWaveProgress(wave, notUpdated)
			unpaused, err := ctlrutils.UnpauseMCPs(ctx, spokeClient, t.logger, wave)
			if err != nil {
				return ctrl.Result{}, false, fmt.Errorf("failed to unpause MCPs: %w", err)
			}
			if unpaused || len(notUpdated) > 0 {
				updatedPools := append(append([]string(nil), completedPools...), updatedInWave...)
				message := workerPoolRolloutProgressMessage(workerPoolUpgrade.Strategy,
					fmt.Sprintf("stage %q in progress", stage.Name), updatedPools, waitingPools)
				if err := t.updateUpgradeStatus(ctx,
					provisioningv1alpha1.CRconditionReasons.InProgress, message,
				); err != nil {
					return ctrl.Result{}, false, err
				}
				return requeueWithMediumInterval(), false, nil
			}

			// All pools in the stage have reported Updated=true, so mark the stage as completed with a timestamp.
			stage.State = provisioningv1alpha1.WorkerPoolUpgradeStageStateCompleted
			if stage.CompletedAt == nil {
				now := metav1.Now()
				stage.CompletedAt = &now
			}
		}
		// Include both previously completed and newly completed stages in the
		// progress message.
		completedPools = append(completedPools, stage.Pools...)
	}

	// All authorized stages have completed, so check if there is a next stage that still needs authorization.
	nextStageIndex := lastAuthorizedStageIndex + 1
	if nextStageIndex < len(workerPoolUpgrade.Stages) {
		now := metav1.Now()
		// Suspend the timeout if the next stage is awaiting authorization.
		if upgradeStatus.TimeoutSuspendedAt == nil {
			upgradeStatus.TimeoutSuspendedAt = &now
		}

		nextStage := workerPoolUpgrade.Stages[nextStageIndex]
		message := "ClusterVersion upgrade completed. Custom worker-pool rollout: "
		if nextStageIndex > 0 {
			message += fmt.Sprintf("stage %q completed; ", workerPoolUpgrade.Stages[nextStageIndex-1].Name)
		}
		message += fmt.Sprintf(
			"awaiting authorization for stage %q (pools [%s]). Set workerPoolUpgrade.upgradeThrough to %q",
			nextStage.Name, strings.Join(nextStage.Pools, ", "), nextStage.Name)
		if nextStageIndex < len(workerPoolUpgrade.Stages)-1 {
			message += " or a later stage"
		}
		message += " to continue"
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization, message,
		); err != nil {
			return ctrl.Result{}, false, err
		}
		// PR spec updates trigger reconciliation; keep only a slower fallback poll here.
		return requeueWithCustomInterval(15 * time.Minute), false, nil
	}

	return ctrl.Result{}, true, nil
}

// workerPoolWaveProgress partitions a pre-unpause wave snapshot. Waiting pools
// are paused or reported not updated; all others count as updated.
func workerPoolWaveProgress(
	wave []mcfgv1.MachineConfigPool, notUpdated []string,
) (updated, waiting []string) {
	waitingSet := make(map[string]struct{}, len(notUpdated))
	for _, name := range notUpdated {
		waitingSet[name] = struct{}{}
	}
	for i := range wave {
		name := wave[i].Name
		if wave[i].Spec.Paused {
			waitingSet[name] = struct{}{}
		}
		if _, found := waitingSet[name]; found {
			waiting = append(waiting, name)
		} else {
			updated = append(updated, name)
		}
	}
	return updated, waiting
}

// workerPoolRolloutProgressMessage reports the completed ClusterVersion update,
// rollout strategy, optional phase, updated pools, and pools still waiting.
func workerPoolRolloutProgressMessage(strategy, phase string, updatedPools, waitingPools []string) string {
	message := fmt.Sprintf("ClusterVersion upgrade completed. %s worker-pool rollout:", strategy)
	if phase != "" {
		message += " " + phase + ";"
	}
	if len(updatedPools) > 0 {
		message += fmt.Sprintf(" updated pools [%s];", strings.Join(updatedPools, ", "))
	}
	return message + fmt.Sprintf(" waiting for pools [%s] to finish updating", strings.Join(waitingPools, ", "))
}

// handleCVUpgradeInProgress handles the state where the target version has a
// history entry that is not yet completed. It resets startAt to the CVO's actual
// start time and reports the upgrade progressing status based on CV conditions.
func (t *provisioningRequestReconcilerTask) handleCVUpgradeInProgress(
	ctx context.Context, cv *configv1.ClusterVersion,
	action *ctlrutils.CVUpgradeAction,
) (ctrl.Result, error) {
	intermediateVersion := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.IntermediateVersion
	// Reset startAt to CVO's actual start time. The initial startAt was set
	// when entering the upgrade flow (covering the pre-start/setup phase).
	// Once CVO creates a history entry, we reset to its StartedTime so the
	// timeout window reflects the real upgrade duration, not the time spent
	// on precondition checks or spoke client setup which may take extended
	// time if the user needs to fix issues (e.g., install addon, fix channel).
	//
	// For EUS, only reset for the intermediate version — a single timeout
	// (default 8h) covers the entire procedure: intermediate upgrade +
	// target upgrade + worker MCP reconciliation. A single timeout is used
	// rather than per-phase timeouts because the phases are sequential parts
	// of one atomic operation, and individual phase durations vary widely
	// depending on cluster size and workload.
	shouldReset := !action.IsEUS || action.IsEUSIntermediate(intermediateVersion)
	historyEntry := ctlrutils.FindCVHistoryEntry(cv, action.UpgradeToVersion)
	if historyEntry != nil && shouldReset &&
		!t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt.Equal(&historyEntry.StartedTime) {
		upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
		upgradeStatus.StartedAt = &historyEntry.StartedTime
		upgradeStatus.AccumulatedSuspendedDuration = nil
		upgradeStatus.TimeoutSuspendedAt = nil
	}

	var msg string
	reason := provisioningv1alpha1.CRconditionReasons.Unknown
	progressing := ctlrutils.GetCVCondition(cv, configv1.OperatorProgressing)
	if progressing != nil && progressing.Status == configv1.ConditionTrue {
		reason = provisioningv1alpha1.CRconditionReasons.InProgress
		msg = fmt.Sprintf("Upgrading to %s version %s: %s",
			action.VersionLabel(intermediateVersion), action.UpgradeToVersion, progressing.Message)
	} else {
		msg = fmt.Sprintf("Upgrading to %s version %s: CVO stalled",
			action.VersionLabel(intermediateVersion), action.UpgradeToVersion)
		failing := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionFailing)
		if failing != nil && failing.Status == configv1.ConditionTrue {
			msg = fmt.Sprintf("Upgrading to %s version %s: %s",
				action.VersionLabel(intermediateVersion), action.UpgradeToVersion, failing.Message)
		}
	}
	if err := t.updateUpgradeStatus(ctx, reason, msg); err != nil {
		return ctrl.Result{}, err
	}
	return requeueWithMediumInterval(), nil
}

// handleCVUpgradePreStart handles the case where the target version has no
// history entry (not yet started). It merges and validates PR/CT parameters,
// runs precondition checks, patches channel/upstream if needed, verifies the
// update graph, triggers the upgrade, and monitors post-trigger conditions.
func (t *provisioningRequestReconcilerTask) handleCVUpgradePreStart(
	ctx context.Context, spokeClient client.Client, cv *configv1.ClusterVersion,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	action *ctlrutils.CVUpgradeAction,
) (ctrl.Result, error) {
	// Always merge+validate — catches invalid input including updated PR params.
	cvSpec, err := t.prepareCVSpec(ctx, clusterTemplate, cv, action)
	if err != nil {
		if !typederrors.IsInputError(err) {
			if err := t.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.Pending,
				fmt.Sprintf("Preparing upgrade configuration: %s", err.Error()),
			); err != nil {
				return ctrl.Result{}, err
			}
			return requeueWithMediumInterval(), nil
		}

		return ctrl.Result{}, t.handleTerminalCVPreStartFailure(
			ctx, spokeClient, action, err.Error())
	}

	// Check Upgradeable condition for major and minor version upgrades but not if force is set.
	if !cvSpec.DesiredUpdate.Force {
		currentVersion := ctlrutils.GetCurrentCVVersion(cv)
		isMajorOrMinor, err := ctlrutils.IsMajorOrMinorUpgrade(currentVersion, action.UpgradeToVersion)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to determine upgrade type: %w", err)
		}
		if isMajorOrMinor {
			upgradeable := ctlrutils.GetCVCondition(cv, configv1.OperatorUpgradeable)
			if upgradeable != nil && upgradeable.Status == configv1.ConditionFalse {
				if err := t.updateUpgradeStatus(ctx,
					provisioningv1alpha1.CRconditionReasons.Pending,
					fmt.Sprintf("Cluster is not upgradeable: %s", upgradeable.Message),
				); err != nil {
					return ctrl.Result{}, err
				}
				return requeueWithMediumInterval(), nil
			}
		}
	}

	upgradeTriggered := ctlrutils.IsCVUpgradeTriggered(cv, cvSpec.DesiredUpdate)
	if err := t.validateMCPsPreconditions(ctx, spokeClient, action, upgradeTriggered); err != nil {
		if typederrors.IsInputError(err) {
			return ctrl.Result{}, t.handleTerminalCVPreStartFailure(
				ctx, spokeClient, action, err.Error())
		}
		return ctrl.Result{}, err
	}

	// Graph preconditions: patch channel/upstream, check RetrievedUpdates,
	// verify target in availableUpdates.
	if result, proceed, err := t.checkCVGraphPreconditions(
		ctx, spokeClient, cv, cvSpec, action,
	); result.RequeueAfter > 0 || !proceed || err != nil {
		return result, err
	}

	// Prepare the worker MCPs right before triggering the upgrade.
	err = t.prepareWorkerMCPsForUpgrade(ctx, spokeClient, action)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply desiredUpdate. Returns whether the spec actually changed.
	changed, err := ctlrutils.TriggerCVUpgrade(ctx, spokeClient, t.logger, cv, cvSpec.DesiredUpdate)
	if err != nil {
		if errors.IsInvalid(err) || errors.IsBadRequest(err) || errors.IsForbidden(err) {
			return ctrl.Result{}, t.handleTerminalCVPreStartFailure(
				ctx, spokeClient, action, err.Error())
		}
		return ctrl.Result{}, fmt.Errorf("failed to apply desiredUpdate: %w", err)
	}
	if changed {
		intermediateVersion := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.IntermediateVersion
		msg := fmt.Sprintf("Upgrade to %s version %s triggered. Waiting for upgrade to start",
			action.VersionLabel(intermediateVersion), action.UpgradeToVersion)
		if err := t.updateUpgradeStatus(ctx, provisioningv1alpha1.CRconditionReasons.InProgress, msg); err != nil {
			return ctrl.Result{}, err
		}
		return requeueWithShortInterval(), nil
	}

	return t.monitorPostTriggerConditions(ctx, spokeClient, cv, action)
}

// monitorPostTriggerConditions checks CV conditions when desiredUpdate is set
// but no history entry exists yet.
func (t *provisioningRequestReconcilerTask) monitorPostTriggerConditions(
	ctx context.Context, spokeClient client.Client, cv *configv1.ClusterVersion,
	action *ctlrutils.CVUpgradeAction,
) (ctrl.Result, error) {
	intermediateVersion := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.IntermediateVersion
	// Invalid=True — CVO won't retry on invalid input, terminal.
	invalid := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionInvalid)
	if invalid != nil && invalid.Status == configv1.ConditionTrue {
		return ctrl.Result{}, t.handleTerminalCVPreStartFailure(
			ctx, spokeClient, action,
			fmt.Sprintf("Upgrade spec is invalid: %s", invalid.Message))
	}

	// ReleaseAccepted=False — CVO retries payload loading failures, non-terminal.
	releaseAccepted := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionReleaseAccepted)
	if releaseAccepted != nil && releaseAccepted.Status != configv1.ConditionTrue {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.Pending,
			fmt.Sprintf("Release is not accepted: %s", releaseAccepted.Message),
		); err != nil {
			return ctrl.Result{}, err
		}
		return requeueWithMediumInterval(), nil
	}

	msg := fmt.Sprintf("Upgrading to %s version %s: upgrade not started yet",
		action.VersionLabel(intermediateVersion), action.UpgradeToVersion)
	failing := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionFailing)
	if failing != nil && failing.Status == configv1.ConditionTrue {
		msg = fmt.Sprintf("Upgrading to %s version %s: %s",
			action.VersionLabel(intermediateVersion), action.UpgradeToVersion, failing.Message)
	}
	if err := t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.Unknown, msg,
	); err != nil {
		return ctrl.Result{}, err
	}
	return requeueWithMediumInterval(), nil
}

// validateMCPsPreconditions validates MachineConfigPool state before triggering an upgrade.
// All MCPs must be unpaused when the upgrade first starts. Serial and Parallel upgrade strategies
// also require worker MCPs to be updated. The EUS target hop skips these initial checks because
// its workers should stay paused from the intermediate hop. User-correctable
// precondition failures are returned as input errors for the caller to handle.
func (t *provisioningRequestReconcilerTask) validateMCPsPreconditions(
	ctx context.Context, spokeClient client.Client, action *ctlrutils.CVUpgradeAction,
	upgradeTriggered bool,
) error {
	workerPoolUpgrade := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.WorkerPoolUpgrade
	mcps, err := ctlrutils.ListMCPs(ctx, spokeClient)
	if err != nil {
		return fmt.Errorf("failed to list MCPs: %w", err)
	}
	nonMasterMCPs := make([]mcfgv1.MachineConfigPool, 0, len(mcps))
	for i := range mcps {
		if mcps[i].Name != "master" {
			nonMasterMCPs = append(nonMasterMCPs, mcps[i])
		}
	}

	stages := make([]upgradevalidation.WorkerPoolUpgradeStage, 0, len(workerPoolUpgrade.Stages))
	for _, stage := range workerPoolUpgrade.Stages {
		stages = append(stages, upgradevalidation.WorkerPoolUpgradeStage{
			Name: stage.Name, Pools: append([]string(nil), stage.Pools...),
		})
	}
	workerPoolConfig := upgradevalidation.WorkerPoolUpgrade{
		Strategy:              workerPoolUpgrade.Strategy,
		PoolsWithControlPlane: append([]string(nil), workerPoolUpgrade.PoolsWithControlPlane...),
		Stages:                stages,
		UpgradeThrough:        workerPoolUpgrade.UpgradeThrough,
	}

	if err := upgradevalidation.ValidateWorkerPoolUpgradeMCPs(nonMasterMCPs, workerPoolConfig); err != nil {
		return typederrors.NewInputError("%s", err.Error())
	}

	intermediateVersion := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.IntermediateVersion
	checkInitialMCPState := (!action.IsEUS || action.IsEUSIntermediate(intermediateVersion)) && !upgradeTriggered && !workerPoolUpgrade.PauseStateManaged
	if checkInitialMCPState {
		if paused := ctlrutils.GetPausedMCPs(mcps); len(paused) > 0 {
			return typederrors.NewInputError("MachineConfigPools are paused: %v", paused)
		}

		if workerPoolUpgrade.Strategy != constants.WorkerPoolUpgradeStrategyOpenShiftDefault {
			if notUpdated := ctlrutils.GetNonUpdatedMCPs(ctx, t.logger, nonMasterMCPs); len(notUpdated) > 0 {
				return typederrors.NewInputError("MachineConfigPools not updated: %v", notUpdated)
			}
		}
	}
	return nil
}

// prepareWorkerMCPsForUpgrade configures worker MachineConfigPool pause states
// before triggering a ClusterVersion upgrade. OpenShiftDefault restores any
// pause state previously managed by the controller and otherwise leaves MCPs
// unchanged. Serial and Parallel strategies keep PoolsWithControlPlane unpaused
// and pause the remaining worker pools. The function persists pause-state ownership
// before modifying MCPs so transient failures can be retried safely.
func (t *provisioningRequestReconcilerTask) prepareWorkerMCPsForUpgrade(
	ctx context.Context, spokeClient client.Client, action *ctlrutils.CVUpgradeAction,
) error {
	workerPoolUpgrade := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.WorkerPoolUpgrade
	if workerPoolUpgrade.Strategy == constants.WorkerPoolUpgradeStrategyOpenShiftDefault {
		if !workerPoolUpgrade.PauseStateManaged {
			return nil
		}
		// PauseStateManaged can still be true when the user changes a Serial or
		// Parallel strategy to OpenShiftDefault during a pre-start retry. Restore
		// pools paused by the previous strategy before relinquishing pause-state
		// management to OpenShift.
		if err := t.unpauseNonMasterMCPs(ctx, spokeClient); err != nil {
			return err
		}
		workerPoolUpgrade.PauseStateManaged = false
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return fmt.Errorf("failed to persist worker MachineConfigPool pause-state ownership: %w", err)
		}
		return nil
	}

	if !workerPoolUpgrade.PauseStateManaged {
		// Persist ownership before changing the first MCP. Pausing several MCPs is
		// not atomic, and either an MCP patch or the following ClusterVersion patch
		// can fail transiently. The persisted flag lets the next reconciliation
		// recognize a partially or fully controller-paused set and resume safely.
		workerPoolUpgrade.PauseStateManaged = true
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return fmt.Errorf("failed to persist worker MachineConfigPool pause-state ownership: %w", err)
		}
	}

	// For other strategies, pause the worker pools except for the ones in PoolsWithControlPlane.
	nonMasterMCPs, err := ctlrutils.ListNonMasterMCPs(ctx, spokeClient)
	if err != nil {
		return fmt.Errorf("failed to list MCPs: %w", err)
	}

	toPauseMCPs := nonMasterMCPs
	toUnpauseMCPs := []mcfgv1.MachineConfigPool{}
	if !action.IsEUS {
		toPauseMCPs = ctlrutils.ExcludeMCPsByNames(nonMasterMCPs, workerPoolUpgrade.PoolsWithControlPlane)
		toUnpauseMCPs = ctlrutils.FilterMCPsByNames(nonMasterMCPs, workerPoolUpgrade.PoolsWithControlPlane)
	}
	if err := ctlrutils.PauseMCPs(ctx, spokeClient, t.logger, toPauseMCPs); err != nil {
		return fmt.Errorf("failed to pause MCPs: %w", err)
	}

	// These pools (PoolsWithControlPlane) are normally already unpaused because initial
	// validation requires it and they are excluded from toPause. Reasserting the state is
	// needed when a pre-start configuration change moves a previously paused
	// pool into PoolsWithControlPlane, and also protects against an external
	// actor pausing one during a retry.
	if _, err := ctlrutils.UnpauseMCPs(ctx, spokeClient, t.logger, toUnpauseMCPs); err != nil {
		return fmt.Errorf("failed to unpause MCPs: %w", err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) unpauseNonMasterMCPs(
	ctx context.Context, spokeClient client.Client,
) error {
	nonMaster, err := ctlrutils.ListNonMasterMCPs(ctx, spokeClient)
	if err != nil {
		return fmt.Errorf("failed to list MCPs for unpause: %w", err)
	}
	if _, err := ctlrutils.UnpauseMCPs(ctx, spokeClient, t.logger, nonMaster); err != nil {
		return fmt.Errorf("failed to unpause MCPs: %w", err)
	}
	return nil
}

// handleTerminalCVPreStartFailure records a terminal ClusterVersion pre-start
// failure. Worker MCPs whose pause state is managed by the controller are
// restored for non-EUS upgrades and for the EUS intermediate hop, where the
// control plane has not advanced. They remain paused when the EUS target hop
// fails so the user can investigate safely.
func (t *provisioningRequestReconcilerTask) handleTerminalCVPreStartFailure(
	ctx context.Context, spokeClient client.Client,
	action *ctlrutils.CVUpgradeAction,
	message string,
) error {
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	workerPoolUpgrade := upgradeStatus.WorkerPoolUpgrade
	intermediateVersion := upgradeStatus.IntermediateVersion
	shouldRestore := workerPoolUpgrade != nil && workerPoolUpgrade.PauseStateManaged &&
		(!action.IsEUS || action.IsEUSIntermediate(intermediateVersion))

	if shouldRestore {
		if err := t.unpauseNonMasterMCPs(ctx, spokeClient); err != nil {
			return err
		}
		workerPoolUpgrade.PauseStateManaged = false
	}

	return t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, message)
}

// checkCVGraphPreconditions patches channel/upstream, checks RetrievedUpdates,
// and verifies the target version is in availableUpdates. Returns true if all
// preconditions pass. When false, the ctrl.Result carries the requeue interval.
func (t *provisioningRequestReconcilerTask) checkCVGraphPreconditions(
	ctx context.Context, spokeClient client.Client,
	cv *configv1.ClusterVersion, cvSpec *configv1.ClusterVersionSpec,
	action *ctlrutils.CVUpgradeAction,
) (ctrl.Result, bool, error) {
	patched, err := ctlrutils.PatchCVChannelUpstream(ctx, spokeClient, t.logger, cv, cvSpec)
	if err != nil {
		if errors.IsInvalid(err) || errors.IsBadRequest(err) || errors.IsForbidden(err) {
			return ctrl.Result{}, false, t.handleTerminalCVPreStartFailure(
				ctx, spokeClient, action, err.Error())
		}
		return ctrl.Result{}, false, fmt.Errorf("failed to apply channel/upstream update: %w", err)
	}
	if patched {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.Pending,
			"Channel/upstream updated. Waiting for update to be processed",
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return requeueWithShortInterval(), false, nil
	}

	// RetrievedUpdates=False — CVO retries graph/channel fetch failures, non-terminal.
	retrieved := ctlrutils.GetCVCondition(cv, configv1.RetrievedUpdates)
	if retrieved != nil && retrieved.Status != configv1.ConditionTrue {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.Pending,
			fmt.Sprintf("Update graph is not retrieved: %s", retrieved.Message),
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return requeueWithMediumInterval(), false, nil
	}

	// Verify target version is in availableUpdates (only when image is not set).
	if cvSpec.DesiredUpdate.Image == "" &&
		!ctlrutils.IsCVUpdateAvailable(cv, cvSpec.DesiredUpdate.Version) {
		return ctrl.Result{}, false, t.handleTerminalCVPreStartFailure(
			ctx, spokeClient, action,
			fmt.Sprintf("Target version %s is not available for upgrade", cvSpec.DesiredUpdate.Version))
	}

	return ctrl.Result{}, true, nil
}

// initUpgradeStatus ensures ClusterUpgradeStatus exists with StartedAt and
// StartVersion populated. StartVersion is read from the ManagedCluster's
// openshiftVersion label on first entry and persisted so EUS detection remains
// stable after intermediate upgrades.
func (t *provisioningRequestReconcilerTask) initUpgradeStatus(
	ctx context.Context, clusterName string,
) error {
	if t.object.Status.Extensions.ClusterDetails == nil {
		t.object.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
	}
	if t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus == nil {
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{}
	}
	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	if upgradeStatus.StartedAt.IsZero() {
		now := metav1.Now()
		upgradeStatus.StartedAt = &now
	}
	if upgradeStatus.StartVersion == "" {
		managedCluster := &clusterv1.ManagedCluster{}
		if err := t.client.Get(ctx, types.NamespacedName{Name: clusterName}, managedCluster); err != nil {
			return fmt.Errorf("failed to get ManagedCluster: %w", err)
		}
		openshiftVersion, ok := managedCluster.GetLabels()["openshiftVersion"]
		if !ok {
			return typederrors.NewInputError(
				"openshiftVersion label not found on ManagedCluster %s", clusterName)
		}
		upgradeStatus.StartVersion = openshiftVersion
	}
	return nil
}

// updateUpgradeStatus persists the UpgradeCompleted condition and provisioning
// state based on the given reason:
//   - Terminal success (Completed): condition=True, clears upgrade status
//   - Terminal failure (PreconditionChecksFailed, Failed, TimedOut):
//     condition=False, provisioningState=Failed. TimedOut preserves timing and
//     rollout diagnostics; other failures clear the active timer.
//   - Non-terminal (Pending, InProgress, Unknown, AwaitingStageAuthorization):
//     condition=False, provisioningState=InProgress
func (t *provisioningRequestReconcilerTask) updateUpgradeStatus(
	ctx context.Context,
	reason provisioningv1alpha1.ConditionReason,
	message string,
) error {
	conditionStatus := metav1.ConditionFalse

	switch reason {
	case provisioningv1alpha1.CRconditionReasons.Completed:
		conditionStatus = metav1.ConditionTrue
		// Upgrade finished — clear the entire ClusterUpgradeStatus.
		t.clearUpgradeStatus()
	case provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
		provisioningv1alpha1.CRconditionReasons.Failed,
		provisioningv1alpha1.CRconditionReasons.TimedOut:
		t.logger.ErrorContext(ctx, "Upgrade failed",
			slog.String("reason", string(reason)),
			slog.String("message", message))
		ctlrutils.SetProvisioningStateFailed(t.object, message)
		if reason != provisioningv1alpha1.CRconditionReasons.TimedOut {
			// On other terminal failures, reset the timeout clock for retries while
			// retaining StartVersion and the rest of the upgrade status; They are eventually
			// cleared by cleanupStaleUpgradeState along with the stale terminal
			// condition when switching to a new CT that matches the current cluster version.
			t.resetUpgradeTimeoutAccounting()
		}
	default:
		t.logger.InfoContext(ctx, "Upgrade status update",
			slog.String("reason", string(reason)),
			slog.String("message", message))
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
	}

	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
		reason,
		conditionStatus,
		message,
	)
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
	}
	return nil
}

// isCVUpgradeTimedOut checks if the upgrade has exceeded its timeout. If not,
// it returns false. If timed out, it cleans up spoke resources and sets TimedOut.
// cv may be nil if the spoke client was never ready.
func (t *provisioningRequestReconcilerTask) isCVUpgradeTimedOut(
	ctx context.Context, clusterName string, cv *configv1.ClusterVersion,
) (bool, error) {
	if ctlrutils.IsClusterUpgradeCompleted(t.object) || ctlrutils.IsClusterUpgradeInTerminalFailure(t.object) {
		return false, nil
	}

	if t.object.Status.Extensions.ClusterDetails == nil ||
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus == nil ||
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt == nil {
		return false, nil
	}

	upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
	now := time.Now()
	activeDuration := activeUpgradeDuration(upgradeStatus, now)
	if activeDuration <= t.timeouts.clusterUpgrade {
		return false, nil
	}

	t.logger.InfoContext(ctx, "Upgrade timed out",
		slog.String("clusterName", clusterName),
		slog.Time("timedOutAt", now),
		slog.Time("startAt", upgradeStatus.StartedAt.Time),
		slog.Duration("activeDuration", activeDuration),
		slog.Duration("timeout", t.timeouts.clusterUpgrade))
	msaName := t.object.Name + "-upgrade"
	mwName := t.object.Name + "-upgrade-rbac"
	if err := spokeclient.CleanupSpokeAccess(ctx, t.client, clusterName, msaName, mwName); err != nil {
		return false, fmt.Errorf("failed to cleanup spoke access: %w", err)
	}

	msg := "Upgrade timed out"
	failing := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionFailing)
	if failing != nil && failing.Status == configv1.ConditionTrue {
		msg = fmt.Sprintf("Upgrade timed out: %s", failing.Message)
	}
	if err := t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.TimedOut, msg,
	); err != nil {
		return false, err
	}
	return true, nil
}

// activeUpgradeDuration returns nonnegative time spent actively upgrading,
// excluding completed authorization waits and any current suspended wait.
func activeUpgradeDuration(
	upgradeStatus *provisioningv1alpha1.ClusterUpgradeStatus, now time.Time,
) time.Duration {
	if upgradeStatus == nil || upgradeStatus.StartedAt == nil {
		return 0
	}
	end := now
	if upgradeStatus.TimeoutSuspendedAt != nil {
		end = upgradeStatus.TimeoutSuspendedAt.Time
	}
	duration := end.Sub(upgradeStatus.StartedAt.Time)
	if upgradeStatus.AccumulatedSuspendedDuration != nil {
		duration -= upgradeStatus.AccumulatedSuspendedDuration.Duration
	}
	if duration < 0 {
		return 0
	}
	return duration
}

// cleanupStaleUpgradeState removes stale upgrade state. Cleans up spoke
// resources if present and removes the UpgradeCompleted condition.
func (t *provisioningRequestReconcilerTask) cleanupStaleUpgradeState(
	ctx context.Context, clusterName string,
) error {
	t.logger.InfoContext(ctx, "Cleaning up stale upgrade state",
		slog.String("clusterName", clusterName))

	msaName := t.object.Name + "-upgrade"
	mwName := t.object.Name + "-upgrade-rbac"
	if err := spokeclient.CleanupSpokeAccess(ctx, t.client, clusterName, msaName, mwName); err != nil {
		return fmt.Errorf("failed to cleanup spoke access: %w", err)
	}

	t.clearUpgradeStatus()
	meta.RemoveStatusCondition(&t.object.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
	}
	return nil
}

// resetUpgradeTimeoutAccounting clears timeout fields after a non-timeout
// terminal failure so a retry can establish a new start time. Other upgrade
// status fields, including StartVersion, are left unchanged.
func (t *provisioningRequestReconcilerTask) resetUpgradeTimeoutAccounting() {
	if t.object.Status.Extensions.ClusterDetails != nil &&
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus != nil {
		upgradeStatus := t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus
		upgradeStatus.StartedAt = nil
		upgradeStatus.AccumulatedSuspendedDuration = nil
		upgradeStatus.TimeoutSuspendedAt = nil
	}
}

// clearUpgradeStatus clears the entire upgrade status. Called on completion
// and stale cleanup when the upgrade is fully done or abandoned.
func (t *provisioningRequestReconcilerTask) clearUpgradeStatus() {
	if t.object.Status.Extensions.ClusterDetails != nil {
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = nil
	}
}
