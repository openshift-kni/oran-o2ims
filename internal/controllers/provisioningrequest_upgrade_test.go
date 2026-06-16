/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testUpgradeSchema is the common upgrade parameter schema used across upgrade tests.
var testUpgradeSchema = runtime.RawExtension{
	Raw: []byte(`{
		"type":"object",
		"properties":{
			"upgradeParameters":{
				"type":"object",
				"properties":{
					"imageBasedGroupUpgrade":{
						"type":"object",
						"properties":{
							"ibuSpec":{
								"type":"object",
								"properties":{
									"seedImageRef":{
										"type":"object",
										"properties":{
											"image":{"type":"string"},
											"version":{"type":"string"}
										}
									},
									"oadpContent":{
										"type":"array",
										"items":{
											"type":"object",
											"properties":{
												"name":{"type":"string"},
												"namespace":{"type":"string"}
											}
										}
									}
								}
							},
							"plan":{
								"type":"array",
								"items":{
									"type":"object",
									"properties":{
										"actions":{
											"type":"array",
											"items":{"type":"string"}
										},
										"rolloutStrategy":{
											"type":"object",
											"properties":{
												"maxConcurrency":{"type":"integer"},
												"timeout":{"type":"integer"}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`),
}

// toMap unmarshals JSON bytes to map[string]any for test comparisons.
func toMap(jsonBytes []byte) map[string]any {
	var m map[string]any
	ExpectWithOffset(1, json.Unmarshal(jsonBytes, &m)).To(Succeed())
	return m
}

