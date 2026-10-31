# Contribution delivery adapter candidate (#378)

The bounded #423 [publication custody and issue recovery](contribution-publication-custody.md)
correction separates retained contribution history from active repository/base
exclusion and recovers the original issue-close intent. Earlier status notes
below remain historical evidence, not a claim of whole #378/#379 qualification.

## Fresh instruction scope at the delivery sink

Both application delivery entrypoints wrap the supplied real authority for canonical reservation/fencing and engine effects. Every fresh write admission reloads the authenticated sealed contract/current controls and uses the existing contribution Reader for exactly the approved profile read action and issue URL. Repository/issue/creator identity, title/body, open state and complete discussion identities/content must still match. Observation time, ETag/wire digest and incidental issue update time are not semantic changes. Changed, incomplete or unreadable instructions hold scope with a fixed typed denial before effects; untrusted edits never grant rights, mint owner cancellation/approval, replace the intent key, or replenish budgets. The actual actor/native authority remains mandatory and is repeated after the read.

Read-only same-intent reconciliation delegates to the real read authority without requiring an open issue or a new source read. Thus an authorized own issue-close postread is not rejected because that write closed the issue. This is a point-in-time reread, not an atomic transaction with GitHub, and does not prove native continuity or full-scope completion.

## V20 bounded controller readiness

Trusted composition may supply `WithReadiness(NewRecheckPolicy(...), budget)` to
`NewConductor` or the optional typed `RunContributionDeliveryAs` dependency.
`NewRecheckPolicy` accepts only maximum 1–2 and delay 0–30 seconds. No runtime
request, grant, consent prompt, default accepting counter or new owner is added.
The delay and read admission must remain inside the sealed intake deadline.
One Source callback/private context survives the cancellable timer and all reads.

`ReadinessBudget.ReserveReadiness(ctx, exactIntent, maximum)` must durably reserve
against **#377's existing root-global infrastructure counter**, validating exact
root/contract/candidate and Check/Merge ID plus native continuity. That owner must
retain consumption across candidates, phases, processes and repeated Run calls;
the Conductor's additional per-invocation ceiling cannot replace persistence.
The productive owner validates actual private authority and fresh instructions
before charging the existing counter. Each call reserves a NEW ATTEMPT using the
exact intent and next GLOBAL slot, even if UNKNOWN and its revision are unchanged.
A crash before HTTP or uncertain committed CAS response retains the charge, with
no refund, replay freebie or revision-generation veto. One fixed-head CAS is
attempted per call; all CAS errors deny HTTP, even if the deterministic ID exists,
because a concurrent winner cannot be distinguished from an uncertain own write.
The next call may consume the next slot, never replay the mutation/effect intent.
This conservative attempt accounting does not claim exactly-once HTTP.

Canonical Check reservation returns freshness before any HTTP. Its first read
uses Reconcile/read-only while retaining fresh reservation/CAS semantics. Every
extra Check read and every post-merge readiness retry reserves budget first;
missing policy/budget, deadline, cancellation, unavailable owner and exhaustion
hold explicitly. Confirmed/Failed/Denied records do not retry HTTP. Existing
UNKNOWN cannot be escaped with a new intent, candidate, key or Run-local counter.
Post-merge retries only reconcile the same Merge intent after a persisted merge
fact; pending/exhausted results preserve Merge and canonical history. No second
Merge API mutation is issued.

An UNKNOWN write **without** a known Merge fact is different from ordinary
readiness: the existing Engine admits at most one exact same-intent read-only
reconciliation per operation invocation, through fresh read fence/authority. It
never resends mutation or loops on that uncertainty. Once it discovers the merge
fact, any subsequent readiness read requires the root-global budget. This is
bounded existing reconciliation admission, not silent policy renewal.

No review/check/protection/TTL rule or named-review support changes. #379 remains
mandatory for issue disposition. No launch/CLI/credential operation is added.

## V13 bounded controller-owned sequence

