# Contribution publication custody and issue recovery (#423)

This is a bounded source correction, not qualification of the complete #379
journey, actual-provider/protected-remote execution, or the #352 installed release.
It introduces no service, queue, daemon, generic lease, credential scope or grant.
The canonical contribution happy-path contract remains authoritative.

## History is not active custody

The existing Fleet Badger publication family retains the immutable contribution
root, exact intent slots, digest-linked observations and their legacy mirrors.
A root-local slot index separates membership from the current repository/base
owner. `delivery.PublicationRoot`, `HistorySnapshot`, intents, observations and
preclosure bytes keep their existing wire shapes. A later owner never rewrites
an earlier root, candidate, role, epoch, policy, budget or result.

The repository/base record is the active exclusion fence. Its complete sealed
root identity is non-reusable: a displaced historical root cannot reacquire it.
Historical `Reserve` replay and `History` remain read-only; active `Fence` and
`Finish` cannot authorize effects for a released or displaced root.

A successor can acquire custody only after all of these hold:

- Exact canonical terminal disposition and Queue projection/basis agree.
- Builder/tester native boundaries and stop acknowledgements remain exact;
  every recorded native effect is completed.
- The exact controller authority/epoch has an acknowledged owning-adapter Stop.
- Owning publication calls have drained and an immutable exact-root release seal
  has been retained.
- Every publication intent has a resolved latest observation. UNKNOWN Check,
  graph transfer and merge-with-known-SHA also prevent handoff. A merge SHA alone
  does not establish successful post-merge validation.

The store transaction rechecks those canonical facts when transferring the active
pointer. Elapsed time, mandate expiry, PID absence and an empty in-memory map are
not release evidence. Existing source/workspace custody remains separate.

Terminal-only cleanup can retry the terminal-before-release crash boundary on a
fresh authenticated invocation. It stops only the exact retained controller;
when genuine cold recovery is required it uses the existing validated workspace
recovery boundary and exact adapter identity proof. It cannot resume execution,
renew authority, reset budgets, or manufacture an acknowledgement for a missing
process. Unavailable proof keeps custody held. The next same-owner contribution
selects the predecessor from canonical repository/base custody, not caller IDs.

## Legacy and damaged records

Legacy roots remain readable and same-intent replay remains idempotent. A legacy
active owner may materialize its complete root-local index during normal new-slot
reservation, without deleting or rewriting old slots/history. An unindexed legacy
root is not silently reclaimed. Missing/corrupt ownership, markers, slots, mirrors,
roots or history are not interpreted as free custody. Membership is checked in
both directions so deleting one indexed UNKNOWN slot cannot hide it. No destructive
migration or clock-based compatibility fallback is introduced.

## Original issue-close continuation

A reserved issue reconciliation remains recoverable rather than being converted
into a terminal merged-unresolved boundary merely because its result is UNKNOWN.
A new invocation checks the original issue slot before replaying earlier delivery
operations. It reloads the exact original historical policy and revalidates the
original pre-issue history/native/claim/attempt and canonical continuity prefixes
against the existing content-addressed preclosure. It never substitutes a new
preclosure, intent, candidate or operation budget.

A renewed controller is admitted independently under current authority. Only its
controller-session/role/epoch provenance may differ; changing publication payload,
checks, repository, candidate, credential pin or other policy fields is not a
compatible recovery. The original intent/policy remains an immutable read target,
not a grant to reuse old productive authority. Active repository/base custody is
still required; recovery cannot issue effects after handoff.

Unknown issue effects use exact authoritative readback only. An open/unavailable
or inexact issue stays held; there is no repeated PATCH or new reservation. An
already confirmed canonical issue observation is read unchanged, not replaced
with a newly timestamped proof. Exact confirmed issue/preclosure evidence plus
fresh finalization admission can reach the same canonical terminal winner. Cold
credential use is restricted to the installed owner's synchronous historical-read
permit and cannot authorize Apply. Missing/corrupt preclosure or unsupported
read authority denies rather than reconstructing success from narration.

## Verification and evidence limits

The regression fixture exercises real native preparation/acceptance, the actual
application verifier/finalizer and Fleet store, and production delivery code
against loopback GitHub protocol fixtures. Initial RED reproduced both local-only
and successful merged A followed by blocked B, plus interrupted original issue
reconciliation. B's direct reservation probe uses explicitly synthetic reservation
authority; it does not establish a separately qualified real-provider B execution.

Focused coverage includes sequential custody, retained A result/history, stale
A fence/finish denial, same-intent read replay, concurrent acquisition, missing
indexes/mirrors, conservative legacy records, unchanged budgets and issue intent,
controller re-admission, and canonical Fleet close/reopen around issue closure.
A Fleet reopen with reconstructed consumers is not a claim that all authority,
runtime and installed-service processes were restarted.

Run the tagged controlled journeys using the repository's existing finalization
overlay procedure and shared lifetime port helper. Ordinary/race commands, frozen
source/executable identities, independent acceptance results and exact-head CI
belong in the task's retained evidence and PR. Passing selected cases does not
imply a complete suite or live qualification. No release or installation is part
of this correction.

## Launch-asset impact

Changed: this engineering guide, the delivery/readback guide pointers and the
Unreleased changelog. No CLI syntax, public UI, installation command, runtime
provider, configuration schema, release archive, trust grant or network exposure
changes. Root README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, threat
model, architecture, quickstart, no-key demo and recording retain their separate
scopes; none is promoted into contribution qualification. Existing release
binaries/checksums are unchanged. #423 owns this source correction; broader
#378/#379/#352 acceptance stays open.
