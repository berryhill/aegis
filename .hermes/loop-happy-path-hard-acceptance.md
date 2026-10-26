# Hard acceptance prompt: publish and execute an exact Doer Loop

Establish and exercise a hard end-to-end acceptance case for Aegis Loops: an authenticated agent creates, validates and publishes an immutable Doer Loop, then separately executes that exact published revision through Aegis and independently verifies its real output. This is product acceptance, not a schema demonstration, standalone Doer run, manually created file or synthetic-runtime success claim.

This document defines acceptance; saving it does not authorize execution, implementation, authority changes, release or deployment. Use it under the operator's separately requested scope. It is the single-Loop counterpart to `graph-happy-path-hard-acceptance.md` in this directory.

## Target and discovery

For source-candidate testing first read [acceptance port isolation](acceptance-port-isolation.md).
Candidates dynamically claim 8450–8469; main-only 8448/8098 is reserved for
Matt/Xander clean-main integration. The installed URL below is historical target
context, not a candidate default. Location does not change any criterion below.

Target deployment:
https://silas-workstation.taild7c550.ts.net:8446/console

Use the current installed workstation version first; do not assume a version from historical conversation. The URL selects the owning deployment. Use installed Aegis skills and supported typed CLI/owning-service interfaces—not browser automation, invented clients, direct-store writes or hand-built orchestration.

Load aegis first, then relevant Registry, Loop, Queue, evidence, approval and lifecycle skills. Confirm installed command help and supported request schemas before preparing inputs. Read repository AGENTS.md, specs/MVP.md and the relevant delivered Doer documentation when source inspection is available. Distinguish specification, source implementation, installed capability and observed live behavior.

## The hard acceptance case

Prove TWO distinct actions:

1. An authenticated agent creates a bounded Doer Loop for registered Agent Xander, validates it and publishes one immutable revision. Read back the exact revision, digest, validation and publication provenance.
2. In a separate execution action, the agent runs that exact published revision through Aegis and independently verifies the output, artifact/verification chain and terminal disposition.

Do not collapse these into a combined create-and-run action as the sole evidence. A combined retained-task convenience operation may be tested additionally, but cannot substitute for proving separate publication and execution. Do not activate or execute merely to prove publication; explicit Run intent authorizes the execution action under its existing independent gates.

## Exact task contract

- Task: write result.txt containing exactly the five UTF-8 bytes hello, without a newline. Write no other file.
- Agent: the exact registered, enabled Xander revision returned by authenticated readback, never a substitute Agent selected to escape admission.
- Workspace: an existing operator-approved isolated workspace; result.txt must be initially absent. Record the initial state before any runtime effect. Do not delete or overwrite an existing result to manufacture this precondition. If it exists, reconcile prior execution and obtain an appropriate fresh approved test workspace rather than destroying history.
- Writable files: only the relative path result.txt under that exact workspace.
- Verification: exact raw UTF-8 bytes, not trimmed text, mere file presence, model judgment or process exit. Use the installed exact-byte contract when supported; if unavailable, report the missing capability rather than silently weakening the assertion.
- Budget: at most two implementation attempts, enforced by the Aegis execution contract. Distinguish inner implementation passes from Queue attempts; an outer retry must not silently reset or multiply the task's authorized implementation budget.
- Neither builder, tester nor controller operator may manually create the expected output. Controller-applied edits produced by the admitted runtime are allowed only through Aegis's normal bounded execution mechanism.
- Use the configured, approved real runtime/provider for live acceptance. Synthetic runtime fixtures may prove isolated regressions only; they do not establish live-provider success.

## Publication boundary

Resolve the current authenticated principal, exact Agent revision, stable owner and charter references through supported product readback. Do not infer authority from the Hermes profile name, prompt or display identity.

Create the selected-file Doer candidate through the shipped builder and strict schema. Pin the task, workspace, allowlist, exact-byte verification and attempt bound before execution. Require authenticated structural validation and exact immutable publication readback. Preserve the service-returned digest and provenance; a locally computed digest alone is not publication proof.

Publication grants no runtime effects, provider credentials, host-write access or provisioning. An inactive published definition is publication success only—not task completion.

## Execution and independent verification

Use the supported single authenticated execution action for the exact published Loop and exact Agent references. Resolve internal Graph/Queue/session mechanics through Aegis, not operator-carried binding files or separate hand-operated worker commands.

Trace and prove:

1. Fresh authenticated authority selection under one exact trust context.
2. Approved provider use and controller-owned provider authentication, without exposing credentials to the Agent or test evidence.
3. Exact approved provisioning/runtime preparation and bounded host-write authorization.
4. Lifecycle/Graph/Queue admission preserving the exact published Loop revision and digest.
5. The real managed runtime produces the allowed edit within the implementation budget.
6. Independent selected-file verification observes the actual resulting bytes.
7. Artifact and verification evidence bind this exact run, Loop execution, attempt, producer and authority context.
8. Content-addressed artifacts and receipts are independently reloaded through supported controller verification before successful disposition.
9. Authoritative post-execution readback agrees with the terminal result. Independently inspect result.txt read-only and confirm its raw bytes. Report any inspection dimension unavailable through the supported interface; do not invent missing records.

A successful API response, ready model, helper probe, active lifecycle, queued item, worker PID, runtime narration or passing unit suite does not establish success.

## Identity and recovery invariants

