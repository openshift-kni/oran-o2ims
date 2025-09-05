/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

// This file provides resource selection functionality for filtering BareMetalHosts
// based on hardware characteristics and resource criteria. The resource selection system
// uses a two-stage filtering approach:
//
//  1. Primary Filter: Uses Kubernetes label selectors for efficient server-side filtering
//     of allocation status, site ID, resource pool ID, and custom resource selector labels.
//
//  2. Secondary Filter: Evaluates hardware data criteria by fetching HardwareData CRs
//     and applying complex matching logic for CPU, memory, network, and storage requirements.
//
// This approach optimizes performance by reducing the number of BMHs that need detailed
// hardware evaluation while supporting sophisticated hardware-based selection criteria.
package controller

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// ============================================================================
// Hardware Data Field Constants
// ============================================================================

const (
	// HardwareDataPrefix is the prefix used to identify hardware data criteria
	// in resource selector keys. Only keys starting with this prefix are processed
	// by the secondary filter.
	HardwareDataPrefix = "hardwaredata/"

	// Hardware data field names - these correspond to fields in the HardwareDetails struct
	FieldCPUArch      = "cpu_arch"     // CPU architecture (e.g., "x86_64", "arm64")
	FieldCPUModel     = "cpu_model"    // CPU model string
	FieldNumThreads   = "num_threads"  // Number of CPU threads
	FieldRAM          = "ramMebibytes" // RAM size in mebibytes
	FieldNIC          = "nics"         // Network interface criteria
	FieldStorage      = "storage"      // Storage device criteria
	FieldManufacturer = "manufacturer" // System manufacturer
	FieldProductName  = "productname"  // System product name
)

// ============================================================================
// String Qualifier Constants
// ============================================================================

const (
	// String qualifier names - used for string field matching behavior
	QualifierSubstring = "substring" // Match substring within field value
	QualifierICase     = "icase"     // Case-insensitive matching
)

// ============================================================================
// Integer Comparison Constants
// ============================================================================

const (
	// Integer comparison qualifier names - used for numeric field comparisons
	QualifierGT  = "gt"  // Greater than
	QualifierGTE = "gte" // Greater than or equal
	QualifierLT  = "lt"  // Less than
	QualifierLTE = "lte" // Less than or equal
	QualifierEQ  = "eq"  // Equal
	QualifierNEQ = "neq" // Not equal
)

// ============================================================================
// Operator Symbol Constants
// ============================================================================

const (
	// Operator symbols for comparisons - alternative syntax for qualifiers
	OpEqual              = "="  // Equal
	OpEqualStrict        = "==" // Strict equal
	OpNotEqual           = "!=" // Not equal
	OpGreaterThan        = ">"  // Greater than
	OpGreaterThanOrEqual = ">=" // Greater than or equal
	OpLessThan           = "<"  // Less than
	OpLessThanOrEqual    = "<=" // Less than or equal
	OpContains           = "~"  // Contains substring
	OpNotContains        = "!~" // Does not contain substring
)

// ============================================================================
// Types and Variables
// ============================================================================

// OpQualifier represents a parsed qualifier with operator and value
type OpQualifier struct {
	Op    string
	Value string
}

// OpQualifierSet is a map of qualifier keys to their parsed OpQualifier objects
type OpQualifierSet map[string]OpQualifier

// REPatternHardwareData matches hardware data criteria keys and extracts the field and qualifiers.
// It captures everything after the "hardwaredata/" prefix as group 1, which contains
// the field name and optional qualifiers separated by semicolons.
// Example: "hardwaredata/cpu_arch;icase" -> captures "cpu_arch;icase"
var REPatternHardwareData = regexp.MustCompile(`^` + HardwareDataPrefix + `(.*)`)

// REPatternQualifierOp parses qualifier strings in "key<operator>value" format.
// Captures: group 1 = key, group 2 = operator, group 3 = value
// Example: "speedGbps>=10" -> captures ["speedGbps", ">=", "10"]
var REPatternQualifierOp = regexp.MustCompile(`^([^!<>=~]+)([!<>=~]+)(.*)$`)

// ============================================================================
// Primary Filter Functions (Label-based filtering)
// ============================================================================

