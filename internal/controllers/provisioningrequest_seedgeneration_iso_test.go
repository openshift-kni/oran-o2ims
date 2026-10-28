/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func seedGenerationISOJobFixture(t *testing.T, baseISOURL string) (*provisioningv1alpha1.ProvisioningRequest, *seedGenerationInputSnapshot, *seedGenerationISOArtifact) {
	t.Helper()
	pr, snapshot := seedGenerationISOInputFixture()
	snapshot.Document.ToolsImage = "registry.example.test/ose-tools-rhel9:latest"
	snapshot.Document.RegistryTrustedCAKeys = map[string]string{"registry.example.test": "registry-ca-0"}
	snapshot.Document.SeedGeneration["liveISO"].(map[string]any)["storageClass"] = "fast"
	if baseISOURL != "" {
		snapshot.Document.SeedGeneration["liveISO"].(map[string]any)["baseISOURL"] = baseISOURL
	}
	snapshot.ConfigMap = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "request-config", Namespace: "operator"}}
	snapshot.Secrets["pullSecret"].ObjectMeta = metav1.ObjectMeta{Name: "request-pull", Namespace: "operator"}
	snapshot.Secrets["pullSecret"].Data[seedGenerationISOConfigKey] = []byte("installer config")
	snapshot.Secrets["upload"].ObjectMeta = metav1.ObjectMeta{Name: "request-upload", Namespace: "operator"}
	snapshot.Secrets["upload"].Data["privateKey"] = []byte("ssh-private-key")
	snapshot.Secrets["upload"].Data["knownHosts"] = []byte("iso.example.test ssh-ed25519 AAAA")
	artifact, err := seedGenerationISOArtifactForRun(pr,
		snapshot.Document.SeedGeneration["liveISO"].(map[string]any), snapshot.Secrets["upload"])
	if err != nil {
		t.Fatalf("failed to create ISO artifact fixture: %v", err)
	}
	return pr, snapshot, artifact
}

func TestMakeSeedGenerationISOJob(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseISOURL string
		wantEnv    string
	}{
		{name: "without base ISO override"},
		{name: "with base ISO override", baseISOURL: "https://mirror.example.test/rhcos-live.iso", wantEnv: "OPENSHIFT_INSTALL_OS_IMAGE_OVERRIDE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pr, snapshot, artifact := seedGenerationISOJobFixture(t, tc.baseISOURL)
			job, err := makeSeedGenerationISOJob(pr, snapshot, artifact,
				"quay.io/openshift-release-dev/ocp-release@sha256:"+strings.Repeat("a", 64), 3600)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(job.Namespace).To(Equal(seedGenerationSnapshotNamespace()))
			g.Expect(job.Labels[seedGenerationISOResourceKindLabel]).To(Equal(seedGenerationISOJobKind))
			g.Expect(job.Spec.Template.Labels[seedGenerationISOResourceKindLabel]).To(Equal(seedGenerationISOPodKind))
			g.Expect(job.Spec.Template.Labels[seedGenerationISOJobLabel]).To(Equal(job.Name))
			g.Expect(job.Spec.BackoffLimit).ToNot(BeNil())
			g.Expect(*job.Spec.BackoffLimit).To(BeZero())
			g.Expect(job.Spec.ActiveDeadlineSeconds).ToNot(BeNil())
			g.Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(int64(3600)))
			g.Expect(job.Spec.Template.Spec.AutomountServiceAccountToken).ToNot(BeNil())
			g.Expect(*job.Spec.Template.Spec.AutomountServiceAccountToken).To(BeFalse())
			g.Expect(job.Spec.Template.Spec.SecurityContext.RunAsNonRoot).ToNot(BeNil())
			g.Expect(*job.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
			g.Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := job.Spec.Template.Spec.Containers[0]
			g.Expect(container.Image).To(Equal(snapshot.Document.ToolsImage))
			g.Expect(container.SecurityContext.AllowPrivilegeEscalation).ToNot(BeNil())
			g.Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			g.Expect(container.SecurityContext.ReadOnlyRootFilesystem).ToNot(BeNil())
			g.Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			g.Expect(container.Command).To(Equal([]string{"/bin/bash", "-c"}))

			hasOverride := false
			for _, env := range container.Env {
				if env.Name == "OPENSHIFT_INSTALL_OS_IMAGE_OVERRIDE" {
					hasOverride = true
					g.Expect(env.Value).To(Equal(tc.baseISOURL))
				}
			}
			g.Expect(hasOverride).To(Equal(tc.wantEnv != ""))
			g.Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "ISO_URL", Value: artifact.URL}))
			g.Expect(container.Env).To(ContainElements(
				corev1.EnvVar{Name: "TMPDIR", Value: "/workspace/tmp"},
				corev1.EnvVar{Name: "XDG_CACHE_HOME", Value: "/workspace/cache"},
			))
			g.Expect(container.Env).ToNot(ContainElement(corev1.EnvVar{Name: "ISO_RELEASE_IMAGE", Value: "quay.io/openshift-release-dev/ocp-release:mutable"}))

			volumeNames := make([]string, 0, len(job.Spec.Template.Spec.Volumes))
			for _, volume := range job.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, volume.Name)
				if volume.Name == controllerTempVolumeName {
					g.Expect(volume.EmptyDir).ToNot(BeNil())
					g.Expect(volume.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory))
					g.Expect(volume.EmptyDir.SizeLimit).ToNot(BeNil())
					g.Expect(volume.EmptyDir.SizeLimit.Value()).To(Equal(int64(64 << 20)))
				}
			}
			g.Expect(volumeNames).To(ContainElements("workspace", "installer-config", "upload-credentials", "mirror-config", "registry-certs", "tmp"))
			g.Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"}))
		})
	}
}

