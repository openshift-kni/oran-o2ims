<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Example: IBI Live ISO server with rootless Podman

> [!WARNING]
> **Example only: this server setup is outside O-Cloud Manager support.**
> This guide illustrates one way to provide SSH uploads and HTTPS downloads for
> IBI live ISOs. It is not a required architecture or a supported server deployment.
> You are responsible for adapting, securing, updating, and operating the host,
> accounts, container image, SSH service, certificates, and published artifacts.

This IBI live ISO server example runs Apache httpd in a rootless Podman
container and uses two dedicated Linux accounts:

| Account | Purpose and access |
| --- | --- |
| `ibiserver` | Owns the rootless container, Apache configuration, and TLS private key. Serves uploaded files through HTTPS. |
| `ibiupload` | Receives the operator's SSH uploads. Within the server tree, it can write only to `htdocs/liveiso` and traverse the parent directories. It cannot list those parents, change Apache configuration, or read the TLS private key. |

The upload account has its own home directory and runs normal SSH commands.
These filesystem permissions limit its access within the web-server tree;
they do not create a chroot or confine every command to the upload directory.
An SFTP-only account or a forced SSH command is incompatible with the
controller's remote command execution.

For the upload contract and required Secret fields, see
[Configure an IBI live ISO upload destination](seed-generation-live-iso-upload.md).
You can also refer directly to
[Create and authorize the upload key](#create-and-authorize-the-upload-key)
and [Create the upload Secret](#create-the-upload-secret).

## Prerequisites and example values

The commands assume a new Linux host with systemd, rootless Podman, OpenSSH
server with SFTP enabled, OpenSSL, curl, GNU coreutils, and filesystem ACL
tools (`setfacl` and `getfacl`). The firewall example uses firewalld. Run
administrator commands from an account with `sudo` access. Commands in the
`ibiserver` session and on the administration host are identified separately.

Use a DNS name that resolves to the server from the hub manager, ISO build
Jobs, and intended ISO consumers. Check the target BMC or provisioning
service's DNS, routing, HTTPS port, and certificate requirements as well.
Replace `iso.example.com` and the example network ranges with your values.

| Setting | Example |
| --- | --- |
| HTTPS origin | `https://iso.example.com:10443/` |
| HTTPS ISO base URL (`liveISO.urlBase`) | `https://iso.example.com:10443/liveiso/` |
| SSH endpoint | `ibiupload@iso.example.com`, port 22 |
| Server directory | `/home/ibiserver/httpd-server` |
| Apache document root | `/home/ibiserver/httpd-server/htdocs` |
| Upload base directory (Secret `remotePath`) | `/home/ibiserver/httpd-server/htdocs/liveiso` |

The resulting layout is:

```text
/home/ibiserver/httpd-server/               ibiserver manages this tree
├── httpd.conf                              Apache configuration
├── httpd-ssl.conf                          HTTPS virtual host
├── certs/                                  accessible only to ibiserver
│   ├── server.crt                          public TLS certificate
│   └── server.key                          TLS private key
└── htdocs/                                 Apache document root
    └── liveiso/                            ibiupload owns this directory
        └── seedgen/
            └── <ProvisioningRequest-UID>/
                └── rhcos-ibi.iso           published artifact
```

O-Cloud Manager appends `seedgen/<ProvisioningRequest UID>/rhcos-ibi.iso` to
both the upload base directory and HTTPS base URL. The UID is the Kubernetes
resource UID, not the ProvisioningRequest's name. The HTTPS URL and SSH file
path must identify the same file.

Generated ISOs contain the effective installation pull secret. Limit HTTPS
access to the hub and authorized provisioning consumers using your network
controls. This workflow does not configure HTTP download authentication, and
the run UID in the URL is not an access-control mechanism. The Apache
configuration below disables directory listing.

## Create the two accounts and configure access

On the server, as an administrator, create the accounts and enable lingering
for the account that runs Podman:

```bash
sudo useradd --create-home --shell /bin/bash ibiserver
sudo useradd --create-home --shell /bin/bash ibiupload
sudo chmod 700 /home/ibiserver /home/ibiupload
sudo loginctl enable-linger ibiserver
```

Arrange administrator login to `ibiserver` using your site's SSH policy.
The upload account's dedicated key is installed later; it does not need
access to the administrator's login key or the container owner's credentials.
Grant neither account sudo privileges.

Confirm that rootless Podman has subordinate UID and GID ranges for
`ibiserver`:

```bash
sudo getent passwd ibiserver ibiupload
grep '^ibiserver:' /etc/subuid /etc/subgid
sudo loginctl show-user ibiserver -p Linger -p RuntimePath
```

If the UID/GID ranges are missing, configure unused ranges following
the [rootless Podman setup instructions](https://github.com/podman-container-tools/podman/blob/main/docs/tutorials/rootless_tutorial.md)
and your distribution's account-management guidance.
Do not assign ranges already used by another account.

### Configure SSH access separately

Allow SSH on port 22 from the hub and administration hosts. Preserve existing
administrator SSH access and keep the current session open while configuring
the firewall. Configure and verify SSH independently of the HTTPS rules below.

For firewalld, identify the zone handling the server's incoming connections
with `sudo firewall-cmd --get-active-zones`. Replace `public` and the
documentation source ranges below with the appropriate zone and approved hub
and administration networks. These commands add rules to both the runtime and
permanent configurations without reloading the firewall:

```bash
FIREWALL_ZONE=public
SSH_FAMILY=ipv4
SSH_SOURCES=(192.0.2.0/24)

for network in "${SSH_SOURCES[@]}"; do
    rule="rule family=\"$SSH_FAMILY\" source address=\"$network\" port port=\"22\" protocol=\"tcp\" accept"
    sudo firewall-cmd --zone="$FIREWALL_ZONE" --add-rich-rule="$rule"
    sudo firewall-cmd --zone="$FIREWALL_ZONE" --permanent --add-rich-rule="$rule"
done
```

If SSH clients use IPv6, repeat this example with `SSH_FAMILY=ipv6` and the
approved IPv6 sources in `SSH_SOURCES`, for example `(2001:db8:1::/64)`.

Confirm a new SSH login from an approved administration host before closing
the original session or continuing. Keep existing administrator SSH rules in
place; this example adds access rather than replacing the host's SSH policy.

### Allow HTTPS downloads

Allow HTTPS on port 10443 from the hub and authorized ISO consumers. Use a
separate source list for these clients; ISO consumers do not need SSH access.
Using the firewalld zone selected above, replace the documentation ranges
with your approved HTTPS source networks:

```bash
HTTPS_FAMILY=ipv4
HTTPS_SOURCES=(192.0.2.0/24 198.51.100.0/24)

for network in "${HTTPS_SOURCES[@]}"; do
    rule="rule family=\"$HTTPS_FAMILY\" source address=\"$network\" port port=\"10443\" protocol=\"tcp\" accept"
    sudo firewall-cmd --zone="$FIREWALL_ZONE" --add-rich-rule="$rule"
    sudo firewall-cmd --zone="$FIREWALL_ZONE" --permanent --add-rich-rule="$rule"
done
```

If HTTPS clients use IPv6, repeat this example with `HTTPS_FAMILY=ipv6` and
the approved IPv6 sources in `HTTPS_SOURCES`, for example `(2001:db8:2::/64)`.
Check that broader existing rules do not expose HTTPS to unintended clients.
The example does not publish an HTTP port.

## Prepare the server files as ibiserver

Log in directly as `ibiserver` in a separate terminal:

```bash
ssh ibiserver@iso.example.com
```

Use this login session for all Podman commands below. A `su` or `sudo -iu`
session can lack the logind-managed runtime directory needed by rootless
Podman. Confirm the runtime directory before continuing:

```bash
RUNTIME_DIR="/run/user/$(id -u)"
if [[ "$XDG_RUNTIME_DIR" == "$RUNTIME_DIR" &&
      -d "$RUNTIME_DIR" && -w "$RUNTIME_DIR" ]]; then
    echo "PASS: rootless Podman runtime directory is available"
else
    echo "FAIL: log in directly as ibiserver and check its logind session"
    exit 1
fi
```

Lingering keeps the user's systemd manager available after logout. It does
not configure the container to start after a host reboot. See
[Operating the example server](#operating-the-example-server) for that
distinction.

Set the server name from the host's fully qualified hostname and create
the directories:

```bash
ISO_HOST=$(hostname -f)
printf 'HTTPS hostname: %s\n' "$ISO_HOST"
HTTPD_IMAGE=docker.io/library/httpd:2.4
SERVER_ROOT="$HOME/httpd-server"
HTDOCS="$SERVER_ROOT/htdocs"
CERTS="$SERVER_ROOT/certs"

install -d -m 700 "$SERVER_ROOT" "$CERTS"
install -d -m 755 "$HTDOCS" "$HTDOCS/liveiso"
```

Confirm that `ISO_HOST` is the DNS name used in the HTTPS URL. If clients use
a DNS alias, set `ISO_HOST` to that alias before creating the certificate.
This example uses the [Docker Official Image for httpd](https://hub.docker.com/_/httpd).
Select and maintain an image version or digest appropriate to your
environment, and use the same image to obtain the configuration and run
the container.

### Create the TLS certificate

For this example, generate a self-signed certificate with the HTTPS hostname
in its Subject Alternative Name. An organizational CA-issued certificate can
be used in its place. Keep the private key on the server.

```bash
openssl req -x509 -nodes -days 365 -newkey rsa:3072 \
    -keyout "$CERTS/server.key" \
    -out "$CERTS/server.crt" \
    -subj "/CN=$ISO_HOST" \
    -addext "subjectAltName=DNS:$ISO_HOST" \
    -addext "basicConstraints=critical,CA:FALSE" \
    -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
    -addext "extendedKeyUsage=serverAuth"
chmod 600 "$CERTS/server.key"
openssl x509 -in "$CERTS/server.crt" -noout -ext subjectAltName -enddate
```

The client must trust this certificate and use a hostname in its SAN.
The certificate's public half is later placed in the upload Secret's
`caCert` key. The HTTPS server's private key is never placed in that Secret.
Choose a certificate and trust chain acceptable to the intended ISO consumers.

### Configure Apache

Copy the default configuration from the selected image and enable its SSL
modules and SSL configuration include:

```bash
podman create --name liveiso-config-copy "$HTTPD_IMAGE"
podman cp liveiso-config-copy:/usr/local/apache2/conf/httpd.conf \
    "$SERVER_ROOT/httpd.conf"
podman rm liveiso-config-copy

sed -i \
    -e 's/^#\(LoadModule .*mod_ssl.so\)/\1/' \
    -e 's/^#\(LoadModule .*mod_socache_shmcb.so\)/\1/' \
    -e 's/^#\(Include .*httpd-ssl.conf\)/\1/' \
    "$SERVER_ROOT/httpd.conf"
printf '\nServerName %s\n' "$ISO_HOST" >> "$SERVER_ROOT/httpd.conf"

cat >> "$SERVER_ROOT/httpd.conf" <<'APACHE_DIRECTORY'
<Directory "/usr/local/apache2/htdocs">
    Options -Indexes -FollowSymLinks
    AllowOverride None
    Require all granted
</Directory>
APACHE_DIRECTORY

cat > "$SERVER_ROOT/httpd-ssl.conf" <<'APACHE_SSL'
Listen 443
SSLSessionCache "shmcb:/usr/local/apache2/logs/ssl_scache(512000)"

<VirtualHost *:443>
    SSLEngine on
    SSLCertificateFile "/usr/local/apache2/conf/server.crt"
    SSLCertificateKeyFile "/usr/local/apache2/conf/server.key"
    DocumentRoot "/usr/local/apache2/htdocs"
</VirtualHost>
APACHE_SSL

chmod 600 "$SERVER_ROOT/httpd.conf" "$SERVER_ROOT/httpd-ssl.conf"
```

### Start and verify the rootless container

Start Apache with read-only bind mounts and an unprivileged host port.
The `:z` mount option applies a shared SELinux container-file label to the
example's server files. Keep these dedicated directories separate from
system configuration directories.

```bash
podman run --detach --name liveiso-httpd \
    -v "$HTDOCS":/usr/local/apache2/htdocs:ro,z \
    -v "$CERTS/server.crt":/usr/local/apache2/conf/server.crt:ro,z \
    -v "$CERTS/server.key":/usr/local/apache2/conf/server.key:ro,z \
    -v "$SERVER_ROOT/httpd.conf":/usr/local/apache2/conf/httpd.conf:ro,z \
    -v "$SERVER_ROOT/httpd-ssl.conf":/usr/local/apache2/conf/extra/httpd-ssl.conf:ro,z \
    -p 10443:443 \
    "$HTTPD_IMAGE"
podman exec liveiso-httpd httpd -t
podman ps
podman logs liveiso-httpd
```

The read-only document-root mount prevents writes through the container;
SSH uploads write the corresponding host directory. Set up the container
and its SELinux labels before transferring the upload directory's ownership
in the next section.

Create and fetch a small test file as `ibiserver`:

```bash
printf 'IBI live ISO server ready\n' > "$HTDOCS/health.txt"
chmod 644 "$HTDOCS/health.txt"
curl --fail --show-error --cacert "$CERTS/server.crt" \
    "https://$ISO_HOST:10443/health.txt"
```

Expect `IBI live ISO server ready`. A request to the empty `/liveiso/` directory
should return HTTP 403 because directory listing is disabled. Verify HTTPS
from an authorized host on the hub network as well, using a trusted copy of
the public certificate.

Exit the `ibiserver` session and return to the administrator account for the
next section.

## Grant the upload account access to one directory

As the server administrator, allow `ibiupload` to traverse the parent
directories without listing or writing them, and give it ownership of the
single upload directory:

```bash
for directory in \
    /home/ibiserver \
    /home/ibiserver/httpd-server \
    /home/ibiserver/httpd-server/htdocs; do
    sudo setfacl -m u:ibiupload:--x "$directory"
done
sudo chown ibiupload:ibiupload \
    /home/ibiserver/httpd-server/htdocs/liveiso
sudo chmod 755 /home/ibiserver/httpd-server/htdocs/liveiso
```

Do not change ownership of the whole `ibiserver` home, Apache configuration,
certificate directory, or Podman storage. The upload directory remains
readable and traversable by Apache, while `ibiupload` cannot read the private
configuration and certificate files.

Check the permissions, with explicit pass/fail output:

```bash
for directory in \
    /home/ibiserver \
    /home/ibiserver/httpd-server \
    /home/ibiserver/httpd-server/htdocs; do
    if sudo -u ibiupload test -x "$directory" &&
       sudo -u ibiupload test ! -r "$directory" &&
       sudo -u ibiupload test ! -w "$directory"; then
        echo "PASS: ibiupload can only traverse $directory"
    else
        echo "FAIL: unexpected parent permissions on $directory"
    fi
done

if sudo -u ibiupload test -w \
    /home/ibiserver/httpd-server/htdocs/liveiso; then
    echo "PASS: ibiupload can write to liveiso"
else
    echo "FAIL: ibiupload cannot write to liveiso"
fi

for file in \
    /home/ibiserver/httpd-server/httpd.conf \
    /home/ibiserver/httpd-server/httpd-ssl.conf \
    /home/ibiserver/httpd-server/certs/server.key; do
    if sudo test -f "$file" && sudo -u ibiupload test ! -r "$file"; then
        echo "PASS: ibiupload cannot read $file"
    else
        echo "FAIL: missing file or unexpected read access to $file"
    fi
done
```

## Create and authorize the upload key

**This is the key pair for O-Cloud Manager uploads.** Generate it on a trusted
administration host, outside Git. It is separate from any administrator login
key, the SSH server's host key, and any optional `liveISO.sshKey` embedded in
the generated ISO.

The controller needs an unencrypted private key for unattended uploads.
These commands use Ed25519; choose an algorithm supported by both endpoints
and your environment's cryptographic policy.

```bash
ISO_HOST=iso.example.com
ISO_USER=ibiupload
ISO_REMOTE_BASE=/home/ibiserver/httpd-server/htdocs/liveiso
KEY_DIR="$HOME/.config/oran-o2ims/iso-upload"
install -d -m 700 "$KEY_DIR"
ssh-keygen -t ed25519 -N '' -f "$KEY_DIR/id_ed25519"
chmod 600 "$KEY_DIR/id_ed25519"
```

Transfer **only** `id_ed25519.pub` to the server administrator through a
trusted channel. On the server, install it for `ibiupload`, replacing
`/path/to/id_ed25519.pub` with the received public-key file:

```bash
sudo install -d -m 700 -o ibiupload -g ibiupload /home/ibiupload/.ssh
sed 's/^/restrict /' /path/to/id_ed25519.pub | \
    sudo tee -a /home/ibiupload/.ssh/authorized_keys > /dev/null
sudo chown ibiupload:ibiupload /home/ibiupload/.ssh/authorized_keys
sudo chmod 600 /home/ibiupload/.ssh/authorized_keys
sudo restorecon -RF /home/ibiupload/.ssh
```

The OpenSSH `restrict` key option disables forwarding and terminal allocation
while permitting the command execution and SFTP transfers this workflow
needs. Confirm that your SSH account policy allows these operations. Do not
configure `ForceCommand internal-sftp` or a forced command for this key.
On hosts without SELinux, omit the `restorecon` command.

### Pin the SSH server host key

On the server, obtain the SSH host-key fingerprint through the administrator:

```bash
sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

On the administration host, collect the public host key and compare its
fingerprint with that trusted result:

```bash
ssh-keyscan -T 10 -t ed25519 "$ISO_HOST" > "$KEY_DIR/known_hosts"
ssh-keygen -lf "$KEY_DIR/known_hosts"
```

Continue only if the fingerprints match. `ssh-keyscan` alone does not
authenticate the server. The entry must match the exact name in the Secret's
`host` key; use the server's enabled host-key algorithm if it differs from
this example.

### Verify the upload path over SSH

On the administration host, test a real write under the upload directory:

```bash
if ssh -i "$KEY_DIR/id_ed25519" -o BatchMode=yes -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile="$KEY_DIR/known_hosts" \
    "$ISO_USER@$ISO_HOST" \
    "set -eu; umask 022; mkdir -p '$ISO_REMOTE_BASE/seedgen'; \
    probe=\$(mktemp -d '$ISO_REMOTE_BASE/seedgen/.access-check.XXXXXX'); \
    trap 'rm -f -- \"\$probe/check\"; rmdir -- \"\$probe\"' EXIT; \
    printf 'upload access check\n' > \"\$probe/check\"; \
    sha256sum \"\$probe/check\""; then
    echo "PASS: SSH upload access and remote checksum command"
else
    echo "FAIL: SSH authentication, host key, permissions, or SELinux access"
fi
```

This checks actual remote command execution and file creation; a local
`test -w` check alone does not catch all SSH or SELinux failures. It does not
replace verification of the HTTPS download path.

### Obtain and verify the public HTTPS certificate

For the self-signed certificate used by this example, record its fingerprint
on the server through the trusted administrator:

```bash
sudo openssl x509 \
    -in /home/ibiserver/httpd-server/certs/server.crt \
    -noout -fingerprint -sha256
```

On the administration host, retrieve the presented certificate and compare
its fingerprint with the trusted result before using it:

```bash
openssl s_client -connect "$ISO_HOST:10443" -servername "$ISO_HOST" \
    -showcerts </dev/null 2>/dev/null | \
    openssl x509 -out "$KEY_DIR/server.crt"
openssl x509 -in "$KEY_DIR/server.crt" -noout -fingerprint -sha256
curl --fail --show-error --cacert "$KEY_DIR/server.crt" \
    "https://$ISO_HOST:10443/health.txt"
```

The operator uses the same public certificate to verify HTTPS. Never copy
`server.key` to the administration host or into a Kubernetes Secret.

### Verify an uploaded file through HTTPS

Use a small temporary file to check the same SSH/SFTP upload and HTTPS download
mapping that the operator uses. Run on the administration host, then remove the
test file from both hosts:

```bash
SSH_OPTIONS=(-i "$KEY_DIR/id_ed25519" -o BatchMode=yes -o IdentitiesOnly=yes
    -o StrictHostKeyChecking=yes -o UserKnownHostsFile="$KEY_DIR/known_hosts")
PROBE_FILE=$(mktemp "$KEY_DIR/upload-check.XXXXXX")
PROBE_NAME=$(basename "$PROBE_FILE")
printf 'uploaded file is served through HTTPS\n' > "$PROBE_FILE"

scp "${SSH_OPTIONS[@]}" "$PROBE_FILE" \
    "$ISO_USER@$ISO_HOST:$ISO_REMOTE_BASE/$PROBE_NAME"
ssh "${SSH_OPTIONS[@]}" "$ISO_USER@$ISO_HOST" \
    "chmod 0644 -- '$ISO_REMOTE_BASE/$PROBE_NAME'; sha256sum -- '$ISO_REMOTE_BASE/$PROBE_NAME'"
sha256sum "$PROBE_FILE"
curl --fail --show-error --cacert "$KEY_DIR/server.crt" \
    "https://$ISO_HOST:10443/liveiso/$PROBE_NAME"

ssh "${SSH_OPTIONS[@]}" "$ISO_USER@$ISO_HOST" \
    "rm -f -- '$ISO_REMOTE_BASE/$PROBE_NAME'"
rm -f -- "$PROBE_FILE"
```

The local and remote checksums must match, and the HTTPS response must contain
the test text. Resolve any failure before starting a seed-generation request.

## Create the upload Secret

**Create the Secret in the same namespace as the seed-generation
ClusterTemplate.** Run the following commands on the administration host with
an `oc` context for the hub. Use the variables from the upload-key section
and replace the namespace with your ClusterTemplate's namespace:

```bash
TEMPLATE_NAMESPACE=seed-templates
SECRET_NAME=iso-server-credentials

oc -n "$TEMPLATE_NAMESPACE" create secret generic "$SECRET_NAME" \
    --from-literal=host="$ISO_HOST" \
    --from-literal=username="$ISO_USER" \
    --from-file=privateKey="$KEY_DIR/id_ed25519" \
    --from-literal=remotePath="$ISO_REMOTE_BASE" \
    --from-file=knownHosts="$KEY_DIR/known_hosts" \
    --from-file=caCert="$KEY_DIR/server.crt"

oc -n "$TEMPLATE_NAMESPACE" describe secret "$SECRET_NAME"
```

`oc describe` lists the keys and their sizes without printing their values.
This Secret contains the **upload SSH private key**, not Apache's TLS private
key or an ISO debugging key. Keep private-key files and populated Secret
manifests outside Git; see [Handling Secrets](secrets-management.md).

The `caCert` value above applies to this example's self-signed certificate.
For a publicly trusted certificate, omit `caCert`. For a private CA-issued
certificate, use the appropriate PEM CA chain as described in the
[upload destination guide](seed-generation-live-iso-upload.md#verify-https-trust).

## Configure the ClusterTemplate

Add the matching settings under
`spec.templateDefaults.upgradeDefaults.seedGeneration`:

```yaml
liveISO:
  uploadSecretRef:
    name: iso-server-credentials
  urlBase: https://iso.example.com:10443/liveiso/
  # Optional public key for core SSH access while the ISO pre-provisions a host.
  # sshKey: "ssh-ed25519 AAAA... administrator@example.com"
```

The `urlBase` maps to the Secret's `remotePath`; it does not include the
generated `seedgen/<ProvisioningRequest UID>/rhcos-ibi.iso` suffix.
`liveISO` remains owned by the ClusterTemplate. For release-image and
installation-disk defaults, monitoring, and cleanup of the factory request, see
the [automated IBI workflow](ibi-based-cluster-provisioning.md#automated-seed-image-and-live-iso-generation).

## Operating the example server

Manage the container from a direct `ibiserver` login:

```bash
podman ps -a
podman logs liveiso-httpd
podman stop liveiso-httpd
podman start liveiso-httpd
```

Use `podman start` for an existing stopped container; the initial `podman run`
command creates a new one. Enabling lingering keeps the user manager alive
after logout, but automatic container startup after a reboot requires a user
systemd service, for example [a rootless Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html).
Consult your distribution's service-management guidance.

Maintain the container image, OS packages, certificates, upload keys, and
host-key pins. After SSH host-key rotation, verify the new fingerprint and
update the upload Secret's `knownHosts` data before the next run. Remove
published ISOs when they are no longer needed; deleting a ProvisioningRequest
does not delete a successfully published ISO.

If Podman cannot write to `/run/user/<UID>`, check the direct login session
and `loginctl show-user ibiserver -p Linger -p RuntimePath`. Do not change the
runtime directory's ownership manually. For upload permission errors, review
the parent ACLs, directory ownership, SSH authorization, and SELinux labels.
Verify both SSH writes and HTTPS reads after any permissions or container
changes.

### Rotate the HTTPS certificate

Apache loads its certificate and private key at startup. Install the new
matching certificate and key at `certs/server.crt` and `certs/server.key`,
keeping them owned by `ibiserver` with the container-readable SELinux labels
used during setup. As `ibiserver`, restrict the private key's permissions and
restart the container so Apache loads the new files and Podman refreshes the
individual file bind mounts:

```bash
CERTS="$HOME/httpd-server/certs"
chmod 600 "$CERTS/server.key"
podman restart liveiso-httpd
podman logs liveiso-httpd
openssl x509 -in "$CERTS/server.crt" -noout -fingerprint -sha256
```

On the administration host, repeat
[Obtain and verify the public HTTPS certificate](#obtain-and-verify-the-public-https-certificate)
to retrieve the served certificate, compare it with the new fingerprint from
the server, and verify the HTTPS response. Complete this check before
refreshing the Secret.

For this example's self-signed certificate, update the existing Secret using
the original `TEMPLATE_NAMESPACE`, `SECRET_NAME`, and `KEY_DIR` values:

```bash
oc -n "$TEMPLATE_NAMESPACE" set data "secret/$SECRET_NAME" \
    --from-file=caCert="$KEY_DIR/server.crt"
```

For a certificate issued by a private CA, supply the appropriate CA chain
instead, following the [HTTPS trust guidance](seed-generation-live-iso-upload.md#verify-https-trust).
Verify the upload destination before starting another seed-generation request.

## References

- [O-Cloud Manager IBI live ISO upload contract](seed-generation-live-iso-upload.md)
- [Docker Official Image for httpd](https://hub.docker.com/_/httpd)
- [Podman volume options and SELinux labels](https://docs.podman.io/en/latest/markdown/podman-run.1.html)
- [Apache Options directive](https://httpd.apache.org/docs/2.4/mod/core.html#options)
- [Apache TLS certificate configuration](https://httpd.apache.org/docs/2.4/mod/mod_ssl.html#sslcertificatefile)
- [OpenSSH authorized_keys options](https://man.openbsd.org/sshd.8#AUTHORIZED_KEYS_FILE_FORMAT)
- [systemd loginctl and lingering](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)
