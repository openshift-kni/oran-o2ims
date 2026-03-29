/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const ConfigAnnotation = "clcm.openshift.io/config-in-progress"

// hasNodeGroupHwProfileChanges checks whether any node group in the NAR has a different
// HW profile than what is currently assigned to its allocated nodes.
func hasNodeGroupHwProfileChanges(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (bool, error) {
	nodelist, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return false, fmt.Errorf("failed to get child nodes for HW profile change check: %w", err)
	}

	for _, group := range nodeAllocationRequest.Spec.NodeGroup {
		for i := range nodelist.Items {
			if nodelist.Items[i].Spec.GroupName == group.NodeGroupData.Name &&
				nodelist.Items[i].Spec.HwProfile != group.NodeGroupData.HwProfile {
				return true, nil
			}
		}
	}
	return false, nil
}

// enableBMOManagementForIBINodes sets spec.online=true and removes the detached annotation
// on IBI-provisioned BMHs (externallyProvisioned=true with detached annotation) so that BMO
// can fully manage them. This is called when the cluster is reported as fully provisioned.
func enableBMOManagementForIBINodes(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) error {
	nodelist, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get child nodes for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	for i := range nodelist.Items {
		node := &nodelist.Items[i]
		bmh, err := getBMHForNode(ctx, noncachedClient, node)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get BMH for node, skipping IBI management setup",
				slog.String("node", node.Name), slog.String("error", err.Error()))
			continue
		}

		if !bmh.Spec.ExternallyProvisioned {
			continue
		}

		_, hasDetached := bmh.Annotations[metal3v1alpha1.DetachedAnnotation]
		if !hasDetached && bmh.Spec.Online {
			// Already managed by BMO
			continue
		}

		bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}

		if !bmh.Spec.Online {
			logger.InfoContext(ctx, "Setting BMH online=true for IBI post-provisioning",
				slog.String("bmh", bmh.Name))
			if err := patchBMHOnline(ctx, c, bmh, true); err != nil {
				return fmt.Errorf("failed to set online=true on BMH %s: %w", bmh.Name, err)
			}
		}

		if hasDetached {
			logger.InfoContext(ctx, "Removing detached annotation for IBI post-provisioning",
				slog.String("bmh", bmh.Name))
			if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation,
				metal3v1alpha1.DetachedAnnotation, "", OpRemove); err != nil {
				return fmt.Errorf("failed to remove detached annotation from BMH %s: %w", bmh.Name, err)
			}
		}
	}

	return nil
}

// UpdateAbandonedAnnotation marks an AllocatedNode whose in-progress hardware update
// was abandoned because the desired hardware profile changed mid-flight. The node is
// safe to re-process with the new profile on the next reconcile.
const UpdateAbandonedAnnotation = "clcm.openshift.io/update-abandoned"

const (
	DoNotRequeue               = 0
	RequeueAfterShortInterval  = 15
	RequeueAfterMediumInterval = 30
	RequeueAfterLongInterval   = 60
)

func setConfigAnnotation(object client.Object, reason string) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[ConfigAnnotation] = reason
	object.SetAnnotations(annotations)
}

func getConfigAnnotation(object client.Object) string {
	annotations := object.GetAnnotations()
	if annotations == nil {
		return ""
	}
	if val, ok := annotations[ConfigAnnotation]; ok {
		return val
	}
	return ""
}

func removeConfigAnnotation(object client.Object) {
	annotations := object.GetAnnotations()
	if annotations != nil {
		delete(annotations, ConfigAnnotation)
		object.SetAnnotations(annotations)
	}
}

// clearConfigAnnotationWithPatch removes the config-in-progress annotation from an AllocatedNode and patches it
func clearConfigAnnotationWithPatch(ctx context.Context, c client.Client, node *pluginsv1alpha1.AllocatedNode) error {
	// Create a patch to remove the annotation
	patch := client.MergeFrom(node.DeepCopy())

	// Remove the annotation from the local copy
	removeConfigAnnotation(node)

	// Apply the patch
	if err := c.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to clear config annotation from AllocatedNode %s: %w", node.Name, err)
	}
	return nil
}

func hasUpdateAbandonedAnnotation(node *pluginsv1alpha1.AllocatedNode) bool {
	if node.Annotations == nil {
		return false
	}
	_, ok := node.Annotations[UpdateAbandonedAnnotation]
	return ok
}

func setUpdateAbandonedAnnotation(ctx context.Context, c client.Client, node *pluginsv1alpha1.AllocatedNode) error {
	patch := client.MergeFrom(node.DeepCopy())
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[UpdateAbandonedAnnotation] = "true"
	if err := c.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to set update-abandoned annotation on AllocatedNode %s: %w", node.Name, err)
	}
	return nil
}

func clearUpdateAbandonedAnnotation(ctx context.Context, c client.Client, node *pluginsv1alpha1.AllocatedNode) error {
	if !hasUpdateAbandonedAnnotation(node) {
		return nil
	}
	patch := client.MergeFrom(node.DeepCopy())
	delete(node.Annotations, UpdateAbandonedAnnotation)
	if err := c.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to clear update-abandoned annotation from AllocatedNode %s: %w", node.Name, err)
	}
	return nil
}

// findNodesInProgress scans the nodelist to find all nodes in InProgress state or no condition
func findNodesInProgress(nodelist *pluginsv1alpha1.AllocatedNodeList) []*pluginsv1alpha1.AllocatedNode {
	var nodes []*pluginsv1alpha1.AllocatedNode
	for _, node := range nodelist.Items {
		condition := meta.FindStatusCondition(node.Status.Conditions, (string(hwmgmtv1alpha1.Provisioned)))
		if condition == nil || (condition.Status == metav1.ConditionFalse && condition.Reason == string(hwmgmtv1alpha1.InProgress)) {
			nodes = append(nodes, &node)
		}
	}

	return nodes
}

func applyPostConfigUpdates(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	node *pluginsv1alpha1.AllocatedNode) (int, error) {

	// nolint:wrapcheck
	err := retry.OnError(retry.DefaultRetry, k8serrors.IsConflict, func() error {
		updatedNode := &pluginsv1alpha1.AllocatedNode{}

		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, updatedNode); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}

		removeConfigAnnotation(updatedNode)
		if err := c.Update(ctx, updatedNode); err != nil {
			return fmt.Errorf("failed to remove annotation for node %s/%s: %w", updatedNode.Name, updatedNode.Namespace, err)
		}

		hwmgrutils.SetStatusCondition(&updatedNode.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned),
			string(hwmgmtv1alpha1.Completed),
			metav1.ConditionTrue,
			"Provisioned")
		if err := c.Status().Update(ctx, updatedNode); err != nil {
			return fmt.Errorf("failed to update node status: %w", err)
		}

		return nil
	})
	if err != nil {
		return RequeueAfterShortInterval, fmt.Errorf("failed to remove annotation for node %s/%s: %w", node.Name, node.Namespace, err)
	}

	return DoNotRequeue, nil
}

// deriveNARStatusFromSingleNode determines the NAR Configured status condition based on
// the single child AllocatedNode's status for a single-node cluster.
// AllocatedNode name is included in the message, for example:
//
//	"Configuration update in progress (AllocatedNode <name>)"
//	"Configuration update failed (AllocatedNode <name>: <error>)"
func deriveNARStatusFromSingleNode(
	ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	node *pluginsv1alpha1.AllocatedNode,
) (metav1.ConditionStatus, string, string) {
	updatedNode, err := hwmgrutils.GetNode(ctx, logger, noncachedClient, node.Namespace, node.Name)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to fetch updated AllocatedNode",
			slog.String("name", node.Name), slog.String("error", err.Error()))
		return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
			fmt.Sprintf("AllocatedNode %s could not be fetched: %v", node.Name, err)
	}

	cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
	if cond != nil &&
		cond.Status == metav1.ConditionTrue &&
		cond.Reason == string(hwmgmtv1alpha1.ConfigApplied) {
		return metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigApplied),
			string(hwmgmtv1alpha1.ConfigSuccess)
	}
	if cond != nil &&
		cond.Status == metav1.ConditionFalse &&
		(cond.Reason == string(hwmgmtv1alpha1.InvalidInput) ||
			cond.Reason == string(hwmgmtv1alpha1.Failed)) {
		return metav1.ConditionFalse, string(hwmgmtv1alpha1.Failed),
			fmt.Sprintf("%s (AllocatedNode %s: %s)", string(hwmgmtv1alpha1.ConfigFailed), node.Name, cond.Message)
	}
	return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
		fmt.Sprintf("%s (AllocatedNode %s)", string(hwmgmtv1alpha1.ConfigInProgress), node.Name)
}

