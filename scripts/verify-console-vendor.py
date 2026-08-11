#!/usr/bin/env python3
"""Verify the exact self-hosted Datastar browser asset and public provenance."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / "web" / "console" / "vendor"
ASSET = VENDOR / "datastar-v1.0.2.js"
LICENSE = VENDOR / "datastar-v1.0.2.LICENSE"
PROVENANCE = VENDOR / "datastar-v1.0.2.provenance.json"


def main() -> int:
    metadata = json.loads(PROVENANCE.read_text(encoding="utf-8"))
    actual = hashlib.sha256(ASSET.read_bytes()).hexdigest()
    assert metadata == {
        "asset": "bundles/datastar.js",
        "sha256": actual,
        "source": "https://github.com/starfederation/datastar/blob/v1.0.2/bundles/datastar.js",
        "upstream_commit": "75bd6c6d345ef5e59a3164e6ffa273979b3e4e46",
        "upstream_tag": "v1.0.2",
    }, "Datastar provenance or digest mismatch"
    assert LICENSE.is_file() and LICENSE.stat().st_size > 0, "Datastar license missing"
    print(json.dumps({"asset": str(ASSET.relative_to(ROOT)), "sha256": actual, "upstream_tag": metadata["upstream_tag"]}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
