/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils/spokeclient"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
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

var _ = Describe("prepareCVSpec", func() {
	var (
		task            *provisioningRequestReconcilerTask
		clusterTemplate *provisioningv1alpha1.ClusterTemplate
	)

	BeforeEach(func() {
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "cv-spec-test-pr"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
			},
		}
		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template.v1.0.0", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.22.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					UpgradeDefaults: runtime.RawExtension{
						Raw: []byte(`{"clusterVersion":{"desiredUpdate":{}}}`),
					},
				},
				TemplateParameterSchema: runtime.RawExtension{
					Raw: []byte(`{"properties":{"upgradeParameters":{"type":"object"}}}`),
				},
			},
		}
		task = &provisioningRequestReconcilerTask{
			object: pr,
			logger: slog.New(slog.DiscardHandler),
		}
	})

	It("should return cvSpec with version, channel, image, and force from upgrade defaults", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{"channel":"stable-4.22","upstream":"https://custom.graph","desiredUpdate":{"version":"4.22.0","image":"quay.io/ocp:4.22.0","force":true}}}`),
		}
		cvSpec, err := task.prepareCVSpec(clusterTemplate, "4.22.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(cvSpec.DesiredUpdate).ToNot(BeNil())
		Expect(cvSpec.DesiredUpdate.Version).To(Equal("4.22.0"))
		Expect(cvSpec.DesiredUpdate.Image).To(Equal("quay.io/ocp:4.22.0"))
		Expect(cvSpec.DesiredUpdate.Force).To(BeTrue())
		Expect(cvSpec.Channel).To(Equal("stable-4.22"))
		Expect(string(cvSpec.Upstream)).To(Equal("https://custom.graph"))
	})

	It("should return InputError when clusterVersion key is missing", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"imageBasedGroupUpgrade":{}}`),
		}
		_, err := task.prepareCVSpec(clusterTemplate, "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("not found in merged upgrade data"))
	})

	It("should return InputError when desiredUpdate version mismatches target", func() {
		clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
			Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.21.0"}}}`),
		}
		_, err := task.prepareCVSpec(clusterTemplate, "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("does not match the ClusterTemplate spec.release"))
	})

	It("should return InputError when target version is not valid semver", func() {
		_, err := task.prepareCVSpec(clusterTemplate, "invalid-version")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("invalid target version"))
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
			client:   c,
			object:   pr,
			logger:   slog.New(slog.DiscardHandler),
			timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
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

var _ = Describe("parseUpgradeConfig", func() {
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

	Context("upgrade type detection", func() {
		It("should detect clusterVersion from CT defaults when PR has no upgrade params", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.UpgradeType).To(Equal(utils.UpgradeDefaultsClusterVersionKey))
		})

		It("should detect imageBasedGroupUpgrade from CT defaults when PR has no upgrade params", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"imageBasedGroupUpgrade":{"ibuSpec":{}}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.UpgradeType).To(Equal(utils.UpgradeDefaultsIBGUKey))
		})

		It("should detect clusterVersion from PR params when CT defaults is empty", func() {
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.UpgradeType).To(Equal(utils.UpgradeDefaultsClusterVersionKey))
		})

		It("should detect imageBasedGroupUpgrade from PR params when CT defaults is empty", func() {
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"imageBasedGroupUpgrade":{"ibuSpec":{}}}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.UpgradeType).To(Equal(utils.UpgradeDefaultsIBGUKey))
		})

		It("should return error when both types are in CT defaults", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"imageBasedGroupUpgrade":{}}`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only one upgrade type is allowed"))
		})

		It("should return error when both types are in PR params", func() {
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterVersion":{},"imageBasedGroupUpgrade":{}}}`),
			}
			_, err := parseUpgradeConfig(ct, pr)
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
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only one upgrade type is allowed"))
		})

		It("should return clusterVersion when both CT defaults and PR params have clusterVersion", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"channel":"stable-4.22"}}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.UpgradeType).To(Equal(utils.UpgradeDefaultsClusterVersionKey))
		})

		It("should return error when no upgrade configuration is provided", func() {
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no upgrade configuration found"))
		})

		It("should return error when upgradeDefaults is not valid JSON", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{invalid`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse upgradeDefaults"))
		})

		It("should return error when templateParameters is not valid JSON", func() {
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{invalid`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse templateParameters"))
		})

		It("should return error when upgradeParameters is not a map", func() {
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":"not-a-map"}`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is not a map"))
		})
	})

	Context("timeout extraction", func() {
		It("should return zero timeout when no timeout is set", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Timeout).To(BeZero())
		})

		It("should return custom timeout from PR params", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{}}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterUpgradeTimeout":"120m"}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Timeout).To(Equal(120 * time.Minute))
		})

		It("should return error for invalid duration in PR params", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{}}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterUpgradeTimeout":"invalid"}}`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid clusterUpgradeTimeout"))
		})

		It("should fall back to CT defaults when PR has no timeout", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"clusterUpgradeTimeout":"90m"}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Timeout).To(Equal(90 * time.Minute))
		})

		It("should prefer PR timeout over CT defaults", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"clusterUpgradeTimeout":"90m"}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"clusterUpgradeTimeout":"120m"}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Timeout).To(Equal(120 * time.Minute))
		})

		It("should return error when CT default timeout is invalid and PR has no timeout", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"clusterUpgradeTimeout":"not-a-duration"}`),
			}
			_, err := parseUpgradeConfig(ct, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid clusterUpgradeTimeout"))
		})
	})

	Context("intermediateVersion extraction", func() {
		It("should extract intermediateVersion from PR params", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{}}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"intermediateVersion":"4.21.0"}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.IntermediateVersion).To(Equal("4.21.0"))
		})

		It("should fall back to CT defaults when PR has no intermediateVersion", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"intermediateVersion":"4.19.0"}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.IntermediateVersion).To(Equal("4.19.0"))
		})

		It("should prefer PR intermediateVersion over CT defaults", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{},"intermediateVersion":"4.19.0"}`),
			}
			pr.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(`{"upgradeParameters":{"intermediateVersion":"4.21.0"}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.IntermediateVersion).To(Equal("4.21.0"))
		})

		It("should return empty intermediateVersion when not set", func() {
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{}}`),
			}
			cfg, err := parseUpgradeConfig(ct, pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.IntermediateVersion).To(BeEmpty())
		})
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

		spokeclient.ClearCache()

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

	AfterEach(func() {
		spokeclient.ClearCache()
	})

	setupClient := func(objs ...client.Object) {
		c = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(pr).
			Build()
		task = &provisioningRequestReconcilerTask{
			client:   c,
			object:   pr,
			logger:   slog.New(slog.DiscardHandler),
			timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
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
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   clusterName,
					Labels: map[string]string{"openshiftVersion": "4.16.0"},
				},
			},
			&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-serviceaccount", Namespace: clusterName,
				},
			},
		)

		result, proceed, err := task.handleUpgrade(ctx, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(proceed).To(BeFalse())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})
})

