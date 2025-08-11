<!-- Generated-By: Cursor/claude-4-sonnet -->

# O-Cloud Manager Provisioning Watcher

A standalone CLI tool for watching and monitoring specific Kubernetes CRDs related to O-Cloud provisioning workflows.

## Supported CRDs

The tool can watch the following Custom Resource Definitions (CRDs):

### 1. ProvisioningRequests (clcm.openshift.io/v1alpha1)

Shows cluster provisioning requests with their current status and details.

**Fields displayed:**

- NAME: Request name
- DISPLAYNAME: Human-readable display name
- AGE: Time since creation
- PHASE: Current provisioning phase
- DETAILS: Additional provisioning details

### 2. NodeAllocationRequests (plugins.clcm.openshift.io/v1alpha1)

Shows requests for node allocation within clusters.

**Fields displayed:**

- NAME: Request name
- CLUSTER-ID: Target cluster identifier
- PROVISIONING: Provisioning condition status
- DAY2-UPDATE: Configuration condition status

### 3. AllocatedNodes (plugins.clcm.openshift.io/v1alpha1)

Shows nodes that have been allocated and their current state.

**Fields displayed:**

- NAME: Node name
- NODE-ALLOC-REQUEST: Associated allocation request
- HWMGR-NODE-ID: Hardware manager node identifier
- PROVISIONING: Provisioning condition status
- DAY2-UPDATE: Configuration condition status

### 4. BareMetalHosts (metal3.io/v1alpha1)

Shows bare metal host resources managed by Metal3.

**Filtering**: Only includes BareMetalHosts that have labels whose keys start with `resourceselector.clcm.openshift.io/`. This ensures that only resources managed by the O-Cloud Manager resource selector are displayed.

**Fields displayed:**

- NS: Namespace
- BMH: BareMetalHost name (.metadata.name)
- STATUS: Operational status (.status.operationalStatus)
- STATE: Provisioning state (.status.provisioning.state)
- ONLINE: Host online status (.spec.online)
- POWEREDON: Power state (.status.poweredOn)
- NETDATA: Pre-provisioning network data (.spec.preprovisioningNetworkDataName)
- ERROR: Error type (.status.errorType)

### 5. HostFirmwareComponents (metal3.io/v1alpha1)

Shows firmware component information for bare metal hosts managed by Metal3.

**Filtering**: Only includes HostFirmwareComponents whose `.metadata.name` matches the name of a BareMetalHost that passed the resource selector filtering (i.e., has labels starting with `resourceselector.clcm.openshift.io/`).
Deletion events are always processed regardless of BareMetalHost existence to ensure accurate removal display.

**Fields displayed:**

- HOSTFIRMWARECOMPONENTS: Component resource name (.metadata.name)
- GEN: Metadata generation (.metadata.generation)
- OBSERVED: All observed generations from conditions (.status.conditions[*].observedGeneration) as comma-separated list
- VALID: Status of "Valid" condition (.status.conditions[?(@.type=="Valid")].status)
- CHANGE: Status of "ChangeDetected" condition (.status.conditions[?(@.type=="ChangeDetected")].status)

### 6. HostFirmwareSettings (metal3.io/v1alpha1)

Shows firmware settings information for bare metal hosts managed by Metal3.

**Filtering**: Only includes HostFirmwareSettings whose `.metadata.name` matches the name of a BareMetalHost that passed the resource selector filtering (i.e., has labels starting with `resourceselector.clcm.openshift.io/`).
Deletion events are always processed regardless of BareMetalHost existence to ensure accurate removal display.

**Fields displayed:**

- HOSTFIRMWARESETTINGS: Settings resource name (.metadata.name)
- GEN: Metadata generation (.metadata.generation)
- OBSERVED: All observed generations from conditions (.status.conditions[*].observedGeneration) as comma-separated list
- VALID: Status of "Valid" condition (.status.conditions[?(@.type=="Valid")].status)
- CHANGE: Status of "ChangeDetected" condition (.status.conditions[?(@.type=="ChangeDetected")].status)

## Installation

```bash
# Build the tool
make crd-watcher

# The binary will be created at: bin/crd-watcher
```

## Usage

### Basic Commands

```bash
# Watch all supported CRDs in all namespaces (default)
./bin/crd-watcher --watch

# Watch specific CRD types
./bin/crd-watcher --watch --crds=provisioningrequests,baremetalhosts

# Watch in a specific namespace
./bin/crd-watcher --watch --namespace=my-namespace --all-namespaces=false

# List resources in table format (one-time snapshot)
./bin/crd-watcher --crds=baremetalhosts

# Output in JSON format
./bin/crd-watcher --output=json

# Output in YAML format
./bin/crd-watcher --output=yaml

# Configure screen refresh interval (default: 5 seconds)
./bin/crd-watcher --watch --refresh-interval=10
```

