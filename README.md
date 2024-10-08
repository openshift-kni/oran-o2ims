# O-RAN O2IMS

This project is an implementation of the O-RAN O2 IMS API on top of
OpenShift and ACM.

Note that at this point this is just experimental and at its very beginnings,
so don't try to use it for anything close to a production environment.

***Note: this README is only for development purposes.***

## Quick Start

### Build binary
``` bash
make binary
```

### Run

#### Metadata server

The metadata server returns information about the supported versions of the
API. It doesn't require any backend, only the O-Cloud identifier. You can start
it with a command like this:

```
$ ./oran-o2ims start metadata-server \
--log-level=debug \
--log-file=stdout \
--api-listener-address=localhost:8000 \
--metrics-listener-address="127.0.0.1:8008" \
--cloud-id=123
```

You can send requests with commands like these:

```
$ curl -s http://localhost:8000/o2ims-infrastructureInventory/api_versions | jq

$ curl -s http://localhost:8000/o2ims-infrastructureInventory/v1 | jq
```

Inside _VS Code_ use the _Run and Debug_ option with the `start
metadata-server` [configuration](.vscode/launch.json).

#### Deployment manager server

The deployment manager server needs to connect to the non-kubernetes API of the
ACM global hub. If you are already connected to an OpenShift cluster that has
that global hub installed and configured you can obtain the required URL and
token like this:

```
$ export BACKEND_URL=$(
  oc get route -n multicluster-global-hub multicluster-global-hub-manager -o json |
  jq -r '"https://" + .spec.host'
)
$ export BACKEND_TOKEN=$(
  oc create token -n multicluster-global-hub multicluster-global-hub-manager --duration=24h
)
$ export INSECURE_SKIP_VERIFY=true
```

Start the deployment manager server with a command like this:

```
$ ./oran-o2ims start deployment-manager-server \
--log-level=debug \
--log-file=stdout \
--api-listener-address=localhost:8001 \
--metrics-listener-address="127.0.0.1:8008" \
--cloud-id=123 \
--backend-url="${BACKEND_URL}" \
--backend-token="${BACKEND_TOKEN}"
```

Note that by default all the servers listen on `localhost:8000`, so there will
be conflicts if you try to run multiple servers in the same machine. The
`--api-listener-address` and `--metrics-listener-address` options are used to select a port number that isn't in
use.

The `cloud-id` is any string that you want to use as identifier of the O-Cloud instance.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start deployment-manager-server --help
```

You can send requests with commands like this:

```
$ curl -s http://localhost:8001/o2ims-infrastructureInventory/v1/deploymentManagers | jq
```

Inside _VS Code_ use the _Run and Debug_ option with the `start
deployment-manager-server` [configuration](.vscode/launch.json).

#### Resource server

The resource server exposes endpoints for retrieving resource types, resource pools
and resources objects. The server relies on the Search Query API of ACM hub.
Follow the these [instructions](docs/dev/env_acm.md#search-query-api) to enable
and configure the search API access.

The required URL and token can be obtained
as follows:

```
$ export BACKEND_URL=$(
  oc get route -n open-cluster-management search-api -o json |
  jq -r '"https://" + .spec.host'
)
$ export BACKEND_TOKEN=$(
  oc create token -n openshift-oauth-apiserver oauth-apiserver-sa --duration=24h
)
$ export INSECURE_SKIP_VERIFY=true
```

Start the resource server with a command like this:

```
$ ./oran-o2ims start resource-server \
--log-level=debug \
--log-file=stdout \
--api-listener-address=localhost:8002 \
--metrics-listener-address="127.0.0.1:8008" \
--cloud-id=123 \
--backend-url="${BACKEND_URL}" \
--backend-token="${BACKEND_TOKEN}"
```

Notes:
- `--backend-token-file="${BACKEND_TOKEN_FILE}"` can also be used instead of `--backend-token`.
- see more details regarding `api-listener-address` and `cloud-id` in the previous [section](#deployment-manager-server).

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start resource-server --help
```

##### Run and Debug

Inside _VS Code_ use the _Run and Debug_ option with the `start
resource-server` [configuration](.vscode/launch.json).

