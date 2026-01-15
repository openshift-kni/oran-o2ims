# Add to HostFirmwareComponents the NIC vendor/model Info

## Summary

NIC firmware updates require vendor/model identification to filter adapters,
but there is no CR that exposes this information. Therefore users must manually
query BMC Redfish APIs to identify which firmware applies to which NIC vendor.

This enhancement proposes extending the HostFirmwareComponents CRD [1]
status.components[] with NIC metadata field: `vendor` (contains vendor/model combined).
This makes HostFirmwareComponents self-contained for firmware management
workflows without requiring correlation with other CRs.

## Goals

- Enable automated vendor-specific firmware updates using single CR
- Identify changes needed in Metal3 CRD and Ironic firmware component API
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
  - component: nic:<ID>
    url: http://firmware-repo/nic-firmware.bin
```

The `<ID>` is the identifier used to target the NIC for firmware updates.

Current HostFirmwareComponents CR status only provides component identifier and version,
requiring users to correlate with other data sources to determine vendor.

```yaml
apiVersion: metal3.io/v1alpha1
kind: HostFirmwareComponents
metadata:
  name: server-01
status:
  components:
  - component: nic:<ID>
    currentVersion: "14.32.20.04"
    # No other information
```

## Problem Statement

Current NIC firmware update workflow requires privileged BMC access or CR correlation outside the
normal firmware update flow, complicating automation and violating the design principle that only
Metal3 should communicate with BMCs.

### Use Case

Update all **Intel NIC adapters** of node server-01 to firmware version **22.5.7**

### Current Workflow

Users must manually query BMC Redfish APIs to determine vendor:

```
Step 1: List all NetworkAdapters
  └─> curl -k -u user:pass https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/
  └─> Data model: ["NIC.Slot1", "NIC.Slot2"]

Step 2: For each NetworkAdapter, query to get vendor/model
  └─> curl https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/NIC.Slot1
  └─> curl https://bmc-ip/redfish/v1/Chassis/<chassis-id>/NetworkAdapters/NIC.Slot2
  └─> Extract Manufacturer and Model properties
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", vendor: "Intel Corporation", model: "Intel(R) 25GbE XXV710"},
        {adapterId: "NIC.Slot2", vendor: "Mellanox", model: "ConnectX-5"}
      ]

Step 3: Filter for Intel adapters only
  └─> Result: ["NIC.Slot1"]

Step 4: Query HostFirmwareComponents to check current versions
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Data model:
      [
        {adapterId: "NIC.Slot1", currentVersion: "14.32.20.04"},
        {adapterId: "NIC.Slot2", currentVersion: "16.35.30.06"}
      ]
  └─> Compare NIC.Slot1 with target version (22.5.7)
  └─> Version mismatch → update required

Step 5: Create HostFirmwareComponents update for Intel adapters
  └─> cat <<EOF | kubectl apply -f -
      apiVersion: metal3.io/v1alpha1
      kind: HostFirmwareComponents
      metadata:
        name: server-01
      spec:
        updates:
        - component: nic:NIC.Slot1
          url: http://firmware-repo/intel-nic-22.5.7.bin
      EOF

Step 6: Validate firmware updates completed
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Verify status.components[].currentVersion matches 22.5.7 for NIC.Slot1
```

**Note**: Step 3 is unreliable on HPE hardware due to a known bug where the adapterId obtained from
Redfish cannot be trusted for firmware updates.

### Proposed Workflow

Extend HostFirmwareComponents CRD [1] status.components[] to include NIC metadata.

```yaml
apiVersion: metal3.io/v1alpha1
kind: HostFirmwareComponents
metadata:
  name: server-01
status:
  components:
  - component: nic:<ID>
    currentVersion: "14.32.20.04"
    vendor: "Intel Corporation/Intel(R) 25GbE XXV710"  # NEW FIELD
  - component: nic:<ID>
    currentVersion: "16.35.30.06"
    vendor: "Mellanox/ConnectX-5"         # NEW FIELD
```

Given that change in HostFirmwareComponents, the workflow becomes (same for all vendors):

```
Step 1: Query HostFirmwareComponents CR to check current versions and filter by vendor
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Parse .status.components[] array
  └─> Filter for Intel adapters by parsing vendor field (contains "Intel")
  └─> Data model from .status.components[]:
      [
        {component: "nic:<ID>", vendor: "Intel Corporation/Intel(R) 25GbE XXV710",
         currentVersion: "14.32.20.04"}
      ]
      # vendor format: "Manufacturer/Model" (primary) or "0xVVVV/0xDDDD" (fallback)
  └─> Compare currentVersion with target version from user goal (22.5.7)
  └─> Version mismatch (14.32.20.04 < 22.5.7) → update required