// deriveNARStatusFromMultipleNodes determines the NAR Configured status condition based on
// all child AllocatedNodes' statuses for a multi-node cluster. The NAR is considered failed
// as long as there is a failed AllocatedNode.
// Per-group progress is reported in the message, for example:
//
//	"Configuration update in progress (group master: 2/3 completed, group worker: 0/10 completed)"
//	"Configuration update failed (group master: 1/3 failed, group worker: 0/10 completed)"
func deriveNARStatusFromMultipleNodes(
	ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
	nar *pluginsv1alpha1.NodeAllocationRequest,
) (metav1.ConditionStatus, string, string) {

	// Track the number of completed, failed, and total nodes per group
	type groupCounts struct {
		completed int
		failed    int
		total     int
	}

	groupStats := make(map[string]*groupCounts)
	for _, node := range nodelist.Items {
		updatedNode, err := hwmgrutils.GetNode(ctx, logger, noncachedClient, node.Namespace, node.Name)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to fetch updated AllocatedNode",
				slog.String("name", node.Name), slog.String("error", err.Error()))
			return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
				fmt.Sprintf("AllocatedNode %s could not be fetched: %v", node.Name, err)
		}

		groupCount := groupStats[node.Spec.GroupName]
		if groupCount == nil {
			groupCount = &groupCounts{}
			groupStats[node.Spec.GroupName] = groupCount
		}
		groupCount.total++

		// Count completed and failed nodes for the group, the rest are in progress nodes.
		cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		if cond != nil &&
			cond.Status == metav1.ConditionTrue &&
			cond.Reason == string(hwmgmtv1alpha1.ConfigApplied) {
			groupCount.completed++
			continue
		}
		if cond != nil &&
			cond.Status == metav1.ConditionFalse &&
			(cond.Reason == string(hwmgmtv1alpha1.InvalidInput) ||
				cond.Reason == string(hwmgmtv1alpha1.Failed)) {
			groupCount.failed++
			continue
		}
	}

	groups := getGroupsSortedByRole(nar)
	buildGroupDetail := func() string {
		var parts []string
		for _, group := range groups {
			name := group.NodeGroupData.Name
			groupCount := groupStats[name]
			if groupCount == nil || groupCount.total == 0 {
				continue
			}
			if groupCount.failed > 0 {
				parts = append(parts, fmt.Sprintf("group %s: %d/%d failed",
					name, groupCount.failed, groupCount.total))
			} else {
				parts = append(parts, fmt.Sprintf("group %s: %d/%d completed",
					name, groupCount.completed, groupCount.total))
			}
		}
		return strings.Join(parts, ", ")
	}

	var overallCompleted, overallFailed int
	for _, groupStat := range groupStats {
		overallCompleted += groupStat.completed
		overallFailed += groupStat.failed
	}

	if overallCompleted == len(nodelist.Items) {
		return metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigApplied), string(hwmgmtv1alpha1.ConfigSuccess)
	}
	if overallFailed > 0 {
		return metav1.ConditionFalse, string(hwmgmtv1alpha1.Failed),
			fmt.Sprintf("%s (%s)", string(hwmgmtv1alpha1.ConfigFailed), buildGroupDetail())
	}
	return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
		fmt.Sprintf("%s (%s)", string(hwmgmtv1alpha1.ConfigInProgress), buildGroupDetail())
}

// getGroupsSortedByRole returns NodeGroups sorted master-first, preserving spec order within same role.
func getGroupsSortedByRole(nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) []pluginsv1alpha1.NodeGroup {
	var groupPriority = func(role string) int {
		switch strings.ToLower(role) {
		case hwmgmtv1alpha1.NodeRoleMaster:
			return 0
		default:
			return 1
		}
	}

	groups := append([]pluginsv1alpha1.NodeGroup(nil), nodeAllocationRequest.Spec.NodeGroup...)
	sort.SliceStable(groups, func(i, j int) bool {
		return groupPriority(groups[i].NodeGroupData.Role) < groupPriority(groups[j].NodeGroupData.Role)
	})
	return groups
}

// createNode creates an AllocatedNode CR with specified attributes
func createNode(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	nodename, nodeId, nodeNs, groupname, hwprofile string) (*pluginsv1alpha1.AllocatedNode, error) {
	logger.InfoContext(ctx, "Ensuring AllocatedNode exists",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

	nodeKey := types.NamespacedName{
		Name:      nodename,
		Namespace: pluginNamespace,
	}

	existing := &pluginsv1alpha1.AllocatedNode{}
	err := c.Get(ctx, nodeKey, existing)
	if err == nil {
		logger.InfoContext(ctx, "AllocatedNode already exists, skipping create", slog.String("nodename", nodename))
		return existing, nil
	}

	if !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check if AllocatedNode exists: %w", err)
	}

	blockDeletion := true
	node := &pluginsv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodename,
			Namespace: pluginNamespace,
			Labels: map[string]string{
				hwmgrutils.HardwarePluginLabel: hwmgrutils.Metal3HardwarePluginID,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodeAllocationRequest.APIVersion,
				Kind:               nodeAllocationRequest.Kind,
				Name:               nodeAllocationRequest.Name,
				UID:                nodeAllocationRequest.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Spec: pluginsv1alpha1.AllocatedNodeSpec{
			NodeAllocationRequest: nodeAllocationRequest.Name,
			GroupName:             groupname,
			HwProfile:             hwprofile,
			HardwarePluginRef:     nodeAllocationRequest.Spec.HardwarePluginRef,
			HwMgrNodeNs:           nodeNs,
			HwMgrNodeId:           nodeId,
		},
	}

	if err := c.Create(ctx, node); err != nil {
		return nil, fmt.Errorf("failed to create AllocatedNode: %w", err)
	}

	logger.InfoContext(ctx, "AllocatedNode created", slog.String("nodename", nodename))
	return node, nil
}

// updateNodeStatus updates an AllocatedNode CR status field with additional node information
func updateNodeStatus(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	info bmhNodeInfo, nodename, hwprofile string, updating bool) error {
	logger.InfoContext(ctx, "Updating AllocatedNode", slog.String("nodename", nodename))
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, k8serrors.IsConflict, func() error {
		node := &pluginsv1alpha1.AllocatedNode{}

		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: nodename, Namespace: pluginNamespace}, node); err != nil {
			return fmt.Errorf("failed to fetch AllocatedNode: %w", err)
		}

		logger.InfoContext(ctx, "Retrying update for AllocatedNode", slog.String("nodename", nodename))

		logger.InfoContext(ctx, "Adding info to AllocatedNode",
			slog.String("nodename", nodename),
			slog.Any("info", info))

		node.Status.BMC = &pluginsv1alpha1.BMC{
			Address:         info.BMC.Address,
			CredentialsName: info.BMC.CredentialsName,
		}
		node.Status.Interfaces = info.Interfaces

		if updating {
			reason := hwmgmtv1alpha1.InProgress
			message := "Hardware configuration in progress"
			status := metav1.ConditionFalse
			hwmgrutils.SetStatusCondition(&node.Status.Conditions,
				string(hwmgmtv1alpha1.Provisioned),
				string(reason),
				status,
				message)
		}

		node.Status.HwProfile = hwprofile

		return c.Status().Update(ctx, node)

	})
}

// checkNodeAllocationRequestProgress checks to see if a NodeAllocationRequest is fully allocated,
// allocating additional resources as needed
func checkNodeAllocationRequestProgress(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (ctrl.Result, bool, error) {

	// Check if we're fully allocated now that processing is complete
	full := isNodeAllocationRequestFullyAllocated(ctx, noncachedClient, logger, pluginNamespace, nodeAllocationRequest)
	if !full {
		// Still not fully allocated, continue processing
		res, err := processNodeAllocationRequestAllocation(ctx, c, noncachedClient, logger, pluginNamespace, nodeAllocationRequest)
		return res, false, err
	}

	// check if there are any pending work such as bios configuring
	res, updating, err := checkForPendingUpdate(ctx, c, noncachedClient, logger, pluginNamespace, nodeAllocationRequest)
	if err != nil || res.Requeue || res.RequeueAfter > 0 {
		return res, false, err
	}
	if updating {
		return hwmgrutils.RequeueWithShortInterval(), false, nil
	}
	return ctrl.Result{}, true, nil
}

// processNewNodeAllocationRequest processes a new NodeAllocationRequest CR, verifying that there are enough free
// resources to satisfy the request
func processNewNodeAllocationRequest(ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	logger.InfoContext(ctx, "Processing processNewNodeAllocationRequest request")

	// Check if enough resources are available for each NodeGroup
	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Fetch unallocated BMHs for the specific site and NodeGroupData using non-cached client
		// to avoid race conditions with concurrent allocations
		bmhListForGroup, err := fetchBMHList(ctx, noncachedClient, logger, nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData)
		if err != nil {
			return fmt.Errorf("unable to fetch BMHs for nodegroup=%s: %w", nodeGroup.NodeGroupData.Name, err)
		}

		// Ensure enough resources exist that satisfy the request
		if len(bmhListForGroup.Items) < nodeGroup.Size {
			return fmt.Errorf("not enough free resources matching nodegroup=%s criteria: freenodes=%d, required=%d",
				nodeGroup.NodeGroupData.Name, len(bmhListForGroup.Items), nodeGroup.Size)
		}
	}

	return nil
}

