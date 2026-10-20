/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSeedGenerationFinalizerCleanupSurvivesSnapshotDeletion(t *testing.T) {
	ctx := context.Background()
	clusterName := "spoke-a"
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("seed-pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		Name:                 clusterName,
		SeedGenerationStatus: seedLifecycleTestStatus(),
	}
	document := &seedGenerationInputDocument{
		ClusterTemplateName:      "seed-template.v1",
		ClusterTemplateNamespace: "templates",
		Release:                  "4.22.0",
		SeedGeneration:           map[string]any{"seedImage": "quay.io/example/seed:4.22"},
	}
	configMap, secrets, err := makeSeedGenerationSnapshotObjects(pr, document, map[string]map[string][]byte{
		"seedAuth":        {seedGenerationAuthSecretKey: []byte(`{"auths":{}}`)},
		"spokeKubeconfig": {"kubeconfig": []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}
	configMap.UID = types.UID("snapshot-config-uid")
	secrets["seedAuth"].UID = types.UID("snapshot-auth-uid")
	secrets["spokeKubeconfig"].UID = types.UID("snapshot-kubeconfig-uid")
	snapshot := &seedGenerationInputSnapshot{ConfigMap: configMap, Secrets: secrets}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs = seedGenerationInputUIDs(snapshot)

	hubScheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		provisioningv1alpha1.AddToScheme, corev1.AddToScheme, batchv1.AddToScheme,
		addonv1alpha1.AddToScheme, clusterv1.AddToScheme,
	} {
		if err := add(hubScheme); err != nil {
			t.Fatal(err)
		}
	}
	hubObjects := []client.Object{
		pr, configMap, secrets["seedAuth"], secrets["spokeKubeconfig"],
		&clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}},
	}
	hubClient := fake.NewClientBuilder().WithScheme(hubScheme).
		WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).
		WithObjects(hubObjects...).Build()

	spokeClient := newSeedLifecycleSpokeClient(t)
	task := seedTestTask(pr, hubClient)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spokeClient, nil }

	pending, err := task.cleanupSeedGenerationDetachedResourcesAndRecordCompletion(ctx)
	if err != nil || pending {
		t.Fatalf("first cleanup pending=%t err=%v; want completed", pending, err)
	}
	if !pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachedCleanupCompleted {
		t.Fatal("detached cleanup completion status was not persisted")
	}

	reconciler := &ProvisioningRequestReconciler{Client: hubClient}
	clean, err := reconciler.cleanupSeedGenerationResources(ctx, pr)
	if err != nil || clean {
		t.Fatalf("snapshot cleanup clean=%t err=%v; want deletion initiated", clean, err)
	}
	clean, err = reconciler.cleanupSeedGenerationResources(ctx, pr)
	if err != nil || !clean {
		t.Fatalf("retry after snapshot deletion clean=%t err=%v; want cleanup complete", clean, err)
	}
}

func TestSeedGenerationCleanupDoesNotInferSuccessFromMissingSnapshot(t *testing.T) {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr", UID: types.UID("pr-uid")}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "spoke-a", SeedGenerationStatus: seedLifecycleTestStatus()}
	task := seedTestTask(pr, newSeedTestClient(t, pr))
	if _, err := task.cleanupSeedGenerationDetachedResourcesAndRecordCompletion(context.Background()); err == nil {
		t.Fatal("missing credentials were treated as proof of completed cleanup")
	}
	if pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachedCleanupCompleted {
		t.Fatal("cleanup was incorrectly marked complete")
	}
	// Normal PR deletion wipes the node and must not need a spoke connection.
	clean, err := (&ProvisioningRequestReconciler{Client: task.client}).cleanupSeedGenerationResources(context.Background(), pr)
	if err != nil || !clean {
		t.Fatalf("normal deprovisioning was blocked: clean=%t err=%v", clean, err)
	}
}

