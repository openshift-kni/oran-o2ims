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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validateProvisioningRequestCR validates the ProvisioningRequest CR
func (t *provisioningRequestReconcilerTask) validateProvisioningRequestCR(ctx context.Context) error {
	// Check the referenced cluster template is present and valid
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return ctlrutils.NewInputError("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}
	t.ctDetails = &clusterTemplateDetails{
		namespace: clusterTemplate.Namespace,
		templates: clusterTemplate.Spec.TemplateDefaults,
	}

	if err = t.validateAndLoadTimeouts(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to load timeouts: %w", err)
	}

	if err = t.object.ValidateTemplateInputMatchesSchema(clusterTemplate); err != nil {
		return ctlrutils.NewInputError("%s", err.Error())
	}

	if err = t.validateClusterInstanceInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate ClusterInstance input: %w", err)
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
		return ctlrutils.NewInputError(
			"the provided %s does not match the schema from ClusterTemplate (%s): %w",
			ctlrutils.TemplateParamClusterInstance, clusterTemplate.Name, err)
	}
	clusterInstanceMatchingInputMap := clusterInstanceMatchingInput.(map[string]any)

	// Get the merged ClusterInstance input data
	mergedClusterInstanceData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.TemplateDefaults.ClusterInstanceDefaults,
		clusterInstanceMatchingInputMap,
		ctlrutils.TemplateParamClusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	t.clusterInput.clusterInstanceData = mergedClusterInstanceData
	return nil
}

