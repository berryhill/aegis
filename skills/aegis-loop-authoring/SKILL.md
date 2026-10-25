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

## Contribution acceptance boundary (#386)

For contribution implementation/testing, read the repository's
[canonical contribution happy path](../../.hermes/contribution-happy-path-contract.md)
and mandatory #375–#379 stage/evidence/owner matrix. This repository-only reference
is not bundled/installed adoption; #348 owns final matching distribution. Without
a checkout use installed discovery and report missing capability, never invent
stages/APIs. Selected-file Doer, native coding and whole-contribution success are
separate evidence claims. Current v6 intake is not the whole autonomous journey;
Queue/model/build/PR/CI alone cannot attest it. Narrower scope and real gates prevail.


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
- `aegis loops doer FILE [--output NEW_FILE]` (local canonical v4 selected-file draft; no authority, publication or execution)
- `aegis loops doer-reusable FILE [--output NEW_FILE]` (local typed v5 reusable draft; does not publish or execute; Run separately binds exact direct or Graph inputs and approved task authority)
- `aegis loops templates` (included non-authorizing authoring recipes)
- `aegis loops hello FILE --output NEW_FILE` (local canonical v2 fixed-output draft; exclusive create; named Agent, Loop ID, revision, optional predecessor and idempotency key; exact UTF-8 `hello` without newline, precommitted SHA-256 and artifact verifier policy; no receipt or execution)
- `aegis loops example` (typed publication template only)
- `aegis loops validate FILE` (authenticated workspace structural validation, no publication)
- `aegis loops list`
- `aegis loops inventory` (all published revisions, including archived logical Loops)
- `aegis loops availability LOOP` (discovery state, sequence and CAS digest)
- `aegis loops archive LOOP FILE` (reversibly hide a logical Loop from discovery)
- `aegis loops restore LOOP FILE` (restore published discovery)
- `aegis loops publish FILE`
- `aegis loops queue FILE` (one authenticated exact request; inspect returned blocker/execution)
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

Direct-store CLI access is unavailable when the daemon owns the store (`control_plane_online`). Use `aegis --config OWNER_CONFIG --target CONSOLE_URL agents list`, then the same flags for `loops validate FILE`, `loops publish FILE`, and `loops show LOOP REVISION`. The URL selects the existing instance, not a credential destination: the installed adapter uses its configured protected Unix API and matches returned configuration before acting. There is no local-store fallback. Do not stop the daemon, initialize another store, extract its token, or build an ad-hoc client. The shipped online adapter also routes exact lifecycle mutations and `loops queue FILE` through authenticated service endpoints; this transport grants no runtime authority and does not itself prove an attempt or disposition. Confirm the installed command surface before using it.

## Single retained-task create and execute

When the owner requests creation **and execution**, prefer `aegis --config OWNER_CONFIG --target CONSOLE_URL loops task-execute FILE`. Strict create input is `{"create":{"creation_key":"STABLE_REQUEST_KEY","agent":EXACT_AGENT_REF,"loop_id":"EXACT_LOOP_ID","revision":1,"contract":DOER_CONTRACT}}`. Resume input is only `{"id":"RETURNED_TASK_ID","expected_version":RETURNED_VERSION}`. This foreground service checks current full readiness, including the bounded helper probe, before publication, activation or Queue mutation. Missing independent setup returns the retained `task`, readiness reason and contextual `confirmation_url`, without publishing. The retained task is not a user-facing draft or an authority grant.

Present that URL to the human without operating their controls. After independent exact decisions, read the same task and resume this same operation. Its publication and Run keys are service-derived and stable; uncertain transport outcomes require same-task reconciliation, not a new creation key. Execution freezes the exact version against edits/rebinding. Inspect authoritative `run.execution` artifact, verifier receipt and disposition; `execution_observed` is not a claim of success. No background scheduling, provider/model changes or synthetic approval is performed. The isolated regression uses a qualified synthetic runtime, not live-provider acceptance.

## Candidate readiness and retained proposals

