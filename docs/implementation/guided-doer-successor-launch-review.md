# Guided credential-only Doer successor: source launch review

## Scope and result boundary

Existing task-owned `fix/guided-doer-successor` worktree; source-only implementation and isolated native tests. No GitHub writes, release publication, live charter/draft mutation, provisioning, controller configuration, credentials, runtime restart or live model/native companion invocation. The standalone source-helper Doer produced a BLOCKED result, not implementation or live-product verification; these changes were implemented and tested directly.

## Changed behavior

- Native retained-draft setup owns protected browser confirmation. Typed setup review shares the credential-only proposal and returns its browser handoff; typed approve-successor denies without independent proof, and typed rejection applies no authority. No model action may click/type human confirmation.
- Eligible proposals correct only the sole `provider:codex` credential scope of one sole enabled, already zero-capability/tool/toolset/memory/integration stanza. They request controller Codex authentication without changing model, provider, runtime or authentication methods/selectors. Unrelated authority denies instead of being erased.
- Exact compatible imported canonical revisions are reused for review, not automatically approved. Both canonical artifacts/digests are reviewed; the principal makes a separate decision. No provider readiness is asserted.
- Browser review uses the existing session-bound single-use receipt and CSRF/origin boundary. Typed service review uses bounded five-minute single-use transport-identity-bound receipts; decision accepts only draft ID, receipt and explicit decision, never editable proposal bytes. Restart loses reviews and fails closed.
- Exact import/Agent-successor/draft rebind is not one cross-store transaction. Identical imported candidates and an identical immediate approved successor can be re-reviewed/recovered; conflicting occupied revisions, unrelated successors, stale versions, substituted principals/artifacts and expiry deny. Task contract/lifetime survive, publication bindings renew only on explicit approval.
- Provisioning, host-write consent, native local-OS continuation, publication and Run remain independent. Browser identity is not local-OS identity. No scopes/tools or runtime effects are automatically granted.

## Launch assets

Updated: README, SECURITY, CONTRIBUTING, CHANGELOG, Doer guide, threat model, architecture boundary, canonical Loop skill, operation matrix and generated skill-bundle manifest; native templ output regenerated.

Reviewed unaffected: LICENSE and CODE_OF_CONDUCT (no legal/conduct change); five-minute quickstart/no-key demonstration (feature outside that proof); historical no-key recording and retained transcript/timing (not successor/live-provider proof); release script and binary/checksum contract (no packaging/publication change); local early-contributor issue backlog (still accurate, no remote writes).

The no-key demonstration invokes a real runtime and was not executed under this no-live-mutations contract. Existing recording is historical, not a new candidate recording. Clean committed release binaries/checksums and any authorized GitHub publication remain parent/owner gates. No release artifacts, recording output or issue URLs were fabricated.

## Verification

- Initial negative authority tests exposed invalid synthetic fixtures (tool/toolset mismatch, unsupported profile and overlapping selectors); corrected the fixtures without weakening product validation.
- Focused native app/API/command successor and online-route tests passed.
- Independent-human-decision correction: targeted API tests `TestDoerSetupTyped*` and `TestDoerSetupNativeProposalExactDecision` passed with `GOMAXPROCS=2 go test -p 1 ./internal/api -run 'TestDoerSetupTyped|TestDoerSetupNativeProposalExactDecision' -count=1`; targeted Doer online command tests passed. Review and agent approval denial leave imported charters, latest Agent and retained draft unchanged; safe rejection still works. The existing browser exact-receipt confirmation test remains green. Canonical opt-in skill manifest refresh/validation and `git diff --check` passed. Parent owns the full-suite rerun and publication; no live human/native/provider journey was exercised.
- Focused app/API/command race suite passed, including missing/tampered receipts, foreign principals/origin, editable payload denial, replay, expiry/rejection, exact imported-r4 reuse, partial import/approved-successor recovery, authority-removal denial and contract preservation.
- Full skill-bundle package tests passed; bundle validation passed for 15 skills; evaluation passed 101 structural fixtures, not behavioral/service/runtime execution.
- Console generation and Python console security checks passed; `git diff --check` passed.
- Full suite/build/vet execution is retained in `.guided-doer-full.log`, `.guided-doer-build.log`, `.guided-doer-vet.log`. Read actual process completion before claiming those gates pass; final source rerun supersedes any pre-final compilation.

## Remaining acceptance

Independent parent review, source publication/CI/merge, extracted-candidate installed/browser/visual qualification and separately authorized live provider/native-continuation/task execution. Local fixtures do not qualify the current live r4 charter or retained draft, and neither was changed.
