# Issue 352: bounded aggregate scheduling repair

## Contract and evidence boundary

Source baseline: `qualify/352-aggregate-repair`, HEAD
`f3b8fa81918f51ec1a3fe12c3a0964aa6848629d`; initial worktree was clean.
Read `AGENTS.md`, `.hermes/README.md`, the canonical contribution happy-path
contract and generic implementation-delivery contract. Explicit local-only scope
controls: no publication, installation, live authority, candidate launch or helper
changes. This document is a presence artifact, not an acceptance receipt.

Owner-reported existing RED: API aggregate timed out at 600 seconds with 178
completed passes and no completed assertion failure. That run was not repeated
or independently reconstructed here. The bounded repair adds exactly one entry
`t.Parallel()` to each existing top-level test:

- `TestRetainedDoerSlowHelperExecutionParity`
- `TestConsoleQueueScopeAuthenticatedRoutes`
- `TestInstalledOnlineLifecycle`

No other existing test bytes, assertions, sleeps, operations, negative boundaries,
deadlines, production limits, CI topology or helpers were changed. This permits
scheduler overlap; it does not itself prove aggregate performance or correctness.

## Fixture custody review

- `apiService` delegates to `apiServiceAt` with a fresh `t.TempDir()`.
  `apiServiceAt` creates root-local synthetic Hermes executables, state,
  principal-verifier file and authority store. `configureAPIFleet` opens the
  corresponding root-local Fleet store and creates a fresh service/worker.
  `LookupEnv` is instance-local; no process environment mutation is needed.
- `socketTestDir` uses unique `os.MkdirTemp` directories, with directory cleanup.
  Each service has its own socket/lock and loopback ephemeral listener. Serve
  constructs per-instance Console/command managers, readiness and rate limiters.
- Principal enrollment is shared only through `sync.OnceValues`; its record has
  string/integer fields and is returned by value. Each fixture publishes its own
  verifier file. `loginConsole` creates its own cookie jar/client and performs
  actual password login against that fixture's listener.
- `queueScopeFixture` retains a unique repository-local `.issue327-state-*` root
  for parent inspection. Its repository maps, registered Agent and seeded history
  are fixture-local. Composer values/requests and readback fingerprint maps are
  freshly allocated. `queueScopeStates` has only its declaration and read-only
  iteration/slicing uses. Sequential subtests and existing HTTP pacing remain.
- `newInstalledLoopQueueClient` creates private TempDir executable/config/request
  paths. `AEGIS_TEST_FROZEN_BINARY`, when supplied, is read/executed, not rebuilt
  or removed. The retained-slow path's synthetic Laya, Hermes installation,
  workspace and synchronized stage clock are private to that test.
- Lifecycle's fallback build output is `.aegis350-lifecycle-test-bin`; scoped
  reference search found no other writer/reference to that filename. Its build
  uses a unique `.aegis-build-*` staging directory. The retained client builds a
  different private TempDir executable. Lifecycle's supplied
  `AEGIS_TEST_PACKAGED_EXECUTABLE` is read/executed and checked for byte stability,
  not overwritten or deleted. Fallback cleanup remains lifecycle-owned.
- Lifecycle's response-loss proxy renames only its own unique socket. Service
  cancellation/wait precedes registered Fleet/authority/socket cleanup; Console
  shutdown cleanup is registered after store cleanup and runs first (LIFO).
  Child process environment/working-directory settings are per-command, not
  global `Setenv`/`Chdir`. `testprocess.CombinedOutput` owns each child, deadline,
  output buffer and cleanup independently. No reachable test/helper path examined
  uses `t.Setenv`, global cwd mutation or a conflicting shared mutable executable.

This supports the narrowly scoped scheduling change. It is not a claim that
parallel/race execution has passed. Parent custody must exclude competing package
processes writing lifecycle's fixed fallback, or supply the frozen executable.

## Launch-asset impact review

Only test scheduling and this note change; no product behavior, CLI syntax,
configuration, architecture, trust boundary, dependency, runtime compatibility or
release packaging changes. No launch assets were edited. README, LICENSE,
SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG, threat model, architecture
diagram, quickstart, no-key demonstration, terminal recording, release binaries/
checksums and contributor issues have no identified impact from these three
insertions. Full asset-content/currency and release-readiness verification were
not performed under this named-path/timeboxed scope and remain pending, not PASS.

