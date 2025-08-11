/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

// nolint: wrapcheck
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

// CRD type constants
const (
	CRDTypeProvisioningRequests   = "provisioningrequests"
	CRDTypeNodeAllocationRequests = "nodeallocationrequests"
	CRDTypeAllocatedNodes         = "allocatednodes"
	CRDTypeBareMetalHosts         = "baremetalhosts"
	CRDTypeHostFirmwareComponents = "hostfirmwarecomponents"
	CRDTypeHostFirmwareSettings   = "hostfirmwaresettings"
	CRDTypeInventoryResources     = "inventory-resources"
	CRDTypeInventoryResourcePools = "inventory-resource-pools"
	CRDTypeInventoryNodeClusters  = "inventory-node-clusters"
)

// Common string constants
const (
	StringNone           = "<none>"
	StringUnknown        = "<unknown>"
	StringTrue           = "true"
	StringFalse          = "false"
	StringValid          = "Valid"
	StringChangeDetected = "ChangeDetected"
	StringLocalCluster   = "local-cluster"
	StringAvailable      = "available"
	StringFulfilled      = "fulfilled"
)

type OutputFormatter interface {
	FormatEvent(event WatchEvent) error
}

type TableFormatter struct {
	headersPrinted map[string]bool         // Track headers printed per CRD type
	events         map[string][]WatchEvent // Collect events for sorting
	eventMutex     sync.Mutex
}

type JSONFormatter struct{}

type YAMLFormatter struct{}

func NewOutputFormatter(format string, watchMode bool, refreshInterval int, crdTypes []string, verifyFunc func(WatchEvent) bool) OutputFormatter {
	switch format {
	case "json":
		return &JSONFormatter{}
	case "yaml":
		return &YAMLFormatter{}
	default:
		if watchMode {
			return NewTUIFormatter(refreshInterval, crdTypes, verifyFunc)
		}
		return NewTableFormatter()
	}
}

func NewTableFormatter() *TableFormatter {
	return &TableFormatter{
		headersPrinted: make(map[string]bool),
		events:         make(map[string][]WatchEvent),
	}
}

func (f *TableFormatter) FormatEvent(event WatchEvent) error {
	f.eventMutex.Lock()
	defer f.eventMutex.Unlock()

	// Collect events for later sorting and display
	f.events[event.CRDType] = append(f.events[event.CRDType], event)

	return nil
}

