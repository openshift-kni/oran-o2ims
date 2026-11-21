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

Hardware validation is performed by **creating the BareMetalHost and letting BMO run
fast out-of-band (Redfish) inspection** (`spec.inspectionMode: fast`). The BMH is
created with each server's **provided credentials** in v1, or with the pool's **shared
default credentials** in the credential-provisioning phase (then swapped to per-server)
— see [Credentials: v1 vs credential provisioning](#2-server-onboarding-state-machine).
The inspection results are checked against the
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

    // BootInterfaceSelector is the pool-level default for identifying each host's
    // provisioning/boot NIC among the NICs fast inspection reports, so the controller
    // can set the boot-interface label / spec.bootMACAddress before Publishing (a host
    // with no boot interface cannot later be provisioned). It selects by NIC name or
    // MAC prefix (OUI). Resolution per server: a per-server servers[].bootMACAddress
    // wins; else this selector; else, if exactly one NIC was discovered, it is used;
    // else (ambiguous multi-NIC, no match) the server fails with a clear message.
    // Optional. See "BMH creation".
    BootInterfaceSelector *BootInterfaceSelector `json:"bootInterfaceSelector,omitempty"`

    // OnboardingTimeout bounds how long a single server may take to reach Completed.
    // Transient errors are retried with backoff (no fixed retry-count budget); if a
    // server has not completed within this duration (from status.onboardingStartedAt)
    // it goes terminal Failed with reason OnboardingTimeout. Pool-level, applied per
    // server. Optional; defaults to a sensible value (e.g. 30m) when unset.
    OnboardingTimeout *metav1.Duration `json:"onboardingTimeout,omitempty"`

    // HardeningTimeout bounds the post-Publishing DisablingLocalAccounts step, which
    // is best-effort and exempt from OnboardingTimeout (the server is already
    // published). Measured from entry to DisablingLocalAccounts; when it elapses the
    // controller stops retrying, records the incomplete hardening in
    // status.stateMessage, sets the LocalAccountsDisabled condition False with reason
    // HardeningTimeout, and proceeds to Completed. Optional; defaults e.g. 30m.
    HardeningTimeout *metav1.Duration `json:"hardeningTimeout,omitempty"`

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

    // BootMACAddress optionally pins this server's provisioning/boot NIC MAC. It is a
    // per-server override that takes precedence over the pool-level
    // bootInterfaceSelector when resolving the boot interface before Publishing (see
    // BMH creation). Use it for a host whose boot NIC the pool selector cannot
    // identify. Optional.
    BootMACAddress string `json:"bootMACAddress,omitempty"`
}

