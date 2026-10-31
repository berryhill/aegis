# Issue 382 — current Queue outcome source candidate

## Recorded-reason correction continuation

Base integrated by normal fast-forward to `f752790768d35d299d72fc4e1b942ff0743a440c`
(PR #432); no manager edits belong to this correction. Production
`internal/orchestration/doer_runtime.go` records `doer_loop_failed` and
`doer_verified`. The banner now preserves those exact closed root codes, without
inferring the underlying failure cause. Inspection of `queue_worker.go`,
`implementation_controller.go`, `queue_worker_raw_chain.go`, `doer_preclaim.go`
and `queue_lifecycle.go` found the same omission for the exact terminal lifecycle
codes `operator_cancelled`, `execution_expired`, `authority_revoked`; these three
are included. `retry_exhausted` was already supported. Retry/lease-reclaim progress,
free-form adapter reasons, other unqualified codes and unknown/backend strings
are not newly allowed. There is no prefix matching or generic error rendering.

Five exact-code render/helper regressions failed before the correction; unknown,
near-match, raw error and nonterminal reason tests retain `unavailable`. Full
`web/console` ordinary and race tests, Console vendor/security checks passed.
`go generate ./web/console` regenerated the derived Go; no hand-generated edit.
The correction changes neither DTOs, JavaScript, runtime/result authorization,
execution, evidence nor verification decisions. The failed-root detail remains
unavailable and a success reason alone does not mark selected evidence verified.

Launch ledger for this continuation: CHANGELOG and this ledger updated. README,
LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, threat model, architecture
including diagram, QUICKSTART, no-key demo, RECORDING and contributor backlog
reviewed as unaffected by this five-code read-only presentation correction.
Their authority, commands, licensing and historical proof scope do not change.
No new demo, recording, release binary/checksum publication or remote issue was
created; release and broader historical launch gaps are not qualified by these
checks. Only a new local source/executable freeze is produced under
`/home/silas/.hermes/profiles/xander/workspace/aegis-382-reason-candidate`.
Earlier candidate and current-acceptance evidence remain immutable. Fresh
independent real-journey acceptance and source publication remain parent-owned.
No listeners, live state, runtime budgets, grants or configuration were changed.

## Scope and boundaries

The Console renders a current-outcome summary before topology and refreshes it
through the authenticated exact-run poll. It separates Queue/disposition state,
closed recorded reason, verification and selected-content availability. Missing
legacy failure detail stays explicitly unavailable; `implementation_failed` is
not expanded from raw runtime errors, Requeue policy or topology. Success is a
recorded disposition, not a projection-only verified-acceptance claim. Selected
content still requires the existing exact stable-owner and intact evidence reader.
Native/Graph selected-content unsupported is not an execution failure reason.
The combined #349/#382 candidate projects only the controller's existing closed
native failure vocabulary. Native success now independently reloads exact
artifact/receipt custody, binds it to the selected disposition attempt, participant,
authority and native workspace digest, and requires validated native check history.
Corrupt artifact references, corrupt receipt evidence and foreign-attempt receipt
projections are rejected through the production authenticated debugger reader.
Digest presence alone never creates a `go_test_failed` classification.

## Combined candidate continuation

README, CHANGELOG and EXECUTION_DEBUGGER describe the added native readback.
LICENSE, CODE_OF_CONDUCT, SECURITY, CONTRIBUTING, threat-model and architecture
assets were reviewed: no authority edge, protocol, execution permission, license,
or disclosure-policy change. Existing escaped-fragment/CSP and independent
custody boundaries remain. QUICKSTART, DEMO_NO_KEY and RECORDING remain historical
or no-key scope and do not prove this native/provider journey; no output was
fabricated or recording replaced. No release/checksum publication or contributor
issue creation is authorized. Existing contributor backlog scope is unchanged.

External builder evidence lives at
`/home/silas/.hermes/profiles/xander/workspace/issue349-382-evidence/builder`.
The earlier race log was interrupted (exit 130), not a pass; resumed and final
focused race logs are separate. Native correction/requeue tests use a synthetic
provider with the real native controller/checker and authentic service readers;
they are not actual-provider qualification. The original actual-provider case is
immutable and exhausted its cumulative 2/2 budget. Replaying, resetting, replacing
it or creating grants/config/model/provider/credential authority is prohibited.
Actual-provider acceptance requires the parent to resolve an already-authorized,
isolated candidate execution prerequisite and an eligible remaining-budget
journey without borrowing main's state or authority. No such execution is claimed
here. Fresh independent tester owns frozen-candidate acceptance; publication,
#404 integration and clean-main/installed acceptance remain parent-owned.

## Launch-asset impact

- Updated README, CHANGELOG and the execution-debugger guide for the outcome and
  authorization boundaries. Updated threat-model and architecture prose for the
  escaped server fragment and independent result-reader join; existing diagrams
  and authority edges remain valid.
- Reviewed LICENSE and CODE_OF_CONDUCT: unaffected by read-only presentation.
- Reviewed SECURITY and CONTRIBUTING: existing ownership, disclosure, CSP,
  authentication and verification requirements remain unchanged.
- Reviewed QUICKSTART and DEMO_NO_KEY: no command syntax, installation, authority
  or runtime requirements changed. Their no-key flow does not demonstrate this
  browser feature and was not rerun as outcome acceptance.
- Reviewed RECORDING and retained no-key capture scope: no demonstrated terminal
  output changed, no replacement recording fabricated or published. Historical
  recording is not current-outcome/browser evidence.
- Release binaries/checksums: no release or deployment is performed. The local
  dirty-source candidate and its full source/binary identities are retained under
  ignored task evidence; release archives, checksums and clean-commit acceptance
  remain the parent's later publication gates.
- Reviewed contributor ISSUE_BACKLOG: no new scheduler, general multi-node
  execution, protected evidence custody or live-provider capability is claimed;
  no remote issue created.

## Evidence and remaining gate

`.scratch/382/` retains focused Go/race, security/vendor, deterministic generation,
authenticated browser and build evidence. Browser fixtures observe real Serve and
durable interpreter history; separate terminal fixtures record failure and
file-verifier-evidence-backed success on the same page while replay is paused.
The success host bytes and admission are seeded and explicitly provider-free.
Reusable selected-result tests cover two stored results and exact owner/run/
attempt/authority/receipt/proof corruption denials. Synthetic reducer tests reject
foreign Queue, Graph-run and snapshot response HTML. Authentication loss removes
protected result/outcome/initial observation data and stops polling.

These are builder checks, not independent acceptance or a merge/deployment claim.
A fresh independent tester must verify the frozen complete source inventory and
candidate executable before actual candidate authenticated browser acceptance in
isolated state. Parent owns commit, PR, exact-head CI and merge; this builder does
not publish or change configuration, models, grants or production state.
