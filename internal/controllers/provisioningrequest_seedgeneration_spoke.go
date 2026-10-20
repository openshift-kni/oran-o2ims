/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	k8sclients "github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationLCAOperatorNamespace = "openshift-lifecycle-agent"
	seedGenerationLCADeploymentName    = "lifecycle-agent-controller-manager"
	seedGenerationContainersPartLabel  = "var-lib-containers"
	seedGenerationContainersMountPath  = "/var/lib/containers"
	seedGenerationSpokeRequestTimeout  = 30 * time.Second
)

func (t *provisioningRequestReconcilerTask) validateSeedGenerationSpokePrerequisites(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) error {
	if !ctlrutils.IsClusterZtpDone(t.object) {
		return fmt.Errorf("spoke cluster has not reached ZTP Done")
	}
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.Name == "" {
		return fmt.Errorf("provisioning request has no spoke cluster name")
	}
	spokeClient, err := t.seedGenerationSpokeClient(snapshot)
	if err != nil {
		return err
	}
	if err := t.validateSeedGenerationSpokeIdentity(ctx, spokeClient); err != nil {
		return err
	}
	if err := validateSeedGenerationContainersPartition(ctx, spokeClient); err != nil {
		return err
	}
	if err := validateSeedGenerationLCAAvailable(ctx, spokeClient); err != nil {
		return err
	}
	for _, object := range []client.Object{newSeedGenerationSeedGenerator(), &corev1.Secret{}} {
		key := client.ObjectKey{Name: seedGenerationLCASeedCRName}
		if _, ok := object.(*corev1.Secret); ok {
			key = client.ObjectKey{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}
		}
		if err := spokeClient.Get(ctx, key, object); err == nil {
			return fmt.Errorf("spoke resource %s already exists; finish the existing seed run before starting another", key)
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to inspect LCA seed generation resources: %w", err)
		}
	}
	return t.pinSeedGenerationIdentity(ctx, spokeClient, snapshot)
}

// validateSeedGenerationSpokeIdentity checks the kubeconfig target against the
// cluster ID recorded by the ClusterDeployment before the workflow removes ACM
// agents from either the hub or spoke.
func (t *provisioningRequestReconcilerTask) validateSeedGenerationSpokeIdentity(
	ctx context.Context,
	spokeClient client.Client,
) error {
	clusterDeployment, err := t.getSeedGenerationClusterDeployment(ctx)
	if err != nil {
		return err
	}
	if clusterDeployment.Spec.ClusterMetadata == nil ||
		strings.TrimSpace(clusterDeployment.Spec.ClusterMetadata.ClusterID) == "" {
		return fmt.Errorf("spoke ClusterDeployment %s/%s has no cluster ID",
			clusterDeployment.Namespace, clusterDeployment.Name)
	}
	expectedClusterID := clusterDeployment.Spec.ClusterMetadata.ClusterID
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterKey := client.ObjectKey{Name: clusterDeployment.Name}
	if err := t.client.Get(ctx, managedClusterKey, managedCluster); err != nil {
		return fmt.Errorf("failed to get hub ManagedCluster %s for identity validation: %w", managedClusterKey.Name, err)
	}
	if managedCluster.DeletionTimestamp != nil {
		return fmt.Errorf("hub ManagedCluster %s is being deleted", managedCluster.Name)
	}
	managedClusterID := ""
	for _, claim := range managedCluster.Status.ClusterClaims {
		if claim.Name == "id.k8s.io" {
			managedClusterID = claim.Value
			break
		}
	}
	if strings.TrimSpace(managedClusterID) == "" {
		return fmt.Errorf("hub ManagedCluster %s has no id.k8s.io cluster claim", managedCluster.Name)
	}
	if expectedClusterID != managedClusterID {
		return fmt.Errorf("cluster identity mismatch: ClusterDeployment %s/%s records %q, hub ManagedCluster %s claims %q",
			clusterDeployment.Namespace, clusterDeployment.Name, expectedClusterID, managedCluster.Name, managedClusterID)
	}

	clusterVersion := &unstructured.Unstructured{}
	clusterVersion.SetAPIVersion("config.openshift.io/v1")
	clusterVersion.SetKind("ClusterVersion")
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion); err != nil {
		return fmt.Errorf("failed to get spoke ClusterVersion for identity validation: %w", err)
	}
	actualClusterID, found, err := unstructured.NestedString(clusterVersion.Object, "spec", "clusterID")
	if err != nil || !found || strings.TrimSpace(actualClusterID) == "" {
		return fmt.Errorf("spoke ClusterVersion is missing spec.clusterID")
	}
	if actualClusterID != managedClusterID {
		return fmt.Errorf("spoke cluster identity mismatch: kubeconfig targets cluster ID %q, hub ManagedCluster %s claims %q",
			actualClusterID, managedCluster.Name, managedClusterID)
	}
	return nil
}

func seedGenerationSpokeClient(snapshot *seedGenerationInputSnapshot) (client.Client, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("seed generation input snapshot is missing")
	}
	secret := snapshot.Secrets["spokeKubeconfig"]
	if secret == nil || len(secret.Data["kubeconfig"]) == 0 {
		return nil, fmt.Errorf("seed generation snapshot is missing the spoke admin kubeconfig")
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(secret.Data["kubeconfig"])
	if err != nil {
		return nil, fmt.Errorf("failed to load spoke admin kubeconfig from snapshot: %w", err)
	}
	config.Timeout = seedGenerationSpokeRequestTimeout
	spokeClient, err := client.New(config, client.Options{Scheme: k8sclients.GetSchemeForCluster()})
	if err != nil {
		return nil, fmt.Errorf("failed to create spoke client for seed generation: %w", err)
	}
	return spokeClient, nil
}

