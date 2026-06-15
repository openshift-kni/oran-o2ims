/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

func TestValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Validation")
}

var _ = Describe("IsReservedNamespace", func() {
	It("rejects kube-system", func() {
		reserved, _ := IsReservedNamespace("kube-system")
		Expect(reserved).To(BeTrue())
	})

	It("rejects kube-public", func() {
		reserved, _ := IsReservedNamespace("kube-public")
		Expect(reserved).To(BeTrue())
	})

	It("rejects default", func() {
		reserved, _ := IsReservedNamespace("default")
		Expect(reserved).To(BeTrue())
	})

	It("rejects openshift exactly", func() {
		reserved, _ := IsReservedNamespace("openshift")
		Expect(reserved).To(BeTrue())
	})

	It("rejects openshift-monitoring", func() {
		reserved, _ := IsReservedNamespace("openshift-monitoring")
		Expect(reserved).To(BeTrue())
	})

	It("rejects open-cluster-management", func() {
		reserved, _ := IsReservedNamespace("open-cluster-management")
		Expect(reserved).To(BeTrue())
	})

	It("rejects open-cluster-management-agent", func() {
		reserved, _ := IsReservedNamespace("open-cluster-management-agent")
		Expect(reserved).To(BeTrue())
	})

	It("rejects multicluster-engine", func() {
		reserved, _ := IsReservedNamespace("multicluster-engine")
		Expect(reserved).To(BeTrue())
	})

	It("rejects ztp-something", func() {
		reserved, _ := IsReservedNamespace("ztp-my-templates")
		Expect(reserved).To(BeTrue())
	})

	It("rejects the operator namespace", func() {
		os.Setenv(constants.DefaultNamespaceEnvName, "custom-operator-ns")
		defer os.Unsetenv(constants.DefaultNamespaceEnvName)
		reserved, _ := IsReservedNamespace("custom-operator-ns")
		Expect(reserved).To(BeTrue())
	})

	It("rejects default operator namespace when env not set", func() {
		os.Unsetenv(constants.DefaultNamespaceEnvName)
		reserved, _ := IsReservedNamespace(constants.DefaultNamespace)
		Expect(reserved).To(BeTrue())
	})

	It("accepts a valid cluster name", func() {
		reserved, _ := IsReservedNamespace("my-cluster-01")
		Expect(reserved).To(BeFalse())
	})

	It("accepts default-team (not an exact match for default)", func() {
		reserved, _ := IsReservedNamespace("default-team")
		Expect(reserved).To(BeFalse())
	})

	It("accepts openshift-like names that don't start with the prefix", func() {
		reserved, _ := IsReservedNamespace("my-openshift-cluster")
		Expect(reserved).To(BeFalse())
	})

	It("rejects openshift-anything (prefix match)", func() {
		reserved, _ := IsReservedNamespace("openshift-custom")
		Expect(reserved).To(BeTrue())
	})

	It("rejects kube-anything (prefix match)", func() {
		reserved, _ := IsReservedNamespace("kube-custom")
		Expect(reserved).To(BeTrue())
	})

	It("accepts defaulting (not exact match for default)", func() {
		reserved, _ := IsReservedNamespace("defaulting")
		Expect(reserved).To(BeFalse())
	})

	It("accepts a name that contains but doesn't start with reserved prefix", func() {
		reserved, _ := IsReservedNamespace("cluster-openshift-test")
		Expect(reserved).To(BeFalse())
	})
})

var _ = Describe("ValidateClusterNameFormat", func() {
	It("rejects empty string", func() {
		Expect(ValidateClusterNameFormat("")).To(HaveOccurred())
	})

	It("rejects uppercase letters", func() {
		Expect(ValidateClusterNameFormat("MyCluster")).To(HaveOccurred())
	})

	It("rejects names starting with hyphen", func() {
		Expect(ValidateClusterNameFormat("-cluster")).To(HaveOccurred())
	})

	It("rejects names ending with hyphen", func() {
		Expect(ValidateClusterNameFormat("cluster-")).To(HaveOccurred())
	})

	It("rejects names with underscores", func() {
		Expect(ValidateClusterNameFormat("my_cluster")).To(HaveOccurred())
	})

	It("rejects names longer than 63 characters", func() {
		longName := "a234567890123456789012345678901234567890123456789012345678901234"
		Expect(len(longName)).To(BeNumerically(">", 63))
		Expect(ValidateClusterNameFormat(longName)).To(HaveOccurred())
	})

	It("accepts valid lowercase name", func() {
		Expect(ValidateClusterNameFormat("my-cluster-01")).ToNot(HaveOccurred())
	})

	It("accepts single character", func() {
		Expect(ValidateClusterNameFormat("a")).ToNot(HaveOccurred())
	})
})

var _ = Describe("ValidateClusterNameOwnership", func() {
	const ownerLabel = "provisioningrequest.clcm.openshift.io/name"

	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	It("allows when namespace does not exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		err := ValidateClusterNameOwnership(ctx, c, "new-cluster", "my-pr", ownerLabel)
		Expect(err).ToNot(HaveOccurred())
	})

	It("allows when namespace is owned by this PR", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-cluster",
				Labels: map[string]string{
					ownerLabel: "my-pr",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
		err := ValidateClusterNameOwnership(ctx, c, "my-cluster", "my-pr", ownerLabel)
		Expect(err).ToNot(HaveOccurred())
	})

	It("rejects when namespace is owned by a different PR", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-cluster",
				Labels: map[string]string{
					ownerLabel: "other-pr",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
		err := ValidateClusterNameOwnership(ctx, c, "my-cluster", "my-pr", ownerLabel)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not owned by ProvisioningRequest"))
	})

	It("rejects when namespace exists without PR label", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-ns",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
		err := ValidateClusterNameOwnership(ctx, c, "existing-ns", "my-pr", ownerLabel)
		Expect(err).To(HaveOccurred())
	})
})
