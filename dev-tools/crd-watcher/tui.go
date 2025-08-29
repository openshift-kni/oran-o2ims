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
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

const (
	// ANSI escape sequences
	ansiClearScreen = "\033[2J"
	ansiHome        = "\033[H"
	ansiHideCursor  = "\033[?25l"
	ansiShowCursor  = "\033[?25h"
	ansiReset       = "\033[0m"
	ansiBold        = "\033[1m"
	ansiGreen       = "\033[32m"
	ansiYellow      = "\033[33m"
	ansiRed         = "\033[31m"
	ansiBlue        = "\033[34m"
	// Anti-flickering sequences
	ansiClearLine     = "\033[2K"     // Clear entire line
	ansiMoveTo        = "\033[%d;%dH" // Move cursor to row, col (1-indexed)
	ansiSaveCursor    = "\033[s"      // Save cursor position
	ansiRestoreCursor = "\033[u"      // Restore cursor position
)

// safeMax is a defensive max function to avoid potential builtin issues
func safeMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// safeMin is a defensive min function to avoid potential builtin issues
func safeMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type TUIFormatter struct {
	events          []WatchEvent
	maxEvents       int
	mutex           sync.RWMutex
	termWidth       int
	termHeight      int
	isTerminal      bool
	startTime       time.Time
	lastActivity    time.Time
	refreshTimer    *time.Timer
	refreshInterval time.Duration
	watchedCRDTypes []string
	lastCleanup     time.Time
	verifyFunc      func(WatchEvent) bool // Function to verify if resource still exists in source

	// Anti-flickering features
	pendingUpdate     bool
	debounceTimer     *time.Timer
	debounceDelay     time.Duration
	lastScreenContent string // Cache of last screen content for differential updates
	forceFullRedraw   bool   // Force full redraw flag for first draw or terminal resize
	useUnicode        bool   // Whether to use Unicode characters (default true)
}

func NewTUIFormatter(refreshIntervalSeconds int, watchedCRDTypes []string, verifyFunc func(WatchEvent) bool, useUnicode bool) *TUIFormatter {
	width, height := 80, 24 // Default values
	isTerminal := false

	// Try to get actual terminal size
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			width, height = w, h
			isTerminal = true
		}
	}

	// Use more reasonable defaults to reduce flickering
	if refreshIntervalSeconds < 5 {
		refreshIntervalSeconds = 5 // Minimum 5 seconds to reduce excessive refreshes
	}

	tui := &TUIFormatter{
		events:          make([]WatchEvent, 0),
		maxEvents:       height - 6, // Reserve space for header and status
		termWidth:       width,
		termHeight:      height,
		isTerminal:      isTerminal,
		startTime:       time.Now(),
		lastActivity:    time.Now(),
		refreshInterval: time.Duration(refreshIntervalSeconds) * time.Second,
		watchedCRDTypes: watchedCRDTypes,
		lastCleanup:     time.Now(),
		verifyFunc:      verifyFunc,

		// Anti-flickering initialization
		pendingUpdate:     false,
		debounceDelay:     250 * time.Millisecond, // 250ms debounce for rapid events
		lastScreenContent: "",
		forceFullRedraw:   true, // Force full redraw on first display
		useUnicode:        useUnicode,
	}

	// Start the refresh timer for screen redraws during inactivity
	if isTerminal {
		tui.startRefreshTimer()
	}

	return tui
}

func (t *TUIFormatter) FormatEvent(event WatchEvent) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Generate a unique key for this resource
	resourceKey := t.getResourceKey(event)

	// Handle the event based on its type
	found := false
	for i, existingEvent := range t.events {
		if t.getResourceKey(existingEvent) == resourceKey && existingEvent.CRDType == event.CRDType {
			found = true
			if event.Type == watch.Deleted {
				// Remove the resource from display when deleted
				klog.V(3).Infof("Removing deleted resource: %s (was at index %d of %d)", resourceKey, i, len(t.events))
				// Safely remove the element to avoid index issues
				if i < len(t.events)-1 {
					copy(t.events[i:], t.events[i+1:])
				}
				t.events = t.events[:len(t.events)-1]
			} else {
				// Replace the existing event with the new one (latest update)
				klog.V(4).Infof("Updating resource: %s", resourceKey)
				t.events[i] = event
			}
			break
		}
	}

	// If this is a new resource and not a delete event, add it to the buffer
	if !found && event.Type != watch.Deleted {
		klog.V(3).Infof("Adding new resource: %s", resourceKey)
		t.events = append(t.events, event)
	}

	// Periodic cleanup of stale resources (every 30 seconds)
	// Only removes resources confirmed deleted from source database/API
	if time.Since(t.lastCleanup) > 30*time.Second {
		t.cleanupDeletedResources()
		t.lastCleanup = time.Now()
	}

	// Keep only the most recent unique resources (limit applies to number of distinct resources shown)
	if len(t.events) > t.maxEvents {
		t.events = t.events[len(t.events)-t.maxEvents:]
	}

	// Update activity time and reset refresh timer
	t.lastActivity = time.Now()
	if t.isTerminal {
		t.resetRefreshTimer()
	}

	// Use immediate update for deletion events, debounced for others to prevent flickering
	if event.Type == watch.Deleted {
		return t.immediateUpdate()
	}
	return t.scheduleUpdate()
}

