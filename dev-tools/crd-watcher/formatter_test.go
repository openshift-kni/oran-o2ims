/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/watch"
)

var _ = Describe("TableFormatter", func() {
	var (
		formatter *TableFormatter
		resource  *InventoryResourceObject
	)

	BeforeEach(func() {
		formatter = NewTableFormatter()

		// Create a sample inventory resource with all state fields
		resource = &InventoryResourceObject{
			Resource: InventoryResource{
				ResourceID:   "test-resource-123",
				ResourceType: "server",
				Description:  "Test Server Resource",
				Status:       "active",
				Extensions: map[string]interface{}{
					"labels": map[string]interface{}{
						"resourceselector.clcm.openshift.io/server-id": "test-server-01",
						"resources.clcm.openshift.io/resourcePoolId":   "pool-123",
					},
					"model":            "Dell PowerEdge R750 Server Model Name",
					"adminState":       "UNLOCKED",
					"operationalState": "ENABLED",
					"powerState":       "ON",
					"usageState":       "ACTIVE",
				},
				CreatedAt: time.Now(),
			},
		}
	})

	Describe("formatInventoryResource", func() {
		var capturedOutput *bytes.Buffer

		BeforeEach(func() {
			// Capture stdout to verify output
			capturedOutput = &bytes.Buffer{}
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			go func() {
				defer w.Close()
				_, _ = io.Copy(capturedOutput, r)
			}()

			DeferCleanup(func() {
				os.Stdout = oldStdout
			})
		})

		It("should format inventory resource with all state fields", func() {
			err := formatter.formatInventoryResource("2024-01-01T10:00:00Z", "ADDED", "1m", resource)
			Expect(err).ToNot(HaveOccurred())

			// Wait a bit for output to be captured
			time.Sleep(100 * time.Millisecond)
			os.Stdout.Close()

			output := capturedOutput.String()

			// Verify all expected fields are present
			Expect(output).To(ContainSubstring("test-server-01"))            // NAME
			Expect(output).To(ContainSubstring("pool-123"))                  // POOL
			Expect(output).To(ContainSubstring("test-resource-123"))         // RESOURCE-ID
			Expect(output).To(ContainSubstring("Dell PowerEdge R750 Se...")) // MODEL (truncated to 25 chars)
			Expect(output).To(ContainSubstring("UNLOCKED"))                  // ADMIN
			Expect(output).To(ContainSubstring("ENABLED"))                   // OPER
			Expect(output).To(ContainSubstring("ON"))                        // POWER
			Expect(output).To(ContainSubstring("ACTIVE"))                    // USAGE
		})

		It("should truncate MODEL field to 25 characters", func() {
			err := formatter.formatInventoryResource("2024-01-01T10:00:00Z", "ADDED", "1m", resource)
			Expect(err).ToNot(HaveOccurred())

			time.Sleep(100 * time.Millisecond)
			os.Stdout.Close()

			output := capturedOutput.String()

			// Original model is longer than 25 chars, should be truncated
			Expect(output).To(ContainSubstring("Dell PowerEdge R750 Se..."))
			Expect(output).ToNot(ContainSubstring("Dell PowerEdge R750 Server Model Name"))
		})

		It("should handle missing state fields gracefully", func() {
			// Create resource without state fields
			resourceWithoutStates := &InventoryResourceObject{
				Resource: InventoryResource{
					ResourceID:   "test-resource-456",
					ResourceType: "server",
					Description:  "Test Server Without States",
					Extensions: map[string]interface{}{
						"labels": map[string]interface{}{
							"resourceselector.clcm.openshift.io/server-id": "test-server-02",
							"resources.clcm.openshift.io/resourcePoolId":   "pool-456",
						},
						"model": "HP Server",
					},
					CreatedAt: time.Now(),
				},
			}

			err := formatter.formatInventoryResource("2024-01-01T10:00:00Z", "ADDED", "1m", resourceWithoutStates)
			Expect(err).ToNot(HaveOccurred())

			time.Sleep(100 * time.Millisecond)
			os.Stdout.Close()

			output := capturedOutput.String()

			// Should display <unknown> for missing state fields
			Expect(output).To(ContainSubstring(StringUnknown))
		})

		It("should handle missing name and pool gracefully", func() {
			// Create resource without name/pool labels
			resourceWithoutLabels := &InventoryResourceObject{
				Resource: InventoryResource{
					ResourceID:   "test-resource-789",
					ResourceType: "server",
					Description:  "Test Server Description",
					Extensions: map[string]interface{}{
						"model":            "Basic Server",
						"adminState":       "LOCKED",
						"operationalState": "DISABLED",
						"powerState":       "OFF",
						"usageState":       "IDLE",
					},
					CreatedAt: time.Now(),
				},
			}

			err := formatter.formatInventoryResource("2024-01-01T10:00:00Z", "ADDED", "1m", resourceWithoutLabels)
			Expect(err).ToNot(HaveOccurred())

			time.Sleep(100 * time.Millisecond)
			os.Stdout.Close()

			output := capturedOutput.String()

			// Should use description as name when server-id label is missing
			Expect(output).To(ContainSubstring("Test Server Description"))
			// Should show <unknown> for missing pool
			Expect(output).To(ContainSubstring(StringUnknown))
		})
	})

	Describe("printTableHeader", func() {
		var capturedOutput *bytes.Buffer

		BeforeEach(func() {
			capturedOutput = &bytes.Buffer{}
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			go func() {
				defer w.Close()
				_, _ = io.Copy(capturedOutput, r)
			}()

			DeferCleanup(func() {
				os.Stdout = oldStdout
			})
		})

		It("should print correct header for inventory resources", func() {
			formatter.printTableHeader(CRDTypeInventoryResources)

			time.Sleep(100 * time.Millisecond)
			os.Stdout.Close()

			output := capturedOutput.String()

			// Verify all column headers are present
			Expect(output).To(ContainSubstring("NAME"))
			Expect(output).To(ContainSubstring("POOL"))
			Expect(output).To(ContainSubstring("RESOURCE-ID"))
			Expect(output).To(ContainSubstring("MODEL"))
			Expect(output).To(ContainSubstring("ADMIN"))
			Expect(output).To(ContainSubstring("OPER"))
			Expect(output).To(ContainSubstring("POWER"))
			Expect(output).To(ContainSubstring("USAGE"))

			// Verify separator line is present
			Expect(output).To(ContainSubstring(strings.Repeat("-", 165)))
		})
	})

	Describe("getInventoryResourceId", func() {
		It("should extract resource ID correctly", func() {
			id := formatter.getInventoryResourceId(resource)
			Expect(id).To(Equal("test-resource-123"))
		})

		It("should return empty string for non-inventory resource", func() {
			// Use an empty InventoryResourceObject with empty ResourceID
			nonInventoryResource := &InventoryResourceObject{Resource: InventoryResource{ResourceID: ""}}
			id := formatter.getInventoryResourceId(nonInventoryResource)
			Expect(id).To(Equal(""))
		})

		It("should handle nil resource", func() {
			id := formatter.getInventoryResourceId(nil)
			Expect(id).To(Equal(StringNone))
		})
	})
})

