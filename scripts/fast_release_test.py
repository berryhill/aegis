"""Canonical release control-flow tests; all Git remotes and API results are fixtures."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("ci_evidence", ROOT / "scripts/verify-ci-evidence.py")
assert SPEC is not None and SPEC.loader is not None
EVIDENCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVIDENCE)


class FastReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=Path.cwd())
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        (self.repo / "scripts").mkdir()
        self.git = shutil.which("git")
        assert self.git
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_CONFIG_")}
        self.env.update(GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null",
                        GIT_AUTHOR_NAME="Release Fixture", GIT_AUTHOR_EMAIL="release@example.invalid",
                        GIT_COMMITTER_NAME="Release Fixture", GIT_COMMITTER_EMAIL="release@example.invalid")
        for name in ("release.sh", "prepare-release-changelog.py", "verify-ci-evidence.py"):
            shutil.copyfile(ROOT / "scripts" / name, self.repo / "scripts" / name)
        (self.repo / "CHANGELOG.md").write_text("# Changelog\n\n## Unreleased\n\n### Fixed\n- Fixture change.\n")
        self.run_git("init", "-q", "-b", "main")
        self.run_git("add", ".")
        self.run_git("commit", "-qm", "initial")
        self.sha = self.run_git("rev-parse", "HEAD").strip()
        origin = self.root / "origin.git"
        subprocess.run([self.git, "init", "-q", "--bare", str(origin)], env=self.env, check=True)
        self.run_git("remote", "add", "origin", str(origin))
        self.run_git("push", "-q", "origin", "main")
        binaries = self.root / "bin"
        binaries.mkdir()
        (binaries / "git").write_text('''#!/usr/bin/env python3
import os,sys
if sys.argv[1:] == ['remote','get-url','origin']:
    print(os.environ['TEST_ORIGIN_URL'])
else:
    os.execv(os.environ['TEST_REAL_GIT'], [os.environ['TEST_REAL_GIT'], *sys.argv[1:]])
''')
        (binaries / "make").write_text("#!/bin/sh\nprintf 'unexpected full local verification' >&2\nexit 88\n")
        (binaries / "gh").write_text('''#!/usr/bin/env python3
import json,os,sys
path = next(arg for arg in sys.argv if arg.startswith('repos/'))
sha = os.environ['TEST_CI_SHA']
run = {'id':123,'run_attempt':2,'workflow_id':456,'head_sha':sha,'head_branch':'main',
       'event':'push','path':'.github/workflows/ci.yml','status':'completed',
       'conclusion':os.environ.get('TEST_CI_CONCLUSION','success'),
       'repository':{'full_name':'berryhill/aegis'},'head_repository':{'full_name':'berryhill/aegis'}}
if '/jobs?' in path:
    jobs = [{'id':i+1,'run_id':123,'head_sha':sha,'name':name,'status':'completed','conclusion':'success'}
            for i,name in enumerate(json.loads(os.environ['TEST_CI_JOBS']))]
    result = [{'jobs':jobs}]
elif '/ci.yml/runs?' in path:
    result = [{'workflow_runs':[run]}]
elif path.endswith('/ci.yml'):
    result = {'id':456,'path':'.github/workflows/ci.yml','state':'active'}
elif path.endswith('/runs/123'):
    result = run
else:
    raise SystemExit('unexpected fixture API path')
print(json.dumps(result))
''')
        for path in binaries.iterdir():
            path.chmod(0o700)
        self.env.update(PATH=str(binaries) + os.pathsep + self.env["PATH"],
                        TEST_REAL_GIT=self.git, TEST_CI_SHA=self.sha,
                        TEST_CI_JOBS=json.dumps(sorted(EVIDENCE.REQUIRED_JOBS)),
                        TEST_ORIGIN_URL="https://github.com/berryhill/aegis.git",
                        RELEASE_DRY_RUN="1", RELEASE_SKIP_GITHUB_STATUS="1")

    def run_git(self, *args):
        assert self.git is not None
        return subprocess.check_output([self.git, *args], cwd=self.repo, env=self.env, text=True, stderr=subprocess.STDOUT)

    def snapshot(self):
        return tuple(self.run_git(*args) for args in [("rev-parse", "HEAD"), ("status", "--porcelain=v1"),
                                                      ("show-ref",), ("ls-remote", "origin")])

    def release(self):
        return subprocess.run(["sh", "scripts/release.sh", "1.2.3"], cwd=self.repo, env=self.env,
                              capture_output=True, text=True, timeout=30)

    def test_verified_canonical_source_avoids_duplicate_suite_without_ref_mutation(self):
        before = self.snapshot()
        result = self.release()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("exact source CI reused", result.stdout)
        self.assertEqual(before, self.snapshot())

    def test_failed_evidence_denies_before_refs_and_ignores_dirty_working_helper(self):
        self.env["TEST_CI_CONCLUSION"] = "failure"
        (self.repo / "scripts/verify-ci-evidence.py").write_text("print('fake success')\n")
        before = self.snapshot()
        result = self.release()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exact source CI is not verified", result.stderr)
        self.assertNotIn("fake success", result.stdout)
        self.assertEqual(before, self.snapshot())

    def test_noncanonical_github_origin_denies(self):
        self.env["TEST_ORIGIN_URL"] = "https://github.com/other/aegis.git"
        before = self.snapshot()
        result = self.release()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not the canonical", result.stderr)
        self.assertEqual(before, self.snapshot())

    def test_recovery_reuses_exact_parent_and_preserves_existing_tag(self):
        source = self.repo / "CHANGELOG.md"
        output = self.root / "prepared"
        date = self.run_git("show", "-s", "--format=%cs", "HEAD").strip()
        subprocess.run(["python3", "scripts/prepare-release-changelog.py", str(source), str(output), "1.2.3", date],
                       cwd=self.repo, env=self.env, check=True)
        source.write_bytes(output.read_bytes())
        self.run_git("add", "CHANGELOG.md")
        self.run_git("commit", "-qm", "Prepare v1.2.3 release")
        self.run_git("tag", "-a", "v1.2.3", "-m", "Aegis v1.2.3\n\n-----BEGIN PGP SIGNATURE-----\nfixture\n-----END PGP SIGNATURE-----")
        # Signature verification is a fixture only; no real signer is accessed.
        wrapper = self.root / "bin/git"
        text = wrapper.read_text().replace("if sys.argv[1:] ==", "if sys.argv[1:2] == ['verify-tag']:\n    raise SystemExit(0)\nelif sys.argv[1:] ==", 1)
        wrapper.write_text(text)
        before = self.snapshot()
        result = self.release()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("exact source CI reused", result.stdout)
        self.assertEqual(before, self.snapshot())


if __name__ == "__main__":
    unittest.main()