func TestReconcileSeedGeneratorDoesNotRecreateMissingLCAObjects(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("seed-pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		Name: "spoke-a", SeedGenerationStatus: seedLifecycleTestStatus(),
	}
	hubClient := newSeedTestClient(t, pr)
	task := seedTestTask(pr, hubClient)
	spokeClient := newSeedLifecycleSpokeClient(t)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spokeClient, nil }
	snapshot := seedGenerationLifecycleTestSnapshot()

	if _, err := task.reconcileSeedGenerator(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if !pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted {
		t.Fatal("SeedGenerator submission marker was not persisted")
	}
	seedGenerator := newSeedGenerationSeedGenerator()
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, seedGenerator); err != nil {
		t.Fatalf("SeedGenerator was not created: %v", err)
	}
	if err := validateSeedGeneratorSpec(seedGenerator, snapshot); err != nil {
		t.Fatalf("created SeedGenerator has an unexpected spec: %v", err)
	}
	seedSecret := &corev1.Secret{}
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}, seedSecret); err != nil {
		t.Fatalf("seedgen Secret was not created: %v", err)
	}

	if err := spokeClient.Delete(ctx, seedGenerator); err != nil {
		t.Fatal(err)
	}
	if err := spokeClient.Delete(ctx, seedSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := task.reconcileSeedGenerator(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, newSeedGenerationSeedGenerator()); err == nil {
		t.Fatal("missing SeedGenerator was recreated after LCA submission")
	}
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}, &corev1.Secret{}); err == nil {
		t.Fatal("missing seedgen Secret was recreated after LCA submission")
	}
	g := gomega.NewWithT(t)
	g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal(
		"Waiting for LCA seed generation status; LCA temporarily removes the SeedGenerator during image creation"))
	g.Expect(seedGenerationCondition(pr).Message).To(gomega.Equal(pr.Status.ProvisioningStatus.ProvisioningDetails))
	version := pr.ResourceVersion
	_, err := task.reconcileSeedGenerator(ctx, snapshot)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(pr.ResourceVersion).To(gomega.Equal(version))
}

func TestObserveSeedGeneratorUpdatesBothProgressMessages(t *testing.T) {
	for _, tc := range []struct {
		name             string
		lcaMessage       string
		conditionCurrent bool
		detailsCurrent   bool
	}{
		{name: "waiting for first LCA progress"},
		{name: "LCA cleanup", lcaMessage: "Cleaning cluster resources"},
		{name: "LCA launch", lcaMessage: "Launching imager container"},
		{name: "repair stale details", lcaMessage: "Cleaning cluster resources", conditionCurrent: true},
		{name: "unchanged progress", lcaMessage: "Cleaning cluster resources", conditionCurrent: true, detailsCurrent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			ctx := context.Background()
			pr := seedDetachmentTestRequest()
			message := "Waiting for LCA to report seed generation progress"
			if tc.lcaMessage != "" {
				message = "LCA seed generation last reported: " + tc.lcaMessage
			}
			transition := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
			pr.Status.Conditions = []metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted),
				Status: metav1.ConditionFalse, Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Starting LCA seed image generation and push", LastTransitionTime: transition,
			}}
			if tc.conditionCurrent {
				pr.Status.Conditions[0].Message = message
			}
			pr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateProgressing
			pr.Status.ProvisioningStatus.ProvisioningDetails = "Waiting for ACM observability artifact cleanup"
			pr.Status.ProvisioningStatus.UpdateTime = transition
			if tc.detailsCurrent {
				pr.Status.ProvisioningStatus.ProvisioningDetails = message
			}
			hub := newSeedTestClient(t, pr)
			g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
			version := pr.ResourceVersion
			task := seedTestTask(pr, hub)
			seed := newSeedGenerationSeedGenerator()
			seed.Object["status"] = map[string]any{"conditions": []any{map[string]any{
				"type": seedGenerationLCAInProgress, "status": "True", "reason": "InProgress", "message": tc.lcaMessage,
			}}}
			_, err := task.observeSeedGenerator(ctx, seed, seedGenerationLifecycleTestSnapshot())
			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
			g.Expect(seedGenerationCondition(pr).Message).To(gomega.Equal(message))
			g.Expect(seedGenerationCondition(pr).LastTransitionTime).To(gomega.Equal(transition))
			g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal(message))
			if tc.conditionCurrent && tc.detailsCurrent {
				g.Expect(pr.ResourceVersion).To(gomega.Equal(version))
				g.Expect(pr.Status.ProvisioningStatus.UpdateTime).To(gomega.Equal(transition))
			} else {
				g.Expect(pr.ResourceVersion).ToNot(gomega.Equal(version))
				g.Expect(pr.Status.ProvisioningStatus.UpdateTime.Time.After(transition.Time)).To(gomega.BeTrue())
			}
			version = pr.ResourceVersion
			_, err = task.observeSeedGenerator(ctx, seed, seedGenerationLifecycleTestSnapshot())
			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(pr.ResourceVersion).To(gomega.Equal(version))
		})
	}
}

