/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

var _ = Describe("InfrastructureResourceStatuses", Label("envtest"), func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	// buildPR creates a new ProvisioningRequest struct with a cluster template
	// with the given name
	buildPR := func(name string) *provisioningv1alpha1.ProvisioningRequest {
		return &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "cluster-template",
				TemplateVersion: "v1",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(`{}`),
				},
			},
		}
	}

	// updatePRStatus updates the status of the ProvisioningRequest CR
	updatePRStatus := func(pr *provisioningv1alpha1.ProvisioningRequest) {
		EventuallyWithOffset(1, func() error {
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched); err != nil {
				return fmt.Errorf("failed to get ProvisioningRequest: %w", err)
			}
			fetched.Status = pr.Status
			return k8sClient.Status().Update(ctx, fetched)
		}, timeout, interval).Should(Succeed())
	}

	Context("Persist statuses", func() {
		It("should store and retrieve infrastructure resource statuses", func() {
			pr := buildPR("infra-persist")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			pr.Status.Extensions.InfrastructureResourceStatuses = []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "node-1",
					ResourceId:                "hw-id-001",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProvisioned,
				},
				{
					ResourceName:              "node-2",
					ResourceId:                "hw-id-002",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				},
			}

			// Update the statuses on the CRD
			updatePRStatus(pr)

			// Now the statuses should be retrieved from the CRD
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses).To(HaveLen(2))

			// Now the statuses should be equal to the original statuses
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses[0]).To(Equal(
				provisioningv1alpha1.InfrastructureResourceStatus{
					ResourceName:              "node-1",
					ResourceId:                "hw-id-001",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProvisioned,
				}))
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses[1]).To(Equal(
				provisioningv1alpha1.InfrastructureResourceStatus{
					ResourceName:              "node-2",
					ResourceId:                "hw-id-002",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				}))
		})
	})

	Context("Enum validation", func() {
		It("should reject an invalid ResourceProvisioningPhase value", func() {
			pr := buildPR("infra-enum-invalid")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			fetched.Status.Extensions.InfrastructureResourceStatuses = []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "node-bad",
					ResourceId:                "hw-id-bad",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhase("BOGUS"),
				},
			}

			// Update the statuses on the CRD
			err := k8sClient.Status().Update(ctx, fetched)

			// Now the statuses should be rejected
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resourceProvisioningPhase"))
		})

		It("should accept all valid enum values", func() {
			pr := buildPR("infra-enum-valid")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			validPhases := []provisioningv1alpha1.ResourceProvisioningPhase{
				provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				provisioningv1alpha1.ResourceProvisioningPhaseProvisioned,
				provisioningv1alpha1.ResourceProvisioningPhaseAwaitingFreeResources,
				provisioningv1alpha1.ResourceProvisioningPhaseFailed,
			}

			resourceIds := []string{"id-1", "id-2", "id-3", "id-4"}
			var statuses []provisioningv1alpha1.InfrastructureResourceStatus
			for i, phase := range validPhases {
				statuses = append(statuses, provisioningv1alpha1.InfrastructureResourceStatus{
					ResourceName:              "node",
					ResourceId:                resourceIds[i],
					ResourceProvisioningPhase: phase,
				})
			}
			pr.Status.Extensions.InfrastructureResourceStatuses = statuses

			// Update the statuses on the CRD
			updatePRStatus(pr)

			// Now the statuses should reflect the valid phases
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses).To(HaveLen(4))
		})
	})

	Context("Clear stale statuses", func() {
		It("should clear infrastructure resource statuses by setting slice to nil", func() {
			pr := buildPR("infra-clear-stale")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			pr.Status.Extensions.InfrastructureResourceStatuses = []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "stale-node",
					ResourceId:                "stale-id",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				},
			}

			// Update the statuses on the CRD
			updatePRStatus(pr)

			// Now the statuses should reflect the PROCESSING phase
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses).To(HaveLen(1))

			// Clear the statuses on the CRD
			fetched.Status.Extensions.InfrastructureResourceStatuses = nil
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			// Now the statuses should be cleared
			cleared := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), cleared)).To(Succeed())
			Expect(cleared.Status.Extensions.InfrastructureResourceStatuses).To(BeEmpty())
		})
	})

	Context("Hostname upgrade", func() {
		It("should update ResourceName from node ID to hostname", func() {
			pr := buildPR("infra-hostname-upgrade")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			// Re-use the same node ID for the resource name until the hostname is populated
			pr.Status.Extensions.InfrastructureResourceStatuses = []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "hw-id-100",
					ResourceId:                "hw-id-100",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				},
			}
			// Update the statuses on the CRD
			updatePRStatus(pr)

			// Now the resource name should reflect the node ID
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses[0].ResourceName).To(Equal("hw-id-100"))

			// Once the hostname is populated, update the resource name
			fetched.Status.Extensions.InfrastructureResourceStatuses[0].ResourceName = "worker-01.example.com"
			fetched.Status.Extensions.InfrastructureResourceStatuses[0].ResourceProvisioningPhase = provisioningv1alpha1.ResourceProvisioningPhaseProvisioned
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			// Now the resource name should reflect the hostname
			updated := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), updated)).To(Succeed())
			Expect(updated.Status.Extensions.InfrastructureResourceStatuses[0].ResourceName).To(Equal("worker-01.example.com"))
			Expect(updated.Status.Extensions.InfrastructureResourceStatuses[0].ResourceId).To(Equal("hw-id-100"))
			Expect(updated.Status.Extensions.InfrastructureResourceStatuses[0].ResourceProvisioningPhase).To(
				Equal(provisioningv1alpha1.ResourceProvisioningPhaseProvisioned))
		})
	})

	Context("Phase upgrade", func() {
		It("should persist a phase transition from PROCESSING to PROVISIONED", func() {
			pr := buildPR("infra-phase-upgrade")
			Expect(k8sClient.Create(ctx, pr)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			// PROCESSING phase
			original := []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "node-x",
					ResourceId:                "id-x",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProcessing,
				},
			}

			// Update the statuses on the CRD
			pr.Status.Extensions.InfrastructureResourceStatuses = original
			updatePRStatus(pr)

			// Now the statuses should reflect the PROCESSING phase
			fetched := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
			Expect(fetched.Status.Extensions.InfrastructureResourceStatuses).To(Equal(original))

			// PROVISIONED phase
			updated := []provisioningv1alpha1.InfrastructureResourceStatus{
				{
					ResourceName:              "node-x",
					ResourceId:                "id-x",
					ResourceProvisioningPhase: provisioningv1alpha1.ResourceProvisioningPhaseProvisioned,
				},
			}

			// Update the statuses on the CRD
			fetched.Status.Extensions.InfrastructureResourceStatuses = updated
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			// Now the statuses should reflect the PROVISIONED phase
			changed := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), changed)).To(Succeed())
			Expect(changed.Status.Extensions.InfrastructureResourceStatuses).To(Equal(updated))
		})
	})
})
