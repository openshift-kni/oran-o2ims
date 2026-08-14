/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// ExtractSchemaRequired extracts the required field of a subschema
func ExtractSchemaRequired(mainSchema []byte) (required []string, err error) {
	requireListAny, err := provisioningv1alpha1.ExtractMatchingInput(mainSchema, requiredString)
	if err != nil {
		return required, fmt.Errorf("could not extract the 'required' section of schema: %w", err)
	}
	requiredAny, ok := requireListAny.([]any)
	if !ok {
		return required, fmt.Errorf("could not cast 'required' section as []any")
	}
	for _, item := range requiredAny {
		itemString, ok := item.(string)
		if !ok {
			return required, fmt.Errorf(`could not cast 'required' section item as a string`)
		}
		required = append(required, itemString)
	}
	return required, nil
}

// ExtractTimeoutFromConfigMap extracts the timeout config from the ConfigMap by key if exits.
// converting it from duration string to time.Duration. Returns an error if the value is not a
// valid duration string.
func ExtractTimeoutFromConfigMap(cm *corev1.ConfigMap, key string) (time.Duration, error) {
	if timeoutStr, err := GetConfigMapField(cm, key); err == nil {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return 0, typederrors.NewInputError("the value of key %s from ConfigMap %s is not a valid duration string: %v", key, cm.GetName(), err)
		}
		return timeout, nil
	}

	return 0, nil
}

// TimeoutExceeded returns true if it's been more time than the timeout configuration.
func TimeoutExceeded(startTime time.Time, timeout time.Duration) bool {
	return time.Since(startTime) > timeout
}

// ClusterIsReadyForPolicyConfig checks if a cluster is ready for policy configuration
// by looking at its availability, joined status and hub acceptance.
func ClusterIsReadyForPolicyConfig(
	ctx context.Context, c client.Client, clusterInstanceName string) (bool, error) {
	// Check if the managed cluster is available. If not, return.
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := DoesK8SResourceExist(
		ctx, c, clusterInstanceName, "", managedCluster)
	if err != nil {
		return false, fmt.Errorf("failed to check if managed cluster exists: %w", err)
	}
	if !managedClusterExists {
		return false, nil
	}

	available := false
	hubAccepted := false
	joined := false

	availableCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionAvailable)
	if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
		available = true
	}

	acceptedCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionHubAccepted)
	if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
		hubAccepted = true
	}

	joinedCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionJoined)
	if joinedCondition != nil && joinedCondition.Status == metav1.ConditionTrue {
		joined = true
	}

	return available && hubAccepted && joined, nil
}

// ValidateDefaultInterfaces verifies that each interface has a specified label field,
// as labels are not part of the ClusterInstance structure by default.
func ValidateDefaultInterfaces(data map[string]any, entriesKey string) error {
	entries, ok := data[entriesKey].([]any)
	if ok {
		for _, entry := range entries {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected: invalid %s data structure", entriesKey)
			}
			interfaces := getInterfaces(entryMap)
			if interfaces == nil {
				return fmt.Errorf("failed to extract the interfaces from the node map")
			}
			for _, intf := range interfaces {
				value, exists := intf["label"]
				if !exists {
					return fmt.Errorf("'label' is missing for interface: %s", intf["name"])
				}
				if value == "" {
					return fmt.Errorf("'label' is empty for interface: %s", intf["name"])
				}
			}
		}
	}
	return nil
}

// RemoveLabelFromInterfaces removes the label property for each interface as the label
// property is not part of the ClusterInstance schema.
func RemoveLabelFromInterfaces(data map[string]any, entriesKey string) error {
	entries, ok := data[entriesKey].([]any)
	if ok {
		for _, entry := range entries {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected: invalid %s data structure", entriesKey)
			}
			interfaces := getInterfaces(entryMap)
			if interfaces == nil {
				return fmt.Errorf("failed to extract the interfaces from the node map")
			}
			for _, intf := range interfaces {
				delete(intf, "label")
			}
		}
	}
	return nil
}

