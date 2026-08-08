# Aegis Fleet-Control MVI Implementation Gap and Drift Review

Date: 2026-08-08
Audited revision: `379a86c9cb88ab7e4a5ba0bd04e3611984fab983` (`v0.2.1`)
Audit mode: current operator scope confirmation, prior-decision reconstruction, three independent read-only subagent lanes, parent source readback, executable repository verification, and iterative adversarial report review

## Correction notice

An earlier version of this report incorrectly treated the credential-centric vertical slice in `specs/MVP.md` as the complete Aegis MVI. That was a scope error.

The established product target had already been defined in prior Aegis design and audit sessions:

- Agent Registry = executable participants.
- Loops = reusable internal control flow.
- Graphs = versioned coordination and participant binding.
- Execution Queue = admission and operational lifecycle.
- Credentials = supporting infrastructure, deferred from the center of the near-term MVI.
- Contextual readiness = action-specific setup and repair.

The current operator correction in this audit is the controlling scope authority: register an existing fleet agent, allow that agent to create Loops and Graphs, and operate the queue within its context; credential details come later. Retained assistant design/audit readbacks corroborate that direction in session `20260802_115323_e88f62`, message `51893`, and session `20260807_143401_77d26c`, message `81035`. Those messages are supporting reconstruction evidence, not independent operator ratification of every prototype field or behavior. The latter readback records the OpenDesign target as a participant registry, versioned Graph and Loop definitions, execution operations, operator credential custody, and contextual readiness, with `/agents`, `/graphs`, `/loops`, and `/queue` as first-class product routes.

This report replaces the earlier scope and conclusions. Credential-broker findings may remain independently valid, but they are not the primary release gates for the fleet-control MVI requested here.

## Executive verdict

Aegis is **not feature-complete for the established fleet-control MVI**.

The repository contains a strong reusable authority and session-control substrate:

- strict canonical charters and immutable revisions;
- authenticated subjects and exact one-stanza selection;
- mandate and authority-context binding;
- append-only authority transitions and revocation;
- fresh exact-context runtime admission primitives;
- content-addressed artifacts and verification receipts;
- tamper-evident audit storage;
- deterministic provisioning and Hermes runtime integration.

But the product layer that defines the MVI is absent or disconnected:

1. There is no canonical registration record for an existing fleet agent.
2. There is no Aegis-owned Loop or LoopRevision domain.
3. There is no Aegis-owned Graph or GraphRevision domain.
4. There is no exact Graph-to-Loop revision reference or participant-binding model.
5. There is no durable product Execution Queue with enqueue, admission, claim/lease, attempt, retry, cancellation, and evidence-gated terminal semantics.
6. Existing `execution.Dispatch` and `execution.Turn` primitives are not persisted or integrated into an application queue path.
7. There is no immutable cross-domain submission/run snapshot tying Agent, Graph, Loop, authority, queue, runtime, artifacts, verification, and disposition together.
8. No CLI, API, test, or installed proof demonstrates the fleet-control vertical slice.
9. The highest-authority specifications and launch assets describe a different, credential/session-centered product and formally defer or erase the agreed fleet-control domains.

The accurate status is:

> Strong identity, authority, session, persistence, and evidence substrate; fleet-control MVI product domains and end-to-end loop not implemented.

## 1. Actual MVI contract

### 1.1 Product objective

The minimum viable implementation must prove one narrow end-to-end control-plane loop:

1. Register or reference one existing agent from the current fleet as an executable Aegis participant.
2. Preserve its stable Aegis identity and immutable charter/revision provenance.
3. Let that authenticated agent create one immutable, versioned Loop.
4. Let that agent create one immutable, versioned Graph that references the exact Loop revision and binds the intended participant.
5. Submit one typed Graph execution.
6. Resolve the exact authenticated subject, stanza, mandate, authority context, Agent revision, Graph revision, Loop revision, runtime, and typed inputs.
7. Fail closed with a durable rejected submission or admit one durable Execution Queue item.
8. Operate that queue item through explicit parent Graph and child Loop execution records.
9. Require fresh authority admission at consequential runtime boundaries.
10. Persist content-addressed outputs, verification claims, transitions, and terminal disposition.
11. Reconstruct historical execution truth after the current Agent, Loop, or Graph definitions change.
12. Expose active, queued, denied, failed, cancelled, expired, and completed lifecycle truth without treating projections or model narration as authority.

### 1.2 In-scope product domains

#### Agent Registry

A stable registry of executable participants already present in the fleet. Registration is not dynamic model-created agent provisioning. The minimum record must distinguish:

- stable `AgentID` from mutable display metadata;
- fleet-source identity/reference;
- runtime-adapter binding;
- immutable Agent/charter revision and digest;
- ownership/accountability;
- enabled, disabled, or retired lifecycle;
- capability declarations or policy references;
- current operational status as non-authoritative projection.

#### Loops

Reusable internal control-flow definitions. A Loop is neither an Agent nor a running session. The minimum contract requires:

- stable Loop ID;
- immutable Loop revision and digest;
- typed inputs and outputs;
- bounded steps and transitions;
- entry and terminal states;
- bounded retry/attempt policy;
- validation record bound to the exact revision;
- evidence requirements and explicit terminal reasons;
- no fallback to a mutable live definition after admission.

#### Graphs

Versioned coordination definitions. Graphs bind participants and exact Loop revisions without absorbing authority, queue state, or evidence into a mega-aggregate. The minimum contract requires:

- stable Graph ID;
- immutable Graph revision and digest;
- participant nodes bound to exact Agent revisions;
- nodes or edges bound to exact Loop revisions;
- typed dependencies and input/output mappings;
- stored validation against the pinned revision;
- admission constraints;
- immutable run snapshot;
- no model/runtime mutation of structure or participant binding.

#### Execution Queue

The authoritative operational lifecycle for admitted work. It is not the in-memory TUI event queue and not merely a runtime Dispatch. The minimum contract requires:

- queue item ID and idempotency/admission key;
- exact Graph run/submission snapshot;
- exact Agent, Graph, Loop, mandate, and authority-context references/digests;
- enough queued, active/running, denied/rejected, failed, cancelled/expired, and completed semantics to prove the first vertical slice truthfully;
- one deterministic claim/attempt boundary that prevents duplicate successful execution;
- attempt identity and a bounded first-slice retry rule;
- cancellation, expiry, and revocation propagation;
- durable transitions and rebuildable read projections;
- evidence-gated completion;
- stable reason codes.

### 1.3 Supporting substrate, not product completion

The following are necessary supporting controls but do not by themselves satisfy the MVI:

- charter canonicalization;
- authenticated subject derivation;
- exactly-one-stanza selection;
- mandates and authority contexts;
- runtime launch and process custody;
- authority admission and revocation;
- audit integrity;
- content-addressed blobs and verification receipts;
- encrypted credential custody.

#### Contextual readiness

Readiness is evaluated for the attempted action; it is not a universal onboarding gate. The minimum contract must distinguish:

- ready for the exact registration, publication, submission, claim, or execution action;
- denied by authority or policy;
- unavailable because an owning service or dependency cannot be reached;
- degraded or repair-required with bounded actionable remediation;
- genuinely empty only when the authoritative collection is successfully read and contains no records.

Credential setup must not block credential-independent Agent, Loop, Graph, or Queue actions.

### 1.4 Deferred credentials boundary

New fleet-control Graph/Loop credential integration and credential-centric release acceptance are deferred. Existing bounded Agent/stanza credential scopes, encrypted custody, bindings, and the narrow broker remain reusable supporting substrate. If a later Graph requires a credential, admission may resolve one exact non-secret grant reference. The defining fleet-control MVI proof does not require:

- generic credential retrieval;
- broad provider/action support;
- credential relationship administration;
- fleet credential projection/distribution;
- credential-centric launch acceptance;
- a typed GitHub operation as the defining product edge.

Existing credential custody may remain in the repository, but it must not block credential-independent Agent/Loop/Graph/Queue work.

## 2. Audit method and authority handling

The review used four evidence layers:

1. Prior product decisions and the retained OpenDesign target.
2. Current repository authority documents and launch claims.
3. Current source, persistence, CLI/API, tests, and installed proof.
4. Independent adversarial review of scope, source mapping, and remediation.

Three independent subagent lanes were used:

- target-contract reconstruction from prior sessions and retained design evidence;
- source implementation inventory against the exact fleet-control MVI;
- specification, storage, launch-asset, and historical-refactor drift review.

The parent directly reread the key current source surfaces. Subagent conclusions are treated as claims unless supported by current source or precise retained-session evidence.

No product source or normative specification was changed during the audit. The implementation plan created after this report is stored in PlanStore rather than the repository filesystem.

## 3. Obligation matrix