func TestReconcileSeedGeneratorReportsSpokeAPIOutageAndRecovery(t *testing.T) {
	for _, kind := range []string{"ClusterVersion", seedGenerationLCAKind} {
		for _, submitted := range []bool{false, true} {
			name := kind + "/before submission"
			if submitted {
				name = kind + "/after submission"
			}
			t.Run(name, func(t *testing.T) {
				g := gomega.NewWithT(t)
				ctx := context.Background()
				pr := seedDetachmentTestRequest()
				pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = submitted
				setSeedGenerationProgress(pr, provisioningv1alpha1.CRconditionReasons.InProgress, "Preparing LCA seed generation")
				hub := newSeedTestClient(t, pr)
				snapshot := seedGenerationLifecycleTestSnapshot()
				seed := desiredSeedGenerator(pr, snapshot)
				seed.Object["status"] = map[string]any{"conditions": []any{map[string]any{
					"type": seedGenerationLCAInProgress, "status": "True", "reason": "InProgress", "message": "Launching imager container",
				}}}
				unavailable := true
				spoke := interceptor.NewClient(newSeedLifecycleSpokeClient(t, seed), interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if unavailable && obj.GetObjectKind().GroupVersionKind().Kind == kind {
							return apierrors.NewServiceUnavailable("spoke API is offline")
						}
						return c.Get(ctx, key, obj, opts...)
					},
				})
				task := seedTestTask(pr, hub)
				task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
				_, err := task.reconcileSeedGenerator(ctx, snapshot)
				g.Expect(err).To(gomega.HaveOccurred())
				g.Expect(hub.Get(ctx, client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
				if submitted {
					g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.ContainSubstring("Waiting to reconnect to the spoke API"))
				} else {
					g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal("Preparing LCA seed generation"))
				}
				g.Expect(seedGenerationCondition(pr).Message).To(gomega.Equal(pr.Status.ProvisioningStatus.ProvisioningDetails))
				g.Expect(pr.Status.ProvisioningStatus.ProvisioningPhase).To(gomega.Equal(provisioningv1alpha1.StateProgressing))
				version := pr.ResourceVersion
				_, err = task.reconcileSeedGenerator(ctx, snapshot)
				g.Expect(err).To(gomega.HaveOccurred())
				g.Expect(pr.ResourceVersion).To(gomega.Equal(version))

				unavailable = false
				_, err = task.reconcileSeedGenerator(ctx, snapshot)
				g.Expect(err).ToNot(gomega.HaveOccurred())
				g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal("LCA seed generation last reported: Launching imager container"))
				g.Expect(seedGenerationCondition(pr).Message).To(gomega.Equal(pr.Status.ProvisioningStatus.ProvisioningDetails))
			})
		}
	}
}

