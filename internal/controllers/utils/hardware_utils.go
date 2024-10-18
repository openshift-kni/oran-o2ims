package utils

import (
	"context"
	"fmt"
	"slices"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
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

// FindNodeGroupByRole finds the matching NodeGroup by role
func FindNodeGroupByRole(role string, nodeGroups []hwv1alpha1.NodeGroup) (*hwv1alpha1.NodeGroup, error) {
	for i, group := range nodeGroups {
		if group.Name == role {
			return &nodeGroups[i], nil
		}
	}
	return nil, fmt.Errorf("node group with role %s not found", role)
}

// ProcessClusterNodeGroups extracts the node interfaces per role and count the nodes per group
func ProcessClusterNodeGroups(clusterInstance *siteconfig.ClusterInstance, nodeGroups []hwv1alpha1.NodeGroup, roleCounts map[string]int) error {
	// Map to keep track of processed roles and the corresponding interfaces
	processedRoles := make(map[string][]string)

	for _, node := range clusterInstance.Spec.Nodes {
		// Count the nodes per group
		roleCounts[node.Role]++

		// Find the node group corresponding to this role
		nodeGroup, err := FindNodeGroupByRole(node.Role, nodeGroups)
		if err != nil {
			return fmt.Errorf("could not find node group for role %s: %w", node.Role, err)
		}

		// Get the interface names for the current node
		var currentInterfaces []string
		for _, iface := range node.NodeNetwork.Interfaces {
			currentInterfaces = append(currentInterfaces, iface.Name)
		}

		// If the role has not been processed yet, add the interfaces
		// else check if the interfaces are the same as the first node with this role
		if _, ok := processedRoles[node.Role]; !ok {
			nodeGroup.Interfaces = currentInterfaces
			processedRoles[node.Role] = currentInterfaces
		} else if !slices.Equal(processedRoles[node.Role], currentInterfaces) {
			// Nodes with the same role and hardware profile should have identical interfaces
			return fmt.Errorf("%s has inconsistent interfaces for role %s", node.HostName, node.Role)
		}
	}
	return nil
}

// GetBootMacAddress selects the boot interface based on label and return the interface MAC address
func GetBootMacAddress(interfaces []*hwv1alpha1.Interface, nodePool *hwv1alpha1.NodePool) (string, error) {
	// Get the boot interface label from annotation
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

	for _, iface := range interfaces {
		if iface.Label == bootIfaceLabel {
			return iface.MACAddress, nil
		}
	}
	return "", fmt.Errorf("no boot interface found")
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

// SetCloudManagerGenerationStatus sets the CloudManager's ObservedGeneration on the node pool resource status field
func SetCloudManagerGenerationStatus(ctx context.Context, c client.Client, nodePool *hwv1alpha1.NodePool) error {
	// Get the generated NodePool and its metadata.generation
	exists, err := DoesK8SResourceExist(ctx, c, nodePool.GetName(),
		nodePool.GetNamespace(), nodePool)
	if err != nil {
		return fmt.Errorf("failed to get NodePool %s in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	if !exists {
		return fmt.Errorf("nodePool %s does not exist in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	// We only set ObservedGeneration when the NodePool is first created because we do not update the spec after creation.
	// Once ObservedGeneration is set, no need to update it again.
	if nodePool.Status.CloudManager.ObservedGeneration != 0 {
		// ObservedGeneration is already set, so we do nothing.
		return nil
	}
	// Set ObservedGeneration to the current generation of the resource
	nodePool.Status.CloudManager.ObservedGeneration = nodePool.ObjectMeta.Generation
	err = UpdateK8sCRStatus(ctx, c, nodePool)
	if err != nil {
		return fmt.Errorf("failed to update status for NodePool %s %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	return nil
}

// getInterfaces extracts the interfaces from the node map.
func getInterfaces(nodeMap map[string]interface{}) []map[string]interface{} {
	if nodeNetwork, ok := nodeMap["nodeNetwork"].(map[string]interface{}); ok {
		if interfaces, ok := nodeNetwork["interfaces"].([]any); ok {
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
				return fmt.Errorf("failed to extrace the interfaces from the node map")
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
