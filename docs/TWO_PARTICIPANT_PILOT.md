# Two-participant controller-mediated pilot

Qualification recipe for #351, not a record of a passing live pilot. Follow
[the contribution delivery rules](../.hermes/implementation-delivery-contract.md)
and [the reusable Doer hard acceptance](../.hermes/loop-happy-path-hard-acceptance.md).
Use this with the delivered revision-matched skills; #348 owns their packaging
and discovery, #350 owns essential online adapters. Do not install an unaccepted
branch or describe help presence as proof of an owning-service adapter.

## Approval and custody before live effects

Record an explicit owner selection of exactly two existing fleet participants and
exact skill installation destinations. For each participant review canonical
Agent ID, fleet/kind/source ID, exact charter ID/revision/digest, runtime
adapter/runtime/target, stable owner and accountability ID, lifecycle and
capability/policy declarations. A profile directory or metadata selector is not
approval, authentication, canonical registration or a runtime grant. An unanswered
decision is pending/no decision, not denial or approval.

For named profiles use canonical fixture registration, not profile import. Do not
read or copy private prompts, tools, memories, sessions, credentials or persistent
profile configuration. Disposable admitted execution homes are not persistent
participant identity. Neither two names nor two Runs establishes independently
authenticated profile authority. Controller transport authenticates its configured
principal; `agent_id` is a selector. Do not copy controller/provider credentials
or configuration into either participant.

Within approved destinations only, inspect installation inventory, aliases and
local edits before invoking the supported bundle installer. Shared-root aliases,
foreign ownership or local drift must not be overwritten. Use #348's verified
archive, supported profile-home semantics, inventory and enabled discovery, then
fresh sessions/effective skill loading for adoption. An existing session or a CLI
update alone does not prove new skill adoption. Do not substitute direct copies
or a mutable hub update for the matched archive.

An existing populated `skills` directory is not a managed-install destination.
Use a profile-private managed home beside it, preserving the original directory
and any shared-tree aliases. Before enabling the managed generation through the
profile's `skills.external_dirs`, check for the same official suite names in the
existing discovery roots. Installing beside old copies can make bare skill names
ambiguous even when managed inventory passes. Verify actual fresh `skill_view`
resolution for every suite member, not just installation or a listing. If this
addition introduces ambiguity, restore only the newly changed discovery setting
to its verified original bytes, retain both generations, and report adoption as
BLOCKED. Moving older copies outside discovery requires an explicit recoverable
archive choice with original paths, hashes and restore metadata; never silently
overwrite, delete or hide differing local content to manufacture adoption.

Coordinate acceptance location with the current service owner and effective
repository port policy. On the designated workstation, 8448 and its 8098 backend
are reserved for clean-main integration, not candidate activation. Candidate
services use an available loopback port in 8450–8469, with the permanent port
lock held for the entire foreground service/test lifetime and actual listener
availability checked inside that lock. No fixed issue assignment, lock-file
replacement, foreign listener termination or new tailnet/public exposure.
Consult the owner coordination policy and shipped repository helper when available;
this document is not another port allocator. Keep worktree, configuration, state,
workspace and authority isolated; do not copy main configuration or credentials.
A Console URL selects the instance; it does not authorize browser operation,
gateway replacement/restart or configuration changes. Match candidate CLI, owning
configuration and target through the supported Unix transport. A mismatch or
missing adapter is BLOCKED/UNAVAILABLE, never permission for a second store writer,
token extraction or an ad-hoc authenticated client.

## Registration and workspace qualification

Load `aegis`, Registry, Loop, Queue, evidence, approval/provisioning and lifecycle
skills in fresh participant sessions. Confirm installed command help before
preparing strict requests. Use the matched executable with
`--config OWNER_CONFIG --target CONSOLE_URL` for owning-service operations below;
these variables identify approved artifacts, not credentials.

1. Read `agents list`, then exact `agents show AGENT REVISION` and `agents history
   AGENT` for existing registrations. Never substitute mutable latest for an exact
   approval binding.
2. After exact registration approval, use `agents register FILE` with the strict
   embedded canonical fixture and fleet/source selector. The charter must already
   exist through its supported approved workflow. Preserve `{agent, created}` and
   exact immutable readback. Replay the identical registration; require
   `created: false`, unchanged identity/source/charter/runtime/ownership and digest.
   Conflicting registration must deny without overwrite; exercise deliberate
   conflicts in isolated state, not by inventing live participant bindings.
3. Publish and mutate an owned definition under authenticated workspace authority.
   Inspect service-derived publisher, principal and stable owner provenance.
4. Under another admitted context with a genuinely different recorded owner,
   attempt revision/lifecycle takeover in isolated qualification state; require
   denial and unchanged owner/history. Two participants sharing one owner cannot
   satisfy this negative. Use existing authority; do not fabricate another
   principal, owner or grant to make a live test pass.
5. Read/reference the shared immutable definition and use it with the exact admitted
   participating Agent. Sharing grants neither mutation rights nor runtime
   authority. Preserve normal enabled/stale/participation checks and zero authority
   union. Workspace authority alone cannot claim or execute Queue work.

## Two distinct tasks, one reusable immutable Loop

Use `loops doer-reusable FILE --output NEW_FILE` for a local typed v5 candidate,
then authenticated `loops validate FILE`, `loops publish FILE` and exact
`loops show LOOP REVISION`. The local builder is not publication or execution.
Preserve returned validation, digest, publisher and immutable provenance.