// removeRequiredFromSchema removes all the required properties from a map.
func removeRequiredFromSchema(schema map[string]any) {
	// Check if the current schema level has "properties" defined.
	if properties, hasProperties := schema["properties"]; hasProperties {
		delete(schema, "required")

		// Recurse into each property defined under "properties"
		if propsMap, ok := properties.(map[string]any); ok {
			for _, propValue := range propsMap {
				if propSchema, ok := propValue.(map[string]any); ok {
					removeRequiredFromSchema(propSchema)
				}
			}
		}
	}

	// Recurse into each property defined under "items"
	if items, hasItems := schema["items"]; hasItems {
		if itemSchema, ok := items.(map[string]any); ok {
			removeRequiredFromSchema(itemSchema)
		}
	}
}

// ValidateNodeGroupsNames validates nodeGroups entries: unique non-empty names,
// optional existence in allowedGroupNames, and rejects nested nodes.
func ValidateNodeGroupsNames(
	data map[string]any, allowedGroupNames map[string]struct{}) error {
	groups, ok := data[constants.ClusterInstanceNodeGroupsKey].([]any)
	if !ok {
		return fmt.Errorf("%q must be an array", constants.ClusterInstanceNodeGroupsKey)
	}

	seen := map[string]struct{}{}
	for i, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d] is not an object", constants.ClusterInstanceNodeGroupsKey, i)
		}
		if _, hasNodes := groupMap[constants.ClusterInstanceNodesKey]; hasNodes {
			return fmt.Errorf(
				"%s[%d] must omit %q; per-host identity belongs only in the ProvisioningRequest",
				constants.ClusterInstanceNodeGroupsKey, i, constants.ClusterInstanceNodesKey)
		}
		name, _ := groupMap["name"].(string)
		if name == "" {
			return fmt.Errorf("%s[%d].name must be a non-empty string",
				constants.ClusterInstanceNodeGroupsKey, i)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate %s name %q", constants.ClusterInstanceNodeGroupsKey, name)
		}
		seen[name] = struct{}{}
		if allowedGroupNames != nil {
			if _, exists := allowedGroupNames[name]; !exists {
				return fmt.Errorf(
					"%s name %q does not match any hardware node group in the nodeGroupData",
					constants.ClusterInstanceNodeGroupsKey, name)
			}
		}
	}
	return nil
}

// RemoveNodeGroupNames strips the O-Cloud-only name field from each nodeGroup entry.
func RemoveNodeGroupNames(data map[string]any) error {
	groups, ok := data[constants.ClusterInstanceNodeGroupsKey].([]any)
	if !ok {
		return nil
	}
	for i, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d] is not an object", constants.ClusterInstanceNodeGroupsKey, i)
		}
		delete(groupMap, "name")
	}
	return nil
}

