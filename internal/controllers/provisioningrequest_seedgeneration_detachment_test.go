/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func seedDetachmentTestRequest() *provisioningv1alpha1.ProvisioningRequest {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr", UID: "seed-pr-uid"}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "spoke-a", SeedGenerationStatus: seedLifecycleTestStatus()}
	return pr
}

func TestSeedGenerationDetachmentOrdersCleanup(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pr := seedDetachmentTestRequest()
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"}}
	addon := &addonv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
		Name: "observability-controller", Namespace: "spoke-a", UID: "addon-uid", Finalizers: []string{"addon-hook"},
	}}
	work := &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{
		Name: "spoke-a-klusterlet", Namespace: "spoke-a", Generation: 1,
		Labels: map[string]string{seedGenerationKlusterletWorksLabel: "true"},
	}, Spec: workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{
		Raw: []byte(`{"apiVersion":"operator.open-cluster-management.io/v1","kind":"Klusterlet","metadata":{"name":"klusterlet"}}`),
	}}}}}}
	hub := newSeedTestClient(t, pr, cluster, addon, work)
	klusterlet := newSeedGenerationKlusterlet()
	klusterlet.SetUID(types.UID("klusterlet-uid"))
	klusterlet.SetFinalizers([]string{"klusterlet-cleanup"})
	crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: seedGenerationKlusterletCRDName, UID: "crd-uid"}}
	importWork := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{
		Name: "import-work", UID: "work-uid", Finalizers: []string{workv1.AppliedManifestWorkFinalizer},
	}, Spec: workv1.AppliedManifestWorkSpec{AgentID: "klusterlet-uid", ManifestWorkName: "spoke-a-klusterlet-crds"}}
	configWork := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "config-work", UID: "config-work-uid"},
		Spec: workv1.AppliedManifestWorkSpec{AgentID: "klusterlet-uid", ManifestWorkName: "policies"}}
	residual := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hub-alertmanager-router-ca-hub", Namespace: "openshift-monitoring", UID: "ca-uid"}}
	unrelated := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "user-alertmanager-ca", Namespace: "openshift-monitoring", UID: "user-ca-uid"}}
	baseSpoke := newSeedLifecycleSpokeClient(t, klusterlet, crd, importWork, configWork, residual, unrelated)
	propagations := map[string]metav1.DeletionPropagation{}
	spoke := interceptor.NewClient(baseSpoke, interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		options := (&client.DeleteOptions{}).ApplyOptions(opts)
		if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != obj.GetUID() {
			t.Fatal("destructive operation lacked a UID precondition")
		}
		if options.PropagationPolicy != nil {
			propagations[obj.GetName()] = *options.PropagationPolicy
		}
		return c.Delete(ctx, obj, opts...)
	}})
	task := seedTestTask(pr, hub)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
	reconcile := func() bool {
		t.Helper()
		pending, err := task.reconcileSeedGenerationAgentDetachment(ctx, seedGenerationLifecycleTestSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		return pending
	}
	if !reconcile() {
		t.Fatal("did not wait for add-on pre-delete hook")
	}
	g.Expect(seedGenerationCondition(pr).Message).To(gomega.ContainSubstring("observability-controller"))
	if err := baseSpoke.Get(ctx, client.ObjectKey{Name: seedGenerationKlusterletName}, klusterlet); err != nil || klusterlet.GetDeletionTimestamp() != nil {
		t.Fatalf("Klusterlet teardown preceded add-on cleanup: %v", err)
	}
	g.Expect(hub.Get(ctx, client.ObjectKey{Name: "spoke-a"}, cluster)).ToNot(gomega.HaveOccurred())
	if cluster.Labels["observability"] != "disabled" || cluster.Annotations[disableAutoImportAnnotation] != "true" || len(cluster.Spec.Taints) != 1 {
		t.Fatal("ACM recreation was not disabled")
	}
	g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(addon), addon)).ToNot(gomega.HaveOccurred())
	addon.Finalizers = nil
	g.Expect(hub.Update(ctx, addon)).ToNot(gomega.HaveOccurred())
	if !reconcile() {
		t.Fatal("did not wait for import ReadOnly acknowledgment")
	}
	if err := baseSpoke.Get(ctx, client.ObjectKey{Name: seedGenerationKlusterletName}, klusterlet); err != nil || klusterlet.GetDeletionTimestamp() != nil {
		t.Fatal("Klusterlet deleted while import could still recreate it")
	}
	g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(work), work)).ToNot(gomega.HaveOccurred())
	g.Expect(work.Spec.ManifestConfigs).To(gomega.HaveLen(1))
	g.Expect(work.Spec.ManifestConfigs[0].ResourceIdentifier.Resource).To(gomega.Equal("klusterlets"))
	g.Expect(work.Spec.ManifestConfigs[0].UpdateStrategy.Type).To(gomega.Equal(workv1.UpdateStrategyTypeReadOnly))
	g.Expect(seedGenerationCondition(pr).Message).To(gomega.ContainSubstring("ReadOnly"))
	// Even an Applied condition from the previous generation is insufficient.
	work.Generation = 2
	work.Status.Conditions = []metav1.Condition{{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	g.Expect(hub.Update(ctx, work)).ToNot(gomega.HaveOccurred())
	g.Expect(reconcile()).To(gomega.BeTrue())
	g.Expect(baseSpoke.Get(ctx, client.ObjectKeyFromObject(klusterlet), klusterlet)).ToNot(gomega.HaveOccurred())
	g.Expect(klusterlet.GetDeletionTimestamp()).To(gomega.BeNil())
	work.Status.Conditions[0].ObservedGeneration = 2
	g.Expect(hub.Update(ctx, work)).ToNot(gomega.HaveOccurred())
	if !reconcile() {
		t.Fatal("did not wait for Klusterlet finalizer")
	}
	if err := baseSpoke.Get(ctx, client.ObjectKey{Name: seedGenerationKlusterletName}, klusterlet); err != nil {
		t.Fatal(err)
	}
	if klusterlet.GetDeletionTimestamp() == nil {
		t.Fatal("Klusterlet was not deleted")
	}
	// Simulate ACM completing its finalizer: it removes AMW finalizers but
	// leaves the AMWs and their owned Klusterlet CRD installed.
	klusterlet.SetFinalizers(nil)
	g.Expect(baseSpoke.Update(ctx, klusterlet)).ToNot(gomega.HaveOccurred())
	if !reconcile() {
		t.Fatal("unfinished AppliedManifestWork was not respected")
	}
	if err := baseSpoke.Get(ctx, client.ObjectKeyFromObject(importWork), importWork); err != nil {
		t.Fatal(err)
	}
	importWork.Finalizers = nil
	g.Expect(baseSpoke.Update(ctx, importWork)).ToNot(gomega.HaveOccurred())
	if !reconcile() {
		t.Fatal("did not wait for AppliedManifestWork deletion")
	}
	if propagations["config-work"] != metav1.DeletePropagationOrphan || propagations["import-work"] != metav1.DeletePropagationBackground {
		t.Fatalf("incorrect workload preservation: %v", propagations)
	}
	for range 5 {
		if !reconcile() {
			break
		}
	}
	if !pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.AgentDetachmentCompleted {
		t.Fatal("detachment did not finish")
	}
	g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal("ACM detachment completed; preparing LCA seed generation"))
	g.Expect(seedGenerationCondition(pr).Message).To(gomega.Equal(pr.Status.ProvisioningStatus.ProvisioningDetails))
	if err := baseSpoke.Get(ctx, client.ObjectKeyFromObject(residual), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("residual secret remains: %v", err)
	}
	if err := baseSpoke.Get(ctx, client.ObjectKeyFromObject(unrelated), &corev1.Secret{}); err != nil {
		t.Fatalf("unrelated secret removed: %v", err)
	}
}

