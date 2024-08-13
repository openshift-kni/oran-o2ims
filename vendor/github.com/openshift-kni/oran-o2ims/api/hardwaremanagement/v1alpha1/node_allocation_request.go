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

// NodeAllocationRequestSpec describes a request to allocate and prepare a node that will
// eventually be part of a deployment manager.
type NodeAllocationRequestSpec struct {
	// CloudID is the identifier of the O-Cloud that generated this request. The hardware
	// manager may want to use this to tag the nodes in its database, and to generate
	// statistics.
	//
	// +kubebuilder:validation:Required
	CloudID string `json:"cloudID"`

	// Location is the geographical location of the requested node.
	//
	// +kubebuilder:validation:Required
	Location string `json:"location"`

	// Extensions contains additional information that is associated to the request.
	//
	// This will be populated from the extensions that are defined in the top level of the
	// deployment manager template, in the node profile, and in the node set. For example,
	// if the deployment manager template contains this:
	//
	//	extensions:
	//	  "oran.openshift.io/release": "4.16.1"
	//	  "oran.acme.com/cores": "16"
	//	nodeProfiles:
	//	- name: high-performance
	//	  extensions:
	//	    "oran.acme.com/cores": "32"
	//	    "oran.acme.com/memory": "128GiB"
	//	nodeSets:
	//	- name: control-plane
	//        size: 3
	//        profile: high-performance
	//	  extensions:
	//	    "oran.acme.com/memory": "256GiB"
	//
	// Then three node orders will be generated, and each will contain the following:
	//
	//	extensions:
	//	  "oran.acme.com/cores": "32"
	//	  "oran.acme.com/memory": "256GiB"
	//
	// Note how the extensions not related to the hardware like `oran.openshift.io/release`
	// aren't copied to the request, and how the `oran.acme.com/memory` extension in the node
	// set overrides the same extesions from the node profile.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// NodeAllocationRequestStatus describes the observed state of a request to allocate and prepare
// a node that will eventually be part of a deployment manager.
type NodeAllocationRequestStatus struct {
	// NodeID is the identifier of the node used by the hardware manager. This will be used
	// by the IMS implementation to reference the node later when it needs to be updated or
	// decomissioned.
	NodeID string `json:"nodeID,omitempty"`

	// BMC contains the details to connect to the baseboard managment controller
	// of the node.
	BMC BMCDetails `json:"bmc,omitempty"`

	// Conditions represents the observations of the current state of the template. Possible
	// values of the condition type are `Fulfilled` and `Failed`.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// BMCDetails contains the details needed to connect to the baseboard management controller of
// a node.
type BMCDetails struct {
	// Address contains the URL for accessing the BMC over the network.
	Address string `json:"address,omitempty"`

	// CredentiasName is a reference to a secret containing the credentials. That secret
	// should contain the keys `username` and `password`.
	CredentialsName string `json:"credentialsName,omitempty"`

	// DisableCertificateVerification disables verification of server certificates when using
	// HTTPS to connect to the BMC. This is required when the server certificate is
	// self-signed, but is insecure because it allows a man-in-the-middle to intercept the
	// connection.
	DisableCertificateVerification bool `json:"disableCertificateVerification,omitempty"`
}

// NodeAllocationRequest is the schema for a node allocation request.
//
// +kubebuilder:resource:shortName=nar
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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
