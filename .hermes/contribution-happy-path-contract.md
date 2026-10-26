# Canonical contribution happy-path contract

This is the single contribution lifecycle and acceptance oracle for #386, epic
#344 and implementation slices #375–#379. It adopts the accepted historical
specification #373; a closed specification is not shipped behavior. The existing
[delivery contract](implementation-delivery-contract.md) retains generic delivery,
authority and release rules. The [Loop hard acceptance](loop-happy-path-hard-acceptance.md)
is an additional non-regression requirement, not a substitute for this contract.
Neither document nor issue creation grants execution or publication authority.

## Invocation and execution boundary

One explicit authenticated session invocation of one selected immutable
contribution Loop with an issue URL owns the complete path below, without
operator-selected workers, binding files, receipt hunting, manual GitHub work or
repeated task-consent/continue prompts. Ordinary conversation, quoted URLs and
issue instructions do not start execution or grant rights. Definition publication
and per-run invocation are distinct. Default target is verified protected-base
merge; explicit local-only, no-publish and no-merge restrictions prevail. A local
artifact is a valid narrower deliverable, never merge success or permission to
close a broader issue. Release, installation and live activation are separate
explicit targets. No new queue, scheduler, watcher, daemon, framework or UI.

Temporary Hermes engineering routing is not Aegis-native product execution.
For owner requests “Hey, implement this GitHub issue”, “implement this issue” or
“fix this GitHub issue”, load `aegis` then `aegis-implementation-model` and
`aegis-development-loop` before selecting execution tools. Use the supported
execution-capable conductor for active `aegis_implementation_loop` only within the
owner's scope; rendered `FlowExecutor.run_task_flow()` instructions are not
engineering execution. Label parent-attended conduction honestly. A detached
one-shot worker does not inherit approval delivery. An unanswerable request is
`approval_transport_unavailable`, not denial or a fictitious pending human request.
Do not create PlanStore/TaskQueue records to install instructions or evade a gate.

## Source observation and truthful topology

Baseline: `52bfce39464868c102da1a51e0acd75a2218b36d`. Reinspect changed source at
integration; these are observations, not permanent capability assertions.

- `internal/managergateway/contribution.go` supports explicit `/contribute
  select`, `run`, `status`, `narrow`, `stop`. `run` returns
  `intake_accepted_execution_separate`, `execution_started: false`,
  `completed: false`; status also reports execution unavailable. This is intake,
  not the autonomous whole contribution.
- `internal/app/contribution.go` selects active exact v6, reads bounded public
  issue/discussion, seals role/scope/acceptance/base/workspace/deadline references,
  replays original intent and submits directly. `internal/contribution/contract.go`
  currently limits the admitted candidate budget to one or two, not the proposed
  three-candidate maximum. A profile's existing workspace/base reference is not
  evidence that a fresh-base isolated worktree was prepared or source custody
  enforced. Replaying the sealed snapshot does not reread latest owner comments.
- `internal/app/contribution_control.go` and fleet Badger control persistence
  append digest-linked narrowing/sticky stop. This is not complete contribution
  crash recovery, fresh-role continuation or acknowledged runtime quiescence.
- `internal/loop/contribution.go:NewContributionRevision` calls
  `internal/loop/implementation_builder.go:NewImplementationRevision`: v6 still
  declares `implement -> done`. `internal/orchestration/contribution.go` rejects
  non-local delivery as `contribution delivery execution unavailable`. Local
  implementation execution is not a fresh independent tester/merge procedure.
- `internal/loop/doer_reusable.go:doerReusableCandidate` deliberately reuses v4
  Doer topology. Native coding context/checks do not turn this into the outer
  contribution path. `web/console/loop_workspace.go` projects declared topology;
  changing its labels cannot supply missing operations.

The required topology is the ordered matrix below, with the concrete repair
edges described afterward. It is a required product contract, NOT today's v6
executable graph. A grouped UI node must drill down to actual ordered operations,
role dispatches, gate/effect intents and evidence checkpoints. Builder, freeze,
independent tester, review, publication, CI, merge and disposition must remain
separately observable. Doer may be an inner builder primitive. #375/#376/#378/#379
must implement those boundaries, not rename Doer steps or hide them in task prose.

## Obligation → operation/checkpoint → evidence → owner matrix

