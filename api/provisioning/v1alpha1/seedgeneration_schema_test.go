/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

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
			ct := &ClusterTemplate{}
			ct.Spec.TemplateDefaults.UpgradeDefaults.Raw = []byte(tt.defaults)
			pr := &ProvisioningRequest{}
			pr.Spec.TemplateParameters.Raw = []byte(tt.input)
			got, err := HasSeedGenerationConfig(ct, pr)
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
			data: `{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedAuthSecretRef":{"name":"push-auth"},"seedGenerationTimeout":"3h","liveISO":{"releaseImage":"quay.io/ocp/release:4.22","installationDisk":"/dev/sda","uploadSecretRef":{"name":"upload"},"urlBase":"https://iso.example.test/images/","imageDigestSources":[{"source":"quay.io/ocp","mirrors":["mirror.example.test/ocp"]}]}}}`,
		},
		{
			name:    "missing seed image",
			data:    `{"seedGeneration":{"seedAuthSecretRef":{"name":"push-auth"}}}`,
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

func TestSeedGenerationRawPRValidationAllowsPartialOverride(t *testing.T) {
	ct := &ClusterTemplate{
		Spec: ClusterTemplateSpec{
			TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
				"type":"object","additionalProperties":false,
				"properties":{
					"clusterInstanceParameters":{"type":"object","properties":{}},
					"policyTemplateParameters":{"type":"object","properties":{}},
					"upgradeParameters":{"type":"object","properties":{
						"seedGeneration":{"type":"object","additionalProperties":false,"properties":{"seedImage":{"type":"string"},"seedGenerationTimeout":{"type":"string"}}}
					}}
				}}`)},
		},
	}
	pr := &ProvisioningRequest{
		Spec: ProvisioningRequestSpec{
			TemplateParameters: runtime.RawExtension{Raw: []byte(`{"clusterInstanceParameters":{},"policyTemplateParameters":{},"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:4.22","seedGenerationTimeout":"90m"}}}`)},
		},
	}
	if err := pr.ValidateTemplateInputMatchesSchema(ct); err != nil {
		t.Fatalf("seedImage and timeout PR overrides should pass raw admission: %v", err)
	}

	pr.Spec.TemplateParameters.Raw = []byte(`{"clusterInstanceParameters":{},"policyTemplateParameters":{},"upgradeParameters":{"seedGeneration":{"seedAuthSecretRef":{"name":"other-secret"}}}}`)
	if err := pr.ValidateTemplateInputMatchesSchema(ct); err == nil {
		t.Fatal("template-owned credential reference should be rejected at raw admission")
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
		{name: "credential override", schema: `{"type":"object","additionalProperties":false,"properties":{"seedAuthSecretRef":{"type":"object"}}}`, wantErr: true},
		{name: "ISO override", schema: `{"type":"object","additionalProperties":false,"properties":{"liveISO":{"type":"object"}}}`, wantErr: true},
		{name: "required PR value", schema: `{"type":"object","additionalProperties":false,"required":["seedImage"],"properties":{"seedImage":{"type":"string"}}}`, wantErr: true},
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
