<!-- vscode-markdown-toc -->

# O-RAN O-Cloud Manager

<!-- TOC -->
* [O-RAN O-Cloud Manager](#o-ran-o-cloud-manager)
  * [Operator Deployment](#operator-deployment)
    * [Deploy the operator on your cluster](#deploy-the-operator-on-your-cluster)
    * [Deploying operator from catalog](#deploying-operator-from-catalog)
  * [mTLS pre-requisites](#mtls-pre-requisites)
  * [Registering the O-Cloud Manager with the SMO](#registering-the-o-cloud-manager-with-the-smo)
    * [OAuth Expectations/Requirements](#oauth-expectationsrequirements)
    * [Sample OAuth JWT Token](#sample-oauth-jwt-token)
  * [Testing API endpoints on a cluster](#testing-api-endpoints-on-a-cluster)
    * [Acquiring a token using OAuth](#acquiring-a-token-using-oauth)
    * [Acquiring a token using Service Account (development testing only)](#acquiring-a-token-using-service-account-development-testing-only)
    * [Access an API endpoint](#access-an-api-endpoint)
  * [Using the development debug mode to attach the DLV debugger](#using-the-development-debug-mode-to-attach-the-dlv-debugger)
  * [Request Examples](#request-examples)
    * [Query the Metadata endpoints](#query-the-metadata-endpoints)
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
<!-- TOC -->

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

```console
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

```console
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

```console
$ oc get pods -n oran-o2ims
NAME                                             READY   STATUS    RESTARTS      AGE
alarms-server-5d5cfb75bf-rbp6g                   2/2     Running   0             21s
artifacts-server-c48f6bd99-xnk2n                 2/2     Running   0             21s
cluster-server-68f8946f74-l82bn                  2/2     Running   0             21s
oran-o2ims-controller-manager-555755dbd7-sprs9   2/2     Running   0             26s
postgres-server-674458bfbd-mnzt5                 1/1     Running   0             23s
provisioning-server-86bd6bf6f-kl829              2/2     Running   0             20s
resource-server-6dbd5788df-vpq44                 2/2     Running   0             22s
```

Several routes were created in the same namespace too. The `HOST` column is the URI where the o2ims API will be listening from outside the OpenShift cluster, for instance where the SMO will connect to.

```console
$ oc get route -n oran-o2ims
NAME                       HOST/PORT                                    PATH                                SERVICES              PORT   TERMINATION          WILDCARD
oran-o2ims-ingress-8v8lp   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureArtifacts      artifacts-server      api    reencrypt/Redirect   None
oran-o2ims-ingress-92sf5   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureCluster        cluster-server        api    reencrypt/Redirect   None
oran-o2ims-ingress-gfm9r   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureProvisioning   provisioning-server   api    reencrypt/Redirect   None
oran-o2ims-ingress-n6p9w   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureInventory      resource-server       api    reencrypt/Redirect   None
oran-o2ims-ingress-n9d7w   o2ims.apps.hubcluster2.hub.dev.vz.bos2.lab   /o2ims-infrastructureMonitoring     alarms-server         api    reencrypt/Redirect   None
```

The operator by default creates a default inventory CR in the oran-o2ims namespace:

```console
$ oc get inventory -n oran-o2ims
NAME      AGE
default   4m20s
```

> :warning: Currently, the following components are enabled by default.

```console
$ oc get inventory -n oran-o2ims -oyaml
apiVersion: v1
items:
- apiVersion: o2ims.oran.openshift.io/v1alpha1
  kind: Inventory
  metadata:
    creationTimestamp: "2025-01-22T16:45:32Z"
    generation: 1
    name: default
    namespace: oran-o2ims
    resourceVersion: "116847464"
    uid: e296aede-6309-478b-be10-6fd8f7904324
  spec:
    alarmServerConfig: {}
    clusterServerConfig: {}
    resourceServerConfig: {}
```

### Deploying operator from catalog

To deploy from catalog, first build the operator, bundle, and catalog images, pushing to your repo:

```console
make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-o2ims docker-build docker-push bundle-build bundle-push catalog-build catalog-push
```

You can then use the `catalog-deploy` target to generate the catalog and subscription resources and deploy the operator:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-o2ims catalog-deploy
hack/generate-catalog-deploy.sh \
        --package oran-o2ims \
        --namespace oran-o2ims \
        --catalog-image quay.io/${MY_REPO}/oran-o2ims-catalog:v4.18.0 \
        --channel alpha \
        --install-mode AllNamespaces \
        | oc create -f -
catalogsource.operators.coreos.com/oran-o2ims created
namespace/oran-o2ims created
operatorgroup.operators.coreos.com/oran-o2ims created
subscription.operators.coreos.com/oran-o2ims created
```

To undeploy and clean up the installed resources, use the `catalog-undeploy` target:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-o2ims VERSION=4.18.0 catalog-undeploy
hack/catalog-undeploy.sh --package oran-o2ims --namespace oran-o2ims --crd-search "o2ims.*oran"
subscription.operators.coreos.com "oran-o2ims" deleted
clusterserviceversion.operators.coreos.com "oran-o2ims.v4.18.0" deleted
customresourcedefinition.apiextensions.k8s.io "clustertemplates.o2ims.provisioning.oran.org" deleted
customresourcedefinition.apiextensions.k8s.io "hardwaretemplates.o2ims-hardwaremanagement.oran.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "inventories.o2ims.oran.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "nodepools.o2ims-hardwaremanagement.oran.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "nodes.o2ims-hardwaremanagement.oran.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "provisioningrequests.o2ims.provisioning.oran.org" deleted
namespace "oran-o2ims" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-alarms-server" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-alertmanager" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-cluster-server" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-deployment-manager-server" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-subject-access-reviewer" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-metrics-reader" deleted
clusterrole.rbac.authorization.k8s.io "oran-o2ims-resource-server" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-alarms-server" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-alarms-server-subject-access-reviewer-binding" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-alertmanager" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-cluster-server" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-cluster-server-subject-access-reviewer-binding" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-deployment-manager-server" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-deployment-manager-server-subject-access-reviewer-binding" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-metadata-server-subject-access-reviewer-binding" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-resource-server" deleted
clusterrolebinding.rbac.authorization.k8s.io "oran-o2ims-resource-server-subject-access-reviewer-binding" deleted
catalogsource.operators.coreos.com "oran-o2ims" deleted
```

## mTLS pre-requisites

In a production environment, using mTLS to secure communications between the O-Cloud and the SMO in both directions is
recommended. The O-Cloud manager uses an ingress controller to terminate incoming TLS sessions. Before configuring the
O-Cloud Manager, the ingress controller needs to be re-configured to enforce mTLS. If the ingress controller is shared
with other applications, this will also impact those applications by requiring client certificates on all incoming
connections. If that is not desirable, then consider creating a secondary ingress controller to manage a separate DNS
domain and enable mTLS only on that controller. Alternatively, it can be configured as optional as long as OAuth is
enabled, in which case it will effectively be mandatory since the OAuth implementation of RFC8705 will require that the
client present a certificate.

To configure the primary controller to enable mTLS the following attributes must be set in the `clientTLS` section of
the `spec`. For more information, please refer to the
documention [here](https://docs.openshift.com/container-platform/4.17/networking/networking_operators/ingress-operator.html#configuring-ingress-controller-tls).

```yaml
apiVersion: operator.openshift.io/v1
kind: IngressController
metadata:
  name: default
  namespace: openshift-ingress-operator
spec:
  clientTLS:
    allowedSubjectPatterns:
    - ^/C=CA/ST=Ontario/L=Ottawa/O=Red\ Hat/OU=ORAN/
    clientCA:
      name: ingress-client-ca-certs
    clientCertificatePolicy: Required
...
...
```

1. `allowedSubjectPatterns` is optional but can be set to limit access to specific certificate subjects.
2. `clientCA` is mandatory and must be set to a `ConfigMap` containing a list of CA certificates used to validate
   incoming client certificates.
3. `clientCertificatePolicy` must be set to `Required` to enforce that clients provide a valid certificate. As noted
   above, set to `Optional` if OAuth is enabled and it is not possible to set to `Required` due to other application
   requirements.

## Registering the O-Cloud Manager with the SMO

Once the hub cluster is set up and the O-Cloud Manager is started, the end user must update the Inventory CR to
configure the SMO attributes so that the application can register with the SMO. The user must provide the Global
O-Cloud ID value, which is provided by the SMO. This registration is a one-time operation. Once it succeeds, it will
not be repeated.

> :exclamation: For development/debug purposes, if repeating the registration is necessary, the following annotation can
> be applied to the Inventory CR.
>
>```bash
> oc annotate -n oran-o2ims inventories default o2ims.oran.openshift.io/register-on-restart=true
>```

In a production environment, this requires that an OAuth2 authorization server be available and configured with the
appropriate client configurations for both the SMO and the O-Cloud Manager. In debug/test environments, the OAuth2
can also be used if the appropriate server and configurations exist, but OAuth2 can also be disabled to simplify the
configuration requirements.

1. Create a ConfigMap that contains the custom X.509 CA certificate bundle if either of the SMO or OAuth2 server TLS
   certificates are signed by a non-public CA certificate. This is optional. If not required, then the 'caBundle'
   attribute can be omitted from the Inventory CR.

   > :warning: Do this only if the cluster proxy isn't currently pointing to some other custom CA bundle. If it is
   > pointing to an existing bundle, then the new private certificates need to be appended to the existing set. Refer to
   > the OpenShift documentation for more
   details [here](https://docs.openshift.com/container-platform/4.17/networking/configuring-a-custom-pki.html).

   ```console
   oc create configmap -n openshift-config custom-ca-certs --from-file=ca-bundle.crt=/some/path/to/ca-bundle.crt
   oc patch proxy/cluster --type=merge --patch='{"spec":{"trustedCA":{"name": "custom-ca-certs"}}}'
   ```

2. Create a new empty config map that includes the trust bundle injection label so that we end up with a config
   map that includes the private CA certificates *plus* the full set of public CA certificates.

   ```console
   cat << EOF > o2ims-trusted-ca-bundle.yaml 
   apiVersion: v1
   kind: ConfigMap
   metadata:
     labels:
       config.openshift.io/inject-trusted-cabundle: "true"
     name: o2ims-trusted-ca-bundle
     namespace: oran-o2ims
   EOF
   oc apply -f o2ims-trusted-ca-bundle.yaml
   ```

3. Create a Secret that contains the OAuth client-id and client-secret for the O-Cloud Manager. These values should
   be obtained from the administrator of the OAuth server that set up the client credentials. The client secrets must
   not be stored locally once the secret is created. The values used here are for example purposes only, your values may
   differ for the client-id and will definitely differ for the client-secret.

   ```console
   oc create secret generic -n oran-o2ims oauth-client-secrets --from-literal=client-id=o2ims-client --from-literal=client-secret=SFuwTyqfWK5vSwaCPSLuFzW57HyyQPHg
   ```

4. Create a Secret that contains a TLS client certificate and key to be used to enable mTLS to the SMO and OAuth2
   authorization servers. The Secret is expected to have the 'tls.crt' and 'tls.key' attributes. The 'tls.crt'
   attribute must contain the full certificate chain having the device certificate first, then intermediate
   certificates, and the root certificate being last.

    * In a development environment, if mTLS is not required, then this can be skipped and the corresponding attributes
      can be omitted from the Inventory CR.

    * In a production environment, it is expected that this certificate should be renewed periodically and managed by
      cert-manager. When managing this certificate with cert-manager, it is expected that the CN and DNS SAN values of
      the certificate will be set to the Ingress domain name for the application
      (e.g., o2ims.apps.${CLUSTER_DOMAIN_NAME}). The certificate's extended usage should be set to both `server auth`
      and `client auth`. The certificate should be set in both the `spec.smo.tls.secretName` and
      `spec.ingress.tls.secretName` attributes.

   **cert-manager method (recommended):**
   > :exclamation: ensure that the `LAB_URL` environment variable is set to your DNS domain name as described above.

   ```console
   cat << EOF > oran-o2ims-tls-certificate.yaml
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata:
     name: oran-o2ims-tls-certificate
      namespace: oran-o2ims
   spec:
     secretName: oran-o2ims-tls-certificate
     subject:
       organizations: [ ORAN ]
     commonName: ${LAB_URL}
     usages:
     - server auth
     - client auth
     dnsNames:
     - ${LAB_URL}
     issuerRef:
       name: acme-cert-issuer
       kind: ClusterIssuer
       group: cert-manager.io
     EOF
   
   oc apply -f oran-o2ims-tls-certificate.yaml
     ```

   **manual method:**

   ```console
   oc create secret tls -n oran-o2ims oran-o2ims-tls-certificate --cert /some/path/to/tls.crt --key /some/path/to/tls.key
   ```

5. Update the Inventory CR to include the SMO and OAuth configuration attributes. These values will vary depending
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
                - openid
                - role:ocloud-manager
             usernameClaim: preferred_username
             groupsClaim: roles
          tls:
             secretName: oran-o2ims-tls-certificate
       ingress:
         tls:
           secretName: oran-o2ims-tls-certificate
       caBundleName: o2ims-trusted-ca-bundle
   ```

6. Once the Inventory CR is updated, the following condition will be updated to reflect the status of the SMO
   registration. If an error occurred that prevented registration from completing, then the error will be noted here.

   ```console
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

### OAuth Expectations/Requirements

To ensure interoperability between the SMO, the O-Cloud Manager, and the Authorization Server, these are the
requirements
regarding the OAuth settings and JWT contents.

1. It is expected that the administrator of the Authentication Server has created clients for both the SMO and IMS
   resource servers. For example purposes, this document assumes these to be "smo-client" and "o2ims-client".

2. It is expected that the "smo-client" has been assigned "roles" which map to the Kubernetes RBAC roles
   defined [here](config/rbac/oran_o2ims_oauth_role_bindings.yaml) according to the level of access required by the SMO.
   The "roles" attribute is expected to contain a list of roles assigned to the client.
   For example, one or more of "o2ims-reader", "o2ims-subscriber", "o2ims-maintainer", "o2ims-provisioner", or
   "o2ims-admin".

3. The "o2ims-client" is also expected to be assigned some form of authorization on the SMO. This depends largely on the
   SMO implementation and is somewhat transparent to the O-Cloud Manager. If specific scopes are required for the
   "o2ims-client" to access the SMO resource server, then the Inventory CR must be customized by setting the "scopes"
   attribute.

4. It is expected that the JWT token will contain the mandatory attributes defined in
   [RFC9068](https://datatracker.ietf.org/doc/html/rfc9068) as well as the optional attributes related to "roles".

5. The "aud" attribute is expected to contain a list of intended audiences and must at a minimum include the
   "o2ims-client" identifier.

6. It is expected that all attributes are top-level attributes (i.e., are not nested). For example,
   `"realm_access.roles": ["a", "b", "c"]` is valid, but `"realm_access": {"roles": ["a", "b", "c"]}` is not. In some
   cases, this may require special configuration steps on the Authorization Server to ensure the proper format in the
   JWT tokens.

7. Client certificate binding (i.e., RFC8705) is enabled by default. If the authorization server has inserted a claim
   into the token which contains the SHA256 fingerprint of the client certificate which requested a token then it will
   be validated. If the client certificate which is making the request doesn't match the client certificate that
   requested the token then the request will be rejected. That claim is expected to be `"cnf": {"x5t#S256": "...."}`.
   This is an exception to the rule described in (6) above. See below for an example.

### Sample OAuth JWT Token

The following is a sample JWT payload and header. The signature footer has been removed.

Header:

```json
{
        "alg": "RS256",
        "typ": "JWT",
        "kid": "VJr29clVBAFFo6rbwn4HvTByCH5KbhioharsRbXx3N8"
}
```

Payload:

```json
{
        "exp": 1737565732,
        "iat": 1737564832,
        "jti": "cc0f4c88-fffd-48da-91d8-24b80f1f6955",
        "iss": "https://keycloak.example.com/realms/oran",
        "aud": [
                "o2ims-client"
        ],
        "sub": "34c94cc0-720d-4f29-81e9-b9e794b51e9a",
        "typ": "Bearer",
        "azp": "smo-client",
        "acr": "1",
        "cnf": {
                "x5t#S256": "S1ArY2E4qD_l4lCuNhLh501CxjVJmNFJbLI2RRzigDc"
        },
        "scope": "openid profile role:o2ims-reader role:o2ims-subscriber role:o2ims-maintainer role:o2ims-provisioner",
        "resource_access.o2ims-client.roles": [
                "o2ims-reader",
                "o2ims-subscriber",
                "o2ims-maintainer",
                "o2ims-provisioner"
        ],
        "preferred_username": "service-account-smo-client",
        "clientAddress": "192.168.1.2",
        "client_id": "smo-client"
}
```

## Testing API endpoints on a cluster

Before accessing any O2IMS API endpoint, an access token must be acquired. The approach used depends on the
configuration of the system. The following subsections describe both the OAuth and non-OAuth cases.

### Acquiring a token using OAuth

In a production environment, the system should be configured with mTLS and OAuth enabled. In this configuration, an API
requests must include a valid OAuth JWT token acquired from the authorization server that is configured in the
Inventory CR. To manually acquire a token from the authorization server, a command similar to this one should be used.
This method may vary depending on the type of authorization server used. This example is for a Keycloak server.

```console
export MY_TOKEN=$(curl -s --cert /path/to/client.crt --key /path/to/client.key --cacert /path/to/ca-bundle.crt \
  -XPOST https://keycloak.example.com/realms/oran/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=${SMO_CLIENT_ID} \
  -d client_secret=${SMO_CLIENT_SECRET} \
  -d 'response_type=token id_token' \
  -d 'scope=profile o2ims-audience roles'| jq -j .access_token)
```

### Acquiring a token using Service Account (development testing only)

In a development environment in which OAuth is not being used, the access token must be acquired from a Kubernetes
Service Account. This Service Account must be assigned appropriate RBAC permissions to access the O2IMS API endpoints.
As a convenience, a pre-canned Service Account and ClusterRoleBinding is defined
[here](config/testing/client-service-account-rbac.yaml). It can be applied as follows.

   ```console
   $ oc apply -f config/testing/client-service-account-rbac.yaml
   serviceaccount/test-client created
   clusterrole.rbac.authorization.k8s.io/oran-o2ims-test-client-role created
   clusterrolebinding.rbac.authorization.k8s.io/oran-o2ims-test-client-binding created
   ```

And then the following command can be used to acquire a token.

   ```console
   export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)
   ```

### Access an API endpoint

Note that here the `--cert` and `--key` options can be omitted if not using mTLS and the `--cacert` option can be
removed if the ingress certificate is signed by a public certificate or if you are operating in a development
environment, in which case it can be replaced with `-k`.

   ```console
   MY_CLUSTER=your.domain.com
   curl --cert /path/to/client.crt --key /path/to/client.key --cacert /path/to/ca-bundle.crt -q \
     https://o2ims.apps.${MY_CLUSTER}/o2ims-infrastructureInventory/v1/api_version \
     -H "Authorization: Bearer ${MY_TOKEN}"
   ```

## Using the development debug mode to attach the DLV debugger

The following instructions provide a mechanism to build an image that is based on a more full-featured distro so that
debug tools are available in the image. It also tailors the deployment configuration so that certain features are
disabled which would otherwise cause debugging with a debugger to be more difficult (or impossible).

1. Build and deploy the debug image

   ```console
   make IMAGE_TAG_BASE=quay.io/${USER}/oran-o2ims VERSION=latest DEBUG=yes build docker-build docker-push install deploy
   ```

2. Forward a port to the Pod to be debugged so the debugger can attach to it. This command will remain active until
   it is terminated with ctrl+c therefore you will need to execute it in a dedicated window (or move it to the
   background).

   ```console
   oc port-forward -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s 40000:40000
   ```

3. Execute a shell into the Pod to be debugged.

   ```console
   oc rsh -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s
   ```

4. Attach the DLV debugger to the process. This is usually PID 1, but this may vary based on the deployment. Use the
   same port number that was specified earlier in the `port-forward` command.

   ```console
   dlv attach --continue --accept-multiclient --api-version 2 --headless --listen :40000 --log 1
   ```

5. Use your IDE's debug capabilities to attach to `localhost:40000` to start your debug session. This will vary based
   on which IDE is being used.

## Request Examples

### Query the Metadata endpoints

> :warning: Confirm that an authorization token has already been acquired. See
> section [Testing API endpoints on a cluster](#testing-api-endpoints-on-a-cluster)

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
