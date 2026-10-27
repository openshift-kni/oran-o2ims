/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func seedGenerationISOPodLabels(
	pr *provisioningv1alpha1.ProvisioningRequest, job *batchv1.Job,
) map[string]string {
	labels := seedGenerationISOResourceLabels(pr, seedGenerationISOPodKind)
	labels[seedGenerationISOJobLabel] = job.Name
	labels[batchv1.JobNameLabel] = job.Name
	labels[batchv1.ControllerUidLabel] = string(job.UID)
	labels["job-name"] = job.Name
	labels["controller-uid"] = string(job.UID)
	return labels
}

func seedGenerationISOJobOwnerReference(job *batchv1.Job) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job",
		Name: job.Name, UID: job.UID, Controller: k8sptr.To(true),
	}
}

func TestValidateSeedGenerationISOPodOwner(t *testing.T) {
	pr, _, _ := seedGenerationISOJobFixture(t, "")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "job-uid",
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: job.Name + "-abcde", Namespace: job.Namespace,
		Labels:          seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}}
	if err := validateSeedGenerationISOPodOwner(pod, pr, job.Name, job.UID, false); err != nil {
		t.Fatalf("Job-owned Pod was rejected: %v", err)
	}
	for name, mutate := range map[string]func(*corev1.Pod){
		"wrong ProvisioningRequest UID": func(p *corev1.Pod) {
			p.Labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] = "other-pr"
		},
		"wrong Job UID label": func(p *corev1.Pod) {
			p.Labels[batchv1.ControllerUidLabel] = "other-job"
		},
		"wrong Job name label": func(p *corev1.Pod) {
			p.Labels["job-name"] = "other-job"
		},
		"wrong controller": func(p *corev1.Pod) {
			p.OwnerReferences[0].UID = "other-job"
		},
		"missing controller": func(p *corev1.Pod) {
			p.OwnerReferences[0].Controller = k8sptr.To(false)
		},
		"wrong namespace": func(p *corev1.Pod) {
			p.Namespace = "elsewhere"
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := pod.DeepCopy()
			mutate(changed)
			if err := validateSeedGenerationISOPodOwner(changed, pr, job.Name, job.UID, false); err == nil {
				t.Fatal("invalid Pod was accepted")
			}
		})
	}
	orphan := pod.DeepCopy()
	orphan.OwnerReferences = nil
	if err := validateSeedGenerationISOPodOwner(orphan, pr, job.Name, job.UID, false); err == nil {
		t.Fatal("active Job accepted an orphaned Pod")
	}
	if err := validateSeedGenerationISOPodOwner(orphan, pr, job.Name, job.UID, true); err != nil {
		t.Fatalf("cleanup rejected Pod orphaned by Job deletion: %v", err)
	}
	if err := validateSeedGenerationISOPodOwner(orphan, pr, job.Name, "other-job", true); err == nil {
		t.Fatal("cleanup accepted orphan from another Job UID")
	}
}

func TestSeedGenerationISOJobReadsJobOwnedPod(t *testing.T) {
	pr, _, _ := seedGenerationISOJobFixture(t, "")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "job-uid",
	}}
	url := "https://iso.example.test/seedgen/run/rhcos-ibi.iso"
	digest := "sha256:" + strings.Repeat("a", 64)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: job.Name + "-abcde", Namespace: job.Namespace,
		Labels:          seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: url + "\n" + digest}}}},
	}}
	task := seedTestTask(pr, newSeedTestClient(t, pr, pod))
	result, ready, err := task.seedGenerationISOJobResult(context.Background(), job, url)
	if err != nil || !ready || result.URL != url || result.Digest != digest {
		t.Fatalf("completed Job Pod result was not read: result=%+v ready=%t err=%v", result, ready, err)
	}
}

func TestSeedGenerationISOJobProgressReadsJobOwnedPod(t *testing.T) {
	ctx := context.Background()
	pr, _, _ := seedGenerationISOJobFixture(t, "")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "job-uid",
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: job.Name + "-abcde", Namespace: job.Namespace,
		Labels:          seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	task := seedTestTask(pr, newSeedTestClient(t, pr, pod))
	previous := readSeedGenerationISOPodLogs
	t.Cleanup(func() { readSeedGenerationISOPodLogs = previous })
	readSeedGenerationISOPodLogs = func(_ context.Context, namespace, name string) ([]byte, error) {
		if namespace != pod.Namespace || name != pod.Name {
			t.Fatalf("read logs from unexpected Pod %s/%s", namespace, name)
		}
		return []byte("SEEDGEN_STATUS=uploading-iso\n"), nil
	}
	message, err := task.seedGenerationISOJobProgress(ctx, job)
	if err != nil || message != "Uploading the live ISO to the configured server" {
		t.Fatalf("Job Pod progress was not read: message=%q err=%v", message, err)
	}
}

