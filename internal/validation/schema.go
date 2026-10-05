/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/xeipuuv/gojsonschema"
)

// ValidateTemplateInputMatchesSchema validates the input parameters from the ProvisioningRequest
// against the schema defined in the ClusterTemplate. This function focuses on validating the
// input other than clusterInstanceParameters and policyTemplateParameters, as those will be
// validated separately. It ensures the input parameters have the expected types and any
// required parameters are present.
func ValidateTemplateInputMatchesSchema(templateName string, templateSchemaRaw, templateParametersRaw []byte) error {
	// Unmarshal the full schema from the ClusterTemplate
	templateParamSchema := make(map[string]any)
	err := json.Unmarshal(templateSchemaRaw, &templateParamSchema)
	if err != nil {
		// Unlikely to happen since it has been validated by API server
		return fmt.Errorf("error unmarshaling template schema: %w", err)
	}

	// Unmarshal the template input from the ProvisioningRequest
	templateParamsInput := make(map[string]any)
	if err = json.Unmarshal(templateParametersRaw, &templateParamsInput); err != nil {
		// Unlikely to happen since it has been validated by API server
		return fmt.Errorf("error unmarshaling templateParameters: %w", err)
	}

	// The following errors of missing keys are unlikely since the schema should already
	// be validated by ClusterTemplate controller
	schemaProperties, ok := templateParamSchema["properties"]
	if !ok {
		return fmt.Errorf(
			"missing keyword 'properties' in the schema from ClusterTemplate (%s)", templateName)
	}
	clusterInstanceSubSchema, ok := schemaProperties.(map[string]any)[constants.TemplateParamClusterInstance]
	if !ok {
		return fmt.Errorf(
			"missing required property '%s' in the schema from ClusterTemplate (%s)",
			constants.TemplateParamClusterInstance, templateName)
	}
	policyTemplateSubSchema, ok := schemaProperties.(map[string]any)[constants.TemplateParamPolicyConfig]
	if !ok {
		return fmt.Errorf(
			"missing required property '%s' in the schema from ClusterTemplate (%s)",
			constants.TemplateParamPolicyConfig, templateName)
	}

	// The ClusterInstance, PolicyTemplate, and HwMgmt parameters have their own specific
	// validation rules and will be handled separately. For now, remove the subschemas for
	// those parameters to ensure they are not validated at this stage.
	delete(clusterInstanceSubSchema.(map[string]any), "properties")
	delete(policyTemplateSubSchema.(map[string]any), "properties")

	// hwMgmtParameters is validated after merging with hwMgmtDefaults from the ClusterTemplate,
	// so strip its detailed schema here to avoid rejecting partial overrides (e.g. nodeGroupData
	// entries that omit fields like "role" because they come from the defaults).
	if hwMgmtSubSchema, ok := schemaProperties.(map[string]any)[constants.TemplateParamHwMgmt]; ok {
		if hwMgmtMap, ok := hwMgmtSubSchema.(map[string]any); ok {
			delete(hwMgmtMap, "properties")
		}
	}

	err = ValidateJSONSchema(templateParamSchema, templateParamsInput)
	if err != nil {
		return fmt.Errorf(
			"spec.templateParameters does not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema: %w",
			templateName, err)
	}

	return nil
}

// ValidateJSONSchema validates input against a JSON schema.
func ValidateJSONSchema(schema, input any) error {
	schemaLoader := gojsonschema.NewGoLoader(schema)
	inputLoader := gojsonschema.NewGoLoader(input)

	result, err := gojsonschema.Validate(schemaLoader, inputLoader)
	if err != nil {
		return fmt.Errorf("failed when validating the input against the schema: %w", err)
	}
	if result.Valid() {
		return nil
	}
	var errs []string
	for _, description := range result.Errors() {
		errs = append(errs, description.String())
	}
	return fmt.Errorf("invalid input: %s", strings.Join(errs, "; "))
}

// ErrSubSchemaNotFound is returned by ExtractSubSchema when the requested key
// does not exist in the schema's properties.
var ErrSubSchemaNotFound = errors.New("sub-schema not found")

// IsErrSubSchemaNotFound returns true if the error is or wraps ErrSubSchemaNotFound.
func IsErrSubSchemaNotFound(err error) bool {
	return errors.Is(err, ErrSubSchemaNotFound)
}

func ExtractSubSchema(mainSchema []byte, subSchemaKey string) (subSchema map[string]any, err error) {
	jsonObject := make(map[string]any)
	if len(mainSchema) == 0 {
		return subSchema, nil
	}
	err = json.Unmarshal(mainSchema, &jsonObject)
	if err != nil {
		return subSchema, fmt.Errorf("failed to UnMarshall Main Schema: %w", err)
	}
	if _, ok := jsonObject["properties"]; !ok {
		return subSchema, fmt.Errorf("non compliant Main Schema, missing 'properties' section: %w", err)
	}
	properties, ok := jsonObject["properties"].(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("could not cast 'properties' section of schema as map[string]any: %w", err)
	}

	subSchemaValue, ok := properties[subSchemaKey]
	if !ok {
		return subSchema, fmt.Errorf("subSchema '%s' does not exist: %w", subSchemaKey, ErrSubSchemaNotFound)
	}

	subSchema, ok = subSchemaValue.(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("subSchema '%s' is not a valid map: %w", subSchemaKey, err)
	}
	return subSchema, nil
}

// ExtractMatchingInput extracts the portion of the input data that corresponds to a given subSchema key.
func ExtractMatchingInput(parentSchema []byte, subSchemaKey string) (any, error) {
	inputData := make(map[string]any)
	err := json.Unmarshal(parentSchema, &inputData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal parent schema: %w", err)
	}

	// Check if the input contains the subSchema key
	matchingInput, ok := inputData[subSchemaKey]
	if !ok {
		return nil, fmt.Errorf("parent schema does not contain key '%s': %w", subSchemaKey, err)
	}
	return matchingInput, nil
}

// DisallowUnknownFieldsInSchema updates a schema by adding "additionalProperties": false
// to all objects/arrays that define "properties". This ensures that any unknown fields
// not defined in the schema will be disallowed during validation.
func DisallowUnknownFieldsInSchema(schema map[string]any) {
	// Check if the current schema level has "properties" defined
	if properties, hasProperties := schema["properties"]; hasProperties {
		// If "additionalProperties" is not already set, add it with the value false
		if _, exists := schema["additionalProperties"]; !exists {
			schema["additionalProperties"] = false
		}

		// Recurse into each property defined under "properties"
		if propsMap, ok := properties.(map[string]any); ok {
			for _, propValue := range propsMap {
				if propSchema, ok := propValue.(map[string]any); ok {
					DisallowUnknownFieldsInSchema(propSchema)
				}
			}
		}
	}

	// Recurse into each property defined under "items"
	if items, hasItems := schema["items"]; hasItems {
		if itemSchema, ok := items.(map[string]any); ok {
			DisallowUnknownFieldsInSchema(itemSchema)
		}
	}

	// Ignore other keywords that could have "properties"
}