### Available Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--watch, -w` | `false` | Enable real-time screen updates (live dashboard mode) |
| `--crds` | `all` | Comma-separated list of CRD types to watch |
| `--namespace, -n` | `""` | Target namespace (empty = current context namespace) |
| `--all-namespaces` | `true` | Watch resources across all namespaces |
| `--output, -o` | `table` | Output format: `table`, `json`, `yaml` |
| `--refresh-interval` | `5` | Screen refresh interval in seconds (watch mode only) |
| `--inventory-refresh-interval` | `120` | Inventory data refresh interval in seconds (0 to disable) |
| `--kubeconfig` | `""` | Path to kubeconfig file |
| `--log-level, -v` | `1` | Log verbosity level (0-4) |

## Sorting

The tool automatically sorts output based on the CRD type:

- **ProvisioningRequests**: Sorted by `DISPLAYNAME` field (.spec.name)
- **NodeAllocationRequests**: Sorted by `CLUSTER-ID` field (.spec.clusterId)
- **AllocatedNodes**: Sorted by resource name (.metadata.name)
- **BareMetalHosts**: Sorted by resource name (.metadata.name)
- **HostFirmwareComponents**: Sorted by resource name (.metadata.name)
- **HostFirmwareSettings**: Sorted by resource name (.metadata.name)

Sorting is applied in both table mode (non-watch) and watch mode, providing consistent ordered output for easier monitoring and troubleshooting.

## Inventory Data Refresh

When the inventory module is enabled (`--enable-inventory`), the tool can periodically refresh inventory data from the O2IMS API to ensure new cluster nodes and resources are detected at runtime.

### Configuration

- **`--inventory-refresh-interval`**: Set the refresh interval in seconds (default: 120 = 2 minutes)
- Set to `0` to disable periodic refresh and only fetch data once at startup
- Recommended values: 60-600 seconds (1-10 minutes) depending on how frequently your inventory changes

### How It Works

1. **Initial Fetch**: On startup, the tool fetches all inventory data (resource pools, resources, node clusters)
2. **Periodic Refresh**: A background timer periodically re-queries the O2IMS API for fresh data
3. **Event-Triggered Refresh**: Inventory refreshes automatically when BareMetalHost or ProvisioningRequest CRs are updated
4. **Live Updates**: New resources appear in the display immediately when discovered
5. **Stale Object Cleanup**: Before each inventory refresh, stale cached objects are verified and removed from the display
6. **Continuous Cleanup**: Periodic verification ensures long-term accuracy of displayed data

#### Event-Triggered Refreshes

The tool automatically triggers inventory refreshes when:

- **BareMetalHost** resources are deleted or when their provisioning state changes to "available"
- **ProvisioningRequest** resources are deleted or when their provisioning phase changes to "fulfilled"

**Refresh Timing:**

- **BareMetalHost deletions or "available" state**: 1-second delay to quickly reflect infrastructure changes
- **ProvisioningRequest deletions or "fulfilled" phase**: 1-second delay to quickly reflect provisioning completion

This ensures that infrastructure changes are quickly reflected in the inventory data without waiting for the next periodic refresh. Critical events (infrastructure becoming available, provisioning completion, or resource deletions) get priority with a 1-second response time.

#### Display Updates

The watch mode display is optimized for immediate feedback:

- **Deletion Events**: Reflected instantly in the display without any debouncing delay
- **Firmware CR Deletions**: Always processed regardless of BareMetalHost existence to ensure accurate removal display
- **Other Events**: Use 250ms debouncing to prevent flickering during rapid updates

### Example Usage

```bash
# Refresh inventory data every 2 minutes (default)
./bin/crd-watcher --enable-inventory --watch

# Refresh inventory data every 5 minutes
./bin/crd-watcher --enable-inventory --watch --inventory-refresh-interval=300

# Disable periodic refresh (startup fetch only)
./bin/crd-watcher --enable-inventory --watch --inventory-refresh-interval=0
```

## Watch Mode

The watch mode (`--watch`) provides a real-time dashboard with:

- **Live Updates**: Resources are updated in real-time as changes occur
- **Automatic Refresh**: Screen refreshes after the configured interval of inactivity
- **Dynamic Field Sizing**: Column widths adjust based on content
- **Organized Display**: CRDs are grouped and ordered for better readability
- **Flicker-free Updates**: Optimized rendering to minimize screen flashing

