/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BootInterfaceLabelAnnotation = "o2ims.oran.openshift.io/boot-interface-label"
)

// LocationSpec is the geographical location of the requested node.
type LocationSpec struct {
	// Location
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Location",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Location string `json:"location,omitempty"`
	// Site
	// +kubebuilder:validation:Required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Site",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Site string `json:"site"`
}

// NodeAllocationRequestSpec describes a group of nodes to allocate
type NodeAllocationRequestSpec struct {
	// CloudID is the identifier of the O-Cloud that generated this request. The hardware
	// manager may want to use this to tag the nodes in its database, and to generate
	// statistics.
	//
	// +kubebuilder:validation:Required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cloud ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	CloudID string `json:"cloudID"`

	// LocationSpec is the geographical location of the requested node.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Location Spec",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	LocationSpec `json:",inline"`

	// HwMgrId is the identifier for the hardware manager plugin instance.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Hardware Manager ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	HwMgrId string `json:"hwMgrId,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec
	NodeGroup []NodeGroup `json:"nodeGroup"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec
	Extensions map[string]string `json:"extensions,omitempty"`
}

type NodeGroup struct {
	NodeGroupData NodeGroupData `json:"nodeGroupData"` // Explicitly include as a named field
	Size          int           `json:"size" yaml:"size"`
}

type Properties struct {
	NodeNames []string `json:"nodeNames,omitempty"`
}

// GenerationStatus represents the observed generation for an operator.
type GenerationStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// NodeAllocationRequestStatus describes the observed state of a request to allocate and prepare
// a node that will eventually be part of a deployment manager.
type NodeAllocationRequestStatus struct {
	// Properties represent the node properties in the pool
	//+operator-sdk:csv:customresourcedefinitions:type=status
	Properties Properties `json:"properties,omitempty"`

	// Conditions represent the latest available observations of an NodeAllocationRequest's state.
	// +optional
	// +kubebuilder:validation:Type=array
	// +kubebuilder:validation:Items=Type=object
	//+operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	//+operator-sdk:csv:customresourcedefinitions:type=status
	HwMgrPlugin GenerationStatus `json:"hwMgrPlugin,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=status
	SelectedGroups map[string]string `json:"selectedGroups,omitempty"`
}

// NodeAllocationRequest is the schema for an allocation request of nodes
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nodeallocationrequests,shortName=orannar
// +kubebuilder:printcolumn:name="HwMgr Id",type="string",JSONPath=".spec.hwMgrId"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.conditions[-1:].reason"
// +operator-sdk:csv:customresourcedefinitions:displayName="Node Allocation Request",resources={{Namespace, v1}}
type NodeAllocationRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeAllocationRequestSpec   `json:"spec,omitempty"`
	Status NodeAllocationRequestStatus `json:"status,omitempty"`
}

// NodeAllocationRequestList contains a list of node allocation requests.
//
// +kubebuilder:object:root=true
type NodeAllocationRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeAllocationRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&NodeAllocationRequest{},
		&NodeAllocationRequestList{},
	)
}
