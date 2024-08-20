/*
Copyright 2024.

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
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MachineNetworkEntry is a single IP address block for node IP blocks.
type MachineNetworkEntry struct {
	// CIDR is the IP block address pool for machines within the cluster.
	// +required
	CIDR string `json:"cidr"`
}

// ClusterNetworkEntry is a single IP address block for pod IP blocks. IP blocks
// are allocated with size 2^HostSubnetLength.
type ClusterNetworkEntry struct {
	// CIDR is the IP block address pool.
	// +required
	CIDR string `json:"cidr"`

	// HostPrefix is the prefix size to allocate to each node from the CIDR.
	// For example, 24 would allocate 2^8=256 adresses to each node. If this
	// field is not used by the plugin, it can be left unset.
	// +optional
	HostPrefix int32 `json:"hostPrefix,omitempty"`
}

// ServiceNetworkEntry is a single IP address block for node IP blocks.
type ServiceNetworkEntry struct {
	// CIDR is the IP block address pool for machines within the cluster.
	// +required
	CIDR string `json:"cidr"`
}

// BmcCredentialsName
type BmcCredentialsName struct {
	// +required
	Name string `json:"name"`
}

// IronicInspect
type IronicInspect string

type TangConfig struct {
	URL        string `json:"url,omitempty"`
	Thumbprint string `json:"thumbprint,omitempty"`
}

type DiskEncryption struct {
	// +kubebuilder:default:=none
	Type string       `json:"type,omitempty"`
	Tang []TangConfig `json:"tang,omitempty"`
}

// CPUPartitioningMode is used to drive how a cluster nodes CPUs are Partitioned.
type CPUPartitioningMode string

const (
	// The only supported configurations are an all or nothing configuration.
	CPUPartitioningNone     CPUPartitioningMode = "None"
	CPUPartitioningAllNodes CPUPartitioningMode = "AllNodes"
)

// TemplateRef is used to specify the installation CR templates
type TemplateRef struct {
	// +required
	Name string `json:"name"`
	// +required
	Namespace string `json:"namespace"`
}

// NodeSpec
type NodeSpec struct {
	// BmcAddress holds the URL for accessing the controller on the network.
	// +required
	BmcAddress string `json:"bmcAddress"`

	// BmcCredentialsName is the name of the secret containing the BMC credentials (requires keys "username" and "password").
	// +required
	BmcCredentialsName BmcCredentialsName `json:"bmcCredentialsName"`

	// Which MAC address will PXE boot? This is optional for some
	// types, but required for libvirt VMs driven by vbmc.
	// +kubebuilder:validation:Pattern=`[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}`
	// +required
	BootMACAddress string `json:"bootMACAddress"`

	// When set to disabled, automated cleaning will be avoided during provisioning and deprovisioning.
	// Set the value to metadata to enable the removal of the disk’s partitioning table only, without fully wiping the disk. The default value is disabled.
	// +optional
	// +kubebuilder:default:=disabled
	AutomatedCleaningMode bmh_v1alpha1.AutomatedCleaningMode `json:"automatedCleaningMode,omitempty"`

	// RootDeviceHints specifies the device for deployment.
	// Identifiers that are stable across reboots are recommended, for example, wwn: <disk_wwn> or deviceName: /dev/disk/by-path/<device_path>
	// +optional
	RootDeviceHints *bmh_v1alpha1.RootDeviceHints `json:"rootDeviceHints,omitempty"`

	// NodeNetwork is a set of configurations pertaining to the network settings for the node.
	// +optional
	NodeNetwork *aiv1beta1.NMStateConfigSpec `json:"nodeNetwork,omitempty"`

	// NodeLabels allows the specification of custom roles for your nodes in your managed clusters.
	// These are additional roles are not used by any OpenShift Container Platform components, only by the user.
	// When you add a custom role, it can be associated with a custom machine config pool that references a specific configuration for that role.
	// Adding custom labels or roles during installation makes the deployment process more effective and prevents the need for additional reboots
	// after the installation is complete.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`

	// Hostname is the desired hostname for the host
	// +required
	HostName string `json:"hostName"`

	// Provide guidance about how to choose the device for the image being provisioned.
	// +kubebuilder:default:=UEFI
	// +optional
	BootMode bmh_v1alpha1.BootMode `json:"bootMode,omitempty"`

	// Json formatted string containing the user overrides for the host's coreos installer args
	// +optional
	InstallerArgs string `json:"installerArgs,omitempty"`

	// Json formatted string containing the user overrides for the host's ignition config
	// IgnitionConfigOverride enables the assignment of partitions for persistent storage.
	// Adjust disk ID and size to the specific hardware.
	// +optional
	IgnitionConfigOverride string `json:"ignitionConfigOverride,omitempty"`

	// +kubebuilder:validation:Enum=master;worker
	// +kubebuilder:default:=master
	// +optional
	Role string `json:"role,omitempty"`

	// Additional node-level annotations to be applied to the rendered templates
	// +optional
	ExtraAnnotations map[string]map[string]string `json:"extraAnnotations,omitempty"`

	// SuppressedManifests is a list of node-level manifest names to be excluded from the template rendering process
	// +optional
	SuppressedManifests []string `json:"suppressedManifests,omitempty"`

	// IronicInspect is used to specify if automatic introspection carried out during registration of BMH is enabled or disabled
	// +kubebuilder:default:=""
	// +optional
	IronicInspect IronicInspect `json:"ironicInspect,omitempty"`

	// TemplateRefs is a list of references to node-level templates. A node-level template consists of a ConfigMap
	// in which the keys of the data field represent the kind of the installation manifest(s).
	// Node-level templates are instantiated once for each node in the ClusterInstance CR.
	// +required
	TemplateRefs []TemplateRef `json:"templateRefs"`
}

// ClusterType is a string representing the cluster type
type ClusterType string

const (
	ClusterTypeSNO             ClusterType = "SNO"
	ClusterTypeHighlyAvailable ClusterType = "HighlyAvailable"
)

// ClusterInstanceSpec defines the desired state of ClusterInstance
type ClusterInstanceSpec struct {
	// Desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ClusterName is the name of the cluster.
	// +required
	ClusterName string `json:"clusterName"`

	// PullSecretRef is the reference to the secret to use when pulling images.
	// +required
	PullSecretRef corev1.LocalObjectReference `json:"pullSecretRef"`

	// ClusterImageSetNameRef is the name of the ClusterImageSet resource indicating which
	// OpenShift version to deploy.
	// +required
	ClusterImageSetNameRef string `json:"clusterImageSetNameRef"`

	// SSHPublicKey is the public Secure Shell (SSH) key to provide access to instances.
	// This key will be added to the host to allow ssh access
	// +optional
	SSHPublicKey string `json:"sshPublicKey,omitempty"`

	// BaseDomain is the base domain to use for the deployed cluster.
	// +required
	BaseDomain string `json:"baseDomain"`

	// APIVIPs are the virtual IPs used to reach the OpenShift cluster's API.
	// Enter one IP address for single-stack clusters, or up to two for dual-stack clusters (at
	// most one IP address per IP stack used). The order of stacks should be the same as order
	// of subnets in Cluster Networks, Service Networks, and Machine Networks.
	// +kubebuilder:validation:MaxItems=2
	// +optional
	ApiVIPs []string `json:"apiVIPs,omitempty"`

	// IngressVIPs are the virtual IPs used for cluster ingress traffic.
	// Enter one IP address for single-stack clusters, or up to two for dual-stack clusters (at
	// most one IP address per IP stack used). The order of stacks should be the same as order
	// of subnets in Cluster Networks, Service Networks, and Machine Networks.
	// +kubebuilder:validation:MaxItems=2
	// +optional
	IngressVIPs []string `json:"ingressVIPs,omitempty"`

	// HoldInstallation will prevent installation from happening when true.
	// Inspection and validation will proceed as usual, but once the RequirementsMet condition is true,
	// installation will not begin until this field is set to false.
	// +kubebuilder:default:=false
	// +optional
	HoldInstallation bool `json:"holdInstallation,omitempty"`

	// AdditionalNTPSources is a list of NTP sources (hostname or IP) to be added to all cluster
	// hosts. They are added to any NTP sources that were configured through other means.
	// +optional
	AdditionalNTPSources []string `json:"additionalNTPSources,omitempty"`

	// MachineNetwork is the list of IP address pools for machines.
	// +optional
	MachineNetwork []MachineNetworkEntry `json:"machineNetwork,omitempty"`

	// ClusterNetwork is the list of IP address pools for pods.
	// +optional
	ClusterNetwork []ClusterNetworkEntry `json:"clusterNetwork,omitempty"`

	// ServiceNetwork is the list of IP address pools for services.
	// +optional
	ServiceNetwork []ServiceNetworkEntry `json:"serviceNetwork,omitempty"`

	// NetworkType is the Container Network Interface (CNI) plug-in to install
	// The default value is OpenShiftSDN for IPv4, and OVNKubernetes for IPv6 or SNO
	// +kubebuilder:validation:Enum=OpenShiftSDN;OVNKubernetes
	// +kubebuilder:default:=OVNKubernetes
	// +optional
	NetworkType string `json:"networkType,omitempty"`

	// Additional cluster-wide annotations to be applied to the rendered templates
	// +optional
	ExtraAnnotations map[string]map[string]string `json:"extraAnnotations,omitempty"`

	// ClusterLabels is used to assign labels to the cluster to assist with policy binding.
	// +optional
	ClusterLabels map[string]string `json:"clusterLabels,omitempty"`

	// InstallConfigOverrides is a Json formatted string that provides a generic way of passing
	// install-config parameters.
	// +optional
	InstallConfigOverrides string `json:"installConfigOverrides,omitempty"`

	// Json formatted string containing the user overrides for the initial ignition config
	// +optional
	IgnitionConfigOverride string `json:"ignitionConfigOverride,omitempty"`

	// DiskEncryption is the configuration to enable/disable disk encryption for cluster nodes.
	// +optional
	DiskEncryption *DiskEncryption `json:"diskEncryption,omitempty"`

	// Proxy defines the proxy settings used for the install config
	// +optional
	Proxy *aiv1beta1.Proxy `json:"proxy,omitempty"`

	// ExtraManifestsRefs is list of config map references containing additional manifests to be applied to the cluster.
	// +optional
	ExtraManifestsRefs []corev1.LocalObjectReference `json:"extraManifestsRefs,omitempty"`

	// SuppressedManifests is a list of manifest names to be excluded from the template rendering process
	// +optional
	SuppressedManifests []string `json:"suppressedManifests,omitempty"`

	// CPUPartitioning determines if a cluster should be setup for CPU workload partitioning at install time.
	// When this field is set the cluster will be flagged for CPU Partitioning allowing users to segregate workloads to
	// specific CPU Sets. This does not make any decisions on workloads it only configures the nodes to allow CPU Partitioning.
	// The "AllNodes" value will setup all nodes for CPU Partitioning, the default is "None".
	// +kubebuilder:validation:Enum=None;AllNodes
	// +kubebuilder:default=None
	// +optional
	CPUPartitioning CPUPartitioningMode `json:"cpuPartitioningMode,omitempty"`

	// +kubebuilder:validation:Enum=SNO;HighlyAvailable
	// +optional
	ClusterType ClusterType `json:"clusterType,omitempty"`

	// TemplateRefs is a list of references to cluster-level templates. A cluster-level template consists of a ConfigMap
	// in which the keys of the data field represent the kind of the installation manifest(s).
	// Cluster-level templates are instantiated once per cluster (ClusterInstance CR).
	// +required
	TemplateRefs []TemplateRef `json:"templateRefs"`

	// CABundle is a reference to a config map containing the new bundle of trusted certificates for the host.
	// The tls-ca-bundle.pem entry in the config map will be written to /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem
	// +optional
	CaBundleRef *corev1.LocalObjectReference `json:"caBundleRef,omitempty"`

	// +required
	Nodes []NodeSpec `json:"nodes"`
}

const (
	ManifestRenderedSuccess   = "rendered"
	ManifestRenderedFailure   = "failed"
	ManifestRenderedValidated = "validated"
	ManifestSuppressed        = "suppressed"
)

// ManifestReference contains enough information to let you locate the
// typed referenced object inside the same namespace.
// +structType=atomic
type ManifestReference struct {
	// APIGroup is the group for the resource being referenced.
	// If APIGroup is not specified, the specified Kind must be in the core API group.
	// For any other third-party types, APIGroup is required.
	// +required
	APIGroup *string `json:"apiGroup"`
	// Kind is the type of resource being referenced
	// +required
	Kind string `json:"kind"`
	// Name is the name of the resource being referenced
	// +required
	Name string `json:"name"`
	// Namespace is the namespace of the resource being referenced
	// +optional
	Namespace string `json:"namespace,omitempty"`
	//SyncWave is the order in which the resource should be processed: created in ascending order, deleted in descending order.
	// +required
	SyncWave int `json:"syncWave"`
	// Status is the status of the manifest
	// +required
	Status string `json:"status"`
	// lastAppliedTime is the last time the manifest was applied.
	// This should be when the underlying manifest changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	// +required
	LastAppliedTime metav1.Time `json:"lastAppliedTime"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +kubebuilder:validation:MaxLength=32768
	// +optional
	Message string `json:"message,omitempty"`
}

// ClusterInstanceStatus defines the observed state of ClusterInstance
type ClusterInstanceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors={"urn:alm:descriptor:io.kubernetes.conditions"}
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClusterDeploymentRef is a reference to the ClusterDeployment.
	// +optional
	ClusterDeploymentRef *corev1.LocalObjectReference `json:"clusterDeploymentRef,omitempty"`

	// Conditions is a list of conditions associated with syncing to the cluster.
	// +optional
	DeploymentConditions []hivev1.ClusterDeploymentCondition `json:"deploymentConditions,omitempty"`

	// List of manifests that have been rendered along with their status.
	// +optional
	ManifestsRendered []ManifestReference `json:"manifestsRendered,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=clusterinstances,scope=Namespaced
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.conditions[-1:].reason"
//+kubebuilder:printcolumn:name="Details",type="string",JSONPath=".status.conditions[-1:].message"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ClusterInstance is the Schema for the clusterinstances API
type ClusterInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterInstanceSpec   `json:"spec,omitempty"`
	Status ClusterInstanceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterInstanceList contains a list of ClusterInstance
type ClusterInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterInstance{}, &ClusterInstanceList{})
}
