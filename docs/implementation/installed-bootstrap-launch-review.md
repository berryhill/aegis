# Installed bootstrap launch impact review

Source-only candidate; no commit, push, release or production authentication.

Affected and updated: README, SECURITY, threat model, architecture including diagram,
five-minute quickstart routing, changelog, supported installed-bootstrap procedure,
shipped operator-lifecycle skill and regenerated content-addressed skill manifest.

Reviewed unaffected: Apache LICENSE, CODE_OF_CONDUCT, CONTRIBUTING (existing build
and test policy), no-key demonstration script, docs/RECORDING.md historical recording,
docs/contributing/ISSUE_BACKLOG.md early contributor proposals, release workflow and
scripts/release.sh canonical archive/checksum naming. The bootstrap is not shown by
the historical recording and is not part of the no-key demo. Those workflows were
not re-recorded or represented as bootstrap proof. No release artifact or checksum
was fabricated or republished. New official coordinator release and independent
exact-candidate/live authenticated acceptance are outstanding launch gates owned
by the parent. Existing published coordinators do not gain this command retroactively.

Verification: full internal/update and internal/userservice suites, skill bundle
validation, focused installed upgrade lifecycle command suite, source build and
public update help. Python acquisition regressions stub downloads and verify exact
canonical URLs, checksums, archive rejection, private permissions and exec argv.
RED: digest-only prior Prepare admission accepts same-byte inode and mode changes;
GREEN: exact inode/mode/digest admission rejects both. Existing Prepared seals remain.

Opt-in official v0.2.29 subprocess reproduction was actually exercised, downloading
public GitHub latest metadata/assets only. All target/config/state/service responses
were private isolated fixtures. It returned preflight `conflicting upgrade intent;
exact journal reconciliation required` for retained v0.2.30; then the new coordinator
completed isolated upgrade and repeated authenticated Console verification. This
proves neither live user input nor production gateway recovery.

Acquisition prerequisite: a trusted installed Go toolchain (`go version -m`), Python 3, and Linux `/proc` with fd-based exec support. No automatic Go installation or toolchain download occurs. Before execution the helper checks ELF architecture, exact Aegis main package/module and release version, and clean Git provenance with a 40-hex revision without executing the artifact. Inspection and launch use the same open inode. Evidence/coordinator live in a fresh private directory directly under the actual passwd operator home, never HOME/TMPDIR or authority state; symlink and writable/unowned ancestors are denied without chmod or deletion. Same-user/root hostile mutation is not a sandbox boundary.