| MVI obligation | Status | Current evidence | Principal gap |
|---|---|---|---|
| Existing fleet-agent registration | FAIL | `Charter` has `AgentID`, revision, runtime and stanzas (`internal/core/model.go:89-103`); principal can import canonical charters (`internal/app/service.go:245-277`) | No AgentRegistration/AgentRevision/fleet-source model, repository, lifecycle, or agent-authenticated registration path |
| Stable immutable participant identity | PARTIAL | Canonical charter revisions and digests exist; immutable charter storage exists | Identity is inferred from charter directories rather than owned by a canonical executable-participant registry |
| Agent creates immutable Loop revisions | FAIL | No production Loop/LoopRevision types or services found | Entire domain, persistence, API/CLI, validation, authorization, and tests absent |
| Agent creates immutable Graph revisions | FAIL | No production Graph/GraphRevision types or services found | Entire domain absent |
| Graph references exact Loop revisions | FAIL | `LaunchContract` explicitly contains no graph (`internal/execution/model.go:70-78`) | No LoopRef, Graph node, digest pinning, or substitution denial |
| Graph binds exact participant revisions | FAIL | Mandates bind an `AgentID`, charter revision and digest (`internal/core/model.go:150-167`) | No Graph participant-binding record or admission-time resolution |
| Typed Graph submission | FAIL | Existing provisioning `Plan` models effects, not Graph execution (`internal/core/model.go:202-212`) | No submission aggregate, schema normalization, idempotency, rejection record, or run snapshot |
| Durable Execution Queue | FAIL | `Dispatch` and `Turn` have bounded states (`internal/execution/model.go:14-59`) | No queue repository, enqueue/claim/lease/retry/cancel/reclaim/application service |
| Authority-context-bound operation | PARTIAL | `Mandate`, `AuthorityContext`, exact validation, fresh admission, revocation primitives exist | Not integrated with Agent/Graph/Loop/Queue records because those records do not exist |
| Parent Graph and child Loop executions | FAIL | No production GraphRun/LoopRun/Attempt hierarchy | No causal execution hierarchy or persisted events |
| Immutable submission/run snapshot | FAIL | Authority and artifact digests provide reusable patterns | No cross-domain run snapshot preserving exact definitions and admission facts |
| Evidence-gated terminal completion | PARTIAL | RuntimeArtifact/VerificationReceipt and fresh blob verification exist | No queue/run repository consumes required verification claims before terminal success |
| Historical reconstruction after drift | FAIL | Audit and authority facts are reconstructable within their domains | No Agent/Loop/Graph/Queue execution chain to reconstruct |
| Product CLI/API | FAIL | Current CLI groups omit Agent Registry, Loop, Graph, and Execution Queue (`internal/command/root.go:249`) | No supported mutation/readback surfaces |
| Installed MVI proof | FAIL | Packaging and denial proofs pass | No clean-install demonstration of Registry → Loop → Graph → Queue → evidence closure |
| Design route parity | FAIL | OpenDesign target defines `/agents`, `/graphs`, `/loops`, `/queue` | Current application exposes no corresponding live product routes |
| Action-specific contextual readiness | FAIL | Existing onboarding/readiness checks are manager/session oriented | No readiness contract for Registry, Loop, Graph, submission, queue claim, or execution actions; unavailable/denied/degraded/empty are not consistently separated |

## 4. What is implemented and reusable

### 4.1 Charter and identity substrate — RETAIN, REFACTOR INTO REGISTRY

Current evidence:

- `core.Charter` carries schema version, Agent ID, display name, revision, runtime, stanzas, creator, and timestamp (`internal/core/model.go:89-98`).
- `CanonicalCharter` carries a digest and canonical bytes (`internal/core/model.go:99-103`).
- Principal-authenticated charter import validates and persists canonical revisions (`internal/app/service.go:245-277`).
- `Service.ListAgents` currently delegates to the generic store (`internal/app/service.go:303-308`).

Value:

This provides strict, immutable policy material for a registered participant.

Gap:

A charter is not a complete executable-participant registration. It does not express current-fleet provenance, registration lifecycle, runtime binding status, ownership/accountability, or agent-authenticated mutation authority. `ListAgents` is effectively an index of charter directories, not a first-class Registry.

Remediation:

Create a bounded Registry domain. Registry revisions should reference immutable charter revisions/digests rather than duplicate authority. Preserve stable Agent identity across display-name and charter changes.

Verification:

Register an existing fleet fixture; change its display name and publish a new charter revision; prove Agent ID stability, exact revision retrieval, disabled-agent admission denial, and no model-created enable/rebind path.

### 4.2 Authentication, stanza, mandate, and authority context — RETAIN

Current evidence:

- Authenticated subjects and exact selection inputs are modeled (`internal/core/model.go:105-149`).
- Mandates bind subject, Agent ID, stanza, charter revision/digest, runtime, capabilities, tools, scopes, and expiry (`internal/core/model.go:150-167`).
- AuthorityContext binds a mandate to exactly one session and effective authority (`internal/core/authority.go`).
- Selection rejects invalid or unauthorized contexts (`internal/app/service.go:343-410` and following).
- `execution.ValidateAdmission` requires a fresh exact-context decision (`internal/execution/model.go:94-106`).

