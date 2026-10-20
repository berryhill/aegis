# Loop discovery availability (source candidate)

Published/Archived is reversible discovery metadata for a logical Loop ID, not
execution lifecycle. Archive hides every current and future revision of that ID
from ordinary published discovery. Restore returns it to discovery. Neither
operation edits immutable definitions, activates or retires a revision, stops a
runtime, cancels Queue work, deletes data, or grants authority. Existing exact
pinned Graphs, Queue snapshots, executions, evidence and historical links remain
preserved and readable. Retirement remains a separate terminal lifecycle action.

## CLI and owning service

Confirm these operations in the installed `aegis loops --help` before use:

| Command | Meaning |
| --- | --- |
| `aegis loops list` | Published discovery; archived logical Loops hidden |
| `aegis loops inventory` | All published revisions, including archived Loops |
| `aegis loops show LOOP REVISION` | Exact immutable revision readback |
| `aegis loops availability LOOP` | Current logical availability and CAS head |
| `aegis loops archive LOOP FILE` | Append an archive event |
| `aegis loops restore LOOP FILE` | Append a restore event |

When the gateway owns state, use `aegis --config OWNER_CONFIG --target CONSOLE_URL`
with those commands. The URL selects the instance; the configured protected Unix
transport authenticates to the owner. Do not stop the gateway, extract credentials,
open another writer or invent a client. A missing installed operation is unavailable,
not permission for direct-store fallback.

## Exact mutation input

`FILE` is strict JSON decoded as `app.SetLoopAvailabilityInput`, not lifecycle
input. For registered-Agent workspace operations supply:

- `agent_id`: the owning registered Agent selector; not authentication.
- `head`: exact current publication reference with `id`, `revision`, `digest`.
  The positional `LOOP` must equal `head.id`.
- `event_id`: stable identity for this one intended transition.
- `sequence`: the next availability event sequence.
- `previous_digest`: current availability digest; empty or omitted only before
  the first event.
- `expected_state`: the observed `published` or `archived` state.
- `state`: target state for HTTP PUT. CLI archive forces `archived`; CLI restore
  forces `published`, regardless of the file's supplied state.

Read inventory and availability first; never invent references or digests. Initial
availability is `published`, sequence zero, without an event digest. Archive and
restore append distinct transitions; a same-state request is not a transition.

Workspace input omits `authority` and `publisher`: the service derives them from
the authenticated principal and exact enabled Agent/stable owner. The online CLI
requires this workspace form. The application input also supports the existing
runtime-authority form (`authority` digest reference and exact `publisher`
revision reference, without `agent_id`); these must come from authenticated
readback, never prompt content. Neither form grants runtime execution authority.

Stable ownership, exact publication head, expected state, sequence and predecessor
digest are checked. Stale or substituted input must not be papered over. Preserve
the exact event identity and payload after an uncertain response; reconcile
readback before any retry rather than rotating the intent.

## HTTP contract and verification

- `GET /v1/loops` is published discovery.
- `GET /v1/loops/inventory` returns all published revision views, including archived.
- `GET /v1/loops/:loop/availability` returns `loop_id`, `state`, `sequence`, and
  optional `digest`.
- `PUT /v1/loops/:loop/availability` accepts the input above with explicit `state`
  and returns `event` plus `idempotent` (created or replay response).

An availability event records the logical ID, observed exact publication `head`,
sequence, expected/target states, predecessor, publisher, stable owner, principal,
authority, occurrence time and digest. Require the returned event to match the
intended operation, then read the exact availability target again. A successful
exit, HTTP status or model report alone is not verified mutation. Authentication,
owner admission and replay checks remain mandatory.

## Console

The Loops Console offers Published and Archived discovery views, with direct
Archive/Restore actions. These reuse authenticated same-origin CSRF-protected
forms and server-derived owning-Agent context; they do not introduce a draft,
new setup journey or runtime prerequisite. Exact historical selection remains
readable regardless of its discovery view. Restore does not imply active lifecycle
or run readiness.

## Evidence and limits

This document describes the workspace source candidate, not a released or installed
acceptance result. The MCP Loop adapter is UNAVAILABLE; CLI, HTTP and Console
coverage must not be represented as MCP integration or whole issue334 closure.
Parent acceptance, broad regression checks, publication and installed/browser
proof remain separate. This documentation-only continuation does not execute
archive/restore against an external instance or regenerate release assets.
