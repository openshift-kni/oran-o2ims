# Expose adapterId in HardwareData CRD

## Summary

NIC firmware updates require Redfish NetworkAdapter resource identifiers, but this information is
not exposed through any k8s CRD. This forces users to manually query BMC Redfish APIs
out-of-band to correlate NICs with Redfish identifiers, which breaks the design principle that only
Metal3 should communicate with BMCs.

This enhancement proposes extending the HardwareData CRD [1] NIC struct with one field:
`adapterId` which will lift this limitation.

## Goals

- Enable automated firmware updates without requiring out-of-band BMC access
- Identify changes needed in Metal3 CRD and Ironic inspection process
- No breaking changes to existing APIs

## Firmware Update Background

HostFirmwareComponents CRD [1] requires Redfish-specific component identifiers:

```yaml
apiVersion: metal3.io/v1alpha1
kind: HostFirmwareComponents
metadata:
  name: server-01
spec:
  updates:
  - component: nic:<NetworkAdapter-ID>
    url: http://firmware-repo/nic-firmware.bin
```

The `<NetworkAdapter-ID>` is the Redfish NetworkAdapter resource identifier.

Component identifier format: `nic:<NetworkAdapter-ID>`

Vendor-specific adapter naming conventions:
- Dell iDRAC: `NIC.Integrated.1`, `NIC.Slot.2` (FQDD format without port suffix) [17][18]
- HPE iLO: `AD0700`, `NIC1` (adapter identifiers) [19][20]

**Note:** NIC firmware is managed at the adapter level (not per-port). All ports on
the same physical adapter share the same firmware version and are updated
together.

**Note:** Firmware versions are available in the HostFirmwareComponents CR status, indexed by the same
`adapterId` value.


```yaml
apiVersion: metal3.io/v1alpha1
kind: HostFirmwareComponents
metadata:
  name: server-01
status:
  components:
  - component: nic:NIC.Integrated.1
    currentVersion: "14.32.20.04"
```

## Problem Statement

Current NIC firmware update workflow requires privileged BMC access outside the normal API path,
violating the design principle that only Metal3 should communicate with BMCs.

### Use Case

User goal: Update all Intel NIC interfaces of node server-01 to firmware version 22.5.7

### Current Workflow

Users must manually query BMC Redfish APIs to figure out the network adapter id:

```
Step 1: List all NetworkAdapters
  └─> curl -k -u user:pass https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/
  └─> Data model: ["NIC.Slot1", "NIC.Slot2"]

Step 2: For each NetworkAdapter, query NetworkDeviceFunctions to get MACs
  └─> curl https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/NIC.Slot1/NetworkDeviceFunctions/
  └─> curl https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/NIC.Slot2/NetworkDeviceFunctions/
  └─> For each function, extract Ethernet.MACAddress (Redfish does not provide vendor)
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", macs: ["0c:42:a1:8a:7c:a4", "0c:42:a1:8a:7c:a5"]},
        {adapterId: "NIC.Slot2", macs: ["50:7c:6f:4b:9a:28"]}
      ]

Step 3: Query HardwareData to determine vendor by correlating MACs
  └─> kubectl get hardwaredata server-01 -o yaml
  └─> Match MAC addresses to find vendor from model field (0x8086=Intel, 0x15b3=Mellanox)
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", macs: ["0c:42:a1:8a:7c:a4", "0c:42:a1:8a:7c:a5"],
         vendor: "Intel"},
        {adapterId: "NIC.Slot2", macs: ["50:7c:6f:4b:9a:28"],
         vendor: "Mellanox"}
      ]

Step 4: Filter for Intel adapters only
  └─> Result: ["NIC.Slot1"]

Step 5: Query HostFirmwareComponents to check current versions
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", vendor: "Intel", currentVersion: "14.32.20.04"},
        {adapterId: "NIC.Slot2", vendor: "Mellanox", currentVersion: "16.35.30.06"}
      ]
  └─> Compare NIC.Slot1 with target version (22.5.7)
  └─> Version mismatch → update required

Step 6: Create HostFirmwareComponents update for Intel adapters
  └─> kubectl apply -f hostfirmwarecomponents.yaml
  └─> spec.updates:
      - component: nic:NIC.Slot1
        url: http://firmware-repo/intel-nic-22.5.7.bin

Step 7: Validate firmware updates completed
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Verify status.components[].currentVersion matches 22.5.7 for NIC.Slot1
```

### Proposed Workflow

Extend the NIC inventory structure in the Metal3 HardwareData CRD [1] to provide adapterId.

