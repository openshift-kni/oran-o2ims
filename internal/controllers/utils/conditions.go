package utils

import (
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
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

// The following constants define the different types of conditions that will be set for ProvisioningRequest
var PRconditionTypes = struct {
	Validated                ConditionType
	HardwareTemplateRendered ConditionType
	HardwareProvisioned      ConditionType
	ClusterInstanceRendered  ConditionType
	ClusterResourcesCreated  ConditionType
	ClusterInstanceProcessed ConditionType
	ClusterProvisioned       ConditionType
	ConfigurationApplied     ConditionType
}{
	Validated:                "ProvisioningRequestValidated",
	HardwareTemplateRendered: "HardwareTemplateRendered",
	HardwareProvisioned:      "HardwareProvisioned",
	ClusterInstanceRendered:  "ClusterInstanceRendered",
	ClusterResourcesCreated:  "ClusterResourcesCreated",
	ClusterInstanceProcessed: "ClusterInstanceProcessed",
	ClusterProvisioned:       "ClusterProvisioned",
	ConfigurationApplied:     "ConfigurationApplied",
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

// The following constants define the different reasons that conditions will be set for ProvisioningRequest
var CRconditionReasons = struct {
	NotApplied      ConditionReason
	ClusterNotReady ConditionReason
	Completed       ConditionReason
	Failed          ConditionReason
	InProgress      ConditionReason
	Missing         ConditionReason
	OutOfDate       ConditionReason
	TimedOut        ConditionReason
	Unknown         ConditionReason
}{
	NotApplied:      "NotApplied",
	ClusterNotReady: "ClusterNotReady",
	Completed:       "Completed",
	Failed:          "Failed",
	InProgress:      "InProgress",
	Missing:         "Missing",
	OutOfDate:       "OutOfDate",
	TimedOut:        "TimedOut",
	Unknown:         "Unknown",
}

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(existingConditions *[]metav1.Condition, conditionType ConditionType, conditionReason ConditionReason, conditionStatus metav1.ConditionStatus, message string) {
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

// IsClusterProvisionPresent checks if the cluster provision condition is present
func IsClusterProvisionPresent(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, (string(PRconditionTypes.ClusterProvisioned)))
	return condition != nil
}

// IsClusterProvisionCompleted checks if the cluster provision condition status is completed
func IsClusterProvisionCompleted(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, (string(PRconditionTypes.ClusterProvisioned)))
	if condition != nil {
		if condition.Status == metav1.ConditionTrue && condition.Reason == string(CRconditionReasons.Completed) {
			return true
		}
	}
	return false
}

// IsClusterProvisionTimedOutOrFailed checks if the cluster provision condition status is timedout or failed
func IsClusterProvisionTimedOutOrFailed(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, (string(PRconditionTypes.ClusterProvisioned)))
	if condition != nil {
		if condition.Status == metav1.ConditionFalse &&
			(condition.Reason == string(CRconditionReasons.Failed) ||
				condition.Reason == string(CRconditionReasons.TimedOut)) {
			return true
		}
	}
	return false
}

// IsClusterProvisionCompletedOrFailed checks if the cluster provision condition status is completed or failed
func IsClusterProvisionCompletedOrFailed(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, (string(PRconditionTypes.ClusterProvisioned)))
	if condition != nil {
		if condition.Status == metav1.ConditionTrue ||
			(condition.Status == metav1.ConditionFalse &&
				condition.Reason == string(CRconditionReasons.Failed)) {
			return true
		}
	}
	return false
}

// IsSmoRegistrationCompleted checks if registration with SMO has been completed
func IsSmoRegistrationCompleted(cr *inventoryv1alpha1.Inventory) bool {
	condition := meta.FindStatusCondition(cr.Status.DeploymentsStatus.Conditions,
		string(InventoryConditionTypes.SmoRegistrationCompleted))
	if condition != nil {
		if condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