Exact published v5 readiness accepts `agent`, exact `loop`, normalized `inputs`, optional `idempotency_key`, `activate`, and `probe`. Never mix this with v4 `candidate` or partial refs. Null, duplicate and unknown fields deny; zero omitted typed structs remain compatible. Observation never reserves intent: the optional key checks retained payload identity and returns the same `request_id` and `contract_digest` used by Run.

Host absent/invalid/expired causes require `review_exact_host_write_contract` for v4; v5 returns `exact_v5_host_review_adapter_unavailable`, not a URL or grant. Unsafe workspace requires `review_exact_workspace_scope`; Go/Laya/home integrity failures require `review_controller_dependency_integrity`. Cancellation and closed unknown errors require `controller_diagnostic_adapter_unavailable`. Downstream checks stay `not_checked`; helper qualification is protocol-only. Existing exact preauthorization may support Run; this is not real-provider or protected-v5 review/resume proof.

Before execution-intended publication, use `aegis loops readiness FILE` with strict JSON `{ "agent": EXACT_REVISION_REF, "candidate": CANONICAL_V4_REVISION }`. `can_author` and `can_execute` are separate: deliberate registry-only inactive publication is allowed when authoring is ready even if execution is blocked. Readiness grants no authority and writes no Loop, Graph, Queue item, approval or receipt. Ordinary inspection does not invoke the helper. `--probe` explicitly requests the bounded (10-second maximum) local protocol check, not task eligibility or provider acceptance.

Use `aegis loops task-save FILE` to retain a proposal (`agent`, `loop_id`, `revision`, optional `previous_digest`, `contract`); update with its exact `id` and `expected_version`. `task-show ID` reads authoritative retention. `task-continue FILE` takes `id`, `expected_version`, `expected` (old exact Agent reference), and `charter` (already approved exact successor). Compatible continuation archives the prior binding and preserves contract, lifetime and original Loop/publication identities. It does not approve, provision, rewrite old definitions/receipts/runs or automatically publish. Stale versions, changed or corrupt publication, foreign ownership and frozen runtime bindings deny. All four commands support the existing `--config OWNER_CONFIG --target CONSOLE_URL` owning-service Unix transport without opening a second store.

A tool-free Codex successor may explicitly set `hermes.provider_authentication.mode: controller-codex-access-token.v1`. This is controller-owned inference transport, not an Agent credential scope or broker grant; zero Agent tools/credentials, fresh exact charter approval/provisioning and clean session admission remain mandatory. Never remove existing live credential scopes, copy host auth files, read credentials or edit controller configuration to cure readiness. Missing/expired controller binding is an operator gate; no token or refresh capability is returned by these adapters. The access-only transport requires Hermes 0.21.3 and the adapter's exact qualified transport-source fingerprints; other sources deny `doer_provider_auth_runtime_unqualified`, independently of general Hermes version support. Tokens must remain outside the 120-second refresh margin throughout the approved session. Offline synthetic-pool qualification is not live provider authentication or successful task execution. Installed/live qualification remains a separate proof boundary.

## Browser setup and the original create/run request

Prefer `aegis --config OWNER_CONFIG --target CONSOLE_URL loops setup-browser FILE` with strict `{"id":"EXACT_RETAINED_ID","expected_version":EXACT_VERSION}`. This read-only handoff returns the exact retained `task`, `confirmation_url`, and `independent_setup_required`; it needs neither DISPLAY nor a native companion and does not claim readiness or completion. Give the URL to the human; never operate their authentication or decision controls. The existing page supports exact successor, provisioning and host-write review with session-bound single-use receipts, fresh authentication, origin and CSRF checks.

After actual decisions, `loops task-show ID` reads the current exact retained version. Preserve the original task/output contract and fixed expiry. Compatible successor approval advances only version and runtime participant binding; original publication identity survives. Resume the already requested create/run through `loops task-execute FILE` with `{"id":"EXACT_RETAINED_ID","expected_version":EXACT_CURRENT_VERSION}`; the owning service reuses the verified existing immutable Loop (publishing only on authoritative absence) and calls its existing foreground Queue operation. Historical publisher provenance remains unchanged. Do not hand-coordinate publication files, reference JSON, Run keys or a worker, and do not ask the human to consent to the task again. Setup decisions are independent authority decisions, not repeated task consent. Never relabel a browser password subject as local OS, report an inactive definition as completion, rotate an uncertain publication/Run key, or invent an original Run request that was never made.

