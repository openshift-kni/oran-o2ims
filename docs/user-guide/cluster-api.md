<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Cluster API

The Cluster API provides information about managed clusters (node clusters),
their resource composition (cluster resources), and alarm definitions. Cluster
data is collected by watching ACM ManagedCluster resources and assisted-service
Agent resources on the hub cluster.

For authentication and common query parameters (filtering, field selection),
see [API Overview](./api-overview.md).

All cluster API endpoints use the base path
`/o2ims-infrastructureCluster/v1`.

- [Node Clusters](#node-clusters)
- [Node Cluster Types](#node-cluster-types)
- [Cluster Resources](#cluster-resources)
- [Cluster Resource Types](#cluster-resource-types)
- [Subscriptions](#subscriptions)
- [Alarm Dictionaries](#alarm-dictionaries)

## Node Clusters

A node cluster represents a managed OpenShift cluster registered with ACM.
Each ManagedCluster on the hub corresponds to a node cluster in the API.

### List Node Clusters

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusters" | jq
```

Example response:

```json
[
  {
    "nodeClusterId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "nodeClusterTypeId": "d1eabf91-f0e6-5170-97dc-797d35146dad",
    "name": "my-sno-cluster",
    "description": "SNO RAN DU cluster",
    "extensions": {
      "clusterID": "c5e3f8a2-1b4d-4e6f-8a9b-0c1d2e3f4a5b",
      "platform": "BareMetal",
      "openshiftVersion": "4.20.4"
    },
    "clusterResourceIds": []
  }
]
```

### Get a Specific Node Cluster

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusters/{nodeClusterId}" | jq
```

### Filter Node Clusters

```bash
# Find clusters by name
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusters?filter=(eq,name,my-sno-cluster)" | jq

# Exclude the local hub cluster
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusters?filter=(neq,name,local-cluster)" | jq
```

## Node Cluster Types

Node cluster types categorize managed clusters. Each unique cluster
configuration (platform, version) corresponds to a node cluster type.

### List Node Cluster Types

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusterTypes" | jq
```

### Get a Specific Node Cluster Type

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusterTypes/{nodeClusterTypeId}" | jq
```

### Get the Alarm Dictionary for a Node Cluster Type

Each node cluster type has an associated alarm dictionary that defines the
alerts that can be raised for clusters of that type:

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusterTypes/{nodeClusterTypeId}/alarmDictionary" | jq
```

## Cluster Resources

Cluster resources represent individual nodes within a managed cluster. The
data is collected from assisted-service Agent CRs on the hub.

> [!NOTE]
> Cluster resources are only populated for clusters provisioned via the
> assisted-installer flow. IBI-provisioned clusters do not create Agent CRs,
> so their cluster resources will be empty. See
> [OCPBUGS-83739](https://redhat.atlassian.net/browse/OCPBUGS-83739).

### List Cluster Resources

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/clusterResources" | jq
```

### Get a Specific Cluster Resource

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/clusterResources/{clusterResourceId}" | jq
```

## Cluster Resource Types

Cluster resource types categorize the nodes within managed clusters.

### List Cluster Resource Types

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/clusterResourceTypes" | jq
```

### Get a Specific Cluster Resource Type

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/clusterResourceTypes/{clusterResourceTypeId}" | jq
```

## Subscriptions

Subscriptions allow an SMO to receive notifications when cluster resources
change. When a subscription is created, the O-Cloud Manager sends HTTP
callbacks to the specified URL whenever matching resources are created,
modified, or deleted.

### List Subscriptions

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/subscriptions" | jq
```

### Get a Specific Subscription

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/subscriptions/{subscriptionId}" | jq
```

### Create a Subscription

```bash
curl -ks -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  -d '{
    "consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
    "filter": "",
    "callback": "https://smo.example.com/v1/o2ims_cluster_observer"
  }' \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/subscriptions" | jq
```

### Delete a Subscription

```bash
curl -ks -X DELETE \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/subscriptions/{subscriptionId}" | jq
```

## Alarm Dictionaries

Alarm dictionaries define the set of alarms that can be raised for managed
clusters. They are derived from Prometheus alerting rules on the spoke
clusters.

### List Alarm Dictionaries

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/alarmDictionaries" | jq
```

### Get a Specific Alarm Dictionary

```bash
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/alarmDictionaries/{alarmDictionaryId}" | jq
```
