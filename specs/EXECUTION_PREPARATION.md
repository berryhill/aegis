# Execution preparation and executable admission

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

## Authority boundary

Definition/workspace authority is not runtime authority. An enabled registered Agent may author and submit exact owned definitions without a provisioned runtime. Such acceptance MUST NOT be described as executable Queue admission. Foundational charter and provisioning approvals remain explicit; neither a model assertion nor a workspace submission grants them.

Accepted workspace submissions have durable `preparation-pending` state. They have zero attempts and cannot be claimed. Selected-file Doer v4 must first reject missing foundational execution prerequisites inline before a new activation, Graph or Queue submission; this pre-submission blocker is not a preparation record. `queued` denotes runtime-bound admission, still subject to fresh claim-time and effect-time checks. A stored record does not establish worker availability or automatic execution.

## Compatibility and recovery

Existing `awaiting-runtime` immutable facts remain valid and are never rewritten. Both preparation states permit the supported owner-authenticated runtime binding operation to append a transition to `queued` on the same Queue ID. Binding requires independently fresh exact same-Agent runtime authority. Replay of the same binding identity returns the original timestamp and digest after fresh authorization; conflicting authority or binding identity fails. Cancellation and revocation remain available before runtime binding.

Read current projection, not the immutable initial transition, to determine lifecycle. Immutable submission history is historical acceptance, not current execution status.

## Single authenticated Loop execution request

When the operator says “queue it” for an approved exact bounded Loop, use this single execution action, not a preparation-page handoff, binding file, second worker command, or repeated consent. Return the exact inline blocker if admission fails; never promote missing authority into executable Queue. The Queue overview omits generic prerequisite blocks; existing action and record-specific admission still fail closed.

`aegis loops queue FILE` (including the product-owned online adapter) and `POST /v1/loops/queue` accept exact `agent` and `loop` revision references, `idempotency_key`, optional normalized `inputs`, and explicit `activate: true` when activation is intended. `queue_item_id` selects only the same request key's exact accepted Queue item for recovery without changing its immutable identity. Inspect the returned reason and current execution projection; HTTP success alone is not execution success.

The controller composes the minimal exact Graph, prepares or reuses one session only within already approved charter/provisioning scope, binds fresh same-Agent runtime authority, and invokes the bounded foreground worker in this single authenticated request. Foundational approval and provisioning are never automated. After submission, missing runtime prerequisites remain durable non-executable preparation; before a new selected-file Doer v4 submission, missing exact receipt/model/controller-local prerequisites return a named inline blocker without a new Graph or Queue item. This is not a background scheduler or live-provider acceptance claim.

The durable, create-only `queue-session-preparation` denial fence prevents concurrent duplicate authority issuance. Successful session preparation separately creates a `queue-session-prepared` receipt containing the exact runtime authority reference. When a reservation exists, reuse requires that receipt to match the freshly resolved authority exactly; active authority alone, including authority left by a failed launch, is insufficient. Before binding, the controller also requires the exact persisted session and mandate to validate against the authority context, with status `running`, a nonempty runtime session ID and process-start identity, and a positive runtime PID. These recorded process attributes are not a claim of host confinement.

An interrupted reservation without matching success proof remains blocked as `session_preparation_in_progress_or_interrupted`, even if active authority exists. Explicit operator recovery is required; neither deleting the fence nor minting substitute authority is automatic crash reconciliation. Prior expired, revoked, or interrupted authority is not implicitly resurrected.

A completed failed session start is distinct from an unknown interruption. An
authenticated retry of the same exact owner-principal Doer Run may append a
bounded child reservation only with immutable failure/transition evidence,
unchanged Queue/Agent/Loop bindings, no claim, attempt, artifact or runtime
binding, and proof that no process was launched or that the exact process was
synchronously reaped. Failed authority is retired through normal authoritative
commands, never reused. Original fences, mandates and history remain intact.
Legacy recovery is limited to the reproducible nil-versus-explicit-empty
zero-authority mandate defect with a unique authoritative pre-launch mandate
and no issued authority context; elapsed time or process absence is not proof.

Controller-authenticated tool-free Hermes sessions launch the qualified
headless `tui_gateway.entry` protocol, not the interactive TUI. Admission checks
the actual launched process's ready event and zero-tool snapshot before binding.

## Collection and evidence boundaries

`GET /v1/queue`, `queue list`, and the Queue console exclude current preparation and never-bound terminal preparation using immutable provenance plus exact persisted runtime binding. `GET /v1/preparations` and `queue preparations` retain diagnostic history for compatibility; they are not a user management handoff. There is no Preparation console page or navigation. Exact blockers are returned by the originating queue action; accepted preparation retains them on its durable record, while an early v4 Doer preflight blocker has no Queue record to show. Lifecycle labels use the current projection; initial state is explicitly historical.

Repository tests use isolated state and synthetic runtime fixtures with independently executed native checks. They do not establish access to a live host or prove live-provider/model acceptance.
