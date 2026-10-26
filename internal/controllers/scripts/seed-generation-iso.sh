#!/bin/bash
# SPDX-FileCopyrightText: Red Hat
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
umask 022
STAGE=initializing
REMOTE_OWNED=0
WORKSPACE_LOCKED=0
REMOTE_DIR=
REMOTE_OWNER=
REMOTE_TMP=
REMOTE_FINAL=
SSH_TARGET=
SSH_OPTIONS=()
LOCAL_TEMP=/tmp/seedgen
CONFIG_LINK=/workspace/build/image-based-installation-config.yaml
RESULT_FILE=/workspace/.oran-seedgen-result

# Redact before writing to stdout: cluster log collectors may retain Pod logs
# even after the controller has deleted the Pod and its temporary volume.
diagnostics() {
    python3 - "$LOCAL_TEMP" <<'PY'
import base64
import json
import os
import re
import sys

try:
    credentials = set()

    def collect(value):
        if isinstance(value, dict):
            for item in value.values():
                collect(item)
            auth = value.get("auth")
            if isinstance(auth, str):
                try:
                    decoded = base64.b64decode(auth + "===").decode()
                    credentials.add(decoded)
                    if ":" in decoded:
                        credentials.add(decoded.split(":", 1)[1])
                except (ValueError, UnicodeError):
                    pass
        elif isinstance(value, list):
            for item in value:
                collect(item)
        elif isinstance(value, str) and value:
            credentials.update((value, json.dumps(value)[1:-1]))

    for directory in ("/var/run/seedgen/config", "/var/run/seedgen/upload"):
        for name in os.listdir(directory):
            filename = os.path.join(directory, name)
            if name.startswith(".") or not os.path.isfile(filename):
                continue
            with open(filename, "rb") as source:
                data = source.read()
            value = data.decode(errors="replace")
            credentials.update((value, base64.b64encode(data).decode()))
            if "PRIVATE KEY" in value:
                credentials.update(value.splitlines())
            try:
                collect(json.loads(value))
            except (ValueError, TypeError):
                pass

    for name in ("release-extract.log", "openshift-install.log", "scp.log", "curl.log"):
        filename = os.path.join(sys.argv[1], name)
        if not os.path.isfile(filename):
            continue
        with open(filename, "rb") as source:
            size = source.seek(0, os.SEEK_END)
            source.seek(max(0, size - 1024 * 1024))
            if size > 1024 * 1024:
                source.readline()  # Discard the initial partial credential/log line.
            text = source.read().decode(errors="replace")
        for value in sorted(filter(None, credentials), key=len, reverse=True):
            text = text.replace(value, "[REDACTED]")
        text = re.sub(r"(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----", "[REDACTED PRIVATE KEY]", text)
        text = re.sub(r"(?im)^.*(?:authorization|password|token|secret|credential|private[_ -]?key|auth)\s*[:=].*$", "[REDACTED SENSITIVE LOG LINE]", text)
        text = re.sub(r"(?i)(bearer\s+)[a-z0-9._~+/-]+=*", r"\1[REDACTED]", text)
        text = re.sub(r"(?i)(https?://)[^/@\s]+:[^/@\s]+@", r"\1[REDACTED]@", text)
        text = "".join(c for c in text if c in "\n\t" or ord(c) >= 32 and ord(c) != 127)
        # Truncate after redaction so a boundary cannot expose part of a key.
        tail = text.encode()[-48 * 1024:].decode(errors="replace")
        if tail:
            print("\n--- " + name + " (redacted tail) ---\n" + tail)
except Exception:
    # Never echo an exception or the input if the redactor fails.
    print("ISO command diagnostics could not be safely retained")
PY
}

