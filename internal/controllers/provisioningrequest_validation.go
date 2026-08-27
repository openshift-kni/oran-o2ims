/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	clustervalidation "github.com/openshift-kni/oran-o2ims/internal/validation"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validateProvisioningRequestCR validates the ProvisioningRequest CR
func (t *provisioningRequestReconcilerTask) validateProvisioningRequestCR(ctx context.Context) error {
	// Check the referenced cluster template is present and valid
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return typederrors.NewInputError("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}
	t.ctDetails = &clusterTemplateDetails{
		namespace: clusterTemplate.Namespace,
		templates: clusterTemplate.Spec.TemplateDefaults,
	}

	if err = t.validateAndLoadTimeouts(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to load timeouts: %w", err)
	}

	if err = t.object.ValidateTemplateInputMatchesSchema(clusterTemplate); err != nil {
		return typederrors.NewInputError("%s", err.Error())
	}

	if err = t.validateClusterInstanceInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate ClusterInstance input: %w", err)
	}

	if err = t.validateClusterName(ctx); err != nil {
		return fmt.Errorf("failed to validate clusterName: %w", err)
	}

	if err = t.validatePolicyTemplateInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate PolicyTemplate input: %w", err)
	}

	if err = t.validateAndMergeHwMgmtInput(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate hwMgmt input: %w", err)
	}

	// TODO: Verify that ClusterInstance is per ClusterRequest basis.
	//       There should not be multiple ClusterRequests for the same ClusterInstance.
	return nil
}

// validateAndLoadTimeouts validates and loads timeout values from configmaps for
// hardware provisioning, cluster provisioning, and configuration into timeouts variable.
// If a timeout is not defined in the configmap, the default timeout value is used.
func (t *provisioningRequestReconcilerTask) validateAndLoadTimeouts(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {
	// Initialize with default timeouts
	t.timeouts.clusterProvisioning = ctlrutils.DefaultClusterInstallationTimeout
	t.timeouts.hardwareProvisioning = ctlrutils.DefaultHardwareProvisioningTimeout
	t.timeouts.clusterConfiguration = ctlrutils.DefaultClusterConfigurationTimeout

	// Hardware provisioning timeout is loaded after hwMgmt merge in validateAndMergeHwMgmtInput

	// Load cluster provisioning timeout if exists.
	ciCmName := clusterTemplate.Spec.TemplateDefaults.ClusterInstanceDefaults
	ciCm, err := ctlrutils.GetConfigmap(
		ctx, t.client, ciCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ciCmName, err)
	}
	ciTimeout, err := ctlrutils.ExtractTimeoutFromConfigMap(
		ciCm, ctlrutils.ClusterInstallationTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for cluster provisioning: %w", err)
	}
	if ciTimeout != 0 {
		t.timeouts.clusterProvisioning = ciTimeout
	}

	// Load configuration timeout if exists.
	ptCmName := clusterTemplate.Spec.TemplateDefaults.PolicyTemplateDefaults
	ptCm, err := ctlrutils.GetConfigmap(
		ctx, t.client, ptCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ptCmName, err)
	}
	ptTimeout, err := ctlrutils.ExtractTimeoutFromConfigMap(
		ptCm, ctlrutils.ClusterConfigurationTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for cluster configuration: %w", err)
	}
	if ptTimeout != 0 {
		t.timeouts.clusterConfiguration = ptTimeout
	}
	return nil
}

// validateClusterInstanceInputMatchesSchema validates that the ClusterInstance input
// from the ProvisioningRequest matches the schema defined in the ClusterTemplate.
// If valid, the merged ClusterInstance data is stored in the clusterInput.
func (t *provisioningRequestReconcilerTask) validateClusterInstanceInputMatchesSchema(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	clusterInstanceMatchingInput, err := t.object.ValidateClusterInstanceInputMatchesSchema(clusterTemplate)
	if err != nil {
		return typederrors.NewInputError(
			"the provided %s does not match the schema from ClusterTemplate (%s): %w",
			constants.TemplateParamClusterInstance, clusterTemplate.Name, err)
	}
	clusterInstanceMatchingInputMap := clusterInstanceMatchingInput.(map[string]any)

	// Get the merged ClusterInstance input data
	mergedClusterInstanceData, err := t.getMergedClusterInstanceData(
		ctx, clusterTemplate.Spec.TemplateDefaults.ClusterInstanceDefaults,
		clusterInstanceMatchingInputMap)
	if err != nil {
		return err
	}

	t.clusterInput.clusterInstanceData = mergedClusterInstanceData
	return nil
}

