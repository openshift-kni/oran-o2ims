/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	AllocatedNodeSpecNodeAllocationRequestKey = "spec.nodeAllocationRequest"
	AllocatedNodeFinalizer                    = "clcm.openshift.io/allocatednode-finalizer"
)

// RegisterAllocatedNodeFieldIndexer registers a field indexer for
// spec.nodeAllocationRequest on AllocatedNode CRs. This allows efficient
// filtering by NAR name via client.MatchingFields.
func RegisterAllocatedNodeFieldIndexer(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &hwmgmtv1alpha1.AllocatedNode{},
		AllocatedNodeSpecNodeAllocationRequestKey,
		func(obj client.Object) []string {
			return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
		}); err != nil {
		return fmt.Errorf("failed to register AllocatedNode field indexer: %w", err)
	}
	return nil
}

// GetNode get a node resource for a provided name
func GetNode(
	ctx context.Context,
	logger *slog.Logger,
	c client.Reader,
	namespace, nodename string) (*hwmgmtv1alpha1.AllocatedNode, error) {

	logger.InfoContext(ctx, "Getting AllocatedNode", slog.String("nodename", nodename))

	node := &hwmgmtv1alpha1.AllocatedNode{}

	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: namespace}, node)
	}); err != nil {
		return node, fmt.Errorf("failed to get AllocatedNode for update: %w", err)
	}
	return node, nil
}

// GetNodeList retrieves the node list
func GetNodeList(ctx context.Context, c client.Client) (*hwmgmtv1alpha1.AllocatedNodeList, error) {

	nodeList := &hwmgmtv1alpha1.AllocatedNodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return nodeList, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}

	return nodeList, nil
}

// GetBMHToNodeMap get a list of nodes, mapped to BMH namespace/name
func GetBMHToNodeMap(ctx context.Context,
	logger *slog.Logger,
	c client.Client) (map[string]hwmgmtv1alpha1.AllocatedNode, error) {
	nodes := make(map[string]hwmgmtv1alpha1.AllocatedNode)

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

func GetNodeForBMH(nodes map[string]hwmgmtv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) *hwmgmtv1alpha1.AllocatedNode {
	bmhName := bmh.Name
	bmhNamespace := bmh.Namespace

	if node, exists := nodes[bmhNamespace+"/"+bmhName]; exists {
		return &node
	}
	return nil
}

// GenerateNodeName generates a deterministic AllocatedNode name based on the clusterID,
// BareMetalHost namespace and name. The generated name complies with Kubernetes
// Custom Resource naming specifications (RFC 1123 subdomain).
func GenerateNodeName(clusterID, bmhNamespace, bmhName string) string {

	// Format: <cluster-id>-<bmh-namespace>-<bmh-name>
	baseName := fmt.Sprintf("%s-%s-%s", clusterID, bmhNamespace, bmhName)

	// Sanitize the name to ensure Kubernetes compliance
	sanitizedName := sanitizeKubernetesName(baseName)

	// Ensure the name doesn't exceed Kubernetes name length limits (253 characters)
	if sanitizedName == "" || len(sanitizedName) > 253 {
		// If too long, use a hash-based approach for deterministic truncation
		clusterUUID := uuid.NewSHA1(uuid.Nil, []byte(clusterID))
		hash := uuid.NewSHA1(clusterUUID, []byte(bmhNamespace+"/"+bmhName))
		sanitizedName = hash.String()[:10]
	}

	return sanitizedName
}

// sanitizeKubernetesName sanitizes a string to comply with Kubernetes resource name requirements.
// Kubernetes names must be RFC 1123 compliant:
// - contain only lowercase alphanumeric characters or '-'
// - start and end with an alphanumeric character
// - be no more than 253 characters long
func sanitizeKubernetesName(name string) string {
	// Convert to lowercase
	result := strings.ToLower(name)

	// Replace invalid characters with hyphens
	// Keep only alphanumeric characters and hyphens
	reg := regexp.MustCompile("[^a-z0-9-]")
	result = reg.ReplaceAllString(result, "-")

	// Remove consecutive hyphens
	reg = regexp.MustCompile("-+")
	result = reg.ReplaceAllString(result, "-")

	// Ensure it starts with alphanumeric character
	reg = regexp.MustCompile("^[^a-z0-9]+")
	result = reg.ReplaceAllString(result, "")

	// Ensure it ends with alphanumeric character
	reg = regexp.MustCompile("[^a-z0-9]+$")
	result = reg.ReplaceAllString(result, "")

	return result
}

// GetChildNodes gets a list of nodes allocated to a NodeAllocationRequest
func GetChildNodes(
	ctx context.Context,
	logger *slog.Logger,
	c client.Client,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) (*hwmgmtv1alpha1.AllocatedNodeList, error) {

	nodelist := &hwmgmtv1alpha1.AllocatedNodeList{}

	opts := []client.ListOption{
		client.MatchingFields{"spec.nodeAllocationRequest": nodeAllocationRequest.Name},
	}

	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.List(ctx, nodelist, opts...)
	}); err != nil {
		logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to query node list: %w", err)
	}

	return nodelist, nil
}