Value:

This is the security context that must govern agent mutations and queue effects.

Gap:

The primitives are session-centered and have no first-class references to AgentRevision, LoopRevision, GraphRevision, submission snapshot, queue item, or execution attempt.

Remediation:

Reference the immutable authority context from Registry mutation, Loop/Graph publication, submission admission, queue claim, runtime launch, and terminal disposition. Do not duplicate or union authority inside new domain objects.

Verification:

Attempt each mutation under wrong subject, wrong stanza, stale mandate, substituted context digest, expired context, revoked context, and ambiguous participant binding. Every path must deny without partial publication.

### 4.3 Execution Dispatch/Turn primitives — RETAIN NARROWLY, DO NOT RELABEL AS QUEUE

Current evidence:

- Explicit lifecycle states and terminal transition rules exist (`internal/execution/model.go:14-35`).
- Dispatch binds a parent admission to an authority context (`internal/execution/model.go:37-46`).
- Turn binds runtime evidence to Dispatch and authority context (`internal/execution/model.go:48-59`).
- LaunchContract carries owner, mandate, authority context, and successful parent Dispatch (`internal/execution/model.go:70-78`).
- Fresh admission is checked within a one-second boundary (`internal/execution/model.go:94-106`).

Value:

These types are useful at the queue-worker-to-runtime edge.

Gap:

They are integrated at the Hermes attempt-adapter boundary, where `internal/runtime/hermes/attempt.go` consumes a LaunchContract, checks fresh admission, and produces a Turn. They are still not a durable product queue:

- no durable repository;
- no enqueue operation;
- no claim or lease;
- no priority/dependency readiness;
- no idempotency key;
- no retry history or reclaim;
- no cancellation propagation contract;
- no Graph or Loop reference;
- no application service or public route;
- no product application orchestration that turns queued work into persisted attempts.

Remediation:

Introduce a bounded queue domain. A queue attempt may produce or reference a narrow runtime Dispatch/Turn, but queue item, Graph run, Loop execution, runtime attempt, and evidence disposition must remain distinct records.

Verification:

Atomic double-claim test, lease-expiry reclaim, retry-budget exhaustion, revocation during execution, idempotent enqueue, crash recovery, terminal-state immutability, and duplicate-success denial.

### 4.4 Evidence and audit substrate — RETAIN AND INTEGRATE

Current evidence:

- RuntimeArtifact binds owner, action, run, authority context and content digest (`internal/evidence/model.go`).
- VerificationReceipt binds the exact artifact, claim, verifier, policy, expected/observed digest, and outcome.
- Blob verification rereads content-addressed bytes (`internal/evidence/verifier.go`).
- Authority transitions and audit records have create-only and tamper-evident mechanisms.

Value:

This is the right foundation for evidence-gated completion.

Gap:

No durable execution repository owns artifact metadata and verification receipts as part of a Graph/Loop/Queue history. No terminal disposition declares which claims were required and satisfied. A runtime result or queue projection could not yet be mechanically reconciled into end-to-end product truth.

Remediation:

Add narrow repositories and a disposition service. Required Loop/Graph claims must be known at admission and exact verification receipts must be present before success.

Verification:

Wrong artifact, wrong run, wrong authority context, wrong expected digest, reused receipt, missing required claim, and model-narrated completion must all fail terminal success.

### 4.5 TUI Queue — DO NOT REUSE AS PRODUCT QUEUE

The TUI queue is an in-memory bounded queue for presentation Events (`internal/tui/queue.go`). Its `TurnQueued`, `TurnStarted`, and related event kinds are presentation state. It has no product durability, authority, claim/lease, or execution semantics.

Remediation:

Keep it as a presentation transport. Feed it authoritative product events rather than elevating it into domain storage.

## 5. P0 product gaps

### P0-1. The normative MVI contract is missing and contradicted

Obligation:
The repository’s highest-authority documents must state the actual fleet-control target.

Evidence:

- `AGENTS.md` defines Aegis primarily as identity, trust, and session control.
- `specs/MVP.md` makes credential custody and typed GitHub use the release-defining edge.
- `specs/CANONICAL_DOMAINS.md:75` classifies workflow, graph, delivery, and disposition as future domains.
- `specs/PLUMBING.md:5-9` records removal of the participant-centric aggregate and GraphRun surfaces without defining bounded replacements.
- `specs/STORAGE.md` has no canonical ownership or persistence matrix for Agent, Loop, Graph, Queue, attempt, or run-snapshot records.

