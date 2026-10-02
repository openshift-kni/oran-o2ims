/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/coreos/go-semver/semver"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	seedGenerationToolsImageEnv      = "RELATED_IMAGE_SEED_GENERATION_TOOLS_IMAGE"
	seedGenerationPreflightTimeout   = int64(900)
	seedGenerationPreflightLogLimit  = 4 << 20
	seedGenerationPreflightContainer = "preflight"
)

var seedGenerationDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type seedGenerationReleaseInfo struct {
	Digest   string `json:"digest"`
	Metadata struct {
		Version string `json:"version"`
	} `json:"metadata"`
}

type seedGenerationPodLogReader func(context.Context, string, string) ([]byte, error)

var readSeedGenerationPodLogs seedGenerationPodLogReader = readSeedGenerationPodLogsFromAPI

// ensureSeedGenerationToolsPreflight validates the release payload and proves
// the related tools image can extract the matching openshift-install binary.
// It creates no PVC or Job.
func (t *provisioningRequestReconcilerTask) ensureSeedGenerationToolsPreflight(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (bool, bool, error) {
	if snapshot == nil || snapshot.Document == nil {
		return false, false, fmt.Errorf("seed generation input snapshot is missing")
	}
	seedConfig := snapshot.Document.SeedGeneration
	liveISOValue, hasISO := seedConfig["liveISO"]
	if !hasISO {
		return true, false, nil
	}
	_, ok := liveISOValue.(map[string]any)
	if !ok {
		return false, false, fmt.Errorf("frozen liveISO configuration is not an object")
	}
	if snapshot.Document.ResolvedReleaseImage == "" {
		return false, false, fmt.Errorf("frozen resolved release image is missing")
	}
	toolsImage, err := seedGenerationToolsImage()
	if err != nil {
		return false, false, err
	}
	infoPodName := seedGenerationResourceName(t.object, "release-info")
	if snapshotStatus := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus; snapshotStatus.ReleaseImageDigest == "" {
		pod, logs, complete, err := t.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage,
			infoPodName, seedGenerationReleaseInfoCommand(), snapshot.Document.ResolvedReleaseImage)
		if err != nil {
			return false, false, err
		}
		if !complete {
			return false, true, nil
		}
		releaseInfo, err := parseSeedGenerationReleaseInfo(logs)
		if err != nil {
			return false, false, fmt.Errorf("release info preflight output is invalid: %w", err)
		}
		if err := validateSeedGenerationReleaseVersion(snapshot.Document.Release, releaseInfo.Metadata.Version); err != nil {
			return false, false, err
		}
		digestReference, err := pinnedSeedGenerationReleaseImage(snapshot.Document.ResolvedReleaseImage, releaseInfo.Digest)
		if err != nil {
			return false, false, err
		}
		if err := t.persistSeedGenerationReleaseDigest(ctx, digestReference); err != nil {
			return false, false, err
		}
		if err := t.deleteSeedGenerationPreflightPod(ctx, pod); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	if pending, err := t.deleteSeedGenerationPreflightPodIfPresent(ctx, infoPodName); err != nil || pending {
		return false, pending, err
	}
	extractPodName := seedGenerationResourceName(t.object, "installer")
	_, _, complete, err := t.observeSeedGenerationPreflightPod(ctx, snapshot, toolsImage,
		extractPodName, seedGenerationInstallerCommand(), t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ReleaseImageDigest)
	if err != nil {
		return false, false, err
	}
	if !complete {
		return false, true, nil
	}
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.InProgress, metav1.ConditionFalse,
		"Seed generation inputs and installer are validated and frozen")
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return false, false, fmt.Errorf("failed to record seed generation installer preflight completion: %w", err)
	}
	if pending, err := t.deleteSeedGenerationPreflightPodIfPresent(ctx, extractPodName); err != nil || pending {
		return false, pending, err
	}
	return true, false, nil
}

func seedGenerationToolsImage() (string, error) {
	image := strings.TrimSpace(os.Getenv(seedGenerationToolsImageEnv))
	if _, _, err := imageRepositoryFromPullSpec(image); err != nil {
		return "", fmt.Errorf("%s must contain a valid image reference: %w", seedGenerationToolsImageEnv, err)
	}
	if strings.Contains(image, "@") {
		if !seedGenerationDigestPattern.MatchString(image[strings.LastIndex(image, "@")+1:]) {
			return "", fmt.Errorf("%s must contain a valid sha256 digest", seedGenerationToolsImageEnv)
		}
		return image, nil
	}
	imageName := strings.TrimPrefix(image, "docker://")
	if strings.LastIndexByte(imageName, ':') <= strings.LastIndexByte(imageName, '/') {
		return "", fmt.Errorf("%s must use a tag or digest", seedGenerationToolsImageEnv)
	}
	return image, nil
}

