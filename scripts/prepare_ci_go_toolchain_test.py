import importlib.util
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("prepare-ci-go-toolchain.py")
spec = importlib.util.spec_from_file_location("prepare", SCRIPT)
assert spec is not None and spec.loader is not None
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PrepareToolchainTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.cache = Path(self.tmp.name).resolve() / "cache"
        self.root = self.cache / "go/1.26.10/x64"
        for d in ("bin", "pkg/tool/linux_amd64", "src/runtime", "lib"):
            (self.root / d).mkdir(parents=True, exist_ok=True)
        for f, mode in (("bin/go", 0o775), ("pkg/tool/linux_amd64/compile", 0o777), ("src/runtime/runtime.go", 0o664), ("VERSION", 0o644), ("lib/data", 0o660)):
            p = self.root / f
            p.write_text("isolated fake distribution\n")
            p.chmod(mode)
        self.root.chmod(0o777)

    def prepare(self, root=None):
        module.prepare(root or self.root, self.cache, "1.26.10", "X64")

    def test_complete_distribution_only_preserves_bits_and_bytes(self):
        paths = [self.root, *self.root.rglob("*")]
        before = {p: (stat.S_IMODE(p.stat().st_mode), p.read_bytes() if p.is_file() else None) for p in paths}
        ancestor = self.cache.stat().st_mode
        self.prepare()
        self.prepare()  # idempotent
        for p, (mode, content) in before.items():
            self.assertEqual(stat.S_IMODE(p.stat().st_mode), mode & ~0o022)
            if content is not None:
                self.assertEqual(p.read_bytes(), content)
        self.assertEqual(self.cache.stat().st_mode, ancestor)

    def test_rejects_symlink_before_mutation(self):
        external = self.cache / "outside"
        external.write_text("untouched")
        external.chmod(0o666)
        (self.root / "src/link").symlink_to(external)
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertEqual(stat.S_IMODE(external.stat().st_mode), 0o666)
        self.assertEqual(stat.S_IMODE(self.root.stat().st_mode), 0o777)

    def test_rejects_wrong_root_and_root_symlink(self):
        with self.assertRaises(ValueError):
            self.prepare(self.cache)
        moved = self.root.with_name("moved")
        self.root.rename(moved)
        self.root.symlink_to(moved, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.prepare()

    def test_cli_denies_workstation(self):
        result = subprocess.run(["python3", str(SCRIPT), "--goroot", str(self.root), "--version", "1.26.10"], env={**os.environ, "GITHUB_ACTIONS": "false"}, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(stat.S_IMODE(self.root.stat().st_mode), 0o777)

    def test_workflow_retains_mandatory_normal_race_and_preparation(self):
        workflow = (SCRIPT.parent.parent / ".github/workflows/ci.yml").read_text()
        self.assertIn("mode: [normal, race]", workflow)
        self.assertIn('test "$SHARDS_RESULT" = success', workflow)
        self.assertEqual(workflow.count("python3 scripts/prepare-ci-go-toolchain.py"), workflow.count("uses: actions/setup-go@v6"))
        self.assertNotIn("chmod go-w", workflow)
        self.assertNotIn("continue-on-error", workflow)


if __name__ == "__main__":
    unittest.main()