## Optional protected desktop setup through installed skills (source candidate)

Use this workflow only when installed `aegis loops --help` lists `setup-protected`. Skill installation itself grants no authority. The agent requests a product-launched independent desktop interaction; it never operates a human terminal, browser, password field or approval dialog. Do not run the hidden `doer-setup-companion` manually. Do not use `setup-approve` through an agent tool/PTY: that command is solely for a genuine operator's direct terminal invocation.

1. Read the same retained task with `aegis --config OWNER_CONFIG --target CONSOLE_URL loops task-show DRAFT_ID`. Keep its exact `id`, `version`, `agent`, `contract`, publication key and expiry; do not recreate the task when a prerequisite blocks it.
2. For an explicitly requested remedy, create a private JSON invocation file containing only `{"id":"EXACT_RETURNED_DRAFT_ID","expected_version":EXACT_RETURNED_VERSION,"action":"successor"}`. Run `aegis --config OWNER_CONFIG --target CONSOLE_URL loops setup-protected FILE`. The typed owning service validates current principal/ownership and exact draft version, and launches its pinned hidden companion. It accepts no password, decision, review receipt, charter bytes or approval flag. `successor` is only the bounded credential-only correction described below, not an arbitrary charter editor.
3. The human enters the enrolled Aegis principal password (not the authority passphrase) in an independently displayed protected native UI, then reviews the complete exact scope in a separate protected native confirmation. Root-owned trusted helpers capture input outside agent stdin. No GUI means unavailable; never fall back to a model-operated terminal or generic typed approve. Password session, CSRF and single-use review handle remain memory-only in the companion, never CLI output or agent input.
4. On `approved`, require exact owning-service readback, not helper exit text. For compatible successor, verify version increments once and the same Agent advances to its immediate approved Registry revision. Contract, fixed retention, original Loop ID/revision/predecessor and publication key survive. Existing exact validated publication is reused with its historical publisher provenance unchanged; runtime participant admission is separate. Corrupt, changed, foreign, unknown publication and frozen runtime binding deny without replacement. Re-read the draft before another action. `successor` grants no provisioning, host write, publication or Run.
5. Only with separate owner intent, invoke the same command with `action: "provision"` and the new exact draft version. The human reviews the complete canonical charter/plan/effects and independently approves that exact plan for deterministic apply. Require its returned verified receipt, exact plan/charter digests and verified artifacts. Then independently invoke `action: "host"` to review the exact task/workspace/paths/assertion/retry contract and signed expiring host grant. Each action requires its own fresh password/native review. Neither action runs the task.
6. For an already requested create-and-execute task, use `loops task-execute FILE` with the current exact ID/version instead of the following lower-level authoring-only workflow. For explicitly publication-only work, resume authoring from the returned task: build a private Doer authoring file using `agent_id: draft.agent.id`, `loop_id`, `revision`, optional `previous_digest`, `idempotency_key: draft.publication_key`, and `doer: draft.contract`; `loops doer FILE --output NEW_FILE`, authenticated `loops validate NEW_FILE`, then explicitly authorized `loops publish NEW_FILE`. Read back the exact immutable revision via `loops show LOOP REVISION`.
7. For separately requested Run, prepare `loops queue FILE` with exact readback `agent` and `loop` revision references, a stable owner-approved `idempotency_key`, and `activate: true` only if intended. The owning service repeats all current runtime/provider/Laya/host/receipt prerequisites. Require Queue attempt, independently checked selected-file artifact, verifier receipt and successful terminal disposition; `approved`, readiness or publication is not execution.

