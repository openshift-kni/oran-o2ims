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

// Firmware holds an inline firmware version and download URL.
type Firmware struct {
	// Version is the desired firmware version
	Version string `json:"version,omitempty"`
	// URL points to the firmware file
	URL string `json:"url,omitempty"`
}

// Nic holds an inline NIC firmware version and download URL.
type Nic struct {
	// Version is the NIC firmware version
	Version string `json:"version,omitempty"`
	// URL points to the NIC firmware file
	URL string `json:"url,omitempty"`
}

// IsEmpty returns true when both Version and URL are empty.
func (fm Firmware) IsEmpty() bool {
	return fm.Version == "" && fm.URL == ""
}

// HardwareProfileSpec defines the desired state of HardwareProfile
type HardwareProfileSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Bios defines a set of bios attributes
	//+operator-sdk:csv:customresourcedefinitions:type=spec
	Bios Bios `json:"bios"`

	// BiosFirmware is the inline BIOS firmware (version and url).
	//
	// Deprecated: use FirmwareImages with FirmwareCatalog entries instead.
	// Mutually exclusive with FirmwareImages.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="BIOS Firmware",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BiosFirmware Firmware `json:"biosFirmware,omitempty"`

	// BmcFirmware is the inline BMC firmware (version and url).
	//
	// Deprecated: use FirmwareImages with FirmwareCatalog entries instead.
	// Mutually exclusive with FirmwareImages.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="BMC Firmware",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BmcFirmware Firmware `json:"bmcFirmware,omitempty"`

	// NicFirmware is the inline NIC firmware list (version and url per entry).
	//
	// Deprecated: use FirmwareImages with FirmwareCatalog entries instead.
	// Mutually exclusive with FirmwareImages.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NIC Firmware",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	NicFirmware []Nic `json:"nicFirmware,omitempty"`

	// FirmwareImages is a list of FirmwareCatalog entry names. Each entry is
	// looked up in the singleton FirmwareCatalog to resolve its component type
	// (bios, bmc, or nic), URL, and version. At most one entry may resolve to
	// component "bios" and at most one to component "bmc"; multiple "nic"
	// entries are allowed.
	//
	// Mutually exclusive with the deprecated BiosFirmware, BmcFirmware, and
	// NicFirmware fields.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Firmware Images",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	FirmwareImages []string `json:"firmwareImages,omitempty"`
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
