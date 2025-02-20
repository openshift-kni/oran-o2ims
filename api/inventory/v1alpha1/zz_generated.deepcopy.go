//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AlarmServerConfig) DeepCopyInto(out *AlarmServerConfig) {
	*out = *in
	in.ServerConfig.DeepCopyInto(&out.ServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AlarmServerConfig.
func (in *AlarmServerConfig) DeepCopy() *AlarmServerConfig {
	if in == nil {
		return nil
	}
	out := new(AlarmServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ArtifactsServerConfig) DeepCopyInto(out *ArtifactsServerConfig) {
	*out = *in
	in.ServerConfig.DeepCopyInto(&out.ServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ArtifactsServerConfig.
func (in *ArtifactsServerConfig) DeepCopy() *ArtifactsServerConfig {
	if in == nil {
		return nil
	}
	out := new(ArtifactsServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterServerConfig) DeepCopyInto(out *ClusterServerConfig) {
	*out = *in
	in.ServerConfig.DeepCopyInto(&out.ServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterServerConfig.
func (in *ClusterServerConfig) DeepCopy() *ClusterServerConfig {
	if in == nil {
		return nil
	}
	out := new(ClusterServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Inventory) DeepCopyInto(out *Inventory) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Inventory.
func (in *Inventory) DeepCopy() *Inventory {
	if in == nil {
		return nil
	}
	out := new(Inventory)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Inventory) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InventoryList) DeepCopyInto(out *InventoryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Inventory, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InventoryList.
func (in *InventoryList) DeepCopy() *InventoryList {
	if in == nil {
		return nil
	}
	out := new(InventoryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *InventoryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InventorySpec) DeepCopyInto(out *InventorySpec) {
	*out = *in
	if in.Image != nil {
		in, out := &in.Image, &out.Image
		*out = new(string)
		**out = **in
	}
	if in.CloudID != nil {
		in, out := &in.CloudID, &out.CloudID
		*out = new(string)
		**out = **in
	}
	if in.ResourceServerConfig != nil {
		in, out := &in.ResourceServerConfig, &out.ResourceServerConfig
		*out = new(ResourceServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ClusterServerConfig != nil {
		in, out := &in.ClusterServerConfig, &out.ClusterServerConfig
		*out = new(ClusterServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.AlarmServerConfig != nil {
		in, out := &in.AlarmServerConfig, &out.AlarmServerConfig
		*out = new(AlarmServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ArtifactsServerConfig != nil {
		in, out := &in.ArtifactsServerConfig, &out.ArtifactsServerConfig
		*out = new(ArtifactsServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ProvisioningServerConfig != nil {
		in, out := &in.ProvisioningServerConfig, &out.ProvisioningServerConfig
		*out = new(ProvisioningServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.IngressHost != nil {
		in, out := &in.IngressHost, &out.IngressHost
		*out = new(string)
		**out = **in
	}
	if in.SmoConfig != nil {
		in, out := &in.SmoConfig, &out.SmoConfig
		*out = new(SmoConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.CaBundleName != nil {
		in, out := &in.CaBundleName, &out.CaBundleName
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InventorySpec.
func (in *InventorySpec) DeepCopy() *InventorySpec {
	if in == nil {
		return nil
	}
	out := new(InventorySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InventoryStatus) DeepCopyInto(out *InventoryStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.UsedServerConfig.DeepCopyInto(&out.UsedServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InventoryStatus.
func (in *InventoryStatus) DeepCopy() *InventoryStatus {
	if in == nil {
		return nil
	}
	out := new(InventoryStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OAuthConfig) DeepCopyInto(out *OAuthConfig) {
	*out = *in
	if in.Scopes != nil {
		in, out := &in.Scopes, &out.Scopes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OAuthConfig.
func (in *OAuthConfig) DeepCopy() *OAuthConfig {
	if in == nil {
		return nil
	}
	out := new(OAuthConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProvisioningServerConfig) DeepCopyInto(out *ProvisioningServerConfig) {
	*out = *in
	in.ServerConfig.DeepCopyInto(&out.ServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProvisioningServerConfig.
func (in *ProvisioningServerConfig) DeepCopy() *ProvisioningServerConfig {
	if in == nil {
		return nil
	}
	out := new(ProvisioningServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceServerConfig) DeepCopyInto(out *ResourceServerConfig) {
	*out = *in
	in.ServerConfig.DeepCopyInto(&out.ServerConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceServerConfig.
func (in *ResourceServerConfig) DeepCopy() *ResourceServerConfig {
	if in == nil {
		return nil
	}
	out := new(ResourceServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServerConfig) DeepCopyInto(out *ServerConfig) {
	*out = *in
	if in.ClientTLS != nil {
		in, out := &in.ClientTLS, &out.ClientTLS
		*out = new(TLSConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServerConfig.
func (in *ServerConfig) DeepCopy() *ServerConfig {
	if in == nil {
		return nil
	}
	out := new(ServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SmoConfig) DeepCopyInto(out *SmoConfig) {
	*out = *in
	if in.OAuthConfig != nil {
		in, out := &in.OAuthConfig, &out.OAuthConfig
		*out = new(OAuthConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = new(TLSConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SmoConfig.
func (in *SmoConfig) DeepCopy() *SmoConfig {
	if in == nil {
		return nil
	}
	out := new(SmoConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSConfig) DeepCopyInto(out *TLSConfig) {
	*out = *in
	if in.ClientCertificateName != nil {
		in, out := &in.ClientCertificateName, &out.ClientCertificateName
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSConfig.
func (in *TLSConfig) DeepCopy() *TLSConfig {
	if in == nil {
		return nil
	}
	out := new(TLSConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UsedServerConfig) DeepCopyInto(out *UsedServerConfig) {
	*out = *in
	if in.ArtifactsServerUsedConfig != nil {
		in, out := &in.ArtifactsServerUsedConfig, &out.ArtifactsServerUsedConfig
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.AlarmsServerUsedConfig != nil {
		in, out := &in.AlarmsServerUsedConfig, &out.AlarmsServerUsedConfig
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ClusterServerUsedConfig != nil {
		in, out := &in.ClusterServerUsedConfig, &out.ClusterServerUsedConfig
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ResourceServerUsedConfig != nil {
		in, out := &in.ResourceServerUsedConfig, &out.ResourceServerUsedConfig
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ProvisioningServerUsedConfig != nil {
		in, out := &in.ProvisioningServerUsedConfig, &out.ProvisioningServerUsedConfig
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UsedServerConfig.
func (in *UsedServerConfig) DeepCopy() *UsedServerConfig {
	if in == nil {
		return nil
	}
	out := new(UsedServerConfig)
	in.DeepCopyInto(out)
	return out
}
