#!/usr/bin/env python3
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0

"""Redact sensitive fields from must-gather pod logs and collected CRs.

This script post-processes the JSON-structured pod logs and the collected
custom resources (gathered as JSON) produced by the O-Cloud Manager
must-gather `gather` script. Sensitive fields (IP addresses, hostnames,
user identities, MAC addresses, and serial numbers) are replaced with
consistent pseudonyms so that support archives can be shared across
organizational boundaries without exposing the original values, while
still allowing events to be correlated within a single collection.

Pseudonymization uses HMAC-SHA256 with a random per-collection salt:

    pseudonym = CATEGORY_PREFIX + hex(HMAC-SHA256(salt, value))[:8]

The same value always maps to the same pseudonym within one invocation
(one salt), and different invocations produce different pseudonyms. The
salt is generated in memory and is never written to any file, so the
mapping cannot be reversed from the archive. Because logs and CRs are
redacted in a single invocation, a value that appears in both (for
example a `bmcAddress`) maps to the same pseudonym in both, so it can
still be correlated across the two.

Structured JSON log lines and CR documents are redacted by key name. In
addition, distinctive IP and MAC tokens embedded in string values (for
example in `msg` or `error` text, or a `redfish://` BMC URL) and in
non-JSON lines are scrubbed by pattern, so those values do not leak
through free text. Hostnames, users, and serial numbers are only redacted
where a key identifies the content, since they have no distinctive
format.

A string value that is itself a serialized JSON document — such as the
metal3 `baremetalhost.metal3.io/status` annotation or the
`kubectl.kubernetes.io/last-applied-configuration` annotation — is parsed
and redacted by key as well, so sensitive fields nested inside embedded
JSON strings do not survive in the archive.

Only the Python standard library is used, so the script runs on the
Python 3.9 interpreter shipped in the ose-cli-rhel9 must-gather base
image without any additional build dependencies.
"""

import argparse
import base64
import hashlib
import hmac
import json
import os
import re
import secrets
import sys
import tempfile

# Sensitive keys grouped by category. Keys are matched exactly and
# case-sensitively. The list mirrors the slog attribute names used across
# the codebase (for pod logs) and the JSON field names of the collected
# CRs (for `oc get -o json` output). Case and spelling are intentionally
# inconsistent because the two sources differ: for example logs use
# `hostName` while the metal3 CRs use `hostname`, and cluster identifiers
# appear as both `clusterID` (ocloud API) and `clusterId` (clcm API).
CATEGORY_KEYS = {
    "ip": ("clientIp", "bmcAddress", "host", "ip", "address"),
    "host": (
        "clusterName", "hostName", "bmh", "managedCluster",
        "hostname", "nodeNames", "resourceName", "ingressHost",
        "clusterID", "clusterId",
    ),
    "user": ("user", "preferred_username"),
    "mac": ("bootMACAddress", "macAddress", "mac"),
    "serial": ("serialNumber", "serial", "wwn", "wwnWithExtension", "wwnVendorExtension"),
}

# Keys whose sensitive data lives in the value subtree rather than under a
# further sensitive key — for example a map of node ID to hostname, where
# the hostnames are the (dynamically-keyed) values. Every scalar in the
# subtree is pseudonymized with the category's prefix. Applied only when
# the mapped category is enabled.
SUBTREE_VALUE_KEYS = {
    "allocatedNodeHostMap": "host",
}

# Pseudonym prefix for each category.
CATEGORY_PREFIX = {
    "ip": "ip-",
    "host": "host-",
    "user": "user-",
    "mac": "mac-",
    "serial": "serial-",
}

# Canonical category order, used when "all" is requested.
ALL_CATEGORIES = ("ip", "host", "user", "mac", "serial")

