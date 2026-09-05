# Aegis Minimum Viable Feature Set

## MVP objective

The first Aegis release must prove one coherent fleet-control vertical slice:

> Register one existing fleet agent, let that authenticated participant publish immutable Loop and Graph revisions, and operate one typed Graph submission through a durable, authority-bound Execution Queue to evidence-gated disposition.

The design-to-launch workflow, charters, trust stanzas, mandates, clean Hermes runtime boundary, audit, and evidence remain required enabling controls. Encrypted credential custody and the existing typed `github.get_repository.v1` broker are reusable supporting infrastructure, not release-defining gates. Credential-independent Registry, Loop, Graph, and Queue actions MUST NOT require credential setup.

## 0. Fleet-control product contract

The MVI has four separate product domains:

- **Agent Registry** owns stable executable-participant identity, existing-fleet provenance, immutable Agent revisions, exact charter/runtime references, accountability, and enabled/disabled/retired lifecycle.
- **Loops** own reusable bounded control flow, immutable validated revisions, typed ports, retry limits, terminal reasons, and evidence requirements. A Loop is not a Hermes TaskFlow or a running session.
- **Graphs** own immutable validated coordination revisions, exact participant and Loop-revision bindings, typed dependency mappings, and admission constraints. A Graph never absorbs authority or queue state.
- **Execution Queue** owns durable submission admission, idempotency, claim/lease, attempt, retry, cancellation/expiry, and evidence-gated terminal lifecycle.

Every runtime-ready submission captures one immutable snapshot of the exact Agent, Graph, Loop, mandate, authority-context, runtime, and normalized-input IDs/digests. A registered-Agent workspace submission first captures exact workspace/owner provenance and remains `awaiting-runtime` until a fresh runtime binding supplies the mandate, authority context, and runtime. Definitions are never resolved from mutable `latest` state after admission. Missing, unknown, disabled, expired, revoked, substituted, ambiguous, or partially persisted control input denies. Permissions from different stanzas are never unioned.

Application services compose these domains by immutable references. No universal mutable participant/workflow/run aggregate may duplicate their authority or mutation semantics.

### Registered-Agent workspace contract

A freshly authenticated principal MAY delegate one sealed `aegis.workspace-authority.v1` workspace to one exact latest enabled registered Agent revision. Its exact capabilities are `fleet.loops.define`, `fleet.loops.manage-own`, `fleet.graphs.define`, `fleet.graphs.submit-participant`, `fleet.queue.manage-own`, and `fleet.definitions.read-shared`. Principal, Agent revision/digest, stable owner, capability ordering, and workspace digest are immutable inputs; mismatch, stale revision, disabled Agent, ownership drift, or expired authentication denies.

The workspace MAY publish and lifecycle-manage only definitions owned by that stable Agent owner, submit only a Graph containing that exact Agent revision as a participant, manage only Queue work carrying matching owner provenance, and read/reference/use definitions across the fleet. Definition reads are shared; later publication and lifecycle mutation remain owner-only. Definition authoring and submission do not require a provisioning receipt, mandate, authority context, or running session.

Workspace authority MUST NOT authorize provisioning, Queue claim, processing, runtime effects, sessions, mandates, or credentials. Before processing, the Aegis controller MUST attach a fresh runtime-authority binding and repeat normal runtime admission. Ordinary workspaces have no credential capabilities or bindings; only controller authority may administer or apply credentials. This feature supplies no native agent transport, autonomous scheduler, or automatic execution.

## 0a. Contextual readiness and public routes

Fresh or resumed authenticated bootstrap additionally registers one canonical immutable built-in Agent `aegis` after a separate explicit default-decline approval. Revision 1 fixes source kind `aegis-system`, runtime target `manager-disposable`, Aegis product ownership, and authenticated-principal accountability. Full-record readback is mandatory, replay is idempotent, collisions deny, and generic lifecycle mutation is unavailable. The record creates no persistent Hermes profile and grants no credential, stanza, mandate, runtime, queue, or model authority. Existing pre-feature gateways are not silently backfilled while they own the stores.

Once that built-in record is exactly verified, fresh and resumed offline bootstrap checks for an exact prior default-profile import, then safely inspects only the configured principal's canonical `~/.hermes/config.yaml` when no import exists. An eligible profile is offered through separate default-decline review and registration decisions; review is non-mutating, confirmation regenerates and digest-checks the proposal, and success creates one immutable disabled `hermes-default-*` Agent with no declared capabilities or policies. Missing profile evidence is non-gating; unsafe evidence and collisions deny. No profile content or ambient authority is inherited, and exact existing imports are verified without rereading mutable profile evidence.

