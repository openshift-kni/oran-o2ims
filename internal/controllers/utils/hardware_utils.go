/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

// ConditionDoesNotExistsErr represents an error when a specific condition is missing
type ConditionDoesNotExistsErr struct {
	ConditionName string
}

// Error implements the error interface for ConditionDoesNotExistsErr,
// returning a formatted error message
func (e *ConditionDoesNotExistsErr) Error() string {
	return fmt.Sprintf("Condition does not exist: %s", e.ConditionName)
}

// IsConditionDoesNotExistsErr checks if the given error is of type ConditionDoesNotExistsErr
func IsConditionDoesNotExistsErr(err error) bool {
	var customErr *ConditionDoesNotExistsErr
	return errors.As(err, &customErr)
}

// GetBootMacAddress selects the boot interface based on label and return the interface MAC address
func GetBootMacAddress(interfaces []*hwmgmtv1alpha1.Interface, bootIfaceLabel string) (string, error) {
	for _, iface := range interfaces {
		if iface.Label == bootIfaceLabel {
			return iface.MACAddress, nil
		}
	}
	return "", fmt.Errorf("no boot interface found; missing interface with label %q", bootIfaceLabel)
}

// GetBareMetalHostForAllocatedNode returns the BareMetalHost labeled with the
// given allocatedNodeID. There is a 1:1 mapping between AllocatedNode and BMH,
// so at most one BMH is expected to carry the label.
func GetBareMetalHostForAllocatedNode(ctx context.Context, c client.Client, allocatedNodeID string) (*metal3v1alpha1.BareMetalHost, error) {
	if allocatedNodeID == "" {
		return nil, nil //nolint:nilnil
	}

	listOpts := []client.ListOption{
		client.MatchingLabels{
			AllocatedNodeLabel: allocatedNodeID,
		},
	}

	bmhList := &metal3v1alpha1.BareMetalHostList{}
	if err := c.List(ctx, bmhList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts for allocated node %s: %w", allocatedNodeID, err)
	}

	if len(bmhList.Items) == 0 {
		return nil, nil //nolint:nilnil
	}

	return &bmhList.Items[0], nil
}

func GetPullSecretName(clusterInstance *unstructured.Unstructured) (string, error) {
	pullSecretName, found, err := unstructured.NestedString(clusterInstance.Object, "spec", "pullSecretRef", "name")
	if err != nil {
		return "", fmt.Errorf("error getting pullSecretRef.name: %w", err)
	}
	if !found {
		return "", fmt.Errorf("pullSecretRef.name not found")
	}
	return pullSecretName, nil
}

// CopyPullSecret copies the pull secrets from the cluster template namespace to the bmh namespace.
func CopyPullSecret(ctx context.Context, c client.Client, ownerObject client.Object, sourceNamespace, pullSecretName string,
	hwNodes map[string][]NodeInfo) error {

	pullSecret := &corev1.Secret{}
	exists, err := DoesK8SResourceExist(ctx, c, pullSecretName, sourceNamespace, pullSecret)
	if err != nil {
		return fmt.Errorf("failed to check existence of pull secret %q in namespace %q: %w", pullSecretName, sourceNamespace, err)
	}
	if !exists {
		return NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
			pullSecretName, sourceNamespace)
	}

	// Extract the namespace from any node (all nodes in the same pool share the same namespace).
	var targetNamespace string
	for _, nodes := range hwNodes {
		if len(nodes) > 0 {
			targetNamespace = nodes[0].HwMgrNodeNs
			break
		}
	}
	if targetNamespace == "" {
		return fmt.Errorf("failed to determine the target namespace for pull secret copy")
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pullSecretName,
			Namespace: targetNamespace,
		},
		Data: pullSecret.Data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	if err := CreateK8sCR(ctx, c, newSecret, ownerObject, UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for PullSecret: %w", err)
	}

	return nil
}

// getInterfaces extracts the interfaces from the node map.
func getInterfaces(nodeMap map[string]interface{}) []map[string]interface{} {
	if nodeNetwork, ok := nodeMap["nodeNetwork"].(map[string]interface{}); ok {
		if !ok {
			return nil
		}
		if interfaces, ok := nodeNetwork["interfaces"].([]any); ok {
			if !ok {
				return nil
			}
			var result []map[string]interface{}
			for _, iface := range interfaces {
				if eth, ok := iface.(map[string]interface{}); ok {
					result = append(result, eth)
				}
			}
			return result
		}
	}
	return nil
}