# Distinctive token patterns for the non-JSON fallback path. Only IP and
# MAC addresses are matched by shape; other categories require a key to
# identify the content and are therefore left untouched in free text.
_MAC_RE = re.compile(r"(?<![0-9A-Fa-f:])(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}(?![0-9A-Fa-f:])")
_IPV4_RE = re.compile(r"(?<![0-9.])(?:\d{1,3}\.){3}\d{1,3}(?![0-9.])")
_IPV6_RE = re.compile(
    r"(?<![0-9A-Fa-f:])(?:"
    r"(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,7}:|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,6}:[0-9A-Fa-f]{1,4}|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,5}(?::[0-9A-Fa-f]{1,4}){1,2}|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,4}(?::[0-9A-Fa-f]{1,4}){1,3}|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,3}(?::[0-9A-Fa-f]{1,4}){1,4}|"
    r"(?:[0-9A-Fa-f]{1,4}:){1,2}(?::[0-9A-Fa-f]{1,4}){1,5}|"
    r"[0-9A-Fa-f]{1,4}:(?::[0-9A-Fa-f]{1,4}){1,6}|"
    r":(?::[0-9A-Fa-f]{1,4}){1,7}"
    r")(?![0-9A-Fa-f:])"
)


def pseudonym(prefix, value, salt):
    """Return a deterministic pseudonym for value under the given salt."""
    digest = hmac.new(salt, str(value).encode("utf-8"), hashlib.sha256).hexdigest()
    return prefix + digest[:8]


def parse_categories(spec):
    """Parse a categories spec into an ordered list of category names.

    Accepts "all" (the default) or a comma-separated list drawn from
    ip, host, user, mac, serial. Unknown categories are ignored with a
    warning so that a misconfiguration never aborts a must-gather run.
    """
    spec = (spec or "all").strip()
    if spec.lower() == "all":
        return list(ALL_CATEGORIES)

    selected = []
    for raw in spec.split(","):
        name = raw.strip().lower()
        if not name:
            continue
        if name not in CATEGORY_KEYS:
            print("redact_logs: warning: unknown category %r ignored" % name, file=sys.stderr)
            continue
        if name not in selected:
            selected.append(name)
    return selected


def build_key_prefix_map(categories):
    """Build a {slog key: pseudonym prefix} map for the enabled categories."""
    key_prefix = {}
    for category in categories:
        prefix = CATEGORY_PREFIX[category]
        for key in CATEGORY_KEYS[category]:
            key_prefix[key] = prefix
    return key_prefix


def _redact_matched(value, prefix, key_prefix, categories, salt):
    """Redact a value found under a sensitive key.

    Scalars are pseudonymized with the category prefix. Lists are
    pseudonymized element-wise. Dicts are recursed into so that nested
    sensitive keys are still redacted. None and empty strings are left
    untouched.
    """
    if isinstance(value, list):
        return [_redact_matched(item, prefix, key_prefix, categories, salt) for item in value]
    if isinstance(value, dict):
        # A structured value under a sensitive key is not itself a scalar to
        # pseudonymize; recurse so nested sensitive keys are still redacted.
        return redact_obj(value, key_prefix, categories, salt)
    if value is None or value == "":
        return value
    return pseudonym(prefix, value, salt)


def _redact_subtree_values(value, prefix, salt):
    """Pseudonymize every scalar in a subtree with a single prefix.

    Used for keys listed in ``SUBTREE_VALUE_KEYS`` whose sensitive data is
    carried by the values (at any depth) rather than under a further
    sensitive key. Structure is preserved; None and empty strings are left
    untouched.
    """
    if isinstance(value, dict):
        return {k: _redact_subtree_values(v, prefix, salt) for k, v in value.items()}
    if isinstance(value, list):
        return [_redact_subtree_values(item, prefix, salt) for item in value]
    if value is None or value == "":
        return value
    return pseudonym(prefix, value, salt)


