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
	controller "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
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
	StringYes            = "yes"
	StringNo             = "no"
	StringValid          = "Valid"
	StringChangeDetected = "ChangeDetected"
	SidebarCharUnicode   = "│"
	SidebarCharASCII     = "|"
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
	crdTypes       []string // List of CRD types being watched
	useUnicode     bool     // Whether to use Unicode characters (default true)
}

type JSONFormatter struct{}

type YAMLFormatter struct{}

func NewOutputFormatter(format string, watchMode bool, refreshInterval int, crdTypes []string, verifyFunc func(WatchEvent) bool, useASCII bool) OutputFormatter {
	switch format {
	case "json":
		return &JSONFormatter{}
	case "yaml":
		return &YAMLFormatter{}
	default:
		if watchMode {
			return NewTUIFormatter(refreshInterval, crdTypes, verifyFunc, !useASCII)
		}
		tableFormatter := NewTableFormatter(crdTypes)
		tableFormatter.useUnicode = !useASCII
		return tableFormatter
	}
}

func NewTableFormatter(crdTypes []string) *TableFormatter {
	return &TableFormatter{
		headersPrinted: make(map[string]bool),
		events:         make(map[string][]WatchEvent),
		crdTypes:       crdTypes,
		useUnicode:     true, // Default to Unicode characters
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

	// Print main header like TUIFormatter
	f.printMainHeader()

	// Get ordered CRD types based on preferred order
	orderedTypes := f.getOrderedCRDTypes()

	// Process each CRD type in order
	for i, crdType := range orderedTypes {
		events := f.events[crdType]

		// Print section header for this CRD type
		f.printSectionHeader(crdType)

		// Calculate dynamic field widths for this CRD type
		widths := f.calculateFieldWidths(crdType, events)

		// Print table header for this CRD type
		f.printTableHeader(crdType, widths)

		if len(events) == 0 {
			// Show empty message when no resources exist
			sidebarChar := SidebarCharUnicode
			if !f.useUnicode {
				sidebarChar = "|"
			}
			fmt.Printf("%s No resources found\n", sidebarChar)
		} else {
			// Sort and display events for this CRD type
			sortedEvents := f.sortEventsByCRDType(crdType, events)

			for _, event := range sortedEvents {
				age := getAge(getTimestamp(event.Object))

				switch event.CRDType {
				case CRDTypeProvisioningRequests:
					if err := f.formatProvisioningRequest(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeNodeAllocationRequests:
					if err := f.formatNodeAllocationRequest(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeAllocatedNodes:
					if err := f.formatAllocatedNode(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeBareMetalHosts:
					if err := f.formatBareMetalHost(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeHostFirmwareComponents:
					if err := f.formatHostFirmwareComponents(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeHostFirmwareSettings:
					if err := f.formatHostFirmwareSettings(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeInventoryResources:
					if err := f.formatInventoryResource(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeInventoryResourcePools:
					if err := f.formatInventoryResourcePool(age, event.Object, widths); err != nil {
						return err
					}
				case CRDTypeInventoryNodeClusters:
					if err := f.formatInventoryNodeCluster(age, event.Object, widths); err != nil {
						return err
					}
				}
			}
		}

		// Add spacing between sections (except for last section)
		if i < len(orderedTypes)-1 {
			fmt.Println()
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
		if accessor1 == nil || accessor2 == nil {
			return false // Maintain consistent ordering when accessors are nil
		}
		return accessor1.GetName() < accessor2.GetName()
	case CRDTypeBareMetalHosts, CRDTypeHostFirmwareComponents, CRDTypeHostFirmwareSettings:
		// Sort by name
		accessor1, _ := meta.Accessor(a.Object)
		accessor2, _ := meta.Accessor(b.Object)
		if accessor1 == nil || accessor2 == nil {
			return false // Maintain consistent ordering when accessors are nil
		}
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
		if accessor1 == nil || accessor2 == nil {
			return false // Maintain consistent ordering when accessors are nil
		}
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
	if accessor != nil {
		return accessor.GetName()
	}
	return ""
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

func (f *TableFormatter) printTableHeader(crdType string, widths FieldWidths) {
	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	switch crdType {
	case CRDTypeProvisioningRequests:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "DISPLAYNAME", widths.Age, "AGE",
			widths.Field2, "PHASE", widths.Field3, "DETAILS")
	case CRDTypeNodeAllocationRequests:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "CLUSTER-ID",
			widths.Field2, "PROVISIONING", widths.Field3, "DAY2-UPDATE")
	case CRDTypeAllocatedNodes:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "NODE-ALLOC-REQUEST",
			widths.Field2, "HWMGR-NODE-ID", widths.Field3, "PROVISIONING", widths.Field4, "DAY2-UPDATE")
	case CRDTypeBareMetalHosts:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Namespace, "NS", widths.Name, "BMH", widths.Field1, "STATUS", widths.Field2, "STATE",
			widths.Field3, "ONLINE", widths.Field4, "POWEREDON", widths.Field5, "NETDATA", widths.Field6, "ERROR",
			widths.Field7, "OCLOUD", widths.Field8, "CLCMVALIDATION")
	case CRDTypeHostFirmwareComponents:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "HOSTFIRMWARECOMPONENTS", widths.Field1, "GEN", widths.Field2, "OBSERVED",
			widths.Field3, "VALID", widths.Field4, "CHANGE")
	case CRDTypeHostFirmwareSettings:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "HOSTFIRMWARESETTINGS", widths.Field1, "GEN", widths.Field2, "OBSERVED",
			widths.Field3, "VALID", widths.Field4, "CHANGE")
	case CRDTypeInventoryResources:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "NAME", widths.Field2, "POOL", widths.Field3, "RESOURCE-ID",
			widths.Field4, "MODEL", widths.Field5, "ADMIN", widths.Field6, "OPER",
			widths.Field7, "POWER", widths.Field8, "USAGE")
	case CRDTypeInventoryResourcePools:
		fmt.Printf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "SITE", widths.Field2, "POOL", widths.Field3, "RESOURCE-POOL-ID")
	case CRDTypeInventoryNodeClusters:
		fmt.Printf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "NODE-NAME", widths.Field2, "NODE-CLUSTER-ID", widths.Field3, "NODE-CLUSTER-TYPE-ID")
	default:
		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Age, "AGE", widths.Name, "NAME", widths.Namespace, "NAMESPACE", 15, "KIND")
	}
}

func (f *TableFormatter) formatProvisioningRequest(age string, obj runtime.Object, widths FieldWidths) error {
	pr, ok := obj.(*provisioningv1alpha1.ProvisioningRequest)
	if !ok {
		return fmt.Errorf("expected ProvisioningRequest, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	name := truncateToWidth(pr.ObjectMeta.Name, widths.Name)
	displayName := pr.Spec.Name
	if displayName == "" {
		displayName = StringNone
	}
	displayName = truncateToWidth(displayName, widths.Field1)

	phase := string(pr.Status.ProvisioningStatus.ProvisioningPhase)
	if phase == "" {
		phase = StringNone
	}
	phase = truncateToWidth(phase, widths.Field2)

	details := pr.Status.ProvisioningStatus.ProvisioningDetails
	if details == "" {
		details = StringNone
	}
	details = truncateToWidth(details, widths.Field3)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Name, name, widths.Field1, displayName, widths.Age, age,
		widths.Field2, phase, widths.Field3, details)

	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatNodeAllocationRequest(age string, obj runtime.Object, widths FieldWidths) error {
	nar, ok := obj.(*pluginsv1alpha1.NodeAllocationRequest)
	if !ok {
		return fmt.Errorf("expected NodeAllocationRequest, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	name := truncateToWidth(nar.ObjectMeta.Name, widths.Name)
	clusterId := nar.Spec.ClusterId
	if clusterId == "" {
		clusterId = StringNone
	}
	clusterId = truncateToWidth(clusterId, widths.Field1)

	provisioning := getConditionReason(nar.Status.Conditions, "Provisioned")
	provisioning = truncateToWidth(provisioning, widths.Field2)
	day2Update := getConditionReason(nar.Status.Conditions, "Configured")
	day2Update = truncateToWidth(day2Update, widths.Field3)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Name, name, widths.Field1, clusterId,
		widths.Field2, provisioning, widths.Field3, day2Update)

	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatAllocatedNode(age string, obj runtime.Object, widths FieldWidths) error {
	an, ok := obj.(*pluginsv1alpha1.AllocatedNode)
	if !ok {
		return fmt.Errorf("expected AllocatedNode, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	name := truncateToWidth(an.ObjectMeta.Name, widths.Name)
	nodeAllocRequest := an.Spec.NodeAllocationRequest
	if nodeAllocRequest == "" {
		nodeAllocRequest = StringNone
	}
	nodeAllocRequest = truncateToWidth(nodeAllocRequest, widths.Field1)

	hwMgrNodeId := an.Spec.HwMgrNodeId
	if hwMgrNodeId == "" {
		hwMgrNodeId = StringNone
	}
	hwMgrNodeId = truncateToWidth(hwMgrNodeId, widths.Field2)

	provisioning := getConditionReason(an.Status.Conditions, "Provisioned")
	provisioning = truncateToWidth(provisioning, widths.Field3)
	day2Update := getConditionReason(an.Status.Conditions, "Configured")
	day2Update = truncateToWidth(day2Update, widths.Field4)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Name, name,
		widths.Field1, nodeAllocRequest,
		widths.Field2, hwMgrNodeId,
		widths.Field3, provisioning, widths.Field4, day2Update)

	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatBareMetalHost(age string, obj runtime.Object, widths FieldWidths) error {
	bmh, ok := obj.(*metal3v1alpha1.BareMetalHost)
	if !ok {
		return fmt.Errorf("expected BareMetalHost, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	namespace := truncateToWidth(bmh.ObjectMeta.Namespace, widths.Namespace)
	name := truncateToWidth(bmh.ObjectMeta.Name, widths.Name)

	status := string(bmh.Status.OperationalStatus)
	if status == "" {
		status = StringNone
	}
	status = truncateToWidth(status, widths.Field1)

	state := string(bmh.Status.Provisioning.State)
	if state == "" {
		state = StringNone
	}
	state = truncateToWidth(state, widths.Field2)

	online := StringFalse
	if bmh.Spec.Online {
		online = StringTrue
	}
	online = truncateToWidth(online, widths.Field3)

	poweredOn := StringFalse
	if bmh.Status.PoweredOn {
		poweredOn = StringTrue
	}
	poweredOn = truncateToWidth(poweredOn, widths.Field4)

	netData := bmh.Spec.PreprovisioningNetworkDataName
	if netData == "" {
		netData = StringNone
	}
	netData = truncateToWidth(netData, widths.Field5)

	errorType := string(bmh.Status.ErrorType)
	if errorType == "" {
		errorType = StringNone
	}
	errorType = truncateToWidth(errorType, widths.Field6)

	// OCLOUD field - check if BMH is O-Cloud managed
	ocloud := StringNo
	if controller.IsOCloudManaged(bmh) {
		ocloud = StringYes
	}
	ocloud = truncateToWidth(ocloud, widths.Field7)

	// CLCMVALIDATION field - get the validation label value if present
	clcmvalidation := ""
	if bmh.Labels != nil {
		if labelValue, exists := bmh.Labels[controller.ValidationUnavailableLabelKey]; exists {
			clcmvalidation = labelValue
		}
	}
	clcmvalidation = truncateToWidth(clcmvalidation, widths.Field8)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Namespace, namespace, widths.Name, name, widths.Field1, status, widths.Field2, state,
		widths.Field3, online, widths.Field4, poweredOn, widths.Field5, netData, widths.Field6, errorType,
		widths.Field7, ocloud, widths.Field8, clcmvalidation)

	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatHostFirmwareComponents(age string, obj runtime.Object, widths FieldWidths) error {
	hfc, ok := obj.(*metal3v1alpha1.HostFirmwareComponents)
	if !ok {
		return fmt.Errorf("expected HostFirmwareComponents, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	name := truncateToWidth(hfc.ObjectMeta.Name, widths.Name)
	generation := fmt.Sprintf("%d", hfc.ObjectMeta.Generation)
	generation = truncateToWidth(generation, widths.Field1)

	// Get all observedGeneration values from all conditions
	var observedGens []string
	for _, condition := range hfc.Status.Conditions {
		observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
	}
	observedGeneration := StringNone
	if len(observedGens) > 0 {
		observedGeneration = strings.Join(observedGens, ",")
	}
	observedGeneration = truncateToWidth(observedGeneration, widths.Field2)

	// Get Valid condition status
	validStatus := StringNone
	for _, condition := range hfc.Status.Conditions {
		if condition.Type == StringValid {
			validStatus = string(condition.Status)
			break
		}
	}
	validStatus = truncateToWidth(validStatus, widths.Field3)

	// Get ChangeDetected condition status
	changeStatus := StringNone
	for _, condition := range hfc.Status.Conditions {
		if condition.Type == StringChangeDetected {
			changeStatus = string(condition.Status)
			break
		}
	}
	changeStatus = truncateToWidth(changeStatus, widths.Field4)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Name, name, widths.Field1, generation, widths.Field2, observedGeneration,
		widths.Field3, validStatus, widths.Field4, changeStatus)

	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatHostFirmwareSettings(age string, obj runtime.Object, widths FieldWidths) error {
	hfs, ok := obj.(*metal3v1alpha1.HostFirmwareSettings)
	if !ok {
		return fmt.Errorf("expected HostFirmwareSettings, got %T", obj)
	}

	sidebarChar := SidebarCharUnicode
	if !f.useUnicode {
		sidebarChar = SidebarCharASCII
	}

	name := truncateToWidth(hfs.ObjectMeta.Name, widths.Name)
	generation := fmt.Sprintf("%d", hfs.ObjectMeta.Generation)
	generation = truncateToWidth(generation, widths.Field1)

	// Get all observedGeneration values from all conditions
	var observedGens []string
	for _, condition := range hfs.Status.Conditions {
		observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
	}
	observedGeneration := StringNone
	if len(observedGens) > 0 {
		observedGeneration = strings.Join(observedGens, ",")
	}
	observedGeneration = truncateToWidth(observedGeneration, widths.Field2)

	// Get Valid condition status
	validStatus := StringNone
	for _, condition := range hfs.Status.Conditions {
		if condition.Type == StringValid {
			validStatus = string(condition.Status)
			break
		}
	}
	validStatus = truncateToWidth(validStatus, widths.Field3)

	// Get ChangeDetected condition status
	changeStatus := StringNone
	for _, condition := range hfs.Status.Conditions {
		if condition.Type == StringChangeDetected {
			changeStatus = string(condition.Status)
			break
		}
	}
	changeStatus = truncateToWidth(changeStatus, widths.Field4)

	fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		sidebarChar, widths.Name, name, widths.Field1, generation, widths.Field2, observedGeneration,
		widths.Field3, validStatus, widths.Field4, changeStatus)

	return nil
}

//nolint:gocyclo,unparam // Complex state field extraction logic is required for inventory resources, age parameter required for interface consistency
func (f *TableFormatter) formatInventoryResource(age string, obj runtime.Object, widths FieldWidths) error {
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

		// Extract state fields from extensions
		adminState := StringUnknown
		if resource.Extensions != nil {
			if adminVal, exists := resource.Extensions["adminState"]; exists {
				if adminStr, ok := adminVal.(string); ok && adminStr != "" {
					adminState = adminStr
				}
			}
		}

		operationalState := StringUnknown
		if resource.Extensions != nil {
			if operVal, exists := resource.Extensions["operationalState"]; exists {
				if operStr, ok := operVal.(string); ok && operStr != "" {
					operationalState = operStr
				}
			}
		}

		powerState := StringUnknown
		if resource.Extensions != nil {
			if powerVal, exists := resource.Extensions["powerState"]; exists {
				if powerStr, ok := powerVal.(string); ok && powerStr != "" {
					powerState = powerStr
				}
			}
		}

		usageState := StringUnknown
		if resource.Extensions != nil {
			if usageVal, exists := resource.Extensions["usageState"]; exists {
				if usageStr, ok := usageVal.(string); ok && usageStr != "" {
					usageState = usageStr
				}
			}
		}

		sidebarChar := SidebarCharUnicode
		if !f.useUnicode {
			sidebarChar = "|"
		}

		name = truncateToWidth(name, widths.Field1)
		pool = truncateToWidth(pool, widths.Field2)
		resourceID := truncateToWidth(resource.ResourceID, widths.Field3)
		model = truncateToWidth(model, widths.Field4)
		adminState = truncateToWidth(adminState, widths.Field5)
		operationalState = truncateToWidth(operationalState, widths.Field6)
		powerState = truncateToWidth(powerState, widths.Field7)
		usageState = truncateToWidth(usageState, widths.Field8)

		fmt.Printf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, name, widths.Field2, pool, widths.Field3, resourceID,
			widths.Field4, model, widths.Field5, adminState, widths.Field6, operationalState,
			widths.Field7, powerState, widths.Field8, usageState)
	}
	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatInventoryResourcePool(age string, obj runtime.Object, widths FieldWidths) error {
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

		sidebarChar := SidebarCharUnicode
		if !f.useUnicode {
			sidebarChar = "|"
		}

		site = truncateToWidth(site, widths.Field1)
		poolName = truncateToWidth(poolName, widths.Field2)
		resourcePoolID = truncateToWidth(resourcePoolID, widths.Field3)

		fmt.Printf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, site, widths.Field2, poolName, widths.Field3, resourcePoolID)
	}
	return nil
}

//nolint:unparam // age parameter required for interface consistency
func (f *TableFormatter) formatInventoryNodeCluster(age string, obj runtime.Object, widths FieldWidths) error {
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

		sidebarChar := SidebarCharUnicode
		if !f.useUnicode {
			sidebarChar = "|"
		}

		nodeName = truncateToWidth(nodeName, widths.Field1)
		nodeClusterID = truncateToWidth(nodeClusterID, widths.Field2)
		nodeClusterTypeID = truncateToWidth(nodeClusterTypeID, widths.Field3)

		fmt.Printf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, nodeName, widths.Field2, nodeClusterID, widths.Field3, nodeClusterTypeID)
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

// New helper functions for sectioned output formatting

func (f *TableFormatter) printMainHeader() {
	currentTime := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	if f.useUnicode {
		// Unicode version like TUIFormatter
		headerContent := fmt.Sprintf("╔═══ O-Cloud Manager Provisioning Watcher ═══ %s", currentTime)
		contentLength := len(fmt.Sprintf("═══ O-Cloud Manager Provisioning Watcher ═══ %s", currentTime))
		bottomLine := "╚" + strings.Repeat("═", contentLength)

		fmt.Println(headerContent)
		fmt.Println(bottomLine)
	} else {
		// ASCII version
		headerContent := fmt.Sprintf("+--- O-Cloud Manager Provisioning Watcher --- %s", currentTime)
		contentLength := len(fmt.Sprintf("--- O-Cloud Manager Provisioning Watcher --- %s", currentTime))
		bottomLine := "+" + strings.Repeat("-", contentLength)

		fmt.Println(headerContent)
		fmt.Println(bottomLine)
	}
}

func (f *TableFormatter) printSectionHeader(crdType string) {
	// Map CRD types to readable headers (same as TUIFormatter)
	headerMap := map[string]string{
		CRDTypeProvisioningRequests:   "Provisioning Requests",
		CRDTypeNodeAllocationRequests: "Node Allocation Requests",
		CRDTypeAllocatedNodes:         "Allocated Nodes",
		CRDTypeBareMetalHosts:         "Bare Metal Hosts",
		CRDTypeHostFirmwareComponents: "Host Firmware Component CRs",
		CRDTypeHostFirmwareSettings:   "Firmware Settings CRs",
		CRDTypeInventoryResourcePools: "O-RAN Resource Pools",
		CRDTypeInventoryResources:     "O-RAN Resources",
		CRDTypeInventoryNodeClusters:  "O-RAN Nodes",
	}

	// Use the mapped header or fall back to the original crdType
	displayName := headerMap[crdType]
	if displayName == "" {
		displayName = crdType
	}

	if f.useUnicode {
		fmt.Printf("┌─ %s ─\n", displayName)
	} else {
		fmt.Printf("+- %s -\n", displayName)
	}
}

func (f *TableFormatter) getOrderedCRDTypes() []string {
	// Define preferred order for all possible CRD types (same as TUIFormatter)
	preferredOrder := []string{
		CRDTypeProvisioningRequests,
		CRDTypeNodeAllocationRequests,
		CRDTypeAllocatedNodes,
		CRDTypeBareMetalHosts,
		CRDTypeHostFirmwareComponents,
		CRDTypeHostFirmwareSettings,
		CRDTypeInventoryResourcePools,
		CRDTypeInventoryResources,
		CRDTypeInventoryNodeClusters,
	}

	var result []string

	// Add watched CRD types in preferred order (show all watched types, even if empty)
	for _, crdType := range preferredOrder {
		// Check if this CRD type is being watched
		for _, watchedType := range f.crdTypes {
			if crdType == watchedType {
				result = append(result, crdType)
				break
			}
		}
		// Also include inventory types that have events (even if not explicitly in watched types)
		if strings.HasPrefix(crdType, "inventory-") {
			if _, exists := f.events[crdType]; exists {
				// Check if we already added it
				found := false
				for _, addedType := range result {
					if addedType == crdType {
						found = true
						break
					}
				}
				if !found {
					result = append(result, crdType)
				}
			}
		}
	}

	// Add any other CRD types that might exist but aren't in our predefined order
	for crdType := range f.events {
		found := false
		for _, addedType := range result {
			if crdType == addedType {
				found = true
				break
			}
		}
		if !found {
			result = append(result, crdType)
		}
	}

	return result
}

// calculateFieldWidths calculates dynamic field widths based on actual data
// This mirrors the logic from tui.go exactly
func (f *TableFormatter) calculateFieldWidths(crdType string, events []WatchEvent) FieldWidths {
	// Initialize minimum widths for headers
	widths := f.initializeHeaderWidths(crdType)

	// Calculate NAME field width based on actual data (like TUIFormatter)
	for _, event := range events {
		if event.Object == nil {
			continue
		}

		accessor, _ := meta.Accessor(event.Object)
		nameLen := 0
		if accessor != nil {
			nameLen = len(accessor.GetName())
		} else {
			// For objects that don't implement metav1.Object (like ResourcePoolObject),
			// use a fallback approach to get a meaningful name
			switch obj := event.Object.(type) {
			case *ResourcePoolObject:
				if obj == nil {
					nameLen = len("UNKNOWN")
				} else {
					nameLen = len(obj.ResourcePool.ResourcePoolID)
				}
			case *InventoryResourceObject:
				if obj == nil {
					nameLen = len("UNKNOWN")
				} else {
					nameLen = len(obj.Resource.ResourceID)
				}
			default:
				nameLen = len("UNKNOWN")
			}
		}
		widths.Name = safeMax(widths.Name, nameLen)
	}

	// Calculate field widths based on CRD type
	switch crdType {
	case CRDTypeProvisioningRequests:
		widths = f.calculateProvisioningRequestWidths(events, widths)
	case CRDTypeNodeAllocationRequests:
		widths = f.calculateNodeAllocationRequestWidths(events, widths)
	case CRDTypeAllocatedNodes:
		widths = f.calculateAllocatedNodeWidths(events, widths)
	case CRDTypeBareMetalHosts:
		widths = f.calculateBareMetalHostWidths(events, widths)
	case CRDTypeHostFirmwareComponents:
		widths = f.calculateHostFirmwareComponentsWidths(events, widths)
	case CRDTypeHostFirmwareSettings:
		widths = f.calculateHostFirmwareSettingsWidths(events, widths)
	case CRDTypeInventoryResources:
		widths = f.calculateInventoryResourceWidths(events, widths)
	case CRDTypeInventoryResourcePools:
		widths = f.calculateInventoryResourcePoolWidths(events, widths)
	case CRDTypeInventoryNodeClusters:
		widths = f.calculateInventoryNodeClusterWidths(events, widths)
	}

	// Apply maximum width limits
	return f.applyMaxWidthLimits(widths)
}

// initializeHeaderWidths sets minimum widths based on headers
func (f *TableFormatter) initializeHeaderWidths(crdType string) FieldWidths {
	widths := FieldWidths{
		Name:      8,  // "NAME"
		Field1:    8,  // varies by CRD
		Field2:    8,  // varies by CRD
		Field3:    8,  // varies by CRD
		Field4:    12, // varies by CRD
		Field5:    8,  // BareMetalHost only
		Field6:    8,  // BareMetalHost only
		Field7:    8,  // future use
		Field8:    8,  // future use
		Age:       3,  // "AGE"
		Namespace: 10, // "NAMESPACE"
	}

	switch crdType {
	case CRDTypeProvisioningRequests:
		widths.Field1 = safeMax(widths.Field1, len("DISPLAYNAME"))
		widths.Field2 = safeMax(widths.Field2, len("PHASE"))
		widths.Field3 = safeMax(widths.Field3, len("DETAILS"))
	case CRDTypeNodeAllocationRequests:
		widths.Field1 = safeMax(widths.Field1, len("CLUSTER-ID"))
		widths.Field2 = safeMax(widths.Field2, len("PROVISIONING"))
		widths.Field3 = safeMax(widths.Field3, len("DAY2-UPDATE"))
	case CRDTypeAllocatedNodes:
		widths.Field1 = safeMax(widths.Field1, len("NODE-ALLOC-REQUEST"))
		widths.Field2 = safeMax(widths.Field2, len("HWMGR-NODE-ID"))
		widths.Field3 = safeMax(widths.Field3, len("PROVISIONING"))
		widths.Field4 = safeMax(widths.Field4, len("DAY2-UPDATE"))
	case CRDTypeBareMetalHosts:
		widths.Namespace = safeMax(widths.Namespace, len("NS"))
		widths.Name = safeMax(widths.Name, len("BMH"))
		widths.Field1 = safeMax(widths.Field1, len("STATUS"))
		widths.Field2 = safeMax(widths.Field2, len("STATE"))
		widths.Field3 = safeMax(widths.Field3, len("ONLINE"))
		widths.Field4 = safeMax(widths.Field4, len("POWEREDON"))
		widths.Field5 = safeMax(widths.Field5, len("NETDATA"))
		widths.Field6 = safeMax(widths.Field6, len("ERROR"))
		widths.Field7 = safeMax(widths.Field7, len("OCLOUD"))
		widths.Field8 = safeMax(widths.Field8, len("CLCMVALIDATION"))
	case CRDTypeHostFirmwareComponents:
		widths.Name = safeMax(widths.Name, len("HOSTFIRMWARECOMPONENTS"))
		widths.Field1 = safeMax(widths.Field1, len("GEN"))
		widths.Field2 = safeMax(widths.Field2, len("OBSERVED"))
		widths.Field3 = safeMax(widths.Field3, len("VALID"))
		widths.Field4 = safeMax(widths.Field4, len("CHANGE"))
	case CRDTypeHostFirmwareSettings:
		widths.Name = safeMax(widths.Name, len("HOSTFIRMWARESETTINGS"))
		widths.Field1 = safeMax(widths.Field1, len("GEN"))
		widths.Field2 = safeMax(widths.Field2, len("OBSERVED"))
		widths.Field3 = safeMax(widths.Field3, len("VALID"))
		widths.Field4 = safeMax(widths.Field4, len("CHANGE"))
	case CRDTypeInventoryResources:
		widths.Field1 = safeMax(widths.Field1, len("NAME"))
		widths.Field2 = safeMax(widths.Field2, len("POOL"))
		widths.Field3 = safeMax(widths.Field3, len("RESOURCE-ID"))
		widths.Field4 = safeMax(widths.Field4, len("MODEL"))
		widths.Field5 = safeMax(widths.Field5, len("ADMIN"))
		widths.Field6 = safeMax(widths.Field6, len("OPER"))
		widths.Field7 = safeMax(widths.Field7, len("POWER"))
		widths.Field8 = safeMax(widths.Field8, len("USAGE"))
	case CRDTypeInventoryResourcePools:
		widths.Field1 = safeMax(widths.Field1, len("SITE"))
		widths.Field2 = safeMax(widths.Field2, len("POOL"))
		widths.Field3 = safeMax(widths.Field3, len("RESOURCE-POOL-ID"))
	case CRDTypeInventoryNodeClusters:
		widths.Field1 = safeMax(widths.Field1, len("NODE-NAME"))
		widths.Field2 = safeMax(widths.Field2, len("NODE-CLUSTER-ID"))
		widths.Field3 = safeMax(widths.Field3, len("NODE-CLUSTER-TYPE-ID"))
	}

	return widths
}

// Real width calculation functions - exactly like TUIFormatter

func (f *TableFormatter) calculateProvisioningRequestWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		pr, ok := event.Object.(*provisioningv1alpha1.ProvisioningRequest)
		if !ok {
			continue
		}
		displayName := pr.Spec.Name
		if displayName == "" {
			displayName = StringNone
		}
		widths.Field1 = safeMax(widths.Field1, len(displayName))

		phase := string(pr.Status.ProvisioningStatus.ProvisioningPhase)
		if phase == "" {
			phase = StringNone
		}
		widths.Field2 = safeMax(widths.Field2, len(phase))

		details := pr.Status.ProvisioningStatus.ProvisioningDetails
		if details == "" {
			details = StringNone
		}
		widths.Field3 = safeMax(widths.Field3, len(details))
	}
	return widths
}

func (f *TableFormatter) calculateNodeAllocationRequestWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		nar, ok := event.Object.(*pluginsv1alpha1.NodeAllocationRequest)
		if !ok {
			continue
		}
		clusterId := nar.Spec.ClusterId
		if clusterId == "" {
			clusterId = StringNone
		}
		widths.Field1 = safeMax(widths.Field1, len(clusterId))

		provisioning := getConditionReason(nar.Status.Conditions, "Provisioned")
		widths.Field2 = safeMax(widths.Field2, len(provisioning))

		day2Update := getConditionReason(nar.Status.Conditions, "Configured")
		widths.Field3 = safeMax(widths.Field3, len(day2Update))
	}
	return widths
}

func (f *TableFormatter) calculateAllocatedNodeWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		an, ok := event.Object.(*pluginsv1alpha1.AllocatedNode)
		if !ok {
			continue
		}
		nodeAllocRequest := an.Spec.NodeAllocationRequest
		if nodeAllocRequest == "" {
			nodeAllocRequest = StringNone
		}
		widths.Field1 = safeMax(widths.Field1, len(nodeAllocRequest))

		hwMgrNodeId := an.Spec.HwMgrNodeId
		if hwMgrNodeId == "" {
			hwMgrNodeId = StringNone
		}
		widths.Field2 = safeMax(widths.Field2, len(hwMgrNodeId))

		provisioning := getConditionReason(an.Status.Conditions, "Provisioned")
		widths.Field3 = safeMax(widths.Field3, len(provisioning))

		day2Update := getConditionReason(an.Status.Conditions, "Configured")
		widths.Field4 = safeMax(widths.Field4, len(day2Update))
	}
	return widths
}

func (f *TableFormatter) calculateBareMetalHostWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		bmh, ok := event.Object.(*metal3v1alpha1.BareMetalHost)
		if !ok {
			continue
		}
		widths.Namespace = safeMax(widths.Namespace, len(bmh.ObjectMeta.Namespace))

		status := string(bmh.Status.OperationalStatus)
		if status == "" {
			status = StringNone
		}
		widths.Field1 = safeMax(widths.Field1, len(status))

		state := string(bmh.Status.Provisioning.State)
		if state == "" {
			state = StringNone
		}
		widths.Field2 = safeMax(widths.Field2, len(state))

		online := StringFalse
		if bmh.Spec.Online {
			online = StringTrue
		}
		widths.Field3 = safeMax(widths.Field3, len(online))

		poweredOn := StringFalse
		if bmh.Status.PoweredOn {
			poweredOn = StringTrue
		}
		widths.Field4 = safeMax(widths.Field4, len(poweredOn))

		netData := bmh.Spec.PreprovisioningNetworkDataName
		if netData == "" {
			netData = StringNone
		}
		widths.Field5 = safeMax(widths.Field5, len(netData))

		errorType := string(bmh.Status.ErrorType)
		if errorType == "" {
			errorType = StringNone
		}
		widths.Field6 = safeMax(widths.Field6, len(errorType))

		// OCLOUD field - "yes" or "no"
		ocloud := StringNo
		if controller.IsOCloudManaged(bmh) {
			ocloud = StringYes
		}
		widths.Field7 = safeMax(widths.Field7, len(ocloud))

		// CLCMVALIDATION field - get the validation label value if present
		clcmvalidation := ""
		if bmh.Labels != nil {
			if labelValue, exists := bmh.Labels[controller.ValidationUnavailableLabelKey]; exists {
				clcmvalidation = labelValue
			}
		}
		widths.Field8 = safeMax(widths.Field8, len(clcmvalidation))
	}
	return widths
}

