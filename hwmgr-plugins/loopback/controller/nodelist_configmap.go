/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// Struct definitions for the nodelist configmap
type cmBmcInfo struct {
	Address        string `json:"address,omitempty"`
	UsernameBase64 string `json:"username-base64,omitempty"`
	PasswordBase64 string `json:"password-base64,omitempty"`
}

type processorInfo struct {
	Architecture string `json:"architecture,omitempty"`
	Cores        int    `json:"cores,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
}

type cmNodeInfo struct {
	ResourcePoolID   string                       `json:"poolID,omitempty"`
	BMC              *cmBmcInfo                   `json:"bmc,omitempty"`
	Interfaces       []*pluginsv1alpha1.Interface `json:"interfaces,omitempty"`
	Description      string                       `json:"description,omitempty"`
	GlobalAssetID    string                       `json:"globalAssetId,omitempty"`
	Vendor           string                       `json:"vendor,omitempty"`
	Model            string                       `json:"model,omitempty"`
	Memory           int                          `json:"memory,omitempty"`
	AdminState       string                       `json:"adminState,omitempty"`
	OperationalState string                       `json:"operationalState,omitempty"`
	UsageState       string                       `json:"usageState,omitempty"`
	PowerState       string                       `json:"powerState,omitempty"`
	SerialNumber     string                       `json:"serialNumber,omitempty"`
	PartNumber       string                       `json:"partNumber,omitempty"`
	Labels           map[string]string            `json:"labels,omitempty"`
	Processors       []processorInfo              `json:"processors,omitempty"`
}

type cmResources struct {
	ResourcePools []string              `json:"resourcepools" yaml:"resourcepools"`
	Nodes         map[string]cmNodeInfo `json:"nodes" yaml:"nodes"`
}

type cmAllocatedNode struct {
	NodeName string `json:"nodeName" yaml:"nodeName"`
	NodeId   string `json:"nodeId" yaml:"nodeId"`
}

type cmAllocatedCloud struct {
	CloudID    string                       `json:"cloudID" yaml:"cloudID"`
	Nodegroups map[string][]cmAllocatedNode `json:"nodegroups" yaml:"nodegroups"`
}

type cmAllocations struct {
	Clouds []cmAllocatedCloud `json:"clouds" yaml:"clouds"`
}

const (
	resourcesKey   = "resources"
	allocationsKey = "allocations"
	cmName         = "loopback-hardwareplugin-nodelist"
)

// getFreeNodesInPool compares the parsed configmap data to get the list of free nodes for a given resource pool
func getFreeNodesInPool(resources cmResources, allocations cmAllocations, poolID string) (freenodes []string) {
	inuse := make(map[string]bool)
	for _, cloud := range allocations.Clouds {
		for groupname := range cloud.Nodegroups {
			for _, node := range cloud.Nodegroups[groupname] {
				inuse[node.NodeId] = true
			}
		}
	}

	for nodeId, node := range resources.Nodes {
		// Check if the node belongs to the specified resource pool
		if node.ResourcePoolID == poolID {
			// Only add to the freenodes if not in use
			if _, used := inuse[nodeId]; !used {
				freenodes = append(freenodes, nodeId)
			}
		}
	}

	return
}

// getCurrentResources parses the nodelist configmap to get the current available and allocated resource lists
func getCurrentResources(ctx context.Context, c client.Client, logger *slog.Logger, namespace string) (
	cm *corev1.ConfigMap, resources cmResources, allocations cmAllocations, err error) {
	cm, err = sharedutils.GetConfigmap(ctx, c, cmName, namespace)
	if err != nil {
		err = fmt.Errorf("unable to get configmap: %w", err)
		return
	}

	resources, err = sharedutils.ExtractDataFromConfigMap[cmResources](cm, resourcesKey)
	if err != nil {
		err = fmt.Errorf("unable to parse resources from configmap: %w", err)
		return
	}

	allocations, err = sharedutils.ExtractDataFromConfigMap[cmAllocations](cm, allocationsKey)
	if err != nil {
		// Allocated node field may not be present
		logger.InfoContext(ctx, "unable to parse allocations from configmap")
		err = nil
	}

	return
}

// getAllocatedNodes gets a list of nodes allocated for the specified NodeAllocationRequest CR
func getAllocatedNodes(ctx context.Context,
	c client.Client, logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (allocatedNodes []string, err error) {
	clusterID := nodeAllocationRequest.Spec.ClusterId

	_, _, allocations, err := getCurrentResources(ctx, c, logger, nodeAllocationRequest.Namespace)
	if err != nil {
		err = fmt.Errorf("unable to get current resources: %w", err)
		return
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
		return
	}

	// Get allocated resources
	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		for _, node := range cloud.Nodegroups[nodegroup.NodeGroupData.Name] {
			allocatedNodes = append(allocatedNodes, node.NodeName)
		}
	}

	slices.Sort(allocatedNodes)
	return
}
