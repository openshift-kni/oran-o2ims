/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/wI2L/jsondiff"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
)

const maxK8sNameLength = 253
const maxGenerationLength = maxK8sNameLength - 100

// OperationType represents the type of node operation
type OperationType string

// Operation type constants for node changes
const (
	OperationTypeNoChange     OperationType = "no-change"    // No changes detected in the node array
	OperationTypeScaleOut     OperationType = "scale-out"    // Adding nodes only (pure scale-out)
	OperationTypeScaleIn      OperationType = "scale-in"     // Removing nodes only (pure scale-in)
	OperationTypeMixed        OperationType = "mixed"        // Adding and removing nodes simultaneously
	OperationTypeReplacement  OperationType = "replacement"  // Replacing all nodes with new ones (same count)
	OperationTypeModification OperationType = "modification" // Modifying existing nodes without count change
	OperationTypeInvalid      OperationType = "invalid"      // Invalid operation with unauthorized changes
)

// SpecChangePermission defines what types of spec changes are allowed during updates
type SpecChangePermission int

// SpecChangePermission constants define the different levels of change permissions
const (
	// SpecChangeBlocked - No spec changes allowed (HoldInstallation disabled and provisioning / reinstall in progress)
	SpecChangeBlocked SpecChangePermission = iota

	// SpecChangeUnrestricted - All spec changes allowed (HoldInstallation enabled and provisioning not completed)
	SpecChangeUnrestricted

	// SpecChangeBaseOnly - Only base field changes allowed (post-provisioning, no special operations)
	SpecChangeBaseOnly

	// SpecChangeReinstall - Base + reinstall field changes allowed (reinstall requested)
	SpecChangeReinstall
)

// String provides a human-readable description of the permission level
func (p SpecChangePermission) String() string {
	switch p {
	case SpecChangeBlocked:
		return "no changes allowed"
	case SpecChangeUnrestricted:
		return "all changes allowed"
	case SpecChangeBaseOnly:
		return "only base field changes allowed"
	case SpecChangeReinstall:
		return "base and reinstall field changes allowed"
	default:
		return "unknown permission"
	}
}

// IsBlocked returns true if no spec changes are allowed
func (p SpecChangePermission) IsBlocked() bool {
	return p == SpecChangeBlocked
}

// AllowsAllChanges returns true if all spec changes are permitted
func (p SpecChangePermission) AllowsAllChanges() bool {
	return p == SpecChangeUnrestricted
}

// GetAllowedPaths returns the list of allowed JSON paths for this permission level.
// Returns nil for SpecChangeUnrestricted (all paths allowed) or SpecChangeBlocked (no paths allowed).
func (p SpecChangePermission) GetAllowedPaths() []string {
	switch p {
	case SpecChangeBlocked:
		return []string{} // No changes allowed
	case SpecChangeUnrestricted:
		return nil // Special case: all changes allowed (validated elsewhere)
	case SpecChangeBaseOnly:
		paths := append([]string{}, ClusterLevelAllowedPaths...)
		return append(paths, NodeLevelAllowedPaths...)
	case SpecChangeReinstall:
		paths := append([]string{}, ClusterLevelAllowedPaths...)
		paths = append(paths, NodeLevelAllowedPaths...)
		paths = append(paths, ReinstallClusterPaths...)
		return append(paths, getReinstallNodePaths()...)
	default:
		return []string{}
	}
}

// Permission constants for field changes
var (
	// Base permissible fields (always allowed)
	BasePermissibleFields = []string{
		"extraAnnotations",
		"extraLabels",
		"suppressedManifests",
		"pruneManifests",
	}

	// Reinstall permissions - single source of truth
	ReinstallPermissions = map[string]string{
		"bmcAddress":             "/nodes/*/bmcAddress",
		"bootMACAddress":         "/nodes/*/bootMACAddress",
		"nodeNetwork/interfaces": "/nodes/*/nodeNetwork/interfaces/*/macAddress",
		"rootDeviceHints":        "/nodes/*/rootDeviceHints",
	}

	// Cluster-level paths (always allowed)
	ClusterLevelAllowedPaths = []string{
		"/extraAnnotations",
		"/extraLabels",
		"/suppressedManifests",
		"/pruneManifests",
		"/clusterImageSetNameRef",
		"/holdInstallation", // Allow toggling HoldInstallation (only true→false is valid, enforced in webhook)
	}

	// Node-level path patterns (always allowed)
	NodeLevelAllowedPaths = []string{
		"/nodes/*/extraAnnotations",
		"/nodes/*/extraLabels",
		"/nodes/*/suppressedManifests",
		"/nodes/*/pruneManifests",
	}

	// Additional cluster-level paths allowed during reinstall
	ReinstallClusterPaths = []string{
		"/reinstall",
	}
)

