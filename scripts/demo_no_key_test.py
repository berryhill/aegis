import os
from pathlib import Path
import subprocess
import tempfile
import unittest


REPOSITORY = Path(__file__).resolve().parents[1]


class NoKeyDemoTest(unittest.TestCase):
    def test_checkout_profile_and_honest_provider_boundary(self):
        with tempfile.TemporaryDirectory(prefix="aegis-demo-hermes-") as fixture:
            fixture_root = Path(fixture)
            installation = fixture_root / "installation"
            (installation / "venv" / "bin").mkdir(parents=True)
            hermes = fixture_root / "hermes"
            hermes.write_text(
                "#!/bin/sh\n"
                "printf '%s\\n' 'Hermes Agent v0.18.2 (2026.7.7.2)'\n"
                f"printf '%s\\n' 'Project: {installation}'\n"
                "printf '%s\\n' 'Python: 3.11.15'\n"
                "printf '%s\\n' 'OpenAI SDK: 2.24.0'\n"
            )
            hermes.chmod(0o700)

            environment = os.environ.copy()
            environment["PATH"] = f"{fixture_root}:{environment['PATH']}"
            result = subprocess.run(
                ["sh", "scripts/demo-no-key.sh"],
                cwd=REPOSITORY,
                env=environment,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                timeout=180,
                check=False,
            )
            hermes.write_text(
                "#!/bin/sh\n"
                "printf '%s\\n' 'Hermes Agent v0.14.0'\n"
                f"printf '%s\\n' 'Install directory: {installation}'\n"
            )
            unsupported = subprocess.run(
                ["sh", "scripts/demo-no-key.sh"],
                cwd=REPOSITORY,
                env=environment,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                timeout=180,
                check=False,
            )

        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("== Explicit Hermes discovery ==", result.stdout)
        self.assertIn('"runtime": "hermes-agent"', result.stdout)
        self.assertIn("== Checkout execution profile ==\naegis version dev", result.stdout)
        self.assertIn("Strict validation passed.", result.stdout)
        self.assertIn("== Redacted effective configuration ==", result.stdout)
        self.assertNotIn("REPLACE_WITH_ABSOLUTE_TRANSPORT_DIR", result.stdout)
        self.assertIn('"token_file":', result.stdout)
        self.assertIn('"token": "[REDACTED]"', result.stdout)
        self.assertIn("no model success is claimed", result.stdout)
        self.assertNotEqual(unsupported.returncode, 0, unsupported.stdout)
        self.assertIn("unsupported Hermes version 0.14.0", unsupported.stdout)
        self.assertIn("Hermes discovery failed", unsupported.stdout)
        self.assertNotIn("Strict validation passed.", unsupported.stdout)
        self.assertEqual(list(REPOSITORY.glob(".aegis-no-key-*")), [])
        self.assertEqual(list(REPOSITORY.glob("aegis-no-key-demo-*")), [])
        ignored = subprocess.run(
            [
                "git",
                "check-ignore",
                "--no-index",
                ".aegis-no-key-fixture/probe",
                "aegis-no-key-demo-fixture",
            ],
            cwd=REPOSITORY,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(ignored.returncode, 0, ignored.stderr)
        self.assertEqual(
            ignored.stdout.splitlines(),
            [".aegis-no-key-fixture/probe", "aegis-no-key-demo-fixture"],
        )


if __name__ == "__main__":
    unittest.main()
