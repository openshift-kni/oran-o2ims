/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestHardwareManagementWebhookSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HardwareManagement Webhook Suite")
}

var s = scheme.Scheme

var _ = BeforeSuite(func() {
	s.AddKnownTypes(hwmgmtv1alpha1.GroupVersion,
		&hwmgmtv1alpha1.FirmwareCatalog{}, &hwmgmtv1alpha1.FirmwareCatalogList{},
		&hwmgmtv1alpha1.HardwareProfile{}, &hwmgmtv1alpha1.HardwareProfileList{},
	)
})