// GetChildNodesUncached lists AllocatedNode CRs for a NodeAllocationRequest using the non-cached client.
func GetChildNodesUncached(
	ctx context.Context,
	noncachedClient client.Reader,
	namespace string,
	narName string,
) (*hwmgmtv1alpha1.AllocatedNodeList, error) {

	allNodes := &hwmgmtv1alpha1.AllocatedNodeList{}
	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return noncachedClient.List(ctx, allNodes, client.InNamespace(namespace))
	}); err != nil {
		return nil, fmt.Errorf("failed to list AllocatedNodes in namespace %s: %w", namespace, err)
	}

	filtered := &hwmgmtv1alpha1.AllocatedNodeList{}
	for i := range allNodes.Items {
		if allNodes.Items[i].Spec.NodeAllocationRequest == narName {
			filtered.Items = append(filtered.Items, allNodes.Items[i])
		}
	}
	return filtered, nil
}

// SetNodeConditionStatus sets a condition on the AllocatedNode status with the provided
// condition type. It sets both the in-memory node and the API node status condition.
func SetNodeConditionStatus(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	node *hwmgmtv1alpha1.AllocatedNode,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
) error {
	// Sync the condition to the caller's in-memory node
	SetStatusCondition(&node.Status.Conditions, conditionType, reason, conditionStatus, message)

	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		freshNode := &hwmgmtv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, freshNode); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}

		SetStatusCondition(&freshNode.Status.Conditions, conditionType, reason, conditionStatus, message)
		return c.Status().Update(ctx, freshNode)
	})
}

func SetNodeFailedStatus(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	node *hwmgmtv1alpha1.AllocatedNode,
	conditionType string,
	message string,
) error {

	SetStatusCondition(&node.Status.Conditions, conditionType, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse, message)

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
		slog.String("reason", string(hwmgmtv1alpha1.Failed)))
	return nil
}

// SetNodeConfigApplied sets a node's Configured condition to ConfigApplied and
// updates the node's status.hwProfile to the new hardware profile.
func SetNodeConfigApplied(ctx context.Context,
	c client.Client, noncachedClient client.Reader, logger *slog.Logger,
	node *hwmgmtv1alpha1.AllocatedNode, newHwProfile string) error {
	if err := SetNodeHwProfile(ctx, c, node, newHwProfile); err != nil {
		return fmt.Errorf("failed to update node hwProfile on node %s: %w", node.Name, err)
	}

	// Sync in-memory node state
	node.Status.HwProfile = newHwProfile
	SetStatusCondition(&node.Status.Conditions,
		string(hwmgmtv1alpha1.Configured),
		string(hwmgmtv1alpha1.ConfigApplied),
		metav1.ConditionTrue,
		string(hwmgmtv1alpha1.ConfigSuccess))

	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		freshNode := &hwmgmtv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, freshNode); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}
		// Update the node's status to reflect the new hardware profile.
		freshNode.Status.HwProfile = newHwProfile
		SetStatusCondition(&freshNode.Status.Conditions,
			string(hwmgmtv1alpha1.Configured),
			string(hwmgmtv1alpha1.ConfigApplied),
			metav1.ConditionTrue,
			string(hwmgmtv1alpha1.ConfigSuccess))
		if err := c.Status().Update(ctx, freshNode); err != nil {
			return fmt.Errorf("failed to update node config applied status: %w", err)
		}
		logger.InfoContext(ctx, "Set node config applied", slog.String("node", node.Name))
		return nil
	})
}