// String returns the string representation of the OperationType
func (ot OperationType) String() string {
	return string(ot)
}

// IsScaling returns true if the operation involves changing node count
func (ot OperationType) IsScaling() bool {
	return ot == OperationTypeScaleOut || ot == OperationTypeScaleIn || ot == OperationTypeMixed
}

// IsValid returns true if the operation type is valid (not invalid)
func (ot OperationType) IsValid() bool {
	return ot != OperationTypeInvalid
}

// ValidateClusterInstance ensures the ClusterInstance has required fields and valid configurations.
func ValidateClusterInstance(clusterInstance *ClusterInstance) error {
	// Ensure a cluster-level template reference exists.
	if len(clusterInstance.Spec.TemplateRefs) == 0 {
		return fmt.Errorf("missing cluster-level template reference")
	}

	// Ensure each node has a template reference.
	for _, node := range clusterInstance.Spec.Nodes {
		if len(node.TemplateRefs) == 0 {
			return fmt.Errorf("missing node-level template reference for node %q", node.HostName)
		}
	}

	// Validate JSON fields in the spec.
	if err := validateClusterInstanceJSONFields(clusterInstance); err != nil {
		return fmt.Errorf("invalid JSON field(s): %w", err)
	}

	// Validate control-plane agent count.
	if err := validateControlPlaneAgentCount(clusterInstance); err != nil {
		return fmt.Errorf("control-plane agent validation failed: %w", err)
	}

	return nil
}

// validatePostProvisioningChanges checks for changes between old and new ClusterInstance specifications.
// It ensures that only permissible fields are modified, enforcing immutability where required.
func validatePostProvisioningChanges(
	log logr.Logger,
	oldClusterInstance, newClusterInstance *ClusterInstance,
	allowReinstall bool,
) error {
	// Analyze node changes to determine the operation type
	nodeAnalysis := analyzeNodeChanges(oldClusterInstance.Spec.Nodes, newClusterInstance.Spec.Nodes, allowReinstall)

	// If there are unauthorized node modifications, reject immediately
	if nodeAnalysis.hasUnauthorizedChanges {
		return fmt.Errorf("detected unauthorized node modifications: %v", nodeAnalysis.errors)
	}

	// Log detected node operation
	if nodeAnalysis.IsPermitted() {
		log.Info(fmt.Sprintf("Detected node operation: %s", nodeAnalysis.description))
	}

	// Use JSON diff validation for all other spec changes
	return validateChangesWithJSONDiff(log, oldClusterInstance, newClusterInstance, allowReinstall, nodeAnalysis)
}

// nodeChangeAnalysis holds the simplified analysis of node changes
type nodeChangeAnalysis struct {
	operationType          OperationType
	description            string
	hasUnauthorizedChanges bool
	errors                 []string
}

// IsPermitted returns true if this is a valid operation without unauthorized changes
func (a *nodeChangeAnalysis) IsPermitted() bool {
	return a.operationType.IsValid() && !a.hasUnauthorizedChanges
}