// pinSeedGenerationIdentity records the identifiers that must remain stable
// after ACM stops reporting claims. No destructive step may precede this write.
func (t *provisioningRequestReconcilerTask) pinSeedGenerationIdentity(
	ctx context.Context, spokeClient client.Client, snapshot *seedGenerationInputSnapshot,
) error {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status == nil {
		return fmt.Errorf("seed generation run state is missing")
	}
	clusterName := t.object.Status.Extensions.ClusterDetails.Name
	managedCluster := &clusterv1.ManagedCluster{}
	if err := t.client.Get(ctx, client.ObjectKey{Name: clusterName}, managedCluster); err != nil {
		return fmt.Errorf("failed to pin ManagedCluster identity: %w", err)
	}
	clusterVersion := newSeedGenerationClusterVersion()
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion); err != nil {
		return fmt.Errorf("failed to pin spoke cluster identity: %w", err)
	}
	clusterID, _, _ := unstructured.NestedString(clusterVersion.Object, "spec", "clusterID")
	matchingClaim := false
	for _, claim := range managedCluster.Status.ClusterClaims {
		matchingClaim = matchingClaim || (claim.Name == "id.k8s.io" && claim.Value == clusterID)
	}
	if !matchingClaim || managedCluster.DeletionTimestamp != nil {
		return fmt.Errorf("managedCluster identity changed during seed preflight")
	}
	version, _, _ := unstructured.NestedString(clusterVersion.Object, "status", "desired", "version")
	if snapshot.Document == nil || version != snapshot.Document.Release {
		return fmt.Errorf("spoke release %q does not match the seed template release", version)
	}
	nodes := &corev1.NodeList{}
	if err := spokeClient.List(ctx, nodes); err != nil {
		return fmt.Errorf("failed to validate seed cluster node count: %w", err)
	}
	if len(nodes.Items) != 1 {
		return fmt.Errorf("seed generation requires a single-node cluster, found %d nodes", len(nodes.Items))
	}
	klusterlets := &unstructured.UnstructuredList{}
	klusterlets.SetAPIVersion(seedGenerationKlusterletAPIVersion)
	klusterlets.SetKind("KlusterletList")
	if err := spokeClient.List(ctx, klusterlets); err != nil {
		return fmt.Errorf("failed to inspect spoke Klusterlets: %w", err)
	}
	if len(klusterlets.Items) != 1 || klusterlets.Items[0].GetName() != seedGenerationKlusterletName {
		return fmt.Errorf("seed generation requires exactly one Klusterlet named %s", seedGenerationKlusterletName)
	}
	klusterlet := &klusterlets.Items[0]
	mode, _, _ := unstructured.NestedString(klusterlet.Object, "spec", "deployOption", "mode")
	if mode == "Hosted" {
		return fmt.Errorf("seed generation does not support hosted Klusterlets")
	}
	if clusterID == "" || managedCluster.UID == "" || klusterlet.GetUID() == "" {
		return fmt.Errorf("seed generation cluster identity is incomplete")
	}
	if status.ClusterID != "" && (status.ClusterID != clusterID ||
		status.ManagedClusterUID != string(managedCluster.UID) || status.KlusterletUID != string(klusterlet.GetUID())) {
		return fmt.Errorf("seed generation cluster identity changed after preflight")
	}
	status.ClusterID = clusterID
	status.ManagedClusterUID = string(managedCluster.UID)
	status.KlusterletUID = string(klusterlet.GetUID())
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to persist seed generation cluster identity: %w", err)
	}
	return nil
}

func newSeedGenerationClusterVersion() *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("config.openshift.io/v1")
	object.SetKind("ClusterVersion")
	return object
}

// verifiedSeedGenerationSpokeClient rechecks the frozen target on every
// reconcile, including cleanup, before allowing any spoke mutation.
func (t *provisioningRequestReconcilerTask) verifiedSeedGenerationSpokeClient(
	ctx context.Context, snapshot *seedGenerationInputSnapshot,
) (client.Client, error) {
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.SeedGenerationStatus == nil || details.SeedGenerationStatus.ClusterID == "" {
		return nil, fmt.Errorf("seed generation has no pinned spoke identity")
	}
	spokeClient, err := t.seedGenerationSpokeClient(snapshot)
	if err != nil {
		return nil, err
	}
	version := newSeedGenerationClusterVersion()
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: "version"}, version); err != nil {
		return nil, fmt.Errorf("failed to verify spoke cluster identity: %w", err)
	}
	clusterID, _, _ := unstructured.NestedString(version.Object, "spec", "clusterID")
	if clusterID != details.SeedGenerationStatus.ClusterID {
		return nil, fmt.Errorf("spoke cluster identity mismatch: got %q, expected %q", clusterID, details.SeedGenerationStatus.ClusterID)
	}
	return spokeClient, nil
}

func (t *provisioningRequestReconcilerTask) seedGenerationSpokeClient(
	snapshot *seedGenerationInputSnapshot,
) (client.Client, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("seed generation input snapshot is missing")
	}
	secret := snapshot.Secrets["spokeKubeconfig"]
	if secret == nil || len(secret.Data["kubeconfig"]) == 0 {
		return nil, fmt.Errorf("seed generation snapshot is missing the spoke admin kubeconfig")
	}
	if t.seedGenerationSpokeClientBuilder != nil {
		return t.seedGenerationSpokeClientBuilder(secret.Data["kubeconfig"])
	}
	return seedGenerationSpokeClient(snapshot)
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
