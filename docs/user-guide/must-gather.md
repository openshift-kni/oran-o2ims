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
  - Secrets referenced by BMH `preprovisioningNetworkDataName` fields
- Pod logs
  - All O-Cloud Manager pods (controller manager, resource-server,
    cluster-server, alarms-server, artifacts-server, provisioning-server,
    postgres-server, metal3-hardwareplugin-server,
    hardwareplugin-manager-server)
  - Metal3 pods in `openshift-machine-api` (metal3-baremetal-operator, metal3)
  - Both current and previous container logs are collected

## Usage

Run the must-gather using the O-Cloud Manager must-gather image:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0
```

To specify a custom output directory:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0 --dest-dir=must-gather/tmp
```

To combine with the standard OpenShift must-gather:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0 --image=quay.io/openshift/origin-must-gather
```
