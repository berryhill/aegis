# Retained contribution continuity implementation guide

## Owner discovery and safe checkpoint readback

`/contribute status [issue-url]` and `/contribute resume [issue-url]` resolve
through existing Queue inventory and `GetContributionForQueue`, not a new index.
No reference selects only one same-owner nonterminal contribution; ambiguity is
an explicit conflict, never latest-wins. Canonical issue URL matches are exact;
opaque retained keys remain compatible. Terminal, foreign-owner and substituted
immutable Loop references deny. Observation restores the exact selected Loop/key
but starts no runtime and grants no continuation authority.

`internal/app/contribution_discovery.go` owns resolution;
`internal/app/contribution_readback.go` owns the typed allowlist projected by
`internal/managergateway/contribution.go`. It reports stage, lineage separately
from frozen digest, exact Loop/run/Queue, independent used/limit counters, role
epoch and stop-request/ack booleans, closed decision outcomes and unresolved native
intent IDs. No PID/start/boot, command/environment, private path, runtime home or
receipt secret is copied from internal continuity. `completed` remains false;
checkpoint observation is not #379 whole-contribution disposition.


The [canonical contribution contract](../../.hermes/contribution-happy-path-contract.md)
is the acceptance oracle. This guide describes source implementation, not #379
completion, real-provider qualification, installed acceptance or publication.

## Retained invocation and admission

Use `/contribute resume [reference]` in a freshly authenticated same-owner manager
session. An authorized Resume reloads the original contract and restores its
immutable Loop selection. It retains the original Queue/run/claim/attempt, scope,
absolute deadline, source custody and cumulative counters. Login is not approval
or new task consent. Compatible replacement role epochs use independently admitted
clean sessions; the immutable initial RuntimeBinding and expired leases are not
rewritten. Doer same-session controls and qualification tiers remain unchanged.

`internal/app/contribution_resume.go`, the owning execution observer, and the
ManagedContribution checkpoint path retain held freeze/test continuation. The
accepted controller is retained through handoff; builder/tester shutdown and
failure cleanup still require exact owned process Wait/group-empty proof.

## Protected provisioning decision

`ExecuteContributionAs` invokes a trusted owning-service delivery port only when
an exact sealed role lacks verified provisioning. The real API constructor installs
`internal/api/contribution_decision_delivery.go`: it resolves the intent from the
canonical contract/current stage/candidate/control/role, reserves delivery before
launch, and invokes the existing inode-pinned hidden companion with FD3 bearer
custody and the exact owning config. No arbitrary command, automatic approval,
new queue or effect store is introduced.

Native password login and session/purpose-bound exact plan review remain required.
The native review reuses the already reserved canonical plan without spending twice.
A companion's `approved` output alone is unknown: canonical decision, exact plan,
consumed approval and verified matching provisioning receipt must be reloaded.
Ordinary fresh admission follows before any execution. Missing helper/transport is
unavailable; timeout, cancellation, denial, expiry and unknown remain distinct.
Same-intent replay reads retained status and never launches another helper, including
requested/applying/unknown crash boundaries. Reservation is not delivery evidence.

## Native/source custody and delivery seam

Native command bindings use stable `run-pass-N` lineage before a binary exists.
Frozen manifest identity is `CandidateEvidence.Digest`, not that lineage. The
lock-held `WithContributionContinuityCandidateAs` joins the exact accepted frozen
candidate to current lineage, same original attempt/run/policy, completed closed
native inventories and acknowledged builder/tester stops. Both roles need current
lineage coverage; bounded earlier pass lineages are historical, never substitution.
`CheckContributionEffectAs` takes the actual frozen digest. Writes require its
private synchronous live custody context; outside or retained/escaped contexts deny.
Unknown native outcomes retain custody. `writing=false` is a distinct fresh
same-owner historical observation check: exact contract/run/Queue/frozen candidate,
original role-authority reference and epoch, and approved contract scope remain
required. The session argument identifies the observing session on this path; it
need not revive the historical runtime. Cancellation, role stop and runtime expiry
do not deny this read or mint a new mandate, authority, event or counter. #378 must
separately admit the exact original effect intent, credential binding and typed
Read/Reconcile operation; this neutral check never authorizes a write.
These checks do not mint a GitHub effect or whole-contribution success; #378 owns
delivery records and #379 owns joined disposition.

Whole cancellation commits sticky stop before signalling the exact retained run
and waits at most 15 seconds for its actual done channel. Missing owner, mismatched
process identity, unknown native effects and failed/timed-out stops stay pending;
process absence is not acknowledgement. Fresh-owner cancellation after deadline
is safety cleanup only. Confirmed Queue terminal history is immutable.

The compiled #378 integration port is
`Service.ContributionPublicationQuiescer`, implementing
`QuiesceContributionPublication(context.Context, app.ContributionPublicationBinding) error`.
The binding carries exact `Contract` (Queue ID/digest), `RunID`, `QueueItemID`,
`Candidate` (frozen evidence ref), `Controller` (role-authority ref), and `Epoch`.
Trusted controller composition alone installs it. It is called only after sticky
cancellation; #378 must join its real `Repository.Fence`, in-flight effects and
unknown/confirmed same-intent remote readbacks, returning nil only for durable
quiescence of that exact binding. Missing port, missing candidate/controller and
port errors keep cancellation pending. No caller boolean/default allow, second
remote effect log, remote retry or credential grant exists here. Local-only has no
authorized remote effects. Nonlocal cancellation remains **unqualified** until
#378 implements and independently tests the actual owning-repository join.

## Evidence and remaining gates

Focused protected HTTP/owning-port tests cover false-approved output, unavailable
transport, replay and prelaunch reservation without Queue claims or provisioning.
The managed manager fixture adds the real source-lock/native/role/effect join and
outside/escaped-context denials. Its expensive full run must be independently rerun
on the final candidate; compilation is not that acceptance. Controlled fixtures are
not real protected human approval, real-provider execution or live qualification.
Parent retains full verification, freeze, independent tester and publication decisions.
