/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Bios defines attributes as key value pairs
type Bios struct {

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Attributes map[string]intstr.IntOrString `json:"attributes,omitempty"`
}

// HardwareProfileSpec defines the desired state of HardwareProfile
type HardwareProfileSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Bios defines a set of bios attributes
	//+operator-sdk:csv:customresourcedefinitions:type=spec
	Bios Bios `json:"bios"`

	// BiosFirmware is the name of a firmware image entry in the FirmwareCatalog
	// for BIOS firmware.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	BiosFirmware string `json:"biosFirmware,omitempty"`

	// BmcFirmware is the name of a firmware image entry in the FirmwareCatalog
	// for BMC firmware.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	BmcFirmware string `json:"bmcFirmware,omitempty"`

	// NicFirmware is a list of firmware image entry names in the FirmwareCatalog
	// for NIC firmware.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	NicFirmware []string `json:"nicFirmware,omitempty"`
}

// HardwareProfileStatus defines the observed state of HardwareProfile
type HardwareProfileStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Represents the observations of a HardwareProfile's current state
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:Optional
	//+operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:validation:XValidation:message="HardwareProfile spec is immutable", rule="oldSelf.spec == self.spec"
// +operator-sdk:csv:customresourcedefinitions:displayName="Hardware Profile",resources={{ConfigMap, v1}}
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=hardwareprofiles,scope=Namespaced
// +kubebuilder:resource:shortName=hwprofile;hwprofiles
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the HardwareProfile resource."
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[-1:].reason"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[-1:].status"
// +kubebuilder:printcolumn:name="Details",type="string",JSONPath=".status.conditions[-1:].message"

// HardwareProfile is the Schema for the hardwareprofiles API
type HardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HardwareProfileSpec   `json:"spec,omitempty"`
	Status HardwareProfileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HardwareProfileList contains a list of HardwareProfile
type HardwareProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HardwareProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HardwareProfile{}, &HardwareProfileList{})
}
