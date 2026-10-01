/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("FirmwareCatalog URL validation", Label("envtest"), func() {
	newCatalog := func(name, url string) *hwmgmtv1alpha1.FirmwareCatalog {
		return &hwmgmtv1alpha1.FirmwareCatalog{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{
				Images: []hwmgmtv1alpha1.FirmwareImage{
					{Name: "bios-1", Component: hwmgmtv1alpha1.ComponentBIOS, URL: url, Version: "1.0"},
				},
			},
		}
	}

	It("accepts an image URL with a valid HTTPS host", func() {
		catalog := newCatalog("fw-cel-valid", "https://example.com/firmware/bios.bin")
		Expect(k8sClient.Create(ctx, catalog)).To(Succeed())
		defer func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, catalog))).To(Succeed())
		}()
	})

	It("rejects an image URL that has a scheme but no hostname", func() {
		catalog := newCatalog("fw-cel-nohost", "https://")
		err := k8sClient.Create(ctx, catalog)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("url must be a valid HTTP or HTTPS URL with a hostname"))
	})
})
