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

	Templates       Templates       `json:"templates"`
	InputDataSchema InputDataSchema `json:"inputDataSchema"`
}

// Templates defines the references to the templates required for ClusterTemplate.
type Templates struct {
	// HwTemplate defines a reference to a hardware template config map
	HwTemplate string `json:"hwTemplate"`

	// ClusterInstanceDefaults defines a reference to a configmap with default values for ClusterInstance
	ClusterInstanceDefaults string `json:"clusterInstanceDefaults"`
}

// InputDataSchema encapsulates all the schemas required for ClusterTemplate.
type InputDataSchema struct {
	// ClusterInstanceSchema defines the parameters required for provisioning.
	// The parameter definitions should follow the OpenAPI V3 schema and explicitly define required fields.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	ClusterInstanceSchema runtime.RawExtension `json:"clusterInstanceSchema"`

	// TODO: Additional parameters can be added here
	// PolicyTemplateSchema
}

// ClusterTemplateStatus defines the observed state of ClusterTemplate
type ClusterTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterTemplate is the Schema for the clustertemplates API
// +kubebuilder:validation:XValidation:message="Spec changes are not allowed for a ClusterTemplate that has passed the validation", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='ClusterTemplateValidated' && c.status=='False') || oldSelf.spec == self.spec"
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