// ResourceSelectionPrimaryFilter creates Kubernetes client list options for the primary filtering stage.
// This function builds label selectors to filter BareMetalHosts by allocation status, site ID,
// resource pool ID, and other label-based criteria. Hardware data criteria are excluded from
// this stage and handled by ResourceSelectionSecondaryFilter.
//
// The primary filter includes:
// - Allocation status: excludes BMHs with allocated=true label
// - Site ID: matches the specified site if provided
// - Resource pool ID: matches the pool ID from nodeGroupData if provided
// - Resource selector labels: applies non-hardware data labels from nodeGroupData.ResourceSelector
//
// Example usage:
//
//	nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
//	  ResourcePoolId: "pool1",
//	  ResourceSelector: map[string]string{
//	    "zone": "east",
//	    "hardwaredata/cpu_arch": "x86_64", // This will be skipped in primary filter
//	  },
//	}
//	opts, err := ResourceSelectionPrimaryFilter(ctx, client, logger, "site1", nodeGroupData)
//	if err != nil {
//	  return err
//	}
//	// Use opts with client.List() to fetch filtered BMHs
//
// Returns a slice of client.ListOption that can be used with client.List() to fetch BMHs.
func ResourceSelectionPrimaryFilter(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	site string,
	nodeGroupData hwmgmtv1alpha1.NodeGroupData) ([]client.ListOption, error) {

	opts := []client.ListOption{}

	// Fetch only unallocated BMHs
	selector := metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      BmhAllocatedLabel,
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   []string{ValueTrue}, // Exclude allocated=true
			},
		},
	}
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, fmt.Errorf("failed to create label selector: %w", err)
	}
	opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})

	matchingLabels := make(client.MatchingLabels)

	// Add site ID filter if provided
	if site != "" {
		matchingLabels[LabelSiteID] = site
	}

	// Add pool ID filter if provided
	if nodeGroupData.ResourcePoolId != "" {
		matchingLabels[LabelResourcePoolID] = nodeGroupData.ResourcePoolId
	}

	for key, value := range nodeGroupData.ResourceSelector {
		fullLabelName := key

		// HardwareData criteria is processed by the secondary filter, so we skip it here
		if REPatternHardwareData.MatchString(fullLabelName) {
			continue
		}

		if !REPatternResourceSelectorLabel.MatchString(fullLabelName) {
			fullLabelName = LabelPrefixResourceSelector + key
		}

		matchingLabels[fullLabelName] = value
	}

	opts = append(opts, matchingLabels)

	return opts, nil
}

// ============================================================================
// Secondary Filter Functions (Hardware data filtering)
// ============================================================================