## Observed checks and pending qualification

- `git diff --check`: exit 0, no output.
- `gofmt -l internal/api/installed_doer_success_test.go internal/api/console_queue_scope_test.go internal/api/online_lifecycle_installed_test.go`:
  exit 0, no output (no formatting writes).
- `git diff --numstat` and scoped `git diff`: exit 0; exactly one added line and
  zero removed lines in each named existing test file; each addition is entry
  `t.Parallel()`.
- No Go tests, costly aggregates or candidate builds were executed here, as
  explicitly permitted by the owner. There is no new test PASS output to report.

Parent-owned next verification: run exactly these three tests together with
`-parallel=2` under the existing resource supervisor and unchanged normal 10-minute/
race 15-minute budgets; retain JSON overlap evidence and independently AST-compare
unchanged bodies. Then freeze and perform fresh independent qualification.
Aggregate GREEN, race safety, measured overlap/performance and issue 352 closure
remain unverified. Synthetic ephemeral fixture listeners are not live candidate
launches. Any actual candidate service must use the shipped shared 8450–8469
lifetime port pool; reserved 8448/8098 must not be used.

## Phase 2: superseding three-line candidate failure

The preceding record is retained as history, not current aggregate acceptance.
Parent-reported trusted full normal API verification of the three-line candidate
FAILED at 600.08 seconds: 189 top-level passes, 15 existing optional skips, no
completed assertion failure, and only the slow-helper test active at the deadline.
The parallel group waited until serial tests completed; that scheduling left too
little time for the unchanged slow-helper test. Prior focused normal verification
passed (121.123 seconds); its race run was interrupted by agent_close, neither
PASS nor an assertion failure. No aggregate GREEN is claimed.

The next minimal scheduling candidate preserves those three insertions and adds
one entry `t.Parallel()` to each of `TestPackagedFleetRecipes`,
`TestDoerCandidateSlowPublicProbeRemainsBounded` and
`TestExactLoopQueueImplementationSuccessReplay`. Owner-measured original normal
times were 29.67, 20.29 and 25.7 seconds respectively. Six fixtures can now use
Go's existing bounded `-parallel=2` scheduling. This is not a CI/sharding change.

Pre-edit custody inspection: packaged recipe archive construction writes only a
private TempDir archive, installation targets a distinct private home, and each
recipe subtest uses `apiService` plus a private installed CLI/config/request root.
Optional supplied archive/executable bindings are read-only. Candidate readiness
uses its own service, Fleet store, workspace, private helper and private Laya home.
Exact Queue replay's sequential modes each use private stores, Hermes executable,
installation, source workspace, entered/release files and child command Dir/Env.
No reachable process-global Setenv/Chdir or shared executable writer was found.
Socket directories use unique MkdirTemp; Serve is cancelled and joined before
store cleanup. Shared principal enrollment is synchronized immutable value-copy
only. The process/cwd check found no competing candidate-cwd worker; optional
AEGIS_TEST_/AEGIS_QUEUE_ fixture bindings were absent. Existing three-file custody
conditions above remain binding. No live Aegis Run or reserved listener was used.

Phase 2 focused normal runs only the three additional tests together through the
unchanged `scripts/verify-budget.py` resource supervisor with GOMAXPROCS=2,
GOMEMLIMIT=512MiB, GOFLAGS=-p=1, -count=1, -parallel=2 and -timeout=10m; an outer
180-second worker timebox bounds the focused command without altering test or
production deadlines. Evidence is retained outside source under
`352-evidence/aggregate-repair/phase2`. Parent owns full normal/race qualification.
Whole-file comparison of all six files to HEAD plus only their authorized entry
insertion passed; assertions, helpers, operation/subtest order, latencies, clocks,
authentication, timeouts, limiters, KDFs and expected outputs are unchanged.

Focused actual normal result: exit 0, package PASS 45.897 seconds, no failures or
skips. Top-level elapsed values: slow public probe 20.48s, packaged recipes 35.56s,
exact Queue replay 25.36s. JSON CONT records show recipes/probe start together,
then replay starts after the probe finishes while recipes remain active. These
are scheduler wall intervals, not CPU-overlap measurements. Test-internal fixture
builds remained unchanged; no separate package build or full aggregate/race ran.
The full six-file candidate is not yet aggregate-qualified.