The `contribution-matrix-v1` block is the canonical machine-readable matrix used
by repository architecture tests, not a runtime schema or executable request.
`partial` means only the named foundation exists; `unavailable` means no complete
operation for the stated obligation was observed. `source` points to an inspected
foundation, NOT proof that the obligation is implemented. Each `missing` field
is mandatory until its owner supplies observed replacement evidence. All evidence
names below denote required immutable, controller-validated records, not new
command names. #377 applies fresh authority/continuity to **every** stage; #379
independently joins the complete chain.

<!-- contribution-matrix-v1 -->
```json
[
  {
    "id": "resolve",
    "obligation": "Explicit invocation; resolve canonical issue and latest owner instructions; reconcile existing contribution, source and PR ownership",
    "owners": [375, 377],
    "status": "partial",
    "source": "internal/app/contribution.go",
    "symbol": "AdmitContributionAs",
    "missing": "Complete current-instruction and worktree/PR/merge reconciliation; safe owner reattachment, not replay of stale intake alone",
    "evidence": ["invocation", "instruction-snapshot", "ownership"],
    "recovery": "Same-key same-payload readback; changed payload conflicts; different keys cannot create overlapping writers; reconcile remote state before allocation",
    "boundary": "Resolved intake is neither execution nor completion"
  },
  {
    "id": "seal",
    "obligation": "Seal scope, affected real journeys, trusted acceptance, security invariants, exact roles, target/base, cumulative budgets and deadline; resolve genuine prerequisites",
    "owners": [375, 377],
    "status": "partial",
    "source": "internal/contribution/contract.go",
    "symbol": "Seal",
    "missing": "Full execution/check policy, contribution decision delivery and fresh independently admitted role authority; sealed role references alone are not dispatched roles",
    "evidence": ["contract", "role-authority", "budget"],
    "recovery": "Narrow/stop supersedes stale snapshot; material expansion needs approved amendment and re-admission; no silent authority union or renewal",
    "boundary": "Sealing grants no host, GitHub or merge rights"
  },
  {
    "id": "prepare",
    "obligation": "Prepare one owned isolated worktree from freshly fetched verified protected base; understand, inspect and trace",
    "owners": [376, 377],
    "status": "unavailable",
    "source": "internal/implementation/executor.go",
    "symbol": "snapshot",
    "missing": "Product-owned fresh-base worktree creation/reuse and exclusive fenced source custody; bounded native snapshots are only a reusable foundation",
    "evidence": ["worktree-custody", "base-readback", "inspection"],
    "recovery": "Preserve unrelated dirty/private work; exclude prior writer before replacement; same root/worktree/branch across repairs",
    "boundary": "A configured path or Git HEAD alone proves no owned isolated worktree"
  },
  {
    "id": "build",
    "obligation": "One source-owning builder regresses, implements minimally, runs qualified checks and builds the real candidate",
    "owners": [376, 377],
    "status": "partial",
    "source": "internal/loop/implementation_builder.go",
    "symbol": "NewImplementationRevision",
    "missing": "Closed contribution builder dispatch with bounded secret-safe source/test context, real host admission and enforced edit/command policy",
    "evidence": ["builder-dispatch", "regression-red", "build"],
    "recovery": "Concrete failures return to same source owner; no recursive Doer, speculative polish or edits to trusted acceptance to obtain green",
    "boundary": "Native build/test or selected-file success is not whole-contribution acceptance"
  },
  {
    "id": "freeze",
    "obligation": "Freeze complete source, generated/build inputs, executable and trusted acceptance-policy identity",
    "owners": [376, 377],
    "status": "unavailable",
    "source": "internal/implementation/executor.go",
    "symbol": "snapshot",
    "missing": "Complete contribution manifest linking modes/types/bytes, generated and ignored build inputs, toolchain, binary, policy and candidate lineage",
    "evidence": ["source-inventory", "build-inputs", "executable", "acceptance-policy"],
    "recovery": "Any source/input/mode/binary/policy drift invalidates dependent proof; rebuild and refreeze, never repair an old receipt",
    "boundary": "HEAD, tracked diff or executable hash alone is incomplete source identity"
  },
  {
    "id": "test",
    "obligation": "Fresh independent no-product-edits tester verifies identity before/after and exercises real affected journeys in isolated state",
    "owners": [376, 377],
    "status": "unavailable",
    "source": "internal/orchestration/queue_worker.go",
    "symbol": "Process",
    "missing": "Independent tester runtime/session/authority and criterion-specific real behavior acceptance for the contribution profile",
    "evidence": ["tester-dispatch", "identity-before-after", "behavior-acceptance"],
    "recovery": "FAIL yields reproduction packet; BLOCKED/UNAVAILABLE/NOT EXERCISED never become PASS; no product edits or manually authored expected output",
    "boundary": "Judgment turn, role label, model PASS JSON and selected-file/native receipts cannot substitute"
  },
  {
    "id": "repair",
    "obligation": "Bounded concrete repair, rebuild, refreeze and fresh retest; preserve immutable earlier failures",
    "owners": [376, 377],
    "status": "partial",
    "source": "internal/looprun/run.go",
    "symbol": "CursorStore",
    "missing": "Contribution candidate-lineage budget and automatic same-builder failure handoff spanning test, review, CI and conflict repairs",
    "evidence": ["repair-lineage", "counters"],
    "recovery": "Return to build then freeze/test; initial candidate counts; new SHA/worker/Queue attempt cannot reset budget; exhaustion is non-success",
    "boundary": "A cursor does not make effects atomic or qualify a repaired candidate"
  },
  {
    "id": "review",
    "obligation": "Candidate-bound security and launch-asset review followed by final verification",
    "owners": [376, 379],
    "status": "unavailable",
    "source": "internal/evidence/verifier.go",
    "symbol": "Verify",
    "missing": "Required candidate-bound security/launch reviews and final-verification policy over the full contribution",
    "evidence": ["security-review", "launch-review", "final-verification"],
    "recovery": "Concrete review defects return to build/freeze/test; later doc/generated/build edits invalidate affected acceptance",
    "boundary": "Changed/unaffected/missing launch ledger is required; a narrative review alone cannot mint success"
  },
  {
    "id": "publish",
    "obligation": "Scoped commit and feature publication; one issue-owned non-draft PR under retained repository/base publication custody",
    "owners": [378, 377],
    "status": "unavailable",
    "source": "internal/credentials/broker/model.go",
    "symbol": "github.get_repository.v1",
    "missing": "Closed controller-custodied local Git/feature push/PR adapter, durable intent/results and actor/repo/ref/privacy admission; metadata broker is not it",
    "evidence": ["publication-intent", "scoped-commit", "feature-readback", "pr-readback"],
    "recovery": "Unknown response stays unknown; reconcile same intent/target before retry; no second PR, force push, ambient PAT or operator GitHub substitute",
    "boundary": "PR creation is not delivery; narrower no-publish/local-only targets forbid these effects"
  },
  {
    "id": "checks",
    "obligation": "All required exact-head CI, independent review and current-base compatibility; repair concrete CI/conflicts within retained budget",
    "owners": [378, 377],
    "status": "unavailable",
    "source": "scripts/verify-ci-evidence.py",
    "symbol": "head",
    "missing": "Contribution runtime checks/review/base adapter; host release-evidence script is only a policy foundation, not a shipped PR runtime adapter",
    "evidence": ["exact-head-ci", "required-review", "current-base"],
    "recovery": "Code/conflict change returns to build/freeze/test/review and exact new-head checks; infrastructure rechecks have separate finite budget",
    "boundary": "Older-head green, missing/skipped shards, watcher exit and auto-merge enabled are not passing gates"
  },
  {
    "id": "merge",
    "obligation": "Permitted non-force protected-base merge; exact PR/merge/tree/base containment and applicable post-merge readback",
    "owners": [378, 377],
    "status": "unavailable",
    "source": "internal/credentials/broker/model.go",
    "symbol": "github.get_repository.v1",
    "missing": "Closed merge/readback adapter with fresh permitted actor/head/base/review admission and unknown-effect reconciliation",
    "evidence": ["merge-intent", "merged-pr", "tested-tree-integration", "base-containment", "post-merge"],
    "recovery": "Read exact remote after unknown merge; confirmed merge is irreversible history; failed post-merge validation is merged-but-unresolved, linked admitted correction not merge replay",
    "boundary": "Squash SHA need not equal candidate SHA; prove tested-tree/current-base integration; no-merge forbids this effect"
  },
  {
    "id": "dispose",
    "obligation": "Independently reload joined evidence; authoritative terminal disposition, truthful issue reconciliation and retrievable retained-session result",
    "owners": [379, 378, 377],
    "status": "unavailable",
    "source": "internal/persistence/fleet/badger/completion.go",
    "symbol": "Disposition",
    "missing": "Closed whole-contribution verifier/disposition and retained result delivery over all exact candidate, role, policy and GitHub evidence",
    "evidence": ["joined-verification", "contribution-disposition", "issue-readback", "retained-result"],
    "recovery": "Lost notification retrieves same persisted result without repeating effects; partial scope leaves broader issue open; corrupt/missing evidence denies success",
    "boundary": "Queue success, CI or merged PR alone is not whole-contribution success"
  }
]
```
<!-- /contribution-matrix-v1 -->

