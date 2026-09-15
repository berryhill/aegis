# Protected terminal creation contract v1

This reference travels with the installed skill. It describes the product-owned
`aegis.credential-intake.v1` protocol, not a generic HTTP client or permission to
handle values. The ordinary external Hermes agent must not implement this client,
read Aegis authentication configuration, or collect any credential bytes itself.
Use the authenticated Linux `aegis` / `aegis manager` terminal that owns the
protected interaction. A normal registered-Agent workspace has zero credential
rights.

## Supported operator workflow

Ask `can we add a secret for a test?` without a value. On a compatible gateway
and terminal the deterministic Aegis route bypasses the model and offers a
metadata-only handoff. Enter a non-secret reference and kind (default `opaque`).
Review the exact metadata and authenticated principal. Type exactly `yes` at the
fresh approval prompt. Earlier pasted type-ahead is discarded. Only then enter
and separately confirm the value in Aegis's no-echo prompt. For multiline values
use terminal-framed bracketed paste, never an unframed multiline paste. The limit
is 1 MiB. The skill/model must never receive the value.

Both input and review output must be terminals; supported cancellation-safe
intake is Linux-specific. Unsupported clients receive guidance only. Never
substitute a pipe, handwritten transport, model turn, or second state writer to
make this flow appear available. Browser credential forms are a separate
reviewed interface and cannot opt into this terminal protocol.

On success Aegis returns metadata, verifies the canonical encrypted record and
returns to the same conversation. Creation does not grant a binding, broker
capability, runtime authority or Agent workspace credential rights.

## Metadata wire contract (for compatibility review, not agent execution)

The dedicated product client uses these registered routes:

- `POST /v1/manager/sessions/:session/credential-intake` — one metadata object.
- `POST /v1/manager/sessions/:session/credential-intake/:operation/value` —
  separate protected bytes, never JSON and never a turn payload.

The transport principal and manager-session authentication are independently
required. `X-Aegis-Protected-Intake: aegis.credential-intake.v1` advertises protocol
compatibility, not authentication or proof of terminal ownership. Requests with
`Origin` or `Sec-Fetch-Site` deny. Authentication values are product-custodied;
none belongs in a skill request, shell argument, transcript or example.

Metadata is `application/json`, at most 4096 bytes, with exactly the permitted
string fields `operation_id`, `action`, optional `reference` and optional `kind`.
`operation_id` and `action` must be present. Every field value is at most 255
bytes. Duplicate, case-folded, unknown, null, non-string and trailing fields
deny. `protected-intake.v1.json` contains complete synthetic metadata examples;
its operation identity is deliberately not live and must never be submitted.

The server-generated handoff contains protocol, operation_id, session_id,
principal_id, expires_at and stage; reference/kind appear after review. Neither
request nor skill supplies identity, expiry, stage or authority. State transitions:

1. Initiating the supported phrase produces `metadata` and `created:false`.
   Repeating the initiating phrase before expiry reuses the outstanding operation.
2. `review` with valid non-secret reference/kind freezes them and enters `review`.
3. `approve` with only operation_id/action enters `intake`. Including reference
   or kind here denies; changed metadata requires a new reviewed operation.
4. `cancel` with only operation_id/action clears the pending operation and returns
   `credential_creation_cancelled`, `created:false`.
5. The protected value endpoint requires `application/octet-stream`, nonempty
   bytes no larger than 1 MiB, current `intake` stage and exact session/operation.
   The operation is consumed before canonical creation. It is not replayable.

Responses are actual `TurnResult` envelopes: kind, origin, message, optional
intake and optional data. A successful creation is `kind:credential_created`,
`origin:aegis_authoritative`, with data containing created, record_id, reference,
kind, operation_id and model_bypassed. The identifiers and timestamps are
server-derived; reduced interpretation fixtures are not literal wire receipts.

## Denial, expiry and uncertain outcomes

Ctrl-C/Ctrl-D outside framed paste cancels. Decline, empty input and mismatched
confirmation do not create a record. Unsynchronized, incomplete or oversized
protected input stops the conversation to keep late bytes out of chat. Aegis
restores terminal modes; it cannot protect against external terminal recording,
same-account/root compromise or guarantee heap erasure.

An operation lives at most five minutes and no longer than the authenticated
session. Session closure/revocation or gateway restart invalidates pending
operations. Wrong session/operation, expiry, wrong stage, replay and unavailable
credential authority deny. These are not permission to widen authority.

A `credential_intake_not_confirmed` response or interrupted post-submission
transport is an UNKNOWN mutation outcome, not evidence of no write. Do not replay
the value or create a second operation immediately. Use the authenticated terminal
request `what secrets do I have?` and authoritative metadata/audit readback first.
Malformed requests rejected before consumption do not prove that all failures
consume an operation. Preserve these distinctions.

## Scope and evidence

This delivered protocol supports CREATE and pending-operation CANCEL, not online
ROTATE, REVOKE, BIND or BACKUP. The `aegis secret` CLI operations remain separate
principal-controlled direct-service paths; when the gateway owns stores their
`control_plane_online` denial must not be bypassed. The #248 general typed
conversational platform work is not implemented by this credential protocol.
No HTTP Queue bind-runtime adapter is implied.

Source contracts: internal/managergateway/credential_intake.go,
internal/api/manager_credential_intake.go and
internal/command/manager_credential_intake.go, delivered by #245 / #254.
Regression sources: internal/api/manager_credential_intake_test.go,
internal/managergateway/credential_intake_test.go,
internal/command/manager_gateway_intake_linux_test.go and
internal/command/manager_installed_intake_linux_test.go. These references identify
checks, not a claim they ran on the installed consumer. PTY/real-store proof,
actual Hermes skill behavior, inventory and publication must be reported
separately. No source checkout is needed to follow this operator workflow.