// SetNodeConfigUpdatePending sets a node's Configured condition to ConfigUpdatePending
// and ensures the node's hardware profile is the new one.
func SetNodeConfigUpdatePending(ctx context.Context,
	c client.Client, noncachedClient client.Reader, logger *slog.Logger,
	node *hwmgmtv1alpha1.AllocatedNode, newHwProfile, message string) error {

	if err := SetNodeHwProfile(ctx, c, node, newHwProfile); err != nil {
		return fmt.Errorf("failed to update node hwProfile on node %s: %w", node.Name, err)
	}

	cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
	if cond != nil &&
		cond.Reason == string(hwmgmtv1alpha1.ConfigUpdatePending) &&
		cond.Status == metav1.ConditionFalse &&
		cond.Message == message {
		// Condition already matches, no need to update
		return nil
	}

	if err := SetNodeConditionStatus(
		ctx, c, noncachedClient, node,
		string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
		string(hwmgmtv1alpha1.ConfigUpdatePending), message); err != nil {
		return fmt.Errorf("failed to set node config update pending status: %w", err)
	}
	logger.InfoContext(ctx, "Set node config update pending", slog.String("node", node.Name))
	return nil
}

// SetNodeConfigUpdateRequested sets a node's Configured condition to ConfigUpdateRequested
// and ensures the node's hardware profile is the new one.
// Skips the condition update if the condition already matches (avoids unnecessary API writes).
func SetNodeConfigUpdateRequested(ctx context.Context,
	c client.Client, noncachedClient client.Reader, logger *slog.Logger,
	node *hwmgmtv1alpha1.AllocatedNode, newHwProfile, message string) error {
	// Ensure the node's hardware profile is the new one
	err := SetNodeHwProfile(ctx, c, node, newHwProfile)
	if err != nil {
		return fmt.Errorf("failed to update node hwProfile on node %s: %w", node.Name, err)
	}

	cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
	if cond != nil &&
		cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate) &&
		cond.Status == metav1.ConditionFalse &&
		cond.Message == message {
		// Condition already matches, no need to update
		return nil
	}

	if err := SetNodeConditionStatus(
		ctx, c, noncachedClient, node,
		string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
		string(hwmgmtv1alpha1.ConfigUpdate), message); err != nil {
		return fmt.Errorf("failed to update node config update requested status: %w", err)
	}
	logger.InfoContext(ctx, "Set node config update requested", slog.String("node", node.Name))
	return nil
}

// SetNodeHwProfile sets the node's hardware profile to the new one if it's not already set.
func SetNodeHwProfile(ctx context.Context, c client.Client, node *hwmgmtv1alpha1.AllocatedNode, newHwProfile string) error {
	if newHwProfile != "" && node.Spec.HwProfile != newHwProfile {
		patch := client.MergeFrom(node.DeepCopy())
		node.Spec.HwProfile = newHwProfile
		if err := c.Patch(ctx, node, patch); err != nil {
			return fmt.Errorf("failed to patch AllocatedNode %s hw profile: %w", node.Name, err)
		}
	}
	return nil
}

func AllocatedNodeAddFinalizer(
	ctx context.Context,
	noncachedClient client.Reader,
	c client.Client,
	allocatedNode *hwmgmtv1alpha1.AllocatedNode,
) error {
	// nolint: wrapcheck
	err := ctlrutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newAllocatedNode := &hwmgmtv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, client.ObjectKeyFromObject(allocatedNode), newAllocatedNode); err != nil {
			return err
		}
		controllerutil.AddFinalizer(newAllocatedNode, AllocatedNodeFinalizer)
		if err := c.Update(ctx, newAllocatedNode); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to add finalizer to NodeAllocationRequest: %w", err)
	}
	return nil
}

func AllocatedNodeRemoveFinalizer(
	ctx context.Context,
	noncachedClient client.Reader,
	c client.Client,
	allocatedNode *hwmgmtv1alpha1.AllocatedNode,
) error {
	// nolint: wrapcheck
	err := ctlrutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newAllocatedNode := &hwmgmtv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, client.ObjectKeyFromObject(allocatedNode), newAllocatedNode); err != nil {
			return err
		}
		controllerutil.RemoveFinalizer(newAllocatedNode, AllocatedNodeFinalizer)
		if err := c.Update(ctx, newAllocatedNode); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to remove finalizer from AllocatedNode: %w", err)
	}
	return nil
}
