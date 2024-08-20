/*
Copyright (c) 2024 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeReleaseRequestSpec specifies how to release a node that has been previously allocated to be
// part of a deployment manager.
type NodeReleaseRequestSpec struct {
	// CloudID is the identifier of the O-Cloud that generated this request. The hardware
	// manager may want to use this to tag the nodes in its database, and to generate statistics.
	//
	// +kubebuilder:validation:Required
	CloudID string `json:"cloudID"`

	// NodeID is the identifier of the node that was provided by the hardware manager when the
	// node was previosly allocated.
	//
	// +kubebuilder:validation:Required
	NodeID string `json:"nodeID"`

	// Extensions contains additional information that is associated to the request.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// NodeReleaseRequestStatus describes the status of the release request.
type NodeReleaseRequestStatus struct {
	// Conditions represents the observations of the current state of the request. Possible
	// values of the condition type are `Fulfilled` and `Failed`.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NodeReleaseRequest is the schema for an request to release a node.
//
// +kubebuilder:resource:shortName=ndo
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type NodeReleaseRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeReleaseRequestSpec   `json:"spec,omitempty"`
	Status NodeReleaseRequestStatus `json:"status,omitempty"`
}

// NodeReleaseRequestList contains a list of node release requests.
//
// +kubebuilder:object:root=true
type NodeReleaseRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeReleaseRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&NodeReleaseRequest{},
		&NodeReleaseRequestList{},
	)
}
