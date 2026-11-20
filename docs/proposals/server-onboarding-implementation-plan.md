# Server Onboarding Service - Implementation Plan

This document sequences the work to build the Server Onboarding Service into
independently reviewable, testable phases. It is an execution plan, not a spec:
the behavior it delivers is defined in the
[Design Proposal](server-onboarding-service.md) and the
[Implementation](server-onboarding-implementation.md) document. Every deliverable
below traces back to a section of those two documents; file paths follow
[Project Structure](server-onboarding-implementation.md#8-project-structure-new-files).

The plan is organized as a **minimum viable v1** followed by **post-v1
enhancements**. v1 is a deployable, tested **basic onboarding flow** driven through
the CRDs (`oc`/Kubernetes API): it accepts each server's current BMC address and
provided credentials, inspects and validates the host, and publishes it to O2 IMS
inventory. Everything that depends on the **forthcoming Metal3 BMC-config APIs** —
BMC address pinning (networking), credential provisioning, and LDAP — is deferred
below the [v1 done](#v1-done) line, as are the REST facade, the firmware/BIOS
baseline, and production hardening.

This sequencing is a deliberate, reviewer-requested change: the Metal3 BMC-config
APIs (networking config / OCPSTRAT-3585, credentials provisioning / OCPSTRAT-3519,
LDAP / OCPSTRAT-3552) are **not yet finalized or available**, so v1 commits only to
what can be built and exercised today — the core flow on top of fast out-of-band
inspection ([METAL-1784](https://redhat.atlassian.net/browse/METAL-1784), Dev
Complete) — and the Metal3-dependent capabilities bolt on as later phases once each
API lands. Building the working core first lets us validate it on a live cluster
early, before the dependent features exist.

## Scope and approach

The service adds two CRDs (`OnboardingRequest`, `OnboardingServer` in
`onboarding.openshift.io`), two controllers, supporting BMH / (post-v1) Metal3
BMC-config orchestration packages, a validating webhook, and (post-v1) a REST
facade — plus deployment and RBAC wiring. There is no database. The controller
performs **no BMC I/O of its own**: v1 relies only on BMO's fast out-of-band
inspection, and every post-v1 BMC mutation is driven declaratively through the
forthcoming Metal3/BMO APIs, so the service never needs a direct BMC/Redfish
client.

Guiding rules for every phase:

- **Each phase is PR-sized and independently mergeable.** A phase leaves the tree
  green (`make ci-job`) and adds its own tests; nothing is merged behind a feature
  it cannot exercise.
- **Each phase is validatable on a live cluster.** The webhook and controller(s) a
  phase introduces are **registered (main.go wiring + manifests) within that same
  phase**, not deferred to a final deploy phase — so every phase can be deployed and
  exercised on a real cluster as it lands, rather than only through unit/envtest.
- **Regenerate on every API change.** After touching types, markers, or the
  OpenAPI spec, run `make generate && make manifests && make bundle` and commit
  the generated output — `bundle-check` in `make ci-job` fails on a dirty tree.
- **Tests land with the code.** New behavior ships with unit/envtest coverage in
  the same PR (see [Testing strategy](#testing-strategy)).
- **Metal3-dependent phases are gated on their API.** A post-v1 BMC-config phase can
  be built against the interface, but is not _done_ until its Metal3 API is
  available and validated against a real BMC through BMO.
- **Sign off and attribute.** All commits use `git commit -s`; AI-assisted commits
  carry a `Co-Authored-By` / `Assisted-By` trailer.

## Phase overview and effort

Effort is AI-assisted engineer-days: I draft the code, tests, manifests, and
OpenAPI specs and iterate; a human reviews, runs the `make` targets, and does the
on-cluster and real-BMC work. See [Effort estimate](#effort-estimate) for how these
roll up and what they exclude.

| Phase | Deliverable | Release | Effort (days) | Depends on |
|---|---|---|---|---|
| 0 | API types (v1 basic), scheme, CRD manifests, scaffolding | v1 | 2–4 | — |
| 1 | Validating webhook (core) + registration | v1 | 2–3 | 0 |
| 2 | BMH creation + inventory match (inspect/validate) | v1 | 4–6 | 0 |
| 3 | OnboardingRequest controller + registration | v1 | 3–5 | 0, 1 |
| 4 | OnboardingServer basic state machine + registration | v1 | 5–8 | 2, 3 |
| 5 | v1 deployment + end-to-end integration | v1 | 2–4 | 4 |
| **v1 subtotal** | | | **18–30** | |
| 6 | BMC address pinning (`PinningBmcAddress`, OCPSTRAT-3585) | post-v1 | 3–5 | 4 |
| 7 | BMC credential provisioning (`UpdatingCredentials`, OCPSTRAT-3519) | post-v1 | 3–5 | 4 |
| 8 | Firmware/BIOS baseline (`ApplyingHardwareProfile`) | post-v1 | 2–4 | 4 |
| 9 | REST API facade + RBAC roles (DD-004) | post-v1 | 3–5 | 0, 5 |
| 10 | LDAP authentication mode (OCPSTRAT-3552) | post-v1 | 4–7 | 7 |
| 11 | Observability, scale tuning, mTLS | post-v1 | 3–6 | 5 |
| **post-v1 subtotal** | | | **18–32** | |
| **Total** | | | **36–62** | |

## v1 — minimum viable release

**What v1 delivers.** A deployable onboarding service that takes an
`OnboardingRequest` (via `oc`/Kubernetes API) and drives each declared server
through a **basic onboarding flow**: it accepts the server's **current BMC address
and provided per-server credentials**, creates and inspects the BMH
(`inspectionMode: fast`), validates vendor/hardware, and publishes matched hosts
into O2 IMS inventory — with finalizer-based cleanup and idempotent re-onboarding.
The BMH references the provided credentials directly (no default→desired swap) and
the BMC keeps its current address, so v1 needs **none of the forthcoming Metal3
BMC-config APIs** — only fast out-of-band inspection (METAL-1784). The v1 state
machine is the subset `Pending` → `CreatingExternalSecrets` → `Inspecting` →
`Inventorying` → `Publishing` → `Completed`, plus terminal `VendorMismatch`,
`AuthFailed`, `HwMismatch`, and `Failed`.

**What v1 excludes** (all post-v1, each gated on its Metal3 API where applicable):
BMC address pinning (`PinningBmcAddress`), BMC credential provisioning
(`UpdatingCredentials`), LDAP auth mode, the firmware/BIOS baseline
(`ApplyingHardwareProfile`), the REST facade, and observability/scale/mTLS.

### Phase 0 — API types and scaffolding

**Goal.** The two CRDs exist, install, and (de)serialize everywhere they are
needed. No behavior yet.

**Deliverables.**

- `api/onboarding/v1alpha1/`: `onboardingrequest_types.go`,
  `onboardingserver_types.go`, `groupversion_info.go`, generated
  `zz_generated.deepcopy.go`. Encode the **v1 (basic-flow) subset** of
  [§1](server-onboarding-service.md#1-api-design-crds) via kubebuilder markers
  (required vs `omitempty`, enums, CEL where the design calls for it). The
  Metal3-dependent fields — `bmcSubnet`/`bmcGateway` (Phase 6),
  `disableBmcCertificateVerification` (Phase 7), `hardwareProfileName` (Phase 8),
  `authMode`/`ldapConfig` (Phase 10) — are added with their phases, so v1 does not
  commit to field shapes whose details depend on APIs that are not finalized.
- Scheme registration: add `onboarding/v1alpha1` to `GetSchemeForHub` in
  `internal/service/common/clients/k8s/k8s.go`; ensure the controller-manager
  scheme also pulls in `clcm.openshift.io/v1alpha1` and the Metal3 types.
- `internal/constants/constants.go`: onboarding constants, the
  `onboarding.openshift.io/inventory-hold` annotation key, and the server
  subcommand name.
- `config/crd/` generated manifests; `must-gather/gather` `gather_resource` calls
  for both CRDs.

**Tests.** deepcopy/type compilation; envtest that both CRDs apply and round-trip;
a required-vs-optional field validation smoke test (confirms optional fields are
accepted when omitted).

**Exit criteria.** `make generate manifests bundle` leaves a clean tree;
`make ci-job` green; CRDs installable in envtest.

### Phase 1 — Validating webhook (core)

**Goal.** Reject invalid or unsafe `OnboardingRequest` mutations at admission, and
register the webhook so it can be validated on a live cluster.

**Deliverables.**

- `internal/webhooks/onboardingrequest_webhook.go`: server-list validation,
  spec immutability, append-only server list, single-address-family enforcement,
  and the **deletion guard** that rejects deletion while a _published_ BMH exists.
  (The mid-BMC-mutation part of the deletion guard, and the request finalizer's
  quiesce/drain of in-flight mutations, arrive with the pinning/credential phases
  that introduce BMC-mutation states — v1 has none. See
  [Deletion semantics](server-onboarding-service.md#deletion-semantics).)
  Per-field validation for the Metal3-dependent fields lands in their phases.
- `config/webhook/`: `service.yaml` (service-ca annotated), `manifests.yaml`
  (`ValidatingWebhookConfiguration` on create/update/delete, `failurePolicy: Fail`,
  `sideEffects: None`, cabundle injection, API-server ingress allowed),
  `kustomization.yaml`; **register the webhook in `main.go`** so it runs when
  deployed.

**Tests.** envtest webhook cases: immutability rejection, append-only enforcement,
single-family enforcement, deletion allowed with no published BMH / rejected with
one. **On-cluster:** deploy and confirm the webhook admits/rejects against a live
API server.

**Exit criteria.** Webhook validated in envtest and exercised on a live cluster;
manifests wired into kustomize.

### Phase 2 — BMH creation and inventory match

**Goal.** The building blocks the state machine uses to create a held BMH, read
inspection results, and match hardware — usable and unit-tested in isolation.

**Deliverables.**

- `internal/onboarding/bmh/creator.go`: BMH CR + per-host Secret creation using the
  **provided per-server credentials**, fast out-of-band (Redfish) inspection
  (`spec.inspectionMode: fast`), and the inventory-hold marker; marker removal at
  publish. Follow
  [BMH and per-host Secret naming](server-onboarding-implementation.md#bmh-and-per-host-secret-naming)
  — names are a **deterministic UUID** (UUIDv5 from a stable pool/request identity +
  BMC address; the BMH is `onboarded-<uuid>`), with request and BMC address exposed
  as printer columns / field selectors.
- `internal/onboarding/bmh/inventory.go`: map inspection `HardwareData` to
  canonical attributes; vendor (case-insensitive substring) and hardware-attribute
  matching with `operator` semantics. Attribute matching is skipped cleanly when
  `spec.hardwareAttributes` is empty (vendor check still applies).
- Modify `internal/hardwaremanager/controller/inventory.go` so
  `IncludeInInventory()` returns `false` for a BMH carrying the inventory-hold
  marker, and exclude held BMHs from the allocation selection path (see
  [Inventory-hold marker](server-onboarding-implementation.md#inventory-hold-marker-and-includeininventory)).

**Tests.** unit: canonical-attribute mapping, integer/string comparisons, operator
rules, empty-`hardwareAttributes` path, deterministic-UUID name derivation
(UUIDv5 from pool/request + `bmcAddress`), and `IncludeInInventory()` / allocation
exclusion for a held BMH.

**Exit criteria.** Packages compile and are unit-covered; the hardware-manager
inventory/allocation change has a regression test.

### Phase 3 — OnboardingRequest controller

**Goal.** Reconcile an `OnboardingRequest` into the inventory hierarchy, the
per-server `OnboardingServer` CRs, ExternalSecrets, and status — and register the
controller so the phase is live-cluster testable.

**Deliverables.**

- `internal/controllers/onboardingrequest_controller.go`: ensure the inventory
  hierarchy — require an **existing** `Location` (report `LocationNotFound` when
  absent; never create one, since the request carries none of the address/coordinate
  data a `Location` needs) and create `OCloudSite`/`ResourcePool` when absent (with
  default descriptions, validating an existing site's parent Location), per
  [Resource provisioning on reconciliation](server-onboarding-service.md#1-api-design-crds);
  create one `OnboardingServer` per `spec.servers` entry, create per-server
  ExternalSecrets against the configured `ClusterSecretStore`, and maintain the
  request status (validation + aggregated onboarding status and counts) per
  [§1](server-onboarding-service.md#1-api-design-crds).
- `DeletionTimestamp` handling immediately after fetch (per AGENTS.md reconciler
  rule); `//+kubebuilder:rbac` markers for the resources it touches; **register the
  controller in `main.go`** and add its RBAC to the deployment.

**Tests.** envtest: hierarchy creation, OnboardingServer CR fan-out from the
server list, status correctness (including the empty/unset-state Pending window),
and a deleting-request regression test that skips normal reconciliation.
**On-cluster:** deploy and confirm the hierarchy + OnboardingServer fan-out on a
live cluster.

**Exit criteria.** Controller reconciles in envtest and on a live cluster; RBAC
markers regenerate into `config/rbac/role.yaml`.

### Phase 4 — OnboardingServer basic state machine

**Goal.** Drive each server through the **basic flow** to `Completed`, with correct
terminal failures, cleanup, and idempotent re-onboarding — and register the
controller for live-cluster validation.

**Deliverables.**

- `internal/controllers/onboardingserver_controller.go`: wire the v1 subset of the
  state machine from [§2](server-onboarding-service.md#2-server-onboarding-state-machine)
  — `Pending` → `CreatingExternalSecrets` → `Inspecting` → `Inventorying` →
  `Publishing` → `Completed`; terminal `VendorMismatch`, `AuthFailed`,
  `HwMismatch`, `Failed`. **No `PinningBmcAddress` or `UpdatingCredentials`** — the
  BMH uses the provided per-server credentials as-is and the BMC keeps its current
  address. `ApplyingHardwareProfile`, `PinningBmcAddress`, `UpdatingCredentials`,
  `ConfiguringLdap`, and `DisablingLocalAccounts` are added in later phases; v1
  admission rejects `hardwareProfileName` and `authMode: ldap` until then.
- Finalizer cleanup: delete a held BMH and per-server ExternalSecret/Secret on
  delete; **retain** a published BMH and its credentials; the request finalizer
  sweeps remaining credentials on teardown; `status.cleanupStatus`; retry-by-delete
  recreates the server as `Pending`. (Managed explicitly, not via owner-reference
  GC — see
  [Cleanup mechanism](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior).)
- **Register the controller in `main.go`** with its RBAC.

**Tests.** envtest per [§11.2](server-onboarding-implementation.md#11-verification):
mismatch deletes the held BMH and goes terminal; marker removal at `Publishing`;
finalizer cleanup retaining published hosts; deleting-server regression test.
**On-cluster:** drive a server to `Completed` on a live cluster.

**Exit criteria.** Basic-path state progression passes in envtest against a
simulated `HardwareData` and on a live cluster; every v1 state in §2 has coverage.

### Phase 5 — v1 deployment and end-to-end integration

**Goal.** Prove the whole basic-auth flow end-to-end on a cluster. (The webhook and
controllers are already registered in Phases 1/3/4; this phase is end-to-end
validation and packaging, not first-time wiring.)

**Deliverables.**

- Deployment manifests finalized: onboarding Deployment with the controller-manager
  container (no REST-server container yet); `config/rbac/` aggregation and
  `make bundle`.
- Integration + manual verification per [§11.3–4](server-onboarding-implementation.md#11-verification):
  create an `OnboardingRequest`, watch servers progress via
  `oc get onboardingservers`, confirm a held BMH is not surfaced to O2 IMS
  inventory until `Publishing`.

**Tests.** integration deploy test; `make test-e2e` for the controller paths this
touches.

**Exit criteria.** Basic-auth flow green on a cluster; `make ci-job` and
`make test-e2e` pass.

### v1 done

At this line the service is **usable in production for basic onboarding via the CRD
interface**: servers can be declared with their current address and credentials,
validated, and published to inventory, with cleanup and re-onboarding — on top of
fast out-of-band inspection alone, with **no dependency on the unfinalized Metal3
BMC-config APIs**. The remaining phases are additive and do not modify the v1
contract.

## Post-v1 enhancements

The pinning, credential, and LDAP phases below each **depend on a forthcoming
Metal3 BMC-config API**; they can be built against the interface but are not done
until that API is available and validated against a real BMC through BMO.

### Phase 6 — BMC address pinning

**Goal.** Pin each BMC from its DHCP-assigned address to a static one (same
address), via the Metal3 **BMC networking config API**
([OCPSTRAT-3585](https://redhat.atlassian.net/browse/OCPSTRAT-3585)).

**Deliverables.**

- API: add `bmcSubnet`/`bmcGateway` fields and their webhook validation (CIDR /
  gateway / same-family / `bmcAddress`-within-subnet; updatable for recovery).
- `internal/onboarding/bmh/bmcconfig.go` (illustrative name): declare the
  DHCP→static pin + DNS on the BMH / companion Metal3 CRs and interpret the
  resulting BareMetalHost conditions, per
  [§5](server-onboarding-implementation.md#5-metal3-bmc-configuration). No direct
  BMC/Redfish client; BMO performs the Redfish work.
- `onboardingserver_controller.go`: insert the `PinningBmcAddress` state before
  `Publishing`; add the mid-BMC-mutation deletion guard and the finalizer's
  drain/async-rollback handling for this state; record progress in
  `status.mutationProgress`.

**Tests.** unit: `bmcconfig.go` pin against a fake client (desired-state set +
BMH-condition mapping). envtest: `PinningBmcAddress` transition and rollback.
Integration: pin a real BMC via BMO. **Gated on OCPSTRAT-3585.**

**Exit criteria.** Pinning works end-to-end against a real BMC; states covered.

### Phase 7 — BMC credential provisioning

**Goal.** Replace factory-default credentials with per-server desired credentials,
via the Metal3 **BMC credentials provisioning API**
([OCPSTRAT-3519](https://redhat.atlassian.net/browse/OCPSTRAT-3519)).

**Deliverables.**

- API: add `disableBmcCertificateVerification` (default `false`), and the
  shared-default-credentials model the credential swap uses.
- `bmcconfig.go`: declare the credential change (default → per-server desired),
  propagated by BMO to the BMC and the BMH Secret, and interpret the conditions.
- `onboardingserver_controller.go`: insert the `UpdatingCredentials` state (after
  pinning, before `Publishing`); extend the mid-mutation guard and
  `mutationProgress`/rollback to cover it; the BMH-creation webhook guard pins
  `bmc.address` host to the declared `bmcAddress` and restricts `credentialsName`.

**Tests.** unit: credential-provisioning helper against a fake client. envtest:
`UpdatingCredentials` transition and the point-of-no-return semantics; a
compromised-service-account test. Integration: provision a real BMC via BMO.
**Gated on OCPSTRAT-3519.**

**Exit criteria.** Credential provisioning works end-to-end against a real BMC;
states covered.

### Phase 8 — Firmware/BIOS baseline

**Goal.** Bring the pool to a desired firmware/BIOS baseline during onboarding when
`spec.hardwareProfileName` is set.

**Deliverables.**

- API: add `hardwareProfileName` and its best-effort existence validation.
- `onboardingserver_controller.go`: the `ApplyingHardwareProfile` state (after
  `Inventorying`, before `PinningBmcAddress`; skipped when `hardwareProfileName` is
  empty), reusing the hardware manager's `processHwProfile` translation into
  `HostFirmwareSettings`/`HostFirmwareComponents`/`HostUpdatePolicy`.
- `//+kubebuilder:rbac` for `hardwareprofiles` (get/list) and the Metal3 servicing
  resources.

**Tests.** envtest: skip vs run of `ApplyingHardwareProfile`; missing-profile →
`Failed`; servicing CRs created and matched to the held BMH.

**Exit criteria.** Firmware baseline applies end-to-end; state covered in envtest.

### Phase 9 — REST API facade

**Goal.** Expose the O-RAN-style REST API as a thin facade over the CRDs, with the
RBAC required by DD-004.

**Deliverables.**

- `internal/service/onboarding/`: `api/openapi.yaml`
  (`/ocloud-infrastructureOnboarding/v1`), generated code via oapi-codegen,
  `config.go`, `server.go` (`StrictServerInterface` over `HubClient`),
  `converters.go`; `cmd/root.go`, `cmd/serve.go`, `serve.go`; add the subcommand
  to the multi-service root command. No `db/` subtree. Add the REST-server
  container to the onboarding Deployment (OAuth issuer config + serving TLS cert).
- RBAC: new `config/rbac/clusterrole_onboarding_server.yaml`
  (`get,list,create,patch,delete` on `onboardingrequests` only — no
  `onboardingservers`); `onboarding-server-subject-access-reviewer-binding` in
  `clusterrolebindings_services.yaml`; DD-004 updates to
  `oran_o2ims_user_roles.yaml` and `oran_o2ims_oauth_role_bindings.yaml`; and the
  `security` annotations in `openapi.yaml`.

**Tests.** `server_test.go` for handler behavior, converter round-trips, and the
k8s-error-to-HTTP-status mapping (NotFound→404, AlreadyExists→409,
Forbidden/BadRequest/Invalid→400, else 500).

**Exit criteria.** `make go-generate` clean; server serves the spec; RBAC files
regenerate through `make manifests && make bundle`.

### Phase 10 — LDAP authentication mode

**Goal.** Add the alternative `authMode=ldap` path via the Metal3 **LDAP integration
API** ([OCPSTRAT-3552](https://redhat.atlassian.net/browse/OCPSTRAT-3552)).

**Deliverables.**

- API: add `authMode`/`ldapConfig` and their validation (required fields when
  `authMode=ldap`).
- `internal/onboarding/bmh/ldap.go` (illustrative name): declare LDAP configuration
  and the irreversible local-account disable via the Metal3 LDAP integration API,
  and interpret the resulting BareMetalHost conditions, per
  [§5](server-onboarding-implementation.md#5-metal3-bmc-configuration).
- `onboardingserver_controller.go`: the `ConfiguringLdap` state (in place of
  `UpdatingCredentials` when `authMode=ldap`) and the `DisablingLocalAccounts`
  state that runs after `Publishing` when `disableLocalAccounts` is set, per
  [§2](server-onboarding-service.md#2-server-onboarding-state-machine).

**Tests.** envtest: `ConfiguringLdap` transition on `authMode=ldap`;
`DisablingLocalAccounts` after `Publishing` (best-effort — a failure does not fail
the server); `LdapConfig` webhook validation. Unit: `bmh/ldap.go` with a fake
client (asserts the desired LDAP config is declared and BMH conditions are mapped).
Integration: the
[LDAP mode variant](server-onboarding-implementation.md#ldap-mode-variant)
walkthrough end-to-end against real BMCs (driven via BMO). **Gated on
OCPSTRAT-3552.**

**Exit criteria.** LDAP mode onboards end-to-end; the remaining §2 states have
coverage; `make ci-job` and `make test-e2e` pass.

### Phase 11 — Observability, scale tuning, and mTLS

**Goal.** Production-readiness items that depend on external inputs.

**Deliverables.**

- Metrics/logging per [Observability](server-onboarding-implementation.md#observability),
  following the [Logging Conventions](server-onboarding-implementation.md).
- `MaxConcurrentReconciles` default validated once the
  [§12](server-onboarding-implementation.md#12-open-questions--product-inputs)
  batch-scale and per-Hub concurrency targets are supplied.
- mTLS (TELCOSTRAT-290): RFC 8705 certificate-bound tokens and Ingress
  client-CA bundle, matching the other services.

**Exit criteria.** Scale target met once numbers land; mTLS parity with peer
services.

## Dependency graph

```text
                      Phase 0: API types (v1 basic)
                    /        |            \
              Phase 1     Phase 2        Phase 3
              Webhook     BMH/Inv        OnbReq controller
                    \        |            /
                     \       └──► Phase 4 (basic state machine)
                      \              │
                       └──► Phase 5: v1 deploy + integration
        ══════════════════════ v1 released ══════════════════════
                                     │  (each gated on its Metal3 API)
          ┌──────────┬──────────┬────┴─────┬──────────┬──────────┐
          ▼          ▼          ▼          ▼          ▼          ▼
      Phase 6    Phase 7    Phase 8    Phase 9    Phase 10   Phase 11
      Pinning    Creds      Firmware   REST       LDAP       Observability/
      (3585)     (3519)     baseline   facade     (3552)     scale / mTLS
```

Within v1, Phases 1, 2, and 3 proceed in parallel once Phase 0 lands; Phase 4 (the
basic state machine) is the critical path — it depends on 2 and the CRs created in
3. The post-v1 phases are largely independent and can be scheduled in any order
after v1, subject to each Metal3 API's availability (Phase 10 builds on the
credential-provisioning foundation from Phase 7).

## Testing strategy

Coverage mirrors [§11 Verification](server-onboarding-implementation.md#11-verification):

- **Unit** (Phases 2, 6, 7, 9, 10): attribute matching, deterministic-UUID name
  derivation, `IncludeInInventory()` / allocation exclusion, the Metal3 BMC-config
  helpers against a fake client (desired-state set + BMH-condition mapping), REST
  converters and error mapping.
- **envtest** (Phases 1, 3, 4, 6, 7, 8, 10): webhook validation, controller
  reconciliation, state transitions (basic path in Phase 4; pinning/creds in 6/7;
  firmware in 8; LDAP in 10), held-BMH deletion on mismatch, marker removal,
  finalizer cleanup, and a deleting-resource regression test per reconciler.
- **On-cluster / integration** (every phase): because each phase registers its
  webhook/controller, it can be deployed and exercised on a live cluster as it
  lands — the basic flow end-to-end (Phase 5); each BMC-config operation against a
  real BMC via BMO (Phases 6, 7, 10), including a foreground-deletion (GC-enabled)
  test of credential cleanup ordering.

Gomega conventions from AGENTS.md apply throughout (`.ToNot()`,
`Expect(err).ToNot(HaveOccurred())`).

## RBAC and manifests checklist

Per the RBAC guidance in AGENTS.md, verify the generated
`config/rbac/role.yaml` after every marker change (`make manifests`), and edit
service ClusterRoles by hand — they are not generated from markers:

- Controller-manager permissions come from `//+kubebuilder:rbac` markers (Phases 3,
  4, 6, 7, 8) and must include `hardwareprofiles` (get/list, Phase 8), the Metal3
  servicing resources, and the Metal3 BMC-config resources for networking/
  credentials/LDAP (Phases 6, 7, 10 — exact kinds per the final BMC-config APIs).
- REST-server ClusterRole is hand-maintained and minimal (Phase 9).
- DD-004: user roles, OAuth bindings, and `openapi.yaml` `security` annotations
  are updated together whenever a public endpoint changes (Phase 9).

## Effort estimate

The per-phase days above are AI-assisted engineer-days and roll up to:

- **v1: ~18–30 days** — about **4–6 weeks** for one engineer, or **2–3 weeks** with
  two engineers exploiting the parallelism (Phases 1/2/3 fan out; one owns the
  Phase 4 long pole). A **demoable single-server happy path** lands earlier still,
  and v1 has **no dependency on the unfinalized Metal3 BMC-config APIs**.
- **Post-v1: ~18–32 days**, schedulable incrementally and independently — but the
  BMC-config phases (6, 7, 10) are **gated on their Metal3 APIs landing**.
- **Full proposal: ~36–62 days.**

These numbers assume prompt code review and an available test cluster with BMCs.
They **do not** compress the parts AI does not touch, which dominate the calendar:

- **Real-hardware BMC testing via the Metal3 APIs** (Phases 6, 7, 10) — validating
  the DHCP→static pin, credential, and LDAP operations end-to-end against physical
  BMCs (driven through BMO, not the onboarding controller). Vendor DHCP-lease and
  LDAP behavior is inconsistent; fakes pass, physical BMCs surprise. Bench time,
  not coding time — and gated on the Metal3 APIs landing (see below).
- **Cluster + e2e environment** (Phase 5) — needs a hub with Metal3/BMO, ESO, a
  vault, and BMC routing. Environment readiness, not code, usually gates this.
- **External prerequisites** (§9) — ESO/ClusterSecretStore, the vault, and
  (Phase 10) an LDAP server are often owned by other teams.
- **Open product inputs** (§12) — the scale numbers gate Phase 11 validation only,
  not the mechanism.

## Risks and open questions

- **Product inputs (non-blocking).** The batch-scale and per-Hub concurrency
  targets in [§12](server-onboarding-implementation.md#12-open-questions--product-inputs)
  gate only the Phase 11 scale validation, not the mechanism.
- **Dependency on forthcoming Metal3 APIs (sequencing).** v1 deliberately avoids
  this risk: the basic flow (Phases 0–5) depends only on fast out-of-band inspection
  (METAL-1784, Dev Complete), not on the unfinalized BMC-config APIs. The
  BMC-config phases depend on forthcoming Metal3/BMO APIs — BMC networking config
  (OCPSTRAT-3585, Phase 6), credentials provisioning (OCPSTRAT-3519, Phase 7), and
  LDAP integration (OCPSTRAT-3552, Phase 10). Each can be built against the
  interface but is blocked until its API lands; the exact shape is not frozen, and
  the `bmcconfig.go` helpers are the single reconciliation point. See
  [Dependency on forthcoming Metal3 APIs](server-onboarding-service.md#dependency-on-forthcoming-metal3-apis).
- **External prerequisites.** ESO, `ClusterSecretStore`, the external vault, and
  hub→BMC routing **from the Metal3/BMO pods** (not the onboarding pod) must be in
  place for v1 integration (Phase 5); routing matters only once the BMC-config
  phases run. A `HardwareProfile` CR is needed for Phase 8 and an LDAP server for
  Phase 10. See [§9 Prerequisites](server-onboarding-implementation.md#9-prerequisites).
- **Vendor Redfish variance.** BMC DHCP-lease, DNS, and LDAP support is inconsistent
  across vendors (the BMC-config APIs note OEM-extension gaps); Phases 6/7/10 must
  honor `bmcSubnet`/`bmcGateway` when supplied rather than trusting the lease, and
  treat DNS/transition support as best-effort per vendor as BMO reports it.

## Definition of done

**v1 done** (see the [v1 done](#v1-done) line):

- Phases 0–5 merged; `make ci-job` and `make test-e2e` pass; generated tree clean.
- Every v1 (basic-flow) state in
  [§2](server-onboarding-service.md#2-server-onboarding-state-machine) has
  unit/envtest coverage; the held-until-`Publishing` invariant is verified; the
  flow is validated on a live cluster.
- Controller RBAC, must-gather, bundle, and the v1 Deployment are complete and
  regenerated.

**Full proposal done:**

- Phases 6–11 merged: BMC pinning, credential provisioning, firmware baseline, REST
  facade (with DD-004 RBAC), LDAP mode, and observability/mTLS complete — each
  validated against real BMCs through its Metal3 API; scale target met once §12
  numbers land.
- The design and implementation documents remain consistent with the shipped code.
