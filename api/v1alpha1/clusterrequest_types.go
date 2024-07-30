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
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterRequestSpec defines the desired state of ClusterRequest
type ClusterRequestSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// LocationSpec is the geographical location of the requested node.
	hwv1alpha1.LocationSpec `json:",inline"`

	// Reference to an existing clusterTemplate CR.
	ClusterTemplateRef string `json:"clusterTemplateRef"`

	ClusterTemplateInput ClusterTemplateInput `json:"clusterTemplateInput"`
}

// ClusterTemplateInput provides the input data that follows the schema defined in the referenced ClusterTemplate.
type ClusterTemplateInput struct {
	// ClusterInstanceInput provides the input values required for provisioning.
	// The input follows the schema defined in the referenced ClusterTemplate's spec.inputDataSchema.clusterInstanceSchema.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	ClusterInstanceInput runtime.RawExtension `json:"clusterInstanceInput"`

	// PolicyTemplateInput provides input values for ACM configuration policies.
	// The input follows the schema defined in the referenced ClusterTemplate's
	// spec.inputDataSchema.policyTemplateSchema.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	PolicyTemplateInput runtime.RawExtension `json:"policyTemplateInput"`
}

type NodePoolRef struct {
	// Contains the name of the created NodePool.
	Name string `json:"name,omitempty"`
	// Contains the namespace of the created NodePool.
	Namespace string `json:"namespace,omitempty"`
}

type ClusterInstanceRef struct {
	// Contains the name of the created ClusterInstance.
	Name string `json:"name,omitempty"`

	// Says if ZTP has complete or not.
	ZtpStatus string `json:"ztpStatus,omitempty"`
}

// ClusterRequestStatus defines the observed state of ClusterRequest
type ClusterRequestStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClusterInstanceRef references to the ClusterInstance.
	ClusterInstanceRef *ClusterInstanceRef `json:"clusterInstanceRef,omitempty"`

	// NodePoolRef references to the NodePool
	NodePoolRef *NodePoolRef `json:"nodePoolRef,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterRequest is the Schema for the clusterrequests API
type ClusterRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterRequestSpec   `json:"spec,omitempty"`
	Status ClusterRequestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterRequestList contains a list of ClusterRequest
type ClusterRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRequest{}, &ClusterRequestList{})
}
