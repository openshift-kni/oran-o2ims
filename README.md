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

Note: see more details regarding `api-listener-address` and `cloud-id` in the previous [section](#deployment-manager-server).

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

To get a list of resource pools:
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
--cloud-id="123" 
```

Note that by default all the servers listen on `localhost:8000`, so there will
be conflicts if you try to run multiple servers in the same machine. The
`--api-listener-address` and `--metrics-listener-address` options are used to select a port number that isn't in use.

The `cloud-id` is any string that you want to use as identifier of the O-Cloud instance.

For more information about other command line flags use the `--help` command:

```
$ ./oran-o2ims start alarm-subscription-server --help
```

You can send requests with commands like this:

```
$ curl -s http://localhost:8001/o2ims-infrastructureMonitoring/v1/alarmSubscriptions | jq
```
Above example will get a list of existing alarm subscriptions

Inside _VS Code_ use the _Run and Debug_ option with the `start
alarm-subscription-server` [configuration](.vscode/launch.json).