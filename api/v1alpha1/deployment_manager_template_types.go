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

// DeploymentManagerTemplate is the schema for a template that defines how to create a deployment
// manager.
//
// +kubebuilder:resource:shortName=dmtemplate
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type DeploymentManagerTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// NodeProfiles is a collection of named node profiles that will be used in the template.
	//
	// +kubebuilder:validation:MinItems=1
	NodeProfiles []DeploymentManagerTemplateNodeProfile `json:"nodeProfiles,omitempty"`

	// NodeSets is a collection of named sets of nodes that will be used in the template.
	//
	// +kubebuilder:validation:MinItems=1
	NodeSets []DeploymentManagerTemplateNodeSet `json:"nodeSets,omitempty"`

	// Extensions contains additional information that is associated to the template.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// DeploymentManagerTemplateNodeSet associates a name with the configuration of a set of nodes. For
// example, it is common to have a set of control plane nodes and a set of worker nodes where all
// the control plane nodes share the same hardware settings, but the worker nodes have different
// settings.
type DeploymentManagerTemplateNodeSet struct {
	// Name is the name of the node set.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Profile is the name of the profile that defines the settings that will be used by
	// nodes in this set.
	//
	// +kubebuilder:validation:Required
	Profile string `json:"profile"`

	// Extensions contains additional information associated with the node set that can't
	// be expressed in the other fields.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// DeploymentManagerTemplateNodeProfile associates a name with a set of node settings.
type DeploymentManagerTemplateNodeProfile struct {
	// Name is the name of the profile.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Extensions contains additional information associated with the profile that can't
	// be expressed in the other fields.
	//
	// +kubebuilder:validation:Optional
	Extensions map[string]string `json:"extensions,omitempty"`
}

// DeploymentManagerTemplateList contains a list of deployment manager templates.
//
// +kubebuilder:object:root=true
type DeploymentManagerTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentManagerTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&DeploymentManagerTemplate{},
		&DeploymentManagerTemplateList{},
	)
}
