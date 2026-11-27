<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Metal3 configuration delivery for IBI pre-provisioning

## Overview

The request to Metal3 is to **boot a live ISO with a secondary configuration
data ISO mounted at the same time**. Both images must be attached before the
first power-on so that the live environment can read its configuration as it
starts. This allows multiple servers to share one boot ISO while receiving
their own configuration, without rebuilding the boot ISO for each server.

For automated IBI pre-provisioning, Metal3 would coordinate attachment of
both images, report a failure before power-on if the required configuration
cannot be attached, and provide reliable attachment status and cleanup. The
sections below describe how existing BMO and Ironic functionality can support
this capability, the implemented lifecycle and hardware evidence, and the
remaining contracts and qualification needed for production use.

## Summary

The requested capability is to boot a live ISO with a second, per-host
configuration ISO already mounted. The development implementation reuses
BMH live-ISO provisioning, DataImage resources, URL-based configdrives,
Ironic image publication, and its existing pre-power-on configdrive hook.
BMO and Ironic retain ownership of BMC and power operations.

There are now two evidence checkpoints: the November 23, 2026 delivery
prototype and a November 25 complete IBI preparation and installation.
The latter passed with no manual reconcile, media-attachment, or power-on
workaround during the fresh attempt. Static networking, authenticated
completion, media teardown, disk preservation and normal IBI installation
were exercised on one Dell/iDRAC combination.