```yaml
apiVersion: metal3.io/v1alpha1
kind: HardwareData
metadata:
  name: server-01
  namespace: openshift-machine-api
status:
  hardware:
    nics:
    - ip: 10.6.34.10
      mac: 0c:42:a1:8a:7c:a4
      model: 0x8086 0x1593
      adapterId: "NIC.Slot1" # NEW FIELD
      ...
    - mac: 0c:42:a1:8a:7c:a5
      model: 0x8086 0x1593
      adapterId: "NIC.Slot1" # Same adapter, different port
      ...
    - mac: 50:7c:6f:4b:9a:28
      model: 0x15b3 0x1015
      adapterId: "NIC.Slot2" # Different adapter (Mellanox)
      ...
```

 Given that change in HardwareData, the workflow becomes:

```
Step 1: Query HardwareData CR to build adapter inventory with vendor
  └─> kubectl get hardwaredata server-01 -o yaml
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", macs: ["0c:42:a1:8a:7c:a4", "0c:42:a1:8a:7c:a5"],
         vendor: "Intel"},
        {adapterId: "NIC.Slot2", macs: ["50:7c:6f:4b:9a:28"],
         vendor: "Mellanox"}
      ]

Step 2: Filter for Intel adapters only
  └─> Result: ["NIC.Slot1"]

Step 3: Query HostFirmwareComponents to check current versions
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", vendor: "Intel", currentVersion: "14.32.20.04"},
        {adapterId: "NIC.Slot2", vendor: "Mellanox", currentVersion: "16.35.30.06"}
      ]
  └─> Compare NIC.Slot1 with target version (22.5.7)
  └─> Version mismatch → update required

Step 4: Create HostFirmwareComponents update for Intel adapters
  └─> kubectl apply -f hostfirmwarecomponents.yaml
  └─> spec.updates:
      - component: nic:NIC.Slot1
        url: http://firmware-repo/intel-nic-22.5.7.bin

Step 5: Validate firmware updates completed
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Verify status.components[].currentVersion matches 22.5.7 for NIC.Slot1
```

No manual BMC queries required. All information available via Kubernetes API.

## Required Pull Requests

### PR #1: Ironic RFE Submission

Location: https://bugs.launchpad.net/ironic/+filebug
- File bug report with 'rfe' tag describing NIC inspection enhancement
- Example RFE format: https://bugs.launchpad.net/ironic-python-agent/+bug/1635253
- If spec requested, submit to https://opendev.org/openstack/ironic-specs

Proposed RFE content:

```
Summary: [RFE] Add NIC adapter identifier to inventory API

Description:
NIC firmware updates require Redfish NetworkAdapter resource identifiers, but
the inventory API currently does not expose this information. Users must
manually query BMCs out-of-band to correlate NICs with Redfish resources.

Proposed API Change:

API Endpoint: GET /v1/nodes/{uuid}/inventory

Add adapter_id field to network interface inventory data:

Expected Response Format:
{
  "inventory": {
    "interfaces": [
      {
        "name": "eno1np0",
        "mac_address": "0c:42:a1:8a:7c:a4",
        "vendor": "0x15b3",
        "product": "0x1015",
        "speed_mbps": 25000,
        "ipv4_address": "10.6.34.10",
        "adapter_id": "NIC.Integrated.1"  // NEW FIELD
      }
    ]
  }
}

Implementation: Ironic enriches IPA data with Redfish NetworkAdapter identifiers
by querying BMC Redfish APIs and correlating by MAC address.

Benefit: This enables automated firmware updates via Metal3 HostFirmwareComponents
without requiring privileged BMC access outside normal workflows.
```

### PR #2: Ironic Implementation

Repository: https://opendev.org/openstack/ironic [10]

Implement RFE from PR #1: Add `adapter_id` field to `GET /v1/nodes/{uuid}/inventory` response.

```
Implementation approach:
1. IPA collects OS-level NIC data (name, MAC, vendor/product)
2. Ironic queries BMC Redfish APIs:
   GET /redfish/v1/Chassis/{id}/NetworkAdapters/
   GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}/NetworkDeviceFunctions/
3. Match NetworkDeviceFunction.Ethernet.MACAddress with IPA MAC address
4. Extract NetworkAdapter.Id → add as adapter_id to inventory.interfaces[]
```

### PR #3: Metal3 Baremetal-Operator

Repository: https://github.com/metal3-io/baremetal-operator [1]

Add `AdapterId` field to HardwareData CRD NIC struct and populate from Ironic inventory.

