<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Cluster Upgrade

- [Cluster Upgrade](#cluster-upgrade)
  - [Overview](#overview)
  - [SNO Image-Based Upgrade (IBU)](#sno-image-based-upgrade-ibu)
    - [Prerequisites](#prerequisites)
    - [Steps](#steps)
    - [Example ClusterTemplate and upgrade defaults for the new release](#example-clustertemplate-and-upgrade-defaults-for-the-new-release)
    - [Overriding Upgrade Parameters per ProvisioningRequest](#overriding-upgrade-parameters-per-provisioningrequest)
    - [Monitoring Upgrade Progress](#monitoring-upgrade-progress)
    - [Retry After Failure](#retry-after-failure)
  - [MNO ClusterVersion Upgrade](#mno-clusterversion-upgrade)
    - [MNO Prerequisites](#mno-prerequisites)
    - [Preparing the Upgrade ClusterTemplate](#preparing-the-upgrade-clustertemplate)
    - [Triggering the Upgrade](#triggering-the-upgrade)
    - [Timeout Configuration](#timeout-configuration)
    - [How It Works](#how-it-works)
    - [MNO Monitoring Upgrade Progress](#mno-monitoring-upgrade-progress)
    - [MNO Retry After Failure](#mno-retry-after-failure)

## Overview

The O-Cloud Manager supports two cluster upgrade methods:

- **SNO Image-Based Upgrade** — upgrades Single-Node OpenShift clusters via ImageBasedGroupUpgrade (IBGU).
- **MNO ClusterVersion upgrade** — upgrades Multi-Node OpenShift clusters via the ClusterVersion CR on the spoke cluster.

The upgrade type is determined by the upgrade configuration in the ClusterTemplate's `upgradeDefaults` and
the ProvisioningRequest's `upgradeParameters`: `clusterVersion` for MNO or `imageBasedGroupUpgrade` for SNO.
Only one upgrade type can be configured per ClusterTemplate.

## SNO Image-Based Upgrade (IBU)

This section describes how to perform an Image-Based Upgrade (IBU) of a spoke cluster
from one OCP version to a newer version using the O-Cloud Manager. IBU uses a seed
image containing a pre-installed OCP version to perform a fast, image-based upgrade with
automatic rollback on failure.

The upgrade is orchestrated through an ImageBasedGroupUpgrade (IBGU) CR, which is
created automatically by the O-Cloud Manager when a ProvisioningRequest is updated to
reference a ClusterTemplate with a higher OCP release version. The default IBGU parameters are
defined inline in the ClusterTemplate's `spec.templateDefaults.upgradeDefaults` field.
Individual ProvisioningRequests can override specific upgrade parameters via
`spec.templateParameters.upgradeParameters`.

### Prerequisites

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

### Steps

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

### Example ClusterTemplate and upgrade defaults for the new release

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
  templateDefaults:
    clusterInstanceDefaults: clusterinstance-defaults-v1
    policyTemplateDefaults: policytemplate-defaults-v1
    # The upgrade step timeout needs to be 30 minutes longer than the auto
    # rollback timeout so that the abort/cleanup can be executed as part of IBGU.
    upgradeDefaults:
      imageBasedGroupUpgrade:
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
  templateParameterSchema:
    properties:
      # ... other parameters ...
      upgradeParameters:
        description: >
          upgradeParameters allows overriding upgrade defaults defined in
          templateDefaults.upgradeDefaults.
        properties:
          imageBasedGroupUpgrade:
            type: object
            properties:
              ibuSpec:
                type: object
              plan:
                type: array
        type: object
    required:
      - nodeClusterName
      - oCloudSiteId
      - policyTemplateParameters
      - clusterInstanceParameters
    type: object
```

### Overriding Upgrade Parameters per ProvisioningRequest

ProvisioningRequests can override specific upgrade defaults by providing
`upgradeParameters` in `spec.templateParameters`. The values are deep-merged on
top of the ClusterTemplate's `upgradeDefaults`, with ProvisioningRequest values
taking precedence.

Both `upgradeDefaults` and `upgradeParameters` must conform to the
`upgradeParameters` sub-schema defined in the ClusterTemplate's
`templateParameterSchema`.

Note that `plan` is an array and is merged by index position — element 0 of
the override merges with element 0 of the defaults, element 1 with element 1,
and so on. If the override array is longer, extra elements are appended.

#### Example: Override the seed image and increase the Upgrade timeout

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: ProvisioningRequest
metadata:
  name: cluster-name
spec:
  templateName: sno-ran-du
  templateVersion: v4-Y-Z+1-1
  templateParameters:
    nodeClusterName: cluster-name
    oCloudSiteId: site-1
    upgradeParameters:
      imageBasedGroupUpgrade:
        ibuSpec:
          seedImageRef:
            image: "quay.io/seed-repo/seed-image:4.Y.Z+1-v2"
        plan:
          - actions: ["Prep"]
            rolloutStrategy:
              timeout: 15
          - actions: ["AbortOnFailure"]
          - actions: ["Upgrade"]
            rolloutStrategy:
              timeout: 90
    # ... other parameters ...
```

In this example:

- The seed image is overridden to use a different image tag.
- The Prep step timeout is set to 15 minutes and the Upgrade step timeout is
  increased to 90 minutes.
- Fields not specified in `upgradeParameters` (such as `oadpContent`,
  `autoRollbackOnFailure`, and remaining plan steps) are inherited from the
  ClusterTemplate's `upgradeDefaults`.

### Monitoring Upgrade Progress

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
| False | PreconditionChecksFailed | Upgrade preconditions not met (schema validation, version mismatch) |

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

### Retry After Failure

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

## MNO ClusterVersion Upgrade

This section describes how to upgrade Multi-Node OpenShift (MNO) clusters
using the O-Cloud Manager. The upgrade is performed by patching the
ClusterVersion CR on the spoke cluster, where the Cluster Version Operator
(CVO) orchestrates the component-by-component update of the control plane
and worker nodes.

### MNO Prerequisites

- The [ManagedServiceAccount](https://github.com/open-cluster-management-io/managed-serviceaccount)
  addon is enabled on the hub cluster. This addon is enabled by default in
  ACM/MCE. To verify, check the MultiClusterEngine CR:

  ```console
  oc get multiclusterengines.multicluster.openshift.io multiclusterengine \
    -o jsonpath='{.spec.overrides.components[?(@.name=="managedserviceaccount")].enabled}'
  ```

  If it returns `false` or is not present, enable it using
  `oc edit` to add or update the entry while preserving existing
  component overrides:

  ```console
  oc edit multiclusterengines.multicluster.openshift.io multiclusterengine
  ```

  Ensure the `managedserviceaccount` component is listed and enabled:

  ```yaml
  spec:
    overrides:
      components:
      - name: managedserviceaccount
        enabled: true
      # ... other existing components ...
  ```

- Administrator pre-upgrade tasks are completed before triggering the upgrade.
  These are the administrator's responsibility and must be done before updating
  the ProvisioningRequest. See [Preparing to update a cluster](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/updating_clusters/preparing-to-update-a-cluster) for details including:
  - etcd backup
  - Reviewing deprecated APIs
  - Administrator acknowledgement for minor version upgrades
    This can be done by updating a ConfigMap on the spoke cluster via an ACM policy. Follow the pattern described in
    [Adding a new manifest to an existing ACM PolicyGenerator](./cluster-configuration.md#adding-a-new-manifest-to-an-existing-acm-policygenerator)
    to add the acknowledgement ConfigMap:

    ```yaml
    - name: v2-admin-acks-policy
      manifests:
        - path: source-crs/ConfigMapGeneric.yaml
          patches:
          - metadata:
              name: admin-acks
              namespace: openshift-config
            data:
              ack-4.18-kube-1.32-api-removals-in-4.19: "true"
    ```

    For EUS-to-EUS upgrades where both the intermediate and target versions require acknowledgement, include entries for both:

    ```yaml
            data:
              ack-4.18-kube-1.32-api-removals-in-4.19: "true"
              ack-4.19-admissionregistration-v1beta1-api-removals-in-4.20: "true"
    ```

  - The spoke cluster is successfully deployed via ProvisioningRequest.

### Preparing the Upgrade ClusterTemplate

Create a new ClusterTemplate version for the target OCP release. See the complete sample upgrade templates
for [std-ran-du](../samples/git-setup/clustertemplates/version_4.Y.Z+1/std-ran-du/)
and [3node-ran-du](../samples/git-setup/clustertemplates/version_4.Y.Z+1/3node-ran-du/).
This involves updating the sub-templates and creating the ClusterTemplate CR.

1. **Create a ClusterInstance defaults ConfigMap** in the target version namespace.
   Update `clusterImageSetNameRef` to the new OCP image version and update `extraLabels`
   to bind the cluster to the new version's policies:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: clusterinstance-defaults-v1
     namespace: std-ran-du-v4-22-0
   data:
     clusterinstance-defaults: |
       clusterImageSetNameRef: "4.22.0"
       extraLabels:
         ManagedCluster:
           cluster-version: "v4-22-0"
           std-ran-du-policy: "v1"
   ```

   > [!NOTE]
   > The SiteConfig operator only permits updates to a limited
   > subset of ClusterInstance fields after initial installation. The rest
   > of the configuration must remain identical to the original ConfigMap
   > used for provisioning. In particular, `extraManifestsRefs` must
   > reference the same ConfigMap names as the original version — for
   > example, if the original used `std-ran-extra-manifest-v1`, the new
   > ConfigMap must also use `std-ran-extra-manifest-v1`.

2. **Create policy templates** for the new OpenShift version. Create a new PolicyGenerator
   and policy template defaults ConfigMap in the target version namespace with the updated
   operator index image referencing the new release.

3. **Create the ClusterTemplate CR** referencing the new default configurations. Set `spec.release`
   to the target version, configure `upgradeDefaults` with the `clusterVersion` upgrade settings,
   and define `templateParameterSchema` to expose overridable upgrade fields. Hardware configuration
   parameters `hwMgmtDefaults` should remain the same as the original provisioning version.

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: ClusterTemplate
metadata:
  name: std-ran-du.v4-22-0-v1
  namespace: std-ran-du-v4-22-0
spec:
  name: std-ran-du
  version: v4-22-0-v1
  release: "4.22.0"
  templateDefaults:
    # ... other default configurations ...
    upgradeDefaults:
      # clusterUpgradeTimeout: "6h"            # optional; defaults to 4h (standard upgrades) or 8h (EUS upgrades)
      # intermediateVersion: "4.21.5"          # optional; for EUS upgrades (auto-selected if omitted)
      clusterVersion:
        channel: "stable-4.22"                 # required for minior version and EUS upgrades
        # upstream: "https://api.openshift.com/api/upgrades_info/v1/graph"  # optional
        desiredUpdate:
          version: "4.22.0"                    # must match spec.release 
  templateParameterSchema:
    type: object
    properties:
      # ... other parameters ...
      upgradeParameters:
        type: object
        properties:
          clusterUpgradeTimeout:
            type: string
          intermediateVersion:
            type: string
          clusterVersion:
            type: object
            properties:
              channel:
                type: string
              upstream:
                type: string
              desiredUpdate:
                type: object
                properties:
                  version:
                    type: string
                  image:
                    type: string
                  force:
                    type: boolean
                  architecture:
                    type: string
                    enum:
                    - Multi
                    - ""
```

**Notes:**

- `upgradeDefaults` must contain either `clusterVersion` or `imageBasedGroupUpgrade`, not both.
- `desiredUpdate.version` must match the ClusterTemplate's `spec.release`.
- An EUS-to-EUS upgrade is detected when both the current and target cluster versions are even-numbered minor releases exactly 2 minor
  versions apart (e.g., 4.20 to 4.22). The upgrade proceeds through an intermediate version (e.g., 4.21.x) before reaching the target.
  If `intermediateVersion` is specified, it is used directly and must be valid semver with the same major version and exactly one minor
  version below `desiredUpdate.version`. If `intermediateVersion` is not specified, the controller auto-selects the highest valid
  intermediate version from the configured `upstream` and `channel`, where the selected version has a valid upgrade path from the current
  version and to the target version. `channel` is required for auto-selection.

### Triggering the Upgrade

Update the ProvisioningRequest to reference the new ClusterTemplate to trigger the upgrade. The ProvisioningRequest can be updated via the
CLI or the [Provisioning REST API](./cluster-provisioning.md#provisioning-rest-apis).

ProvisioningRequests can also override specific upgrade defaults by providing `upgradeParameters` in `spec.templateParameters`. The values
are deep-merged on top of the ClusterTemplate's `upgradeDefaults`, with ProvisioningRequest values taking precedence:

```yaml
spec:
  templateParameters:
    upgradeParameters:
      clusterUpgradeTimeout: "6h"
      clusterVersion:
        channel: "eus-4.22"
        upstream: "https://example.com/graph"
```

### Timeout Configuration

The `clusterUpgradeTimeout` controls how long the controller waits for the upgrade to complete before marking it as `TimedOut`.

| Upgrade Type | Default Timeout |
|---|---|
| Standard (z-stream / y-stream) | 4 hours |
| EUS-to-EUS | 8 hours |

A single timeout covers the entire upgrade process, including both hops for EUS upgrades. The timeout is tracked from
`status.clusterDetails.clusterUpgradeStatus.startedAt`.

The timeout can be configured via `upgradeDefaults` or overridden per ProvisioningRequest via `upgradeParameters`.
`clusterUpgradeTimeout` is the only parameter update recognized during an in-progress upgrade. Other parameter changes are
ignored until the upgrade completes or fails.

### How It Works

The MNO ClusterVersion upgrade follows these steps:

1. **Create spoke client** — the controller creates a ManagedServiceAccount
   and RBAC ManifestWork for scoped access to the managed cluster's
   ClusterVersion and MachineConfigPool resources.

2. **Pre-upgrade checks** — for minor version upgrades, verifies the
   `Upgradeable` condition on the spoke ClusterVersion. For standard
   upgrades, requires all MachineConfigPools are unpaused. For EUS
   upgrades, requires all MachineConfigPools show `Updated=True`.

3. **Set channel and upstream** — patches the spoke ClusterVersion with
   the configured channel and upstream if provided, then waits for the CVO
   to retrieve the update graph (`RetrievedUpdates=True`).

4. **Verify target available** — for version-based upgrades (no
   `desiredUpdate.image`), confirms the target version is listed in the
   spoke ClusterVersion's `status.availableUpdates`.

5. **Trigger upgrade and monitor** — patches `spec.desiredUpdate` on
   the spoke ClusterVersion and monitors CVO progress via ClusterVersion
   history and conditions.

6. **For EUS** — resolves the intermediate version (from configuration
   or the Cincinnati update graph), pauses non-master worker MCPs,
   triggers the intermediate version upgrade first, then the target
   version upgrade, and finally unpauses MCPs and waits for workers to
   update.

### MNO Monitoring Upgrade Progress

Monitor the `UpgradeCompleted` condition on the ProvisioningRequest:

```console
oc get provisioningrequests.clcm.openshift.io <name> \
  -o jsonpath='{.status.conditions[?(@.type=="UpgradeCompleted")]}'
```

| Status | Reason | Meaning | Common Causes |
|---|---|---|---|
| False | Pending | Upgrade not yet started or waiting | Spoke client setup in progress, waiting for CVO to retrieve update graph or image payload, `Upgradeable=False` on spoke (minor version upgrade) |
| False | InProgress | Upgrade is running | CVO updating control plane and worker nodes |
| False | Unknown | Upgrade stalled | CVO not progressing, possible `Failing` condition on spoke |
| False | PreconditionChecksFailed | Cannot proceed with upgrade | MCPs not unpaused (standard) or not updated (EUS), spoke client setup issue, invalid upgrade configuration |
| True | Completed | Upgrade finished successfully | — |
| False | TimedOut | Upgrade exceeded the configured timeout | Timeout too short for cluster size, spoke connectivity issues, upgrade stalled |

The condition message provides details about the current phase:

- Spoke client setup: `"Preparing upgrade resources"`
- Upgrade triggered, waiting for CVO to begin: `"Upgrade to [intermediate/target] version X.Y.Z triggered. Waiting for upgrade to start"`
- Upgrade is in progress with CVO Progressing details: `"Upgrading to [intermediate/target] version X.Y.Z: ..."`
- EUS: MCPs unpaused, workers updating: `"Cluster version upgrade completed. Waiting for worker MachineConfigPools to finish updating"`
- Upgrade completed: `"Upgrade to version X.Y.Z completed"`
- Timeout with optional CVO Failing details: `"Upgrade timed out"` or `"Upgrade timed out: ..."`

The `ClusterUpgradeStatus` in the ProvisioningRequest status provides
additional upgrade state:

```yaml
status:
  extensions:
    clusterDetails:
      clusterUpgradeStatus:
        startedAt: "2026-07-20T15:30:00Z"
        startVersion: "4.18.25"
        intermediateVersion: "4.19.31"  # present for EUS upgrades
```

The upgrade status is also reflected in the ProvisioningRequest's `provisioningPhase` and `provisioningDetails`. The message from the
`UpgradeCompleted` condition is propagated to `provisioningDetails`:

```console
oc get provisioningrequests.clcm.openshift.io
```

| UpgradeCompleted Reason | provisioningPhase |
|---|---|
| Pending, InProgress, Unknown | progressing |
| PreconditionChecksFailed, TimedOut | failed |
| Completed | fulfilled |

### MNO Retry After Failure

**Pending (requires attention):** Some `Pending` states indicate issues that may require
investigation. The controller continues to requeue in ase the issue is temporaray
(e.g., network connectivity), but if the condition persists, user action may be needed:

  The `UpgradeCompleted` condition message includes the relevant spoke
  ClusterVersion condition details (e.g., `Upgradeable`, `RetrievedUpdates`,
  `ReleaseAccepted`). Check the message to determine the root cause —
  for example, it may indicate a required administrator acknowledgement,
  a degraded cluster operator, a channel/upstream misconfiguration, or
  a payload verification failure.

The controller will automatically proceed once the issue is resolved.

**PreconditionChecksFailed:** The upgrade is stopped. Check the
`UpgradeCompleted` condition message for details, fix the underlying
cause, and update the ProvisioningRequest to retry.

**TimedOut:** The upgrade exceeded its timeout. The administrator should
investigate the spoke cluster to determine the root cause and resolve it.

- If the cluster is still on the original version and ready to retry:
  revert the ProvisioningRequest to the original ClusterTemplate, wait
  for `fulfilled`, then update it again to the upgrade ClusterTemplate
  to reattempt the upgrade.

- If the administrator managed to complete the upgrade to the target
  version outside of the O-Cloud Manager: add or update a ManagedCluster
  label through the ProvisioningRequest's `clusterInstanceParameters` to
  trigger reconciliation, and the ProvisioningRequest will detect the
  new version and transition to `fulfilled`.
