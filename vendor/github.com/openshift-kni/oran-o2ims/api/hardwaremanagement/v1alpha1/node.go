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

// NodeSpec describes a node presents a hardware server
type NodeSpec struct {
	NodePool  string `json:"nodePool"`
	GroupName string `json:"groupName"`
	HwProfile string `json:"hwProfile"`
}

type BMC struct {
	// Address contains the URL for accessing the BMC over the network.
	Address string `json:"address,omitempty"`

	// CredentialsName is a reference to a secret containing the credentials. That secret
	// should contain the keys `username` and `password`.
	CredentialsName string `json:"credentialsName,omitempty"`
}

// NodePoolStatus describes the observed state of a request to allocate and prepare
// a node that will eventually be part of a deployment manager.
type NodeStatus struct {
	BMC *BMC `json:"bmc,omitempty"`

	BootMACAddress string `json:"bootMACAddress,omitempty"`

	Hostname string `json:"hostname,omitempty"`

	// Conditions represent the observations of the current state of the NodePool. Possible
	// values of the condition type are `Provisioned`, `Unprovisioned`, `Updating` and `Failed`.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Node is the schema for an allocated node

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Node struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeSpec   `json:"spec,omitempty"`
	Status NodeStatus `json:"status,omitempty"`
}

// NodeList contains a list of provisioned node.
//
// +kubebuilder:object:root=true
type NodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Node `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&Node{},
		&NodeList{},
	)
}