func seedGenerationReleaseInfoCommand() string {
	return `set -eu
if ! oc adm release info --registry-config=/var/run/seedgen/auth/config.json --idms-file=/var/run/seedgen/config/idms.yaml -o json "$1" >/tmp/release-info.json 2>/tmp/oc.stderr; then
	cat /tmp/oc.stderr >&2
	exit 1
fi
cat /tmp/release-info.json`
}

func seedGenerationInstallerCommand() string {
	return `set -eu
mkdir -p /tmp/seedgen-installer
if ! oc adm release extract --registry-config=/var/run/seedgen/auth/config.json --idms-file=/var/run/seedgen/config/idms.yaml --command=openshift-install --to=/tmp/seedgen-installer "$1" >/tmp/release-extract.log 2>&1; then
	cat /tmp/release-extract.log >&2
	exit 1
fi
test -x /tmp/seedgen-installer/openshift-install`
}

func parseSeedGenerationReleaseInfo(output []byte) (*seedGenerationReleaseInfo, error) {
	info := &seedGenerationReleaseInfo{}
	if err := json.Unmarshal(output, info); err != nil {
		return nil, fmt.Errorf("failed to decode release metadata: %w", err)
	}
	if info.Metadata.Version == "" {
		return nil, fmt.Errorf("release metadata has no version")
	}
	if !seedGenerationDigestPattern.MatchString(info.Digest) {
		return nil, fmt.Errorf("release metadata has no valid sha256 digest")
	}
	return info, nil
}

func validateSeedGenerationReleaseVersion(expected, actual string) error {
	expectedVersion, err := semver.NewVersion(expected)
	if err != nil {
		return fmt.Errorf("invalid ClusterTemplate release version %q: %w", expected, err)
	}
	actualVersion, err := semver.NewVersion(actual)
	if err != nil {
		return fmt.Errorf("invalid release image version %q: %w", actual, err)
	}
	if expectedVersion.Compare(*actualVersion) != 0 {
		return fmt.Errorf("release image version %s does not match ClusterTemplate release %s", actualVersion, expectedVersion)
	}
	return nil
}

func pinnedSeedGenerationReleaseImage(image, digest string) (string, error) {
	if !seedGenerationDigestPattern.MatchString(digest) {
		return "", fmt.Errorf("release image digest %q is invalid", digest)
	}
	registry, repository, err := imageRepositoryFromPullSpec(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse resolved release image: %w", err)
	}
	return registry + "/" + repository + "@" + digest, nil
}

func (t *provisioningRequestReconcilerTask) persistSeedGenerationReleaseDigest(ctx context.Context, digest string) error {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status.ReleaseImageDigest != "" {
		if status.ReleaseImageDigest != digest {
			return fmt.Errorf("resolved release image digest changed from %s to %s", status.ReleaseImageDigest, digest)
		}
		return nil
	}
	status.ReleaseImageDigest = digest
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to persist resolved release image digest: %w", err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) observeSeedGenerationPreflightPod(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
	toolsImage string,
	name string,
	command string,
	argument string,
) (*corev1.Pod, []byte, bool, error) {
	pod := &corev1.Pod{}
	key := types.NamespacedName{Name: name, Namespace: seedGenerationSnapshotNamespace()}
	if err := t.client.Get(ctx, key, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, nil, false, fmt.Errorf("failed to get seed generation preflight Pod %s: %w", name, err)
		}
		pod = makeSeedGenerationPreflightPod(t.object, snapshot, toolsImage, name, command, argument)
		if err := t.client.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, nil, false, fmt.Errorf("failed to create seed generation preflight Pod %s: %w", name, err)
		}
		return nil, nil, false, nil
	}
	if pod.DeletionTimestamp != nil {
		return nil, nil, false, nil
	}
	if err := validateSeedGenerationSnapshotOwner(pod, t.object, seedGenerationSnapshotToolKind, nil); err != nil {
		return nil, nil, false, err
	}
	expectedPod := makeSeedGenerationPreflightPod(t.object, snapshot, toolsImage, name, command, argument)
	if err := validateSeedGenerationPreflightPodSpec(pod, expectedPod); err != nil {
		return nil, nil, false, fmt.Errorf("seed generation preflight Pod %s is invalid: %w", name, err)
	}
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning, corev1.PodUnknown:
		return nil, nil, false, nil
	case corev1.PodFailed:
		return nil, nil, false, fmt.Errorf("seed generation preflight Pod %s failed: %s", name, seedGenerationPodFailureReason(pod))
	case corev1.PodSucceeded:
		logs, err := readSeedGenerationPodLogs(ctx, key.Namespace, key.Name)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to read seed generation preflight Pod %s logs: %w", name, err)
		}
		return pod, logs, true, nil
	default:
		return nil, nil, false, fmt.Errorf("seed generation preflight Pod %s has unsupported phase %q", name, pod.Status.Phase)
	}
}