## Superseding fixture hermeticity evidence (local source repair)

This section supersedes the earlier fixture-isolation claim: production
ConfigureFleet can discover the OS user's installed Laya even when the synthetic
QueueWorker starts with NoKeyAdapter. Production discovery is intended and unchanged.
Only configureAPIFleet in internal/api/server_test.go now withholds svc.Hermes in
a synchronous IIFE around ConfigureFleet and defers restoration of the same pointer.
Config is never modified. Later explicit ConfigureImplementation callers are unchanged.

New setup-only TestSyntheticFleetFixtureHermeticity allocates a mode-0700
repository-local MkdirTemp root beneath the operator home, with two independent
apiServiceAt/configureAPIFleet fixtures. It asserts runtime-path absence, exact
original Hermes pointer restoration, byte-identical marshalled synthetic Config,
distinct state/stores/services/workers/adapters and successful later explicit fake
implementation configuration with the original Hermes. No fake or installed
runtime is executed. Cleanup removes only that newly allocated test-owned root,
after registered store cleanups; existing runtime evidence is untouched.

Commands (default toolchain/cache, private workspace-local TMPDIR
`/home/silas/.hermes/profiles/xander/workspace/aegis-352-aggregate/.352-go-tmp-C0rDQ1`):

- RED, before helper edit: `go test ./internal/api -run '^TestSyntheticFleetFixtureHermeticity$' -count=1 -timeout=90s`.
  Exit 1; FAIL TestSyntheticFleetFixtureHermeticity (3.69s), package 3.768s.
  Both first/state/runtime and second/state/runtime actually existed (Lstat err=nil).
  This is real host-dependent installed-discovery leakage, not fabricated output.
- GREEN, after helper edit: same exact command. Exit 0;
  `ok  github.com/berryhill/aegis/internal/api  0.590s`.
- Startup non-regression: `go test ./internal/app -run '^TestConfigureFleetPreparesInstalledDoerWithNativeTrust$' -count=1 -timeout=90s`.
  Exit 0; `ok  github.com/berryhill/aegis/internal/app  0.006s`.
  This existing setup-only test checks the nil-adapter discovery-unavailable path
  and unchanged native configuration, not successful installed model execution.
- `git diff --check`: exit 0, no output.

Raw outputs and individual exit codes are retained in
`/home/silas/.hermes/profiles/xander/workspace/352-evidence/aggregate-repair/fixture-hermeticity/`
as red.log/red.exit, green.log/green.exit and startup.log/startup.exit.
The RED prerequisite is qualified public installed Laya metadata/assets plus trusted
operator-home ancestry. On a host without it, the pre-fix regression may pass;
the post-fix absence invariant remains tested without requiring installation.

Callback audit: internal/app/fleet.go ConfigureFleet assigns the repository/service/
worker, then stores bound Service method values via SetContributionRoleResolver,
SetDoerHostApprovalResolver, SetDoerPassCapResolver and
SetCodingContinuationResolver. Those setters only assign fields; they neither
invoke callbacks nor start goroutines. SetDoerProtectedPaths copies a path slice.
doerControllerProtectedPaths reads configuration/public executable path metadata,
not Hermes. PrepareInstalledDoerRuntime is synchronous; its nil-adapter guard in
prepareInstalledDoerRuntime returns before installed metadata/assets or runtime
directory creation. No worker processing or Serve is started in this setup window.
Stored method values retain the Service receiver, not a snapshot of its Hermes
field. Deferred restoration precedes helper return and any later explicit worker
configuration. No asynchronous missing-Hermes consumer was found on this path.

Custody: the six existing dirty top-level t.Parallel additions and complete original
bodies remain untouched; final git diff shows only those original one-line
insertions in those six files. No production, CI or #377-#379 source was edited.
No server/listener, model, install, authority/config/provider change, commit, push,
publication or external system write occurred. All test commands completed in
the foreground, with fixture store cleanup; no running test handle remains.

