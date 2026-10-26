/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestSeedGenerationISOJobAPIDefaults(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("requires envtest binaries, run make test-envtest")
	}
	environment := &envtest.Environment{BinaryAssetsDirectory: assets}
	config, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, batchv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	pr, snapshot, artifact := seedGenerationISOJobFixture(t, "")
	expected, err := makeSeedGenerationISOJob(pr, snapshot, artifact, "example.test/release@sha256:"+strings.Repeat("a", 64), 3600)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: expected.Namespace}}
	if err := c.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	actual := expected.DeepCopy()
	if err := c.Create(ctx, actual); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(actual), actual); err != nil {
		t.Fatal(err)
	}
	if actual.Spec.Template.Labels[batchv1.JobNameLabel] != actual.Name || actual.Spec.Template.Labels[batchv1.ControllerUidLabel] != string(actual.UID) {
		t.Fatalf("API did not add expected Job labels: %v", actual.Spec.Template.Labels)
	}
	if err := validateSeedGenerationISOJob(actual, expected); err != nil {
		t.Fatalf("rejected Job persisted by API: %v", err)
	}
	actual.Spec.Template.Labels[seedGenerationISOResourceKindLabel] = "foreign"
	if err := validateSeedGenerationISOJob(actual, expected); err == nil {
		t.Fatal("accepted a changed ownership label")
	}
}

func TestSeedGenerationISOPendingClaimAllowsJobCreation(t *testing.T) {
	pr, snapshot, _ := seedGenerationISOJobFixture(t, "")
	claim, err := makeSeedGenerationISOPVC(pr, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	claim.Status.Phase = corev1.ClaimPending
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
			StartedAt: &metav1.Time{Time: time.Now()}, TimeoutSeconds: 3600,
			DetachedCleanupCompleted: true, SeedImage: "example.test/seed:v1",
			ReleaseImageDigest: "example.test/release@sha256:" + strings.Repeat("a", 64),
		},
	}
	task := seedTestTask(pr, newSeedTestClient(t, pr, claim))
	if _, err := task.reconcileSeedGenerationISO(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{}
	if err := task.client.Get(context.Background(), client.ObjectKey{
		Name: seedGenerationISOJobName(pr), Namespace: claim.Namespace,
	}, job); err != nil {
		t.Fatalf("Pending claim blocked creation of the consuming Job: %v", err)
	}
	if !pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOJobSubmitted {
		t.Fatal("Job submission was not checkpointed")
	}
	if err := task.client.Get(context.Background(), client.ObjectKeyFromObject(claim), claim); err != nil {
		t.Fatal(err)
	}
	if claim.Status.Phase != corev1.ClaimPending {
		t.Fatal("test required a bound PVC to create the Job")
	}
}
