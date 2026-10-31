# Conversational operations (#248 source candidate)

The manager model proposes a closed `platform.call` envelope with arguments
`{"operation":"agents.list","input":{}}`. This is a general typed dispatcher,
not a phrase scanner or shell/tool bridge. Ordinary first/changed prompts supply a
compact operation index with lossless positional metadata rows
`[operation,mutation,service,availability,reason]`, and a `catalog_digest` binding
the entire canonical catalog, including schemas. An unchanged delivered index is
referenced by digest; failed delivery and new sessions do not inherit that claim.
Capabilities and principal admission are still fetched every turn.

The existing `platform.discover` has one closed additive extension:
`{"operation":"loops.show","catalog_digest":"sha256:…"}` requests exactly one
complete capability, including its reflected typed `input_schema` and every
metadata field. Both nonempty string fields must be present together. Unknown or
ambiguous exact names and changed digests fail without preparing/executing an
effect. The response binds `catalog_digest` and `capability`; it is retained as
untrusted result data for the next model turn so required inputs can be collected.
The complete compact JSON response is bounded to 24 KiB; oversize returns an
explicit error, never a partial/truncated schema. Every current catalog capability
fits this bound (regressed). Strict owning-service validators remain authoritative.

Legacy empty discovery `{}` retains full-catalog semantics and its existing
64 KiB result-rendering bound (the current full corpus exceeds that bound and
returns an oversize error). The adapter's full `Capabilities` API is unchanged;
ordinary model schema collection should use scoped discovery, not empty discovery.
No schema corpus is silently cut down to satisfy a size limit. History and all
existing user/model/result inference scanning remain intact, at the unchanged
production 250ms deadline.

Unknown fields, duplicate keys and trailing JSON are rejected. Root authority,
publisher, workspace, subject, principal, approval, mandate and stanza fields are
not accepted from the model. `agent_id` is a selector, not authenticated identity.

## Shared authority and confirmation contract

Every exposed row below requires the captured authenticated principal, checked
again at execution. Workspace/publisher/Queue authority is derived by the owning
application service from that subject; no model claim grants authority. Reads
execute without confirmation. **M** rows prepare immutable normalized bytes and
require the operator's subsequent exact `confirm sha256:…`; `cancel` discards them.
Expiry is five minutes or route expiry, whichever is earlier. Confirmation is
consumed before execution; duplicate confirmation cannot execute twice. A stale
revision fails canonical compare-and-append; it is never silently re-resolved.
This confirmation does not approve a charter, provision a host or grant runtime
rights. Mutation results and errors come from Aegis, not model success narration.

## Exposed matrix

All entries are implemented and exposed in the local terminal and conversational
gateway. `object` means `{id,revision?}`; exact definition reads require a positive
revision. Canonical request types below are in `internal/app` (aliases may point
to orchestration types). The live discovery schema is the exact field inventory;
this table is a compact navigation index, not an alternate permissive schema.

