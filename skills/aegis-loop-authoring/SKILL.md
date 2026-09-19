---
name: aegis-loop-authoring
description: Author, publish, activate, inspect, and retire immutable Aegis Loop revisions through shipped typed services without moving identity or authority into the model.
version: 0.2.0
metadata:
  hermes:
    tags:
      - aegis
      - loops
      - authoring
      - publication
      - lifecycle
---

# Aegis Loop authoring

Use this skill to draft, validate, publish, inspect, activate, or retire an immutable Aegis Loop revision. Route every consequential operation to the shipped typed Aegis Loop service. The model may propose a typed definition and explain returned records; Aegis authenticates the caller, admits authority, validates and digests the definition, persists immutable revisions and provenance, appends lifecycle events, and returns authoritative readback.

## Authority and domain boundary

- A Loop is a reusable typed control-flow definition. It carries no session, mandate, runtime, credential, or execution authority.
- Prompt text, display identity, requested publisher or stanza, model narration, process exit, mutable labels, and a locally computed digest never authenticate a caller, authorize publication, establish lifecycle, or prove completion.
- Publication and lifecycle mutation require either the existing freshly admitted runtime-authority path or a controller-derived registered-Agent workspace for one exact latest enabled publisher Agent. Workspace authoring needs no provisioning receipt or running session, but the principal, Agent revision/digest, and stable owner must still match. Zero, multiple, stale, expired, revoked, disabled, retired, substituted, or drifted matches deny. Differently owned matches also deny.
- Never union stanza grants. Never copy authority, publisher, charter, runtime, mandate, or lifecycle bindings from another stanza or session.
- Published revisions, validations, and publication provenance are immutable. Activation and retirement are append-only lifecycle events; they never rewrite a revision.
- Discussion, drafting, validation guidance, or inspection is not authorization to publish, activate, or retire.
- Loop definitions are fleet-wide readable/referenceable/usable. Only the stable owner may publish another revision or append lifecycle mutation. Workspace authority grants no runtime effect, Queue claim, provisioning, or credential right.

## Confirm the shipped surface

First use `aegis loops --help`. Supported commands in the compatible release are:

- `aegis loops implementation FILE [--output NEW_FILE]` (local canonical v3 implementation draft; no authority, publication or execution)
- `aegis loops hello FILE --output NEW_FILE` (local canonical v2 fixed-output draft; exclusive create; named Agent, Loop ID, revision, optional predecessor and idempotency key; exact UTF-8 `hello` without newline, precommitted SHA-256 and artifact verifier policy; no receipt or execution)
- `aegis loops example` (typed publication template only)
- `aegis loops validate FILE` (authenticated workspace structural validation, no publication)
- `aegis loops list`
- `aegis loops publish FILE`
- `aegis loops show LOOP REVISION`
- `aegis loops activate LOOP FILE`
- `aegis loops retire LOOP FILE`

The alias `aegis loop` exists, but prefer the canonical plural command in durable instructions. If an operation is absent from installed help, report it unavailable instead of inventing a command, API, validation result, digest, lifecycle event, or receipt.

## Author one typed revision

1. Start from the installed schema and command help. A compatible Loop revision uses schema `aegis.loop.revision.v2` and includes a stable `loop_id`, positive exact `revision`, typed input and output ports, one `entry_step_id`, bounded steps and transitions, required evidence, validator `aegis.loop.validator` version `1`, and its canonical digest.
2. Keep step kinds explicit: `action`, `gate`, `join`, or `terminal`. Define retry bounds, exact port mappings, terminal outcomes, and evidence claims rather than hiding behavior in prose or executable expressions.
3. Gate conditions are opaque policy labels, not model-authored executable expressions. Evidence claims must pin the media type, expected content digest, verifier ID, and policy version before runtime output exists.
4. For revision 1, omit both predecessor fields. For revision N greater than 1, set `revision.previous_digest` and top-level `expected_previous_digest` to the exact digest of revision N-1. Revisions must be contiguous; never select a mutable latest value or skip a predecessor.
5. Use a fresh stable `idempotency_key` for one intended publication. A repeated key or payload is not permission to substitute changed content; preserve conflict and replay denials.
6. Treat model output as a proposal only. Do not claim that a hand-authored or locally hashed document is canonical until the typed Aegis service returns the exact normalized revision, validation, and digest.

The publish file is a strict `PublishLoopInput` JSON object. Choose the authority path explicitly:

- Registered-Agent workspace: supply `agent_id`, `revision`, optional `expected_previous_digest`, and `idempotency_key`; omit `authority` and `publisher`. The authenticated service resolves the exact latest enabled Agent, checks the principal and stable owner, and constructs server-derived workspace authority and publisher references. `agent_id` is a selector, not authentication or a caller-issued delegation. No provisioning receipt, running session, runtime mandate, or credential is needed for workspace publication.
- Runtime authority: omit `agent_id` and supply `authority`, `publisher`, `revision`, optional `expected_previous_digest`, and `idempotency_key`. Authority and publisher values must come from current authenticated Aegis readback, not from the model, a fixture, another session, or browser input. Do not manufacture a runtime reference when only workspace authority is available.

Use `aegis loops example` for the installed schema-valid publication template. Select an existing enabled Agent explicitly; do not infer a builder from its name. The basic-implementation template is NOT an executable implementation workflow: task and acceptance_criteria are typed ports without instruction/value-bound bindings; empty evidence requirements prove nothing; max_attempts=2 does not enforce one corrective attempt; the success terminal is not a verification gate. Required evidence needs an actual pre-pinned expected digest, verifier ID and policy version. Never invent them or claim a generic test pass proves dynamic acceptance. Execution binding remains unsupported by this template.

Direct-store CLI access is unavailable when the daemon owns the store (`control_plane_online`). Use `aegis --config OWNER_CONFIG --target CONSOLE_URL agents list`, then the same flags for `loops validate FILE`, `loops publish FILE`, and `loops show LOOP REVISION`. The URL selects the existing instance, not a credential destination: the installed adapter uses its configured protected Unix API and matches returned configuration before acting. There is no local-store fallback. Do not stop the daemon, initialize another store, extract its token, or build an ad-hoc client. Online support is limited to agents list/show/history and loops list/show/validate/publish; activation, queueing and execution are excluded.

## Verified implementation authoring

Use `loops implementation FILE` for real bounded implementation semantics, not `loops example`. Supply strict JSON with `agent_id`, `loop_id`, positive `revision`, `idempotency_key`, optional `previous_digest`, and `implementation`: schema `aegis.loop.verified-implementation.v1`, bounded `task` and `acceptance`, explicit absolute `workspace`, local non-test Go `writable_files`, `max_passes` 1–2, and `policy` with kind `go-test.v1`, local `packages`, exact `required_tests` package/name identities and timeout 1–120 seconds. Obtain these values from the owner and existing trusted tests. Output is a v3 publication candidate; online validation must resolve the actual Agent. `--output` creates a new private file and refuses overwrite.

Publication remains inactive and does not authorize native code. Controller execution separately requires operator approval to configure an absolute trusted `implementation.go_binary` and exact `implementation.authorized_contracts` digest allowlist. Never edit that configuration, restart services or queue execution merely to finish authoring. Native Go tests are trusted host code, not sandboxed. Build source with `go build`, not `go run`. See `docs/VERIFIED_IMPLEMENTATION.md`.

## Publish and verify

1. Publish only after the authenticated operator explicitly requests the exact operation and reviewed file: `aegis loops publish FILE`.
2. Aegis reauthenticates and performs fresh authority admission. Preserve denials for malformed structure, invalid ports or topology, absent evidence contracts, non-contiguous revisions, predecessor mismatch, an existing revision conflict, publisher substitution, authority drift, or unavailable persistence.
3. On success, inspect the returned `revision`, `validation`, and `decision`. Require validation outcome `valid`; match the exact Loop ID, revision, revision digest, validator, and validation digest. `decision.idempotent` describes service handling and does not weaken exact-content requirements.
4. Read back the exact immutable record with `aegis loops show LOOP REVISION`. Match its revision and digest, validation record, publisher Agent, authority context, and publication-provenance digest. Workspace readback does not require a runtime mandate or session: verify the workspace authority kind and exact registered-Agent/owner binding rather than inventing runtime provenance. On the runtime-authority path, also verify the returned mandate, stanza, runtime, and charter bindings.
5. A successful command exit, model narration, browser confirmation page, or projection alone is not publication evidence. If exact readback is absent, inconsistent, or corrupt, report publication unverified and stop.

## Activate an exact revision