Preserve `denied`, `cancelled`, `timeout`, `unavailable`, `outcome_unknown` and `response_unverified` distinctly. On uncertain mutation, read retained draft, exact Agent history, receipts and signed host consent through supported owning-service readback before any retry; do not rotate intent or manufacture a human decision. Provider/model authentication, independent desktop interaction and real selected-file Run remain installed/live acceptance gates, not unit-test claims.

## Guided credential-only successor review

The guided setup page now reviews a native credential-only successor for the same retained draft: only a sole enabled stanza already without capabilities, tools/toolsets, memory or persistent integrations and with exactly `provider:codex` is eligible. It removes that one credential scope and requests `controller-codex-access-token.v1`; model, provider, runtime, authentication methods/selectors, other policies and other stanzas remain unchanged. Exact compatible imported canonical successors are reused, not automatically approved. Both original and proposed canonical bytes/digests are displayed before an independent principal decision. Browser confirmation consumes a session-bound, expiring, single-use receipt; rejection applies no authority. No setup step implies provider readiness, provisioning, host-write consent, publication or Run.

The skill-native review uses typed owning-service transport: `POST /v1/loops/doer/drafts/:id/setup-review` takes strict `{"id": DRAFT_ID, "expected_version": VERSION}` and returns `proposal`, `receipt`, `expires_at`, and `confirmation_url` (relative to the configured console origin). Present both exact canonical charters and give the human this URL. The human authenticates independently, requests a fresh exact proposal review on the existing protected browser setup page, and confirms or rejects its session-bound, expiring, single-use receipt. The typed review receipt is not browser approval proof. Never have the model click or type human confirmation, carry browser credentials, or manufacture assent. `POST /v1/loops/doer/drafts/:id/setup-decision` accepts strict `{"id": DRAFT_ID, "receipt": RECEIPT, "decision": "reject"}` for no-effect rejection. `approve-successor` fails closed with HTTP 403 even with a valid review token: independently captured typed continuation proof is unavailable. Account-level SO_PEERCRED authentication does not prove a human decision. Typed review receipts bind transport identity, expire after five minutes, are single-use for rejection, and fail closed on restart; at most 1024 outstanding reviews are retained. Optional `loops setup-review FILE` and `setup-decide FILE` wrappers require `--target` and never open another store. After human browser confirmation, use typed draft readback to recover the same task and exact successor; provisioning, host consent, publication and Run remain separate. If the protected browser journey is unavailable, report that gate instead of manual binding-file commands or an approval string. Partial writes remain visible; recover exact facts without rotating intent.

## Verified implementation authoring

Use `loops implementation FILE` for bounded Go implementation semantics, not `loops example`. Supply strict JSON with `agent_id`, `loop_id`, positive `revision`, `idempotency_key`, optional `previous_digest`, and `implementation`: schema `aegis.loop.verified-implementation.v1`, bounded `task` and `acceptance`, explicit absolute `workspace`, local non-test Go `writable_files`, `max_passes` 1–2 (or 1–3 with exact `decision_mode: doer.v1`), and `policy` with kind `go-test.v1`, local `packages`, exact `required_tests` package/name identities and timeout 1–120 seconds. Obtain these values from the owner and existing trusted tests. Output is a v3 publication candidate; online validation must resolve the actual Agent. `--output` creates a new private file and refuses overwrite.

For Doer's selected-file one-task Loop, use `aegis loops doer FILE [--output NEW_FILE]`, not the Go-test-only implementation builder. The JSON supplies exact existing `agent_id`, `loop_id`, `revision`, `idempotency_key`, optional predecessor digest, and a `doer` object with bounded task, absolute operator-approved workspace, relative allowlisted `writable_files`, one matching `verify_file`, optional `expected_text` (absent means regular-file presence; explicit empty string asserts empty trimmed UTF-8 unless raw-byte policy is selected), optional `exact_bytes: true` (requires `expected_text` and checks exact raw UTF-8 bytes without trimming; omitted/false preserves legacy trimmed semantics), and `max_attempts` 1–3. The closed v4 step graph is Laya eligibility → bounded tool-free Hermes edits → Laya report judgment → independent selected-file check → terminal assessment or bounded diagnosis/retry. The builder prints the exact contract digest; publication remains an inactive definition and does not configure local Laya, select a model or authorize host writes. Execution requires separately approved host-write scope: either the existing operator-approved `implementation.authorized_contracts` allowlist or the guided Console's exact signed expiring host-write approval. Trusted `implementation.go_binary`, absolute `implementation.laya_python` and private offline `implementation.laya_home`, plus normal Graph/Queue runtime authority, remain independent prerequisites. Never infer model availability from the skill or edit controller configuration merely to run a task. See `docs/DOER_LOOP.md` for the delivered bounded behavior and limits. It is not the upstream Doer skill's experimental model-family router or a generic multi-node Graph scheduler.

