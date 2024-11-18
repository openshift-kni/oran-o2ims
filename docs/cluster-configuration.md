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

**Note**: ManagedCluster and node extra labels&annotations are the only fields that can be edited post installation. All the other fields are immutable and are rejected by the O-Cloud Manager. These changes would be rejected anyway by webhooks put in place by other operators for cluster installation resources (ex: `ClusterDeployment`)

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
      sriov-network-pfNames-1: '["ens4f1"]'
```
**Note:** Only policy configuration values exposed in the `policyTemplateParameters` property within the `spec.templateParameterSchema` field of the associated `ClusterTemplate` can be updated through the `ProvisioningRequest`.

Once the update is made, the `<cluster-name>-pg` ConfigMap in the `ztp-<cluster-template-namespace>` namespace gets updated with the new value. This ConfigMap is used by the ACM policies in their hub templates.
```console
$  oc get clustertemplate -A
NAMESPACE                 NAME                     AGE
sno-ran-du-v4-Y-Z         sno-ran-du.v4-Y-Z-1      3d23h

$  oc get cm -n ztp-sno-ran-du-v4-Y-Z <cluster name>-pg -oyaml
apiVersion: v1
data:
  cpu-isolated: 0-1,64-65
  cpu-reserved: 2-10
  hugepages-count: "32"
  hugepages-default: 1G
  hugepages-size: 1G
  install-plan-approval: Automatic
  sriov-network-vlan-1: "111"
  sriov-network-pfNames-1: '["ens4f1"]'
kind: ConfigMap
metadata:
  name: sno-ran-du-1-pg
  namespace: ztp-sno-ran-du-v4-Y-Z
```

Once a policy matched with a ManagedCluster deployed through a ProvisioningRequest becomes `NonCompliant`, it's reflected in the ProvisioningRequest `status.extensions.policies` and the time when it becomes `NonCompliant` is also recorded. The `ConfigurationApplied` condition reflects that the configuration is being applied.
```yaml
status:
  extensions:
    clusterDetails:
      clusterProvisionStartedAt: "2024-10-07T17:59:23Z"
      name: sno-ran-du-1
      nonCompliantAt: "2024-10-07T21:53:29Z"  <<< non compliance timestamp recorded here
      ztpStatus: ZTP Done
    policies:
    - compliant: Compliant
      policyName: v1-perf-configuration-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
    - compliant: NonCompliant <<< Policy is NonCompliant
      policyName: v1-sriov-configuration-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
    - compliant: Compliant
      policyName: v1-subscriptions-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
  conditions:
  - lastTransitionTime: "2024-10-07T21:53:29Z"
    message: The configuration is still being applied
    reason: InProgress
    status: "False"
    type: ConfigurationApplied
```

**Notes**:
* The format of the `nonCompliantAt` timestamps might move to another structure in the status, but it will still be recorded.
* Some changes happen so fast that the Policy doesn't even switch to `NonCompliant`, so the O-Cloud Manager cannot record the event. In this case, the O-Cloud Manager still holds a correct recording since all the policies are/remain Compliant.
* Once an enforce `NonCompliant` Policy becomes `Compliant` again, the `status.extensions.policies` is updated, the `status.extensions.clusterDetails.nonCompliantAt` value removed and the `ConfigurationApplied` condition updated to show that the configuration is up to date:
* When refactored, the start and end times of the configuration being NonCompliant will be recorded.

Once all the policies become `Compliant`, the status is updated as follows:
```yaml
status:
  extensions:
    clusterDetails:
      clusterProvisionStartedAt: "2024-10-07T17:59:23Z"
      name: sno-ran-du-1
      ztpStatus: ZTP Done
      >>> no nonCompliantAt <<<
    policies:
    - compliant: Compliant
      policyName: v1-perf-configuration-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
    - compliant: Compliant
      policyName: v1-sriov-configuration-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
    - compliant: Compliant
      policyName: v1-subscriptions-policy
      policyNamespace: ztp-sno-ran-du-v4-Y-Z
      remediationAction: enforce
  conditions:
  - lastTransitionTime: "2024-10-07T22:15:32Z"
    message: The configuration is up to date
    reason: Completed
    status: "True"
    type: ConfigurationApplied
