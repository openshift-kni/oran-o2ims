<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Automated IBI Server Pre-Provisioning via ProvisioningRequest API

## Background

The IBI-based cluster provisioning workflow (documented in
`docs/user-guide/ibi-based-cluster-provisioning.md`) is split into two stages:

1. **Factory preparation** — done once per hardware/software configuration:
   generate a seed image, build a live ISO, and pre-provision servers by
   booting them from the ISO via BMC virtual media.
2. **Site deployment** — done per cluster via ProvisioningRequest: since
   servers arrive pre-provisioned, cluster provisioning skips OS installation
   and image pull.

The [automated seed image and live ISO generation proposal](./automated-seed-image-generation.md)
automates the seed image and ISO creation. This proposal automates the final
factory step: **pre-provisioning servers by booting them from the IBI live ISO**.

Today, pre-provisioning is a manual process. An operator must:

1. Mount the live ISO to each target server via BMC virtual media
2. Set a one-time boot override to boot from the virtual CD/ISO
3. Power on the server and monitor the `install-rhcos-and-restore-seed.service`
   logs via SSH until "IBI preparation process finished successfully!" appears
4. Power off or leave the server running

This proposal adds an optional pre-provisioning phase to the ProvisioningRequest
workflow by extending the IBI Operator (`stolostron/image-based-install-operator`)
to handle the ISO boot and preparation lifecycle natively.

## Problem Statement

- **Manual BMC interaction**: Operators must manually mount the ISO via each
  server's BMC console (Redfish, iDRAC, iLO, etc.)
- **Unmonitored**: No status reporting through the O-Cloud Manager API;
  operators must SSH and tail logs on each server
- **Serial execution**: Without automation, operators typically pre-provision
  servers one at a time rather than in parallel
- **Error-prone handoff**: The boundary between "pre-provisioned" and "ready
  for IBI deployment" is manual — operators must remember to label BMHs
  and set the correct state

## Architecture

### Why the IBI Operator

The pre-provisioning step is best integrated into the IBI Operator
(`stolostron/image-based-install-operator`) rather than the O-Cloud Manager
controller for several reasons:

- **Already owns BMH lifecycle for IBI**: The IBI Operator creates
  `DataImage` CRs, manages BMH annotations (`detached`, `reboot`,
  `image-based-install-managed`), sets `externallyProvisioned`, and
  handles power management. Adding a "boot from ISO first" phase is a
  natural extension.
- **Already serves an HTTP endpoint**: The IBI Operator runs an image
  server (`internal/imageserver/`) that serves config ISOs to BMHs. This
  existing HTTP infrastructure can be extended to receive completion
  callbacks from pre-provisioned servers.
- **Reusable**: Other consumers of the IBI Operator (not just
  ProvisioningRequest users) would benefit from automated pre-provisioning.
- **Clean separation**: O-Cloud Manager handles orchestration (what to
  provision), the IBI Operator handles mechanics (how to provision).
  Pre-provisioning is a "how" concern.

### Monitoring Approach: HTTP Callback

Rather than SSH-based monitoring (which requires hub-to-server network
access, SSH key management, and IP discovery), the IBI Operator uses an
**HTTP callback** pattern:

1. The IBI Operator generates a small ignition config override containing a
   systemd unit that calls back to the hub after the IBI preparation service
   completes (success or failure).
2. This callback unit is injected into the ISO via the
   `ignitionConfigOverride` field already supported by the
   `ImageBasedInstallationConfig`.
3. The IBI Operator's existing HTTP server exposes a callback endpoint
   (e.g., `/callbacks/<ici-namespace>/<ici-name>/status`).
4. When the preparation service finishes, the callback unit `curl`s the
   hub endpoint with the result.
5. The IBI Operator receives the callback, updates the ICI condition, and
   proceeds with the BMH state transition.

This approach:

- **Requires no SSH**: No SSH keys to manage, no IP discovery needed, and
  no hub → server connectivity. It does **not** eliminate the network
  dependency — it inverts its direction: the model requires
  **server → hub** reachability instead.