##### Requests Examples

###### GET Resource Type List

To get a list of resource types:
```
$ curl -s http://localhost:8002/o2ims-infrastructureInventory/v1/resourceTypes | jq
```

###### GET Resource Pool List

To get a list of resource pools:
```
$ curl -s http://localhost:8002/o2ims-infrastructureInventory/v1/resourcePools | jq
```

###### GET Resource List

To get a list of resources in a resource pool:
```
$ curl -s http://localhost:8002/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}
/resources | jq
```

#### Infrastructure Inventory Subscription Server (Resource Server)

The infrastructure inventory subscription server exposes endpoints for creating, retrieving
and deleting resource subscriptions.

***Notes:***
- No URL or token are required
- A connection to an ACM hub cluster is required

Start the infrastructure inventory subscription server with a command like this:

```
$ ./oran-o2ims start infrastructure-inventory-subscription-server \
--log-level=debug \
--log-file=stdout \
--log-field="server=resource-subscriptions" \
--api-listener-address=localhost:8004 \
--metrics-listener-address=localhost:8008 \
--namespace="test" \
--configmap-name="testInventorySubscriptionConfigmap" \
--cloud-id=123
```

Note: By default, the namespace of "orantest" and "oran-infra-inventory-sub" are used currently.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start infrastructure-inventory-subscription-server --help
```

##### Run and Debug

Inside _VS Code_ use the _Run and Debug_ option with the `start
infrastructure-inventory-subscription-server` [configuration](.vscode/launch.json).

##### Request Examples

###### GET Infrastructure Inventory Subscription List

To get a list of resource subscriptions:
```
$ curl -s http://localhost:8004/o2ims-infrastructureInventory/v1/subscriptions | jq
```

###### GET Infrastructure Inventory Subscription Information

To get all the information about an existing resource subscription:
```
$ curl -s http://localhost:8004/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```

###### POST a new Infrastructure Inventory Subscription Information

To add a new resource subscription:
```
$ curl -s -X POST \
--header "Content-Type: application/json" \
-d @infra-sub.json http://127.0.0.1:8004/o2ims-infrastructureInventory/v1/subscriptions | jq
```
Where the content of `infra-sub.json` is as follows:
```
{
  "consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
  "filter": "(eq,extensions/country,US);",
  "callback": "https://128.224.115.15:1081/smo/v1/o2ims_inventory_observer"
}
```

###### DELETE an Infrastructure Inventory Subscription

To delete an existing resource subscription:
```
$ curl -s -X DELETE \
http://localhost:8004/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```

#### Alarm server