`delivery.Conductor.Run` holds exactly one accepted-source callback and its private
context for Commit -> ordered graph Publish -> canonical own non-draft PR result
-> closed `CheckDelivery` checkpoint -> permitted Merge and exact post-merge
readback. PR numbers from policy/callers are ignored. `executeAccepted` reuses the
existing Engine reservation/CAS/replay graph path without reacquiring Source.
The result lists completed/held operations and exact refs; it is not canonical
Queue state or whole-contribution disposition.

`CheckDelivery` performs production HTTP reads of exact head, current base,
ancestry, mergeability, required checks, independent reviews and pinned protection.
Missing/pending/skipped/stale gates remain held. Concrete CI/conflict failures are
persisted before the existing RepairOwner receives their exact root/intent/digest.
Missing repair ownership holds; no candidate or builder is manufactured. Optional
bounded readiness reads use the explicit persistent #377 budget port described
below; without policy/budget, reconnect retains UNKNOWN without another HTTP read.
Unknown publication/PR/merge effects reconcile the same reserved intent, never
resending mutation. Merged-but-failed postchecks retain the immutable merge fact.

Local-only/no-publish runs stop after Commit. No-merge stops at published/checked.
Issue closure is attempted only after the trusted #379 CompletionOwner returns a
verified immutable full-scope reference; without that owner the issue stays open.
The joined verifier is not implemented here. `RunContributionDeliveryAs` assembles
the real Source/Badger/Engine/Conductor from trusted dependencies; actual #377
composition and automatic manager hooks remain pending, not silently installed.

Qualification includes sequence/replay/concurrency/context/narrowing/held gates,
repair handoff and a **syntheticHTTP**, not realGitHub, pipeline using production
object publication and GitHub PR/check/merge HTTP operations. Its local commit
port is synthetic; this is not live protected-repository acceptance. Focused
normal/race/vet and architecture results are retained with the V13 freeze.


This is **not working whole-contribution publication** or an activated Loop.
The application now has an explicit synchronous downstream consumer, backed by
canonical Badger delivery reservations and the actual #376 accepted-candidate
custody callback. Automatic manager continuation is not installed. The real #377
native quiescence gate and per-effect authority remain required controller-owned
dependencies; nil integration reports unavailable, never simulated success.

## V11 policy isolation and functional authority clock

Resolved trusted policy slices and projected Plan check/reviewer lists are detached
copies. Synchronous and retained callbacks cannot mutate the canonical resolver
policy through these values; concurrent writes to incoming policy pointers are
not granted authority or made safe by this copy. The resolver remains a trusted
immutable controller store, not a hostile same-UID sandbox.

The manager native fixture injects one synchronized logical stage clock into app,
Fleet, worker and manager before capture. All existing 300-second authority TTLs
and contribution deadlines remain unchanged; real native tools/context deadlines
remain live. Explicit final clock advancement tests expiry denial. Epoch/role/actor
pins remain policy projections, not actual-current per-effect #377 admission.
Evidence is confined to `.acceptance/v11/`; tester V10 remains immutable.

## V9 accepted-source application bridge

`internal/app/contribution_delivery.go` resolves a content-addressed immutable
reviewed controller policy, reloads sealed intake and the real accepted candidate
through `WithAcceptedContributionCandidateAs`, checks exact bindings and current
narrowing, and projects publication objects while the kernel owns the workspace.
Policy/control are reloaded at both native gate boundaries; the kernel rechecks
source, toolchain, evidence and authority after the synchronous callback.
`ExecuteContributionDeliveryAs` consumes an existing sealed key/reference using
the canonical Badger adapter and Engine, with no new queue or credentials.

