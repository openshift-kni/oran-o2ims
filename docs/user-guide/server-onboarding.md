<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Server Onboarding

This guide describes how to define the infrastructure hierarchy for your
O-Cloud deployment and how to register bare-metal hosts for cluster
provisioning.

- [Overview](#overview)
- [Data Model Relationships](#data-model-relationships)
- [Infrastructure Hierarchy](#infrastructure-hierarchy)
  - [Location CRs](#location-crs)
  - [OCloudSite CRs](#ocloudsite-crs)
  - [ResourcePool CRs](#resourcepool-crs)
- [Server Inventory](#server-inventory)
  - [Namespace Organization](#namespace-organization)
  - [BMC Credentials Secret](#bmc-credentials-secret)
  - [Preprovisioning Network Data Secret](#preprovisioning-network-data-secret)
  - [BareMetalHost CR](#baremetalhost-cr)
    - [Required Labels](#required-labels)
    - [Interface Labels](#interface-labels)
    - [Resource Selector Labels](#resource-selector-labels)
    - [Annotations](#annotations)
    - [BMH Spec Fields](#bmh-spec-fields)
  - [Hardware Inspection](#hardware-inspection)
- [Validation](#validation)
  - [Check Infrastructure Hierarchy CRs](#check-infrastructure-hierarchy-crs)
  - [Check BareMetalHosts](#check-baremetalhosts)
  - [Verify via API](#verify-via-api)
- [Complete Example](#complete-example)
- [Deploying via GitOps](#deploying-via-gitops)

**Note:** The resources in this guide can be applied directly with `oc apply`
or managed via GitOps. For GitOps-based management, see
[GitOps Repository Layout and Setup](./gitops-layout-and-setup.md).

## Overview

Server onboarding is the process of defining the infrastructure hierarchy
and registering bare-metal hosts with the O-Cloud Manager so they can be
allocated for cluster provisioning.

The infrastructure hierarchy consists of four levels:

```text
Location (geographic place)
    └── OCloudSite (logical site at a location)
            └── ResourcePool (group of resources)
                    └── Resource (BareMetalHost/server)
```

Each level is defined by a Custom Resource (CR) in the hub cluster, except for
Resources which are discovered from BareMetalHost CRs via labels.

The onboarding process has two parts:

**Infrastructure Hierarchy** (organizational, created once):

1. Creating Location, OCloudSite, and ResourcePool CRs to define the
   geographic and organizational structure.

**Server Inventory** (per-server, created for each host):

1. Creating a namespace for the server resource pool.
2. Creating a BMC credentials Secret for each server.
3. Optionally creating a preprovisioning network data Secret with nmstate
   configuration for host inspection.
4. Creating a BareMetalHost CR with the required labels and annotations.
5. Allowing Metal3 to inspect the host and populate hardware inventory data.
   This process typically takes several minutes per host.

Once onboarded, servers are available for selection by the O-Cloud Manager
when processing [ProvisioningRequests](./cluster-provisioning.md). Servers are
matched based on
[resource selector criteria](./template-overview.md#hwmgmtdefaults) defined in
each node group's `resourceSelector` under the ClusterTemplate `hwMgmtDefaults`.

## Data Model Relationships

The following diagram shows how the CRs relate to each other through their
fields:

![Data Model Relationships](../images/data-model-relationships.svg)

### Key Relationships

| Source CR | Field | Target CR Field |
|-----------|-------|-----------------|
| OCloudSite | `spec.globalLocationName` | Location `metadata.name` |
| ResourcePool | `spec.oCloudSiteName` | OCloudSite `metadata.name` |
| BareMetalHost | label `resources.clcm.openshift.io/resourcePoolName` | ResourcePool `metadata.name` |

### Identifiers in API Responses

| CR Type | CR Identifier | API Response Identifier |
|---------|---------------|------------------------|
| Location | `metadata.name` | `globalLocationId` = `metadata.name` (string) |
| OCloudSite | `metadata.name` | `oCloudSiteId` = `metadata.uid` (UUID) |
| ResourcePool | `metadata.name` | `resourcePoolId` = `metadata.uid` (UUID) |

## Infrastructure Hierarchy

The infrastructure hierarchy defines the geographic and organizational
structure of your O-Cloud deployment using Location, OCloudSite, and
ResourcePool CRs. These CRs are created once in the `oran-o2ims` namespace
and are shared across all servers. They must be created before onboarding
servers.

### Location CRs

Locations represent physical or logical places where O-Cloud Sites can be
deployed.

```yaml
apiVersion: ocloud.openshift.io/v1alpha1
kind: Location
metadata:
  # The name serves as the globalLocationId in API responses
  name: east-datacenter
  namespace: oran-o2ims
spec:
  description: "Primary east coast data center facility"

  # At least ONE of: coordinate, civicAddress, or address is required
  coordinate:
    latitude: "38.8951"
    longitude: "-77.0364"
    altitude: "100.5"  # optional

  civicAddress:
    - caType: 1    # Country (ISO 3166-1)
      caValue: "US"
    - caType: 3    # State/Province
      caValue: "Virginia"
    - caType: 6    # City
      caValue: "Ashburn"
    - caType: 22   # Street name
      caValue: "Technology Way"
    - caType: 26   # Building number
      caValue: "123"

  address: "123 Technology Way, Ashburn, VA 20147, USA"

  extensions:
    region: "us-east"
    tier: "primary"
```

#### Location Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `metadata.name` | Yes | Used as `globalLocationId` in API responses. Must be unique. |
| `description` | No | Detailed description |
| `coordinate` | One of three | GeoJSON-compatible coordinates |
| `civicAddress` | One of three | RFC 4776 civic address elements |
| `address` | One of three | Human-readable address string |
| `extensions` | No | Custom key-value metadata |

Apply the Location CR:

```bash
oc apply -f location-east.yaml
```

### OCloudSite CRs

O-Cloud Sites represent logical groupings of infrastructure at a Location.

```yaml
apiVersion: ocloud.openshift.io/v1alpha1
kind: OCloudSite
metadata:
  # name is used for references from ResourcePool CRs
  # metadata.uid becomes oCloudSiteId in API responses
  name: site-east-1
  namespace: oran-o2ims
spec:
  # MUST MATCH an existing Location's metadata.name
  globalLocationName: "east-datacenter"
  description: "Primary compute site at east data center"

  extensions:
    environment: "production"
    managed-by: "team-infra"
```

#### OCloudSite Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `metadata.name` | Yes | Used for references from ResourcePool CRs. Must be unique. |
| `globalLocationName` | Yes | **Must match** an existing Location's `metadata.name` |
| `description` | No | Detailed description |
| `extensions` | No | Custom key-value metadata |

Apply the OCloudSite CR:

```bash
oc apply -f ocloudsite-east-1.yaml
```

### ResourcePool CRs

Resource Pools group related resources (servers) within an O-Cloud Site.

```yaml
apiVersion: ocloud.openshift.io/v1alpha1
kind: ResourcePool
metadata:
  # name is used for BMH label matching
  # metadata.uid becomes resourcePoolId in API responses
  name: dell-xr8620t-pool
  namespace: oran-o2ims
spec:
  # MUST MATCH an existing OCloudSite's metadata.name
  oCloudSiteName: "site-east-1"
  description: "Dell XR8620t compute resources"

  extensions:
    hardware-profile: "high-performance"
    purpose: "ran-du"
```

#### ResourcePool Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `metadata.name` | Yes | Used for BMH label matching. Must be unique. |
| `oCloudSiteName` | Yes | **Must match** an existing OCloudSite's `metadata.name` |
| `description` | No | Detailed description |
| `extensions` | No | Custom key-value metadata |

Apply the ResourcePool CR:

```bash
oc apply -f resourcepool-dell-xr8620t.yaml
```

## Server Inventory

The server inventory consists of per-server Metal3 resources that represent
individual physical hosts. Each server requires a BMC credentials Secret and a
BareMetalHost CR, with an optional network data Secret for inspection.

### Namespace Organization

Servers are organized into namespaces. The recommended convention is to use a
namespace per resource pool. Servers within a resource pool should be
homogeneous (same hardware type and configuration) so that any server in the
pool can satisfy a provisioning request targeting that pool.

Create a namespace for your servers:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: dell-xr8620t-pool
```

### BMC Credentials Secret

Each server requires a Secret containing the BMC username and password. The
Secret must be in the same namespace as the BareMetalHost:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bmc-secret-dell-xr8620t-node1
  namespace: dell-xr8620t-pool
type: Opaque
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
```

### Preprovisioning Network Data Secret

The preprovisioning network data Secret contains an nmstate network
configuration that is built into the Metal3 discovery ISO used during hardware
inspection. This Secret is **optional** — if not provided, all interfaces will
default to up and DHCP-enabled during discovery and inspection.

You will need a network data Secret if:

- You are using static IP addresses for the inspection network
- You need to configure VLANs for network connectivity during inspection
- You need to customize DNS resolver settings or other network parameters

If provided, the Secret is referenced by the BMH's
`spec.preprovisioningNetworkDataName` field. Multiple BMHs can share a common
network data Secret, as long as the same configuration applies to each BMH.
For example, BMHs on the same VLAN with DHCP could share a single Secret, but
BMHs with static IP addresses would each need their own Secret.

> [!WARNING]
> Do not name a Secret with just the BMH name (e.g., a Secret named
> `dell-xr8620t-node1` in the same namespace as a BMH named
> `dell-xr8620t-node1`). During IBI provisioning, a DataImage Secret is
> created using the BMH name, which would conflict with an existing Secret of
> the same name.

> [!WARNING]
> Interface names in the nmstate configuration must use the standard device
> name as assigned by the kernel (e.g., `ens3f0`, `eno1np0`), not an alternate
> name. The device names may differ from those assigned by other Linux
> distributions. Using alternate names can cause inspection failures,
> particularly with VLAN configurations.

DHCP example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: network-data-dell-xr8620t-node1
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
apiVersion: v1
kind: Secret
metadata:
  name: network-data-vlan310
  namespace: dell-xr8620t-pool
type: Opaque
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

### BareMetalHost CR

The BareMetalHost CR represents a physical server. The O-Cloud Manager uses
labels and annotations on the BMH to manage server identity, selection
criteria, and interface mappings.

#### Required Labels

The following label is required for the O-Cloud Manager to recognize a BMH as
a managed server:

| Label | Description |
|---|---|
| `resources.clcm.openshift.io/resourcePoolName` | **Must match** an existing ResourcePool's `metadata.name` |

> **Note**: The BMH is linked to the infrastructure hierarchy through the
> ResourcePool. The OCloudSite and Location are determined by navigating up
> the hierarchy from the ResourcePool.

#### Interface Labels

Interface labels use the `interfacelabel.clcm.openshift.io/` prefix to
associate user-defined labels with physical NIC names on the server:

```yaml
interfacelabel.clcm.openshift.io/boot-interface: ens3f0
interfacelabel.clcm.openshift.io/data-interface: ens3f1
interfacelabel.clcm.openshift.io/sriov-1: ens2f0
interfacelabel.clcm.openshift.io/sriov-2: ens2f1
```

With the exception of `boot-interface`, these labels are optional and
user-defined — the label keys are opaque strings as far as the O-Cloud
Manager is concerned. The labels are attached to the corresponding interfaces
in the inventory resource data for user reference, and can be used to match
interfaces in the ClusterInstance defaults.

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
`interfacelabel.clcm.openshift.io/data-interface` labels are used to resolve
the actual NIC names and MAC addresses on that specific server.

> [!NOTE]
> The `boot-interface` label is required if the BMH's `spec.bootMACAddress` is
> not set. The O-Cloud Manager uses this label to determine the boot NIC's MAC
> address from the hardware inventory. If `spec.bootMACAddress` is already set,
> the `boot-interface` label is optional.

#### Resource Selector Labels

Resource selector labels use the `resourceselector.clcm.openshift.io/` prefix
and provide custom criteria for matching servers to provisioning requests.
These labels are matched against the per-node-group `resourceSelector` field in
the ClusterTemplate `hwMgmtDefaults.nodeGroupData`:

```yaml
resourceselector.clcm.openshift.io/server-type: XR8620t
resourceselector.clcm.openshift.io/server-colour: blue
resourceselector.clcm.openshift.io/server-id: dell-xr8620t-node1
resourceselector.clcm.openshift.io/subnet: "192.0.2.0"
```

These labels are user-defined — you can create any key-value pairs that make
sense for your environment (e.g., rack location, hardware generation,
deployment zone).

#### Annotations

The following annotations provide additional metadata:

| Annotation | Description |
|---|---|
| `resourceinfo.clcm.openshift.io/description` | Human-readable description of the server |
| `resourceinfo.clcm.openshift.io/partNumber` | Part number or asset identifier |
| `resourceinfo.clcm.openshift.io/groups` | Comma-separated list of group memberships |

These annotations are optional and are exposed through the O-Cloud inventory
API.

#### BMH Spec Fields

Key fields in the BareMetalHost spec:

| Field | Description |
|---|---|
| `spec.online` | Set to `false` for initial onboarding. The O-Cloud Manager manages power state during provisioning. |
| `spec.bmc.address` | BMC/iDRAC address in Redfish format (e.g., `idrac-virtualmedia+https://<ip>/redfish/v1/Systems/System.Embedded.1`) |
| `spec.bmc.credentialsName` | Name of the BMC credentials Secret |
| `spec.bmc.disableCertificateVerification` | Set to `true` if the BMC uses a self-signed certificate |
| `spec.bootMACAddress` | MAC address of the boot NIC. Optional if `boot-interface` label is set. |
| `spec.preprovisioningNetworkDataName` | Optional. Name of the network data Secret containing nmstate configuration for inspection. If not set, inspection uses DHCP on all interfaces. |

### Hardware Inspection

After a BareMetalHost is created, the Metal3 Bare Metal Operator automatically
inspects the host via its BMC to discover hardware details. The inspection
results are stored in a `HardwareData` CR in the same namespace as the BMH.

The HardwareData CR contains:

- CPU architecture, model, and thread count
- Total RAM
- Storage devices (type, size, model, vendor)
- Network interfaces (model, MAC address, speed)
- System manufacturer and product name

This data is used by the O-Cloud Manager for
[hardware data selectors](./template-overview.md#hwmgmtdefaults) in the
ClusterTemplate `hwMgmtDefaults.nodeGroupData[].resourceSelector`. For example, a selector like
`hardwaredata/num_threads;>=: "64"` is evaluated against the HardwareData CR
for each candidate BMH.

You can view a server's hardware inventory:

```console
oc get hardwaredata.metal3.io <bmh-name> -n <namespace> -o yaml
```

## Validation

After creating all the CRs, verify the infrastructure hierarchy and server
inventory are correct.

### Check Infrastructure Hierarchy CRs

```bash
$ oc get locations,ocloudsites,resourcepools -n oran-o2ims \
  -o custom-columns='NAME:.metadata.name,KIND:.kind,READY:.status.conditions[?(@.type=="Ready")].status'
NAME                KIND           READY
east-datacenter     Location       True
site-east-1         OCloudSite     True
dell-xr8620t-pool   ResourcePool   True
```

### Check BareMetalHosts

Verify that your BareMetalHosts have the required label and are in a valid
provisioning state:

```bash
$ oc get baremetalhosts -A -o custom-columns=\
'NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATE:.status.provisioning.state,POOL:.metadata.labels.resources\.clcm\.openshift\.io/resourcePoolName'
NAMESPACE           NAME                   STATE       POOL
dell-xr8620t-pool   dell-xr8620t-node1     available   dell-xr8620t-pool
```

If the `POOL` column is empty (`<none>`), the BMH will not appear in the
Resources API. Add the label:

```bash
oc label baremetalhost dell-xr8620t-node1 -n dell-xr8620t-pool \
  resources.clcm.openshift.io/resourcePoolName=dell-xr8620t-pool
```

**Valid provisioning states for inventory inclusion:**

| State | Description |
|-------|-------------|
| `available` | BMH is registered and ready for provisioning |
| `provisioned` | BMH has been provisioned with an OS |
| `provisioning` | BMH is currently being provisioned |
| `deprovisioning` | BMH is being returned to available state |
| `externally provisioned` | BMH was provisioned outside Metal3 |

If a BMH has the correct label but does not appear in the Resources API, check
its provisioning state. BMHs in early lifecycle states (registering, inspecting)
will appear once they reach the `available` state.

### Verify via API

After the collector processes the CRs, verify via the Inventory API.

> **Note**: Data collection timing differs by resource type:
>
> All inventory data (Locations, OCloudSites, ResourcePools, and Resources)
> is collected via K8s watches and reflected in the API within seconds of
> the underlying CR change.

First, set up authentication (see
[Testing API endpoints](environment-setup.md#testing-api-endpoints-on-a-cluster)
for details):

```bash
# For development testing, use a Service Account token
oc apply -f config/testing/client-service-account-rbac.yaml
export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)

# Get the API URI from any route (all O2IMS routes share the same host)
export API_URI=$(oc get routes -n oran-o2ims -o jsonpath='{.items[0].spec.host}')
```

Then query the API:

```bash
# Get Resource Pools
curl -ks --header "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools" | jq

# Get Resources within a Resource Pool (BareMetalHosts)
# Replace {resourcePoolId} with the UUID from the resourcePools response
curl -ks --header "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}/resources" | jq
```

## Complete Example

The following example shows all the resources needed to set up the
infrastructure hierarchy and onboard a single server:

```yaml
---
# 1. Location
apiVersion: ocloud.openshift.io/v1alpha1
kind: Location
metadata:
  name: east-datacenter
  namespace: oran-o2ims
spec:
  description: "Primary east coast data center facility"
  address: "123 Technology Way, Ashburn, VA 20147, USA"
---
# 2. O-Cloud Site (references Location by name)
apiVersion: ocloud.openshift.io/v1alpha1
kind: OCloudSite
metadata:
  name: site-east-1
  namespace: oran-o2ims
spec:
  globalLocationName: "east-datacenter"
  description: "Primary compute site at east data center"
---
# 3. Resource Pool (references OCloudSite by name)
apiVersion: ocloud.openshift.io/v1alpha1
kind: ResourcePool
metadata:
  name: dell-xr8620t-pool
  namespace: oran-o2ims
spec:
  oCloudSiteName: "site-east-1"
  description: "Dell XR8620t compute resources"
---
# 4. Server namespace
apiVersion: v1
kind: Namespace
metadata:
  name: dell-xr8620t-pool
---
# 5. BMC credentials Secret
apiVersion: v1
kind: Secret
metadata:
  name: bmc-secret-dell-xr8620t-node1
  namespace: dell-xr8620t-pool
type: Opaque
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
---
# 6. Network data Secret (optional)
apiVersion: v1
kind: Secret
metadata:
  name: network-data-dell-xr8620t-node1
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
# 7. BareMetalHost (references ResourcePool via label)
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  labels:
    resources.clcm.openshift.io/resourcePoolName: dell-xr8620t-pool
    resourceselector.clcm.openshift.io/server-type: XR8620t
    resourceselector.clcm.openshift.io/server-id: dell-xr8620t-node1
    interfacelabel.clcm.openshift.io/boot-interface: ens3f0
    interfacelabel.clcm.openshift.io/data-interface: ens3f1
    interfacelabel.clcm.openshift.io/sriov-1: ens2f0
    interfacelabel.clcm.openshift.io/sriov-2: ens2f1
  annotations:
    resourceinfo.clcm.openshift.io/description: "XR8620t server"
    resourceinfo.clcm.openshift.io/partNumber: "00001"
    resourceinfo.clcm.openshift.io/groups: "groupA, groupB"
  name: dell-xr8620t-node1
  namespace: dell-xr8620t-pool
spec:
  online: false
  bmc:
    address: idrac-virtualmedia+https://198.51.100.10/redfish/v1/Systems/System.Embedded.1
    credentialsName: bmc-secret-dell-xr8620t-node1
    disableCertificateVerification: true
  bootMACAddress: 02:00:00:00:00:01
  preprovisioningNetworkDataName: network-data-dell-xr8620t-node1
```

Additional sample files are available under
[inventory](../samples/git-setup/clustertemplates/inventory/).

## Deploying via GitOps

In a production environment, server inventory is typically managed through
GitOps. The infrastructure hierarchy CRs, server namespace, secrets, and BMH
CRs are stored in a Git repository and synced to the hub cluster via ArgoCD.

See [GitOps Layout and Setup](./gitops-layout-and-setup.md) for the recommended
directory structure and ArgoCD configuration.