// validateClusterName validates that the merged clusterName value is a valid
// DNS-1123 label, does not conflict with reserved system namespaces, and is
// not already in use by another ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) validateClusterName(ctx context.Context) error {
	clusterName, ok := t.clusterInput.clusterInstanceData["clusterName"].(string)
	if !ok || clusterName == "" {
		return typederrors.NewInputError("clusterName is required and must be a non-empty string")
	}

	if err := clustervalidation.ValidateClusterNameFormat(clusterName); err != nil {
		return fmt.Errorf("invalid clusterName format: %w", err)
	}

	if err := clustervalidation.ValidateClusterNameNotReserved(clusterName); err != nil {
		return fmt.Errorf("reserved clusterName: %w", err)
	}

	if err := clustervalidation.ValidateClusterNameOwnership(ctx, t.client, clusterName, t.object.Name,
		provisioningv1alpha1.ProvisioningRequestNameLabel); err != nil {
		return fmt.Errorf("clusterName ownership check failed: %w", err)
	}

	return nil
}

// validatePolicyTemplateInputMatchesSchema validates that the merged PolicyTemplate input
// (from both the ProvisioningRequest and the default configmap) matches the schema defined
// in the ClusterTemplate. If valid, the merged PolicyTemplate data is stored in clusterInput.
func (t *provisioningRequestReconcilerTask) validatePolicyTemplateInputMatchesSchema(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	// Get the subschema for PolicyTemplateParameters
	policyTemplateSubSchema, err := provisioningv1alpha1.ExtractSubSchema(
		clusterTemplate.Spec.TemplateParameterSchema.Raw, constants.TemplateParamPolicyConfig)
	if err != nil {
		return typederrors.NewInputError(
			"failed to extract %s subschema: %s", constants.TemplateParamPolicyConfig, err.Error())
	}
	// Get the matching input for PolicyTemplateParameters
	policyTemplateMatchingInput, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, constants.TemplateParamPolicyConfig)
	if err != nil {
		return typederrors.NewInputError(
			"failed to extract matching input for subschema %s: %w", constants.TemplateParamPolicyConfig, err)
	}
	policyTemplateMatchingInputMap := policyTemplateMatchingInput.(map[string]any)

	// Retrieve the ConfigMap holding the PolicyTemplate default data and merge the
	// ProvisioningRequest input over it.
	policyTemplateDefaultsCm := clusterTemplate.Spec.TemplateDefaults.PolicyTemplateDefaults
	templateCm, err := ctlrutils.GetConfigmap(ctx, t.client, policyTemplateDefaultsCm, t.ctDetails.namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", policyTemplateDefaultsCm, err)
	}
	policyTemplateDefaults, err := ctlrutils.ExtractTemplateDataFromConfigMap[map[string]any](
		templateCm, ctlrutils.PolicyTemplateDefaultsConfigmapKey)
	if err != nil {
		return fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", policyTemplateDefaultsCm, err)
	}

	mergedPolicyTemplateData, err := mergeClusterTemplateInputWithDefaults(
		policyTemplateMatchingInputMap, policyTemplateDefaults, nil)
	if err != nil {
		return typederrors.NewInputError(
			"failed to merge data for %s: %s", constants.TemplateParamPolicyConfig, err.Error())
	}
	t.logger.InfoContext(ctx,
		fmt.Sprintf("Merged the PolicyTemplate default data with the %s for ProvisioningRequest", constants.TemplateParamPolicyConfig),
		slog.String("name", t.object.Name))

	// Validate the merged PolicyTemplate input data matches the schema
	err = provisioningv1alpha1.ValidateJsonAgainstJsonSchema(
		policyTemplateSubSchema, mergedPolicyTemplateData)
	if err != nil {
		return typederrors.NewInputError(
			"spec.templateParameters.%s does not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema.%s: %w",
			constants.TemplateParamPolicyConfig, clusterTemplate.Name, constants.TemplateParamPolicyConfig, err)
	}

	t.clusterInput.policyTemplateData = mergedPolicyTemplateData
	return nil
}