```
### Updates to the ClusterInstance defaults ConfigMap
We assume a ManagedCluster has been installed through a `ProvisioningRequest` referencing the [sno-ran-du.v4-Y-Z-1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-1.yaml) `ClusterTemplate` CR.

In this example we are adding a new annotation to the `ManagedCluster` through the [clusterinstance-defaults-v1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v1.yaml) `ConfigMap` holding default values for the corresponding `ClusterInstance`.
The following steps need to be taken:
1. Upversion the cluster template:
    * Create a new version of the [clusterinstance-defaults-v1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v1.yaml) `ConfigMap` - [clusterinstance-defaults-v2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v2.yaml):
        * Update the name to `clusterinstance-defaults-v2` (the namespace stays `sno-ran-du-v4-Y-Z`).
        * Update `data.clusterinstance-defaults.extraAnnotations` with the desired new annotation.
    * Create a new version of the [sno-ran-du.v4-Y-Z-1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-1.yaml) `ClusterTemplate` CR - [sno-ran-du.v4-Y-Z-2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-2.yaml)
        * Update the `metadata.name` from `sno-ran-du.v4-Y-Z-1` to `sno-ran-du.v4-Y-Z-2`
        * Update `spec.version` from `v4-Y-Z-1` to `v4-Y-Z-2`
        * Update `spec.templates.clusterInstanceDefaults` to `clusterinstance-defaults-v2`
2. ArgoCD sync to the hub cluster:
    * Add the newly created files to their corresponding kustomization.yaml.
    * All the resources from above are created on the hub cluster.
3. The SMO selects the new `ClusterTemplate` CR for the `ProvisioningRequest`:
    * `spec.templateName` remains `sno-ran-du`, `spec.templateVersion` is updated from `v4-Y-Z-1` to `v4-Y-Z-2`
4. The O-Cloud Manager detects the change:
    * It updates the `ClusterInstance` with the new annotation.
5. The siteconfig operator detects the change to the `ClusterInstance` CR:
    * The new annotation is added to the `ManagedCluster`.
    * Any issues are reported in the `ProvisioningRequest`, under `status.conditions`.
    * **Note:** Some installation manifests cannot be updated after provisioning as the underlying operators have webhooks to prevent such updates.

### Updates to an existing ACM PolicyGenerator manifest

For updating a manifest in an existing ACM PolicyGenerator, the following steps need to be taken (we'll take [sno-ran-du-pg-v4-Y-Z-v1](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v1.yaml) as an example):
1. Upversion the cluster template content:
    * Create a new version of the ACM PG - [sno-ran-du-pg-v4-Y-Z-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v2.yaml):
        * The name is updated to `sno-ran-du-pg-v4-Y-Z-v2` (the `ztp-sno-ran-du-v4-Y-Z` namespace is kept).
        * `policyDefaults.placement.labelSelector.sno-ran-du-policy` is updated from `v1` to `v2` such that the policy binding is updated.
        * All policy names are updated from `v1` to `v2` (example: `v1-subscriptions-policy` -> `v2-subscriptions-policy`).
        * The desired manifest section is updated. The current [sno-ran-du-pg-v4-Y-Z-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v2.yaml) sample adds a `sysctl` section to the `TunedPerformancePatch` section under the `v2-tuned-configuration-policy` policy.
    * Create a new version of the [clusterinstance-defaults-v2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v2.yaml) `ConfigMap` - [clusterinstance-defaults-v3](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v3.yaml):
        * Update the name to `clusterinstance-defaults-v3` (the namespace stays `sno-ran-du-v4-Y-Z`).
        * Update the `sno-ran-du-policy` ManagedCluster `extraLabel` from `v1` to `v2`.
    * Create a new version of the [sno-ran-du.v4-Y-Z-2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-2.yaml) `ClusterTemplate` CR - [sno-ran-du.v4-Y-Z-3](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-3.yaml)
        * Update the `metadata.name` from `sno-ran-du.v4-Y-Z-2` to `sno-ran-du.v4-Y-Z-3`.
        * Update `spec.version` from `v4-Y-Z-2` to `v4-Y-Z-3`.
        * Update `spec.templates.clusterInstanceDefaults` to `clusterinstance-defaults-v3`.
2. ArgoCD sync to the hub cluster:
    * Add the newly created files to their corresponding kustomization.yaml.
    * All the resources created from above are created on the hub cluster, including the `v2` policies and the new ClusterTemplate is validated.
    * The new policies are not yet applied to the cluster because the `ManagedCluster` still has the old `sno-ran-du-policy: "v1"` label.
3. The SMO selects the new ClusterTemplate CR for the ProvisioningRequest:
    * `spec.templateName` remains `sno-ran-du`, `spec.templateVersion` is updated from `v4-Y-Z-2` to `v4-Y-Z-3`
4. The O-Cloud Manager detects the change:
    * It updates the ClusterInstance with the new `sno-ran-du-policy: "v2"` ManagedCluster label.
    * The siteconfig operator applies the new label to the ManagedCluster.
5. The ACM Policy Propagator detects the new binding:
    * The old policies created through the [sno-ran-du-pg-v4-Y-Z-v1](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v1.yaml) Policy Generator are no longer matched to the ManagedCluster.
    * The new policies created through the [sno-ran-du-pg-v4-Y-Z-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v2.yaml) Policy Generator are matched to the ManagedCluster.
    * The `ConfigurationApplied` condition is updated in the ProvisioningRequest to show that the configuration has changed and is being applied (the policies depend on each other, so some are in a `Pending` state until ACM confirms their compliance):
    ```yaml
    status:
      extensions:
        ...
        policies:
        - compliant: Pending
          policyName: v2-perf-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Pending
          policyName: v2-sriov-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Compliant
          policyName: v2-subscriptions-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Pending
          policyName: v2-tuned-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
      conditions:
        ...
        - lastTransitionTime: "2024-10-11T19:48:06Z"
          message: The configuration is still being applied
          reason: InProgress
          status: "False"
          type: ConfigurationApplied
    ```
    * The affected CRs are updated on the ManagedCluster, not deleted and recreated.
6. The O-Cloud Manager updates the ProvisioningRequest once all the policies are `Compliant`
    ```yaml
    status:
      extensions:
        ...
        policies:
        - compliant: Compliant
          policyName: v2-tuned-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Compliant
          policyName: v2-perf-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Compliant
          policyName: v2-sriov-configuration-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
        - compliant: Compliant
          policyName: v2-subscriptions-policy
          policyNamespace: ztp-sno-ran-du-v4-Y-Z
          remediationAction: enforce
      conditions:
      ...
      - lastTransitionTime: "2024-10-11T19:48:36Z"
        message: The configuration is up to date
        reason: Completed
        status: "True"
        type: ConfigurationApplied
    ```

### Adding a new manifest to an existing ACM PolicyGenerator

This usecase is identical to the [previous one](#updates-to-an-existing-acm-policygenerator-manifest), with the following distinctions:
* If the new manifest does not have a corresponding `source-cr` file, the CSP should add a new yaml file to the `custom-crs` directory.

**Directory structure example:**
```
policytemplates
|
└──version_4.Y.Z
|  | sno-ran-du
|  | source-crs
|  | custom-crs
|  | kustomization.yaml
|
└─── kustomization.yaml
```
* Depending on the dependencies, the new policy can be added to an existing policy as a new manifest or as a new policy.

**Adding manifests to an existing policy - adding the LCA operator:**
```yaml
policies:
- name: v1-subscriptions-policy
  manifests:
    - path: source-crs/DefaultCatsrc.yaml
      patches:
      - metadata:
          name: redhat-operators
        spec:
          displayName: redhat-operators
          image: registry.redhat.io/redhat/redhat-operator-index:v4.16
    # Everything below would be added for installing the LCA operator:
    - path: source-crs/LcaSubscriptionNS.yaml
    - path: source-crs/LcaSubscriptionOperGroup.yaml
    - path: source-crs/LcaSubscription.yaml
      patches:
      - spec:
          source: redhat-operators
          installPlanApproval:
            '{{hub $configMap:=(lookup "v1" "ConfigMap" "" (printf "%s-pg" .ManagedClusterName)) hub}}{{hub dig "data" "install-plan-approval" "Manual" $configMap hub}}'
    - path: source-crs/LcaSubscriptionOperGroup.yaml
