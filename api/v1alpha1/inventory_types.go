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

type ServerConfig struct {
	// Enabled indicates if the server should be started.
	//
	//+kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// MetadataServerConfig contains the configuration for the metadata server.
type MetadataServerConfig struct {
	//+kubebuilder:default:={enabled:true}
	ServerConfig `json:",inline"`
}

// DeploymentManagerServerConfig contains the configuration for the deployment manager server.
type DeploymentManagerServerConfig struct {
	//+kubebuilder:default:={enabled:true}
	ServerConfig `json:",inline"`
	//+optional
	BackendURL string `json:"backendURL,omitempty"`
	//+optional
	BackendToken string `json:"backendToken,omitempty"`
	//+kubebuilder:default=regular-hub
	//+kubebuilder:validation:Enum=regular-hub;global-hub
	BackendType string `json:"backendType,omitempty"`
	// This field allows the addition of extra O-Cloud information for the deployment manager server.
	//+optional
	Extensions []string `json:"extensions,omitempty"`
}

// ResourceServerConfig contains the configuration for the resource server.
type ResourceServerConfig struct {
	//+kubebuilder:default:={enabled:true}
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:bool"}
	ServerConfig `json:",inline"`
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backend URL",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BackendURL string `json:"backendURL,omitempty"`
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backend Token",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BackendToken string `json:"backendToken,omitempty"`
	// This field allows the addition of extra O-Cloud information for the resource server.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extensions",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Extensions []string `json:"extensions,omitempty"`
}

// AlarmSubscriptionServerConfig contains the configuration for the alarm subscription server.
type AlarmSubscriptionServerConfig struct {
	//+kubebuilder:default:={enabled:true}
	ServerConfig `json:",inline"`
}

// InventorySpec defines the desired state of Inventory
type InventorySpec struct {
	// Image is the full reference of the container image that contains the binary. This is
	// optional and the default will be the value passed to the `--image` command line flag of
	// the controller manager.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Image string `json:"image"`
	// CloudId is used to correlate the SMO inventory record with the deployed cloud instance.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cloud Id",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	CloudId string `json:"cloudId"`
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metadata Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	MetadataServerConfig MetadataServerConfig `json:"metadataServerConfig"`
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Deployment Manager Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DeploymentManagerServerConfig DeploymentManagerServerConfig `json:"deploymentManagerServerConfig"`
	// ResourceServerConfig contains the configuration for the resource server.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ResourceServerConfig ResourceServerConfig `json:"resourceServerConfig,omitempty"`
	// AlarmSubscriptionServerConfig contains the configuration for the alarm server.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Alarm Subscription Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	AlarmSubscriptionServerConfig AlarmSubscriptionServerConfig `json:"alarmSubscriptionServerConfig"`
	// IngressHost defines the FQDN for the IMS endpoints.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressHost string `json:"ingressHost,omitempty"`
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

type UsedServerConfig struct {
	MetadataServerUsedConfig          []string `json:"metadataServerUsedConfig,omitempty"`
	ResourceServerUsedConfig          []string `json:"resourceServerUsedConfig,omitempty"`
	DeploymentManagerServerUsedConfig []string `json:"deploymentManagerServerUsedConfig,omitempty"`
}

// InventoryStatus defines the observed state of Inventory
type InventoryStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployments Status"
	DeploymentsStatus DeploymentsStatus `json:"deploymentStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployments Status"
	UsedServerConfig UsedServerConfig `json:"usedServerConfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// Inventory is the Schema for the Inventory API
// +operator-sdk:csv:customresourcedefinitions:displayName="ORAN O2IMS Inventory",resources={{Deployment,apps/v1}}
type Inventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InventorySpec   `json:"spec,omitempty"`
	Status InventoryStatus `json:"status,omitempty"`
}

// InventoryList contains a list of Inventory
//
// +kubebuilder:object:root=true
type InventoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Inventory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Inventory{}, &InventoryList{})
}