- **Aligns with existing egress, not "works behind any firewall"**: the
  server must reach the hub's callback endpoint over HTTPS (DNS resolution
  of the endpoint hostname, IP routing from the provisioning network to the
  hub Route/Service, and TLS trust of the hub certificate). This is the
  same *direction* of connectivity already needed for the server to pull
  images from the hub, but it is a concrete precondition, not a given —
  an environment that blocks arbitrary provisioning-network → hub egress,
  or that resolves/routes the endpoint differently from the image
  registries, will drop callbacks. The reachability contract is therefore
  made explicit: the IBI Operator validates that the callback endpoint is
  resolvable, routable, and TLS-trusted from the target's network **before**
  booting the ISO (see [Challenge 1](#1-callback-url-must-be-reachable-from-the-pre-provisioned-server)),
  and a `PreProvisioningTimedOut` is treated as a possible reachability
  failure, not only a preparation failure.
- **Leverages existing infrastructure**: The IBI Operator already serves
  HTTP; adding a callback handler is minimal
- **Scales naturally**: Callbacks are event-driven, not poll-based — no
  per-server reconcile loops

### Component Responsibilities

```text
┌─────────────────────────────────────────────────────────────────┐
│ O-Cloud Manager (ProvisioningRequest Controller)                │
│                                                                 │
│  1. Hardware allocation (NAR)                                   │
│  2. Populate ImageClusterInstall with pre-provisioning config   │
│  3. Create ClusterInstance (triggers SiteConfig → ICI creation) │
│  4. Monitor ICI conditions for pre-provisioning + install       │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ SiteConfig Operator                                             │
│                                                                 │
│  Renders ClusterInstance → creates ImageClusterInstall CR       │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ IBI Operator (upstream changes required)                        │
│                                                                 │
│  1. NEW: Generate callback ignition override                    │
│  2. NEW: Boot BMH from live ISO via spec.image (live-iso)       │
│  3. NEW: Receive HTTP callback on preparation completion        │
│  4. NEW: Transition BMH to IBI-ready state                      │
│  5. Existing: Create config DataImage, reboot from disk,        │
│     monitor cluster installation                                │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ Metal3 / Ironic (BMO)                                           │
│                                                                 │
│  Handles BMH spec.image (live-iso) → virtual media mount + boot │
│  Handles DataImage → virtual media attach for config ISO        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Proposed API Changes

### IBI Operator: ImageClusterInstall CRD Changes

New optional fields in `ImageClusterInstallSpec`:

```go
type ImageClusterInstallSpec struct {
    // ... existing fields ...

    // PreProvisioning configures optional server pre-provisioning.
    // When set, the IBI Operator boots the BMH from the specified
    // live ISO and monitors the IBI preparation process via HTTP
    // callback before proceeding to cluster installation.
    // +optional
    PreProvisioning *PreProvisioningConfig `json:"preProvisioning,omitempty"`
}

type PreProvisioningConfig struct {
    // ISOURL is the HTTPS URL of the IBI live ISO accessible from
    // the BMC management network. It must resolve to an operator-owned or
    // explicitly allowlisted ISO origin; the operator rejects any off-allowlist
    // URL at admission (PreProvisioningConfigInvalid) and never fetches it, so
    // a caller cannot direct the operator's pre-fetch at an arbitrary host.
    ISOURL string `json:"isoURL"`

    // ISODigest is the expected SHA-256 digest ("sha256:<hex>") of the
    // live ISO served at ISOURL. The operator verifies the fetched ISO against
    // it before setting the BMH image so a mutated or stale artifact at that URL
    // cannot be booted. This is the same digest the seed-generation workflow
    // records in SeedGenerationStatus.ISODigest; wiring it through pins the
    // pre-provisioning boot to the exact artifact that was built and verified.
    // Required for automated boot: admission rejects a pre-provisioning config
    // that omits it (PreProvisioningConfigInvalid) rather than falling back to
    // URL-only trust. The json tag keeps omitempty only so the zero value is
    // caught by the required-field check, not silently defaulted.
    ISODigest string `json:"isoDigest,omitempty"`

    // ISOServerCACertRef is a reference to a ConfigMap containing the
    // CA certificate bundle ('ca-bundle.crt' key) for the HTTPS ISO
    // server. Required when the server uses a private CA.
    // +optional
    ISOServerCACertRef *corev1.LocalObjectReference `json:"isoServerCACertRef,omitempty"`

    // Timeout is the maximum duration to wait for the IBI preparation
    // to complete. Default: 60m.
    // +optional
    Timeout *metav1.Duration `json:"timeout,omitempty"`

    // CallbackBaseURL is the externally-reachable base URL that the
    // booted host uses to reach the IBI Operator's callback endpoint,
    // e.g. "https://ibi-callback.example.com:8443". The operator appends
    // "/callbacks/<namespace>/<name>/status" to form the full endpoint
    // that is injected into the ISO's ignition. This must be set to an
    // address reachable from the *provisioning network the booted host
    // is on*, which is frequently NOT the in-cluster Service DNS name
    // (`<svc>.<ns>.svc`): that name resolves only inside the hub cluster,
    // while the callback originates from the bare-metal host. When unset,
    // the operator falls back to the in-cluster Service URL, which is
    // only correct when the host shares the hub's cluster network.
    // The operator validates that the resolved host is reachable from the
    // target network before booting the ISO (see Challenge 1); a value
    // that only resolves in-cluster is treated as a configuration error.
    // +optional
    CallbackBaseURL string `json:"callbackBaseURL,omitempty"`

    // MaxRetries caps the number of ADDITIONAL boot attempts after the
    // first, when an attempt fails or times out. 0 (the default) means a
    // single attempt with no retry. Retries stop once this budget is
    // exhausted, after which the result is terminal. There is no infinite
    // retry and no retry after a terminal result.
    // +optional
    // +kubebuilder:default=0
    MaxRetries int `json:"maxRetries,omitempty"`

    // RetryBackoff is the delay between a failed attempt's teardown and the
    // next boot, applied as a self-triggering RequeueAfter. Default: 5m.
    // The operator enforces an overall ceiling of
    // (MaxRetries+1)*Timeout + MaxRetries*RetryBackoff so total retry effort
    // is finite.
    // +optional
    RetryBackoff *metav1.Duration `json:"retryBackoff,omitempty"`
}
```

New condition and status fields:

```go
const (
    PreProvisioningPendingReason    = "PreProvisioningPending"
    PreProvisioningBootingReason    = "PreProvisioningBooting"
    PreProvisioningInProgressReason = "PreProvisioningInProgress"
    PreProvisioningRetryingReason   = "PreProvisioningRetrying"
    PreProvisioningSucceededReason  = "PreProvisioningSucceeded"
    PreProvisioningFailedReason     = "PreProvisioningFailed"
    PreProvisioningTimedOutReason   = "PreProvisioningTimedOut"
)

type ImageClusterInstallStatus struct {
    // ... existing fields ...

    // PreProvisioningBootTime indicates when the BMH was booted from the
    // live ISO for pre-provisioning, and is the anchor for the timeout. It is
    // set ONLY after the BMH boot request has been applied (NOT in the
    // pre-boot status update that records PreProvisioningAttempt), so the
    // timeout clock cannot start before the host was actually asked to boot.
    // See the preProvisionHost ordering in "IBI Operator Workflow Changes."
    // +optional
    PreProvisioningBootTime *metav1.Time `json:"preProvisioningBootTime,omitempty"`

    // PreProvisioningRetryCount records the number of additional attempts
    // already consumed against the MaxRetries budget. It is persisted so the
    // budget survives operator restarts; a retry is scheduled only while
    // this remains below MaxRetries.
    // +optional
    PreProvisioningRetryCount int `json:"preProvisioningRetryCount,omitempty"`

    // RetryNotBefore is the durable retry-pending marker. When a failed attempt
    // still has retry budget, the operator sets this (now + RetryBackoff) in the
    // SAME update that clears PreProvisioningAttempt, so recovery can tell a
    // retired attempt in backoff (attempt unset, RetryNotBefore set) from an
    // in-flight one (attempt set). The remaining backoff is recomputed from this
    // value on every reconcile, so a restart that loses the in-memory
    // RequeueAfter still honors the delay. Cleared when the next attempt is
    // recorded (RetryNotBefore and PreProvisioningAttempt are never both set).
    // +optional
    RetryNotBefore *metav1.Time `json:"retryNotBefore,omitempty"`

    // PreProvisioningAttempt identifies the current boot attempt. The
    // operator increments (or regenerates) it each time it boots the BMH
    // for pre-provisioning, and mints the per-attempt callback token bound
    // to this value (the token embeds the attempt id as a claim). It is
    // the single source of truth for "which boot are we waiting on."
    // +optional
    PreProvisioningAttempt string `json:"preProvisioningAttempt,omitempty"`

    // PreProvisioningResult records the result received via HTTP callback
    // from the pre-provisioned server for a specific attempt. The operator
    // clears this (sets it to nil) atomically in the same pre-boot status
    // update that records a new PreProvisioningAttempt (NOT
    // PreProvisioningBootTime, which is written later — see that field), so a
    // stale result from a prior boot can never be mistaken for the outcome of
    // the current one. A callback is accepted only if its Attempt matches the
    // current PreProvisioningAttempt; callbacks for any other attempt are
    // rejected.
    // +optional
    PreProvisioningResult *PreProvisioningResult `json:"preProvisioningResult,omitempty"`

    // PreProvisioningTeardownComplete gates cessation of teardown retries for
    // a terminal (failed/timed-out) attempt. When the operator claims the
    // terminal state it writes this as false in the same update; teardown
    // (clear image, remove DataImage, power off, delete the per-attempt token
    // Secret) is then idempotently re-driven on every reconcile until all
    // actions succeed, at which point this flips to true — the ONLY state that
    // stops teardown retries. It survives restarts so a crash mid-teardown
    // cannot leave a terminal ICI with cleanup still owed. Reset to false (or
    // cleared) when a retry mints a fresh attempt; meaningful only once a
    // terminal reason (PreProvisioningFailed/PreProvisioningTimedOut) is set.
    // +optional
    PreProvisioningTeardownComplete bool `json:"preProvisioningTeardownComplete,omitempty"`
}

type PreProvisioningResult struct {
    // Attempt is the PreProvisioningAttempt this result belongs to. The
    // operator records it from the (token-verified) callback and compares
    // it to Status.PreProvisioningAttempt; a mismatch means the result is
    // for a superseded boot and is discarded.
    Attempt string `json:"attempt"`
    // Status is "success" or "failure".
    Status string `json:"status"`
    // Message contains the completion or error message from the
    // preparation service.
    // +optional
    Message string `json:"message,omitempty"`
    // ReceivedAt is the timestamp when the callback was received.
    ReceivedAt metav1.Time `json:"receivedAt"`
}
```

The IBI Operator reports pre-provisioning progress via the existing
`RequirementsMet` condition before proceeding to its normal `configureHost`
flow. **`RequirementsMet` is the single authoritative status field for the
handoff** — the design deliberately does *not* introduce a separate
`PreProvisioningCompleted` condition, so producer (IBI Operator) and consumer
(O-Cloud Manager) agree on exactly one field and cannot end up watching
different conditions (which would advance only after timeout). The
pre-provisioning phase is distinguished by the condition `reason`
(`PreProvisioning*`), not by a different condition `type`. This one contract
is used consistently by the IBI Operator, the O-Cloud Manager mapping, and the
integration tests.

### O-Cloud Manager: ClusterTemplate Configuration

The pre-provisioning configuration is placed in `hwMgmtDefaults` (and
overridable via `hwMgmtParameters`) since it describes how the hardware
should be prepared:

```yaml
# In ClusterTemplate spec.templateDefaults.hwMgmtDefaults
hwMgmtDefaults:
  hardwareProvisioningTimeout: "120m"
  nodeGroupData:
    - name: controller
      role: master
      resourceSelector:
        resourceselector.clcm.openshift.io/server-type: XR8620t
  # Optional: IBI pre-provisioning configuration.
  # Passed through to ImageClusterInstall.spec.preProvisioning.
  ibiPreProvisioning:
    # Required: HTTPS URL of the live IBI ISO accessible from the BMC
    # management network.
    isoURL: https://iso-server.example.com/ibi/rhcos-ibi-4.Y.Z.iso
    # Optional (recommended): expected SHA-256 digest of the ISO, verified
    # before boot to pin the exact artifact. Matches
    # SeedGenerationStatus.ISODigest from the seed-generation workflow.
    isoDigest: sha256:0000000000000000000000000000000000000000000000000000000000000000
    # Optional: reference to a ConfigMap containing the CA certificate
    # bundle for the HTTPS ISO server.
    isoServerCACertRef:
      name: iso-server-ca-cert
    # Optional: timeout for pre-provisioning a single server.
    # Default: 60m.
    preProvisioningTimeout: "60m"
    # Optional: number of automatic re-boot attempts after a failed/timed-out
    # attempt before the phase is terminal. Default: 0 (single attempt).
    maxRetries: 2
    # Optional: delay between a failed attempt and the next boot.
    # Default: 5m.
    retryBackoff: "5m"
```

The O-Cloud Manager controller passes these values through to the
`ImageClusterInstall` CR when rendering the ClusterInstance. The IBI
templates in the SiteConfig Operator would need to support the new
`preProvisioning` fields (or the O-Cloud Manager patches the ICI directly
after SiteConfig creates it).

The `hwMgmtDefaults` field names are hardware-management-oriented and do not
match the `ImageClusterInstall.spec.preProvisioning` field names one-to-one;
the passthrough is an explicit mapping, not a verbatim copy. The controller
applies this mapping when rendering the ICI:

| `hwMgmtDefaults.ibiPreProvisioning` | `ImageClusterInstall.spec.preProvisioning` |
| --- | --- |
| `isoURL` | `isoURL` |
| `isoDigest` | `isoDigest` |
| `isoServerCACertRef` | `isoServerCACertRef` |
| `preProvisioningTimeout` | `timeout` |
| `maxRetries` | `maxRetries` |
| `retryBackoff` | `retryBackoff` |

`callbackBaseURL` on the ICI is **not** sourced from `hwMgmtDefaults`; the
operator derives it from operator-level configuration (the externally-
reachable callback address is an environment property, not a per-template
input). This is a **security boundary, not just a naming choice**: the
callback client sends the per-attempt **bearer token** to whatever host
`callbackBaseURL` resolves to, so a template or ProvisioningRequest author who
could set it freely could redirect the token to a host they control and then
forge a success callback. The field is therefore **operator-owned**:

1. The IBI Operator's own configuration: an `IBI_CALLBACK_BASE_URL` value
   sourced from the operator Deployment environment (populated from the
   O-Cloud Manager `Inventory` CR's externally-reachable ingress/route
   configuration). This is the normal way to set it fleet-wide.
2. Otherwise the in-cluster Service URL (`<svc>.<ns>.svc`) — usable **only**
   when the host shares the hub cluster network.

`PreProvisioningConfig.CallbackBaseURL` on the ICI is **not** a
template/ProvisioningRequest input. It is not surfaced in
`hwMgmtParameters`, and write access to `ImageClusterInstall.spec.preProvisioning.callbackBaseURL`
is restricted by RBAC to the operator's own service account (template and PR
authors cannot set it). If a value is present, it is accepted **only** when it
matches an operator-maintained **allowlist** of callback origins (the same
`Inventory`-derived hosts); any off-allowlist origin is rejected at admission
with `PreProvisioningConfigInvalid` and the ISO is never booted. An admission
test covers a spoofed off-allowlist origin.

If none of these yields a URL reachable from the target provisioning
network, the operator **fails closed**: it does not boot the ISO and reports
a configuration error (`PreProvisioningConfigInvalid`), rather than booting a
host that can never call back and then timing out 60 minutes later. In
particular the in-cluster fallback is accepted only when the reachability
check in Challenge 1 confirms the host is on the hub cluster network.

Keeping the names distinct on each side is intentional:
`preProvisioningTimeout` reads clearly in the hardware-timeouts block
alongside `hardwareProvisioningTimeout`, while `timeout` is scoped by its
`preProvisioning` parent on the ICI. The mapping table above is the
authoritative contract between the two field sets.

### Schema in templateParameterSchema

```yaml
hwMgmtParameters:
  properties:
    ibiPreProvisioning:
      type: object
      properties:
        isoURL:
          type: string
          description: >
            HTTPS URL of the IBI live ISO. Must resolve to an operator-owned
            or allowlisted ISO origin; off-allowlist URLs are rejected before
            any fetch.
          minLength: 1
        isoDigest:
          type: string
          description: >
            Expected SHA-256 digest ("sha256:<hex>") of the live ISO;
            verified before boot to pin the exact artifact. Required for
            automated boot.
          pattern: '^sha256:[a-f0-9]{64}$'
        isoServerCACertRef:
          type: object
          properties:
            name:
              type: string
          required: [name]
        preProvisioningTimeout:
          type: string
          description: Timeout for pre-provisioning (e.g., "60m")
        maxRetries:
          type: integer
          minimum: 0
          description: >
            Automatic re-boot attempts after a failed/timed-out attempt
            before the phase is terminal (default 0 = single attempt).
        retryBackoff:
          type: string
          description: >
            Delay before the next attempt, as a Go duration (e.g., "5m").
      required: [isoURL, isoDigest]
  type: object
```

Validation mirrors the schema: `maxRetries` must be a non-negative integer
and `retryBackoff` must parse as a non-negative Go `time.Duration`. Both the
admission webhook and the ICI passthrough reject out-of-range values so a bad
override is caught at write time rather than surfacing as a stuck attempt. When
omitted, the controller applies the `PreProvisioningConfig` defaults
(`maxRetries: 0`, `retryBackoff: 5m`).

Because `isoURL` and `isoDigest` are caller-overridable through
`hwMgmtParameters`, validation also enforces the two artifact-trust checks
before the operator ever fetches the URL (see "Restricting `ISOURL`"): `isoURL`
must resolve to an operator-owned or allowlisted origin, and `isoDigest` is
required (the schema marks it required; the webhook and passthrough re-check it,
rejecting a missing or off-allowlist value with `PreProvisioningConfigInvalid`).
The allowlist is operator configuration and cannot be widened by a template or
`ProvisioningRequest`.

## IBI Operator Workflow Changes

### Current Flow (no pre-provisioning)

```text
ImageClusterInstall created
  → validateBMH (check BMH state, hardware details)
  → writeInputData (generate config ISO)
  → configureHost:
      → ensureBMHDataImage (attach config ISO via DataImage)
      → updateBMHProvisioningState (set online, reboot annotation)
      → set Status.BootTime
  → monitor cluster installation
  → installation complete
```

### New Flow (with pre-provisioning)

```text
ImageClusterInstall created (with spec.preProvisioning set)
  → validateBMH (check BMH state)
      # IMPORTANT: when spec.preProvisioning is set, validateBMH/validateHost
      # must NOT set BMH spec.externallyProvisioned. The host is not yet
      # prepared; marking it externallyProvisioned here would tell BMO the OS
      # is already installed and skip/short-circuit the very boot we need.
      # externallyProvisioned is set ONLY on a verified success callback,
      # in transitionToIBIReady below.
  → NEW: preProvisionHost (ordered so every write is recoverable — see
    "Crash-safety of the boot handoff" below; each step is idempotent and the
    next reconcile resumes from the first incomplete one):
      # RECOVERY: classify the current state before doing anything:
      #   (a) Status.PreProvisioningAttempt set        → in-flight attempt:
      #       resume it. Do NOT mint a new id/token; reuse the existing attempt
      #       and its token Secret and re-drive the remaining steps idempotently.
      #   (b) attempt unset, but a pending token Secret owned by this ICI and
      #       carrying the ibi.openshift.io/attempt label exists → the status
      #       write in step 2 was lost after step 1; ADOPT that Secret's attempt
      #       id (do NOT mint a second one) and continue at step 2.
      #   (c) attempt unset, no pending Secret, Status.RetryNotBefore set → a
      #       retired attempt is in backoff: wait until now >= RetryNotBefore
      #       (RequeueAfter = the remainder, recomputed here so a restart that
      #       lost the in-memory timer still honors the backoff), then mint.
      #   (d) none of the above → first attempt: mint.
      → 1. mint intent (only in cases (c)-after-backoff and (d)):
          → mint attempt id + per-attempt token
          → write the token to its own Secret, named deterministically from
            the attempt id, OWNED by this ICI (ownerReference) and labelled
            ibi.openshift.io/attempt=<id> (create-or-adopt). The token lives
            here, NOT in status, so it must be persisted BEFORE the attempt id
            is recorded. The owner + label make a written-but-not-yet-recorded
            Secret discoverable (case (b)), so a lost step-2 write is adopted
            rather than orphaned — the attempt id is recoverable from the Secret
            even when it is absent from status.
      → 2. record the attempt (ONE status update, flushed):
          → set Status.PreProvisioningAttempt = <id>
          → clear Status.PreProvisioningResult (a stale result from a prior
            attempt must not be read as this attempt's outcome)
          → clear Status.RetryNotBefore (this attempt is now in flight, not
            pending)
          → condition: RequirementsMet = False / PreProvisioningBooting
          → do NOT set PreProvisioningBootTime yet (see step 5)
      → 3. deliver the per-host callback data (URL, attempt id, token,
         CALLBACK_BUDGET_SECS) on a DataImage carrier (create-or-adopt), NOT via
         preprovisioningNetworkDataName (that field is an nmstate network-data
         Secret and does not inject callback config into a live-iso boot — see
         Challenge 4). The DataImage MUST be named and namespaced after the
         BareMetalHost (same name + namespace): BMO v0.13.2's
         DataImageReconciler resolves the owning host by matching the DataImage
         request name/namespace to a BareMetalHost, so an attempt-id-named
         DataImage would never attach. The attempt id is bound instead through
         carrier content and a label (e.g. `ibi.openshift.io/attempt: <id>`),
         which the create-or-adopt step also uses to detect and replace a stale
         carrier from a prior attempt. The generic callback client is already
         baked into the live ISO via ignitionConfigOverride at build time
         (identical for every host).
      → 4. mutate the BMH (idempotent patch):
          → set BMH spec.image.url = isoURL, spec.image.format = "live-iso"
          → set BMH spec.online = true
      → 5. set Status.PreProvisioningBootTime = now, ONLY after the BMH boot
         request in step 4 is confirmed applied. BootTime is the timeout
         anchor, so it must mark an actual boot request, not merely recorded
         intent; a resumed attempt whose steps 3-4 completed but whose BootTime
         is unset verifies the BMH is booting the expected ISO and then stamps
         BootTime.
  → NEW: waitForCallback:
      → IBI Operator HTTP server receives POST to
        /callbacks/<namespace>/<name>/status
      → callback payload: {"attempt": "<n>", "status": "success|failure", "message": "..."}
      → on success:
          → clear BMH spec.image
          → set BMH spec.externallyProvisioned = true
          → set BMH spec.online = false (power off)
          → set Status.PreProvisioningResult
          → condition: RequirementsMet = True / PreProvisioningSucceeded
      → on failure (or timeout): tear the attempt down (clear image,
        remove DataImage, power off, invalidate the per-attempt token),
        then branch on the retry budget — do NOT set externallyProvisioned:
          → if PreProvisioningRetryCount < MaxRetries (budget remains):
              → increment Status.PreProvisioningRetryCount
              → set Status.RetryNotBefore = now + RetryBackoff, and in the SAME
                update CLEAR Status.PreProvisioningAttempt, PreProvisioningResult
                and PreProvisioningBootTime. Clearing the attempt id is what
                stops the recovery rule from re-driving the retired attempt with
                its now-invalidated token; RetryNotBefore is the durable
                retry-pending marker that survives a restart (the next reconcile
                recomputes the remaining backoff from it rather than relying on
                an in-memory RequeueAfter that a crash would lose).
              → condition: RequirementsMet = False / PreProvisioningRetrying
                (NON-terminal — the ProvisioningRequest keeps progressing)
              → RequeueAfter = RetryBackoff. A fresh attempt (new id + token) is
                minted only once now >= RetryNotBefore, via the mint path above —
                never inline here, so the boot is driven from one place and a
                restart mid-backoff still waits out the remainder.
          → else (budget exhausted): this is the terminal attempt
              → set Status.PreProvisioningResult (status=failure)
              → condition: RequirementsMet = False /
                PreProvisioningFailed (timeout → PreProvisioningTimedOut)
                — terminal; only now does the PR map to failed
  → writeInputData (generate config ISO) — existing flow
  → configureHost — existing flow (BMH is now externallyProvisioned)
  → monitor cluster installation — existing flow
  → installation complete
```

**Crash-safety of the boot handoff.** A single attempt spans four durable
resources — the **token Secret**, the ICI **status** (`PreProvisioningAttempt`,
`PreProvisioningResult`, `PreProvisioningBootTime`), the **DataImage** carrier,
and the **BMH** spec — so "one atomic status update" is not enough on its own;
the write *ordering* above is what makes the whole handoff recoverable. The
invariants:

- **Token before attempt id, and discoverable.** The per-attempt token is
  persisted in its own deterministically-named Secret — owned by the ICI and
  labelled `ibi.openshift.io/attempt=<id>` — before the attempt id is written to
  status. An attempt id can therefore never reference a token that was never
  stored. If the status write in step 2 is lost after the Secret exists, the
  next reconcile finds the pending Secret by owner + label and **adopts its
  attempt id** rather than minting a second token, so the "token written, status
  not" window resumes the same attempt (no orphaned Secret, no duplicate token).
- **Attempt id is the recovery key for an in-flight attempt.** Once
  `PreProvisioningAttempt` is set, every subsequent reconcile **resumes the same
  attempt** — it reuses the existing id and token Secret and re-drives steps 3-5
  idempotently (create-or-adopt the BMH-named DataImage whose attempt-id label
  identifies the current attempt, idempotent BMH patch), rather than minting a
  fresh attempt while the previous ISO may still be booting.
- **A retired attempt is never resumed.** On a retry, the failed attempt's
  `PreProvisioningAttempt` is cleared in the same update that sets
  `RetryNotBefore`, so recovery can never mistake the retired attempt (whose
  token is already invalidated) for an in-flight one. Recovery distinguishes
  three post-failure states purely from persisted status: attempt set = resume;
  attempt unset + `RetryNotBefore` in the future = wait out the backoff;
  attempt unset + `RetryNotBefore` reached = mint the next attempt. The backoff
  remainder is recomputed from `RetryNotBefore` on every reconcile, so a restart
  that loses the in-memory `RequeueAfter` still honors it.
- **BootTime marks a real boot, and is written last.** `PreProvisioningBootTime`
  — the timeout anchor — is stamped only after the BMH boot request is
  confirmed applied. A resumed attempt with steps 3-4 complete but no BootTime
  verifies the BMH is booting the expected ISO, then stamps it; the timeout
  clock cannot start before the host was actually asked to boot.
- **Timeout still bounds a stuck resume.** An in-flight attempt whose
  `PreProvisioningBootTime` + `timeout` has elapsed is treated as timed out and
  enters the retry/terminal branch — never a silent re-mint.

Each partial-write window — "token Secret written, status not" (asserts the
pending Secret is adopted by owner + label, not re-minted); "status written,
DataImage not"; "DataImage created, BMH not patched"; "BMH patched, BootTime not
stamped"; and "retry recorded (`RetryNotBefore` set, attempt cleared), process
restarted mid-backoff" (asserts the remaining backoff is honored and exactly one
fresh attempt is then minted) — is covered by its own **fault-injection test**
that kills the reconcile at that point and asserts the next reconcile resumes
the same attempt (no new token, no duplicate boot, exactly one booting ISO).

### Key Implementation Details in the IBI Operator

#### 1. Callback Ignition Override

The IBI Operator generates an ignition config snippet containing a systemd
unit that runs after `install-rhcos-and-restore-seed.service` completes.
This unit reports the result back to the hub via HTTP.

**This depends on `install-rhcos-and-restore-seed.service` being a
`Type=oneshot` unit** (which the IBI seed-restore service is): a oneshot unit
does not reach `active`/exit until its `ExecStart` has finished, so its
`Result` property is a *terminal* success/failure verdict by the time the
reporter reads it. `After=` only orders start-up — it does **not** wait for a
`Type=simple` (or other long-running) unit to finish, so if the referenced unit
were not oneshot the reporter could read a non-failure `Result` while
preparation is still running and send a premature "success". If a future base
changes that unit's type, the design must instead gate on an explicit
completion sentinel (a dedicated oneshot unit ordered `After=` the prep service,
whose own terminal `Result` the reporter reads) rather than the prep service's
live state. The reporter contract is: read a terminal result, never a
mid-flight one.

```yaml
# Generated by the IBI Operator, injected as ignition override
systemd:
  units:
    - name: ibi-prep-callback.service
      enabled: true
      contents: |
        [Unit]
        Description=Report IBI preparation result to hub
        # Order AFTER the prep service, and use Wants= (NOT Requires=). The
        # whole point of this unit is to report BOTH success and failure, so
        # it must run even when the prep service fails. With Requires=, a
        # failed prep service would cause systemd to skip this reporter's job
        # entirely, no failure callback would be sent, and the hub would wait
        # out the full PreProvisioningTimedOut. Wants= keeps the ordering
        # dependency without cancelling the reporter when the prep service
        # fails. Requires=/After= on the data unit below is different: that
        # data must exist for the reporter to work at all, so it stays
        # Requires=. After= alone only orders start-up; it does not wait for
        # the prep service to finish, so it is paired with the current-boot
        # result check in ExecStart (the prep service reaches a terminal
        # success/failure state, so the result is deterministic when read).
        After=install-rhcos-and-restore-seed.service
        Wants=install-rhcos-and-restore-seed.service
        # Runtime dependency on the per-host data (see Challenge 4): the
        # reporter cannot run without its URL/token/attempt files. The data
        # unit is Type=oneshot with RemainAfterExit=yes, so it stays
        # active (exited) after materializing the files; without that this
        # Requires= would drop as soon as the data unit's ExecStart returned
        # and systemd could stop the reporter before it ran.
        Requires=ibi-callback-data.service
        After=ibi-callback-data.service
        # StartLimit* live in [Unit] (moved here from [Service] in systemd
        # v230). Backstop only — the real retry budget is the script's
        # absolute deadline.
        StartLimitIntervalSec=660
        StartLimitBurst=3

        [Service]
        # Type=oneshot with Restart=on-failure requires systemd v244 or
        # later (before v244 Restart= is silently ignored for oneshot
        # units, so the callback would never be retried). RHCOS ships
        # systemd 252+, so this is satisfied; the dependency is called out
        # here because the retry semantics below are load-bearing, not
        # decorative. On any older base the ExecStart must instead wrap the
        # send in its own bounded retry loop.
        Type=oneshot
        ExecStart=/usr/local/bin/ibi-prep-callback
        # The retry budget is enforced as an ABSOLUTE deadline inside the
        # script (a wall-clock loop), not by per-attempt timers, and it is
        # sized from the operator's PreProvisioningConfig.Timeout (default 60m,
        # delivered as CALLBACK_BUDGET_SECS) so callbacks stay alive for the
        # whole window the operator waits — see the script below.
        # TimeoutStartSec/Restart alone cannot bound total effort:
        # TimeoutStartSec caps a SINGLE start attempt, and Restart=on-failure
        # would keep re-launching the unit (subject to start-rate limits)
        # with no ceiling on cumulative time. So the script owns the budget:
        # it retries curl until it succeeds or the deadline passes, then
        # exits (0 on delivery, non-zero if the deadline elapsed).
        # TimeoutStartSec is DISABLED (infinity) because the budget is now
        # host-specific and can be up to the operator timeout; a fixed cap
        # baked into the shared ISO would truncate it. The script's persisted
        # deadline is the real bound. Restart + StartLimit remain only as a
        # backstop for an outright script crash.
        TimeoutStartSec=infinity
        Restart=on-failure
        RestartSec=30
        # Non-secret callback parameters (URL, attempt id). The bearer token
        # is NOT here — it is delivered in a root-only curl config file (see
        # below) so it never lands in an environment variable, the unit
        # contents, or the process argument list.
        EnvironmentFile=/etc/ibi-callback/env

        [Install]
        WantedBy=multi-user.target
```

The reporter logic is delivered as a small script (`/usr/local/bin/ibi-prep-callback`,
also injected via the ignition overlay) rather than an inline `bash -c`,
so the JSON body can be serialized safely and the token kept out of argv:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Determine the prep service result for THIS boot/attempt only. Use the
# systemd-tracked result (Result property) rather than grepping the journal,
# and scope any journal read to the current boot (-b 0). An unscoped
# `journalctl -u ...` would also match a "finished successfully" line left
# over from an earlier boot or a previous pre-provisioning attempt on the
# same disk, producing a false success. After= ordering alone does NOT
# disambiguate attempts.
RESULT=$(systemctl show -p Result --value install-rhcos-and-restore-seed.service)
if [ "${RESULT}" = "success" ]; then
  STATUS="success"
  MSG="IBI preparation process finished successfully"
else
  STATUS="failure"
  MSG=$(journalctl -b 0 -u install-rhcos-and-restore-seed.service \
    --no-pager -n 20 | tail -5)
fi

# Serialize the body with a real JSON encoder. MSG is arbitrary journal text
# containing newlines, double quotes, and backslashes; hand-built
# "{\"message\": \"${MSG}\"}" produces invalid JSON and can be used to
# inject fields. Use jq (shipped in RHCOS) rather than python3 (NOT present
# in the RHCOS live ISO — invoking it would fail and, under `set -e`, exit
# before curl, turning every callback into a timeout). --arg passes each
# value as a literal string so no field injection is possible. The attempt
# id ties this callback to the exact boot the operator is waiting on (see
# PreProvisioningAttempt); the operator rejects any mismatch.
PAYLOAD=$(jq -nc \
  --arg attempt "${CALLBACK_ATTEMPT}" \
  --arg status "${STATUS}" \
  --arg message "${MSG}" \
  '{attempt: $attempt, status: $status, message: $message}')

# Retry against an ABSOLUTE wall-clock deadline, not a per-attempt timer.
# This bounds cumulative effort regardless of how many individual attempts
# fail, which neither TimeoutStartSec nor Restart= can do on their own. On the
# first successful delivery, exit 0 immediately; if the deadline passes without
# success, exit non-zero (systemd's Restart is only a crash backstop, bounded
# by StartLimit).
#
# The budget MUST cover the operator's whole wait window. The operator waits up
# to PreProvisioningConfig.Timeout (default 60m) for a callback; if the host
# stopped retrying after a fixed ~10m, a hub that is unreachable for 10-59m
# after prep succeeds would never receive the (already-produced) result and the
# operator would time out a host that actually succeeded. So the operator
# delivers CALLBACK_BUDGET_SECS per-host in the env file, derived from the
# effective timeout (timeout minus a small margin so the last send precedes the
# operator's own deadline). The script uses that as its budget instead of a
# hardcoded constant.
#
# CALLBACK_BUDGET_SECS is a REQUIRED per-host value (see env file below). There
# is deliberately NO silent fallback: a missing budget is a delivery bug, and
# defaulting to a short window would reintroduce the very "host stops before the
# operator's timeout" failure this value exists to prevent. Fail loudly instead.
: "${CALLBACK_BUDGET_SECS:?CALLBACK_BUDGET_SECS not delivered}"
#
# Anchor the deadline to the host's BOOT time, not to this script's first-run
# time. The operator's timeout starts at PreProvisioningBootTime (≈ when the
# host was asked to boot), but this reporter only starts after preparation
# finishes, which can be many minutes later. Computing `now + budget` here would
# hand a late-starting reporter a fresh full budget and push its window past the
# operator's deadline. Using boot time keeps the two clocks aligned.
#
# The deadline is also PERSISTED to a tmpfs file so it survives a
# Restart=on-failure relaunch (a crash + re-exec must not restart the budget);
# the first invocation stamps it, later invocations read it back.
DEADLINE_FILE=/run/ibi-callback/deadline   # tmpfs, cleared on reboot
mkdir -p "$(dirname "${DEADLINE_FILE}")"
if [ -s "${DEADLINE_FILE}" ]; then
  DEADLINE=$(cat "${DEADLINE_FILE}")
else
  UPTIME=$(cut -d. -f1 /proc/uptime)              # seconds since boot
  BOOT_EPOCH=$(( $(date +%s) - UPTIME ))          # ≈ operator's boot request
  DEADLINE=$(( BOOT_EPOCH + CALLBACK_BUDGET_SECS ))
  printf '%s' "${DEADLINE}" > "${DEADLINE_FILE}"
fi
ATTEMPT_TIMEOUT=20                   # per-curl cap so one hang can't eat the budget
# --config points at a root-only (0600) file that carries ONLY the
# Authorization header:  header = "Authorization: Bearer <token>"
# This keeps the token out of argv (visible via ps / /proc/*/cmdline) and
# out of the environment. The endpoint is header-authenticated only; the
# token is never placed in the URL query string.
while :; do
  if curl -sf -X POST \
      --max-time "${ATTEMPT_TIMEOUT}" \
      --cacert /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem \
      --config /etc/ibi-callback/auth.cfg \
      -H "Content-Type: application/json" \
      -d "${PAYLOAD}" \
      "${CALLBACK_URL}"; then
    exit 0
  fi
  [ "$(date +%s)" -ge "${DEADLINE}" ] && exit 1
  sleep 30
done
```

The ignition overlay therefore delivers three per-server files, all as data
(not baked into the shared ISO — see [Challenge 4](#4-callback-url-injection-into-the-iso)):

- `/etc/ibi-callback/env` (0644): `CALLBACK_URL=...`, `CALLBACK_ATTEMPT=...`,
  and `CALLBACK_BUDGET_SECS=...` (the callback retry budget in seconds, derived
  by the operator from the effective `PreProvisioningConfig.Timeout` minus a
  margin so the host keeps resending for the entire window the operator waits)
  — all non-secret.
- `/etc/ibi-callback/auth.cfg` (0600, root-only): a curl config file whose
  sole line is `header = "Authorization: Bearer <per-attempt-token>"`.
- `/usr/local/bin/ibi-prep-callback` (0755): the script above (identical for
  every server; safe to bake into the shared ISO).

`CALLBACK_URL` is the operator's `callbackBaseURL` (see
`PreProvisioningConfig.CallbackBaseURL`) with
`/callbacks/<ici-namespace>/<ici-name>/status` appended — an externally-
reachable address, not the in-cluster `<service>.<namespace>.svc` name,
which resolves only inside the hub. In disconnected environments the server
typically reaches the hub via an external route, so `callbackBaseURL` is
that route hostname.

The operator builds `CALLBACK_URL` with **URL-aware joining**, not string
concatenation: it parses `callbackBaseURL`, rejects any value carrying a
query or fragment, and collapses a trailing slash so
`https://host/` and `https://host` both yield exactly one separator before
`/callbacks/...` (naive concatenation would produce `https://host//callbacks`
or splice the path into a query/fragment and never reach the endpoint). The
admission-time validation below enforces the same constraints so a malformed
base is rejected at write time rather than surfacing 60 minutes later as a
`PreProvisioningTimedOut`.

Delivering the snippet to a **live-iso** boot is the subtle part, and the
mechanism must be chosen against what BMO/Ironic actually honor for
`DiskFormat: live-iso`, not against what is available for a normal
provisioned boot:

- **`ignitionConfigOverride` baked into the ISO (authoritative path).** The
  live ISO's embedded Ignition is the one delivery surface Ironic reliably
  applies for a live-iso boot. The `ImageBasedInstallationConfig` supports
  `ignitionConfigOverride`, so the callback systemd unit can be merged into
  the ISO at build time. The cost is that the callback target must be known
  at build time — which conflicts with a per-ICI URL/token baked into a
  shared ISO (see [Challenge 4](#4-callback-url-injection-into-the-iso)).
  The resolution is to bake only a **generic** callback client into the ISO
  that reads its per-server URL and token from a well-known location, and
  deliver only that small per-server data at boot.
- **`DataImage` is not an ignition overlay.** A `DataImage` attaches an
  extra virtual-media device; BMO does **not** merge its contents into the
  live environment's Ignition, so it cannot be relied on to *inject* the
  callback systemd unit. It can only carry data that the ISO's baked-in
  generic client explicitly mounts and reads (e.g. a config drive the
  client looks for).
- **`spec.preprovisioningNetworkDataName` / `userData` / `networkData` /
  `metaData` are not a safe assumption for live-iso.** For a live-iso boot
  BMO/Ironic do not process these the way they do for a normal deploy, so
  the design must not depend on them to carry the ignition override.

**Recommended path:** bake a generic callback client into the ISO via
`ignitionConfigOverride` at build time, and deliver the per-server callback
URL + token at boot through a mechanism the client reads itself. Because the
exact behavior of `live-iso` + `DataImage` + config-drive discovery is
version-dependent in BMO/Ironic, this path **must be covered by a
version-pinned integration test** against the supported BMO/Ironic version
rather than assumed; if that test cannot be made to pass, the fallback is a
unique per-ICI ISO with the URL/token baked directly into
`ignitionConfigOverride`. This tension is tracked in
[Open Question 2](#open-questions).

#### Host network configuration for the callback

Everything above assumes the live-ISO host can already reach
`CALLBACK_URL` over the network. That is not automatic: the callback carrier
(`DataImage`) delivers only the URL/token/attempt data, and — as noted above —
`preprovisioningNetworkDataName` / `networkData` are **not** reliably honored
for a live-iso boot, so they cannot be assumed to configure the host's IP,
route, and DNS. Without a defined network path the callback never leaves the
host and the operator times the (possibly successful) preparation out. The
design therefore requires one of two explicit modes, selected per template:

- **DHCP (default, must be validated).** DHCP on the provisioning network is
  the assumed baseline. It is stated as an explicit prerequisite, and the
  reachability check in
  [Challenge 1](#1-callback-url-must-be-reachable-from-the-pre-provisioned-server)
  is the gate: if the host cannot obtain an address and reach
  `callbackBaseURL`, the
  operator fails closed with `PreProvisioningConfigInvalid` rather than booting
  a host that can never call back.
- **Static networking (DHCP-less environments).** When DHCP is unavailable,
  the host IP/route/DNS are supplied at ISO build time — as dracut
  `ip=`/`nameserver=` kernel arguments and/or NetworkManager keyfiles injected
  into the ISO via `coreos-installer iso customize`, carried in the same
  `ignitionConfigOverride`/ISO-build path that bakes the generic callback
  client. Because this configuration is baked, a static-network deployment
  needs its own per-host or per-site ISO variant (it cannot ride the shared
  ISO's per-host data files), which is called out as a cost of DHCP-less
  operation.

The supported live-ISO network path (whichever mode) is exercised by the same
version-pinned integration test, so "the host can reach the hub" is verified,
not assumed.

#### 2. HTTP Callback Endpoint

The IBI Operator's image server (`internal/imageserver/`) is extended with
a callback handler:

```go
// POST /callbacks/{namespace}/{name}/status
// The bearer token is carried ONLY in the Authorization header; the
// endpoint accepts no ?token= query parameter (see auth notes below).
type CallbackPayload struct {
    Attempt string `json:"attempt"` // must equal status.preProvisioningAttempt
    Status  string `json:"status"`  // "success" or "failure"
    Message string `json:"message"` // detail message
}
```

On receiving a callback:

1. Look up the `ImageClusterInstall` by namespace/name
2. Verify the ICI has `spec.preProvisioning` set and is in the
   `PreProvisioningBooting` or `PreProvisioningInProgress` state
3. Verify the payload `attempt` equals `status.preProvisioningAttempt` (and
   that the bearer token is the one minted for that attempt); reject with
   409/401 otherwise. This drops late callbacks from a superseded boot.
4. Reject if `status.preProvisioningResult` for this attempt is **already
   set** — only the **first** terminal callback wins.
5. Store the result in `status.preProvisioningResult` (with its `attempt`)
   using an **atomic compare-and-swap on the ICI `resourceVersion`** (a
   status update with the read `resourceVersion` set as precondition). If the
   update is rejected with a conflict, re-read and re-apply step 4: a
   concurrent callback (e.g. duplicate retries from the host, or a
   success/failure race) that already recorded a terminal result causes this
   one to be dropped with 409. This guarantees exactly one terminal result is
   recorded even under concurrent POSTs, rather than a last-writer-wins
   overwrite.
6. Trigger a reconciliation of the ICI (the reconciler picks up the
   result and proceeds with the BMH state transition)

The endpoint is authenticated using a **per-attempt** bearer token
generated by the IBI Operator when the pre-provisioning phase starts. The
token is presented in an HTTP `Authorization: Bearer <token>` header (not
in the URL as a query parameter or path segment), so it does not leak into
image-server access logs, proxy logs, or the ISO's cloud-init/journal
output. The token is stored only in a per-ICI Secret owned by the ICI
(not in `status`, which is world-readable to anyone with `get` on the ICI),
delivered to the server inside the ignition overlay, compared in constant
time on receipt, and invalidated once a terminal callback is accepted or
the attempt times out. A fresh token is minted on every boot attempt so a
token captured from an earlier attempt cannot drive a later one. Where the
deployment can support it, mutual TLS (a client certificate provisioned
into the ignition overlay) is preferred over the bearer token. See
[Challenge 3](#3-callback-authentication-and-security).

#### 3. BMH Image Configuration for ISO Boot

The IBI Operator sets `spec.image` on the BMH to trigger Ironic's virtual
media boot:

```go
patch := client.MergeFrom(bmh.DeepCopy())
bmh.Spec.Image = &bmh_v1alpha1.Image{
    URL:        preProvisioning.ISOURL,
    DiskFormat: pointer.String("live-iso"),
    // Populated from PreProvisioningConfig.ISODigest when set.
    Checksum:     preProvisioning.ISODigest, // "sha256:<hex>"
    ChecksumType: bmh_v1alpha1.SHA256,
}
bmh.Spec.Online = true
err := r.Patch(ctx, bmh, patch)
```

The `live-iso` format instructs Ironic to mount the ISO via BMC virtual
media (Redfish `VirtualMedia.InsertMedia`) and boot from it. The IBI
preparation service within the ISO handles the actual disk installation.

**Binding the boot to an immutable artifact.** When
`PreProvisioningConfig.ISODigest` is set, the operator ensures the ISO
actually booted is the exact artifact that was built and verified — a
mutable HTTPS URL alone can be swapped for a stale or tampered ISO between
build and boot. Because Ironic's `live-iso` path does **not** reliably
enforce the BMH `Image.Checksum` the way the normal deploy path does, the
operator does not depend solely on populating `Checksum` above: before
setting `spec.image`, it **streams the ISO bytes from `ISOURL`** over an HTTPS
channel and computes the SHA-256 **over those bytes**, comparing the result to
`ISODigest` and failing closed with `PreProvisioningConfigInvalid` on mismatch.
A published `.sha256` **sidecar is explicitly not trusted** as the verification
source: comparing sidecar text to `ISODigest` only checks that two pieces of
metadata agree, not that `ISOURL` actually returns those bytes — a stale or
mispublished sidecar would pass while the URL serves an ISO that was never
hashed. The bytes the operator will hand to the BMC are the bytes that must be
hashed. This digest is the same value the seed-generation workflow records in
`SeedGenerationStatus.ISODigest`, so the two proposals pin to one artifact
end to end.

**Restricting `ISOURL` before the operator fetches it.** Because `isoURL` is
overridable through `hwMgmtParameters` on a `ProvisioningRequest`, an untrusted
caller could otherwise point the operator's pre-fetch at an arbitrary HTTPS
endpoint. Requiring HTTPS is not sufficient: a valid HTTPS URL can still name an
internal service, making the operator's fetch a server-side request forgery
(SSRF) vector. The operator therefore enforces two checks, **both before any
byte of `ISOURL` is fetched**, failing closed with `PreProvisioningConfigInvalid`:

- **Origin allowlist.** `ISOURL` must resolve to an operator-owned or
  explicitly allowlisted ISO origin (the origins configured for the
  seed-generation upload target, plus any operator-configured additions). A URL
  outside that set is rejected before the fetch — the operator never issues a
  request to a caller-chosen host. The allowlist is operator configuration, not
  template or `ProvisioningRequest` input, so a request author cannot widen it.
- **Mandatory digest for automated boot.** `ISODigest` is **required** whenever
  the operator drives an automated boot from a caller-influenceable `ISOURL`;
  URL-only trust is not accepted for that path. A missing digest is rejected up
  front rather than silently falling back to URL-only trust, so the bytes handed
  to the BMC are always pinned to a verified artifact.

**The operator's pre-fetch is necessary but not sufficient on its own — the
`ISOURL` must also be immutable.** There is an unavoidable time-of-check /
time-of-use gap: the operator verifies the bytes at `ISOURL` *before* setting
`spec.image`, but the BMC (via Ironic) fetches the bytes *later*, and BMO's
`live-iso` path passes only `imageData.URL` as the Ironic `boot_iso` while
clearing the checksum fields — so nothing in the boot path re-binds what the
BMC actually pulls. If the content at `ISOURL` changes after the operator's
check, the BMC can boot different bytes than were verified. The design
therefore **requires `ISOURL` to be an immutable, content-addressed reference**
(a per-run URL that never has its bytes rewritten), not a mutable "latest"
URL. The seed-generation workflow already satisfies this: it uploads to a
**unique per-run path** (ProvisioningRequest name + seed image digest +
filename) that is written once and never overwritten, so the URL effectively
addresses one immutable artifact. Immutability of `ISOURL` is a
**producer-side contract** (the seed workflow guarantees it; a generic webhook
cannot reliably prove a URL is immutable), and the operator's pre-fetch
verification then confirms that immutable artifact matches `ISODigest`.
Immutability closes the TOCTOU window; the pre-fetch confirms identity. Because
`ISODigest` is mandatory for automated boot (above), the operator always
verifies identity at check time; what a non-immutable `ISOURL` costs is the
guarantee that the BMC's *later* fetch pulls those same verified bytes. A
deployment that cannot guarantee an immutable, allowlisted URL therefore cannot
offer automated IBI pre-provisioning through this path — it is rejected up
front, not silently downgraded to URL-only trust.

The HTTPS client used for this fetch trusts the **same private CA as the
boot**: when `ISOServerCACertRef` is set, the operator reads the
`ca-bundle.crt` key from that ConfigMap (resolved in the ICI's namespace,
requiring `get` on `configmaps` there) and uses it as the root pool for the
verification request; otherwise it uses the system trust store. Without this,
digest verification against a private-CA ISO server would fail the TLS
handshake before the BMH ever boots. This is distinct from the BMC's own
trust of the ISO server (a pre-installed prerequisite, out of scope here) —
`ISOServerCACertRef` configures the *operator's* client, not the BMC's.

#### 4. BMH State Transition After Pre-Provisioning

When the callback reports success:

```go
patch := client.MergeFrom(bmh.DeepCopy())
bmh.Spec.Image = nil                    // clear ISO boot
bmh.Spec.ExternallyProvisioned = true   // mark as pre-provisioned
bmh.Spec.Online = false                 // power off until cluster install
err := r.Patch(ctx, bmh, patch)
```

The BMH is now in the same state it would be if it had been manually
pre-provisioned. The IBI Operator's existing `configureHost` flow proceeds
unchanged — it creates the config DataImage, sets the reboot annotation,
and monitors cluster installation.

#### 5. Condition Reporting

Pre-provisioning progress is reported via the ICI `RequirementsMet`
condition (already used for pending states) — the **single** authoritative
condition type for this handoff (see above); the phase is carried in the
`reason`, not a separate condition type:

| Phase | Condition | Reason | Terminal? |
|---|---|---|---|
| Booting from ISO | `RequirementsMet = False` | `PreProvisioningBooting` | No |
| Waiting for callback | `RequirementsMet = False` | `PreProvisioningInProgress` | No |
| Attempt failed/timed out, retry budget remains | `RequirementsMet = False` | `PreProvisioningRetrying` | No (backoff, then re-boot) |
| Callback received (success) | `RequirementsMet = True` | `PreProvisioningSucceeded` | Yes |
| Failure with retry budget exhausted | `RequirementsMet = False` | `PreProvisioningFailed` | Yes |
| Timeout with retry budget exhausted | `RequirementsMet = False` | `PreProvisioningTimedOut` | Yes |

`PreProvisioningRetrying` is a **non-terminal** state: while it is set the
`ProvisioningRequest` keeps progressing (it does **not** map to `failed`).
`PreProvisioningFailed`/`PreProvisioningTimedOut` are emitted **only** once the
retry budget is exhausted, so the terminal-failure contract is never entered
while a retry is still owed.

The O-Cloud Manager must map these ICI conditions into
`ProvisioningRequest` status; this is **not** covered by existing monitoring
(see [O-Cloud Manager Changes](#o-cloud-manager-changes) for the required
handoff — the PR controller today watches only a fixed set of
`ClusterInstance` conditions and would otherwise ignore pre-provisioning
state entirely).

#### 6. Timeout Handling

The reconciler checks `Status.PreProvisioningBootTime` against the
configured `Timeout` (default: 60m) on each reconciliation.

**The timeout must be self-triggering.** A callback is the only external
event that wakes the reconciler; if no callback ever arrives, nothing would
re-enqueue the ICI and it would remain `PreProvisioningInProgress` forever.
While an attempt is in flight, every reconcile therefore returns
`RequeueAfter` set to the remaining time until
`PreProvisioningBootTime + Timeout` (bounded to a minimum floor so clock
skew cannot produce a zero/negative delay and a tight requeue loop). This
guarantees a reconcile fires at — or just after — the deadline even with no
callback, no BMH change, and no other watched event. The no-callback path
is explicitly covered by an envtest that advances the fake clock past the
deadline and asserts the transition to `PreProvisioningTimedOut`.

**Timeout and callback compete for the same terminal state.** The timeout
teardown uses the **same resourceVersion compare-and-swap** on
`preProvisioningResult` as the callback handler (see the callback flow
above): it re-reads the ICI, and only if the attempt is still non-terminal
does it write the terminal result, guarded by `resourceVersion`. A callback
that lands in the same instant either wins the CAS (the timeout write then
gets a conflict and is dropped) or loses it (the timeout wins and the late
callback is rejected against the invalidated token). Exactly one of the two
paths becomes the terminal writer, and **only that winner performs the
teardown** below — the loser observes the already-terminal state and does
nothing. This prevents double teardown and a torn state where, e.g., the
token is invalidated by timeout while the callback is concurrently accepted.

The winning timeout path then performs teardown. Because teardown spans
several resources (ICI status, BMH spec, DataImage, token Secret), it must be
**restart-safe**: a crash partway through must not leave a terminal ICI with
live cleanup still owed. Two rules make it safe:

- **Claiming the terminal state and *finishing* teardown are distinct.**
  Winning the CAS records the terminal **reason** (`PreProvisioningTimedOut`)
  *and* a `PreProvisioningTeardownComplete = false` marker in the same write.
  The condition is reported as terminal, but the reconciler treats the attempt
  as **not done** until the marker flips to `true`.
- **Teardown is idempotent and re-driven until every action succeeds.** While
  `PreProvisioningTeardownComplete` is `false`, each reconcile re-runs all
  teardown actions (each a no-op if already applied) and only sets the marker
  `true` once every one is confirmed:
  1. Clear the ISO image (`spec.image = nil`)
  2. Remove the per-attempt pre-provisioning DataImage
  3. Power off the BMH (`spec.online = false`)
  4. Invalidate/delete the per-attempt callback token Secret
  5. Set `PreProvisioningTeardownComplete = true` (the **only** state that
     stops teardown retries)

Only step 5 stops the retries; a failure in any of steps 1-4 leaves the marker
`false` and the next reconcile resumes teardown. This is the **same teardown as
the failure-callback path** (see the flow above), which uses the same
marker-gated loop: a completed teardown leaves no live ISO, no DataImage
carrier, and no usable token behind, so a late or replayed callback for the
abandoned attempt cannot be accepted. A **fault-injection test** kills the
reconcile after the terminal claim but before teardown completes and asserts
the next reconcile drives cleanup to completion. The ProvisioningRequest fails,
and the server can be manually investigated.

## O-Cloud Manager Changes

The O-Cloud Manager changes are minimal:

1. **Configuration passthrough**: Read `ibiPreProvisioning` from
   `hwMgmtDefaults` / `hwMgmtParameters` and populate the
   `ImageClusterInstall.spec.preProvisioning` fields. The **preferred** path
   is for SiteConfig to render `preProvisioning` into the ICI at creation
   time, so the field is present before the IBI Operator ever reconciles the
   ICI. If instead the field must be patched onto an ICI that SiteConfig
   already created, the patch introduces a race: the IBI Operator could
   reconcile the ICI (and begin a normal, non-pre-provisioning install)
   before the patch lands. The design therefore requires that when the
   patch-after-create path is used, IBI reconciliation is **gated** until
   `preProvisioning` is present — e.g. the ICI is created paused/annotated and
   the O-Cloud Manager clears the gate only after the patch is applied — so
   the operator never observes a pre-provisioning ICI without its
   `preProvisioning` spec.
2. **Validation**: Verify ConfigMaps referenced by `ibiPreProvisioning`
   exist and are valid during ClusterTemplate validation.
3. **Status handoff (new controller logic)**: define how the ICI's
   pre-provisioning status maps onto `ProvisioningRequest` conditions and
   `provisioningStatus`. This is a real gap, not a no-op: the
   ProvisioningRequest controller today gates cluster-install progress on a
   fixed set of `ClusterInstance` conditions
   (`ClusterInstanceValidated`, `RenderedTemplates`,
   `RenderedTemplatesValidated`, `RenderedTemplatesApplied`) and does **not**
   look at `ImageClusterInstall` status. Without an explicit handoff the
   request could advance to (or report) cluster installation while
   pre-provisioning is still pending or has failed. The controller must
   therefore, **before** it treats hardware as ready, block on the ICI
   pre-provisioning state and translate it as follows:

   | ICI pre-provisioning reason | PR `HardwareProvisioned` condition | PR `provisioningStatus.provisioningPhase` |
   | --- | --- | --- |
   | `PreProvisioningBooting` / `PreProvisioningInProgress` | `False`, reason `InProgress` | `progressing` |
   | `PreProvisioningRetrying` | `False`, reason `InProgress` | `progressing` (attempt failed, backing off to retry) |
   | `PreProvisioningSucceeded` | `True`, reason `Completed` | `progressing` (proceed to cluster install) |
   | `PreProvisioningFailed` | `False`, reason `Failed` | `failed` |
   | `PreProvisioningTimedOut` | `False`, reason `TimedOut` | `failed` |

   Pre-provisioning is modelled as part of the hardware-provisioning gate:
   the PR does not proceed to (or report) cluster installation until the ICI
   reports `PreProvisioningSucceeded`. Crucially, `PreProvisioningRetrying` is
   **non-terminal** — the PR keeps `progressing` across a failed attempt's
   backoff, so the configured `MaxRetries`/`RetryBackoff` budget can actually
   be used without the PR prematurely latching `failed`. Only
   `PreProvisioningFailed`/`PreProvisioningTimedOut` (emitted after the retry
   budget is exhausted) drive the PR to a terminal `failed` `provisioningStatus`
   with the ICI message surfaced. `MaxRetries` and `RetryBackoff` are carried
   in `ibiPreProvisioning` (the same `hwMgmtDefaults`/`hwMgmtParameters`
   passthrough as the other fields, and present in the ClusterTemplate schema).
   This mapping is the contract between the two controllers and must be covered
   by an integration test, including the retry path.
4. **Watch `ImageClusterInstall` to enqueue the owning
   `ProvisioningRequest`.** The mapping in item 3 only runs when the PR
   reconciles, and pre-provisioning transitions (callback success/failure,
   timeout, retry) update **`ImageClusterInstall` status and nothing else**.
   The `ProvisioningRequestReconciler` today watches `NodeAllocationRequest`
   and `ClusterInstance` but **not** `ImageClusterInstall`, so without a new
   watch a callback- or timeout-driven ICI status change would not wake the PR,
   and the request would sit stale — never advancing on success nor reporting
   failure — until some unrelated event happened to requeue it. The reconciler
   therefore adds a watch on `ImageClusterInstall` with a mapper that enqueues
   the owning `ProvisioningRequest` (resolved via the ICI→ClusterInstance→PR
   ownership chain, or a field index on the PR name). Consistent with **DD-002**,
   the enqueued `NamespacedName` omits the namespace because
   `ProvisioningRequest` is cluster-scoped. An integration test asserts that a
   callback-only and a timeout-only ICI status change — with no NAR or
   ClusterInstance change — each wake the PR and drive the item-3 transition.

No hub-side Jobs and no SSH infrastructure are introduced, but the status
handoff above is new controller logic on the O-Cloud Manager side.

## Integration with Seed Generation Proposal

When both proposals are implemented, the full automated pipeline is:

```text
ProvisioningRequest (seed gen CT)     ProvisioningRequest (IBI CT)
  ├─ Phase 1: Validation               ├─ Pre-provisioning (NAR)
  ├─ Phase 2: ACM Cleanup              ├─ Hardware provisioning
  ├─ Phase 3: Seed Generation          ├─ Cluster installation (IBI):
  ├─ Phase 4: ISO Generation           │    ├─ ICI created with preProvisioning
  ├─ Phase 5: Completion + Cleanup     │    ├─ IBI Operator: boot from ISO
  └─ Status: seedImage + isoURL        │    ├─ IBI Operator: receive callback
                                        │    ├─ IBI Operator: transition BMH
                                        │    ├─ IBI Operator: config + install
                                        │    └─ Cluster provisioned
                                        └─ Post-provisioning (policies)
```

## Sample: ClusterTemplate with IBI Pre-Provisioning

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: ClusterTemplate
metadata:
  name: sno-ran-du-ibi.v4-Y-Z-1
  namespace: sno-ran-du-v4-Y-Z
spec:
  name: sno-ran-du-ibi
  version: v4-Y-Z-1
  release: 4.Y.Z
  templateDefaults:
    hwMgmtDefaults:
      hardwareProvisioningTimeout: "120m"
      nodeGroupData:
        - name: controller
          role: master
          resourceSelector:
            resourceselector.clcm.openshift.io/server-type: XR8620t
      ibiPreProvisioning:
        isoURL: https://iso-server.example.com/ibi/rhcos-ibi-4.Y.Z.iso
        isoDigest: sha256:0000000000000000000000000000000000000000000000000000000000000000
        isoServerCACertRef:
          name: iso-server-ca-cert
        preProvisioningTimeout: "60m"
    clusterInstanceDefaults: clusterinstance-defaults-ibi-v1
    policyTemplateDefaults: policytemplate-defaults-v1
  templateParameterSchema:
    properties:
      # ... standard provisioning parameters ...
      hwMgmtParameters:
        properties:
          ibiPreProvisioning:
            type: object
            properties:
              isoURL:
                type: string
                minLength: 1
              isoDigest:
                type: string
                pattern: '^sha256:[a-f0-9]{64}$'
              isoServerCACertRef:
                type: object
                properties:
                  name:
                    type: string
                required: [name]
              preProvisioningTimeout:
                type: string
            required: [isoURL]
        type: object
    type: object
```

## Challenges and Mitigations

### 1. Callback URL must be reachable from the pre-provisioned server

**Challenge**: The server running the IBI preparation service must be able
to reach the IBI Operator's HTTP endpoint on the hub cluster. In
disconnected environments, the server may be on a different network
(provisioning/BMC network) than the hub's service network.

**Mitigation**: The IBI Operator's image server is already exposed via a
Route or NodePort for serving config ISOs to BMHs. The same network path
that allows BMH/Ironic to fetch ISOs from the hub can be used for the
callback. The callback URL uses the same externally-reachable hostname. If
the provisioning network is isolated, a Route or LoadBalancer service
exposes the callback endpoint — this is the same requirement that exists
for DataImage ISO serving.

This reachability is an explicit **precondition**, not an assumption. Note
that BMH/Ironic fetches ISOs from the hub's *service* network via the BMC,
whereas the callback originates from the *booted host* on the provisioning
network — these can differ, so "Ironic can pull the ISO" does not by itself
prove "the host can reach the callback." Before booting the ISO, the IBI
Operator validates that the resolved callback endpoint is DNS-resolvable and
its hostname is one the target network can route to, and that its TLS
certificate chains to a CA the host will trust (the same bundle injected via
`additionalTrustBundle`, see [Challenge 7](#7-hub-ca-trust-on-the-pre-provisioned-server)).
If these checks fail, the ICI reports a configuration error up front rather
than booting a server that can never call back. A `PreProvisioningTimedOut`
is documented as ambiguous between "preparation never finished" and
"callback could not be delivered," and the operator surfaces both
possibilities.

### 2. Callback delivery reliability

**Challenge**: The callback `curl` command may fail due to transient
network issues, DNS resolution delays during boot, or the hub being
temporarily unavailable. A missed callback would leave the ICI stuck in
`PreProvisioningInProgress` until timeout.

**Mitigation**: The `ibi-prep-callback` script retries curl against an
**absolute wall-clock deadline derived from `CALLBACK_BUDGET_SECS`** (a loop
with a per-attempt `--max-time`), so cumulative retry effort is bounded
regardless of how many individual attempts fail — something `TimeoutStartSec`
(per single start) and `Restart=on-failure` (unbounded relaunches) cannot
guarantee on their own. `CALLBACK_BUDGET_SECS` is the per-host budget delivered
on the data carrier, derived by the operator from
`PreProvisioningConfig.Timeout` (default 60m) anchored to `PreProvisioningBootTime`,
so the host's retry window always tracks the operator's timeout rather than a
fixed value that could expire before it (a hard-coded 10m deadline, for
instance, would give up ~50m early on a default-timeout host and mark a prepared
server as timed out). The script fails closed if `CALLBACK_BUDGET_SECS` is
absent rather than substituting a default. `Restart=on-failure` plus
`StartLimitIntervalSec`/`StartLimitBurst` remain only as a bounded backstop for
an outright script crash. This covers
transient network issues during boot. Additionally, the IBI Operator's
reconcile loop checks the timeout — if no callback is received within the
configured timeout, the ICI transitions to `PreProvisioningTimedOut`. The
operator can distinguish "callback never arrived" (timeout) from
"preparation failed" (failure callback).

### 3. Callback authentication and security

**Challenge**: The callback endpoint is publicly reachable (same as the
image server). An attacker could send a fake success callback, causing the
IBI Operator to proceed with cluster installation on an unprepared server.

**Mitigation**: The IBI Operator generates a **per-attempt** authentication
token (cryptographically random, or an HMAC over the ICI identity + attempt
marker) when each pre-provisioning boot starts. Key properties:

- **Header, not URL**: the token is sent in an `Authorization: Bearer`
  header and verified with a constant-time comparison. It is never placed
  in the URL (query parameter or path segment), where it would be captured
  by image-server logs, intermediary proxies, and the server's own journal.
- **Secret, not status**: the token is stored in a per-ICI Secret owned by
  the ICI, not in `status.preProvisioningResult` or any status field —
  status is readable by any principal with `get` on the ICI, which would
  expose the shared secret needed to forge a success callback.
- **Per-attempt and expiring**: a new token is minted on every boot attempt
  and invalidated as soon as a terminal callback is accepted or the attempt
  times out, so a token observed during one attempt cannot authorize a
  callback for a later attempt (see also [Challenge 6](#6-server-state-after-failed-pre-provisioning)
  on per-attempt binding).
- **State check as defense in depth**: the handler additionally requires the
  ICI to be in `PreProvisioningBooting` / `PreProvisioningInProgress` and
  the token to match the *current* attempt before accepting the callback.
- **mTLS preferred where available**: provisioning a short-lived client
  certificate into the ignition overlay and requiring mutual TLS at the
  endpoint is stronger than a bearer token and is the recommended option
  when the environment supports it.

### 4. Callback URL injection into the ISO

**Challenge**: The callback URL is ICI-specific (contains namespace and
name). If the ISO is built once and reused across multiple servers, the
callback URL cannot be baked into the ISO at build time.

**Mitigation**: The design must account for the fact that, for a
`live-iso` boot, BMO does **not** merge `DataImage` contents (or
`userData`/`networkData`/`metaData`) into the live environment's Ignition —
only the ISO's baked-in Ignition (`ignitionConfigOverride`) is reliably
applied (see [ISO ignition delivery](#1-callback-ignition-override)). So a
DataImage cannot be used to *inject* the callback systemd unit into a shared
ISO. Two viable options remain:

- **Generic client baked in + per-server data read at boot (recommended
  when validated).** The ISO carries, via `ignitionConfigOverride`, the
  generic callback client (the `ibi-prep-callback` script and its
  `ibi-prep-callback.service` unit — identical for every server). The
  per-server data (`/etc/ibi-callback/env` and the root-only
  `/etc/ibi-callback/auth.cfg`) is delivered at boot through a concrete,
  specified carrier rather than "a well-known location":

  - **Carrier**: a per-ICI `DataImage` (a tiny ISO9660/FAT image the IBI
    Operator builds and attaches as an additional virtual-media device),
    used **purely as a data device**, not as an ignition overlay. The
    `DataImage` resource is **named and namespaced after the BareMetalHost**
    (BMO v0.13.2's `DataImageReconciler` resolves the owning host by matching
    the request name/namespace to a `BareMetalHost`, so a differently named
    carrier never attaches); the attempt id it carries is bound through the
    carrier content and an `ibi.openshift.io/attempt` label, not the resource
    name. Its filesystem is labelled `ibi-callback` so it is addressable by
    label regardless of device enumeration order. An integration test verifies
    BMO attaches the image and the guest sees the `ibi-callback` device label.
  - **Mount + materialize**: a baked-in `ibi-callback-data.service`
    (`Type=oneshot` **with `RemainAfterExit=yes`**) mounts
    `/dev/disk/by-label/ibi-callback` read-only, copies `env` to
    `/etc/ibi-callback/env` (0644) and `auth.cfg` to
    `/etc/ibi-callback/auth.cfg` (0600, root-only), then unmounts. Copying
    to a root-only path — rather than reading the token off the shared
    virtual-media device directly — keeps the 0600 permission guarantee.
    `RemainAfterExit=yes` is load-bearing: without it a `Type=oneshot` unit
    goes `inactive` the moment `ExecStart` returns, and because the reporter
    declares `Requires=` on it, systemd would consider the dependency no longer
    active and could stop the reporter before it runs. With
    `RemainAfterExit=yes` the data unit stays `active (exited)` after
    materialization, so the `Requires=`/`After=` pair holds for the whole boot.
  - **Ordering and dependency (runtime, not `[Install]`)**: the dependency
    must be expressed as a **runtime** relationship in the reporter's
    `[Unit]` section — `ibi-prep-callback.service` declares
    `Requires=ibi-callback-data.service` and
    `After=ibi-callback-data.service`. It is a mistake to rely on
    `RequiredBy=`/`WantedBy=` in the *data* unit's `[Install]` section for
    this: `[Install]` directives take effect only when the unit is
    `enable`d, so if enablement is ever missed the reporter would run before
    (or without) its data files. Both units are additionally marked
    `enabled: true` in the ignition so they are active on first boot, but the
    correctness guarantee comes from the runtime `Requires=`/`After=`, not
    from enablement. With `Requires=`, if the data mount fails (device
    absent) the reporter is not started, surfacing as a timeout rather than a
    callback with an empty URL/token. This enablement-plus-runtime-dependency
    behavior must be covered by a systemd integration test that asserts the data
    unit stays `active (exited)` after materialization and the reporter then
    actually runs (not just that ordering is declared).

  This keeps the ISO reusable. It depends on version-specific `live-iso` +
  DataImage behavior in BMO/Ironic and therefore **must be backed by a
  version-pinned integration test**.
- **Unique per-ICI ISO (fallback).** If the generic-client path cannot be
  validated on the supported BMO/Ironic version, the IBI Operator (or the
  ISO build step) bakes the ICI-specific callback URL and token directly
  into `ignitionConfigOverride`, producing one ISO per ICI. This is simpler
  and robust but sacrifices ISO reuse.

Using DataImage as an ignition overlay is not viable: BMO does not merge
DataImage contents into live-iso Ignition, so a per-ICI callback URL and
token cannot be injected that way.

### 5. BMC CA trust for HTTPS virtual media

**Challenge**: When the ISO server uses HTTPS with a private CA, the BMC
must trust the CA certificate to fetch the ISO via virtual media. BMC CA
trust configuration is vendor-specific.

**Mitigation**: The `isoServerCACertRef` provides the CA cert, but supplying
the cert is not the same as establishing BMC trust, and that trust must
exist **before** `spec.image` is set (Ironic mounts the virtual media
immediately). The design defines two concrete, ordered paths rather than
leaving the flow implicit:

- **Runtime injection (only where actually supported).** For BMCs whose
  Redfish/vendor API exposes a certificate-management endpoint, trust can be
  established at runtime **before** the virtual-media insert. This is *not*
  something the IBI Operator implements generically here — BMO/Ironic and
  the BMC driver own certificate handling, and support is vendor- and
  firmware-specific. The IBI Operator relies on the underlying stack's
  existing capability and does not attempt ad-hoc BMC certificate pushes.
- **Pre-installed trust (the portable default).** Where runtime injection is
  not available or not verifiable, the private CA must already be present in
  the BMC trust store. This is documented as an explicit **prerequisite**,
  and pre-provisioning does not silently proceed assuming HTTPS will work.

There is **no portable contract** — across BMO, Ironic, and vendor BMC
drivers — by which the IBI Operator can query whether a given BMC already
trusts a given CA. Neither the `BareMetalHost` status, the Ironic node, nor
any capability annotation exposes the BMC's virtual-media trust store, and
Redfish certificate-management support is optional and vendor-specific. The
design therefore does **not** claim the operator validates BMC trust before
booting; asserting a precondition it cannot actually check would be false
assurance. Instead:

- **Pre-installed BMC trust is the single documented prerequisite** when the
  ISO server uses a private CA. Establishing that trust (via the BMC's own
  management interface, out of band) is the integrator's responsibility and
  is stated as an explicit prerequisite of enabling pre-provisioning with an
  HTTPS+private-CA ISO server.
- If runtime injection *is* available on a given stack (the "Runtime
  injection" path above), it is handled entirely by BMO/Ironic/the BMC
  driver — outside this design — and remains subject to the same
  prerequisite from the IBI Operator's perspective.

Consequently, a BMC that does not trust the CA manifests as the ISO fetch
failing and the flow reaching `PreProvisioningTimedOut`; the timeout message
explicitly names "BMC could not fetch the ISO — verify the ISO server CA is
present in the BMC trust store" so the failure is diagnosable rather than
mysterious. The corresponding
[seed-generation proposal](./automated-seed-image-generation.md) surfaces the
ISO server CA in status precisely so this reference is available to whoever
configures BMC trust out of band.

### 6. Server state after failed pre-provisioning

**Challenge**: If pre-provisioning fails mid-way, the server may be in an
inconsistent state (partial RHCOS installation, incomplete seed image pull).

**Mitigation**: The IBI preparation is idempotent — rebooting from the ISO
restarts the process. On failure callback (or timeout), the IBI Operator
first tears the attempt down cleanly (clears `spec.image`, removes the
per-attempt data carrier, powers the BMH off, invalidates the per-attempt
token — the host is left **not** `externallyProvisioned`), records the
retry-pending marker (`Status.RetryNotBefore`) while **clearing** the retired
`PreProvisioningAttempt` so the invalidated token is never re-driven, then can
retry by re-triggering the ISO boot with a **fresh attempt**: a new per-attempt
token and marker, and `Status.PreProvisioningBootTime` reset so the per-attempt
timeout is measured from the new boot.

**Retry is explicitly bounded — there is no infinite retry and no retry
after a terminal result.** `PreProvisioningConfig` carries the retry policy
so the operator does not have to invent unbounded behavior:

- `MaxRetries` (default: `0`, i.e. a single attempt) caps the number of
  additional attempts after the first. Retries stop as soon as this budget
  is exhausted.
- `RetryBackoff` (default: `5m`) is the delay between a failed attempt's
  teardown and the next boot. It is persisted durably as
  `Status.RetryNotBefore` (= failure time + `RetryBackoff`) in the same update
  that clears the retired `PreProvisioningAttempt`, and also applied as a
  `RequeueAfter` so the retry is self-triggering. The `RequeueAfter` is only a
  wake-up hint; `RetryNotBefore` is the source of truth, so an operator restart
  that loses the in-memory timer still waits out the remaining backoff and never
  re-drives the retired attempt.
- The per-attempt `Timeout` bounds each individual boot; the operator also
  enforces an overall ceiling of
  `(MaxRetries + 1) × Timeout + MaxRetries × RetryBackoff` so total retry
  effort is finite even in the worst case.

**A failed attempt with budget remaining is a distinct, non-terminal state —
`PreProvisioningFailed`/`PreProvisioningTimedOut` are *not* recorded until the
budget is exhausted.** This matters because those two reasons are terminal and
map the `ProvisioningRequest` to `failed`; recording them on a failed attempt
that still owes a retry would make the configured budget unusable (the PR would
latch `failed` before the retry ever ran). Instead, a failed/timed-out attempt
with `PreProvisioningRetryCount < MaxRetries` transitions to the non-terminal
`PreProvisioningRetrying` reason, the PR keeps `progressing`, and the operator
re-boots a fresh attempt after `RetryBackoff`. Only when the budget is
exhausted does the operator write the terminal `PreProvisioningFailed` (or
`PreProvisioningTimedOut`) result — via the compare-and-swap above — and let
the PR map to `failed`.

`Status.PreProvisioningRetryCount` records attempts consumed so the budget
survives operator restarts. A retry is scheduled **only** from the
non-terminal `PreProvisioningRetrying` state (reached only after a clean
teardown) and **only** while the budget remains; once a terminal result
(`PreProvisioningSucceeded` / `PreProvisioningFailed` /
`PreProvisioningTimedOut`) has been written via the compare-and-swap above, no
further attempt is booted.

Each attempt is explicitly bound so a stale signal cannot be mistaken for
the current one:

- The callback carries the **current attempt's** token; the handler rejects
  a callback whose token does not match the active attempt, so a late
  callback from a prior boot cannot complete a new attempt.
- The on-host reporter reads the prep service's result for the **current
  boot only** (see [the ignition unit](#1-callback-ignition-override)), so a
  "finished successfully" record left on disk from an earlier attempt does
  not produce a false success.

Once the last attempt's timeout is exceeded and the `MaxRetries` budget is
exhausted (see above), the operator reports `PreProvisioningTimedOut` and
the ProvisioningRequest fails. The server can be manually investigated or
released for re-allocation.

### 7. Hub CA trust on the pre-provisioned server

**Challenge**: The callback URL uses HTTPS (the IBI Operator's image server
is TLS-enabled). The pre-provisioned server needs to trust the hub's TLS
certificate to send the callback.

**Mitigation**: The callback systemd unit uses
`--cacert /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem` which
includes the system trust bundle. If the hub uses a private CA, the CA cert
must be included in the ISO's `additionalTrustBundle` field (already
supported by `ImageBasedInstallationConfig`). This is the same requirement
that exists for the server to pull images from the hub's registries.

## Required Upstream Changes (IBI Operator)

### Summary

The following changes are required in the `stolostron/image-based-install-operator`
repository:

### 1. API Types (`api/v1alpha1/imageclusterinstall_types.go`)

- Add `PreProvisioning *PreProvisioningConfig` to `ImageClusterInstallSpec`
- Add `PreProvisioningConfig` struct with fields: `ISOURL`, `ISODigest`,
  `ISOServerCACertRef`, `Timeout`, `CallbackBaseURL`, `MaxRetries`,
  `RetryBackoff`
- Add `PreProvisioningBootTime *metav1.Time` to `ImageClusterInstallStatus`
  (written only after the BMH boot request has been applied — never in the
  pre-boot status update that records a new `PreProvisioningAttempt`)
- Add `PreProvisioningRetryCount int` to `ImageClusterInstallStatus`
- Add `RetryNotBefore *metav1.Time` to `ImageClusterInstallStatus`: durable
  retry-pending marker set (now + `RetryBackoff`) in the same update that clears
  `PreProvisioningAttempt` on a budgeted failure, so recovery distinguishes a
  retired attempt in backoff from an in-flight one; the remaining backoff is
  recomputed from it after a restart. Cleared when the next attempt is recorded
  (never set at the same time as `PreProvisioningAttempt`)
- Add `PreProvisioningResult` struct to `ImageClusterInstallStatus`
- Add `PreProvisioningTeardownComplete bool` to `ImageClusterInstallStatus`:
  gates cessation of teardown retries for a terminal attempt. Written `false`
  in the same update that claims a terminal reason; flipped to `true` only
  once every teardown action (clear image, remove DataImage, power off,
  delete the per-attempt token Secret) has succeeded. Reset to `false` (or
  cleared) when a retry mints a fresh attempt; meaningful only once a
  terminal reason is set
- Add condition reason constants: `PreProvisioningPendingReason`,
  `PreProvisioningBootingReason`, `PreProvisioningInProgressReason`,
  `PreProvisioningRetryingReason`, `PreProvisioningSucceededReason`,
  `PreProvisioningFailedReason`, `PreProvisioningTimedOutReason`

### 2. Controller (`controllers/imageclusterinstall_controller.go`)

- Add `preProvisionHost()` method:
  - Generates the callback ignition override with a fresh per-attempt auth
    token, stored in a per-ICI Secret (not in status)
  - Delivers the per-server callback URL + token to the generic client baked
    into the ISO (config-drive / data-carrier read at boot), or bakes a
    unique per-ICI ISO as the fallback — it does **not** rely on a DataImage
    as an ignition overlay (BMO does not merge DataImage contents into
    live-iso Ignition)
  - Patches BMH `spec.image` with the ISO URL and `live-iso` format
  - Sets BMH `spec.online = true`
  - Records `Status.PreProvisioningBootTime`
  - Sets `RequirementsMet` condition with `PreProvisioningBooting` reason

- Add `handlePreProvisioningCallback()` method:
  - Called when a callback is received via the HTTP handler
  - Validates the auth token
  - Stores the result in `Status.PreProvisioningResult`
  - Triggers ICI reconciliation

- Add `transitionToIBIReady()` method:
  - Clears BMH `spec.image`
  - Sets BMH `spec.externallyProvisioned = true` (only here, after a
    verified success callback — never in `validateBMH`/`validateHost`)
  - Sets BMH `spec.online = false`
  - Removes any per-attempt pre-provisioning data carrier and invalidates
    the per-attempt callback token
  - Sets condition with `PreProvisioningSucceeded` reason

- Modify the main `Reconcile()` flow to:
  - Insert the pre-provisioning phase before `configureHost()` when
    `spec.preProvisioning` is set
  - Check `Status.PreProvisioningResult` for callback results
  - Handle timeout when no callback is received

### 3. Image Server (`internal/imageserver/imageserver.go`)

- Add callback HTTP handler:
  - `POST /callbacks/{namespace}/{name}/status` (bearer token in the
    `Authorization` header only — no `?token=` query parameter, so the
    token never lands in access/proxy logs)
  - Parse `CallbackPayload` from request body
  - Look up ICI, verify the bearer token against the active attempt, verify
    `payload.attempt == status.preProvisioningAttempt`, store result
  - Trigger reconciliation via channel or annotation update

### 4. Ignition Package (`internal/ignition/` — new)

- New package for generating ignition config snippets, split along the
  **build-time vs per-attempt** boundary so per-attempt secrets never enter
  the shared, build-time ISO surface (see the recommended path in Challenge 4):
  - `GenerateCallbackClient() ([]byte, error)` — the **build-time**,
    attempt-independent surface baked into the shared ISO via
    `ignitionConfigOverride`: the `ibi-prep-callback.service` systemd unit,
    the `/usr/local/bin/ibi-prep-callback` script, and the generic callback
    client. Identical for every server, contains no URL/token/attempt.
  - `GenerateCallbackData(callbackURL, attempt, token string, budget time.Duration) ([]byte, error)`
    — the **per-attempt** surface delivered at boot through the `DataImage`
    carrier: the non-secret `/etc/ibi-callback/env` (`CALLBACK_URL`,
    `CALLBACK_ATTEMPT`, and **`CALLBACK_BUDGET_SECS`**) and the root-only
    `/etc/ibi-callback/auth.cfg` curl config carrying the bearer token.
    `budget` is the callback retry window, derived from the effective
    `PreProvisioningConfig.Timeout` minus a margin, so the host keeps resending
    for the whole window the operator waits (see below). It is a **required**
    part of the contract — the script does **not** silently fall back to a
    fixed budget when it is absent — and a unit test asserts the generated
    `env` file contains a non-empty `CALLBACK_BUDGET_SECS` matching the
    requested budget. Regenerated per boot attempt; never baked into the shared
    ISO.

    Note the two clocks: the operator's timeout is anchored at
    `PreProvisioningBootTime`, while the on-host callback loop can only start
    once preparation finishes. To keep them aligned, the script anchors its
    deadline to the host's **boot** time (via `/proc/uptime`), not to its own
    first-run time — so a long preparation can neither push the host's retry
    window past the operator's timeout nor collapse it to a short window.

### 5. Webhook (`api/v1alpha1/imageclusterinstall_webhook.go`)

- Validate `PreProvisioning` fields when set:
  - `ISOURL` must be a valid HTTPS URL
  - `ISODigest`, when set, must match `^sha256:[a-f0-9]{64}$`
  - `Timeout` must be a valid duration if provided
  - `MaxRetries` must be `>= 0`; `RetryBackoff`, when set, must be a valid
    non-negative duration
  - `CallbackBaseURL`, when set, must be an **absolute HTTPS URL with a valid
    host** (reject `http://`, relative values, and empty/malformed hosts) and
    must carry **no query or fragment** (a base path is allowed but is joined
    URL-aware, collapsing any trailing slash). A
    non-HTTPS base would transmit the per-attempt bearer token without TLS,
    and a malformed base silently breaks callback delivery (surfacing only as
    a `PreProvisioningTimedOut`), so this is rejected at admission rather than
    discovered at boot. When `CallbackBaseURL` is instead sourced from
    operator configuration (its fallback), the operator applies the **same**
    validation at startup and refuses to run pre-provisioning with an invalid
    value.

### 6. RBAC

- No new CRD permissions needed — the IBI Operator already manages BMH
  objects, DataImages, and Secrets

### 7. Tests

- Unit tests for callback ignition generation
- Unit tests for the pre-provisioning state machine
- Unit tests for the callback HTTP handler (with mock HTTP client)
- Integration tests for the full pre-provisioning → cluster installation
  flow

### Estimated Upstream Effort

| Area | Est. lines |
|---|---|
| API types + webhook validation | ~70 |
| Pre-provisioning controller methods | ~250 |
| Callback HTTP handler | ~100 |
| Ignition generation package | ~80 |
| Tests | ~500 |
| **Total** | **~1000** |

## O-Cloud Manager Effort Estimate

With the IBI Operator handling the mechanics, the O-Cloud Manager changes
are minimal:

| Area | Files | Est. lines | Complexity |
|---|---|---|---|
| API types — `IBIPreProvisioning` in `HwMgmtDefaults` | 1 file | ~25 | Low |
| CT validation — ConfigMap checks, ISO URL validation | 1 file | ~40 | Low |
| ClusterInstance rendering — pass preProvisioning config to ICI | 1 file | ~40 | Low |
| Condition mapping — map ICI pre-provisioning reasons to PR status | 1 file | ~30 | Low |
| Tests | 1 file | ~150 | Low |
| Samples and docs | 2 files | ~80 | Low |

**Total O-Cloud Manager: ~365 lines, roughly 1 working session.**

Combined with the upstream IBI Operator changes (~1000 lines), the total
effort is ~1365 lines.

## Alternatives Considered

### A. SSH-based monitoring from the IBI Operator

The IBI Operator SSHs into each server to poll the preparation service
status. Rejected because it requires SSH key management, hub-to-server
network access (often blocked by firewalls in edge environments), IP
discovery for live-booted servers, and an SSH client library dependency
in the operator.

### B. Pre-provisioning entirely in the O-Cloud Manager controller

The O-Cloud Manager patches BMH `spec.image`, boots the server, and creates
a hub-side SSH monitoring Job. Rejected because it duplicates BMH lifecycle
management that the IBI Operator already owns, requires SSH-from-a-Job
plumbing on the hub, and is not reusable by other IBI Operator consumers.

### C. Pre-provisioning as part of the hardware manager / NAR flow

Integrate pre-provisioning into the NAR controller. Rejected because the
NAR controller handles hardware allocation and firmware updates, not
image-based installation. The pre-provisioning step is logically part of the
IBI installation lifecycle.

### D. Pre-provisioning without monitoring (timeout-based)

Wait a fixed duration then assume pre-provisioning succeeded. Rejected
because it provides no failure detection, wastes time on fast completions,
and risks proceeding to cluster installation on servers that failed
pre-provisioning.

### E. BMH provisioning state as completion signal

Detect completion by monitoring BMH state transitions after the live-iso
boot. Rejected because BMH transitions to `provisioned` when the boot
*starts*, not when the preparation *finishes*. BMO has no visibility into
the preparation service running inside the live environment.

## Open Questions

1. **Condition type for the status handoff — resolved.** The design
   standardizes on the existing `RequirementsMet` condition with new
   `PreProvisioning*` reason codes rather than adding a dedicated
   `PreProvisioningCompleted` condition. A single authoritative field keeps the
   IBI Operator (producer) and O-Cloud Manager (consumer) from watching
   different conditions; the mild overloading of `RequirementsMet`'s meaning is
   accepted in exchange for one unambiguous cross-layer contract (see
   [Condition Reporting](#5-condition-reporting)).

2. **Does the recommended carrier (generic client baked in +
   per-server data on a labelled `DataImage`, read at boot — see
   [Challenge 4](#4-callback-url-injection-into-the-iso)) work on the
   supported BMO/Ironic version?** This must be settled by a version-pinned
   integration test. Note the DataImage is used **purely as a data device**,
   never as an ignition overlay (BMO does not merge DataImage contents into
   live-iso Ignition). If the test cannot be made to pass, the fallback is a
   unique per-ICI ISO with the URL/token baked directly into
   `ignitionConfigOverride`, sacrificing ISO reuse.

3. **Should the SiteConfig Operator's IBI templates be updated to support
   the `preProvisioning` field natively?** If so, the config flows through
   ClusterInstance → ICI cleanly. If not, the O-Cloud Manager needs to
   patch the ICI after SiteConfig creates it — functional but less elegant.
