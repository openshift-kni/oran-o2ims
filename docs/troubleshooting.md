# troubleshooting

## Using the development debug mode to attach the DLV debugger

The following instructions provide a mechanism to build an image that is based on a more full-featured distro so that
debug tools are available in the image. It also tailors the deployment configuration so that certain features are
disabled which would otherwise cause debugging with a debugger to be more difficult (or impossible).

1. Build and deploy the debug image

   ```console
   make IMAGE_TAG_BASE=quay.io/${USER}/oran-o2ims VERSION=latest DEBUG=yes build docker-build docker-push install deploy
   ```

2. Forward a port to the Pod to be debugged so the debugger can attach to it. This command will remain active until
   it is terminated with ctrl+c therefore you will need to execute it in a dedicated window (or move it to the
   background).

   ```console
   oc port-forward -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s 40000:40000
   ```

3. Execute a shell into the Pod to be debugged.

   ```console
   oc rsh -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s
   ```

4. Attach the DLV debugger to the process. This is usually PID 1, but this may vary based on the deployment. Use the
   same port number that was specified earlier in the `port-forward` command.

   ```console
   dlv attach --continue --accept-multiclient --api-version 2 --headless --listen :40000 --log 1
   ```

5. Use your IDE's debug capabilities to attach to `localhost:40000` to start your debug session. This will vary based
   on which IDE is being used.

## Troubleshooting ProvisioningRequest failures due to unavailable resources

When a ProvisioningRequest fails with an error indicating no resources are available, it may be because some BareMetalHosts
have validation issues that prevent them from being used for provisioning. These issues are tracked using the
`validation.clcm.openshift.io/unavailable` label on BareMetalHost resources.

### Checking for hosts with validation issues

To list all BareMetalHosts across all namespaces that have validation issues:

```console
oc get baremetalhosts -A -l validation.clcm.openshift.io/unavailable
```

This will show hosts that cannot be used for provisioning due to missing or incomplete firmware component data.

### Understanding validation label values

The `validation.clcm.openshift.io/unavailable` label can have the following values:

- `hfc-missing-firmware-data` - Multiple firmware components are missing (2 or more of BIOS, BMC, NIC)
- `hfc-missing-bios-data` - BIOS firmware component data is missing
- `hfc-missing-bmc-data` - BMC firmware component data is missing
- `hfc-missing-nic-data` - NIC firmware component data is missing

To check the specific validation issue for a host:

```console
oc get baremetalhost <hostname> -n <namespace> -o jsonpath='{.metadata.labels.validation\.clcm\.openshift\.io/unavailable}{"\n"}'
```

### Resolution

These validation labels are automatically managed by the HostFirmwareComponents controller. The controller:

- Monitors HostFirmwareComponents status for each BareMetalHost
- Only applies validation labels to HPE and Dell systems that are O-Cloud managed
- Automatically removes the label when the firmware component data becomes available

To resolve the issue:

1. Check the corresponding HostFirmwareComponents resource status to see which firmware components are missing
2. Wait for the Metal3 baremetal-operator to populate the firmware component data through inspection
3. The validation label will be automatically removed once all required firmware components are present

**Note:** Certain nodes may be unable to return complete firmware data to Metal3 Ironic queries when the node is powered off.
This can result in incomplete data in the HostFirmwareComponents CR. If the firmware component data remains incomplete after
inspection, the user should:

1. Delete the BareMetalHost resource
2. Manually power on the physical node
3. Recreate the BareMetalHost resource to trigger reinspection with the node powered on
