<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: NodeGroups for ClusterInstance Input

```yaml
title: nodegroups-clusterinstance-input
authors:
  - @angiewang
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-08-05
last-updated: 2026-08-06
jira: CNF-18657
```

## Table of Contents

- [Summary](#summary)
- [Motivation](#motivation)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Current State](#current-state)
- [Proposed Design](#proposed-design)
  - [Overview](#overview)
  - [Example: defaults ConfigMap](#example-defaults-configmap)
  - [Example: ProvisioningRequest](#example-provisioningrequest)
  - [What comes from where](#what-comes-from-where)
  - [`clusterInstanceParameters` schema (ProvisioningRequest)](#clusterinstanceparameters-schema-provisioningrequest)
  - [Merge and Expansion](#merge-and-expansion)
  - [Dual-Format Support and Deprecation](#dual-format-support-and-deprecation)
  - [Validation](#validation)
- [Impact on Existing Flows](#impact-on-existing-flows)
  - [Hardware Provisioning and ClusterInstance Render](#hardware-provisioning-and-clusterinstance-render)
  - [Scale-In / Scale-Out](#scale-in--scale-out)
- [Phased Implementation](#phased-implementation)
- [Open Questions](#open-questions)

## Summary

Introduce a `nodeGroups` structure for ClusterInstance defaults and
ProvisioningRequest `clusterInstanceParameters` so multi-node (MNO) clusters
no longer require duplicating shared per-node configuration once per host.

Input is organized by node group (aligned with `hwMgmtDefaults.nodeGroupData`
by `name`). Shared fields live once per group; only per-node identity and
addressing are listed under each group. The controller expands this input into
the existing flat ClusterInstance `spec.nodes[]` shape consumed by SiteConfig
and the rest of the provisioning pipeline.

Both `nodes` (legacy) and `nodeGroups` (new) are supported during migration.
`nodes` will be deprecated and removed after a documented migration period.

## Motivation

Today, `clusterInstanceDefaults` and `clusterInstanceParameters` follow the
ClusterInstance schema and merge `nodes` **by index**. For a large MNO
(for example 3 control-plane + 50 workers), the defaults ConfigMap must contain
53 near-identical node stubs, and scale-out documentation requires growing or
swapping that ConfigMap when workers are added.

Hardware selection is already group-based via `hwMgmtDefaults.nodeGroupData`.
ClusterInstance input should follow the same model: one shared definition per
node group, plus a list of per-node identity/addressing overrides.

## Goals

- Add a `nodeGroups` list (keyed by `name`) usable in both the
  ClusterInstance defaults ConfigMap and ProvisioningRequest
  `clusterInstanceParameters`.
- Deduplicate shared node fields (`bootMode`, `rootDeviceHints`, interface
  skeleton, `templateRefs`, shared nmstate pieces, labels/annotations, etc.).
- Allow the ProvisioningRequest to supply:
  - **group-level** common overrides (for example DNS, routes, extraLabels /
    extraAnnotations) via opaque `nodeNetwork.config` nmstate overlays, and
  - **per-node** identity (`hostName`) and flattened per-interface addresses
    on `nodeNetwork.interfaces[]` (`name` + `addresses.ipv4` /
    `addresses.ipv6`), merged by **interface name** into defaults’
    `nodeNetwork.config.interfaces`.
- Expand early to flat ClusterInstance `spec.nodes[]` so NAR sizing, BMC/MAC
  injection, and SiteConfig apply remain unchanged.
- Support **both** `nodes` and `nodeGroups` during migration; deprecate
  `nodes` with documentation and remove it after a transition period.
- Keep defaults’ `nodeNetwork.config` as today’s opaque nmstate object
  (`x-kubernetes-preserve-unknown-fields: true`); do not change the defaults
  ConfigMap nmstate shape for addressing.

## Non-Goals

- Changing the legacy flat `nodes` path to name-keyed interface merge — it
  keeps **index** merge as today.
- Fully flattening group-level DNS/routes (those remain nmstate under
  `nodeNetwork.config` in the PR).
- Changing SiteConfig / ClusterInstance CRD shape — rendered output remains
  flat `spec.nodes[]` with standard nmstate `config` (sugar fields stripped).
- Reworking scale-in/out in the same delivery as initial provision support
  (scale adaptation is a later phase under this epic; see
  [Phased Implementation](#phased-implementation)).

## Current State

- ClusterInstance defaults and PR `clusterInstanceParameters` both use
  `nodes: []`, merged with `DeepMergeMaps` / `DeepMergeSlices` (**by index**).
- Role and most node fields typically come from defaults; the PR supplies
  `hostName` and a partial `nodeNetwork.config` overlay.
- `hwMgmtDefaults.nodeGroupData` is already a **list with `name`**, merged
  name-keyed between defaults and `hwMgmtParameters`.
- Interface **labels** (`nodeNetwork.interfaces[].label`, e.g.
  `boot-interface`) join BMH inventory to logical interfaces for MAC
  assignment; they are stripped before ClusterInstance apply. That mechanism
  is unchanged by this proposal.
- Scale-out/in and webhook immutability logic largely inspect either the
  rendered ClusterInstance or raw PR paths under `nodes.*`.

## Proposed Design

### Overview

Two inputs still feed ClusterInstance rendering, but node config is grouped:

| Input | Who writes it | Purpose |
|---|---|---|
| ClusterInstance **defaults ConfigMap** | Template author | Shared per-group node config (once per MCP/group) |
| ProvisioningRequest **`clusterInstanceParameters`** | SMO | Site overlays + per-node hostname/addresses |

```text
defaults ConfigMap (`nodeGroups`: shared fields, no `nodes[]`)
         +
PR clusterInstanceParameters (`nodeGroups`: overlays + `nodes[]`)
         |
         |  name-keyed merge of groups, then expand
         ▼ 
flat ClusterInstance spec.nodes[]  (unchanged SiteConfig shape)
```

`nodeGroups` is a **list** of objects with `name` (same pattern as
`hwMgmtDefaults.nodeGroupData`), not a map with arbitrary keys.
`nodeGroups[].name` must match `hwMgmtDefaults.nodeGroupData[].name` and is
also the spoke MCP name.

Each template’s `templateParameterSchema` exposes **either** `nodes` **or**
`nodeGroups` (unknown fields are disallowed on the PR). The defaults ConfigMap
must use the same format as that schema. Legacy flat `nodes` remains supported
during migration for templates that still publish the `nodes` schema (see
[Dual-Format Support and Deprecation](#dual-format-support-and-deprecation)).

### Example: defaults ConfigMap

Template author — **one entry per group**, no per-host list (`nodeGroups[].nodes`
must be omitted). Cluster-level fields (`baseDomain`, `clusterImageSetNameRef`, …)
remain as today; only the former duplicated `nodes:` stubs become
`nodeGroups:`.

```yaml
# ConfigMap data.clusterinstance-defaults (excerpt)
baseDomain: example.com
clusterType: HighlyAvailable
# ... other cluster-level defaults ...
nodeGroups:
  - name: master                    # matches hwMgmtDefaults.nodeGroupData[].name
    role: master
    bootMode: UEFI
    ironicInspect: "disabled"
    automatedCleaningMode: disabled
    rootDeviceHints:
      deviceName: /dev/disk/by-path/pci-0000:01:00.0-nvme-1
    nodeNetwork:
      interfaces:
        - name: ens3f0
          label: boot-interface
      config:
        # Defaults keep the shared route/interface skeleton.
        routes:
          config:
            - destination: 0.0.0.0/0
              next-hop-interface: ens3f0
              table-id: 254
        interfaces:
          - name: ens3f0
            type: ethernet
            state: up
            ipv4:
              enabled: true
            ipv6:
              enabled: false
    templateRefs:
      - name: ai-node-templates-v1
        namespace: open-cluster-management
  - name: worker
    role: worker
    bootMode: UEFI
    ironicInspect: "disabled"
    automatedCleaningMode: disabled
    rootDeviceHints:
      deviceName: /dev/disk/by-path/pci-0000:01:00.0-nvme-1
    nodeNetwork:
      interfaces:
        - name: ens3f0
          label: boot-interface
      config:
        routes:
          config:
            - destination: 0.0.0.0/0
              next-hop-interface: ens3f0
              table-id: 254
        interfaces:
          - name: ens3f0
            type: ethernet
            state: up
            ipv4:
              enabled: true
            ipv6:
              enabled: false
    templateRefs:
      - name: ai-node-templates-v1
        namespace: open-cluster-management
```

### Example: ProvisioningRequest

SMO — cluster-level parameters as today, plus `nodeGroups` with **group-level
site network overlays** (nmstate under `nodeNetwork.config`) and **per-node
identity + flattened addresses** on `nodeNetwork.interfaces.addresses` (O-Cloud
convention).

```yaml
# ProvisioningRequest spec.templateParameters.clusterInstanceParameters
clusterName: mno1
apiVIPs: ["10.6.34.21"]
ingressVIPs: ["10.6.34.22"]
nodeGroups:
  - name: master
    # common network configuration across nodes
    nodeNetwork:
      config:
        dns-resolver:
          config:
            search: ["example.com"]
            server: ["10.11.5.160", "fd00:dns::1"]
        routes:
          config:
            - next-hop-address: "10.6.34.254"
    nodes:
      - hostName: master-1.example.com
        nodeNetwork:
          interfaces:
            - name: ens3f0
              addresses:
                ipv4: ["10.6.34.10/24"]
                ipv6: ["fd00:1:1::10/64"]
      - hostName: master-2.example.com
        nodeNetwork:
          interfaces:
            - name: ens3f0
              addresses:
                ipv4: ["10.6.34.11/24"]
                ipv6: ["fd00:1:1::11/64"]
      - hostName: master-3.example.com
        nodeNetwork:
          interfaces:
            - name: ens3f0
              addresses:
                ipv4: ["10.6.34.12/24"]
                ipv6: ["fd00:1:1::12/64"]
  - name: worker
    nodeNetwork:
      config:
        dns-resolver:
          config:
            search: ["example.com"]
            server: ["10.11.5.160", "fd00:dns::1"]
        routes:
          config:
            - next-hop-address: "10.6.34.254"
    nodes:
      - hostName: worker-1.example.com
        nodeNetwork:
          interfaces:
            - name: ens3f0
              addresses:
                ipv4: ["10.6.34.51/24"]
                ipv6: ["fd00:1:1::51/64"]
      # ... more workers: hostname + named interface addresses only ...
```

### What comes from where

| Field | Defaults ConfigMap | PR group level | PR `nodes[]` |
|---|---|---|---|
| `name`, `role`, boot/hints/`templateRefs`, iface name+label | **yes** | `name` only | no |
| iface skeleton, route structure (`next-hop-interface`, …) | **yes** | rare | no |
| `dns-resolver`, `next-hop-address` (site network) | no (as today) | **yes** | no |
| `extraLabels` / `extraAnnotations` / `nodeLabels` | **yes** | optional override | optional override |
| `hostName` | no | no | **yes** |
| per-interface addresses (IPv4/IPv6) | no | no | **yes** |
| `bmcAddress`, `bootMACAddress`, `bmcCredentialsName`, MACs | no | no | no — injected after HW provisioning (`applyNodeConfiguration`), not from either input |

Group-level PR fields deep-merge over defaults (PR wins). Hosts in a
node group are expected to share the same hardware and network shape, so
shared settings live once in the defaults ConfigMap. When a site needs
different shared values for a group (for example DNS or labels), set them
once at the PR group level instead of editing or duplicating the defaults
ConfigMap; per-host fields stay under `nodes[]`.

`nodeNetwork.interfaces[].addresses` is an **O-Cloud-only** field (like
`label`): it is not part of the ClusterInstance / nmstate schema. The
controller maps CIDR strings into
`nodeNetwork.config.interfaces[<name>].ipv4/ipv6.address`, then strips
`addresses` (and `label`) before dry-run / SiteConfig apply. Using
`addresses.ipv4` / `addresses.ipv6` avoids clashing with nmstate’s
`config.interfaces[].ipv4` **object** shape.

### `clusterInstanceParameters` schema (ProvisioningRequest)

`ClusterTemplate.spec.templateParameterSchema` exposes what the SMO may send.
For templates that adopt this format, the top-level flat `nodes` array is
replaced by `nodeGroups`. Per-host site input moves under each group’s nested
`nodes` list; other cluster-level properties (`clusterName`, `apiVIPs`, …)
stay unchanged. Illustrative excerpt:

```yaml
clusterInstanceParameters:
  type: object
  required:
    - clusterName
    - nodeGroups
  properties:
    clusterName:
      type: string
    # apiVIPs, ingressVIPs, machineNetwork, extraLabels, ... as today
    nodeGroups:
      type: array
      description: >
        Node configuration by group. Each name must match
        hwMgmtDefaults.nodeGroupData[].name (MCP name).
      items:
        type: object
        required:
          - name
          - nodes
        properties:
          name:
            type: string
            description: Node group / MCP name (matches hwMgmt nodeGroupData)
          # Optional group-level overlays (role is defaults-only; not exposed here)
          nodeLabels:
            type: object
            additionalProperties:
              type: string
            description: >
              Custom node labels applied to every host in this group
              (merged onto each expanded ClusterInstance node)
          extraLabels:
            type: object
            additionalProperties:
              type: object
              additionalProperties:
                type: string
          extraAnnotations:
            type: object
            additionalProperties:
              type: object
              additionalProperties:
                type: string
          nodeNetwork:
            type: object
            properties:
              config:
                type: object
                description: Opaque nmstate overlay (dns-resolver, routes, ...)
                x-kubernetes-preserve-unknown-fields: true
          nodes:
            type: array
            minItems: 1
            items:
              type: object
              required:
                - hostName
                - nodeNetwork
              properties:
                hostName:
                  type: string
                nodeLabels:
                  type: object
                  additionalProperties:
                    type: string
                extraLabels:
                  type: object
                  additionalProperties:
                    type: object
                    additionalProperties:
                      type: string
                extraAnnotations:
                  type: object
                  additionalProperties:
                    type: object
                    additionalProperties:
                      type: string
                nodeNetwork:
                  type: object
                  required:
                    - interfaces
                  properties:
                    interfaces:
                      type: array
                      minItems: 1
                      description: Per-interface addressing.
                      items:
                        type: object
                        required:
                          - name
                          - addresses
                        properties:
                          name:
                            type: string
                            minLength: 1
                            description: >
                              NIC name; must match defaults
                              nodeNetwork.interfaces[].name
                          addresses:
                            type: object
                            minProperties: 1
                            description: >
                              At least one of ipv4 or ipv6; each list
                              has minItems 1 when present
                            properties:
                              ipv4:
                                type: array
                                minItems: 1
                                items:
                                  type: string
                                  minLength: 1
                                description: CIDR strings, e.g. "10.6.34.10/24"
                              ipv6:
                                type: array
                                minItems: 1
                                items:
                                  type: string
                                  minLength: 1
                                description: CIDR strings, e.g. "fd00:1:1::10/64"
```

Notes:

- `role` is **not** part of the PR schema; it comes from the defaults
  ConfigMap `nodeGroups[].role` (and must stay aligned with
  `hwMgmtDefaults.nodeGroupData[].role`).
- Group-level `nodeNetwork.config` stays opaque nmstate (DNS, routes, …).
  Per-node input uses flattened `interfaces[].name` + `addresses` only.
  - `nodeNetwork.config` stays opaque (same as today / ClusterInstance).
- Defaults ConfigMap is **not** constrained by this PR schema; it may include
  full node fields (`role`, `bootMode`, `rootDeviceHints`, `templateRefs`,
  `nodeNetwork.interfaces` with labels, full nmstate skeleton).
- During migration, templates may still publish the legacy `nodes` schema
  instead of `nodeGroups`.

### Merge and Expansion

1. Detect format: `nodeGroups` present → this path; else legacy `nodes` path.
2. Merge PR `nodeGroups` with defaults `nodeGroups` (**by group `name`**).
   Shared group fields deep-merge (PR wins). The host list
   (`nodeGroups[].nodes`) comes from the PR only — defaults must omit it
   (see [Validation](#validation)).
3. For each merged group, for each PR node under that group, merge the per-node
   input onto the group’s shared fields to form one flat ClusterInstance node:
   - Slice fields that carry a `name` (in particular `nodeNetwork.interfaces`)
     merge **by `name`**. If there are duplicate names, later entries win (no
     uniqueness validation - same spirit as allowing duplicate hostnames today
     and letting downstream behaviour decide).
   - map `interfaces[].addresses` into the matching `nodeNetwork.config.interfaces`
     entry by `name`
   - strip O-Cloud-only fields (`addresses`, group `name`, and later `label` as today)
     before dry-run / render
   - append to a flat `nodes` result
4. Collect the expanded nodes as the merged ClusterInstance input used
   today (`clusterInstanceData` with `nodes`).

After expansion, existing steps apply unchanged: dry-run validation, NAR size
from rendered node roles, BMC/MAC injection, SiteConfig apply.

### Dual-Format Support and Deprecation

| Phase | Behavior |
|---|---|
| Introduction (Phase 1) | Accept `nodes` **or** `nodeGroups`. Ship MNO samples on `nodeGroups`; SNO samples may remain on legacy `nodes`. |
| SNO migration + deprecation (Phase 3) | Migrate SNO samples/tests to `nodeGroups`; mark `nodes` deprecated; `nodeGroups` is the recommended format for all templates. |
| Removal (Phase 4) | After the deprecation window: reject `nodes` and delete the legacy merge path. |

Legacy `nodes` remains fully supported through Phase 3’s window. Removal is a
separate phase.

### Validation

Checks are split across the ClusterTemplate controller, the
ProvisioningRequest controller (merge/validate path), and the
ProvisioningRequest webhook. Schema validation against
`templateParameterSchema` continues to apply as today.

#### ClusterTemplate controller

- Ensure the defaults ConfigMap format is consistent with the CT
  `templateParameterSchema` for `clusterInstanceParameters`:
  - if the schema exposes `nodes`, defaults must use `nodes` (not
    `nodeGroups`);
  - if the schema exposes `nodeGroups`, defaults must use `nodeGroups` (not
    `nodes`).
- When defaults use `nodes`: keep today’s
  `ValidateConfigmapSchemaAgainstClusterInstanceCRD` path (including
  `DisallowUnknownFieldsInSchema` and stripping interface `label` before
  validate).
- When defaults use `nodeGroups`: adapt that same CRD-based check rather
  than inventing a parallel field-by-field validator. Reuse that path with
  a small adaptation:
  1. rename CRD `nodes` → `nodeGroups` for validation;
  2. treat group `name` like interface `label` — an O-Cloud-only field:
     validate it separately (non-empty, unique, match
     `hwMgmtDefaults.nodeGroupData`), then strip it before the CRD schema
     check (same pattern as `RemoveLabelFromInterfaces`).

  Group-level fields that already exist on NodeSpec (`role`, `bootMode`,
  `rootDeviceHints`, `nodeNetwork`, `templateRefs`, labels/annotations,
  …) are then covered by the CRD schema the same way flat `nodes` are
  today.

- Additional `nodeGroups`-specific checks (not in the ClusterInstance CRD):
  - non-empty unique `nodeGroups[].name` (before strip);
  - every `nodeGroups[].name` must exist in `hwMgmtDefaults.nodeGroupData[].name`
  - `role` valid and consistent with existing `hwMgmtDefaults.nodeGroupData`
    rules (including one group per role, as today);
  - **`nodeGroups[].nodes` must be omitted** in the defaults ConfigMap —
    per-host identity belongs only in the ProvisioningRequest;
  - existing interface-label checks (`ValidateDefaultInterfaces`) adapted
    for the `nodeGroups` shape.

#### ProvisioningRequest controller

During merge/validation of `clusterInstanceParameters` with defaults:

- Validate PR input against `templateParameterSchema` as today (unknown
  fields disallowed via `DisallowUnknownFieldsInSchema`). A template
  publishes **either** `nodes` **or** `nodeGroups` — not both — so format
  selection, required `nodeGroups[].nodes`, per-node `hostName` /
  `nodeNetwork.interfaces` (with `name` + `addresses`), are covered by that
  schema; do not duplicate those checks.
- For the `nodeGroups` path, **before** the name-keyed merge:
  - reject duplicate `nodeGroups[].name` in the PR (defaults uniqueness is
    enforced by the ClusterTemplate controller);
  - require an **exact name-set match** between PR `nodeGroups[].name` and
    defaults `nodeGroups[].name` — reject unknown PR groups **and** reject
    defaults groups missing from the PR. That means SMO cannot invent a
    node group that was not in the template defaults; adding a new node
    group type requires a new ClusterTemplate version (new defaults).
- When applying flattened addresses: each PR
  `nodeNetwork.interfaces[].name` must exist on the merged group’s
  `nodeNetwork.interfaces` / `config.interfaces`; reject unknown NIC names.
- After merge, every `nodeGroups[].name` must match
  `hwMgmtDefaults` / merged `hwMgmtParameters` `nodeGroupData[].name`.
- Expand to flat `nodes[]`, then continue with existing ClusterInstance
  schema dry-run / render validation.
- Existing hwMgmt checks remain (nodeGroupData presence, role constraints,
  HardwareProfile references, etc.).

#### ProvisioningRequest webhook

##### Create

- Lightweight checks only if useful before reconcile; full shape validation
  against the CT schema happens in the controller as today. No separate
  mixed-format or hostname-required checks beyond what the schema already
  encodes.

##### Update

- **Phase 1: no webhook code changes.** Keep today’s immutability
  policy and old-vs-new diff as-is:
  - during cluster installation (`ClusterProvisioned=InProgress`): reject
    all `clusterInstanceParameters` changes (fields and membership);
  - after installation completes: only `extraLabels` / `extraAnnotations`
    (and worker scaling on flat `nodes`) are allowed;
  Host add/remove under `nodeGroups[].nodes` is not recognized as scaling
  yet, so it is rejected like any other disallowed field change until
  Phase 2.
- **Phase 2:** update the webhook (and allowed-field path patterns) for
  `nodeGroups` so membership diffs under `nodeGroups[].nodes` follow
  today’s worker scaling rules, and label/annotation allow-lists cover
  `nodeGroups.*` paths as needed.

## Impact on Existing Flows

### Hardware Provisioning and ClusterInstance Render

Low impact if expansion happens during merge/validation **before** NAR build
and HW injection. Those components already consume flat `nodes[]`.

### Scale-In / Scale-Out

Controller scale logic compares hostnames on the **rendered** ClusterInstance
and should need little change once expansion exists.

Webhook immutability / scaling detection today diffs PR paths under `nodes.*`
and **will** need updates for `nodeGroups`.

Scale-out today often requires adding another stub to the defaults ConfigMap;
with `nodeGroups`, scale-out only adds an entry under the worker group’s
`nodes` list in the ProvisioningRequest — defaults stay fixed. That
simplification lands in Phase 2.

## Phased Implementation

### Phase 1 — MNO provision with `nodeGroups` (this epic’s first delivery)

- Merge/expand `nodeGroups` → flat `nodes[]`, including flattened
  `interfaces[].addresses` mapped by **interface name** into nmstate
  `config.interfaces`.
- Dual-format support (schema selects `nodes` or `nodeGroups` per template;
  legacy `nodes` path kept working as today).
- Migrate **MNO** sample ClusterTemplates / defaults ConfigMaps and related
  docs to `nodeGroups`.
- Unit and e2e coverage for:
  - **MNO** provision with `nodeGroups`
  - Legacy `nodes` path still works (compat; covers existing SNO samples)
- Webhook: immutability policy unchanged; scale path updates deferred to Phase 2.

### Phase 2 — Scale-in / scale-out on `nodeGroups`

- Webhook + controller support for adding/removing worker entries under
  `nodeGroups[].nodes`.
- Update scale docs: no more growing the defaults ConfigMap per worker.
- Swap/abort behavior preserved on expanded hostnames.

### Phase 3 — SNO migration and deprecate `nodes`

- Migrate **SNO** sample ClusterTemplates / defaults, user-guide examples,
  and e2e tests to `nodeGroups` (single group, one host).
- Mark legacy `nodes` deprecated in docs and release notes;
  `nodeGroups` becomes the recommended format for all templates.
- Announce the deprecation window; keep the legacy merge path working
  through the window.

### Phase 4 — Remove legacy `nodes` path (separate phase)

- After the deprecation window: reject `nodes` in defaults and
  `clusterInstanceParameters`.
- Delete the legacy index-merge path for flat `nodes`.
- Dev-preview may treat this as a breaking change with clear release notes.

## Open Questions

1. Deprecation window length (dates) before `nodes` removal.
