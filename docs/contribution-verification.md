# Contribution verification (issue #379)

Read the [canonical contribution happy-path contract](../.hermes/contribution-happy-path-contract.md).
This document describes implementation boundaries, not an alternate acceptance
oracle, permission to run a contribution, or a claim that the whole journey works.

## Implemented independent verifier work

- `internal/evidence/contribution.go` defines versioned, immutable reference-only
  manifest/report schemas. Strict decoding checks the content address, duplicate
  keys at every depth, exact field spelling, unknown/omitted/null fields,
  canonical encoding and trailing data. A valid schema grants no authority.
- `contribution_reload.go` reloads actual blobs through the existing blob owner,
  compares the exact run/Queue/claim/attempt/invocation/contract/control/Loop/
  profile/candidate/policy binding and target, and enforces exact evidence sets.
  Missing, duplicate, extra/substituted or cross-run evidence is not accepted.
- `contribution_merge.go` reloads raw Git commit objects and independently checks
  their Git object identities plus SHA-256 blob custody. It proves the accepted
  tree -> reviewed publication -> merge tree relationship and bounded
  first-parent protected-base containment. Squash, merge and fast-forward are
  distinct policies; squash does not require equal candidate/merge SHAs. A base
  different from the tested base requires new integrated-candidate acceptance,
  not a permissive equality exception. These checks alone prove neither remote
  provenance nor branch protection, reviews, CI, issue disposition or authority.
- `implementation.ManagedContribution.ReloadAcceptedEvidence` reconstructs the
  real native producer's immutable baseline, complete candidate lineage, trusted
  check observations, independent tester identities, candidate-bound asset
  ledger and policy. It checks canonical primary-record bytes, exact expected
  run/contract/attempt/candidate/policy, consumed budget and actual current
  source/Git/toolchain/executable identity, with fresh admission before/after.
  It accepts no caller-provided PASS report or accepting reload function.
- `QueueWorker.ReloadContributionCandidateEvidence` obtains that evidence from
  the actual kernel store selected by the admitted Queue and immutable Loop,
  persists it in existing blob custody, independently reads it back and repeats
  the native owner verification. `Service.ReadContributionCandidateEvidenceAs`
  resolves the retained key and exact definitions under authenticated readback.
  This is private internal composition, not a new public source-export API.

The native blob includes source and private test evidence. Do not expose it in
public GitHub text, model-visible broadcasts or unauthenticated responses.
Only bounded references returned by the authenticated owner may be reported.
This readback grants no source lease, role authority or publication permission.

## Closed outcome vocabulary

The report schema distinguishes whole-contribution `succeeded`, explicitly
`narrower-target-completed`, `failed`, `denied`, `cancelled`, `expired`,
`exhausted`, `unknown` and `merged-unresolved`. A known merge cannot be erased
into ordinary failure/cancellation/expiry, and a no-merge/local-only result
cannot claim merge success. Outcome/reason pairs are closed.

These are report semantics, not new Queue enums. Existing Queue disposition
and atomic completion remain authoritative. A report's content hash or valid
JSON is not verifier provenance and must never be passed to the generic
artifact verifier to manufacture whole-contribution success.

## Retained native/delivery assessment (source integration, not completion)

`Service.AssessContributionRetainedDeliveryAs` accepts an authenticated retained
key and a trusted-controller reviewed policy reference/resolver. It calls the
actual native candidate and delivery-history read owners, reloads native bytes
from private blob custody, compares the native candidate/immutable Loop policy
and delivery binding, validates sealed history, and repeats policy, native,
control and history readback before returning. It does not enter source custody
or effect callbacks. The result contains only closed codes and private evidence
references; it is not a final receipt, current GitHub admission or source lease.

The history assessment distinguishes local/no-publish commit-only evidence,
no-merge publication/PR/check evidence and merge-target evidence. Required remote
observations must parse their retained proof; independently resolved check,
reviewer, protection and post-check requirements cannot be self-selected by that
proof. Earlier candidate histories are retained but cannot satisfy current effects.
Available same-root valid merge-envelope references survive subsequent native or
policy failure. If the canonical history owner rejects the entire snapshot before
returning it, this consumer cannot recover a partial merge history: a supported
owner readback preserving those irreversible references remains a coordination gap.

Qualification currently covers real app/Badger canonical history, fresh/stale/
foreign admission, unknown versus confirmed commit, policy/candidate/role/epoch/
tree/head/chain substitution, false scope promotion and reviewed-requirement
substitution. Native absence denies rather than using a fake acceptance callback.
The positive Service + canonical Badger + actual native accepted producer +
intent-reserved Engine local commit composition is now exercised by
`TestRetainedDeliveryNativeComposition`. It verifies RED/GREEN native execution,
actual native blob reload, retained local assessment and unchanged claimed Queue
disposition. Protocol transport/reservation authority/quiescence remain synthetic;
this is NOT #377 native Wait, provider or GitHub qualification. Wrong authorship,
committer, timestamp, message, approved paths, source, tree, toolchain, actor,
candidate, role, stale/foreign subjects and stop are covered in that fixture.

The assessment now derives the reviewed authored commit and object graph using
actual `delivery.Git.ProjectAccepted`, then independently reads the committed
branch/object via read-only `Git.Reconcile`. It compares retained observations
to that exact head/tree; present optional current effects cannot silently fail or
omit proof. Upload-commit metadata/message/tree/parent is compared to independently
reviewed policy, and PR facts require canonical URL/creator/head/base.

