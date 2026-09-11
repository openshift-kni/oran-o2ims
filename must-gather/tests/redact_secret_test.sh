#!/bin/bash

# Self-contained test for the Secret redaction pipeline used by
# gather_bmh_preprovisioning_secrets() in must-gather/gather.
#
# The redaction below MUST stay in sync with the two sed pipes in
# must-gather/gather. It exercises the two guarantees the collection
# relies on:
#   1. Values under the top-level data: block are redacted.
#   2. The kubectl.kubernetes.io/last-applied-configuration annotation
#      (present on apply-created Secrets, carrying a full copy of the
#      data values) is redacted.
#
# The sample fixtures match how `oc get secret -o yaml` actually renders
# the annotation: a single-quoted scalar on one physical line (the value
# is newline-free compact JSON, so the YAML printer never uses a block
# scalar and never folds it across lines, even for multi-KB values).
#
# Run manually: ./must-gather/tests/redact_secret_test.sh

set -uo pipefail

# redact_secret_yaml applies the same two-stage sed redaction as
# gather_bmh_preprovisioning_secrets() in must-gather/gather.
redact_secret_yaml() {
    sed '/^data:/,/^[^ ]/{/^data:/!{/^[^ ]/!s/:.*$/: REDACTED/}}' \
        | sed 's#\(kubectl\.kubernetes\.io/last-applied-configuration:\).*#\1 REDACTED#'
}

failures=0

fail() {
    echo "FAIL: $1"
    failures=$((failures + 1))
}

pass() {
    echo "PASS: $1"
}

# A representative apply-created Secret as rendered by `oc get secret -o yaml`:
# the last-applied-configuration annotation is a single-quoted scalar on one
# line whose JSON embeds the full data values. Note the metadata.name line
# immediately follows it — redaction must not consume that line.
secret_with_annotation() {
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

# A Secret created without apply carries no last-applied-configuration
# annotation; the second sed must be a no-op for it.
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

# Test 1: the annotation JSON value is redacted.
output=$(secret_with_annotation | redact_secret_yaml)
if echo "${output}" | grep -qE '^ *kubectl.kubernetes.io/last-applied-configuration: REDACTED$'; then
    pass "last-applied-configuration annotation is redacted"
else
    fail "last-applied-configuration annotation was not redacted"
fi

# Test 2: no base64/plaintext secret material survives via the annotation.
if echo "${output}" | grep -qE 'aW50ZXJmYWNlcz|"nmstate":"'; then
    fail "secret material leaked into redacted output"
else
    pass "no secret material remains in redacted output"
fi

# Test 3: top-level data: values are still redacted (regression).
if echo "${output}" | grep -qE '^  nmstate: REDACTED$'; then
    pass "top-level data: values are redacted"
else
    fail "top-level data: value was not redacted"
fi

# Test 4: the line immediately following the annotation is preserved
# (guards against an over-consuming sed that deletes adjacent lines).
if echo "${output}" | grep -qE '^  name: bmh-netdata$'; then
    pass "line following the annotation is preserved"
else
    fail "line following the annotation was consumed by redaction"
fi

# Test 5: non-sensitive metadata is preserved.
for field in "name: bmh-netdata" "namespace: openshift-machine-api" "type: Opaque"; do
    if echo "${output}" | grep -qF "${field}"; then
        pass "preserved: ${field}"
    else
        fail "expected preserved field missing: ${field}"
    fi
done

# Test 6: data key names are preserved (only values are masked).
if echo "${output}" | grep -qE '^  nmstate:'; then
    pass "data key name preserved"
else
    fail "data key name was removed"
fi

# Test 7: line count is unchanged — redaction masks values in place and
# neither adds nor drops lines.
in_lines=$(secret_with_annotation | wc -l)
out_lines=$(echo "${output}" | wc -l)
if [ "${in_lines}" -eq "${out_lines}" ]; then
    pass "line count preserved (${out_lines})"
else
    fail "line count changed: ${in_lines} in, ${out_lines} out"
fi

# Test 8: a Secret without the annotation still has its data values
# redacted and passes through otherwise unchanged.
output2=$(secret_without_annotation | redact_secret_yaml)
if echo "${output2}" | grep -qE '^  nmstate: REDACTED$' \
    && echo "${output2}" | grep -qE '^  extra: REDACTED$'; then
    pass "annotation-less Secret still has data values redacted"
else
    fail "annotation-less Secret data values were not redacted"
fi
if echo "${output2}" | grep -qE 'aW50ZXJmYWNlcz|Zm9vYmFy'; then
    fail "annotation-less Secret leaked data values"
else
    pass "annotation-less Secret leaks no data values"
fi

echo
if [ "${failures}" -eq 0 ]; then
    echo "All redaction tests passed"
    exit 0
fi
echo "${failures} redaction test(s) failed"
exit 1