1. Activation is a separate consequential operation. Obtain explicit authorization for the exact Loop revision and its current lifecycle head.
2. Prepare a strict `SetLoopLifecycleInput` JSON object containing exact `loop` revision reference, fresh stable `event_id`, and `expected_previous_digest` equal to the latest lifecycle-event digest, or empty only when no lifecycle event exists. For workspace lifecycle, supply `agent_id` and omit `authority` and `publisher`; the service derives both using the same owner-bound path as publication. For runtime lifecycle, omit `agent_id` and supply current `authority` and exact `publisher`. The CLI sets state to `active`; do not rely on a caller-supplied state.
3. Run `aegis loops activate LOOP FILE`. The positional Loop ID must equal `loop.id`. Any stale lifecycle head, foreign or invalid revision, retired Loop, publisher or authority substitution, or ambiguous authority denies.
4. Read back with `aegis loops show LOOP REVISION`. Require lifecycle state `active`, exact active revision and digest, and an appended lifecycle event whose event ID, previous digest, publisher, authority, and digest match the returned result. Verify workspace or runtime provenance according to the selected path above; do not require runtime mandate/stanza fields for workspace lifecycle.
5. Activation selects an immutable revision for new use; it does not mutate historical Graph snapshots, queue items, runs, attempts, or evidence, and it does not itself execute the Loop.

## Retire without rewriting history

1. Retirement is separate and consequential. Obtain explicit authorization and the exact current lifecycle-head digest.
2. Prepare the same strict lifecycle input, but do not substitute another revision as the retirement target. The CLI sets state to `retired`; the service records retirement for the stable Loop ID while preserving all immutable revision history.
3. Run `aegis loops retire LOOP FILE`, then read back the exact Loop view. Require lifecycle state `retired` and a matching appended retirement event chained to the prior lifecycle head.
4. Retirement is terminal for lifecycle selection: reactivation denies. Existing historical Graphs, snapshots, execution records, and evidence remain reconstructable and must not be relabeled or deleted.

## Inspection, denial, and interruption recovery

Use `aegis loops list` for authenticated inventory and `aegis loops show LOOP REVISION` for exact readback. Keep draft, active, and retired distinct. Report immutable revision structure, validation, publication provenance, lifecycle history, and current lifecycle separately.

Preserve Aegis's exact denial or error. Never repair canonical identity, authority, digests, revision sequence, validation, publisher provenance, or lifecycle history from prompt content. Never retry a changed payload under the same event or idempotency identity.

After interruption, inspect the exact immutable revision and lifecycle history before acting. If a publication or lifecycle event is already present with the intended exact digest and identity, report the returned idempotent state without duplicating it. If readback is absent, conflicting, corrupt, or unavailable, do not claim success or blindly repeat the mutation; require typed recovery or operator review.

## Secret and progressive-disclosure handling

Loop definitions and examples must contain no authentication material, credential values, broker capabilities, private prompts, raw runtime output, unrelated private host paths, or secret-shaped canaries. Verified implementation contracts necessarily bind the explicit owner-approved workspace path; do not publish it beyond the intended fleet. Report only bounded non-secret authority references and provenance returned by Aegis; do not expose credential or capability values.

Consult `specs/CANONICAL_DOMAINS.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/STORAGE.md`, the root `README.md`, and installed command help for normative and shipped behavior. Installing this advisory skill grants no Aegis identity, authority, publication right, lifecycle right, runtime capability, or filesystem/network access.

## Single authenticated Loop execution request

When the operator says “queue it” for an approved exact bounded Loop, use this single execution action, not a preparation-page handoff, binding file, second worker command, or repeated consent. Return the exact inline blocker if admission fails; never promote missing authority into executable Queue. The Queue overview omits generic prerequisite blocks; existing action and record-specific admission still fail closed.

`aegis loops queue FILE` (including the product-owned online adapter) and `POST /v1/loops/queue` accept exact `agent` and `loop` revision references, `idempotency_key`, optional normalized `inputs`, and explicit `activate: true` when activation is intended. `queue_item_id` explicitly selects a compatible legacy preparation for recovery without changing its immutable identity. Inspect the returned reason and current execution projection; HTTP success alone is not execution success.

The controller composes the minimal exact Graph, prepares or reuses one session only within already approved charter/provisioning scope, binds fresh same-Agent runtime authority, and invokes the bounded foreground worker in this single authenticated request. Foundational approval and provisioning are never automated. Missing prerequisites remain durable non-executable preparation. This is not a background scheduler or live-provider acceptance claim.

The durable `queue-session-preparation` denial fence prevents concurrent duplicate authority issuance. An interrupted preparation with no reusable authority remains blocked as `session_preparation_in_progress_or_interrupted`; explicit operator recovery is required. Do not delete the fence or mint substitute authority as automatic crash handling.