`Git.ProjectAccepted` compares every accepted tree entry against the complete
inventory, including ignored inputs and read-only trusted tests. Only changed or
deleted entries require the reviewed allowlist. Projection is read-only: it reads
the existing frozen graph, retains exact accepted tree/blob bytes and replaces
only its final commit with canonical reviewed author/committer, date and message
bytes/OID computed in memory. It creates no objects, private index, refs or source
changes. Unsupported metadata is denied rather than normalized. The synthetic
freeze commit remains only a historical anchor. Actual private-index Git plumbing
and object writes occur only in the reserved, freshly authorized Commit effect;
its actual tree/commit must equal the pure projection before updating the branch.

Focused tests cover real native #376 build/repair/acceptance and kernel custody,
publication projection and wrong tree/inventory/scope/parent denial. The existing
`TestContributionManagerApprovedNativeRepair` now continues the same authenticated
manager -> application -> Queue -> real #376 role resolver -> accepted-candidate
fixture through actual `NewContributionDeliverySource(...).WithAccepted` and its
full Plan callback. It checks exact intake/candidate/source/tree/base/workspace/
Loop/attempt/epoch/role pins, actual publication author/committer metadata, unchanged
full tree/HEAD/index, exclusion of the synthetic freeze commit, and synchronous
kernel custody (a nested application handoff cannot acquire the workspace).
Post-positive denials exercise project checks for tree/source/attempt/policy/owner
and other bindings after real gate entry, plus wrong subject, nil gate, callback
source drift, policy drift and current sticky stop. The policy resolver is a
trusted immutable controller fixture; the gate checks the exact real candidate
but supplies **SYNTHETIC quiescence only**. This is positive application callback
fixture proof, not native #377 Wait/claim/stop acknowledgement, real-provider,
remote publication or automatic manager/conductor integration acceptance.
Builder evidence and the new frozen candidate are under `.acceptance/v9/`;
fresh independent acceptance remains the parent's separate responsibility.

Launch impact: detailed delivery documentation and dependency-direction validation
changed. No CLI, quickstart, release, license, conduct or deployment surface changed;
existing no-key demonstration and released binaries are not delivery proof.

## Implemented contracts

`internal/delivery` provides a closed Engine and local Git / GitHub adapters:

- Accepted-source callback port; stable, intent-bound reservations; read-only
  reconciliation of unknown effects; fresh fence/authority guards before writes.
  Duplicate negative observations do not finish an in-flight reservation.
- Scoped deterministic local commit plumbing, private index, reviewed files and
  modes, isolated Git environment, no hooks/shell/ambient credential helpers.
  ClosedAdapter binds exact plan/intent, branch/head/tree/parent and approved
  worktree root on apply and reconcile. Protected base equals feature is denied,
  including slash-containing bases. This is not a hostile same-UID sandbox.
- Accepted local Git object extraction and ordered blob/tree/commit HTTP transfer;
  feature ref creation/non-force update **only after the exact graph is remote**;
  exact repository/actor/ref checks, non-draft issue-owned PR lookup/create/update,
  exact-head checks/reviews, current-base ancestry, permitted squash/merge,
  actual merge tree/parents/base containment and post-merge check readback.
- Fresh publication-content review port; no automatic closing keywords in public
  PR text or commit message. Issue closure requires a trusted full-scope completion
  reference. Structured persisted CI failures may be forwarded to a repair owner.
- Any error after an attempted remote mutation remains UNKNOWN. A merged PR
  retains its merge identity when later validation fails; it is merged-unresolved,
  not unmerged, denied or permission to replay. Read reconciliation needs separately
  valid observation authority, not revoked write permission.

Policy check/reviewer lists must be nonempty, exact and complete; missing, skipped,
stale or duplicate evidence does not pass. Post-merge checks also require nonempty
coverage. Checks are controller policy input, never inferred from an empty result.
Earlier operations use explicit positive payload schemas; postmerge check policy does
not rotate publication/PR slots. A stable run/contract/repository/base/branch/operation
slot rejects a changed actor/candidate/precondition/scope/payload with ErrConflict,
even after UNKNOWN. PR content repairs use the separate exact-owned existing-PR
update operation, never another create. Repeated different updates or later
candidate publications still require an explicit canonical owner transition; no
implicit replacement/retry permission is implemented.

