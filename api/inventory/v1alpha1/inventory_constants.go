/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

// Condition types for inventory CRs
const (
	// ConditionTypeReady indicates the CR is valid and synced
	ConditionTypeReady = "Ready"
)

// Condition reasons for inventory CRs
const (
	// ReasonReady indicates the CR is ready
	ReasonReady = "Ready"

	// ReasonParentNotFound indicates the referenced parent CR does not exist
	ReasonParentNotFound = "ParentNotFound"

	// ReasonParentNotReady indicates the parent CR exists but is not ready
	ReasonParentNotReady = "ParentNotReady"
)
