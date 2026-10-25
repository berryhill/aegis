# Owning-path integration progress (not full #377 acceptance)

Changes in this pass are on the delivered #376 call path, not only isolated fixtures:

- Fleet construction installs `contributionExecutionObserver` on the real QueueWorker.
- `processContribution` observes the actual controller/builder/tester sessions and routes ManagedContribution native commands through that observer.
- ManagedContribution Git reads/worktree commands, candidate object commands and Go build/test commands all use the native port when the owning service installs it. Output limits, native environment and Go timeouts remain enforced.
- The observer appends initialization, queue-attempt consumption, a preparation checkpoint, independently checked role associations, incrementally registered command inventories, native reserve/completion/unknown facts and closure/real adapter-stop joins to the existing continuity store. It has no public arbitrary-command route.
- Native process cancellation kills the owned process group, not only the leader. Unknown joins remain held.
- Actual ProcessQueueItemAs execution no longer inherits HTTP cancellation; it retains the sealed contribution deadline. Fresh effect admission is unchanged.
- Root command registers the existing hidden protected decision companion; the existing protected native routes are registered on the real server. No auth policy/configuration changes.

Verification:

- `go build -o .continuity-aegis ./cmd/aegis`: PASS.
- `go test ./internal/contribution ./internal/runtime/hermes ./internal/api ./internal/command -run 'Test(Contribution|NativeRole)' -count=1`: PASS.
- `go test ./internal/app ./internal/implementation ./internal/contribution ./internal/orchestration -run 'Test(ContributionNativeProducer|ContributionRoleAuthorityPositiveAppAdmission|NativeRoleJoinRejectsUnknownMissingStale)' -count=1 -timeout=45s`: PASS (orchestration had no matching test).
- `git diff --check`: PASS.
- Broad `TestContribution` invocation across app/orchestration/implementation/contribution/api/command exceeded the 180-second shell timeout. Only app had emitted PASS before timeout; remaining package results are unverified, not failed assertions. Log: `.continuity-test.log`.

Remaining (do not claim complete #377):

- No `ResumeContributionAs` whole-execution entry point or `/contribute resume` grammar yet. Existing role-only continuation is not a whole-worker continuation.
- Original claim lease/authority checks remain unchanged, including source handoff. No expiry bypass was introduced.
- Preparation observer reserves the queue-attempt counter after the canonical claim exists, not before claim allocation. Candidate and runtime-turn counters still need exact engine checkpoints wired before effects.
- Precise freeze/test held-stage continuation, canonical same-attempt reconstruction and end-to-end interruption/disconnect/resume/denial tests through manager/app/worker are not yet verified.
- Native/role shutdown and record-held fencing are connected; full retained source-custody lifetime through unknown child shutdown still needs end-to-end verification.
- No independent frozen acceptance, launch-asset sweep, publication, commit, push, restart or deployment. Parent retains integration/acceptance/delivery ownership.