// ResourceSelectionSecondaryFilter applies hardware data criteria to filter BareMetalHosts.
// This function processes BMHs that passed the primary filter and evaluates them against
// hardware data criteria specified in nodeGroupData.ResourceSelector. Only criteria with
// the "hardwaredata/" prefix are processed in this stage.
//
// Supported hardware data fields:
// - cpu_arch: CPU architecture (e.g., "x86_64", "arm64")
// - cpu_model: CPU model string
// - num_threads: Number of CPU threads (supports comparison operators)
// - ramMebibytes: RAM size in mebibytes (supports comparison operators)
// - nic: Network interface presence (value must be "present")
// - storage: Storage device presence (value must be "present")
// - manufacturer: System manufacturer string
// - productname: System product name string
//
// String qualifiers:
// - substring: Match substring within the field value
// - icase: Case-insensitive matching
//
// Integer qualifiers and operators:
// - gt, >: Greater than
// - gte, >=: Greater than or equal
// - lt, <: Less than
// - lte, <=: Less than or equal
// - eq, ==: Equal
// - neq, !=: Not equal
//
// NIC and storage qualifiers use key=value format:
// - model=<value>: Match device model
// - speedGbps=<value>: Match NIC speed (integer operators supported)
// - count=<value>: Match device count (integer operators supported)
// - type=<value>: Match storage type (HDD, SSD, etc.)
// - sizeBytes=<value>: Match storage size (integer operators supported)
// - vendor=<value>: Match storage vendor
// - name=<value>: Match storage name
//
// Example usage:
//
//	nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
//	  ResourceSelector: map[string]string{
//	    "hardwaredata/cpu_arch": "x86_64",
//	    "hardwaredata/num_threads;gte": "8",
//	    "hardwaredata/ramMebibytes;gt": "16384",
//	    "hardwaredata/nic;model=Intel": "present",
//	    "hardwaredata/storage;type=SSD;count>=2": "present",
//	  },
//	}
//	filtered, err := ResourceSelectionSecondaryFilter(ctx, client, logger, nodeGroupData, bmhList)
//
// Returns a filtered BareMetalHostList containing only BMHs that match all hardware criteria.
// BMHs without corresponding HardwareData CRs are excluded but logged as errors.
// BMHs that are not in "Available" state are excluded.
func ResourceSelectionSecondaryFilter(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	nodeGroupData hwmgmtv1alpha1.NodeGroupData,
	bmhList metal3v1alpha1.BareMetalHostList) (metal3v1alpha1.BareMetalHostList, error) {
	var filteredBMHs metal3v1alpha1.BareMetalHostList
	for _, bmh := range bmhList.Items {
		if bmh.Status.Provisioning.State != metal3v1alpha1.StateAvailable {
			continue
		}

		// Get the corresponding HardwareData CR and check it against the resource selector criteria
		hwdata := &metal3v1alpha1.HardwareData{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: bmh.Namespace, Name: bmh.Name}, hwdata); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.ErrorContext(ctx, "Failed to get HardwareData for BMH, skipping", "bmh", bmh.Name, "error", err)
			} else {
				logger.ErrorContext(ctx, "HardwareData not found for BMH, skipping hardware criteria evaluation", "bmh", bmh.Name)
			}
			continue
		}

		if hwdata.Spec.HardwareDetails == nil {
			continue
		}

		include := true
		for key, value := range nodeGroupData.ResourceSelector {
			rc, err := ResourceSelectionSecondaryFilterHardwareData(ctx, c, logger, key, value, hwdata)
			if err != nil {
				return filteredBMHs, fmt.Errorf("failed to evaluate criteria: criteria=%s, %w", key, err)
			}
			if !rc {
				include = false
				break
			}
		}
		if !include {
			continue
		}

		filteredBMHs.Items = append(filteredBMHs.Items, bmh)
	}
	return filteredBMHs, nil
}

// ResourceSelectionSecondaryFilterHardwareData evaluates a single hardware data criterion.
// This function checks if the given HardwareData matches the specified key-value criterion.
// Only keys with the "hardwaredata/" prefix are processed; other keys return true (pass).
//
// The key format is: "hardwaredata/<field>[;<qualifier1>[;<qualifier2>...]]"
// Examples:
//   - "hardwaredata/cpu_arch" - exact CPU architecture match
//   - "hardwaredata/cpu_model;substring;icase" - case-insensitive substring match in CPU model
//   - "hardwaredata/num_threads;gte" - CPU thread count greater than or equal to value
//   - "hardwaredata/nic;model=Intel;speedGbps>=10" - Intel NICs with speed >= 10 Gbps
//
// Returns true if the hardware data matches the criterion, false otherwise.
func ResourceSelectionSecondaryFilterHardwareData(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	key, value string,
	hwdata *metal3v1alpha1.HardwareData) (bool, error) {
	match := REPatternHardwareData.FindStringSubmatch(key)
	if len(match) != 2 {
		// Skip non-HardwareData criteria
		return true, nil
	}

	// The hardwaredata criteria key is composed of a field with a series of optional qualifiers, delimited by a semicolon.
	// We need to split the key, minus the HardwareDataPrefix, and process each qualifier accordingly.
	parts := strings.Split(match[1], ";")
	qualifiers := parts[1:]

	caseSensitive := true
	if slices.Contains(qualifiers, QualifierICase) {
		caseSensitive = false
		// Delete icase from the list of qualifiers, since we have the caseSensitive bool
		qualifiers = slices.DeleteFunc(qualifiers, func(s string) bool {
			return s == QualifierICase
		})
	}

	switch parts[0] {
	case FieldCPUArch:
		return checkStringWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.CPU.Arch, caseSensitive), nil
	case FieldCPUModel:
		return checkStringWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.CPU.Model, caseSensitive), nil
	case FieldNumThreads:
		rc, err := checkIntWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.CPU.Count)
		if err != nil {
			return false, fmt.Errorf("checkIntWithQualifiers failed: %w", err)
		}
		return rc, nil
	case FieldRAM:
		rc, err := checkIntWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.RAMMebibytes)
		if err != nil {
			return false, fmt.Errorf("checkIntWithQualifiers failed: %w", err)
		}
		return rc, nil
	case FieldNIC:
		rc, err := checkNicsWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.NIC, caseSensitive)
		if err != nil {
			return false, fmt.Errorf("checkNicsWithQualifiers failed: %w", err)
		}
		return rc, nil
	case FieldStorage:
		rc, err := checkStorageWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.Storage, caseSensitive)
		if err != nil {
			return false, fmt.Errorf("checkStorageWithQualifiers failed: %w", err)
		}
		return rc, nil
	case FieldManufacturer:
		return checkStringWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.SystemVendor.Manufacturer, caseSensitive), nil
	case FieldProductName:
		return checkStringWithQualifiers(qualifiers, value, hwdata.Spec.HardwareDetails.SystemVendor.ProductName, caseSensitive), nil
	}
	return true, nil
}

