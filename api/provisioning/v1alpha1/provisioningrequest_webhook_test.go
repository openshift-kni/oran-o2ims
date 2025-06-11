/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ProvisioningRequestValidator", func() {
	var (
		ctx        context.Context
		validator  *provisioningRequestValidator
		oldPr      *ProvisioningRequest
		newPr      *ProvisioningRequest
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.TODO()
		fakeClient = fake.NewClientBuilder().WithScheme(s).
			WithStatusSubresource(
				&ClusterTemplate{},
				&ProvisioningRequest{},
			).Build()

		validator = &provisioningRequestValidator{
			Client: fakeClient,
		}

		oldPr = &ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "123e4567-e89b-12d3-a456-426614174000",
			},
			Spec: ProvisioningRequestSpec{
				Name:            "cluster-1",
				TemplateName:    "clustertemplate-a",
				TemplateVersion: "v1.0.1",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{
					"oCloudSiteId": "local-123",
					"nodeClusterName": "exampleCluster",
					"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
					"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
					}`)},
			},
		}

		// Copy the old PR to serve as a base for new PR
		newPr = oldPr.DeepCopy()
	})

	Describe("ValidateUpdate", func() {
		BeforeEach(func() {
			// Create a new ClusterTemplate
			newCt := &ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "clustertemplate-b.v1.0.2",
					Namespace: "default",
				},
				Spec: ClusterTemplateSpec{
					Name:       "clustertemplate-b",
					Version:    "v1.0.2",
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Templates: Templates{
						ClusterInstanceDefaults: "clusterinstance-defaults-v1",
						PolicyTemplateDefaults:  "policytemplate-defaults-v1",
						HwTemplate:              "hardwaretemplate-v1",
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
				},
				Status: ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(CTconditionTypes.Validated),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, newCt)).To(Succeed())
		})

		Context("when spec.templateName or spec.templateVersion is changed", func() {
			BeforeEach(func() {
				newPr.Spec.TemplateName = "clustertemplate-b"
				newPr.Spec.TemplateVersion = "v1.0.2"
			})

			It("should allow the change when the ProvisioningRequest is fulfilled", func() {
				newPr.Status.ProvisioningStatus.ProvisioningPhase = StateFulfilled
				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should allow the change when the ProvisioningRequest is failed", func() {
				newPr.Status.ProvisioningStatus.ProvisioningPhase = StateFailed
				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
