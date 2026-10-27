/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/validation"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getNAR retrieves the NodeAllocationRequest CR for this ProvisioningRequest
// through the cached client. The NAR name matches the ProvisioningRequest name
// (1:1 relationship).
func (t *provisioningRequestReconcilerTask) getNAR(ctx context.Context) (*hwmgmtv1alpha1.NodeAllocationRequest, error) {
	return t.getNARWithReader(ctx, t.client)
}

// noncachedReader returns a reader that bypasses the informer cache when one is
// configured, falling back to the cached client otherwise (e.g. in unit tests
// that construct the task without a noncached client).
func (t *provisioningRequestReconcilerTask) noncachedReader() client.Reader {
	if t.noncachedClient != nil {
		return t.noncachedClient
	}
	return t.client
}

// getNARWithReader retrieves the NodeAllocationRequest CR using the supplied reader.
func (t *provisioningRequestReconcilerTask) getNARWithReader(ctx context.Context, reader client.Reader) (*hwmgmtv1alpha1.NodeAllocationRequest, error) {
	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	nar := &hwmgmtv1alpha1.NodeAllocationRequest{}
	if err := reader.Get(ctx, types.NamespacedName{Name: t.object.Name, Namespace: narNS}, nar); err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest %s/%s: %w", narNS, t.object.Name, err)
	}
	return nar, nil
}

