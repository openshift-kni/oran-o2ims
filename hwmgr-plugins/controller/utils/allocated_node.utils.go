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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	c client.Client,
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

// GenerateNodeName
func GenerateNodeName() string {
	return uuid.NewString()
}

func FindNodeInList(nodelist pluginv1alpha1.AllocatedNodeList, hwMgrId, nodeId string) string {
	for _, node := range nodelist.Items {
		if node.Spec.HwMgrId == hwMgrId && node.Spec.HwMgrNodeId == nodeId {
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
		client.MatchingFields{"spec.allocatedNodeRequest": nodeAllocationRequest.Name},
	}

	if err := sharedutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.List(ctx, nodelist, opts...)
	}); err != nil {
		logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to query node list: %w", err)
	}

	return nodelist, nil
}