// validateAndMergeHwMgmtInput converts the inline hwMgmtDefaults to a map, extracts any
// hwMgmtParameters from the ProvisioningRequest, performs a name-keyed merge of
// nodeGroupData, and stores the merged result. It also loads the hardware provisioning
// timeout from the merged data.
func (t *provisioningRequestReconcilerTask) validateAndMergeHwMgmtInput(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	// Convert the inline hwMgmtDefaults struct to map[string]any for merging
	hwMgmtDefaults := hwMgmtDefaultsToMap(clusterTemplate.Spec.TemplateDefaults.HwMgmtDefaults)

	// Start with defaults
	mergedData := maps.Clone(hwMgmtDefaults)

	// Extract hwMgmtParameters from ProvisioningRequest if present.
	// ExtractMatchingInput returns an error both for unmarshal failures and missing keys.
	// Missing key is expected (no overrides); unmarshal failure is a real input error.
	hwMgmtParams, extractErr := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, constants.TemplateParamHwMgmt)
	if extractErr != nil && strings.Contains(extractErr.Error(), "failed to unmarshal") {
		return typederrors.NewInputError("failed to extract %s from templateParameters: %s",
			constants.TemplateParamHwMgmt, extractErr.Error())
	}
	if hwMgmtParams != nil {
		if !provisioningv1alpha1.SchemaDefinesHwMgmtParameters(clusterTemplate) {
			return typederrors.NewInputError(
				"templateParameters.%s is not defined in ClusterTemplate %q spec.templateParameterSchema",
				constants.TemplateParamHwMgmt, clusterTemplate.Name)
		}

		// Validate the raw hwMgmtParameters input against the CT's hwMgmt subschema
		hwMgmtSubSchema, err := provisioningv1alpha1.ExtractSubSchema(
			clusterTemplate.Spec.TemplateParameterSchema.Raw, constants.TemplateParamHwMgmt)
		if err == nil {
			if err := provisioningv1alpha1.ValidateJsonAgainstJsonSchema(hwMgmtSubSchema, hwMgmtParams); err != nil {
				return typederrors.NewInputError(
					"templateParameters.%s does not match the schema defined in ClusterTemplate (%s): %s",
					constants.TemplateParamHwMgmt, clusterTemplate.Name, err.Error())
			}
		}

		hwMgmtParamsMap, ok := hwMgmtParams.(map[string]any)
		if !ok {
			return typederrors.NewInputError("templateParameters.%s must be an object", constants.TemplateParamHwMgmt)
		}

		// Merge hwMgmtParameters over defaults. nodeGroupData is matched by name.
		// DeepMergeMaps treats defaults as destination and ProvisioningRequest input as source.
		if err := ctlrutils.DeepMergeMaps(mergedData, hwMgmtParamsMap, false,
			ctlrutils.SliceMergeRules{ctlrutils.HwMgmtNodeGroupDataKey: {Key: "name"}},
		); err != nil {
			return typederrors.NewInputError(
				"failed to merge ProvisioningRequest templateParameters.%s over ClusterTemplate hwMgmtDefaults: %s",
				constants.TemplateParamHwMgmt, err.Error())
		}
	}

	t.clusterInput.hwMgmtData = mergedData

	// Validate merged nodeGroupData: presence, non-empty, name/role constraints
	if err := validateMergedNodeGroups(mergedData); err != nil {
		return err
	}

	// Validate that referenced HardwareProfile CRs exist in the merged data
	if err := t.validateMergedHwProfiles(ctx, mergedData); err != nil {
		return err
	}

	// Load hardware provisioning timeout from the merged data
	if timeoutStr, ok := mergedData["hardwareProvisioningTimeout"].(string); ok && timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return typederrors.NewInputError(
				"hardwareProvisioningTimeout %q is not a valid duration: %s", timeoutStr, err.Error())
		}
		if timeout <= 0 {
			return typederrors.NewInputError(
				"hardwareProvisioningTimeout %q must be a positive duration", timeoutStr)
		}
		t.timeouts.hardwareProvisioning = timeout
	}

	t.logger.InfoContext(ctx,
		"Merged hwMgmt default data with hwMgmtParameters for ProvisioningRequest",
		slog.String("name", t.object.Name),
	)
	return nil
}

