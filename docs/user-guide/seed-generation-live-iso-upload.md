<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Configure a live ISO upload destination

The seed-generation workflow can build a live installation ISO after it pushes
the seed image. It uploads the ISO over SSH and publishes an HTTPS URL in the
ProvisioningRequest status. This guide configures the upload credentials and
the matching ClusterTemplate settings. The HTTPS server and its writable
directory must already exist.

## Match the SSH directory to the HTTPS URL

The `liveISO` block belongs in the ClusterTemplate's
`spec.templateDefaults.upgradeDefaults.seedGeneration`, not in the
ProvisioningRequest. Its `uploadSecretRef` names a Secret in the **same
namespace as the ClusterTemplate**. The controller reads the Secret before
hardware provisioning and again before the ISO build.

| Secret key | Value |
| --- | --- |
| `host` | SSH server DNS name or IP address, without a scheme or port. SSH uses port 22. |
| `username` | SSH account that can create directories and files below `remotePath`. |
| `privateKey` | Unencrypted SSH private key for that account. |
| `remotePath` | Absolute, writable **base directory** served by the HTTPS server. It is not an ISO filename. |
| `knownHosts` | Verified SSH server host key in OpenSSH `known_hosts` format, using the same name as `host`. |
| `caCert` | Optional PEM certificate or CA bundle that validates the HTTPS server. Required for a self-signed certificate. |

The controller appends `seedgen/<ProvisioningRequest UID>/rhcos-ibi.iso` to
both `remotePath` and `liveISO.urlBase`. For example:

```text
remotePath: /srv/iso/ibi
urlBase:    https://iso.example.com/ibi/
SSH file:   /srv/iso/ibi/seedgen/<PR-UID>/rhcos-ibi.iso
HTTPS URL:  https://iso.example.com/ibi/seedgen/<PR-UID>/rhcos-ibi.iso
```

Both paths must refer to the same file. The HTTPS URL may use a non-default
port; the SSH `host` value cannot specify an SSH port. The upload account
needs SSH/SFTP access and permission to run `mkdir`, `rmdir`, `sha256sum`,
`chmod`, `mv`, and `rm` on the server. The HTTPS server must be able to traverse
the generated directories and read the ISO.

Prefer a dedicated upload account that can write only under `remotePath`,
separate from the account that manages the web server and its TLS private key.
The controller runs SSH commands on the server, so a forced-command or
SFTP-only account will not work with this workflow.

The generated ISO contains the effective installation pull secret. Restrict
HTTPS downloads to the hub build Job and authorized provisioning consumers,
such as BMCs or provisioning services, using firewall rules or network access
controls. This workflow does not configure HTTP download authentication. The
run UID and ISO URL are visible in ProvisioningRequest status and do not
provide access control. Disable directory listing and remove generated ISOs
when they are no longer needed.

## Create an upload key and authorize it

Run these commands on a trusted administration host. Keep the private key
outside Git and restrict its file permissions. Use a dedicated key for the ISO
upload account. The controller requires an unencrypted private key because its
Job cannot answer a passphrase prompt.

```bash
ISO_HOST=iso.example.com
ISO_USER=iso-uploader
KEY_DIR="$HOME/.config/oran-o2ims/iso-upload"
install -d -m 700 "$KEY_DIR"
ssh-keygen -t ed25519 -N '' -f "$KEY_DIR/id_ed25519"
chmod 600 "$KEY_DIR/id_ed25519"
```

Add **only** `id_ed25519.pub` to the upload account's
`~/.ssh/authorized_keys` on the SSH server. If that account accepts SSH login,
`ssh-copy-id` can do this after you pin the server host key in the next
section. Otherwise, transfer the public key through a trusted administrator
and append it on the server:

```bash
install -d -m 700 ~/.ssh
cat /path/to/id_ed25519.pub >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
```

Run those server commands as the upload account. The `authorized_keys` public
key authenticates the account; it is **different** from the SSH server host
key used in `knownHosts`.

## Pin the SSH server host key

Get the server's host-key fingerprint through a trusted administrator channel,
for example by running this **on the SSH server**:

```bash
sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

Then collect the public host key on the administration host and compare its
fingerprint with the server-side result:

```bash
ssh-keyscan -T 10 -t ed25519 "$ISO_HOST" > "$KEY_DIR/known_hosts"
ssh-keygen -lf "$KEY_DIR/known_hosts"
```

Continue only if the fingerprints match. `ssh-keyscan` by itself does not
authenticate the server. The `known_hosts` file must contain an entry for the
exact DNS name or IP address stored in the Secret's `host` key. The controller
and upload Job reject an absent or mismatched host key; they do not use
trust-on-first-use.

If password login is available, install the upload public key now:

```bash
ssh-copy-id -i "$KEY_DIR/id_ed25519.pub" \
    -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile="$KEY_DIR/known_hosts" \
    "$ISO_USER@$ISO_HOST"
