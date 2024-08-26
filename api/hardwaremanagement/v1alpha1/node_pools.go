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

type LocationSpec struct {
	Location string `json:"location,omitempty"`
	Site     string `json:"site"`
}

// NodePoolSpec describes a pool of nodes to allocate
type NodePoolSpec struct {
	// CloudID is the identifier of the O-Cloud that generated this request. The hardware
	// manager may want to use this to tag the nodes in its database, and to generate
	// statistics.
	//
	// +kubebuilder:validation:Required
	CloudID string `json:"cloudID"`

	// LocationSpec is the geographical location of the requested node.
	LocationSpec `json:",inline"`

	NodeGroup []NodeGroup `json:"nodeGroup"`
}

type NodeGroup struct {
	Name      string `json:"name" yaml:"name"`
	HwProfile string `json:"hwProfile"`
	Size      int    `json:"size" yaml:"size"`
}

type Properties struct {
	NodeNames []string `json:"nodeNames,omitempty"`
}

// NodePoolStatus describes the observed state of a request to allocate and prepare
// a node that will eventually be part of a deployment manager.
type NodePoolStatus struct {
	// Properties represent the node properties in the pool
	Properties Properties `json:"properties,omitempty"`

	// Conditions represent the observations of the NodePool's current state.
	// Possible values of the condition type are `Provisioned` and `Unknown`.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// NodePool is the schema for an allocation request of nodes
//
// +kubebuilder:resource:shortName=np
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodePoolSpec   `json:"spec,omitempty"`
	Status NodePoolStatus `json:"status,omitempty"`
}

// NodePoolList contains a list of node allocation requests.
//
// +kubebuilder:object:root=true
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&NodePool{},
		&NodePoolList{},
	)
}
