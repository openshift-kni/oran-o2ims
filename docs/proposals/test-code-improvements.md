<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Test Code Improvements

```yaml
title: test-code-improvements
authors:
  - @donpenney
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-05-25
last-updated: 2026-05-25
```

## Summary

Improve test code quality across the project by addressing duplicated
helpers, naming issues, stale code, inconsistent patterns, and
divergences between test and production code paths.

## Findings

### 1. Duplicated Test Helper Functions

`getFakeClientFromObjects` is defined twice with different
implementations:

- `internal/controllers/suite_test.go:192` — registers Inventory CRD,
  ClusterTemplate and ProvisioningRequest status subresources, field
  indexers
- `internal/controllers/utils/utils_test.go:43` — simpler version with
  fewer types registered

These should be consolidated into a single shared helper in
`test/utils/` with all necessary registrations.

### 2. Test Fixture Naming

The test fixture prefix "cnfdg" in `test/utils/vars.go:481` matches
internal lab naming conventions (cnfdg16, cnfdg21). This has caused
confusion during CI failure investigation (OCPBUGS-85549).

Similarly, `docs/user-guide/cluster-api.md` references "cnfdg16" as
an example cluster name, which is a real lab node.

**Fix:** Rename the test fixture prefix to something clearly synthetic
(e.g., "test-node" or "test-worker") and update the documentation
example to use a generic name.

### 3. Misleading Test Comment

`test/e2e/bmh_test.go` (381 lines, 6 test cases) has a misleading
TODO comment: "These tests do not test any actual functionality, leave
them here for now for revisiting." Despite the comment, the tests do
validate meaningful behavior — bootMACAddress workflow scenarios and
BMH state verification for allocation readiness.

**Fix:** Remove the misleading TODO comment.

### 4. Stale Test Names

`test/e2e/mno_hw_configuration_test.go:871` has a test named "Should
PR reach HardwareConfigured=True after callback". The "after callback"
terminology references the old webhook callback architecture that was
removed during the NAR re-architecture. PR #2549 fixed one instance
but this one remains.

**Fix:** Remove "after callback" from the test name.

### 5. Self-Referential Comment

`test/e2e/mno_hw_configuration_test.go:886` has a comment that says
"intentionally different from the production
testNonCachingListAllocatedNodesForNAR" — but
`testNonCachingListAllocatedNodesForNAR` is itself a test function,
not a production function. The comment should reference the production
function `listAllocatedNodesForNAR` in
`internal/controllers/helpers.go`.

### 6. Inconsistent Error Assertion Patterns

Two issues:

**a) `BeNil()` vs `HaveOccurred()`:** 84+ instances of
`Expect(err).To(BeNil())` across test files. The Gomega-idiomatic
pattern is `Expect(err).NotTo(HaveOccurred())` which produces better
failure messages (prints the error message vs just "expected nil").

Affected files: `cluster/api/server_test.go` (12 instances),
`hostfirmwarecomponents_manager_test.go` (28 instances),
`clusterserver_test.go` (15), `resourceserver_test.go` (14),
`provision_test.go` (8), others.

**b) `NotTo` vs `ToNot`:** Both are semantically identical but used
inconsistently within the same files. Pick one and use it throughout.

### 7. Hardcoded Magic Values in E2E Tests

E2E tests define timeout/interval constants but then use different
hardcoded values in many places:

- `mno_hw_configuration_test.go`: defines `timeout = 2m, interval = 3s`
  but uses `3*time.Minute, time.Second*2` and `time.Minute*3,
  time.Second*3` in many Eventually blocks
- `sno_provisioning_test.go`: defines `timeout = 60s, interval = 3s`
  but uses `time.Minute*3, time.Second*3` and `time.Minute*3,
  time.Second*5` extensively

**Fix:** Define suite-level constants for all timeout/interval
combinations and use them consistently.

### 8. Inline Unnamed Struct Parameter

`test/utils/functions.go:331` — `CreateHardwareData()` takes an inline
unnamed struct as a parameter. The `BMHData` type already exists for
`CreateBareMetalHost` and could be reused.

### 9. Client Type Divergence (Informational)

The e2e test framework intentionally uses different client
configurations for different reconcilers:

- ProvisioningRequestReconciler uses `ProvisioningManager.GetClient()`
  (cached, with field indexers) — matches production
- ClusterTemplateTestReconciler uses `K8SClient` (non-caching) —
  diverges from production

This is documented in comments (e2e_suite_test.go:173-177) and is
intentional for the ClusterTemplate reconciler since it doesn't need
field indexers. However, it means the ClusterTemplate reconciler tests
don't exercise the same cache behavior as production.

## Proposed Changes

### Phase 1: Naming and Cleanup (Low Risk)

1. Rename "cnfdg" prefix to "test-node" in `test/utils/vars.go`
2. Update "cnfdg16" in `docs/user-guide/cluster-api.md` to a generic
   example name
3. Fix "after callback" test name
4. Fix self-referential comment in testNonCachingListAllocatedNodesForNAR
5. Remove misleading TODO comment from `test/e2e/bmh_test.go`

### Phase 2: Consistency (Low-Medium Risk)

1. Replace `Expect(err).To(BeNil())` with
   `Expect(err).NotTo(HaveOccurred())` throughout
2. Standardize on `NotTo` (vs `ToNot`) throughout
3. Extract hardcoded timeout/interval values into constants
4. Consolidate `getFakeClientFromObjects` into a single shared helper

### Phase 3: Structural Improvements (Medium Risk)

1. Refactor `CreateHardwareData` to use named struct parameter
2. Improve error context in `CreateResources` helper
3. Evaluate and address ClusterTemplate reconciler client divergence
