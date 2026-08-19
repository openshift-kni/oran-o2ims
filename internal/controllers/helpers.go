/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
)

// collectNodeDetails collects BMC and node interfaces details
func collectNodeDetails(nodeList *hwmgmtv1alpha1.AllocatedNodeList) (map[string][]ctlrutils.NodeInfo, error) {
	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]ctlrutils.NodeInfo)
	sort.Slice(nodeList.Items, func(i, j int) bool {
		return nodeList.Items[i].Name < nodeList.Items[j].Name
	})
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