// setNARClusterProvisioned sets the ClusterProvisioned field on the NodeAllocationRequest
// to signal to the hardware manager that the cluster is fully provisioned and operational.
func (t *provisioningRequestReconcilerTask) setNARClusterProvisioned(ctx context.Context) error {
	nar, err := t.getNAR(ctx)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	// Skip if already set
	if nar.Spec.ClusterProvisioned {
		return nil
	}

	// Set ClusterProvisioned and update
	patch := client.MergeFrom(nar.DeepCopy())
	nar.Spec.ClusterProvisioned = true
	if err := t.client.Patch(ctx, nar, patch); err != nil {
		return fmt.Errorf("failed to set clusterProvisioned on NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, "Set clusterProvisioned on NodeAllocationRequest", slog.String("nar", t.object.Name))
	return nil
}

// syncNARSkipCleanup synchronizes the skip-cleanup annotation from the ProvisioningRequest
// to the SkipCleanup field on the NodeAllocationRequest. This is used for fulfilled PRs
// where createOrUpdateNodeAllocationRequest is no longer called.
func (t *provisioningRequestReconcilerTask) syncNARSkipCleanup(ctx context.Context) error {
	nar, err := t.getNAR(ctx)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	_, hasAnnotation := t.object.Annotations[ctlrutils.SkipCleanupAnnotation]

	if hasAnnotation == nar.Spec.SkipCleanup {
		return nil // Already in sync
	}

	patch := client.MergeFrom(nar.DeepCopy())
	nar.Spec.SkipCleanup = hasAnnotation
	if err := t.client.Patch(ctx, nar, patch); err != nil {
		return fmt.Errorf("failed to sync skipCleanup on NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, "Synced skipCleanup on NodeAllocationRequest",
		slog.String("nar", t.object.Name),
		slog.Bool("skipCleanup", hasAnnotation))
	return nil
}

// createOrUpdateNodeAllocationRequest creates a new NodeAllocationRequest resource if it doesn't exist or updates it if the spec has changed.
func (t *provisioningRequestReconcilerTask) createOrUpdateNodeAllocationRequest(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) error {

	// Check if the NAR already exists
	existingNAR, err := t.getNAR(ctx)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
		}
		// NAR doesn't exist — create it
		return t.createNodeAllocationRequestResources(ctx, clusterNamespace, nodeAllocationRequest)
	}

	// NAR exists — compare spec and update if changed.
	// Carry over fields not driven by the rendered NAR to avoid false-positive change detection.
	nodeAllocationRequest.Spec.ClusterProvisioned = existingNAR.Spec.ClusterProvisioned
	renderedConfigTransactionId := nodeAllocationRequest.Spec.ConfigTransactionId
	nodeAllocationRequest.Spec.ConfigTransactionId = existingNAR.Spec.ConfigTransactionId

	specChanged := !equality.Semantic.DeepEqual(existingNAR.Spec, nodeAllocationRequest.Spec)

	if specChanged {
		nodeAllocationRequest.Spec.ConfigTransactionId = renderedConfigTransactionId
		patch := client.MergeFrom(existingNAR.DeepCopy())
		existingNAR.Spec = nodeAllocationRequest.Spec
		if err := t.client.Patch(ctx, existingNAR, patch); err != nil {
			return fmt.Errorf("failed to update NodeAllocationRequest %s: %w", t.object.Name, err)
		}

		t.logger.InfoContext(ctx,
			fmt.Sprintf("NodeAllocationRequest (%s) spec changes have been detected", t.object.Name))
		return nil
	}

	// Check for swap case: spec unchanged but AllocatedNode count is short.
	// After a swap's scale-in removes an AllocatedNode, the spec still has
	// the same sizes, so DeepEqual sees no change. Only trigger when the
	// NAR was previously fully provisioned (Provisioned=Completed) — this
	// distinguishes a swap from initial provisioning where nodes haven't
	// been allocated yet.
	provisionedCond := meta.FindStatusCondition(existingNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
	isProvisioned := provisionedCond != nil && provisionedCond.Status == metav1.ConditionTrue
	totalRequested := 0
	for _, ng := range existingNAR.Spec.NodeGroup {
		totalRequested += ng.Size
	}
	currentAllocated := len(existingNAR.Status.Properties.NodeNames)
	if isProvisioned &&
		existingNAR.Status.ObservedConfigTransactionId == existingNAR.Spec.ConfigTransactionId &&
		currentAllocated < totalRequested {
		t.logger.InfoContext(ctx, "NAR spec unchanged but allocation incomplete, forcing reallocation",
			slog.Int("requested", totalRequested),
			slog.Int("allocated", currentAllocated))
		patch := client.MergeFrom(existingNAR.DeepCopy())
		existingNAR.Spec.ConfigTransactionId++
		if err := t.client.Patch(ctx, existingNAR, patch); err != nil {
			return fmt.Errorf("failed to bump ConfigTransactionId on NodeAllocationRequest %s: %w", t.object.Name, err)
		}
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) createNodeAllocationRequestResources(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) error {

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, clusterNamespace)
	if err != nil {
		return err
	}

	// Create the NodeAllocationRequest CR
	if err := t.client.Create(ctx, nodeAllocationRequest); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create the NodeAllocationRequest", slog.Any("error", err))
		return fmt.Errorf("failed to create NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, fmt.Sprintf("Created NodeAllocationRequest (%s)", t.object.Name))

	return nil
}

// waitForHardwareData waits for the NodeAllocationRequest to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *hwmgmtv1alpha1.NodeAllocationRequest) (bool, *bool, bool, error) {

	var configured *bool
	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestProvisionStatus(ctx, clusterInstance, nodeAllocationRequestResponse)

	// Update per-node infrastructure resource statuses on every hardware check,
	// regardless of whether provisioning is complete, in-progress, or failed.
	if updateErr := t.updateInfrastructureResourceStatuses(ctx); updateErr != nil {
		t.logger.WarnContext(ctx, "Failed to update infrastructure resource statuses",
			slog.Any("error", updateErr))
	}

	if err != nil {
		return provisioned, nil, timedOutOrFailed, err
	}
	if provisioned {
		configured, timedOutOrFailed, err = t.checkNodeAllocationRequestConfigStatus(ctx, nodeAllocationRequestResponse)
	}

	return provisioned, configured, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodeAllocationRequest.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) error {

	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	allocatedNodeList, err := listAllocatedNodesForNAR(ctx, t.client, t.object.Name, narNS)
	if err != nil {
		return fmt.Errorf("failed to list AllocatedNodes for NodeAllocationRequest '%s': %w", t.object.Name, err)
	}

	hwNodes, err := collectNodeDetails(allocatedNodeList)
	if err != nil {
		return fmt.Errorf("failed to collect hardware node %s details for node allocation request: %w", t.object.Name, err)
	}

	// The pull secret must be in the same namespace as the BMH.
	pullSecretName, err := ctlrutils.GetPullSecretName(clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get pull secret name from cluster instance: %w", err)
	}
	if err := ctlrutils.CopyPullSecret(ctx, t.logger, t.client, t.object, t.ctDetails.namespace, pullSecretName, hwNodes); err != nil {
		return fmt.Errorf("failed to copy pull secret: %w", err)
	}

	configErr := t.applyNodeConfiguration(ctx, hwNodes, nodeAllocationRequest, clusterInstance)
	if configErr != nil {
		if ctlrutils.IsClusterProvisionCompleted(t.object) {
			// During scale-out, the new AllocatedNode may not exist yet when the
			// first reconcile runs. Treat this as in-progress rather than failed
			// so the user sees "progressing" while the HW manager allocates the node.
			msg := "Waiting for hardware allocation for new nodes"
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
				provisioningv1alpha1.CRconditionReasons.InProgress,
				metav1.ConditionFalse,
				msg)
			ctlrutils.SetProvisioningStateInProgress(t.object, msg)
		} else {
			failMsg := "Failed to apply node configuration to the rendered ClusterInstance: " + configErr.Error()
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
				provisioningv1alpha1.CRconditionReasons.NotApplied,
				metav1.ConditionFalse,
				failMsg)
			ctlrutils.SetProvisioningStateFailed(t.object, failMsg)
		}
	} else {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Node configuration has been applied to the rendered ClusterInstance")
	}

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if configErr != nil {
		return fmt.Errorf("failed to apply node configuration for NodeAllocationRequest '%s': %w", t.object.Name, configErr)
	}
	return nil
}