**Display Order in Watch Mode:**

1. ProvisioningRequests
2. NodeAllocationRequests
3. AllocatedNodes
4. BareMetalHosts
5. HostFirmwareComponents
6. HostFirmwareSettings

### Example Watch Mode Output

```text
╔═══ O-Cloud Manager Provisioning Watcher ═══ 2024-01-15 14:30:45 UTC
╚═══════════════════════════════════════════════════════════════════

┌─ provisioningrequests ─
│ NAME                     DISPLAYNAME          AGE    PHASE       DETAILS
│ cluster-req-001          prod-cluster-01      2h     Provisioned Complete

┌─ baremetalhosts ─
│ NS              BMH                              STATUS       STATE           ONLINE   POWEREDON   NETDATA                      ERROR
│ openshift-sno   master-0                        OK           provisioned     true     true        master-0-network-config     <none>
│ openshift-sno   worker-1                        discovered   available       false    false       <none>                       <none>

┌─ hostfirmwarecomponents ─
│ HOSTFIRMWARECOMPONENTS                    GEN      OBSERVED     VALID      CHANGE
│ master-0                                   3        2,3,3       True       False
│ worker-1                                   1        1,1         True       True

┌─ hostfirmwaresettings ─
│ HOSTFIRMWARESETTINGS                      GEN      OBSERVED     VALID      CHANGE
│ master-0                                   2        1,2         True       False
│ worker-1                                   1        1,1         True       True

Press Ctrl+C to exit
```

## Use Cases

- **Cluster Provisioning Monitoring**: Track the progress of cluster provisioning requests
- **Node Management**: Monitor node allocation and configuration status
- **Bare Metal Operations**: Watch bare metal host provisioning and power management
- **Firmware Management**: Monitor firmware component updates and validation status
- **Troubleshooting**: Identify issues in provisioning workflows
- **Operations Dashboard**: Real-time view of infrastructure provisioning status

## Inventory Integration

The tool can be easily integrated into:

- CI/CD pipelines for provisioning validation
- Monitoring and alerting systems
- Operational dashboards
- Troubleshooting workflows
- Infrastructure automation scripts

## Inventory Module (Optional)

The watcher includes an optional inventory module that can connect to O2IMS Infrastructure Inventory APIs to fetch and display resources alongside Kubernetes CRDs.

### Setup

Enable the inventory module with OAuth authentication:

```bash
# Enable inventory module with OAuth credentials
crd-watcher --enable-inventory \
    --inventory-server "https://o2ims.example.com" \
    --oauth-token-url "https://auth.example.com/realms/oran/protocol/openid-connect/token" \
    --oauth-client-id "my-client" \
    --oauth-client-secret "my-secret" \
    --oauth-scopes "role:o2ims-reader"
```

### TLS Certificate Authentication

For mutual TLS authentication, add certificate configuration:

```bash
# Enable inventory module with OAuth + mTLS
crd-watcher --enable-inventory \
    --inventory-server "https://o2ims.example.com" \
    --oauth-token-url "https://auth.example.com/realms/oran/protocol/openid-connect/token" \
    --oauth-client-id "my-client" \
    --oauth-client-secret "my-secret" \
    --tls-cert "${HOME}/smo-config/client/client.crt" \
    --tls-key "${HOME}/smo-config/client/client.key" \
    --tls-cacert "${HOME}/smo-config/ca/ca-bundle.pem"
```

You can also use environment variables to pass certificate paths:

```bash
# Set PKI arguments
export PKI_ARGS="--tls-cert ${HOME}/smo-config/client/client.crt --tls-key ${HOME}/smo-config/client/client.key --tls-cacert ${HOME}/smo-config/ca/ca-bundle.pem"

# Use with inventory module
crd-watcher --enable-inventory \
    --inventory-server "https://o2ims.example.com" \
    --oauth-token-url "https://auth.example.com/token" \
    --oauth-client-id "my-client" \
    --oauth-client-secret "my-secret" \
    ${PKI_ARGS}
```

### OAuth Scopes

The `--oauth-scopes` flag supports multiple formats for specifying OAuth scopes:

```bash
# Multiple individual scopes (recommended)
--oauth-scopes "role:o2ims-admin" --oauth-scopes "openid" --oauth-scopes "profile"

# Space-separated scopes in a single string
--oauth-scopes "role:o2ims-admin openid profile"

# Comma-separated (standard pflag behavior)
--oauth-scopes "role:o2ims-admin,openid,profile"
```

