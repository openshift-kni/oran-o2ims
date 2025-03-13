<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Service for Alertmanager

## IMPORTANT NOTE: Only an operator will finally manage the alarm server. Files here are simply here to unblock any work with api implementation and test. Please delete these static k8s files once we have this integrated. Update anything else as needed (e.g makefile)

This directory contains everything to expose a DNS entry for Alertmanager URL config.
Replace the IP address with the actual IP address of your cluster in the [endpoints.yaml](base/endpoints.yaml) file.

```shell
make create-am-service
```