// validateMergedNodeGroups checks that the merged nodeGroupData entries have valid
// name and role values. Selector fields (hwProfile, resourcePoolId, resourceSelector) are optional.
func validateMergedNodeGroups(mergedData map[string]any) error {
	ngRaw, ok := mergedData[ctlrutils.HwMgmtNodeGroupDataKey]
	if !ok {
		return typederrors.NewInputError(
			"%s is required: provide it via hwMgmtDefaults in the ClusterTemplate "+
				"or hwMgmtParameters in the ProvisioningRequest", ctlrutils.HwMgmtNodeGroupDataKey)
	}
	ngSlice, ok := ngRaw.([]any)
	if !ok {
		return typederrors.NewInputError("%s must be an array", ctlrutils.HwMgmtNodeGroupDataKey)
	}
	if len(ngSlice) == 0 {
		return typederrors.NewInputError(
			"%s must not be empty: provide at least one node group via hwMgmtDefaults "+
				"or hwMgmtParameters", ctlrutils.HwMgmtNodeGroupDataKey)
	}

	var masterGroup string
	for _, ng := range ngSlice {
		ngMap, ok := ng.(map[string]any)
		if !ok {
			return typederrors.NewInputError("%s element is not a map", ctlrutils.HwMgmtNodeGroupDataKey)
		}
		name, _ := ngMap["name"].(string)
		if name == "" {
			return typederrors.NewInputError("%s element is missing required field 'name'", ctlrutils.HwMgmtNodeGroupDataKey)
		}
		role, _ := ngMap["role"].(string)
		if role == "" {
			return typederrors.NewInputError("no role specified for nodeGroup %q", name)
		}
		if role != "master" && role != "worker" {
			return typederrors.NewInputError("invalid role %q for nodeGroup %q: must be 'master' or 'worker'", role, name)
		}
		if role == "master" {
			if masterGroup != "" {
				return typederrors.NewInputError("duplicate role %q in %s for groups %q and %q", role, ctlrutils.HwMgmtNodeGroupDataKey, masterGroup, name)
			}
			masterGroup = name
		}

		// hwProfile, resourcePoolId, and resourceSelector are all optional.
		// The hardware manager handles node selection based on whatever criteria are provided.
		// Type validation for resourceSelector is handled by schema validation in validateAndMergeHwMgmtInput.
	}

	return nil
}

// validateMergedHwProfiles checks that hwProfile values in the merged nodeGroupData
// reference existing HardwareProfile CRs.
func (t *provisioningRequestReconcilerTask) validateMergedHwProfiles(ctx context.Context, mergedData map[string]any) error {
	ngRaw, ok := mergedData[ctlrutils.HwMgmtNodeGroupDataKey]
	if !ok {
		return nil
	}
	ngSlice, ok := ngRaw.([]any)
	if !ok {
		return nil
	}

	hwProfileNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
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
		if err := t.client.Get(ctx, client.ObjectKey{Name: hwProfile, Namespace: hwProfileNS}, hwProfileObj); err != nil {
			if k8serrors.IsNotFound(err) {
				return typederrors.NewInputError("HardwareProfile %q referenced by nodeGroup %q does not exist", hwProfile, name)
			}
			return fmt.Errorf("failed to get HardwareProfile %q for nodeGroup %q: %w", hwProfile, name, err)
		}
	}

	return nil
}

