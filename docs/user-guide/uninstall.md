# Uninstalling the Operator

This guide covers the proper procedure for uninstalling the O-Cloud Manager operator
and troubleshooting common issues.

**Important:** OLM does not automatically delete Custom Resources (CRs) when an operator
is uninstalled. This is a safety feature to prevent data loss.

The correct uninstall order is:

1. **Delete CRs** - Remove all Custom Resources managed by the operator
2. **Uninstall the operator** - Remove the operator via OLM (Console or CLI)
3. **(Optional) Delete CRDs** - Remove Custom Resource Definitions for complete cleanup

## Before Uninstalling

The Location, OCloudSite, and ResourcePool Custom Resources use Kubernetes finalizers
to protect against accidental deletion of resources that are still in use. This ensures
data integrity by preventing deletion of parent resources while child resources still
reference them.

**First, scale down the operator to allow manual finalizer removal:**

> **Note:** The controller would keep finalizers on parent
> resources while child resources still exist. Scaling down allows you to bypass dependency
> checks if needed.

```bash
$ oc scale deployment -l control-plane=controller-manager -n oran-o2ims --replicas=0
$ oc wait --for=delete pod -l control-plane=controller-manager -n oran-o2ims --timeout=60s
```

**Important:** Before uninstalling the operator, delete these resources in the correct
order (children before parents):

```bash
# 1. Delete ResourcePools
$ oc delete resourcepools --all -n oran-o2ims

# 2. Delete OCloudSites
$ oc delete ocloudsites --all -n oran-o2ims

# 3. Delete Locations
$ oc delete locations --all -n oran-o2ims

# 4. Delete the Inventory CR
$ oc delete inventory --all -n oran-o2ims

# 5. Check for any remaining resources and delete if needed
$ oc get all -n oran-o2ims
```

Then proceed with operator uninstallation via the OpenShift Console or CLI.

## Uninstalling via OpenShift Console

1. Navigate to **Operators** → **Installed Operators**
2. Find the **O-Cloud Manager** operator
3. Click the operator name, then click **Uninstall Operator**
4. Confirm the uninstallation

## Uninstalling via CLI

```bash
# Delete the operator subscription
$ oc delete subscription o-cloud-manager -n oran-o2ims

# Delete the ClusterServiceVersion
$ oc delete csv -n oran-o2ims -l operators.coreos.com/o-cloud-manager.oran-o2ims
```

## (Optional) Deleting CRDs

After uninstalling the operator, you can optionally delete the CRDs for a complete cleanup:

```bash
oc get crd | grep -E 'ocloud.openshift.io|clcm.openshift.io|plugins.clcm.openshift.io' | awk '{print $1}' | xargs oc delete crd
```

## Troubleshooting: Stuck Resources After Uninstall

If the operator was uninstalled before deleting the hierarchy CRs, resources may be
stuck in a "Terminating" state due to unprocessed finalizers.

### Symptoms

- Resources show `Terminating` status but never complete deletion
- Namespace deletion hangs indefinitely
- Check for stuck resources: `$ oc get all -n oran-o2ims -o jsonpath='{range .items[?(@.metadata.deletionTimestamp)]}{.kind}/{.metadata.name}{"\n"}{end}'`

### Recovery Procedure

**First, scale down the operator to allow manual finalizer removal:**

```bash
# Scale down the controller (if still running)
oc scale deployment -l control-plane=controller-manager -n oran-o2ims --replicas=0
oc wait --for=delete pod -l control-plane=controller-manager -n oran-o2ims --timeout=60s
```

Then remove finalizers from the stuck resources:

```bash
# Remove finalizers from ResourcePools
$ oc get resourcepools -n oran-o2ims -o name | xargs -r -I {} oc patch {} -n oran-o2ims --type=merge -p '{"metadata":{"finalizers":null}}'

# Remove finalizers from OCloudSites
$ oc get ocloudsites -n oran-o2ims -o name | xargs -r -I {} oc patch {} -n oran-o2ims --type=merge -p '{"metadata":{"finalizers":null}}'

# Remove finalizers from Locations
$ oc get locations -n oran-o2ims -o name | xargs -r -I {} oc patch {} -n oran-o2ims --type=merge -p '{"metadata":{"finalizers":null}}'
```

After removing finalizers, the resources will be deleted automatically by the k8s garbage collector.
