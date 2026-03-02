/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OCloudSiteSpec defines the desired state of OCloudSite.
// Represents an O-Cloud site instance at a specific location.
// Based on O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00 section 3.2.6.2.17
type OCloudSiteSpec struct {
	// SiteID is the string identifier that matches BareMetalHost siteId labels.
	// This value is used to map BMH resources to this site and to generate
	// the deterministic UUID for oCloudSiteId.
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Site ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SiteID string `json:"siteId"`

	// GlobalLocationID references the Location this site belongs to.
	// Must match an existing Location's globalLocationId.
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Global Location ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	GlobalLocationID string `json:"globalLocationId"`

	// Name is the human-readable name of the site
	// +kubebuilder:validation:MinLength=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`

	// Description provides additional details about the site
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Description",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Description string `json:"description"`

	// Extensions contains additional custom attributes
	// +optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// OCloudSiteStatus defines the observed state of OCloudSite
type OCloudSiteStatus struct {
	// Conditions represent the latest available observations of the OCloudSite's state
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=ocloudsites,shortName=ocs
// +kubebuilder:printcolumn:name="SiteID",type="string",JSONPath=".spec.siteId"
// +kubebuilder:printcolumn:name="LocationID",type="string",JSONPath=".spec.globalLocationId"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// OCloudSite is the Schema for the ocloudsites API.
// Represents an O-Cloud site instance at a specific location.
// +operator-sdk:csv:customresourcedefinitions:displayName="O-Cloud Site",resources={{OCloudSite,v1alpha1}}
type OCloudSite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCloudSiteSpec   `json:"spec,omitempty"`
	Status OCloudSiteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OCloudSiteList contains a list of OCloudSite
type OCloudSiteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCloudSite `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OCloudSite{}, &OCloudSiteList{})
}