// isNodeAllocationRequestFullyAllocated checks to see if a NodeAllocationRequest CR has been fully allocated
func isNodeAllocationRequestFullyAllocated(ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) bool {

	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		allocatedNodes := countNodesInGroup(ctx, noncachedClient, logger, pluginNamespace, nodeAllocationRequest.Status.Properties.NodeNames, nodeGroup.NodeGroupData.Name)
		if allocatedNodes < nodeGroup.Size {
			return false // At least one group is not fully allocated
		}
	}
	return true
}

// handleNodeInProgressUpdate progresses a node that is in progress of being configured.
// If its associated BMH status indicates that the update has completed, it validates the
// configuration, waits for the K8s node to be Ready on the spoke cluster, uncordons the node
// if multi-node cluster, marks the node status as complete, and clears the config annotation.
func handleNodeInProgressUpdate(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	node *pluginsv1alpha1.AllocatedNode,
	nodeOps NodeOps,
) (ctrl.Result, error) {
	bmh, err := getBMHForNode(ctx, noncachedClient, node)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err)
	}

	// Check if the update is complete by examining the BMH operational status.
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
		logger.InfoContext(ctx, "BMH update complete", slog.String("BMH", bmh.Name))

		// Validate node configuration (firmware versions and BIOS settings)
		configValid, err := validateNodeConfiguration(ctx, c, noncachedClient, logger, bmh, pluginNamespace, node.Spec.HwProfile)
		if err != nil {
			return hwmgrutils.RequeueWithMediumInterval(), err
		}
		if !configValid {
			return hwmgrutils.RequeueWithMediumInterval(), nil
		}

		if _, hasAnnotation := bmh.Annotations[BmhErrorTimestampAnnotation]; hasAnnotation {
			if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
				logger.WarnContext(ctx, "failed to clean up transient error annotation", slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
				return ctrl.Result{}, err
			}
		}

		// Check if the K8s node is Ready on the managed cluster before marking update as complete.
		// This ensures the node has successfully rejoined the cluster after the hardware update.
		hostname := node.Status.Hostname
		ready, err := nodeOps.IsNodeReady(ctx, hostname)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to check node readiness",
				slog.String("allocatedNode", node.Name), slog.String("error", err.Error()))
			return hwmgrutils.RequeueWithMediumInterval(), nil
		}
		if !ready {
			// Update condition message to indicate waiting for node readiness
			if err := hwmgrutils.SetNodeConfigUpdateRequested(ctx, c, noncachedClient, logger, node,
				node.Spec.HwProfile, string(hwmgmtv1alpha1.NodeWaitingReady)); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update node condition for waiting ready: %w", err)
			}
			return hwmgrutils.RequeueWithMediumInterval(), nil
		}
		logger.InfoContext(ctx, "Node is ready on managed cluster", slog.String("allocatedNode", node.Name))

		if err := nodeOps.UncordonNode(ctx, node.Status.Hostname); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to uncordon node (%s) after HW update: %w",
				node.Status.Hostname, err)
		}

		if err := hwmgrutils.SetNodeConfigApplied(ctx, c, noncachedClient, logger, node, node.Spec.HwProfile); err != nil {
			logger.ErrorContext(ctx, "Failed to set node config applied",
				slog.String("node", node.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, fmt.Errorf("failed to mark node config applied %s: %w", node.Name, err)
		}

		if err := clearConfigAnnotationWithPatch(ctx, c, node); err != nil {
			logger.ErrorContext(ctx, "Failed to clear config annotation",
				slog.String("node", node.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, err
		}

		// Node completed successfully - requeue immediately so the controller re-evaluates and picks the next node to process.
		return hwmgrutils.RequeueImmediately(), nil
	}

	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusError {
		tolerate, err := tolerateAndAnnotateTransientBMHError(ctx, c, logger, bmh)
		if err != nil || tolerate {
			return hwmgrutils.RequeueWithMediumInterval(), err
		}

		logger.InfoContext(ctx, "BMH update failed", slog.String("BMH", bmh.Name))

		if err := nodeOps.UncordonNode(ctx, node.Status.Hostname); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to uncordon node (%s) after BMH error: %w",
				node.Status.Hostname, err)
		}

		// Clean up the config-in-progress annotation
		if err := clearConfigAnnotationWithPatch(ctx, c, node); err != nil {
			return ctrl.Result{}, err
		}

		// Clear BMH update annotations to ensure clean state for retry
		if err := clearBMHUpdateAnnotations(ctx, c, logger, bmh); err != nil {
			logger.WarnContext(ctx, "Failed to clear BMH update annotations after error",
				slog.String("BMH", bmh.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, fmt.Errorf("failed to clear BMH update annotations %s:%w", bmh.Name, err)
		}

		// Clear BMH error annotation to allow future retry attempts
		if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
			logger.WarnContext(ctx, "failed to clear BMH error annotation for future retries",
				slog.String("BMH", bmh.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, fmt.Errorf("failed to clear BMH error annotation %s:%w", bmh.Name, err)
		}

		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient, node,
			string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
			string(hwmgmtv1alpha1.Failed), BmhServicingErr); err != nil {
			logger.ErrorContext(ctx, "failed to update AllocatedNode status", slog.String("node", node.Name), slog.String("error", err.Error()))
			return ctrl.Result{}, fmt.Errorf("failed to update AllocatedNode status %s:%w", node.Name, err)
		}

		// Successfully handled BMH error state: updated node status and cleared annotations
		logger.InfoContext(ctx, "Successfully handled BMH error state", slog.String("BMH", bmh.Name))
		return ctrl.Result{}, fmt.Errorf("bmh %s/%s is in error state, node status updated to Failed", bmh.Namespace, bmh.Name)
	}

	// Update condition message to indicate waiting for BMH completion
	if err := hwmgrutils.SetNodeConfigUpdateRequested(ctx, c, noncachedClient, logger, node,
		node.Spec.HwProfile, string(hwmgmtv1alpha1.NodeWaitingBMHComplete)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update node condition for waiting BMH: %w", err)
	}
	logger.InfoContext(ctx, "BMH config in progress", slog.String("bmh", bmh.Name))
	return hwmgrutils.RequeueWithMediumInterval(), nil
}

// abandonNodeUpdate safely stops an in-progress hardware update for a node whose desired
// profile changed mid-flight. If the BMH is actively servicing or preparing, the update
// cannot be interrupted and a requeue is returned so it can exit the state on its own.
// Otherwise the node is uncordoned, stale annotations are cleared, and the update-abandoned
// annotation is set so that the next reconcile can re-process the node with the new profile.
func abandonNodeUpdate(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	newHwProfile string,
	node *pluginsv1alpha1.AllocatedNode,
	nodeOps NodeOps,
) (ctrl.Result, error) {
	bmh, err := getBMHForNode(ctx, noncachedClient, node)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get BMH for abandoned node %s: %w", node.Name, err)
	}

	// Don't interrupt if BMH is actively servicing or preparing — the hardware
	// operation is underway and must exit the state on its own (complete, fail,
	// or time out at the NAR level) before the node can be safely re-processed.
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusServicing ||
		bmh.Status.Provisioning.State == metal3v1alpha1.StatePreparing {
		logger.InfoContext(ctx, "Desired hardware profile changed but metal3 is actively processing the current update, cannot abandon yet, waiting for completion",
			slog.String("node", node.Name),
			slog.String("currentProfile", node.Spec.HwProfile),
			slog.String("desiredProfile", newHwProfile),
			slog.String("bmh", bmh.Name),
			slog.String("bmhOperationalStatus", string(bmh.Status.OperationalStatus)),
			slog.String("bmhProvisioningState", string(bmh.Status.Provisioning.State)))
		return hwmgrutils.RequeueWithMediumInterval(), nil
	}

	logger.InfoContext(ctx, "Desired profile changed during in-progress update, abandoning current update",
		slog.String("node", node.Name),
		slog.String("currentProfile", node.Spec.HwProfile),
		slog.String("desiredProfile", newHwProfile))

	if err := nodeOps.UncordonNode(ctx, node.Status.Hostname); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to uncordon abandoned node %s: %w", node.Status.Hostname, err)
	}

	if getConfigAnnotation(node) != "" {
		if err := clearConfigAnnotationWithPatch(ctx, c, node); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clear config annotation on abandoned node %s: %w", node.Name, err)
		}
	}

	if err := clearBMHUpdateAnnotations(ctx, c, logger, bmh); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to clear BMH annotations for abandoned node %s: %w", node.Name, err)
	}

	if err := setUpdateAbandonedAnnotation(ctx, c, node); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set abandoned annotation on node %s: %w", node.Name, err)
	}

	// Return immediately to allow the next reconcile to re-process the node with the new profile.
	return hwmgrutils.RequeueImmediately(), nil
}