```

**Adding manifests to a new policy - adding the LCA operator:**
```yaml
policies:
# Everything below would be added for installing the LCA operator:
- name: v1-lca-operator-policy
  manifests:
    - path: source-crs/LcaSubscriptionNS.yaml
    - path: source-crs/LcaSubscriptionOperGroup.yaml
    - path: source-crs/LcaSubscription.yaml
      patches:
      - spec:
          source: redhat-operators
          installPlanApproval:
            '{{hub $configMap:=(lookup "v1" "ConfigMap" "" (printf "%s-pg" .ManagedClusterName)) hub}}{{hub dig "data" "install-plan-approval" "Manual" $configMap hub}}'
    - path: source-crs/LcaSubscriptionOperGroup.yaml
```

### Updating the ClusterTemplate schemas

We assume a ManagedCluster has been installed through a `ProvisioningRequest` referencing the [sno-ran-du.v4-Y-Z-3](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-3.yaml) `ClusterTemplate` CR.

In this example we are updating the policy template schema - `spec.templateParameterSchema.policyTemplateParameters`. This update means that the ACM PG requires extra configuration values.
We assume we are starting from the [sno-ran-du-pg-v4-Y-Z-v2](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v2.yaml) ACM PG, but want to add configuration for one more SRIOV network, so 2 extra manifests (`SriovNetwork` and `SriovNetworkNodePolicy`) are needed.

The following steps need to be taken:
1. Upversion the cluster template content:
    * A new ACM PG is created - [sno-ran-du-pg-v4-Y-Z-v3](samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v3.yaml):
        * `metadata.name` is updated from `sno-ran-du-pg-v4-Y-Z-v2` to `sno-ran-du-pg-v4-Y-Z-v3` (the `ztp-sno-ran-du-v4-Y-Z` namespace is kept).
        * `policyDefaults.placement.labelSelector.sno-ran-du-policy` is updated from `v2` to `v3` such that the policy binding is updated.
        * All policy names are updated from `v2` to `v3` (example: `v2-subscriptions-policy` -> `v3-subscriptions-policy`).
        * The following manifests are added under the `v3-subscriptions-policy`:
        ```yaml
        - path: source-crs/SriovNetwork.yaml
          patches:
          - metadata:
              name: sriov-nw-du-mh
            spec:
              resourceName: du_mh
              vlan: '{{hub fromConfigMap "" (printf "%s-pg" .ManagedClusterName) "sriov-network-vlan-2" | toInt hub}}'
        - path: source-crs/SriovNetworkNodePolicy-SetSelector.yaml
          patches:
          - metadata:
              name: sriov-nnp-du-mh
            spec:
              deviceType: vfio-pci
              isRdma: false
              nicSelector:
                pfNames: '{{hub fromConfigMap "" (printf "%s-pg" .ManagedClusterName) "sriov-network-pfNames-2" | toLiteral hub}}'
              nodeSelector:
                node-role.kubernetes.io/master: ""
              numVfs: 8
              priority: 10
              resourceName: du_mh
        ```

    * A new version of the [policytemplate-defaults-v1](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/policytemplates-defaults-v1.yaml) ConfigMap is created - [policytemplate-defaults-v2](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/policytemplates-defaults-v2.yaml):
        * `metadata.name` is updated from `policytemplate-defaults-v1` to `policytemplate-defaults-v2`.
        * update the defaults to reflect the new schema and thus the needed configuration values, in our case: `sriov-network-vlan-2` and `sriov-network-pfNames-2`.
    
    * Create a new version of the [clusterinstance-defaults-v3](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v3.yaml) `ConfigMap` - [clusterinstance-defaults-v4](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-v4.yaml):
        * Update the name to `clusterinstance-defaults-v4` (the namespace stays `sno-ran-du-v4-Y-Z`).
        * Update the `sno-ran-du-policy` ManagedCluster `extraLabel` from `v2` to `v3`.

    * Create a new version of the [sno-ran-du.v4-Y-Z-3](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-3.yaml) `ClusterTemplate` CR - [sno-ran-du.v4-Y-Z-4](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-4.yaml)
        * Update the `metadata.name` from `sno-ran-du.v4-Y-Z-3` to `sno-ran-du.v4-Y-Z-4`.
        * Update `spec.version` from `v4-Y-Z-3` to `v4-Y-Z-4`.
        * Update `spec.templates.clusterInstanceDefaults` to `clusterinstance-defaults-v4`.
        * Update `spec.templates.policyTemplateDefaults` to `policytemplate-defaults-v2`.
        * Update `spec.templateParameterSchema.properties.policyTemplateParameters` to include the newly desired configuration options:
        ```yaml
        ...
        sriov-network-vlan-2:
          type: string
        sriov-network-pfNames-2:
          type: string
        ...
        ```

The remaining steps are similar to those from the [Updates to an existing ACM PolicyGenerator manifest](#updates-to-an-existing-acm-policygenerator-manifest) section, starting with step 2.

The only distinction is that for the current usecase, the `<cluster-name>-pg` ConfigMap in the `ztp-<cluster-template-namespace>` will be updated by the O-Cloud Manager to include the new values (`sriov-network-vlan-2` and `sriov-network-pfNames-2`):
```console
$  oc get cm -n ztp-sno-ran-du-v4-Y-Z <cluster name>-pg -oyaml
apiVersion: v1
data:
  cpu-isolated: 0-1,64-65
  cpu-reserved: 2-10
  hugepages-count: "32"
  hugepages-default: 1G
  hugepages-size: 1G
  install-plan-approval: Automatic
  sriov-network-pfNames-1: '["ens4f1"]'
  sriov-network-pfNames-2: '["ens4f2"]'
  sriov-network-vlan-1: "111"
  sriov-network-vlan-2: "222"