The working implementation adds durable ownership annotations, identity-aware
observation, interruption-safe media records and ordered release. It changes
no BMH or DataImage CRD and adds no Ironic HTTP request field. Those choices
remain proposed contracts for maintainer review. The separate IBI-O changes
include an ICI CRD extension, guest bootstrap and callback processing; see the
[end-to-end implementation plan](https://github.com/openshift-kni/oran-o2ims/pull/5357).

The original five-file, 69-line addition count described forwarding and
insertion only. The current non-test Metal3 source changes span 12 files,
with 697 lines added and 40 removed against the baselines below, plus runtime
packaging. Tests, generated output, IBI-O and O-Cloud Manager are additional.
This is a reviewable extension of existing machinery, rather than a complete
feature whose cost can be inferred from the original forwarding diff.

All implementation and hardware results described here use development
builds. Upstream acceptance, release integration, failure/restart hardware
qualification and broader compatibility remain required before support.
Independent BMO/Ironic updates must also preserve existing workflows when
preparation is unused. Current implicit opt-in and legacy ramdisk adoption/
cleanup gaps still need resolution; the all-updated lab run does not qualify
that compatibility contract.

## Problem and overall goal

Image-Based Installation (IBI) separates preparing a server's disk with an
OS and seed image from configuring its final OpenShift cluster. Automating
the preparation step should allow many servers to reuse one boot ISO while
receiving different configuration, without rebuilding that ISO per server.

Automated IBI preparation needs two different inputs:

- A shared, bootable IBI live ISO containing the OS, preparation tooling,
  and generic services needed by the workflow.
- A small per-server configuration ISO carrying data such as the preparation
  attempt's callback settings and credentials. The live environment must
  already contain a service that reads this data; a `DataImage` does not
  inject Ignition configuration or executable services into the boot ISO.

The original proposal assumes that the configuration can be delivered as a
BMH-associated `DataImage` alongside the live ISO. Investigation identified
three gaps in the tested release sources:

1. BMO's `getConfigDrive` deliberately omits a configdrive for live-ISO
   provisioning, [citing hardware limitations with concurrent virtual media][bmo-configdrive].
2. BMO's separate [DataImage attachment path runs during power management][bmo-power]
   of an already provisioned host that is currently off. Ironic's initial
   live-ISO deployment has already powered on the host before this path
   normally runs. Pre-creating a DataImage therefore did not attach it for
   the first boot in the stock-platform experiment.
3. [Ironic's configdrive attachment code][ironic-configdrive-attach] ejects USB-capable media before
   insertion. Some devices advertise CD, DVD, and USB support for the same
   slot, so this can eject the live boot ISO while adding configuration.

The goal is a coordinated deployment in which both required images are
attached before Ironic powers on the server. If the requested configuration
cannot be attached, deployment must report the failure before power-on.
This capability should be reusable by other live-ISO consumers.

## Existing functionality to reuse

The investigation and prototype used OpenShift `release-5.0` sources:

- BMO baseline: [`34bbeb376836`](https://github.com/openshift/baremetal-operator/tree/34bbeb376836bf01793d4e70d29065d619ebcaa1).
- Ironic baseline: [`7828a1008203`](https://github.com/openshift/openstack-ironic/tree/7828a10082034f48ca1d6d305a6766cddb160da4).

These identify the evaluated implementation; final patches need review
against the appropriate upstream branches and platform release targets.

| Existing capability | Use in this proposal |
| --- | --- |
| BMH `spec.image` with `format: live-iso` | Select the shared boot ISO and Ironic's existing ramdisk deployment path. |
| BMH-named, same-namespace `DataImage` | Identify the existing configuration ISO for that host. |
| Ironic's existing `configdrive` request field | Pass the configuration ISO URL as part of the deployment request. |
| URL-based configdrive handling | Fetch and publish the existing binary ISO without rebuilding its filesystem. |
| `RamdiskDeploy` and `RedfishVirtualMediaBoot.prepare_instance` | Prepare both media before the existing power-on operation. |
| Redfish virtual-media discovery and vendor restrictions | Select an available compatible device using the existing driver integration. |

The important ordering already exists in Ironic: its [ramdisk deployment][ironic-ramdisk-deploy]
calls `boot.prepare_instance` before powering on. The Redfish boot
implementation already calls `_attach_configdrive` after inserting the boot
ISO. The proposal connects BMO to that hook and makes the hook safe for
concurrent media; it does not introduce a new deployment engine.

### Configdrive input and guest layout

Ironic's [Redfish virtual-media ramdisk documentation][ironic-configdrive-doc]
supports a configdrive supplied as structured metadata or as a URL to a raw
configuration image, with that capability documented since Wallaby. The
existing [configdrive helper][ironic-configdrive-source] builds an image for
structured input and returns URL/raw-image input unchanged. Passing the
prebuilt configuration ISO URL uses that existing input form.

The IBI guest reads its own filesystem label and payload layout. The lab
already demonstrated that a `cluster-config` ISO retained its label, contents
and complete checksum through the exercised URL/ramdisk path. The remaining
maintainer question is whether differently labelled configuration images are
an agreed supported contract for this path across the relevant drivers and
versions.

## Proposed workflow

1. IBI-O binds a preparation attempt to its ICI and BMH identities. It
   freezes the attempt input and creates an immutable configuration ISO.
   The DataImage uses the BMH's name and namespace.
2. IBI-O requests live-ISO deployment with cleaning disabled. The host must
   permit actual provisioning; `externallyProvisioned: true` is restored
   for the later normal disk-boot installation.
3. Before issuing the provisioner request, BMO protects the DataImage with
   its finalizer and records the immutable deployment association. It
   forwards the configuration URL through the existing `configdrive` input.
4. Ironic publishes the boot and configuration ISOs, records their identities,
   and inserts them into compatible devices without replacing inserted media.
5. Only after both required images are observed inserted does the existing
   ramdisk deployment path request power-on.
6. BMO observes the intended configuration through the Ironic virtual-media
   API, using redaction-safe URL fingerprints. DataImage status reports that
   configuration, independently of whether a boot ISO is also present.
7. The live environment applies the per-host network configuration, prepares
   the disk, and reports its terminal result to IBI-O over authenticated HTTPS.
8. IBI-O requests power-off, confirms it, and clears the live deployment.
   Ironic ejects only owned media and cleans its publications. BMO persists
   release acknowledgement before clearing host ownership.
9. IBI-O deletes the preparation DataImage and waits for its finalizer,
   revokes/removes preparation credentials and artifacts, then returns the
   host to the normal IBI installation path with a fresh configuration
   DataImage. Normal disk boot is requested by that existing path.

Steps 1 through 9 were exercised in the November 25 success-path run.
BMH `provisioned` and Ironic `active` still mean that live boot was arranged;
only IBI-O's authenticated result establishes preparation completion.

## BMO changes

The current implementation retains the small forwarding change and adds its
ownership, observation and finalization contract:

| Location | Change and purpose |
| --- | --- |
| `internal/controller/metal3.io/baremetalhost_controller.go` | Claim live-ISO configuration before provisioning; isolate ordinary DataImage power-management handling while deployment ownership is active; release ownership after teardown. |
| `internal/controller/metal3.io/liveiso_dataimage.go` | Versioned durable claim, immutable input checks, deployment-aware DataImage status and positive release acknowledgement. |
| `internal/controller/metal3.io/dataimage_controller.go` | Dispatch claimed images to deployment-aware observation/finalization instead of ordinary any-media status and ejection handling. |
| `internal/controller/metal3.io/host_state_machine.go` | Preserve the owned teardown path through deletion and deprovisioning. |
| `pkg/provisioner/provisioner.go` | Configuration URL in internal `ProvisionData`; optional `ConfigDriveProvisioner` observation interface. |
| `pkg/provisioner/ironic/ironic.go` | Forward the unchanged configuration ISO URL for initial deployment/retry, preserve ordinary structured configdrive behavior, and check the owned configuration. |
| `pkg/provisioner/ironic/configdrive_status.go` | Compare recorded source/publication URL fingerprints with the requested configuration and actual inserted media through Ironic API 1.93. |
| `pkg/provisioner/demo/demo.go`, `pkg/provisioner/fixture/fixture.go` | Adapt provisioner implementations to the internal interface. |

A live-ISO request with an associated DataImage is the implemented opt-in.
A live ISO without requested configuration retains its existing path.
Once BMO has claimed configuration, a missing or changed resource cannot
silently turn the request into a boot-only deployment. API lookup failure
is distinct from absence, and a deleting DataImage is not newly consumed.
Maintainers must agree this opt-in and its mixed-version delivery contract.
An existing consumer could already create a live ISO plus DataImage without
requesting automated IBI preparation. Agree a discriminator or compatible
legacy handling before this combination can automatically impose new Ironic
metadata/status requirements. An ordinary installation must not silently
enter the new ownership protocol merely because BMO was updated.

The ownership keys are controller-maintained durable state:

- BMH: `baremetalhost.metal3.io/live-iso-dataimage`.
- DataImage: `dataimage.metal3.io/deployment-owner`.

The version-1 JSON association binds host UID, BMC address and boot MAC,
DataImage UID/generation/source URL, and boot URL. BMO records it before
consumption and checks it during observation and cleanup. The DataImage
receives `released: true` only after successful provisioner teardown.
A missing host annotation alone is not evidence that media was released.
Users and GitOps must preserve these keys and must not edit them to request
or cancel a deployment.

The URL passes through unchanged. Rebuilding an OpenStack-layout configdrive
would discard the IBI label and payload. This uses an internal Go interface
extension and existing Ironic APIs; BMH/DataImage CRDs and Kubernetes RBAC
are unchanged. Build the BMO executable and its provisioner plugin together
from the same revision and toolchain. IBI-O's separate ICI CRD changes are
outside this Metal3 API statement.

## Ironic changes

### Preserve boot media when inserting configuration

The working code in `ironic/drivers/modules/redfish/boot.py` adds an insertion
mode to `_insert_vmedia` and `_insert_vmedia_in_resource` that:

- Preserves already inserted media, including the boot ISO.
- Selects a known-empty compatible slot. An unknown insertion state does
  not count as empty.
- Uses the driver's existing System/Manager discovery and vendor slot
  restrictions, without hardcoding a second device or slot number.
- Reports an error when no usable slot exists instead of replacing the boot
  image or proceeding without the requested configuration.

The existing `_attach_configdrive` hook uses that mode without a type-wide USB
eject. The existing eject/reinsert behavior remains the default for ordinary
boot-media replacement, including its hardware workarounds.
Regression tests must verify both the configuration-preservation mode and
the unchanged default replacement path, plus their interaction during retry
and later handoff.

This matters on the tested iDRAC because both System-level slots advertise
CD, DVD, and USB capabilities. A USB request does not identify a distinct
physical device, and ejecting all media of that type can remove the boot ISO.
The existing `ramdisk_boot_configdrive` capability check still applies.

This is a candidate for a standalone generic Ironic fix: configuration-media
insertion can remove the boot ISO when advertised media types overlap, without
any IBI-specific payload being involved. Review and test the fix independently
of BMO forwarding, while retaining the default boot-media replacement path.
Acceptance and any upstream/downstream differences still need maintainer review.

### Publish configuration ISOs with an ISO filename

The first corrected lab image reached the second-slot insertion, but iDRAC
rejected Ironic's extensionless `configdrive-<node UUID>` URL with
`IDRAC.2.9.RAC0786`, "Unable to attach the folder of HTTP or HTTPS type."

The required behaviour is to publish ISO payloads with an ISO filename, such
as `configdrive-<node UUID>.iso`, while preserving their bytes and retaining
the existing handling of other binary formats.

The working implementation in `ironic/drivers/modules/image_utils.py` reuses
the result of the [existing MIME-type detection][ironic-image-preparation] to choose that
suffix. The helper already invokes `file(1)` to distinguish text-encoded
configuration from binary image content; the change adds no extra probe.
Its ISO decision uses the detected payload format rather than a source URL's
suffix.

Maintainers can select the existing detected format or an explicit media-format
signal available through an agreed interface. Any new signal needs a defined
producer-to-publisher contract and must preserve other supported formats.
Removing the `file(1)` dependency would separately require replacing the
helper's existing text/binary detection.

Update publication cleanup to account for the ISO filename and the legacy
extensionless filename belonging to that node. Cleaning published files is
separate from selecting which BMC device to eject; both operations need the
correct ownership and lifetime rules.

### Include the existing helper's runtime dependency

The existing URL-based configdrive helper invokes `file(1)` to detect the
payload format. The initial stock-based lab image lacked that executable,
so configuration preparation failed before power-on. The development image
now includes it, and the working `openshift/ironic-image` change adds `file`
with an explanatory comment to `main-packages-list.ocp`.

Ship that maintained packaging change, or an agreed replacement for the
existing detection requirement. The local Cachito checkout override used for
lab builds is not part of the contribution. The maintained image build and
CI must validate the final dependency pins and package list.

### Required configuration, observation and teardown

`ironic/drivers/modules/ramdisk.py` rejects requested configuration when the
boot interface lacks `ramdisk_boot_configdrive`, including validation again
when the deployment request arrives. It retains the existing ordering:
prepare the instance and its media, then request power-on. The Redfish path
observes both expected URLs inserted before returning from preparation.

The working Redfish code records `driver_internal_info.ramdisk_virtual_media`
before insertion, so an interrupted insertion still has an ownership record
for cleanup. It retains the boot URL, published configuration URL and source
configuration URL, and records two SHA-256 URL fingerprints:

- `configuration_source_sha256`: the DataImage source URL identity.
- `configuration_media_sha256`: the published configuration URL identity.

Ironic masks `configdrive` and `configdrive_source` in API responses. BMO
compares these fingerprints with its claimed URL and the inserted device's
image URL, preserving redaction and existing API permissions. These are
URL identities, not ISO-content hashes; IBI-O separately verifies the boot
ISO's content digest before serving it to Ironic.

Teardown identifies owned URLs, ejects only matching devices and waits for
release before deleting publications/metadata. It preserves unrelated media
and fails closed when inserted media identity cannot be established. The
active-instance path observes the existing media instead of silently
replacing it or rebooting. That logic has unit coverage; conductor restart
and loss of the Ironic database still require separate hardware qualification.

## Existing workflows and independent rollout

BMO and Ironic updates must preserve supported existing installations when
preparation is unused, including ordinary IBI, non-IBI provisioning, normal
DataImage management and existing ramdisk/configdrive consumers. Updating one
component must not require simultaneous updates to the other or to IBI-O for
those workflows. A compatible component set is required before enabling the
new deployment-owned configuration path. Define that feature requirement
separately from the ability to roll out each component independently.

This is an acceptance requirement, not a property established by the current
working branches. Their shared-path changes expose two compatibility gaps:

- BMO's live-ISO/DataImage combination is an implicit opt-in that can match
  existing consumers. Without agreed gating or compatible legacy handling,
  updating BMO alone can introduce fingerprint/status dependencies on a newer
  Ironic even though the consumer did not request the new IBI workflow.
- Ironic's ramdisk/configdrive behavior also changes for existing consumers.
  An unsupported configdrive interface now causes an error where the previous
  path logged a warning and continued. Active-instance adoption and selective
  cleanup can require `ramdisk_virtual_media` records that an older Ironic
  never produced. An Ironic update or restart could therefore reject adoption
  or cleanup of a deployment created under the previous contract.

Preserve supported legacy behavior, or obtain explicit maintainer agreement
on any intentional change and its compatibility handling. Distinguish a
pre-existing legacy deployment from loss of ownership records for a newly
claimed deployment. Accommodating legacy state must not bypass the new
configuration-required boot check, infer release from absent records, or
justify ejecting unidentified media. The exact gating/adoption contract
remains a design and implementation decision for review.

### Mixed-version regression matrix

These are development tests with preparation unused. "Existing" refers to
the supported baseline, and "updated" to the proposed implementation. Record
exact versions and test both fresh requests and workloads that were already
running before each component update.

| BMO | Ironic/runtime image | Required existing-workflow result |
| --- | --- | --- |
| Updated | Existing | Ordinary IBI/non-IBI provisioning, normal DataImage attach/detach and boot-only live ISO work without the new observation metadata/API path. Existing live-ISO/DataImage consumers retain a supported path. |
| Existing | Updated | Ordinary provisioning, virtual-media replacement, existing ramdisk/configdrive requests and their cleanup retain supported behavior. Qualify any agreed change to previously ignored unsupported configdrive input. |
| Updated | Updated | With existing IBI-O and preparation unused, ordinary installation, power handling and cleanup remain functional; the component combination alone does not activate preparation. |
| Either supported version | Updated over an existing ramdisk/configdrive deployment | Restart/adoption and deletion/teardown remain supported without requiring records the old implementation never created. |

Include structured configdrive input, prebuilt URL-based images, supported
and unsupported boot interfaces, existing mounted media and default boot-media
replacement. Preserve image contents and supported publication/cleanup behavior.
Tests of the new concurrent-media path do not replace these regressions.

The November 25 run used updated BMO, Ironic and IBI-O throughout, including
normal IBI handoff after preparation. It did not qualify this matrix or a
preparation-disabled installation. Report independent rollout as unqualified
until the gaps are resolved and the matrix passes; a shared API version alone
does not establish compatibility of the new observation protocol.

IBI-O's optional ICI API and callback Service/certificate deployment have
separate packaging and compatibility requirements in the
[end-to-end plan](https://github.com/openshift-kni/oran-o2ims/pull/5357).
Those resources do not create a dependency on new BMO/Ironic versions for
ordinary installation. The IBI-O image and its own deployment resources must
form a supported package, including ordinary image serving when its preparation
listener is disabled.

## Prototype size and validation

| Repository | Production source files | Lines added | Lines removed |
| --- | ---: | ---: | ---: |
| BMO | 3 | 28 | 7 |
| Ironic | 2 | 41 | 17 |
| **Total** | **5** | **69** | **24** |

These are measured prototype diffs, including comments and blank lines.
They exclude tests, generated artifacts, and runtime image packaging. The
net increase is 45 source lines. This is evidence that the bootstrap
integration itself is small, not an estimate or upper bound for the complete
production lifecycle or the broader IBI feature.

The lab experiment completed on November 23, 2026, using an OpenShift 5.0.2
hub and a Dell PowerEdge XR8620t with iDRAC 7.30.30.51:

1. A stock-platform boot established the gap: the live ISO booted, but the
   pre-created DataImage was not attached for that first boot.
2. A separate hardware check established that this BMC could present both
   readable ISOs concurrently.
3. The prototype then performed a fresh automated deployment from an
   available, powered-off host with both slots empty. Ironic inserted both
   ISOs before power-on, without manual BMC media attachment or an additional
   power cycle to obtain the result.
4. Both files published by Ironic matched their expected checksums. The fresh
   live guest read the configuration marker, manifest, and matching checksum
   from the `cluster-config` device.
5. The IBI preparation service remained masked and inactive, with no recorded
   execution. This verified media delivery without performing an installation.

The stock baseline and manual hardware checks preceded the successful
November 23 automated test. In the successful guest, configuration enumerated
as `/dev/sr0` and the live ISO as `/dev/sr1`, reinforcing the need to use
filesystem labels rather than assume a device index.

BMO unit suites passed for the prototype. The final Ironic changes passed
231 focused image-utils, Redfish boot, and ramdisk unit tests, with no skips
or failures, plus the configured Ironic lint checks. BMO checks on changed
code passed; its full lint run also reported pre-existing findings in unchanged
code. These results do not substitute for upstream CI and broader platform
and hardware qualification.

## Current source footprint and end-to-end validation

Measured on November 25 against the baseline commits in
[Existing functionality to reuse](#existing-functionality-to-reuse):

| Repository | Non-test source files | Lines added | Lines removed |
| --- | ---: | ---: | ---: |
| BMO | 9 | 508 | 7 |
| Ironic | 3 | 189 | 33 |
| **Total** | **12** | **697** | **40** |

Counts include comments, blank lines, new source files and provisioner adapters.
They exclude tests, generated files, documentation, IBI-O and the runtime
package-list addition. They describe the current working implementation,
including the original prototype; they are not a new estimate of all remaining
production work. Publication format, persisted metadata and ownership interfaces
may change during upstream review.

The November 25 fresh run used an OpenShift 5.0.2 hub, ACM/MCE 5.0.0,
modified BMO/Ironic/IBI-O images, and a Dell PowerEdge XR8620t with iDRAC
7.30.30.51. The installed SNO ran OCP 4.22.15. These identify a development
test platform, not minimum supported release versions.

| Observation, workstation UTC | Result |
| --- | --- |
| 18:45:17 | Both new preparation ISOs inserted while the server was Off. |
| 18:46:23 | Automatic live-ISO power-on; BMO subsequently reached provisioned and DataImage attached status with zero errors. |
| During live preparation | Static networking with DHCP disabled; the guest's entire configuration ISO matched its immutable Secret; source/publication URL fingerprints matched while raw URLs stayed redacted. |
| 19:06:29 | IBI-O persisted the authenticated success result after seed restoration and 145 successful image pulls. |
| 19:06:41 / 19:06:47 | Physical Off, followed by both preparation images absent. |
| 19:07:11 / 19:09:16 | Fresh normal configuration ISO inserted while Off, followed by automatic normal power-on. |
| By 19:26:21 | ICI preparation and installation Completed; node Ready; ClusterVersion Available and not Failing; API cluster ID and disk boot ID matched the intended installation. |

The new disk's sysroot, boot and container storage were on the intended NVMe
partitions. Automatic cleaning stayed disabled through the handoff. Preparation
Secrets, token, verified cache and ownership claims were released; the normal
configuration DataImage remained under the ordinary installation lifecycle.
No manual reconcile, media attachment, power-on patch or finalizer bypass was
needed during this fresh attempt.

An earlier November 25 run exposed the API-redaction identity bug and an IBI-O
handoff bug: setting its normal managed marker too early suppressed the real
normal boot request. Both were corrected with failing-before/passing-after
regressions, rebuilt and exercised by the fresh run above. The initial run
with manual recovery is not counted as the unassisted pass.

BMO's full unit target passed. The affected Ironic suites, including the API
redaction regression, passed 238 tests; affected IBI-O tests and scoped/configured
lint checks also passed. Pre-existing BMO full-lint findings remain separate
from changed-code validation. Upstream CI, failure/restart hardware testing,
ordinary-workflow regression and cross-vendor qualification remain required.

## Production lifecycle requirements

The working implementation now covers the success-path ownership, observation,
release and disk-boot handoff below. These remain proposed upstream contracts.
Unit coverage and the fresh end-to-end run establish different levels of evidence;
interruption, failure and compatibility qualification remain explicit work.

### Existing management paths

Two BMO paths need coordination: provisioning forwards the configuration to
Ironic's deployment-time configdrive handling, while the BMH controller's
[ordinary DataImage handler][bmo-dataimage-handling] performs attachment and
detachment during power management.

The separate [DataImage reconciler][bmo-dataimage-controller] observes media,
updates status and manages owner references/finalizers. It does not itself
perform the hardware attach/detach operations. Changing only that reconciler
would therefore leave the competing BMH power-management path unresolved.

### Delivery and ownership options

The decision is how to coordinate the deployment and ordinary DataImage
operations, rather than whether per-host configuration is a legitimate
configdrive use. Consider both approaches:

| Approach | Delivery and lifecycle model | Work still required |
| --- | --- | --- |
| A: configdrive delivery, implemented direction | Reuse Ironic's pre-power-on hook; deployment owns both images, with DataImage as immutable input. | Annotation claims, identity-aware status and ordered release are implemented; upstream contract review and recovery qualification remain. |
| B: coordinated DataImage attachment | Ordinary BMH DataImage handling owns configuration and coordinates with initial live-ISO deployment. | Order attachment with boot preparation and power-on; fix identity, cleanup and finalizers. |

Option A is preferred because it reuses a demonstrated coordination point:
Ironic already inserts configdrive media after setting up the live boot image
and before power-on. The working code supplies the annotation-based ownership
contract described above; the November 23 forwarding prototype alone did not
provide it.

Option B deserves review, but coherent status and cleanup do not follow from
moving attachment earlier. Existing status accepts any inserted image and
detachment targets all matching CD media. Those limitations need resolution
under either approach. In addition, Ironic's boot preparation ejects matching
media before inserting the boot ISO, so simply attaching configuration before
submitting the deployment can remove it again. Attaching independently after
submission can race power-on. B needs a supported coordination point after
boot-media preparation and before power-on, or boot preparation that explicitly
preserves configuration attached earlier.

Configdrive delivery and a coherent DataImage resource contract can coexist.
The BMO-facing API can report and finalize deployment-owned media while Ironic
performs the physical operations. Compare that integration with B before
accepting the upstream contract. Option A now has success-path implementation
and hardware evidence; option B remains an alternative rather than an
implemented and qualified path.

### Preferred direction: deployment-owned preparation media

Treat preparation configuration as an immutable input consumed by the
live-ISO deployment. The development implementation uses the following model;
its success path is hardware-verified and its upstream acceptance remains open:

1. Record the DataImage UID, requested version and source URL against the
   BMH/deployment identity before consuming it. Configuration required by a
   claimed deployment cannot silently become optional if the resource changes
   or disappears before attachment.
2. The deployment owns attachment and teardown of the boot ISO and its
   configuration media. While that ownership is active, ordinary BMH
   DataImage handling must not independently attach, replace or eject them.
3. Reject or explicitly fail in-place changes to configuration consumed by an
   active deployment. A new attempt uses new immutable configuration; a
   running guest must not be assumed to observe replacement contents.
4. DataImage status reports the actual deployment-owned configuration, and
   its finalizer waits for the owning deployment to release that media.
   The preferred deletion policy is to wait for producer-coordinated teardown;
   whether deletion should instead request deployment cancellation is an
   explicit maintainer decision. It must not fall through to ordinary
   DataImage ejection while deployment ownership is active.
5. After preparation completes or is cancelled, the producer coordinates
   power-off and deployment teardown. Release both owned preparation images
   and their publications when no longer needed, then allow the preparation
   DataImage to finish deleting. Do not require independent configuration
   replacement while that live environment continues to run.
6. Clear preparation ownership before creating the final IBI configuration
   DataImage and returning to ordinary DataImage management. A new resource
   must not inherit the previous deployment's attachment or completion state.

The implementation manages the two preparation images through deployment
teardown, with identity-aware ejection that preserves unrelated devices. It
adds no independent configuration-replacement API. Restart behavior still
requires the qualification described below. If maintainers require
independent configuration replacement/ejection while the live boot remains
active, that is additional scope requiring identity-aware selective operations.

Ownership of two images does not authorize ejecting unrelated media. Any
reuse of type-wide deployment teardown needs an agreed exclusive-media
precondition, or identity-aware cleanup that preserves other devices. The
supported policy must be explicit and tested through final handoff.

### Configuration identity and status

The baseline [`GetDataImageStatus`][bmo-dataimage-status] accepts any inserted
virtual media, so the boot ISO alone cannot prove configuration attachment.
That ordinary helper is retained for the existing workflow. Deployment-owned
DataImages instead use the new `ConfigDriveProvisioner` observation path.

BMO verifies the source URL fingerprint against the claimed DataImage and the
published URL fingerprint against the actual inserted device image. Busy node
operations are retried. An absent media record on an active deployment is an
error; a not-yet-recorded publication fingerprint is pending. A missing or mismatched source fingerprint
is an error rather than permission to observe arbitrary media.
A mismatched or malformed claim does not fall back to ordinary attachment.
DataImage attached status records the claimed source URL only when the correct
published configuration is observed inserted.

This relies on existing Ironic virtual-media GET support at API version 1.93
and the new metadata keys in the matching Ironic build. Neither raw URL
unredaction nor a new permission is required. Supporting version skew and
stabilizing these metadata keys are delivery/maintainer decisions; ordinary
metadata presence is not a substitute for a supported component combination.

### Deletion and handoff semantics

The implemented DataImage finalizer waits for producer-coordinated deployment
teardown. It does not invoke ordinary type-wide ejection while ownership is
active. BMO records release on the matching DataImage after provisioner
teardown, then removes the host claim. A released deleting DataImage can finish
finalizing even if that host claim has already been cleared.

The immutable association prevents a missing/replaced configuration resource
from silently becoming optional. Changes to consumed generation or URL produce
an error, rather than independently replacing media beneath a running guest.
API read errors and unknown/malformed ownership retain the cleanup barrier.
Maintainers must review this deletion contract; deletion-as-cancellation would
be a different explicit policy.

IBI-O waits for physical power-off, clears the live deployment with automatic
cleaning disabled, waits for available/release, and deletes the preparation
DataImage. It then removes attempt artifacts and creates a fresh normal
configuration DataImage. The November 25 run verified that handoff preserved
the prepared NVMe contents and automatically completed disk installation.

Retain publications until the BMC no longer needs them. Failure and interruption
cases must additionally establish that an in-flight insertion cannot survive
cancellation and restart an obsolete deployment. No recovery path may simply
remove a finalizer or infer release from missing metadata.

### Failure reporting and restart recovery

Preserve actionable causes for download, TLS, format, slot-selection, and
BMC insertion failures. During the experiment, a generic "no suitable virtual
USB slot" error hid the more useful firmware response about an extensionless
URL. Configured-media failure must not result in power-on without that media.

Qualify reconciliation and re-adoption after BMO/Ironic restart, lost Ironic
node state, retry, cancellation, and BMC unavailability. Replacing the lab
Ironic service exposed a recovery gap with the previous live-ISO deployment
that required manual setup recovery. The successful cold-boot test does not
establish interruption recovery. No production recovery path should depend
on manually editing status or removing finalizers.

### Hardware and transport compatibility

The passed tests establish development evidence for one server/firmware combination.
Advertising multiple media slots does not by itself prove concurrent boot
and configuration delivery. Qualify the supported vendor/firmware matrix,
System- and Manager-level APIs, media-type overlap, vendor restrictions,
publication URLs, and BMC-to-Ironic connectivity and TLS trust.

The preparation configuration ISO contains a real per-attempt credential.
The full run exercised authenticated callbacks, Secret-backed immutable bytes
and token/artifact cleanup. Keep the internal image service private, use TLS
on the qualified fetch/publication paths, keep credentials out of logs, and
coordinate retention with the producer. The external callback endpoint must
not expose the credential-bearing image paths. This is functional validation,
not a claim that transport/access control is qualified for every deployment.

The current configdrive hook requests USB-compatible media; supporting a
second optical-only device would need its own compatibility decision and
testing. Hardware without a usable concurrent device must receive a clear
failure for a configuration-required boot. Any per-server combined-ISO
fallback remains a producer-level design decision, not an implicit fallback
inside this small Metal3 change.

## Component boundaries and discussion points

Metal3 owns delivery, boot ordering, media observation, and media cleanup.
IBI-O owns preparation-attempt data, completion monitoring, timeouts, and
the transition to normal IBI installation. The reusable live ISO still needs
its configuration-reader and callback services, and networking must work
early enough for preparation. The IBI-O/ISO implementation is covered by the
end-to-end plan in PR 5357.
It was exercised together with Metal3 in the November 25 full run; the earlier
marker-only test remains limited to delivery.

The working implementation supports the proposed deployment-owned direction.
Before finalizing contributions or enabling the supported feature, agree the
following contracts with maintainers:

1. Confirm the documented URL-based configdrive path's supported contract for
   prebuilt images with an IBI-specific label and payload layout.
2. Is deployment ownership the right model for preparation media, and what
   controller-owned annotation contract should be accepted? The implemented
   deletion policy waits for producer-coordinated teardown; should upstream
   retain that policy or make DataImage deletion request cancellation?
3. Is live ISO plus an associated DataImage the appropriate opt-in, how are
   existing consumers protected during independent rollout, and how should
   the requirement for configuration survive resource changes?
4. Are the redaction-safe URL fingerprints and existing API 1.93 observation
   the appropriate identity interface? Which persisted metadata is stable
   across conductor restart and which recovery behavior is required if the
   Ironic database or publications are lost?
5. Does the implemented identity-aware deployment teardown provide the required
   release contract for both images and publications? Is independent replacement
   while live boot remains active required, or can it remain outside this scope?
6. Should publication naming use the existing detected format or an explicit
   format signal, and how should any such signal reach the publisher?
7. Which upstream and OpenShift release targets and hardware combinations
   support enabling the new path, and what legacy behavior/adoption contract
   permits independent component updates when preparation is unused?

### Proposed review and delivery breakdown

1. Review A and B with BMO/Ironic maintainers, including the recommended
   ownership model, the required-configuration opt-in and lifecycle acceptance
   cases. Settle the contract before treating the forwarding prototype as the
   complete BMO contribution.
2. Present the concurrent-media preservation fix as a generic Ironic change
   with overlapping-media-type, occupied/unknown-slot, vendor-restriction,
   default-replacement and failure-before-power-on coverage.
3. Declare the existing format-detection runtime dependency in the maintained
   Ironic image packaging, with its purpose documented, or agree an equivalent
   implementation. This packaging issue exists independently of IBI.
4. Agree the publication filename/format contract and its cleanup behaviour.
   Existing detection and an explicit signal are implementation choices to
   evaluate; neither should silently change other configdrive input forms.
5. Review the implemented opted-in BMO ownership/status/finalizer behavior and
   successful disk-preserving handoff. Resolve implicit opt-in and legacy
   adoption/cleanup gaps, and qualify the mixed-version matrix. Complete
   hardware cancellation, failure, restart and ordinary-workflow qualification
   before enabling the feature. Coordinate compatible versions for preparation
   without making ordinary installs depend on simultaneous component updates.

Required regression coverage includes live ISO with and without configuration,
ordinary provisioning, lookup failures and deleting resources, initial deploy
and retry, occupied/unknown/unavailable slots, preservation of boot media,
unchanged ISO contents, filename compatibility, rejection of required
configuration on unsupported boot interfaces, failure before power-on,
status identity, ownership isolation from ordinary DataImage handling,
updates/deletion during attachment, teardown of both owned preparation images,
finalizer release, restart recovery, and disk-preserving IBI handoff. Verify
that normal boot-media replacement retains its existing behaviour while
configuration insertion preserves the boot ISO. The working lifecycle and
full success-path run now provide the baseline; remaining recovery, regression
and compatibility cases are release gates for the supported workflow.

[bmo-configdrive]: https://github.com/openshift/baremetal-operator/blob/34bbeb376836bf01793d4e70d29065d619ebcaa1/pkg/provisioner/ironic/ironic.go#L1169
[bmo-power]: https://github.com/openshift/baremetal-operator/blob/34bbeb376836bf01793d4e70d29065d619ebcaa1/internal/controller/metal3.io/baremetalhost_controller.go#L1680
[bmo-dataimage-handling]: https://github.com/openshift/baremetal-operator/blob/34bbeb376836bf01793d4e70d29065d619ebcaa1/internal/controller/metal3.io/baremetalhost_controller.go#L1825
[bmo-dataimage-controller]: https://github.com/openshift/baremetal-operator/blob/34bbeb376836bf01793d4e70d29065d619ebcaa1/internal/controller/metal3.io/dataimage_controller.go#L97
[bmo-dataimage-status]: https://github.com/openshift/baremetal-operator/blob/34bbeb376836bf01793d4e70d29065d619ebcaa1/pkg/provisioner/ironic/ironic.go#L1977
[ironic-configdrive-doc]: https://docs.openstack.org/ironic/latest/admin/drivers/redfish.html#virtual-media-ramdisk
[ironic-configdrive-source]: https://github.com/openshift/openstack-ironic/blob/7828a10082034f48ca1d6d305a6766cddb160da4/ironic/conductor/configdrive_utils.py#L531
[ironic-ramdisk-deploy]: https://github.com/openshift/openstack-ironic/blob/7828a10082034f48ca1d6d305a6766cddb160da4/ironic/drivers/modules/ramdisk.py#L106
[ironic-configdrive-attach]: https://github.com/openshift/openstack-ironic/blob/7828a10082034f48ca1d6d305a6766cddb160da4/ironic/drivers/modules/redfish/boot.py#L1210
[ironic-image-preparation]: https://github.com/openshift/openstack-ironic/blob/7828a10082034f48ca1d6d305a6766cddb160da4/ironic/drivers/modules/image_utils.py#L257
