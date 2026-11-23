<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Metal3 support for live ISO boot with configuration media

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
XR8620t with iDRAC 7.30.30.51. Its production source changes are small:
**five files, 69 added lines and 24 removed lines**, excluding tests and
packaging. This establishes that the first-boot delivery gap can be closed
with a small extension of existing code. Completing the status, cleanup,
and recovery contract requires additional work, described below; those
changes are not included in the prototype's line count.

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
   provisioning, citing hardware limitations with concurrent virtual media.
2. BMO's separate DataImage attachment path runs during power management of
   an already provisioned host that is currently off. Ironic's initial
   live-ISO deployment has already powered on the host before this path
   normally runs. Pre-creating a DataImage therefore did not attach it for
   the first boot in the stock-platform experiment.
3. Ironic's configdrive attachment code ejects USB-capable media before
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

The important ordering already exists in Ironic: its ramdisk deployment
calls `boot.prepare_instance` before powering on. The Redfish boot
implementation already calls `_attach_configdrive` after inserting the boot
ISO. The proposal connects BMO to that hook and makes the hook safe for
concurrent media; it does not introduce a new deployment engine.

## Proposed workflow

1. The producer, initially the IBI Operator (IBI-O), creates the per-attempt
   configuration ISO and its DataImage before requesting the live-ISO
   deployment. The DataImage uses the BMH's name and namespace.
2. The producer requests deployment through the existing BMH live-ISO and
   power controls. For IBI preparation, it must arrange a host state that
   permits actual provisioning; the usual `externallyProvisioned: true`
   handoff for an already prepared IBI server is a different stage.
3. BMO reads the configuration URL and supplies it through the existing
   Ironic deployment request's `configdrive` field, alongside the boot ISO.
4. Ironic stages and inserts the live ISO through its existing boot path.
5. Ironic fetches the configuration ISO, publishes it with a usable filename,
   and inserts it into a free compatible slot while preserving the boot ISO.
6. After successful preparation of both media, Ironic sets the boot device
   and powers on the server using its existing deployment sequence.
7. The live guest locates the configuration by filesystem label, reads its
   contents, and runs the IBI preparation workflow. IBI-O monitors actual
   preparation completion and coordinates subsequent cleanup and handoff.

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

A live ISO with no associated DataImage keeps its existing behavior, and
ordinary provisioning keeps its structured configdrive path. A DataImage
that is being deleted must not be attached. An API-read error must not be
treated as an absent resource and silently omit configuration. The prototype
tests cover these cases and forwarding through the actual deployment request.

The prototype uses the presence of a live-ISO request and DataImage as its
opt-in. The production contract must also say how configuration remains
**required** if that resource disappears or changes during deployment; see
[the lifecycle requirements](#production-lifecycle-requirements).

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

This matters on the tested iDRAC because both System-level slots advertise
CD, DVD, and USB capabilities. A USB request does not identify a distinct
physical device, and ejecting all media of that type can remove the boot ISO.
The existing `ramdisk_boot_configdrive` capability check still applies.

### Publish configuration ISOs with an ISO filename

The first corrected lab image reached the second-slot insertion, but iDRAC
rejected Ironic's extensionless `configdrive-<node UUID>` URL with
`IDRAC.2.9.RAC0786`, "Unable to attach the folder of HTTP or HTTPS type."

In `ironic/drivers/modules/image_utils.py`, detect an ISO payload and publish
it as `configdrive-<node UUID>.iso`. This decision is based on the payload's
detected format, not the suffix of its source URL. Preserve its bytes and
keep the current naming for other binary formats.

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

### Configuration identity and status

BMO's current `GetDataImageStatus` returns true when **any** virtual media is
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

### Updates, deletion, and selective cleanup

The BMH and DataImage reconcilers must agree on which operation owns the
configuration media. A DataImage deletion or replacement must eject only
that configuration, preserve the live boot ISO when still needed, and remove
its finalizer when the configuration is gone even if the boot ISO remains.
Existing CD-type-wide detachment and the any-image status check do not meet
this contract when both media are present.

Define what happens if configuration changes or is deleted while Ironic is
fetching or attaching it. The producer must not replace attempt data in place
and assume a running guest sees the new contents. Agree whether immutable
attempt data, a durable deployment snapshot, or an explicit requirement is
needed so a missing resource cannot turn a configuration-required deployment
into a boot-only deployment. Live ISO without requested configuration must
continue to work as it does today.

Whole-deployment teardown may intentionally eject both images. Keep that
distinct from configuration-only cleanup, and retain published files for as
long as the BMC can still fetch them. IBI-O must coordinate the later move
from live-ISO deployment to disk boot and normal IBI configuration. The lab
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

The first-boot patch can be reviewed as a small, reusable BMO/Ironic
enhancement, accompanied by the runtime image fix and regression tests.
Before the IBI feature depends on it, resolve these maintainer questions:

1. Is live ISO plus an associated DataImage the appropriate opt-in, and how
   should the requirement for configuration survive resource changes?
2. How should BMO and Ironic represent configuration identity and report
   its actual attachment without confusing it with the boot ISO?
3. What interface supports selective detach and publication cleanup across
   both the deployment and DataImage management paths?
4. Which upstream and OpenShift release targets and hardware combinations
   should receive the changes together?

Required regression coverage includes live ISO with and without configuration,
ordinary provisioning, lookup failures and deleting resources, initial deploy
and retry, occupied/unknown/unavailable slots, preservation of boot media,
unchanged ISO contents, filename compatibility, failure before power-on,
status identity, selective deletion, restart recovery, and disk-preserving
IBI handoff. The delivery prototype supplies a tested starting point for this
work; the lifecycle cases remain release gates for the full workflow.
