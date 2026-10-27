/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	_ "embed"
	"fmt"
	"reflect"
	"strings"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sptr "k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationISOResourceLabel     = "clcm.openshift.io/seed-generation-iso"
	seedGenerationISOResourceKindLabel = "clcm.openshift.io/seed-generation-iso-kind"
	seedGenerationISOJobLabel          = "clcm.openshift.io/seed-generation-iso-job"
	seedGenerationISOJobKind           = "job"
	seedGenerationISOPodKind           = "pod"
	seedGenerationISOPVCKind           = "pvc"
	seedGenerationISOCAKind            = "ca"
	seedGenerationISOCAKey             = "ca-bundle.crt"
	seedGenerationISOContainer         = "iso-builder"
	seedGenerationISOStorageRequest    = "20Gi"
	seedGenerationISOJobAdoptionWindow = 2 * time.Minute
	seedGenerationISOUploadOwnerFile   = ".oran-seedgen-owner"
)

func seedGenerationISOResourceLabels(pr *provisioningv1alpha1.ProvisioningRequest, kind string) map[string]string {
	return map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
		seedGenerationISOResourceLabel:                    "true",
		seedGenerationISOResourceKindLabel:                kind,
	}
}

func validateSeedGenerationISOResourceOwner(
	resource client.Object,
	pr *provisioningv1alpha1.ProvisioningRequest,
	kind string,
) error {
	labels := resource.GetLabels()
	if labels[provisioningv1alpha1.ProvisioningRequestNameLabel] != pr.Name ||
		labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] != string(pr.UID) ||
		labels[seedGenerationISOResourceLabel] != "true" ||
		labels[seedGenerationISOResourceKindLabel] != kind ||
		!hasSeedGenerationSnapshotOwner(resource, pr) {
		return fmt.Errorf("resource %s/%s is not owned by provisioning request %s with UID %s",
			resource.GetNamespace(), resource.GetName(), pr.Name, pr.UID)
	}
	return nil
}

// Job Pods are controlled by their Job, not directly by the ProvisioningRequest.
// The Job UID labels remain available if garbage collection orphans a Pod while
// cleanup is in progress.
func validateSeedGenerationISOPodOwner(
	pod *corev1.Pod,
	pr *provisioningv1alpha1.ProvisioningRequest,
	jobName string,
	jobUID types.UID,
	allowOrphan bool,
) error {
	labels := pod.Labels
	if pod.Namespace != seedGenerationSnapshotNamespace() || jobUID == "" ||
		labels[provisioningv1alpha1.ProvisioningRequestNameLabel] != pr.Name ||
		labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] != string(pr.UID) ||
		labels[seedGenerationISOResourceLabel] != "true" ||
		labels[seedGenerationISOResourceKindLabel] != seedGenerationISOPodKind ||
		labels[seedGenerationISOJobLabel] != jobName ||
		!seedGenerationISOJobLabelMatches(labels, batchv1.JobNameLabel, "job-name", jobName) ||
		!seedGenerationISOJobLabelMatches(labels, batchv1.ControllerUidLabel, "controller-uid", string(jobUID)) {
		return fmt.Errorf("live ISO build Pod %s/%s does not belong to Job %s with UID %s",
			pod.Namespace, pod.Name, jobName, jobUID)
	}
	for _, ref := range pod.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		if ref.APIVersion == batchv1.SchemeGroupVersion.String() && ref.Kind == "Job" &&
			ref.Name == jobName && ref.UID == jobUID {
			return nil
		}
		return fmt.Errorf("live ISO build Pod %s/%s is controlled by an unexpected resource", pod.Namespace, pod.Name)
	}
	if allowOrphan && len(pod.OwnerReferences) == 0 {
		return nil
	}
	return fmt.Errorf("live ISO build Pod %s/%s has no controlling Job %s with UID %s",
		pod.Namespace, pod.Name, jobName, jobUID)
}

func seedGenerationISOJobLabelMatches(labels map[string]string, current, legacy, expected string) bool {
	found := false
	for _, key := range []string{current, legacy} {
		if value, exists := labels[key]; exists {
			if value != expected {
				return false
			}
			found = true
		}
	}
	return found
}

func seedGenerationISOJobName(pr *provisioningv1alpha1.ProvisioningRequest) string {
	return seedGenerationResourceName(pr, "iso-build")
}

func seedGenerationISOPVCName(pr *provisioningv1alpha1.ProvisioningRequest) string {
	return seedGenerationResourceName(pr, "iso-workspace")
}