// hwMgmtDefaultsToMap converts the inline HwMgmtDefaults struct to a map[string]any
// for use with the deep merge functions.
func hwMgmtDefaultsToMap(defaults provisioningv1alpha1.HwMgmtDefaults) map[string]any {
	result := make(map[string]any)

	if defaults.HardwareProvisioningTimeout != nil {
		result["hardwareProvisioningTimeout"] = defaults.HardwareProvisioningTimeout.Duration.String()
	}

	if len(defaults.NodeGroupData) > 0 {
		ngSlice := make([]any, len(defaults.NodeGroupData))
		for i, ng := range defaults.NodeGroupData {
			ngMap := map[string]any{
				"name": ng.Name,
				"role": ng.Role,
			}
			if ng.HwProfile != "" {
				ngMap["hwProfile"] = ng.HwProfile
			}
			if ng.ResourcePoolId != "" {
				ngMap["resourcePoolId"] = ng.ResourcePoolId
			}
			if len(ng.ResourceSelector) > 0 {
				rs := make(map[string]any, len(ng.ResourceSelector))
				for k, v := range ng.ResourceSelector {
					rs[k] = v
				}
				ngMap["resourceSelector"] = rs
			}
			ngSlice[i] = ngMap
		}
		result[ctlrutils.HwMgmtNodeGroupDataKey] = ngSlice
	}

	return result
}

// getMergedClusterInstanceData retrieves the ClusterInstance default data from the
// referenced ConfigMap and merges the ProvisioningRequest clusterInstanceParameters
// over it. extraLabels/extraAnnotations common to both are forced to the default
// values. nodeGroups-format input is merged by group name and expanded into the flat
// nodes[] shape the rest of the pipeline consumes; legacy flat nodes use an
// index-based deep merge.
func (t *provisioningRequestReconcilerTask) getMergedClusterInstanceData(
	ctx context.Context, templateDefaultsCm string, clusterInstanceInput map[string]any) (map[string]any, error) {

	// Retrieve the configmap that holds the ClusterInstance default data.
	templateCm, err := ctlrutils.GetConfigmap(ctx, t.client, templateDefaultsCm, t.ctDetails.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", templateDefaultsCm, err)
	}
	clusterInstanceDefaults, err := ctlrutils.ExtractTemplateDataFromConfigMap[map[string]any](
		templateCm, ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", templateDefaultsCm, err)
	}

	// Special handling for overrides of ClusterInstance's extraLabels and extraAnnotations.
	// The clusterInstanceInput values are overridden with the values from the default
	// configmap if the same labels/annotations exist in both.
	if err := t.overrideClusterInstanceLabelsOrAnnotations(
		clusterInstanceInput, clusterInstanceDefaults); err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
	}

	format, err := resolveClusterInstanceFormat(clusterInstanceInput, clusterInstanceDefaults)
	if err != nil {
		return nil, typederrors.NewInputError("%s", err.Error())
	}

	var rules ctlrutils.SliceMergeRules
	if format == constants.ClusterInstanceNodeGroupsKey {
		rules = ctlrutils.SliceMergeRules{
			constants.ClusterInstanceNodeGroupsKey: {
				Key:   "name",
				ByKey: ctlrutils.ByKeyMergeOptions{RejectUnmatchedSrc: true},
			},
			// Kept for consistency with per-node expand: intended PR schema doesn't
			// overlay nodeNetwork.interfaces at group-level; this path is unused for
			// schema-compliant input.
			constants.ClusterInstanceNodeGroupsKey + "[].nodeNetwork.interfaces": {
				Key: "name",
				ByKey: ctlrutils.ByKeyMergeOptions{
					AllowDuplicateKeys: true,
					RejectUnmatchedSrc: true,
				},
			},
		}
	}

	mergedClusterDataMap, err := mergeClusterTemplateInputWithDefaults(
		clusterInstanceInput, clusterInstanceDefaults, rules)
	if err != nil {
		return nil, typederrors.NewInputError(
			"failed to merge data for %s: %s", constants.TemplateParamClusterInstance, err.Error())
	}

	if format == constants.ClusterInstanceNodeGroupsKey {
		// Expand nodeGroups to flat nodes and capture hostName -> group name
		// before expansion strips nodeGroups[].name.
		hostToGroupName, err := ctlrutils.ExpandNodeGroupsToNodes(mergedClusterDataMap)
		if err != nil {
			return nil, typederrors.NewInputError(
				"failed to expand %s nodeGroups: %s",
				constants.TemplateParamClusterInstance, err.Error())
		}
		t.clusterInput.hostToGroupName = hostToGroupName
	}

	t.logger.InfoContext(ctx,
		fmt.Sprintf("Merged the ClusterInstance default data with the %s for ProvisioningRequest", constants.TemplateParamClusterInstance),
		slog.String("name", t.object.Name),
	)
	return mergedClusterDataMap, nil
}

