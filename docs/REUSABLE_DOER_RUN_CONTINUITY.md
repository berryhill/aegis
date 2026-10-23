# Reusable Doer run continuity

The owning service handles the registered-Agent happy path: publish an immutable reusable Loop, then execute that exact revision for an explicitly requested task through one authenticated Run action. Internal lifecycle and runtime preparation are not operator-carried binding files or repeated task consent.

## Terminal predecessors and bounded tasks

An Agent-scoped preparation reservation must not become an Agent-wide failed-task lock. The service validates the exact terminal predecessor, Queue/disposition/claim/attempt/runtime bindings, principal ownership, idle authority and durable ended session before retirement. It retains the predecessor and every reservation; it never deletes a fence or adopts an old runtime.

A different concrete task contract may proceed with independently admitted fresh authority. The same failed concrete task cannot obtain a fresh budget merely by changing its request key or Loop label. History checks include older failures even when another task ran in between. Exact v5 reexecution uses the existing typed continuation references and immutable snapshot inputs; canonical checkpoint counting determines the remaining implementation passes. Each failed predecessor has one successor. Exhaustion remains denial.

A still-valid signed v5 exact-task approval can cover only its canonical budget-linked same-candidate continuation, without extending its expiry or rewriting consent. The worker resolves the reduced pass cap from authoritative lineage at every effect. An expired approval requires fresh exact Run admission; no TTL widening is used.

## Completed diagnostics and pre-launch recovery

A completed provider diagnostic has its own zero-authority session, not a Queue reservation. Future tasks may ignore that authority only after exact stored diagnostic dispatch, matching session/mandate/Agent/principal, authoritative terminated lifecycle and denied current authority admission are verified. A missing process alone is insufficient.

For a returned pre-launch authority-readiness refusal, same-intent recovery verifies the canonical preparation transition, zero claims/attempts/bindings/output, complete ended authority footprint and absence of unaccounted mandates. It appends an immutable recovery proof and child reservation for the same Queue. Unknown interrupted outcomes, live authority, mismatched identities and unrelated failure reasons remain denied.

A committed failed execution returns its exact authenticated result rather than a generic admission-only HTTP error. Its failed disposition is not relabeled as success.

## Explicit native Codex rebind

`controller-codex preview PROFILE_HOME --credential OPAQUE_ID --replace` reviews replacement of one exact current operational login binding. `controller-codex connect REVIEW_ID` applies that single-use fresh kernel-authenticated principal review. Configuration bytes, reviewed predecessor, exact credential/account metadata and expiry are revalidated. The model/provider and Agent charter are unchanged.

Replacement metadata is published as contiguous append-only private records. Owning YAML, native auth bytes and original connection facts are never overwritten. Stale reviews, conflicts, gaps, unknown/tampered records, unsafe paths and configuration drift deny. The running resolver adopts only the freshly validated exact binding. Login inspection is not model entitlement: use the bounded fixed-prompt diagnostic before spending task budget. The diagnostic emits only closed reviewed codes, never native error messages, credentials or request bodies.

## Required regression and evidence

- `TestInstalledOnlineReusableDoerFailureContinuity`: real public CLI and owning-service integration, synthetic tool-free runtime proposals, no preauthorized contract digest shortcut; failure, independent task on another Loop, exact remaining-budget continuation, preserved failed history and replay/fork denial.
- `TestReusableNextTaskBudgetBoundary` and `TestReusableApprovalContinuationRequiresExactCanonicalLineage`: scope, owner, candidate, ancestry and remaining-cap denials.
- `TestProviderProbeRetirementRequiresExactEndedZeroAuthority` and `TestCompletedPrelaunchRefusalRecoveryKeepsFenceAndQueue`: no live/unknown authority adoption or fence deletion.
- `TestControllerCodexReplacementPreservesHistoryAndConfig`: mandatory append-only replacement and stale/gapped history denial.
- `TestControllerCodexOwningTransport` with an explicitly selected installed native parser: synthetic login metadata, public CLI review/apply, stale/replayed review denial, unchanged native auth/config/Loop and live resolver adoption. No provider network call.

Normal and race package CI must retain these regression tests. Synthetic fixtures do not establish live-provider success. Live completion requires the exact published Loop, runtime-produced output, independently reloaded content/receipt evidence and a matching successful disposition on the named target.
