<!-- vscode-markdown-toc -->
# O-RAN O2IMS

* [Operator Deployment](#operator-deployment)
  * [Deploy the operator on your cluster](#deploy-the-operator-on-your-cluster)
  * [Update Inventory CR](#update-inventory-cr)
* [Local Deployment Start](#local-deployment-start)
  * [Build binary](#build-binary)
  * [Run](#run)
    * [Metadata server](#metadata-server)
    * [Deployment manager server](#deployment-manager-server)
    * [Resource server](#resource-server)
    * [Alarm server](#alarm-server)
    * [Alarm Subscription server](#alarm-subscription-server)
    * [Alarm Notification server](#alarm-notification-server)
* [Testing API endpoints on a cluster](#testing-api-endpoints-on-a-cluster)
* [Registering the O2IMS application with the SMO](#registering-the-o2ims-application-with-the-smo)
* [Using the development debug mode to attach the DLV debugger](#using-the-development-debug-mode-to-attach-the-dlv-debugger)
* [Request Examples](#request-examples)
  * [Query the Metadata server](#query-the-metadata-server)
    * [GET api_versions](#get-api_versions)
    * [GET O-Cloud infrastructure information](#get-o-cloud-infrastructure-information)
  * [Query the Deployment manager server](#query-the-deployment-manager-server)
    * [GET deploymentManagers List](#get-deploymentmanagers-list)
    * [GET field or fields from the deploymentManagers List](#get-field-or-fields-from-the-deploymentmanagers-list)
    * [GET deploymentManagers List using filter](#get-deploymentmanagers-list-using-filter)
  * [Query the Resource server](#query-the-resource-server)
    * [GET Resource Type List](#get-resource-type-list)
    * [GET Specific Resource Type](#get-specific-resource-type)
    * [GET Resource Pool List](#get-resource-pool-list)
    * [GET Specific Resource Pool](#get-specific-resource-pool)
    * [GET all Resources of a specific Resource Pool](#get-all-resources-of-a-specific-resource-pool)
  * [Query the Infrastructure Inventory Subscription (Resource Server)](#query-the-infrastructure-inventory-subscription-resource-server)
    * [GET Infrastructure Inventory Subscription List](#get-infrastructure-inventory-subscription-list)
    * [GET Infrastructure Inventory Subscription Information](#get-infrastructure-inventory-subscription-information)
    * [POST a new Infrastructure Inventory Subscription Information](#post-a-new-infrastructure-inventory-subscription-information)
    * [DELETE an Infrastructure Inventory Subscription](#delete-an-infrastructure-inventory-subscription)

<!-- vscode-markdown-toc-config
	numbering=false
	autoSave=false
	/vscode-markdown-toc-config -->
<!-- /vscode-markdown-toc -->

This project is an implementation of the O-RAN O2 IMS API on top of
OpenShift and ACM.

Note that at this point this is just experimental and at its very beginnings,
so don't try to use it for anything close to a production environment.

***Note: this README is only for development purposes.***

## Operator Deployment

The ORAN O2 IMS implementation in OpenShift is managed by the IMS operator.
It configures the different components defined in the specification:
the deployment manager service, the resource server, alarm server, subscriptions to resource and alert.

The IMS operator will create an O-Cloud API that will be available to be queried, for instance from a SMO. It
also provides a configuration mechanism using a Kubernetes custom resource definition (CRD) that allows the hub cluster administrator to configure the different IMS microservices properly.

### Deploy the operator on your cluster

The IMS operator is installed on an OpenShift cluster where Red Hat Advanced Cluster Management for Kubernetes (RHACM), a.k.a. hub cluster, is installed too. Let’s install the operator.
You can use the latest automatic build from the [openshift-kni namespace](https://quay.io/repository/openshift-kni/oran-o2ims-operator) in quay.io or build your container image with the latest code.

If you want to build the image yourself and push it to your registry right after:

> :warning: Replace the USERNAME and IMAGE_NAME values with the full name of your container image.

```bash
$ export USERNAME=your_user
$ export IMAGE_NAME=quay.io/${USERNAME}/oran-o2ims:latest
$ git clone https://github.com/openshift-kni/oran-o2ims.git
$ cd oran-o2ims
$ make docker-build docker-push CONTAINER_TOOL=podman IMG=${IMAGE_NAME}

..REDACTED..
Update dependencies
hack/update_deps.sh
hack/install_test_deps.sh
Downloading golangci-lint
…
[3/3] STEP 5/5: ENTRYPOINT ["/usr/bin/oran-o2ims"]
[3/3] COMMIT quay.io/${USERNAME}/oran-o2ims:v4.16
--> eaa55268bfff
Successfully tagged quay.io/${USERNAME}/oran-o2ims:latest
eaa55268bfffeb23644c545b3d0a768326821e0afea8b146c51835b3f90a9d0c
```

Now, let's deploy the operator. If you want to deploy your already built image then add the `IMG=${IMAGE_NAME}` argument to the `make` command:

```sh
$ make deploy install

… REDACTED …
Update dependencies
hack/update_deps.sh
hack/install_test_deps.sh
Downloading golangci-lint
… REDACTED …
$PATH/oran-o2ims/bin/kustomize build config/default | $PATH/oran-o2ims/bin/kubectl apply -f -
namespace/oran-o2ims created
serviceaccount/oran-o2ims-controller-manager created
role.rbac.authorization.k8s.io/oran-o2ims-leader-election-role created
clusterrole.rbac.authorization.k8s.io/oran-o2ims-manager-role created
clusterrole.rbac.authorization.k8s.io/oran-o2ims-metrics-reader created
clusterrole.rbac.authorization.k8s.io/oran-o2ims-proxy-role created
rolebinding.rbac.authorization.k8s.io/oran-o2ims-leader-election-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/oran-o2ims-manager-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/oran-o2ims-proxy-rolebinding created
configmap/oran-o2ims-env-config created
service/oran-o2ims-controller-manager-metrics-service created
deployment.apps/oran-o2ims-controller-manager created
```

The operator and the default enabled components are installed in the `oran-o2ims` namespace, which is created during the install.

```bash
$ oc get pods -n oran-o2ims-system
NAME                                             READY   STATUS    RESTARTS   AGE
oran-o2ims-controller-manager-8668794cdc-27xvf   2/2     Running   0          9m15s
deployment-manager-server-565f5cc68d-gwhmh       2/2     Running   0          8m50s
metadata-server-7f4f8f87fb-zc7s8                 2/2     Running   0          8m50s
resource-server-8668dffd44-xn9pj                 2/2     Running   0          8m50s
```

Several routes were created in the same namespace too. The `HOST` column is the URI where the o2ims API will be listening from outside the OpenShift cluster, for instance where the SMO will connect to.

```sh
$ oc get route -n oran-o2ims
NAME        HOST/PORT                                                   PATH                                                   SERVICES                    PORT   TERMINATION          WILDCARD
api-2fv99   o2ims.apps.example.com   /                                                      metadata-server             api    reencrypt/Redirect   None
api-cbvbz   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/resourceTypes        resource-server             api    reencrypt/Redirect   None
api-v92jb   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/deploymentManagers   deployment-manager-server   api    reencrypt/Redirect   None
api-xllml   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/resourcePools        resource-server             api    reencrypt/Redirect   None
```

The operator by default creates a default inventory CR in the oran-o2ims namespace:

```bash
$ oc get inventory -n oran-o2ims
NAME      AGE
default   4m20s
```

> :warning: Currently, the following components are enabled by default.

```sh
$ oc get inventory -n oran-o2ims -oyaml

```yaml
apiVersion: o2ims.oran.openshift.io/v1alpha1
kind: Inventory
metadata:
  creationTimestamp: "2024-11-07T09:26:12Z"
  generation: 1
  name: default
  namespace: oran-o2ims
  resourceVersion: "279507769"
  uid: aeb85580-f7ca-4861-8c38-3f57902b1617
spec:
  alarmSubscriptionServerConfig:
    enabled: false
  deploymentManagerServerConfig:
    backendType: regular-hub
    enabled: true
  metadataServerConfig:
    enabled: true
  resourceServerConfig:
    enabled: true
```

### Update Inventory CR

The `Inventory` custom resource (CR) is the way to configure the different components of the IMS operator. The default Inventory CR that is created by the system is sufficient to enable the basic API functionality.

The end user must edit the default Inventory CR to enable communication with the SMO instance after the SMO has been configured to accept connections from this O-Cloud.
Specifically, the `cloudID` and `smo` attributes will need to be configured appropriately. The `cloudID` is the globally unique UUID value assigned to this O-Cloud by the SMO.

```yaml
apiVersion: o2ims.oran.openshift.io/v1alpha1
kind: Inventory
metadata:
  annotations:
  labels:
    app.kubernetes.io/created-by: oran-o2ims
    app.kubernetes.io/instance: orano2ims-sample
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: orano2ims
    app.kubernetes.io/part-of: oran-o2ims
  name: default
  namespace: oran-o2ims
spec:  
  smo:
    url: http://smo.example.com
    registrationEndpoint: /mock_smo/v1/ocloud_observer
  #alarmSubscription is not yet available, enable it otherwise.
  alarmSubscriptionServerConfig:
    enabled: false 
  cloudID: f7fd171f-57b5-4a17-b176-9a73bf6064a4
  deploymentManagerServerConfig:
    enabled: true
  metadataServerConfig:
    enabled: true
  resourceServerConfig:
    enabled: true
```

Apply the patch to the hub cluster:

```bash
$ oc apply -f inventory_sample.yaml 
inventory.o2ims.oran.openshift.io/default configured
```

The status of the different components of the O2IMS operator can be checked by examining the `status` field of the inventory CR.

```bash
oc get inventory default -ojson | jq .status.deploymentStatus.conditions
```

```json
[
  {
    "lastTransitionTime": "2024-11-14T11:29:02Z",
    "message": "Registered with SMO at: http://smo.example.com",
    "reason": "SmoRegistrationSuccessful",
    "status": "True",
    "type": "SmoRegistrationCompleted"
  },
  {
    "lastTransitionTime": "2024-11-14T11:29:02Z",
    "message": "Deployment has minimum availability.",
    "reason": "MinimumReplicasAvailable",
    "status": "True",
    "type": "MetadataServerAvailable"
  },
  {
    "lastTransitionTime": "2024-11-14T11:29:02Z",
    "message": "Deployment has minimum availability.",
    "reason": "MinimumReplicasAvailable",
    "status": "True",
    "type": "ResourceServerAvailable"
  },
  {
    "lastTransitionTime": "2024-11-14T11:29:02Z",
    "message": "Deployment has minimum availability.",
    "reason": "MinimumReplicasAvailable",
    "status": "True",
    "type": "DeploymentServerAvailable"
  }
]
```

Also, you can check the logs of the Pods running in the `oran-o2ims` namespace searching for any error.

## Local Deployment Start

### Build binary

``` bash
make binary
```

### Run

#### Metadata server

The metadata server returns information about the supported versions of the
API. It doesn't require any backend, only the O-Cloud identifier. You can start
it with a command like this:

``` bash
$ ./oran-o2ims start metadata-server \
--log-level=debug \
--log-file=stdout \
--api-listener-address=localhost:8000 \
--metrics-listener-address="127.0.0.1:8008" \
--cloud-id=123
```

You can send requests with commands like these:

``` bash
curl -s http://localhost:8000/o2ims-infrastructureInventory/api_versions | jq
curl -s http://localhost:8000/o2ims-infrastructureInventory/v1 | jq
```

Inside *VS Code* use the *Run and Debug* option with the `start
metadata-server` [configuration](.vscode/launch.json).

#### Deployment manager server

The deployment manager server needs to connect to the non-kubernetes API of the
ACM global hub. If you are already connected to an OpenShift cluster that has
that global hub installed and configured you can obtain the required URL and
token like this:

``` bash
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

``` bash
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

``` bash
./oran-o2ims start deployment-manager-server --help
```

You can send requests with commands like this:

``` bash
curl -s http://localhost:8001/o2ims-infrastructureInventory/v1/deploymentManagers | jq
```

Inside *VS Code* use the *Run and Debug* option with the `start
deployment-manager-server` [configuration](.vscode/launch.json).

#### Resource server

The resource server exposes endpoints for retrieving resource types, resource pools
and resources objects. The server relies on the Search Query API of ACM hub.
Follow the these [instructions](docs/dev/env_acm.md#search-query-api) to enable
and configure the search API access.

The required URL and token can be obtained
as follows:

``` bash
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

``` bash
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

* `--backend-token-file="${BACKEND_TOKEN_FILE}"` can also be used instead of `--backend-token`.
* see more details regarding `api-listener-address` and `cloud-id` in the previous [section](#deployment-manager-server).

For more information about other command line flags use the `--help` command:

``` console
./oran-o2ims start resource-server --help
```

##### Run and Debug resource server

Inside *VS Code* use the *Run and Debug* option with the `start
resource-server` [configuration](.vscode/launch.json).

#### Alarm server

The alarm server exposes endpoints for retrieving alarms (AlarmEventRecord objects).
The server relies on the Alertmanager API from Observability operator.
Follow the these [instructions](docs/dev/env_acm.md#observability) to enable
and configure Observability.

The required URL and token can be obtained
as follows:

``` bash
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

``` bash
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

``` console
./oran-o2ims start alarm-server --help
```

##### Run and Debug alarm server

Inside *VS Code* use the *Run and Debug* option with the `start
alarm-server` [configuration](.vscode/launch.json).

##### Requests Examples

###### GET Alarm List

To get a list of alarms:

``` bash
curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarms | jq
```

###### GET an Alarm

To get a specific alarm:

``` bash
curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarms/{alarmEventRecordId} | jq
```

###### GET Alarm Probable Causes

To get a list of alarm probable causes:

``` bash
curl -s http://localhost:8003/o2ims-infrastructureMonitoring/v1/alarmProbableCauses | jq
```

Notes:

* This API is not defined by O2ims Interface Specification.
* The server supports the `alarmProbableCauses` endpoint for exposing a custom list of probable causes.
* The list is available in [data folder](internal/files/alarms/probable_causes.json). Can be customized and maintained as required.

#### Alarm Subscription server

To use the configmap to persist the subscriptions, the namespace "orantest" should be created at hub cluster for now.

Start the alarm subscription server with a command like this:

``` bash
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

``` bash
./oran-o2ims start alarm-subscription-server --help
```

You can send requests with commands like this:

``` bash
curl -s http://localhost:8001/o2ims-infrastructureMonitoring/v1/alarmSubscriptions | jq
```

Above example will get a list of existing alarm subscriptions

``` bash
curl -s -X POST --header "Content-Type: application/json" -d @subscription.json http://localhost:8000/o2ims-infrastructureMonitoring/v1/alarmSubscriptions
```

Above example will post an alarm subscription defined in subscription.json file

Inside *VS Code* use the *Run and Debug* option with the `start
alarm-subscription-server` [configuration](.vscode/launch.json).

#### Alarm Notification server

The alarm-notification-server should use together with alarm subscription server. The alarm subscription sever accept and manages the alarm subscriptions.
The alarm notificaton servers synch the alarm subscriptions via perisist storage. To use the configmap to persist the subscriptions,
the namespace "orantest" should be created at hub cluster for now (will use official oranims namespace in future). Alarm subscripton server and corresonding
alarm notification server should have same namespace and configmap-name. The alarm notification server accept the alerts, match the subscription filter,
build and send out the alarm notification based on url in the subscription.

The required Resource server URL and token can be obtained as follows:

``` bash
$ export RESOURCE_SERVER_URL=http://localhost:8002/o2ims-infrastructureInventory/v1/
$ export RESOURCE_SERVER_TOKEN=$(
  oc whoami --show-token
)
$ export INSECURE_SKIP_VERIFY=true
```

Start the alarm notification server with a command like this:

``` bash
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

``` bash
./oran-o2ims start alarm-notification-server --help
```

Inside *VS Code* use the *Run and Debug* option with the `start
alarm-notification-server` [configuration](.vscode/launch.json).

## Testing API endpoints on a cluster

> :exclamation: If you already have a user account from which you can generate an API access token then you can skip ahead to step
3 assuming you have already stored your access token in a variable called `MY_TOKEN`.

1. Apply the test client service account CR instances. It will enable us to authenticate against the O2IMS API. It creates the proper cluster role (`oran-o2ims-test-client-role`) and bind it (`oran-o2ims-test-client-binding`) to the service account (`test-client`).

   ```shell
   $ oc apply -f config/testing/client-service-account-rbac.yaml
   serviceaccount/test-client created
   clusterrole.rbac.authorization.k8s.io/oran-o2ims-test-client-role created
   clusterrolebinding.rbac.authorization.k8s.io/oran-o2ims-test-client-binding created
   ```

   Verify a new service account called test-client is created in the oran-o2ims namespace.

   ```sh
   $ oc get sa -n oran-o2ims
   NAME                            SECRETS   AGE
   builder                         0         117m
   default                         0         117m
   deployer                        0         117m
   deployment-manager-server       0         16m
   metadata-server                 0         16m
   oran-o2ims-controller-manager   0         117m
   resource-server                 0         16m
   test-client                     0         15m
   ```

2. Generate a token to access the API endpoint
Export the token in a variable such as MY_TOKEN and adjust the duration of the token to your needs.

   ```sh
   export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)
   ```

   That token will be used as an authorization bearer in our queries sent from the SMO or any other object outside OpenShift:

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

3. Create a Secret that contains a TLS client certificate and key to be used to enable mTLS to the SMO and OAuth2
   authorization servers. The Secret is expected to have the 'tls.crt' and 'tls.key' attributes. The 'tls.crt'
   attribute must contain the full certificate chain having the device certificate first and the root certificate being
   last.

   ```shell
   oc create secret tls -n oran-o2ims o2ims-client-tls-certificate --cert /some/path/to/tls.crt --key /some/path/to/tls.key
   ```

4. Update the Inventory CR to include the SMO and OAuth configuration attributes. These values will vary depending
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
                - roles
          tls:
             clientCertificateName: o2ims-client-tls-certificate
       caBundleName: o2ims-custom-ca-certs
   ```

    notes:</p>
    a) The `caBundleName` can be omitted if step 1 was skipped.</p>
    b) The `scopes` attribute will vary based on the actual configuration of the OAuth2 server. At a minimum there must be
    scopes established on the server to allow a client to request its profile info (i.e., account info), and the intended
    audience identifier (i.e., the OAuth client-id of the SMO application).

5. Once the Inventory CR is updated the following condition will be used updated to reflect the status of the SMO
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

## Using the development debug mode to attach the DLV debugger

The following instructions provide a mechanism to build an image that is based on a more full-featured distro so that
debug tools are available in the image. It also tailors the deployment configuration so that certain features are
disabled which would otherwise cause debugging with a debugger to be more difficult (or impossible).

1. Build and deploy the debug image

   ```shell
   make IMAGE_TAG_BASE=quay.io/${USER}/oran-o2ims VERSION=latest DEBUG=yes build docker-build docker-push install deploy
   ```

2. Forward a port to the Pod to be debugged so the debugger can attach to it. This command will remain active until
   it is terminated with ctrl+c therefore you will need to execute it in a dedicated window (or move it to the
   background).

   ```shell
   oc port-forward -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s 40000:40000
   ```

3. Execute a shell into the Pod to be debugged.

   ```shell
   oc rsh -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s
   ```

4. Attach the DLV debugger to the process. This is usually PID 1, but this may vary based on the deployment. Use the
   same port number that was specified earlier in the `port-forward` command.

   ```shell
   dlv attach --continue --accept-multiclient --api-version 2 --headless --listen :40000 --log 1
   ```

5. Use your IDE's debug capabilities to attach to `localhost:40000` to start your debug session. This will vary based
   on which IDE is being used.

## Request Examples

### Query the Metadata server

> :warning: Double check you already applied the proper permissions to access the O2IMS API and created the test-client service account. See section [Testing API endpoints on a cluster](#testing-api-endpoints-on-a-cluster)

Notice the `API_URI` is the route HOST/PORT column of the oran-o2ims operator.

```sh
$ oc get routes -n oran-o2ims
NAME        HOST/PORT                                                   PATH                                                   SERVICES                    PORT   TERMINATION          WILDCARD
api-2fv99   o2ims.apps.example.com   /                                                      metadata-server             api    reencrypt/Redirect   None
api-cbvbz   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/resourceTypes        resource-server             api    reencrypt/Redirect   None
api-v92jb   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/deploymentManagers   deployment-manager-server   api    reencrypt/Redirect   None
api-xllml   o2ims.apps.example.com   /o2ims-infrastructureInventory/v1/resourcePools        resource-server             api    reencrypt/Redirect   None
```

Export the o2ims endpoint as the API_URI variable so it can be re-used in the requests.

```sh
export API_URI=o2ims.apps.${DOMAIN}
```

#### GET api_versions

To get the api versions supported

```sh
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/api_versions" | jq
```

#### GET O-Cloud infrastructure information

To obtain information from the O-Cloud:

```bash
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1"
```

### Query the Deployment manager server

The deployment manager server (DMS) needs to connect to kubernetes API of the RHACM hub to obtain the required information. Here we can see a couple of queries to the DMS.

#### GET deploymentManagers List

To get a list of all the deploymentManagers (clusters) available in our O-Cloud:

```bash
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers" | jq
```

#### GET field or fields from the deploymentManagers List

To get a list of only the `name` of the available deploymentManagers available in our O-Cloud:

```bash
$ curl --insecure --silent --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/deploymentManagers?fields=name" | jq
```

#### GET deploymentManagers List using filter

To get a list of all the deploymentManagers whose name is **not** local-cluster in our O-Cloud:

```bash
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

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes" | jq
```

#### GET Specific Resource Type

To get information of a specific resource type:

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourceTypes/${resource_type_name} | jq
```

#### GET Resource Pool List

To get a list of available resource pools:

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools" | jq
```

#### GET Specific Resource Pool

To get information of a specific resource pool:

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}" | jq
```

#### GET all Resources of a specific Resource Pool

We can filter down to get all the resources of a specific resourcePool.

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}
/resources" | jq
```

### Query the Infrastructure Inventory Subscription (Resource Server)

#### GET Infrastructure Inventory Subscription List

To get a list of resource subscriptions:

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions | jq
```

#### GET Infrastructure Inventory Subscription Information

To get all the information about an existing resource subscription:

```bash
$ curl -ks --header "Authorization: Bearer ${MY_TOKEN}" 
"https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```

#### POST a new Infrastructure Inventory Subscription Information

To add a new resource subscription:

```bash
$ curl -ks -X POST \
--header "Content-Type: application/json" \
--header "Authorization: Bearer ${MY_TOKEN}" \
-d @infra-sub.json https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions | jq
```

Where the content of `infra-sub.json` is as follows:

``` json
{
  "consumerSubscriptionId": "69253c4b-8398-4602-855d-783865f5f25c",
  "filter": "(eq,extensions/country,US);",
  "callback": "https://128.224.115.15:1081/smo/v1/o2ims_inventory_observer"
}
```

#### DELETE an Infrastructure Inventory Subscription

To delete an existing resource subscription:

```bash
$ curl -ks -X DELETE \
--header "Authorization: Bearer ${MY_TOKEN}" \
https://${API_URI}/o2ims-infrastructureInventory/v1/subscriptions/<subscription_uuid> | jq
```
