<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Cluster Provisioning with ProvisioningRequest CR and Image-Based Install (IBI)

## Overview

This document provides a walkthrough for provisioning single-node OpenShift (SNO) clusters using the ProvisioningRequest Custom Resource (CR) and the default Image-Based Install (IBI) templates provided by the SiteConfig Operator.

## Prerequisites

### Management Cluster Requirements

* OpenShift cluster with Advanced Cluster Management (ACM) 2.14 or later
* Red Hat OpenShift GitOps Operator
* SiteConfig Operator enabled
* Image-Based Install Operator enabled
* ORAN O2IMS controllers installed and running

### Git Repository Setup

Prepare a Git repository with all required provisioning and configuration files, and sync it with ArgoCD. Follow the recommended [directory structure](../samples/git-setup/).

## Image-Based Installation Concepts

### What is IBI?

Image-Based Installation (IBI) for single-node OpenShift is a streamlined provisioning method that reduces deployment time by using pre-built, bootable OS images. This method is particularly valuable for edge deployments and telecom use cases where rapid, repeatable cluster deployment is essential.

Some of the key benefits of using IBI provisioning are:

* Significantly faster cluster deployment compared to standard installation
* Consistent cluster configurations through seed images
* Reduced network bandwidth requirements for remote deployments
* Simplified deployment process for edge environments

For additional details, see the [Red Hat OpenShift Image-Based Installation documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/edge_computing/image-based-installation-for-single-node-openshift).

## IBI Workflow Overview

The Image-Based Installation workflow consists of four main phases. The first three are preparation steps, while the final phase is performed per cluster deployment.

### Preparation Phases (Completed in Advance)

* Phase 1: Seed Image Preparation – Create a seed image from a reference bare metal cluster.
* Phase 2: Live ISO Creation – Generate a bootable ISO that embeds the seed image.
* Phase 3: Server Pre-Provisioning – Install the live ISO on target hardware via virtual media.

### Deployment Phase (Executed Per Cluster)

* Phase 4: Cluster Provisioning – Deploy clusters using a ProvisioningRequest CR referencing IBI templates.

### Timing and Reusability Considerations

* Phases 1–3 must be completed in advance to enable rapid cluster deployment when needed.
* Phases 1 and 2 are reusable across multiple servers with the same hardware configuration and desired cluster setup. A single seed image and installation ISO can be used to pre-provision multiple servers.
* Phase 3 is server-specific and must be performed individually for each target server, but can be run in parallel across multiple servers using the same ISO.
* Phase 4 is cluster-specific and is executed when you are ready to deploy. It requires a ProvisioningRequest CR that references ClusterTemplates pointing to the default IBI templates in the SiteConfig Operator.

## Available Sample Resources

This repository includes sample resources for IBI-based provisioning:

* [IBI Cluster Instance Defaults](../samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/clusterinstance-defaults-ibi.yaml): References the IBI templates, `ibi-cluster-templates-v1` and `ibi-node-templates-v1`, from the SiteConfig Operator
* [IBI ClusterTemplate](../samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-ibi-v4-Y-Z-1.yaml): Uses the IBI-specific cluster instance defaults

## Implementation Guide

### Phase 1: Seed Image Preparation

A seed image is built from a reference OpenShift cluster that includes the desired configuration, applications, and customizations. The reference cluster should be running OpenShift 4.18 or later.

#### Steps to Create the Seed Image

**Step 1:** Provision the reference seed cluster using a ProvisioningRequest CR and the SiteConfig Operator's default assisted-installer templates.
Before provisioning, be sure to include the following MachineConfig in the extra manifests.
This ensures that a separate partition is created and the `/var/lib/containers` partition is shared between the two ostree stateroots used during the pre-install process.

```yaml
  98-var-lib-containers-partitioned.yaml: |        
    apiVersion: machineconfiguration.openshift.io/v1
    kind: MachineConfig
    metadata:
      labels:
        machineconfiguration.openshift.io/role: master
      name: 98-var-lib-containers-partitioned
    spec:
      config:
        ignition:
          version: 3.2.0
        storage:
          disks:
            - device: /dev/disk/by-path/pci-0000:43:00.0-nvme-1
              partitions:
                - label: var-lib-containers
                  startMiB: 0
                  sizeMiB: 500000
          filesystems:
            - device: /dev/disk/by-partlabel/var-lib-containers
              format: xfs
              mountOptions:
                - defaults
                - prjquota
              path: /var/lib/containers
              wipeFilesystem: true
        systemd:
          units:
            - contents: |-
                # Generated by Butane
                [Unit]
                Before=local-fs.target
                Requires=systemd-fsck@dev-disk-by\x2dpartlabel-var\x2dlib\x2dcontainers.service
                After=systemd-fsck@dev-disk-by\x2dpartlabel-var\x2dlib\x2dcontainers.service
                [Mount]
                Where=/var/lib/containers
                What=/dev/disk/by-partlabel/var-lib-containers
                Type=xfs
                Options=defaults,prjquota
                [Install]
                RequiredBy=local-fs.target
              enabled: true
              name: var-lib-containers.mount 
```

