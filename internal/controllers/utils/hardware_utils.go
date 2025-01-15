package utils

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// getBootInterfaceLabel extracts the boot interface label from the NodePool annotations
func getBootInterfaceLabel(nodePool *hwv1alpha1.NodePool) (string, error) {
	// Get the annotations from the NodePool
	annotation := nodePool.GetAnnotations()
	if annotation == nil {
		return "", fmt.Errorf("annotations are missing from nodePool %s in namespace %s", nodePool.Name, nodePool.Namespace)
	}

	// Ensure the boot interface label annotation exists and is not empty
	bootIfaceLabel, exists := annotation[HwTemplateBootIfaceLabel]
	if !exists || bootIfaceLabel == "" {
		return "", fmt.Errorf("%s annotation is missing or empty from nodePool %s in namespace %s",
			HwTemplateBootIfaceLabel, nodePool.Name, nodePool.Namespace)
	}
	return bootIfaceLabel, nil
}

// GetBootMacAddress selects the boot interface based on label and return the interface MAC address
func GetBootMacAddress(interfaces []*hwv1alpha1.Interface, nodePool *hwv1alpha1.NodePool) (string, error) {
	// Get the boot interface label from annotation
	bootIfaceLabel, err := getBootInterfaceLabel(nodePool)
	if err != nil {
		return "", fmt.Errorf("error getting boot interface label: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Label == bootIfaceLabel {
			return iface.MACAddress, nil
		}
	}
	return "", fmt.Errorf("no boot interface found; missing interface with label %q", bootIfaceLabel)
}

// CollectNodeDetails collects BMC and node interfaces details
func CollectNodeDetails(ctx context.Context, c client.Client,
	nodePool *hwv1alpha1.NodePool) (map[string][]NodeInfo, error) {

	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]NodeInfo)

	for _, nodeName := range nodePool.Status.Properties.NodeNames {
		node := &hwv1alpha1.Node{}
		exists, err := DoesK8SResourceExist(ctx, c, nodeName, nodePool.Namespace, node)
		if err != nil {
			return nil, fmt.Errorf("failed to get the Node object %s in namespace %s: %w",
				nodeName, nodePool.Namespace, err)
		}
		if !exists {
			return nil, fmt.Errorf("the Node object %s in namespace %s does not exist: %w",
				nodeName, nodePool.Namespace, err)
		}
		// Verify the node object is generated from the expected pool
		if node.Spec.NodePool != nodePool.GetName() {
			return nil, fmt.Errorf("the Node object %s in namespace %s is not from the expected NodePool : %w",
				nodeName, nodePool.Namespace, err)
		}

		if node.Status.BMC == nil {
			return nil, fmt.Errorf("the Node %s status in namespace %s does not have BMC details",
				nodeName, nodePool.Namespace)
		}
		// Store the nodeInfo per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], NodeInfo{
			BmcAddress:     node.Status.BMC.Address,
			BmcCredentials: node.Status.BMC.CredentialsName,
			NodeName:       node.Name,
			Interfaces:     node.Status.Interfaces,
		})
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
	nodePool *hwv1alpha1.NodePool) error {

	for _, nodeInfos := range hwNodes {
		for _, node := range nodeInfos {
			err := copyHwMgrPluginBMCSecret(ctx, c, node.BmcCredentials, nodePool.GetNamespace(), nodePool.GetName())
			if err != nil {
				return fmt.Errorf("copy BMC secret %s from the plugin namespace %s to the cluster namespace%s failed: %w",
					node.BmcCredentials, nodePool.GetNamespace(), nodePool.GetName(), err)
			}
		}
	}
	return nil
}

// UpdateNodeStatusWithHostname updates the Node status with the hostname after BMC information has been assigned.
func UpdateNodeStatusWithHostname(ctx context.Context, c client.Client, nodeName, hostname, namespace string) error {
	node := &hwv1alpha1.Node{}
	exists, err := DoesK8SResourceExist(ctx, c, nodeName, namespace, node)
	if err != nil || !exists {
		return fmt.Errorf("failed to get the Node object %s in namespace %s: %w, exists %v", nodeName, namespace, err, exists)
	}

	node.Status.Hostname = hostname
	err = c.Status().Update(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to update the Node object %s in namespace %s: %w", nodeName, namespace, err)
	}
	return nil
}

