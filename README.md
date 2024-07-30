# O-RAN O2IMS

This project is an implementation of the O-RAN O2 IMS API on top of
OpenShift and ACM.

Note that at this point this is just experimental and at its very beginnings,
so don't try to use it for anything close to a production environment.

***Note: this README is only for development purposes.***

<!-- vscode-markdown-toc -->
* 1. [Operator Deployment](#OperatorDeployment)
	* 1.1. [Deploy the operator on your cluster](#Deploytheoperatoronyourcluster)
	* 1.2. [Run](#Run)
		* 1.2.1. [Metadata server](#Metadataserver)
		* 1.2.2. [Deployment manager server](#Deploymentmanagerserver)
		* 1.2.3. [Resource server](#Resourceserver)
* 2. [Local Deployment Start](#LocalDeploymentStart)
	* 2.1. [Build binary](#Buildbinary)
	* 2.2. [Run](#Run-1)
		* 2.2.1. [Metadata server](#Metadataserver-1)
		* 2.2.2. [Deployment manager server](#Deploymentmanagerserver-1)
		* 2.2.3. [Resource server](#Resourceserver-1)
		* 2.2.4. [Infrastructure Inventory Subscription Server (Resource Server)](#InfrastructureInventorySubscriptionServerResourceServer)
		* 2.2.5. [Alarm server](#Alarmserver)
		* 2.2.6. [Alarm Subscription server](#AlarmSubscriptionserver)
		* 2.2.7. [Alarm Notification server](#AlarmNotificationserver)

<!-- vscode-markdown-toc-config
	numbering=true
	autoSave=true
	/vscode-markdown-toc-config -->
<!-- /vscode-markdown-toc -->


##  1. <a name='OperatorDeployment'></a>Operator Deployment

The ORAN O2 IMS implementation in OpenShift is managed by the IMS operator. 
It configures the different components defined in the specification: 
the deployment managers service, the resource server, alert server, subscriptions to resource and alert.

The IMS operator will create an O-Cloud API that will be available to be queried, for instance from a SMO. It
also provides a configuration mechanism using a Kubernetes custom resource definition (CRD) that allows the hub cluster administrator to configure the different IMS microservices properly.


###  1.1. <a name='Deploytheoperatoronyourcluster'></a>Deploy the operator on your cluster

The IMS operator is installed on an OpenShift cluster where Red Hat Advanced Cluster Management for Kubernetes (RHACM), a.k.a. hub cluster, is installed too.
Be sure you are connected to your hub cluster and run:

``` bash
make deploy
```
The operator then is installed in the `oran-o2ims-system` namespace.

```bash
$ oc get pods -n oran-o2ims-system
NAME                                             READY   STATUS    RESTARTS   AGE
oran-o2ims-controller-manager-8668794cdc-27xvf   2/2     Running   0          11s
```

###  1.2. <a name='Run'></a>Run

The ORAN2IMS custom resource is the way to configure the different components of the IMS operator. 
Notice that the ORAN2IMS object must be placed in one of the [allowed namespaces](https://www.google.com/url?q=https://github.com/openshift-kni/oran-o2ims/blob/main/internal/cmd/operator/start_controller_manager.go%23L127&sa=D&source=docs&ust=1722264090981796&usg=AOvVaw1mnJq0RKXe-dd1os-EfDBP), such as the oran-o2ims. 
Let’s configure the Metadata server.

```bash
$ oc create ns oran-o2ims
namespace/oran-o2ims created
```

####  1.2.1. <a name='Metadataserver'></a>Metadata server

The metadata server returns information about the supported versions of the
API. It doesn't require any backend, only the O-Cloud identifier (`cloudID`) and the route where the Metadata
server is available (`ingressHost`).

You can start by creating the ORANO2IMS custom resource in a yaml file:

```yaml
apiVersion: oran.openshift.io/v1alpha1
kind: ORANO2IMS
metadata:
  annotations:
  labels:
    app.kubernetes.io/created-by: oran-o2ims
    app.kubernetes.io/instance: orano2ims-sample
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: orano2ims
    app.kubernetes.io/part-of: oran-o2ims
  name: sample
  namespace: oran-o2ims
spec:
  alarmSubscriptionServerConfig:
    enabled: false
  cloudId: c0332915-6bff-4d8d-8628-0ab3cc2c7e5e #your cloudID
  deploymentManagerServerConfig:
    enabled: false
  image: quay.io/openshift-kni/oran-o2ims:4.16
  ingressHost: oran-o2ims.apps.hub0.inbound-int.se-lab.eng.rdu2.dc.redhat.com #your ingress URI
  metadataServerConfig:
    enabled: true
  resourceServerConfig:
    enabled: false
```

And apply it to the hub cluster. 

```bash
$ oc apply -f sample-oran2ims-metadata.yaml 
orano2ims.oran.openshift.io/sample created
```

Then, a metadata-server Pod is running on the oran-o2ims namespace and a route created with the value included in the previous custom resource, 
specifically in the spec.ingressHost field. That’s the URI where the IMS API will be listening from outside the OpenShift cluster:

```bash
$ oc get pods -n oran-o2ims
NAME                              READY   STATUS    RESTARTS   AGE
metadata-server-c655b559b-6chmx   1/1     Running   0          12s

$ oc get route -n oran-o2ims
NAME        HOST/PORT                                                        PATH                                                   SERVICES                    PORT   TERMINATION          WILDCARD
api-27qrj   oran-o2ims.apps.hub0.inbound-int.se-lab.eng.rdu2.dc.redhat.com   /                                                      metadata-server             api    reencrypt/Redirect   None
```

Before querying the Metadata Server we need to allow that request. 
Therefore we need to create a token associated with an already created serviceAccount named `client`:

```bash
$ export CLIENT_TOKEN=$(oc create token -n oran-o2ims client --duration=24h)
$ export API_URI=oran-o2ims.apps.hub0.inbound-int.se-lab.eng.rdu2.dc.redhat.com
```

##### Requests Examples

###### GET API version

To obtain the API version deployed:

```bash
$ curl --insecure --silent --header "Authorization: Bearer $CLIENT_TOKEN" 
'https://${API_URI}/o2ims-infrastructureInventory/v1/api_versions' | jq
```

###### GET O-Cloud information

The next query obtains O-Cloud information:

```bash
$ curl --insecure --silent --header "Authorization: Bearer $CLIENT_TOKEN" 
'https://${API_URI}/o2ims-infrastructureInventory/v1' | jq
```

####  1.2.2. <a name='Deploymentmanagerserver'></a>Deployment manager server

The deployment manager server needs to connect to kubernetes API of the
RHACM hub. If you are already connected to an OpenShift cluster that has
RHACM installed and configured you can obtain the token like this:

```
$ export BACKEND_TOKEN=$(oc create token -n open-cluster-management multiclusterhub-operator --duration=24h)
```
Next, enable and configure the `deploymentManagerServerConfig` section of the ORANO2IMS CR yaml file.

```yaml
apiVersion: oran.openshift.io/v1alpha1
kind: ORANO2IMS
metadata:
  annotations:
  labels:
    app.kubernetes.io/created-by: oran-o2ims
    app.kubernetes.io/instance: orano2ims-sample
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: orano2ims
    app.kubernetes.io/part-of: oran-o2ims
  name: sample
  namespace: oran-o2ims
spec:
  alarmSubscriptionServerConfig:
    enabled: false
  cloudId: c0332915-6bff-4d8d-8628-0ab3cc2c7e5e
  deploymentManagerServerConfig:
    backendToken: ${BACKEND_TOKEN}
    backendType: regular-hub
    enabled: true
  image: quay.io/openshift-kni/oran-o2ims:4.16.0
  ingressHost: oran-o2ims.apps.hub0.inbound-int.se-lab.eng.rdu2.dc.redhat.com
  metadataServerConfig:
    enabled: true
  resourceServerConfig:
    enabled: false
```
Next, apply the sample ORANO2IMS custom resource with the new configuration. 

```bash
$ oc apply -f sample-oran2ims-dms.yaml 
orano2ims.oran.openshift.io/sample configured
```

See that a new Pod started in the oran-o2ims namespace.

```bash
$ oc get pods -n oran-o2ims
NAME                                         READY   STATUS    RESTARTS   AGE
deployment-manager-server-785997bd4b-r8qbt   1/1     Running   0          3s
metadata-server-c655b559b-tf7f7              1/1     Running   0          13h
```

Check that the Pod started successfully, you can see the output of the Pod to look for errors or check the status of the ORAN2IMS custom resource:

```bash
$ oc get orano2imses.oran.openshift.io sample -ojsonpath={.status.deploymentStatus} | jq
{
  "conditions": [
    {
      "lastTransitionTime": "2024-07-24T07:02:48Z",
      "message": "Deployment has minimum availability.",
      "reason": "MinimumReplicasAvailable",
      "status": "True",
      "type": "MetadataServerAvailable"
    },
    {
      "lastTransitionTime": "2024-07-24T07:17:48Z",
      "message": "Deployment has minimum availability.",
      "reason": "MinimumReplicasAvailable",
      "status": "True",
      "type": "DeploymentServerAvailable"
    }
  ]
}
```

##### Requests Examples

###### GET deploymentManagers List

To get a list of deploymentManagers:

```bash
$ curl --insecure --silent --header "Authorization: Bearer $CLIENT_TOKEN" 
'https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers' | jq
```

###### GET fields from the deploymentManagers List

To get only the name of the available deploymentManagers:

```bash
$ curl --insecure --silent --header "Authorization: Bearer $CLIENT_TOKEN" 
'https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?fields=name' | jq
```

###### GET deploymentManagers List using filter

To get the deploymentManagers whose name is **not** local-cluster.

```bash
$ curl --insecure --silent --header "Authorization: Bearer $CLIENT_TOKEN" 
'https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?filter=(neq,name,local-cluster)' | jq
 | jq
```

####  1.2.3. <a name='Resourceserver'></a>Resource server

The resource server exposes endpoints for retrieving resource types, resource pools
and resources objects. The server relies on the Search Query API of ACM hub.
Follow these [instructions](docs/dev/env_acm.md#search-query-api) to enable
and configure the search API access. The resource server will translate those REST requests and send them to the ACM search server that implements a graphQL API.

The required URL and token can be obtained as follows:

```bash
$ export BACKEND_URL=$(
  oc get route -n open-cluster-management search-api -o json |
  jq -r '"https://" + .spec.host'
)
$ export BACKEND_TOKEN_RS=$(
  oc create token oauth-apiserver-sa -n openshift-oauth-apiserver --duration=48h
  )
```

Next, configure the `resourceServerConfig` section of the ORANO2IMS CR:

```yaml
apiVersion: oran.openshift.io/v1alpha1
kind: ORANO2IMS
metadata:
  annotations:
  labels:
    app.kubernetes.io/created-by: oran-o2ims
    app.kubernetes.io/instance: orano2ims-sample
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: orano2ims
    app.kubernetes.io/part-of: oran-o2ims
  name: sample
  namespace: oran-o2ims
spec:
  alarmSubscriptionServerConfig:
    enabled: false
  cloudId: c0332915-6bff-4d8d-8628-0ab3cc2c7e5e
  deploymentManagerServerConfig:
    backendToken: ${BACKEND_TOKEN}
    backendType: regular-hub
    enabled: true
  image: quay.io/openshift-kni/oran-o2ims:4.16.0
  ingressHost: oran-o2ims.apps.hub0.inbound-int.se-lab.eng.rdu2.dc.redhat.com
  metadataServerConfig:
    enabled: true
  resourceServerConfig:
    backendToken: ${BACKEND_TOKEN_RS}
    backendURL: https://${BACKEND_URL}
    enabled: true
```
Apply the configuration: 

```bash
$ oc apply -f sample-oran2ims-resource.yaml 
orano2ims.oran.openshift.io/sample configured
```

Validate a new Pod is running in the oran-o2ims namespace:

```bash
$ oc get pods
NAME                                         READY   STATUS    RESTARTS   AGE
deployment-manager-server-7488685b78-5wjmh   1/1     Running   0          67m
metadata-server-c655b559b-57x9w              1/1     Running   0          85m
resource-server-846b96545-4f5sj              1/1     Running   0          116s
```

Check that the Pod started successfully, you can see the output of the Pod to look for errors or check the status of the ORAN2IMS custom resource:

```bash
$ oc get orano2imses.oran.openshift.io sample -ojsonpath={.status.deploymentStatus} | jq
{
  "conditions": [
    {
      "lastTransitionTime": "2024-07-24T07:02:48Z",
      "message": "Deployment has minimum availability.",
      "reason": "MinimumReplicasAvailable",
      "status": "True",
      "type": "MetadataServerAvailable"
    },
    {
      "lastTransitionTime": "2024-07-24T07:17:48Z",
      "message": "Deployment has minimum availability.",
      "reason": "MinimumReplicasAvailable",
      "status": "True",
      "type": "DeploymentServerAvailable"
    },
    {
      "lastTransitionTime": "2024-07-24T08:22:49Z",
      "message": "Deployment has minimum availability.",
      "reason": "MinimumReplicasAvailable",
      "status": "True",
      "type": "ResourceServerAvailable"
    }
  ]
}
```

##### Requests Examples

###### GET Resource Type List

To get a list of resource types:

```bash
$ curl -ks --header "Authorization: Bearer $CLIENT_TOKEN" https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes | jq
```

###### GET Resource Pool List

To get a list of resource pools:

```bash
$ curl -ks --header "Authorization: Bearer $CLIENT_TOKEN" https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools | jq
```

###### GET Resource List

To get a list of resources in a resource pool:

```bash
$ curl -ks --header "Authorization: Bearer $CLIENT_TOKEN" ttps://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}
/resources | jq
```


##  2. <a name='LocalDeploymentStart'></a>Local Deployment Start

###  2.1. <a name='Buildbinary'></a>Build binary
``` bash
make binary
```

###  2.2. <a name='Run-1'></a>Run

####  2.2.1. <a name='Metadataserver-1'></a>Metadata server

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

####  2.2.2. <a name='Deploymentmanagerserver-1'></a>Deployment manager server

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

####  2.2.3. <a name='Resourceserver-1'></a>Resource server

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

####  2.2.4. <a name='InfrastructureInventorySubscriptionServerResourceServer'></a>Infrastructure Inventory Subscription Server (Resource Server)

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
http://localhost:8000/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```

####  2.2.5. <a name='Alarmserver'></a>Alarm server

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

####  2.2.6. <a name='AlarmSubscriptionserver'></a>Alarm Subscription server

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

####  2.2.7. <a name='AlarmNotificationserver'></a>Alarm Notification server

The alarm-notification-server should use together with alarm subscription server. The alarm subscription sever accept and manages the alarm subscriptions. The alarm notificaton servers synch the alarm subscriptions via perisist storage. To use the configmap to persist the subscriptions, the namespace "orantest" should be created at hub cluster for now (will use official oranims namespace in future). Alarm subscripton server and corresonding alarm notification server should have same namespace and configmap-name. The alarm notification server accept the alerts, match the subscription filter, build and send out the alarm notification based on url in the subscription.

The required Resource server URL and token can be obtained as follows:

```
$ export RESOURCE_SERVER_URL=http://localhost:8002/o2ims-infrastructureInventory/v1/
$ export RESOURCE_SERVER_TOKEN=$(
  oc whoami --show-token
)
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
