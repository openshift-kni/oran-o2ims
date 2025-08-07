/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
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

// findNodeInProgress scans the nodelist to find the first node in InProgress
func findNodeInProgress(nodelist *pluginsv1alpha1.AllocatedNodeList) *pluginsv1alpha1.AllocatedNode {
	for _, node := range nodelist.Items {
		condition := meta.FindStatusCondition(node.Status.Conditions, (string(hwmgmtv1alpha1.Provisioned)))
		if condition != nil {
			if condition.Status == metav1.ConditionFalse && condition.Reason == string(hwmgmtv1alpha1.InProgress) {
				return &node
			}
		}
	}

	return nil
}

func applyPostConfigUpdates(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	bmhName types.NamespacedName, node *pluginsv1alpha1.AllocatedNode) error {

	if err := clearBMHNetworkData(ctx, c, bmhName); err != nil {
		return fmt.Errorf("failed to clearBMHNetworkData bmh (%+v): %w", bmhName, err)
	}
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
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

		reason := hwmgmtv1alpha1.Completed
		message := "Provisioned"
		status := metav1.ConditionTrue
		if updating {
			reason = hwmgmtv1alpha1.InProgress
			message = "Hardware configuration in progess"
			status = metav1.ConditionFalse
		}
		hwmgrutils.SetStatusCondition(&node.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned),
			string(reason),
			status,
			message)

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
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (full bool, requeueAfter int, err error) {

	// Always check allocation/processing status, regardless of current allocation count
	// This handles both initial allocation and post-allocation processing (e.g., network data clearing)
	requeueAfter, err = processNodeAllocationRequestAllocation(ctx, c, noncachedClient, logger, pluginNamespace, nodeAllocationRequest)
	if err != nil {
		return false, DoNotRequeue, err
	}
	if requeueAfter > DoNotRequeue {
		return false, requeueAfter, nil
	}

	// Check if we're fully allocated now that processing is complete
	full = isNodeAllocationRequestFullyAllocated(ctx, noncachedClient, logger, pluginNamespace, nodeAllocationRequest)
	if !full {
		// Still not fully allocated, continue processing
		return false, DoNotRequeue, nil
	}

	// check if there are any pending work such as bios configuring
	if updating, err := checkForPendingUpdate(ctx, c, noncachedClient, logger, pluginNamespace, nodeAllocationRequest); err != nil {
		return false, DoNotRequeue, err
	} else if updating {
		return false, DoNotRequeue, nil
	}
	return true, DoNotRequeue, nil
}