// ============================================================================
// Basic Field Matching Functions
// ============================================================================

// checkStringWithQualifiers performs string matching with optional qualifiers.
// Supports case-insensitive matching and substring matching.
//
// Qualifiers:
// - "substring": Match if value is a substring of fieldValue
// - "icase": Perform case-insensitive matching
// - Both qualifiers can be combined for case-insensitive substring matching
//
// Examples:
//
//	checkStringWithQualifiers([]string{}, "x86_64", "x86_64") // true (exact match)
//	checkStringWithQualifiers([]string{"icase"}, "X86_64", "x86_64") // true
//	checkStringWithQualifiers([]string{"substring"}, "Intel", "Intel Xeon") // true
//	checkStringWithQualifiers([]string{"substring", "icase"}, "INTEL", "Intel Xeon") // true
func checkStringWithQualifiers(qualifiers []string, value, fieldValue string, caseSensitive bool) bool {
	if !caseSensitive {
		fieldValue = strings.ToLower(fieldValue)
		value = strings.ToLower(value)
	}

	substringMatch := slices.Contains(qualifiers, QualifierSubstring)
	if substringMatch {
		return strings.Contains(fieldValue, value)
	}

	return fieldValue == value
}

// checkIntWithQualifiers performs integer comparison with optional qualifiers.
// Supports various comparison operators for numeric hardware data fields.
//
// Supported qualifiers and operators:
// - "gt", ">": Greater than
// - "gte", ">=": Greater than or equal
// - "lt", "<": Less than
// - "lte", "<=": Less than or equal
// - "eq", "==", "=": Equal
// - "neq", "!=": Not equal
//
// Examples:
//
//	checkIntWithQualifiers([]string{}, "8", 8) // true (exact match when no qualifier)
//	checkIntWithQualifiers([]string{"gt"}, "4", 8) // true (8 > 4)
//	checkIntWithQualifiers([]string{">="}, "8", 8) // true (8 >= 8)
//	checkIntWithQualifiers([]string{"lt"}, "16", 8) // true (8 < 16)
func checkIntWithQualifiers(qualifiers []string, value string, fieldValue int) (bool, error) {
	valueInt, err := strconv.Atoi(value)
	if err != nil {
		return false, fmt.Errorf("invalid value: %s", value)
	}

	if len(qualifiers) == 0 {
		return fieldValue == valueInt, nil
	}

	if len(qualifiers) != 1 {
		return false, fmt.Errorf("supports at most one qualifier")
	}

	switch qualifiers[0] {
	case QualifierGT, OpGreaterThan:
		return fieldValue > valueInt, nil
	case QualifierGTE, OpGreaterThanOrEqual:
		return fieldValue >= valueInt, nil
	case QualifierLT, OpLessThan:
		return fieldValue < valueInt, nil
	case QualifierLTE, OpLessThanOrEqual:
		return fieldValue <= valueInt, nil
	case QualifierEQ, OpEqual, OpEqualStrict:
		return fieldValue == valueInt, nil
	case QualifierNEQ, OpNotEqual:
		return fieldValue != valueInt, nil
	default:
		return false, fmt.Errorf("invalid qualifier: %s", qualifiers[0])
	}
}

// checkStringWithOpQualifier performs string comparison using operator qualifiers.
// This function handles string operations with explicit operators.
func checkStringWithOpQualifier(op, value, fieldValue string, caseSensitive bool) (bool, error) {
	if !caseSensitive {
		fieldValue = strings.ToLower(fieldValue)
		value = strings.ToLower(value)
	}

	switch op {
	case OpNotEqual:
		return fieldValue != value, nil
	case OpEqual, OpEqualStrict:
		return fieldValue == value, nil
	case OpContains:
		return strings.Contains(fieldValue, value), nil
	case OpNotContains:
		return !strings.Contains(fieldValue, value), nil
	}

	return false, fmt.Errorf("invalid operator: %s", op)
}

