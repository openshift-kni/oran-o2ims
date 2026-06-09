/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("UpdateNodeAllocationRequestObservedStatus", func() {
	var (
		ctx context.Context
		c   client.Client
		nar *hwmgmtv1alpha1.NodeAllocationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

		nar = &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-nar",
				Namespace:  "default",
				Generation: 5,
			},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				ConfigTransactionId: 4,
			},
			Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
				ObservedGeneration:          3,
				ObservedConfigTransactionId: 2,
			},
		}

		c = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(nar).
			WithStatusSubresource(nar).
			Build()
	})

	It("should update both ObservedGeneration and ObservedConfigTransactionId", func() {
		err := UpdateNodeAllocationRequestObservedStatus(ctx, c, nar)
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		Expect(updated.Status.ObservedGeneration).To(Equal(updated.ObjectMeta.Generation))
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(4)))
	})
})
