<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# API Overview

The O-Cloud Manager exposes several REST APIs for managing infrastructure
inventory, clusters, provisioning, alarms, and artifacts. All APIs share
common authentication, query parameters, and error handling patterns
described in this document.

- [API Routes](#api-routes)
- [Authentication](#authentication)
- [Interactive API Documentation](#interactive-api-documentation)
- [Common Query Parameters](#common-query-parameters)
  - [Field Selection](#field-selection)
  - [Field Exclusion](#field-exclusion)
  - [Filtering](#filtering)
- [Error Responses](#error-responses)

## API Routes

All O-Cloud Manager APIs are exposed through a single ingress hostname with
different path prefixes routing to different backend services:

| Path Prefix | Service | Description |
|-------------|---------|-------------|
| `/o2ims-infrastructureInventory` | resource-server | Locations, sites, pools, resources, resource types |
| `/o2ims-infrastructureCluster` | cluster-server | Node clusters, cluster resources, cluster resource types |
| `/o2ims-infrastructureProvisioning` | provisioning-server | Provisioning requests |
| `/o2ims-infrastructureMonitoring` | alarms-server | Alarms and alarm definitions |
| `/o2ims-infrastructureArtifacts` | artifacts-server | Artifact management |

Get the API hostname from the ingress routes:

```bash
export API_URI=$(oc get routes -n oran-o2ims -o jsonpath='{.items[0].spec.host}')
```

## Authentication

API requests require a bearer token in the `Authorization` header. For
development and testing, you can use an OpenShift service account token.
For production use with an SMO, OAuth2 authentication is used.

For setup instructions, see
[Testing API endpoints on a cluster](./environment-setup.md#testing-api-endpoints-on-a-cluster).

All examples in the API documentation use the following variables:

```bash
# API hostname
export API_URI=$(oc get routes -n oran-o2ims -o jsonpath='{.items[0].spec.host}')

# Authentication token
export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)
```

## Interactive API Documentation

You can explore the APIs interactively using Swagger UI:

```bash
# Start the Swagger UI container
make swagger-ui-start

# Open http://localhost:9090 in your browser

# Stop when done
make swagger-ui-stop
```

## Common Query Parameters

All list endpoints support the following query parameters for filtering and
field selection.

### Field Selection

Use the `fields` parameter to return only specific fields:

```bash
# Return only the name field
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/resourcePools?fields=name" | jq
```

Use dot-separated paths for nested fields:

```bash
# Return name and a specific extension field
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/resourcePools?fields=name,extensions/purpose" | jq
```

### Field Exclusion

Use the `exclude_fields` parameter to exclude specific fields from the
response. Fields in this list are excluded even if they are explicitly
included via the `fields` parameter:

```bash
# Return everything except extensions
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/resourcePools?exclude_fields=extensions" | jq
```

### Filtering

Use the `filter` parameter to search for specific records. Filters use the
syntax `(operator,field,value)`, with multiple filters separated by
semicolons (AND logic):

| Operator | Meaning |
|----------|---------|
| `eq` | Equal to |
| `neq` | Not equal to |
| `gt` | Greater than |
| `gte` | Greater than or equal to |
| `lt` | Less than |
| `lte` | Less than or equal to |
| `cont` | Contains |
| `ncont` | Does not contain |
| `in` | One of the values |
| `nin` | Not one of the values |

Examples:

```bash
# Filter by exact name
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/resourcePools?filter=(eq,name,pool-east-compute)" | jq

# Exclude a specific name
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/deploymentManagers?filter=(neq,name,local-cluster)" | jq

# Filter by extension field
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/resourcePools?filter=(eq,extensions/purpose,ran-du)" | jq

# Multiple filters (AND)
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureInventory/v2/locations?filter=(eq,extensions/region,us-east);(eq,extensions/tier,primary)" | jq

# Values with spaces must be quoted
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "https://${API_URI}/o2ims-infrastructureCluster/v1/nodeClusters?filter=(eq,name,'my cluster')" | jq
```

## Error Responses

Errors are returned in the
[RFC 7807](https://tools.ietf.org/html/rfc7807) Problem Details format:

```json
{
  "status": 404,
  "detail": "Resource not found"
}
```