// analyzeNodeChanges performs simplified analysis of node operations
func analyzeNodeChanges(oldNodes, newNodes []NodeSpec, allowReinstall bool) *nodeChangeAnalysis {
	analysis := &nodeChangeAnalysis{}

	oldCount := len(oldNodes)
	newCount := len(newNodes)

	// Handle simple cases first
	if oldCount == newCount {
		if areSameNodeSets(oldNodes, newNodes) {
			analysis.operationType = OperationTypeNoChange
			analysis.description = "no node changes detected"
			return analysis
		}

		if isPureReplacement, err := isPureNodeReplacement(oldNodes, newNodes); err != nil {
			analysis.hasUnauthorizedChanges = true
			analysis.errors = append(analysis.errors, fmt.Sprintf("failed to check node replacement: %v", err))
			analysis.operationType = OperationTypeInvalid
			analysis.description = "invalid node operation due to key generation error"
			return analysis
		} else if isPureReplacement {
			analysis.operationType = OperationTypeReplacement
			analysis.description = fmt.Sprintf("pure node replacement (%d nodes)", oldCount)
			return analysis
		}

		// Same count but mixed changes - check for unauthorized modifications
		analysis.checkNodeModifications(oldNodes, newNodes, allowReinstall)
		analysis.operationType = OperationTypeModification
		analysis.description = "node modifications detected"
		return analysis
	}

	// Different counts - analyze if it's pure scaling or mixed operation
	if newCount != oldCount {
		// Check if this is a pure operation or mixed
		addedNodes, removedNodes, err := analyzeNodeAdditionsAndRemovals(oldNodes, newNodes)
		if err != nil {
			analysis.hasUnauthorizedChanges = true
			analysis.errors = append(analysis.errors, fmt.Sprintf("failed to analyze node additions/removals: %v", err))
			analysis.operationType = OperationTypeInvalid
			analysis.description = "invalid node operation due to key generation error"
			return analysis
		}

		// Determine operation type based on changes
		switch {
		case len(addedNodes) > 0 && len(removedNodes) > 0:
			// Mixed operation: both additions and removals
			analysis.operationType = OperationTypeMixed
			netChange := newCount - oldCount
			if netChange > 0 {
				analysis.description = fmt.Sprintf("mixed operation: %d added, %d removed (net +%d)",
					len(addedNodes), len(removedNodes), netChange)
			} else {
				analysis.description = fmt.Sprintf("mixed operation: %d added, %d removed (net %d)",
					len(addedNodes), len(removedNodes), netChange)
			}
		case len(addedNodes) > 0:
			// Pure scale-out: only additions
			analysis.operationType = OperationTypeScaleOut
			analysis.description = fmt.Sprintf("pure scale-out: %d nodes added", len(addedNodes))
		default:
			// Pure scale-in: only removals
			analysis.operationType = OperationTypeScaleIn
			analysis.description = fmt.Sprintf("pure scale-in: %d nodes removed", len(removedNodes))
		}
	}

	// Check for unauthorized modifications in scaling operations
	analysis.checkNodeModifications(oldNodes, newNodes, allowReinstall)

	return analysis
}

// analyzeNodeAdditionsAndRemovals determines which nodes are added vs removed
func analyzeNodeAdditionsAndRemovals(oldNodes, newNodes []NodeSpec) ([]NodeSpec, []NodeSpec, error) {
	// Create maps for efficient lookups using node keys
	oldNodeMap := make(map[string]NodeSpec)
	newNodeMap := make(map[string]NodeSpec)

	for _, node := range oldNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate key for old node: %w", err)
		}
		oldNodeMap[key] = node
	}

	for _, node := range newNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate key for new node: %w", err)
		}
		newNodeMap[key] = node
	}

	var addedNodes, removedNodes []NodeSpec

	// Find added nodes (in new but not in old)
	for _, newNode := range newNodes {
		key, err := generateNodeMatchingKey(newNode)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate key for new node: %w", err)
		}
		if _, exists := oldNodeMap[key]; !exists {
			addedNodes = append(addedNodes, newNode)
		}
	}

	// Find removed nodes (in old but not in new)
	for _, oldNode := range oldNodes {
		key, err := generateNodeMatchingKey(oldNode)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate key for old node: %w", err)
		}
		if _, exists := newNodeMap[key]; !exists {
			removedNodes = append(removedNodes, oldNode)
		}
	}

	return addedNodes, removedNodes, nil
}

