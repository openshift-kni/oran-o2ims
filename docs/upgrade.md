<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Example upgrade from 4.Y.Z to 4.Y.Z+1

## Starting conditions

- A compatible seed image with target cluster is created. Seed image has `4.Y.Z+1` OCP version. [Both target cluster and seed image has configured a shared container partition for IBU.](https://docs.openshift.com/container-platform/latest/edge_computing/image_based_upgrade/preparing_for_image_based_upgrade/cnf-image-based-upgrade-shared-container-partition.html)
- Spoke cluster is successfully deployed and configured using a `ProvisioningRequest`:

```bash
$ oc get provisioningrequests.o2ims.provisioning.oran.org        
NAME           AGE   PROVISIONSTATE   PROVISIONDETAILS
cluster-name   37m   fulfilled        Provisioning request has completed successfully
```

- [OADP and LCA is installed on the spoke cluster. OADP backend is configured on the spoke cluster.](https://docs.openshift.com/container-platform/latest/edge_computing/image_based_upgrade/preparing_for_image_based_upgrade/cnf-image-based-upgrade-install-operators.html)

```yaml
# policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v1.yaml

policies:
- name: operator-subscriptions
  manifests:
    # LCA
    - path: source-crs/LcaOperatorStatus.yaml
    - path: source-crs/LcaSubscriptionNS.yaml
    - path: source-crs/LcaSubscriptionOperGroup.yaml
    - path: source-crs/LcaSubscription.yaml
    # OADP
    - path: source-crs/OadpOperatorStatus.yaml
    - path: source-crs/OadpSubscriptionNS.yaml
    - path: source-crs/OadpSubscriptionOperGroup.yaml
    - path: source-crs/OadpSubscription.yaml
- name: oadp-backend
  manifests:
    - path: source-crs/OadpSecret.yaml
      patches:
        - data: <FILL>
    - path: source-crs/OadpDataProtectionApplication.yaml
      patches:
        - spec:
            backupLocations:
            <FILL>
```

## Steps

- Create new [clustertemplates](samples/git-setup/clustertemplates/version_4.Y.Z+1/) and [policytemplates](samples/git-setup/policytemplates/version_4.Y.Z+1/) directories for the new release according to sample git-setup,
if they are not already created. File names, namespaces and names should match the new release version.
- Update release version in new clustertemplates files.
- If there are changes to the templateParameterSchema in the new ClusterTemplate version, update templateParameters to match the new schema.

```yaml
# clustertemplates/version_4.Y.Z+1/sno-ran-du/clusterinstance-defaults-v1.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: clusterinstance-defaults-v1
  namespace: sno-ran-du-v4-Y-Z+1
data:
  clusterinstance-defaults: |
    clusterImageSetNameRef: "4.Y.Z+1"
---
# clustertemplates/version_4.Y.Z+1/sno-ran-du/sno-ran-du-v4-Y-Z+1-1.yaml
kind: ClusterTemplate
metadata:
  name: sno-ran-du.v4-Y-Z+1-1
  namespace: sno-ran-du-v4-Y-Z+1
spec:
  name: sno-ran-du
  version: v4-Y-Z+1-1
  release: 4.Y.Z+1
  templates:
    upgradeDefaults: upgrade-defaults-v1
---
# clustertemplates/version_4.Y.Z+1/sno-ran-du/upgrade-defaults-v1.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: upgrade-defaults-v1
  namespace: sno-ran-du-v4-Y-Z+1
data:
  ibgu: |
    ibuSpec:
      seedImageRef:
        image: "quay.io/seed-repo/seed-image:4.Y.Z+1"
        version: "4.Y.Z+1"
      oadpContent:
        - name: sno-ran-du-ibu-platform-backup-v4-Y-Z+1-1
          namespace: openshift-adp
```

- Wait for ztp repo to be synced to the hub cluster.
- Patch `ProvisioningRequest.spec.templateName` and `ProvisioningRequest.spec.templateVersion` to point to the new `ClusterTemplate`. This will trigger the image based upgrade.
- Wait for upgrade to be completed.

```yaml
  status:                     
    conditions: 
      message: Upgrade is completed                
      reason: Completed
      status: "True"      
      type: UpgradeCompleted    
  provisioningStatus:
      provisionedResources:
      provisioningDetails: Provisioning request has completed successfully
      provisioningState: fulfilled

```

- To retry the upgrade after a upgrade failure, wait for rollback or abort to be completed, change the template version and name to the previous values, and then change them back again to the new values.
