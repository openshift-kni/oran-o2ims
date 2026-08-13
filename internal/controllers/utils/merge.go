/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"fmt"
	"reflect"
)

// SliceMergeRule controls how a slice is merged when DeepMergeMaps encounters it.
// Key == "" means merge by index. A non-empty Key merges map elements by that field
// (e.g. "name") and applies ByKey options.
//
// ByKey is ignored when Key is empty. Index-merge-specific options can be added later
// as a sibling field without overloading ByKey.
type SliceMergeRule struct {
	Key   string
	ByKey ByKeyMergeOptions
}

// ByKeyMergeOptions applies only when SliceMergeRule.Key is non-empty.
// Zero-value bools are the defaults.
//
// AllowDuplicateKeys:
//   - false (default): error if the same key appears more than once in dst or src
//   - true: keep the last occurrence (dst: replace; src: merge each occurrence in
//     order so later fields override earlier ones)
//
// RejectUnmatchedSrc:
//   - false (default): append src entries whose key has no match in dst
//   - true: error if a src key does not match any dst entry
//
// Unmatched dst entries are always preserved.
type ByKeyMergeOptions struct {
	AllowDuplicateKeys bool
	RejectUnmatchedSrc bool
}

// SliceMergeRules maps a merge path to the SliceMergeRule for the slice at that path.
// Nil or empty means all slices merge by index.
// Path uses "." to represent map fields and "[]" to represent nesting inside a list element.
// Example:
//
//	SliceMergeRules{
//	    "nodeGroups": {Key: "name"}, // top-level slice field merged by name
//	    "nodeGroups[].nodeNetwork.interfaces": {Key: "name"}, // slice nested under a list element map merged by name
//	}
type SliceMergeRules map[string]SliceMergeRule

// DeepMergeMaps performs a deep merge of the src map into the dst map.
// dst is mutated in place, including nested maps and slice elements.
// Callers that need to preserve the original must copy before calling.
// Merge rules:
//  1. If a key exists in both src and dst maps:
//     a. If the values are of different types and matched type is required,
//     it returns an error, otherwise, the src value overrides the dst element.
//     b. If the values are both maps, recursively merge them.
//     c. If the values are both slices, deeply merge the slices (by index, or by
//     key when rules lists a by-key rule for that path).
//     d. For other types, the src value overrides the dst value.
//  2. If a key exists only in src, add it to dst.
//  3. If a key exists only in dst, preserve it.
//
// rules may be nil; see SliceMergeRules for path key syntax and examples.
func DeepMergeMaps(dst, src map[string]any, checkType bool, rules SliceMergeRules) error {
	return deepMergeMaps(dst, src, checkType, rules, "")
}

func deepMergeMaps(dst, src map[string]any, checkType bool, rules SliceMergeRules, path string) error {
	for key, srcValue := range src {
		childPath := joinMergePath(path, key)
		if dstValue, exists := dst[key]; exists {
			if reflect.TypeOf(dstValue) != reflect.TypeOf(srcValue) {
				// If types do not match, return an error if checkType is true
				if checkType {
					return fmt.Errorf("type mismatch for key: %v (dst: %T, src: %T)", key, dstValue, srcValue)
				}
				// Otherwise, override dst with src
				dst[key] = srcValue
			} else {
				// Types match, handle according to type
				switch dstValueTyped := dstValue.(type) {
				case map[string]any:
					// If both values are maps, recursively merge them
					srcValueTyped := srcValue.(map[string]any)
					if err := deepMergeMaps(dstValueTyped, srcValueTyped, checkType, rules, childPath); err != nil {
						return fmt.Errorf("error merging maps for key: %v: %w", key, err)
					}
				case []any:
					// If both values are slices, deeply merge them (by key or by index)
					var mergedSlice []any
					var err error

					srcValueTyped := srcValue.([]any)
					if rule := rules[childPath]; rule.Key != "" {
						mergedSlice, err = deepMergeSlicesByKey(dstValueTyped, srcValueTyped, rule, checkType, rules, childPath)
					} else {
						mergedSlice, err = deepMergeSlicesByIndex(dstValueTyped, srcValueTyped, checkType, rules, childPath)
					}
					if err != nil {
						return fmt.Errorf("error merging slices for key: %v: %w", key, err)
					}
					dst[key] = mergedSlice
				default:
					// For other types, override dst with src
					dst[key] = srcValue
				}
			}
		} else {
			// If the key exists only in src, add it to dst
			dst[key] = srcValue
		}
	}
	return nil
}