cleanup() {
    result=$?
    if [[ "$result" -ne 0 ]]; then
        printf 'SEEDGEN_FAILURE=%s\n' "$STAGE" > /dev/termination-log
        diagnostics || true
        if [[ "$REMOTE_OWNED" -eq 1 && -n "$SSH_TARGET" ]]; then
            # Only the attempt that reserved the directory may remove its files.
            # shellcheck disable=SC2029
            ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" \
                "if test -f '$REMOTE_OWNER' && test \"\$(cat '$REMOTE_OWNER')\" = '$ISO_PR_UID'; then rm -f -- '$REMOTE_TMP' '$REMOTE_FINAL' '$REMOTE_OWNER'; rmdir -- '$REMOTE_DIR'; fi" \
                >/dev/null 2>&1 || true
        fi
    fi
    rm -f -- "$LOCAL_TEMP/id_rsa" "$LOCAL_TEMP/ca-bundle.pem"
    if [[ "$WORKSPACE_LOCKED" -eq 1 ]]; then
        rm -f -- "$CONFIG_LINK"
    fi
}
trap cleanup EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

for binary in ssh scp sha256sum curl oc cat cp chmod mkdir cut mv rm ln rmdir python3; do
    command -v "$binary" >/dev/null 2>&1 || { echo "required tool is missing: $binary" >&2; exit 1; }
done
mkdir -p "$LOCAL_TEMP/home" "$LOCAL_TEMP/tmp" /workspace/tools /workspace/build
# Jobs can start duplicate Pods. Keep the lock's file descriptor open in this
# shell so no second attempt can change the workspace until cleanup finishes.
exec 9>/workspace/.oran-seedgen-build.lock
python3 - <<'PY'
import fcntl
fcntl.flock(9, fcntl.LOCK_EX)
PY
WORKSPACE_LOCKED=1

# A later Pod reuses the result only after the first attempt has verified HTTPS
# content and atomically recorded the result on this run's private workspace.
if [[ -f "$RESULT_FILE" ]]; then
    STAGE=loading-build-result
    mapfile -t result < "$RESULT_FILE"
    if [[ "${#result[@]}" -ne 3 || "${result[0]}" != "$ISO_PR_UID" || "${result[1]}" != "$ISO_URL" || ! "${result[2]}" =~ ^sha256:[a-f0-9]{64}$ ]]; then
        exit 1
    fi
    printf '%s\n%s\n' "${result[1]}" "${result[2]}" > /dev/termination-log
    exit 0
fi
INSTALL_CONFIG=/var/run/seedgen/config/image-based-installation-config.yaml
ln -s "$INSTALL_CONFIG" "$CONFIG_LINK"

if [[ -s /var/run/seedgen/upload/caCert ]]; then
    if [[ -r /etc/pki/tls/certs/ca-bundle.crt ]]; then
        SYSTEM_CA=/etc/pki/tls/certs/ca-bundle.crt
    elif [[ -r /etc/ssl/certs/ca-certificates.crt ]]; then
        SYSTEM_CA=/etc/ssl/certs/ca-certificates.crt
    else
        STAGE=loading-https-trust
        exit 1
    fi
    cat "$SYSTEM_CA" /var/run/seedgen/upload/caCert > "$LOCAL_TEMP/ca-bundle.pem"
    export SSL_CERT_FILE="$LOCAL_TEMP/ca-bundle.pem"
    export CURL_CA_BUNDLE="$LOCAL_TEMP/ca-bundle.pem"
fi

STAGE=preparing-upload
UPLOAD_HOST=$(cat /var/run/seedgen/upload/host)
UPLOAD_USER=$(cat /var/run/seedgen/upload/username)
REMOTE_BASE=$(cat /var/run/seedgen/upload/remotePath)
SSH_TARGET="${UPLOAD_USER}@${UPLOAD_HOST}"
SCP_HOST="$UPLOAD_HOST"
if [[ "$SCP_HOST" == *:* ]]; then
    SCP_HOST="[$SCP_HOST]"
fi
SCP_TARGET="${UPLOAD_USER}@${SCP_HOST}"
REMOTE_BASE=${REMOTE_BASE%/}
REMOTE_PARENT="${REMOTE_BASE}/seedgen"
REMOTE_DIR="${REMOTE_PARENT}/${ISO_PR_UID}"
REMOTE_OWNER="${REMOTE_DIR}/.oran-seedgen-owner"
REMOTE_FINAL="${REMOTE_DIR}/rhcos-ibi.iso"
REMOTE_TMP="${REMOTE_FINAL}.tmp-${ISO_PR_UID}"
cp /var/run/seedgen/upload/privateKey "$LOCAL_TEMP/id_rsa"
chmod 0600 "$LOCAL_TEMP/id_rsa"
SSH_OPTIONS=(-i "$LOCAL_TEMP/id_rsa" -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/run/seedgen/upload/knownHosts -o ConnectTimeout=10 -o ServerAliveInterval=5 -o ServerAliveCountMax=2)
if ! ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" true >/dev/null 2>&1; then
    STAGE=connecting-to-upload-server
    exit 1
