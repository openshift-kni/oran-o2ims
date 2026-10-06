/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/validation"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getEnvOrDefault(name, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}

// validateHwMgmtHwProfiles validates that hwProfile values in the ProvisioningRequest's
// hwMgmtParameters.nodeGroupData reference existing HardwareProfile CRs.
// This provides early feedback at admission time for user-supplied profile names.
func validateHwMgmtHwProfiles(ctx context.Context, c client.Client,
	r *provisioningv1alpha1.ProvisioningRequest) error {

	if r.Spec.TemplateParameters.Raw == nil {
		return nil
	}

	var params map[string]any
	if err := json.Unmarshal(r.Spec.TemplateParameters.Raw, &params); err != nil {
		return nil
	}

	hwMgmtParams, ok := params[constants.TemplateParamHwMgmt]
	if !ok {
		return nil
	}
	hwMgmtMap, ok := hwMgmtParams.(map[string]any)
	if !ok {
		return nil
	}

	nodeGroupData, ok := hwMgmtMap["nodeGroupData"]
	if !ok {
		return nil
	}
	ngSlice, ok := nodeGroupData.([]any)
	if !ok {
		return nil
	}

	hwmgmtNS := getEnvOrDefault("OCLOUD_MANAGER_NAMESPACE", "oran-o2ims")
	for _, ng := range ngSlice {
		ngMap, ok := ng.(map[string]any)
		if !ok {
			continue
		}
		hwProfile, ok := ngMap["hwProfile"].(string)
		if !ok || hwProfile == "" {
			continue
		}
		name, _ := ngMap["name"].(string)

		hwProfileObj := &hwmgmtv1alpha1.HardwareProfile{}
		if err := c.Get(ctx, client.ObjectKey{Name: hwProfile, Namespace: hwmgmtNS}, hwProfileObj); err != nil {
			if k8serrors.IsNotFound(err) {
				return fmt.Errorf("hardwareProfile %q referenced by nodeGroup %q does not exist", hwProfile, name)
			}
			return fmt.Errorf("failed to get HardwareProfile %q for nodeGroup %q: %w", hwProfile, name, err)
		}
	}

	return nil
}

// ValidateUpgradeInput validates upgrade business rules on the
// ProvisioningRequest's upgradeParameters against the ClusterTemplate's
// release version. Schema validation is handled separately; this covers
// semantic constraints that schema cannot express.
func validateUpgradeInput(r *provisioningv1alpha1.ProvisioningRequest, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {
	if r.Spec.TemplateParameters.Raw == nil {
		return nil
	}

	upgradeParamsRaw, err := validation.ExtractMatchingInput(r.Spec.TemplateParameters.Raw, constants.TemplateParamUpgrade)
	if err != nil {
		return nil
	}
	upgradeParams, ok := upgradeParamsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("spec.templateParameters.%s must be an object", constants.TemplateParamUpgrade)
	}

	if err := validation.ValidateCVUpgradeData(upgradeParams, clusterTemplate.Spec.Release, constants.TemplateParamUpgrade); err != nil {
		return fmt.Errorf("spec.templateParameters.%s: %w", constants.TemplateParamUpgrade, err)
	}

	return nil
}