func (f *TableFormatter) calculateHostFirmwareComponentsWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		hfc, ok := event.Object.(*metal3v1alpha1.HostFirmwareComponents)
		if !ok {
			continue
		}
		widths.Name = safeMax(widths.Name, len(hfc.ObjectMeta.Name))

		generation := fmt.Sprintf("%d", hfc.ObjectMeta.Generation)
		widths.Field1 = safeMax(widths.Field1, len(generation))

		// Get all observedGeneration values from all conditions
		var observedGens []string
		for _, condition := range hfc.Status.Conditions {
			observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
		}
		observedGeneration := StringNone
		if len(observedGens) > 0 {
			observedGeneration = strings.Join(observedGens, ",")
		}
		widths.Field2 = safeMax(widths.Field2, len(observedGeneration))

		// Get Valid condition status
		validStatus := StringNone
		for _, condition := range hfc.Status.Conditions {
			if condition.Type == StringValid {
				validStatus = string(condition.Status)
				break
			}
		}
		widths.Field3 = safeMax(widths.Field3, len(validStatus))

		// Get ChangeDetected condition status
		changeStatus := StringNone
		for _, condition := range hfc.Status.Conditions {
			if condition.Type == StringChangeDetected {
				changeStatus = string(condition.Status)
				break
			}
		}
		widths.Field4 = safeMax(widths.Field4, len(changeStatus))
	}
	return widths
}

