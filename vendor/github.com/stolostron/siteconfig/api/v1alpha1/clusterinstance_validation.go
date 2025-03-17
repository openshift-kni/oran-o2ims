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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/wI2L/jsondiff"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
)

const maxK8sNameLength = 253
const maxGenerationLength = maxK8sNameLength - 100

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
	allowedUpdates := []string{
		"/extraAnnotations",
		"/extraLabels",
		"/suppressedManifests",
		"/pruneManifests",
		"/nodes/*/extraAnnotations",
		"/nodes/*/extraLabels",
		"/nodes/*/suppressedManifests",
		"/nodes/*/pruneManifests",
	}

	// Define additional permissible changes if reinstall is requested.
	if allowReinstall {
		allowedUpdates = append(allowedUpdates, []string{
			"/reinstall",
			"/nodes/*/bootMACAddress",
			"/nodes/*/nodeNetwork/interfaces/macAddress",
			"/nodes/*/rootDeviceHints",
		}...)
	}

	var restrictedChanges []string

	// Validate each detected change.
	for _, diff := range diffs {

		if pathMatchesAnyPattern(diff.Path, allowedUpdates) {
			continue // Change is allowed
		}

		// Detect node scaling operations (adding/removing nodes).
		if pathMatchesPattern(diff.Path, "/nodes/*") {
			if diff.Type == jsondiff.OperationAdd {
				log.Info("Detected scale-out: new worker node added")
				continue
			} else if diff.Type == jsondiff.OperationRemove {
				log.Info("Detected scale-in: worker node removed")
				continue
			}
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
	for _, node := range clusterInstance.Spec.Nodes {
		if node.Role == "master" {
			controlPlaneCount++
		}
	}

	if controlPlaneCount < 1 {
		return fmt.Errorf("at least 1 control-plane agent is required")
	}

	// Ensure that SNO (Single Node OpenShift) clusters have exactly 1 control-plane node.
	if clusterInstance.Spec.ClusterType == ClusterTypeSNO && controlPlaneCount != 1 {
		return fmt.Errorf("single node OpenShift cluster-type must have exactly 1 control-plane agent")
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
