# Managed contribution candidate qualification (source candidate)

The managed controller is an intermediate candidate path, not GitHub publication
or final contribution success. Its native execution profile is restricted to
explicitly approved dependency-free Go modules; native checks are not a sandbox.
Production Git origin checks require exact canonical public GitHub HTTPS. The
optional trusted fetch seam replaces transport only: origin and fetched base
checks still execute. It is not model-selected or exposed in a Loop document.

## Sealed identity

`head_commit` and `head_tree` identify the Git HEAD anchor, **not** a commit of
candidate changes. `source.digest` is the tested complete worktree identity,
including staged, unstaged, new and ignored regular files and permission modes;
`.git` is excluded. The candidate also binds index, executable bytes, Go binary,
contract, Queue attempt and builder identity. `candidate_commit` and
`candidate_tree` are real local Git objects of the complete tested inventory,
created with a disposable index and fixed controller identity, without changing
the branch/index or publishing. HEAD remains only an anchor.
Readback reloads immutable baseline, candidate and acceptance records and checks
content-addressed source, executable and check output before returning evidence.

## Executed regression

```
GOMAXPROCS=2 go test -p=1 ./internal/implementation -run '^TestManagedContributionRepairAndCustody$' -count=1 -v
```

This deterministic role/transport fixture uses real Git linked-worktree creation,
real native builds and tests, and invokes the built candidate executable through
an immutable required test. A two-file defect starts RED; the wrong first patch
fails a frozen check; a second proposal from the same builder receives prior
failure output and passes with a new tester identity. Drift checks cover source,
new/ignored files, mode, index, toolchain and executable substitution. Same-run
reservation and restart replay are denied, and corrupt prior failure evidence
makes readback fail closed.

## Unqualified acceptance / remaining work

This fixture does not prove real-provider behavior or installed/live acceptance.
The separate `TestContributionQueueNativeRepairIntegration` exercises actual Queue,
Git worktrees, native build/checks and Hermes protocol with a synthetic role
resolver and synthetic runtime/authority fixtures. It does **not** exercise real
application role provisioning and is not live-provider qualification.

