/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"encoding/json"
	"testing"
)

func TestSeedGenerationSharedSchema(t *testing.T) {
	valid := map[string]any{
		"seedGeneration": map[string]any{
			"seedImage":             "quay.io/example/seed:4.22",
			"seedAuthSecretRef":     map[string]any{"name": "push-auth"},
			"seedGenerationTimeout": "2h",
		},
	}
	if err := ValidateSeedGenerationUpgradeData(valid); err != nil {
		t.Fatalf("valid seed configuration rejected: %v", err)
	}
	valid["seedGenerationTimeout"] = "3h"
	if err := ValidateSeedGenerationUpgradeData(valid); err == nil {
		t.Fatal("upgrade-level seed generation timeout accepted")
	}
	delete(valid, "seedGenerationTimeout")
	valid["clusterVersion"] = map[string]any{}
	if err := ValidateSeedGenerationUpgradeData(valid); err == nil {
		t.Fatal("mixed operation types accepted")
	}

	var restrictedSchema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"properties":{"seedImage":{"type":"string"}}}`), &restrictedSchema); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSeedGenerationPRSchema(restrictedSchema); err != nil {
		t.Fatalf("seed image override schema rejected: %v", err)
	}
	var secretRefSchema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1}}}`), &secretRefSchema); err != nil {
		t.Fatal(err)
	}
	restrictedSchema["properties"].(map[string]any)["seedAuthSecretRef"] = secretRefSchema
	if err := ValidateSeedGenerationPRSchema(restrictedSchema); err != nil {
		t.Fatalf("Secret reference override schema rejected: %v", err)
	}
	secretRefSchema["properties"].(map[string]any)["namespace"] = map[string]any{"type": "string"}
	if err := ValidateSeedGenerationPRSchema(restrictedSchema); err == nil {
		t.Fatal("Secret namespace override schema accepted")
	}
}

