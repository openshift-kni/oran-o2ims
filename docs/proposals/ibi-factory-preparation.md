<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Independent IBI Factory Preparation

```yaml
title: ibi-factory-preparation
status: Discussion
creation-date: 2026-11-02
jira: CNF-26353
```

## Summary

Introduce a namespaced `IBIPreparation` custom resource in O-Cloud Manager to
prepare multiple bare-metal servers using a previously generated IBI live ISO.
One request supplies the ISO, credentials, target BMC and SSH addresses, and a
concurrency limit. A controller coordinates one worker Job per target, with
independent progress and results.

The workflow mounts the ISO through Redfish, boots the server, monitors disk
preparation over SSH, powers the server off, and ejects the ISO. Successful
completion means the server is prepared for subsequent IBI deployment or
shipment. It does not create a cluster.

The shared live ISO relies on DHCP to configure the booted server's network.
SSH monitoring additionally requires a known address reachable from the worker;
callbacks would avoid that addressing requirement but need new ISO behavior.

**Targets must not be represented by BareMetalHost (BMH) resources while they
are being prepared. Preparation uses direct Redfish and SSH operations without
Metal3 inspection, BMO actions, or Ironic provisioning.** The only BMH access is
a read-only check that a target is not already represented by an existing BMH.
SiteConfig and the IBI Operator do not participate in preparation. The controller
and Jobs run on an O-Cloud Manager hub, which can be separate from the eventual
site hub.

This is a discussion alternative to
[Automated IBI Server Pre-Provisioning via ProvisioningRequest API](./ibi-server-pre-provisioning.md).
The API names and examples below are proposed contracts, not implemented APIs.

## Table of Contents