func makeSeedGenerationISOPVC(
	pr *provisioningv1alpha1.ProvisioningRequest,
	snapshot *seedGenerationInputSnapshot,
) (*corev1.PersistentVolumeClaim, error) {
	liveISO, ok := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frozen liveISO configuration is missing")
	}
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            seedGenerationISOPVCName(pr),
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          seedGenerationISOResourceLabels(pr, seedGenerationISOPVCKind),
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(seedGenerationISOStorageRequest)},
			},
		},
	}
	if storageClass, exists := liveISO["storageClass"]; exists {
		value, ok := storageClass.(string)
		if !ok {
			return nil, fmt.Errorf("liveISO.storageClass must be a string")
		}
		if value != "" {
			claim.Spec.StorageClassName = &value
		}
	}
	return claim, nil
}

func (t *provisioningRequestReconcilerTask) ensureSeedGenerationISOPVC(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (bool, error) {
	expected, err := makeSeedGenerationISOPVC(t.object, snapshot)
	if err != nil {
		return false, err
	}
	actual := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}
	err = t.client.Get(ctx, key, actual)
	if apierrors.IsNotFound(err) {
		if err := t.client.Create(ctx, expected); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, fmt.Errorf("failed to create ISO workspace PVC %s: %w", expected.Name, err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get ISO workspace PVC %s: %w", expected.Name, err)
	}
	if actual.DeletionTimestamp != nil {
		return false, fmt.Errorf("workspace PVC %s is being deleted", actual.Name)
	}
	if err := validateSeedGenerationISOResourceOwner(actual, t.object, seedGenerationISOPVCKind); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(actual.Spec.AccessModes, expected.Spec.AccessModes) {
		return false, fmt.Errorf("workspace PVC %s has unexpected access modes", actual.Name)
	}
	requested, ok := actual.Spec.Resources.Requests[corev1.ResourceStorage]
	minimum := expected.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok || requested.Cmp(minimum) < 0 {
		return false, fmt.Errorf("workspace PVC %s is smaller than the required %s", actual.Name, seedGenerationISOStorageRequest)
	}
	if expected.Spec.StorageClassName != nil &&
		(actual.Spec.StorageClassName == nil || *actual.Spec.StorageClassName != *expected.Spec.StorageClassName) {
		return false, fmt.Errorf("workspace PVC %s uses a different storage class than the frozen configuration", actual.Name)
	}
	if actual.Status.Phase == corev1.ClaimLost {
		return false, fmt.Errorf("workspace PVC %s is lost", actual.Name)
	}
	// WaitForFirstConsumer requires the Job's Pod to exist before provisioning
	// and binding can start. Scheduling will wait for a usable claim.
	return true, nil
}

func makeSeedGenerationISOJob(
	pr *provisioningv1alpha1.ProvisioningRequest,
	snapshot *seedGenerationInputSnapshot,
	artifact *seedGenerationISOArtifact,
	releaseImage string,
	activeDeadline int64,
) (*batchv1.Job, error) {
	if snapshot == nil || snapshot.Document == nil || artifact == nil {
		return nil, fmt.Errorf("frozen live ISO inputs are incomplete")
	}
	toolsImage, err := validateSeedGenerationToolsImage(snapshot.Document.ToolsImage)
	if err != nil {
		return nil, err
	}
	digestSeparator := strings.LastIndex(releaseImage, "@")
	if digestSeparator < 0 || !seedGenerationDigestPattern.MatchString(releaseImage[digestSeparator+1:]) {
		return nil, fmt.Errorf("pinned release image digest is missing or invalid")
	}
	pullSecret := snapshot.Secrets["pullSecret"]
	uploadSecret := snapshot.Secrets["upload"]
	if pullSecret == nil || len(pullSecret.Data[corev1.DockerConfigJsonKey]) == 0 ||
		len(pullSecret.Data[seedGenerationISOConfigKey]) == 0 || uploadSecret == nil {
		return nil, fmt.Errorf("frozen live ISO credentials or installer configuration are missing")
	}
	config := snapshot.ConfigMap
	if config == nil {
		return nil, fmt.Errorf("frozen live ISO ConfigMap is missing")
	}
	liveISO, ok := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frozen liveISO configuration is missing")
	}
	jobName := seedGenerationISOJobName(pr)
	labels := seedGenerationISOResourceLabels(pr, seedGenerationISOJobKind)
	podLabels := seedGenerationISOResourceLabels(pr, seedGenerationISOPodKind)
	podLabels[seedGenerationISOJobLabel] = jobName
	workspaceName := seedGenerationISOPVCName(pr)
	configVolumeItems := []corev1.KeyToPath{
		{Key: corev1.DockerConfigJsonKey, Path: "config.json"},
		{Key: seedGenerationISOConfigKey, Path: seedGenerationISOConfigKey},
	}
	volumes := []corev1.Volume{
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: workspaceName,
			}},
		},
		{
			Name: "installer-config",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  pullSecret.Name,
				Items:       configVolumeItems,
				DefaultMode: k8sptr.To[int32](420),
				Optional:    k8sptr.To(false),
			}},
		},
		{
			Name: "upload-credentials",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: uploadSecret.Name,
				// Keep the projection readable by OpenShift's arbitrary nonroot UID.
				// The script copies the key to tmpfs and restricts it to mode 0600.
				DefaultMode: k8sptr.To[int32](420),
				Optional:    k8sptr.To(false),
			}},
		},
		{
			Name: "mirror-config",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: config.Name},
				Items:                []corev1.KeyToPath{{Key: seedGenerationSnapshotMirrorFile, Path: seedGenerationSnapshotMirrorFile}},
				DefaultMode:          k8sptr.To[int32](420),
				Optional:             k8sptr.To(false),
			}},
		},
		{
			Name: controllerTempVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory, SizeLimit: k8sptr.To(resource.MustParse("64Mi")),
			}},
		},
	}
	mounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "installer-config", MountPath: "/var/run/seedgen/config", ReadOnly: true},
		{Name: "upload-credentials", MountPath: "/var/run/seedgen/upload", ReadOnly: true},
		{Name: "mirror-config", MountPath: "/var/run/seedgen/mirror", ReadOnly: true},
		{Name: controllerTempVolumeName, MountPath: controllerTempMountPath},
	}
	if len(snapshot.Document.RegistryTrustedCAKeys) > 0 {
		registries := make([]string, 0, len(snapshot.Document.RegistryTrustedCAKeys))
		for registry := range snapshot.Document.RegistryTrustedCAKeys {
			registries = append(registries, registry)
		}
		// The document is assembled in sorted order, and the generated key
		// names are retained there so volume projection is deterministic.
		for i := 0; i < len(registries); i++ {
			for j := i + 1; j < len(registries); j++ {
				if registries[j] < registries[i] {
					registries[i], registries[j] = registries[j], registries[i]
				}
			}
		}
		items := make([]corev1.KeyToPath, 0, len(registries))
		for _, registry := range registries {
			items = append(items, corev1.KeyToPath{
				Key: snapshot.Document.RegistryTrustedCAKeys[registry], Path: registry + "/ca.crt",
			})
		}
		volumes = append(volumes, corev1.Volume{
			Name: "registry-certs",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: config.Name},
				Items:                items,
				DefaultMode:          k8sptr.To[int32](420),
				Optional:             k8sptr.To(false),
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: "registry-certs", MountPath: "/etc/containers/certs.d", ReadOnly: true})
	}
	env := []corev1.EnvVar{
		{Name: "HOME", Value: "/tmp/seedgen/home"},
		{Name: "TMPDIR", Value: "/workspace/tmp"},
		{Name: "XDG_CACHE_HOME", Value: "/workspace/cache"},
		{Name: "ISO_URL", Value: artifact.URL},
		{Name: "ISO_RELEASE_IMAGE", Value: releaseImage},
		{Name: "ISO_PR_UID", Value: string(pr.UID)},
	}
	if baseISOURL, exists := liveISO["baseISOURL"]; exists {
		value, ok := baseISOURL.(string)
		if !ok || !validSeedGenerationISOHTTPSURL(value) {
			return nil, fmt.Errorf("frozen liveISO.baseISOURL is invalid")
		}
		env = append(env, corev1.EnvVar{Name: "OPENSHIFT_INSTALL_OS_IMAGE_OVERRIDE", Value: value})
	}
	backoffLimit := int32(0)
	completions := int32(1)
	parallelism := int32(1)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            jobName,
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoffLimit,
			Completions:           &completions,
			Parallelism:           &parallelism,
			ActiveDeadlineSeconds: &activeDeadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:          podLabels,
					OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken:  k8sptr.To(false),
					RestartPolicy:                 corev1.RestartPolicyNever,
					TerminationGracePeriodSeconds: k8sptr.To[int64](30),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   k8sptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Volumes: volumes,
					Containers: []corev1.Container{{
						Name:                     seedGenerationISOContainer,
						Image:                    toolsImage,
						ImagePullPolicy:          corev1.PullIfNotPresent,
						Command:                  []string{"/bin/bash", "-c"},
						Args:                     []string{seedGenerationISOJobScript()},
						WorkingDir:               "/workspace",
						Env:                      env,
						VolumeMounts:             mounts,
						TerminationMessagePath:   "/dev/termination-log",
						TerminationMessagePolicy: corev1.TerminationMessageReadFile,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: k8sptr.To(false),
							ReadOnlyRootFilesystem:   k8sptr.To(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{controllerDropAllCaps}},
						},
					}},
				},
			},
		},
	}
	return job, nil
}

