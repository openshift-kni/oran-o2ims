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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestPinSeedGenerationIdentity(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong release", "multiple nodes", "multiple agents", "hosted agent", "changed identity", "changed hub claim"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pr := seedDetachmentTestRequest()
			pr.Status.Extensions.ClusterDetails.SeedGenerationStatus = &provisioningv1alpha1.SeedGenerationStatus{}
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", UID: "managed-cluster-uid"},
				Status:     clusterv1.ManagedClusterStatus{ClusterClaims: []clusterv1.ManagedClusterClaim{{Name: "id.k8s.io", Value: "cluster-id-a"}}},
			}
			klusterlet := newSeedGenerationKlusterlet()
			klusterlet.SetUID("klusterlet-uid")
			objects := []client.Object{klusterlet, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "sno"}}}
			snapshot := seedGenerationLifecycleTestSnapshot()
			snapshot.Document.Release = "4.22.0"
			switch scenario {
			case "wrong release":
				snapshot.Document.Release = "4.23.0"
			case "multiple nodes":
				objects = append(objects, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "other"}})
			case "multiple agents":
				other := newSeedGenerationKlusterlet()
				other.SetName("other")
				objects = append(objects, other)
			case "hosted agent":
				klusterlet.Object["spec"] = map[string]any{"deployOption": map[string]any{"mode": "Hosted"}}
			case "changed identity":
				pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.ClusterID = "old-id"
			case "changed hub claim":
				cluster.Status.ClusterClaims[0].Value = "other-id"
			}
			hub := newSeedTestClient(t, pr, cluster)
			task := seedTestTask(pr, hub)
			err := task.pinSeedGenerationIdentity(ctx, newSeedLifecycleSpokeClient(t, objects...), snapshot)
			if (err != nil) != (scenario != "valid") {
				t.Fatalf("pin identity error=%v", err)
			}
			stored := &provisioningv1alpha1.ProvisioningRequest{}
			if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), stored); err != nil {
				t.Fatal(err)
			}
			status := stored.Status.Extensions.ClusterDetails.SeedGenerationStatus
			if scenario == "valid" {
				if status.ClusterID != "cluster-id-a" || status.ManagedClusterUID != "managed-cluster-uid" || status.KlusterletUID != "klusterlet-uid" {
					t.Fatalf("identifiers not durably recorded: %+v", status)
				}
			} else if status.ManagedClusterUID != "" || status.KlusterletUID != "" {
				t.Fatal("failed validation persisted a new identity")
			}
		})
	}
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
		if err := task.validateSeedGenerationSpokePrerequisites(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "ZTP Done") {
			t.Fatalf("prerequisite error=%v", err)
		}
	})
	t.Run("requires a spoke cluster name", func(t *testing.T) {
		pr := &provisioningv1alpha1.ProvisioningRequest{}
		pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{ZtpStatus: ctlrutils.ClusterZtpDone}
		task := seedTestTask(pr, nil)
		if err := task.validateSeedGenerationSpokePrerequisites(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "cluster name") {
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

func TestValidateSeedGenerationSpokeIdentity(t *testing.T) {
	const (
		clusterName = "spoke-a"
		clusterID   = "cluster-id-a"
	)

	for _, tc := range []struct {
		name              string
		expectedClusterID string
		hubClusterID      string
		actualClusterID   string
		wantError         bool
	}{
		{name: "matching cluster IDs", expectedClusterID: clusterID, hubClusterID: clusterID, actualClusterID: clusterID},
		{name: "different spoke cluster ID", expectedClusterID: clusterID, hubClusterID: clusterID, actualClusterID: "cluster-id-b", wantError: true},
		{name: "different ClusterDeployment cluster ID", expectedClusterID: "cluster-id-b", hubClusterID: clusterID, actualClusterID: clusterID, wantError: true},
		{name: "missing ClusterDeployment cluster ID", hubClusterID: clusterID, actualClusterID: clusterID, wantError: true},
		{name: "missing hub cluster ID claim", expectedClusterID: clusterID, actualClusterID: clusterID, wantError: true},
		{name: "missing spoke cluster ID", expectedClusterID: clusterID, hubClusterID: clusterID, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := &provisioningv1alpha1.ProvisioningRequest{}
			pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: clusterName}
			clusterDeployment := &hivev1.ClusterDeployment{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: clusterName},
				Spec: hivev1.ClusterDeploymentSpec{ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID: tc.expectedClusterID,
				}},
			}
			managedCluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}
			if tc.hubClusterID != "" {
				managedCluster.Status.ClusterClaims = []clusterv1.ManagedClusterClaim{{
					Name:  "id.k8s.io",
					Value: tc.hubClusterID,
				}}
			}
			hubClient := newSeedTestClient(t, clusterDeployment, managedCluster)

			spokeScheme := runtime.NewScheme()
			clusterVersionGV := schema.GroupVersion{Group: "config.openshift.io", Version: "v1"}
			spokeScheme.AddKnownTypeWithName(clusterVersionGV.WithKind("ClusterVersion"), &unstructured.Unstructured{})
			clusterVersion := &unstructured.Unstructured{}
			clusterVersion.SetGroupVersionKind(clusterVersionGV.WithKind("ClusterVersion"))
			clusterVersion.SetName("version")
			if tc.actualClusterID != "" {
				clusterVersion.Object["spec"] = map[string]any{"clusterID": tc.actualClusterID}
			}
			spokeClient := fake.NewClientBuilder().WithScheme(spokeScheme).WithObjects(clusterVersion).Build()

			task := seedTestTask(pr, hubClient)
			if err := task.validateSeedGenerationSpokeIdentity(context.Background(), spokeClient); (err != nil) != tc.wantError {
				t.Fatalf("identity validation error=%v, wantError=%t", err, tc.wantError)
			}
		})
	}
}
