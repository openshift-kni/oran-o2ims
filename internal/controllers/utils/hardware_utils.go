/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/client"
)

const (
	bmhNamespaceLabel = "baremetalhost.metal3.io/namespace"
)

var (
	oranHwUtilsLog       = ctrl.Log.WithName("oranHwUtilsLog")
	hwMgrPluginNameSpace string
	once                 sync.Once
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
func GetBootMacAddress(interfaces []*hwv1alpha1.Interface, bootIfaceLabel string) (string, error) {
	for _, iface := range interfaces {
		if iface.Label == bootIfaceLabel {
			return iface.MACAddress, nil
		}
	}
	return "", fmt.Errorf("no boot interface found; missing interface with label %q", bootIfaceLabel)
}

// GetBareMetalHostFromHostname retrieves the BareMetalHost that matches the given hostname
func GetBareMetalHostFromHostname(ctx context.Context, c client.Client, hostname string) (*metal3v1alpha1.BareMetalHost, error) {

	bmhList := &metal3v1alpha1.BareMetalHostList{}
	opts := []client.ListOption{
		client.MatchingFields{"status.hardware.hostname": hostname},
	}

	if err := RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.List(ctx, bmhList, opts...)
	}); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	var matchedBMH *metal3v1alpha1.BareMetalHost
	for _, bmh := range bmhList.Items {
		if bmh.Status.HardwareDetails != nil && bmh.Status.HardwareDetails.Hostname == hostname {
			if matchedBMH != nil {
				return nil, fmt.Errorf("multiple BareMetalHosts found with hostname %s", hostname)
			}
			matchedBMH = bmh.DeepCopy()
		}
	}

	if matchedBMH == nil {
		return nil, fmt.Errorf("no BareMetalHost found with hostname %s", hostname)
	}

	return matchedBMH, nil
}

func GetBareMetalHostForAllocatedNode(ctx context.Context, c client.Client, allocatedNodeID string) *metal3v1alpha1.BareMetalHost {
	if allocatedNodeID == "" {
		return nil
	}

	listOpts := []client.ListOption{
		client.MatchingLabels{
			AllocatedNodeLabel: allocatedNodeID,
		},
	}

	bmhList := &metal3v1alpha1.BareMetalHostList{}
	if err := c.List(ctx, bmhList, listOpts...); err != nil || len(bmhList.Items) == 0 {
		return nil
	}

	// return the first BareMetalHost item
	return &bmhList.Items[0]
}

// CollectNodeDetails collects BMC and node interfaces details
func CollectNodeDetails(ctx context.Context, c client.Client, nodes *[]hwmgrpluginapi.AllocatedNode) (map[string][]NodeInfo, error) {
	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]NodeInfo)
	for _, node := range *nodes {
		if node.Bmc.CredentialsName == "" {
			return nil, fmt.Errorf("the AllocatedNode does not have BMC details")
		}

		interfaces := []*hwv1alpha1.Interface{}
		for _, ifc := range node.Interfaces {
			interfaces = append(interfaces, &hwv1alpha1.Interface{
				Name:       ifc.Name,
				MACAddress: ifc.MacAddress,
				Label:      ifc.Label,
			})
		}

		tmpNode := NodeInfo{
			BmcAddress:     node.Bmc.Address,
			BmcCredentials: node.Bmc.CredentialsName,
			NodeID:         node.Id,
			Interfaces:     interfaces,
		}

		if bmh := GetBareMetalHostForAllocatedNode(ctx, c, node.Id); bmh != nil {
			tmpNode.HwMgrNodeId = bmh.Name
			tmpNode.HwMgrNodeNs = bmh.Namespace
		}

		// Store the nodeInfo per group
		hwNodes[node.GroupName] = append(hwNodes[node.GroupName], tmpNode)
	}

	return hwNodes, nil
}

