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

## Mandatory production join still pending

The above work does NOT implement the complete final contribution verifier or
register whole-contribution success. No accepting callbacks or temporary fake
producer integrations are installed.

- #377 owns actual independently admitted role/continuation bindings, fenced
  epochs, native Wait/stop/quiescence, current controls and cumulative counters.
  Final verification must reload their real immutable history and consume live
  source custody; a caller-supplied role label, stop digest or nil gate is invalid.
- #378 owns actual durable delivery intents/observation history and sanitized
  remote proof. At interface coordination its `Observation` retained commit/tree/
  merge/base/PR but not the complete per-check/review/protection facts. Its owner
  is adding those proof records and a root/history reload surface. Do not invent
  those missing facts or pass fixture observations as production provenance.
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
