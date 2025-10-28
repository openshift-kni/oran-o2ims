/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Overview for Controllers Package

This test suite (suite_test.go) provides the foundational setup and utilities for comprehensive testing
of the O2IMS controllers package. The suite supports the following test categories:

1. ClusterTemplate Controller Tests (clustertemplate_controller_test.go):
   - ClusterTemplate reconciliation and validation
   - ConfigMap reference validation and schema compliance
   - Hardware template timeout validation
   - Policy template parameter schema validation
   - Cluster instance name and template ID validation
   - Upgrade defaults ConfigMap validation for Image-Based Group Upgrades
   - Status condition management for validation results

2. ProvisioningRequest Controller Tests (provisioningrequest_controller_test.go):
   - Complete provisioning request lifecycle management
   - Multi-phase reconciliation (validation, resource creation, hardware provisioning,
     cluster installation, configuration)
   - Hardware plugin integration and node allocation
   - Cluster template resolution and application
   - BMC secret creation and management
   - Status condition tracking through provisioning phases

3. ProvisioningRequest Hardware Provisioning Tests (provisioningrequest_hwprovision_test.go):
   - Hardware template rendering and validation
   - Node allocation request creation and management
   - AllocatedNode resource handling
   - Hardware plugin API integration
   - BMC credential management
   - Node group processing (controller/worker nodes)

4. ProvisioningRequest Cluster Installation Tests (provisioningrequest_clusterinstall_test.go):
   - ClusterInstance rendering and dry-run validation
   - Node detail extraction and assignment
   - BMC and network interface configuration
   - ManagedCluster annotation handling
   - ClusterInstance validation with hardware details

5. ProvisioningRequest Resource Creation Tests (provisioningrequest_resourcecreation_test.go):
   - Policy template ConfigMap creation
   - BMC secret generation from credentials
   - Cluster resource creation and management
   - Label validation for policy applications

6. ProvisioningRequest Validation Tests (provisioningrequest_validation_test.go):
   - Configuration override and merging logic
   - Labels and annotations management
   - Nested resource validation
   - Input validation for provisioning requests

7. ProvisioningRequest Configuration Tests (provisioningrequest_clusterconfig_test.go):
   - Cluster configuration phase management
   - Policy application and validation
   - Configuration status tracking

8. Inventory Controller Tests (inventory_controller_test.go):
   - Inventory resource reconciliation
   - Server deployment management
   - Pod readiness and status monitoring
   - Inventory service lifecycle

9. Mock Hardware Plugin Server (mock_hardware_plugin_server.go):
   - Simulated hardware plugin API endpoints
   - NodeAllocationRequest lifecycle testing
   - AllocatedNode management simulation
   - Authentication testing scenarios
   - Status and condition management simulation

Test Infrastructure:
- Ginkgo BDD-style test framework with Gomega assertions
- Fake Kubernetes client with comprehensive scheme registration
- Mock hardware plugin server for integration testing
- BMC secret and authentication setup for realistic scenarios
- Status subresource support for all custom resources
- Indexing support for efficient resource lookups

The test suite ensures comprehensive coverage of the O2IMS provisioning workflow from
cluster template validation through complete cluster deployment and configuration.
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	openshiftv1 "github.com/openshift/api/config/v1"
	openshiftoperatorv1 "github.com/openshift/api/operator/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/api/common"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers")
}

const testHwMgrPluginNameSpace = constants.DefaultNamespace
const testMetal3HardwarePluginRef = "hwmgr"

// SSACompatibleClient wraps a fake client and converts Server-Side Apply operations
// to traditional create/update operations, providing compatibility for testing.
type SSACompatibleClient struct {
	client.WithWatch
}

// Patch intercepts Server-Side Apply operations and converts them to create/update
func (c *SSACompatibleClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	// Check if this is a Server-Side Apply operation
	if patch.Type() == types.ApplyPatchType {
		return c.handleServerSideApply(ctx, obj, opts...)
	}

	// For non-SSA patches, delegate to the underlying client
	if err := c.WithWatch.Patch(ctx, obj, patch, opts...); err != nil {
		return fmt.Errorf("failed to apply patch: %w", err)
	}
	return nil
}

