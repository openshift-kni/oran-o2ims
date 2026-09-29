/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSeedGenerationStatusDeepCopy(t *testing.T) {
	started := metav1.Now()
	original := &ProvisioningRequest{
		Status: ProvisioningRequestStatus{
			Extensions: Extensions{
				ClusterDetails: &ClusterDetails{
					SeedGenerationStatus: &SeedGenerationStatus{
						StartedAt:                 &started,
						DetachmentStarted:         true,
						SeedImage:                 "quay.io/example/seed@sha256:original",
						ISOServerCACertRef:        &ConfigMapKeyRef{Name: "iso-ca", Namespace: "operator", Key: "ca-bundle.crt"},
						InputSnapshotResourceUIDs: map[string]string{"pull-secret": "original-uid"},
					},
				},
			},
		},
	}

	copied := original.DeepCopy()
	status := copied.Status.Extensions.ClusterDetails.SeedGenerationStatus
	status.StartedAt.Time = started.Time.Add(time.Hour)
	status.ISOServerCACertRef.Name = "changed-ca"
	status.InputSnapshotResourceUIDs["pull-secret"] = "changed-uid"
	status.SeedImage = "quay.io/example/seed@sha256:changed"

	untouched := original.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if !untouched.StartedAt.Equal(&started) || untouched.ISOServerCACertRef.Name != "iso-ca" ||
		untouched.InputSnapshotResourceUIDs["pull-secret"] != "original-uid" ||
		untouched.SeedImage != "quay.io/example/seed@sha256:original" {
		t.Fatal("deep copy changed the original seed generation status")
	}

	if standalone := untouched.DeepCopy(); standalone == untouched || standalone.InputSnapshotResourceUIDs["pull-secret"] != "original-uid" {
		t.Fatal("standalone seed status copy must preserve values without sharing the object")
	}
	if copiedRef := untouched.ISOServerCACertRef.DeepCopy(); copiedRef == untouched.ISOServerCACertRef || copiedRef.Name != "iso-ca" {
		t.Fatal("ConfigMap reference copy must preserve values without sharing the object")
	}
}