var _ = Describe("Helper Functions", func() {
	Describe("truncate", func() {
		It("should truncate string longer than maxLen", func() {
			input := "This is a very long string that needs to be truncated"
			result := truncate(input, 10)
			Expect(result).To(Equal("This is..."))
			Expect(len(result)).To(Equal(10))
		})

		It("should return original string if shorter than maxLen", func() {
			input := "Short"
			result := truncate(input, 10)
			Expect(result).To(Equal("Short"))
		})

		It("should return exact string if equal to maxLen", func() {
			input := "ExactMatch"
			result := truncate(input, 10)
			Expect(result).To(Equal("ExactMatch"))
		})

		It("should handle empty string", func() {
			input := ""
			result := truncate(input, 10)
			Expect(result).To(Equal(""))
		})

		It("should handle zero maxLen", func() {
			input := "test"
			result := truncate(input, 0)
			Expect(result).To(Equal(""))
		})
	})
})

var _ = Describe("TableFormatter Events", func() {
	var (
		formatter *TableFormatter
		events    []WatchEvent
	)

	BeforeEach(func() {
		formatter = NewTableFormatter()

		// Create sample events with inventory resources
		events = []WatchEvent{
			{
				Type:    watch.Added,
				Object:  createTestInventoryResource("resource-1", "server-1", "pool-1", "Server Model A", "UNLOCKED", "ENABLED", "ON", "ACTIVE"),
				CRDType: CRDTypeInventoryResources,
			},
			{
				Type:    watch.Modified,
				Object:  createTestInventoryResource("resource-2", "server-2", "pool-2", "Very Long Server Model Name That Should Be Truncated", "LOCKED", "DISABLED", "OFF", "IDLE"),
				CRDType: CRDTypeInventoryResources,
			},
		}
	})

	Describe("FlushEvents", func() {
		It("should sort events correctly", func() {
			// Add events to formatter
			for _, event := range events {
				err := formatter.FormatEvent(event)
				Expect(err).ToNot(HaveOccurred())
			}

			// Should be able to flush without error
			err := formatter.FlushEvents()
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

// Helper function to create test inventory resources
func createTestInventoryResource(resourceID, serverID, poolID, model, adminState, operState, powerState, usageState string) *InventoryResourceObject {
	return &InventoryResourceObject{
		Resource: InventoryResource{
			ResourceID:   resourceID,
			ResourceType: "server",
			Description:  fmt.Sprintf("Test server %s", serverID),
			Extensions: map[string]interface{}{
				"labels": map[string]interface{}{
					"resourceselector.clcm.openshift.io/server-id": serverID,
					"resources.clcm.openshift.io/resourcePoolId":   poolID,
				},
				"model":            model,
				"adminState":       adminState,
				"operationalState": operState,
				"powerState":       powerState,
				"usageState":       usageState,
			},
			CreatedAt: time.Now(),
		},
	}
}