// initiateNodeUpdate starts the day2 update process for the given AllocatedNode. It validates
// whether HW changes are needed, cordons and drains the node if so (skipped for SNO), then
// applies the hardware profile changes. If no update is needed, it marks the node as ConfigApplied
// directly without additional actions.
func initiateNodeUpdate(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	node *pluginsv1alpha1.AllocatedNode,
	newHwProfile string,
	nodeOps NodeOps,
) (ctrl.Result, error) {

	// Clear the abandoned annotation if this node was previously abandoned due to a
	// mid-flight profile change. The node is now being re-processed with the new profile.
	if err := clearUpdateAbandonedAnnotation(ctx, c, node); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to clear abandoned annotation on node %s: %w", node.Name, err)
	}

	bmh, err := getBMHForNode(ctx, noncachedClient, node)
	if err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err)
	}
	logger.InfoContext(ctx, "Issuing profile update to AllocatedNode",
		slog.String("hwMgrNodeId", node.Spec.HwMgrNodeId),
		slog.String("curHwProfile", node.Spec.HwProfile),
		slog.String("newHwProfile", newHwProfile))

	// Step 1: Validate — determine if HW changes are actually needed (no updates to HFS/HFC/HUP).
	validateOnly := true
	updateNeeded, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace,
		bmh, node, newHwProfile, true, validateOnly)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to evaluate HW profile for node (%s): %w", node.Name, err)
	}
	if !updateNeeded {
		logger.InfoContext(ctx, "No HW changes needed, marking node config applied", slog.String("node", node.Name))
		if err := hwmgrutils.SetNodeConfigApplied(ctx, c, noncachedClient, logger, node, newHwProfile); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to mark node config applied: %w", err)
		}
		// Stop here if no update is needed
		return ctrl.Result{}, nil
	}

	// Step 2: Cordon and drain before applying HW changes if drain is not skipped.
	if !nodeOps.SkipDrain() {
		logger.InfoContext(ctx, "Proceeding with node drain", slog.String("node", node.Name), slog.String("hostname", node.Status.Hostname))
		// Update the condition message to indicate draining is in progress.
		if err := hwmgrutils.SetNodeConfigUpdatePending(ctx, c, noncachedClient, logger, node, newHwProfile,
			string(hwmgmtv1alpha1.NodeDraining)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update node condition for drain: %w", err)
		}
		if err := nodeOps.DrainNode(ctx, node.Status.Hostname); err != nil {
			logger.ErrorContext(ctx, "Drain failed, will retry",
				slog.String("node", node.Name),
				slog.String("hostname", node.Status.Hostname),
				slog.String("error", err.Error()))
			return hwmgrutils.RequeueWithMediumInterval(), nil
		}
	}

	// Step 3: Apply HW profile changes (create/update HFS/HFC/HUP, annotate BMH).
	logger.InfoContext(ctx, "Applying HW profile", slog.String("node", node.Name), slog.String("hwProfile", newHwProfile))
	validateOnly = false
	updateRequired, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace,
		bmh, node, newHwProfile, true, validateOnly)
	if err != nil {
		if err := nodeOps.UncordonNode(ctx, node.Status.Hostname); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to uncordon node (%s): %w", node.Status.Hostname, err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to apply hw profile for node (%s): %w", node.Name, err)
	}
	if updateRequired {
		if err := hwmgrutils.SetNodeConfigUpdateRequested(ctx, c, noncachedClient, logger, node, newHwProfile,
			string(hwmgmtv1alpha1.NodeUpdateRequested)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to mark node config update requested: %w", err)
		}
		// Return a medium interval requeue to allow time for the update to progress.
		return hwmgrutils.RequeueWithMediumInterval(), nil
	} else {
		// It's unlikely to reach here because we already checked for update needed at the beginning, but we'll handle it anyway to be safe.
		if err := nodeOps.UncordonNode(ctx, node.Status.Hostname); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to uncordon node (%s): %w", node.Status.Hostname, err)
		}
		if err := hwmgrutils.SetNodeConfigApplied(ctx, c, noncachedClient, logger, node, newHwProfile); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to mark node config applied: %w", err)
		}
	}
	return ctrl.Result{}, nil
}

// nodeAction represents a node to process for day2 update and the type of action to perform.
// The nodeActionType is used to determine the next step in the day2 update process.
// Action types are:
// - actionInitiate: Cordon, drain, apply HW profile, mark update requested
// - actionTransition: Reboot annotation, wait for servicing, mark config-in-progress
// - actionInProgressUpdate: Check ready, uncordon, mark complete
type nodeAction struct {
	node       *pluginsv1alpha1.AllocatedNode
	actionType nodeActionType
}

type nodeActionType int

const (
	actionInitiate nodeActionType = iota
	actionTransition
	actionInProgressUpdate
)

// handleNodeAllocationRequestConfiguring orchestrates day2 hardware profile updates for all nodes
// in a NodeAllocationRequest.
//
// Updates are processed by node group in priority order (masters first, then workers),
// with each group completing before the next begins. When a hardware profile change is
// detected for a node group, all nodes in that group are marked as ConfigUpdatePending
// to ensure a clean starting state.
//
// Within a group, the number of nodes selected for processing is limited by the MCP
// maxUnavailable value, and the selected nodes are processed in parallel.

