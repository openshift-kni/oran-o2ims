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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	AllocatedNodeSpecNodeAllocationRequestKey = "spec.nodeAllocationRequest"
	AllocatedNodeFinalizer                    = "clcm.openshift.io/allocatednode-finalizer"
)

// GetNode get a node resource for a provided name
func GetNode(
	ctx context.Context,
	logger *slog.Logger,
	c client.Reader,
	namespace, nodename string) (*pluginsv1alpha1.AllocatedNode, error) {

	logger.InfoContext(ctx, "Getting AllocatedNode", slog.String("nodename", nodename))

	node := &pluginsv1alpha1.AllocatedNode{}

	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: namespace}, node)
	}); err != nil {
		return node, fmt.Errorf("failed to get AllocatedNode for update: %w", err)
	}
	return node, nil
}

// GetNodeList retrieves the node list
func GetNodeList(ctx context.Context, c client.Client) (*pluginsv1alpha1.AllocatedNodeList, error) {

	nodeList := &pluginsv1alpha1.AllocatedNodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return nodeList, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}

	return nodeList, nil
}

// GetBMHToNodeMap get a list of nodes, mapped to BMH namespace/name
func GetBMHToNodeMap(ctx context.Context,
	logger *slog.Logger,
	c client.Client) (map[string]pluginsv1alpha1.AllocatedNode, error) {
	nodes := make(map[string]pluginsv1alpha1.AllocatedNode)

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

func GetNodeForBMH(nodes map[string]pluginsv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) *pluginsv1alpha1.AllocatedNode {
	bmhName := bmh.Name
	bmhNamespace := bmh.Namespace

	if node, exists := nodes[bmhNamespace+"/"+bmhName]; exists {
		return &node
	}
	return nil
}

// GenerateNodeName generates a deterministic AllocatedNode name based on the hardware plugin identifier,
// clusterID, BareMetalHost namespace and name. The generated name complies with Kubernetes
// Custom Resource naming specifications (RFC 1123 subdomain).
func GenerateNodeName(pluginID, clusterID, bmhNamespace, bmhName string) string {

	// Create deterministic name using hardware pluginID, clusterID, BMH namespace, BMH name
	// Format: <plugin-id>-<cluster-id>-<bmh-namespace>-<bmh-name>
	// This ensures uniqueness across different namespaces and BMHs
	baseName := fmt.Sprintf("%s-%s-%s-%s", pluginID, clusterID, bmhNamespace, bmhName)

	// Sanitize the name to ensure Kubernetes compliance
	sanitizedName := sanitizeKubernetesName(baseName)

	// Ensure the name doesn't exceed Kubernetes name length limits (253 characters)
	if sanitizedName == "" || len(sanitizedName) > 253 {
		// If too long, use a hash-based approach for deterministic truncation
		clusterUUID := uuid.NewSHA1(uuid.Nil, []byte(clusterID))
		hash := ctlrutils.MakeUUIDFromNames(bmhNamespace, clusterUUID, bmhName, pluginID)
		sanitizedName = fmt.Sprintf("%s-%s", pluginID, hash.String()[:10])
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

func FindNodeInList(nodelist pluginsv1alpha1.AllocatedNodeList, hardwarePluginRef, nodeId string) string {
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
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (*pluginsv1alpha1.AllocatedNodeList, error) {

	nodelist := &pluginsv1alpha1.AllocatedNodeList{}

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
		node := &pluginsv1alpha1.AllocatedNode{}
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
	node *pluginsv1alpha1.AllocatedNode,
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

func AllocatedNodeAddFinalizer(
	ctx context.Context,
	noncachedClient client.Reader,
	c client.Client,
	allocatedNode *pluginsv1alpha1.AllocatedNode,
) error {
	// nolint: wrapcheck
	err := ctlrutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newAllocatedNode := &pluginsv1alpha1.AllocatedNode{}
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
	allocatedNode *pluginsv1alpha1.AllocatedNode,
) error {
	// nolint: wrapcheck
	err := ctlrutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newAllocatedNode := &pluginsv1alpha1.AllocatedNode{}
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