// checkNodeAllocationRequestStatus checks the NodeAllocationRequest status of a given condition type
// and updates the provisioning request status accordingly.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestStatus(
	ctx context.Context,
	nodeAllocationRequestResponse *hwmgmtv1alpha1.NodeAllocationRequest,
	condition hwmgmtv1alpha1.ConditionType) (bool, bool, error) {

	var status bool
	var timedOutOrFailed bool
	var err error

	// Single gate on the hardware manager catching up to the scheduled configuration.
	// When a Day-2 configuration is scheduled but the hardware manager has not yet
	// observed it, report the configuration as not-yet-complete (status=false) and
	// requeue. This is the source of truth for "is a hardware configuration still in
	// flight", covering two windows:
	//
	//   - The NAR still carries a Configured condition from a previous attempt (Failed,
	//     TimedOut, or a prior True) that must not be consumed before the hardware manager
	//     reprocesses the new transaction.
	//   - No Configured condition exists yet — the window after a Day-2 firmware update is
	//     scheduled but before the hardware manager attaches a condition. Returning
	//     status=false here (so the derived `configured` is non-nil false, not nil) keeps
	//     the downstream gate blocking, so the upgrade phase does not create the IBGU while
	//     the firmware flash power-cycles the node and kills IBU Prep (OCPBUGS-132825).
	//
	// Two independent "has the hardware manager caught up" signals are checked, both on
	// the NAR's own status (not the ProvisioningRequest generation):
	//
	//   - Spec.ConfigTransactionId vs Status.ObservedConfigTransactionId. ConfigTransactionId
	//     is seeded from the generation but incremented independently on swap scale-in (see
	//     createOrUpdateNodeAllocationRequest), so it can diverge.
	//   - ObjectMeta.Generation vs Status.ObservedGeneration. Because ConfigTransactionId is
	//     seeded from the PR generation yet also bumped independently on swap, a swap can leave
	//     Spec.ConfigTransactionId at a value a later generation re-renders to the same number
	//     (e.g. swap bumps 1->2 and is observed, then a generation-2 HW-profile render sets it
	//     back to 2). The transaction check alone then sees spec==observed and passes while the
	//     new generation is still unprocessed. The generation comparison closes that gap.
	//
	// Both depend on the hardware manager acknowledging the baseline generation and
	// ConfigTransactionId when it first creates the NAR (handleNewNodeAllocationRequestCreate ->
	// UpdateNodeAllocationRequestObservedStatus) and on every terminal Configured write
	// (UpdateNodeAllocationRequestStatusCondition). Without those acks the seeded baseline
	// would read as perpetually unobserved and deadlock cluster installation.
	transactionInFlight := nodeAllocationRequestResponse.Spec.ConfigTransactionId != 0 &&
		nodeAllocationRequestResponse.Status.ObservedConfigTransactionId != nodeAllocationRequestResponse.Spec.ConfigTransactionId
	generationUnobserved := nodeAllocationRequestResponse.Status.ObservedGeneration != nodeAllocationRequestResponse.ObjectMeta.Generation
	if condition == hwmgmtv1alpha1.Configured && (transactionInFlight || generationUnobserved) {
		t.logger.InfoContext(ctx, "Hardware manager has not observed the scheduled configuration",
			slog.Int64("specTransaction", nodeAllocationRequestResponse.Spec.ConfigTransactionId),
			slog.Int64("observedTransaction", nodeAllocationRequestResponse.Status.ObservedConfigTransactionId),
			slog.Int64("generation", nodeAllocationRequestResponse.ObjectMeta.Generation),
			slog.Int64("observedGeneration", nodeAllocationRequestResponse.Status.ObservedGeneration))
		return false, false, nil
	}

	// Update the provisioning request Status with status from the NodeAllocationRequest object.
	status, timedOutOrFailed, err = t.updateHardwareStatus(ctx, nodeAllocationRequestResponse, condition)
	if err != nil && !ctlrutils.IsConditionDoesNotExistsErr(err) {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodeAllocationRequest status for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.Any("error", err),
		)
	}

	return status, timedOutOrFailed, err
}

// checkNodeAllocationRequestProvisionStatus checks the provisioned status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestProvisionStatus(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *hwmgmtv1alpha1.NodeAllocationRequest,
) (bool, bool, error) {

	nodeAllocationRequestID := t.object.Name
	if nodeAllocationRequestID == "" {
		return false, false, fmt.Errorf("missing NodeAllocationRequest identifier")
	}

	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwmgmtv1alpha1.Provisioned)
	if provisioned && err == nil {
		t.logger.InfoContext(ctx, fmt.Sprintf("NodeAllocationRequest (%s) is provisioned", nodeAllocationRequestID))
		if err = t.updateClusterInstance(ctx, clusterInstance, nodeAllocationRequestResponse); err != nil {
			return provisioned, timedOutOrFailed, fmt.Errorf("failed to update the rendered cluster instance: %w", err)
		}
	}

	return provisioned, timedOutOrFailed, err
}

