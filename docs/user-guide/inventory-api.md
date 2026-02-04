# Inventory API

- [Request Examples](#request-examples)
  - [Query the Metadata endpoints](#query-the-metadata-endpoints)
    - [GET api\_versions](#get-api_versions)
    - [GET O-Cloud infrastructure information](#get-o-cloud-infrastructure-information)
  - [Query the Deployment manager server](#query-the-deployment-manager-server)
    - [GET deploymentManagers List](#get-deploymentmanagers-list)
    - [GET field or fields from the deploymentManagers List](#get-field-or-fields-from-the-deploymentmanagers-list)
    - [GET deploymentManagers List using filter](#get-deploymentmanagers-list-using-filter)
  - [Query the Resource server](#query-the-resource-server)
    - [GET Resource Type List](#get-resource-type-list)
    - [GET Specific Resource Type](#get-specific-resource-type)
    - [GET Resource Pool List](#get-resource-pool-list)
    - [GET Specific Resource Pool](#get-specific-resource-pool)
    - [GET all Resources of a specific Resource Pool](#get-all-resources-of-a-specific-resource-pool)
  - [Query the Infrastructure Inventory Subscription (Resource Server)](#query-the-infrastructure-inventory-subscription-resource-server)
    - [GET Infrastructure Inventory Subscription Information](#get-infrastructure-inventory-subscription-information)
    - [POST a new Infrastructure Inventory Subscription Information](#post-a-new-infrastructure-inventory-subscription-information)
    - [DELETE an Infrastructure Inventory Subscription](#delete-an-infrastructure-inventory-subscription)

## Request Examples

> **Interactive API Documentation**
>
> You can explore the API interactively using Swagger UI. From the project root, run:
>
> ```bash
> make swagger-ui-start
> ```
>
> Then open <http://localhost:9090> in your browser. This provides an interactive interface
> to browse endpoints, view schemas, and try out requests. To stop the Swagger UI container:
>
> ```bash
> make swagger-ui-stop
> ```
>

### Query the Metadata endpoints

> :warning: Confirm that an authorization token has already been acquired. See
> section [Testing API endpoints on a cluster](./environment-setup.md#testing-api-endpoints-on-a-cluster)

Notice the `API_URI` is the route HOST/PORT column of the oran-o2ims operator.

```console
$ oc get routes -n oran-o2ims
NAME                       HOST/PORT                                    PATH                                SERVICES              PORT   TERMINATION          WILDCARD
oran-o2ims-ingress-ghcwc   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureInventory      resource-server       api    reencrypt/Redirect   None
oran-o2ims-ingress-pz8hc   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureMonitoring     alarms-server         api    reencrypt/Redirect   None
oran-o2ims-ingress-qrnfq   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureProvisioning   provisioning-server   api    reencrypt/Redirect   None
oran-o2ims-ingress-t842p   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureArtifacts      artifacts-server      api    reencrypt/Redirect   None
oran-o2ims-ingress-tbzbl   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureCluster        cluster-server        api    reencrypt/Redirect   None
```

Export the o2ims endpoint as the API_URI variable so it can be re-used in the requests.

```console
export API_URI=o2ims.apps.${DOMAIN}
```

#### GET api_versions

To get the api versions supported

```console
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/api_versions" | jq
```

#### GET O-Cloud infrastructure information

To obtain information from the O-Cloud:

```console
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1"
```

### Query the Deployment manager server

The deployment manager server (DMS) needs to connect to kubernetes API of the RHACM hub to obtain the required information. Here we can see a couple of queries to the DMS.

#### GET deploymentManagers List

To get a list of all the deploymentManagers (clusters) available in our O-Cloud:

```console
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers" | jq
```

#### GET field or fields from the deploymentManagers List

To get a list of only the `name` of the available deploymentManagers available in our O-Cloud:

```console
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?fields=name" | jq
```

#### GET deploymentManagers List using filter

To get a list of all the deploymentManagers whose name is **not** local-cluster in our O-Cloud:

```console
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?filter=(neq,name,local-cluster)" | jq
 | jq
```

### Query the Resource server

The resource server exposes endpoints for retrieving resource types, resource pools and resources objects. The server relies on the Search Query API of ACM hub. Follow these [instructions](docs/dev/env_acm.md#search-query-api) to enable
and configure the search API access. The resource server will translate those REST requests and send them to the ACM search server that implements a graphQL API.

> :exclamation: To obtain the requested information we need to enable the searchCollector of all the managed clusters, concretely, in the KlusterletAddonConfig CR.

#### GET Resource Type List

To get a list of available resource types:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes" | jq
```

#### GET Specific Resource Type

To get information of a specific resource type:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes/${resource_type_name} | jq
```

#### GET Resource Pool List

To get a list of available resource pools:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools" | jq
```

#### GET Specific Resource Pool

To get information of a specific resource pool:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}" | jq
```

#### GET all Resources of a specific Resource Pool

We can filter down to get all the resources of a specific resourcePool.

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}
/resources" | jq
```

### Query the Infrastructure Inventory Subscription (Resource Server)

#### GET Infrastructure Inventory Subscription List

To get a list of resource subscriptions:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions | jq
```

#### GET Infrastructure Inventory Subscription Information

To get all the information about an existing resource subscription:

```console
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```

#### POST a new Infrastructure Inventory Subscription Information

To add a new resource subscription:

```console
$ curl -ks -X POST \
--header "Content-Type: application/json" \
--header "Authorization: Bearer ${MY_TOKEN}" \
-d @infra-sub.json https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions | jq
```

Where the content of `infra-sub.json` is as follows:

```json
{
  "consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
  "filter": "(eq,extensions/country,US);",
  "callback": "https://128.224.115.15:1081/smo/v1/o2ims_inventory_observer"
}
```

#### DELETE an Infrastructure Inventory Subscription

To delete an existing resource subscription:

```console
$ curl -ks -X DELETE \
--header "Authorization: Bearer ${MY_TOKEN}" \
https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```
