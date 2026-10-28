/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSeedGenerationISOTimeoutRetainsOnlyOwnedJobDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name        string
		missingJob  bool
		missingSnap bool
		wrongUID    bool
		wrongOwner  bool
		wantLogs    bool
	}{
		{name: "owned Job and snapshot", wantLogs: true},
		{name: "missing Job", missingJob: true},
		{name: "missing snapshot", missingSnap: true},
		{name: "different Job UID", wrongUID: true},
		{name: "different Job owner", wrongOwner: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := NewWithT(t), context.Background()
			task, snapshot, _, job := seedGenerationISOObservationFixture(t)
			if tc.wrongUID {
				job.UID = "replacement-job"
			}
			if tc.wrongOwner {
				job.Labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] = "other-request"
			}
			if !tc.missingJob {
				g.Expect(task.client.Create(ctx, job)).To(Succeed())
			}
			if tc.missingSnap {
				g.Expect(task.client.Delete(ctx, snapshot.ConfigMap)).To(Succeed())
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "iso-builder", Namespace: job.Namespace,
				Labels: seedGenerationISOPodLabels(task.object, job), OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
			}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			if tc.wantLogs {
				g.Expect(task.client.Create(ctx, pod)).To(Succeed())
			}
			reads := 0
			previous := readSeedGenerationISOPodLogs
			t.Cleanup(func() { readSeedGenerationISOPodLogs = previous })
			readSeedGenerationISOPodLogs = func(_ context.Context, namespace, name string) ([]byte, error) {
				reads++
				g.Expect(namespace).To(Equal(pod.Namespace))
				g.Expect(name).To(Equal(pod.Name))
				return append([]byte("useful timeout diagnostic\n"), snapshot.Secrets["upload"].Data["privateKey"]...), nil
			}
			message := "Seed generation timed out while building the live ISO"
			_, err := task.failSeedGenerationISOTimeout(ctx, message)
			if tc.wrongUID {
				// A replacement Job cannot supply logs or authorize deletion.
				g.Expect(err).To(MatchError(ContainSubstring("Job UID does not match")))
				g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).To(Succeed())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			if tc.wrongOwner {
				g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).To(Succeed())
			}
			persisted := &provisioningv1alpha1.ProvisioningRequest{}
			g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
			condition := seedGenerationCondition(persisted)
			g.Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
			g.Expect(condition.Message).To(ContainSubstring(message))
			g.Expect(condition.Message).ToNot(ContainSubstring(string(snapshot.Secrets["upload"].Data["privateKey"])))
			ref := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef
			if tc.wantLogs {
				g.Expect(reads).To(Equal(1))
				g.Expect(ref).ToNot(BeNil())
				retained := &corev1.ConfigMap{}
				g.Expect(task.client.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, retained)).To(Succeed())
				g.Expect(retained.Data[ref.Key]).To(ContainSubstring("useful timeout diagnostic"))
				g.Expect(retained.Data[ref.Key]).ToNot(ContainSubstring(string(snapshot.Secrets["upload"].Data["privateKey"])))
			} else {
				g.Expect(reads).To(BeZero())
				g.Expect(ref).To(BeNil())
			}
		})
	}
}

func seedGenerationISOFailureFixture(t *testing.T) (
	*provisioningRequestReconcilerTask, *seedGenerationISOArtifact, *batchv1.Job, *corev1.Pod,
) {
	t.Helper()
	g, ctx := NewWithT(t), context.Background()
	task, snapshot, artifact, job := seedGenerationISOObservationFixture(t)
	job.Status = batchv1.JobStatus{Failed: 1}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "failed-builder", Namespace: job.Namespace, Labels: seedGenerationISOPodLabels(task.object, job),
		OwnerReferences: []metav1.OwnerReference{seedGenerationISOJobOwnerReference(job)},
	}, Status: corev1.PodStatus{Phase: corev1.PodFailed}}
	g.Expect(task.client.Create(ctx, job)).To(Succeed())
	g.Expect(task.client.Create(ctx, pod)).To(Succeed())
	previous := readSeedGenerationISOPodLogs
	t.Cleanup(func() { readSeedGenerationISOPodLogs = previous })
	readSeedGenerationISOPodLogs = func(context.Context, string, string) ([]byte, error) {
		return append([]byte("useful build failure\n"), snapshot.Secrets["upload"].Data["privateKey"]...), nil
	}
	return task, artifact, job, pod
}