Gap:
The product target survived in design decisions and sessions but was erased from the normative repository hierarchy.

Risk:
Implementers correctly following repository authority build credential/session infrastructure while the agreed product remains absent. Green tests reinforce false scope completion.

Remediation:
First implementation task must re-baseline `AGENTS.md`, `specs/MVP.md`, `specs/README.md`, `specs/CANONICAL_DOMAINS.md`, `specs/PLUMBING.md`, and `specs/STORAGE.md`. Preserve the prohibition on universal aggregates while defining separate Registry, Loop, Graph, Queue, execution, evidence, and disposition responsibilities.

Verification:
Add executable scope/terminology checks and a contradiction check that fails if credentials are simultaneously deferred and mandatory or if any of the four product domains disappears from the MVI authority chain.

### P0-2. No canonical existing-fleet Agent Registry

Obligation:
Register one existing current-fleet agent as an executable participant.

Evidence:
`Charter` and immutable charter import exist, but no AgentRegistration/AgentRevision or fleet-source model exists.

Gap:
No stable fleet provenance, registry lifecycle, runtime binding, ownership, agent-authenticated mutation path, or explicit enable/disable semantics.

Risk:
A charter directory can be mistaken for a registered executable participant. Names or mutable runtime state may become accidental identity.

Remediation:
Add `internal/registry` with canonical types, repository interfaces, strict codecs, immutable revisions, lifecycle transitions, and application services. Define an adapter over the current fleet directory/store rather than copying dashboard routes or legacy handoff schemas into the public contract.

Verification:
Register a fleet fixture by immutable source identity, prove exact readback, duplicate/idempotency behavior, immutable revision publication, disabled-agent denial, and no secret or ambient profile import.

### P0-3. No Loop domain

Obligation:
The registered agent creates and versions reusable internal control flow.

Evidence:
No Loop/LoopRevision type, repository, service, command, API route, or test exists.

Gap:
Complete domain absence.

Risk:
Hermes task-flow templates or implementation plans may be mistaken for Aegis product Loops, coupling the public product to internal orchestration machinery and its mutable snapshots.

Remediation:
Define an Aegis-owned Loop schema independent of Hermes TaskFlow. Support exact immutable revisions, typed ports, bounded transitions, retry and terminal semantics, stored validation, and evidence requirements.

Verification:
Strict decode, unknown-field denial, invalid transition denial, unreachable terminal denial, unbounded retry denial, immutable revision conflict, digest substitution denial, and exact historical retrieval.

### P0-4. No Graph domain or exact participant/Loop binding

Obligation:
The registered agent creates versioned coordination that references exact Loop revisions and exact participants.

Evidence:
No Graph/GraphRevision/LoopRef/ParticipantBinding types exist. `LaunchContract` explicitly has no graph (`internal/execution/model.go:70-78`).

Gap:
Complete domain absence.

Risk:
Execution could resolve mutable “latest” definitions, switch participants, or reconstruct history from current state.

Remediation:
Add a bounded Graph domain with immutable revision/digest, exact AgentRevision and LoopRevision references, typed edges/mappings, stored validation, activation lifecycle, and immutable run snapshot.

Verification:
Wrong/missing Agent revision, disabled Agent, wrong/missing Loop revision, duplicate node ID, invalid mapping, unauthorized participant rebinding, and post-publication mutation must deny.

### P0-5. No durable authority-bound Execution Queue

Obligation:
Admit and operate work within the agent’s exact authenticated context.

Evidence:
Execution state primitives exist, but no queue domain or persistence/application path exists.

Gap:
No enqueue, claim, lease, heartbeat, reclaim, dependency readiness, attempt history, retry budget, cancellation, or evidence-gated terminal state.

Risk:
The product cannot operate work. A direct Hermes launch can be confused with queue execution, and process exit can be confused with workflow completion.

Remediation:
Implement append-only queue transition facts and rebuildable projections using the proven authority command/fact/replay pattern. Select a persistence path qualified for atomic claim/lease semantics. Keep queue status non-authoritative for security admission.

Verification:
Concurrent claim race, crash after claim, stale lease reclaim, duplicate worker, duplicate completion, cancellation, revocation, expiry, retry exhaustion, dependency block, and projection rebuild equivalence.

### P0-6. No integrated submission → queue → runtime → evidence loop

Obligation:
One typed Graph submission must become either a durable rejection or admitted queue item, execute, and close from evidence.

Evidence:
The app service directly owns charter/provision/session flows. `AttemptTurn` and evidence types are isolated components without Graph/Queue integration.

Gap:
No cross-domain application service or immutable run snapshot.

