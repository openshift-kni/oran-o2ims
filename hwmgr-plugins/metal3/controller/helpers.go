/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
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
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const ConfigAnnotation = "clcm.openshift.io/config-in-progress"

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
	delete(annotations, ConfigAnnotation)
}

// clearConfigAnnotationWithPatch removes the config-in-progress annotation from an AllocatedNode and patches it
func clearConfigAnnotationWithPatch(ctx context.Context, c client.Client, node *pluginsv1alpha1.AllocatedNode) error {
	removeConfigAnnotation(node)
	if err := ctlrutils.CreateK8sCR(ctx, c, node, nil, ctlrutils.PATCH); err != nil {
		return fmt.Errorf("failed to clear config annotation from AllocatedNode %s: %w", node.Name, err)
	}
	return nil
}

// findNodeInProgress scans the nodelist to find the first node in InProgress
func findNodeInProgress(nodelist *pluginsv1alpha1.AllocatedNodeList) *pluginsv1alpha1.AllocatedNode {
	for _, node := range nodelist.Items {
		condition := meta.FindStatusCondition(node.Status.Conditions, (string(hwmgmtv1alpha1.Provisioned)))
		if condition == nil || (condition.Status == metav1.ConditionFalse && condition.Reason == string(hwmgmtv1alpha1.InProgress)) {
			return &node
		}
	}

	return nil
}

func applyPostConfigUpdates(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	bmhName types.NamespacedName, node *pluginsv1alpha1.AllocatedNode) (int, error) {

	if res, err := clearBMHNetworkData(ctx, c, logger, bmhName); err != nil {
		// preserve prior behavior: short retry on error
		return RequeueAfterShortInterval, fmt.Errorf("clear BMH network data %s: %w", bmhName.String(), err)
	} else if code := RequeueCodeFromResult(res); code > DoNotRequeue {
		return code, nil
	}

	// nolint:wrapcheck
	err := retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
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

// findNextNodeToUpdate scans the AllocatedNodeList to find the first node with stale HwProfile
func findNextNodeToUpdate(nodelist *pluginsv1alpha1.AllocatedNodeList, groupname, newHwProfile string) *pluginsv1alpha1.AllocatedNode {
	for _, node := range nodelist.Items {
		if groupname != node.Spec.GroupName {
			continue
		}

		if newHwProfile != node.Spec.HwProfile {
			return &node
		}

		// Profile is already set — but check if it failed due to invalid inputs
		cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		if cond == nil || cond.Reason == string(hwmgmtv1alpha1.InvalidInput) {
			// retry this node
			return &node
		}
	}

	return nil
}

// deriveNodeAllocationRequestStatusFromNodes evaluates all child AllocatedNodes and returns an appropriate
// NodeAllocationRequest Configured condition status and reason.
func deriveNodeAllocationRequestStatusFromNodes(
	ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
) (metav1.ConditionStatus, string, string) {

	for _, node := range nodelist.Items {
		// Fetch the latest version of the AllocatedNode from the API server
		updatedNode, err := hwmgrutils.GetNode(ctx, logger, noncachedClient, node.Namespace, node.Name)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to fetch updated AllocatedNode", slog.String("name", node.Name), slog.String("error", err.Error()))
			// Fail conservatively if we can't confirm the node's status
			return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
				fmt.Sprintf("AllocatedNode %s could not be fetched: %v", node.Name, err)
		}

		cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		if cond == nil {
			return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
				fmt.Sprintf("Node %s missing Configured condition", node.Name)
		}

		// If not successfully applied, return this node’s current condition
		if cond.Reason != string(hwmgmtv1alpha1.ConfigApplied) {
			return cond.Status, cond.Reason, fmt.Sprintf("AllocatedNode %s: %s", node.Name, cond.Message)
		}
	}

	// All AllocatedNodes are successfully configured
	return metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigApplied), string(hwmgmtv1alpha1.ConfigSuccess)
}

// findNodeConfigInProgress scans the AllocatedNodeList to find the first AllocatedNode with config-in-progress
// annotation
func findNodeConfigInProgress(nodelist *pluginsv1alpha1.AllocatedNodeList) *pluginsv1alpha1.AllocatedNode {
	for _, node := range nodelist.Items {
		if getConfigAnnotation(&node) != "" {
			return &node
		}
	}

	return nil
}