var _ = Describe("handleClusterVersionUpgrade", func() {
	var (
		ctx         context.Context
		task        *provisioningRequestReconcilerTask
		c           client.Client
		pr          *provisioningv1alpha1.ProvisioningRequest
		clusterName string
		ct          *provisioningv1alpha1.ClusterTemplate
	)

	// Reusable CV conditions.
	var (
		progressingTrue = configv1.ClusterOperatorStatusCondition{
			Type: configv1.OperatorProgressing, Status: configv1.ConditionTrue,
			Message: "Working towards 4.22.0: 485 of 904 done (53% complete)",
		}
		upgradeableFalse = configv1.ClusterOperatorStatusCondition{
			Type: configv1.OperatorUpgradeable, Status: configv1.ConditionFalse,
			Message: "Cluster should not be upgraded between minor versions for multiple reasons",
		}
		failingTrue = configv1.ClusterOperatorStatusCondition{
			Type: utils.CVConditionFailing, Status: configv1.ConditionTrue,
			Message: "Unable to update: multiple errors occurred",
		}
		invalidTrue = configv1.ClusterOperatorStatusCondition{
			Type: utils.CVConditionInvalid, Status: configv1.ConditionTrue,
			Message: "spec.desiredUpdate.version: Invalid value",
		}
		releaseAcceptedFalse = configv1.ClusterOperatorStatusCondition{
			Type: utils.CVConditionReleaseAccepted, Status: configv1.ConditionFalse,
			Message: "Release verification failed",
		}
		retrievedUpdatesTrue = configv1.ClusterOperatorStatusCondition{
			Type: configv1.RetrievedUpdates, Status: configv1.ConditionTrue,
		}
		retrievedUpdatesFalse = configv1.ClusterOperatorStatusCondition{
			Type: configv1.RetrievedUpdates, Status: configv1.ConditionFalse,
			Message: "Unable to retrieve available updates",
		}
	)

	newBaseCV := func() *configv1.ClusterVersion {
		return &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{Name: "version", Generation: 1},
			Status: configv1.ClusterVersionStatus{
				ObservedGeneration: 1,
			},
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		clusterName = "test-cluster"
		spokeclient.ClearCache()

		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template.v1.0.0", Namespace: "test-ns"},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.22.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					UpgradeDefaults: runtime.RawExtension{
						Raw: []byte(`{"clusterVersion":{"desiredUpdate":{}}}`),
					},
				},
				TemplateParameterSchema: runtime.RawExtension{
					Raw: []byte(`{"properties":{"upgradeParameters":{"type":"object"}}}`),
				},
			},
		}
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{
						Name: clusterName,
					},
				},
			},
		}
	})

	var spokeClient client.Client

	// buildSpoke creates a mock spoke client with a ClusterVersion and optional extra objects.
	buildSpoke := func(cv *configv1.ClusterVersion, extraObjs ...client.Object) {
		objs := append([]client.Object{cv}, extraObjs...)
		spokeClient = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objs...).Build()
		spokeclient.SetTestSpokeClientCreator(func(apiServerURL, token string, caCert []byte, spokeScheme *runtime.Scheme) (client.Client, error) {
			return spokeClient, nil
		})
	}

	// setupWithSpokeReady creates a hub client with all resources needed for
	// the spoke client to be ready.
	setupWithSpokeReady := func(extraObjs ...client.Object) {
		objs := []client.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}},
			&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-serviceaccount", Namespace: clusterName,
				},
			},
			&msav1beta1.ManagedServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr-upgrade", Namespace: clusterName,
				},
				Status: msav1beta1.ManagedServiceAccountStatus{
					TokenSecretRef: &msav1beta1.SecretRef{Name: "test-pr-upgrade-token"},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr-upgrade-token", Namespace: clusterName,
					ResourceVersion: "100",
				},
				Data: map[string][]byte{"token": []byte("t"), "ca.crt": []byte("c")},
			},
			&workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr-upgrade-rbac", Namespace: clusterName,
				},
				Status: workv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               workv1.WorkAvailable,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   clusterName,
					Labels: map[string]string{"openshiftVersion": "4.21.0"},
				},
				Spec: clusterv1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []clusterv1.ClientConfig{
						{URL: "https://api.test-cluster.example.com:6443"},
					},
				},
			},
			pr,
		}
		objs = append(objs, extraObjs...)

		c = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(pr).
			Build()
		task = &provisioningRequestReconcilerTask{
			client:   c,
			object:   pr,
			logger:   slog.New(slog.DiscardHandler),
			timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
		}
	}

	// setupClientWithoutSpokeReady creates a hub client without spoke resources.
	setupClientWithoutSpokeReady := func(objs ...client.Object) {
		allObjs := append([]client.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}},
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   clusterName,
					Labels: map[string]string{"openshiftVersion": "4.21.0"},
				},
			},
			pr,
		}, objs...)
		c = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(allObjs...).
			WithStatusSubresource(pr).
			Build()
		task = &provisioningRequestReconcilerTask{
			client:   c,
			object:   pr,
			logger:   slog.New(slog.DiscardHandler),
			timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
		}
	}

	assertSpokeResourcesCleaned := func() {
		msa := &msav1beta1.ManagedServiceAccount{}
		err := c.Get(ctx, types.NamespacedName{Name: "test-pr-upgrade", Namespace: clusterName}, msa)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())

		mw := &workv1.ManifestWork{}
		err = c.Get(ctx, types.NamespacedName{Name: "test-pr-upgrade-rbac", Namespace: clusterName}, mw)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	}

	assertUpgradeCondition := func(reason string, msgSubstring string) {
		condition := meta.FindStatusCondition(task.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Reason).To(Equal(reason))
		if msgSubstring != "" {
			Expect(condition.Message).To(ContainSubstring(msgSubstring))
		}
	}

	// setEUSStartVersion pre-sets ClusterUpgradeStatus with StartVersion=4.20.0
	// so initUpgradeStatus skips the ManagedCluster read.
	setEUSStartVersion := func() {
		task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
			StartVersion: "4.20.0",
		}
	}

	// newUpdatedWorkerMCP returns a worker MCP with Updated=True.
	newUpdatedWorkerMCP := func() *mcfgv1.MachineConfigPool {
		return &mcfgv1.MachineConfigPool{
			ObjectMeta: metav1.ObjectMeta{Name: "worker"},
			Status: mcfgv1.MachineConfigPoolStatus{
				Conditions: []mcfgv1.MachineConfigPoolCondition{
					{Type: mcfgv1.MachineConfigPoolUpdated, Status: corev1.ConditionTrue},
				},
			},
		}
	}

	// assertMCPPaused checks whether the named MCP on the spoke has spec.paused set as expected.
	assertMCPPaused := func(mcpName string, expectedPaused bool) {
		mcp := &mcfgv1.MachineConfigPool{}
		ExpectWithOffset(1, spokeClient.Get(ctx, types.NamespacedName{Name: mcpName}, mcp)).To(Succeed())
		ExpectWithOffset(1, mcp.Spec.Paused).To(Equal(expectedPaused))
	}

	// --- Resource Preparation ---

	Context("resource preparation", func() {
		It("should set PreconditionChecksFailed when openshiftVersion label is missing", func() {
			allObjs := []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}},
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName},
				},
				pr,
			}
			c = fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(allObjs...).
				WithStatusSubresource(pr).
				Build()
			task = &provisioningRequestReconcilerTask{
				client:   c,
				object:   pr,
				logger:   slog.New(slog.DiscardHandler),
				timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"openshiftVersion label not found")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
		})

		It("should set PreconditionChecksFailed when managed-serviceaccount addon is missing", func() {
			setupClientWithoutSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"managed-serviceaccount addon is not available")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set Pending and requeue when spoke client not ready", func() {
			setupClientWithoutSpokeReady(
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managed-serviceaccount", Namespace: clusterName,
					},
				},
			)

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Preparing upgrade resources")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should time out when spoke client not ready and timeout exceeded", func() {
			setupClientWithoutSpokeReady(
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managed-serviceaccount", Namespace: clusterName,
					},
				},
			)

			pastTime := metav1.NewTime(time.Now().Add(-5 * time.Hour))
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &pastTime}

			result, _, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

	})

	// --- observedGeneration mismatch protection ---

	Context("observedGeneration mismatch", func() {
		It("should requeue when observedGeneration does not match generation", func() {
			cv := newBaseCV()
			cv.Generation = 5
			cv.Status.ObservedGeneration = 4
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		})

		It("should time out when observedGeneration mismatch persists beyond timeout", func() {
			cv := newBaseCV()
			cv.Generation = 5
			cv.Status.ObservedGeneration = 4
			buildSpoke(cv)
			setupWithSpokeReady()

			pastTime := metav1.NewTime(time.Now().Add(-5 * time.Hour))
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &pastTime}

			result, _, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
			assertSpokeResourcesCleaned()
		})
	})

	// --- Pre-Start (The target version is not available in the ClusterVersion's history) ---

	Context("pre-start", func() {
		It("should set PreconditionChecksFailed when upgrade data is invalid", func() {
			cv := newBaseCV()
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{invalid`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed), "")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set PreconditionChecksFailed when MCPs are paused", func() {
			cv := newBaseCV()
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv,
				&mcfgv1.MachineConfigPool{
					ObjectMeta: metav1.ObjectMeta{Name: "master"},
				},
				&mcfgv1.MachineConfigPool{
					ObjectMeta: metav1.ObjectMeta{Name: "worker"},
					Spec:       mcfgv1.MachineConfigPoolSpec{Paused: true},
				},
			)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"MachineConfigPools are paused")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set PreconditionChecksFailed when desiredUpdate version mismatches target", func() {
			cv := newBaseCV()
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.21.0"}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"does not match the ClusterTemplate spec.release")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set Pending when Upgradeable=False for minor upgrade", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				upgradeableFalse, retrievedUpdatesTrue,
			}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.0"}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Cluster should not be upgraded")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should bypass Upgradeable check when force=true for minor upgrade", func() {
			cv := newBaseCV()
			// Current version is 4.21.0, target version is 4.22.0.
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				upgradeableFalse, retrievedUpdatesTrue,
			}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.0","force":true}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to desired version 4.22.0 triggered. Waiting for upgrade to start")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should bypass Upgradeable check for z-stream upgrade", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				upgradeableFalse, retrievedUpdatesTrue,
			}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.3"}}
			buildSpoke(cv)

			ct.Spec.Release = "4.22.3"
			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.3"}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to desired version 4.22.3 triggered. Waiting for upgrade to start")
		})

		It("should set Pending when channel/upstream patched", func() {
			cv := newBaseCV()
			cv.Spec.Channel = "stable-4.21"
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"channel":"stable-4.22","desiredUpdate":{}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Channel/upstream updated")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set Pending when RetrievedUpdates is not True", func() {
			cv := newBaseCV()
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesFalse}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Unable to retrieve available updates")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set PreconditionChecksFailed when target not in availableUpdates and no image", func() {
			cv := newBaseCV()
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"not available for upgrade")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set Pending 'triggered' when desiredUpdate changed", func() {
			cv := newBaseCV()
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to desired version 4.22.0 triggered. Waiting for upgrade to start")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set PreconditionChecksFailed when Invalid=True after trigger", func() {
			cv := newBaseCV()
			cv.Spec.DesiredUpdate = &configv1.Update{Version: "4.22.0"}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				retrievedUpdatesTrue, invalidTrue,
			}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"Invalid value")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set Pending when ReleaseAccepted=False after trigger", func() {
			cv := newBaseCV()
			cv.Spec.DesiredUpdate = &configv1.Update{
				Version: "4.22.0",
				Image:   "quay.io/openshift-release/ocp-release:4.22.0",
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				retrievedUpdatesTrue, releaseAcceptedFalse,
			}
			buildSpoke(cv)

			ct.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{
				Raw: []byte(`{"clusterVersion":{"desiredUpdate":{"version":"4.22.0","image":"quay.io/openshift-release/ocp-release:4.22.0"}}}`),
			}
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Release verification failed")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set Unknown with Failing message when Failing=True after trigger", func() {
			cv := newBaseCV()
			cv.Spec.DesiredUpdate = &configv1.Update{Version: "4.22.0"}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
				retrievedUpdatesTrue, failingTrue,
			}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Unknown),
				"multiple errors occurred")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set Unknown 'upgrade not started yet' when no Failing condition after trigger", func() {
			cv := newBaseCV()
			cv.Spec.DesiredUpdate = &configv1.Update{Version: "4.22.0"}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Unknown),
				"upgrade not started yet")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should time out during pre-start phase", func() {
			cv := newBaseCV()
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()

			pastTime := metav1.NewTime(time.Now().Add(-5 * time.Hour))
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &pastTime}

			result, _, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
			assertSpokeResourcesCleaned()
		})

		It("[EUS] should set PreconditionChecksFailed when MCPs not updated", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.21.0"}}
			buildSpoke(cv,
				&mcfgv1.MachineConfigPool{
					ObjectMeta: metav1.ObjectMeta{Name: "worker"},
					Status: mcfgv1.MachineConfigPoolStatus{
						Conditions: []mcfgv1.MachineConfigPoolCondition{
							{Type: mcfgv1.MachineConfigPoolUpdated, Status: corev1.ConditionFalse},
						},
					},
				},
			)
			setupWithSpokeReady()
			setEUSStartVersion()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"MachineConfigPools not updated")
		})

		It("[EUS] should pause worker MCPs and trigger intermediate version", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.21.0"}}
			masterMCP := &mcfgv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "master"},
				Status: mcfgv1.MachineConfigPoolStatus{
					Conditions: []mcfgv1.MachineConfigPoolCondition{
						{Type: mcfgv1.MachineConfigPoolUpdated, Status: corev1.ConditionTrue},
					},
				},
			}
			buildSpoke(cv, masterMCP, newUpdatedWorkerMCP())
			setupWithSpokeReady()
			setEUSStartVersion()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to intermediate version 4.21.0 triggered")
			assertMCPPaused("worker", true)
			assertMCPPaused("master", false)
		})

		It("[EUS] should unpause worker MCPs on intermediate version PreconditionChecksFailed", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			pausedWorkerMCP := newUpdatedWorkerMCP()
			pausedWorkerMCP.Spec.Paused = true
			buildSpoke(cv, pausedWorkerMCP)
			setupWithSpokeReady()
			setEUSStartVersion()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"not available for upgrade")
			assertMCPPaused("worker", false)
		})

		It("[EUS] intermediate completed should trigger target version", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.CompletedUpdate},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.22.0"}}
			buildSpoke(cv)
			setupWithSpokeReady()
			setEUSStartVersion()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to desired version 4.22.0 triggered")
		})

		It("[EUS] should set PreconditionChecksFailed when target not in availableUpdates after intermediate completed", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.CompletedUpdate},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{retrievedUpdatesTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			setEUSStartVersion()

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"not available for upgrade")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
		})
	})

	// --- In-Progress (The target version is available in the ClusterVersion's history, not completed) ---

	Context("in-progress", func() {
		It("should set InProgress with Progressing message and reset startAt", func() {
			startedTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.PartialUpdate, StartedTime: startedTime},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{progressingTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &now, StartVersion: "4.21.0",
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"485 of 904 done")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt.Time).To(
				BeTemporally("~", startedTime.Time, time.Second))
		})

		It("should set Unknown 'CVO stalled' when Progressing=False and no Failing", func() {
			now := metav1.Now()
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.PartialUpdate, StartedTime: now},
			}
			buildSpoke(cv)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Unknown),
				"CVO stalled")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should set Unknown with Failing message when Progressing=False and Failing=True", func() {
			now := metav1.Now()
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.PartialUpdate, StartedTime: now},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{failingTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Unknown),
				"multiple errors occurred")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).ToNot(BeNil())
		})

		It("should time out when upgrade exceeds timeout and clear startAt", func() {
			pastTime := metav1.NewTime(time.Now().Add(-5 * time.Hour))
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.PartialUpdate, StartedTime: pastTime},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{progressingTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &pastTime, StartVersion: "4.21.0",
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out")
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
			assertSpokeResourcesCleaned()
		})

		It("[EUS] should report upgrading to intermediate version", func() {
			now := metav1.Now()
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.PartialUpdate, StartedTime: now},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{progressingTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &now, StartVersion: "4.20.0",
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to intermediate version 4.21.0")
		})

		It("[EUS] should time out and leave worker MCPs paused", func() {
			pastTime := metav1.NewTime(time.Now().Add(-9 * time.Hour))
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.PartialUpdate, StartedTime: pastTime},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{progressingTrue}
			workerMCP := &mcfgv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec:       mcfgv1.MachineConfigPoolSpec{Paused: true},
			}
			buildSpoke(cv, workerMCP)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &pastTime, StartVersion: "4.20.0",
			}

			result, _, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			Expect(task.timeouts.clusterUpgrade).To(Equal(utils.DefaultClusterEUSUpgradeTimeout))
			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out")
			assertMCPPaused("worker", true)
		})

		It("[EUS] should use custom timeout over 8h default", func() {
			now := metav1.Now()
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.21.0", State: configv1.PartialUpdate, StartedTime: now},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{progressingTrue}
			buildSpoke(cv)
			setupWithSpokeReady()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &now, StartVersion: "4.20.0",
			}

			_, _, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0", Timeout: 2 * time.Hour})
			Expect(err).ToNot(HaveOccurred())
			Expect(task.timeouts.clusterUpgrade).To(Equal(2 * time.Hour))
		})
	})

	// --- Completed ---

	Context("completed", func() {
		It("should set Completed, cleanup, proceed=true, and clear startAt", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.CompletedUpdate},
			}
			buildSpoke(cv)
			setupWithSpokeReady()
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeTrue())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Completed),
				"Upgrade to version 4.22.0 completed")
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).To(BeNil())
			assertSpokeResourcesCleaned()
		})

		It("[EUS] should wait for MCPs to finish updating", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.CompletedUpdate},
				{Version: "4.21.0", State: configv1.CompletedUpdate},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			workerMCP := &mcfgv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec:       mcfgv1.MachineConfigPoolSpec{Paused: true},
			}
			buildSpoke(cv, workerMCP)
			setupWithSpokeReady()
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &now, StartVersion: "4.20.0",
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName,
				&utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeFalse())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Waiting for worker MachineConfigPools to finish updating")
			assertMCPPaused("worker", false)
		})

		It("[EUS] should complete when MCPs are updated", func() {
			cv := newBaseCV()
			cv.Status.History = []configv1.UpdateHistory{
				{Version: "4.22.0", State: configv1.CompletedUpdate},
				{Version: "4.21.0", State: configv1.CompletedUpdate},
				{Version: "4.20.0", State: configv1.CompletedUpdate},
			}
			buildSpoke(cv, newUpdatedWorkerMCP())
			setupWithSpokeReady()
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{
				StartedAt: &now, StartVersion: "4.20.0",
			}

			result, proceed, err := task.handleClusterVersionUpgrade(ctx, ct, clusterName, &utils.UpgradeConfig{IntermediateVersion: "4.21.0"})
			Expect(err).ToNot(HaveOccurred())
			Expect(proceed).To(BeTrue())
			Expect(result.RequeueAfter).To(BeZero())

			assertUpgradeCondition(string(provisioningv1alpha1.CRconditionReasons.Completed),
				"Upgrade to version 4.22.0 completed")
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).To(BeNil())
			assertSpokeResourcesCleaned()
		})
	})
})

var _ = Describe("updateUpgradeStatus", func() {
	var (
		ctx  context.Context
		task *provisioningRequestReconcilerTask
		c    client.Client
		pr   *provisioningv1alpha1.ProvisioningRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pr"},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{},
				},
			},
		}
		c = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(pr).
			WithStatusSubresource(pr).
			Build()
		task = &provisioningRequestReconcilerTask{
			client:   c,
			object:   pr,
			logger:   slog.New(slog.DiscardHandler),
			timeouts: &timeouts{clusterUpgrade: utils.DefaultClusterUpgradeTimeout},
		}
	})

	Context("terminal success", func() {
		It("should set ConditionTrue and clear startAt for Completed", func() {
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.Completed, "Upgrade completed")
			Expect(err).ToNot(HaveOccurred())

			condition := meta.FindStatusCondition(task.object.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Completed)))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus).To(BeNil())
		})
	})

	Context("terminal failure", func() {
		It("should set Failed state and clear startAt for PreconditionChecksFailed", func() {
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, "addon missing")
			Expect(err).ToNot(HaveOccurred())

			condition := meta.FindStatusCondition(task.object.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed)))
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})

		It("should set Failed state and clear startAt for TimedOut", func() {
			now := metav1.Now()
			task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus = &provisioningv1alpha1.ClusterUpgradeStatus{StartedAt: &now}

			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.TimedOut, "Upgrade timed out")
			Expect(err).ToNot(HaveOccurred())

			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateFailed))
			Expect(task.object.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.StartedAt).To(BeNil())
		})
	})

	Context("non-terminal", func() {
		It("should set InProgress state for Pending reason", func() {
			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.Pending, "Preparing resources")
			Expect(err).ToNot(HaveOccurred())

			condition := meta.FindStatusCondition(task.object.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Pending)))
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
		})

		It("should set InProgress state for InProgress reason", func() {
			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.InProgress, "Upgrading")
			Expect(err).ToNot(HaveOccurred())

			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
		})

		It("should set InProgress state for Unknown reason", func() {
			err := task.updateUpgradeStatus(ctx,
				provisioningv1alpha1.CRconditionReasons.Unknown, "CVO stalled")
			Expect(err).ToNot(HaveOccurred())

			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(
				Equal(provisioningv1alpha1.StateProgressing))
		})
	})
})