func validateSeedGenerationPreflightPodSpec(actual, expected *corev1.Pod) error {
	if len(actual.Spec.Containers) != 1 || len(expected.Spec.Containers) != 1 {
		return fmt.Errorf("expected exactly one container")
	}
	if err := validateSeedGenerationPreflightContainer(&actual.Spec.Containers[0], &expected.Spec.Containers[0]); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual.Spec.Volumes, expected.Spec.Volumes) {
		return fmt.Errorf("volumes do not match the expected preflight inputs")
	}
	if actual.Spec.RestartPolicy != corev1.RestartPolicyNever ||
		actual.Spec.AutomountServiceAccountToken == nil || *actual.Spec.AutomountServiceAccountToken ||
		actual.Spec.HostNetwork || actual.Spec.HostPID || actual.Spec.HostIPC {
		return fmt.Errorf("pod security settings do not match the expected preflight")
	}
	if err := validateSeedGenerationPreflightPodSecurityContext(actual.Spec.SecurityContext); err != nil {
		return err
	}
	return validateSeedGenerationPreflightContainerSecurity(actual.Spec.Containers[0].SecurityContext)
}

func validateSeedGenerationPreflightContainer(actual, expected *corev1.Container) error {
	if actual.Name != expected.Name || actual.Image != expected.Image ||
		actual.ImagePullPolicy != expected.ImagePullPolicy ||
		!reflect.DeepEqual(actual.Command, expected.Command) ||
		!reflect.DeepEqual(actual.Args, expected.Args) ||
		actual.WorkingDir != expected.WorkingDir ||
		!reflect.DeepEqual(actual.Env, expected.Env) ||
		!reflect.DeepEqual(actual.VolumeMounts, expected.VolumeMounts) {
		return fmt.Errorf("container command, image, or mounts do not match the expected preflight")
	}
	return nil
}

func validateSeedGenerationPreflightPodSecurityContext(securityContext *corev1.PodSecurityContext) error {
	if securityContext == nil || securityContext.RunAsNonRoot == nil || !*securityContext.RunAsNonRoot ||
		securityContext.SeccompProfile == nil ||
		securityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		return fmt.Errorf("pod security context does not enforce non-root execution and the runtime-default seccomp profile")
	}
	return nil
}

func validateSeedGenerationPreflightContainerSecurity(securityContext *corev1.SecurityContext) error {
	if securityContext == nil || securityContext.AllowPrivilegeEscalation == nil ||
		*securityContext.AllowPrivilegeEscalation || securityContext.ReadOnlyRootFilesystem == nil ||
		!*securityContext.ReadOnlyRootFilesystem || securityContext.Capabilities == nil ||
		len(securityContext.Capabilities.Add) != 0 || !slices.Contains(securityContext.Capabilities.Drop, corev1.Capability("ALL")) {
		return fmt.Errorf("container security context does not meet the expected preflight restrictions")
	}
	return nil
}

func seedGenerationPodFailureReason(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == seedGenerationPreflightContainer && status.State.Terminated != nil {
			terminated := status.State.Terminated
			if terminated.Reason != "" {
				return fmt.Sprintf("%s (exit code %d)", terminated.Reason, terminated.ExitCode)
			}
			return fmt.Sprintf("container exited with code %d", terminated.ExitCode)
		}
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	return "unknown failure"
}

