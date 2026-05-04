<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Inventory API

The Inventory API provides information about the O-Cloud infrastructure
hierarchy (locations, sites, resource pools), the physical resources
(servers/BareMetalHosts) within those pools, resource types, and deployment
managers (managed clusters). It also supports subscriptions for change
notifications.

For authentication and common query parameters (filtering, field selection),
see [API Overview](./api-overview.md).

All inventory API endpoints use the base path
`/o2ims-infrastructureInventory/v1`.

- [O-Cloud Information](#o-cloud-information)
- [Locations](#locations)
- [O-Cloud Sites](#o-cloud-sites)
- [Resource Pools](#resource-pools)
- [Resources](#resources)
- [Resource Types](#resource-types)
- [Deployment Managers](#deployment-managers)
- [Subscriptions](#subscriptions)
- [Alarm Dictionaries](#alarm-dictionaries)

## O-Cloud Information

Returns metadata about the O-Cloud instance, including its unique identifier
and supported API versions.

### Get O-Cloud Info

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1" | jq
```

### Get API Versions

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/api_versions" | jq
```

## Locations

Locations represent geographic places where O-Cloud Sites can be deployed.
They are defined via `Location` CRs in the hub cluster (see
[Server Onboarding](./server-onboarding.md#location-crs)).

Location data is collected via Kubernetes watches and appears in the API
nearly immediately after the CR is created.

### List Locations

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/locations" | jq
```

Example response:

```json
[
  {
    "globalLocationId": "east-datacenter",
    "name": "east-datacenter",
    "description": "Primary east coast data center facility",
    "coordinate": {
      "type": "Point",
      "coordinates": [-77.0364, 38.8951]
    },
    "address": "123 Technology Way, Ashburn, VA 20147, USA",
    "oCloudSiteIds": ["fddfbbae-0fb3-402e-8408-6adbb6ba382a"]
  }
]
```

### Get a Specific Location

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/locations/{globalLocationId}" | jq
```

> **Note**: The `globalLocationId` is the Location CR's `metadata.name`
> (a string), not a UUID.

## O-Cloud Sites

O-Cloud Sites represent logical groupings of infrastructure at a Location.
They are defined via `OCloudSite` CRs (see
[Server Onboarding](./server-onboarding.md#ocloudsite-crs)).

### List O-Cloud Sites

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/oCloudSites" | jq
```

Example response:

```json
[
  {
    "oCloudSiteId": "fddfbbae-0fb3-402e-8408-6adbb6ba382a",
    "globalLocationId": "east-datacenter",
    "name": "site-east-1",
    "description": "Primary compute site at east data center",
    "resourcePools": ["7208c495-18c9-45ac-be47-e4b900c6f204"]
  }
]
```

### Get a Specific O-Cloud Site

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/oCloudSites/{oCloudSiteId}" | jq
```

## Resource Pools

Resource pools group related resources (servers) within an O-Cloud Site.
They are defined via `ResourcePool` CRs (see
[Server Onboarding](./server-onboarding.md#resourcepool-crs)).

### List Resource Pools

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools" | jq
```

Example response:

```json
[
  {
    "resourcePoolId": "7208c495-18c9-45ac-be47-e4b900c6f204",
    "oCloudSiteId": "fddfbbae-0fb3-402e-8408-6adbb6ba382a",
    "name": "dell-xr8620t-pool",
    "description": "Dell XR8620t compute resources",
    "extensions": {}
  }
]
```

### Get a Specific Resource Pool

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}" | jq
```

## Resources

Resources represent individual physical servers (BareMetalHosts) within a
resource pool. Resource data is collected by polling the hardware manager on
a configurable interval (default: 1 minute).

Resources are accessed as sub-resources of a resource pool.

> [!NOTE]
> A BMH will only appear in the Resources API if it has a
> `resources.clcm.openshift.io/resourcePoolName` label and has completed
> hardware inspection (i.e., it has reached the `available` provisioning
> state or later). Resource data is polled every minute, so newly labeled
> or newly inspected BMHs may take up to 1 minute to appear.

### List Resources in a Resource Pool

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}/resources" | jq
```

Example response:

```json
[
  {
    "resourceId": "f3e79178-b199-4eb6-bfe3-b1417b7d5b8d",
    "resourcePoolId": "7208c495-18c9-45ac-be47-e4b900c6f204",
    "description": "XR8620t server",
    "resourceTypeId": "d1eabf91-f0e6-5170-97dc-797d35146dad",
    "globalAssetId": "296S2Z3",
    "tags": ["server-type: XR8620t", "server-id: dell-xr8620t-node1"],
    "extensions": {
      "adminState": "LOCKED",
      "operationalState": "DISABLED",
      "usageState": "IDLE",
      "powerState": "OFF",
      "vendor": "Dell Inc.",
      "model": "PowerEdge XR8620t",
      "labels": {
        "resources.clcm.openshift.io/resourcePoolName": "dell-xr8620t-pool"
      }
    }
  }
]
```

### Get a Specific Resource

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}/resources/{resourceId}" | jq
```

## Resource Types

Resource types categorize resources by their hardware characteristics
(manufacturer, model). Resources that share the same hardware profile are
grouped under the same resource type.

### List Resource Types

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes" | jq
```

### Get a Specific Resource Type

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes/{resourceTypeId}" | jq
```

### Get the Alarm Dictionary for a Resource Type

Each resource type has an associated alarm dictionary:

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes/{resourceTypeId}/alarmDictionary" | jq
```

## Deployment Managers

Deployment managers represent the managed clusters that can deploy workloads
to the infrastructure. Each ACM ManagedCluster corresponds to a deployment
manager.

### List Deployment Managers

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers" | jq
```

### Get a Specific Deployment Manager

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers/{deploymentManagerId}" | jq
```

### Filter Deployment Managers

```bash
# Return only names
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?fields=name" | jq

# Exclude the local hub cluster
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?filter=(neq,name,local-cluster)" | jq
```

## Subscriptions

Subscriptions allow an SMO to receive notifications when inventory resources
change. When a subscription is created, the O-Cloud Manager sends HTTP
callbacks to the specified URL whenever matching resources are created,
modified, or deleted.

### List Subscriptions

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions" | jq
```

### Get a Specific Subscription

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/{subscriptionId}" | jq
```

### Create a Subscription

```bash
curl -ks -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  -d '{
    "consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
    "filter": "",
    "callback": "https://smo.example.com/v1/o2ims_inventory_observer"
  }' \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions" | jq
```

### Delete a Subscription

```bash
curl -ks -X DELETE \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/{subscriptionId}" | jq
```

## Alarm Dictionaries

Alarm dictionaries define the set of alarms that can be raised for
infrastructure resources.

### List Alarm Dictionaries

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/alarmDictionaries" | jq
```

### Get a Specific Alarm Dictionary

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v1/alarmDictionaries/{alarmDictionaryId}" | jq
```