func TestValidateSeedGenerationISOJobRejectsUnsafeChanges(t *testing.T) {
	g := NewWithT(t)
	pr, snapshot, artifact := seedGenerationISOJobFixture(t, "")
	digest := "quay.io/openshift-release-dev/ocp-release@sha256:" + strings.Repeat("b", 64)
	expected, err := makeSeedGenerationISOJob(pr, snapshot, artifact, digest, 3600)
	g.Expect(err).ToNot(HaveOccurred())
	jobHash, err := seedGenerationISOJobSpecHashV1(expected)
	g.Expect(err).ToNot(HaveOccurred())
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{TimeoutSeconds: 3600, ISOJobSpecHash: jobHash},
	}
	g.Expect(validateSeedGenerationISOJob(expected, pr)).To(Succeed())

	cases := []struct {
		name   string
		mutate func(*batchv1.Job)
	}{
		{name: "host network", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.HostNetwork = true }},
		{name: "additional init container", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "injected", Image: "example.test/injected"}}
		}},
		{name: "service account token", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.AutomountServiceAccountToken = nil }},
		{name: "pod security context", mutate: func(job *batchv1.Job) { job.Spec.Template.Spec.SecurityContext.RunAsNonRoot = nil }},
		{name: "container privilege escalation", mutate: func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation = nil
		}},
		{name: "retry enabled", mutate: func(job *batchv1.Job) { value := int32(1); job.Spec.BackoffLimit = &value }},
		{name: "extended deadline", mutate: func(job *batchv1.Job) { value := int64(3601); job.Spec.ActiveDeadlineSeconds = &value }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			actual := expected.DeepCopy()
			tc.mutate(actual)
			g.Expect(validateSeedGenerationISOJob(actual, pr)).To(HaveOccurred())
		})
	}
}

func TestRedactSeedGenerationISODiagnostics(t *testing.T) {
	snapshot := &seedGenerationInputSnapshot{Secrets: map[string]*corev1.Secret{
		"pullSecret": {Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(`{"auth":"pull-secret-value"}`),
		}},
		"upload": {Data: map[string][]byte{"privateKey": []byte("ssh-private-key-value")}},
	}}
	logs := []byte("starting ISO build\n\x1b[31mpassword=plain-password\x1b[0m\n" +
		"Authorization: Bearer bearer-token-value\n" +
		"upload failed for https://user:password@iso.example.test/path\n" +
		"pull-secret-value\nssh-private-key-value\n")
	redacted := string(redactSeedGenerationISODiagnostics(logs, snapshot))
	for _, secret := range []string{
		"plain-password", "bearer-token-value", "user:password", "pull-secret-value", "ssh-private-key-value",
	} {
		if strings.Contains(redacted, secret) {
			t.Errorf("redacted diagnostics contain secret %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "starting ISO build") {
		t.Fatalf("redaction removed useful log context: %s", redacted)
	}
	if strings.ContainsRune(redacted, '\x1b') {
		t.Fatalf("redacted diagnostics retained terminal control characters: %q", redacted)
	}
}

func TestSeedGenerationISOFailureRetainsRedactedDiagnosticsAndDeletesWorkspace(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-request", UID: types.UID("generation-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
			DetachmentStarted: true, DetachedCleanupCompleted: true, ISOJobUID: "iso-job-uid",
		},
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: types.UID("iso-job-uid"),
		Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISOJobKind),
		OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "iso-builder-pod", Namespace: job.Namespace, UID: types.UID("iso-pod-uid"),
		Labels:          seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOPVCName(pr), Namespace: job.Namespace,
		Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISOPVCKind),
		OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	c := newSeedTestClient(t, pr, job, pod, pvc)
	task := seedTestTask(pr, c)
	snapshot := &seedGenerationInputSnapshot{Secrets: map[string]*corev1.Secret{
		"pullSecret": {Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte("registry-secret-value")}},
	}}
	previousReader := readSeedGenerationISOPodLogs
	readSeedGenerationISOPodLogs = func(_ context.Context, namespace, podName string) ([]byte, error) {
		if namespace != job.Namespace || podName != pod.Name {
			t.Fatalf("unexpected log request for %s/%s", namespace, podName)
		}
		return []byte("starting ISO build\nregistry secret: registry-secret-value\ninstall failed\n"), nil
	}
	t.Cleanup(func() { readSeedGenerationISOPodLogs = previousReader })

	result, err := task.failSeedGenerationISOJob(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
		"Live ISO build Job failed", job, snapshot)
	if err != nil {
		t.Fatalf("failed to record ISO build failure: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected cleanup requeue, got %+v", result)
	}
	status := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status.ISOFailureDiagnosticsExpiresAt == nil ||
		status.ISOFailureDiagnosticsExpiresAt.Before(&metav1.Time{Time: time.Now().Add(23 * time.Hour)}) {
		t.Fatalf("failure diagnostics do not have the expected retention expiry: %+v", status.ISOFailureDiagnosticsExpiresAt)
	}
	if status.ISOFailureDiagnosticsRef == nil {
		t.Fatal("failed run did not persist a diagnostics ConfigMap reference")
	}
	configMap := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: status.ISOFailureDiagnosticsRef.Name, Namespace: status.ISOFailureDiagnosticsRef.Namespace}
	if err := c.Get(ctx, key, configMap); err != nil {
		t.Fatalf("failed to retrieve retained diagnostics: %v", err)
	}
	if got := configMap.Data[seedGenerationISODiagnosticsKey]; strings.Contains(got, "registry-secret-value") ||
		!strings.Contains(got, "starting ISO build") {
		t.Fatalf("unexpected retained diagnostic contents: %s", got)
	}
	condition := seedGenerationCondition(pr)
	if condition == nil || strings.Contains(condition.Message, "registry-secret-value") ||
		!strings.Contains(condition.Message, configMap.Name) || !strings.Contains(condition.Message, "Redacted log tail") {
		t.Fatalf("failure condition does not safely point to diagnostics: %+v", condition)
	}
	for i := 0; i < 3; i++ {
		if _, err := task.cleanupSeedGenerationRunResources(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("failed ISO workspace PVC was not deleted: %v", err)
	}
}