Risk:
Individually strong components can all pass while the product edge remains nonexistent.

Remediation:
Create one narrow application service:

`PrepareGraphRun → ValidateExactDefinitions → ResolveAuthority → PersistRejectedSubmission | AdmitQueueItem → ClaimAttempt → LaunchWithFreshAdmission → PersistArtifacts → VerifyClaims → TerminalDisposition`

Use immutable IDs/digests between domains; do not create a universal mutable aggregate.

Verification:
One installed deterministic end-to-end fixture with wrong-definition, wrong-context, revoked-context, invalid-input, failed-verification, crash/retry, and historical-drift cases.

## 6. P1 integration and contract gaps

### P1-1. Existing session launch is accidentally credential-coupled

`StartSessionAs` resolves a provider credential before launch in the current application path. A credential-independent fleet-control run should not require product credential setup merely to use the authority/runtime substrate.

Remediation:
Separate model-provider runtime configuration from downstream execution credentials and from optional Graph grant references. Credential-independent test/runtime adapters must operate without credential-domain onboarding.

### P1-2. No stored validation authority for Agent, Loop, or Graph revisions

The design distinguishes stored validation of a pinned revision from a current check that may later disagree. The repository has canonical charter validation but no generalized per-revision validation record for the target domains.

Remediation:
Store validator ID/version, validated object ID/revision/digest, result, issue codes, timestamp, and validation digest. Current revalidation must not rewrite historical validation.

### P1-3. No immutable run/submission snapshot

The execution detail target requires historical authority sealed against current Registry/Loop/Graph drift.

Remediation:
Persist one canonical submission/run snapshot containing exact immutable references and normalized typed inputs. Keep current-state drift as a separate projection.

### P1-4. No explicit Graph parent / Loop child / runtime attempt identities

The current `Session`, `Dispatch`, and `Turn` records do not model Graph runs and Loop executions.

Remediation:
Define separate IDs and causal references. Retries produce new attempts under the same logical child execution; they do not rewrite the execution identity.

### P1-5. Architecture ownership tests do not know the target domains

Current architecture tests enforce existing package ownership but have no owners for Registry, Loop, Graph, or Queue.

Remediation:
Update package-family classification and canonical type ownership before implementation to prevent dumping new product records into `core` or recreating plumbing.

### P1-6. Storage qualification is incomplete for queue semantics

The storage contract qualifies authority Badger and credential bbolt combinations, but not atomic queue claim/lease/recovery semantics or ordinary immutable definition storage.

Remediation:
Extend `specs/STORAGE.md` before selecting adapters. Separate canonical definitions/facts, queue operational metadata, rebuildable indexes, blobs, runtime state, and credentials.

### P1-7. Public surfaces and fault-state contracts are absent

The design requires Agent, Loop, Graph, Queue list/detail readback and distinct loading, empty, unavailable, and denied states.

Remediation:
Add typed service/API results first. UI should render authoritative decisions rather than recreating validation/admission logic.

### P1-8. Queue lifecycle does not distinguish active from queued product truth

Prior design feedback explicitly required Active Executions separately from queued and other lifecycle records.

Remediation:
Define server-side lifecycle projections and counts derived from canonical facts. Unknown/unavailable must not render as empty or zero.

### P1-9. Contextual readiness is named in the design but absent as a product contract

The retained design treats readiness as action-specific setup and repair. Current readiness and onboarding surfaces are centered on manager/session startup and credential/model/runtime prerequisites.

Remediation:
Define a bounded readiness service for each attempted Registry, Loop, Graph, submission, queue, and execution action. Return stable ready, denied, unavailable, degraded/repair-required, and empty semantics without making optional credentials a global gate.

Verification:
A missing optional credential must not block a credential-independent Graph submission. A disabled Agent must return denied, an unavailable definition store must return unavailable rather than empty, and a repairable missing runtime adapter must return a bounded repair action without mutating state before approval.

## 7. Specification and launch drift

### D1. Scope inversion in `specs/MVP.md`

Credentials and typed GitHub use are mandatory; Agent Registry, Loop, Graph, and Execution Queue are absent. This is the opposite of the established near-term target.

Disposition:
Rewrite the MVI objective and success demonstration. Preserve credential work as implemented/deferred supporting infrastructure.

### D2. Participant/GraphRun refactor removed product semantics without bounded replacements

The old plumbing mega-aggregate was correctly rejected, but the replacement kept only identity/authority, runtime Dispatch/Turn, and evidence. `specs/CANONICAL_DOMAINS.md:75` then deferred workflow and graph domains.

Disposition:
Do not restore `internal/plumbing`. Add separate bounded packages and immutable cross-domain references.