Launch impact review: root README, LICENSE, SECURITY, CONTRIBUTING,
CODE_OF_CONDUCT, CHANGELOG, docs/THREAT_MODEL, docs/ARCHITECTURE,
docs/QUICKSTART, docs/DEMO_NO_KEY, docs/RECORDING, docs/RELEASE_PIPELINE
and docs/contributing/ISSUE_BACKLOG were inspected for scoped impact. This is
test-factory-only isolation: public commands, production startup, architecture,
security policy, demo/recording output, release binaries/checksums and contributor
issue proposals are unaffected; no launch asset edit is required. Historical
recordings and remote binaries/checksums/issues were not requalified or published.
The existing documented missing conduct-reporting route remains a launch blocker,
not a blocker for this local test repair.

Pending parent acceptance: old failed-launch/interrupted cases, full normal and
race suites, frozen-candidate independent testing and any broader #352 delivery.
They were NOT EXERCISED here by explicit scope. These two focused passes are not
aggregate, installed-target, live-provider or whole-contribution qualification.

## Parent review: authenticated home versus ambient HOME

Review found that the new regression used os.UserHomeDir while production
installed discovery uses os/user.Current. A disposable HOME override therefore
caused the regression to fail before fixture setup, despite the checkout remaining
under the actual operator home. Parent reproduced that failure (exit 1,
repository-local fixture requires operator-home ancestry), then changed only the
new regression's ancestry lookup to user.Current().HomeDir. The same isolated
HOME invocation passed with exit 0, using explicitly pinned existing public Go
caches. No existing assertion, production code or authority was changed. Raw
review-home-red.log and review-home-green.log are retained alongside the other
fixture-hermeticity evidence. The source candidate must be refrozen after this
review correction; earlier worker hashes are historical, not the final freeze.

## Superseding synthetic rewire repair

Baseline for this repair is clean `qualify/352-aggregate-repair` at
`2d700ab13bda15ee48af2224a8e8513d48ab75ee`. The factory-only isolation above
was incomplete: native-Go-only ConfigureImplementation followed by a direct
ConfigureFleet rewire could rediscover installed Laya metadata and create
state/runtime. Production discovery is intended and remains unchanged.

The typed test helper `configureSyntheticAPIFleet` now calls the same
`svc.ConfigureFleet(repo, fleetService, worker)` with only `svc.Hermes`
temporarily nil, restoring its exact original pointer with defer. All synthetic
ConfigureFleet entry points use it: configureAPIFleet's initial factory, the
loop_queue_success_test.go worker rewire and the installed_hello_chain_test.go
worker rewire. The nil window is synchronous setup only; no configuration,
admission, runtime-directory removal, deadlines or production behavior changes.

TestSyntheticFleetFixtureHermeticity retains its original first/second fixture
isolation and assertions, and adds native-Go-only configuration with the existing
absolute fake GoBinary and no Laya values, followed by the real synthetic rewire.
It asserts runtime-path absence, unchanged Config JSON bytes, exact original
Hermes pointer and expected store/repository/service/worker pointer identities.
The later explicit fake Laya configuration still succeeds with original Hermes.

Actual TDD: the initial typed helper delegated unchanged to ConfigureFleet while
the original factory protection remained. The new regression compiled and failed
both runtime-path-absence assertions (err=nil), exit 1, package 4.021s. After
adding the nil/defer window and routing all three entry points, the SAME regression
passed, exit 0, package 1.172s. App startup check passed, exit 0, 0.006s;
the exact failed-launch API subtest passed, exit 0, 1.511s. Exact commands,
environment and complete observed outputs are retained in
`/home/silas/a352s/.352-rewire-qualification.md`.

All six existing entry t.Parallel insertions remain. Unlike the earlier candidate,
the loop_queue_success_test.go direct rewire is an additional explicit fixture-only
exception: no six-file byte-equality claim applies to this candidate. Existing
assertions, parameters, operation order, deadlines, fault paths, latencies and
expected output are unchanged. Diff review and gofmt listing passed.

Launch impact: test setup isolation only; the production/public launch surface is
unaffected. No launch asset edit is required for this repair. Earlier launch asset
review is historical; full launch currency, remote releases/checksums, recordings
and contributor issue verification were not repeated. No release readiness claim.
No commits, pushes, deployments, installed-runtime execution or external writes.
Full normal/race, other API cases, archives, independent frozen-candidate testing
and broader issue 352 qualification remain parent-owned and NOT EXERCISED here.