//go:embed scripts/seed-generation-iso.sh
var seedGenerationISOJobCommand string

func seedGenerationISOJobScript() string {
	return seedGenerationISOJobCommand
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOCurrentTime() time.Time {
	if t.seedGenerationISONow != nil {
		return t.seedGenerationISONow()
	}
	return time.Now()
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationISO(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (ctrl.Result, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if !status.DetachedCleanupCompleted {
		return requeueWithShortInterval(), nil
	}
	if status.SeedImage == "" || status.ReleaseImageDigest == "" {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"verified seed image or pinned release image is missing before live ISO generation")
	}
	upload := snapshot.Secrets["upload"]
	liveISO, ok := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	if !ok {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"frozen liveISO configuration is missing")
	}
	artifact, err := seedGenerationISOArtifactForRun(t.object, liveISO, upload)
	if err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			fmt.Sprintf("live ISO publication inputs are invalid: %v", err))
	}
	if !status.ISOJobSubmitted {
		ready, err := t.ensureSeedGenerationISOPVC(ctx, snapshot)
		if err != nil {
			return t.handleSeedGenerationLifecycleError(ctx, "failed to prepare live ISO workspace", err)
		}
		if !ready {
			if err := t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.BuildingISO,
				"Waiting for live ISO workspace storage"); err != nil {
				return requeueWithError(err)
			}
			return requeueWithMediumInterval(), nil
		}
		remaining := status.StartedAt.Add(time.Duration(status.TimeoutSeconds) * time.Second).Sub(t.seedGenerationISOCurrentTime())
		if remaining <= 0 {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.TimedOut,
				"Seed generation timed out while preparing the live ISO workspace")
		}
		activeDeadline := int64(remaining / time.Second)
		if activeDeadline < 1 {
			activeDeadline = 1
		}
		job, err := makeSeedGenerationISOJob(t.object, snapshot, artifact, status.ReleaseImageDigest, activeDeadline)
		if err != nil {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
				fmt.Sprintf("live ISO Job configuration is invalid: %v", err))
		}
		now := metav1.NewTime(t.seedGenerationISOCurrentTime())
		status.ISOJobSubmitted = true
		status.ISOJobSubmittedAt = &now
		setSeedGenerationProgress(t.object, provisioningv1alpha1.CRconditionReasons.BuildingISO,
			"Live ISO build Job has been submitted")
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			status.ISOJobSubmitted = false
			status.ISOJobSubmittedAt = nil
			return requeueWithError(fmt.Errorf("failed to record live ISO Job submission intent: %w", err))
		}
		// Updating the resource can replace its nested status objects. Refresh
		// the pointer before recording rejection or the created Job's UID.
		status = t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
		if err := t.client.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsBadRequest(err) ||
				apierrors.IsTooManyRequests(err) {
				// These responses confirm that the Job was not created. Clear the
				// checkpoint so reconciliation retries Create instead of waiting
				// through the Job adoption window.
				status.ISOJobSubmitted = false
				status.ISOJobSubmittedAt = nil
				setSeedGenerationProgress(t.object, provisioningv1alpha1.CRconditionReasons.BuildingISO,
					"Retrying live ISO build Job creation after API rejection")
				if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
					return requeueWithError(fmt.Errorf("failed to clear live ISO Job submission intent: %w", updateErr))
				}
				return requeueWithError(fmt.Errorf("failed to create live ISO build Job: %w", err))
			}
			return t.handleSeedGenerationLifecycleError(ctx, "failed to create live ISO build Job", err)
		}
		if job.UID != "" {
			status.ISOJobUID = string(job.UID)
			if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				return requeueWithError(fmt.Errorf("failed to record live ISO Job UID: %w", err))
			}
		}
		return requeueWithShortInterval(), nil
	}
	return t.observeSeedGenerationISOJob(ctx, artifact)
}