func TestSeedGenerationISOFailureArtifactsExpire(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-request", UID: types.UID("generation-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
			ISOFailureDiagnosticsExpiresAt: &metav1.Time{Time: time.Now().Add(-time.Minute)},
		},
	}
	diagnostics := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISODiagnosticsName(pr), Namespace: seedGenerationSnapshotNamespace(),
		Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISODiagnosticsKind),
		OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOPVCName(pr), Namespace: seedGenerationSnapshotNamespace(),
		Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISOPVCKind),
		OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
	}}
	c := newSeedTestClient(t, diagnostics, pvc)
	task := seedTestTask(pr, c)
	pending, err := task.cleanupSeedGenerationISOResources(ctx, false, true)
	if err != nil || !pending {
		t.Fatalf("expected expired artifacts to be deleted, pending=%t err=%v", pending, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: diagnostics.Name, Namespace: diagnostics.Namespace}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Errorf("expired diagnostics ConfigMap was not deleted: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Errorf("expired workspace PVC was not deleted: %v", err)
	}
	if pending, err = task.cleanupSeedGenerationISOResources(ctx, false, true); err != nil || pending {
		t.Fatalf("expired artifact cleanup did not converge, pending=%t err=%v", pending, err)
	}
}

func TestParseSeedGenerationISOJobResult(t *testing.T) {
	g := NewWithT(t)
	url := "https://iso.example.test/seedgen/run-123/rhcos-ibi.iso"
	digest := "sha256:" + strings.Repeat("c", 64)
	result, err := parseSeedGenerationISOJobResult(url+"\n"+digest, url)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(Equal(seedGenerationISOResult{URL: url, Digest: digest}))
	for _, invalid := range []string{
		url + "\nsha256:invalid",
		"https://attacker.example.test/image.iso\n" + digest,
		url + "\n" + digest + "\nextra",
	} {
		_, err := parseSeedGenerationISOJobResult(invalid, url)
		g.Expect(err).To(HaveOccurred())
	}
}

func TestSeedGenerationISOJobScriptVerifiesPublishedBytes(t *testing.T) {
	g := NewWithT(t)
	script := seedGenerationISOJobScript()
	for _, required := range []string{
		"StrictHostKeyChecking=yes",
		"sha256sum -- '$REMOTE_TMP'",
		"if [[ \"$REMOTE_DIGEST\" != \"$LOCAL_DIGEST\" ]]",
		"curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \"$ISO_URL\"",
		"if [[ \"$HTTPS_DIGEST\" != \"$LOCAL_DIGEST\" ]]",
	} {
		g.Expect(script).To(ContainSubstring(required))
	}
}

func TestSeedGenerationISOProgressUsesAllowlistedMessages(t *testing.T) {
	g := NewWithT(t)
	logs := []byte("private output\nSEEDGEN_STATUS=building-iso\nsecret=never expose\nSEEDGEN_STATUS=verifying-https\n")
	g.Expect(seedGenerationISOProgressMessage(logs)).To(Equal("Verifying the published ISO bytes over HTTPS"))
	g.Expect(seedGenerationISOProgressMessage([]byte("password=secret\n"))).To(BeEmpty())
	g.Expect(seedGenerationISOJobName(&provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "pr", UID: types.UID("uid"),
	}})).To(Equal("pr-seedgen-iso-build"))
}
