/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSeedGenerationISOJobSurvivesControllerTemplateChanges(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		for name, change := range map[string]func(*batchv1.Job){
			"previous embedded script": func(job *batchv1.Job) {
				job.Spec.Template.Spec.Containers[0].Args[0] += "\n# script from the previous operator\n"
			},
			"previous temporary volume default": func(job *batchv1.Job) {
				for i := range job.Spec.Template.Spec.Volumes {
					volume := &job.Spec.Template.Spec.Volumes[i]
					if volume.Name == controllerTempVolumeName {
						volume.EmptyDir.SizeLimit = k8sptr.To(resource.MustParse("32Mi"))
					}
				}
			},
		} {
			adoption := "before UID checkpoint/"
			if pinned {
				adoption = "after UID checkpoint/"
			}
			t.Run(adoption+name, func(t *testing.T) {
				g, ctx := NewWithT(t), context.Background()
				task, snapshot, artifact, job := seedGenerationISOObservationFixture(t)
				change(job)
				job.Status.Active = 1
				originalHash, err := seedGenerationISOJobSpecHashV1(job)
				g.Expect(err).ToNot(HaveOccurred())
				status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
				status.ISOJobSpecHash = originalHash
				if !pinned {
					status.ISOJobUID = ""
				}
				g.Expect(task.client.Status().Update(ctx, task.object)).To(Succeed())
				g.Expect(task.client.Create(ctx, job)).To(Succeed())
				current, err := makeSeedGenerationISOJob(task.object, snapshot, artifact, status.ReleaseImageDigest, status.TimeoutSeconds)
				g.Expect(err).ToNot(HaveOccurred())
				currentHash, err := seedGenerationISOJobSpecHashV1(current)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(currentHash).ToNot(Equal(originalHash), "the test must represent different controller templates")

				creates := 0
				task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
						if _, isJob := object.(*batchv1.Job); isJob {
							creates++
						}
						return c.Create(ctx, object, opts...)
					},
				})
				for i := 0; i < 2; i++ {
					persisted := &provisioningv1alpha1.ProvisioningRequest{}
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					restarted := seedTestTask(persisted, task.client)
					_, err = restarted.reconcileSeedGenerationISO(ctx, snapshot)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(seedGenerationTerminal(persisted)).To(BeFalse())
					g.Expect(persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobUID).To(Equal(string(job.UID)))
				}

				job.Status = batchv1.JobStatus{Succeeded: 1}
				g.Expect(task.client.Status().Update(ctx, job)).To(Succeed())
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name: "original-builder", Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(task.object, job),
					OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
				}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
					ContainerStatuses: []corev1.ContainerStatus{{Name: seedGenerationISOContainer,
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
							Message: artifact.URL + "\nsha256:" + strings.Repeat("a", 64),
						}}}},
				}}
				g.Expect(task.client.Create(ctx, pod)).To(Succeed())
				persisted := &provisioningv1alpha1.ProvisioningRequest{}
				g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
				restarted := seedTestTask(persisted, task.client)
				_, err = restarted.reconcileSeedGenerationISO(ctx, snapshot)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(restarted.seedGenerationISOCompleted()).To(BeTrue())
				g.Expect(persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobSpecHash).To(Equal(originalHash))
				g.Expect(creates).To(BeZero())
			})
		}
	}
}

func TestSeedGenerationISOJobFingerprintV1Compatibility(t *testing.T) {
	// Pin the persisted v1 encoding using a script independent of today's
	// embedded builder. Changing the fingerprint format needs a new version.
	job := &batchv1.Job{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: k8sptr.To(false),
		Containers: []corev1.Container{{Name: "iso-builder", Image: "example.test/tools:v1", Args: []string{"echo original"}}},
	}}}}
	actual, err := seedGenerationISOJobSpecHashV1(job)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "v1:sha256:a293b3e18412fa4a36196247fbe2b90b31f3e1f971496a121ed43ad9c1060824"
	if actual != expected {
		t.Fatalf("persisted v1 fingerprint changed: got %s, want %s", actual, expected)
	}
}

func TestSeedGenerationISOJobFingerprintRejectsMissingContainer(t *testing.T) {
	for _, job := range []*batchv1.Job{nil, {}, {Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "one"}, {Name: "two"}}},
	}}}} {
		if _, err := seedGenerationISOJobSpecHashV1(job); err == nil {
			t.Fatal("invalid container count produced a Job fingerprint")
		}
	}
}