## #377 executable continuity source mapping

The retained continuity seam is implemented in `internal/app/contribution_resume.go`,
`internal/app/contribution_execution_observer_linux.go`,
`internal/app/contribution_continuity_join.go` and
`internal/app/contribution_effect_admission.go`; manager routing is in
`internal/managergateway/contribution.go` (`/contribute resume [reference]`).
Protected role-provision delivery composition is
`internal/app/contribution_decision_delivery.go` ->
`internal/api/contribution_decision_delivery.go` -> pinned companion launcher ->
canonical decision/plan/consumed approval/verified receipt readback.
See [current implementation guide](../docs/continuity/contribution-continuity.md).
This maps source and executable seams only: it does not promote the matrix to
#379 completion, real-provider qualification or installed/live acceptance.

## Sequence, authority and recovery policy

Normal path follows matrix order. `repair` is an evaluated checkpoint: when no
repair is needed record that fact with lineage/counters, not a fictitious failure.
On concrete test/security/launch/CI/conflict failure, preserve the failure packet
(candidate, criterion, command, expected/actual, exit, sanitized evidence, class,
minimal repair and invalidated-proof scope); return to **the same** builder at
`build -> freeze -> test -> repair -> review`, then resume remaining delivery.
A new candidate consumes the retained candidate budget, including CI/conflict
repairs. Earlier records are immutable. Do not append a fake PASS over a failure.