// processNewNodeAllocationRequest processes a new NodeAllocationRequest CR, verifying that there are enough free
// resources to satisfy the request
func processNewNodeAllocationRequest(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	logger.InfoContext(ctx, "Processing processNewNodeAllocationRequest request")

	// Check if enough resources are available for each NodeGroup
	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Fetch unallocated BMHs for the specific site and poolID
		bmhListForGroup, err := fetchBMHList(ctx, c, logger, nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData, UnallocatedBMHs, "")
		if err != nil {
			return fmt.Errorf("unable to fetch BMHs for nodegroup=%s: %w", nodeGroup.NodeGroupData.Name, err)
		}

		// Ensure enough resources exist in the requested pool
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
	nodelist *pluginsv1alpha1.AllocatedNodeList,
) (ctrl.Result, bool, error) {
	node := findNodeConfigInProgress(nodelist)
	if node == nil {
		logger.InfoContext(ctx, "No AllocatedNode found that is in progress")
		return ctrl.Result{}, false, nil
	}
	logger.InfoContext(ctx, "Node found that is in progress", slog.String("node", node.Name))
	bmh, err := getBMHForNode(ctx, c, node)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("failed to get BMH for AllocatedNode %s: %w", node.Name, err)
	}

	// Check if the update is complete by examining the BMH operational status.
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
		logger.InfoContext(ctx, "BMH update complete", slog.String("BMH", bmh.Name))

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
			return ctrl.Result{}, true, fmt.Errorf("failed to update status for AllocatedNode %s: %w", node.Name, err)
		}
		removeConfigAnnotation(node)
		if err := ctlrutils.CreateK8sCR(ctx, c, node, nil, ctlrutils.PATCH); err != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to clear annotation from AllocatedNode %s: %w", node.Name, err)
		}

		return hwmgrutils.RequeueImmediately(), true, nil
	}

	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusError {
		tolerate, err := tolerateAndAnnotateTransientBMHError(ctx, c, logger, bmh)
		if err != nil || tolerate {
			return hwmgrutils.RequeueWithMediumInterval(), true, err
		}
		logger.InfoContext(ctx, "BMH update failed", slog.String("BMH", bmh.Name))
		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient,
			node.Name, node.Namespace,
			string(hwmgmtv1alpha1.Configured), metav1.ConditionFalse,
			string(hwmgmtv1alpha1.Failed), BmhServicingErr); err != nil {
			logger.ErrorContext(ctx, "failed to update AllocatedNode status", slog.String("node", node.Name), slog.String("error", err.Error()))
		}
		return ctrl.Result{}, false, fmt.Errorf("failed to apply changes for BMH %s/%s", bmh.Namespace, bmh.Name)
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

	bmh, err := getBMHForNode(ctx, c, node)
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

	// STEP 1: Look for the next node that requires an update.
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

	// STEP 2: Handle nodes in transition (from update-needed to update in-progress).
	updating, err := handleTransitionNodes(ctx, c, logger, pluginNamespace, nodelist, true)
	if err != nil {
		return ctrl.Result{}, nodelist, fmt.Errorf("error handling transitioning nodes: %w", err)
	}
	if updating {
		// Return a short interval requeue to allow time for the transition
		return hwmgrutils.RequeueWithShortInterval(), nodelist, nil
	}

	// STEP 3: Process any node that is already in the update-in-progress state.
	res, handled, err := handleInProgressUpdate(ctx, c, noncachedClient, logger, nodelist)
	if err != nil {
		if !handled {
			logger.InfoContext(ctx, "Not handled", slog.String("error", err.Error()))
			return hwmgrutils.DoNotRequeue(), nodelist, nil
		}
		return res, nodelist, err
	}
	if handled {
		return res, nodelist, err
	}

	// STEP 4: If no nodes are pending updates, mark the NodeAllocationRequest as fully configured.
	logger.InfoContext(ctx, "All AllocatedNodes have been updated to new profile")

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
// Returns requeueAfter int (seconds) and error. If requeueAfter > DoNotRequeue, caller should requeue after that duration.
func allocateBMHToNodeAllocationRequest(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	bmh *metal3v1alpha1.BareMetalHost,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	group pluginsv1alpha1.NodeGroup,
) (int, error) {

	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	nodeName := bmh.Annotations[NodeNameAnnotation]
	if nodeName == "" {
		nodeName = hwmgrutils.GenerateNodeName()
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation, NodeNameAnnotation,
			nodeName, OpAdd); err != nil {
			return DoNotRequeue, fmt.Errorf("failed to save AllocatedNode name annotation to BMH (%s): %w", bmh.Name, err)
		}
	}

	// Set AllocatedNode label
	allocatedNodeLbl := bmh.Labels[ctlrutils.AllocatedNodeLabel]
	if allocatedNodeLbl != nodeName {
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeLabel, ctlrutils.AllocatedNodeLabel,
			nodeName, OpAdd); err != nil {
			return DoNotRequeue, fmt.Errorf("failed to save AllocatedNode name label to BMH (%s): %w", bmh.Name, err)
		}
	}

	nodeId := bmh.Name
	nodeNs := bmh.Namespace

	// Ensure node is created
	if err := createNode(ctx, c, logger, pluginNamespace, nodeAllocationRequest, nodeName, nodeId, nodeNs, group.NodeGroupData.Name, group.NodeGroupData.HwProfile); err != nil {
		return DoNotRequeue, fmt.Errorf("failed to create allocated node (%s): %w", nodeName, err)
	}

	// Process HW profile
	nodeNamespace := pluginNamespace
	updating, err := processHwProfileWithHandledError(ctx, c, noncachedClient, logger, pluginNamespace, bmh, nodeName, nodeNamespace, group.NodeGroupData.HwProfile, false)
	if err != nil {
		return DoNotRequeue, fmt.Errorf("failed to process hw profile for node (%s): %w", nodeName, err)
	}
	logger.InfoContext(ctx, "processed hw profile", slog.Bool("updating", updating))

	// Mark BMH allocated
	if err := markBMHAllocated(ctx, c, logger, bmh); err != nil {
		return DoNotRequeue, fmt.Errorf("failed to add allocated label to BMH (%s): %w", bmh.Name, err)
	}

	// Allow Host Management
	if err := allowHostManagement(ctx, c, logger, bmh); err != nil {
		return DoNotRequeue, fmt.Errorf("failed to add host management annotation to BMH (%s): %w", bmh.Name, err)
	}

	// Update node status
	bmhInterface, err := buildInterfacesFromBMH(nodeAllocationRequest, bmh)
	if err != nil {
		return DoNotRequeue, fmt.Errorf("failed to build interfaces from BareMetalHost '%s': %w", bmh.Name, err)
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
		return DoNotRequeue, fmt.Errorf("failed to update node status (%s): %w", nodeName, err)
	}

	// Update NodeAllocationRequest status BEFORE network data clearing
	// This ensures the node is tracked even if we need to requeue for network data clearing
	if !contains(nodeAllocationRequest.Status.Properties.NodeNames, nodeName) {
		nodeAllocationRequest.Status.Properties.NodeNames = append(nodeAllocationRequest.Status.Properties.NodeNames, nodeName)

		// Immediately persist the NodeAllocationRequest status to the cluster
		// This prevents loss of NodeNames when requeuing for network data clearing
		if err := hwmgrutils.UpdateNodeAllocationRequestProperties(ctx, c, nodeAllocationRequest); err != nil {
			return DoNotRequeue, fmt.Errorf("failed to update NodeAllocationRequest properties for node %s: %w", nodeName, err)
		}
		logger.InfoContext(ctx, "Updated NodeAllocationRequest with allocated node",
			slog.String("nodeName", nodeName),
			slog.String("nodeAllocationRequest", nodeAllocationRequest.Name))
	}

	if !updating {
		bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}

		// First, clear BMH NetworkData so metal3 controller can propagate to PreprovisioningImage
		if err := clearBMHNetworkData(ctx, c, bmhName); err != nil {
			return DoNotRequeue, fmt.Errorf("failed to clear network data for BMH (%s/%s): %w", bmh.Name, bmh.Namespace, err)
		}

		// Wait for metal3 controller to propagate the change to PreprovisioningImage network status
		networkDataCleared, err := waitForPreprovisioningImageNetworkDataCleared(ctx, c, logger, bmhName)
		if err != nil {
			return DoNotRequeue, fmt.Errorf("failed to check PreprovisioningImage network status for BMH (%s/%s): %w", bmh.Name, bmh.Namespace, err)
		}

		if !networkDataCleared {
			// PreprovisioningImage network data is not yet cleared, return requeue after 15 seconds
			logger.InfoContext(ctx, "Waiting for PreprovisioningImage network data to be cleared, requeueing",
				slog.String("bmh", bmhName.String()))
			return RequeueAfterShortInterval, nil
		}
	}

	// Clean up annotation
	if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, "annotation", NodeNameAnnotation, "", OpRemove); err != nil {
		logger.ErrorContext(ctx, "failed to clear node name annotation from BMH", slog.Any("bmh", bmhName), slog.String("error", err.Error()))
	}

	return DoNotRequeue, nil
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
func processNodeAllocationRequestAllocation(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (int, error) {

	var (
		wg            sync.WaitGroup
		mu            sync.Mutex
		allocationErr error
		requeueAfter  int
	)

	// Get the BMH namespace from an already allocated node in this pool
	bmhNamespace, err := getNodeAllocationRequestBMHNamespace(ctx, c, logger, nodeAllocationRequest)
	if err != nil {
		return DoNotRequeue, fmt.Errorf("unable to determine BMH namespace for pool %s: %w", nodeAllocationRequest.Name, err)
	}

	// Process allocation for each NodeGroup
	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Calculate pending nodes for the group
		pendingNodes := nodeGroup.Size - countNodesInGroup(
			ctx, noncachedClient, logger, pluginNamespace,
			nodeAllocationRequest.Status.Properties.NodeNames, nodeGroup.NodeGroupData.Name)
		if pendingNodes <= 0 {
			// No new nodes needed, but check if existing allocated BMHs are still processing
			// This handles the case where requeue is needed for network data clearing
			allocatedBMHs, err := fetchBMHList(ctx, c, logger, nodeAllocationRequest.Spec.Site,
				nodeGroup.NodeGroupData, AllocatedBMHs, bmhNamespace)
			if err != nil {
				logger.WarnContext(ctx, "Failed to fetch allocated BMHs for processing check",
					slog.String("error", err.Error()))
				continue
			}

			// Check if any allocated BMHs are still processing (e.g., network data clearing)
			for _, bmh := range allocatedBMHs.Items {
				requeue, err := allocateBMHToNodeAllocationRequest(ctx, c, noncachedClient, logger, pluginNamespace, &bmh, nodeAllocationRequest, nodeGroup)
				if err != nil {
					logger.WarnContext(ctx, "Error checking BMH processing status",
						slog.String("bmh", bmh.Name),
						slog.String("error", err.Error()))
					continue
				}
				if requeue > DoNotRequeue {
					logger.InfoContext(ctx, "Allocated BMH still processing, requeueing",
						slog.String("bmh", bmh.Name),
						slog.Int("requeueAfter", requeue))
					return requeue, nil
				}
			}
			continue
		}

		// Only fetch unallocated BMHs if we actually need new nodes
		unallocatedBMHs, err := fetchBMHList(ctx, c, logger, nodeAllocationRequest.Spec.Site,
			nodeGroup.NodeGroupData, UnallocatedBMHs, bmhNamespace)
		if err != nil {
			return DoNotRequeue, fmt.Errorf("unable to fetch unallocated BMHs for site=%s, nodegroup=%s: %w",
				nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData.Name, err)
		}

		if len(unallocatedBMHs.Items) == 0 {
			return DoNotRequeue, fmt.Errorf("no available nodes for site=%s, nodegroup=%s",
				nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData.Name)
		}

		// Shared counter to track remaining nodes needed
		nodeCounter := pendingNodes

		// Allocate multiple nodes concurrently within the group
		for _, bmh := range unallocatedBMHs.Items {
			mu.Lock()
			if nodeCounter <= 0 {
				mu.Unlock()
				break // Stop allocation if we've reached the required count
			}

			nodeCounter--
			mu.Unlock()

			wg.Add(1)
			go func(bmh *metal3v1alpha1.BareMetalHost) {
				defer wg.Done()

				// Allocate BMH to NodeAllocationRequest
				requeue, err := allocateBMHToNodeAllocationRequest(ctx, c, noncachedClient, logger, pluginNamespace, bmh, nodeAllocationRequest, nodeGroup)
				if err != nil || requeue > DoNotRequeue {
					mu.Lock()
					if requeue > DoNotRequeue {
						// Set requeue duration - any requeue takes precedence
						requeueAfter = requeue
					}
					if err != nil {
						// Set error if there was an actual error
						if typederrors.IsInputError(err) {
							allocationErr = err
						} else {
							allocationErr = fmt.Errorf("failed to allocate BMH %s: %w", bmh.Name, err)
						}
					}
					mu.Unlock()
				}
			}(&bmh)
		}
	}

	wg.Wait()

	// Check if any error occurred or requeue needed in goroutines
	if allocationErr != nil {
		return requeueAfter, allocationErr
	}

	// If only requeue needed without error, return that
	if requeueAfter > DoNotRequeue {
		return requeueAfter, nil
	}

	// Update NodeAllocationRequest properties after all allocations are complete
	if err := hwmgrutils.UpdateNodeAllocationRequestProperties(ctx, c, nodeAllocationRequest); err != nil {
		return DoNotRequeue, fmt.Errorf("failed to update status for NodeAllocationRequest %s: %w", nodeAllocationRequest.Name, err)
	}

	return DoNotRequeue, nil
}

// getNodeAllocationRequestBMHNamespace retrieves the namespace of an already allocated BMH in the given NodeAllocationRequest.
func getNodeAllocationRequestBMHNamespace(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
) (string, error) {

	for _, nodeGroup := range nodeAllocationRequest.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Fetch only allocated BMHs that match site and resourcePoolId
		bmhList, err := fetchBMHList(ctx, c, logger, nodeAllocationRequest.Spec.Site, nodeGroup.NodeGroupData, AllocatedBMHs, "")
		if err != nil {
			return "", fmt.Errorf("unable to fetch allocated BMHs for nodegroup=%s: %w", nodeGroup.NodeGroupData.Name, err)
		}

		// Return the namespace of the first allocated BMH and stop searching
		if len(bmhList.Items) > 0 {
			return bmhList.Items[0].Namespace, nil
		}
	}

	return "", nil // No allocated BMH found, return empty namespace
}

func isNodeProvisioningInProgress(allocatednode *pluginsv1alpha1.AllocatedNode) bool {
	condition := meta.FindStatusCondition(allocatednode.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
	return condition != nil &&
		condition.Status == metav1.ConditionFalse &&
		condition.Reason == string(hwmgmtv1alpha1.InProgress)
}
