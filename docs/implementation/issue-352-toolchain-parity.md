# #352 compiled Go bundle toolchain/PATH parity

## Scoped contract

Baseline `3eb51a7d584a45ebf402f090ea1e7d28d98bbca9`, branch
`fix/352-compiled-toolchain-env`. One source-owning engineering builder under the
attended parent; this is not Aegis-managed Loop execution or a product receipt.
The combined repair covers the shard runner/PATH tests, the baseline-derived
WNOWAIT-owned native barrier-first cleanup and focused Linux regressions, this
ledger, bounded changelog and affected cleanup-boundary documentation. Historical
failed traversal-order candidates below are retained as history, not current
claims. Native cleanup changes algorithm order within the actual authority/proof
boundary; admission/ownership, full fresh post-scan, sole Wait, unknown custody,
fixture hard assertions, CI topology, group membership, alarms and budgets remain
required. No
commit, publication, release, installed update or standing service restart.
Parent owns independent acceptance and the eventual single correction PR after
#435 publication custody. The separate earlier proof clone remains untouched.

## Demonstrated defect and minimal correction

Go 1.26.10 `src/cmd/go/internal/test/test.go:92–94` documents that tests receive
`GOROOT/bin` before the ambient `PATH`, so nested `go` commands use the selected
compiler. Execution at lines 1665–1668 uses `cfg.OrigEnv`, `base.AppendPATH` and
`base.AppendPWD`; `internal/base/env.go:29–45` implements the PATH prefix.
This correction is only PATH parity, not general environment normalization.

The bundle previously compiled using the selected compiler but directly ran
inventory and test binaries with the bootstrap PATH. The runner now queries
`go env GOROOT` in the unchanged build context, requires an absolute unambiguous
root with an executable Go driver, and supplies a copied environment with that
root's `bin` prefixed to the original PATH to both compiled subprocess kinds.
Other environment keys and native-command PATH entries remain unchanged. Empty
PATH receives only the compiler bin. The build and ordinary `go test` subprocess
paths remain unchanged; no shell evaluation or inferred compiler-root layout.

## Builder evidence

Operational evidence is retained outside source at the owner-authorized
`/home/silas/.hermes/profiles/xander/workspace/352-toolchain-parity-repair`.

- `regression-red.log`: new focused environment regressions fail against the old
  implementation before the runner repair (missing environment adapter).
- `real-fixture-baseline-red.log`: the real TestMain fixture applied to the exact
  baseline runner fails compiled inventory when `exec.LookPath("go")` resolves
  the bootstrap instead of `runtime.GOROOT()/bin/go`. This causal control does
  not modify baseline source. The frozen baseline control supplied by the parent
  separately demonstrates the actual native-production test failure and pass
  with only the compiler PATH prefix changed.
- `focused-green.log`: all 33 shard tests PASS, including real normal/race compiled
  fixtures, default test/example/fuzz-child coverage, TestMain and unexpected
  `os.Exit(0)` behavior, race-detector witness and stack preservation, bootstrap
  versus selected toolchain, shared inventory/execution environment, list-only
  nonexecution, invalid/missing/nonexecutable root denial, unchanged ambient
  environment, empty PATH and the existing exact-once topology assertions.
- `native-causal-green.log`: the actual compiled
  `TestContributionNativeProductionCallerJoinsWriter` passes with the fixed
  runner's environment adapter, bootstrap PATH and selected Go 1.26.10; compiled
  image retained as `native-causal.test`.
- `architecture.log`: `python3 scripts/verify-budget.py go test
  ./internal/architecture -count=1` PASS under unchanged limits.
- `build.log`: `scripts/build-source.sh` builds the development candidate outside
  source. Final source, compiler/dependency inputs and executable identities are
  external freeze artifacts, not clean-commit or release claims.