func TestSeedGenerationISOCleanupFindsOrphanedJobPod(t *testing.T) {
	ctx := context.Background()
	task, _ := seedGenerationISOCleanupFixture(t)
	pr := task.object
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "iso-job-uid",
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: job.Name + "-abcde", Namespace: job.Namespace,
		Labels: seedGenerationISOPodLabels(pr, job),
	}}
	if err := task.client.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if pending, err := task.deleteSeedGenerationISOJobAndPods(ctx); err != nil || !pending {
		t.Fatalf("orphaned Pod was not deleted: pending=%t err=%v", pending, err)
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphaned Pod remains: %v", err)
	}
	foreign := pod.DeepCopy()
	foreign.Name = job.Name + "-foreign"
	foreign.UID = ""
	foreign.ResourceVersion = ""
	foreign.Labels[batchv1.ControllerUidLabel] = "other-job"
	if err := task.client.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := task.deleteSeedGenerationISOJobAndPods(ctx); err == nil {
		t.Fatal("cleanup accepted a Pod with a different Job UID")
	}
	if err := task.client.Get(ctx, client.ObjectKeyFromObject(foreign), &corev1.Pod{}); err != nil {
		t.Fatalf("cleanup deleted a foreign Pod: %v", err)
	}
}

func TestSeedGenerationISOJobResultIgnoresFailedDuplicatePods(t *testing.T) {
	ctx := context.Background()
	pr, _, _ := seedGenerationISOJobFixture(t, "")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "job-uid",
	}}
	url := "https://iso.example.test/seedgen/run/rhcos-ibi.iso"
	digest := "sha256:" + strings.Repeat("a", 64)
	succeeded := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "a-succeeded", Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(pr, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: url + "\n" + digest}}}},
	}}
	failed := succeeded.DeepCopy()
	failed.Name, failed.Status.Phase = "z-failed", corev1.PodFailed
	failed.Status.ContainerStatuses[0].State.Terminated = &corev1.ContainerStateTerminated{ExitCode: 143, Message: "SEEDGEN_FAILURE=initializing"}
	task := seedTestTask(pr, newSeedTestClient(t, pr, succeeded, failed))
	result, ready, err := task.seedGenerationISOJobResult(ctx, job, url)
	if err != nil || !ready || result.URL != url || result.Digest != digest {
		t.Fatalf("failed duplicate obscured the successful result: result=%+v ready=%t err=%v", result, ready, err)
	}
	if err := task.client.Delete(ctx, succeeded); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := task.seedGenerationISOJobResult(ctx, job, url); err != nil || ready {
		t.Fatalf("failed Pod was accepted as a successful result: ready=%t err=%v", ready, err)
	}
}

func TestSeedGenerationISOJobResultRejectsConflictingSuccessfulResults(t *testing.T) {
	pr, _, _ := seedGenerationISOJobFixture(t, "")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: seedGenerationISOJobName(pr), Namespace: seedGenerationSnapshotNamespace(), UID: "job-uid",
	}}
	url := "https://iso.example.test/seedgen/run/rhcos-ibi.iso"
	var objects []client.Object
	for _, digest := range []string{"a", "b"} {
		objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "iso-" + digest, Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(pr, job),
			OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
		}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: url + "\nsha256:" + strings.Repeat(digest, 64)}}}},
		}})
	}
	task := seedTestTask(pr, newSeedTestClient(t, objects...))
	if _, _, err := task.seedGenerationISOJobResult(context.Background(), job, url); err == nil {
		t.Fatal("different results from successful Pods were accepted")
	}
}

func TestSeedGenerationISOJobCompletesDespiteFailedDuplicate(t *testing.T) {
	ctx := context.Background()
	task, snapshot := seedGenerationISOCleanupFixture(t)
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.TimeoutSeconds = 3600
	status.ReleaseImageDigest = "example.test/release@sha256:" + strings.Repeat("a", 64)
	artifact, err := seedGenerationISOArtifactForRun(task.object,
		snapshot.Document.SeedGeneration["liveISO"].(map[string]any), snapshot.Secrets["upload"])
	if err != nil {
		t.Fatal(err)
	}
	job, err := makeSeedGenerationISOJob(task.object, snapshot, artifact, status.ReleaseImageDigest, status.TimeoutSeconds)
	if err != nil {
		t.Fatal(err)
	}
	job.UID = "iso-job-uid"
	status.ISOJobSpecHash, err = seedGenerationISOJobSpecHashV1(job)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = batchv1.JobStatus{Succeeded: 1, Failed: 1}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "succeeded", Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(task.object, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: artifact.URL + "\nsha256:" + strings.Repeat("a", 64)}}}},
	}}
	for _, object := range []client.Object{job, pod} {
		if err := task.client.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := task.observeSeedGenerationISOJob(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	if !task.seedGenerationISOCompleted() || status.ISOURL != artifact.URL {
		t.Fatalf("failed duplicate prevented completion: condition=%+v", seedGenerationCondition(task.object))
	}
}
