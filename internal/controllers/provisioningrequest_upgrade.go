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
	"time"

	"github.com/coreos/go-semver/semver"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils/spokeclient"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	configv1 "github.com/openshift/api/config/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

	upgradeType, upgradeTimeout, err := parseUpgradeConfig(clusterTemplate, t.object)
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
	t.timeouts.clusterUpgrade = upgradeTimeout

	switch upgradeType {
	case ctlrutils.UpgradeDefaultsClusterVersionKey:
		return t.handleClusterVersionUpgrade(ctx, clusterTemplate, clusterName)
	case ctlrutils.UpgradeDefaultsIBGUKey:
		return t.handleIBGUUpgrade(ctx, clusterTemplate, clusterName)
	default:
		t.logger.ErrorContext(ctx, "Unexpected upgrade type from parseUpgradeConfig",
			slog.String("upgradeType", upgradeType))
		return doNotRequeue(), false, nil
	}
}

// parseUpgradeConfig inspects the ProvisioningRequest's upgradeParameters and
// the ClusterTemplate's upgradeDefaults to determine the upgrade type and
// extract the clusterUpgradeTimeout (PR overrides CT). Returns an error if
// both clusterVersion and imageBasedGroupUpgrade keys are found, or if the
// timeout value is invalid.
func parseUpgradeConfig(
	ct *provisioningv1alpha1.ClusterTemplate,
	pr *provisioningv1alpha1.ProvisioningRequest,
) (string, time.Duration, error) {
	hasCV, hasIBGU := false, false
	var timeout time.Duration

	// Check ProvisioningRequest upgradeParameters first.
	if pr.Spec.TemplateParameters.Size() > 0 {
		var templateParams map[string]any
		if err := json.Unmarshal(pr.Spec.TemplateParameters.Raw, &templateParams); err != nil {
			return "", 0, fmt.Errorf("failed to parse templateParameters: %w", err)
		}
		if upgradeParamsRaw, ok := templateParams[constants.TemplateParamUpgrade]; ok {
			upgradeParams, ok := upgradeParamsRaw.(map[string]any)
			if !ok {
				return "", 0, fmt.Errorf("%s is not a map", constants.TemplateParamUpgrade)
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
				hasCV = true
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
				hasIBGU = true
			}
			if ts, ok := upgradeParams[ctlrutils.ClusterUpgradeTimeoutConfigKey].(string); ok {
				d, err := time.ParseDuration(ts)
				if err != nil {
					return "", 0, fmt.Errorf("invalid clusterUpgradeTimeout %q in upgradeParameters: %w", ts, err)
				}
				timeout = d
			}
		}
	}

	// Check ClusterTemplate upgradeDefaults.
	if ct.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0 {
		var defaults map[string]any
		if err := json.Unmarshal(ct.Spec.TemplateDefaults.UpgradeDefaults.Raw, &defaults); err != nil {
			return "", 0, fmt.Errorf("failed to parse upgradeDefaults: %w", err)
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
			hasCV = true
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
			hasIBGU = true
		}
		if timeout == 0 {
			if ts, ok := defaults[ctlrutils.ClusterUpgradeTimeoutConfigKey].(string); ok {
				d, err := time.ParseDuration(ts)
				if err != nil {
					return "", 0, fmt.Errorf("invalid clusterUpgradeTimeout %q in upgradeDefaults: %w", ts, err)
				}
				timeout = d
			}
		}
	}

	if timeout == 0 {
		timeout = ctlrutils.DefaultClusterUpgradeTimeout
	}

	if hasCV && hasIBGU {
		return "", 0, fmt.Errorf(
			"upgrade configuration contains both %q and %q keys; only one upgrade type is allowed",
			ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey)
	}

	if hasCV {
		return ctlrutils.UpgradeDefaultsClusterVersionKey, timeout, nil
	}
	if hasIBGU {
		return ctlrutils.UpgradeDefaultsIBGUKey, timeout, nil
	}
	return "", 0, fmt.Errorf(
		"no upgrade configuration found: upgradeDefaults or upgradeParameters must contain %q or %q",
		ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey)
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
		if err := ctlrutils.CreateK8sCR(ctx, t.client, ibgu, t.object, ctlrutils.UPDATE); err != nil {
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

// prepareCVSpec merges and validates upgrade data, then parses the
// clusterVersion spec into typed structs. Returns the ClusterVersionSpec
// (for channel/upstream) and the cvSpec.DesiredUpdate (with version set to target).
func (t *provisioningRequestReconcilerTask) prepareCVSpec(
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	targetVersion string,
) (*configv1.ClusterVersionSpec, error) {
	// Merge and validate upgrade data against the schema.
	mergedUpgradeData, err := t.mergeAndValidateUpgradeData(clusterTemplate)
	if err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
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

	// Ensure cvSpec.DesiredUpdate exists with version set to target.
	if cvSpec.DesiredUpdate == nil {
		cvSpec.DesiredUpdate = &configv1.Update{}
	}
	if cvSpec.DesiredUpdate.Version != "" && cvSpec.DesiredUpdate.Version != targetVersion {
		return nil, typederrors.NewInputError(
			"the clusterVersion desiredUpdate version (%s) does not match the target version (%s)",
			cvSpec.DesiredUpdate.Version, targetVersion)
	}
	cvSpec.DesiredUpdate.Version = targetVersion

	// Validate target version is valid semver.
	if _, err := semver.NewVersion(cvSpec.DesiredUpdate.Version); err != nil {
		return nil, typederrors.NewInputError("invalid target version %q: %s", cvSpec.DesiredUpdate.Version, err.Error())
	}

	return &cvSpec, nil
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
	mergedUpgradeData, err := mergeClusterTemplateInputWithDefaults(upgradeParamsMap, upgradeDefaultsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to merge upgrade parameters with defaults: %w", err)
	}

	// Validate the merged data against the upgradeParameters schema
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
}

// upgradeSpokeScheme is the scheme used by the spoke client for upgrade operations.
var upgradeSpokeScheme = spokeclient.NewSpokeScheme(configv1.Install)

// handleClusterVersionUpgrade manages the lifecycle of a ClusterVersion upgrade
// on the spoke cluster. It sets up spoke access via ManagedServiceAccount, then
// drives a state machine based on the spoke CV's history:
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
) (ctrl.Result, bool, error) {
	nextReconcile := ctrl.Result{}
	proceed := false
	var err error

	targetVersion := clusterTemplate.Spec.Release
	msaName := t.object.Name + "-upgrade"
	mwName := t.object.Name + "-upgrade-rbac"

	// Record the upgrade start time if not already set.
	if t.object.Status.Extensions.ClusterDetails == nil {
		t.object.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
	}
	if t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt.IsZero() {
		now := metav1.Now()
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt = &now
	}

	// Ensure spoke client is ready.
	spokeClient, ready, err := spokeclient.EnsureSpokeClient(
		ctx, t.client, t.logger, clusterName,
		msaName, mwName,
		upgradeRBACRules, upgradeSpokeScheme)
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

	// Get ClusterVersion from spoke.
	cv := &configv1.ClusterVersion{}
	if err := spokeClient.Get(ctx, types.NamespacedName{Name: ctlrutils.ClusterVersionName}, cv); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to get spoke ClusterVersion: %w", err)
	}

	// observedGeneration guard — don't act on stale conditions.
	if cv.Status.ObservedGeneration == cv.Generation {
		historyEntry := ctlrutils.FindCVHistoryEntry(cv, targetVersion)
		switch {
		case historyEntry == nil:
			nextReconcile, err = t.handleCVUpgradePreStart(ctx, spokeClient, cv, clusterTemplate, targetVersion)
		case historyEntry.State == configv1.CompletedUpdate:
			nextReconcile, err = t.handleCVUpgradeCompleted(ctx, clusterName, msaName, mwName, targetVersion)
			if nextReconcile.RequeueAfter == 0 && err == nil {
				proceed = true
			}
		default:
			nextReconcile, err = t.handleCVUpgradeInProgress(ctx, cv, targetVersion, historyEntry)
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

// handleCVUpgradeCompleted handles the terminal success state.
func (t *provisioningRequestReconcilerTask) handleCVUpgradeCompleted(
	ctx context.Context, clusterName, msaName, mwName, targetVersion string,
) (ctrl.Result, error) {
	t.logger.InfoContext(ctx, "Cluster upgrade completed",
		slog.String("clusterName", clusterName), slog.String("targetVersion", targetVersion))

	if err := spokeclient.CleanupSpokeAccess(ctx, t.client, clusterName, msaName, mwName); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to cleanup spoke access: %w", err)
	}

	if err := t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.Completed,
		fmt.Sprintf("Upgrade to version %s completed", targetVersion),
	); err != nil {
		return ctrl.Result{}, err
	}
	return doNotRequeue(), nil
}

// handleCVUpgradeInProgress handles the state where the target version has a
// history entry that is not yet completed. It resets startAt to the CVO's actual
// start time and reports the upgrade progressing status based on CV conditions.
func (t *provisioningRequestReconcilerTask) handleCVUpgradeInProgress(
	ctx context.Context, cv *configv1.ClusterVersion, targetVersion string,
	historyEntry *configv1.UpdateHistory,
) (ctrl.Result, error) {
	// Reset startAt to CVO's actual start time. The initial startAt was set
	// when entering the upgrade flow (covering the pre-start/setup phase).
	// Once CVO creates a history entry, we reset to its StartedTime so the
	// timeout window reflects the real upgrade duration, not the time spent
	// on precondition checks or spoke client setup which may take extended
	// time if the user needs to fix issues (e.g., install addon, fix channel).
	if !t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt.Equal(&historyEntry.StartedTime) {
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt = &historyEntry.StartedTime
	}

	var msg string
	var reason provisioningv1alpha1.ConditionReason

	progressing := ctlrutils.GetCVCondition(cv, configv1.OperatorProgressing)
	if progressing != nil && progressing.Status == configv1.ConditionTrue {
		reason = provisioningv1alpha1.CRconditionReasons.InProgress
		msg = fmt.Sprintf("Upgrading to version %s: %s", targetVersion, progressing.Message)
	} else {
		reason = provisioningv1alpha1.CRconditionReasons.Unknown
		msg = fmt.Sprintf("Upgrading to version %s: CVO stalled", targetVersion)
		failing := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionFailing)
		if failing != nil && failing.Status == configv1.ConditionTrue {
			msg = fmt.Sprintf("Upgrading to version %s: %s", targetVersion, failing.Message)
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
	targetVersion string,
) (ctrl.Result, error) {
	// Always merge+validate — catches invalid input including updated PR params.
	cvSpec, err := t.prepareCVSpec(clusterTemplate, targetVersion)
	if err != nil {
		if !typederrors.IsInputError(err) {
			return ctrl.Result{}, fmt.Errorf("failed to prepare CV upgrade spec: %w", err)
		}
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
		); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check Upgradeable condition for minor version upgrades but not if force is set.
	if !cvSpec.DesiredUpdate.Force {
		currentVersion := ctlrutils.GetCurrentCVVersion(cv)
		isMinor, err := ctlrutils.IsMinorUpgrade(currentVersion, targetVersion)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to determine upgrade type: %w", err)
		}
		if isMinor {
			upgradeable := ctlrutils.GetCVCondition(cv, configv1.OperatorUpgradeable)
			if upgradeable != nil && upgradeable.Status == configv1.ConditionFalse {
				if err := t.updateUpgradeStatus(ctx,
					provisioningv1alpha1.CRconditionReasons.Pending, upgradeable.Message,
				); err != nil {
					return ctrl.Result{}, err
				}
				return requeueWithMediumInterval(), nil
			}
		}
	}

	// Graph preconditions: patch channel/upstream, check RetrievedUpdates,
	// verify target in availableUpdates.
	if result, proceed, err := t.checkCVGraphPreconditions(
		ctx, spokeClient, cv, cvSpec,
	); result.RequeueAfter > 0 || !proceed || err != nil {
		return result, err
	}

	// Apply desiredUpdate. Returns whether the spec actually changed.
	changed, err := ctlrutils.TriggerCVUpgrade(ctx, spokeClient, t.logger, cv, cvSpec.DesiredUpdate)
	if err != nil {
		if errors.IsInvalid(err) || errors.IsBadRequest(err) || errors.IsForbidden(err) {
			if err := t.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
			); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to apply desiredUpdate: %w", err)
	}
	if changed {
		msg := fmt.Sprintf("Upgrade to version %s triggered. Waiting for upgrade to start", targetVersion)
		if err := t.updateUpgradeStatus(ctx, provisioningv1alpha1.CRconditionReasons.Pending, msg); err != nil {
			return ctrl.Result{}, err
		}
		return requeueWithShortInterval(), nil
	}

	// Monitor post-trigger CV conditions that may cause the CV upgrade to not start.
	// Invalid=True — CVO won't retry on invalid input, terminal.
	invalid := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionInvalid)
	if invalid != nil && invalid.Status == configv1.ConditionTrue {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, invalid.Message,
		); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// ReleaseAccepted=False — CVO retries payload loading failures, non-terminal.
	releaseAccepted := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionReleaseAccepted)
	if releaseAccepted != nil && releaseAccepted.Status != configv1.ConditionTrue {
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.Pending, releaseAccepted.Message,
		); err != nil {
			return ctrl.Result{}, err
		}
		return requeueWithMediumInterval(), nil
	}

	// Unknown reason that caused the CV upgrade to not start.
	msg := fmt.Sprintf("Upgrading to version %s: upgrade not started yet", targetVersion)
	failing := ctlrutils.GetCVCondition(cv, ctlrutils.CVConditionFailing)
	if failing != nil && failing.Status == configv1.ConditionTrue {
		msg = fmt.Sprintf("Upgrading to version %s: %s", targetVersion, failing.Message)
	}
	if err := t.updateUpgradeStatus(ctx,
		provisioningv1alpha1.CRconditionReasons.Unknown, msg,
	); err != nil {
		return ctrl.Result{}, err
	}
	return requeueWithMediumInterval(), nil
}

