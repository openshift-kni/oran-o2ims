/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"encoding/json"
	"testing"
)

func TestHasSeedGenerationConfigRawInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		defaults string
		request  string
		want     bool
		wantErr  bool
	}{
		{"none", `{}`, `{"upgradeParameters":{}}`, false, false},
		{"template", `{"seedGeneration":{}}`, `{}`, true, false},
		{"request", `{}`, `{"upgradeParameters":{"seedGeneration":{}}}`, true, false},
		{"bad defaults", `{`, `{}`, false, true},
		{"bad request", `{}`, `{`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HasSeedGenerationConfig([]byte(tc.defaults), []byte(tc.request))
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got %t, %v; want %t, error=%t", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestSeedGenerationSharedSchema(t *testing.T) {
	valid := map[string]any{
		"seedGeneration": map[string]any{
			"seedImage":         "quay.io/example/seed:4.22",
			"seedAuthSecretRef": map[string]any{"name": "push-auth"},
		},
	}
	if err := ValidateSeedGenerationUpgradeData(valid); err != nil {
		t.Fatalf("valid seed configuration rejected: %v", err)
	}
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
	restrictedSchema["properties"].(map[string]any)["seedAuthSecretRef"] = map[string]any{"type": "object"}
	if err := ValidateSeedGenerationPRSchema(restrictedSchema); err == nil {
		t.Fatal("credential override schema accepted")
	}
}