// This function also returns the nodeList for the caller to determine the NAR status.
// Note that status conditions in the returned nodeList may be stale since nodes are updated
// during processing, but the caller will refetch the latest version of each node.
func handleNodeAllocationRequestConfiguring(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (ctrl.Result, *pluginsv1alpha1.AllocatedNodeList, error) {

	logger.InfoContext(ctx, "Handling NodeAllocationRequest Configuring")

	nodelist, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, nil, fmt.Errorf("failed to get child nodes for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}
	// Deterministic ordering of listed nodes by name across reconciles
	sort.Slice(nodelist.Items, func(i, j int) bool {
		return nodelist.Items[i].Name < nodelist.Items[j].Name
	})

	// Mark all nodes whose spec.HwProfile differs from the target profile as ConfigUpdatePending
	if err := markPendingNodesForUpdate(ctx, c, noncachedClient, logger, nodelist, nodeAllocationRequest); err != nil {
		return ctrl.Result{}, nodelist, fmt.Errorf("failed to mark pending nodes: %w", err)
	}

	// Ensure every node has Status.Hostname populated (reads PR once, patches nodes that are missing it)
	if err := populateNodeHostnames(ctx, c, logger, nodelist, nodeAllocationRequest); err != nil {
		return ctrl.Result{}, nodelist, fmt.Errorf("failed to populate node hostnames: %w", err)
	}

	spokeClient, spokeClientset, err := createSpokeClients(ctx, c, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, nodelist, fmt.Errorf("failed to create spoke clients: %w", err)
	}

	// Skip drain if there is only one node in the NAR
	skipDrain := len(nodelist.Items) == 1
	nodeOps := NewNodeOps(spokeClient, spokeClientset, logger, skipDrain)

	nodesToProcess, err := selectNodesToProcess(ctx, logger, nodeOps, nodelist, nodeAllocationRequest)
	if err != nil {
		return ctrl.Result{}, nodelist, err
	}

	result, err := executeNodeUpdates(ctx, c, noncachedClient, logger, pluginNamespace,
		nodeAllocationRequest, nodeOps, nodesToProcess)
	if err != nil {
		return ctrl.Result{}, nodelist, err
	}

	return result, nodelist, nil
}

// selectNodesToProcess selects nodes that need to be processed for day2 hardware updates.
// It selects nodes from one group at a time in priority order (master first, then workers).
// The next group is not considered until all nodes in the current group have completed.
// Within the active group, it retrieves the MCP maxUnavailable as a rolling concurrency ceiling.
// As long as there capacity remains, pending nodes are selected for processing. If there are
// abandoned nodes (from a mid-flight profile change), they are prioritized over regular pending
// nodes. Nodes already in progress are also included so they can continue processing to completion.
func selectNodesToProcess(
	ctx context.Context,
	logger *slog.Logger,
	nodeOps NodeOps,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
	nar *pluginsv1alpha1.NodeAllocationRequest,
) ([]nodeAction, error) {

	var nodesToProcess []nodeAction

	for _, nodegroup := range getGroupsSortedByRole(nar) {
		currentGroup := nodegroup.NodeGroupData.Name
		newHwProfile := nodegroup.NodeGroupData.HwProfile

		nodesInGroup := filterNodesByGroup(nodelist, currentGroup)
		if len(nodesInGroup) == 0 {
			continue
		}

		nc := classifyNodes(ctx, logger, nodeOps, nodesInGroup, newHwProfile)
		logger.InfoContext(ctx, "Node group classification",
			slog.String("group", currentGroup),
			slog.Int("doneNodes", len(nc.DoneNodes)),
			slog.Int("inProgressNodes", len(nc.InProgressNodes)),
			slog.Int("failedNodes", len(nc.FailedNodes)),
			slog.Int("priorityNodes", len(nc.PriorityNodes)),
			slog.Int("pendingNodes", len(nc.PendingNodes)),
			slog.Int("totalNodes", len(nodesInGroup)))

		if len(nc.DoneNodes) == len(nodesInGroup) {
			logger.InfoContext(ctx, "Group fully updated, moving to next group",
				slog.String("group", currentGroup))
			continue
		}

		// The current group is not fully done, so we need to process the nodes in the group.
		maxUnavailable, err := nodeOps.GetMaxUnavailable(ctx, currentGroup, len(nodesInGroup))
		if err != nil {
			return nil, fmt.Errorf("failed to get maxUnavailable for group %s: %w", currentGroup, err)
		}

		// FailedNodes should not occur in practice since any node failure puts the NAR into
		// a terminal Failed state, stopping further reconciliation. We still account for it
		// defensively in the unavailable count.
		unavailable := len(nc.InProgressNodes) + len(nc.FailedNodes)
		capacity := max(maxUnavailable-unavailable, 0)
		logger.InfoContext(ctx, "Rolling ceiling",
			slog.String("group", currentGroup),
			slog.Int("maxUnavailable", maxUnavailable),
			slog.Int("unavailable", unavailable),
			slog.Int("capacity", capacity))

		// Select candidates for initiation in priority order:
		// 1. Not-ready nodes — already degraded, so updating them first avoids draining
		//    healthy nodes while a down node sits idle.
		// 2. Abandoned nodes — previous update was interrupted by a mid-flight profile
		//    change, HFS/HFC may carry stale settings that may want to be updated first.
		// 3. Pending nodes — regular candidates ready for a fresh update.
		var candidates = []*pluginsv1alpha1.AllocatedNode{}
		candidates = append(candidates, nc.PriorityNodes...)
		candidates = append(candidates, nc.PendingNodes...)
		for i := range candidates {
			if capacity <= 0 {
				// If we've reached the maxUnavailable capacity, break out of the loop.
				break
			}
			// Process the node for update initiation.
			nodesToProcess = append(nodesToProcess, nodeAction{
				node:       candidates[i],
				actionType: actionInitiate,
			})
			// Decrement the capacity for the next node.
			capacity--
		}

		for i := range nc.InProgressNodes {
			node := nc.InProgressNodes[i]
			if getConfigAnnotation(node) != "" {
				// If the node is in the config-in-progress state, process it for completion.
				nodesToProcess = append(nodesToProcess, nodeAction{
					node:       node,
					actionType: actionInProgressUpdate,
				})
			} else {
				// If the node has config update requested, but isn't in config-in-progress yet,
				// process it for transition to the config-in-progress state.
				nodesToProcess = append(nodesToProcess, nodeAction{
					node:       node,
					actionType: actionTransition,
				})
			}
		}

		// Stop processing the next group as the current group is not fully updated.
		break
	}

	return nodesToProcess, nil
}

// executeNodeUpdates performs the hardware configuration update for
// each selected node in parallel. Each node is dispatched to the handler
// matching its current state (initiate, transition, or in-progress check).
// The function returns a requeue result with the shortest interval requested
// across all nodes.
func executeNodeUpdates(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nar *pluginsv1alpha1.NodeAllocationRequest,
	nodeOps NodeOps,
	nodesToProcess []nodeAction,
) (ctrl.Result, error) {
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		errs       []error
		minRequeue time.Duration
	)

	for _, nodeToProcess := range nodesToProcess {
		wg.Add(1)
		go func(nodeToProcess nodeAction) {
			defer wg.Done()

			var res ctrl.Result
			var err error

			newHwProfile := getNewHwProfileForNode(nar, nodeToProcess.node)

			// For actionTransition and actionInProgressUpdate nodes, detect mid-flight profile changes:
			// if the desired profile no longer matches the node's current spec, safely abandon the stale
			// update so the node can be re-processed with the new profile.
			switch nodeToProcess.actionType {
			case actionInitiate:
				res, err = initiateNodeUpdate(ctx, c, noncachedClient, logger,
					pluginNamespace, nodeToProcess.node, newHwProfile, nodeOps)
			case actionTransition:
				if nodeToProcess.node.Spec.HwProfile != newHwProfile {
					res, err = abandonNodeUpdate(ctx, c, noncachedClient, logger,
						newHwProfile, nodeToProcess.node, nodeOps)
				} else {
					res, err = handleTransitionNode(ctx, c, noncachedClient, logger,
						pluginNamespace, nodeToProcess.node, true, nodeOps)
				}
			case actionInProgressUpdate:
				if nodeToProcess.node.Spec.HwProfile != newHwProfile {
					res, err = abandonNodeUpdate(ctx, c, noncachedClient, logger,
						newHwProfile, nodeToProcess.node, nodeOps)
				} else {
					res, err = handleNodeInProgressUpdate(ctx, c, noncachedClient, logger,
						pluginNamespace, nodeToProcess.node, nodeOps)
				}
			}

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				logger.ErrorContext(ctx, "Node processing error",
					slog.String("node", nodeToProcess.node.Name),
					slog.String("error", err.Error()))
				errs = append(errs, fmt.Errorf("node %s, error: %w", nodeToProcess.node.Name, err))
			}

			// Get the shortest requeue interval to requeue with.
			if res.RequeueAfter > 0 || res.Requeue {
				interval := res.RequeueAfter
				if interval == 0 {
					interval = time.Second
				}
				if minRequeue == 0 || interval < minRequeue {
					minRequeue = interval
				}
			}
		}(nodeToProcess)
	}

	wg.Wait()

	if len(errs) > 0 {
		aggErr := fmt.Errorf("failed to process nodes: %w", errors.Join(errs...))
		return ctrl.Result{}, aggErr
	}
	if minRequeue > 0 {
		return ctrl.Result{RequeueAfter: minRequeue}, nil
	}
	return ctrl.Result{}, nil
}

// markPendingNodesForUpdate marks each node whose spec.HwProfile does not match the
// target hardwareprofile to ConfigUpdatePending, indicating that the node is waiting
// to be processed.
func markPendingNodesForUpdate(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
	nar *pluginsv1alpha1.NodeAllocationRequest,
) error {
	for _, nodegroup := range nar.Spec.NodeGroup {
		newHwProfile := nodegroup.NodeGroupData.HwProfile
		for i := range nodelist.Items {
			node := &nodelist.Items[i]
			if node.Spec.GroupName != nodegroup.NodeGroupData.Name {
				continue
			}
			if node.Spec.HwProfile == newHwProfile {
				continue
			}

			// Skip nodes actively in ConfigUpdate (being processed or ready for processing by metal3) —
			// they will either complete the current update or be abandoned by executeNodeUpdates if
			// the profile changed mid-flight. If they have already been safely abandoned, reset them to
			// the new profile and ConfigUpdatePending so they can be re-initiated.
			// We don't clear the abandoned annotation here because classifyNodes needs to identify
			// abandoned nodes based on the annotation, and initiateNodeUpdate will clear it.
			cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
			if cond != nil &&
				cond.Status == metav1.ConditionFalse &&
				cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate) &&
				!hasUpdateAbandonedAnnotation(node) {
				continue
			}

			// Set condition to ConfigUpdatePending
			if err := hwmgrutils.SetNodeConfigUpdatePending(ctx, c, noncachedClient, logger, node, newHwProfile,
				string(hwmgmtv1alpha1.NodeUpdatePending)); err != nil {
				return fmt.Errorf("failed to set ConfigUpdatePending on node %s: %w", node.Name, err)
			}
		}
	}
	return nil
}

// nodeClassification holds the result of classifyNodes — each bucket represents a
// distinct lifecycle state for nodes within a single group.
type nodeClassification struct {
	// DoneNodes: ConfigApplied and spec.hwProfile == newHwProfile
	DoneNodes []*pluginsv1alpha1.AllocatedNode
	// InProgressNodes: Configured=False with reason ConfigUpdate (without abandoned annotation)
	InProgressNodes []*pluginsv1alpha1.AllocatedNode
	// FailedNodes: Configured=False with reason Failed or InvalidInput
	FailedNodes []*pluginsv1alpha1.AllocatedNode
	// PriorityNodes: nodes that should be processed before regular pending nodes.
	// This includes nodes whose k8s node is not Ready (previous update failed)
	// and nodes with the UpdateAbandonedAnnotation (previous update was abandoned
	// due to a mid-flight profile change).
	PriorityNodes []*pluginsv1alpha1.AllocatedNode
	// PendingNodes: ready nodes without special conditions that need to be evaluated/updated
	PendingNodes []*pluginsv1alpha1.AllocatedNode
}

