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

	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers")
}

const testHwMgrPluginNameSpace = "hwmgr"
const testHwMgrId = "hwmgr"

func getFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	// Add fake hardwaremanager CR
	hwmgr := &pluginv1alpha1.HardwareManager{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testHwMgrPluginNameSpace,
			Name:      testHwMgrId,
		},
		Spec: pluginv1alpha1.HardwareManagerSpec{
			AdaptorID: "loopback",
		},
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithObjects([]client.Object{hwmgr}...).
		WithStatusSubresource(&inventoryv1alpha1.Inventory{}).
		WithStatusSubresource(&provisioningv1alpha1.ClusterTemplate{}).
		WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).
		WithStatusSubresource(&siteconfig.ClusterInstance{}).
		WithStatusSubresource(&clusterv1.ManagedCluster{}).
		WithStatusSubresource(&hwv1alpha1.HardwareTemplate{}).
		WithStatusSubresource(&hwv1alpha1.NodeAllocationRequest{}).
		WithStatusSubresource(&hwv1alpha1.Node{}).
		WithStatusSubresource(&openshiftv1.ClusterVersion{}).
		WithStatusSubresource(&openshiftoperatorv1.IngressController{}).
		WithStatusSubresource(&policiesv1.Policy{}).
		WithStatusSubresource(&clusterv1.ManagedCluster{}).
		WithStatusSubresource(&pluginv1alpha1.HardwareManager{}).
		WithIndex(&hwv1alpha1.Node{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
			return []string{obj.(*hwv1alpha1.Node).Spec.NodeAllocationRequest}
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
	scheme.AddKnownTypes(hwv1alpha1.GroupVersion, &hwv1alpha1.HardwareTemplate{})
	scheme.AddKnownTypes(hwv1alpha1.GroupVersion, &hwv1alpha1.NodeAllocationRequest{})
	scheme.AddKnownTypes(hwv1alpha1.GroupVersion, &hwv1alpha1.Node{})
	scheme.AddKnownTypes(hwv1alpha1.GroupVersion, &hwv1alpha1.NodeList{})
	scheme.AddKnownTypes(policiesv1.SchemeGroupVersion, &policiesv1.Policy{})
	scheme.AddKnownTypes(policiesv1.SchemeGroupVersion, &policiesv1.PolicyList{})
	scheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedCluster{})
	scheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedClusterList{})
	scheme.AddKnownTypes(openshiftv1.SchemeGroupVersion, &openshiftv1.ClusterVersion{})
	scheme.AddKnownTypes(openshiftoperatorv1.SchemeGroupVersion, &openshiftoperatorv1.IngressController{})
	scheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion, &ibguv1alpha1.ImageBasedGroupUpgrade{})
	scheme.AddKnownTypes(pluginv1alpha1.GroupVersion, &pluginv1alpha1.HardwareManager{})
	scheme.AddKnownTypes(assistedservicev1beta1.GroupVersion, &assistedservicev1beta1.Agent{})
	scheme.AddKnownTypes(assistedservicev1beta1.GroupVersion, &assistedservicev1beta1.AgentList{})
	scheme.AddKnownTypes(apiextensionsv1.SchemeGroupVersion, &apiextensionsv1.CustomResourceDefinition{})
})