var _ = Describe("mergeAndValidateUpgradeData", func() {
	var (
		task            *provisioningRequestReconcilerTask
		clusterTemplate *provisioningv1alpha1.ClusterTemplate
	)

	upgradeDefaults := runtime.RawExtension{
		Raw: []byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"image","version":"4.17.0"},"oadpContent":[{"name":"test","namespace":"test"}]},"plan":[{"actions":["Prep"]},{"actions":["AbortOnFailure"],"rolloutStrategy":{"maxConcurrency":1,"timeout":5}},{"actions":["Upgrade"]},{"actions":["AbortOnFailure"],"rolloutStrategy":{"maxConcurrency":1,"timeout":5}},{"actions":["FinalizeUpgrade"]}]}}`),
	}

	BeforeEach(func() {
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "merge-test-pr", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "test-template",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(`{"clusterInstanceParameters":{"clusterName":"test"}}`),
				},
			},
		}
		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template.v1.0.0", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.17.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					UpgradeDefaults: upgradeDefaults,
				},
				TemplateParameterSchema: testUpgradeSchema,
			},
		}
		task = &provisioningRequestReconcilerTask{
			object: pr,
			logger: slog.New(slog.DiscardHandler),
		}
	})

	It("should return defaults when no upgradeParameters in PR", func() {
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		expected := toMap(upgradeDefaults.Raw)
		Expect(result).To(Equal(expected))
	})

	It("should merge PR overrides on top of defaults including plan with rolloutStrategy", func() {
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"new-image"}},"plan":[{"actions":["Prep"],"rolloutStrategy":{"maxConcurrency":1,"timeout":15}},{"actions":["AbortOnFailure"]},{"actions":["Upgrade"],"rolloutStrategy":{"maxConcurrency":1,"timeout":60}},{"actions":["AbortOnFailure"]}]}}}`),
		}
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		expected := toMap([]byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"new-image","version":"4.17.0"},"oadpContent":[{"name":"test","namespace":"test"}]},"plan":[{"actions":["Prep"],"rolloutStrategy":{"maxConcurrency":1,"timeout":15}},{"actions":["AbortOnFailure"],"rolloutStrategy":{"maxConcurrency":1,"timeout":5}},{"actions":["Upgrade"],"rolloutStrategy":{"maxConcurrency":1,"timeout":60}},{"actions":["AbortOnFailure"],"rolloutStrategy":{"maxConcurrency":1,"timeout":5}},{"actions":["FinalizeUpgrade"]}]}}`))
		Expect(result).To(Equal(expected))
	})

	It("should use PR input when defaults are empty object", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(`{}`)}
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"pr-image","version":"4.17.0"}},"plan":[{"actions":["Prep"]}]}}}`),
		}
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		expected := toMap([]byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"pr-image","version":"4.17.0"}},"plan":[{"actions":["Prep"]}]}}`))
		Expect(result).To(Equal(expected))
	})

	It("should use PR input when defaults are not set", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{}
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"pr-image","version":"4.17.0"}},"plan":[{"actions":["Prep"]}]}}}`),
		}
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		expected := toMap([]byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"pr-image","version":"4.17.0"}},"plan":[{"actions":["Prep"]}]}}`))
		Expect(result).To(Equal(expected))
	})

	It("should merge when some values are in defaults and others in PR", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"version":"4.17.0"}}}}`),
		}
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"pr-image"}},"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}}`),
		}
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		expected := toMap([]byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"version":"4.17.0","image":"pr-image"}},"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]}]}}`))
		Expect(result).To(Equal(expected))
	})

	It("should return error when upgradeParameters schema does not exist", func() {
		clusterTemplate.Spec.TemplateParameterSchema = runtime.RawExtension{
			Raw: []byte(`{"type":"object","properties":{"otherParam":{"type":"string"}}}`),
		}
		_, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract upgradeParameters schema"))
	})

	It("should return empty map when imageBasedGroupUpgrade is missing from both defaults and PR", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(`{}`)}
		result, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeEmpty())
	})

	It("should return error when merged value does not match the schema", func() {
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":123}}}}}`),
		}
		_, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("do not match the schema"))
	})

	It("should return error when upgradeParameters is not a map", func() {
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":"not-an-object"}`),
		}
		_, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("upgradeParameters is not a map"))
	})

	It("should return error when templateParameters is malformed JSON", func() {
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{invalid`),
		}
		_, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to unmarshal templateParameters"))
	})

	It("should return error when upgradeDefaults is not a map", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`[]`),
		}
		_, err := task.mergeAndValidateUpgradeData(clusterTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("upgradeDefaults is not a map"))
	})
})

var _ = Describe("prepareIBGU", func() {
	var (
		ctx             context.Context
		task            *provisioningRequestReconcilerTask
		clusterTemplate *provisioningv1alpha1.ClusterTemplate
		c               client.Client
	)

	upgradeDefaults := runtime.RawExtension{
		Raw: []byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"image":"image","version":"4.17.0"},"oadpContent":[{"name":"test","namespace":"test"}]},"plan":[{"actions":["Prep"]},{"actions":["Upgrade"]},{"actions":["FinalizeUpgrade"]}]}}`),
	}

	BeforeEach(func() {
		ctx = context.Background()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prepare-cluster"}}
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "prepare-cluster", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "test-template",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(`{"clusterInstanceParameters":{"clusterName":"prepare-cluster"}}`),
				},
			},
		}
		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template.v1.0.0", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.17.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					UpgradeDefaults: upgradeDefaults,
				},
				TemplateParameterSchema: testUpgradeSchema,
			},
		}
		c = fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			pr, clusterTemplate, ns,
		).WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).Build()
		task = &provisioningRequestReconcilerTask{
			client: c,
			object: pr,
			logger: slog.New(slog.DiscardHandler),
		}
	})

	It("should build a valid IBGU from defaults", func() {
		ibguCR, err := task.prepareIBGU(ctx, clusterTemplate, "prepare-cluster")
		Expect(err).ToNot(HaveOccurred())
		Expect(ibguCR).ToNot(BeNil())
		Expect(ibguCR.Spec.IBUSpec.SeedImageRef.Version).To(Equal("4.17.0"))
		Expect(ibguCR.Spec.IBUSpec.SeedImageRef.Image).To(Equal("image"))
		Expect(ibguCR.Spec.IBUSpec.OADPContent).To(HaveLen(1))
		Expect(ibguCR.Spec.Plan).To(HaveLen(3))
		Expect(ibguCR.Spec.ClusterLabelSelectors).To(HaveLen(1))
		Expect(ibguCR.Spec.ClusterLabelSelectors[0].MatchLabels["name"]).To(Equal("prepare-cluster"))
	})

	It("should return InputError when version mismatches", func() {
		task.object.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{"seedImageRef":{"version":"4.18.0"}}}}}`),
		}
		_, err := task.prepareIBGU(ctx, clusterTemplate, "prepare-cluster")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("does not match the ClusterTemplate spec.release"))
	})

	It("should return InputError when IBGU key is missing from both defaults and PR", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"wrongKey":{}}`),
		}
		_, err := task.prepareIBGU(ctx, clusterTemplate, "prepare-cluster")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
	})
})