// checkCVGraphPreconditions patches channel/upstream, checks RetrievedUpdates,
// and verifies the target version is in availableUpdates. Returns true if all
// preconditions pass. When false, the ctrl.Result carries the requeue interval.
func (t *provisioningRequestReconcilerTask) checkCVGraphPreconditions(
	ctx context.Context, spokeClient client.Client,
	cv *configv1.ClusterVersion, cvSpec *configv1.ClusterVersionSpec,
) (ctrl.Result, bool, error) {
	patched, err := ctlrutils.PatchCVChannelUpstream(ctx, spokeClient, t.logger, cv, cvSpec)
	if err != nil {
		if errors.IsInvalid(err) || errors.IsBadRequest(err) || errors.IsForbidden(err) {
			if err := t.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error(),
			); err != nil {
				return ctrl.Result{}, false, err
			}
			return ctrl.Result{}, false, nil
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
			provisioningv1alpha1.CRconditionReasons.Pending, retrieved.Message,
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return requeueWithMediumInterval(), false, nil
	}

	// Verify target version is in availableUpdates (only when image is not set).
	if cvSpec.DesiredUpdate.Image == "" &&
		!ctlrutils.IsCVUpdateAvailable(cv, cvSpec.DesiredUpdate.Version) {
		msg := fmt.Sprintf("Target version %s is not available for upgrade", cvSpec.DesiredUpdate.Version)
		if err := t.updateUpgradeStatus(ctx,
			provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, msg,
		); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, false, nil
	}

	return ctrl.Result{}, true, nil
}

