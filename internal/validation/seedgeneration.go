/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/distribution/reference"
)

const (
	seedImageField       = "seedImage"
	seedRegistryRefField = "seedAuthSecretRef"
)

// HasSeedGenerationConfig reports whether the template defaults or request
// parameters select seed generation. Check both because defaults can request it
// without any ProvisioningRequest override.
func HasSeedGenerationConfig(upgradeDefaultsRaw, templateParametersRaw []byte) (bool, error) {
	if len(upgradeDefaultsRaw) > 0 {
		var defaults map[string]json.RawMessage
		if err := json.Unmarshal(upgradeDefaultsRaw, &defaults); err != nil {
			return false, fmt.Errorf("failed to parse ClusterTemplate upgradeDefaults: %w", err)
		}
		if _, ok := defaults["seedGeneration"]; ok {
			return true, nil
		}
	}

	if len(templateParametersRaw) == 0 {
		return false, nil
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(templateParametersRaw, &parameters); err != nil {
		return false, fmt.Errorf("failed to parse ProvisioningRequest templateParameters: %w", err)
	}
	upgradeRaw, ok := parameters["upgradeParameters"]
	if !ok {
		return false, nil
	}
	var upgrade map[string]json.RawMessage
	if err := json.Unmarshal(upgradeRaw, &upgrade); err != nil {
		return false, fmt.Errorf("failed to parse ProvisioningRequest upgradeParameters: %w", err)
	}
	_, ok = upgrade["seedGeneration"]
	return ok, nil
}

// ValidateSeedGenerationSupported rejects unfinished workflow options before
// provisioning starts, including options supplied entirely by the template.
func ValidateSeedGenerationSupported(upgradeDefaultsRaw, templateParametersRaw []byte) error {
	var defaults map[string]json.RawMessage
	if len(upgradeDefaultsRaw) > 0 {
		if err := json.Unmarshal(upgradeDefaultsRaw, &defaults); err != nil {
			return fmt.Errorf("invalid upgradeDefaults: %w", err)
		}
	}
	var parameters struct {
		UpgradeParameters map[string]json.RawMessage `json:"upgradeParameters"`
	}
	if len(templateParametersRaw) > 0 {
		if err := json.Unmarshal(templateParametersRaw, &parameters); err != nil {
			return fmt.Errorf("invalid templateParameters: %w", err)
		}
	}
	for _, input := range []map[string]json.RawMessage{defaults, parameters.UpgradeParameters} {
		if raw, ok := input["seedGeneration"]; ok {
			var seed map[string]json.RawMessage
			if err := json.Unmarshal(raw, &seed); err != nil {
				return fmt.Errorf("invalid seedGeneration configuration: %w", err)
			}
			if _, ok := seed["liveISO"]; ok {
				return fmt.Errorf("liveISO is not supported by this controller version")
			}
		}
	}
	return nil
}

// The effective schema applies after ClusterTemplate defaults and PR overrides
// are merged. The PR-facing templateParameterSchema must remain narrower: it
// can expose seedImage and seedGenerationTimeout, but cannot expose
// credentials or execution targets.
const seedGenerationEffectiveSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "seedGeneration": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "seedImage": {"type": "string", "minLength": 1, "pattern": "^\\S+$", "not": {"pattern": "://"}},
        "seedGenerationTimeout": {"type": "string", "minLength": 1},
        "seedAuthSecretRef": {
          "type": "object", "additionalProperties": false,
          "properties": {"name": {"type": "string", "minLength": 1}},
          "required": ["name"]
        },
        "recertImage": {"type": "string"},
        "liveISO": {
          "type": "object", "additionalProperties": false,
          "properties": {
            "releaseImage": {"type": "string", "minLength": 1},
            "installationDisk": {"type": "string", "minLength": 1},
            "sshKey": {"type": "string"},
            "uploadSecretRef": {
              "type": "object", "additionalProperties": false,
              "properties": {"name": {"type": "string", "minLength": 1}},
              "required": ["name"]
            },
            "urlBase": {"type": "string", "minLength": 1, "pattern": "^https://\\S+$"},
            "pullSecretRef": {
              "type": "object", "additionalProperties": false,
              "properties": {"name": {"type": "string", "minLength": 1}},
              "required": ["name"]
            },
            "storageClass": {"type": "string"},
            "imageDigestSources": {
              "type": "array",
              "items": {
                "type": "object", "additionalProperties": false,
                "properties": {
                  "source": {"type": "string", "minLength": 1},
                  "mirrors": {"type": "array", "minItems": 1, "items": {"type": "string", "minLength": 1}}
                },
                "required": ["source", "mirrors"]
              }
            },
            "additionalTrustBundleConfigMapRef": {
              "type": "object", "additionalProperties": false,
              "properties": {"name": {"type": "string", "minLength": 1}},
              "required": ["name"]
            }
          },
          "required": ["releaseImage", "installationDisk", "uploadSecretRef", "urlBase"]
        }
      },
      "required": ["seedImage", "seedAuthSecretRef"]
    }
  },
  "required": ["seedGeneration"]
}`

// ValidateSeedGenerationUpgradeData validates the complete merged seed config.
// It deliberately does not use the PR-facing schema, which only describes
// fields that a ProvisioningRequest author is allowed to override.
func ValidateSeedGenerationUpgradeData(upgradeData map[string]any) error {
	return validateSeedGenerationUpgradeData(upgradeData, []any{seedImageField, seedRegistryRefField})
}

// SeedGenerationDefaultsComplete reports whether both required fields are
// present. Their values are validated separately against the effective schema.
func SeedGenerationDefaultsComplete(upgradeData map[string]any) bool {
	seedGeneration, ok := upgradeData["seedGeneration"].(map[string]any)
	if !ok {
		return false
	}
	_, hasImage := seedGeneration[seedImageField]
	_, hasAuth := seedGeneration[seedRegistryRefField]
	return hasImage && hasAuth
}

// ValidateSeedGenerationTemplateDefaults validates template-owned seed
// generation defaults. Missing required values must be exposed and required
// by the PR schema. When both values are present, the template may omit the
// seedGeneration PR schema entry entirely.
func ValidateSeedGenerationTemplateDefaults(
	upgradeData, templateParameterSchema map[string]any,
) error {
	if err := validateSeedGenerationUpgradeData(upgradeData, []any{}); err != nil {
		return err
	}
	templateProperties, ok := templateParameterSchema["properties"].(map[string]any)
	if !ok {
		return fmt.Errorf("templateParameterSchema must have a properties section")
	}
	seedGeneration := upgradeData["seedGeneration"].(map[string]any)
	upgradeSchemaRaw, exposed := templateProperties["upgradeParameters"]
	if !exposed {
		for _, key := range []string{seedImageField, seedRegistryRefField} {
			if _, hasDefault := seedGeneration[key]; !hasDefault {
				return fmt.Errorf("%s is missing from upgradeDefaults and is not exposed for ProvisioningRequest input", key)
			}
		}
		if templateParameterSchema["additionalProperties"] != false {
			return fmt.Errorf("templateParameterSchema must set additionalProperties to false when upgradeParameters is omitted")
		}
		if _, ok := templateParameterSchema["patternProperties"]; ok {
			return fmt.Errorf("templateParameterSchema must not use patternProperties when upgradeParameters is omitted")
		}
		if _, ok := templateParameterSchema["$ref"]; ok {
			return fmt.Errorf("templateParameterSchema must not use $ref for seed generation")
		}
		return nil
	}
	upgradeSchema, ok := upgradeSchemaRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("templateParameterSchema must define upgradeParameters as an object schema")
	}
	upgradeProperties, ok := upgradeSchema["properties"].(map[string]any)
	if !ok {
		return fmt.Errorf("upgradeParameters schema must have a properties section")
	}
	seedGenerationPRSchemaRaw, exposed := upgradeProperties["seedGeneration"]
	if !exposed {
		for _, key := range []string{seedImageField, seedRegistryRefField} {
			if _, hasDefault := seedGeneration[key]; !hasDefault {
				return fmt.Errorf("%s is missing from upgradeDefaults and is not exposed for ProvisioningRequest input", key)
			}
		}
		if len(upgradeProperties) != 0 {
			return fmt.Errorf("upgradeParameters schema cannot expose another operation when seed generation is template-owned")
		}
		if upgradeSchema["additionalProperties"] != false {
			return fmt.Errorf("upgradeParameters must set additionalProperties to false for seed generation")
		}
		if _, ok := upgradeSchema["patternProperties"]; ok {
			return fmt.Errorf("upgradeParameters must not use patternProperties for seed generation")
		}
		if _, ok := upgradeSchema["$ref"]; ok {
			return fmt.Errorf("upgradeParameters must not use $ref for seed generation")
		}
		if _, ok := templateParameterSchema["$ref"]; ok {
			return fmt.Errorf("templateParameterSchema must not use $ref for seed generation")
		}
		return nil
	}
	seedGenerationPRSchema, ok := seedGenerationPRSchemaRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("upgradeParameters schema must define seedGeneration as an object schema")
	}
	if err := ValidateSeedGenerationPRSchema(seedGenerationPRSchema); err != nil {
		return fmt.Errorf("invalid seed generation PR schema: %w", err)
	}

	properties, _ := seedGenerationPRSchema["properties"].(map[string]any)
	missingPRFields := false
	for _, key := range []string{seedImageField, seedRegistryRefField} {
		if _, hasDefault := seedGeneration[key]; hasDefault {
			continue
		}
		if _, exposed := properties[key]; !exposed {
			return fmt.Errorf("%s is missing from upgradeDefaults and is not exposed for ProvisioningRequest input", key)
		}
		if !schemaRequiresField(seedGenerationPRSchema, key) {
			return fmt.Errorf("%s is missing from upgradeDefaults and must be required by the ProvisioningRequest schema", key)
		}
		missingPRFields = true
	}
	if missingPRFields {
		if !schemaRequiresField(upgradeSchema, "seedGeneration") {
			return fmt.Errorf("upgradeParameters.seedGeneration must be required when seed generation defaults are incomplete")
		}
		if !schemaRequiresField(templateParameterSchema, "upgradeParameters") {
			return fmt.Errorf("templateParameterSchema must require upgradeParameters when seed generation defaults are incomplete")
		}
	}
	return nil
}

func validateSeedGenerationUpgradeData(upgradeData map[string]any, requiredFields []any) error {
	var schema map[string]any
	if err := json.Unmarshal([]byte(seedGenerationEffectiveSchema), &schema); err != nil {
		return fmt.Errorf("invalid internal seed generation schema: %w", err)
	}
	properties := schema["properties"].(map[string]any)
	seedGenerationSchema := properties["seedGeneration"].(map[string]any)
	seedGenerationSchema["required"] = requiredFields
	if err := ValidateJSONSchema(schema, upgradeData); err != nil {
		return fmt.Errorf("seed generation configuration is invalid: %w", err)
	}
	seedGeneration, ok := upgradeData["seedGeneration"].(map[string]any)
	if !ok {
		return fmt.Errorf("seedGeneration must be an object")
	}
	if image, exists := seedGeneration[seedImageField]; exists {
		parsed, err := reference.ParseNormalizedNamed(image.(string))
		if err != nil {
			return fmt.Errorf("invalid seedImage: %w", err)
		}
		if _, digest := parsed.(reference.Digested); digest {
			return fmt.Errorf("seedImage must be a repository or tagged reference, not a digest push destination")
		}
	}
	if rawTimeout, ok := seedGeneration["seedGenerationTimeout"]; ok {
		timeout, err := time.ParseDuration(rawTimeout.(string))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("seedGenerationTimeout must be a positive duration")
		}
	}
	return nil
}

// ValidateSeedGenerationPRSchema limits PR overrides to the image, timeout,
// and Secret name. The referenced Secret is always resolved in the
// ClusterTemplate namespace and its data is never part of the PR.
func ValidateSeedGenerationPRSchema(schema map[string]any) error {
	if schema["type"] != "object" {
		return fmt.Errorf("upgradeParameters.seedGeneration must have type \"object\"")
	}
	if schema["additionalProperties"] != false {
		return fmt.Errorf("upgradeParameters.seedGeneration must set additionalProperties to false")
	}
	// Both keywords can admit fields outside properties despite the closed
	// object declaration.
	if _, ok := schema["patternProperties"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration must not use patternProperties")
	}
	if _, ok := schema["$ref"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration must not use $ref")
	}
	required, ok := schema["required"].([]any)
	if !ok {
		if _, exists := schema["required"]; exists {
			return fmt.Errorf("upgradeParameters.seedGeneration required must be an array")
		}
		required = []any{}
	}
	propsRaw, exists := schema["properties"]
	if !exists {
		if len(required) > 0 {
			return fmt.Errorf("upgradeParameters.seedGeneration required fields must be defined in properties")
		}
		return nil
	}
	props, ok := propsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("upgradeParameters.seedGeneration properties must be an object")
	}
	for key, value := range props {
		switch key {
		case seedImageField:
			field, ok := value.(map[string]any)
			if !ok || field["type"] != "string" {
				return fmt.Errorf("upgradeParameters.seedGeneration.seedImage must have type \"string\"")
			}
		case "seedGenerationTimeout":
			field, ok := value.(map[string]any)
			if !ok || field["type"] != "string" {
				return fmt.Errorf("upgradeParameters.seedGeneration.seedGenerationTimeout must have type \"string\"")
			}
		case seedRegistryRefField:
			if err := validateSeedAuthSecretRefPRSchema(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("upgradeParameters.seedGeneration cannot expose field %q", key)
		}
	}
	for _, rawField := range required {
		field, ok := rawField.(string)
		if !ok {
			return fmt.Errorf("upgradeParameters.seedGeneration required entries must be strings")
		}
		if _, exists := props[field]; !exists {
			return fmt.Errorf("upgradeParameters.seedGeneration required field %q must be defined in properties", field)
		}
		switch field {
		case seedImageField, "seedGenerationTimeout", seedRegistryRefField:
		default:
			return fmt.Errorf("upgradeParameters.seedGeneration cannot require field %q", field)
		}
	}
	return nil
}

func validateSeedAuthSecretRefPRSchema(value any) error {
	field, ok := value.(map[string]any)
	if !ok || field["type"] != "object" || field["additionalProperties"] != false {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef must be a closed object schema")
	}
	if _, ok := field["patternProperties"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef must not use patternProperties")
	}
	if _, ok := field["$ref"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef must not use $ref")
	}
	properties, ok := field["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef must expose only the name field")
	}
	nameSchema, ok := properties["name"].(map[string]any)
	if !ok || nameSchema["type"] != "string" {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef.name must have type \"string\"")
	}
	minLength, ok := nameSchema["minLength"].(float64)
	if !ok || minLength < 1 {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef.name must have minLength of at least 1")
	}
	required, ok := field["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "name" {
		return fmt.Errorf("upgradeParameters.seedGeneration.seedAuthSecretRef must require name")
	}
	return nil
}

func schemaRequiresField(schema map[string]any, field string) bool {
	required, ok := schema["required"].([]any)
	if !ok {
		return false
	}
	for _, rawField := range required {
		if rawField == field {
			return true
		}
	}
	return false
}
