import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("publication", Path(__file__).with_name("verify-release-publication.py"))
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class PublicationTests(unittest.TestCase):
    def fixture(self, root):
        names = ["SHA256SUMS", "aegis-skills_v1.2.3.tar.gz"]
        names += [f"aegis_v1.2.3_{os}_{arch}.tar.gz" for os in ("linux", "darwin") for arch in ("amd64", "arm64")]
        assets = []
        for name in names:
            content = ("fixture-" + name).encode()
            (root / name).write_bytes(content)
            assets.append({"name": name, "size": len(content), "state": "uploaded",
                           "digest": "sha256:" + hashlib.sha256(content).hexdigest()})
        return {"tag_name": "v1.2.3", "draft": False, "prerelease": False,
                "published_at": "2026-10-17T00:00:00Z", "assets": assets,
                "html_url": "https://github.com/berryhill/aegis/releases/tag/v1.2.3"}

    def test_exact_readback(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory)
            self.assertEqual(MODULE.verify(self.fixture(root), "v1.2.3", root)["status"], "published_verified")

    def test_remote_tampering_and_incomplete_publication(self):
        for mutate in [lambda r: r.update(draft=True), lambda r: r.update(prerelease=True),
                       lambda r: r.update(tag_name="v1.2.4"), lambda r: r.update(published_at=None),
                       lambda r: r["assets"].pop(), lambda r: r["assets"].append(dict(r["assets"][0])),
                       lambda r: r["assets"][0].update(digest="sha256:" + "0" * 64),
                       lambda r: r["assets"][0].update(size=0),
                       lambda r: r["assets"][0].update(state="new")]:
            with self.subTest(mutate=mutate), tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
                root = Path(directory)
                release = self.fixture(root)
                mutate(release)
                with self.assertRaises(ValueError):
                    MODULE.verify(release, "v1.2.3", root)

    def test_local_replacement_and_symlink_denied(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory)
            release = self.fixture(root)
            (root / "SHA256SUMS").write_bytes(b"changed")
            with self.assertRaises(ValueError):
                MODULE.verify(release, "v1.2.3", root)
            (root / "SHA256SUMS").unlink()
            (root / "SHA256SUMS").symlink_to(root / "aegis-skills_v1.2.3.tar.gz")
            with self.assertRaises(ValueError):
                MODULE.verify(release, "v1.2.3", root)


if __name__ == "__main__":
    unittest.main()
