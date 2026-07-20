/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package fakeclient

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	openshiftv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	openshiftoperatorv1 "github.com/openshift/api/operator/v1"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

const inventoryCRDName = "inventories.ocloud.openshift.io"

// Scheme is the shared scheme for all unit tests that use GetFakeClientFromObjects.
// All custom resource types are registered here so callers don't need to manage
// scheme setup individually. Uses a dedicated scheme rather than the global
// clientgoscheme.Scheme to avoid leaking type registrations across test suites.
var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	Scheme.AddKnownTypes(inventoryv1alpha1.GroupVersion,
		&inventoryv1alpha1.Inventory{}, &inventoryv1alpha1.InventoryList{})
	Scheme.AddKnownTypes(provisioningv1alpha1.GroupVersion,
		&provisioningv1alpha1.ClusterTemplate{}, &provisioningv1alpha1.ClusterTemplateList{},
		&provisioningv1alpha1.ProvisioningRequest{}, &provisioningv1alpha1.ProvisioningRequestList{})
	Scheme.AddKnownTypes(siteconfig.GroupVersion,
		&siteconfig.ClusterInstance{}, &siteconfig.ClusterInstanceList{})
	Scheme.AddKnownTypes(hwmgmtv1alpha1.GroupVersion,
		&hwmgmtv1alpha1.HardwareProfile{}, &hwmgmtv1alpha1.HardwareProfileList{},
		&hwmgmtv1alpha1.NodeAllocationRequest{},
		&hwmgmtv1alpha1.AllocatedNode{}, &hwmgmtv1alpha1.AllocatedNodeList{})
	Scheme.AddKnownTypes(policiesv1.SchemeGroupVersion,
		&policiesv1.Policy{}, &policiesv1.PolicyList{})
	Scheme.AddKnownTypes(clusterv1.SchemeGroupVersion,
		&clusterv1.ManagedCluster{}, &clusterv1.ManagedClusterList{})
	Scheme.AddKnownTypes(openshiftv1.SchemeGroupVersion,
		&openshiftv1.ClusterVersion{},
		&openshiftv1.Proxy{}, &openshiftv1.ProxyList{})
	Scheme.AddKnownTypes(mcfgv1.SchemeGroupVersion,
		&mcfgv1.MachineConfigPool{}, &mcfgv1.MachineConfigPoolList{})
	Scheme.AddKnownTypes(openshiftoperatorv1.SchemeGroupVersion, &openshiftoperatorv1.IngressController{})
	Scheme.AddKnownTypes(apiextensionsv1.SchemeGroupVersion, &apiextensionsv1.CustomResourceDefinition{})
	Scheme.AddKnownTypes(assistedservicev1beta1.GroupVersion,
		&assistedservicev1beta1.Agent{}, &assistedservicev1beta1.AgentList{})
	Scheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion, &ibguv1alpha1.ImageBasedGroupUpgrade{})
	Scheme.AddKnownTypes(msav1beta1.SchemeGroupVersion,
		&msav1beta1.ManagedServiceAccount{}, &msav1beta1.ManagedServiceAccountList{})
	Scheme.AddKnownTypes(workv1.SchemeGroupVersion,
		&workv1.ManifestWork{}, &workv1.ManifestWorkList{})
	Scheme.AddKnownTypes(addonv1alpha1.SchemeGroupVersion,
		&addonv1alpha1.ManagedClusterAddOn{}, &addonv1alpha1.ManagedClusterAddOnList{})
	utilruntime.Must(metal3v1alpha1.AddToScheme(Scheme))
	utilruntime.Must(hivev1.AddToScheme(Scheme))
}

// GetFakeClientFromObjects creates a fake Kubernetes client with comprehensive
// test infrastructure: status subresources for all custom resource types, the
// Inventory CRD for owner reference testing, a field indexer for AllocatedNode
// lookups, and an SSA-compatible wrapper.
func GetFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	inventoryCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: inventoryCRDName,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(Scheme).
		WithObjects(objs...).
		WithObjects(inventoryCRD).
		WithStatusSubresource(
			&inventoryv1alpha1.Inventory{},
			&provisioningv1alpha1.ClusterTemplate{},
			&provisioningv1alpha1.ProvisioningRequest{},
			&siteconfig.ClusterInstance{},
			&clusterv1.ManagedCluster{},
			&hwmgmtv1alpha1.NodeAllocationRequest{},
			&hwmgmtv1alpha1.AllocatedNode{},
			&openshiftv1.ClusterVersion{},
			&openshiftoperatorv1.IngressController{},
			&policiesv1.Policy{},
		).
		WithIndex(&hwmgmtv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
			return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
		}).
		Build()

	return &SSACompatibleClient{WithWatch: fakeClient}
}

// SSACompatibleClient wraps a fake client and converts Server-Side Apply operations
// to traditional create/update operations, providing compatibility for testing.
type SSACompatibleClient struct {
	client.WithWatch
}

// Patch intercepts Server-Side Apply operations and converts them to create/update
func (c *SSACompatibleClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if patch.Type() == types.ApplyPatchType {
		return c.handleServerSideApply(ctx, obj, opts...)
	}

	if err := c.WithWatch.Patch(ctx, obj, patch, opts...); err != nil {
		return fmt.Errorf("failed to apply patch: %w", err)
	}
	return nil
}

func (c *SSACompatibleClient) handleServerSideApply(ctx context.Context, obj client.Object, opts ...client.PatchOption) error {
	patchOpts := &client.PatchOptions{}
	patchOpts.ApplyOptions(opts)
	for _, v := range patchOpts.DryRun {
		if v == metav1.DryRunAll {
			return nil
		}
	}

	existing := obj.DeepCopyObject().(client.Object)
	err := c.WithWatch.Get(ctx, client.ObjectKeyFromObject(obj), existing)

	switch {
	case errors.IsNotFound(err):
		if createErr := c.WithWatch.Create(ctx, obj); createErr != nil {
			return fmt.Errorf("failed to create object: %w", createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to check existing object: %w", err)
	default:
		obj.SetResourceVersion(existing.GetResourceVersion())
		if updateErr := c.WithWatch.Update(ctx, obj); updateErr != nil {
			return fmt.Errorf("failed to update object: %w", updateErr)
		}
		return nil
	}
}