func (t *provisioningRequestReconcilerTask) observeSeedGenerationISOJob(
	ctx context.Context,
	artifact *seedGenerationISOArtifact,
) (ctrl.Result, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	job := &batchv1.Job{}
	key := types.NamespacedName{Name: seedGenerationISOJobName(t.object), Namespace: seedGenerationSnapshotNamespace()}
	if err := t.client.Get(ctx, key, job); err != nil {
		if !apierrors.IsNotFound(err) {
			return requeueWithError(fmt.Errorf("failed to get live ISO build Job %s: %w", key.Name, err))
		}
		if status.ISOJobSubmittedAt != nil && t.seedGenerationISOCurrentTime().Sub(status.ISOJobSubmittedAt.Time) < seedGenerationISOJobAdoptionWindow {
			return requeueWithShortInterval(), nil
		}
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"submitted live ISO build Job is missing; it will not be resubmitted")
	}
	if err := validateSeedGenerationISOResourceOwner(job, t.object, seedGenerationISOJobKind); err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed, err.Error())
	}
	if job.DeletionTimestamp != nil {
		return requeueWithShortInterval(), nil
	}
	if status.ISOJobUID != "" && status.ISOJobUID != string(job.UID) {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"live ISO build Job UID does not match the persisted run state")
	}
	if !status.ISOJobSubmitted {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"live ISO build Job exists without persisted submission intent")
	}
	expectedSnapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err != nil {
		return t.handleSeedGenerationLifecycleError(ctx, "failed to load live ISO inputs", err)
	}
	expected, err := makeSeedGenerationISOJob(t.object, expectedSnapshot, artifact,
		status.ReleaseImageDigest, max(int64(1), status.TimeoutSeconds))
	if err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			fmt.Sprintf("live ISO build Job inputs are invalid: %v", err))
	}
	if err := validateSeedGenerationISOJob(job, expected); err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			fmt.Sprintf("live ISO build Job does not match frozen inputs: %v", err))
	}
	if status.ISOJobUID == "" {
		status.ISOJobUID = string(job.UID)
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return requeueWithError(fmt.Errorf("failed to pin live ISO build Job UID: %w", err))
		}
		return requeueWithShortInterval(), nil
	}
	// A duplicate Pod may fail or be terminated after another Pod succeeds.
	// Prefer a successful Pod's verified result over failures from other attempts.
	if job.Status.Succeeded == 0 && (job.Status.Failed > 0 || seedGenerationISOJobFailed(job)) {
		return t.failSeedGenerationISOJob(ctx, seedGenerationISOJobFailureReason(job),
			seedGenerationISOJobFailureMessage(job), job, expectedSnapshot)
	}
	if job.Status.Succeeded == 0 {
		message := "Waiting for the live ISO build Job to start"
		if job.Status.Active > 0 {
			message, err = t.seedGenerationISOJobProgress(ctx, job)
			if err != nil {
				return requeueWithError(fmt.Errorf("failed to read live ISO build progress: %w", err))
			}
		}
		if err := t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.BuildingISO, message); err != nil {
			return requeueWithError(err)
		}
		return requeueWithMediumInterval(), nil
	}
	result, ready, err := t.seedGenerationISOJobResult(ctx, job, artifact.URL)
	if err != nil {
		return t.failSeedGenerationISOJob(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			fmt.Sprintf("live ISO build Job completed without a valid result: %v", err), job, expectedSnapshot)
	}
	if !ready {
		return requeueWithShortInterval(), nil
	}
	return t.completeSeedGenerationISO(ctx, expectedSnapshot, result)
}

