# Canonical Domain Boundaries

## Purpose

Aegis represents executable participants, reusable control flow, coordination, queue lifecycle, identity, authority, execution, evidence, disposition, provisioning, and sessions through bounded canonical records. It does not admit runtime work through a mutable aggregate that duplicates those records or unions their mutation semantics.

The controlling invariant remains:

> Agent identity and authority must be established outside the model, and every runtime session receives exactly one authenticated, reviewable trust context.

## Identity and authority (`internal/core`)

The canonical identity and authorization types are:

- `Subject`: authenticated identity provenance established outside the model.
- `Decision`: deterministic exactly-one-stanza selection result.
- `EffectiveAuthority`: the selected capabilities, tools, memory, credentials, and Hermes mapping.
- `Mandate`: short-lived authorization grant bound to subject, stanza, charter revision, runtime, and effective grants.
- `AuthorityContext`: immutable instantiation of one mandate for exactly one session.
- `AuthorityRevocation`: append-only fact targeting a mandate or one authority context.
- `Session`: one runtime execution under one mandate and authority context.

A mandate is not a session authority snapshot. `AuthorityContext` must bind the exact mandate ID, session ID, subject, logical agent, charter revision and digest, runtime descriptor, effective authority, issuance interval, and its own canonical digest. Validation denies any widened or changed authority.

Revocation never rewrites a mandate or authority context. Authority is effective only inside the half-open interval `[issued_at, expires_at)` and only when no applicable revocation fact has become effective.

`WorkspaceAuthority` (`aegis.workspace-authority.v1`) is a separate orchestration record: controller-issued, digest-sealed delegation from a fresh authenticated principal to one exact latest enabled registered Agent and stable owner. It carries only the fixed definition/submission/own-Queue/shared-read capabilities. It is not a mandate, authority context, session, provisioning receipt, runtime admission, claim, or credential grant.

## Agent Registry (`internal/registry`)

Registry owns stable executable-participant identity. `AgentRegistration` records existing-fleet provenance, runtime-adapter binding, accountability, and enabled/disabled/retired lifecycle. Immutable `AgentRevision` records reference one exact canonical charter revision and digest; display metadata and operational health are not identity or admission authority.

Registration never grants a model authority to create, enable, rebind, or retire a participant. The authenticated operator establishes the initial fleet binding. Later agent-authenticated mutations require one exact authority context and explicit policy. Disabled or retired participants deny new publication, submission, claim, and launch while historical records remain readable.

## Loops (`internal/loop`)

Loop owns reusable internal control-flow definitions. A stable Loop ID has immutable, content-digested `LoopRevision` records with typed inputs/outputs, bounded steps and transitions, entry and terminal states, bounded retries, evidence requirements, and an exact validator/version result. A Loop is not a Graph, Hermes TaskFlow, queue item, session, or runtime attempt.

Publication is create-only. Admission references one exact Loop ID, revision, digest, and validation digest; it never falls back to a mutable current revision.

Workspace-authored publication records owner Agent, stable owner ID, principal, and workspace-authority provenance. All workspaces may read/reference/use a Loop; only the stable owner may publish a later revision or append lifecycle mutation.

## Graphs (`internal/graph`)

Graph owns versioned coordination. An immutable `GraphRevision` binds each participant node to one exact Agent revision/digest and each control-flow node or edge to one exact Loop revision/digest. Typed dependencies and input/output mappings are validated against the pinned revisions. Graph structure, participant binding, and admission constraints cannot be changed by prompt or runtime output.

A Graph does not own authority, queue status, artifacts, or disposition. A `GraphRunSnapshot` is a create-only composition record containing normalized typed inputs and the exact immutable references resolved at submission; it preserves historical truth but cannot authorize an effect by itself.

Workspace-authored Graph revisions seal owner Agent, publishing principal, and workspace-authority provenance. Definitions are fleet-shared for reads and exact references, while mutation is stable-owner-only. A workspace may submit only when its exact Agent revision is one of the Graph's pinned participants.

## Execution Queue (`internal/queue`)

Queue owns the authoritative operational lifecycle of submitted work. Canonical records include submission or durable rejection, idempotency/admission key, queue item, append-only transition facts, claim/lease, attempt identity, retry budget, cancellation/expiry, and terminal reason. Queue items reference an exact GraphRunSnapshot and authority context by ID and digest.

