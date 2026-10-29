# Issue #379 source delivery and qualification ledger

This records scoped source delivery, not whole-contribution qualification.

## Ordered source integration

- Continuity PR #402 merged as `29ccf8d7d215410dee2a8ff44a3b921b19eba565`.
- Delivery PR #403 (#378) merged as `f6031e0ea40340e8751d8c054a86593d705684aa`.
- The retained #379 branch integrated that main in `f95bb35`.
- After resolving the integration to retain main's existing CI topology, the
  effective source delta against main was empty. The joined-verifier,
  evidence-integrity and run/resume corrections had already arrived through
  the shared immutable source included in #403. This ledger does not introduce
  a second implementation or describe those changes as newly delivered here.
- The #379-owned follow-up PR records this reconciliation and the qualification
  boundaries. It adds no CI jobs, shards, runtime behavior or authority.

## Retained correction provenance

The retained branch includes independently reloaded terminal backing evidence
and corruption denial (`9618768`), canonical winner/waiter and issue-intent
checks, terminal navigation recovery (`e8688fc`), owned-disconnect terminal
metadata recovery (`47d5401`), and run/resume late-error reconciliation
(`514c96c`). Dependency integration reached `723f1d3` before this main merge.
These source revisions are provenance, not independent evidence that every
required contribution journey has passed.

## Observed acceptance, with original scope retained

- Candidate v35 (`514c96c`): independent tester v36 passed normal/race terminal
  notification boundaries, canonical terminal equality/corruption denial and
  explicit fresh resume. Shared-helper late-error injection was not by itself
  a combined public resume/cancellation/final-completion journey.
- Candidate v37 (`c2c9316`): independent tester v38 passed the production HTTP
  abort/same-intent resume/final-result and selected-approval denial in normal
  and race modes, plus race terminal readback/error/corruption boundaries.
  The optional boundary invocation's initial interruption remains historical;
  its separate successful retest does not erase that interruption.
- At `723f1d3`, local supervisor-owned normal/race manager continuity,
  remainder, joined, cancellation, finalization and readback partitions passed.
  API and implementation race partitions also passed. These are local source
  checks, not hosted exact-head CI or installed/live-provider proof.
- Frozen v39 source/executable identity readbacks matched. That identity check
  alone is not fresh independent whole-candidate acceptance.

## Failures and remaining unverified requirements

- The historical aggregate application run at `514c96c` failed
  `TestContributionRoleAuthorityPositiveAppAdmission` with context deadline
  exceeded. Its isolated pass did not clear that aggregate failure.
- Earlier aggregate CLI normal/race timeout failures remain failed historical
  runs. Later successful partition runs do not rewrite those results.
- No claim is made here that every current-main normal/race group, every
  concurrency variant, or all required post-merge readbacks were exercised
  together against one final frozen executable.
- Real-provider, installed-target, protected remote delivery and complete
  whole-contribution acceptance remain unverified by this ledger.
- Scoped source landing was explicitly authorized with these gaps documented.
  This does not close broader #344/#352 qualification or declare #379's full
  contribution completion oracle accepted. CI, a merged PR, a Queue success
  or a model PASS alone is insufficient whole-contribution evidence.

Retained private candidate state and raw evidence remain outside this public
ledger; no credentials, session material, runtime state or grants are published.