// BootInterfaceSelector identifies the boot NIC among inspected NICs. Exactly one of
// the fields is set.
type BootInterfaceSelector struct {
    // NICName matches the inspected NIC by name (e.g. "eno1").
    NICName string `json:"nicName,omitempty"`
    // MACPrefix matches the inspected NIC whose MAC starts with this prefix/OUI
    // (e.g. "aa:bb:cc"). The selector must resolve to exactly one NIC: if the
    // prefix matches more than one inspected NIC (or none), boot-interface
    // resolution fails rather than guessing — use a more specific prefix, NICName,
    // or the per-server bootMACAddress override to disambiguate.
    MACPrefix string `json:"macPrefix,omitempty"`
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
    // Conditions carries two distinct conditions so request validation and
    // aggregated onboarding progress are not conflated (the earlier single "Ready"
    // was ambiguous — it could read as "the request is ready to process" or "all
    // servers are ready"):
    //
    //   Validated — did the request pass validation?
    //     False, reason InvalidSpec | LocationNotFound | OCloudSiteConflict |
    //       ResourcePoolConflict
    //     True,  reason Completed
    //     InvalidSpec is retained even though the webhook validates the same fields:
    //     the controller must still handle bad input safely (e.g. a bmcAddress that
    //     fails to parse before any dependent resource is created), and the webhook
    //     can be bypassed or a check may be controller-only.
    //
    //   Onboarded — aggregated onboarding status across all servers.
    //     False, reason InProgress | Failed
    //     True,  reason Completed
    //     The ONBOARDINGSTATE printer column surfaces this condition's reason
    //     alongside the server counts for a quick overview.
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // Summary counts — updated by the OnboardingRequest controller on each reconciliation.
    // The controller lists OnboardingServer CRs in the resource pool namespace filtered by
    // spec.onboardingRequestName, and counts by status.state:
    //   TotalServerCount       = len(spec.servers)
    //   PendingServerCount     = count where state == "Pending" or unset (an
    //                            OnboardingServer created but not yet picked up —
    //                            status is a subresource, so a freshly created CR
    //                            has an empty state until its first reconcile),
    //                            plus server entries for which no OnboardingServer
    //                            CR exists yet
    //   InProgressServerCount  = count where state is non-terminal and not Pending
    //                            (CreatingExternalSecrets, Inspecting, Inventorying,
    //                             ApplyingHardwareProfile, PinningBmcAddress,
    //                             UpdatingCredentials, ConfiguringLdap, Publishing,
    //                             DisablingLocalAccounts)
    //   CompletedServerCount   = count where state == "Completed"
    //   FailedServerCount      = count where state in terminal failure states
    //                            (VendorMismatch, AuthFailed, HwMismatch, Failed)
    // Invariant: PendingServerCount + InProgressServerCount + CompletedServerCount +
    //   FailedServerCount == TotalServerCount at all times (Pending absorbs both
    //   not-yet-created and not-yet-started servers so the counts always add up).
    // The controller watches OnboardingServer CRs via an index on spec.onboardingRequestName
    // and re-reconciles the parent OnboardingRequest on any OnboardingServer state change.
    TotalServerCount      int `json:"totalServerCount,omitempty"`
    PendingServerCount    int `json:"pendingServerCount,omitempty"`
    InProgressServerCount int `json:"inProgressServerCount,omitempty"`
    CompletedServerCount  int `json:"completedServerCount,omitempty"`
    FailedServerCount     int `json:"failedServerCount,omitempty"`

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
// heavy or internal per-server fields (e.g. hardwareDetails, vendor), which remain on
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

**Field availability (phased delivery).** The spec above is the **target** shape;
the CRD is built up over the phases in the
[Implementation Plan](server-onboarding-implementation-plan.md), **not** all at once.
To avoid committing to a field shape before the dependent Metal3 API is finalized,
each Metal3-dependent field is added **with the phase that implements it** (alongside
its validation, controller behavior, and tests), not in the v1 CRD:

| Field(s) | Delivered in | Gated on |
|---|---|---|
| `globalLocationName`, `oCloudSiteName`, `resourcePoolName`, the description fields, `defaultCredentialsVaultSecret`, `bmcVendor`, `hardwareAttributes`, `bmcAddressTemplate`, `bootInterfaceSelector`, `onboardingTimeout`, `servers[]` | **v1** | METAL-1784 |
| `bmcSubnet`, `bmcGateway` | BMC address-pinning phase | OCPSTRAT-3585 |
| `disableBmcCertificateVerification` | BMC credential-provisioning phase | OCPSTRAT-3519 |
| `authMode`, `ldapConfig`, `hardeningTimeout` | LDAP mode phase | OCPSTRAT-3552 |
| `hardwareProfileName` | Firmware/BIOS baseline phase | — (reuses the existing hardware-manager profile path) |

Until a field's phase lands it is not part of the served CRD, so the API never
commits to a shape that the forthcoming Metal3 APIs might change.

**Printcolumns:** OnboardingState (the `Onboarded` condition's reason), Total
(`totalServerCount`), Pending (`pendingServerCount`), In-Progress
(`inProgressServerCount`), Completed (`completedServerCount`), Failed
(`failedServerCount`), Age — the summary row shown by `oc get onboardingrequest`
in the [Walkthrough](server-onboarding-implementation.md#10-walkthrough).

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
- `resourcePoolName` must be unique across all OnboardingRequests — only one
  OnboardingRequest per resource pool. The validating webhook enforces this on a
  best-effort basis by listing existing OnboardingRequests, but a webhook cannot
  guarantee global uniqueness under concurrent admissions (two requests naming the
  same pool can pass admission simultaneously — a TOCTOU race). The
  OnboardingRequest controller therefore re-checks on reconciliation: if it finds
  another OnboardingRequest already owning the same `resourcePoolName`, the later
  one (by `creationTimestamp`, then name) is set `Validated=False,
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
  failed server — **without** recreating the whole request. The webhook re-validates
  the CIDR/gateway/family rules on update.
- **`servers` is the desired set (add and remove)**: `spec.servers` declares the
  servers the pool should contain. Adding an entry onboards a server; **removing** an
  entry **de-onboards** it (the controller restores the BMC and deletes the BMH and
  its resources — see [Deletion semantics](#deletion-semantics)). An entry's identity
  is its `bmcAddress`, which cannot be changed in place — correcting a mistyped
  address is a remove-then-add. A removal is rejected by the webhook while that
  server's OnboardingServer is mid-onboarding or its BMH is allocated/provisioned
  (deprovision the host first).

The webhook validates on update:

1. All fields other than `servers`, `bmcSubnet`, and `bmcGateway` are unchanged.
2. Existing server entries are not **modified in place** (an entry's `bmcAddress` is
   its identity); entries may be **added or removed**, but a removal is allowed only
   when the server is safe to de-onboard (OnboardingServer in a terminal state and
   its BMH not allocated/provisioned — see [Deletion semantics](#deletion-semantics)).
3. Added entries pass the same validation rules as at creation time.
4. `bmcAddress` values remain unique within the list.
5. Updated `bmcSubnet`/`bmcGateway` still satisfy the CIDR/gateway/family rules, and every server's `bmcAddress` still falls within `bmcSubnet`.

**OnboardingServer CR naming:** The CR name is a **deterministic UUID** (UUIDv5)
computed over a stable identity — the resource pool (equivalently the
OnboardingRequest, one per pool) plus the server's **canonicalized** `bmcAddress`
(RFC 5952 for IPv6, so equivalent spellings map to one UUID). This mirrors how
Agent CRs are named, and sidesteps the pitfalls of deriving an RFC-1123 label from
an IP — `.`/`:` collisions, IPv6 uppercase and `::` compression, trailing hyphens —
**and the 253-character length budget entirely**: a UUID is always a fixed-length,
valid label. It is deterministic, so a recreated OnboardingServer for the same
server resolves to the **same** name (part of the idempotent re-onboarding
guarantee). The derived names are:

- OnboardingServer: `<uuid>`
- BMH: `onboarded-<uuid>` (same UUID)
- per-server credential Secret / ExternalSecret: `<uuid>-desired-creds`

All are fixed-length and well within 253 characters regardless of
`onboardingRequestName` length, so no length budget or request-name bound is
needed.

Because a UUID is opaque, the request name and BMC address are surfaced as
**printer columns** (and `bmcAddress` is selectable) so the resource association
stays legible:

```text
NAME                                   REQUEST              BMC ADDRESS     STATE
2a773a0e-d230-5ee3-a94b-60d3448bd921   toronto-east-pool1   10.10.10.101    Completed
```

Clients filter by request via the `spec.onboardingRequestName` field — e.g.
`oc get onboardingservers -n <pool> -o json | jq '.items[] | select(.spec.onboardingRequestName == "<name>")'`,
or a field selector if one is registered. (For brevity, the examples elsewhere in
this document still show a readable `onboarded-10-10-10-101`-style name in place of
the UUID.)

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

1. **Location CR** — must already exist in `oran-o2ims`. If not found, the OnboardingRequest is set to `Validated=False, Reason=LocationNotFound`.
2. **OCloudSite CR** — created in `oran-o2ims` with `metadata.name` =
   `oCloudSiteName`, `spec.globalLocationName`, and the required `spec.description`
   (from `oCloudSiteDescription`, or a generated default) if not found. If it
   **already exists** but its `spec.globalLocationName` differs from the request's,
   the OnboardingRequest is set to `Validated=False, Reason=OCloudSiteConflict` and no
   ResourcePool is created — otherwise the pool and its hosts would be filed under a
   different Location than the request names.
3. **ResourcePool CR** — created in `oran-o2ims` with `metadata.name` = `resourcePoolName`, `spec.oCloudSiteName`, and the required `spec.description` (from `resourcePoolDescription`, or a generated default) if not found.
4. **Namespace** — created matching `resourcePoolName` if it does not exist, for BMH, Secret, and OnboardingServer resources. (Distinct from `oran-o2ims`.)
5. **OnboardingServer CRs** — one created per entry in `spec.servers`, in the resource pool namespace, owned by the OnboardingRequest.

If the ResourcePool already exists but references a different OCloudSite, the OnboardingRequest is set to `Validated=False, Reason=ResourcePoolConflict`. (The analogous parent check one level up — an existing OCloudSite pointing at a different Location — yields `OCloudSiteConflict`, per step 2.)

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

    // OnboardingStartedAt is when onboarding first began for this server. Onboarding
    // does not use a fixed retry-count budget — transient errors are retried with
    // backoff like any controller — but a server that has not reached Completed within
    // the pool-level spec.onboardingTimeout (measured from this time) goes terminal
    // Failed with reason OnboardingTimeout. This gives one clear deadline instead of
    // trying to define which errors consume a retry budget.
    OnboardingStartedAt *metav1.Time    `json:"onboardingStartedAt,omitempty"`

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
    // (reverted during cleanup). Rollback must handle "Requested", "Applied", AND
    // "Failed": a "Requested"-but-not-"Applied" op may already have taken effect, and
    // a "Failed" op can fail only after partially changing the BMC. A reversible op
    // in any of those phases is rolled back on cleanup.
    Phase string `json:"phase"`
    UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}
```

**Printcolumns:** Request (`spec.onboardingRequestName`), BmcAddress, State, Vendor, BMH, Age — the Request and BmcAddress columns keep the UUID-named CR legible (see [OnboardingServer CR naming](#1-api-design-crds))

### Deletion semantics

`spec.servers` is the **full desired set** of servers in the pool, and the
controller reconciles actual resources to match it. Membership is managed through
the OnboardingRequest (via `oc` or the REST `PATCH`), **never** by deleting BMHs or
OnboardingServer CRs out of band:

- **Add** an entry → the controller creates an OnboardingServer and onboards it.
- **Remove** an entry → the controller **de-onboards** that server: it restores the
  BMC, deletes the BMH and the per-server onboarding-managed resources, and removes
  the OnboardingServer once cleanup completes.
- **Delete the OnboardingRequest** → the controller de-onboards **every** server in
  the pool, then finishes request deletion after all cleanups complete.

This replaces an earlier "published BMHs must be manually deleted before the request
can be deleted" model. Request-driven de-onboarding makes the lifecycle one
desired-state reconciliation and removes the ambiguous case where an admin deletes a
BMH while its `spec.servers` entry remains (the controller would otherwise just
re-onboard it on the next reconcile).

**De-onboarding is the exact inverse of onboarding, and the controller — not the
administrator — restores the BMC.** It runs as the OnboardingServer finalizer
whenever a server is removed from `spec.servers`, the request is deleted, or the
OnboardingServer is deleted directly:

1. **Claim the BMH for de-onboarding via a `resourceVersion`-guarded write that
   serializes with allocation.** A plain "check not-allocated, then delete" — or even
   "set a hold, then re-check" — is not race-free: the two are separate reads/writes,
   so an allocator can pass its hold check, pause, and commit its claim after the
   finalizer's re-check. The hold and the allocation claim must **serialize on a single
   object**. Both sides write the **BMH** under an **optimistic-concurrency
   (`resourceVersion`) precondition** — the same primitive used in
   [ibi-server-pre-provisioning](ibi-server-pre-provisioning.md):
   - **Allocator** (hardware manager): claiming a host is a `resourceVersion`-guarded
     update to the BMH (its reservation marker / `AllocatedNode`-owner write). It first
     rejects any BMH carrying the de-onboard hold; if its guarded write **conflicts**
     (the BMH changed since it read it — e.g. the finalizer just set the hold), it
     **retries from selection** and then skips the now-held host.
   - **Finalizer**: sets the `onboarding.openshift.io/deonboarding` hold with a
     `resourceVersion` precondition. If that write **conflicts** (an allocator just
     claimed the host), it re-reads and treats the host as **in use** — a **reservation
     counts as in use even before the `AllocatedNode` exists** — and backs off (step 2).
     Only when the hold is written **and** no allocation/reservation is present does it
     proceed.
   Because both operations are `resourceVersion`-guarded writes to the same BMH, at
   most one wins; the loser observes the conflict and re-evaluates. **Abandoned
   reservations** (an allocator that set a reservation then crashed before creating the
   `AllocatedNode`) are reclaimable after a bounded staleness timeout, so a crashed
   allocation cannot block de-onboarding forever. **The allocator's `AllocatedNode`
   commit is itself fenced by the BMH `resourceVersion` observed at reservation:** the
   commit is a `resourceVersion`-guarded write to the BMH (promoting the reservation
   marker to the `AllocatedNode`-owner state), so a **resumed** allocator whose stale
   reservation was already reclaimed — and whose BMH the finalizer then de-onboarded
   (or another allocator re-claimed) — **conflicts and fails to commit**, and retries
   from selection. Reclamation and the commit can therefore never both succeed for the
   same reservation. (This serialization is required in both docs — see
   [Deletion semantics](#deletion-semantics).)
2. **Never tear down a host that is in use.** If the re-check in step 1 finds the BMH
   **allocated or provisioned**, cleanup is **blocked**: the finalizer clears the hold
   (so the host is not stranded), records that it is waiting, and requeues until the
   host is returned to the pool through the normal provisioning workflow (deprovision
   the cluster; the hardware manager deallocates it back to `available`), then retries
   from step 1.
3. **Wait for any in-flight BMC operation to settle, then restore the BMC**
   best-effort via the Metal3 APIs so the server returns to a cleanly
   **re-onboardable** state with **no manual BMC work**. Restore in an order that keeps
   the BMC reachable at its current `bmcAddress` until the end: **(a)** restore the
   pool-default credentials, **(b)** (LDAP mode) disable LDAP / re-enable local auth,
   and only **(c)** last, revert the BMC networking pin to DHCP — the single step that
   can change the BMC's address (releasing its static assignment lets the BMC take a new
   DHCP lease). Doing the address-affecting revert last means any **partial** restore
   that fails at (a) or (b) leaves the original static `bmcAddress` intact, so the
   `needs-bmc-reset` record (keyed by `bmcAddress`) stays reachable for the next
   request; the networking revert only runs once (a) and (b) have succeeded, i.e. when
   no reset gate is needed. Restoring the BMC is the controller's responsibility;
   failures are recorded (`cleanupStatus: partial`, with the reason and the step that
   failed) but do not wedge teardown.
4. **Delete the BMH** and the per-server ExternalSecret/Secret, then remove the
   finalizer. The OnboardingServer stays visible (reporting de-onboarding progress in
   `status`) until its cleanup completes.

**Admission guards (validating webhook) — advisory, with the finalizer as the real
authority:**

- **Reject removing a `spec.servers` entry, or deleting the request, while the
  server's OnboardingServer is in a non-terminal onboarding state** (mid-BMC-mutation,
  etc.) — let the in-flight operation settle first.
- **Reject removal/deletion while the associated BMH is allocated or provisioned** —
  the host is live; deprovision it first. The finalizer re-checks and waits, so a host
  that becomes allocated in the TOCTOU window is still never torn down.

Direct `oc delete onboardingserver` is likewise a **protected** operation: the
webhook rejects it while the BMH is in use, and the finalizer repeats the check as a
fallback (for an allocated BMH it reports cleanup is blocked and waits for
deallocation before proceeding). Note that deleting an OnboardingServer whose entry
is **still in `spec.servers`** is transient — the controller recreates and re-onboards
it to match desired state, so direct deletion is only useful as a manual retry (see
[Retrying a failed server](#retrying-a-failed-server)).

**Request deletion** then proceeds via a **finalizer** on the OnboardingRequest. The
webhook checks are advisory (a server can become allocated or enter a mutation in the
TOCTOU window), so the finalizer is the authority:

1. **Quiesce the children.** The `DeletionTimestamp` is a stop signal — the
   OnboardingServer controller starts no new onboarding work.
2. **Explicitly delete each OnboardingServer and wait for its finalizer** (the
   de-onboarding above) to complete — restoring and deleting each BMH while the
   shared credential Secrets still exist. It does **not** rely on owner-reference GC
   (under the default _background_ policy the owner can be removed before children
   finalize), so the service account needs `delete` on `onboardingservers`. If any
   server's BMH is still **in use**, this step **blocks and surfaces why** until the
   operator deprovisions those hosts — the one remaining case that needs operator
   action, and only because a running cluster depends on the host.
3. **Delete the shared credentials, and sweep any remaining per-server
   ExternalSecrets/Secrets** in the pool namespace (the credential ExternalSecrets
   carry [no owner reference](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior),
   so nothing else removes them), then remove the request finalizer **last**, so the
   OnboardingRequest outlives its children regardless of GC ordering.

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
| `PATCH .../v1/onboardingRequests/{onboardingRequestId}` | `Patch` OnboardingRequest | Amend a request — the mutations the spec allows: **add or remove** entries in `spec.servers` (adding onboards, removing de-onboards) and **correct** `bmcSubnet`/`bmcGateway` (the failed-pin recovery path). The webhook enforces the immutability/de-onboard/field rules, so illegal edits are rejected regardless of interface (see [Spec mutability](#1-api-design-crds)) |
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
`serialNumber`, `bmhName`, cleanup status) stays on
the OnboardingServer CR for `oc`-based inspection. Retrying a single failed server is
likewise a cluster-side operation (`oc delete onboardingserver …`, see
[Retrying a failed server](#retrying-a-failed-server)), not a REST call.

`POST`/`PATCH`/`DELETE` are conveniences that create/amend/delete the very same
OnboardingRequest CR an operator could `oc apply`/`oc patch`/`oc delete`. The
validating webhook ([Controllers](server-onboarding-implementation.md#7-controllers))
and the finalizer are enforced by the API server on the CR write itself, so they
apply identically no matter which interface originates the change — the REST facade
gains the de-onboard/immutability/deletion-safety checks for free. `PATCH` closes
the asymmetry where a request could be created and deleted via REST but only
_amended_ via `oc`; the mutable operations (add/remove servers, correct
`bmcSubnet`/`bmcGateway`) are now first-class in both interfaces.

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

- Handlers call `HubClient.List/Get/Create/Patch/Delete` on the CRs and translate
  Kubernetes errors into O-RAN `ProblemDetails` responses — the same mapping
  `internal/service/provisioning` already uses: `IsNotFound` → `404`,
  `IsAlreadyExists` → `409`, `IsForbidden`/`IsBadRequest`/`IsInvalid` (API-server
  and webhook validation, including a rejected illegal amend) → `400`, otherwise
  `500`. The `Patch` handler applies the client-supplied partial update to the named
  OnboardingRequest, letting the webhook arbitrate legality. It **requires optimistic
  concurrency** (a `resourceVersion` precondition) for any change to `spec.servers`:
  a merge patch replaces the whole array, so two clients that both read `[A]` and
  separately patch `[A,B]` and `[A,C]` would otherwise have the second silently drop
  `B` — and dropping a completed, unallocated entry would **de-onboard** it. With the
  precondition, a stale update is rejected (`409`) and the client must re-read and
  retry. The same explicit precondition is required via `oc` — `oc replace` (which
  sends the read `resourceVersion`) or `oc patch --resource-version=<rv>`. Server-side
  apply is **not** a substitute: its field-ownership conflict detection does not
  enforce a resource-version precondition, so a concurrent array replacement can still
  be lost.
  (Artifacts is read-only, so it
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
  onboarding role additionally receives `POST`/`PATCH`/`DELETE` on
  `onboardingRequests`. That privileged role is a **new** write-capable role modeled
  on the existing `provisioner-role` / `subscriber-role` (which grant
  `create`/`delete`), **not** the `deployer-role` (which is read-only on
  `deploymentManagers`). There are no
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
    │          (BMO cannot register/authenticate with the BMH's credentials)
    │  Create BMH: inspectionMode: fast, inventory-hold marker. Credentials: in
    │  v1 the server's provided credentials; in the credential-provisioning phase
    │  the shared default credentials (then swapped at UpdatingCredentials). See
    │  "Credentials: v1 vs credential provisioning" below.
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
    │  Remove inventory-hold marker, then WAIT until the host's O2 IMS Resource is
    │  actually present/queryable in inventory (not just the marker removed).
    ▼
DisablingLocalAccounts (authMode=ldap + disableLocalAccounts only; skipped otherwise)
    │  Disable local BMC accounts via the Metal3 LDAP/credentials API
    │  (irreversible). Best-effort: the host is already published and works over
    ▼  LDAP, so this never fails the server — it proceeds to Completed regardless.
Completed (terminal) — host is queryable in the O2 IMS inventory

Any non-terminal state EXCEPT DisablingLocalAccounts ──► Failed (on an unrecoverable
error, or reason=OnboardingTimeout when the server has not reached Completed within
the pool-level spec.onboardingTimeout; transient errors are retried with backoff, not
counted against a fixed budget). DisablingLocalAccounts is best-effort and always
ends at Completed.
On VendorMismatch / HwMismatch the controller deletes the (still-held) BMH.
```

**Credentials: v1 vs credential provisioning.** The diagram above shows the full
(post-v1) flow, in which the BMH is created in `Inspecting` with the pool's **shared
default credentials** and switched to per-server credentials at `UpdatingCredentials`
(the credential-provisioning phase). **In v1 there is no `UpdatingCredentials` state**,
so the BMH is created and inspected with **each server's own provided credentials**
(`servers[].desiredCredentialsVaultSecret`, used as-is — the credentials the BMC
already has), and no swap occurs. The shared `defaultCredentialsVaultSecret` and the
default→per-server swap belong to the credential-provisioning phase; until it lands, a
BMC whose current credentials the operator does not supply per server fails at
`AuthFailed` in `Inspecting`. (A homogeneous pool whose BMCs still carry factory
defaults can point every `desiredCredentialsVaultSecret` at the one shared default
entry.) The `Inspecting`/BMH-creation description in the implementation companion
reflects both cases.

The detailed, per-state behavior — what the controller does in each state above,
including `CreatingExternalSecrets`, `Inspecting`, `Inventorying`,
`ApplyingHardwareProfile`, `PinningBmcAddress`, the credential/LDAP states,
`Publishing`, and `DisablingLocalAccounts` — is in
[Server onboarding state machine — detailed per-state behavior](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior)
in the implementation document. The subsections below cover lifecycle operations
that apply across states.

### Retrying a failed server

A server that lands in a terminal failure state (`AuthFailed`, `VendorMismatch`,
`HwMismatch`, or `Failed`) is retried **per server**, without touching the rest of
the pool, in either of two equivalent ways:

- **Request-driven (recommended).** Remove the server's entry from `spec.servers`,
  wait for its de-onboarding to finish (the controller restores the BMC and deletes
  the held BMH — see [Deletion semantics](#deletion-semantics)), fix the external
  cause, then add the entry back for a fresh attempt.
- **Direct delete (manual shortcut).** `oc delete onboardingserver -n <pool>
  <name>`. The entry is still in `spec.servers`, so the controller recreates it as
  `Pending` and re-onboards. The finalizer first cleans up the **held** BMH (never
  published for a failed server) and restores the BMC; the recreate then starts clean
  **unless the restore was only partial** — in that case the durable
  `needs-bmc-reset` marker (below) holds the recreate until the operator resets the
  BMC, since the OnboardingServer's own `cleanupStatus` is lost when the object is
  deleted.

The important change from a naive retry: **de-onboarding restores the BMC** (reverts
the NIC pin, restores pool-default credentials, disables LDAP). So in the normal case
there is **no "post-mutation failure needs a manual BMC reset" step** — even a
`Failed` reached during/after `UpdatingCredentials` or `ConfiguringLdap` is recovered
by the controller returning the BMC to the pool-default state before the retry, rather
than the operator doing an out-of-band reset.

**Exception — partial restore.** BMC restore is best-effort: if it cannot complete it
ends `partial` and the BMC may **retain non-default credentials (or LDAP config)**. A
retry then created with the provided/default credentials would fail at `AuthFailed`.
The `OnboardingServer`'s `cleanupStatus` is **lost when that object is deleted** (direct
delete, or the de-onboard that removes it), so the partial outcome must be recorded in
durable state. It **cannot** live on the `OnboardingRequest`: a partial restore also
happens during **request deletion** (de-onboarding the whole pool), and the request
finalizer then deletes the request — losing the marker and letting a **brand-new
request for the same BMC** re-onboard without the required reset. The partial outcome is
therefore persisted to a resource that **outlives both the OnboardingServer and the
OnboardingRequest**: a per-BMC **`needs-bmc-reset` record** (a small operator-owned CR —
or a ConfigMap — in the operator namespace, carrying the attempted restore step). The
**gate is checked before any BMC I/O**, so it must key on something known **without
inspecting** the host. The only pre-inspection identifier is the **`bmcAddress`** from
`spec.servers[]`, so that is the **authoritative gate key** (the record also stores the
BMC management NIC MAC, learned during the earlier onboarding's inspection, but only as a
**secondary correlation attribute** — the MAC is not available before inspection, so it
cannot serve the pre-inspection check). Keying on `bmcAddress` is sound here because the
design keeps the address **stable for as long as a gate exists**: the DHCP→static pin
reverts to the **same leased IP** (backed by the durable per-MAC reservation / IPAM
exclusion that is a BMC networking prerequisite), and the restore order above reverts
networking **last**, so a partial restore that fails at an earlier step never reaches the
address-affecting revert. Thus whenever a `needs-bmc-reset` record is written, the BMC
still answers at the same `bmcAddress` a later request will use, and the pre-inspection
check matches. The request finalizer must **write this record before it removes the
request's finalizer**, so a partial restore is never dropped by request deletion.
**Before onboarding any server** — on first onboarding and on every retry/recreate,
regardless of which request owns it — the controller **checks for a `needs-bmc-reset`
record matching the server's `bmcAddress`**; if one is present it holds the server in a
`Blocked`/`NeedsBmcReset` state and does **not** proceed.

**Changed-address edge (unsupported configuration).** If the durable per-MAC reservation
prerequisite is **not** satisfied and the BMC's address nonetheless changes, a new
request arriving with the new `bmcAddress` will **not** match the old record
pre-inspection — the "regardless of which request owns it" guarantee holds only for the
supported, stable-address configuration. This case still **fails safe**: the BMC retains
non-default credentials from the partial restore, so onboarding cannot authenticate and
stops at `AuthFailed` rather than completing into a bad state. The MAC recorded on the
record lets the operator correlate that `AuthFailed` with the earlier partial restore
(the MAC becomes available once inspection is attempted).

In the supported case, while the gate is set the operator resets the BMC out of band
(reset credentials / disable LDAP — the same action the `AuthFailed` guidance describes)
and **deletes the record** to confirm; only then does onboarding proceed. This keeps a partial restore from silently looping into
`AuthFailed`, even across request deletion and re-creation.

Fix the root cause first — bad default-credentials/vault entry (`AuthFailed`), wrong
hardware model (`VendorMismatch` / `HwMismatch`), or vault/ESO connectivity
(`Failed`). (A `Failed` in `ApplyingHardwareProfile` may have already flashed
firmware/BIOS; that hardware state is a baseline the recreated run simply re-applies,
so it converges regardless.)

### Deleting a Completed server (idempotent re-onboarding)

Deleting a `Completed` OnboardingServer whose entry is **still in `spec.servers`** is
different from **removing** that entry: the former is a transient CR delete (the
server is still desired, so the controller recreates it as `Pending`); the latter is
a **de-onboard** (the server is no longer desired, so the controller restores the BMC
and deletes the BMH — see [Deletion semantics](#deletion-semantics)). The finalizer
tells them apart: if the entry is still desired and the request is not being deleted,
it **retains** the published BMH and credentials (no de-onboard); only an entry
removal or request deletion triggers teardown.

For the transient-delete case, naively re-running the state machine would be
harmful: the BMC now holds the per-server desired (or LDAP) credentials, so a fresh
`Inspecting` run — which creates the BMH with the pool default credentials — would
fail to register (→ `AuthFailed`) even though a perfectly healthy, published BMH
already exists.

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
Combined with the finalizer **retaining** the BMH and per-server credentials when the
entry is still desired (no de-onboard — see
[Cleanup mechanism](server-onboarding-implementation.md#server-onboarding-state-machine--detailed-per-state-behavior)),
deleting a `Completed` OnboardingServer whose entry remains is therefore a safe
no-op: the CR is cleaned up without touching the live BMH or its credentials,
recreated as `Pending`, and immediately reconciled back to `Completed` against the
existing BMH.

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
  onboarding-owned Secrets in the resource pool namespace; the **host of the rendered
  `spec.bmc.address` URL must equal the server's declared `bmcAddress`** (an exact
  per-server match, not merely "inside `bmcSubnet`" — a subnet check alone is
  insufficient, see below); and `disableCertificateVerification` must follow the pool
  policy (`spec.disableBmcCertificateVerification`) rather than being set freely.
- **The approved destinations must be protected independently of the controller
  service account.** The checks above pin a BMH to a _declared_ `bmcAddress`, but the
  declared set and `bmcSubnet` live in the OnboardingRequest spec — so if the same
  compromised controller SA could **edit that spec** (append an attacker `bmcAddress`,
  broaden `bmcSubnet`), it could pre-approve its own endpoint and then pass the BMH
  webhook. To close this, **the controller service account has no write access to the
  OnboardingRequest spec**: it is granted only `onboardingrequests/status` and
  `onboardingrequests/finalizers` (not `update`/`patch` on the resource itself).
  Appending servers or changing `bmcSubnet`/`bmcGateway` is a **user-driven** spec
  change requiring separate RBAC a compromised controller does not hold. The
  sensitive `create` on `baremetalhosts` is thus bounded by an approved-destination
  set the controller cannot unilaterally expand. A **compromised-service-account
  test** (controller SA attempts to add an endpoint / widen the subnet / create an
  off-destination BMH, and is rejected) is part of the verification plan.
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
  | `onboarding.openshift.io` | `onboardingrequests` | get, list, watch | Reconcile. **No `update`/`patch` on the resource** — the controller must not edit the request spec (declared servers, `bmcSubnet`/`bmcGateway`), so a compromised controller cannot pre-approve its own BMC destination (see [Security Considerations](#4-security-considerations)) |
  | `onboarding.openshift.io` | `onboardingrequests/status` | get, update, patch | Update summary counts / conditions |
  | `onboarding.openshift.io` | `onboardingrequests/finalizers` | update | Manage the request finalizer without granting spec write |
  | `onboarding.openshift.io` | `onboardingservers` | get, list, watch, create, update, patch, delete | Create one per server entry; drive reconcile; `delete` to explicitly tear children down and wait for their finalizers during request deletion (not relying on owner-reference GC) |
  | `onboarding.openshift.io` | `onboardingservers/status` | get, update, patch | Record state machine state |
  | `ocloud.openshift.io` | `locations` | get, list | Prerequisite lookup (must exist) |
  | `ocloud.openshift.io` | `ocloudsites` | get, list, create | Create if not found |
  | `ocloud.openshift.io` | `resourcepools` | get, list, create | Create if not found |
  | (core) | `namespaces` | get, list, create | Resource pool namespace |
  | `external-secrets.io` | `externalsecrets` | get, list, watch, create, delete | Create shared/per-server ExternalSecrets; delete on finalizer/cleanup |
  | (core) | `secrets` | delete | **Namespace-scoped** (`Role` + per-pool `RoleBinding`). Deletes per-server and shared ESO-synced Secrets (both in the pool namespace) by name. Controller never reads Secret data (readiness from `ExternalSecret` `.status`; BMO reads it) — so no `get`/`list`/`watch` |
  | `metal3.io` | `baremetalhosts` | get, list, watch, create, update, patch, delete | Create the BMH (`inspectionMode: fast`, hold marker); `patch` for BMC config and to clear the hold annotation (`Publishing`); watch conditions; delete a BMH on de-onboard when **not in use** (code-constrained) |
  | `metal3.io` | `allocatednodes`; `clcm.openshift.io` `nodeallocationrequests` | get, list, watch | Check whether a BMH is **allocated/provisioned** (in use by a cluster) before de-onboarding — the finalizer refuses to tear down an in-use host |
  | `metal3.io` | BMC networking / credentials / LDAP config resources (exact kinds TBD — may be BareMetalHost sub-resources or companion CRs) | get, list, watch, create, update, patch, delete | Declare and track the DHCP→static pin + DNS, credential provisioning, and LDAP config that BMO applies over Redfish. The precise resources/verbs depend on the final Metal3 API — see [Dependency on forthcoming Metal3 APIs](#dependency-on-forthcoming-metal3-apis) |
  | `metal3.io` | `hardwaredata` | get, list, watch | Read the `HardwareData` CR produced by fast out-of-band (Redfish) inspection for vendor/hardware matching (`Inventorying`) |
  | `clcm.openshift.io` | `hardwareprofiles` | get, list | Read the referenced `HardwareProfile` for the firmware/BIOS baseline (`ApplyingHardwareProfile`); only when `hardwareProfileName` is set |
  | `metal3.io` | `hostfirmwaresettings`, `hostfirmwarecomponents`, `hostupdatepolicies` | get, list, watch, create, update, patch, delete | Render the profile into servicing CRs on the held BMH and watch convergence (`ApplyingHardwareProfile`) |
  | `metal3.io` | `firmwareschemas` | get, list, watch | Read the BMH's `FirmwareSchema` to validate BIOS settings in the reused `HostFirmwareSettings` translation (`ApplyingHardwareProfile`); the grant every controller on this path already holds |
  | (core) | `events` | create, patch | Emit events on state transitions and failures |
  | `coordination.k8s.io` | `leases` | get, list, watch, create, update | Controller-runtime leader election |

  The service account needs `delete` on `baremetalhosts` because the BMH is created
  during onboarding and torn down on **de-onboarding** (a server removed from
  `spec.servers`, or the request deleted) as well as when a server fails to match
  before it is published. Kubernetes RBAC cannot express "only a BMH that is not in
  use," so the constraint is **enforced in controller logic**: the finalizer deletes a
  BMH only after confirming it is **not allocated or provisioned** (no referencing
  AllocatedNode / NodeAllocationRequest, not past `available`) and after restoring the
  BMC; an in-use host blocks de-onboarding until it is deprovisioned through the normal
  provisioning workflow (see [Deletion semantics](#deletion-semantics)). The `delete`
  grant is scoped to BMHs only; the service account has no `delete` on
  `ocloudsites`/`resourcepools`.
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
