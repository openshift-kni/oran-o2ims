<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Resource Collector Improvements

```yaml
title: resource-collector-improvements
authors:
  - @donpenney
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-05-08
last-updated: 2026-05-08
```

## Summary

Improve the resource collector's hardware inventory data collection by
replacing the 60-second polling loop with event-driven K8s watches, aligning
it with the pattern already used by the other four data sources. Also address
data change event table growth and other operational concerns.

## Background

The resource collector maintains a PostgreSQL database of O2IMS inventory
data (resources, resource types, resource pools, locations, deployment
managers) and serves it via the O2IMS REST API. It collects data from
several Kubernetes CRD types on the hub cluster.

### Current Architecture

The collector uses two parallel collection strategies:

**Watch-based (4 data sources)**: Location, OCloudSite, ResourcePool, and
DeploymentManager (ManagedCluster) use K8s Reflectors for event-driven
updates. Changes are detected in <1 second and persisted via async events.

**Polling-based (1 data source)**: HardwareDataSource does a full re-list of
all BareMetalHost, HardwareData, and AllocatedNode CRs every 60 seconds.
No incremental updates or deltas — every cycle re-fetches all resources and
uses a generation ID mark-and-sweep to detect deletions.

```text
                    Watch-based (real-time)          Polling (60s interval)
                    ─────────────────────           ─────────────────────
Data Sources:       Location                        HardwareDataSource
                    OCloudSite                        └─ BMH + HardwareData
                    ResourcePool                       + AllocatedNode CRs
                    K8S (ManagedCluster)

Interface:          WatchableDataSource             ResourceDataSource
                    └─ Watch()                      └─ GetResources()
                    └─ async events → channel       └─ full List every 60s

Update latency:     <1 second                       1-60 seconds
API calls/min:      ~0 (watch stream)               O(n) per cycle
```

### How Hardware Data Reaches the Database

The HardwareDataSource calls `hwmgrcontroller.GetResources()` which:

1. Lists all BareMetalHosts across all namespaces
2. Lists all HardwareData CRs (joined by name to BMH)
3. Lists all AllocatedNode CRs (for allocation state)
4. Builds a `ResourceInfo` struct per BMH with vendor, model, memory,
   processors, admin/operational/usage/power state, resource pool ID,
   hardware profile, labels, NICs, and storage

This data is then converted to `models.Resource` with extensions, resource
types are derived and deduplicated by `{vendor}/{model}`, each resource is
persisted via UPSERT (create-or-update with change detection), stale
resources are purged (generation ID < current), and data change events are
written to the outbox table for subscriber notifications.

### Origin of Current Design

The HardwareDataSource was originally backed by REST API calls to the
hwmgr-plugin server's inventory endpoint. The NAR re-architecture (Phases
1-4) eliminated the REST API, and Phase 4 replaced the REST client with a
direct K8s client call (`hwmgrcontroller.GetResources`). However, the
fundamental polling architecture was preserved — the data source still does
a full re-list every 60 seconds rather than watching for changes.

## Motivation

The polling-based hardware data collection was a consequence of the original
architecture: the resource collector had to use a REST API to retrieve
hardware inventory from the hwmgr-plugin server, and REST APIs are
inherently request/response — they cannot push updates. With the NAR
re-architecture (Phases 1-4) eliminating the REST API layer, the collector
now accesses K8s CRs directly, which opens the opportunity to use watches
instead of polling.

### Polling Overhead at Scale

With 100 BMHs across multiple namespaces, the collector makes 100+ K8s API
calls every minute, re-fetching data that is 99% unchanged. At 1000 BMHs,
this becomes a significant API server load. The watch-based data sources
have no such scaling issue.

### Update Latency

Hardware state changes (BMH online/offline, provisioning state transitions,
allocation changes) take 1-60 seconds to appear in the inventory API. The
watch-based data sources reflect changes in <1 second.

### Generation ID Churn

Every polling cycle increments the generation ID and performs a database
read + comparison for every resource, even when nothing changed. The
`PersistObjectWithChangeEvent` function compares fields and only emits
change events for actual changes, but the per-resource database round-trip
still occurs.

### Data Change Event Table Growth

The `data_change_event` outbox table grows with every resource
create/update/delete and has no TTL or archival mechanism. With active
polling, the table accumulates records proportional to the number of
resources and the uptime of the collector.

## Proposal

### Phase 1: Watch-Based Hardware Collection

Convert HardwareDataSource from `ResourceDataSource` (polling) to
`WatchableDataSource` (event-driven), using the same K8s Reflector pattern
as the other four data sources.

**Watches needed:**

| CRD | Events of interest |
|---|---|
| BareMetalHost | spec/status changes (online, provisioning state) |
| HardwareData | spec changes (hardware details) |
| AllocatedNode | creation/deletion (allocation state changes) |

**Key design consideration:** The current `GetResources` function joins data
across three CRD types (BMH + HardwareData + AllocatedNode) to build a
complete `ResourceInfo`. With watches, a change to any one of these three
CRDs needs to trigger a rebuild of the affected resource.

The three CRDs are linked by naming conventions:

- **BMH ↔ HardwareData**: HardwareData has the same name and namespace as
  the corresponding BMH (1:1 relationship, created by the Metal3 Bare Metal
  Operator).
- **BMH ↔ AllocatedNode**: AllocatedNode references its BMH via
  `spec.hwMgrNodeId` (BMH name) and `spec.hwMgrNodeNs` (BMH namespace).

The join logic for watch events needs to handle:

- BMH created/updated → look up HardwareData by same name/namespace,
  look up AllocatedNode by `spec.hwMgrNodeId`/`spec.hwMgrNodeNs`
- HardwareData updated → find corresponding BMH by same name/namespace
- AllocatedNode created/deleted → find corresponding BMH via
  `spec.hwMgrNodeId`/`spec.hwMgrNodeNs`, update allocation state

**Approach options:**

1. **Three separate watches with cross-reference**: Watch each CRD
   independently. On any event, look up the related objects and rebuild the
   full ResourceInfo. Simple to implement but may cause duplicate database
   writes when multiple related CRDs change simultaneously.

2. **Single composite watch with debouncing**: Watch all three CRDs, queue
   events keyed by BMH name/namespace, and debounce with a short delay
   (e.g., 500ms) to coalesce related changes. More complex but avoids
   redundant writes.

3. **BMH-primary watch with lazy enrichment**: Watch only BMH changes.
   On each BMH event, fetch HardwareData and AllocatedNode data on demand.
   Simplest approach but misses changes to HardwareData or AllocatedNode
   that don't coincide with BMH changes.

**Recommendation:** Option 1 (three separate watches) is the simplest
starting point and consistent with the existing Reflector pattern. The
cross-reference lookups are cheap (single Get by name), and duplicate writes
are handled by the existing change detection in `PersistObjectWithChangeEvent`.

**Impact on polling loop:** Once HardwareDataSource implements
`WatchableDataSource`, the `execute()` polling loop will have no
`ResourceDataSource` instances to process. It can be simplified or
removed, with the `pollingDelay` timer replaced by watch-driven events.

### Phase 2: Data Change Event Cleanup

Add TTL-based cleanup for the `data_change_event` outbox table:

- Delete events older than a configurable retention period (default 7 days)
- Implement as a periodic cleanup in the collector's main loop
- To protect active subscribers, only delete events whose `sequence_id` is
  older than the minimum `event_cursor` across all active subscriptions.
  This ensures no subscriber misses events it has not yet processed. If no
  subscriptions exist, all events older than the retention period can be
  safely deleted.

### Phase 3: Additional Improvements (Optional)

These are lower-priority items that can be addressed independently:

- **Configurable poll interval**: If polling is retained as a fallback,
  expose the interval as an environment variable.

- **API pagination**: The inventory REST API endpoints (e.g.,
  `GET /resourcePools/{poolId}/resources`) currently load all matching
  records from the database and serialize them into a single JSON response.
  With large inventories (1000+ BMHs), this means building a multi-megabyte
  JSON array in memory for every request, which increases response latency
  and memory pressure on the resource server pod. Adding cursor-based
  pagination (e.g., `?limit=100&offset=0` or `?nextpage_opaque_marker=...`)
  would allow clients to retrieve results in manageable pages, reduce
  per-request memory allocation, and enable the database query to use
  `LIMIT`/`OFFSET` rather than returning all rows. The O-RAN O2IMS spec
  already defines pagination fields (`nextpage_opaque_marker`) in the API
  model.

- **Alarm dictionary caching**: When the API serves resource type listings,
  it needs to include alarm dictionary information for each resource type.
  The current implementation fetches all alarm dictionaries in one query,
  then for each resource type, makes a separate database query to fetch
  that type's alarm definitions. With 50 resource types, this means 51
  database queries per API request (1 for the dictionaries + 50 for the
  definitions). Alarm dictionaries and their definitions rarely change
  (only when new resource types are discovered or alarm definitions are
  updated), so they are good candidates for in-memory caching. The
  database already sends a PostgreSQL `NOTIFY` on the
  `resource_type_changed` channel when resource types change, which can
  serve as a cache invalidation signal. Caching would reduce the
  per-request database queries from 1 + N (where N is the number of
  resource types) to a single in-memory lookup for the common case where
  nothing has changed.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Watch disconnection loses events | K8s Reflector handles reconnection and re-list automatically |
| Increased memory from watch caches | Reflector does not cache full objects — it uses a queue |
| Cross-reference lookups add latency | Single Get-by-name is <1ms on the local API server |
| Stale data if watch lags | Generation ID sweep can be retained as a periodic consistency check |

## Alternatives Considered

### Keep Polling, Optimize Interval

Make the poll interval configurable and increase it for large deployments.
This reduces API server load but does not address latency or architectural
inconsistency.

**Rejected because:** It does not solve the fundamental scaling issue and
leaves the hardware data source as an outlier among five data sources.

### Use Informers Instead of Reflectors

Use controller-runtime's `cache.Informer` instead of the custom
`ReflectorStore`. This would give access to the shared cache and field
indexers.

**Deferred because:** The existing Reflector pattern works well for the
other four data sources and avoids coupling the collector to the controller
manager's cache lifecycle. Worth revisiting if the collector is ever
integrated into the controller manager process.