func TestValidateSeedGenerationTemplateDefaults(t *testing.T) {
	var templateParameterSchema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"required":["upgradeParameters"],"properties":{"upgradeParameters":{"type":"object","additionalProperties":false,"required":["seedGeneration"],"properties":{"seedGeneration":{"type":"object","additionalProperties":false,"required":["seedImage","seedAuthSecretRef"],"properties":{"seedImage":{"type":"string"},"seedAuthSecretRef":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1}}}}}}}}}`), &templateParameterSchema); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]any{
		"seedGeneration": map[string]any{
			"seedGenerationTimeout": "3h",
		},
	}
	if err := ValidateSeedGenerationTemplateDefaults(defaults, templateParameterSchema); err != nil {
		t.Fatalf("template defaults without seedImage or Secret reference rejected when PR schema requires them: %v", err)
	}

	upgradeSchema := templateParameterSchema["properties"].(map[string]any)["upgradeParameters"].(map[string]any)
	seedSchema := upgradeSchema["properties"].(map[string]any)["seedGeneration"].(map[string]any)
	delete(seedSchema["properties"].(map[string]any), "seedImage")
	if err := ValidateSeedGenerationTemplateDefaults(defaults, templateParameterSchema); err == nil {
		t.Fatal("template defaults without seedImage accepted when PR schema does not expose it")
	}

	seedSchema["properties"].(map[string]any)["seedImage"] = map[string]any{"type": "string"}
	seedSchema["required"] = []any{"seedAuthSecretRef"}
	if err := ValidateSeedGenerationTemplateDefaults(defaults, templateParameterSchema); err == nil {
		t.Fatal("template defaults without seedImage accepted when the PR schema does not require it")
	}
}

func TestTemplateOwnedSeedGenerationWithoutPRSchemaEntry(t *testing.T) {
	upgradeSchema := map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}
	prSchema := map[string]any{
		"type": "object", "properties": map[string]any{"upgradeParameters": upgradeSchema},
	}
	defaults := map[string]any{"seedGeneration": map[string]any{
		"seedImage": "quay.io/example/seed:4.22", "seedAuthSecretRef": map[string]any{"name": "push-auth"},
	}}
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err != nil {
		t.Fatalf("complete template-owned defaults rejected: %v", err)
	}
	delete(defaults["seedGeneration"].(map[string]any), "seedAuthSecretRef")
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err == nil {
		t.Fatal("incomplete template defaults accepted without a required PR field")
	}
	defaults["seedGeneration"].(map[string]any)["seedAuthSecretRef"] = map[string]any{"name": "push-auth"}
	upgradeSchema["properties"].(map[string]any)["clusterVersion"] = map[string]any{"type": "object"}
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err == nil {
		t.Fatal("template-owned seed generation accepted a PR schema for another operation")
	}
	delete(upgradeSchema["properties"].(map[string]any), "clusterVersion")
	delete(upgradeSchema, "additionalProperties")
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err == nil {
		t.Fatal("template-owned seed generation accepted an open upgradeParameters schema")
	}
	delete(prSchema["properties"].(map[string]any), "upgradeParameters")
	prSchema["additionalProperties"] = false
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err != nil {
		t.Fatalf("complete defaults without an upgradeParameters schema were rejected: %v", err)
	}
	delete(prSchema, "additionalProperties")
	if err := ValidateSeedGenerationTemplateDefaults(defaults, prSchema); err == nil {
		t.Fatal("template-owned seed generation accepted an open root schema without upgradeParameters")
	}
}

func TestHasSeedGenerationConfig(t *testing.T) {
	tests := []struct {
		name     string
		defaults string
		input    string
		want     bool
		wantErr  bool
	}{
		{name: "no seed generation", defaults: `{"clusterUpgradeTimeout":"2h"}`, input: `{"upgradeParameters":{}}`},
		{name: "template defaults", defaults: `{"seedGeneration":{}}`, input: `{}`, want: true},
		{name: "request override", defaults: `{}`, input: `{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed"}}}`, want: true},
		{name: "invalid defaults", defaults: `{`, wantErr: true},
		{name: "invalid request", defaults: `{}`, input: `{`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HasSeedGenerationConfig([]byte(tt.defaults), []byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("HasSeedGenerationConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("HasSeedGenerationConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateSeedGenerationUpgradeData(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{
			name: "seed only",
			data: `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"}}}`,
		},
		{
			name: "seed and ISO",
			data: `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"seedGenerationTimeout":"3h","liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/","baseISOURL":"https://mirror.example.test/rhcos-live.iso","imageDigestSources":[{"source":"quay.io/ocp","mirrors":["mirror.example.test/ocp"]}]}}}`,
		},
		{
			name: "seed and ISO with derived release image and disk",
			data: `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/"}}}`,
		},
		{
			name:    "missing seed image",
			data:    `{"seedGeneration":{"seedAuthSecretRef":{"name":"push-auth"}}}`,
			wantErr: true,
		},
		{
			name:    "invalid image tag",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:bad!tag","seedAuthSecretRef":{"name":"push-auth"}}}`,
			wantErr: true,
		},
		{
			name:    "transport prefix is not a pull specification",
			data:    `{"seedGeneration":{"seedImage":"docker://quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"}}}`,
			wantErr: true,
		},
		{
			name:    "digest cannot be a push destination",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","seedAuthSecretRef":{"name":"push-auth"}}}`,
			wantErr: true,
		},
		{
			name:    "missing auth reference",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22"}}`,
			wantErr: true,
		},
		{
			name:    "unknown seed field",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"seedimage":"typo"}}`,
			wantErr: true,
		},
		{
			name:    "unknown nested reference field",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth","namespace":"other"}}}`,
			wantErr: true,
		},
		{
			name:    "missing ISO upload target",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","urlBase":"https://iso.example.test/images/"}}}`,
			wantErr: true,
		},
		{
			name:    "non-HTTPS ISO URL",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"http://iso.example.test/images/"}}}`,
			wantErr: true,
		},
		{
			name:    "non-HTTPS base ISO URL",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/","baseISOURL":"http://mirror.example.test/rhcos-live.iso"}}}`,
			wantErr: true,
		},
		{
			name:    "base ISO URL with user info",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/","baseISOURL":"https://user@mirror.example.test/rhcos-live.iso"}}}`,
			wantErr: true,
		},
		{
			name:    "base ISO URL with fragment",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/","baseISOURL":"https://mirror.example.test/rhcos-live.iso#latest"}}}`,
			wantErr: true,
		},
		{
			name:    "mixed operation types",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"}},"clusterVersion":{}}`,
			wantErr: true,
		},
		{
			name:    "invalid timeout",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"seedGenerationTimeout":"soon"}}`,
			wantErr: true,
		},
		{
			name:    "zero timeout",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"seedGenerationTimeout":"0s"}}`,
			wantErr: true,
		},
		{
			name:    "upgrade timeout is not a seed timeout",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"clusterUpgradeTimeout":"3h"}}`,
			wantErr: true,
		},
		{
			name:    "timeout at upgrade level is rejected",
			data:    `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"}},"seedGenerationTimeout":"3h"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data map[string]any
			if err := json.Unmarshal([]byte(tt.data), &data); err != nil {
				t.Fatalf("invalid test fixture: %v", err)
			}
			err := ValidateSeedGenerationUpgradeData(data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSeedGenerationUpgradeData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSeedGenerationEffectiveSchemaRejectsTransportPrefixes(t *testing.T) {
	var effectiveSchema map[string]any
	if err := json.Unmarshal([]byte(seedGenerationEffectiveSchema), &effectiveSchema); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"seedGeneration": map[string]any{
		"seedAuthSecretRef": map[string]any{"name": "push-auth"},
	}}
	seed := data["seedGeneration"].(map[string]any)
	for _, image := range []string{"docker://quay.io/example/seed:4.22", "oci://quay.io/example/seed:4.22"} {
		seed["seedImage"] = image
		if err := ValidateJSONSchema(effectiveSchema, data); err == nil {
			t.Fatalf("effective schema accepted transport-prefixed image %q", image)
		}
	}
	seed["seedImage"] = "quay.io/example/seed:4.22"
	if err := ValidateJSONSchema(effectiveSchema, data); err != nil {
		t.Fatalf("effective schema rejected plain image reference: %v", err)
	}
}

func TestValidateSeedGenerationPRSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{name: "no overrides", schema: `{"type":"object","additionalProperties":false}`},
		{name: "seed image override", schema: `{"type":"object","additionalProperties":false,"properties":{"seedImage":{"type":"string"}}}`},
		{name: "seed timeout override", schema: `{"type":"object","additionalProperties":false,"properties":{"seedGenerationTimeout":{"type":"string"}}}`},
		{name: "missing strictness", schema: `{"type":"object","properties":{"seedImage":{"type":"string"}}}`, wantErr: true},
		{name: "pattern properties bypass", schema: `{"type":"object","additionalProperties":false,"patternProperties":{".*":{}},"properties":{"seedImage":{"type":"string"}}}`, wantErr: true},
		{name: "reference bypass", schema: `{"type":"object","additionalProperties":false,"$ref":"#/definitions/openSeed","properties":{"seedImage":{"type":"string"}}}`, wantErr: true},
		{name: "Secret name override", schema: `{"type":"object","additionalProperties":false,"properties":{"seedAuthSecretRef":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1}}}}}`},
		{name: "Secret namespace override", schema: `{"type":"object","additionalProperties":false,"properties":{"seedAuthSecretRef":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1},"namespace":{"type":"string"}}}}}`, wantErr: true},
		{name: "ISO override", schema: `{"type":"object","additionalProperties":false,"properties":{"liveISO":{"type":"object"}}}`, wantErr: true},
		{name: "required PR value", schema: `{"type":"object","additionalProperties":false,"required":["seedImage"],"properties":{"seedImage":{"type":"string"}}}`},
		{name: "incorrect seed image type", schema: `{"type":"object","additionalProperties":false,"properties":{"seedImage":{"type":"number"}}}`, wantErr: true},
		{name: "incorrect timeout type", schema: `{"type":"object","additionalProperties":false,"properties":{"seedGenerationTimeout":{"type":"number"}}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal([]byte(tt.schema), &schema); err != nil {
				t.Fatalf("invalid test fixture: %v", err)
			}
			err := ValidateSeedGenerationPRSchema(schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSeedGenerationPRSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
