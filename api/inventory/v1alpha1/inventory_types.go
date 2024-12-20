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

// OAuthConfig defines the configurable attributes that represent the authentication mechanism.  This is currently
// expected to be a way to acquire a token from an OAuth2 server.
type OAuthConfig struct {
	// Url represents the base URL of the authorization server. (e.g., https://keycloak.example.com/realms/oran)
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OAuth URL",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Url string `json:"url"`
	// TokenEndpoint represents the API endpoint used to acquire a token (e.g., /protocol/openid-connect/token) which
	// will be appended to the base URL to form the full URL
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OAuth Token Endpoint",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TokenEndpoint string `json:"tokenEndpoint"`
	// ClientSecretName represents the name of a secret (in the current namespace) which contains the client-id and
	// client-secret values used by the OAuth client.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Client Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClientSecretName string `json:"clientSecretName"`
	// Scopes represents the OAuth scope values to request when acquiring a token.  Typically, this should be set to
	// "openid" in addition to any other scopes that the SMO specifically requires (e.g., "roles", "groups", etc...) to
	// authorize our requests
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OAuth Scopes",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Scopes []string `json:"scopes"`
}

// TlsConfig defines the TLS specific attributes specific to the SMO and OAuth servers
type TlsConfig struct {
	// ClientCertificateName represents the name of a secret (in the current namespace) which contains an X.509
	// certificate and private key to be used when initiating connections to the SMO and OAuth servers.  The secret is
	// expected to contain a 'tls.key' and 'tls.crt' keys.  If the client is signed by intermediate CA certificate(s)
	// then it is expected that the full chain is to be appended to the certificate file with the device certificate
	// being first and the root CA being last.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TLS client certificate"
	ClientCertificateName *string `json:"clientCertificateName"`
}

// SmoConfig defines the configurable attributes to represent the SMO instance
type SmoConfig struct {
	// Url represents the base URL of the SMO instance
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SMO URL",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Url string `json:"url"`
	// RegistrationEndpoint represents the API endpoint used to register the O2IMS with the SMO.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Registration API Endpoint",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	RegistrationEndpoint string `json:"registrationEndpoint"`
	// OAuthConfig defines the configurable attributes required to access the OAuth2 authorization server
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SMO OAuth Configuration"
	OAuthConfig *OAuthConfig `json:"oauth,omitempty"`
	// TlsConfig defines the TLS attributes specific to the SMO and OAuth servers
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TLS Configuration"
	Tls *TlsConfig `json:"tls"`
}

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

// ClusterServerConfig contains the configuration for the cluster server.
type ClusterServerConfig struct {
	//+kubebuilder:default:={enabled:true}
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:bool"}
	ServerConfig `json:",inline"`
}

// AlarmServerConfig contains the configuration for the alarm server.
type AlarmServerConfig struct {
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
	Image *string `json:"image,omitempty"`
	// CloudID is the global cloud ID value used to correlate the SMO inventory record with the deployed cloud instance.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cloud ID",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	CloudID *string `json:"cloudID"`
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
	// ClusterServerConfig contains the configuration for the resource server.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterServerConfig ClusterServerConfig `json:"clusterServerConfig,omitempty"`
	// AlarmServerConfig contains the configuration for the alarm server.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Alarm Server Configuration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	AlarmServerConfig AlarmServerConfig `json:"alarmServerConfig"`
	// IngressHost defines the FQDN for the IMS endpoints.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressHost *string `json:"ingressHost,omitempty"`
	// SmoConfig defines the configurable attributes to represent the SMO instance
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SMO Configuration"
	SmoConfig *SmoConfig `json:"smo,omitempty"`
	// CaBundleName references a config map that contains a set of custom CA certificates to be used when communicating
	// with any outside entity (e.g., the SMO, the authorization server, etc.) that has its TLS certificate signed by
	// a non-public CA certificate.
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Custom CA Certificates",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	CaBundleName *string `json:"caBundleName,omitempty"`
}

type DeploymentsStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployment Server Status"
	DeploymentServerStatus string `json:"deploymentServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Metadata Server Status"
	MetadataServerStatus string `json:"metadataServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Resource Server Status"
	ResourceServerStatus string `json:"resourceServerStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Alarm Server Status"
	AlarmServerStatus string `json:"alarmServerStatus,omitempty"`
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
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployments Status"
	DeploymentsStatus DeploymentsStatus `json:"deploymentStatus,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployments Status"
	UsedServerConfig UsedServerConfig `json:"usedServerConfig,omitempty"`
	// Stores the ingress host domain resolved at runtime; either from a user override or automatically computed from
	// the default ingress controller.
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Resolved Ingress Host Address"
	IngressHost string `json:"ingressHost,omitempty"`
	// Stores the local cluster ID used as the local Cloud ID value.
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Local Cluster ID"
	ClusterID string `json:"clusterID,omitempty"`
	// Stores the Search API URL resolved at runtime; either from a user override or automatically computed from the
	// Search API service.
	SearchURL string `json:"searchURL,omitempty"`
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