// immediateUpdate performs an immediate screen update without debouncing
// Used for critical events like deletions that need instant reflection
func (t *TUIFormatter) immediateUpdate() error {
	if !t.isTerminal {
		return t.fallbackFormat()
	}

	// Cancel any pending debounced update since we're doing immediate update
	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
		t.pendingUpdate = false
	}

	return t.redrawScreen()
}

// scheduleUpdate schedules a screen update with debouncing to prevent flickering from rapid events
func (t *TUIFormatter) scheduleUpdate() error {
	if !t.isTerminal {
		return t.fallbackFormat()
	}

	// If there's already a pending update, let the existing timer handle it
	if t.pendingUpdate {
		return nil
	}

	t.pendingUpdate = true

	// Cancel existing debounce timer if any
	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
	}

	// Schedule debounced update
	t.debounceTimer = time.AfterFunc(t.debounceDelay, func() {
		t.mutex.Lock()
		defer t.mutex.Unlock()

		t.pendingUpdate = false
		if err := t.redrawScreen(); err != nil {
			klog.V(3).Infof("Error during debounced redraw: %v", err)
		}
	})

	return nil
}

// applyDifferentialUpdate updates only the changed parts of the screen to minimize flickering
func (t *TUIFormatter) applyDifferentialUpdate(oldContent, newContent string) {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Hide cursor during update to prevent flashing
	fmt.Print(ansiHideCursor)

	// Compare line by line and update only what changed
	maxLines := len(newLines)
	if len(oldLines) > maxLines {
		maxLines = len(oldLines)
	}

	for i := 0; i < maxLines; i++ {
		var oldLine, newLine string

		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		// If the line changed, update it
		if oldLine != newLine {
			// Move cursor to the beginning of this line (1-indexed)
			fmt.Printf(ansiMoveTo, i+1, 1)

			// Clear the line and write new content
			fmt.Print(ansiClearLine)
			if newLine != "" {
				fmt.Print(newLine)
			}
		}
	}

	// If new content has fewer lines than old, clear the remaining lines
	if len(oldLines) > len(newLines) {
		for i := len(newLines); i < len(oldLines); i++ {
			fmt.Printf(ansiMoveTo, i+1, 1)
			fmt.Print(ansiClearLine)
		}
	}
}

// getResourceKey generates a unique key for a resource based on its metadata
func (t *TUIFormatter) getResourceKey(event WatchEvent) string {
	// For inventory objects that don't implement standard Kubernetes metadata
	switch event.CRDType {
	case CRDTypeInventoryResources:
		if iro, ok := event.Object.(*InventoryResourceObject); ok && iro != nil {
			return fmt.Sprintf("%s/%s", event.CRDType, iro.Resource.ResourceID)
		}
	case CRDTypeInventoryResourcePools:
		if rpo, ok := event.Object.(*ResourcePoolObject); ok && rpo != nil {
			return fmt.Sprintf("%s/%s", event.CRDType, rpo.ResourcePool.ResourcePoolID)
		}
	case CRDTypeInventoryNodeClusters:
		if nco, ok := event.Object.(*NodeClusterObject); ok && nco != nil {
			return fmt.Sprintf("%s/%s", event.CRDType, nco.NodeCluster.Name)
		}
	}

	// For standard Kubernetes resources, use name and namespace
	accessor, err := meta.Accessor(event.Object)
	if err != nil {
		// Fallback to timestamp if we can't get metadata
		return fmt.Sprintf("%s/unknown-%d", event.CRDType, event.Timestamp.UnixNano())
	}

	// Use namespace/name as the key, or just name if no namespace
	namespace := accessor.GetNamespace()
	name := accessor.GetName()
	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s", event.CRDType, namespace, name)
	}
	return fmt.Sprintf("%s/%s", event.CRDType, name)
}