func TestSeedGenerationISODiagnosticsTransientErrorsPreserveBuildResources(t *testing.T) {
	for _, timedOut := range []bool{false, true} {
		for _, operation := range []string{"get", "create", "lost create response"} {
			name := "failed/"
			if timedOut {
				name = "timed out/"
			}
			t.Run(name+operation, func(t *testing.T) {
				g, ctx := NewWithT(t), context.Background()
				task, artifact, job, pod := seedGenerationISOFailureFixture(t)
				diagnosticsName := seedGenerationISODiagnosticsName(task.object)
				rejected, deletes := false, 0
				task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
						if _, isConfig := object.(*corev1.ConfigMap); isConfig && key.Name == diagnosticsName && operation == "get" && !rejected {
							rejected = true
							return apierrors.NewServiceUnavailable("temporary API failure")
						}
						return c.Get(ctx, key, object, opts...)
					},
					Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
						if _, isConfig := object.(*corev1.ConfigMap); isConfig && object.GetName() == diagnosticsName && !rejected {
							rejected = true
							if operation == "lost create response" {
								if err := c.Create(ctx, object, opts...); err != nil {
									return fmt.Errorf("failed to simulate a lost diagnostics response: %w", err)
								}
								return apierrors.NewTimeoutError("response lost after persistence", 1)
							}
							return apierrors.NewServiceUnavailable("temporary API failure")
						}
						return c.Create(ctx, object, opts...)
					},
					Delete: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
						deletes++
						persisted := &provisioningv1alpha1.ProvisioningRequest{}
						g.Expect(c.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
						ref := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef
						g.Expect(ref).ToNot(BeNil(), "cleanup must wait for the diagnostics checkpoint")
						g.Expect(c.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, &corev1.ConfigMap{})).To(Succeed())
						return c.Delete(ctx, object, opts...)
					},
				})
				for attempt := 0; attempt < 2; attempt++ {
					persisted := &provisioningv1alpha1.ProvisioningRequest{}
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					restarted := seedTestTask(persisted, task.client)
					restarted.seedGenerationISONow = task.seedGenerationISONow
					var err error
					if timedOut {
						_, err = restarted.failSeedGenerationISOTimeout(ctx, "Live ISO build timed out")
					} else {
						_, err = restarted.observeSeedGenerationISOJob(ctx, artifact)
					}
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					if attempt == 0 {
						g.Expect(err).To(MatchError(ContainSubstring("failed to retain redacted live ISO diagnostics")))
						g.Expect(rejected).To(BeTrue())
						g.Expect(deletes).To(BeZero())
						g.Expect(seedGenerationTerminal(persisted)).To(BeFalse())
						g.Expect(seedGenerationCondition(persisted).Message).To(ContainSubstring("waiting to retain redacted diagnostics"))
						g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).To(Succeed())
						g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).To(Succeed())
						continue
					}
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(deletes).To(BeNumerically(">", 0))
					reason := provisioningv1alpha1.CRconditionReasons.Failed
					if timedOut {
						reason = provisioningv1alpha1.CRconditionReasons.TimedOut
					}
					g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(reason)))
					status := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus
					g.Expect(status.ISOFailureDiagnosticsRef).ToNot(BeNil())
					g.Expect(status.ISOFailureDiagnosticsExpiresAt).ToNot(BeNil())
					retained := &corev1.ConfigMap{}
					g.Expect(task.client.Get(ctx, client.ObjectKey{Name: status.ISOFailureDiagnosticsRef.Name, Namespace: pod.Namespace}, retained)).To(Succeed())
					g.Expect(retained.Data[seedGenerationISODiagnosticsKey]).To(ContainSubstring("useful build failure"))
					g.Expect(retained.Data[seedGenerationISODiagnosticsKey]).ToNot(ContainSubstring("ssh-private-key"))
				}
			})
		}
	}
}

