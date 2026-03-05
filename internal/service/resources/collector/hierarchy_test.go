/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

var _ = Describe("Hierarchy Helpers", func() {
	Describe("isResourceReady", func() {
		It("returns false when conditions is nil", func() {
			result := isResourceReady(nil)
			Expect(result).To(BeFalse())
		})

		It("returns false when conditions is empty", func() {
			result := isResourceReady([]metav1.Condition{})
			Expect(result).To(BeFalse())
		})

		It("returns true when Ready=True", func() {
			conditions := []metav1.Condition{
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionTrue,
					Reason: inventoryv1alpha1.ReasonReady,
				},
			}
			result := isResourceReady(conditions)
			Expect(result).To(BeTrue())
		})

		It("returns false when Ready=False", func() {
			conditions := []metav1.Condition{
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionFalse,
					Reason: inventoryv1alpha1.ReasonParentNotFound,
				},
			}
			result := isResourceReady(conditions)
			Expect(result).To(BeFalse())
		})

		It("returns false when Ready=Unknown", func() {
			conditions := []metav1.Condition{
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionUnknown,
				},
			}
			result := isResourceReady(conditions)
			Expect(result).To(BeFalse())
		})

		It("returns false when only other conditions exist", func() {
			conditions := []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionTrue,
				},
			}
			result := isResourceReady(conditions)
			Expect(result).To(BeFalse())
		})

		It("returns true when Ready=True among multiple conditions", func() {
			conditions := []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionFalse,
				},
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionTrue,
					Reason: inventoryv1alpha1.ReasonReady,
				},
			}
			result := isResourceReady(conditions)
			Expect(result).To(BeTrue())
		})
	})

	Describe("getReadyReason", func() {
		It("returns empty string when conditions is nil", func() {
			result := getReadyReason(nil)
			Expect(result).To(BeEmpty())
		})

		It("returns the reason from Ready condition", func() {
			conditions := []metav1.Condition{
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionFalse,
					Reason: inventoryv1alpha1.ReasonParentNotFound,
				},
			}
			result := getReadyReason(conditions)
			Expect(result).To(Equal(inventoryv1alpha1.ReasonParentNotFound))
		})

		It("returns Ready reason when status is True", func() {
			conditions := []metav1.Condition{
				{
					Type:   inventoryv1alpha1.ConditionTypeReady,
					Status: metav1.ConditionTrue,
					Reason: inventoryv1alpha1.ReasonReady,
				},
			}
			result := getReadyReason(conditions)
			Expect(result).To(Equal(inventoryv1alpha1.ReasonReady))
		})

		It("returns empty string when no Ready condition exists", func() {
			conditions := []metav1.Condition{
				{
					Type:   "OtherCondition",
					Status: metav1.ConditionTrue,
					Reason: "SomeReason",
				},
			}
			result := getReadyReason(conditions)
			Expect(result).To(BeEmpty())
		})
	})
})
