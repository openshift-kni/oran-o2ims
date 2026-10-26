/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sptr "k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationISODiagnosticsKind           = "diagnostics"
	seedGenerationISODiagnosticsKey            = "build.log"
	seedGenerationISODiagnosticsRetention      = 24 * time.Hour
	seedGenerationISODiagnosticsLogLimit       = 256 << 10
	seedGenerationISODiagnosticsMessageLimit   = 4 << 10
	seedGenerationISODiagnosticsRequeueMaximum = time.Hour
)

var (
	seedGenerationSensitiveAssignmentPattern = regexp.MustCompile(`(?im)^.*(?:authorization|password|token|secret|credential|private[_ -]?key|auth)\s*[:=].*$`)
	seedGenerationBearerPattern              = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/-]+=*`)
	seedGenerationURLCredentialPattern       = regexp.MustCompile(`(?i)(https?://)[^/@\s]+:[^/@\s]+@`)
	seedGenerationPrivateKeyPattern          = regexp.MustCompile(`(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)
)

func seedGenerationISODiagnosticsName(pr *provisioningv1alpha1.ProvisioningRequest) string {
	return seedGenerationResourceName(pr, "iso-diagnostics")
}

func (t *provisioningRequestReconcilerTask) failSeedGenerationISOTimeout(
	ctx context.Context,
	message string,
) (ctrl.Result, error) {
	if t.object.Status.Extensions.ClusterDetails != nil {
		status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
		if status != nil && status.ISOJobSubmitted {
			job := &batchv1.Job{}
			key := types.NamespacedName{Name: seedGenerationISOJobName(t.object), Namespace: seedGenerationSnapshotNamespace()}
			if err := t.client.Get(ctx, key, job); err == nil &&
				validateSeedGenerationISOResourceOwner(job, t.object, seedGenerationISOJobKind) == nil &&
				(status.ISOJobUID == "" || status.ISOJobUID == string(job.UID)) {
				snapshot, snapshotErr := t.loadSeedGenerationInputSnapshot(ctx)
				if snapshotErr == nil {
					return t.failSeedGenerationISOJob(ctx, provisioningv1alpha1.CRconditionReasons.TimedOut,
						message, job, snapshot)
				}
			}
		}
	}
	return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.TimedOut, message)
}

func seedGenerationISOJobFailureReason(job *batchv1.Job) provisioningv1alpha1.ConditionReason {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue &&
			condition.Reason == "DeadlineExceeded" {
			return provisioningv1alpha1.CRconditionReasons.TimedOut
		}
	}
	return provisioningv1alpha1.CRconditionReasons.Failed
}

func (t *provisioningRequestReconcilerTask) failSeedGenerationISOJob(
	ctx context.Context,
	reason provisioningv1alpha1.ConditionReason,
	message string,
	job *batchv1.Job,
	snapshot *seedGenerationInputSnapshot,
) (ctrl.Result, error) {
	var status *provisioningv1alpha1.SeedGenerationStatus
	if details := t.object.Status.Extensions.ClusterDetails; details != nil {
		status = details.SeedGenerationStatus
	}
	if status != nil {
		var summary string
		if status.ISOFailureDiagnosticsRef == nil && job != nil && snapshot != nil {
			logs, failure, err := t.seedGenerationISOJobFailureLogs(ctx, job)
			if failure != "" && reason == provisioningv1alpha1.CRconditionReasons.Failed {
				message = failure
			}
			if err != nil {
				t.logger.WarnContext(ctx, "Unable to read failed live ISO build logs", slog.Any("error", err))
			}
			if len(logs) > 0 {
				redacted := redactSeedGenerationISODiagnostics(logs, snapshot)
				retained := []byte(seedGenerationISODiagnosticsTail(redacted, seedGenerationISODiagnosticsLogLimit))
				if len(retained) > 0 {
					summary = seedGenerationISODiagnosticsTail(retained, seedGenerationISODiagnosticsMessageLimit)
					ref, err := t.createSeedGenerationISODiagnostics(ctx, retained)
					if err != nil {
						t.logger.WarnContext(ctx, "Unable to retain redacted live ISO build diagnostics", slog.Any("error", err))
					} else {
						status.ISOFailureDiagnosticsRef = ref
						expiresAt := metav1.NewTime(time.Now().Add(seedGenerationISODiagnosticsRetention))
						status.ISOFailureDiagnosticsExpiresAt = &expiresAt
					}
				}
			}
		}
		if status.ISOFailureDiagnosticsRef != nil && status.ISOFailureDiagnosticsExpiresAt == nil {
			expiresAt := metav1.NewTime(time.Now().Add(seedGenerationISODiagnosticsRetention))
			status.ISOFailureDiagnosticsExpiresAt = &expiresAt
		}
		if ref := status.ISOFailureDiagnosticsRef; ref != nil {
			message += fmt.Sprintf("; redacted build logs are in ConfigMap %s/%s",
				ref.Namespace, ref.Name)
			if status.ISOFailureDiagnosticsExpiresAt != nil {
				message += fmt.Sprintf(" until %s", status.ISOFailureDiagnosticsExpiresAt.UTC().Format(time.RFC3339))
			}
		} else if job != nil {
			message += "; redacted build logs could not be retained"
		}
		if summary != "" {
			message += "\nRedacted log tail:\n" + summary
		}
	}
	return t.failSeedGeneration(ctx, reason, message)
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOJobFailureLogs(
	ctx context.Context, job *batchv1.Job,
) ([]byte, string, error) {
	pods := &corev1.PodList{}
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationISOResourceLabel:                    "true",
		seedGenerationISOJobLabel:                         job.Name,
	}
	if err := t.client.List(ctx, pods, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return nil, "", fmt.Errorf("failed to list live ISO build Pods: %w", err)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	var logs strings.Builder
	var failure string
	var readErrors error
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := validateSeedGenerationISOPodOwner(pod, t.object, job.Name, job.UID, false); err != nil {
			return nil, "", err
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodUnknown {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == seedGenerationISOContainer && status.State.Terminated != nil {
				if message := seedGenerationISOFailureMessage(status.State.Terminated.Message); message != "" {
					failure = message
					logs.WriteString(message + "\n")
				}
			}
		}
		podLogs, err := readSeedGenerationISOPodLogs(ctx, pod.Namespace, pod.Name)
		if err != nil {
			readErrors = errors.Join(readErrors, fmt.Errorf("failed to read live ISO build Pod %s logs: %w", pod.Name, err))
			continue
		}
		logs.Write(podLogs)
		logs.WriteString("\n")
	}
	return []byte(logs.String()), failure, readErrors
}

// Only fixed stage codes are trusted; arbitrary termination text can contain
// secrets or a result URL that was never verified.
func seedGenerationISOFailureMessage(message string) string {
	stages := map[string]string{
		"initializing":                "initializing the build workspace",
		"loading-build-result":        "loading a previously verified build result",
		"loading-https-trust":         "loading HTTPS trust",
		"preparing-upload":            "loading the upload configuration",
		"connecting-to-upload-server": "checking SSH connectivity, authentication, and the upload server host key",
		"preparing-remote-directory":  "creating the remote upload directory",
		"preparing-remote-file":       "reserving a new remote run directory and upload file",
		"extracting-installer":        "extracting the installer from the pinned release image",
		"building-iso":                "generating the live ISO",
		"uploading-iso":               "uploading the live ISO over SSH",
		"checking-remote-digest":      "verifying the remote upload SHA-256 checksum (unreadable file or checksum mismatch)",
		"publishing-remote-file":      "publishing the verified remote file",
		"verifying-https-content":     "retrieving the published ISO over HTTPS",
		"checking-https-digest":       "verifying the HTTPS content SHA-256 checksum (checksum mismatch)",
	}
	stage, ok := strings.CutPrefix(strings.TrimSpace(message), "SEEDGEN_FAILURE=")
	if !ok {
		return ""
	}
	if description, ok := stages[stage]; ok {
		return "Live ISO build failed while " + description
	}
	return ""
}

func redactSeedGenerationISODiagnostics(logs []byte, snapshot *seedGenerationInputSnapshot) []byte {
	redacted := string(logs)
	var credentials []string
	for _, secret := range snapshot.Secrets {
		if secret == nil {
			continue
		}
		for _, value := range secret.Data {
			if len(value) == 0 {
				continue
			}
			credentials = append(credentials, string(value), base64.StdEncoding.EncodeToString(value))
			var decoded any
			if json.Unmarshal(value, &decoded) == nil {
				credentials = appendSeedGenerationJSONStrings(credentials, decoded)
			}
		}
	}
	sort.Slice(credentials, func(i, j int) bool { return len(credentials[i]) > len(credentials[j]) })
	for _, credential := range credentials {
		redacted = strings.ReplaceAll(redacted, credential, "[REDACTED]")
	}
	redacted = seedGenerationPrivateKeyPattern.ReplaceAllString(redacted, "[REDACTED PRIVATE KEY]")
	redacted = seedGenerationURLCredentialPattern.ReplaceAllString(redacted, `${1}[REDACTED]@`)
	redacted = seedGenerationBearerPattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	redacted = seedGenerationSensitiveAssignmentPattern.ReplaceAllString(redacted, "[REDACTED SENSITIVE LOG LINE]")
	validUTF8 := strings.ToValidUTF8(redacted, "�")
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			return r
		}
		return -1
	}, validUTF8)
	return []byte(clean)
}

func appendSeedGenerationJSONStrings(values []string, decoded any) []string {
	switch typed := decoded.(type) {
	case string:
		if typed != "" {
			values = append(values, typed)
		}
	case []any:
		for _, item := range typed {
			values = appendSeedGenerationJSONStrings(values, item)
		}
	case map[string]any:
		for _, item := range typed {
			values = appendSeedGenerationJSONStrings(values, item)
		}
	}
	return values
}

func seedGenerationISODiagnosticsTail(logs []byte, limit int) string {
	if limit <= 0 || len(logs) == 0 {
		return ""
	}
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
		if newline := strings.IndexByte(string(logs), '\n'); newline >= 0 {
			logs = logs[newline+1:]
		}
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(logs), "�"))
}

func (t *provisioningRequestReconcilerTask) createSeedGenerationISODiagnostics(
	ctx context.Context,
	logs []byte,
) (*provisioningv1alpha1.ConfigMapKeyRef, error) {
	name := seedGenerationISODiagnosticsName(t.object)
	expected := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          seedGenerationISOResourceLabels(t.object, seedGenerationISODiagnosticsKind),
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(t.object)},
		},
		Immutable: k8sptr.To(true),
		Data:      map[string]string{seedGenerationISODiagnosticsKey: string(logs)},
	}
	actual := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}
	err := t.client.Get(ctx, key, actual)
	if apierrors.IsNotFound(err) {
		if err := t.client.Create(ctx, expected); err == nil {
			actual = expected
		} else if apierrors.IsAlreadyExists(err) {
			if getErr := t.client.Get(ctx, key, actual); getErr != nil {
				return nil, fmt.Errorf("failed to get existing redacted live ISO diagnostics ConfigMap: %w", getErr)
			}
		} else {
			return nil, fmt.Errorf("failed to create redacted live ISO diagnostics ConfigMap: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get redacted live ISO diagnostics ConfigMap: %w", err)
	}
	if err := validateSeedGenerationISOResourceOwner(actual, t.object, seedGenerationISODiagnosticsKind); err != nil {
		return nil, err
	}
	if actual.DeletionTimestamp != nil || actual.Immutable == nil || !*actual.Immutable ||
		actual.Data[seedGenerationISODiagnosticsKey] != string(logs) {
		return nil, fmt.Errorf("redacted live ISO diagnostics ConfigMap does not match this failed run")
	}
	return &provisioningv1alpha1.ConfigMapKeyRef{
		Name: name, Namespace: expected.Namespace, Key: seedGenerationISODiagnosticsKey,
	}, nil
}

func (t *provisioningRequestReconcilerTask) seedGenerationISOFailureRetentionDelay() time.Duration {
	if t.object == nil || t.object.Status.Extensions.ClusterDetails == nil ||
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus == nil {
		return 0
	}
	expiresAt := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.ISOFailureDiagnosticsExpiresAt
	if expiresAt == nil {
		return 0
	}
	remaining := time.Until(expiresAt.Time)
	if remaining <= 0 {
		return 0
	}
	return min(remaining, seedGenerationISODiagnosticsRequeueMaximum)
}