| Operations | Input/schema | Owning service(s) | Effect |
|---|---|---|---|
| agents.list; agents.show; agents.history | `{}`; object; `{id}` | ListFleetAgentsAs; GetFleetAgentAs; ListFleetAgentRevisionsAs | read |
| agents.register | RegisterFleetAgentInput: fixture + exact identity | RegisterFleetAgentAs | M |
| agents.enable; agents.disable; agents.retire | SetAgentLifecycleInput, exact expected ref resolved before review | SetAgentLifecycleAs | M |
| loops.list; loops.inventory; loops.show; loops.availability | `{}`; `{}`; exact object; `{id}` | ListLoopsAs; ListLoopInventoryAs; GetLoopViewAs; GetLoopAvailabilityAs | read |
| loops.validate; loops.publish | PublishLoopInput: agent_id, canonical revision, stable idempotency key | ValidateLoopAs; PublishLoopAs | read / M |
| loops.activate; loops.retire | SetLoopLifecycleInput, exact loop ref | SetLoopLifecycleAs | M |
| loops.archive; loops.restore | SetLoopAvailabilityInput, exact head ref | SetLoopAvailabilityAs | M |
| graphs.list; graphs.show; graphs.lifecycle | `{}`; exact object; `{id}` | ListGraphsAs; GetGraphAs; GetGraphLifecycleAs | read |
| graphs.publish | PublishGraphInput | PublishGraphAs | M |
| graphs.submit | SubmitGraphInput, exact graph, agent_id, normalized inputs, bounded max_attempts | SubmitGraphAs | M; admission is not execution |
| submissions.list | `{}` | ListSubmissionHistoryAs | read |
| loops.queue | QueueLoopInput, exact Agent and Loop refs | QueueLoopAs | M; supported exact Run, independent runtime gates retained |
| queue.list; queue.preparations; queue.show; queue.result | `{}`; `{}`; `{id}`; `{id}` | ListExecutableQueueAs; ListPreparationsAs; GetQueueItemAs; GetQueueResultAs | read |
| queue.cancel; queue.revoke; queue.expire; queue.exhaust | TerminalQueueItemInput, queue_item_id, optional workspace agent selector | CancelQueueItemAs; RevokeQueueItemAs; ExpireQueueItemAs; ExhaustQueueItemAs | M |
| queue.retry | RetryQueueItemInput with reclaimed=true | RetryQueueItemAs | M; expired-lease reclaim only |
| queue.preferences; queue.preferences.set | `{}`; SetQueuePreferencesInput | GetQueuePreferencesAs; SetQueuePreferencesAs | read / M |
| queue.debugger | `{id,cursor?}` | ExecutionDebuggerAs | read |
| queue.archive.status; queue.archive.set | DigestRef; SetQueueArchiveInput | GetQueueArchiveAs; SetQueueArchiveAs | read / M |
| queue.requeue | RequeueQueueItemInput | RequeueQueueItemAs | M; owning service validates eligibility |
| loops.task.show; loops.task.save; loops.task.execute | `{id}`; DoerDraftInput; ExecuteDoerTaskInput | ReadDoerDraftAs; SaveDoerDraftAs; ExecuteDoerTaskAs | read / M / M |
| loops.readiness | DoerCandidateReadinessInput | ReadDoerCandidateReadinessAs | read; observation grants no rights |
| contribution.result; contribution.continuity; contribution.evidence; contribution.delivery | `{id}` | GetContributionResultAs; ReadContributionContinuityViewAs; ReadContributionCandidateEvidenceAs; ReadContributionDeliveryHistoryAs | read |
| charter.list; charter.show; charter.history | `{}`; exact object; `{id}` | ListAgents; GetCharter; ListCharters, behind principal adapter | read |
| charter.validate; charter.import | `{charter: canonical JSON}` | ValidateCharterAs; ImportCharterAs | read / M; import is not approval |
| plan.preview; plan.list; plan.show | `{id,revision,environment}`; `{}`; `{id}` | PreviewPlanAs; ListPlans; GetPlan | M / read / read; preview persists review |
| approval.list; approval.show | `{}`; `{id}` | ListApprovals; GetApproval | read only |
| provision.receipts; provision.receipt | `{}`; `{id}` | ListReceipts; GetReceipt | read only |
| session.list; session.show; session.mandate | `{}`; `{id}`; `{id}` | ListSessions; GetSession; GetMandate | read only |
| controller-codex.show | `{}` | ReadControllerCodexAs | read only |
| agents.prepare | canonical charter, current-fleet fixture, fleet_id, source_id | PrepareFleetAgentRegistrationAs | non-authorizing read/preview |
| approval.request | exact plan id, ttl_seconds 1..3600 | RequestApprovalAs | M; pending request, NOT approval |
| runtime.discover | {} | Runtime | read, runtime discovery only; no config secrets |
| session.revoke; session.terminate | exact session id and reason | RevokeSessionAs; TerminateSessionAs | M; canonical lifecycle and readback |
| audit.delivery.status; audit.projection.verify | {} | AuditDeliveryStatusAs; VerifyAuditProjectionAs | read/verification |
| audit.deliver; audit.projection.rebuild | limit 1..100; {} | DeliverAuditAs; RebuildAuditProjectionAs | M; canonical delivery/projection only |
| audit.query; audit.verify | `{}` | AuditEventsAs; VerifyAuditAs | read / verification |