func (f *TableFormatter) calculateHostFirmwareSettingsWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		hfs, ok := event.Object.(*metal3v1alpha1.HostFirmwareSettings)
		if !ok {
			continue
		}
		widths.Name = safeMax(widths.Name, len(hfs.ObjectMeta.Name))

		generation := fmt.Sprintf("%d", hfs.ObjectMeta.Generation)
		widths.Field1 = safeMax(widths.Field1, len(generation))

		// Get all observedGeneration values from all conditions
		var observedGens []string
		for _, condition := range hfs.Status.Conditions {
			observedGens = append(observedGens, fmt.Sprintf("%d", condition.ObservedGeneration))
		}
		observedGeneration := StringNone
		if len(observedGens) > 0 {
			observedGeneration = strings.Join(observedGens, ",")
		}
		widths.Field2 = safeMax(widths.Field2, len(observedGeneration))

		// Get Valid condition status
		validStatus := StringNone
		for _, condition := range hfs.Status.Conditions {
			if condition.Type == StringValid {
				validStatus = string(condition.Status)
				break
			}
		}
		widths.Field3 = safeMax(widths.Field3, len(validStatus))

		// Get ChangeDetected condition status
		changeStatus := StringNone
		for _, condition := range hfs.Status.Conditions {
			if condition.Type == StringChangeDetected {
				changeStatus = string(condition.Status)
				break
			}
		}
		widths.Field4 = safeMax(widths.Field4, len(changeStatus))
	}
	return widths
}