## V7 partial merge reconciliation repair

The Engine now persists a newly read UNKNOWN merge fact through canonical Finish,
under the same reserved intent. It validates every nonempty head/tree/merge/base/PR
fact against SHA syntax and the exact operation/accepted projection, joins omitted
immutable facts from the prior record, and rejects contradictory merge/head/tree/PR.
Negative duplicate reads still cannot consume an original writer's CAS or terminalize
its reservation. One competing Finish permits only one bounded same-intent Reserve
reload (which must return existing, not fresh) and one further Finish; no resend,
rotated intent, unbounded retry or new authority follows.

An actual merge with failed post-merge validation remains UNKNOWN with
`merged_validation_unresolved`. Repeated failure cannot promote it. Later complete
adapter readback may confirm the same merge only after current containment and all
required post-merge checks pass. Observation still requires real separately admitted
read authority; cancelled contexts and revoked observation authority deny.

`Observation.BaseHead` is explicitly the first observed **historical** base tip,
not a promise that the branch is still there. Both Engine joining and canonical
Finish retain it when the protected branch advances. The GitHub adapter independently
reads and proves the current base contains the same actual merge before confirming;
that current tip does not rewrite the historical record or the intent precondition.
Merge/tree/head/PR remain immutable. The first observed tip alone is not proof that
all containment/check validation succeeded.

Product regressions promote the exact V6 loopback loss and canonical Badger clean
reopen probes, preserving the original evidence copies. Additional regressions
exercise concurrent negative/partial reads versus the original Finish, repeated
failure, later green checks, read authority denial and base advancement with the
same merge. Actual loopback HTTP and actual Badger history/reopen are exercised;
authority/Source fixtures remain synthetic. `.acceptance/v7/` records builder
normal/race/vet, current-source independent overlays and rebuilt frozen identity.
This is not fresh independent acceptance, production wiring or live GitHub proof.

## V2 threat and GitHub support envelope

Local mutation is allowed only at the exact approved owned root. The target ref
must not be checked out in the root OR any linked worktree (including locked or
prunable records returned by Git). A valid existing HEAD is required; detached
HEAD is permitted. Caller HEAD/index/dirty paths are never intentionally changed;
all commit staging uses a private index. Source custody must prevent concurrent
checkout/ref/filesystem changes. This is not a hostile same-UID sandbox.

Closing syntax is conservatively denied in actual proposed commit message,
proposed file bytes, and PR title/body: all nine documented closing verbs,
case-insensitive, optional colon/whitespace/newlines, same-repository numbers,
owner/repo references and full URLs. False-positive source examples are denied
rather than interpreted as safe Markdown. A Privacy implementation is still required.

Merge requires independently reading classic GitHub branch protection AFTER final
admission and adjacent actor/permission revalidation. `ProtectionPolicy` pins the
complete returned JSON policy (canonical `digest`), not a caller protection bool.
Strict required checks must be enabled, enforcement for admins enabled, and the
complete check-name set equal to controller RequiredChecks. No bypass allowance
users/teams/apps are accepted. Only the independently read built-in `write` role
is supported; admin, maintain and custom roles deny. Active rulesets deny; missing,
unreadable, unknown or disabled protection denies. App-pinned check-source rules
are not yet supported (only classic contexts or app_id=-1); they deny. We do not
request or mutate branch-protection settings. Full-scope issue closure is unchanged.

The last client base/head/PR reads catch known drift but cannot remove the final
network race. The safety boundary for an advance after those reads is **GitHub's
server-enforced strict up-to-date rule**, with exact head SHA in the merge request
and a non-bypass actor. Our fixture actually rejects a late stale merge at the
server; it does not return a synthetic successful merge for a stale base. An
attempted request with uncertain/denied response remains UNKNOWN, never replayable.
Exact-success checks remain mandatory locally: GitHub can treat skipped checks as
green. Trusted administrators must keep the pinned policy enforced during the
operation; hostile administrator protection changes are outside this envelope.
A rejected final guard advancement and server-side last-window rejection are
separate regressions. This is **not live GitHub qualification**.

