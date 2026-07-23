<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# IBI Media Server

```yaml
title: ibi-media-server
authors:
  - @donpenney
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-07-06
last-updated: 2026-07-06
```

## Table of Contents

- [IBI Media Server](#ibi-media-server)
  - [Table of Contents](#table-of-contents)
  - [Summary](#summary)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
  - [Current State](#current-state)
  - [Proposed Design](#proposed-design)
    - [IBIImage Resource](#ibiimage-resource)
      - [API Definition](#api-definition)
      - [Example CR](#example-cr)
      - [Example Status After Successful Build](#example-status-after-successful-build)
      - [Lifecycle](#lifecycle)
    - [Media Server Service](#media-server-service)
      - [HTTP Endpoints](#http-endpoints)
      - [Storage](#storage)
    - [ISO Build Jobs](#iso-build-jobs)
      - [Job Specification](#job-specification)
      - [Installer Image Resolution](#installer-image-resolution)
      - [PVC Sharing](#pvc-sharing)
    - [IBIImage Controller](#ibiimage-controller)
      - [Reconciliation Flow](#reconciliation-flow)
      - [Conditions](#conditions)
      - [Spec Change Handling](#spec-change-handling)
      - [Finalizer and Cleanup](#finalizer-and-cleanup)
    - [Integration with Operator](#integration-with-operator)
    - [Security](#security)
  - [Future Enhancements](#future-enhancements)
  - [Impact](#impact)
    - [Breaking Changes](#breaking-changes)
    - [Files to Create or Modify](#files-to-create-or-modify)
  - [CR Relationships](#cr-relationships)

## Summary

IBI (Image-Based Installation) provisioning requires users to manually run
`openshift-install image-based create image` on their local workstation to
build a live ISO, host that ISO on their own HTTP server, and reference the
URL in their ProvisioningRequest. This is the most manual and error-prone
step in the IBI workflow, requiring the user to manage local tooling, storage,
and HTTP infrastructure outside the cluster.

This proposal introduces a Media Server service and a new `IBIImage` CRD that
automates the entire ISO lifecycle: generation, storage, and HTTP serving. The
user creates an `IBIImage` CR specifying the seed image and configuration, and
the operator builds the ISO and serves it at a stable URL. The media server's
storage and HTTP infrastructure are designed to also serve firmware images in
the future, providing a shared platform for all file-serving needs.

## Goals

- Automate IBI live ISO generation via a declarative `IBIImage` CRD.
- Serve generated ISOs via HTTP with stable URLs accessible by BMC virtual
  media.
- Avoid bundling `openshift-install` into the operator image by using
  version-specific Kubernetes Jobs for ISO builds.
- Provide configurable persistent storage for ISO files.
- Design the storage and HTTP infrastructure to support firmware image
  serving in the future (see the
  [FirmwareCatalog proposal](firmware-catalog.md)).

## Non-Goals

- Firmware image hosting (future work that reuses this infrastructure).
- ISO customization beyond what `image-based-installation-config.yaml`
  supports.
- Multi-architecture ISO builds.
- Automatic resolution of `installerImage` from OCP release images (initial
  implementation requires the user to specify the installer image explicitly;
  automatic resolution is a future enhancement).

## Current State

The IBI provisioning workflow is documented in
[ibi-based-cluster-provisioning.md](../user-guide/ibi-based-cluster-provisioning.md).
The ISO build step requires the user to:

1. Install `openshift-install` locally (version must match the seed image's
   OCP version).
2. Create an `image-based-installation-config.yaml` file with fields
   including `seedImage`, `seedVersion`, `pullSecret`, `sshKey`, and
   `installationDisk`.
3. Run `openshift-install image-based create image --dir <workdir>` to
   produce `rhcos-ibi.iso`.
4. Copy the ISO to an HTTP server accessible from the BMC management
   network.
5. Reference the HTTP URL in the ProvisioningRequest's cluster template
   parameters.

The `openshift-install` command consumes the configuration file during the
build, so rebuilding requires restoring the file from a backup and using a
clean working directory. The generated ISO is typically 1-2 GB.

## Proposed Design

### IBIImage Resource

A new namespaced CRD in the `clcm.openshift.io/v1alpha1` API group. Each
`IBIImage` CR represents a single IBI live ISO with its build configuration
and serving state.

#### API Definition

File: `api/hardwaremanagement/v1alpha1/ibiimage_types.go`

```go
type IBIImageSpec struct {
    // SeedImage is the container image reference for the IBI seed image.
    // +kubebuilder:validation:MinLength=1
    SeedImage string `json:"seedImage"`

    // SeedVersion is the OCP version string of the seed image (e.g., "4.17.0").
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+.*$`
    SeedVersion string `json:"seedVersion"`

    // InstallerImage is the container image containing openshift-install for
    // the target OCP version. This image is used as the build Job's container
    // image to generate the ISO.
    // +kubebuilder:validation:MinLength=1
    InstallerImage string `json:"installerImage"`

    // PullSecretRef is a reference to a Secret in the same namespace containing
    // the pull secret for accessing the seed image and release image registries.
    // The secret must contain a .dockerconfigjson key.
    PullSecretRef LocalSecretReference `json:"pullSecretRef"`

    // SSHKeyRef is a reference to a Secret containing the SSH public key to
    // embed in the ISO for pre-provisioned node access. The secret must
    // contain a key named "ssh-publickey".
    // +optional
    SSHKeyRef *LocalSecretReference `json:"sshKeyRef,omitempty"`

    // InstallationDisk is the target disk device path for installation
    // (e.g., "/dev/sda" or "/dev/disk/by-path/...").
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:Pattern=`^/dev/.*$`
    InstallationDisk string `json:"installationDisk"`

    // ExtraPartitionStart is the start offset in MiB for an additional data
    // partition on the installation disk. Recommended minimum: 25000.
    // +optional
    ExtraPartitionStart *int32 `json:"extraPartitionStart,omitempty"`

    // ExtraPartitionSize is the size in MiB for the additional data partition.
    // +optional
    ExtraPartitionSize *int32 `json:"extraPartitionSize,omitempty"`

    // ExtraPartitionLabel is the filesystem label for the additional data
    // partition.
    // +optional
    ExtraPartitionLabel string `json:"extraPartitionLabel,omitempty"`

    // NetworkConfig is NMState network configuration to embed in the ISO for
    // pre-provisioning network setup.
    // +optional
    // +kubebuilder:validation:Type=object
    // +kubebuilder:pruning:PreserveUnknownFields
    NetworkConfig *runtime.RawExtension `json:"networkConfig,omitempty"`
}

type LocalSecretReference struct {
    // Name is the name of the Secret in the same namespace.
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`
}

type IBIImageStatus struct {
    // ObservedGeneration is the most recent generation observed by the controller.
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // ISOURL is the HTTP URL where the generated ISO is available for download.
    // +optional
    ISOURL string `json:"isoURL,omitempty"`

    // ISOSize is the size of the generated ISO in bytes.
    // +optional
    ISOSize *int64 `json:"isoSize,omitempty"`

    // ISOChecksum is the SHA256 checksum of the generated ISO.
    // +optional
    ISOChecksum string `json:"isoChecksum,omitempty"`

    // BuildJobName is the name of the Kubernetes Job that built (or is
    // building) this ISO.
    // +optional
    BuildJobName string `json:"buildJobName,omitempty"`

    // Conditions report the current state of the IBIImage.
    // +patchMergeKey=type
    // +patchStrategy=merge
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

#### Example CR

```yaml
apiVersion: clcm.openshift.io/v1alpha1
kind: IBIImage
metadata:
  name: sno-4-17-0
  namespace: oran-o2ims
spec:
  seedImage: "quay.io/example/sno-seed:4.17.0"
  seedVersion: "4.17.0"
  installerImage: "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123..."
  pullSecretRef:
    name: ibi-pull-secret
  sshKeyRef:
    name: ibi-ssh-key
  installationDisk: "/dev/disk/by-path/pci-0000:43:00.0-nvme-1"
  extraPartitionStart: 25000
  extraPartitionSize: 500000
  extraPartitionLabel: "data"
```

#### Example Status After Successful Build

```yaml
status:
  observedGeneration: 1
  isoURL: "https://o2ims.apps.example.com/media/v1alpha1/images/sno-4-17-0/installation.iso?token=eyJhbGciOi..."
  isoSize: 1073741824
  isoChecksum: "sha256:e3b0c44298fc1c149afbf4c8996fb924..."
  buildJobName: "sno-4-17-0-build-x7k2m"
  conditions:
  - type: ImageBuilding
    status: "False"
    reason: BuildCompleted
    message: "Build job completed successfully"
  - type: ImageReady
    status: "True"
    reason: BuildSucceeded
    message: "ISO is available on storage"
  - type: ImageServing
    status: "True"
    reason: Serving
    message: "ISO is being served at the isoURL"
```

#### Lifecycle

1. The user creates Secrets for the pull secret (`.dockerconfigjson` format)
   and optionally the SSH public key.
2. The user creates an `IBIImage` CR referencing these secrets and specifying
   the seed image, installer image, and disk configuration.
3. The IBIImage controller validates the spec, creates a build Job, and sets
   `ImageBuilding=True`.
4. The build Job runs `openshift-install image-based create image`, writes
   the ISO to the shared PVC, and exits.
5. The controller detects Job completion, computes the ISO checksum, and
   updates the status with the serving URL. Conditions are set to
   `ImageReady=True` and `ImageServing=True`.
6. If the build fails, `ImageReady` is set to `False` with error details
   from the Job.
7. The user references the `status.isoURL` in their provisioning workflow.
8. Spec changes (e.g., different seedImage) trigger a rebuild: the old ISO
   is deleted and a new build Job is created.
9. On CR deletion, a finalizer cleans up the ISO from the PVC and deletes
   any active build Job.

### Media Server Service

The media server follows the existing multi-service binary pattern. It is a
lightweight HTTP file server that serves ISOs from a persistent volume.

- Subcommand: `media-server serve`
- Service directory: `internal/service/media/`
- No database required.
- Uses `CommonServerConfig` for TLS configuration.
- Serves files from the PVC mount at `/data/images/`.

#### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/media/v1alpha1/images/<name>/installation.iso?token=<token>` | Download the ISO for the named IBIImage |
| `HEAD` | `/media/v1alpha1/images/<name>/installation.iso?token=<token>` | Get ISO metadata (size, checksum) without downloading |
| `GET` | `/media/v1alpha1/health` | Health check |

The `GET` and `HEAD` endpoints return standard HTTP headers:

- `Content-Type: application/octet-stream`
- `Content-Length: <size>`
- `ETag: "<sha256-checksum>"`

ISO serving endpoints are authenticated using a **per-ISO token** passed as
a query parameter. The controller generates a cryptographically random token
for each IBIImage and stores it in a Secret. The token is included in the
`status.isoURL` field so that BMCs can access the ISO via virtual media
without Kubernetes ServiceAccount tokens. Requests without a valid token
receive a `401 Unauthorized` response.

This approach is necessary because ISOs embed pull secrets and SSH keys in
their ignition config, making unauthenticated access a credential-leak risk.
BMCs support HTTP/HTTPS URLs with query parameters, so token-in-URL is
compatible with virtual media workflows. Tokens are revoked when the
IBIImage CR is deleted.

#### Storage

The media server uses a PersistentVolumeClaim for ISO storage:

- Access mode: `ReadWriteOnce`
- Default size: 100Gi (configurable via the Inventory CRD spec)
- Storage class: cluster default
- Deployment strategy: `Recreate` (required for RWO PVC with single replica)

The PVC size is configurable through a new field on the Inventory CRD spec,
allowing administrators to adjust storage based on the expected number and
size of ISOs.

Filesystem layout on the PVC:

```text
/data/
  images/
    <ibiimage-name>/
      installation.iso
      installation.iso.sha256
  firmware/                    # Reserved for future firmware serving
```

EmptyDir volumes are used for `/tmp` and working directories to support
`readOnlyRootFilesystem`.

### ISO Build Jobs

#### Job Specification

The controller creates a Kubernetes Job for each ISO build. The Job uses the
installer container image specified in the IBIImage spec, which contains
`openshift-install` for the target OCP version. This avoids bundling
`openshift-install` into the operator image.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: <ibiimage-name>-build-<suffix>
  namespace: oran-o2ims
  ownerReferences:
  - apiVersion: clcm.openshift.io/v1alpha1
    kind: IBIImage
    name: <ibiimage-name>
spec:
  backoffLimit: 2
  activeDeadlineSeconds: 1800    # 30-minute timeout
  template:
    spec:
      restartPolicy: OnFailure
      affinity:
        podAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchLabels:
                app: media-server
            topologyKey: kubernetes.io/hostname
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: build
        image: <installerImage from IBIImage spec>
        command: ["/usr/bin/openshift-install"]
        args: ["image-based", "create", "image", "--dir", "/workspace"]
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
          limits:
            memory: 2Gi
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: output
          mountPath: /output
        - name: pull-secret
          mountPath: /secrets/pull-secret
          readOnly: true
        - name: config
          mountPath: /config
          readOnly: true
      initContainers:
      - name: setup
        image: <installerImage>
        command: ["/bin/sh", "-c"]
        args:
        - |
          cp /config/image-based-installation-config.yaml /workspace/
          # Build produces ISO in /workspace; post-build copies to /output
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: config
          mountPath: /config
          readOnly: true
      volumes:
      - name: workspace
        emptyDir: {}
      - name: output
        persistentVolumeClaim:
          claimName: media-server-data
      - name: pull-secret
        secret:
          secretName: <pullSecretRef.name>
      - name: config
        secret:
          secretName: <ibiimage-name>-build-config
```

#### Installer Image Resolution

The initial implementation requires the user to specify `installerImage`
explicitly in the IBIImage spec. This is the container image that contains
`/usr/bin/openshift-install` for the target OCP version.

Users can find the appropriate installer image from the OCP release image:

```bash
oc adm release info quay.io/openshift-release-dev/ocp-release:<version>-x86_64 \
  --image-for=installer
```

#### PVC Sharing

The build Job and the media server Deployment share the same RWO PVC. Since
RWO volumes can be mounted by multiple pods on the **same node**, the build
Job uses pod affinity to schedule on the same node as the media server pod.

### IBIImage Controller

#### Reconciliation Flow

1. **Validate**: Verify referenced Secrets exist and contain expected keys.
2. **Check existing Job**: If a build Job already exists, monitor its status.
3. **Generate config**: Create a Secret containing the
   `image-based-installation-config.yaml` built from the IBIImage spec and
   referenced Secret values. A Secret is used instead of a ConfigMap because
   the config includes the pull secret and SSH key.
4. **Create Job**: Create the build Job with the installer image, mounting
   the config Secret, pull secret, and output PVC.
5. **Monitor Job**: Requeue until the Job reaches a terminal state
   (succeeded or failed).
6. **On success**: Compute the ISO SHA256 checksum, set `isoURL`,
   `isoSize`, `isoChecksum` in status, and set conditions to indicate the
   ISO is ready and being served.
7. **On failure**: Set `ImageReady=False` with error details. The Job's pod
   logs contain the build output for debugging.

#### Conditions

| Type | When True | When False |
|------|-----------|------------|
| `ImageBuilding` | A build Job is in progress | No active build (completed, failed, or not started) |
| `ImageReady` | ISO has been built and is present on storage | Build not complete or failed |
| `ImageServing` | ISO is being served by the media server | Server not ready or ISO file missing |

#### Spec Change Handling

When the IBIImage spec changes (detected via `observedGeneration`):

1. Delete the existing ISO file from the PVC.
2. Delete any active build Job.
3. Set `ImageReady=False` and `ImageServing=False`.
4. Create a new build Job with the updated configuration.

#### Finalizer and Cleanup

The controller adds a finalizer (`clcm.openshift.io/ibiimage-cleanup`) on
creation. On deletion:

1. Cancel any active build Job.
2. Remove the ISO file and its checksum file from the PVC.
3. Remove the build config Secret and the access token Secret.
4. Remove the finalizer.

A periodic reconciliation (or startup scan) detects orphaned directories on
the PVC (directories without a corresponding IBIImage CR) and removes them.

### Integration with Operator

The media server integrates with the operator following existing patterns:

- **Constants**: Add `MediaServerCmd`, `InventoryMediaServerName` to
  `internal/controllers/utils/constants.go`.
- **Deployment**: Add `setupMediaServerConfig()` to
  `internal/controllers/inventory_controller.go`, following the pattern of
  `setupArtifactsServerConfig()`. The Deployment includes the PVC volume
  mount and uses `Recreate` strategy.
- **PVC**: Create a PVC following the PostgreSQL PVC pattern in
  `inventory_controller_postgres.go`, with size configurable via the
  Inventory CRD spec.
- **Ingress**: Add a path rule for `/media` routing to the media server.
  This path serves token-authenticated content for BMC access.
- **Service**: Create a Kubernetes Service exposing the media server.
- **NetworkPolicy**: Allow ingress from the ingress controller and from
  build Job pods.
- **RBAC**: The controller needs permissions for IBIImage CRs, Secrets
  (create/get/list/watch/delete for build config, access tokens, and
  referenced pull secret/SSH key Secrets), and Jobs
  (create/get/list/watch/delete).
- **Command registration**: Add `mediacmd.GetMediaRootCmd` to `main.go`.
- **Controller registration**: Register the IBIImage controller in
  `internal/cmd/operator/start_controller_manager.go`.

### Security

**Pull secrets**: Stored in Kubernetes Secrets and mounted into build Job
pods as volumes. The media server Deployment does not mount pull secrets.
Pull secrets are embedded in the ISO's ignition config by
`openshift-install`, not written to the PVC in cleartext.

**SSH keys**: Mounted into build Job pods only, embedded in the ISO by
`openshift-install`.

**ISO serving**: The media server uses TLS (same serving cert pattern as
other services). ISO downloads require a per-ISO token passed as a query
parameter. Tokens are generated by the controller, stored in a Secret, and
included in the `status.isoURL`. This prevents unauthorized access to ISOs,
which embed pull secrets and SSH keys in their ignition config.

**Build config**: The `image-based-installation-config.yaml` is stored as a
Secret (not a ConfigMap) because it contains pull secret and SSH key values.
The Secret is mounted into the build Job pod and deleted after the build
completes.

**Security contexts**: All pods (media server and build Jobs) use the
standard security context: `runAsNonRoot`, `readOnlyRootFilesystem`,
`seccompProfile: RuntimeDefault`, `allowPrivilegeEscalation: false`,
`capabilities: drop ALL`.

**Storage**: The PVC is mounted read-only by the media server and
read-write by build Jobs. The media server serves files but cannot modify
them.

## Future Enhancements

- **Firmware image serving**: The media server's PVC and HTTP infrastructure
  can serve firmware images referenced by FirmwareCatalog entries. A future
  controller could download firmware images from external URLs and cache
  them on the PVC under `/data/firmware/`. The HTTP handler already serves
  static files from the PVC, so firmware images would be served without
  additional code. Unlike ISOs, firmware images do not contain embedded
  credentials, so the `/data/firmware/` path could be served without
  token authentication. The filesystem separation between `/data/images/`
  and `/data/firmware/` supports applying different access policies per
  path prefix.
- **Automatic installer image resolution**: Resolve the `installerImage`
  automatically from the OCP release image for the specified
  `seedVersion`, removing the need for users to look up the installer
  image reference manually.
- **Storage monitoring**: Track PVC usage and set a condition when storage
  is near capacity, alerting administrators to clean up unused ISOs or
  increase PVC size.

## Impact

### Breaking Changes

None. This proposal introduces a new CRD and a new service without
modifying existing APIs or behavior.

### Files to Create or Modify

| File | Action |
|------|--------|
| `api/hardwaremanagement/v1alpha1/ibiimage_types.go` | **Create** -- CRD type definitions |
| `api/hardwaremanagement/v1alpha1/ibiimage_webhook.go` | **Create** -- validating webhook |
| `internal/service/media/cmd/root.go` | **Create** -- root cobra command |
| `internal/service/media/cmd/serve.go` | **Create** -- serve subcommand |
| `internal/service/media/serve.go` | **Create** -- HTTP server setup |
| `internal/service/media/api/server.go` | **Create** -- server config and handler |
| `internal/service/media/api/openapi.yaml` | **Create** -- OpenAPI spec |
| `internal/service/media/api/generated/` | **Create** -- generated server code |
| `internal/controllers/ibiimage_controller.go` | **Create** -- IBIImage reconciler |
| `internal/controllers/ibiimage_controller_test.go` | **Create** -- controller tests |
| `must-gather/gather` | **Modify** -- add IBIImage to collected resources |
| `main.go` | **Modify** -- add media-server command |
| `internal/constants/constants.go` | **Modify** -- add media server constants |
| `internal/controllers/utils/constants.go` | **Modify** -- add server name and args |
| `internal/controllers/utils/utils.go` | **Modify** -- update helper functions |
| `internal/controllers/inventory_controller.go` | **Modify** -- add media server setup |
| `internal/cmd/operator/start_controller_manager.go` | **Modify** -- register controller and webhook |
| `config/crd/` | **Regenerate** via `make manifests` |
| `config/rbac/` | **Modify** -- add RBAC for Jobs, IBIImage, Secrets |

## CR Relationships

```text
IBIImage (user-managed)
  ├── spec.pullSecretRef ──────► Secret (.dockerconfigjson)
  ├── spec.sshKeyRef ──────────► Secret (ssh-publickey)
  │
  ├── (controller creates)
  │   ├── Secret               (build config with embedded credentials)
  │   ├── Secret               (access token for ISO download)
  │   └── Job                  (build job using installerImage)
  │       ├── mounts PVC       (shared with media server)
  │       ├── mounts Secret    (build configuration)
  │       ├── mounts Secret    (pull secret)
  │       └── writes ISO to PVC
  │
  └── status.isoURL ──────────► Media Server HTTP endpoint
                                  ├── serves ISO from PVC (token-authenticated)
                                  └── URL used in ProvisioningRequest

Inventory (operator-managed)
  └── creates media server Deployment, Service, PVC, NetworkPolicy
```