def redact_obj(obj, key_prefix, categories, salt):
    """Recursively redact a parsed JSON structure.

    Values under sensitive keys are pseudonymized by category. Any string
    value (including free text under non-sensitive keys such as ``msg`` and
    ``error``) additionally has distinctive IP and MAC tokens scrubbed, so
    that sensitive values embedded in message text do not leak.
    """
    if isinstance(obj, dict):
        result = {}
        for key, value in obj.items():
            subtree_category = SUBTREE_VALUE_KEYS.get(key)
            if subtree_category is not None and subtree_category in categories:
                result[key] = _redact_subtree_values(
                    value, CATEGORY_PREFIX[subtree_category], salt
                )
                continue
            prefix = key_prefix.get(key)
            if prefix is not None:
                result[key] = _redact_matched(value, prefix, key_prefix, categories, salt)
            else:
                result[key] = redact_obj(value, key_prefix, categories, salt)
        return result
    if isinstance(obj, list):
        return [redact_obj(item, key_prefix, categories, salt) for item in obj]
    if isinstance(obj, str):
        # A string value may itself be a serialized JSON document — for
        # example the metal3 `baremetalhost.metal3.io/status` annotation (which
        # carries hostname, serialNumber and wwn) or the
        # `kubectl.kubernetes.io/last-applied-configuration` annotation (a full
        # copy of the CR spec). Those sensitive fields are keyed by name inside
        # the embedded JSON, so free-text IP/MAC scrubbing alone would leave
        # them exposed. Parse such values, redact them by key, and re-serialize;
        # fall back to plain text redaction for ordinary (non-JSON) strings.
        stripped = obj.strip()
        if stripped[:1] in ("{", "["):
            try:
                nested = json.loads(stripped)
            except ValueError:
                pass
            else:
                return json.dumps(
                    redact_obj(nested, key_prefix, categories, salt),
                    separators=(",", ":"),
                    ensure_ascii=False,
                )
        return redact_text(obj, categories, salt)
    return obj


def redact_text(text, categories, salt):
    """Redact distinctive IP and MAC tokens in free text."""
    category_set = set(categories)
    if "mac" in category_set:
        text = _MAC_RE.sub(lambda m: pseudonym("mac-", m.group(0), salt), text)
    if "ip" in category_set:
        text = _IPV6_RE.sub(lambda m: pseudonym("ip-", m.group(0), salt), text)
        text = _IPV4_RE.sub(lambda m: pseudonym("ip-", m.group(0), salt), text)
    return text


def redact_line(line, key_prefix, categories, salt):
    """Redact a single log line.

    JSON objects and arrays are walked and redacted (by key name, and by
    IP/MAC pattern within string values); anything else falls back to
    regex-based IP/MAC redaction of the raw line.
    """
    try:
        obj = json.loads(line)
    except ValueError:
        obj = None
    else:
        if isinstance(obj, (dict, list)):
            return json.dumps(
                redact_obj(obj, key_prefix, categories, salt),
                separators=(",", ":"),
                ensure_ascii=False,
            )
    return redact_text(line, categories, salt)


def _split_eol(raw):
    """Split a raw line into its body and end-of-line marker."""
    for eol in ("\r\n", "\n", "\r"):
        if raw.endswith(eol):
            return raw[: -len(eol)], eol
    return raw, ""


def process_file(path, key_prefix, categories, salt):
    """Redact a log file in place, preserving line endings.

    Lines are streamed one at a time from the input handle straight into the
    temporary output file, so a large pod log never has to be held in memory
    all at once. Both handles use ``newline=""`` so that line endings
    (``\\r\\n``, ``\\n``, ``\\r``) are recognized for splitting but never
    translated, matching the original ``_split_eol`` behavior.
    """
    directory = os.path.dirname(path) or "."
    fd, tmp_path = tempfile.mkstemp(dir=directory, prefix=".redact-", suffix=".tmp")
    try:
        with open(path, "r", encoding="utf-8", errors="replace", newline="") as src, \
                os.fdopen(fd, "w", encoding="utf-8", newline="") as dst:
            for raw in src:
                body, eol = _split_eol(raw)
                dst.write(redact_line(body, key_prefix, categories, salt) + eol)
        os.replace(tmp_path, path)
    except BaseException:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)
        raise


def process_directory(log_dir, key_prefix, categories, salt):
    """Redact every *.log file under log_dir; return the file count."""
    count = 0
    for root, _dirs, files in os.walk(log_dir):
        for name in files:
            if name.endswith(".log"):
                process_file(os.path.join(root, name), key_prefix, categories, salt)
                count += 1
    return count


