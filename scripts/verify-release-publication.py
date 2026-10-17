#!/usr/bin/env python3
"""Read back the exact published release and compare GitHub asset digests."""
import hashlib
import json
from pathlib import Path
import re
import sys


def verify(release, tag, dist):
    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", tag):
        raise ValueError("invalid stable release tag")
    if (release.get("tag_name") != tag or release.get("draft") is not False
            or release.get("prerelease") is not False or not release.get("published_at")
            or release.get("html_url") != "https://github.com/berryhill/aegis/releases/tag/" + tag):
        raise ValueError("exact stable release is not published")
    version = tag[1:]
    expected = {"SHA256SUMS", f"aegis-skills_v{version}.tar.gz"}
    expected.update(f"aegis_v{version}_{os}_{arch}.tar.gz" for os in ("linux", "darwin") for arch in ("amd64", "arm64"))
    local = {p.name for p in dist.iterdir()}
    if local != expected or any((dist / name).is_symlink() or not (dist / name).is_file() for name in expected):
        raise ValueError("local release asset set is not exact")
    assets = release.get("assets", [])
    if len(assets) != len(expected) or {asset.get("name") for asset in assets} != expected:
        raise ValueError("published release asset set is incomplete or ambiguous")
    for asset in assets:
        content = (dist / asset["name"]).read_bytes()
        digest = "sha256:" + hashlib.sha256(content).hexdigest()
        if asset.get("state") != "uploaded" or asset.get("size") != len(content) or asset.get("digest") != digest:
            raise ValueError("published asset does not match the verified local bytes: " + asset["name"])
    return {"tag": tag, "url": release["html_url"], "assets": len(expected), "status": "published_verified"}


if __name__ == "__main__":
    try:
        if len(sys.argv) != 4:
            raise ValueError("usage: verify-release-publication.py READBACK_JSON TAG DIST")
        print(json.dumps(verify(json.loads(Path(sys.argv[1]).read_text()), sys.argv[2], Path(sys.argv[3])), sort_keys=True))
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("release publication denied: " + str(error), file=sys.stderr)
        sys.exit(1)
