/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HardwarePluginSpec defines the desired state of HardwarePlugin
type HardwarePluginSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// ApiRoot is the root URL for the Hardware Plugin.
	// +kubebuilder:validation:MinLength=1
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Hardware Plugin API root",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ApiRoot string `json:"apiRoot"`
}

// HardwarePluginStatus defines the observed state of HardwarePlugin
type HardwarePluginStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// HardwarePlugin is the Schema for the hardwareplugins API
type HardwarePlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HardwarePluginSpec   `json:"spec,omitempty"`
	Status HardwarePluginStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HardwarePluginList contains a list of HardwarePlugin
type HardwarePluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HardwarePlugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HardwarePlugin{}, &HardwarePluginList{})
}
