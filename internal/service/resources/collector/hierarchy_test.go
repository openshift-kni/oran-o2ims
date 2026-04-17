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
	Describe("IsResourceReady", func() {
		It("returns false when conditions is nil", func() {
			Expect(inventoryv1alpha1.IsResourceReady(nil)).To(BeFalse())
		})

		It("returns false when conditions is empty", func() {
			Expect(inventoryv1alpha1.IsResourceReady([]metav1.Condition{})).To(BeFalse())
		})

		It("returns true when Ready=True", func() {
			conditions := []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionTrue,
				Reason: inventoryv1alpha1.ReasonReady,
			}}
			Expect(inventoryv1alpha1.IsResourceReady(conditions)).To(BeTrue())
		})

		It("returns false when Ready=False", func() {
			conditions := []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionFalse,
				Reason: inventoryv1alpha1.ReasonParentNotFound,
			}}
			Expect(inventoryv1alpha1.IsResourceReady(conditions)).To(BeFalse())
		})

		It("returns false when Ready=Unknown", func() {
			conditions := []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionUnknown,
			}}
			Expect(inventoryv1alpha1.IsResourceReady(conditions)).To(BeFalse())
		})

		It("returns false when only other conditions exist", func() {
			conditions := []metav1.Condition{{
				Type:   "SomeOtherCondition",
				Status: metav1.ConditionTrue,
			}}
			Expect(inventoryv1alpha1.IsResourceReady(conditions)).To(BeFalse())
		})

		It("returns true when Ready=True among multiple conditions", func() {
			conditions := []metav1.Condition{
				{Type: "SomeOtherCondition", Status: metav1.ConditionFalse},
				{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady},
			}
			Expect(inventoryv1alpha1.IsResourceReady(conditions)).To(BeTrue())
		})
	})

	Describe("GetReadyReason", func() {
		It("returns empty string when conditions is nil", func() {
			Expect(inventoryv1alpha1.GetReadyReason(nil)).To(BeEmpty())
		})

		It("returns the reason from Ready condition", func() {
			conditions := []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionFalse,
				Reason: inventoryv1alpha1.ReasonParentNotFound,
			}}
			Expect(inventoryv1alpha1.GetReadyReason(conditions)).To(Equal(inventoryv1alpha1.ReasonParentNotFound))
		})

		It("returns Ready reason when status is True", func() {
			conditions := []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionTrue,
				Reason: inventoryv1alpha1.ReasonReady,
			}}
			Expect(inventoryv1alpha1.GetReadyReason(conditions)).To(Equal(inventoryv1alpha1.ReasonReady))
		})

		It("returns empty string when no Ready condition exists", func() {
			conditions := []metav1.Condition{{
				Type:   "OtherCondition",
				Status: metav1.ConditionTrue,
				Reason: "SomeReason",
			}}
			Expect(inventoryv1alpha1.GetReadyReason(conditions)).To(BeEmpty())
		})
	})

	Describe("convertCoordinateToGeoJSON", func() {
		It("returns nil for nil coordinate", func() {
			m, err := convertCoordinateToGeoJSON(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil())
		})

		It("builds a Point without altitude", func() {
			m, err := convertCoordinateToGeoJSON(&inventoryv1alpha1.GeoLocation{
				Latitude:  "38.8951",
				Longitude: "-77.0364",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m["type"]).To(Equal("Point"))
			Expect(m["coordinates"]).To(Equal([]float64{-77.0364, 38.8951}))
		})

		It("includes altitude when set", func() {
			alt := "100.5"
			m, err := convertCoordinateToGeoJSON(&inventoryv1alpha1.GeoLocation{
				Latitude:  "0",
				Longitude: "0",
				Altitude:  &alt,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m["coordinates"]).To(Equal([]float64{0, 0, 100.5}))
		})

		It("returns error on invalid latitude", func() {
			_, err := convertCoordinateToGeoJSON(&inventoryv1alpha1.GeoLocation{
				Latitude:  "x",
				Longitude: "0",
			})
			Expect(err).To(HaveOccurred())
		})

		It("returns error on invalid longitude", func() {
			_, err := convertCoordinateToGeoJSON(&inventoryv1alpha1.GeoLocation{
				Latitude:  "0",
				Longitude: "y",
			})
			Expect(err).To(HaveOccurred())
		})

		It("returns error on invalid altitude", func() {
			bad := "z"
			_, err := convertCoordinateToGeoJSON(&inventoryv1alpha1.GeoLocation{
				Latitude:  "1",
				Longitude: "2",
				Altitude:  &bad,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("convertCivicAddress", func() {
		It("returns nil for empty input", func() {
			Expect(convertCivicAddress(nil)).To(BeNil())
			Expect(convertCivicAddress([]inventoryv1alpha1.CivicAddressElement{})).To(BeNil())
		})

		It("maps civic elements to generic maps", func() {
			out := convertCivicAddress([]inventoryv1alpha1.CivicAddressElement{
				{CaType: 1, CaValue: "CA"},
				{CaType: 6, CaValue: "Toronto"},
			})
			Expect(out).To(HaveLen(2))
			Expect(out[0]["caType"]).To(Equal(1))
			Expect(out[0]["caValue"]).To(Equal("CA"))
			Expect(out[1]["caValue"]).To(Equal("Toronto"))
		})
	})
})