Authenticated manager `/contribute run` now routes qualified managed profiles
through `ExecuteContributionAs`, reusing the exact Queue preparation/binding path.
Historical profiles without `managed` remain explicitly intake-only. Missing
independently approved role provisioning is returned inline with exact role and
charter; Run does not provision or renew authority. Same-intent attempts read
retained evidence rather than starting another worker. Candidate acceptance is
not delivery, merge or final contribution disposition (#378/#379).

## Retained custody and restart

Existing foreign worktrees remain denied and untouched. Only the same durable
run, contract, Queue attempt, implementation, base, workspace, repository and
controller-derived branch may continue. Continuation acquires the existing
advisory custody lock, reloads durable state and validates the complete boundary
source and Git identity. Supported boundaries are after trusted failing baseline
and after a completed failed candidate/check with the tester closed. Every new
candidate consumes its durable pass before runtime/file effects. Exhaustion,
stale Queue attempts, unknown running/interrupted effects and drift deny without
releasing reservations. A retained builder must still match its original role;
this does not provide #377 authority renewal. Noncooperating same-UID writers
remain outside advisory-lock guarantees.

Focused retained-boundary and privacy regressions:
```
GOMAXPROCS=2 go test -p=1 ./internal/implementation -run '^TestContribution(RetainedBoundary|TesterEvidencePrivacy)$' -count=1 -v
```

## Source privacy

Complete inventories and bytes remain private controller custody for native
checks and frozen identity. Tester model input contains source schema/digest only,
not inventory bytes or filenames. Builder context remains restricted to the
explicit admitted UTF-8 repository context and its per-file/total/serialized
bounds; role validation grants no credential access. Sensitive-name and
recognizable private-key rejection are narrow checks, not a general secret
scanner or proof that arbitrary approved source contains no secrets.
The tester runtime has no generic tools. A one-shot controller capability runs
immutable checks after clean runtime-session creation, with fresh tester admission
before and after execution. Acceptance uses controller observations, not model
text. Each tester is closed before repair; close failure aborts rather than
resuming the builder. Typed sanitized criterion packets feed the same builder.
Readback rederives observations and pins exact policy; internal accepted-candidate
export denies missing/corrupt evidence and stale policy. Runtime callback protocol
regressions are deterministic fixtures, not real-provider acceptance.

Skill/launch impact: existing shipped contribution/intake instructions must not
advertise whole-contribution execution or delivery from this intermediate path.
No release, installation, production activation, model or provider change is
included. Full launch-asset synchronization remains required before completion.


## Downstream source-custody consumer seam (#376)

`Service.WithAcceptedContributionCandidateAs(ctx, subject, key, gate, action) error`
resolves sealed intake and immutable Loop server-side, then calls
`QueueWorker.WithAcceptedContributionCandidate`. The kernel API
`ManagedContribution.WithAcceptedCandidate` holds the existing exclusive advisory
workspace lock throughout the synchronous action and pre/post checks.
`AcceptedContributionCandidateAs` and `AcceptedCandidate` keep their exact
candidate/error return types as diagnostic readback, **not custody leases**.

Exact Go signatures (the Queue candidate and gate types alias implementation):
```go
func (s *Service) WithAcceptedContributionCandidateAs(ctx context.Context, subject core.Subject, key string, gate orchestration.ContributionQuiescenceGate, action func(context.Context, orchestration.ContributionCandidate) error) error
func (w *QueueWorker) WithAcceptedContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision, gate ContributionQuiescenceGate, action func(context.Context, ContributionCandidate) error) error
func (e *ManagedContribution) WithAcceptedCandidate(ctx context.Context, run, contract string, policy loop.ContributionExecutionPolicy, gate ContributionQuiescenceGate, action func(context.Context, ContributionCandidate) error) error
```

The gate runs before and after the callback under the same source lock. Both
receive independently cloned proof; neither can mutate the retained proof anchor.

CI preparation uses `scripts/prepare-ci-go-toolchain.py` only for the exact
GitHub-hosted `RUNNER_TOOL_CACHE/go/$GO_VERSION/$arch` setup-go distribution.
It removes group/other write across that complete distribution without changing
read/execute bits or parent directories; workstation execution, path drift,
symlinks, hardlinks and non-job-owned entries fail closed. The production native
toolchain identity and complete mode/source gate remain unchanged. Both normal
and race shards remain mandatory under their existing time bounds.

The shared fresh admission retains authenticated subject, host approval, sealed
contract/policy, Queue claim/attempt, execution, controller Agent and runtime
authority checks. Under lock it reloads immutable evidence and verifies full source,
Git HEAD/tree/index, executable bytes and toolchain before and after the action,
including on callback failure. Callback errors remain discoverable with errors.Is/As.
Any source/evidence/Git identity drift denies; no old receipt is rewritten.
The callback grants no source edits, commit, remote/publication, merge or stop
authority. #378 separately owns actual author/committer commit binding.

`ContributionQuiescenceGate` is mandatory controller-supplied exact stop/epoch
validation. Nil returns `implementation.ErrContributionQuiescenceUnavailable`;
there is no production default-allow gate. #377 must supply exact run/claim/epoch
and durable quiescence validation. A successful gate is a controller assertion,
not #376-generated stop proof. Nil role.Close, an idle map, dead PID, expired lease
or readback is not durable stop or replacement-writer authority. Existing claims
about tester closure describe application lifecycle only, not crash-safe stop.
Current roles are tool-free and edits controller-owned: this seam proves
cooperative kernel exclusion and checked identity, not a sandbox for noncooperating
native code. All callback work must finish before return; no asynchronous source
access may survive it. Post-check failure cannot undo prior effects: consumers
must reserve/reconcile effects and enforce their own authority gates.

The callback does not allocate a new candidate, change durable Consumed/pass
history, or reset Queue attempts, runtime turns, decision attempts, infrastructure
rechecks or original absolute deadline. #377/#378 consume the same root and
cumulative budget. Source/input/mode/index/HEAD/binary/toolchain/policy/evidence
drift invalidates dependent acceptance and requires admitted same-root rebuild,
freeze, fresh independent test and review within remaining budget. Never reset
counters, reopen terminal Queue work or treat readback as publication custody.

Focused synthetic Queue regression (not real-provider or durable-stop proof):
```sh
GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/orchestration -run '^TestContributionQueueNativeRepairIntegration/tester_close_failure_false$' -count=1 -timeout=180s
```
Custody subtests reuse the fixture for lock-held concurrent writer exclusion,
callback error preservation, missing/denied gates, expired/revoked authority and
pre/post source/evidence drift. The synthetic gate is not #377 implementation.
No publication adapter or broad CI repair scheduler is added.
