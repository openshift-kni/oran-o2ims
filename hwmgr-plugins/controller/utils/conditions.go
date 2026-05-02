/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(existingConditions *[]metav1.Condition, conditionType, conditionReason string, conditionStatus metav1.ConditionStatus, message string) {
	conditions := *existingConditions
	condition := meta.FindStatusCondition(*existingConditions, conditionType)
	if condition != nil &&
		condition.Status != conditionStatus &&
		conditions[len(conditions)-1].Type != conditionType {
		meta.RemoveStatusCondition(existingConditions, conditionType)
	}
	meta.SetStatusCondition(
		existingConditions,
		metav1.Condition{
			Type:               conditionType,
			Status:             conditionStatus,
			Reason:             conditionReason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		},
	)
}
