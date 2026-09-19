# Queue request contracts, v1

These complete JSON request shapes are synthetic decoder examples, not runnable
credentials or authenticated facts. Replace every synthetic identity and digest
with exact public readback from your authorized operation. Never hash invented
authority into existence. Example filenames version this recipe; do not add a
request-level schema_version field absent from the public decoder.

## Approved exact Loop execution

Use `aegis loops queue FILE` for the approved bounded task. The controller handles approved-scope session creation/reuse, exact runtime binding and foreground execution in one request. Return exact blockers inline and read back the existing record; do not require a Preparation page, binding file, second worker command, or repeated approval for this action. Foundational authority is never inferred.

## Lower-level binding workspace work

Read `aegis queue show ITEM` first. Require preparation-pending state and exact
workspace owner/Agent provenance. The controller obtains an existing authenticated
runtime authority for that same Agent through the supported session boundary.
Copy bind-runtime.v1.json to an operator-approved request file and replace its
references and operation IDs. Execute `aegis queue bind-runtime FILE` only with
explicit authority for that operation. The Agent selector derives workspace
ownership; it does not grant runtime power. The protected HTTP binding endpoint is `POST /v1/queue/:item/bind-runtime`.
Use the product-owned online adapter while the gateway owns state. Do not stop the daemon, open another writer, or extract API credentials.

The actual CLI response envelope has binding and created fields. This reference
does not pretend a reduced illustrative binding is a full serialized response.
Read `aegis queue show ITEM` after binding and reconcile the exact binding and
queued projection before separately authorizing processing. Binding is not a
claim or execution. Preserve operation IDs if the response is interrupted;
read authoritative history before deciding whether any replay is safe.

## Expired-lease reclaim

Read the exact active claim and its authoritative lease expiry, attempt budget,
current authority and transition head. Ordinary reclaimed:false retry is not
supported: no durable runtime-stop acknowledgment exists. Process absence does
not prove lease release. Use reclaim.v1.json only after expiry and with remaining
budget; backoff is an integer count of nanoseconds, not a duration string. The
example is one second. Valid backoff is nonnegative and at most 24 hours.

Execute `aegis queue retry FILE`; the protected public HTTP route is
POST /v1/queue/:item/retry with matching queue_item_id, using only an available
product-owned authenticated adapter. This reference does not authorize an ad hoc
transport client. The response is the canonical Retry record, not a new attempt.
Read `aegis queue show ITEM` and require the same item and Graph/Loop identities,
cleared active claim, unchanged consumed-attempt count, and exact availability.
A subsequent separately admitted process operation creates the next attempt.

Live lease, exhausted budget, false reclaimed flag, wrong reason, stale authority,
conflicting IDs and terminal history deny. Do not change IDs or weaken guards to
turn denial or unknown outcomes into apparent success.

## Evidence qualification

The bundle's command regression loads these files through the real strict CLI
decoder, checks typed values and unknown-field denial, and confirms commands
reach the application-service boundary. It does not open a store, authenticate,
bind, reclaim, run an agent, or prove a model follows these instructions. Runtime
and real-store acceptance must be reported separately.