// mapAllocatedNodeToResourceProvisioningPhase derives a ResourceProvisioningPhase from an
// AllocatedNode's status conditions by inspecting the "Provisioned" condition.
func mapAllocatedNodeToResourceProvisioningPhase(conditions []metav1.Condition) provisioningv1alpha1.ResourceProvisioningPhase {
	cond := meta.FindStatusCondition(conditions, string(hwmgmtv1alpha1.Provisioned))
	if cond == nil {
		return provisioningv1alpha1.ResourceProvisioningPhaseProcessing
	}
	switch {
	case cond.Status == metav1.ConditionTrue:
		return provisioningv1alpha1.ResourceProvisioningPhaseProvisioned
	case cond.Reason == string(hwmgmtv1alpha1.Failed) ||
		cond.Reason == string(hwmgmtv1alpha1.TimedOut):
		return provisioningv1alpha1.ResourceProvisioningPhaseFailed
	default:
		return provisioningv1alpha1.ResourceProvisioningPhaseProcessing
	}
}

// hasInfrastructureResourceStatuses returns true when the CRD still
// holds per-node statuses that should be cleared.
func (t *provisioningRequestReconcilerTask) hasInfrastructureResourceStatuses() bool {
	return len(t.object.Status.Extensions.InfrastructureResourceStatuses) > 0
}

// updateInfrastructureResourceStatuses updates the infrastructure resource statuses for the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) updateInfrastructureResourceStatuses(ctx context.Context) error {

	// Get the AllocatedNodes for the NAR
	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	allocatedNodeList, err := listAllocatedNodesForNAR(ctx, t.client, t.object.Name, narNS)
	if err != nil {
		return fmt.Errorf("failed to list AllocatedNodes for NAR '%s': %w", t.object.Name, err)
	}

	// Clear stale statuses when AllocatedNodes no longer exist but previous statuses
	// are still recorded on the CRD (e.g. nodes were deallocated while the NAR reference remains).
	hasAllocatedNodes := len(allocatedNodeList.Items) > 0
	if !hasAllocatedNodes {
		if t.hasInfrastructureResourceStatuses() {
			t.object.Status.Extensions.InfrastructureResourceStatuses = nil
			if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				return fmt.Errorf("failed to clear stale infrastructure resource statuses: %w", err)
			}
			t.logger.InfoContext(ctx, "cleared stale infrastructure resource statuses",
				slog.String("nar", t.object.Name))
		}
		t.logger.InfoContext(ctx, "no allocated nodes found, skipping infrastructure resource statuses update",
			slog.String("nar", t.object.Name))
		return nil
	}

	// Compute the new statuses
	statuses := make([]provisioningv1alpha1.InfrastructureResourceStatus, 0, len(allocatedNodeList.Items))
	for i := range allocatedNodeList.Items {
		node := &allocatedNodeList.Items[i]
		// Prefer the assigned hostname from AllocatedNodeHostMap once available; fall back to the
		// AllocatedNode CR name (the hardware manager's node identifier).
		resourceName := node.Name
		if host := t.object.Status.Extensions.AllocatedNodeHostMap[node.Name]; host != "" {
			resourceName = host
		}
		statuses = append(statuses, provisioningv1alpha1.InfrastructureResourceStatus{
			ResourceName:              resourceName,
			ResourceId:                node.Name,
			ResourceProvisioningPhase: mapAllocatedNodeToResourceProvisioningPhase(node.Status.Conditions),
		})
	}

	// Sort by ResourceId for deterministic comparison (List order is not guaranteed).
	slices.SortFunc(statuses, func(a, b provisioningv1alpha1.InfrastructureResourceStatus) int {
		return strings.Compare(a.ResourceId, b.ResourceId)
	})

	// Skip the statuses update if the computed statuses are the same as the previous ones.
	if equality.Semantic.DeepEqual(t.object.Status.Extensions.InfrastructureResourceStatuses, statuses) {
		t.logger.InfoContext(ctx, "No changes in infrastructure resource statuses; skipping update",
			slog.Int("nodeCount", len(statuses)))
		return nil
	}

	// Update the statuses
	t.object.Status.Extensions.InfrastructureResourceStatuses = statuses
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update infrastructure resource statuses: %w", err)
	}

	t.logger.InfoContext(ctx, "Updated infrastructure resource statuses",
		slog.Int("nodeCount", len(statuses)))
	return nil
}

