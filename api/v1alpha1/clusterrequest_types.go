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

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Location Spec",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	hwv1alpha1.LocationSpec `json:",inline"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Template Reference",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterTemplateRef string `json:"clusterTemplateRef"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Template Input",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterTemplateInput ClusterTemplateInput `json:"clusterTemplateInput"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Timeout",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Timeout Timeout `json:"timeout,omitempty"`
}

// ClusterTemplateInput provides the input data that follows the schema defined in the referenced ClusterTemplate.
type ClusterTemplateInput struct {
	// ClusterInstanceInput provides the input values required for provisioning.
	// The input must adhere to the schema defined in the referenced ClusterTemplate's
	// spec.inputDataSchema.clusterInstanceSchema.
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

// NodePoolRef references a node pool.
type NodePoolRef struct {
	// Contains the name of the created NodePool.
	Name string `json:"name,omitempty"`
	// Contains the namespace of the created NodePool.
	Namespace string `json:"namespace,omitempty"`
}

type ClusterDetails struct {
	// Contains the name of the created ClusterInstance.
	Name string `json:"name,omitempty"`

	// Says if ZTP has complete or not.
	ZtpStatus string `json:"ztpStatus,omitempty"`

	// A timestamp indicating the cluster provisoning has started
	ClusterProvisionStartedAt metav1.Time `json:"clusterProvisionStartedAt,omitempty"`

	// Holds the first timestamp when the configuration was found NonCompliant for the cluster.
	NonCompliantAt metav1.Time `json:"nonCompliantAt,omitempty"`
}

// Timeout contains timeout values for hardware provisioning, cluster provisioning and
// cluster configuration.
type Timeout struct {
	// ClusterProvisioning defines the timeout for the initial cluster installation in minutes.
	//+kubebuilder:default=90
	ClusterProvisioning int `json:"clusterProvisioning,omitempty"`
	// HardwareProvisioning defines the timeout for the hardware provisioning in minutes.
	//+kubebuilder:default=90
	HardwareProvisioning int `json:"hardwareProvisioning,omitempty"`
	// Configuration defines the timeout for ACM policy configuration.
	//+kubebuilder:default=30
	Configuration int `json:"configuration,omitempty"`
}

// PolicyDetails holds information about an ACM policy.
type PolicyDetails struct {
	// The compliance of the ManagedCluster created through a ClusterRequest with the current
	// policy.
	Compliant string `json:"compliant,omitempty"`
	// The policy's name.
	PolicyName string `json:"policyName,omitempty"`
	// The policy's namespace.
	PolicyNamespace string `json:"policyNamespace,omitempty"`
	// The policy's remediation action.
	RemediationAction string `json:"remediationAction,omitempty"`
}

// ClusterRequestStatus defines the observed state of ClusterRequest
type ClusterRequestStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClusterDetails references to the ClusterInstance.
	ClusterDetails *ClusterDetails `json:"clusterDetails,omitempty"`

	// NodePoolRef references to the NodePool.
	NodePoolRef *NodePoolRef `json:"nodePoolRef,omitempty"`

	// Holds policies that are matched with the ManagedCluster created by the ClusterRequest.
	Policies []PolicyDetails `json:"policies,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterRequest is the Schema for the clusterrequests API
// +operator-sdk:csv:customresourcedefinitions:displayName="ORAN O2IMS Cluster Request",resources={{Namespace, v1},{ClusterInstance, siteconfig.open-cluster-management.io/v1alpha1}}
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