// cleanupDeletedResources removes resources that are confirmed deleted from their source
// This ensures that only resources that have actually been deleted from the database/API are removed,
// regardless of when they were last updated. Age is not a factor in removal decisions.
func (t *TUIFormatter) cleanupDeletedResources() {
	if t.verifyFunc == nil {
		klog.V(3).Info("No verification function provided, skipping resource cleanup")
		return
	}

	// Only verify a subset of resources each cleanup cycle to avoid excessive API calls
	// We'll verify up to 10 resources per cleanup, cycling through all resources over time
	const maxVerificationsPerCycle = 10

	// Filter out resources that are confirmed deleted from their source
	var filteredEvents []WatchEvent
	removedCount := 0
	verifiedCount := 0

	for i, event := range t.events {
		// Verify resources in batches to avoid overwhelming the API
		// Use modulo to cycle through all resources over multiple cleanup calls
		shouldVerify := verifiedCount < maxVerificationsPerCycle && (i%5 == int(time.Now().Unix()/60)%5)

		if shouldVerify {
			verifiedCount++
			resourceKey := t.getResourceKey(event)
			klog.V(3).Infof("Verifying resource existence: %s", resourceKey)

			if t.verifyFunc(event) {
				// Resource still exists in source database/API, keep it
				filteredEvents = append(filteredEvents, event)
				klog.V(3).Infof("Resource still exists in source, keeping: %s", resourceKey)
			} else {
				// Resource confirmed deleted from source database/API, safe to remove from display
				removedCount++
				klog.V(2).Infof("Removing resource confirmed deleted from source: %s", resourceKey)
			}
		} else {
			// Not verifying this resource in this cycle, keep it
			filteredEvents = append(filteredEvents, event)
		}
	}

	t.events = filteredEvents

	if verifiedCount > 0 {
		klog.V(1).Infof("Verified %d resources: removed %d confirmed deleted, kept %d (%d total remaining)",
			verifiedCount, removedCount, verifiedCount-removedCount, len(t.events))
	}
}

// cleanupStaleInventoryObjects performs immediate cleanup of stale inventory objects only
func (t *TUIFormatter) cleanupStaleInventoryObjects() {
	if t.verifyFunc == nil {
		klog.V(3).Info("No verification function provided, skipping inventory object cleanup")
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Filter out only stale inventory objects
	var filteredEvents []WatchEvent
	removedCount := 0

	for _, event := range t.events {
		// Only verify inventory objects during inventory refresh
		if event.CRDType == "inventory-resources" ||
			event.CRDType == "inventory-resource-pools" ||
			event.CRDType == "inventory-node-clusters" {

			resourceKey := t.getResourceKey(event)
			klog.V(3).Infof("Verifying inventory object existence: %s", resourceKey)

			if t.verifyFunc(event) {
				// Resource still exists in inventory, keep it
				filteredEvents = append(filteredEvents, event)
				klog.V(3).Infof("Inventory object still exists, keeping: %s", resourceKey)
			} else {
				// Resource confirmed deleted from inventory, remove from display
				removedCount++
				klog.V(2).Infof("Removing stale inventory object: %s", resourceKey)
			}
		} else {
			// Not an inventory object, keep it
			filteredEvents = append(filteredEvents, event)
		}
	}

	t.events = filteredEvents
	klog.V(2).Infof("Inventory cleanup removed %d stale objects", removedCount)

	// Trigger screen update if we removed any objects
	if removedCount > 0 && t.isTerminal {
		if err := t.scheduleUpdate(); err != nil {
			klog.V(2).Infof("Error scheduling screen update after inventory cleanup: %v", err)
		}
	}
}

// TriggerCleanup manually triggers a cleanup of deleted resources (can be called externally)
func (t *TUIFormatter) TriggerCleanup() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	klog.V(2).Info("Manual cleanup triggered")
	t.cleanupDeletedResources()
	t.lastCleanup = time.Now()

	// Trigger screen update using debounced mechanism
	if err := t.scheduleUpdate(); err != nil {
		klog.V(2).Infof("Error scheduling screen update after cleanup: %v", err)
	}
}

