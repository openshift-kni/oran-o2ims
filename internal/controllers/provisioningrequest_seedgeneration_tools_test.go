/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

func TestSeedGenerationToolsImageAndReleaseValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name    string
		image   string
		wantErr bool
	}{
		{name: "tagged image", image: "quay.io/tools/ose:latest"},
		{name: "digest image", image: "quay.io/tools/ose@" + digest},
		{name: "missing tag", image: "quay.io/tools/ose", wantErr: true},
		{name: "invalid digest", image: "quay.io/tools/ose@sha256:bad", wantErr: true},
		{name: "invalid reference", image: "https://quay.io/tools/ose:latest", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(seedGenerationToolsImageEnv, tc.image)
			got, err := seedGenerationToolsImage()
			if (err != nil) != tc.wantErr {
				t.Fatalf("image=%q err=%v, wantErr=%t", got, err, tc.wantErr)
			}
			if err == nil && got != tc.image {
				t.Fatalf("image=%q, want %q", got, tc.image)
			}
		})
	}

	validInfo := []byte(fmt.Sprintf(`{"digest":%q,"metadata":{"version":"4.22.1"}}`, digest))
	if info, err := parseSeedGenerationReleaseInfo(validInfo); err != nil || info.Metadata.Version != "4.22.1" {
		t.Fatalf("parsed release info=%+v err=%v", info, err)
	}
	for _, output := range [][]byte{
		[]byte("{"),
		[]byte(fmt.Sprintf(`{"digest":%q,"metadata":{}}`, digest)),
		[]byte(`{"digest":"sha256:bad","metadata":{"version":"4.22.1"}}`),
	} {
		if _, err := parseSeedGenerationReleaseInfo(output); err == nil {
			t.Errorf("invalid release metadata %s was accepted", output)
		}
	}

	for _, tc := range []struct {
		expected string
		actual   string
		wantErr  bool
	}{
		{expected: "4.22.1", actual: "4.22.1"},
		{expected: "4.22.1", actual: "4.22.2", wantErr: true},
		{expected: "invalid", actual: "4.22.1", wantErr: true},
		{expected: "4.22.1", actual: "invalid", wantErr: true},
	} {
		err := validateSeedGenerationReleaseVersion(tc.expected, tc.actual)
		if (err != nil) != tc.wantErr {
			t.Errorf("expected=%q actual=%q err=%v, wantErr=%t", tc.expected, tc.actual, err, tc.wantErr)
		}
	}
	pinned, err := pinnedSeedGenerationReleaseImage("quay.io/ocp/release:4.22", digest)
	if err != nil || pinned != "quay.io/ocp/release@"+digest {
		t.Fatalf("pinned image=%q err=%v", pinned, err)
	}
	if _, err := pinnedSeedGenerationReleaseImage("quay.io/ocp/release:4.22", "sha256:bad"); err == nil {
		t.Fatal("invalid release digest was accepted")
	}
	if _, err := pinnedSeedGenerationReleaseImage("invalid?image", digest); err == nil {
		t.Fatal("invalid release image was accepted")
	}
	if !strings.Contains(seedGenerationReleaseInfoCommand(), "oc adm release info") ||
		!strings.Contains(seedGenerationInstallerCommand(), "--command=openshift-install") {
		t.Fatal("preflight commands do not invoke the release metadata and installer checks")
	}
}

