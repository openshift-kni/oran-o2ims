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
				input := "metal3-hwmgr-cluster1-namespace-host"
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
				input := "METAL3-HWMGR"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
			})

			It("should replace underscores with hyphens", func() {
				input := "metal3_hwmgr_test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr-test"))
			})

			It("should replace special characters with hyphens", func() {
				input := "metal3@hwmgr#test$name"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr-test-name"))
			})

			It("should replace spaces with hyphens", func() {
				input := "metal3 hwmgr test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr-test"))
			})

			It("should remove consecutive hyphens", func() {
				input := "metal3---hwmgr--test"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr-test"))
			})
		})

		Context("when input has leading/trailing invalid characters", func() {
			It("should remove leading hyphens", func() {
				input := "---metal3-hwmgr"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
			})

			It("should remove trailing hyphens", func() {
				input := "metal3-hwmgr---"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
			})

			It("should remove leading special characters", func() {
				input := "@#$metal3-hwmgr"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
			})

			It("should remove trailing special characters", func() {
				input := "metal3-hwmgr@#$"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
			})

			It("should handle both leading and trailing invalid characters", func() {
				input := "---@#$metal3-hwmgr@#$---"
				result := sanitizeKubernetesName(input)
				Expect(result).To(Equal("metal3-hwmgr"))
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
			clusterID    string
			bmhNamespace string
			bmhName      string
		)

		BeforeEach(func() {
			clusterID = "cluster1"
			bmhNamespace = "openshift-machine-api"
			bmhName = "master-0"
		})

		Context("when all inputs are valid", func() {
			It("should generate deterministic names", func() {
				result1 := GenerateNodeName(clusterID, bmhNamespace, bmhName)
				result2 := GenerateNodeName(clusterID, bmhNamespace, bmhName)
				Expect(result1).To(Equal(result2))
			})

			It("should include all components in the name", func() {
				result := GenerateNodeName(clusterID, bmhNamespace, bmhName)
				Expect(result).To(Equal("cluster1-openshift-machine-api-master-0"))
			})

			It("should be Kubernetes compliant", func() {
				result := GenerateNodeName(clusterID, bmhNamespace, bmhName)
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
				Expect(len(result)).To(BeNumerically("<=", 253))
			})

			It("should generate different names for different BMH names", func() {
				result1 := GenerateNodeName(clusterID, bmhNamespace, "master-0")
				result2 := GenerateNodeName(clusterID, bmhNamespace, "master-1")
				Expect(result1).NotTo(Equal(result2))
			})

			It("should generate different names for different namespaces", func() {
				result1 := GenerateNodeName(clusterID, "namespace1", bmhName)
				result2 := GenerateNodeName(clusterID, "namespace2", bmhName)
				Expect(result1).NotTo(Equal(result2))
			})

			It("should generate different names for different cluster IDs", func() {
				result1 := GenerateNodeName("cluster1", bmhNamespace, bmhName)
				result2 := GenerateNodeName("cluster2", bmhNamespace, bmhName)
				Expect(result1).NotTo(Equal(result2))
			})
		})

		Context("when inputs contain invalid characters", func() {
			It("should sanitize special characters", func() {
				result := GenerateNodeName("cluster#1", "namespace_test", "host$name")
				Expect(result).To(MatchRegexp("^[a-z0-9-]+$"))
				Expect(result).To(ContainSubstring("cluster-1"))
				Expect(result).To(ContainSubstring("namespace-test"))
				Expect(result).To(ContainSubstring("host-name"))
			})

			It("should handle uppercase characters", func() {
				result := GenerateNodeName("CLUSTER1", "NAMESPACE", "HOST")
				Expect(result).To(Equal("cluster1-namespace-host"))
			})
		})

		Context("when the generated name is too long", func() {
			It("should use hash-based fallback for very long names", func() {
				longClusterID := strings.Repeat("very-long-cluster-id", 10)
				longNamespace := strings.Repeat("very-long-namespace", 10)
				longBMHName := strings.Repeat("very-long-bmh-name", 10)

				result := GenerateNodeName(longClusterID, longNamespace, longBMHName)

				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should generate deterministic hash-based names", func() {
				longName := strings.Repeat("a", 300)
				result1 := GenerateNodeName(longName, longName, longName)
				result2 := GenerateNodeName(longName, longName, longName)
				Expect(result1).To(Equal(result2))
			})

			It("should generate different hash-based names for different inputs", func() {
				longName1 := strings.Repeat("a", 300)
				longName2 := strings.Repeat("b", 300)
				result1 := GenerateNodeName(longName1, "namespace", "bmh")
				result2 := GenerateNodeName(longName2, "namespace", "bmh")
				Expect(result1).NotTo(Equal(result2))
			})
		})

		Context("when inputs result in empty sanitized name", func() {
			It("should use hash-based fallback for empty sanitized names", func() {
				result := GenerateNodeName("###", "!!!", "%%%")
				Expect(result).NotTo(BeEmpty())
				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should be deterministic even with invalid characters", func() {
				result1 := GenerateNodeName("###", "!!!", "%%%")
				result2 := GenerateNodeName("###", "!!!", "%%%")
				Expect(result1).To(Equal(result2))
			})
		})

		Context("when testing edge cases", func() {
			It("should handle empty strings", func() {
				result := GenerateNodeName("", "", "")
				Expect(result).NotTo(BeEmpty())
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})

			It("should handle single characters", func() {
				result := GenerateNodeName("a", "b", "c")
				Expect(result).To(Equal("a-b-c"))
			})

			It("should handle names with only hyphens", func() {
				result := GenerateNodeName("---", "---", "---")
				Expect(result).NotTo(BeEmpty())
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})
		})

		Context("when testing name length constraints", func() {
			It("should never exceed 253 characters", func() {
				for i := 1; i <= 10; i++ {
					longInput := strings.Repeat("x", i*50)
					result := GenerateNodeName(longInput, longInput, longInput)
					Expect(len(result)).To(BeNumerically("<=", 253),
						"Failed for input length: %d, result: %s", i*50*3, result)
				}
			})

			It("should handle exactly 253 character inputs gracefully", func() {
				// 83 + 1 + 83 + 1 + 85 = 253 (tests the exact boundary, not the hash fallback)
				clusterID := strings.Repeat("a", 83)
				bmhNamespace := strings.Repeat("a", 83)
				bmhName := strings.Repeat("a", 85)
				result := GenerateNodeName(clusterID, bmhNamespace, bmhName)
				Expect(len(result)).To(BeNumerically("<=", 253))
				Expect(result).To(MatchRegexp("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
			})
		})

		Context("when validating uniqueness", func() {
			It("should generate unique names for different parameter combinations", func() {
				results := make(map[string]bool)

				clusters := []string{"cluster1", "cluster2"}
				namespaces := []string{"ns1", "ns2"}
				bmhs := []string{"bmh1", "bmh2"}

				for _, c := range clusters {
					for _, n := range namespaces {
						for _, b := range bmhs {
							result := GenerateNodeName(c, n, b)
							Expect(results[result]).To(BeFalse(),
								"Duplicate name generated: %s for params (%s, %s, %s)",
								result, c, n, b)
							results[result] = true
						}
					}
				}

				Expect(len(results)).To(Equal(8)) // 2^3 combinations
			})
		})
	})
})
