"""Credential-independent guards for the installed Credentials proof harness."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('credentials_proof', REPO / 'scripts/verify-installed-credentials.py')
assert spec is not None and spec.loader is not None
proof = importlib.util.module_from_spec(spec)
spec.loader.exec_module(proof)


class InstalledCredentialsGuards(unittest.TestCase):
    def test_existing_workspace_is_denied_without_writes(self):
        with patch.object(sys, 'argv', ['proof', '/usr/bin/true', str(REPO), '/nonexistent-design']):
            with self.assertRaisesRegex(proof.ProofError, 'fresh repository-local'):
                proof.main()

    def test_wrong_design_is_denied_before_workspace_creation(self):
        scratch = REPO / '.scratch'
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='credential-guard-', dir=scratch) as directory:
            root = Path(directory)
            artifact = root / 'artifact.html'
            artifact.write_text('<html>not the accepted design</html>')
            workspace = root / 'proof'
            with patch.object(sys, 'argv', ['proof', '/usr/bin/true', str(workspace), str(artifact)]):
                with self.assertRaisesRegex(proof.ProofError, 'design identity mismatch'):
                    proof.main()
            self.assertFalse(workspace.exists())

    def test_private_json_is_exclusive_and_owner_only(self):
        scratch = REPO / '.scratch'
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='credential-guard-', dir=scratch) as directory:
            path = Path(directory) / 'metadata.json'
            proof.private_json(path, {'metadata_only': True})
            self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                proof.private_json(path, {})
            self.assertEqual(json.loads(path.read_text()), {'metadata_only': True})


if __name__ == '__main__':
    unittest.main()
