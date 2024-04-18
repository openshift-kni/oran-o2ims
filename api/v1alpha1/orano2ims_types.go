/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ORANO2IMSSpec defines the desired state of ORANO2IMS
type ORANO2IMSSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Image is the full reference of the container image that contains the binary. This is
	// optional and the default will be the value passed to the `--image` command line flag of
	// the controller manager.
	//
	//+optional
	Image string `json:"image"`

	CloudId string `json:"cloudId"`
	//+kubebuilder:default=false
	MetadataServer bool `json:"metadataServer"`
	//+kubebuilder:default=false
	DeploymentManagerServer bool `json:"deploymentManagerServer"`
	//+kubebuilder:default=false
	ResourceServer          bool `json:"resourceServer"`
	AlarmSubscriptionServer bool `json:"alarmSubscriptionServer"`
	//+optional
	IngressHost string `json:"ingressHost,omitempty"`
	//+optional
	BackendURL string `json:"backendURL,omitempty"`
	//+optional
	SearchAPIBackendURL string `json:"searchAPIBackendURL,omitempty"`
	//+optional
	BackendToken string `json:"backendToken,omitempty"`
	//+kubebuilder:default=regular-hub
	//+kubebuilder:validation:Enum=regular-hub;global-hub
	BackendType string `json:"backendType,omitempty"`
	// This field allows the addition of extra O-Cloud information
	//+optional
	Extensions []string `json:"extensions,omitempty"`
}

type DeploymentsStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployment Server Status"
	DeploymentServerStatus string `json:"deploymentServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Metadata Server Status"
	MetadataServerStatus string `json:"metadataServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Resource Server Status"
	ResourceServerStatus string `json:"resourceServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ORANO2IMSStatus defines the observed state of ORANO2IMS
type ORANO2IMSStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	DeploymentsStatus DeploymentsStatus `json:"deploymentStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ORANO2IMS is the Schema for the orano2ims API
type ORANO2IMS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ORANO2IMSSpec   `json:"spec,omitempty"`
	Status ORANO2IMSStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ORANO2IMSList contains a list of ORANO2IMS
type ORANO2IMSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ORANO2IMS `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ORANO2IMS{}, &ORANO2IMSList{})
}
