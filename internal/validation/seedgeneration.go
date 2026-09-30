/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"encoding/json"
	"fmt"
	"time"
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

// The effective schema applies after ClusterTemplate defaults and PR overrides
// are merged. The PR-facing templateParameterSchema must remain narrower: it
// can expose seedImage, but cannot expose credentials or execution targets.
const seedGenerationEffectiveSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "seedGeneration": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "seedImage": {"type": "string", "minLength": 1, "pattern": "^([a-z0-9]+://)?\\S+$"},
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
    },
    "clusterUpgradeTimeout": {"type": "string", "minLength": 1}
  },
  "required": ["seedGeneration"]
}`

// ValidateSeedGenerationUpgradeData validates the complete merged seed config.
// It deliberately does not use the PR-facing schema, which only describes
// fields that a ProvisioningRequest author is allowed to override.
func ValidateSeedGenerationUpgradeData(upgradeData map[string]any) error {
	var schema map[string]any
	if err := json.Unmarshal([]byte(seedGenerationEffectiveSchema), &schema); err != nil {
		return fmt.Errorf("invalid internal seed generation schema: %w", err)
	}
	if err := ValidateJSONSchema(schema, upgradeData); err != nil {
		return fmt.Errorf("seed generation configuration is invalid: %w", err)
	}
	if rawTimeout, ok := upgradeData["clusterUpgradeTimeout"]; ok {
		timeout, err := time.ParseDuration(rawTimeout.(string))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("clusterUpgradeTimeout must be a positive duration")
		}
	}
	return nil
}

// ValidateSeedGenerationPRSchema keeps dangerous fields out of PR overrides.
// A trusted template may choose to expose seedImage; every other seed field
// must come from template defaults.
func ValidateSeedGenerationPRSchema(schema map[string]any) error {
	if schema["type"] != "object" {
		return fmt.Errorf("upgradeParameters.seedGeneration must have type \"object\"")
	}
	if schema["additionalProperties"] != false {
		return fmt.Errorf("upgradeParameters.seedGeneration must set additionalProperties to false")
	}
	// Both keywords can admit fields outside properties despite the closed
	// object declaration. Only seedImage may be selected by a PR author.
	if _, ok := schema["patternProperties"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration must not use patternProperties")
	}
	if _, ok := schema["$ref"]; ok {
		return fmt.Errorf("upgradeParameters.seedGeneration must not use $ref")
	}
	if required, ok := schema["required"]; ok {
		fields, ok := required.([]any)
		if !ok || len(fields) != 0 {
			return fmt.Errorf("upgradeParameters.seedGeneration must not require PR overrides")
		}
	}
	propsRaw, exists := schema["properties"]
	if !exists {
		return nil
	}
	props, ok := propsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("upgradeParameters.seedGeneration properties must be an object")
	}
	for key, value := range props {
		if key != "seedImage" {
			return fmt.Errorf("upgradeParameters.seedGeneration cannot expose template-owned field %q", key)
		}
		field, ok := value.(map[string]any)
		if !ok || field["type"] != "string" {
			return fmt.Errorf("upgradeParameters.seedGeneration.seedImage must have type \"string\"")
		}
	}
	return nil
}
