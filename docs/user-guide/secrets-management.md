<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Handling Secrets

This guide describes how to manage sensitive Secrets when deploying and
configuring clusters with the O-Cloud Manager. The GitOps layout and sample
content in this repository include Kubernetes `Secret` objects that contain
credentials. Read this guide before committing any of that content to a Git
repository.

- [Why Secrets need special handling](#why-secrets-need-special-handling)
- [Which resources are sensitive](#which-resources-are-sensitive)
- [Keeping Secrets out of Git](#keeping-secrets-out-of-git)
- [About the sample Secret files](#about-the-sample-secret-files)
- [References](#references)

## Why Secrets need special handling

> [!WARNING]
> Kubernetes `Secret` objects are only **base64-encoded, not encrypted**.
> Base64 is a reversible encoding, not a security control. Committing a
> `Secret` to Git in cleartext exposes its credentials to anyone with read
> access to the repository — and to the repository's entire history, even if
> the file is later removed. Do **not** store Secrets that contain sensitive
> information directly in a Git repository.

The GitOps workflow described in
[GitOps Repository Layout and Setup](./gitops-layout-and-setup.md) and
[Server Onboarding](./server-onboarding.md) syncs repository content to the
hub cluster via ArgoCD. Any `Secret` committed to that repository is stored in
plaintext-equivalent form in Git and distributed to everyone with access.

## Which resources are sensitive

The following resources in the O-Cloud Manager workflow are Secrets that
contain sensitive credentials and must not be committed to Git in cleartext:

- **BMC credential Secrets** — the username and password used to access each
  server's baseboard management controller (see
  [BMC Credentials Secret](./server-onboarding.md#bmc-credentials-secret)).
- **Cluster pull secrets** — the registry pull secret (`pull-secret.yaml`)
  referenced by the cluster templates.
- **Preprovisioning network-data Secrets** — where used, the nmstate network
  configuration built into the discovery ISO (see
  [Preprovisioning Network Data Secret](./server-onboarding.md#preprovisioning-network-data-secret)).
  These may contain sensitive network details and should be handled with the
  same care.

## Keeping Secrets out of Git

Several established mechanisms let you reference Secrets from Git without
storing their sensitive values there. The right choice depends on your
organization's requirements and existing tooling — the O-Cloud Manager does
not mandate any particular approach:

- **External Secrets Operator** — synchronizes Secrets into the cluster from
  an external secret store (for example, HashiCorp Vault, AWS Secrets Manager,
  or Azure Key Vault). Only a non-sensitive `ExternalSecret` reference is
  committed to Git.
- **Sealed Secrets** — encrypts a `Secret` into a `SealedSecret` custom
  resource that is safe to commit to Git; the controller decrypts it in the
  cluster.
- **HashiCorp Vault with the ArgoCD Vault Plugin** — keeps secret values in
  Vault and injects them into manifests at sync time, so only placeholders are
  committed to Git. HashiCorp Vault is a commonly preferred approach.

Whichever mechanism you choose, the goal is the same: keep the sensitive
values out of the Git repository while still driving the deployment from Git.

Ultimately, the choice is up to you. If, after understanding the risks
described above, you decide to store Secrets directly in Git, that is your
decision and responsibility.

## About the sample Secret files

> [!WARNING]
> The `Secret` manifests included under
> [git-setup](../samples/git-setup/) (for example, the `pull-secret.yaml`
> files and the `bmc-secret-*` / `network-data-*` Secrets in the sample
> inventory) are **illustrative placeholders only**. They show the required
> shape and fields of each resource. Do not treat them as a pattern for
> storing real credentials, and do not commit real credential values into
> these files. Use one of the mechanisms above for production deployments.

## References

- [Managing secrets in a GitOps workflow](https://access.redhat.com/articles/7128378)
- [ecoeng-security secrets guidance](https://edge-infrastructure.pages.redhat.com/ecoeng-security/security-documentation/secrets/)
