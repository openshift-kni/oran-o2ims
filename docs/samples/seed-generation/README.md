<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Seed-generation API sample

These [ClusterTemplate](cluster-template.yaml) and
[ProvisioningRequest](provisioning-request.yaml) examples show the seed-only
configuration contract. Adapt the existing SNO ClusterTemplate, ConfigMaps,
hardware selectors, and provisioning inputs for your site. Replace `4.Y.Z`
with the installed OpenShift release and use a new ProvisioningRequest UUID.

Create the ProvisioningRequest with seed-generation configuration in place.
The controller starts the workflow after the cluster reaches ZTP Done, so the
request does not need a post-install patch to trigger seed generation.

The sample ClusterTemplate leaves `seedImage` and `seedAuthSecretRef` out of
`upgradeDefaults`, so its schema requires the ProvisioningRequest to provide
both. A ClusterTemplate may instead set either as a default and expose it as
an optional PR override. The PR-facing schema accepts only the Secret `name`;
the controller resolves it in the ClusterTemplate namespace. The Secret data
and namespace cannot be supplied by the PR.

Set `upgradeDefaults.seedGeneration.seedGenerationTimeout` to override the
default operation timeout of 2h. The ProvisioningRequest can override this
value at `upgradeParameters.seedGeneration.seedGenerationTimeout`;
`clusterUpgradeTimeout` applies only to cluster upgrades. This sample shows
seed-only generation and omits `liveISO`. To build and publish a live ISO after
seed generation, add `liveISO` to the ClusterTemplate defaults and follow
[Configure a live ISO upload destination](../../user-guide/seed-generation-live-iso-upload.md).
That guide also covers the optional `liveISO.sshKey` public key for SSH access
to the pre-provisioning host as `core`, including log monitoring.

Initial ProvisioningRequest validation checks that the referenced registry
credentials match the `seedImage` repository and can start a push upload before
hardware or cluster provisioning begins. The workflow repeats that check and
validates the spoke prerequisites before detaching ACM agents. Seed generation
starts after ZTP Done; there is no need to patch the request after installation.

## Deployment and progress

Install the matching operator and regenerated ProvisioningRequest CRD together.
The workflow persists identity, submission, and cleanup checkpoints in status;
an older CRD can prune these fields and prevent reliable recovery.

Use `SeedGenerationCompleted` conditions and
`status.extensions.clusterDetails.seedGenerationStatus` to follow progress:

1. Validate repository-scoped push credentials before hardware provisioning.
   Registry trust includes the hub's additional trusted CAs.
2. After ZTP Done, freeze inputs and verify the cluster identity, single-node
   topology, installed release, containers partition, and LCA availability.
3. Disable ACM placement, observability, and automatic import; wait for add-on
   hooks, remove the Klusterlet and its work records, and clean up monitoring
   remnants. `agentDetachmentCompleted` marks completion of this step.
4. Submit LCA and monitor generation and push. LCA can temporarily remove its
   SeedGenerator and Secret during the build. `seedGeneratorSubmitted` prevents
   the controller from creating another run while they are absent.
5. On success, report the configured image reference in `seedImage`, mark the
   PR fulfilled, and remove transient resources. The source cluster stays
   detached from ACM.

Normal PR deletion cleans hub artifacts and deprovisions/wipes the node; it
must not depend on a reachable spoke. With `skip-cleanup`, the node is retained,
so deletion waits for LCA to finish and for spoke credentials to be scrubbed.
A timeout does not cancel LCA's host process. If a submission response is lost,
the controller waits rather than risking a duplicate build; recovery may
require deleting and recreating the PR.