fi
# The parent may be shared, but the run directory must be newly created.
# shellcheck disable=SC2029
if ! ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" "umask 022; mkdir -p -- '$REMOTE_PARENT'" >/dev/null 2>&1; then
    STAGE=preparing-remote-directory
    exit 1
fi
# shellcheck disable=SC2029
if ! ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" "set -eu; umask 022; mkdir -m 0755 -- '$REMOTE_DIR'; (umask 077; printf '%s\\n' '$ISO_PR_UID' > '$REMOTE_OWNER'); (set -C; : > '$REMOTE_TMP')" >/dev/null 2>&1; then
    STAGE=preparing-remote-file
    exit 1
fi
REMOTE_OWNED=1

STAGE=extracting-installer
printf '%s\n' 'SEEDGEN_STATUS=extracting-release'
if ! oc adm release extract --registry-config=/var/run/seedgen/config/config.json --idms-file=/var/run/seedgen/mirror/idms.yaml --command=openshift-install --to=/workspace/tools "$ISO_RELEASE_IMAGE" >"$LOCAL_TEMP/release-extract.log" 2>&1; then
    exit 1
fi
INSTALLER=/workspace/tools/openshift-install
if [[ ! -x "$INSTALLER" ]]; then
    exit 1
fi

STAGE=building-iso
printf '%s\n' 'SEEDGEN_STATUS=building-iso'
if ! "$INSTALLER" image-based create image --dir=/workspace/build >"$LOCAL_TEMP/openshift-install.log" 2>&1; then
    exit 1
fi
ISO_FILE=/workspace/build/rhcos-ibi.iso
if [[ ! -s "$ISO_FILE" ]]; then
    exit 1
fi
LOCAL_DIGEST=$(sha256sum "$ISO_FILE" | cut -d ' ' -f 1)

STAGE=uploading-iso
printf '%s\n' 'SEEDGEN_STATUS=uploading-iso'
if ! scp "${SSH_OPTIONS[@]}" "$ISO_FILE" "${SCP_TARGET}:${REMOTE_TMP}" >"$LOCAL_TEMP/scp.log" 2>&1; then
    exit 1
fi
STAGE=checking-remote-digest
# shellcheck disable=SC2029
REMOTE_DIGEST=$(ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" "sha256sum -- '$REMOTE_TMP'" 2>/dev/null | cut -d ' ' -f 1)
if [[ "$REMOTE_DIGEST" != "$LOCAL_DIGEST" ]]; then
    exit 1
fi
STAGE=publishing-remote-file
# shellcheck disable=SC2029
if ! ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" "chmod 0644 -- '$REMOTE_TMP'; mv -Tn -- '$REMOTE_TMP' '$REMOTE_FINAL' && test ! -e '$REMOTE_TMP'" >/dev/null 2>&1; then
    exit 1
fi

STAGE=verifying-https-content
printf '%s\n' 'SEEDGEN_STATUS=verifying-https'
if ! HTTPS_DIGEST=$(curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$ISO_URL" 2>"$LOCAL_TEMP/curl.log" | sha256sum | cut -d ' ' -f 1); then
    exit 1
fi
if [[ "$HTTPS_DIGEST" != "$LOCAL_DIGEST" ]]; then
    STAGE=checking-https-digest
    exit 1
fi
(umask 077; printf '%s\n%s\n%s\n' "$ISO_PR_UID" "$ISO_URL" "sha256:${LOCAL_DIGEST}" > "${RESULT_FILE}.tmp")
mv -- "${RESULT_FILE}.tmp" "$RESULT_FILE"
printf '%s\n%s\n' "$ISO_URL" "sha256:${LOCAL_DIGEST}" > /dev/termination-log