func TestSeedGenerationPausesLegacyAddonConfigs(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	config := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": seedGenerationAddonConfigAPIVersion, "kind": "KlusterletAddonConfig",
		"metadata": map[string]any{"name": "spoke-a", "namespace": "spoke-a", "uid": "config-uid",
			"annotations": map[string]any{"existing": "preserved"}},
		"spec": map[string]any{"policyController": map[string]any{"enabled": true}},
	}}
	other := config.DeepCopy()
	other.SetNamespace("other-cluster")
	other.SetUID("other-uid")
	pr := seedDetachmentTestRequest()
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"}}
	addon := &addonv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
		Name: "config-policy-controller", Namespace: "spoke-a", UID: "addon-uid",
	}}
	hub := newSeedTestClient(t, pr, cluster, config, other, addon)
	task := seedTestTask(pr, hub)
	spoke := newSeedLifecycleSpokeClient(t)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
	pending, err := task.reconcileSeedGenerationAgentDetachment(ctx, seedGenerationLifecycleTestSnapshot())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(pending).To(gomega.BeTrue())
	g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(config), config)).ToNot(gomega.HaveOccurred())
	g.Expect(config.GetAnnotations()).To(gomega.HaveKeyWithValue(seedGenerationAddonConfigPause, "true"))
	g.Expect(config.GetAnnotations()).To(gomega.HaveKeyWithValue("existing", "preserved"))
	enabled, _, err := unstructured.NestedBool(config.Object, "spec", "policyController", "enabled")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(enabled).To(gomega.BeTrue())
	// The first reconciliation pauses the producer and leaves the add-on intact.
	g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(addon), addon)).ToNot(gomega.HaveOccurred())
	g.Expect(addon.DeletionTimestamp).To(gomega.BeNil())
	g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(other), other)).ToNot(gomega.HaveOccurred())
	g.Expect(other.GetAnnotations()).ToNot(gomega.HaveKey(seedGenerationAddonConfigPause))
	_, err = task.reconcileSeedGenerationAgentDetachment(ctx, seedGenerationLifecycleTestSnapshot())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(apierrors.IsNotFound(hub.Get(ctx, client.ObjectKeyFromObject(addon), addon))).To(gomega.BeTrue())
	// Subsequent checks are idempotent and do not unpause the source.
	pending, err = task.pauseSeedGenerationAddonConfigs(ctx, "spoke-a")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(pending).To(gomega.BeFalse())
}

