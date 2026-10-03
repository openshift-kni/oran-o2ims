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

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func TestSeedGenerationDigestMirrorConfiguration(t *testing.T) {
	raw := []any{
		map[string]any{
			"source":             " quay.io/team/ ",
			"mirrors":            []any{"mirror.example/team", "mirror2.example/team"},
			"mirrorSourcePolicy": string(configv1.NeverContactSource),
		},
		map[string]any{
			"source":             "quay.io/team",
			"mirrors":            []any{"mirror.example/team", "mirror3.example/team"},
			"mirrorSourcePolicy": string(configv1.AllowContactingSource),
		},
	}
	sources, err := parseSeedGenerationImageDigestSources(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Source != "quay.io/team" ||
		sources[0].MirrorSourcePolicy != string(configv1.NeverContactSource) ||
		strings.Join(sources[0].Mirrors, ",") != "mirror.example/team,mirror2.example/team,mirror3.example/team" {
		t.Fatalf("unexpected normalized mirror sources: %+v", sources)
	}

	invalid := []struct {
		name string
		raw  any
	}{
		{name: "not an array", raw: "invalid"},
		{name: "entry is not an object", raw: []any{"invalid"}},
		{name: "missing source", raw: []any{map[string]any{"mirrors": []any{"mirror.example"}}}},
		{name: "source is not a string", raw: []any{map[string]any{"source": 1, "mirrors": []any{"mirror.example"}}}},
		{name: "mirrors is not an array", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": "mirror.example"}}},
		{name: "mirrors is empty", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{}}}},
		{name: "mirror is not a string", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{1}}}},
		{name: "mirror is empty", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{" "}}}},
		{name: "invalid policy", raw: []any{map[string]any{
			"source": "quay.io/team", "mirrors": []any{"mirror.example"}, "mirrorSourcePolicy": "Sometimes",
		}}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseSeedGenerationImageDigestSources(tc.raw); err == nil {
				t.Fatal("invalid mirror input was accepted")
			}
		})
	}

	normalized := normalizeSeedGenerationImageDigestSources([]seedGenerationImageDigestSource{
		{Source: "z.example/", Mirrors: []string{" mirror.example/", "mirror.example"}},
		{Source: " ", Mirrors: []string{"ignored.example"}},
		{Source: "a.example", Mirrors: []string{"a-mirror.example"}},
	})
	if len(normalized) != 2 || normalized[0].Source != "a.example" || normalized[1].Source != "z.example" ||
		len(normalized[1].Mirrors) != 1 {
		t.Fatalf("normalization did not sort, trim, deduplicate, or drop empty sources: %+v", normalized)
	}

	encoded, err := seedGenerationMirrorConfigYAML(sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"apiVersion: config.openshift.io/v1", "kind: ImageDigestMirrorSet", "quay.io/team"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("encoded mirror config does not contain %q: %s", expected, encoded)
		}
	}
}