func validateSeedGenerationISOJob(actual, expected *batchv1.Job) error {
	if err := validateSeedGenerationISOJobIdentity(actual, expected); err != nil {
		return err
	}
	if err := validateSeedGenerationISOJobPodSpec(actual, expected); err != nil {
		return err
	}
	if err := validateSeedGenerationISOJobContainer(actual, expected); err != nil {
		return err
	}
	return validateSeedGenerationISOJobExecutionPolicy(actual, expected)
}

func validateSeedGenerationISOJobIdentity(actual, expected *batchv1.Job) error {
	if actual.Name != expected.Name || actual.Namespace != expected.Namespace {
		return fmt.Errorf("job identity does not match")
	}
	for key, value := range expected.Spec.Template.Labels {
		if actual.Spec.Template.Labels[key] != value {
			return fmt.Errorf("job pod label %s does not match", key)
		}
	}
	if len(actual.Spec.Template.Spec.Containers) != 1 || len(expected.Spec.Template.Spec.Containers) != 1 {
		return fmt.Errorf("job must have one container")
	}
	return nil
}

func validateSeedGenerationISOJobPodSpec(actual, expected *batchv1.Job) error {
	got, want := actual.Spec.Template.Spec, expected.Spec.Template.Spec
	if got.RestartPolicy != corev1.RestartPolicyNever || got.AutomountServiceAccountToken == nil || *got.AutomountServiceAccountToken {
		return fmt.Errorf("pod restart or service account token settings do not match")
	}
	if got.ServiceAccountName != want.ServiceAccountName || got.HostNetwork != want.HostNetwork ||
		got.HostPID != want.HostPID || got.HostIPC != want.HostIPC {
		return fmt.Errorf("pod service account or host namespace settings do not match")
	}
	if !reflect.DeepEqual(got.ImagePullSecrets, want.ImagePullSecrets) || !reflect.DeepEqual(got.SecurityContext, want.SecurityContext) {
		return fmt.Errorf("pod security settings do not match")
	}
	if !reflect.DeepEqual(got.Volumes, want.Volumes) {
		return fmt.Errorf("pod security settings or volumes do not match")
	}
	return nil
}