// CreateHwMgrPluginNamespace creates the namespace of the hardware manager plugin
// where the node pools resource resides
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
func AssignMacAddress(clusterInput map[string]any, hwInterfacess []*hwv1alpha1.Interface,
	nodeSpec *siteconfig.NodeSpec) error {

	nodesInput, ok := clusterInput["nodes"].([]any)
	if !ok {
		return fmt.Errorf("unexpected: invalid nodes slice from the cluster input data")
	}
	// Iterate over each interface in the node specification to assign MAC addresses.
OuterLoop:
	for i, iface := range nodeSpec.NodeNetwork.Interfaces {
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
				name, nameOk := intf["name"]
				if !labelOk || !nameOk {
					return fmt.Errorf("interface map from the cluster input is missing 'label' or 'name' key")
				}
				for _, nodeIface := range hwInterfacess {
					if nodeIface.Label == label && iface.Name == name {
						nodeSpec.NodeNetwork.Interfaces[i].MacAddress = nodeIface.MACAddress
						macAddressAssigned = true
						continue OuterLoop
					}
				}
			}
		}
		if !macAddressAssigned {
			return fmt.Errorf("mac address not assigned for interface %s, node name %s", iface.Name, nodeSpec.HostName)
		}
	}
	return nil
}

// HandleHardwareTimeout checks for provisioning or configuration timeout
func HandleHardwareTimeout(
	condition hwv1alpha1.ConditionType,
	provisioningStartTime metav1.Time,
	configurationStartTime metav1.Time,
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

// CompareHardwareTemplateWithNodePool checks if there are any changes in the hardware template resource
func CompareHardwareTemplateWithNodePool(hardwareTemplate *hwv1alpha1.HardwareTemplate, nodePool *hwv1alpha1.NodePool) (bool, error) {

	changesDetected := false

	// Check for changes in hwMgrId
	if hardwareTemplate.Spec.HwMgrId != nodePool.Spec.HwMgrId {
		return true, fmt.Errorf("unallowed change detected in '%s': Hardware Template has %s, but NodePool has %s",
			HwTemplatePluginMgr, hardwareTemplate.Spec.HwMgrId, nodePool.Spec.HwMgrId)
	}

	// Check for changes in bootInterfaceLabel
	bootIfaceLabel, err := getBootInterfaceLabel(nodePool)
	if err != nil {
		return false, err
	}
	if hardwareTemplate.Spec.BootInterfaceLabel != bootIfaceLabel {
		return true, fmt.Errorf("unallowed change detected in '%s': Hardware Template has %s, but NodePool has %s",
			HwTemplateBootIfaceLabel, hardwareTemplate.Spec.BootInterfaceLabel, bootIfaceLabel)
	}

	// Check each group, allowing only hwProfile to be changed
	for _, specNodeGroup := range nodePool.Spec.NodeGroup {
		var found bool
		for _, ng := range hardwareTemplate.Spec.NodePoolData {

			if specNodeGroup.NodePoolData.Name == ng.Name {
				found = true

				// Check for changes in HwProfile
				if specNodeGroup.NodePoolData.HwProfile != ng.HwProfile {
					changesDetected = true
				}
				break
			}
		}

		// If no match was found for the current specNodeGroup, return an error
		if !found {
			return true, fmt.Errorf("node group %s found in NodePool spec but not in Hardware Template", specNodeGroup.NodePoolData.Name)
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

// GetRoleToGroupNameMap creates a mapping of Role to Group Name from NodePool
func GetRoleToGroupNameMap(nodePool *hwv1alpha1.NodePool) map[string]string {
	roleToNodeGroupName := make(map[string]string)
	for _, nodeGroup := range nodePool.Spec.NodeGroup {

		if _, exists := roleToNodeGroupName[nodeGroup.NodePoolData.Role]; !exists {
			roleToNodeGroupName[nodeGroup.NodePoolData.Role] = nodeGroup.NodePoolData.Name
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

// NewNodeGroup populates NodeGroup
func NewNodeGroup(group hwv1alpha1.NodePoolData, roleCounts map[string]int) hwv1alpha1.NodeGroup {
	var nodeGroup hwv1alpha1.NodeGroup

	// Populate embedded NodePoolData fields
	nodeGroup.NodePoolData = group

	// Assign size if available in roleCounts
	if count, ok := roleCounts[group.Role]; ok {
		nodeGroup.Size = count
	}

	return nodeGroup
}

// SetNodePoolAnnotations sets annotations on the NodePool
func SetNodePoolAnnotations(nodePool *hwv1alpha1.NodePool, name, value string) {
	annotations := nodePool.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[name] = value
	nodePool.SetAnnotations(annotations)
}

// SetNodePoolLabels sets labels on the NodePool
func SetNodePoolLabels(nodePool *hwv1alpha1.NodePool, label, value string) {
	labels := nodePool.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[label] = value
	nodePool.SetLabels(labels)
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