// updateUpgradeStatus persists the UpgradeCompleted condition and provisioning
// state based on the given reason:
//   - Terminal success (Completed): condition=True, clears startAt
//   - Terminal failure (PreconditionChecksFailed, Failed, TimedOut):
//     condition=False, provisioningState=Failed, clears startAt
//   - Non-terminal (Pending, InProgress, Unknown):
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
		t.clearUpgradeStartTime()
	case provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
		provisioningv1alpha1.CRconditionReasons.Failed,
		provisioningv1alpha1.CRconditionReasons.TimedOut:
		ctlrutils.SetProvisioningStateFailed(t.object, message)
		t.clearUpgradeStartTime()
	default:
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

	if t.object.Status.Extensions.ClusterDetails == nil || t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt == nil {
		return false, nil
	}

	if !ctlrutils.TimeoutExceeded(
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt.Time,
		t.timeouts.clusterUpgrade) {
		return false, nil
	}

	t.logger.InfoContext(ctx, "Upgrade timed out", slog.String("clusterName", clusterName))
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

	t.clearUpgradeStartTime()
	meta.RemoveStatusCondition(&t.object.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
	}
	return nil
}

// clearUpgradeStartTime clears the upgrade start timestamp.
func (t *provisioningRequestReconcilerTask) clearUpgradeStartTime() {
	if t.object.Status.Extensions.ClusterDetails != nil {
		t.object.Status.Extensions.ClusterDetails.ClusterUpgradeStartAt = nil
	}
}
