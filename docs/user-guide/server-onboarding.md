# Server Onboarding

This guide describes how to define the geographic and organizational hierarchy for your O-Cloud infrastructure using Custom Resources (CRs), and how to associate BareMetalHost (BMH) resources with this hierarchy.

- [Overview](#overview)
- [Data Model Relationships](#data-model-relationships)
- [Step 1: Create Location CRs](#step-1-create-location-crs)
- [Step 2: Create OCloudSite CRs](#step-2-create-ocloudsite-crs)
- [Step 3: Create ResourcePool CRs](#step-3-create-resourcepool-crs)
- [Step 4: Label BareMetalHost Resources](#step-4-label-baremetalhost-resources)
- [Validation](#validation)
- [Example: Complete Hierarchy](#example-complete-hierarchy)

## Overview

The O-Cloud infrastructure hierarchy consists of four levels:

```text
Location (geographic place)
    └── OCloudSite (logical site at a location)
            └── ResourcePool (group of resources)
                    └── Resource (BareMetalHost/server)
```

Each level is defined by a Custom Resource (CR) in the hub cluster, except for Resources which are discovered from BareMetalHost CRs via labels.

## Data Model Relationships

The following diagram shows how the CRs relate to each other through their fields:

![Data Model Relationships](../images/data-model-relationships.svg)

### Key Relationships

| Source CR | Field | References | Target CR Field |
|-----------|-------|------------|-----------------|
| OCloudSite | `spec.globalLocationName` | → | Location `metadata.name` |
| ResourcePool | `spec.oCloudSiteName` | → | OCloudSite `metadata.name` |
| BareMetalHost | label `resourcePoolName` | → | ResourcePool `metadata.name` |

### Identifiers in API Responses

| CR Type | CR Identifier | API Response Identifier |
|---------|---------------|------------------------|
| Location | `metadata.name` | `globalLocationId` = `metadata.name` (string) |
| OCloudSite | `metadata.name` | `oCloudSiteId` = `metadata.uid` (UUID) |
| ResourcePool | `metadata.name` | `resourcePoolId` = `metadata.uid` (UUID) |

## Step 1: Create Location CRs

Locations represent physical or logical places where O-Cloud Sites can be deployed.

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

### Location Field Reference

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

## Step 2: Create OCloudSite CRs

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

### OCloudSite Field Reference

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

## Step 3: Create ResourcePool CRs

Resource Pools group related resources (servers) within an O-Cloud Site.

```yaml
apiVersion: ocloud.openshift.io/v1alpha1
kind: ResourcePool
metadata:
  # name is used for BMH label matching
  # metadata.uid becomes resourcePoolId in API responses
  name: pool-east-compute
  namespace: oran-o2ims
spec:
  # MUST MATCH an existing OCloudSite's metadata.name
  oCloudSiteName: "site-east-1"
  description: "Compute resources for production workloads"

  extensions:
    hardware-profile: "high-performance"
    purpose: "ran-du"
```

### ResourcePool Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `metadata.name` | Yes | Used for BMH label matching. Must be unique. |
| `oCloudSiteName` | Yes | **Must match** an existing OCloudSite's `metadata.name` |
| `description` | No | Detailed description |
| `extensions` | No | Custom key-value metadata |

Apply the ResourcePool CR:

```bash
oc apply -f resourcepool-east-compute.yaml
```

## Step 4: Label BareMetalHost Resources

BareMetalHost (BMH) resources are associated with the hierarchy via labels:

```yaml
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: server-001
  namespace: dell-r740-pool
  labels:
    # MUST MATCH an existing ResourcePool's metadata.name
    resources.clcm.openshift.io/resourcePoolName: "pool-east-compute"

    # Additional selection labels
    resourceselector.clcm.openshift.io/server-type: "R740"
    resourceselector.clcm.openshift.io/server-id: "server-001"
  annotations:
    resourceinfo.clcm.openshift.io/description: "Dell R740 server for RAN DU"
spec:
  online: false
  bmc:
    address: idrac-virtualmedia+https://192.168.1.10/redfish/v1/Systems/System.Embedded.1
    credentialsName: bmc-secret-server-001
    disableCertificateVerification: true
  bootMACAddress: "02:00:00:00:00:01"
```

### Required Labels

| Label | Required | Description |
|-------|----------|-------------|
| `resources.clcm.openshift.io/resourcePoolName` | Yes | **Must match** an existing ResourcePool's `metadata.name` |

> **Note**: The BMH is linked to the hierarchy through the ResourcePool. The OCloudSite and Location are determined by navigating up the hierarchy from the ResourcePool.

## Validation

After creating the CRs, verify the hierarchy is correct:

### Check Locations

```bash
$ oc get locations -n oran-o2ims
NAME              READY   AGE
east-datacenter   True    5m
```

### Check O-Cloud Sites

```bash
$ oc get ocloudsites -n oran-o2ims
NAME          LOCATION          READY   AGE
site-east-1   east-datacenter   True    4m
```

### Check Resource Pools

```bash
$ oc get resourcepools -n oran-o2ims
NAME                SITE          READY   AGE
pool-east-compute   site-east-1   True    3m
```

### Verify via API

After the collector processes the CRs (watch-based, so nearly immediate), verify via the Inventory API.

First, set up authentication (see [Testing API endpoints](environment-setup.md#testing-api-endpoints-on-a-cluster) for details):

```bash
# For development testing, use a Service Account token
oc apply -f config/testing/client-service-account-rbac.yaml
export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)

# Get the API URI from any route (all O2IMS routes share the same host)
export API_URI=$(oc get routes -n oran-o2ims -o jsonpath='{.items[0].spec.host}')
```

> **Note**: The O2IMS operator creates multiple routes that share the same hostname but use
> different path prefixes to route to different backend services:
>
> | Path Prefix | Backend Service | Purpose |
> |-------------|-----------------|---------|
> | `/o2ims-infrastructureInventory` | resource-server | Locations, Sites, Pools, Resources |
> | `/o2ims-infrastructureCluster` | cluster-server | Cluster templates, deployments |
> | `/o2ims-infrastructureProvisioning` | provisioning-server | Provisioning requests |
> | `/o2ims-infrastructureMonitoring` | alarms-server | Alarms and alarm definitions |
> | `/o2ims-infrastructureArtifacts` | artifacts-server | Artifact management |

Then query the API:

```bash
# Get Locations
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/locations" | jq
[
  {
    "address": "123 Technology Way, Ashburn, VA",
    "description": "Primary east coast facility",
    "extensions": {},
    "globalLocationId": "east-datacenter",
    "name": "east-datacenter",
    "oCloudSiteIds": [
      "fddfbbae-0fb3-402e-8408-6adbb6ba382a"
    ]
  }
]

# Get O-Cloud Sites
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/oCloudSites" | jq
[
  {
    "description": "Primary compute site",
    "extensions": {},
    "globalLocationId": "east-datacenter",
    "name": "site-east-1",
    "oCloudSiteId": "fddfbbae-0fb3-402e-8408-6adbb6ba382a",
    "resourcePools": [
      "7067f73f-2e62-47ca-8a31-6264f97f8cdc"
    ]
  }
]

# Get Resource Pools
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools" | jq
[
  {
    "description": "Compute resources",
    "extensions": {},
    "name": "pool-east-compute",
    "oCloudSiteId": "fddfbbae-0fb3-402e-8408-6adbb6ba382a",
    "resourcePoolId": "7067f73f-2e62-47ca-8a31-6264f97f8cdc"
  }
]
```

> **Note**: The `oCloudSiteId` and `resourcePoolId` values are UUIDs derived from the CR's
> `metadata.uid`, which is assigned by Kubernetes when the CR is created. The `globalLocationId`
> is the Location's `metadata.name`.

### Correlating API Responses with CRs

The API response includes a `name` field that corresponds to the CR's `metadata.name`,
allowing direct lookup:

```bash
# For Location: use globalLocationId (equals metadata.name)
$ oc get location east-datacenter -n oran-o2ims -o yaml

# For OCloudSite: use name field from API response (equals metadata.name)
$ oc get ocloudsite site-east-1 -n oran-o2ims -o yaml

# For ResourcePool: use name field from API response (equals metadata.name)
$ oc get resourcepool pool-east-compute -n oran-o2ims -o yaml
```

### Finding Parent CRs from API Responses

The ResourcePool API response includes `oCloudSiteId` (a UUID) referencing its parent.
Since `oc get` doesn't support filtering by `metadata.uid` directly, use jsonpath:

```bash
# Given a ResourcePool response with oCloudSiteId: "fddfbbae-0fb3-402e-8408-6adbb6ba382a"
# Find the parent OCloudSite by its metadata.uid:
$ oc get ocloudsites -n oran-o2ims \
  -o jsonpath='{.items[?(@.metadata.uid=="fddfbbae-0fb3-402e-8408-6adbb6ba382a")].metadata.name}'
site-east-1

# Then inspect the parent OCloudSite:
$ oc get ocloudsite site-east-1 -n oran-o2ims -o yaml
```

For OCloudSite responses, the `globalLocationId` field is already the Location's
`metadata.name`, so direct lookup works:

```bash
# Given an OCloudSite response with globalLocationId: "east-datacenter"
$ oc get location east-datacenter -n oran-o2ims -o yaml
```

## Example: Complete Hierarchy

Here's a complete example showing all CRs for a typical deployment:

```yaml
---
# 1. Location
apiVersion: ocloud.openshift.io/v1alpha1
kind: Location
metadata:
  name: east-datacenter      # ◄── globalLocationId in API responses
  namespace: oran-o2ims
spec:
  description: "Primary east coast facility"
  address: "123 Technology Way, Ashburn, VA"
---
# 2. O-Cloud Site (references Location by name)
apiVersion: ocloud.openshift.io/v1alpha1
kind: OCloudSite
metadata:
  name: site-east-1          # ◄── Referenced by ResourcePool
  namespace: oran-o2ims
spec:
  globalLocationName: "east-datacenter"  # ◄── Must match Location metadata.name
  description: "Primary compute site"
---
# 3. Resource Pool (references OCloudSite by name)
apiVersion: ocloud.openshift.io/v1alpha1
kind: ResourcePool
metadata:
  name: pool-east-compute    # ◄── Referenced by BMH label
  namespace: oran-o2ims
spec:
  oCloudSiteName: "site-east-1"          # ◄── Must match OCloudSite metadata.name
  description: "Compute resources"
---
# 4. BareMetalHost (references ResourcePool via label)
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: server-001
  namespace: dell-r740-pool
  labels:
    resources.clcm.openshift.io/resourcePoolName: "pool-east-compute"  # ◄── ResourcePool
spec:
  online: false
  bmc:
    address: idrac-virtualmedia+https://192.168.1.10/redfish/v1/Systems/System.Embedded.1
    credentialsName: bmc-secret
    disableCertificateVerification: true
  bootMACAddress: "02:00:00:00:00:01"
```

Apply all resources:

```bash
oc apply -f complete-hierarchy.yaml
```

> **Note**: When applying all CRs simultaneously, there may be a brief moment where
> OCloudSite and ResourcePool show `Ready=False`. This is normal and will self-correct
> as the controllers reconcile and discover the referenced resources.

Verify all resources are Ready:

```bash
$ oc get locations,ocloudsites,resourcepools -n oran-o2ims \
  -o custom-columns='NAME:.metadata.name,KIND:.kind,READY:.status.conditions[0].status'
NAME                KIND           READY
east-datacenter     Location       True
site-east-1         OCloudSite     True
pool-east-compute   ResourcePool   True
```