// createNode creates an AllocatedNode CR with specified attributes
func createNode(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	nodename, nodeId, nodeNs, groupname, hwprofile string) error {
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
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if AllocatedNode exists: %w", err)
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
		return fmt.Errorf("failed to create AllocatedNode: %w", err)
	}

	logger.InfoContext(ctx, "AllocatedNode created", slog.String("nodename", nodename))
	return nil
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
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
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

// handleInProgressUpdate checks for any node marked as having a configuration update in progress.
// If a AllocatedNode is found and its associated BMH status indicates that the update has completed,
// it updates the node status, clears the annotation, applies the post-change annotation, and
// requeues immediately.
func handleInProgressUpdate(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodelist *pluginsv1alpha1.AllocatedNodeList,
) (ctrl.Result, bool, error) {
	node := findNodeConfigInProgress(nodelist)
	if node == nil {
		logger.InfoContext(ctx, "No AllocatedNode found that is in progress")
		return ctrl.Result{}, false, nil
	}
	logger.InfoContext(ctx, "Node found that is in progress", slog.String("node", node.Name))
	bmh, err := getBMHForNode(ctx, noncachedClient, node)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err)
	}

	// Check if the update is complete by examining the BMH operational status.
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
		logger.InfoContext(ctx, "BMH update complete", slog.String("BMH", bmh.Name))

		// Validate node configuration (firmware versions and BIOS settings)
		configValid, err := validateNodeConfiguration(ctx, c, noncachedClient, logger, bmh, pluginNamespace, node.Spec.HwProfile)
		if err != nil {
			return hwmgrutils.RequeueWithMediumInterval(), true, err
		}
		if !configValid {
			return hwmgrutils.RequeueWithMediumInterval(), true, nil
		}

		if _, hasAnnotation := bmh.Annotations[BmhErrorTimestampAnnotation]; hasAnnotation {
			if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
				logger.WarnContext(ctx, "failed to clean up transient error annotation", slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
				return ctrl.Result{}, true, err
			}
		}

		// Update the node's status to reflect the new hardware profile.
		node.Status.HwProfile = node.Spec.HwProfile
		hwmgrutils.SetStatusCondition(&node.Status.Conditions,
			string(hwmgmtv1alpha1.Configured),
			string(hwmgmtv1alpha1.ConfigApplied),
			metav1.ConditionTrue,
			string(hwmgmtv1alpha1.ConfigSuccess))

		if err := ctlrutils.UpdateK8sCRStatus(ctx, c, node); err != nil {
			logger.ErrorContext(ctx, "Failed to update AllocatedNode status",
				slog.String("node", node.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, true, fmt.Errorf("failed to update status for AllocatedNode %s: %w", node.Name, err)
		}

		if err := clearConfigAnnotationWithPatch(ctx, c, node); err != nil {
			logger.ErrorContext(ctx, "Failed to clear config annotation",
				slog.String("node", node.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, true, err
		}

		return hwmgrutils.RequeueImmediately(), true, nil
	}

	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusError {
		tolerate, err := tolerateAndAnnotateTransientBMHError(ctx, c, logger, bmh)
		if err != nil || tolerate {
			return hwmgrutils.RequeueWithMediumInterval(), true, err
		}

		logger.InfoContext(ctx, "BMH update failed", slog.String("BMH", bmh.Name))

		// Clean up the config-in-progress annotation
		if err := clearConfigAnnotationWithPatch(ctx, c, node); err != nil {
			return ctrl.Result{}, true, err
		}

		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient,
			node.Name, node.Namespace,
			string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
			string(hwmgmtv1alpha1.Failed), BmhServicingErr); err != nil {
			logger.ErrorContext(ctx, "failed to update AllocatedNode status", slog.String("node", node.Name), slog.String("error", err.Error()))
			return ctrl.Result{}, true, fmt.Errorf("failed to update AllocatedNode status %s:%w", node.Name, err)
		}

		// Clear BMH error annotation to allow future retry attempts
		if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
			logger.WarnContext(ctx, "failed to clear BMH error annotation for future retries",
				slog.String("BMH", bmh.Name),
				slog.String("error", err.Error()))
			return ctrl.Result{}, true, fmt.Errorf("failed to clear BMH error annotation %s:%w", bmh.Name, err)
		}

		// Successfully handled BMH error state: updated node status and cleared annotations
		logger.InfoContext(ctx, "Successfully handled BMH error state", slog.String("BMH", bmh.Name))
		return ctrl.Result{}, false, fmt.Errorf("bmh %s/%s is in error state, node status updated to Failed", bmh.Namespace, bmh.Name)
	}

	logger.InfoContext(ctx, "BMH config in progress", slog.String("bmh", bmh.Name))
	return hwmgrutils.RequeueWithMediumInterval(), true, nil
}

