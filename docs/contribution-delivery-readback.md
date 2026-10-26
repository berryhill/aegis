# Delivery readback producer v1 (source-qualified V19 bridge)

`internal/delivery/readback.go` produces closed `aegis.delivery.readback.v1`
inside the HTTP adapter. Bounded typed facts retain requested repository, checked
head, ref, PR, comparison base/head, exact upstream selected identities and
statuses, and SHA-256 of original successful response bytes. Non-2xx responses
retain status and closed request target, never response body. Protection includes
strict/admin enforcement, contexts/app IDs, review count, stale/last-push policy,
bypass allowances and active rule facts. Ruleset/app-pinned protection remains
unsupported and fails closed; absence is not inferred success.

`Requirements` comes from the accepted Plan. Semantic validation reconstructs
the existing Check/Merge payload preimage and compares Intent.Payload and Policy.
**#379 must still reload and compare its reviewed policy definition**; an opaque
policy digest alone proves neither policy meaning nor independent acceptance.
Confirmed real HTTP reads must carry matching actor/repository, PR/head/base,
checks, latest reviews, protection, ancestry, merge tree/parents, containment and
postchecks as applicable. Missing/failed/skipped/wrong-target facts do not qualify.
The adapter returns a fixed unknown code if proof qualification fails, retaining
irreversible merge observations. Explicit non-HTTP Adapter fixtures can omit proof.

`ObservedResult` preserves the exact adapter result, including the current base.
`Result` is the canonical aggregate: its BaseHead retains the first historical
containment tip. When a join changes facts, `RetainedFrom` contains the actual
previous record digest/revision and previous plain result. Parse checks canonical
closed JSON, hashes, immutable intent and raw/derived consistency. Canonical
Badger Finish and history load additionally compare the retained link against the
actual previous record. They never rewrite upstream facts to pretend the current
base was the historical first base. Existing fleet database/audit owns all history;
there is no new store, Queue, verifier or disposition.

Capture rejects bounded selected strings with unsupported grammar, known token
prefixes/canaries or echoed exact credential bytes before persistence. Rejected
capture emits no proof, fixed unknown diagnostics and preserves merge SHA facts.
This is conservative filtering, not a claim that arbitrary private strings can be
recognized without a privacy classification source. No raw headers/tokens/private
body are intentionally retained. Original response digests are pins, not retained
response bytes or an independently reproducible signature.

## Owning read APIs

- `Store.GetContributionDeliveryHistory(ctx, ownerID, key)` resolves canonical
  intake/root index, verifies publication custody and Queue/run coherence, then
  reloads actual slot histories in one existing Badger snapshot.
- `Service.ReadContributionDeliveryHistoryAs(ctx, subject, key)` repeats existing
  same-principal read admission. It grants no runtime effects, renewal or epoch
  mutation; historical authority remains unchanged.
- `delivery.ParseReadback(intent, observation)` validates format and producer
  semantics; `ValidateRetainedReadback(previousRecord, observation)` additionally
  binds carry-forward to its actual canonical predecessor.

Limits: 256 KiB/proof, 400 facts, 512 root chains, 2048 revisions per chain.

## Qualification and remaining gates

Focused normal/race tests cover semantic rehash tampering (head, strict policy,
check/review/source/missing facts/requirements), structured-success canaries,
proof-preserving current-vs-historical base, retained revision forgery and missing
or corrupt root/slot/Queue/run records. Existing synthetic HTTP conductor and
Badger suites remain separate evidence tiers.

**Still not complete:** the requested single actual-HTTP → Engine → canonical
Badger → clean reopen → root history → Parse-all lost-merge/postfailure/advanced-
base/later-green integration has not been added. Latest-review multi-reviewer HTTP
regressions and app fresh/stale/foreign-principal expansion remain unqualified.
Expected check-source policy beyond existing classic protection's unrestricted
app setting is not implemented; app-pinned protection denies rather than guessing.
Full upstream policy bytes are deliberately not retained, so the response pin is
not independent reconstruction of omitted policy fields. No real GitHub, managed
provider, installed target, #377 production continuity or #379 whole-contribution
completion was exercised or claimed.

## Launch impact ledger