func classifyNodes(
	ctx context.Context, logger *slog.Logger, nodeOps NodeOps,
	nodes []*pluginsv1alpha1.AllocatedNode, newHwProfile string,
) nodeClassification {
	var nc nodeClassification
	var notReadyNodes, abandonedNodes []*pluginsv1alpha1.AllocatedNode
	for _, node := range nodes {
		cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))

		if cond != nil &&
			cond.Status == metav1.ConditionTrue &&
			cond.Reason == string(hwmgmtv1alpha1.ConfigApplied) &&
			node.Spec.HwProfile == newHwProfile {
			nc.DoneNodes = append(nc.DoneNodes, node)
			continue
		}

		if cond != nil &&
			cond.Status == metav1.ConditionFalse &&
			cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate) &&
			!hasUpdateAbandonedAnnotation(node) {
			nc.InProgressNodes = append(nc.InProgressNodes, node)
			continue
		}

		if cond != nil &&
			cond.Status == metav1.ConditionFalse &&
			(cond.Reason == string(hwmgmtv1alpha1.Failed) ||
				cond.Reason == string(hwmgmtv1alpha1.InvalidInput)) {
			nc.FailedNodes = append(nc.FailedNodes, node)
			continue
		}

		// Remaining nodes are pending or abandoned. Collect not-ready and abandoned
		// nodes separately so PriorityNodes is ordered: not-ready first (already
		// degraded, should recover promptly), then abandoned ready nodes (stale HFS/HFC).
		ready, err := nodeOps.IsNodeReady(ctx, node.Status.Hostname)
		if err != nil {
			logger.WarnContext(ctx, "Failed to check node readiness, assuming not ready",
				slog.String("node", node.Name), slog.String("error", err.Error()))
			ready = false
		}
		if !ready {
			notReadyNodes = append(notReadyNodes, node)
			continue
		}
		if hasUpdateAbandonedAnnotation(node) {
			abandonedNodes = append(abandonedNodes, node)
			continue
		}

		nc.PendingNodes = append(nc.PendingNodes, node)
	}
	nc.PriorityNodes = append(nc.PriorityNodes, notReadyNodes...)
	nc.PriorityNodes = append(nc.PriorityNodes, abandonedNodes...)
	return nc
}

// getNewHwProfileForNode finds the target HwProfile for a node based on its group.
func getNewHwProfileForNode(nar *pluginsv1alpha1.NodeAllocationRequest, node *pluginsv1alpha1.AllocatedNode) string {
	for _, group := range nar.Spec.NodeGroup {
		if group.NodeGroupData.Name == node.Spec.GroupName {
			return group.NodeGroupData.HwProfile
		}
	}
	return node.Spec.HwProfile
}

// filterNodesByGroup returns AllocatedNodes belonging to the specified group.
func filterNodesByGroup(nodelist *pluginsv1alpha1.AllocatedNodeList, groupName string) []*pluginsv1alpha1.AllocatedNode {
	var result []*pluginsv1alpha1.AllocatedNode
	for i := range nodelist.Items {
		if nodelist.Items[i].Spec.GroupName == groupName {
			result = append(result, &nodelist.Items[i])
		}
	}
	return result
}

// extractPRNameFromCallback extracts the ProvisioningRequest name from the callback URL.
// The callback URL follows the pattern: /nar-callback/v1/provisioning-requests/{provisioningRequestName}
//
// Note: The callback URL is automatically populated by the provisioning controller when creating
// the NAR, so format errors indicate a bug in the provisioning controller that should be fixed there
// or user corruption should be fixed by the user.
func extractPRNameFromCallback(callback *pluginsv1alpha1.Callback) (string, error) {
	if callback == nil || callback.CallbackURL == "" {
		return "", fmt.Errorf("no callback configured")
	}

	callbackURL, err := url.Parse(callback.CallbackURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse callback URL: %w", err)
	}

	if !strings.HasPrefix(callbackURL.Path, constants.NarCallbackServicePath+"/") {
		return "", fmt.Errorf("callback URL does not match expected pattern: %s", callback.CallbackURL)
	}

	prName := strings.TrimPrefix(callbackURL.Path, constants.NarCallbackServicePath+"/")
	if prName == "" {
		return "", fmt.Errorf("could not extract provisioning request name from callback URL: %s", callback.CallbackURL)
	}
	return prName, nil
}

// populateNodeHostnames ensures each given AllocatedNode has Status.Hostname set.
// It reads the ProvisioningRequest's status.extensions.allocatedNodeHostMap once
// and patches any node with hostname that is not yet persisted. In-memory nodelist
// items are also updated so downstream code can read node.Status.Hostname directly.
func populateNodeHostnames(
	ctx context.Context,
	hubClient client.Client,
	logger *slog.Logger,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
	nar *pluginsv1alpha1.NodeAllocationRequest,
) error {
	// Quick check: skip if all nodes already have hostnames
	var needsPatch []int
	for i := range nodelist.Items {
		if nodelist.Items[i].Status.Hostname == "" {
			needsPatch = append(needsPatch, i)
		}
	}
	if len(needsPatch) == 0 {
		return nil
	}

	prName, err := extractPRNameFromCallback(nar.Spec.Callback)
	if err != nil {
		return fmt.Errorf("failed to extract provisioning request name: %w", err)
	}

	pr := &provisioningv1alpha1.ProvisioningRequest{}
	if err := hubClient.Get(ctx, client.ObjectKey{Name: prName}, pr); err != nil {
		return fmt.Errorf("failed to get ProvisioningRequest %s: %w", prName, err)
	}

	hostMap := pr.Status.Extensions.AllocatedNodeHostMap
	for _, idx := range needsPatch {
		node := &nodelist.Items[idx]
		hostname, ok := hostMap[node.Name]
		if !ok || hostname == "" {
			return fmt.Errorf("hostname not found for AllocatedNode %s in ProvisioningRequest %s", node.Name, prName)
		}

		patch := client.MergeFrom(node.DeepCopy())
		node.Status.Hostname = hostname
		if err := hubClient.Status().Patch(ctx, node, patch); err != nil {
			return fmt.Errorf("failed to persist hostname for AllocatedNode %s: %w", node.Name, err)
		}
		logger.InfoContext(ctx, "Populated hostname for AllocatedNode",
			slog.String("node", node.Name), slog.String("hostname", hostname))
	}

	return nil
}

func setAwaitConfigCondition(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (ctrl.Result, error) {
	err := hwmgrutils.UpdateNodeAllocationRequestStatusCondition(
		ctx, c,
		nodeAllocationRequest,
		hwmgmtv1alpha1.Configured,
		hwmgmtv1alpha1.ConfigUpdate,
		metav1.ConditionFalse,
		string(hwmgmtv1alpha1.AwaitConfig),
	)
	if err != nil {
		return hwmgrutils.RequeueWithMediumInterval(), fmt.Errorf(
			"failed to update status for NodeAllocationRequest %s: %w",
			nodeAllocationRequest.Name,
			err,
		)
	}
	return ctrl.Result{}, nil
}

// releaseNodeAllocationRequest frees resources allocated to a NodeAllocationRequest
func releaseNodeAllocationRequest(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (bool, error) {

	clusterID := nodeAllocationRequest.Spec.ClusterId

	logger.InfoContext(ctx, "Processing releaseNodeAllocationRequest request:",
		slog.String("clusterID", clusterID),
	)

	// remove the allocated label from BMHs and finalizer from the corresponding PreprovisioningImage resources
	nodelist, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return false, fmt.Errorf("failed to get child nodes for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}
	if len(nodelist.Items) == 0 {
		logger.InfoContext(ctx, "All nodes have been deleted")
		return true, nil
	}

	return false, nil
}

func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// allocateBMHToNodeAllocationRequest assigns a BareMetalHost to a NodeAllocationRequest.
func allocateBMHToNodeAllocationRequest(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	bmh *metal3v1alpha1.BareMetalHost,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	group pluginsv1alpha1.NodeGroup,
) error {

	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}

	nodeName := hwmgrutils.GenerateNodeName(hwmgrutils.Metal3HardwarePluginID, nodeAllocationRequest.Spec.ClusterId, bmh.Namespace, bmh.Name)

	// Set AllocatedNode label
	allocatedNodeLbl := bmh.Labels[ctlrutils.AllocatedNodeLabel]
	if allocatedNodeLbl != nodeName {
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeLabel, ctlrutils.AllocatedNodeLabel,
			nodeName, OpAdd); err != nil {
			return fmt.Errorf("failed to save AllocatedNode name label to BMH (%s): %w", bmh.Name, err)
		}
	}

	nodeId := bmh.Name
	nodeNs := bmh.Namespace

	// Ensure node is created
	node, err := createNode(ctx, c, logger, pluginNamespace, nodeAllocationRequest, nodeName, nodeId, nodeNs, group.NodeGroupData.Name, group.NodeGroupData.HwProfile)
	if err != nil {
		return fmt.Errorf("failed to create allocated node (%s): %w", nodeName, err)
	}

	// Process HW profile
	updating, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace, bmh, node, group.NodeGroupData.HwProfile, false, false)
	if err != nil {
		return fmt.Errorf("failed to process hw profile for node (%s): %w", nodeName, err)
	}
	logger.InfoContext(ctx, "processed hw profile", slog.Bool("updating", updating))

	// Mark BMH allocated
	if err := markBMHAllocated(ctx, c, logger, bmh); err != nil {
		return fmt.Errorf("failed to add allocated label to BMH (%s): %w", bmh.Name, err)
	}

	// Allow Host Management
	if err := allowHostManagement(ctx, c, logger, bmh); err != nil {
		return fmt.Errorf("failed to add host management annotation to BMH (%s): %w", bmh.Name, err)
	}

	// Set bootMACAddress from interface labels if not already set
	// This enables the pre-provisioned hardware workflow where boot interface
	// is identified via labels instead of requiring bootMACAddress in the spec
	if err := setBootMACAddressFromLabel(ctx, c, logger, bmh); err != nil {
		return fmt.Errorf("failed to set bootMACAddress from interface label for BMH (%s): %w", bmh.Name, err)
	}

	// Update node status
	bmhInterface, err := buildInterfacesFromBMH(bmh)
	if err != nil {
		return fmt.Errorf("failed to build interfaces from BareMetalHost '%s': %w", bmh.Name, err)
	}
	nodeInfo := bmhNodeInfo{
		ResourcePoolID: group.NodeGroupData.ResourcePoolId,
		BMC: &bmhBmcInfo{
			Address:         bmh.Spec.BMC.Address,
			CredentialsName: bmh.Spec.BMC.CredentialsName,
		},
		Interfaces: bmhInterface,
	}
	if err := updateNodeStatus(ctx, c, noncachedClient, logger, pluginNamespace, nodeInfo, nodeName, group.NodeGroupData.HwProfile, updating); err != nil {
		return fmt.Errorf("failed to update node status (%s): %w", nodeName, err)
	}

	// Update NodeAllocationRequest status
	if !contains(nodeAllocationRequest.Status.Properties.NodeNames, nodeName) {
		nodeAllocationRequest.Status.Properties.NodeNames = append(nodeAllocationRequest.Status.Properties.NodeNames, nodeName)

		if err := hwmgrutils.UpdateNodeAllocationRequestProperties(ctx, c, nodeAllocationRequest); err != nil {
			return fmt.Errorf("failed to update NodeAllocationRequest properties for node %s: %w", nodeName, err)
		}
		logger.InfoContext(ctx, "Updated NodeAllocationRequest with allocated node",
			slog.String("nodeName", nodeName),
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))
	}

	return nil
}