// checkNodeModifications looks for unauthorized changes in node modifications
func (a *nodeChangeAnalysis) checkNodeModifications(oldNodes, newNodes []NodeSpec, allowReinstall bool) {
	// Create maps for efficient lookups
	oldNodeMap := make(map[string]NodeSpec)
	for _, node := range oldNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			a.hasUnauthorizedChanges = true
			a.errors = append(a.errors, fmt.Sprintf("failed to generate key for old node: %v", err))
			return
		}
		oldNodeMap[key] = node
	}

	// Check each new node for modifications
	for _, newNode := range newNodes {
		key, err := generateNodeMatchingKey(newNode)
		if err != nil {
			a.hasUnauthorizedChanges = true
			a.errors = append(a.errors, fmt.Sprintf("failed to generate key for new node: %v", err))
			return
		}
		if oldNode, exists := oldNodeMap[key]; exists {
			// Found matching node, check for unauthorized changes
			if unauthorized := getUnauthorizedChanges(oldNode, newNode, allowReinstall); len(unauthorized) > 0 {
				a.hasUnauthorizedChanges = true
				nodeId := getNodeIdentifier(oldNode)
				for _, change := range unauthorized {
					a.errors = append(a.errors, fmt.Sprintf("node %s: unauthorized change to %s", nodeId, change))
				}
			}
		}
	}
}

// getNodeStableIdentifier returns the most stable identifier for a node in priority order
func getNodeStableIdentifier(node NodeSpec) (string, bool) {
	// Use hostname as primary identifier if available
	if node.HostName != "" {
		return node.HostName, true
	}
	// Use BMC address as secondary identifier
	if node.BmcAddress != "" {
		return node.BmcAddress, true
	}
	// Use boot MAC as tertiary identifier
	if node.BootMACAddress != "" {
		return node.BootMACAddress, true
	}
	// No stable identifier available
	return "", false
}

// generateNodeMatchingKey creates a unique key for node matching based on stable fields
func generateNodeMatchingKey(node NodeSpec) (string, error) {
	identifier, found := getNodeStableIdentifier(node)
	if !found {
		return "", fmt.Errorf("node has no unique identifiers (hostname, BMC address, or boot MAC address)")
	}

	// Add prefix based on identifier type for uniqueness across different identifier types
	switch {
	case node.HostName != "":
		return "host:" + identifier, nil
	case node.BmcAddress != "":
		return "bmc:" + identifier, nil
	default: // BootMACAddress
		return "mac:" + identifier, nil
	}
}

// areSameNodeSets checks if two node arrays contain identical nodes (order-independent)
func areSameNodeSets(oldNodes, newNodes []NodeSpec) bool {
	if len(oldNodes) != len(newNodes) {
		return false
	}

	// Create maps for order-independent comparison
	oldNodeMap := make(map[string]NodeSpec)
	newNodeMap := make(map[string]NodeSpec)

	// Build old nodes map
	for _, node := range oldNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			return false // If we can't generate keys, consider them different
		}
		oldNodeMap[key] = node
	}

	// Build new nodes map and check for missing nodes
	for _, node := range newNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			return false // If we can't generate keys, consider them different
		}
		newNodeMap[key] = node

		// Check if this node exists in old set
		if _, exists := oldNodeMap[key]; !exists {
			return false // New node not in old set
		}
	}

	// Check if all old nodes exist in new set and have identical content
	for key, oldNode := range oldNodeMap {
		newNode, exists := newNodeMap[key]
		if !exists {
			return false // Old node not in new set
		}

		// Compare node content using JSON marshaling
		oldJSON, _ := json.Marshal(oldNode)
		newJSON, _ := json.Marshal(newNode)
		if !bytes.Equal(oldJSON, newJSON) {
			return false // Same key but different content
		}
	}

	return true
}

// isPureNodeReplacement checks if all nodes are being replaced with new ones
func isPureNodeReplacement(oldNodes, newNodes []NodeSpec) (bool, error) {
	if len(oldNodes) != len(newNodes) {
		return false, nil
	}

	oldKeys := make(map[string]bool)
	for _, node := range oldNodes {
		key, err := generateNodeMatchingKey(node)
		if err != nil {
			return false, fmt.Errorf("failed to generate key for old node: %w", err)
		}
		oldKeys[key] = true
	}

	// Check if any new nodes match old ones
	for _, newNode := range newNodes {
		key, err := generateNodeMatchingKey(newNode)
		if err != nil {
			return false, fmt.Errorf("failed to generate key for new node: %w", err)
		}
		if oldKeys[key] {
			return false, nil // Found matching node, not pure replacement
		}
	}

	return true, nil // No matching nodes, pure replacement
}

