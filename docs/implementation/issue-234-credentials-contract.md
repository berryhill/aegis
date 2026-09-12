# Credentials workspace — issue 234

Domain contract: Credentials is a metadata-only operator view. Selection and restored browser context must never authorize a read of credential material or a mutation. Existing authenticated server reads and reviewed-operation admission remain authoritative.

Technical contract: keep native server-backed detail links with `#/credentials/:record-id`, render a semantic Reference/Kind/Status/Version inventory and immutable version table, use independent 42/58 desktop panes and one visible pane at the accepted narrow breakpoint, and restore collection filters, selection, scroll and focus without introducing a client authority store. Remove per-record vault dossiers and host-path-bearing backup previews; retain existing reviewed operation entry points.

Acceptance: render active/revoked, empty, unavailable and missing selections; assert semantic tables, unique IDs, safe metadata, native query-bearing routes, responsive pane rules and browser Back restoration. Run focused console/API tests. Visual, security, launch and broad verification remain separately gated flow steps.

Comparison input: `/home/silas/.hermes/.scratch/xander-accepted-design-recovery/index.html`, SHA-256 `634e23fafafb5cd032a86ac80dbc3809d8e5bd9939cda80c16584beb4d4d2a9b`, 755716 bytes. Offline Chrome inspected `#/credentials` and `#/credentials/secret-7d31c9` at 1440 and 390 pixels. Observed semantic inventory; selected detail header with prepare rotation/revoke; concise metadata; immutable version table with concealed content address; narrow detail replaces inventory and offers Back. Fixture output is design evidence only, not an authenticated authority response.

Non-goals: change credentials authority, custody, authentication, operation execution, fleet deployment, or other domain layouts. No provisioning or activation is authorized by this work.

Implementation-step readback:
- Semantic inventory and immutable-version tables replace the generic list and timeline. Detail excludes raw JSON, backup target paths, vault dossier and CLI previews; reviewed operation links remain server-backed.
- Selected desktop panes use 42/58 columns with independent overflow. At <=900px exactly one pane is visible. Browse-only desktop remains full-width as observed in the accepted artifact.
- Existing query-bearing fragment routes remain the no-script fallback. Script reconciles credential fragments to authenticated server routes, preserves filters, uses per-history-entry presentation snapshots, and keeps a separate collection fallback for explicit Back links.
- Independent read-only navigation review identified stale click-only snapshots, cross-entry overwrite and fragment-removal drift. The implementation now saves entry-specific state on pagehide, separates collection/detail fallback state and handles explicit fragment removal. These fixes have source readback and passing unit/API coverage; live Back/Forward/BFCache proof remains for the behavior-test step.
- `go generate ./web/console` succeeded with the repository-pinned templ generator. `go test ./web/console ./internal/api` and `git diff --check` passed after implementation. The Go executable required adding `/home/silas/go/bin` to PATH.
- Generated renderer, structure/metadata regression tests and navigation assertions are updated. No external mutation, credential operation, provisioning, activation, publication or merge was performed.

## Attempt 5 source-repair contract

Previous-error identity: `tflp_5a02ad8b75fc45e7`, SHA-256 `0f4002439e7991fd642e295146267d858797998b8853aeaf10813f4cf8a37366`; matched against the active profile-scoped FlowStore attempt 5 before edits.

Domain obligation: ambiguity in the complete authenticated authorized-stanza set must deny before any requested-stanza constraint. A requested stanza is not authentication and cannot resolve ambiguity; no permissions may be unioned. Development binaries must report the actual Aegis repository revision, not an enclosing repository revision.

Technical acceptance: regress zero matches, unauthorized requests, requested selection from an ambiguous set, and exact single-stanza grants. Validate nested registered-worktree VCS discovery and add a reproducible embedded-revision comparison without disabling stamping, forging build metadata, modifying the toolchain, or weakening dirty-source denial. Preserve the Credentials fragment adapter, CSP, and metadata-only boundary.

Non-goals: unrelated authority changes, owner reporting policy changes, publication, provisioning, or activation in this implementation step. Installed committed-candidate evidence and final launch decisions remain subsequent gates.

## Attempt 4 source-repair contract

