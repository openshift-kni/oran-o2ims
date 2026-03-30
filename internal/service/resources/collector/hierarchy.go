/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

// isResourceReady checks if a CR has the Ready=True condition.
// Used to filter out CRs that haven't passed controller validation.
func isResourceReady(conditions []metav1.Condition) bool {
	for _, c := range conditions {
		if c.Type == inventoryv1alpha1.ConditionTypeReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	// No Ready condition found = not ready
	return false
}

// getReadyReason returns the reason from the Ready condition, or empty string if not found.
// Useful for logging why a CR was filtered out.
func getReadyReason(conditions []metav1.Condition) string {
	for _, c := range conditions {
		if c.Type == inventoryv1alpha1.ConditionTypeReady {
			return c.Reason
		}
	}
	return ""
}
