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
	"k8s.io/client-go/kubernetes/scheme"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

func TestProvisioningApiSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning API Suite")
}

var s = scheme.Scheme

var _ = BeforeSuite(func() {
	s.AddKnownTypes(GroupVersion, &ProvisioningRequest{}, &ClusterTemplate{}, &ClusterTemplateList{})
	s.AddKnownTypes(hwmgmtv1alpha1.GroupVersion,
		&hwmgmtv1alpha1.HardwareTemplate{}, &hwmgmtv1alpha1.HardwareTemplateList{},
		&hwmgmtv1alpha1.HardwareProfile{}, &hwmgmtv1alpha1.HardwareProfileList{},
	)
	os.Setenv("OCLOUD_MANAGER_NAMESPACE", "oran-o2ims")
})
