# Bootstrap test isolation incident

`TestDiscoverInstalledExecutable` wrote its rendered unit through `Preview` while
`serviceFixture` inherited the workstation's `XDG_CONFIG_HOME`. The reported
workstation unit pointed at `/tmp/TestDiscoverInstalledExecutable2186807511/001/aegis`.
This is a test isolation incident, not evidence of a production updater defect.
Production diagnosis and repair belong to the parent operator; this change does
not read, repair, reload, or activate the workstation unit or authority state.

The shared fixture now sets `XDG_CONFIG_HOME` beneath its own `t.TempDir()` before
any caller can invoke `Preview`. A regression seeds an inherited config directory
and asserts both its replacement and the exact private fixture unit path.
Audit: bootstrap discovery lacked isolation; migration, upgrade, and process-image
unit tests already explicitly isolated XDG; status tests use `readyServicePlan`,
which explicitly isolates XDG. Centralizing the fixture closes the caller omission.

Validation (corrected source only, with an additional externally private
`XDG_CONFIG_HOME`): focused discovery/isolation tests passed; full
`go test ./internal/userservice -count=1` passed. `git diff --check` passed.
No production implementation, installed unit, authentication state, or publication
was changed. Existing `.proof-*` directories were preserved.

Launch impact review: README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT,
CHANGELOG, threat model, architecture diagram, quickstart, no-key demonstration,
and recording documentation retain unchanged product contracts. This test-only
repair does not regenerate demonstrations, recordings, release binaries/checksums,
or contributor issues. Existing release-readiness documentation retains those
gates; no remote release checksum, published binary, or external issue was
verified or changed here. No live-repair or release-readiness claim is made.
