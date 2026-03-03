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

The Location, OCloudSite, and ResourcePool Custom Resources form a parent-child hierarchy.
While CRs can be deleted in any order, deleting a parent while children still exist will
cause the children to become `Ready=False` with reason `ParentNotFound`.

**Recommended:** Delete resources in reverse hierarchy order (children before parents)
for a clean removal:

```bash
# 1. Delete ResourcePools
oc delete resourcepools --all -n oran-o2ims

# 2. Delete OCloudSites
oc delete ocloudsites --all -n oran-o2ims

# 3. Delete Locations
oc delete locations --all -n oran-o2ims

# 4. Delete the Inventory CR
oc delete inventory --all -n oran-o2ims

# 5. Check for any remaining resources and delete if needed
oc get all -n oran-o2ims
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
oc delete subscription o-cloud-manager -n oran-o2ims

# Delete the ClusterServiceVersion
oc delete csv -n oran-o2ims -l operators.coreos.com/o-cloud-manager.oran-o2ims
```

## (Optional) Deleting CRDs

After uninstalling the operator, you can optionally delete the CRDs for a complete cleanup:

```bash
oc get crd | grep -E 'ocloud.openshift.io|clcm.openshift.io|plugins.clcm.openshift.io' | awk '{print $1}' | xargs oc delete crd
```
