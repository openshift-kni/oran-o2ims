<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# MNO Cluster Scale In/Out

```yaml
title: mno-scale-in-out
authors:
  - @donpenney
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-07-14
last-updated: 2026-07-14
```

## Table of Contents

- [MNO Cluster Scale In/Out](#mno-cluster-scale-inout)
  - [Table of Contents](#table-of-contents)
  - [Summary](#summary)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
  - [Current State](#current-state)
  - [Proposed Design](#proposed-design)
    - [Scale-Out Flow](#scale-out-flow)
    - [Scale-In Flow](#scale-in-flow)
    - [Worker-Only Validation](#worker-only-validation)
    - [Scale/Upgrade Mutual Exclusion](#scaleupgrade-mutual-exclusion)
    - [Status Tracking](#status-tracking)
    - [AllocatedNodeHostMap Cleanup](#allocatednodehostmap-cleanup)
    - [Hardware Manager Interaction](#hardware-manager-interaction)
    - [Timeout Handling](#timeout-handling)
  - [Edge Cases](#edge-cases)
  - [Phased Implementation](#phased-implementation)
  - [Impact](#impact)
    - [Files to Create or Modify](#files-to-create-or-modify)
  - [Open Questions](#open-questions)
  - [CR Relationships](#cr-relationships)

## Summary

Add the ability to scale worker nodes in and out on already-provisioned
MNO clusters by updating the ProvisioningRequest's
`clusterInstanceParameters.nodes` array. Adding node entries triggers
scale-out (hardware allocation, node provisioning, cluster join).
Removing node entries triggers scale-in (cordon, drain, node removal
from ClusterInstance, hardware deallocation).

No new API fields are required. The operation is fully declarative:
the controller detects added or removed nodes by diffing the old and
new node arrays, then orchestrates the appropriate flow using existing
infrastructure.

## Goals

- Allow users to add worker nodes to a provisioned MNO cluster by
  appending entries to the ProvisioningRequest nodes array.
- Allow users to remove specific worker nodes from a provisioned MNO
  cluster by removing entries from the ProvisioningRequest nodes array.
- Prevent scale operations from running concurrently with upgrades.
- Enforce that only worker nodes can be added or removed; control plane
  nodes are immutable after initial provisioning.
- Reuse existing infrastructure (webhook validation, NAR Size tracking,
  SSA, spoke drain) to minimize new code.

## Non-Goals

- Automatic scaling based on cluster load or resource pressure.
- Scale-in/out of control plane nodes.
- Node replacement (remove one, add another) as a single atomic
  operation — this is two sequential operations.
- Supporting scale operations on SNO clusters.
- Live migration of workloads during scale-in — standard Kubernetes
  drain handles pod eviction.
- Force drain of unreachable nodes — deferred to a follow-up
  enhancement.

## Current State

### What works today

The webhook (`provisioningrequest_webhook.go`) already distinguishes
between immutable field changes and node scaling changes. After
`ClusterProvisioned=Completed`, it allows node scaling — the
`scalingNodes` return value from `FindClusterInstanceImmutableFieldUpdates`
is not appended to the `disallowedFields` rejection list.

When the PR spec changes (generation change), the controller re-runs the
full `executeProvisioningPhases` pipeline:

1. **Pre-provisioning**: Renders the ClusterInstance with the updated
   node list. `extractNodeDetails`/`assignNodeDetails` preserves
   existing node hardware data.
2. **Hardware provisioning**: `buildNodeAllocationRequestSpec` counts
   nodes per role and sets `NodeGroup.Size`. The NAR is patched with
   updated sizes.
3. **Cluster installation**: SSA applies the updated ClusterInstance.

### What's missing

- **Scale-in has no drain step.** Removing nodes from the ClusterInstance
  without first draining them causes workload disruption.
- **No worker-only enforcement.** The webhook allows scaling of any node
  type after `ClusterProvisioned=Completed`. Control plane node removal
  must be rejected.
- **No scale/upgrade mutual exclusion.** Nothing prevents a user from
  scaling while an upgrade is in progress.
- **No AllocatedNodeHostMap cleanup.** When nodes are scaled in, stale
  entries remain in the host map.
- **No scale-specific status messages.** The controller has no way to
  communicate "drain in progress" or "waiting for new nodes" to the
  user.

## Proposed Design

### Scale-Out Flow

When a user adds new worker node entries to the ProvisioningRequest
nodes array:

1. **Webhook allows the change.** The cluster is provisioned, and node
   scaling is permitted. No webhook changes needed.

2. **Controller detects generation change** and re-enters
   `executeProvisioningPhases`.

3. **Pre-provisioning** renders the updated ClusterInstance. New nodes
   get placeholder BMC/MAC values. Existing node hardware data is
   preserved via `extractNodeDetails`/`assignNodeDetails`.

4. **Hardware provisioning**: `buildNodeAllocationRequestSpec` counts
   the increased worker count, producing a larger `NodeGroup.Size`.
   `createOrUpdateNodeAllocationRequest` patches the NAR. The hardware
   manager allocates new BMHs and creates new AllocatedNode CRs.
   `applyNodeConfiguration` maps the new AllocatedNodes to the new
   ClusterInstance node entries.

5. **Cluster installation**: SSA applies the updated ClusterInstance
   with the new nodes. Siteconfig/ZTP provisions the new nodes and
   they join the cluster.

6. **Return to Fulfilled** once all new nodes are ready.

Scale-out requires minimal changes because the existing pipeline
already handles incremental node addition:

- NAR Size increase works via `createOrUpdateNodeAllocationRequest`
- `applyNodeConfiguration` preserves existing assignments and picks
  up new ones
- SSA handles incremental node addition natively
- The controller re-runs the full pipeline on generation change

### Scale-In Flow

When a user removes worker node entries from the ProvisioningRequest
nodes array:

1. **Webhook allows the change** and validates that only worker nodes
   are being removed (see [Worker-Only Validation](#worker-only-validation)).

2. **Controller detects generation change** and re-enters
   `executeProvisioningPhases`.

3. **Scale-in pre-processing** (new). Before the ClusterInstance is
   applied with fewer nodes, the controller must drain the removed
   nodes:

   a. **Detect removed nodes.** Compare the current ClusterInstance's
      node hostnames against the rendered ClusterInstance's node
      hostnames. Nodes present in the current CI but absent from the
      rendered CI are being scaled in.

   b. **Establish spoke client.** Use `spokeclient.EnsureSpokeClient`
      to create an authenticated client to the spoke cluster.

   c. **Cordon and drain** each removed node using `NodeOps.DrainNode`.
      This function cordons the node, then drains with force,
      ignore-daemonsets, and a 30-second timeout per node.

   d. **Update ProvisioningDetails** with drain progress (e.g.,
      "Scale-in: draining node worker1 (1/2)").

4. **Cluster installation**: Once drain completes, SSA applies the
   reduced ClusterInstance. Siteconfig removes the BMHs for
   deprovisioned nodes.

5. **Hardware deallocation**: The controller identifies the
   AllocatedNode CRs corresponding to the removed nodes (via
   `allocatedNodeHostMap`) and deletes them. The AllocatedNode
   controller's existing deallocation logic releases the BMHs.
   The NAR is updated with the reduced `NodeGroup.Size`.

6. **AllocatedNodeHostMap cleanup**: Stale entries for removed nodes
   are deleted from the host map.

7. **Return to Fulfilled** once the reduced ClusterInstance is stable.

#### Where to insert the drain step

The drain must happen after the ClusterInstance is rendered (to know
which nodes are removed) but before `applyClusterInstance` (so nodes
are drained before removal).

Insert a new `handleScaleInDrain` method in
`executeClusterInstallationPhase`, before `handleClusterInstallation`:

```text
executeClusterInstallationPhase
  ├── [NEW] handleScaleInDrain(ctx, renderedCI)
  │     ├── Detect removed nodes (current CI vs rendered CI)
  │     ├── If no removed nodes, return immediately
  │     ├── Establish spoke client
  │     ├── Cordon + drain each removed node
  │     ├── Update ProvisioningDetails with progress
  │     └── Requeue if drain incomplete
  └── handleClusterInstallation(ctx, renderedCI)  [existing]
```

This keeps the rendering phase pure and concentrates spoke interaction
in the cluster installation phase, consistent with how upgrades
interact with the spoke cluster.

### Worker-Only Validation

Two enforcement points:

1. **Webhook** (admission-time): In `validateCreateOrUpdate`, when the
   cluster is provisioned and scaling nodes are detected, check whether
   any removed node has `role: master`. Reject with a clear error
   message if so. This is the primary gate.

2. **Controller** (reconcile-time, defensive): In `handleScaleInDrain`,
   verify that only worker nodes are being removed. If a control-plane
   node reaches this point, fail with an error. This is a safety net
   only.

### Scale/Upgrade Mutual Exclusion

Scale operations and upgrade operations must not run concurrently:

- **Prevent scale during upgrade**: In the webhook, if
  `UpgradeCompleted` condition exists with reason `InProgress`, reject
  node scaling changes. This gives immediate feedback to the user.

- **Prevent upgrade during scale**: In the controller's
  `handleClusterUpgrades`, check if the current and desired node counts
  differ (indicating a scale operation is in progress). If so, skip
  upgrade handling and requeue.

### Status Tracking

Reuse existing condition types with scale-specific messages rather than
introducing new condition types. The `ProvisioningDetails` field
provides fine-grained progress:

**Scale-out messages:**

- ProvisioningDetails: "Scale-out: waiting for hardware allocation
  (2/3 nodes provisioned)"
- ProvisioningDetails: "Scale-out: waiting for new nodes to join the
  cluster (1/2 ready)"

**Scale-in messages:**

- ProvisioningDetails: "Scale-in: draining node worker1 (1/2)"
- ProvisioningDetails: "Scale-in: drain complete, applying updated
  ClusterInstance"

**Provisioning status transitions:**

`Fulfilled` → `Pending` → `Progressing` → `Fulfilled`

This is the same transition pattern used for any PR spec change and
does not require new phases.

### AllocatedNodeHostMap Cleanup

When nodes are scaled in, their entries must be removed from
`status.extensions.allocatedNodeHostMap`. The cleanup happens after
the NAR is updated and the hardware manager has deallocated the excess
AllocatedNodes.

The controller determines which entries to remove by:

1. Listing current AllocatedNodes for the NAR.
2. Removing any entries from `allocatedNodeHostMap` whose key
   (AllocatedNode ID) does not appear in the current AllocatedNode
   list.

This cleanup fits naturally in the existing
`updateInfrastructureResourceStatuses` flow.

### Hardware Manager Interaction

The metal3 hardware manager does **not** currently handle
`NodeGroup.Size` decreases. When the allocation loop in
`helpers.go` finds `pending <= 0` (allocated count >= Size), it
simply skips the group — there is no logic to detect and remove
excess AllocatedNodes.

However, the hardware manager **does** handle individual AllocatedNode
deletion: the `allocatednode_controller.go` watches for AllocatedNode
CR deletions and calls `deallocateBMH` to release the underlying BMH
back to the available pool.

Therefore, the O-Cloud Manager should directly delete the specific
AllocatedNode CRs for the removed nodes as part of the scale-in flow.
This triggers the existing AllocatedNode controller deallocation path
without requiring hardware manager changes. The flow becomes:

1. O-Cloud Manager identifies which AllocatedNodes correspond to the
   removed ClusterInstance nodes (via `allocatedNodeHostMap`).
2. O-Cloud Manager deletes those AllocatedNode CRs.
3. The AllocatedNode controller detects the deletion and calls
   `deallocateBMH` to release the BMH.
4. O-Cloud Manager updates the NAR with the reduced `NodeGroup.Size`
   (so the allocation count stays consistent).

This approach avoids any hardware manager code changes.

### Timeout Handling

- **Scale-out**: Inherits the existing hardware provisioning and cluster
  provisioning timeouts. No changes needed.

- **Scale-in drain**: `DrainNode` uses a 30-second timeout per node. If
  a node is unreachable, drain fails and the controller requeues and
  retries. If the node remains unreachable, the operation stays in a
  failed state until the user resolves the issue manually.

- **Configurable drain timeout**: Deferred to a follow-up enhancement.
  The 30-second default is sufficient for the initial implementation.

## Edge Cases

### Concurrent scale requests

If the user submits a second scale request while the first is in
progress, the controller sees a new generation change and re-evaluates
the desired state. For scale-out, the controller renders the
ClusterInstance with all desired nodes. For scale-in, it re-diffs and
drains any nodes not in the new desired state.

### Drain failure (node unreachable)

If a node cannot be drained, the operation fails with a clear error
message indicating which node could not be drained and why. The user
must manually resolve the issue (fix the node, or delete the Node
object on the spoke cluster) and retry by updating the PR spec.

Force drain options are deferred to a follow-up enhancement.

### Partial completion

If the controller drains some nodes but fails on others, successfully
drained nodes remain cordoned until the entire operation completes. If
the user wants to abort, they can add the removed nodes back to the PR
spec, which triggers a new generation change. The controller re-renders
the ClusterInstance with all nodes and applies it via SSA. The
previously drained nodes would need to be uncordoned manually or by a
subsequent reconciliation.

### Mixed scale-in and scale-out

If the user simultaneously adds some nodes and removes others in a
single PR update, both operations are handled in the same
reconciliation cycle. The drain step handles removed nodes, and the
hardware provisioning step handles added nodes.

### Scale to zero workers

Allowed. Drain handles workload eviction. If pods cannot be
rescheduled, that is the user's responsibility.

## Phased Implementation

### Phase 1: Scale-out support

- Verify and test that the existing pipeline correctly handles node
  additions on an already-provisioned cluster.
- Add worker-only webhook validation for node additions.
- Add unit and e2e tests for scale-out.
- Fix any issues found in the existing flow.

### Phase 2: Scale-in support

- Implement `handleScaleInDrain` with spoke client setup and drain.
- Add AllocatedNodeHostMap cleanup.
- Add scale/upgrade mutual exclusion (webhook + controller).
- Implement AllocatedNode CR deletion for removed nodes (triggers
  existing `deallocateBMH` in the AllocatedNode controller).
- Add unit and e2e tests for scale-in.

### Phase 3: Edge cases and hardening

- Handle drain failures (force options, configurable timeout).
- Handle partial completion abort (uncordon drained nodes).
- Test mixed scale-in/out operations.
- Test concurrent scale and upgrade rejection.

## Impact

No breaking changes. Scale operations use the existing
ProvisioningRequest API — users add or remove node entries from the
existing `clusterInstanceParameters.nodes` array.

### Files to Create or Modify

| File | Phase | Action |
|------|-------|--------|
| `api/provisioning/v1alpha1/provisioningrequest_webhook.go` | 1 | **Modify** — worker-only validation for node scaling, scale/upgrade mutual exclusion |
| `api/provisioning/v1alpha1/provisioningrequest_webhook_test.go` | 1 | **Modify** — tests for worker-only and mutual exclusion |
| `internal/controllers/provisioningrequest_clusterinstall.go` | 2 | **Modify** — add `handleScaleInDrain`, node removal detection |
| `internal/controllers/provisioningrequest_controller.go` | 2 | **Modify** — scale/upgrade check in `handlePostProvisioning` |
| `internal/controllers/provisioningrequest_hwprovision.go` | 2 | **Modify** — AllocatedNodeHostMap cleanup |
| `internal/controllers/provisioningrequest_upgrade.go` | 2 | **Modify** — scale-in-progress check |
| `test/e2e/mno_scale_test.go` | 1-3 | **Create** — e2e tests for scale-out and scale-in |
| `internal/controllers/provisioningrequest_clusterinstall_test.go` | 2 | **Modify** — tests for drain and node removal |

## Open Questions

1. **Drain timeout configurability**: Should the 30-second per-node
   drain timeout be configurable via the ProvisioningRequest or
   ClusterTemplate?

2. **Spoke client approach**: Should scale-in drain use
   `spokeclient.EnsureSpokeClient` (scoped ManagedServiceAccount,
   more secure) or the full admin kubeconfig from the spoke cluster's
   Secret (simpler, already available)?

## CR Relationships

```text
ProvisioningRequest (user updates nodes array)
  │
  ├── [Scale-Out: nodes added]
  │     ├── NodeAllocationRequest (patched: NodeGroup.Size increased)
  │     │     └── Hardware manager creates new AllocatedNode CRs
  │     ├── ClusterInstance (SSA: new nodes added with BMC/MAC from AllocatedNodes)
  │     │     └── Siteconfig provisions new nodes → join cluster
  │     └── ProvisioningRequest status
  │           ├── allocatedNodeHostMap: new entries added
  │           └── provisioningStatus: fulfilled → pending → progressing → fulfilled
  │
  └── [Scale-In: nodes removed]
        ├── Spoke Cluster (via spokeclient)
        │     ├── ManagedServiceAccount (drain RBAC)
        │     └── Nodes: cordon + drain on each removed node
        ├── ClusterInstance (SSA: removed nodes deleted, after drain)
        │     └── Siteconfig removes BMHs for deprovisioned nodes
        ├── NodeAllocationRequest (patched: NodeGroup.Size decreased)
        │     └── Hardware manager deallocates excess AllocatedNodes
        └── ProvisioningRequest status
              ├── allocatedNodeHostMap: stale entries removed
              └── provisioningStatus: fulfilled → pending → progressing → fulfilled
```