Dedicated internal producer/API documentation changed. Root README, LICENSE,
SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG, threat model, architecture,
quickstart, demo/recording, release binaries/checksums and contributor issues have
no new public command/release claim from this source-only internal change. No
release, remote issue, publication, deployment or other owner's files changed.


## V16 producer metadata (synthetic qualification only)

ReadbackFact.HTMLURL retains only the exact expected
https://github.com/{approved repository}/pull/{number}; queries, fragments,
userinfo, alternate hosts and alternate number encodings deny capture.
PR readback requires admitted creator actor, non-draft and exact head/base/repository.

ReadbackFact.Author and Committer use ReadbackPerson (name, email, RFC3339 date).
MessageDigest is SHA-256 of actual returned message bytes, never raw message.
Permissions retains explicit repository booleans; AllowSquash, AllowMerge and
AllowRebase retain actual repository settings. Confirmed check/merge semantics
require push permission and the requested permitted method. Issue facts retain
requested target, ID, number, state and state_reason. Confirmed issue readback
requires closed/completed and exact completion-authorized intent preimage.
Independent #379 authorization must precede closure; final observation follows
closure, and is not itself the preclosure authorization.

ReadbackRequirements.Publication binds tree, parent, author, committer and
message digest. ObjectDigest binds the exact accepted Git object. Object intent
preimage is now {Intent, ObjectDigest, Publication}, replacing raw object bytes.
UploadCommit requires actual exact remote GET facts under the existing same-context
collector. No expected final merge hash is synthesized. #379 must reload source
policy and independently join local accepted commit/tree and publication records.

V16 regressions exercise loopback HTTP publication metadata, canonical PR URLs,
metadata tampering and latest multi-reviewer decisions: old changes-requested
superseded by current-head approval passes; latest changes-requested denies.
Existing semantic tamper/canary regressions remain. This is not real GitHub proof.

### Explicit remaining gates

Single actual HTTP -> Engine -> Badger -> close/reopen integration remains
NOT qualified; no test-only overlay integration was produced this iteration.
Exhaustive root orphan/slot/head integrity and expanded Queue/run coherence,
and actual app history fresh-owner/stale/foreign-principal readback qualification
remain incomplete beyond existing V15 coverage. Semantic intent-preimage checking
covers check/merge, UploadCommit and issue reconciliation; remaining operation
schemas still require equivalent full validation. Actual #377 native Wait
composition, reviewed production policy resolution and #379 independent final
whole-chain disposition remain external gates. No whole-contribution success claim.


## V19 stable neutral history bridge

`delivery.HistorySnapshot` carries `Contract`, exact canonical `ContractBytes`,
`RootRun`, sealed `Root delivery.PublicationRoot`, exact canonical `RootBytes`,
and ordered `Histories [][]delivery.HistoryEnvelope` from one Badger view.
`PublicationRoot` retains the actual existing publication seal and metadata;
there is no invented authoritative snapshot digest. `HistoryEnvelope` retains
actual `Record` (including revision and both outcomes), `PreviousDigest`, `Digest`.
Its explicit wire field order is Intent, Outcome, Observation, Revision,
PreviousDigest, Digest, unchanged from the pre-bridge disk representation.

Stable exports: `delivery.ParseHistoryEnvelope([]byte) (HistoryEnvelope, error)`,
`delivery.ValidateHistoryEnvelope(HistoryEnvelope) error`, and
`delivery.ValidateHistorySnapshot(HistorySnapshot) error`. Snapshot validation
checks sealed bytes, owner/contract/root binding, lexically ordered unique intent
chains, revision/predecessor continuity and retained proof relations. These are
structural/binding checks, not final verification or authority grants. Neutral
validation returns delivery errors; durable Badger boundaries map invalid history
to `fleet.ErrCorrupt`. Badger aliases its existing DeliveryObservation and
ContributionDeliveryHistory types to the neutral types; application read returns
`delivery.HistorySnapshot`, rechecks owner/key/sealed contract/root after fresh
same-principal admission, and never renews historical epoch or role facts.

Earlier V15/V16 missing-integration notes above are historical: the retained V17
isolated HTTP→Engine→Badger→clean reopen proof passed with synthetic source and
reservation authority, not production #377. V19 promotes the isolated real app
fresh/stale/foreign read test and adds neutral legacy-wire/tamper coverage.
No real provider, GitHub, installed/live journey or #379 completion is advertised.
