<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Implementation Plan: Automated IBI Server Pre-Provisioning

## Purpose and baseline

This is the end-to-end implementation proposal for
[CNF-26353](https://redhat.atlassian.net/browse/CNF-26353), extending the
[original proposal](./ibi-server-pre-provisioning.md) and the network-bootstrap
discussion in [PR 5311](https://github.com/openshift-kni/oran-o2ims/pull/5311).
The [Metal3 proposal](https://github.com/openshift-kni/oran-o2ims/pull/5477)
contains the detailed BMO/Ironic ownership, observation and media changes.

The feature adds optional preparation to a ProvisioningRequest-driven SNO
installation. Hardware is allocated and represented by a BareMetalHost (BMH).
IBI-O uses BMO/Ironic to boot a reusable live ISO with per-host configuration,
prepare the disk, and continue automatically into normal IBI installation.
Authenticated HTTPS callbacks report preparation completion. The
[independent factory-preparation proposal](https://github.com/openshift-kni/oran-o2ims/pull/5231)
remains a separate workflow.

This revision incorporates the November 25, 2026 development implementation
and successful standalone ImageClusterInstall (ICI) experiment. It distinguishes
working code and observed behavior from proposed integration and remaining
qualification. Development images are not evidence that a released OpenShift
or ACM version provides this feature.

## Current implementation and evidence

| Area | Current state | Remaining delivery work |
| --- | --- | --- |
| BMO, Ironic and Ironic image | Development code implements concurrent media, durable ownership, identity-aware status, selective cleanup and the runtime dependency. | Maintainer agreement, release integration, recovery and ordinary-workflow qualification. |
| IBI-O API and lifecycle | Optional preparation API, immutable inputs, callback handling, teardown and normal handoff are implemented; standalone success path passed on hardware. | API/configuration review, failure/restart qualification and ACM packaging. |
| Reusable ISO bootstrap | Generic network/callback assets and a development Ignition utility work with one shared ISO across distinct attempts. | Public versioned asset interface and automatic O-Cloud Manager ISO generation. |
| O-Cloud Manager integration | Existing seed/ISO publication and ordinary provisioning provide the base. | Preparation inputs, creation-time ICI configuration, status, budgets and deletion ordering. |
| SiteConfig integration | Existing templates render the ICI. | Agree a supported input path before normal reconciliation can begin, including BMH field ownership. |
| Supported release and scale | One development server/firmware combination exercised. | Component compatibility matrix, capacity limits, broader hardware and parallel acceptance. |

The fresh November 25 run used an OpenShift 5.0.2 hub, ACM/MCE 5.0.0,
modified BMO/Ironic/IBI-O images, and a Dell PowerEdge XR8620t with iDRAC
7.30.30.51. The resulting SNO ran OCP 4.22.15. It demonstrated:

- Both preparation images inserted while Off before automatic power-on.
- Static networking with DHCP disabled; exact configuration ISO bytes matched
  the immutable Secret, and guest host/attempt identities matched the ICI.
- Seed restoration, 145 successful image pulls and authenticated completion.
- Power-off before removal of both preparation images, followed by a fresh
  normal configuration DataImage and automatic disk boot with cleaning disabled.
- Preparation and ICI installation Completed, node Ready and ClusterVersion
  Available with Failing=False. Cluster ID and disk boot ID matched the target.
- Revoked callback credentials, removed preparation Secrets/cache/claims,
  and no manual reconcile, media-attachment or power-on workaround in this run.

An earlier run exposed two bugs that were corrected and regression-tested:
Ironic API redaction broke raw-URL identity comparisons, and an early IBI-O
managed marker suppressed normal power-on. The fresh pass used rebuilt images
with both fixes. The earlier run requiring recovery is not counted as an
unassisted pass. O-Cloud Manager did not drive this direct-ICI experiment;
restart, retry, cancellation and cross-vendor hardware qualification remain.

## Component ownership

| Component | Responsibility |
| --- | --- |
| O-Cloud Manager | Validate effective inputs, allocate hardware, generate a compatible ISO, supply ICI preparation intent, observe durable status and retain dependencies through deletion. |
| SiteConfig | Render the ICI through the ClusterInstance/template path with preparation intent or an agreed creation gate already present. |
| IBI-O, delivered through ACM | Own ICI preparation API, attempts, frozen input/media, generic guest bootstrap, callback authentication, cleanup and normal IBI handoff. |
| BMO | Associate the DataImage with the live deployment, separate deployment ownership from ordinary media management, report identity-aware status and acknowledge release. |
| Ironic | Publish and insert both images before power-on, retain their identities, preserve unrelated media and tear down owned publications/devices. |
| Ironic runtime image | Supply existing configdrive format detection's `file(1)` dependency. |
| OpenShift installer | Generate the existing IBI live ISO; its current Ignition override/embedding interfaces support the demonstrated bootstrap. |

IBI-O's manager and image server are separate processes. The callback receiver
persists the authenticated result through the Kubernetes API. The manager's
ICI watch and explicit deadline requeues drive reconciliation; no shared-memory
notification or mandatory SSH polling is needed.

## Intended workflow

### Generate and select a compatible live ISO

O-Cloud Manager extends its existing seed/ISO generation path with a
template-owned opt-in. The proposed spelling is `liveISO.preProvisioning: true`;
the final schema and compatibility metadata need API review. It adds generic
networking/reporting assets to complete installer-generated Ignition while
preserving its disk-preparation command and configured `liveISO.sshKey`.

Freeze the selected bootstrap/protocol version with the existing immutable
build inputs. Publish the final ISO through the existing verified upload path
and record its URL, SHA-256 digest and preparation capability/version together.
The exact output field names remain an integration decision. Validation must
reject unsupported capability/version combinations before allocation. A file's
URL or digest alone does not prove that it contains the required bootstrap.

The shared ISO contains generic code, rather than target identity, network
settings or attempt credentials. Existing/manual ISO generation remains usable
without the opt-in. Preserve existing release/disk derivation, base ISO override,
mirror configuration, trust and upload verification.

### Provision an unprepared target

| Step | Component | Action |
| --- | --- | --- |
| 1 | O-Cloud Manager | Validate defaults/overrides, compatible artifact and required components before hardware allocation. |
| 2 | O-Cloud Manager / hardware manager | Allocate and configure compatible hardware; retain the established hardware-completion contract. |
| 3 | O-Cloud Manager / SiteConfig | Create the ClusterInstance and ICI with preparation intent present before IBI-O can start normal installation. |
| 4 | IBI-O | Bind an attempt to the ICI/BMH identities, freeze its effective input, verify/cache the boot ISO and create immutable configuration media. |
| 5 | BMO / Ironic | Claim the associated DataImage, publish both images, confirm concurrent attachment and request live boot. |
| 6 | Live environment | Read `cluster-config`, apply per-host networking, prepare the disk and precache images. |
| 7 | Live environment / IBI-O server | Authenticate the terminal callback and persist its attempt-bound result. |
| 8 | IBI-O / BMO / Ironic | Revoke the token, stop the live deployment, confirm media teardown and finalize the preparation DataImage. |
| 9 | IBI-O | Remove preparation artifacts, restore the normal externally-provisioned/offline handoff and create a fresh normal configuration DataImage. |
| 10 | IBI-O / BMO | Request normal disk boot and monitor cluster installation using the existing path. |
| 11 | O-Cloud Manager | Report preparation independently, continue normal installation/policy monitoring and reach `fulfilled`. |

The earlier plan reused one DataImage through both boots. The implemented and
recommended lifecycle instead uses an immutable preparation DataImage followed
by a fresh normal-installation DataImage after release. Both use the existing
`cluster-config` label and nmstate manifest layout. The preparation image adds
attempt data and omits final cluster credentials; normal credentials/media are
generated after preparation. This lifecycle refinement needs review alongside
the previous preference for reusing the existing configuration artifact.
There are two images mounted during preparation, never a third network ISO.

## Input API and rendering

### Template and request contract

Retain template-owned defaults and schema-controlled request overrides. The
proposed O-Cloud Manager mapping is:

| `hwMgmtDefaults.ibiPreProvisioning` | ICI `spec.preProvisioning` | Working ICI contract |
| --- | --- | --- |
| `isoURL` | `isoURL` | Required HTTPS source on an operator-approved origin. |
| `isoDigest` | `isoDigest` | Required SHA-256 digest of the exact boot ISO. |
| `isoServerCACertRef` | `isoServerCACertRef` | Optional ConfigMap reference; `ca-bundle.crt` in the ICI namespace. |
| `preProvisioningTimeout` | `timeout` | Per-attempt duration; default 1 hour, positive and at most 24 hours. |
| `maxRetries` | `maxRetries` | Additional attempts; default zero, range 0..10. |
| `retryBackoff` | `retryBackoff` | Default 5 minutes; range zero to 24 hours; begins after teardown. |

The optional ICI block exists in the development CRD:

```yaml
spec:
  preProvisioning:
    isoURL: https://iso.example.com/seedgen/run-id/rhcos-ibi.iso
    isoDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    isoServerCACertRef:
      name: iso-server-ca
    timeout: 60m
    maxRetries: 0
    retryBackoff: 5m
```

An ICI omitting preparation follows ordinary IBI installation. Active installation
input is frozen once preparation starts. Requests cannot redirect callback
traffic or choose arbitrary callback credentials. The initial implementation
accepts unauthenticated HTTPS ISO sources, rejects redirects, and verifies
content before boot; authenticated source downloads need separate design.

Resolve name-only references in their documented source namespace and copy CA
data into the ICI namespace. Retain that trust material independently of the
source seed-generation request. Validate defaults, allowed overrides and the
merged effective configuration before allocation. O-Cloud Manager shared rules
belong in `internal/validation`; merge with existing `DeepMergeMaps` rules.

### Creation-time ICI intent

SiteConfig already renders the ICI from a ClusterInstance and referenced
templates. A template alone cannot read a new field absent from the
ClusterInstance API. Agree the supported producer-to-template input path before
implementing the O-Cloud Manager integration.

Prefer preparation configuration in the initial rendered ICI. Use an existing
supported interface if it can carry the complete configuration; otherwise
propose the smallest typed SiteConfig input/template extension. This document
does not assert that a SiteConfig code or CRD change is already required.

An explicit creation gate is an alternative only if IBI-O honors it before
normal side effects and SiteConfig preserves both configured intent and gate
release on later renders. O-Cloud Manager would populate preparation and release
that gate atomically. A normal ICI followed by an asynchronous patch is unsafe:
normal installation may start first. A permanently gated or repeatedly re-gated
ICI is not a valid fallback. Add creation/rerender race tests for the chosen path.

Rendering ownership also applies to the BMH: the IBI template initializes
`online: false` and `externallyProvisioned: true`, while preparation must
change those fields and temporarily set `spec.image`. Later SiteConfig renders
must preserve the active IBI-O/BMO-owned state and annotations. Review the
existing power-field work in
[SiteConfig PR 1446](https://github.com/stolostron/siteconfig/pull/1446), but do
not assume its online-field handling covers all preparation fields. Test label
and annotation updates during preparation and after installation. The direct-ICI
lab run bypassed SiteConfig, so it did not qualify this interaction.

### Supported components and configuration

The deployed combination must include the new ICI schema/controller/server,
matching guest protocol, BMO ownership behavior, matching Ironic metadata/media
handling and existing virtual-media GET support at Ironic API 1.93.
No BMH or DataImage CRD changes are used by the current Metal3 implementation.
ICI CRD/deepcopy/bundle generation is required for the IBI-O API extension.

Define the supported minimum OpenShift/ACM builds and reject an unsupported
combination before requesting preparation. A generic API version or the
existence of a DataImage finalizer does not prove the new behavior is installed.
The lab's custom images and observed platform versions are not that compatibility
contract. Upstream merges and downstream ACM/OpenShift delivery need coordination.

## Configuration ISO and generic bootstrap

The working preparation ISO contains:

```text
Volume label: cluster-config
/
├── cluster-configuration/
│   └── manifest.json       # api_version and raw_nm_state_config
└── ibi-preparation/
    └── config.json         # version, attempt/host identity, callback and deadline
```

The extractor reads the existing `raw_nm_state_config` string. The added
version-1 callback payload contains attempt ID, BMH UID, callback URL/token,
deadline and optional callback CA. Endpoint paths also bind the ICI namespace,
name and UID. Network data continues to come from the BMH-referenced nmstate
Secret; BMO is not expected to apply it to a live-ISO guest.

IBI-O freezes effective inputs in an owned immutable Secret and stores exact
configuration ISO bytes in another immutable Secret for each attempt. Referenced
network or CA changes cannot silently rewrite an active attempt. These Secrets
carry the manager's existing Secret-cache label as well as backup labeling;
without that label the cached client cannot recover them on reconciliation.
Retry uses a new attempt ID, credential and DataImage after the previous release.

The generic bootstrap requires and precedes
`install-rhcos-and-restore-seed.service`. It locates configuration by filesystem
label, materializes private runtime files, applies nmstate and waits for
networking. Missing/invalid required input or failed networking blocks disk
preparation. The existing preparation command must be a foreground oneshot;
its terminal success triggers a callback, and failure invokes a failure reporter.
Callback retries are bounded by the attempt deadline and keep tokens out of
logs and command arguments. The guest leaves power control to IBI-O.

The live environment must already contain `jq`, `curl`, `nmstatectl`,
`nm-online`, `mount` and `udevadm`. No network-dependent tool download can
establish the initial network. Working static configuration removes the DHCP
requirement; otherwise the environment must provide networking such as DHCP.
Hub-side probes cannot prove connectivity from a not-yet-booted target.

The current development utility, `cmd/preparation-ignition`, embeds assets
from `internal/preparation` into complete installer-generated Ignition. It
preserves SSH keys and preparation commands and rejects unsupported unresolved
Ignition inputs. The callback design uses the existing public SSH key for
diagnostics and requires no matching private SSH key Secret.

For product integration, expose a small public versioned bootstrap package,
provisionally `pkg/preparation/bootstrap`, owned by IBI-O. Export the Ignition
transform, assets and protocol/version constants without importing controllers
or the HTTP server. Retain lifecycle/serving logic internally. O-Cloud Manager
cannot import the current `internal/preparation` package across repositories.
Pin the reviewed public package in its module/vendor dependencies, freeze its
version with build inputs, and validate compatibility before an automated boot.

## Callback, transport and storage configuration

The development manager uses operator-owned settings:

| Setting | Purpose |
| --- | --- |
| `PREPARATION_BASE_URL` | HTTPS callback origin reachable from the target. |
| `PREPARATION_CA_FILE` | Optional mounted PEM bundle trusted by the guest. |
| `PREPARATION_ISO_ORIGINS` | Approved HTTPS source origins, including nonstandard ports. |

The internal image service on port 8000 serves verified boot/configuration
images to Ironic. Ironic publishes its own BMC-facing copies. A separate
`image-based-install-preparation` Service on port 8444 accepts callbacks and
has its own serving certificate; it does not serve ISO content. Expose only
the callback listener through the qualified Route/proxy and configure frontend
and backend trust independently. Never expose credential-bearing image paths
through that endpoint.

The guest-to-callback, Ironic-to-image-service, source-ISO-to-controller and
BMC-to-Ironic paths have distinct connectivity/TLS requirements. An ISO-server
CA ConfigMap does not configure the BMC's trust store. NetworkPolicy must allow
the preparation proxy/network to reach 8444 and allow required registry/source
egress, including explicit rules for nonstandard HTTPS ports. Keep the image
listener private. Runtime allowlisting currently checks origins; supported
path-prefix/publication policy still needs deployment configuration/qualification.

The production ACM configuration surface must supply the callback endpoint,
trust, allowed sources, certificates and network policy. Patching an ACM-managed
Deployment, as done for development setup, is not the supported configuration
interface. Agree that surface with ACM before enabling O-Cloud Manager consumers.

Verified boot caches are currently per attempt, under the IBI-O data directory,
with a maximum ISO size of 20 GiB. Pod replacement can discard this local cache;
the manager then fetches and verifies the same source again. Retain source
availability through completion and cleanup. Set aggregate storage/concurrency
limits and qualify cache/publication recovery before scale delivery; a per-ISO
limit alone does not bound a hundred simultaneous attempts.

## Preparation state and ProvisioningRequest sequencing

### Durable ICI state

The development API records the following in `status.preProvisioning`:

- Phase, attempt number/ID, host reference/UID, BMC address and boot MAC.
- Boot/configuration URLs, start time, attempt/overall deadlines and retry time.
- Token hash, immutable terminal result and bounded diagnostic message.
- `deploymentReleased`, the media-teardown checkpoint.

The phases are `PreparingArtifacts`, `Booting`, `Stopping`, `RetryBackoff`,
`Completed` and `Failed`. A persistent `PreProvisioningCompleted` condition is
True only at preparation completion. `RequirementsMet` explains current blocking
or progress reasons. Normal installation can change its reasons without erasing
preparation outcome. Consumers reconcile durable state after missed/coalesced
events; they do not depend on witnessing a transient success transition.

The callback server bounds request size, validates current ICI/attempt/host,
checks the deadline and token in constant time, and persists one result using
fresh API reads and conflict retries. Replayed matching results are idempotent;
stale or conflicting results are rejected. An API outage is retriable, rather
than a false successful acknowledgement. Deadline/result races have focused tests.

### Ordered success handoff

1. Persist authenticated success and revoke its token hash.
2. Request power-off while retaining media; wait for observed power-off.
3. Clear the live image with automatic cleaning disabled and wait for BMO to
   complete deployment teardown and return the host to available.
4. Delete the preparation DataImage and wait for its finalizer/release.
5. Remove attempt Secrets and verified cache; restore the externally-provisioned,
   offline state and persist preparation Completed.
6. Clear IBI-O's preparation host claim, then enter normal configuration.
   Deletion also finishes this release if it interrupts the final handoff.
7. Generate fresh normal installation credentials/media. Only the existing
   normal boot path sets `image-based-install-managed` with its online/reboot
   request. Setting that marker during preparation would suppress disk boot.

BMO's durable keys are `baremetalhost.metal3.io/live-iso-dataimage` and
`dataimage.metal3.io/deployment-owner`; IBI-O has its own preparation owner.
They bind immutable resource/target identities and are controller-owned, rather
than user commands. BMO persists positive release on the DataImage after
successful teardown. Missing ownership metadata is not a release acknowledgement.
The Metal3 proposal describes the exact fields and redaction-safe URL identities.

`deploymentReleased` alone does not mean preparation finalization is finished:
Secret/cache cleanup and handoff can remain. Neither that boolean nor callback
success authorizes O-Cloud Manager to release hardware early.

### ProvisioningRequest observation and budgets

Keep `HardwareProvisioned` for hardware allocation/configuration completion.
Current sequencing must create the ClusterInstance/ICI after that gate; waiting
for ICI preparation at the earlier hardware gate would deadlock object creation.
Track preparation separately, with proposed PR condition
`IBIPreProvisioningCompleted`, then continue normal installation and policies.
The final condition name and extension/status shape need O-Cloud Manager review.

Watch relevant ICI status updates and map the ICI/ClusterInstance ownership to
the owning cluster-scoped ProvisioningRequest. Validate that association before
writing PR status. Report boot/waiting, retry/backoff, cleanup and normal
installation separately. Missing callbacks mean completion was not confirmed,
not proof that disk preparation itself failed. A retriable attempt keeps the
request progressing; only exhausted policy produces terminal failure.

The working ICI attempt policy is:

```text
T_prepare = (maxRetries + 1) * T_attempt + maxRetries * T_backoff
```

This establishes its overall preparation deadline. Retries begin after teardown,
so cleanup time consumes elapsed budget and later attempts can have less time.
Progress messages and restarts do not reset deadlines. Cleanup may outlive a
work deadline when release cannot yet be proven; normal installation stays gated.

O-Cloud Manager must extend its existing overall timeout calculation, rather
than retain the current hardware-plus-cluster cap:

```text
T_request = T_hardware + T_prepare + T_handoff_allowance + T_cluster
```

The teardown/handoff allowance and its configuration source are integration
policy to settle; it is not a new implemented ICI field. Start normal cluster
timing after successful preparation handoff. Test slow allocation, the full
configured preparation/retry budget, delayed cleanup and restart with stored
deadlines. Exceeding a PR budget can report failure, but must not bypass an
ICI finalizer, abandon cleanup or return an in-use server to the pool.

## Failure, retry and deletion

| Case | Required outcome |
| --- | --- |
| Invalid effective inputs, unsupported artifact/component or known disk mismatch | Reject before allocation/boot where the relevant information is available. |
| Guest reports failure or no authenticated result arrives by deadline | Retain the cause or unknown-completion diagnostic; revoke credentials and enter safe teardown. |
| Retry allowed | Wait for old deployment, DataImage and media release; observe backoff; allocate a fresh immutable attempt and credential. |
| Retry budget exhausted | Persist terminal preparation failure and keep normal installation gated. |
| BMC/API unavailable or ambiguous cleanup | Retry observation; retain ownership/finalizers and dependencies rather than claiming release. |
| Host or resource identity changed | Stop actions against that identity; do not operate on another deployment or replacement BMH. |
| PR deleted during preparation | Stop normal work, retain allocation/credentials, delete the ICI through its cleanup path and wait for finalization before dependent hardware release. |

IBI-O's finalizer uses the same ordered stop/release barrier as failure and
success. Once deletion starts, it must not create a retry or initiate normal
installation. O-Cloud Manager must keep the BMH, BMC credentials, trust and
referenced input available until IBI-owned cleanup completes. Manual finalizer
removal or status editing is not an implementation recovery mechanism.

Manager/server/BMO restart and Ironic conductor restart with retained state are
distinct from replacing Ironic and losing its database/publications. The latter
already required manual setup recovery in an earlier experiment. Identity-aware
failure is implemented, but automatic hardware recovery from every interruption
has not been qualified. Define and test that contract before supported delivery.

## Target disk compatibility

The live ISO contains a fixed `installationDisk`. Today's generator takes the
explicit `liveISO.installationDisk` override or derives it from the seed
ClusterInstance node's `rootDeviceHints.deviceName`. Choosing an ISO whose disk
selection and hardware requirements suit the target is an existing prerequisite
of manual preparation at a staging facility. Automation retains that same
prerequisite; it does not introduce a new disk-selection mechanism.

Document the required disk layout for each ISO/template and use hardware
selection to allocate compatible targets. Validate known incompatibilities when
the effective disk configuration is available to the operator. An ISO URL or
digest alone does not establish disk compatibility, and hub-side checks cannot
portably resolve Linux device paths on a server that has not booted.

An incompatible target needs a compatible ISO or a new ISO generated with an
appropriate `liveISO.installationDisk`. The configuration DataImage does not
override that baked-in value. The fresh run verified the intended NVMe and disk
preservation through handoff on the selected hardware.

Digest-bound disk metadata and a live-environment device-identity guard would
be additional safeguards beyond the existing manual workflow. They require
separate scope and API agreement and are not prerequisites or included work
packages in this plan.

## ISO identity and availability

Consume the verified ISO URL/digest produced by seed generation. Its existing
publication layout remains:

```text
<liveISO.urlBase>/seedgen/<ProvisioningRequest.metadata.uid>/rhcos-ibi.iso
```

A unique location and initial HTTPS hash do not enforce future immutability.
The current generic upload publishes an owner-writable file. Keep the source
bytes unchanged and available for active consumers, retries and recovery.

The implemented delivery path is source HTTPS, digest-verified IBI-O cache,
private internal image service, then Ironic's BMC-facing publication. BMCs do
not directly refetch the external source URL. This supplies a concrete mechanism
for booting verified bytes; source trust, retained copies and cleanup still
need an agreed lifetime and recovery contract.

Qualify either protected/versioned source publication, or retained verified
copies covering the entire consumer lifetime. The policy must prevent later
replacement, deletion or redirection from changing bytes served to an active
consumer. A changed source must fail digest revalidation on recovery. Operator
approval must cover the qualified origin/path and retention policy; an HTTP
probe cannot certify arbitrary storage policy. Runtime origin allowlisting
alone is not proof of that policy.

The fresh run establishes first-boot byte binding and successful handoff cleanup.
It does not establish continued availability after loss of controller cache,
Ironic state or publications. Exercise those losses, mutable-source rejection,
retirement while active, and recovery against the original digest before
claiming the retained-copy alternative fully qualified.

## Code locations and API impact

Paths are relative to each repository. Working paths exist in the development
branches; the proposed public asset package and O-Cloud Manager helpers are new.

| Repository | Locations | Scope |
| --- | --- | --- |
| BMO | `internal/controller/metal3.io/`, `pkg/provisioner/` | Working claim, status, finalizer and provisioner changes; see PR 5477 for details. No BMH/DataImage CRD change. |
| Ironic / runtime image | `ironic/drivers/modules/`, affected tests; `main-packages-list.ocp` | Working media/publication/identity handling and `file(1)` packaging. |
| IBI-O | `api/v1alpha1/preprovisioning_types.go`, `imageclusterinstall_types.go`, `imageclusterinstall_webhook.go` | Working optional API, immutability, validation and generated CRD/deepcopy/bundle output. |
| IBI-O | `controllers/preprovisioning.go`, `imageclusterinstall_controller.go`, related tests | Working preparation, input/Secret lifecycle, host ownership, handoff and finalization. |
| IBI-O | `internal/preparation/`, `cmd/server/main.go`, `cmd/preparation-ignition/` | Working ISO payload, download/cache, callback/image handlers and generic assets. Export only the bootstrap interface for O-Cloud Manager. |
| IBI-O / ACM | `config/manager/`, `bundle/`, effective RBAC/network configuration | Working separate callback listener/service; supported ACM configuration, capacity and release integration remain. |
| O-Cloud Manager | `api/provisioning/v1alpha1/clustertemplate_types.go`, `conditions.go`, `provisioningrequest_types.go` | Preparation defaults, persistent PR progress/budgets and generated API output. |
| O-Cloud Manager | `internal/validation/`, `clustertemplate_controller.go`, `provisioningrequest_validation.go` under `internal/controllers/` | Effective input/reference/capability validation and established deep-merge semantics. |
| O-Cloud Manager | `internal/controllers/provisioningrequest_seedgeneration_iso_inputs.go`, related snapshot/Job code; `scripts/seed-generation-iso.sh` | Bootstrap opt-in/version, frozen Ignition input, build and artifact compatibility output. The script is under `internal/controllers/`. |
| O-Cloud Manager | `internal/controllers/provisioningrequest_hwprovision.go`, `provisioningrequest_clusterinstall.go`, controller/setup/finalizer and timeout helpers | Creation ordering, ICI watches, durable status, independent budgets and deletion barrier. |
| O-Cloud Manager | CRD/bundle/RBAC output, `test/utils/vars.go`, `must-gather/gather`, dependency documentation | API generation, external ICI test prerequisites, diagnostics and TLS-scan CRD coverage. |
| SiteConfig integration | ClusterInstance inputs and referenced IBI templates | Establish creation-time configuration; add code/API changes only if the agreed supported path requires them. |

Apply least privilege against generated effective roles, including cached
LIST/WATCH reads and direct status writes. IBI-O's server uses Kubernetes API
access for durable callbacks; its existing shared service account is not proof
that every future permission is needed. O-Cloud Manager observes ICI state and
coordinates inputs; preparation BMH mutations remain IBI-O/BMO-owned. Include
scheme/watch prerequisites and support collection for external ICI resources.

## Reviewable work packages and estimates

Development order now builds on the standalone pass. Package names describe
responsibilities; delivery-stage numbers must not appear in source filenames.
First checkpoint the tested changes and separate local deployment/build artifacts
from feature contributions. Keep the Dockerfile cache cleanup independent.

| Package | Current state / next checkpoint | Remaining effort | Additional non-test LOC allowance |
| --- | --- | --- | --- |
| 1. Metal3 foundation | Working success path; review contracts, prepare component PRs and qualify recovery/legacy behavior. | 2–4 person-days | 100–300 |
| 2. IBI API and lifecycle | Working standalone path; review API and harden failure, deletion and restart behavior. | 2–4 person-days | 100–300 |
| 3. Public bootstrap assets | Working internal assets/utility; expose versioned consumer interface and compatibility tests. | 1–2 person-days | 100–200 |
| 4. O-Cloud Manager ISO generation | Implement opt-in, immutable asset inputs and artifact capability metadata; preserve ordinary generation. | 2–4 person-days | 150–350 |
| 5. O-Cloud Manager provisioning | Implement effective inputs, selected creation path, ICI observation, budget changes and deletion ordering. | 4–7 person-days | 450–800 |
| 6. Combined recovery and regression | Exercise the integrated PR, missed events, failures, retries, interruption and ordinary IBI. | 3–5 person-days | 100–250 |
| 7. Supported delivery and guidance | ACM configuration/certificates/storage, component version matrix, examples, diagnostics and QE handoff. | 2–4 person-days | 100–250 |

These remaining allowances total roughly 16–30 engineering person-days and
1,100–2,450 additional non-test Go/Python/shell lines. Allow approximately
1,500–3,000 additional test lines. They exclude generated manifests/deepcopy,
documentation, existing working code, upstream review queues, release lead time,
hardware availability and QE scale execution. Package 6 covers combined tests,
while earlier rows include their component-focused coverage. Re-estimate after
creation-time rendering, public assets and ACM configuration are agreed;
substantial SiteConfig API work or publication/storage changes need additional
estimation. Optional disk guards/metadata remain outside scope.

For context, the current Metal3 working source changes are 697 added and 40
removed lines across 12 non-test files, plus image packaging; the earlier
69-line prototype is not the complete foundation. IBI-O already has additional
working API/controller/bootstrap/serving code. The remaining estimates above
must not be presented as the total feature implementation size.

Useful next checkpoints are a saved/reviewable standalone baseline, development
recovery qualification, an automatically generated preparation ISO, and the
first ProvisioningRequest reaching `fulfilled`. API-only and documentation
changes do not each require another full hardware installation.

## Development validation and QA acceptance

Development owns the media mechanism, source correctness and race/recovery
proof. The standalone success path is hardware-verified. BMO unit tests and
238 affected Ironic tests, including real API-redaction coverage, passed;
affected IBI-O tests and scoped/configured lint checks passed. These results
do not stand in for upstream CI or the remaining hardware cases.

Before supported delivery, development must cover:

- Ordinary IBI without preparation and live ISO without required configuration.
- Occupied/unknown slots, format/TLS/download errors and unsupported versions.
- Exact input/target binding, immutable configuration, stale/replayed callbacks,
  timeout/result races and absence of a false success for a skipped service.
- Manager and server restarts, BMO restart and Ironic restart, distinguishing
  retained-state recovery from database/publication loss.
- Failure callback, missing callback, bounded retry, cancellation during boot
  and preparation, and deletion during attachment and cleanup.
- Positive release before finalization, preserved prepared disk, and no normal
  installation or allocation reuse while cleanup remains uncertain.
- Creation/rerender races, BMH field ownership during metadata updates, missed
  ICI updates, persistent PR outcome and the
  complete overall timeout budget after slow hardware allocation.
- Verified-copy/source retention, disconnected trust/egress and aggregate
  capacity/concurrency limits before large-scale qualification.

For functional O-Cloud Manager changes, run `make ci-job` and appropriate
`make test-e2e` coverage before submission; regenerate APIs/manifests/bundles
when required. Documentation changes use `make markdownlint`. Each other
repository retains its own unit, lint, generation and integration checks.

QA validates supported user-visible behavior rather than rediscovering the
basic delivery mechanism:

| Scenario | Acceptance outcome |
| --- | --- |
| Unprepared SNO with preparation configured | One PR drives preparation and installation to a healthy accessible cluster and `fulfilled`. |
| Already prepared SNO with preparation omitted | Existing IBI installation succeeds without a preparation boot. |
| Representative failure/timeout | Useful terminal PR failure after the configured budget; normal installation stays gated. |
| Retry followed by success | PR remains progressing through backoff/retry and subsequently completes. |
| Delete PR during preparation | Cleanup completes without stranded dependent resources or unsafe hardware reuse. |
| Representative operator restart | Workflow resumes without duplicate destructive preparation or lost final outcome. |
| Supported DHCP/static networks | Documented networking and trust prerequisites support preparation and installation. |
| Up to 100 SNOs in parallel | Independent workflows retain target/result isolation and make progress within qualified capacity limits. |

Record transitions, including short-lived handoff states, and observe actual
power/media state rather than only desired fields. QA can reference development
coverage for callback authentication, payload layout and detailed controller races.

## Decisions remaining for review and delivery

1. Accept the implemented deployment-owned Metal3 direction, annotation and
   URL-fingerprint contracts, opt-in and component-skew behavior.
2. Accept the two successive configuration DataImages and existing layout reuse,
   refining the earlier same-resource handoff design.
3. Select the supported creation-time SiteConfig input path or qualify an explicit
   gate, including ICI and BMH rerender ownership, and identify necessary upstream changes.
4. Agree the public bootstrap package, protocol/version metadata, template opt-in
   and artifact compatibility checks before O-Cloud Manager integration.
5. Agree the separate PR preparation condition/status and overall timeout policy,
   including handoff allowance and allocation-retention behavior on failure.
6. Agree ACM-owned endpoint/trust/network/storage configuration, required component
   releases and the supported restart/state-loss recovery contract.
7. Qualify publication/retention and aggregate capacity, document compatible target
   disks, and complete the remaining development and QA matrix.

SSH polling remains an alternative requiring a known reachable address, private
key Secret, host-key verification and current-boot/target correlation. It does
not itself deliver per-host static networking. The demonstrated callback design
avoids that polling dependency; selecting SSH instead would revise these API,
bootstrap, configuration and qualification contracts.
