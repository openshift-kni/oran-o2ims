# Gitops setup

This directory highlights the recommended GIT layout and goes over the Gitops setup for deploying and configuring a cluster through the O-Cloud Manager.

## Layout

The content is as follows:
* `clustertemplates`
    * the hardware provisioning ConfigMap
    * the default ConfigMaps for installation and configuration
    * the ClusterTemplate and its namespace
    * the pull secret
    * the extra-manifests
        * The content of the extra-manifests directory should be copied over from the [cnf-features-deploy](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/source-crs/extra-manifest) repo or extracted from the [ztp-site-generate](https://catalog.redhat.com/software/containers/openshift4/ztp-site-generate-rhel8/6154c29fd2c7f84a4d2edca1) container.
        * Make sure to always bring over the extra-manifests corresponding to the OCP release provided in the ClusterTemplate CR.
        * ArgoCD will create the extra-manifests ConfigMap by putting together all the extra-manifests.
        * The extra-manifests included here are just samples.

* `policytemplates`
    * the namespace where the ACM Policies are created
    * the ManagedClusterSetBinding
    * the ACM PolicyGenerator(s)
    * the source CRs
        * The content of the `source-crs` directory should be copied over from the [cnf-features-deploy](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/source-crs/) repo or extracted from the [ztp-site-generate](https://catalog.redhat.com/software/containers/openshift4/ztp-site-generate-rhel8/6154c29fd2c7f84a4d2edca1) container.
        * Make sure to bring over the source CRs corresponding to the OCP release provided in the ClusterTemplate CR.
        * The ACM PGs will reference the source CRs to generate the ACM Policies that will be applied on the spoke cluster(s).
        * The source CRs included here are just samples.

## Preparation of the Hub cluster

Preparing the hub cluster for provisioning clusters through the O-Cloud Manager is similar to the [ZTP way](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp/gitops-subscriptions/argocd#preparation-of-hub-cluster-for-ztp).

Distinctions:
* The `Application` CRs should point to the [clustertemplates](./clustertemplates/) and respectively the [policytemplates](./policytemplates/) directories.
* The following should be added to the [AppProject](https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/deployment/app-project.yaml) for `clustertemplates`, under `spec.clusterResourceWhitelist`:
```yaml
  - group: o2ims.provisioning.oran.org
    kind: ClusterTemplate
```
* TALM is not used for the initial cluster installation and configuration, but it's still needed for cluster upgrades.
