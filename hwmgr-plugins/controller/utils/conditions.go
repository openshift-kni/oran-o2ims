/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
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

func UpdateHardwarePluginStatusCondition(
	ctx context.Context,
	c client.Client,
	hwplugin *hwmgmtv1alpha1.HardwarePlugin,
	conditionType hwmgmtv1alpha1.ConditionType,
	conditionReason hwmgmtv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string) error {

	SetStatusCondition(&hwplugin.Status.Conditions,
		string(conditionType),
		string(conditionReason),
		conditionStatus,
		message)

	if err := UpdateK8sCRStatus(ctx, c, hwplugin); err != nil {
		return fmt.Errorf("failed to update HardwarePlugin status %s: %w", hwplugin.Name, err)
	}

	return nil
}