Before reporting v4 as ready to request, check the exact charter's selected tool-free stanza and usable model before its verified provisioning receipt, then current Hermes runtime/version, authorized contract digest, trusted local checker/workspace prerequisites and one bounded typed Laya response. If both the model and receipt are absent, the model/charter successor is the first independent review action; provisioning the unchanged charter cannot cure the model blocker. `loops queue FILE` with explicit activation and direct lifecycle activation recheck those conditions before new lifecycle/Graph/Queue mutation and return a named blocker without a new executable preparation when they are missing. Its `request_id` and exact `required_charter` identify the resumable intent and prerequisite revision; `required_action: plan_preview_exact_charter` directs the operator to the existing exact-charter plan review, not an automatic approval. Keep the unchanged request file/key for a retry after independently approved provisioning of that exact charter. `review_charter_successor_with_usable_model` is a separate model/charter approval gate, not a license to change the current authority: a successor requires a newly reviewed exact Agent/Loop request and new key, not replay of the old request. The Laya verdict grants no authority. Do not queue merely to discover a missing foundational approval, and do not claim that a preflight pass substitutes for fresh worker claim/effect admission or live-provider proof.

The distinct `doer-reusable` builder defines a typed v5 template without an embedded task or workspace. Agents can execute the same exact published revision with `loops queue FILE`: supply exact Agent/Loop refs, stable intent key, explicit activation and normalized `inputs` for task, workspace, writable_files, verify_file and optional expected_text. Keep workspace/files/budget in the existing fixed request recipe; task and separately declared assertion vary by intent. All five canonical ports remain; v5 has zero Graph-dataflow outputs and trimmed-text semantics (no exact_bytes port). Each resulting contract requires prior exact host authorization and fresh runtime admission; typed inputs and template defaults grant none. Retrieve the named task and stored verified result with `queue result ITEM` (owned by aegis-evidence-disposition); humans inspect the selected Queue detail, not a new mandatory launch form. Same-key replay returns the existing run; changed payload conflicts. Preserve the already-satisfying preclaim guard and existing output. For the same exact owned successful direct v5 predecessor, the Run service verifies terminal evidence and idle authority, uses the existing session lifecycle to retire it, then prepares fresh authority; no manual session-management handoff is needed. Failed, busy, foreign, drifted or unknown predecessors remain blocked; a failed task cannot reset its pass budget through a new key. See `docs/REUSABLE_DOER.md` and `examples/reusable-doer-queue.json`.

Each normalized input entry has the exact shape `{"port_id":"task","type":"string","value":"THE TASK"}`. `workspace` and `verify_file` also use `type: "string"`; `writable_files` uses `type: "array"` and a JSON array of approved relative paths. Optional `expected_text` uses `type: "string"` and is a predeclared assertion, not inferred from prompt prose. Resolve Agent/Loop revision objects through authenticated readback, then use the existing `--config OWNER_CONFIG --target CONSOLE_URL loops queue FILE` owning-service route and `queue result RETURNED_QUEUE_ITEM`. Do not invent authority references, modify approved configuration, or add a browser confirmation between already-authorized task requests.

### Terminal-run continuity