// ValidateConfigmapSchemaAgainstClusterInstanceCRD checks if the data of the ClusterInstance
// default ConfigMap matches the ClusterInstance CRD schema. The entriesKey is "nodes" or "nodeGroups".
// For nodeGroups, the CRD nodes property is renamed to nodeGroups before validation.
// Caller must strip O-Cloud-only fields (interface labels, and nodeGroups[].name) from the
// data before validation.
func ValidateConfigmapSchemaAgainstClusterInstanceCRD(
	ctx context.Context, c client.Client, data map[string]any, entriesKey string) error {
	// Get the ClusterInstance CRD.
	clusterInstanceCrd := &unstructured.Unstructured{}
	clusterInstanceCrd.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	crdName := fmt.Sprintf("%s.%s", ClusterInstanceCrdName, siteconfig.Group)
	err := c.Get(ctx, types.NamespacedName{Name: crdName}, clusterInstanceCrd)
	if err != nil {
		return fmt.Errorf("failed to obtain the %s.%s CRD: %w", ClusterInstanceCrdName, siteconfig.Group, err)
	}

	// Extract the OpenAPIV3Schema.
	openAPIV3Schema := make(map[string]any)
	versions, found, err := unstructured.NestedSlice(clusterInstanceCrd.Object, "spec", "versions")
	if err != nil || !found {
		return fmt.Errorf("failed to obtain the versions of the %s.%s CRD: %w", ClusterInstanceCrdName, siteconfig.Group, err)
	}

	// Find the version that is stored and served.
	for index, version := range versions {
		versionMap, ok := version.(map[string]any)
		if !ok {
			return fmt.Errorf(
				"failed to convert version %d of the %s.%s CRD to map: %w",
				index, ClusterInstanceCrdName, siteconfig.Group, err)
		}
		if versionMap["served"] != true || versionMap["storage"] != true {
			continue
		}
		// Extract the schema.
		openAPIV3Schema, found, err = unstructured.NestedMap(versionMap, "schema", "openAPIV3Schema")
		if err != nil || !found {
			return fmt.Errorf(
				"failed to obtain the openAPIV3Schema from version %d of the %s.%s CRD: %w",
				index, ClusterInstanceCrdName, siteconfig.Group, err)
		}
		break
	}
	if len(openAPIV3Schema) == 0 {
		return fmt.Errorf("no version served & stored in the %s.%s CRD ", ClusterInstanceCrdName, siteconfig.Group)
	}

	// If the properties and spec attributes are missing or the conversion fails, then something is wrong
	// with k8s itself.
	openAPIV3SchemaSpec := openAPIV3Schema["properties"].(map[string]any)["spec"].(map[string]any)

	if entriesKey == constants.ClusterInstanceNodeGroupsKey {
		props, ok := openAPIV3SchemaSpec["properties"].(map[string]any)
		if !ok {
			return fmt.Errorf("%s CRD spec schema is missing properties", ClusterInstanceCrdName)
		}
		nodesSchema, ok := props[constants.ClusterInstanceNodesKey]
		if !ok {
			return fmt.Errorf("%s CRD schema is missing %q property", ClusterInstanceCrdName, constants.ClusterInstanceNodesKey)
		}
		props[constants.ClusterInstanceNodeGroupsKey] = nodesSchema
		delete(props, constants.ClusterInstanceNodesKey)
	}

	// Prepare the data for schema validation.
	// Remove the `required` property as the default ConfigMaps contains only a subset of the ClusterInstance spec.
	removeRequiredFromSchema(openAPIV3SchemaSpec)
	// Disallow unknown properties in the ClusterInstance CRD schema.
	provisioningv1alpha1.DisallowUnknownFieldsInSchema(openAPIV3SchemaSpec)

	err = provisioningv1alpha1.ValidateJsonAgainstJsonSchema(openAPIV3SchemaSpec, data)
	if err != nil {
		return fmt.Errorf("the ConfigMap does not match the ClusterInstance schema: %w", err)
	}
	return nil
}

// GetParentPolicyNameAndNamespace extracts the parent policy name and namespace
// from the child policy name. The child policy name follows the format:
// "<parent_policy_namespace>.<parent_policy_name>". Since the namespace is disallowed
// to contain ".", splitting the string with "." into two substrings is safe.
func GetParentPolicyNameAndNamespace(childPolicyName string) (policyName, policyNamespace string) {
	res := strings.SplitN(childPolicyName, ".", 2)
	return res[1], res[0]
}

// IsParentPolicyInZtpClusterTemplateNs checks whether the parent policy resides
// in the namespace "ztp-<clustertemplate-ns>".
func IsParentPolicyInZtpClusterTemplateNs(policyNamespace, ctNamespace string) bool {
	return policyNamespace == fmt.Sprintf("ztp-%s", ctNamespace)
}

// RootPolicyMatchesClusterTemplate returns true if the root policy annotations include the given
// ClusterTemplate reference string. The annotation value is a comma-separated list
// of ClusterTemplate refs using metadata.name (name.version).
func RootPolicyMatchesClusterTemplate(annotations map[string]string, ctRef string) bool {
	if annotations == nil || ctRef == "" {
		return false
	}
	raw, ok := annotations[CTPolicyTemplatesAnnotation]
	if !ok || raw == "" {
		return false
	}
	// Split comma-separated list and match exact ref after trimming spaces
	for _, item := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(item), ctRef) {
			return true
		}
	}
	return false
}

func ConvertToUnstructured(ci siteconfig.ClusterInstance) (*unstructured.Unstructured, error) {
	objMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ci)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cluster instance to unstructured: %w", err)
	}
	unstructuredObj := &unstructured.Unstructured{Object: objMap}
	return unstructuredObj, nil
}