func TestSeedGenerationPreflightPodSecurityValidation(t *testing.T) {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr", UID: types.UID("pr-uid")}}
	snapshot := &seedGenerationInputSnapshot{
		ConfigMap: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "seed-config"}},
		Secrets: map[string]*corev1.Secret{
			"pullSecret": {ObjectMeta: metav1.ObjectMeta{Name: "pull-secret"}},
		},
		Document: &seedGenerationInputDocument{RegistryTrustedCAKeys: map[string]string{
			"z-mirror.example.com:5000": "registry-ca-2",
			"a-mirror.example.com":      "registry-ca-0",
			"m-mirror.example.com":      "registry-ca-1",
		}},
	}
	pod := makeSeedGenerationPreflightPod(pr, snapshot, "quay.io/tools:latest", "release-info", "check-release", "release-image")
	if len(pod.Spec.Volumes) != 4 || len(pod.Spec.Containers) != 1 {
		t.Fatalf("unexpected preflight Pod shape: %+v", pod.Spec)
	}
	if err := validateSeedGenerationPreflightPodSpec(pod, pod.DeepCopy()); err != nil {
		t.Fatalf("generated preflight Pod failed validation: %v", err)
	}
	wantRegistryItems := []corev1.KeyToPath{
		{Key: "registry-ca-0", Path: "a-mirror.example.com/ca.crt"},
		{Key: "registry-ca-1", Path: "m-mirror.example.com/ca.crt"},
		{Key: "registry-ca-2", Path: "z-mirror.example.com:5000/ca.crt"},
	}
	for range 10 {
		pod := makeSeedGenerationPreflightPod(pr, snapshot, "quay.io/tools:latest", "release-info", "check-release", "release-image")
		if items := pod.Spec.Volumes[3].ConfigMap.Items; !reflect.DeepEqual(items, wantRegistryItems) {
			t.Fatalf("registry CA items are not stable and sorted: got %+v, want %+v", items, wantRegistryItems)
		}
	}

	invalid := []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{name: "extra container", mutate: func(pod *corev1.Pod) {
			pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "extra"})
		}},
		{name: "command changed", mutate: func(pod *corev1.Pod) { pod.Spec.Containers[0].Args[0] = "untrusted-command" }},
		{name: "volume changed", mutate: func(pod *corev1.Pod) { pod.Spec.Volumes[0].Name = "unexpected" }},
		{name: "host network", mutate: func(pod *corev1.Pod) { pod.Spec.HostNetwork = true }},
		{name: "missing pod security context", mutate: func(pod *corev1.Pod) { pod.Spec.SecurityContext = nil }},
		{name: "privilege escalation", mutate: func(pod *corev1.Pod) { pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation = nil }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			actual := pod.DeepCopy()
			tc.mutate(actual)
			if err := validateSeedGenerationPreflightPodSpec(actual, pod); err == nil {
				t.Fatal("unsafe or changed Pod spec was accepted")
			}
		})
	}

	for _, tc := range []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "terminated container reason",
			pod: &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name:  seedGenerationPreflightContainer,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 2}},
			}}}},
			want: "Error (exit code 2)",
		},
		{
			name: "terminated exit code",
			pod: &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name:  seedGenerationPreflightContainer,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 2}},
			}}}},
			want: "container exited with code 2",
		},
		{name: "pod reason", pod: &corev1.Pod{Status: corev1.PodStatus{Reason: "DeadlineExceeded"}}, want: "DeadlineExceeded"},
		{name: "unknown", pod: &corev1.Pod{}, want: "unknown failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := seedGenerationPodFailureReason(tc.pod); got != tc.want {
				t.Fatalf("failure reason=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestObserveSeedGenerationPreflightPod(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "seed-pr", UID: types.UID("pr-uid")}}
	snapshot := &seedGenerationInputSnapshot{
		ConfigMap: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "seed-config"}},
		Secrets: map[string]*corev1.Secret{
			"pullSecret": {ObjectMeta: metav1.ObjectMeta{Name: "pull-secret"}},
		},
		Document: &seedGenerationInputDocument{},
	}
	name := "seed-preflight"
	command := "check-release"
	argument := "release-image"
	toolsImage := "quay.io/tools:latest"

	t.Run("creates a missing Pod", func(t *testing.T) {
		c := newSeedTestClient(t, pr.DeepCopy())
		task := seedTestTask(pr.DeepCopy(), c)
		pod, logs, complete, err := task.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage, name, command, argument)
		if err != nil || pod != nil || logs != nil || complete {
			t.Fatalf("observation=%v, %q, %t, %v", pod, logs, complete, err)
		}
		created := &corev1.Pod{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: seedGenerationSnapshotNamespace()}, created); err != nil {
			t.Fatalf("preflight Pod was not created: %v", err)
		}
	})

	t.Run("waits for a pending Pod", func(t *testing.T) {
		pod := makeSeedGenerationPreflightPod(pr, snapshot, toolsImage, name, command, argument)
		pod.Status.Phase = corev1.PodPending
		c := newSeedTestClient(t, pr.DeepCopy(), pod)
		task := seedTestTask(pr.DeepCopy(), c)
		_, _, complete, err := task.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage, name, command, argument)
		if err != nil || complete {
			t.Fatalf("pending Pod observation complete=%t err=%v", complete, err)
		}
	})

	t.Run("returns logs for a succeeded Pod", func(t *testing.T) {
		pod := makeSeedGenerationPreflightPod(pr, snapshot, toolsImage, name, command, argument)
		pod.Status.Phase = corev1.PodSucceeded
		c := newSeedTestClient(t, pr.DeepCopy(), pod)
		task := seedTestTask(pr.DeepCopy(), c)
		previousReader := readSeedGenerationPodLogs
		readSeedGenerationPodLogs = func(_ context.Context, namespace, podName string) ([]byte, error) {
			if namespace != seedGenerationSnapshotNamespace() || podName != name {
				t.Errorf("unexpected Pod log request %s/%s", namespace, podName)
			}
			return []byte("release metadata"), nil
		}
		t.Cleanup(func() { readSeedGenerationPodLogs = previousReader })
		gotPod, logs, complete, err := task.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage, name, command, argument)
		if err != nil || !complete || gotPod == nil || string(logs) != "release metadata" {
			t.Fatalf("observation=%v, %q, %t, %v", gotPod, logs, complete, err)
		}
	})

	t.Run("reports a failed Pod", func(t *testing.T) {
		pod := makeSeedGenerationPreflightPod(pr, snapshot, toolsImage, name, command, argument)
		pod.Status.Phase = corev1.PodFailed
		pod.Status.Reason = "DeadlineExceeded"
		c := newSeedTestClient(t, pr.DeepCopy(), pod)
		task := seedTestTask(pr.DeepCopy(), c)
		if _, _, _, err := task.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage, name, command, argument); err == nil ||
			!strings.Contains(err.Error(), "DeadlineExceeded") {
			t.Fatalf("failed Pod error=%v", err)
		}
	})
}
