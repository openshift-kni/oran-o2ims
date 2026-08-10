/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DeepMergeMaps", func() {
	var (
		dst map[string]interface{}
		src map[string]interface{}
	)

	BeforeEach(func() {
		dst = make(map[string]interface{})
		src = make(map[string]interface{})
	})

	It("should merge non-conflicting keys", func() {
		dst = map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}

		src = map[string]interface{}{
			"key3": "value3",
			"key4": "value4",
		}

		expected := map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
			"key4": "value4",
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should override conflicting keys with src values", func() {
		dst = map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}

		src = map[string]interface{}{
			"key2": "new_value2",
			"key3": "value3",
		}

		expected := map[string]interface{}{
			"key1": "value1",
			"key2": "new_value2",
			"key3": "value3",
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should recursively merge nested maps", func() {
		dst = map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey1": "subvalue1",
				"subkey2": "subvalue2",
			},
		}

		src = map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey2": "new_subvalue2",
				"subkey3": "subvalue3",
			},
		}

		expected := map[string]interface{}{
			"key1": map[string]interface{}{
				"subkey1": "subvalue1",
				"subkey2": "new_subvalue2",
				"subkey3": "subvalue3",
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should deeply merge slices of maps by index", func() {
		dst = map[string]interface{}{
			"outer": []interface{}{
				map[string]interface{}{
					"label": "first",
					"mid": map[string]interface{}{
						"inner": []interface{}{
							map[string]interface{}{
								"subkey1": "subvalue1",
								"subkey2": "subvalue2",
							},
						},
					},
				},
			},
		}

		src = map[string]interface{}{
			"outer": []interface{}{
				map[string]interface{}{
					"mid": map[string]interface{}{
						"inner": []interface{}{
							map[string]interface{}{
								"subkey2": "new_subvalue2",
								"subkey3": "subvalue3",
							},
						},
					},
				},
			},
		}

		expected := map[string]interface{}{
			"outer": []interface{}{
				map[string]interface{}{
					"label": "first",
					"mid": map[string]interface{}{
						"inner": []interface{}{
							map[string]interface{}{
								"subkey1": "subvalue1",
								"subkey2": "new_subvalue2",
								"subkey3": "subvalue3",
							},
						},
					},
				},
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should append elements when src slice is longer than dst slice", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		expected := map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should preserve elements when dst slice is longer than src slice", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
		}

		expected := map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "new_subvalue2",
					"subkey3": "subvalue3",
				},
			},
			"key2": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
					"subkey2": "subvalue2",
				},
			},
		}
		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should return error on type mismatch when checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": "value1",
		}

		src = map[string]interface{}{
			"key1": 10,
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("type mismatch for key: key1"))

		err = DeepMergeMaps(dst, src, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error if type do not match in maps when checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": map[string]interface{}{
				"subKey1": "test",
			},
		}

		src = map[string]interface{}{
			"key1": map[string]interface{}{
				"subKey1": 10,
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging maps for key: key1: type mismatch for key: subKey1 (dst: string, src: int)"))

		err = DeepMergeMaps(dst, src, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error if types do not match in slices and checkType is true, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{"value1"},
		}

		src = map[string]interface{}{
			"key1": []interface{}{10},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging slices for key: key1: type mismatch at path \"key1\" index 0 (dst: string, src: int)"))

		err = DeepMergeMaps(dst, src, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})
	It("should return error when merging slices for key with mismatched types, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				"string_value",
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging slices for key: key1: type mismatch at path \"key1\" index 0 (dst: map[string]interface {}, src: string)"))

		err = DeepMergeMaps(dst, src, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should return error when merging maps at index with mismatched types, and no error when false", func() {
		dst = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": "subvalue1",
				},
			},
		}

		src = map[string]interface{}{
			"key1": []interface{}{
				map[string]interface{}{
					"subkey1": 123, // Type mismatch here
				},
			},
		}

		err := DeepMergeMaps(dst, src, true, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"error merging maps at path \"key1\" slice index 0: type mismatch for key: subkey1"))

		err = DeepMergeMaps(dst, src, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(src))
	})

	It("should deeply merge slices of maps by key via path rules", func() {
		dst = map[string]any{
			"timeout": "30m",
			"groups": []any{
				map[string]any{"name": "controller", "role": "master", "profile": "profile-64G"},
				map[string]any{"name": "worker", "role": "worker", "profile": "profile-128G"},
			},
		}
		src = map[string]any{
			"timeout": "45m",
			"groups": []any{
				map[string]any{"name": "worker", "pool": "pool-east"},
				map[string]any{"name": "extra-worker", "role": "worker", "profile": "profile-256G"},
			},
		}
		expected := map[string]any{
			"timeout": "45m",
			"groups": []any{
				map[string]any{"name": "controller", "role": "master", "profile": "profile-64G"},
				map[string]any{"name": "worker", "role": "worker", "profile": "profile-128G", "pool": "pool-east"},
				map[string]any{"name": "extra-worker", "role": "worker", "profile": "profile-256G"},
			},
		}

		err := DeepMergeMaps(dst, src, false, SliceMergeRules{
			"groups": {Key: "name"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should apply nested path rules with AllowDuplicateKeys", func() {
		dst = map[string]any{
			"groups": []any{
				map[string]any{
					"name": "masters",
					"net": map[string]any{
						"ifaces": []any{
							map[string]any{"name": "eno1", "mac": "aa:aa"},
							map[string]any{"name": "eno2", "mac": "bb:bb"},
						},
					},
				},
			},
		}
		src = map[string]any{
			"groups": []any{
				map[string]any{
					"name": "masters",
					"net": map[string]any{
						"ifaces": []any{
							map[string]any{"name": "eno1", "mac": "cc:cc"},
							map[string]any{"name": "eno1", "mtu": float64(9000)},
						},
					},
				},
			},
		}
		expected := map[string]any{
			"groups": []any{
				map[string]any{
					"name": "masters",
					"net": map[string]any{
						"ifaces": []any{
							map[string]any{"name": "eno1", "mac": "cc:cc", "mtu": float64(9000)},
							map[string]any{"name": "eno2", "mac": "bb:bb"},
						},
					},
				},
			},
		}

		err := DeepMergeMaps(dst, src, false, SliceMergeRules{
			"groups":              {Key: "name"},
			"groups[].net.ifaces": {Key: "name", ByKey: ByKeyMergeOptions{AllowDuplicateKeys: true}},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(dst).To(Equal(expected))
	})

	It("should return error when AllowDuplicateKeys is false and keys are duplicated", func() {
		dst = map[string]any{
			"groups": []any{
				map[string]any{
					"name": "masters",
					"net": map[string]any{
						"ifaces": []any{
							map[string]any{"name": "eno1", "mac": "aa:aa"},
						},
					},
				},
			},
		}
		src = map[string]any{
			"groups": []any{
				map[string]any{
					"name": "masters",
					"net": map[string]any{
						"ifaces": []any{
							map[string]any{"name": "eno1", "mac": "bb:bb"},
							map[string]any{"name": "eno1", "mtu": float64(9000)},
						},
					},
				},
			},
		}

		err := DeepMergeMaps(dst, src, false, SliceMergeRules{
			"groups":              {Key: "name"},
			"groups[].net.ifaces": {Key: "name"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"duplicate name \"eno1\" at path \"groups[].net.ifaces\" in source"))
	})

	It("should return error when RejectUnmatchedSrc is true and src key is unmatched", func() {
		dst = map[string]any{
			"groups": []any{
				map[string]any{"name": "controller", "role": "master"},
			},
		}
		src = map[string]any{
			"groups": []any{
				map[string]any{"name": "extra", "role": "worker"},
			},
		}

		err := DeepMergeMaps(dst, src, false, SliceMergeRules{
			"groups": {Key: "name", ByKey: ByKeyMergeOptions{RejectUnmatchedSrc: true}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not match any destination entry"))
	})
})

var _ = Describe("deepMergeSlicesByIndex", func() {
	It("should merge maps at the same index", func() {
		dst := []any{
			map[string]any{"name": "a", "role": "master"},
			map[string]any{"name": "b", "role": "worker"},
		}
		src := []any{
			map[string]any{"name": "a", "pool": "pool-1"},
		}
		result, err := deepMergeSlicesByIndex(dst, src, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal([]any{
			map[string]any{"name": "a", "role": "master", "pool": "pool-1"},
			map[string]any{"name": "b", "role": "worker"},
		}))
	})

	It("should replace scalar elements at the same index", func() {
		dst := []any{"old", "keep"}
		src := []any{"new"}
		result, err := deepMergeSlicesByIndex(dst, src, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal([]any{"new", "keep"}))
	})

	It("should work with empty src", func() {
		dst := []any{map[string]any{"name": "a"}}
		result, err := deepMergeSlicesByIndex(dst, []any{}, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal([]any{map[string]any{"name": "a"}}))
	})

	It("should work with empty dst", func() {
		src := []any{map[string]any{"name": "a"}}
		result, err := deepMergeSlicesByIndex([]any{}, src, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal([]any{map[string]any{"name": "a"}}))
	})

	It("should mutate dst slice element maps in place", func() {
		dst := []any{
			map[string]any{"name": "a", "role": "master"},
		}
		src := []any{
			map[string]any{"name": "a", "role": "worker"},
		}
		result, err := deepMergeSlicesByIndex(dst, src, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result[0].(map[string]any)["role"]).To(Equal("worker"))
		Expect(dst[0].(map[string]any)["role"]).To(Equal("worker"))
	})

	It("should return error on type mismatch when checkType is true", func() {
		dst := []any{"value"}
		src := []any{10}
		_, err := deepMergeSlicesByIndex(dst, src, true, nil, "items")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`type mismatch at path "items" index 0`))
	})
})

var _ = Describe("deepMergeSlicesByKey", func() {
	nameRule := SliceMergeRule{Key: "name"}

	It("should merge matched groups by name, preserving dst fields and overriding with src", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master", "hwProfile": "profile-64G", "resourcePoolId": "pool-1"},
			map[string]any{"name": "worker", "role": "worker", "hwProfile": "profile-128G", "resourcePoolId": "pool-1"},
		}
		src := []any{
			map[string]any{"name": "controller", "resourcePoolId": "pool-2"},
			map[string]any{"name": "worker", "resourcePoolId": "pool-2", "resourceSelector": map[string]any{"rack": "rack-3"}},
		}
		result, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(2))

		controller := result[0].(map[string]any)
		Expect(controller["name"]).To(Equal("controller"))
		Expect(controller["role"]).To(Equal("master"))
		Expect(controller["hwProfile"]).To(Equal("profile-64G"))
		Expect(controller["resourcePoolId"]).To(Equal("pool-2"))

		worker := result[1].(map[string]any)
		Expect(worker["name"]).To(Equal("worker"))
		Expect(worker["role"]).To(Equal("worker"))
		Expect(worker["hwProfile"]).To(Equal("profile-128G"))
		Expect(worker["resourcePoolId"]).To(Equal("pool-2"))
		Expect(worker["resourceSelector"]).To(Equal(map[string]any{"rack": "rack-3"}))
	})

	It("should preserve unmatched dst groups", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
			map[string]any{"name": "worker", "role": "worker"},
		}
		src := []any{
			map[string]any{"name": "controller", "resourcePoolId": "pool-1"},
		}
		result, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(2))

		worker := result[1].(map[string]any)
		Expect(worker["name"]).To(Equal("worker"))
		Expect(worker["role"]).To(Equal("worker"))
	})

	It("should append new src groups not in dst", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		src := []any{
			map[string]any{"name": "extra-worker", "role": "worker", "hwProfile": "profile-128G", "resourcePoolId": "pool-2"},
		}
		result, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(2))

		Expect(result[0].(map[string]any)["name"]).To(Equal("controller"))
		Expect(result[1].(map[string]any)["name"]).To(Equal("extra-worker"))
		Expect(result[1].(map[string]any)["role"]).To(Equal("worker"))
	})

	It("should handle mixed matched, unmatched, and new groups", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master", "hwProfile": "profile-64G"},
			map[string]any{"name": "worker", "role": "worker", "hwProfile": "profile-128G"},
		}
		src := []any{
			map[string]any{"name": "worker", "resourcePoolId": "pool-east"},
			map[string]any{"name": "extra-worker", "role": "worker", "hwProfile": "profile-256G"},
		}
		result, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(3))

		Expect(result[0].(map[string]any)["name"]).To(Equal("controller"))
		Expect(result[0].(map[string]any)["hwProfile"]).To(Equal("profile-64G"))

		Expect(result[1].(map[string]any)["name"]).To(Equal("worker"))
		Expect(result[1].(map[string]any)["hwProfile"]).To(Equal("profile-128G"))
		Expect(result[1].(map[string]any)["resourcePoolId"]).To(Equal("pool-east"))

		Expect(result[2].(map[string]any)["name"]).To(Equal("extra-worker"))
	})

	It("should work with empty src", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		result, err := deepMergeSlicesByKey(dst, []any{}, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].(map[string]any)["name"]).To(Equal("controller"))
	})

	It("should work with empty dst", func() {
		src := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		result, err := deepMergeSlicesByKey([]any{}, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].(map[string]any)["name"]).To(Equal("controller"))
	})

	It("should mutate dst slice element maps in place", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master", "hwProfile": "old-profile"},
		}
		src := []any{
			map[string]any{"name": "controller", "hwProfile": "new-profile"},
		}
		result, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).ToNot(HaveOccurred())

		Expect(result[0].(map[string]any)["hwProfile"]).To(Equal("new-profile"))
		Expect(dst[0].(map[string]any)["hwProfile"]).To(Equal("new-profile"))
	})

	It("should return error when dst element is not a map", func() {
		dst := []any{"not-a-map"}
		src := []any{}
		_, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a map"))
	})

	It("should return error when dst element is missing name", func() {
		dst := []any{
			map[string]any{"role": "master"},
		}
		src := []any{}
		_, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing \"name\" field"))
	})

	It("should return error when src element is not a map", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		src := []any{"not-a-map"}
		_, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a map"))
	})

	It("should return error when src element is missing name", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		src := []any{
			map[string]any{"role": "worker"},
		}
		_, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing \"name\" field"))
	})

	It("should return error for duplicate names in dst", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
			map[string]any{"name": "controller", "role": "worker"},
		}
		_, err := deepMergeSlicesByKey(dst, []any{}, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate name \"controller\" at path \"<root>\" in destination"))
	})

	It("should return error for duplicate names in src", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		src := []any{
			map[string]any{"name": "worker", "role": "worker"},
			map[string]any{"name": "worker", "hwProfile": "profile-2"},
		}
		_, err := deepMergeSlicesByKey(dst, src, nameRule, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate name \"worker\" at path \"<root>\" in source"))
	})

	It("should keep last occurrence when AllowDuplicateKeys is set", func() {
		dst := []any{
			map[string]any{"name": "eno1", "label": "old-dst"},
			map[string]any{"name": "eno1", "label": "new-dst"},
		}
		src := []any{
			map[string]any{"name": "eno1", "mtu": float64(1500)},
			map[string]any{"name": "eno1", "mtu": float64(9000)},
		}
		result, err := deepMergeSlicesByKey(dst, src, SliceMergeRule{Key: "name", ByKey: ByKeyMergeOptions{AllowDuplicateKeys: true}}, false, nil, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].(map[string]any)["label"]).To(Equal("new-dst"))
		Expect(result[0].(map[string]any)["mtu"]).To(Equal(float64(9000)))
	})

	It("should reject unmatched src when RejectUnmatchedSrc is set", func() {
		dst := []any{
			map[string]any{"name": "controller", "role": "master"},
		}
		src := []any{
			map[string]any{"name": "extra", "role": "worker"},
		}
		_, err := deepMergeSlicesByKey(dst, src, SliceMergeRule{Key: "name", ByKey: ByKeyMergeOptions{RejectUnmatchedSrc: true}}, false, nil, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not match any destination entry"))
	})
})