Documentation basis:
- https://docs.github.com/repositories/configuring-branches-and-merges-in-your-repository/defining-the-mergeability-of-pull-requests/about-protected-branches
- https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks
- https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue

The existing-PR update operation requires one exact PR number, non-draft/open,
matching repository/head/base and creator actor, privacy-reviewed new text, its own
reserved intent and GET readback. It never POSTs a replacement PR. An in-scope
`dirty`/nonmergeable conflict produces persisted Failed/merge_conflict evidence
accepted by existing ForwardRepair/RepairOwner; no builder, conductor or budget is
added. Actual owner integration and repeated repaired-revision transitions remain
unimplemented. Local-to-remote accepted object transfer is now implemented in
controlled local Git/synthetic HTTP qualification; actual remote transfer is NOT EXERCISED.

## Integration status and remaining required interfaces (NOT shipped)

| Owner | Required bridge / remaining gate |
| --- | --- |
| #376 source/custody | Implemented application `ContributionDeliverySource.WithAccepted` reloads sealed intake and the real accepted candidate under kernel callback custody. Actual native #377 quiescence/stop acknowledgement and production controller wiring remain unavailable. A read-only returned candidate alone is not custody. |
| #376 accepted Git identity | Implemented `Git.ProjectAccepted` validates the complete accepted tree/inventory and projects a distinct publication commit with reviewed policy author/committer, timestamp and message. The synthetic freeze commit is not published. Trusted actual authorship review and real remote qualification remain external. |
| canonical persistence owner | Implemented on concrete canonical Badger Store: `ContributionDelivery`, `Reserve/Finish/Fence`, exact sealed intake/Queue/run binding, retained repository/base/branch custody, immutable history, CAS and audit. External actual-current-fence Authority remains mandatory; no production #377 authority or conductor wiring is installed. |
| #377 authority/controller | Fresh exact role/mandate/current narrowing/stop and actor/repository/ref/privacy admission; authorized read-only reconciliation after stop/revocation where permitted. Existing credential authority `Use/UseResolved` is a possible primitive, not installed delivery scopes. Do not widen policy automatically. |
| credential/privacy owners | `CredentialCustody` implements exact pinned existing binding/deployment/destination/record/version/revision and closed operation/repository/actor checks using `Authority.UseResolved`; no create/bind/grant/widen. Production owner construction and trusted `Privacy` audience review remain required. No encrypted-authority fixture or live credential use is claimed in this cycle; loopback HTTP is synthetic-only. |
| object transfer | Implemented: closed local extraction and remote blob/tree/commit create/readback with canonical local OIDs, reserved child intents and read-only UNKNOWN reconciliation. Existing owner source/custody must supply the accepted graph projection; no production wiring or actual remote transfer is claimed. |
| #376/#377 repair | Real cumulative candidate/infrastructure budgets, same-builder handoff, rebuild/refreeze/fresh independent acceptance and updated exact-head checks. `ForwardRepair` is a port, not a running repair loop. |
| #375/#377 orchestration | Ordered observable Loop/check/readback operations and durable controller wiring into existing application/runtime; no new daemon/queue. The typed application Source and synchronous consumer exist; automatic manager/conductor delivery continuation and real native #377 acknowledgement remain unavailable. |
| #379 verifier | Independently reload and join actual accepted source, roles, checks, merge, completion and issue/result readback before disposition. Merge alone is not full success. |

Delivery restrictions use `internal/contribution/policy.go` canonical values:
`local-only`, `no-publish`, `no-merge`, `verified-protected-base-merge`. The source
owner must supply current effective restriction from canonical control history;
this package does not independently resolve that history.

## Evidence and acceptance

