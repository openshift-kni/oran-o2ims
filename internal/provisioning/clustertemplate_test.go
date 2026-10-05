/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var s = scheme.Scheme

func TestProvisioningSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning Suite")
}

var _ = BeforeSuite(func() {
	s.AddKnownTypes(provisioningv1alpha1.GroupVersion,
		&provisioningv1alpha1.ProvisioningRequest{},
		&provisioningv1alpha1.ClusterTemplate{},
		&provisioningv1alpha1.ClusterTemplateList{})
})

var _ = Describe("GetClusterTemplateRef", func() {
	var (
		ctx          context.Context
		fakeClient   client.Client
		pr           *provisioningv1alpha1.ProvisioningRequest
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the provisioning request.
		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
				Finalizers: []string{},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
			},
		}

		fakeClient = fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(pr).Build()
	})

	It("returns error if the referred ClusterTemplate is missing", func() {
		// Define the cluster template.
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-template-name.v1.0.0",
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       "other-cluster-template-name",
				Version:    "v1.0.0",
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{},
			},
		}

		Expect(fakeClient.Create(ctx, ct)).To(Succeed())

		retCt, err := GetClusterTemplateRef(context.TODO(), fakeClient, pr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf(
				"a valid ClusterTemplate (%s) does not exist in any namespace",
				fmt.Sprintf("%s.%s", tName, tVersion))))
		Expect(retCt).To(Equal((*provisioningv1alpha1.ClusterTemplate)(nil)))
	})

	It("returns the referred ClusterTemplate if it exists", func() {
		// Define the cluster template.
		ctName := fmt.Sprintf("%s.%s", tName, tVersion)
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Reason: "Completed",
						Type:   "ClusterTemplateValidated",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		Expect(fakeClient.Create(ctx, ct)).To(Succeed())

		retCt, err := GetClusterTemplateRef(context.TODO(), fakeClient, pr)
		Expect(err).ToNot(HaveOccurred())
		Expect(retCt.Name).To(Equal(ctName))
		Expect(retCt.Namespace).To(Equal(ctNamespace))
		Expect(retCt.Spec.TemplateDefaults.ClusterInstanceDefaults).To(Equal(ciDefaultsCm))
		Expect(retCt.Spec.TemplateDefaults.PolicyTemplateDefaults).To(Equal(ptDefaultsCm))
	})
})
