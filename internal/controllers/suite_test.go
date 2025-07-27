/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers")
}

const testHwMgrPluginNameSpace = "hwmgr"
const testMetal3HardwarePluginRef = "hwmgr"

func getFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	// Start mock hardware plugin server for tests
	mockServer := NewMockHardwarePluginServer()

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

	// Create BMC secrets that tests expect for allocated nodes
	bmcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node-1-bmc-secret",
			Namespace: testHwMgrPluginNameSpace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("password123"),
		},
	}

	bmcSecret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "master-node-2-bmc-secret",
			Namespace: testHwMgrPluginNameSpace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("password123"),
		},
	}

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

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithObjects([]client.Object{metal3HwPlugin, authSecret, bmcSecret, bmcSecret2}...).
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
			return []string{}
		}).
		Build()
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

	os.Setenv(utils.HwMgrPluginNameSpace, testHwMgrPluginNameSpace)

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
})