func validateSeedGenerationISOJobContainer(actual, expected *batchv1.Job) error {
	got, want := actual.Spec.Template.Spec, expected.Spec.Template.Spec
	actualContainer, expectedContainer := got.Containers[0], want.Containers[0]
	if actualContainer.Name != expectedContainer.Name || actualContainer.Image != expectedContainer.Image ||
		!reflect.DeepEqual(actualContainer.Command, expectedContainer.Command) ||
		!reflect.DeepEqual(actualContainer.Args, expectedContainer.Args) ||
		!reflect.DeepEqual(actualContainer.Env, expectedContainer.Env) ||
		!reflect.DeepEqual(actualContainer.VolumeMounts, expectedContainer.VolumeMounts) ||
		!reflect.DeepEqual(actualContainer.SecurityContext, expectedContainer.SecurityContext) ||
		actualContainer.TerminationMessagePath != expectedContainer.TerminationMessagePath ||
		actualContainer.TerminationMessagePolicy != expectedContainer.TerminationMessagePolicy {
		return fmt.Errorf("container image, command, environment, or mounts do not match")
	}
	return nil
}

func validateSeedGenerationISOJobExecutionPolicy(actual, expected *batchv1.Job) error {
	if actual.Spec.BackoffLimit == nil || *actual.Spec.BackoffLimit != 0 ||
		actual.Spec.Completions == nil || *actual.Spec.Completions != 1 ||
		actual.Spec.Parallelism == nil || *actual.Spec.Parallelism != 1 ||
		actual.Spec.ActiveDeadlineSeconds == nil || *actual.Spec.ActiveDeadlineSeconds <= 0 ||
		*actual.Spec.ActiveDeadlineSeconds > *expected.Spec.ActiveDeadlineSeconds ||
		!reflect.DeepEqual(actual.Spec.TTLSecondsAfterFinished, expected.Spec.TTLSecondsAfterFinished) {
		return fmt.Errorf("job retry, deadline, or completion settings do not match")
	}
	return nil
}

func seedGenerationISOJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func seedGenerationISOJobFailureMessage(job *batchv1.Job) string {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			if condition.Reason == "DeadlineExceeded" {
				return "Live ISO build Job exceeded its active deadline"
			}
			return "Live ISO build Job failed"
		}
	}
	if job.Status.Failed > 0 {
		return "Live ISO build Job failed"
	}
	return "Live ISO build Job did not complete successfully"
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOJobProgress(
	ctx context.Context,
	job *batchv1.Job,
) (string, error) {
	pods := &corev1.PodList{}
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationISOResourceLabel:                    "true",
		seedGenerationISOJobLabel:                         job.Name,
	}
	if err := t.client.List(ctx, pods, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return "", fmt.Errorf("failed to list ISO build Pods: %w", err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := validateSeedGenerationISOPodOwner(pod, t.object, job.Name, job.UID, false); err != nil {
			return "", err
		}
		if pod.Status.Phase == corev1.PodPending {
			continue
		}
		logs, err := readSeedGenerationISOPodLogs(ctx, pod.Namespace, pod.Name)
		if err != nil {
			return "", fmt.Errorf("failed to read ISO build Pod progress: %w", err)
		}
		if message := seedGenerationISOProgressMessage(logs); message != "" {
			return message, nil
		}
	}
	return "Generating the live ISO", nil
}

func seedGenerationISOProgressMessage(logs []byte) string {
	var last string
	for _, line := range strings.Split(string(logs), "\n") {
		switch strings.TrimSpace(line) {
		case "SEEDGEN_STATUS=extracting-release":
			last = "Extracting the installer from the pinned release image"
		case "SEEDGEN_STATUS=building-iso":
			last = "Generating the live ISO"
		case "SEEDGEN_STATUS=uploading-iso":
			last = "Uploading the live ISO to the configured server"
		case "SEEDGEN_STATUS=verifying-https":
			last = "Verifying the published ISO bytes over HTTPS"
		}
	}
	return last
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOJobResult(
	ctx context.Context,
	job *batchv1.Job,
	expectedURL string,
) (seedGenerationISOResult, bool, error) {
	pods := &corev1.PodList{}
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationISOResourceLabel:                    "true",
		seedGenerationISOJobLabel:                         job.Name,
	}
	if err := t.client.List(ctx, pods, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return seedGenerationISOResult{}, false, fmt.Errorf("failed to list completed ISO build Pods: %w", err)
	}
	var result seedGenerationISOResult
	ready := false
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := validateSeedGenerationISOPodOwner(pod, t.object, job.Name, job.UID, false); err != nil {
			return seedGenerationISOResult{}, false, err
		}
		if pod.Status.Phase != corev1.PodSucceeded {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != seedGenerationISOContainer || status.State.Terminated == nil || status.State.Terminated.ExitCode != 0 {
				continue
			}
			observed, err := parseSeedGenerationISOJobResult(status.State.Terminated.Message, expectedURL)
			if err != nil {
				return seedGenerationISOResult{}, false, err
			}
			if ready && observed != result {
				return seedGenerationISOResult{}, false, fmt.Errorf("successful ISO build Pods reported different results")
			}
			result, ready = observed, true
		}
	}
	return result, ready, nil
}

