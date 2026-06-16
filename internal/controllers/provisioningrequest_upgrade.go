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

	upgradeType, err := detectUpgradeType(clusterTemplate, t.object)
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

	switch upgradeType {
	case ctlrutils.UpgradeDefaultsClusterVersionKey:
		return t.handleClusterVersionUpgrade(ctx, clusterTemplate, clusterName)
	case ctlrutils.UpgradeDefaultsIBGUKey:
		return t.handleIBGUUpgrade(ctx, clusterTemplate, clusterName)
	default:
		t.logger.ErrorContext(ctx, "Unexpected upgrade type from detectUpgradeType",
			slog.String("upgradeType", upgradeType))
		return doNotRequeue(), false, nil
	}
}

// detectUpgradeType inspects the top-level keys in the ClusterTemplate's
// upgradeDefaults and the ProvisioningRequest's upgradeParameters to determine
// the upgrade type. Returns an error if both clusterVersion and
// imageBasedGroupUpgrade keys are found.
func detectUpgradeType(
	ct *provisioningv1alpha1.ClusterTemplate,
	pr *provisioningv1alpha1.ProvisioningRequest,
) (string, error) {
	hasCV, hasIBGU := false, false

	// Check ClusterTemplate upgradeDefaults.
	if ct.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0 {
		var defaults map[string]any
		if err := json.Unmarshal(ct.Spec.TemplateDefaults.UpgradeDefaults.Raw, &defaults); err != nil {
			return "", fmt.Errorf("failed to parse upgradeDefaults: %w", err)
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
			hasCV = true
		}
		if _, ok := defaults[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
			hasIBGU = true
		}
	}

	// Check ProvisioningRequest upgradeParameters.
	if pr.Spec.TemplateParameters.Size() > 0 {
		var templateParams map[string]any
		if err := json.Unmarshal(pr.Spec.TemplateParameters.Raw, &templateParams); err != nil {
			return "", fmt.Errorf("failed to parse templateParameters: %w", err)
		}
		if upgradeParamsRaw, ok := templateParams[constants.TemplateParamUpgrade]; ok {
			upgradeParams, ok := upgradeParamsRaw.(map[string]any)
			if !ok {
				return "", fmt.Errorf("%s is not a map", constants.TemplateParamUpgrade)
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsClusterVersionKey]; ok {
				hasCV = true
			}
			if _, ok := upgradeParams[ctlrutils.UpgradeDefaultsIBGUKey]; ok {
				hasIBGU = true
			}
		}
	}

	if hasCV && hasIBGU {
		return "", fmt.Errorf(
			"upgrade configuration contains both %q and %q keys; only one upgrade type is allowed",
			ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey)
	}

	if hasCV {
		return ctlrutils.UpgradeDefaultsClusterVersionKey, nil
	}
	if hasIBGU {
		return ctlrutils.UpgradeDefaultsIBGUKey, nil
	}
	return "", fmt.Errorf(
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

// handleClusterVersionUpgrade handles upgrades via direct spoke ClusterVersion
// patching. It sets up a spoke client and reads the current ClusterVersion.
// Full upgrade logic (precondition checks, trigger, monitoring) will be added
// in a subsequent change.
func (t *provisioningRequestReconcilerTask) handleClusterVersionUpgrade(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	clusterName string,
) (ctrl.Result, bool, error) {

	managedServiceAccountName := t.object.Name + "-upgrade"
	manifestWorkName := t.object.Name + "-upgrade-rbac"

	spokeClient, ready, err := spokeclient.EnsureSpokeClient(
		ctx, t.client, t.logger, clusterName,
		managedServiceAccountName, manifestWorkName,
		upgradeRBACRules, upgradeSpokeScheme)
	if err != nil {
		if typederrors.IsInputError(err) {
			ctlrutils.SetProvisioningStateFailed(t.object, err.Error())
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
		return ctrl.Result{}, false, fmt.Errorf("failed to setup spoke client: %w", err)
	}
	if !ready {
		ctlrutils.SetProvisioningStateInProgress(t.object, "Preparing upgrade resources")
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.Pending,
			metav1.ConditionFalse,
			"Preparing upgrade resources",
		)
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", updateErr)
		}
		return requeueWithShortInterval(), false, nil
	}

	cv := &configv1.ClusterVersion{}
	if err := spokeClient.Get(ctx, types.NamespacedName{Name: ctlrutils.ClusterVersionName}, cv); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to get spoke ClusterVersion: %w", err)
	}

	currentVersion := "unknown"
	for _, h := range cv.Status.History {
		if h.State == configv1.CompletedUpdate {
			currentVersion = h.Version
			break
		}
	}

	t.logger.InfoContext(ctx, "Spoke client ready, ClusterVersion read",
		slog.String("clusterName", clusterName),
		slog.String("currentVersion", currentVersion),
		slog.String("targetRelease", clusterTemplate.Spec.Release))

	// TODO: Full upgrade logic (precondition checks, trigger, monitoring)
	// will be added in a subsequent change.
	return requeueWithMediumInterval(), false, nil
}
