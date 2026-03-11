/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

// Finalizer constants for hierarchical CR deletion protection
const (
	// LocationFinalizer prevents deletion of a Location until all
	// dependent OCloudSites have been removed
	LocationFinalizer = "location.ocloud.openshift.io/finalizer"

	// OCloudSiteFinalizer prevents deletion of an OCloudSite until all
	// dependent ResourcePools and BareMetalHosts have been removed
	OCloudSiteFinalizer = "ocloudsite.ocloud.openshift.io/finalizer"

	// ResourcePoolFinalizer prevents deletion of a ResourcePool until all
	// dependent BareMetalHosts have been removed
	ResourcePoolFinalizer = "resourcepool.ocloud.openshift.io/finalizer"
)

// Condition types for inventory CRs
const (
	// ConditionTypeReady indicates the CR is valid and synced
	ConditionTypeReady = "Ready"

	// ConditionTypeDeleting indicates the CR is being deleted
	ConditionTypeDeleting = "Deleting"
)

// Condition reasons for inventory CRs
const (
	// ReasonReady indicates the CR is ready
	ReasonReady = "Ready"

	// ReasonParentNotFound indicates the referenced parent CR does not exist
	ReasonParentNotFound = "ParentNotFound"

	// ReasonParentNotReady indicates the parent CR exists but is not ready
	ReasonParentNotReady = "ParentNotReady"

	// ReasonDependentsExist indicates deletion is blocked due to existing dependents
	ReasonDependentsExist = "DependentsExist"
)
