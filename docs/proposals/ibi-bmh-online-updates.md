<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Discussion: IBI BMH Power State Across Installation and Day 2 Updates

```yaml
title: ibi-bmh-online-updates
authors:
  - "@donpenney"
status: discussion
creation-date: 2026-11-12
last-updated: 2026-11-12
```

## Contents

- [Summary](#summary)
- [Current Installation Workflow](#current-installation-workflow)
- [Automatic Update After Installation](#automatic-update-after-installation)
- [How a Day 2 Update Powers Off the Node](#how-a-day-2-update-powers-off-the-node)
- [Requirements for a Fix](#requirements-for-a-fix)
- [Option 1: Suppress the BMH Manifest](#option-1-suppress-the-bmh-manifest)
- [Option 2: Suppression With O-Cloud Manager Metadata Reconciliation](#option-2-suppression-with-o-cloud-manager-metadata-reconciliation)
- [Option 3: Make SiteConfig Respect the IBI Power Lifecycle](#option-3-make-siteconfig-respect-the-ibi-power-lifecycle)
- [Option 4: SiteConfig Initializes Online Once, Then Relinquishes It](#option-4-siteconfig-initializes-online-once-then-relinquishes-it)
- [Other Approaches](#other-approaches)
- [Comparison](#comparison)
- [Validation Needed](#validation-needed)
- [Discussion and Next Steps](#discussion-and-next-steps)

## Summary

An ordinary update to a fulfilled ProvisioningRequest can power off a running
Image-Based Install (IBI) cluster. For example, adding a ManagedCluster label
causes O-Cloud Manager to update the ClusterInstance. SiteConfig then reapplies
the IBI BareMetalHost (BMH) template, which contains `spec.online: false`.
Once the BMH is under Bare Metal Operator (BMO) management, BMO acts on that
desired state and powers off the node.

The same template reset can also occur during normal provisioning, before
fulfillment and without any user update. O-Cloud Manager changes the
ClusterInstance after software installation to enable automatic import.
The BMH can still be detached at this point, masking the incorrect `online: false`.
This provides a concrete explanation for the hardware manager's need to
restore `online: true` before removing detachment at fulfillment.

[OCPBUGS-140666](https://redhat.atlassian.net/browse/OCPBUGS-140666) records this
failure on an IBI-installed SNO, including the sequence from a label-only
ProvisioningRequest update to a BMO power-off event. Its environment includes
O-Cloud Manager 4.22.3 and ACM 2.17.0.

Suppressing the BMH manifest has been successfully tested as a mitigation.
However, it also prevents SiteConfig from applying supported day 2 BMH labels
and annotations. Extending O-Cloud Manager to reconcile that metadata itself
has been discussed but has **not been implemented or tested**. A narrower
SiteConfig change could preserve metadata updates while preventing the
installation template from resetting the host's desired power state.

The leading candidate for discussion is to have SiteConfig initialize
`online` once for each host's installation lifecycle, then relinquish that
field while continuing to reconcile the rest of the BMH. This could address
both IBI and assisted installations without adding power-state inputs or a
metadata reconciler to O-Cloud Manager. It requires a SiteConfig change and
an ownership-handoff design; it is **not implemented or tested**.

This document describes the alternatives for discussion. It does not select a
final API, change controller behavior, or claim that the unimplemented
extensions are ready for deployment.

The source links below identify the versions inspected for this analysis.
The final design must also be checked against the SiteConfig and IBI operator
versions installed on the target ACM release.

## Current Installation Workflow

O-Cloud Manager renders and applies a **ClusterInstance**, not the individual
BMH manifest. The referenced SiteConfig node template renders the BMH.
The sequence below shows the reset occurring while BMO management is still
detached; asynchronous ordering is discussed in the next section.

| Step | Component | Action and power-management effect |
| --- | --- | --- |
| 1 | O-Cloud Manager | Allocates hardware and applies a ClusterInstance referencing the IBI templates, with automatic ManagedCluster import disabled. |
| 2 | SiteConfig | Renders the BMH with `online: false` and `externallyProvisioned: true`. |
| 3 | IBI operator | Creates the configuration DataImage, then sets BMH `online: true` and requests a reboot when needed. BMO performs the power operation. |
| 4 | IBI operator | Monitors installation. When installation completes, adds `baremetalhost.metal3.io/detached` to suspend BMO management. |
| 5 | O-Cloud Manager | Observes completed installation, removes the live ManagedCluster's disable-auto-import annotation, and omits it from the next rendered ClusterInstance. |
| 6 | SiteConfig | Processes the changed ClusterInstance and can render BMH `online: false` again. If management is still detached, the host remains running. |
| 7 | O-Cloud Manager | Completes provisioning and configuration, persists PR `fulfilled`, and sets NAR `spec.clusterProvisioned: true`. |
| 8 | Hardware manager | Cleans up IBI installation artifacts, restores BMH `online: true` if needed, then removes the detached annotation so BMO resumes management. |

The BMH does **not** remain `online: false` until fulfillment: the IBI operator
sets it to `true` earlier to boot the node. The hardware manager's fulfillment
handling ensures that BMO resumes with the correct desired power state.
`externallyProvisioned` remains `true` throughout this handoff.
However, the initial `online: true` set by the IBI operator can be overwritten
again during the automatic post-installation update described below.

The initial `online: false` is deliberate.
[ACM-18144](https://redhat.atlassian.net/browse/ACM-18144) introduced that default
so the IBI operator could attach the configuration DataImage before starting
the host, avoiding an unnecessary boot and subsequent reboot. Changing the
template to render `online: true` unconditionally would lose that sequencing.

Relevant code:

- SiteConfig's [IBI BMH template][ibi-bmh-template] hardcodes `online: false`.
- The IBI operator's [`updateBMHProvisioningState()`][ibi-start] sets
  `online: true`; its [installation monitor][ibi-complete] adds detachment
  after installation completes.
- O-Cloud Manager's `finalizeProvisioningIfComplete()` in
  [provisioningrequest_controller.go](../../internal/controllers/provisioningrequest_controller.go)
  calls `setNARClusterProvisioned()` in
  [provisioningrequest_hwprovision.go](../../internal/controllers/provisioningrequest_hwprovision.go).
- The hardware manager's `enableBMOManagementForIBINodes()` in
  [helpers.go](../../internal/hardwaremanager/controller/helpers.go) deletes the
  DataImage, cleans up the IBI-generated network-data Secret and references,
  ensures `online: true`, and removes detachment.

## Automatic Update After Installation

While `ClusterProvisioned` is not true, `buildClusterInstance()` injects the
following annotation into the rendered ClusterInstance:

```yaml
spec:
  extraAnnotations:
    ManagedCluster:
      import.open-cluster-management.io/disable-auto-import: "true"
```

Once software installation completes, the normal controller flow changes
that input even though the user has not modified the ProvisioningRequest:

1. `handleClusterInstallation()` observes the ClusterInstance's installation
   status and records PR `ClusterProvisioned=True`.
2. It calls `removeDisableAutoImportAnnotation()` to remove the annotation
   directly from the live ManagedCluster, allowing ACM import to proceed.
   This operation by itself does not change the ClusterInstance spec.
3. On a subsequent reconcile, `buildClusterInstance()` sees
   `ClusterProvisioned=True` and no longer injects the annotation into the
   desired ClusterInstance. Applying that desired state removes the
   previously managed spec entry and changes the ClusterInstance generation.
4. SiteConfig renders and applies the templates for that changed generation,
   including the IBI BMH's `online: false`.

These functions are in
[provisioningrequest_clusterinstall.go](../../internal/controllers/provisioningrequest_clusterinstall.go).
The [SiteConfig reconciler][siteconfig-reconcile] skips template processing
when the generation is already observed; an unchanged O-Cloud Manager apply
alone does not imply that the BMH will be reapplied. Removing this annotation
is a concrete spec change that causes another render.

The IBI operator adds detachment **before** reporting installation complete.
If SiteConfig's update arrives while management is still detached, the BMH can have desired
`spec.online: false` while the physical server stays running. BMO's
[detachment handling][bmo-detachment] suspends normal management, so it does
not act on that false value. At fulfillment, the hardware manager must
restore `online: true` **before** removing detachment; otherwise BMO could
power off the server as management resumes.

This places the automatic reset at the installation-to-import transition.
ACM import and policy application are asynchronous, so it is not a strict
barrier before every policy can start applying. The code establishes a
normal-provisioning trigger; correlating audit records and logs would confirm
the exact ordering in a particular historical lab run.

Validation must also check the ordering of the SiteConfig apply and the
hardware manager's fulfillment handling. A delayed template apply after
reattachment would no longer be masked by detachment. Restoring `online: true`
once at fulfillment cannot replace preventing later template-driven resets.

## How a Day 2 Update Powers Off the Node

A user can update a fulfilled PR with a label change such as this fragment:

```yaml
spec:
  templateParameters:
    clusterInstanceParameters:
      extraLabels:
        ManagedCluster:
          example.com/site: site-a
```

The resulting sequence is:

1. The PR generation changes and reconciliation validates and merges its input
   with the ClusterTemplate defaults.
2. O-Cloud Manager renders and applies the updated ClusterInstance, including
   the new ManagedCluster label.
3. SiteConfig reconciles the changed ClusterInstance and renders its referenced
   templates, including the BMH template. The label change need not target the
   BMH for its template to be reapplied.
4. The IBI template supplies `online: false` again. SiteConfig applies that BMH
   desired state.
5. The BMH is now attached to BMO management. BMO observes that the desired
   power state is off and powers down the running node.

The issue reports the PR remaining `progressing` and the ManagedCluster
becoming unavailable until the host is powered back on. This is a reconciliation
ownership problem: an installation-time default overwrites a value deliberately
changed later in the installation lifecycle.

The distinction from the automatic provisioning update is the BMO attachment
state: before fulfillment, detachment can mask the reset; after the hardware
manager reattaches the BMH, the same reset can cause an actual power-off.

The fulfillment handoff does not continuously repair `online`:
`setNARClusterProvisioned()` returns without changing the NAR when its flag is
already true. A label-only PR update therefore does not necessarily trigger
the hardware manager's post-installation handling again. Similarly, the IBI
operator marks the BMH as managed after initiating installation and does not
continually reassert `online: true` through that initialization path.

O-Cloud Manager already adds `AgentClusterInstall` suppression when
`clusterImageSetNameRef` changes, through `handleClusterInstanceUpgrade()` in
[provisioningrequest_clusterinstall.go](../../internal/controllers/provisioningrequest_clusterinstall.go).
That does not protect a label-only update or suppress an IBI BMH. Protection
must cover all relevant post-installation ClusterInstance applies, including
updates associated with upgrades, rather than only image-set changes.

## Requirements for a Fix

- Keep the initial IBI `online: false` and `externallyProvisioned: true`
  behavior and let the IBI operator control when installation boot begins.
- Protect the automatic post-installation ClusterInstance update as well as
  explicit day 2 updates. Protection must take effect before fulfillment.
- Prevent unrelated updates from changing the host's desired power state.
- Preserve supported BMH label and annotation updates, as documented in
  [Day-2 Cluster Configuration Changes](../user-guide/cluster-configuration.md#updates-to-the-clusterinstanceparameters-field-under-provisioningrequest-spectemplateparameters).
- Preserve hardware-profile updates, including BMO-controlled BIOS and firmware
  servicing and intentional reboots, as well as cluster upgrades and deletion.
- Distinguish existing installed hosts from new or replacement targets. The
  suppression prototype also covers assisted installations, so MNO scale-out
  and scale-in must remain functional.
- Make retries and operator restarts safe. A persisted cluster-level completion
  flag alone must not incorrectly classify a newly added node as installed.
- Define how existing ClusterInstances and intentionally configured suppression
  will be handled when adopting the final solution.

## Option 1: Suppress the BMH Manifest

### Mechanism and Tested Behavior

Add `BareMetalHost` to `spec.nodes[].suppressedManifests` for existing installed
nodes before applying their ClusterInstance updates. SiteConfig then skips
applying those BMH manifests, leaving their current power configuration intact.

The basic mitigation is also described in
[ACM-28009](https://redhat.atlassian.net/browse/ACM-28009). Its original report
observed the unwanted `online: false` while the host remained running; the
O-Cloud Manager case additionally removes detachment at fulfillment, exposing
the running host to BMO's power-state reconciliation.

The O-Cloud Manager prototype implements suppression in the common
`applyClusterInstance()` path. It matches existing nodes by hostname and
`hostRef`, preserves their suppression across reconciles, and leaves new
targets unsuppressed. It also uses the fulfilled node count to avoid treating
pending scale-out nodes as installed after a restart. Node-level suppression
avoids preventing a new worker's installation through blanket cluster-level
BMH suppression.

The prototype begins adding suppression when cluster installation is complete,
rather than waiting for PR fulfillment. Its common apply-path placement can
therefore cover the automatic disable-auto-import removal as well as explicit
day 2 changes. That specific controller sequence still needs validation;
the label-update test alone does not demonstrate it.

**The suppression mitigation has been successfully tested for power-off
prevention.** The local prototype also passed `make ci-job` and all 50 e2e
scenarios, including the existing hardware-update, scale, and cluster-version
upgrade workflows. Those results do not establish full day 2 compatibility:
the e2e environment does not run the actual SiteConfig and BMO controllers.

### Open Issues

Suppression applies to the **whole BMH manifest**, including metadata. It
therefore blocks these otherwise supported PR changes from reaching the BMH:

```yaml
extraLabels:
  BareMetalHost:
    example.com/rack: rack-b
extraAnnotations:
  BareMetalHost:
    example.com/description: updated
```

The PR can pass validation and the ClusterInstance can contain the changed
metadata while the live BMH retains its old values. The existing tests verify
ClusterInstance input and suppression; they do not exercise SiteConfig's
propagation of BMH metadata. A blanket assertion that no day 2 operation needs
to re-render a BMH therefore overlooks a documented feature.

Suppression does not disable BMO or block other clients from modifying the
BMH. Hardware-manager BIOS and firmware operations update the BMH and Metal3
resources directly, so they do not depend on SiteConfig reapplying this
manifest. Scale-in uses explicit pruning, which SiteConfig evaluates before
suppression. However, those code paths and the passing tests are not a
substitute for checking real controller interactions in the lab.

Other implications to resolve include:

- BMH template corrections and other template-supplied changes also stop being
  applied to suppressed hosts. This is broader than protecting `online` alone.
- Applying the mitigation to assisted installations broadens the behavior
  change beyond the reported IBI defect; that scope needs explicit agreement.
- Suppression must not prevent new or replacement hosts from receiving their
  installation manifests, or interfere with pruning and finalizer cleanup.
- A later move to a SiteConfig fix must handle suppression already persisted
  by the mitigation without removing suppression deliberately supplied by users.

## Option 2: Suppression With O-Cloud Manager Metadata Reconciliation

### Proposed Extension: Not Implemented or Tested

Keep the BMH manifest suppressed and make O-Cloud Manager apply the permitted
BMH labels and annotations separately. This is a proposed extension to the
tested mitigation, not a capability present in the prototype.

The additional reconciliation would need to:

1. Resolve the effective BMH metadata from the already merged ClusterTemplate
   and PR input, including node-group expansion and SiteConfig's node-level
   lookup with cluster-level fallback. Preserve template-defined metadata
   precedence as well.
2. Identify the existing allocated BMH through its node association and
   `hostRef`; skip deleting hosts, new installation targets, and hosts being
   removed by scale-in.
3. Apply a conflict-checked patch containing only the intended metadata
   changes. Leave BMH spec fields, owner references, finalizers, and unrelated
   controller metadata intact. Dry-run ClusterInstance validation must not
   perform these writes.
4. Support additions, value changes, and removals. Persist a record of the
   metadata being managed, potentially in a bookkeeping annotation on the BMH,
   including a safe initial handoff from SiteConfig.
5. Retry partial failures and report completion only when both the
   ClusterInstance and BMH metadata have converged.

Comparing only the old and new ClusterInstance is insufficient. A restart can
occur after the new ClusterInstance is stored but before the BMH patch
succeeds. At that point, the old metadata needed to calculate removals is no
longer available from the ClusterInstance alone. The bookkeeping and its
initialization must be durable before that information is lost.

### Costs and Risks

- O-Cloud Manager takes over part of SiteConfig's metadata responsibility and
  must reproduce its precedence, removal, and field-ownership behavior,
  including custom templates. This is more than copying two maps.
- The handoff must distinguish requested metadata from template-owned and
  runtime metadata. It must preserve annotations used by BMO, the IBI operator,
  and hardware-manager servicing, rather than replaying stale values.
- ClusterInstance and BMH writes are not an atomic transaction. Ordering,
  conflicts, in-flight SiteConfig reconciliation, and restart recovery need
  dedicated tests.
- A metadata-only patch can still request an operational action if it changes
  an annotation interpreted by BMO. The extension must retain the intended
  metadata semantics and avoid accidentally triggering actions through its
  own bookkeeping or reconciliation.
- It retains suppression's broader effect on BMH template changes and adds
  another reconciliation path to maintain.

The likely O-Cloud Manager changes are a focused metadata helper and tests,
integration with the real post-installation ClusterInstance apply path, and
error/status handling. The current manager ClusterRole already grants BMH
read and patch operations; a metadata-only extension does not inherently
require new RBAC or an O-Cloud Manager CRD change. This still needs validation
against the final implementation.

The integration must also handle the automatic post-installation update,
which does not require a new PR generation. It cannot depend only on a user
changing the request or on the PR already being fulfilled.

## Option 3: Make SiteConfig Respect the IBI Power Lifecycle

### Proposed Behavior

Continue rendering and applying the BMH, including its metadata, while
preventing the initial power-off default from being reapplied after the IBI
operator has taken control of booting the node.

The desired contract is:

| Lifecycle point | BMH power behavior |
| --- | --- |
| Initial installation, before the IBI operator prepares the DataImage | Render `online: false`, preserving the current installation sequence. |
| IBI operator initiates installation boot | IBI operator sets `online: true`; BMO boots the host. |
| Automatic disable-auto-import removal after installation completes | Preserve the desired power state during this ClusterInstance update, before PR fulfillment or BMO reattachment. |
| Subsequent renders for that same host after the IBI power handoff | Preserve its current desired `spec.online` rather than restoring the installation default. For a normally running cluster this is `true`. |
| New installation target or a new installation attempt | Establish the initial-installation behavior for that target; do not inherit another host's completed lifecycle. |

The preserved value must be the desired **`spec.online`**, not observed
`status.poweredOn`. A host can be temporarily powered off during a valid
reboot or firmware operation while its desired state remains online.
Likewise, forcing `online: true` on every day 2 render could undo an
intentional desired shutdown. The ownership of such requests must be part
of the agreed contract.

Adoption also needs care: a live `online: false` might already be the result
of the old template reset, rather than an intentional shutdown request.
Preserving that value blindly does not repair an earlier reset. The fix must
prevent the automatic reset at its source, and the rollout plan must define
how existing affected states are handled without undoing deliberate power
requests. The existing fulfillment restore-before-reattach ordering remains
necessary while that behavior is being validated.

There is no existing `ClusterInstance.spec.nodes[].online` override. The
current [template context][siteconfig-context] exposes the ClusterInstance
spec and node spec, not an arbitrary live-BMH lookup or installation status.
O-Cloud Manager therefore cannot solve this by adding `online: true` to its
current ClusterInstance input alone.

Two possible SiteConfig implementations are outlined below. Both are design
options and have not been implemented or tested as part of this work.

### Variant A: SiteConfig Preserves the Value Internally

SiteConfig could obtain the existing BMH's desired power state and supply it
to the IBI template, or preserve that field while preparing the BMH apply.
It must first establish that the IBI operator has already taken control of
booting this particular host. The mere existence of a BMH is insufficient:
O-Cloud Manager allocates an existing BMH before installation starts.

An agreed IBI lifecycle signal is needed. The IBI operator's existing
`image-based-install-managed` annotation, set when it initiates installation,
is one candidate to evaluate. Its meaning, removal, and reuse across
deallocation or reinstall must be verified before treating it as a contract.
A per-host installation signal is preferable to assuming that every host in
a previously completed ClusterInstance has been installed.

The new workflow would be:

1. O-Cloud Manager applies the initial ClusterInstance as it does today.
2. SiteConfig renders `online: false`; the IBI operator prepares the DataImage
   and requests the initial boot through BMO.
3. After the agreed IBI handoff signal exists, SiteConfig preserves that
   host's desired power state on subsequent BMH applies.
4. IBI completes installation and detaches the BMH. O-Cloud Manager enables
   import and updates the ClusterInstance to remove disable-auto-import.
   SiteConfig processes that update while preserving `online: true`.
5. Import, policy configuration, and O-Cloud Manager's fulfillment cleanup
   proceed as today, including ensuring `online: true` before reattachment.
6. A later PR label update changes the ClusterInstance. SiteConfig applies
   the new metadata while retaining the current desired `online` value, so
   an unrelated update does not request a power-off.

With this variant, O-Cloud Manager might need **no new lifecycle input or
metadata reconciler**. It would need the compatible SiteConfig release or
template behavior, regression coverage, and any necessary removal of
operator-generated BMH suppression. Its existing fulfillment cleanup should
remain: the SiteConfig fix does not replace DataImage/network-data cleanup
or the removal of IBI detachment.

Simply omitting `online` from later server-side apply payloads is not an
established solution. Omitting a previously managed field can remove or reset
it depending on field ownership and defaulting. The implementation must
explicitly validate the value-preservation or ownership-handoff behavior
against a real API server. See the
[Kubernetes server-side apply documentation](https://kubernetes.io/docs/reference/using-api/server-side-apply/).

### Variant B: An Explicit Per-Node Template Input

Alternatively, SiteConfig could make the IBI template consume an explicit
per-node input supplied by O-Cloud Manager. A dedicated optional field would
require SiteConfig API, CRD, and validation changes. An agreed operator-owned
marker in existing node `extraAnnotations` could avoid a new CRD field, but
still requires an updated template and a defined input/ownership contract.
No marker name or new API field is specified by this discussion.

The new workflow would be:

1. Initial ClusterInstance nodes have no post-installation override, so the
   template renders `online: false`.
2. Installation and the existing IBI/BMO handoffs proceed normally.
3. At the first post-installation ClusterInstance render, including the
   automatic disable-auto-import removal, O-Cloud Manager identifies the
   existing installed nodes and supplies the agreed power-preservation input.
   This must happen before that changed ClusterInstance is applied, without
   waiting for PR fulfillment or a user-requested day 2 change.
   A simple running-node implementation would render `online: true`; the
   final design must also address intentional desired-off states.
4. SiteConfig renders the BMH with that input and continues applying normal
   labels and annotations. New targets retain the installation default.
5. O-Cloud Manager preserves the input across retries and restarts, bound to
   the same host identity, and resets it only for an explicitly new lifecycle.

O-Cloud Manager would replace the suppression transformation with this
per-node input transformation. The same care around hostname/`hostRef`, new
nodes, and stale completion status would still be needed. A cluster-wide
"already installed" flag would be insufficient for the generalized MNO path.

If this uses a new SiteConfig API field, the Go dependency, installed
SiteConfig CRD, and templates must all support it. O-Cloud Manager's
ClusterTemplate schema and input validation would need changes only if that
field is exposed to users; an internally derived field still needs a
compatible platform API. An annotation-based input avoids that API change
but does not avoid template delivery or compatibility checks.

A custom IBI node-template ConfigMap could prototype this behavior before an
upstream change. That introduces template maintenance and requires a defined
adoption path for existing ClusterInstances. Changes to `templateRefs` cannot
be assumed to be allowed after installation; the current SiteConfig day 2
field allowlist does not include them. Mutating the shared default ConfigMap
per PR would also affect other consumers and is not a suitable per-request
override mechanism.

### Other Installation Fields to Check

Protecting `online` alone does not establish that every other installation
field is safe to reapply after the handoff. For example, the hardware manager
restores the BMH's original `preprovisioningNetworkDataName` during IBI cleanup,
while the IBI template can supply a network-data reference again when node
network configuration is present. The narrower solution must check whether
later renders undo that cleanup or recreate installation artifacts. This is
an integration question to investigate, not a second confirmed defect from
the reported power-off test.

### Code Areas and Integration Questions

| Area | Potential work |
| --- | --- |
| SiteConfig IBI BMH template | Replace the unconditional power-off value with the agreed lifecycle-aware behavior; keep initial installation offline and externally provisioned. |
| SiteConfig rendering/apply code | Supply the existing desired power state or new input, with host identity checks and correct server-side apply behavior. |
| SiteConfig API and webhook | Needed if adding a dedicated field; define its default, update permissions, and reinstall/reset semantics. |
| O-Cloud Manager ClusterInstance apply | Variant A may need no new input. Variant B needs a per-node transformation before `applyClusterInstance()`, covering automatic post-installation, day 2, and upgrade-associated applies. |
| O-Cloud Manager hardware manager | Retain the existing IBI cleanup and BMO reattachment. Do not add a competing continuous power-state override. |
| Delivery and migration | Establish minimum SiteConfig/template support and how existing clusters adopt it, including any operator-generated suppression. |

## Option 4: SiteConfig Initializes Online Once, Then Relinquishes It

### Proposed Contract

SiteConfig would set the initial desired power state when it first configures
a BMH for a particular installation lifecycle, then stop managing that field.
Later applies would still reconcile the BMH's supported metadata and other
fields. This is the preferred candidate to investigate with the SiteConfig
maintainers, subject to the implementation and compatibility questions below.

| Point in the host lifecycle | SiteConfig responsibility for `spec.online` |
| --- | --- |
| Initial setup for IBI | Apply the [IBI template][ibi-bmh-template]'s `false` value so the IBI operator can prepare the DataImage before requesting boot. |
| Initial setup for assisted installation | Apply the [assisted template][assisted-bmh-template]'s `true` value, preserving the current initial boot behavior. |
| After SiteConfig's initial setup and field handoff | Exclude `online` from SiteConfig's ongoing managed intent; leave later desired-state changes to the installing controller, hardware manager, or an explicit power request. BMO enforces that state. |
| New target, reallocation, or explicit reinstall | Establish a new installation lifecycle and initialize the field again according to that lifecycle's template. |

Unlike Option 3's value-preservation variant, this option would not read the
live value and continually resubmit it as SiteConfig's desired value. It would
stop asserting the field. That avoids both resetting it to the template
default and competing with an intentional desired-off state through a stale
read followed by a forced apply.

The current [SiteConfig apply routine][siteconfig-apply] copies the rendered
object into a server-side apply payload and uses `ForceOwnership`. A targeted
BMH preparation step could implement this policy before that apply, leaving
the initial template values and the normal metadata rendering path intact.
There is no existing initialization-only option in the inspected apply path;
this is a proposed controller change, not an available configuration setting.

### Initialization Must Belong to a Host Lifecycle

"The BMH already exists" is not a sufficient test. O-Cloud Manager's hardware
manager allocates BMHs from an existing inventory, so the first SiteConfig
apply must still initialize their power state. SiteConfig needs to distinguish
**its first setup of the host for this lifecycle** from an update to that
already initialized host.

The implementation would need to:

- Persist or reliably derive that initialization has succeeded for the
  particular ClusterInstance/host association and installation attempt.
- Ensure a restart or a failure between applying the initial value and
  recording initialization cannot cause that value to be replayed after
  another controller has taken over. Dry-run validation must not advance
  the persisted lifecycle state.
- Treat label changes, new ClusterInstance generations, and software upgrades
  as updates to the existing lifecycle. They must not initialize power again.
- Handle genuine reinstall and host reuse explicitly. A hostname or resource
  name alone cannot distinguish a new allocation from an old one.
- Adopt already initialized clusters without writing the initial power value
  again merely because they lack a newly introduced bookkeeping marker.

The exact record or signal is an open design decision. It would not need to
depend on the IBI-specific `image-based-install-managed` annotation if
SiteConfig can track its own initialization and handoff reliably, which makes
the approach applicable to assisted installation as well.

### Server-Side Apply Ownership Handoff

Simply deleting `spec.online` from later apply payloads is insufficient when
SiteConfig is its sole field owner. Server-side apply may remove or reset a
previously managed field when it is omitted. The handoff must leave the value
intact and prevent SiteConfig from reclaiming it on subsequent reconciles.

The [documented ownership-transfer mechanism][ssa-handoff] allows another
field manager to share ownership of the same value before the outgoing
manager omits the field. A manager used only for the initial handoff is one
implementation possibility; the actual BMH protocol still needs to be
designed and verified against a real API server. Manually editing
`managedFields` is not part of this proposal.

In particular, the design cannot assume another installer will always change
the value first. IBI normally changes the initial `false` to `true`, but
assisted installation can leave the initial `true` unchanged. Both cases must
hand off safely, including when SiteConfig is still the sole owner. The
handoff must also tolerate another controller changing `online` concurrently
without restoring an earlier value.

### Resulting Workflow and O-Cloud Manager Impact

1. O-Cloud Manager applies the initial ClusterInstance normally. SiteConfig
   initializes the host using the existing installer-specific template value
   and safely relinquishes ongoing ownership of `online`.
2. The installer and BMO perform the normal boot/install workflow. SiteConfig
   no longer reasserts the initial power value on later BMH applies.
3. After installation, O-Cloud Manager removes disable-auto-import. SiteConfig
   processes the changed ClusterInstance and applies its metadata without
   modifying `online`, even before PR fulfillment.
4. Existing fulfillment cleanup and BMO reattachment proceed normally. Later
   day 2 metadata changes follow the same SiteConfig path without causing a
   template-driven power change.

O-Cloud Manager could retain its normal ClusterInstance update behavior,
without BMH suppression, a separate BMH metadata reconciler, or a new per-node
power input. No O-Cloud Manager API change is inherent in this design. Its
existing DataImage/network-data cleanup and BMO reattachment should remain;
removing the fulfillment power-state safeguard would be a separate decision
after validating the new ownership behavior.

Adoption would require a compatible deployed SiteConfig release and regression
coverage. An internal SiteConfig controller fix would not itself require an
O-Cloud Manager Go API dependency update. Any operator-generated suppression
already applied would still need a safe transition, and the other installation
fields discussed under Option 3 still need checking.

### Remaining Tradeoffs

This option avoids duplicating metadata reconciliation in O-Cloud Manager and
can cover both installers through one power-ownership contract. However,
durable initialization, ownership transfer, and existing-cluster adoption are
required work, not a proven one-line field removal.

The SiteConfig maintainers must also agree on compatibility with custom BMH
templates that intentionally control power beyond initial installation. They
may require an explicit policy for that behavior rather than changing every
template's contract unconditionally. This proposal targets `online` only;
it does not define a general mechanism for ignoring arbitrary BMH fields.

## Other Approaches

| Approach | Limitation |
| --- | --- |
| Hardcode `online: true` in the IBI template | Reintroduces the initial boot/DataImage sequencing issue addressed by ACM-18144. |
| Patch `online: true` after every ClusterInstance apply | Races BMO: the power-off request may already have been acted on. It also competes with intentional power management. |
| Skip the entire ClusterInstance update after fulfillment | Blocks supported ManagedCluster and other day 2 changes. |
| Leave the BMH detached permanently | Prevents normal BMO management and conflicts with hardware servicing and cleanup. |

## Comparison

| Approach | Power-off prevention | BMH metadata updates | Main cost or unresolved issue | Evidence |
| --- | --- | --- | --- | --- |
| BMH suppression alone | Demonstrated for the reported trigger | Blocked through SiteConfig | Broader manifest freeze and a documented day 2 regression | Successfully tested mitigation; prototype CI/e2e passed |
| Suppression plus O-Cloud Manager metadata reconciliation | Retains the demonstrated mitigation | Would be implemented by O-Cloud Manager | Metadata ownership, removals, handoff, retries, and duplicated responsibility | Proposed extension; not implemented or tested |
| SiteConfig internal power-state preservation | Expected if lifecycle and apply semantics are correct | Retained through SiteConfig | Agree on a per-host lifecycle signal and preservation behavior | Discussion only |
| SiteConfig explicit per-node input | Expected if input is set and retained correctly | Retained through SiteConfig | Template/API support and O-Cloud Manager input management | Discussion only |
| SiteConfig initializes, then relinquishes `online` (preferred candidate) | Expected after a safe initial handoff | Retained through SiteConfig | Durable host-lifecycle tracking, SSA handoff, and adoption | Proposed; not implemented or tested |

## Validation Needed

The final fix needs focused unit/API tests and lab tests with the actual
SiteConfig, IBI, BMO, and O-Cloud Manager controllers. Passing the current
O-Cloud Manager e2e suite alone does not exercise all of those interactions.

| Scenario | Required evidence |
| --- | --- |
| Initial IBI installation | DataImage preparation still precedes the intended boot; the cluster installs and reaches fulfillment. |
| Initial assisted installation and an existing inventory BMH | SiteConfig applies the correct initial power value on its first setup even when the BMH already exists. |
| Ownership release with both initial boolean values | The first omission preserves `online` when SiteConfig was its sole owner and when another controller already changed it. Verify SiteConfig relinquishes ownership and does not reclaim it later. |
| Power changes after handoff | Another controller can change `online` in either direction; subsequent SiteConfig metadata applies leave it unchanged, including under concurrent updates. |
| Automatic installation-to-import transition, with no user PR change | Observe disable-auto-import removal and the resulting ClusterInstance generation change; BMH `online` stays true, and reattachment occurs only after the desired power state is correct. |
| ManagedCluster label/annotation update | Requested metadata appears, BMH desired power remains unchanged, and the running node stays Ready. |
| BMH label/annotation update | Additions, replacements, and removals reach the live BMH with correct precedence and without changing its spec. |
| Operator/controller restart, dry-run, or partial API failure | Metadata and lifecycle state converge without losing removals, replaying initial power defaults, or recording a handoff that did not persist. |
| BIOS/firmware update | Intended servicing and reboot work, runtime annotations remain correct, and the node returns Ready. |
| Cluster-version and IBU updates | Any ClusterInstance reapply retains the correct power behavior; upgrade-specific handling remains functional. |
| Assisted MNO scaling, if the fix also covers that path | Existing hosts stay protected, new targets install, and removed hosts prune and deallocate correctly. |
| Deletion and a new installation attempt | Finalizers finish and stale lifecycle information does not affect another allocation or reinstall. |
| Intentional desired-off state | An unrelated update does not force the host online or infer desired state from a transient observed power status. |
| Existing ClusterInstance adoption | The fix is effective with the deployed SiteConfig/templates, and user-supplied suppression is preserved. |

## Discussion and Next Steps

The tested suppression approach is a useful mitigation, but it is not a
complete replacement for the existing day 2 BMH update behavior. Its metadata
extension should be evaluated as additional controller functionality, with
the associated ownership and recovery work, rather than assumed to be a
small or already validated addition.

The preferred candidate is **Option 4: initialize `online` once, then
relinquish it**. It addresses the ownership boundary directly, retains normal
BMH metadata rendering, and could cover IBI and assisted installation without
new O-Cloud Manager reconciliation or API inputs. This preference is for
discussion; feasibility and the compatibility contract still need agreement
and tests. Option 3's preservation and explicit-input variants remain
alternatives if a general initialization-only contract is unsuitable.

The decisions needed are:

1. Can SiteConfig safely initialize and relinquish `online` for both installers,
   including when no other controller has changed the initial value?
2. How is successful initialization identified durably for an existing BMH,
   and how is a new allocation or reinstall distinguished from an update?
3. Does the power-field handoff finish early enough for the automatic installation-to-import
   update, and how will an already-reset false value be distinguished from
   an intentional shutdown during adoption?
4. How are intentional power changes represented without introducing another
   controller that continuously forces the host online?
5. What compatibility policy is needed for custom templates that intentionally
   manage power beyond initial installation?
6. How will existing ClusterInstances receive the new behavior, and what
   platform/template versions are required?
7. If suppression is delivered as an interim mitigation, how will the BMH
   metadata limitation be handled and the eventual transition tested?

The outcome should determine the implementation scope in SiteConfig and
O-Cloud Manager before an untested option is treated as the final fix.

[ibi-bmh-template]: https://github.com/stolostron/siteconfig/blob/604a6a3095fc/internal/templates/image-based-installer/template.go#L136
[ibi-start]: https://github.com/openshift/image-based-install-operator/blob/4bc5f2a691cab4b02638dfd6bd567cd0cf2f9408/controllers/imageclusterinstall_controller.go#L721
[ibi-complete]: https://github.com/openshift/image-based-install-operator/blob/4bc5f2a691cab4b02638dfd6bd567cd0cf2f9408/controllers/imageclusterinstall_monitor.go#L169
[siteconfig-context]: https://github.com/stolostron/siteconfig/blob/604a6a3095fc/internal/controller/clusterinstance/helper.go#L41
[siteconfig-reconcile]: https://github.com/stolostron/siteconfig/blob/604a6a3095fc/internal/controller/clusterinstance_controller.go#L219
[bmo-detachment]: https://github.com/metal3-io/baremetal-operator/blob/f10aa3a42ab52d1e524f8c2dddf3379c808c7d17/internal/controller/metal3.io/host_state_machine.go#L292
[assisted-bmh-template]: https://github.com/stolostron/siteconfig/blob/604a6a3095fc/internal/templates/assisted-installer/template.go#L257
[siteconfig-apply]: https://github.com/stolostron/siteconfig/blob/eadd0287e113d26400229d62ee0683098eb85b85/internal/controller/clusterinstance_controller.go#L653
[ssa-handoff]: https://kubernetes.io/docs/reference/using-api/server-side-apply/#transferring-ownership-between-managers
