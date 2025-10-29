<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Prerequisites

- [Prerequisites](#prerequisites)
  - [Hub Cluster Requirements](#hub-cluster-requirements)
  - [mTLS](#mtls)
  - [OAuth Server Expectations/Requirements](#oauth-server-expectationsrequirements)

## Hub Cluster Requirements

### Platform

- OpenShift Container Platform 4.20.0-rc3 or newer

### Required operators and add‑ons on the hub

- Advanced Cluster Management (ACM) v2.14 or newer
  - SiteConfig Operator

    Enable SiteConfig in ACM by running the following command:

    ```console
    oc patch multiclusterhubs.operator.open-cluster-management.io multiclusterhub -n <ACM_NAMESPACE> --type json --patch '[{"op": "add", "path":"/spec/overrides/components/-", "value": {"name":"siteconfig","enabled": true}}]'
    ```

  - Observablity Operator

    Enable Observablity in ACM by following the official guide: [Red Hat ACM Observability - Enabling the Observability service](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.14/html-single/observability/index#enabling-observability)
- Red Hat OpenShift GitOps Operator
- Topology Aware Lifecycle Manager

### Storage

- A default StorageClass that supports ReadWriteOnce (RWO)
- Ensure a free PersistentVolume with at least 20 Gi capacity is available for the operator’s internal database

## mTLS

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
[Ingress Operator in OpenShift Container Platform documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/networking_operators/configuring-ingress#configuring-ingress-controller-tls).

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

## OAuth Server Expectations/Requirements

To ensure interoperability between the SMO, the O-Cloud Manager, and the Authorization Server, these are the
requirements
regarding the OAuth settings and JWT contents.

1. It is expected that the administrator of the Authentication Server has created clients for both the SMO and IMS
   resource servers. For example purposes, this document assumes these to be "smo-client" and "o2ims-client".

2. It is expected that the "smo-client" has been assigned "roles" which map to the Kubernetes RBAC roles
   defined [here](../config/rbac/oran_o2ims_oauth_role_bindings.yaml) according to the level of access required by the SMO.
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
