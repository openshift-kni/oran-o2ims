<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Implementation Plan: Automated IBI Server Pre-Provisioning

## Purpose and baseline

This document proposes the implementation and delivery plan for
[CNF-26353](https://redhat.atlassian.net/browse/CNF-26353). It develops the
[original server pre-provisioning proposal](./ibi-server-pre-provisioning.md)
into work packages spanning O-Cloud Manager, the
[IBI Operator](https://github.com/openshift/image-based-install-operator),
and ACM delivery.

The baseline includes Brent's proposed changes in
[PR 5311](https://github.com/openshift-kni/oran-o2ims/pull/5311), reviewed at
`fa16f086`. That PR is still under review. This plan incorporates its proposed
per-host network extractor while accounting for the two outstanding corrections:
the existing configuration ISO payload format and the actual ISO publication
layout.

The intended feature remains an optional preparation step within a
ProvisioningRequest-driven SNO installation. The allocated server has a
BareMetalHost (BMH), and the IBI Operator uses BMO/Ironic to manage its boot and
media. Preparation is followed automatically by normal IBI cluster installation.
The [independent factory-preparation proposal](https://github.com/openshift-kni/oran-o2ims/pull/5231)
is a separate workflow choice.

The working design uses authenticated HTTPS callbacks. SSH polling remains an
alternative discussed below; it has not been selected as a replacement.

## Component ownership

| Component | Responsibility |
| --- | --- |
| O-Cloud Manager | Validate template/request inputs, allocate hardware, supply preparation configuration, generate a compatible reusable ISO, and report progress through the ProvisioningRequest. |
| SiteConfig | Render the ImageClusterInstall (ICI) with its preparation intent present before the IBI Operator acts on it. Agree any required template/API changes with its maintainers. |
| IBI Operator, delivered through ACM | Own the preparation API, per-attempt state and carrier, network/callback bootstrap contract, callback receiver, BMH preparation lifecycle, recovery, and handoff to normal IBI installation. |
| BMO/Ironic | Perform supported virtual-media and power operations requested through the BMH and DataImage resources. |
| OpenShift installer | Build the live ISO using its existing image-based installation and Ignition override interfaces. Extend it only if the feasibility work establishes a missing capability. |

The IBI Operator's controller manager and image server are separate processes
(`cmd/manager` and `cmd/server`). A callback cannot wake the reconciler through
an in-memory channel shared between them. The receiver will persist an
authenticated attempt result through the Kubernetes API; the manager's ICI watch
then drives reconciliation. Reconciliation also uses explicit deadline requeues
so an absent callback cannot leave preparation waiting indefinitely.

## Intended workflow

The successful path is:

| Step | Component | Action |
| --- | --- | --- |
| 1 | OCM | Validate the request and allocate hardware. |
| 2 | SiteConfig | Render the ICI with preparation intent. |
| 3 | IBI Operator | Record an attempt and create its preparation DataImage. |
| 4 | BMO/Ironic | Attach the live ISO and preparation DataImage, then boot the server. |
| 5 | Live ISO | Apply the per-host network configuration. |
| 6 | Live ISO | Prepare the disk and pre-cache images. |
| 7 | Live ISO and IBI callback server | Send an authenticated HTTPS callback and persist the preparation result. |
| 8 | IBI Operator | Clean up preparation media and hand off to normal IBI installation. |
| 9 | IBI Operator and BMO/Ironic | Attach the normal configuration DataImage and boot from disk. |
| 10 | OCM | Observe cluster installation and fulfill the request. |

The shared live ISO contains generic code and trust configuration. The
preparation DataImage contains the target's network settings and the current
attempt's callback configuration. The normal IBI configuration DataImage is
attached later, after the preparation carrier has been removed.

## Contracts to settle before implementation

### Media delivery and network bootstrap

Brent's update proposes applying the existing per-host nmstate configuration
before preparation pulls images or sends a callback. This can remove the DHCP
dependency when usable static configuration is supplied, while retaining a
shared ISO. It requires a working delivery path for the per-host data.

The first work package must prove that the supported BMC, BMO, and Ironic
versions can expose the live ISO and preparation DataImage simultaneously.
Having two slots in the BMC is insufficient evidence by itself: the driver must
select compatible devices without replacing the boot ISO. Verify attachment
completion and guest visibility before relying on the carrier at boot.

The proposed implementation is to extend the callback carrier already needed by
the original proposal with network data. This produces one preparation carrier
containing both payloads, followed later by the normal configuration carrier.
It reuses the networking values generated from ClusterInstance input; it does
not assume an existing configuration ISO already contains the callback files.

For the preparation carrier, agree and version an explicit filesystem layout
and volume label. A small raw nmstate file alongside the callback files is the
preferred starting point. The producer and the on-ISO reader must use the same
contract. Read the existing BMH-referenced nmstate Secret through the IBI
Operator's networking input path, without relying on BMO to apply that Secret
to a `live-iso` boot.

If reusing the complete configuration ISO is preferable after prototyping, its
existing format is already known: volume label `cluster-config`, with the
nmstate string in `cluster-configuration/manifest.json` under
`raw_nm_state_config`. It is neither a standalone nmstate file nor config-drive
`network_data.json`. Reuse would require explicit extraction and addition of the
callback payload; the normal generator cannot simply be moved earlier unchanged.

The generic network unit must run before
`install-rhcos-and-restore-seed.service`. Its dependency ordering must prevent
disk preparation from starting with missing or failed required network data.
Check that the target live environment contains `nmstatectl` and the callback
client's dependencies for each supported release. No network-dependent tool
download can be needed to establish that network.

Static configuration must include the addresses, routes, DNS, and interface
identity needed on the preparation network. DHCP remains a possible explicit
environment choice; a shared ISO without usable per-host static configuration
still needs DHCP. The later installation configuration does not configure the
earlier live boot automatically.

Hub-side probes cannot establish that a not-yet-booted host can obtain an address
or reach the callback endpoint. Validate what can be checked before boot and
document the remaining network prerequisites. Prove the actual host path in the
development experiment, then bound runtime failures with preparation timeouts
and useful diagnostics.

If simultaneous media cannot be demonstrated, stop and revisit the delivery
design with ACM before implementing the full controller. The original fallback
of a customized ISO per target/attempt changes reuse, generation, credential
lifetime, and cleanup; it needs an explicit decision and revised estimates.

### Input API and rendering

Retain the original proposal's template-owned defaults and schema-controlled
request overrides. The initial field mapping is:

| OCM `hwMgmtDefaults.ibiPreProvisioning` | ICI `spec.preProvisioning` |
| --- | --- |
| `isoURL` | `isoURL` |
| `isoDigest` | `isoDigest` |
| `isoServerCACertRef` | `isoServerCACertRef` |
| `preProvisioningTimeout` | `timeout` |
| `maxRetries` | `maxRetries` |
| `retryBackoff` | `retryBackoff` |

Resolve name-only references in their documented source namespace and copy
required CA data into the ICI namespace. Retain that trust material for the
target installation's lifetime, independently of cleanup of the source seed
generation request. Template validation checks defaults; request validation
checks the permitted input and merged effective configuration before hardware
allocation. Shared OCM rules belong in `internal/validation`.

The callback origin and its trust configuration are environment/operator-owned
settings, not arbitrary request destinations. Agree their supported ACM
configuration surface. OCM must not rely on directly patching an ACM-managed
Deployment to configure them. Ordinary Kubernetes RBAC does not restrict writes
to a particular spec field; callback-origin policy needs admission/configuration
validation as well as appropriate resource permissions.

Prefer having SiteConfig render preparation configuration into the ICI at
creation. Confirm which supported ClusterInstance/template interface carries
those values; a template change alone cannot create a new field in the
ClusterInstance API. If a supported creation-time passthrough is unavailable,
the alternative must create an explicitly gated ICI, populate it, then release
the gate. A normal ICI followed by an asynchronous patch can start the wrong
workflow and is not an acceptable ordering.

Define an explicit template-owned opt-in for producing callback-capable live
ISOs. Agree its final schema during API review. Ordinary/manual ISO generation
must continue to work without a preparation carrier. A callback-capable ISO is
an identifiable prerequisite for automated preparation; existing ISOs do not gain
the new executable behavior from a DataImage.

### ISO identity and availability

Consume the verified `isoURL` and `isoDigest` already published by seed
generation. Today's URL layout is:

```text
<liveISO.urlBase>/seedgen/<ProvisioningRequest.metadata.uid>/rhcos-ibi.iso
```

This is a unique location per request instance. It does not contain a seed-image
digest and is not a content-addressed URL. Retain the original requirement to
keep the published bytes unchanged and available while consumers use them.
The URL layout alone does not enforce server-side immutability.

The IBI Operator validates the approved ISO origin and expected ISO digest
before boot. Prototype the actual BMO/Ironic fetch/rehosting behavior and the
URL ultimately supplied to the BMC, including DNS and TLS behavior. Distinguish
the operator's ISO-server trust, the BMC/Ironic media-fetch trust, and the live
host's callback-server trust. A ConfigMap consumed by the operator does not
configure the BMC's certificate store.

### Preparation state and ProvisioningRequest sequencing

Current OCM ordering completes hardware allocation before applying the
ClusterInstance that produces the ICI. Waiting for ICI preparation at that
earlier hardware gate would prevent creation of the object being awaited.

The proposed refinement is to retain `HardwareProvisioned` for the allocation
result, create the ClusterInstance/ICI, and track preparation separately before
reporting cluster installation complete. A dedicated PR condition, provisionally
`IBIPreProvisioningCompleted`, would expose the preparation result alongside
normal installation status. Agree the final condition contract with ACM/OCM
reviewers before code generation; it refines the original proposal's mapping
onto `HardwareProvisioned`.

The IBI Operator gates its normal configuration/disk-boot path until preparation
has succeeded and preparation cleanup is confirmed. In particular, its existing
`validateHost` path must not set `externallyProvisioned` before verified
preparation success. Preserve the existing behavior when preparation is omitted.

Use the proposed `RequirementsMet` reasons for current ICI progress, with
durable attempt/result and cleanup state. Retain a completed-preparation outcome
after the condition advances to normal installation reasons. OCM must recover
the outcome after a restart or coalesced watch updates, rather than depending
on observing a temporary `PreProvisioningSucceeded` transition.

Give preparation its own timeout and finite retry budget. Coordinate these with
the overall PR timeout and start normal installation timing at the appropriate
handoff. Persist start/deadline and retry-backoff state; progress-message updates
must not reset the permitted duration. A retry remains `progressing`; only an
exhausted budget produces terminal failure.

## Development stages and reviewable work packages

Stage numbers describe delivery order. Proposed filenames below describe the
feature's responsibilities; final organization should follow each repository's
conventions.

### 1. Prove the boot, network, and reporting path

**Owner:** joint OCM/IBI Operator development.

- Use a controlled target and record the OCP, ACM, IBI Operator, BMO/Ironic,
  BMC model/firmware, and installer versions.
- Boot an existing IBI live ISO through the BMH path. Record the actual media
  URLs and verify the additional preparation carrier stays attached.
- Build a prototype ISO containing the generic network and callback units using
  the existing installer Ignition override. Apply per-host static networking
  with DHCP unavailable, then observe preparation and an authenticated callback.
- Reuse the same ISO on another target or distinct attempt with different
  network/callback data. Confirm the code and artifact are independent of the
  target and attempt.
- Demonstrate removal of preparation media and the normal configuration
  DataImage/disk-boot handoff. Record unsupported combinations and the fallback
  decision if the preferred path fails.

**Exit:** reproducible development evidence for the chosen delivery path and a
short agreed API/bootstrap contract. This is a development prerequisite, not a
mechanism QA must discover or prove for us.

### 2. Establish the IBI API and cross-component contracts

**Owner:** IBI Operator, with OCM and SiteConfig review.

- Add the optional ICI preparation configuration, validation/defaults, durable
  attempt/result/deadline/cleanup state, and progress reasons.
- Define immutability of active inputs and safe handling of updates, deletion,
  and existing ICIs that omit preparation.
- Agree the creation-time rendering path, PR status mapping, callback endpoint
  configuration, and capability/version contract.
- Add validation and API regression tests; regenerate deepcopy, CRD, and bundle
  output in the owning repositories.

**Exit:** reviewed producer/consumer contract that allows both repositories to
develop against the same API and distinguishes unsupported component versions.

### 3. Implement the preparation carrier and generic ISO bootstrap

**Owner:** IBI Operator; coordinate asset consumption with OCM.

- Build and serve one preparation carrier containing validated network data and
  per-attempt callback parameters. Bind it to the ICI UID, BMH UID, and attempt;
  name/namespace the DataImage according to the BMO host-matching contract.
- Freeze the effective payload for an attempt and reuse those bytes on recovery.
  A changed network Secret must not silently change an already recorded attempt.
- Keep token material in an owned Secret and treat the carrier as secret-bearing
  media with restricted access and short retention. Do not expose it through an
  unrestricted generic file-serving path.
- Implement the mount/materialization, network setup, and completion reporter
  units. Verify current-boot service completion and bound callback retries by
  the agreed attempt budget. Keep credentials out of logs and command arguments.
- Version the carrier layout and shared assets. Prefer a public, versioned
  package of embedded assets owned by the IBI project, consumed by OCM at build
  time. An OCM import of another repository's `internal/ignition` package is not
  possible; settle the public package or equivalent distribution interface.

**Exit:** tested carrier writer/reader and generic bootstrap, with no target or
attempt credentials baked into the reusable ISO.

### 4. Implement the IBI preparation lifecycle and callback receiver

**Owner:** IBI Operator, delivered as an ACM component.

- Add the preparation reconciler path before normal host configuration, with
  explicit BMH/media ownership checks and confirmed attachment before boot.
- Extend `cmd/server` with an authenticated callback handler and Kubernetes
  client. Validate active identity, payload bounds, and attempt state; persist a
  result without coupling the HTTP process to the controller's memory.
- Serialize result acceptance and timeout against one terminal outcome. Re-drive
  cleanup idempotently after partial failure or restart.
- On success, remove preparation media, confirm the required power transition,
  mark the host externally provisioned, retain durable preparation success, and
  continue the existing configuration DataImage and disk-boot flow.
- On failure/timeout, remove preparation media, invalidate credentials, and
  power down before a budgeted retry. Deletion stops normal work and performs
  finalizer-owned cleanup without initiating another installation.

**Exit:** a manually created ICI can take a controlled unprepared BMH through
preparation and normal IBI installation, with focused lifecycle tests passing.

### 5. Integrate callback-capable ISO generation into OCM

**Owner:** O-Cloud Manager, using the agreed IBI-owned bootstrap assets.

- Add the template-owned ISO capability opt-in and extend its shared validation,
  frozen input snapshot, installation configuration serialization, and tests.
- Supply the generic Ignition override through the existing ISO build Job and
  installer interface. Include required callback trust; carry per-target network
  values in the preparation DataImage, not in the shared build configuration.
- Freeze the selected bootstrap version and serialized configuration with the
  run inputs so a resumed Job does not silently adopt different operator assets.
  Agree how generation outputs identify the compatible bootstrap/carrier version
  before a consumer boots the ISO.
- Preserve the current upload/HTTPS verification, status outputs, and generation
  layout. Document how to obtain or generate a compatible ISO.

**Exit:** OCM can publish a reusable preparation-capable ISO; ordinary generation
and its existing outputs remain compatible. This can develop in parallel with
the IBI lifecycle after the asset contract is established.

### 6. Integrate target preparation into OCM provisioning

**Owner:** O-Cloud Manager, with any agreed SiteConfig changes.

- Extend typed hardware defaults, schema validation, request/default merging,
  reference resolution, and supported-component checks before allocation.
- Render preparation intent atomically with the ICI, or implement the agreed
  explicit creation gate. Keep allocation and preparation sequencing distinct.
- Watch ICI status and enqueue its owning cluster-scoped ProvisioningRequest.
  Reconcile durable outcomes after missed events/restarts and preserve correct
  failure/retry/timeout handling.
- Report observable progress: booting the preparation ISO, waiting for a result,
  retry/backoff, preparation complete, and normal cluster installation. A missing
  callback must not be described as proof that disk preparation failed.
- Ensure PR deletion waits for IBI-owned cleanup as required before destructive
  dependent-resource removal. Retain credentials/references until cleanup can
  complete; never remove another controller's finalizers as normal recovery.

**Exit:** one accepted PR drives the full workflow, with a supported ACM build,
without manual BMC actions. Omitted preparation retains the existing IBI path.

### 7. Complete recovery and integration testing

**Owner:** development teams across both repositories.

- Exercise callback-only and timeout-only ICI updates waking OCM, including a
  missed intermediate condition and a controller restart after preparation.
- Cover invalid/replayed/stale callbacks, deadline/result races, interrupted
  carrier creation, attachment failure, bounded retries, interrupted cleanup,
  deletion, and restarts of each IBI process and OCM.
- Verify least-privilege RBAC for each process, regenerate effective manifests,
  and test the feature without granting OCM BMH mutation permissions for work
  owned by the IBI Operator.
- Add the external ICI API/watch prerequisites to local manager tests and check
  the TLS-scan CI environment, its stub CRDs, scheme registration, and support
  collection. A missing external CRD must not cause an unexpected manager
  crash-loop in supported deployments.

**Exit:** development regression coverage and required CI checks pass with no
known lifecycle gap being deferred to a hardware acceptance test.

### 8. Deliver through ACM and qualify the supported workflow

**Owner:** ACM/IBI and OCM delivery teams, with QE and documentation.

- Deliver the IBI controller, server, CRD, configuration, and bundle changes
  through the supported ACM/MCE component release. Record its compatibility with
  OCM and the ISO bootstrap version.
- Publish the OCM templates, samples, user workflow, timeout/retry semantics,
  networking/media prerequisites, diagnostics, and cleanup guidance.
- Run the QA acceptance scenarios below, including the epic's parallel scale
  requirement. Supply release enablement and reference configuration updates.

**Exit:** supported versions, operator behavior, generated ISO requirements, and
user instructions agree. Merging upstream code alone does not deliver the ACM
component dependency.

## Code locations expected to change

Paths are relative to their repository. New file/package names are suggestions,
not existing interfaces.

| Repository | Existing locations | Planned change |
| --- | --- | --- |
| IBI Operator | `api/v1alpha1/imageclusterinstall_types.go`, `imageclusterinstall_webhook.go`, and their tests | Optional preparation API, immutable active inputs, durable status, validation, and generated CRD/deepcopy output. |
| IBI Operator | `controllers/imageclusterinstall_controller.go`, `imageclusterinstall_monitor.go`, and related tests | Dispatch, host validation, preparation lifecycle, handoff, finalizers, and monitoring. |
| IBI Operator | `internal/installer/`, `internal/imageserver/`, `cmd/server/main.go`, `cmd/manager/main.go` | Carrier construction/serving, callback route and API client, configuration, timeout handling, and process integration. |
| IBI Operator | New shared bootstrap package, for example `pkg/preprovisioning/` | Versioned generic assets and carrier contract consumable outside the repository. |
| IBI Operator | `config/manager/`, `config/rbac/`, `config/crd/`, `bundle/`, `hack/integration-test/` | Supported endpoint/configuration, effective permissions, generated artifacts, and development integration coverage. |
| OCM | `api/provisioning/v1alpha1/clustertemplate_types.go`, `conditions.go`, `provisioningrequest_types.go` as needed | Typed preparation defaults and the agreed PR progress/durable timeout contract; regenerate API artifacts. |
| OCM | `internal/validation/`, `internal/controllers/clustertemplate_controller.go`, `provisioningrequest_validation.go` | Shared business validation, template/request call sites, `hwMgmtDefaultsToMap`, and existing `DeepMergeMaps` integration. |
| OCM | `internal/controllers/provisioningrequest_controller.go`, `provisioningrequest_clusterinstall.go`, `provisioningrequest_hwprovision.go` | Correct creation/monitoring order, independent timeout budgets, status mapping, and deletion sequencing. |
| OCM | `internal/controllers/provisioningrequest_setup.go` and a new preparation helper file | ICI status watch, ownership mapping, and focused preparation observation logic. |
| OCM | `internal/validation/seedgeneration.go`, `internal/controllers/provisioningrequest_seedgeneration_iso_inputs.go`, related snapshot/Job code and tests | ISO capability opt-in, generic Ignition serialization, asset compatibility, and unchanged verified publication behavior. |
| OCM | `config/rbac/role.yaml`, CRD/bundle output, `test/utils/vars.go`, `must-gather/gather`, developer dependency documentation | Generated permissions/API output, external ICI test prerequisite, diagnostics, and collection coverage. |
| OCM | `go.mod`, `go.sum`, `vendor/` | Pin the agreed ICI API and shared bootstrap dependencies. |
| SiteConfig | IBI cluster/node templates and their supported input interface | Creation-time ICI preparation intent or the agreed explicit gate; exact upstream locations agreed with maintainers. |

New lifecycle helpers can live in a focused file such as
`controllers/imageclusterinstall_preprovisioning.go` in the IBI repository.
Check generated RBAC before adding grants: controller markers are unioned, and
the IBI server and manager may share a service account. Tie each new grant to an
actual operation, including cached `list`/`watch` reads and direct status writes.

## Dependencies, estimates, and integration checkpoints

Work packages 1 and 2 establish the design and API. Packages 3 and 4 implement
the ACM-owned execution path; package 5 supplies compatible ISO generation.
Package 6 can develop against that agreed API in parallel, but its full lab
checkpoint needs the IBI implementation and a compatible ISO. Package 7 hardens
the combined implementation before package 8 release qualification.

These are initial engineering-effort ranges, including focused tests and code
generation work. They are not elapsed delivery promises; review queues, lab
availability, ACM release lead time, and QE's scale execution are additional.
Re-estimate after package 1, especially if a BMO/Ironic change or a per-target ISO
fallback is required.

| Package | Effort | Main lab/review checkpoint |
| --- | --- | --- |
| 1. Feasibility | 3–5 person-days | Same ISO plus distinct preparation data works, including a static-network boot without DHCP. |
| 2. API/contracts | 2–4 person-days | ACM/OCM/SiteConfig agree inputs, status, gating, and bootstrap compatibility. |
| 3. Carrier/bootstrap | 3–6 person-days | Writer and reader agree; normal/manual ISO behavior is preserved. |
| 4. IBI lifecycle | 5–8 person-days | Standalone ICI reaches preparation success and normal installation. |
| 5. ISO generation | 2–4 person-days | OCM publishes an ISO that passes the preparation boot experiment. |
| 6. OCM integration | 4–7 person-days | A PR installs an initially unprepared SNO and reaches `fulfilled`. |
| 7. Hardening | 3–6 person-days | Development failure/recovery and cross-controller tests pass. |
| 8. Delivery/docs | 2–4 person-days | Supported ACM/OCM artifacts, examples, and qualification handoff are ready. |

The combined range is approximately 24–44 engineering person-days across the
repositories, with some work parallelizable. A preliminary size allowance is
roughly 1,500–3,000 production Go/shell lines and 2,000–4,000 test lines, excluding
generated manifests/deepcopy output and documentation. Reuse and the final
carrier/asset interface can materially change those figures.

Useful incremental lab deployments are the feasibility prototype, the standalone
ICI lifecycle, and the first integrated PR. API-only and documentation changes
do not each require a full hardware installation.

## Development validation and QA acceptance

Development owns proving the delivery mechanism, source-level correctness, and
recovery checkpoints. Unit/integration coverage should accompany the relevant
work package, with script lint and systemd/ISO fixture checks where appropriate.
For functional OCM changes, run `make ci-job` and the relevant `make test-e2e`
coverage before submission; regenerate API/manifests/bundles when required.
Documentation changes use `make markdownlint`. Use the IBI repository's test,
generation, and integration targets for its changes.

QA should validate the supported feature through user-visible behavior:

| Scenario | Acceptance outcome |
| --- | --- |
| Unprepared SNO with preparation configured | One PR drives preparation and installation to a healthy, accessible cluster and `fulfilled`. |
| Already prepared SNO with preparation omitted | Existing IBI installation succeeds without booting the preparation ISO. |
| Representative preparation failure/timeout | PR reports a useful terminal failure after its configured budget; normal installation does not proceed. |
| Configured retry followed by success | PR remains progressing during retry/backoff and subsequently completes. |
| Delete a PR during preparation | Cleanup completes and the request/dependent resources do not remain stranded behind finalizers. |
| Representative operator restart | The workflow resumes without a duplicate destructive preparation run or loss of the final result. |
| Supported DHCP/static networking configurations | Documented prerequisites produce working preparation and installation on the supported environment. |
| Up to 100 SNOs in parallel | Independent PR/ICI workflows progress correctly, results are associated with the right targets, and one target's failure does not corrupt others. |

Capture progress transitions throughout the acceptance run. A one-time read
after waiting cannot prove the order of short-lived handoff states. Check actual
observed power state when a shutdown is part of the assertion, rather than only
the desired `spec.online` value. QA can reference development coverage for the
carrier format, callback authentication, and controller race cases instead of
repeating every internal test on hardware.

## Decisions remaining before implementation

1. Confirm the media/network proof and supported BMC/BMO/Ironic combination.
2. Agree the preparation carrier format and public bootstrap asset interface.
3. Agree creation-time SiteConfig input/gating and the durable ICI/PR status
   contract, including the proposed separate PR preparation condition.
4. Agree ACM-owned callback endpoint configuration, trust, and carrier access.
5. Agree the template-owned ISO capability opt-in and compatibility check.
6. Schedule the ACM component delivery and identify the minimum supported build.

SSH polling can reduce callback and ISO bootstrap work, but it does not deliver
per-host static networking. With DHCP and an already SSH-enabled ISO it avoids
the callback carrier; without DHCP, the network-data delivery problem remains.
It also requires a known reachable host address, a private-key Secret, host-key
verification, and current-boot/physical-target correlation. Selecting it would
revise the bootstrap, API, networking assumptions, and estimates above, while
retaining the original PR/ICI/BMH orchestration model if desired.
