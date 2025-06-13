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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// Loopback HardwarePlugin FSM
type fsmAction int

const (
	NodeAllocationRequestFSMCreate = iota
	NodeAllocationRequestFSMProcessing
	NodeAllocationRequestFSMSpecChanged
	NodeAllocationRequestFSMNoop
)

func determineAction(ctx context.Context, logger *slog.Logger, nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) fsmAction {
	if len(nodeAllocationRequest.Status.Conditions) == 0 {
		logger.InfoContext(ctx, "Handling Create NodeAllocationRequest request")
		return NodeAllocationRequestFSMCreate
	}

	provisionedCondition := meta.FindStatusCondition(
		nodeAllocationRequest.Status.Conditions,
		string(pluginv1alpha1.Provisioned))
	if provisionedCondition != nil {
		if provisionedCondition.Status == metav1.ConditionTrue {
			// Check if the generation has changed
			if nodeAllocationRequest.ObjectMeta.Generation != nodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration {
				logger.InfoContext(ctx, "Handling NodeAllocationRequest Spec change")
				return NodeAllocationRequestFSMSpecChanged
			}
			logger.InfoContext(ctx, "NodeAllocationRequest request in Provisioned state")
			return NodeAllocationRequestFSMNoop
		}

		return NodeAllocationRequestFSMProcessing
	}

	return NodeAllocationRequestFSMNoop
}