**⚠️ Important**: OAuth scopes should be identifiers only, **not** include parameter prefixes:

```bash
# ✅ CORRECT - scope identifiers only
--oauth-scopes "profile role:o2ims-admin openid"

# ❌ INCORRECT - contains 'scope=' prefix
--oauth-scopes "scope=profile role:o2ims-admin openid"
```

The `scope=` prefix is an OAuth parameter name, not part of the scope value. The tool will warn you if it detects this common formatting issue.

All formats are automatically processed and deduplicated. The tool will log the processed scopes when using `-v 1` or higher log levels.

### Environment Variables

OAuth credentials can also be provided via environment variables:

```bash
export OAUTH_CLIENT_ID="my-client"
export OAUTH_CLIENT_SECRET="my-secret"
```

### Inventory Resource Types

When enabled, the inventory module fetches:

- **Resource Pools**: Collections of O2IMS resources with site and pool information
- **Resources**: Individual infrastructure resources from all pools
- **Resource Types**: Classifications and metadata for resources

### Display Fields

#### O-RAN Resource Pool Display

| Field | Description |
|-------|-------------|
| `SITE` | Site location (from extensions or inferred from name) |
| `POOL` | Pool name or identifier |
| `RESOURCE-POOL-ID` | Unique resource pool identifier |
| `DESCRIPTION` | Human-readable description |

#### Inventory Resource Display

| Field | Description |
|-------|-------------|
| `RESOURCE-ID` | Unique identifier for the resource |
| `RESOURCE-TYPE` | Type/classification of the resource |
| `DESCRIPTION` | Human-readable description |
| `STATUS` | Current status (from extensions) |
| `CREATED` | Resource creation timestamp |

### Inventory Integration Features

The inventory module integrates seamlessly with the existing watcher:

- **Sorting**:
  - Resource pools sorted by Site, then Pool name
  - Inventory resources sorted by Resource ID
- **Filtering**: All inventory resources and pools are displayed (no filtering)
- **Formatting**: Supports table, JSON, YAML, and watch mode formats
- **Authentication**: Uses OAuth2 client credentials flow

### Example Output

#### O-RAN Resource Pool Example

```text
TIME       EVENT    AGE             SITE                 POOL                           RESOURCE-POOL-ID                     DESCRIPTION
09:15:30   ADDED    0s              edge-site-1          compute-pool-high-perf         550e8400-e29b-41d4-a716-446655440000 High performance compute resources
09:15:30   ADDED    0s              edge-site-1          storage-pool-ssd               6ba7b810-9dad-11d1-80b4-00c04fd430c8 SSD storage pool for edge workloads
09:15:30   ADDED    0s              central-site         network-pool-5g                7ba7b810-9dad-11d1-80b4-00c04fd430c9 5G network infrastructure pool
```

#### Inventory Resource Example

```text
TIME       EVENT    AGE             RESOURCE-ID                          RESOURCE-TYPE                        DESCRIPTION          STATUS           CREATED
09:15:30   ADDED    0s              550e8400-e29b-41d4-a716-446655440000 compute-node-type                    Physical server      Available        2024-01-15
09:15:30   ADDED    0s              6ba7b810-9dad-11d1-80b4-00c04fd430c8 storage-volume-type                  Storage volume       In-Use           2024-01-15
```

## OAuth Authentication

The inventory module supports OAuth 2.0 client credentials flow as specified in the O2IMS standard:

- **Token Endpoint**: Configurable OAuth token URL
- **Client Credentials**: ID and secret for authentication
- **Scopes**: Configurable OAuth scopes (default: `role:o2ims-reader`)
- **Token Refresh**: Automatic token renewal and error handling

### TLS Security

The inventory module supports comprehensive TLS security:

- **Mutual TLS (mTLS)**: Client certificate authentication using `--tls-cert` and `--tls-key`
- **Server Verification**: Custom CA bundle support using `--tls-cacert`
- **TLS 1.2+ Only**: Enforces modern TLS versions for security
- **Combined Auth**: OAuth + mTLS for enterprise security requirements

| Flag | Description | Example |
|------|-------------|---------|
| `--tls-cert` | Client certificate file for mTLS | `--tls-cert /path/to/client.crt` |
| `--tls-key` | Client private key file for mTLS | `--tls-key /path/to/client.key` |
| `--tls-cacert` | CA certificate bundle for server verification | `--tls-cacert /path/to/ca-bundle.pem` |