// validateActiveUpgradeUpdate freezes upgrade inputs once the upgrade is active,
// except for timeout changes and forward Custom stage authorization while waiting
// between stages.
func validateActiveUpgradeUpdate(
	oldPr, newPr *provisioningv1alpha1.ProvisioningRequest, clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) error {
	upgradeCond := meta.FindStatusCondition(
		oldPr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
	if upgradeCond == nil ||
		(upgradeCond.Reason != string(provisioningv1alpha1.CRconditionReasons.InProgress) &&
			upgradeCond.Reason != string(provisioningv1alpha1.CRconditionReasons.Unknown) &&
			upgradeCond.Reason != string(provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization)) {
		return nil
	}

	if oldPr.Spec.TemplateName != newPr.Spec.TemplateName ||
		oldPr.Spec.TemplateVersion != newPr.Spec.TemplateVersion {
		return fmt.Errorf("switching ClusterTemplate is not allowed while a cluster upgrade is active")
	}

	oldUpgradeInput, err := activeUpgradeInput(oldPr.Spec.TemplateParameters.Raw)
	if err != nil {
		return err
	}
	newUpgradeInput, err := activeUpgradeInput(newPr.Spec.TemplateParameters.Raw)
	if err != nil {
		return err
	}
	defaultsRaw := clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults.Raw
	oldUpgradeThrough, err := provisioning.RequestedWorkerPoolUpgradeThrough(defaultsRaw, oldPr.Spec.TemplateParameters.Raw)
	if err != nil {
		return fmt.Errorf("failed to resolve previous worker-pool stage authorization: %w", err)
	}
	newUpgradeThrough, err := provisioning.RequestedWorkerPoolUpgradeThrough(defaultsRaw, newPr.Spec.TemplateParameters.Raw)
	if err != nil {
		return fmt.Errorf("failed to resolve requested worker-pool stage authorization: %w", err)
	}
	if !equality.Semantic.DeepEqual(oldUpgradeInput, newUpgradeInput) {
		return fmt.Errorf(
			"upgradeParameters cannot be changed while a cluster upgrade is active, except for clusterVersion.clusterUpgradeTimeout and forward clusterVersion.workerPoolUpgrade.upgradeThrough changes while awaiting stage authorization")
	}
	if upgradeCond.Reason != string(provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization) {
		if newUpgradeThrough != oldUpgradeThrough {
			return fmt.Errorf("clusterVersion.workerPoolUpgrade.upgradeThrough can only change while awaiting stage authorization")
		}
		return nil
	}

	upgradeStatus := oldPr.Status.Extensions.ClusterDetails
	if upgradeStatus == nil || upgradeStatus.ClusterUpgradeStatus == nil ||
		upgradeStatus.ClusterUpgradeStatus.WorkerPoolUpgrade == nil {
		return nil
	}
	workerStatus := upgradeStatus.ClusterUpgradeStatus.WorkerPoolUpgrade
	if workerStatus.Strategy != constants.WorkerPoolUpgradeStrategyCustom {
		return nil
	}
	// An accepted PR edit may be newer than status; honor both cursors to reject rapid reversals.
	currentIndex := provisioning.StageIndex(workerStatus, workerStatus.UpgradeThrough)
	oldIndex := provisioning.StageIndex(workerStatus, oldUpgradeThrough)
	currentUpgradeThrough := workerStatus.UpgradeThrough
	if oldIndex > currentIndex {
		currentIndex = oldIndex
		currentUpgradeThrough = oldUpgradeThrough
	}
	requestedIndex := provisioning.StageIndex(workerStatus, newUpgradeThrough)
	if newUpgradeThrough != "" && requestedIndex < 0 {
		return fmt.Errorf("clusterVersion.workerPoolUpgrade.upgradeThrough refers to unknown stage %q", newUpgradeThrough)
	}
	if requestedIndex < currentIndex {
		return fmt.Errorf(
			"clusterVersion.workerPoolUpgrade.upgradeThrough cannot move backwards from %q to %q",
			currentUpgradeThrough, newUpgradeThrough)
	}
	return nil
}

// activeUpgradeInput removes the two mutable upgrade fields from raw PR input
// before comparing an old and new request.
func activeUpgradeInput(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	params := make(map[string]any)
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("failed to decode templateParameters for active upgrade validation: %w", err)
	}
	if params == nil {
		return nil, fmt.Errorf("templateParameters must be an object")
	}
	upgradeValue, found := params[constants.TemplateParamUpgrade]
	if !found {
		return map[string]any{}, nil
	}
	upgradeParameters, ok := upgradeValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("templateParameters.%s must be an object", constants.TemplateParamUpgrade)
	}
	cvValue, found := upgradeParameters["clusterVersion"]
	if !found {
		return upgradeParameters, nil
	}
	cvParameters, ok := cvValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("templateParameters.%s.clusterVersion must be an object", constants.TemplateParamUpgrade)
	}
	if workerValue, found := cvParameters["workerPoolUpgrade"]; found {
		workerPoolUpgrade, ok := workerValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("templateParameters.%s.clusterVersion.workerPoolUpgrade must be an object", constants.TemplateParamUpgrade)
		}
		delete(workerPoolUpgrade, "upgradeThrough")
		if len(workerPoolUpgrade) == 0 {
			delete(cvParameters, "workerPoolUpgrade")
		}
	}
	delete(cvParameters, "clusterUpgradeTimeout")
	if len(cvParameters) == 0 {
		delete(upgradeParameters, "clusterVersion")
	}
	if len(upgradeParameters) == 0 {
		return map[string]any{}, nil
	}
	return upgradeParameters, nil
}