// getUnauthorizedChanges compares two nodes and returns unauthorized field changes
func getUnauthorizedChanges(oldNode, newNode NodeSpec, allowReinstall bool) []string {
	var unauthorized []string

	// Check key immutable fields
	if oldNode.BmcAddress != newNode.BmcAddress && !allowReinstall {
		unauthorized = append(unauthorized, "bmcAddress")
	}
	if oldNode.BootMACAddress != newNode.BootMACAddress && !allowReinstall {
		unauthorized = append(unauthorized, "bootMACAddress")
	}
	if oldNode.HostName != newNode.HostName {
		unauthorized = append(unauthorized, "hostName")
	}

	// Check other fields using JSON diff for completeness
	if len(unauthorized) == 0 {
		unauthorized = append(unauthorized, getUnauthorizedJSONDiffs(oldNode, newNode, allowReinstall)...)
	}

	return unauthorized
}

// getUnauthorizedJSONDiffs compares two nodes via JSON diff and returns unauthorized field changes
func getUnauthorizedJSONDiffs(oldNode, newNode NodeSpec, allowReinstall bool) []string {
	oldJSON, _ := json.Marshal(oldNode)
	newJSON, _ := json.Marshal(newNode)

	if bytes.Equal(oldJSON, newJSON) {
		return nil
	}

	diffs, err := jsondiff.CompareJSON(oldJSON, newJSON)
	if err != nil {
		return nil
	}

	var unauthorized []string
	for _, diff := range diffs {
		if isCRDUpgradeFieldChange(diff) {
			continue
		}
		if !isPermissibleNodeChange(diff.Path, allowReinstall) {
			unauthorized = append(unauthorized, diff.Path)
		}
	}
	return unauthorized
}

// isCRDUpgradeFieldChange returns true if the diff represents a field being added or
// removed during/after a CRD upgrade, allowing GitOps tools to sync with pre-upgrade git sources.
func isCRDUpgradeFieldChange(diff jsondiff.Operation) bool {
	return diff.Path == "/cpuArchitecture" &&
		(diff.Type == jsondiff.OperationAdd || diff.Type == jsondiff.OperationRemove)
}

// getNodeIdentifier returns a human-readable identifier for a node
func getNodeIdentifier(node NodeSpec) string {
	identifier, found := getNodeStableIdentifier(node)
	if found {
		return identifier
	}
	return "unknown"
}

// isPermissibleNodeChange checks if a node field change is allowed
func isPermissibleNodeChange(fieldPath string, allowReinstall bool) bool {
	// Start with base permissible fields
	permissibleChanges := append([]string{}, BasePermissibleFields...)

	if allowReinstall {
		permissibleChanges = append(permissibleChanges, getReinstallPermissibleFields()...)
	}

	for _, allowed := range permissibleChanges {
		if strings.Contains(fieldPath, allowed) {
			return true
		}
	}

	return false
}