// deepMergeSlicesByIndex performs a deep indexing merge of the src slice into the dst slice.
// Element maps are mutated in place (same as deepMergeMaps). The returned slice may
// differ from the input header when elements are appended.
// Merge rules:
//  1. For elements present in both src and dst slices at the same index:
//     a. If the elements are of different types and matched type is required,
//     it returns an error, otherwise, the src element overrides the dst element.
//     b. If the elements are both maps, deeply merge them in place.
//     c. For other types, the src element overrides the dst element.
//  2. If the src slice is longer, append the additional elements from src to dst.
//  3. If the dst slice is longer, preserve the additional elements from dst.
func deepMergeSlicesByIndex(
	dst, src []any, checkType bool, rules SliceMergeRules, listPath string,
) ([]any, error) {
	maxLen := len(dst)
	if len(src) > maxLen {
		maxLen = len(src)
	}

	result := make([]any, 0, maxLen)
	// Path of each element for nested rule lookups (e.g. "nodeGroups" → "nodeGroups[]")
	elemPath := sliceElemMergePath(listPath)
	pathLabel := mergePathLabel(listPath)

	for i := 0; i < maxLen; i++ {
		if i < len(dst) && i < len(src) { // nolint: gocritic
			dstElem := dst[i]
			srcElem := src[i]
			if reflect.TypeOf(dstElem) != reflect.TypeOf(srcElem) {
				// If types do not match, return an error if checkType is true
				if checkType {
					return nil, fmt.Errorf("type mismatch at path %q index %d (dst: %T, src: %T)", pathLabel, i, dstElem, srcElem)
				}
				// Otherwise, use the src element
				result = append(result, srcElem)
			} else {
				// Types match, handle according to type
				if dstMap, ok := dstElem.(map[string]any); ok {
					// If both elements are maps, deeply merge them
					srcElemTyped := srcElem.(map[string]any)
					if err := deepMergeMaps(dstMap, srcElemTyped, checkType, rules, elemPath); err != nil {
						return nil, fmt.Errorf("error merging maps at path %q slice index %d: %w", pathLabel, i, err)
					}
					result = append(result, dstMap)
					continue
				}
				// For other types, use the src element
				result = append(result, srcElem)
			}
		} else if i < len(dst) {
			// Only dst has the element
			result = append(result, dst[i])
		} else {
			// Only src has the element
			result = append(result, src[i])
		}
	}
	return result, nil
}

// deepMergeSlicesByKey merges src into dst by matching map elements on rule.Key.
// Element maps are mutated in place (same as deepMergeMaps). The returned slice may
// differ from the input header when elements are appended.
// Merge rules:
//  1. Elements are matched by the string value of rule.Key (e.g. "name").
//  2. For matched pairs, src fields are deep-merged over dst fields (src wins).
//  3. Unmatched dst elements are preserved.
//  4. Unmatched src elements are appended, unless ByKey.RejectUnmatchedSrc is set.
//  5. Duplicate keys within dst or src error unless ByKey.AllowDuplicateKeys is set.
func deepMergeSlicesByKey(
	dst, src []any, rule SliceMergeRule, checkType bool, rules SliceMergeRules, listPath string,
) ([]any, error) {
	elemPath := sliceElemMergePath(listPath)
	pathLabel := mergePathLabel(listPath)

	// Build index of dst elements by key, starting result as dst's element maps.
	// Duplicate keys in dst are handled here (error or last-wins replace).
	dstByKey := make(map[string]int)
	result := make([]any, 0, len(dst))
	for i, item := range dst {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("destination element at path %q index %d is not a map", pathLabel, i)
		}
		keyValue, ok := itemMap[rule.Key].(string)
		if !ok || keyValue == "" {
			return nil, fmt.Errorf("destination element at path %q index %d missing %q field", pathLabel, i, rule.Key)
		}
		if oldIdx, exists := dstByKey[keyValue]; exists {
			if !rule.ByKey.AllowDuplicateKeys {
				return nil, fmt.Errorf("duplicate %s %q at path %q in destination", rule.Key, keyValue, pathLabel)
			}
			result[oldIdx] = itemMap
			continue
		}
		result = append(result, itemMap)
		dstByKey[keyValue] = len(result) - 1
	}

	// Track which src elements have been seen. Separate from the dst check above:
	// with AllowDuplicateKeys, each src occurrence is still merged in order into the same
	// dst entry so later fields override earlier ones.
	srcSeen := make(map[string]bool)

	for i, srcItem := range src {
		srcMap, ok := srcItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("source element at path %q index %d is not a map", pathLabel, i)
		}
		keyValue, ok := srcMap[rule.Key].(string)
		if !ok || keyValue == "" {
			return nil, fmt.Errorf("source element at path %q index %d missing %q field", pathLabel, i, rule.Key)
		}
		if srcSeen[keyValue] {
			if !rule.ByKey.AllowDuplicateKeys {
				return nil, fmt.Errorf("duplicate %s %q at path %q in source", rule.Key, keyValue, pathLabel)
			}
		}
		srcSeen[keyValue] = true

		if dstIdx, exists := dstByKey[keyValue]; exists {
			// Merge src over dst for this key
			dstMap := result[dstIdx].(map[string]any)
			if err := deepMergeMaps(dstMap, srcMap, checkType, rules, elemPath); err != nil {
				return nil, fmt.Errorf("failed to merge %s %q at path %q: %w", rule.Key, keyValue, pathLabel, err)
			}
		} else {
			// New element from src — reject or append
			if rule.ByKey.RejectUnmatchedSrc {
				return nil, fmt.Errorf("%s %q at path %q in source does not match any destination entry", rule.Key, keyValue, pathLabel)
			}
			result = append(result, srcMap)
			dstByKey[keyValue] = len(result) - 1
		}
	}

	return result, nil
}

// mergePathLabel returns listPath for error messages, or "<root>" when the slice
// has no map path context.
func mergePathLabel(listPath string) string {
	if listPath == "" {
		return "<root>"
	}
	return listPath
}

// joinMergePath builds a dotted path for nested map keys (e.g. ""+"a" → "a", "a"+"b" → "a.b").
func joinMergePath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// sliceElemMergePath appends "[]" so nested rules can target fields inside list elements
// (e.g. "nodeGroups" → "nodeGroups[]", then "nodeGroups[].nodeNetwork.interfaces").
func sliceElemMergePath(listPath string) string {
	if listPath == "" {
		return "[]"
	}
	return listPath + "[]"
}
