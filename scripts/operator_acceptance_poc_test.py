import importlib.util
import json
import os
import stat
import sys
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("operator_acceptance_poc.py")
SPEC = importlib.util.spec_from_file_location("operator_acceptance_poc", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load {MODULE_PATH}")
POC = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = POC
SPEC.loader.exec_module(POC)


FIXTURE = r'''#!/usr/bin/env python3
import os
import sys
import termios

if sys.argv[-2:] == ["audit", "list"]:
    print('[{"type":"credential_created","outcome":"ok"},{"type":"manager_session_closed","outcome":"ok","metadata":{"cleanup":"complete"}}]')
    raise SystemExit(0)
if sys.argv[-2:] == ["audit", "verify"]:
    print('{"valid":true,"events":4}')
    raise SystemExit(0)
if sys.argv[-1:] != ["manager"]:
    raise SystemExit(2)

def prompt():
    print("\n[composer] > ", end="", flush=True)

def line():
    return sys.stdin.buffer.readline().rstrip(b"\r\n")

print("[AEGIS] Authenticated as principal. Preparing exact-local manager.")
prompt()
line()
print("[origin: Hermes model / untrusted] I can help administer protected credentials.")
print("[origin: AEGIS / authoritative] guarded turn complete")
prompt()
line()
print("[origin: AEGIS / authoritative] natural create request mapped locally")
fd = sys.stdin.fileno()
original = termios.tcgetattr(fd)
protected = termios.tcgetattr(fd)
protected[3] &= ~(termios.ECHO | termios.ECHONL)
termios.tcsetattr(fd, termios.TCSANOW, protected)
print("Secret value: ", end="", flush=True)
first = line()
print("Confirm secret value: ", end="", flush=True)
second = line()
termios.tcsetattr(fd, termios.TCSANOW, original)
if os.environ.get("AEGIS_POC_FIXTURE_LEAK") == "1":
    print(first.decode())
if first != second:
    raise SystemExit(3)
print("[origin: AEGIS / authoritative] protected intake completed; content not retained")
print("[origin: AEGIS / authoritative] Credential created\n  reference  test\n  status     active")
prompt()
line()
print("[origin: AEGIS / authoritative] Credential inventory\n  total    1\n  active   1\n  revoked  0")
prompt()
line()
print('[origin: AEGIS / authoritative] Credentials matching "test" (1)\n\n  1. test\n     active | opaque | version 1')
prompt()
line()
print("Shutting down Aegis manager (user_exit).")
print("Aegis manager stopped; cleanup complete.")
'''


class OperatorAcceptancePOCTest(unittest.TestCase):
    def fixture(self, root: Path) -> Path:
        path = root / "aegis-fixture"
        path.write_text(FIXTURE)
        path.chmod(0o700)
        return path

    def test_complete_journey_retains_safe_structured_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture = self.fixture(root)
            evidence_dir = root / "evidence"
            args = POC.parse_args(
                ["--aegis", str(fixture), "--evidence", str(evidence_dir), "--turn-timeout", "5"]
            )
            path = POC.run(args)
            data = path.read_bytes()
            self.assertNotIn(b"aegis-poc-canary-", data)
            records = [json.loads(line) for line in data.splitlines()]
            self.assertEqual(records[0]["kind"], "run")
            self.assertEqual(records[-1]["kind"], "summary")
            self.assertEqual(records[-1]["outcome"], "passed")
            self.assertEqual(
                [record.get("journey") for record in records if record["kind"] == "turn"],
                [
                    "ordinary-conversation",
                    "create-credential-test",
                    "credential-count",
                    "natural-reference",
                    "exit",
                    "audit-verification",
                ],
            )
            create = next(record for record in records if record.get("journey") == "create-credential-test")
            self.assertEqual(
                create["submitted_non_secret_input"],
                "Please create a credential named test. [protected input omitted]",
            )
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(evidence_dir.stat().st_mode), 0o700)

    def test_pty_canary_leak_fails_without_retaining_plaintext(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture = self.fixture(root)
            evidence_dir = root / "evidence"
            previous = os.environ.get("AEGIS_POC_FIXTURE_LEAK")
            os.environ["AEGIS_POC_FIXTURE_LEAK"] = "1"
            try:
                args = POC.parse_args(
                    ["--aegis", str(fixture), "--evidence", str(evidence_dir), "--turn-timeout", "5"]
                )
                with self.assertRaisesRegex(POC.AcceptanceFailure, "canary appeared"):
                    POC.run(args)
            finally:
                if previous is None:
                    os.environ.pop("AEGIS_POC_FIXTURE_LEAK", None)
                else:
                    os.environ["AEGIS_POC_FIXTURE_LEAK"] = previous
            data = (evidence_dir / "journey.jsonl").read_bytes()
            self.assertNotIn(b"aegis-poc-canary-", data)
            records = [json.loads(line) for line in data.splitlines()]
            self.assertEqual(records[-1]["outcome"], "failed")


if __name__ == "__main__":
    unittest.main()
