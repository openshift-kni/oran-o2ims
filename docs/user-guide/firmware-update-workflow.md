<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Firmware Update Workflow

- [Firmware Update Workflow](#firmware-update-workflow)
  - [Overview](#overview)
  - [HardwareProfile](#hardwareprofile)
  - [Day-0: Firmware During Initial Provisioning](#day-0-firmware-during-initial-provisioning)
    - [Workflow](#workflow)
    - [CR Relationship](#cr-relationship)
    - [Monitoring Day-0 Progress](#monitoring-day-0-progress)
  - [Day-2: Firmware Updates on Provisioned Clusters](#day-2-firmware-updates-on-provisioned-clusters)
    - [Triggering a Day-2 Update](#triggering-a-day-2-update)
    - [Day-2 Workflow](#day-2-workflow)
    - [Monitoring Day-2 Progress](#monitoring-day-2-progress)
  - [Status Conditions Reference](#status-conditions-reference)
  - [Timeouts](#timeouts)
  - [Failure Handling](#failure-handling)
    - [Timeout Failures](#timeout-failures)
    - [Firmware Application Failures](#firmware-application-failures)
    - [Retry After Failure](#retry-after-failure)

## Overview

The O-Cloud Manager coordinates firmware and BIOS updates on bare-metal hosts through
the Metal3 hardware plugin. Firmware updates can occur in two contexts:

- **Day-0 (initial provisioning):** When a cluster is first
  [provisioned](./cluster-provisioning.md) via a ProvisioningRequest, the hardware plugin
  applies the firmware and BIOS settings defined in the HardwareProfile before cluster
  installation begins.
- **Day-2 (post-provisioning update):** After a cluster is running, firmware and BIOS
  settings can be updated by changing the HardwareProfile referenced in the
  ProvisioningRequest.

Both flows use the same underlying mechanism: the hardware plugin translates
HardwareProfile settings into Metal3 `HostFirmwareSettings` and
`HostFirmwareComponents` CRs, which the Metal3 Bare Metal Operator (BMO) applies to
the host.

## HardwareProfile

A HardwareProfile CR defines the desired firmware and BIOS state for a class of servers.
All fields are optional — specify only what you need to manage:

- **`spec.bios.attributes`** — BIOS settings to apply (e.g., `SriovGlobalEnable`,
  `WorkloadProfile`, `AcPwrRcvryUserDelay`).
- **`spec.biosFirmware`** — Target BIOS firmware version and download URL.
- **`spec.bmcFirmware`** — Target BMC/iDRAC firmware version and download URL.
- **`spec.nicFirmware`** — Target NIC firmware versions and download URLs.

Example:

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: HardwareProfile
metadata:
  name: dell-xr8620t-bios-2.3.5-bmc-7.10.70.10
  namespace: oran-o2ims
spec:
  bios:
    attributes:
      SysProfile: Custom
      WorkloadProfile: TelcoOptimizedProfile
      SriovGlobalEnable: Enabled
      AcPwrRcvryUserDelay: 120
  biosFirmware:
    version: 2.3.5
    url: https://example.com:8888/firmware/xr8620t/BIOS_JDR1R_WN64_2.3.5.EXE
  bmcFirmware:
    version: 7.10.70.10
    url: https://example.com:8888/firmware/xr8620t/iDRAC-with-Lifecycle-Controller_Firmware_W4NV9_WN64_7.10.70.10_A00.EXE
```

The HardwareProfile can be specified in two places:

1. **[HardwareTemplate](./template-overview.md#hardwaretemplate)** —
   `spec.nodeGroupData[].hwProfile` sets the default profile for a node group.
2. **ProvisioningRequest** —
   `spec.templateParameters.hwTemplateParameters.nodeGroupData.<name>.hwProfile`
   overrides the HardwareTemplate default.

When both are set, the ProvisioningRequest value takes precedence.

## Day-0: Firmware During Initial Provisioning

### Workflow

When a ProvisioningRequest is created, the following sequence occurs:

1. **HardwareTemplate rendering** — The O-Cloud Manager resolves the HardwareProfile
   for each node group (from the ProvisioningRequest override or HardwareTemplate
   default) and creates a `NodeAllocationRequest` CR.

2. **BMH selection** — The Metal3 hardware plugin selects available BareMetalHosts
   matching the [resource selector criteria](./template-overview.md#hardwaretemplate)
   (labels and hardware data).

3. **AllocatedNode creation** — For each selected BMH, the plugin creates an
   `AllocatedNode` CR referencing the target HardwareProfile.

4. **Firmware diff computation** — The plugin compares the HardwareProfile's desired
   state against the host's current state:
   - **BIOS settings:** Compares `spec.bios.attributes` against the host's
     `HostFirmwareSettings` status. If any setting differs, BIOS update is needed.
   - **Firmware versions:** Compares `spec.biosFirmware.version`,
     `spec.bmcFirmware.version`, and `spec.nicFirmware[].version` against the host's
     `HostFirmwareComponents` status. If any version differs, firmware update is needed.

5. **Metal3 CR updates** — If updates are needed, the plugin:
   - Updates the `HostFirmwareSettings` CR with the desired BIOS attributes.
   - Updates the `HostFirmwareComponents` CR with the firmware versions and download
     URLs.

6. **Firmware application** — The Metal3 Bare Metal Operator detects the changes,
   downloads the firmware, applies the updates, and reboots the host as needed.

7. **Validation** — After the host comes back, the plugin validates that the firmware
   versions and BIOS settings match the desired state.

8. **Completion** — Once all nodes pass validation, hardware provisioning is marked
   complete and cluster installation begins.

> [!NOTE]
> If the host's firmware and BIOS already match the HardwareProfile, no updates are
> applied and provisioning proceeds immediately.

### CR Relationship

The following diagram shows how CRs relate during firmware provisioning:

```text
ProvisioningRequest
  └─ references ClusterTemplate
       └─ references HardwareTemplate
            └─ specifies HardwareProfile per node group

NodeAllocationRequest (created by O-Cloud Manager)
  └─ contains node groups with hwProfile references
       └─ AllocatedNode (created by Metal3 plugin, one per host)
            ├─ spec.hwProfile → HardwareProfile CR
            ├─ maps to BareMetalHost
            │    ├─ HostFirmwareSettings (BIOS attributes)
            │    └─ HostFirmwareComponents (firmware versions + URLs)
            └─ status.hwProfile (set on completion)
```

### Monitoring Day-0 Progress

Monitor the ProvisioningRequest status conditions:

```console
oc get provisioningrequests.clcm.openshift.io <name> -o yaml
```

Key conditions during Day-0 firmware provisioning:

| Condition | Status | Reason | Meaning |
|---|---|---|---|
| `HardwareTemplateRendered` | True | Completed | HardwareTemplate validated and NodeAllocationRequest created |
| `HardwareProvisioned` | False | InProgress | BMH selection and allocation in progress |
| `HardwareProvisioned` | True | Completed | All nodes allocated |
| `HardwareConfigured` | False | InProgress | Firmware/BIOS updates being applied |
| `HardwareConfigured` | True | Completed | All firmware/BIOS updates applied and validated |

To monitor individual node status, check the AllocatedNode CRs:

```console
oc get allocatednodes.plugins.clcm.openshift.io -A
oc get allocatednodes.plugins.clcm.openshift.io <name> -n <namespace> -o yaml
```

To check the Metal3 CRs directly:

```console
# BIOS settings status
oc get hostfirmwaresettings.metal3.io <bmh-name> -n <bmh-namespace> -o yaml

# Firmware component versions
oc get hostfirmwarecomponents.metal3.io <bmh-name> -n <bmh-namespace> -o yaml
```

## Day-2: Firmware Updates on Provisioned Clusters

### Triggering a Day-2 Update

To update firmware on a running cluster, change the HardwareProfile referenced in the
ProvisioningRequest:

1. Create the new HardwareProfile CR (if it doesn't already exist) with the desired
   firmware versions and BIOS settings. See [sample profiles](../samples/git-setup/clustertemplates/hardwareprofiles/)
   for examples.

2. Update the ProvisioningRequest to reference the new profile:

```yaml
spec:
  templateParameters:
    hwTemplateParameters:
      nodeGroupData:
        controller:
          hwProfile: dell-xr8620t-bios-2.6.3-bmc-7.20.30.50
```

```console
oc patch provisioningrequests.clcm.openshift.io <name> --type merge \
  -p '{"spec":{"templateParameters":{"hwTemplateParameters":{"nodeGroupData":{"controller":{"hwProfile":"dell-xr8620t-bios-2.6.3-bmc-7.20.30.50"}}}}}}'
```

The O-Cloud Manager detects the change and propagates it through the
NodeAllocationRequest to the Metal3 hardware plugin. For a step-by-step walkthrough
with example status output, see
[Switching to a new hardware profile](./cluster-configuration.md#switching-to-a-new-hardware-profile).

### Day-2 Workflow

The Day-2 flow differs from Day-0 in several important ways:

1. **Serial execution** — Nodes are updated one at a time, with master nodes updated
   before worker nodes. Each node must complete its update and rejoin the cluster before
   the next node is updated.
   <!-- TODO: Update when parallel worker node updates are implemented for MNO clusters -->

2. **Host reboot** — Since the host is running a cluster workload, the plugin creates a
   `HostUpdatePolicy` CR and adds a `reboot.metal3.io` annotation to the BMH to trigger
   a controlled reboot after firmware is staged.

3. **Node readiness check** — After reboot, the plugin waits for the Kubernetes node to
   rejoin the cluster and reach Ready state before proceeding to the next node.

The detailed sequence for each node:

1. **Diff computation** — The plugin compares the new HardwareProfile against the host's
   current firmware/BIOS state (same as Day-0).

2. **Metal3 CR updates** — Updates `HostFirmwareSettings` and/or
   `HostFirmwareComponents` with the new desired state.

3. **Change validation** — Waits for Metal3 BMO to detect and validate the changes
   (via `ChangeDetected` and `Valid` status conditions on the Metal3 CRs).

4. **Reboot trigger** — Adds the `reboot.metal3.io` annotation to the BMH, causing
   BMO to apply the firmware and reboot the host.

5. **Post-reboot validation** — Validates that firmware versions and BIOS settings match
   the new profile and that the Kubernetes node is Ready.

6. **Next node** — Proceeds to the next node in the group.

### Monitoring Day-2 Progress

The ProvisioningRequest `HardwareConfigured` condition tracks the overall Day-2 update:

```console
oc get provisioningrequests.clcm.openshift.io <name> -o jsonpath='{.status.conditions[?(@.type=="HardwareConfigured")]}'
```

| Condition | Status | Reason | Meaning |
|---|---|---|---|
| `HardwareConfigured` | False | InProgress | Firmware update in progress |
| `HardwareConfigured` | True | Completed | All nodes updated and validated |
| `HardwareConfigured` | False | TimedOut | Update exceeded timeout |
| `HardwareConfigured` | False | Failed | Update failed on a node |

The NodeAllocationRequest `Configured` condition provides more detail:

```console
oc get nodeallocationrequests.plugins.clcm.openshift.io -A -o yaml
```

Individual AllocatedNode CRs show per-node status:

```console
oc get allocatednodes.plugins.clcm.openshift.io -A \
  -o custom-columns=NAME:.metadata.name,HWPROFILE:.status.hwProfile,CONFIGURED:.status.conditions[*].reason
```

When a node's `status.hwProfile` matches `spec.hwProfile`, the update for that node is
complete.

To verify that firmware was actually applied, check the Metal3 CRs:

```console
# Verify BIOS firmware version
oc get hostfirmwarecomponents.metal3.io <bmh-name> -n <bmh-namespace> \
  -o jsonpath='{range .status.components[*]}{.component}: {.currentVersion}{"\n"}{end}'

# Verify BIOS settings
oc get hostfirmwaresettings.metal3.io <bmh-name> -n <bmh-namespace> \
  -o jsonpath='{.status.settings}'
```

## Status Conditions Reference

### ProvisioningRequest Conditions

| Condition | Context | Description |
|---|---|---|
| `HardwareTemplateRendered` | Day-0 | HardwareTemplate validated and NodeAllocationRequest created |
| `HardwareProvisioned` | Day-0 | BMH allocation and initial provisioning status |
| `HardwareNodeConfigApplied` | Day-0 | Node configuration (BMC, MAC addresses) applied to ClusterInstance |
| `HardwareConfigured` | Day-0 and Day-2 | Firmware/BIOS configuration status |

### AllocatedNode Conditions

| Condition | Description |
|---|---|
| `Provisioned` | Node has been allocated and BMH selected |
| `Configured` | Firmware/BIOS configuration status for this specific node |

### Condition Reasons

| Reason | Meaning |
|---|---|
| `Completed` | Operation completed successfully |
| `InProgress` | Operation is ongoing |
| `ConfigurationUpdateRequested` | Day-2 update has been requested (AllocatedNode) |
| `ConfigurationApplied` | Firmware/BIOS successfully applied and validated |
| `TimedOut` | Operation exceeded the configured timeout |
| `Failed` | Operation failed (see condition message for details) |

## Timeouts

Hardware provisioning and configuration share a single timeout value, configured in the
HardwareTemplate:

```yaml
spec:
  hardwareProvisioningTimeout: "90m"
```

If not specified, the default timeout is 90 minutes.

The timeout applies independently to each phase:

- **Day-0 provisioning** (`HardwareProvisioned`) — Time allowed for BMH selection and
  allocation.
- **Day-0/Day-2 configuration** (`HardwareConfigured`) — Time allowed for firmware/BIOS
  updates to complete across all nodes.

> [!NOTE]
> For Day-2 updates, the timeout clock resets when a new HardwareProfile change is
> detected, allowing a fresh timeout window for the new configuration.

## Failure Handling

### Timeout Failures

When a timeout occurs:

- The `HardwareProvisioned` or `HardwareConfigured` condition is set to
  `Status=False, Reason=TimedOut`.
- The ProvisioningRequest `provisioningPhase` is set to `failed`.
- Any in-progress BMH annotations are cleared.

### Firmware Application Failures

If the Metal3 Bare Metal Operator fails to apply firmware:

- The BMH enters an error state (`OperationalStatus=error`).
- The plugin tolerates transient errors for up to 5 minutes.
- If the error persists, the AllocatedNode `Configured` condition is set to
  `Status=False, Reason=Failed` with an error message.
- The failure propagates up to the NodeAllocationRequest and ProvisioningRequest.

Common causes of firmware application failures:

- Firmware download URL is unreachable from the BMC.
- Firmware file is corrupted or incompatible with the hardware.
- BMC credentials are invalid or expired.
- Host is unreachable on the management network.

### Retry After Failure

For **hardware provisioning** timeouts or failures: delete and recreate the
ProvisioningRequest.

For **hardware configuration** (Day-2) timeouts or failures: update the
ProvisioningRequest spec to trigger a retry. Changing the HardwareProfile (even to the
same value with other spec changes) resets the timeout and restarts the configuration
process.
