# Manager response-phase PTY sender launch review

Scope: test-only repair for `TestManagerSlashRoutingConsumesUnknownMalformedAndLeadingWhitespaceLocally`. Response text is emitted after the composer restores canonical input; sending `/quit\r` at that point can translate CR to LF and leave the next rich editor waiting for submission. The sender now waits for the next composer prompt, emitted in raw mode. Production input behavior and process deadlines are unchanged.

## Evidence

The held-response regression runs the real manager command with an isolated PTY and disposable state. It pauses the response writer after the visible marker, verifies canonical CR-to-LF mode, observes no early exit input, releases the response, and verifies normal shutdown and exact terminal restoration. The original sender failed this regression with `sender wrote exit before composer readiness (canonical CR becomes LF): ready=1 err=<nil>` in the preceding implementation run. The fixture cleanup now joins a close-only completion channel and releases its writer exactly once.

Final cleanup edits were retested with:

```sh
GOMAXPROCS=2 GOMEMLIMIT=512MiB go test -p=2 -parallel=2 -race ./cmd/aegis -run '^TestManager(ResponseSenderWaitsForComposerReadiness|SlashRoutingConsumesUnknownMalformedAndLeadingWhitespaceLocally|PTYLifecycleSignalsEOFAndExitAliases|ExclusiveWaiterCleanup)$' -count=3 -timeout=120s
GOMAXPROCS=2 GOMEMLIMIT=512MiB go test -p=2 -parallel=2 -race ./internal/tui -count=1 -timeout=30s
```

Both passed (23.028s and 1.397s package durations). Earlier implementation evidence also records ten passing race iterations of lifecycle/exclusive-waiter/readiness helpers, before the final cleanup edit. No full-suite, laptop, live-provider, release, or installed-candidate acceptance is claimed.

## Required launch-asset ledger

| Asset | Review/result |
| --- | --- |
| Root README | Reviewed; unaffected test-only synchronization, no changed public commands. |
| LICENSE | Reviewed; unaffected licensing. |
| SECURITY.md | Reviewed; no production authority, terminal-mode, credential, or security-boundary changes. |
| CONTRIBUTING.md | Updated with response-versus-composer readiness rule and regression name. |
| CODE_OF_CONDUCT.md | Reviewed; unaffected. |
| CHANGELOG.md | Updated with narrow test repair and unchanged production/deadline boundary. |
| Threat model (`docs/THREAT_MODEL.md`) | Reviewed; unaffected production trust boundary. |
| Architecture diagram (`docs/ARCHITECTURE.md`) | Reviewed; unchanged application architecture. |
| Five-minute quickstart (`docs/QUICKSTART.md`) | Reviewed; unchanged commands; not rerun under this focused-test-only assignment. |
| No-key demonstration (`docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh`) | Reviewed; does not exercise this manager test sender; not rerun. |
| Short terminal recording (`docs/RECORDING.md`, retained assets) | Reviewed recording contract; remains historical no-key development evidence, not a fresh manager or release proof. No regeneration/publication. |
| Release binaries/checksums (`.github/workflows/release.yml` and documented installed verifier) | Reviewed contract; source packaging unchanged. No archive generation, checksum acceptance, tag movement, or release publication. Laptop candidate `a2d5a334ad48d74deda194a02cb6b5c63aacc740` remains untouched. |
| Focused contributor issues (`docs/contributing/ISSUE_BACKLOG.md`) | Reviewed local proposals; unaffected. No remote issues created. |

Release readiness remains with the parent: this narrow PR does not certify all launch assets, rerun the full release gate, or resolve pre-existing documented launch/publication gaps. PR 269's separate worktree is outside this change.

## Integration of merged PR 269

Merged main `d61fa2ba18e343f3551a250acb1a3099df5b96a6` into the existing PTY feature branch with a normal merge, preserving both changelog entries and the reset implementation/security documentation without conflict. The PTY fixes remain unchanged; the reset security and command-proof ledger is retained in `reset-orphan-launch-review.md`. No parent proof files, socket, or installed candidate were changed.

Bounded combined regression:

```sh
timeout --kill-after=5s 160s env GOMAXPROCS=2 GOMEMLIMIT=512MiB go test -p=1 -parallel=1 -race ./cmd/aegis ./internal/tui ./internal/reset ./internal/initialize ./internal/command -run 'TestManager(ResponseSenderWaitsForComposerReadiness|SlashRoutingConsumesUnknownMalformedAndLeadingWhitespaceLocally|PTYLifecycleSignalsEOFAndExitAliases|ExclusiveWaiterCleanup)$|TestOrphan|Test.*Reset|TestFreshBootstrapTransport|TestBootstrapTransport|TestPlanRejectsPresentTransport|TestExplicitInitDecline' -count=1 -timeout=120s
```

Passed: manager 9.802s, reset 3.060s, initialize 1.018s, command 23.144s. The selected expression matched no `internal/tui` tests; its package result is not additional TUI coverage. No full suite was run.

Integration launch review: README, SECURITY, threat model, architecture, path layout and quickstart retain PR 269's exact development-only reset safety claims; CONTRIBUTING retains the response/composer readiness rule; CHANGELOG retains both fixes. LICENSE, CODE_OF_CONDUCT, no-key demonstration/script, recording contract, contributor proposals and release workflow remain unaffected by this integration. No demo/recording replay, release archive/checksum verification, release publication or live-service acceptance is claimed. Parent retains exact-head CI, installed proof and final PR merge responsibility.
