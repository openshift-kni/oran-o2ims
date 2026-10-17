/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

var _ = Describe("CheckHardwareProfileReferences", func() {
	const (
		hpName      = "profile-a"
		operatorNS  = constants.DefaultNamespace
		otherNSName = "other-ns"
	)

	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	// clusterTemplate builds a ClusterTemplate whose single nodeGroup references
	// the given hardware profile name.
	clusterTemplate := func(name, ns, hwProfile string) *provisioningv1alpha1.ClusterTemplate {
		return &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
							{Name: "master", Role: "master", HwProfile: hwProfile},
						},
					},
				},
			},
		}
	}

	// provisioningRequest builds a cluster-scoped ProvisioningRequest whose
	// templateParameters.hwMgmtParameters.nodeGroupData references the profile.
	provisioningRequest := func(name, hwProfile string) *provisioningv1alpha1.ProvisioningRequest {
		params := map[string]any{
			constants.TemplateParamHwMgmt: map[string]any{
				HwMgmtNodeGroupDataKey: []any{
					map[string]any{"name": "master", "hwProfile": hwProfile},
				},
			},
		}
		raw, err := json.Marshal(params)
		Expect(err).ToNot(HaveOccurred())
		return &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{Raw: raw},
			},
		}
	}

	// provisioningRequestRaw builds a ProvisioningRequest with arbitrary raw
	// templateParameters bytes (used for nil/malformed cases).
	provisioningRequestRaw := func(name string, raw []byte) *provisioningv1alpha1.ProvisioningRequest {
		return &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{Raw: raw},
			},
		}
	}

	newReader := func(objs ...client.Object) client.Reader {
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	}

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	It("allows deletion when nothing references the profile", func() {
		reader := newReader(
			clusterTemplate("ct-a.v1", operatorNS, "different-profile"),
			provisioningRequest("pr-a", "different-profile"),
		)
		Expect(CheckHardwareProfileReferences(ctx, reader, hpName, operatorNS)).To(Succeed())
	})

	It("rejects deletion when a ClusterTemplate references the profile", func() {
		reader := newReader(clusterTemplate("ct-a.v1", otherNSName, hpName))
		err := CheckHardwareProfileReferences(ctx, reader, hpName, operatorNS)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("referenced by ClusterTemplate"))
		Expect(err.Error()).To(ContainSubstring("ct-a.v1"))
	})

	It("rejects deletion when a ProvisioningRequest references the profile", func() {
		reader := newReader(provisioningRequest("pr-a", hpName))
		err := CheckHardwareProfileReferences(ctx, reader, hpName, operatorNS)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("referenced by ProvisioningRequest"))
		Expect(err.Error()).To(ContainSubstring("pr-a"))
	})

	It("gracefully ignores ProvisioningRequests with nil or unexpectedly shaped templateParameters", func() {
		reader := newReader(
			provisioningRequestRaw("pr-nil", nil),
			provisioningRequestRaw("pr-empty", []byte(`{}`)),
			provisioningRequestRaw("pr-no-hwmgmt", []byte(`{"clusterInstanceParameters":{}}`)),
			provisioningRequestRaw("pr-wrong-shape", []byte(`{"hwMgmtParameters":{"nodeGroupData":"not-a-list"}}`)),
			provisioningRequestRaw("pr-ng-not-object", []byte(`{"hwMgmtParameters":{"nodeGroupData":["not-a-map"]}}`)),
		)
		Expect(CheckHardwareProfileReferences(ctx, reader, hpName, operatorNS)).To(Succeed())
	})

	It("allows deletion of a profile outside the operator namespace even if the name matches", func() {
		reader := newReader(
			clusterTemplate("ct-a.v1", operatorNS, hpName),
			provisioningRequest("pr-a", hpName),
		)
		// The profile being deleted lives outside the operator namespace, so it
		// can never be the one these references resolve to.
		Expect(CheckHardwareProfileReferences(ctx, reader, hpName, otherNSName)).To(Succeed())
	})
})