Previous-error identity: `tflp_6853ea8140a645aa`, SHA-256 `d4a1fdb83a0a1d19bac90c62e3076fa7bfae81e36f7c47db84fe4680bc5c0532`; matched to active attempt 4 through profile-scoped FlowStore readback, including full previous review outputs.

Domain obligation: distinguish runtime provider-environment bindings from encrypted credential custody. Console inventory receives only credential-record/version metadata through principal-authenticated shared application services; never resolved secret values. Correct the diagram, not runtime authority.

Acceptance: remove the direct environment-binding-to-Console edge, retain selected-provider bindings to the Hermes adapter, and show encrypted authority metadata through Service to Console. Verify against `QueryCredentialsAs` and `CredentialAs`, preserve navigation/CSP and pinentry denial coverage, and repeat the previously failing nested VCS build test without disabling provenance stamping. Historical failure evidence remains; an unreproduced bus error is not a diagnosed root cause.

Non-goals: production code changes, policy-contact invention, provisioning, activation, publication, or merge during this step. Exact committed-candidate and owner launch gates remain later flow obligations.

## Attempt 3 source-repair contract

Previous-error identity: `tflp_4505f79d62014093`, SHA-256 `f4db5ede32d86d7ef9fbf51e5797199e7b084ee5b63995dd7af2b54cefc3ea29`; reloaded from the profile-scoped FlowStore and matched to active loop attempt 3.

Domain obligation: preserve protected-input fail-closed handling and metadata-only Credentials authority while clearing the specifically carried source/launch defects. Existing pinentry and launch prose defects are inherited baseline findings, explicitly included by this attempt's error packet; unrelated refactoring is excluded.

Acceptance: diagnose subprocess failures with bounded process-exit/timing evidence before changing behavior; retain nonzero-exit/cancellation/timeout denial tests. Reconcile expertise, console authentication, and custody prose with current source; regenerate a genuine sanitized no-key recording without operator-state mutation. Retain fixture-browser/CSP/adversarial checks. Clean committed-candidate packaging, authenticated installed acceptance, independent review, and owner reporting decisions remain later gates, not claims of this edit.

## Implementation-loop attempt 2 readback

Authoritative previous-error input: packet `tflp_863e24779da0486a`, hash `901801334e43e62b625e7477a5041c0316d55232349cce31253d12c8e2d9e3c4`. Its failures describe attempt 1; they are not erased by source-repair continuation.

- Independently rehashed and read the accepted artifact: exact supplied digest and 755716 bytes. Offline Chrome opened `#/credentials` at 1440 and 390 pixels without page errors. Visible desktop columns were Reference/Kind/Status/Version; narrow visible columns were Reference/Status.
- The preserved bounded fragment adapter and adversarial static harness pass `python3 scripts/console_security_test.py` and `python3 scripts/console_security_contract_test.py`. This supersedes the reproduced attempt-1 static failure only; it does not establish installed acceptance.
- A fresh real-Chrome run exposed a browser-test defect: Playwright string `wait_for_function` attempted evaluation forbidden by the fixture's strict CSP after Back. Replaced focus waits with auto-retrying locator assertions and URL waits with host-side parsed URL predicates. No CSP, browser authority, or production routing was relaxed.
- `TestCredentialWorkspaceBrowser -count=3 -v` passed at 1440/900/390 pixels after that repair, including Back/Forward, explicit Back, pane proportions/scroll, filter/focus restoration, and hostile fragment cases. These remain synthetic Go-rendered fixture tests, not installed-candidate evidence.
- Launch impact of this incremental repair is test reliability only: no new command syntax, dependency, release format, authority, or product behavior. The attempt-1 launch review remains historical failure evidence; its unresolved documentation, recording, clean-candidate proof and owner-policy findings must be reconciled in the remaining flow. This implementation-step continuation neither authorizes publication nor waives those gates.

Remaining flow gates: real-browser candidate screenshots and interaction tests (including mobile Back and independent scroll), broader/security verification, complete launch-asset impact review, final verification, task-owned PR, exact-head CI and verified merge. No visual-compliance, launch-readiness or completed-task claim is made by this implementation-step evidence.