### D3. `specs/STORAGE.md` lacks target ownership

No storage class names Agent, Loop, Graph, queue item, lease, attempt, run snapshot, validation, or disposition.

Disposition:
Add record ownership and atomicity/recovery requirements before persistence code.

### D4. README and launch copy advertise the wrong product

Current launch material centers local trust-stanza sessions and treats fleet projection/control as absent or future.

Disposition:
Synchronize only as implementation phases land. Until then, state honestly that fleet-control is the target but not implemented.

### D5. OpenDesign target is not canonically retained in this checkout

The design provenance is recoverable from prior sessions and OpenDesign IDs, but the exact target contract is not a normative repository artifact.

Disposition:
Extract product obligations into focused specifications. Do not make a 755 KB static fixture the authority or copy its fixture logic into production.

### D6. Existing installed-MVI scripts prove packaging/denial, not product behavior

Passing archive/checksum/first-run denial checks are valuable but cannot establish Registry/Loop/Graph/Queue completion.

Disposition:
Retain packaging proof and add a product acceptance lane.

## 8. Required implementation sequence

### Phase 0 — Contract reset

Deliverables:

- corrected `AGENTS.md` MVI objective;
- corrected `specs/MVP.md` success demonstration;
- focused Registry, Loop, Graph, Queue, execution/evidence specifications;
- updated canonical-domain boundaries;
- updated storage ownership/qualification;
- explicit credential deferral;
- architecture ownership tests and terminology/contradiction checks.

Exit gate:
One reviewed normative hierarchy in which all four product domains exist, credentials are not the release-defining edge, and no universal aggregate is reintroduced.

### Phase 1 — Thin connected fleet-control vertical slice

Deliverables:

- minimum current-fleet Agent registration record referencing an immutable charter revision;
- one minimal immutable Loop revision with typed input/output and bounded control flow;
- one minimal immutable Graph revision binding the Agent and exact Loop revision;
- one typed submission and immutable run snapshot;
- minimal distinct GraphRun, child LoopRun, and runtime-attempt identities with persisted causal links;
- one durable rejected-submission path and one admitted queue-item path;
- one deterministic queue claim and runtime attempt under exact authority;
- one content-addressed artifact, verification receipt, and evidence-gated disposition;
- minimal CLI/API readback for the complete chain;
- action-specific contextual readiness for every step.

Exit gate:
The first installed vertical scenario proves Registry → Loop → Graph → submission → rejection/admission → queue → runtime attempt → evidence/disposition. No domain may be reported complete while disconnected from this path.

### Phase 2 — Definition and authoring hardening

Expand Registry, Loop, and Graph strict codecs, immutable revision rules, lifecycle transitions, stored validation, branch/join semantics, typed mappings, authorized create/revise/publish paths, disabled/retired behavior, concurrency/idempotency, and complete service/API tests.

Exit gate:
The authenticated registered Agent can publish multiple immutable Loop and Graph revisions; substitution, unauthorized rebinding, mutation, and current-check drift fail closed without rewriting historical validation.

### Phase 3 — Queue operational hardening

Add qualified atomic claim/lease behavior, heartbeat where needed, crash-safe reclaim, dependency readiness, bounded retries, cancellation, revocation/expiry propagation, transition-fact replay, projection rebuild, backpressure/concurrency policy, and stable reason codes.

Exit gate:
Queue operation survives concurrent claim, crash/restart, stale lease, cancellation, retry exhaustion, and projection rebuild tests without duplicate success.

### Phase 4 — Execution and evidence hardening

Complete parent Graph, child Loop, and runtime-attempt identities; exact launch bindings; fresh effect admission; artifact and verification repositories; branch-aware transitions and outputs; required-claim disposition; replay/substitution denial; and historical reconstruction after current-definition drift.

Exit gate:
No runtime/model/output/projection can grant authority or assert success. Exact verified claims close the run, and historical truth survives current Registry/Loop/Graph/validator drift.

### Phase 5 — Full design surfaces and installed proof

Deliverables:

- Agent Registry list/detail;
- Loop list/detail;
- Graph list/detail;
- Execution Queue active/queued/lifecycle/detail;
- loading/empty/unavailable/denied contracts;
- action-specific ready/degraded/repair-required contracts;
- deterministic installed acceptance suite;
- synchronized README, architecture, threat model, quickstart, demo, changelog, recording, release artifacts, and contributor backlog.

Exit gate:
A clean installation runs the complete acceptance suite and every launch claim is bound to the tested revision.

## 9. Acceptance demonstration

Aegis is MVI-complete only when one exact-head installed acceptance suite runs multiple deterministic scenarios: one canonical successful vertical run plus separate rejection, execution failure, cancellation/expiry, definition/validation drift, contextual-readiness, and service fault-state cases. The suite must demonstrate:

1. Existing fleet Agent `A` is registered with stable ID and immutable revision `A1`.
2. Authenticated Agent `A` publishes Loop revision `L1`.
3. Agent `A` publishes Graph revision `G1` binding `A1` and referencing `L1`.
4. Typed submission `S1` is normalized and validated.
5. Exact subject/stanza/mandate/authority context is resolved.
6. A distinct malformed or unauthorized submission becomes a durable rejection and never enters the queue.
7. Valid `S1` becomes one idempotent queue item `Q1`.
8. Exactly one worker claims `Q1`; duplicate claim fails.
9. Graph execution `GR1`, child Loop execution `LR1`, and runtime attempt `AT1` are distinct and causally linked.
10. Fresh exact-context admission is required immediately before consequential runtime work.
11. A separate revocation or expiry scenario prevents the next effect and terminalizes honestly.
12. Output artifact is content-addressed and independently verified against required claims.
13. Queue success follows verification evidence rather than process exit or model narration.
14. Changing current Agent, Loop, and Graph definitions afterward does not alter historical `GR1` reconstruction.
15. Separate scenarios produce denied, failed, cancelled/expired, and completed records in the correct lifecycle surfaces.
16. No reusable credential value appears anywhere in the proof; no credential setup is required for the credential-independent defining Graph.
17. Stored validation remains bound to its exact Agent/Loop/Graph revision; a later disagreeing current check is shown separately and cannot rewrite historical validation or topology truth.
18. At least one bounded branch/join or equivalent nontrivial transition proves typed input/output mappings, branch decisions, and output/evidence provenance.
19. Contextual readiness evaluates only the attempted action: ready admits, denied discloses no protected collection, unavailable is not rendered as empty, and degraded/repair-required returns a bounded action without unauthorized mutation.
20. Loading, populated, genuinely empty, unavailable, and denied service/surface states are mechanically distinct.
21. Active execution readback is mechanically separate from queued work and terminal lifecycle history.
22. Any topology preview retained in the MVI is derived from the same pinned definition/validation semantics; richer decorative topology presentation may remain post-MVI.

## 10. Priority gap register

| Gap | Severity | Blocks MVI | Existing reusable substrate | Required proof |
|---|---:|---:|---|---|
| Normative contract contradicts target | P0 | Yes | Prior decisions and bounded-domain doctrine | Reviewed scope hierarchy and contradiction tests |
| No existing-fleet Agent Registry | P0 | Yes | Charter ID/revision/digest | Register/read/revise/disable fixture |
| No Loop domain | P0 | Yes | Strict codec/digest patterns | Immutable Loop revision proof |
| No Graph domain | P0 | Yes | Immutable ID/digest patterns | Exact Agent/Loop binding proof |
| No durable Execution Queue | P0 | Yes | Authority command/fact/replay; execution states | Atomic claim/reclaim/retry/cancel proof |
| No integrated vertical slice | P0 | Yes | Authority, Hermes adapter, evidence primitives | Installed end-to-end proof |
| No immutable run snapshot | P1 | Yes | Canonical digest/store patterns | Historical reconstruction after drift |
| No evidence-gated disposition | P1 | Yes | RuntimeArtifact/VerificationReceipt | Missing/wrong/replayed claim denial |
| Session launch credential coupling | P1 | Yes for credential-free path | Existing runtime adapter | Credential-independent execution proof |
| No target-domain storage qualification | P1 | Yes | Badger/bbolt qualification method | Queue atomicity/recovery qualification |
| No live product routes | P1 | Yes | API/Cobra patterns | Agent/Loop/Graph/Queue list/detail proof |
| Launch assets advertise old scope | P1 | Yes for launch | Launch synchronization rule | Exact-head launch evidence packet |
| Rich credentials integration | Deferred | No | Encrypted custody/broker | Post-MVI plan |

## 11. Final assessment

The correct answer is not that Aegis lacks useful implementation. It has a substantial security and runtime foundation.

The correct answer is that implementation effort converged on the wrong release center. The current repository can authenticate subjects, select trust stanzas, issue mandates, launch sessions, persist authority, broker a narrow credential operation, and verify artifacts. It cannot yet do the primary product job that had already been selected:

> Register a current-fleet agent, let that agent create versioned Loops and Graphs, and let it operate durable queue work within its authenticated context.

That is the MVI gap. The remediation must start by repairing the repository’s own product authority, then build the four bounded domains and one narrow installed vertical loop. Credentials remain available as later supporting capability rather than the MVI’s organizing principle.