The owner-principal zero-authority ephemeral Run path prepares exact v5 scope under the same narrow policy as v4; registration, a template or readiness grants none. For a new task, the service verifies and retires only an exact idle terminal predecessor. A prior failure must not lock the entire Agent or unrelated Loop. A fresh key cannot reset the same failed concrete contract, even after intervening work. Exact v5 continuation uses the existing typed `reexecution` predecessor Queue/disposition references and derives remaining passes from immutable history, never caller counts. One successor, original failed records, approval expiry and runtime effect admission remain enforced. Completed zero-authority diagnostics and recorded pre-launch refusals are reconciled internally only from exact durable proof; unknown interrupted starts still deny. Do not clear fences, republish to escape failure, or hand the operator session/binding files. Earlier failure-block descriptions apply to unsupported or unproven predecessor shapes, not every failed v5 task. See `docs/REUSABLE_DOER_RUN_CONTINUITY.md`.

Publication remains inactive and does not authorize native code. Controller execution separately requires operator approval to configure an absolute trusted `implementation.go_binary` and exact `implementation.authorized_contracts` digest allowlist. Never edit that configuration, restart services or queue execution merely to finish authoring. Native Go tests are trusted host code, not sandboxed. Build source with `go build`, not `go run`. See `docs/VERIFIED_IMPLEMENTATION.md`.

## Bounded repository CODING (additive v4)

For an existing-repository Go task, `loops doer FILE` also accepts optional
`doer.coding` with `acceptance`, `repository_context` and the existing
`go-test.v1` policy. Set `verify_file` to empty and omit selected-file text/byte
assertions; do not reinterpret a presence receipt as software acceptance. Pin
the actual task, one bounded production-file allowlist, trusted existing tests,
explicit immutable `go.mod` and a fixed implementation budget before execution.

The context manifest binds exact task/source commitments, ordered relative
paths, observed byte counts/digests/roles and finite declared file/per-file/total
limits. The controller reloads actual UTF-8 bytes; no repository crawl, implicit
module read, silent truncation, private-path access or writable trusted tests.
The entire serialized managed input must additionally fit 64 KiB. Context is
untrusted task data, never authentication or authority.

Native execution independently requires an approved trusted absolute
`implementation.go_binary` and the exact **outer Doer-contract digest** in the
operator's configured native allowlist. A Loop digest, derived implementation
digest or signed selected-file host approval is insufficient. Qualified local
Laya, selected Agent/model/provider/login and fresh runtime admission remain
separate. Do not alter configuration merely to complete authoring; report exact
native-authority/checker blockers through readiness. Native Go code executes as
the controller OS user and is not sandboxed.

Use existing retained/exact publication and the single supported exact Run
operation under already-approved scope; no manual binding or separate worker.
Reload authenticated `queue show` coding history, exact artifacts,
`aegis.implementation.verifier` receipts and the terminal disposition. Verify
actual source/test input commitments and managed delivery, unchanged trusted
tests, behavioral RED→GREEN, cumulative reserved passes and original failed
history. Known complete-pass correction, or a prepared managed reservation with
durable no-implement/no-apply proof and unchanged recorded preimage, uses existing
exact requeue lineage without restoring consumed passes. Unknown/interrupted or
partial-apply effects and exhaustion deny rather than reset budget or
republish. Synthetic fixtures, real managed-provider, packaged and installed
proof are distinct tiers. See `docs/BOUNDED_CODING.md` for the source contract.

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

Use `aegis loops list` for published discovery, hiding archived logical Loops; `aegis loops inventory` includes every published revision, including archived Loops. Use `aegis loops show LOOP REVISION` for exact readback. Keep draft, active, and retired distinct from availability. Report immutable revision structure, validation, publication provenance, lifecycle history, and current lifecycle separately.

### Reversible availability (source candidate)

Confirm installed help for `loops availability LOOP` and `loops archive|restore LOOP FILE`. These and `loops inventory` support `--config OWNER_CONFIG --target CONSOLE_URL` through the protected owning service, not a second writer. The MCP Loop adapter is UNAVAILABLE; CLI/HTTP coverage is not MCP integration or whole-issue completion.

