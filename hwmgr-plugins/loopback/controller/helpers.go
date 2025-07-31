/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// processNewNodeAllocationRequest processes a new NodeAllocationRequest CR, verifying that there are enough free resources
// to satisfy the request
func processNewNodeAllocationRequest(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	clusterID := nodeAllocationRequest.Spec.ClusterId
	logger.InfoContext(ctx, "Processing New NodeAllocationRequest:", slog.String("clusterID", clusterID))

	_, resources, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodeGroupData.ResourcePoolId)
		if nodegroup.Size > len(freenodes) {
			return fmt.Errorf("not enough free resources in resource pool %s: freenodes=%d",
				nodegroup.NodeGroupData.ResourcePoolId, len(freenodes))
		}
	}

	return nil
}

// checkNodeAllocationRequestProgress checks to see if a NodeAllocationRequest is fully allocated, allocating additional resources as needed
func checkNodeAllocationRequestProgress(
	ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (full bool, err error) {

	clusterID := nodeAllocationRequest.Spec.ClusterId

	if full, err = isNodeAllocationRequestFullyAllocated(ctx, c, logger, nodeAllocationRequest); err != nil {
		err = fmt.Errorf("failed to check NodeAllocationRequest allocation: %w", err)
		return
	} else if full {
		// Node is fully allocated
		return
	}

	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		logger.InfoContext(ctx, "Allocating node for checkNodeAllocationRequestProgress request:",
			slog.String("clusterID", clusterID),
			slog.String("nodegroup name", nodegroup.NodeGroupData.Name),
		)

		if err = allocateNode(ctx, c, logger, nodeAllocationRequest); err != nil {
			err = fmt.Errorf("failed to allocate node: %w", err)
			return
		}
	}

	return
}

// isNodeAllocationRequestFullyAllocated checks to see if a NodeAllocationRequest CR has been fully allocated
func isNodeAllocationRequestFullyAllocated(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (bool, error) {

	clusterID := nodeAllocationRequest.Spec.ClusterId

	_, resources, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return false, fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == clusterID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// Cloud has not been allocated yet
		return false, nil
	}

	// Check allocated resources
	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		used := cloud.Nodegroups[nodegroup.NodeGroupData.Name]
		remaining := nodegroup.Size - len(used)
		if remaining <= 0 {
			// This group is allocated
			logger.InfoContext(ctx, "nodegroup is fully allocated", slog.String("nodegroup", nodegroup.NodeGroupData.Name))
			continue
		}

		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodeGroupData.ResourcePoolId)
		if remaining > len(freenodes) {
			return false, fmt.Errorf("not enough free resources remaining in resource pool %s", nodegroup.NodeGroupData.ResourcePoolId)
		}

		// Cloud is not fully allocated, and there are resources available
		return false, nil
	}

	return true, nil
}

func handleNodeAllocationRequestConfiguring(
	ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	var nodesToCheck []*pluginsv1alpha1.AllocatedNode // To track nodes that we actually attempted to upgrade
	var result ctrl.Result

	logger.InfoContext(ctx, "Handling NodeAllocationRequest Configuring")

	allocatedNodes, err := getAllocatedNodes(ctx, c, logger, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for %s: %w", nodeAllocationRequest.Name, err)
	}

	// Stage 1: Initiate upgrades by updating node.Spec.HwProfile as necessary
	for _, name := range allocatedNodes {
		node, err := hwmgrutils.GetNode(ctx, logger, c, nodeAllocationRequest.Namespace, name)
		if err != nil {
			return hwmgrutils.RequeueWithShortInterval(), err
		}
		// Check each node against each nodegroup in the NodeAllocationRequest spec
		for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
			if node.Spec.GroupName != nodegroup.NodeGroupData.Name || node.Spec.HwProfile == nodegroup.NodeGroupData.HwProfile {
				continue
			}
			// Node needs an upgrade, so update Spec.HwProfile
			patch := rtclient.MergeFrom(node.DeepCopy())
			node.Spec.HwProfile = nodegroup.NodeGroupData.HwProfile
			if err = c.Patch(ctx, node, patch); err != nil {
				return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to patch Node %s in namespace %s: %w", node.Name, node.Namespace, err)
			}
			nodesToCheck = append(nodesToCheck, node) // Track nodes we attempted to upgrade
			break
		}
	}

	// Requeue if there are nodes to check
	if len(nodesToCheck) > 0 {
		return hwmgrutils.RequeueWithCustomInterval(30 * time.Second), nil
	}

	// Stage 2: Verify and track completion of upgrades
	_, nodesStillUpgrading, err := checkAllocatedNodeUpgradeProcess(ctx, c, logger, nodeAllocationRequest.Namespace, allocatedNodes)
	if err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to check upgrade status for nodes: %w", err)
	}

	// Update NodeAllocationRequest status if all nodes are upgraded
	if len(nodesStillUpgrading) == 0 {
		if err := hwmgrutils.UpdateNodeAllocationRequestStatusCondition(ctx, c, nodeAllocationRequest,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
			return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
		}
		// Update the NodeAllocationRequest hwMgrPlugin status
		if err = hwmgrutils.UpdateNodeAllocationRequestPluginStatus(ctx, c, nodeAllocationRequest); err != nil {
			return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
		}
	} else {
		// Requeue if there are still nodes upgrading
		return hwmgrutils.RequeueWithMediumInterval(), nil
	}

	return result, nil
}

