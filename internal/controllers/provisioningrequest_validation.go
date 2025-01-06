package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// validateProvisioningRequestCR validates the ProvisioningRequest CR
func (t *provisioningRequestReconcilerTask) validateProvisioningRequestCR(ctx context.Context) error {
	// Check the referenced cluster template is present and valid
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return utils.NewInputError("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}
	t.ctDetails = &clusterTemplateDetails{
		namespace: clusterTemplate.Namespace,
		templates: clusterTemplate.Spec.Templates,
	}

	if err = t.validateAndLoadTimeouts(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to load timeouts: %w", err)
	}

	if err = t.object.ValidateTemplateInputMatchesSchema(clusterTemplate); err != nil {
		return utils.NewInputError("%s", err.Error())
	}

	if err = t.validateClusterInstanceInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate ClusterInstance input: %w", err)
	}

	if err = t.validatePolicyTemplateInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate PolicyTemplate input: %w", err)
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
	t.timeouts.clusterProvisioning = utils.DefaultClusterInstallationTimeout
	t.timeouts.hardwareProvisioning = utils.DefaultHardwareProvisioningTimeout
	t.timeouts.clusterConfiguration = utils.DefaultClusterConfigurationTimeout

	// Load hardware provisioning timeout if exists.
	if !t.isHardwareProvisionSkipped() {
		hwCmName := clusterTemplate.Spec.Templates.HwTemplate
		hwTimeout, err := utils.GetTimeoutFromHWTemplate(ctx, t.client, hwCmName)
		if err != nil {
			return fmt.Errorf("failed to get timeout from hardware template %s: %w", hwCmName, err)
		}

		if hwTimeout != 0 {
			t.timeouts.hardwareProvisioning = hwTimeout
		}
	}

	// Load cluster provisioning timeout if exists.
	ciCmName := clusterTemplate.Spec.Templates.ClusterInstanceDefaults
	ciCm, err := utils.GetConfigmap(
		ctx, t.client, ciCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ciCmName, err)
	}
	ciTimeout, err := utils.ExtractTimeoutFromConfigMap(
		ciCm, utils.ClusterInstallationTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for cluster provisioning: %w", err)
	}
	if ciTimeout != 0 {
		t.timeouts.clusterProvisioning = ciTimeout
	}

	// Load configuration timeout if exists.
	ptCmName := clusterTemplate.Spec.Templates.PolicyTemplateDefaults
	ptCm, err := utils.GetConfigmap(
		ctx, t.client, ptCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ptCmName, err)
	}
	ptTimeout, err := utils.ExtractTimeoutFromConfigMap(
		ptCm, utils.ClusterConfigurationTimeoutConfigKey)
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
		return utils.NewInputError(
			"the provided %s does not match the schema from ClusterTemplate (%s): %w",
			utils.TemplateParamClusterInstance, clusterTemplate.Name, err)
	}
	clusterInstanceMatchingInputMap := clusterInstanceMatchingInput.(map[string]any)

	// Get the merged ClusterInstance input data
	mergedClusterInstanceData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.Templates.ClusterInstanceDefaults,
		clusterInstanceMatchingInputMap,
		utils.TemplateParamClusterInstance)
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
		clusterTemplate.Spec.TemplateParameterSchema.Raw, utils.TemplateParamPolicyConfig)
	if err != nil {
		return utils.NewInputError(
			"failed to extract %s subschema: %s", utils.TemplateParamPolicyConfig, err.Error())
	}
	// Get the matching input for PolicyTemplateParameters
	policyTemplateMatchingInput, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamPolicyConfig)
	if err != nil {
		return utils.NewInputError(
			"failed to extract matching input for subschema %s: %w", utils.TemplateParamPolicyConfig, err)
	}
	policyTemplateMatchingInputMap := policyTemplateMatchingInput.(map[string]any)

	// Get the merged PolicyTemplate input data
	mergedPolicyTemplateData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.Templates.PolicyTemplateDefaults,
		policyTemplateMatchingInputMap,
		utils.TemplateParamPolicyConfig)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	// Validate the merged PolicyTemplate input data matches the schema
	err = provisioningv1alpha1.ValidateJsonAgainstJsonSchema(
		policyTemplateSubSchema, mergedPolicyTemplateData)
	if err != nil {
		return utils.NewInputError(
			"spec.templateParameters.%s does not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema.%s: %w",
			utils.TemplateParamPolicyConfig, clusterTemplate.Name, utils.TemplateParamPolicyConfig, err)
	}

	t.clusterInput.policyTemplateData = mergedPolicyTemplateData
	return nil
}

func (t *provisioningRequestReconcilerTask) getMergedClusterInputData(
	ctx context.Context, templateDefaultsCm string, clusterTemplateInput map[string]any, templateParam string) (map[string]any, error) {

	var templateDefaultsCmKey string

	switch templateParam {
	case utils.TemplateParamClusterInstance:
		templateDefaultsCmKey = utils.ClusterInstanceTemplateDefaultsConfigmapKey
	case utils.TemplateParamPolicyConfig:
		templateDefaultsCmKey = utils.PolicyTemplateDefaultsConfigmapKey
	default:
		return nil, utils.NewInputError("unsupported template parameter")
	}

	// Retrieve the configmap that holds the default data.
	templateCm, err := utils.GetConfigmap(ctx, t.client, templateDefaultsCm, t.ctDetails.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", templateDefaultsCm, err)
	}
	clusterTemplateDefaultsMap, err := utils.ExtractTemplateDataFromConfigMap[map[string]any](
		templateCm, templateDefaultsCmKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", templateDefaultsCm, err)
	}

	if templateParam == utils.TemplateParamClusterInstance {
		// Special handling for overrides of ClusterInstance's extraLabels and extraAnnotations.
		// The clusterTemplateInput will be overridden with the values from defaut configmap
		// if same labels/annotations exist in both.
		if err := t.overrideClusterInstanceLabelsOrAnnotations(
			clusterTemplateInput, clusterTemplateDefaultsMap); err != nil {
			return nil, utils.NewInputError("%s", err.Error())
		}
	}

	// Get the merged cluster data
	mergedClusterDataMap, err := mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateDefaultsMap)
	if err != nil {
		return nil, utils.NewInputError("failed to merge data for %s: %s", templateParam, err.Error())
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
		err := utils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, checkType) // clusterTemplateInput overrides the defaults
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