// validateChangesWithJSONDiff is the original JSON diff-based validation logic
func validateChangesWithJSONDiff(
	log logr.Logger,
	oldClusterInstance, newClusterInstance *ClusterInstance,
	allowReinstall bool,
	nodeAnalysis *nodeChangeAnalysis,
) error {
	// Marshal old and new ClusterInstance specs to JSON for comparison.
	oldSpecJSON, err := json.Marshal(oldClusterInstance.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal old ClusterInstance spec: %w", err)
	}

	newSpecJSON, err := json.Marshal(newClusterInstance.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal new ClusterInstance spec: %w", err)
	}

	// Compute JSON differences between old and new specs.
	diffs, err := jsondiff.CompareJSON(oldSpecJSON, newSpecJSON)
	if err != nil {
		return fmt.Errorf("failed to compute differences between ClusterInstance specs: %w", err)
	}

	// If no differences are found, return early.
	if len(diffs) == 0 {
		log.Info("did not detect spec changes")
		return nil
	}

	// Define permissible field changes without requiring reinstall.
	allowedUpdates := append([]string{}, ClusterLevelAllowedPaths...)
	allowedUpdates = append(allowedUpdates, NodeLevelAllowedPaths...)

	// Define additional permissible changes if reinstall is requested.
	if allowReinstall {
		allowedUpdates = append(allowedUpdates, ReinstallClusterPaths...)
		allowedUpdates = append(allowedUpdates, getReinstallNodePaths()...)
	}

	var restrictedChanges []string
	oldNodeCount := len(oldClusterInstance.Spec.Nodes)
	newNodeCount := len(newClusterInstance.Spec.Nodes)
	isScalingOperation := oldNodeCount != newNodeCount

	// Validate each detected change.
	for _, diff := range diffs {
		if pathMatchesAnyPattern(diff.Path, allowedUpdates) {
			continue // Change is allowed
		}

		// Handle node array changes during scaling operations
		if isScalingOperation && pathMatchesPattern(diff.Path, "/nodes") {
			// Allow the entire nodes array to change during scaling
			continue
		}

		// Handle node replacement operations (same count)
		if nodeAnalysis.IsPermitted() {
			if pathMatchesPattern(diff.Path, "/nodes") || pathMatchesPattern(diff.Path, "/nodes/*") {
				// Allow all node-related changes for validated node operations
				continue
			}
		}

		// Detect node scaling operations (adding/removing nodes).
		if pathMatchesPattern(diff.Path, "/nodes/*") {
			switch diff.Type {
			case jsondiff.OperationAdd:
				log.Info("Detected scale-out: new worker node added")
				continue
			case jsondiff.OperationRemove:
				log.Info("Detected scale-in: worker node removed")
				continue
			case jsondiff.OperationReplace:
				// Node replacement/modification should be rejected unless it's a pure scaling operation
				// For now, let's be conservative and reject all node modifications
				log.Info("Detected node replacement/modification - rejecting")
				restrictedChanges = append(restrictedChanges, diff.Path)
				continue
			}
		}

		if isCRDUpgradeFieldChange(diff) {
			continue
		}

		// Record disallowed changes.
		log.Info(fmt.Sprintf("spec change is disallowed %v", diff.String()))
		restrictedChanges = append(restrictedChanges, diff.Path)
	}

	// If there are disallowed changes, return an error listing the affected fields.
	if len(restrictedChanges) > 0 {
		return fmt.Errorf("detected unauthorized changes in immutable fields: %s",
			strings.Join(restrictedChanges, ", "))
	}

	return nil
}

// pathMatchesPattern checks if the given path matches a specific pattern.
// The pattern may contain "*" as a wildcard, which matches any single path segment.
func pathMatchesPattern(path, pattern string) bool {
	subPaths := strings.Split(path, "/")
	subPatterns := strings.Split(pattern, "/")

	// A valid match requires the path to have at least as many segments as the pattern.
	if len(subPaths) < len(subPatterns) {
		return false
	}

	// Compare each segment of the pattern against the corresponding segment of the path.
	for i, segment := range subPatterns {
		if segment == "*" {
			// Wildcard matches any single path segment.
			continue
		}
		if subPaths[i] != segment {
			return false
		}
	}
	return true
}

// pathMatchesAnyPattern checks if the given path matches any pattern in the provided list.
func pathMatchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if pathMatchesPattern(path, pattern) {
			return true
		}
	}
	return false
}

// hasSpecChanged determines if the Spec of a ClusterInstance has changed.
func hasSpecChanged(oldCluster, newCluster *ClusterInstance) bool {
	return !equality.Semantic.DeepEqual(oldCluster.Spec, newCluster.Spec)
}

// isProvisioningInProgress checks if the ClusterInstance is in the provisioning "InProgress" state.
func isProvisioningInProgress(clusterInstance *ClusterInstance) bool {
	condition := meta.FindStatusCondition(clusterInstance.Status.Conditions, string(ClusterProvisioned))
	return condition != nil && condition.Reason == string(InProgress)
}

// isProvisioningCompleted checks if the ClusterInstance has completed provisioning.
func isProvisioningCompleted(clusterInstance *ClusterInstance) bool {
	condition := meta.FindStatusCondition(clusterInstance.Status.Conditions, string(ClusterProvisioned))
	return condition != nil && condition.Reason == string(Completed)
}

// isReinstallRequested checks if a reinstall operation has been newly requested.
func isReinstallRequested(clusterInstance *ClusterInstance) bool {
	if clusterInstance.Spec.Reinstall == nil {
		return false
	}
	if clusterInstance.Status.Reinstall == nil {
		return true
	}
	if isReinstallInProgress(clusterInstance) {
		return false
	}
	return clusterInstance.Status.Reinstall.ObservedGeneration != clusterInstance.Spec.Reinstall.Generation
}

