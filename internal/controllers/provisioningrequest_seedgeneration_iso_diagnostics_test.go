/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
