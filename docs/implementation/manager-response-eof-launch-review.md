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

Release readiness remains with the parent: this narrow PR does not certify all launch assets, rerun the full release gate, or resolve pre-existing documented launch/publication gaps. Active PR 269's worktree is outside this change.
