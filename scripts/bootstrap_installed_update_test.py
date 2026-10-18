import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("bootstrap", Path(__file__).with_name("bootstrap-installed-update.py"))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)

class BootstrapTests(unittest.TestCase):
    def archive(self, name="aegis", mode=0o755, kind=tarfile.REGTYPE):
        out = io.BytesIO()
        with tarfile.open(fileobj=out, mode="w:gz") as tf:
            m = tarfile.TarInfo(name); m.size = 7; m.mode = mode; m.type = kind
            tf.addfile(m, io.BytesIO(b"fixture"))
        return out.getvalue()

    def test_downloads_and_strict_archive(self):
        for bad in (None, "digest", "path", "mode", "link", "duplicate-checksum"):
            with self.subTest(bad=bad), tempfile.TemporaryDirectory() as root:
                archive = self.archive("../aegis" if bad == "path" else "aegis", 0o777 if bad == "mode" else 0o755, tarfile.SYMTYPE if bad == "link" else tarfile.REGTYPE)
                asset = "aegis_v1.2.3_linux_amd64.tar.gz"
                sums = f"{hashlib.sha256(archive).hexdigest()}  {asset}\n".encode()
                if bad == "digest": sums = b"0" * 64 + b"  " + asset.encode() + b"\n"
                if bad == "duplicate-checksum": sums *= 2
                calls = []
                def fetch(url, limit):
                    calls.append(url)
                    self.assertTrue(url.startswith(b.BASE + "/v1.2.3/"))
                    return sums if url.endswith("SHA256SUMS") else archive
                with patch.object(b.platform, "machine", return_value="x86_64"), patch.object(b.platform, "system", return_value="Linux"):
                    if bad:
                        with self.assertRaises(ValueError): b.acquire("1.2.3", root, fetch)
                    else:
                        result = b.acquire("1.2.3", root, fetch)
                        self.assertEqual(result.read_bytes(), b"fixture")
                        self.assertEqual(result.stat().st_mode & 0o777, 0o700)
                        self.assertEqual(result.parent.stat().st_mode & 0o777, 0o700)
                        self.assertEqual(len(calls), 2)

    def test_official_identity_and_negatives(self):
        import os
        fixture = Path(__file__).resolve().parents[1] / ".proof-old-release/aegis-v0.2.29"
        if not fixture.exists():
            self.skipTest("optional official v0.2.29 offline fixture absent")
        evidence = fixture.parent
        with tempfile.TemporaryDirectory() as root:
            def fetch(url, limit):
                return (evidence / url.rsplit("/", 1)[1]).read_bytes()
            acquired = b.acquire("0.2.29", root, fetch)
            self.assertEqual(acquired.read_bytes(), fixture.read_bytes())
        fd = os.open(fixture, os.O_RDONLY)
        try:
            b.validate_executable(fd, "0.2.29")
            with self.assertRaises(ValueError): b.validate_executable(fd, "0.2.30")
        finally: os.close(fd)
        with tempfile.TemporaryFile() as f:
            f.write(b"fixture"); f.flush()
            with self.assertRaises(ValueError): b.validate_executable(f.fileno(), "0.2.29")

    def test_metadata_denials(self):
        good = "binary: go1.26\n\tpath\tgithub.com/berryhill/aegis/cmd/aegis\n\tmod\tgithub.com/berryhill/aegis\tv0.2.29\n" + "".join("\tbuild\t" + x + "\n" for x in ["GOOS=linux", "GOARCH=amd64", "vcs=git", "vcs.modified=false", "vcs.revision=" + "a" * 40])
        b.validate_metadata(good, "0.2.29", "amd64")
        for old, new in [("cmd/aegis", "cmd/other"), ("v0.2.29", "v0.2.30"), ("GOOS=linux", "GOOS=darwin"), ("GOARCH=amd64", "GOARCH=arm64"), ("modified=false", "modified=true"), ("a" * 40, "a" * 39)]:
            with self.subTest(old=old), self.assertRaises(ValueError): b.validate_metadata(good.replace(old, new), "0.2.29", "amd64")

    def test_home_and_fd_launch(self):
        import os
        home = b.trusted_home()
        self.assertEqual(str(home), b.pwd.getpwuid(os.getuid()).pw_dir)
        with tempfile.TemporaryDirectory(dir=home) as root:
            executable = Path(root) / "aegis"; executable.write_bytes(b"fixture"); executable.chmod(0o700)
            with patch.object(b.sys, "argv", ["bootstrap", "1.2.3"]), patch.object(b, "acquire", return_value=executable) as acquire, patch.object(b, "validate_executable") as validate, patch.object(b.os, "execve") as execute:
                b.main()
                acquire.assert_called_once_with("1.2.3", home)
                self.assertEqual(validate.call_args.args[0], execute.call_args.args[0])
                self.assertEqual(execute.call_args.args[1], [str(executable), "update", "--installed"])
                self.assertEqual(execute.call_args.args[2], dict(os.environ))
            with patch.object(b.pwd, "getpwuid") as user:
                user.return_value.pw_dir = str(Path(root) / "link")
                (Path(root) / "link").symlink_to(home, target_is_directory=True)
                with self.assertRaises(ValueError): b.trusted_home()
                user.return_value.pw_dir = root
                Path(root).chmod(0o770)
                with self.assertRaises(ValueError): b.trusted_home()

    def test_launch_seal_ignores_read_atime_but_rejects_mutation(self):
        import os
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory(dir=b.trusted_home()) as root:
            executable = Path(root) / "aegis"
            executable.write_bytes(b"fixture")
            executable.chmod(0o700)
            st = executable.stat()
            fields = {key: getattr(st, key) for key in ("st_dev", "st_ino", "st_mode", "st_uid", "st_gid", "st_size", "st_mtime_ns", "st_ctime_ns")}
            for changed in (False, True):
                before = SimpleNamespace(**fields, st_atime_ns=1)
                after_fields = dict(fields)
                if changed:
                    after_fields["st_mtime_ns"] += 1
                after = SimpleNamespace(**after_fields, st_atime_ns=2)
                with self.subTest(changed=changed), patch.object(b.sys, "argv", ["bootstrap", "1.2.3"]), patch.object(b, "acquire", return_value=executable), patch.object(b, "validate_executable"), patch.object(b.os, "fstat", side_effect=[before, after]), patch.object(b.os, "execve") as execute:
                    if changed:
                        with self.assertRaises(ValueError): b.main()
                        execute.assert_not_called()
                    else:
                        b.main()
                        execute.assert_called_once()

    def test_bad_versions_never_download(self):
        for version in ("v1.2.3", "1.2.3;id", "../1.2.3"):
            with self.assertRaises(ValueError): b.acquire(version, None, lambda *_: self.fail("network"))

if __name__ == "__main__": unittest.main()
