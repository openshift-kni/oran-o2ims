# Cluster Configuration

## Preparation
TO DO

## Initial install and configuration
TO DO

## Day 2 configuration

### Updates to the clusterInstanceParameters field under ProvisioningRequest spec.templateParameters

A ProvisioningRequest can be edited to update:
* the cluster labels and annotations
```yaml
apiVersion: o2ims.provisioning.oran.org/v1alpha1
kind: ProvisioningRequest
metadata:
  finalizers:
    - provisioningrequest.o2ims.provisioning.oran.org/finalizer
  name: sno-ran-du-1
spec:
  description: Provisioning request for basic SNO with sample ACM policies
  name: Dev-main-SNO-Provisioning-sno-ran-du-1
  templateName: sno-ran-du
  templateVersion: v1
  templateParameters:
    clusterInstanceParameters:
      clusterName: sno-ran-du-1
      extraAnnotations:
        ManagedCluster:
          test: test <<< added as a Day 2 change
      extraLabels:
        ManagedCluster:
          sno-ran-du-policy: v1
          test: test <<< added as a Day 2 change
```
* the node labels and annotations
```yaml
apiVersion: o2ims.provisioning.oran.org/v1alpha1
kind: ProvisioningRequest
metadata:
  finalizers:
    - provisioningrequest.o2ims.provisioning.oran.org/finalizer
  name: sno-ran-du-1
spec:
  description: Provisioning request for basic SNO with sample ACM policies
  name: Dev-main-SNO-Provisioning-sno-ran-du-1
  templateName: sno-ran-du
  templateVersion: v1
  templateParameters:
    clusterInstanceParameters:
      clusterName: sno-ran-du-1
      nodes:
      - bmcCredentialsName:
          name: sno-ran-du-1-bmc-secret
        extraAnnotations:
          BareMetalHost:
            test: test <<< added as a Day 2 change
        extraLabels:
          BareMetalHost:
            test: test <<< added as a Day 2 change
```
The `status.conditions` records the success/failure of the update.

The cluster configuration goes to the `ManagedCluster` CR and the nodes configuration to the corresponding `BMH`s, as expected.

**Note**: ManagedCluster and node extra labels&annotations are the only fields that can be edited post installation. All the other fields are immutable and are rejected by the IMS operator. These changes would be rejected anyway by webhooks put in place by other operators for cluster installation resources (ex: `ClusterDeployment`)

### Updates to the policyTemplateParameters field under ProvisioningRequest spec.templateParameters

These types of changes can be made under the ProvisioningRequest `spec.templateParameters` by updating the `policyTemplateParameters` entry, if it's present.
```yaml
apiVersion: o2ims.provisioning.oran.org/v1alpha1
kind: ProvisioningRequest
metadata:
  finalizers:
    - provisioningrequest.o2ims.provisioning.oran.org/finalizer
  name: sno-ran-du-1
spec:
  description: Provisioning request for basic SNO with sample ACM policies
  name: Dev-main-SNO-Provisioning-sno-ran-du-1
  templateName: sno-ran-du
  templateVersion: v1
  templateParameters:
    nodeClusterName: sno-ran-du-1
    oCloudSiteId: local-west-12345
    policyTemplateParameters:
      sriov-network-vlan-1: "111"
      sriov-network-vlan-2: "222"
```
**Note:** Only policy configuration values exposed in the `policyTemplateParameters` property within the `spec.templateParameterSchema` field of the associated `ClusterTemplate` can be updated through the `ProvisioningRequest`.

Once the update is made, the `<cluster-name>-pg` ConfigMap in the `ztp-<cluster-template-namespace>` namespace gets updated with the new value. This ConfigMap is used by the ACM policies in their hub templates.
```yaml
$  oc get clustertemplate -A
NAMESPACE                 NAME                   AGE
sno-ran-du-v4-16   sno-ran-du.v1   3d23h

$  oc get cm -n ztp-sno-ran-du-v4-16 sno-ran-du-1-pg -oyaml
apiVersion: v1
data:
  cpu-isolated: 0-1,64-65
  cpu-reserved: 2-10
  hugepages-count: "32"
  hugepages-default: 1G
  hugepages-size: 1G
  install-plan-approval: Automatic
  sriov-network-vlan-1: "111"
  sriov-network-vlan-2: "222"
kind: ConfigMap
metadata:
  name: sno-ran-du-1-pg
  namespace: ztp-sno-ran-du-v4-16
```

Once a policy matched with a ManagedCluster deployed through a ProvisioningRequest becomes `NonCompliant`, it's reflected in the ProvisioningRequest `status.policies` and the time when it becomes `NonCompliant` is also recorded. The `ConfigurationApplied` condition reflects that the configuration is being applied.
```yaml
status:
  clusterDetails:
    clusterProvisionStartedAt: "2024-10-07T17:59:23Z"
    name: sno-ran-du-1
    nonCompliantAt: "2024-10-07T21:53:29Z"  <<< non compliance timestamp recorded here
    ztpStatus: ZTP Done
  conditions:
  - lastTransitionTime: "2024-10-07T21:53:29Z"
    message: The configuration is still being applied
    reason: InProgress
    status: "False"
    type: ConfigurationApplied
  policies:
  - compliant: Compliant
    policyName: v1-perf-configuration-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
  - compliant: NonCompliant <<< Policy is NonCompliant
    policyName: v1-sriov-configuration-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
  - compliant: Compliant
    policyName: v1-subscriptions-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
```