// copyHwMgrPluginBMCSecret copies the BMC secret from the plugin namespace to the cluster namespace
func copyHwMgrPluginBMCSecret(ctx context.Context, c client.Client, name, sourceNamespace, targetNamespace string) error {

	// if the secret already exists in the target namespace, do nothing
	secret := &corev1.Secret{}
	exists, err := DoesK8SResourceExist(
		ctx, c, name, targetNamespace, secret)
	if err != nil {
		return fmt.Errorf("failed to check if secret exists in namespace %s: %w", targetNamespace, err)
	}
	if exists {
		oranHwUtilsLog.Info("BMC secret already exists in the cluster namespace",
			"name", name, "namespace", targetNamespace)
		return nil
	}

	if err := CopyK8sSecret(ctx, c, name, sourceNamespace, targetNamespace); err != nil {
		return fmt.Errorf("failed to copy Kubernetes secret: %w", err)
	}

	return nil
}

// CopyBMCSecrets copies BMC secrets from the plugin namespace to the cluster namespace.
func CopyBMCSecrets(ctx context.Context, c client.Client, hwNodes map[string][]NodeInfo,
	clusterNamespace string) error {

	sourceNamespace := GetHwMgrPluginNS()
	for _, nodeInfos := range hwNodes {
		for _, node := range nodeInfos {

			// TODO: change the copying of secrets functionality -> create secret with BMC.Username, BMC.Password

			err := copyHwMgrPluginBMCSecret(ctx, c, node.BmcCredentials, sourceNamespace, clusterNamespace)
			if err != nil {
				return fmt.Errorf("copy BMC secret %s from the plugin namespace %s to the cluster namespace %s failed: %w",
					node.BmcCredentials, sourceNamespace, clusterNamespace, err)
			}
		}
	}
	return nil
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

// CreateHwMgrPluginNamespace creates the namespace of the hardware manager plugin
// where the node allocation requests resource resides
func CreateHwMgrPluginNamespace(ctx context.Context, c client.Client, name string) error {

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := CreateK8sCR(ctx, c, namespace, nil, ""); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for namespace %s: %w", namespace, err)
	}

	return nil
}

// HwMgrPluginNamespaceExists checks if the namespace of the hardware manager plugin exists
func HwMgrPluginNamespaceExists(ctx context.Context, c client.Client, name string) (bool, error) {

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	exists, err := DoesK8SResourceExist(ctx, c, name, "", namespace)
	if err != nil {
		return false, fmt.Errorf("failed to check if namespace exists %s: %w", name, err)
	}

	return exists, nil
}

