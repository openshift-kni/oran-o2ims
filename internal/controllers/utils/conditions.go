/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(
	existingConditions *[]metav1.Condition,
	conditionType provisioningv1alpha1.ConditionType,
	conditionReason provisioningv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string,
) {
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

// SetProvisioningStatePending updates the provisioning state to pending with detailed message
func SetProvisioningStatePending(cr *provisioningv1alpha1.ProvisioningRequest, message string) {
	if cr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StatePending ||
		cr.Status.ProvisioningStatus.ProvisioningDetails != message {
		cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StatePending
		cr.Status.ProvisioningStatus.ProvisioningDetails = message
		cr.Status.ProvisioningStatus.UpdateTime = metav1.Now()
	}
}

// SetProvisioningStateInProgress updates the provisioning state to progressing with detailed message
func SetProvisioningStateInProgress(cr *provisioningv1alpha1.ProvisioningRequest, message string) {
	if cr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateProgressing ||
		cr.Status.ProvisioningStatus.ProvisioningDetails != message {
		cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateProgressing
		cr.Status.ProvisioningStatus.ProvisioningDetails = message
		cr.Status.ProvisioningStatus.UpdateTime = metav1.Now()
	}
}

// SetProvisioningStateFailed updates the provisioning state to failed with detailed message
func SetProvisioningStateFailed(cr *provisioningv1alpha1.ProvisioningRequest, message string) {
	if cr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateFailed ||
		cr.Status.ProvisioningStatus.ProvisioningDetails != message {
		cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateFailed
		cr.Status.ProvisioningStatus.ProvisioningDetails = message
		cr.Status.ProvisioningStatus.UpdateTime = metav1.Now()
	}
}

// SetProvisioningStateFulfilled updates the provisioning state to fulfilled with detailed message
func SetProvisioningStateFulfilled(cr *provisioningv1alpha1.ProvisioningRequest) {
	if cr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateFulfilled {
		cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateFulfilled
		cr.Status.ProvisioningStatus.ProvisioningDetails = "Provisioning request has completed successfully"
		cr.Status.ProvisioningStatus.UpdateTime = metav1.Now()
	}
}

// SetProvisioningStateDeleting updates the provisioning state to deleting with detailed message
func SetProvisioningStateDeleting(cr *provisioningv1alpha1.ProvisioningRequest) {
	if cr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateDeleting {
		cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateDeleting
		cr.Status.ProvisioningStatus.ProvisioningDetails = "Deletion is in progress"
		cr.Status.ProvisioningStatus.UpdateTime = metav1.Now()
	}
}

// IsProvisioningStateFulfilled checks if the provisioning status is fulfilled
func IsProvisioningStateFulfilled(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	return cr.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFulfilled
}

// IsClusterProvisionPresent checks if the cluster provision condition is present
func IsClusterProvisionPresent(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	return condition != nil
}

// IsClusterProvisionInProgress checks if the cluster provision condition status is in progress.
func IsClusterProvisionInProgress(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	return condition != nil && condition.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress)
}

// IsClusterProvisionCompleted checks if the cluster provision condition status is completed.
func IsClusterProvisionCompleted(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	return condition != nil && (condition.Status == metav1.ConditionTrue)
}

// IsClusterProvisionTimedOutOrFailed checks if the cluster provision condition status is timedout or failed
func IsClusterProvisionTimedOutOrFailed(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	if condition != nil {
		if condition.Status == metav1.ConditionFalse &&
			(condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
				condition.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut)) {
			return true
		}
	}
	return false
}

// IsClusterProvisionFailed checks if the cluster provision condition status is failed
func IsClusterProvisionFailed(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	return condition != nil && condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed)
}

// IsClusterConfigCompleted checks if the cluster config condition status is completed
func IsClusterConfigCompleted(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// IsHardwareConfigCompleted checks if the hardware config condition status is completed
// For initial provisioning, this condition may not exist, which is considered completed.
// For Day2 updates, the condition must be present and True.
func IsHardwareConfigCompleted(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
	if condition == nil {
		// No HardwareConfigured condition means no Day2 hardware config was needed
		return true
	}
	// If condition exists, it must be True to be considered completed
	return condition.Status == metav1.ConditionTrue
}

// IsSmoRegistrationCompleted checks if registration with SMO has been completed
func IsSmoRegistrationCompleted(cr *inventoryv1alpha1.Inventory) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(InventoryConditionTypes.SmoRegistrationCompleted))
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// IsClusterUpgradeInProgress checks if the cluster upgrade condition status is in progress
func IsClusterUpgradeInProgress(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
	if condition != nil {
		if condition.Status == metav1.ConditionFalse &&
			condition.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) {
			return true
		}
	}
	return false
}

// IsClusterUpgradeCompleted checks if the cluster upgrade is completed
func IsClusterUpgradeCompleted(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
	if condition != nil {
		if condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsClusterUpgradeInitiated checks if the cluster upgrade is initiated
func IsClusterUpgradeInitiated(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
	return condition != nil
}

// IsClusterZtpDone checks if the cluster ZTP is done
func IsClusterZtpDone(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	if cr.Status.Extensions.ClusterDetails != nil {
		return cr.Status.Extensions.ClusterDetails.ZtpStatus == ClusterZtpDone
	}
	return false
}

// IsHardwareProvisionTimedOutOrFailed checks if the hardware provision condition status is timedout or failed
func IsHardwareProvisionTimedOutOrFailed(cr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
	if condition != nil {
		if condition.Status == metav1.ConditionFalse &&
			(condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
				condition.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut)) {
			return true
		}
	}
	return false
}

// HasFatalProvisioningFailure checks if the ProvisioningRequest
// has a fatal provisioning failure that cannot be recovered
// on its own.
func HasFatalProvisioningFailure(conditions []metav1.Condition) bool {
	for _, condType := range provisioningv1alpha1.FatalPRconditionTypes {
		cond := meta.FindStatusCondition(conditions, string(condType))
		if cond != nil && cond.Status == metav1.ConditionFalse &&
			(cond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
				cond.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut)) {
			return true
		}
	}
	return false
}
