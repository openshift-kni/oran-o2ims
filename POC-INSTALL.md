<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

<!-- vscode-markdown-toc -->

# POC Operator Installation

<!-- TOC -->
- [POC Operator Installation](#poc-operator-installation)
  - [POC Images](#poc-images)
  - [Platform Requirements](#platform-requirements)
  - [Installing POC Operators](#installing-poc-operators)
    - [CatalogSource CRs](#catalogsource-crs)
    - [Namespace CRs](#namespace-crs)
    - [OperatorGroup CRs](#operatorgroup-crs)
    - [Subscription CRs](#subscription-crs)
  - [Checking Status](#checking-status)
  - [Uninstalling POC Operators](#uninstalling-poc-operators)
    - [Uninstall O-Cloud Hardware Manager Plugin](#uninstall-o-cloud-hardware-manager-plugin)
    - [Uninstall O-Cloud Manager](#uninstall-o-cloud-manager)
<!-- TOC -->

<!-- vscode-markdown-toc-config
	numbering=false
	autoSave=false
	/vscode-markdown-toc-config -->
<!-- /vscode-markdown-toc -->

## POC Images

```console
# Operator images:
quay.io/openshift-kni/oran-o2ims-operator:4.18.0-poc.2503.0
quay.io/openshift-kni/oran-hwmgr-plugin:4.18.0-poc.2503.0

# Bundle images:
quay.io/openshift-kni/oran-o2ims-operator-bundle:4.18.0-poc.2503.0
quay.io/openshift-kni/oran-hwmgr-plugin-bundle:4.18.0-poc.2503.0

# Catalog images:
quay.io/openshift-kni/oran-o2ims-operator-catalog:v4.18.0-poc.2503.0
quay.io/openshift-kni/oran-hwmgr-plugin-catalog:v4.18.0-poc.2503.0
```

## Platform Requirements

The POC O-Cloud Manager and O-Cloud Hardware Manager Plugin operators have been tested against the following platform versions:

- OCP: 4.18.5
- ACM: 2.12.2
- MCE: 2.7.2

## Installing POC Operators

For installation of the POC O-Cloud Manager and O-Cloud Hardware Manager Plugin operators, you need to create CatalogSource, Namespace, OperatorGroup, and Subscription CRs.

### CatalogSource CRs

```yaml
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  annotations:
    target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
  name: oran-o2ims-poc
  namespace: openshift-marketplace
spec:
  displayName: "poc-oran-o2ims-v4.18.0-poc.2503.0"
  image: quay.io/openshift-kni/oran-o2ims-operator-catalog:v4.18.0-poc.2503.0
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 1h
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  annotations:
    target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
  name: oran-hwmgr-plugin-poc
  namespace: openshift-marketplace
spec:
  displayName: "poc-oran-hwmgr-plugin-v4.18.0-poc.2503.0"
  image: quay.io/openshift-kni/oran-hwmgr-plugin-catalog:v4.18.0-poc.2503.0
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 1h
```

### Namespace CRs

```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: oran-o2ims
  annotations:
    workload.openshift.io/allowed: management
---
apiVersion: v1
kind: Namespace
metadata:
  name: oran-hwmgr-plugin
  annotations:
    workload.openshift.io/allowed: management
```

### OperatorGroup CRs

```yaml
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: oran-o2ims-operators
  namespace: oran-o2ims
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: oran-hwmgr-plugin-operators
  namespace: oran-hwmgr-plugin
spec:
  targetNamespaces:
  - oran-hwmgr-plugin
```

### Subscription CRs

```yaml
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: oran-o2ims-subscription
  namespace: oran-o2ims
spec:
  channel: alpha
  name: oran-o2ims
  source: oran-o2ims-poc
  sourceNamespace: openshift-marketplace
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: oran-hwmgr-plugin-subscription
  namespace: oran-hwmgr-plugin
spec:
  channel: alpha
  name: oran-hwmgr-plugin
  source: oran-hwmgr-plugin-poc
  sourceNamespace: openshift-marketplace
```

## Checking Status

```console
$ oc get csv -n oran-o2ims oran-o2ims.v4.18.0-poc.2503.0
NAME                            DISPLAY               VERSION             REPLACES   PHASE
oran-o2ims.v4.18.0-poc.2503.0   ORAN O2IMS Operator   4.18.0-poc.2503.0              Succeeded

$ oc get csv -n oran-hwmgr-plugin oran-hwmgr-plugin.v4.18.0-poc.2503.0
NAME                                   DISPLAY                           VERSION             REPLACES   PHASE
oran-hwmgr-plugin.v4.18.0-poc.2503.0   O-Cloud Hardware Manager Plugin   4.18.0-poc.2503.0              Succeeded

$ oc get pods -n oran-o2ims
NAME                                             READY   STATUS    RESTARTS   AGE
alarms-server-5cc4467bf8-nhz4v                   1/1     Running   0          6h45m
artifacts-server-7595c7ccd5-zdxbq                1/1     Running   0          6h45m
cluster-server-9968f875b-rkmt4                   1/1     Running   0          6h45m
oran-o2ims-controller-manager-6b9b7ddb67-tv4lp   1/1     Running   0          6h45m
postgres-server-6c569785d4-vljjf                 1/1     Running   0          6h45m
provisioning-server-5db9c98bf5-k2hv6             1/1     Running   0          6h45m
resource-server-5787b487dc-dvr4k                 1/1     Running   0          6h45m

$ oc get pods -n oran-hwmgr-plugin
NAME                                                   READY   STATUS    RESTARTS   AGE
oran-hwmgr-plugin-controller-manager-98d996565-klpsd   1/1     Running   0          6h46m
```

## Uninstalling POC Operators

### Uninstall O-Cloud Hardware Manager Plugin

```console
oc delete -n oran-hwmgr-plugin subscription.operators.coreos.com oran-hwmgr-plugin-subscription
oc delete -n oran-hwmgr-plugin csv oran-hwmgr-plugin.v4.18.0-poc.2503.0
oc delete crd \
    hardwaremanagers.hwmgr-plugin.oran.openshift.io \
    hardwareprofiles.hwmgr-plugin.oran.openshift.io
oc delete namespace oran-hwmgr-plugin
oc delete operators oran-hwmgr-plugin.oran-hwmgr-plugin
oc delete clusterrole.rbac.authorization.k8s.io oran-hwmgr-plugin-metrics-reader
oc delete -n openshift-marketplace catalogsource oran-hwmgr-plugin-poc
```

### Uninstall O-Cloud Manager

```console
oc delete -n oran-o2ims subscription.operators.coreos.com oran-o2ims-subscription
oc delete -n oran-o2ims csv oran-o2ims.v4.18.0-poc.2503.0
oc delete crd \
    clustertemplates.o2ims.provisioning.oran.org \
    hardwaretemplates.o2ims-hardwaremanagement.oran.openshift.io \
    inventories.o2ims.oran.openshift.io \
    nodepools.o2ims-hardwaremanagement.oran.openshift.io \
    nodes.o2ims-hardwaremanagement.oran.openshift.io \
    provisioningrequests.o2ims.provisioning.oran.org
oc delete namespace oran-o2ims
oc delete operators oran-o2ims.oran-o2ims
oc delete rolebindings -n kube-system oran-o2ims-controller-manager-service-auth-reader
oc delete clusterrole.rbac.authorization.k8s.io \
    oran-o2ims-admin-role \
    oran-o2ims-alarms-server \
    oran-o2ims-alertmanager \
    oran-o2ims-artifacts-server \
    oran-o2ims-cluster-server \
    oran-o2ims-maintainer-role \
    oran-o2ims-metrics-reader \
    oran-o2ims-provisioner-role \
    oran-o2ims-provisioning-server \
    oran-o2ims-reader-role \
    oran-o2ims-resource-server \
    oran-o2ims-subject-access-reviewer \
    oran-o2ims-subscriber-role
oc delete clusterrolebinding.rbac.authorization.k8s.io \
    oran-o2ims-admin-binding \
    oran-o2ims-alarms-server \
    oran-o2ims-alarms-server-subject-access-reviewer-binding \
    oran-o2ims-alertmanager \
    oran-o2ims-artifacts-server \
    oran-o2ims-artifacts-server-subject-access-reviewer-binding \
    oran-o2ims-cluster-server \
    oran-o2ims-cluster-server-subject-access-reviewer-binding \
    oran-o2ims-maintainer-binding \
    oran-o2ims-provisioner-binding \
    oran-o2ims-provisioning-server \
    oran-o2ims-provisioning-server-subject-access-reviewer-binding \
    oran-o2ims-reader-binding \
    oran-o2ims-resource-server \
    oran-o2ims-resource-server-subject-access-reviewer-binding \
    oran-o2ims-subscriber-binding
oc delete -n openshift-marketplace catalogsource oran-o2ims-poc
```
