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
	SiteID string `json:"siteId"`

	// GlobalLocationID references the Location this site belongs to.
	// Must match an existing Location's globalLocationId.
	// +kubebuilder:validation:MinLength=1
	GlobalLocationID string `json:"globalLocationId"`

	// Name is the human-readable name of the site
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Description provides additional details about the site
	Description string `json:"description"`

	// Extensions contains additional custom attributes
	// +optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ocloudsites,shortName=ocs
// +kubebuilder:printcolumn:name="SiteID",type="string",JSONPath=".spec.siteId"
// +kubebuilder:printcolumn:name="LocationID",type="string",JSONPath=".spec.globalLocationId"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// OCloudSite is the Schema for the ocloudsites API.
// Represents an O-Cloud site instance at a specific location.
// +operator-sdk:csv:customresourcedefinitions:displayName="O-Cloud Site"
type OCloudSite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OCloudSiteSpec `json:"spec,omitempty"`
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