func (t *TUIFormatter) redrawScreen() error {
	if !t.isTerminal {
		// Fall back to regular table output if not a terminal
		return t.fallbackFormat()
	}

	// Ensure stale resources are cleaned up during refresh
	// Only removes resources confirmed deleted from source database/API
	if time.Since(t.lastCleanup) > 15*time.Second {
		t.cleanupDeletedResources()
		t.lastCleanup = time.Now()
	}

	// Build the entire screen content in memory to compare with previous
	var screenContent strings.Builder

	// Add header content
	t.buildHeader(&screenContent)

	// Draw events grouped by CRD type in specified order
	eventsByCRD := t.groupEventsByType()
	orderedTypes := t.getOrderedCRDTypes(eventsByCRD)

	lineCount := 2 // Account for header lines (now just 2 lines)
	for _, crdType := range orderedTypes {
		events := eventsByCRD[crdType]
		if lineCount >= t.termHeight-2 {
			break // Don't exceed terminal height
		}

		// Build CRD type header
		screenContent.WriteString(fmt.Sprintf("%s%s%s\n", ansiBold, t.formatCRDHeader(crdType), ansiReset))
		lineCount++

		// Calculate dynamic field widths for this CRD type
		widths := t.calculateFieldWidths(crdType, events)

		// Build table header for this CRD type
		t.buildCRDTableHeader(crdType, widths, &screenContent)
		lineCount++

		// Build events for this CRD type
		if len(events) == 0 {
			// Show empty message when no resources exist
			screenContent.WriteString(fmt.Sprintf("%s No resources found\n", t.getSidebarChar()))
			lineCount++
		} else {
			for i, event := range events {
				if lineCount >= t.termHeight-1 {
					break
				}
				t.buildEventLine(event, widths, &screenContent)
				lineCount++

				// Limit events per CRD type to avoid screen overflow
				if i >= 10 {
					break
				}
			}
		}

		if lineCount < t.termHeight-1 {
			screenContent.WriteString("\n") // Add spacing between CRD types
			lineCount++
		}
	}

	// Build status line
	t.buildStatusLine(&screenContent)

	newContent := screenContent.String()

	// Implement differential updates to minimize flickering
	if t.forceFullRedraw || t.lastScreenContent == "" {
		// First time or forced full redraw - clear screen once and draw everything
		fmt.Print(ansiClearScreen + ansiHome + ansiHideCursor)
		fmt.Print(newContent)
		t.forceFullRedraw = false
	} else if newContent != t.lastScreenContent {
		// Content changed - use differential update
		t.applyDifferentialUpdate(t.lastScreenContent, newContent)
	}
	// If content is identical, do nothing (no flicker)

	t.lastScreenContent = newContent
	fmt.Print(ansiShowCursor)

	return nil
}

func (t *TUIFormatter) buildHeader(sb *strings.Builder) {
	currentTime := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	// Use double-line connectors on the left side (Unicode or ASCII)
	var headerContent, bottomLine string
	if t.useUnicode {
		headerContent = fmt.Sprintf("╔═══ O-Cloud Manager Provisioning Watcher ═══ %s", currentTime)
		// Calculate length of content after the connector for alignment
		contentLength := len(fmt.Sprintf("═══ O-Cloud Manager Provisioning Watcher ═══ %s", currentTime))
		bottomLine = "╚" + strings.Repeat("═", contentLength)
	} else {
		headerContent = fmt.Sprintf("+--- O-Cloud Manager Provisioning Watcher --- %s", currentTime)
		// Calculate length of content after the connector for alignment
		contentLength := len(fmt.Sprintf("--- O-Cloud Manager Provisioning Watcher --- %s", currentTime))
		bottomLine = "+" + strings.Repeat("-", contentLength)
	}

	sb.WriteString(fmt.Sprintf("%s%s%s\n", ansiBold, headerContent, ansiReset))
	sb.WriteString(fmt.Sprintf("%s%s%s\n", ansiBold, bottomLine, ansiReset))
}

// getSidebarChar returns the appropriate sidebar character based on Unicode mode
func (t *TUIFormatter) getSidebarChar() string {
	if t.useUnicode {
		return SidebarCharUnicode
	}
	return SidebarCharASCII
}

func (t *TUIFormatter) formatCRDHeader(crdType string) string {
	// Map CRD types to readable headers
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

	if t.useUnicode {
		return fmt.Sprintf("┌─ %s ─", displayName)
	} else {
		return fmt.Sprintf("+- %s -", displayName)
	}
}

func (t *TUIFormatter) groupEventsByType() map[string][]WatchEvent {
	grouped := make(map[string][]WatchEvent)

	// Reverse order to show most recent first
	for i := len(t.events) - 1; i >= 0; i-- {
		event := t.events[i]
		grouped[event.CRDType] = append(grouped[event.CRDType], event)
	}

	// Sort events within each group by the appropriate field
	for crdType, events := range grouped {
		grouped[crdType] = t.sortEventsByCRDType(crdType, events)
	}

	return grouped
}

// sortEventsByCRDType sorts events based on the CRD type's preferred sorting field
func (t *TUIFormatter) sortEventsByCRDType(crdType string, events []WatchEvent) []WatchEvent {
	if len(events) == 0 {
		return events
	}

	// Create a copy to avoid modifying the original slice
	sortedEvents := make([]WatchEvent, len(events))
	copy(sortedEvents, events)

	sort.Slice(sortedEvents, func(i, j int) bool {
		return t.compareEvents(crdType, sortedEvents[i], sortedEvents[j])
	})

	return sortedEvents
}