func TestSeedGenerationMirrorMatchingAndTagResolution(t *testing.T) {
	matching := []struct {
		name       string
		source     string
		registry   string
		repository string
		want       bool
	}{
		{name: "host and repository prefix", source: "quay.io/team", registry: "quay.io", repository: "team/release", want: true},
		{name: "host only", source: "quay.io", registry: "quay.io", repository: "other/release", want: true},
		{name: "wildcard child", source: "*.example.com", registry: "east.example.com", repository: "team/release", want: true},
		{name: "wildcard excludes apex", source: "*.example.com", registry: "example.com", repository: "team/release", want: false},
		{name: "repository mismatch", source: "quay.io/team", registry: "quay.io", repository: "team2/release", want: false},
		{name: "host mismatch", source: "quay.io/team", registry: "registry.example", repository: "team/release", want: false},
	}
	for _, tc := range matching {
		t.Run(tc.name, func(t *testing.T) {
			if got := seedGenerationMirrorSourceMatches(tc.source, tc.registry, tc.repository); got != tc.want {
				t.Fatalf("match=%t, want %t", got, tc.want)
			}
		})
	}

	image := "quay.io/team/release:4.22"
	sets := []configv1.ImageTagMirrorSet{{Spec: configv1.ImageTagMirrorSetSpec{ImageTagMirrors: []configv1.ImageTagMirrors{
		{Source: "quay.io", Mirrors: []configv1.ImageMirror{"mirror.example/general"}},
		{Source: "quay.io/team", Mirrors: []configv1.ImageMirror{"mirror.example/team"}},
	}}}}
	resolved, matched, err := resolveSeedGenerationImageTagMirror(image, sets)
	if err != nil || !matched || resolved != "mirror.example/team/release:4.22" {
		t.Fatalf("resolved image=%q matched=%t err=%v", resolved, matched, err)
	}

	resolved, matched, err = resolveSeedGenerationImageTagMirror(image, nil)
	if err != nil || matched || resolved != image {
		t.Fatalf("unmatched image=%q matched=%t err=%v", resolved, matched, err)
	}
	if _, _, err := resolveSeedGenerationImageTagMirror("invalid?image", nil); err == nil {
		t.Fatal("invalid release image reference was accepted")
	}
	noMirrors := []configv1.ImageTagMirrorSet{{Spec: configv1.ImageTagMirrorSetSpec{ImageTagMirrors: []configv1.ImageTagMirrors{
		{Source: "quay.io/team"},
	}}}}
	if _, _, err := resolveSeedGenerationImageTagMirror(image, noMirrors); err == nil {
		t.Fatal("matching tag mirror set without mirrors was accepted")
	}

	if !hasDigestMirrorForImage("quay.io/team/release:4.22", []seedGenerationImageDigestSource{{
		Source: "quay.io/team", Mirrors: []string{"mirror.example/team"},
	}}) {
		t.Fatal("matching digest mirror was not detected")
	}
	if hasDigestMirrorForImage("quay.io/other/release:4.22", []seedGenerationImageDigestSource{{
		Source: "quay.io/team", Mirrors: []string{"mirror.example/team"},
	}}) || hasDigestMirrorForImage("invalid?image", nil) {
		t.Fatal("non-matching digest mirror was detected")
	}
	if !seedGenerationImageHasDigest("quay.io/team/release@sha256:abc") ||
		seedGenerationImageHasDigest("quay.io/team/release:4.22") || !seedGenerationImageHasTag("quay.io/team/release:4.22") {
		t.Fatal("release image tag and digest classification is incorrect")
	}

	for image, want := range map[string]string{
		"quay.io/team/release:4.22":          ":4.22",
		"quay.io/team/release":               ":latest",
		"docker://quay.io/team/release:4.22": ":4.22",
		"quay.io/team/release@sha256:abc":    "@sha256:abc",
	} {
		if got := seedGenerationImageTagSuffix(image); got != want {
			t.Errorf("suffix for %q = %q, want %q", image, got, want)
		}
	}
}

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

