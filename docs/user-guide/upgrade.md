<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Example upgrade from 4.Y.Z to 4.Y.Z+1

This guide describes how to perform an Image-Based Upgrade (IBU) of a spoke cluster
from one OCP version to a newer version using the O-Cloud Manager. IBU uses a seed
image containing a pre-installed OCP version to perform a fast, image-based upgrade with
automatic rollback on failure.

The upgrade is orchestrated through an ImageBasedGroupUpgrade (IBGU) CR, which is
created automatically by the O-Cloud Manager when a ProvisioningRequest is updated to
reference a ClusterTemplate with a higher OCP release version. The content of the IBGU
is defined by the user in the
[upgrade-defaults](../samples/git-setup/clustertemplates/version_4.Y.Z+1/sno-ran-du/upgrade-defaults-v1.yaml)
ConfigMap, which is referenced by the ClusterTemplate's `spec.templates.upgradeDefaults`
field.

## Prerequisites

- The OpenShift API for Data Protection (OADP) operator and the Lifecycle Agent (LCA)
  operator are installed on the spoke cluster, and the OADP backend is configured.
  See [Installing operators for IBU](https://docs.openshift.com/container-platform/latest/edge_computing/image_based_upgrade/preparing_for_image_based_upgrade/cnf-image-based-upgrade-install-operators.html).

- A compatible seed image for the target OCP version (`4.Y.Z+1`) has been created.
  Both the target cluster and the seed image must have a
  [shared container partition configured for IBU](https://docs.openshift.com/container-platform/latest/edge_computing/image_based_upgrade/preparing_for_image_based_upgrade/cnf-image-based-upgrade-shared-container-partition.html).

- The spoke cluster is successfully deployed and configured:

```console
$ oc get provisioningrequests.clcm.openshift.io
NAME           AGE   PROVISIONSTATE   PROVISIONDETAILS
cluster-name   37m   fulfilled        Provisioning request has completed successfully
```

- The OADP and LCA operators are deployed via ACM policies in the PolicyGenerator:

```yaml
# policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v1.yaml

policies:
- name: operator-subscriptions
  manifests:
    # LCA (Lifecycle Agent)
    - path: source-crs/LcaOperatorStatus.yaml
    - path: source-crs/LcaSubscriptionNS.yaml
    - path: source-crs/LcaSubscriptionOperGroup.yaml
    - path: source-crs/LcaSubscription.yaml
    # OADP (OpenShift API for Data Protection)
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

1. Create new [clustertemplates](../samples/git-setup/clustertemplates/version_4.Y.Z+1/)
   and [policytemplates](../samples/git-setup/policytemplates/version_4.Y.Z+1/) directories
   for the new release according to the sample git-setup, if they are not already created.
   File names, namespaces, and names should match the new release version.
2. Update the release version in new clustertemplates files. If there are changes to the
   `templateParameterSchema` in the new ClusterTemplate version, update
   `templateParameters` to match the new schema. See the example below.
3. Wait for the GitOps repo to be synced to the hub cluster.
4. Update the ProvisioningRequest to reference the new ClusterTemplate. This triggers the
   image-based upgrade.

```console
oc patch provisioningrequests.clcm.openshift.io <name> --type merge \
  -p '{"spec":{"templateName":"sno-ran-du","templateVersion":"v4-Y-Z+1-1"}}'
```

**Example ClusterTemplate and upgrade defaults for the new release:**

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
  # The upgrade step timeout needs to be 30 minutes longer than the auto rollback timeout
  # so that the abort/cleanup can be executed as part of ibgu
  ibgu: |
    ibuSpec:
      seedImageRef:
        image: "quay.io/seed-repo/seed-image:4.Y.Z+1"
        version: "4.Y.Z+1"
      oadpContent:
        - name: sno-ran-du-ibu-platform-backup-v4-Y-Z+1-1
          namespace: openshift-adp
      autoRollbackOnFailure:
        initMonitorTimeoutSeconds: 1800
    plan:
      - actions: ["Prep"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 15
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["Upgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 60
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["FinalizeUpgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
```

## Monitoring Upgrade Progress

Monitor the `UpgradeCompleted` condition on the ProvisioningRequest:

```console
oc get provisioningrequests.clcm.openshift.io <name> \
  -o jsonpath='{.status.conditions[?(@.type=="UpgradeCompleted")]}{"\n"}'
```

| Status | Reason | Meaning |
|---|---|---|
| False | InProgress | Upgrade is in progress |
| True | Completed | Upgrade completed successfully |
| False | Failed | Upgrade failed (see condition message for details) |

On successful completion, the ProvisioningRequest status returns to `fulfilled`:

```yaml
status:
  conditions:
    - message: Upgrade is completed
      reason: Completed
      status: "True"
      type: UpgradeCompleted
  provisioningStatus:
    provisioningDetails: Provisioning request has completed successfully
    provisioningPhase: fulfilled
```

## Retry After Failure

If the upgrade fails, the spoke cluster may be automatically rolled back depending on
how far the upgrade progressed. The IBGU plan includes `AbortOnFailure` actions to
handle rollback and cleanup. To retry after the rollback or abort completes:

1. Wait for the rollback to complete. The `UpgradeCompleted` condition will show
   `reason: Failed` with details in the message.

2. Revert the ProvisioningRequest to the original ClusterTemplate:

   ```console
   oc patch provisioningrequests.clcm.openshift.io <name> --type merge \
     -p '{"spec":{"templateName":"sno-ran-du","templateVersion":"v4-Y-Z-1"}}'
   ```

3. Once the ProvisioningRequest returns to `fulfilled`, update it again to the new
   ClusterTemplate to reattempt the upgrade:

   ```console
   oc patch provisioningrequests.clcm.openshift.io <name> --type merge \
     -p '{"spec":{"templateName":"sno-ran-du","templateVersion":"v4-Y-Z+1-1"}}'
   ```
