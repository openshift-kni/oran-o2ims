/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	k8sclients "github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationLCAOperatorNamespace = "openshift-lifecycle-agent"
	seedGenerationLCADeploymentName    = "lifecycle-agent-controller-manager"
	seedGenerationContainersPartLabel  = "var-lib-containers"
	seedGenerationContainersMountPath  = "/var/lib/containers"
)

func (t *provisioningRequestReconcilerTask) validateSeedGenerationSpokePrerequisites(ctx context.Context) error {
	if !ctlrutils.IsClusterZtpDone(t.object) {
		return fmt.Errorf("spoke cluster has not reached ZTP Done")
	}
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.Name == "" {
		return fmt.Errorf("provisioning request has no spoke cluster name")
	}
	spokeClient, err := k8sclients.NewClientForCluster(ctx, t.client, details.Name)
	if err != nil {
		return fmt.Errorf("failed to create spoke client for seed generation preflight: %w", err)
	}
	if err := validateSeedGenerationContainersPartition(ctx, spokeClient); err != nil {
		return err
	}
	if err := validateSeedGenerationLCAAvailable(ctx, spokeClient); err != nil {
		return err
	}
	return nil
}

func validateSeedGenerationContainersPartition(ctx context.Context, spokeClient client.Client) error {
	machineConfigs := &machineconfigv1.MachineConfigList{}
	if err := spokeClient.List(ctx, machineConfigs); err != nil {
		return fmt.Errorf("failed to list spoke MachineConfigs: %w", err)
	}
	partitionFound, filesystemFound := false, false
	for i := range machineConfigs.Items {
		machineConfig := &machineConfigs.Items[i]
		if machineConfig.DeletionTimestamp != nil {
			continue
		}
		partition, filesystem := machineConfigSharedContainersPartitionParts(machineConfig.Spec.Config.Raw)
		partitionFound = partitionFound || partition
		filesystemFound = filesystemFound || filesystem
	}
	if !partitionFound || !filesystemFound {
		return fmt.Errorf("spoke is missing MachineConfig configuration for the shared %s partition and filesystem", seedGenerationContainersPartLabel)
	}
	return nil
}

func machineConfigSharedContainersPartitionParts(raw []byte) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var ignition struct {
		Storage struct {
			Disks []struct {
				Partitions []struct {
					Label string `json:"label"`
				} `json:"partitions"`
			} `json:"disks"`
			Filesystems []struct {
				Device string `json:"device"`
				Path   string `json:"path"`
			} `json:"filesystems"`
		} `json:"storage"`
	}
	if err := json.Unmarshal(raw, &ignition); err != nil {
		return false, false
	}
	partitionFound := false
	for _, disk := range ignition.Storage.Disks {
		for _, partition := range disk.Partitions {
			if partition.Label == seedGenerationContainersPartLabel {
				partitionFound = true
			}
		}
	}
	filesystemFound := false
	for _, filesystem := range ignition.Storage.Filesystems {
		if filesystem.Path == seedGenerationContainersMountPath &&
			filesystem.Device == "/dev/disk/by-partlabel/"+seedGenerationContainersPartLabel {
			filesystemFound = true
		}
	}
	return partitionFound, filesystemFound
}

func validateSeedGenerationLCAAvailable(ctx context.Context, spokeClient client.Client) error {
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Name: seedGenerationLCADeploymentName, Namespace: seedGenerationLCAOperatorNamespace}
	if err := spokeClient.Get(ctx, key, deployment); err != nil {
		return fmt.Errorf("failed to get LCA operator Deployment %s/%s: %w", key.Namespace, key.Name, err)
	}
	if deployment.DeletionTimestamp != nil || deployment.Status.AvailableReplicas == 0 {
		return fmt.Errorf("lca operator Deployment %s/%s is not Available", key.Namespace, key.Name)
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return nil
		}
	}
	return fmt.Errorf("lca operator Deployment %s/%s is not Available", key.Namespace, key.Name)
}