// compareEvents compares two events based on the CRD type's sorting criteria
func (t *TUIFormatter) compareEvents(crdType string, a, b WatchEvent) bool {
	switch crdType {
	case CRDTypeProvisioningRequests:
		// Sort by DISPLAYNAME
		displayNameA := t.getProvisioningRequestDisplayName(a.Object)
		displayNameB := t.getProvisioningRequestDisplayName(b.Object)
		return displayNameA < displayNameB
	case CRDTypeNodeAllocationRequests:
		// Sort by CLUSTER-ID
		clusterIdA := t.getNodeAllocationRequestClusterId(a.Object)
		clusterIdB := t.getNodeAllocationRequestClusterId(b.Object)
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
		resourceA := t.getInventoryResourceId(a.Object)
		resourceB := t.getInventoryResourceId(b.Object)
		return resourceA < resourceB
	case CRDTypeInventoryResourcePools:
		// Sort by site, then pool name
		siteA := t.getInventoryResourcePoolSite(a.Object)
		siteB := t.getInventoryResourcePoolSite(b.Object)
		if siteA != siteB {
			return siteA < siteB
		}
		poolA := t.getInventoryResourcePoolName(a.Object)
		poolB := t.getInventoryResourcePoolName(b.Object)
		return poolA < poolB
	case CRDTypeInventoryNodeClusters:
		// Sort by node name
		nameA := t.getInventoryNodeClusterName(a.Object)
		nameB := t.getInventoryNodeClusterName(b.Object)
		return nameA < nameB
	default:
		// Default to sorting by name with nil safety
		accessor1, _ := meta.Accessor(a.Object)
		accessor2, _ := meta.Accessor(b.Object)
		if accessor1 == nil || accessor2 == nil {
			return false // Maintain consistent ordering when accessors are nil
		}
		return accessor1.GetName() < accessor2.GetName()
	}
}

// Helper functions to extract sorting fields from objects
func (t *TUIFormatter) getProvisioningRequestDisplayName(obj runtime.Object) string {
	if pr, ok := obj.(*provisioningv1alpha1.ProvisioningRequest); ok {
		if pr.Spec.Name != "" {
			return pr.Spec.Name
		}
	}
	// Fallback to object name if displayName is empty
	accessor, _ := meta.Accessor(obj)
	return accessor.GetName()
}

func (t *TUIFormatter) getNodeAllocationRequestClusterId(obj runtime.Object) string {
	if nar, ok := obj.(*pluginsv1alpha1.NodeAllocationRequest); ok {
		if nar.Spec.ClusterId != "" {
			return nar.Spec.ClusterId
		}
	}
	return StringNone
}

func (t *TUIFormatter) getInventoryResourceId(obj runtime.Object) string {
	if iro, ok := obj.(*InventoryResourceObject); ok {
		return iro.Resource.ResourceID
	}
	return StringNone
}

