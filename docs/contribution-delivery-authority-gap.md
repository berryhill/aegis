# Contribution delivery composition and remaining gates

The bounded [#423 custody/recovery correction](contribution-publication-custody.md)
adds proof-gated sequential handoff and original issue-intent continuation. It does
not supply the independent authority or broader qualification described below.

## Implemented private-source composition

`InstallContributionDeliveryController(service, exactServerPolicy,
existingCredentialAuthority, privacy)` snapshots validated trusted configuration
on the **existing same-service** `ContributionDeliveryPublicationOwner`. Its mutex
protects one immutable controller. Identical setup returns that controller;
conflicting policy, authority or privacy rejects. Setup is not a request API and
supplies no default policy, credential, grant or approval. Install the existing
publication owner first, during trusted startup before concurrent requests.

Manager `/contribute run` now calls `ExecuteContributionJourneyAs(ctx, subject,
key)`. The actual existing `ExecuteContributionAs` retains native run registration,
preparation, original task binding and candidate execution. Only a canonical
`candidate_accepted` result continues automatically. The wrapper reloads canonical
controller session/role/epoch and fresh role admission, then invokes the installed
concrete controller in the **same request**, without another task consent, receipt,
operator-supplied role or manual `RunAs` call. Publication `enter` and canonical
sticky Stop fence the post-build cancellation gap. Replays retain original native
attempts, candidate budget, role identity and canonical delivery slots.

The manager returns both candidate execution and transient delivery readback,
with a closed held stage/reason. Missing composition is explicitly
`delivery_controller_unavailable`, never candidate-only completion or readiness.
Held results use the existing auditable `unavailable` outcome, not an unsupported
`blocked` audit outcome. Status/resume/cancel routing is unchanged.

## Authority and completion boundaries

Productive delivery derives immutable intents from existing closed Engine builders,
checks fresh native private custody and runtime admission for every operation
(including productive CheckDelivery), current instruction/control anchors, exact
server policy and canonical PR history. Historical read-only reconciliation stays
separate. Local-only/no-publish performs only scoped Git Commit without consulting
remote credentials. Remote delivery requires genuine existing exact pinned enabled
credential custody, runtime credential scope, trusted privacy review and all actual
operation approvals; no ambient credentials or accepting fallback.

Repair, readiness-budget and #379 completion owners remain nil and explicitly
held. A local target, PR, CI or merged delivery is **not** whole-contribution
completion: manager `completed` remains false. No #379 implementation or competing
repair owner is introduced. No remote GitHub/provider/installed-target acceptance,
release, deployment, service exposure or host grant is claimed by this change.

## Narrow readiness reservation port — canonical contract blocked

`(*Service).reserveContributionDeliveryReadiness` is an unexported controller-only
owning port, not a generic continuity mutation API. It repeats actual held private
Source custody, fresh principal/controller role/session/epoch/mandate, exact
CheckDelivery/Merge semantic intent, canonical UNKNOWN, current instructions,
control, deadline, candidate and Loop checks. Its accounting seam derives the next
global-slot operation ID, seals the real controller SessionID and performs exactly
one append through the existing canonical continuity repository. Any CAS/storage
error denies HTTP, retaining any uncertain committed charge; no found reservation
grants another caller permission. The two-attempt global ceiling is unchanged.

**Native positive readiness remains blocked:** `ApplyContinuity` currently rejects
`reserve` from any SessionID other than the original intake session (its explicit
role-event exceptions omit reserve). The first repaired native normal run reached
this canonical rejection with root count zero. No reducer change or session spoof
was made. Generic `RecordContributionContinuityAs` remains original-only. A narrow
canonical role-reservation contract requires separate owner authorization; the
port alone does not establish usable readiness. Accounting seam tests are not
private-custody proof. Repair evidence is retained in
`.acceptance/readiness-repair/REPORT.md`, distinct from the earlier tester freeze.

## Qualification

`TestContributionDeliveryLocalNarrowJourney` reuses the controlled, explicitly
nonprovider native fixture: manager command, actual clean native roles and Wait,
RED/GREEN accepted candidate, then automatic canonical Store Reserve/Finish and
Git Commit in the same request. It checks exact accepted tree/ref/history and
HEAD/index/private-dirt preservation, drift denial and same-key replay with no
new attempts, roles or budget. Empty isolated credential custody and deny
privacy/historical/credential counters must remain unused. The existing
candidate-only fixture now asserts the honest missing-controller hold.

Launch review: this is private source integration, not production setup or remote
readiness. Root README, license, security/contributing/conduct/changelog, threat
model, architecture, quickstart, no-key demo, recording and release/issue assets
must not be promoted to full contribution success. No launch asset or release
publication is authorized here. Full architecture retains the three known peer
layer violations unwaived; test evidence is kept under a new `.acceptance/journey-*`
namespace and does not overwrite older proof.
