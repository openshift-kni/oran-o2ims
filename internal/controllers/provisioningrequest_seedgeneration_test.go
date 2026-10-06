/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"log/slog"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func newSeedTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		provisioningv1alpha1.AddToScheme, corev1.AddToScheme, batchv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&provisioningv1alpha1.ProvisioningRequest{}).
		WithObjects(objects...).Build()
}

func seedTestTask(pr *provisioningv1alpha1.ProvisioningRequest, c client.Client) *provisioningRequestReconcilerTask {
	return &provisioningRequestReconcilerTask{
		object: pr, client: c, logger: slog.New(slog.DiscardHandler),
	}
}

func TestSeedGenerationOneShotRouting(t *testing.T) {
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
	task := seedTestTask(pr, nil)
	if task.seedGenerationNeedsFastPath() || seedGenerationTerminal(pr) {
		t.Fatal("new request entered seed fast path")
	}

	started := metav1.Now()
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus = &provisioningv1alpha1.SeedGenerationStatus{StartedAt: &started, TimeoutSeconds: 3600}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Validating, metav1.ConditionFalse, "validating")
	if !task.seedGenerationNeedsFastPath() || seedGenerationTerminal(pr) {
		t.Fatal("active run did not enter seed fast path")
	}

	ctlrutils.SetStatusCondition(&pr.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, metav1.ConditionFalse, "failed")
	if task.seedGenerationNeedsFastPath() || !seedGenerationTerminal(pr) {
		t.Fatal("pre-detachment terminal failure should resume normal reconciliation")
	}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachmentStarted = true
	if !task.seedGenerationNeedsFastPath() {
		t.Fatal("detached terminal run entered normal reconciliation")
	}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Completed, metav1.ConditionTrue, "done")
	if !seedGenerationTerminal(pr) || !task.seedGenerationNeedsFastPath() {
		t.Fatal("completed detached run was not gated")
	}
}

func TestSeedGenerationFailureCleanupResumesNormalReconciliation(t *testing.T) {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("pr-uid"),
	}}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, metav1.ConditionFalse, "preflight failed")
	task := seedTestTask(pr, newSeedTestClient(t))

	handled, result, err := task.reconcileSeedGenerationFailureCleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if handled || result.Requeue || result.RequeueAfter != 0 {
		t.Fatalf("completed cleanup stopped normal reconciliation: handled=%t result=%+v", handled, result)
	}
}

func TestSeedGenerationTimeoutResolution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
		want   time.Duration
	}{
		{"seed only", map[string]any{"seedGeneration": map[string]any{}}, 2 * time.Hour},
		{"with ISO", map[string]any{"seedGeneration": map[string]any{"liveISO": map[string]any{}}}, 3 * time.Hour},
		{"custom", map[string]any{"seedGeneration": map[string]any{"seedGenerationTimeout": "4h30m"}}, 4*time.Hour + 30*time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := seedGenerationTimeout(tc.config)
			if err != nil || got != tc.want {
				t.Fatalf("got %s, %v; want %s", got, err, tc.want)
			}
		})
	}
	if _, err := seedGenerationTimeout(map[string]any{"seedGeneration": map[string]any{"seedGenerationTimeout": "0s"}}); err == nil {
		t.Fatal("zero timeout accepted")
	}
	if _, err := seedGenerationTimeout(map[string]any{"seedGenerationTimeout": "4h"}); err == nil {
		t.Fatal("upgrade-level seed generation timeout accepted")
	}
	if _, err := seedGenerationTimeout(map[string]any{"clusterUpgradeTimeout": "4h"}); err == nil {
		t.Fatal("upgrade timeout accepted for seed generation")
	}
}

