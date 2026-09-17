# Console action-readiness copy correction

## Scope and boundary

`FleetSurfaceAs` requests per-action readiness without an exact operation authority. Orchestration correctly denies such action admission with `authority_context_required`; that does not establish incomplete bootstrap. The console previously described every blocked action as “Finish setup for this destination.”

The presentation now says “Action prerequisites” and explains missing operation-specific authority without asserting bootstrap completion or granting permission. Raw reason codes, denied/unavailable/degraded states, repair instructions, native preparation forms, and fresh submission admission remain unchanged. No credential, authorization, configuration, or runtime code changed.

## Verification

- RED: `GOMAXPROCS=2 go test -p 1 ./web/console -run TestActionReadiness -count=1` failed for all seven authority-dependent action cases on the old setup warning.
- GREEN: `web/console`, `internal/orchestration`, and `internal/app` package suites passed after regenerating templ.
- Authenticated HTTP route regression and contextual surface regression passed: `GOMAXPROCS=2 go test -p 1 ./internal/api -run 'TestConsoleSharedShellRendersAllFiveWorkspaceRoutesWithWiredActionReadiness|TestConsoleSurfacePreservesContextualReadinessAndCredentialMetadata' -count=1 -timeout=50s`.
- Console security source checks and pinned vendor verification passed.
- The combined package command timed out at 120 seconds while in `internal/api`; its complete suite is not claimed green. Full repository/race suites and installed-browser acceptance remain unrun for this bounded correction.

## Launch-asset impact review

Updated: `CHANGELOG.md` and `docs/QUICKSTART.md` explain the corrected guidance.

Reviewed for this presentation-only impact, unchanged: root `README.md`, `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `docs/THREAT_MODEL.md`, the diagram in `docs/ARCHITECTURE.md`, `docs/DEMO_NO_KEY.md`, `docs/RECORDING.md`, and `docs/contributing/ISSUE_BACKLOG.md`. The no-key CLI demonstration and historical terminal recording do not exercise this browser panel; they are not new browser verification. No CLI syntax, security boundary, architecture, license, or contributor proposal changed.

Release workflow `.github/workflows/release.yml` still requires clean-source generation, tests, installed archive proof and checksums before publication. No binaries/checksums or remote GitHub release/issue state were generated, inspected remotely, or published here. Exact-candidate packaging/checksum/browser qualification remains a release gate, not evidence provided by this dirty local worktree. No installs, service restarts, commits, pushes, or deployments were performed.