func checkAllocatedNodeUpgradeProcess(
	ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeNamespace string,
	allocatedNodes []string) ([]*pluginsv1alpha1.AllocatedNode, []*pluginsv1alpha1.AllocatedNode, error) {

	var upgradedNodes []*pluginsv1alpha1.AllocatedNode
	var nodesStillUpgrading []*pluginsv1alpha1.AllocatedNode

	for _, name := range allocatedNodes {
		// Fetch the latest version of each node to ensure up-to-date status
		updatedNode, err := hwmgrutils.GetNode(ctx, logger, c, nodeNamespace, name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get node %s: %w", name, err)
		}

		if updatedNode.Status.HwProfile == updatedNode.Spec.HwProfile {
			// AllocatedNode has completed the upgrade
			upgradedNodes = append(upgradedNodes, updatedNode)
		} else {
			updatedNode.Status.HwProfile = updatedNode.Spec.HwProfile
			if err := hwmgrutils.UpdateK8sCRStatus(ctx, c, updatedNode); err != nil {
				return nil, nil, fmt.Errorf("failed to update status for AllocatedNode %s: %w", updatedNode.Name, err)
			}
			nodesStillUpgrading = append(nodesStillUpgrading, updatedNode)
		}
	}

	return upgradedNodes, nodesStillUpgrading, nil
}

// releaseNodeAllocationRequest frees resources allocated to a NodeAllocationRequest
func releaseNodeAllocationRequest(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	clusterID := nodeAllocationRequest.Spec.ClusterId

	logger.InfoContext(ctx, "Processing releaseNodeAllocationRequest request:",
		slog.String("clusterID", clusterID),
	)

	cm, _, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	index := -1
	for i, cloud := range allocations.Clouds {
		if cloud.CloudID == clusterID {
			index = i
			break
		}
	}

	if index == -1 {
		logger.InfoContext(ctx, "no allocated nodes found", slog.String("clusterID", clusterID))
		return nil
	}

	allocations.Clouds = slices.Delete(allocations.Clouds, index, index+1)

	// Update the configmap
	yamlString, err := yaml.Marshal(&allocations)
	if err != nil {
		return fmt.Errorf("unable to marshal allocated data: %w", err)
	}
	cm.Data[allocationsKey] = string(yamlString)
	if err := c.Update(ctx, cm); err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}

// AllocateNode processes a NodeAllocationRequest CR, allocating a free node for each specified nodegroup as needed
func allocateNode(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	clusterID := nodeAllocationRequest.Spec.ClusterId

	// Inject a delay before allocating node
	time.Sleep(10 * time.Second)

	cm, resources, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == clusterID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// The cloud wasn't found in the list, so create a new entry
		allocations.Clouds = append(allocations.Clouds, cmAllocatedCloud{CloudID: clusterID, Nodegroups: make(map[string][]cmAllocatedNode)})
		cloud = &allocations.Clouds[len(allocations.Clouds)-1]
	}

	// Check available resources
	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		used := cloud.Nodegroups[nodegroup.NodeGroupData.Name]
		remaining := nodegroup.Size - len(used)
		if remaining <= 0 {
			// This group is allocated
			logger.InfoContext(ctx, "nodegroup is fully allocated", slog.String("nodegroup", nodegroup.NodeGroupData.Name))
			continue
		}

		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodeGroupData.ResourcePoolId)
		if remaining > len(freenodes) {
			return fmt.Errorf("not enough free resources remaining in resource pool %s", nodegroup.NodeGroupData.ResourcePoolId)
		}

		nodename := hwmgrutils.GenerateNodeName()

		// Grab the first node
		nodeId := freenodes[0]

		nodeinfo, exists := resources.Nodes[nodeId]
		if !exists {
			return fmt.Errorf("unable to find nodeinfo for %s", nodeId)
		}

		if err := createBMCSecret(ctx, c, logger, nodeAllocationRequest, nodename,
			nodeinfo.BMC.UsernameBase64, nodeinfo.BMC.PasswordBase64); err != nil {
			return fmt.Errorf("failed to create bmc-secret when allocating node %s, nodeId %s: %w", nodename, nodeId, err)
		}

		cloud.Nodegroups[nodegroup.NodeGroupData.Name] = append(cloud.Nodegroups[nodegroup.NodeGroupData.Name], cmAllocatedNode{NodeName: nodename, NodeId: nodeId})

		// Update the configmap
		yamlString, err := yaml.Marshal(&allocations)
		if err != nil {
			return fmt.Errorf("unable to marshal allocated data: %w", err)
		}
		cm.Data[allocationsKey] = string(yamlString)
		if err := c.Update(ctx, cm); err != nil {
			return fmt.Errorf("failed to update configmap: %w", err)
		}

		if err := createNode(ctx, c, logger, nodeAllocationRequest, clusterID, nodename, nodeId, nodegroup.NodeGroupData.Name, nodegroup.NodeGroupData.HwProfile); err != nil {
			return fmt.Errorf("failed to create allocated node (%s): %w", nodename, err)
		}

		if err := updateNodeStatus(ctx, c, logger, nodename, nodeAllocationRequest.Namespace, nodeinfo, nodegroup.NodeGroupData.HwProfile); err != nil {
			return fmt.Errorf("failed to update node status (%s): %w", nodename, err)
		}
	}

	return nil
}

