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
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	openshiftv1 "github.com/openshift/api/config/v1"
	openshiftoperatorv1 "github.com/openshift/api/operator/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

const inventoryCRDName = "inventories.ocloud.openshift.io"

// GetFakeClientFromObjects creates a fake Kubernetes client with comprehensive
// test infrastructure: status subresources for all custom resource types known
// to the scheme, the Inventory CRD for owner reference testing, a field indexer
// for AllocatedNode lookups (if the type is in the scheme), and an
// SSA-compatible wrapper.
func GetFakeClientFromObjects(scheme *runtime.Scheme, objs ...client.Object) client.WithWatch {
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...)

	inventoryCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: inventoryCRDName,
		},
	}
	if isTypeRegistered(scheme, inventoryCRD) {
		builder.WithObjects(inventoryCRD)
	}

	statusSubresources := []client.Object{
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
	}
	for _, obj := range statusSubresources {
		if isTypeRegistered(scheme, obj) {
			builder.WithStatusSubresource(obj)
		}
	}

	if isTypeRegistered(scheme, &hwmgmtv1alpha1.AllocatedNode{}) {
		builder.WithIndex(&hwmgmtv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
			return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
		})
	}

	return &SSACompatibleClient{WithWatch: builder.Build()}
}

func isTypeRegistered(scheme *runtime.Scheme, obj runtime.Object) bool {
	_, _, err := scheme.ObjectKinds(obj)
	return err == nil
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
