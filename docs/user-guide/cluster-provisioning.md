<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Cluster Provisioning and Configuration

- [Cluster Provisioning and Configuration](#cluster-provisioning-and-configuration)
  - [Overview](#overview)
  - [Preparation](#preparation)
  - [Day-0 Cluster Provisioning](#day-0-cluster-provisioning)
    - [ProvisioningRequest CR](#provisioningrequest-cr)
    - [Provisioning process walkthrough](#provisioning-process-walkthrough)
    - [Timeout configuration](#timeout-configuration)
    - [Monitoring Provisioning Progress](#monitoring-provisioning-progress)
    - [Delete Provisioned Cluster](#delete-provisioned-cluster)
  - [Day-2 Cluster Configuration Changes](#day-2-cluster-configuration-changes)
  - [Provisioning REST APIs](#provisioning-rest-apis)

## Overview

The O-Cloud Manager provides orchestration of the complete lifecycle of cluster provisioning via the `ProvisioningRequest` CR, starting from hardware provisioning and cluster installation to cluster configuration.

The operator leverages several key components to achieve this:

- [Metal3 Baremetal Operator](https://github.com/metal3-io/baremetal-operator): Manages the `BaremetalHost`, `HostFirmwareSettings` and `HostFirmwareComponents` CRs, provisioning bare‑metal hosts and applying hardware settings.
- [SiteConfig Operator](https://github.com/stolostron/siteconfig): Manages the `ClusterInstance` CR, initiating the cluster installation process.
- [ACM Policy Engine](https://github.com/open-cluster-management-io/governance-policy-propagator): Enforces configuration `Policies` to ensure the installed cluster is properly configured and compliant with operational requirements.

## Preparation

Refer to [Hub Cluster Requirements](./prereqs.md#hub-cluster-requirements) for hub cluster preparation, and [Gitops Layout And Setup](./gitops-layout-and-setup.md) and [Template Overiew](./template-overview.md) for onboarding of ClusterTemplate, ACM PolicyGenerator and other required resources.

Ensure all resources(such as ClusterTemplates, ACM policies and others) have been created on the hub cluster through ArgoCD before start provisioning.

## Day-0 Cluster Provisioning

### ProvisioningRequest CR

A cluster-scoped CR managed by the O-Cloud Manager, providing all neccessary parameters for provisioning a cluster. An example of ProvisioningRequest can be found [here](../../config/samples/v1alpha1_provisioningrequest.yaml).

The [CRD](../../config/crd/bases/clcm.openshift.io_provisioningrequests.yaml)'s `spec` fields include:

- name: Defines a human-readable name for the Provisioning Request.
- description: A brief description of the Provisioning Request.
- templateName: Specifies the base name of the referenced ClusterTemplate.
- templateVersion: Specifies the version of the referenced ClusterTemplate.
- templateParameters: Provides the input data that conforms to the OpenAPI v3 schema defined in the referenced ClusterTemplate.
- extensions(FFS): A set of key-value pairs extending the cluster's configuration.

The status of the provisioning process is tracked via the following `status.conditions`:

- ProvisioningRequestValidated: The ProvisioningRequest has been validated.
- ClusterInstanceRendered: The ClusterInstance has been successfully rendered and validated.
- ClusterResourcesCreated: The necessary cluster resources have been created.
- HardwareTemplateRendered: The hardware template has been successfully rendered.
- HardwareProvisioned: Hardware provisioning is complete.
- HardwareNodeConfigApplied: Hardware node configuration is applied to the rendered ClusterInstance.
- ClusterProvisioned: Cluster installation is complete.
- ConfigurationApplied: Configuration has been successfully applied via ACM enforce policies.

The `status.provisioningStatus` tracks the overall provisioning state.

- pending: During resource preparation, prior to hardware provisioning.
- progressing: When any part of provisioning process begins (hardware provisioning, cluster installation, or cluster configuration).
- fulfilled: When all stages of provisioning process are successfully completed.
- failed: When any stage of provisioning process fails or times out, including resources validation or preparation.

> [!NOTE]

> - The `metadata.name` of a ProvisioningRequest CR must be a valid UUID. Any attempt to create a ProvisioningRequest with an invalid `metadata.name` will be rejected by the webhook.
> - The ProvisioningRequest cannot override extraLabels that are already set in the defaults. If the same key appears in both the ClusterTemplate defaults and the ProvisioningRequest (for example, `extraLabels.ManagedCluster.cluster-version` is `4.18` in the defaults ConfigMap and `4.19` in the ProvisioningRequest),
> the value from the defaults ConfigMap is used.
> - Once cluster installation has started (`provisioningPhase` is `progressing` and `provisioningDetails` shows `Cluster installation is in progress`), any updates to the ClusterInstance fields are disallowed and will be rejected by the webhook.

### Provisioning process walkthrough

The O-Cloud Manager orchestrates the cluster provisioning process, which is initiated by creating a `ProvisioningRequest` CR. Refer to [Provisioning REST APIs](#provisioning-rest-apis) for details on creating a request using the REST API. Below is the general flow of the provisioning process:

**The general success workflow**:

- `status.conditions`:
ProvisioningRequestValidated -> ClusterInstanceRendered -> ClusterResourcesCreated -> HardwareTemplateRendered -> HardwareProvisioned -> HardwareNodeConfigApplied -> ClusterProvisioned -> ConfigurationApplied
- `status.provisioningStatus`:
Pending (Validating and preparing resources)
-> Progressing (Hardware provisioning is in progress)
-> Progressing (Cluster installation is in progress)
-> Progressing (Cluster configuration is being applied)
-> Fulfilled (Provisioning request has completed successfully)

**Steps performed by the O-Cloud Manager as part of cluster provisioning**:

1. O-Cloud Manager first validates the ProvisioningRequest CR, including but not limited to:
    - Verify timeout values for hardware provisioning, cluster installation, or configuration as specified in the respective ConfigMaps if provided.
    - Validate the `clusterInstanceParameters` against the subschema defined in the ClusterTemplate. Any fields not present in the subschema but provided in the `clusterInstanceParameters` are disallowed and will cause validation failure.
    - Validate the merged policy template input data (`policyTemplateParameters` combined with default values in the `policyTemplateDefaults` ConfigMap) against the PolicyTemplate subschema.
2. Render the ClusterInstance CR with the merged ClusterInstance input (data from `clusterInstanceParameters` combined with the default values in the `clusterInstanceDefaults` ConfigMap) and validate it via client dry-run.
3. Prepare the neccessary resources for provisioning.
    - Copy the extra-manifests ConfigMap from the ClusterTemplate namespace to the cluster namespace
    - Copy the pull-secret from the ClusterTemplate namespace to the cluster namespace
    - Create the ConfigMap for templated ACM policies in the ClusterTemplate namespace
4. Render the NodeAllocationRequest CR based on the provided hardware template in the `hwTemplate` resource.

   Example status:

    ```console
    status:
      conditions:
      - lastTransitionTime: "2025-10-01T22:14:26Z"
        message: The provisioning request validation succeeded
        reason: Completed
        status: "True"
        type: ProvisioningRequestValidated
      - lastTransitionTime: "2025-10-01T22:14:26Z"
        message: ClusterInstance rendered and passed dry-run validation
        reason: Completed
        status: "True"
        type: ClusterInstanceRendered
      - lastTransitionTime: "2025-10-01T22:14:26Z"
        message: Cluster resources applied
        reason: Completed
        status: "True"
        type: ClusterResourcesCreated
      - lastTransitionTime: "2025-10-01T22:14:26Z"
        message: Rendered Hardware template successfully
        reason: Completed
        status: "True"
        type: HardwareTemplateRendered
      provisioningStatus:
        provisioningDetails: Validating and preparing resources
        provisioningPhase: pending
        updateTime: "2025-10-01T22:14:26Z"
    ```

5. Create the NodeAllocationRequest to start hardware provisioning. The `provisioningStatus.provisioningPhase`transitions to progressing.
The O‑Cloud Metal3 hardware plugin consumes the CR, selects matching BareMetalHosts using the label selectors from the referenced HardwareTemplate, and allocates them to the request. For each selected host, the plugin:
   - Creates an `AllocatedNode` CR.
   - Applies the hardware settings by creating/updating the relevant Metal3 CRs (HostFirmwareSettings/HostFirmwareComponents).
   - Reports progress through the statuses of both `NodeAllocationRequest` and `AllocatedNode` resources.

    Example status:

    ```console
    status:
      extensions:
        allocatedNodeHostMap:
          metal3-hwplugin-sno1-dell-r740-pool-dell-r740-sno1: sno1.example.com
        nodeAllocationRequestRef:
          hardwareProvisioningCheckStart: "2025-10-01T22:14:27Z"
          nodeAllocationRequestID: metal3-f5a72a45c49c41969b8d
      conditions:
      ...
      - lastTransitionTime: "2025-10-01T22:14:27Z"
        message: Hardware provisioning is in progress
        reason: InProgress
        status: "False"
        type: HardwareProvisioned
      provisioningStatus:
        provisioningDetails: Hardware provisioning is in progress
        provisioningPhase: progressing
        updateTime: "2025-10-01T22:14:27Z"
    ```

6. Wait for hardware provisioning to complete. Once it completes, the Metal3 hardware plugin sends the allocated node infomation (BMC secret, BMC url, interface MAC addresses, etc.) in the `AllocatedNode` status. O-Cloud Manager retrieves this data and updates the rendered ClusterInstance CR with it.

   Example status:

    ```console
    status:
      extensions:
        allocatedNodeHostMap:
          metal3-hwplugin-sno1-dell-r740-pool-dell-r740-sno1: sno1.example.com
        nodeAllocationRequestRef:
          hardwareProvisioningCheckStart: "2025-10-01T22:14:27Z"
          nodeAllocationRequestID: metal3-f5a72a45c49c41969b8d
      conditions:
      ...
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Created
        reason: Completed
        status: "True"
        type: HardwareProvisioned
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Node configuration has been applied to the rendered ClusterInstance
        reason: Completed
        status: "True"
        type: HardwareNodeConfigApplied
      provisioningStatus:
        provisioningDetails: Hardware provisioning is in progress
        provisioningPhase: progressing
        updateTime: "2025-10-01T22:14:27Z"
    ```

7. Create the rendered ClusterInstance to kick off cluster installation. The SiteConfig operator consumes the CR and starts the installation.

    Example status:

    ```console
    status:
      extensions:
        clusterDetails:
          clusterProvisionStartedAt: "2025-10-01T22:47:17Z"
          name: sno1
          ztpStatus: ZTP Not Done
        ...
      conditions:
      ...
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Created
        reason: Completed
        status: "True"
        type: HardwareProvisioned
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Node configuration has been applied to the rendered ClusterInstance
        reason: Completed
        status: "True"
        type: HardwareNodeConfigApplied
      - lastTransitionTime: "2025-10-01T22:47:17Z"
        message: Applied and processed ClusterInstance (sno1) successfully
        reason: Completed
        status: "True"
        type: ClusterInstanceProcessed
      - lastTransitionTime: "2025-10-01T22:47:17Z"
        message: Provisioning cluster
        reason: InProgress
        status: "False"
        type: ClusterProvisioned
      - lastTransitionTime: "2025-10-01T22:47:17Z"
        message: The Cluster is not yet ready
        reason: ClusterNotReady
        status: "False"
        type: ConfigurationApplied
      provisioningStatus:
        provisioningDetails: Cluster installation is in progress
        provisioningPhase: progressing
        updateTime: "2025-10-01T22:47:17Z"
    ```

8. Wait for cluster installation to complete and for the ManagedCluster to become Ready. ACM reconciles the enforce policies and applies the configuration.

    Example status:

    ```console
    status:
      extensions:
        clusterDetails:
          clusterProvisionStartedAt: "2025-10-01T22:47:17Z"
          name: sno1
          ztpStatus: ZTP Not Done
        ...
      conditions:
      ...
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Created
        reason: Completed
        status: "True"
        type: HardwareProvisioned
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Node configuration has been applied to the rendered ClusterInstance
        reason: Completed
        status: "True"
        type: HardwareNodeConfigApplied
      - lastTransitionTime: "2025-10-01T22:47:17Z"
        message: Applied and processed ClusterInstance (sno1) successfully
        reason: Completed
        status: "True"
        type: ClusterInstanceProcessed
      - lastTransitionTime: "2025-10-01T23:35:39Z"
        message: Provisioning completed
        reason: Completed
        status: "True"
        type: ClusterProvisioned
      - lastTransitionTime: "2025-10-01T23:36:01Z"
        message: The configuration is still being applied
        reason: InProgress
        status: "False"
        type: ConfigurationApplied
      provisioningStatus:
        provisioningDetails: Cluster configuration is being applied
        provisioningPhase: progressing
        updateTime: "2025-10-01T23:36:01Z"
    ```

9. After all policies become compliant, set `provisioningStatus.provisioningPhase` to fulfilled and `extensions.clusterDetails.ztpStatus` to ZTP Done. At this point, `provisioningStatus.provisionedResources.oCloudNodeClusterId` is also populated, indicating successful provisioning.

   Example status:

    ```console
    status:
      extensions:
        clusterDetails:
          clusterProvisionStartedAt: "2025-10-01T22:47:17Z"
          name: sno1
          ztpStatus: ZTP Done
        ...
      conditions:
      ...
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Created
        reason: Completed
        status: "True"
        type: HardwareProvisioned
      - lastTransitionTime: "2025-10-01T22:47:16Z"
        message: Node configuration has been applied to the rendered ClusterInstance
        reason: Completed
        status: "True"
        type: HardwareNodeConfigApplied
      - lastTransitionTime: "2025-10-01T22:47:17Z"
        message: Applied and processed ClusterInstance (sno1) successfully
        reason: Completed
        status: "True"
        type: ClusterInstanceProcessed
      - lastTransitionTime: "2025-10-01T23:35:39Z"
        message: Provisioning completed
        reason: Completed
        status: "True"
        type: ClusterProvisioned
      - lastTransitionTime: "2025-10-01T23:50:55Z"
        message: The configuration is up to date
        reason: Completed
        status: "True"
        type: ConfigurationApplied
      provisioningStatus:
        provisionedResources:
          oCloudNodeClusterId: 1929b3c9-276e-45ab-aae6-0b91c9985caf
        provisioningDetails: Provisioning request has completed successfully
        provisioningPhase: fulfilled
        updateTime: "2025-10-01T23:50:55Z"
    ```

### Timeout configuration

Each stage of the cluster provisioning process has a default timeout. If an operation does not complete within the allowed time, the provisioning process is considered failed, and the relevant status conditions and overall provisioning status will be updated to reflect the timeout.

Default timeouts:

- Hardware provisioning: 90m
- Cluster installation: 90m
- Cluster configuration: 30m

#### Hardware Provisioning Timeout

The timeout is configured in the `HardwareTemplate` resource.

Configure hardware provisioning timeout in the `spec.templates.hwTemplate` hardware template resource:

``` yaml
spec:
  hardwareProvisioningTimeout: "100m"
```

If not specified, the default timeout value (90m) will be applied.

#### Cluster Installation Timeout

For cluster installation, set in the `spec.templates.clusterInstanceDefaults` ConfigMap:

``` yaml
data:
  clusterInstallationTimeout: "100m"
```

#### Cluster Configuration Timeout

For cluster configuration, set in the `spec.templates.policyTemplateDefaults` ConfigMap:

``` yaml
data:
  clusterConfigurationTimeout: "40m"
```

### Monitoring Provisioning Progress

You can monitor the progress either from the oc CLI or via the O‑Cloud Manager REST APIs.

### Using the CLI

```console
watch -n 1 "oc get provisioningrequests.clcm.openshift.io -n oran-o2ims 123e4567-e89b-12d3-a456-426614174000"
```

#### Using the REST API

Refer to [Provisioning REST APIs](#provisioning-rest-apis) to setup token and base url.

```console
watch -n 1 "curl -sk -H 'Authorization: Bearer ${MY_TOKEN}' '${BASE_URL}/provisioningRequests/123e4567-e89b-12d3-a456-426614174000' | jq -r '.status'"
```

### Delete Provisioned Cluster

Deleting the ProvisioningRequest CR initiates the deletion of a provisioned cluster. O-Cloud manager sets the ProvisioningPhase to `deleting`, ensuring that all dependent resources are fully cleaned up before completing the deletion.

During this process, the associated BareMetalHost resources transition to the `deprovisioning` state. Once deprovisioning completes, the hosts return to the `available` state, and the ProvisioningRequest CR is removed.

```console
oc get pr 123e4567-e89b-12d3-a456-426614174000
NAME                                   DISPLAYNAME       AGE     PROVISIONPHASE   PROVISIONDETAILS
123e4567-e89b-12d3-a456-426614174000   sno1-request      71m     deleting         Deletion is in progress
```

For details on deleting a ProvisioningRequest using the REST API, refer to [Provisioning REST APIs](#provisioning-rest-apis).

## Day-2 Cluster Configuration Changes

Refer to [Cluster Configuration](./cluster-configuration.md) for guidelines on performing day-2 configuration changes.

## Provisioning REST APIs

O-Cloud manager provides the following provisioning REST APIs.

First, acquire an authorization token as described in [Testing API endpoints on a cluster](./environment-setup.md#testing-api-endpoints-on-a-cluster).
Then, get API endpoint URLs.

```console
export API_URI=$(oc get route -n oran-o2ims -o jsonpath='{.items[?(@.spec.path=="/o2ims-infrastructureProvisioning")].spec.host}')
export BASE_URL="https://${API_URI}/o2ims-infrastructureProvisioning/v1"
```

### List all ProvisioningRequests

```console
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" "${BASE_URL}/provisioningRequests" | jq
```

### Get a ProvisioningRequest by `provisioningRequestId`

```console
curl -ks -H "Authorization: Bearer ${MY_TOKEN}" \
  "${BASE_URL}/provisioningRequests/123e4567-e89b-12d3-a456-426614174000" | jq
```

### Create a ProvisioningRequest

```console
curl -sk -X POST \
-H 'Content-Type: application/json' \
-H "Authorization: Bearer $MY_TOKEN" \
"${BASE_URL}/provisioningRequests" -d \
'{
  "provisioningRequestId": "123e4567-e89b-12d3-a456-426614174000",
  "name": "sno1-request",
  "description": "provision sno1",
  "templateName": "sno-ran-du",
  "templateVersion": "v4.18.5-v1",
  "templateParameters": {
    "nodeClusterName": "sno1",
    "oCloudSiteId": "local-west",
    "policyTemplateParameters": {
      "sriov-network-pfNames-1": "[\"ens1f2\"]",
      "sriov-network-pfNames-2": "[\"ens1f3\"]"
    },
    "clusterInstanceParameters": {
      "extraLabels": {
        "ManagedCluster": {
          "test-label": "true"
        }
      },
      "clusterName": "sno1",
      "nodes": [
        {
          "hostName": "sno1.example.com",
          "nodeNetwork": {
            "config": {
              "dns-resolver": {
                "config": {
                  "server": ["198.51.100.20"]
                }
              },
              "routes": {
                "config": [
                  {
                    "next-hop-address": "192.0.2.254"
                  }
                ]
              },
              "interfaces": [
                {
                  "ipv6": {
                    "enabled": false
                  },
                  "ipv4": {
                    "enabled": true,
                    "address": [
                      {
                        "ip": "192.0.2.34",
                        "prefix-length": 24
                      }
                    ]
                  }
                }
              ]
            }
          }
        }
      ]
    }
  }
}'
```

### Update a ProvisioningRequest

```console
curl -sk -X PUT \
-H 'Content-Type: application/json' \
-H "Authorization: Bearer $MY_TOKEN" \
"${BASE_URL}/provisioningRequests/123e4567-e89b-12d3-a456-426614174000" -d \
'{
  "provisioningRequestId": "123e4567-e89b-12d3-a456-426614174000",
  "name": "sno1-request",
  "description": "provision sno1",
  "templateName": "sno-ran-du",
  "templateVersion": "v4.18.5-v2",
  "templateParameters": {
    "nodeClusterName": "sno1",
    "oCloudSiteId": "local-west",
    "policyTemplateParameters": {
      "sriov-network-pfNames-1": "[\"ens1f2\"]",
      "sriov-network-pfNames-2": "[\"ens1f3\"]"
    },
    "clusterInstanceParameters": {
      "extraLabels": {
        "ManagedCluster": {
          "test-label": "true"
        }
      },
      "clusterName": "sno1",
      "nodes": [
        {
          "hostName": "sno1.example.com",
          "nodeNetwork": {
            "config": {
              "dns-resolver": {
                "config": {
                  "server": ["198.51.100.20"]
                }
              },
              "routes": {
                "config": [
                  {
                    "next-hop-address": "192.0.2.254"
                  }
                ]
              },
              "interfaces": [
                {
                  "ipv6": {
                    "enabled": false
                  },
                  "ipv4": {
                    "enabled": true,
                    "address": [
                      {
                        "ip": "192.0.2.34",
                        "prefix-length": 24
                      }
                    ]
                  }
                }
              ]
            }
          }
        }
      ]
    }
  }
}'
```

### Delete a ProvisioningRequest

```console
curl -sk -X DELETE -H "Authorization: Bearer ${MY_TOKEN}" \
  "${BASE_URL}/provisioningRequests/123e4567-e89b-12d3-a456-426614174000"
```
