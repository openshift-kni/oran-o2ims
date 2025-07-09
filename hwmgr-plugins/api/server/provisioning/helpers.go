/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func GetLoopbackHWPluginNamespace() string {
	return utils.GetHwMgrPluginNS()
}

func GetMetal3HWPluginNamespace() string {
	return utils.GetHwMgrPluginNS()
}

func GenerateResourceIdentifier(baseName string) (string, error) {
	// Generate a new UUID
	uuid, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate UUID: %w", err)
	}

	// Convert UUID to string and remove hyphens
	uuidStr := strings.ReplaceAll(uuid.String(), "-", "")

	// Kubernetes names must be lowercase, no longer than 253 characters,
	// and contain only lowercase alphanumeric characters, '-', or '.'
	// Truncate to ensure length is reasonable (e.g., 20 chars from UUID)
	resourceID := fmt.Sprintf("%s-%s", strings.ToLower(baseName), uuidStr[:20])

	// Ensure the name is no longer than 253 characters
	if len(resourceID) > 253 {
		resourceID = resourceID[:253]
	}

	return resourceID, nil
}

// NodeAllocationRequestCRToResponseObject Converts a NodeAllocationRequest CR to NodeAllocationRequestResponse object
func NodeAllocationRequestCRToResponseObject(nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) (NodeAllocationRequestResponse, error) {
	// Convert NodeGroup slice
	nodeGroups := []NodeGroup{}
	for _, ng := range nodeAllocationRequest.Spec.NodeGroup {
		nodeGroup := NodeGroup{
			NodeGroupData: NodeGroupData{
				Name:             ng.NodeGroupData.Name,
				Role:             ng.NodeGroupData.Role,
				HwProfile:        ng.NodeGroupData.HwProfile,
				ResourceGroupId:  ng.NodeGroupData.ResourcePoolId,
				ResourceSelector: ng.NodeGroupData.ResourceSelector,
				Size:             ng.Size,
			},
		}
		nodeGroups = append(nodeGroups, nodeGroup)
	}

	// Create generated.NodeAllocationRequest object
	nodeAllocationRequestObject := NodeAllocationRequest{
		NodeGroup:           nodeGroups,
		Site:                nodeAllocationRequest.Spec.Site,
		BootInterfaceLabel:  nodeAllocationRequest.Spec.BootInterfaceLabel,
		ClusterId:           nodeAllocationRequest.Spec.ClusterId,
		ConfigTransactionId: nodeAllocationRequest.Spec.ConfigTransactionId,
	}

	nodeAllocationRequestStatus := NodeAllocationRequestStatus{}
	conditions := []Condition{}
	for _, condition := range nodeAllocationRequest.Status.Conditions {
		conditions = append(conditions, Condition{
			Type:               condition.Type,
			Reason:             condition.Reason,
			Status:             string(condition.Status),
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	nodeAllocationRequestStatus.Conditions = &conditions
	nodeAllocationRequestStatus.SelectedGroups = &nodeAllocationRequest.Status.SelectedGroups
	nodeAllocationRequestStatus.Properties = &Properties{
		NodeNames: &nodeAllocationRequest.Status.Properties.NodeNames,
	}
	nodeAllocationRequestStatus.ObservedConfigTransactionId = &nodeAllocationRequest.Status.ObservedConfigTransactionId

	return NodeAllocationRequestResponse{
		NodeAllocationRequest: &nodeAllocationRequestObject,
		Status:                &nodeAllocationRequestStatus,
	}, nil
}

func GetNodeAllocationRequest(ctx context.Context,
	c client.Client,
	namespace, nodeAllocationRequestId string,
) (*hwv1alpha1.NodeAllocationRequest, error) {

	nodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: nodeAllocationRequestId}, nodeAllocationRequest)
	// nolint: wrapcheck
	return nodeAllocationRequest, err
}

// CreateOrUpdateNodeAllocationRequest creates a new NodeAllocationRequest resource if it doesn't exist or updates it if the spec has changed.
func CreateOrUpdateNodeAllocationRequest(
	ctx context.Context, c client.Client, logger *slog.Logger, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {

	existingNodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}

	exist, err := utils.DoesK8SResourceExist(ctx, c, nodeAllocationRequest.Name, nodeAllocationRequest.Namespace, existingNodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get NodeAllocationRequest %s in namespace %s: %w", nodeAllocationRequest.GetName(), nodeAllocationRequest.GetNamespace(), err)
	}

	if !exist {
		// Create the NodeAllocationRequest resource
		err := utils.CreateK8sCR(ctx, c, nodeAllocationRequest, nil, "")
		if err != nil {
			logger.ErrorContext(
				ctx,
				fmt.Sprintf(
					"Failed to create the NodeAllocationRequest object %s in the namespace %s",
					nodeAllocationRequest.GetName(),
					nodeAllocationRequest.GetNamespace(),
				),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("failed to create NodeAllocationRequest %s: %s", nodeAllocationRequest.Name, err.Error())
		}

		// Create operation succeeded
		return nil
	}

	// Update existing NodeAllocationRequest resource

	// Compare NodeGroup and update it if necessary
	if !equality.Semantic.DeepEqual(existingNodeAllocationRequest.Spec.NodeGroup, nodeAllocationRequest.Spec.NodeGroup) {
		// Only process the configuration changes
		patch := client.MergeFrom(existingNodeAllocationRequest.DeepCopy())
		// Update the spec field with the new data
		existingNodeAllocationRequest.Spec = nodeAllocationRequest.Spec
		// Apply the patch to update the NodeAllocationRequest with the new spec
		if err = c.Patch(ctx, existingNodeAllocationRequest, patch); err != nil {
			return fmt.Errorf("failed to patch NodeAllocationRequest %s in namespace %s: %w", nodeAllocationRequest.GetName(), nodeAllocationRequest.GetNamespace(), err)
		}

		logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodeAllocationRequest (%s) in the namespace %s configuration changes have been detected",
				nodeAllocationRequest.GetName(),
				nodeAllocationRequest.GetNamespace(),
			),
		)
	}
	return nil
}

func AllocatedNodeCRToAllocatedNodeObject(node *hwv1alpha1.AllocatedNode) (AllocatedNode, error) {
	interfaces := []Interface{}
	for _, ifc := range node.Status.Interfaces {
		interfaces = append(interfaces, Interface{
			Label:      ifc.Label,
			MacAddress: ifc.MACAddress,
			Name:       ifc.Name,
		})
	}

	conditions := []Condition{}
	for _, cond := range node.Status.Conditions {
		conditions = append(conditions, Condition{
			Type:               cond.Type,
			Status:             string(cond.Status),
			Reason:             cond.Reason,
			Message:            cond.Message,
			LastTransitionTime: cond.LastTransitionTime.Time,
		})
	}

	nodeObject := AllocatedNode{
		Id: node.Name,
		Bmc: BMC{
			Address:         node.Status.BMC.Address,
			CredentialsName: node.Status.BMC.CredentialsName,
		},
		HwProfile:  node.Status.HwProfile,
		GroupName:  node.Spec.GroupName,
		Interfaces: interfaces,
		Status: AllocatedNodeStatus{
			Conditions: &conditions,
		},
	}
	return nodeObject, nil
}

func GetAllocatedNode(ctx context.Context, c client.Client, namespace, nodeId string) (*hwv1alpha1.AllocatedNode, error) {
	allocatedNode := &hwv1alpha1.AllocatedNode{}
	err := c.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: nodeId},
		allocatedNode)
	// nolint: wrapcheck
	return allocatedNode, err
}