Readiness is action-specific for registration, revision publication, submission, queue claim, and runtime execution. A typed result distinguishes `ready`, `denied`, `unavailable`, and `degraded`; list readback may report `empty` only after an authoritative successful read. Repair guidance is bounded and never mutates state without the required approval. Missing optional credentials do not make credential-independent actions unready.

The shared service surface is exposed through CLI and HTTP resources for `/v1/agents`, `/v1/loops`, `/v1/graphs`, and `/v1/queue`. Transports do not make identity, validation, admission, readiness, or disposition decisions.

## 1. Principal authentication

- Support one explicitly configured principal.
- Authenticate outside the model before a principal design or operational session starts.
- Initially support a local authentication mechanism tied to the principal's OS account, with a pluggable interface for stronger authentication.
- Never accept a CLI flag, display name, prompt statement, or model conclusion as authentication.
- Fail closed when principal authentication is absent, expired, or ambiguous.

## 2. Explicit runtime selection

- Support one runtime adapter: Hermes Agent.
- Discover and display the Hermes executable, version, installation, and adapter version.
- Select the runtime by explicit flag, charter setting, or visibly displayed configured default.
- Never hide Hermes behind a generic-agent label.
- Refuse unsupported runtime versions rather than silently degrading security behavior.

## 3. Dedicated design session

- Provide a principal-only agent-design command and session mode.
- Run the design assistant on the explicit Hermes runtime.
- Do not require creation of a named Hermes profile for design.
- Isolate design from the user's ordinary Hermes memory, sessions, plugins, MCP servers, and project instructions.
- Give the design runtime read-only design and capability-discovery tools only.
- Do not give the design runtime provisioning, arbitrary file, shell, plugin, MCP, credential, or profile-management authority.
- Clearly display `Design mode: no provisioning capability`.

## 4. Canonical agent charter

- Produce one versioned, machine-readable charter for each logical agent.
- Include:
  - Stable logical-agent ID and name
  - Explicit runtime and runtime constraints
  - One or more trust stanzas
  - Authentication rule for each stanza
  - Tools and capabilities for each stanza
  - Memory scope for each stanza
  - Credential scope for each stanza
  - Session lifetime for each stanza
  - Approval requirements
  - Runtime-specific Hermes mapping
- Use deterministic serialization.
- Compute and display a charter digest.
- Reject unknown fields, invalid combinations, unsafe implicit defaults, and ambiguous stanza rules.
- Keep the charter—not the conversation transcript—as the source of truth.

## 5. One-to-many trust stanzas

- Allow each logical agent to define 1–N trust stanzas.
- Require a stable ID and explicit authentication policy for every stanza.
- Support at least:
  - `principal`
  - `teamwide`
  - User-defined stanza names
- Keep tools, capabilities, memory, credentials, and session policy independently configurable per stanza.
- Treat stanza names as metadata, not authentication evidence.
- Do not implement transitive trust or stanza inheritance in the MVP.

## 6. Deterministic stanza selection

- Bind every operational session to exactly one stanza.
- Select the stanza from authenticated identity plus an explicit request or deterministic policy.
- Zero authorized matches means deny.
- More than one valid match means deny as ambiguous.
- Never union permissions from multiple stanzas.
- Never allow a model message to change the active stanza.
- Require a new mandate and clean session to change stanzas or materially change authority.

## 7. Authenticated session mandate

- Issue a short-lived mandate after identity and stanza authorization succeed.
- Bind the mandate to:
  - Authenticated subject
  - Logical-agent ID
  - Stanza ID
  - Charter version and digest
  - Hermes runtime identity and configuration
  - Effective capabilities
  - Memory and credential scopes
  - Issue and expiry times
- Prevent the runtime or model from modifying or extending the mandate.
- Support explicit session termination and revocation.
- Do not support mandate delegation in the MVP.

## 8. Clean per-stanza runtime launch

- Start a new Hermes process or isolated Hermes execution context for each session.
- Give it only the selected stanza's effective configuration.
- Do not carry principal transcript, memory, secrets, or tool handles into a teamwide session.
- Use separate runtime state directories where needed to prevent session-history and memory collision.
- Keep persistent runtime profiles optional; use a disposable Hermes home for design sessions.
- Make the distinction between Hermes state isolation and host sandboxing explicit.

