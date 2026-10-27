/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// The v1 projection and encoding are a persisted contract. Future formats must
// use a new version while retaining validation of existing v1 fingerprints.
// Exclude API-managed labels, selectors, and defaulted scheduling settings.
// Mutable execution controls are checked separately against the run deadline.
func seedGenerationISOJobSpecHashV1(job *batchv1.Job) (string, error) {
	if job == nil || len(job.Spec.Template.Spec.Containers) != 1 {
		return "", fmt.Errorf("live ISO Job must have one container")
	}
	pod := job.Spec.Template.Spec
	container := pod.Containers[0]
	contract := corev1.PodSpec{
		RestartPolicy:                pod.RestartPolicy,
		AutomountServiceAccountToken: pod.AutomountServiceAccountToken,
		ServiceAccountName:           pod.ServiceAccountName,
		HostNetwork:                  pod.HostNetwork,
		HostPID:                      pod.HostPID,
		HostIPC:                      pod.HostIPC,
		ImagePullSecrets:             pod.ImagePullSecrets,
		SecurityContext:              pod.SecurityContext,
		Volumes:                      pod.Volumes,
		InitContainers:               pod.InitContainers,
		EphemeralContainers:          pod.EphemeralContainers,
		Containers: []corev1.Container{{
			Name:                     container.Name,
			Image:                    container.Image,
			Command:                  container.Command,
			Args:                     container.Args,
			Env:                      container.Env,
			VolumeMounts:             container.VolumeMounts,
			SecurityContext:          container.SecurityContext,
			TerminationMessagePath:   container.TerminationMessagePath,
			TerminationMessagePolicy: container.TerminationMessagePolicy,
		}},
	}
	data, err := json.Marshal(contract)
	if err != nil {
		return "", fmt.Errorf("failed to encode live ISO Job fingerprint: %w", err)
	}
	return fmt.Sprintf("v1:sha256:%x", sha256.Sum256(data)), nil
}
