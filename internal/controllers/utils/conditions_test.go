/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

func TestIsClusterUpgradeInProgress(t *testing.T) {
	tests := []struct {
		name      string
		condition *metav1.Condition
		want      bool
	}{
		{name: "not initiated"},
		{name: "pending", condition: &metav1.Condition{Status: metav1.ConditionFalse, Reason: "Pending"}, want: true},
		{name: "in progress", condition: &metav1.Condition{Status: metav1.ConditionFalse, Reason: "InProgress"}, want: true},
		{name: "unknown", condition: &metav1.Condition{Status: metav1.ConditionUnknown, Reason: "Unknown"}, want: true},
		{name: "completed", condition: &metav1.Condition{Status: metav1.ConditionTrue, Reason: "Completed"}},
		{name: "failed", condition: &metav1.Condition{Status: metav1.ConditionFalse, Reason: "Failed"}},
		{name: "timed out", condition: &metav1.Condition{Status: metav1.ConditionFalse, Reason: "TimedOut"}},
		{name: "precondition failed", condition: &metav1.Condition{Status: metav1.ConditionFalse, Reason: "PreconditionChecksFailed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &provisioningv1alpha1.ProvisioningRequest{}
			if tt.condition != nil {
				condition := *tt.condition
				condition.Type = string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted)
				pr.Status.Conditions = []metav1.Condition{condition}
			}
			if got := IsClusterUpgradeInProgress(pr); got != tt.want {
				t.Errorf("IsClusterUpgradeInProgress() = %t, want %t", got, tt.want)
			}
		})
	}
}
