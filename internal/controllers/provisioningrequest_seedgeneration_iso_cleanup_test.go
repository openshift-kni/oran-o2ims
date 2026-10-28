/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func seedGenerationISOCleanupFixture(t *testing.T) (*provisioningRequestReconcilerTask, *seedGenerationInputSnapshot) {
	t.Helper()
	pr, snapshot, _ := seedGenerationISOJobFixture(t, "")
	snapshot.Document.ClusterTemplateName = "seed-template"
	snapshot.Document.ClusterTemplateNamespace = "templates"
	config, secrets, err := makeSeedGenerationSnapshotObjects(pr, snapshot.Document, map[string]map[string][]byte{
		"upload": snapshot.Secrets["upload"].Data, "pullSecret": snapshot.Secrets["pullSecret"].Data,
		"seedAuth": {"seedAuth": []byte(`{"auths":{}}`)}, "spokeKubeconfig": {"kubeconfig": []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}
	config.UID = "config-uid"
	objects := []client.Object{pr, config}
	for key, secret := range secrets {
		secret.UID = types.UID(key + "-uid")
		objects = append(objects, secret)
	}
	snapshot.ConfigMap, snapshot.Secrets = config, secrets
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
		ISOJobSubmitted: true, ISOJobUID: "iso-job-uid", DetachedCleanupCompleted: true,
		InputSnapshotResourceUIDs: seedGenerationInputUIDs(snapshot),
	}}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Failed, metav1.ConditionFalse, "Live ISO failed")
	return seedTestTask(pr, newSeedTestClient(t, objects...)), snapshot
}

func TestSeedGenerationISOUploadCleanupWaitsForPodsAndKeepsCredentials(t *testing.T) {
	ctx := context.Background()
	task, snapshot := seedGenerationISOCleanupFixture(t)
	pr := task.object
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: types.UID("iso-job-uid"),
		Labels: seedGenerationISOResourceLabels(pr, seedGenerationISOJobKind), OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "iso-pod", Namespace: job.Namespace, Finalizers: []string{"example.test/wait"},
		Labels: seedGenerationISOPodLabels(pr, job), OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}}
	for _, object := range []client.Object{job, pod} {
		if err := task.client.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	calls := 0
	runSeedGenerationSSHCommand = func(context.Context, string, map[string][]byte, string) ([]byte, error) {
		calls++
		return []byte("removed"), nil
	}
	reconciler := &ProvisioningRequestReconciler{Client: task.client, Logger: task.logger}
	for i := 0; i < 2; i++ {
		if clean, err := reconciler.cleanupSeedGenerationResources(ctx, pr); err != nil || clean {
			t.Fatalf("cleanup skipped active Pod: clean=%t err=%v", clean, err)
		}
	}
	if calls != 0 {
		t.Fatal("remote deletion raced with a live Pod")
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Job remained active while Pod cleanup was blocked: %v", err)
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["upload"]), &corev1.Secret{}); err != nil {
		t.Fatal("credentials removed before Pod stopped", err)
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
		t.Fatal(err)
	}
	pod.Finalizers = nil
	if err := task.client.Update(ctx, pod); err != nil {
		t.Fatal(err)
	}
	// Use a new task, as after a manager restart.
	task = seedTestTask(pr, task.client)
	if pending, err := task.cleanupSeedGenerationISOResources(ctx, false, false); err != nil || pending {
		t.Fatalf("cleanup after Pod stopped: pending=%t err=%v", pending, err)
	}
	status := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if calls != 1 || !status.ISOUploadCleanupCompleted || status.ISOUploadCleanupDeadline == nil {
		t.Fatalf("missing cleanup checkpoint: %+v calls=%d", status, calls)
	}
	for i := 0; i < 3; i++ {
		if _, err := reconciler.cleanupSeedGenerationResources(ctx, pr); err != nil {
			t.Fatal(err)
		}
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["upload"]), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("credentials not reclaimed: %v", err)
	}
	if calls != 1 {
		t.Fatal("cleanup retried after discarding credentials")
	}
}

