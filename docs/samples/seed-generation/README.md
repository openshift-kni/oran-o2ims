<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Seed-generation samples

The [ClusterTemplate](cluster-template.yaml) and
[ProvisioningRequest](provisioning-request.yaml) show a seed-only run. The
ClusterTemplate uses `upgradeDefaults.seedGeneration: {}` because this sample
schema requires the ProvisioningRequest to supply `seedImage` and
`seedAuthSecretRef`. To build and publish an ISO after the seed image push,
replace that empty object with the commented mapping in the ClusterTemplate;
`liveISO` is template-owned and is not added to the ProvisioningRequest. The
same ProvisioningRequest structure works for both flows.

If a ClusterTemplate supplies both `seedImage` and `seedAuthSecretRef` as
defaults, it can omit `upgradeParameters` from the ProvisioningRequest schema,
and the request should omit `templateParameters.upgradeParameters`.

Replace `4.Y.Z`, the cluster names, resource selectors, and the UUID with values
for your site. Start from a validated SNO ClusterTemplate and its referenced
ConfigMaps rather than applying these standalone examples unchanged.

## Prerequisites

The seed cluster template must install the LifeCycle Agent (LCA) Operator and
create the shared `/var/lib/containers` partition required by LCA. OADP is not
required. The image and credential Secret must refer to the same registry
repository, and the Secret must contain a `seedAuth` key with the registry auth
JSON used by LCA.

The overall `seedGenerationTimeout` defaults to 2h for seed-only generation and
3h when `liveISO` is configured. Set it under `upgradeDefaults.seedGeneration`
in the ClusterTemplate, or expose it through the ProvisioningRequest schema if
operators need to override it per run.

The files [seed-auth-secret.example.yaml](seed-auth-secret.example.yaml) and
[iso-upload-secret.example.yaml](iso-upload-secret.example.yaml) show the
required Secret shapes. They contain placeholders only. Do not apply them as
written and do not commit real credentials. Create or synchronize the Secrets
with your site's approved secret-management process. The seed registry Secret
and ISO upload Secret must be in the ClusterTemplate namespace.

For example, create the seed registry Secret from a path-scoped auth JSON file
kept outside Git:

```console
CT_NAMESPACE=sno-ran-du-v4-Y-Z
oc -n "$CT_NAMESPACE" create secret generic seed-registry-credentials --from-file=seedAuth=registry-auth.json
```

The registry auth entry must cover the repository path used by `seedImage` and
allow image pushes. The sample request uses `registry.example.com/seed` for
both. The upload guide provides the corresponding command for its SSH and
HTTPS credential Secret.

For live ISO settings, configure `urlBase` and `uploadSecretRef` in the
ClusterTemplate. The upload Secret's `knownHosts` value must pin the SSH server
key, and the HTTPS destination must be reachable from the hub build Job and the
target BMCs. See [Configure a live ISO upload destination](../../user-guide/seed-generation-live-iso-upload.md)
for key generation, host-key verification, certificate trust, and server path
requirements.

The ClusterTemplate includes an optional `liveISO.sshKey` example. It is a
public key installed for the `core` user on a server booted from the ISO, for
debugging and log access. It is unrelated to the upload Secret's SSH private
key.

## Create and monitor the request

Create the ProvisioningRequest using the input shape permitted by its
ClusterTemplate schema. In this sample, the image and registry Secret reference
are ProvisioningRequest inputs, so they must be present from the start. If the
ClusterTemplate supplies both as defaults, omit `templateParameters.upgradeParameters`
from the request. The controller starts seed generation after the cluster reaches
ZTP Done; no post-install patch is needed.

The registry and optional ISO upload preflight checks run asynchronously during
controller reconciliation, after the API server accepts the ProvisioningRequest
and before hardware is allocated. A successful `oc create` therefore does not
mean those checks have passed. Inspect `ProvisioningRequestValidated` and
`SeedGenerationRegistryValidated` in status, and also
`SeedGenerationISOUploadValidated` when the template configures a live ISO. The
controller proceeds to hardware provisioning after validation succeeds.

Use the condition and provisioning summary to follow progress:

```console
PR_NAME=your-provisioningrequest-name
oc get provisioningrequest "$PR_NAME" \
  -o custom-columns=PHASE:.status.provisioningStatus.provisioningPhase,DETAIL:.status.provisioningStatus.provisioningDetails
oc get provisioningrequest "$PR_NAME" -o yaml
```

The `SeedGenerationCompleted` condition and
`status.provisioningStatus.provisioningDetails` describe validation, frozen
inputs, ACM cleanup, LCA progress, and optional ISO build/upload. The condition
becomes `True` with reason `Completed` when the requested artifacts are ready.
Failure reasons include `PreconditionChecksFailed`, `Failed`, and `TimedOut`.

On success, collect the status outputs before deleting the request:

* `status.extensions.clusterDetails.seedGenerationStatus.seedImage` is the
  configured image reference whose push completed successfully.
* `status.extensions.clusterDetails.seedGenerationStatus.releaseImageDigest`
  pins the release used to extract `openshift-install` for the ISO build.
* When `liveISO` is configured, `isoURL` is the verified HTTPS artifact URL and
  `isoDigest` is its SHA-256 digest.

For example, retrieve the generated ISO URL and checksum with:

```console
oc get provisioningrequest "$PR_NAME" -o jsonpath='{.status.extensions.clusterDetails.seedGenerationStatus.isoURL}{"\n"}'
oc get provisioningrequest "$PR_NAME" -o jsonpath='{.status.extensions.clusterDetails.seedGenerationStatus.isoDigest}{"\n"}'
```

## Lifecycle and cleanup

Seed generation is a one-shot operation. Inputs are frozen before detachment.
If preflight fails before ACM detachment, the cluster remains attached. Once
detachment begins, the controller does not restore ACM agents, including after
a failure or timeout; the seed cluster remains detached for inspection until
you delete the ProvisioningRequest. A timeout does not stop an LCA host process
that is already running.

Deleting the factory ProvisioningRequest cleans run-owned hub resources and
allows the hardware manager to deprovision and wipe the allocated server. This
is the normal way to reclaim the factory hardware after saving the artifact
outputs. The `clcm.openshift.io/skip-cleanup` annotation retains the node and
changes finalizer cleanup. With that annotation, deletion waits for LCA to
finish and for the spoke credentials to be scrubbed. Do not set it when the goal
is to reclaim the hardware. See the [IBI provisioning guide](../../user-guide/ibi-based-cluster-provisioning.md#automated-seed-image-and-live-iso-generation)
for the full workflow and failure behavior.
