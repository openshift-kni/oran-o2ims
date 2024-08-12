package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionType is a string representing the condition's type
type ConditionType string

// The following constants define the different types of conditions that will be set for ClusterTemplate
var CTconditionTypes = struct {
	Validated ConditionType
}{
	Validated: "ClusterTemplateValidated",
}

// ConditionReason is a string representing the condition's reason
type ConditionReason string

// The following constants define the different reasons that conditions will be set for ClusterTemplate
var CTconditionReasons = struct {
	Completed ConditionReason
	Failed    ConditionReason
}{
	Completed: "Completed",
	Failed:    "Failed",
}

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(existingConditions *[]metav1.Condition, conditionType ConditionType, conditionReason ConditionReason, conditionStatus metav1.ConditionStatus, message string) {
	conditions := *existingConditions
	condition := meta.FindStatusCondition(*existingConditions, string(conditionType))
	if condition != nil &&
		condition.Status != conditionStatus &&
		conditions[len(conditions)-1].Type != string(conditionType) {
		meta.RemoveStatusCondition(existingConditions, string(conditionType))
	}
	meta.SetStatusCondition(
		existingConditions,
		metav1.Condition{
			Type:               string(conditionType),
			Status:             conditionStatus,
			Reason:             string(conditionReason),
			Message:            message,
			LastTransitionTime: metav1.Now(),
		},
	)
}
