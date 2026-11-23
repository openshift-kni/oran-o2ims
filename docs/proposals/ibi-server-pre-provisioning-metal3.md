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
this capability, what the prototype demonstrated, and the lifecycle work
still needed for production use.

## Summary

Extend the existing Metal3 live-ISO provisioning path so that a separate
configuration ISO can be attached **before the first power-on**. The initial
consumer is [automated IBI server pre-provisioning](./ibi-server-pre-provisioning.md):
many servers boot the same IBI live ISO, while each server receives its own
preparation-attempt configuration through a small `DataImage`.

The proposed approach builds on functionality already present in the
Bare Metal Operator (BMO) and Ironic: BMH live-ISO provisioning, `DataImage`
resources, URL-based configdrives, local image publication, and Ironic's
pre-power-on configdrive hook. BMO and Ironic continue to own BMC operations.

A prototype successfully demonstrated this sequence on a Dell PowerEdge
XR8620t with iDRAC 7.30.30.51. Its first-boot production source changes are
small: **five files, 69 added lines and 24 removed lines** across BMO and
Ironic. The complete production contribution spans `baremetal-operator`,
`openstack-ironic`, and `ironic-image`; tests, runtime packaging, and additional
status, cleanup and recovery work are outside that measured source diff.
The complete production scope has not yet been estimated.

The preferred lifecycle direction is for the live-ISO deployment to own both
preparation images, using the DataImage as an immutable deployment input.
Status and finalization must reflect that ownership. This model still needs
maintainer agreement and lifecycle validation beyond the first-boot prototype.

This is a discussion proposal, not a declaration that the complete IBI
pre-provisioning workflow or all supported hardware has been qualified.

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

1. The producer, initially the IBI Operator (IBI-O), creates the per-attempt
   configuration ISO and its DataImage before requesting the live-ISO
   deployment. The DataImage uses the BMH's name and namespace.
2. The producer requests deployment through the existing BMH live-ISO and
   power controls. For IBI preparation, it must arrange a host state that
   permits actual provisioning; the usual `externallyProvisioned: true`
   handoff for an already prepared IBI server is a different stage.
3. BMO captures the configuration identity/version for the deployment and
   supplies its URL through the existing Ironic request's `configdrive` field,
   alongside the boot ISO. The prototype forwards the URL; durable input and
   ownership capture are additional production work.
4. Ironic stages and inserts the live ISO through its existing boot path.
5. Ironic fetches the configuration ISO, publishes it with a usable filename,
   and inserts it into a free compatible slot while preserving the boot ISO.
6. After successful preparation of both media, Ironic sets the boot device
   and powers on the server using its existing deployment sequence.
7. The live guest locates the configuration by filesystem label, reads its
   contents, and runs the IBI preparation workflow. IBI-O monitors actual
   preparation completion and coordinates subsequent cleanup and handoff.
8. Under the preferred ownership model, the producer requests a safe stop of
   the live-ISO deployment. Its teardown releases both preparation images,
   and DataImage finalization waits for that release.
9. Once preparation ownership is cleared, the producer transitions to normal
   IBI installation with a fresh cluster-configuration DataImage. Preserving
   the prepared disk through that transition remains a validation gate.

BMH `provisioned` or Ironic `active` means that live boot was arranged; it
does not establish that the OS installation and seed restoration succeeded.
That completion signal remains the responsibility of the IBI workflow.

## BMO changes

The prototype changes three production source files:

| File | Change and purpose |
| --- | --- |
| `internal/controller/metal3.io/baremetalhost_controller.go` | In `actionProvisioning`, read the BMH-associated DataImage for a live-ISO deployment and pass its URL to the provisioner. |
| `pkg/provisioner/provisioner.go` | Add the configuration URL to the internal `ProvisionData` structure. |
| `pkg/provisioner/ironic/ironic.go` | Let `getConfigDrive` return the existing ISO URL for this case, and forward it on both initial deployment and deployment retry. |

The URL must pass through unchanged. Converting the payload into a new
OpenStack-style configdrive from host configuration would lose the intended
filesystem label and IBI-specific file layout.