Current evidence: observed source and controlled Go/in-memory/HTTP plus local real
Git fixtures. Normal/race tests and vet qualify these adapters only. A built
executable containing an unwired package does not prove its use. Fresh independent
candidate testing, real managed-provider journey,
exact authorized GitHub actor/repository/base/issue/controller binding, live
publication/merge and retained authoritative result remain NOT EXERCISED.
Clarification timeout is not human denial. No remote acceptance or live Loop was
attempted. Specification, source, synthetic qualification, managed-provider,
revision-matched package, installed, active and accepted-live tiers are distinct.
See [launch ledger](implementation/issue-378-launch-review.md).

## V3 object-transfer envelope

`Git.ExtractObjects` reads exact SHA-1 OIDs through closed local `cat-file`
plumbing, verifying canonical raw Git hashes. It never reads unrelated dirty
files, invokes a shell, pushes, or uses credentials/network. The positive profile
allows blobs, ordered trees (100644/100755/040000), and one-parent unsigned UTF-8
commits with exactly tree/parent/author/committer headers. Signatures, encoding,
merges, symlinks, gitlinks and extensions deny. Actual raw author/committer dates,
identities and message are preserved; the source must establish reviewed actual
authorship rather than publish the existing synthetic #376 freeze identity.

Bounds: 512 KiB/object, 16 MiB/graph, 2048 objects, tree depth 32. Known private
filenames and secret patterns deny conservatively; comprehensive audience/content
review still belongs to trusted Privacy. Unreachable extra objects deny.

An immutable whole-graph operational reservation precedes separately reserved
OID-specific child effects, all under the same contribution/publication owner.
This is not another frozen candidate or acceptance PASS. Changed graph/candidate
conflicts before child effects, including after UNKNOWN. Upload payloads use
base64 blobs, explicit reviewed tree paths/modes/approved references, and exact
commit metadata. Returned OIDs and GET readback must agree. UNKNOWN absence never
authorizes resend. Dependencies and approved parent must exist before commit
creation; the complete accepted graph must be remote before ref publication.

Existing GitHub reads now resolve custody per request; mutations resolve fresh
custody after final checks/admission rather than retaining earlier plaintext.
`CredentialCustody` consumes only exact existing pinned approved bindings and
cannot create/bind/grant/widen. Production construction and #377 admission remain
external. Real encrypted-authority fixture coverage was not added in this cycle;
no live credential, actual remote transfer, durable integration or whole #378
completion is claimed. Controlled local Git and synthetic HTTP results are not
actual-live qualification.


## V5 tester-finding repairs and immutable admission pins

V4 independent findings are repaired in the adapter envelope, not promoted to
whole-product acceptance. Empty local Git trees use an explicit nonnil `[]`
create payload and require canonical nonnil `[]` readback. The regression deletes
the last tracked file in actual isolated Git and preserves unrelated private work.

Classic server protection must contain required PR reviews, an approval count at
least the exact independent reviewer count, stale-review dismissal and independent
last-push approval, as well as existing strict-check/admin/no-bypass requirements.
After the final guard, exact current-head reviewers are reread inside the SAME
fresh custody callback that performs merge. Actor, repository identity and push
permission are checked with that same credential for every remote mutation,
including object uploads. Separate callback identity is not write admission.
Client reads are not atomic: the server's enforced review/strict rules must reject
withdrawal or a new push in the final request window. Fixtures test both a local
final-guard dismissal with zero writes and a real loopback handler rejecting a
late dismissal; attempted rejection stays UNKNOWN and unmerged. These are not
live GitHub policy qualification.

`Plan.FenceEpoch`/`Intent.FenceEpoch` are explicit nonzero immutable epoch pins;
`RoleBinding` is the immutable admitted-role binding digest with strict SHA256
syntax. They are part of semantic payloads, never part of stable effect-slot IDs.
A replacement epoch or role cannot rotate a slot to escape UNKNOWN. Adapter-only
fixtures explicitly label the synthetic role-binding input as NOT real authority.
These pins themselves grant no authority: fresh #377 admission is still required.

