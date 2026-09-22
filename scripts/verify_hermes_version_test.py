#!/usr/bin/env python3
"""Synthetic release identity tests, not live-Hermes qualification."""
import importlib.util
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("verify-hermes-version.py")
SPEC = importlib.util.spec_from_file_location("verify_hermes_version", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class HermesMinimumTests(unittest.TestCase):
    def test_stable_minimum_without_upper_cap(self):
        for version in ("0.18.0", "0.18.2", "0.19.0", "0.99.0", "1.0.0", "2026.9.22", "999999999999999999999.0.0"):
            with self.subTest(version=version):
                self.assertEqual(MODULE.accepted_version(f"Hermes Agent v{version}\n".encode()), version)

    def test_decorated_identity(self):
        self.assertEqual(MODULE.accepted_version(
            "Hermes Agent v0.21.3 (2026.9.14) · upstream e589b739 · local 56631482 (+19448 carried commits)\n".encode()), "0.21.3")
        self.assertEqual(MODULE.accepted_version(
            b"Hermes Agent v0.19.0 (2026.9.22.1)\nInstall directory: /fixture/hermes\n"), "0.19.0")
        self.assertEqual(MODULE.accepted_version(
            "Hermes Agent v1.0.0 (2026.9.22.1) · upstream abcdef01 · local abcdef02 (+2 carried commits)\n".encode()), "1.0.0")

    def test_rejects_old_malformed_ambiguous_and_oversized(self):
        values = [b"", b"unrelated 0.18.0", b"hermes 0.18.0", b"x" * 4097,
                  b"Hermes Agent v0.18.0\0", b"\xff",
                  b"Hermes Agent v0.18.0\nHermes Agent v0.19.0\n",
                  b"Hermes Agent v0.18.0\nInstall directory: relative\n",
                  b"Hermes Agent v0.18.0\nInstall directory: /a\nInstall directory: /b\n"]
        values += [f"Hermes Agent v{v}\n".encode() for v in (
            "0.0.0", "0.9.999", "0.17.99", "00.18.0", "0.018.0", "0.18.00",
            "0.18", "0.18.0-rc.1", "0.19.0+metadata", "1.0.0 trailing")]
        for value in values:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    MODULE.accepted_version(value)

    def test_cli_exit_status(self):
        with tempfile.TemporaryDirectory(prefix=".hermes-version-test-", dir=SCRIPT.parent.parent) as directory:
            output = pathlib.Path(directory) / "version.out"
            for version, accepted in (("0.17.9", False), ("0.19.0", True), ("1.0.0", True)):
                output.write_text(f"Hermes Agent v{version}\n")
                result = subprocess.run([sys.executable, str(SCRIPT), str(output)], capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, accepted)


if __name__ == "__main__":
    unittest.main()
