/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"strings"
	"testing"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestValidateUpgradeInputIBGUPlan(t *testing.T) {
	tests := []struct {
		name, defaults, params string
		wantErr                bool
	}{
		{"prep only in defaults", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]}]}}`, `{}`, true},
		{"prep only in defaults with no request parameters", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]}]}}`, ``, true},
		{"prep only in request", `{}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]}]}}}`, true},
		{"upgrade only in request", `{}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["Upgrade"]}]}}}`, true},
		{"upgrade before prep", `{}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["Upgrade"]},{"actions":["Prep"]}]}}}`, true},
		{"upgrade in defaults", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}`, `{}`, false},
		{"override removes upgrade", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]},{"actions":["AbortOnFailure"]}]}}}`, true},
		{"override removes prep", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["AbortOnFailure"]}]}}}`, true},
		{"partial override retains upgrade", `{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}`, `{"upgradeParameters":{"imageBasedGroupUpgrade":{"plan":[{"actions":["Prep"]}]}}}`, false},
		{"no IBGU", `{}`, `{}`, false},
		{"cluster version upgrade", `{"clusterVersion":{"cvSpec":{"desiredUpdate":{"version":"4.17.0"}}}}`, `{}`, false},
		{"seed generation", `{"seedGeneration":{"isoImage":"example"}}`, `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &provisioningv1alpha1.ClusterTemplate{}
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(tt.defaults)}
			pr := &provisioningv1alpha1.ProvisioningRequest{}
			if tt.params != "" {
				pr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(tt.params)}
			}
			err := validateUpgradeInput(pr, ct)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateUpgradeInput() error = %v, want error = %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "plan must include Prep followed by Upgrade") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