// processNodeAllocationRequestAllocation allocates BareMetalHosts to a NodeAllocationRequest while ensuring all
// BMHs are in the same namespace.
func processNodeAllocationRequestAllocation(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (ctrl.Result, error) {

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		aggErr error
	)

	// For each NodeGroup, allocate pending nodes
	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue
		}

		// Calculate how many nodes are still needed for this group
		pending := nodeGroup.Size - countNodesInGroup(
			ctx, noncachedClient, logger, pluginNamespace,
			nodeAllocationRequest.Status.Properties.NodeNames, nodeGroup.NodeGroupData.Name,
		)
		if pending <= 0 {
			continue
		}

		// Only fetch unallocated BMHs if we actually need new nodes using non-cached client
		// to avoid race conditions with concurrent allocations
		unallocatedBMHs, err := fetchBMHList(ctx, noncachedClient, logger, nodeAllocationRequest.Spec.Site,
			nodeGroup.NodeGroupData)
		if err != nil {
			return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("unable to fetch unallocated BMHs for site=%s, nodegroup=%s: %w",
				nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData.Name, err)
		}
		if len(unallocatedBMHs.Items) == 0 {
			// No capacity available; surface error to upper layer
			return ctrl.Result{}, fmt.Errorf("no available nodes for site=%s, nodegroup=%s",
				nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData.Name)
		}

		// Allocate up to 'pending' nodes concurrently
		need := pending
		for i := range unallocatedBMHs.Items {
			mu.Lock()
			if need <= 0 {
				mu.Unlock()
				break
			}
			need--
			mu.Unlock()

			bmh := &unallocatedBMHs.Items[i] // address of slice element (avoid range-var bug)

			wg.Add(1)
			go func(bmh *metal3v1alpha1.BareMetalHost) {
				defer wg.Done()

				err := allocateBMHToNodeAllocationRequest(
					ctx, c, noncachedClient, logger, pluginNamespace,
					bmh, nodeAllocationRequest, nodeGroup,
				)

				if err != nil {
					mu.Lock()
					// Record the first error
					if aggErr == nil {
						aggErr = err
					}
					mu.Unlock()
				}
			}(bmh)
		}
	}

	wg.Wait()

	if aggErr != nil {
		return ctrl.Result{}, aggErr
	}

	// Update NAR properties after all successful allocations
	if err := hwmgrutils.UpdateNodeAllocationRequestProperties(ctx, c, nodeAllocationRequest); err != nil {
		return ctrl.Result{}, fmt.Errorf("update NodeAllocationRequest %s properties: %w",
			nodeAllocationRequest.Name, err)
	}

	return ctrl.Result{}, nil
}

func isNodeProvisioningInProgress(allocatednode *pluginsv1alpha1.AllocatedNode) bool {
	condition := meta.FindStatusCondition(allocatednode.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
	return condition != nil &&
		condition.Status == metav1.ConditionFalse &&
		condition.Reason == string(hwmgmtv1alpha1.InProgress)
}

// validateFirmwareVersions checks whether HostFirmwareComponents on the BMH
// match the firmware versions specified in the HardwareProfile.
// Returns (valid=true) if matches or no versions are specified;
// returns (valid=false, nil) for mismatches or missing required components;
// returns (false, err) on API errors.
func validateFirmwareVersions(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
	pluginNamespace string,
	hwProfileName string,
) (bool, error) {

	// 1) Fetch HardwareProfile
	prof := &hwmgmtv1alpha1.HardwareProfile{}
	if err := c.Get(ctx, types.NamespacedName{Name: hwProfileName, Namespace: pluginNamespace}, prof); err != nil {
		return false, fmt.Errorf("get HardwareProfile %s/%s: %w", pluginNamespace, hwProfileName, err)
	}

	// 2) Build expected versions map (normalized)
	expected := map[string]string{}
	if v := strings.TrimSpace(prof.Spec.BiosFirmware.Version); v != "" {
		expected["bios"] = normalizeVersion(v)
	}
	if v := strings.TrimSpace(prof.Spec.BmcFirmware.Version); v != "" {
		expected["bmc"] = normalizeVersion(v)
	}

	if len(expected) == 0 {
		// No versions specified => nothing to validate
		logger.DebugContext(ctx, "No firmware versions specified in hardware profile; treating as valid")
		return true, nil
	}

	// 3) Get HostFirmwareComponents
	hfc, err := getHostFirmwareComponents(ctx, noncachedClient, bmh.Name, bmh.Namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Profile expects versions but HFC absent -> not valid yet
			logger.InfoContext(ctx, "HostFirmwareComponents not found while versions are specified; not valid yet",
				slog.String("bmh", bmh.Name))
			return false, nil
		}
		return false, fmt.Errorf("get HostFirmwareComponents %s/%s: %w", bmh.Namespace, bmh.Name, err)
	}

	// 4) Index actual components by normalized name
	actual := map[string]string{}
	for _, comp := range hfc.Status.Components {
		k := strings.ToLower(strings.TrimSpace(comp.Component))
		actual[k] = normalizeVersion(comp.CurrentVersion)
	}

	// 5) Check presence & equality for each expected component
	for k, want := range expected {
		have, ok := actual[k]
		if !ok {
			logger.InfoContext(ctx, "Firmware component missing in HFC",
				slog.String("component", k),
				slog.String("bmh", bmh.Name))
			return false, nil
		}
		if have != want {
			logger.InfoContext(ctx, "Firmware version mismatch",
				slog.String("component", k),
				slog.String("current", have),
				slog.String("expected", want),
				slog.String("bmh", bmh.Name))
			return false, nil
		}
		logger.DebugContext(ctx, "Firmware version matches",
			slog.String("component", k),
			slog.String("version", have),
			slog.String("bmh", bmh.Name))
	}

	logger.InfoContext(ctx, "All required firmware versions match", slog.String("bmh", bmh.Name))
	return true, nil
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	// strip optional leading "v"
	if strings.HasPrefix(v, "v") && len(v) > 1 && (v[1] >= '0' && v[1] <= '9') {
		return v[1:]
	}
	return v
}

