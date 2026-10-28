/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package spokeclient

import (
	"context"
	"encoding/json"
	"fmt"
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
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
		var err error
		mw, err = BuildRBACManifestWork("test-pr-upgrade-rbac", clusterName, msa.Name, defaultAddonInstallNamespace, testRules)
		Expect(err).ToNot(HaveOccurred())
		mw.Generation = 1
		mw.Status.Conditions = []metav1.Condition{
			{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 1},
		}

		SetTestSpokeClientCreator(func(apiServerURL, token string, caCert []byte, spokeScheme *runtime.Scheme) (client.Client, error) {
			return fake.NewClientBuilder().WithScheme(testScheme).Build(), nil
		})
		SetTestSpokeClientsetCreator(func(apiServerURL, token string, caCert []byte) (kubernetes.Interface, error) {
			return kubefake.NewSimpleClientset(), nil
		})
	})

	AfterEach(func() {
		newSpokeClientFunc = buildSpokeClient
		newSpokeClientsetFunc = buildSpokeClientset
	})

	// bumpGenerationOnPatch stands in for the API server incrementing
	// metadata.generation when a patch changes the spec. The fake client does
	// no generation tracking of its own, and EnsureSpokeClient decides
	// readiness by comparing conditions against the current generation.
	bumpGenerationOnPatch := interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object,
			patch client.Patch, opts ...client.PatchOption) error {
			if err := c.Patch(ctx, obj, patch, opts...); err != nil {
				return fmt.Errorf("patch failed: %w", err)
			}
			obj.SetGeneration(obj.GetGeneration() + 1)
			if err := c.Update(ctx, obj); err != nil {
				return fmt.Errorf("generation bump failed: %w", err)
			}
			return nil
		},
	}

	// cacheSpokeClientForTest seeds the cache with a client built from a token
	// at the given resourceVersion.
	cacheSpokeClientForTest := func(tokenResourceVersion string) {
		spokeClientsMu.Lock()
		defer spokeClientsMu.Unlock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			clients:              Clients{Client: fake.NewClientBuilder().WithScheme(testScheme).Build()},
			tokenResourceVersion: tokenResourceVersion,
		}
	}

	It("should return InputError when managed-serviceaccount addon is not found", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ns).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("managed-serviceaccount addon is not available"))
	})

	// The addon agent mints and rotates the MSA token, so a cached client only
	// outlives it until the current token expires. The dependency is reported
	// rather than masked, even though it costs the cached fast path.
	It("should return InputError when the addon is unavailable even with a cached client", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue(), "the cached entry must be seeded before the addon goes away")

		Expect(c.Delete(ctx, addon)).To(Succeed())
		_, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("managed-serviceaccount addon is not available"))
	})

	It("should surface non-NotFound addon errors as retryable errors", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*addonv1alpha1.ManagedClusterAddOn); ok {
						return fmt.Errorf("test addon read failure")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).To(MatchError(ContainSubstring("test addon read failure")))
		Expect(typederrors.IsInputError(err)).To(BeFalse())
		Expect(ready).To(BeFalse())
	})

	It("should return not ready when token is not yet synced", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ns, addon).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("should return not ready when ManifestWork exists but resources are not yet available", func() {
		mw.Status.Conditions = nil

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	DescribeTable("waits for successful RBAC application even when resources are available", func(status metav1.ConditionStatus) {
		mw.Status.Conditions[0].Status = status
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()
		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
	}, Entry("Applied=False", metav1.ConditionFalse), Entry("Applied=Unknown", metav1.ConditionUnknown))

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
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
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
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), WithClientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		// Simulate ManifestWork controller reporting the current work applied and available.
		mw := &workv1.ManifestWork{}
		Expect(c.Get(ctx, types.NamespacedName{
			Name: "test-pr-upgrade-rbac", Namespace: clusterName,
		}, mw)).To(Succeed())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(2))
		mw.Status.Conditions = []metav1.Condition{
			{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, ObservedGeneration: mw.Generation},
			{
				Type:               workv1.WorkAvailable,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: mw.Generation,
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(c.Status().Update(ctx, mw)).To(Succeed())

		// Second call: MW Available → ready.
		clients, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), WithClientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(clients.Client).ToNot(BeNil())
		Expect(clients.Clientset).ToNot(BeNil())

		// Verify both clients are cached.
		spokeClientsMu.RLock()
		entry, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeTrue())
		Expect(entry.tokenResourceVersion).To(Equal("100"))
		Expect(entry.clients.Clientset).ToNot(BeNil())
	})

	It("should not build a clientset when called with RuntimeClientOnly", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		clients, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(clients.Client).ToNot(BeNil())
		Expect(clients.Clientset).To(BeNil())

		spokeClientsMu.RLock()
		entry := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(entry.clients.Clientset).To(BeNil())
	})

	It("should return InputError when token secret is missing the 'token' key", func() {
		tokenSecret.Data = map[string][]byte{"ca.crt": []byte("test-ca")}

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
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
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
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
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).To(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("missing the 'token' key"))
	})

	DescribeTable("reconciles changed RBAC and waits for current-generation status", func(cacheClient bool) {
		mw.Labels = map[string]string{"preserved": "label"}
		mw.Annotations = map[string]string{"preserved": "annotation"}
		mw.Spec.DeleteOption = &workv1.DeleteOption{PropagationPolicy: workv1.DeletePropagationPolicyTypeOrphan}
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithStatusSubresource(&workv1.ManifestWork{}).
			WithInterceptorFuncs(bumpGenerationOnPatch).Build()

		var original Clients
		if cacheClient {
			var ready bool
			var err error
			original, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
				msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		}

		// Replace the old rules, including removing permissions no longer requested.
		newRules := []rbacv1.PolicyRule{{
			APIGroups: []string{"machineconfiguration.openshift.io"},
			Resources: []string{"machineconfigpools"}, Verbs: []string{"get"},
		}}
		clients, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, newRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(clients.Client).To(BeNil())
		Expect(c.Get(ctx, client.ObjectKeyFromObject(mw), mw)).To(Succeed())
		role := &rbacv1.ClusterRole{}
		Expect(json.Unmarshal(mw.Spec.Workload.Manifests[0].Raw, role)).To(Succeed())
		Expect(role.Rules).To(Equal(newRules))
		Expect(mw.Labels).To(HaveKeyWithValue("preserved", "label"))
		Expect(mw.Annotations).To(HaveKeyWithValue("preserved", "annotation"))
		Expect(mw.Spec.DeleteOption.PropagationPolicy).To(Equal(workv1.DeletePropagationPolicyTypeOrphan))

		// The patch bumped the generation, so the conditions the work agent
		// reported for the previous one no longer count.
		Expect(mw.Generation).To(BeNumerically(">", 1))
		_, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, newRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		// Applied for this generation is insufficient while Available is stale.
		mw.Status.Conditions[0].ObservedGeneration = mw.Generation
		Expect(c.Status().Update(ctx, mw)).To(Succeed())
		_, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, newRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		mw.Status.Conditions[1].ObservedGeneration = mw.Generation
		Expect(c.Status().Update(ctx, mw)).To(Succeed())
		clients, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, newRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(clients.Client).ToNot(BeNil())
		if cacheClient {
			Expect(clients.Client).To(Equal(original.Client))
		}
	}, Entry("after an operator restart", false), Entry("with a cached client", true))

	It("does not patch unchanged RBAC even when manifest JSON is formatted differently", func() {
		for i := range mw.Spec.Workload.Manifests {
			var manifest map[string]any
			Expect(json.Unmarshal(mw.Spec.Workload.Manifests[i].Raw, &manifest)).To(Succeed())
			raw, err := json.MarshalIndent(manifest, "", "  ")
			Expect(err).ToNot(HaveOccurred())
			mw.Spec.Workload.Manifests[i].Raw = raw
		}
		patchCount := 0
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patchCount++
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
		for range 2 {
			_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
				msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		}
		Expect(patchCount).To(BeZero())
	})

	It("updates the binding when the addon service account namespace changes", func() {
		addon.Status.Namespace = "new-addon-namespace"
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithInterceptorFuncs(bumpGenerationOnPatch).Build()
		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse(), "the rewritten binding is not yet reported applied")
		Expect(c.Get(ctx, client.ObjectKeyFromObject(mw), mw)).To(Succeed())
		binding := &rbacv1.ClusterRoleBinding{}
		Expect(json.Unmarshal(mw.Spec.Workload.Manifests[1].Raw, binding)).To(Succeed())
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind: "ServiceAccount", Name: msa.Name, Namespace: addon.Status.Namespace,
		}))
	})

	It("recreates a missing ManifestWork even when the client is cached", func() {
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).Build()
		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(c.Delete(ctx, mw)).To(Succeed())
		_, ready, err = EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(c.Get(ctx, client.ObjectKeyFromObject(mw), mw)).To(Succeed())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(2))
	})

	It("does not patch or use a deleting RBAC ManifestWork", func() {
		mw.Spec.Workload.Manifests = nil // Would otherwise require a patch.
		now := metav1.Now()
		mw.SetDeletionTimestamp(&now)
		mw.SetFinalizers([]string{"test-finalizer"})
		patchCount := 0
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patchCount++
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
		clients, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())
		Expect(clients.Client).To(BeNil())
		Expect(patchCount).To(BeZero())
	})

	It("returns ManifestWork patch failures for retry", func() {
		mw.Spec.Workload.Manifests = nil
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, tokenSecret, mc, msa, mw).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					return fmt.Errorf("test patch failure")
				},
			}).Build()
		clients, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			msa.Name, mw.Name, testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).To(MatchError(ContainSubstring("failed to reconcile RBAC ManifestWork: test patch failure")))
		Expect(ready).To(BeFalse())
		Expect(clients.Client).To(BeNil())
	})

	It("should return cached client when token resourceVersion is unchanged", func() {
		fakeSpokeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
		fakeSpokeClientset := kubefake.NewSimpleClientset()
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			clients:              Clients{Client: fakeSpokeClient, Clientset: fakeSpokeClientset},
			tokenResourceVersion: "100",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, msa, tokenSecret, mw).Build()

		result, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), WithClientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(result.Client).To(Equal(fakeSpokeClient))
		Expect(result.Clientset).To(Equal(fakeSpokeClientset))
	})

	It("should rebuild client when token resourceVersion changes", func() {
		oldClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
		oldClientset := kubefake.NewSimpleClientset()
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			clients:              Clients{Client: oldClient, Clientset: oldClientset},
			tokenResourceVersion: "99",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, msa, tokenSecret, mc, mw).Build()

		result, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), WithClientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(result.Client).ToNot(Equal(oldClient))
		Expect(result.Clientset).ToNot(BeNil())
		Expect(result.Clientset).ToNot(Equal(oldClientset))

		spokeClientsMu.RLock()
		entry := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(entry.tokenResourceVersion).To(Equal("100"))
		Expect(entry.clients.Clientset).ToNot(Equal(oldClientset))
	})

	It("should clear cache and re-setup when the token secret is missing", func() {
		cacheSpokeClientForTest("99")

		// The MSA still references a token secret that no longer exists.
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).To(MatchError(ContainSubstring("failed to read token secret")))
		Expect(ready).To(BeFalse())

		spokeClientsMu.RLock()
		_, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeFalse())
	})

	It("should clear cache and wait when the MSA token is no longer synced", func() {
		cacheSpokeClientForTest("99")

		msa.Status.TokenSecretRef = nil
		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, mc, msa, mw).Build()

		_, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), RuntimeClientOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeFalse())

		spokeClientsMu.RLock()
		_, ok := spokeClients["test-pr-upgrade"]
		spokeClientsMu.RUnlock()
		Expect(ok).To(BeFalse())
	})

	It("should rebuild a cached runtime-only client when a clientset is requested", func() {
		cachedClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
		spokeClientsMu.Lock()
		spokeClients["test-pr-upgrade"] = &spokeClientEntry{
			clients:              Clients{Client: cachedClient},
			tokenResourceVersion: "100",
		}
		spokeClientsMu.Unlock()

		c := fake.NewClientBuilder().WithScheme(testScheme).
			WithObjects(ns, addon, mc, msa, tokenSecret, mw).Build()

		result, ready, err := EnsureSpokeClient(ctx, c, testLogger, clusterName,
			"test-pr-upgrade", "test-pr-upgrade-rbac", testRules, NewSpokeScheme(), WithClientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(BeTrue())
		Expect(result.Client).ToNot(Equal(cachedClient))
		Expect(result.Clientset).ToNot(BeNil())
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
			clients:              Clients{Client: fake.NewClientBuilder().WithScheme(testScheme).Build()},
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
