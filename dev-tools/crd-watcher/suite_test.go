/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

/*
CRD Watcher Test Suite

This test suite provides comprehensive unit testing for the CRD Watcher tool components,
including formatters, inventory resource handling, and output generation.

TEST FILES AND COVERAGE:

1. Formatter Tests (formatter_test.go):
   - TableFormatter inventory resource formatting
   - Header generation and field truncation
   - State field extraction from extensions
   - Output formatting verification
   - Error handling for malformed data

2. TUI Formatter Tests (tui_test.go):
   - TUIFormatter inventory resource display
   - Field width calculations for new state columns
   - Truncation logic for MODEL field (25 character limit)
   - Header formatting with new ADMIN, OPER, POWER, USAGE columns
   - buildInventoryResourceLine function validation

3. Inventory Tests (inventory_test.go):
   - InventoryResource to runtime.Object conversion
   - Extension field parsing and validation
   - State field extraction (adminState, operationalState, powerState, usageState)
   - Resource metadata handling

TESTING FRAMEWORK:
- Uses Ginkgo v2 BDD testing framework with Gomega assertions
- Mock inventory resource objects for isolated unit testing
- Comprehensive validation of output formatting
- Field width and truncation testing
- State field extraction validation

KEY VALIDATION AREAS:
- Inventory resource output formatting with new state fields
- MODEL field truncation to 25 characters maximum
- State field extraction from resource extensions
- Header formatting and field alignment
- Error handling for missing or malformed extension data
- Field width calculations for dynamic content
*/

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestCRDWatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CRD Watcher Suite")
}