Only one qualified atomic writer protocol may win a claim or terminal transition. Retries create new attempts under the same logical Graph and Loop executions. Queue projections and counts are rebuildable read models; they cannot admit work. Missing, stale, duplicate, revoked, expired, or ambiguous control state denies rather than guessing or merging.

A workspace submission records `authority_kind=registered-agent-workspace` and owner provenance and begins `awaiting-runtime`. A `RuntimeBinding` is the immutable owner-authorized handoff to one fresh mandate/runtime authority. Only after that binding and normal fresh admission may Aegis transition work to claimable `queued` state. Workspace authority alone never authorizes processing.

## Execution (`internal/execution`)

Execution owns Graph-run and Loop-execution causality plus dispatch and runtime-turn lifecycle state:

- `GraphRun`: parent execution identity bound to one immutable GraphRunSnapshot and queue item.
- `LoopExecution`: child execution identity bound to one exact Graph node and Loop revision.
- `Attempt`: one bounded try under a Loop execution and queue claim; retry creates a new Attempt.
- `Dispatch`: controller-owned parent admission for a runtime session.
- `Turn`: one runtime turn under a dispatch and authority context.
- `LaunchContract`: immutable owner, mandate, authority context, and successful parent dispatch supplied to an adapter.
- `AdmissionDecision`: fresh authoritative answer bound to the exact authority-context ID and digest.

Legal transitions are explicit and terminal states cannot transition again. A runtime adapter must deny unless:

1. the authority context exactly validates against its mandate;
2. the parent dispatch succeeded under that same context and before its expiry;
3. an authoritative admission checker allows the exact context ID/digest;
4. the decision is no more than one second old at the effect boundary; and
5. the authority remains unexpired and unrevoked at the final check.

Historical projections, prompt content, model output, runtime narration, artifacts, and verification receipts cannot satisfy admission.

## Evidence (`internal/evidence`)

Evidence owns runtime output and verifier claims:

- `RuntimeArtifact`: content-addressed runtime output bound to owner, action, run, and authority context.
- `VerificationReceipt`: one verifier's claim bound to the exact artifact, action, run, owner, authority context and digest, verifier, policy version, expected/observed digest, outcome, and evidence reference.

Verification rereads the content-addressed blob rather than trusting a filename or caller-supplied bytes. A receipt is evidence only: it cannot grant authority, mutate execution state, or declare a larger workflow complete. The MVP verifier remains an in-process component using the local store; this is not independent attestation or separately protected evidence custody.

## Disposition

Disposition is the terminal decision over one exact Graph run or Loop execution. It references queue transitions, attempts, required artifact digests, and verification receipts without copying them. A successful process exit is not completion. Success requires every verification claim required by the pinned revision; failure, denial, cancellation, expiry, revocation, retry exhaustion, and success remain distinct stable reasoned outcomes.

## Provisioning and receipts (`internal/core` and application services)

`core.Artifact` remains the canonical deterministic provisioning artifact. `core.Receipt` remains the canonical provisioning receipt. Runtime artifacts do not replace or duplicate provisioning artifacts.

Provisioning continues to use exact approved charter and plan digests, typed deterministic effects, create-only publication, containment checks, and interrupted-intent recovery. The model does not provision.

## Composition boundary

Application services may compose bounded domain records for one operation, but no production service may recreate a universal cross-domain mutation aggregate or validator. Transport and runtime adapters call shared services and receive only their narrow contracts.

Persistence follows the same boundary. Registry, Loop, Graph, Queue, execution, evidence, core authority, and credential packages each own their schemas. Persistence adapters encode those records without redefining them. Canonical definitions and facts, rebuildable projections, blobs, operational metadata, runtime state, and credential custody remain separate as specified in `STORAGE.md`; no engine or projection is a universal domain owner.

The removed experimental surfaces are not compatibility APIs:

- `internal/plumbing` and its aggregate/universal validator;
- `internal/poc` orchestration;
- `aegis plumbing ...`;
- `POST /v1/plumbing/poc`;
- `GET /v1/graph-runs/:id`.

Future delivery or additional workflow domains must define their own authoritative state and transition rules. The MVI Registry, Loop, Graph, Queue, execution, evidence, and disposition domains reference one another by immutable ID/digest; they must not copy one another into a mega-aggregate or infer completion from model narration.
