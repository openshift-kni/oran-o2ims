/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package spokeclient

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

var testScheme = fakeclient.Scheme

func TestSpokeClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SpokeClient")
}

var testLogger = slog.New(slog.DiscardHandler)

var testRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{"config.openshift.io"},
		Resources: []string{"clusterversions"},
		Verbs:     []string{"get", "list", "watch", "update", "patch"},
	},
}

var _ = Describe("EnsureSpokeClient", func() {
	var (
		ctx         context.Context
		clusterName string
		ns          *corev1.Namespace
		mc          *clusterv1.ManagedCluster
		addon       *addonv1alpha1.ManagedClusterAddOn
		tokenSecret *corev1.Secret
		msa         *msav1beta1.ManagedServiceAccount
		mw          *workv1.ManifestWork
	)

	BeforeEach(func() {
		ctx = context.Background()
		clusterName = "test-cluster"
		ClearCache()

		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}
		mc = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
			Spec: clusterv1.ManagedClusterSpec{
				ManagedClusterClientConfigs: []clusterv1.ClientConfig{
					{URL: "https://api.test-cluster.example.com:6443"},
				},
			},
		}
		addon = &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedServiceAccountAddonName, Namespace: clusterName,
			},
		}
		tokenSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr-upgrade-token", Namespace: clusterName,
				ResourceVersion: "100",
			},
			Data: map[string][]byte{
				"token":  []byte("test-token"),
				"ca.crt": []byte("test-ca"),
			},
		}
		msa = &msav1beta1.ManagedServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr-upgrade", Namespace: clusterName,
			},
			Status: msav1beta1.ManagedServiceAccountStatus{
				TokenSecretRef: &msav1beta1.SecretRef{Name: "test-pr-upgrade-token"},
			},
		}
		mw = &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pr-upgrade-rbac", Namespace: clusterName,
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{{
					Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(),
				}},
			},
		}

		SetTestSpokeClientCreator(func(apiServerURL, token string, caCert []byte, spokeScheme *runtime.Scheme) (client.Client, error) {
			return fake.NewClientBuilder().WithScheme(testScheme).Build(), nil
		})
	})

	AfterEach(func() {
		newSpokeClientFunc = buildSpokeClient
	})

	It("should return InputError when managed-serviceaccount addon is not found", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ns).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("managed-serviceaccount addon is not available"))
	})

	It("should return not ready when token is not yet synced", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ns, addon).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("should return not ready when ManifestWork exists but resources are not yet available", func() {
		mw.Status.Conditions = nil

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("should use addon status.namespace for ClusterRoleBinding subject namespace", func() {
		customNamespace := "custom-addon-ns"
		addon.Status.Namespace = customNamespace

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc).
			WithStatusSubresource(&msav1beta1.ManagedServiceAccount{}, &workv1.ManifestWork{}).
			Build()

		Expect(c.Create(ctx, msa)).To(Succeed())
		Expect(c.Status().Update(ctx, msa)).To(Succeed())

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		mw := &workv1.ManifestWork{}
		Expect(c.Get(ctx, types.NamespacedName{
			Name: "test-pr-upgrade-rbac", Namespace: clusterName,
		}, mw)).To(Succeed())

		var crb rbacv1.ClusterRoleBinding
		Expect(json.Unmarshal(mw.Spec.Workload.Manifests[1].RawExtension.Raw, &crb)).To(Succeed())
		Expect(crb.Subjects).To(HaveLen(1))
		Expect(crb.Subjects[0].Namespace).To(Equal(customNamespace))
	})

	It("should create MSA, ManifestWork, and return spoke client on happy path", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc).
			WithStatusSubresource(&msav1beta1.ManagedServiceAccount{}, &workv1.ManifestWork{}).
			Build()

		Expect(c.Create(ctx, msa)).To(Succeed())
		Expect(c.Status().Update(ctx, msa)).To(Succeed())

		// First call: MW created but not yet Available → not ready.
		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		// Simulate ManifestWork controller setting Available=True.
		mw := &workv1.ManifestWork{}
		Expect(c.Get(ctx, types.NamespacedName{
			Name: "test-pr-upgrade-rbac", Namespace: clusterName,
		}, mw)).To(Succeed())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(2))
		mw.Status.Conditions = []metav1.Condition{
			{
				Type:               workv1.WorkAvailable,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(c.Status().Update(ctx, mw)).To(Succeed())

		// Second call: MW Available → ready.
		spokeClient, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(spokeClient).ToNot(BeNil())

		// Verify client is cached.
		spokeClientsMu.RLock()
		entry, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeTrue())
		Expect(entry.tokenResourceVersion).To(Equal("100"))
	})

	It("should return InputError when token secret is missing the 'token' key", func() {
		tokenSecret.Data = map[string][]byte{"ca.crt": []byte("test-ca")}

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("missing the 'token' key"))
	})

	It("should return InputError when token secret is missing the 'ca.crt' key", func() {
		tokenSecret.Data = map[string][]byte{"token": []byte("test-token")}

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("missing the 'ca.crt' key"))
	})

	It("should return InputError when token secret has empty token value", func() {
		tokenSecret.Data = map[string][]byte{"token": []byte(""), "ca.crt": []byte("test-ca")}

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("missing the 'token' key"))
	})

	It("should return cached client when token resourceVersion is unchanged", func() {
		fakeSpokeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			client:               fakeSpokeClient,
			tokenResourceVersion: "100",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, msa, tokenSecret).Build()

		result, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(result).To(Equal(fakeSpokeClient))
	})

	It("should rebuild client when token resourceVersion changes", func() {
		oldClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			client:               oldClient,
			tokenResourceVersion: "99",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, msa, tokenSecret, mc).Build()

		result, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(result).ToNot(Equal(oldClient))

		spokeClientsMu.RLock()
		entry := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(entry.tokenResourceVersion).To(Equal("100"))
	})

	It("should clear cache and re-setup when token secret is missing", func() {
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			client:               fake.NewClientBuilder().WithScheme(testScheme).Build(),
			tokenResourceVersion: "99",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ns).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme())
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())

		spokeClientsMu.RLock()
		_, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("CleanupSpokeAccess", func() {
	It("should delete MSA, ManifestWork, and clear cache", func() {
		ctx := context.Background()
		clusterName := "test-cluster"

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName},
				},
				&msav1beta1.ManagedServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pr-upgrade", Namespace: clusterName},
				},
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pr-upgrade-rbac", Namespace: clusterName},
				},
			).Build()

		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			client:               fake.NewClientBuilder().WithScheme(testScheme).Build(),
			tokenResourceVersion: "100",
		}
		spokeClientsMu.Unlock()

		err := CleanupSpokeAccess(ctx, c, clusterName, "test-pr-upgrade", "test-pr-upgrade-rbac")
		Expect(err).ToNot(HaveOccurred())

		err = c.Get(ctx, types.NamespacedName{
			Name: "test-pr-upgrade", Namespace: clusterName,
		}, &msav1beta1.ManagedServiceAccount{})
		Expect(errors.IsNotFound(err)).To(BeTrue())

		err = c.Get(ctx, types.NamespacedName{
			Name: "test-pr-upgrade-rbac", Namespace: clusterName,
		}, &workv1.ManifestWork{})
		Expect(errors.IsNotFound(err)).To(BeTrue())

		spokeClientsMu.RLock()
		_, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeFalse())
	})

	It("should handle already-deleted resources gracefully", func() {
		ctx := context.Background()
		clusterName := "test-cluster"

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}).
			Build()

		err := CleanupSpokeAccess(ctx, c, clusterName, "test-pr-upgrade", "test-pr-upgrade-rbac")
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("BuildRBACManifestWork", func() {
	It("should create ManifestWork with correct ClusterRole and ClusterRoleBinding", func() {
		mw, err := BuildRBACManifestWork("test-pr-upgrade-rbac", "test-cluster", "test-sa", "test-ns", testRules)
		Expect(err).ToNot(HaveOccurred())

		Expect(mw.Name).To(Equal("test-pr-upgrade-rbac"))
		Expect(mw.Namespace).To(Equal("test-cluster"))
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(2))

		var cr rbacv1.ClusterRole
		Expect(json.Unmarshal(mw.Spec.Workload.Manifests[0].RawExtension.Raw, &cr)).To(Succeed())
		Expect(cr.Name).To(Equal("test-pr-upgrade-rbac-role"))
		Expect(cr.Rules).To(HaveLen(1))
		Expect(cr.Rules[0].APIGroups).To(Equal([]string{"config.openshift.io"}))
		Expect(cr.Rules[0].Resources).To(Equal([]string{"clusterversions"}))
		Expect(cr.Rules[0].Verbs).To(ContainElements("get", "update", "patch"))

		var crb rbacv1.ClusterRoleBinding
		Expect(json.Unmarshal(mw.Spec.Workload.Manifests[1].RawExtension.Raw, &crb)).To(Succeed())
		Expect(crb.Name).To(Equal("test-pr-upgrade-rbac-binding"))
		Expect(crb.RoleRef.Name).To(Equal("test-pr-upgrade-rbac-role"))
		Expect(crb.Subjects).To(HaveLen(1))
		Expect(crb.Subjects[0].Name).To(Equal("test-sa"))
		Expect(crb.Subjects[0].Namespace).To(Equal("test-ns"))
	})
})