// resolveClusterInstanceFormat returns the nodes-list format when both sides
// exclusively use the same key (nodeGroups or nodes). Mixed or incomplete
// combinations are rejected.
func resolveClusterInstanceFormat(input, defaults map[string]any) (string, error) {
	_, inputHasNodeGroups := input[constants.ClusterInstanceNodeGroupsKey]
	_, defaultsHasNodeGroups := defaults[constants.ClusterInstanceNodeGroupsKey]
	_, inputHasNodes := input[constants.ClusterInstanceNodesKey]
	_, defaultsHasNodes := defaults[constants.ClusterInstanceNodesKey]

	switch {
	case inputHasNodeGroups && defaultsHasNodeGroups && !inputHasNodes && !defaultsHasNodes:
		return constants.ClusterInstanceNodeGroupsKey, nil
	case inputHasNodes && defaultsHasNodes && !inputHasNodeGroups && !defaultsHasNodeGroups:
		return constants.ClusterInstanceNodesKey, nil
	default:
		//nolint:revive // string-format: capitalize CR name ProvisioningRequest
		return "", fmt.Errorf(
			"ProvisioningRequest %s and ClusterTemplate defaults must both use %q or both use %q",
			constants.TemplateParamClusterInstance, constants.ClusterInstanceNodeGroupsKey, constants.ClusterInstanceNodesKey)
	}
}

// mergeClusterTemplateInputWithDefaults merges the cluster template input with the default data.
// rules controls how nested slices are merged (nil = by-index); see DeepMergeMaps.
func mergeClusterTemplateInputWithDefaults(
	clusterTemplateInput, clusterTemplateInputDefaults map[string]any,
	rules ctlrutils.SliceMergeRules,
) (map[string]any, error) {
	// Initialize a map to hold the merged data
	var mergedClusterData map[string]any

	switch {
	case len(clusterTemplateInputDefaults) != 0 && len(clusterTemplateInput) != 0:
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)

		checkType := false
		// DeepMergeMaps treats defaults as destination and ProvisioningRequest input as source.
		err := ctlrutils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, checkType, rules)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to merge ProvisioningRequest clusterTemplateInput over ClusterTemplate defaults: %w", err)
		}
	case len(clusterTemplateInputDefaults) == 0 && len(clusterTemplateInput) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInput)
	case len(clusterTemplateInput) == 0 && len(clusterTemplateInputDefaults) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)
	default:
		return nil, fmt.Errorf("expected clusterTemplateInput data not provided in either ProvisioningRequest or Configmap")
	}

	return mergedClusterData, nil
}