type seedGenerationISOResult struct {
	URL    string
	Digest string
}

func parseSeedGenerationISOJobResult(message, expectedURL string) (seedGenerationISOResult, error) {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if len(lines) != 2 || lines[0] != expectedURL || !seedGenerationDigestPattern.MatchString(lines[1]) {
		return seedGenerationISOResult{}, fmt.Errorf("job result did not contain the expected HTTPS URL and a SHA-256 digest")
	}
	return seedGenerationISOResult{URL: lines[0], Digest: lines[1]}, nil
}

func (t *provisioningRequestReconcilerTask) completeSeedGenerationISO(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
	result seedGenerationISOResult,
) (ctrl.Result, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status.ISOURL != "" && status.ISOURL != result.URL {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"verified live ISO URL does not match the persisted run result")
	}
	if status.ISODigest != "" && status.ISODigest != result.Digest {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"verified live ISO digest does not match the persisted run result")
	}
	caRef, hasCA, err := t.ensureSeedGenerationISOCAConfigMap(ctx, snapshot)
	if err != nil {
		return t.handleSeedGenerationLifecycleError(ctx, "failed to persist live ISO HTTPS trust", err)
	}
	status.ISOURL = result.URL
	status.ISODigest = result.Digest
	status.ISOServerCACertRef = nil
	if hasCA {
		status.ISOServerCACertRef = caRef
	}
	message := "Live ISO generation, upload, and HTTPS content verification completed"
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Completed, metav1.ConditionTrue, message)
	ctlrutils.SetProvisioningStateFulfilled(t.object)
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return requeueWithError(fmt.Errorf("failed to record live ISO generation completion: %w", err))
	}
	return requeueWithShortInterval(), nil
}

func (t *provisioningRequestReconcilerTask) ensureSeedGenerationISOCAConfigMap(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (*provisioningv1alpha1.ConfigMapKeyRef, bool, error) {
	upload := snapshot.Secrets["upload"]
	caCert := upload.Data["caCert"]
	if len(caCert) == 0 {
		return nil, false, nil
	}
	name := seedGenerationResourceName(t.object, "iso-ca")
	expected := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          seedGenerationISOResourceLabels(t.object, seedGenerationISOCAKind),
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(t.object)},
		},
		Immutable: k8sptr.To(true),
		Data:      map[string]string{seedGenerationISOCAKey: string(caCert)},
	}
	actual := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}
	if err := t.client.Get(ctx, key, actual); apierrors.IsNotFound(err) {
		if err := t.client.Create(ctx, expected); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, false, fmt.Errorf("failed to create live ISO CA ConfigMap: %w", err)
		}
		return &provisioningv1alpha1.ConfigMapKeyRef{Name: name, Namespace: expected.Namespace, Key: seedGenerationISOCAKey}, true, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get live ISO CA ConfigMap: %w", err)
	}
	if err := validateSeedGenerationISOResourceOwner(actual, t.object, seedGenerationISOCAKind); err != nil {
		return nil, false, err
	}
	if actual.DeletionTimestamp != nil || !reflect.DeepEqual(actual.Data, expected.Data) {
		return nil, false, fmt.Errorf("live ISO CA ConfigMap does not match the frozen upload trust")
	}
	return &provisioningv1alpha1.ConfigMapKeyRef{Name: name, Namespace: expected.Namespace, Key: seedGenerationISOCAKey}, true, nil
}

func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationISOResources(
	ctx context.Context,
	preserveCA bool,
	preserveDiagnostics bool,
) (bool, error) {
	pending, err := t.deleteSeedGenerationISOJobAndPods(ctx)
	if err != nil || pending {
		return pending, err
	}
	pending, err = t.cleanupSeedGenerationISOUpload(ctx)
	if err != nil || pending {
		return pending, err
	}
	return t.deleteSeedGenerationISOResources(ctx, preserveCA,
		preserveDiagnostics && t.seedGenerationISOFailureRetentionDelay() > 0,
		&corev1.PersistentVolumeClaimList{}, &corev1.ConfigMapList{})
}