kind: ConfigMap
metadata:
  name: sno-ran-du-1-pg
  namespace: ztp-sno-ran-du-v4-Y-Z
```

**Note:** The steps are similar for updating the `spec.templateParameterSchema.properties.clusterInstanceParameters`. Any change to the `clusterInstanceParameters` must match the `ClusterInstance` CR of the siteconfig operator.


### Switching to a new hardware profile

We assume a ManagedCluster has been installed through a `ProvisioningRequest` referencing the [sno-ran-du.v4-Y-Z-4](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-4.yaml) `ClusterTemplate` CR.

In this example we are updating the `hwProfile` under `data.node-pools-data.hwProfile` in the [placeholder-du-template-configmap-v1](samples/git-setup/clustertemplates/hardwaretemplates/sno-ran-du/placeholder-du-template-configmap-v1.yaml) hardware template ConfigMap.

The following steps are required:

1. Upversion the [sno-ran-du.v4-Y-Z-4](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-4.yaml) ClusterTemplate:
    * Create a new version of the [placeholder-du-template-configmap-v1](samples/git-setup/clustertemplates/hardwaretemplates/sno-ran-du/placeholder-du-template-configmap-v1.yaml) hardware template ConfigMap - [placeholder-du-template-configmap-v2](samples/git-setup/clustertemplates/hardwaretemplates/sno-ran-du/placeholder-du-template-configmap-v2.yaml)
        * The content is updated to point to new `hwProfile(s)`. For our example we are updating `data.node-pools-data.hwProfile` from `profile-proliant-gen11-dual-processor-256G-v1` to `profile-proliant-gen11-dual-processor-256G-v2`.
    * Create a new version of the [sno-ran-du.v4-Y-Z-4](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-4.yaml) `ClusterTemplate` - [sno-ran-du.v4-Y-Z-5](samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-v4-Y-Z-5.yaml).
        * update the name from `sno-ran-du.v4-Y-Z-4` to `sno-ran-du.v4-Y-Z-5` (the namespace remains `sno-ran-du-v4-Y-Z`).
        * update `spec.version` from `v4-Y-Z-4` to `v4-Y-Z-5`.
        * update `spec.templates.hwTemplate` from `placeholder-du-template-configmap-v1` to `placeholder-du-template-configmap-v2`.
2. Update the kustomization files to include the new resources. ArgoCD will automatically sync them to the hub cluster.
    * The O-Cloud manager validates the new `ClusterTemplate`, but no other action is taken since the `ProvisioningRequest` has not been updated.
3. The SMO selects the new `ClusterTemplate` version in the `ProvisioningRequest`.
    * `spec.templateVersion` is updated from `v4-Y-Z-4` to `v4-Y-Z-5`, the one pointing to the new `hwProfile`.
4. The O-Cloud manager:
    * Updates the status of the `ProvisioningRequest`:
    ```yaml
        - lastTransitionTime: "2024-11-06T16:55:30Z"
          message: Hardware configuring is in progress
          reason: ConfigurationUpdateRequested
          status: "False"
          type: HardwareConfigured
        ...
        provisioningStatus:
          provisionedResources:
            oCloudNodeClusterId: 95f4a2cf-04dc-42d5-9d1e-f6cbc693d8ea
          provisioningDetails: Hardware configuring is in progress
          provisioningState: progressing
    ```
    * Updates the desired hardware profile in the `nodePools` CR for that cluster and the status condition to reflect the configuration change.
    ```yaml
    spec:
      ...
        - hwProfile: profile-proliant-gen11-dual-processor-256G-v2
          interfaces:
      ...
    status:
      conditions:
      - lastTransitionTime: "2024-10-20T01:22:19Z"
        message: Created
        reason: Completed
        status: "True"
        type: Provisioned
      - lastTransitionTime: "2024-11-06T16:55:30Z"
        message: Spec updated; awaiting configuration application by the hardware plugin
        reason: ConfigurationUpdateRequested
        status: "False"
        type: Configured
    ```
    * Obtains the list of nodes from the `NodePool` CR for the master MCP.
    * For the SNO case that we are considering, there is only one node that cannot be cordoned and drained.
    * Updates `spec.hwProfile` in the `Node` (`node.o2ims-hardwaremanagement.oran.openshift.io/v1alpha1`) CR.

5. The hardware plugin requests the hardware manager to apply the new hardware profile from the `Node` `spec`.
6. The hardware manager updates the profile.
7. The hardware plugin waits for the result from the hardware manager.
    * Success scenario:
        * The hardware plugin updates the status of the `Node` CR.
    * Failure scenario:
        * The operation is aborted.
        * The status of the `Node` CR is updated with the failure reason.
        * The O-Cloud manager does not initiate a rollback of any nodes already updates. This is left to the user to remediate.
8. Once all nodes have been updated, the hardware plugin will update the status of the `NodePool` CR `Configured` condition to reflect the result of the operation:
```yaml
    - lastTransitionTime: "2024-10-20T01:22:19Z"
      message: Configuration has been applied successfully
      reason: ConfigApplied
      status: "True"
      type: Configured
```
9. The O-Cloud manager will update the `ProvisioningRequest` status to reflect the result of the operation, based on the status update of the `NodePool` CR:
```yaml
    - lastTransitionTime: "2024-11-06T17:57:31Z"
      message: Configuration has been applied successfully
      reason: ConfigApplied
      status: "True"
      type: HardwareConfigured
    ...
    provisioningStatus:
      provisionedResources:
        oCloudNodeClusterId: 95f4a2cf-04dc-42d5-9d1e-f6cbc693d8ea
      provisioningDetails: Provisioning request has completed successfully
      provisioningState: fulfilled
```