// checkIntWithOpQualifier performs integer comparison using operator qualifiers.
// This function handles numeric operations with explicit operators.
func checkIntWithOpQualifier(op, value string, fieldValue int) (bool, error) {
	valueInt, err := strconv.Atoi(value)
	if err != nil {
		return false, fmt.Errorf("invalid value: %s", value)
	}

	switch op {
	case OpEqual, OpEqualStrict:
		return fieldValue == valueInt, nil
	case OpNotEqual:
		return fieldValue != valueInt, nil
	case OpGreaterThan:
		return fieldValue > valueInt, nil
	case OpGreaterThanOrEqual:
		return fieldValue >= valueInt, nil
	case OpLessThan:
		return fieldValue < valueInt, nil
	case OpLessThanOrEqual:
		return fieldValue <= valueInt, nil
	default:
		return false, fmt.Errorf("invalid operator: %s", op)
	}
}

// ============================================================================
// Complex Device Matching Functions
// ============================================================================

// qualifierSetFromQualifiers parses qualifier strings into structured OpQualifier objects.
// Each qualifier string should be in the format "key<operator>value" where operator
// can be =, !=, ~, !~, >, >=, <, <=, ==.
//
// Examples:
//
//	qualifierSetFromQualifiers([]string{"model=Intel", "speedGbps>=10"})
//	// Returns: map["model"]{Op: "=", Value: "Intel"}, map["speedGbps"]{Op: ">=", Value: "10"}
func qualifierSetFromQualifiers(qualifiers []string) (OpQualifierSet, error) {
	qualifierSet := make(OpQualifierSet)
	for _, qualifier := range qualifiers {
		match := REPatternQualifierOp.FindStringSubmatch(qualifier)
		if len(match) != 4 {
			return nil, fmt.Errorf("invalid qualifier: %s", qualifier)
		}
		qualifierSet[match[1]] = OpQualifier{Op: match[2], Value: match[3]}
	}
	return qualifierSet, nil
}