func (f *TableFormatter) FlushEvents() error {
	f.eventMutex.Lock()
	defer f.eventMutex.Unlock()

	// Display events sorted by CRD type
	for crdType, events := range f.events {
		if len(events) == 0 {
			continue
		}

		// Print header if not already printed
		if !f.headersPrinted[crdType] {
			f.printTableHeader(crdType)
			f.headersPrinted[crdType] = true
		}

		// Sort events for this CRD type
		sortedEvents := f.sortEventsByCRDType(crdType, events)

		// Display sorted events
		for _, event := range sortedEvents {
			timestamp := event.Timestamp.Format("15:04:05")
			age := getAge(getTimestamp(event.Object))

			switch event.CRDType {
			case CRDTypeProvisioningRequests:
				if err := f.formatProvisioningRequest(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeNodeAllocationRequests:
				if err := f.formatNodeAllocationRequest(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeAllocatedNodes:
				if err := f.formatAllocatedNode(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeBareMetalHosts:
				if err := f.formatBareMetalHost(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeHostFirmwareComponents:
				if err := f.formatHostFirmwareComponents(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeHostFirmwareSettings:
				if err := f.formatHostFirmwareSettings(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeInventoryResources:
				if err := f.formatInventoryResource(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeInventoryResourcePools:
				if err := f.formatInventoryResourcePool(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			case CRDTypeInventoryNodeClusters:
				if err := f.formatInventoryNodeCluster(timestamp, string(event.Type), age, event.Object); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// sortEventsByCRDType sorts events based on the CRD type's preferred sorting field
func (f *TableFormatter) sortEventsByCRDType(crdType string, events []WatchEvent) []WatchEvent {
	if len(events) == 0 {
		return events
	}

	// Create a copy to avoid modifying the original slice
	sortedEvents := make([]WatchEvent, len(events))
	copy(sortedEvents, events)

	sort.Slice(sortedEvents, func(i, j int) bool {
		return f.compareEvents(crdType, sortedEvents[i], sortedEvents[j])
	})

	return sortedEvents
}

// compareEvents compares two events based on the CRD type's sorting criteria
func (f *TableFormatter) compareEvents(crdType string, a, b WatchEvent) bool {
	switch crdType {
	case CRDTypeProvisioningRequests:
		// Sort by DISPLAYNAME
		displayNameA := f.getProvisioningRequestDisplayName(a.Object)
		displayNameB := f.getProvisioningRequestDisplayName(b.Object)
		return displayNameA < displayNameB
	case CRDTypeNodeAllocationRequests:
		// Sort by CLUSTER-ID
		clusterIdA := f.getNodeAllocationRequestClusterId(a.Object)
		clusterIdB := f.getNodeAllocationRequestClusterId(b.Object)
		return clusterIdA < clusterIdB
	case CRDTypeAllocatedNodes:
		// Sort by name
		accessor1, _ := meta.Accessor(a.Object)
		accessor2, _ := meta.Accessor(b.Object)
		return accessor1.GetName() < accessor2.GetName()
	case CRDTypeBareMetalHosts, CRDTypeHostFirmwareComponents, CRDTypeHostFirmwareSettings:
		// Sort by name
		accessor1, _ := meta.Accessor(a.Object)
		accessor2, _ := meta.Accessor(b.Object)
		return accessor1.GetName() < accessor2.GetName()
	case CRDTypeInventoryResources:
		// Sort by resource ID
		resourceA := f.getInventoryResourceId(a.Object)
		resourceB := f.getInventoryResourceId(b.Object)
		return resourceA < resourceB
	case CRDTypeInventoryResourcePools:
		// Sort by site, then pool name
		siteA := f.getInventoryResourcePoolSite(a.Object)
		siteB := f.getInventoryResourcePoolSite(b.Object)
		if siteA != siteB {
			return siteA < siteB
		}
		poolA := f.getInventoryResourcePoolName(a.Object)
		poolB := f.getInventoryResourcePoolName(b.Object)
		return poolA < poolB
	case CRDTypeInventoryNodeClusters:
		// Sort by node name
		nameA := f.getInventoryNodeClusterName(a.Object)
		nameB := f.getInventoryNodeClusterName(b.Object)
		return nameA < nameB
	default:
		// Default to sorting by name
		accessor1, _ := meta.Accessor(a.Object)
		accessor2, _ := meta.Accessor(b.Object)
		return accessor1.GetName() < accessor2.GetName()
	}
}

// Helper functions to extract sorting fields from objects
func (f *TableFormatter) getProvisioningRequestDisplayName(obj runtime.Object) string {
	if pr, ok := obj.(*provisioningv1alpha1.ProvisioningRequest); ok {
		if pr.Spec.Name != "" {
			return pr.Spec.Name
		}
	}
	// Fallback to object name if displayName is empty
	accessor, _ := meta.Accessor(obj)
	return accessor.GetName()
}

func (f *TableFormatter) getNodeAllocationRequestClusterId(obj runtime.Object) string {
	if nar, ok := obj.(*pluginsv1alpha1.NodeAllocationRequest); ok {
		if nar.Spec.ClusterId != "" {
			return nar.Spec.ClusterId
		}
	}
	return StringNone
}

func (f *TableFormatter) getInventoryResourceId(obj runtime.Object) string {
	if iro, ok := obj.(*InventoryResourceObject); ok {
		return iro.Resource.ResourceID
	}
	return StringNone
}

func (f *TableFormatter) getInventoryResourcePoolSite(obj runtime.Object) string {
	if rpo, ok := obj.(*ResourcePoolObject); ok && rpo != nil {
		// Safe site extraction without calling GetSite()
		if rpo.ResourcePool.Extensions != nil {
			if siteVal, exists := rpo.ResourcePool.Extensions["site"]; exists {
				if siteStr, ok := siteVal.(string); ok && siteStr != "" {
					return siteStr
				}
			}
		}

		// Fallback to parsing name
		if rpo.ResourcePool.Name != "" && strings.Contains(rpo.ResourcePool.Name, "-") {
			parts := strings.Split(rpo.ResourcePool.Name, "-")
			if len(parts) > 1 {
				return parts[0]
			}
		}

		return StringUnknown
	}
	return StringNone
}

func (f *TableFormatter) getInventoryResourcePoolName(obj runtime.Object) string {
	if rpo, ok := obj.(*ResourcePoolObject); ok && rpo != nil {
		// Safe pool name extraction without calling GetPoolName()
		if rpo.ResourcePool.Name != "" {
			return rpo.ResourcePool.Name
		}
		if rpo.ResourcePool.ResourcePoolID != "" {
			return rpo.ResourcePool.ResourcePoolID
		}
		return StringUnknown
	}
	return StringNone
}

func (f *TableFormatter) getInventoryNodeClusterName(obj runtime.Object) string {
	if nco, ok := obj.(*NodeClusterObject); ok && nco != nil {
		if nco.NodeCluster.Name != "" {
			return nco.NodeCluster.Name
		}
		return StringUnknown
	}
	return StringNone
}

func (f *TableFormatter) printTableHeader(crdType string) {
	switch crdType {
	case CRDTypeProvisioningRequests:
		fmt.Printf("%-40s %-20s %-10s %-15s %-20s %-15s %-30s\n",
			"NAME", "DISPLAYNAME", "AGE", "TIME", "EVENT", "PHASE", "DETAILS")
	case CRDTypeNodeAllocationRequests:
		fmt.Printf("%-40s %-20s %-10s %-15s %-20s %-20s %-20s\n",
			"NAME", "CLUSTER-ID", "AGE", "TIME", "EVENT", "PROVISIONING", "DAY2-UPDATE")
	case CRDTypeAllocatedNodes:
		fmt.Printf("%-40s %-30s %-30s %-10s %-15s %-20s %-20s %-20s\n",
			"NAME", "NODE-ALLOC-REQUEST", "HWMGR-NODE-ID", "AGE", "TIME", "EVENT", "PROVISIONING", "DAY2-UPDATE")
	case CRDTypeBareMetalHosts:
		fmt.Printf("%-15s %-40s %-12s %-15s %-10s %-12s %-30s %-20s\n",
			"NS", "BMH", "STATUS", "STATE", "ONLINE", "POWEREDON", "NETDATA", "ERROR")
	case CRDTypeHostFirmwareComponents:
		fmt.Printf("%-40s %-10s %-15s %-10s %-10s\n",
			"HOSTFIRMWARECOMPONENTS", "GEN", "OBSERVED", "VALID", "CHANGE")
	case CRDTypeHostFirmwareSettings:
		fmt.Printf("%-10s %-8s %-15s %-20s %-12s %-12s %-8s %-15s\n",
			"TIME", "EVENT", "AGE", "NAME", "GENERATION", "OBSERVED", "VALID", "CHANGE-DETECTED")
		fmt.Printf("%s\n", strings.Repeat("-", 120))
	case CRDTypeInventoryResources:
		fmt.Printf("%-30s %-20s %-36s %s\n",
			"NAME", "POOL", "RESOURCE-ID", "MODEL")
		fmt.Printf("%s\n", strings.Repeat("-", 88))
	case CRDTypeInventoryResourcePools:
		fmt.Printf("%-20s %-30s %-36s\n",
			"SITE", "POOL", "RESOURCE-POOL-ID")
		fmt.Printf("%s\n", strings.Repeat("-", 88))
	case CRDTypeInventoryNodeClusters:
		fmt.Printf("%-20s %-30s %-36s\n",
			"NODE-NAME", "NODE-CLUSTER-ID", "NODE-CLUSTER-TYPE-ID")
		fmt.Printf("%s\n", strings.Repeat("-", 88))
	default:
		fmt.Printf("%-10s %-8s %-15s %-30s %-20s %-15s\n",
			"TIME", "EVENT", "AGE", "NAME", "NAMESPACE", "KIND")
		fmt.Printf("%s\n", strings.Repeat("-", 100))
	}
}

func (f *TableFormatter) formatProvisioningRequest(timestamp, eventType, age string, obj runtime.Object) error {
	pr, ok := obj.(*provisioningv1alpha1.ProvisioningRequest)
	if !ok {
		return fmt.Errorf("expected ProvisioningRequest, got %T", obj)
	}

	name := pr.ObjectMeta.Name
	displayName := pr.Spec.Name
	if displayName == "" {
		displayName = StringNone
	}

	phase := string(pr.Status.ProvisioningStatus.ProvisioningPhase)
	if phase == "" {
		phase = StringNone
	}

	details := pr.Status.ProvisioningStatus.ProvisioningDetails
	if details == "" {
		details = StringNone
	}

	fmt.Printf("%-8s %-8s %-25s %-25s %-8s %-15s %-30s\n",
		timestamp, eventType, truncate(name, 25), truncate(displayName, 25),
		age, truncate(phase, 15), truncate(details, 30))

	return nil
}

func (f *TableFormatter) formatNodeAllocationRequest(timestamp, eventType, age string, obj runtime.Object) error {
	nar, ok := obj.(*pluginsv1alpha1.NodeAllocationRequest)
	if !ok {
		return fmt.Errorf("expected NodeAllocationRequest, got %T", obj)
	}

	name := nar.ObjectMeta.Name
	clusterId := nar.Spec.ClusterId
	if clusterId == "" {
		clusterId = StringNone
	}

	provisioning := getConditionReason(nar.Status.Conditions, "Provisioned")
	day2Update := getConditionReason(nar.Status.Conditions, "Configured")

	fmt.Printf("%-8s %-8s %-25s %-25s %-8s %-15s %-15s\n",
		timestamp, eventType, truncate(name, 25), truncate(clusterId, 25),
		age, truncate(provisioning, 15), truncate(day2Update, 15))

	return nil
}

func (f *TableFormatter) formatAllocatedNode(timestamp, eventType, age string, obj runtime.Object) error {
	an, ok := obj.(*pluginsv1alpha1.AllocatedNode)
	if !ok {
		return fmt.Errorf("expected AllocatedNode, got %T", obj)
	}

	name := an.ObjectMeta.Name
	nodeAllocRequest := an.Spec.NodeAllocationRequest
	if nodeAllocRequest == "" {
		nodeAllocRequest = StringNone
	}

	hwMgrNodeId := an.Spec.HwMgrNodeId
	if hwMgrNodeId == "" {
		hwMgrNodeId = StringNone
	}

	provisioning := getConditionReason(an.Status.Conditions, "Provisioned")
	day2Update := getConditionReason(an.Status.Conditions, "Configured")

	fmt.Printf("%-8s %-8s %-25s %-25s %-15s %-8s %-15s %-15s\n",
		timestamp, eventType, truncate(name, 25), truncate(nodeAllocRequest, 25),
		truncate(hwMgrNodeId, 15), age, truncate(provisioning, 15), truncate(day2Update, 15))

	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatBareMetalHost(timestamp, eventType, age string, obj runtime.Object) error {
	bmh, ok := obj.(*metal3v1alpha1.BareMetalHost)
	if !ok {
		return fmt.Errorf("expected BareMetalHost, got %T", obj)
	}

	namespace := bmh.ObjectMeta.Namespace
	name := bmh.ObjectMeta.Name
	status := string(bmh.Status.OperationalStatus)
	if status == "" {
		status = StringNone
	}

	state := string(bmh.Status.Provisioning.State)
	if state == "" {
		state = StringNone
	}

	online := StringFalse
	if bmh.Spec.Online {
		online = StringTrue
	}

	poweredOn := StringFalse
	if bmh.Status.PoweredOn {
		poweredOn = StringTrue
	}

	netData := bmh.Spec.PreprovisioningNetworkDataName
	if netData == "" {
		netData = StringNone
	}

	errorType := string(bmh.Status.ErrorType)
	if errorType == "" {
		errorType = StringNone
	}

	fmt.Printf("%-15s %-40s %-12s %-15s %-10s %-12s %-30s %-20s\n",
		namespace, name, status, state, online, poweredOn, netData, errorType)

	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatHostFirmwareComponents(timestamp, eventType, age string, obj runtime.Object) error {
	hfc, ok := obj.(*metal3v1alpha1.HostFirmwareComponents)
	if !ok {
		return fmt.Errorf("expected HostFirmwareComponents, got %T", obj)
	}

	name := hfc.ObjectMeta.Name
	generation := fmt.Sprintf("%d", hfc.ObjectMeta.Generation)

	// Get all observedGeneration values from all conditions
	var observedGens []string
	for _, condition := range hfc.Status.Conditions {
		observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
	}
	observedGeneration := StringNone
	if len(observedGens) > 0 {
		observedGeneration = strings.Join(observedGens, ",")
	}

	// Get Valid condition status
	validStatus := StringNone
	for _, condition := range hfc.Status.Conditions {
		if condition.Type == StringValid {
			validStatus = string(condition.Status)
			break
		}
	}

	// Get ChangeDetected condition status
	changeStatus := StringNone
	for _, condition := range hfc.Status.Conditions {
		if condition.Type == StringChangeDetected {
			changeStatus = string(condition.Status)
			break
		}
	}

	fmt.Printf("%-40s %-10s %-15s %-10s %-10s\n",
		name, generation, observedGeneration, validStatus, changeStatus)

	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatHostFirmwareSettings(timestamp, eventType, age string, obj runtime.Object) error {
	hfs, ok := obj.(*metal3v1alpha1.HostFirmwareSettings)
	if !ok {
		return fmt.Errorf("expected HostFirmwareSettings, got %T", obj)
	}

	name := hfs.ObjectMeta.Name
	generation := fmt.Sprintf("%d", hfs.ObjectMeta.Generation)

	// Get all observedGeneration values from all conditions
	var observedGens []string
	for _, condition := range hfs.Status.Conditions {
		observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
	}
	observedGeneration := StringNone
	if len(observedGens) > 0 {
		observedGeneration = strings.Join(observedGens, ",")
	}

	// Get Valid condition status
	validStatus := StringNone
	for _, condition := range hfs.Status.Conditions {
		if condition.Type == StringValid {
			validStatus = string(condition.Status)
			break
		}
	}

	// Get ChangeDetected condition status
	changeStatus := StringNone
	for _, condition := range hfs.Status.Conditions {
		if condition.Type == StringChangeDetected {
			changeStatus = string(condition.Status)
			break
		}
	}

	fmt.Printf("%-40s %-10s %-15s %-10s %-10s\n",
		name, generation, observedGeneration, validStatus, changeStatus)

	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatInventoryResource(timestamp, eventType, age string, obj runtime.Object) error {
	if iro, ok := obj.(*InventoryResourceObject); ok {
		resource := iro.Resource

		// Extract Name from extensions.labels."resourceselector.clcm.openshift.io/server-id"
		name := StringUnknown
		if resource.Extensions != nil {
			if labelsVal, exists := resource.Extensions["labels"]; exists {
				if labelsMap, ok := labelsVal.(map[string]interface{}); ok {
					if nameVal, exists := labelsMap["resourceselector.clcm.openshift.io/server-id"]; exists {
						if nameStr, ok := nameVal.(string); ok && nameStr != "" {
							name = nameStr
						}
					}
				}
			}
		}
		if name == StringUnknown && resource.Description != "" {
			name = resource.Description
		}

		// Extract Pool from extensions.labels."resources.clcm.openshift.io/resourcePoolId"
		pool := StringUnknown
		if resource.Extensions != nil {
			if labelsVal, exists := resource.Extensions["labels"]; exists {
				if labelsMap, ok := labelsVal.(map[string]interface{}); ok {
					if poolVal, exists := labelsMap["resources.clcm.openshift.io/resourcePoolId"]; exists {
						if poolStr, ok := poolVal.(string); ok && poolStr != "" {
							pool = poolStr
						}
					}
				}
			}
		}

		// Extract Model from extensions
		model := StringUnknown
		if resource.Extensions != nil {
			if modelVal, exists := resource.Extensions["model"]; exists {
				if modelStr, ok := modelVal.(string); ok && modelStr != "" {
					model = modelStr
				}
			}
		}

		fmt.Printf("%-30s %-20s %-36s %s\n",
			truncate(name, 30),
			truncate(pool, 20),
			truncate(resource.ResourceID, 36),
			model)
	}
	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatInventoryResourcePool(timestamp, eventType, age string, obj runtime.Object) error {
	if rpo, ok := obj.(*ResourcePoolObject); ok && rpo != nil {
		// Add comprehensive safety checks before accessing fields
		site := StringUnknown
		poolName := StringUnknown
		resourcePoolID := ""

		// Safe field access
		resourcePoolID = rpo.ResourcePool.ResourcePoolID

		// Safe pool name extraction
		if rpo.ResourcePool.Name != "" {
			poolName = rpo.ResourcePool.Name
		} else if resourcePoolID != "" {
			poolName = resourcePoolID
		}

		// Safe site extraction
		if rpo.ResourcePool.Extensions != nil {
			if siteVal, exists := rpo.ResourcePool.Extensions["site"]; exists {
				if siteStr, ok := siteVal.(string); ok && siteStr != "" {
					site = siteStr
				}
			}
		}

		// Fallback to parsing name if no site found
		if site == StringUnknown && rpo.ResourcePool.Name != "" && strings.Contains(rpo.ResourcePool.Name, "-") {
			parts := strings.Split(rpo.ResourcePool.Name, "-")
			if len(parts) > 1 {
				site = parts[0]
			}
		}

		fmt.Printf("%-20s %-30s %-36s\n",
			truncate(site, 20),
			truncate(poolName, 30),
			truncate(resourcePoolID, 36))
	}
	return nil
}

//nolint:unparam // timestamp parameter required for interface consistency
func (f *TableFormatter) formatInventoryNodeCluster(timestamp, eventType, age string, obj runtime.Object) error {
	if nco, ok := obj.(*NodeClusterObject); ok && nco != nil {
		// Add comprehensive safety checks before accessing fields
		nodeName := StringUnknown
		nodeClusterID := StringUnknown
		nodeClusterTypeID := StringUnknown

		// Safe field access
		if nco.NodeCluster.Name != "" {
			nodeName = nco.NodeCluster.Name
		}
		if nco.NodeCluster.NodeClusterID != "" {
			nodeClusterID = nco.NodeCluster.NodeClusterID
		}
		if nco.NodeCluster.NodeClusterTypeID != "" {
			nodeClusterTypeID = nco.NodeCluster.NodeClusterTypeID
		}

		fmt.Printf("%-20s %-30s %-36s\n",
			truncate(nodeName, 20),
			truncate(nodeClusterID, 30),
			truncate(nodeClusterTypeID, 36))
	}
	return nil
}

func (f *JSONFormatter) FormatEvent(event WatchEvent) error {
	eventData := map[string]interface{}{
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"type":      string(event.Type),
		"crdType":   event.CRDType,
		"object":    event.Object,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(eventData)
}

func (f *YAMLFormatter) FormatEvent(event WatchEvent) error {
	eventData := map[string]interface{}{
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"type":      string(event.Type),
		"crdType":   event.CRDType,
		"object":    event.Object,
	}

	yamlBytes, err := yaml.Marshal(eventData)
	if err != nil {
		return fmt.Errorf("failed to marshal to YAML: %w", err)
	}

	fmt.Print(string(yamlBytes))
	fmt.Println("---")
	return nil
}

// Helper functions

func getConditionReason(conditions []metav1.Condition, conditionType string) string {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Reason
		}
	}
	return StringNone
}

func getAge(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return StringUnknown
	}

	duration := time.Since(timestamp.Time)

	// If duration is negative (timestamp is in the future), return empty string
	if duration < 0 {
		return ""
	}

	switch {
	case duration.Hours() >= 24:
		return fmt.Sprintf("%.0fd", duration.Hours()/24)
	case duration.Hours() >= 1:
		return fmt.Sprintf("%.0fh", duration.Hours())
	case duration.Minutes() >= 1:
		return fmt.Sprintf("%.0fm", duration.Minutes())
	default:
		return fmt.Sprintf("%.0fs", duration.Seconds())
	}
}

func getTimestamp(obj runtime.Object) metav1.Time {
	if accessor, err := meta.Accessor(obj); err == nil {
		return accessor.GetCreationTimestamp()
	}
	return metav1.NewTime(time.Now()) // Fallback to current time if accessor is not available
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
