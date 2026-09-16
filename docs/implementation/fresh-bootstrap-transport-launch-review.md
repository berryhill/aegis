# Fresh-bootstrap transport ordering — launch-asset review

Scope: follow-up to approved exact-gateway offline recovery. Check the planned transport before fresh initialization publication; repeat configured recovery admission after configuration creation; recheck offline status after operational/custody approvals and protected passphrase intake before the corresponding writes. Presence checks are not an atomic lifecycle lock. Existing approved artifacts are preserved on a later denial. Unknown/stale/foreign transport is not deleted; absent configuration may prevent exact recovery and require owner repair.

## Required asset ledger

| Asset | Review outcome |
|---|---|
| `README.md` | Updated narrow ordering summary and linked existing recovery guidance. |
| `LICENSE` | Reviewed; Apache-2.0 terms unaffected. |
| `SECURITY.md` | Updated pre-write guards and explicit residual lifecycle race. |
| `CONTRIBUTING.md` | Updated isolated regression requirements for no-config and approval/intake transport appearance. |
| `CODE_OF_CONDUCT.md` | Reviewed; conduct/reporting policy unaffected. |
| `CHANGELOG.md` | Added unreleased ordering correction without claiming automatic stale repair. |
| `docs/THREAT_MODEL.md` | Updated bootstrap threat controls and non-atomic residual boundary. |
| `docs/ARCHITECTURE.md` | Updated diagram with fresh initialization, post-configuration admission and pre-write rechecks. |
| `docs/QUICKSTART.md` | Added absent-config repair guidance and preservation/non-atomic limits; retained exact healthy approved-stop workflow. |
| `docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh` | Reviewed; disposable no-key workflow does not enter interactive bootstrap/custody/gateway recovery, so unchanged. |
| `docs/RECORDING.md`, `docs/assets/aegis-no-key.typescript`, `docs/assets/aegis-no-key.timing` | Reviewed; retained historical no-key recording excludes this flow and claims no current bootstrap acceptance. No fabricated replacement capture. |
| `.github/workflows/release.yml`, `scripts/verify-installed-mvi.sh`, `scripts/verify-release-archive.py` | Reviewed; archive/checksum and publication mechanics unchanged. No local `dist` candidate exists in this worktree; current release binaries/checksums were not regenerated or remotely verified. Publication acceptance remains outstanding, not inferred from this review. |
| `docs/contributing/ISSUE_BACKLOG.md` | Reviewed; focused local proposals remain applicable. Online gateway-owned backfill remains separate from offline recovery; no remote issue created. |

`docs/launch/LAUNCH_COPY.md` also reviewed; no change to advertised product scope.

## Local verification

- `git diff --check` passed.
- `go test ./internal/command ./internal/initialize -run 'Test.*(Bootstrap|Initialization|Transport)' -count=1` passed for both packages.
- `python3 scripts/demo_no_key_test.py` passed (two tests).

These results are isolated source regression evidence, not real systemd, live-model, installed release, or remote publication acceptance. The full no-key demonstration and interactive quickstart were not replayed in this docs-only workstream; no operator service, custody material, or Hermes profile was touched. Final combined-source verification belongs to the integrating implementation workstream.
