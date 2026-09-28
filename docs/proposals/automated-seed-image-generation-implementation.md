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
| PR API and conditions | `api/provisioning/v1alpha1/provisioningrequest_types.go`, `conditions.go` | Add `SeedGenerationStatus` under `ClusterDetails`, `ConfigMapKeyRef`, `SeedGenerationCompleted`, and the missing reasons. Keep seed failures out of generic upgrade condition handling. |
| Schema and admission | `api/provisioning/v1alpha1/provisioningrequest_validation.go`, `provisioningrequest_webhook.go` | Keep restricted raw input validation; reject terminal-run edits. Validate the merged config separately. |
| CT validation | `internal/controllers/clustertemplate_controller.go` | Recognize the third type; validate seed defaults against a full internal schema while preserving CV/IBGU behavior. |
| Merge and parse | `internal/controllers/provisioningrequest_upgrade.go`, `internal/controllers/utils/constants.go` | Parse `seedGeneration`; use the existing deep merge and full schema for effective config. |
| Reconcile dispatch | `internal/controllers/provisioningrequest_controller.go`, `provisioningrequest_clusterconfig.go`, `provisioningrequest_setup.go` | Route seed runs before upgrades and ACM-dependent phases; watch or poll the Job. |
| Spoke access | `internal/service/common/clients/k8s/k8s.go`, `internal/spokeclient/spokeclient.go` | Read the exact ClusterDeployment admin-kubeconfig Secret; stop using MSA access after detachment. |
| Cleanup and permissions | `internal/controllers/provisioningrequest_controller.go`, RBAC markers, generated `config/rbac/role.yaml` | Scrub seed resources before NAR deletion; regenerate and inspect hub permissions. |
| User guidance | `docs/user-guide/ibi-based-cluster-provisioning.md`, `docs/samples/` | Add CT, PR, and Secret samples; explain seed-only and seed-plus-ISO outcomes, terminal failures, digest and URL status, and manual PR deletion to reclaim hardware. |
| Tool image | `Dockerfile.iso-builder`, `Makefile`, `config/manager/manager.yaml`, `telco5g-konflux/` | Build, publish, pin, and mirror the ISO Job image. |

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
effective-object schema. Put that schema near the existing API JSON schema
helpers, for example in
`api/provisioning/v1alpha1/seedgeneration_schema.go`, and call it from CT
validation and the controller's post-merge validation. The existing
`ProvisioningRequest.ValidateUpgradeInput` runs at admission on raw PR input,
before controller merge; it must not require template-owned fields from a
partial override. This places full validation at the actual post-merge point
without changing the accepted two-schema behavior.

Add status fields exactly as specified by the proposal: `StartedAt`,
`DetachmentStarted`, `ReleaseImageDigest`, `SeedImage`, `ISOURL`, `ISODigest`,
and `ISOServerCACertRef`. Persist the release digest before ACM removal and
the seed image digest before starting the ISO Job. Conditions encode the
current phase. A terminal `Completed`, `Failed`, `TimedOut`, or
`PreconditionChecksFailed` state never triggers a second run; a new PR is
required. The webhook rejects a late `seedGeneration` edit with that guidance.

Use the existing `clusterUpgradeTimeout` input, with seed defaults of two
hours without `liveISO` and three hours with it. Calculate remaining time
from persisted `StartedAt`, including across controller restarts, and bound
the ISO Job's `activeDeadlineSeconds` to that remaining budget.

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
3. Phase 1 validates the effective config, ZTP/version, LCA, OADP, shared
   `/var/lib/containers`, registry push access, and, when configured, ISO
   credentials, HTTPS trust, release accessibility, mirror behavior, and
   installer extraction. Use a short-lived preflight Pod for tools absent
   from the operator image. Persist the immutable release digest before
   moving to Phase 2. Fail before detachment when a precondition is unmet.
4. Phase 2 obtains and tests the exact spoke admin kubeconfig. Persist
   `DetachmentStarted=true` **before** deleting the first addon CR. Delete
   every hub `ManagedClusterAddOn` in the spoke namespace, then the spoke
   `Klusterlet` CR. Wait until agent namespaces, pods, and identity secrets
   are gone. The seed CT supplies `observability: disabled` at import time.
5. Phase 3 creates or adopts the spoke `seedgen` Secret and singleton
   `SeedGenerator` (`seedimage`), monitors LCA conditions with requeue, and
   records only the digest reported by the successful push. A registry lookup
   of the mutable tag does not prove which image this run published.