func TestSeedGenerationISOUploadCleanupRetryBudget(t *testing.T) {
	ctx := context.Background()
	task, snapshot := seedGenerationISOCleanupFixture(t)
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	calls := 0
	runSeedGenerationSSHCommand = func(context.Context, string, map[string][]byte, string) ([]byte, error) {
		calls++
		return nil, errors.New("unreachable")
	}
	if pending, err := task.cleanupSeedGenerationISOUpload(ctx); err != nil || !pending {
		t.Fatalf("expected bounded retry: %t %v", pending, err)
	}
	deadline := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOUploadCleanupDeadline.DeepCopy()
	stored := &provisioningv1alpha1.ProvisioningRequest{}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(task.object), stored); err != nil {
		t.Fatal(err)
	}
	task = seedTestTask(stored, task.client)
	if pending, err := task.cleanupSeedGenerationISOUpload(ctx); err != nil || !pending {
		t.Fatalf("expected retry after restart: %t %v", pending, err)
	}
	if !deadline.Equal(task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOUploadCleanupDeadline) {
		t.Fatal("restart reset the cleanup budget")
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["upload"]), &corev1.Secret{}); err != nil {
		t.Fatal(err)
	}
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.ISOUploadCleanupDeadline = &metav1.Time{Time: time.Now().Add(-time.Minute)}
	if err := task.client.Status().Update(ctx, task.object); err != nil {
		t.Fatal(err)
	}
	if pending, err := task.cleanupSeedGenerationISOUpload(ctx); err != nil || pending {
		t.Fatalf("cleanup budget did not expire: %t %v", pending, err)
	}
	status = task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if !status.ISOUploadCleanupCompleted || calls != 2 || !strings.Contains(seedGenerationCondition(task.object).Message, "manually") {
		t.Fatalf("cleanup expiry missing warning/checkpoint: %+v", status)
	}
	reconciler := &ProvisioningRequestReconciler{Client: task.client, Logger: task.logger}
	for i := 0; i < 3; i++ {
		if _, err := reconciler.cleanupSeedGenerationResources(ctx, task.object); err != nil {
			t.Fatal(err)
		}
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["upload"]), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expired cleanup retained credentials: %v", err)
	}
}

func TestSeedGenerationISOUploadCleanupPreservesPublishedISO(t *testing.T) {
	task, _ := seedGenerationISOCleanupFixture(t)
	ctlrutils.SetStatusCondition(&task.object.Status.Conditions, provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Completed, metav1.ConditionTrue, "Completed")
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	runSeedGenerationSSHCommand = func(context.Context, string, map[string][]byte, string) ([]byte, error) {
		t.Fatal("published ISO was targeted for cleanup")
		return nil, nil
	}
	if _, err := task.cleanupSeedGenerationISOResources(context.Background(), false, false); err != nil {
		t.Fatal(err)
	}
}

func TestSeedGenerationSSHPreflightServingPermissions(t *testing.T) {
	root := t.TempDir()
	artifact := &seedGenerationISOArtifact{RemoteDirectory: root + "/seedgen/run-123"}
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	runSeedGenerationSSHCommand = func(ctx context.Context, _ string, _ map[string][]byte, command string) ([]byte, error) {
		return exec.CommandContext(ctx, "/bin/bash", "-c", command).CombinedOutput()
	}
	if err := validateSeedGenerationSSHUploadAccess(context.Background(), "example.test", nil, artifact, "run-123"); err != nil {
		t.Fatal(err)
	}
	assertISOTestServingParent(t, root)
}

func assertISOTestServingParent(t *testing.T, root string) {
	t.Helper()
	info, err := os.Stat(root + "/seedgen")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("serving parent mode: %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(root + "/seedgen")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("preflight reserved a run directory or left a private probe: %v", entries)
	}
}

