# O2IMS Interface v13 Alignment

```yaml
title: o2ims-v13
version: "3.2"
authors:
  - @rauherna
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-09-21
last-updated: 2026-10-30
change-history: "#appendix-b-document-version-history"
```

**Epic**: [CNF-26925](https://redhat.atlassian.net/browse/CNF-26925)
**Spec**: O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v13.00

---

## Table of Contents

- [1. Executive Summary](#1-executive-summary)
- [2. Previous v11 Implementation Reference](#2-previous-v11-implementation-reference)
- [3. Inventory Interface Analysis](#3-inventory-interface-analysis-o2ims_infrastructureinventory)
  - [3.1 API Version History](#31-api-version-history)
  - [3.2 Gap Assessment](#32-gap-assessment)
- [4. Provisioning Interface Analysis](#4-provisioning-interface-analysis-o2ims_infrastructureprovisioning)
  - [4.1 API Version History](#41-api-version-history)
  - [4.2 Gap Assessment](#42-gap-assessment)
- [5. Monitoring/Alarms Interface Analysis](#5-monitoringalarms-interface-analysis-o2ims_infrastructuremonitoring)
  - [5.1 API Version History](#51-api-version-history)
  - [5.2 Gap Assessment](#52-gap-assessment)
- [6. Phased Implementation](#6-phased-implementation)
- [7. Verification Plan](#7-verification-plan)
- [Appendix A: Spec Reference Mapping](#appendix-a-spec-reference-mapping)
- [Appendix B: Document Version History](#appendix-b-document-version-history)
- [Appendix C: PR 4 OpenAPI Draft (Purge + Task Operations)](#appendix-c-pr-4-openapi-draft-purge--task-operations)

---

## 1. Executive Summary

This document analyzes the changes required to align the O-Cloud Manager's O2 APIs
with v13 of the O-RAN O2IMS Interface specification. Three interfaces are in scope:
Inventory, Provisioning, and Monitoring (alarms).

**Key finding**: Inventory and Provisioning are already ~95–100% aligned with v13
thanks to the v11 PRs (#2187, #2322, #2369), which implemented draft CRs later
ratified in v13. **Monitoring/Alarms is the main work area**, requiring new endpoints,
data model changes, and a major API version bump (v1 → v2).

| Interface | Current API | v13 API |
|-----------|-------------|---------|
| Inventory | 2.0.0 | 2.0.0 |
| Provisioning | 1.2.0 | 1.2.0 |
| Monitoring | ~1.1.0 | 2.2.0 |

---

## 2. Previous v11 Implementation Reference

Our last spec alignment was to v11, implemented across three PRs:

| PR | Description |
|----|-------------|
| [#2187](https://github.com/openshift-kni/oran-o2ims/pull/2187) | Inventory phase 1 (non-breaking): new Location, OCloudSite, ResourcePool CRDs and controllers |
| [#2322](https://github.com/openshift-kni/oran-o2ims/pull/2322) | Inventory phase 2 (breaking): full service layer — DB, REST endpoints, collectors, HW plugin, new BMH labels |
| [#2369](https://github.com/openshift-kni/oran-o2ims/pull/2369) | Provisioning (non-breaking): status fields, response code changes, field renames |

---

## 3. Inventory Interface Analysis (O2ims_InfrastructureInventory)

### 3.1 API Version History

| API Version | Changes |
|-------------|---------|
| 1.0.0 | Initial — O-Cloud, ResourceType, ResourcePool, Resource, DeploymentManager, Subscriptions |
| 1.1.0 | Modified: InventorySubscriptionDescription; New: Performance/Alarm Dictionaries |
| 1.2.0 | Modified: ResourceTypeInfo (resourceKind, resourceClass) |
| **2.0.0** | **New: Location, O-Cloud Site endpoints. Modified: ResourcePoolInfo, CloudInfo** |

### 3.2 Gap Assessment

Functionally aligned. The v11 PRs implemented all v2.0.0 features ahead of
v13 ratification (Location, OCloudSite, ResourcePool endpoints, globalCloudId,
oCloudSiteId, notification support).

| # | v13 Requirement | Current | Action |
|---|-----------------|---------|--------|
| I-1 | OpenAPI spec references v13 | References v11 | Update `info` block |
| I-2 | Deprecated fields on ResourcePoolInfo | Absent | See below |
| I-3 | `probableCauses` list in AlarmDictionary (mandatory, §3.2.6.2.8) | Absent | Generated from the alert-name mapping table. See [probableCauseId Population](#probablecauseid-population-m-14) (M-14) |

**I-2: Deprecated fields — leave absent (decided).**

The v11 and v13 specs both mark `oCloudId` and `globalLocationId` on
ResourcePoolInfo as mandatory but deprecated. Our implementation never
introduced these fields — we went directly to `oCloudSiteId` as the
replacement. The team agreed to leave them absent and document the deviation.
No consumer expects them.

---

## 4. Provisioning Interface Analysis (O2ims_InfrastructureProvisioning)

### 4.1 API Version History

| API Version | Changes |
|-------------|---------|
| 1.0.0 | Initial: ProvisioningRequest List + Description |
| 1.1.0 | Modified: ProvisioningRequestInfo |
| **1.2.0** | **Modified: ProvisionedResourceSet, ProvisioningStatus. New: ResourceProvisioningStatus** |

### 4.2 Gap Assessment

Functionally aligned. PR #2369 implemented all v1.2.0 changes
(ProvisionedResourceSet, ProvisioningStatus, ResourceProvisioningStatus type
and ResourceProvisioningPhase enum). Only documentation-level update remains.

| # | v13 Requirement | Current | Action |
|---|-----------------|---------|--------|
| P-1 | OpenAPI spec references v13 | References v11 | Update `info` block |

**Total provisioning changes: 1 item, documentation-level only.**

---

## 5. Monitoring/Alarms Interface Analysis (O2ims_InfrastructureMonitoring)

### 5.1 API Version History

| API Version | Changes |
|-------------|---------|
| 1.0.0 | Initial: Alarm Query, Subscription, Notification |
| 1.1.0 | New: Acknowledge, Clear, Service Config. Updated: Subscription |
| 1.2.0 | New: Purge Alarms Task, Task Operations. Updated: Notification, AlarmSubscription, AlarmEventRecord |
| 2.0.0 | Updated: Alarm Change Notification |
| 2.1.0 | Incremental updates |
| **2.2.0** | **New: Alarm Subscription Update (PATCH). Updated: AlarmSubscriptionInfo** |

Our current implementation references R003-v06.00 (June 2024) and is at roughly
API v1.1.0 level. We need to reach v2.2.0.

### 5.2 Gap Assessment

> **Note — Out of scope for this proposal:**
>
> - **Alarm dictionary taxonomy (Section 4)**: The standardized alarm *definition*
>   IDs (e.g., `CLUSTER_WARNING`) are alarm-producer taxonomy concerns, not API
>   contract changes, and remain out of scope. Note: `probableCauseId` population
>   is **no longer deferred** — it is addressed by [M-14](#probablecauseid-population-m-14)
>   via a maintained alert-name mapping table, which also closes the mandatory
>   dictionary `probableCauses` gap.
> - **Performance dictionaries**: Our implementation derives everything from
>   Prometheus — alarms from PrometheusRules/AlertManager, and metrics go
>   directly through the Prometheus/Thanos stack without passing through the
>   O-Cloud Manager. The O-RAN spec envisions the O-Cloud exposing a performance
>   monitoring interface, but we don't implement
>   `O2ims_InfrastructurePerformanceMonitoring` at all. The performance dictionary
>   endpoints in the inventory API are just the catalog of "what can be measured,"
>   independent of the actual measurement delivery.

| # | Requirement | Since | Current | Action | Breaking? |
|---|-------------|-------|---------|--------|-----------|
| M-1 | `objectClass` on AlarmEventRecord | 2.0.0 | Not implemented | See [AlarmEventRecord](#alarmeventrecord), [DB: alarm_event_record](#db-schema-alarm_event_record-m-1-m-9) | Yes (when mandatory) |
| M-2 | `eventFilter` on AlarmSubscriptionInfo | 2.2.0 (v13) | Uses `filter` enum | See [AlarmSubscriptionInfo](#alarmsubscriptioninfo), [DB: alarm_subscription_info](#db-schema-alarm_subscription_info-m-2-m-10) | No |
| M-3 | PATCH `/alarmSubscriptions/{id}` | 2.2.0 (v13) | Not implemented | See [Missing Endpoints](#missing-endpoints), [AlarmSubscriptionUpdate](#alarmsubscriptionupdate-new-type) | No |
| M-4 | `objectClass` on Alarm Change Notification | 2.0.0 | Not implemented | See [Alarm Change Notification](#alarm-change-notification) | No |
| M-5 | `objectRef` on Alarm Change Notification | 2.0.0 | Not implemented | See [Alarm Change Notification](#alarm-change-notification) | No |
| M-6 | `consumerSubscriptionId` in notification | 2.0.0 | Not in payload | See [Alarm Change Notification](#alarm-change-notification) | No |
| M-7 | API URL path v1 → v2 | 2.0.0 | Currently `/v1/` | Change to `/v2/`. See [Version Management](#version-management-m-7) | **Yes** |
| M-8 | OpenAPI spec references v13 | v13 | References R003-v06.00 | Update `info` block | No |
| M-9 | JSON field naming (5 fields) | 2.0.0 | Wrong names/casing | See [AlarmEventRecord](#alarmeventrecord), [Alarm Change Notification](#alarm-change-notification), [DB: alarm_event_record](#db-schema-alarm_event_record-m-1-m-9) | Yes (v2 wire format) |
| M-10 | `consumerSubscriptionId` UUID→String | Inherited | UUID-only | See [AlarmSubscriptionInfo](#alarmsubscriptioninfo), [DB: alarm_subscription_info](#db-schema-alarm_subscription_info-m-2-m-10) | No |
| M-11 | `POST /alarms/purge` (Purge Alarms Task) | 1.2.0 | Not implemented | New async endpoint. See [Purge Alarms + Task Operations](#purge-alarms--task-operations-m-11-m-12-m-13) | No (additive) |
| M-12 | `GET /taskOperations`, `GET /taskOperations/{id}` | 1.2.0 | Not implemented | New endpoints backing async purge. See [Purge Alarms + Task Operations](#purge-alarms--task-operations-m-11-m-12-m-13) | No (additive) |
| M-13 | Purge suppression (no resurrection of acked-active alarms) | impl | N/A | New DB state + sync-path guard. See [Purge Alarms + Task Operations](#purge-alarms--task-operations-m-11-m-12-m-13) | No |
| M-14 | `probableCauseId` populated on AlarmEventRecord | 1.0.0 | Unpopulated | Alert-name mapping table → `indeterminate` fallback; also builds dictionary `probableCauses`. See [probableCauseId Population](#probablecauseid-population-m-14) | No (additive) |

#### Missing Endpoints

| Resource | URI | Method | Since | Action |
|----------|-----|--------|-------|--------|
| Alarm Subscription Update | `/alarmSubscriptions/{id}` | PATCH | 2.2.0 (v13) | New method + `AlarmSubscriptionUpdate` body |
| Purge Alarms Task | `/alarms/purge` | POST | 1.2.0 | New async endpoint: `202` + `Location`, `409` on conflict. All other methods `405`. |
| Task Operation List | `/taskOperations` | GET | 1.2.0 | New endpoint: `200` `TaskOperationInfo[]`. All other methods `405`. |
| Task Operation Occurrence | `/taskOperations/{operationOccurrenceId}` | GET | 1.2.0 | New endpoint: `200` `TaskOperationInfo`. All other methods `405`. |

#### AlarmEventRecord

> **Spec reference (Table 3.3.6.2.2-1):** `objectClass` is type String,
> mandatory, cardinality 1. Described as: "The fully qualified name of the
> class of the object being referenced by the objectId attribute." The value
> is **free-form** — no controlled vocabulary or enumeration. Examples from
> the spec: `oran.o2ims.cluster.ClusterResource`,
> `oran.o2ims.cluster.NodeCluster`.

| Field | Change | Since |
|-------|--------|-------|
| `objectClass` | Add new mandatory field. Free-form string derived from resource type via infrastructure clients. | 2.0.0 |
| `objectTypeId` | Rename JSON key from `resourceTypeID` to `objectTypeId` | 2.0.0 |
| `objectId` | Rename JSON key from `resourceID` to `objectId` | 2.0.0 |
| `alarmDefinitionId` | Fix casing: `alarmDefinitionID` → `alarmDefinitionId` | 2.0.0 |
| `probableCauseId` | Fix casing: `probableCauseID` → `probableCauseId` | 2.0.0 |
| `alarmAcknowledgeTime` | Fix name: `alarmAcknowledgedTime` → `alarmAcknowledgeTime` | 2.0.0 |

#### AlarmSubscriptionInfo

> **`filter` is removed in v2, not deprecated:**
>
> The spec marks `filter` as deprecated (NOTE on Table 3.3.6.2.3-1) and
> introduces `eventFilter` as a typed enum replacement. Our current `filter`
> implementation already uses a string enum `[NEW, CHANGE, CLEAR, ACKNOWLEDGE]`
> — we accidentally implemented `eventFilter` semantics under the `filter`
> name, and never implemented the ETSI attribute-based filtering that `filter`
> was originally intended for. Since there is nothing to preserve and the
> v1 → v2 bump is already breaking, `filter` is removed entirely in v2.
> In v1 (PR 2), `eventFilter` is added alongside `filter` to keep the
> change non-breaking; in v2 (PR 3), `filter` is removed.

| Field | Change | Since |
|-------|--------|-------|
| `eventFilter` | Add new typed enum field (NEW, CHANGE, CLEAR, ACKNOWLEDGE). In v1 (PR 2): added alongside `filter`. In v2 (PR 3): `filter` removed entirely. | 2.2.0 (v13) |
| `consumerSubscriptionId` | Change type from UUID to String (spec allows arbitrary strings) | Inherited |

#### AlarmSubscriptionUpdate (new type)

> **Spec reference (Section 3.3.4.5.3.4, Table 3.3.4.5.3.4-2):** The PATCH
> method uses `Content-Type: application/merge-patch+json` (IETF RFC 7396).
> Fields not included in the request remain unmodified.
>
> | Response Code | Body | Description |
> |---------------|------|-------------|
> | 200 OK | AlarmSubscriptionInfo | Request accepted and completed. Response contains the updated subscription. |
> | 412 Precondition Failed | ProblemDetails | ETag mismatch — resource was modified by another entity. |
> | 4xx/5xx | ProblemDetails | Common error codes per ETSI GS NFV-SOL 013 clause 6.4. |

| Field | Type | Card. | Since |
|-------|------|-------|-------|
| `consumerSubscriptionId` | String | 0..1 | 2.2.0 (v13) |
| `eventFilter` | EventFilter | 0..1 | 2.2.0 (v13) |
| `callback` | Uri | 0..1 | 2.2.0 (v13) |

> **Implementation note:** The spec lists 412 Precondition Failed for ETag
> mismatch. The current codebase does not use ETags for any endpoint.
> ETag generation (`If-Match` / `If-None-Match` handling) and atomic update
> semantics should be addressed during PR 2 implementation. If deferred,
> document that conditional updates are not yet supported and omit 412 from
> the initial OpenAPI spec.

#### Alarm Change Notification

> **Spec reference (Table 3.3.5.1.2-1):** Complete v13 notification type
> sent via POST to the subscriber's callback URL:
>
> | Attribute | Type | P | Card. | Description |
> |-----------|------|---|-------|-------------|
> | `globalCloudId` | Identifier | M | 1 | Global cloud ID assigned by SMO |
> | `consumerSubscriptionId` | String | M | 0..1 | Consumer-provided subscription ID |
> | `notificationEventType` | Integer | M | 1 | 0=NEW, 1=CHANGE, 2=CLEAR, 3=ACKNOWLEDGE |
> | `objectRef` | String | M | 0..1 | URL to the AlarmEventRecord |
> | `alarmEventRecordId` | Identifier | M | 1 | Alarm record ID |
> | `objectTypeId` | Identifier | M | 1 | Type of object causing alarm |
> | `objectId` | Identifier | M | 1 | Instance of object causing alarm |
> | `objectClass` | String | M | 1 | Fully qualified class name (e.g., `oran.o2ims.cluster.ClusterResource`) |
> | `alarmDefinitionId` | Identifier | M | 1 | Alarm definition reference |
> | `probableCauseId` | Identifier | M | 1 | Probable cause reference |
> | `alarmRaisedTime` | DateTime | M | 1 | When raised |
> | `alarmChangedTime` | DateTime | M | 0..1 | When changed |
> | `alarmClearedTime` | DateTime | M | 0..1 | When cleared |
> | `alarmAcknowledgeTime` | DateTime | M | 0..1 | When acknowledged |
> | `alarmAcknowledged` | Boolean | M | 1 | Whether acknowledged |
> | `perceivedSeverity` | Integer | M | 1 | 0=CRITICAL, 1=MAJOR, 2=MINOR, 3=WARNING, 4=INDETERMINATE, 5=CLEARED |
> | `extensions` | KeyValue | M | 0..N | Vendor/operator extension properties |

> **Implementation note:** Several fields are marked `P=M` (mandatory) with
> cardinality `0..1`. This is standard O-RAN convention: `P=M` means the JSON
> key must always be present in the serialized response; cardinality `0..1`
> means the value may be null (e.g., `alarmClearedTime` is null until the
> alarm clears). Serialize these fields as `null` when the value is absent,
> not by omitting the key.

Changes required from our current implementation:

| Field | Change | Since |
|-------|--------|-------|
| `objectClass` | Add new mandatory field | 2.0.0 |
| `objectRef` | Add URL to AlarmEventRecord | 2.0.0 |
| `consumerSubscriptionId` | Include from subscription record in notification payload | 2.0.0 |
| `objectTypeId` | Rename from `resourceTypeID` | 2.0.0 |
| `objectId` | Rename from `resourceID` | 2.0.0 |
| `alarmDefinitionId` | Fix casing from `alarmDefinitionID` | 2.0.0 |
| `probableCauseId` | Fix casing from `probableCauseID` | 2.0.0 |

#### probableCauseId Population (M-14)

> **Scope:** closes two coupled v13 gaps with one mechanism — the mandatory
> `probableCauses` list on `AlarmDictionary` (§3.2.6.2.8, cardinality 1..N,
> currently absent) and the unpopulated `AlarmEventRecord.probableCauseId`
> (§3.3.6.2.2). The broader standardized *alarm-definition* taxonomy (spec §4)
> stays out of scope. This work is cross-cutting: dictionary generation is on the
> Inventory side (resources/cluster collectors), consumption is on the Monitoring
> side (alarms server). See also Inventory [I-3](#32-gap-assessment).

**Problem.** `AlarmDefinition` (§3.2.6.2.9) has no mandatory probable-cause field;
the dictionary carries `probableCauses` as a separate list, and Prometheus rules
— our only alarm source, authored outside our control — carry no probable-cause
concept. A meaningful probable cause is therefore domain classification we must
own, not something derivable from the alert itself (echoing the alert name is not
a cause).

**Mechanism — embedded alert-name mapping table → guaranteed `indeterminate` fallback:**

1. Maintain a versioned, **embedded** `alertName → probableCause` table (YAML),
   bootstrapped from the published OCP monitoring / kube-prometheus / ACM alert
   catalogs. Key on `alertName` with an optional `(alertName, severity)`
   qualifier. Values map to the ITU-T X.733 / 3GPP TS 28.111 [42] probable-cause
   enumeration and carry `standardReference` accordingly. The table carries its
   own **`mappingVersion`**. The embedded default is authoritative; an
   operator-supplied ConfigMap may *extend* it (union by alert-name key; embedded
   entries win on conflict) but is not a dependency.
2. At dictionary-generation time (resources + cluster collectors), build each
   dictionary's `probableCauses` list from the distinct mapped causes **plus the
   reserved `indeterminate` cause, which is always included** so that every
   `probableCauseId` a record can emit — the fallback included — resolves within
   the same dictionary. Record each definition's probable-cause code in its
   `alarmAdditionalFields`.
3. **The fallback is total.** Any alarm that cannot be resolved to a mapped cause
   is assigned the reserved `indeterminate` cause. This covers *every* miss — an
   unmapped alert name, a missing/absent `ObjectTypeID`, and a dictionary
   fetch/lookup failure — not just unmapped names. Echoing the alert name is never
   used.
4. The alarms server, which already resolves `alarmDefinitionId` from the fetched
   dictionary (`resourceserver.go` / `clusterserver.go` / `converter.go`), reads
   the associated probable-cause id in the same lookup and sets `probableCauseId`
   on the record. **`converter.go` must change:** today it performs the lookup
   only when `ObjectTypeID` is present, logs failures, and still appends a record
   with a nil `ProbableCauseID` (serialized by the API as a zero UUID). Under this
   design it instead assigns the `indeterminate` id on any such miss, so a
   persisted record's `probableCauseId` is **never nil/zero**.

**`probableCauseId` identity.** `ProbableCause` is keyed by `probableCauseCode`
(String), but the record column is `probable_cause_id uuid`. Resolve the mismatch
by deriving a **deterministic UUIDv5** and exposing the *same* value at both ends:

- **Namespace:** a fixed, committed application namespace UUID constant,
  `oranProbableCauseNamespace` (single documented value in code).
- **Canonical input:** the UTF-8 string `"{alarmDictionaryId}:{probableCauseCode}"`
  — `alarmDictionaryId` in canonical lowercase UUID form, a literal `:` separator,
  and `probableCauseCode` verbatim.
- **Exposure at both ends:** the `ProbableCause` entry in the dictionary response
  carries this derived UUID alongside its `probableCauseCode`, and
  `AlarmEventRecord.probableCauseId` holds the identical value — so a consumer can
  resolve a record's cause back to a dictionary entry. (The earlier reference to
  M-10 is dropped; M-10 governs a different field and does not define this
  algorithm.)

**Dictionary identity and history.** Because the `probableCauses` set is derived
from the mapping table, a mapping change must not silently alter a dictionary that
keeps the same identity. The table's `mappingVersion` is therefore folded into
`AlarmDictionaryVersion` (and thus into the inputs for `alarmDictionaryId`), so
any mapping change — embedded or ConfigMap-extended — yields a **new dictionary
version**. Historical alarm records continue to reference the `probableCauseId` of
the dictionary version under which they were raised, so previously emitted causes
remain resolvable rather than dangling.

**Coverage observability.** Alert names are renamed/added across OCP/ACM releases,
so table entries silently rot to `indeterminate`. Log — and expose a count metric
for — every alarm that falls through to `indeterminate` (for any of the step-3
reasons), turning coverage gaps into an actionable signal.

**Deferred (optional tier).** An `expr`/metric-based classifier, keyed off the
`Expr` already stored in `alarmAdditionalFields`, could fill the unmapped tail
with better-than-`indeterminate` causes. Deferred as an optional tier between the
table and the fallback.

**Compliance note.** The spec mandates the field be *present*, not *meaningful*.
Because the `indeterminate` cause is always in the dictionary, is assigned on
every lookup miss, has a well-defined resolvable id, and is versioned with the
dictionary, `probableCauseId` is **guaranteed non-null and resolvable** from day
one; meaningfulness is bounded by — and growable through — table curation.

#### Purge Alarms + Task Operations (M-11, M-12, M-13)

> **Scope note:** Purge and task operations were removed in doc v2.0 and are
> re-introduced here per re-scoping. They are O2IMS API v1.2.0 features and are
> **mandatory** in the v13 monitoring resource table (§3.3.3). They are delivered
> together because purge is an *asynchronous task*: `POST /alarms/purge` returns
> `202 Accepted` with a `Location` header pointing at a Task Operation Occurrence
> that the consumer then polls via `GET /taskOperations/{id}`.

**Data types (spec §3.3.6.2.6–7):**

- `PurgeRequest`: `alarmRecordId: string[]` (0..N) and/or `purgeConditions
  {startWindowTime?, endWindowTime}`, where both window bounds are measured
  against **`alarmClearedTime`**. At least one of `alarmRecordId` /
  `purgeConditions` must be present; when both are given, apply the **intersection**
  (purge conditions scoped to the listed records).
- `TaskOperationInfo`: `id`, `operationState {PROCESSING | COMPLETED | FAILED |
  ROLLED_BACK}`, `stateEnteredTime`, `startTime`, `operation {PURGE}`,
  `operationParams` (the originating `PurgeRequest`), `error` (ProblemDetails;
  present when `operationState == FAILED`).

**Eligibility — authoritative source: O-RAN.WG6.ORCH-USE-CASES R003-v10.00,
clause 3.7.13** ([download id=672](https://specifications.o-ran.org/download?id=672)):

> "Only inactive alarms or acknowledged active alarms can be purged."

Mapped to our schema:

- **Eligible:** `alarm_status = 'resolved'` **OR** (`alarm_status = 'firing'` AND
  `alarm_acknowledged = true`)
- **Never eligible:** `alarm_status = 'firing'` AND `alarm_acknowledged = false`
- A request naming only non-existent/ineligible records is a **no-op success**
  (3.7.13 "no alarms matching the request" exception), not an error.

**Resurrection problem and suppression (M-13).**
`alarm_event_record` is a projection continuously reconciled from Alertmanager
(`UpsertAlarmEventCaaSRecord` / `ResolveStaleAlarmEventCaaSRecord`), with upsert
conflict key `(fingerprint, alarm_raised_time)`. Hard-deleting an alarm whose
underlying alert is **still firing** (the *acknowledged-active* case) removes the
conflict target, so the next sync re-INSERTs it as a brand-new `NEW` alarm,
losing the acknowledgement and emitting a spurious notification. To honor 3.7.13
without resurrection, purge is split by state:

- **Inactive (resolved) alarms → hard `DELETE`.** Identical to the existing
  retention cleanup (`DeleteResolvedAlarmEventsBefore`), which already deletes
  resolved rows safely: the upstream alert is gone, so a later re-fire is a
  genuinely new alarm (new `alarm_raised_time`). This frees DB storage, matching
  the capacity motivation in 3.7.13 NOTE 1.
- **Acknowledged-active (firing) alarms → soft-purge (the suppression).** Set a
  new `purged` flag instead of deleting. The persisted row *is* the suppression:
  because the row still exists, the next sync's upsert takes the `ON CONFLICT
  DoUpdate` branch (not INSERT), so no spurious `NEW` is emitted and the
  acknowledgement is preserved. The row is excluded from the Alarm List
  (`GET /alarms`; `GET /alarms/{id}` → `404`), its outbox notifications are
  suppressed (`manage_alarm_event` BEFORE trigger: `IF NEW.purged THEN
  should_create_data_change_event := FALSE`), and it is reclaimed by normal
  retention once the alert finally clears upstream.

> **Alternative considered — tombstone table.** A separate
> `alarm_purge_tombstone (fingerprint, alarm_raised_time)` with a `NOT EXISTS`
> guard on the upsert insert and TTL-based reaping. Rejected for the initial
> implementation: it adds hot-path complexity and a TTL heuristic, and a live
> firing alert cannot have its storage reclaimed anyway (it would be resent), so
> soft-purge is both simpler and more honest about what is reclaimable.

**Async task model (spec §3.3.4.7–3.3.4.9):**

1. `PurgeAlarms` handler validates the body (oneOf present; `endWindowTime`
   required when `purgeConditions` is set), atomically creates a `task_operation`
   row in state `PROCESSING`, starts a background worker, and returns `202` with
   `Location: {baseURL}/taskOperations/{id}`.
2. **Worker** runs under `AlarmsServer.Wg` (graceful-shutdown drain) holding a
   **session-scoped advisory lock** `pg_advisory_lock(hash(task_id))` so only one
   replica executes a given task; on success → `COMPLETED`, on error → `FAILED`
   with populated `error`.
3. **Concurrency / 409:** a `UNIQUE` partial index on `(operation) WHERE
   operation_state = 'PROCESSING'` guarantees at most one active purge; a
   concurrent accept hits a unique violation → `409 Conflict` ("another task
   operation is ongoing on the affected resources", spec §3.3.4.7.3.1).
4. **Restart recovery:** on startup — and periodically via the retention ticker —
   scan `PROCESSING` tasks and, for each, `pg_try_advisory_lock(hash(task_id))`.
   If acquired, no replica is running it, so **resume** it (purge is idempotent:
   re-deleting/re-flagging by the same criteria is a no-op) then mark
   `COMPLETED`; if the lock is held, a peer owns it, so skip. This avoids marking
   a task `FAILED` while another replica is still running it.
5. **Terminal-state reaping:** remove `task_operation` rows some time after they
   reach a terminal state (permitted by spec §3.3.4.9.1), folded into the
   retention ticker.

**Field selection on `GET /taskOperations` (spec §3.3.4.8.3.2).**
The list endpoint mirrors `GET /alarms` exactly: it reuses the shared
`allFields` / `fields` / `excludeFields` / `filter` query parameters
(`../../common/api/openapi.yaml#/components/parameters/*`) and validates them
with `commonapi.NewFieldOptions(...)` against `TaskOperationInfo{}`. The complex
attribute subject to selection is `operationParams` (the embedded `PurgeRequest`).
Consistent with the current `GET /alarms` implementation, **paging**
(`nextpage_opaque_marker`) and **`exclude_default`** are not implemented — the
spec marks them "should"/conditional, and no existing monitoring list endpoint
supports them. `GET /taskOperations/{id}` returns the full object, including
`error` when the state is `FAILED` (per §3.3.6.2.7, `error` is always present on
individual reads).

**RBAC (DD-004).**
The monitoring `reader-role` already grants `get` on
`/O2ims_infrastructureMonitoring/*` (a wildcard, unlike the enumerated inventory
paths), so both **`GET /taskOperations`** and **`GET /taskOperations/{id}`** are
already covered — no change required, though they may be enumerated for clarity.
**`POST /alarms/purge`** is *not* covered today: `reader-role`'s wildcard is
`get`-only and `maintainer-role`'s `/alarms/*` rule grants only `patch`. HTTP
`POST` maps to the `create` verb (as `CreateSubscription` does), so a new,
path-specific rule must be added to **`maintainer-role`** (the role that already
owns alarm mutations via `PatchAlarm`):

```yaml
# config/rbac/oran_o2ims_user_roles.yaml — maintainer-role
- nonResourceURLs:
  - /O2ims_infrastructureMonitoring/v1/alarms/purge
  verbs:
  - create
```

`admin-role` inherits this through role aggregation. No change is needed in
`oran_o2ims_oauth_role_bindings.yaml` (maintainer-role is already bound). In
`openapi.yaml`, annotate `security` as `role:o2ims-admin` + `role:o2ims-maintainer`
for purge (matching `PatchAlarm`), and `role:o2ims-admin` + `role:o2ims-reader`
for the task-operation GETs (matching `GetAlarms`).

#### DB Schema additions: purge + task operations (M-11, M-12, M-13)

**`alarm_event_record`** — add soft-purge state (modify the existing
`000002_create_alarm_event_record_table` baseline in place, per AGENTS.md):

| Column | Type | Nullable | Change |
|--------|------|----------|--------|
| `purged` | BOOLEAN NOT NULL DEFAULT FALSE | NO | **Add** — soft-purge flag for acknowledged-active alarms; excluded from Alarm List and from outbox |
| `purged_at` | TIMESTAMPTZ | YES | **Add** — when the row was soft-purged (observability) |

Associated changes: `GET /alarms` and `GET /alarms/{id}` filter `purged = false`;
the upsert `DoUpdate` SET list must **not** reset `purged`; the
`manage_alarm_event` BEFORE trigger suppresses the outbox when `NEW.purged`.

**`task_operation`** — new baseline file `000005_create_task_operation_table`
(a new table is its own baseline, not an incremental migration):

```sql
CREATE TABLE IF NOT EXISTS task_operation (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation          VARCHAR(20) NOT NULL CHECK (operation IN ('PURGE')),
    operation_state    VARCHAR(20) NOT NULL DEFAULT 'PROCESSING'
                         CHECK (operation_state IN ('PROCESSING','COMPLETED','FAILED','ROLLED_BACK')),
    operation_params   JSONB NOT NULL,       -- the originating PurgeRequest
    error              JSONB,                -- ProblemDetails when FAILED
    start_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    state_entered_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- At most one active purge at a time (atomic 409 on concurrent accept)
CREATE UNIQUE INDEX one_active_purge
    ON task_operation ((operation)) WHERE operation_state = 'PROCESSING';
```

#### DB Schema: alarm_event_record (M-1, M-9)

Add `object_class` column. Existing columns for reference:

| Column | Type | Nullable | Change |
|--------|------|----------|--------|
| `object_class` | VARCHAR | NO | **Add** — fully qualified class name |

Existing columns `resource_type_id`, `resource_id`, `alarm_definition_id`,
`probable_cause_id` remain unchanged in the DB — the JSON key renames (M-9)
are handled in the OpenAPI spec and serialization layer, not the schema.

#### DB Schema: alarm_subscription_info (M-2, M-10)

| Column | Type | Change |
|--------|------|--------|
| `event_filter` | VARCHAR | **Add** — enum (NEW, CHANGE, CLEAR, ACKNOWLEDGE) |
| `consumer_subscription_id` | UUID → TEXT | **Change column type** from UUID to TEXT in migration. Update Go model (`*uuid.UUID` → `*string`) and OpenAPI schema (remove `format: uuid`). |

#### Version Management (M-7)

> **Spec reference (Section 3.1.8, Table 3.1.6.1.6-1):** The API producer
> SHALL support dedicated URIs for version information. The
> `ApiVersionInformation` type contains:
>
> | Attribute | Type | Card. | Description |
> |-----------|------|-------|-------------|
> | `uriPrefix` | String | 1 | URI prefix in the form `{apiRoot}/{apiName}/{apiMajorVersion}/` |
> | `apiVersions` | Structure | 1..N | Supported versions for this API |
> | `>version` | String | 1 | Version identifier (e.g., `2.2.0`) |
> | `>isDeprecated` | Boolean | 0..1 | Whether the version is deprecated |
> | `>retirementDate` | DateTime | 0..1 | When the deprecated version will be removed (required if isDeprecated=true) |
>
> A deprecated version is still supported but recommended not to be used.
> When a version is no longer supported, it does not appear in the response.
> The `Version` HTTP header field conveys the API version in responses.

> **Implementation note:** The existing alarms service already returns
> `apiVersions` and `uriPrefix` from the discovery URIs. The `Version`
> response header must also be added to all v2 responses. Verify both
> the discovery endpoints and the header in PR 3.

#### Impacted Files

| File | Change | Req |
|------|--------|-----|
| `internal/service/alarms/api/openapi.yaml` | Add `objectClass`, `eventFilter`, PATCH method, v2 paths, field renames | [M-1](#52-gap-assessment), [M-2](#52-gap-assessment), [M-3](#52-gap-assessment), [M-7](#52-gap-assessment), [M-9](#52-gap-assessment) |
| `internal/service/alarms/api/generated/alarms.generated.go` | Regenerated from OpenAPI | All |
| `internal/service/alarms/api/server.go` | New handler: PatchSubscription; update field handling | [M-1](#52-gap-assessment), [M-3](#52-gap-assessment) |
| `internal/service/alarms/internal/db/migrations/000002_*` | Add `object_class` column | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/db/migrations/000003_*` | Add `event_filter` column | [M-2](#52-gap-assessment) |
| `internal/service/alarms/internal/db/models/alarm_event_record.go` | Add ObjectClass field | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/db/models/alarm_subscription.go` | Add EventFilter field | [M-2](#52-gap-assessment) |
| `internal/service/alarms/internal/db/models/converters.go` | Update converters for new fields and v2 naming | [M-1](#52-gap-assessment), [M-9](#52-gap-assessment) |
| `internal/service/alarms/internal/db/repo/` | Subscription update and queries | [M-2](#52-gap-assessment), [M-3](#52-gap-assessment) |
| `internal/alertmanager/converter.go` | Populate objectClass when creating records | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/infrastructure/infrastructure.go` | Expose objectClass in client interface | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/infrastructure/clusterserver.go` | Derive objectClass from resource type | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/infrastructure/resourceserver.go` | Derive objectClass from resource type | [M-1](#52-gap-assessment) |
| `internal/service/alarms/internal/notifier_provider/` | Add objectClass, objectRef, consumerSubscriptionId to notification payload | [M-4](#52-gap-assessment), [M-5](#52-gap-assessment), [M-6](#52-gap-assessment) |
| `config/rbac/oran_o2ims_user_roles.yaml` | Update monitoring endpoint paths for v2 | [M-7](#52-gap-assessment) |
| `config/rbac/oran_o2ims_oauth_role_bindings.yaml` | Update monitoring bindings for v2 | [M-7](#52-gap-assessment) |
| `internal/service/alarms/cmd/serve.go` | Update base path if hardcoded | [M-7](#52-gap-assessment) |
| `internal/service/alarms/api/openapi.yaml` | Add `/alarms/purge`, `/taskOperations[/{id}]` paths; `PurgeRequest`, `TaskOperationInfo` schemas; `405` for unsupported methods | [M-11](#52-gap-assessment), [M-12](#52-gap-assessment) |
| `internal/service/alarms/internal/db/migrations/000002_*` | Add `purged`, `purged_at` columns; suppress outbox when purged in `manage_alarm_event` | [M-13](#52-gap-assessment) |
| `internal/service/alarms/internal/db/migrations/000005_create_task_operation_table*` | New baseline table + `one_active_purge` partial unique index | [M-11](#52-gap-assessment), [M-12](#52-gap-assessment) |
| `internal/service/alarms/internal/db/models/` | New `TaskOperation` model; `Purged`/`PurgedAt` on `AlarmEventRecord` | [M-11](#52-gap-assessment), [M-13](#52-gap-assessment) |
| `internal/service/alarms/internal/db/repo/` | `PurgeAlarmEventRecords` (hybrid delete/soft-purge, eligibility predicate); task CRUD + state transitions; regenerate mock | [M-11](#52-gap-assessment), [M-12](#52-gap-assessment), [M-13](#52-gap-assessment) |
| `internal/service/alarms/internal/db/repo/alarms_repository.go` | Exclude `purged` from `GetAlarmEventRecords`/`GetAlarmEventRecord`; do not reset `purged` in upsert `DoUpdate` | [M-13](#52-gap-assessment) |
| `internal/service/alarms/api/server.go` | New handlers: `PurgeAlarms`, `GetTaskOperations`, `GetTaskOperation`; async worker + advisory lock | [M-11](#52-gap-assessment), [M-12](#52-gap-assessment) |
| `internal/service/alarms/serve.go` | Startup restart-recovery + terminal-state reaping of task operations | [M-11](#52-gap-assessment) |
| `config/rbac/oran_o2ims_user_roles.yaml` | Add `create` on `/alarms/purge` to maintainer-role; task GETs already covered by reader wildcard (DD-004) | [M-11](#52-gap-assessment) |
| `internal/service/{resources,cluster}/collector/collector_alarms.go` | Build dictionary `probableCauses` (always incl. `indeterminate`) + associate codes to definitions; fold mapping `mappingVersion` into `AlarmDictionaryVersion` | [M-14](#52-gap-assessment) |
| `internal/service/{resources,cluster}/.../alarm_probable_cause_map.yaml` (new, embedded) | Versioned `alertName → probableCause` table bootstrapped from OCP/ACM catalogs + loader | [M-14](#52-gap-assessment) |
| `internal/service/{resources,cluster}/db/models/` + migrations | Persist probable causes (code + derived UUIDv5) + definition association | [M-14](#52-gap-assessment) |
| `internal/service/{resources,cluster}/api/openapi.yaml` | Expose `probableCauses` in the `alarmDictionaries` response | [I-3](#32-gap-assessment), [M-14](#52-gap-assessment) |
| `internal/service/alarms/internal/infrastructure/{resourceserver,clusterserver}.go` | Carry probable-cause id through the existing dictionary lookup | [M-14](#52-gap-assessment) |
| `internal/service/alarms/internal/alertmanager/converter.go` | Set `probableCauseId` on the AlarmEventRecord; assign `indeterminate` on any lookup miss (incl. missing `objectTypeId`) so it is never nil/zero | [M-14](#52-gap-assessment) |

---

## 6. Phased Implementation

### Overview

This feature is implemented in three phases to minimize disruption. Each phase
is delivered as a single, self-contained pull request containing only the logic
for that phase (3 PRs in total).

No v13 changes are **GitOps-breaking** — there are no new CRDs or user-facing
configuration changes. All breaking changes are **REST API consumer-facing**
(SMO-side), affecting the monitoring interface URL path.

### PR Summary

| PR | Title | Type | Interface |
|----|-------|------|-----------|
| PR 1 | Inventory + Provisioning spec alignment | Non-breaking | Inventory, Provisioning |
| PR 2 | Monitoring non-breaking additions | Non-breaking | Monitoring |
| PR 3 | Monitoring v2 breaking changes | Breaking (API consumers) | Monitoring |
| PR 4 | Purge Alarms + Task Operations | Non-breaking (additive endpoints) | Monitoring |

### PR 1: Inventory + Provisioning Spec Alignment

| Item | Change |
|------|--------|
| OpenAPI spec info blocks | Update to reference v13 in resources and provisioning specs |
| Deprecated-field deviation | Document in inventory docs that legacy 1.x fields are absent |
| User-guide docs | Update `inventory-api.md` and `cluster-provisioning.md` |

### PR 2: Monitoring Non-Breaking Additions

Add new fields and endpoints while keeping the v1 API path:

| Item | Change |
|------|--------|
| `objectClass` on AlarmEventRecord | Add as optional initially (verify population logic before making mandatory) |
| `eventFilter` on AlarmSubscriptionInfo | Add `eventFilter` alongside existing `filter` (non-breaking) |
| `consumerSubscriptionId` type | Change from UUID to String |
| PATCH `/alarmSubscriptions/{id}` | New method + `AlarmSubscriptionUpdate` type |
| Alarm Change Notification | Add `objectRef`, `objectClass`, `consumerSubscriptionId` |
| `probableCauseId` population | Build dictionary `probableCauses` from an embedded alert-name mapping table (→ `indeterminate` fallback); populate record `probableCauseId` (M-14, I-3) |
| DB migrations | New columns (`object_class`, `event_filter`) |
| Test coverage | Unit, envtest, server tests for all new endpoints and fields |

### PR 3: Monitoring v2 Breaking Changes

| Item | Change |
|------|--------|
| API URL path | Bump from `/v1/` to `/v2/` |
| `objectClass` | Make mandatory on AlarmEventRecord |
| `eventFilter` | Make required in v2. Remove `filter` entirely. |
| `filter` removal | Drop `filter` field from schema, model, and DB. |
| RBAC roles | Update monitoring endpoint paths in `oran_o2ims_user_roles.yaml` |
| OAuth bindings | Update monitoring bindings in `oran_o2ims_oauth_role_bindings.yaml` |
| OpenAPI spec version | Update to reference v13 |
| Docs | Update monitoring-related documentation |

### PR 4: Purge Alarms + Task Operations

Self-contained PR adding the async purge capability and its backing task
resources under the current (v2) monitoring path:

| Item | Change |
|------|--------|
| OpenAPI | Add `/alarms/purge` (POST), `/taskOperations` (GET), `/taskOperations/{id}` (GET); `PurgeRequest` + `TaskOperationInfo` schemas; `405` for all other methods |
| DB migrations | `purged`/`purged_at` on `alarm_event_record` (000002 baseline); new `task_operation` table + `one_active_purge` index (000005) |
| Repo | `PurgeAlarmEventRecords` (hybrid hard-delete inactive / soft-purge acked-active; eligibility predicate per 3.7.13); task CRUD + state transitions |
| Server | `PurgeAlarms` (202 + Location, 409 conflict, validation), `GetTaskOperations`, `GetTaskOperation`; background worker under advisory lock |
| Sync path | Exclude `purged` rows from Alarm List; preserve `purged` across upsert; suppress outbox for purged rows |
| Lifecycle | Startup restart-recovery (resume orphaned PROCESSING tasks) + terminal-state reaping, folded into the retention ticker |
| RBAC | Add `alarms/purge` + `taskOperations` paths; restrict purge beyond reader-role |
| Tests | Repo (eligibility, hard vs soft purge, no-resurrection, advisory-lock single-runner), server (202/409/validation/404), worker (COMPLETED/FAILED), restart recovery |

### Ordering Rationale

| Order | Reason |
|-------|--------|
| Inventory/Provisioning first | Trivial changes; clears the backlog early |
| Monitoring non-breaking before breaking | New fields/endpoints available before the URL path changes. `objectClass` verified before making it mandatory. `eventFilter` added alongside `filter` in v1; `filter` removed in PR 3 with the v2 bump. |
| Breaking changes last | Clean cutover after all features are in place |
| Purge/tasks (PR 4) after the v2 bump | Additive endpoints land cleanly on the stabilized v2 path; the suppression touches the Alertmanager sync path, so isolating it in its own PR limits blast radius. Could ship earlier under v1 if prioritized — the endpoints are v1.2.0 features. |

---

## 7. Verification Plan

### Per-PR Build Verification

| Check | Command | When to Run |
|-------|---------|-------------|
| Code generation | `make generate && make manifests && make bundle` | After API/CRD changes |
| Full CI pipeline | `make ci-job` | Every PR (format, vet, lint, test, envtest, coverage, bundle-check) |
| End-to-end tests | `make test-e2e` | Changes touching alarm flows or controller logic |
| Go lint | `make golangci-lint` | Go file changes |
| YAML lint | `make yamllint` | OpenAPI spec changes |
| Markdown lint | `make markdownlint` | Documentation changes |

### Functional Verification (Monitoring)

| Test | Description |
|------|-------------|
| PATCH subscription | Update callback, eventFilter, consumerSubscriptionId via PATCH |
| objectClass in records | Verify objectClass appears in alarm event records after creation |
| objectClass in notifications | Verify objectClass in Alarm Change Notification payload |
| eventFilter subscription | Create subscription with eventFilter, verify filtering works |
| filter removed in v2 | Verify `filter` field is absent from v2 schema and rejected by the server |
| consumerSubscriptionId | Verify non-UUID strings are accepted |
| v2 URL path | All endpoints accessible under `/v2/` after bump |
| v2 field names | Verify JSON keys use v13 names (objectTypeId, objectId, etc.) |
| Version header | Verify `Version` response header is present on v2 responses |
| Version discovery | Verify `apiVersions` and `uriPrefix` in discovery endpoints reflect v2 after the URL bump |
| Existing flows | Alarm create, acknowledge, clear unaffected by changes |
| RBAC | Verify monitoring roles grant access to v2 paths |

---

## Appendix A: Spec Reference Mapping

| Spec Section | Content | Relevance |
|--------------|---------|-----------|
| 3.1.7 | Security (OAuth scope verification) | RBAC changes when paths move to v2 (M-7) |
| 3.1.8 | Version management | v1/v2 coexistence and API version discovery (M-7) |
| 3.2 | Inventory API definition | Endpoints, data model, notifications |
| 3.2.2 (Table 3.2.2-1) | Inventory API version history | v1.0.0 → v2.0.0 changes |
| 3.2.3 (Table 3.2.3-1) | Inventory REST resources and methods | Mandatory endpoints including performance dictionaries (I-2, out-of-scope note) |
| 3.2.6.2.3 | ResourcePoolInfo type | Deprecated fields: oCloudId, globalLocationId (I-2) |
| 3.3 | Monitoring API definition | Endpoints, data model, notifications |
| 3.3.2 (Table 3.3.2-1) | Monitoring API version history | v1.0.0 → v2.2.0 changes |
| 3.3.3 (Table 3.3.3-1) | Monitoring REST resources and methods | Mandatory endpoints table (M-3) |
| 3.3.4.5 | Alarm Subscription Description | PATCH method (M-3, p106-107) |
| 3.3.6.2.8 | AlarmSubscriptionUpdate type | PATCH body (M-3) |
| 3.3.5 | Alarm Change Notification | objectClass, objectRef, consumerSubscriptionId in notification (M-3, M-5, M-6) |
| 3.3.6.2.2 | AlarmEventRecord type | objectClass field (M-1) |
| 3.3.6.2.3 | AlarmSubscriptionInfo type | eventFilter field (M-2) |
| 3.3.6.3.3.1 | EventFilter enumeration | NEW, CHANGE, CLEAR, ACKNOWLEDGE (M-2) |
| 2.1.3.1.2.5 | Infrastructure Alarm Purge (service model) | Purge operation flow (M-11) |
| 3.3.4.7 | Purge Alarms Task resource | POST `/alarms/purge`, 202 + Location, 409 (M-11) |
| 3.3.4.8 / 3.3.4.9 | Task Operation List / Occurrence | GET `/taskOperations[/{id}]` (M-12) |
| 3.3.6.2.6 | PurgeRequest type | Purge body: alarmRecordId / purgeConditions on alarmClearedTime (M-11) |
| 3.3.6.2.7 | TaskOperationInfo type | Task occurrence representation + operationState (M-12) |
| ORCH-USE-CASES 3.7.13 | Alarm Purge Use Case (id=672, R003-v10.00) | Eligibility: inactive OR acknowledged-active only (M-11, M-13) |
| 3.2.6.2.8 | AlarmDictionary type | Mandatory `probableCauses` list (I-3, M-14) |
| 3.2.6.2.10 | ProbableCause type | probableCauseCode / description / standardReference (M-14) |
| 3.3.6.2.2 | AlarmEventRecord type | `probableCauseId` field population (M-14) |
| [42] 3GPP TS 28.111 / ITU-T X.733 | Fault Management / probable-cause taxonomy | Source enumeration for mapping-table values (M-14) |
| 3.4 | Provisioning API definition | Endpoints, data model |
| 3.4.2 (Table 3.4.2-1) | Provisioning API version history | v1.0.0 → v1.2.0 changes |
| 4 | O-Cloud Alarms dictionary | Out of scope for this feature |
| Annex (Change History) | CR list v11→v13 | Traceability |

---

## Appendix B: Document Version History

| Version | Item | Comment |
|---------|------|---------|
| 1.0 | Initial proposal | Gap analysis across inventory, provisioning, and monitoring interfaces. Included purge/task operations, scope decisions, spec extracts, and impacted files with requirement traceability. |
| 2.0 | Remove purge/tasks | Purge alarms and task operations removed from scope per Brent's decision. |
|     | Close I-2 | Deprecated fields on ResourcePoolInfo: leave absent per team consensus. |
|     | filter vs eventFilter | Added semantic clarification: `filter` is ETSI attribute-based (deprecated), `eventFilter` is typed enum (new). |
| 3.0 | Re-add purge/tasks (PR 4) | Purge Alarms + Task Operations re-scoped in (M-11, M-12, M-13). Eligibility per ORCH-USE-CASES 3.7.13 (inactive OR acknowledged-active). Full compliance via soft-purge suppression to prevent Alertmanager re-sync resurrection; tombstone alternative deferred. |
| 3.1 | Add probableCauseId (M-14, I-3) | Populate `probableCauseId` via an embedded alert-name → probable-cause mapping table (X.733 / 3GPP TS 28.111 values) → `indeterminate` fallback; deterministic UUIDv5 ids; also builds the mandatory dictionary `probableCauses`. expr-classifier deferred. |
| 3.2 | Address CodeRabbit (PR 5196) | Harden M-14: always include `indeterminate` per dictionary; total fallback on any lookup miss incl. missing `objectTypeId`; explicit UUIDv5 contract exposed at both ends; fold mapping version into `AlarmDictionaryVersion` so history stays resolvable. |

---

## Appendix C: PR 4 OpenAPI Draft (Purge + Task Operations)

Draft additions for `internal/service/alarms/api/openapi.yaml`, to be applied
when PR 4 is implemented (followed by `make go-generate`). Conventions match the
existing file: shared parameter `$ref`s, `ProblemDetails` error bodies, and
role-based `security`. The `v1` prefix matches the current file; if PR 4 lands
after the PR 3 `v1`→`v2` bump, these paths become `/v2/`.

**Design notes:**

- `405` for unsupported methods is not declared explicitly — the router returns
  `405` for undefined methods on a defined path, matching the rest of the file.
- `anyOf` is the closest OpenAPI 3.0 expression of "at least one of
  `alarmRecordId`/`purgeConditions`"; intersection semantics and the
  `endWindowTime`-required rule are enforced in the handler.
- `202` carries no body, only the `Location` header (spec §3.3.4.7.3.1).
- `operationParams`/`error` use sibling `$ref` + `description`; if oapi-codegen
  rejects `$ref` siblings, wrap each in `allOf`.

**Paths** (add under `paths:`):

```yaml
  /O2ims_infrastructureMonitoring/v1/alarms/purge:
    post:
      operationId: PurgeAlarms
      summary: Request an asynchronous purge of alarm records
      description: >-
        Purges eligible alarm records from the Alarm List irrespective of the
        retention period. Per O-RAN.WG6.ORCH-USE-CASES clause 3.7.13, only
        inactive (resolved) alarms or acknowledged active alarms are eligible;
        a request naming only ineligible/non-existent records is a no-op success.
        Returns 202 with a Location header to the Task Operation Occurrence to poll.
      security:
      - oauth2:
        - role:o2ims-admin
        - role:o2ims-maintainer
      tags:
      - alarms
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/PurgeRequest'
      responses:
        '202':
          description: Purge request accepted for processing.
          headers:
            Location:
              required: true
              description: URI of the created Task Operation Occurrence resource.
              schema:
                type: string
                format: uri
        '400':
          description: Bad request
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '401':
          description: Unauthorized
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '403':
          description: Forbidden
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '409':
          description: Conflict — another purge task operation is already in progress.
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '500':
          description: Internal server error
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'

  /O2ims_infrastructureMonitoring/v1/taskOperations:
    get:
      operationId: GetTaskOperations
      summary: Retrieve the list of task operation occurrences
      security:
      - oauth2:
        - role:o2ims-admin
        - role:o2ims-reader
      tags:
      - taskOperations
      parameters:
      - $ref: '../../common/api/openapi.yaml#/components/parameters/allFields'
      - $ref: '../../common/api/openapi.yaml#/components/parameters/excludeFields'
      - $ref: '../../common/api/openapi.yaml#/components/parameters/fields'
      - $ref: '../../common/api/openapi.yaml#/components/parameters/filter'
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/TaskOperationInfo'
        '400':
          description: Bad request
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '401':
          description: Unauthorized
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '403':
          description: Forbidden
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '500':
          description: Internal server error
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'

  /O2ims_infrastructureMonitoring/v1/taskOperations/{operationOccurrenceId}:
    get:
      operationId: GetTaskOperation
      summary: Retrieve exactly one task operation occurrence
      security:
      - oauth2:
        - role:o2ims-admin
        - role:o2ims-reader
      tags:
      - taskOperations
      parameters:
      - in: path
        name: operationOccurrenceId
        required: true
        schema:
          type: string
          format: uuid
        example: 78081d85-5736-4215-afab-772461544e60
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/TaskOperationInfo'
        '400':
          description: Bad request
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '401':
          description: Unauthorized
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '403':
          description: Forbidden
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '404':
          description: Task operation occurrence not found
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
        '500':
          description: Internal server error
          content:
            application/problem+json:
              schema:
                $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
```

**Schemas** (add under `components.schemas:`):

```yaml
    PurgeRequest:
      type: object
      description: >-
        Identifies the AlarmEventRecord(s) to purge. At least one of
        alarmRecordId or purgeConditions must be provided; if both are provided,
        the purge conditions are applied only to the listed records (intersection).
      anyOf:
      - required: [alarmRecordId]
      - required: [purgeConditions]
      properties:
        alarmRecordId:
          type: array
          description: AlarmEventRecord IDs to be purged.
          items:
            type: string
            format: uuid
        purgeConditions:
          type: object
          description: >-
            Time window for inactive/acknowledged alarms to be purged. Times
            refer to the alarmClearedTime of the AlarmEventRecord.
          required:
          - endWindowTime
          properties:
            startWindowTime:
              type: string
              format: date-time
              description: >-
                Start of the window. If omitted, all cleared alarms older than
                endWindowTime are considered.
            endWindowTime:
              type: string
              format: date-time
              description: >-
                End of the window. If only this is provided, all cleared alarms
                older than this time are considered.

    TaskOperationInfo:
      type: object
      description: Represents a task operation occurrence (e.g., an alarm purge).
      required:
      - id
      - operationState
      - stateEnteredTime
      - startTime
      - operation
      properties:
        id:
          type: string
          format: uuid
          description: Identifier of this task operation occurrence.
        operationState:
          type: string
          description: State of the task operation.
          enum: [PROCESSING, COMPLETED, FAILED, ROLLED_BACK]
        stateEnteredTime:
          type: string
          format: date-time
          description: When the current state was entered.
        startTime:
          type: string
          format: date-time
          description: When the operation started.
        operation:
          type: string
          description: Type of operation represented by this occurrence.
          enum: [PURGE]
        operationParams:
          description: >-
            Input parameters of the triggered operation. For PURGE this is the
            originating PurgeRequest.
          $ref: '#/components/schemas/PurgeRequest'
        error:
          description: Error information; present when operationState is FAILED.
          $ref: '../../common/api/openapi.yaml#/components/schemas/ProblemDetails'
```

**New tag** (add under `tags:`):

```yaml
- name: taskOperations
  description: Asynchronous task operations (alarm purge)
```