The engineering lifecycle maps here, not in a competing procedure:
`prepare_feature_worktree` = prepare; `understand`, `inspect`, `trace` = prepare;
`implement_minimally` = build; `test_the_behavior` = freeze/test;
`diagnose_and_repeat` = repair; `review_security_boundaries`, `review_launch_assets`,
`final_verification` = review; `push_feature_branch` = publish; `watch_cicd` = checks;
`merge_on_success_or_loop` = checks/merge/repair. `report_concisely` is progress
before publication if needed, with authoritative final reporting only at dispose.

Every boundary requires current authenticated principal/stable owner, exact
immutable Loop/profile/contract, repository/base/ref, Agent/charter/runtime and
separately admitted role authority. Role references are not role execution.
Controller, builder and tester do not inherit or union mandates/stanzas or controller
credentials. Material authority/stanza changes require normal fresh admission and
clean runtime, with compatible append-only role/continuation bindings preserving
the original immutable RuntimeBinding. No new Agent/Queue task per activity.
Native build/tests run repository code: isolated homes/worktrees are **not a host
sandbox**. Unsupported languages/tools/journeys deny qualified admission rather
than fall back to file presence. Protect trusted tests/module/check policy from
builder edits and tester source writes by actual capability enforcement.

| Condition | Required retained outcome and recovery (#377 unless otherwise noted) |
| --- | --- |
| Ordinary success | Actual checkpoint/effect evidence advances only its own stage, never the entire contribution from a primitive receipt. |
| Concrete product failure | Same builder/root/worktree/branch/PR repairs; rebuild/refreeze/fresh retest; preserve failure, scope and counters (#376/#378). |
| Genuine authority block | Hold exact stage; explain purpose/identity/scope before supported protected decision; read actual decision; zero forbidden effects. No duplicate task consent. |
| Unanswered / denied / expired | Distinct pending/denied/expired records; delivered request is not approval; no silent renewal, channel/account switching or unchanged redispatch. |
| Dialog cancelled / run cancelled | Dialog dismissal cancels that decision only. Whole-run cancellation fences new effects, requests bounded shutdown and requires durable exact stop/quiescence acknowledgement before custody release. HTTP abort, dead/reused PID or lease expiry is not stop proof. |
| Unavailable / timeout-no-decision | Record whether delivery occurred; no nonexistent pending-human claim, approval configuration changes, fake human terminal or tool bypass. |
| Unknown source/GitHub effect | Preserve durable reserved intent, semantic payload/expected target, stable key and consumed budget; quarantine conflicting writes until authoritative same-target readback (#378). No cross-store/external atomicity claim. |
| Disconnect / expiry / reattach | Disconnect neither cancels nor renews. Continue only while exact authority/custody permits; otherwise quiesce. New authenticated same-owner session may observe; supported explicit resume admission is required for fresh role authority, not renewed task consent. Different-principal transfer denies. |
| Service/runtime interruption | Reconstruct immutable phase/candidate/intents/results and counter history; exclude prior claimant with fenced epoch and exact process/stop evidence before replacement. No second writer or second daemon. |
| Budget/deadline exhaustion | Evidence-backed non-success with phase/counters/partial effects; no replacement root, hidden extra attempt or zombie churn. |
| Post-merge failure | MERGED but unresolved; preserve actual merge and issue truth. Linked corrective contribution requires separate ownership/admission, not replay (#378/#379). |

Distinct finite counters: Queue claim attempts; candidate attempts/repairs;
runtime turns; decision-delivery attempts; infrastructure readiness rechecks;
absolute deadline. Report each independently. #373 proposes at most three total
candidates including initial and two infrastructure rechecks; a stricter admitted
budget wins (current intake admits at most two). Engineering retry allowances
do not expand product limits. A new SHA, runtime,
Queue attempt, phase or reattachment cannot reset consumed counts. A terminal
Queue run cannot be reopened merely because a candidate repair is desired.

Publication keeps one repository/base lease through push/PR/CI/review/merge/readback,
not just a process-local source lock. Verify author/committer, current effective
actor/permissions, approved repository/base/ref, staged allowlist and public-text
privacy immediately before effects. Never stage an entire dirty worktree or expose
PATs/private source/session/approval canaries. Re-read latest owner instructions
before publication and merge. Local tested head = remote feature head = PR head;
all required checks/reviews must finish on that exact head with current-base
compatibility. Missing/skipped/cancelled evidence denies; non-force integration
preserves both scopes and retests invalidated proof. No protection/admin bypass.

## Freeze, independent acceptance and completion evidence

Freeze full tracked/staged/unstaged/new source with type/mode/byte hashes; include
referenced ignored/generated inputs, lockfiles, compiler/toolchain/dependency
identity, build flags/environment policy, actual executable bytes, base/candidate
commit/tree, inventory inclusion/exclusion policy and immutable trusted acceptance
policy. Exclude only declared non-source evidence/cache/runtime outputs; fail on
missing required inputs, unsafe files or source-affecting excluded drift. Keep
secrets out. A dirty development candidate needs full inventory, not a false clean
HEAD claim. Before commit and publication, bind accepted inventory/tree to actual
commit and repeat invalidated proof. Test before release.

Tester is a fresh distinct session/runtime with exact Agent/charter/authority,
no product-code editing rights, and only isolated test/evidence writes. Verify
candidate and executable before **and after** testing. Exercise applicable real
CLI/API/browser/denial/persistence/restart/recovery journeys. Report criterion,
commands, exit, environment, expected/observed, evidence and limitations. PASS,
FAIL, BLOCKED, UNAVAILABLE and downstream NOT EXERCISED stay distinct; prerequisites
skipped are uncovered acceptance, never PASS. Builder cannot mint tester receipts,
edit trusted policy or manually create expected runtime output.

Required immutable joins: same root/Queue claim/attempt/contract/Loop/profile;
exact builder/tester role/session/authority; candidate inventory/generated inputs/
binary/toolchain; criterion/check/verifier policy; ordered remote effect intents/
actor/targets/readbacks; final tested/published/PR head; actual merge/tree/current
base containment/post-merge checks; scope-correct issue state and persisted result.
Independently reload referenced artifact contents and receipts, verify content
addresses and all bindings before authoritative completion. Digests/logs alone
are not behavior evidence. A repository test-only policy checker cannot mint a
product receipt or replace #379's closed verifier.

### Three separate claims, no automatic promotion

1. **Selected-file Doer** (v4/v5): exact selected policy (presence, text or bytes),
   admitted paths/attempts, runtime-produced artifact/receipt/disposition. It does
   not prove directory state, compilation, independent tester, PR or merge.
2. **Trusted native coding**: qualified language/check/edit scope and immutable
   trusted acceptance, real RED→GREEN, exact native policy/receipt. A Go checker
   is not a universal language verifier, independent contribution tester or GitHub
   delivery. Do not upgrade a historical weak verdict with later manual work.
3. **Whole contribution**: every required matrix stage, real affected behavior,
   candidate-bound reviews, exact-head gates, permitted protected merge/readback,
   joined independently validated disposition and retained result. Selected-file
   or native receipt, model PASS JSON, build, PID, Queue presence, PR creation and
   CI alone are rejected substitutes. A narrower target reports its own boundary,
   never whole-contribution merge success.

Evidence tiers remain distinct: specification; observed source implementation;
controlled/synthetic qualification; real managed-provider development proof;
revision-matched package; installed target; active target; accepted live target.
No source fixture, archive, manual engineering lifecycle, old verdict or healthy
listener upgrades another tier. Source capability without qualification is
implemented-but-unqualified, not shipped-and-accepted.

## Integration and acceptance ownership

- #375 consumes resolve/seal and declares truthful stage/checkpoint interfaces;
  intake admission cannot close the journey. #376 consumes prepare through review;
  #377 applies the recovery/authority table to every stage. #378 consumes the
  accepted frozen candidate and owns publish/checks/merge and issue readback.
- Builders and independent testers for **each** #375–#379 slice must read this
  matrix before defining acceptance. Report each row's observed operation or exact
  missing seam, evidence tier, IDs and recovery. Omission or weak substitution
  fails slice acceptance; unavailable downstream rows remain explicit, not falsely
  claimed implemented by that slice. Run architecture regression tests below.
- #379 is the mandatory final behavioral join: actual Aegis-managed runtime with a
  nontrivial real defect, trusted RED/GREEN, deliberate same-root repair and real
  permitted GitHub protected merge, retained result and applicable recovery/
  negative rows. Exercise duplicates, ownership, wrong run/role/candidate/policy,
  stub/model PASS/edited tests, stale evidence, denied/expired/unavailable decisions,
  disconnect/reattach/crash, cancellation/unknown effects, exhausted budgets,
  CI/conflicts/base advance, post-merge failure, narrower scope and privacy canaries.
  No operator completion or fabricated expected output. Missing proof blocks its
  completion, not creation of this upstream contract.
- Sequence: #386 contract -> slices adopt -> #379 actual joined proof -> #348 final
  revision-matched discovery/package/fresh adoption and #352 integrated release/
  installed qualification. Preparation may proceed independently; reconcile active
  branches before acceptance, regenerate manifests and retest only invalidated
  proof. Do not make #386 depend on #379 or serialize unrelated writers.
- #348 owns final distribution. Source skills reference this repository contract
  without pretending it is already bundled or installed. Existing manifest refresh,
  validation/evaluation and archive integrity mechanisms remain in force. Fresh
  effective AGENTS context loading and new-session adoption must be independently
  verified; `/reload-skills` alone does not prove adoption by existing workers.
- #352 cannot close with required #379 proof missing or contribution stages
  unavailable. Installed/release-target proof requires exact named executable,
  bundle, runtime/config, Loop/roles/policy, protected identity and readbacks.
- #351 qualifies the applicable two-participant controller-mediated fleet outcome;
  it does not establish independent profile authentication or installed release
  acceptance. #352 consumes that pilot plus #348's matching guidance and #379's
  complete contribution proof; none substitutes for the others.

Run `GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/architecture -count=1`.
Its mutation-tested repository-only validator checks stage order, owner/evidence
mappings, source anchors, consumers and synthetic proof joins; it is **not** a
runtime verifier or actual managed/GitHub qualification. Preserve existing
skillbundle validation/evaluation after manifest refresh.

The historical pre-merge reusable Doer hard requirement remains mandatory:
through supported skills/owning-service interfaces, separately publish and Run the
**same exact immutable revision/digest**, real runtime creates predeclared output
within approved paths/attempts, independently read actual output plus artifact,
PASSED receipt and SUCCEEDED disposition; preserve subsequent task and cumulative
failure recovery. Follow [acceptance port isolation](acceptance-port-isolation.md):
the owner amendment naming development 8448 now means Matt/Xander clean-main
integration (actual backend 8098), never candidate activation. Builder acceptance
dynamically claims 8450–8469 in isolated state; historical installed 8446 remains a
different target. These location rules do not weaken any product criterion or grant
authority. Reconfirm the authorized target. When a current request prohibits live Loop
execution, report that gate as NOT EXERCISED; the parent coordinates existing-owner
acceptance or obtains explicit owner revision before claiming the gate passed.
Do not run a Loop, spend another owner's runtime budgets, restart a service or
manually create expected output merely to make documentation green.
