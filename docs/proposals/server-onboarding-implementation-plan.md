# Server Onboarding Service - Implementation Plan

This document sequences the work to build the Server Onboarding Service into
independently reviewable, testable phases. It is an execution plan, not a spec:
the behavior it delivers is defined in the
[Design Proposal](server-onboarding-service.md) and the
[Implementation](server-onboarding-implementation.md) document. Every deliverable
below traces back to a section of those two documents; file paths follow
[Project Structure](server-onboarding-implementation.md#8-project-structure-new-files).

The plan is organized as a **minimum viable v1** followed by **post-v1
enhancements**. v1 is a deployable, tested, local-auth onboarding flow driven
through the CRDs (`oc`/Kubernetes API). Everything that v1 does not need — the REST
facade, LDAP auth mode, the firmware/BIOS baseline, and production hardening — is
deferred below the [v1 done](#v1-done) line and bolts on without rework, because
the phases isolate it.

## Scope and approach

The service adds two CRDs (`OnboardingRequest`, `OnboardingServer` in
`onboarding.openshift.io`), two controllers, supporting BMH / Metal3 BMC-config
orchestration packages, a validating webhook, and (post-v1) a REST facade — plus
deployment and RBAC wiring. There is no database. The controller performs **no BMC
I/O of its own** — every BMC operation is driven declaratively through forthcoming
Metal3/BMO APIs (BMC networking config, BMC credentials provisioning, LDAP
integration, and fast out-of-band inspection), so the service needs no direct
BMC/Redfish client.

Guiding rules for every phase:

- **Each phase is PR-sized and independently mergeable.** A phase leaves the tree
  green (`make ci-job`) and adds its own tests; nothing is merged behind a feature
  it cannot exercise.
- **Regenerate on every API change.** After touching types, markers, or the
  OpenAPI spec, run `make generate && make manifests && make bundle` and commit
  the generated output — `bundle-check` in `make ci-job` fails on a dirty tree.
- **Tests land with the code.** New behavior ships with unit/envtest coverage in
  the same PR (see [Testing strategy](#testing-strategy)).
- **Sign off and attribute.** All commits use `git commit -s`; AI-assisted commits
  carry a `Co-Authored-By` / `Assisted-By` trailer.

## Phase overview and effort

Effort is AI-assisted engineer-days: I draft the code, tests, manifests, and
OpenAPI specs and iterate; a human reviews, runs the `make` targets, and does the
on-cluster and real-BMC work. See [Effort estimate](#effort-estimate) for how these
roll up and what they exclude.

| Phase | Deliverable | Release | Effort (days) | Depends on |
|---|---|---|---|---|
| 0 | API types, scheme, CRD manifests, scaffolding | v1 | 2–4 | — |
| 1 | Validating webhook (core) | v1 | 2–3 | 0 |
| 2 | BMH creation + inventory match | v1 | 4–6 | 0 |
| 3 | Metal3 BMC-config orchestration (local credentials) | v1 | 3–5 | 0 |
| 4 | OnboardingRequest controller | v1 | 3–5 | 0, 1 |
| 5 | OnboardingServer state machine (local, no firmware) | v1 | 6–9 | 2, 3, 4 |
| 6 | Deployment + integration (v1) | v1 | 3–5 | 5 |
| **v1 subtotal** | | | **24–38** | |
| 7 | Firmware/BIOS baseline (`ApplyingHardwareProfile`) | post-v1 | 2–4 | 5 |
| 8 | REST API facade + RBAC roles (DD-004) | post-v1 | 3–5 | 0, 6 |
| 9 | LDAP authentication mode | post-v1 | 4–7 | 5, 6 |
| 10 | Observability, scale tuning, mTLS | post-v1 | 3–6 | 6 |
| **post-v1 subtotal** | | | **12–22** | |
| **Total** | | | **36–60** | |

## v1 — minimum viable release

**What v1 delivers.** A deployable onboarding service that takes an
`OnboardingRequest` (via `oc`/Kubernetes API), drives each declared server through
inspection, vendor/hardware matching, BMC address pinning, and the local
credential swap, and publishes matched hosts into O2 IMS inventory — with
finalizer-based cleanup and idempotent re-onboarding.

**What v1 excludes** (all post-v1): the REST facade, LDAP auth mode, the
firmware/BIOS baseline (`ApplyingHardwareProfile`), observability/scale tuning, and
mTLS.

### Phase 0 — API types and scaffolding

**Goal.** The two CRDs exist, install, and (de)serialize everywhere they are
needed. No behavior yet.

**Deliverables.**

- `api/onboarding/v1alpha1/`: `onboardingrequest_types.go`,
  `onboardingserver_types.go`, `groupversion_info.go`, generated
  `zz_generated.deepcopy.go`. Encode the field-level optionality from
  [§1](server-onboarding-service.md#1-api-design-crds) via kubebuilder markers
  (required vs `omitempty`, enums, CEL where the design calls for it).
- Scheme registration: add `onboarding/v1alpha1` to `GetSchemeForHub` in
  `internal/service/common/clients/k8s/k8s.go`; ensure the controller-manager
  scheme also pulls in `clcm.openshift.io/v1alpha1` and the Metal3 types.
- `internal/constants/constants.go`: onboarding constants, the
  `onboarding.openshift.io/inventory-hold` annotation key, and the server
  subcommand name.
- `config/crd/` generated manifests; `must-gather/gather` `gather_resource` calls
  for both CRDs.

**Tests.** deepcopy/type compilation; envtest that both CRDs apply and round-trip;
a required-vs-optional field validation smoke test (confirms
`hardwareProfileName` and `hardwareAttributes` are accepted when omitted).

**Exit criteria.** `make generate manifests bundle` leaves a clean tree;
`make ci-job` green; CRDs installable in envtest.

### Phase 1 — Validating webhook (core)

**Goal.** Reject invalid or unsafe `OnboardingRequest` mutations at admission.

**Deliverables.**

- `internal/webhooks/onboardingrequest_webhook.go`: server-list validation,
  spec immutability, append-only server list, the **deletion guard** that
  rejects deletion while a *published* BMH exists **or any server is
  mid-BMC-mutation** (`PinningBmcAddress` / `UpdatingCredentials` /
  `ConfiguringLdap`) — these webhook checks are advisory, and the request
  **finalizer** is the authority that quiesces the child controllers and drains
  in-flight mutations before removing shared state (see
  [Deletion semantics](server-onboarding-service.md#deletion-semantics)) — and a
  **v1 scope guard** that rejects requests setting `hardwareProfileName` or
  `authMode: ldap` until the `ApplyingHardwareProfile` / `ConfiguringLdap` phases
  land — otherwise such a request would reach `Completed` without applying the
  requested baseline, and the published-BMH guard would then block retrying it.
  `LdapConfig` validation is added later with the LDAP mode.
- `config/webhook/`: `service.yaml` (service-ca annotated), `manifests.yaml`
  (`ValidatingWebhookConfiguration` on create/update/delete, `failurePolicy: Fail`,
  `sideEffects: None`, cabundle injection), `kustomization.yaml`.

**Tests.** envtest webhook cases: immutability rejection, append-only enforcement,
deletion allowed with no published BMH and no in-flight mutation / rejected when a
published BMH exists / rejected while a server is mid-BMC-mutation, the v1 scope
guard rejecting `hardwareProfileName` and `authMode: ldap`, and the
`hardwareProfileName`-references-existing-profile best-effort check. (The
finalizer's quiesce-and-drain behavior is covered by the Phase 4/6 controller
tests, since it depends on the OnboardingServer state machine.)

**Exit criteria.** Webhook validated in envtest; manifests wired into kustomize.

### Phase 2 — BMH creation and inventory match

**Goal.** The building blocks the state machine uses to create a held BMH, read
inspection results, and match hardware — usable and unit-tested in isolation.

**Deliverables.**

- `internal/onboarding/bmh/creator.go`: BMH CR + per-host Secret creation with
  shared default credentials, fast out-of-band (Redfish) inspection
  (`spec.inspectionMode: fast`),
  and the inventory-hold marker; marker removal at publish. (BMC credential
  provisioning is driven through the Metal3 credentials API in Phases 3/5, not by
  swapping `spec.bmc.credentialsName` directly.)
  Follow [BMH and per-host Secret naming](server-onboarding-implementation.md#bmh-and-per-host-secret-naming).
- `internal/onboarding/bmh/inventory.go`: map inspection `HardwareData` to
  canonical attributes; vendor (case-insensitive substring) and hardware-attribute
  matching with `operator` semantics. Attribute matching is skipped cleanly when
  `spec.hardwareAttributes` is empty (vendor check still applies).
- Modify `internal/hardwaremanager/controller/inventory.go` so
  `IncludeInInventory()` returns `false` for a BMH carrying the inventory-hold
  marker (see [Inventory-hold marker](server-onboarding-implementation.md#inventory-hold-marker-and-includeininventory)).

**Tests.** unit: canonical-attribute mapping, integer/string comparisons, operator
rules, empty-`hardwareAttributes` path, BMH/server name derivation from
`bmcAddress`, and `IncludeInInventory()` false for a held BMH.

**Exit criteria.** Packages compile and are unit-covered; the hardware-manager
inventory change has a regression test.

### Phase 3 — Metal3 BMC-config orchestration (local credentials)

**Goal.** The helpers that declare the BMC-side mutations the local flow drives and
interpret the outcome from BareMetalHost status — independent of controllers. The
onboarding service issues **no Redfish/IPMI itself**; BMO performs the Redfish work
and reports back on the BMH.

**Deliverables.**

- No direct BMC/Redfish client dependency; no `internal/onboarding/redfish/` package.
- `internal/onboarding/bmh/bmcconfig.go` (illustrative name): helpers to request
  the DHCP→static BMC-IP pin + DNS (BMC networking) and local
  credential provisioning (BMC credentials, `authMode=local`)
  by setting desired state on the BareMetalHost (or its companion Metal3 CRs /
  Secrets), plus helpers to read the corresponding BareMetalHost status conditions
  (in-progress / success / failure). Operations per
  [§5](server-onboarding-implementation.md#5-metal3-bmc-configuration). LDAP
  integration is deferred
  to the LDAP mode phase. Inventory comes from BMO's fast out-of-band inspection
  (`HardwareData`), not from any onboarding-side BMC client. **The exact Metal3 API
  surface is not yet available; these helpers wrap whatever shape it
  ships as and are the single place to reconcile with it.**

**Tests.** unit tests with a fake Kubernetes client: assert the helpers set the
expected desired BMC-config state on the BMH/companion CRs, and that they map
simulated BareMetalHost conditions to in-progress/success/auth-failure/failure
outcomes.

**Exit criteria.** Helpers unit-covered; no controller dependency and no BMC
network dependency introduced.

### Phase 4 — OnboardingRequest controller

**Goal.** Reconcile an `OnboardingRequest` into the inventory hierarchy, the
per-server `OnboardingServer` CRs, ExternalSecrets, and status counts.

**Deliverables.**

- `internal/controllers/onboardingrequest_controller.go`: ensure the inventory
  hierarchy — require an **existing** `Location` (report `LocationNotFound` when
  absent; never create one, since the request carries none of the address/coordinate
  data a `Location` needs) and create `OCloudSite`/`ResourcePool` when absent (with
  default descriptions), per
  [Resource provisioning on reconciliation](server-onboarding-service.md#1-api-design-crds);
  create one `OnboardingServer` per `spec.servers` entry, create
  per-server ExternalSecrets against the configured `ClusterSecretStore`, and
  maintain the status counts (`serversTotal/Pending/InProgress/Completed/Failed`)
  per the predicate in [§1](server-onboarding-service.md#1-api-design-crds).
- `DeletionTimestamp` handling immediately after fetch (per AGENTS.md reconciler
  rule); `//+kubebuilder:rbac` markers for the resources it touches.

**Tests.** envtest: hierarchy creation, OnboardingServer CR fan-out from the
server list, status-count correctness (including the empty/unset-state Pending
window), and a deleting-request regression test that skips normal reconciliation.

**Exit criteria.** Controller reconciles in envtest; RBAC markers regenerate into
`config/rbac/role.yaml`.

### Phase 5 — OnboardingServer state machine (local auth, no firmware)

**Goal.** Drive each server through the state machine to `Completed` on the
default local auth path, with correct terminal failures, cleanup, and idempotent
re-onboarding.

**Deliverables.**

- `internal/controllers/onboardingserver_controller.go`: wire the states from
  [§2](server-onboarding-service.md#2-server-onboarding-state-machine) using the
  Phase 2/3 packages on the default local auth path — `Pending` →
  `CreatingExternalSecrets` → `Inspecting` → `Inventorying` → `PinningBmcAddress`
  → `UpdatingCredentials` → `Publishing` → `Completed`; terminal `VendorMismatch`,
  `AuthFailed`, `HwMismatch`, `Failed`. The `ApplyingHardwareProfile`,
  `ConfiguringLdap`, and `DisablingLocalAccounts` states are added post-v1, so v1
  admission rejects `hardwareProfileName` and `authMode: ldap` until then (the
  Phase 1 scope guard above).
- Finalizer cleanup: delete a held BMH and per-server ExternalSecret/Secret on
  delete; **retain** a published BMH and its credentials; set
  `status.cleanupStatus`; retry-by-delete recreates the server as `Pending`.

**Tests.** envtest per [§11.2](server-onboarding-implementation.md#11-verification):
mismatch deletes the held BMH and goes terminal; credential swap; marker removal
at `Publishing`; finalizer cleanup retaining published hosts; deleting-server
regression test.

**Exit criteria.** Local-path state progression passes in envtest against a
simulated `HardwareData`; every local-path state in §2 has coverage.

### Phase 6 — Deployment and integration (v1)

**Goal.** Ship the controllers and prove the local-auth flow end-to-end on a
cluster.

**Deliverables.**

- Deployment manifests: onboarding Deployment with the controller-manager
  container (no REST-server container yet).
- `main.go` controller/webhook registration; `config/rbac/` aggregation and
  `make bundle`.
- Integration + manual verification per [§11.3–4](server-onboarding-implementation.md#11-verification):
  create an `OnboardingRequest`, watch servers progress via
  `oc get onboardingservers`, confirm a held BMH is not surfaced to O2 IMS
  inventory until `Publishing`.

**Tests.** integration deploy test; `make test-e2e` for the controller paths this
touches.

**Exit criteria.** Local-auth flow green on a cluster; `make ci-job` and
`make test-e2e` pass.

### v1 done

At this line the service is **usable in production for local-auth onboarding via
the CRD interface**: servers can be declared, validated, credential-swapped, and
published to inventory, with cleanup and re-onboarding. The remaining phases are
additive and do not modify the v1 contract.

## Post-v1 enhancements

### Phase 7 — Firmware/BIOS baseline

**Goal.** Bring the pool to a desired firmware/BIOS baseline during onboarding when
`spec.hardwareProfileName` is set.

**Deliverables.**

- `onboardingserver_controller.go`: the `ApplyingHardwareProfile` state (after
  `Inventorying`, before `PinningBmcAddress`; skipped when `hardwareProfileName` is
  empty), reusing the hardware manager's `processHwProfile` translation into
  `HostFirmwareSettings`/`HostFirmwareComponents`/`HostUpdatePolicy`.
- `//+kubebuilder:rbac` for `hardwareprofiles` (get/list) and the Metal3 servicing
  resources.

**Tests.** envtest: skip vs run of `ApplyingHardwareProfile`; missing-profile →
`Failed`; servicing CRs created and matched to the held BMH.

**Exit criteria.** Firmware baseline applies end-to-end; state covered in envtest.

### Phase 8 — REST API facade

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
  (`get,list,create,delete` on `onboardingrequests` only — no
  `onboardingservers`); `onboarding-server-subject-access-reviewer-binding` in
  `clusterrolebindings_services.yaml`; DD-004 updates to
  `oran_o2ims_user_roles.yaml` and `oran_o2ims_oauth_role_bindings.yaml`; and the
  `security` annotations in `openapi.yaml`.

**Tests.** `server_test.go` for handler behavior, converter round-trips, and the
k8s-error-to-HTTP-status mapping (NotFound→404, AlreadyExists→409,
Forbidden/BadRequest/Invalid→400, else 500).

**Exit criteria.** `make go-generate` clean; server serves the spec; RBAC files
regenerate through `make manifests && make bundle`.

### Phase 9 — LDAP authentication mode

**Goal.** Add the alternative `authMode=ldap` path as a self-contained feature.

**Deliverables.**

- `internal/onboarding/bmh/ldap.go` (illustrative name): declare LDAP
  configuration and the irreversible local-account disable via the Metal3 LDAP
  integration API,
  and interpret the resulting BareMetalHost conditions, per
  [§5](server-onboarding-implementation.md#5-metal3-bmc-configuration).
- `onboardingserver_controller.go`: the `ConfiguringLdap` state (in place of
  `UpdatingCredentials` when `authMode=ldap`) and the `DisablingLocalAccounts`
  state that runs after `Publishing` when `disableLocalAccounts` is set, per
  [§2](server-onboarding-service.md#2-server-onboarding-state-machine).
- `onboardingrequest_webhook.go`: `LdapConfig` validation (required fields when
  `authMode=ldap`).

**Tests.** envtest: `ConfiguringLdap` transition on `authMode=ldap`;
`DisablingLocalAccounts` after `Publishing` (best-effort — a failure does not fail
the server); `LdapConfig` webhook validation. Unit: `bmh/ldap.go` with a fake
client (asserts the desired LDAP config is declared and BMH conditions are mapped).
Integration: the
[LDAP mode variant](server-onboarding-implementation.md#ldap-mode-variant)
walkthrough end-to-end against real BMCs (driven via BMO).

**Exit criteria.** LDAP mode onboards end-to-end; the remaining §2 states have
coverage; `make ci-job` and `make test-e2e` pass.

### Phase 10 — Observability, scale tuning, and mTLS

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
                      Phase 0: API types
                    /        |         \
              Phase 1     Phase 2     Phase 3
              Webhook     BMH/Inv     BMC-config (local)
                    \        |         /
                     Phase 4 ──► Phase 5
                     OnbReq      state machine (local)
                                     │
                              Phase 6: Deploy + integration
        ══════════════════════ v1 released ══════════════════════
                                     │
              ┌──────────┬───────────┼───────────┐
              ▼          ▼           ▼            ▼
         Phase 7     Phase 8     Phase 9     Phase 10
         Firmware    REST        LDAP        Observability/
         baseline    facade      mode        scale / mTLS
```

Within v1, Phases 1, 2, and 3 proceed in parallel once Phase 0 lands; Phase 5 (the
state machine) is the critical path — it depends on 2, 3, and the CRs created in 4.
The post-v1 phases are independent of each other and can be scheduled in any order
after v1.

## Testing strategy

Coverage mirrors [§11 Verification](server-onboarding-implementation.md#11-verification):

- **Unit** (Phases 2, 3, 8): attribute matching, name derivation, BMC template
  rendering, `IncludeInInventory()`, the Metal3 BMC-config helpers against a fake
  client (desired-state set + BMH-condition mapping), REST converters and error
  mapping.
- **envtest** (Phases 1, 4, 5, 7, 9): webhook validation, controller
  reconciliation, state transitions (local path in Phase 5; firmware in Phase 7;
  LDAP in Phase 9), held-BMH deletion on mismatch, marker removal, finalizer
  cleanup, and a deleting-resource regression test per reconciler.
- **Integration/manual** (Phases 6, 9): on-cluster local-auth flow and the
  held-until-`Publishing` inventory invariant (Phase 6); the LDAP mode variant
  end-to-end (Phase 9).

Gomega conventions from AGENTS.md apply throughout (`.ToNot()`,
`Expect(err).ToNot(HaveOccurred())`).

## RBAC and manifests checklist

Per the RBAC guidance in AGENTS.md, verify the generated
`config/rbac/role.yaml` after every marker change (`make manifests`), and edit
service ClusterRoles by hand — they are not generated from markers:

- Controller-manager permissions come from `//+kubebuilder:rbac` markers (Phases 4,
  5, 7) and must include `hardwareprofiles` (get/list, Phase 7), the Metal3
  servicing resources, and the Metal3 BMC-config resources for networking/
  credentials/LDAP (Phases 5, 9 — exact kinds per the final BMC-config
  APIs).
- REST-server ClusterRole is hand-maintained and minimal (Phase 8).
- DD-004: user roles, OAuth bindings, and `openapi.yaml` `security` annotations
  are updated together whenever a public endpoint changes (Phase 8).

## Effort estimate

The per-phase days above are AI-assisted engineer-days and roll up to:

- **v1: ~24–38 days** — about **5–7 weeks** for one engineer, or **3–4 weeks** with
  two engineers exploiting the parallelism (Phases 1/2/3 fan out; one owns the
  Phase 5 long pole). A **demoable single-server happy path** lands earlier still.
- **Post-v1: ~12–22 days**, schedulable incrementally and independently.
- **Full proposal: ~36–60 days.**

These numbers assume prompt code review and an available test cluster with BMCs.
They **do not** compress the parts AI does not touch, which dominate the calendar:

- **Real-hardware BMC testing via the Metal3 APIs** (Phases 3, 5, 9) — validating
  the DHCP→static pin, credential, and LDAP operations end-to-end against physical
  BMCs (driven through BMO, not the onboarding controller). Vendor DHCP-lease and
  LDAP behavior is inconsistent; fakes pass, physical BMCs surprise. Bench time,
  not coding time — and gated on the Metal3 APIs landing (see below).
- **Cluster + e2e environment** (Phase 6) — needs a hub with Metal3/BMO, ESO, a
  vault, and BMC routing. Environment readiness, not code, usually gates this.
- **External prerequisites** (§9) — ESO/ClusterSecretStore, the vault, and
  (Phase 9) an LDAP server are often owned by other teams.
- **Open product inputs** (§12) — the scale numbers gate Phase 10 validation only,
  not the mechanism.

## Risks and open questions

- **Product inputs (non-blocking).** The batch-scale and per-Hub concurrency
  targets in [§12](server-onboarding-implementation.md#12-open-questions--product-inputs)
  gate only the Phase 10 scale validation, not the mechanism.
- **Dependency on forthcoming Metal3 APIs (sequencing).** The BMC-config states
  depend on forthcoming Metal3/BMO APIs: BMC networking config, BMC credentials
  provisioning, and LDAP integration, on top of fast out-of-band inspection.
  The service can be built against these interfaces, but a state is
  blocked until its API lands: local-auth v1 (Phases 3/5/6) needs fast inspection +
  BMC networking + BMC credentials; the LDAP mode (Phase 9) additionally needs
  LDAP integration. The exact API shape is not frozen — the Phase 3 orchestration
  helpers are the single reconciliation point. See
  [Dependency on forthcoming Metal3 APIs](server-onboarding-service.md#dependency-on-forthcoming-metal3-apis).
- **External prerequisites.** ESO, `ClusterSecretStore`, the external vault, and
  hub→BMC routing **from the Metal3/BMO pods** (not the onboarding pod) must be in
  place for v1 integration (Phase 6); a `HardwareProfile` CR is needed for Phase 7
  and an LDAP server for Phase 9. See
  [§9 Prerequisites](server-onboarding-implementation.md#9-prerequisites).
- **Vendor Redfish variance.** BMC DHCP-lease, DNS, and LDAP support is inconsistent
  across vendors (the BMC-config APIs note OEM-extension gaps); Phases 3/5/9 must
  honor `bmcSubnet`/`bmcGateway` when supplied rather than trusting the lease, and
  treat DNS/transition support as best-effort per vendor as BMO reports it.

## Definition of done

**v1 done** (see the [v1 done](#v1-done) line):

- Phases 0–6 merged; `make ci-job` and `make test-e2e` pass; generated tree clean.
- Every local-path state in
  [§2](server-onboarding-service.md#2-server-onboarding-state-machine) has
  unit/envtest coverage; the held-until-`Publishing` invariant is verified.
- Controller RBAC, must-gather, bundle, and the v1 Deployment are complete and
  regenerated.

**Full proposal done:**

- Phases 7–10 merged; firmware baseline, REST facade (with DD-004 RBAC), LDAP mode,
  and observability/mTLS complete; scale target met once §12 numbers land.
- The design and implementation documents remain consistent with the shipped code.
