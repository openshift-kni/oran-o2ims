#!/bin/bash

# Self-contained test for the Secret redaction pipeline used by
# gather_bmh_preprovisioning_secrets() in must-gather/gather.
#
# The redaction below MUST stay in sync with redact_secret_yaml() in
# must-gather/gather. It exercises the two guarantees the collection
# relies on:
#   1. Values under the top-level data: block are redacted.
#   2. The kubectl.kubernetes.io/last-applied-configuration annotation
#      (present on apply-created Secrets, carrying a full copy of the
#      data values) is redacted.
#
# The redaction is YAML-aware (yq), so the guarantee holds regardless of
# how the YAML serializer renders the annotation value. This test covers
# both renderings: an inline single-quoted scalar on one physical line
# (the value is newline-free compact JSON) and a multi-line block scalar
# (the form a different serializer/version could emit), asserting that no
# Secret material survives either way.
#
# Requires yq (mikefarah) on PATH, matching the binary bundled into the
# must-gather image by Dockerfile.must-gather.
#
# Run manually: ./must-gather/tests/redact_secret_test.sh

set -uo pipefail

if ! command -v yq >/dev/null 2>&1; then
    echo "SKIP: yq not found on PATH; install mikefarah/yq to run this test" >&2
    exit 127
fi

# redact_secret_yaml applies the same YAML-aware redaction as
# redact_secret_yaml() in must-gather/gather.
redact_secret_yaml() {
    yq 'with(select(.data != null); .data.[] |= "REDACTED") | with(select(.metadata.annotations."kubectl.kubernetes.io/last-applied-configuration" != null); .metadata.annotations."kubectl.kubernetes.io/last-applied-configuration" = "REDACTED")'
}

failures=0

fail() {
    echo "FAIL: $1"
    failures=$((failures + 1))
}

pass() {
    echo "PASS: $1"
}

# A representative apply-created Secret with the last-applied-configuration
# annotation rendered as an inline single-quoted scalar on one line whose
# JSON embeds the full data values (how the currently pinned serializer
# renders it).
secret_with_inline_annotation() {
    cat <<'EOF'
apiVersion: v1
data:
  nmstate: aW50ZXJmYWNlczoKICAtIG5hbWU6IGVubzEK
kind: Secret
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"v1","data":{"nmstate":"aW50ZXJmYWNlczoKICAtIG5hbWU6IGVubzEK"},"kind":"Secret","metadata":{"annotations":{},"name":"bmh-netdata","namespace":"openshift-machine-api"},"type":"Opaque"}'
  name: bmh-netdata
  namespace: openshift-machine-api
type: Opaque
EOF
}

# The same Secret with the annotation rendered as a multi-line block scalar
# (|). A YAML-unaware line substitution would mask only the header line and
# leave the indented JSON payload — including the Secret data — behind. The
# YAML-aware redaction must collapse the whole scalar.
secret_with_block_annotation() {
    cat <<'EOF'
apiVersion: v1
data:
  nmstate: aW50ZXJmYWNlczoKICAtIG5hbWU6IGVubzEK
kind: Secret
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"v1","data":{"nmstate":"aW50ZXJmYWNlczoKICAtIG5hbWU6IGVubzEK"},"kind":"Secret","metadata":{"annotations":{},"name":"bmh-netdata","namespace":"openshift-machine-api"},"type":"Opaque"}
  name: bmh-netdata
  namespace: openshift-machine-api
type: Opaque
EOF
}

# A Secret created without apply carries no last-applied-configuration
# annotation; the redaction must not invent one.
secret_without_annotation() {
    cat <<'EOF'
apiVersion: v1
data:
  nmstate: aW50ZXJmYWNlczoK
  extra: Zm9vYmFy
kind: Secret
metadata:
  name: bmh-netdata2
  namespace: openshift-machine-api
type: Opaque
EOF
}

# assert_annotation_redacted runs the shared assertions against a fixture
# whose annotation is rendered in the given style ("inline" or "block").
assert_annotation_redacted() {
    local style="$1"
    local output="$2"

    # The annotation value is redacted to exactly REDACTED, whatever the
    # input scalar style was.
    local ann
    ann=$(echo "${output}" | yq '.metadata.annotations."kubectl.kubernetes.io/last-applied-configuration"')
    if [ "${ann}" == "REDACTED" ]; then
        pass "${style}: last-applied-configuration annotation is redacted"
    else
        fail "${style}: annotation not redacted (got: ${ann})"
    fi

    # No base64/plaintext Secret material survives via the annotation,
    # including any indented block-scalar payload lines.
    if echo "${output}" | grep -qE 'aW50ZXJmYWNlcz|"nmstate":"'; then
        fail "${style}: secret material leaked into redacted output"
    else
        pass "${style}: no secret material remains in redacted output"
    fi

    # Top-level data: values are still redacted (regression).
    local data_val
    data_val=$(echo "${output}" | yq '.data.nmstate')
    if [ "${data_val}" == "REDACTED" ]; then
        pass "${style}: top-level data: values are redacted"
    else
        fail "${style}: top-level data: value not redacted (got: ${data_val})"
    fi

    # Data key names are preserved (only values are masked).
    if [ "$(echo "${output}" | yq '.data | has("nmstate")')" == "true" ]; then
        pass "${style}: data key name preserved"
    else
        fail "${style}: data key name was removed"
    fi

    # Non-sensitive metadata is preserved.
    local field
    for field in \
        '.metadata.name=bmh-netdata' \
        '.metadata.namespace=openshift-machine-api' \
        '.type=Opaque'; do
        local path="${field%%=*}" want="${field#*=}" got
        got=$(echo "${output}" | yq "${path}")
        if [ "${got}" == "${want}" ]; then
            pass "${style}: preserved ${path}=${want}"
        else
            fail "${style}: expected ${path}=${want}, got ${got}"
        fi
    done
}

# Tests 1-N: inline and block-scalar renderings both redact fully.
assert_annotation_redacted "inline" "$(secret_with_inline_annotation | redact_secret_yaml)"
assert_annotation_redacted "block" "$(secret_with_block_annotation | redact_secret_yaml)"

# A Secret without the annotation still has its data values redacted, leaks
# nothing, and gains no spurious last-applied-configuration annotation.
output2=$(secret_without_annotation | redact_secret_yaml)
if [ "$(echo "${output2}" | yq '.data.nmstate')" == "REDACTED" ] \
    && [ "$(echo "${output2}" | yq '.data.extra')" == "REDACTED" ]; then
    pass "annotation-less Secret still has data values redacted"
else
    fail "annotation-less Secret data values were not redacted"
fi
if echo "${output2}" | grep -qE 'aW50ZXJmYWNlcz|Zm9vYmFy'; then
    fail "annotation-less Secret leaked data values"
else
    pass "annotation-less Secret leaks no data values"
fi
if [ "$(echo "${output2}" | yq '.metadata | has("annotations")')" == "false" ]; then
    pass "annotation-less Secret gains no spurious annotation"
else
    fail "annotation-less Secret gained an unexpected annotation"
fi

echo
if [ "${failures}" -eq 0 ]; then
    echo "All redaction tests passed"
    exit 0
fi
echo "${failures} redaction test(s) failed"
exit 1
