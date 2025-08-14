/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

package main

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/watch"
)

var _ = Describe("TUIFormatter", func() {
	var (
		formatter *TUIFormatter
		resource  *InventoryResourceObject
	)

	BeforeEach(func() {
		// Create TUIFormatter with test configuration
		formatter = &TUIFormatter{
			refreshInterval: 2 * time.Second,
			watchedCRDTypes: []string{CRDTypeInventoryResources},
			events:          make([]WatchEvent, 0),
			verifyFunc:      func(event WatchEvent) bool { return true },
			maxEvents:       100,
			isTerminal:      false,
		}

		// Create sample inventory resource with all state fields
		resource = &InventoryResourceObject{
			Resource: InventoryResource{
				ResourceID:   "test-resource-abc123",
				ResourceType: "server",
				Description:  "Test Server Resource",
				Status:       "active",
				Extensions: map[string]interface{}{
					"labels": map[string]interface{}{
						"resourceselector.clcm.openshift.io/server-id": "test-server-name",
						"resources.clcm.openshift.io/resourcePoolId":   "resource-pool-id",
					},
					"model":            "Dell PowerEdge R750 Ultra Long Model Name That Should Be Truncated",
					"adminState":       "UNLOCKED",
					"operationalState": "ENABLED",
					"powerState":       "ON",
					"usageState":       "ACTIVE",
				},
				CreatedAt: time.Now(),
			},
		}
	})

	Describe("FieldWidths struct", func() {
		It("should have Field8 member for usage state", func() {
			widths := FieldWidths{
				Field8: 10,
			}
			Expect(widths.Field8).To(Equal(10))
		})
	})

	Describe("initializeHeaderWidths", func() {
		It("should set correct minimum widths for inventory resources", func() {
			widths := formatter.initializeHeaderWidths(CRDTypeInventoryResources)

			Expect(widths.Field1).To(BeNumerically(">=", len("NAME")))
			Expect(widths.Field2).To(BeNumerically(">=", len("POOL")))
			Expect(widths.Field3).To(BeNumerically(">=", len("RESOURCE-ID")))
			Expect(widths.Field4).To(BeNumerically(">=", len("MODEL")))
			Expect(widths.Field5).To(BeNumerically(">=", len("ADMIN")))
			Expect(widths.Field6).To(BeNumerically(">=", len("OPER")))
			Expect(widths.Field7).To(BeNumerically(">=", len("POWER")))
			Expect(widths.Field8).To(BeNumerically(">=", len("USAGE")))
		})
	})

	Describe("calculateInventoryResourceWidths", func() {
		var events []WatchEvent

		BeforeEach(func() {
			events = []WatchEvent{
				{
					Type:    watch.Added,
					Object:  resource,
					CRDType: CRDTypeInventoryResources,
				},
				{
					Type:    watch.Added,
					Object:  createTestInventoryResource("short-id", "name", "pool", "Short Model", "LOCKED", "DISABLED", "OFF", "IDLE"),
					CRDType: CRDTypeInventoryResources,
				},
			}
		})

		It("should calculate correct field widths based on data", func() {
			widths := FieldWidths{}
			result := formatter.calculateInventoryResourceWidths(events, widths)

			// Verify widths are calculated based on actual data
			Expect(result.Field1).To(BeNumerically(">=", len("test-server-name")))
			Expect(result.Field2).To(BeNumerically(">=", len("resource-pool-id")))
			Expect(result.Field3).To(BeNumerically(">=", len("test-resource-abc123")))
			Expect(result.Field4).To(BeNumerically(">=", len("Dell PowerEdge R750 Ultra Long Model Name That Should Be Truncated")))
			Expect(result.Field5).To(BeNumerically(">=", len("UNLOCKED")))
			Expect(result.Field6).To(BeNumerically(">=", len("ENABLED")))
			Expect(result.Field7).To(BeNumerically(">=", len("ON")))
			Expect(result.Field8).To(BeNumerically(">=", len("ACTIVE")))
		})

		It("should handle resources with missing state fields", func() {
			resourceWithoutStates := createTestInventoryResource("test-id", "test-name", "test-pool", "Test Model", "", "", "", "")
			events := []WatchEvent{
				{
					Type:    "ADDED",
					Object:  resourceWithoutStates,
					CRDType: CRDTypeInventoryResources,
				},
			}

			widths := FieldWidths{}
			result := formatter.calculateInventoryResourceWidths(events, widths)

			// Should account for "<unknown>" string length
			Expect(result.Field5).To(BeNumerically(">=", len(StringUnknown)))
			Expect(result.Field6).To(BeNumerically(">=", len(StringUnknown)))
			Expect(result.Field7).To(BeNumerically(">=", len(StringUnknown)))
			Expect(result.Field8).To(BeNumerically(">=", len(StringUnknown)))
		})

		It("should skip non-inventory resource objects", func() {
			nonInventoryEvents := []WatchEvent{
				{
					Type:    watch.Added,
					Object:  &ResourcePoolObject{}, // Different type of inventory object
					CRDType: CRDTypeInventoryResources,
				},
			}

			widths := FieldWidths{Field1: 5, Field2: 5}
			result := formatter.calculateInventoryResourceWidths(nonInventoryEvents, widths)

			// Widths should remain unchanged
			Expect(result.Field1).To(Equal(5))
			Expect(result.Field2).To(Equal(5))
		})
	})

	Describe("applyMaxWidthLimits", func() {
		It("should limit MODEL field to 25 characters", func() {
			widths := FieldWidths{
				Field4: 50, // MODEL field - should be limited to 25
				Field5: 30, // Other fields should use default maxWidth (50)
			}

			result := formatter.applyMaxWidthLimits(widths)

			Expect(result.Field4).To(Equal(25)) // MODEL field limited to 25
			Expect(result.Field5).To(Equal(30)) // Other field unchanged (under limit)
		})

		It("should apply maxWidth to other fields", func() {
			widths := FieldWidths{
				Field1: 100, // Should be limited to 50
				Field2: 75,  // Should be limited to 50
				Field4: 30,  // MODEL field - should remain 30 (under 25 limit)
				Field5: 60,  // Should be limited to 50
			}

			result := formatter.applyMaxWidthLimits(widths)

			Expect(result.Field1).To(Equal(50))
			Expect(result.Field2).To(Equal(50))
			Expect(result.Field4).To(Equal(25)) // MODEL gets limited to 25, not 50
			Expect(result.Field5).To(Equal(50))
		})
	})

	Describe("buildCRDTableHeader", func() {
		It("should generate correct header for inventory resources", func() {
			widths := FieldWidths{
				Field1: 15, // NAME
				Field2: 20, // POOL
				Field3: 25, // RESOURCE-ID
				Field4: 25, // MODEL
				Field5: 10, // ADMIN
				Field6: 10, // OPER
				Field7: 8,  // POWER
				Field8: 10, // USAGE
			}

			var sb strings.Builder
			formatter.buildCRDTableHeader(CRDTypeInventoryResources, widths, &sb)

			header := sb.String()

			// Verify all column headers are present with correct formatting
			Expect(header).To(ContainSubstring("NAME"))
			Expect(header).To(ContainSubstring("POOL"))
			Expect(header).To(ContainSubstring("RESOURCE-ID"))
			Expect(header).To(ContainSubstring("MODEL"))
			Expect(header).To(ContainSubstring("ADMIN"))
			Expect(header).To(ContainSubstring("OPER"))
			Expect(header).To(ContainSubstring("POWER"))
			Expect(header).To(ContainSubstring("USAGE"))

			// Verify proper spacing
			Expect(header).To(ContainSubstring("│"))
		})
	})

	Describe("buildInventoryResourceLine", func() {
		It("should build correct line with all fields", func() {
			widths := FieldWidths{
				Field1: 20, // NAME
				Field2: 20, // POOL
				Field3: 25, // RESOURCE-ID
				Field4: 25, // MODEL
				Field5: 15, // ADMIN
				Field6: 15, // OPER
				Field7: 8,  // POWER
				Field8: 10, // USAGE
			}

			var sb strings.Builder
			formatter.buildInventoryResourceLine("1m", resource, widths, &sb)

			line := sb.String()

			// Verify all data fields are present
			Expect(line).To(ContainSubstring("test-server-name"))
			Expect(line).To(ContainSubstring("resource-pool-id"))
			Expect(line).To(ContainSubstring("test-resource-abc123"))
			Expect(line).To(ContainSubstring("Dell PowerEdge R750 Ul...")) // Truncated model
			Expect(line).To(ContainSubstring("UNLOCKED"))
			Expect(line).To(ContainSubstring("ENABLED"))
			Expect(line).To(ContainSubstring("ON"))
			Expect(line).To(ContainSubstring("ACTIVE"))

			// Verify proper formatting
			Expect(line).To(ContainSubstring("│"))
		})

		It("should handle missing extension data gracefully", func() {
			resourceWithoutExtensions := &InventoryResourceObject{
				Resource: InventoryResource{
					ResourceID:  "test-resource-456",
					Description: "Test Description",
					Extensions:  nil,
				},
			}

			widths := FieldWidths{
				Field1: 20, Field2: 20, Field3: 25, Field4: 25,
				Field5: 15, Field6: 15, Field7: 8, Field8: 10,
			}

			var sb strings.Builder
			formatter.buildInventoryResourceLine("1m", resourceWithoutExtensions, widths, &sb)

			line := sb.String()

			// Should display fallback values
			Expect(line).To(ContainSubstring("Test Description")) // Uses description as name
			Expect(line).To(ContainSubstring(StringUnknown))      // For missing fields
		})

		It("should truncate MODEL field correctly", func() {
			widths := FieldWidths{
				Field1: 20, Field2: 20, Field3: 25, Field4: 25,
				Field5: 15, Field6: 15, Field7: 8, Field8: 10,
			}

			var sb strings.Builder
			formatter.buildInventoryResourceLine("1m", resource, widths, &sb)

			line := sb.String()

			// Original model is longer than 25 chars, should be truncated
			originalModel := "Dell PowerEdge R750 Ultra Long Model Name That Should Be Truncated"
			Expect(len(originalModel)).To(BeNumerically(">", 25))

			// Should contain truncated version with "..."
			truncatedModel := originalModel[:22] + "..." // 25 chars total
			Expect(line).To(ContainSubstring(truncatedModel))
			Expect(line).ToNot(ContainSubstring(originalModel))
		})

		It("should handle non-inventory resource objects gracefully", func() {
			nonInventoryResource := &ResourcePoolObject{}
			widths := FieldWidths{Field1: 10, Field2: 10, Field3: 10, Field4: 10}

			var sb strings.Builder
			formatter.buildInventoryResourceLine("1m", nonInventoryResource, widths, &sb)

			// Should not add anything to the string builder
			Expect(sb.String()).To(Equal(""))
		})
	})

	Describe("getResourceKey", func() {
		It("should generate unique key for inventory resource", func() {
			event := WatchEvent{
				Type:    watch.Added,
				Object:  resource,
				CRDType: CRDTypeInventoryResources,
			}

			key := formatter.getResourceKey(event)
			expected := fmt.Sprintf("%s/%s", CRDTypeInventoryResources, resource.Resource.ResourceID)
			Expect(key).To(Equal(expected))
		})

		It("should handle non-inventory resource", func() {
			event := WatchEvent{
				Type:    watch.Added,
				Object:  &ResourcePoolObject{},
				CRDType: CRDTypeInventoryResources,
			}

			key := formatter.getResourceKey(event)
			// Should fall back to generic handling
			Expect(key).ToNot(BeEmpty())
		})
	})

	Describe("Integration Tests", func() {
		It("should handle complete inventory resource formatting workflow", func() {
			// Set isTerminal to true to avoid fallback to TableFormatter which has initialization issues
			formatter.isTerminal = true

			event := WatchEvent{
				Type:    watch.Added,
				Object:  resource,
				CRDType: CRDTypeInventoryResources,
			}

			// Format event (adds to internal events slice)
			err := formatter.FormatEvent(event)
			Expect(err).ToNot(HaveOccurred())

			// Verify event was stored (events are added to the slice)
			// Note: FormatEvent should add the event since it's not found and not a delete
			Expect(len(formatter.events)).To(Equal(1))
			Expect(formatter.events[0].Type).To(Equal(watch.Added))
			Expect(formatter.events[0].CRDType).To(Equal(CRDTypeInventoryResources))
		})
	})
})

