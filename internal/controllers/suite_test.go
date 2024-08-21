/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package controllers

import (
	"log/slog"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers")
}

func getFakeClientFromObjects(objs ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&oranv1alpha1.ORANO2IMS{}).
		WithStatusSubresource(&oranv1alpha1.ClusterTemplate{}).
		WithStatusSubresource(&oranv1alpha1.ClusterRequest{}).
		WithStatusSubresource(&siteconfig.ClusterInstance{}).
		WithStatusSubresource(&hwv1alpha1.NodePool{}).
		WithStatusSubresource(&hwv1alpha1.Node{}).
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

	// Add all the required types to the scheme used by the tests:
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ORANO2IMS{})
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ORANO2IMSList{})
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ClusterTemplate{})
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ClusterTemplateList{})
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ClusterRequest{})
	scheme.AddKnownTypes(oranv1alpha1.GroupVersion, &oranv1alpha1.ClusterRequestList{})
	scheme.AddKnownTypes(networkingv1.SchemeGroupVersion, &networkingv1.Ingress{})
	scheme.AddKnownTypes(networkingv1.SchemeGroupVersion, &networkingv1.IngressList{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceAccount{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceAccountList{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Service{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ServiceList{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.Deployment{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DeploymentList{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &siteconfig.ClusterInstance{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &siteconfig.ClusterInstanceList{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &hwv1alpha1.NodePool{})
	scheme.AddKnownTypes(appsv1.SchemeGroupVersion, &hwv1alpha1.Node{})
})