Step 2: Create HostFirmwareComponents update for Intel adapters
  └─> cat <<EOF | kubectl apply -f -
      apiVersion: metal3.io/v1alpha1
      kind: HostFirmwareComponents
      metadata:
        name: server-01
      spec:
        updates:
        - component: nic:<ID>  # Copy component value from Step 1 status.components[].component
          url: http://firmware-repo/intel-nic-22.5.7.bin
      EOF

Step 3: Validate firmware updates completed
  └─> kubectl get hostfirmwarecomponents server-01 -o yaml
  └─> Verify status.components[].currentVersion matches 22.5.7 for matching component
```

No manual BMC queries or CR correlation required. All information available in single CR.

## Required Pull Requests

### Ironic RFE

Proposed RFE content:

```
Summary: [RFE] Add NIC vendor/model metadata to firmware component inventory

Description:
NIC firmware updates require vendor identification to filter adapters, but the
firmware component API currently does not expose NIC metadata. Users must query
multiple APIs or correlate data to determine which firmware applies to which vendor.

Proposed API Change:

API Endpoint: GET /v1/nodes/{uuid}/firmware (or equivalent)

**Note**: This change requires a new Ironic API microversion.

Add vendor field (combines manufacturer/model or PCI vendor/device) to NIC firmware component data:

Expected Response Format:
{
  "firmware": {
    "components": [
      {
        "component": "nic:NIC.Integrated.1",
        "current_version": "14.32.20.04",
        "vendor": "Intel Corporation/Intel(R) 25GbE XXV710"  // NEW FIELD*
      }
    ]
  }
}

The single vendor field combines manufacturer and model strings, but that can be split 
into separate vendor and model fields.

Benefit: This enables automated vendor-specific firmware updates via Metal3
HostFirmwareComponents without requiring BMC access or CR correlation workflows.

Vendor field format depends on which approach succeeds:
- Primary (Redfish): "Manufacturer/Model" (e.g., "Intel Corporation/Intel(R) 25GbE XXV710")
- Fallback (IPA): "0xVVVV/0xDDDD" (e.g., "0x8086/0x1593")
- Both failed: null
Implementation Strategy:

Primary Approach - Redfish NetworkAdapter Properties:
- Query Redfish NetworkAdapter resource directly and extract vendor/model from Manufacturer/Model properties.
- Both Dell iDRAC and HPE iLO implement these fields:
  - Dell: "Manufacturer": "Intel Corporation", "Model": "Intel(R) 25GbE 2P XXV710 Adptr"
  - HPE: "Manufacturer": "Intel Corp.", "Model": "Intel(R) Ethernet Network Adapter E810-XXVDA4T"
  - Other vendors (Supermicro, Lenovo) may not populate these optional Redfish properties.

Fallback Approach - IPA Inspection Data Correlation (if Redfish returns null):
- Correlate Redfish NetworkAdapter with Ironic Python Agent (IPA) inspection data using MAC
- Vendor/product always available (IPA collects from PCI during inspection)

Important: The fallback complexity is significant and may not justify
implementation. Even if technically feasible, the added complexity may
outweigh the benefit given that both major vendors (Dell, HPE) populate
Redfish Manufacturer/Model fields. Manufacturer/Model are optional in Redfish
schema (may be null), if empty we have gracefull fallback to unknown

```

### Ironic Implementation

Repository: https://opendev.org/openstack/ironic [10]

Implement RFE from PR #1 with primary approach and optional fallback.

```
Implementation sketch:

Primary Approach (Redfish):
1. When returning firmware components, query Redfish for each NIC component:
   GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}

2. Extract metadata from NetworkAdapter resource:
   - Manufacturer
   - Model

3. If both Manufacturer and Model are available:
   - Combine as: vendor = "Manufacturer/Model"
   - Example: "Intel Corporation/Intel(R) 25GbE XXV710"
   - Return enriched component

4. If either Manufacturer or Model is null:
   - Proceed to fallback approach (if implemented)
   - If fallback not implemented: set vendor = null, log warning

Fallback Approach (IPA Correlation) - Optional:

1. Query NetworkDeviceFunctions to get MACs:
   GET /redfish/v1/Chassis/{id}/NetworkAdapters/{adapter_id}/NetworkDeviceFunctions/
   Extract: NetworkDeviceFunction.Ethernet.MACAddress