// PrepareClusterInstanceForServerSideApply prepares a ClusterInstance object for Server-Side Apply (SSA)
// by clearing metadata fields that must not be present in SSA requests and ensuring type information is set.
// This is necessary when reusing an existing ClusterInstance object retrieved from the cluster.
//
// Server-Side Apply requirements:
//   - metadata.managedFields MUST be nil (Kubernetes API requirement)
//   - metadata.resourceVersion and metadata.uid should be empty (not used by SSA)
//   - apiVersion and kind MUST be set for proper object identification
func PrepareClusterInstanceForServerSideApply(ci *siteconfig.ClusterInstance) {
	// Clear server-managed metadata that must not be present in SSA requests
	ci.SetManagedFields(nil)
	ci.SetResourceVersion("")
	ci.SetUID("")

	// Ensure type information is set for proper object identification
	// This is defensive: controller-runtime should set these, but we guarantee they're present
	ci.APIVersion = fmt.Sprintf("%s/%s", siteconfig.Group, siteconfig.Version)
	ci.Kind = siteconfig.ClusterInstanceKind
}

// ExpandNodeGroupsToNodes replaces merged["nodeGroups"] with a flat merged["nodes"]
// slice. For each group, shared fields are deep-copied onto every host, then the
// host overlay is merged on top (interfaces by name). Flattened interface
// addresses (CIDR strings) are mapped into nmstate config. O-cloud-only sugar fields
// (group name, interfaces[].addresses) are stripped.
func ExpandNodeGroupsToNodes(merged map[string]any) error {
	raw, ok := merged[constants.ClusterInstanceNodeGroupsKey]
	if !ok {
		return fmt.Errorf("%q is required for the nodeGroups format", constants.ClusterInstanceNodeGroupsKey)
	}
	groups, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%q must be an array", constants.ClusterInstanceNodeGroupsKey)
	}

	nodeMergeRules := SliceMergeRules{
		"nodeNetwork.interfaces": {
			Key: "name",
			ByKey: ByKeyMergeOptions{
				AllowDuplicateKeys: true, // Per-node interface merge is by interface name (last occurrence wins).
				RejectUnmatchedSrc: true, // PR group nodes may only overlay interfaces that exist on the group defaults.
			},
		},
	}

	flatNodes := []any{}
	for i, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d] is not an object", constants.ClusterInstanceNodeGroupsKey, i)
		}
		groupName, _ := groupMap["name"].(string)

		// Groups with missing or empty nodes are kept in defaults for shared
		// template fields (e.g. a worker group with no hosts yet) and contribute
		// no flat hosts. PR groups may be a subset of defaults groups.
		nodesRaw, hasNodes := groupMap[constants.ClusterInstanceNodesKey]
		if !hasNodes {
			continue
		}
		groupNodes, ok := nodesRaw.([]any)
		if !ok {
			return fmt.Errorf("node group %q nodes must be an array", groupName)
		}
		if len(groupNodes) == 0 {
			continue
		}

		// Drop nodes (already held in groupNodes) and name (O-Cloud-only; not a
		// ClusterInstance node field) before copying the remaining shared fields
		// onto each expanded host.
		delete(groupMap, constants.ClusterInstanceNodesKey)
		delete(groupMap, "name")

		for j, node := range groupNodes {
			nodeMap, ok := node.(map[string]any)
			if !ok {
				return fmt.Errorf("node group %q node[%d] is not an object", groupName, j)
			}

			// Fresh copy of the shared/common fields per node so expanded nodes are independent.
			flatNode := runtime.DeepCopyJSON(groupMap)
			if err := DeepMergeMaps(flatNode, nodeMap, false, nodeMergeRules); err != nil {
				return fmt.Errorf("failed to merge node[%d] onto node group %q: %w", j, groupName, err)
			}

			// Map flattened nodeNetwork.interfaces[].addresses CIDR strings into the matching nmstate entry.
			if err := applyInterfaceAddresses(flatNode); err != nil {
				return fmt.Errorf("node group %q node[%d]: %w", groupName, j, err)
			}

			flatNodes = append(flatNodes, flatNode)
		}
	}

	if len(groups) > 0 && len(flatNodes) == 0 {
		return fmt.Errorf(
			"at least one node is required across %s in clusterInstanceParameters",
			constants.ClusterInstanceNodeGroupsKey)
	}

	delete(merged, constants.ClusterInstanceNodeGroupsKey)
	merged[constants.ClusterInstanceNodesKey] = flatNodes
	return nil
}

