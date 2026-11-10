# Server Onboarding Service - Design Proposal

## Context

Currently, BareMetalHost CRs must be pre-created with known BMC addresses and
credentials before servers can be managed. This proposal adds a new onboarding
service that automates the lifecycle from a declared set of BMC endpoints:
inspection-based hardware validation, BMC address pinning, credential
provisioning, and BMH creation. Each BMC starts on a DHCP-assigned management
address (declared up front in the onboarding request, typically via a per-MAC
DHCP reservation); once a server is validated that same address is pinned to
static on the BMC NIC, so the identity recorded in the request stays stable
without an ongoing DHCP dependency. Target BMC credentials are specified per
server, allowing unique credentials for each BMC.

**The onboarding controller never talks to a BMC directly.** Every BMC operation —
inspection, the DHCP-to-static network pin, DNS, credential provisioning, and LDAP
configuration — is driven **declaratively through Metal3 / Bare Metal Operator
(BMO) APIs**, and BMO performs the underlying Redfish work against the BMC. The
controller sets the desired state on the BareMetalHost (and its companion Metal3
resources / Secrets) and watches the BareMetalHost status conditions BMO reports;
it holds no Redfish client, no BMC credentials in memory, and needs no network
route to the BMC management subnet. This design assumes four forthcoming Metal3 BMC
lifecycle APIs — fast out-of-band inspection, BMC networking config, BMC credentials
provisioning, and LDAP integration — detailed under
[Dependency on forthcoming Metal3 APIs](#dependency-on-forthcoming-metal3-apis).

These are forthcoming features; this proposal is written **against
the capabilities they describe**, not a frozen API surface. The illustrative YAML
below is subject to the final Metal3 API shapes.

Hardware validation is performed by **creating the BareMetalHost with the shared
default credentials and letting BMO run fast out-of-band (Redfish) inspection**
(`spec.inspectionMode: fast`). The inspection results are checked against the
pool's expected vendor and, if any are declared, its hardware attributes
(`spec.hardwareAttributes` is optional — when omitted only the vendor check
applies): on a match the server continues onboarding (credential provisioning,
then the host is published to the O-RAN inventory); on a mismatch the controller
deletes the BMH and marks that server's onboarding attempt failed. While a BMH is
still being onboarded it carries an **inventory-hold marker** so it is not surfaced
to the O-RAN/O2 IMS inventory until the host is published (the `Publishing` state).

---

## Architecture Diagram

```text
                                    Hub Cluster
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                                                                 │
│  Onboarding Pod (separate Deployment)                                           │
│  ┌───────────────────────────────────┐                                          │
│  │  Onboarding Service Container     │                                          │
│  │                                   │                                          │
│  │  ┌─────────────────────────────┐  │                                          │
│  │  │ OnboardingRequest           │  │                                          │
│  │  │ Controller                  │  │                                          │
│  │  │ - Create inventory hierarchy│  │                                          │
│  │  │ - Create OnboardingServer   │  │                                          │
│  │  │   CRs from server list     │  │                                          │
│  │  │ - Update status counts     │  │                                          │
│  │  └─────────────────────────────┘  │                                          │
│  │                                   │                                          │
│  │  ┌─────────────────────────────┐  │                                          │
│  │  │ OnboardingServer            │  │                                          │
│  │  │ Controller                  │  │                                          │
│  │  │ - State machine per server  │  │                                          │
│  │  │ - Create/patch BMH + Metal3 │  │                                          │
│  │  │   BMC-config CRs & Secrets  │  │                                          │
│  │  │ - Watch BMH conditions      │  │                                          │
│  │  └─────────────────────────────┘  │                                          │
│  └───────────────────────────────────┘                                          │
│            │                                                                     │
│            │ K8s API only (no Redfish/BMC traffic from this pod)                 │
│            ▼                                                                     │
│  ┌─────────┼──────────────────────────────────────────────────────────────────┐  │
│  │  K8s Resources                                                             │  │
│  │                                                                            │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                    │  │
│  │  │ ExternalSecret│  │ K8s Secret   │  │ BMH CR       │                    │  │
│  │  │ (default     │─►│ (synced by   │  │ (onboarded-  │                    │  │
│  │  │  shared +    │  │  ESO)        │  │  <bmcAddr>)  │                    │  │
│  │  │  per-server  │  │              │  │ inspected    │                    │  │
│  │  │  desired)    │  │              │  │ (fast/OOB),  │                    │  │
│  │  │              │  │              │  │ inventory-   │                    │  │
│  │  │              │  │              │  │ hold marker  │                    │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘                    │  │
│  │                                                                            │  │
│  │  Resource Pool Namespace (namespace = resourcePoolName)                    │  │
│  └────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                 │
│  ┌───────────────────────────────────────────────────────────────────────────┐   │
│  │  Inventory Hierarchy CRs (namespaced, in oran-o2ims namespace)          │   │
│  │                                                                           │   │
│  │  Location CR ──► OCloudSite CR ──► ResourcePool CR                        │   │
│  │  (must exist)    (created)         (created, name = BMH namespace)         │   │
│  └───────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
│  ┌──────────────────┐  ┌──────────────┐                                        │
│  │ External Secrets │  │ Metal3 / BMO │                                        │
│  │ Operator (ESO)   │  │ (Ironic):    │                                        │
│  │ + Vault Config   │  │ inspect+BMC  │                                        │
│  └──────────────────┘  └──────────────┘                                        │
│         ▲                                                                       │
│         │ Prerequisites (pre-configured)                                        │
└─────────┼───────────────────────────────────────────────────────────────────────┘
          │
══════════╪═══════════════════════════════════════════════════ Network Boundary
          │
          │           Remote BMC Subnet (e.g. 10.10.10.0/24)
          │    ┌──────────────────────────────────────────────────────────┐
          │    │                                                          │
          │    │  ┌──────────┐  ┌──────────┐  ┌──────────┐               │
          └────┼─►│  BMC     │  │  BMC     │  │  BMC     │  ...          │
 Redfish from  │  │  Server 1│  │  Server 2│  │  Server 3│               │
 Metal3/BMO    │  │          │  │          │  │          │               │
               │  │ Dell     │  │ Dell     │  │ Dell     │               │
               │  │ R750     │  │ R750     │  │ R750     │               │
               │  └─────┬────┘  └─────┬────┘  └─────┬────┘               │
               │        │             │              │                    │
               │        │  LDAPS (authMode=ldap)     │                    │
               │        └─────────────┼──────────────┘                    │
               │                      ▼                                   │
               │               ┌──────────────┐                           │
               │               │ LDAP Server  │                           │
               │               │ (external)   │                           │
               │               └──────────────┘                           │
               │                                                          │
               │  Homogeneous Resource Pool                               │
               └──────────────────────────────────────────────────────────┘
```

The server onboarding state machine is shown in full in [section 2](#2-server-onboarding-state-machine), with the detailed per-state behavior in the [implementation companion](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior).

---

## 1. API Design (CRDs)

The onboarding domain is modeled as Kubernetes CRDs in the
`onboarding.openshift.io` API group — these are the source of truth, and the
controllers reconcile the workflow from them. Clients can interact directly via
`oc`/`kubectl` or the Kubernetes API. In addition, the service exposes an
O-RAN-style **REST API** ([REST API](#rest-api), below) that follows the same
conventions as the other O2 IMS services in this repository. There is **no
database**: the REST layer is a thin facade over the CRDs (the same pattern used
by the artifacts and provisioning servers), so both interfaces read and write the
same CR state.

### OnboardingRequest CRD

Cluster-scoped. One per resource pool.

```go
type OnboardingRequestSpec struct {
    // Inventory hierarchy
    GlobalLocationName string `json:"globalLocationName"` // Parent Location CR (must exist)
    OCloudSiteName     string `json:"oCloudSiteName"`     // OCloudSite (created if not found)
    ResourcePoolName   string `json:"resourcePoolName"`   // ResourcePool (created if not found, also BMH namespace)

    // Descriptions applied to the OCloudSite / ResourcePool CRs when the
    // controller creates them. OCloudSiteSpec.description and
    // ResourcePoolSpec.description are required fields on those CRDs, so the
    // controller must supply a value. If left empty, the controller defaults
    // each to "Created by onboarding request <name>". Ignored when the
    // OCloudSite / ResourcePool already exists.
    OCloudSiteDescription   string `json:"oCloudSiteDescription,omitempty"`
    ResourcePoolDescription string `json:"resourcePoolDescription,omitempty"`

    // Default credentials (factory defaults, shared across pool — same vendor ships same defaults)
    DefaultCredentialsVaultSecret string `json:"defaultCredentialsVaultSecret"`

    // Authentication mode
    AuthMode string `json:"authMode,omitempty"` // "local" (default) or "ldap"

    // LDAP configuration (required when authMode is "ldap")
    LdapConfig *LdapConfig `json:"ldapConfig,omitempty"`

    // Hardware matching. BmcVendor serves two purposes: it selects the default
    // bmcAddressTemplate (see the vendor defaults table) and it is matched against
    // the inspected SystemVendor.Manufacturer during Inventorying (see "Vendor
    // matching" below for the comparison semantics — a case-insensitive substring
    // test, so "Dell" matches the inspected "Dell Inc.").
    BmcVendor          string              `json:"bmcVendor"`                    // e.g. "Dell", "HPE", "Lenovo"
    HardwareAttributes []HardwareAttribute `json:"hardwareAttributes,omitempty"` // All must pass (AND); see matching approach below

    // Firmware / BIOS baseline. Optional. Names an existing HardwareProfile CR
    // (clcm.openshift.io/v1alpha1, resolved in the O-Cloud manager namespace)
    // whose desired BIOS attributes and firmware versions are applied uniformly
    // to every server in the pool during the ApplyingHardwareProfile state,
    // before the BMC is reconfigured or published. When empty, no firmware/BIOS
    // baseline is applied and the state is skipped (Inventorying transitions
    // straight to PinningBmcAddress). Pool-uniform, not per-server: the pool is
    // homogeneous, so one profile fits every server. See "Hardware profile
    // (firmware and BIOS baseline)" below.
    HardwareProfileName string `json:"hardwareProfileName,omitempty"`

    // BMH configuration
    BmcAddressTemplate string `json:"bmcAddressTemplate,omitempty"` // Optional override. If empty, derived from bmcVendor (see vendor defaults below)

    // BMC management network (optional, pool-level). All BMCs in a homogeneous
    // pool share one management subnet, so these are single values, not per-server.
    // When set, they are passed to the Metal3 BMC networking config API
    // as the static subnet/gateway for the DHCP-to-static
    // conversion in the PinningBmcAddress state, and they enable a pre-mutation
    // webhook check that every server's bmcAddress falls inside bmcSubnet. When
    // unset, BMO derives the static mask/gateway from the BMC's current DHCP lease.
    // Supplying them is recommended: vendor DHCP-lease reporting is inconsistent
    // (the gateway in particular is often absent), so an explicit subnet/gateway
    // removes a silent failure mode.
    BmcSubnet  string `json:"bmcSubnet,omitempty"`  // Management subnet in CIDR form, e.g. "10.10.10.0/24"
    BmcGateway string `json:"bmcGateway,omitempty"` // Management gateway, e.g. "10.10.10.1"

    // DisableBmcCertificateVerification controls BMO's verification of each BMC's
    // serving certificate (set as spec.bmc.disableCertificateVerification on every
    // BMH in the pool). Defaults to false — BMO verifies the certificate. Set true
    // ONLY as an explicit exception for a pool whose BMCs present self-signed
    // certificates with no trusted CA available; disabling verification exposes the
    // management path to BMC impersonation and credential interception. See
    // "Security Considerations".
    DisableBmcCertificateVerification bool `json:"disableBmcCertificateVerification,omitempty"`

    // No provisioning-network fields. Hardware validation uses fast out-of-band
    // (Redfish) inspection (spec.inspectionMode: fast), which queries the BMC
    // directly and never boots the host over a provisioning network, so no DHCP/
    // PXE/nmstate configuration is required to onboard a server. (A provisioning
    // network is still needed later, by the normal provisioning flow, when the
    // published host is actually deployed — but that is outside onboarding.)

    // Servers to onboard — each entry declares a BMC endpoint reachable at its
    // current DHCP-assigned address
    Servers []ServerSpec `json:"servers"`
}

type ServerSpec struct {
    // BmcAddress is the IP address of the BMC. Prior to onboarding the BMC obtains
    // this address from DHCP; after validation the controller pins the BMC NIC to
    // static, reusing this same address (see the PinningBmcAddress state), so it
    // remains stable. Used in the bmcAddressTemplate to construct the full BMH
    // address. Can be IPv4 (e.g. "10.10.10.101") or IPv6 (e.g. "fd00:bmc::101").
    BmcAddress string `json:"bmcAddress"`

    // DesiredCredentialsVaultSecret is the vault secret name containing the target
    // BMC credentials for this specific server. Each server gets unique credentials.
    // Required when authMode is "local". Not used when authMode is "ldap".
    DesiredCredentialsVaultSecret string `json:"desiredCredentialsVaultSecret,omitempty"`
}

type HardwareAttribute struct {
    // Name is a pool-uniform hardware attribute matched identically against every
    // server. It is compared against the fields of the Metal3 inspection result
    // (BareMetalHost/HardwareData), not raw Redfish. Allowed: model, cpuArch,
    // cpuModel (string); cpuCount, memoryGiB, nicCount, diskCount (integer).
    // Per-server unique identifiers (serialNumber, uuid) are NOT valid here — they
    // are recorded in OnboardingServer status but cannot be used as match criteria,
    // since every server would report a different value and all but one would fail
    // HwMismatch. Vendor matching is expressed via spec.bmcVendor, not here. See
    // "Hardware attribute matching" below for the canonical attribute catalog,
    // units, and comparison semantics.
    Name string `json:"name"`
    // Operator selects the comparison. Defaults to "Equal". "GreaterThanOrEqual"
    // and "LessThanOrEqual" are valid for integer attributes only (e.g. require
    // at least a given amount of memory); the webhook rejects them for string
    // attributes.
    Operator string `json:"operator,omitempty"`
    // Value is compared in the attribute's canonical unit (see catalog): a decimal
    // integer for integer attributes (e.g. "512" GiB), or a case-insensitive
    // string for string attributes.
    Value string `json:"value"`
}

type LdapConfig struct {
    // ServiceAddresses is a list of LDAP server URIs (e.g. "ldaps://ldap.example.com:636")
    ServiceAddresses []string `json:"serviceAddresses"`
    // BindDnVaultSecret is the vault secret name containing the LDAP bind DN and password
    BindDnVaultSecret string `json:"bindDnVaultSecret"`
    // BaseDn is the base distinguished name for LDAP searches
    BaseDn string `json:"baseDn"`
    // UsernameAttribute is the LDAP attribute used for username matching (e.g. "uid")
    UsernameAttribute string `json:"usernameAttribute,omitempty"`
    // GroupsAttribute is the LDAP attribute used for group membership (e.g. "memberOf")
    GroupsAttribute string `json:"groupsAttribute,omitempty"`
    // RemoteRoleMapping maps LDAP groups to BMC roles
    RemoteRoleMapping []RemoteRoleMapping `json:"remoteRoleMapping"`
    // LdapCredentialsVaultSecret is the vault secret name containing the LDAP user
    // credentials used for BMH access (username/password for an LDAP account with
    // BMC admin role). This is used for the BMH credential Secret.
    LdapCredentialsVaultSecret string `json:"ldapCredentialsVaultSecret"`
    // DisableLocalAccounts disables all local BMC accounts as a final hardening
    // step, applied in the DisablingLocalAccounts state that runs after Publishing (once the
    // BMH is published and LDAP is confirmed working end-to-end), immediately
    // before Completed. This is irreversible — once local accounts are disabled
    // there is no non-LDAP path to re-enable them. Deferring it past Publishing
    // keeps the ConfiguringLdap step reversible. Best-effort: a failure does not
    // fail the server, which stays reachable over LDAP. Defaults to false.
    DisableLocalAccounts bool `json:"disableLocalAccounts,omitempty"`
}

type RemoteRoleMapping struct {
    RemoteGroup string `json:"remoteGroup"` // e.g. "cn=bmc-admins,ou=groups,dc=example,dc=com"
    LocalRole   string `json:"localRole"`   // e.g. "Administrator"
}

type OnboardingRequestStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Summary counts — updated by the OnboardingRequest controller on each reconciliation.
    // The controller lists OnboardingServer CRs in the resource pool namespace filtered by
    // spec.onboardingRequestName, and counts by status.state:
    //   ServersTotal       = len(spec.servers)
    //   ServersPending     = count where state == "Pending" or unset (an
    //                        OnboardingServer created but not yet picked up —
    //                        status is a subresource, so a freshly created CR
    //                        has an empty state until its first reconcile),
    //                        plus server entries for which no OnboardingServer
    //                        CR exists yet
    //   ServersInProgress  = count where state is non-terminal and not Pending
    //                        (CreatingExternalSecrets, Inspecting, Inventorying,
    //                         ApplyingHardwareProfile, PinningBmcAddress,
    //                         UpdatingCredentials, ConfiguringLdap, Publishing,
    //                         DisablingLocalAccounts)
    //   ServersCompleted   = count where state == "Completed"
    //   ServersFailed      = count where state in terminal failure states
    //                        (VendorMismatch, AuthFailed, HwMismatch, Failed)
    // Invariant: ServersPending + ServersInProgress + ServersCompleted +
    //   ServersFailed == ServersTotal at all times (Pending absorbs both
    //   not-yet-created and not-yet-started servers so the counts always add up).
    // The controller watches OnboardingServer CRs via an index on spec.onboardingRequestName
    // and re-reconciles the parent OnboardingRequest on any OnboardingServer state change.
    ServersTotal       int `json:"serversTotal,omitempty"`
    ServersPending     int `json:"serversPending,omitempty"`
    ServersInProgress  int `json:"serversInProgress,omitempty"`
    ServersCompleted   int `json:"serversCompleted,omitempty"`
    ServersFailed      int `json:"serversFailed,omitempty"`

    // Servers is a compact per-server summary, one entry per OnboardingServer,
    // refreshed by the controller on any child state change (it already watches
    // OnboardingServer CRs via the index on spec.onboardingRequestName). It carries
    // only lightweight status fields — heavy data such as hardwareDetails is
    // deliberately NOT copied here. This is the complete per-server view the REST
    // API returns: the facade serves GET onboardingRequests/{id} straight from this
    // status and never reads the OnboardingServer CRs. hardwareDetails is inventory
    // data (authoritative source: the O2 IMS Resource derived from the BMH) and is
    // not surfaced through the onboarding API; it stays on the OnboardingServer CR
    // for oc-based inspection.
    Servers []OnboardingServerSummary `json:"servers,omitempty"`
}

// OnboardingServerSummary is the compact per-server record embedded in
// OnboardingRequestStatus. It intentionally excludes hardwareDetails and other
// heavy or internal per-server fields (e.g. retryCount, vendor), which remain on
// the OnboardingServer CR. Name is the OnboardingServer CR handle; because it is
// derived from the server's bmcAddress it also identifies which spec.servers
// entry the record corresponds to, so no separate bmcAddress field is needed. The
// inspected vendor is omitted too: it is declared per request in spec.bmcVendor,
// and a mismatch surfaces as the terminal VendorMismatch state with detail in
// Message, so a per-server vendor field would add nothing a client acts on.
type OnboardingServerSummary struct {
    Name    string `json:"name"`              // OnboardingServer CR name (BMC-address-derived)
    State   string `json:"state"`             // current state-machine state
    Message string `json:"message,omitempty"` // status.stateMessage — never credential values
}
```

**Printcolumns:** Total (`serversTotal`), Pending (`serversPending`), Completed
(`serversCompleted`), In-Progress (`serversInProgress`), Failed (`serversFailed`),
Age — the summary row shown by `oc get onboardingrequest` in the
[Walkthrough](server-onboarding-implementation.md#10-walkthrough).

**Validation (webhook or CEL rules):**

- `servers` must not be empty
- Each `bmcAddress` must be a valid IPv4 or IPv6 address
- `bmcAddress` must be unique within the `servers` list
- **All `bmcAddress` values must be the same address family** (all IPv4 or all
  IPv6). A homogeneous pool shares one BMC management subnet, and `bmcSubnet` /
  `bmcGateway` are single values; allowing mixed families would make the
  `bmcSubnet`/`bmcGateway` recovery path (see [Spec mutability](#1-api-design-crds))
  impossible — no single CIDR can contain both families, so the per-server-inside-
  `bmcSubnet` check would reject every update and, once a server has published, the
  deletion guard would block recreating the request. Dual-stack pools would be a
  separate OnboardingRequest per family.
- `bmcSubnet`, if set, must be a valid CIDR; `bmcGateway`, if set, must be a valid
  IP address; setting either requires the other, `bmcGateway` must fall inside
  `bmcSubnet`, and both must be of the same address family (IPv4/IPv6) as the
  servers' `bmcAddress` values
- When `bmcSubnet` is set, every server's `bmcAddress` must fall inside it (a
  pre-mutation catch for mistyped or mis-provisioned addresses); this also fixes
  the address family the `PinningBmcAddress` pin uses (IPv4 vs IPv6 — see the
  [`PinningBmcAddress` state](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior))
- `metadata.name` (the OnboardingRequest name, folded into the per-server
  ExternalSecret/Secret names) must be short enough that
  `<onboardingRequestName>-<serverName>-desired-creds` stays within the
  253-character limit for any valid `bmcAddress` stem (see the length budget in
  [OnboardingServer CR naming](#1-api-design-crds))
- `resourcePoolName` must be unique across all OnboardingRequests — only one
  OnboardingRequest per resource pool. The validating webhook enforces this on a
  best-effort basis by listing existing OnboardingRequests, but a webhook cannot
  guarantee global uniqueness under concurrent admissions (two requests naming the
  same pool can pass admission simultaneously — a TOCTOU race). The
  OnboardingRequest controller therefore re-checks on reconciliation: if it finds
  another OnboardingRequest already owning the same `resourcePoolName`, the later
  one (by `creationTimestamp`, then name) is set `Ready=False,
  Reason=ResourcePoolConflict` and does not create any resources
- `authMode`, if set, must be `"local"` or `"ldap"` (defaults to `"local"` when empty)
- When `authMode` is `"local"` (default), `desiredCredentialsVaultSecret` is required on every server entry
- When `authMode` is `"ldap"`, `ldapConfig` is required (including `serviceAddresses`, `bindDnVaultSecret`, `baseDn`, `remoteRoleMapping`, and `ldapCredentialsVaultSecret`)
- Each `hardwareAttributes[].name` must be from the canonical catalog (`model`,
  `cpuArch`, `cpuModel`, `cpuCount`, `memoryGiB`, `nicCount`, `diskCount` — see
  [Hardware attribute matching](#1-api-design-crds)). Per-server identifiers
  (`serialNumber`, `uuid`) are rejected — matching a homogeneous pool against a
  unique value would fail every server but one
- `hardwareAttributes[].operator`, if set, must be `Equal`, `GreaterThanOrEqual`, or `LessThanOrEqual`; `GreaterThanOrEqual`/`LessThanOrEqual` are rejected for string attributes (`model`, `cpuArch`, `cpuModel`)
- Each `hardwareAttributes[].value` must parse as a decimal integer for integer attributes (`cpuCount`, `memoryGiB`, `nicCount`, `diskCount`)
- `hardwareProfileName`, if set, should reference an existing `HardwareProfile` CR
  in the O-Cloud manager namespace. The webhook verifies existence on a best-effort
  basis (the same check the provisioning flow already performs for node-group
  profiles); a delete/create race is re-checked by the controller in the
  `ApplyingHardwareProfile` state, which fails the server if the profile is missing
  at apply time

**BMC address template defaults by vendor:**

If `bmcAddressTemplate` is not specified, it is derived from `bmcVendor`:

| Vendor | Default template |
|--------|-----------------|
| Dell | `idrac-virtualmedia+https://{{.IP}}/redfish/v1/Systems/System.Embedded.1` |
| HPE | `redfish-virtualmedia+https://{{.IP}}/redfish/v1/Systems/1` |
| Lenovo | `redfish-virtualmedia+https://{{.IP}}/redfish/v1/Systems/1` |

The template is a Go `text/template` string with a single binding: **`.IP`**,
the server's `bmcAddress`. When that address is IPv6 it is substituted in
bracketed form (e.g. `[fd00:bmc::101]`) so the rendered Redfish URL is valid (see
[IPv6 address handling](server-onboarding-implementation.md#5-metal3-bmc-configuration)). No other template variables are
available.

If `bmcVendor` does not match a known vendor, `bmcAddressTemplate` is required — the webhook rejects creation without it. The `bmcAddressTemplate` field can always be set explicitly to override the vendor default.

**Spec mutability:**

The OnboardingRequestSpec is partially immutable after creation:

- **Immutable fields**: All fields except `servers`, `bmcSubnet`, and `bmcGateway`
  — changing credentials, hardware attributes, vendor, resource pool, or the
  `hardwareProfileName` firmware/BIOS baseline while servers are being processed
  would cause inconsistent state (servers already onboarded were brought to the old
  baseline).
- **Updatable BMC network settings (`bmcSubnet`, `bmcGateway`)**: these may be
  corrected after creation. They are consumed **only** at `PinningBmcAddress` (as the
  static subnet/gateway), so an update affects only servers **not yet pinned** — it
  never re-pins or disturbs a server already past `PinningBmcAddress`. This is the
  recovery path for the failure mode where a server lands in `Failed` at
  `PinningBmcAddress` because the BMC's DHCP lease omitted the gateway and no
  `bmcSubnet`/`bmcGateway` was supplied (see
  [Hardware attribute matching / pinning](#1-api-design-crds)): the operator sets or
  corrects `bmcSubnet`/`bmcGateway` on the existing OnboardingRequest and retries the
  failed server (delete its OnboardingServer; it is recreated as `Pending`) —
  **without** recreating the whole request, which the deletion guard would block once
  any server has published. The webhook re-validates the CIDR/gateway/family rules on
  update.
- **Append-only `servers` list**: New server entries may be appended to the `servers` list. Existing entries cannot be modified or removed. This allows adding servers to an onboarding request incrementally without requiring a new CR.

The webhook validates on update:

1. All fields other than `servers`, `bmcSubnet`, and `bmcGateway` are unchanged.
2. The existing server entries (by index) are identical to the previous spec.
3. Any new entries at the end of the list pass the same validation rules as at creation time.
4. New `bmcAddress` values do not duplicate existing entries.
5. Updated `bmcSubnet`/`bmcGateway` still satisfy the CIDR/gateway/family rules, and every server's `bmcAddress` still falls within `bmcSubnet`.

**OnboardingServer CR naming:** The CR name must be a deterministic, unique,
RFC-1123-valid label derived from the BMC address. A naive "replace `.` and `:`
with `-`" is **not** collision-free: `.` and `:` both map to `-` (so
`10.10.10.101` and `10:10:10:101` would both become `10-10-10-101`), IPv6 allows
uppercase hex (invalid in Kubernetes names), and compressed or leading/trailing
`::` can yield leading/trailing hyphens (also invalid). The derivation therefore:

1. **Canonicalizes** the address — parse it and re-emit in canonical form
   (RFC 5952 for IPv6: lowercase, fully compressed) so equivalent spellings map
   to one name.
2. **Builds a readable stem** by lowercasing and replacing `.`/`:` with `-`.
3. **Prefixes the address family** (`v4-`/`v6-`) — this disambiguates IPv4 from
   IPv6 and guarantees the name starts with an alphanumeric character.
4. **Appends a short hash suffix** — the first 8 hex characters of a SHA-256 over
   the canonical address (e.g. `-a1b2c3d4`). This guarantees uniqueness even when
   the stem is truncated (see the length budget below), and avoids any
   trailing-hyphen edge case.

For example, `10.10.10.101` → `v4-10-10-10-101-<hash>` and `fd00:bmc::101` →
`v6-fd00-bmc--101-<hash>`. The result is deterministic and unique within the
resource pool namespace. (For brevity, the examples elsewhere in this document
show only the readable stem, e.g. `10-10-10-101`, and omit the `v4-`/`v6-` prefix
and hash suffix.)

**Length budget.** The 253-character limit applies to every object the name
seeds, not just the OnboardingServer CR — the same derived stem is also
wrapped to name the BMH (`onboarded-<bmcAddress>`) and, most restrictively, the
per-server ExternalSecret/Secret
(`<onboardingRequestName>-<serverName>-desired-creds`). The
stem is therefore truncated against the **longest** of these wrappers, not against
253 directly: the budget for the stem is `253 − maxAffix`, where `maxAffix` sums
**every** fixed part wrapped around the stem for the longest (per-server Secret)
name — `len(onboardingRequestName) + 1` (the `-` join) + `len("v4-")` (the address-
family prefix, 3) + `len("-<hash>")` (the `-` plus 8 hex chars, 9) +
`len("-desired-creds")` (14) — so the prefix and hash that keep the name unique and
valid are reserved, not just named. (For a request name of length _R_ that leaves a
stem budget of `253 − (R + 1) − 3 − 9 − 14 = 226 − R` characters.)
Because the per-server Secret name folds in `onboardingRequestName`, the webhook
also bounds `onboardingRequestName` so that this composed name cannot exceed 253
for any valid `bmcAddress` stem (a request name is normally short — a few tens of
characters — so this bound is not restrictive in practice). This guarantees every
name the service generates from a server entry stays within the limit.

**Homogeneous resource pool assumption:**

All servers in a resource pool are homogeneous — same vendor, same hardware model, same factory-default credentials. This means:

- A single OnboardingRequest covers an entire resource pool
- The `hardwareAttributes` and `bmcVendor` apply uniformly to every server
- The optional `hardwareProfileName` firmware/BIOS baseline is likewise pool-uniform — one profile applies to every server (see [Hardware profile (firmware and BIOS baseline)](#1-api-design-crds))
- Servers that do not match are rejected (terminal `VendorMismatch` or `HwMismatch` state)
- All servers share the same factory-default BMC credentials for initial access
- Each server receives unique target credentials (local mode) or shared LDAP configuration (LDAP mode)
- **Network configuration is pool-level, not per-server.** The BMC management
  network (`bmcSubnet`, `bmcGateway`) is a single value on the OnboardingRequest,
  applied identically to every server. This is a deliberate design decision: a
  resource pool is a homogeneous rack/site block wired to one BMC management
  network, so per-server network settings would be redundant and error-prone. The
  only per-server network value is each server's own `bmcAddress`. (There is no
  provisioning-network configuration at all — fast out-of-band inspection never
  boots the host over a provisioning network; see
  [Hardware attribute matching](#1-api-design-crds).) If a deployment ever needs
  heterogeneous per-server networking, that would be a separate OnboardingRequest
  per network domain (one request per pool already being the model), not per-server
  fields.

**Hardware attribute matching:**

Matching is performed against the **Metal3 inspection result** — the
`HardwareData` CR (and BMH `status.hardwareDetails`) that BMO produces when the
BMH is inspected during the `Inspecting` state (see
[Hardware Inventory and BMH Creation](server-onboarding-implementation.md#6-hardware-inventory-and-bmh-creation)).
Inspection runs in **fast out-of-band (Redfish) mode** (`spec.inspectionMode:
fast`), so BMO populates `HardwareData` from the BMC rather than from a booted
agent, but the CR shape and the match logic are identical — consuming the
normalized `HardwareData` still gives a single source of truth for every attribute.
Each catalog attribute maps to a `HardwareData` field; the controller compares it
against the matching `hardwareAttributes` entry using the per-attribute `operator`.
Field paths below use Go struct notation (`HardwareData.Spec.HardwareDetails.…`);
the equivalent CR/YAML path is `spec.hardware.…` (the `HardwareDetails` field
carries the JSON tag `hardware`).

> **Caveat — fast inspection may report fewer details.** Out-of-band Redfish
> inspection can return a smaller or less granular set of hardware facts than
> agent-based inspection, and coverage varies by vendor/BMC firmware (e.g. storage
> and NIC enumeration are less consistent over Redfish than from the in-band
> agent). The catalog below is retained in full, but a pool's `hardwareAttributes`
> should be limited to fields its BMCs actually populate under fast inspection.
> **TODO:** verify per-vendor (Dell/HPE/Lenovo) which `HardwareData` fields fast
> inspection reliably fills; an attribute the BMC does not report reads as
> missing/zero and would fail the match. Vendor (`SystemVendor.Manufacturer`) and
> model (`SystemVendor.ProductName`) are reported by all supported BMCs.

Canonical attribute catalog (the only valid `name` values):

| `name` | Type | Canonical unit | Derived from inspection (`HardwareData.Spec.HardwareDetails`) |
|---|---|---|---|
| `model` | string | trimmed, case-insensitive | `SystemVendor.ProductName` |
| `cpuArch` | string | trimmed, case-insensitive (e.g. `x86_64`) | `CPU.Arch` |
| `cpuModel` | string | trimmed, case-insensitive | `CPU.Model` |
| `cpuCount` | integer | logical processor count | `CPU.Count` |
| `memoryGiB` | integer | GiB, rounded to nearest integer | `round(RAMMebibytes / 1024)` (`HardwareData` reports RAM in MiB) |
| `nicCount` | integer | number of NICs | `len(NIC)` |
| `diskCount` | integer | number of storage devices | `len(Storage)` |

Comparison semantics:

- **String attributes** (`model`, `cpuArch`, `cpuModel`) — trimmed and compared
  case-insensitively. Only `operator: Equal` (the default) is allowed; the webhook
  rejects `GreaterThanOrEqual`/`LessThanOrEqual` for these.
- **Integer attributes** (`cpuCount`, `memoryGiB`, `nicCount`, `diskCount`) — both
  sides are parsed as integers in the canonical unit and compared numerically, so
  `"512"` and an inspection-reported `524288 MiB` (→ `512 GiB`) compare equal. `operator`
  selects `Equal` (default), `GreaterThanOrEqual`, or `LessThanOrEqual`. Using
  `GreaterThanOrEqual` expresses the usual intent for capacity — "at least this
  much" — and tolerates minor per-unit variance better than exact equality.
- All listed `hardwareAttributes` must pass (logical AND). The value the
  controller read for each attribute from the inspection result is recorded in
  `status.hardwareDetails` (see below) so a `HwMismatch` is diagnosable — the
  `status.stateMessage` names the attribute, the operator, the expected value, and
  the actual inspected value.

**Vendor matching:** The vendor is matched separately from `hardwareAttributes`,
via `spec.bmcVendor` against the inspected
`HardwareData.Spec.HardwareDetails.SystemVendor.Manufacturer`. The comparison is a
**case-insensitive substring test**: the match succeeds when the trimmed,
lowercased `bmcVendor` is a substring of the trimmed, lowercased `Manufacturer`.
This is deliberately lenient so that a short catalog value tolerates the
suffixes/qualifiers vendors add to the reported manufacturer string — e.g.
`bmcVendor: Dell` matches the inspected `Dell Inc.`, while `HPE` and `Lenovo`
match the `HPE` and `Lenovo` their BMCs report verbatim. It is **not** the
trimmed-equality rule used for the string `hardwareAttributes` (`model`,
`cpuArch`, `cpuModel`), which must match exactly (case-insensitively). Note that
`bmcVendor` also selects the default `bmcAddressTemplate` (see the vendor defaults
table in [API Design (CRDs)](#1-api-design-crds)); use the short catalog form (`Dell`,
not `Dell Inc.`) so both uses resolve correctly. A failing vendor check is
terminal `VendorMismatch`; `status.stateMessage` records the expected `bmcVendor`
and the inspected `Manufacturer`.

**Hardware profile (firmware and BIOS baseline):**

`spec.hardwareProfileName` optionally brings every server in the pool to a desired
firmware and BIOS baseline as part of onboarding. It references an **existing
`HardwareProfile` CR** (`clcm.openshift.io/v1alpha1`, the same CRD the
provisioning flow already uses for node groups), resolved by name in the O-Cloud
manager namespace (`oran-o2ims` by default), where the other inventory CRs
(`Location`, `OCloudSite`, `ResourcePool`) also live. Nothing new is defined for
firmware/BIOS — the field reuses the existing CRD and the existing apply path.

The `HardwareProfileSpec` already carries exactly the two things needed:

- `bios.attributes` — desired BIOS settings as a `map[string]IntOrString`
  (name → value).
- `biosFirmware` / `bmcFirmware` (`{version, url}`) and `nicFirmware`
  (`[{version, url}]`) — desired firmware versions and the images to flash.

Because the pool is homogeneous (same vendor and model — see
[Homogeneous resource pool assumption](#1-api-design-crds)), a single profile
applies uniformly to every server; the reference is therefore pool-level, not
per-server. The profile is a **day-0 baseline applied once during onboarding**,
not ongoing day-2 firmware enforcement — that remains a provisioning / hardware
manager concern for hosts already allocated to clusters.

**How it is applied.** The controller does not write firmware or BIOS over
Redfish itself. It reuses the hardware manager's existing HardwareProfile →
Metal3 translation (`processHwProfile` and the `HostFirmwareSettings` /
`HostFirmwareComponents` / `HostUpdatePolicy` managers in
`internal/hardwaremanager/controller/`): the profile's BIOS attributes become a
`HostFirmwareSettings` CR, its firmware versions become a `HostFirmwareComponents`
CR (`updates: [{component, url}]`), and a `HostUpdatePolicy` CR gates when BMO
applies them. These CRs are linked to the BMH by the **same name/namespace**, so
they are created alongside the (still-held) BMH in the resource pool namespace.
BMO then services the host — including any reboots — to converge it. This all
happens in the dedicated [`ApplyingHardwareProfile` state](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior),
which runs after `Inventorying` (so firmware is never flashed onto a
vendor/hardware-mismatched box) and before `PinningBmcAddress` (so BMC firmware
updates, which can bounce the BMC, complete before the BMC NIC and credentials are
reconfigured). When `hardwareProfileName` is empty the state is skipped entirely.

**The BMH is held out of the O-RAN/O2 IMS inventory until the host is published**
— from the moment it is created for inspection, through matching and
credential provisioning — and is published at the `Publishing` state (before
`Completed`, and before `DisablingLocalAccounts` in the ldap + disableLocalAccounts path). This is enforced by the inventory-hold marker described in
[Hardware Inventory and BMH Creation](server-onboarding-implementation.md#6-hardware-inventory-and-bmh-creation);
inspection itself runs in the BMH `inspecting` provisioning state, which the
existing inventory pipeline already excludes, and the marker keeps the host
excluded during the later steps when the BMH would otherwise be `available`. A
server whose inspection does not match (`VendorMismatch`/`HwMismatch`) never
publishes — its held BMH is deleted.

**Inventory CR namespace:**

`Location`, `OCloudSite`, and `ResourcePool` are **namespace-scoped** CRDs
(`scope: Namespaced`) and live in the **O-Cloud manager namespace** — `oran-o2ims`
by default, resolved from the `OCLOUD_MANAGER_NAMESPACE` environment variable /
`--ocloud-manager-namespace` flag on the onboarding controller. This is a
**different** namespace from the resource pool namespace (named after
`resourcePoolName`) where BMHs, Secrets, and OnboardingServer CRs live. The
`ResourcePool` CR's `metadata.name` equals `resourcePoolName`, matching both the
BMH namespace name and the `resources.clcm.openshift.io/resourcePoolName` label
the BMHs carry — but the ResourcePool CR object itself resides in `oran-o2ims`,
not in the pool namespace.

**Resource provisioning on reconciliation:**

When an OnboardingRequest is first reconciled, the controller ensures the
inventory hierarchy exists. All three inventory CRs are created/looked up in the
O-Cloud manager namespace (`oran-o2ims`):

1. **Location CR** — must already exist in `oran-o2ims`. If not found, the OnboardingRequest is set to `Ready=False, Reason=LocationNotFound`.
2. **OCloudSite CR** — created in `oran-o2ims` with `metadata.name` =
   `oCloudSiteName`, `spec.globalLocationName`, and the required `spec.description`
   (from `oCloudSiteDescription`, or a generated default) if not found. If it
   **already exists** but its `spec.globalLocationName` differs from the request's,
   the OnboardingRequest is set to `Ready=False, Reason=OCloudSiteConflict` and no
   ResourcePool is created — otherwise the pool and its hosts would be filed under a
   different Location than the request names.
3. **ResourcePool CR** — created in `oran-o2ims` with `metadata.name` = `resourcePoolName`, `spec.oCloudSiteName`, and the required `spec.description` (from `resourcePoolDescription`, or a generated default) if not found.
4. **Namespace** — created matching `resourcePoolName` if it does not exist, for BMH, Secret, and OnboardingServer resources. (Distinct from `oran-o2ims`.)
5. **OnboardingServer CRs** — one created per entry in `spec.servers`, in the resource pool namespace, owned by the OnboardingRequest.

If the ResourcePool already exists but references a different OCloudSite, the OnboardingRequest is set to `Ready=False, Reason=ResourcePoolConflict`. (The analogous parent check one level up — an existing OCloudSite pointing at a different Location — yields `OCloudSiteConflict`, per step 2.)

When new servers are appended to the spec, the controller creates OnboardingServer CRs for the new entries on the next reconciliation.

### Example OnboardingRequests

The [Walkthrough](server-onboarding-implementation.md#10-walkthrough) uses a
local-mode request with no firmware baseline. The two variants below show the
other shapes: centralized BMC auth via LDAP, and an enforced firmware/BIOS
baseline via `hardwareProfileName`.

**LDAP mode** — no per-server `desiredCredentialsVaultSecret`; `ldapConfig` is
required. With `disableLocalAccounts: true` the state machine runs
`DisablingLocalAccounts` after `Publishing`, immediately before `Completed`:

```yaml
apiVersion: onboarding.openshift.io/v1alpha1
kind: OnboardingRequest
metadata:
  name: toronto-east-pool1
spec:
  globalLocationName: toronto-east
  oCloudSiteName: site-toronto-east
  resourcePoolName: pool-toronto-east-1
  defaultCredentialsVaultSecret: bmc/dell-r750-defaults
  authMode: ldap
  bmcVendor: Dell
  bmcSubnet: "10.10.10.0/24"
  bmcGateway: "10.10.10.1"
  ldapConfig:
    serviceAddresses:
      - "ldaps://ldap.example.com:636"
    bindDnVaultSecret: bmc/ldap-bind            # DN + password for LDAP bind
    baseDn: "ou=people,dc=example,dc=com"
    usernameAttribute: uid
    groupsAttribute: memberOf
    remoteRoleMapping:
      - remoteGroup: "cn=bmc-admins,ou=groups,dc=example,dc=com"
        localRole: Administrator
    ldapCredentialsVaultSecret: bmc/ldap-bmc-user   # LDAP account for BMH access
    disableLocalAccounts: true
  hardwareAttributes:
    - name: model
      value: "PowerEdge R750"
  servers:
    - bmcAddress: "10.10.10.101"
    - bmcAddress: "10.10.10.102"
    - bmcAddress: "10.10.10.103"
```

**Firmware/BIOS baseline** — local mode with `hardwareProfileName` set, which
activates the optional `ApplyingHardwareProfile` state (after `Inventorying`,
before `PinningBmcAddress`). Empty `hardwareProfileName` skips that state:

```yaml
apiVersion: onboarding.openshift.io/v1alpha1
kind: OnboardingRequest
metadata:
  name: toronto-east-pool1
spec:
  globalLocationName: toronto-east
  oCloudSiteName: site-toronto-east
  resourcePoolName: pool-toronto-east-1
  defaultCredentialsVaultSecret: bmc/dell-r750-defaults
  authMode: local
  bmcVendor: Dell
  bmcSubnet: "10.10.10.0/24"
  bmcGateway: "10.10.10.1"
  hardwareProfileName: dell-r750-baseline     # references a HardwareProfile CR
  hardwareAttributes:
    - name: model
      value: "PowerEdge R750"
  servers:
    - bmcAddress: "10.10.10.101"
      desiredCredentialsVaultSecret: bmc/pool1-server-101
    - bmcAddress: "10.10.10.102"
      desiredCredentialsVaultSecret: bmc/pool1-server-102
    - bmcAddress: "10.10.10.103"
      desiredCredentialsVaultSecret: bmc/pool1-server-103
```

`hardwareProfileName` references an existing `HardwareProfile`
(`clcm.openshift.io/v1alpha1`) in the O-Cloud manager namespace. It is applied
by reusing the hardware manager's existing translation into Metal3
`HostFirmwareSettings` / `HostFirmwareComponents` / `HostUpdatePolicy`:

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: HardwareProfile
metadata:
  name: dell-r750-baseline
  namespace: oran-o2ims
spec:
  biosFirmware:
    version: "1.15.2"
    url: "http://fw.example.com/dell/r750/bios-1.15.2.exe"
  bmcFirmware:
    version: "7.10.30.00"
    url: "http://fw.example.com/dell/r750/idrac-7.10.30.exe"
  bios:
    attributes:
      BootMode: Uefi
      SysProfile: PerfOptimized
      LogicalProc: Disabled
```

### OnboardingServer CRD

Namespace-scoped (in the resource pool namespace). One per server entry in the OnboardingRequest. Owned by the OnboardingRequest (owner reference for garbage collection).

```go
type OnboardingServerSpec struct {
    OnboardingRequestName        string `json:"onboardingRequestName"` // Parent OnboardingRequest name
    BmcAddress                   string `json:"bmcAddress"`
    DesiredCredentialsVaultSecret string `json:"desiredCredentialsVaultSecret,omitempty"` // Per-server (local mode)
}

type OnboardingServerStatus struct {
    // Conditions carries standard metav1 conditions. The controller sets a
    // "LocalAccountsDisabled" condition in the DisablingLocalAccounts state
    // (ldap + disableLocalAccounts) recording whether the best-effort hardening
    // actually completed, so an incomplete hardening stays observable even though
    // the server still reaches Completed (see the DisablingLocalAccounts state in
    // the implementation document).
    Conditions      []metav1.Condition `json:"conditions,omitempty"`
    State           string             `json:"state"`                     // Current state machine state
    StateMessage    string             `json:"stateMessage,omitempty"`    // Detail on failure
    Vendor          string             `json:"vendor,omitempty"`
    Model           string             `json:"model,omitempty"`
    SerialNumber    string             `json:"serialNumber,omitempty"`
    HardwareDetails map[string]string  `json:"hardwareDetails,omitempty"` // Normalized canonical match attributes (see Hardware attribute matching)
    BmhName         string             `json:"bmhName,omitempty"`
    RetryCount      int                `json:"retryCount,omitempty"`
    LastAttemptAt   *metav1.Time       `json:"lastAttemptAt,omitempty"`

    // MutationProgress records every BMC-mutation operation the controller has
    // requested and how far it got — independent of State. When a mutation fails,
    // State collapses to "Failed" and no longer says which operation was in flight;
    // the finalizer reads MutationProgress (not State) to select the correct
    // rollback, including for a partially applied operation. Each entry is written
    // before the request (phase Requested) and updated when BMO reports the outcome
    // (Applied / Failed), and again when cleanup reverts it (RolledBack).
    MutationProgress []MutationOp `json:"mutationProgress,omitempty"`

    // CleanupStatus is the outcome of the per-server finalizer's cleanup run, not a
    // record of what was mutated (that is MutationProgress).
    CleanupStatus   string             `json:"cleanupStatus,omitempty"`   // pending, completed, partial
}

// MutationOp is one BMC-mutation operation and how far it progressed. The finalizer
// uses it to decide what to roll back after a failure, since status.state is only
// the (possibly terminal) state-machine state.
type MutationOp struct {
    // Operation is the BMC mutation performed via a Metal3 BMC-config API:
    // "PinNetwork" (DHCP->static + DNS), "ProvisionCredentials" (local-mode
    // per-server credentials), "ConfigureLdap", or "DisableLocalAccounts".
    Operation string `json:"operation"`
    // Phase is how far it got: "Requested" (desired state declared on the BMH /
    // companion CR), "Applied" (BMO confirmed success), "Failed", or "RolledBack"
    // (reverted during cleanup). A "Requested" (but not "Applied") op is the
    // partially-applied case the rollback must still handle.
    Phase string `json:"phase"`
    UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}
```

**Printcolumns:** State, Vendor, BmcAddress, BMH, Age

### Deletion semantics

Deletion is gated on **live infrastructure**, not on OnboardingServer state: an
OnboardingRequest may be deleted once the resources it produced (clusters and
BMHs) have been torn down through the normal provisioning lifecycle. Gating on
OnboardingServer state instead would make a request permanently undeletable —
every `Completed` server sits "past Inventorying" forever, with no path to
removal even after its BMH is gone.

Because there is exactly one OnboardingRequest per resource pool and its BMHs
live in the namespace named after `resourcePoolName`, the request's BMHs are
identified by that namespace — no extra labelling is required. "Live
infrastructure" here means a **published** BMH: an `onboarded-*` BMH whose
inventory-hold marker has been removed (onboarding reached `Publishing`), which is
therefore visible to the O2 IMS inventory and may be allocated to clusters. A
still-**held** BMH (inventory-hold marker present — a server mid-onboarding) is
not live infrastructure; it is cleaned up automatically by the owning
OnboardingServer's finalizer during the GC cascade below. Deletion is enforced by
a **validating webhook** on delete:

- **Reject if any published BMH from this request still exists** — i.e. any
  `onboarded-*` BMH in the resource pool namespace (carrying the
  `resources.clcm.openshift.io/resourcePoolName: <poolName>` label the service
  already applies) that no longer has the inventory-hold marker. Published BMH CRs
  are live infrastructure and are **never** deleted by the onboarding service.
  (Held BMHs, by contrast, are deleted by the service — see
  [Cleanup mechanism](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior) — so they do not block
  request deletion.) Decommissioning a published host is a deliberate,
  operator-driven sequence:
  1. **Deprovision any clusters** using those hosts. Deprovisioning does **not**
     delete the BMH — the hardware manager deallocates it (`deallocateBMH` in
     `baremetalhost_manager.go`) and returns it to the resource pool as
     `available`. The BMH CR persists.
  2. **Delete the BMH CRs** explicitly (`oc delete bmh -n <resourcePoolName>
     onboarded-<bmcAddress>`) once they are `available` and no longer referenced by
     any AllocatedNode / NodeAllocationRequest.
  3. **Delete the per-host BMC credential Secrets** referenced by
     `spec.bmc.credentialsName`. Note the credential
     Secrets are ESO-synced from ExternalSecret CRs, so ESO re-creates them until
     the owning ExternalSecret is removed; in practice these are cleaned up by
     the OnboardingRequest finalizer (below), which deletes the ExternalSecret
     first. Deleting them manually only sticks after the ExternalSecrets are gone.

  Only after all published `onboarded-*` BMHs are gone does the webhook permit
  deleting the OnboardingRequest.
- **Reject if any OnboardingServer is mid-BMC-mutation** — state
  `PinningBmcAddress`, `UpdatingCredentials`, or `ConfiguringLdap`. In these states
  a Metal3 BMC-config operation is in flight (BMO applying it over Redfish);
  deleting then could strand a BMC in a half-configured network or credential
  state. The operator
  waits for these to reach `Publishing`/`Completed` (then decommission the BMH
  per above) or a terminal failure state. (The `DisablingLocalAccounts` state also mutates the
  BMC, but it runs against an **already-published** BMH, so the published-BMH
  check above already blocks deletion while a server is hardening — no separate
  state check is needed for it.)

When both checks pass, deletion proceeds via a **finalizer** on the
OnboardingRequest. The webhook checks above are **advisory** — a server can enter a
BMC mutation or `Publishing` in the TOCTOU window between admission and teardown — so
the finalizer, not the webhook, is the authority that drains in-flight work before
destroying any shared state:

1. **Quiesce the children.** Once the OnboardingRequest has a `DeletionTimestamp`,
   the OnboardingServer controller treats that as a stop signal and starts **no new**
   BMC mutation and **no** `Publishing` transition. (Per-server reconcilers still run
   their own deletion/cleanup.)
2. **Drain in-flight mutations.** Block and requeue while any OnboardingServer is
   mid-BMC-mutation (`PinningBmcAddress` / `UpdatingCredentials` / `ConfiguringLdap`)
   or mid-`Publishing` — wait for BMO to settle each to a stable state (success or
   terminal failure). This closes the window the webhook's mid-mutation and
   published-BMH checks cannot.
3. **Re-check published BMHs.** If any **published** `onboarded-*` BMH remains in the
   resource pool namespace (e.g. one that reached `Publishing` during the drain),
   block and requeue — published hosts are live infrastructure and must be
   decommissioned first. (Held BMHs do not block; they are removed by the per-server
   finalizers in step 4.)
4. **Delete the children explicitly and wait for their finalizers.** The request
   finalizer does **not** rely on owner-reference garbage collection to clean up the
   children: under the default _background_ GC policy the owner can be removed before
   the child finalizers complete, and the controller must not depend on the parent
   finalizer to keep the children (and shared credentials) available. Instead it
   **explicitly deletes each OnboardingServer** and **blocks/requeues until their
   finalizers have run** — so each per-server rollback/cleanup completes **while the
   shared credential Secrets still exist** (BMO needs them to roll a held BMC back; see
   [Cleanup mechanism](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior)).
   This makes correctness independent of the foreground/background GC policy. (The
   service account therefore needs `delete` on `onboardingservers` — see the RBAC
   table.)
5. **Delete shared credentials last.** Only after every child OnboardingServer is gone
   does the request finalizer delete the shared ExternalSecret CRs (default
   credentials, LDAP bind/user) and their ESO-synced K8s Secrets. Per-server
   ExternalSecrets were already removed by their own OnboardingServer finalizers in
   step 4. The request finalizer is removed last, so the OnboardingRequest object
   outlives its children regardless of GC ordering.

**Inventory CRs are intentionally not deleted.** The `OCloudSite` and
`ResourcePool` CRs the controller created (in the O-Cloud manager namespace) are
**not** removed by the finalizer, and the `Location` CR — a prerequisite the
controller never creates — is likewise left untouched. These represent durable
O-Cloud inventory that outlives any single onboarding request: a ResourcePool may
still be referenced by other O2 IMS resources, an OCloudSite may host multiple
resource pools, and a Location may parent multiple sites. Deleting them on
OnboardingRequest teardown could orphan or invalidate unrelated inventory.
Removing these CRs is a separate, deliberate administrative action outside the
onboarding service's lifecycle. (This is also why the service account is granted
`create` but **not** `delete` on `ocloudsites`/`resourcepools` — see the RBAC
table in [Security Considerations](#4-security-considerations).)

### REST API

In addition to the CRD interface, the onboarding service exposes a REST API that
follows the same conventions as the other O-RAN O2 IMS services in this repository
(resources, alarms, provisioning, artifacts). Like the **artifacts** and
**provisioning** servers, it is a **thin facade over the CRDs** — it holds no
database. Every request is served by reading or writing the OnboardingRequest CR
through the hub Kubernetes client — that single CR carries everything the API
returns, so the facade never reads the OnboardingServer CRs. **OnboardingRequest is
the sole public resource in the REST contract**; OnboardingServer is an internal
reconciliation CR with no endpoint of its own (see the endpoint table below). The
CRs remain the source of truth and the controllers ([Controllers](server-onboarding-implementation.md#7-controllers))
continue to drive the workflow; the REST layer only projects that state to O-RAN
clients and accepts new requests.

**Base path and versioning.** The onboarding API is served under
`/ocloud-infrastructureOnboarding/v1`. It follows the _structural_ convention of
the other services — the `/<prefix>-infrastructure<Domain>/<version>` shape, the
`api_versions` endpoints, and the shared schemas/middleware — but deliberately
uses the **`ocloud-`** prefix rather than the **`o2ims-`** prefix the O2 IMS
services use (`/o2ims-infrastructureInventory/v2`,
`/o2ims-infrastructureProvisioning/v1`, `/o2ims-infrastructureArtifacts/v1`, …).
Server onboarding is **not** part of the O-RAN O2 IMS specification, so its API
must not live in the O2 IMS namespace; the `ocloud-` prefix keeps it clearly
separate while still reading as a first-class O-Cloud infrastructure API. (The
Kubernetes API group is likewise `onboarding.openshift.io`, not an `o2ims` group.)

**Endpoints.**

| Method and path | CR operation | Description |
|---|---|---|
| `GET /ocloud-infrastructureOnboarding/api_versions` | — | Supported major API versions (standard O-RAN endpoint) |
| `GET /ocloud-infrastructureOnboarding/v1/api_versions` | — | Minor versions for `v1` (standard) |
| `GET .../v1/onboardingRequests` | `List` OnboardingRequest | List all onboarding requests |
| `POST .../v1/onboardingRequests` | `Create` OnboardingRequest | Create a request (drives onboarding of its servers) |
| `GET .../v1/onboardingRequests/{onboardingRequestId}` | `Get` OnboardingRequest | Single request: summary counts plus the compact per-server list (name, state, message per server) |
| `DELETE .../v1/onboardingRequests/{onboardingRequestId}` | `Delete` OnboardingRequest | Tear down a request (subject to the finalizer / deletion guard — see [Deletion semantics](#deletion-semantics)) |

`onboardingRequestId` is the OnboardingRequest CR name (cluster-scoped, so no
namespace on lookup). The list endpoint supports the shared O-RAN field-selection
query parameters: `fields`, `allFields`, and `excludeFields` via the common
`NewFieldOptions` helper (`internal/service/common/api/options.go`), and `filter`
via the shared response-filtering middleware (the `FilterAdapter` / response
interceptor in `internal/service/common/api/middleware`). All four parameters are
defined in the common OpenAPI spec.

**OnboardingServer is internal.** OnboardingRequest is the only resource in the
public REST contract; there are no `onboardingServers` endpoints. OnboardingServer
CRs remain the per-server reconciliation unit and stay readable cluster-side via
`oc` for debugging, but REST clients never address them directly — and the facade
never reads them either. `GET onboardingRequests/{id}` returns exactly what is in
`OnboardingRequest.status`: the summary counts and the compact per-server list
(`status.servers`). It deliberately does **not** return per-server
`hardwareDetails`: that is inventory data, whose source of truth is the O2 IMS
Resource derived from the BMH (available from the inventory API once the host is
published), so composing it here would duplicate the inventory and re-couple the
facade to OnboardingServer. Full per-server forensic detail (`vendor`, `hardwareDetails`,
`serialNumber`, `bmhName`, `retryCount`, cleanup status) stays on
the OnboardingServer CR for `oc`-based inspection. Retrying a single failed server is
likewise a cluster-side operation (`oc delete onboardingserver …`, see
[Retrying a failed server](#retrying-a-failed-server)), not a REST call.

`POST`/`DELETE` are conveniences that create/delete the very same OnboardingRequest
CR an operator could `oc apply`/`oc delete`. The validating webhook
([Controllers](server-onboarding-implementation.md#7-controllers)) and the finalizer are enforced by the API server on
the CR write itself, so they apply identically no matter which interface
originates the change — the REST facade gains deletion safety and immutability
checks for free.

**Implementation.** Following the DB-free facade shape of `internal/service/artifacts`
(read-only, `List`/`Get` only) and — for the write handlers and Kubernetes-error
mapping — `internal/service/provisioning` (which implements the full
`Create`/`Delete` path), the server struct holds only a hub client and implements
the generated strict server interface:

```go
type OnboardingServer struct {
    HubClient client.Client
}

var _ api.StrictServerInterface = (*OnboardingServer)(nil)
```

- Handlers call `HubClient.List/Get/Create/Delete` on the CRs and translate
  Kubernetes errors into O-RAN `ProblemDetails` responses — the same mapping
  `internal/service/provisioning` already uses: `IsNotFound` → `404`,
  `IsAlreadyExists` → `409`, `IsForbidden`/`IsBadRequest`/`IsInvalid` (API-server
  and webhook validation) → `400`, otherwise `500`. (Artifacts is read-only, so it
  exercises only the `List`/`Get` path and produces `404` from an empty-result
  check rather than `IsNotFound`.)
- CR ↔ API converters map the CRD types to the generated OpenAPI models — there
  are no DB models and no repository layer.
- The onboarding CRD scheme is registered in the shared hub-client scheme
  (`internal/service/common/clients/k8s/k8s.go`, `GetSchemeForHub`).

**OpenAPI and code generation.** The API is defined in
`internal/service/onboarding/api/openapi.yaml` and code is generated with
`oapi-codegen` (strict-server, embedded-spec) via the standard `//go:generate`
directive and `make go-generate`, landing in
`internal/service/onboarding/api/generated/`. Error responses use
`application/problem+json` referencing the shared `ProblemDetails` schema in
`internal/service/common/api/openapi.yaml`; the shared field-selection parameters
are referenced from the same common spec.

**Authentication and authorization.** The REST server reuses the shared middleware
chain exactly as the other services do — OpenAPI request validation and response
filtering from `internal/service/common/api/middleware`, and from
`internal/service/common/auth` the OIDC/OAuth authenticator (with the Kubernetes
`TokenReview` fallback for internal service-to-service calls), the RFC 8705
certificate-bound-token verifier (`WithClientVerification`, see
[Transport security and mTLS](#transport-security-and-mtls) below), and the
`SubjectAccessReview`-based `Authorizer`. Two accepted design decisions apply
directly:

- **DD-003** — scopes are not read from the token; the OAuth server maps granted
  scopes to group memberships, which the authorizer maps to RBAC.
- **DD-004** — the onboarding API paths are added to the user-facing ClusterRoles as
  explicit `nonResourceURLs`, and the `openapi.yaml` `security` annotations track
  which role grants each operation. (The existing `oran_o2ims_user_roles.yaml` mixes
  explicit item paths with per-service `/o2ims-infrastructure*/*` wildcards; onboarding
  follows the explicit-path style so each verb maps to a specific role.) Because
  `onboardingRequests` is the only public resource, the enumerated paths are just its
  collection/item paths: a read-only role receives the two `GET` paths; a privileged
  onboarding role additionally receives `POST`/`DELETE` on `onboardingRequests`. That
  privileged role is a **new** write-capable role modeled on the existing
  `provisioner-role` / `subscriber-role` (which grant `create`/`delete`), **not** the
  `deployer-role` (which is read-only on `deploymentManagers`). There are no
  `onboardingServers` paths to grant. See the RBAC file list in
  [Project Structure](server-onboarding-implementation.md#8-project-structure-new-files).

#### Transport security and mTLS

The onboarding REST API satisfies the transport-security and mutual-TLS
requirement (TELCOSTRAT-290) using the **same mechanism as every other O2 IMS
service in this repository** — it does not invent a new one. There are two layers,
and the onboarding server inherits both for free by mounting the shared
`GetAuthenticator`/`GetAuthorizer` middleware:

1. **TLS/mTLS is terminated at the OpenShift Ingress (HAProxy) edge, not in the
   service.** Requests reach the service only after the router has terminated TLS
   and, when a client certificate is presented, verified it against the configured
   client-CA bundle. The router forwards the verification outcome and the DER-
   encoded client certificate in the trusted `x-ssl-client-verify` /
   `x-ssl-client-der` headers (and strips any client-supplied copies to prevent
   spoofing). The service never terminates TLS itself, so — like the other
   services — it does **not** set `tls.Config{ClientAuth: …}` for inbound
   connections; it trusts the edge. (The service's own client cert/key/CA-bundle
   settings are for **outbound** mTLS to the SMO/OAuth servers, not the inbound
   API.)
2. **Certificate-bound access tokens (RFC 8705).** The shared
   `WithClientVerification` authenticator wraps the OAuth bearer-token
   authenticator. When a presented token carries a confirmation (`cnf`) binding
   claim, the middleware computes the SHA-256 fingerprint of the client
   certificate from the trusted proxy header and requires it to match the token's
   bound fingerprint — proof-of-possession. A bound token replayed without the
   matching client certificate is rejected. Tokens with no binding claim continue
   to work as ordinary bearer tokens, so the same deployment supports both plain
   OAuth2 and mTLS-bound-token clients; requiring binding is a matter of issuing
   only bound tokens to onboarding clients. RFC 9440 `Client-Cert` / `Client-Chain`
   headers are stripped defensively on every request.

Because this is entirely shared code, the onboarding server picks it up by using
the common middleware and setting the common OAuth config (issuer, client ID,
`ClientBindingClaim`) — no onboarding-specific TLS or certificate-verification
logic is written. The **CRD interface** (`oc`/Kubernetes API) is unaffected: it is
protected by the Kubernetes API server's own TLS and RBAC, independent of this
REST-layer mTLS.

---

## 2. Server Onboarding State Machine

```text
Pending
    │
    ▼
CreatingExternalSecrets
    │
    ▼
Inspecting ──► AuthFailed (terminal)
    │          (BMO cannot register/authenticate with default creds)
    │  Create BMH: default creds, inspectionMode: fast, inventory-hold marker.
    │  BMO runs out-of-band (Redfish) inspection — no ramdisk boot — and
    │  produces HardwareData in seconds.
    ▼
Inventorying ──► VendorMismatch (terminal) ─┐
    │        └──► HwMismatch (terminal) ─────┤ delete the (held) BMH
    │  Compare inspection HardwareData to    │
    │  desired vendor/profile.               ┘
    ▼
ApplyingHardwareProfile (only if spec.hardwareProfileName set; skipped otherwise)
    │  Render the referenced HardwareProfile into HostFirmwareSettings /
    │  HostFirmwareComponents / HostUpdatePolicy on the held BMH; BMO services
    │  the host (incl. reboots) to the desired firmware/BIOS baseline. On
    │  failure → Failed (the held BMH is deleted on cleanup).
    ▼
PinningBmcAddress (both authModes)
    │  Request the DHCP→static transition (same address) + DNS via the Metal3
    │  BMC networking config API; BMO applies it over Redfish.
    │  Watch BMH conditions for the outcome. Reversible (revert to DHCP).
    ▼
UpdatingCredentials (authMode=local) ─── OR ─── ConfiguringLdap (authMode=ldap)
    │  Request the credential change via      │  Request LDAP config via the Metal3
    │  the Metal3 BMC credentials API         │  LDAP integration API;
    │  BMO applies it and                     │  BMO applies it over Redfish. ESO
    ▼  updates the BMH Secret.                ▼  keeps the BMC Secret in sync.
Publishing
    │  Remove inventory-hold marker → BMH becomes visible to O2 IMS inventory.
    ▼
DisablingLocalAccounts (authMode=ldap + disableLocalAccounts only; skipped otherwise)
    │  Disable local BMC accounts via the Metal3 LDAP/credentials API
    │  (irreversible). Best-effort: the host is already published and works over
    ▼  LDAP, so this never fails the server — it proceeds to Completed regardless.
Completed (terminal)

Any non-terminal state EXCEPT DisablingLocalAccounts ──► Failed (on unrecoverable
error or retries exhausted). DisablingLocalAccounts is best-effort and always
ends at Completed.
On VendorMismatch / HwMismatch the controller deletes the (still-held) BMH.
```

The detailed, per-state behavior — what the controller does in each state above,
including `CreatingExternalSecrets`, `Inspecting`, `Inventorying`,
`ApplyingHardwareProfile`, `PinningBmcAddress`, the credential/LDAP states,
`Publishing`, and `DisablingLocalAccounts` — is in
[Server onboarding state machine — detailed per-state behavior](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior)
in the implementation document. The subsections below cover lifecycle operations
that apply across states.

### Retrying a failed server

A server that lands in a terminal failure state (`AuthFailed`,
`VendorMismatch`, `HwMismatch`, or `Failed`) is retried **per server**, without
touching the rest of the pool:

- **Delete the individual OnboardingServer CR** (`oc delete onboardingserver
  -n <resourcePoolName> <name>`). Because OnboardingServer CRs are (re)created
  from `spec.servers` by the OnboardingRequest controller, the controller
  recreates the deleted one as `Pending` on its next reconciliation and the state
  machine runs again from the start. The server's spec entry remains in the
  OnboardingRequest, so nothing needs to be re-declared.

This is the **only** supported retry path. Deleting and recreating the whole
OnboardingRequest is **not** a viable retry mechanism once any server has
succeeded: the deletion webhook rejects removal while any `onboarded-*` BMH
exists (see [Deletion semantics](#deletion-semantics)), so a request with a mix
of completed and failed servers cannot be deleted just to retry the failures.

Whether a recreate succeeds depends on **when** the failure occurred:

- **Pre-mutation terminal states** (`AuthFailed`, `VendorMismatch`, `HwMismatch`,
  and any `Failed` reached during `Pending` / `CreatingExternalSecrets` /
  `Inspecting` / `Inventorying` / `ApplyingHardwareProfile` / `PinningBmcAddress`):
  the BMC still holds the
  pool's default
  credentials, so a
  recreated OnboardingServer re-registers cleanly. Deleting the OnboardingServer
  first deletes any held BMH via the finalizer (it was never published) and
  best-effort requests a revert of a pinned BMC NIC to DHCP (via the Metal3
  networking config API), so the recreated run starts from a clean slate. (A `Failed` in `ApplyingHardwareProfile`
  may have flashed firmware or applied BIOS settings already; that hardware state
  is not rolled back, but the recreated run simply re-applies the same profile, so
  the baseline converges regardless.) (Even if the revert is skipped, the
  BMC keeps the same address, so re-registration still succeeds.) Recreate
  **after** fixing the root
  cause — reset/replace the BMC-default credentials or vault entry (`AuthFailed`),
  rack the correct hardware model (`VendorMismatch` / `HwMismatch`), or restore
  vault/ESO connectivity (`Failed`).
- **Post-mutation `Failed`** (reached during `UpdatingCredentials`,
  `ConfiguringLdap`, or `Publishing`, after a BMC credential/LDAP change may have
  applied): a recreated OnboardingServer restarts from `Inspecting`, which creates
  the BMH with the **pool default** credentials, but the BMC may already have the
  per-server desired (or LDAP) credentials, so BMO would fail to register at
  `Inspecting` (→ `AuthFailed`). The operator must first restore the BMC to accept
  the pool default credentials (e.g. an out-of-band BMC reset) **before** deleting
  the OnboardingServer to retry.

### Deleting a Completed server (idempotent re-onboarding)

Retry-by-delete is intended for **failed** servers, but nothing stops an operator
from deleting a `Completed` OnboardingServer — and the OnboardingRequest
controller will recreate it as `Pending` all the same, because its entry is still
in `spec.servers`. Naively re-running the state machine would be harmful: the BMC
now holds the per-server desired (or LDAP) credentials, so a fresh `Inspecting`
run — which creates the BMH with the pool default credentials — would fail to
register (→ `AuthFailed`) even though a perfectly healthy, published BMH already
exists.

To make this safe, a reconcile of a CR **with no in-progress state of its own**
(`status.state` empty or `Pending` — a freshly created or recreated
OnboardingServer) begins with an **already-onboarded check**: the controller
looks for an existing **published** BMH for this server in the resource pool
namespace. Because the BMH name is deterministically derived from the server's
`bmcAddress` (see
[BMH and per-host Secret naming](server-onboarding-implementation.md#6-hardware-inventory-and-bmh-creation)), the
controller recomputes that name from `bmcAddress` and gets the BMH directly — no
address annotation or list-and-filter is needed. It then checks that the BMH is
**no longer carrying the inventory-hold marker**. If such a BMH is found, the
server is already onboarded — but before short-circuiting the controller must also
confirm that any **post-publication hardening was completed**, because a recreated
CR has no memory of its predecessor's progress. A server deleted while in
`DisablingLocalAccounts` (ldap + `disableLocalAccounts: true`) comes back with an
empty state and would otherwise pass straight to `Completed`, silently skipping the
local-account disable. To prevent that, the hardening **outcome** — not merely that
the state ran — is recorded on the **retained BMH** itself. The
`onboarding.openshift.io/hardening-complete` annotation is set **only when the local
account is actually disabled** (and set at `Publishing` when `disableLocalAccounts`
is not requested, so the common case is already marked). Because
`DisablingLocalAccounts` is best-effort and still reaches `Completed` after its
retries are exhausted, a **failed** attempt must not mark the BMH complete; instead
the controller records the failure outcome on the BMH (a
`onboarding.openshift.io/hardening-failed` annotation carrying the reason) so a
recreated server can restore the reported state:

- If the published BMH is marked hardening-complete (or hardening was never
  requested), the controller **short-circuits straight to `Completed`**, re-records
  `status.bmhName`, and performs no inspection or BMC mutation.
- If hardening was requested but the BMH is **not** marked complete — whether it was
  never attempted or a prior attempt failed — the controller adopts the BMH and
  **resumes at `DisablingLocalAccounts`** rather than short-circuiting, restoring the
  `LocalAccountsDisabled=false` condition (with the recorded reason) so the failure
  stays observable and the disable is retried before the server reaches `Completed`.

The in-progress-state gate still prevents the check from firing on a server that is
**the same CR** still progressing (e.g. mid-`DisablingLocalAccounts`); the BMH
marker additionally covers the delete-and-recreate case the gate cannot see.
Combined with the finalizer **retaining**
the per-server credentials whenever a published BMH exists (see
[Cleanup mechanism](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior)), deleting a `Completed`
OnboardingServer is therefore a safe no-op: the CR is cleaned up without touching
the live BMH or its credentials, recreated as `Pending`, and immediately
reconciled back to `Completed` against the existing BMH.

A BMH that is found **still holding** the inventory-hold marker is treated
differently — it is a mid-onboarding host, not a finished one. The controller
adopts it (records `status.bmhName`) and resumes the state machine rather than
short-circuiting. This is also the idempotency guarantee for `Inspecting`: a
recreated or requeued server that already has its (held) BMH does not create a
duplicate; it adopts the existing one and continues.

---

## 3. Network Dependencies

This section consolidates all external network requirements. **The onboarding
controller itself makes no connections to the BMC management subnet — it talks only
to the Kubernetes API.** All BMC communication (inspection and every BMC-config
operation) is performed by **Metal3/BMO** over Redfish HTTPS; the optional LDAP
directory is contacted by the BMCs themselves.

```text
Hub Cluster                               Remote Site
┌───────────────────────┐                 ┌──────────────────────────────────┐
│ Onboarding Pod        │                 │  BMC Management Subnet           │
│  (K8s API only —      │                 │                                  │
│   no BMC traffic)     │                 │  ┌─────┐ ┌─────┐ ┌─────┐       │
│                       │                 │  │ BMC │ │ BMC │ │ BMC │       │
│ Metal3 / BMO          │  [1] Redfish    │  └──┬──┘ └──┬──┘ └──┬──┘       │
│  ┌────────────┐       │     (HTTPS)     │     │      │       │          │
│  │ BMO/Ironic │───────┼─────────────────┼──►──┴──────┴───────┘          │
│  └────────────┘       │                 │     │ [2] LDAPS                │
│                       │                 │     ▼                          │
│                       │                 │  ┌────────────────┐            │
│                       │                 │  │ LDAP Server    │            │
│                       │                 │  │ (optional)     │            │
│                       │                 │  └────────────────┘            │
└───────────────────────┘                 └──────────────────────────────────┘
```

### Network paths

| # | Path | Protocol | Direction | Purpose |
|---|------|----------|-----------|---------|
| 1 | **Metal3/BMO → BMCs** | HTTPS (TCP 443) | Outbound from hub | BMO/Ironic reaches each BMC's Redfish API for all BMC work: fast OOB inspection, the DHCP→static pin + DNS, credentials, and LDAP. Requires hub→BMC routing (from BMO pods). No provisioning net/DHCP/TFTP/PXE. |
| 2 | **BMCs → LDAP server** | LDAPS (TCP 636) | From BMCs | (authMode=ldap only) After LDAP is configured on a BMC, the BMC authenticates users against the LDAP server. The LDAP server must be reachable from the BMC management subnet. This path does not involve the hub cluster. |
| — | **Onboarding Pod → BMCs** | — | — | **None.** The onboarding controller never connects to a BMC; it drives BMC operations declaratively through Metal3 CRs and watches BareMetalHost status. |

### Firewall and routing requirements

| Requirement | Source | Destination | Ports | Notes |
|---|---|---|---|---|
| Redfish from hub | Metal3/BMO (hub) | BMC IPs on management subnet | TCP 443 | Hub must have routed connectivity to BMC subnet from the Metal3/BMO pods; TLS with self-signed certs. Carries all onboarding BMC traffic — inspection and net/creds/LDAP config. The onboarding pod needs no such route. |
| LDAP from BMCs | BMC IPs | LDAP server | TCP 636 (LDAPS) | Only for authMode=ldap; BMCs initiate connections after LDAP is configured |

### Isolation assumptions

- The BMC management subnet is a **dedicated, isolated network** (typically a separate VLAN) — not shared with production traffic or cluster pod/service networks.
- The hub cluster's routing to BMC subnets should be restricted to the Metal3/BMO pods — not open to the onboarding pod (which needs no such route) or to arbitrary cluster workloads.

---

## 4. Security Considerations

- **Credential storage**: Credentials are stored in an external vault, not directly in the cluster. The OnboardingRequest CR contains only vault secret names — never credential values. The External Secrets Operator (ESO) syncs vault secrets into K8s Secrets.
- **Per-server credential isolation**: In local mode, each server has unique target credentials stored as separate vault entries. Compromise of one server's credentials does not expose other servers in the pool.
- **ExternalSecret CRs**: Created by the controller in the resource pool namespace.
  Each ExternalSecret references the `ClusterSecretStore` configured globally on the
  controller (`--secret-store-name` / `SECRET_STORE_NAME`, default
  `onboarding-vault`) and the vault secret name from the OnboardingRequest or
  ServerSpec. ESO handles the sync lifecycle.
- **The onboarding controller never handles BMC credentials in memory.** It
  references vault/Secret names only; BMO reads the ESO-synced Secret and performs
  all BMC authentication over Redfish. This removes the controller as a _direct_
  credential attack surface — there is no BMC credential material, and no BMC
  connection, in the onboarding pod.
- **Indirect credential exposure via BMH creation must be constrained.** Removing
  Secret `get` is not sufficient on its own: an attacker who obtained the controller
  service account could create a BareMetalHost that references an existing credential
  Secret, point `spec.bmc.address` at an endpoint they control, and set
  `disableCertificateVerification: true` — causing BMO to authenticate to the
  attacker and disclose the credentials. Because BMH creation is therefore a trust
  boundary, service-created BMHs are constrained by a validating webhook (and the
  controller's own creation path): `spec.bmc.credentialsName` may reference only
  onboarding-owned Secrets in the resource pool namespace; `spec.bmc.address` must be
  the `bmcAddressTemplate` rendered for a declared server `bmcAddress` that falls
  inside `bmcSubnet` (it cannot point at an arbitrary endpoint); and
  `disableCertificateVerification` must follow the pool policy
  (`spec.disableBmcCertificateVerification`) rather than being set freely. The
  controller service account's `create` on `baremetalhosts` is the sensitive grant
  this guard protects; it must stay tightly scoped.
- `status.stateMessage` on OnboardingServer must never contain credential values
- BMC Redfish transport (HTTPS, TLS) is handled by Metal3/BMO, not the onboarding
  service. **BMC certificate verification is on by default** (`spec.bmc.disableCertificateVerification:
  false` on every BMH); it is disabled only when a pool explicitly opts in via
  `spec.disableBmcCertificateVerification` for self-signed BMCs with no trusted CA —
  an explicit exception, because disabling it lets an attacker on the management path
  impersonate a BMC and intercept credentials. Prefer provisioning a trusted CA bundle
  for the BMCs over disabling verification
- Management network assumed isolated (dedicated VLAN)
- **Authorization**: OnboardingRequest is cluster-scoped. Access is controlled via
  ClusterRoleBindings — any user with `create` permission on OnboardingRequests can
  onboard servers into any resource pool at any site. This is appropriate for the
  expected use case where onboarding is performed by a small set of platform
  administrators. If multi-tenant or per-site access control is needed in the
  future, a validating webhook could restrict operations based on the requesting
  user's group membership or a site-scoped authorization CR.
- **REST API authorization**: The [REST API](#rest-api) reuses the shared OAuth/OIDC
  authenticator and `SubjectAccessReview` authorizer. Per DD-003, granted scopes are
  mapped to groups and then to RBAC; per DD-004, the onboarding API paths are
  enumerated explicitly in the user-facing ClusterRoles, with read-only vs.
  privileged (`POST`/`DELETE`) split across roles. Because the facade ultimately
  performs the same CR writes, a request is only admitted when _both_ the endpoint
  RBAC (path authorization) and the API server (CRD write, webhook, finalizer)
  allow it.
- **REST API transport security / mTLS (TELCOSTRAT-290)**: TLS and, when a client
  certificate is presented, mutual TLS are terminated at the OpenShift Ingress
  (HAProxy) edge; the shared `WithClientVerification` middleware then enforces
  RFC 8705 certificate-bound access tokens by matching the client-certificate
  fingerprint (from the trusted `x-ssl-client-*` headers) against the token's
  binding claim. This is the identical mechanism used by the other O2 IMS services,
  reused unchanged — see [Transport security and mTLS](#transport-security-and-mtls).
  Bound tokens replayed without the matching certificate are rejected; unbound
  tokens still work as plain OAuth2, so mTLS can be enforced by issuing only bound
  tokens to onboarding clients.
- Service account RBAC — the onboarding controllers run with a dedicated service
  account scoped to exactly the verbs below. Note the read verbs (`list`/`watch`)
  the controllers need to _watch_ `ExternalSecret` sync status (readiness is taken
  from the `ExternalSecret` `.status`, not by reading Secret data) and to _re-check_
  BMHs in the deletion finalizer, which the earlier one-line summary omitted:

  | API group | Resource | Verbs | Why |
  |---|---|---|---|
  | `onboarding.openshift.io` | `onboardingrequests` | get, list, watch, update, patch | Reconcile; manage finalizer |
  | `onboarding.openshift.io` | `onboardingrequests/status` | get, update, patch | Update summary counts / conditions |
  | `onboarding.openshift.io` | `onboardingservers` | get, list, watch, create, update, patch, delete | Create one per server entry; drive reconcile; `delete` to explicitly tear children down and wait for their finalizers during request deletion (not relying on owner-reference GC) |
  | `onboarding.openshift.io` | `onboardingservers/status` | get, update, patch | Record state machine state |
  | `ocloud.openshift.io` | `locations` | get, list | Prerequisite lookup (must exist) |
  | `ocloud.openshift.io` | `ocloudsites` | get, list, create | Create if not found |
  | `ocloud.openshift.io` | `resourcepools` | get, list, create | Create if not found |
  | (core) | `namespaces` | get, list, create | Resource pool namespace |
  | `external-secrets.io` | `externalsecrets` | get, list, watch, create, delete | Create shared/per-server ExternalSecrets; delete on finalizer/cleanup |
  | (core) | `secrets` | delete | Delete per-server ESO-synced credential Secrets on cleanup (by name). The controller does **not** read Secret data — readiness comes from the `ExternalSecret` `.status`, and BMO reads the credential Secret — so it holds no `get`/`list`/`watch` on Secrets |
  | `metal3.io` | `baremetalhosts` | get, list, watch, create, update, patch, delete | Create the BMH (`inspectionMode: fast`, hold marker) in `Inspecting`; `patch` to request BMC config and clear the hold annotation (`Publishing`); watch conditions; delete a **held** BMH (code-constrained) |
  | `metal3.io` | BMC networking / credentials / LDAP config resources (exact kinds TBD — may be BareMetalHost sub-resources or companion CRs) | get, list, watch, create, update, patch, delete | Declare and track the DHCP→static pin + DNS, credential provisioning, and LDAP config that BMO applies over Redfish. The precise resources/verbs depend on the final Metal3 API — see [Dependency on forthcoming Metal3 APIs](#dependency-on-forthcoming-metal3-apis) |
  | `metal3.io` | `hardwaredata` | get, list, watch | Read the `HardwareData` CR produced by fast out-of-band (Redfish) inspection for vendor/hardware matching (`Inventorying`) |
  | `clcm.openshift.io` | `hardwareprofiles` | get, list | Read the referenced `HardwareProfile` for the firmware/BIOS baseline (`ApplyingHardwareProfile`); only when `hardwareProfileName` is set |
  | `metal3.io` | `hostfirmwaresettings`, `hostfirmwarecomponents`, `hostupdatepolicies` | get, list, watch, create, update, patch, delete | Render the profile into servicing CRs on the held BMH and watch convergence (`ApplyingHardwareProfile`) |
  | `metal3.io` | `firmwareschemas` | get, list, watch | Read the BMH's `FirmwareSchema` to validate BIOS settings in the reused `HostFirmwareSettings` translation (`ApplyingHardwareProfile`); the grant every controller on this path already holds |
  | (core) | `events` | create, patch | Emit events on state transitions and failures |
  | `coordination.k8s.io` | `leases` | get, list, watch, create, update | Controller-runtime leader election |

  Because the BMH is created early (in `Inspecting`) and must be deleted when a
  server fails to match or is retried before it is published, the service account
  needs `delete` on `baremetalhosts`. Kubernetes RBAC cannot restrict `delete` to
  the subset of BMHs carrying the inventory-hold marker, so the constraint is
  **enforced in controller logic**: the controller only ever deletes a BMH that
  still has the `onboarding.openshift.io/inventory-hold` annotation, and never a
  **published** (live infrastructure) BMH. This is backed by the OnboardingRequest
  **deletion guard**, which blocks request teardown while any published BMH exists
  (see [Deletion semantics](#deletion-semantics)). The `delete` grant is scoped to
  BMHs only; the service account has no `delete` on `ocloudsites`/`resourcepools`.
- No database dependency — all state in CRDs; the REST API server is a stateless,
  DB-free facade over them and runs under its own ClusterRole
  (`clusterrole_onboarding_server.yaml`), separate from the controller-manager's
  service account above. It touches only OnboardingRequest CRs — reads and writes —
  and never reads or mutates OnboardingServer CRs; it exposes no `onboardingServers`
  endpoint

**Container hardening requirements:**

| Requirement | Purpose |
|-------------|---------|
| Distroless base image | No shell, no package manager — limits post-exploitation capability |
| Non-root user | Prevents privilege escalation within the container |
| Read-only root filesystem | Minimal writable surface |
| Drop all capabilities | No privileged operations needed |
| NetworkPolicy | Egress: Kubernetes API only (no BMC-subnet egress — BMO does all BMC traffic). Ingress: **API server → validating-webhook port only** (the webhook's `failurePolicy: Fail` would otherwise block OnboardingRequest create/delete); no other ingress |

---

## Dependency on forthcoming Metal3 APIs

The onboarding controller performs **no BMC I/O of its own**. Every BMC operation
is delegated to Metal3/BMO through declarative, BareMetalHost-integrated APIs, and
the controller observes outcomes via BareMetalHost status conditions/events. This
design depends on four forthcoming Metal3 capabilities:

- **Fast out-of-band inspection** — `spec.inspectionMode: fast` produces `HardwareData` over Redfish with no ramdisk boot or provisioning network (`Inspecting` → `Inventorying`).
- **BMC networking config API** — DHCP→static BMC-IP pin (same address) and DNS (`PinningBmcAddress`).
- **BMC credentials provisioning API** — Day-0 factory-default replacement, propagated to the BMC and the BareMetalHost Secret (`UpdatingCredentials`); also the local-account disable step.
- **LDAP integration API** — configure LDAP auth on the BMC, with ESO credential sync (`ConfiguringLdap`, `DisablingLocalAccounts`).

**Implications and open questions:**

- **API shape is not yet frozen.** Whether each capability is surfaced as new
  fields on `BareMetalHost`, as companion CRs, or as a sub-resource is still being
  decided by the Metal3 team. The YAML, field names, and the `metal3.io` RBAC row
  for "BMC networking / credentials / LDAP config resources" in this proposal are
  **illustrative** and must be reconciled with the final APIs. The onboarding
  state machine and its state names do not depend on the exact shape — only the
  mechanics of each BMC-mutation state do.
- **Vendor Redfish coverage is partial.** The BMC-config APIs note that DHCP
  toggle and `StaticNameServers` are inconsistent across Dell/HPE and may require
  OEM extensions; DNS and the DHCP→static transition should be treated as
  best-effort per vendor until BMO reports them supported. This is the same
  per-vendor caveat called out for fast inspection under
  [Hardware attribute matching](#1-api-design-crds).
- **Sequencing.** Onboarding of local-credential pools needs fast inspection + BMC
  networking + BMC credentials; LDAP pools additionally need LDAP integration. Until
  a given API lands, the corresponding state is blocked — the service can be built
  and merged against these interfaces, but end-to-end onboarding is gated on their
  delivery.
- **No interim direct-Redfish fallback is specified.** This proposal deliberately
  assumes the Metal3 APIs rather than giving the onboarding controller its own
  BMC/Redfish client as a stopgap; adding one later would reintroduce the
  credential and network attack surface this design removes.

---

## Detailed implementation

The detailed implementation — the detailed per-state behavior, the Metal3
BMC-config orchestration, hardware inventory and BMH creation, controllers,
project structure, prerequisites, the end-to-end walkthrough, verification, and
open questions — is in the companion document (network dependencies and security
considerations are covered above, in sections 3 and 4):

**[Server Onboarding Service - Implementation](./server-onboarding-implementation.md)**

The phased engineering plan for building the service — work breakdown, sequencing,
dependencies, and per-phase exit criteria — is in the
**[Server Onboarding Service - Implementation Plan](./server-onboarding-implementation-plan.md)**.
