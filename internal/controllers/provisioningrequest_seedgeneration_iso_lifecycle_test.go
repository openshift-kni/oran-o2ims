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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func seedGenerationISOObservationFixture(t *testing.T) (
	*provisioningRequestReconcilerTask, *seedGenerationInputSnapshot, *seedGenerationISOArtifact, *batchv1.Job,
) {
	t.Helper()
	task, snapshot := seedGenerationISOCleanupFixture(t)
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	task.seedGenerationISONow = func() time.Time { return now }
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	started, submitted := metav1.NewTime(now.Add(-time.Minute)), metav1.NewTime(now.Add(-time.Second))
	status.StartedAt, status.TimeoutSeconds, status.ISOJobSubmittedAt = &started, 3600, &submitted
	status.DetachmentStarted, status.AgentDetachmentCompleted, status.SeedGeneratorFinished = true, true, true
	status.SeedImage = snapshot.Document.SeedGeneration["seedImage"].(string)
	status.ReleaseImageDigest = "example.test/release@sha256:" + strings.Repeat("a", 64)
	setSeedGenerationProgress(task.object, provisioningv1alpha1.CRconditionReasons.BuildingISO, "Waiting for the live ISO build Job")
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
	if err := task.client.Status().Update(context.Background(), task.object); err != nil {
		t.Fatal(err)
	}
	claim, err := makeSeedGenerationISOPVC(task.object, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.client.Create(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	previous := runSeedGenerationSSHCommand
	t.Cleanup(func() { runSeedGenerationSSHCommand = previous })
	runSeedGenerationSSHCommand = func(context.Context, string, map[string][]byte, string) ([]byte, error) {
		return []byte("removed"), nil
	}
	return task, snapshot, artifact, job
}

func TestSeedGenerationISOMissingSubmittedJobIsNeverRecreated(t *testing.T) {
	for _, tc := range []struct {
		name      string
		age       time.Duration
		missingAt bool
		wantFail  bool
	}{
		{name: "inside adoption window", age: seedGenerationISOJobAdoptionWindow - time.Second},
		{name: "at adoption deadline", age: seedGenerationISOJobAdoptionWindow, wantFail: true},
		{name: "after adoption deadline", age: seedGenerationISOJobAdoptionWindow + time.Second, wantFail: true},
		{name: "missing submission timestamp", missingAt: true, wantFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := NewWithT(t), context.Background()
			task, snapshot, _, _ := seedGenerationISOObservationFixture(t)
			now := task.seedGenerationISOCurrentTime()
			status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
			submitted := metav1.NewTime(now.Add(-tc.age))
			status.ISOJobSubmittedAt = &submitted
			if tc.missingAt {
				status.ISOJobSubmittedAt = nil
			}
			g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
			creates := 0
			task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
					if _, isJob := object.(*batchv1.Job); isJob {
						creates++
					}
					return c.Create(ctx, object, opts...)
				},
			})
			result, err := task.reconcileSeedGenerationISO(ctx, snapshot)
			g.Expect(err).ToNot(HaveOccurred())
			persisted := &provisioningv1alpha1.ProvisioningRequest{}
			g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
			if tc.wantFail {
				g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Failed)))
				g.Expect(seedGenerationCondition(persisted).Message).To(ContainSubstring("will not be resubmitted"))
			} else {
				g.Expect(seedGenerationTerminal(persisted)).To(BeFalse())
				g.Expect(result.RequeueAfter).To(Equal(requeueWithShortInterval().RequeueAfter))
			}
			// Resume with the persisted checkpoint, as after a manager restart.
			restarted := seedTestTask(persisted, task.client)
			restarted.seedGenerationISONow = func() time.Time { return now }
			if tc.wantFail {
				_, err = restarted.reconcileSeedGeneration(ctx)
			} else {
				_, err = restarted.reconcileSeedGenerationISO(ctx, snapshot)
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(creates).To(BeZero())
			jobs := &batchv1.JobList{}
			g.Expect(task.client.List(ctx, jobs)).To(Succeed())
			g.Expect(jobs.Items).To(BeEmpty())
		})
	}
}

