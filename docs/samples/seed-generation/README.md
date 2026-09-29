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

Phase 1 supports creating and validating the ClusterTemplate. The operator
rejects creation or update of a ProvisioningRequest that selects
`seedGeneration` until the seed-generation workflow is implemented. Keep this
ProvisioningRequest sample for that later phase; it does not start seed
generation in Phase 1.

The ClusterTemplate owns the registry credential reference. Its
`templateParameterSchema` allows a ProvisioningRequest to override only
`seedImage`. To keep the template's seed image, omit
`templateParameters.upgradeParameters` from the ProvisioningRequest. A template
can also add `liveISO` to `upgradeDefaults.seedGeneration` when ISO generation
is required.

The sample references a Secret named `seed-registry-credentials` in the
ClusterTemplate namespace. The workflow validates that Secret and the spoke
prerequisites before detaching ACM agents. Seed generation starts after ZTP
Done; there is no need to patch the request after installation.