// GetHwMgrPluginNS returns the value of environment variable HWMGR_PLUGIN_NAMESPACE
func GetHwMgrPluginNS() string {
	// Ensure that this code only runs once
	once.Do(func() {
		hwMgrPluginNameSpace = GetEnvOrDefault(HwMgrPluginNameSpace, DefaultPluginNamespace)
	})
	return hwMgrPluginNameSpace
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
func AssignMacAddress(clusterInput map[string]any, hwInterfaces []*hwv1alpha1.Interface,
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

// HandleHardwareTimeout checks for provisioning or configuration timeout
func HandleHardwareTimeout(
	condition hwv1alpha1.ConditionType,
	provisioningStartTime *metav1.Time,
	configurationStartTime *metav1.Time,
	timeout time.Duration,
	currentReason string,
	currentMessage string) (bool, string, string) {

	reason := currentReason
	message := currentMessage
	timedOutOrFailed := false

	// Handle timeout for Provisioned condition
	if condition == hwv1alpha1.Provisioned && TimeoutExceeded(provisioningStartTime.Time, timeout) {
		reason = string(hwv1alpha1.TimedOut)
		message = "Hardware provisioning timed out"
		timedOutOrFailed = true
		return timedOutOrFailed, reason, message
	}

	// Handle timeout for Configured condition
	if condition == hwv1alpha1.Configured && TimeoutExceeded(configurationStartTime.Time, timeout) {
		reason = string(hwv1alpha1.TimedOut)
		message = "Hardware configuration timed out"
		timedOutOrFailed = true
		return timedOutOrFailed, reason, message
	}

	// Return the current reason and message when no timeout occurred
	return timedOutOrFailed, reason, message
}

// CompareHardwareTemplateWithNodeAllocationRequest checks if there are any changes in the hardware template resource
func CompareHardwareTemplateWithNodeAllocationRequest(hardwareTemplate *hwv1alpha1.HardwareTemplate, nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) (bool, error) {

	changesDetected := false

	// Check each group, allowing only hwProfile to be changed
	for _, specNodeGroup := range nodeAllocationRequest.NodeGroup {
		var found bool
		for _, ng := range hardwareTemplate.Spec.NodeGroupData {

			if specNodeGroup.NodeGroupData.Name == ng.Name {
				found = true

				// Check for changes in HwProfile
				if specNodeGroup.NodeGroupData.HwProfile != ng.HwProfile {
					changesDetected = true
				}
				break
			}
		}

		// If no match was found for the current specNodeGroup, return an error
		if !found {
			return true, fmt.Errorf("node group %s found in NodeAllocationRequest but not in Hardware Template", specNodeGroup.NodeGroupData.Name)
		}
	}

	return changesDetected, nil
}

// GetStatusMessage returns a status message based on the given condition typ
func GetStatusMessage(condition hwv1alpha1.ConditionType) string {
	if condition == hwv1alpha1.Configured {
		return "configuring"
	}
	return "provisioning"
}

// GetRoleToGroupNameMap creates a mapping of Role to Group Name from NodeAllocationRequest
func GetRoleToGroupNameMap(nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) map[string]string {
	roleToNodeGroupName := make(map[string]string)
	for _, nodeGroup := range nodeAllocationRequest.NodeGroup {

		if _, exists := roleToNodeGroupName[nodeGroup.NodeGroupData.Role]; !exists {
			roleToNodeGroupName[nodeGroup.NodeGroupData.Role] = nodeGroup.NodeGroupData.Name
		}
	}
	return roleToNodeGroupName
}

// GetHardwareTemplate retrieves the hardware template resource for a given name
func GetHardwareTemplate(ctx context.Context, c client.Client, hwTemplateName string) (*hwv1alpha1.HardwareTemplate, error) {
	hwTemplate := &hwv1alpha1.HardwareTemplate{}

	exists, err := DoesK8SResourceExist(ctx, c, hwTemplateName, InventoryNamespace, hwTemplate)
	if err != nil {
		return hwTemplate, fmt.Errorf("failed to retrieve hardware template resource %s: %w", hwTemplateName, err)
	}
	if !exists {
		return hwTemplate, fmt.Errorf("hardware template resource %s does not exist", hwTemplateName)
	}
	return hwTemplate, nil
}

// GetHardwarePluginRefFromProvisioningRequest retrieves the HardwarePlugin Reference from the ProvisioningRequest.
// The HardwarePluginRef is stored in the HardwareTemplate which can be obtained by fetching the ClusterTemplate
// associated with the given ProvisioningRequest.
func GetHardwarePluginRefFromProvisioningRequest(ctx context.Context, c client.Client,
	pr *provisioningv1alpha1.ProvisioningRequest) (string, error) {

	// Get the ClusterTemplate used by the current ProvisioningRequest.
	clusterTemplate, err := pr.GetClusterTemplateRef(ctx, c)
	if err != nil {
		return "", fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}

	// Get the HardwarePluginRef from the HardwareTemplate
	if clusterTemplate.Spec.Templates.HwTemplate == "" {
		return "", fmt.Errorf("missing HardwareTemplate reference in ClusterTemplate")
	}

	hwTemplate, err := GetHardwareTemplate(ctx, c, clusterTemplate.Spec.Templates.HwTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to get HardwareTemplate: %w", err)
	}

	return hwTemplate.Spec.HardwarePluginRef, nil
}

// GetHardwarePlugin retrieves the HardwarePlugin resource for a given name
func GetHardwarePlugin(ctx context.Context, c client.Client, hwPluginName string) (*hwv1alpha1.HardwarePlugin, error) {
	hwPlugin := &hwv1alpha1.HardwarePlugin{}

	exists, err := DoesK8SResourceExist(ctx, c, hwPluginName, GetHwMgrPluginNS(), hwPlugin)
	if err != nil {
		return hwPlugin, fmt.Errorf("failed to retrieve HardwarePlugin resource %s: %w", hwPluginName, err)
	}
	if !exists {
		return hwPlugin, fmt.Errorf("hardwarePlugin resource %s does not exist", hwPluginName)
	}
	return hwPlugin, nil
}

// GetHardwarePluginFromProvisioningRequest retrieves the HardwarePlugin resource associated with a given ProvisioningRequest resource
func GetHardwarePluginFromProvisioningRequest(ctx context.Context,
	c client.Client,
	pr *provisioningv1alpha1.ProvisioningRequest) (*hwv1alpha1.HardwarePlugin, error) {

	hwpluginRef, err := GetHardwarePluginRefFromProvisioningRequest(ctx, c, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve the HardwarePluginRef from the ProvisioningRequest '%s': %w", pr.Name, err)
	}

	// Get and return the HardwarePlugin CR from the HardwarePluginRef
	hwPlugin, err := GetHardwarePlugin(ctx, c, hwpluginRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get HardwarePlugin: %w", err)
	}

	return hwPlugin, nil
}

// NewNodeGroup populates NodeGroup
func NewNodeGroup(group hwmgrpluginapi.NodeGroupData, roleCounts map[string]int) hwmgrpluginapi.NodeGroup {
	var nodeGroup hwmgrpluginapi.NodeGroup

	// Populate embedded NodeAllocationRequestData fields
	nodeGroup.NodeGroupData = group

	// Assign size if available in roleCounts
	if count, ok := roleCounts[group.Role]; ok {
		nodeGroup.NodeGroupData.Size = count
	}

	return nodeGroup
}

// UpdateHardwareTemplateStatusCondition updates the status condition of the HardwareTemplate resource
func UpdateHardwareTemplateStatusCondition(ctx context.Context, c client.Client, hardwareTemplate *hwv1alpha1.HardwareTemplate,
	conditionType provisioningv1alpha1.ConditionType, conditionReason provisioningv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus, message string) error {

	SetStatusCondition(&hardwareTemplate.Status.Conditions,
		conditionType,
		conditionReason,
		conditionStatus,
		message)

	err := UpdateK8sCRStatus(ctx, c, hardwareTemplate)
	if err != nil {
		return fmt.Errorf("failed to update status for HardwareTemplate %s: %w", hardwareTemplate.Name, err)
	}
	return nil
}

// GetTimeoutFromHWTemplate retrieves the timeout value from the hardware template resource.
// converting it from duration string to time.Duration. Returns an error if the value is not a
// valid duration string.
func GetTimeoutFromHWTemplate(ctx context.Context, c client.Client, name string) (time.Duration, error) {

	hwTemplate, err := GetHardwareTemplate(ctx, c, name)
	if err != nil {
		return 0, err
	}

	if hwTemplate.Spec.HardwareProvisioningTimeout != "" {
		timeout, err := time.ParseDuration(hwTemplate.Spec.HardwareProvisioningTimeout)
		if err != nil {
			errMessage := fmt.Sprintf("the value of HardwareProvisioningTimeout from hardware template %s is not a valid duration string: %v",
				name, err)
			updateErr := UpdateHardwareTemplateStatusCondition(ctx, c, hwTemplate, provisioningv1alpha1.ConditionType(hwv1alpha1.Validation),
				provisioningv1alpha1.ConditionReason(hwv1alpha1.Failed), metav1.ConditionFalse, errMessage)
			if updateErr != nil {
				// nolint: wrapcheck
				return 0, updateErr
			}
			return 0, NewInputError("%s", errMessage)
		}
		return timeout, nil
	}

	return 0, nil
}

// GetBMHNamespace returns the BMH namespace for the given node.
// Check both node label and Spec.HwMgrNodeNs to ensure compatibility until plugin transitions to Spec.HwMgrNodeNs
func GetBMHNamespace(node *hwv1alpha1.AllocatedNode) string {

	if ns, ok := node.ObjectMeta.Labels[bmhNamespaceLabel]; ok {
		return ns
	}
	return node.Spec.HwMgrNodeNs
}

// GetDeployLoopbackHWPlugin returns the value of environment variable DEPLOY_LOOPBACK_HW_PLUGIN
func GetDeployLoopbackHWPlugin() string {
	return GetEnvOrDefault(DeployLoopbackHWPluginEnvVar, DefaultDeployLoopbackHWPlugin)
}

// ShouldDeployLoopbackHWPlugin returns a boolean value indiciating if the Loopback HardwarePlugin should be installed
func ShouldDeployLoopbackHWPlugin() bool {
	return strings.EqualFold(GetDeployLoopbackHWPlugin(), DeployLoopbackHWPluginOk)
}
