/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AllocatedNode Utilities", func() {

	Describe("sanitizeKubernetesName", func() {
		Context("when input is valid", func() {
			It("should return the same name for already valid names", func() {
				input := "metal3-hwplugin-cluster1-namespace-host"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(input))
			})

			It("should handle lowercase alphanumeric with hyphens", func() {
				input := "valid-name-123"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(input))
			})
		})

		Context("when input has invalid characters", func() {
			It("should convert uppercase to lowercase", func() {
				input := "METAL3-HWPLUGIN"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})

			It("should replace underscores with hyphens", func() {
				input := "metal3_hwplugin_test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin-test"))
			})

			It("should replace special characters with hyphens", func() {
				input := "metal3@hwplugin#test$name"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin-test-name"))
			})

			It("should replace spaces with hyphens", func() {
				input := "metal3 hwplugin test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin-test"))
			})

			It("should remove consecutive hyphens", func() {
				input := "metal3---hwplugin--test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin-test"))
			})
		})

		Context("when input has leading/trailing invalid characters", func() {
			It("should remove leading hyphens", func() {
				input := "---metal3-hwplugin"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})

			It("should remove trailing hyphens", func() {
				input := "metal3-hwplugin---"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})

			It("should remove leading special characters", func() {
				input := "@#$metal3-hwplugin"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})

			It("should remove trailing special characters", func() {
				input := "metal3-hwplugin@#$"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})

			It("should handle both leading and trailing invalid characters", func() {
				input := "---@#$metal3-hwplugin@#$---"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwplugin"))
			})
		})

		Context("when input results in empty string", func() {
			It("should return empty string for all invalid characters", func() {
				input := "@#$%^&*()"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(""))
			})

			It("should return empty string for only hyphens", func() {
				input := "-----"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(""))
			})

			It("should return empty string for empty input", func() {
				input := ""
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(""))
			})

			It("should return empty string for only spaces", func() {
				input := "   "
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal(""))
			})
		})

		Context("when testing RFC 1123 compliance", func() {
			It("should start with alphanumeric character", func() {
				result := sanitizeKubernetesName("test-name")
				Expect(result).To(MatchRegexp("^[a-z0-9]"))
			})

			It("should end with alphanumeric character", func() {
				result := sanitizeKubernetesName("test-name")
				Expect(result).To(MatchRegexp("[a-z0-9]$"))
			})

			It("should contain only lowercase alphanumeric characters and hyphens", func() {
				result := sanitizeKubernetesName("Test@Name#123")
				Expect(result).To(MatchRegexp("^[a-z0-9-]+$"))
			})
		})
	})

	Describe("GenerateNodeName", func() {
		var (
			pluginID     string
			clusterID    string
			bmhNamespace string
			bmhName      string
		)

		BeforeEach(func() {
			pluginID = "metal3-hwplugin"
			clusterID = "cluster1"
			bmhNamespace = "openshift-machine-api"
			bmhName = "master-0"
		})

		Context("when all inputs are valid", func() {
			It("should generate deterministic names", func() {
				result1 := GenerateNodeName(pluginID, clusterID, bmhNamespace, bmhName)
				result2 := GenerateNodeName(pluginID, clusterID, bmhNamespace, bmhName)
				Expect(result1).To(Equal(result2))
			})

			It("should include all components in the name", func() {
				result := GenerateNodeName(pluginID, clusterID, bmhNamespace, bmhName)
				expectedFormat := "metal3-hwplugin-cluster1-openshift-machine-api-master-0"
				Expect(result).To(Equal(expectedFormat))
			})

			It("should be Kubernetes compliant", func() {
				result := GenerateNodeName(pluginID, clusterID, bmhNamespace, bmhName)
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
				Expect(len(result)).To(BeNumerically("<=", 253))
			})

			It("should generate different names for different BMH names", func() {
				result1 := GenerateNodeName(pluginID, clusterID, bmhNamespace, "master-0")
				result2 := GenerateNodeName(pluginID, clusterID, bmhNamespace, "master-1")
				Expect(result1).NotTo(Equal(result2))
			})

			It("should generate different names for different namespaces", func() {
				result1 := GenerateNodeName(pluginID, clusterID, "namespace1", bmhName)
				result2 := GenerateNodeName(pluginID, clusterID, "namespace2", bmhName)
				Expect(result1).NotTo(Equal(result2))
			})

			It("should generate different names for different cluster IDs", func() {
				result1 := GenerateNodeName(pluginID, "cluster1", bmhNamespace, bmhName)
				result2 := GenerateNodeName(pluginID, "cluster2", bmhNamespace, bmhName)
				Expect(result1).NotTo(Equal(result2))
			})
		})

		Context("when inputs contain invalid characters", func() {
			It("should sanitize special characters", func() {
				result := GenerateNodeName("metal3@hwplugin", "cluster#1", "namespace_test", "host$name")
				Expect(result).To(MatchRegexp("^[a-z0-9-]+$"))
				Expect(result).To(ContainSubstring("metal3-hwplugin"))
				Expect(result).To(ContainSubstring("cluster-1"))
				Expect(result).To(ContainSubstring("namespace-test"))
				Expect(result).To(ContainSubstring("host-name"))
			})

			It("should handle uppercase characters", func() {
				result := GenerateNodeName("METAL3-HWPLUGIN", "CLUSTER1", "NAMESPACE", "HOST")
				Expect(result).To(Equal("metal3-hwplugin-cluster1-namespace-host"))
			})
		})

		Context("when the generated name is too long", func() {
			It("should use hash-based fallback for very long names", func() {
				longPluginID := strings.Repeat("very-long-plugin-id", 10)
				longClusterID := strings.Repeat("very-long-cluster-id", 10)
				longNamespace := strings.Repeat("very-long-namespace", 10)
				longBMHName := strings.Repeat("very-long-bmh-name", 10)

				result := GenerateNodeName(longPluginID, longClusterID, longNamespace, longBMHName)

				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(HavePrefix(longPluginID[:len(longPluginID)-1])) // Should start with truncated plugin ID
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should generate deterministic hash-based names", func() {
				longName := strings.Repeat("a", 300)
				result1 := GenerateNodeName(longName, longName, longName, longName)
				result2 := GenerateNodeName(longName, longName, longName, longName)
				Expect(result1).To(Equal(result2))
			})

			It("should generate different hash-based names for different inputs", func() {
				longName1 := strings.Repeat("a", 300)
				longName2 := strings.Repeat("b", 300)
				result1 := GenerateNodeName(longName1, "cluster1", "namespace", "bmh")
				result2 := GenerateNodeName(longName2, "cluster1", "namespace", "bmh")
				Expect(result1).NotTo(Equal(result2))
			})
		})

		Context("when inputs result in empty sanitized name", func() {
			It("should use hash-based fallback for empty sanitized names", func() {
				result := GenerateNodeName("@#$", "###", "!!!", "%%%")
				Expect(result).NotTo(BeEmpty())
				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should be deterministic even with invalid characters", func() {
				result1 := GenerateNodeName("@#$", "###", "!!!", "%%%")
				result2 := GenerateNodeName("@#$", "###", "!!!", "%%%")
				Expect(result1).To(Equal(result2))
			})
		})

		Context("when testing edge cases", func() {
			It("should handle empty strings", func() {
				result := GenerateNodeName("", "", "", "")
				Expect(result).NotTo(BeEmpty())
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should handle single characters", func() {
				result := GenerateNodeName("a", "b", "c", "d")
				Expect(result).To(Equal("a-b-c-d"))
			})

			It("should handle names with only hyphens", func() {
				result := GenerateNodeName("---", "---", "---", "---")
				Expect(result).NotTo(BeEmpty())
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})
		})

		Context("when testing name length constraints", func() {
			It("should never exceed 253 characters", func() {
				// Test with various input lengths
				for i := 1; i <= 10; i++ {
					longInput := strings.Repeat("x", i*50)
					result := GenerateNodeName(longInput, longInput, longInput, longInput)
					Expect(len(result)).To(BeNumerically("<=", 253),
						"Failed for input length: %d, result: %s", i*50*4, result)
				}
			})

			It("should handle exactly 253 character inputs gracefully", func() {
				longInput := strings.Repeat("a", 63) // Each component ~63 chars, total ~252 with hyphens
				result := GenerateNodeName(longInput, longInput, longInput, longInput)
				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})
		})

		Context("when validating uniqueness", func() {
			It("should generate unique names for different parameter combinations", func() {
				results := make(map[string]bool)

				plugins := []string{"metal3-hwplugin", "other-plugin"}
				clusters := []string{"cluster1", "cluster2"}
				namespaces := []string{"ns1", "ns2"}
				bmhs := []string{"bmh1", "bmh2"}

				for _, p := range plugins {
					for _, c := range clusters {
						for _, n := range namespaces {
							for _, b := range bmhs {
								result := GenerateNodeName(p, c, n, b)
								Expect(results[result]).To(BeFalse(),
									"Duplicate name generated: %s for params (%s, %s, %s, %s)",
									result, p, c, n, b)
								results[result] = true
							}
						}
					}
				}

				Expect(len(results)).To(Equal(16)) // 2^4 combinations
			})
		})
	})
})