// validatePolicyTemplateInputMatchesSchema validates that the merged PolicyTemplate input
// (from both the ProvisioningRequest and the default configmap) matches the schema defined
// in the ClusterTemplate. If valid, the merged PolicyTemplate data is stored in clusterInput.
func (t *provisioningRequestReconcilerTask) validatePolicyTemplateInputMatchesSchema(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	// Get the subschema for PolicyTemplateParameters
	policyTemplateSubSchema, err := provisioningv1alpha1.ExtractSubSchema(
		clusterTemplate.Spec.TemplateParameterSchema.Raw, ctlrutils.TemplateParamPolicyConfig)
	if err != nil {
		return ctlrutils.NewInputError(
			"failed to extract %s subschema: %s", ctlrutils.TemplateParamPolicyConfig, err.Error())
	}
	// Get the matching input for PolicyTemplateParameters
	policyTemplateMatchingInput, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamPolicyConfig)
	if err != nil {
		return ctlrutils.NewInputError(
			"failed to extract matching input for subschema %s: %w", ctlrutils.TemplateParamPolicyConfig, err)
	}
	policyTemplateMatchingInputMap := policyTemplateMatchingInput.(map[string]any)

	// Get the merged PolicyTemplate input data
	mergedPolicyTemplateData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.TemplateDefaults.PolicyTemplateDefaults,
		policyTemplateMatchingInputMap,
		ctlrutils.TemplateParamPolicyConfig)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	// Validate the merged PolicyTemplate input data matches the schema
	err = provisioningv1alpha1.ValidateJsonAgainstJsonSchema(
		policyTemplateSubSchema, mergedPolicyTemplateData)
	if err != nil {
		return ctlrutils.NewInputError(
			"spec.templateParameters.%s does not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema.%s: %w",
			ctlrutils.TemplateParamPolicyConfig, clusterTemplate.Name, ctlrutils.TemplateParamPolicyConfig, err)
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
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamHwMgmt)
	if extractErr != nil && strings.Contains(extractErr.Error(), "failed to unmarshal") {
		return ctlrutils.NewInputError("failed to extract %s from templateParameters: %s",
			ctlrutils.TemplateParamHwMgmt, extractErr.Error())
	}
	if hwMgmtParams != nil {
		if !provisioningv1alpha1.SchemaDefinesHwMgmtParameters(clusterTemplate) {
			return ctlrutils.NewInputError(
				"templateParameters.%s is not defined in ClusterTemplate %q spec.templateParameterSchema",
				ctlrutils.TemplateParamHwMgmt, clusterTemplate.Name)
		}

		// Validate the raw hwMgmtParameters input against the CT's hwMgmt subschema
		hwMgmtSubSchema, err := provisioningv1alpha1.ExtractSubSchema(
			clusterTemplate.Spec.TemplateParameterSchema.Raw, ctlrutils.TemplateParamHwMgmt)
		if err == nil {
			if err := provisioningv1alpha1.ValidateJsonAgainstJsonSchema(hwMgmtSubSchema, hwMgmtParams); err != nil {
				return ctlrutils.NewInputError(
					"templateParameters.%s does not match the schema defined in ClusterTemplate (%s): %s",
					ctlrutils.TemplateParamHwMgmt, clusterTemplate.Name, err.Error())
			}
		}

		hwMgmtParamsMap, ok := hwMgmtParams.(map[string]any)
		if !ok {
			return ctlrutils.NewInputError("templateParameters.%s must be an object", ctlrutils.TemplateParamHwMgmt)
		}

		// Handle nodeGroupData with name-keyed merge
		srcNodeGroups, srcHasNG := hwMgmtParamsMap["nodeGroupData"]
		if srcHasNG {
			srcSlice, ok := srcNodeGroups.([]any)
			if !ok {
				return ctlrutils.NewInputError("templateParameters.%s.nodeGroupData must be an array", ctlrutils.TemplateParamHwMgmt)
			}
			dstSlice := []any{}
			if dstNodeGroups, dstHasNG := mergedData["nodeGroupData"]; dstHasNG {
				dstSlice, ok = dstNodeGroups.([]any)
				if !ok {
					return ctlrutils.NewInputError("hwMgmtDefaults nodeGroupData must be an array")
				}
			}
			mergedNG, err := ctlrutils.MergeNodeGroupData(dstSlice, srcSlice)
			if err != nil {
				return ctlrutils.NewInputError("failed to merge nodeGroupData: %s", err.Error())
			}
			mergedData["nodeGroupData"] = mergedNG
			// Remove nodeGroupData from params so DeepMergeMaps doesn't overwrite
			delete(hwMgmtParamsMap, "nodeGroupData")
		}

		// Merge remaining scalar fields (e.g., hardwareProvisioningTimeout)
		if len(hwMgmtParamsMap) > 0 {
			if err := ctlrutils.DeepMergeMaps(mergedData, hwMgmtParamsMap, false); err != nil {
				return ctlrutils.NewInputError("failed to merge hwMgmt parameters: %s", err.Error())
			}
		}
	}

	t.clusterInput.hwMgmtData = mergedData

	// Validate merged nodeGroupData constraints (name, role, selectors)
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
			return ctlrutils.NewInputError(
				"hardwareProvisioningTimeout %q is not a valid duration: %s", timeoutStr, err.Error())
		}
		if timeout <= 0 {
			return ctlrutils.NewInputError(
				"hardwareProvisioningTimeout %q must be a positive duration", timeoutStr)
		}
		t.timeouts.hardwareProvisioning = timeout
	}

	t.logger.Info(
		fmt.Sprintf("Merged hwMgmt default data with hwMgmtParameters for ProvisioningRequest"),
		slog.String("name", t.object.Name),
	)
	return nil
}

// validateMergedNodeGroups checks that the merged nodeGroupData entries have valid
// name and role values. Selector fields (hwProfile, resourcePoolId, resourceSelector) are optional.
func validateMergedNodeGroups(mergedData map[string]any) error {
	ngRaw, ok := mergedData["nodeGroupData"]
	if !ok {
		return nil
	}
	ngSlice, ok := ngRaw.([]any)
	if !ok {
		return nil
	}

	seenRoles := map[string]string{}
	for _, ng := range ngSlice {
		ngMap, ok := ng.(map[string]any)
		if !ok {
			return ctlrutils.NewInputError("nodeGroupData element is not a map")
		}
		name, _ := ngMap["name"].(string)
		if name == "" {
			return ctlrutils.NewInputError("nodeGroupData element is missing required field 'name'")
		}
		role, _ := ngMap["role"].(string)
		if role == "" {
			return ctlrutils.NewInputError("no role specified for nodeGroup %q", name)
		}
		if role != "master" && role != "worker" {
			return ctlrutils.NewInputError("invalid role %q for nodeGroup %q: must be 'master' or 'worker'", role, name)
		}
		if prev, exists := seenRoles[role]; exists {
			return ctlrutils.NewInputError("duplicate role %q in nodeGroupData for groups %q and %q", role, prev, name)
		}
		seenRoles[role] = name

		// hwProfile, resourcePoolId, and resourceSelector are all optional.
		// The hardware plugin handles node selection based on whatever criteria are provided.
		// Type validation for resourceSelector is handled by schema validation in validateAndMergeHwMgmtInput.
	}

	return nil
}