- Preserve the immutable Loop identity and revision between publication and Run. Never publish a replacement Loop, change its definition or substitute a successor Agent just to evade a failure.
- Reconcile existing retained tasks, publications, execution intents, Queue records and workspaces before creating or dispatching anything. Historical IDs are discovery anchors, not evidence of current state or authority.
- Preserve stable intent/idempotency identities across uncertain transport outcomes. Read authoritative history before retrying. Do not rotate request keys to make an unknown result appear new and successful.
- If independent setup genuinely changes a binding, expose that incompatibility honestly. Do not call execution of a new publication success for the originally published Loop. A fresh separately reviewed case does not erase the original blocked case.
- Do not silently remove credential scopes, reinterpret an approved charter, union stanzas, copy host auth files, change model/provider configuration or broaden host-write authority.
- Distinguish existing approved authority not carried through, genuinely missing authority, provider-authentication failure and incorrect authority modeling. A credential-scope denial before provider checking is not evidence of absent provider authentication.
- Approval, provisioning and host-write gates are independent of task consent. Present actual required decisions through the supported human-controlled surface; never operate human approval controls or fabricate approval receipts. Do not ask for duplicate Run consent when the original bounded Run remains authorized.

## Hard negative tests

Use isolated test state and controlled fixtures; never corrupt production records or the retained live workspace to manufacture a negative test. Prove fail-closed behavior for:

- Missing result.txt, incorrect content, empty content, trailing newline, extra whitespace and invalid UTF-8.
- A symlink or path traversal escaping the approved workspace, including an output path changed between admission and effect/verification.
- A runtime proposal attempting to write any file outside the exact allowlist.
- A model claiming success without producing the file: no successful terminal disposition.
- Repeated incorrect output: no more than the authorized implementation budget, and no verifier-bypassing success.
- Missing, stale, revoked, expired or mismatched authority at admission/effect/verification boundaries: no unauthorized effects or false successful disposition. Report the actual durable outcome instead of assuming every stage has identical failure semantics.
- Missing provider authentication versus prohibited Agent credential scope: distinct authoritative reasons, with unchecked stages labeled not_checked rather than failed.
- An artifact or receipt from another run, Loop revision, execution, attempt or authority context—even when its content bytes match the expected bytes.
- Artifact content or selected-file substitution after observation: reject inconsistent evidence, never reuse a stale passing check as final proof.
- Missing, failed, duplicate or mismatched required verification evidence: no successful disposition.
- Repeated or concurrent submission of the same execution intent: no duplicate accepted run/effects or reset implementation budget.
- Interrupted/unknown execution response: authoritative same-intent reconciliation rather than blind replay or a replacement publication.

Where testing a boundary requires synthetic injection or an interface is absent, state that evidence tier explicitly. Isolated negative tests do not replace the real-provider positive acceptance run.

## Historical regression anchor—not current fact

A prior workstation 0.2.39 observation found the original Loop published and structurally valid, but execution returned independent_setup_required with doer_agent_credentials_denied. Agent/model/tool-free checks passed; provider authentication and subsequent runtime prerequisites were not checked; no Queue attempt ran and result.txt remained absent.

Use this only to reproduce the relevant regression against current evidence. Do not assume the installed version, charter contents, provider authentication, gateway readiness or retained identities are unchanged. Diagnose any gateway-readiness failure separately rather than attributing every denial to it.

## Execution discipline and implementation boundary

First exercise the installed product when execution is authorized. A missing capability or independent authority gate must produce an exact BLOCKED or UNAVAILABLE result, not a simulation or fabricated receipt. Do not use standalone Doer, manually coordinated commands or direct-store fallbacks to manufacture the outcome.

If implementation is separately authorized, use the corrected aegis-implementation-model and aegis-development-loop: establish an execution-capable conductor and approval channel; preserve the existing task/session/history; use one source-owning builder, freeze the candidate, and independently test it. Rendering run_task_flow instructions is not execution. Do not redispatch an unchanged single-query approval failure or weaken approvals. A shared-runtime owner request is not the product repair or acceptance result.

Test the source candidate before requiring a release. Keep source implementation, independent candidate acceptance, exact-head CI, verified merge, release publication, supported workstation update and live acceptance separate. Complete only those delivery targets actually authorized; never claim a local or synthetic proof is installed acceptance.

## Required final evidence

Report PASS, FAIL, BLOCKED or UNAVAILABLE for each criterion, distinguishing NOT EXERCISED downstream stages from tested failures. Include:

- Installed CLI and running-gateway version/source provenance and separately observed readiness.
- Exact Agent and charter references, stable owner and authenticated publication provenance.
- Exact published Loop ID, revision, digest, validation and lifecycle.
- Exact bounded task contract and pre-run absence evidence.
- Distinct publication and execution operation identities and their authoritative readbacks.
- Execution intent, internal Graph/Queue/run/Loop-execution identities where exposed.
- Runtime/provider identity and preparation evidence, without secrets.
- Implementation pass count, Queue attempt count and proof of budget enforcement.
- Independently observed raw output bytes and allowed-path effects.
- Artifact/content references, verifier identity/policy, receipt outcomes and causal bindings.
- Evidence-backed terminal disposition and matching post-execution readback.
- Negative-test results, reproduction commands, evidence locations and evidence tier.
- Any missing adapter, unexercised stage, unknown transport outcome or independent gate.

The case passes only when an authenticated agent has separately published and executed the same immutable Doer Loop, the real Aegis-managed runtime has produced exactly the authorized output within bounds, and independent verification plus authoritative disposition prove the entire chain. Do not substitute readiness, publication, a release or narration for this result.