The bounded persistence continuation now supplies concrete
`Store.ContributionDelivery(ctx, binding, authority)` without changing the common
fleet repository interface or inherited Badger lifecycle. Family 0x79 stores
sealed digest-linked observations and checked head revisions; 0x7a retains
repository/base publication custody and exact reserved slots across all phases.
Reservation, observation, head and canonical audit share `Store.update`.
`History` returns exact stored intents and all verified observations. Canonical
JSON rejects duplicate/unknown keys, noncanonical bytes and missing/rolled-back
history/head. A dedicated disk envelope preserves both Outcome fields in the
existing embedded `delivery.Record` without losing the observation outcome.

Constructor binding reloads existing sealed contract plus actual Queue/run and
request/issue indexes. Candidate remains an existing digest reference, not another
candidate or PASS format: accepted membership/quiescent custody still belongs to
Source and actual #377 admission. FenceEpoch and RoleBinding are pinned, not
minted. External Authority is called outside transactions, before fresh admission
and again after it; nil/unavailable denies. Sticky stop/current narrowing and
sealed deadline deny new writes while admitted reads may reconcile prior effects.
There is no cross-store/provider atomicity claim and no production synthetic fence.
The real #377 writing admission remains unavailable pending native quiescence;
this adapter does not bypass it.

Builder regressions use actual clean Badger close/reopen and concurrent adapters,
with explicitly synthetic sealed intake and authority fixtures. UNKNOWN remains
read-only, semantic/epoch substitutions conflict, terminal facts and merge/tree
identities cannot be erased, and cancellation alone never releases publication
custody; exact terminal/quiescence release follows the #423 boundary above.
No live Loop, remote effect or production conductor wiring is claimed.

Builder normal/race/vet and architecture logs plus source/executable/toolchain
identity are retained under `.acceptance/v5/`. Prior tester evidence is immutable;
fresh independent parent testing and the remaining canonical integration are not
replaced by these builder regressions.


## v12 context-preserving custody seam

`delivery.Source.WithAccepted(ctx, ref, func(context.Context, Plan) error)`
passes the **exact** owning callback context to Reserve, Fence, Authority,
adapter, Finish and every graph child. Caller cancellation is checked without
replacing the private context. Retained callbacks and guards cannot authorize
work after synchronous return; duplicate callbacks do not reacquire custody.

The productive app constructor requires the exact observed #377 service method
`WithContributionContinuityCandidateAs(context.Context, core.Subject, string,
string, func(context.Context, orchestration.ContributionCandidate) error) error`.
Its session argument is the explicit reviewed `ControllerSession` policy pin
(included in the policy digest), never the manager/intake/builder session.
#377 owns private proof creation, lifetime and current controller-role admission;
trusted per-effect authority must validate the matching session through #377's
actual effect admission. Neither policy resolution nor an Authority port grants
rights by itself. The productive runner invokes #377 exactly once and never
nests a second #376 lock. On this #376-only base, absent #377 returns
`ErrContributionDeliveryUnavailable`; missing session pins deny. No fake receipt
or automatic qualification fallback exists.

`NewQualificationContributionDeliverySource` explicitly selects the existing
#376 lock-held kernel with a **synthetic** gate for tests only. The manager-to-
Source qualification remains real #376 kernel evidence, not productive #377,
actual auto-run, provider, remote delivery or installed runtime acceptance.
Sentinel port tests and fake owning-runner transport tests are separately
synthetic context-transport evidence. Actual #377 runtime proof remains
UNAVAILABLE until its owner integration and fresh independent acceptance.
Policy slices are detached, and policy/current controls are checked before and
after the synchronous action under custody. No model inputs or private tokens
are exposed. Launch assets outside this dedicated API/gate documentation are
unaffected by this internal integration seam; no release/deployment is claimed.
