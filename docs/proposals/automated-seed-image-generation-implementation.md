<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Automated seed image generation: implementation design

## Purpose and authority

This document plans the implementation of [CNF-26348](https://redhat.atlassian.net/browse/CNF-26348)
in O-Cloud Manager. The merged [feature proposal](./automated-seed-image-generation.md)
defines the product behavior. This document maps that behavior onto the current
repository, identifies integration work, and estimates effort. It does not
replace the feature proposal.

The Jira epic still describes an earlier ACM recovery design that preserves the
ManagedServiceAccount and restores addons. Implement the merged proposal's
disposable seed cluster instead: retain the hub `ManagedCluster`, remove all
spoke ACM agents before capture, use the hub-held admin kubeconfig for spoke
access, and leave ACM detached. `liveISO` remains optional so a seed can also
be produced for IBU without building installation media.

## Existing code and required changes

| Area | Current code | Planned change |
| --- | --- | --- |
| PR API and conditions | `api/provisioning/v1alpha1/provisioningrequest_types.go`, `conditions.go` | Add seed status, snapshot UIDs, CA reference, condition and reasons. Isolate seed failures from upgrade conditions. |
| Schema and admission | `internal/validation/seedgeneration.go`, `api/provisioning/v1alpha1/provisioningrequest_validation.go`, `provisioningrequest_webhook.go` | Keep restricted raw input validation; reject seed input and timeout edits after run start. Validate the merged config separately. |
| CT validation | `internal/controllers/clustertemplate_controller.go` | Recognize the third type; validate seed defaults against a full internal schema while preserving CV/IBGU behavior. |
| Merge and parse | `internal/controllers/provisioningrequest_upgrade.go`, `internal/controllers/utils/constants.go` | Parse `seedGeneration`; use the existing deep merge and full schema for effective config. |
| Reconcile dispatch | `internal/controllers/provisioningrequest_controller.go`, `provisioningrequest_clusterconfig.go`, `provisioningrequest_setup.go` | Route seed runs before upgrades and ACM-dependent phases; watch or poll the Job. |
| Spoke access | `internal/service/common/clients/k8s/k8s.go`, `internal/spokeclient/spokeclient.go` | Read the exact ClusterDeployment admin-kubeconfig Secret; stop using MSA access after detachment. |
| Cleanup and permissions | `internal/controllers/provisioningrequest_controller.go`, RBAC markers, generated `config/rbac/role.yaml` | Scrub seed resources before NAR deletion; regenerate and inspect hub permissions. |
| User guidance | `docs/user-guide/ibi-based-cluster-provisioning.md`, `docs/samples/` | Add CT, PR, and Secret samples; explain seed-only and seed-plus-ISO outcomes, terminal failures, digest and URL status, and manual PR deletion to reclaim hardware. |
| ISO toolchain | `config/manager/manager.yaml`, `internal/constants/constants.go`, `telco5g-konflux/` release overlay, generated bundle CSV | Supply a pinned Red Hat tools image reference to the controller, include it in `relatedImages`, and use it for the preflight Pod and ISO Job. |

New controller files should remain in `internal/controllers/` alongside the
existing ProvisioningRequest code:

- `provisioningrequest_seedgeneration.go`: state machine, condition and status
  transitions, timeout, ownership checks, and cleanup coordination.
- `provisioningrequest_seedgeneration_preflight.go`: prerequisite checks,
  credential resolution, release digest pinning, and installer preflight.
- `provisioningrequest_seedgeneration_acm.go`: exact kubeconfig lookup,
  spoke client, addon and Klusterlet removal, and teardown polling.
- `provisioningrequest_seedgeneration_iso.go`: mirror/trust resolution,
  workspace and Job resources, result inspection, and artifact verification.
- `seedgeneration-assets/`: embedded Job scripts and configuration templates
  kept out of large inline Go strings.

Each new behavior gets focused unit or envtest coverage in adjacent `_test.go`
files. Generated deepcopy, CRD, RBAC, and bundle files are committed after
regeneration, but are excluded from the handwritten LOC estimate below.

## API and validation contract

`upgradeDefaults.seedGeneration` is mutually exclusive with `clusterVersion`
and `imageBasedGroupUpgrade`. A PR may override only fields exposed by its
ClusterTemplate's `templateParameterSchema`. The safe default is an empty
`seedGeneration` object with `additionalProperties: false`; trusted templates
may expose `seedImage`. The controller does not accept PR overrides of Secret
references, release images, URLs, mirror sources, CA references, or
`recertImage` through the default schema.

The current `validateUpgradeDefaultsAgainstSchema` validates CT defaults
against the PR-facing schema, and `mergeAndValidateUpgradeData` does the same
after merging. Both paths need a seed branch that uses one strict internal
effective-object schema. Put reusable seed rules and the full schema in
`internal/validation/seedgeneration.go`, with thin adapters in the API
package for the webhook and existing callers. Call the shared validator
from CT validation and the controller's post-merge validation. The existing
`ProvisioningRequest.ValidateUpgradeInput` runs at admission on raw PR input,
before controller merge; it must not require template-owned fields from a
partial override. This places full validation at the actual post-merge point
without changing the accepted two-schema behavior.

Add the proposal's status fields: `StartedAt`, `DetachmentStarted`,
`ReleaseImageDigest`, `SeedImage`, `ISOURL`, `ISODigest`, and
`ISOServerCACertRef`. Also add `InputSnapshotResourceUIDs`, a map of the
per-run ConfigMap and Secret object UIDs. This implementation-only status
field pins the exact frozen objects without exposing their contents. Persist
the release digest and complete input snapshot before ACM removal, and the
seed image digest before starting the ISO Job. Conditions encode the current
phase. A terminal `Completed`, `Failed`, `TimedOut`, or
`PreconditionChecksFailed` state never triggers a second run; a new PR is
required. The webhook rejects changes to the PR's `seedGeneration` and
`seedGenerationTimeout` inputs after `StartedAt` is set, since the run uses
its frozen inputs and a later edit would otherwise appear to take effect.

Use a separate `seedGenerationTimeout` input in ClusterTemplate
`upgradeDefaults` and optional ProvisioningRequest `upgradeParameters`.
Do not reuse `clusterUpgradeTimeout`, which remains specific to cluster
upgrades. The seed defaults are two hours without `liveISO` and three
hours with it. A positive PR value overrides the template default.
Calculate remaining time from persisted `StartedAt`, including across
controller restarts, and bound the ISO Job's `activeDeadlineSeconds`
to that remaining budget.

## Reconcile and resource lifecycle

1. After ZTP Done, detect a merged `seedGeneration` key before calling
   `IsUpgradeRequested`. Do not gate the seed trigger on version equality.
   Phase 1 reports either direction of version mismatch as terminal
   `PreconditionChecksFailed`.
2. While a seed run is active, or after detachment has begun, route to the
   seed state machine early in `run`. A terminal pre-detachment
   `PreconditionChecksFailed` remains one-shot but continues normal
   reconciliation of the still-attached cluster. The existing
   `executeProvisioningPhases` and
   `handlePostProvisioning` can otherwise repeat ACM-dependent work before
   the seed handler runs. In particular, policy configuration currently runs
   before upgrade dispatch. After detachment, skip policy compliance checks
   and leave their last pre-detachment status intact.
3. Phase 1 reads the effective config and source credentials into one
   in-memory input set, then validates that set against ZTP/version, LCA,
   OADP, shared `/var/lib/containers`, registry push access, and, when
   configured, ISO credentials, HTTPS trust, release accessibility, mirror
   behavior, and installer extraction. Persist an immutable per-run snapshot
   of the validated config, derived mirror/CA data, and credential copies in
   the operator namespace; the short-lived preflight Pod runs the pinned Red
   Hat tools image and uses those copies.
   Persist the immutable release digest before moving to Phase 2. Fail and
   scrub partial snapshots before detachment when a precondition is unmet.
4. Phase 2 obtains and tests the exact spoke admin kubeconfig, then includes
   its bytes in an immutable per-run hub Secret. Require the complete input
   snapshot before persisting `DetachmentStarted=true` **before** deleting the
   first addon CR. Delete every hub `ManagedClusterAddOn` in the spoke
   namespace, then the spoke `Klusterlet` CR. Wait until agent namespaces,
   pods, and identity secrets are gone. The seed CT supplies
   `observability: disabled` at import time.
5. Phase 3 creates or adopts the spoke `seedgen` Secret from the frozen hub
   credential copy and singleton `SeedGenerator` (`seedimage`), monitors LCA
   conditions with requeue, and records only the digest reported by the
   successful push. A registry lookup of the mutable tag does not prove
   which image this run published.
6. When `liveISO` is absent, proceed to cleanup. Otherwise Phase 4 uses the
   frozen per-run Secrets and mirror/CA ConfigMap, then creates a roughly
   20 GiB PVC and a Job using the same Red Hat tools image by digest. The Job
   extracts the installer from `ReleaseImageDigest`, builds an
   `ImageBasedInstallationConfig` using the pinned `SeedImage` and generic
   IBI callback Ignition override, builds the ISO,
   uploads it with pinned SSH host keys, and verifies the remote SHA-256 over
   SSH. It returns only small, structured non-secret results, such as digest
   and resolved URL, through the pod termination message. The controller
   records `ISOURL` only after path and host/content binding checks pass.
7. If an HTTPS CA was provided, Phase 5 first materializes `ca-bundle.crt`
   from the frozen CA bytes in a stable ConfigMap and records
   `ISOServerCACertRef` for downstream consumers. It then deletes the
   input/mirror ConfigMap, all credential copies, Job, PVC, preflight Pod,
   spoke SeedGenerator, and `seedgen` Secret on success. On failure or timeout,
   scrub credential-bearing resources immediately and
   retain only bounded, redacted diagnostics for a finite TTL. The existing
   PR finalizer performs the same scrub before NAR deletion when the PR is
   deleted mid-run. The cluster remains running and detached until the
   operator deletes the PR.

Every create uses a deterministic name plus labels containing PR name and UID.
On `AlreadyExists`, adopt only a resource labeled for that exact UID. Reject
unowned or foreign resources with the same name. Hub namespaced resources
also use an owner reference to the cluster-scoped PR as a garbage-collection
backstop; the finalizer still deletes them promptly. The feature proposal
says a namespaced CA ConfigMap cannot have a cluster-scoped owner, but
[Kubernetes ownership rules](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
explicitly allow that relationship. Confirm owner-reference behavior in an
envtest and keep explicit finalizer cleanup for the durable CA ConfigMap.

The persisted condition and status decide recovery after a controller restart:

| Persisted state | Next reconcile |
| --- | --- |
| No seed condition, ZTP Done, `seedGeneration` present | Start Phase 1; set `StartedAt` and `Validating`. |
| `Validating`, `DetachmentStarted=false` | Adopt complete Phase 1 copies and repeat preflight against them; discard incomplete owned copies and start Phase 1 again. |
| `CleaningACMResources`, `DetachmentStarted=false` | Validate and copy the admin kubeconfig, then confirm all frozen inputs before marking detachment and deleting addons. |
| `CleaningACMResources`, `DetachmentStarted=true` | Use only the snapshot; resume deletion and teardown polling; never re-enter normal ACM reconciliation. |
| `InProgress`, no `SeedImage` digest | Use the snapshot to adopt or create LCA resources, then poll SeedGenerator. |
| `BuildingISO`, `SeedImage` present | Use frozen inputs to adopt or create the PVC and Job; inspect Job result. |
| Terminal condition | Scrub any remaining transient resources; never create a new seed run for this PR. A pre-detachment failure may continue normal cluster reconciliation. |

### Frozen inputs and recovery

Before the detachment marker is written, store the validated effective
`seedGeneration` configuration, CT release, timeout, pinned release digest,
resolved mirror mappings, and CA trust in an immutable per-run ConfigMap in
the operator namespace. Store only the required validated credential bytes in
immutable per-run Secrets there: seed push auth, effective ISO pull auth, ISO
upload credentials and trust material when `liveISO` is present, and the spoke
admin kubeconfig selected by the `ClusterDeployment`. Do not put credential
bytes in PR status or a ConfigMap. The short-lived preflight Pod and later ISO
Job mount only the copies they need; the spoke `seedgen` Secret is made from
the seed-auth copy. Derive the stable ISO-server CA ConfigMap from the frozen
CA bytes, not from a later read of the source Secret.

Use deterministic names and PR UID ownership checks for this snapshot. Read
each source object before validating it, and create its copy from those same
in-memory bytes. A restart before detachment adopts complete, immutable
Phase 1 copies and re-runs any unfinished preflight against them. Incomplete
Phase 1 copies are deleted and rebuilt from newly read, revalidated inputs
while the spoke is still attached. Phase 2 adopts an existing admin-kubeconfig
copy or validates and creates it before any addon deletion. Confirm the full
snapshot and, when `liveISO` is present, the persisted release digest before
writing `DetachmentStarted=true` and the snapshot object UIDs in one status
update. On restart, require the persisted release digest to match the frozen
value. After the marker, never re-read CT defaults, PR overrides, source
Secrets, or hub mirror/CA objects to drive this run. Compare each owned,
immutable object's UID against status on recovery. Missing or replaced
snapshot objects fail an active run and trigger credential cleanup; they are
never filled from changed source data.

Changing a ClusterTemplate or source Secret during an active run therefore
cannot alter Phase 3, Phase 4, or restart behavior. Reject PR
`seedGeneration` or `seedGenerationTimeout` edits after `StartedAt` so users
are not shown accepted changes that the running operation will ignore.
Creating ISO credential copies earlier extends their lifetime; do not mount
the upload copy before the ISO Job, and scrub all copies on every terminal,
timeout, or deletion path.

## ISO-specific integration

### ISO Job image decision

The merged proposal calls for an operator-owned, immutable-digest image with
`oc`, POSIX shell, `ssh`, `scp`, and `sha256sum`. The controller creates a
Phase 1 preflight Pod and a Phase 4 ISO Job through the Kubernetes API; those
pods run the tools. The manager never invokes `oc` or copies binaries between
images. The image choice does not change the Job script, credentials,
permissions, or workspace.

The original implementation plan proposed a custom `Dockerfile.iso-builder`
and a separately built and published tool image. We then considered changing
the manager image from distroless to a CLI-capable base and using it for the
Job. Both approaches leave us responsible for maintaining the tool packages
and responding to image CVEs: the first adds a build and publication stream,
while the second puts the tools in every long-running service container.

| Approach | Benefit | Cost |
| --- | --- | --- |
| Custom ISO image: original plan | Keeps the manager distroless and limits tools to ISO pods. | Build, publish, scan, patch, and mirror another image ourselves. |
| Shared operator image: considered | Reuses the existing image publication path. | Changes the manager base and carries the full toolset in every service pod. |
| Red Hat OpenShift tools image: selected | Red Hat builds and updates the toolset; manager stays distroless; no custom image build. | Add a pinned external image to the bundle and mirror it; qualify each new digest. |

**Decision: use `registry.redhat.io/openshift4/ose-tools-rhel9` for both ISO
pods.** The Red Hat [OpenShift tools image](https://catalog.redhat.com/en/software/containers/openshift4/ose-tools-rhel9/652809d13aa6bf4a998c7579)
is built from the [OpenShift tools Dockerfile](https://github.com/openshift/oc/blob/release-4.22/images/tools/Dockerfile).
On 2026-09-28, the AMD64 `v4.22` image was checked directly for `oc`, shell,
`ssh`, `scp`, and `sha256sum`; `oc` and `ssh` also ran as a non-root UID.
Its manifest list covers AMD64, ARM64, s390x, and ppc64le. The simpler
`ose-cli-rhel9` image checked locally (`v4.22.0`) lacks `ssh` and `scp`; it is
the base used by `Dockerfile.must-gather`. The tools Dockerfile does not
explicitly list the OpenSSH client package, so verify both commands for every
new digest.
The operator `Dockerfile` remains distroless and no ISO image build target is
added. `openshift-install` is still extracted from the pinned release image
at runtime, not baked into the tools image.

This intentionally replaces the merged proposal's **operator-owned image**
requirement with a Red Hat-built image that the operator references and
validates. Add `ISO_TOOLS_IMAGE` to `config/manager/manager.yaml` and read it
from the controller; the value used by an enabled `liveISO` run must be an
immutable manifest-list digest. Extend the existing release pin/mapping
metadata in `telco5g-konflux/` and the resulting bundle `relatedImages`, as
already done for `POSTGRES_IMAGE`. The preflight Pod and Job use that same
reference. Disconnected deployments mirror it alongside the other related
images. Neither a PR nor a ClusterTemplate can override the tool image.

The Red Hat Trusted Artifact Signer operator provides a direct precedent: its
[v1.4.3 catalog bundle](https://github.com/securesign/fbc/blob/61a9b73502f8afc9f49d66df93770a6e5d92684f/v4.22/rhtas-operator/catalog/rhtas-operator/catalog.json#L2838)
lists `ose-tools-rhel9` by digest in `relatedImages` as `trillian-netcat`, and
its [image configuration](https://github.com/securesign/secure-sign-operator/blob/430cd4fef47d291fbefabee833a1590da45c2a94/config/default/images.env#L11)
sets `RELATED_IMAGE_TRILLIAN_NETCAT` to a tools image digest. This confirms
the packaging pattern for an external OpenShift tools image. Our required
commands and execution environment still need their own qualification.

Red Hat owns the image build and package updates, but our release still owns
digest selection, CVE triage, compatibility checks, and refreshing the pin.
This is a broad diagnostic image, so its size and package count remain a
security and download cost. For every selected digest, qualify all required
commands, non-root execution with a writable workspace, supported
architectures, release extraction, and disconnected pull behavior. Phase 1
fails before PVC creation or ACM detachment if the pinned image cannot run the
required tools or extract `openshift-install`.

Mirror resolution reads hub IDMS, ITMS, ICSP, and additionalTrustedCA data.
Keep ICSP's `repositoryDigestMirrors` field and `operator.openshift.io` RBAC
group distinct from IDMS/ITMS. Preserve `mirrorSourcePolicy` when creating
`idms.yaml`. For ITMS-only tag resolution, derive and verify a digest mirror
mapping before Phase 4 uses the pinned digest. Explicit `liveISO` mirror and
trust settings replace the corresponding hub-derived values. Tests cover
connected, IDMS, ICSP-only, ITMS-only, and untrusted mirror cases.

Credentials referenced by `seedAuthSecretRef`, `uploadSecretRef`, and optional
`pullSecretRef` always resolve in the ClusterTemplate namespace. The fallback
ISO pull secret merges the hub pull secret with seed registry credentials;
Phase 1 checks access to both the seed and release registries and freezes the
effective pull secret with the other per-run credentials in the Job namespace.
Do not put credentials in command arguments, condition messages, pod
termination messages, or unredacted retained logs.

The proposal requires the SSH upload path and public HTTPS URL to address
the same bytes. Before implementing Phase 4, specify a checkable mapping
between `uploadSecretRef.remotePath` and `liveISO.urlBase`; the present inputs
do not themselves describe the server's document root. Until a same-store
relationship is proven, hash the actual HTTPS response as required by the
proposal. Treat a path mismatch, host mismatch, digest mismatch, or unknown
mapping as `Failed`, and do not publish `ISOURL`.

## Permissions and generated artifacts

Add or adjust `//+kubebuilder:rbac` markers for hub Jobs, Pods/logs, PVCs,
Secrets, ConfigMaps, ClusterDeployments, ManagedClusterAddOns, image mirror
resources, and the source Secrets/ConfigMaps. The effective operator
ClusterRole is generated into `config/rbac/role.yaml`; inspect it after
`make manifests`. The current role already grants some broad Secret, PVC,
and ConfigMap operations, but lacks several of the new API groups and addon
deletion. Do not infer effective access from one marker alone. The spoke
client uses the hub-held admin kubeconfig and creates no spoke RBAC.

Regenerate `api/provisioning/v1alpha1/zz_generated.deepcopy.go`, the PR CRD
under `config/crd/bases/`, RBAC, and bundle manifests after API/marker
changes. Update vendored LCA API types if the selected dependency version
does not include `SeedGenerator`. Generated and vendored changes are tracked
in the PR but excluded from the LOC estimate.

## Reviewable work packages and estimates

Estimates count handwritten added or materially changed lines, including
tests and supporting scripts/docs. They exclude generated CRD, bundle,
deepcopy, and vendored lines. They are planning estimates, roughly ±25%,
for one engineer familiar with the repository. Calendar time also depends on
access to an ACM/MCE seed SNO, a registry, and an ISO server.

| Package | Main files and deliverable | Go LOC | Test LOC | Other LOC | Effort |
| --- | --- | ---: | ---: | ---: | ---: |
| 1. Contract and API | PR types/conditions, internal schema, CT validation, parse/merge, samples | ~250 | ~300 | ~25 | 2–3 days |
| 2. Dispatch and durability | Reconciler fast path, one-shot gate, timeout, active-run webhook, finalizer wiring | ~350 | ~350 | — | 3–5 days |
| 3. Preflight and input snapshot | Spoke/registry/TLS/release checks, frozen config and credentials, digest pinning, installer Pod | ~500 | ~500 | ~20 | 4–6 days |
| 4. ACM and LCA | Exact admin client, teardown, SeedGenerator lifecycle, cleanup, RBAC | ~450 | ~400 | ~30 | 4–6 days |
| 5. Live ISO | Frozen credential mounts, PVC/Job, Red Hat tools image, upload verification | ~900 | ~600 | ~50 | 6–9 days |
| 6. Integration and guidance | Cross-phase tests, user guide, sample manifests, release wiring | ~100 | ~200 | ~200 | 2–3 days |
| **Total** | | **~2,550** | **~2,350** | **~325** | **21–32 days** |

The midpoint is about **5,200 handwritten lines**, using the Red Hat tools
image for the ISO Job. A custom image would add roughly 50–100 LOC and
1–2 days of image build and pipeline work. The merged proposal's
~2,900-line estimate is likely low because this plan includes the restricted
schema refactor, preflight Pod, external image qualification, restart and
cleanup tests, and content verification. An upstream LCA change, a new API
field for path binding, or a custom image build pipeline would add effort.
The effort estimate includes a short contract check in package 1, but excludes
waiting for upstream changes or access to a suitable validation cluster.

Deliver the packages in order, with the seed-only path usable after package
4. Package 5 adds optional ISO generation. Each package should include its
own behavior tests so the final integration pass focuses on interaction
between phases rather than a large backlog of untested code.

Suggested review sequence: merge package 1 as the API/schema foundation;
land packages 2 and 3 as inactive routing and safe preflight code; enable
the seed-only path with package 4 after real ACM teardown validation; add
optional ISO support with package 5; finish package 6 with docs, generated
bundle, and end-to-end evidence. Each PR must compile and keep the public
trigger inactive until its full path and cleanup are ready.

## Validation and acceptance

- Schema tests: exactly one of three operation types; partial `seedImage`
  override; rejection of template-owned PR fields, unknown defaults, and
  malformed effective config.
- State tests: equal and both mismatched OCP version directions; restart
  after each create/status write; own-resource adoption; foreign-name
  collision; one-shot terminal behavior; active-run edit rejection; timeout
  and deletion cleanup.
- Snapshot tests: CT defaults, PR input, source Secret data, and hub mirror/CA
  changes after preflight do not alter Phase 3, Phase 4, or restart behavior;
  a partial pre-detachment snapshot is discarded and revalidated; a missing
  or altered post-detachment snapshot fails and scrubs credentials.
- ACM tests: admin kubeconfig selected by ClusterDeployment reference;
  no destructive action when client setup fails; durable detachment marker
  before addon deletion; all addons and Klusterlet removed; no policy
  reconciliation after detachment.
- Seed tests: prerequisite failures, LCA failure and transient API errors,
  authoritative digest capture, missing digest failure, seed-only success.
- ISO tests: both registry credentials, IDMS/ITMS/ICSP and CA paths,
  pinned tools image and installer preflight, Job restart/failure, log redaction,
  SSH host-key checking, remote and HTTPS digest verification, credential copies
  scrubbed on every terminal path.
- Cluster validation: on the target ACM/MCE and LCA versions, inspect the
  captured seed for absent hub registration/observability artifacts, verify
  registry push, and boot or consume the generated ISO from its HTTPS URL.

For implementation PRs, run relevant focused tests and lint, regenerate
after API/RBAC changes, then run `make ci-job` and `make test-e2e` before
submission as required by `AGENTS.md`. Run `make markdownlint` for docs.
The Jira epic also calls for release enablement, downstream documentation,
QE evidence, and RDS reference CR updates. Those cross-repository deliverables
need owners and tracking but are outside this repository LOC estimate.

## Decisions to resolve before the destructive or ISO phases

1. **LCA digest contract:** the currently vendored LCA dependency exposes
   ImageBasedUpgrade types but no SeedGenerator API. Verify the LCA version
   deployed with the target OCP reports an immutable digest produced by its
   push. If it does not, coordinate an LCA API change; a later registry tag
   lookup does not meet the proposal's integrity rule.
2. **ISO path binding:** define the remote document-root convention or add a
   trusted template field that makes `remotePath` to `urlBase` mapping
   verifiable. Keep the mandatory HTTPS hash path for different hosts.
3. **External tools image contract:** select a supported Red Hat tools image
   version and manifest-list digest for each release. Verify the required
   commands on supported architectures, non-root execution in the target
   OpenShift cluster, compatibility with the configured release image, and
   disconnected mirroring. Define who refreshes the pinned digest when Red Hat
   publishes security updates.
4. **ACM teardown proof:** validate Klusterlet and addon deletion against the
   target ACM/MCE version before relying on it to produce a clean seed.
