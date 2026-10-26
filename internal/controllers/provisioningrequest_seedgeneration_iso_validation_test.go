/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSeedGenerationISOFrozenCredentialsRevalidatedAfterRotation(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	pr, snapshot, _ := seedGenerationISOJobFixture(t, "")
	liveISO := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	liveISO["urlBase"] = server.URL
	liveISO["releaseImage"] = "example.test/release@sha256:" + strings.Repeat("a", 64)
	liveISO["imageDigestSources"] = []any{}
	liveISO["uploadSecretRef"] = map[string]any{"name": "upload"}
	liveISO["pullSecretRef"] = map[string]any{"name": "pull"}
	template := &provisioningv1alpha1.ClusterTemplate{ObjectMeta: metav1.ObjectMeta{Name: "seed-template", Namespace: "templates"}}
	upload := snapshot.Secrets["upload"].DeepCopy()
	upload.ObjectMeta = metav1.ObjectMeta{Name: "upload", Namespace: template.Namespace}
	upload.Data["caCert"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	pull := snapshot.Secrets["pullSecret"].DeepCopy()
	pull.ObjectMeta = metav1.ObjectMeta{Name: "pull", Namespace: template.Namespace}
	c := newSeedTestClient(t, pr, upload, pull)
	if err := configv1.AddToScheme(c.Scheme()); err != nil {
		t.Fatal(err)
	}
	task := seedTestTask(pr, c)
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	calls := 0
	runSeedGenerationSSHCommand = func(_ context.Context, _ string, data map[string][]byte, _ string) ([]byte, error) {
		calls++
		if string(data["privateKey"]) != "ssh-private-key" {
			return nil, errors.New("rotated key is no longer authorized")
		}
		return nil, nil
	}
	if err := task.validateSeedGenerationISOUploadPreflight(ctx, template, snapshot.Document.SeedGeneration); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("initial preflight did not validate SSH credentials")
	}
	upload.Data["privateKey"] = []byte("rotated-key")
	if err := c.Update(ctx, upload); err != nil {
		t.Fatal(err)
	}
	credentials := map[string]map[string][]byte{}
	err := task.resolveISOInputs(ctx, template, liveISO, snapshot.Document, nil, credentials)
	if err == nil || !strings.Contains(err.Error(), "frozen live ISO upload credentials failed validation") || calls != 2 {
		t.Fatalf("frozen inputs bypassed new credential validation: calls=%d err=%v", calls, err)
	}
	if len(credentials) != 0 {
		t.Fatal("invalid rotated credentials were frozen")
	}
	if status := pr.Status.Extensions.ClusterDetails; status != nil && status.SeedGenerationStatus != nil && status.SeedGenerationStatus.DetachmentStarted {
		t.Fatal("validation failure detached the cluster")
	}
}

func TestSeedGenerationISOFailureStageSurvivesMissingPodLogs(t *testing.T) {
	ctx := context.Background()
	task, snapshot := seedGenerationISOCleanupFixture(t)
	task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobSubmitted = false
	task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID = "job-uid"
	if err := task.client.Status().Update(ctx, task.object); err != nil {
		t.Fatal(err)
	}
	pr := task.object
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: types.UID("job-uid")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "iso-builder", Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: "SEEDGEN_FAILURE=checking-https-digest", ExitCode: 1}},
	}}}}
	if err := task.client.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	previous := readSeedGenerationISOPodLogs
	t.Cleanup(func() { readSeedGenerationISOPodLogs = previous })
	readSeedGenerationISOPodLogs = func(context.Context, string, string) ([]byte, error) { return nil, errors.New("logs unavailable") }
	if _, err := task.failSeedGenerationISOJob(ctx, provisioningv1alpha1.CRconditionReasons.Failed, "Live ISO Job failed", job, snapshot); err != nil {
		t.Fatal(err)
	}
	condition := seedGenerationCondition(pr)
	if condition == nil || !strings.Contains(condition.Message, "HTTPS content SHA-256 checksum (checksum mismatch)") {
		t.Fatalf("missing failure detail: %+v", condition)
	}
	ref := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef
	if ref == nil {
		t.Fatal("stage diagnostics not retained without Pod logs")
	}
	retained := &corev1.ConfigMap{}
	if err := task.client.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, retained); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(retained.Data[ref.Key], "checksum mismatch") {
		t.Fatal("retained diagnostics lost the stage")
	}
	for _, input := range []string{"secret-token", "SEEDGEN_FAILURE=secret-token", "SEEDGEN_FAILURE=building-iso\nsecret-token"} {
		if got := seedGenerationISOFailureMessage(input); got != "" {
			t.Fatalf("arbitrary termination text was trusted: %s", got)
		}
	}
}