// checkNodeAllocationRequestConfigStatus checks the Configured status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestConfigStatus(
	ctx context.Context,
	nodeAllocationRequestResponse *hwmgmtv1alpha1.NodeAllocationRequest,
) (*bool, bool, error) {

	status, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwmgmtv1alpha1.Configured)
	if err != nil {
		if ctlrutils.IsConditionDoesNotExistsErr(err) {
			// Condition does not exist, return nil (acceptable case)
			return nil, timedOutOrFailed, nil
		}
		return nil, timedOutOrFailed, fmt.Errorf("failed to check NodeAllocationRequest Configured status: %w", err)
	}
	return &status, timedOutOrFailed, nil
}

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *provisioningRequestReconcilerTask) applyNodeConfiguration(
	ctx context.Context,
	hwNodes map[string][]ctlrutils.NodeInfo,
	nar *hwmgmtv1alpha1.NodeAllocationRequest,
	clusterInstance *unstructured.Unstructured,
) error {

	// Create a map to track unmatched nodes
	unmatchedNodes := make(map[int]string)

	// ClusterInstance nodes format (legacy): one hardware group per installer role.
	roleToNodeGroupName := getRoleToGroupNameMap(&nar.Spec)
	// ClusterInstance nodeGroups format: hardware group by hostname.
	hostToGroupName := map[string]string{}
	if t.clusterInput != nil {
		hostToGroupName = t.clusterInput.hostToGroupName
	}

	// Extract the nodes slice
	nodes, found, err := unstructured.NestedSlice(clusterInstance.Object, "spec", "nodes")
	if err != nil {
		return fmt.Errorf("failed to extract nodes from cluster instance: %w", err)
	}
	if !found {
		return fmt.Errorf("spec.nodes not found in cluster instance")
	}

	// Classify hardware nodes into two groups based on existing AllocatedNodeHostMap:
	//   - assignedByHost: nodes with an existing host assignment (preserve on re-reconciliation)
	//   - availableByGroup: nodes available for fresh assignment, ordered by the sorted
	//     input from collectNodeDetails so initial assignment is deterministic
	assignedByHost := make(map[string]ctlrutils.NodeInfo)
	availableByGroup := make(map[string][]ctlrutils.NodeInfo)
	for groupName, nodeInfos := range hwNodes {
		for _, ni := range nodeInfos {
			if hostName, ok := t.object.Status.Extensions.AllocatedNodeHostMap[ni.NodeID]; ok {
				assignedByHost[groupName+"/"+hostName] = ni
			} else {
				availableByGroup[groupName] = append(availableByGroup[groupName], ni)
			}
		}
	}

	for i, n := range nodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			return fmt.Errorf("node at index %d is not a valid map", i)
		}

		role, _, _ := unstructured.NestedString(nodeMap, "role")
		hostName, _, _ := unstructured.NestedString(nodeMap, "hostName")
		var groupName string
		if len(hostToGroupName) > 0 {
			groupName = hostToGroupName[hostName]
		} else {
			groupName = roleToNodeGroupName[role]
		}

		var nodeInfo ctlrutils.NodeInfo
		matched := false

		// Resolve nodeInfo: prefer existing assignment, fall back to available nodes
		if ni, ok := assignedByHost[groupName+"/"+hostName]; ok {
			nodeInfo = ni
			matched = true
		}

		if !matched {
			if candidates := availableByGroup[groupName]; len(candidates) > 0 {
				nodeInfo = candidates[0]
				availableByGroup[groupName] = candidates[1:]
				matched = true
			}
		}

		if !matched {
			unmatchedNodes[i] = hostName
			continue
		}

		// Make a copy of the nodeMap before mutating
		updatedNode := maps.Clone(nodeMap)

		// Set BMC info
		updatedNode["bmcAddress"] = nodeInfo.BmcAddress
		updatedNode["bmcCredentialsName"] = map[string]interface{}{
			"name": nodeInfo.BmcCredentials,
		}

		if nodeInfo.HwMgrNodeId != "" && nodeInfo.HwMgrNodeNs != "" {
			hostRef, ok := updatedNode["hostRef"].(map[string]interface{})
			if !ok {
				hostRef = make(map[string]interface{})
			}
			hostRef["name"] = nodeInfo.HwMgrNodeId
			hostRef["namespace"] = nodeInfo.HwMgrNodeNs
			updatedNode["hostRef"] = hostRef
		}
		// Boot MAC
		bootMAC, err := ctlrutils.GetBootMacAddress(nodeInfo.Interfaces, constants.BootInterfaceLabel)
		if err != nil {
			return fmt.Errorf("failed to get boot MAC for node '%s': %w", hostName, err)
		}
		updatedNode["bootMACAddress"] = bootMAC

		// Assign MACs to interfaces
		if err := ctlrutils.AssignMacAddress(t.clusterInput.clusterInstanceData, nodeInfo.Interfaces, updatedNode); err != nil {
			return fmt.Errorf("failed to assign MACs for node '%s': %w", hostName, err)
		}

		// Update AllocatedNodeHostMap
		if err := t.updateAllocatedNodeHostMap(ctx, nodeInfo.NodeID, hostName); err != nil {
			return fmt.Errorf("failed to update status for node '%s': %w", hostName, err)
		}

		// Update the node only after all mutations succeed
		nodes[i] = updatedNode
	}

	// Final write back to clusterInstance
	if err := unstructured.SetNestedSlice(clusterInstance.Object, nodes, "spec", "nodes"); err != nil {
		return fmt.Errorf("failed to update nodes in cluster instance: %w", err)
	}
	// Check if there are unmatched nodes
	if len(unmatchedNodes) > 0 {
		var unmatchedDetails []string
		for idx, name := range unmatchedNodes {
			unmatchedDetails = append(unmatchedDetails, fmt.Sprintf("Index: %d, Host Name: %s", idx, name))
		}
		return fmt.Errorf("failed to find matches for the following nodes: %s", strings.Join(unmatchedDetails, "; "))
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) updateAllocatedNodeHostMap(ctx context.Context, allocatedNodeID, hostName string) error {

	if allocatedNodeID == "" || hostName == "" {
		t.logger.InfoContext(ctx, "Missing either allocatedNodeID or hostName for updating AllocatedNodeHostMap status")
		return nil
	}

	if t.object.Status.Extensions.AllocatedNodeHostMap == nil {
		t.object.Status.Extensions.AllocatedNodeHostMap = make(map[string]string)
	}

	if t.object.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID] == hostName {
		// nothing to do
		return nil
	}

	t.logger.InfoContext(ctx, "Updating AllocatedNodeHostMap status",
		slog.String("allocatedNodeID", allocatedNodeID),
		slog.String("hostName", hostName))

	t.object.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID] = hostName

	// Update the CR status for the ProvisioningRequest.
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update AllocatedNodeHostMap: %w", err)
	}

	return nil
}