```

Test SSH access with the same key and host-key file:

```bash
ssh -i "$KEY_DIR/id_ed25519" -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile="$KEY_DIR/known_hosts" \
    "$ISO_USER@$ISO_HOST" true
```

## Verify HTTPS trust

The HTTPS certificate must be valid for the hostname in `liveISO.urlBase` and
reachable from the hub manager and ISO build Job. With a self-signed
certificate, obtain its PEM file through a trusted channel and set `CA_CERT`
to its local path. Verify TLS with `curl` before creating the Secret:

```bash
CA_CERT=/path/to/iso-server.crt
ISO_URL_BASE=https://iso.example.com/ibi/
curl --head --cacert "$CA_CERT" "$ISO_URL_BASE"
```

An HTTP 403 on a directory without an index is acceptable for this TLS check.
For a certificate trusted by the system, omit `--cacert` and omit the Secret's
`caCert` key. If using a private CA, supply the CA chain rather than the
server's private key. The HTTPS server's private key must stay on that server.

## Create the Secret and configure the ClusterTemplate

Set the actual namespace and directory values, then create the Secret directly
on the hub. Do not commit a manifest containing `privateKey` to Git; see
[Handling Secrets](secrets-management.md).

```bash
TEMPLATE_NAMESPACE=sno-ran-du-v4-Y-Z
SECRET_NAME=iso-server-credentials
ISO_REMOTE_BASE=/srv/iso/ibi
ISO_CA_ARGS=()
if [[ -n "${CA_CERT:-}" ]]; then
    ISO_CA_ARGS=(--from-file=caCert="$CA_CERT")
fi

oc -n "$TEMPLATE_NAMESPACE" create secret generic "$SECRET_NAME" \
    --from-literal=host="$ISO_HOST" \
    --from-literal=username="$ISO_USER" \
    --from-file=privateKey="$KEY_DIR/id_ed25519" \
    --from-literal=remotePath="$ISO_REMOTE_BASE" \
    --from-file=knownHosts="$KEY_DIR/known_hosts" \
    "${ISO_CA_ARGS[@]}"
```

Leave `CA_CERT` unset for a publicly trusted certificate. Confirm that the
Secret has the expected keys without printing their contents:

```bash
oc -n "$TEMPLATE_NAMESPACE" describe secret "$SECRET_NAME"
```

Add the matching configuration to the ClusterTemplate:

```yaml
spec:
  templateDefaults:
    upgradeDefaults:
      seedGeneration:
        liveISO:
          uploadSecretRef:
            name: iso-server-credentials
          urlBase: https://iso.example.com/ibi/
          # Optional public key for SSH access as core during pre-provisioning.
          # sshKey: "ssh-ed25519 AAAA... user@example"
```

Set `sshKey` to the complete public-key line from your `.pub` file to enable
SSH access as `core` while the live ISO pre-provisions the server. This is
separate from the upload Secret's `privateKey`, which the controller uses to
copy the ISO to the upload server. If `sshKey` is omitted, the ISO has no
debugging SSH key. See [Monitor pre-provisioning progress](./ibi-based-cluster-provisioning.md#monitoring-pre-provisioning-progress)
for SSH and log commands.

`releaseImage` defaults to the installed cluster's ClusterImageSet release
image, and `installationDisk` defaults to the single seed node's
`rootDeviceHints.deviceName`. Set either in `liveISO` only when that value is
not suitable for the ISO. For a disconnected build that needs a mirrored base
RHCOS ISO, set the optional template-owned `liveISO.baseISOURL` to its HTTPS
URL. The upload Secret's `caCert` is also used to validate that URL if present.

The controller validates the upload Secret, SSH access, and HTTPS TLS before
starting hardware provisioning. After the build, it verifies the published
ISO bytes over HTTPS and records `isoURL` and `isoDigest` in
`status.extensions.clusterDetails.seedGenerationStatus`.

Before submitting the build Job, the controller records a versioned fingerprint
of its template in `seedGenerationStatus.isoJobSpecHash`. Operator updates
validate an existing Job against that saved fingerprint, so changes to the
operator's embedded build script affect new Jobs without invalidating an
active build. Ownership, Job identity, and execution safeguards are still
checked on every observation.

If retaining redacted failure diagnostics encounters a transient API error,
the controller keeps the Job and Pods available while it retries, up to the
persisted overall seed-generation deadline. Cleanup continues once the
diagnostics reference has been persisted. At the deadline, the controller
makes a final best-effort retention attempt, marks the run `TimedOut`, and
proceeds with cleanup even if diagnostics could not be retained. Permanent
ownership or content conflicts also use best-effort cleanup and report that
the logs could not be retained.