2. Query IPA inspection data:
   GET /v1/nodes/{uuid}/inventory
   Extract: interfaces[].mac_address, vendor, product

3. Correlate by MAC address:
   - Match NetworkDeviceFunction MACs with inventory.interfaces[].mac_address
   - Extract vendor/product from matched interface
   - Combine as: vendor = "0xVVVV/0xDDDD" (PCI vendor ID/device ID)
   - Example: "0x8086/0x1593"

4. Error handling:
   - MAC not found in IPA inventory: set vendor = null
   - Multiple MACs per adapter: use first matching interface
   - No MACs in NetworkDeviceFunction: set vendor = null
   - Log warnings for debugging

Final Response Format:
{
  "component": "nic:<adapter_id>",
  "current_version": "22.5.7",
  "vendor": "Intel Corporation/Intel(R) 25GbE XXV710"  // Format depends on which approach succeeded
}
```


### Metal3 Baremetal-Operator

Repository: https://github.com/metal3-io/baremetal-operator [1]

Add `Vendor` field (combines manufacturer/model or PCI vendor/device) to HostFirmwareComponents CRD
status.components[] and populate from Ironic firmware component API.

**Note**: Metal3 BareMetal Operator will need to be updated to support the new Ironic API microversion
introduced by PR #2.

```go
type FirmwareComponentStatus struct {
    Component      string   `json:"component"`
    InitialVersion string   `json:"initialVersion,omitempty"`
    CurrentVersion string   `json:"currentVersion,omitempty"`
    LastVersionFlashed string `json:"lastVersionFlashed,omitempty"`
    UpdatedAt      metav1.Time `json:"updatedAt,omitempty"`
    Vendor         string   `json:"vendor,omitempty"`         // NEW FIELD (format: "Manufacturer/Model" or "0xVVVV/0xDDDD")
}
```

No breaking change (optional field).

```
Implementation approach:
1. Query Ironic firmware component API
2. Extract vendor field from Ironic response
3. Map to HostFirmwareComponents.Status.Components[]:
   component → Component
   current_version → CurrentVersion
   vendor → Vendor (NEW, contains combined manufacturer/model or PCI IDs)
4. Update HostFirmwareComponents CR status
```

### BareMetalHost NIC.Model mismatch to HostFirmwareComponents vendor

The BareMetalHost NIC struct uses the `Model` field with space-separated PCI vendor/device IDs format
(e.g., `"0x8086 0x1572"`), as defined in [baremetalhost_types.go#L713](https://github.com/metal3-io/baremetal-operator/blob/a958720595d6ca33fffb2f925ae6669e8635b1cc/apis/metal3.io/v1alpha1/baremetalhost_types.go#L713).

**The primary approach (Redfish Manufacturer/Model strings) has a format mismatch with BareMetalHost.**
The primary approach produces human-readable strings like `"Intel Corporation/Intel(R) 25GbE XXV710"`,
while BMH uses PCI IDs like `"0x8086 0x1572"`. This inconsistency across Metal3 APIs means users would
see different vendor formats depending on which CR they query.

The fallback approach (IPA correlation with PCI IDs) matches the BMH format, providing consistency
across Metal3 APIs. Nevertheless that PCI vendor/device IDs are static, so maintaining an internal
mapping from PCI IDs to manufacturer/model names could be an option. We could either do only the fallback
or extend BareMetalHost NIC Struct

### Sushy-Tools for Libvirt

Repository: https://opendev.org/openstack/sushy-tools [21]

Enable missing Redfish NetworkAdapter emulation to support the above use case for testing without
physical hardware.

## References

- Metal3 Ironic container: https://github.com/metal3-io/ironic-image
- Metal3 API docs: https://github.com/metal3-io/baremetal-operator/blob/main/docs/api.md
- Metal3 firmware updates guide: https://book.metal3.io/bmo/firmware_updates
- Ironic firmware updates guide: https://docs.openstack.org/ironic/latest/admin/firmware-updates.html
- Ironic Redfish driver: https://docs.openstack.org/ironic/2025.1/admin/drivers/redfish.html
- Ironic NIC firmware updates spec: https://specs.openstack.org/openstack/ironic-specs/specs/not-implemented/nic-firmware-updates.html
- Ironic NIC firmware update implementation: https://opendev.org/openstack/ironic/commit/0624d19876abeb9e1e82104ab6993fb9cc860bd6
- Ironic NetworkAdapter identifiers implementation: https://review.opendev.org/c/openstack/ironic/+/972421 (not merged)
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
