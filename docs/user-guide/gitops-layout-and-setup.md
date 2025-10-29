<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Preparing GitOps repository

## Repository layout

This guide describes the Git repository content required by the O‑Cloud Manager to deploy and configure a cluster. We recommend organizing the repo as follows. For concrete examples, see the sample content under [git-setup](./samples/git-setup/).

Recommended layout:

```text
git-root/
  clustertemplates/
    hardwareprofiles/                   # HardwareProfile CRs for hardware provisioning
    hardwaretemplates/                  # HardwareTemplate CRs for hardware provisioning
    inventory/                          # BareMetalHost inventory for sites
    version_4.Y.Z/                      # Version matches the OCP version to be installed
      sno-ran-du/
        clusterinstance-defaults-*.yaml # Defaults in ConfigMap for Cluster installation
        policytemplates-defaults-*.yaml # Defaults in ConfigMap for ACM policy configuration
        pull-secret.yaml                # Pull secret used for spoke installation
        extra-manifest/                 # Extra manifests installed on day0
        ns.yaml                         # Namespace where the ClusterTemplate is created
        sno-ran-du-v4-Y-Z-*.yaml        # ClusterTemplate(s) for this OCP version
    version_4.Y.Z+1/                    # Optional upgrade content
  policytemplates/
    version_4.Y.Z/                      # Version matches the OCP version to be installed
      sno-ran-du/
        ns.yaml                         # Namespace where policies are created
        msc-binding.yaml                # ManagedClusterSetBinding for policy placement
        sno-ran-du-pg-v4-Y-Z-*.yaml     # ACM PolicyGenerator(s) for this OCP version
      source-crs/                       # ZTP source CRs; keep in sync with OCP version
    version_4.Y.Z+1/                    # Optional upgrade content
```

Notes

* Keep content versioned by OCP release using the `version_4.Y.Z/` folders so templates and policies align with the target OCP.
* The content of the `extra-manifest` directory should be copied over from the [cnf-features-deploy extra-manifest](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/source-crs/extra-manifest) repo
or extracted from the [ztp-site-generate](https://catalog.redhat.com/software/containers/openshift4/ztp-site-generate-rhel8/6154c29fd2c7f84a4d2edca1) container.
  * ArgoCD will assemble all the extra-manifests into a ConfigMap.
* The content of the `source-crs` directory should be copied over from the [cnf-features-deploy source-crs](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/source-crs/) repo
or extracted from the [ztp-site-generate](https://catalog.redhat.com/software/containers/openshift4/ztp-site-generate-rhel8/6154c29fd2c7f84a4d2edca1) container.
  * The ACM PGs will reference the source CRs to generate the ACM Policies that will be applied on the spoke cluster(s).
* Make sure to bring over the `extra-manifest` and `source-crs` corresponding to the OCP release provided in the ClusterTemplate CR.
* ACM policies must be created under the namespace `ztp-<cluster-template-namespace>`. See the [example](../samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/ns.yaml).
* In the ACM PGs, set `policyAnnotations` to include the annotation `clustertemplates.clcm.openshift.io/templates` with a comma-separated
  list of ClusterTemplates that PG is associated with. Use the ClusterTemplate metadata.name for each entry. This annotation is propagated to
  each generated root Policy. It enables the O‑Cloud Manager to identify which root policies are associated with the
  ClusterTemplate used by a ProvisioningRequest, determine the expected child policies, and accurately detect when configuration
  is complete - ensuring correct provisioning status reporting during Day-2 policy configuration changes. See the [example](../samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-du/sno-ran-du-pg-v4-Y-Z-v1.yaml).

## Full DU profile

For configuring an SNO with a full DU profile according to the [4.19 RAN RDS](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/scalability_and_performance/telco-ran-du-ref-design-specs#telco-ran-du-reference-configuration-crs),
the following main samples can be used as a starting example:

* [ClusterInstance defaults ConfigMap](./samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-full-du/clusterinstance-defaults-full-du-v1.yaml)
* [PolicyTemplate defaults ConfigMap](./samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-full-du/policytemplates-defaults-full-du-v1.yaml)
* [ClusterTemplate](./samples/git-setup/clustertemplates/version_4.Y.Z/sno-ran-full-du/sno-ran-full-du-v4-Y-Z-1.yaml)
* [Full DU profile ACM Policy Generator](./samples/git-setup/policytemplates/version_4.Y.Z/sno-ran-full-du/sno-ran-full-du-pg-v4-Y-Z-v1.yaml)
* Observability configuration. This requires the creation of an ACM policy in the
`open-cluster-management-observability` namespace, as seen in [copy-acm-route-observability-v1.yaml](./samples/git-setup/policytemplates/common/copy-acm-route-observability-v1.yaml).
This policy will create a ConfigMap containing the acm-route in the same namespace as the ACM policies such that it can be used in the hub templates.
  * A custom source-cr ([source-cr-observability.yaml](./samples/git-setup/policytemplates/common/source-cr-observability.yaml)) is used. Currently **the namespaces where ACM policies are created need to be manually added to the namespace list.**
  * For allowing the creation of this policy in the `open-cluster-management-observability` namespace,
  the `AppProject` associated to the desired ACM policies needs to also contain the following in
  its `spec.destinations`:

  ```yaml
    - namespace: open-cluster-management-observability
      server: '*'
  ```

## Preparation of ArgoCD applications

Preparing the ArgoCD applications for provisioning clusters through the O-Cloud Manager is similar to the [ZTP apporach](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/gitops-subscriptions/argocd#preparation-of-hub-cluster-for-ztp), with the following **distinctions**:

* The source path for the ArgoCD Application [clusters](https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/deployment/clusters-app.yaml) should point to the [clustertemplates](./clustertemplates/) directory.
Additionally, configure `spec.ignoreDifferences` for the `BareMetalHost` kind to ignore the following fields:

```yaml
spec:
  ignoreDifferences:
  - group: metal3.io
    jsonPointers:
    - /spec/preprovisioningNetworkDataName
    - /spec/online
    kind: BareMetalHost
```

* The source path for the ArgoCD Application [policies](https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/deployment/policies-app.yaml) should point to the [policytemplates](./policytemplates/) directory.

* The following **additional CRDs** should be added to the [AppProject](https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/deployment/app-project.yaml), under `spec.clusterResourceWhitelist`:

```yaml
  - group: clcm.openshift.io
    kind: ClusterTemplate
  - group: clcm.openshift.io
    kind: HardwareProfile
  - group: clcm.openshift.io
    kind: HardwareTemplate
```