- Full normal/race implementation bundle commands use the existing
  `scripts/verify-budget.py` supervisor, `GOTOOLCHAIN=go1.26.10`, bootstrap PATH
  `/home/silas/go/bin:/usr/bin:/bin`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`,
  `GOMEMLIMIT=512MiB`; each compiled partition retains `-test.timeout=8m`.
  Both earlier actual bundles completed with exit 1; immutable logs/exit files
  remain in the earlier evidence root. Both include the unchanged
  `TestCheckCandidateReturnQuiescence` failure. The earlier whole-package images
  remain old baseline artifacts, not rebuilt native-order candidates. No earlier
  full-bundle PASS or general timeout/quiescence repair is claimed.

## Bounded native inventory-order continuation

Combined-source evidence is retained separately at
`/home/silas/.hermes/profiles/xander/workspace/352-native-order-repair`;
the preceding freeze and independent review remain immutable.

`contributionProcessInventoryFrom` stable-sorts the complete `/proc` result by
descending numeric PID preference. It neither filters entries nor consults their
metadata. Nonnumeric/overflow entries remain present; readDir partial results and
errors are preserved. Membership still comes exclusively from the existing stat
and pidfd identity checks. Pre-observation must still return nil before pinned
group SIGKILL; a fresh post-barrier scan, exact Wait and all one-second bounds
remain unchanged. Unknown cleanup still denies joined proof and retains custody.

- `regression-red.log`: two new behavioral regressions fail against old traversal
  (lexical/input rather than descending numeric visit order).
- `native-normal.log`, `native-race.log`: new ordering/full-entry/error regressions
  and all five original native boundary test roots PASS unchanged in both modes.
  Includes foreign groups, identity reuse, pidfd errors, cancellation, late fork,
  exact Wait and group signal denial; no trusted test was edited.
- `checker-normal.log`, `checker-race.log`: the original 20-attempt/64-busy-child
  production checker fixture FAILS in both modes under the unchanged supervisor
  and one-second cleanup budget. Therefore this heuristic is NOT a qualified
  checker repair. The host now has low newly allocated PIDs alongside high older
  PIDs (PID wrap); descending numeric order is not guaranteed recency. No new
  ownership inference, ascending fallback or boundary rewrite is introduced.
- Actual rebuilt normal/race implementation bundles are owned by the external
  serialized `run-bundles.py`; both retain the fixed PATH runner, existing 8m
  partition alarms and 6GiB/2CPU/256-task supervisor. Read each final `.exit`,
  complete log and `full-bundle-outcomes.json` for terminal outcomes. Pending
  work at process handoff is not PASS; each actual linked image is retained.
- Combined development executable/source/build-input freeze is external. It is
  an unqualified dirty candidate for fresh parent-owned independent review, not
  a clean-commit, merge, release, provider or installed-success claim.

## Pinned-leader numeric traversal continuation

The descending hint above was insufficient after PID wrap; its failures and
interrupted race campaign remain immutable historical evidence, not PASS.
The current `contributionProcessInventoryFrom(group, readDir)` instead visits
positive numeric PIDs >= the kernel-pinned leader group ascending, then lower
positive PIDs ascending, then all remaining entries in stable input order.
No metadata reads, guessed PID maximum, membership inference or dropped entries.
Directory errors and partial results are preserved unchanged. Every original
stat/pidfd/identity/cancellation/error check, full successful pre-scan before group
SIGKILL, fresh post-barrier scan, exact Wait and one-second timer remains intact.

New evidence root:
`/home/silas/.hermes/profiles/xander/workspace/352-native-anchored-repair`.

- `regression-red.log`: both current-low/old-high and current-high/wrapped-low
  regressions fail causally against the descending helper, exit 1.
- `native-normal.log` and `native-race.log`: all new inventory regressions and
  all five original native guard roots PASS, exit 0, unchanged trusted tests.
- Original unchanged 20×64 checker: normal PASS, race FAIL, exit 1 at
  `checker_quiescence_linux_test.go:39`, `checker quiescence unavailable` (6.87s).
  The algorithm is therefore still NOT QUALIFIED; a successful overlay control
  does not establish robust candidate acceptance. No full bundles were launched.
- The next diagnostic must distinguish pre-scan deadline, pinned group barrier,
  post-barrier scan and exact Wait failure without changing production guards or
  trusted tests. The generic fixture message alone does not identify that stage.
- The accepted PATH helper and tests are preserved. Complete source/compiler/
  executable freeze is for parent-owned independent diagnosis, not suite PASS,
  merge, release, installed or provider qualification.

## Streaming readonly priority continuation (FAILED historical candidate)

Evidence root:
`/home/silas/.hermes/profiles/xander/workspace/352-native-priority-repair`.
The two prior numeric-sort continuations above are historical failed experiments,
not current implementation or accepted qualification. Their current unpublished
source/test bytes, modes and hashes were archived as `quarantine.tar.gz` and
`quarantine.json` before replacement; earlier freezes/logs remain untouched.

Production now calls `terminateContributionGroupHintedFrom` with the fixed
internal readonly `unix.Getpgid` function. The original `From` seam calls the
same implementation with a nil hint. There is ONE local `visit(pid)` validation
body: original fresh stat and same-snapshot group/zombie filtering, nonempty
before pidfd open, pidfd pinning, fresh start/group recheck, cancellation before
SIGKILL, exact close/error precedence and benign ESRCH semantics. No cached
identity or hint authority. Positive PIDs with a matching hint are visited
immediately, before later hint enumeration; all other numeric entries are queued
for the identical mandatory validation before a successful scan return. Hint
errors are only priority misses. Nonnumeric/overflow names retain Atoi skip;
zero, negative and duplicate numeric entries retain mandatory validation.
Partial directory errors deny before hints, reads or effects. No metadata is
consulted. No sorting helper/import, goroutine, timer, group signal or API added.
The outer observe -> nil gate -> pinned kernel barrier -> fresh full observe ->
sole Wait -> completion chain remains byte-identical to HEAD, with 1s unchanged.

- `regression-red.log`/`.exit`: functional streaming, hint/cancellation and full
  inventory regressions FAIL against the original implementation with a thin
  unhinted adapter (exit 1), not a syntax-only negative control.
- `native-normal.log` and `native-race.log`/`.exit`: new streaming/full-inventory
  and negative regressions plus all five original native guard roots PASS.
- `checker-normal.log` and `checker-race.log`/`.exit`: unchanged original
  `TestCheckCandidateReturnQuiescence` (20 attempts, 64 busy children) PASS both.
- Only after those stages passed, serialized actual normal/race implementation
  bundles are permitted under the original supervisor and 8m partition limits,
  including the normal contribution partition. Their external logs and terminal
  `.exit` files are authoritative; pending or absent evidence is not PASS.
- Full combined dirty source/compiler inputs/executable freeze and parent-owned
  fresh independent acceptance remain distinct. No publication, release,
  installed adoption, real owner recovery or #375/#379 provider qualification
  is asserted by this source repair.

## WNOWAIT-owned barrier-first continuation (current)

Evidence root:
`/home/silas/.hermes/profiles/xander/workspace/352-native-barrier-repair`.
The preceding priority candidate is NOT accepted: fresh independent original race
checker failed at 2.46s despite parent full-bundle PASS. Its source/tests/docs
were recoverably archived with path/mode/hash metadata before replacement. All
older failed candidates and logs remain untouched.

This repair is an algorithm-order correction, not another scheduling heuristic.
Independent domain review found no normative initial proc-observation permission
gate before signaling an already WNOWAIT-owned private group. Review is not user
approval; the existing explicit repair authorization applies under the actual
authority/proof boundary. Starting from HEAD's baseline native implementation,
remove the initial inventory and run the REAL kernel barrier first, under the
same one-second cleanup context created before the barrier. Command/digest and
reservation before Start, private Setpgid, unreaped exact child, repeated WNOWAIT
and context checks remain required. Lost ownership skips group signal. The
baseline full per-PID observer and pinned helper are unchanged. Fresh complete
post-barrier observation plus sole Cmd.Wait remain necessary for Joined. Signal
success/ESRCH never proves absence. Unknown/errors retain home/workspace custody.
Meaningful effect difference: the admitted owned group may be killed BEFORE a
later proc observation error. That error still denies proof, success and replay.
RoleStop is unchanged pidfd-only cleanup and cannot borrow this group proof.

The original late-fork fixture now delegates only its choreography to a helper:
capture stale proc inventory before barrier, release enumerated worker, prove
actual new same-group writer and enumerated parent exit, run baseline stat/pidfd
validation against captured entries and assert stale-empty, THEN real barrier.
Every original hard assertion and post-return constant-writes check is retained;
immediate writer-state inspection and fresh observer presence are added. Pin the
actual fixture writer with pidfd before sole Wait. Default negative-control test
runs a child test binary with a TEST-ONLY unsafe inventory-only variant, omitting
the real barrier and returning actual stale-empty observation; it must fail the
hard live-writer assertion, not a timeout or call count. Its pinned writer is
killed on failure without signaling a reusable post-Wait group. Production has
no selector or nil/no-op fallback; new AST coverage pins its unconditional real
kernel adapter, and nil barrier denies before reservation. Real barrier followed
by actual full post-scan and EACCES/malformed/unknown errors still denies Joined.

Original 20x64 checker normal/race and original native guards normal/race PASS;
no checker or descendant/group-boundary assertions changed. Subsequent actual
serialized full bundles retain existing groups, 8m partition alarms and cgroup
bounds; read their external terminal exits, not old candidate results. Independent
acceptance, build freeze and publication remain distinct. See external report
for commands, preservation identities and exact current full-bundle outcomes.

Launch impact: CHANGELOG, this ledger, VERIFIED_IMPLEMENTATION and THREAT_MODEL
now describe barrier-first effects and unchanged proof/custody. Other launch
assets remain unaffected; no CLI, authority, API/DTO, model/config, installation,
provider, release binary/checksum or historical demo change is requested.

## Security and launch impact (earlier scheduling/PATH review)

The new root query is a fixed argv invocation of supported `go env`; newline,
carriage-return, PATH-separator, relative, missing and nonexecutable roots deny
before compiled tests. No credential, host Go installation/configuration,
permission, provider or native guard is changed. The parent still must perform
fresh independent testing of the frozen candidate. Existing unrelated failures
are retained, not weakened or hidden.

Reviewed root README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT,
`docs/THREAT_MODEL.md`, `docs/ARCHITECTURE.md`, `docs/QUICKSTART.md`,
`docs/DEMO_NO_KEY.md` and `docs/RECORDING.md` for impact. This development-runner
repair changes only native scan traversal performance, not product CLI, authority,
cleanup/security semantics, installation,
quickstart or historical demonstration contracts. Existing contributor check
commands remain valid. Only CHANGELOG and this ledger need corresponding edits.
Release binaries/checksums, published recordings and contributor issues are
unaffected by scope and were not regenerated, published or globally requalified.
No new launch artifacts, checksum claims, live-provider proof, installed adoption
or #352 closure are inferred from this local source repair.