func TestSeedGenerationISOUploadCleanupPreservesUnownedDirectory(t *testing.T) {
	task, _ := seedGenerationISOCleanupFixture(t)
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	runSeedGenerationSSHCommand = func(context.Context, string, map[string][]byte, string) ([]byte, error) {
		return []byte("unowned"), nil
	}
	if pending, err := task.cleanupSeedGenerationISOUpload(context.Background()); err != nil || pending {
		t.Fatalf("unowned directory blocked cleanup: %t %v", pending, err)
	}
	if !task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOUploadCleanupCompleted || !strings.Contains(seedGenerationCondition(task.object).Message, "manually") {
		t.Fatal("missing manual cleanup warning")
	}
}

func seedGenerationISOUnpinnedCleanupFixture(t *testing.T) (*provisioningRequestReconcilerTask, *batchv1.Job, *corev1.Pod) {
	t.Helper()
	ctx := context.Background()
	task, _ := seedGenerationISOCleanupFixture(t)
	pr := task.object
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID = ""
	if err := task.client.Status().Update(ctx, pr); err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "iso-job-uid",
		Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISOJobKind),
		OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "iso-pod", Namespace: job.Namespace,
		Labels:          seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}}
	for _, object := range []client.Object{job, pod} {
		if err := task.client.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
	return task, job, pod
}

func TestSeedGenerationISOCleanupPersistsJobUIDBeforeInterruptedPodDeletion(t *testing.T) {
	ctx := context.Background()
	task, job, pod := seedGenerationISOUnpinnedCleanupFixture(t)
	base := task.client
	task.client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
			if _, isPod := object.(*corev1.Pod); isPod {
				return errors.New("connection lost after deleting Job")
			}
			stored := &provisioningv1alpha1.ProvisioningRequest{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(task.object), stored); err != nil {
				t.Fatal(err)
			}
			if stored.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID != string(job.UID) {
				t.Fatal("Job was deleted before its UID was persisted")
			}
			return c.Delete(ctx, object, opts...)
		},
	})
	if _, err := task.deleteSeedGenerationISOJobAndPods(ctx); err == nil {
		t.Fatal("Pod deletion did not exercise the interrupted cleanup path")
	}
	if err := base.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Job deletion was not completed: %v", err)
	}
	// The Job's orphan propagation removes its owner reference. Reload the PR
	// as after a manager restart, when the Job is no longer available to adopt.
	pod.OwnerReferences = nil
	if err := base.Update(ctx, pod); err != nil {
		t.Fatal(err)
	}
	stored := &provisioningv1alpha1.ProvisioningRequest{}
	if err := base.Get(ctx, client.ObjectKeyFromObject(task.object), stored); err != nil {
		t.Fatal(err)
	}
	task = seedTestTask(stored, base)
	if pending, err := task.deleteSeedGenerationISOJobAndPods(ctx); err != nil || !pending {
		t.Fatalf("cleanup could not resume after Job deletion: pending=%t err=%v", pending, err)
	}
	if pending, err := task.deleteSeedGenerationISOJobAndPods(ctx); err != nil || pending {
		t.Fatalf("cleanup did not complete: pending=%t err=%v", pending, err)
	}
}

func TestSeedGenerationISOCleanupWaitsForJobUIDPersistence(t *testing.T) {
	ctx := context.Background()
	task, job, pod := seedGenerationISOUnpinnedCleanupFixture(t)
	base := task.client
	task.client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
			return errors.New("status update unavailable")
		},
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			t.Fatal("resource deleted without a persisted Job UID")
			return nil
		},
	})
	if _, err := task.deleteSeedGenerationISOJobAndPods(ctx); err == nil {
		t.Fatal("status persistence failure was ignored")
	}
	if task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID != "" {
		t.Fatal("unsaved UID would bypass persistence on retry")
	}
	for _, object := range []client.Object{job, pod} {
		if err := base.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			t.Fatalf("resource was removed before UID persistence: %v", err)
		}
	}
	task.client = base
	if pending, err := task.deleteSeedGenerationISOJobAndPods(ctx); err != nil || !pending {
		t.Fatalf("cleanup could not retry UID persistence: pending=%t err=%v", pending, err)
	}
}
