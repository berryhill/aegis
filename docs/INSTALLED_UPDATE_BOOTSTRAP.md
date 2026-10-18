# Installed update coordinator bootstrap

This source candidate solves the older-updater bootstrap boundary. It does not
claim that a previously published release already contains the new coordinator.
After a release containing this change is published, choose its exact version:

```sh
./scripts/bootstrap-installed-update.sh VERSION
```

Use a trusted checkout and Python 3 on Linux amd64/arm64. Replace VERSION with
MAJOR.MINOR.PATCH without `v`; it must name an official stable coordinator release
with `update --installed`. No production mutation is needed to build or test:

```sh
go test ./internal/update ./internal/userservice
python3 scripts/bootstrap_installed_update_test.py
./scripts/build-source.sh ./aegis
./aegis update --help
```

The script downloads only `berryhill/aegis` canonical release assets and SHA256SUMS
over HTTPS, bounds response sizes, allows only GitHub release-asset redirects,
requires one exact checksum and one regular `aegis` member at mode 0755, and writes
the executable/archive/checksums to a new mode-0700 private directory beneath the trusted actual operator home.
It prints that retained evidence location and execs the coordinator with original
stdin/stdout/stderr and environment. Keep that evidence until the journey is
resolved; remove only that exact task-created directory when no longer needed.
It never captures a password, evaluates shell text, replaces installed bytes,
changes model settings or edits an upgrade journal. Official HTTPS release custody
is the checksum trust root; this is not detached-signature verification.

The public coordinator discovers the exact Aegis-owned user unit/config, inspects
installed version/provenance from the same inode-sealed byte snapshot, and binds
its inode/mode/digest through Prepare. It uses the ordinary authenticated lifecycle:
principal input through supported protected surfaces, exact pending-intent
reconciliation/history, stop, bounded recovery, replacement, restart, current audit
and authenticated Console verification. Missing ownership, authority, protected
input, ambiguous history or substituted image fails closed. Caller TTY/display
are inherited, not invented; unavailable input is an operator gate. The acquired
coordinator version is distinct from the latest target release it discovers.

## Isolated official old-release reproduction

`TestInstalledBootstrapOfficialOld` is opt-in using `AEGIS_TEST_OLD_RELEASE` pointing
to the official v0.2.29 Linux amd64 executable (pinned SHA256 in the test). It runs
that actual executable's `update` subprocess, with private HOME/XDG/config/state,
private executable and synthetic systemctl readbacks. **This opt-in test downloads
public official release metadata/assets from GitHub.** It never accesses live
configuration, service manager, authority or authentication. It asserts preflight
conflicting-intent denial with retained v0.2.30, then uses the new coordinator's
isolated release fixture to finish and repeat authenticated Console verification.
The script tests stub network downloads; they make no live requests. Neither is
real workstation acceptance. Do not bypass authority gates or hand-replace an
installed image to make an old coordinator proceed.

Acquisition prerequisite: a trusted installed Go toolchain (`go version -m`), Python 3, and Linux `/proc` with fd-based exec support. No automatic Go installation or toolchain download occurs. Before execution the helper checks ELF architecture, exact Aegis main package/module and release version, and clean Git provenance with a 40-hex revision without executing the artifact. Inspection and launch use the same open inode. Evidence/coordinator live in a fresh private directory directly under the actual passwd operator home, never HOME/TMPDIR or authority state; symlink and writable/unowned ancestors are denied without chmod or deletion. Same-user/root hostile mutation is not a sandbox boundary.
