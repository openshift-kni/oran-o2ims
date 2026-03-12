<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Server Onboarding

- [Server Onboarding](#server-onboarding)
  - [Overview](#overview)
  - [Namespace and Resource Pool Organization](#namespace-and-resource-pool-organization)
  - [BMC Credentials Secret](#bmc-credentials-secret)
  - [Preprovisioning Network Data Secret](#preprovisioning-network-data-secret)
  - [BareMetalHost Resource](#baremetalhost-resource)
    - [Required Labels](#required-labels)
    - [Interface Labels](#interface-labels)
    - [Resource Selector Labels](#resource-selector-labels)
    - [Annotations](#annotations)
    - [BMH Spec](#bmh-spec)
  - [Hardware Inspection](#hardware-inspection)
  - [Complete Example](#complete-example)
  - [Deploying via GitOps](#deploying-via-gitops)

## Overview

Server onboarding is the process of registering bare-metal hosts with the O-Cloud Manager
so they can be allocated for cluster provisioning. Each server is represented by a Metal3
`BareMetalHost` (BMH) CR along with supporting Secrets for BMC credentials and network
configuration.

The onboarding process involves:

1. Creating a namespace to serve as a resource pool for the servers.
2. Creating a BMC credentials Secret for each server.
3. Creating a preprovisioning network data Secret with nmstate configuration for host
   inspection.
4. Creating a BareMetalHost CR with the required labels and annotations.
5. Allowing Metal3 to inspect the host and populate hardware inventory data. This
   process typically takes several minutes per host.

Once onboarded, servers are available for selection by the Metal3 hardware plugin when
processing [ProvisioningRequests](./cluster-provisioning.md). The plugin matches servers
based on [resource selector criteria](./template-overview.md#hardwaretemplate) defined
in the HardwareTemplate.

## Namespace and Resource Pool Organization

Servers are grouped into resource pools using the `resources.clcm.openshift.io/resourcePoolId`
label on the BMH. BMHs that share the same `resourcePoolId` value belong to the same pool,
regardless of which namespace they are in.

The recommended convention is to use a namespace per resource pool, with the namespace
name matching the `resourcePoolId` value. Servers within a resource pool should be
homogeneous (same hardware type and configuration) so that any server in the pool can
satisfy a provisioning request targeting that pool.

Create a namespace for your servers:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: dell-xr8620t-pool
```

## BMC Credentials Secret

Each server requires a Secret containing the BMC username and password. The Secret must
be in the same namespace as the BareMetalHost:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bmc-secret-dell-xr8620t-sno1
  namespace: dell-xr8620t-pool
type: Opaque
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
```

## Preprovisioning Network Data Secret

Each server requires a Secret containing an nmstate network configuration for the
preprovisioning phase. This configuration is built into the Metal3 discovery ISO used
during hardware inspection to provide network connectivity. Without it, the host will
not have networking and inspection will fail.

The Secret must be named `network-data-<bmh-name>` (e.g., `network-data-dell-xr8620t-sno1`
for a BMH named `dell-xr8620t-sno1`). This naming convention is required by the Metal3
hardware plugin, which uses it to restore the Secret reference during deprovisioning.

The Secret is referenced by the BMH's `preprovisioningNetworkDataName` field.

> [!WARNING]
> Interface names in the nmstate configuration must use the standard device name as
> assigned by the RHEL CoreOS kernel (e.g., `ens3f0`, `eno1np0`), not an alternate name.
> The device names may differ from those assigned by other Linux distributions. Using
> alternate names can cause inspection failures, particularly with VLAN configurations.

Basic example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: network-data-dell-xr8620t-sno1
  namespace: dell-xr8620t-pool
type: Opaque
stringData:
  nmstate: |
    interfaces:
    - name: ens3f0
      type: ethernet
      state: up
      ipv4:
        enabled: true
        dhcp: true
      ipv6:
        enabled: false
    dns-resolver:
      config:
        server:
        - 198.51.100.20
```

VLAN example:

```yaml
stringData:
  nmstate: |
    interfaces:
    - name: eno1np0
      type: ethernet
      state: up
    - name: eno1np0.310
      type: vlan
      state: up
      vlan:
        base-iface: eno1np0
        id: 310
      ipv4:
        enabled: true
        dhcp: true
      ipv6:
        enabled: false
    dns-resolver:
      config:
        server:
        - 198.51.100.20
```

## BareMetalHost Resource

The BareMetalHost CR represents a physical server. The O-Cloud Manager uses labels and
annotations on the BMH to manage server identity, selection criteria, and interface
mappings.

### Required Labels

The following labels are required for the O-Cloud Manager to recognize a BMH as a
managed server:

| Label | Description |
|---|---|
| `resources.clcm.openshift.io/siteId` | Identifies the physical site where the server is located |
| `resources.clcm.openshift.io/resourcePoolId` | Identifies the resource pool (typically matches the namespace) |

Both labels must be present for the server to be considered O-Cloud managed and eligible
for allocation.

### Interface Labels

Interface labels use the `interfacelabel.clcm.openshift.io/` prefix to associate
user-defined labels with physical NIC names on the server:

```yaml
interfacelabel.clcm.openshift.io/boot-interface: ens3f0
interfacelabel.clcm.openshift.io/data-interface: ens3f1
interfacelabel.clcm.openshift.io/sriov-1: ens2f0
interfacelabel.clcm.openshift.io/sriov-2: ens2f1
```

With the exception of `boot-interface`, these labels are optional and user-defined — the
label keys are opaque strings as far as the O-Cloud Manager is concerned. The labels are
attached to the corresponding interfaces in the inventory resource data for user
reference, and can be used to match interfaces in the ClusterInstance defaults.

For example, if the ClusterInstance defaults define:

```yaml
nodes:
  - nodeNetwork:
      interfaces:
        - name: eno1
          label: boot-interface
        - name: eth1
          label: data-interface
```

Then the BMH's `interfacelabel.clcm.openshift.io/boot-interface` and
`interfacelabel.clcm.openshift.io/data-interface` labels are used to resolve the actual
NIC names and MAC addresses on that specific server.

> [!NOTE]
> The `boot-interface` label is required if the BMH's `spec.bootMACAddress` is not set.
> The hardware plugin uses this label to determine the boot NIC's MAC address from the
> hardware inventory. If `spec.bootMACAddress` is already set, the `boot-interface` label
> is optional.

### Resource Selector Labels

Resource selector labels use the `resourceselector.clcm.openshift.io/` prefix and
provide custom criteria for matching servers to provisioning requests. These labels are
matched against the `resourceSelector` field in the
[HardwareTemplate](./template-overview.md#hardwaretemplate):

```yaml
resourceselector.clcm.openshift.io/server-type: XR8620t
resourceselector.clcm.openshift.io/server-colour: blue
resourceselector.clcm.openshift.io/server-id: dell-xr8620t-sno1
resourceselector.clcm.openshift.io/subnet: "192.0.2.0"
```

These labels are user-defined — you can create any key-value pairs that make sense for
your environment (e.g., rack location, hardware generation, deployment zone).

### Annotations

The following annotations provide additional metadata:

| Annotation | Description |
|---|---|
| `resourceinfo.clcm.openshift.io/description` | Human-readable description of the server |
| `resourceinfo.clcm.openshift.io/partNumber` | Part number or asset identifier |
| `resourceinfo.clcm.openshift.io/groups` | Comma-separated list of group memberships |

These annotations are optional and are exposed through the O-Cloud inventory API.

### BMH Spec

Key fields in the BareMetalHost spec:

| Field | Description |
|---|---|
| `spec.online` | Set to `false` for initial onboarding. The hardware plugin manages power state during provisioning. |
| `spec.bmc.address` | BMC/iDRAC address in Redfish format (e.g., `idrac-virtualmedia+https://<ip>/redfish/v1/Systems/System.Embedded.1`) |
| `spec.bmc.credentialsName` | Name of the BMC credentials Secret |
| `spec.bmc.disableCertificateVerification` | Set to `true` if the BMC uses a self-signed certificate |
| `spec.bootMACAddress` | MAC address of the boot NIC. Optional if `boot-interface` label is set. |
| `spec.preprovisioningNetworkDataName` | Name of the network data Secret containing nmstate configuration for inspection. |

## Hardware Inspection

After a BareMetalHost is created, the Metal3 Bare Metal Operator automatically inspects
the host via its BMC to discover hardware details. The inspection results are stored in a
`HardwareData` CR in the same namespace as the BMH.

The HardwareData CR contains:

- CPU architecture, model, and thread count
- Total RAM
- Storage devices (type, size, model, vendor)
- Network interfaces (model, MAC address, speed)
- System manufacturer and product name

This data is used by the hardware plugin for
[hardware data selectors](./template-overview.md#hardwaretemplate) in the
HardwareTemplate `resourceSelector`. For example, a selector like
`hardwaredata/num_threads;>=: "64"` is evaluated against the HardwareData CR for each
candidate BMH.

You can view a server's hardware inventory:

```console
oc get hardwaredata.metal3.io <bmh-name> -n <namespace> -o yaml
```

## Complete Example

The following example shows all the resources needed to onboard a single server:

```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: dell-xr8620t-pool
---
apiVersion: v1
kind: Secret
metadata:
  name: network-data-dell-xr8620t-sno1
  namespace: dell-xr8620t-pool
type: Opaque
stringData:
  nmstate: |
    interfaces:
    - name: ens3f0
      type: ethernet
      state: up
      ipv4:
        enabled: true
        dhcp: true
      ipv6:
        enabled: false
    dns-resolver:
      config:
        server:
        - 198.51.100.20
---
apiVersion: v1
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
kind: Secret
metadata:
  name: bmc-secret-dell-xr8620t-sno1
  namespace: dell-xr8620t-pool
type: Opaque
---
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  labels:
    resources.clcm.openshift.io/siteId: local-west
    resources.clcm.openshift.io/resourcePoolId: dell-xr8620t-pool
    resourceselector.clcm.openshift.io/server-colour: blue
    resourceselector.clcm.openshift.io/server-type: XR8620t
    resourceselector.clcm.openshift.io/server-id: dell-xr8620t-sno1
    interfacelabel.clcm.openshift.io/boot-interface: ens3f0
    interfacelabel.clcm.openshift.io/data-interface: ens3f1
    interfacelabel.clcm.openshift.io/sriov-1: ens2f0
    interfacelabel.clcm.openshift.io/sriov-2: ens2f1
  annotations:
    resourceinfo.clcm.openshift.io/description: "XR8620t server with Blue label"
    resourceinfo.clcm.openshift.io/partNumber: "00001"
    resourceinfo.clcm.openshift.io/groups: "groupA, groupB"
  name: dell-xr8620t-sno1
  namespace: dell-xr8620t-pool
spec:
  online: false
  bmc:
    address: idrac-virtualmedia+https://198.51.100.10/redfish/v1/Systems/System.Embedded.1
    credentialsName: bmc-secret-dell-xr8620t-sno1
    disableCertificateVerification: true
  bootMACAddress: 02:00:00:00:00:01
  preprovisioningNetworkDataName: network-data-dell-xr8620t-sno1
```

Additional sample files are available under
[inventory](../samples/git-setup/clustertemplates/inventory/).

## Deploying via GitOps

In a production environment, server inventory is typically managed through GitOps. The
inventory resources (namespace, secrets, and BMH CRs) are stored in a Git repository and
synced to the hub cluster via ArgoCD.

See [GitOps Layout and Setup](./gitops-layout-and-setup.md) for the recommended
directory structure and ArgoCD configuration.