//nolint:gocyclo // Complex state field extraction logic is required for width calculations
func (f *TableFormatter) calculateInventoryResourceWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		iro, ok := event.Object.(*InventoryResourceObject)
		if !ok {
			continue
		}
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

		// Extract state fields from extensions
		adminState := StringUnknown
		if resource.Extensions != nil {
			if adminVal, exists := resource.Extensions["adminState"]; exists {
				if adminStr, ok := adminVal.(string); ok && adminStr != "" {
					adminState = adminStr
				}
			}
		}

		operationalState := StringUnknown
		if resource.Extensions != nil {
			if operVal, exists := resource.Extensions["operationalState"]; exists {
				if operStr, ok := operVal.(string); ok && operStr != "" {
					operationalState = operStr
				}
			}
		}

		powerState := StringUnknown
		if resource.Extensions != nil {
			if powerVal, exists := resource.Extensions["powerState"]; exists {
				if powerStr, ok := powerVal.(string); ok && powerStr != "" {
					powerState = powerStr
				}
			}
		}

		usageState := StringUnknown
		if resource.Extensions != nil {
			if usageVal, exists := resource.Extensions["usageState"]; exists {
				if usageStr, ok := usageVal.(string); ok && usageStr != "" {
					usageState = usageStr
				}
			}
		}

		widths.Field1 = safeMax(widths.Field1, len(name))
		widths.Field2 = safeMax(widths.Field2, len(pool))
		widths.Field3 = safeMax(widths.Field3, len(resource.ResourceID))
		widths.Field4 = safeMax(widths.Field4, len(model))
		widths.Field5 = safeMax(widths.Field5, len(adminState))
		widths.Field6 = safeMax(widths.Field6, len(operationalState))
		widths.Field7 = safeMax(widths.Field7, len(powerState))
		widths.Field8 = safeMax(widths.Field8, len(usageState))
	}
	return widths
}