func bmcSecretName(nodename string) string {
	return fmt.Sprintf("%s-bmc-secret", nodename)
}

// createBMCSecret creates the bmc-secret for a node
func createBMCSecret(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	nodename, usernameBase64, passwordBase64 string,
) error {

	logger.InfoContext(ctx, "Creating bmc-secret:", slog.String("nodename", nodename))

	secretName := bmcSecretName(nodename)

	username, err := base64.StdEncoding.DecodeString(usernameBase64)
	if err != nil {
		return fmt.Errorf("failed to decode usernameBase64 string (%s) for node %s: %w", usernameBase64, nodename, err)
	}

	password, err := base64.StdEncoding.DecodeString(passwordBase64)
	if err != nil {
		return fmt.Errorf("failed to decode usernameBase64 string (%s) for node %s: %w", passwordBase64, nodename, err)
	}

	blockDeletion := true
	bmcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: nodeAllocationRequest.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodeAllocationRequest.APIVersion,
				Kind:               nodeAllocationRequest.Kind,
				Name:               nodeAllocationRequest.Name,
				UID:                nodeAllocationRequest.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Data: map[string][]byte{
			"username": username,
			"password": password,
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, c, bmcSecret, nil, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create bmc-secret for node %s: %w", nodename, err)
	}

	return nil
}

// createNode creates an AllocatedNode CR with specified attributes
func createNode(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest, cloudID, nodename, nodeId, groupname, hwprofile string,
) error {

	logger.InfoContext(ctx, "Creating node",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

	blockDeletion := true
	node := &pluginsv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodename,
			Namespace: nodeAllocationRequest.Namespace,
			Labels: map[string]string{
				hwmgrutils.HardwarePluginLabel: hwmgrutils.LoopbackHardwarePluginID,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodeAllocationRequest.APIVersion,
				Kind:               nodeAllocationRequest.Kind,
				Name:               nodeAllocationRequest.Name,
				UID:                nodeAllocationRequest.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Spec: pluginsv1alpha1.AllocatedNodeSpec{
			NodeAllocationRequest: cloudID,
			GroupName:             groupname,
			HwProfile:             hwprofile,
			HardwarePluginRef:     nodeAllocationRequest.Spec.HardwarePluginRef,
			HwMgrNodeId:           nodeId,
		},
	}

	if err := c.Create(ctx, node); err != nil {
		return fmt.Errorf("failed to create Node: %w", err)
	}

	return nil
}

// updateNodeStatus updates a Node CR status field with additional node information from the nodelist configmap
func updateNodeStatus(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodename, nodeNamespace string,
	info cmNodeInfo, hwprofile string) error {

	logger.InfoContext(ctx, "Updating AllocatedNode", slog.String("nodename", nodename))

	node := &pluginsv1alpha1.AllocatedNode{}

	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: nodeNamespace}, node)
	}); err != nil {
		return fmt.Errorf("failed to get AllocatedNode for update: %w", err)
	}

	logger.InfoContext(ctx, "Adding info to AllocatedNode",
		slog.String("nodename", nodename),
		slog.Any("info", info))
	node.Status.BMC = &pluginsv1alpha1.BMC{
		Address:         info.BMC.Address,
		CredentialsName: bmcSecretName(nodename),
	}
	node.Status.Interfaces = info.Interfaces

	hwmgrutils.SetStatusCondition(&node.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned),
		string(hwmgmtv1alpha1.Completed),
		metav1.ConditionTrue,
		"Provisioned")
	node.Status.HwProfile = hwprofile
	if err := hwmgrutils.UpdateK8sCRStatus(ctx, c, node); err != nil {
		return fmt.Errorf("failed to update status for AllocatedNode %s: %w", nodename, err)
	}

	return nil
}
