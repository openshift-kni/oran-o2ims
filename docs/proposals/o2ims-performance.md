# O2ims_InfrastructurePerformance over ACM Observability

```yaml
title: o2ims-performance
version: "0.1"
authors:
  - @browsell
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-11-05
last-updated: 2026-11-05
change-history: "#appendix-c-document-version-history"
```

**Epic**: TBD
**Specs**:

- O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v13.00, clause 2.1.7 (service definitions)
  and clause 3.7 (`O2ims_InfrastructurePerformance` Service API, version 2.3.0)
- O-RAN.WG6.O-CLOUD-IM.0-R004 (O-Cloud Information Model, ref [36]), clause
  4.2.1.4.14–4.2.1.4.27 (Performance Management information elements)
- O-RAN.WG10.TS.Information Model and Data Models.0-R004 (WG10 IM&DM), clause
  5.2 (`PerformanceMeasureDictionary` / `PerformanceMeasureDefinition`, the
  metric definition; built on 3GPP TS 32.404 and ETSI NFV-IFA027)

---

## Table of Contents

- [1. Executive Summary](#1-executive-summary)
- [2. Background](#2-background)
  - [2.1 What the spec requires](#21-what-the-spec-requires)
  - [2.2 What ACM Observability provides](#22-what-acm-observability-provides)
  - [2.3 Hard constraint: do not change ACM Observability](#23-hard-constraint-do-not-change-acm-observability)
- [3. Key Design Decision: Virtual PM Jobs over Thanos](#3-key-design-decision-virtual-pm-jobs-over-thanos)
- [4. Architecture](#4-architecture)
  - [4.1 Component overview](#41-component-overview)
  - [4.2 Read path (measurement retrieval)](#42-read-path-measurement-retrieval)
  - [4.3 Write path (subscriptions and reporting)](#43-write-path-subscriptions-and-reporting)
- [5. Data Model Mapping](#5-data-model-mapping)
  - [5.1 MeasurementJob](#51-measurementjob)
  - [5.2 The PM Dictionary (metric catalog)](#52-the-pm-dictionary-metric-catalog)
  - [5.3 Resource scope resolution and layered attribution](#53-resource-scope-resolution-and-layered-attribution)
    - [5.3.1 Single source via ACM Thanos](#531-single-source-via-acm-thanos)
  - [5.4 PerformanceServiceConfiguration](#54-performanceserviceconfiguration)
  - [5.5 Subscriptions and PerformanceSubscriptionCriteria](#55-subscriptions-and-performancesubscriptioncriteria)
  - [5.6 State and status model](#56-state-and-status-model)
  - [5.7 Measurement data and report payload (from O-Cloud IM)](#57-measurement-data-and-report-payload-from-o-cloud-im-ref-36)
  - [5.8 Schema reconciliation and the Measurement Mapping Catalog](#58-schema-reconciliation-and-the-measurement-mapping-catalog)
- [6. Semantic Gaps and How We Resolve Them](#6-semantic-gaps-and-how-we-resolve-them)
- [7. Phased Implementation](#7-phased-implementation)
- [8. Verification Plan](#8-verification-plan)
- [9. Spec Coverage and Omissions](#9-spec-coverage-and-omissions)
  - [9.1 Coverage summary](#91-coverage-summary)
  - [9.2 Material items — design](#92-material-items--design)
  - [9.3 Deferred by explicit choice](#93-deferred-by-explicit-choice)
  - [9.4 Thin / partially covered](#94-thin--partially-covered)
  - [9.5 Realized-coverage limits (from the spike)](#95-realized-coverage-limits-from-the-spike)
  - [9.6 Tier-0 preinstalled PM Jobs (golden set)](#96-tier-0-preinstalled-pm-jobs-golden-set)
- [Appendix A: REST Resource / Method Coverage](#appendix-a-rest-resource--method-coverage)
- [Appendix B: Files to Create and Modify](#appendix-b-files-to-create-and-modify)
- [Appendix C: Document Version History](#appendix-c-document-version-history)

---

## 1. Executive Summary

This proposal defines how the O-Cloud Manager will implement the
`O2ims_InfrastructurePerformance` Service API (O2IMS v13, clause 3.7, API
version 2.3.0) **using ACM Observability as the sole metrics source, without
changing any ACM Observability fundamentals** (collection, storage, retention,
allowlist, or Thanos topology).

**Core idea.** ACM Observability already runs a complete, continuously-running
performance measurement pipeline: managed-cluster collectors scrape Prometheus
and remote-write to a hub-side Thanos stack with long-term object storage,
queryable through a PromQL-compatible endpoint. The O2IMS Performance service
does **not** build a second collection pipeline. Instead it exposes a new
`performance-server` microservice that:

1. Treats every `MeasurementJob` as a **virtual query definition** — persisted
   metadata (scope + measurement selection + interval) that is *evaluated on
   demand* by translating it into PromQL range queries against ACM's Thanos
   Query endpoint. No metrics are copied or re-collected.
2. Publishes a set of **preinstalled PM Jobs** derived from ACM's metrics
   allowlist, so an SMO sees a working inventory of measurements out of the box.
3. Implements **Performance Management Subscriptions** by periodically
   evaluating a subscription's criteria against Thanos and delivering
   `PerformanceMeasurementEventNotification` reports to the subscriber
   callback, reusing the alarms service's existing notifier/subscription
   infrastructure. NOTIFICATION delivery is the first-class mechanism; FILE and
   STREAM are deferred.

This keeps ACM Observability authoritative and untouched while making the
O-Cloud the O2IMS Performance **producer** the SMO talks to.

| Aspect | Decision |
|--------|----------|
| PM Job model | Virtual query definition over Thanos (no re-collection) |
| Metrics source | ACM Observability Thanos Query (PromQL), in-cluster |
| First delivery | NOTIFICATION callback (FILE/STREAM later) |
| Target scope | Full API incl. subscriptions, delivered in phases |
| New service | `performance-server`, modeled on `alarms` |
| API base URL | `/O2ims_infrastructurePerformance/v2` |

---

## 2. Background

### 2.1 What the spec requires

Clause 3.7 defines a REST API (major version 2) with these resources
(Figure 3.7.3-1):

```text
{apiRoot}/O2ims_infrastructurePerformance/{apiMajorVersion}
  /measurementJobs
    /{measurementJobId}
      /suspend          (POST task)
      /resume           (POST task)
  /performanceServiceConfiguration
  /performanceSubscriptions
    /{performanceSubscriptionId}
```

Operations: query/create/delete/update/suspend/resume Performance Measurement
Jobs; get/set/patch the Performance Service Configuration; and
create/query/delete/update Performance Management Subscriptions. The
`O2ims_InfrastructurePerformance` Service API itself specifies **no
notifications** (clause 3.7.5) — reporting to the SMO is driven entirely
through subscriptions. The full REST/method matrix is in
[Appendix A](#appendix-a-rest-resource--method-coverage).

The core structured types (clause 3.7.6) are `MeasurementJob`,
`MeasurementJobRequest`, `MeasurementJobUpdate`, `MeasurementJobSuspendRequest`,
`PerformanceServiceConfiguration`, `PerformanceSubscriptionData`,
`PerformanceSubscriptionInfo`, `PerformanceSubscriptionCriteria`, and
`PerformanceReportingFrequency`. States are `ACTIVE | SUSPENDED | DEPRECATED`;
statuses are `RUNNING | FAILED | DEGRADED | IDLE | PENDING_DELETE`.

### 2.2 What ACM Observability provides

ACM Observability (the `multicluster-observability-operator`, MCO) deploys into
the `open-cluster-management-observability` namespace and provides exactly the
"Performance Measurement Store" + "preinstalled collection" the spec assumes:

- **Collection** — the observability-endpoint-operator on each managed cluster
  runs a metrics-collector that scrapes OCP Prometheus and remote-writes to the
  hub. Which series are forwarded is governed by the
  `observability-metrics-allowlist` ConfigMap (defaults) and the
  `observability-metrics-custom-allowlist` ConfigMap (operator additions).
- **Storage** — a hub-side Thanos stack (receive / store / compactor / ruler)
  with long-term object storage and its own retention/downsampling policy.
- **Query** — a PromQL-compatible HTTP API. Two access points exist:
  - In-cluster service `observability-thanos-query.open-cluster-management-observability.svc`
    (Thanos Querier, PromQL `/api/v1/query`, `/api/v1/query_range`,
    `/api/v1/series`, `/api/v1/labels`). **This is the endpoint we use.**
  - External RBAC-enforcing route/service `rbac-query-proxy`, which fronts the
    querier and injects per-tenant cluster filtering. **Not used** — see
    [query-access decision](#query-access-in-cluster-thanos-querier) below.

Series carry labels that identify scope — notably `cluster` /
`clusterID` (managed cluster), `node`, `namespace`, `instance`, `pod`, etc. —
which is what lets us resolve an O2IMS resource scope to a Thanos selector.

> Precedent in this repo: the **alarms** service already integrates
> non-invasively with ACM Observability by merging an O-RAN receiver into the
> existing `alertmanager-config` secret in
> `open-cluster-management-observability`
> (`internal/service/alarms/internal/alertmanager/config.go`, using
> `ctlrutils.OpenClusterManagementObservabilityNamespace`). The Performance
> service follows the same "read/augment, never restructure" philosophy, but
> against Thanos Query instead of Alertmanager.

### 2.3 Hard constraint: do not change ACM Observability

The design must not alter ACM Observability fundamentals. Concretely, the
Performance service **must not**:

- change the metrics-collector scrape/collection interval,
- add to or require edits of the metrics allowlist for correctness,
- change Thanos retention, downsampling, or storage topology,
- deploy a parallel Prometheus/Thanos, or remote-write/duplicate ACM's TSDB.

Everything the Performance service needs is obtained **read-only** from Thanos
Query plus read-only reads of ACM ConfigMaps (allowlist) and, optionally, the
`MultiClusterObservability` CR for availability/health. The consequences of
this constraint are called out explicitly in
[Section 6](#6-semantic-gaps-and-how-we-resolve-them).

---

## 3. Key Design Decision: Virtual PM Jobs over Thanos

A spec `MeasurementJob` is a long-lived entity that "collects measurements at a
collection interval and stores them in the Performance Measurement Store." ACM
already does the collecting and storing. Rebuilding that inside the O-Cloud
Manager would mean copying/downsampling ACM's metrics into our own Postgres —
duplicating the TSDB, re-implementing a collection pipeline, and owning
retention we promised not to change.

**Decision: a `MeasurementJob` is a *virtual* query definition.** We persist
only the job's *definition and lifecycle metadata* in the `performance`
Postgres database; the measurements themselves are never stored by us. When a
job's measurements are needed (a report tick, or a future direct read), we
translate the job definition into PromQL and evaluate it against ACM Thanos
Query at that moment.

This reinterprets two spec fields, which we document as behavioral contracts:

- **`collectionInterval`** — with virtual jobs there is no collection we
  control; ACM's collector interval is fixed. We treat `collectionInterval` as
  the **evaluation/report resolution** (the PromQL `step` and the reporting
  cadence floor). It is validated against a **floor equal to ACM's effective
  collection interval**; requests below the floor are rejected (400) with a
  `ProblemDetails` explaining the minimum, mirroring the spec's
  "reportInterval lower than the service can provide → error" rule
  (clause 3.7.6.2.8).
- **`measuredResources` / `collectedMeasurements` / `qualifiedResourceTypes`**
  — these are *derived* read-only outputs (the spec already says so for
  `qualifiedResourceTypes` and `measuredResources`). `measuredResources` is
  computed by resolving `resourceScopeCriteria` against the inventory at query
  time; `collectedMeasurements` is projected from the Thanos result set rather
  than read from a local store.

Trade-off accepted: we rely on ACM/Thanos availability for every read, and we
cannot serve measurements for windows ACM did not retain. Both are acceptable
because ACM *is* the Performance Measurement Store by design. See
[Section 6](#6-semantic-gaps-and-how-we-resolve-them).

---

## 4. Architecture

### 4.1 Component overview

A new `performance-server` microservice (one more subcommand of the existing
multi-service binary), deployed on the hub by the inventory controller exactly
like `alarms-server`:

```text
                SMO / API consumer
                        │  O2 (HTTPS, OAuth)
                        ▼
        ┌───────────────────────────────────────┐
        │          performance-server            │
        │  (generated OpenAPI strict handlers)   │
        │                                        │
        │  ├─ Job registry  ── Postgres          │   job defs, state,
        │  │   (performance DB)                   │   subscriptions, outbox
        │  ├─ Measurement Mapping Catalog ────────┼──► ACM allowlist + Thanos
        │  │   (dictionary generator + policy)    │      metadata (read-only, §5.8)
        │  ├─ Thanos query client ───────────────┼──► observability-thanos-query
        │  │   (PromQL translate + evaluate)      │      .open-cluster-management-
        │  ├─ Resource-scope resolver ───────────┼──► resource-server (inventory)
        │  └─ Reporter (notifier) ───────────────┼──► subscriber callbacks
        └───────────────────────────────────────┘
                        ▲
                        │ read-only
          ACM Observability (unchanged):
          allowlist ConfigMaps, Thanos stack, object storage
```

Reused wholesale from `internal/service/common` and the alarms template: cobra
cmd wiring, OpenAPI codegen, the Postgres pool/migration framework, the
service-config retention-cleanup pattern, the generic notifier engine, the PG
LISTEN/NOTIFY listener, OAuth auth middleware, and the metrics endpoint. The
net-new pieces are the **Measurement Mapping Catalog** (dictionary generator +
mapping-policy file, [5.8](#58-schema-reconciliation-and-the-measurement-mapping-catalog)) —
the largest new component — the **Thanos query client + PromQL translation**,
the **resource-scope resolver**, and the **subscription reporter** loop.

### 4.2 Read path (measurement retrieval)

1. Resolve `resourceScopeCriteria` (key/value pairs) to a set of O2IMS
   inventory Resources via the resource-server, then to Thanos label matchers
   (e.g. `{cluster="...", node="..."}`). See [5.3](#53-resource-scope-resolution-and-layered-attribution).
2. Resolve `measurementSelectionCriteria` to PromQL metric name(s) via the PM
   Dictionary ([5.2](#52-the-pm-dictionary-metric-catalog)).
3. Issue a Thanos `query_range` with `step = collectionInterval` over the
   requested window; map each returned series+sample into the spec's
   measurement representation (`collectedMeasurements`).
4. Health of the evaluation feeds `status`
   (`RUNNING`/`DEGRADED`/`FAILED`, [5.6](#56-state-and-status-model)).

#### Query access: in-cluster Thanos Querier

**Decision: the Performance service queries the in-cluster
`observability-thanos-query.open-cluster-management-observability.svc` Thanos
Querier directly, and does not route through `rbac-query-proxy`.** Rationale:

- **Authorization is already enforced at our own API boundary.** The O-Cloud
  Manager authenticates the SMO via OAuth and authorizes it via the shared
  authenticator/authorizer (OIDC groups → Kubernetes RBAC via
  `SubjectAccessReview`, per **DD-003**). The performance-server is a single
  trusted O2IMS producer applying O2IMS-level authz — it does not need Thanos
  to re-filter per tenant. `rbac-query-proxy`'s per-user cluster filtering
  targets a different consumer model (individual OCP users via the console).
- **Simplicity and performance** — one in-cluster hop over the service network,
  no extra proxy, no mTLS/route dependency, no duplicated authz layer.
- **No ACM change** — we only *read* an existing service; nothing in ACM
  Observability is reconfigured.

Scope narrowing that a tenant proxy would otherwise do is instead applied by
**our** resource-scope resolver ([5.3](#53-resource-scope-resolution-and-layered-attribution)): a job
or subscription only ever queries the Thanos label matchers derived from its
resolved inventory scope, so results are bounded by the request, not by the
querier's global visibility. The server obtains the in-cluster k8s client and
any audience-scoped tokens via `internal/service/common/clients`.

### 4.3 Write path (subscriptions and reporting)

Subscriptions reuse the alarms notifier architecture (`common/notifier` +
per-service `notifier_provider` adapters + `listener` + outbox table):

1. `POST /performanceSubscriptions` persists a `PerformanceSubscriptionInfo`
   (criteria + `reportDeliveryMechanism` + `measurementReportingFrequency`).
2. A **reporter** goroutine schedules per-subscription ticks from
   `measurementReportingFrequency` (`subscriptionMode` TARGET_DEFINED /
   ON_CHANGE / SAMPLE, `reportInterval`, `suppressRedundant`,
   `heartbeatInterval`).
3. On each tick the reporter evaluates the subscription's
   `PerformanceSubscriptionCriteria` against Thanos (read path above), assembles
   a nested `PerformanceMeasurementReport`
   (Report → JobReport → ResourceReport → MeasurementValue, see
   [5.7](#57-measurement-data-and-report-payload-from-o-cloud-im-ref-36)), and
   enqueues it to the outbox; the notifier POSTs it to the subscriber
   `callback`.
4. `suppressRedundant`/ON_CHANGE are implemented by comparing against the last
   reported sample per series and honoring `heartbeatInterval`.

`reportDeliveryMechanism = NOTIFICATION` is implemented first. `FILE`
(generate file + FileReady notification + `remoteFileLocation` push) and
`STREAM` (persistent GPB session) are deferred; the server advertises only the
mechanisms it supports and rejects others with a clear `ProblemDetails`.

---

## 5. Data Model Mapping

### 5.1 MeasurementJob

| Spec attribute | Source / mapping |
|----------------|------------------|
| `performanceMeasurementJobId` | IMS-assigned UUID (our DB primary key) |
| `consumerPerformanceJobId` | stored verbatim from request |
| `state` | lifecycle field in DB (`ACTIVE`/`SUSPENDED`/`DEPRECATED`) |
| `status` | derived from last Thanos evaluation + ACM health |
| `collectionInterval` | stored; reinterpreted as eval/report resolution with floor (Section 3) |
| `resourceScopeCriteria` | stored; resolved to Thanos matchers at eval time |
| `measurementSelectionCriteria` | stored; resolved via PM Dictionary |
| `preinstalledJob` | `true` for allowlist-derived jobs, `false` for SMO-created |
| `qualifiedResourceTypes` | derived from resolved scope (inventory ResourceTypes) |
| `measuredResources` | derived: inventory Resources matching scope |
| `collectedMeasurements` | derived: projected from Thanos result set |
| `extensions` | stored key/value map |

### 5.2 The PM Dictionary (metric catalog)

Per the O-Cloud Information Model (ref [36], clause 4.2.1.4), the "PM
Dictionary" is formally the WG10 `PerformanceMeasureDictionary`, **linked per
`ResourceType`** through `ResourceType.performanceDictionaryId` (a UUID, or
`null` if that resource type exposes no measurements). Each dictionary entry is
a `MeasurementDefinition` identified by a `performanceMeasureName` (string) and
`performanceMeasurementId` (unsigned integer). A `MeasurementJob`'s
`measurementSelectionCriteria` selects definitions (the IM example uses keys
`measurementGroup` + `measurementName`); `resourceScopeCriteria` selects
resources (example key `resourceTypeId`).

In the ACM-backed realization we reconcile this with ACM's available series:

- We build a catalog by reading ACM's allowlist ConfigMaps
  (`observability-metrics-allowlist` + `observability-metrics-custom-allowlist`)
  read-only at startup and on change, yielding the PromQL metric names, labels,
  and the resource types they apply to.
- We **synthesize/align `MeasurementDefinition`s** from that catalog and surface
  a `performanceDictionaryId` per qualified `ResourceType`, mapping each
  `performanceMeasureName` → a PromQL metric (+ label template). Where inventory
  `ResourceType.performanceDictionaryId` is already populated from a WG10
  dictionary, we align names to it rather than inventing new ones.

This catalog backs `measurementSelectionCriteria` validation and PromQL
translation, defines the **preinstalled PM Jobs** published at boot, and is
refreshed by watching the ConfigMaps (no writes — we never edit the allowlist).
How the catalog is structured, generated at scale, and kept compliant is
detailed in [5.8](#58-schema-reconciliation-and-the-measurement-mapping-catalog).

**The allowlist governs coverage** (spike-confirmed,
[docs/spike/perf-spike-findings.md](../spike/perf-spike-findings.md)). ACM's
allowlist is a curated subset of node-exporter, not the full set, and it often
forwards **recording rules instead of raw counters**. Observed consequences:

- Core node metrics are present out of the box (CPU, memory, filesystem) plus
  `kube_node_*` and the `instance:node_*:rate1m` recording rules.
- **Network** is covered via the `instance:node_network_*:rate1m` recording
  rules; the raw `node_network_*_bytes_total` counters are intentionally not
  forwarded — so network measures map to the rules, no allowlist change needed.
- **Disk I/O** (`node_disk_*`) and **hardware telemetry** (`baremetal_*` from the
  ironic exporter) are **not** in the default allowlist and require a
  `observability-metrics-custom-allowlist` addition — an additive, supported ACM
  change that increases forwarded data volume. This is the concrete boundary: we
  serve what ACM forwards; widening coverage is an explicit allowlist decision
  with a volume cost.

### 5.3 Resource scope resolution and layered attribution

`resourceScopeCriteria` keys reference inventory attributes (Resource /
ResourceType, per clause 3.7.6.2). Resolving them to Thanos series is more than a
label lookup, because a performance measurement attaches to a **layered stack of
distinct entities**, not a single resource:

- **`Resource`** (resource-server, `O2ims_InfrastructureInventory`) — the
  **physical server** (bare-metal host, from the BMH; `resourceClass=COMPUTE`,
  `resourceKind=PHYSICAL`).
- **`ClusterResource`** (cluster-server, `O2ims_InfrastructureCluster`) — the
  **Kubernetes node** (the OS/kubelet running on that server, from the Agent).
  It carries `Name = hostname` and `NodeClusterName = cluster`, and links to its
  host via **`ClusterResource.ResourceID → Resource`** (the hosting edge).

Hosting is 1:1 for bare-metal and potentially 1 server : N nodes for virtualized
deployments. Metrics partition by the layer that produces them — but all arrive
through a **single query source, ACM Observability Thanos** (see
[5.3.1](#531-single-source-via-acm-thanos)):

| Bucket | Producer → how it reaches ACM Thanos | Attaches to | Join label(s) |
|---|---|---|---|
| Server-only — thermal, fan, power/drive status (`baremetal_*`) | `metal3-ironic-prometheus-exporter` (hub) → hub Prometheus → ACM `local-cluster` collector → Thanos (needs custom allowlist) | physical `Resource` only | `node_name` → BMH → `Resource` |
| Node-only — kubelet, pod capacity/allocatable, logical FS | kube-state / kubelet → spoke collector → Thanos | `ClusterResource` only | `(cluster, instance)` |
| Overlap — CPU, memory, NIC/disk | node-exporter → spoke collector → Thanos | **both**, via the hosting edge | `(cluster, instance)` |

For the **overlap** bucket, node-exporter reports the OS's view of the physical
server's devices, so the series is genuinely a property of both the Kubernetes
node and the physical server. Attribution therefore depends on hosting
cardinality:

- **1:1 (bare-metal)** — the same series/value attaches directly to both the
  `ClusterResource` and (through `ResourceID`) the physical `Resource`.
- **1:N (virtualized)** — the per-node value is *not* the server's total;
  server-level attribution becomes an **aggregate across the host's nodes** (a
  derived definition, see [5.8](#58-schema-reconciliation-and-the-measurement-mapping-catalog)),
  not the same series.

**Resolver.** The resolver queries the inventory upstreams (resource-server and
cluster-server, via `internal/infrastructure`, as alarms calls other O2IMS
servers) to resolve entities, and walks the `ClusterResource.ResourceID` hosting
edge. Given a job's `resourceScopeCriteria`, it (1) selects the target
ResourceType(s), (2) resolves matching entities, (3) maps each to its Thanos
identity labels, and (4) emits records with the `resourceId` / `resourceTypeId`
of the scoped layer. An empty/match-nothing scope yields an empty result, not an
error.

Identity labels, as confirmed by the feasibility spike
([docs/spike/perf-spike-findings.md](../spike/perf-spike-findings.md)):

- **Node / overlap layer:** node-exporter series carry **`instance` = node FQDN**
  and a **`cluster`** label, but **no `node` label** (and `kube_node_info` is not
  forwarded). The FQDN equals the Agent `hostname` = `ClusterResource.Name`, so
  the join key is **`(cluster, instance)`** — direct string match, no
  correspondence table. (Consequently PromQL recipes and aggregations group by
  **`instance`, not `node`**.)
- **Server / hardware layer:** `metal3-ironic-prometheus-exporter` series
  (`baremetal_*` — `temp_cpu_celsius`, `temp_systemboard_celsius`, `fan_status`,
  `power_status`, `drive_status`, …; all GAUGE/status) carry
  `node_name` = `<bmh-namespace>~<bmh-name>` plus `redfish_system_uuid` /
  `node_uuid` / `managed_cluster` (the `instance` label is just the exporter
  endpoint). The join is **`node_name` → BareMetalHost → `bmh.UID` → physical
  `Resource`** (or via `redfish_system_uuid`). Spike-confirmed; the `ironic_*`
  provisioning-operational metrics are excluded.

**Cross-layer bridging (spike-confirmed).** The hardware layer and the OS/node
layer share **no common label** — a server's BMH name (e.g. `xr8720txdg60`)
differs from its OS hostname (e.g. `cnfdg60`). Node↔server can therefore only be
correlated **through the inventory** (`ClusterResource.ResourceID` hosting edge),
never by matching Prometheus labels across layers — which is exactly why the
resolver routes through the inventory rather than joining series directly.

#### 5.3.1 Single source via ACM Thanos

All layers are queried from the **one** ACM endpoint
(`observability-thanos-query`), because ACM Observability already aggregates the
hub itself as the `local-cluster` managed cluster. Hardware telemetry from the
`metal3-ironic-prometheus-exporter` (a **sidecar in the metal3 pod**,
`openshift-machine-api`, scraped into the hub's Prometheus via its ServiceMonitor)
is federated by the hub's `local-cluster` collector and, once added to the
`observability-metrics-custom-allowlist`, lands in ACM Thanos tagged
`cluster=local-cluster` — exactly like node metrics. So the Performance service
keeps a single query client, one auth model, and one identity model for all
layers.

A dual-source variant — querying the hub's `thanos-querier.openshift-monitoring`
directly for hardware (bearer token + `cluster-monitoring-view`) — is kept only
as a **fallback** for deployments that deliberately keep hardware telemetry out
of ACM to avoid the added data volume.

### 5.4 PerformanceServiceConfiguration

`PerformanceServiceConfiguration.retentionPeriod` is the sharp edge of the
"don't change ACM" constraint: real retention is owned by Thanos and we will
not change it. We will:

- persist `retentionPeriod` using the **same serviceconfig pattern as alarms**
  (`internal/service/alarms/internal/serviceconfig`, a single config row +
  periodic cleanup), and
- **clamp its effective meaning to ACM's actual Thanos retention**: GET returns
  the configured value but the service never serves/claims data beyond what ACM
  retains, and PUT/PATCH validate against a minimum (as alarms does with
  `minRetentionPeriod`). The `extensions` map carries any implementation
  tailoring.

This is documented as a behavioral note in the OpenAPI `description` so
consumers understand retention is bounded by the underlying store.

### 5.5 Subscriptions and PerformanceSubscriptionCriteria

`PerformanceSubscriptionCriteria` (`jobCriteria`, `resourceTypeCriteria`,
`resourceCriteria`, `measurementCriteria`; AND-combined when multiple present)
is stored and evaluated by the reporter. `jobCriteria` references existing
(virtual) jobs; `resource*Criteria` reuse the scope resolver; `measurementCriteria`
reuse the PM Dictionary. `PerformanceReportingFrequency` drives the reporter
cadence as in [4.3](#43-write-path-subscriptions-and-reporting).

### 5.6 State and status model

- **State** (`ACTIVE`/`SUSPENDED`/`DEPRECATED`): owned by us.
  - `suspend` → stop evaluating/reporting the job and mark `SUSPENDED`
    (`status: IDLE`). Note: ACM keeps collecting underneath — suspend only
    pauses *our* reporting, which we document.
  - `resume` → back to `ACTIVE` (only from `SUSPENDED`).
  - `delete` → `DEPRECATED` until retention elapses, then purged
    (`status: PENDING_DELETE`), matching the spec's deprecation semantics.
    Preinstalled jobs cannot be deleted/updated/suspended (403).
- **Status** (`RUNNING`/`FAILED`/`DEGRADED`/`IDLE`/`PENDING_DELETE`): derived
  from the most recent Thanos evaluation — `RUNNING` when queries succeed and
  return data, `DEGRADED` on partial series/partial cluster coverage, `FAILED`
  when Thanos is unreachable or the managed-cluster collector is reporting no
  data, `IDLE` when `SUSPENDED`, `PENDING_DELETE` when `DEPRECATED`. The IM ties
  status to state as a constraint (status ∈ {RUNNING,FAILED,DEGRADED} ⟺ ACTIVE;
  IDLE ⟺ SUSPENDED; PENDING_DELETE ⟺ DEPRECATED), which we enforce.

### 5.7 Measurement data and report payload (from O-Cloud IM, ref [36])

The interface spec (clause 3.7) defers the measurement-value schemas to the
O-Cloud Information Model. The relevant types and their mapping to Thanos:

- **`PerformanceMeasurementRecord`** (IM 4.2.1.4.15) — a single collected
  sample: `performanceMeasurementJobId`, `resourceId`,
  `performanceMeasurementDefinitionId`, `timeStamp`, `measurementValue` (Any),
  and **`isSuspect`** (boolean; "anomalous behaviour detected on the resource
  during the last collection interval"). We **project each Thanos
  sample** into this shape at query time: `timeStamp` ← sample time,
  `measurementValue` ← sample value, `resourceId` ← reverse-mapped from series
  labels, `performanceMeasurementDefinitionId` ← dictionary lookup.
  `isSuspect` is set `true` when Thanos marks the point stale/partial, and
  (per interface NOTE on resume) `true` on the first sample after a job resumes.
- **`CollectedMeasurement`** (IM 4.2.1.4.18) — per-`resourceTypeId` catalog
  entry: `performanceMeasurementDefinitionId`, `timeAdded[]`, `timeDeleted[]`,
  `isCurrentlyMeasured`. This is derived bookkeeping (what the job measures over
  its life), tracked in our DB, not read from Thanos.
- **`PerformanceMeasurementStore`** (IM 4.2.1.4.14) — `retentionPeriod` (days,
  minimum 0; the IM explicitly says "the exact timing of when measurements are
  deleted is implementation dependent"), plus `measurements[]` and `extensions`.
  The "implementation dependent" clause is what makes our clamp-to-Thanos-
  retention approach ([5.4](#54-performanceserviceconfiguration)) spec-compliant.

**NOTIFICATION report payload** — the subscription callback body is
`PerformanceMeasurementReport` (IM 4.2.1.4.24), a nested aggregation the
reporter builds from Thanos results:

```text
PerformanceMeasurementReport
  globalCloudId, notificationTime,
  performanceSubscriptionId, consumerPerformanceSubscriptionId
  jobReports[]  ── JobReport (one per subscribed job)
     performanceJobId, consumerJobId
     measuredResources[]  ── ResourceReport (one per resource)
        resourceId
        measurementValues[]  ── MeasurementValue
           performanceMeasureName | performanceMeasurementId,
           measurementCollectionTime, value, isSuspect
```

This structure drives both the OpenAPI schema for the callback and the
reporter's result assembly in [4.3](#43-write-path-subscriptions-and-reporting).

### 5.8 Schema reconciliation and the Measurement Mapping Catalog

The O-RAN performance schema and the Prometheus/Thanos schema are structurally
different; bridging them is the central engineering task of this service and a
net-new component (bigger than [5.2](#52-the-pm-dictionary-metric-catalog)
alone implies).

#### 5.8.1 Spec layering and the compliance boundary

The schemas come from three layers, and only the first is the O2 wire contract:

| Layer | Spec | Defines |
|---|---|---|
| Management API | WG6 interface, clause 3.7.6 | job / config / subscription JSON schemas |
| Measurement value model | O-Cloud IM (id 892), 4.2.1.4.15–.27 | `PerformanceMeasurementRecord`, `CollectedMeasurement`, `MeasuredResource`, the report payload |
| Metric definition | WG10 IM&DM (id 980), 5.2 → 3GPP TS 32.404 / ETSI NFV-IFA027 | `PerformanceMeasureDictionary`, `PerformanceMeasureDefinition` (incl. `collectionMethod`, `measurementUnit`, `measureObjectClass`) |

The WG6 interface spec itself defines **no metric/measurement-value schema**
(clause 3.7.5: *"does not specify any notifications"*); it only names the
value/record types and defers them to the O-Cloud IM.

**Compliance boundary.** Prometheus/Thanos is a permitted *source* but is not a
compliant *wire format*. Delivering raw Prometheus (exposition text or raw
PromQL JSON) over O2 fails on media type (clause 3.1 mandates JSON), on
structure (must match the IM `PerformanceMeasurementReport`), and on identity
(O-RAN requires `resourceId` / `performanceMeasurementDefinitionId` UUIDs, which
Prometheus has no concept of). **Translation into the O-RAN JSON schema is
therefore mandatory** — it is what makes the implementation compliant, not an
optimization. The SMO never sees Prometheus format; a Prom-native endpoint may
only exist as a non-standard extra, outside O2 conformance.

#### 5.8.2 The mismatch

| Axis | Prometheus/Thanos | O-RAN | Resolution |
|---|---|---|---|
| Identity | metric name + label set | `(resourceId, performanceMeasurementDefinitionId)` UUIDs | catalog + reverse label→resource resolution |
| Granularity / layer | one series per label combo | measure attaches to a layered stack (physical server / k8s node / cluster / pool) | owning-layer assignment + hosting edge + aggregation ([5.3](#53-resource-scope-resolution-and-layered-attribution)) |
| Value semantics | counter/gauge/histogram (float64) | one scalar per definition | `collectionMethod`-driven transform |
| Definition | none (name only) | `PerformanceMeasureDefinition` (unit, method, objectClass) | synthesized dictionary |
| Time/resolution | scrape-aligned, downsampled | `timeStamp` at `collectionInterval` | `step` + `isSuspect` on resolution mismatch |

`collectionMethod` is the key reconciliation lever:

| `collectionMethod` | Prom type | PromQL transform |
|---|---|---|
| CC (cumulative counter) | counter | `rate()` / `increase()` over `counterWindow` (≥2 samples) |
| GAUGE | gauge | raw / `avg_over_time` / `last_over_time` |
| DER (discrete event) | counter | `increase()` over `counterWindow` |
| SI (status inspection) | gauge | instant sample |
| (histogram/summary) | histogram | `histogram_quantile(q, rate(..._bucket[counterWindow]))` |

#### 5.8.3 Catalog entry schema

The catalog is a populated `PerformanceMeasureDictionary` **per ResourceType**.
Each metric is assigned to its **owning layer** ([5.3](#53-resource-scope-resolution-and-layered-attribution)):
most metrics produce **one** definition in their owning type's dictionary;
overlap (hardware-device) metrics additionally get an entry under the physical
`Resource` type via the hosting edge; cluster/pool aggregates are **separate**
definitions with their own PromQL (not reuse of the base series). Each entry is a
real `PerformanceMeasureDefinition` **plus** the two facts Prometheus carries
that O-RAN has no field for — the PromQL recipe and the resource-identity labels:

```yaml
# --- the O-RAN PerformanceMeasureDefinition (standardized fields) ---
performanceMeasureDefinitionId: 31040dec-…   # synthesized, stable
performanceMeasureId: 1001                    # synthesized, stable
measurementName: cpuAverageUtilization
collectionMethod: GAUGE                       # drives the transform
measurementUnit: percent
measureObjectClass: KubernetesNode            # owning layer (ClusterResource)
description: Average CPU utilization over the collection interval
# --- the Prometheus bridge (our additions, not O-RAN fields) ---
promql: '100 * (1 - avg by (cluster,instance)(rate(node_cpu_seconds_total{mode="idle", <scope>}[${counterWindow}])))'
resourceIdentityLabels: [cluster, instance]   # instance = node FQDN = ClusterResource.Name (spike-confirmed)
sourceMetrics: [node_cpu_seconds_total]
counterWindow: 5m                             # rate()/increase() window; ≥2 samples, independent of the query step
alsoAttachesTo:                               # overlap bucket only
  resourceType: PhysicalServer                # via ClusterResource.ResourceID (1:1)
  whenHosting: oneToOne                        # 1:N ⇒ use the separate aggregate definition
```

Node-only metrics omit `alsoAttachesTo`. Server-only (hardware) metrics have
`measureObjectClass: PhysicalServer`, `resourceIdentityLabels` on the BMH/node
name, and resolve directly on the BMH-keyed `Resource` (arriving via
`cluster=local-cluster`, [5.3.1](#531-single-source-via-acm-thanos)). All
recipes group by **`instance`, not `node`** — node-exporter carries no `node`
label in ACM (spike-confirmed).

The query **`step`** is the job's `collectionInterval`; **`counterWindow`** is a
*separate* `rate()`/`increase()` window for counter-backed recipes, set long
enough to span at least two forwarded samples (default
`max(5m, 2× the effective collection interval)`). Keeping it independent of the
step matters: at the interval floor the step alone could contain a single
forwarded sample, and `rate()` over a single-sample window returns no series —
so counter-backed definitions could otherwise yield no data at a valid interval.

#### 5.8.4 Translation

- **Forward (query):** `resourceScopeCriteria` → target ResourceType(s) → the
  resolver ([5.3](#53-resource-scope-resolution-and-layered-attribution)) yields
  the matching entities and their identity-label matchers (walking the
  `ClusterResource.ResourceID` hosting edge when the scope is the physical layer);
  `measurementSelectionCriteria` → catalog entries → `promql` with `${step}`
  (= `collectionInterval`), `${counterWindow}`, and `<scope>` substituted →
  Thanos `query_range`.
- **Reverse (project):** each returned series → read its `resourceIdentityLabels`
  → resolve to the entity at the **scoped layer** (ClusterResource directly, or
  the physical `Resource` through the hosting edge) → emit a
  `PerformanceMeasurementRecord` with that layer's `resourceId` /
  `resourceTypeId`, `performanceMeasurementDefinitionId` from the entry, value
  after transform, `timeStamp` from the sample, `isSuspect` from
  staleness/resolution.

#### 5.8.5 Building the catalog at scale

We do **not** hand-author a recipe per metric. The catalog is mostly generated;
the hand-written part is small and per-*pattern*:

1. **Scope bounds the problem.** Only metrics whose labels carry O-Cloud
   resource identity (`cluster`, `node`, storage/pool labels) qualify;
   pod/container/runtime metrics self-filter out. "All Prom metrics" becomes the
   node/cluster/storage-scoped subset of the ACM allowlist — tens to
   low-hundreds, per a few ResourceTypes.
2. **Auto-generate the long tail** from Prometheus metadata: `/api/v1/metadata`
   gives `TYPE` (→ `collectionMethod`) and `HELP` (→ `description`); the
   name-suffix convention gives `measurementUnit`; `/api/v1/labels` + series
   sampling give available identity labels; IDs are synthesized (stable hash of
   name+type). A raw gauge yields `promql: <name>{<scope>}`, a counter yields
   `rate(<name>{<scope>}[counterWindow])` — where `counterWindow` is independent
   of the query step and long enough to span at least two forwarded samples
   (default `max(5m, 2× the effective collection interval)`) — zero per-metric
   authoring.
3. **Hand-write only a compact mapping-policy file** (per-pattern, not
   per-metric): owning-layer rules that map a metric source/label-set to its
   ResourceType (`node-exporter {cluster,node}` → KubernetesNode; BMC/Redfish →
   PhysicalServer) plus the hosting relationship per ResourceType (so the
   resolver knows whether overlap metrics attach to the physical server directly
   (1:1) or via aggregation (1:N)); unit/type overrides for exceptions; and
   ~20–40 composite/derived KPI recipes (utilization %, saturation, percentiles,
   cluster/pool aggregates) that need bespoke PromQL.
4. **Validate at startup** against the live allowlist; drop entries whose source
   metrics are absent (→ not advertised).

```text
ACM allowlist ─┐
Thanos /metadata,/labels,/series ─┤→ generator → draft dictionary (per ResourceType)
hand-written mapping-policy file ─┘        ↓ validate vs live allowlist
                                   versioned published dictionary (data, not code)
```

**Coverage degrades gracefully:** Tier 0 = ~30 curated KPIs per ResourceType
(ship with MVP); Tier 1 = auto-generated pass-through definitions for the rest
(compliant, if less polished); Tier 2 = composites on demand. A metric without a
curated entry still yields compliant output — never all-or-nothing.

#### 5.8.6 Edge cases

Unmappable series (labels resolve to no Resource) are dropped and logged;
Resources with no series → omitted or `isSuspect`, feeding DATA UNAVAILABLE /
`status=DEGRADED`; windows served from 5m/1h downsample → `isSuspect=true` with
the actual step noted.

---

## 6. Semantic Gaps and How We Resolve Them

| Spec expectation | Reality with ACM (unchanged) | Resolution |
|------------------|------------------------------|------------|
| PM Job defines its own collection interval | ACM collector interval is fixed and shared | `collectionInterval` = eval/report resolution, floored at ACM's interval; sub-floor requests → 400 |
| PM Job *collects and stores* measurements | ACM collects & stores; we must not duplicate | Virtual jobs; measurements read on-demand from Thanos (Section 3) |
| Service controls retention (`retentionPeriod`) | Thanos owns retention | Store the value (alarms pattern) but bound effective behavior to Thanos retention (5.4) |
| Suspend stops collection | We cannot stop ACM collection | Suspend stops *our* reporting/evaluation only; documented |
| Measurements always available when expected | Bounded by Thanos availability/retention; gaps possible | `status` reflects DEGRADED/FAILED; subscription DATA UNAVAILABLE path per clause 2.1.7.2.6 |
| Measured values queried per PM Dictionary | ACM exposes PromQL series + allowlist | PM Dictionary built read-only from allowlist; selection → PromQL (5.2) |

---

## 7. Phased Implementation

The chosen scope is the **full API including subscriptions**, delivered as
reviewable PRs. Each phase is independently mergeable; the API advertises only
implemented capability until complete. A **feasibility spike (P-1) gates the
build** — the design rests on empirical assumptions about ACM (the identity join,
allowlist coverage, hardware telemetry) that must be validated against a live
cluster before committing to P1+.

| Phase | PR theme | Scope |
|-------|----------|-------|
| **P-1** | Feasibility spike (gate) | Validate three risks on a live ACM hub via `hack/perf-spike.sh` — identity join, coverage, hardware telemetry (`docs/spike/perf-spike-findings.md`). **Gates P1+**; rescope (e.g. k8s-node only) if the join or coverage is weak. |
| **P0** | Proposal + contract | This document; hand-authored `openapi.yaml` for `/O2ims_infrastructurePerformance/v2` covering all clause-3.7 resources/types. No handlers yet. |
| **P1** | Service scaffolding | New `internal/service/performance` tree (cmd, `serve.go`, generated handlers, Postgres `performance` DB + baseline migrations, serviceconfig stub). Inventory-controller deploy wiring, RBAC, SA, bundle. Health/`apiVersions` only. See [Appendix B](#appendix-b-files-to-create-and-modify). |
| **P2** | Read path + mapping catalog | Measurement Mapping Catalog ([5.8](#58-schema-reconciliation-and-the-measurement-mapping-catalog)): dictionary generator (from ACM allowlist + Thanos metadata), mapping-policy file, Tier-0 golden-set KPIs. Thanos query client + PromQL translation; resource-scope resolver; preinstalled PM Jobs; `GET /measurementJobs`, `GET /measurementJobs/{id}` (filter/field selection/paging per ETSI SOL 013); `GET /performanceServiceConfiguration`. |
| **P3** | Job lifecycle | `POST`/`PATCH`/`DELETE /measurementJobs/{id}`; `suspend`/`resume` task resources; state machine (ACTIVE/SUSPENDED/DEPRECATED) + deprecation purge; `PUT`/`PATCH /performanceServiceConfiguration`; `collectionInterval` floor validation. |
| **P4** | Subscriptions + NOTIFICATION | Subscription CRUD (`performanceSubscriptions`); reporter loop; `PerformanceMeasurementEventNotification` delivery via the existing notifier (NOTIFICATION); `suppressRedundant`/ON_CHANGE/heartbeat logic. |
| **P5** (future) | FILE / STREAM | File generation + FileReady notification + `remoteFileLocation` push; STREAM (GPB) session. Deferred. |

Cross-cutting, every phase: `make go-generate` + `make generate` + `make
bundle`; RBAC reviewed against `config/rbac/role.yaml`; reader/deployer role
coverage and `openapi.yaml` `security` annotations updated per **DD-004**;
tests added with each behavior.

**CI note (external dependency).** The service reads ACM Observability (Thanos
service, allowlist ConfigMaps) and may watch the `MultiClusterObservability`
CR. The TLS-scan CI environment installs only ACM + our bundle. Reading the
Thanos *Service* and ConfigMaps needs no extra CRDs, but if we add a
**`Watches`/`Owns` on `MultiClusterObservability`** we must confirm the CRD is
present in CI or add a stub to the CI prereq script, otherwise the manager's
informer cache sync will time out and crash-loop (see
`docs/dev/external-dependencies.md`). Preference: avoid watching the MCO CR;
poll availability via the Thanos Service instead.

**must-gather.** Add a `gather_resource` call for the new performance resources
in `must-gather/gather`.

---

## 8. Verification Plan

- **Unit** — PromQL translation (scope + selection → query_range), PM
  Dictionary parsing from sample allowlist ConfigMaps, `collectionInterval`
  floor validation, state-machine transitions (incl. preinstalled-job
  immutability → 403, non-suspended delete → 409, deprecation purge),
  `suppressRedundant`/ON_CHANGE/heartbeat reporter logic, filter/field-selector
  handling per ETSI SOL 013.
- **Deletion-handling regression** — per AGENTS.md, verify a deleting resource
  doesn't trigger normal reconciliation side effects where reconcilers are
  involved.
- **envtest** — Postgres repo layer (job defs, subscriptions, outbox, service
  config) against the baseline migrations.
- **Integration (mock Thanos)** — a stub PromQL endpoint exercising read path,
  DEGRADED/FAILED status derivation, and the subscription reporter → callback
  flow end to end.
- **e2e (ACM present)** — against a cluster with ACM Observability enabled:
  preinstalled jobs appear, `query_range` returns real series, a NOTIFICATION
  subscription delivers reports to a test callback. Run `make test-e2e`.
- **CI** — `make ci-job` (includes `bundle-check`); confirm the new
  `performance` DB provisions via `internal/service/postgres/files/init.sh`
  from the added password env var.

---

## 9. Spec Coverage and Omissions

### 9.1 Coverage summary

Covered by the OpenAPI contract and §§4–5 (spike-validated where noted):

- All **clause-2.1.7** service operations (12) and all **clause-3.7** REST
  resources, methods, and management data types.
- The **measurement/report data model** (O-Cloud IM 4.2.1.4) — §5.7.
- **Metric-definition reconciliation** to Prometheus (WG10 + §5.8), with real
  metric names confirmed by the spike.
- Identity/attribution (§5.3), states/statuses (§5.6), retention (§5.4),
  NOTIFICATION reporting + the OpenAPI `callback` declaration.
- The ACM-constraint semantic gaps (collection interval, retention, suspend) —
  §6.

### 9.2 Material items — design

#### 9.2.1 PM-job failure → O-Cloud alarm (clause 2.1.7.2.6)

When a job's status becomes `FAILED`/`DEGRADED` (Thanos unreachable, the
managed-cluster collector reporting no data, or evaluation errors — §5.6), the
Performance service **raises an O-Cloud alarm through the Alarms/Monitoring
service**, keyed by a PM-job alarm definition (e.g. `PerformanceMeasurementJobFailed`,
`PerformanceDataUnavailable`), and **clears it** when the job returns to
`RUNNING`. Because PM jobs are virtual (our DB), Alertmanager cannot source these,
so the Performance service raises them directly via the alarms server (a
cross-service call); infrastructure-level data-source outages (collector down)
may also already surface through ACM's own alerts. The alarm definition(s) + an
ingestion path on the alarms server are **net-new cross-service work**.

#### 9.2.2 Reporting processes: single structure, multiple via subscriptions

The service description (2.1.7.2.4) speaks of an *array* of monitoring processes,
but the normative interface data model (3.7.6.2.6) types
`measurementReportingFrequency` as a **single** `PerformanceReportingFrequency`.
We follow the data model (the wire contract) and realize "multiple monitoring
processes" by **creating multiple subscriptions** (one per cadence/mode) rather
than an array within one subscription. Documented so the discrepancy is explicit;
revisit if the IM stage-3 OpenAPI adopts an array.

#### 9.2.3 Delivery resilience: connection loss, data unavailable, storm control (clause 2.1.7.2.6)

- **Undelivered reports** persist in the outbox with retry + backoff, bounded by
  a retention limit; the subscription cursor advances only on success (reuses the
  alarms notifier/outbox).
- **Storm control on reconnect:** after a subscriber outage, stale reports beyond
  a max age are coalesced/dropped rather than replayed en masse; for
  `SAMPLE` + `suppressRedundant`, only the latest value matters, which naturally
  bounds the burst.
- **Data unavailable** (Thanos/collector down): empty/failed queries →
  `DEGRADED`/`FAILED`, the report flags the gap, and the PM-job alarm
  ([9.2.1](#921-pm-job-failure--o-cloud-alarm-clause-21726)) is raised.

These land in **P4** (subscriptions), alongside the NOTIFICATION reporter.

### 9.3 Deferred by explicit choice

- **FILE and STREAM** report delivery — only NOTIFICATION is implemented (P5). The
  data model includes them; delivery is deferred.

### 9.4 Thin / partially covered

- `isSuspect` — approximated from Thanos staleness, not true anomaly detection.
- `measurementValue` "Any type" — modeled untyped; ValueType variety not elaborated.
- Response-too-big (ETSI SOL 013 alternative-1 error) — only paging covered.
- Preinstalled-job read-only `extensions` — subsumed under 403-on-preinstalled.
- The concrete preinstalled PM Job set — now enumerated in
  [9.6](#96-tier-0-preinstalled-pm-jobs-golden-set).

### 9.5 Realized-coverage limits (from the spike)

Not spec gaps, but they bound what is deliverable on the tested environment:

- **Multi-node untested** (all SNO) — per-node join/grouping validated only for SNO.
- **Hardware thin on power-in-watts** (mostly thermal + component status);
  `ironic_*` excluded.
- **Disk I/O + hardware require a custom-allowlist addition** — the coverage boundary.
- **Single-source hardware round-trip** (`baremetal_*` into ACM Thanos) not yet
  confirmed.

### 9.6 Tier-0 preinstalled PM Jobs (golden set)

The O-Cloud publishes these preinstalled jobs at boot (`preinstalledJob=true`,
state `ACTIVE`), one per ResourceType (optionally split by domain). Every measure
below is backed by a metric the spike confirmed present. Availability: ✅ default
allowlist; ⚠️ requires a `observability-metrics-custom-allowlist` entry.

**KubernetesNode (`ClusterResource`) — per node** — join `(cluster, instance)`

| measureName | method | unit | source (PromQL) | avail |
|---|---|---|---|---|
| cpuUtilization | GAUGE | percent | `instance:node_cpu_utilisation:rate1m` | ✅ |
| numCpu | GAUGE | count | `instance:node_num_cpu:sum` | ✅ |
| load1PerCpu | GAUGE | ratio | `instance:node_load1_per_cpu:ratio` | ✅ |
| memoryUtilization | GAUGE | ratio | `instance:node_memory_utilisation:ratio` | ✅ |
| memoryAvailableBytes | GAUGE | bytes | `node_memory_MemAvailable_bytes` | ✅ |
| memoryTotalBytes | GAUGE | bytes | `node_memory_MemTotal_bytes` | ✅ |
| filesystemUtilization | GAUGE | percent | `100*(1-node_filesystem_avail_bytes/node_filesystem_size_bytes)` | ✅ |
| filesystemAvailBytes | GAUGE | bytes | `node_filesystem_avail_bytes` | ✅ |
| networkReceiveThroughput | GAUGE | bytes/s | `instance:node_network_receive_bytes_excluding_lo:rate1m` | ✅ |
| networkTransmitThroughput | GAUGE | bytes/s | `instance:node_network_transmit_bytes_excluding_lo:rate1m` | ✅ |
| networkReceiveDrops | GAUGE | pkt/s | `instance:node_network_receive_drop_excluding_lo:rate1m` | ✅ |
| networkTransmitDrops | GAUGE | pkt/s | `instance:node_network_transmit_drop_excluding_lo:rate1m` | ✅ |
| majorPageFaults | GAUGE | faults/s | `instance:node_vmstat_pgmajfault:rate1m` | ✅ |
| diskIoUtilization | CC | ratio | `rate(node_disk_io_time_seconds_total[${counterWindow}])` | ⚠️ |

**NodeCluster — per cluster** — aggregate scope

| measureName | method | unit | source (PromQL) | avail |
|---|---|---|---|---|
| clusterCpuUtilization | GAUGE | ratio | `cluster:node_cpu:ratio` | ✅ |
| clusterCpuUsageCores | GAUGE | cores | `cluster:cpu_usage_cores:sum` | ✅ |
| clusterCpuCapacityCores | GAUGE | cores | `cluster:capacity_cpu_cores:sum` | ✅ |
| clusterMemoryUtilization | GAUGE | ratio | `cluster:memory_utilized:ratio` | ✅ |
| clusterMemoryUsageBytes | GAUGE | bytes | `cluster:memory_usage_bytes:sum` | ✅ |
| clusterMemoryCapacityBytes | GAUGE | bytes | `cluster:capacity_memory_bytes:sum` | ✅ |

**PhysicalServer (`Resource`) — per bare-metal host** — join `node_name` → BMH

| measureName | method | unit | source (PromQL) | avail |
|---|---|---|---|---|
| cpuTemperature | GAUGE | celsius | `baremetal_temp_cpu_celsius` | ⚠️ |
| systemBoardTemperature | GAUGE | celsius | `baremetal_temp_systemboard_celsius` | ⚠️ |
| temperatureStatus | SI | status | `baremetal_temperature_status` | ⚠️ |
| fanStatus | SI | status | `baremetal_fan_status` | ⚠️ |
| powerStatus | SI | status | `baremetal_power_status` | ⚠️ |
| driveStatus | SI | status | `baremetal_drive_status` | ⚠️ |
| nvmePowerBudgetWatts | GAUGE | watts | `machine_nvm_avg_power_budget_watts` | ⚠️ |

The ✅ KubernetesNode + NodeCluster measures ship with no ACM change; the
PhysicalServer set and `diskIoUtilization` activate once their metrics are added
to the custom allowlist ([5.2](#52-the-pm-dictionary-metric-catalog)). IDs
(`performanceMeasureDefinitionId`/`performanceMeasureId`) are synthesized and
stable per [5.8.3](#583-catalog-entry-schema).

---

## Appendix A: REST Resource / Method Coverage

| Resource | Method | Cat | Notes |
|----------|--------|-----|-------|
| `/measurementJobs` | POST | M | Create job (virtual); 201 `MeasurementJob` |
| `/measurementJobs` | GET | M | List; filter / all_fields / fields / exclude_fields / exclude_default / nextpage_opaque_marker |
| `/measurementJobs/{id}` | GET | M | Get one |
| `/measurementJobs/{id}` | PATCH | M | merge-patch; 403 preinstalled, 404, 412 etag |
| `/measurementJobs/{id}` | DELETE | M | 204; 409 if not suspended; 403 preinstalled; 404 |
| `/measurementJobs/{id}/suspend` | POST | M | 202; `MeasurementJobSuspendRequest` (time window); 409 deprecated; 404 |
| `/measurementJobs/{id}/resume` | POST | M | 202; 409 deprecated; 404 |
| `/performanceServiceConfiguration` | GET | M | `retentionPeriod` (+ extensions) |
| `/performanceServiceConfiguration` | PUT | M | whole replace; 412 etag |
| `/performanceServiceConfiguration` | PATCH | M | merge-patch; 412 etag |
| `/performanceSubscriptions` | POST | M | `PerformanceSubscriptionData` → 201 `PerformanceSubscriptionInfo` |
| `/performanceSubscriptions` | GET | M | List; filter / field selectors / paging |
| `/performanceSubscriptions/{id}` | GET | M | Get one |
| `/performanceSubscriptions/{id}` | PATCH | M | merge-patch; 412 etag |
| `/performanceSubscriptions/{id}` | DELETE | M | 204; 404 |

Unsupported methods return 405 per ETSI GS NFV-SOL 013 clause 6.4. Errors use
`ProblemDetails` (RFC 7807) per clause 3.1.5.

## Appendix B: Files to Create and Modify

**Create — `internal/service/performance/`** (mirroring `internal/service/alarms`):
`api/openapi.yaml` (+ `api/tools/{generate.go,oapi-codegen.yaml}` →
`api/generated/performance.generated.go`), `api/server.go`
(`PerformanceServer`/`PerformanceServerConfig`, `baseURL =
constants.O2IMSPerformanceBaseURL`, `DefaultRetentionPeriod`), `cmd/{root.go,
serve.go,migrate.go}`, `serve.go`, `migration.go`,
`internal/db/{migrations,models,repo}` (outbox + subscription-info +
performance-job + performance-service-configuration tables; repo interface +
mock), `internal/serviceconfig/serviceconfig.go`,
`internal/notifier_provider/{subscription.go,notification.go}`,
`internal/listener/pg_listener.go`, and net-new `internal/thanos` (query
client + PromQL translation), `internal/pmdictionary` (allowlist catalog),
`internal/resolver` (resource-scope), `internal/reporter` (subscription ticks).

**Modify — central wiring:**

- `main.go` — register `GetPerformanceRootCmd`.
- `internal/constants/constants.go` — `O2IMSPerformanceAPIPath`,
  `O2IMSPerformanceBaseURL = O2IMSPerformanceAPIPath + APIVersionV2`, endpoint
  segments, `PerformanceServerCmd`.
- `internal/controllers/utils/constants.go` — `InventoryPerformance` /
  `InventoryPerformanceServerName`, `PerformanceServerArgs`,
  `PerformancePasswordEnvName`.
- `internal/controllers/utils/{utils.go,types.go}` — `HasDatabase`,
  `NeedsOAuthAccess`, `GetServerDatabasePasswordName`, `GetServerArgs`;
  condition types + Available/Error name→type maps.
- `internal/controllers/inventory_controller.go` —
  `setupPerformanceServerConfig` + call site; pod-ready list; deployment-status
  list; ingress path rule; used-config status.
- `internal/controllers/inventory_controller_postgres.go` — add
  `PerformancePasswordEnvName` to `createPasswords`.
- `api/inventory/v1alpha1/inventory_types.go` — `PerformanceServerUsedConfig`
  (+ `make generate` deepcopy).
- `config/rbac/` — `clusterrole_performance_server.yaml` (+ role/rolebinding if
  needed), `performance-server` SA in `service_accounts_services.yaml`,
  bindings in `clusterrolebindings_services.yaml`, register in
  `kustomization.yaml`; update `oran_o2ims_user_roles.yaml` /
  `oran_o2ims_oauth_role_bindings.yaml` per **DD-004**.
- `config/prometheus/services-monitor.yaml`, CSV
  `config/manifests/bases/oran-o2ims.clusterserviceversion.yaml`, regenerate
  `bundle/`.
- `must-gather/gather` — `gather_resource` for performance resources.

## Appendix C: Document Version History

| Version | Date | Author | Change |
|---------|------|--------|--------|
| 0.1 | 2026-11-05 | @browsell | Initial proposal: `O2ims_InfrastructurePerformance` over ACM Observability — virtual PM jobs over Thanos, layered attribution, Measurement Mapping Catalog (O-RAN↔Prometheus), NOTIFICATION-first subscriptions, feasibility-spike gate, Tier-0 golden set |
