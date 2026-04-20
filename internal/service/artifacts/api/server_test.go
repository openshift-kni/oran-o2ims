/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestArtifactsAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Artifacts/API")
}

var _ = Describe("clusterTemplateToManagedInfrastructureTemplate", func() {
	var (
		validUUID       = "fa242779-dfef-414e-b2b1-3b75d6f6b65d"
		paramSchemaJSON = []byte(`{"properties":{"nodeClusterName":{"type":"string"}}}`)
	)

	newClusterTemplate := func(characteristics map[string]string) provisioningv1alpha1.ClusterTemplate {
		return provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "sno-ran-du.v4-18-5-v1",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:                    "sno-ran-du",
				Version:                 "v4-18-5-v1",
				Description:             "Test template",
				TemplateID:              validUUID,
				Characteristics:         characteristics,
				TemplateParameterSchema: runtime.RawExtension{Raw: paramSchemaJSON},
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: "cluster-defaults",
					PolicyTemplateDefaults:  "policy-defaults",
				},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(provisioningv1alpha1.CTconditionTypes.Validated),
						Status:  metav1.ConditionTrue,
						Reason:  "Completed",
						Message: "The cluster template validation succeeded",
					},
				},
			},
		}
	}

	It("should include characteristics when present and field is included", func() {
		chars := map[string]string{"deploymentType": "sno", "networkType": "ran"}
		ct := newClusterTemplate(chars)
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).ToNot(BeNil())
		Expect(*result.Characteristics).To(Equal(chars))
	})

	It("should include all characteristics when multiple are defined", func() {
		chars := map[string]string{
			"deploymentType": "sno",
			"networkType":    "ran",
			"vendor":         "acme",
			"region":         "us-east",
			"tier":           "edge",
		}
		ct := newClusterTemplate(chars)
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).ToNot(BeNil())
		Expect(*result.Characteristics).To(HaveLen(5))
		Expect(*result.Characteristics).To(Equal(chars))
	})

	It("should not include characteristics when field is excluded", func() {
		chars := map[string]string{"deploymentType": "sno"}
		ct := newClusterTemplate(chars)
		excludeFields := "characteristics"
		options := commonapi.NewFieldOptions(nil, nil, &excludeFields)

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).To(BeNil())
	})

	It("should not include characteristics when they are empty", func() {
		ct := newClusterTemplate(map[string]string{})
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).To(BeNil())
	})

	It("should not include characteristics when they are nil", func() {
		ct := newClusterTemplate(nil)
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).To(BeNil())
	})

	It("should include both characteristics and extensions when both are present", func() {
		chars := map[string]string{"deploymentType": "sno"}
		ct := newClusterTemplate(chars)
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Characteristics).ToNot(BeNil())
		Expect(*result.Characteristics).To(Equal(chars))
		Expect(result.Extensions).ToNot(BeNil())
		Expect(*result.Extensions).To(HaveKey("status"))
	})

	It("should map basic fields correctly", func() {
		ct := newClusterTemplate(nil)
		options := commonapi.NewDefaultFieldOptions()

		result, err := clusterTemplateToManagedInfrastructureTemplate(ct, options)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.ArtifactResourceId.String()).To(Equal(validUUID))
		Expect(result.Name).To(Equal("sno-ran-du"))
		Expect(result.Version).To(Equal("v4-18-5-v1"))
		Expect(result.Description).To(Equal("Test template"))
		Expect(result.ParameterSchema).To(HaveKey("properties"))
	})
})
