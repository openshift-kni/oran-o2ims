/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InfrastructureResourceStatus DeepCopy", func() {
	It("should produce an independent copy", func() {
		original := InfrastructureResourceStatus{
			ResourceName:              "worker-01.example.com",
			ResourceId:                "node-abc",
			ResourceProvisioningPhase: ResourceProvisioningPhaseProvisioned,
		}

		copied := original.DeepCopy()
		Expect(copied).ToNot(BeNil())
		Expect(*copied).To(Equal(original))

		copied.ResourceName = "changed"
		Expect(original.ResourceName).To(Equal("worker-01.example.com"))
	})

	It("should handle nil receiver", func() {
		var nilStatus *InfrastructureResourceStatus
		Expect(nilStatus.DeepCopy()).To(BeNil())
	})
})

var _ = Describe("Extensions DeepCopy", func() {
	It("should produce an independent copy of InfrastructureResourceStatuses", func() {
		original := Extensions{
			InfrastructureResourceStatuses: []InfrastructureResourceStatus{
				{
					ResourceName:              "worker-01.example.com",
					ResourceId:                "node-abc",
					ResourceProvisioningPhase: ResourceProvisioningPhaseProvisioned,
				},
				{
					ResourceName:              "worker-02.example.com",
					ResourceId:                "node-def",
					ResourceProvisioningPhase: ResourceProvisioningPhaseProcessing,
				},
			},
		}

		copied := original.DeepCopy()
		Expect(copied).ToNot(BeNil())
		Expect(copied.InfrastructureResourceStatuses).To(HaveLen(2))
		Expect(copied.InfrastructureResourceStatuses).To(Equal(original.InfrastructureResourceStatuses))

		copied.InfrastructureResourceStatuses[0].ResourceName = "changed"
		Expect(original.InfrastructureResourceStatuses[0].ResourceName).To(Equal("worker-01.example.com"))
	})
})