// checkNicsWithQualifiers evaluates NIC presence and characteristics against criteria.
// The value parameter must be "present". Qualifiers specify matching criteria for NICs.
//
// Supported qualifiers (key=value format):
// - model=<value>: Match NIC model (supports string operators =, !=, ~, !~)
// - speedGbps=<value>: Match NIC speed (supports integer operators ==, !=, >, >=, <, <=)
// - count=<value>: Match total count of NICs that meet other criteria (supports integer operators)
//
// Examples:
//
//	// Check for presence of any NIC
//	checkNicsWithQualifiers([]string{}, "present", nics)
//
//	// Check for Intel NICs
//	checkNicsWithQualifiers([]string{"model=Intel"}, "present", nics)
//
//	// Check for NICs with speed >= 10 Gbps
//	checkNicsWithQualifiers([]string{"speedGbps>=10"}, "present", nics)
//
//	// Check for at least 2 Intel NICs with speed >= 10 Gbps
//	checkNicsWithQualifiers([]string{"model~Intel", "speedGbps>=10", "count>=2"}, "present", nics)
//
// Returns true if the NIC criteria are satisfied, false otherwise.
func checkNicsWithQualifiers(qualifiers []string, value string, nics []metal3v1alpha1.NIC, caseSensitive bool) (bool, error) {
	qualifierSet, err := qualifierSetFromQualifiers(qualifiers)
	if err != nil {
		return false, fmt.Errorf("qualifierSetFromQualifiers failed: %w", err)
	}

	if value != "present" {
		return false, fmt.Errorf("expected value to be 'present', actual value: %s", value)
	}

	matchingCount := 0
	for _, nic := range nics {
		if model, ok := qualifierSet["model"]; ok {
			rc, err := checkStringWithOpQualifier(model.Op, model.Value, nic.Model, caseSensitive)
			if err != nil {
				return false, fmt.Errorf("checkStringWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		if speedGbps, ok := qualifierSet["speedGbps"]; ok {
			rc, err := checkIntWithOpQualifier(speedGbps.Op, speedGbps.Value, nic.SpeedGbps)
			if err != nil {
				return false, fmt.Errorf("checkIntWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		matchingCount++
	}

	if count, ok := qualifierSet["count"]; ok {
		rc, err := checkIntWithOpQualifier(count.Op, count.Value, matchingCount)
		if err != nil {
			return false, fmt.Errorf("checkIntWithOpQualifier failed: %w", err)
		}
		return rc, nil
	}

	return matchingCount > 0, nil
}

// checkStorageWithQualifiers evaluates storage device presence and characteristics against criteria.
// The value parameter must be "present". Qualifiers specify matching criteria for storage devices.
//
// Supported qualifiers (key=value format):
// - type=<value>: Match storage type (HDD, SSD, etc.) (supports string operators =, !=, ~, !~)
// - sizeBytes=<value>: Match storage size in bytes (supports integer operators ==, !=, >, >=, <, <=)
// - vendor=<value>: Match storage vendor (supports string operators =, !=, ~, !~)
// - model=<value>: Match storage model (supports string operators =, !=, ~, !~)
// - name=<value>: Match storage name or any alternate name (supports string operators =, !=, ~, !~)
// - count=<value>: Match total count of storage devices that meet other criteria (supports integer operators)
//
// Examples:
//
//	// Check for presence of any storage device
//	checkStorageWithQualifiers([]string{}, "present", storage)
//
//	// Check for SSD storage
//	checkStorageWithQualifiers([]string{"type=SSD"}, "present", storage)
//
//	// Check for storage devices larger than 1TB
//	checkStorageWithQualifiers([]string{"sizeBytes>1000000000000"}, "present", storage)
//
//	// Check for at least 2 Samsung SSDs larger than 500GB
//	checkStorageWithQualifiers([]string{"type=SSD", "vendor~Samsung", "sizeBytes>500000000000", "count>=2"}, "present", storage)
//
// Returns true if the storage criteria are satisfied, false otherwise.
func checkStorageWithQualifiers(qualifiers []string, value string, storage []metal3v1alpha1.Storage, caseSensitive bool) (bool, error) {
	qualifierSet, err := qualifierSetFromQualifiers(qualifiers)
	if err != nil {
		return false, fmt.Errorf("qualifierSetFromQualifiers failed: %w", err)
	}

	if value != "present" {
		return false, fmt.Errorf("expected value to be 'present', actual value: %s", value)
	}

	matchingCount := 0
	for _, storage := range storage {
		if diskType, ok := qualifierSet["type"]; ok {
			rc, err := checkStringWithOpQualifier(diskType.Op, diskType.Value, string(storage.Type), caseSensitive)
			if err != nil {
				return false, fmt.Errorf("checkStringWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		if sizeBytes, ok := qualifierSet["sizeBytes"]; ok {
			rc, err := checkIntWithOpQualifier(sizeBytes.Op, sizeBytes.Value, int(storage.SizeBytes))
			if err != nil {
				return false, fmt.Errorf("checkIntWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		if name, ok := qualifierSet["name"]; ok {
			names := storage.AlternateNames
			names = append(names, storage.Name)
			matchFound := false
			for _, itername := range names {
				rc, err := checkStringWithOpQualifier(name.Op, name.Value, itername, caseSensitive)
				if err != nil {
					return false, fmt.Errorf("checkStringWithOpQualifier failed: %w", err)
				}
				if rc {
					matchFound = true
					break
				}
			}
			if !matchFound {
				continue
			}
		}
		if vendor, ok := qualifierSet["vendor"]; ok {
			rc, err := checkStringWithOpQualifier(vendor.Op, vendor.Value, storage.Vendor, caseSensitive)
			if err != nil {
				return false, fmt.Errorf("checkStringWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		if model, ok := qualifierSet["model"]; ok {
			rc, err := checkStringWithOpQualifier(model.Op, model.Value, storage.Model, caseSensitive)
			if err != nil {
				return false, fmt.Errorf("checkStringWithOpQualifier failed: %w", err)
			}
			if !rc {
				continue
			}
		}
		matchingCount++
	}

	if count, ok := qualifierSet["count"]; ok {
		rc, err := checkIntWithOpQualifier(count.Op, count.Value, matchingCount)
		if err != nil {
			return false, fmt.Errorf("checkIntWithOpQualifier failed: %w", err)
		}
		return rc, nil
	}
	return matchingCount > 0, nil
}
