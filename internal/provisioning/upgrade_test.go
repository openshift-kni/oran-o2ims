/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

func TestRequestedWorkerPoolUpgradeThrough(t *testing.T) {
	tests := []struct {
		name, defaults, params, expected string
	}{
		{"no authorization", `{}`, `{}`, ""},
		{"template default", `{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "canary"}}}`, `{}`, "canary"},
		{"PR override", `{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "canary"}}}`,
			`{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "remaining"}}}}`, "remaining"},
		{"explicitly empty PR override", `{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "canary"}}}`,
			`{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": ""}}}}`, ""},
		{"omitted PR field retains default", `{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "canary"}}}`,
			`{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {}}}}`, "canary"},
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
		{"invalid defaults pool", `{"clusterVersion": {"workerPoolUpgrade": []}}`, `{}`,
			"ClusterTemplate upgradeDefaults.clusterVersion.workerPoolUpgrade must be an object"},
		{"invalid defaults stage", `{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": 4}}}`, `{}`,
			"ClusterTemplate upgradeDefaults.clusterVersion.workerPoolUpgrade.upgradeThrough must be a string"},
		{"invalid defaults clusterVersion", `{"clusterVersion":[]}`, `{}`,
			"ClusterTemplate upgradeDefaults.clusterVersion must be an object"},
		{"invalid parameters JSON", `{}`, `{`, "failed to parse templateParameters"},
		{"null parameters", `{}`, `null`, "templateParameters must be an object"},
		{"invalid upgradeParameters", `{}`, `{"upgradeParameters":null}`,
			"templateParameters.upgradeParameters must be an object"},
		{"invalid parameters pool", `{}`, `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": []}}}`,
			"templateParameters.upgradeParameters.clusterVersion.workerPoolUpgrade must be an object"},
		{"invalid parameters stage", `{}`, `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": null}}}}`,
			"templateParameters.upgradeParameters.clusterVersion.workerPoolUpgrade.upgradeThrough must be a string"},
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

var _ = Describe("WorkerPoolUpgradeStatus.StageIndex", func() {
	It("returns no authorized stage for a missing status", func() {
		var status *provisioningv1alpha1.WorkerPoolUpgradeStatus
		Expect(StageIndex(status, "canary")).To(Equal(-1))
	})

	DescribeTable("StageIndex",
		func(name string, expected int) {
			status := &provisioningv1alpha1.WorkerPoolUpgradeStatus{Stages: []provisioningv1alpha1.WorkerPoolUpgradeStage{
				{Name: "canary"}, {Name: "remaining"},
			}}
			Expect(StageIndex(status, name)).To(Equal(expected))
		},
		Entry("no stage authorized", "", -1),
		Entry("first stage", "canary", 0),
		Entry("last stage", "remaining", 1),
		Entry("unknown stage", "missing", -1),
	)
})