6. When `liveISO` is absent, proceed to cleanup. Otherwise Phase 4 creates
   per-run Secrets and mirror/CA ConfigMaps in the operator namespace, a
   roughly 20 GiB PVC, and a Job using a digest-pinned tool image. The Job
   extracts the installer from `ReleaseImageDigest`, builds an
   `ImageBasedInstallationConfig` using the pinned `SeedImage` and generic
   IBI callback Ignition override, builds the ISO,
   uploads it with pinned SSH host keys, and verifies the remote SHA-256 over
   SSH. It returns only small, structured non-secret results, such as digest
   and resolved URL, through the pod termination message. The controller
   records `ISOURL` only after path and host/content binding checks pass.
7. Phase 5 deletes credential copies, Job, PVC, preflight Pod, mirror
   ConfigMap, spoke SeedGenerator, and `seedgen` Secret on success. If an
   HTTPS CA was provided, materialize `ca-bundle.crt` in a stable ConfigMap
   and record `ISOServerCACertRef` for downstream consumers. On
   failure or timeout, scrub credential-bearing resources immediately and
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
| `Validating`, `DetachmentStarted=false` | Repeat safe preflight checks; continue only after release pinning succeeds. |
| `CleaningACMResources`, `DetachmentStarted=true` | Resume deletion and teardown polling; never re-enter normal ACM reconciliation. |
| `InProgress`, no `SeedImage` digest | Adopt or create LCA resources, then poll SeedGenerator. |
| `BuildingISO`, `SeedImage` present | Adopt or create the PVC, Secrets, ConfigMap, and Job; inspect Job result. |
| Terminal condition | Scrub any remaining transient resources; never create a new seed run for this PR. A pre-detachment failure may continue normal cluster reconciliation. |

## ISO-specific integration

The current manager image is distroless and has no `oc`, shell, SSH, or SCP.
Add a separate ISO-builder image definition (for example
`Dockerfile.iso-builder`) and a build/publication target in `Makefile`.
Publish and reference it by immutable digest through a manager environment
variable and bundle `relatedImages`; add the matching release pipeline
metadata under `telco5g-konflux/`. Phase 1 checks that the image can execute
the required tools and extract `openshift-install` before allocating the
workspace PVC. The image build and release wiring are part of this feature,
not an assumed external dependency.

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
Phase 1 checks access to both the seed and release registries. Copy only the
resolved per-run credentials into the Job namespace. Do not put credentials
in command arguments, condition messages, pod termination messages, or
unredacted retained logs.

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
| 2. Dispatch and durability | Reconciler fast path, one-shot gate, timeout, webhook, finalizer wiring | ~325 | ~325 | — | 3–4 days |
| 3. Preflight | Spoke/registry/TLS/release checks, digest pinning, installer Pod | ~400 | ~350 | — | 3–5 days |
| 4. ACM and LCA | Exact admin client, teardown, SeedGenerator lifecycle, cleanup, RBAC | ~450 | ~400 | ~30 | 4–6 days |
| 5. Live ISO | Mirror/CA, credential copies, PVC/Job, tool image, upload verification | ~900 | ~600 | ~120 | 6–9 days |
| 6. Integration and guidance | Cross-phase tests, user guide, sample manifests, release wiring | ~100 | ~200 | ~200 | 2–3 days |
| **Total** | | **~2,425** | **~2,175** | **~375** | **20–30 days** |

The midpoint is about **5,000 handwritten lines**. The merged proposal's
~2,900-line estimate is likely low because this plan includes the restricted
schema refactor, preflight Pod, ISO tool image and release wiring, restart and
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
  collision; one-shot terminal behavior; timeout and deletion cleanup.
- ACM tests: admin kubeconfig selected by ClusterDeployment reference;
  no destructive action when client setup fails; durable detachment marker
  before addon deletion; all addons and Klusterlet removed; no policy
  reconciliation after detachment.
- Seed tests: prerequisite failures, LCA failure and transient API errors,
  authoritative digest capture, missing digest failure, seed-only success.
- ISO tests: both registry credentials, IDMS/ITMS/ICSP and CA paths,
  installer preflight, Job restart/failure, log redaction, SSH host-key
  checking, remote and HTTPS digest verification, all credential copies
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
2. **Effective config stability:** a PR override or ClusterTemplate default
   can change between preflight and ISO upload. Choose a durable way to freeze
   the effective non-secret configuration at run start, or reject relevant
   edits once `StartedAt` is set. The proposal explicitly requires rejection
   after terminal state; active-run immutability needs to be settled too.
3. **ISO path binding:** define the remote document-root convention or add a
   trusted template field that makes `remotePath` to `urlBase` mapping
   verifiable. Keep the mandatory HTTPS hash path for different hosts.
4. **Tool image publication:** select the supported base image and where its
   immutable digest enters the bundle/release pipeline. Include disconnected
   mirroring of this image in the deployment plan.
5. **ACM teardown proof:** validate Klusterlet and addon deletion against the
   target ACM/MCE version before relying on it to produce a clean seed.