func (t *TUIFormatter) getInventoryResourcePoolSite(obj runtime.Object) string {
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

func (t *TUIFormatter) getInventoryResourcePoolName(obj runtime.Object) string {
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

func (t *TUIFormatter) getInventoryNodeClusterName(obj runtime.Object) string {
	if nco, ok := obj.(*NodeClusterObject); ok && nco != nil {
		if nco.NodeCluster.Name != "" {
			return nco.NodeCluster.Name
		}
		return StringUnknown
	}
	return StringNone
}

func (t *TUIFormatter) getOrderedCRDTypes(grouped map[string][]WatchEvent) []string {
	// Define preferred order for all possible CRD types
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
		for _, watchedType := range t.watchedCRDTypes {
			if crdType == watchedType {
				result = append(result, crdType)
				break
			}
		}
		// Also include inventory types that have events (even if not explicitly in watched types)
		if strings.HasPrefix(crdType, "inventory-") {
			if _, exists := grouped[crdType]; exists {
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
	for crdType := range grouped {
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

type FieldWidths struct {
	Name      int
	Field1    int
	Field2    int
	Field3    int
	Field4    int
	Field5    int
	Field6    int
	Field7    int
	Field8    int
	Age       int
	Namespace int
}

// Helper functions for calculating field widths to reduce cyclomatic complexity

func (t *TUIFormatter) initializeHeaderWidths(crdType string) FieldWidths {
	// Set minimum widths for headers
	widths := FieldWidths{
		Name:      8,  // "NAME"
		Field1:    8,  // varies by CRD
		Field2:    8,  // varies by CRD
		Field3:    8,  // varies by CRD
		Field4:    12, // varies by CRD
		Field5:    8,  // BareMetalHost only
		Field6:    8,  // BareMetalHost only
		Field7:    8,  // future use
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

func (t *TUIFormatter) calculateProvisioningRequestWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
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

func (t *TUIFormatter) calculateNodeAllocationRequestWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
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

func (t *TUIFormatter) calculateAllocatedNodeWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
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

func (t *TUIFormatter) calculateBareMetalHostWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
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
	}
	return widths
}

func (t *TUIFormatter) calculateHostFirmwareComponentsWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		hfc, ok := event.Object.(*metal3v1alpha1.HostFirmwareComponents)
		if !ok {
			continue
		}
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

func (t *TUIFormatter) calculateHostFirmwareSettingsWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		hfs, ok := event.Object.(*metal3v1alpha1.HostFirmwareSettings)
		if !ok {
			continue
		}
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
func (t *TUIFormatter) calculateInventoryResourceWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
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

func (t *TUIFormatter) calculateInventoryResourcePoolWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		rpo, ok := event.Object.(*ResourcePoolObject)
		if !ok || rpo == nil {
			continue
		}
		// Add comprehensive nil checks to prevent panic
		site := StringUnknown
		poolName := StringUnknown
		resourcePoolID := ""

		// Safe field access with nil checks
		resourcePoolID = rpo.ResourcePool.ResourcePoolID

		// Only call methods if we have valid data
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

		widths.Field1 = safeMax(widths.Field1, len(site))
		widths.Field2 = safeMax(widths.Field2, len(poolName))
		widths.Field3 = safeMax(widths.Field3, len(resourcePoolID))
	}
	return widths
}

func (t *TUIFormatter) calculateInventoryNodeClusterWidths(events []WatchEvent, widths FieldWidths) FieldWidths {
	for _, event := range events {
		if nco, ok := event.Object.(*NodeClusterObject); ok && nco != nil {
			nodeCluster := nco.NodeCluster
			widths.Field1 = safeMax(widths.Field1, len(nodeCluster.Name))
			widths.Field2 = safeMax(widths.Field2, len(nodeCluster.NodeClusterID))
			widths.Field3 = safeMax(widths.Field3, len(nodeCluster.NodeClusterTypeID))
		}
	}
	return widths
}

func (t *TUIFormatter) applyMaxWidthLimits(widths FieldWidths) FieldWidths {
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

func (t *TUIFormatter) calculateFieldWidths(crdType string, events []WatchEvent) FieldWidths {
	// Initialize minimum widths for headers
	widths := t.initializeHeaderWidths(crdType)

	// Calculate widths based on actual data
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
		widths = t.calculateProvisioningRequestWidths(events, widths)
	case CRDTypeNodeAllocationRequests:
		widths = t.calculateNodeAllocationRequestWidths(events, widths)
	case CRDTypeAllocatedNodes:
		widths = t.calculateAllocatedNodeWidths(events, widths)
	case CRDTypeBareMetalHosts:
		widths = t.calculateBareMetalHostWidths(events, widths)
	case CRDTypeHostFirmwareComponents:
		widths = t.calculateHostFirmwareComponentsWidths(events, widths)
	case CRDTypeHostFirmwareSettings:
		widths = t.calculateHostFirmwareSettingsWidths(events, widths)
	case CRDTypeInventoryResources:
		widths = t.calculateInventoryResourceWidths(events, widths)
	case CRDTypeInventoryResourcePools:
		widths = t.calculateInventoryResourcePoolWidths(events, widths)
	case CRDTypeInventoryNodeClusters:
		widths = t.calculateInventoryNodeClusterWidths(events, widths)
	}

	// Apply reasonable maximum widths to prevent overly wide columns
	widths = t.applyMaxWidthLimits(widths)

	return widths
}

func (t *TUIFormatter) buildCRDTableHeader(crdType string, widths FieldWidths, sb *strings.Builder) {
	sidebarChar := t.getSidebarChar()
	switch crdType {
	case CRDTypeProvisioningRequests:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "DISPLAYNAME", widths.Age, "AGE",
			widths.Field2, "PHASE", widths.Field3, "DETAILS"))
	case CRDTypeNodeAllocationRequests:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "CLUSTER-ID",
			widths.Field2, "PROVISIONING", widths.Field3, "DAY2-UPDATE"))
	case CRDTypeAllocatedNodes:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "NAME", widths.Field1, "NODE-ALLOC-REQUEST",
			widths.Field2, "HWMGR-NODE-ID", widths.Field3, "PROVISIONING", widths.Field4, "DAY2-UPDATE"))
	case CRDTypeBareMetalHosts:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Namespace, "NS", widths.Name, "BMH", widths.Field1, "STATUS", widths.Field2, "STATE",
			widths.Field3, "ONLINE", widths.Field4, "POWEREDON", widths.Field5, "NETDATA", widths.Field6, "ERROR"))
	case CRDTypeHostFirmwareComponents:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "HOSTFIRMWARECOMPONENTS", widths.Field1, "GEN", widths.Field2, "OBSERVED",
			widths.Field3, "VALID", widths.Field4, "CHANGE"))
	case CRDTypeHostFirmwareSettings:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Name, "HOSTFIRMWARESETTINGS", widths.Field1, "GEN", widths.Field2, "OBSERVED",
			widths.Field3, "VALID", widths.Field4, "CHANGE"))
	case CRDTypeInventoryResources:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "NAME", widths.Field2, "POOL", widths.Field3, "RESOURCE-ID",
			widths.Field4, "MODEL", widths.Field5, "ADMIN", widths.Field6, "OPER",
			widths.Field7, "POWER", widths.Field8, "USAGE"))
	case CRDTypeInventoryResourcePools:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "SITE", widths.Field2, "POOL", widths.Field3, "RESOURCE-POOL-ID"))
	case CRDTypeInventoryNodeClusters:
		sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s\n",
			sidebarChar, widths.Field1, "NODE-NAME", widths.Field2, "NODE-CLUSTER-ID", widths.Field3, "NODE-CLUSTER-TYPE-ID"))
	}
}