The alarm server exposes endpoints for retrieving alarms (AlarmEventRecord objects).
The server relies on the Alertmanager API from Observability operator.
Follow the these [instructions](docs/dev/env_acm.md#observability) to enable
and configure Observability.

The required URL and token can be obtained
as follows:

```
$ export BACKEND_URL=$(
  oc get route -n open-cluster-management-observability alertmanager -o json |
  jq -r '"https://" + .spec.host'
)
$ export BACKEND_TOKEN=$(
  oc create token -n openshift-oauth-apiserver oauth-apiserver-sa --duration=24h
)
$ export RESOURCE_SERVER_URL=http://localhost:8002/o2ims-infrastructureInventory/v1/
$ export INSECURE_SKIP_VERIFY=true
```

Start the resource server with a command like this:

```
$ ./oran-o2ims start alarm-server \
--log-level=debug \
--log-file=stdout \
--api-listener-address=localhost:8003 \
--metrics-listener-address="127.0.0.1:8008" \
--cloud-id=123 \
--backend-url="${BACKEND_URL}" \
--backend-token="${BACKEND_TOKEN}" \
--resource-server-url="${RESOURCE_SERVER_URL}"
```

Notes: 
* See more details regarding `api-listener-address` and `cloud-id` in the previous [section](#deployment-manager-server).
* The alarm server requires the `resource-server-url`, which is needed for fetching information about resources that are associated with retrieved alarms.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start alarm-server --help
```

##### Run and Debug

Inside _VS Code_ use the _Run and Debug_ option with the `start
alarm-server` [configuration](.vscode/launch.json).

##### Requests Examples

###### GET Alarm List

To get a list of alarms:
```
$ curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarms | jq
```

###### GET an Alarm

To get a specific alarm:
```
$ curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarms/{alarmEventRecordId} | jq
```

###### GET Alarm Probable Causes

To get a list of alarm probable causes:
```
$ curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarmProbableCauses | jq
```

Notes:
* This API is not defined by O2ims Interface Specification.
* The server supports the `alarmProbableCauses` endpoint for exposing a custom list of probable causes.
* The list is available in [data folder](internal/files/alarms/probable_causes.json). Can be customized and maintained as required.

#### Alarm Subscription server

To use the configmap to persist the subscriptions, the namespace "orantest" should be created at hub cluster for now.

Start the alarm subscription server with a command like this:

```
$./oran-o2ims start alarm-subscription-server \
--log-file="servers.log" \
--log-level="debug" \
--log-field="server=alarm-subscription" \
--log-field="pid=%p" \
--api-listener-address="127.0.0.1:8006" \
--metrics-listener-address="127.0.0.1:8008" \
--namespace="test" \
--configmap-name="testConfigmap" \
--cloud-id="123" 
```

Note that by default all the servers listen on `localhost:8000`, so there will
be conflicts if you try to run multiple servers in the same machine. The
`--api-listener-address` and `--metrics-listener-address` options are used to select a port number that isn't in use.

The `cloud-id` is any string that you want to use as identifier of the O-Cloud instance.

By default, the namespace of "orantest" and configmap-name of "oran-o2ims-alarm-subscriptions" are used.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start alarm-subscription-server --help
```

You can send requests with commands like this:

```
$ curl -s http://localhost:8001/o2ims-infrastructureMonitoring/v1/alarmSubscriptions | jq
```
Above example will get a list of existing alarm subscriptions

```
$ curl -s -X POST --header "Content-Type: application/json" -d @subscription.json http://localhost:8000/o2ims-infrastructureMonitoring/v1/alarmSubscriptions
```
Above example will post an alarm subscription defined in subscription.json file 

Inside _VS Code_ use the _Run and Debug_ option with the `start
alarm-subscription-server` [configuration](.vscode/launch.json).

#### Alarm Notification server

The alarm-notification-server should use together with alarm subscription server. The alarm subscription sever accept and manages the alarm subscriptions. The alarm notificaton servers synch the alarm subscriptions via perisist storage. To use the configmap to persist the subscriptions, the namespace "orantest" should be created at hub cluster for now (will use official oranims namespace in future). Alarm subscripton server and corresonding alarm notification server should have same namespace and configmap-name. The alarm notification server accept the alerts, match the subscription filter, build and send out the alarm notification based on url in the subscription.

The required Resource server URL and token can be obtained as follows:

```
$ export RESOURCE_SERVER_URL=http://localhost:8002/o2ims-infrastructureInventory/v1/
$ export RESOURCE_SERVER_TOKEN=$(
  oc whoami --show-token
)
$ export INSECURE_SKIP_VERIFY=true
```

Start the alarm notification server with a command like this:

```
$./oran-o2ims start alarm-notification-server \
--log-file="servers.log" \
--log-level="debug" \
--log-field="server=alarm-notification" \
--log-field="pid=%p" \
--api-listener-address="127.0.0.1:8010" \
--metrics-listener-address="127.0.0.1:8011" \
--cloud-id="123" \
--namespace="test" \
--configmap-name="testConfigmap" \
--resource-server-url="${RESOURCE_SERVER_URL}"
--resource-server-token="${RESOURCE_SERVER_TOKEN}" \
```

Note that by default all the servers listen on `localhost:8000`, so there will
be conflicts if you try to run multiple servers in the same machine. The
`--api-listener-address` and `--metrics-listener-address` options are used to select a port number that isn't in use.

The `cloud-id` is any string that you want to use as identifier of the O-Cloud instance.

By default, the namespace of "orantest" and configmap-name of "oran-o2ims-alarm-subscriptions" are used.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start alarm-notification-server --help
```

Inside _VS Code_ use the _Run and Debug_ option with the `start
alarm-notification-server` [configuration](.vscode/launch.json).


## Testing API endpoints on a cluster

**NOTE:** If you already have a user account from which you can generate an API access token then you can skip ahead to step
3 assuming you have already stored your access token in a variable called `MY_TOKEN`.

1. Apply the test client service account CR instances.

```shell
$ oc apply -f config/samples/testing/client-service-account-rbac.yaml
```

2. Generate a token to access the API endpoint

```shell
$ MY_SECRET_NAME=$(oc get sa -n oran-o2ims test-client -ojsonpath='{.secrets[0].name}')
$ MY_TOKEN=$(oc get secret -n oran-o2ims ${MY_SECRET_NAME} -ojsonpath='{.metadata.annotations.openshift\.io/token-secret\.value}')
```

3. Access an API endpoint

```shell
MY_CLUSTER=your.domain.com
curl -kq https://o2ims.apps.${MY_CLUSTER}/o2ims-infrastructureInventory/v1/api_version \
-H "Authorization: Bearer ${MY_TOKEN}"
```

## Registering the O2IMS application with the SMO

Once the hub cluster is setup and the O2IMS application is started the end user must update the Inventory CR to
configure the SMO attributes so that the application can register with the SMO. In a production environment this
requires that an OAuth2 authorization server be available and configured with the appropriate client configurations for
both the SMO and the O2IMS applications. In debug/test environments, the OAuth2 can also be used if the appropriate
server and configurations exist but OAuth2 can also be disabled to simplify the configuration requirements.

1. Create a ConfigMap that contains the custom X.509 CA certificate bundle if either of the SMO or OAuth2 server TLS
   certificates are signed by a non-public CA certificate.

```shell
oc create configmap -n oran-o2ims o2ims-custom-ca-certs --from-file=ca-bundle.pem=/some/path/to/ca-bundle.pem
```

2. Create a Secret that contains the OAuth client-id and client-secret for the O2IMS application. These values should
   be obtained from the administrator of the OAuth server that set up the client credentials. The client secrets must
   not
   be stored locally once the secret is created. The values used here are for example purposes only, your values may
   differ for the client-id and will definitely differ for the client-secret.

```shell
oc create secret generic -n oran-o2ims oauth-client-secrets --from-literal=client-id=o2ims-client --from-literal=client-secret=SFuwTyqfWK5vSwaCPSLuFzW57HyyQPHg
```

3. Update the Inventory CR to include the SMO and OAuth configuration attributes. These values will vary depending
   on the domain names used in your environment and by the type of OAuth2 server deployed. Check the configuration
   documentation for the actual server being used.

The following block can be added to the `spec` section of the Inventory CR.

```yaml
    smo:
       url: https://smo.example.com
       registrationEndpoint: /mock_smo/v1/ocloud_observer
       oauth:
          url: https://keycloak.example.com/realms/oran
          clientSecretName: oauth-client-secrets
          tokenEndpoint: /protocol/openid-connect/token
          scopes:
             - profile
             - smo-audience
    caBundleName: o2ims-custom-ca-certs
```

notes:</p>
a) The `caBundleName` can be omitted if step 1 was skipped.</p>
b) The `scopes` attribute will vary based on the actual configuration of the OAuth2 server. At a minimum there must be
scopes established on the server to allow a client to request its profile info (i.e., account info), and the intended
audience identifier (i.e., the OAuth client-id of the SMO application).

4. Once the Inventory CR is updated the following condition will be used updated to reflect the status of the SMO
   registration. If an error occurred that prevented registration from completing the error will be noted here.

```shell
oc describe inventories.o2ims.oran.openshift.io sample
...
Status:
  Deployment Status:
    Conditions:
      Last Transition Time:  2024-10-04T15:39:46Z
      Message:               Registered with SMO at: https://smo.example.com
      Reason:                SmoRegistrationSuccessful
      Status:                True
      Type:                  SmoRegistrationCompleted
...
```