// validateMergedHwProfiles checks that hwProfile values in the merged nodeGroupData
// reference existing HardwareProfile CRs.
func (t *provisioningRequestReconcilerTask) validateMergedHwProfiles(ctx context.Context, mergedData map[string]any) error {
	ngRaw, ok := mergedData["nodeGroupData"]
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
				return ctlrutils.NewInputError("HardwareProfile %q referenced by nodeGroup %q does not exist", hwProfile, name)
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

	if defaults.HardwareProvisioningTimeout != "" {
		result["hardwareProvisioningTimeout"] = defaults.HardwareProvisioningTimeout
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
		result["nodeGroupData"] = ngSlice
	}

	return result
}

func (t *provisioningRequestReconcilerTask) getMergedClusterInputData(
	ctx context.Context, templateDefaultsCm string, clusterTemplateInput map[string]any, templateParam string) (map[string]any, error) {

	var templateDefaultsCmKey string

	switch templateParam {
	case ctlrutils.TemplateParamClusterInstance:
		templateDefaultsCmKey = ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey
	case ctlrutils.TemplateParamPolicyConfig:
		templateDefaultsCmKey = ctlrutils.PolicyTemplateDefaultsConfigmapKey
	default:
		return nil, ctlrutils.NewInputError("unsupported template parameter")
	}

	// Retrieve the configmap that holds the default data.
	templateCm, err := ctlrutils.GetConfigmap(ctx, t.client, templateDefaultsCm, t.ctDetails.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", templateDefaultsCm, err)
	}
	clusterTemplateDefaultsMap, err := ctlrutils.ExtractTemplateDataFromConfigMap[map[string]any](
		templateCm, templateDefaultsCmKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", templateDefaultsCm, err)
	}

	if templateParam == ctlrutils.TemplateParamClusterInstance {
		// Special handling for overrides of ClusterInstance's extraLabels and extraAnnotations.
		// The clusterTemplateInput will be overridden with the values from defaut configmap
		// if same labels/annotations exist in both.
		if err := t.overrideClusterInstanceLabelsOrAnnotations(
			clusterTemplateInput, clusterTemplateDefaultsMap); err != nil {
			return nil, ctlrutils.NewInputError("%s", err.Error())
		}
	}

	// Get the merged cluster data
	mergedClusterDataMap, err := mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateDefaultsMap)
	if err != nil {
		return nil, ctlrutils.NewInputError("failed to merge data for %s: %s", templateParam, err.Error())
	}

	t.logger.Info(
		fmt.Sprintf("Merged the %s default data with the clusterTemplateInput data for ProvisioningRequest", templateParam),
		slog.String("name", t.object.Name),
	)
	return mergedClusterDataMap, nil
}

// mergeClusterTemplateInputWithDefaults merges the cluster template input with the default data
func mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateInputDefaults map[string]any) (map[string]any, error) {
	// Initialize a map to hold the merged data
	var mergedClusterData map[string]any

	switch {
	case len(clusterTemplateInputDefaults) != 0 && len(clusterTemplateInput) != 0:
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)

		checkType := false
		err := ctlrutils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, checkType) // clusterTemplateInput overrides the defaults
		if err != nil {
			return nil, fmt.Errorf("failed to merge the clusterTemplateInput(src) with the defaults(dst): %w", err)
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

	// Process label/annotation overrides for nodes
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

	return nil
}