var _ = Describe("detectUpgradeType", func() {
	var (
		ct *provisioningv1alpha1.ClusterTemplate
		pr *provisioningv1alpha1.ProvisioningRequest
	)

	BeforeEach(func() {
		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ct"},
		}
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
			},
		}
	})

	It("should detect clusterVersion from CT defaults when PR has no upgrade params", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}`),
		}
		upgradeType, err := detectUpgradeType(ct, pr)
		Expect(err).ToNot(HaveOccurred())
		Expect(upgradeType).To(Equal(utils.UpgradeDefaultsClusterVersionKey))
	})

	It("should detect imageBasedGroupUpgrade from CT defaults when PR has no upgrade params", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{}}}`),
		}
		upgradeType, err := detectUpgradeType(ct, pr)
		Expect(err).ToNot(HaveOccurred())
		Expect(upgradeType).To(Equal(utils.UpgradeDefaultsIBGUKey))
	})

	It("should detect clusterVersion from PR params when CT defaults is empty", func() {
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}}`),
		}
		upgradeType, err := detectUpgradeType(ct, pr)
		Expect(err).ToNot(HaveOccurred())
		Expect(upgradeType).To(Equal(utils.UpgradeDefaultsClusterVersionKey))
	})

	It("should detect imageBasedGroupUpgrade from PR params when CT defaults is empty", func() {
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{}}}}`),
		}
		upgradeType, err := detectUpgradeType(ct, pr)
		Expect(err).ToNot(HaveOccurred())
		Expect(upgradeType).To(Equal(utils.UpgradeDefaultsIBGUKey))
	})

	It("should return error when both types are in CT defaults", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{},"imageBasedGroupUpgrade":{}}`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("only one upgrade type is allowed"))
	})

	It("should return error when both types are in PR params", func() {
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"clusterVersion":{},"imageBasedGroupUpgrade":{}}}`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("only one upgrade type is allowed"))
	})

	It("should return error when clusterVersion in CT defaults and imageBasedGroupUpgrade in PR params", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{}}`),
		}
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{}}}`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("only one upgrade type is allowed"))
	})

	It("should return error when no upgrade configuration is provided", func() {
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no upgrade configuration found"))
	})

	It("should return error when upgradeDefaults is not valid JSON", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{invalid`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse upgradeDefaults"))
	})

	It("should return error when templateParameters is not valid JSON", func() {
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{invalid`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse templateParameters"))
	})

	It("should return error when upgradeParameters is not a map", func() {
		pr.Spec.TemplateParameters = runtime.RawExtension{
			Raw: []byte(`{"upgradeParameters":"not-a-map"}`),
		}
		_, err := detectUpgradeType(ct, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a map"))
	})
})

var _ = Describe("handleUpgrade", func() {
	var (
		ctx         context.Context
		task        *provisioningRequestReconcilerTask
		c           client.Client
		ct          *provisioningv1alpha1.ClusterTemplate
		pr          *provisioningv1alpha1.ProvisioningRequest
		clusterName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		clusterName = "test-cluster"

		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template.v1.0.0", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.17.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					UpgradeDefaults: runtime.RawExtension{Raw: []byte(`{}`)},
				},
				TemplateParameterSchema: testUpgradeSchema,
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:       "test-template",
				TemplateVersion:    "v1.0.0",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
			},
		}
	})

	setupClient := func(objs ...client.Object) {
		c = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(pr).
			Build()
		task = &provisioningRequestReconcilerTask{
			client: c,
			object: pr,
			logger: slog.New(slog.DiscardHandler),
		}
	}

	It("should set PreconditionChecksFailed when no recognized upgrade configuration is found", func() {
		setupClient(ct, pr)

		result, proceed, err := task.handleUpgrade(ctx, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(proceed).To(BeFalse())
		Expect(result.RequeueAfter).To(BeZero())

		upgradeCond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
		Expect(upgradeCond).ToNot(BeNil())
		Expect(upgradeCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed)))
		Expect(upgradeCond.Message).To(ContainSubstring("no upgrade configuration found"))
		Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
			Equal(provisioningv1alpha1.StateFailed))
	})

	It("should return error when ClusterTemplate is missing", func() {
		pr.Spec.TemplateName = "non-existent"
		setupClient(pr)

		_, _, err := task.handleUpgrade(ctx, clusterName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get clusterTemplate"))
	})

	It("should dispatch to handleClusterVersionUpgrade for clusterVersion type", func() {
		ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.17.0"}}}`),
		}
		setupClient(
			ct, pr,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}},
		)

		result, proceed, err := task.handleUpgrade(ctx, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(proceed).To(BeFalse())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})
})