func (t *TUIFormatter) buildEventLine(event WatchEvent, widths FieldWidths, sb *strings.Builder) {
	accessor, _ := meta.Accessor(event.Object)
	var age string
	if accessor != nil {
		age = getAge(accessor.GetCreationTimestamp())
	} else {
		// For objects that don't implement metav1.Object, use a default age
		age = "0s"
	}

	switch event.CRDType {
	case CRDTypeProvisioningRequests:
		t.buildProvisioningRequestLine(age, event.Object, widths, sb)
	case CRDTypeNodeAllocationRequests:
		t.buildNodeAllocationRequestLine(age, event.Object, widths, sb)
	case CRDTypeAllocatedNodes:
		t.buildAllocatedNodeLine(age, event.Object, widths, sb)
	case CRDTypeBareMetalHosts:
		t.buildBareMetalHostLine(age, event.Object, widths, sb)
	case CRDTypeHostFirmwareComponents:
		t.buildHostFirmwareComponentsLine(age, event.Object, widths, sb)
	case CRDTypeHostFirmwareSettings:
		t.buildHostFirmwareSettingsLine(age, event.Object, widths, sb)
	case CRDTypeInventoryResources:
		t.buildInventoryResourceLine(age, event.Object, widths, sb)
	case CRDTypeInventoryResourcePools:
		t.buildInventoryResourcePoolLine(age, event.Object, widths, sb)
	case CRDTypeInventoryNodeClusters:
		t.buildInventoryNodeClusterLine(age, event.Object, widths, sb)
	}
}

func (t *TUIFormatter) buildProvisioningRequestLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	pr, ok := obj.(*provisioningv1alpha1.ProvisioningRequest)
	if !ok {
		return
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Name, name, widths.Field1, displayName, widths.Age, age,
		widths.Field2, phase, widths.Field3, details))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildNodeAllocationRequestLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	nar, ok := obj.(*pluginsv1alpha1.NodeAllocationRequest)
	if !ok {
		return
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Name, name, widths.Field1, clusterId,
		widths.Field2, provisioning, widths.Field3, day2Update))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildAllocatedNodeLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	an, ok := obj.(*pluginsv1alpha1.AllocatedNode)
	if !ok {
		return
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Name, name,
		widths.Field1, nodeAllocRequest,
		widths.Field2, hwMgrNodeId,
		widths.Field3, provisioning,
		widths.Field4, day2Update))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildBareMetalHostLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	bmh, ok := obj.(*metal3v1alpha1.BareMetalHost)
	if !ok {
		return
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

	online := "false"
	if bmh.Spec.Online {
		online = "true"
	}
	online = truncateToWidth(online, widths.Field3)

	poweredOn := "false"
	if bmh.Status.PoweredOn {
		poweredOn = "true"
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Namespace, namespace, widths.Name, name, widths.Field1, status, widths.Field2, state,
		widths.Field3, online, widths.Field4, poweredOn, widths.Field5, netData, widths.Field6, errorType))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildHostFirmwareComponentsLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	hfc, ok := obj.(*metal3v1alpha1.HostFirmwareComponents)
	if !ok {
		return
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Name, name, widths.Field1, generation, widths.Field2, observedGeneration,
		widths.Field3, validStatus, widths.Field4, changeStatus))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildHostFirmwareSettingsLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	hfs, ok := obj.(*metal3v1alpha1.HostFirmwareSettings)
	if !ok {
		return
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

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Name, name, widths.Field1, generation, widths.Field2, observedGeneration,
		widths.Field3, validStatus, widths.Field4, changeStatus))
}

