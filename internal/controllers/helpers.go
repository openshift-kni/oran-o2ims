/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// collectNodeDetails collects BMC and node interfaces details
func collectNodeDetails(ctx context.Context, c client.Client, nodes *[]hwmgrpluginapi.AllocatedNode) (map[string][]ctlrutils.NodeInfo, error) {
	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]ctlrutils.NodeInfo)
	for _, node := range *nodes {
		if node.Bmc.CredentialsName == "" {
			return nil, fmt.Errorf("the AllocatedNode does not have BMC details")
		}

		interfaces := []*pluginsv1alpha1.Interface{}
		for _, ifc := range node.Interfaces {
			interfaces = append(interfaces, &pluginsv1alpha1.Interface{
				Name:       ifc.Name,
				MACAddress: ifc.MacAddress,
				Label:      ifc.Label,
			})
		}

		tmpNode := ctlrutils.NodeInfo{
			BmcAddress:     node.Bmc.Address,
			BmcCredentials: node.Bmc.CredentialsName,
			NodeID:         node.Id,
			Interfaces:     interfaces,
		}

		bmh, err := ctlrutils.GetBareMetalHostForAllocatedNode(ctx, c, node.Id)
		if err != nil {
			return nil, fmt.Errorf("failed to get BareMetalHost: %w", err)
		}
		if bmh != nil {
			tmpNode.HwMgrNodeId = bmh.Name
			tmpNode.HwMgrNodeNs = bmh.Namespace
		}

		// Store the nodeInfo per group
		hwNodes[node.GroupName] = append(hwNodes[node.GroupName], tmpNode)
	}

	return hwNodes, nil
}

// validateNodeGroupsMatchNAR verifies that every node group in the existing
// NodeAllocationRequest has a corresponding entry in the merged hwMgmt data.
func validateNodeGroupsMatchNAR(hwMgmtData map[string]any, nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) error {
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
	for _, specNodeGroup := range nodeAllocationRequest.NodeGroup {
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
func newNodeGroup(group hwmgrpluginapi.NodeGroupData, roleCounts map[string]int) hwmgrpluginapi.NodeGroup {
	var nodeGroup hwmgrpluginapi.NodeGroup

	// Populate embedded NodeAllocationRequestData fields
	nodeGroup.NodeGroupData = group

	// Assign size if available in roleCounts
	if count, ok := roleCounts[group.Role]; ok {
		nodeGroup.NodeGroupData.Size = count
	}

	return nodeGroup
}

// getRoleToGroupNameMap creates a mapping of Role to Group Name from NodeAllocationRequest
func getRoleToGroupNameMap(nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) map[string]string {
	roleToNodeGroupName := make(map[string]string)
	for _, nodeGroup := range nodeAllocationRequest.NodeGroup {

		if _, exists := roleToNodeGroupName[nodeGroup.NodeGroupData.Role]; !exists {
			roleToNodeGroupName[nodeGroup.NodeGroupData.Role] = nodeGroup.NodeGroupData.Name
		}
	}
	return roleToNodeGroupName
}