// overrideClusterInstanceLabelsOrAnnotations handles the overrides of ClusterInstance's extraLabels
// or extraAnnotations. It overrides the values in the ProvisioningRequest with those from the default
// configmap when the same labels/annotations exist in both. Labels/annotations that are not common
// between the default configmap and ProvisioningRequest are ignored.
func (t *provisioningRequestReconcilerTask) overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap map[string]any) error {
	fields := []string{"extraLabels", "extraAnnotations"}

	for _, field := range fields {
		srcValue, existsSrc := srcConfigmap[field]
		dstValue, existsDst := dstProvisioningRequestInput[field]
		// Check only when both configmap and ProvisioningRequestInput contain the field
		if existsSrc && existsDst {
			dstMap, okDst := dstValue.(map[string]any)
			srcMap, okSrc := srcValue.(map[string]any)
			if !okDst || !okSrc {
				return fmt.Errorf("type mismatch for field %s: (from ProvisioningRequest: %T, from default Configmap: %T)",
					field, dstValue, srcValue)
			}

			// Iterate over the resource types (e.g., ManagedCluster, AgentClusterInstall)
			// Check labels/annotations only if the resource exists in both
			for resourceType, srcFields := range srcMap {
				if dstFields, exists := dstMap[resourceType]; exists {
					dstFieldsMap, okDstFields := dstFields.(map[string]any)
					srcFieldsMap, okSrcFields := srcFields.(map[string]any)
					if !okDstFields || !okSrcFields {
						return fmt.Errorf("type mismatch for field %s: (from ProvisioningRequest: %T, from default Configmap: %T)",
							field, dstValue, srcValue)
					}

					// Override ProvisioningRequestInput's values with defaults if the label/annotation key exists in both
					for srcFieldKey, srcLabelValue := range srcFieldsMap {
						if _, exists := dstFieldsMap[srcFieldKey]; exists {
							t.logger.Info(fmt.Sprintf("%s.%s.%s found in both default configmap and clusterInstanceInput. "+
								"Overriding it in ClusterInstanceInput with value %s from the default configmap.",
								field, resourceType, srcFieldKey, srcLabelValue))
							dstFieldsMap[srcFieldKey] = srcLabelValue
						}
					}
				}
			}
		}
	}

	// Process label/annotation overrides for the flat nodes format.
	dstNodes, dstExists := dstProvisioningRequestInput["nodes"]
	srcNodes, srcExists := srcConfigmap["nodes"]
	if dstExists && srcExists {
		// Determine the minimum length to merge
		minLen := len(dstNodes.([]any))
		if len(srcNodes.([]any)) < minLen {
			minLen = len(srcNodes.([]any))
		}

		for i := 0; i < minLen; i++ {
			if err := t.overrideClusterInstanceLabelsOrAnnotations(
				dstNodes.([]any)[i].(map[string]any),
				srcNodes.([]any)[i].(map[string]any),
			); err != nil {
				return fmt.Errorf("type mismatch for nodes: %w", err)
			}
		}
	}

	// Process label/annotation overrides for nodeGroups: match by name; group-level defaults win over PR group and node extra*.
	dstGroups, dstGroupsExists := dstProvisioningRequestInput[constants.ClusterInstanceNodeGroupsKey].([]any)
	srcGroups, srcGroupsExists := srcConfigmap[constants.ClusterInstanceNodeGroupsKey].([]any)
	if dstGroupsExists && srcGroupsExists {
		// Index the defaults groups by name.
		srcGroupsByName := make(map[string]map[string]any, len(srcGroups))
		for _, g := range srcGroups {
			if groupMap, ok := g.(map[string]any); ok {
				if name, _ := groupMap["name"].(string); name != "" {
					srcGroupsByName[name] = groupMap
				}
			}
		}

		for _, g := range dstGroups {
			dstGroup, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := dstGroup["name"].(string)
			srcGroup, ok := srcGroupsByName[name]
			if !ok {
				continue
			}

			// Group-level extra*: defaults win over the PR group-level values. The
			// defaults group carries no per-host nodes, so the flat-nodes branch above
			// is a no-op on this recursion.
			if err := t.overrideClusterInstanceLabelsOrAnnotations(dstGroup, srcGroup); err != nil {
				return fmt.Errorf("node group %q: %w", name, err)
			}

			// Node-level extra*: the same group-level defaults win over each PR node's values.
			if nodes, ok := dstGroup[constants.ClusterInstanceNodesKey].([]any); ok {
				for _, n := range nodes {
					nodeMap, ok := n.(map[string]any)
					if !ok {
						continue
					}
					if err := t.overrideClusterInstanceLabelsOrAnnotations(nodeMap, srcGroup); err != nil {
						return fmt.Errorf("node group %q node: %w", name, err)
					}
				}
			}
		}
	}

	return nil
}
