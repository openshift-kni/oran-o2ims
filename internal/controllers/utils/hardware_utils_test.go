/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("CopyPullSecret", func() {
	It("copies the secret to every allocated node namespace in one call", func() {
		ctx := context.Background()
		secretData := map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}
		owner := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "request", UID: types.UID("request-uid")},
		}
		fakeClient := fakeclient.GetFakeClientFromObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "template"},
				Data:       secretData,
				Type:       corev1.SecretTypeDockerConfigJson,
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "pool-b"},
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"old":true}`)},
				Type:       corev1.SecretTypeDockerConfigJson,
			},
		)
		hwNodes := map[string][]NodeInfo{
			"group-a": {
				{HwMgrNodeNs: "pool-a"},
				{HwMgrNodeNs: "pool-a"},
			},
			"group-b": {
				{HwMgrNodeNs: "pool-b"},
				{HwMgrNodeNs: "pool-c"},
			},
		}

		Expect(CopyPullSecret(ctx, slog.Default(), fakeClient, owner, "template", "pull-secret", hwNodes)).To(Succeed())

		for _, namespace := range []string{"pool-a", "pool-b", "pool-c"} {
			copied := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "pull-secret", Namespace: namespace}, copied)).To(Succeed())
			Expect(copied.Data).To(Equal(secretData))
			Expect(copied.Type).To(Equal(corev1.SecretTypeDockerConfigJson))
			Expect(copied.OwnerReferences).To(HaveLen(1))
			Expect(copied.OwnerReferences[0].UID).To(Equal(owner.UID))
		}
	})
})

var _ = Describe("MapHardwareReasonToProvisioningReason", func() {
	Context("when mapping standard hardware management reasons", func() {
		It("should correctly map Failed reason", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.Failed))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Failed))
		})

		It("should correctly map TimedOut reason", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.TimedOut))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.TimedOut))
		})

		It("should correctly map InProgress reason", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.InProgress))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.InProgress))
		})

		It("should correctly map Completed reason", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.Completed))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Completed))
		})
	})

	Context("when mapping hardware-specific failure reasons to provisioning Failed", func() {
		It("should map InvalidInput to Failed", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.InvalidInput))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Failed))
		})

		It("should map Unprovisioned to Failed", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.Unprovisioned))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Failed))
		})

		It("should map NotInitialized to Failed", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.NotInitialized))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Failed))
		})
	})

	Context("when mapping configuration-specific reasons", func() {
		It("should map ConfigUpdate to InProgress", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.ConfigUpdate))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.InProgress))
		})

		It("should map ConfigApplied to Completed", func() {
			result := MapHardwareReasonToProvisioningReason(string(hwmgmtv1alpha1.ConfigApplied))
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Completed))
		})
	})

	Context("when mapping unknown or invalid reasons", func() {
		It("should map unknown reason to Unknown", func() {
			result := MapHardwareReasonToProvisioningReason("UnknownReason")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})

		It("should map empty string to Unknown", func() {
			result := MapHardwareReasonToProvisioningReason("")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})

		It("should map arbitrary string to Unknown", func() {
			result := MapHardwareReasonToProvisioningReason("SomeRandomString")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})

		It("should map special characters to Unknown", func() {
			result := MapHardwareReasonToProvisioningReason("@#$%^&*()")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})
	})

	Context("when testing all known hardware management reasons comprehensively", func() {
		type reasonMapping struct {
			hardwareReason     string
			expectedProvReason provisioningv1alpha1.ConditionReason
			description        string
		}

		DescribeTable("should map hardware reasons correctly",
			func(mapping reasonMapping) {
				result := MapHardwareReasonToProvisioningReason(mapping.hardwareReason)
				Expect(result).To(Equal(mapping.expectedProvReason), mapping.description)
			},
			Entry("Failed maps to Failed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.Failed),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Failed,
				description:        "Hardware failure should map to provisioning failure",
			}),
			Entry("TimedOut maps to TimedOut", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.TimedOut),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.TimedOut,
				description:        "Hardware timeout should map to provisioning timeout",
			}),
			Entry("InProgress maps to InProgress", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.InProgress),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.InProgress,
				description:        "Hardware in progress should map to provisioning in progress",
			}),
			Entry("Completed maps to Completed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.Completed),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Completed,
				description:        "Hardware completion should map to provisioning completion",
			}),
			Entry("InvalidInput maps to Failed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.InvalidInput),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Failed,
				description:        "Invalid hardware input should be treated as provisioning failure",
			}),
			Entry("Unprovisioned maps to Failed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.Unprovisioned),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Failed,
				description:        "Unexpected unprovisioned state should be treated as failure",
			}),
			Entry("NotInitialized maps to Failed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.NotInitialized),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Failed,
				description:        "Hardware initialization failure should be treated as failure",
			}),
			Entry("ConfigUpdate maps to InProgress", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.ConfigUpdate),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.InProgress,
				description:        "Configuration update request should be treated as in progress",
			}),
			Entry("ConfigApplied maps to Completed", reasonMapping{
				hardwareReason:     string(hwmgmtv1alpha1.ConfigApplied),
				expectedProvReason: provisioningv1alpha1.CRconditionReasons.Completed,
				description:        "Configuration applied successfully should be treated as completed",
			}),
		)
	})

	Context("when testing case sensitivity", func() {
		It("should be case sensitive for known reasons", func() {
			// Test that the mapping is case-sensitive - uppercase should not match
			result := MapHardwareReasonToProvisioningReason("FAILED")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})

		It("should be case sensitive for mixed case", func() {
			result := MapHardwareReasonToProvisioningReason("Failed")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Failed))

			result = MapHardwareReasonToProvisioningReason("failed")
			Expect(result).To(Equal(provisioningv1alpha1.CRconditionReasons.Unknown))
		})
	})
})
