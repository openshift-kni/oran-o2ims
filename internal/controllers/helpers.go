/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// collectNodeDetails collects BMC and node interfaces details
func collectNodeDetails(ctx context.Context, c client.Client, nodes *[]hwmgrpluginapi.AllocatedNode) (map[string][]utils.NodeInfo, error) {
	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]utils.NodeInfo)
	for _, node := range *nodes {
		if node.Bmc.CredentialsName == "" {
			return nil, fmt.Errorf("the AllocatedNode does not have BMC details")
		}

		interfaces := []*hwv1alpha1.Interface{}
		for _, ifc := range node.Interfaces {
			interfaces = append(interfaces, &hwv1alpha1.Interface{
				Name:       ifc.Name,
				MACAddress: ifc.MacAddress,
				Label:      ifc.Label,
			})
		}

		tmpNode := utils.NodeInfo{
			BmcAddress:     node.Bmc.Address,
			BmcCredentials: node.Bmc.CredentialsName,
			NodeID:         node.Id,
			Interfaces:     interfaces,
		}

		if bmh := utils.GetBareMetalHostForAllocatedNode(ctx, c, node.Id); bmh != nil {
			tmpNode.HwMgrNodeId = bmh.Name
			tmpNode.HwMgrNodeNs = bmh.Namespace
		}

		// Store the nodeInfo per group
		hwNodes[node.GroupName] = append(hwNodes[node.GroupName], tmpNode)
	}

	return hwNodes, nil
}

// compareHardwareTemplateWithNodeAllocationRequest checks if there are any changes in the hardware template resource
func compareHardwareTemplateWithNodeAllocationRequest(hardwareTemplate *hwv1alpha1.HardwareTemplate, nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) (bool, error) {

	changesDetected := false

	// Check each group, allowing only hwProfile to be changed
	for _, specNodeGroup := range nodeAllocationRequest.NodeGroup {
		var found bool
		for _, ng := range hardwareTemplate.Spec.NodeGroupData {

			if specNodeGroup.NodeGroupData.Name == ng.Name {
				found = true

				// Check for changes in HwProfile
				if specNodeGroup.NodeGroupData.HwProfile != ng.HwProfile {
					changesDetected = true
				}
				break
			}
		}

		// If no match was found for the current specNodeGroup, return an error
		if !found {
			return true, fmt.Errorf("node group %s found in NodeAllocationRequest but not in Hardware Template", specNodeGroup.NodeGroupData.Name)
		}
	}

	return changesDetected, nil
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
