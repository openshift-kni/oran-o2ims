<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Automated Seed Image and Live ISO Generation via ProvisioningRequest API

## Table of Contents

- [Background](#background)
- [Problem Statement](#problem-statement)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Prerequisites](#prerequisites)
- [Proposed API Changes](#proposed-api-changes)
  - [New key: `seedGeneration`](#new-key-seedgeneration)
  - [ProvisioningRequest overrides](#provisioningrequest-overrides)
  - [Schema in templateParameterSchema](#schema-in-templateparameterschema)
- [Controller Workflow](#controller-workflow)
  - [Functional Overview](#functional-overview)
  - [Trigger and Dispatch](#trigger-and-dispatch)
  - [Phase 1: Validation](#phase-1-validation)
  - [Phase 2: ACM Agent Cleanup](#phase-2-acm-agent-cleanup)
  - [Phase 3: Trigger Seed Generation](#phase-3-trigger-seed-generation)
  - [Phase 4: Live ISO Generation](#phase-4-live-iso-generation)
  - [Phase 5: Completion and Cleanup](#phase-5-completion-and-cleanup)
- [New Status Fields](#new-status-fields)
- [New Condition Type](#new-condition-type)
- [Timeout](#timeout)
- [Sample: End-to-End ClusterTemplate](#sample-end-to-end-clustertemplate)
- [Challenges and Mitigations](#challenges-and-mitigations)
  - [1. Incomplete ACM cleanup contaminates the seed image](#1-incomplete-acm-cleanup-contaminates-the-seed-image)
  - [2. Spoke client availability during seed generation](#2-spoke-client-availability-during-seed-generation)
  - [3. ACM self-healing during cleanup phase](#3-acm-self-healing-during-cleanup-phase)
  - [4. Seed cluster afterlife and PR terminal state](#4-seed-cluster-afterlife-and-pr-terminal-state)
  - [5. Seed SNO prerequisites](#5-seed-sno-prerequisites)
  - [6. Spoke access after ACM removal](#6-spoke-access-after-acm-removal)
  - [7. ISO generation Job storage requirements](#7-iso-generation-job-storage-requirements)
  - [8. Disconnected environment registry configuration](#8-disconnected-environment-registry-configuration)
  - [9. HTTPS ISO server certificate management](#9-https-iso-server-certificate-management)
  - [10. Registry pre-flight check limitations](#10-registry-pre-flight-check-limitations)
- [Alternatives Considered](#alternatives-considered)
  - [A. Full ACM detachment (delete ManagedCluster)](#a-full-acm-detachment-delete-managedcluster)
  - [B. Separate SeedGenerationRequest CRD](#b-separate-seedgenerationrequest-crd)
  - [C. External job/pipeline](#c-external-jobpipeline)
  - [D. Controller-managed LCA installation](#d-controller-managed-lca-installation)
  - [E. ISO generation via external pipeline (Tekton, CI/CD)](#e-iso-generation-via-external-pipeline-tekton-cicd)
  - [F. Default to public release image registry](#f-default-to-public-release-image-registry)
- [Implementation Constants](#implementation-constants)
- [Effort Estimate](#effort-estimate)
- [Open Questions](#open-questions)

## Background

Today, generating a seed image and live ISO for IBI-based cluster provisioning is a multi-step manual process (documented in `docs/user-guide/ibi-based-cluster-provisioning.md`). An operator must:

1. Provision a reference SNO cluster via ProvisioningRequest (with a specific `var-lib-containers` MachineConfig)
2. Annotate the PR with `clcm.openshift.io/skip-cleanup` and delete it to detach the spoke from ACM
3. SSH into the spoke, install the LifeCycle Agent (LCA) operator, create a `seedgen` Secret and a `SeedGenerator` CR
4. Monitor the SeedGenerator until the seed image is pushed to the container registry
5. Obtain the `openshift-install` binary matching the OCP version, create an `ImageBasedInstallationConfig`, and run `openshift-install image-based create image` to produce a live ISO
6. Upload the ISO to an HTTPS server accessible from the BMC management network
7. Pre-provision servers using the ISO via BMC virtual media

This proposal automates steps 1 through 6 as a first-class operation within the ProvisioningRequest API.

## Problem Statement

The manual seed generation workflow has several pain points:

- **Error-prone**: Requires precise ordering of annotate → delete → install LCA → create resources → monitor, plus matching the correct `openshift-install` binary version to build the ISO
- **Untracked**: No status reporting through the O-Cloud Manager API; operators must SSH and tail logs
- **Inconsistent**: Each team may follow a slightly different procedure
- **Not scalable**: Generating seed images and ISOs for multiple hardware/OCP configurations requires repeating the full manual procedure
- **Disconnected complexity**: In disconnected environments (the predominant use case), operators must also manage release image mirrors and ensure the `openshift-install` binary, seed image, and OCP release images are all accessible from the correct registries

## Goals

- Automate seed image generation (annotate → detach → install/create LCA
  resources → monitor → push) as a first-class operation driven by the
  existing ProvisioningRequest API, eliminating the manual SSH-based
  procedure.
- Automate live installation ISO generation and upload to the HTTPS server
  as part of the same workflow, matching the correct `openshift-install`
  version automatically.
- Report progress and results through the O-Cloud Manager API via a typed
  `SeedGenerationCompleted` condition and `SeedGenerationStatus` fields, so
  operators no longer need to SSH and tail logs.
- Treat the seed cluster as a disposable factory: remove all ACM agent
  state (klusterlet and addons) from the spoke so it does not contaminate
  the seed, retain spoke access during generation via the spoke's admin
  kubeconfig retrieved from the hub (which survives ACM teardown and writes
  nothing to the spoke, so nothing is baked into the seed), and do **not**
  restore ACM afterward. The controller never deletes the `ManagedCluster`
  itself, and it **never auto-deletes the ProvisioningRequest** — results
  stay readable as conditions on the PR until the operator deletes it to
  reclaim the hardware once the seed is captured.
- Reuse the existing `upgradeDefaults` / `upgradeParameters`
  merge-and-validate infrastructure rather than introducing a new CRD or
  controller.
- Validate prerequisites and fail fast with a clear
  `PreconditionChecksFailed` condition (registry push access, release image
  reachability, TLS/CA trust, upload credentials) before the lengthy
  generation begins.
- Support disconnected environments as the primary use case, auto-deriving
  mirror mappings and registry trust from the hub (local mirrors, private CA)
  so the template author supplies only the required `releaseImage`.

## Non-Goals

- Installing or managing the prerequisite operators (LCA, OADP) or the
  `var-lib-containers` MachineConfig — these remain the cluster template
  author's responsibility; the controller only validates their presence.
- Validating hardware-specific seed prerequisites the hub cannot inspect
  (CPU topology alignment, FIPS, proxy, IP-version match with target SNOs);
  see [Challenge 5](#5-seed-sno-prerequisites).
- Configuring BMC virtual media or BMC CA trust to consume the generated
  ISO — that belongs to the hardware manager / pre-provisioning workflow
  (see the IBI Server Pre-Provisioning proposal). This proposal only
  surfaces the ISO server CA cert in status for downstream consumption.
- Introducing a dedicated seed-generation CRD or a separate controller
  (rejected in [Alternative B](#b-separate-seedgenerationrequest-crd)).
- Detaching the spoke from ACM by deleting the ManagedCluster (rejected in
  [Alternative A](#a-full-acm-detachment-delete-managedcluster)).
- Supporting ISO upload mechanisms beyond SCP/SFTP in the initial
  implementation (see [Open Questions](#open-questions)).

## Prerequisites

The seed SNO cluster must be provisioned with the following already in place:

- **LifeCycle Agent (LCA) Operator**: Must be installed as part of the initial cluster configuration via the ClusterTemplate's `policyTemplateDefaults`. The controller validates LCA is present before proceeding with seed generation.
- **`var-lib-containers` partition**: The ClusterTemplate's `clusterInstanceDefaults` must include the MachineConfig that creates a separate `/var/lib/containers` partition. This is a **seed-generation** prerequisite on the seed cluster:
  LCA validates a shared `/var/lib/containers` before it will generate the seed, independent of whether the resulting seed is later consumed by IBI or IBU. (The matching *target*-side shared partition is what is IBU-specific — for the dual-stateroot
  pivot during upgrade — and is out of scope for this proposal, which only produces the seed and, optionally, the IBI live ISO.)
- **OADP Operator**: Required by LCA for backup/restore operations during seed generation.

These are the responsibility of the cluster template author. The controller validates their presence but does not install them.

Live ISO generation (Phase 4) is **IBI-only and optional**: it is driven by the `liveISO` block and produces the bootable installation ISO that IBI needs to install bare-metal targets. A seed intended only for IBU (in-place upgrade of an existing
cluster) needs no ISO — omit `liveISO` and the controller stops after the seed image is pushed.

## Proposed API Changes

All new attributes go under `upgradeDefaults` / `upgradeParameters` (the existing extensible JSON schema), following the same merge-and-validate pattern used by `clusterVersion` and `imageBasedGroupUpgrade`.

Today two mutually-exclusive upgrade-type keys are recognized — `clusterVersion` and `imageBasedGroupUpgrade` — plus the optional `clusterUpgradeTimeout` and `intermediateVersion` (EUS) keys. `seedGeneration` becomes a third upgrade-type key. Adding it requires updating **both**
enforcement points that currently require *exactly one of* the two existing types:

- **Schema validation** — `validateUpgradeParametersSchema` / `validateUpgradeTypeProperty` in `clustertemplate_controller.go`, which today require exactly one of `clusterVersion` / `imageBasedGroupUpgrade` in the `templateParameterSchema`.
- **Runtime parsing** — `parseUpgradeConfig` in `provisioningrequest_upgrade.go`, which today errors if both types are present or if neither is present.

Both must be taught that `seedGeneration` is a valid third type (still mutually exclusive with the other two).

### New key: `seedGeneration`

A new top-level key in `upgradeDefaults`, mutually exclusive with `clusterVersion` and `imageBasedGroupUpgrade`:

```yaml
# In ClusterTemplate spec.templateDefaults.upgradeDefaults
upgradeDefaults:
  seedGeneration:
    # Required: full pull-spec for the seed image to be published
    seedImage: quay.io/my-org/seed-image:4.Y.Z
    # Required: reference to a Secret (in the ClusterTemplate namespace)
    # containing registry credentials for pushing the seed image.
    # The Secret must have a 'seedAuth' key with base64-encoded
    # docker/podman auth JSON (same format as LCA's seedgen Secret).
    seedAuthSecretRef:
      name: seed-registry-credentials
    # Optional: custom recert image override (LCA default used if omitted)
    recertImage: ""
    # Optional: live ISO generation configuration. When present, the
    # controller builds a live installation ISO after seed generation
    # and uploads it to an external HTTPS server.
    liveISO:
      # Required: the OCP release image to extract openshift-install from.
      # In disconnected environments this may be the canonical release
      # pull-spec (redirected to the mirror by the hub-derived mappings)
      # or a direct mirror pull-spec.
      releaseImage: my-mirror.example.com/ocp-release:4.Y.Z-x86_64
      # Required: disk device path on the target servers
      installationDisk: /dev/disk/by-path/pci-0000:43:00.0-nvme-1
      # Optional: SSH public key baked into the ISO for debugging access
      sshKey: "ssh-rsa AAAA..."
      # Required: reference to a Secret containing SSH credentials for
      # uploading the ISO to the HTTPS server. The Secret must contain:
      #   host        - hostname or IP of the server
      #   username    - SSH username
      #   privateKey  - SSH private key (PEM-encoded)
      #   remotePath  - destination file path on the server
      #   knownHosts  - the server's SSH host public key(s) in
      #                 known_hosts format (host-key pinning). Required:
      #                 the upload runs with StrictHostKeyChecking=yes
      #                 against this file and fails closed if the server's
      #                 key is absent or does not match, preventing a
      #                 man-in-the-middle from intercepting the ISO and
      #                 the mounted credentials. Trust-on-first-use is
      #                 not permitted.
      #   caCert      - (optional) PEM-encoded CA certificate bundle for
      #                 the HTTPS server. If provided, the controller
      #                 validates the server's TLS certificate during
      #                 pre-flight checks. This CA cert is also used
      #                 by BMCs that need to trust the server when
      #                 fetching the ISO via virtual media — the
      #                 controller stores it in status so it can be
      #                 referenced when configuring BMC virtual media.
      uploadSecretRef:
        name: iso-server-credentials
      # Required: the HTTPS origin plus base path from which the ISO is
      # served, backed by the same directory tree the upload writes into.
      # The controller computes the effective per-run ISO URL as
      # urlBase + a per-run suffix and records it in status; the full URL
      # is never supplied directly.
      urlBase: https://iso-server.example.com/ibi/
      # Optional: reference to a Secret containing the pull secret for
      # the ISO build. Must include credentials for both the seed image
      # registry and the OCP release image registry. If omitted, the
      # controller merges the hub cluster pull secret with the
      # seedAuthSecretRef credentials.
      pullSecretRef:
        name: iso-pull-secret
      # Optional: StorageClass for the ISO build workspace PVC (~20GB).
      # If omitted, the cluster's default StorageClass is used.
      storageClass: fast-storage
      # Optional: mirror mappings for the ISO build. If omitted, the
      # controller auto-derives them from the hub's ImageDigestMirrorSet
      # (and legacy ImageContentSourcePolicy) resources. Set this only to
      # target a mirror that differs from the hub's.
      imageDigestSources:
        - source: registry.redhat.io/openshift4/ose-cli
          mirrors:
            - my-mirror.example.com/openshift4/ose-cli
      # Optional: reference to a ConfigMap (in the ClusterTemplate
      # namespace) holding the registry CA trust bundle for the mirror.
      # If omitted, the controller auto-derives it from the hub's
      # image.config.openshift.io/cluster additionalTrustedCA.
      additionalTrustBundleConfigMapRef:
        name: mirror-registry-ca
  # Optional: timeout for the entire operation (seed generation + ISO build)
  clusterUpgradeTimeout: "3h"
```

### ProvisioningRequest overrides

Per-PR overrides are supplied via `templateParameters.upgradeParameters.seedGeneration`, following the existing merge precedence (PR overrides CT defaults):

```yaml
# In ProvisioningRequest spec.templateParameters
upgradeParameters:
  seedGeneration:
    # Override the seed image tag for this specific generation
    seedImage: quay.io/my-org/seed-image:4.Y.Z-custom
```

**Validation applies to the merged object, not the raw override.** A PR
override is intentionally partial — the example above supplies only
`seedImage`, relying on the ClusterTemplate defaults for
`seedAuthSecretRef`, `liveISO`, and the rest. Required-field validation
(`required: [seedImage, seedAuthSecretRef]`, and the `liveISO` required
set) must therefore run **after** the CT defaults are merged over the PR
overrides, not against the raw `spec.templateParameters` in isolation.

There is a specific ordering hazard the implementation must resolve. The
admission webhook today calls `ValidateTemplateInputMatchesSchema` on the
**raw** `spec.templateParameters` *before* `ValidateUpgradeInput` runs, and that
first call does **not** merge `TemplateDefaults.UpgradeDefaults`. If the
`seedGeneration` sub-schema carries `required: [seedImage, seedAuthSecretRef]`
(and the `liveISO` required set), a partial override supplying only `seedImage`
is rejected at that first gate before any merge. Two acceptable fixes: (a) merge
`UpgradeDefaults` into the effective object *before* the schema check, so
admission sees the same document the controller acts on; or (b) keep the
raw-params check structural only (types/patterns) and move `required`
enforcement into the post-merge `ValidateUpgradeInput` path. This proposal
specifies option (a) — validate the merged object — so a partial override is
never rejected for fields the defaults provide, and admission and the controller
agree on one document. A test must cover this ordering by submitting a
`seedImage`-only override against defaults that provide the rest.

**Restricting PR-controlled outbound targets and credential selection.**
Two distinct classes of `seedGeneration` field are dangerous in the hands
of a less-trusted PR author:

- **Outbound targets** that drive hub-side network egress while credentials
  are mounted: `liveISO.urlBase`, `liveISO.releaseImage`, `seedImage`,
  `liveISO.imageDigestSources` / `additionalTrustBundleConfigMapRef`.
  Overriding these can redirect hub egress to attacker-controlled or
  internal endpoints (SSRF) or push/pull against an unintended registry.
- **Executable image references** that select code that runs during seed
  generation: `recertImage`. Phase 3 passes `recertImage` to the
  `SeedGenerator` CR, and the LCA runs it (the recert tool) on the spoke, so
  a PR-supplied value would let an untrusted author run an arbitrary image on
  the seed cluster. This is template-owned; when set from a PR it is rejected,
  and the resolved value must be an allowlisted, immutable
  (`repo@sha256:<digest>`) reference.
- **Credential references** that select which hub Secret/ConfigMap the
  workflow reads and mounts into the ISO-build Job: `seedAuthSecretRef`,
  `liveISO.uploadSecretRef`, `liveISO.pullSecretRef`. Overriding these lets
  a caller point the workflow at *any* Secret the controller's service
  account can read, exfiltrating those credentials into an ISO/Job the
  caller influences.

The default posture is that **all** of these fields are template-owned: set by
trusted authors in `upgradeDefaults` and **not** exposed in
`templateParameterSchema.upgradeParameters`, so admission rejects any override
attempt, making them effectively immutable from the PR side. The only field
expected to be overridable is a benign one such as the `seedImage` tag *when*
the deployment trusts its PR authors. Where a deployment must accept these
overrides from less-trusted principals, it must enforce admission checks rather
than trust the value: validate that Secret/ConfigMap references resolve to an
allowlisted set, that URL schemes are `https`, and that resolved destinations
pass an egress allowlist rejecting private and link-local addresses after DNS
resolution. Requiring `https` alone is **not** sufficient SSRF protection.

### Schema in templateParameterSchema

The ClusterTemplate's `templateParameterSchema` would include the `seedGeneration` sub-schema under `upgradeParameters`, following the same pattern as `imageBasedGroupUpgrade`.

Two points about this schema, both tied to the security posture above:

- **Declaring a field here does not make it PR-overridable.** Because the
  design validates the **merged** object (defaults over PR input — option (a)
  above), the schema must describe the full `seedGeneration` shape, including
  the credential and outbound-target fields, or the merged document would fail
  validation. JSON Schema alone therefore cannot express "present in defaults
  but forbidden from PR input." Template-ownership of `seedAuthSecretRef`,
  `recertImage`, `liveISO.releaseImage`, `uploadSecretRef`, `urlBase`,
  `pullSecretRef`, `imageDigestSources`, and `additionalTrustBundleConfigMapRef`
  is instead enforced by an **admission check on the raw
  `spec.templateParameters`** that rejects those keys in PR input unless the
  deployment has explicitly opted them in. The schema does structural
  validation of the merged object; the admission check is what makes these
  fields effectively immutable from the PR side.
- **`additionalProperties: false` at every object level.** Unknown or
  mistyped keys are rejected rather than silently ignored, so a typo like
  `seedimage` or an attempt to smuggle an unrecognized field fails admission
  instead of being dropped.

```yaml
upgradeParameters:
  properties:
    seedGeneration:
      type: object
      additionalProperties: false
      properties:
        seedImage:
          type: string
          description: Full pull-spec for the seed container image to publish
          minLength: 1
          pattern: "^([a-z0-9]+://)?[\\S]+$"
        seedAuthSecretRef:
          type: object
          additionalProperties: false
          properties:
            name:
              type: string
          required: [name]
        recertImage:
          type: string
          description: >
            Override for the recert tool image used during seed generation.
            Template-owned (see the security posture and the admission check
            above): rejected as PR input, and when set it must be an
            allowlisted, immutable digest reference (repo@sha256:<digest>),
            because Phase 3 passes it to the SeedGenerator CR where it runs
            as executable code during seed generation.
        liveISO:
          type: object
          additionalProperties: false
          properties:
            releaseImage:
              type: string
              description: >
                OCP release image to extract openshift-install from.
                In disconnected environments, either the canonical release
                pull-spec (redirected by the hub-derived mirror mappings)
                or a direct mirror pull-spec.
              minLength: 1
            installationDisk:
              type: string
              description: Disk device path on the target servers
              minLength: 1
            sshKey:
              type: string
              description: SSH public key for debugging access to pre-provisioned servers
            uploadSecretRef:
              type: object
              additionalProperties: false
              properties:
                name:
                  type: string
              required: [name]
            urlBase:
              type: string
              description: >
                HTTPS origin plus base path from which the uploaded ISO is
                served, backed by the same directory tree the upload
                writes into. The effective per-run ISO URL is computed by
                the controller as urlBase + the per-run suffix and recorded
                in status; it is never supplied directly.
              minLength: 1
            pullSecretRef:
              type: object
              additionalProperties: false
              properties:
                name:
                  type: string
              required: [name]
            storageClass:
              type: string
              description: >
                StorageClass for the ISO build workspace PVC (~20GB).
                Uses the cluster default StorageClass if omitted.
            imageDigestSources:
              type: array
              description: >
                Mirror mappings for the ISO build. Auto-derived from the
                hub's ImageDigestMirrorSet/ImageContentSourcePolicy if
                omitted; set only to target a different mirror.
              items:
                type: object
                additionalProperties: false
                properties:
                  source:
                    type: string
                  mirrors:
                    type: array
                    items:
                      type: string
                required: [source, mirrors]
            additionalTrustBundleConfigMapRef:
              type: object
              additionalProperties: false
              description: >
                ConfigMap holding the registry CA trust bundle for the
                mirror. Auto-derived from the hub's image config
                additionalTrustedCA if omitted.
              properties:
                name:
                  type: string
              required: [name]
          required: [releaseImage, installationDisk, uploadSecretRef, urlBase]
      required: [seedImage, seedAuthSecretRef]
```

## Controller Workflow

### Functional Overview

At a functional level, the operator declares — in the cluster definition — that a
given cluster type should produce a seed image (and, optionally, a ready-to-use
installation ISO), and the platform performs the rest as a single, self-service
operation reported through the normal API:

1. **Declare intent** — the cluster definition names where to publish the seed
   image and, optionally, how to build and deliver the installation ISO.
2. **Build a reference cluster** — the platform provisions a normal cluster to
   serve as the template for the image.
3. **Sanitize it** — everything that ties the cluster to *this* management
   environment is removed, so the captured image is clean and portable and a
   clone will not accidentally re-register with the original hub.
4. **Capture the golden image** — the seed image is generated and published to
   the operator-named registry.
5. **Produce the installation media** — the bootable ISO is built from that image
   and uploaded to the server the field servers boot from, handling the
   disconnected-registry and certificate details automatically.
6. **Report and reclaim** — results (image location, ISO URL) are surfaced in the
   API; the reference cluster is treated as **disposable** and the operator tears
   it down to reclaim the hardware once the image is captured.

The value: a repeatable API operation replaces a manual, expert-only runbook;
progress and results are visible instead of requiring SSH-and-tail-logs;
prerequisite and credential problems fail fast before the long image build; and
air-gapped environments — the primary use case — are handled without extra
operator effort. The rest of this section describes how the controller realizes
that flow.

### Trigger and Dispatch

Seed generation reuses the upgrade config plumbing (`upgradeDefaults` / `upgradeParameters`, `parseUpgradeConfig`) but **not** the upgrade *trigger*. The existing `IsUpgradeRequested` returns true only when `ClusterTemplate.spec.release` is strictly greater than the spoke's
`openshiftVersion` label. Seed generation is different: a seed image is generated from a cluster that is already *at* the target release, so `spec.release == openshiftVersion` and `IsUpgradeRequested` would return false. Seed generation therefore needs its own trigger.

The trigger is the presence of the `seedGeneration` key in the merged upgrade config (PR `upgradeParameters` over CT `upgradeDefaults`) on a spoke that has reached ZTP Done with no pending upgrade.
**The trigger deliberately does not gate on `spec.release == openshiftVersion`.** Version equality is a Phase 1 *precondition* that is checked and (on any mismatch) fails terminally — it is not part of the predicate.
Gating the predicate on equality would leave an ahead-of-release spoke (`openshiftVersion > spec.release`) in a state where neither the seed predicate nor `IsUpgradeRequested` is true, stalling the request non-terminally with no progress. Concretely:

- `parseUpgradeConfig` is extended to recognize `seedGeneration` as a third config type (see Proposed API Changes) and return it as the detected type.
  Because a `seedGeneration` config is **not** an upgrade config, it is never a valid input to `handleUpgrade` — the reconciler must route it to the seed-generation state machine, not the upgrade switch (which has no seed-generation case and would otherwise return without action).
- The main reconciler checks `IsSeedGenerationRequested` **before** `IsUpgradeRequested` so a `seedGeneration` request is never misrouted into the upgrade path.
  A new predicate — `IsSeedGenerationRequested` (parallel to `IsUpgradeRequested`) — returns true when the merged upgrade config contains `seedGeneration` and the spoke has reached ZTP Done, **regardless of the version relationship**.
- **Any version mismatch is a terminal precondition failure, not a stall — fail closed in both directions.** Seed generation requires the cluster to already be *at* `spec.release`; it performs neither an upgrade nor a downgrade.
  Because the predicate fires on presence alone, a version-mismatched spoke
  reaches Phase 1, which requires `openshiftVersion == spec.release` and fails
  terminally with `PreconditionChecksFailed` and a direction-specific message:
  - `openshiftVersion < spec.release` (behind): "seed generation requires the cluster at spec.release; upgrade the cluster first, or configure an upgrade".
  - `openshiftVersion > spec.release` (ahead): "seed generation requires the cluster at spec.release; the cluster is ahead of the template release — align spec.release with the running version".
  Routing a `seedGeneration`-only config through `IsSeedGenerationRequested`
  rather than the upgrade switch (which has no seed case) thus guarantees the
  request always reaches a state that makes progress or fails closed, never one
  that returns without action. Both mismatch directions are covered by tests.
- The main reconciler dispatches to the seed-generation state machine when `IsSeedGenerationRequested` is true, mutually exclusively with the upgrade path.

#### One-shot semantics

Seed generation runs **at most once** per ProvisioningRequest. The trigger predicate above stays true after a successful run — `spec.release` still equals `openshiftVersion` and the `seedGeneration` key is still present — so dispatch is additionally gated on the `SeedGenerationCompleted` condition:

- **Absent** — the state machine starts.
- **`False` with a non-terminal reason** (`Validating`, `CleaningACMResources`, `InProgress`, `BuildingISO`) — the state machine resumes from its current phase.
- **Terminal** — `True / Completed`, or `False` with `Failed`, `TimedOut`, or `PreconditionChecksFailed` — the controller does **not** re-enter the state machine (see [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state)).

`PreconditionChecksFailed` is terminal: it is set by Phase 1 **before** any
ACM detachment, so the cluster is still ACM-attached and healthy. The
operator fixes the reported configuration and submits a new
ProvisioningRequest rather than retrying in place (consistent with the
one-shot model below). Because no detachment occurred, the terminal-state
ACM suppression described in [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state)
does not apply to this reason — normal reconciliation of the still-attached
cluster continues.

Transient errors inside an active phase (registry blips, spoke API timeouts,
an evicted Job pod) are handled by the normal reconcile requeue and retried
until the overall timeout; they do not set a terminal condition. A terminal
`Failed` / `TimedOut` / `PreconditionChecksFailed` is never retried
automatically.

#### Idempotent resource handling

Because a reconcile can be interrupted after a resource is created but before
its status is flushed, every create in the state machine is **create-or-adopt**,
not blind create. Deterministic names alone are not enough: a restart re-issuing
a `create` hits `AlreadyExists`, and a name collision with an unrelated object
(prior run or external actor) could silently reuse the wrong resource. For each
generated resource — the `seedgen` Secret, the ISO-build `Job`, its workspace
`PVC`, and the per-run mirror/CA `ConfigMap` — the controller:

- Stamps a **per-run ownership label** carrying the ProvisioningRequest name
  and UID at creation time.
- On `AlreadyExists`, **gets** the existing object and compares its ownership
  label/UID: on a match it **adopts** it (idempotent, reconcile continues); on
  a mismatch (no label, or a different UID) it fails closed rather than reusing
  an object it does not own (a stale object under the same deterministic name is
  deleted and recreated only when provably this PR's own residue).
- Hub-side resources additionally carry an `ownerReference` to the
  ProvisioningRequest so Kubernetes garbage-collects them if the PR is deleted
  mid-run.

Each create has restart-after-create test coverage: kill the reconcile
immediately after the create and assert the next reconcile adopts (not
duplicates, not `AlreadyExists`-fails) its own resource and rejects a same-named
foreign object.

#### Regenerating a seed

There is no in-place re-trigger. To produce a new seed — after a failure, or
with changed configuration — the operator submits a **new**
ProvisioningRequest, which provisions a fresh disposable cluster. This keeps
the workflow strictly one-shot and consistent with the disposable-factory
model: one ProvisioningRequest maps to one cluster and one seed generation
attempt.

Rather than silently ignoring a late edit, the validating webhook
(`provisioningrequest_webhook.go`) is **extended to reject** changes to
`upgradeParameters.seedGeneration` once `SeedGenerationCompleted` is terminal.
The existing webhook allows updates after provisioning completes but only
compares a disallowed-ClusterInstance-fields set, not `seedGeneration`, so
without this a changed `seedImage` or `urlBase` would be admitted and then
dropped by the one-shot dispatch gate — an accepted-but-ignored change that
misleads the operator. The webhook instead returns a validation error directing
the operator to submit a new ProvisioningRequest. (In-place regeneration, if
ever desired, would require an explicit generation-aware retry — e.g. bumping a
`seedGeneration.generation` counter the dispatch gate treats as a new attempt —
which is out of scope here.)

The spoke's `ManagedCluster` on the hub is left intact throughout — the
controller never deletes it, so there is no re-provisioning or ClusterInstance
recreation. What must be removed is the spoke-side ACM *agent state* (the
klusterlet and all addon agents), which would otherwise be captured in the seed
and cause a clone to register with the original hub on first boot. Because
klusterlet teardown deletes the agent namespaces (`open-cluster-management-agent*`)
where the `ManagedServiceAccount` token lives, the controller cannot rely on that
token; instead it drives generation with the spoke's hub-held admin kubeconfig
(see Phase 2 and [Challenge 6](#6-spoke-access-after-acm-removal)). The seed
cluster is a disposable factory, so ACM is **not** restored: cleanup removes the
transient artifacts, a terminal `SeedGenerationCompleted` condition is set, and
the operator deletes the ProvisioningRequest to reclaim the hardware. Note that
lca-cli's own cleanup does **not** strip klusterlet/registration identity, so the
controller must remove it explicitly rather than defer to LCA's seed preparation.

The workflow is a multi-phase state machine tracked via a new `SeedGenerationCompleted` condition and `SeedGenerationStatus` in the PR status.

### Phase 1: Validation

- Verify `seedGeneration` configuration is present and valid
- Verify the `seedAuthSecretRef` Secret exists in the ClusterTemplate namespace and contains the `seedAuth` key
- **Validate registry access**: Decode the `seedAuth` credentials and perform a token exchange against the target registry (extracted from the `seedImage` pull-spec) to confirm the credentials are valid and have push access. This catches misconfigured credentials or unreachable
  registries before the lengthy seed generation process begins. Report a `PreconditionChecksFailed` condition with a specific message on failure (e.g., "registry authentication failed for quay.io/my-org/seed-image", "registry quay.io is not reachable").
- Verify the spoke cluster is fully provisioned (ZTP Done)
- Verify the spoke has the `var-lib-containers` partition (check for the MachineConfig via spoke client)
- Verify the LCA operator is installed and Available on the spoke (check the `openshift-lifecycle-agent` namespace for the LCA operator Deployment via spoke client)
- Verify the OADP operator is installed on the spoke
- If `liveISO` is configured:
  - Verify the `uploadSecretRef` Secret exists and contains the required keys (`host`, `username`, `privateKey`, `remotePath`, `knownHosts`). A missing or empty `knownHosts` is a `PreconditionChecksFailed` — the upload fails closed rather than falling back to trust-on-first-use
  - If `caCert` is present in the upload Secret, validate that it is a well-formed PEM certificate bundle
  - Verify HTTPS connectivity to the ISO server: perform a TLS handshake against the `urlBase` host using the `caCert` from the upload Secret (or the system trust store if `caCert` is not provided). Report `PreconditionChecksFailed` if the certificate cannot be verified — this catches
    CA misconfigurations before the ISO build
  - Resolve the mirror configuration (hub-derived `ImageDigestMirrorSet`/`ImageContentSourcePolicy` mappings and `additionalTrustedCA` trust bundle, or the `liveISO` overrides) and verify the `releaseImage` is reachable and credentials are valid using it (attempt
    `oc adm release info <releaseImage>` equivalent)
  - Verify the `releaseImage` version reported by `oc adm release info` matches `ClusterTemplate.spec.release`. The ISO's `seedVersion` and the extracted `openshift-install` must be the same OCP version as the seed; report `PreconditionChecksFailed` on mismatch (e.g., "releaseImage is
    4.Y.Z but spec.release is 4.Y.W")
  - **Resolve and pin the `releaseImage` digest.** `releaseImage` is often a
    mutable tag (e.g. `...:4.Y.Z-x86_64`). Resolve it to an immutable
    `repo@sha256:<digest>` from the same `oc adm release info` call and persist
    it in `SeedGenerationStatus.ReleaseImageDigest`, so Phase 4 extracts
    `openshift-install` from exactly the release validated here rather than
    re-resolving a tag that could have moved. Persisting to status (not an
    in-memory value) is required so the pin survives a controller restart
    between Phase 1 and Phase 4. Report `PreconditionChecksFailed` if the digest
    cannot be resolved. In disconnected environments there is a subtlety:
    `IDMS`/`ICSP` redirect **by-digest** specs only, not tags, so a mutable-tag
    `releaseImage` is not redirected to the mirror by the hub's
    `ImageDigestMirrorSet`/`ImageContentSourcePolicy`. Phase 1 therefore requires
    one of: a by-digest `releaseImage`, a direct mirror pull-spec, or a hub
    `ImageTagMirrorSet` covering the tag; a mutable tag with no tag-mirror or
    direct-mirror path fails closed with `PreconditionChecksFailed` rather than
    silently reaching the (unreachable) upstream registry
  - **An `ImageTagMirrorSet`-only resolution must be translated into a
    by-digest mapping for Phase 4.** When the tag was reachable *only* through
    an `ImageTagMirrorSet` (no by-digest `releaseImage`, direct mirror pull-spec,
    or covering `IDMS`/`ICSP`), the digest Phase 1 persists to
    `ReleaseImageDigest` is a `repo@sha256:<digest>` on the *source* registry,
    and Phase 4 passes it to `oc adm release extract --idms-file`, which honors
    only `IDMS`/`ICSP`. Because tag mirrors do **not** redirect digest
    references, Phase 4 would then fail despite Phase 1 passing. Phase 1
    therefore does not accept ITMS coverage alone: it resolves the digest
    **through the mirror** and records an equivalent by-digest mapping (source
    repo → mirror repo, keyed on the resolved digest) emitted into the generated
    `idms.yaml`. If no such mapping can be derived (the mirror repo does not hold
    the resolved digest), resolution fails closed with `PreconditionChecksFailed`
    rather than deferring the failure to Phase 4
  - **Validate the effective ISO-build pull secret against both images.** The
    ISO-build pull secret is not the same credential as `seedAuthSecretRef`
    (which only needs push access to the seed registry). Resolve it exactly as
    Phase 4 will — `liveISO.pullSecretRef` if provided, else the hub pull secret
    (`openshift-config/pull-secret`) merged with the `seedAuthSecretRef`
    credentials — and confirm it can authenticate to **both** the `seedImage`
    and `releaseImage` registries (after mirror redirection). A mere-existence
    check is insufficient: a `pullSecretRef` that reaches the release mirror but
    lacks credentials for the seed mirror (or vice versa) passes it and then
    fails deep in the ISO build. Report `PreconditionChecksFailed` naming the
    registry that rejected the credentials.
- **Condition**: `SeedGenerationCompleted = False / Reason: Validating`

### Phase 2: ACM Agent Cleanup

Remove all ACM agent state from the spoke so it doesn't contaminate the seed image. The `ManagedCluster` on the hub is left intact.

1. **Retrieve the spoke admin kubeconfig from the hub**: Before any ACM
   removal, read the spoke's admin kubeconfig from the hub — the Secret named by
   the spoke `ClusterDeployment`'s
   `spec.clusterMetadata.adminKubeconfigSecretRef` in the cluster's namespace —
   and build the spoke client from it. This kubeconfig authenticates with an
   embedded `system:admin` **client certificate**, not a ServiceAccount token,
   which is what makes it suitable (see
   [Challenge 6](#6-spoke-access-after-acm-removal) for the full rationale): it
   **survives ACM teardown** (the cert is independent of the klusterlet and the
   `ManagedServiceAccount` token in the `open-cluster-management-agent*`
   namespaces, breaking only on API-server cert rotation, which happens at
   *restore* on the target, not generation), **writes nothing to the spoke's
   etcd** (no ServiceAccount/token/RBAC is created, so no new credential is
   captured into the seed — the only object placed on the spoke is the transient
   Phase 3 `seedgen` Secret), and **needs no revocation** (nothing is minted;
   Phase 5 spoke cleanup is limited to the `seedgen` Secret and `SeedGenerator`
   CR). The trade-off — a broad `cluster-admin` credential rather than a
   least-privilege one — is accepted because it is used only during this
   controlled one-shot operation, never persisted or copied to the spoke, and
   never baked into the seed.

   **Precondition.** If the admin-kubeconfig Secret is absent, malformed, or the
   resulting client cannot reach the spoke API server, Phase 2 fails with
   `PreconditionChecksFailed` **before** any destructive ACM removal, so the
   spoke is never left partially detached without a working access path. This
   needs only a hub-side `get` on the admin-kubeconfig Secret in the cluster's
   namespace (see the hub RBAC contract) — no spoke-side RBAC at all.
2. **Record detachment intent**: Persist
   `SeedGenerationStatus.DetachmentStarted = true` (and flush the status
   update) **before** step 3 performs the first destructive ACM removal. This
   marker must be durable before, not after, any irreversible teardown so that
   a controller restart mid-detachment still treats the spoke as (partially)
   detached — see [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state).
3. **Delete hub-side addon CRs**: Delete all `ManagedClusterAddOn` resources from the spoke's namespace on the hub (including `managed-serviceaccount`). This tells the ACM addon framework to stop reconciling them.
4. **Delete the klusterlet**: Delete the `Klusterlet` CR on the spoke (via the admin-kubeconfig client). The klusterlet operator tears down the registration and work agents and their namespaces (`open-cluster-management-agent*`) — including the identity secrets
   (`bootstrap-hub-kubeconfig`, `hub-kubeconfig-secret`) that would otherwise be captured in the seed. Deleting only the klusterlet Deployment is **not** sufficient: those secrets persist in etcd and on disk.
5. **Wait for teardown**: Poll the spoke until the klusterlet and addon agent namespaces/pods are gone.

- **Condition**: `SeedGenerationCompleted = False / Reason: CleaningACMResources`

### Phase 3: Trigger Seed Generation

Using the admin-kubeconfig spoke client established in Phase 2:

1. **Create the seedgen Secret**: Deliver the `seedgen` Secret to the `openshift-lifecycle-agent` namespace on the spoke, reading credentials from the hub Secret referenced by `seedAuthSecretRef`.
2. **Create the SeedGenerator CR**: Apply the singleton `seedimage` SeedGenerator CR with the configured `seedImage` (and optionally `recertImage`).
3. **Monitor progress**: Poll the SeedGenerator CR status conditions. The LCA orchestrates the full seed image lifecycle:
   - System validation
   - Cluster operator shutdown
   - Seed image generation via lca-cli (`lca_image_builder` container)
   - Image push to registry
   - Cluster operator recovery
4. **Capture the pushed digest**: On completion, record the pushed seed image
   digest in `SeedGenerationStatus.SeedImage` as an immutable
   `<repository>@sha256:<digest>` reference. The configured
   `seedGeneration.seedImage` is a **mutable tag** that could be overwritten
   between this push and the Phase 4 ISO build, so all downstream consumption
   pins the digest rather than re-resolving the tag. The digest is taken
   **only** from the authoritative output of the push itself — the digest the
   SeedGenerator/LCA reports for the image it just pushed, produced atomically
   with the push and thus provably this run's image. The controller does **not**
   fall back to a post-hoc registry tag lookup (manifest `HEAD`/`GET`): resolving
   the tag after the fact cannot prove the returned image is this run's (another
   writer can move the tag before or during the lookup), defeating the very
   integrity guarantee the pin exists for. If the SeedGenerator/LCA surfaces no
   immutable digest, the workflow **fails closed** with a terminal `Failed`
   rather than building an ISO from an unverifiable tag.

- **Condition**: `SeedGenerationCompleted = False / Reason: InProgress`
- **Status message**: Reflects the SeedGenerator's condition messages (e.g., `SeedGenInProgress=True` → "Generating seed image", `SeedGenCompleted=True` → generation finished)

### Phase 4: Live ISO Generation

Live ISO generation is a first-class, fully automated part of the workflow. This phase is *conditional on configuration*: it runs whenever the `liveISO` block is present under `seedGeneration`. A template that only needs to publish a seed image (building ISOs elsewhere) may omit
`liveISO`, in which case the workflow skips directly to Phase 5.

The controller creates a Kubernetes Job on the hub cluster that builds the live installation ISO and uploads it to the configured HTTPS server. The Job runs in the O-Cloud Manager namespace.

**Job workflow:**

1. **Resolve disconnected mirror configuration**: In a disconnected/mirrored environment the Job needs the same mirror mappings and registry trust the hub already uses. Rather than duplicating this in the ClusterTemplate, the controller **auto-derives it from the hub** by default:
   - **Mirror mappings**: Read the hub's `ImageDigestMirrorSet` resources (field `spec.imageDigestMirrors`)
     and, if present, legacy `ImageContentSourcePolicy` resources (field `spec.repositoryDigestMirrors` —
     **not** `imageDigestMirrors`, which ICSP does not define). Translate their source → mirrors entries into
     both IBI `imageDigestSources` and an equivalent `idms.yaml` passed to `oc adm release extract --idms-file`.
     Reading the wrong field for ICSP yields empty mappings and silently breaks disconnected release extraction, so a dedicated test covers the ICSP-only path.
     **Preserve `mirrorSourcePolicy`.** When an entry sets `mirrorSourcePolicy: NeverContactSource`, copy that into the generated `idms.yaml` alongside its `source` and `mirrors`; if dropped,
     `oc adm release extract` may fall back to the source registry after a mirror miss, causing upstream egress on a connected hub or an extraction failure on a disconnected one. A dedicated test covers this case.
   - **Registry trust**: Read the ConfigMap named by `image.config.openshift.io/cluster` `spec.additionalTrustedCA` (in `openshift-config`) and concatenate its CA values into a single PEM bundle, used as the IBI `additionalTrustBundle` and mounted into the Job pod's trust store so
     `oc` can reach the mirror.

   Optional overrides let a template author point the ISO build at a mirror that differs from the hub's: `liveISO.imageDigestSources` and `liveISO.additionalTrustBundleConfigMapRef`, when set, **replace** the corresponding hub-derived values. On a connected hub with no mirror set and
   no overrides, nothing is injected and images resolve from their canonical registries.

2. **Extract `openshift-install`**: Run `oc adm release extract --command=openshift-install --idms-file=<derived idms.yaml> <pinned releaseImage digest>` to obtain the binary matching the OCP version from `ClusterTemplate.spec.release`, using the derived registry trust. This uses the **immutable
   `repo@sha256:<digest>` reference read from `SeedGenerationStatus.ReleaseImageDigest`** (resolved and persisted in Phase 1), not the configured tag, so the installer is extracted from exactly the release that was validated.
   If that field is empty when `liveISO` is configured (e.g. status was lost), the Job fails terminally rather than re-resolving the tag. The `releaseImage` field is
   required; in disconnected environments it may be given either as the canonical release pull-spec (redirected to the mirror by the derived mappings) or as a direct mirror pull-spec. There is no default public release image fallback.

3. **Generate `ImageBasedInstallationConfig`**: Build the configuration from the `liveISO` parameters and the resolved mirror config:

   ```yaml
   apiVersion: v1beta1
   kind: ImageBasedInstallationConfig
   metadata:
     name: ibi-config
   seedImage: <SeedGenerationStatus.SeedImage>   # immutable repo@sha256:... digest, not the mutable tag
   seedVersion: <ClusterTemplate.spec.release>
   installationDisk: <liveISO.installationDisk>
   sshKey: <liveISO.sshKey>       # if provided
   pullSecret: <merged pull secret>
   imageDigestSources:           # hub-derived (or liveISO override); omitted when empty
     - source: registry.redhat.io/...
       mirrors:
         - my-mirror.example.com/...
   additionalTrustBundle: |      # hub-derived (or liveISO override); omitted when empty
     -----BEGIN CERTIFICATE-----
     ...
   ```

   The pull secret is constructed by merging the hub cluster pull secret (`openshift-config/pull-secret`) with the `seedAuthSecretRef` credentials. If `liveISO.pullSecretRef` is provided, its contents are used directly instead of the auto-merged secret. In disconnected environments,
   the pull secret must include credentials for both the local seed image mirror and the OCP release image mirror.

4. **Build the ISO**: Run `openshift-install image-based create image --dir <workdir>`. This pulls the seed image, generates ignition configuration, and produces the `rhcos-ibi.iso` file. This step requires significant ephemeral storage (~15GB for seed image pull + ISO generation).

5. **Upload the ISO**: SCP the ISO to the HTTPS server using the SSH credentials from `uploadSecretRef`, pinning the server host key from `knownHosts` with strict checking:

   ```bash
   scp -i <privateKey> \
       -o StrictHostKeyChecking=yes \
       -o UserKnownHostsFile=<knownHosts from uploadSecretRef> \
       rhcos-ibi.iso <username>@<host>:<remotePath>
   ```

   Strict host-key checking is mandatory and there is no
   trust-on-first-use fallback: if the server's key is not present in
   `knownHosts` or does not match, the upload fails closed. This prevents
   a man-in-the-middle from impersonating the ISO server and capturing the
   mounted SSH private key while a bogus (or malicious) ISO is served to
   the BMCs.

   The upload uses SSH (encrypted in transit). The Job computes the ISO's
   SHA-256 digest locally before upload. After upload the controller verifies
   the served artifact is exactly the one it built, not merely that *some* file
   is reachable — a HEAD/`Content-Length` check confirms reachability and TLS
   trust but cannot detect a stale ISO, a truncated upload with a matching size,
   or a same-size substitution:
   - **Unique per-run remote path.** Uploads target the `remotePath` from
     `uploadSecretRef` disambiguated by the ProvisioningRequest name and seed
     image digest, so a new build never overwrites or is confused with an older
     ISO at a shared URL.
   - **Mandatory SSH content identity.** The controller fetches the digest over
     the already-trusted SSH channel (`sha256sum` on the remote `remotePath`)
     and compares it to the local digest; a mismatch or an inability to obtain
     the remote digest is a terminal `Failed`. SSH gives an authenticated,
     integrity-checked channel to the exact file just written, so this check is
     always available without the server exposing anything extra.
   - **Deterministic `remotePath` → ISO-URL binding.** The upload endpoint
     (`uploadSecretRef.host` + `remotePath`) and the HTTPS origin
     (`liveISO.urlBase`) must be two views of the **same backing store**.
     `liveISO.urlBase` is the only URL input; both the SSH target and the
     effective ISO URL are computed from it plus the per-run suffix
     (ProvisioningRequest name + seed image digest + filename), so no
     independently supplied full URL can drift from `remotePath`. Before
     recording `ISOURL` the controller validates the binding on two axes:
     - **Path.** The path component of the computed ISO URL must equal the
       served-root-relative form of `remotePath`; a mismatch is a terminal
       `Failed` (SSH would otherwise verify one file while status advertises
       another).
     - **Host** — because `uploadSecretRef.host` and the `urlBase` host are
       independent inputs, matching paths alone do not prove the same backing
       store (SSH to host A, HTTPS on host B sharing a path would pass a
       path-only check). Either the **same-host fast path** (identical host or
       an explicit alias to one store — the mandatory SSH digest check already
       covers the served bytes), or, when the same-store relationship cannot be
       proven, **mandatory HTTPS content-digest verification**: the controller
       fetches the artifact over HTTPS from the computed URL (using the
       `urlBase` `caCert`), hashes it, and requires equality with the local
       digest. Here the HTTPS check is **not** optional and a HEAD/sidecar is
       no substitute — the actual served bytes must be hashed; a mismatch or
       unreachable URL is a terminal `Failed`.
     `ISOURL` is recorded **only after** the path binding, the host binding, and
     the mandatory SSH digest verification all pass.
   - Optionally, if the server publishes a companion `.sha256` sidecar, an HTTPS
     `GET` of that small file (using the `caCert`) confirms the publicly served
     copy without re-downloading the multi-GB ISO; when present it must match,
     when absent the mandatory SSH-side verification still stands. A plain HEAD
     is only a "served over HTTPS" liveness probe, never the integrity check.

   The verified SHA-256 digest and the resolved per-run effective ISO URL are recorded
   in status (`ISODigest`, `ISOURL`) so downstream consumers can pin the
   exact artifact.

**Job pod spec considerations:**

- The Job image uses `oc` (available from the OCP CLI tools image) and `openshift-install` (extracted at runtime)
- **PVC for build workspace**: The controller creates a dedicated PVC (~20GB), mounted into the Job pod as the working directory, because `openshift-install` pulls the full seed image and generates an ISO, exceeding typical ephemeral storage limits. It is created before the Job and deleted
  during Phase 5 after successful upload. Its name is derived from the ProvisioningRequest name (e.g. `<pr-name>-iso-build`); the StorageClass comes from the optional `liveISO.storageClass` field, or the cluster default if omitted.
- **Per-run credential copies in the Job namespace.** A pod can only mount
  Secrets from its own namespace, but the sources live elsewhere:
  `seedAuthSecretRef` resolves in the **ClusterTemplate** namespace and the hub
  pull secret is `openshift-config/pull-secret`, while the Job runs in the
  **O-Cloud Manager** namespace. Before creating the Job, the controller copies
  the credentials it needs into **short-lived, per-run Secrets in the Job
  namespace** (named deterministically from the ProvisioningRequest) that the
  Job references. `uploadSecretRef` and the optional `pullSecretRef` are
  name-only references that **always resolve in the ClusterTemplate namespace**
  (never the PR or Job namespace — a name-only ref cannot select a Secret the
  caller controls elsewhere) and are copied the same way. These per-run copies
  are deleted on **every** terminal path (success and failure — see Phase 5),
  so credentials are not left lingering in the Job namespace.
- The Job mounts: the build workspace PVC, the per-run copies of
  `seedAuthSecretRef` (registry auth) and the merged/`pullSecretRef` pull
  secret, `uploadSecretRef` (SSH credentials), and the resolved mirror config
  (derived `idms.yaml` and CA trust bundle) as a ConfigMap the controller
  generates per-run
- `networkConfig` is intentionally excluded from the ISO configuration — network and hardware-specific configs are applied later through the IBI templates provided by the SiteConfig Operator
- The Job's active-deadline is bounded by the remaining portion of the overall operation timeout (the `clusterUpgradeTimeout` override, or the with-ISO default of 3h — see [Timeout](#timeout)), not a separate per-Job budget
- On Job failure, the controller surfaces a **bounded, redacted** summary of
  the pod logs in the condition message. Raw pod logs must not be written
  verbatim into `metav1.Condition.message`: that field is capped at 32768
  bytes (the API server rejects longer values, failing the status update and
  stalling the state machine), and the ISO-build environment has registry pull
  secrets, SSH credentials, and CA material mounted, so unfiltered logs risk
  leaking secrets into the PR status. The controller extracts a short tail (a
  fixed byte budget well under the limit) with known credential patterns
  redacted for the message. For deeper debugging it **deletes the original
  log-bearing artifacts** (the failed pod and its raw logs are not kept) and
  retains only a **redacted** copy of the pod logs under a diagnostic-retention
  **TTL** (see Phase 5), which the condition message points the operator at.

**Hub RBAC contract for the controller.** The operator's hub service account
needs an explicit set of permissions in the O-Cloud Manager namespace beyond
the ACM/read permissions already listed; if any are missing the workflow
fails with `Forbidden` or leaks artifacts. The controller therefore requires,
in its own namespace:

- `batch/jobs`: `create`, `get`, `list`, `watch`, `delete` (create and
  monitor the ISO-build Job; delete it in Phase 5).
- `core/persistentvolumeclaims`: `create`, `get`, `delete` (the ~20GB build
  workspace PVC).
- `core/secrets`: `create`, `get`, `delete` (per-run credential copies) and
  `get` on the source Secrets in the ClusterTemplate namespace,
  `openshift-config/pull-secret`, and the spoke's admin-kubeconfig Secret
  (named by the `ClusterDeployment`'s `adminKubeconfigSecretRef`) in the
  cluster's namespace — the credential Phase 2 uses to reach the spoke. This
  `get` is the **only** access the workflow needs to obtain spoke API access;
  no spoke-side RBAC is created.
- `core/configmaps`: `create`, `get`, `delete` (the per-run mirror/CA
  ConfigMap), `get` on the hub image-config `additionalTrustedCA`, and `get` on
  the ConfigMap named by `liveISO.additionalTrustBundleConfigMapRef` in the
  ClusterTemplate namespace. That override **replaces** the hub-derived trust
  bundle (see Phase 4 step 1); without a namespaced `get` for it the override
  fails with `Forbidden` and stops ISO generation.
- `core/pods` and `core/pods/log`: `get`, `list` (read the build pod and its
  logs for the redacted failure summary).
- `config.openshift.io` `images` / `ImageDigestMirrorSet` /
  `ImageTagMirrorSet`: `get`, `list` (derive mirror config).
- `operator.openshift.io` `ImageContentSourcePolicy`: `get`, `list`. **ICSP
  lives in a different API group** (`operator.openshift.io`) than IDMS/ITMS
  (`config.openshift.io`), so a single `config.openshift.io` grant cannot
  authorize reading ICSP and the ICSP-only mirror-resolution path would fail
  with `Forbidden` without this separate entry. An authorization test covers it.
- `addon.open-cluster-management.io` `ManagedClusterAddOn`: `get`, `list`,
  `delete`, scoped to the managed-cluster namespace. Phase 2 deletes all such
  CRs there (including `managed-serviceaccount`); a read-only grant would fail
  the delete with `Forbidden` **after** `DetachmentStarted` was persisted,
  leaving the spoke half-detached, so `list`/`delete` (with the namespace scope)
  must be granted before that marker is written.

These are enumerated so the manually-maintained controller ClusterRole (see
the repository RBAC conventions) can be updated in one place; every artifact
created here is deleted on both the success and failure terminal paths
(Phase 5).

**Monitoring:**

The controller watches the Job status. On completion:

- **Success**: Record the resolved per-run effective ISO URL in `SeedGenerationStatus.ISOURL` and the verified ISO SHA-256 digest in `SeedGenerationStatus.ISODigest`
- **Failure**: Set `SeedGenerationCompleted = False / Reason: Failed` with the Job failure message. The cluster is left detached (no ACM restore); cleanup of workflow artifacts still proceeds (see Phase 5 for credential-artifact scrubbing).

- **Condition**: `SeedGenerationCompleted = False / Reason: BuildingISO`

### Phase 5: Completion and Cleanup

The seed cluster is a disposable factory, so there is no ACM restoration. Once the seed (and ISO, if configured) is confirmed, the workflow verifies success, cleans up its own artifacts, and reaches a terminal state.

On success (seed generation complete, and ISO generation complete if configured):

1. **Record results**: Store the seed image digest reference in `SeedGenerationStatus.SeedImage` and, if ISO was built, the resolved per-run URL in `SeedGenerationStatus.ISOURL` and the verified digest in `SeedGenerationStatus.ISODigest`.
2. **Clean up workflow artifacts**: On the spoke, delete the SeedGenerator CR and the transient `seedgen` Secret (using the admin-kubeconfig client from Phase 2). No standalone credential was minted, so there is nothing else to
   revoke on the spoke — the admin kubeconfig is a hub Secret the controller only read and never copied to the spoke.
   Delete the ISO generation Job, its build workspace PVC, the per-run credential Secret copies created in the Job namespace, and associated resources from the hub.
3. **Update conditions**: `SeedGenerationCompleted = True / Reason: Completed`. This is a **terminal** condition — the controller does not re-run seed generation, and it suppresses the PR's ACM-dependent reconciliation (see [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state)).

The seed cluster is left detached (ACM agents removed) and running. The operator deletes the ProvisioningRequest when ready, which tears the cluster down via the existing deletion path and reclaims the hardware.

On failure (at any phase):

1. **Report the error**: Capture the failure from whichever phase failed (SeedGenerator conditions, Job status, etc.).
2. **Condition**: The reason depends on **where** the failure occurred, and
   this distinction is load-bearing (it is what tells the terminal-state
   handler whether the cluster is still ACM-attached). A **Phase 1**
   validation/precondition failure — which happens before any ACM detachment,
   with the cluster still attached and healthy — sets
   `SeedGenerationCompleted = False / Reason: PreconditionChecksFailed`. A
   failure **at or after Phase 2** (once generation has begun and the spoke is
   being detached) sets `SeedGenerationCompleted = False / Reason: Failed`.
   This mirrors the attached-vs-detached distinction gated on
   `DetachmentStarted` (see [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state)):
   `PreconditionChecksFailed` leaves the still-attached cluster reconciling
   normally, while `Failed` marks a detached cluster whose ACM-dependent
   checks are suppressed.
3. **Scrub credential-bearing artifacts immediately, before any diagnostic
   retention.** The failure path must not leave secrets sitting on hub
   storage or the spoke. On both hub and spoke the controller, on failure:
   - **Hub**: deletes **every** per-run credential copy created in the Job
     namespace — the `seedAuthSecretRef` copy, the pull-secret copy, and the
     per-run `uploadSecretRef` copy (which holds the SSH **private key** and
     `knownHosts`, the most sensitive of the three) — plus the `idms.yaml`/CA-trust
     ConfigMap and any Secret projections mounted into the Job pod, and deletes
     the pod's mounted volumes, all on the same immediate failure path. Every
     per-run Secret the controller generates is covered by this scrub and by a
     test. Only *scrubbed* diagnostics are retained: the bounded, redacted log
     tail (already in the condition) and, if kept, credential-redacted pod logs.
     The build workspace PVC (which may hold the seed image / partial ISO but not
     the input Secrets) may be retained under a **bounded diagnostic-retention
     policy** (a TTL after which it is reclaimed), not indefinitely until the PR
     is deleted.
   - **Spoke**: deletes the transient `seedgen` Secret and the SeedGenerator
     CR. Nothing was minted, so there is no spoke-side ServiceAccount, token, or
     RBAC to revoke — the admin kubeconfig is a hub Secret the controller only
     read, never copied to the spoke, and nothing about it is baked into the
     seed. The only sensitive item placed on the spoke is the `seedgen` Secret
     (registry push credentials); a hub-side finalizer plus a bounded operation
     **deadline** force-run this cleanup even for a stuck or abandoned run while
     the spoke is still reachable. If the spoke is genuinely unreachable at
     cleanup time (e.g. the controller was down and the spoke was independently
     torn down), the `seedgen` Secret is the item to purge manually.
4. The spoke cluster remains running (detached) for manual intervention. ACM
   is **not** restored automatically; the operator can re-import the cluster
   manually for debugging or delete the ProvisioningRequest to tear it down.
   If detachment has begun at or after Phase 2
   (`SeedGenerationStatus.DetachmentStarted == true`; see [Challenge 4](#4-seed-cluster-afterlife-and-pr-terminal-state)),
   the controller suppresses ACM-dependent reconciliation for this terminal
   failure exactly as it does on success; a pre-detachment failure leaves the
   still-attached cluster reconciling normally. Remaining scrubbed
   diagnostics are removed on PR deletion (finalizer) or when the bounded
   retention TTL elapses, whichever comes first.

## New Status Fields

```go
// In provisioningrequest_types.go, inside ClusterDetails:
type ClusterDetails struct {
    // ... existing fields ...

    // SeedGenerationStatus holds the state of a seed image generation in progress.
    SeedGenerationStatus *SeedGenerationStatus `json:"seedGenerationStatus,omitempty"`
}

type SeedGenerationStatus struct {
    // StartedAt indicates when seed generation started.
    StartedAt *metav1.Time `json:"startedAt,omitempty"`

    // DetachmentStarted is set to true and flushed to status immediately
    // BEFORE Phase 2 performs the first destructive ACM removal (deleting the
    // ManagedClusterAddOn CRs, which precedes Klusterlet deletion) — before the
    // point of no return, not after teardown finishes. This is deliberate: if
    // that deletion succeeds but the teardown poll times out before status is
    // updated, an "after Phase 2 completes" marker would still read false and
    // ACM-dependent reconciliation would resume against a spoke that is in fact
    // (partially) detached. Gating suppression on DetachmentStarted instead
    // means any terminal outcome reached once destruction has begun suppresses
    // ACM-dependent checks; a pre-Phase-2 terminal failure (e.g.
    // PreconditionChecksFailed) leaves this false, so the still-attached
    // cluster reconciles normally. See Challenge 4.
    DetachmentStarted bool `json:"detachmentStarted,omitempty"`

    // ReleaseImageDigest is the immutable <repository>@sha256:<digest>
    // resolved from the (often mutable) liveISO.releaseImage tag during
    // Phase 1 validation. It is persisted here so that Phase 4 — which may
    // run in a later reconcile, after a controller restart — extracts
    // openshift-install from exactly the release validated in Phase 1
    // rather than re-resolving a tag that could have moved in between.
    // Phase 4 fails terminally if this is empty when liveISO is configured.
    ReleaseImageDigest string `json:"releaseImageDigest,omitempty"`

    // SeedImage is the published seed image as an immutable
    // <repository>@sha256:<digest> reference, captured from the
    // SeedGenerator status in Phase 3 and populated on success. The digest
    // (not the mutable configured tag) is what Phase 4 builds the ISO from
    // and what downstream consumers pin to.
    SeedImage string `json:"seedImage,omitempty"`

    // ISOURL is the HTTPS URL where the live installation ISO is accessible
    // from the BMC management network, populated on successful ISO build.
    // This is the resolved per-run URL (unique per run), computed from
    // liveISO.urlBase plus the per-run suffix.
    ISOURL string `json:"isoURL,omitempty"`

    // ISODigest is the SHA-256 digest of the built ISO
    // ("sha256:<hex>"), computed locally before upload and verified against
    // the uploaded copy over SSH (mandatory). Populated on successful ISO
    // build so downstream consumers can pin the exact artifact addressed by
    // ISOURL rather than trusting a mutable URL.
    ISODigest string `json:"isoDigest,omitempty"`

    // ISOServerCACertRef is a typed reference to a ConfigMap the controller
    // materializes on completion (in the O-Cloud Manager namespace, named
    // deterministically from the ProvisioningRequest) holding the PEM CA bundle
    // for the HTTPS ISO server under a well-known key. Populated only when
    // uploadSecretRef.caCert is present. The controller creates the ConfigMap
    // rather than storing raw PEM here, so downstream consumers (e.g. BMC
    // virtual media configuration) get a stable object reference, not inline
    // certificate data. Because ProvisioningRequest is cluster-scoped and this
    // ConfigMap is namespaced, GC cannot delete it via an ownerReference (a
    // namespaced dependent may not have a cluster-scoped owner); cleanup is
    // therefore explicit — handleFinalizer deletes it by the recorded name at
    // teardown. It is not credential-bearing, so it is removed on normal
    // teardown rather than in the immediate Phase 5 credential-scrub path.
    ISOServerCACertRef *ConfigMapKeyRef `json:"isoServerCACertRef,omitempty"`
}

// ConfigMapKeyRef references a single key in a ConfigMap.
type ConfigMapKeyRef struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    Key       string `json:"key"`
}
```

## New Condition Type

```go
// In conditions.go
PRconditionTypes.SeedGenerationCompleted = ConditionType("SeedGenerationCompleted")
```

With reasons: `Validating`, `PreconditionChecksFailed`, `CleaningACMResources`, `InProgress`, `BuildingISO`, `Completed`, `Failed`, `TimedOut`. The reasons not already present in `CRconditionReasons` (`Validating`, `CleaningACMResources`, `BuildingISO`) must be added to that set in
`conditions.go`; `Completed`, `Failed`, `InProgress`, `PreconditionChecksFailed`, and `TimedOut` already exist.

## Timeout

The existing `clusterUpgradeTimeout` config key (`ClusterUpgradeTimeoutConfigKey`) is reused, but seed generation gets its **own default constants** rather than the upgrade defaults. For reference, the existing upgrade defaults are `DefaultClusterUpgradeTimeout = 4h` and
`DefaultClusterEUSUpgradeTimeout = 8h` (`constants.go`); these do not apply to seed generation. The new seed-generation defaults are **2 hours** without `liveISO` and **3 hours** with `liveISO` (ISO generation adds ~30-60 minutes for seed image pull, ISO build, and upload). When the
`clusterUpgradeTimeout` key is set in `upgradeDefaults` or `upgradeParameters`, it overrides these defaults.

## Sample: End-to-End ClusterTemplate

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: ClusterTemplate
metadata:
  name: sno-ran-du-seedgen.v4-Y-Z-1
  namespace: sno-ran-du-v4-Y-Z
spec:
  name: sno-ran-du-seedgen
  version: v4-Y-Z-1
  release: 4.Y.Z
  templateDefaults:
    hwMgmtDefaults:
      nodeGroupData:
        - name: controller
          role: master
    clusterInstanceDefaults: clusterinstance-defaults-v1  # includes var-lib-containers MC
    policyTemplateDefaults: policytemplate-defaults-v1    # includes LCA + OADP operators
    upgradeDefaults:
      seedGeneration:
        seedImage: my-mirror.example.com/seed-repo/seed-image:4.Y.Z
        seedAuthSecretRef:
          name: seed-registry-credentials
        liveISO:
          releaseImage: my-mirror.example.com/ocp-release:4.Y.Z-x86_64
          installationDisk: /dev/disk/by-path/pci-0000:43:00.0-nvme-1
          sshKey: "ssh-rsa AAAA..."
          uploadSecretRef:
            name: iso-server-ssh-credentials
          urlBase: https://iso-server.example.com/ibi/
      clusterUpgradeTimeout: "3h"
  templateParameterSchema:
    properties:
      # ... standard provisioning parameters ...
      upgradeParameters:
        properties:
          seedGeneration:
            type: object
            properties:
              seedImage:
                type: string
                minLength: 1
              seedAuthSecretRef:
                type: object
                properties:
                  name:
                    type: string
                required: [name]
              recertImage:
                type: string
              liveISO:
                type: object
                properties:
                  releaseImage:
                    type: string
                    minLength: 1
                  installationDisk:
                    type: string
                    minLength: 1
                  sshKey:
                    type: string
                  uploadSecretRef:
                    type: object
                    properties:
                      name:
                        type: string
                    required: [name]
                  urlBase:
                    type: string
                    minLength: 1
                  pullSecretRef:
                    type: object
                    properties:
                      name:
                        type: string
                    required: [name]
                  storageClass:
                    type: string
                required: [releaseImage, installationDisk, uploadSecretRef, urlBase]
            required: [seedImage, seedAuthSecretRef]
        type: object
    type: object
```

## Challenges and Mitigations

### 1. Incomplete ACM cleanup contaminates the seed image

**Challenge**: If ACM agent resources are not fully removed, hub-specific artifacts (agent certificates, observability configs, managed-cluster identity) end up in the seed image and cause conflicts when the image is deployed to a different cluster.

**Mitigation**: The controller removes hub-side addon CRs (stopping reconciliation) and deletes the spoke `Klusterlet` CR, letting the klusterlet operator tear down the agent namespaces and their identity secrets (`bootstrap-hub-kubeconfig`, `hub-kubeconfig-secret`). lca-cli's own
cleanup does **not** strip klusterlet/registration identity, so this explicit removal is required; lca-cli still handles the remaining cluster-specific artifacts (certificates, node identity) as part of recert.

Observability is handled **preventively rather than by post-hoc cleanup**. RHACM's `multicluster-observability-operator` automatically deploys the
observability-addon (metrics-collector plus the observability-endpoint-operator) to each managed cluster; that addon creates hub-specific secrets such as
`hub-alertmanager-router-ca-*` and `observability-alertmanager-accessor-*` in the spoke's `openshift-monitoring` and `openshift-user-workload-monitoring`
namespaces — exactly the hub-coupled artifact that must not be baked into a portable seed. Because the seed cluster is disposable and purpose-built, the seed
`ClusterTemplate` sets the `observability: disabled` label on the `ManagedCluster` (via `clusterInstanceDefaults`) from the moment it is imported, so the
observability-operator never deploys the addon and those secrets are never created. This is preferable to enumerating and deleting them during teardown, whose
exact set drifts across RHACM/MCE versions — preventing creation is more robust than racing a fragile hardcoded deletion list before capture. A residual secret
sweep is needed only if a cluster that *already* had observability deployed is later repurposed as a seed (not the standard flow); it is a fallback in
[Open Question 4](#open-questions), not the default path.

### 2. Spoke client availability during seed generation

**Challenge**: During Phase 3, the lca-cli shuts down cluster operators on the spoke, which may temporarily affect API server availability and disrupt the spoke client.

**Mitigation**: The controller polls the SeedGenerator CR with backoff. The spoke API server stays running (lca-cli stops operators, not the API server), and the Phase 2 admin kubeconfig remains valid — its client certificate is independent of ACM and unaffected by klusterlet teardown — so the
spoke client keeps working even though the ACM agents are gone. Transient API errors during operator shutdown are retried.

### 3. ACM self-healing during cleanup phase

**Challenge**: ACM's klusterlet operator may attempt to re-deploy agents while the controller is removing them in Phase 2, creating a race condition.

**Mitigation**: The controller deletes the `Klusterlet` CR itself — not just the agent Deployments — so the klusterlet operator tears the agents down and does not recreate them; the race dissolves because there is no CR left to reconcile. Hub-side `ManagedClusterAddOn` CRs are deleted first
so the addon framework also stops. The order is: hub addon CRs → spoke `Klusterlet` CR → wait for namespace/pod teardown. The seed cluster is disposable, so nothing reinstalls the klusterlet afterward.

### 4. Seed cluster afterlife and PR terminal state

**Challenge**: The seed cluster is a disposable factory that is intentionally left detached from ACM after generation (klusterlet and addons removed, not restored). But the ProvisioningRequest controller normally reconciles ACM-dependent state — `ConfigurationApplied` (policy
compliance via the governance addon) and `NonCompliantAt`. Left unhandled, the PR would report those as broken forever once ACM is gone.

**Mitigation**: The controller suppresses the PR's ACM-dependent reconciliation (config/policy compliance)
whenever the spoke has (begun to) detach and `SeedGenerationCompleted` is terminal. Detachment happens in
Phase 2, so **any** terminal outcome reached at or after Phase 2 — `True / Completed`, `False / Failed`, or
`False / TimedOut` — leaves the cluster detached and must suppress those checks; otherwise
`ConfigurationApplied` and `NonCompliantAt` would report it as broken forever. To distinguish this from a
pre-detachment failure (e.g. `PreconditionChecksFailed` in Phase 1, where the cluster is still ACM-attached
and should reconcile normally), the controller records a `DetachmentStarted` marker in
`SeedGenerationStatus` **immediately before** the first destructive ACM removal and gates the suppression on
it (the marker's before-the-point-of-no-return placement is detailed in the status-field comment). On any
terminal outcome it also stops re-running seed generation. The cluster is left running for the operator to
inspect; deleting the ProvisioningRequest tears it down via the existing deletion path and reclaims the
hardware. This mirrors the manual workflow, which also never re-attaches the seed cluster.

### 5. Seed SNO prerequisites

**Challenge**: The seed SNO has hardware-specific prerequisites (CPU topology alignment, FIPS, proxy, IP version match with target SNOs) that cannot be fully validated by the hub controller.

**Mitigation**: The controller validates what it can (OCP version, `var-lib-containers` partition, LCA operator presence, OADP operator presence, registry credentials). Hardware prerequisites are the cluster template author's responsibility — they must ensure the ClusterTemplate's
`clusterInstanceDefaults` and `policyTemplateDefaults` produce a seed SNO that meets the documented prerequisites.

### 6. Spoke access after ACM removal

**Challenge**: The klusterlet teardown in Phase 2 deletes the ACM agent namespaces where the `ManagedServiceAccount` token lives, so the controller cannot use that token to drive the rest of seed generation. It needs a spoke credential that survives ACM removal, stays valid for the whole
operation (2h/3h), and — critically — does **not** end up baked into the seed image, since anything present in the spoke's etcd at capture time is cloned into every cluster deployed from that seed.

**Mitigation**: The controller uses the spoke's **admin kubeconfig, retrieved from the hub** (the Secret named by the spoke `ClusterDeployment`'s `spec.clusterMetadata.adminKubeconfigSecretRef`), rather than minting a new spoke-side credential. This kubeconfig authenticates with an embedded
`system:admin` **client certificate** signed at install time, which has three properties that fit this workflow exactly:

- **It survives ACM teardown.** The client cert is independent of the klusterlet and the `ManagedServiceAccount`; deleting the `Klusterlet` CR does not affect it. It stops working only if the spoke's API server certificate is rotated under a new CA, which happens at *restore* time on the
  target (recert), not during generation on the seed — so it remains valid through Phase 5.
- **Using it writes nothing to the spoke.** No ServiceAccount, token Secret, or RBAC object is created on the spoke, so **no new credential is captured into the seed image**. This directly resolves the concern that a minted credential would become part of the seed and would need to be found and
  deleted after every IBI/IBU: there is nothing to find. The only object this workflow places on the spoke is the transient `seedgen` Secret in Phase 3 (the same Secret the manual LCA workflow uses), which lca-cli consumes and Phase 5 deletes.
- **No revocation is required.** Because nothing is minted, there is no standalone credential to revoke or leak. Phase 5 spoke cleanup is limited to deleting the transient `seedgen` Secret and the `SeedGenerator` CR; a hub-side finalizer plus a bounded operation **deadline** force-run that
  cleanup even for a stuck or abandoned run, so the `seedgen` Secret is not left behind.

The trade-off is that the admin kubeconfig is a broad (`cluster-admin`) credential. This is accepted because it is used only during a controlled one-shot operation, never persisted or copied to the spoke, and never baked into the seed — so its blast radius is bounded by the operation's duration,
not by any artifact that outlives it. The controller's only new access requirement is a hub-side `get` on the admin-kubeconfig Secret in the cluster's namespace (see the hub RBAC contract); it needs **no spoke-side RBAC at all**. The Phase 2 deletion of `ManagedClusterAddOn` CRs is likewise a
**hub-side** operation performed with the O-Cloud Manager's own hub client, covered by the hub RBAC contract, not by any spoke credential.

### 7. ISO generation Job storage requirements

**Challenge**: The `openshift-install image-based create image` command pulls the full seed image and generates an ISO, requiring ~15-20GB of workspace storage. Ephemeral storage on hub nodes is typically insufficient and risks pod eviction.

**Mitigation**: The controller creates a dedicated PVC (~20GB) for the ISO build workspace, mounted into the Job pod, using `liveISO.storageClass` or the cluster default if omitted. It is cleaned up in Phase 5 after successful upload. On failure it is retained for debugging under the **same
bounded diagnostic-retention TTL** described in the failure workflow (reclaimed when the TTL elapses or on PR deletion, whichever comes first) — **not** indefinitely, since a ~20GB workspace per failed request would otherwise accumulate and exhaust hub storage. Operators must ensure the hub has a
StorageClass with sufficient capacity.

### 8. Disconnected environment registry configuration

**Challenge**: In disconnected environments, the ISO build must pull the seed image and OCP release images from local mirrors. The `openshift-install` binary must also be extracted from a mirrored release image. Misconfigured mirrors cause silent failures deep in the ISO build.

**Mitigation**: The controller **auto-derives the mirror configuration from the hub** (Phase 4, step 1) — mirror mappings from the hub's `ImageDigestMirrorSet`/`ImageContentSourcePolicy` and registry trust from its image-config `additionalTrustedCA` — and injects it into both
`oc adm release extract` (`--idms-file` + CA trust) and the `ImageBasedInstallationConfig` (`imageDigestSources` + `additionalTrustBundle`). Because the hub already operates in the same disconnected environment as the seed and target clusters, this avoids duplicating and drifting mirror config
in the ClusterTemplate; optional `liveISO.imageDigestSources` / `liveISO.additionalTrustBundleConfigMapRef` overrides cover targeting a different mirror. `releaseImage` is required (no public-registry fallback) and the pull secret must include credentials for all mirrors involved; Phase 1
validates that `releaseImage` is reachable and `oc adm release extract` can authenticate using the resolved mirror config. The `seedImage` in the `ImageBasedInstallationConfig` is the immutable `repo@sha256:<digest>` captured in Phase 3, not the mutable tag, so the ISO is built from exactly
the generated image.

### 9. HTTPS ISO server certificate management

**Challenge**: In disconnected environments, the HTTPS ISO server uses a private CA. BMCs fetching the ISO via virtual media must trust this CA. Different BMC vendors handle custom CA trust differently — some support injecting a CA cert via the virtual media API, others rely on
pre-configured trust stores.

**Mitigation**: The `uploadSecretRef` Secret carries an optional `caCert` field with the PEM-encoded CA bundle
for the ISO server. Phase 1 validates the chain via a TLS handshake against the `urlBase` host. On completion the
controller **materializes the PEM bundle into a dedicated ConfigMap** (in the O-Cloud Manager namespace, named
deterministically from the ProvisioningRequest) and records a typed reference to it in
`SeedGenerationStatus.ISOServerCACertRef` — a stable object reference, not inline data or a name for an object
that is never created. Downstream consumers (BMC configuration, hardware manager) resolve that reference when
mounting the ISO via virtual media.

The controller's role stops at validating the cert, using it during pre-flight checks, and surfacing it via the
ConfigMap reference. BMC-side certificate management proper — e.g. uploading CA certificates to BMCs via the
Redfish `ImportCertificate` action as a day-0/day-2 operation — is intentionally **out of scope** and is better
handled as a separate certificate-management feature (consistent with the Non-Goals), not taken on here.

### 10. Registry pre-flight check limitations

**Challenge**: The registry validation in Phase 1 runs from the hub cluster, which may have different network access than the spoke (where the actual image push occurs).

**Mitigation**: The pre-flight check validates credential correctness and basic reachability from the hub. If hub and spoke have different network paths to the registry (e.g. disconnected environments with different mirrors), the check may pass on the hub while the spoke-side push still
fails; the `seedImage` pull-spec should therefore reference a mirror accessible to the spoke. The pre-flight check validates what it can, and the SeedGenerator reports push failures in its own conditions.

## Alternatives Considered

### A. Full ACM detachment (delete ManagedCluster)

Detach the spoke entirely by deleting the ClusterInstance/ManagedCluster, as the manual workflow does today. Rejected because deleting the ClusterInstance risks triggering deprovisioning of the running spoke and discards the hub-side record of the cluster. The chosen approach instead
keeps the `ManagedCluster` intact and removes only the spoke-side ACM agent state (klusterlet + addons), achieving the same seed cleanliness without touching the provisioning lifecycle; the operator reclaims the hardware by deleting the ProvisioningRequest when the seed is captured.
(Spoke access is obtained the same way either way — the hub-held admin kubeconfig survives klusterlet teardown regardless — so the access credential is not what distinguishes this alternative.)

Taking on spoke cleanup ourselves adds responsibility that deleting the `ManagedCluster` would otherwise leave to ACM, and it must be bounded so it does not become a maintenance burden as ACM adds features. The design deliberately leans on
ACM's **own teardown primitives** rather than a hardcoded inventory: it deletes the `Klusterlet` CR (so the klusterlet operator tears down *all* current and future registration/work agents and namespaces) and enumerates and deletes **all**
`ManagedClusterAddOn` CRs (so new addons are covered without code changes). The residual risk is limited to spoke state that is neither klusterlet-managed nor a `ManagedClusterAddOn` — the observability secrets in
[Challenge 1](#1-incomplete-acm-cleanup-contaminates-the-seed-image) are the known example, and the pattern there (prevent creation at the source via a label rather than chase deletions) is the template for any future case.

### B. Separate SeedGenerationRequest CRD

A dedicated CRD that references a ProvisioningRequest and drives seed generation independently. Rejected because it adds API surface, doesn't reuse the existing template-defaults/merge infrastructure, and creates a coordination problem between two controllers.

### C. External job/pipeline

Offload seed generation to an external CI/CD pipeline triggered by the ProvisioningRequest. Rejected because it breaks the single-API contract of the ProvisioningRequest and requires external infrastructure the operator may not have.

### D. Controller-managed LCA installation

Have the controller install the LCA operator on the spoke via ManifestWork during seed generation. Rejected in favor of requiring LCA as a prerequisite in the cluster's initial configuration (via `policyTemplateDefaults`). This keeps the controller simpler and ensures LCA is available
and stable before seed generation begins, rather than installing it just-in-time.

### E. ISO generation via external pipeline (Tekton, CI/CD)

Offload ISO generation to an external Tekton pipeline or CI/CD system triggered after seed generation. Rejected because it breaks the single-API model, requires additional infrastructure, and adds complexity for disconnected environments where the pipeline itself needs mirror access.
A hub-side Job keeps the entire workflow within the ProvisioningRequest lifecycle.

### F. Default to public release image registry

Use `quay.io/openshift-release-dev/ocp-release:<version>-<arch>` as the default `releaseImage` when not specified. Rejected because the exact release pull-spec (version and architecture) cannot be inferred reliably, and a silent default risks pulling the wrong image. `releaseImage`
stays required and explicit; the *mirror redirection* for it (and its trust) is handled automatically via the hub-derived `imageDigestSources`/`additionalTrustBundle`, so the template author supplies the release reference but not the mirror plumbing.

## Implementation Constants

```go
// New constants in internal/controllers/utils/constants.go
const UpgradeDefaultsSeedGenerationKey = "seedGeneration"

const DefaultSeedGenerationTimeout = 2 * time.Hour
const DefaultSeedGenerationWithISOTimeout = 3 * time.Hour
```

## Effort Estimate

~2900 lines of new/modified code across API types, controller logic, Job template, tests, and docs.

| Area | Files | Est. lines | Complexity |
|---|---|---|---|
| API types — `SeedGenerationStatus` (with `ReleaseImageDigest`, `ISOURL`, `ISODigest`, `ISOServerCACertRef`, `DetachmentStarted`), condition type, constants | 3 files | ~50 | Low |
| `parseUpgradeConfig` + `validateUpgradeParametersSchema` extension — third upgrade type (both runtime and schema mutual-exclusivity checks) | 2 files | ~40 | Low |
| `IsSeedGenerationRequested` predicate + reconciler dispatch (separate trigger from `IsUpgradeRequested`) | 1 file | ~40 | Medium |
| CT validation — seedGen defaults, registry pre-flight, release image check, TLS handshake, upload Secret validation | 1 file | ~150 | Medium |
| Seed generation controller — Phases 1-3 + Phase 5 state machine (ACM cleanup, SeedGenerator lifecycle, spoke client, terminal state) | 1 new file | ~600 | High |
| ACM removal helpers — admin-kubeconfig retrieval + spoke client build, `Klusterlet` CR delete + wait, hub addon-CR delete (no restore) | 1 new file | ~140 | Medium |
| ISO generation Phase 4 — PVC creation, Job template (extract `openshift-install`, build config, build ISO, SCP upload, HTTPS verification), Job lifecycle monitoring, pull secret merging, hub mirror-config derivation from IDMS/ICSP and `additionalTrustedCA` | 1 new file | ~420 | High |
| RBAC — hub only (`get` on the admin-kubeconfig Secret for spoke access, plus the **hub RBAC contract** above); no spoke-side RBAC is created | 1 file | ~20 | Low |
| Tests — seed gen controller, ACM helpers, ISO Job lifecycle, validation, TLS checks | 2-3 new files | ~1200 | High |
| Samples — ClusterTemplate YAML, upload Secret | 2 files | ~80 | Low |
| Docs — update ibi-based-cluster-provisioning.md | 1 file | ~120 | Low |
| Vendor — add SeedGenerator API types | go mod | — | Low |

Roughly 4 working sessions to implement:

- **Session 1**: API types, constants, conditions, `parseUpgradeConfig` extension, CT validation (registry pre-flight, release image check, TLS handshake, upload Secret validation), sample YAMLs
- **Session 2**: Core seed generation state machine (Phases 1-3, 5), ACM removal helpers (admin-kubeconfig retrieval + spoke client, `Klusterlet` CR delete, addon-CR delete), terminal-state / config-reconcile suppression, hub RBAC, wiring into the main reconciler
- **Session 3**: ISO generation — hub mirror-config derivation, PVC creation, Job template (release image extract, config generation, ISO build, SCP upload, HTTPS post-upload verification), pull secret merging, Job lifecycle monitoring, cleanup
- **Session 4**: Tests, edge cases, docs update, `make ci-job` pass

## Open Questions

1. **Is deleting the `Klusterlet` CR + all `ManagedClusterAddOn` CRs sufficient to fully remove ACM agent state?** Option B relies on the klusterlet operator's own teardown (triggered by deleting the `Klusterlet` CR) rather than a hardcoded list of Deployments/DaemonSets. This should
   be validated against the specific ACM/MCE version — in particular that the agent namespaces and identity secrets are fully removed so the captured seed carries no stale hub-registration state. (Confirmed: lca-cli does **not** strip klusterlet identity, so the controller cannot
   defer this.)

2. **What container image should the ISO generation Job use?** The Job needs `oc` (for `oc adm release extract`) and basic tools (`scp`, shell). Options include the OCP CLI tools image (`registry.redhat.io/openshift4/ose-cli`), which is available on disconnected mirrors and already
   contains `oc`. The `openshift-install` binary is extracted at runtime from the release image rather than baked into the Job image.

3. **Should ISO upload support mechanisms beyond SCP?** The initial implementation uses SCP/SFTP for simplicity (matching the existing manual workflow). Future iterations could add S3-compatible upload, HTTP PUT, or NFS mount as alternative upload mechanisms if there is demand.

4. **Is a residual observability-secret sweep ever required?** The standard flow prevents observability contamination up front by setting the
   `observability: disabled` label on the `ManagedCluster` at import time (see [Challenge 1](#1-incomplete-acm-cleanup-contaminates-the-seed-image)), so the
   observability-addon and its `hub-alertmanager-router-ca-*` / `observability-alertmanager-accessor-*` secrets are never deployed. The only scenario that would
   still leave those secrets behind is repurposing a cluster that *already* had observability deployed as a seed cluster. If that scenario needs to be supported,
   a bounded spoke-side sweep of those namespaces would be added — accepting the corresponding RBAC widening — rather than relaxing the preventive default.
