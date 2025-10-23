<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

<!-- vscode-markdown-toc -->
# O-RAN O-Cloud Manager

<!-- markdownlint-disable MD033 -->
<a href="https://github.com/o-ran-sc/it-test/tree/master/test_scripts/O2IMS_Compliance_Test"><img alt="100%" src="https://img.shields.io/badge/O--RAN_SC_O2_IMS_Automated_Test_Compliance-100%25-green"/></a>
<!-- markdownlint-enable MD033 -->

<!-- TOC -->
- [O-RAN O-Cloud Manager](#o-ran-o-cloud-manager)
  - [Prerequisites](./docs/user-guide/prereqs.md)
  - [Environment Setup](./docs/user-guide/environment-setup.md)
  - [GitOps Repository Layout And Setup](./docs/user-guide/gitops-layout-and-setup.md)
  - [Server Onboarding](./docs/user-guide/server-onboarding.md)
  - [Template Overview](./docs/user-guide/template-overview.md)
  - [Provisioning Request API](./docs/user-guide/cluster-provisioning.md)
  - [Inventory API](./docs/user-guide/inventory-api.md)
  - [Cluster API](./docs/user-guide/cluster-api.md)
  - [Alarm API](./docs/user-guide/alarms-user-guide.md)
  - [Submodules](#submodules)
<!-- TOC -->

<!-- vscode-markdown-toc-config
	numbering=false
	autoSave=false
	/vscode-markdown-toc-config -->
<!-- /vscode-markdown-toc -->

This project is an implementation of the O-RAN O2 IMS API on top of
OpenShift and ACM.

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
