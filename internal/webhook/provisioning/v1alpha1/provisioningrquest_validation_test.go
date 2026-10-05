/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestValidateActiveUpgradeUpdateEquivalentEmptyInputs(t *testing.T) {
	tests := []struct {
		name   string
		oldRaw string
		newRaw string
	}{
		{
			name:   "remove empty workerPoolUpgrade",
			oldRaw: `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {}}}}`,
			newRaw: `{"upgradeParameters":{}}`,
		},
		{
			name:   "add empty workerPoolUpgrade",
			oldRaw: `{"upgradeParameters":{}}`,
			newRaw: `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {}}}}`,
		},
		{
			name:   "add timeout to empty parameters",
			oldRaw: "",
			newRaw: `{"upgradeParameters": {"clusterVersion": {"clusterUpgradeTimeout": "3h"}}}`,
		},
		{
			name:   "add empty upgrade parameters to unrelated input",
			oldRaw: `{"clusterInstanceParameters":{}}`,
			newRaw: `{"clusterInstanceParameters":{},"upgradeParameters":{}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPr := &provisioningv1alpha1.ProvisioningRequest{
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:       "template",
					TemplateVersion:    "v1",
					TemplateParameters: runtime.RawExtension{Raw: []byte(tt.oldRaw)},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{Conditions: []metav1.Condition{{
					Type:   string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
					Status: metav1.ConditionFalse,
					Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				}}},
			}
			newPr := oldPr.DeepCopy()
			newPr.Spec.TemplateParameters.Raw = []byte(tt.newRaw)
			if err := validateActiveUpgradeUpdate(oldPr, newPr, &provisioningv1alpha1.ClusterTemplate{}); err != nil {
				t.Fatalf("empty upgrade input should be equivalent to omission: %v", err)
			}
		})
	}
}

var _ = Describe("active upgrade input validation", func() {
	var oldPr, newPr *provisioningv1alpha1.ProvisioningRequest
	var clusterTemplate *provisioningv1alpha1.ClusterTemplate

	BeforeEach(func() {
		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{}
		oldPr = &provisioningv1alpha1.ProvisioningRequest{
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "clustertemplate-a",
				TemplateVersion: "v1.0.1",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ]
      }
    }
  }
}`)},
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Conditions: []metav1.Condition{{
					Type: string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted), Status: metav1.ConditionFalse,
					Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				}},
			},
		}
		newPr = oldPr.DeepCopy()
		oldPr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
			ClusterUpgradeStatus: &provisioningv1alpha1.ClusterUpgradeStatus{
				WorkerPoolUpgrade: &provisioningv1alpha1.WorkerPoolUpgradeStatus{
					Strategy: constants.WorkerPoolUpgradeStrategyCustom,
					Stages: []provisioningv1alpha1.WorkerPoolUpgradeStage{
						{Name: "canary", Pools: []string{"worker-a"}},
						{Name: "rest", Pools: []string{"worker-b"}},
					},
				},
			},
		}
	})

	for _, reason := range []provisioningv1alpha1.ConditionReason{
		provisioningv1alpha1.CRconditionReasons.InProgress,
		provisioningv1alpha1.CRconditionReasons.Unknown,
		provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization,
	} {
		Context(fmt.Sprintf("with UpgradeCompleted reason %s", reason), func() {
			BeforeEach(func() {
				oldPr.Status.Conditions[0].Reason = string(reason)
			})

			It("should reject plan changes while active", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "changed",
            "pools": [
              "worker-a"
            ]
          }
        ]
      }
    }
  }
}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot be changed"))
			})

			It("should reject switching ClusterTemplate while active", func() {
				newPr.Spec.TemplateVersion = "v2.0.0"
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("switching ClusterTemplate"))
			})

			DescribeTable("should reject changes to other upgrade parameters while active",
				func(changedParameter string) {
					newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
				"upgradeParameters": {
					"clusterVersion": {
						"clusterUpgradeTimeout": "2h",
						"workerPoolUpgrade": {
							"strategy": "Custom",
							"stages": [
								{"name": "canary", "pools": ["worker-a"]},
								{"name": "rest", "pools": ["worker-b"]}
							]
						},
					` + changedParameter + `
					}
				}
			}`)}
					err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("upgradeParameters cannot be changed"))
				},
				Entry("cvSpec", `"cvSpec":{"channel":"stable-4.22"}`),
				Entry("intermediateVersion", `"intermediateVersion":"4.21.3"`),
			)

			It("should allow a timeout update", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "3h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ]
      }
    }
  }
}`)}
				Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
			})

			It("should allow forward stage authorization only while awaiting", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "canary"
      }
    }
  }
}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				if reason == provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(MatchError(ContainSubstring(
						"clusterVersion.workerPoolUpgrade.upgradeThrough can only change while awaiting stage authorization")))
				}
			})

			It("should reject a non-string upgradeThrough", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(
					`{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": 1}}}}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(MatchError(ContainSubstring("workerPoolUpgrade.upgradeThrough must be a string")))
			})

			It("should reject a non-string default upgradeThrough", func() {
				newPr.Spec.TemplateParameters = oldPr.Spec.TemplateParameters
				clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(
					`{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": 1}}}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(MatchError(ContainSubstring("workerPoolUpgrade.upgradeThrough must be a string")))
			})

		})
	}

	Context("while awaiting Custom stage authorization", func() {
		BeforeEach(func() {
			oldPr.Status.Conditions[0].Reason = string(provisioningv1alpha1.CRconditionReasons.AwaitingStageAuthorization)
		})

		It("should reject moving upgradeThrough behind persisted authorization", func() {
			oldPr.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.
				WorkerPoolUpgrade.UpgradeThrough = "rest"
			newPr.Spec.TemplateParameters = oldPr.Spec.TemplateParameters
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough cannot move backwards")))
		})

		It("should report the nested path for an unknown stage", func() {
			var params map[string]any
			Expect(json.Unmarshal(oldPr.Spec.TemplateParameters.Raw, &params)).To(Succeed())
			pool := params["upgradeParameters"].(map[string]any)["clusterVersion"].(map[string]any)["workerPoolUpgrade"].(map[string]any)
			pool["upgradeThrough"] = "missing"
			updated, err := json.Marshal(params)
			Expect(err).ToNot(HaveOccurred())
			newPr.Spec.TemplateParameters.Raw = updated
			err = validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough refers to unknown stage \"missing\"")))
		})

		It("should reject a rapid backward edit before authorization is persisted", func() {
			oldPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "rest"
      }
    }
  }
}`)
			newPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "canary"
      }
    }
  }
}`)
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough cannot move backwards from \"rest\" to \"canary\"")))
		})

		It("should reject an explicitly empty upgradeThrough over a template default", func() {
			oldPr.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.
				WorkerPoolUpgrade.UpgradeThrough = "canary"
			clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(
				`{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "rest"}}}`)}
			newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": ""
      }
    }
  }
}`)}

			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring("cannot move backwards")))
		})
	})

	DescribeTable("should reject malformed active upgrade input",
		func(raw, message string) {
			newPr.Spec.TemplateParameters.Raw = []byte(raw)
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("invalid JSON", `{`, "failed to decode templateParameters"),
		Entry("null template parameters", `null`, "templateParameters must be an object"),
		Entry("non-object upgrade parameters", `{"upgradeParameters":null}`,
			"templateParameters.upgradeParameters must be an object"),
		Entry("non-object worker pool upgrade", `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": []}}}`,
			"templateParameters.upgradeParameters.clusterVersion.workerPoolUpgrade must be an object"),
	)

	DescribeTable("should allow plan changes outside an active upgrade",
		func(reason provisioningv1alpha1.ConditionReason) {
			oldPr.Status.Conditions[0].Reason = string(reason)
			newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Parallel"
      }
    }
  }
}`)}
			Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
		},
		Entry("Pending", provisioningv1alpha1.CRconditionReasons.Pending),
		Entry("PreconditionChecksFailed", provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
	)

	It("should allow authorizing the final stage before the upgrade starts", func() {
		oldPr.Status.Conditions[0].Reason = string(provisioningv1alpha1.CRconditionReasons.Pending)
		newPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "rest"
      }
    }
  }
}`)
		Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
	})
})
