<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Seed-generation tools image

The operator uses Red Hat's `ose-tools-rhel9` image for seed-generation
preflight and live ISO builds. Red Hat provides the image through the OpenShift
image registry; O-Cloud Manager does not build a separate tools image. The
O-Cloud Manager release owner is responsible for selecting and qualifying the
image reference used by each operator release and for keeping the pinned digest
and bundle metadata in sync.

The manager reads the image reference from
`RELATED_IMAGE_SEED_GENERATION_TOOLS_IMAGE`, configured in
[`config/manager/manager.yaml`](../../config/manager/manager.yaml). Konflux
maps the image for staging and production in
[`map_images.in.yaml`](../../.konflux/overlay/map_images.in.yaml) and pins its
source tag to a digest in
[`pin_images.in.yaml`](../../.konflux/overlay/pin_images.in.yaml). The bundle
publishes the image through the CSV `relatedImages` list. Keep these references
aligned; regenerate and inspect the bundle after changing them.

## Image requirements

The image must support the manager's non-root execution and provide the
utilities used by seed-generation preflight and the ISO build, including
`bash`, `oc`, `ssh`, `scp`, `curl`, `sha256sum`, `python3`, and standard file
utilities. The build extracts `openshift-install` from the frozen release
image; it does not use an installer binary selected independently from that
release.

When selecting a new tag or digest, qualify it for the supported OCP release
range and verify that it can:

* inspect and extract `openshift-install` from the pinned release image, using
  the frozen pull secret and mirror configuration;
* create the IBI live ISO as the non-root Job user with the configured
  filesystem and temporary-directory restrictions;
* upload through the configured SSH key and pinned host key, then verify both
  the remote file and the HTTPS-served ISO digest; and
* operate with the mirror and CA trust settings used by disconnected builds.

The seed-generation controller and ISO script tests cover command construction,
security settings, and artifact verification. A release qualification should
also exercise the complete seed-only and seed-plus-ISO paths on the supported
hub and spoke versions before updating the production pin.

## Refreshing the pinned image

1. Select the `ose-tools-rhel9` minor version qualified for the operator's
   supported OpenShift releases.
2. Update the manager's `RELATED_IMAGE_SEED_GENERATION_TOOLS_IMAGE` source tag
   and the matching source entry in `pin_images.in.yaml`. Set its target to the
   image's manifest-list digest, not a platform-specific child digest.
3. Keep `map_images.in.yaml` pointed at the corresponding Red Hat staging and
   production repositories. Regenerate the bundle and confirm its
   `relatedImages` entry names the same image used by the manager.
4. Run the repository CI checks and perform the required seed-generation and
   ISO qualification before release. Review the generated bundle and Konflux
   overlays with the image update.

The image publisher owns image contents and CVE remediation. The O-Cloud
Manager release owner owns when to adopt an updated image digest and the
functional qualification of that digest with the seed-generation workflow.
