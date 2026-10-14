# O2IMS Interface v13 Alignment

```yaml
title: o2ims-v13
version: "2.0"
authors:
  - @rauherna
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-09-21
last-updated: 2026-09-29
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
> - **Alarm dictionary taxonomy (Section 4)**: The standardized alarm definition
>   IDs (e.g., `CLUSTER_WARNING`) and probable cause mappings (e.g., `CPU_BUS`,
>   `NODE_DOWN`) are alarm-producer taxonomy concerns, not API contract changes.
>   `probableCauseId` remains unpopulated. Deferred to a follow-up effort.
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

#### Missing Endpoints

| Resource | URI | Method | Since | Action |
|----------|-----|--------|-------|--------|
| Alarm Subscription Update | `/alarmSubscriptions/{id}` | PATCH | 2.2.0 (v13) | New method + `AlarmSubscriptionUpdate` body |

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

> **`filter` vs `eventFilter` — they are semantically different:**
>
> | Aspect | `filter` (deprecated) | `eventFilter` (new in 2.2.0) |
> |--------|----------------------|------------------------------|
> | Type | String (free-form) | EventFilter (typed enum) |
> | P | O (Optional) | M (Mandatory) |
> | Semantics | ETSI attribute-based filtering expression (clause 5.2 of ETSI GS NFV-SOL 013) — ODATA-style query on any attribute | Enum: NEW, CHANGE, CLEAR, ACKNOWLEDGE — filters by notification event type only |
>
> Our current `filter` implementation already uses a string enum
> `[NEW, CHANGE, CLEAR, ACKNOWLEDGE]` — so we accidentally implemented
> `eventFilter` semantics under the `filter` name. We never implemented
> the ETSI attribute-based filtering that `filter` was originally intended
> for. The spec deprecated `filter` (NOTE on Table 3.3.6.2.3-1).

| Field | Change | Since |
|-------|--------|-------|
| `eventFilter` | Add new enum field (NEW, CHANGE, CLEAR, ACKNOWLEDGE). In v1: optional, `filter` still accepted. In v2: required, `filter` deprecated but accepted for backward compatibility. Reject with 400 if both are provided with conflicting values. | 2.2.0 (v13) |
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
| `eventFilter` on AlarmSubscriptionInfo | Add alongside existing `filter` |
| `consumerSubscriptionId` type | Change from UUID to String |
| PATCH `/alarmSubscriptions/{id}` | New method + `AlarmSubscriptionUpdate` type |
| Alarm Change Notification | Add `objectRef`, `objectClass`, `consumerSubscriptionId` |
| DB migrations | New columns (`object_class`, `event_filter`) |
| Test coverage | Unit, envtest, server tests for all new endpoints and fields |

### PR 3: Monitoring v2 Breaking Changes

| Item | Change |
|------|--------|
| API URL path | Bump from `/v1/` to `/v2/` |
| `objectClass` | Make mandatory on AlarmEventRecord |
| `eventFilter` | Make required in v2. Deprecate `filter` (still accepted for backward compatibility). |
| RBAC roles | Update monitoring endpoint paths in `oran_o2ims_user_roles.yaml` |
| OAuth bindings | Update monitoring bindings in `oran_o2ims_oauth_role_bindings.yaml` |
| OpenAPI spec version | Update to reference v13 |
| Docs | Update monitoring-related documentation |

### Ordering Rationale

| Order | Reason |
|-------|--------|
| Inventory/Provisioning first | Trivial changes; clears the backlog early |
| Monitoring non-breaking before breaking | New fields/endpoints are available before the URL path changes. `objectClass` population is verified before making it mandatory. Consumers can adopt `eventFilter` and PATCH while still on v1. |
| Breaking changes last | Clean cutover after all features are in place |

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
| Deprecated filter | Existing `filter` field still works alongside `eventFilter` |
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
