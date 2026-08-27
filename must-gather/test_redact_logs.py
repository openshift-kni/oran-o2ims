#!/usr/bin/env python3
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests for the must-gather log redaction script."""

import base64
import json
import os
import tempfile
import unittest

import redact_logs

# Two fixed salts so tests are deterministic and can compare pseudonyms
# both within and across "collections".
SALT_A = b"\x00" * 32
SALT_B = b"\x01" * 32


def redact_json_line(obj, salt=SALT_A, categories=redact_logs.ALL_CATEGORIES):
    """Helper: redact a dict as a JSON line and return the parsed result."""
    key_prefix = redact_logs.build_key_prefix_map(categories)
    line = json.dumps(obj)
    return json.loads(redact_logs.redact_line(line, key_prefix, categories, salt))


class PseudonymTest(unittest.TestCase):
    def test_pseudonym_consistency(self):
        # Same value + same salt yields an identical pseudonym every time.
        first = redact_logs.pseudonym("ip-", "10.8.34.97", SALT_A)
        second = redact_logs.pseudonym("ip-", "10.8.34.97", SALT_A)
        self.assertEqual(first, second)
        self.assertTrue(first.startswith("ip-"))
        # 8 hex chars of digest after the prefix.
        self.assertEqual(len(first), len("ip-") + 8)

    def test_pseudonym_differs_across_salts(self):
        # AC #3: different collections (salts) produce different pseudonyms.
        with_a = redact_logs.pseudonym("ip-", "10.8.34.97", SALT_A)
        with_b = redact_logs.pseudonym("ip-", "10.8.34.97", SALT_B)
        self.assertNotEqual(with_a, with_b)


class CategoryRedactionTest(unittest.TestCase):
    def test_each_category_redacted(self):
        # AC #1: every configured key in every category is redacted with
        # the correct prefix.
        for category, keys in redact_logs.CATEGORY_KEYS.items():
            prefix = redact_logs.CATEGORY_PREFIX[category]
            for key in keys:
                result = redact_json_line({key: "sensitive-value"})
                self.assertTrue(
                    result[key].startswith(prefix),
                    "key %r expected prefix %r, got %r" % (key, prefix, result[key]),
                )
                self.assertNotEqual(result[key], "sensitive-value")

    def test_non_sensitive_fields_preserved(self):
        # AC #5: structural / observability fields are untouched.
        obj = {
            "time": "2026-08-27T10:00:00Z",
            "level": "INFO",
            "msg": "reconciled provisioning request",
            "controller": "ProvisioningRequest",
            "clientIp": "10.8.34.97",
        }
        result = redact_json_line(obj)
        self.assertEqual(result["time"], obj["time"])
        self.assertEqual(result["level"], obj["level"])
        self.assertEqual(result["msg"], obj["msg"])
        self.assertEqual(result["controller"], obj["controller"])
        self.assertTrue(result["clientIp"].startswith("ip-"))

    def test_correlation_within_collection(self):
        # AC #2: the same value maps to the same pseudonym across keys and
        # lines within a single collection (salt).
        line_one = redact_json_line({"clientIp": "10.8.34.97"})
        line_two = redact_json_line({"bmcAddress": "10.8.34.97"})
        self.assertEqual(line_one["clientIp"], line_two["bmcAddress"])

    def test_category_selection(self):
        # Only the selected categories are redacted; others are preserved.
        obj = {"ip": "10.8.34.97", "mac": "aa:bb:cc:dd:ee:ff", "serial": "CNFDG21X1234"}
        result = redact_json_line(obj, categories=["ip", "mac"])
        self.assertTrue(result["ip"].startswith("ip-"))
        self.assertTrue(result["mac"].startswith("mac-"))
        self.assertEqual(result["serial"], "CNFDG21X1234")


class StructureTest(unittest.TestCase):
    def test_nested_structures(self):
        # Sensitive keys nested inside dicts and lists are redacted.
        obj = {
            "node": {"bmcAddress": "10.8.34.97", "region": "east"},
            "peers": [{"host": "10.0.0.1"}, {"host": "10.0.0.2"}],
        }
        result = redact_json_line(obj)
        self.assertTrue(result["node"]["bmcAddress"].startswith("ip-"))
        self.assertEqual(result["node"]["region"], "east")
        self.assertTrue(result["peers"][0]["host"].startswith("ip-"))
        self.assertTrue(result["peers"][1]["host"].startswith("ip-"))
        self.assertNotEqual(result["peers"][0]["host"], result["peers"][1]["host"])

    def test_list_valued_fields(self):
        # A list under a sensitive key is pseudonymized element-wise.
        result = redact_json_line({"ip": ["10.0.0.1", "10.0.0.2"]})
        self.assertTrue(all(v.startswith("ip-") for v in result["ip"]))
        self.assertEqual(len(result["ip"]), 2)

    def test_dict_under_sensitive_key_recurses(self):
        # A dict value under a sensitive key must be recursed into so that
        # nested sensitive keys are still redacted (rather than leaked).
        obj = {"host": {"clientIp": "10.0.0.5", "region": "east"}}
        result = redact_json_line(obj)
        self.assertTrue(result["host"]["clientIp"].startswith("ip-"))
        self.assertNotIn("10.0.0.5", json.dumps(result))
        self.assertEqual(result["host"]["region"], "east")

    def test_non_string_values(self):
        # A non-string scalar (e.g. integer serial) is handled without error.
        result = redact_json_line({"serial": 1234567890})
        self.assertTrue(result["serial"].startswith("serial-"))

    def test_null_and_empty_values(self):
        # None and empty strings under sensitive keys are left untouched.
        result = redact_json_line({"host": None, "ip": "", "mac": "aa:bb:cc:dd:ee:ff"})
        self.assertIsNone(result["host"])
        self.assertEqual(result["ip"], "")
        self.assertTrue(result["mac"].startswith("mac-"))


class FreeTextInJsonTest(unittest.TestCase):
    def test_ip_and_mac_in_string_values_redacted(self):
        # IP and MAC tokens embedded in free-text values of a structured JSON
        # line (e.g. msg, error) must not leak, even though the key is not
        # itself a sensitive key.
        obj = {
            "level": "ERROR",
            "msg": "failed to connect to bmc 10.8.34.97 mac aa:bb:cc:dd:ee:ff",
            "error": "dial tcp 10.8.34.97: timeout",
        }
        result = redact_json_line(obj)
        dumped = json.dumps(result)
        self.assertNotIn("10.8.34.97", dumped)
        self.assertNotIn("aa:bb:cc:dd:ee:ff", dumped)
        self.assertIn("ip-", result["msg"])
        self.assertIn("mac-", result["msg"])
        self.assertIn("ip-", result["error"])

    def test_correlation_between_key_and_free_text(self):
        # The same IP redacted via a sensitive key and via free-text scanning
        # must map to the same pseudonym within a collection.
        result = redact_json_line(
            {"clientIp": "10.8.34.97", "msg": "connecting to 10.8.34.97"}
        )
        keyed = result["clientIp"]
        self.assertIn(keyed, result["msg"])

    def test_hostname_in_free_text_not_redacted(self):
        # Hostnames have no distinctive format, so they are only redacted
        # where a key identifies them, not in free text.
        result = redact_json_line({"msg": "reconciling cluster mycluster.example.com"})
        self.assertIn("mycluster.example.com", result["msg"])


class NonJsonTest(unittest.TestCase):
    def test_non_json_line_ip_mac_regex(self):
        # AC coverage for unstructured logs: IPv4, IPv6 and MAC tokens are
        # redacted by shape; hostnames and users are left as-is.
        key_prefix = redact_logs.build_key_prefix_map(redact_logs.ALL_CATEGORIES)
        line = "conn from 10.8.34.97 and 2001:db8::1 mac aa:bb:cc:dd:ee:ff user dpenney host myhost.example.com"
        out = redact_logs.redact_line(line, key_prefix, redact_logs.ALL_CATEGORIES, SALT_A)
        self.assertNotIn("10.8.34.97", out)
        self.assertNotIn("2001:db8::1", out)
        self.assertNotIn("aa:bb:cc:dd:ee:ff", out)
        self.assertIn("ip-", out)
        self.assertIn("mac-", out)
        # Free-text hostnames and usernames are not redacted without a key.
        self.assertIn("dpenney", out)
        self.assertIn("myhost.example.com", out)

    def test_malformed_json_falls_back(self):
        # A broken JSON line must not raise and should use the regex path.
        key_prefix = redact_logs.build_key_prefix_map(redact_logs.ALL_CATEGORIES)
        line = '{"clientIp": "10.8.34.97", broken'
        out = redact_logs.redact_line(line, key_prefix, redact_logs.ALL_CATEGORIES, SALT_A)
        self.assertNotIn("10.8.34.97", out)
        self.assertIn("ip-", out)


class CategoryParsingTest(unittest.TestCase):
    def test_all_expands_to_every_category(self):
        self.assertEqual(redact_logs.parse_categories("all"), list(redact_logs.ALL_CATEGORIES))

    def test_subset_and_whitespace(self):
        self.assertEqual(redact_logs.parse_categories(" ip , mac "), ["ip", "mac"])

    def test_unknown_category_ignored(self):
        self.assertEqual(redact_logs.parse_categories("ip,bogus"), ["ip"])


class FileProcessingTest(unittest.TestCase):
    def test_process_directory_rewrites_logs_and_omits_salt(self):
        salt = SALT_A
        key_prefix = redact_logs.build_key_prefix_map(redact_logs.ALL_CATEGORIES)
        with tempfile.TemporaryDirectory() as tmp:
            nested = os.path.join(tmp, "ocloud-manager")
            os.makedirs(nested)
            log_path = os.path.join(nested, "controller.log")
            with open(log_path, "w", encoding="utf-8") as handle:
                handle.write(json.dumps({"clientIp": "10.8.34.97", "msg": "hello"}) + "\n")
                handle.write("plain line 10.8.34.97\n")

            count = redact_logs.process_directory(
                tmp, key_prefix, redact_logs.ALL_CATEGORIES, salt
            )
            self.assertEqual(count, 1)

            with open(log_path, "r", encoding="utf-8") as handle:
                contents = handle.read()
            self.assertNotIn("10.8.34.97", contents)
            self.assertIn("ip-", contents)
            self.assertIn('"msg":"hello"', contents)
            # Trailing newline preserved.
            self.assertTrue(contents.endswith("\n"))
            # The raw salt must never be written into the archive.
            self.assertNotIn(base64.b64encode(salt).decode("ascii"), contents)

    def test_resolve_salt_decodes_base64_and_generates_random(self):
        raw = b"\x02" * 32
        encoded = base64.b64encode(raw).decode("ascii")
        self.assertEqual(redact_logs.resolve_salt(encoded), raw)
        generated = redact_logs.resolve_salt(None)
        self.assertEqual(len(generated), 32)

    def test_process_file_preserves_crlf_line_endings(self):
        salt = SALT_A
        key_prefix = redact_logs.build_key_prefix_map(redact_logs.ALL_CATEGORIES)
        with tempfile.TemporaryDirectory() as tmp:
            log_path = os.path.join(tmp, "controller.log")
            with open(log_path, "wb") as handle:
                handle.write(json.dumps({"clientIp": "10.8.34.97"}).encode("utf-8") + b"\r\n")
                handle.write(b"plain line 10.8.34.97\r\n")
                handle.write(b"no trailing newline 10.8.34.97")

            redact_logs.process_file(log_path, key_prefix, redact_logs.ALL_CATEGORIES, salt)

            with open(log_path, "rb") as handle:
                raw = handle.read()
            self.assertNotIn(b"10.8.34.97", raw)
            self.assertIn(b"ip-", raw)
            # CRLF terminators preserved untranslated; final line unterminated.
            self.assertEqual(raw.count(b"\r\n"), 2)
            self.assertFalse(raw.endswith(b"\r\n"))
            self.assertFalse(raw.endswith(b"\n"))


class MainTest(unittest.TestCase):
    def test_main_fails_closed_on_fully_invalid_categories(self):
        with tempfile.TemporaryDirectory() as tmp:
            rc = redact_logs.main(["--log-dir", tmp, "--categories", "bogus"])
        self.assertNotEqual(rc, 0)

    def test_main_succeeds_with_at_least_one_valid_category(self):
        with tempfile.TemporaryDirectory() as tmp:
            rc = redact_logs.main(["--log-dir", tmp, "--categories", "ip,bogus", "--salt",
                                   base64.b64encode(SALT_A).decode("ascii")])
        self.assertEqual(rc, 0)


if __name__ == "__main__":
    unittest.main()
