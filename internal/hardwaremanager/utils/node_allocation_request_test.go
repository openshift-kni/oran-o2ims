/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
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
		current := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), current)).To(Succeed())

		err := UpdateNodeAllocationRequestObservedStatus(ctx, c, nar,
			current.ObjectMeta.Generation, current.Spec.ConfigTransactionId)
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		Expect(updated.Status.ObservedGeneration).To(Equal(updated.ObjectMeta.Generation))
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(4)))
	})

	It("does not acknowledge when the ConfigTransactionId advanced after the check", func() {
		// Simulate a Day-2 arriving between the caller's validation and the ack: the caller
		// validated transaction id 2, but the live spec is now 4. The helper must refuse to
		// acknowledge the unhandled transaction.
		err := UpdateNodeAllocationRequestObservedStatus(ctx, c, nar,
			nar.ObjectMeta.Generation, 2)
		Expect(err).To(MatchError(ErrNodeAllocationRequestSpecChanged))

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		// Observed status is left untouched (the BeforeEach seed values).
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(2)))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(3)))
	})

	It("does not acknowledge when the generation advanced after the check", func() {
		err := UpdateNodeAllocationRequestObservedStatus(ctx, c, nar,
			nar.ObjectMeta.Generation+1, nar.Spec.ConfigTransactionId)
		Expect(err).To(MatchError(ErrNodeAllocationRequestSpecChanged))

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(2)))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(3)))
	})
})

var _ = Describe("UpdateNodeAllocationRequestStatusCondition", func() {
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

	DescribeTable("conditionally acks ObservedGeneration",
		func(conditionType hwmgmtv1alpha1.ConditionType,
			reason hwmgmtv1alpha1.ConditionReason,
			status metav1.ConditionStatus,
			expectAck bool) {

			err := UpdateNodeAllocationRequestStatusCondition(ctx, c, nar, conditionType, reason, status, "test")
			Expect(err).ToNot(HaveOccurred())

			updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
			Expect(c.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
			if expectAck {
				Expect(updated.Status.ObservedGeneration).To(Equal(int64(5)))
			} else {
				Expect(updated.Status.ObservedGeneration).To(Equal(int64(3)))
			}
		},
		Entry("Configured InProgress does not ack",
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.InProgress, metav1.ConditionFalse, false),
		Entry("Configured Failed acks",
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.Failed, metav1.ConditionFalse, true),
		Entry("Configured TimedOut acks",
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.TimedOut, metav1.ConditionFalse, true),
		Entry("Configured ConfigApplied acks",
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, true),
		Entry("Provisioned InProgress does not ack",
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.InProgress, metav1.ConditionFalse, false),
		Entry("Provisioned Completed does not ack",
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, false),
		Entry("Provisioned Failed acks",
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse, true),
		Entry("Provisioned TimedOut acks",
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.TimedOut, metav1.ConditionFalse, true),
	)
})

var _ = Describe("UpdateNodeAllocationRequestStatusCondition spec-change guard", func() {
	// Regression coverage for OCPBUGS-132825: when the live NAR spec advances between the
	// time a reconcile validated it and the time this function writes status (a Day-2 config
	// arriving, or a Status().Update conflict forcing a re-fetch onto a newer spec), the
	// observed status (ObservedConfigTransactionId / ObservedGeneration) must not be
	// acknowledged — doing so would mark a transaction/generation this reconcile never
	// processed as observed and let the ProvisioningRequest controller's in-flight gate
	// proceed prematurely. The condition itself is still written.
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	buildClient := func(live *hwmgmtv1alpha1.NodeAllocationRequest) client.Client {
		return fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(live).
			WithStatusSubresource(live).
			Build()
	}

	It("does not acknowledge observed status when the live ConfigTransactionId advanced", func() {
		live := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: "default", Generation: 5},
			Spec:       hwmgmtv1alpha1.NodeAllocationRequestSpec{ConfigTransactionId: 5},
			Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
				ObservedGeneration:          3,
				ObservedConfigTransactionId: 2,
			},
		}
		c := buildClient(live)

		// The reconcile validated the older transaction id 4; the live spec is now 5.
		reconcileStart := live.DeepCopy()
		reconcileStart.Spec.ConfigTransactionId = 4

		err := UpdateNodeAllocationRequestStatusCondition(ctx, c, reconcileStart,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, "test")
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(live), updated)).To(Succeed())
		// Acks skipped — observed status is left at the seeded values.
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(2)))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(3)))
		// The condition itself is still written.
		cond := meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
	})

	It("does not acknowledge observed status when the live generation advanced", func() {
		live := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: "default", Generation: 6},
			Spec:       hwmgmtv1alpha1.NodeAllocationRequestSpec{ConfigTransactionId: 4},
			Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
				ObservedGeneration:          3,
				ObservedConfigTransactionId: 2,
			},
		}
		c := buildClient(live)

		// The reconcile validated generation 5; the live generation is now 6.
		reconcileStart := live.DeepCopy()
		reconcileStart.ObjectMeta.Generation = 5

		err := UpdateNodeAllocationRequestStatusCondition(ctx, c, reconcileStart,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, "test")
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(live), updated)).To(Succeed())
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(2)))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(3)))
	})

	It("acknowledges observed status when the live spec still matches the validated spec", func() {
		live := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nar", Namespace: "default", Generation: 5},
			Spec:       hwmgmtv1alpha1.NodeAllocationRequestSpec{ConfigTransactionId: 4},
			Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
				ObservedGeneration:          3,
				ObservedConfigTransactionId: 2,
			},
		}
		c := buildClient(live)

		err := UpdateNodeAllocationRequestStatusCondition(ctx, c, live.DeepCopy(),
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, "test")
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(live), updated)).To(Succeed())
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(4)))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(5)))
	})
})