// isReinstallInProgress determines if a reinstall operation is actively in progress.
// A reinstall is considered in progress if the Spec.Reinstall.Generation matches
// Status.Reinstall.InProgressGeneration and the request has not been marked as completed.
func isReinstallInProgress(clusterInstance *ClusterInstance) bool {
	if clusterInstance.Spec.Reinstall == nil || clusterInstance.Status.Reinstall == nil {
		return false
	}

	reinstallStatus := clusterInstance.Status.Reinstall
	reinstallSpec := clusterInstance.Spec.Reinstall

	// A reinstall is in progress if the InProgressGeneration matches the current spec's Generation
	// and the request has not been marked as completed (RequestEndTime is still zero).
	return reinstallStatus.InProgressGeneration == reinstallSpec.Generation && reinstallStatus.RequestEndTime.IsZero()
}

// determineSpecChangePermission analyzes the ClusterInstance state and returns what type of spec changes are allowed.
func determineSpecChangePermission(oldClusterInstance, newClusterInstance *ClusterInstance) SpecChangePermission {
	// If HoldInstallation WAS enabled and provisioning has NOT completed,
	// then all changes (including disabling HoldInstallation) should be allowed.
	if oldClusterInstance.Spec.HoldInstallation && !isProvisioningCompleted(newClusterInstance) {
		return SpecChangeUnrestricted
	}
	// Note: If HoldInstallation was enabled but provisioning completed, HoldInstallation no longer has effect.
	// Fall through to normal post-provisioning logic below.

	// If provisioning or reinstall is actively in progress, then block all spec changes.
	// This prevents configuration drift during active operations
	if isProvisioningInProgress(newClusterInstance) || isReinstallInProgress(newClusterInstance) {
		return SpecChangeBlocked
	}

	// If provisioning has completed, determine what changes are allowed based on context
	if isProvisioningCompleted(newClusterInstance) {
		// If reinstall is requested, allow base + reinstall field changes
		if isReinstallRequested(newClusterInstance) {
			return SpecChangeReinstall
		}
		// Normal post-provisioning state: allow only base field changes
		return SpecChangeBaseOnly
	}

	// Default: if we cannot determine the state clearly, block changes for safety
	return SpecChangeBlocked
}

// isValidJSON checks whether a given string is a valid JSON-formatted string.
// An empty string is considered valid.
func isValidJSON(input string) bool {
	if input == "" {
		return true
	}

	var jsonData interface{}
	return json.Unmarshal([]byte(input), &jsonData) == nil
}

// validateClusterInstanceJSONFields ensures that JSON-formatted fields in a ClusterInstance are valid.
func validateClusterInstanceJSONFields(clusterInstance *ClusterInstance) error {
	if !isValidJSON(clusterInstance.Spec.InstallConfigOverrides) {
		return fmt.Errorf("installConfigOverrides is not a valid JSON-formatted string")
	}

	if !isValidJSON(clusterInstance.Spec.IgnitionConfigOverride) {
		return fmt.Errorf("cluster-level ignitionConfigOverride is not a valid JSON-formatted string")
	}

	for _, node := range clusterInstance.Spec.Nodes {
		if !isValidJSON(node.InstallerArgs) {
			return fmt.Errorf("installerArgs is not a valid JSON-formatted string [Node: Hostname=%s]", node.HostName)
		}

		if !isValidJSON(node.IgnitionConfigOverride) {
			return fmt.Errorf(
				"node-level ignitionConfigOverride is not a valid JSON-formatted string [Node: Hostname=%s]",
				node.HostName,
			)
		}
	}

	return nil // Validation succeeded
}

