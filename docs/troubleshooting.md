# Troubleshooting

## Collecting must-gather data

When troubleshooting O-Cloud Manager issues, always collect must-gather data
first. The custom must-gather image collects O-Cloud Manager CRs, Metal3
resources, and pod logs that are not included in the standard OpenShift
must-gather.

```console
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:4.22.0
```

See the [must-gather guide](user-guide/must-gather.md) for details on what is
collected and additional usage options.

## Diagnosing provisioning failures

### Identifying the failed phase

Check the ProvisioningRequest status conditions to determine which phase failed:

```console
oc get provisioningrequests.clcm.openshift.io <name> -o yaml
```

Look at the `status.conditions` list. The last condition with `status: "False"` indicates
the failed phase. Common failure conditions:

| Condition | Phase | Common Causes |
|---|---|---|
| `ProvisioningRequestValidated` | Validation | Invalid template parameters, missing ConfigMaps |
| `NodeAllocationRequestRendered` | NodeAllocationRequest rendering | Invalid hwMgmtDefaults or HardwareProfile CR |
| `HardwareProvisioned` | BMH allocation | No available BMHs matching resource selector |
| `HardwareConfigured` | Firmware/BIOS | Firmware URL unreachable, BMC errors |
| `ClusterProvisioned` | Cluster install | Installation timeout, network issues |
| `ConfigurationApplied` | Policy config | Policy non-compliance, missing policy CRs |

### Checking operator logs

Find the controller manager pod and view logs:

```console
oc logs -n oran-o2ims -l app=o-cloud-manager --tail=100
```

The controller manager and hardware manager logs are the most common to check.
Other server logs may be useful depending on the issue:

```console
# Hardware manager
oc logs -n oran-o2ims -l app=hardwaremanager-server --tail=100

# Resource server
oc logs -n oran-o2ims -l app=resource-server --tail=100

# Cluster server
oc logs -n oran-o2ims -l app=cluster-server --tail=100

# Alarms server
oc logs -n oran-o2ims -l app=alarms-server --tail=100

# Artifacts server
oc logs -n oran-o2ims -l app=artifacts-server --tail=100

# Provisioning server
oc logs -n oran-o2ims -l app=provisioning-server --tail=100

# Database
oc logs -n oran-o2ims -l app=postgres-server --tail=100
```

## Hardware provisioning issues

### No available BareMetalHosts

If `HardwareProvisioned` shows `Failed` with a message about no available resources:

1. Check that BMHs exist with the expected labels:

   ```console
   oc get baremetalhosts -A --show-labels
   ```

2. Verify the resource selector in the ClusterTemplate hwMgmtDefaults matches the BMH labels:

   ```console
   oc get clustertemplates.clcm.openshift.io <name> -n <namespace> -o jsonpath='{.spec.templateDefaults.hwMgmtDefaults}'
   ```

3. Check that BMHs are not already allocated:

   ```console
   oc get baremetalhosts -A -l clcm.openshift.io/allocated=true
   ```

4. Check for BMHs with validation issues (see below).

### BareMetalHost validation issues

When a ProvisioningRequest fails with an error indicating no resources are available, it
may be because some BareMetalHosts have validation issues that prevent them from being
used for provisioning. These issues are tracked using the
`validation.clcm.openshift.io/unavailable` label on BareMetalHost resources.

To list all BareMetalHosts with validation issues:

```console
oc get baremetalhosts -A -l validation.clcm.openshift.io/unavailable
```

The label can have the following values:

- `hfc-missing-firmware-data` - Multiple firmware components are missing (2 or more of BIOS, BMC, NIC)
- `hfc-missing-bios-data` - BIOS firmware component data is missing
- `hfc-missing-bmc-data` - BMC firmware component data is missing
- `hfc-missing-nic-data` - NIC firmware component data is missing

To check the specific validation issue for a host:

```console
oc get baremetalhost <hostname> -n <namespace> \
  -o jsonpath='{.metadata.labels.validation\.clcm\.openshift\.io/unavailable}{"\n"}'
```

These validation labels are automatically managed by the HostFirmwareComponents
controller. The controller monitors HostFirmwareComponents status for each BareMetalHost,
only applies validation labels to HPE and Dell systems that are O-Cloud managed, and
automatically removes the label when the firmware component data becomes available.

To resolve the issue:

1. Check the corresponding HostFirmwareComponents resource status to see which firmware
   components are missing.
2. Wait for the Metal3 baremetal-operator to populate the firmware component data through
   inspection.
3. The validation label will be automatically removed once all required firmware
   components are present.

> [!NOTE]
> Certain nodes may be unable to return complete firmware data to Metal3 Ironic queries
> when the node is powered off. This can result in incomplete data in the
> HostFirmwareComponents CR. If the firmware component data remains incomplete after
> inspection, delete the BareMetalHost resource, manually power on the physical node,
> and recreate the BareMetalHost resource to trigger reinspection with the node powered
> on.

### Firmware update failures

If `HardwareConfigured` shows `Failed` or `TimedOut`:

1. Check the AllocatedNode status for the specific node that failed:

   ```console
   oc get allocatednodes.clcm.openshift.io -A \
     -o custom-columns=NAME:.metadata.name,CONFIGURED:.status.conditions[*].reason,MSG:.status.conditions[*].message
   ```

2. Check the Metal3 CRs for the affected BMH:

   ```console
   # Check firmware update status
   oc get hostfirmwarecomponents.metal3.io <bmh-name> -n <bmh-namespace> -o yaml

   # Check BIOS settings status
   oc get hostfirmwaresettings.metal3.io <bmh-name> -n <bmh-namespace> -o yaml
   ```

3. Check the BMH for error status:

   ```console
   oc get baremetalhost <bmh-name> -n <bmh-namespace> \
     -o jsonpath='{.status.operationalStatus}{"\n"}{.status.errorMessage}{"\n"}'
   ```

See the [Firmware Update Workflow](user-guide/firmware-update-workflow.md) guide for
details on the firmware update process, timeouts, and retry procedures.

## Cluster installation issues

### Installation timeout

If `ClusterProvisioned` shows `TimedOut`:

1. Check the ClusterInstance status:

   ```console
   oc get clusterinstance -A
   ```

2. Check the AgentClusterInstall (for standard provisioning) or ImageClusterInstall
   (for IBI provisioning) for detailed status:

   ```console
   oc get agentclusterinstalls -A -o yaml
   ```

3. Check the agent status on the spoke cluster's namespace:

   ```console
   oc get agents -n <cluster-name> -o yaml
   ```

## Policy configuration issues

### ConfigurationApplied stuck at InProgress

If policies are not becoming compliant:

1. Check which policies are non-compliant:

   ```console
   oc get provisioningrequests.clcm.openshift.io <name> \
     -o jsonpath='{.status.extensions.policies}' | jq
   ```

2. Check the policy status directly:

   ```console
   oc get policies -A | grep <cluster-name>
   ```

3. Verify the PolicyGenerator annotation is set correctly on the root policies. The
   `clustertemplates.clcm.openshift.io/templates` annotation must be present for the
   O-Cloud Manager to track policy compliance. See the
   [GitOps layout guide](user-guide/gitops-layout-and-setup.md) for details.

## Debugging (for developers)

For development debugging with the DLV debugger, see the
[debugging guide](dev/debugging.md).