The additive `delivery.ValidateIntentForPlan` verifier adapter now delegates exact
comparison to the unchanged canonical producer `intentFor`. No private payload
preimages or producer formats are copied. The assessment obtains its expected
Plan from the actual owning Source's pure projection and selects only actual
projected objects and the independently resolved owned PR for each operation.
Altered IDs, payloads, run/contract/candidate, actor/repository/ref/head/policy,
role/epoch or object bytes are rejected. This comparison does not reserve an
effect, perform I/O or grant authority. `ParseReadback` still validates retained
remote observations and their requirements independently of reviewed policy.

When these evidence joins pass, the product returns
`retained-delivery-evidence-assessed / continuity_verification_required`. This is
not a completed contribution, a live source-custody proof, issue-close authority
or terminal disposition. Full remote-proof/merge/later-corruption qualification
and final #377/private-custody integration remain mandatory gaps.

Independent review found that an optional PR update could select its own PR
number for validation. The repair resolves that target from the owned creation
effect or an independent reviewed pin, rejects contradictory/duplicate creation
references, and binds the intent, observation, request target, number and canonical
URL to that same PR. The original counterexample failed before the repair;
focused normal/race regressions now pass. These remain controlled semantic tests,
not a remote mutation or a whole-contribution disposition.

## Mandatory production join still pending

### Current final-verifier core (not yet terminal integration)

`NewContributionFinalVerifier` binds the actual Service and trusted reviewed policy
resolver. `VerifyPreClosureAs` takes only an authenticated retained key. It uses
the retained native/delivery owner assessment, then enters the actual #377
`WithContributionContinuityCandidateAs` private source-custody gate. It replays
canonical immutable continuity events through `ApplyContinuity`, checks the exact
projection/cumulative counters, joins historical native builder/tester identities
to actual admitted RoleAuthority events and completed native-command bindings,
requires native quiescence and fresh controller `CheckContributionEffectAs`, and
checks the original claim/attempt plus any actual bounded continuation. Policy,
history/native references and continuity head are reloaded before minting an
opaque private-field `ContributionPreClosureAuthorization`. Narrower scopes do
not mint issue-close authority. `RevalidatePreClosureAs` repeats that owning join
and rejects forged/stale/service-substituted authorization.

The core compiles and negative normal/race tests cover forged authorization,
missing native joins and corrupt/absent immutable history. A complete positive
core mint using actual full merge-scope retained proof has NOT yet been qualified.
There is no installed finalizer, terminal-report producer, completion adapter or
manager hook from this core yet. Those are unfinished implementation, not PASS.

The actual canonical `CompleteQueueItem` transaction still explicitly permits
only evidence-free EXPIRED completion after the original claim expires. A #377
Continuation does not change that check. Its contribution-only final-completion
contract must be agreed with the owning store: revalidate the exact original
claim/attempt and continuation head, current independently admitted controller,
private custody and opaque verifier provenance atomically without renewing the
original Claim/RuntimeBinding or weakening generic late-success denial. Until
that supported branch exists, do not emulate late completion with a caller flag,
overwrite claim expiry or create an unindexed terminal-result blob. Final issue
readback, canonical DispositionByRun/result retrieval and the manager-to-finalizer
hook remain explicit unfinished integrations.

The above work does NOT implement the complete final contribution verifier or
register whole-contribution success. No accepting callbacks or temporary fake
producer integrations are installed.

- #377 owns actual independently admitted role/continuation bindings, fenced
  epochs, native Wait/stop/quiescence, current controls and cumulative counters.
  Final verification must reload their real immutable history and consume live
  source custody; a caller-supplied role label, stop digest or nil gate is invalid.
- #378 owns durable delivery intents/observation history and sanitized remote
  proof. PR #396 merged at `f3b8fa81918f51ec1a3fe12c3a0964aa6848629d` and is
  integrated here; its sealed history and per-check/review/protection readback
  supply the consumed source interfaces. Source merge does not establish the
  remaining productive authority/quiescer join or real remote acceptance.
- Pre-issue-close verification must produce an opaque exact-scope authorization
  over complete accepted behavior and merge proof. Final disposition follows
  actual scope-correct issue readback, avoiding a circular dependency in which
  issue closure would require its own already-final receipt.
- Final atomic disposition, authoritative retained-session result/retrieval,
  real manager-to-delivery-to-verifier assembly and revision-matched skill
  discoverability still require those real interfaces. No new queue, daemon,
  watcher, scheduler or management page is introduced.

## Qualification tiers and remaining acceptance

Tests under `internal/evidence` use synthetic immutable object/schema fixtures.
The existing native contribution regression now also exercises the real native
export after real RED -> wrong first candidate -> bounded repair -> fresh tester
-> GREEN and rejects wrong run/contract/attempt/candidate/policy, cancellation and
noncanonical producer bytes. The Queue integration additionally verifies exact
causality, foreign-owner denial, retained evidence and unchanged claim/budget.
Its Hermes protocol/runtime authority inputs are synthetic. Neither is a live
provider or real protected GitHub contribution campaign.

All canonical #379 rows remain mandatory: actual managed happy path/repair,
false completion, duplicates, interruption/restart, expiry/reattachment, protected
decision outcomes, acknowledged cancellation/unknown effects, fixed budgets,
CI failure/conflict/base advance, stale/corrupt proof, post-merge failure,
narrower scope and privacy/authority denials. Missing producer integration is
missing implementation, not a passing fail-closed result.

A real controlled protected repository/base/issue and supported controller rights
must be verified before remote acceptance. At discovery, `berryhill/aegis` main
was unprotected; named sandbox/test repositories checked were also unprotected
and the current Xander actor had no push rights there. No repository policy or
credential was changed to cure that prerequisite. No 8448 ownership, historical
case, runtime budget, release, deployment or configuration change is implied.

No issue may close from this slice, a valid manifest, native receipt, fixture,
build, PR creation, CI alone or a manually completed host GitHub workflow.