// AssignMacAddress assigns a MAC address to a node interface based on matching criteria.
// Parameters:
//   - clusterInput: A map containing the merged cluster input data. It should include
//     a "nodes" key with a slice of node data that specifies interface details.
//   - hwInterfaces: A slice of hardware interfaces containing MAC address and label information.
//   - nodeSpec: A reference to the node specification where the MAC address will be assigned.
//
// Returns:
// - error: An error if any unexpected structure or data is encountered; otherwise, nil.
func AssignMacAddress(clusterInput map[string]any, hwInterfaces []*hwmgmtv1alpha1.Interface,
	nodeSpec map[string]interface{}) error {

	nodesInput, ok := clusterInput["nodes"].([]any)
	if !ok {
		return fmt.Errorf("unexpected: invalid nodes slice from the cluster input data")
	}

	// Extract nodeNetwork.interfaces from nodeSpec
	interfacesSlice, found, err := unstructured.NestedSlice(nodeSpec, "nodeNetwork", "interfaces")
	if err != nil {
		return fmt.Errorf("failed to extract nodeNetwork.interfaces: %w", err)
	}
	if !found {
		return fmt.Errorf("nodeNetwork.interfaces not found in nodeSpec")
	}

OuterLoop:
	for i, ifaceRaw := range interfacesSlice {
		ifaceMap, ok := ifaceRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected: interface at index %d is not a valid map", i)
		}

		name, ok := ifaceMap["name"].(string)
		if !ok {
			return fmt.Errorf("missing or invalid 'name' in nodeSpec interface at index %d", i)
		}

		macAddressAssigned := false

		for _, node := range nodesInput {
			nodeMapInput, ok := node.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected: invalid node data structure from the cluster input")
			}

			// Extraction of interfaces from the node map in the cluster input
			interfaces := getInterfaces(nodeMapInput)
			if interfaces == nil {
				return fmt.Errorf("failed to extract the interfaces from the node map")
			}
			// Iterate over extracted interfaces and hardware interfaces to find matches.
			// If an interface in the input data has a label that matches a hardware interface’s label,
			// and its name matches the corresponding name in the node specification, then assign the
			// MAC address from the hardware interface to that interface within the node specification.
			for _, intf := range interfaces {
				// Check that "label" and "name" keys are present in the interface map.
				label, labelOk := intf["label"]
				ifName, nameOk := intf["name"]
				if !labelOk || !nameOk {
					return fmt.Errorf("interface map from the cluster input is missing 'label' or 'name' key")
				}

				for _, nodeIface := range hwInterfaces {
					if nodeIface.Label == label && ifName == name {
						// Assign MAC address
						ifaceMap["macAddress"] = nodeIface.MACAddress
						interfacesSlice[i] = ifaceMap
						macAddressAssigned = true
						continue OuterLoop
					}
				}
			}
		}

		if !macAddressAssigned {
			hostName, _, _ := unstructured.NestedString(nodeSpec, "hostName")
			return fmt.Errorf("mac address not assigned for interface %s, node name %s", name, hostName)
		}
	}

	// Set updated interfaces slice back into nodeSpec
	if err := unstructured.SetNestedSlice(nodeSpec, interfacesSlice, "nodeNetwork", "interfaces"); err != nil {
		return fmt.Errorf("failed to update interfaces in nodeSpec: %w", err)
	}

	return nil
}

// GetStatusMessage returns a status message based on the given condition typ
func GetStatusMessage(condition hwmgmtv1alpha1.ConditionType) string {
	if condition == hwmgmtv1alpha1.Configured {
		return "configuring"
	}
	return "provisioning"
}

// MapHardwareReasonToProvisioningReason converts hardware management condition reasons
// to provisioning request condition reasons with explicit semantic mapping
func MapHardwareReasonToProvisioningReason(hardwareReason string) provisioningv1alpha1.ConditionReason {
	switch hardwareReason {
	case string(hwmgmtv1alpha1.Failed):
		return provisioningv1alpha1.CRconditionReasons.Failed
	case string(hwmgmtv1alpha1.TimedOut):
		return provisioningv1alpha1.CRconditionReasons.TimedOut
	case string(hwmgmtv1alpha1.InProgress):
		return provisioningv1alpha1.CRconditionReasons.InProgress
	case string(hwmgmtv1alpha1.Completed):
		return provisioningv1alpha1.CRconditionReasons.Completed
	case string(hwmgmtv1alpha1.InvalidInput):
		// Hardware InvalidUserInput maps to provisioning Failed
		return provisioningv1alpha1.CRconditionReasons.Failed
	case string(hwmgmtv1alpha1.Unprovisioned):
		// Unexpected unprovisioned state is a failure
		return provisioningv1alpha1.CRconditionReasons.Failed
	case string(hwmgmtv1alpha1.NotInitialized):
		// Initialization failure is a failure
		return provisioningv1alpha1.CRconditionReasons.Failed
	case string(hwmgmtv1alpha1.ConfigUpdate):
		// Configuration update request is in progress
		return provisioningv1alpha1.CRconditionReasons.InProgress
	case string(hwmgmtv1alpha1.ConfigApplied):
		// Configuration applied successfully
		return provisioningv1alpha1.CRconditionReasons.Completed
	default:
		// For unknown hardware reasons, use Unknown
		return provisioningv1alpha1.CRconditionReasons.Unknown
	}
}
