# Implementation and delivery contract

## Meaning of owner requests

- **Implement / fix:** deliver the requested working user-visible behavior end to end, with regression evidence, independent acceptance, a task-owned feature branch/worktree, PR, exact-head CI and verified merge to the protected base. Do not stop at local edits, an open PR, a worker launch or a plan. A document-only request means the actual requested documents, not an unsolicited product implementation or release.
- **Ship / release:** complete the established release workflow for the named artifact, including publication and exact canonical artifact readback, when that is the owner's requested target. Do not reinterpret a named target or invent a release destination.
- **Make it work on my workstation / upgrade:** carry the explicitly named deployment through the supported installed update/activation journey, necessary supported recovery within scope, running-service provenance and real end-to-end acceptance. A replaced binary alone is not a working upgrade.
- **Discuss / review / research / plan:** return that requested analysis or artifact; do not infer implementation or activation permission.
- **Narrower explicit scope wins:** local-only, source-only, no-queue, no-publish, no-release, timebox and named-path restrictions override defaults. Later corrections constrain the current objective; do not revive stale work from history.

Plain implementation includes verified merge, not an unsolicited release or live activation. If the current task explicitly includes release or activation, complete those already-authorized stages without asking for duplicate permission. Product-level standing direction does not supply missing credentials, exact deployment targets or independent approvals.

## Working behavior and acceptance

Define the domain obligation, observable journey, starting state, bounded scope, denial behavior and evidence before editing. Reproduce reported failures and add a regression. Implement the actual integration, not a stub, mocked success, hardcoded expected output or manually coordinated substitute for missing product behavior.

Read the applicable hard acceptance contracts indexed in [README.md](README.md). Preserve exact immutable identities and required authority/evidence. Mark scope coverage honestly: implemented-and-tested, implemented-but-live-proof-pending, or not implemented. Tests prove only what they actually exercise.

Test the built candidate before requiring a release. Freeze source/executable identity and have a fresh independent tester exercise affected integration journeys in isolated state. Use one source-owning builder; tester does not edit product code. Fix concrete acceptance/security failures and rerun affected proof. Synthetic runtime qualification and passing unit tests never substitute for required real-provider or installed-target acceptance.

Do not use speculative polish or repeated still-valid full suites to delay green delivery. Preserve mandatory regression, race, security, provenance and CI checks. Reuse evidence only when its candidate, command, scope, toolchain and relevant environment match.

## Execution, continuity and ownership

Act on explicit implementation authorization: read relevant source, edit, test, debug, publish and merge within the current scope. Do not ask “shall I continue?” between ordinary authorized stages. Skills, audits, plans, workers and handoffs support delivery; none substitutes for it.

For engineering work use the installed Aegis engineering skills and a real execution-capable conductor. Loading a skill or rendering `run_task_flow` instructions is not execution. Preflight approval delivery before dispatch. Detached one-shot workers do not inherit the attended parent's approval capability; never repeatedly dispatch an unchanged approval-transport failure or bypass a tool gate.

Preserve existing task/session/history, worktree, branch and PR across retries where supported. Check ownership and live workers before taking over; do not overlap writers or overwrite unrelated dirty work. A newer follow-up adds constraints unless it explicitly replaces or cancels the objective. Do not substitute a different Agent, Loop, Graph, target or acceptance case to escape a failure.

A shared-owner or external gate blocks only the portion that requires it. Complete independently authorized work, preserve one exact owner-directed repair request when needed, and distinguish submission/acknowledgment from accepted substantive execution and verified repair. Do not manufacture target-side authority or edit another owner's state.

## PR and merge closure

1. Work from freshly fetched protected-base source in an isolated feature worktree; keep the canonical checkout and unrelated work intact.
2. Review the final scope and launch-asset impact. Stage only the intended files; preserve private/ignored state and secrets.
3. Verify the intended GitHub actor and exact repository access using approved profile-scoped credential custody before writes. Never borrow another identity.
4. Commit, push and create/update the task-owned PR. Keep its body consistent with current scope, tests and remaining acceptance boundaries.
5. Require local head, remote feature head and PR head equality. Observe required CI on that exact head, independent review where required, clean mergeability and current-base compatibility. Missing checks are not passing checks.
6. Repair concrete CI or merge conflicts within scope using supported non-force operations, reverify the resulting head, and merge on green without bypassing rulesets, review holds or protected controls. Existing merge authorization needs no repeated consent.
7. Read back PR MERGED, merge SHA and inclusion on the remote protected base. Verify applicable post-merge CI; a feature-head pass is not evidence for a failing base-head run. Preserve an honest pending state while checks run.
8. Reconcile existing task/flow/dependency/issue state only from real delivery evidence through supported owner APIs, when such records belong to this task. Do not create bookkeeping work or close a broader issue merely to describe a smaller merged slice as complete.

## Release and live activation closure

Keep these states separate:

- **Merged:** requested source is on the protected base.
- **Released:** exact verified release artifacts are published.
- **Installed:** the intended target has the intended executable/artifact.
- **Active:** the running service uses that exact executable/artifact and configuration.
- **Accepted:** the requested real user journey passes on that target with required evidence.

Only perform release/update/deployment when requested by the current contract or explicit applicable standing authorization. Use the established repository/product workflow, approved custody and named canonical destination. Verify the exact published artifact and running-target identity/readiness; do not replace supported updates with parallel manual installs. Test prompts, release tags, local hashes, PIDs and HTTP reachability alone are not installed end-to-end proof.

Payments, new billable services, production-secret changes, destructive work, new authority and unrelated infrastructure remain separate gates. Preserve protected authentication, tool approvals, charter/provisioning/host-write consent, CI and deployment controls. A timeout, undeliverable approval and human denial are distinct; none permits alternate-tool bypass.

## Completion report

Lead with what now works and where it was delivered, then provide the PR/merge or artifact URL, exact revision and relevant verification. State any remaining blocked, unavailable or unverified requirement and the smallest genuine next gate.

Do not say done/fixed/deployed based on a plan, task status, worker verdict, passing sample or incomplete acceptance subset. Continue while the requested target is unverified and the next action remains authorized and available. Do not endlessly repeat unchanged failures; diagnose a materially different cause or identify the one actual independent gate.

Instruction changes require fresh effective-loader verification and a new-session adoption boundary. `/reload-skills` alone does not establish that changed repository instructions or running workers were refreshed.