The production implementation must additionally record input identity and
deployment ownership, and keep ordinary BMH power-management DataImage
operations from independently manipulating deployment-owned media. Those
changes are outside the measured forwarding prototype and depend on the
[ownership contract](#preferred-direction-deployment-owned-preparation-media).

A live ISO with no associated DataImage keeps its existing behavior, and
ordinary provisioning keeps its structured configdrive path. A DataImage
that is being deleted must not be attached. An API-read error must not be
treated as an absent resource and silently omit configuration. The prototype
tests cover these cases and forwarding through the actual deployment request.

Keep BMO's live-ISO omission guard for deployments that have not explicitly
requested configuration. The prototype uses a live-ISO request plus an
associated DataImage as its opt-in; maintainers must agree whether that is
sufficient or an explicit requirement needs another representation.

Once configuration is required, unsupported hardware or boot interfaces must
produce a clear pre-power-on failure. Do not fall back to a boot-only request.
The existing [Ironic ramdisk deployment][ironic-ramdisk-deploy] can warn and
ignore configdrive input when its boot interface lacks the capability; that
behaviour cannot satisfy this required-configuration contract. Agree where
the capability gate belongs and preserve legacy behaviour for requests that
do not opt in. A supported boot interface must still fail before power-on if
no usable free device is found.

The requirement must also survive the DataImage disappearing or changing
during deployment; see [the lifecycle requirements](#production-lifecycle-requirements).

`ProvisionData` is an internal Go structure. The prototype requires no new
BMH or DataImage spec field, Ironic HTTP request field, or Kubernetes RBAC
permission. BMO's executable and its Go provisioner plugin must be built
together from the same source revision and toolchain after changing their
shared internal structure.

## Ironic changes

### Preserve boot media when inserting configuration

In `ironic/drivers/modules/redfish/boot.py`, add an insertion mode to
`_insert_vmedia` and `_insert_vmedia_in_resource` that:

- Preserves already inserted media, including the boot ISO.
- Selects a known-empty compatible slot. An unknown insertion state does
  not count as empty.
- Uses the driver's existing System/Manager discovery and vendor slot
  restrictions, without hardcoding a second device or slot number.
- Reports an error when no usable slot exists instead of replacing the boot
  image or proceeding without the requested configuration.

Use that mode from the existing `_attach_configdrive` hook and remove its
type-wide USB eject. Keep the existing eject/reinsert behavior as the default
for boot-media replacement, including its existing hardware workarounds.
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

The prototype in `ironic/drivers/modules/image_utils.py` reuses the result of
the [existing MIME-type detection][ironic-image-preparation] to choose that
suffix. The helper already invokes `file(1)` to distinguish text-encoded
configuration from binary image content; the prototype adds no extra probe.
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
downloaded payload format. The stock-based lab image lacked that executable,
so the initial attempt failed before power-on.

The OpenShift Ironic image needs the `file` runtime package, or an equivalent
maintainer-approved implementation of the existing detection requirement.
For the current image build this means checking and updating
`main-packages-list.ocp` in `openshift/ironic-image`. The lab overlay added
one package-install instruction to the existing Ironic image. A shipping
change belongs in the maintained runtime image build.

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

## Production lifecycle requirements

The bootstrap prototype intentionally held the DataImage stable and checked
actual guest media independently. The following work is required before
enabling the complete automated workflow.

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
| A: configdrive delivery, preferred | Reuse Ironic's pre-power-on hook; deployment owns both images, with DataImage as immutable input. | Record ownership/identity, isolate ordinary attachment, and integrate status, deletion and teardown. |
| B: coordinated DataImage attachment | Ordinary BMH DataImage handling owns configuration and coordinates with initial live-ISO deployment. | Order attachment with boot preparation and power-on; fix identity, cleanup and finalizers. |

Option A is preferred because it reuses a demonstrated coordination point:
Ironic already inserts configdrive media after setting up the live boot image
and before power-on. It requires an explicit ownership contract; the small
delivery prototype does not itself supply that contract.

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
selecting the final contract; neither option is production-complete today.

### Preferred direction: deployment-owned preparation media

Treat preparation configuration as an immutable input consumed by the
live-ISO deployment. The following model is proposed for maintainer discussion;
it has not been implemented or validated by the bootstrap prototype:

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

This may avoid a new independently selective-detach interface for preparation,
because its two images can be managed together. It still requires reliable
identity, status, deletion and restart behaviour. If maintainers require
independent configuration replacement/ejection while the live boot remains
active, that is additional scope requiring identity-aware selective operations.

Ownership of two images does not authorize ejecting unrelated media. Any
reuse of type-wide deployment teardown needs an agreed exclusive-media
precondition, or identity-aware cleanup that preserves other devices. The
supported policy must be explicit and tested through final handoff.

### Configuration identity and status

BMO's current [`GetDataImageStatus`][bmo-dataimage-status] returns true when **any** virtual media is
inserted. A boot ISO alone is therefore insufficient evidence that the
intended configuration is attached. Ironic can also publish a different URL
from the DataImage source URL. In the successful prototype, DataImage's
attached-image status remained empty despite configuration being readable.

Define a supported association among the DataImage identity and requested
version, the Ironic-published image, and the selected device. Use that
association for status and lifecycle operations across retries and restarts.
Matching an arbitrary CD or relying on an undocumented cached filename is
not sufficient. Determine with maintainers whether existing node metadata,
status, and virtual-media APIs can express the contract, or whether an
interface extension is needed. The prototype's lack of API/CRD changes does
not settle that production decision.

### Deletion and handoff semantics

Under deployment ownership, deleting the preparation DataImage must not
trigger the ordinary CD-type-wide detach path. Its finalizer must complete
once the owning deployment has released its configuration; an unrelated
inserted boot image cannot keep that finalizer pending indefinitely. The
existing any-image status check does not meet this identity requirement.

Define what happens if configuration changes or is deleted while Ironic is
fetching or attaching it. The producer must not replace attempt data in place
and assume a running guest sees the new contents. Agree whether immutable
attempt data, a durable deployment snapshot, or an explicit requirement is
needed so a missing resource cannot turn a configuration-required deployment
into a boot-only deployment. Live ISO without requested configuration must
continue to work as it does today.

Retain published files for as long as the BMC can still fetch them. Ensure
cancellation and teardown cannot race an in-flight attachment into starting
an obsolete deployment. IBI-O must coordinate the later move from live-ISO
deployment to disk boot and normal IBI configuration. The lab
used disabled automatic cleaning; preserving the newly prepared disk through
handoff and any BMO deprovisioning/cleaning transitions still needs validation.

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

The passed test establishes support for one server/firmware combination.
Advertising multiple media slots does not by itself prove concurrent boot
and configuration delivery. Qualify the supported vendor/firmware matrix,
System- and Manager-level APIs, media-type overlap, vendor restrictions,
publication URLs, and BMC-to-Ironic connectivity and TLS trust.

The real configuration ISO can contain per-attempt credentials. Apply the
existing publication path's access and transport controls, keep credentials
out of logs, and define the artifact's retention with its producer. The lab
used a non-secret marker payload and did not qualify credential handling.

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
early enough for preparation. Those IBI-O/ISO changes are outside this
Metal3-focused proposal and were not exercised by the marker-only test.

The bootstrap result supports a small, reusable delivery enhancement. Before
finalizing the BMO feature contribution or making IBI depend on it, resolve
the ownership model and these maintainer questions:

1. Confirm the documented URL-based configdrive path's supported contract for
   prebuilt images with an IBI-specific label and payload layout.
2. Is deployment ownership the right model for preparation media, and what
   persisted state keeps ordinary BMH DataImage operations separate from it?
   Should DataImage deletion wait for teardown or request cancellation?
3. Is live ISO plus an associated DataImage the appropriate opt-in, and how
   should the requirement for configuration survive resource changes?
4. How should BMO and Ironic represent configuration identity and report
   its actual attachment without confusing it with the boot ISO?
5. Can existing deployment teardown safely release both preparation images,
   including their publications and finalizers, or are additional interfaces
   needed? Is independently selective configuration cleanup actually required?
6. Should publication naming use the existing detected format or an explicit
   format signal, and how should any such signal reach the publisher?
7. Which upstream and OpenShift release targets and hardware combinations
   should receive the changes together?

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
5. Complete the opted-in BMO delivery and agreed ownership/status/finalizer
   behaviour, then validate actual preparation, cancellation, restart recovery
   and disk-preserving handoff into normal IBI installation before enabling
   the feature. Coordinate the required component versions for platform delivery.

Required regression coverage includes live ISO with and without configuration,
ordinary provisioning, lookup failures and deleting resources, initial deploy
and retry, occupied/unknown/unavailable slots, preservation of boot media,
unchanged ISO contents, filename compatibility, rejection of required
configuration on unsupported boot interfaces, failure before power-on,
status identity, ownership isolation from ordinary DataImage handling,
updates/deletion during attachment, teardown of both owned preparation images,
finalizer release, restart recovery, and disk-preserving IBI handoff. Verify
that normal boot-media replacement retains its existing behaviour while
configuration insertion preserves the boot ISO. The delivery prototype
supplies a tested starting point; the lifecycle model and cases above remain
release gates for the full workflow.

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
