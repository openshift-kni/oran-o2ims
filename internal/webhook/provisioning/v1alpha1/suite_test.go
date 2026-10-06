/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestProvisioningWebhookSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning Webhook Suite")
}

var s = scheme.Scheme

var _ = BeforeSuite(func() {
	s.AddKnownTypes(provisioningv1alpha1.GroupVersion, &provisioningv1alpha1.ProvisioningRequest{}, &provisioningv1alpha1.ClusterTemplate{}, &provisioningv1alpha1.ClusterTemplateList{})
	s.AddKnownTypes(hwmgmtv1alpha1.GroupVersion,
		&hwmgmtv1alpha1.HardwareProfile{}, &hwmgmtv1alpha1.HardwareProfileList{},
	)
	os.Setenv("OCLOUD_MANAGER_NAMESPACE", "oran-o2ims")
})
