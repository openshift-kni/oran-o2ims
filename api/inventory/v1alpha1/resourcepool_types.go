/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourcePoolSpec defines the desired state of ResourcePool.
// Represents a resource pool containing O-Cloud resources.
// Based on O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00 section 3.2.6.2.5
type ResourcePoolSpec struct {
	// ResourcePoolId is the string identifier for this resource pool.
	// This value is used to generate the deterministic UUID for resourcePoolId.
	// It should match the resourcePoolId label on BareMetalHost resources.
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Pool ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ResourcePoolId string `json:"resourcePoolId"`

	// OCloudSiteId references the OCloudSite this pool belongs to.
	// Must match an existing OCloudSite's siteId.
	// This is used to generate the deterministic UUID for oCloudSiteId.
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="O-Cloud Site ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	OCloudSiteId string `json:"oCloudSiteId"`

	// Name is the human-readable name of the resource pool
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`

	// Description provides additional details about the resource pool
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Description",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Description string `json:"description"`

	// Extensions contains additional custom attributes
	// +optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// ResourcePoolStatus defines the observed state of ResourcePool
type ResourcePoolStatus struct {
	// Conditions represent the latest available observations of the ResourcePool's state
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=resourcepools,shortName=rp
// +kubebuilder:printcolumn:name="PoolID",type="string",JSONPath=".spec.resourcePoolId"
// +kubebuilder:printcolumn:name="SiteID",type="string",JSONPath=".spec.oCloudSiteId"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ResourcePool is the Schema for the resourcepools API.
// Represents a resource pool containing O-Cloud resources.
// +operator-sdk:csv:customresourcedefinitions:displayName="Resource Pool",resources={{ResourcePool,v1alpha1}}
type ResourcePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePoolSpec   `json:"spec,omitempty"`
	Status ResourcePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePoolList contains a list of ResourcePool
type ResourcePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePool{}, &ResourcePoolList{})
}
