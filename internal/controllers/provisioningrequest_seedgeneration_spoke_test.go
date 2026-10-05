/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

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
