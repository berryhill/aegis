#!/usr/bin/env python3
"""Adversarial tests for release archive shape and extraction safety."""

from __future__ import annotations

import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from collections.abc import Sequence


REPO = Path(__file__).resolve().parents[1]
VERIFIER = REPO / "scripts" / "verify-release-archive.py"


class ReleaseArchiveContract(unittest.TestCase):
    def write_archive(self, name: str, members: Sequence[tuple[str, bytes, int, bytes | None]]) -> Path:
        path = Path(self.root.name) / name
        with tarfile.open(path, "w:gz") as archive:
            for member_name, payload, mode, link in members:
                info = tarfile.TarInfo(member_name)
                info.mode = mode
                if link is not None:
                    info.type = tarfile.SYMTYPE
                    info.linkname = link.decode()
                    archive.addfile(info)
                else:
                    info.size = len(payload)
                    archive.addfile(info, io.BytesIO(payload))
        return path

    def setUp(self) -> None:
        self.root = tempfile.TemporaryDirectory(prefix="aegis-archive-test-")

    def tearDown(self) -> None:
        self.root.cleanup()

    def verify(self, archive: Path, expected_status: int) -> str:
        result = subprocess.run(
            ["python3", str(VERIFIER), str(archive)], text=True, capture_output=True, check=False
        )
        self.assertEqual(result.returncode, expected_status, result.stderr)
        return result.stderr

    def test_accepts_one_root_executable(self) -> None:
        archive = self.write_archive("valid.tar.gz", [("aegis", b"binary", 0o755, None)])
        self.verify(archive, 0)

    def test_rejects_traversal_absolute_nested_and_extra_members(self) -> None:
        for index, members in enumerate((
            [("../aegis", b"binary", 0o755, None)],
            [("/aegis", b"binary", 0o755, None)],
            [("bin/aegis", b"binary", 0o755, None)],
            [("aegis", b"binary", 0o755, None), ("README", b"extra", 0o644, None)],
        )):
            with self.subTest(index=index):
                error = self.verify(self.write_archive(f"path-{index}.tar.gz", members), 1)
                self.assertIn("release archive denied:", error)

    def test_rejects_links_and_non_executable_or_overpermissive_modes(self) -> None:
        cases = (
            [("aegis", b"", 0o755, b"target")],
            [("aegis", b"binary", 0o644, None)],
            [("aegis", b"binary", 0o775, None)],
        )
        for index, members in enumerate(cases):
            with self.subTest(index=index):
                self.verify(self.write_archive(f"metadata-{index}.tar.gz", members), 1)


if __name__ == "__main__":
    unittest.main()
