/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/r3labs/diff/v3"
)

var (
	// AllowedClusterInstanceFields contains path patterns for fields that are allowed
	// to be updated from the ProvisioningRequest after cluster installation.
	// The wildcard "*" is used to match any index in a list.
	AllowedClusterInstanceFields = [][]string{
		// Cluster-level non-immutable fields
		{"extraAnnotations"},
		{"extraLabels"},
		// Node-level non-immutable fields (legacy flat nodes)
		{constants.ClusterInstanceNodesKey, "*", "extraAnnotations"},
		{constants.ClusterInstanceNodesKey, "*", "extraLabels"},
		// Node-group-level and nested node non-immutable fields
		{constants.ClusterInstanceNodeGroupsKey, "*", "extraAnnotations"},
		{constants.ClusterInstanceNodeGroupsKey, "*", "extraLabels"},
		{constants.ClusterInstanceNodeGroupsKey, "*", constants.ClusterInstanceNodesKey, "*", "extraAnnotations"},
		{constants.ClusterInstanceNodeGroupsKey, "*", constants.ClusterInstanceNodesKey, "*", "extraLabels"},
	}

	// IgnoredClusterInstanceFields contains path patterns for fields that should be ignored
	// when comparing a rendered ClusterInstance spec (already in flat nodes format) with an
	// existing one, for example during upgrade handling.
	// The wildcard "*" is used to match any index in a list.
	IgnoredClusterInstanceFields = [][]string{
		// Node-level ignored fields
		{constants.ClusterInstanceNodesKey, "*", "bmcAddress"},
		{constants.ClusterInstanceNodesKey, "*", "bmcCredentialsName"},
		{constants.ClusterInstanceNodesKey, "*", "bootMACAddress"},
		{constants.ClusterInstanceNodesKey, "*", "hostRef"},
		{constants.ClusterInstanceNodesKey, "*", "nodeNetwork", "interfaces", "*", "macAddress"},
		// The interface labels are not part of the ClusterInstance.
		{constants.ClusterInstanceNodesKey, "*", "nodeNetwork", "interfaces", "*", "label"},
		// modified for upgrade
		{"suppressedManifests"},
	}
)

// ValidateClusterInstanceInputMatchesSchema validates that the ClusterInstance input
// from the ProvisioningRequest matches the schema defined in the ClusterTemplate.
// If valid, it returns the matching ClusterInstance input.
func ValidateClusterInstanceInputMatchesSchema(templateName string, templateSchemaRaw, templateParametersRaw []byte) (any, error) {

	// Get the subschema for ClusterInstanceParameters
	clusterInstanceSubSchema, err := ExtractSubSchema(
		templateSchemaRaw, constants.TemplateParamClusterInstance)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to extract %s subschema: %s", constants.TemplateParamClusterInstance, err.Error())
	}
	// Any unknown fields not defined in the schema will be disallowed
	DisallowUnknownFieldsInSchema(clusterInstanceSubSchema)

	// Get the matching input for ClusterInstanceParameters
	clusterInstanceMatchingInput, err := ExtractMatchingInput(
		templateParametersRaw, constants.TemplateParamClusterInstance)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to extract matching input for subSchema %s: %w", constants.TemplateParamClusterInstance, err)
	}

	// The schema defined in ClusterTemplate's spec.templateParameterSchema for
	// clusterInstanceParameters represents a subschema of ClusterInstance parameters that
	// are allowed/exposed only to the ProvisioningRequest. Therefore, validate the ClusterInstance
	// input from the ProvisioningRequest against this schema, rather than validating the merged
	// ClusterInstance data. A full validation of the complete ClusterInstance input will be
	// performed during the ClusterInstance dry-run later in the controller.
	err = ValidateJSONSchema(
		clusterInstanceSubSchema, clusterInstanceMatchingInput)
	if err != nil {
		return nil, fmt.Errorf(
			"spec.templateParameters.%s does not match the schema defined in ClusterTemplate (%s) spec.templateParameterSchema.%s: %w",
			constants.TemplateParamClusterInstance, templateName, constants.TemplateParamClusterInstance, err)
	}

	return clusterInstanceMatchingInput, nil
}