def process_cr_file(path, key_prefix, categories, salt):
    """Redact a collected CR file (a single JSON document) in place.

    Unlike a pod log, a collected CR is one JSON document (produced by
    ``oc get -o json``) rather than one JSON object per line, so it is
    parsed and rewritten as a whole. Sensitive keys are pseudonymized and
    distinctive IP/MAC tokens in string values are scrubbed, exactly as for
    logs, so pseudonyms stay consistent between logs and CRs under the
    shared salt.

    An empty file (a resource type with no instances yields an empty
    collection file) is left untouched. A file that does not parse as JSON
    falls back to regex-based IP/MAC scrubbing of its raw text, mirroring
    the malformed-line fallback used for logs, so a parse error never aborts
    the run and leaves data unredacted.
    """
    with open(path, "r", encoding="utf-8", errors="replace") as src:
        content = src.read()

    if not content.strip():
        return

    try:
        obj = json.loads(content)
    except ValueError:
        redacted = redact_text(content, categories, salt)
    else:
        redacted = json.dumps(
            redact_obj(obj, key_prefix, categories, salt),
            indent=2,
            ensure_ascii=False,
        )
        if content.endswith("\n"):
            redacted += "\n"

    directory = os.path.dirname(path) or "."
    fd, tmp_path = tempfile.mkstemp(dir=directory, prefix=".redact-", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as dst:
            dst.write(redacted)
        os.replace(tmp_path, path)
    except BaseException:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)
        raise


def process_cr_directory(cr_dir, key_prefix, categories, salt):
    """Redact every *.json CR document under cr_dir; return the file count."""
    count = 0
    for root, _dirs, files in os.walk(cr_dir):
        for name in files:
            if name.endswith(".json"):
                process_cr_file(os.path.join(root, name), key_prefix, categories, salt)
                count += 1
    return count


def resolve_salt(salt_arg):
    """Return the salt bytes: decode a base64 argument or generate one."""
    if salt_arg:
        return base64.b64decode(salt_arg)
    return secrets.token_bytes(32)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Redact sensitive fields from collected must-gather pod logs."
    )
    parser.add_argument(
        "--log-dir",
        default=None,
        help="Directory tree to scan for *.log files to redact in place.",
    )
    parser.add_argument(
        "--cr-dir",
        action="append",
        default=[],
        metavar="DIR",
        dest="cr_dirs",
        help="Directory tree of collected CRs to scan for *.json documents to "
        "redact in place. May be given multiple times.",
    )
    parser.add_argument(
        "--categories",
        default="all",
        help="Comma-separated categories to redact (ip,host,user,mac,serial) or 'all'.",
    )
    parser.add_argument(
        "--salt",
        default=None,
        help="Base64-encoded salt for deterministic output (testing); a random "
        "salt is generated per invocation when omitted.",
    )
    args = parser.parse_args(argv)

    if not args.log_dir and not args.cr_dirs:
        parser.error("at least one of --log-dir or --cr-dir is required")

    categories = parse_categories(args.categories)
    if not categories:
        # Fail closed: a non-empty but fully invalid categories value (e.g.
        # "bogus") must not be treated as "nothing to redact", or the caller
        # would ship unredacted logs. Return non-zero so the gather script's
        # cleanup path removes the collected logs.
        print(
            "redact_logs: error: no valid categories selected in %r" % args.categories,
            file=sys.stderr,
        )
        return 1

    key_prefix = build_key_prefix_map(categories)
    salt = resolve_salt(args.salt)

    if args.log_dir:
        log_count = process_directory(args.log_dir, key_prefix, categories, salt)
        print("redact_logs: redacted %d log file(s) in %s" % (log_count, args.log_dir))

    for cr_dir in args.cr_dirs:
        cr_count = process_cr_directory(cr_dir, key_prefix, categories, salt)
        print("redact_logs: redacted %d CR file(s) in %s" % (cr_count, cr_dir))

    return 0


if __name__ == "__main__":
    sys.exit(main())
