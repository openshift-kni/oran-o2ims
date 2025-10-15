# Environment Setup

- [Environment Setup](#environment-setup)
  - [Operator Deployment](#operator-deployment)
    - [Deploy the operator on your cluster](#deploy-the-operator-on-your-cluster)
    - [Deploying operator from catalog](#deploying-operator-from-catalog)
  - [Registering the O-Cloud Manager with the SMO](#registering-the-o-cloud-manager-with-the-smo)
  - [Testing API endpoints on a cluster](#testing-api-endpoints-on-a-cluster)
    - [Acquiring a token using OAuth](#acquiring-a-token-using-oauth)
    - [Acquiring a token using Service Account (development testing only)](#acquiring-a-token-using-service-account-development-testing-only)
    - [Access an API endpoint](#access-an-api-endpoint)

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
- apiVersion: ocloud.openshift.io/v1alpha1
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
        --catalog-image quay.io/${MY_REPO}/oran-o2ims-catalog:v4.21.0 \
        --channel alpha \
        --install-mode OwnNamespace \
        | oc create -f -
catalogsource.operators.coreos.com/oran-o2ims created
namespace/oran-o2ims created
operatorgroup.operators.coreos.com/oran-o2ims created
subscription.operators.coreos.com/oran-o2ims created
```

To undeploy and clean up the installed resources, use the `catalog-undeploy` target:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-o2ims VERSION=4.21.0 catalog-undeploy
hack/catalog-undeploy.sh --package oran-o2ims --namespace oran-o2ims --crd-search "o2ims.*oran"
subscription.operators.coreos.com "oran-o2ims" deleted
clusterserviceversion.operators.coreos.com "oran-o2ims.v4.21.0" deleted
customresourcedefinition.apiextensions.k8s.io "clustertemplates.clcm.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "hardwaretemplates.clcm.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "inventories.ocloud.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "nodeallocationrequests.plugins.clcm.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "allocatednodes.plugins.clcm.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "provisioningrequests.clcm.openshift.io" deleted
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

## Registering the O-Cloud Manager with the SMO

Once the hub cluster is set up and the O-Cloud Manager is started, the end user must update the Inventory CR to
configure the SMO attributes so that the application can register with the SMO. The user must provide the Global
O-Cloud ID value, which is provided by the SMO. This registration is a one-time operation. Once it succeeds, it will
not be repeated.

> :exclamation: For development/debug purposes, if repeating the registration is necessary, the following annotation can
> be applied to the Inventory CR.
>
>```bash
> oc annotate -n oran-o2ims inventories default ocloud.openshift.io/register-on-restart=true
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
   > the [OpenShift Configuring a custom PKI documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/configuring_network_settings/configuring-a-custom-pki)
   > for more details.

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

    - In a development environment, if mTLS is not required, then this can be skipped and the corresponding attributes
      can be omitted from the Inventory CR.

    - In a production environment, it is expected that this certificate should be renewed periodically and managed by
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
   oc describe inventories.ocloud.openshift.io sample
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
[here](../../config/testing/client-service-account-rbac.yaml). It can be applied as follows.

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