```go
type NIC struct {
    Name      string `json:"name,omitempty"`
    Model     string `json:"model,omitempty"`
    MAC       string `json:"mac,omitempty"`
    IP        string `json:"ip,omitempty"`
    SpeedGbps int    `json:"speedGbps,omitempty"`
    AdapterId string `json:"adapterId,omitempty"`  // NEW FIELD
}
```

No breaking change (optional field).

```
Implementation approach:
1. Switch from GET /v1/nodes/{uuid} to GET /v1/nodes/{uuid}/inventory (requires API version 1.81+)
2. Extract inventory.interfaces[] instead of node.extra['hardware_details']['network']
3. Map existing fields + new adapter_id field to HardwareData.Status.Hardware.NICs[]:
   inventory.interfaces[].name → Name
   inventory.interfaces[].mac_address → MAC
   inventory.interfaces[].vendor + product → Model
   inventory.interfaces[].ipv4_address → IP
   inventory.interfaces[].speed_mbps/1000 → SpeedGbps
   inventory.interfaces[].adapter_id → AdapterId (NEW)
4. Update HardwareData CR status
```

### PR #4: Sushy-Tools for Libvirt

Repository: https://opendev.org/openstack/sushy-tools [21]

Emulate Redfish NetworkAdapter and NetworkDeviceFunction resources for libvirt VMs to enable
testing without physical hardware.

**Endpoints to implement (if not already there):**
- `GET /redfish/v1/Chassis/{id}/NetworkAdapters/`
- `GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}`
- `GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}/NetworkDeviceFunctions/`
- `GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}/NetworkDeviceFunctions/{function_id}`

**Minimum response fields needed:**
- NetworkAdapter response must include: `Id`, `Controllers[0].FirmwarePackageVersion`
- NetworkDeviceFunction response must include: `Id`, `Ethernet.MACAddress`

**Stretch goal:**
Emulate firmware version updates via Redfish SimpleUpdate for testing HostFirmwareComponents workflow.

## Notes

- **Redfish Resource Types**: This enhancement exposes NetworkAdapter identifiers from
  `/redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}`, not NetworkInterface identifiers from
  `/redfish/v1/Systems/{id}/NetworkInterfaces/`.

- **Mixed-Vendor NIC Support**: The adapterId field enables safe firmware updates in mixed-vendor
  environments by correlating vendor-specific firmware to the correct physical adapter.

- **PCI Address**: PCI address is not required for firmware updates and was intentionally left out
  to minimize implementation complexity.

## References

- Metal3 Ironic container: https://github.com/metal3-io/ironic-image
- Metal3 API docs: https://github.com/metal3-io/baremetal-operator/blob/main/docs/api.md
- Metal3 firmware updates guide: https://book.metal3.io/bmo/firmware_updates
- Ironic firmware updates guide: https://docs.openstack.org/ironic/latest/admin/firmware-updates.html
- Ironic Redfish driver: https://docs.openstack.org/ironic/2025.1/admin/drivers/redfish.html
- Ironic NIC firmware updates spec: https://specs.openstack.org/openstack/ironic-specs/specs/not-implemented/nic-firmware-updates.html
- Ironic NIC firmware update implementation: https://opendev.org/openstack/ironic/commit/0624d19876abeb9e1e82104ab6993fb9cc860bd6
- Ironic upstream: https://github.com/openstack/ironic
- Ironic inspection docs: https://docs.openstack.org/ironic/2025.1/admin/inspection/index.html
- Redfish inspection spec: https://specs.openstack.org/openstack/ironic-specs/specs/12.0/redfish-inspection.html
- DSP2062 Firmware Update White Paper: https://www.dmtf.org/sites/default/files/standards/documents/DSP2062_1.0.2.pdf
- DSP2046 Resource and Schema Guide: https://www.dmtf.org/sites/default/files/standards/documents/DSP2046_2025.1.pdf
- Redfish Schema: https://redfish.dmtf.org/schemas/DSP0266_1.15.1.html

1. Metal3 baremetal-operator: https://github.com/metal3-io/baremetal-operator
10. Ironic opendev: https://opendev.org/openstack/ironic
13. Ironic API version history: https://docs.openstack.org/ironic/latest/contributor/webapi-version-history.html
17. Dell iDRAC Redfish scripting: https://github.com/dell/iDRAC-Redfish-Scripting
18. Dell iDRAC API guide: https://www.dell.com/support/manuals/en-us/idrac9-lifecycle-controller-v4.x-series/idrac9_4.00.00.00_redfishapiguide_pub
19. HPE iLO Redfish services: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo6
20. HPE firmware updates: https://developer.hpe.com/blog/hpe-firmware-updates-part-3-the-redfish-update-service/
21. Sushy-tools: https://docs.openstack.org/sushy-tools/latest/