// validateControlPlaneAgentCount ensures that the number of control-plane nodes is valid.
func validateControlPlaneAgentCount(clusterInstance *ClusterInstance) error {
	controlPlaneCount := 0
	arbiterCount := 0
	for _, node := range clusterInstance.Spec.Nodes {
		switch node.Role {
		case "master":
			controlPlaneCount++
		case "arbiter":
			arbiterCount++
		}
	}

	if controlPlaneCount < 1 && clusterInstance.Spec.ClusterType != ClusterTypeHostedControlPlane {
		return fmt.Errorf("at least 1 control-plane agent is required")
	}

	// Ensure that SNO (Single Node OpenShift) clusters have exactly 1 control-plane node.
	if clusterInstance.Spec.ClusterType == ClusterTypeSNO && controlPlaneCount != 1 {
		return fmt.Errorf("single node OpenShift cluster-type must have exactly 1 control-plane agent")
	}

	if controlPlaneCount > 0 && clusterInstance.Spec.ClusterType == ClusterTypeHostedControlPlane {
		return fmt.Errorf("hosted control plane clusters must not have control-plane agents")
	}

	// Ensure that HighlyAvailableArbiter clusters have at least 1 arbiter node and 2 master nodes.
	if clusterInstance.Spec.ClusterType == ClusterTypeHighlyAvailableArbiter &&
		(arbiterCount < 1 || controlPlaneCount < 2) {
		return fmt.Errorf(
			"highly available arbiter cluster-type must have at least 1 arbiter agent and 2 control-plane agents",
		)
	}

	// Ensure that non-arbiter cluster types do not have arbiter nodes.
	if clusterInstance.Spec.ClusterType != ClusterTypeHighlyAvailableArbiter && arbiterCount > 0 {
		return fmt.Errorf("arbiter agents can only be used with HighlyAvailableArbiter cluster-type")
	}

	return nil // Validation succeeded
}

// validateReinstallRequest verifies whether a reinstall request is valid based on the
// current state of the ClusterInstance.
func validateReinstallRequest(clusterInstance *ClusterInstance) error {
	if clusterInstance == nil || clusterInstance.Spec.Reinstall == nil {
		return errors.New("invalid reinstall request: missing reinstall specification")
	}

	// Ensure provisioning is complete before allowing a reinstall.
	if !isProvisioningCompleted(clusterInstance) {
		return errors.New("reinstall can only be requested after successful provisioning completion")
	}

	newGeneration := clusterInstance.Spec.Reinstall.Generation
	if err := validateReinstallGeneration(newGeneration); err != nil {
		return fmt.Errorf("invalid reinstall generation: %w", err)
	}

	reinstallStatus := clusterInstance.Status.Reinstall
	if reinstallStatus == nil {
		// If there is no previous reinstall status, it's a new reinstall request and is valid.
		return nil
	}

	// Ensure no ongoing reinstall operation before allowing a new request.
	if isReinstallInProgress(clusterInstance) {
		return errors.New("cannot request reinstall while an existing reinstall is in progress")
	}

	// Prevent updates to the reinstall generation while a request is still active.
	if reinstallStatus.InProgressGeneration != "" && reinstallStatus.RequestEndTime.IsZero() {
		return errors.New("reinstall generation update is not allowed while a request is still active")
	}

	// Ensure a new generation value is set to trigger reinstall.
	if reinstallStatus.ObservedGeneration == newGeneration {
		return errors.New("must specify a new generation value to trigger a reinstall")
	}

	// Prevent reusing a previously used generation.
	for _, record := range reinstallStatus.History {
		if newGeneration == record.Generation {
			return errors.New("cannot reuse a previously used reinstall generation")
		}
	}

	return nil
}

// validateReinstallGeneration ensures the reinstall generation is a valid Kubernetes resource name.
func validateReinstallGeneration(generation string) error {
	validNameRegex := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

	if generation == "" {
		return errors.New("generation name cannot be empty")
	}

	if len(generation) > maxGenerationLength {
		return fmt.Errorf("generation name %q is too long (%d chars, max allowed: %d)",
			generation, len(generation), maxGenerationLength)
	}

	if !validNameRegex.MatchString(generation) {
		return fmt.Errorf("generation name %q is invalid: must match regex %q", generation, validNameRegex.String())
	}

	return nil
}

// getReinstallPermissibleFields returns field names from ReinstallPermissions
func getReinstallPermissibleFields() []string {
	return slices.Collect(maps.Keys(ReinstallPermissions))
}

// getReinstallNodePaths returns JSON paths from ReinstallPermissions
func getReinstallNodePaths() []string {
	return slices.Collect(maps.Values(ReinstallPermissions))
}