//nolint:unparam,gocyclo // age parameter required for interface consistency; complex state field extraction logic is required for line building
func (t *TUIFormatter) buildInventoryResourceLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	iro, ok := obj.(*InventoryResourceObject)
	if !ok {
		return
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

	name = truncateToWidth(name, widths.Field1)
	pool = truncateToWidth(pool, widths.Field2)
	resourceID := truncateToWidth(resource.ResourceID, widths.Field3)
	model = truncateToWidth(model, widths.Field4)
	adminState = truncateToWidth(adminState, widths.Field5)
	operationalState = truncateToWidth(operationalState, widths.Field6)
	powerState = truncateToWidth(powerState, widths.Field7)
	usageState = truncateToWidth(usageState, widths.Field8)

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Field1, name, widths.Field2, pool, widths.Field3, resourceID,
		widths.Field4, model, widths.Field5, adminState, widths.Field6, operationalState,
		widths.Field7, powerState, widths.Field8, usageState))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildInventoryResourcePoolLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	rpo, ok := obj.(*ResourcePoolObject)
	if !ok {
		return
	}

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

	site = truncateToWidth(site, widths.Field1)
	poolName = truncateToWidth(poolName, widths.Field2)
	resourcePoolID = truncateToWidth(resourcePoolID, widths.Field3)

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Field1, site, widths.Field2, poolName, widths.Field3, resourcePoolID))
}

//nolint:unparam // age parameter required for interface consistency
func (t *TUIFormatter) buildInventoryNodeClusterLine(age string, obj runtime.Object, widths FieldWidths, sb *strings.Builder) {
	nco, ok := obj.(*NodeClusterObject)
	if !ok {
		return
	}

	nodeCluster := nco.NodeCluster
	nodeName := truncateToWidth(nodeCluster.Name, widths.Field1)
	nodeClusterID := truncateToWidth(nodeCluster.NodeClusterID, widths.Field2)
	nodeClusterTypeID := truncateToWidth(nodeCluster.NodeClusterTypeID, widths.Field3)

	sb.WriteString(fmt.Sprintf("%s %-*s   %-*s   %-*s\n",
		t.getSidebarChar(), widths.Field1, nodeName, widths.Field2, nodeClusterID, widths.Field3, nodeClusterTypeID))
}

// truncateToWidth truncates a string to fit within the specified width
// Only truncates if the string is longer than the width
func truncateToWidth(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}

func (t *TUIFormatter) buildStatusLine(sb *strings.Builder) {
	status := "Press Ctrl+C to exit"
	sb.WriteString(fmt.Sprintf("\n%s%s%s", ansiBold+ansiBlue, status, ansiReset))
}

func (t *TUIFormatter) fallbackFormat() error {
	// If not running in a terminal, fall back to regular table output
	if len(t.events) == 0 {
		return nil
	}

	lastEvent := t.events[len(t.events)-1]

	// Use the regular table formatter logic
	tableFormatter := &TableFormatter{
		headersPrinted: make(map[string]bool),
	}

	return tableFormatter.FormatEvent(lastEvent)
}

func (t *TUIFormatter) Cleanup() {
	if t.isTerminal {
		// Stop all timers
		if t.refreshTimer != nil {
			t.refreshTimer.Stop()
		}
		if t.debounceTimer != nil {
			t.debounceTimer.Stop()
		}

		// Position cursor at bottom of screen before exit
		// Move to the last row and add a newline to ensure clean exit
		fmt.Printf(ansiMoveTo, t.termHeight, 1)
		fmt.Print("\n")

		// Restore terminal state
		fmt.Print(ansiShowCursor + ansiReset)
	}
}

func (t *TUIFormatter) startRefreshTimer() {
	t.refreshTimer = time.AfterFunc(t.refreshInterval, func() {
		t.mutex.Lock()
		defer t.mutex.Unlock()

		// Only redraw if we're still in terminal mode and there's been no recent activity
		if t.isTerminal && time.Since(t.lastActivity) >= t.refreshInterval {
			// Use debounced update mechanism for refresh redraws too
			if err := t.scheduleUpdate(); err != nil {
				klog.V(3).Infof("Error during refresh update: %v", err)
			}
		}

		// Restart the timer for continuous refresh
		if t.isTerminal {
			t.startRefreshTimer()
		}
	})
}

func (t *TUIFormatter) resetRefreshTimer() {
	if t.refreshTimer != nil {
		t.refreshTimer.Stop()
	}
	t.startRefreshTimer()
}
