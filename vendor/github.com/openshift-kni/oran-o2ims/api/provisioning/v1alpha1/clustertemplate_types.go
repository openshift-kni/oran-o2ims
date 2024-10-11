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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterTemplateSpec defines the desired state of ClusterTemplate
type ClusterTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Name defines a Human readable name of the Template.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
	// Description defines a Human readable description of the Template.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Description",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Description string `json:"description,omitempty"`
	// Version defines a version or generation of the resource as defined by its provider.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Version",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Version string `json:"version"`
	// TemplateId defines a Identifier for the O-Cloud Template. This identifier is allocated by the O-Cloud.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TemplateId",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TemplateID string `json:"templateId,omitempty"`
	// Characteristics defines a List of key/value pairs describing characteristics associated with the template.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Characteristics",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Characteristics map[string]string `json:"characteristics,omitempty"`
	// Metadata defines a List of key/value pairs describing metadata associated with the template.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metadata",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Metadata map[string]string `json:"metadata,omitempty"`
	// Templates defines the references to the templates required for ClusterTemplate.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Templates",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Templates Templates `json:"templates"`
	// TemplateParameterSchema defines the parameters required for ClusterTemplate.
	// The parameter definitions should follow the OpenAPI V3 schema and
	// explicitly define required fields.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Template Parameter Schema",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TemplateParameterSchema runtime.RawExtension `json:"templateParameterSchema"`
}

// Templates defines the references to the templates required for ClusterTemplate.
type Templates struct {
	// HwTemplate defines a reference to a hardware template config map
	HwTemplate string `json:"hwTemplate"`

	// ClusterInstanceDefaults defines a reference to a configmap with
	// default values for ClusterInstance
	ClusterInstanceDefaults string `json:"clusterInstanceDefaults"`
	// PolicyTemplateDefaults defines a reference to a configmap with
	// default values for ACM policies
	PolicyTemplateDefaults string `json:"policyTemplateDefaults"`
}

// ClusterTemplateStatus defines the observed state of ClusterTemplate
type ClusterTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//+operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterTemplate is the Schema for the clustertemplates API
// +kubebuilder:validation:XValidation:message="Spec changes are not allowed for a ClusterTemplate that has passed the validation", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='ClusterTemplateValidated' && c.status=='False') || oldSelf.spec == self.spec"
// +kubebuilder:validation:XValidation:message="metadata.name must be in the form of spec.name + '.' + spec.version", rule="self.metadata.name == (self.spec.name + '.' + self.spec.version)"
// +operator-sdk:csv:customresourcedefinitions:displayName="ORAN O2IMS Cluster Template",resources={{ConfigMap, v1}}
type ClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterTemplateSpec   `json:"spec,omitempty"`
	Status ClusterTemplateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterTemplateList contains a list of ClusterTemplate
type ClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterTemplate{}, &ClusterTemplateList{})
}
