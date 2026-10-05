/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestSeedGenerationStatusDeepCopy(t *testing.T) {
	started := metav1.Now()
	original := &ProvisioningRequest{
		Status: ProvisioningRequestStatus{
			Extensions: Extensions{
				ClusterDetails: &ClusterDetails{
					SeedGenerationStatus: &SeedGenerationStatus{
						StartedAt:                 &started,
						DetachmentStarted:         true,
						SeedImage:                 "quay.io/example/seed@sha256:original",
						ISOServerCACertRef:        &ConfigMapKeyRef{Name: "iso-ca", Namespace: "operator", Key: "ca-bundle.crt"},
						InputSnapshotResourceUIDs: map[string]string{"pull-secret": "original-uid"},
					},
				},
			},
		},
	}

	copied := original.DeepCopy()
	status := copied.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.StartedAt.Time = started.Time.Add(time.Hour)
	status.ISOServerCACertRef.Name = "changed-ca"
	status.InputSnapshotResourceUIDs["pull-secret"] = "changed-uid"
	status.SeedImage = "quay.io/example/seed@sha256:changed"

	untouched := original.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if !untouched.StartedAt.Equal(&started) || untouched.ISOServerCACertRef.Name != "iso-ca" ||
		untouched.InputSnapshotResourceUIDs["pull-secret"] != "original-uid" ||
		untouched.SeedImage != "quay.io/example/seed@sha256:original" {
		t.Fatal("deep copy changed the original seed generation status")
	}

	if standalone := untouched.DeepCopy(); standalone == untouched || standalone.InputSnapshotResourceUIDs["pull-secret"] != "original-uid" {
		t.Fatal("standalone seed status copy must preserve values without sharing the object")
	}
	if copiedRef := untouched.ISOServerCACertRef.DeepCopy(); copiedRef == untouched.ISOServerCACertRef || copiedRef.Name != "iso-ca" {
		t.Fatal("ConfigMap reference copy must preserve values without sharing the object")
	}
}