func makeSeedGenerationPreflightPod(
	pr *provisioningv1alpha1.ProvisioningRequest,
	snapshot *seedGenerationInputSnapshot,
	toolsImage string,
	name string,
	command string,
	argument string,
) *corev1.Pod {
	configItems := []corev1.KeyToPath{{Key: seedGenerationSnapshotMirrorFile, Path: seedGenerationSnapshotMirrorFile}}
	volumes := []corev1.Volume{
		{
			Name: "snapshot-config",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: snapshot.ConfigMap.Name},
				Items:                configItems,
				DefaultMode:          k8sptr.To[int32](420),
				Optional:             k8sptr.To(false),
			}},
		},
		{
			Name: "pull-secret",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  snapshot.Secrets["pullSecret"].Name,
				Items:       []corev1.KeyToPath{{Key: corev1.DockerConfigJsonKey, Path: "config.json"}},
				DefaultMode: k8sptr.To[int32](420),
				Optional:    k8sptr.To(false),
			}},
		},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "snapshot-config", MountPath: "/var/run/seedgen/config", ReadOnly: true},
		{Name: "pull-secret", MountPath: "/var/run/seedgen/auth", ReadOnly: true},
		{Name: "tmp", MountPath: "/tmp"},
	}
	if len(snapshot.Document.RegistryTrustedCAKeys) > 0 {
		items := make([]corev1.KeyToPath, 0, len(snapshot.Document.RegistryTrustedCAKeys))
		for registry, key := range snapshot.Document.RegistryTrustedCAKeys {
			items = append(items, corev1.KeyToPath{Key: key, Path: registry + "/ca.crt"})
		}
		volumes = append(volumes, corev1.Volume{
			Name: "registry-certs",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: snapshot.ConfigMap.Name},
				Items:                items,
				DefaultMode:          k8sptr.To[int32](420),
				Optional:             k8sptr.To(false),
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: "registry-certs", MountPath: "/etc/containers/certs.d", ReadOnly: true})
	}
	labels := seedGenerationSnapshotLabels(pr, seedGenerationSnapshotToolKind)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  k8sptr.To(false),
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         k8sptr.To(seedGenerationPreflightTimeout),
			TerminationGracePeriodSeconds: k8sptr.To[int64](0),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   k8sptr.To(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Volumes: volumes,
			Containers: []corev1.Container{{
				Name:            seedGenerationPreflightContainer,
				Image:           toolsImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/bin/sh", "-c"},
				Args:            []string{command, "seedgen", argument},
				WorkingDir:      "/tmp",
				Env:             []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}},
				VolumeMounts:    mounts,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: k8sptr.To(false),
					ReadOnlyRootFilesystem:   k8sptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
}

func readSeedGenerationPodLogsFromAPI(ctx context.Context, namespace, name string) ([]byte, error) {
	config, err := clientconfig.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}
	limit := int64(seedGenerationPreflightLogLimit)
	request := clientset.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		Container:  seedGenerationPreflightContainer,
		LimitBytes: &limit,
	})
	stream, err := request.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open Pod log stream: %w", err)
	}
	defer stream.Close()
	output, err := io.ReadAll(io.LimitReader(stream, limit))
	if err != nil {
		return nil, fmt.Errorf("failed to read Pod log stream: %w", err)
	}
	return output, nil
}

func (t *provisioningRequestReconcilerTask) deleteSeedGenerationPreflightPod(ctx context.Context, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	if err := client.IgnoreNotFound(t.client.Delete(ctx, pod, client.GracePeriodSeconds(0))); err != nil {
		return fmt.Errorf("failed to delete seed generation preflight Pod %s: %w", pod.Name, err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) deleteSeedGenerationPreflightPodIfPresent(ctx context.Context, name string) (bool, error) {
	pod := &corev1.Pod{}
	key := types.NamespacedName{Name: name, Namespace: seedGenerationSnapshotNamespace()}
	if err := t.client.Get(ctx, key, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get seed generation preflight Pod %s: %w", name, err)
	}
	if pod.DeletionTimestamp != nil {
		return true, nil
	}
	if err := validateSeedGenerationSnapshotOwner(pod, t.object, seedGenerationSnapshotToolKind, nil); err != nil {
		return false, err
	}
	if err := t.deleteSeedGenerationPreflightPod(ctx, pod); err != nil {
		return false, err
	}
	return true, nil
}

func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationPreflightPods(ctx context.Context) (bool, error) {
	pods := &corev1.PodList{}
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationRunLabel:                            "true",
	}
	if err := t.client.List(ctx, pods, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return false, fmt.Errorf("failed to list seed generation preflight Pods: %w", err)
	}
	pending := false
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := validateSeedGenerationSnapshotOwner(pod, t.object, seedGenerationSnapshotToolKind, nil); err != nil {
			return false, err
		}
		pending = true
		if pod.DeletionTimestamp == nil {
			if err := t.deleteSeedGenerationPreflightPod(ctx, pod); err != nil {
				return false, err
			}
		}
	}
	return pending, nil
}