For each approved participant use a separate authenticated `loops queue FILE`
action binding the same exact Loop reference, that participant's exact Agent
reference, a stable distinct task intent key, normalized typed inputs and explicit
activation only when authorized. `QueueLoopInput` in `internal/app/loop_queue.go`
is the source contract: `agent`, `loop`, `idempotency_key`, optional `inputs`,
`activate`, `queue_item_id` and supported reexecution binding. Use the matched
skill's actual request contract, not an invented command/schema. The owning
service supplies direct submission, preparation/binding and foreground execution;
no manually authored one-node Graph, per-task republication or separate operator
worker is part of this recipe.

When the matching source-candidate service returns `setup_review_task` for a
missing exact provisioning or host decision, keep its returned ID/version and
original normalized `host_run`. Request the existing independently operated
`loops setup-protected` action for the required `provision` or `host` review;
never operate human authentication/approval controls or use `setup-approve` from
an agent terminal. Verify the exact receipt/host fact, then resume the unchanged
`loops queue` payload/key. Review retention is not a grant or executable Queue.
The different-owner path requires separate provisioning and signed exact scope;
it does not inherit the owner-principal shortcut. Older services lacking this
adapter remain unavailable. This recipe does not claim that a source candidate
or installed advisory bundle is already live-provider-qualified.

Freeze the two task contracts and allowed paths/budgets before execution. Tasks
must be distinguishable and their actual runtime results causally verified. These
two v5 Runs prove reusable selected-file behavior only. Current v5 inputs are
closed to task/workspace/writable_files/verify_file/optional expected_text;
`NormalizeDoerBinding` does not populate a `coding` contract. Native coding
through that shared v5 revision is UNAVAILABLE, not merely awaiting host approval.
Do not add an invented coding input or relabel selected-file output as native proof.

The existing-repository criterion is an ADDITIONAL, separately approved v4 native
case for at least one of the same two participants, using the supported additive
`coding` contract in [DOER_LOOP.md](DOER_LOOP.md#bounded-repository-coding-additive-v4).
Record its separate exact v4 Loop/Run references and authority/budget. Require
actual bounded source/test context, a real defect, trusted RED before effects,
managed runtime production-file repair, unchanged trusted tests, trusted GREEN
and the exact native receipt. Reject a presence-only wrong-package/stub result.
This extra case does not prove native capability for v5 and cannot replace either
of the two same-v5 task Runs. If the owner's acceptance requires native coding
inside those same-v5 Runs, report that exact capability gate; do not silently
substitute v4 or waive the requirement. Do not reuse, reset or consume #385's
owner-deferred original case or its budget. Absence of a distinct approved v4
native case blocks the additional repository criterion.

For both Runs preserve the returned request, Queue, direct run and Loop execution
references. Inspect `queue show ITEM` and `queue result ITEM` when shipped by the
matched executable. Require actual content-addressed artifact, independently
reloaded passed verifier receipts, successful terminal disposition, exact
participant/Loop/attempt/authority causality, and actual output inspection within
approved paths. A process exit, model claim, Laya confidence, publication,
preparation record or healthy service is not success. Nobody manually creates
expected runtime output.

## Denial, interruption and budgets

Exercise deliberate negatives only in isolated state. Check unauthenticated/stale,
disabled, expired and revoked authority, cross-owner mutation, substituted
participant/Loop/artifact/receipt, changed-payload replay and exhausted bounds.
Require no unauthorized effects or successful disposition. Keep ordinary retry,
expired-lease reclaim and runtime-stop acknowledgment distinct; do not claim
cancellation proves durable runtime quiescence when the service does not.

After unknown transport outcome, read the same immutable request/Queue/history
before retrying. Recover the same intent, not a replacement publication or key.
Preserve failed history, consumed implementation passes and Queue attempts
separately, remaining cumulative budget and denial fences. A later distinct task
must not erase or reset an earlier failure. Never extend TTLs, renew authority,
reopen terminal history or waive a human gate to obtain PASS.

## Evidence and completion ledger

For each criterion record PASS, FAIL, BLOCKED or UNAVAILABLE, with downstream NOT
EXERCISED when appropriate, exact command/exit, identities and evidence paths.
Separate application-service regression, synthetic provider, packaged executable,
matched archive, installed/fresh-adopted skills and actual managed-provider proof.
Include:

- exact participant, destination and binding approval references;
- source/full inventory, executable and bundle identities;
- exact registration/ownership/sharing readbacks and negative outcomes;
- both tasks' same immutable Loop and distinct request/Run/Queue identities;
- artifact/content, receipt/verifier/policy and terminal-disposition references;
- implementation/Queue attempt accounting, replay and recovery observations;
- independent tester results and all missing gates;
- PR/exact-head CI/merge/main containment only when actually delivered.

`TestIssue351TwoRecordedOwnersSharedReusableLoopAdmission` qualifies the
application-service seam with isolated real persistence and synthetic registered
participants: different recorded owners, owned mutation/cross-owner denial,
shared same-revision direct admission, same-intent replay/budget-conflict denial
and absence of fabricated execution or published Graphs. It does not exercise a
socket, packaged CLI, live profile installation, runtime provider or completed
Run. Existing Registry, Queue lifecycle and runtime tests cover other isolated
boundaries; none alone closes #351. Do not close the issue until its full
applicable pilot acceptance is independently verified.
