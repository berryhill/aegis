#!/usr/bin/env python3
"""Official release acquisition only; all installed mutations belong to Aegis."""
import hashlib
import io
import os
from pathlib import Path
import platform
import pwd
import stat
import struct
import subprocess
import shutil
import re
import sys
import tarfile
import tempfile
import urllib.request
from urllib.parse import urlparse

BASE = "https://github.com/berryhill/aegis/releases/download"
LIMIT = 128 << 20

class ReleaseRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        u = urlparse(newurl)
        if u.scheme != "https" or u.username or u.port not in (None, 443) or u.hostname not in {"release-assets.githubusercontent.com", "objects.githubusercontent.com"}:
            raise ValueError("untrusted release redirect")
        if getattr(req, "release_redirected", False):
            raise ValueError("multiple release redirects")
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        redirected.release_redirected = True
        return redirected

def download(url, limit):
    with urllib.request.build_opener(ReleaseRedirect()).open(url, timeout=120) as response:
        data = response.read(limit + 1)
        if len(data) > limit:
            raise ValueError("release response exceeds bound")
        return data

def acquire(version, root, fetch=download):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("VERSION must be MAJOR.MINOR.PATCH (no v prefix)")
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    if platform.system() != "Linux" or arch is None:
        raise ValueError("installed coordinator bootstrap requires Linux amd64/arm64")
    asset = f"aegis_v{version}_linux_{arch}.tar.gz"
    url = f"{BASE}/v{version}"
    sums = fetch(url + "/SHA256SUMS", 1 << 20)
    entries = [line.split() for line in sums.decode("ascii").splitlines()]
    matches = [line[0] for line in entries if len(line) == 2 and line[1] == asset]
    if len(matches) != 1 or not re.fullmatch(r"[a-fA-F0-9]{64}", matches[0]):
        raise ValueError("missing or ambiguous official checksum")
    archive = fetch(url + "/" + asset, LIMIT)
    if hashlib.sha256(archive).hexdigest() != matches[0].lower():
        raise ValueError("release archive checksum mismatch")
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as tf:
        members = tf.getmembers()
        if len(members) != 1:
            raise ValueError("release must contain exactly one member")
        m = members[0]
        if m.name != "aegis" or not m.isfile() or m.mode != 0o755 or not 0 < m.size <= 256 << 20:
            raise ValueError("unsafe release archive")
        binary = tf.extractfile(m).read()
    # No cleanup: this private directory retains acquisition evidence on failure.
    directory = Path(tempfile.mkdtemp(prefix="aegis-coordinator-", dir=root))
    for name, data in (("SHA256SUMS", sums), (asset, archive), ("aegis", binary)):
        with (directory / name).open("xb") as f:
            f.write(data)
        (directory / name).chmod(0o700 if name == "aegis" else 0o600)
    return directory / "aegis"

def trusted_home():
    # Ignore HOME/XDG/TMPDIR for executable custody; preserve them for native auth.
    home = Path(pwd.getpwuid(os.getuid()).pw_dir)
    if not home.is_absolute():
        raise ValueError("operator home must be absolute")
    for ancestor in [Path("/"), *reversed(home.parents[:-1]), home]:
        st = ancestor.lstat()
        if not stat.S_ISDIR(st.st_mode) or st.st_uid not in (0, os.getuid()) or st.st_mode & 0o022:
            raise ValueError("untrusted operator home ancestry")
    if home.stat().st_uid != os.getuid():
        raise ValueError("operator must own home")
    return home


def validate_metadata(text, version, arch):
    paths, modules, settings = [], [], {}
    for line in text.splitlines()[1:]:
        fields = line.strip().split("\t")
        if fields[0] == "=>":
            raise ValueError("module replacement denied")
        if fields[0] == "path":
            paths.append(fields[1:])
        elif fields[0] == "mod":
            modules.append(fields[1:])
        elif fields[0] == "build" and len(fields) == 2 and "=" in fields[1]:
            key, value = fields[1].split("=", 1)
            if key in settings:
                raise ValueError("duplicate build setting")
            settings[key] = value
    if paths != [["github.com/berryhill/aegis/cmd/aegis"]] or len(modules) != 1 or modules[0][:2] != ["github.com/berryhill/aegis", "v" + version]:
        raise ValueError("wrong Aegis executable package/module/version")
    expected = {"GOOS": "linux", "GOARCH": arch, "vcs": "git", "vcs.modified": "false"}
    if any(settings.get(k) != v for k, v in expected.items()) or not re.fullmatch(r"[0-9a-f]{40}", settings.get("vcs.revision", "")):
        raise ValueError("wrong executable platform or unclean source provenance")


def validate_executable(fd, version):
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    header = os.pread(fd, 64, 0)
    if len(header) != 64 or header[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", header, 16)[0] not in (2, 3) or struct.unpack_from("<H", header, 18)[0] != {"amd64": 62, "arm64": 183}.get(arch):
        raise ValueError("not the expected Linux ELF executable")
    inspector = shutil.which("go")
    if not inspector:
        raise ValueError("installed trusted Go toolchain required for read-only metadata inspection; no automatic install")
    env = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOENV="off")
    # Same open inode is inspected and execve'd; never run artifact for its version.
    with tempfile.TemporaryFile() as output:
        result = subprocess.run([inspector, "version", "-m", f"/proc/self/fd/{fd}"], pass_fds=(fd,), stdout=output, stderr=subprocess.DEVNULL, env=env, timeout=15)
        if result.returncode or output.tell() > 1 << 20:
            raise ValueError("Go metadata inspection failed or exceeded bound")
        output.seek(0)
        validate_metadata(output.read().decode("utf-8"), version, arch)


def main():
    if len(sys.argv) != 2:
        raise ValueError("usage: bootstrap-installed-update.sh VERSION")
    coordinator = acquire(sys.argv[1], trusted_home())
    fd = os.open(coordinator, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        validate_executable(fd, sys.argv[1])
        after = os.fstat(fd)
        stable = lambda st: (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
        if stable(before) != stable(after) or after.st_mode & 0o077 or after.st_uid != os.getuid():
            raise ValueError("coordinator changed during inspection")
        print(f"Verified coordinator and retained evidence: {coordinator.parent}", file=sys.stderr)
        # fd exec prevents pathname substitution; environment/TTY remain native.
        os.execve(fd, [str(coordinator), "update", "--installed"], dict(os.environ))
    finally:
        os.close(fd)

if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, tarfile.TarError, subprocess.SubprocessError) as exc:
        print(f"bootstrap denied: {exc}", file=sys.stderr)
        sys.exit(1)