## 9. Capability restriction

- Resolve a concrete tool list before starting Hermes.
- Expose only tools declared by the selected stanza and supported by the adapter.
- Never rely on a system prompt as the only tool restriction.
- Deny broad wildcard tool selection in the MVP.
- Disable ambient MCP servers and plugins unless they are explicitly represented in the approved charter.
- Do not give the runtime direct access to another stanza's credentials or memory.

## 10. Exact charter approval

- Present a human-readable charter summary and full diff before provisioning or activation.
- Bind approval to the exact canonical charter digest.
- Record principal identity, timestamp, target runtime, target environment, and digest.
- Any charter mutation invalidates approval.
- Approval is not a blanket authorization for later revisions.
- The design agent cannot approve its own charter.

## 11. Deterministic Hermes provisioning

- Keep provisioning separate from the design runtime.
- Generate a complete preview of Hermes artifacts and actions before applying them.
- Provision only an approved charter revision.
- Create runtime-specific artifacts through deterministic application code, not model-generated shell commands.
- Verify the effective Hermes configuration after provisioning.
- Return a provisioning receipt listing created or changed artifacts.
- Do not automatically start gateways, install services, create cron jobs, or contact external systems unless those effects are explicitly included and separately approved.

## 12. Session startup and CLI visibility

- Provide an operational session command that accepts a logical agent and optional stanza.
- Display before launch:
  - Authenticated identity
  - Logical agent
  - Selected stanza
  - Charter version
  - Runtime and version
  - Runtime-specific target
  - Session expiry
- Require fresh authentication for privilege escalation into `principal`.
- Allow downshifting only by creating a clean new session.
- Default to deny when a safe stanza cannot be determined.

## 13. Basic audit trail

- Record authoritative events from Aegis rather than relying on model narration.
- Record at least:
  - Authentication success or failure
  - Design session creation
  - Charter creation and validation
  - Approval or rejection
  - Provisioning result
  - Session issuance, start, expiry, and revocation
  - Identity, stanza, runtime, and charter digest
- Keep secrets and full private prompts out of audit records by default.
- Make audit records append-only to the runtime process.
- Include stable event IDs and machine-readable reason codes.

## 14. Inspection and validation commands

- List logical agents and charter revisions.
- Inspect a logical agent's runtime and trust stanzas.
- Validate a charter without provisioning.
- Show effective stanza capabilities and runtime mapping.
- Explain why an identity is or is not authorized for a stanza.
- Show active sessions and their expiry.
- Revoke an active session.
- Show provisioning and audit receipts.

## 15. Go application foundation

- Implement the CLI in Go with Cobra.
- Construct fresh command trees through constructors; avoid package-level commands and mutable globals.
- Use Viper only to gather configuration sources.
- Decode once into strict, typed, validated configuration.
- Use an explicit configuration precedence contract.
- Use `context` cancellation throughout session and runtime lifecycle.
- Use structured `log/slog` logging with secrets redacted by construction.
- Keep stdout for command results and stderr for diagnostics.
- Centralize error rendering and exit codes.
- Make the optional Echo API call the same application services as the CLI.

## 15a. Supporting personal credential custody and typed use

- Store reusable values only through protected intake outside model context and persist only encrypted credential versions.
- Expose credential metadata, bindings, rotation, revocation, backup, and recovery status without returning plaintext.
- Require an exact active binding for agent, stanza, deployment, scope, destination, record, and rotation policy before use.
- Retain the bounded `github.get_repository.v1` implementation with `github/read` and exact allowlisted `github-api` repositories as supporting infrastructure; the fleet-control acceptance path does not require it.
- Give Hermes only the typed owner/repository operation. Do not expose GetSecret, arbitrary URLs, headers, methods, record IDs, versions, destinations, or generic proxying.
- Keep the session capability outside model context, environment, charter, mandate, session JSON, logs, and audit.
- Verify the live Hermes gateway registers exactly the approved Aegis-owned bridge tool and fail closed on missing or additional tools.
- Apply the credential inside Aegis and return only bounded sanitized repository metadata.
- Reauthorize current session, mandate, charter, process identity, binding, version, deadline, request ID, and revocation state on every use.

## 16. MVP security invariants

The release is not complete unless tests demonstrate:

