# #404 compact source-CI launch review

## Scope and preserved boundaries

The candidate retains all existing package/default-test partitions, with dynamic
actual-mode inventories and complete normal/race coverage, in 7 normal + 13 race
sequential lanes. Seven other required jobs yield 27 expanded ordinary-event jobs.
No child/reusable workflow is introduced. Split packages are compiled/listed once
per lane and executed from the original package cwd; TestMain, runnable examples,
fuzz seeds, anchored selection, uncached execution and independent alarms remain.
Compiler/inventory/execution phase timings are diagnostic, never PASS evidence.
No resource, authentication, crypto, product authority or release policy changes.

## Measurements and outstanding qualification

Packing is revised from actual first-compact hosted run **40884797403**: four
race lanes hit the unchanged 600s deadline (cli-command, manager-a/c/d).
`docs/RELEASE_PIPELINE.md` retains the old/new mapping and observed phase basis.
Seven normal + thirteen race lanes retain exactly 27 jobs; a new stable race
manager-e is offset by removing normal support. Normal manager-a/b and successful
race implementation/support packs remain unchanged. Mixed manager-e compiles each
split package once; the whole CLI group moves to race orchestration.
Planning sums are not hosted success: fresh complete timing including setup is
required. Parent owns publication, metrics, PR/merge and frozen-candidate acceptance.

## Process-lifetime diagnosis and bounded fixture repair

Before editing the fixture, inspected actual admission: `CheckContributionEffectAs`
(writing) calls `CheckContributionRoleAs` -> `checkContributionRole` ->
`Hermes.ContributionProcessIdentity`. That last gate rejects a closed adapter
`p.done`, missing/reused PID/start token, wrong process group or UID. It is NOT
only logical authority freshness. `InspectSession` independently reports the same
PID/start-token liveness through `processMatches`; it is an observation surface,
not the bridge's actual call path. `sleep 240` ends the owned session leader at
240 wall seconds even when the fixture's logical clock and 300s mandate remain
live. Hosted source-drift/current-control callback misses around 243s, plus a
245.97s disconnect stop-join failure after all bridge assertions passed, match
that independent lifetime defect; old logs alone cannot prove every failing gate.

Replace the two shared manager synthetic session scripts' arbitrary sleep with
a private FIFO in each disposable runtime home. The session shell blocks without
a child timer, until normal service process-group stop/Wait or explicit fixture
control; deferred cleanup revokes still-authorized sessions before stores close,
including failure paths. Deliberately expired sessions cannot author a revoke;
cleanup instead terminates/Waits their exact owned process and checks actual
`InspectSession` liveness is false, without changing authority history. No production source changes, TTL/deadline widening,
authority renewal, liveness bypass or changed callback/stop assertion. The fixture
lifetime regression drives real FIFO exit; the existing real-child absent-process
regression now explicitly verifies `ContributionProcessIdentity` denies after
owned process exit. This establishes the pre-callback liveness mechanism, not a
retroactive claim that hosted acceptance passed.

## Focused local builder evidence

Under the existing Go 1.26.10 supervisor and 2-CPU/6-GiB limits:

- Prior independent acceptance passed all 41 Python regressions and the complete
  exact-inventory proof; those artifacts remain unchanged. This continuation
  reran only the 23 affected shard regressions (PASS, 17.638s), including real
  normal/race fixture default leaves and TestMain.
- New FIFO lifetime regression PASS in race mode (1.045s); real-child admission
  after process exit regression PASS in race mode (1.017s).
- The retained logical-clock/absolute-deadline regression remains unchanged.
- Revised compact race manager-c (resume + repair) and manager-d (disconnect +
  joint + readback) are being measured under the existing supervisor with the
  actual pinned distribution bin first on PATH. The first local executions
  completed every journey assertion (resume 166.18s, disconnect 145.85s) but FAILED
  newly checked teardown because deliberate clock expiry correctly denies revoke.
  Cleanup now distinguishes expired process termination from live revocation;
  complete revised lanes are rerunning, not yet claimed PASS. Complete logs/results, build
  and source/executable/toolchain freeze are external evidence; no partial log
  here is represented as complete acceptance.
- Fresh independent and complete hosted tests remain separate parent-owned gates.

## Native binary exit semantics and repository-check repair

Compact native execution and inventory now pass `-test.paniconexit0`, matching
`go test`'s unexpected-zero-exit guard. The existing real Go fixture exercises the
actual compact planner in normal and race modes: ordinary TestMain
`os.Exit(m.Run())` succeeds, but a test-body `os.Exit(0)` must raise a nonzero
subprocess failure with Go's panic diagnostic. Mocked complete-lane checks also
require the guard on every native inventory and execution command. Partition,
resource and alarm assertions are unchanged.

The hosted toolchain-preparation regression now checks the explicit include
matrix against every `(mode, lane)` in `BUNDLES`, including exact multiplicity and
both normal/race modes, instead of requiring the obsolete Cartesian matrix text.
All existing preparation, mandatory-result and no-ignored-failure checks remain.
This changes only source-CI tooling; the launch assets listed below retain their
existing product/security/release contracts. Fresh hosted and independent
acceptance remain parent-owned.

## Launch-asset impact review

- Updated: `CHANGELOG.md`, `docs/RELEASE_PIPELINE.md`, this review.
- Reviewed relevant CI/release statements as unaffected: root `README.md`,
  `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `docs/THREAT_MODEL.md`, `docs/ARCHITECTURE.md` (including its diagram),
  `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`, `docs/RECORDING.md`,
  `docs/contributing/ISSUE_BACKLOG.md`. They name unchanged verification/security
  obligations or product journeys, not a stale expanded lane count. Detailed
  topology belongs in the existing release-pipeline document.
- No-key script/terminal recording and release workflow remain unchanged. This
  engineering-only packing does not alter their CLI, artifact, version, checksum
  or authentication contracts. No new recording, binaries/checksums publication,
  contributor issues, installed update or live-provider qualification is claimed.
- Focused local results and complete source/executable/toolchain identities are
  retained outside the source checkout in the parent's `issue404-evidence`
  directory. Fresh independent frozen-candidate and hosted acceptance are separate.