func TestSeedGenerationRequestDoesNotDependOnReleaseComparison(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-pr"},
		Spec: provisioningv1alpha1.ProvisioningRequestSpec{
			TemplateName: "seed", TemplateVersion: "v1",
			TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
		},
	}
	ct := &provisioningv1alpha1.ClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "seed.v1", Namespace: "templates"},
		Spec: provisioningv1alpha1.ClusterTemplateSpec{
			Release: "4.99.0",
			TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
				UpgradeDefaults: runtime.RawExtension{Raw: []byte(`{"seedGeneration":{}}`)},
			},
		},
		Status: provisioningv1alpha1.ClusterTemplateStatus{Conditions: []metav1.Condition{{
			Type: string(provisioningv1alpha1.CTconditionTypes.Validated), Status: metav1.ConditionTrue,
		}}},
	}
	c := newSeedTestClient(t, pr, ct)
	task := seedTestTask(pr, c)
	requested, template, err := task.IsSeedGenerationRequested(ctx)
	if err != nil || requested || template != nil {
		t.Fatalf("seed request started before ZTP Done: requested=%t templatePresent=%t err=%v",
			requested, template != nil, err)
	}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{ZtpStatus: ctlrutils.ClusterZtpDone}
	requested, template, err = task.IsSeedGenerationRequested(ctx)
	if err != nil || !requested || template == nil {
		t.Fatalf("seed request was not detected without a ManagedCluster: requested=%t templatePresent=%t err=%v",
			requested, template != nil, err)
	}
}

func TestSeedGenerationStartAndTimeout(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr"}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
	c := newSeedTestClient(t, pr)
	task := seedTestTask(pr, c)
	if err := task.startSeedGeneration(ctx, 90*time.Minute); err != nil {
		t.Fatal(err)
	}
	fetched := &provisioningv1alpha1.ProvisioningRequest{}
	if err := c.Get(ctx, types.NamespacedName{Name: pr.Name}, fetched); err != nil {
		t.Fatal(err)
	}
	status := fetched.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status == nil || status.StartedAt == nil || status.TimeoutSeconds != 5400 {
		t.Fatalf("run state was not persisted: %+v", status)
	}
	if err := task.startSeedGeneration(ctx, time.Hour); err == nil {
		t.Fatal("second start accepted")
	}
	status.StartedAt = &metav1.Time{Time: time.Now().Add(-2 * time.Hour)}
	if err := c.Status().Update(ctx, fetched); err != nil {
		t.Fatal(err)
	}
	task = seedTestTask(fetched, c)
	result, err := task.reconcileSeedGeneration(ctx)
	if err != nil || !result.IsZero() {
		t.Fatalf("timeout reconcile returned %v, %v", result, err)
	}
	if !seedGenerationTerminal(fetched) || seedGenerationCondition(fetched).Reason != string(provisioningv1alpha1.CRconditionReasons.TimedOut) {
		t.Fatalf("timeout did not become terminal: %+v", fetched.Status.Conditions)
	}
}

func TestSeedGenerationFinalizerCleanupUsesUID(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr", UID: types.UID("current-uid")}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{},
	}
	labels := map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
	}
	owned := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: constants.DefaultNamespace, Labels: labels}}
	foreignLabels := map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  "old-uid",
	}
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: constants.DefaultNamespace, Labels: foreignLabels}}
	c := newSeedTestClient(t, pr, owned, foreign)
	r := &ProvisioningRequestReconciler{Client: c, Logger: slog.New(slog.DiscardHandler)}
	// The finalizer must stop here, before touching ManagedCluster or NAR.
	clean, err := r.handleProvisioningRequestDeletion(ctx, pr)
	if err != nil || clean {
		t.Fatalf("first cleanup should delete and wait: clean=%t err=%v", clean, err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(foreign), &corev1.Secret{}); err != nil {
		t.Fatalf("foreign resource was removed: %v", err)
	}
	clean, err = r.cleanupSeedGenerationResources(ctx, pr)
	if err != nil || !clean {
		t.Fatalf("cleanup did not complete: clean=%t err=%v", clean, err)
	}
}

func TestReconcileSeedGenerationPreflightFailsWhenSpokeIsNotReady(t *testing.T) {
	ctx := context.Background()
	started := metav1.Now()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		Name: "spoke",
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
			StartedAt: &started, TimeoutSeconds: 3600,
		},
	}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Validating, metav1.ConditionFalse, "validating")
	c := newSeedTestClient(t, pr)
	task := seedTestTask(pr, c)
	if _, err := task.reconcileSeedGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if !seedGenerationTerminal(pr) || seedGenerationCondition(pr).Reason != string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed) {
		t.Fatalf("unready spoke did not fail preflight: %+v", pr.Status.Conditions)
	}
}