// processExistingHardwareCondition processes an existing hardware condition and returns the appropriate status
func (t *provisioningRequestReconcilerTask) processExistingHardwareCondition(
	hwCondition *metav1.Condition,
	condition hwmgmtv1alpha1.ConditionType,
) (metav1.ConditionStatus, string, string, bool) {
	status := hwCondition.Status
	reason := hwCondition.Reason
	message := hwCondition.Message
	timedOutOrFailed := false

	// Check if the condition indicates failure or timeout
	if reason == string(hwmgmtv1alpha1.Failed) || reason == string(hwmgmtv1alpha1.TimedOut) {
		timedOutOrFailed = true
		ctlrutils.SetProvisioningStateFailed(t.object, message)
	}

	// Ensure a consistent message for the provisioning request, regardless of which hardware manager is used.
	// The message is augmented with additional NAR context for in-progress, failure, and success states.
	// - Success: build a completion message for hardware conditions
	// - Timeout/Failure: enrich the base failure message with detailed NAR error context
	// - In-progress: enrich the message with NAR context when available, otherwise provide a generic in-progress message
	if status == metav1.ConditionTrue {
		// Hardware provisioning/configuration completed successfully
		if strings.TrimSpace(message) != "" {
			message = fmt.Sprintf("Hardware %s completed: %s", ctlrutils.GetStatusMessage(condition), message)
		} else {
			message = fmt.Sprintf("Hardware %s completed", ctlrutils.GetStatusMessage(condition))
		}
	} else if status == metav1.ConditionFalse {
		if reason == string(hwmgmtv1alpha1.Failed) || reason == string(hwmgmtv1alpha1.TimedOut) {
			timedOutOrFailed = true
			if reason == string(hwmgmtv1alpha1.Failed) {
				// For failures, preserve the detailed error from NAR
				message = fmt.Sprintf("Hardware %s failed: %s", ctlrutils.GetStatusMessage(condition), message)
			} else if reason == string(hwmgmtv1alpha1.TimedOut) {
				// For timeouts, preserve the timeout message from NAR
				message = fmt.Sprintf("Hardware %s failed: %s", ctlrutils.GetStatusMessage(condition), message)
			}
			ctlrutils.SetProvisioningStateFailed(t.object, message)
		} else {
			// For in-progress states, preserve NAR context if it provides useful information
			if strings.TrimSpace(message) != "" {
				message = fmt.Sprintf("Hardware %s is in progress: %s", ctlrutils.GetStatusMessage(condition), message)
			} else {
				message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))
			}
			ctlrutils.SetProvisioningStateInProgress(t.object, message)
		}
	}

	return status, reason, message, timedOutOrFailed
}