// applyInterfaceAddresses maps flattened nodeNetwork.interfaces[].addresses CIDR
// strings into the matching nodeNetwork.config.interfaces[<name>].ipv4/ipv6.address
// nmstate entry, then strips the O-Cloud-only addresses field. An interface with
// addresses but no matching config interface is rejected.
func applyInterfaceAddresses(node map[string]any) error {
	nodeNetwork, ok := node["nodeNetwork"].(map[string]any)
	if !ok {
		return nil
	}
	interfaces, ok := nodeNetwork["interfaces"].([]any)
	if !ok {
		return nil
	}

	// Index nodeNetwork.config.interfaces by name so addresses can be routed to the right entry.
	configByName := map[string]map[string]any{}
	if config, ok := nodeNetwork["config"].(map[string]any); ok {
		if configIfaces, ok := config["interfaces"].([]any); ok {
			for _, ci := range configIfaces {
				ciMap, ok := ci.(map[string]any)
				if !ok {
					continue
				}
				if name, _ := ciMap["name"].(string); name != "" {
					configByName[name] = ciMap
				}
			}
		}
	}

	for _, iface := range interfaces {
		ifaceMap, ok := iface.(map[string]any)
		if !ok {
			continue
		}
		addrRaw, hasAddr := ifaceMap["addresses"]
		if !hasAddr {
			continue
		}

		name, _ := ifaceMap["name"].(string)
		configIface, ok := configByName[name]
		if !ok {
			return fmt.Errorf(
				"interface %q has addresses but no matching entry in nodeNetwork.config.interfaces", name)
		}

		addresses, ok := addrRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("interface %q addresses must be an object", name)
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			cidrsRaw, ok := addresses[family]
			if !ok {
				continue
			}
			cidrs, ok := cidrsRaw.([]any)
			if !ok {
				return fmt.Errorf("interface %q addresses.%s must be an array", name, family)
			}
			nmAddresses := make([]any, 0, len(cidrs))
			for _, c := range cidrs {
				cidr, ok := c.(string)
				if !ok {
					return fmt.Errorf("interface %q addresses.%s entries must be strings", name, family)
				}
				ip, prefix, err := splitCIDR(cidr)
				if err != nil {
					return fmt.Errorf("interface %q addresses.%s %q: %w", name, family, cidr, err)
				}
				nmAddresses = append(nmAddresses, map[string]any{
					"ip":            ip,
					"prefix-length": prefix,
				})
			}

			familyMap, ok := configIface[family].(map[string]any)
			if !ok {
				familyMap = map[string]any{}
				configIface[family] = familyMap
			}
			familyMap["address"] = nmAddresses
		}

		// Strip the O-Cloud-only addresses field.
		delete(ifaceMap, "addresses")
	}
	return nil
}

// splitCIDR splits a CIDR string (e.g. "10.6.34.10/24" or "fd00:1:1::10/64") into
// its host address and prefix length. The host address is preserved as written
// (host bits are not masked), matching the nmstate config.interfaces[].ipvX.address
// shape. The prefix length is returned as float64 to match yaml/json.Unmarshal into
// map[string]any (same as the legacy nodes format). The prefix must be a base-10
// integer string (rejects values like "24.0" or "1e2"). IP address and prefix-range
// validity are left to later nmstate/dry-run checks.
func splitCIDR(cidr string) (ip string, prefix float64, err error) {
	host, prefixStr, found := strings.Cut(cidr, "/")
	if !found || host == "" || prefixStr == "" {
		return "", 0, fmt.Errorf("not a valid CIDR (expected <address>/<prefix>)")
	}
	// ParseUint rejects non-integer forms (e.g. "24.0", "1e2", "-1").
	p, err := strconv.ParseUint(prefixStr, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid prefix length %q", prefixStr)
	}
	return host, float64(p), nil
}
