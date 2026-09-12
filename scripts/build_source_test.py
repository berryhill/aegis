#!/usr/bin/env python3
"""Real-Go provenance regression: nested linked worktree under another repository."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[1]


class SourceBuildTest(unittest.TestCase):
    def test_nested_registered_worktree(self):
        with tempfile.TemporaryDirectory(prefix=".aegis-build-test-", dir=REPO) as temp:
            root = Path(temp)
            env = os.environ.copy()
            for key in ("GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"):
                env.pop(key, None)
            env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1", GOWORK="off")

            def run(*args, cwd=root, check=True, extra=None):
                return subprocess.run(args, cwd=cwd, env=env | (extra or {}),
                                      check=check, text=True, capture_output=True)

            def commit(path):
                run("git", "add", ".", cwd=path)
                run("git", "-c", "user.name=Aegis Fixture", "-c",
                    "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false",
                    "commit", "-qm", "fixture", cwd=path)
                return run("git", "rev-parse", "HEAD", cwd=path).stdout.strip()

            parent = root / "parent"
            parent.mkdir()
            run("git", "init", "-q", str(parent))
            (parent / "README").write_text("enclosing repository, not source\n")
            parent_head = commit(parent)
            source = root / "source"
            (source / "scripts").mkdir(parents=True)
            (source / "cmd/aegis").mkdir(parents=True)
            (source / "go.mod").write_text("module example.invalid/provenance\n\ngo 1.24.0\n")
            (source / "cmd/aegis/main.go").write_text("package main\nfunc main() {}\n")
            (source / ".gitignore").write_text("/.aegis-build-*/\n/aegis\n")
            shutil.copy2(REPO / "scripts/build-source.sh", source / "scripts/build-source.sh")
            run("git", "init", "-q", str(source))
            expected = commit(source)
            self.assertNotEqual(parent_head, expected)
            worktree = parent / "nested"
            run("git", "worktree", "add", "--detach", str(worktree), "HEAD", cwd=source)
            self.assertTrue((worktree / ".git").is_file())
            output = worktree / "aegis"

            def settings():
                result = run("go", "version", "-m", str(output))
                return dict(line.strip().split("\t", 1)[1].split("=", 1)
                            for line in result.stdout.splitlines()
                            if line.strip().startswith("build\t") and "=" in line)

            # Call from both roots; inherited foreign Git bindings cannot choose
            # the source. Verify actual Go metadata, not application ldflags.
            for cwd in (parent, worktree):
                run(str(worktree / "scripts/build-source.sh"), str(output), cwd=cwd,
                    extra={"GIT_DIR": str(parent / ".git"), "GIT_WORK_TREE": str(parent)})
                self.assertEqual(settings()["vcs.revision"], expected)
                self.assertEqual(settings()["vcs.modified"], "false")
            (worktree / "cmd/aegis/main.go").write_text("package main\nfunc main() { /* dirty */ }\n")
            run(str(worktree / "scripts/build-source.sh"), str(output))
            self.assertEqual(settings()["vcs.revision"], expected)
            self.assertEqual(settings()["vcs.modified"], "true")

            # An isolated adversarial Go shim strips VCS stamping. The helper
            # must reject it and must not replace the last verified output.
            previous = output.read_bytes()
            shim = root / "shim"
            shim.mkdir()
            real_go = shutil.which("go")
            (shim / "go").write_text(
                "#!/usr/bin/env python3\nimport subprocess, sys\n"
                f"sys.exit(subprocess.call([{real_go!r}] + "
                "['-buildvcs=false' if a == '-buildvcs=true' else a for a in sys.argv[1:]]))\n")
            (shim / "go").chmod(0o755)
            denied = run(str(worktree / "scripts/build-source.sh"), str(output), check=False,
                         extra={"PATH": str(shim) + os.pathsep + env["PATH"]})
            self.assertNotEqual(denied.returncode, 0)
            self.assertIn("source build provenance denied", denied.stderr)
            self.assertEqual(output.read_bytes(), previous)


if __name__ == "__main__":
    unittest.main()