var _ = Describe("Helper Functions", func() {
	Describe("truncateToWidth", func() {
		It("should truncate string to exact width", func() {
			input := "This is a long string that needs truncation"
			result := truncateToWidth(input, 15)
			Expect(result).To(Equal("This is a lo..."))
			Expect(len(result)).To(Equal(15))
		})

		It("should return original string if shorter", func() {
			input := "Short"
			result := truncateToWidth(input, 20)
			Expect(result).To(Equal("Short"))
		})

		It("should handle empty string", func() {
			result := truncateToWidth("", 10)
			Expect(result).To(Equal(""))
		})

		It("should handle zero width", func() {
			result := truncateToWidth("test", 0)
			Expect(result).To(Equal(""))
		})
	})

	Describe("safeMax", func() {
		It("should return maximum of two integers", func() {
			Expect(safeMax(5, 10)).To(Equal(10))
			Expect(safeMax(15, 8)).To(Equal(15))
			Expect(safeMax(7, 7)).To(Equal(7))
		})
	})

	Describe("safeMin", func() {
		It("should return minimum of two integers", func() {
			Expect(safeMin(5, 10)).To(Equal(5))
			Expect(safeMin(15, 8)).To(Equal(8))
			Expect(safeMin(7, 7)).To(Equal(7))
		})
	})
})