func (f *TableFormatter) calculateInventoryResourcePoolWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		rpo, ok := event.Object.(*ResourcePoolObject)
		if !ok || rpo == nil {
			continue
		}

		// Calculate field widths based on actual content
		if rpo.ResourcePool.Name != "" {
			widths.Field2 = safeMax(widths.Field2, len(rpo.ResourcePool.Name))
		}
		if rpo.ResourcePool.ResourcePoolID != "" {
			widths.Field3 = safeMax(widths.Field3, len(rpo.ResourcePool.ResourcePoolID))
		}
	}
	return widths
}

func (f *TableFormatter) calculateInventoryNodeClusterWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		nco, ok := event.Object.(*NodeClusterObject)
		if !ok || nco == nil {
			continue
		}

		// Calculate field widths based on actual content
		if nco.NodeCluster.Name != "" {
			widths.Field1 = safeMax(widths.Field1, len(nco.NodeCluster.Name))
		}
		if nco.NodeCluster.NodeClusterID != "" {
			widths.Field2 = safeMax(widths.Field2, len(nco.NodeCluster.NodeClusterID))
		}
		if nco.NodeCluster.NodeClusterTypeID != "" {
			widths.Field3 = safeMax(widths.Field3, len(nco.NodeCluster.NodeClusterTypeID))
		}
	}
	return widths
}

// applyMaxWidthLimits applies reasonable maximum widths to prevent excessive column widths
func (f *TableFormatter) applyMaxWidthLimits(widths FieldWidths) FieldWidths {
	// Apply reasonable maximum widths to prevent overly wide columns
	const maxWidth = 50
	widths.Name = safeMin(widths.Name, maxWidth)
	widths.Field1 = safeMin(widths.Field1, maxWidth)
	widths.Field2 = safeMin(widths.Field2, maxWidth)
	widths.Field3 = safeMin(widths.Field3, maxWidth)
	widths.Field4 = safeMin(widths.Field4, 25) // Limit MODEL field to 25 characters
	widths.Field5 = safeMin(widths.Field5, maxWidth)
	widths.Field6 = safeMin(widths.Field6, maxWidth)
	widths.Field7 = safeMin(widths.Field7, maxWidth)
	widths.Field8 = safeMin(widths.Field8, maxWidth)
	widths.Namespace = safeMin(widths.Namespace, 20)

	return widths
}
