/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

var _ = Describe("CreateDefaultFirmwareCatalogCR", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})

	It("creates the singleton FirmwareCatalog in the operator namespace", func() {
		Expect(CreateDefaultFirmwareCatalogCR(ctx, fakeClient)).To(Succeed())

		catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
		key := client.ObjectKey{
			Name:      hwmgmtv1alpha1.FirmwareCatalogName,
			Namespace: constants.DefaultNamespace,
		}
		Expect(fakeClient.Get(ctx, key, catalog)).To(Succeed())
		Expect(catalog.Spec.Images).To(BeEmpty())
	})

	It("is idempotent when the singleton already exists", func() {
		Expect(CreateDefaultFirmwareCatalogCR(ctx, fakeClient)).To(Succeed())
		// A second call must swallow the AlreadyExists error.
		Expect(CreateDefaultFirmwareCatalogCR(ctx, fakeClient)).To(Succeed())
	})
})