- A prompt cannot authenticate the principal.
- A CLI stanza flag cannot bypass authorization.
- An unauthorized identity cannot enter `principal`.
- A session can bind to only one stanza.
- Multiple matching stanzas fail closed.
- Stanza capabilities are never unioned.
- Changing stanza creates a clean runtime session.
- Teamwide sessions cannot load principal memory or credentials.
- The design session cannot provision or modify Hermes artifacts.
- Provisioning rejects an unapproved or changed charter.
- The actual launched runtime and effective tool list match the approved charter.
- If the optional broker is enabled, its model-visible surface contains exactly the approved typed operation and no secret-reading or generic network operation.
- For credential-bearing operations, replays, stale deadlines, wrong repositories, wrong stanzas, ambiguous bindings, rotation drift, expired sessions, and revoked records/sessions deny.
- Random credential canaries do not appear in model-visible tool definitions/results, configuration, argv, environment, logs, audit, errors, or retained terminal state.
- Revocation prevents a revoked session from continuing through Aegis.
- Audit events identify the authenticated subject, stanza, runtime, and charter revision.
- Graph admission rejects any missing, mutable, substituted, or disabled Agent, Loop, Graph, mandate, or authority-context reference.
- Concurrent workers cannot both claim or successfully complete one queue attempt.
- Runtime launch, retry, and terminal disposition repeat fresh exact-context admission and propagate cancellation, expiry, and revocation.
- Process exit, queue projection state, verification evidence, or model narration cannot independently grant authority or declare completion.

## 16a. Qualified storage boundary

- Apply the exact, fail-closed matrix in `specs/STORAGE.md`: Badger `v4.9.5` owns session-authority persistence and bbolt `v1.5.0` owns credential custody on the qualified Linux/amd64/ext4 host profile.
- Keep canonical authority facts, credential custody, ordinary canonical documents, rebuildable projections, blobs, operational metadata, and runtime state distinct.
- Persist immutable Agent, Loop, and Graph revisions separately from append-only submission, queue, run, attempt, and disposition facts. The queue claim/lease path requires its own explicitly qualified atomic writer protocol before release.
- Never treat a projection, lifecycle marker, runtime home, model narration, or cross-store partial write as authority.
- Require explicit requalification for a new engine version, platform, filesystem, writer model, or relaxed durability option. Cross-built release archives alone are not storage qualification.

## Explicitly deferred

The following are not required for the first release:

- Additional agent runtimes beyond Hermes
- OPA as a mandatory dependency
- SPIFFE/SPIRE deployment
- Cross-organization federation
- Multi-principal or multi-party approval
- General capability delegation
- Agent-to-agent transitive trust
- Formal information-flow labels or taint tracking
- Automatic cross-stanza disclosure
- Shared cross-stanza memory
- Dynamic policy learned by a model
- Arbitrary third-party MCP or plugin installation during design
- Runtime migration between adapters
- Web dashboard
- Multi-tenant SaaS operation
- Hardware-backed runtime attestation
- Public transparency logs
- Generic credential retrieval or arbitrary authenticated HTTP proxying
- Additional downstream providers beyond the one typed GitHub metadata action
- Automated Google, email, Drive, banking, or cloud-administration credential use
- Credential integration into new Graph or Loop definitions and credential-centric fleet-control acceptance

## MVP success demonstration

A successful MVP demonstration should show:

1. The configured principal authenticates, Aegis visibly selects Hermes, and an approved charter establishes one exact stanza and mandate.
2. Aegis registers one existing fleet fixture and reads back its stable Agent ID, exact revision/digest, fleet provenance, runtime binding, and lifecycle.
3. The authenticated agent publishes and reads back one valid immutable Loop revision.
4. The agent publishes and reads back one valid immutable Graph revision pinned to that Loop and Agent revision.
5. One typed Graph submission persists exact normalized inputs and authority/definition digests, then becomes one admitted queue item.
6. A worker claims one attempt, fresh admission succeeds, and explicit Graph-run and Loop-execution records reach evidence-gated completion.
7. A duplicate claim, substituted revision, disabled Agent, invalid input, wrong stanza, and revoked or expired context each fail closed with stable durable reasons.
8. Cancellation and bounded retry preserve separate attempt history and never produce duplicate successful completion.
9. Changing current Agent, Loop, and Graph definitions does not change historical run reconstruction.
10. Agent, Loop, Graph, and Queue routes distinguish denied, unavailable, degraded, and genuinely empty states.
11. The complete proof runs without downstream credential setup; optional credential tests continue to prove plaintext non-disclosure independently.

That vertical slice proves the defining Aegis concept without pretending the first release is a complete agent-security platform.
