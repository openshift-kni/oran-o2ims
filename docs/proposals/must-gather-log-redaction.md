<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Proposal: Redact Sensitive Fields in Must-Gather Log Collection

```yaml
title: must-gather-log-redaction
authors:
  - @dpenney
reviewers:
  - TBD
approvers:
  - TBD
creation-date: 2026-06-26
last-updated: 2026-06-26
```

## Summary

Add a post-collection redaction step to the must-gather script that masks
sensitive fields in collected pod logs before they are included in support
archives. The redaction must be consistent — the same value always maps to
the same pseudonym within a collection — so that events can be correlated
across log entries without exposing the original data.

## Motivation

Must-gather archives are routinely attached to support cases that cross
organizational boundaries. Pod logs collected by the must-gather script
may contain information that customers consider sensitive:

- **IP addresses** — client IPs, BMC management addresses, cluster API
  endpoints. These reveal network topology and infrastructure details.
- **Hostnames and FQDNs** — server names, cluster domains, BMC hostnames.
- **User identities** — OAuth usernames, group memberships logged during
  authentication and authorization.
- **MAC addresses** — hardware identifiers from BMH provisioning and
  network configuration.
- **Serial numbers** — server serial numbers from hardware inventory.

While the O-Cloud Manager's structured logging avoids logging credentials
(tokens, passwords, certificates), the fields above are logged routinely
for operational observability. Customers in regulated environments may
require these to be redacted before sharing logs externally.

### Goals

- Redact sensitive fields in collected pod logs during must-gather
  collection
- Preserve correlation — the same original value always produces the same
  pseudonym within a single must-gather collection
- Maintain log readability — redacted logs should still be useful for
  debugging (structure, timestamps, log levels, and non-sensitive fields
  are preserved)
- Make redaction configurable — allow operators to enable/disable
  redaction and choose which field categories to redact

### Non-Goals

- Redacting sensitive data at the logging source (this is already handled
  by the structured logging conventions — credentials are never logged)