Availability is `published` or `archived`, independent of activation/retirement. Archive covers the logical ID across all current and future revisions, not deletion, retirement, runtime stop, Queue cancellation or an authority grant. Existing pinned Graphs, Queue work, executions, evidence and history remain preserved. Restore reverses discovery hiding without changing lifecycle or executing work.

Inspect inventory and availability before mutation. Strict `app.SetLoopAvailabilityInput` JSON uses `agent_id`, `head` (exact current publication reference with `id`, `revision`, `digest`), stable `event_id`, next `sequence`, `previous_digest` from availability readback (empty/omitted only before the first event), and `expected_state` from that readback. For workspace requests omit `authority` and `publisher`; the online adapter requires server-derived values. CLI archive/restore forces `state` to `archived`/`published`. The positional Loop ID must equal `head.id`. Preserve owner admission, stale publication-head/state/sequence/digest conflicts and exact replay checks; do not invent digests or rotate uncertain event identities.

API: `GET /v1/loops/inventory`, `GET /v1/loops/:loop/availability`, `PUT /v1/loops/:loop/availability`. PUT uses the same input with explicit target `state`. Availability readback returns `loop_id`, `state`, `sequence`, optional `digest`; mutation returns `event` and `idempotent`. Verify the exact returned event and subsequent availability readback. Console Published/Archived views offer direct Archive/Restore actions with existing authentication, origin/CSRF and owner checks, not new drafts or setup friction. See `docs/loop-availability.md`; installed acceptance remains separate.

Preserve Aegis's exact denial or error. Never repair canonical identity, authority, digests, revision sequence, validation, publisher provenance, or lifecycle history from prompt content. Never retry a changed payload under the same event or idempotency identity.

After interruption, inspect the exact immutable revision and lifecycle history before acting. If a publication or lifecycle event is already present with the intended exact digest and identity, report the returned idempotent state without duplicating it. If readback is absent, conflicting, corrupt, or unavailable, do not claim success or blindly repeat the mutation; require typed recovery or operator review.

## Secret and progressive-disclosure handling

Loop definitions and examples must contain no authentication material, credential values, broker capabilities, private prompts, raw runtime output, unrelated private host paths, or secret-shaped canaries. Verified implementation contracts necessarily bind the explicit owner-approved workspace path; do not publish it beyond the intended fleet. Report only bounded non-secret authority references and provenance returned by Aegis; do not expose credential or capability values.

Consult `specs/CANONICAL_DOMAINS.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/STORAGE.md`, the root `README.md`, and installed command help for normative and shipped behavior. Installing this advisory skill grants no Aegis identity, authority, publication right, lifecycle right, runtime capability, or filesystem/network access.

## Single authenticated Loop execution request

## Exact owner-principal Run preparation (source candidate)

For a published selected-file v4 Loop, one fresh authenticated owner-principal
`loops queue` request now prepares its **same immutable Loop** without separate
setup consent when the selected stanza has zero capabilities, tools/toolsets,
memory and credential scopes and the runtime target is `aegis-owned-ephemeral`.
Hermes builds a disposable home directly from the exact mandate; this path does
not create the persistent provisioned mapping, a plan, a generic approval or a
provisioning receipt. `required_operations: ["provision"]` is not rewritten or
waived for actual provisioning. Generic session entry still requires a verified
provisioning receipt. Readiness marks the receipt `not_required` with reason
`zero_authority_ephemeral_doer`, not falsely verified.

The exact authenticated Run is also the owner-principal's write-scope action:
the controller persists the existing signed exact host grant with truthful
`run_request_id` provenance, bounded by authentication expiry/freshness. It does
not infer a grant from publication, a draft, readiness, workspace ownership or
UID alone. The durable intent fence, exact latest enabled Registry/stable-owner
checks, immutable charter and Loop bindings, protected workspace/path checks,
provider admission, fresh per-effect host admission and independent selected-file
verification remain enforced. New authority, foreign owners, ordinary workspace
actors and persistent runtime targets do not qualify. Explicit protected setup
remains available when genuinely independent new authority is required; it is
not a mandatory detour for this zero-authority Run.