func TestSeedGenerationAddonPauseErrorsBlockDeletion(t *testing.T) {
	for _, operation := range []string{"list", "patch", "deleting"} {
		t.Run(operation, func(t *testing.T) {
			g := gomega.NewWithT(t)
			pr := seedDetachmentTestRequest()
			config := &unstructured.Unstructured{}
			config.SetAPIVersion(seedGenerationAddonConfigAPIVersion)
			config.SetKind("KlusterletAddonConfig")
			config.SetName("spoke-a")
			config.SetNamespace("spoke-a")
			config.SetUID("config-uid")
			if operation == "deleting" {
				now := metav1.Now()
				config.SetDeletionTimestamp(&now)
				config.SetFinalizers([]string{"cleanup"})
			}
			cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"}}
			addon := &addonv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "config-policy-controller", Namespace: "spoke-a"}}
			hub := newSeedTestClient(t, pr, cluster, config, addon)
			injected := errors.New("injected API error")
			wrapped := interceptor.NewClient(hub.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if operation == "list" {
						return injected
					}
					return c.List(ctx, list, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if obj.GetObjectKind().GroupVersionKind().Kind == "KlusterletAddonConfig" {
						if operation == "deleting" {
							t.Fatal("patched a deleting configuration")
						}
						return injected
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			task := seedTestTask(pr, wrapped)
			spoke := newSeedLifecycleSpokeClient(t)
			task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
			pending, err := task.reconcileSeedGenerationAgentDetachment(context.Background(), seedGenerationLifecycleTestSnapshot())
			if operation == "deleting" {
				g.Expect(err).ToNot(gomega.HaveOccurred())
				g.Expect(pending).To(gomega.BeTrue())
			} else {
				g.Expect(errors.Is(err, injected)).To(gomega.BeTrue())
			}
			g.Expect(hub.Get(context.Background(), client.ObjectKeyFromObject(addon), addon)).ToNot(gomega.HaveOccurred())
			g.Expect(addon.DeletionTimestamp).To(gomega.BeNil())
		})
	}
}

func TestSeedGenerationReadOnlyConfigsMatchResources(t *testing.T) {
	g := gomega.NewWithT(t)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)
	identifier := workv1.ResourceIdentifier{Resource: "configmaps", Namespace: "agent", Name: "config"}
	work := &workv1.ManifestWork{Spec: workv1.ManifestWorkSpec{
		Workload: workv1.ManifestsTemplate{Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{
			Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"config","namespace":"agent"}}`),
		}}}},
		ManifestConfigs: []workv1.ManifestConfigOption{{ResourceIdentifier: identifier,
			FeedbackRules: []workv1.FeedbackRule{{Type: workv1.WellKnownStatusType}}}},
	}}
	configs, err := seedGenerationReadOnlyConfigs(work, mapper)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(configs).To(gomega.HaveLen(1))
	g.Expect(configs[0].ResourceIdentifier).To(gomega.Equal(identifier))
	g.Expect(configs[0].FeedbackRules).To(gomega.Equal(work.Spec.ManifestConfigs[0].FeedbackRules))
	g.Expect(configs[0].UpdateStrategy.Type).To(gomega.Equal(workv1.UpdateStrategyTypeReadOnly))
	// An entry with the right name but wrong resource does not disable writes.
	work.Spec.ManifestConfigs[0].ResourceIdentifier.Resource = "configmap"
	configs, err = seedGenerationReadOnlyConfigs(work, mapper)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(configs[0].ResourceIdentifier.Resource).To(gomega.Equal("configmaps"))
	for _, raw := range []string{`{`, `{}`, `{"apiVersion":"v1","kind":"Unknown","metadata":{"name":"unknown"}}`} {
		work.Spec.Workload.Manifests[0].Raw = []byte(raw)
		_, err := seedGenerationReadOnlyConfigs(work, mapper)
		g.Expect(err).To(gomega.HaveOccurred())
	}
}

func TestSeedGenerationDetachmentProgressIsStable(t *testing.T) {
	g := gomega.NewWithT(t)
	pr := seedDetachmentTestRequest()
	hub := newSeedTestClient(t, pr)
	task := seedTestTask(pr, hub)
	message := "Waiting for ACM add-on removal: config-policy-controller"
	_, err := task.waitForSeedGenerationDetachment(context.Background(), message)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	version := pr.ResourceVersion
	_, err = task.waitForSeedGenerationDetachment(context.Background(), message)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(pr.ResourceVersion).To(gomega.Equal(version))
	g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal(message))
	g.Expect(seedGenerationCondition(pr).Message).To(gomega.ContainSubstring("config-policy-controller"))
}

func TestSeedGenerationDetachmentRejectsChangedIdentities(t *testing.T) {
	for _, changed := range []string{"spoke", "hub", "missing agent identity", "additional klusterlet"} {
		t.Run(changed, func(t *testing.T) {
			pr := seedDetachmentTestRequest()
			cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"}}
			work := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "other-work"}, Spec: workv1.AppliedManifestWorkSpec{AgentID: "klusterlet-uid"}}
			if changed == "hub" {
				cluster.UID = "replacement-uid"
			}
			if changed == "missing agent identity" {
				pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.KlusterletUID = ""
			}
			if changed == "spoke" {
				pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ClusterID = "other-cluster"
			}
			objects := []client.Object{work}
			if changed == "additional klusterlet" {
				other := newSeedGenerationKlusterlet()
				other.SetName("other-agent")
				objects = append(objects, other, &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: seedGenerationKlusterletCRDName},
				})
			}
			spoke := newSeedLifecycleSpokeClient(t, objects...)
			task := seedTestTask(pr, newSeedTestClient(t, pr, cluster))
			task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
			if _, err := task.reconcileSeedGenerationAgentDetachment(context.Background(), seedGenerationLifecycleTestSnapshot()); err == nil {
				t.Fatal("changed identity was accepted")
			}
			if err := spoke.Get(context.Background(), client.ObjectKeyFromObject(work), &workv1.AppliedManifestWork{}); err != nil {
				t.Fatalf("foreign work removed: %v", err)
			}
		})
	}
}

func TestSeedGenerationWaitsForForeignAppliedManifestWorks(t *testing.T) {
	for _, tc := range []struct {
		name, manifestWorkName, agentID string
	}{
		{"import work", "spoke-a-klusterlet", "previous-agent"},
		{"non-import work", "policies", "previous-agent"},
		{"missing work agent", "policies", ""},
	} {
		for _, outcome := range []string{"removed", "timeout"} {
			t.Run(tc.name+"/"+outcome, func(t *testing.T) {
				g := gomega.NewWithT(t)
				ctx := context.Background()
				pr := seedDetachmentTestRequest()
				startedAt := metav1.NewTime(time.Now().Truncate(time.Second))
				status := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
				status.StartedAt, status.TimeoutSeconds = &startedAt, 3600
				setSeedGenerationProgress(pr, provisioningv1alpha1.CRconditionReasons.CleaningACMResources, "Removing ACM agents from the spoke")
				snapshot, hub := seedRecoveryTestSnapshot(t, pr)
				cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"}}
				g.Expect(hub.Create(ctx, cluster)).ToNot(gomega.HaveOccurred())
				// Put an owned record first so an implementation that deletes as it
				// checks ownership would be caught before reaching the foreign one.
				owned := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "a-owned-import", UID: "owned-uid"},
					Spec: workv1.AppliedManifestWorkSpec{AgentID: "klusterlet-uid", ManifestWorkName: "spoke-a-klusterlet-crds"}}
				foreign := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "z-foreign-work", UID: "foreign-uid"},
					Spec: workv1.AppliedManifestWorkSpec{AgentID: tc.agentID, ManifestWorkName: tc.manifestWorkName}}
				baseSpoke := newSeedLifecycleSpokeClient(t, owned, foreign)
				blocked := true
				spoke := interceptor.NewClient(baseSpoke, interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						if blocked {
							t.Fatal("foreign work must block LCA submission")
						}
						return c.Create(ctx, obj, opts...)
					},
					Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if blocked {
							t.Fatal("no spoke resource should be deleted while a foreign work remains")
						}
						return c.Delete(ctx, obj, opts...)
					},
				})
				reconcile := func() {
					t.Helper()
					// Rebuild from persisted state to exercise recovery after restart.
					g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
					task := seedTestTask(pr, hub)
					task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
					result, err := task.reconcileSeedGeneration(ctx)
					g.Expect(err).ToNot(gomega.HaveOccurred())
					g.Expect(result.RequeueAfter).To(gomega.BeNumerically(">", 0))
					g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
				}
				reconcile()
				g.Expect(seedGenerationTerminal(pr)).To(gomega.BeFalse())
				g.Expect(pr.Status.ProvisioningStatus.ProvisioningPhase).To(gomega.Equal(provisioningv1alpha1.StateProgressing))
				g.Expect(seedGenerationCondition(pr).Reason).To(gomega.Equal(string(provisioningv1alpha1.CRconditionReasons.CleaningACMResources)))
				message := seedGenerationCondition(pr).Message
				g.Expect(message).To(gomega.ContainSubstring("Waiting for AppliedManifestWork z-foreign-work owned by a different ACM agent"))
				g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal(message))
				g.Expect(pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.AgentDetachmentCompleted).To(gomega.BeFalse())
				g.Expect(pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted).To(gomega.BeFalse())
				version := pr.ResourceVersion
				reconcile()
				g.Expect(pr.ResourceVersion).To(gomega.Equal(version))
				status = pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
				g.Expect(status.StartedAt.Equal(&startedAt)).To(gomega.BeTrue())
				g.Expect(status.TimeoutSeconds).To(gomega.Equal(int64(3600)))
				g.Expect(status.InputSnapshotResourceUIDs).To(gomega.Equal(seedGenerationInputUIDs(snapshot)))
				g.Expect(baseSpoke.Get(ctx, client.ObjectKeyFromObject(owned), &workv1.AppliedManifestWork{})).ToNot(gomega.HaveOccurred())
				g.Expect(baseSpoke.Get(ctx, client.ObjectKeyFromObject(foreign), &workv1.AppliedManifestWork{})).ToNot(gomega.HaveOccurred())

				if outcome == "removed" {
					// External cleanup resolves the conflict; the controller never
					// deletes or adopts the foreign work itself.
					g.Expect(baseSpoke.Delete(ctx, foreign)).ToNot(gomega.HaveOccurred())
					blocked = false
					reconcile()
					g.Expect(apierrors.IsNotFound(baseSpoke.Get(ctx, client.ObjectKeyFromObject(owned), &workv1.AppliedManifestWork{}))).To(gomega.BeTrue())
					reconcile()
					g.Expect(pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.AgentDetachmentCompleted).To(gomega.BeTrue())
					g.Expect(pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted).To(gomega.BeTrue())
					g.Expect(baseSpoke.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, newSeedGenerationSeedGenerator())).ToNot(gomega.HaveOccurred())
				} else {
					status.StartedAt = &metav1.Time{Time: time.Now().Add(-2 * time.Hour)}
					g.Expect(hub.Status().Update(ctx, pr)).ToNot(gomega.HaveOccurred())
					reconcile()
					g.Expect(seedGenerationTerminal(pr)).To(gomega.BeTrue())
					g.Expect(seedGenerationCondition(pr).Reason).To(gomega.Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
					g.Expect(seedGenerationCondition(pr).Message).To(gomega.ContainSubstring(message))
					g.Expect(pr.Status.ProvisioningStatus.ProvisioningPhase).To(gomega.Equal(provisioningv1alpha1.StateFailed))
					g.Expect(baseSpoke.Get(ctx, client.ObjectKeyFromObject(owned), &workv1.AppliedManifestWork{})).ToNot(gomega.HaveOccurred())
					g.Expect(baseSpoke.Get(ctx, client.ObjectKeyFromObject(foreign), &workv1.AppliedManifestWork{})).ToNot(gomega.HaveOccurred())
					g.Expect(pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted).To(gomega.BeFalse())
				}
			})
		}
	}
}
