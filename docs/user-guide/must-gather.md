# Must-Gather

The O-Cloud Manager provides a custom must-gather image for collecting
troubleshooting data from a cluster. This collects resources and logs that are
not included in the standard OpenShift must-gather.

## Collected data

The following data is collected:

- O-Cloud Manager custom resources (from all namespaces)
  - `clcm.openshift.io`: ClusterTemplates, ProvisioningRequests,
    HardwarePlugins, HardwareProfiles, HardwareTemplates
  - `ocloud.openshift.io`: Inventories
  - `plugins.clcm.openshift.io`: NodeAllocationRequests, AllocatedNodes
- Metal3 resources (from all namespaces)
  - BareMetalHosts
  - HostFirmwareSettings
  - HostFirmwareComponents
  - PreprovisioningImages
  - HardwareData
  - Secrets referenced by BMH `preprovisioningNetworkDataName` fields
- Pod logs
  - All O-Cloud Manager pods (controller manager, resource-server,
    cluster-server, alarms-server, artifacts-server, provisioning-server,
    postgres-server, metal3-hardwareplugin-server,
    hardwareplugin-manager-server)
  - Metal3 pods in `openshift-machine-api` (metal3-baremetal-operator, metal3)
  - Both current and previous container logs are collected

## Must-gather image versions

The must-gather image is tagged by the branch minor version. Use the tag that
matches the O-Cloud Manager release branch deployed on your hub cluster:

| O-Cloud Manager release | Must-gather image tag |
|---|---|
| release-4.21 | `4.21` or `4.21.0` |
| main (4.22) | `4.22`, `4.22.0`, or `latest` |

To determine the installed ACM version for the ACM must-gather image:

```shell
oc get multiclusterhubs.operator.open-cluster-management.io -A \
  -o jsonpath='{.items[0].status.currentVersion}{"\n"}'
```

## Usage

Run the must-gather using the O-Cloud Manager must-gather image:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0
```

To specify a custom output directory:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0 --dest-dir=must-gather/tmp
```

To include ACM resources and logs (e.g., ClusterInstance, SiteConfig), combine
with the ACM must-gather image. Replace `<ACM-version>` with your installed ACM
version (e.g., `v2.14`):

```shell
oc adm must-gather \
  --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0 \
  --image=registry.redhat.io/rhacm2/acm-must-gather-rhel9:v<ACM-version>
```

To also include the standard OpenShift must-gather:

```shell
oc adm must-gather \
  --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0 \
  --image=registry.redhat.io/rhacm2/acm-must-gather-rhel9:v<ACM-version> \
  --image=quay.io/openshift/origin-must-gather
```

## Data privacy

> [!NOTE]
> The O-Cloud Manager must-gather does not collect BMC credentials. The only Secrets collected are the
> preprovisioning network data Secrets (nmstate configuration) referenced by
> BareMetalHosts. The collected data does include IP addresses, hostnames, and cluster
> configuration details — review before sharing externally if needed.

## Analyzing collected data

The must-gather output is organized by resource type. After extracting the archive:

```shell
tar xvf must-gather.tar.gz
cd must-gather.local.*/<image-digest>/
```

Key locations within the output:

| Path | Contents |
|---|---|
| `clcm/` | O-Cloud Manager CRs (ClusterTemplates, ProvisioningRequests, etc.) |
| `ocloud/` | Inventory CRs |
| `plugins/` | NodeAllocationRequests, AllocatedNodes |
| `metal3/` | BareMetalHosts, HostFirmwareSettings, HostFirmwareComponents, HardwareData |
| `metal3/preprovisioning-secrets/` | Secrets referenced by BMH preprovisioningNetworkDataName |
| `logs/ocloud-manager/` | Pod logs from O-Cloud Manager namespace |
| `logs/metal3/` | Pod logs from Metal3 pods in openshift-machine-api |

To quickly check a ProvisioningRequest status from the collected data:

```shell
cat clcm/provisioningrequests/<namespace>/<name>.yaml | grep -A 5 "provisioningPhase"
```
