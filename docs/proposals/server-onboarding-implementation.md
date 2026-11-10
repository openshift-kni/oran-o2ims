# Server Onboarding Service - Implementation

This is the companion to the
[**Server Onboarding Service - Design Proposal**](./server-onboarding-service.md),
which covers the context, architecture, API/CRD design, and the onboarding state
machine. Read that first for the high-level design; this document assumes it.

This document covers the detailed implementation: the detailed per-state
behavior, the Metal3 BMC configuration (orchestration), hardware inventory and BMH
creation, the controllers, project structure, prerequisites, an end-to-end
walkthrough, verification, and open questions. Network dependencies and security
considerations are covered in the design proposal (sections 3 and 4). For the
phased engineering plan that sequences building this — work breakdown, phase
dependencies, and exit criteria — see the
[**Server Onboarding Service - Implementation Plan**](./server-onboarding-implementation-plan.md).

---

## Server onboarding state machine — detailed per-state behavior

This section details each state introduced in the
[state-machine overview](server-onboarding-service.md#2-server-onboarding-state-machine)
of the design proposal. Read that overview first.

**`CreatingExternalSecrets` state:**

`CreatingExternalSecrets` is the first action after `Pending`. The controller
creates ExternalSecret CRs in the resource pool namespace. The ExternalSecrets
created depend on `authMode` and whether shared secrets already exist:

- **Shared ExternalSecrets (created once per OnboardingRequest, idempotent):**
  - `<onboardingRequestName>-default-creds` — factory-default BMC credentials for initial access.
  - `<onboardingRequestName>-ldap-bind` — (authMode=ldap only) LDAP bind DN credentials.
  - `<onboardingRequestName>-ldap-creds` — (authMode=ldap only) LDAP user credentials for BMH access.

- **Per-server ExternalSecrets (authMode=local only):**
  - `<onboardingRequestName>-<serverName>-desired-creds` — target BMC credentials unique to this server.

  Here and throughout this document, `<serverName>` is the OnboardingServer CR's
  `metadata.name` — the collision-safe name derived from the server's
  `bmcAddress` (see [OnboardingServer CR naming](server-onboarding-service.md#1-api-design-crds)). The
  examples show only its readable stem (e.g. `10-10-10-101`).

Each ExternalSecret references the `ClusterSecretStore` configured globally on the
onboarding controller (via `--secret-store-name` flag or `SECRET_STORE_NAME` env
var, defaulting to `onboarding-vault`). The External Secrets Operator (ESO), which
is pre-configured on the hub as a prerequisite, syncs these into Kubernetes Secrets
from the external vault. The controller gates the transition to `Inspecting` on
**ESO's sync status, not the Secret contents**: it watches each ExternalSecret's
`.status` (the `Ready` / `SecretSynced` condition ESO sets) and never reads the
synced Secret's `data`, so no BMC credential bytes ever enter the controller
process. At minimum the shared default-credentials ExternalSecret must be synced,
since the BMH created in `Inspecting` references the resulting Secret via
`spec.bmc.credentialsName` — and **BMO, not the controller, reads it**.

> **New external integration.** The codebase has no ESO integration today — no
> ESO API types are vendored, registered in any scheme, or referenced by any
> RBAC. Adding this service therefore means: vendoring the ESO
> `external-secrets.io/v1` `ExternalSecret` API type into `go.mod`, registering
> it in the onboarding controller's scheme, and granting the controller
> ServiceAccount access to `externalsecrets` in its ClusterRole (the verbs are
> enumerated in the [RBAC table](server-onboarding-service.md#4-security-considerations)). This is a
> genuinely new dependency; it is not reused from an existing service. Note the
> onboarding controller adds **no** BMC/Redfish client of its own — all BMC I/O
> is delegated to Metal3/BMO (see [Metal3 BMC configuration](#5-metal3-bmc-configuration)),
> so ESO is the one substantial new external integration. ESO itself (the operator,
> the `ClusterSecretStore`, and the backing vault) remains an out-of-scope
> deployment prerequisite (see [Prerequisites](#9-prerequisites)).

**`Inspecting` state:**

`Inspecting` is where the BMH is first created — with the pool's **shared default
credentials**, fast out-of-band inspection (`spec.inspectionMode: fast`), and the
**inventory-hold marker** (`onboarding.openshift.io/inventory-hold: "true"`).
Creating the BMH is what performs authentication: BMO registers the host against
the BMC using the default credentials and then runs out-of-band (Redfish)
inspection to gather its hardware.

1. Create the BMH (see
   [Hardware Inventory and BMH Creation](#6-hardware-inventory-and-bmh-creation)
   for the full spec). Record the chosen name in `status.bmhName`.
2. Watch the BMH. BMO registers it and runs fast out-of-band (Redfish) inspection
   against the BMC — no ramdisk boot — populating the `HardwareData` CR /
   `status.hardwareDetails` in seconds.
3. **On registration/authentication failure** — BMO reports an error status
   (`registration error`, credentials rejected, BMC unreachable) — retry with
   backoff; if unrecoverable, transition to `AuthFailed` (terminal). Because a
   previously onboarded BMC no longer accepts the pool default credentials, this
   is the state that catches it.
4. **When inspection completes** (`HardwareData` available), transition to
   `Inventorying`.

Because the BMH is created here rather than at the end, the host exists — but held
out of inventory — until it is published at the `Publishing` state. See the point-of-no-return
and cleanup notes below for how a held BMH is deleted on mismatch or failure.

**`Inventorying` state:**

The controller reads the inspection result — the `HardwareData` CR (and BMH
`status.hardwareDetails`) — and compares it against the desired vendor and, if
declared, the pool's hardware attributes (`spec.hardwareAttributes` is optional;
see [Hardware attribute matching](server-onboarding-service.md#1-api-design-crds)):

- **Vendor** — `HardwareData.Spec.HardwareDetails.SystemVendor.Manufacturer`
  must match `spec.bmcVendor` under the case-insensitive substring rule (see
  [Vendor matching](server-onboarding-service.md#1-api-design-crds)), so `Dell` matches the inspected
  `Dell Inc.`; otherwise `VendorMismatch` (terminal).
- **Hardware attributes** — each `hardwareAttributes` entry is compared against the
  corresponding inspected field using its `operator`; any failure yields
  `HwMismatch` (terminal). `status.stateMessage` names the attribute, operator,
  expected, and inspected value.

On either mismatch the controller **deletes the held BMH** (it is still on
inventory-hold and has never been published, so deletion is safe and removes the
host from the pool namespace) and counts the server in `serversFailed`. On a full
match, transition to `ApplyingHardwareProfile` when `spec.hardwareProfileName` is
set, or straight to `PinningBmcAddress` when it is not.

**`ApplyingHardwareProfile` state (only if `spec.hardwareProfileName` is set):**

This state brings the host to the desired firmware/BIOS baseline before any BMC
reconfiguration. It is **skipped** when `spec.hardwareProfileName` is empty
(`Inventorying` transitions directly to `PinningBmcAddress`). The BMH is still
**held** and still on the pool **default credentials** here — the baseline is
applied before the credential swap so a BMC firmware update (which can bounce the
BMC) cannot land in the middle of a credential change.

1. Fetch the referenced `HardwareProfile` CR (`clcm.openshift.io/v1alpha1`) from
   the O-Cloud manager namespace. If it does not exist, transition to `Failed`
   with `status.stateMessage` naming the missing profile (the webhook checks this
   at admission too, but the reconcile re-checks to cover deletion races).
2. Render it into the Metal3 servicing CRs next to the BMH (matched by
   name/namespace in the resource pool namespace), reusing the hardware manager's
   existing translation (`processHwProfile` and the `HostFirmwareSettings` /
   `HostFirmwareComponents` / `HostUpdatePolicy` managers): BIOS attributes →
   `HostFirmwareSettings.Spec.Settings`; firmware versions → `HostFirmwareComponents.Spec.Updates`
   (`{component, url}` for `bios`/`bmc`/`nic:*`); and a `HostUpdatePolicy` gating
   both (`firmwareSettings` / `firmwareUpdates: onReboot`).
3. Watch the BMH and the servicing CRs. BMO applies the settings and flashes the
   firmware images, rebooting the host as needed. Convergence is confirmed from
   status — `HostFirmwareComponents.Status.Components[].CurrentVersion` matches the
   desired versions and `HostFirmwareSettings.Status.Settings` reflect the desired
   attributes — reusing the hardware manager's existing verification helpers.
4. **On success**, transition to `PinningBmcAddress`. **On failure** (invalid
   settings for the host's `FirmwareSchema`, a flash error, or timeout) transition
   to `Failed`; the still-held BMH is deleted on cleanup, exactly as for a
   pre-publish mismatch.

Idempotency: the servicing CRs are created if absent and reconciled to the desired
spec if present, so a requeued reconcile adopts what already exists rather than
duplicating it (the same adopt-existing pattern as the BMH in `Inspecting`).

**`PinningBmcAddress` state (both auth modes):**

Before onboarding, each BMC obtains its management IP from a DHCP server (a
prerequisite — see [Prerequisites](#9-prerequisites)). Now that the server has
been validated (reachable, correct vendor, matching hardware), the controller
**requests that the BMC management NIC be converted from DHCP to static, reusing
the same DHCP-leased address** so the BMC keeps the identity already recorded in
`spec.bmcAddress` and in the BMH. The controller does **not** talk to the BMC
itself — it declares the desired networking via the Metal3 **BMC networking config
API**, and BMO performs the DHCP→static transition (and DNS) over Redfish:

1. Determine the static mask/prefix, gateway, and (optionally) DNS to request. The
   address family is taken from `spec.bmcAddress` — IPv4 or IPv6 (see the IPv6 note
   after step 3). If `spec.bmcSubnet` / `spec.bmcGateway` are set (recommended),
   they are supplied to the API as the static subnet/gateway (`bmcSubnet` carries
   the prefix length, same family as `bmcAddress`). If they are unset, BMO derives
   the static mask/gateway from the BMC's current DHCP lease. (Per the BMC
   networking config API, vendor DHCP-lease reporting is inconsistent — the gateway in particular is often
   absent — so supplying `bmcSubnet`/`bmcGateway` removes a silent failure mode; if
   neither the request nor the lease yields a usable mask/prefix **and** gateway,
   BMO reports the failure on the BMH and the controller transitions the server to
   `Failed`, with `status.stateMessage` advising to set `bmcSubnet`/`bmcGateway`.)
2. Declare the desired static networking on the BMH / companion Metal3 resource
   (exact shape per the final BMC networking API — **illustrative** here): the same
   `bmcAddress` as a static assignment, DHCP disabled, with the mask/prefix and
   gateway from step 1, plus any DNS servers. BMO applies this over Redfish
   (disabling DHCP, pinning `IPv4StaticAddresses` / `IPv6StaticAddresses` + default
   gateway as appropriate) and reports progress/outcome via BareMetalHost status
   conditions.
3. The controller **watches the BareMetalHost conditions** for the networking
   operation to report success (BMO confirms the BMC remains reachable at the same
   address after the change). A reported failure moves the server to `Failed`.

This step runs for **both** auth modes and is **reversible** — the controller can
request a revert of the NIC to DHCP through the same API (see the cleanup table).
Because the address is unchanged, no update to `spec.bmcAddress` is needed. It is
deliberately placed before any credential or LDAP change so that the point of no
return is still the credential change (local) or `DisablingLocalAccounts` (ldap).
To ensure the leased address is not handed to another host — not only in the window
between lease and pinning, but for the **entire lifetime of the static assignment** —
each BMC's address must be **durably reserved** (a per-MAC DHCP reservation) or
**excluded from the DHCP pool in IPAM**. After pinning, the BMC stops renewing its
lease, so a non-durable lease could expire and be reassigned to another client,
colliding with the now-static BMC. The reservation/exclusion is released only when the
static assignment is removed (host decommission). On success, transition to
`UpdatingCredentials` (local) or `ConfiguringLdap` (ldap).

An IPv6 `bmcAddress` uses the IPv6 equivalents (DHCPv6 disabled, `IPv6StaticAddresses`
plus a static default gateway) and an IPv6 `bmcSubnet` CIDR; the controller only
declares the desired values, so the family difference is handled by BMO.

**`ConfiguringLdap` state (authMode=ldap only):**

Instead of `UpdatingCredentials`, the controller configures LDAP authentication on
the BMC through the Metal3 **LDAP integration API** — it declares
the desired LDAP configuration and BMO applies it over Redfish:

1. Declare the LDAP settings (exact shape per the final LDAP integration API —
   **illustrative** here): service addresses, bind credentials, base DN,
   username/group attributes, and remote role mappings from the OnboardingRequest's
   `ldapConfig`. BMO applies them to the BMC's Redfish Account Service.
2. Watch the BareMetalHost conditions for BMO to report LDAP **configured and
   verified** — not merely a TLS connection to the directory, but a successful
   **authentication and authorization** of the BMH's LDAP credentials with the
   required admin role. A TLS handshake alone does not prove the credentials or the
   role mapping are valid; publishing (or later disabling local accounts) on that
   basis could leave a live BMH unable to access its own BMC. The server does **not**
   advance to `Publishing` until BMO confirms authenticated access. The LDAP
   integration API also provides ESO credential synchronization, keeping the BMC
   admin Secret used by BMO in step with the external vault.
3. Disabling the local default account (`ldapConfig.disableLocalAccounts: true`) is
   **not** done here — it is deferred to the dedicated `DisablingLocalAccounts`
   state after `Publishing`. See the ordering note below.

Configuring LDAP is **reversible** — unlike local credential changes, LDAP can
be disabled and local auth re-enabled through the same API — but **only while the
local account is still enabled**. Disabling the local account (step 3) removes the
credentials BMO would use to undo the change, so once `disableLocalAccounts` is
applied the operation is no longer reversible.

To preserve reversibility, the controller **does not disable local accounts
during `ConfiguringLdap`**. `disableLocalAccounts` is instead applied in a
dedicated **`DisablingLocalAccounts`** state that runs **after `Publishing`** (the host is
published and LDAP has been confirmed working end-to-end), immediately before
`Completed`. When `disableLocalAccounts` is false (the default), the `DisablingLocalAccounts`
state is skipped and `Publishing` transitions straight to `Completed`.
Consequences:

- Through `ConfiguringLdap` → `Publishing`, the local account remains enabled,
  so a failed server can be rolled back (disable LDAP, re-enable local auth) as
  described in the cleanup table.
- The "point of no return" for the LDAP workflow is the `DisablingLocalAccounts` state. If
  `disableLocalAccounts` is false (the default), there is no irreversible step —
  the local account is always a recovery path.
- `DisablingLocalAccounts` runs against an already-published, fully-functional host, so it is
  **best-effort**. Because the disable is irreversible, the controller **re-confirms
  authenticated LDAP access** (via the BMH conditions, as in `ConfiguringLdap` step 2)
  immediately before applying it; if that check does not pass it does **not** disable
  the local account — it records the `LocalAccountsDisabled` condition as not-done and
  completes with local auth intact. A failure there never fails the server. Transient errors are
  retried with backoff; if they persist past the retry limit, the controller
  records the failure in `status.stateMessage`, emits a `Warning` event, and
  still transitions to `Completed` (local auth remains available as a fallback).
  A `LocalAccountsDisabled` condition on the OnboardingServer records whether the
  hardening actually completed, so an incomplete hardening is still observable.

After LDAP is confirmed working, the controller **swaps** the already-created
BMH's `spec.bmc.credentialsName` from the shared default-credentials Secret to the
`<onboardingRequestName>-ldap-creds` Secret (LDAP user credentials, shared). From
then on BMO authenticates to the BMC using these credentials, and the BMC
validates them against the LDAP server. The controller then transitions to
`Publishing`.

**`UpdatingCredentials` state (authMode=local only):**

The BMH already exists (created in `Inspecting` with the shared default
credentials). In this state the controller requests the credential change through
the Metal3 **BMC credentials provisioning API**, supplying the
per-server desired credentials (the ESO-synced
`<onboardingRequestName>-<serverName>-desired-creds` Secret). BMO applies the new
credentials to the BMC over Redfish, verifies it can authenticate with them, and —
per the BMC credentials provisioning API — propagates the change to the BareMetalHost's referenced
credential Secret so BMO keeps working after the change. The controller watches the
BareMetalHost conditions for success, then transitions to `Publishing`.

The exact mechanism by which the desired credentials are handed to the API and
reconciled onto the BMH Secret (controller re-points `spec.bmc.credentialsName` to
the per-server Secret vs. BMO updating the referenced Secret in place) is
**illustrative and subject to the final BMC credentials provisioning API**; either way the
default-credentials Secret is shared and is never overwritten with one server's
credentials. This is the local-mode point of no return — once the BMC holds the
per-server credentials the pool defaults no longer work (see below).

**`Publishing` state:**

`Publishing` is the final step before `Completed` (or before `DisablingLocalAccounts`, when
authMode is `ldap` and `disableLocalAccounts` is true). The controller **removes the
inventory-hold marker** (`onboarding.openshift.io/inventory-hold`) from the BMH.
Only now does the existing O-Cloud inventory pipeline surface the host as an O2 IMS
Resource (see [O2 IMS inventory population](#o2-ims-inventory-population)) — the
host is not visible to the O-RAN/O2 IMS inventory at any earlier point. Removing
the marker is idempotent; once it is gone the controller transitions to
`DisablingLocalAccounts` (when authMode is `ldap` and `disableLocalAccounts` is true) or
`Completed` (otherwise).

**Error handling:**

- Transient errors: increment `status.retryCount`, set `status.lastAttemptAt`, stay in current state. Controller requeues with backoff.
- Permanent errors: transition to terminal state with detail in `status.stateMessage`
- Max retries: 5 (configurable), then transition to `Failed` — **except** the
  `DisablingLocalAccounts` state, which is best-effort: on exhausted retries it transitions to
  `Completed` (not `Failed`) and records the incomplete hardening in
  `status.stateMessage` and the `LocalAccountsDisabled` condition
- Backoff: `min(30s * 2^retryCount, 5m)` — implemented via controller requeue delay
- Idempotency: each step checks whether its work is already done before acting
  (e.g., if credentials were already changed, use desired creds for subsequent
  steps; if the BMH already exists it is adopted rather than re-created in
  `Inspecting`). Every reconcile also starts with an **already-onboarded check** —
  but **only for a CR with no in-progress state of its own** (`status.state`
  empty or `Pending`, i.e. a freshly created or recreated OnboardingServer). For
  such a CR, if a **published** BMH for this server already exists (one whose
  inventory-hold marker has been removed), the controller short-circuits to
  `Completed` without re-inspecting or mutating the BMC. A CR that is **already
  progressing** through the state machine (any other `status.state`, including
  `DisablingLocalAccounts`, which by design runs against an already-published BMH) is **not**
  short-circuited — the controller resumes its state machine so post-`Publishing`
  steps like `DisablingLocalAccounts` still run. A BMH that still carries the inventory-hold
  marker is a mid-onboarding host (created in `Inspecting` but not yet published)
  and likewise does **not** short-circuit — the controller resumes the state
  machine from where it left off (see
  [Deleting a Completed server](server-onboarding-service.md#deleting-a-completed-server-idempotent-re-onboarding))

**Cleanup on failure:**

Cleanup only applies to **per-server resources**. The shared ExternalSecret CRs (default credentials, LDAP bind/user) cannot be cleaned up when an individual server fails. Shared ExternalSecrets are only cleaned up when the OnboardingRequest itself is deleted (via finalizer).

| Created at state | Resource / side effect | Scope | Cleanup action |
|---|---|---|---|
| `CreatingExternalSecrets` | Shared ExternalSecret CRs + synced K8s Secrets | **Request-level** (shared) | No per-server cleanup. Deleted only via OnboardingRequest finalizer. |
| `CreatingExternalSecrets` | Per-server ExternalSecret CR + synced K8s Secret (local mode) | **Server-level** | Delete the ExternalSecret and synced Secret, **unless a published BMH references it** (`spec.bmc.credentialsName`) — published BMHs are live, so their credential Secret is retained. |
| `Inspecting` | BMH created (default creds, inventory-hold marker) | **Server-level** | **Delete the BMH if it is still held** (inventory-hold marker present, never published). This is safe — a held BMH has never appeared in O2 IMS inventory. A **published** BMH (marker removed, at/after `Publishing`) is live infrastructure and is **never** deleted (see [Deletion semantics](server-onboarding-service.md#deletion-semantics)). |
| `ApplyingHardwareProfile` | HostFirmwareSettings/Components + HostUpdatePolicy CRs (matched to BMH) | **Server-level** | **Deleted with the held BMH.** Flashed firmware/BIOS is **not** rolled back (a baseline) — re-onboarding re-applies it. Only when `hardwareProfileName` set. |
| `PinningBmcAddress` (both modes) | BMC NIC pinned from DHCP to static (same IP) | **Server-level** | **Reversible (best-effort)** — request a revert of the NIC to DHCP via the Metal3 BMC networking config API; BMO applies it. The address is unchanged, so a no-op for the BMH. Runs before any credential change, so rollback creds are the defaults. |
| `UpdatingCredentials` (local) | BMC credentials changed to per-server desired | **Server-level** | **Not reversible** — the default credentials are no longer valid. `status.stateMessage` notes this. |
| `ConfiguringLdap` (ldap) | LDAP configured on BMC | **Server-level** | **Reversible while the local account is enabled** — request disabling LDAP / re-enabling local auth via the Metal3 LDAP integration API; BMO applies it (best-effort). The controller never disables the local account here, so rollback creds stay available. |
| `Publishing` | Inventory-hold marker removed; BMH published | **Server-level** | **No cleanup** — once published the BMH is live infrastructure and is never deleted or re-held; the deletion guard blocks request teardown while it exists (see [Deletion semantics](server-onboarding-service.md#deletion-semantics)). |
| `DisablingLocalAccounts` (ldap) | Local account disabled (`disableLocalAccounts: true`) | **Server-level** | **Not reversible** — with the local account gone there is no non-LDAP path to re-enable it. Applied after `Publishing`, before `Completed`; best-effort. |

**Per-server cleanup order:** BMC rollback — `UpdatingCredentials` (no-op, not
reversible) or `DisablingLocalAccounts` (also not reversible — the local account is
gone; this state is only reached after `Publishing`, so the BMH is retained anyway)
or `ConfiguringLdap` (reversible — disable LDAP, re-enable local auth;
safe because local accounts are still enabled at this point) → revert a pinned BMC
NIC to DHCP (`PinningBmcAddress` onward, best-effort) → delete the held BMH
(`Inspecting` onward, only while the BMH still carries the inventory-hold marker;
retained when a published BMH exists) → `CreatingExternalSecrets` (delete per-server
ExternalSecret, only if no
published BMH depends on it)

**Cleanup tracking:** Rollback selection keys off **`status.mutationProgress`**, not
`status.state`. On a failure `status.state` collapses to `Failed` and no longer says
which BMC mutation was in flight, so it cannot drive rollback. Instead the controller
records each operation in `status.mutationProgress` as it goes — `Requested` before
declaring the desired state on the BMH / companion CR, then `Applied` / `Failed` when
BMO reports the outcome — and the finalizer rolls back exactly the operations that
reached `Requested` or `Applied` (an op left at `Requested` is the partially-applied
case that still needs reverting), marking each `RolledBack`. `status.cleanupStatus`
(`pending`, `completed`, `partial`) is the separate **outcome** of that run: a
best-effort step that fails is logged in `status.stateMessage` and recorded `partial`,
but does not block other cleanup.

**Cleanup mechanism (OnboardingServer finalizer):**

The cleanup actions above are executed by a **finalizer on the OnboardingServer
CR**, added when the controller first reconciles it. Deleting an OnboardingServer
— whether the operator does it to retry a failed server (see
[Retrying a failed server](server-onboarding-service.md#retrying-a-failed-server)) or Kubernetes
garbage-collects it when the parent OnboardingRequest is torn down — sets
`deletionTimestamp` and blocks removal until the finalizer has run. This is what
wires the cleanup table to an actor and gives `status.cleanupStatus` a writer:
without the finalizer, an `oc delete onboardingserver` would drop the CR and
orphan its per-server resources. On finalization the controller:

1. Sets `status.cleanupStatus: pending`.
2. **If the BMH is published, skip all BMC rollback and deletion** (marker removed,
   at/after `Publishing`): a published BMH is live infrastructure the finalizer must
   never mutate or delete. In particular it does **not** revert the BMC NIC to DHCP —
   doing so would drop the static-address guarantee and could change the address the
   live host answers on — and it **retains** the BMH and the per-server
   ExternalSecret/Secret (referenced by `spec.bmc.credentialsName` for the life of
   the host). Those shared/per-server credentials are removed later by the
   OnboardingRequest finalizer, which runs only after all published BMHs are gone (see
   [Deletion semantics](server-onboarding-service.md#deletion-semantics)).

   Otherwise (a **held** BMH, inventory-hold marker present — onboarding did not reach
   `Publishing`), run the per-server cleanup in the order above, selecting what to roll
   back from **`status.mutationProgress`** (not `status.state`, which may be `Failed`)
   so a partially-applied op — recorded `Requested` but not `Applied` — is still
   reverted:
   - **Reversible BMC rollback** for any `ConfigureLdap` op at `Requested`/`Applied`
     (request disable-LDAP / re-enable-local-auth via the Metal3 LDAP API; a no-op for
     `ProvisionCredentials`, which is irreversible, and for pre-mutation states), and
     the **NIC-to-DHCP revert** for a `PinNetwork` op at `Requested`/`Applied` (via the
     Metal3 networking API, best-effort; the address is unchanged so the BMH is
     unaffected). Each reverted op is marked `RolledBack` in `status.mutationProgress`.
   - **Wait for BMO to report the requested rollback settled** before deleting the
     held BMH. Because these operations are applied asynchronously by BMO, deleting
     the BMH immediately could drop a rollback BMO has not yet performed. The
     finalizer watches the BMH conditions for the rollback to complete, or, on a
     bounded timeout, records `status.cleanupStatus: partial` (with the attempted
     operation in `status.stateMessage`) and proceeds. Deleting the BMH's parent
     Metal3 config state (if any) along with the BMH is acceptable only once the
     rollback has settled or the timeout has elapsed.
   - **Delete the held BMH** (it has never appeared in O2 IMS inventory), then delete
     the per-server ExternalSecret and its ESO-synced Secret.
3. Sets `status.cleanupStatus: completed` if every step succeeded, or `partial`
   if a best-effort step failed. `partial` is logged and recorded in
   `status.stateMessage` but **does not block** finalizer removal — cleanup never
   wedges a delete.
4. Removes the finalizer, allowing Kubernetes to delete the object.

The per-server ExternalSecret carries an owner reference to the **OnboardingRequest**
(not the OnboardingServer), so that deleting or retrying an individual
OnboardingServer never garbage-collects credentials a live BMH still depends on;
the ExternalSecret is GC'd as a backstop only when the whole OnboardingRequest is
torn down.

**Composition with the OnboardingRequest finalizer:** the two finalizers divide
responsibility by scope, and the **order matters**. The OnboardingRequest finalizer
first **quiesces and drains** the children — stopping new mutations and waiting for
any in-flight BMC operation to settle — then lets owner-reference GC delete the
OnboardingServer CRs so each per-server finalizer runs its **per-server**
rollback/cleanup **while the shared credential Secrets still exist**, and only
**afterwards** removes the **shared** ExternalSecrets (see
[Deletion semantics](server-onboarding-service.md#deletion-semantics)). After the
drain, every surviving server is one of: **pre-mutation** (nothing to roll back);
**past `Publishing`** (a published BMH — the finalizer never rolls back or deletes
it, and the published-BMH gate blocks whole-request teardown until it is
decommissioned); or **held in a terminal-failure state**. That last case is the one
that still performs a real rollback — e.g. a `ConfiguringLdap` that failed before
`Publishing`: disable LDAP / re-enable local auth on the held BMC. It is reversible
precisely because local accounts are still enabled pre-`Publishing`, and it runs
before the shared credentials are removed, so BMO still has the credentials it needs
to apply it.

**Terminal mismatch states (`VendorMismatch`, `HwMismatch`):** Both are reached
from `Inventorying` — after inspection but before any Redfish mutation. The BMH
created in `Inspecting` exists and is still held, so cleanup **deletes that held
BMH** (safe — it never entered O2 IMS inventory) in addition to the per-server
ExternalSecret in local mode. The `Inventorying` transition itself deletes the BMH
immediately on mismatch; the finalizer's held-BMH deletion is a backstop.

**`AuthFailed`:** Occurs in `Inspecting`, when BMO cannot register/authenticate
the newly-created BMH with the pool default credentials — before any BMC mutation.
This is also where **BMC reachability** is validated: a BMC that is unreachable
(no network path, TLS failure, timeout) or that rejects the default credentials
lands here after the transient-retry budget is exhausted. **Reachability failure
is isolated per server** — an unreachable or unauthenticated BMC fails only that
server's OnboardingServer; the rest of the batch continues onboarding unaffected.
There is no batch-wide pre-flight reachability gate, and one bad BMC never blocks
the others. A held BMH may exist (creation succeeded but registration failed);
cleanup deletes it along with the per-server ExternalSecret.

## 5. Metal3 BMC configuration

The onboarding controller performs **no BMC I/O of its own** — it has no Redfish
client and opens no connection to any BMC. Every BMC operation is
**declared** on the BareMetalHost (or a companion Metal3 resource) and **applied by
BMO** over Redfish; the controller only writes Kubernetes resources and **watches
BareMetalHost status conditions** for the outcome. This removes the BMC credential
and network attack surface from the onboarding pod entirely (see
[Security Considerations](server-onboarding-service.md#4-security-considerations)).

These capabilities are forthcoming Metal3 features (see
[Dependency on forthcoming Metal3 APIs](server-onboarding-service.md#dependency-on-forthcoming-metal3-apis)).
This section describes what the onboarding flow consumes; the exact API shape
(fields on `BareMetalHost` vs. companion CRs) is **illustrative and subject to the
final Metal3 APIs**.

### Operations

The controller drives three BMC-config operations declaratively and relies on BMO
for inspection — in every case it writes a desired state and watches BMH
conditions, never calling the BMC:

| State | Capability (tracking) | What the controller declares | How it observes completion |
|------|----------------------|------------------------------|----------------------------|
| `Inspecting` | Fast OOB inspection | `spec.inspectionMode: fast` on the BMH | `HardwareData` populated; BMH reaches `available` |
| `PinningBmcAddress` | BMC networking | DHCP→static pin (same `bmcAddress`), mask/gateway, optional DNS | BMH condition reports the networking op succeeded (BMC reachable at the same address) |
| `UpdatingCredentials` | BMC credentials | Desired per-server credentials (from the ESO-synced Secret) | BMH condition reports credentials applied + BMH Secret reconciled |
| `ConfiguringLdap`, `DisablingLocalAccounts` | LDAP integration | LDAP config (and, post-`Publishing`, local-account disable) | BMH condition reports LDAP configured/working; ESO keeps the BMC Secret in sync |

**Vendor and hardware validation is not an onboarding-controller operation.** The
controller never queries `/redfish/v1/Systems/{id}` or any BMC endpoint for
vendor/hardware. Instead the BMH is created with `spec.inspectionMode: fast`
(`Inspecting`), and BMO's fast out-of-band (Redfish) inspection produces a
`HardwareData` CR. The controller compares that inspection result — including
`SystemVendor.Manufacturer` for the vendor check (case-insensitive substring, so
`Dell` matches `Dell Inc.` — see [Vendor matching](server-onboarding-service.md#1-api-design-crds)) —
against `bmcVendor` and `hardwareAttributes` during `Inventorying`. A vendor
mismatch yields `VendorMismatch`; any other attribute mismatch yields `HwMismatch`;
both delete the held BMH. See [Hardware attribute matching](server-onboarding-service.md#1-api-design-crds)
for the catalog, units, and comparison rules.

**BMC Redfish transport is BMO's concern.** TLS verification of the BMC's serving
certificate, per-request timeouts, retries, and account discovery (the default admin
account ID varies by vendor — Dell/HPE typically `1`, Lenovo varies) are all
handled inside BMO's Redfish implementation, not the onboarding controller. The
controller only sets `spec.bmc.disableCertificateVerification` on the BMH — **`false`
by default so BMO verifies the certificate**, and `true` only when the pool explicitly
opts in via `spec.disableBmcCertificateVerification` for self-signed BMCs — and
otherwise leaves BMC transport entirely to BMO.

**Vendor Redfish coverage is partial.** Per the BMC-config APIs, DHCP toggle,
`StaticNameServers`, and LDAP Account-Service fields are inconsistent across
Dell/HPE and may require OEM extensions; treat DNS and the DHCP→static transition
as best-effort per vendor until BMO reports them supported on the BMH. This is the
same per-vendor caveat called out for fast inspection under
[Hardware attribute matching](server-onboarding-service.md#1-api-design-crds).

**Previously configured servers:** If a BMC already has non-default credentials
(e.g., previously onboarded or manually configured), BMO cannot register the BMH
created in `Inspecting` — it fails to authenticate with the pool default
credentials — and the server transitions to `AuthFailed`. This is a terminal state;
there is no automatic retry with alternative credentials. The operator must reset
the BMC credentials manually or provide the correct default credentials in the
OnboardingRequest. This is a known limitation.

### IPv6 address handling

When the BMC address is IPv6, the `spec.bmc.address` Redfish URL that BMO uses to
reach the BMC uses bracket notation: `https://[fd00:bmc::101]/redfish/v1/`. The
onboarding controller still constructs this string from the `bmcAddressTemplate`
(the bracketing is applied automatically — see
[BMC address template defaults](server-onboarding-service.md#1-api-design-crds)); BMO consumes it. The
DHCP→static pin for an IPv6 `bmcAddress` likewise uses the IPv6 form (DHCPv6
disabled, `IPv6StaticAddresses` plus a static default gateway) and an IPv6
`bmcSubnet` CIDR, but the controller only declares the desired values — BMO applies
the family-appropriate Redfish changes.

**Concurrency:** The onboarding controller issues no BMC calls, so there is no
Redfish worker pool to size. It writes BMH / companion CRs and waits on BMH
conditions via watch + requeue; BMO owns BMC-side concurrency (how many BMCs it
contacts at once, rate-limiting, session handling). The OnboardingServer
controller's `MaxConcurrentReconciles` bounds reconcile invocations, not BMC
connections — see [Controllers](#7-controllers).

---

## 6. Hardware Inventory and BMH Creation

Hardware inventory and BMH creation follow a **single flow**. The BMH is created
early — in the `Inspecting` state — with the pool's shared default credentials,
fast out-of-band inspection (`spec.inspectionMode: fast`), and the
**inventory-hold marker**. BMO then runs out-of-band (Redfish) inspection against
the BMC — no ramdisk boot — producing a `HardwareData` CR in seconds that the
controller compares against the desired profile (`Inventorying`). On a match, the
controller mutates the BMC (credentials or LDAP), swaps the BMH's credential
reference, and finally removes the inventory-hold marker (`Publishing`) so the
existing O-Cloud inventory pipeline surfaces the host. On a mismatch the held BMH
is deleted. Because inspection is out-of-band, onboarding needs **no provisioning
network** — only hub→BMC Redfish (HTTPS) connectivity (see
[Prerequisites](#9-prerequisites) and
[Network Dependencies](server-onboarding-service.md#3-network-dependencies)).

### BMH and per-host Secret naming

The examples below write the BMH as `onboarded-<bmcAddress>` for readability, but a
raw address is not safe to use directly as a Kubernetes object name (dots/colons,
IPv6 uppercase and `::` compression). Crucially, the BMH is created **before**
inspection runs, so the server's **serial number is not yet known** and cannot be
used in the name — the name must derive from the `bmcAddress`, which is known up
front and validated unique within the pool.

The BMH name is therefore derived from the `bmcAddress` using the same
collision-safe, canonicalize-then-stem-plus-hash approach as the OnboardingServer
name (see [OnboardingServer CR naming](server-onboarding-service.md#1-api-design-crds)): canonicalize the
address (RFC 5952 for IPv6), build a readable stem (lowercase, `.`/`:` → `-`),
prefix the address family (`v4-`/`v6-`), and append an 8-character SHA-256 suffix
over the canonical address. Because `bmcAddress` is unique within the pool, the
BMH name is unique within the resource pool namespace.

So `onboarded-<bmcAddress>` is really `onboarded-<v4|v6>-<addr-stem>-<hash>`, e.g.
`onboarded-v4-10-10-10-101-a1b2c3d4`.
The OnboardingServer records the final BMH name in `status.bmhName`, so the
mapping is always observable. For brevity, the examples below show only
`onboarded-<bmcAddress>` (e.g. `onboarded-10-10-10-101`).

### BMH creation (Inspecting state)

When a server reaches `Inspecting`, resources are created in the **resource pool
namespace** (namespace name = `resourcePoolName` from the onboarding request). The
namespace is created if it does not already exist. The BMH is created with the
pool's **shared default credentials** and `spec.inspectionMode: fast` so BMO runs
a fast out-of-band (Redfish) hardware inspection against the BMC. Only two
resources are created per server here: the BMH and its (ESO-synced) BMC credential
Secret — there is **no Network Data Secret**, because out-of-band inspection never
boots the host over a provisioning network.

- **BareMetalHost CR** (`onboarded-<bmcAddress>`) with:
  - Annotations:
    - `onboarding.openshift.io/inventory-hold: "true"` — the **inventory-hold
      marker**. It keeps the host out of the O2 IMS inventory for the entire
      onboarding process; it is removed only at `Publishing` (see below).
  - `spec.bmc.address`: from `bmcAddressTemplate` with the server's `bmcAddress`
  - `spec.bmc.credentialsName`: reference to the **shared default-credentials**
    Secret (`<onboardingRequestName>-default-creds`). This is swapped to the
    per-server (local) or LDAP Secret later, after the BMC is mutated.
  - `spec.bmc.disableCertificateVerification`: **`false` by default** — BMO verifies
    the BMC's serving certificate. It is set to `true` **only** when the pool opts in
    via `spec.disableBmcCertificateVerification` (self-signed BMCs with no trusted CA)
    — an explicit, per-pool exception, since disabling verification lets an attacker on
    the management path impersonate a BMC and intercept its credentials. Where the
    BMCs' certificates can be trusted (a CA bundle configured for BMO), it stays
    `false`. See [Security Considerations](server-onboarding-service.md#4-security-considerations).
  - `spec.inspectionMode: fast` — selects fast out-of-band (Redfish) inspection;
    BMO queries the BMC directly and produces the `HardwareData` used for matching,
    with no ramdisk boot and no provisioning network
  - `spec.bootMACAddress`: **not set at creation** — it is not needed for
    out-of-band inspection. But the normal provisioning fallback
    `setBootMACAddressFromLabel()` **errors when both** `spec.bootMACAddress` and the
    boot-interface label are absent, so the controller must supply boot-interface
    information **before publication** or an allocated host cannot be provisioned
    without manual repair. Fast inspection enumerates the host NICs in `HardwareData`,
    so at `Publishing` the controller records the boot interface on the BMH — setting
    the boot-interface label `setBootMACAddressFromLabel()` reads (or
    `spec.bootMACAddress` directly) from the inspected NICs. A server whose boot
    interface cannot be determined **fails rather than publishing** a host that
    provisioning could not later boot. (**Open question:** on a multi-NIC host, which
    NIC is the provisioning/boot interface is not derivable from OOB inspection alone
    and may need a pool-level boot-interface selector — see
    [Open Questions](#12-open-questions--product-inputs).)
  - `spec.online`: **not set** (defaults to `false`) — fast inspection does not
    power the host on; the host stays powered off until the normal provisioning
    flow deploys it
  - Labels: `resources.clcm.openshift.io/resourcePoolName: <poolName>`

BMO registers the BMH against the BMC (authenticating with the default
credentials), runs fast out-of-band (Redfish) inspection, and populates the
`HardwareData` CR / `status.hardwareDetails`. If registration/authentication
fails, the server transitions to `AuthFailed` (see
[Metal3 BMC configuration](#5-metal3-bmc-configuration)). When inspection
completes, the controller compares the result in `Inventorying`.

**Inspection produces the authoritative inventory** — CPU, memory, storage, NICs,
firmware, and whatever else the BMC reports over Redfish — surfaced through the
`HardwareData` CR. This is the same data the O2 IMS resources collector reads, so
onboarding-time matching and the eventually-published inventory are derived from
one source.

> **Caveat — fast inspection may report fewer details.** Out-of-band Redfish
> inspection can return a smaller or less granular set of hardware facts than
> agent-based inspection, and coverage varies by vendor/BMC firmware (storage and
> NIC enumeration in particular are less consistent over Redfish than from an
> in-band agent). A pool's `hardwareAttributes` should therefore be limited to
> fields its BMCs actually populate under fast inspection — an unreported attribute
> reads as missing/zero and would fail the match. **TODO:** verify per-vendor
> (Dell/HPE/Lenovo) which `HardwareData` fields fast inspection reliably fills;
> vendor and model are reported by all supported BMCs.

### Credential swap and Publishing

After `Inventorying` confirms a match, the controller first **requests the BMC NIC
pin from DHCP to static** (`PinningBmcAddress`, reusing the same leased IP) via the
Metal3 networking API, then requests the BMC config change (per-server credentials
in local mode via the BMC credentials provisioning API, LDAP config in ldap mode
via the LDAP integration API) — BMO
applies each over Redfish and the BMH's referenced credential Secret
(`<onboardingRequestName>-<serverName>-desired-creds` local, or shared
`<onboardingRequestName>-ldap-creds` ldap) is reconciled so BMO keeps
authenticating after the change. The controller watches BMH conditions throughout
and never contacts the BMC itself.

The final step, `Publishing`, **removes the inventory-hold marker**
(`onboarding.openshift.io/inventory-hold`). Only then does the BMH become eligible
for the O2 IMS inventory (see below). Until that point the host has never appeared
in inventory, satisfying the requirement that a server is not surfaced to the
O-RAN/O2 IMS inventory before it is published. (Publishing happens at
`Publishing`, before `Completed`; for the `ldap` + `disableLocalAccounts` path the
host is therefore published and visible during the subsequent `DisablingLocalAccounts` state,
which runs against the already-published host.)

### Inventory-hold marker and `IncludeInInventory`

The inventory-hold marker requires a **small, focused change to the existing
inventory pipeline** — a single added guard — so a marked BMH is excluded even
when its provisioning state would otherwise qualify. Today `IncludeInInventory()`
(`internal/hardwaremanager/controller/inventory.go`) includes a BMH once its
`status.provisioning.state` is `available` (or `provisioning`/`provisioned`/
`externallyProvisioned`/`preparing`/`deprovisioning`). During onboarding, a
successfully-inspected BMH sits in `available` — which would surface it
prematurely. `IncludeInInventory()` must additionally return `false` when the BMH
carries the `onboarding.openshift.io/inventory-hold` annotation:

```go
func IncludeInInventory(bmh *metal3v1alpha1.BareMetalHost) bool {
    if !IsOCloudManaged(bmh) {
        return false
    }
    // Held by the onboarding service — not yet published to inventory.
    if bmh.Annotations[AnnotationInventoryHold] == "true" {
        return false
    }
    switch bmh.Status.Provisioning.State {
    // ... unchanged ...
    }
}
```

The `inspecting` state is already excluded (it is not in the included set), so the
host is naturally hidden while inspection runs; the marker extends that exclusion
through `available` until `Publishing` removes it.

**The allocation selection path must honor the same marker.** `IncludeInInventory()`
governs only whether a BMH is surfaced as an O2 IMS Resource — it does **not** gate
allocation. A held BMH sits in `available`, so the hardware manager's selection path
(`fetchBMHList` and `ResourceSelectionSecondaryFilter` in
`internal/hardwaremanager/controller/`) could otherwise pick it for a cluster while
onboarding is still mutating its BMC or firmware. The same
`onboarding.openshift.io/inventory-hold` guard must therefore be added to that
selection path so held hosts are excluded from allocation until `Publishing` removes
the marker. These two guards — inventory visibility (`IncludeInInventory`) and
allocation selection — are the only changes to existing inventory/allocation code
required by this design.

### O2 IMS inventory population

The onboarding service's responsibility ends at publishing a correctly-labelled,
no-longer-held BMH. It does **not** write to the O2 IMS Resource inventory itself —
that is handled by the **existing** O-Cloud manager inventory pipeline, and
requires no new code beyond the `IncludeInInventory()` marker check above.

Once the BMH is published (inventory-hold marker removed at `Publishing`), the
resources collector
(`internal/service/resources/collector/collector_hwmgr.go`) surfaces it as an O2
IMS Resource automatically, because the onboarding service already applies the
things that pipeline keys on:

- **The `resources.clcm.openshift.io/resourcePoolName` label** — `IsOCloudManaged()`
  treats a BMH as O-Cloud managed when this label is present, and the collector
  reads the label to place the Resource under the matching ResourcePool.
- **A qualifying provisioning state with no inventory-hold marker** —
  `IncludeInInventory()` includes a BMH once its `status.provisioning.state`
  reaches `available` (after inspection) **and** the inventory-hold marker has
  been removed. Both conditions are true only at/after `Publishing`.

Consequently, no onboarding-specific sync of `status.hardwareDetails` into the O2
IMS inventory is designed or required; that field is a convenience record on the
OnboardingServer CR, while the authoritative Resource inventory is derived from
the BMH's `HardwareData` by the existing collector. (The ResourcePool the Resource
is filed under is the same one the onboarding service ensures exists in
`oran-o2ims`; see [Resource provisioning on reconciliation](server-onboarding-service.md#1-api-design-crds).)

**Expected state of a freshly-published Resource.** A just-onboarded host is
published while its BMH sits in `available` (inspected out-of-band, powered off,
and not yet allocated to any cluster). Per the existing `IncludeInInventory()` /
`GetResourceInfo()` logic in
`internal/hardwaremanager/controller/inventory.go`, the collector therefore
reports it with `usageState: IDLE` and `operationalState: DISABLED` — `available`
maps to `IDLE`, and `DISABLED` is expected until the host is provisioned
(`operationalState` becomes `ENABLED` only in `provisioned`/`externallyProvisioned`
with the host powered on). This is normal for an unallocated pool member and does
**not** indicate an onboarding failure; the state changes once a
ProvisioningRequest allocates and provisions the host.

---

## 7. Controllers

The onboarding controller-manager runs two controllers (the REST API server runs alongside them — see [Deployment](#deployment) and [REST API](server-onboarding-service.md#rest-api)):

| Component | Trigger | Responsibility |
|-----------|---------|---------------|
| OnboardingRequest Controller | CR watch (reconcile loop) | Reconcile OnboardingRequest: create inventory hierarchy and OnboardingServer CRs from `spec.servers`, refresh status counts and the `status.servers` summary. Watches OnboardingServer CRs and re-reconciles the parent on any state change. |
| OnboardingServer Controller | CR watch; also watches its BMH | Reconcile OnboardingServer: drive the state machine — create ExternalSecrets and the BMH (default creds, `inspectionMode: fast`, hold marker), compare the inspection result to the profile, then **declare** the BMC network pin / credentials / LDAP config and **watch BMH conditions** for BMO to apply them, and remove the inventory-hold marker to publish |

The controllers follow the standard controller-runtime reconciler pattern used throughout the project (e.g., `ResourcePoolReconciler`, `NodeAllocationRequestReconciler`).

**Concurrency:** A single knob governs parallelism — the OnboardingServer
controller's `MaxConcurrentReconciles` (configurable). controller-runtime
defaults it to `1`, and the existing controllers in this repo run at `1`; this
controller would set it higher deliberately once a throughput target is
established (see [Open Questions](#12-open-questions--product-inputs)).

What this knob does — and does **not** — bound is worth being precise about:

- It caps the number of `Reconcile` **invocations** running at once. Each reconcile
  only reads/writes Kubernetes resources (the BMH, companion Metal3 config CRs,
  Secrets) and checks BMH status conditions — **the controller makes no BMC/Redfish
  calls** — so this does **not** bound BMC connections. Reconciles are cheap and
  short-lived: they return and requeue while BMO performs the slow BMC work.
- It does **not** cap the number of servers **in flight** overall. Both inspection
  (fast OOB) and every BMC-config operation (`PinningBmcAddress` /
  `UpdatingCredentials` / `ConfiguringLdap` / `DisablingLocalAccounts`) are
  performed asynchronously by BMO; the controller waits on them via watch + requeue,
  so a worker is held only for the brief Kubernetes read/write of a single
  reconcile, not for the duration of any BMC operation. The number of BMCs being
  inspected or reconfigured at once is bounded by **BMO/Ironic** (its out-of-band
  inspection throughput and BMC-session handling), **not** by
  `MaxConcurrentReconciles`. In practice a handful of workers shepherds far more
  than that many servers through onboarding at once.

This limit is **global to the onboarding controller, not per resource pool**. All
OnboardingServer CRs across every OnboardingRequest share the same reconcile
worker pool, so a very large pool can occupy all the slots and slow reconciles for
another pool — the controller does not reserve capacity or enforce fairness per
pool. This is acceptable for the expected scale (onboarding is an infrequent,
administrator-driven burst rather than steady-state load). Servers in a starved
pool are not failed — they simply wait in `Pending`/their current state until a
worker frees up.

**Sizing.** Because reconciles are Kubernetes-only and BMO paces the actual BMC
work, the `1` default is rarely the real bottleneck — BMO/Ironic's BMC throughput
is. A modest value (order of ten) keeps reconcile latency low for large sites, but
the ceiling on how fast servers onboard is set by BMO, not by controller CPU or
`MaxConcurrentReconciles`. If per-pool fairness or isolation is needed later,
options include a higher `MaxConcurrentReconciles` or partitioning work by pool;
both are out of scope for this design.

### Deployment

The onboarding service runs as a **separate Deployment** from the O-Cloud manager,
deployed during O-Cloud instantiation. It comprises two workloads built from the
same multi-service binary (see the Multi-Service Binary note in `AGENTS.md`): the
**controller-manager** container (the two reconcilers and the validating webhook)
and the **REST API server** container (the onboarding `serve` subcommand — see
[REST API](server-onboarding-service.md#rest-api)). The REST server is stateless (a facade over the CRDs) and
can be scaled horizontally; the controller-manager runs as a single active
instance with leader election, as controllers do elsewhere in the project. Both
may run in one Pod, or the REST server may be split into its own Deployment for
independent scaling — either way it is DB-free.

This separation provides:

- **Independent lifecycle** — upgrade/restart onboarding without affecting the O-Cloud manager
- **Failure isolation** — onboarding crash does not take down the O-Cloud manager
- **Optional deployment** — only deploy when onboarding is needed
- **Dedicated RBAC** — separate service account with only the permissions onboarding requires
- **Resource isolation** — onboarding reconciliation does not compete with the manager's CPU/memory

No database is required — all state is stored in CRDs, and the REST API server is a stateless facade over them (see [REST API](server-onboarding-service.md#rest-api)).

### Webhook serving and configuration

The validating webhook (creation validation, update immutability/append-only,
and the delete-time deletion guard — see [Validation](server-onboarding-service.md#1-api-design-crds) and
[Deletion semantics](server-onboarding-service.md#deletion-semantics)) is served by the same onboarding Pod,
using the controller-runtime webhook server. Serving it requires three pieces of
infrastructure that the deployment manifests must include:

- **`Service`** — a ClusterIP Service (e.g. `onboarding-webhook`) fronting the
  onboarding Pod's webhook port (default `9443`), so the API server can reach it.
- **TLS serving certificate** — provisioned via the OpenShift **service-ca**
  operator (part of core OpenShift, so no new prerequisite): the Service is
  annotated with `service.beta.openshift.io/serving-cert-secret-name`, and
  service-ca mints a Secret the webhook server mounts. cert-manager is a viable
  alternative on non-OpenShift hubs but would add a prerequisite.
- **`ValidatingWebhookConfiguration`** — registering the `create`, `update`, and
  `delete` operations on `onboardingrequests`. The CA bundle is injected
  automatically by service-ca via the
  `service.beta.openshift.io/inject-cabundle: "true"` annotation, so the config
  stays in sync with the serving cert without manual rotation.

**`failurePolicy: Fail`.** Admission is rejected when the webhook is unavailable,
so malformed or immutability-violating updates are never silently admitted while
the Pod is down. The operational cost — OnboardingRequest writes are blocked
during a webhook outage — is acceptable because onboarding writes are infrequent,
administrator-driven actions, not a hot path.

Critically, `Fail` is **not** what makes deletion safe: the OnboardingRequest
**finalizer** is the authoritative deletion guard. It re-checks for surviving
`onboarded-*` BMHs and blocks teardown regardless of whether the delete webhook
ran (closing the TOCTOU gap described in [Deletion semantics](server-onboarding-service.md#deletion-semantics)).
The delete webhook is a fast-feedback UX layer that rejects the `oc delete`
immediately with a clear message; even if it were bypassed or unavailable, the
finalizer still prevents a request from being removed while its BMHs exist. This
defense-in-depth split means webhook downtime degrades feedback, not data safety.

`sideEffects: None` (validation only, no external mutations) and a tight
`timeoutSeconds` (e.g. `10`) keep the webhook from stalling API-server requests.

### Observability

The onboarding service exposes its state through three channels — Prometheus
metrics, Kubernetes events, and structured logs — so operators can monitor
progress and diagnose failures without inspecting individual CRs.

**Metrics** (Prometheus, via the standard controller-runtime metrics endpoint):

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `onboarding_server_state` | Gauge | `state`, `resourcePool` | Number of OnboardingServers currently in each state — the primary "where is everything" view |
| `onboarding_server_transitions_total` | Counter | `from_state`, `to_state`, `resourcePool` | State-machine transition counts; terminal transitions (`AuthFailed`, `VendorMismatch`, `HwMismatch`, `Failed`) drive alerting |
| `onboarding_server_retries_total` | Counter | `state`, `resourcePool` | Transient-error retries, by the state that retried; a rising rate signals BMC/vault/ESO trouble |
| `onboarding_bmc_config_duration_seconds` | Histogram | `operation`, `result` | Wall-clock duration and error rate of each BMO-applied BMC-config operation (address pin, credential provisioning, LDAP config, local-account disable) and fast inspection, measured by the controller from request to the BMH condition reporting completion. The controller issues no Redfish itself, so this is the time BMO takes, observed via BMH conditions |
| `onboarding_request_servers` | Gauge | `resourcePool`, `category` | Mirrors the OnboardingRequest status counts (`total`/`pending`/`inProgress`/`completed`/`failed`) for dashboarding |

**Events** — emitted on the OnboardingServer (and, for request-level actions, the
OnboardingRequest), using the `events: create, patch` permission already in the
RBAC table:

- `Normal` on each state transition and on BMH creation (`BmhCreated`, with the BMH name).
- `Warning` on every terminal failure (`AuthFailed`, `VendorMismatch`, `HwMismatch`, `Failed`), carrying the same detail as `status.stateMessage` — **never** credential values (see [Security Considerations](server-onboarding-service.md#4-security-considerations)).
- `Warning` on best-effort cleanup that could not complete (`CleanupPartial`).

**Conditions and status** — the OnboardingRequest's `Ready` condition, summary
counts, and `status.servers` per-server list give the roll-up and the compact
per-server detail (name, state, message); this is exactly what the
REST API returns. Each OnboardingServer's `status` holds the full detail, including
`hardwareDetails`, surfaced by the `oc get` views in the
[Walkthrough](#10-walkthrough).

**Logging** — structured slog per the project logging conventions (see
`AGENTS.md`), with `resourcePool`, `onboardingServer`, `bmcAddress`, and `state` attributes
attached at reconcile entry via `logging.AppendCtx()` so every line for a server
is correlatable. The controller logs the BMC-config requests it declares and the
BMH conditions it observes at DEBUG; it never handles BMC credentials, so none can
appear in its logs.

---

## 8. Project Structure (new files)

### CRD types

```text
api/onboarding/v1alpha1/
├── onboardingrequest_types.go      # OnboardingRequest CRD
├── onboardingserver_types.go       # OnboardingServer CRD
├── groupversion_info.go            # API group registration
└── zz_generated.deepcopy.go        # Generated
```

### Controller

```text
internal/controllers/
├── onboardingrequest_controller.go     # Reconcile OnboardingRequest: hierarchy, server CRs, status
└── onboardingserver_controller.go      # Reconcile OnboardingServer: state machine, BMH + Metal3 BMC-config, BMH creation
```

### Supporting packages

```text
internal/onboarding/
└── bmh/
    ├── creator.go                      # BMH CR + Secret creation (default creds, inspectionMode: fast,
    │                                   #   inventory-hold marker) and marker removal at Publishing
    ├── bmcconfig.go                    # Declare desired BMC net/creds/LDAP config on the BMH /
    │                                   #   companion Metal3 CRs and
    │                                   #   interpret the BMH status conditions BMO reports
    └── inventory.go                    # Inspection HardwareData → canonical attributes; vendor/HW match
```

There is **no `redfish/` package and no BMC/Redfish client dependency** — the
onboarding controller opens no BMC connections. Hardware inventory and vendor validation come
from BMO's fast out-of-band inspection (`HardwareData`), and all BMC mutation is
declared on the BMH / companion Metal3 CRs and applied by BMO (see
[Metal3 BMC configuration](#5-metal3-bmc-configuration)). The exact companion-CR
shape in `bmcconfig.go` depends on the final BMC-config APIs.

### Webhook

```text
internal/webhooks/
└── onboardingrequest_webhook.go        # Validation: server list, immutability, deletion guard
```

Serving the webhook also requires manifests (see
[Webhook serving and configuration](#webhook-serving-and-configuration)):

```text
config/webhook/
├── service.yaml                        # ClusterIP Service fronting the webhook port,
│                                       #   annotated for service-ca serving cert
├── manifests.yaml                      # ValidatingWebhookConfiguration (create/update/delete
│                                       #   on onboardingrequests), inject-cabundle annotation,
│                                       #   failurePolicy: Fail, sideEffects: None
└── kustomization.yaml
```

### REST API server

Following the service layout convention used by the other O2 IMS servers (see
[REST API](server-onboarding-service.md#rest-api)):

```text
internal/service/onboarding/
├── api/
│   ├── openapi.yaml                    # O-RAN OpenAPI spec (/ocloud-infrastructureOnboarding/v1)
│   ├── openapi-overlay.yaml            # Overlay for any internal-only endpoints (optional)
│   ├── generated/                      # oapi-codegen output (strict-server, embedded-spec)
│   │   └── onboarding.generated.go
│   ├── tools/
│   │   ├── generate.go                 # //go:generate oapi-codegen directive
│   │   └── oapi-codegen.yaml           # codegen config (import-maps common ProblemDetails)
│   ├── config.go                       # OnboardingServerConfig (LoadFromEnv/Validate)
│   ├── server.go                       # OnboardingServer{HubClient}: StrictServerInterface impl
│   ├── converters.go                   # CR <-> generated API model converters
│   └── server_test.go
├── cmd/
│   ├── root.go                         # cobra root for the onboarding server subcommands
│   └── serve.go                        # `serve` subcommand
└── serve.go                            # Serve(): hub client, auth middleware, HTTP(S) server
```

There is no `db/` subtree (no models, repository, or migrations) — the server
reads and writes OnboardingRequest / OnboardingServer CRs through `HubClient`.

### Modified existing files

- `main.go` — register controllers and webhook
- Root command wiring — add the onboarding server subcommand to the multi-service
  binary (alongside `resource-server`, `provisioning-server`, etc.)
- `internal/service/common/clients/k8s/k8s.go` — register the
  `onboarding/v1alpha1` scheme in `GetSchemeForHub` so the REST server and
  controllers can (de)serialize the CRs. The onboarding controller-manager
  scheme must also include `clcm.openshift.io/v1alpha1` (for `HardwareProfile`)
  and the Metal3 types; `metal3v1alpha1.AddToScheme` registers the BMH plus
  `HostFirmwareSettings` / `HostFirmwareComponents` / `HostUpdatePolicy` in one
  call, so the servicing CRs come with the BMH scheme already needed for
  inspection
- `internal/constants/constants.go` — add onboarding constants (including the
  `onboarding.openshift.io/inventory-hold` and
  `onboarding.openshift.io/hardening-complete` annotation keys and the server
  subcommand name)
- `internal/hardwaremanager/controller/inventory.go` — `IncludeInInventory()`
  must exclude BMHs carrying the inventory-hold marker (see
  [Inventory-hold marker and `IncludeInInventory`](#inventory-hold-marker-and-includeininventory))
- `internal/hardwaremanager/controller/` allocation selection (`fetchBMHList` /
  `ResourceSelectionSecondaryFilter`) — must likewise exclude held BMHs so a
  mid-onboarding host is never allocated before `Publishing` (same
  [inventory-hold](#inventory-hold-marker-and-includeininventory) guard)
- `config/crd/` — generated CRD manifests
- `config/rbac/` — RBAC for the onboarding service account, plus the user-facing
  role updates required by DD-004:
  - `clusterrole_onboarding_server.yaml` (new) — the REST server
    ServiceAccount's Kubernetes permissions, scoped to exactly what the facade
    does: `get,list,create,delete` on `onboardingrequests` (the API exposes
    `GET`/`POST`/`DELETE`). It needs **no** access to `onboardingservers` — the
    facade serves every response from the OnboardingRequest CR alone and never
    reads the OnboardingServer CRs. This is much narrower than the
    controller-manager's ClusterRole (see the RBAC table in
    [Security Considerations](server-onboarding-service.md#4-security-considerations)), which additionally
    holds full `onboardingservers` access and `watch`/`update`/`patch` to drive
    reconciliation.
  - `clusterrolebindings_services.yaml` — add an
    `onboarding-server-subject-access-reviewer-binding` that binds the onboarding
    server ServiceAccount to the existing shared
    `clusterrole_subject_access_reviewer.yaml` (the auth middleware's
    `SubjectAccessReview`/`TokenReview` permissions), exactly as every other
    service's binding does. The `create` verbs on `subjectaccessreviews`/
    `tokenreviews` are **not** re-declared per service — they come from that one
    shared ClusterRole.
  - `oran_o2ims_user_roles.yaml` — add the onboarding `nonResourceURLs` (the
    `onboardingRequests` API paths — the only public endpoints) to the reader and
    privileged roles
  - `oran_o2ims_oauth_role_bindings.yaml` — bind the onboarding OAuth group(s) to
    those roles
- `must-gather/gather` — add `gather_resource` calls for OnboardingRequest and OnboardingServer CRDs
- Deployment manifests — add separate onboarding Deployment (controller-manager +
  REST API server containers; the server needs the OAuth issuer config and a
  serving TLS certificate, as the other servers do)

---

## 9. Prerequisites

The following must be in place before the onboarding service can be used. All are outside the scope of this design.

| Prerequisite | Purpose | Configured by |
|---|---|---|
| **Location CR** | Parent of the inventory hierarchy. Must exist in the O-Cloud manager namespace (`oran-o2ims` by default) before creating an OnboardingRequest; its `metadata.name` must equal the request's `globalLocationName`. | Platform admin via `oc apply` |
| **External Secrets Operator (ESO)** | Syncs vault secrets into K8s Secrets. Must be deployed on the hub cluster. | Platform admin / cluster setup |
| **ClusterSecretStore CR** | Named `onboarding-vault` by default (configurable via `--secret-store-name` on the controller). Must be configured to connect to the external vault. All ExternalSecrets created by the onboarding controller reference this store. | Platform admin / security team |
| **External vault** | Stores BMC credentials. Must contain: factory-default credentials (shared per vendor), per-server desired credentials (one vault entry per server, local mode), and LDAP bind/user credentials (LDAP mode). | Security team |
| **BMC DHCP address** | Each BMC has a DHCP IP reachable from the hub at `bmcAddress`, backed by a **durable per-MAC reservation or IPAM exclusion** that holds for the lifetime of the static assignment (after pinning the BMC stops renewing, so a non-durable lease could be reassigned and collide). The service later pins it to static, reusing the same IP; set `bmcSubnet`/`bmcGateway` so pinning does not need the lease. | Server / network team |
| **IP routing to BMC subnets** | **BMO/Ironic** must have outbound IP connectivity to the remote BMC management subnets for Redfish HTTPS — it performs all BMC traffic (inspection and net/creds/LDAP config). The **onboarding Pod needs no such route** (it talks only to the Kubernetes API). This requires routing from the Metal3/BMO pods to the BMC networks. | Network team |
| **Metal3/Ironic + forthcoming BMC-config APIs** | BMO/Ironic must be deployed on the hub and support: fast out-of-band (Redfish) inspection; the BMC networking config API; the BMC credentials provisioning API; and, for LDAP pools, the LDAP integration API. The three BMC-config APIs are not yet available — each onboarding state is blocked until its API lands (local-cred onboarding needs fast inspection + BMC networking + BMC credentials; LDAP additionally needs LDAP integration). **No provisioning network, DHCP, TFTP, or PXE is required to onboard** — inspection is out-of-band. (A provisioning network is still needed later, by the normal provisioning flow, when a published host is actually deployed — but that is outside onboarding.) The onboarding controller needs **no direct BMC/Redfish client**. | Platform admin / Metal3 admin |
| **LDAP server** (authMode=ldap only) | Directory server reachable from the BMC management networks. Must contain user accounts and groups for BMC admin access. BMCs must be able to reach the LDAP server for authentication after configuration. | Identity / directory team |
| **HardwareProfile CR** (`hardwareProfileName` only) | Existing `HardwareProfile` (`clcm.openshift.io/v1alpha1`) in the O-Cloud manager namespace, describing the pool's desired BIOS attributes and firmware versions. Firmware image URLs must be reachable by BMO. | Platform admin / hardware team |
| **OAuth/OIDC issuer** (REST API only) | The same OAuth server the other O2 IMS services use. Required only to expose the [REST API](server-onboarding-service.md#rest-api); the CRD interface via `oc` needs only normal Kubernetes RBAC. The issuer must map onboarding scopes to groups (DD-003), and a serving TLS certificate must be provisioned for the server (via service-ca, as elsewhere). For mTLS (TELCOSTRAT-290), the issuer must emit RFC 8705 certificate-bound tokens and the Ingress route must be configured with a client-CA bundle so HAProxy verifies client certificates — same setup as the other services. | Platform admin / security team |

---

## 10. Walkthrough

This section walks through the end-to-end onboarding process using a concrete example: onboarding three Dell R750 servers at a site called `toronto-east` into a resource pool called `pool-toronto-east-1`, using local credential management.

### Step 0: Prerequisites

Before creating the OnboardingRequest, the platform admin ensures:

1. The Location CR exists in the O-Cloud manager namespace (`oran-o2ims`).
   `Location` is a namespaced CRD. The Location's identifier is its
   `metadata.name` (the `globalLocationId` returned by the O2 API is derived from
   it — there is no `globalLocationId` spec field). `LocationSpec` requires
   `description` plus at least one of `coordinate`, `civicAddress`, or `address`:

   ```yaml
   apiVersion: ocloud.openshift.io/v1alpha1
   kind: Location
   metadata:
     name: toronto-east
     namespace: oran-o2ims
   spec:
     description: Toronto East data center
     coordinate:
       latitude: "43.6532"
       longitude: "-79.3832"
   ```

   The OnboardingRequest's `globalLocationName` must equal this Location's
   `metadata.name` (`toronto-east`).

2. ESO is deployed with a ClusterSecretStore named `onboarding-vault` pointing to the external vault.

3. The vault contains the following secrets:

   | Vault path | Contents | Purpose |
   |---|---|---|
   | `bmc/dell-r750-defaults` | `{"username": "root", "password": "calvin"}` | Factory-default credentials (shared) |
   | `bmc/pool1-server-101` | `{"username": "admin", "password": "...unique..."}` | Desired credentials for server at 10.10.10.101 |
   | `bmc/pool1-server-102` | `{"username": "admin", "password": "...unique..."}` | Desired credentials for server at 10.10.10.102 |
   | `bmc/pool1-server-103` | `{"username": "admin", "password": "...unique..."}` | Desired credentials for server at 10.10.10.103 |

4. The three BMCs are powered on and reachable at their DHCP-assigned addresses
   (10.10.10.101–103), still on factory-default credentials. A DHCP server serves
   the BMC management subnet, with a reservation per BMC MAC so each keeps the
   address recorded in `spec.bmcAddress` until the controller pins it to static
   during `PinningBmcAddress` (see [Prerequisites](#9-prerequisites) and the
   [`PinningBmcAddress` state](#server-onboarding-state-machine--detailed-per-state-behavior)).
   **BMO/Ironic** has routed connectivity to the 10.10.10.0/24 subnet for Redfish;
   the onboarding Pod does not need it (it talks only to the Kubernetes API).

5. Metal3/Ironic is deployed on the hub with fast out-of-band (Redfish) inspection
   and the forthcoming BMC-config APIs (networking and credentials)
   available. No provisioning network, DHCP-for-PXE, or TFTP is required to onboard
   — inspection is out-of-band.

### Step 1: Create the OnboardingRequest

The platform admin creates the OnboardingRequest CR (this walkthrough uses `oc`;
the same request can be submitted with `POST /ocloud-infrastructureOnboarding/v1/onboardingRequests`
— see [REST API](server-onboarding-service.md#rest-api)):

```yaml
apiVersion: onboarding.openshift.io/v1alpha1
kind: OnboardingRequest
metadata:
  name: toronto-east-pool1
spec:
  globalLocationName: toronto-east
  oCloudSiteName: site-toronto-east
  resourcePoolName: pool-toronto-east-1

  defaultCredentialsVaultSecret: bmc/dell-r750-defaults
  authMode: local
  bmcVendor: Dell

  # BMC management network (optional, pool-level). Authoritative for the
  # DHCP-to-static pin and used to validate every bmcAddress is in-subnet.
  bmcSubnet: "10.10.10.0/24"
  bmcGateway: "10.10.10.1"

  hardwareAttributes:
    - name: model
      value: "PowerEdge R750"
    - name: memoryGiB
      operator: GreaterThanOrEqual
      value: "512"

  # No provisioning-network fields: hardware validation uses fast out-of-band
  # (Redfish) inspection (spec.inspectionMode: fast on the BMH), which queries the
  # BMC directly and never boots the host over a provisioning network.

  servers:
    - bmcAddress: "10.10.10.101"
      desiredCredentialsVaultSecret: bmc/pool1-server-101
    - bmcAddress: "10.10.10.102"
      desiredCredentialsVaultSecret: bmc/pool1-server-102
    - bmcAddress: "10.10.10.103"
      desiredCredentialsVaultSecret: bmc/pool1-server-103
```

```bash
oc apply -f onboarding-request.yaml
```

### Step 2: OnboardingRequest controller reconciles

On the first reconciliation, the OnboardingRequest controller performs the following (in order):

1. **Looks up Location CR** `toronto-east` — found, continues. (If missing, sets `Ready=False, Reason=LocationNotFound` and stops.)

2. **Creates OCloudSite CR** `site-toronto-east` (if it does not already exist):

   ```yaml
   apiVersion: ocloud.openshift.io/v1alpha1
   kind: OCloudSite
   metadata:
     name: site-toronto-east
     namespace: oran-o2ims
   spec:
     globalLocationName: toronto-east
     description: Created by onboarding request toronto-east-pool1
   ```

3. **Creates ResourcePool CR** `pool-toronto-east-1` (if it does not already exist):

   ```yaml
   apiVersion: ocloud.openshift.io/v1alpha1
   kind: ResourcePool
   metadata:
     name: pool-toronto-east-1
     namespace: oran-o2ims
   spec:
     oCloudSiteName: site-toronto-east
     description: Created by onboarding request toronto-east-pool1
   ```

4. **Creates namespace** `pool-toronto-east-1` (if it does not already exist).

5. **Creates three OnboardingServer CRs** in namespace `pool-toronto-east-1`, one per server entry:

   ```yaml
   apiVersion: onboarding.openshift.io/v1alpha1
   kind: OnboardingServer
   metadata:
     name: 10-10-10-101
     namespace: pool-toronto-east-1
     ownerReferences:
       - apiVersion: onboarding.openshift.io/v1alpha1
         kind: OnboardingRequest
         name: toronto-east-pool1
   spec:
     onboardingRequestName: toronto-east-pool1
     bmcAddress: "10.10.10.101"
     desiredCredentialsVaultSecret: bmc/pool1-server-101
   ```

   (Similarly for `10-10-10-102` and `10-10-10-103`.) `status` is a subresource,
   so it is not set at create time; the OnboardingServer controller sets
   `status.state: Pending` on its first reconcile.

6. **Updates OnboardingRequest status**:

   ```yaml
   status:
     conditions:
       - type: Ready
         status: "True"
     serversTotal: 3
     serversPending: 3
     serversInProgress: 0
     serversCompleted: 0
     serversFailed: 0
     servers:
       - name: 10-10-10-101
         state: Pending
       - name: 10-10-10-102
         state: Pending
       - name: 10-10-10-103
         state: Pending
   ```

### Step 3: OnboardingServer controller processes each server

The OnboardingServer controller picks up each CR and drives it through the state
machine. Reconciles run up to `MaxConcurrentReconciles` at a time (this walkthrough
assumes it is set above the default `1`), and because inspection waits do not hold
a worker, all three servers here progress in parallel. Below is the progression
for one server (`10.10.10.101`);
the others follow the same flow in parallel.

#### 3a. Pending → CreatingExternalSecrets

The first action is to provision credentials; the vendor is validated later,
during `Inventorying`, from the inspection result (see 3c). The controller
creates ExternalSecret CRs in the `pool-toronto-east-1` namespace:

- **Shared** (created once, idempotent — already exists if another server reached this state first):

  ```yaml
  apiVersion: external-secrets.io/v1
  kind: ExternalSecret
  metadata:
    name: toronto-east-pool1-default-creds
    namespace: pool-toronto-east-1
  spec:
    refreshInterval: 1h
    secretStoreRef:
      kind: ClusterSecretStore
      name: onboarding-vault
    target:
      name: toronto-east-pool1-default-creds
    data:
      - secretKey: username
        remoteRef:
          key: bmc/dell-r750-defaults
          property: username
      - secretKey: password
        remoteRef:
          key: bmc/dell-r750-defaults
          property: password
  ```

- **Per-server** (unique to this server):

  ```yaml
  apiVersion: external-secrets.io/v1
  kind: ExternalSecret
  metadata:
    name: toronto-east-pool1-10-10-10-101-desired-creds
    namespace: pool-toronto-east-1
  spec:
    refreshInterval: 1h
    secretStoreRef:
      kind: ClusterSecretStore
      name: onboarding-vault
    target:
      name: toronto-east-pool1-10-10-10-101-desired-creds
    data:
      - secretKey: username
        remoteRef:
          key: bmc/pool1-server-101
          property: username
      - secretKey: password
        remoteRef:
          key: bmc/pool1-server-101
          property: password
  ```

ESO syncs these into K8s Secrets. The controller watches the ExternalSecrets' `.status` until ESO reports at least the `toronto-east-pool1-default-creds` ExternalSecret synced (`Ready`) — it does not read the synced Secret's data; the BMH created next references the resulting Secret and BMO reads it — then transitions to `Inspecting`.

#### 3b. CreatingExternalSecrets → Inspecting

The controller creates the BareMetalHost with the **shared default credentials**,
fast out-of-band inspection (`spec.inspectionMode: fast`), and the **inventory-hold
marker** (so it never appears in the O2 IMS inventory during onboarding):

```yaml
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: onboarded-10-10-10-101      # derived from bmcAddress (serial not yet known)
  namespace: pool-toronto-east-1
  labels:
    resources.clcm.openshift.io/resourcePoolName: pool-toronto-east-1
  annotations:
    # held out of O2 IMS inventory until the host is published (removed at Publishing)
    onboarding.openshift.io/inventory-hold: "true"
spec:
  bmc:
    address: idrac-virtualmedia+https://10.10.10.101/redfish/v1/Systems/System.Embedded.1
    credentialsName: toronto-east-pool1-default-creds   # shared default creds (swapped later)
    disableCertificateVerification: false   # BMO verifies the BMC cert; true only if the pool opts in (self-signed BMCs)
  inspectionMode: fast                # fast out-of-band (Redfish) inspection — no ramdisk boot
  # online defaults to false — fast inspection does not power the host on
  # no bootMACAddress yet — set (or the boot-interface label set) from inspected NICs at Publishing
```

BMO registers the host against the BMC using the default credentials and runs a
fast out-of-band (Redfish) inspection, producing a `HardwareData` CR in seconds.

- **Success**: `HardwareData` becomes available and the BMH reaches `available`
  (still held). Transition to `Inventorying`.
- **Registration/authentication failure** (wrong default credentials, BMC
  unreachable): BMO reports an error status. Retry with backoff up to 5 times; if
  exhausted, transition to `AuthFailed` (terminal) and delete the held BMH.

#### 3c. Inspecting → Inventorying

The controller reads the inspection result (`HardwareData` / BMH
`status.hardwareDetails`), maps it into the canonical attribute catalog (e.g.
`RAMMebibytes` of `524288` → `memoryGiB: 512`), records it in `status.hardwareDetails`,
and validates **both vendor and hardware** against the OnboardingRequest:

- **Vendor match** is checked first: `HardwareData` `SystemVendor.Manufacturer`
  (e.g. `Dell Inc.`) must match `spec.bmcVendor` (`Dell`) under the
  case-insensitive substring rule — `dell` is a substring of `dell inc.`, so this
  passes (see [Vendor matching](server-onboarding-service.md#1-api-design-crds)). If it does not, transition to
  `VendorMismatch` (terminal), delete the held BMH, and count the server in
  `serversFailed`.
- **Hardware attributes** are checked next against `hardwareAttributes`, each using
  its `operator`:
  - **Match**: `model` == `PowerEdge R750`, and `memoryGiB` (512) `>=` 512 — all
    pass. Transition to `PinningBmcAddress`.
  - **Mismatch**: `model` is `PowerEdge R650`, or `memoryGiB` is 256 (fails
    `>= 512`) — transition to `HwMismatch` (terminal) and delete the held BMH.
    `status.stateMessage` names the failing attribute, operator, expected, and
    inspected value.

The OnboardingServer status is updated:

```yaml
status:
  state: Inventorying
  vendor: Dell
  model: PowerEdge R750
  serialNumber: ABC1234              # now known from inspection
  bmhName: onboarded-10-10-10-101
  hardwareDetails:                   # canonical attributes from inspection
    model: PowerEdge R750
    cpuArch: x86_64
    cpuModel: Intel Xeon Gold 6338
    cpuCount: "128"
    memoryGiB: "512"
    nicCount: "4"
    diskCount: "8"
```

#### 3d. Inventorying → PinningBmcAddress

This example omits `spec.hardwareProfileName`, so the `ApplyingHardwareProfile`
state is skipped and `Inventorying` transitions straight to `PinningBmcAddress`.
Had a profile been set, the controller would first render it into
`HostFirmwareSettings` / `HostFirmwareComponents` / `HostUpdatePolicy` CRs beside
the held BMH and wait for BMO to service the host to the desired firmware/BIOS
baseline (see the [`ApplyingHardwareProfile` state](#server-onboarding-state-machine--detailed-per-state-behavior))
before continuing here.

The BMC currently holds a DHCP-assigned address (`10.10.10.101`, the same value
recorded in `spec.bmcAddress`). Now that the server is validated, the controller
**requests** the DHCP→static conversion (reusing that same address) via the Metal3
BMC networking API —
it does not contact the BMC itself. Here the request set `bmcSubnet: 10.10.10.0/24`
and `bmcGateway: 10.10.10.1`, so those are supplied as the static subnet/gateway (if
omitted, BMO derives them from the BMC's DHCP lease). Illustrative desired state
declared on the BMH / companion CR (final shape per the BMC networking API):

```yaml
# illustrative — subject to the final Metal3 BMC networking API
bmcNetwork:
  mode: static
  address: 10.10.10.101          # same as the DHCP lease
  subnet: 10.10.10.0/24
  gateway: 10.10.10.1
```

BMO applies this over Redfish (disabling DHCP, pinning the static address) and
reports the outcome on the BMH. The controller **watches the BMH conditions** and,
once BMO confirms the BMC is still reachable at `10.10.10.101`, transitions to
`UpdatingCredentials` (local) or `ConfiguringLdap` (ldap). Because the address is
unchanged, no update to `spec.bmcAddress` is needed. This step is reversible
(request a revert to DHCP) and runs before any credential change, so it is **not**
the point of no return. If BMO reports failure after retries, the server
transitions to `Failed`; the BMC still holds the pool default credentials, so the
server can be retried cleanly (see [Retrying a failed server](server-onboarding-service.md#retrying-a-failed-server)).

#### 3e. PinningBmcAddress → UpdatingCredentials

**This is the point of no return for local mode.** The controller **requests** the
credential change via the Metal3 BMC credentials provisioning API, supplying the
per-server desired credentials (the ESO-synced
`toronto-east-pool1-10-10-10-101-desired-creds` Secret). Illustrative desired state
(final shape per the BMC credentials provisioning API):

```yaml
# illustrative — subject to the final Metal3 BMC credentials API
bmcCredentials:
  desiredSecretRef: toronto-east-pool1-10-10-10-101-desired-creds
```

BMO applies the new credentials to the BMC over Redfish, verifies it can
authenticate with them, and reconciles the BMH's referenced credential Secret so
BMO keeps authenticating. The controller **watches the BMH conditions** for success,
then transitions to `Publishing`. The controller never contacts the BMC and never
handles the credential values itself.

If BMO reports failure after retries, the server transitions to `Failed`. Since the
credential change may have partially applied, `status.stateMessage` notes that the
BMC may be in an inconsistent credential state.

#### 3f. UpdatingCredentials → Publishing

The BMH already exists and has been inspected and matched; its credential
reference now points at the per-server Secret. The controller **removes the
inventory-hold marker** so the host becomes eligible for the O2 IMS inventory:

```bash
oc annotate bmh -n pool-toronto-east-1 onboarded-10-10-10-101 \
  onboarding.openshift.io/inventory-hold-
```

With the marker gone and the BMH in `available`, the existing inventory pipeline
(`IncludeInInventory()`) now includes it, and the resources collector surfaces it
as an O2 IMS Resource under `pool-toronto-east-1`. Transition to `Completed`.

#### 3g. Publishing → Completed

The OnboardingServer status is updated:

```yaml
status:
  state: Completed
  bmhName: onboarded-10-10-10-101
```

The OnboardingRequest controller re-reconciles (triggered by the OnboardingServer state change) and updates the summary counts and the per-server list:

```yaml
status:
  serversTotal: 3
  serversPending: 0
  serversCompleted: 1
  serversInProgress: 2
  serversFailed: 0
  servers:
    - name: 10-10-10-101
      state: Completed
    - name: 10-10-10-102
      state: UpdatingCredentials
    - name: 10-10-10-103
      state: Inspecting
```

### Step 4: Monitor progress

The platform admin monitors progress using standard `oc` commands:

```bash
# Summary view
$ oc get onboardingrequest toronto-east-pool1
NAME                 TOTAL   PENDING   COMPLETED   IN-PROGRESS   FAILED   AGE
toronto-east-pool1   3       0         1           2             0        5m

# Per-server detail
$ oc get onboardingservers -n pool-toronto-east-1
NAME            STATE                  VENDOR   BMC ADDRESS     BMH                       AGE
10-10-10-101    Completed              Dell     10.10.10.101    onboarded-10-10-10-101    5m
10-10-10-102    UpdatingCredentials    Dell     10.10.10.102    onboarded-10-10-10-102    5m
10-10-10-103    Inspecting             -        10.10.10.103    onboarded-10-10-10-103    4m
# VENDOR is empty for 10-10-10-103 because status.vendor is only populated at Inventorying.

# Inspect a specific server
$ oc get onboardingserver 10-10-10-103 -n pool-toronto-east-1 -o yaml
# ... full status with hardwareDetails, retryCount, etc.

# View the BMHs. All exist from the Inspecting state onward, but only the
# published one (inventory-hold marker removed) is surfaced to O2 IMS inventory.
$ oc get bmh -n pool-toronto-east-1
NAME                     STATE        ONLINE   AGE
onboarded-10-10-10-101   available    false    3m    # published (marker removed)
onboarded-10-10-10-102   available    false    3m    # still held
onboarded-10-10-10-103   inspecting   false    2m    # still held, out-of-band inspection
```

### Step 5: Add more servers later

When new servers are racked at the same site, the platform admin appends them to the existing OnboardingRequest:

```bash
oc edit onboardingrequest toronto-east-pool1
```

Add to `spec.servers`:

```yaml
    - bmcAddress: "10.10.10.104"
      desiredCredentialsVaultSecret: bmc/pool1-server-104
    - bmcAddress: "10.10.10.105"
      desiredCredentialsVaultSecret: bmc/pool1-server-105
```

The webhook validates that existing entries are unchanged and new entries are valid. The OnboardingRequest controller creates two new OnboardingServer CRs on the next reconciliation, and the state machine processes them independently. `serversTotal` updates to 5.

### Step 6: Handle failures

**VendorMismatch example** — server 10.10.10.103 is an HPE server racked in a Dell pool by mistake:

```bash
$ oc get onboardingserver 10-10-10-103 -n pool-toronto-east-1
NAME            STATE            VENDOR   BMC ADDRESS     BMH   AGE
10-10-10-103    VendorMismatch   HPE      10.10.10.103          2m

$ oc get onboardingserver 10-10-10-103 -n pool-toronto-east-1 -o jsonpath='{.status.stateMessage}'
vendor "HPE" does not match expected "Dell"
```

The server stays in `VendorMismatch` until retried. Rack the correct hardware
model, then retry this one server by deleting its OnboardingServer CR — the
OnboardingRequest controller recreates it as `Pending` and re-runs the state
machine (see [Retrying a failed server](server-onboarding-service.md#retrying-a-failed-server)):

```bash
oc delete onboardingserver 10-10-10-103 -n pool-toronto-east-1
```

There is no need to delete the OnboardingRequest — and once other servers have
published BMHs (completed onboarding, inventory-hold marker removed), the deletion
webhook would reject that anyway. Held BMHs still mid-onboarding do not block it.

On `VendorMismatch` the held BMH is deleted, so the BMH column is empty. Note that
the vendor check runs during `Inventorying` on the **inspection result**, but
reaching inspection first requires BMO to register the BMH with the pool's shared
default credentials during `Inspecting`. A foreign-vendor server that ships with
different factory defaults will therefore usually fail earlier at `AuthFailed` —
its BMC does not accept the pool's default credentials — rather than reaching
`VendorMismatch`. `VendorMismatch` is reached only when the BMC accepts those
credentials and inspects successfully but reports a mismatched `Manufacturer`.

**AuthFailed example** — server 10.10.10.102 has non-default credentials from a previous deployment:

```bash
$ oc get onboardingserver 10-10-10-102 -n pool-toronto-east-1
NAME            STATE        VENDOR   BMC ADDRESS     BMH   AGE
10-10-10-102    AuthFailed            10.10.10.102          3m

$ oc get onboardingserver 10-10-10-102 -n pool-toronto-east-1 -o jsonpath='{.status.stateMessage}'
BMO could not register BMH with default credentials after 5 attempts (BMC rejected credentials)
```

`AuthFailed` is a pre-mutation state, so retry is per server: fix the root cause —
provide the correct pool default credentials in the vault, or manually reset the
BMC credentials on the physical server (e.g. via the iDRAC front panel or an
out-of-band reset) so it accepts the pool defaults — then delete this server's
OnboardingServer CR to have it recreated as `Pending`
(see [Retrying a failed server](server-onboarding-service.md#retrying-a-failed-server)):

```bash
oc delete onboardingserver 10-10-10-102 -n pool-toronto-east-1
```

### LDAP mode variant

For LDAP mode, the flow is the same through `PinningBmcAddress` (the BMH is created
in `Inspecting` with the shared default credentials and inspected, and the BMC NIC
is pinned from DHCP to static reusing the same IP). After that:

- Instead of `UpdatingCredentials`, the server enters `ConfiguringLdap`. The
  controller **declares** the LDAP settings from the OnboardingRequest's `ldapConfig`
  via the Metal3 LDAP integration API; BMO applies
  them to the BMC's Redfish Account Service (the optional
  `usernameAttribute`/`groupsAttribute` map to the Redfish `LDAPService.SearchSettings`
  fields and are included only when set — the BMC applies its vendor default
  otherwise; `remoteRoleMapping` maps to `RemoteRoleMapping`). The local account is
  left **enabled** through this state so the step stays reversible. Illustrative
  desired state (final shape per the LDAP integration API; the example sets both attributes):

  ```yaml
  # illustrative — subject to the final Metal3 LDAP integration API
  bmcLdap:
    serviceEnabled: true
    serviceAddresses: ["ldaps://ldap.example.com:636"]
    bindSecretRef: toronto-east-pool1-ldap-bind
    searchSettings:
      baseDistinguishedNames: ["ou=users,dc=example,dc=com"]
      usernameAttribute: uid
      groupsAttribute: memberOf
    remoteRoleMapping:
      - remoteGroup: bmc-admins
        localRole: Administrator
  ```

  The controller watches the BMH conditions for BMO to report LDAP configured and
  **verified** — authenticated access with the required role, not just a TLS
  connection to the directory (see the `ConfiguringLdap` state) — before advancing to
  `Publishing`; the LDAP integration API's ESO credential sync keeps the BMC admin
  Secret current with the vault.

- No per-server desired credentials Secret is created. Once BMO reports LDAP working, the BMH's referenced credential Secret is the shared `toronto-east-pool1-ldap-creds` Secret, so BMO authenticates to the BMC via the LDAP user credentials.
- All BMHs in the pool share the same LDAP user credentials — the LDAP server handles authentication, not per-BMC local accounts.
- The server then enters `Publishing` (inventory-hold marker removed), as in local mode.
- If `ldapConfig.disableLocalAccounts` is true, the server enters a `DisablingLocalAccounts`
  state after `Publishing` (before `Completed`) that disables the local account as
  a final, irreversible step. `DisablingLocalAccounts` is best-effort — a failure leaves the
  already-published host working over LDAP, is retried, and never fails the server;
  on exhausted retries it proceeds to `Completed` anyway with the incomplete
  hardening recorded in `status.stateMessage` / the `LocalAccountsDisabled`
  condition. With `disableLocalAccounts` false (default), `DisablingLocalAccounts` is skipped
  and the local account remains as a recovery path.
- Either way the server ends at `Completed`, as in local mode.

---

## 11. Verification

The scenarios below are representative, not an exhaustive per-state enumeration;
each state in [section 2](server-onboarding-service.md#2-server-onboarding-state-machine) — including
`CreatingExternalSecrets`, `PinningBmcAddress`, `UpdatingCredentials`,
`ConfiguringLdap`, `DisablingLocalAccounts`,
`AuthFailed`, and `Failed` — is expected to have unit/envtest coverage.

1. **Unit tests**: State machine transitions; HW attribute matching against
   inspection `HardwareData` (canonical-attribute mapping, integer/string
   comparison, `operator` semantics); BMC address template rendering; server and
   BMH name derivation from `bmcAddress`; `IncludeInInventory()` returning `false`
   for a BMH carrying the inventory-hold marker
2. **envtest**: Controller reconciliation; OnboardingServer CR creation from server
   list; BMH creation in `Inspecting` with default creds, `inspectionMode: fast`,
   and the inventory-hold marker; vendor/HW comparison against a simulated
   `HardwareData` (match → proceed; mismatch → **held BMH deleted** and terminal
   `VendorMismatch`/`HwMismatch`); declaring the BMC net/creds/LDAP config and
   advancing on simulated BMH conditions (the controller issues no Redfish — BMC
   interactions are mocked at the BMH-condition boundary); marker removal at
   `Publishing`; webhook validation (immutability, server list
   append-only, deletion guard rejecting only when a **published** BMH exists);
   OnboardingServer finalizer cleanup (held BMH deleted, per-server
   ExternalSecret/Secret removed, published BMH and its credentials **retained**,
   `status.cleanupStatus` set; retry-by-delete recreates the server as `Pending`)
3. **Integration**: Deploy onboarding Deployment, create OnboardingRequest CR with
   server list, verify OnboardingServer CRs progress through the state machine —
   BMH created and inspected, matched, credentials applied, marker removed — and
   that the host appears in the O2 IMS inventory **only after** `Publishing`
4. **Manual**: Add servers to OnboardingRequest, observe full onboarding flow via `oc get onboardingservers -n <resourcePoolName>`; confirm a held BMH is not surfaced in the O2 IMS Resource inventory until the host is published (`Publishing`)

## 12. Open Questions / Product Inputs

> **External dependency — forthcoming Metal3 APIs.** The whole design assumes Metal3/BMO
> provides fast out-of-band inspection
> and the BMC networking, credentials, and LDAP config APIs.
> The three BMC-config APIs are not yet available, so the exact
> API shape (BMH fields vs. companion CRs) is **not frozen** — the illustrative YAML,
> the `bmcconfig.go` companion-CR handling, and the `metal3.io` BMC-config RBAC row
> must be reconciled with the final APIs. Each onboarding state is blocked until its
> API lands (see [Dependency on forthcoming Metal3 APIs](server-onboarding-service.md#dependency-on-forthcoming-metal3-apis)
> and [Prerequisites](#9-prerequisites)). This is an external-delivery dependency, not
> an open design decision.

The design is functionally complete; the items below are **non-functional targets
that depend on product-management input**, not unresolved design decisions. They do
not block implementation, but the acceptance criteria and any capacity/scale
testing cannot be finalized until the numbers are supplied.

| # | Item | What is needed | Impact if unset |
|---|------|----------------|-----------------|
| 1 | **Batch scale / timing target** | Max servers per OnboardingRequest and the time budget (e.g. "100 servers within N minutes"). | No throughput criterion; `MaxConcurrentReconciles` can't be validated against a target. The architecture scales via that knob; only the number is absent. |
| 2 | **Per-Hub concurrent-batch count** | Max OnboardingRequests/pools a single Hub onboards concurrently. | No capacity-planning ceiling. The reconcile pool is global, not per-pool, so a supported maximum is needed to size it (and to decide whether per-pool fairness is ever needed). |
| 3 | **Boot-interface selection** | How the provisioning/boot NIC is identified on a multi-NIC host (e.g. a pool-level selector by NIC name or MAC prefix) so the controller can set the boot-interface label / `spec.bootMACAddress` from inspection before `Publishing`. | On single-NIC hosts the controller can infer it; on multi-NIC hosts it cannot pick the boot interface from OOB inspection alone, so such servers would fail to publish without a selector. See [BMH creation](#6-hardware-inventory-and-bmh-creation). |

Both feed directly into the [Concurrency and sizing](#7-controllers) guidance:
once the targets are known, the `MaxConcurrentReconciles` default and any
horizontal scaling of the REST server can be set and verified. Until then the
proposal commits to the mechanism, not a specific number.
