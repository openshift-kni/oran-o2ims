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

// DeploymentManagerOrderSpec defines the desired state of deployment manager template.
type DeploymentManagerOrderSpec struct {
	// ID is the identifier that will be assigned to the deployment manager.
	//
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Template is the name of the deployment manager template that will be used to create the
	// deployment manager.
	//
	// +kubebuilder:validation:Required
	Template string `json:"template"`

	// Location is the geographical location of the requested deployment manager.
	//
	// +kubebuilder:validation:Required
	Location string `json:"location"`

	// Extensions contains additional information that is associated to the order.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// DeploymentManagerOrderStatus describes the observed state of a deployment manager order.
type DeploymentManagerOrderStatus struct {
	// Conditions represents the observations of the current state of the template. Possible
	// values of the condition type are `Fulfilled` and `Failed`.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DeploymentManagerOrder is the schema for an order to create one or more deployment managers.
//
// +kubebuilder:resource:shortName=dmorder
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type DeploymentManagerOrder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentManagerOrderSpec   `json:"spec,omitempty"`
	Status DeploymentManagerOrderStatus `json:"status,omitempty"`
}

// DeploymentManagerOrderList contains a list of deployment manager orders.
//
// +kubebuilder:object:root=true
type DeploymentManagerOrderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentManagerOrder `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&DeploymentManagerOrder{},
		&DeploymentManagerOrderList{},
	)
}