func TestSeedGenerationISORejectsMismatchedJobs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*provisioningv1alpha1.SeedGenerationStatus, *batchv1.Job)
		message    string
		cleanupErr bool
	}{
		{name: "different Job UID", message: "Job UID does not match", cleanupErr: true,
			mutate: func(_ *provisioningv1alpha1.SeedGenerationStatus, job *batchv1.Job) { job.UID = "replacement-job" }},
		{name: "no submission intent", message: "without persisted submission intent",
			mutate: func(status *provisioningv1alpha1.SeedGenerationStatus, _ *batchv1.Job) {
				status.ISOJobSubmitted = false
			}},
		{name: "changed frozen inputs", message: "does not match frozen inputs",
			mutate: func(_ *provisioningv1alpha1.SeedGenerationStatus, job *batchv1.Job) {
				job.Spec.Template.Spec.Containers[0].Image = "example.test/other-tools:v1"
			}},
		{name: "changed script", message: "fingerprint does not match",
			mutate: func(_ *provisioningv1alpha1.SeedGenerationStatus, job *batchv1.Job) {
				job.Spec.Template.Spec.Containers[0].Args[0] += "\necho modified\n"
			}},
		{name: "missing fingerprint", message: "fingerprint is missing",
			mutate: func(status *provisioningv1alpha1.SeedGenerationStatus, _ *batchv1.Job) { status.ISOJobSpecHash = "" }},
		{name: "unsupported fingerprint version", message: "fingerprint version is unsupported",
			mutate: func(status *provisioningv1alpha1.SeedGenerationStatus, _ *batchv1.Job) {
				status.ISOJobSpecHash = "v2:sha256:" + strings.Repeat("a", 64)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := NewWithT(t), context.Background()
			task, _, artifact, job := seedGenerationISOObservationFixture(t)
			tc.mutate(task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus, job)
			g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
			g.Expect(task.client.Create(ctx, job)).To(Succeed())
			creates := 0
			task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
					if _, isJob := object.(*batchv1.Job); isJob {
						creates++
					}
					return c.Create(ctx, object, opts...)
				},
			})
			_, err := task.observeSeedGenerationISOJob(ctx, artifact)
			if tc.cleanupErr {
				g.Expect(err).To(MatchError(ContainSubstring(tc.message)))
				g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).To(Succeed())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			persisted := &provisioningv1alpha1.ProvisioningRequest{}
			g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
			g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Failed)))
			g.Expect(seedGenerationCondition(persisted).Message).To(ContainSubstring(tc.message))
			g.Expect(creates).To(BeZero())
		})
	}
}

