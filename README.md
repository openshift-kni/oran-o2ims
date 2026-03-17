<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# O-Cloud Manager

<!-- markdownlint-disable MD033 -->
<a href="https://github.com/o-ran-sc/it-test/tree/master/test_scripts/O2IMS_Compliance_Test"><img alt="100%" src="https://img.shields.io/badge/O--RAN_SC_O2_IMS_Automated_Test_Compliance-100%25-green"/></a>
<!-- markdownlint-enable MD033 -->

The O-Cloud Manager is an implementation of the O-RAN O2 IMS API on top of OpenShift
and Advanced Cluster Management (ACM). It provides lifecycle management for O-Cloud
infrastructure, including bare-metal server inventory, cluster provisioning and
configuration, firmware management, and alarm monitoring — all exposed through
O-RAN compliant REST APIs.

## User Guide

### Setup

- [Prerequisites](./docs/user-guide/prereqs.md)
- [Environment Setup](./docs/user-guide/environment-setup.md)
- [GitOps Repository Layout and Setup](./docs/user-guide/gitops-layout-and-setup.md)
- [Server Onboarding](./docs/user-guide/server-onboarding-orig.md)
- [Template Overview](./docs/user-guide/template-overview.md)

### Cluster Provisioning

- [Cluster Provisioning](./docs/user-guide/cluster-provisioning.md)
- [Image-Based Installation (IBI)](./docs/user-guide/ibi-based-cluster-provisioning.md)

### Day-2 Operations

- [Day-2 Cluster Configuration](./docs/user-guide/cluster-configuration.md)
- [Firmware Update Workflow](./docs/user-guide/firmware-update-workflow.md)
- [Cluster Upgrade](./docs/user-guide/upgrade.md)

### APIs

- [Inventory API](./docs/user-guide/inventory-api.md)
- [Cluster API](./docs/user-guide/cluster-api.md)
- [Alarms API](./docs/user-guide/alarms-user-guide.md)

### Troubleshooting

- [Troubleshooting](./docs/troubleshooting.md)
- [Must-Gather](./docs/user-guide/must-gather.md)

## Submodules

This repo uses submodules to pull in konflux scripts from another repo. The `hack/update_deps.sh` script, which is called from various Makefile targets,
includes a call to `git submodules update` to ensure submodules are downloaded and up to date automatically in the designer workflow.
If you want to skip running the update command, if you're working on something related to submodules for example, set `SKIP_SUBMODULES_SYNC` to `yes` on the `make` command-line, such as:

```console
$ make SKIP_SUBMODULE_SYNC=yes ci-job
Update dependencies
hack/update_deps.sh
Skipping submodule sync
hack/install_test_deps.sh
...
```
