# Must-Gather

The O-Cloud Manager provides a custom must-gather image for collecting
troubleshooting data from a cluster. This collects resources and logs that are
not included in the standard OpenShift must-gather.

## Collected data

The following data is collected:

- O-Cloud Manager custom resources (from all namespaces)
  - `clcm.openshift.io`: ClusterTemplates, ProvisioningRequests,
    HardwareProfiles, NodeAllocationRequests, AllocatedNodes
  - `ocloud.openshift.io`: Inventories
- Metal3 resources (from all namespaces)
  - BareMetalHosts
  - HostFirmwareSettings
  - HostFirmwareComponents
  - PreprovisioningImages
  - HardwareData
  - Secrets referenced by BMH `preprovisioningNetworkDataName` fields
- Pod logs
  - All O-Cloud Manager pods (controller manager, resource-server,
    cluster-server, alarms-server, artifacts-server, provisioning-server,
    postgres-server, hardwaremanager-server)
  - Metal3 pods in `openshift-machine-api` (metal3-baremetal-operator, metal3)
  - Both current and previous container logs are collected

## Must-gather image versions

The must-gather image is tagged by the branch minor version. Use the tag that
matches the O-Cloud Manager release branch deployed on your hub cluster:

| O-Cloud Manager release | Must-gather image tag |
|---|---|
| release-4.21 | `4.21` or `4.21.0` |
| release-4.22 | `4.22` or `4.22.0` |
| main (5.0) | `5.0`, `5.0.0`, or `latest` |

To determine the installed ACM version for the ACM must-gather image:

```shell
oc get multiclusterhubs.operator.open-cluster-management.io -A \
  -o jsonpath='{.items[0].status.currentVersion}{"\n"}'
```

## Usage

Run the must-gather using the O-Cloud Manager must-gather image:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:5.0.0
```

To specify a custom output directory:

```shell
oc adm must-gather --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:5.0.0 --dest-dir=must-gather/tmp
```

To include ACM resources and logs (e.g., ClusterInstance, SiteConfig), combine
with the ACM must-gather image. Replace `<ACM-version>` with your installed ACM
version (e.g., `v2.14`):

```shell
oc adm must-gather \
  --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:5.0.0 \
  --image=registry.redhat.io/rhacm2/acm-must-gather-rhel9:v<ACM-version>
```

To also include the standard OpenShift must-gather:

```shell
oc adm must-gather \
  --image=quay.io/openshift-kni/oran-o2ims-operator-must-gather:5.0.0 \
  --image=registry.redhat.io/rhacm2/acm-must-gather-rhel9:v<ACM-version> \
  --image=quay.io/openshift/origin-must-gather
```

## Data privacy

> [!NOTE]
> The O-Cloud Manager must-gather does not collect BMC credentials. The only Secrets collected are the
> preprovisioning network data Secrets (nmstate configuration) referenced by
> BareMetalHosts, and their `data` values are always blanked at collection time.
> By default, sensitive fields in the other collected CRs and in the pod logs are
> pseudonymized (see [Data redaction](#data-redaction) below) — review before
> sharing externally if needed.

### Data redaction

By default, sensitive fields in both the collected **pod logs** and the
collected **CRs** are redacted before they are written into the archive. This
lets support archives be shared across organizational boundaries without
exposing operational details. Pod logs and CRs are redacted in a single pass so
a value that appears in both (for example a BMC address) maps to the same
pseudonym in each, keeping them correlatable.

Five categories of fields are redacted, identified by their key name (the
structured slog key in pod logs, or the JSON field name in collected CRs):

| Category | Keys | Pseudonym prefix |
|---|---|---|
| IP addresses | `clientIp`, `bmcAddress`, `host`, `ip`, `address` | `ip-` |
| Hostnames | `clusterName`, `hostName`, `hostname`, `bmh`, `managedCluster`, `nodeNames`, `resourceName`, `ingressHost`, `clusterID`, `clusterId`, `allocatedNodeHostMap` | `host-` |
| User identities | `user`, `preferred_username` | `user-` |
| MAC addresses | `bootMACAddress`, `macAddress`, `mac` | `mac-` |
| Serial numbers | `serialNumber`, `serial`, `wwn`, `wwnWithExtension`, `wwnVendorExtension` | `serial-` |

Each value is replaced with a consistent pseudonym computed as
`prefix + HMAC-SHA256(salt, value)` (for example, `10.8.34.97` becomes
`ip-a3f7b2c1`). A random salt is generated per collection and is never written
to the archive, so:

- the same value always maps to the same pseudonym within one collection,
  preserving event correlation across log lines and CRs;
- different collections produce different pseudonyms for the same value; and
- the mapping cannot be reversed from the archive alone.

In addition to key-based redaction, IP and MAC address tokens embedded in
free-text values (such as `msg` and `error` fields, or a `redfish://` BMC URL)
and in non-JSON log lines are scrubbed by pattern, since those formats are
distinctive. Hostnames, users, and serial numbers are only redacted where a key
identifies the content.

If redaction cannot complete (for example, if the redaction script fails), the
collected pod logs and CRs are removed from the archive rather than shipped
unredacted.

Redaction is controlled by environment variables read by the `gather` script:

- `MUST_GATHER_REDACT` — set to `false` to disable redaction entirely
  (enabled by default). Disabling is not recommended for support cases.
- `MUST_GATHER_REDACT_CATEGORIES` — comma-separated subset of
  `ip,host,user,mac,serial` to redact (default: `all`). Unknown category
  names are ignored, but a value that selects no valid categories at all
  (for example a typo such as `bogus`) is treated as a redaction failure,
  so the collected pod logs and CRs are removed rather than shipped
  unredacted.

The default `oc adm must-gather` invocation redacts all categories. To
override the defaults, run the `gather` script with the environment variables
set, for example when collecting manually from the must-gather image:

```shell
MUST_GATHER_REDACT=false /usr/bin/gather
MUST_GATHER_REDACT_CATEGORIES=ip,mac /usr/bin/gather
```

## Analyzing collected data

The must-gather output is organized by resource type. After extracting the archive:

```shell
tar xvf must-gather.tar.gz
cd must-gather.local.*/<image-digest>/
```

Collected CRs are stored as JSON (`.json`) so that sensitive fields can be
redacted; the preprovisioning-secrets are stored as YAML (`.yaml`) with their
`data` values blanked. Key locations within the output:

| Path | Contents |
|---|---|
| `clcm/` | O-Cloud Manager CRs (ClusterTemplates, ProvisioningRequests, NodeAllocationRequests, AllocatedNodes, etc.) |
| `ocloud/` | Inventory CRs |
| `metal3/` | BareMetalHosts, HostFirmwareSettings, HostFirmwareComponents, HardwareData |
| `metal3/preprovisioning-secrets/` | Secrets referenced by BMH preprovisioningNetworkDataName |
| `logs/ocloud-manager/` | Pod logs from O-Cloud Manager namespace |
| `logs/metal3/` | Pod logs from Metal3 pods in openshift-machine-api |

To quickly check a ProvisioningRequest status from the collected data:

```shell
grep -A 5 "provisioningPhase" clcm/provisioningrequests/<namespace>/<name>.json
```
