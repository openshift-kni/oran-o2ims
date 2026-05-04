<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Glossary

## O-RAN and O-Cloud Terms

| Term | Definition |
|---|---|
| O-RAN | Open Radio Access Network. An industry alliance defining open, interoperable standards for RAN infrastructure. |
| O-Cloud | The cloud platform that hosts O-RAN network functions. In this context, OpenShift clusters managed by the O-Cloud Manager. |
| O2 IMS | O-RAN O-Cloud Infrastructure Management Services. The API specification for managing O-Cloud infrastructure, implemented by the O-Cloud Manager. |
| SMO | Service Management and Orchestration. The external management system that interacts with the O-Cloud Manager via the O2 IMS API. |
| DU | Distributed Unit. A component of the RAN architecture that handles real-time baseband processing, typically deployed on edge servers. |
| RAN | Radio Access Network. The part of a telecommunications network that connects end-user devices to the core network. |

## OpenShift and Kubernetes Terms

| Term | Definition |
|---|---|
| ACM | Advanced Cluster Management for Kubernetes. Red Hat's multi-cluster management solution used by the O-Cloud Manager for cluster lifecycle and policy management. |
| SNO | Single-Node OpenShift. An OpenShift deployment on a single server, common for edge/DU deployments. |
| MNO | Multi-Node OpenShift. An OpenShift deployment with multiple control plane and/or worker nodes. |
| MCP | MachineConfigPool. An OpenShift resource that groups nodes for configuration management and rolling updates. |

## Metal3 and Hardware Terms

| Term | Definition |
|---|---|
| Metal3 | An open-source project for bare-metal host management in Kubernetes, using the Ironic provisioning service. |
| BMO | Bare Metal Operator. The Metal3 operator that manages BareMetalHost resources and drives hardware provisioning via Ironic. |
| BMH | BareMetalHost. A Metal3 CR representing a physical server, including its BMC connection details and hardware inventory. |
| BMC | Baseboard Management Controller. An embedded controller on a server that provides out-of-band management (power control, virtual media, sensor monitoring). |
| HFC | HostFirmwareComponents. A Metal3 CR that tracks firmware component versions and pending firmware updates for a BMH. |
| HFS | HostFirmwareSettings. A Metal3 CR that tracks BIOS/UEFI settings for a BMH. |
| IPA | Ironic Python Agent. A lightweight agent that runs on bare-metal hosts during inspection and provisioning to perform hardware discovery and configuration. |
| iDRAC | Integrated Dell Remote Access Controller. Dell's BMC implementation. |
| iLO | Integrated Lights-Out. HPE's BMC implementation. |

## O-Cloud Manager Terms

| Term | Definition |
|---|---|
| ClusterTemplate (ct) | A CR that references the templates, defaults, and schemas needed to provision a cluster. |
| ProvisioningRequest (pr) | A CR that triggers cluster provisioning by referencing a ClusterTemplate and providing site-specific parameters. |
| HardwareProfile (hwprofile) | A CR that defines desired firmware versions and BIOS settings for a class of servers. |
| NodeAllocationRequest (nar) | A CR created by the O-Cloud Manager to request BMH allocation from the hardware manager. |
| AllocatedNode (allocatednode) | A CR created by the hardware manager representing a BMH that has been allocated to a provisioning request. |

## Provisioning and Lifecycle Terms

| Term | Definition |
|---|---|
| Day-0 | Initial provisioning. The process of deploying a cluster for the first time, including hardware allocation, firmware configuration, and cluster installation. |
| Day-2 | Post-provisioning operations. Configuration changes, firmware updates, and upgrades performed on running clusters. |
| IBU | Image-Based Upgrade. An upgrade method that uses a pre-built seed image to perform fast, image-based cluster upgrades with automatic rollback capability. |
| IBI | Image-Based Installation. A provisioning method that uses a pre-built seed image for rapid cluster deployment, bypassing the standard assisted-installer flow. |
| IBGU | ImageBasedGroupUpgrade. A CR that orchestrates image-based upgrades across one or more clusters. |
| LCA | Lifecycle Agent. An operator that manages image-based upgrades on spoke clusters. |
| OADP | OpenShift API for Data Protection. An operator for backup and restore operations, used during image-based upgrades to preserve cluster state. |

## GitOps and Policy Terms

| Term | Definition |
|---|---|
| ZTP | Zero Touch Provisioning. An approach for automated, hands-off cluster deployment using GitOps. |
| ArgoCD | A GitOps continuous delivery tool that syncs Kubernetes resources from a Git repository to a cluster. |
| PolicyGenerator | An ACM resource that generates governance policies from templates and source CRs. |