The controller does **not** install or choose Laya/model/checker dependencies.
An absent execution controller/Laya configuration returns
`doer_runtime_unconfigured` / `configure_controller_doer_runtime` inline with the
same retained request identity and no new Graph/Queue/claim. Configured helpers
still undergo the real availability and task-eligibility checks. Tests use
synthetic Hermes/Laya protocol fixtures; they do not qualify a live provider.


When the operator says “queue it” for an approved exact bounded Loop, use this single execution action, not a preparation-page handoff, binding file, second worker command, or repeated consent. Return the exact inline blocker if admission fails; never promote missing authority into executable Queue. The Queue overview omits generic prerequisite blocks; existing action and record-specific admission still fail closed.

`aegis loops queue FILE` (including the product-owned online adapter) and `POST /v1/loops/queue` accept exact `agent` and `loop` revision references, `idempotency_key`, optional normalized `inputs`, and explicit `activate: true` when activation is intended. `queue_item_id` selects only the same request key's exact accepted Queue item for recovery without changing its immutable identity. Inspect the returned reason and current execution projection; HTTP success alone is not execution success.

The authenticated Loops Console retains the selected-file v4 task as an owner-bound versioned draft. Return to that same draft after setup or interruption; stale draft versions deny. Its contextual setup separately reviews an existing compatible imported charter successor, exact provisioning plan/approval/application, and a signed expiring approval for the exact host-write contract. It does not import a charter, configure a model or grant generic native-test authority. Publication remains a separate exact preview/confirmation under registered-Agent workspace authority without a runtime session. A published v4 detail provides a native Run form with a server-generated request key; inspect the returned blocker/request identity or exact linked Queue readback.

If the browser principal cannot select the local-OS-only tool-free stanza, use the draft's contextual independent local-review control. The product launches its native approval companion with protected inherited transport and an exact peer/process binding. Do not manually invoke `aegis doer-approval-companion`, copy transport material, implement a client for its protected continuation endpoints, or describe that hidden command as the builder UX. Independent approval binds the exact browser session/requester, draft version, candidate, Agent/charter/runtime and unchanged Run intent; it does not relabel the password principal as local OS, union permissions, publish, or automatically Run. Decline, cancellation, expiry, drift and unavailable native review remain distinct blockers. Observe authoritative signed approval/readiness before returning to publication and the same Run intent; a companion exit or browser status is not approval evidence. If the required product control is unavailable in the installed release, report the missing adapter rather than teaching a manual workaround.

The controller uses direct Loop submission for new intents and preserves historical synthetic Graph replay; it prepares or reuses one session only within already approved charter/provisioning scope, binds fresh same-Agent runtime authority, and invokes the bounded foreground worker in this single authenticated request. Foundational approval and provisioning are never automated. Foundational v4 blockers reserve a durable request intent but create no Graph or executable Queue item; an already admitted Queue item may instead remain in non-executable preparation. This is not a background scheduler or live-provider acceptance claim.

The durable `queue-session-preparation` denial fence prevents concurrent duplicate authority issuance. An interrupted preparation with no reusable authority remains blocked as `session_preparation_in_progress_or_interrupted`; explicit operator recovery is required. Do not delete the fence or mint substitute authority as automatic crash handling.

## Explicit contribution intake (#375)

In an authenticated manager session use `/contribute select <published-loop>` then `/contribute run <issue-url> [narrower-scope]`. The profile must be an active immutable v6 contribution Loop with exact controller, builder and tester Registry/charter/runtime references. No operator bindings or internal idempotency key are needed. Ordinary chat and quoted/tutorial URLs do not admit work. Use `/contribute status`, `/contribute stop`, or `/contribute narrow <closed-delivery>` on the retained invocation; closed restrictions are `no-merge`, `no-publish`, and `local-only`, and can only narrow. Readback exposes the exact sealed contract and durable control sequence. Stop is sticky and uses the current authenticated session. This is intake only: Queue state does not attest whole-contribution completion, and non-local delivery execution is unavailable pending sibling implementation.