// handleServerSideApply converts SSA operations to create/update operations
func (c *SSACompatibleClient) handleServerSideApply(ctx context.Context, obj client.Object, opts ...client.PatchOption) error {
	// Check for dry-run option by inspecting patch options
	isDryRun := false
	for _, opt := range opts {
		// Check if this is a dry-run option by examining the string representation
		optStr := fmt.Sprintf("%T", opt)
		if optStr == "client.dryRunAll" {
			isDryRun = true
			break
		}
	}

	// For dry-run, just validate without actually creating/updating
	if isDryRun {
		return nil
	}

	// Check if the object already exists
	existing := obj.DeepCopyObject().(client.Object)
	err := c.WithWatch.Get(ctx, client.ObjectKeyFromObject(obj), existing)

	switch {
	case errors.IsNotFound(err):
		// Create the object
		if createErr := c.WithWatch.Create(ctx, obj); createErr != nil {
			return fmt.Errorf("failed to create object: %w", createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to check existing object: %w", err)
	default:
		// Update the existing object
		obj.SetResourceVersion(existing.GetResourceVersion())
		if updateErr := c.WithWatch.Update(ctx, obj); updateErr != nil {
			return fmt.Errorf("failed to update object: %w", updateErr)
		}
		return nil
	}
}

func getFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	// Create a basic auth secret for test authentication
	basicAuthSecret := "test-auth-secret"
	authSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      basicAuthSecret,
			Namespace: testHwMgrPluginNameSpace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("test-user"),
			"password": []byte("test-password"),
		},
	}

	// Note: BMC secrets are created by individual tests in their BeforeEach blocks
	// to avoid resource conflicts between tests

	// First create the fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithObjects([]client.Object{authSecret}...).
		WithStatusSubresource(&inventoryv1alpha1.Inventory{}).
		WithStatusSubresource(&provisioningv1alpha1.ClusterTemplate{}).
		WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).
		WithStatusSubresource(&siteconfig.ClusterInstance{}).
		WithStatusSubresource(&clusterv1.ManagedCluster{}).
		WithStatusSubresource(&hwmgmtv1alpha1.HardwareTemplate{}).
		WithStatusSubresource(&pluginsv1alpha1.NodeAllocationRequest{}).
		WithStatusSubresource(&pluginsv1alpha1.AllocatedNode{}).
		WithStatusSubresource(&openshiftv1.ClusterVersion{}).
		WithStatusSubresource(&openshiftoperatorv1.IngressController{}).
		WithStatusSubresource(&policiesv1.Policy{}).
		WithStatusSubresource(&clusterv1.ManagedCluster{}).
		WithIndex(&pluginsv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
			return []string{obj.(*pluginsv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
		}).
		WithIndex(&bmhv1alpha1.BareMetalHost{}, "status.hardware.hostname", func(obj client.Object) []string {
			bmh := obj.(*bmhv1alpha1.BareMetalHost)
			if bmh.Status.HardwareDetails != nil && bmh.Status.HardwareDetails.Hostname != "" {
				return []string{bmh.Status.HardwareDetails.Hostname}
			}
			return nil
		}).
		Build()

	// Start mock hardware plugin server for tests with the client
	mockServer := NewMockHardwarePluginServerWithClient(fakeClient)

	// Add fake Metal3 hardware plugin CR pointing to mock server with Basic auth
	metal3HwPlugin := &hwmgmtv1alpha1.HardwarePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testHwMgrPluginNameSpace,
			Name:      testMetal3HardwarePluginRef,
		},
		Spec: hwmgmtv1alpha1.HardwarePluginSpec{
			ApiRoot: mockServer.GetURL(), // Point to mock server instead of localhost:8443
			AuthClientConfig: &common.AuthClientConfig{
				Type:            common.Basic,
				BasicAuthSecret: &basicAuthSecret,
			},
		},
		Status: hwmgmtv1alpha1.HardwarePluginStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(hwmgmtv1alpha1.ConditionTypes.Registration),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             string(hwmgmtv1alpha1.ConditionReasons.Completed),
					Message:            "HardwarePlugin registered successfully",
				},
			},
		},
	}

	// Add the hardware plugin to the client
	err := fakeClient.Create(context.Background(), metal3HwPlugin)
	if err != nil {
		panic(fmt.Sprintf("Failed to create hardware plugin: %v", err))
	}

	// Wrap the fake client with SSA compatibility for testing
	return &SSACompatibleClient{WithWatch: fakeClient}
}

