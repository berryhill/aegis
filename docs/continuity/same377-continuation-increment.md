# SAME377 continuation increment

## Executable interfaces

- `Service.ResumeContributionRoleAs(ctx, subject, key, role, head) (core.Session, error)`: create-only durable preparation hold before ordinary PreviewSessionAs/StartSessionAs; append observation and compatible next role epoch. Unknown preparation stays held. No old session reactivation or Queue/claim/attempt rewrite.
- Existing `AdmitContributionRoleAs`: successor requires exact acknowledged adapter stop plus complete closed native boundary; reloads historical authority/mandate and compares stanza/runtime/environment/tools/capabilities/scopes exactly.
- Internal Linux `runIsolatedContributionNativeBatchAs`: opens a durable nonempty exact command inventory before executing any command; executes actual RunContributionNative reserve/Wait/group-empty joins, closes only on all completed commands. Explicitly rejects a nonnil QueueWorker: it does NOT certify old uninstrumented ManagedContribution producers.
- `Service.WithContributionContinuityCandidateAs`: invokes the actual integrated376 WithAcceptedContributionCandidateAs and real lock-held pre/post gate, matching canonical candidate/attempt and builder/tester native/stop joins. Original376 authority admission remains in force.
- `CheckContributionEffectAs(writing=true)`: only permits a current exact controller under a live private synchronous custody context issued by that wrapper; exact contract/candidate/scope and builder/tester joins still required. All normal external/direct calls remain denied.

## Verified local behavior

Actual application fixture launches normal independently admitted role sessions, creates a canonical Queue claim and attempt, runs `/usr/bin/true` through the native adapter, persists closure, obtains real adapter stop, races two explicit Resume calls (one clean session admitted), checks epoch 2, denies old role, wrong owner, stale head and duplicate issuance, and verifies original RuntimeBinding unchanged. Fixture deliberately excludes the legacy worker while the isolated producer owns commands; not a ManagedContribution end-to-end positive.

- `go test ./internal/contribution ./internal/app ./internal/api ./internal/command ./internal/persistence/fleet/badger ./internal/runtime/hermes -run 'Test(Contribution|NativeRole)' -count=1`: all packages passed except initial Badger expectation for the no-stop successor error. Reducer fixed to preserve ErrContinuityFenced there; subsequent Badger focused run passed.
- `go test ./internal/persistence/fleet/badger -run TestContribution -count=1`: PASS.
- `go test -race ./internal/app ./internal/contribution -run 'Test(ContributionRoleAuthorityPositiveAppAdmission|NativeRoleJoinRejectsUnknownMissingStale)' -count=1`: PASS.
- `go build ./cmd/aegis`: PASS.

Protected decision fixture now uses production apiHTTPErrorHandler. This exposed real wrong-purpose/replayed receipt 500s; the dedicated decision route maps ErrReviewReceiptUnavailable to 409. Fixture now requires exact 401/403/409 (and malformed input 400), not any failure status.

## Still unavailable / integration gate

No claim of complete #377. No positive real ManagedContribution source-custody/write-effect join yet: its native producers remain uninstrumented, and the original376 handoff admission still resolves the original claim authority. The dedicated source join intentionally does not bypass that. Connecting the registered native producer into those owner-held executor/worker files, and admitting compatible controller continuation at the original authority gate, requires owner release. No central376 file was edited. No publication, restart, live service changes, or commits.

Launch-asset review and full independent frozen acceptance remain with parent; no changed public CLI/runtime/release capability is claimed by this private increment.