// FindClusterInstanceImmutableFieldUpdates identifies updates made to immutable fields
// in the ClusterInstance fields. It returns two lists of paths: a list of updated fields
// that are considered immutable and should not be modified and a list of fields related
// to node scaling, indicating nodes that were added, removed or swapped.
func FindClusterInstanceImmutableFieldUpdates(
	oldData, newData map[string]any, ignoredFields [][]string, allowedFields [][]string) ([]string, []string, error) {

	diffs, err := diff.Diff(oldData, newData, diff.AllowTypeMismatch(true))
	if err != nil {
		return nil, nil, fmt.Errorf("error comparing differences between old "+
			"and new ClusterInstance input: %w", err)
	}

	// First pass: find hosts whose hostName changed in-place (swap) under nodeGroups.
	// When a node's hostName is updated in-place, the diff library reports field-level
	// changes (e.g., nodeGroups.0.nodes.1.hostName) rather than a whole-node
	// add/remove. We treat all changes under that host as scaling.
	// Keys are "nodeGroups.<gi>.nodes.<ni>".
	swappedNodeKeys := make(map[string]bool)
	for _, d := range diffs {
		if d.Type != "update" {
			continue
		}
		// ["nodeGroups", "<gi>", "nodes", "<ni>", "hostName", ...]
		if len(d.Path) >= 5 && d.Path[0] == constants.ClusterInstanceNodeGroupsKey &&
			d.Path[2] == constants.ClusterInstanceNodesKey && d.Path[4] == "hostName" {
			swappedNodeKeys[fmt.Sprintf("%s.%s.%s.%s",
				constants.ClusterInstanceNodeGroupsKey, d.Path[1],
				constants.ClusterInstanceNodesKey, d.Path[3])] = true
		}
	}

	var updatedFields []string
	var scalingNodes []string
	for _, diff := range diffs {
		if diff.Type == "update" {
			if diff.From == nil || diff.To == nil {
				continue
			}
			// Get value and type of the initial field.
			from := reflect.ValueOf(diff.From).Interface()
			fromValue := fmt.Sprintf("%v", from)
			fromType := fmt.Sprintf("%T", from)
			// Get value and type of the new field.
			to := reflect.ValueOf(diff.To).Interface()
			toValue := fmt.Sprintf("%v", to)
			toType := fmt.Sprintf("%T", to)

			// If the type has changed, also check the value. For the IMS usecase we do no support type
			// changes, so this is the case of a mismatch from unmarshalling and it should be ignored if
			// the value has been kept.
			if fromType != toType {
				if fromValue == toValue {
					continue
				}
			}
		}
		/* Examples of diff result in json format

		Label added at the cluster-level
		  {"type": "create", "path": ["extraLabels", "ManagedCluster", "newLabelKey"], "from": null, "to": "newLabelValue"}

		Field updated at the cluster-level
		  {"type": "update", "path": ["baseDomain"], "from": "domain.example.com", "to": "newdomain.example.com"}

		New nodeGroup added (e.g. first workers after a masters-only provision)
		  {"type": "create", "path": ["nodeGroups", "1"], "from": null, "to": {"name": "worker", "nodes": [...]}}

		Entire nodeGroup removed (e.g. scale workers to zero by omitting the group)
		  {"type": "delete", "path": ["nodeGroups", "1"], "from": {"name": "worker", "nodes": [...]}, "to": null}

		Entire nodes list added under a nodeGroup (e.g. first workers after a masters-only provision)
		  {"type": "create", "path": ["nodeGroups", "1", "nodes"], "from": null, "to": [...]}

		Entire nodes list omitted under a nodeGroup (e.g, scale workers to zero by dropping the nodes key from the group)
		  {"type": "delete", "path": ["nodeGroups", "1", "nodes"], "from": [...], "to": null}

		New node added under a nodeGroup
		  {"type": "create", "path": ["nodeGroups", "1", "nodes", "0"], "from": null, "to": {"hostName": "worker2"}}

		Existing node removed under a nodeGroup
		  {"type": "delete", "path": ["nodeGroups", "1", "nodes", "0"], "from": {"hostName": "worker2"}, "to": null}

		Field updated at the node-level (legacy flat nodes)
		  {"type": "update", "path": ["nodes", "0", "nodeNetwork", "config", "dns-resolver", "config", "server", "0"], "from": "192.10.1.2", "to": "192.10.1.3"}

		Field updated at the node-level (nodeGroups)
		  {"type": "update", "path": ["nodeGroups", "0", "nodes", "0", "nodeNetwork", "config", "dns-resolver", "config", "server", "0"], "from": "192.10.1.2", "to": "192.10.1.3"}

		Node swap (hostName changed in-place)
		  {"type": "update", "path": ["nodeGroups", "0", "nodes", "1", "hostName"], "from": "worker1", "to": "worker2"}
		*/

		// Check if the path matches any ignored fields
		if matchesAnyPattern(diff.Path, ignoredFields) {
			// Ignored field; skip
			continue
		}

		slog.Default().Info("ClusterInstance input diff",
			slog.String("type", diff.Type),
			slog.String("path", strings.Join(diff.Path, ".")),
			slog.Any("from", diff.From),
			slog.Any("to", diff.To))

		// Check if the path matches any allowed fields
		if matchesAnyPattern(diff.Path, allowedFields) {
			// Allowed field; skip
			continue
		}

		// Scaling: adding or removing an entire nodeGroup entry.
		// Path like ["nodeGroups", "1"].
		if len(diff.Path) == 2 && diff.Path[0] == constants.ClusterInstanceNodeGroupsKey {
			scalingNodes = append(scalingNodes, strings.Join(diff.Path, "."))
			continue
		}

		// Scaling: adding or removing an entire nodes list or single node under a nodeGroup.
		// Path like ["nodeGroups", "1", "nodes"] or ["nodeGroups", "1", "nodes", "0"].
		if (len(diff.Path) == 3 || len(diff.Path) == 4) &&
			diff.Path[0] == constants.ClusterInstanceNodeGroupsKey &&
			diff.Path[2] == constants.ClusterInstanceNodesKey {
			scalingNodes = append(scalingNodes, strings.Join(diff.Path, "."))
			continue
		}

		// Scaling: any change under a node whose hostName was swapped in-place.
		// Path like ["nodeGroups", "0", "nodes", "1", "hostName"].
		if len(diff.Path) > 4 && diff.Path[0] == constants.ClusterInstanceNodeGroupsKey &&
			diff.Path[2] == constants.ClusterInstanceNodesKey {
			parentKey := fmt.Sprintf("%s.%s.%s.%s",
				constants.ClusterInstanceNodeGroupsKey, diff.Path[1],
				constants.ClusterInstanceNodesKey, diff.Path[3])
			if swappedNodeKeys[parentKey] {
				if !slices.Contains(scalingNodes, parentKey) {
					scalingNodes = append(scalingNodes, parentKey)
				}
				continue
			}
		}

		updatedFields = append(updatedFields, strings.Join(diff.Path, "."))
	}

	return updatedFields, scalingNodes, nil
}

// matchesPattern checks if the path matches the pattern
func matchesPattern(path, pattern []string) bool {
	if len(path) < len(pattern) {
		return false
	}

	for i, p := range pattern {
		if p == "*" {
			// Wildcard matches any single element
			continue
		}
		if path[i] != p {
			return false
		}
	}

	return true
}

// matchesAnyPattern checks if the given path matches any pattern in the provided list.
func matchesAnyPattern(path []string, patterns [][]string) bool {
	for _, pattern := range patterns {
		if matchesPattern(path, pattern) {
			return true
		}
	}
	return false
}