- [Background and motivation](#background-and-motivation)
- [Comparison with the ProvisioningRequest proposal](#comparison-with-the-provisioningrequest-proposal)
- [Goals and boundaries](#goals-and-boundaries)
- [Proposed API](#proposed-api)
- [Controller and worker responsibilities](#controller-and-worker-responsibilities)
- [Workflow](#workflow)
- [Validation and target ownership](#validation-and-target-ownership)
- [Integration with live ISO generation](#integration-with-live-iso-generation)
- [Live ISO networking and reporting trade-off](#live-iso-networking-and-reporting-trade-off)
- [SSH addressing, authentication, and trust](#ssh-addressing-authentication-and-trust)
- [Failure handling, recovery, and deletion](#failure-handling-recovery-and-deletion)
- [Security and deployment](#security-and-deployment)
- [Implementation work and validation](#implementation-work-and-validation)
- [Open decisions](#open-decisions)

## Background and motivation

IBI separates preparation from deployment:

1. Generate a seed image and reusable live ISO for a hardware/software
   configuration.
2. Boot target servers from that ISO to install RHCOS and restore the seed,
   including its cached container content, onto their disks.
3. Power off and, where appropriate, ship the prepared servers.
4. At the deployment site, register the servers as BMHs and use the existing
   IBI provisioning workflow to apply cluster-specific configuration.

Doing the expensive preparation before site deployment allows it to use factory
bandwidth and a schedule where installation time is less consequential. The
factory step needs BMC access and a way to observe preparation, but it does not
need a BMH, hardware allocation, an ImageClusterInstall (ICI), or the eventual
cluster's identity and configuration.

The [automated seed-generation workflow](./automated-seed-image-generation.md)
already provides the preceding artifact-production step. This proposal consumes
its outputs and automates preparation of the target servers. See the
[IBI user guide](../user-guide/ibi-based-cluster-provisioning.md) for the wider
factory and site deployment workflow.

Here, independent or offline preparation means independent of site cluster
provisioning. It still requires network access between the factory workers,
BMCs, booted servers, and ISO server.

## Comparison with the ProvisioningRequest proposal

The existing proposal adds preparation to a target ProvisioningRequest and
extends the IBI Operator with callbacks. It provides a possible unattended
installation path for hardware that arrives unprepared. This alternative
preserves preparation as a separate factory operation.

| Aspect | ProvisioningRequest proposal | Independent factory preparation |
| --- | --- | --- |
| Operation | Allocate, prepare, and install a target cluster | Prepare a batch of servers and power them off |
| Target representation | BMH and ICI participate in preparation | Explicit target list; BMHs must be absent |
| Execution | IBI Operator with BMO/Ironic | O-Cloud Manager controller and direct Redfish/SSH workers |
| Completion reporting | Callback from new code in the live ISO | SSH observation of the existing preparation service |
| Live host networking | DHCP for a shared ISO; customized ISO proposed for static networking | DHCP for the shared ISO; no per-node network configuration mechanism defined |
| Reporting connectivity | Booted server must reach the callback endpoint | Worker must know and reach each booted server's SSH address |
| Per-target context | Proposed callback DataImage or a customized ISO | Job inputs and referenced Secrets |
| Parallelism | Multiple cluster provisioning requests | One CR with multiple independently executed targets |
| Site handoff | Continues directly into cluster installation | Explicit later inventory registration and deployment |

Several concerns motivate the alternative:

- **Preparation on the deployment path.** Combining preparation with final
  installation moves disk installation and seed restoration onto that request's
  critical path. Seed restoration may still have benefits over a conventional
  install, but that does not preserve the offsite preparation advantage by
  itself.
- **New ISO behavior.** Callbacks require a generic client and systemd units in
  the boot ISO. Supplying a DataImage alone cannot add that behavior to an
  otherwise unmodified ISO.
- **Concurrent media assumptions.** Existing IBI deployment reads a configuration
  DataImage while booting an already prepared disk. Using a callback DataImage
  during preparation requires it to coexist with the boot ISO. BMO v0.13.2
  [attaches DataImages as virtual CDs](https://github.com/metal3-io/baremetal-operator/blob/v0.13.2/pkg/provisioner/ironic/ironic.go#L2028),
  so the existing API does not establish an independent second-device path.
  Hardware-specific additional media support is not a portable solution by
  itself.
- **Additional coordination.** The callback approach requires new ICI API and
  controller behavior, ISO generation changes, and a SiteConfig handoff. Its
  proposed hardware gate also needs restructuring: O-Cloud Manager currently
  applies the ClusterInstance after allocation, so waiting for an ICI at that
  earlier gate would introduce a dependency cycle.
- **Live host network bootstrap.** Both shared-ISO workflows depend on DHCP
  before the host can report progress. The original proposal acknowledges this
  and suggests ISO customization for static networking; neither approach defines
  an implemented per-node network configuration path for the reusable live ISO.
  The later deployment's configuration DataImage must not be assumed to provide
  networking during preparation. See the
  [networking and reporting trade-off](#live-iso-networking-and-reporting-trade-off).

The independent workflow has its own responsibilities: portable Redfish
handling, factory-to-host SSH connectivity, target identification, trust, and
durable recovery. Those responsibilities are described below rather than
delegated to the IBI Operator.

## Goals and boundaries

The proposal aims to:

- Prepare many servers with one CR, using bounded parallel execution.
- Reuse an existing ISO across targets and preparation requests.
- Report progress, completion, failure, and cleanup separately for each target.
- Preserve the offsite preparation and later site deployment boundary.
- Support management-cluster and worker restarts without blindly repeating an
  installation.
- Prevent concurrent preparation of the same physical server.
- Refuse preparation of a target already represented by a BMH.
- Discover BMC capabilities and perform media, boot, and power operations
  directly, without invoking Metal3 inspection or provisioning services.

The operation does not create BMHs, DataImages, PreprovisioningImages, NARs,
ClusterInstances, ICIs, or ProvisioningRequests. It does not allocate hardware,
apply firmware profiles, install the final cluster, or register prepared hosts
with the destination hub. These remain separate operations.

The proposal assumes the normal OpenShift hub environment, including Metal3
and its CRDs. Independence from Metal3 describes the preparation execution path:
the workers act directly on the BMC and booted server, while existing Metal3
services and resources remain outside that execution path. The read-only BMH
conflict check is an ownership safeguard.

The initial API is a Kubernetes CR. Adding a new O-RAN REST endpoint is a
separate API decision. No callback endpoint is required on the factory hub.

## Proposed API

A CRD is preferred to a ConfigMap because it provides schema validation,
immutability rules, a status subresource, conditions, and a defined lifecycle.
The initial deployment would accept requests in the configured O-Cloud Manager
namespace, keeping Jobs and credential access within that namespace.

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: IBIPreparation
metadata:
  name: factory-batch-001
  namespace: oran-o2ims
spec:
  iso:
    url: https://iso.example.com/ibi/seedgen/example-run/rhcos-ibi.iso
    # Illustrative digest; use the generated ISO's actual checksum.
    digest: sha256:0000000000000000000000000000000000000000000000000000000000000000
    caBundleRef:
      name: factory-iso-ca
      key: ca-bundle.crt
  bmcCredentialsSecretRef:
    name: factory-bmc-credentials
  bmcCABundleRef:
    name: factory-bmc-ca
    key: ca-bundle.crt
  sshCredentialsSecretRef:
    name: factory-preparation-ssh
  # Explicit factory-network policy; see the SSH trust discussion below.
  sshHostKeyPolicy: TrustOnFirstUse
  parallelism: 5
  timeouts:
    preparation: 60m
    shutdown: 5m
  targets:
    - name: server-01
      bmcAddress: https://bmc-01.factory.example.com
      sshHost: server-01.factory.example.com
    - name: server-02
      bmcAddress: https://bmc-02.factory.example.com
      sshHost: server-02.factory.example.com
      # Optional override when this BMC uses different credentials.
      bmcCredentialsSecretRef:
        name: server-02-bmc-credentials
```

The shared BMC Secret contains `username` and `password`. The SSH Secret contains
`username` (normally `core`) and an unencrypted `privateKey`; strict host-key
verification additionally requires verified `knownHosts` data. References resolve
in the preparation CR's namespace. Private credentials are never stored in the
CR or its status.

Target names are stable keys, unique within the request. Duplicate physical
targets are rejected even if they use different names. A target may identify a
specific Redfish System when its BMC exposes multiple systems; ambiguous
discovery fails validation. Per-target SSH credential overrides can be supported
using the same reference pattern if required.

The spec is immutable. A request UID identifies one batch execution, and an
explicitly recorded attempt identifies each target boot. Reapplying a completed
CR does not repeat preparation. To run the same inputs again, delete and recreate
the CR after cleanup, or create a new CR. For a regenerated ISO, change its URL
and digest in the new request; addresses and Secret references can stay the same.

### Per-target and batch status

Each `spec.targets[]` entry has a matching `status.targets[]` entry keyed by
`name`. The example shows selected proposed fields:

```yaml
status:
  observedGeneration: 1
  summary:
    total: 2
    running: 1
    succeeded: 1
    failed: 0
  targets:
    - name: server-01
      phase: Succeeded
      stage: Completed
      message: Preparation completed; server powered off and ISO ejected
      completionTime: "2026-11-02T13:20:00Z"
    - name: server-02
      phase: Running
      stage: MonitoringPreparation
      message: Waiting for the preparation service to finish
      jobRef:
        name: ibi-preparation-example-server-02
```

Additional fields record start time, attempt identity, discovered system UUID
and serial number, the live boot ID, verified ISO digest, and SSH host-key
fingerprint. They identify what was prepared and distinguish it from a later
reinstall. Credential values and raw SSH command output do not belong in status.

Target phases are `Pending`, `Running`, `Succeeded`, `Failed`, `Cancelled`, and
`RecoveryRequired`. Running stages identify validation, media mounting, boot,
SSH connection, preparation monitoring, shutdown, and media ejection.

Batch conditions distinguish execution completion from success. `Completed=True`
means all targets have settled and required cleanup is complete;
`Succeeded=True` means every target succeeded. A failure on one target does not
stop independent targets. `RecoveryRequired` keeps the outstanding cleanup or
ownership issue visible rather than presenting the batch as finished.

## Controller and worker responsibilities

The controller owns validation, frozen run inputs, target reservations,
concurrency, Job creation, status aggregation, and cleanup. It uses deterministic
resource identities derived from the CR UID and target name, and adopts existing
resources only after verifying ownership and the expected execution inputs.

Each worker Job handles one target's Redfish and SSH operations. Workers can be
implemented as a new subcommand of the existing manager binary, using Go
Redfish and SSH clients. This avoids requiring a separately maintained utility
image or depending on external command-line programs in the manager image.

Progress cannot depend solely on Pod logs or a final Job exit code. A candidate
reporting mechanism is a controller-created ConfigMap per target containing
durable checkpoints. A worker may update only its assigned checkpoint; the
controller validates its run/target ownership and projects progress into the CR
status. The checkpoint also preserves intent before external side effects and
the observed boot and host-key identities. The precise reporting schema remains
an implementation design decision.

The controller creates at most `spec.parallelism` active workers for a request.
A controller-wide concurrency limit also bounds work across multiple requests.
Starting 100 preparations therefore does not require 100 CRs or one worker that
serially processes the whole list. One batch can queue and execute all targets
while retaining individual results.

An artifact verification Job can validate the shared ISO once before workers
start, avoiding a full verification download per target. The BMCs still need to
fetch their boot media; HTTP server capacity and factory network bandwidth
determine useful parallelism.

## Workflow

1. **Validate the batch inputs.** Validate the spec, referenced credentials,
   HTTPS trust, and ISO digest. Freeze the effective non-secret configuration and
   credential/trust snapshots for this run. Invalid shared inputs prevent any
   target from being booted.
2. **Validate and reserve each target.** Discover the Redfish System and media
   capabilities, establish its physical identity, reject conflicting requests
   or BMH references, and acquire an exclusive reservation. A target-specific
   failure is reported separately while other valid targets remain eligible.
3. **Launch workers within the concurrency limits.** Immediately before making
   destructive changes, revalidate the target identity and ownership. A worker
   waits for a confirmed powered-off state, mounts the selected ISO, sets a
   one-time boot override, and powers the server on.
4. **Wait for SSH.** Connect using the supplied address and authentication
   Secret under the selected host-verification policy. Record the current live
   boot ID and compare the host's physical identity with the Redfish System
   identity, using supported UUID/serial normalization. An SSH address pointing
   at a different server must not establish preparation success. Correlate the
   observation with the newly initiated boot; ambiguous identity requires
   investigation rather than accepting another server's service result.
5. **Monitor preparation.** Poll
   `install-rhcos-and-restore-seed.service` for evidence that it actually ran in
   this boot and reached terminal success or failure. An inactive service with
   a default success result is insufficient. Retain bounded journal diagnostics
   for the current boot. A particular English log message is not the sole
   completion contract.
6. **Shut down and eject.** After verified success, request graceful shutdown,
   confirm the BMC reports power off, eject the ISO, and confirm media removal.
   Only then mark the target `Succeeded` and release its reservation.
7. **Complete the batch.** Retain the per-target preparation record and outcome.
   The servers remain powered off. A separate inventory workflow may later
   create BMHs, apply selection labels, and provision clusters at the site.

The preparation timeout includes boot and waiting for SSH, rather than starting
only once a connection succeeds. Separate bounded shutdown and cleanup budgets
prevent an unresponsive BMC from being mistaken for successful completion.

## Validation and target ownership

### BMH ownership exclusion

A target being prepared must not also be controlled through a BMH. The
controller checks `baremetalhosts.metal3.io` across namespaces and rejects a
target whose BMC/System is already referenced. This includes available,
allocated, detached, and deleting BMHs; being unallocated or detached does not
make concurrent ownership acceptable.

Matching must account for BMH transport prefixes, equivalent addresses, and
Redfish System identity. A simple comparison of the request URL with the raw
BMH address is insufficient. Ambiguous matches are rejected rather than assuming
the target is safe.

A failed or unauthorized lookup blocks preparation; an access error must not be
interpreted as proof that no BMH exists. This check only reads existing BMHs. It
does not create or modify Metal3 resources, ask Metal3 to inspect the target, or
use BMO/Ironic to perform BMC actions. Its RBAC must grant only the read operations
required by the selected client, including list/watch if using a cached client.

This check covers the management cluster where O-Cloud Manager runs. It cannot
discover BMHs on arbitrary other hubs or atomically prevent an unrelated
controller from creating one after the check. Factory inventory must keep these
targets out of other management systems until preparation and cleanup finish.
The implementation should recheck local conflicts before destructive operations;
it must not claim that a one-time lookup provides a global ownership lock.

### Exclusive preparation ownership

Concurrent CRs or BMC address aliases must not cause the same physical server to
be prepared twice. Resolve the System identity and acquire a durable, atomic
reservation before starting a worker. A Kubernetes Lease is one candidate for
that reservation; its exact identity and scope need to cover all requests served
by the controller.

Controller or worker restarts do not release the reservation. A timer expiring
does not prove the previous installer has stopped. Release ownership only after
cleanup confirms the target is no longer executing an attempt, or through an
explicit recovery procedure that fences that attempt. This also gates a new
request after deletion or failure.

## Integration with live ISO generation

The existing workflow already supplies the main prerequisites:

| Existing capability | Use in preparation |
| --- | --- |
| `liveISO.sshKey` | Authorizes the factory worker's public SSH login key for the live environment |
| `seedGenerationStatus.isoURL` | Supplies the previously published boot ISO |
| `seedGenerationStatus.isoDigest` | Supplies the expected artifact checksum |
| `seedGenerationStatus.isoServerCACertRef` | Supplies HTTPS trust material that can be copied or referenced for factory use |
| Job, SSH, and frozen-input handling | Provides patterns for implementation and testing; helpers should be shared where appropriate |

**The SSH-based workflow requires no new executable behavior in the live ISO.**
The existing `liveISO.sshKey` capability must be configured when generating it.
The worker observes the existing preparation service over SSH. No callback
script, callback endpoint, per-target ignition override, or second DataImage is
required.

The new credential requirement is on the consumer side: create a Secret
containing the **private SSH key paired with the public key passed as
`liveISO.sshKey`**. This is a factory login credential, separate from the private
key used to upload ISOs to the HTTPS server. The private key is mounted only into
the workers that need it; it is never embedded in the ISO.

For example, create the worker's authentication Secret from an existing key:

```bash
oc -n oran-o2ims create secret generic factory-preparation-ssh \
  --from-literal=username=core \
  --from-file=privateKey=/path/to/factory_id_ed25519
```

The corresponding public key is the value supplied to `liveISO.sshKey` when
generating the ISO. An ISO that does not authorize that key needs to be
regenerated using the existing setting. The ISO generation Job does not need
access to the new private-key Secret. Strict verification also requires the
host-trust material described below; the example above only supplies login
authentication.

The same login key pair can be used across regenerated ISOs. Changing an ISO URL
or digest does not require changing the target list or Secret references.
Optional future provenance metadata, such as the configured SSH public-key
fingerprint, could help detect configuration mistakes, but is not required for
the basic workflow. Successful authentication still depends on using an ISO that
authorizes the supplied key.

The artifact handoff is explicit; preparation does not require the source
ProvisioningRequest to remain present. If its CA ConfigMap is owned by that
request, copy the bundle into an independently managed ConfigMap before deleting
the source request. The published ISO must also remain available for the entire
batch.

Validate the actual ISO bytes against the supplied digest, rather than trusting
only a checksum sidecar. Publication must provide an immutable URL: the BMC
fetches the ISO separately, and a worker's earlier hash check cannot guarantee
the bytes of a later fetch if the server rewrites that URL. ISO generation
already publishes separate paths per generation run.

Direct Redfish mounting retains BMC-specific ISO-fetch requirements. The BMC
must resolve the URL, reach the server, and satisfy its HTTPS trust requirements.
A CA supplied to the worker does not install that CA into the BMC. This design
does not rely on Ironic to cache or republish the ISO, so any factory proxy or
network accommodation must be explicit.

## Live ISO networking and reporting trade-off

### Shared DHCP prerequisite

Both the SSH-based workflow described here and the original proposal's shared
callback ISO need working networking inside the booted live environment. With
the currently generated reusable ISO, **DHCP is a prerequisite for both
workflows**: neither defines a mechanism to supply and apply per-node IP,
route, DNS, or interface configuration during preparation. An `sshHost` value
identifies where to connect; it does not configure the server's network.

The preparation network must provide DHCP on an interface usable by the live
environment, together with the routing and name resolution needed for its
network operations. Configuring the BMC's management interface or successfully
mounting the ISO does not configure the booted host's network. The host needs
network access for preparation itself, including seed-image retrieval, as well
as for SSH or callback reporting.

This differs from the existing **site deployment** stage, where a server boots
from its already prepared disk. The IBI Operator reads the BMH's referenced
nmstate Secret and includes node-specific networking in the configuration ISO
attached through a DataImage. See the
[IBI Operator's configuration generation](https://github.com/openshift/image-based-install-operator/blob/4bc5f2a691cab4b02638dfd6bd567cd0cf2f9408/controllers/imageclusterinstall_controller.go#L1104).
That later configuration path does not automatically configure the earlier live
ISO boot. Using a second image for preparation networking would need an explicit
reader in the live environment and a validated media-delivery mechanism.

The original proposal already discusses
[host network configuration for the callback](./ibi-server-pre-provisioning.md#host-network-configuration-for-the-callback).
It identifies DHCP as the default and proposes ISO build-time customization for
DHCP-less environments, potentially requiring per-host or per-site ISO variants.
This is an implementation and environment prerequisite to resolve for both
workflows, rather than a DHCP dependency introduced solely by SSH. The current
O-Cloud Manager ISO generation interface does not expose per-node network
configuration. Supporting DHCP-less preparation would require a separate design
and corresponding ISO-generation or boot-time configuration work.

### SSH versus callbacks

SSH adds an addressing and connectivity requirement beyond DHCP. The worker must
know a reachable address for each booted target. DHCP reservations with DNS are
one option; a supplied reserved IP or a reliable lease-discovery mechanism could
also work. A hostname is optional, but a deterministically identified endpoint
is necessary. Address discovery is not currently part of this proposal's worker
contract, so the initial example requires `sshHost` for each target.

Callbacks reverse the reporting connection: the booted server connects to a
known endpoint. The controller does not need to discover the server's assigned
address or establish an inbound connection to it. This can simplify networks
with dynamic leases or restrictions on worker-to-host traffic, while retaining
the host's need for DHCP, routing, and any required DNS.

| Consideration | SSH monitoring | Callback reporting |
| --- | --- | --- |
| Shared ISO network bootstrap | Requires DHCP | Requires DHCP |
| Per-target address known to controller | Required, supplied directly or through future discovery | Not required for reporting |
| Reporting connection direction | Worker to each host | Each host to the callback endpoint |
| Existing ISO support | Uses the existing SSH key and preparation service | Requires generic callback logic in the ISO |
| Authentication and identity | Login key, SSH host verification, and physical-target correlation | Secure per-attempt identity, credentials, and result correlation |
| Per-target reporting context | Worker configuration and Secrets | A defined way to deliver context to the live environment |

Independent factory preparation and the reporting transport are separate design
choices. A standalone batch CR with direct BMC operations could use callbacks
without involving BMHs or Metal3 services. This draft starts with SSH to build
on existing ISO support, but the per-target addressing and inbound connectivity
cost must be weighed against callback integration and secure context delivery.
The reporting choice remains open for discussion.

## SSH addressing, authentication, and trust

Keep these three concerns separate:

- **Addressing:** each target supplies an SSH hostname/IP independently of its
  BMC address. DHCP reservations or factory inventory can keep it stable across
  reinstalls. BMC discovery of host addresses is not a portable assumption.
- **Login authentication:** the ISO authorizes a public key; the worker uses the
  matching private key from its Secret. The credentials are reusable across
  compatible ISO builds.
- **Server verification:** the SSH server's host key identifies the remote
  endpoint. A successful public-key login does not independently verify that
  identity. A fresh live boot may present a new host key.

Two proposed verification policies are:

- `Strict`: require verified `knownHosts` data or an explicitly configured host
  certificate trust mechanism. This is the proposed default. Certificate
  enrollment/bootstrap would need a separate design; it is not supplied merely
  by referencing a CA.
- `TrustOnFirstUse`: an explicit option for a controlled factory network.
  After initiating the target's boot, record the first observed host key and
  enforce it for the rest of that attempt, including worker restarts. Persist
  the pin in the target checkpoint. It does not independently authenticate the
  first connection, so it is a deployment trust decision, not equivalent to
  strict verification. A newly authorized attempt establishes a new trust scope.

The example opts into the latter policy to illustrate repeated factory test
runs without pinning fingerprints to individual ISO builds. The policy choices
and default require review. Do not embed a shared SSH server private key in a
reusable ISO to manufacture a stable identity for every server.

## Failure handling, recovery, and deletion

Each target has a durable attempt ID and checkpoints. Record intent before
changing power, boot selection, or virtual media; on recovery, reconcile that
intent with the actual BMC and live-host state. If the outcome is ambiguous,
report `RecoveryRequired` instead of automatically booting a fresh installation.

Temporary BMC/SSH network failures can be retried within the attempt's deadline.
An SSH disconnect alone is not evidence that preparation failed. Reconnect and
check the current boot and preparation result. A stale success from an earlier
boot, a service that never started, or loss of connectivity cannot establish
completion.

Use controlled Job retry behavior. A Pod replacement must resume the attempt
from checkpoints, not rerun an unconditional mount/boot sequence. The initial
scope does not require automatic full reinstalls after a terminal preparation
failure; an operator can create a new request for failed targets after cleanup.

On failure, collect bounded diagnostics and attempt shutdown and media removal.
Keep the target `Failed` even if that cleanup succeeds. If power/media state
cannot be made safe, retain the reservation and report `RecoveryRequired` with
an actionable message. Successful targets remain successful while other targets
finish or require investigation.

Deletion is explicit cancellation. Once deletion starts, normal reconciliation
must not launch Jobs or start new BMC actions. A finalizer stops workers, waits
for Pods to terminate, and performs bounded BMC cleanup using the frozen
credentials. Stopping a Job does not stop the installer already running on the
server, so cancellation must resolve that external state before releasing the
reservation. Required checkpoints and credentials remain available until cleanup
is complete. The cleanup deadline and administrative recovery path must be
defined before implementation; a timeout must not silently unlock a possibly
active server.

Published ISO artifacts are inputs owned outside the preparation request and
are not deleted by its finalizer. Request-owned Jobs, checkpoints, credential
snapshots, and execution RBAC are removed after they are no longer needed.
Retain terminal status and diagnostics for a configurable period while the CR
exists; bound that retention independently of target reservations.

## Security and deployment

Creating an `IBIPreparation` authorizes disk replacement on the listed targets.
Restrict access to factory operators who may perform that operation. Jobs mount
only the resolved credentials for their assigned target, with no credentials in
arguments, logs, status, or diagnostic output. Freeze referenced values for an
attempt so a Secret rotation cannot silently change an in-flight execution.

Use unprivileged workers and the existing supported image where possible.
Network access must allow workers to reach Redfish and SSH endpoints, and BMCs
to reach the ISO server. The booted server does not need to reach a new hub
callback service. No host networking or privileged container is inherently
required by the Redfish/SSH workflow.

Controller RBAC covers its own API, namespaced Jobs and execution resources,
credential/trust inputs, and the selected reservation mechanism. Worker API
access should be restricted to its own reporting/checkpoint resource; it does
not need Kubernetes access to read Secrets already mounted by the kubelet.
BMH conflict checking needs read-only permission across the namespaces being
checked. Tie each grant to the actual client operations, including cached
list/watch reads where applicable.

Portable Redfish handling discovers Systems, Managers, virtual media, and reset
capabilities. Require an explicit System selection for ambiguous BMCs. Vendor
and firmware support needs a tested matrix; a Dell-specific resource path or
reset action is not a universal contract. Capability failures must be reported
before starting preparation where they can be detected in advance.

## Implementation work and validation

These are candidate work packages for discussion, not implementation estimates:

| Work package | Main changes |
| --- | --- |
| API and validation | New preparation types and CRD, immutable inputs, Secret/reference rules, shared validators, and admission/controller integration |
| Factory controller | Target identity and reservations, BMH conflict checks, frozen inputs, bounded concurrency, Job ownership, and status aggregation |
| Worker | Portable Redfish lifecycle, SSH connection and trust, current-boot completion checks, checkpoint reporting, and cleanup |
| Runtime integration | Worker subcommand, scoped RBAC, bundle generation, controller setup, and must-gather collection |
| Recovery and testing | Restart/cancellation/failure scenarios, exclusivity, diagnostics, and real hardware validation |
| Documentation | Public examples for preparation CRs, private-key Secrets, factory networking/trust, and the later site handoff |

Useful tests include:

- Direct Redfish/SSH preparation without creating Metal3 resources or invoking
  Metal3 inspection or provisioning services.
- Rejection of a target referenced by a BMH, including address aliases;
  rejection when the ownership check cannot be completed.
- Multiple targets progressing concurrently under request and controller limits,
  with one target failing while others succeed.
- Two requests racing to claim the same physical target.
- Credential failures, unsupported virtual media, and ISO reachability,
  certificate, or digest failures.
- Missing DHCP, routing or DNS failures in the live environment, and a reachable
  BMC with an unreachable booted host. Include DHCP address changes and blocked
  worker-to-host SSH connectivity when validating addressing assumptions.
- Completion monitoring for the current boot: success, service failure, service
  never started, stale logs, delayed SSH, connection loss, and an SSH address
  pointing at a different physical server.
- Worker and controller restart at each external side-effect boundary, without
  duplicate installation or loss of the SSH pin.
- Shutdown/eject failure, cancellation during preparation, and bounded cleanup
  that retains ownership when external state is unresolved.
- Recreating the same request with a regenerated ISO and the same login key.
- A real preparation followed by a separate, existing IBI cluster deployment.
- Regression coverage for existing seed generation and cluster provisioning.

Validate representative Redfish implementations before claiming broad hardware
support. Exercise the epic's parallel preparation scale target on factory
network and ISO-serving infrastructure. Final effort estimates should follow
agreement on the API, trust policy, reservation/reporting design, and supported
hardware rather than reuse the original proposal's estimate.

## Open decisions

1. Confirm independent factory preparation as the scope of CNF-26353, and whether
   inline preparation during final cluster provisioning remains a separate need.
2. Agree on the `IBIPreparation` API name, supported namespace scope, and
   controller-wide concurrency policy.
3. Confirm DHCP as the shared-ISO preparation prerequisite and decide whether
   DHCP-less operation needs a separate network-configuration design. Select SSH
   or callbacks for reporting, accounting for per-target addressing, connection
   direction, and ISO/context-delivery changes.
4. Choose the initial SSH trust policies/default and how strict host identities
   can be enrolled without tying every request to a particular ISO build.
5. Specify canonical target identity, atomic reservation, and local BMH matching,
   including the operational boundary with other management clusters.
6. Finalize the worker checkpoint/reporting contract, cancellation cleanup
   deadline, diagnostic retention, and administrative recovery procedure.
7. Establish the initial BMC/firmware support matrix and any ISO proxy/network
   prerequisites. These must be tested without assuming Ironic behavior.

These decisions can be reviewed before splitting development into stories.
Neither this proposal nor its examples change the existing provisioning APIs.