// Validate the Pods before deleting their Job. Delete the Job first so it
// cannot replace a Pod while cleanup is in progress. A Pod orphaned by an
// earlier cleanup is checked against the Job UID persisted in the run status.
func (t *provisioningRequestReconcilerTask) deleteSeedGenerationISOJobAndPods(ctx context.Context) (bool, error) {
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationISOResourceLabel:                    "true",
	}
	jobs := &batchv1.JobList{}
	if err := t.client.List(ctx, jobs, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return false, fmt.Errorf("failed to list live ISO build Jobs: %w", err)
	}
	pods := &corev1.PodList{}
	if err := t.client.List(ctx, pods, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return false, fmt.Errorf("failed to list live ISO build Pods: %w", err)
	}
	jobName := seedGenerationISOJobName(t.object)
	var jobUID types.UID
	if details := t.object.Status.Extensions.ClusterDetails; details != nil && details.SeedGenerationStatus != nil {
		jobUID = types.UID(details.SeedGenerationStatus.ISOJobUID)
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.Name != jobName || job.UID == "" {
			return false, fmt.Errorf("unexpected live ISO build Job %s/%s", job.Namespace, job.Name)
		}
		if err := validateSeedGenerationISOResourceOwner(job, t.object, seedGenerationISOJobKind); err != nil {
			return false, err
		}
		if jobUID != "" && jobUID != job.UID {
			return false, fmt.Errorf("live ISO build Job UID does not match the persisted run state")
		}
		jobUID = job.UID
	}
	for i := range pods.Items {
		if err := validateSeedGenerationISOPodOwner(&pods.Items[i], t.object, jobName, jobUID, true); err != nil {
			return false, err
		}
	}
	// Cleanup can start before the creation response's UID was saved. Pin an
	// adopted Job before deleting it so an interruption cannot strand its Pods.
	if jobUID != "" {
		details := t.object.Status.Extensions.ClusterDetails
		if details == nil || details.SeedGenerationStatus == nil {
			return false, fmt.Errorf("live ISO cleanup has no persisted run state")
		}
		status := details.SeedGenerationStatus
		if status.ISOJobUID == "" {
			status.ISOJobUID = string(jobUID)
			if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				status.ISOJobUID = ""
				return false, fmt.Errorf("failed to pin live ISO build Job UID before cleanup: %w", err)
			}
		}
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.DeletionTimestamp == nil {
			if err := client.IgnoreNotFound(t.client.Delete(ctx, job)); err != nil {
				return false, fmt.Errorf("failed to delete live ISO build Job %s: %w", job.Name, err)
			}
		}
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp == nil {
			if err := client.IgnoreNotFound(t.client.Delete(ctx, pod)); err != nil {
				return false, fmt.Errorf("failed to delete live ISO build Pod %s: %w", pod.Name, err)
			}
		}
	}
	return len(jobs.Items) > 0 || len(pods.Items) > 0, nil
}

func (t *provisioningRequestReconcilerTask) deleteSeedGenerationISOResources(
	ctx context.Context,
	preserveCA, retainFailureDiagnostics bool,
	lists ...client.ObjectList,
) (bool, error) {
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationISOResourceLabel:                    "true",
	}
	pending := false
	for _, list := range lists {
		if err := t.client.List(ctx, list, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
			return false, fmt.Errorf("failed to list live ISO resources: %w", err)
		}
		objects, err := meta.ExtractList(list)
		if err != nil {
			return false, fmt.Errorf("failed to inspect live ISO resources: %w", err)
		}
		for _, object := range objects {
			resource, ok := object.(client.Object)
			if !ok {
				return false, fmt.Errorf("live ISO resource does not implement client.Object")
			}
			kind := resource.GetLabels()[seedGenerationISOResourceKindLabel]
			if err := validateSeedGenerationISOResourceOwner(resource, t.object, kind); err != nil {
				return false, err
			}
			if preserveCA && kind == seedGenerationISOCAKind {
				continue
			}
			if retainFailureDiagnostics && kind == seedGenerationISODiagnosticsKind {
				continue
			}
			pending = true
			if resource.GetDeletionTimestamp() == nil {
				if err := client.IgnoreNotFound(t.client.Delete(ctx, resource)); err != nil {
					return false, fmt.Errorf("failed to delete live ISO resource %s: %w", resource.GetName(), err)
				}
			}
		}
	}
	return pending, nil
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOCompleted() bool {
	condition := seedGenerationCondition(t.object)
	return condition != nil && condition.Status == metav1.ConditionTrue &&
		condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Completed)
}