func TestSeedGenerationISOJobCreateRejectionRetriesOnlyDefiniteRejections(t *testing.T) {
	jobResource := schema.GroupResource{Group: batchv1.GroupName, Resource: "jobs"}
	for _, tc := range []struct {
		name     string
		err      error
		canRetry bool
	}{
		{name: "invalid", err: apierrors.NewInvalid(schema.GroupKind{Group: batchv1.GroupName, Kind: "Job"}, "job",
			field.ErrorList{field.Invalid(field.NewPath("spec"), nil, "rejected")}), canRetry: true},
		{name: "forbidden", err: apierrors.NewForbidden(jobResource, "job", nil), canRetry: true},
		{name: "bad request", err: apierrors.NewBadRequest("rejected"), canRetry: true},
		{name: "throttled", err: apierrors.NewTooManyRequests("retry later", 1), canRetry: true},
		{name: "ambiguous timeout", err: apierrors.NewTimeoutError("response lost", 1)},
		{name: "ambiguous service unavailable", err: apierrors.NewServiceUnavailable("response lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := NewWithT(t), context.Background()
			task, snapshot, _, _ := seedGenerationISOObservationFixture(t)
			status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
			status.ISOJobSubmitted, status.ISOJobSubmittedAt, status.ISOJobUID = false, nil, ""
			g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
			creates := 0
			task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
					if _, isJob := object.(*batchv1.Job); isJob {
						creates++
						if creates == 1 {
							return tc.err
						}
					}
					return c.Create(ctx, object, opts...)
				},
			})
			_, err := task.reconcileSeedGenerationISO(ctx, snapshot)
			g.Expect(err).To(MatchError(ContainSubstring("failed to create live ISO build Job")))
			persisted := &provisioningv1alpha1.ProvisioningRequest{}
			g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
			checkpoint := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus
			g.Expect(checkpoint.ISOJobSubmitted).To(Equal(!tc.canRetry))
			g.Expect(checkpoint.ISOJobSubmittedAt == nil).To(Equal(tc.canRetry))
			g.Expect(checkpoint.ISOJobSpecHash == "").To(Equal(tc.canRetry))
			g.Expect(seedGenerationTerminal(persisted)).To(BeFalse())
			restarted := seedTestTask(persisted, task.client)
			restarted.seedGenerationISONow = task.seedGenerationISONow
			_, err = restarted.reconcileSeedGenerationISO(ctx, snapshot)
			g.Expect(err).ToNot(HaveOccurred())
			job := &batchv1.Job{}
			err = task.client.Get(ctx, client.ObjectKey{Name: seedGenerationISOJobName(persisted), Namespace: seedGenerationSnapshotNamespace()}, job)
			if tc.canRetry {
				g.Expect(creates).To(Equal(2))
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(creates).To(Equal(1))
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})
	}
}

func TestSeedGenerationISOJobAlreadyExistsIsAdopted(t *testing.T) {
	g, ctx := NewWithT(t), context.Background()
	task, snapshot, _, job := seedGenerationISOObservationFixture(t)
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.ISOJobSubmitted, status.ISOJobSubmittedAt, status.ISOJobUID = false, nil, ""
	g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
	g.Expect(task.client.Create(ctx, job)).To(Succeed())
	_, err := task.reconcileSeedGenerationISO(ctx, snapshot)
	g.Expect(err).ToNot(HaveOccurred())
	_, err = task.reconcileSeedGenerationISO(ctx, snapshot)
	g.Expect(err).ToNot(HaveOccurred())
	persisted := &provisioningv1alpha1.ProvisioningRequest{}
	g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
	g.Expect(persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID).To(Equal(string(job.UID)))
}

func TestSeedGenerationISOJobCreationPinsUID(t *testing.T) {
	g, ctx := NewWithT(t), context.Background()
	task, snapshot, _, _ := seedGenerationISOObservationFixture(t)
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.ISOJobSubmitted, status.ISOJobSubmittedAt, status.ISOJobUID = false, nil, ""
	g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
	task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
			if job, isJob := object.(*batchv1.Job); isJob {
				persisted := &provisioningv1alpha1.ProvisioningRequest{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
				checkpoint := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus
				g.Expect(checkpoint.ISOJobSubmitted).To(BeTrue())
				jobHash, err := seedGenerationISOJobSpecHashV1(job)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(checkpoint.ISOJobSpecHash).To(Equal(jobHash), "fingerprint must be durable before Job creation")
				object.SetUID("created-job-uid")
			}
			return c.Create(ctx, object, opts...)
		},
	})
	_, err := task.reconcileSeedGenerationISO(ctx, snapshot)
	g.Expect(err).ToNot(HaveOccurred())
	persisted := &provisioningv1alpha1.ProvisioningRequest{}
	g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
	checkpoint := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus
	g.Expect(checkpoint.ISOJobSubmitted).To(BeTrue())
	g.Expect(checkpoint.ISOJobUID).To(Equal("created-job-uid"))
}