- Redacting CRs or other non-log artifacts collected by must-gather
  (Secret redaction is already implemented in PR #2906)
- Real-time log redaction in production — this only applies to must-gather
  collection

## Proposal

### Redaction Approach: Consistent Pseudonymization

Each sensitive value is replaced with a deterministic pseudonym derived
from a per-collection salt and the original value. This ensures:

- The same IP address always maps to the same pseudonym within a
  collection (enabling event correlation)
- Different collections produce different pseudonyms for the same value
  (preventing cross-collection correlation)
- The mapping is not reversible without the salt (which is not included
  in the archive)

**Algorithm:**

```text
pseudonym = CATEGORY-PREFIX + truncated_hex(HMAC-SHA256(salt, original_value))
```

**Examples:**

| Original | Pseudonym |
|----------|-----------|
| `10.8.34.97` | `ip-a3f7b2c1` |
| `10.8.34.97` (same value, same collection) | `ip-a3f7b2c1` |
| `xr8620txdg21` | `host-e9d4f108` |
| `aa:bb:cc:dd:ee:ff` | `mac-7c2e9a45` |
| `dpenney` | `user-b8f31d07` |
| `CNFDG21X1234` | `serial-42a8c6e0` |

### Sensitive Field Categories

| Category | Log Key Pattern | Example Values | Pseudonym Prefix |
|----------|----------------|----------------|------------------|
| IP addresses | `clientIp`, `bmcAddress`, `host`, `ip` | `10.8.34.97`, `fd00:bmc::101` | `ip-` |
| Hostnames | `clusterName`, `hostName`, `bmh`, `managedCluster` | `cnfdg21`, `xr8620txdg21` | `host-` |
| User identities | `user`, `preferred_username` | `dpenney`, `system:admin` | `user-` |
| MAC addresses | `bootMACAddress`, `macAddress`, `mac` | `aa:bb:cc:dd:ee:ff` | `mac-` |
| Serial numbers | `serialNumber`, `serial` | `CNFDG21X1234` | `serial-` |

### Implementation

The redaction is implemented as a post-processing step in the must-gather
script (`must-gather/gather`), applied to log files after collection.

#### Option A: Shell-based (sed/awk)

Process each log file with pattern-based substitution. Uses a lookup
file to maintain consistent mappings within a collection.

**Advantages:** No additional dependencies, works with the existing
`ose-cli-rhel9` base image.

**Disadvantages:** Complex regex patterns for structured JSON logs,
harder to maintain, may miss values in unexpected positions.

#### Option B: Python script

A Python script that parses JSON log lines, identifies sensitive fields
by key name, and applies consistent pseudonymization using `hmac`.

**Advantages:** Clean JSON parsing, easy to extend with new field
patterns, handles nested structures. Python3 is available in the
`ose-cli-rhel9` base image.

**Disadvantages:** Slightly slower for very large log files.

#### Option C: Go binary

Build a dedicated log redaction tool as part of the O-Cloud Manager
binary (e.g., `oran-o2ims redact-logs`). The must-gather script calls
it as a post-processing step.

**Advantages:** Type-safe, fast, can share field name constants with
the main codebase to stay in sync with log key changes.

**Disadvantages:** Increases binary size, requires the main binary to
be available in the must-gather image (which currently only has
`ose-cli`).

#### Recommendation

**Option B (Python script)** provides the best balance of maintainability,
correctness, and no additional build dependencies. The script would be
included in the must-gather directory alongside the `gather` script.

### Configuration

Redaction is enabled by default. An environment variable can be used to
disable it when raw logs are needed for internal debugging:

```bash
# Default behavior: logs are redacted
oc adm must-gather --image=...

# Disable redaction for internal debugging (not recommended for support cases)
REDACT_LOGS=false oc adm must-gather --image=...

# Redact specific categories only
REDACT_CATEGORIES=ip,user,mac oc adm must-gather --image=...
```

When `REDACT_LOGS` is explicitly set to `false`, logs are collected
as-is without redaction.

### Integration with Must-Gather Script

```bash
# After collecting logs...
gather_pod_logs "${OCLOUD_NS}" "ocloud-manager"

# Post-process: redact sensitive fields (enabled by default)
if [ "${REDACT_LOGS}" != "false" ]; then
    log "Redacting sensitive fields from collected logs..."
    python3 "${SCRIPT_DIR}/redact-logs.py" \
        --log-dir "${BASE_DIR}/logs" \
        --categories "${REDACT_CATEGORIES:-all}" \
        --salt "$(head -c 32 /dev/urandom | base64)"
    log "Log redaction complete"
else
    log "WARNING: Log redaction is disabled. Logs may contain sensitive data."
fi
```

### Handling Non-JSON Logs

Some logs may not be structured JSON (e.g., container runtime output,
init container logs). For non-JSON lines, the redaction script applies
regex-based pattern matching for IP addresses and MAC addresses, which
have distinctive formats. Other categories (hostnames, users) are only
redacted in structured JSON logs where the field key identifies the
content type.

## Alternatives Considered

### Redact at the logging source

Instead of post-processing, redact sensitive fields before they are
written to logs. This was rejected because:

- It would degrade operational observability — operators need real IP
  addresses and hostnames in production logs for debugging
- It would require changes across all logging call sites
- The must-gather collection is the right boundary — logs are useful
  internally but need redaction before sharing externally

### Use OpenShift log forwarding with redaction

OpenShift's ClusterLogForwarder supports field-level redaction. However,
this applies to the live log pipeline, not to must-gather collection.
Must-gather collects logs directly from pod log files, bypassing any
log forwarding pipeline.

## Open Questions

1. ~~Should redaction be enabled by default?~~ Yes — redaction is
   default-on, with `REDACT_LOGS=false` to disable for internal debugging.
2. Are there additional field categories beyond IP, hostname, user, MAC,
   and serial number that customers would consider sensitive?
3. Should the redaction mapping (salt + pseudonym table) be preserved
   in a separate, access-controlled file so that support engineers with
   appropriate authorization can reverse the mapping if needed?
4. Should CRs collected by must-gather also be redacted using the same
   pseudonymization approach (currently only Secrets are redacted)?