// processNewNodeAllocationRequest processes a new NodeAllocationRequest CR, verifying that there are enough free resources
// to satisfy the request
func processNewNodeAllocationRequest(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	cloudID := nodeAllocationRequest.Spec.CloudID
	logger.InfoContext(ctx, "Processing New NodeAllocationRequest:", slog.String("cloudID", cloudID))

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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) (full bool, err error) {

	cloudID := nodeAllocationRequest.Spec.CloudID

	if full, err = isNodeAllocationRequestFullyAllocated(ctx, c, logger, nodeAllocationRequest); err != nil {
		err = fmt.Errorf("failed to check NodeAllocationRequest allocation: %w", err)
		return
	} else if full {
		// Node is fully allocated
		return
	}

	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		logger.InfoContext(ctx, "Allocating node for checkNodeAllocationRequestProgress request:",
			slog.String("cloudID", cloudID),
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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest,
) (bool, error) {

	cloudID := nodeAllocationRequest.Spec.CloudID

	_, resources, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return false, fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == cloudID {
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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) (ctrl.Result, error) {

	var nodesToCheck []*pluginv1alpha1.AllocatedNode // To track nodes that we actually attempted to upgrade
	var result ctrl.Result

	logger.InfoContext(ctx, "Handling NodeAllocationRequest Configuring")

	allocatedNodes, err := getAllocatedNodes(ctx, c, logger, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for %s: %w", nodeAllocationRequest.Name, err)
	}

	// Stage 1: Initiate upgrades by updating node.Spec.HwProfile as necessary
	for _, name := range allocatedNodes {
		node, err := hwpluginutils.GetNode(ctx, logger, c, nodeAllocationRequest.Namespace, name)
		if err != nil {
			return hwpluginutils.RequeueWithShortInterval(), err
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
				return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to patch Node %s in namespace %s: %w", node.Name, node.Namespace, err)
			}
			nodesToCheck = append(nodesToCheck, node) // Track nodes we attempted to upgrade
			break
		}
	}

	// Requeue if there are nodes to check
	if len(nodesToCheck) > 0 {
		return hwpluginutils.RequeueWithCustomInterval(30 * time.Second), nil
	}

	// Stage 2: Verify and track completion of upgrades
	_, nodesStillUpgrading, err := checkAllocatedNodeUpgradeProcess(ctx, c, logger, nodeAllocationRequest.Namespace, allocatedNodes)
	if err != nil {
		return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to check upgrade status for nodes: %w", err)
	}

	// Update NodeAllocationRequest status if all nodes are upgraded
	if len(nodesStillUpgrading) == 0 {
		if err := hwpluginutils.UpdateNodeAllocationRequestStatusCondition(ctx, c, nodeAllocationRequest,
			pluginv1alpha1.Configured, pluginv1alpha1.ConfigApplied, metav1.ConditionTrue, string(pluginv1alpha1.ConfigSuccess)); err != nil {
			return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
		}
		// Update the NodeAllocationRequest hwMgrPlugin status
		if err = hwpluginutils.UpdateNodeAllocationRequestPluginStatus(ctx, c, nodeAllocationRequest); err != nil {
			return hwpluginutils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
		}
	} else {
		// Requeue if there are still nodes upgrading
		return hwpluginutils.RequeueWithMediumInterval(), nil
	}

	return result, nil
}

func checkAllocatedNodeUpgradeProcess(
	ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeNamespace string,
	allocatedNodes []string) ([]*pluginv1alpha1.AllocatedNode, []*pluginv1alpha1.AllocatedNode, error) {

	var upgradedNodes []*pluginv1alpha1.AllocatedNode
	var nodesStillUpgrading []*pluginv1alpha1.AllocatedNode

	for _, name := range allocatedNodes {
		// Fetch the latest version of each node to ensure up-to-date status
		updatedNode, err := hwpluginutils.GetNode(ctx, logger, c, nodeNamespace, name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get node %s: %w", name, err)
		}

		if updatedNode.Status.HwProfile == updatedNode.Spec.HwProfile {
			// AllocatedNode has completed the upgrade
			upgradedNodes = append(upgradedNodes, updatedNode)
		} else {
			updatedNode.Status.HwProfile = updatedNode.Spec.HwProfile
			if err := hwpluginutils.UpdateK8sCRStatus(ctx, c, updatedNode); err != nil {
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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	cloudID := nodeAllocationRequest.Spec.CloudID

	logger.InfoContext(ctx, "Processing releaseNodeAllocationRequest request:",
		slog.String("cloudID", cloudID),
	)

	cm, _, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	index := -1
	for i, cloud := range allocations.Clouds {
		if cloud.CloudID == cloudID {
			index = i
			break
		}
	}

	if index == -1 {
		logger.InfoContext(ctx, "no allocated nodes found", slog.String("cloudID", cloudID))
		return nil
	}

	allocations.Clouds = slices.Delete[[]cmAllocatedCloud](allocations.Clouds, index, index+1)

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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	cloudID := nodeAllocationRequest.Spec.CloudID

	// Inject a delay before allocating node
	time.Sleep(10 * time.Second)

	cm, resources, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == cloudID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// The cloud wasn't found in the list, so create a new entry
		allocations.Clouds = append(allocations.Clouds, cmAllocatedCloud{CloudID: cloudID, Nodegroups: make(map[string][]cmAllocatedNode)})
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

		nodename := hwpluginutils.GenerateNodeName()

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

		if err := createNode(ctx, c, logger, nodeAllocationRequest, cloudID, nodename, nodeId, nodegroup.NodeGroupData.Name, nodegroup.NodeGroupData.HwProfile); err != nil {
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
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest,
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

	if err := sharedutils.CreateK8sCR(ctx, c, bmcSecret, nil, sharedutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create bmc-secret for node %s: %w", nodename, err)
	}

	return nil
}

// createNode creates an AllocatedNode CR with specified attributes
func createNode(ctx context.Context,
	c rtclient.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest, cloudID, nodename, nodeId, groupname, hwprofile string,
) error {

	logger.InfoContext(ctx, "Creating node",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

	blockDeletion := true
	node := &pluginv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodename,
			Namespace: nodeAllocationRequest.Namespace,
			Labels: map[string]string{
				hwpluginutils.HardwarePluginLabel: hwpluginutils.LoopbackHardwarePluginID,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodeAllocationRequest.APIVersion,
				Kind:               nodeAllocationRequest.Kind,
				Name:               nodeAllocationRequest.Name,
				UID:                nodeAllocationRequest.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Spec: pluginv1alpha1.AllocatedNodeSpec{
			NodeAllocationRequest: cloudID,
			GroupName:             groupname,
			HwProfile:             hwprofile,
			HwMgrId:               nodeAllocationRequest.Spec.HwMgrId,
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

	node := &pluginv1alpha1.AllocatedNode{}

	if err := sharedutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: nodeNamespace}, node)
	}); err != nil {
		return fmt.Errorf("failed to get AllocatedNode for update: %w", err)
	}

	logger.InfoContext(ctx, "Adding info to AllocatedNode",
		slog.String("nodename", nodename),
		slog.Any("info", info))
	node.Status.BMC = &pluginv1alpha1.BMC{
		Address:         info.BMC.Address,
		CredentialsName: bmcSecretName(nodename),
	}
	node.Status.Interfaces = info.Interfaces

	hwpluginutils.SetStatusCondition(&node.Status.Conditions,
		string(pluginv1alpha1.Provisioned),
		string(pluginv1alpha1.Completed),
		metav1.ConditionTrue,
		"Provisioned")
	node.Status.HwProfile = hwprofile
	if err := hwpluginutils.UpdateK8sCRStatus(ctx, c, node); err != nil {
		return fmt.Errorf("failed to update status for AllocatedNode %s: %w", nodename, err)
	}

	return nil
}