**Notes**:
* The format of the way the `nonCompliantAt` timestamps might move to another structure in the status, but it will still be recorded.
* Some changes happen so fast that the Policy doesn't even switch to `NonCompliant`, so the IMS operator cannot record the event. In this case, IMS still holds a correct recording since all the policies are/remain Compliant.
* Once an enforce `NonCompliant` Policy becomes `Compliant` again, the `status.policies` is updated, the `status.clusterDetails.nonCompliantAt` value removed and the `ConfigurationApplied` condition updated to show that the configuration is up to date:
* When refactored, the start and end times of the configuration being NonCompliant will be recorded.

Once all the policies become `Compliant`, the status is updated as follows:
```yaml
status:
  clusterDetails:
    clusterProvisionStartedAt: "2024-10-07T17:59:23Z"
    name: sno-ran-du-1
    ztpStatus: ZTP Done
    >>> no nonCompliantAt <<<
  conditions:
  - lastTransitionTime: "2024-10-07T22:15:32Z"
    message: The configuration is up to date
    reason: Completed
    status: "True"
    type: ConfigurationApplied
  policies:
  - compliant: Compliant
    policyName: v1-perf-configuration-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
  - compliant: Compliant
    policyName: v1-sriov-configuration-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
  - compliant: Compliant
    policyName: v1-subscriptions-policy
    policyNamespace: ztp-sno-ran-du-v4-16
    remediationAction: enforce
```

### Updates to an existing ACM PolicyGenerator manifest

For updating a manifest in an existing ACM PolicyGenerator, the following steps need to be taken (we'll take [sno-ran-du-pg-v4-16-v1](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v1.yaml) for examples):
1. Upversion the cluster template content:
    * Create a new version of the ACM PG - [sno-ran-du-pg-v4-16-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v2.yaml):
        * The name is updated to `sno-ran-du-pg-v4-16-v2` (the `ztp-sno-ran-du-v4-16` namespace is kept).
        * `policyDefaults.placement.labelSelector.sno-ran-du-policy` is updated from `v1` to `v2` such that the policy binding is updated.
        * All policy names are updated from `v1` to `v2` (example: `v1-subscriptions-policy` -> `v2-subscriptions-policy`).
        * The desired manifest section is updated. In the current samples [sno-ran-du-pg-v4-16-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v2.yaml) adds a `sysctl` section to the `TunedPerformancePatch` section under the `v2-tuned-configuration-policy` policy.
    * Create a new version of the [clusterinstance-defaults-v1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v1.yaml) - [clusterinstance-defaults-v2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v2.yaml):
        * Update the name to `clusterinstance-defaults-v2` (the namespace stays `sno-ran-du-v4-16`).
        * Update the `sno-ran-du-policy` ManagedCluster extraLabel from `v1` to `v2`.
    * Create a new version of the [sno-ran-du.v4-Y-Z-1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-1.yaml) `ClusterTemplate` CR - [sno-ran-du.v4-Y-Z-2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-2.yaml)
        * Update the `metadata.name` from `sno-ran-du.v4-Y-Z-1` to `sno-ran-du.v4-Y-Z-2`
        * Update `spec.version` from `v4-Y-Z-1` to `v4-Y-Z-2`
        * Update `spec.templates.clusterInstanceDefaults` to `clusterinstance-defaults-v2`
2. ArgoCD sync to the hub cluster:
    * Add the newly created files to their corresponding kustomization.yaml.
    * All the resources created from above are created on the hub cluster, including the `v2` policies and the new ClusterTemplate is validated.
    * The new policies are not yet applied to the cluster because the `ManagedCluster` still has the old `sno-ran-du-policy: "v1"` label
3. The SMO selects the new ClusterTemplate CR for the ProvisioningRequest:
    * `spec.templateName` remains `sno-ran-du`, `spec.templateVersion` is updated from `v4-Y-Z-1` to `v4-Y-Z-2`
4. The IMS operator detects the change
    * It updates the ClusterInstance with the updated `sno-ran-du-policy: "v2"` ManagedCluster label
    * the siteconfig operator applies the new label to the ManagedCluster
5. The ACM Policy Propagator detects the new binding:
    * The old policies created through the [sno-ran-du-pg-v4-16-v1](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v1.yaml) Policy Generator are no longer matched to the ManagedCluster.
    * The new policies created through the [sno-ran-du-pg-v4-16-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v2.yaml) Policy Generator are matched to the ManagedCluster.
    * The `ConfigurationApplied` condition is updated in the ProvisioningRequest to show that the configuration has changed and is being applied (the policies depend on each other, so some are in a `Pending` state until ACM confirms their compliance):
    ```yaml
    - lastTransitionTime: "2024-10-11T19:48:06Z"
      message: The configuration is still being applied
      reason: InProgress
      status: "False"
      type: ConfigurationApplied
    policies:
    - compliant: Pending
      policyName: v2-perf-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Pending
      policyName: v2-sriov-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Compliant
      policyName: v2-subscriptions-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Pending
      policyName: v2-tuned-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    ```
    * The affected CRs are updated on the ManagedCluster, not deleted and recreated.
6. The IMS operator updates the ProvisioningRequest once all the policies are `Compliant`
    ```yaml
    - lastTransitionTime: "2024-10-11T19:48:36Z"
      message: The configuration is up to date
      reason: Completed
      status: "True"
      type: ConfigurationApplied
    policies:
    - compliant: Compliant
      policyName: v2-tuned-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Compliant
      policyName: v2-perf-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Compliant
      policyName: v2-sriov-configuration-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    - compliant: Compliant
      policyName: v2-subscriptions-policy
      policyNamespace: ztp-clustertemplate-a-v4-16
      remediationAction: enforce
    ```
