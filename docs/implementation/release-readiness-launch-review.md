# Release-readiness CI launch impact review

## Change and boundary

CI now distinguishes ordinary source from an already prepared release using the
exact checked-out changelog and single-parent commit evidence, not commit-title
inference. Ordinary candidates still transform nonempty Unreleased in a clean,
hook-isolated clone. Prepared candidates require the actual newest release
version, canonical headings/date, nonempty entries, changelog-only/no-mode-change
commit, preserved parent content outside additive Unreleased entries, and exact
reproduction through `prepare-release-changelog.py`. Tag CI additionally binds
the exact stable tag version. Both paths run unchanged full `make verify`, retain
revision/dirty/mutation checks, and never publish. Two checkout commits are
required for parent evidence; missing evidence denies.

Affected assets updated: `CONTRIBUTING.md`, `CHANGELOG.md`, CI workflow and Make
target. `scripts/release.sh` and the tag-triggered release workflow are unchanged;
the latter independently runs `make verify` before archive verification and
publication.

## Reviewed, unaffected assets

- `README.md`: installation/build, checksum and installed-proof claims unchanged.
- `LICENSE`: Apache-2.0 terms unchanged.
- `SECURITY.md`, `docs/THREAT_MODEL.md`: no authentication, runtime, credential,
  publisher-trust or sandbox boundary changes.
- `CODE_OF_CONDUCT.md`: conduct/reporting policy unchanged.
- `docs/ARCHITECTURE.md`: application architecture and diagram unchanged.
- `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`: CLI and no-key journey unchanged.
- `docs/RECORDING.md`, retained `docs/assets/aegis-no-key.typescript` and
  `docs/assets/aegis-no-key.timing`: historical recording remains historical;
  this CI-only repair does not regenerate or qualify live runtime evidence.
- `.github/workflows/release.yml`, `scripts/release.sh`, archive/checksum gates:
  publication, signer and packaging requirements unchanged. No existing binary
  or remote checksum was inspected or republished by this local repair.
- `docs/contributing/ISSUE_BACKLOG.md`: focused contributor materials unchanged;
  no external issue created.

## Verification and remaining gates

RED: the added prepared-release regression failed with the old script's two-arg
usage rejection (exit 2), after fixing an initial fixture output-path collision.
GREEN: `sh scripts/verify_release_readiness_test.sh` passes: ordinary preparation,
prepared exact identity, automatic main/tag selection, wrong-version denial,
stale empty Unreleased denial, malformed/duplicate/empty changelog denial,
verification-failure propagation, inherited hooks, socket path, dirty source and
revision checks. `sh scripts/release_test.sh` passes its isolated recovery suite;
its pushes are to disposable local bare fixtures, never the real origin.
Shell syntax and `git diff --check` pass.

Full resource-supervised verification and exact committed-head CI are delegated
to the parent/publication owner. No source commit, real push, release, issue,
credential or live runtime operation is performed here. Existing private security
contact/recording/live-provider acceptance limitations documented by launch
assets are not resolved or requalified by this repair.
