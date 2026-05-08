/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
)

// collectNodeDetails collects BMC and node interfaces details
func collectNodeDetails(nodeList *hwmgmtv1alpha1.AllocatedNodeList) (map[string][]ctlrutils.NodeInfo, error) {
	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]ctlrutils.NodeInfo)
	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		if node.Status.BMC == nil || node.Status.BMC.CredentialsName == "" {
			return nil, fmt.Errorf("allocatedNode %s does not have BMC details", node.Name)
		}

		tmpNode := ctlrutils.NodeInfo{
			BmcAddress:     node.Status.BMC.Address,
			BmcCredentials: node.Status.BMC.CredentialsName,
			NodeID:         node.Name,
			Interfaces:     node.Status.Interfaces,
			HwMgrNodeId:    node.Spec.HwMgrNodeId,
			HwMgrNodeNs:    node.Spec.HwMgrNodeNs,
		}

		// Store the nodeInfo per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], tmpNode)
	}

	return hwNodes, nil
}

// validateNodeGroupsMatchNAR verifies that every node group in the existing
// NodeAllocationRequest has a corresponding entry in the merged hwMgmt data.
func validateNodeGroupsMatchNAR(hwMgmtData map[string]any, narSpec *hwmgmtv1alpha1.NodeAllocationRequestSpec) error {
	// Build set of valid group names from merged hwMgmt data
	validNames := make(map[string]bool)
	if ngData, ok := hwMgmtData["nodeGroupData"].([]any); ok {
		for _, ng := range ngData {
			if ngMap, ok := ng.(map[string]any); ok {
				if name, ok := ngMap["name"].(string); ok {
					validNames[name] = true
				}
			}
		}
	}

	// Check NAR groups exist in hwMgmt data
	narNames := make(map[string]bool)
	for _, specNodeGroup := range narSpec.NodeGroup {
		narNames[specNodeGroup.NodeGroupData.Name] = true
		if !validNames[specNodeGroup.NodeGroupData.Name] {
			return fmt.Errorf("node group %s found in NodeAllocationRequest but not in hwMgmt data", specNodeGroup.NodeGroupData.Name)
		}
	}

	// Check hwMgmt data groups exist in NAR
	for name := range validNames {
		if !narNames[name] {
			return fmt.Errorf("node group %s found in hwMgmt data but not in NodeAllocationRequest", name)
		}
	}

	return nil
}

// newNodeGroup populates NodeGroup
func newNodeGroup(group hwmgmtv1alpha1.NodeGroupData, roleCounts map[string]int) hwmgmtv1alpha1.NodeGroup {
	nodeGroup := hwmgmtv1alpha1.NodeGroup{
		NodeGroupData: group,
	}

	// Assign size if available in roleCounts
	if count, ok := roleCounts[group.Role]; ok {
		nodeGroup.Size = count
	}

	return nodeGroup
}

// listAllocatedNodesForNAR lists AllocatedNodes that belong to the given NodeAllocationRequest
// using a field index on spec.nodeAllocationRequest. The client must have the field indexer
// registered (via RegisterAllocatedNodeFieldIndexer or SetupWithManager).
func listAllocatedNodesForNAR(ctx context.Context, c client.Client, narName, narNS string) (*hwmgmtv1alpha1.AllocatedNodeList, error) {
	nodes := &hwmgmtv1alpha1.AllocatedNodeList{}
	if err := c.List(ctx, nodes, client.InNamespace(narNS),
		client.MatchingFields{hwmgrutils.AllocatedNodeSpecNodeAllocationRequestKey: narName}); err != nil {
		return nil, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}
	return nodes, nil
}

// getRoleToGroupNameMap creates a mapping of Role to Group Name from NodeAllocationRequest
func getRoleToGroupNameMap(narSpec *hwmgmtv1alpha1.NodeAllocationRequestSpec) map[string]string {
	roleToNodeGroupName := make(map[string]string)
	for _, nodeGroup := range narSpec.NodeGroup {

		if _, exists := roleToNodeGroupName[nodeGroup.NodeGroupData.Role]; !exists {
			roleToNodeGroupName[nodeGroup.NodeGroupData.Role] = nodeGroup.NodeGroupData.Name
		}
	}
	return roleToNodeGroupName
}