// validateAppliedBiosSettings checks whether HostFirmwareSettings status reflects
// the BIOS settings specified in the HardwareProfile.
// Returns (valid=true) if matches or no BIOS settings are specified;
// returns (valid=false, nil) for mismatches or missing required settings;
// returns (false, err) on API errors.
func validateAppliedBiosSettings(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
	pluginNamespace string,
	hwProfileName string,
) (bool, error) {

	// 1) Fetch HardwareProfile
	prof := &hwmgmtv1alpha1.HardwareProfile{}
	if err := c.Get(ctx, types.NamespacedName{Name: hwProfileName, Namespace: pluginNamespace}, prof); err != nil {
		return false, fmt.Errorf("get HardwareProfile %s/%s: %w", pluginNamespace, hwProfileName, err)
	}

	// 2) Check if any BIOS settings are specified
	if len(prof.Spec.Bios.Attributes) == 0 {
		// No BIOS settings specified => nothing to validate
		logger.DebugContext(ctx, "No BIOS settings specified in hardware profile; treating as valid")
		return true, nil
	}

	// 3) Get HostFirmwareSettings
	hfs, err := getHostFirmwareSettings(ctx, noncachedClient, bmh.Name, bmh.Namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Profile expects BIOS settings but HFS absent -> not valid yet
			logger.InfoContext(ctx, "HostFirmwareSettings not found while BIOS settings are specified; not valid yet",
				slog.String("bmh", bmh.Name))
			return false, nil
		}
		return false, fmt.Errorf("get HostFirmwareSettings %s/%s: %w", bmh.Namespace, bmh.Name, err)
	}

	// 4) Compare each expected BIOS setting with actual status
	for key, expectedValue := range prof.Spec.Bios.Attributes {
		actualValue, exists := hfs.Status.Settings[key]
		if !exists {
			logger.InfoContext(ctx, "BIOS setting missing in HFS status",
				slog.String("setting", key),
				slog.String("bmh", bmh.Name))
			return false, nil
		}

		// Compare values (expected is IntOrString, actual is string)
		if !equalIntOrStringWithString(expectedValue, actualValue) {
			logger.InfoContext(ctx, "BIOS setting value mismatch",
				slog.String("setting", key),
				slog.String("current", actualValue),
				slog.String("expected", expectedValue.String()),
				slog.String("bmh", bmh.Name))
			return false, nil
		}

		logger.DebugContext(ctx, "BIOS setting matches",
			slog.String("setting", key),
			slog.String("value", actualValue),
			slog.String("bmh", bmh.Name))
	}

	logger.InfoContext(ctx, "All required BIOS settings match", slog.String("bmh", bmh.Name))

	// 5) Validate NIC firmware if specified
	if len(prof.Spec.NicFirmware) > 0 {
		// Get HostFirmwareComponents to check NIC firmware versions
		hfc, err := getHostFirmwareComponents(ctx, noncachedClient, bmh.Name, bmh.Namespace)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// Profile expects NIC firmware but HFC absent -> not valid yet
				logger.InfoContext(ctx, "HostFirmwareComponents not found while NIC firmware is specified; not valid yet",
					slog.String("bmh", bmh.Name))
				return false, nil
			}
			return false, fmt.Errorf("get HostFirmwareComponents %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}

		// Create a set of current NIC firmware versions from HFC status
		nicVersions := make(map[string]bool)
		for _, component := range hfc.Status.Components {
			if strings.HasPrefix(component.Component, "nic:") && component.CurrentVersion != "" {
				nicVersions[normalizeVersion(component.CurrentVersion)] = true
			}
		}

		// Check each NIC firmware requirement
		for i, nic := range prof.Spec.NicFirmware {
			if nic.Version == "" {
				continue // Skip if no version specified
			}

			normalizedExpected := normalizeVersion(nic.Version)
			if !nicVersions[normalizedExpected] {
				logger.InfoContext(ctx, "NIC firmware version not found in any nic: component",
					slog.Int("nicIndex", i),
					slog.String("expected", nic.Version),
					slog.String("normalizedExpected", normalizedExpected),
					slog.String("bmh", bmh.Name))
				return false, nil
			}

			logger.DebugContext(ctx, "NIC firmware version matches",
				slog.Int("nicIndex", i),
				slog.String("version", nic.Version),
				slog.String("bmh", bmh.Name))
		}

		logger.InfoContext(ctx, "All required NIC firmware versions match", slog.String("bmh", bmh.Name))
	}

	return true, nil
}

// equalIntOrStringWithString compares intstr.IntOrString with string value
func equalIntOrStringWithString(expected intstr.IntOrString, actual string) bool {
	// Compare string representations
	return strings.EqualFold(strings.TrimSpace(expected.String()), strings.TrimSpace(actual))
}

// validateNodeConfiguration validates both firmware versions and BIOS settings
// for a given BMH and hardware profile. Returns appropriate requeue result and error
// if validation fails or is not yet complete.
func validateNodeConfiguration(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
	pluginNamespace string,
	hwProfileName string,
) (bool, error) {
	// Validate firmware versions
	firmwareValid, err := validateFirmwareVersions(ctx, c, noncachedClient, logger, bmh, pluginNamespace, hwProfileName)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to validate firmware versions",
			slog.String("BMH", bmh.Name),
			slog.String("error", err.Error()))
		return false, err
	}
	if !firmwareValid {
		logger.InfoContext(ctx, "Firmware versions not yet updated, continuing to poll",
			slog.String("BMH", bmh.Name))
		return false, nil
	}

	// Validate BIOS settings
	biosValid, err := validateAppliedBiosSettings(ctx, c, noncachedClient, logger, bmh, pluginNamespace, hwProfileName)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to validate BIOS settings",
			slog.String("BMH", bmh.Name),
			slog.String("error", err.Error()))
		return false, err
	}
	if !biosValid {
		logger.InfoContext(ctx, "BIOS settings not yet updated, continuing to poll",
			slog.String("BMH", bmh.Name))
		return false, nil
	}

	logger.InfoContext(ctx, "Firmware versions and BIOS settings validated successfully", slog.String("BMH", bmh.Name))
	return true, nil
}

// clearBMHUpdateAnnotationsForNAR removes BIOS and firmware update annotations from all BMHs
// associated with the provided NodeAllocationRequest. This is intended for cleanup on configuration timeout
// so that subsequent retries can proceed cleanly.
func clearBMHUpdateAnnotationsForNAR(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) error {
	logger.InfoContext(ctx, "Clearing BMH update annotations for NodeAllocationRequest",
		slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))

	nodeList, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get AllocatedNodes for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	var errs []error
	for _, node := range nodeList.Items {
		// Get BMH for this node
		bmh, err := getBMHForNode(ctx, c, &node)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get BMH for annotation clear",
				slog.String("allocatedNode", node.Name), slog.String("error", err.Error()))
			errs = append(errs, fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err))
			continue
		}

		// Clear update annotations from BMH
		if err := clearBMHUpdateAnnotations(ctx, c, logger, bmh); err != nil {
			logger.ErrorContext(ctx, "Failed to clear BMH update annotations",
				slog.String("bmh", bmh.Name), slog.String("error", err.Error()))
			errs = append(errs, fmt.Errorf("failed to clear BMH update annotations for BMH %s: %w", bmh.Name, err))
		} else {
			logger.InfoContext(ctx, "Cleared BMH update annotations",
				slog.String("bmh", bmh.Name))
		}
	}

	if len(errs) > 0 {
		// Use errors.Join to preserve all errors for better debugging
		return fmt.Errorf("failed to clear BMH update annotations for NodeAllocationRequest %s (%d error(s)): %w",
			nodeAllocationRequest.Name, len(errs), errors.Join(errs...))
	}

	return nil
}

// clearConfigAnnotationForAllocatedNodes removes the config-in-progress annotation from all AllocatedNodes
// associated with the provided NodeAllocationRequest. This is intended for cleanup on configuration timeout/failure
// so that subsequent retries can proceed cleanly.
func clearConfigAnnotationForAllocatedNodes(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) error {
	logger.InfoContext(ctx, "Clearing config-in-progress annotations for AllocatedNodes",
		slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))

	nodeList, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get AllocatedNodes for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	for _, node := range nodeList.Items {
		// Fetch latest to avoid conflicts
		updatedNode := &pluginsv1alpha1.AllocatedNode{}
		if err := noncachedClient.Get(ctx, types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, updatedNode); err != nil {
			logger.ErrorContext(ctx, "Failed to fetch AllocatedNode for annotation clear",
				slog.String("allocatedNode", node.Name), slog.String("error", err.Error()))
			continue
		}
		if err := clearConfigAnnotationWithPatch(ctx, c, updatedNode); err != nil {
			logger.ErrorContext(ctx, "Failed to clear config-in-progress annotation",
				slog.String("allocatedNode", updatedNode.Name), slog.String("error", err.Error()))
			// continue to other nodes
		} else {
			logger.InfoContext(ctx, "Cleared config-in-progress annotation",
				slog.String("allocatedNode", updatedNode.Name))
		}
	}

	return nil
}