> **Note:** Remember to update the MachineConfig with the `device` information specific to your server before applying it.

**Step 2:** Annotate the BareMetalHost to prevent cleanup:

```yaml
clcm.openshift.io/skip-cleanup: ""
```

**Step 3:** Detach the spoke cluster by deleting the ProvisioningRequest CR. This automatically removes the corresponding ClusterInstance CR and related installation manifests.

**Step 4:** Install the LifeCycleAgent (LCA) Operator on the spoke cluster and create a `SeedGenerator` CR:

```yaml
apiVersion: lca.openshift.io/v1
kind: SeedGenerator
metadata:
  name: seedimage
spec:
  seedImage: quay.io/<user-account>/seed-container-image:4.Y.Z
```

> **Note:** Before applying the `SeedGenerator` CR, ensure that all ACM-related resources (including observability add-ons) are fully removed from the spoke cluster.

### Phase 2: Live ISO Creation

After generating the seed image, use the `openshift-install` program to create a live installation ISO that embeds the seed image and installation logic:

```bash
openshift-install image-based create image --dir ibi-iso-workdir
INFO Consuming Image-based Installation ISO Config from target directory
INFO Creating Image-based Installation ISO with embedded ignition
```

> **Note:** The `ibi-iso-workdir` directory must contain an `ImageBasedInstallationConfig` resource. If you don't already have one, you can generate a default template as follows:

```bash
openshift-install image-based create image-config-template --dir ibi-iso-workdir
```

The live ISO contains:

* The OpenShift seed image with preconfigured cluster state
* Installation automation and configuration logic

> **Note:** Network and hardware-specific configurations are applied later through the IBI templates provided by the SiteConfig Operator. These are not embedded in the live ISO.

### Phase 3: Server Pre-Provisioning

Before provisioning clusters with a ProvisioningRequest CR, pre-provision your bare metal servers using the live installation ISO and BMC virtual media:

1. Mount the live ISO to the target server.
2. Boot from virtual media. During installation, the ISO will:
   * Install Red Hat Enterprise Linux CoreOS (RHCOS) to disk
   * Pull the seed image
   * Pre-cache OpenShift release container images
   * Apply the preconfigured OpenShift cluster state
   * Prepare the server for cluster provisioning

### Phase 4: Cluster Provisioning

#### ProvisioningRequest CR for IBI

The IBI ProvisioningRequest CR follows the same structure as the [sample ProvisioningRequest](../../config/samples/v1alpha1_provisioningrequest.yaml) but references the IBI-specific ClusterTemplate.

#### IBI Provisioning Process

The provisioning flow mirrors the standard ProvisioningRequest workflow, with adjustments for pre-provisioned hardware:

1. ProvisioningRequestValidated – Parameters validated against the ClusterTemplate schema
2. ClusterInstanceRendered – Renders ClusterInstance with IBI-specific templates
3. ClusterResourcesCreated – Creates required cluster resources
4. HardwareTemplateRendered – Processes hardware allocation for pre-provisioned servers
5. HardwareProvisioned – Allocates pre-provisioned hardware resources
6. ClusterProvisioned – Installs the cluster using the IBI Operator
7. ConfigurationApplied – Applies cluster configuration via ACM policies

## Monitoring and Management

### Monitoring IBI Provisioning

Monitor provisioning progress with standard commands:

```console
# Watch ProvisioningRequest status
oc get provisioningrequest <UUID> -w

# Monitor ClusterInstance
oc get clusterinstance -A

# Monitor ImageClusterInstance
oc get imageclusterinstance -A

# Check O-Cloud Manager logs
oc logs -n oran-o2ims -l control-plane=controller-manager -f
```

### Deleting IBI-Provisioned Clusters

To delete an IBI-provisioned cluster:

```console
oc delete provisioningrequest <UUID>
```

The O-Cloud Manager automatically cleans up all IBI-specific resources.