Stable `graphs.submit` idempotency keys deterministically retain generated
submission/snapshot/Queue/run/transition/rejection identities for the principal.
Changed semantic payloads still conflict in the admission service. Omitted keys
are generated before review: retain the reviewed key/IDs for uncertain-outcome
reconciliation, never replace them just to retry. Pending reviews are memory-only.
A fresh conversation/restart denies an old confirmation and must rediscover exact
persisted objects, history and results; missing pending state is not completion.

## Protected and unavailable are distinct

* `agents.approve-charter`, `charter.approve`, `provision.apply`,
  `controller-codex.connect`: existing backend operations, protected principal
  decisions/custody; deliberately not granted by ordinary conversation.
* `queue.bind-runtime` and `queue.process`: existing backend services with
  controller-issued runtime authority, exact bounded worker identities and fresh
  admission. Their omission is a **protected authority boundary**, not an arbitrary
  product scope cut or a claim the backend is missing. Use the supported exact
  `loops.queue` path, which retains those independent gates.
* `session.start`: backend exists; clean session/independently admitted mandate
  cannot be inferred from a proposal. `secret.create`/`secret.rotate` require the
  existing protected intake protocol, never credential values in `platform.call`.
* `gateway.install`, `update.apply`, `reset`: separate reviewed workstation,
  executable or destructive lifecycle surfaces, absent from this adapter.
* `graphs.activate`/`graphs.retire`: no independent canonical Graph lifecycle
  mutation service (publication selects active revision). `queue.ordinary-retry`
  lacks durable stop acknowledgement. `queue.schedule` has no general scheduler.
* `deployment.project`: specification only; no shipped CLI/API backend for enrollment, binding, projection signing/publication, pull, stage, activation, acknowledgement, revoke or rollback. `loops.mcp`: no shipped conversational MCP Loop adapter.
* `session.preview` calls PreviewSessionAs and issues/persists a mandate: it is not a read-only preview. It remains on the independent authority surface. `approval.decide` (approve and reject) retains exact protected review; pending `approval.request` does not decide.

The complete exported application-service inventory, including explicit client/internal boundaries and protected families, is in [the source inventory](CONVERSATIONAL_SERVICE_INVENTORY.md).

The exact raw-v2 / managed-v7 bounded two-node chain exists; **general scheduling**
is absent. Do not describe all multi-node execution as unsupported.

## Canonical JSON inputs and normal registration

Discovery includes the full current-fleet fixture (registry.CurrentFleetFixture, schema_version aegis.current-fleet.fixture.v1) and core.Charter object properties, including nested runtime, ownership, charter references and trust stanzas. Charter created_at is RFC3339 date-time; integer budgets, float values, typed map values and base64 byte slices retain their JSON wire kinds. Custom decoders for current-fleet lifecycle defaults, DoerDraftInput and LocalInference retain the documented object shape; strict owning validators enforce semantic bounds and null/presence rules beyond discovery. The bounded envelope is at most 256 KiB; fixture/charter validation remains canonical, never a model-defined schema.

Normal registration language reaches the general model proposal/input-collection path in gateway and terminal. Only missing-runtime/degraded mode returns safe typed-command guidance. Explicit slash controls are unchanged. Legacy direct audit.query is no longer advertised or decoded; use platform.call audit.query, which calls AuditEventsAs.

Instruction v9 / conformance v11 adds digest-bound operation schema discovery to the existing platform discovery, natural-language fleet read, typed integer budget and missing-registration-input cases; numeric string budgets fail evaluation. Synthetic executors do not certify a real model.

## Verification tier and gaps

`TestPlatformConversationProductionJourney` and authority-negative companions use
a synthetic model proposal producer with the actual gateway, manager decoder,
application services and Badger state. They prove typed dispatch, immutable
confirmation, authoritative provenance, publication/admission/cancellation,
stable submission replay and durable readback after reopening the store. Negative
cases cover cancellation, stale/duplicate confirmation, invalid/missing selectors,
malformed/duplicate keys, model authority, expired review/subject, foreign principal
and new-conversation pending denial. Principal invalidation is tested separately
from a durable session-revocation journey; it must not be mislabeled as that proof.

These tests do **not** prove paraphrase interpretation by a real model. No live
provider, installed workstation, Console browser acceptance or full operation-by-
operation integration qualification is claimed. New expertise/instruction digests
invalidate old certification; real model recertification remains required. The
parent's independent frozen-candidate acceptance and publication are separate.