// Logger used for tests:
var logger *slog.Logger

// Scheme used for the tests:
var scheme = clientgoscheme.Scheme

var _ = BeforeSuite(func() {
	// Create a logger that writes to the Ginkgo writer, so that the log messages will be
	// attached to the output of the right test:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// Configure the controller runtime library to use our logger:
	adapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	os.Setenv(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)

	// Add all the required types to the scheme used by the tests:
	scheme.AddKnownTypes(inventoryv1alpha1.GroupVersion, &inventoryv1alpha1.Inventory{})
	scheme.AddKnownTypes(inventoryv1alpha1.GroupVersion, &inventoryv1alpha1.InventoryList{})
	scheme.AddKnownTypes(provisioningv1alpha1.GroupVersion, &provisioningv1alpha1.ClusterTemplate{})
	scheme.AddKnownTypes(provisioningv1alpha1.GroupVersion, &provisioningv1alpha1.ClusterTemplateList{})
	scheme.AddKnownTypes(provisioningv1alpha1.GroupVersion, &provisioningv1alpha1.ProvisioningRequest{})
	scheme.AddKnownTypes(provisioningv1alpha1.GroupVersion, &provisioningv1alpha1.ProvisioningRequestList{})
	scheme.AddKnownTypes(networkingv1.SchemeGroupVersion, &networkingv1.Ingress{})
	scheme.AddKnownTypes(networkingv1.SchemeGroupVersion, &networkingv1.IngressList{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceAccount{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceAccountList{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Service{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceList{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.Deployment{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DeploymentList{})
	scheme.AddKnownTypes(siteconfig.GroupVersion, &siteconfig.ClusterInstance{})
	scheme.AddKnownTypes(siteconfig.GroupVersion, &siteconfig.ClusterInstanceList{})
	scheme.AddKnownTypes(hwmgmtv1alpha1.GroupVersion, &hwmgmtv1alpha1.HardwareTemplate{})
	scheme.AddKnownTypes(hwmgmtv1alpha1.GroupVersion, &hwmgmtv1alpha1.HardwarePlugin{})
	scheme.AddKnownTypes(hwmgmtv1alpha1.GroupVersion, &hwmgmtv1alpha1.HardwarePluginList{})
	scheme.AddKnownTypes(pluginsv1alpha1.GroupVersion, &pluginsv1alpha1.NodeAllocationRequest{})
	scheme.AddKnownTypes(pluginsv1alpha1.GroupVersion, &pluginsv1alpha1.AllocatedNode{})
	scheme.AddKnownTypes(pluginsv1alpha1.GroupVersion, &pluginsv1alpha1.AllocatedNodeList{})
	scheme.AddKnownTypes(policiesv1.SchemeGroupVersion, &policiesv1.Policy{})
	scheme.AddKnownTypes(policiesv1.SchemeGroupVersion, &policiesv1.PolicyList{})
	scheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedCluster{})
	scheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedClusterList{})
	scheme.AddKnownTypes(openshiftv1.SchemeGroupVersion, &openshiftv1.ClusterVersion{})
	scheme.AddKnownTypes(openshiftoperatorv1.SchemeGroupVersion, &openshiftoperatorv1.IngressController{})
	scheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion, &ibguv1alpha1.ImageBasedGroupUpgrade{})
	scheme.AddKnownTypes(assistedservicev1beta1.GroupVersion, &assistedservicev1beta1.Agent{})
	scheme.AddKnownTypes(assistedservicev1beta1.GroupVersion, &assistedservicev1beta1.AgentList{})
	scheme.AddKnownTypes(apiextensionsv1.SchemeGroupVersion, &apiextensionsv1.CustomResourceDefinition{})
	utilruntime.Must(bmhv1alpha1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
})
