# Execution preparation and executable admission

## Authority boundary

Definition/workspace authority is not runtime authority. An enabled registered Agent may author and submit exact owned definitions without a provisioned runtime. Such acceptance MUST NOT be described as executable Queue admission. Foundational charter and provisioning approvals remain explicit; neither a model assertion nor a workspace submission grants them.

New workspace submissions have durable `preparation-pending` state. They have zero attempts and cannot be claimed. `queued` denotes runtime-bound admission, still subject to fresh claim-time and effect-time checks. A stored record does not establish worker availability or automatic execution.

## Compatibility and recovery

Existing `awaiting-runtime` immutable facts remain valid and are never rewritten. Both preparation states permit the supported owner-authenticated runtime binding operation to append a transition to `queued` on the same Queue ID. Binding requires independently fresh exact same-Agent runtime authority. Replay of the same binding identity returns the original timestamp and digest after fresh authorization; conflicting authority or binding identity fails. Cancellation and revocation remain available before runtime binding.

Read current projection, not the immutable initial transition, to determine lifecycle. Immutable submission history is historical acceptance, not current execution status.

## Single authenticated Loop execution request

`aegis loops queue FILE` (including the product-owned online adapter) and `POST /v1/loops/queue` accept exact `agent` and `loop` revision references, `idempotency_key`, optional normalized `inputs`, and explicit `activate: true` when activation is intended. `queue_item_id` explicitly selects a compatible legacy preparation for recovery without changing its immutable identity. Inspect the returned reason and current execution projection; HTTP success alone is not execution success.

The controller composes the minimal exact Graph, prepares or reuses one session only within already approved charter/provisioning scope, binds fresh same-Agent runtime authority, and invokes the bounded foreground worker in this single authenticated request. Foundational approval and provisioning are never automated. Missing prerequisites remain durable non-executable preparation. This is not a background scheduler or live-provider acceptance claim.

The durable, create-only `queue-session-preparation` denial fence prevents concurrent duplicate authority issuance. Successful session preparation separately creates a `queue-session-prepared` receipt containing the exact runtime authority reference. When a reservation exists, reuse requires that receipt to match the freshly resolved authority exactly; active authority alone, including authority left by a failed launch, is insufficient. Before binding, the controller also requires the exact persisted session and mandate to validate against the authority context, with status `running`, a nonempty runtime session ID and process-start identity, and a positive runtime PID. These recorded process attributes are not a claim of host confinement.

An interrupted reservation without matching success proof remains blocked as `session_preparation_in_progress_or_interrupted`, even if active authority exists. Explicit operator recovery is required; neither deleting the fence nor minting substitute authority is automatic crash reconciliation. Prior expired, revoked, or interrupted authority is not implicitly resurrected.

## Collection and evidence boundaries

`GET /v1/queue`, `queue list`, and the Queue console exclude current preparation and never-bound terminal preparation using immutable provenance plus exact persisted runtime binding. `GET /v1/preparations`, `queue preparations`, and `/console/preparations` expose that separate history. Exact `queue show` retains both. Lifecycle labels use the current projection; initial state is explicitly historical.

Repository tests use isolated state and synthetic runtime fixtures with independently executed native checks. They do not establish access to a live host or prove live-provider/model acceptance.