// initiateNodeUpdate starts the update process for the given AllocatedNode by processing the new hardware profile,
func initiateNodeUpdate(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	node *pluginsv1alpha1.AllocatedNode,
	newHwProfile string) (ctrl.Result, error) {

	bmh, err := getBMHForNode(ctx, noncachedClient, node)
	if err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err)
	}
	logger.InfoContext(ctx, "Issuing profile update to AllocatedNode",
		slog.String("hwMgrNodeId", node.Spec.HwMgrNodeId),
		slog.String("curHwProfile", node.Spec.HwProfile),
		slog.String("newHwProfile", newHwProfile))

	updateRequired, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace, bmh, node.Name, node.Namespace, newHwProfile, true)
	if err != nil {
		return hwmgrutils.DoNotRequeue(), err
	}
	logger.InfoContext(ctx, "Processed hardware profile", slog.Bool("updatedRequired", updateRequired))

	// Copy the current node object for patching
	patch := client.MergeFrom(node.DeepCopy())

	// Set the new profile in the spec
	node.Spec.HwProfile = newHwProfile

	if err = c.Patch(ctx, node, patch); err != nil {
		return hwmgrutils.RequeueWithShortInterval(), fmt.Errorf("failed to patch AllocatedNode %s in namespace %s: %w", node.Name, node.Namespace, err)
	}

	if updateRequired {
		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient,
			node.Name, node.Namespace,
			string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
			string(hwmgmtv1alpha1.ConfigUpdate), "Update Requested"); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update AllocatedNode status (%s): %w", node.Name, err)
		}
		// Return a medium interval requeue to allow time for the update to progress.
		return hwmgrutils.RequeueWithMediumInterval(), nil
	} else {
		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient,
			node.Name, node.Namespace,
			string(hwmgmtv1alpha1.Configured), metav1.ConditionTrue,
			string(hwmgmtv1alpha1.ConfigApplied), string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
			logger.ErrorContext(ctx, "failed to update AllocatedNode status", slog.String("node", node.Name), slog.String("error", err.Error()))
		}
	}
	return ctrl.Result{}, nil
}

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

	// STEP 1: Process any node that is already in the update-in-progress state.
	logger.InfoContext(ctx, "Checking for nodes in progress", slog.Int("totalNodes", len(nodelist.Items)))

	res, handled, err := handleInProgressUpdate(ctx, c, noncachedClient, logger, pluginNamespace, nodelist)
	if err != nil || handled {
		return res, nodelist, err
	}

	// No nodes are currently in progress, check if all nodes have been updated

	// STEP 2: Look for the next node that requires an update.
	for _, nodegroup := range nodeAllocationRequest.Spec.NodeGroup {
		newHwProfile := nodegroup.NodeGroupData.HwProfile
		node := findNextNodeToUpdate(nodelist, nodegroup.NodeGroupData.Name, newHwProfile)
		if node == nil {
			// No node pending update in this nodegroup; continue to the next one.
			continue
		}

		// Initiate the update process for the selected node.
		res, err := initiateNodeUpdate(ctx, c, noncachedClient, logger, pluginNamespace, node, newHwProfile)
		return res, nodelist, err
	}

	// STEP 3: Handle nodes in transition (from update-needed to update in-progress).
	res, err = handleTransitionNodes(ctx, c, noncachedClient, logger, pluginNamespace, nodelist, true)
	if err != nil {
		return res, nodelist, err
	}
	if res.Requeue || res.RequeueAfter > 0 {
		// handleTransitionNodes found work to do, requeue to let next reconcile handle in-progress nodes
		return res, nodelist, nil
	}

	// No nodes in progress, no nodes needing updates, and no transitions happening
	// All AllocatedNodes have been successfully updated to their target profiles
	logger.InfoContext(ctx, "All AllocatedNodes have been updated to new profile")

	// No work to do, don't requeue
	return ctrl.Result{}, nodelist, nil
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
// Returns a ctrl.Result (for precise requeue timing) and an error for unexpected failures.
// Callers should propagate any non-zero Result (Requeue / RequeueAfter).
func allocateBMHToNodeAllocationRequest(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	bmh *metal3v1alpha1.BareMetalHost,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	group pluginsv1alpha1.NodeGroup,
) (ctrl.Result, error) {

	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}

	nodeName := hwmgrutils.GenerateNodeName(hwmgrutils.Metal3HardwarePluginID, nodeAllocationRequest.Spec.ClusterId, bmh.Namespace, bmh.Name)

	// Set AllocatedNode label
	allocatedNodeLbl := bmh.Labels[ctlrutils.AllocatedNodeLabel]
	if allocatedNodeLbl != nodeName {
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeLabel, ctlrutils.AllocatedNodeLabel,
			nodeName, OpAdd); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to save AllocatedNode name label to BMH (%s): %w", bmh.Name, err)
		}
	}

	nodeId := bmh.Name
	nodeNs := bmh.Namespace

	// Ensure node is created
	if err := createNode(ctx, c, logger, pluginNamespace, nodeAllocationRequest, nodeName, nodeId, nodeNs, group.NodeGroupData.Name, group.NodeGroupData.HwProfile); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create allocated node (%s): %w", nodeName, err)
	}

	// Process HW profile
	nodeNamespace := pluginNamespace
	updating, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace, bmh, nodeName, nodeNamespace, group.NodeGroupData.HwProfile, false)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to process hw profile for node (%s): %w", nodeName, err)
	}
	logger.InfoContext(ctx, "processed hw profile", slog.Bool("updating", updating))

	// Mark BMH allocated
	if err := markBMHAllocated(ctx, c, logger, bmh); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add allocated label to BMH (%s): %w", bmh.Name, err)
	}

	// Allow Host Management
	if err := allowHostManagement(ctx, c, logger, bmh); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add host management annotation to BMH (%s): %w", bmh.Name, err)
	}

	// Set bootMACAddress from interface labels if not already set
	// This enables the pre-provisioned hardware workflow where boot interface
	// is identified via labels instead of requiring bootMACAddress in the spec
	if err := setBootMACAddressFromLabel(ctx, c, logger, nodeAllocationRequest, bmh); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set bootMACAddress from interface label for BMH (%s): %w", bmh.Name, err)
	}

	// Update node status
	bmhInterface, err := buildInterfacesFromBMH(nodeAllocationRequest, bmh)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build interfaces from BareMetalHost '%s': %w", bmh.Name, err)
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
		return ctrl.Result{}, fmt.Errorf("failed to update node status (%s): %w", nodeName, err)
	}

	// Update NodeAllocationRequest status BEFORE network data clearing
	// This ensures the node is tracked even if we need to requeue for network data clearing
	if !contains(nodeAllocationRequest.Status.Properties.NodeNames, nodeName) {
		nodeAllocationRequest.Status.Properties.NodeNames = append(nodeAllocationRequest.Status.Properties.NodeNames, nodeName)

		// Immediately persist the NodeAllocationRequest status to the cluster
		// This prevents loss of NodeNames when requeuing for network data clearing
		if err := hwmgrutils.UpdateNodeAllocationRequestProperties(ctx, c, nodeAllocationRequest); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update NodeAllocationRequest properties for node %s: %w", nodeName, err)
		}
		logger.InfoContext(ctx, "Updated NodeAllocationRequest with allocated node",
			slog.String("nodeName", nodeName),
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))
	}

	if !updating {
		if res, err := clearBMHNetworkData(ctx, c, logger, bmhName); err != nil || res.Requeue || res.RequeueAfter > 0 {
			// transient / propagation wait → bubble up callee’s precise requeue
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// waitForPreprovisioningImageNetworkDataCleared waits for the PreprovisioningImage network status to be cleared
// before proceeding with BMH NetworkData clearing. Returns true if network data is cleared, false if still waiting.
func waitForPreprovisioningImageNetworkDataCleared(ctx context.Context, c client.Client, logger *slog.Logger, bmhName types.NamespacedName) (bool, error) {
	// Get the corresponding PreprovisioningImage (same name/namespace as BMH)
	image := &metal3v1alpha1.PreprovisioningImage{}
	if err := c.Get(ctx, bmhName, image); err != nil {
		if errors.IsNotFound(err) {
			// If PreprovisioningImage doesn't exist, consider network data as cleared
			logger.InfoContext(ctx, "PreprovisioningImage not found, considering network data cleared",
				slog.String("bmh", bmhName.String()))
			return true, nil
		}
		return false, fmt.Errorf("failed to get PreprovisioningImage %s: %w", bmhName.String(), err)
	}

	// Check if network data is cleared (both Name and Version should be empty)
	networkDataCleared := image.Status.NetworkData.Name == "" && image.Status.NetworkData.Version == ""

	if networkDataCleared {
		logger.InfoContext(ctx, "PreprovisioningImage network data is cleared",
			slog.String("bmh", bmhName.String()))
		return true, nil
	}

	logger.InfoContext(ctx, "Waiting for PreprovisioningImage network data to be cleared",
		slog.String("bmh", bmhName.String()),
		slog.String("networkDataName", image.Status.NetworkData.Name),
		slog.String("networkDataVersion", image.Status.NetworkData.Version))
	return false, nil
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
		wg         sync.WaitGroup
		mu         sync.Mutex
		aggErr     error
		minBackoff time.Duration // 0 means "no requeue requested"
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

				res, err := allocateBMHToNodeAllocationRequest(
					ctx, c, noncachedClient, logger, pluginNamespace,
					bmh, nodeAllocationRequest, nodeGroup,
				)

				mu.Lock()
				defer mu.Unlock()

				// Record the first error (or replace with a more informative one)
				if err != nil {
					// Prefer not to wrap multiple times; keep most specific context
					if aggErr == nil {
						aggErr = err
					}
				}

				// Track the shortest requested backoff to stay responsive
				if res.RequeueAfter > 0 || res.Requeue {
					b := res.RequeueAfter
					if b == 0 {
						b = 15 * time.Second
					}
					if minBackoff == 0 || b < minBackoff {
						minBackoff = b
					}
				}
			}(bmh)
		}
	}

	wg.Wait()

	if aggErr != nil {
		if minBackoff > 0 {
			return ctrl.Result{RequeueAfter: minBackoff}, aggErr
		}
		return ctrl.Result{}, aggErr
	}

	if minBackoff > 0 {
		return ctrl.Result{RequeueAfter: minBackoff}, nil
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

// RequeueCodeFromResult maps a ctrl.Result into the int-based requeue codes.
//
// NOTE: This is a compatibility shim. The idiomatic controller-runtime style
// is to return ctrl.Result directly from Reconcile() and helpers.
// Over time, callers should migrate to use ctrl.Result instead of int codes
// so this mapping can be removed.
func RequeueCodeFromResult(res ctrl.Result) int {
	if res.RequeueAfter <= 0 && !res.Requeue {
		return DoNotRequeue
	}
	// Prefer explicit buckets if you have them
	switch res.RequeueAfter {
	case hwmgrutils.RequeueWithShortInterval().RequeueAfter:
		return RequeueAfterShortInterval
	case hwmgrutils.RequeueWithMediumInterval().RequeueAfter:
		return RequeueAfterMediumInterval
	case hwmgrutils.RequeueWithLongInterval().RequeueAfter:
		return RequeueAfterLongInterval
	}
	if res.RequeueAfter > 0 {
		return int(res.RequeueAfter / time.Second) // generic seconds
	}
	// res.Requeue == true without After -> treat as short
	return RequeueAfterShortInterval
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
		if errors.IsNotFound(err) {
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
		if errors.IsNotFound(err) {
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
			if errors.IsNotFound(err) {
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