// updateHardwareStatus updates the hardware status for the ProvisioningRequest.
// Returns:
//   - status (bool): true if the hardware condition is completed successfully (ConditionTrue)
//   - timedOutOrFailed (bool): true if the hardware has timed out or failed
//   - error: any error that occurred during status processing
func (t *provisioningRequestReconcilerTask) updateHardwareStatus(
	ctx context.Context,
	nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest,
	condition hwmgmtv1alpha1.ConditionType,
) (bool, bool, error) {

	nodeAllocationRequestID := t.object.Name
	if nodeAllocationRequestID == "" {
		return false, false, fmt.Errorf("missing NodeAllocationRequest identifier")
	}

	var (
		status  metav1.ConditionStatus
		reason  string
		message string
		err     error
	)
	timedOutOrFailed := false // Default to false unless explicitly needed

	// Retrieve the given hardware condition(Provisioned or Configured) from the nodeAllocationRequest status.
	var hwCondition *metav1.Condition
	for i := range nodeAllocationRequest.Status.Conditions {
		if nodeAllocationRequest.Status.Conditions[i].Type == string(condition) {
			hwCondition = &nodeAllocationRequest.Status.Conditions[i]
		}
	}

	if hwCondition == nil {
		// Condition does not exist in NAR status.
		if condition == hwmgmtv1alpha1.Configured {
			// Reaching here for the Configured condition means the hardware manager has
			// already observed the scheduled ConfigTransactionId — the spec-vs-observed
			// gate in checkNodeAllocationRequestStatus returns earlier otherwise — yet
			// has emitted no Configured condition, i.e. no hardware configuration was
			// initiated. Return a custom error so callers treat configuration as absent
			// (configured==nil) and proceed.
			return false, false, &ctlrutils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
		}
		// The Provisioned condition does not exist yet: provisioning is in progress.
		status = metav1.ConditionFalse
		reason = string(provisioningv1alpha1.CRconditionReasons.InProgress)
		message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
	} else {
		// A hardware condition was found in NAR status - always process it
		// (even if we're waiting for config start, the hardware manager has provided valid state)
		status, reason, message, timedOutOrFailed = t.processExistingHardwareCondition(hwCondition, condition)
	}

	conditionType := provisioningv1alpha1.PRconditionTypes.HardwareProvisioned
	if condition == hwmgmtv1alpha1.Configured {
		conditionType = provisioningv1alpha1.PRconditionTypes.HardwareConfigured
	}

	// Map hardware-specific reasons to provisioning request reasons
	provisioningReason := ctlrutils.MapHardwareReasonToProvisioningReason(reason)

	// Handle unknown reasons with warning (only if Unknown was returned)
	if provisioningReason == provisioningv1alpha1.CRconditionReasons.Unknown {
		t.logger.WarnContext(ctx, "Unknown hardware condition reason encountered",
			slog.String("hardwareReason", reason),
			slog.String("conditionType", string(condition)),
			slog.String("status", string(status)))
	}

	// Set the status condition for hardware status.
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		conditionType,
		provisioningReason,
		status,
		message)
	t.logger.InfoContext(ctx, fmt.Sprintf("NodeAllocationRequest (%s) %s status: %s",
		nodeAllocationRequestID, ctlrutils.GetStatusMessage(condition), message))

	// Update the CR status for the ProvisioningRequest.
	if err = ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update Hardware %s status: %w", ctlrutils.GetStatusMessage(condition), err)
	}

	return status == metav1.ConditionTrue, timedOutOrFailed, err
}

// hardwareConfigInProgress reports whether a hardware configuration transaction
// is still in flight, so the reconciler must not advance to downstream phases
// (cluster installation, policy configuration, or — critically — IBU upgrade Prep).
//
// configured is the tri-state result derived from checkNodeAllocationRequestConfigStatus:
//   - non-nil and false: a configuration transaction is in progress. This covers both a
//     Configured condition that is not yet True (e.g. a Day-2 firmware update applying
//     and power-cycling the node) and the window after a Day-2 firmware update is
//     scheduled but before a Configured condition exists — the spec-vs-observed gate in
//     checkNodeAllocationRequestStatus reports both as status=false. Proceeding in the
//     latter window would let the upgrade phase create the IBGU while the firmware flash
//     reboots the node and kills IBU Prep (OCPBUGS-132825).
//   - non-nil and true: the configuration completed successfully.
//   - nil: no configuration is scheduled or in progress; it is safe to proceed.
func hardwareConfigInProgress(configured *bool) bool {
	return configured != nil && !*configured
}