func TestSeedGenerationISODiagnosticsRetriesStopAtRunDeadline(t *testing.T) {
	for _, operation := range []string{"get", "create"} {
		for _, expiry := range []struct {
			name   string
			offset time.Duration
		}{
			{name: "at deadline"},
			{name: "after deadline", offset: time.Second},
		} {
			t.Run(operation+"/"+expiry.name, func(t *testing.T) {
				g, ctx := NewWithT(t), context.Background()
				task, _, job, pod := seedGenerationISOFailureFixture(t)
				g.Expect(job.Status.Conditions).To(BeEmpty(), "the retry bound must not require a Job terminal condition")
				snapshot, err := task.loadSeedGenerationInputSnapshot(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
				deadline := status.StartedAt.Add(time.Duration(status.TimeoutSeconds) * time.Second)
				diagnosticsName := seedGenerationISODiagnosticsName(task.object)
				attempts, deletes := 0, 0
				task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
						if _, isConfig := object.(*corev1.ConfigMap); isConfig && key.Name == diagnosticsName && operation == "get" {
							attempts++
							return apierrors.NewServiceUnavailable("persistent diagnostics API failure")
						}
						return c.Get(ctx, key, object, opts...)
					},
					Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
						if _, isConfig := object.(*corev1.ConfigMap); isConfig && object.GetName() == diagnosticsName && operation == "create" {
							attempts++
							return apierrors.NewInternalError(errors.New("diagnostics admission webhook unavailable"))
						}
						return c.Create(ctx, object, opts...)
					},
					Delete: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
						deletes++
						persisted := &provisioningv1alpha1.ProvisioningRequest{}
						g.Expect(c.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
						g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)),
							"the timeout must be persisted before cleanup")
						return c.Delete(ctx, object, opts...)
					},
				})
				for _, offset := range []time.Duration{-2 * time.Second, -time.Second, expiry.offset} {
					persisted := &provisioningv1alpha1.ProvisioningRequest{}
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					restarted := seedTestTask(persisted, task.client)
					restarted.seedGenerationISONow = func() time.Time { return deadline.Add(offset) }
					_, err := restarted.reconcileSeedGeneration(ctx)
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					if offset < 0 {
						g.Expect(err).To(MatchError(ContainSubstring("failed to retain redacted live ISO diagnostics")))
						g.Expect(seedGenerationTerminal(persisted)).To(BeFalse())
						g.Expect(deletes).To(BeZero())
						g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).To(Succeed())
						g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).To(Succeed())
						g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.ConfigMap), &corev1.ConfigMap{})).To(Succeed())
						for _, secret := range snapshot.Secrets {
							g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(secret), &corev1.Secret{})).To(Succeed())
						}
						continue
					}
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
					g.Expect(seedGenerationCondition(persisted).Message).To(ContainSubstring("redacted build logs could not be retained"))
					g.Expect(persisted.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
					g.Expect(persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef).To(BeNil())
					g.Expect(deletes).To(BeNumerically(">", 0))
					g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
				}
				g.Expect(attempts).To(Equal(3), "retention is attempted once more at expiry")
				for range 4 {
					persisted := &provisioningv1alpha1.ProvisioningRequest{}
					g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
					_, err := seedTestTask(persisted, task.client).reconcileSeedGeneration(ctx)
					g.Expect(err).ToNot(HaveOccurred())
				}
				g.Expect(attempts).To(Equal(3), "terminal cleanup must not retry diagnostics")
				g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKeyFromObject(snapshot.ConfigMap), &corev1.ConfigMap{}))).To(BeTrue())
				for _, secret := range snapshot.Secrets {
					g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKeyFromObject(secret), &corev1.Secret{}))).To(BeTrue())
				}
				g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKey{
					Name: seedGenerationISOPVCName(task.object), Namespace: job.Namespace,
				}, &corev1.PersistentVolumeClaim{}))).To(BeTrue())
			})
		}
	}
}

func TestSeedGenerationISODiagnosticsRetainedAtRunDeadline(t *testing.T) {
	g, ctx := NewWithT(t), context.Background()
	task, _, _, _ := seedGenerationISOFailureFixture(t)
	status := task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	deadline := status.StartedAt.Add(time.Duration(status.TimeoutSeconds) * time.Second)
	task.seedGenerationISONow = func() time.Time { return deadline }
	_, err := task.reconcileSeedGeneration(ctx)
	g.Expect(err).ToNot(HaveOccurred())
	persisted := &provisioningv1alpha1.ProvisioningRequest{}
	g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(task.object), persisted)).To(Succeed())
	g.Expect(seedGenerationCondition(persisted).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
	ref := persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef
	g.Expect(ref).ToNot(BeNil())
	retained := &corev1.ConfigMap{}
	g.Expect(task.client.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, retained)).To(Succeed())
	g.Expect(retained.Data[ref.Key]).To(ContainSubstring("useful build failure"))
	g.Expect(retained.Data[ref.Key]).ToNot(ContainSubstring("ssh-private-key"))
}

func TestSeedGenerationISODiagnosticsPermanentErrorsAllowCleanup(t *testing.T) {
	for _, failure := range []string{"foreign owner", "different content", "forbidden"} {
		t.Run(failure, func(t *testing.T) {
			g, ctx := NewWithT(t), context.Background()
			task, artifact, job, _ := seedGenerationISOFailureFixture(t)
			config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name: seedGenerationISODiagnosticsName(task.object), Namespace: job.Namespace,
				Labels:          seedGenerationISOResourceLabels(task.object, seedGenerationISODiagnosticsKind),
				OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(task.object)},
			}, Immutable: k8sptr.To(true), Data: map[string]string{seedGenerationISODiagnosticsKey: "other diagnostics"}}
			if failure == "foreign owner" {
				config.Labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] = "other-request"
			}
			if failure != "forbidden" {
				g.Expect(task.client.Create(ctx, config)).To(Succeed())
			} else {
				task.client = interceptor.NewClient(task.client.(client.WithWatch), interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.CreateOption) error {
						if _, isConfig := object.(*corev1.ConfigMap); isConfig && object.GetName() == config.Name {
							return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, config.Name, errors.New("access denied"))
						}
						return c.Create(ctx, object, opts...)
					},
				})
			}
			_, err := task.observeSeedGenerationISOJob(ctx, artifact)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(seedGenerationCondition(task.object).Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Failed)))
			g.Expect(seedGenerationCondition(task.object).Message).To(ContainSubstring("redacted build logs could not be retained"))
			g.Expect(task.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsRef).To(BeNil())
			g.Expect(apierrors.IsNotFound(task.client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
			if failure == "foreign owner" {
				g.Expect(task.client.Get(ctx, client.ObjectKeyFromObject(config), &corev1.ConfigMap{})).To(Succeed())
			}
		})
	}
}
