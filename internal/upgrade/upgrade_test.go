/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package upgrade

import (
	"strings"
	"testing"
)

func TestRequestedWorkerPoolUpgradeThrough(t *testing.T) {
	tests := []struct {
		name, defaults, params, expected string
	}{
		{"no authorization", `{}`, `{}`, ""},
		{"template default", `{"workerPoolUpgrade":{"upgradeThrough":"canary"}}`, `{}`, "canary"},
		{"PR override", `{"workerPoolUpgrade":{"upgradeThrough":"canary"}}`,
			`{"upgradeParameters":{"workerPoolUpgrade":{"upgradeThrough":"remaining"}}}`, "remaining"},
		{"explicitly empty PR override", `{"workerPoolUpgrade":{"upgradeThrough":"canary"}}`,
			`{"upgradeParameters":{"workerPoolUpgrade":{"upgradeThrough":""}}}`, ""},
		{"omitted PR field retains default", `{"workerPoolUpgrade":{"upgradeThrough":"canary"}}`,
			`{"upgradeParameters":{"workerPoolUpgrade":{}}}`, "canary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			through, err := RequestedWorkerPoolUpgradeThrough([]byte(tt.defaults), []byte(tt.params))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if through != tt.expected {
				t.Errorf("upgradeThrough = %q, want %q", through, tt.expected)
			}
		})
	}
}

func TestRequestedWorkerPoolUpgradeThroughMalformedInput(t *testing.T) {
	tests := []struct {
		name, defaults, params, message string
	}{
		{"invalid defaults JSON", `{`, `{}`, "failed to parse ClusterTemplate upgradeDefaults"},
		{"null defaults", `null`, `{}`, "ClusterTemplate upgradeDefaults must be an object"},
		{"invalid defaults pool", `{"workerPoolUpgrade":[]}`, `{}`,
			"ClusterTemplate upgradeDefaults.workerPoolUpgrade must be an object"},
		{"invalid defaults stage", `{"workerPoolUpgrade":{"upgradeThrough":4}}`, `{}`,
			"ClusterTemplate upgradeDefaults.workerPoolUpgrade.upgradeThrough must be a string"},
		{"invalid parameters JSON", `{}`, `{`, "failed to parse templateParameters"},
		{"null parameters", `{}`, `null`, "templateParameters must be an object"},
		{"invalid upgradeParameters", `{}`, `{"upgradeParameters":null}`,
			"templateParameters.upgradeParameters must be an object"},
		{"invalid parameters pool", `{}`, `{"upgradeParameters":{"workerPoolUpgrade":[]}}`,
			"templateParameters.upgradeParameters.workerPoolUpgrade must be an object"},
		{"invalid parameters stage", `{}`, `{"upgradeParameters":{"workerPoolUpgrade":{"upgradeThrough":null}}}`,
			"templateParameters.upgradeParameters.workerPoolUpgrade.upgradeThrough must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RequestedWorkerPoolUpgradeThrough([]byte(tt.defaults), []byte(tt.params))
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("error = %v, want substring %q", err, tt.message)
			}
		})
	}
}