func TestReconcileSeedGeneratorDoesNotTreatIdentityMismatchAsExpectedOutage(t *testing.T) {
	g := gomega.NewWithT(t)
	pr := seedDetachmentTestRequest()
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = true
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ClusterID = "different-cluster"
	snapshot, hub := seedRecoveryTestSnapshot(t, pr)
	task := seedTestTask(pr, hub)
	spoke := newSeedLifecycleSpokeClient(t)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
	// Cleanup also rejects the mismatched identity, but the failure must already
	// be durable and must not be presented as expected image-builder downtime.
	_, err := task.reconcileSeedGenerator(context.Background(), snapshot)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(hub.Get(context.Background(), client.ObjectKeyFromObject(pr), pr)).ToNot(gomega.HaveOccurred())
	g.Expect(pr.Status.ProvisioningStatus.ProvisioningPhase).To(gomega.Equal(provisioningv1alpha1.StateFailed))
	g.Expect(seedGenerationCondition(pr).Reason).To(gomega.Equal(string(provisioningv1alpha1.CRconditionReasons.Failed)))
	g.Expect(pr.Status.ProvisioningStatus.ProvisioningDetails).To(gomega.Equal(seedGenerationCondition(pr).Message))
}

func TestObserveSeedGeneratorRecordsConfiguredImageReference(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("seed-pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: seedLifecycleTestStatus(),
	}
	task := seedTestTask(pr, newSeedTestClient(t, pr))
	snapshot := seedGenerationLifecycleTestSnapshot()
	seedGenerator := newSeedGenerationSeedGenerator()
	seedGenerator.Object["status"] = map[string]any{"conditions": []any{map[string]any{
		"type": seedGenerationLCACompleted, "status": "True", "reason": "Completed", "message": "Seed Generation completed",
	}}}
	if _, err := task.observeSeedGenerator(ctx, seedGenerator, snapshot); err != nil {
		t.Fatal(err)
	}
	if got := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedImage; got != "quay.io/example/seed:4.22" {
		t.Fatalf("SeedImage=%q; want configured image reference", got)
	}
	if condition := seedGenerationCondition(pr); condition == nil || condition.Status != metav1.ConditionTrue ||
		condition.Reason != string(provisioningv1alpha1.CRconditionReasons.Completed) {
		t.Fatalf("seed generation was not marked complete: %+v", condition)
	}
	if pr.Status.ProvisioningStatus.ProvisioningPhase != provisioningv1alpha1.StateFulfilled {
		t.Fatalf("completed seed generation left stale provisioning state: %+v", pr.Status.ProvisioningStatus)
	}
}

func seedGenerationLifecycleTestSnapshot() *seedGenerationInputSnapshot {
	return &seedGenerationInputSnapshot{
		Document: &seedGenerationInputDocument{SeedGeneration: map[string]any{
			"seedImage": "quay.io/example/seed:4.22",
		}},
		Secrets: map[string]*corev1.Secret{
			"seedAuth":        {Data: map[string][]byte{"seedAuth": []byte(`{"auths":{}}`)}},
			"spokeKubeconfig": {Data: map[string][]byte{"kubeconfig": []byte("test-kubeconfig")}},
		},
	}
}

func seedLifecycleTestStatus() *provisioningv1alpha1.SeedGenerationStatus {
	return &provisioningv1alpha1.SeedGenerationStatus{
		DetachmentStarted: true, ClusterID: "cluster-id-a", ManagedClusterUID: "managed-cluster-uid", KlusterletUID: "klusterlet-uid",
	}
}

func newSeedLifecycleSpokeClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, apiextensionsv1.AddToScheme, workv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	for _, gvk := range []schema.GroupVersionKind{
		{Group: "operator.open-cluster-management.io", Version: "v1", Kind: "Klusterlet"},
		{Group: "operator.open-cluster-management.io", Version: "v1", Kind: "KlusterletList"},
		{Group: "lca.openshift.io", Version: "v1", Kind: "SeedGenerator"},
		{Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion"},
	} {
		if gvk.Kind == "KlusterletList" {
			scheme.AddKnownTypeWithName(gvk, &unstructured.UnstructuredList{})
		} else {
			scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		}
	}
	version := newSeedGenerationClusterVersion()
	version.SetName("version")
	version.Object["spec"] = map[string]any{"clusterID": "cluster-id-a"}
	version.Object["status"] = map[string]any{"desired": map[string]any{"version": "4.22.0"}}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(schema.FromAPIVersionAndKind(seedGenerationKlusterletAPIVersion, seedGenerationKlusterletKind), meta.RESTScopeRoot)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)
	return fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(append(objects, version)...).Build()
}