func TestNormalizeSeedGenerationRegistryTrustHost(t *testing.T) {
	for _, tc := range []struct {
		encoded string
		want    string
		valid   bool
	}{
		{encoded: "mirror.example.com", want: "mirror.example.com", valid: true},
		{encoded: "mirror.example.com..5000", want: "mirror.example.com:5000", valid: true},
		{encoded: "mirror.example.com..5000..6000", want: "mirror.example.com:5000..6000", valid: false},
	} {
		got := normalizeSeedGenerationRegistryTrustHost(tc.encoded)
		if got != tc.want || validRegistryCertificateHost(got) != tc.valid {
			t.Errorf("normalize(%q) = %q, valid=%t; want %q, valid=%t",
				tc.encoded, got, validRegistryCertificateHost(got), tc.want, tc.valid)
		}
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

func TestSeedGenerationSpokePrerequisiteHelpers(t *testing.T) {
	partitionConfig := []byte(`{"storage":{"disks":[{"partitions":[{"label":"var-lib-containers"}]}],"filesystems":[{"device":"/dev/disk/by-partlabel/var-lib-containers","path":"/var/lib/containers"}]}}`)
	if partition, filesystem := machineConfigSharedContainersPartitionParts(partitionConfig); !partition || !filesystem {
		t.Fatalf("shared containers config parts=%t,%t", partition, filesystem)
	}
	for _, raw := range [][]byte{nil, []byte("{"), []byte(`{"storage":{"disks":[]}}`)} {
		if partition, filesystem := machineConfigSharedContainersPartitionParts(raw); partition || filesystem {
			t.Errorf("incomplete MachineConfig was accepted: %s", raw)
		}
	}

	t.Run("requires ZTP Done", func(t *testing.T) {
		task := seedTestTask(&provisioningv1alpha1.ProvisioningRequest{}, nil)
		if err := task.validateSeedGenerationSpokePrerequisites(context.Background()); err == nil || !strings.Contains(err.Error(), "ZTP Done") {
			t.Fatalf("prerequisite error=%v", err)
		}
	})
	t.Run("requires a spoke cluster name", func(t *testing.T) {
		pr := &provisioningv1alpha1.ProvisioningRequest{}
		pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{ZtpStatus: ctlrutils.ClusterZtpDone}
		task := seedTestTask(pr, nil)
		if err := task.validateSeedGenerationSpokePrerequisites(context.Background()); err == nil || !strings.Contains(err.Error(), "cluster name") {
			t.Fatalf("prerequisite error=%v", err)
		}
	})

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: seedGenerationLCADeploymentName, Namespace: seedGenerationLCAOperatorNamespace},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			Conditions:        []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
		},
	}
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment).Build()
	if err := validateSeedGenerationLCAAvailable(context.Background(), c); err != nil {
		t.Fatalf("available LCA Deployment was rejected: %v", err)
	}
	deployment.Status.AvailableReplicas = 0
	c = fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment).Build()
	if err := validateSeedGenerationLCAAvailable(context.Background(), c); err == nil {
		t.Fatal("unavailable LCA Deployment was accepted")
	}
}

func TestReconcileSeedGenerationPreflightFailsWhenSpokeIsNotReady(t *testing.T) {
	ctx := context.Background()
	started := metav1.Now()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		Name: "spoke",
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{
			StartedAt: &started, TimeoutSeconds: 3600,
		},
	}
	ctlrutils.SetStatusCondition(&pr.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Validating, metav1.ConditionFalse, "validating")
	c := newSeedTestClient(t, pr)
	task := seedTestTask(pr, c)
	if _, err := task.reconcileSeedGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if !seedGenerationTerminal(pr) || seedGenerationCondition(pr).Reason != string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed) {
		t.Fatalf("unready spoke did not fail preflight: %+v", pr.Status.Conditions)
	}
}

func TestSeedGenerationImageReferenceParsing(t *testing.T) {
	for _, tc := range []struct {
		image     string
		registry  string
		repo      string
		wantError bool
	}{
		{image: "busybox:latest", registry: "docker.io", repo: "library/busybox"},
		{image: "docker://quay.io/team/image:4.22", registry: "quay.io", repo: "team/image"},
		{image: "oci://localhost:5000/team/image", registry: "localhost:5000", repo: "team/image"},
		{image: "team/image:tag", registry: "docker.io", repo: "team/image"},
		{image: "invalid?image", wantError: true},
		{image: "https://quay.io/team/image", wantError: true},
	} {
		registry, repository, err := imageRepositoryFromPullSpec(tc.image)
		if (err != nil) != tc.wantError {
			t.Errorf("image=%q registry=%q repository=%q err=%v", tc.image, registry, repository, err)
			continue
		}
		if err == nil && (registry != tc.registry || repository != tc.repo) {
			t.Errorf("image=%q resolved to %s/%s, want %s/%s", tc.image, registry, repository, tc.registry, tc.repo)
		}
	}
}
