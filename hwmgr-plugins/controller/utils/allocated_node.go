/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	AllocatedNodeSpecNodeAllocationRequestKey = "spec.nodeAllocationRequest"
)

// GetNode get a node resource for a provided name
func GetNode(
	ctx context.Context,
	logger *slog.Logger,
	c client.Reader,
	namespace, nodename string) (*pluginv1alpha1.AllocatedNode, error) {

	logger.InfoContext(ctx, "Getting AllocatedNode", slog.String("nodename", nodename))

	node := &pluginv1alpha1.AllocatedNode{}

	if err := sharedutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: namespace}, node)
	}); err != nil {
		return node, fmt.Errorf("failed to get AllocatedNode for update: %w", err)
	}
	return node, nil
}

// GetNodeList retrieves the node list
func GetNodeList(ctx context.Context, c client.Client) (*pluginv1alpha1.AllocatedNodeList, error) {

	nodeList := &pluginv1alpha1.AllocatedNodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return nodeList, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}

	return nodeList, nil
}

// GetBMHToNodeMap get a list of nodes, mapped to BMH namespace/name
func GetBMHToNodeMap(ctx context.Context,
	logger *slog.Logger,
	c client.Client) (map[string]pluginv1alpha1.AllocatedNode, error) {
	nodes := make(map[string]pluginv1alpha1.AllocatedNode)

	nodelist, err := GetNodeList(ctx, c)
	if err != nil {
		logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nodes, fmt.Errorf("failed to query node list: %w", err)
	}

	for _, node := range nodelist.Items {
		bmhName := node.Spec.HwMgrNodeId
		bmhNamespace := node.Spec.HwMgrNodeNs

		if bmhName != "" && bmhNamespace != "" {
			nodes[bmhNamespace+"/"+bmhName] = node
		}
	}

	return nodes, nil
}

func GetNodeForBMH(nodes map[string]pluginv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) *pluginv1alpha1.AllocatedNode {
	bmhName := bmh.Name
	bmhNamespace := bmh.Namespace

	if node, exists := nodes[bmhNamespace+"/"+bmhName]; exists {
		return &node
	}
	return nil
}

// GenerateNodeName
func GenerateNodeName() string {
	return uuid.NewString()
}

func FindNodeInList(nodelist pluginv1alpha1.AllocatedNodeList, hardwarePluginRef, nodeId string) string {
	for _, node := range nodelist.Items {
		if node.Spec.HardwarePluginRef == hardwarePluginRef && node.Spec.HwMgrNodeId == nodeId {
			return node.Name
		}
	}
	return ""
}

// GetChildNodes gets a list of nodes allocated to a NodeAllocationRequest
func GetChildNodes(
	ctx context.Context,
	logger *slog.Logger,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) (*pluginv1alpha1.AllocatedNodeList, error) {

	nodelist := &pluginv1alpha1.AllocatedNodeList{}

	opts := []client.ListOption{
		client.MatchingFields{"spec.nodeAllocationRequest": nodeAllocationRequest.Name},
	}

	if err := sharedutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.List(ctx, nodelist, opts...)
	}); err != nil {
		logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to query node list: %w", err)
	}

	return nodelist, nil
}

// SetNodeConditionStatus sets a condition on the AllocatedNode status with the provided condition type
func SetNodeConditionStatus(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	nodename, namespace string,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		node := &pluginv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: nodename, Namespace: namespace}, node); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}

		SetStatusCondition(
			&node.Status.Conditions,
			conditionType,
			reason,
			conditionStatus,
			message,
		)

		return c.Status().Update(ctx, node)
	})
}

func SetNodeFailedStatus(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	node *pluginv1alpha1.AllocatedNode,
	conditionType string,
	message string,
) error {

	SetStatusCondition(&node.Status.Conditions, conditionType, string(pluginv1alpha1.Failed), metav1.ConditionFalse, message)

	if err := c.Status().Update(ctx, node); err != nil {
		logger.ErrorContext(ctx, "Failed to update node status with failure",
			slog.String("node", node.Name),
			slog.String("conditionType", conditionType),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to set node failed status: %w", err)
	}

	logger.InfoContext(ctx, "Node status set to failed",
		slog.String("node", node.Name),
		slog.String("conditionType", conditionType),
		slog.String("reason", string(pluginv1alpha1.Failed)))
	return nil
}
