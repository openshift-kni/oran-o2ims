# FirmwareCatalog CRD

```yaml
title: firmware-catalog
authors:
  - @browsell
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-04-21
last-updated: 2026-04-21
```

## Table of Contents

- [FirmwareCatalog CRD](#firmwarecatalog-crd)
  - [Table of Contents](#table-of-contents)
  - [Summary](#summary)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
  - [Current State](#current-state)
  - [Proposed Design](#proposed-design)
    - [FirmwareCatalog Resource](#firmwarecatalog-resource)
      - [API Definition](#api-definition)
      - [Example CR](#example-cr)
      - [Lifecycle](#lifecycle)
    - [HardwareProfile Changes](#hardwareprofile-changes)
      - [Updated API Definition](#updated-api-definition)
      - [Example CR (Before)](#example-cr-before)
      - [Example CR (After)](#example-cr-after)
    - [Controller Changes](#controller-changes)
      - [Catalog Resolution](#catalog-resolution)
      - [Integration Points](#integration-points)
    - [Validation](#validation)
  - [Impact](#impact)
    - [Breaking Changes](#breaking-changes)
    - [Files to Create or Modify](#files-to-create-or-modify)
  - [CR Relationships](#cr-relationships)

## Summary

Introduce a new `FirmwareCatalog` CRD that serves as a centralized registry of firmware
images. Today, each HardwareProfile embeds firmware download URLs and version strings
inline. This couples firmware image management to hardware profile definitions, causing
duplication when multiple profiles use the same firmware and requiring profile recreation
to change a URL.

The FirmwareCatalog decouples these concerns: firmware images are defined once in a
shared catalog, and HardwareProfiles reference catalog entries by name. This enables
reuse across profiles and centralized management of firmware image metadata.

## Goals

- Define a single, shared catalog of firmware images that can be populated and updated
  by users at runtime.
- Allow HardwareProfiles to reference firmware images by name rather than embedding
  URL and version inline.
- Minimize changes to the existing controller reconciliation logic by resolving catalog
  references into the same data structures the downstream code already consumes.

## Non-Goals

- Firmware image hosting or download management.
- Automatic discovery or population of firmware images.
- Multi-namespace or cluster-scoped catalog support.
- Versioning or approval workflows for catalog changes.

## Current State

The `HardwareProfile` CR defines firmware targets as inline objects with `url` and
`version` fields:

```go
type Firmware struct {
    Version string `json:"version,omitempty"`
    URL     string `json:"url,omitempty"`
}

type Nic struct {
    Version string `json:"version,omitempty"`
    URL     string `json:"url,omitempty"`
}

type HardwareProfileSpec struct {
    Bios         Bios      `json:"bios"`
    BiosFirmware Firmware  `json:"biosFirmware,omitempty"`
    BmcFirmware  Firmware  `json:"bmcFirmware,omitempty"`
    NicFirmware  []Nic     `json:"nicFirmware,omitempty"`
}
```

The Metal3 hardware plugin controller reads these fields directly and translates them
into Metal3 `HostFirmwareComponents` updates. Key functions that consume these fields:

- `validateFirmwareUpdateSpec` -- validates URLs and versions
- `convertToFirmwareUpdates` -- builds Metal3 `FirmwareUpdate` objects
- `isVersionChangeDetected` -- compares desired vs. current firmware versions
- `IsFirmwareUpdateRequired` -- entry point for firmware update logic
- `validateFirmwareVersions` -- post-update version validation
- `validateAppliedBiosSettings` -- post-update BIOS validation (including NIC firmware)

## Proposed Design

### FirmwareCatalog Resource

A new namespaced CRD in the `clcm.openshift.io/v1alpha1` API group. The operator
maintains a singleton instance with a fixed well-known name. Users populate and update
the `spec.images` list at runtime.

#### API Definition

File: `api/hardwaremanagement/v1alpha1/firmwarecatalog_types.go`

```go
type FirmwareImage struct {
    // Name is a unique identifier for this firmware image within the catalog.
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // Component identifies the hardware component type.
    // +kubebuilder:validation:Enum=bios;bmc;nic
    Component string `json:"component"`

    // URL points to the firmware image file.
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:Pattern=`^(http|https)://.*$`
    URL string `json:"url"`

    // Version is the firmware version string.
    // +kubebuilder:validation:MinLength=1
    Version string `json:"version"`

    // Description is an optional human-readable description of the firmware image.
    // +optional
    Description string `json:"description,omitempty"`
}

type FirmwareCatalogSpec struct {
    // Images is the set of firmware images available in this catalog.
    // +optional
    // +listType=map
    // +listMapKey=name
    Images []FirmwareImage `json:"images,omitempty"`
}

type ImageValidationStatus struct {
    // Name is the name of the firmware image entry.
    Name string `json:"name"`

    // Valid indicates whether the image entry passed validation.
    Valid bool `json:"valid"`

    // Reason provides a machine-readable reason for the validation result.
    // +optional
    Reason string `json:"reason,omitempty"`

    // Message provides a human-readable description of the validation result.
    // +optional
    Message string `json:"message,omitempty"`
}

type FirmwareCatalogStatus struct {
    // +operator-sdk:csv:customresourcedefinitions:type=status
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // ImageStatuses reports the validation result for each image in the catalog.
    // +optional
    // +listType=map
    // +listMapKey=name
    // +operator-sdk:csv:customresourcedefinitions:type=status
    ImageStatuses []ImageValidationStatus `json:"imageStatuses,omitempty"`

    // +patchMergeKey=type
    // +patchStrategy=merge
    // +listType=map
    // +listMapKey=type
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=status
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=firmwarecatalogs,scope=Namespaced
// +kubebuilder:resource:shortName=fwcatalog;fwcatalogs
type FirmwareCatalog struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   FirmwareCatalogSpec   `json:"spec,omitempty"`
    Status FirmwareCatalogStatus `json:"status,omitempty"`
}
```

#### Example CR

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: FirmwareCatalog
metadata:
  name: firmware-catalog
  namespace: oran-o2ims
spec:
  images:
  - name: dell-xr8620t-bios-2.3.5
    component: bios
    url: https://example.com:8888/firmware/xr8620t/BIOS_JDR1R_WN64_2.3.5.EXE
    version: "2.3.5"
  - name: dell-xr8620t-bmc-7.10.70.10
    component: bmc
    url: https://example.com:8888/firmware/xr8620t/iDRAC-with-Lifecycle-Controller_Firmware_W4NV9_WN64_7.10.70.10_A00.EXE
    version: "7.10.70.10"
  - name: broadcom-nic-25.2.3
    component: nic
    url: https://example.com:8888/firmware/nic/broadcom-25.2.3.bin
    version: "25.2.3"
  - name: dell-xr8620t-bios-2.6.3
    component: bios
    url: https://example.com:8888/firmware/xr8620t/BIOS_JDR1R_WN64_2.6.3.EXE
    version: "2.6.3"
  - name: dell-xr8620t-bmc-7.20.30.50
    component: bmc
    url: https://example.com:8888/firmware/xr8620t/iDRAC-with-Lifecycle-Controller_Firmware_W4NV9_WN64_7.20.30.50_A00.EXE
    version: "7.20.30.50"
```

#### Lifecycle

- The operator creates the singleton `FirmwareCatalog` CR (with an empty `spec.images`
  list) on startup if it does not already exist. It never overwrites user content.
- Users add new entries or delete unreferenced entries in `spec.images`. Entries are
  immutable once created — `component`, `url`, and `version` cannot be changed in
  place. Deletion is blocked if any HardwareProfile references the entry.
- A lightweight FirmwareCatalog controller reconciles on spec changes and validates
  each image entry (URL format, component type). Validation results are written to
  `status.imageStatuses`. An overall `Validation` condition is set to `True` when all
  entries are valid, or `False` if any entry fails validation.

### HardwareProfile Changes

The `biosFirmware`, `bmcFirmware`, and `nicFirmware` fields change from inline structs
to strings that reference entries in the singleton FirmwareCatalog by name.

The existing HardwareProfile immutability rule (`rule="oldSelf.spec == self.spec"`)
is unchanged. HardwareProfile specs remain fully immutable after creation. This is
consistent with the current Day-2 firmware update workflow, which already requires
creating a new HardwareProfile and updating the ProvisioningRequest's
`hwMgmtParameters.nodeGroupData[].hwProfile` to reference it. The FirmwareCatalog
does not change this workflow — it only changes what the firmware fields contain
(catalog entry names instead of inline url/version).

#### Updated API Definition

File: `api/hardwaremanagement/v1alpha1/hardwareprofile_types.go`

```go
type HardwareProfileSpec struct {
    // Bios defines a set of bios attributes.
    // +operator-sdk:csv:customresourcedefinitions:type=spec
    Bios Bios `json:"bios"`

    // BiosFirmware is the name of a firmware image entry in the FirmwareCatalog
    // for BIOS firmware.
    // +optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec
    BiosFirmware string `json:"biosFirmware,omitempty"`

    // BmcFirmware is the name of a firmware image entry in the FirmwareCatalog
    // for BMC firmware.
    // +optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec
    BmcFirmware string `json:"bmcFirmware,omitempty"`

    // NicFirmware is a list of firmware image entry names in the FirmwareCatalog
    // for NIC firmware.
    // +optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec
    NicFirmware []string `json:"nicFirmware,omitempty"`
}
```

The `Firmware` and `Nic` struct types are removed from the public API. They become
internal types used after catalog resolution within the controller.

#### Example CR (Before)

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: HardwareProfile
metadata:
  name: dell-xr8620t-bios-2.3.5-bmc-7.10.70.10
  namespace: oran-o2ims
spec:
  bios:
    attributes:
      SysProfile: Custom
      WorkloadProfile: TelcoOptimizedProfile
      SriovGlobalEnable: Enabled
      AcPwrRcvryUserDelay: 120
  biosFirmware:
    version: 2.3.5
    url: https://example.com:8888/firmware/xr8620t/BIOS_JDR1R_WN64_2.3.5.EXE
  bmcFirmware:
    version: 7.10.70.10
    url: https://example.com:8888/firmware/xr8620t/iDRAC-with-Lifecycle-Controller_Firmware_W4NV9_WN64_7.10.70.10_A00.EXE
```

#### Example CR (After)

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: HardwareProfile
metadata:
  name: dell-xr8620t-bios-2.3.5-bmc-7.10.70.10
  namespace: oran-o2ims
spec:
  bios:
    attributes:
      SysProfile: Custom
      WorkloadProfile: TelcoOptimizedProfile
      SriovGlobalEnable: Enabled
      AcPwrRcvryUserDelay: 120
  biosFirmware: dell-xr8620t-bios-2.3.5
  bmcFirmware: dell-xr8620t-bmc-7.10.70.10
```

### Controller Changes

#### Catalog Resolution

A new resolution function fetches the singleton FirmwareCatalog and converts entry
names into the internal `Firmware`/`Nic` structs that existing downstream code expects.
This is the only new logic required; all existing firmware update, validation, and
comparison code operates on the resolved values without modification.

```go
const FirmwareCatalogName = "firmware-catalog"

// resolvedFirmware holds the url/version pairs resolved from catalog entry names.
type resolvedFirmware struct {
    BiosFirmware Firmware
    BmcFirmware  Firmware
    NicFirmware  []Nic
}

// resolveFirmwareFromCatalog looks up firmware entry names from the HardwareProfile
// in the singleton FirmwareCatalog and returns resolved url/version pairs.
func resolveFirmwareFromCatalog(ctx context.Context, c client.Client,
    namespace string, spec HardwareProfileSpec) (resolvedFirmware, error) {

    catalog := &FirmwareCatalog{}
    if err := c.Get(ctx, types.NamespacedName{
        Name: FirmwareCatalogName, Namespace: namespace,
    }, catalog); err != nil {
        return resolvedFirmware{}, fmt.Errorf("failed to get FirmwareCatalog: %w", err)
    }

    imageMap := make(map[string]FirmwareImage, len(catalog.Spec.Images))
    for _, img := range catalog.Spec.Images {
        imageMap[img.Name] = img
    }

    var resolved resolvedFirmware

    if spec.BiosFirmware != "" {
        img, ok := imageMap[spec.BiosFirmware]
        if !ok {
            return resolvedFirmware{},
                fmt.Errorf("biosFirmware entry %q not found in FirmwareCatalog", spec.BiosFirmware)
        }
        if img.Component != "bios" {
            return resolvedFirmware{},
                fmt.Errorf("biosFirmware entry %q has component %q, expected bios", spec.BiosFirmware, img.Component)
        }
        resolved.BiosFirmware = Firmware{URL: img.URL, Version: img.Version}
    }

    if spec.BmcFirmware != "" {
        img, ok := imageMap[spec.BmcFirmware]
        if !ok {
            return resolvedFirmware{},
                fmt.Errorf("bmcFirmware entry %q not found in FirmwareCatalog", spec.BmcFirmware)
        }
        if img.Component != "bmc" {
            return resolvedFirmware{},
                fmt.Errorf("bmcFirmware entry %q has component %q, expected bmc", spec.BmcFirmware, img.Component)
        }
        resolved.BmcFirmware = Firmware{URL: img.URL, Version: img.Version}
    }

    for _, name := range spec.NicFirmware {
        img, ok := imageMap[name]
        if !ok {
            return resolvedFirmware{},
                fmt.Errorf("nicFirmware entry %q not found in FirmwareCatalog", name)
        }
        if img.Component != "nic" {
            return resolvedFirmware{},
                fmt.Errorf("nicFirmware entry %q has component %q, expected nic", name, img.Component)
        }
        resolved.NicFirmware = append(resolved.NicFirmware, Nic{URL: img.URL, Version: img.Version})
    }

    return resolved, nil
}
```

#### Integration Points

The resolution function is called at the start of each code path that consumes firmware
data from the HardwareProfile. These are the functions that currently read
`spec.BiosFirmware`, `spec.BmcFirmware`, or `spec.NicFirmware` directly:

| Function | File | Change |
|----------|------|--------|
| `IsFirmwareUpdateRequired` | `hostfirmwarecomponents_manager.go` | Call `resolveFirmwareFromCatalog`, pass resolved values to `validateFirmwareUpdateSpec`, `isVersionChangeDetected`, etc. |
| `validateFirmwareVersions` | `helpers.go` | Resolve catalog entries before comparing against `HostFirmwareComponents` status |
| `validateAppliedBiosSettings` | `helpers.go` | Resolve catalog entries for NIC firmware validation |
| `processHwProfileWithHandledError` | (caller of the above) | Pass namespace through for catalog lookup |

All downstream functions (`validateFirmwareUpdateSpec`, `convertToFirmwareUpdates`,
`isVersionChangeDetected`, `validateHFCHasRequiredComponents`) continue to accept the
internal `Firmware`/`Nic` types and require no changes.

### Validation

| Rule | Enforcement |
|------|-------------|
| Image names are unique within the catalog | `+listMapKey=name` on the API type (enforced by the API server) |
| Catalog entries are immutable (component, url, version cannot change) | CEL validation rule on `FirmwareCatalogSpec` |
| Catalog entries cannot be deleted while referenced by a HardwareProfile | Validating webhook on FirmwareCatalog |
| Referenced firmware entry exists in the catalog | Validating webhook on HardwareProfile (create) |
| Entry component type matches usage (e.g. `bios` entry used for `biosFirmware`) | Validating webhook on HardwareProfile (create) |
| Singleton FirmwareCatalog exists | Operator startup; creates if missing |
| Firmware URL is valid | Existing `validateFirmwareUpdateSpec` (unchanged, operates on resolved values) |

### Catalog Entry Immutability

Catalog entries are immutable once created. Users may add new entries or delete
existing entries, but may not modify an entry's `component`, `url`, or `version`
fields in place. This is enforced by a CEL validation rule on the CRD:

```yaml
// +kubebuilder:validation:XValidation:message="Firmware catalog entries are immutable",
//   rule="oldSelf.images.all(old, self.images.exists(cur, cur.name == old.name) ?
//     self.images.filter(cur, cur.name == old.name)[0].component == old.component &&
//     self.images.filter(cur, cur.name == old.name)[0].url == old.url &&
//     self.images.filter(cur, cur.name == old.name)[0].version == old.version : true)"
```

The `description` field is exempt from this rule and may be updated freely.

This prevents two classes of problems:

1. **Silent drift:** If a catalog entry's URL or version could be changed in place,
   existing HardwareProfiles would silently resolve to different firmware than what was
   originally applied. The ProvisioningRequest would appear fulfilled but with
   mismatched firmware.
2. **Race conditions:** If a catalog entry changes during an in-progress firmware
   update, the post-update validation step (`validateFirmwareVersions`) would compare
   the newly resolved version against the firmware that was just applied (the original
   version), causing a false validation failure.

To roll out new firmware, users create a new catalog entry with a new name and create
a new HardwareProfile referencing it, then update the ProvisioningRequest to point to
the new HardwareProfile.

### Catalog Entry Deletion Protection

Deletion of a catalog entry is only allowed if no HardwareProfile references it. A
validating webhook on the FirmwareCatalog rejects updates that remove entries still
referenced by any HardwareProfile in the namespace. The webhook lists all
HardwareProfiles and checks their `biosFirmware`, `bmcFirmware`, and `nicFirmware`
fields against the set of entries being removed.

### Validating Webhooks

Two validating webhooks provide admission-time validation, giving users immediate
feedback rather than deferred errors at reconciliation time.

**HardwareProfile webhook** — validates on create:

- Fetches the singleton FirmwareCatalog from the same namespace.
- Verifies that each firmware reference (`biosFirmware`, `bmcFirmware`,
  `nicFirmware[]`) exists as an entry in the catalog.
- Verifies that the referenced entry's `component` field matches the expected type
  (e.g. a `biosFirmware` reference must point to an entry with `component: bios`).
- Rejects the request with a descriptive error if any reference is invalid.

Since HardwareProfile specs are immutable, only `ValidateCreate` requires catalog
validation. `ValidateUpdate` and `ValidateDelete` do not need firmware checks.

This follows the existing webhook pattern established by the ProvisioningRequest
webhook in `api/provisioning/v1alpha1/provisioningrequest_webhook.go`, using a
`CustomValidator` with a client to perform cross-resource lookups at admission time.

```go
// +kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-hardwareprofile,
//   mutating=false,failurePolicy=fail,sideEffects=None,
//   groups=clcm.openshift.io,resources=hardwareprofiles,
//   verbs=create,versions=v1alpha1,
//   name=hardwareprofiles.clcm.openshift.io,
//   admissionReviewVersions=v1

type hardwareProfileValidator struct {
    client.Client
}

func (v *hardwareProfileValidator) ValidateCreate(
    ctx context.Context, obj runtime.Object,
) (admission.Warnings, error) {
    // Fetch singleton FirmwareCatalog
    // For each non-empty firmware reference, look up the entry
    // Validate entry exists and component matches
}
```

**FirmwareCatalog webhook** — validates on update:

- Compares old and new `spec.images` to identify removed entries.
- For each removed entry, lists HardwareProfiles in the namespace and checks if any
  reference the entry by name.
- Rejects the update if any removed entry is still referenced.

```go
// +kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-firmwarecatalog,
//   mutating=false,failurePolicy=fail,sideEffects=None,
//   groups=clcm.openshift.io,resources=firmwarecatalogs,
//   verbs=update,versions=v1alpha1,
//   name=firmwarecatalogs.clcm.openshift.io,
//   admissionReviewVersions=v1

type firmwareCatalogValidator struct {
    client.Client
}

func (v *firmwareCatalogValidator) ValidateUpdate(
    ctx context.Context, oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
    // Diff old vs new images to find removed entries
    // List HardwareProfiles in namespace
    // Reject if any removed entry is referenced
}
```

## Impact

### Breaking Changes

The `biosFirmware` and `bmcFirmware` fields change from object type to string type.
The `nicFirmware` field changes from `[]object` to `[]string`. This is a breaking CRD
schema change:

- Existing HardwareProfile CRs must be recreated with the new field format.
- The `Firmware` and `Nic` struct types are removed from the public API.
- Any external tooling that constructs HardwareProfile objects using the old struct
  format must be updated.

### Files to Create or Modify

| File | Action |
|------|--------|
| `api/hardwaremanagement/v1alpha1/firmwarecatalog_types.go` | **Create** -- CRD type definitions |
| `api/hardwaremanagement/v1alpha1/hardwareprofile_webhook.go` | **Create** -- HardwareProfile validating webhook |
| `api/hardwaremanagement/v1alpha1/firmwarecatalog_webhook.go` | **Create** -- FirmwareCatalog validating webhook |
| `api/hardwaremanagement/v1alpha1/hardwareprofile_types.go` | **Modify** -- change field types from structs to strings, remove `Firmware` and `Nic` types |
| `api/hardwaremanagement/v1alpha1/zz_generated.deepcopy.go` | **Regenerate** via `make generate` |
| `config/crd/` | **Regenerate** via `make manifests` |
| `hwmgr-plugins/metal3/controller/hostfirmwarecomponents_manager.go` | **Modify** -- add `resolveFirmwareFromCatalog`, update callers |
| `hwmgr-plugins/metal3/controller/helpers.go` | **Modify** -- call resolution before firmware operations |
| `internal/cmd/operator/start_controller_manager.go` | **Modify** -- register webhooks, ensure singleton FirmwareCatalog on startup |
| `hwmgr-plugins/metal3/controller/hostfirmwarecomponents_manager_test.go` | **Modify** -- update test fixtures to use catalog + entry names |
| `test/utils/vars.go` | **Modify** -- update test fixtures |
| `test/e2e/mno_hw_configuration_test.go` | **Modify** -- update test fixtures |
| `test/e2e/sno_provisioning_test.go` | **Modify** -- update test fixtures |
| `docs/user-guide/firmware-update-workflow.md` | **Modify** -- update examples and field descriptions |

## CR Relationships

```text
FirmwareCatalog (singleton, user-managed)
  └─ spec.images[]
       ├─ name: dell-xr8620t-bios-2.3.5
       │    component: bios, url: ..., version: 2.3.5
       ├─ name: dell-xr8620t-bmc-7.10.70.10
       │    component: bmc, url: ..., version: 7.10.70.10
       └─ name: broadcom-nic-25.2.3
            component: nic, url: ..., version: 25.2.3

HardwareProfile
  └─ spec
       ├─ bios.attributes: {key: value, ...}
       ├─ biosFirmware: "dell-xr8620t-bios-2.3.5"       ──► resolved from catalog
       ├─ bmcFirmware: "dell-xr8620t-bmc-7.10.70.10"    ──► resolved from catalog
       └─ nicFirmware: ["broadcom-nic-25.2.3"]           ──► resolved from catalog

Controller (at reconcile time)
  └─ resolveFirmwareFromCatalog()
       └─ returns internal Firmware/Nic structs with url + version
            └─ passed to existing functions unchanged:
                 ├─ validateFirmwareUpdateSpec
                 ├─ convertToFirmwareUpdates
                 ├─ isVersionChangeDetected
                 ├─ IsFirmwareUpdateRequired
                 ├─ validateFirmwareVersions
                 └─ validateAppliedBiosSettings
```