// buildNodeAllocationRequestSpec builds the NodeAllocationRequest from the pre-merged hwMgmt data and cluster instance
func (t *provisioningRequestReconcilerTask) buildNodeAllocationRequestSpec(
	clusterInstance *unstructured.Unstructured) (*hwmgmtv1alpha1.NodeAllocationRequest, error) {

	hwMgmtData := t.clusterInput.hwMgmtData

	// ClusterInstance nodes format (legacy): size each hardware group by installer role
	// (one hardware group per role).
	roleCounts := make(map[string]int)
	nodes, found, err := unstructured.NestedSlice(clusterInstance.Object, "spec", "nodes")
	if err != nil {
		return nil, fmt.Errorf("failed to extract nodes from cluster instance: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("spec.nodes not found in cluster instance")
	}

	for i, n := range nodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("node at index %d is not a valid map", i)
		}

		role, _, _ := unstructured.NestedString(nodeMap, "role")
		roleCounts[role]++
	}

	// ClusterInstance nodeGroups format: size each hardware group by hostname membership.
	groupSizes := make(map[string]int)
	for _, groupName := range t.clusterInput.hostToGroupName {
		groupSizes[groupName]++
	}

	// Extract nodeGroupData from the pre-merged hwMgmt data
	nodeGroupDataRaw, ok := hwMgmtData[ctlrutils.HwMgmtNodeGroupDataKey]
	if !ok {
		return nil, typederrors.NewInputError("%s not found in merged hwMgmt data", ctlrutils.HwMgmtNodeGroupDataKey)
	}
	nodeGroupDataSlice, ok := nodeGroupDataRaw.([]any)
	if !ok {
		return nil, typederrors.NewInputError("%s must be an array", ctlrutils.HwMgmtNodeGroupDataKey)
	}

	// Node group validation (name, role, duplicates) is handled by validateMergedNodeGroups
	// which runs before this function. Here we only extract values to build the NAR.
	nodeGroups := []hwmgmtv1alpha1.NodeGroup{}
	for _, ngRaw := range nodeGroupDataSlice {
		ngMap, ok := ngRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s element is not a map", ctlrutils.HwMgmtNodeGroupDataKey)
		}

		name, _ := ngMap["name"].(string)
		role, _ := ngMap["role"].(string)
		hwProfile, _ := ngMap["hwProfile"].(string)
		resourcePoolId, _ := ngMap["resourcePoolId"].(string)

		resourceSelector := make(map[string]string)
		if rs, ok := ngMap["resourceSelector"].(map[string]any); ok {
			for k, v := range rs {
				if vStr, ok := v.(string); ok {
					resourceSelector[k] = vStr
				}
			}
		}

		ngd := hwmgmtv1alpha1.NodeGroupData{
			HwProfile:        hwProfile,
			Name:             name,
			ResourcePoolId:   resourcePoolId,
			ResourceSelector: resourceSelector,
			Role:             role,
		}

		var size int
		if len(t.clusterInput.hostToGroupName) > 0 {
			// ClusterInstance nodeGroups format path
			size = groupSizes[name]
		} else {
			// ClusterInstance nodes format path (legacy)
			size = roleCounts[role]
		}
		nodeGroups = append(nodeGroups, hwmgmtv1alpha1.NodeGroup{
			NodeGroupData: ngd,
			Size:          size,
		})
	}

	siteIDRaw, err := validation.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, constants.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", constants.TemplateParamOCloudSiteId, err)
	}
	siteID, ok := siteIDRaw.(string)
	if !ok {
		return nil, fmt.Errorf("%s is not a string", constants.TemplateParamOCloudSiteId)
	}

	clusterIdRaw, err := validation.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, constants.TemplateParamNodeClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", constants.TemplateParamNodeClusterName, err)
	}
	clusterId, ok := clusterIdRaw.(string)
	if !ok {
		return nil, fmt.Errorf("%s is not a string", constants.TemplateParamNodeClusterName)
	}

	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)

	// Set HardwareProvisioningTimeout from merged data, or use default
	hwTimeout := &metav1.Duration{Duration: ctlrutils.DefaultHardwareProvisioningTimeout}
	if ts, ok := hwMgmtData["hardwareProvisioningTimeout"].(string); ok && ts != "" {
		d, err := time.ParseDuration(ts)
		if err != nil {
			return nil, fmt.Errorf("invalid hardwareProvisioningTimeout %q: %w", ts, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("invalid hardwareProvisioningTimeout %q: must be > 0", ts)
		}
		hwTimeout = &metav1.Duration{Duration: d}
	}

	_, hasSkipCleanup := t.object.Annotations[ctlrutils.SkipCleanupAnnotation]

	nar := &hwmgmtv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.object.Name,
			Namespace: narNS,
		},
		Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
			ClusterId:                   clusterId,
			NodeGroup:                   nodeGroups,
			LocationSpec:                hwmgmtv1alpha1.LocationSpec{Site: siteID},
			ConfigTransactionId:         t.object.Generation,
			HardwareProvisioningTimeout: hwTimeout,
			SkipCleanup:                 hasSkipCleanup,
		},
	}

	return nar, nil
}

func (t *provisioningRequestReconcilerTask) buildNodeAllocationRequest(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwmgmtv1alpha1.NodeAllocationRequest, error) {

	// Check if NAR can be fetched (ignore NotFound — it may not exist yet)
	if _, err := t.getNAR(ctx); err != nil && !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", t.object.Name, err)
	}

	nodeAllocationRequest, err := t.buildNodeAllocationRequestSpec(clusterInstance)
	if err != nil {
		return nil, err
	}

	return nodeAllocationRequest, nil
}
