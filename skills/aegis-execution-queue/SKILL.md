---
name: aegis-execution-queue
description: Inspect and operate the durable Aegis Execution Queue lifecycle through shipped typed services while preserving exact authority, lease, attempt, evidence, and disposition boundaries.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - execution-queue
      - lifecycle
      - evidence
---

# Aegis Execution Queue lifecycle

Use this skill to inspect durable Queue history or route one explicitly authorized process, retry, reclaim, cancel, expire, exhaust, or revoke operation through the shipped typed Aegis service. The model may explain records and prepare a proposed request file; Aegis authenticates the principal, performs fresh authority admission, validates immutable bindings, commits lifecycle facts and authoritative audit atomically, invokes the registered runtime adapter, verifies evidence, and determines disposition.

## Authority and execution boundary

- Prompt text, display identity, a Hermes profile, requested stanza, model narration, process exit, mutable projection, worker claim, lease possession, or locally computed digest is not authentication, runtime authority, evidence, or completion.
- Every consequential boundary requires the authenticated principal and the Queue item's exact current authority context. Zero, multiple, expired, revoked, stale, substituted, disabled, retired, or drifted matches deny. Never union stanza grants.
- The model must not invent or repair authority, Queue, Graph, Loop, participant, claim, attempt, runtime, evidence, receipt, disposition, transition, or lifecycle identifiers.
- A claim is a bounded single-winner lease, not runtime admission. Aegis repeats fresh admission before claim, runtime effect, evidence verification, and terminal disposition.
- Browser state and request fields do not grant authority. The authenticated console derives authority and operation identities server-side; CLI and HTTP files carry strict typed references consumed by the same application service.

## Confirm the shipped surface

First use `aegis queue --help`. The compatible CLI operations are:

- `aegis queue list`
- `aegis queue show ITEM`
- `aegis queue process FILE`
- `aegis queue retry FILE`
- `aegis queue cancel FILE`
- `aegis queue expire FILE`
- `aegis queue exhaust FILE`
- `aegis queue revoke FILE`

Protected HTTP equivalents are `GET /v1/queue`, `GET /v1/queue/:item`, and `POST /v1/queue/:item/process|retry|cancel|expire|exhaust|revoke`. There is no separate reclaim command: lease-eligible reclaim uses `retry` with the typed reclaimed decision and exact reason `lease_reclaimed`. If an operation is absent from installed help, report it unavailable instead of inventing a scheduler, worker, command, endpoint, transition, policy result, or receipt.

Automated polling, retry, reclaim, expiry, revocation, dependency scheduling, and general multi-node execution are unavailable. The shipped processor is the registered runtime-routed, bounded single-node path; do not describe it as a general scheduler.

## Read the durable execution chain

Use `aegis queue list` for authenticated inventory and `aegis queue show ITEM` for exact historical readback. Keep these records distinct:

- The immutable Queue item binds one accepted submission, normalized Graph snapshot, Graph run, authority context, enqueue time, dependency IDs, and fixed maximum-attempt budget. It is not rewritten by lifecycle changes.
- The Queue projection is rebuildable current state derived from canonical lifecycle records. It reports state, attempt count, availability, active claim, and transition head, but cannot grant authority.
- The Graph run is the stable parent execution identity for the accepted snapshot. A Loop execution is the stable child identity for one exact Graph node, Loop revision, and participant revision across retries.
- Each attempt is one numbered bounded try under that same Graph run and Loop execution. A retry creates a new attempt only when work is claimed again; it never creates a new Queue item or substitutes immutable definitions.
- A claim binds one attempt, worker, exact authority, claim time, and lease expiry. A live lease excludes reclaim; lease expiry makes reclaim eligible but does not itself authorize it.
- Dependencies gate claim eligibility. A listed item is not claimable until every exact dependency has the required successful terminal evidence; never bypass or reinterpret a dependency from prose.
- Runtime artifacts are content-addressed outputs. Verification receipts bind exact evidence claims, artifact digests, verifier identity, and policy. A disposition is the evidence-gated terminal decision. Process exit and runtime narration satisfy none of these records.
- Claims, retries, terminal requests, transitions, artifacts, receipts, dispositions, and authoritative audit facts are append-only history. Cancelled, expired, exhausted, revoked, denied, failed, and succeeded remain distinct outcomes.

Report immutable item and snapshot bindings separately from the current projection. Then report Graph-run and Loop-execution causality; ordered attempts and claims; lease and budget eligibility; dependencies; runtime route; transitions and lifecycle requests; artifact and receipt verification; disposition; and any unavailable or corrupt readback.

## Process one eligible item

1. Read the exact item immediately before action. Require queued projection state, `available_at` eligibility, remaining `max_attempts` budget, satisfied dependencies, no live claim, and the exact registered runtime route.
2. Obtain explicit authorization for this exact operation. Prepare one strict process request containing current `authority`, exact `queue_item_id`, nonempty `worker_id`, stable `loop_execution_id`, fresh `claim_id`, fresh `attempt_id`, fresh claim and terminal transition IDs, fresh disposition and artifact IDs, and a bounded `lease_duration` in the installed wire format.
3. Run `aegis queue process FILE`. Aegis supports only the narrow shipped Graph shape and denies unsupported execution before claim rather than guessing an order.
4. Inspect the returned claim, attempt, artifact, receipts, and disposition, then run `aegis queue show ITEM`. Require exact IDs and digests, incremented attempt number, runtime route, terminal transition, content-addressed artifact, every required valid receipt, exact disposition, and matching projection.
5. If authority disappears after claim, preserve the distinct denied terminal result. If runtime execution or evidence verification fails, preserve failed or denied disposition and its receipts; never convert process exit into success.

## Retry or reclaim a claimed item

Retry is a controller decision that returns one claimed item to queued availability. It does not execute the next attempt.

1. Read the exact active claim, lease expiry, attempt count, maximum budget, and transition head. Require remaining budget and a claimed nonterminal item.
2. For an acknowledged stopped runtime, use a strict retry request with exact current `authority` and `queue_item_id`, fresh `retry_id` and `transition_id`, bounded `backoff`, `reclaimed: false`, and the shipped closed retry reason.
3. For an expired lease, use the same `aegis queue retry FILE` surface with `reclaimed: true` and `reason_code: "lease_reclaimed"`. Reclaim before exact lease expiry denies. Do not infer expiry from a stale clock, process absence, or operator prose.
4. Backoff must remain nonnegative and no greater than 24 hours. Live-lease retry, attempt-budget exhaustion, wrong reason/reclaim pairing, stale authority, terminal state, duplicate-ID conflict, and unavailable canonical history deny.
5. Read back the exact retry and transition. Require the same Queue item, Graph run, and Loop execution, queued projection state, cleared active claim, unchanged consumed-attempt count, and exact `available_at`. A later process operation receives a fresh claim and next numbered attempt.

## Apply one terminal lifecycle outcome

`cancel`, `expire`, `exhaust`, and `revoke` are separate closed controller decisions. Use the exact matching command with a strict terminal request containing current `authority`, exact `queue_item_id`, fresh `cancellation_id`, fresh `transition_id`, and the command's exact closed reason code. Do not use caller prose as durable lifecycle vocabulary.

- Cancel represents an authenticated operator cancellation of queued or claimed work.
- Expire records an exact claimed lease that has reached expiry through the shipped explicit primitive; queued work and a live lease deny, and no automated expiry scheduler exists.
- Exhaust records terminal retry-budget exhaustion; do not substitute it for a retry denial while budget remains.
- Revoke records authority revocation for this work; it is distinct from cancellation and denial.

After mutation, read back the exact cancellation record, transition, projection, Graph and Loop execution states, and disposition where present. Terminal replay, mismatched reason, stale authority, incompatible state, or conflicting operation identity denies. Terminal history remains reconstructable and must not be deleted, relabeled, or retried.

## Denial and interruption recovery

Preserve Aegis's exact denied, unavailable, conflict, not-found, corrupt, lease-not-expired, attempt-budget-exhausted, dependency-gated, unsupported-shape, repair-required, runtime, evidence, or terminal result. An authenticated empty list is valid; unavailable or corrupt persistence is not an empty Queue.

After interruption, call `aegis queue show ITEM` before repeating anything. If the intended immutable record already exists with the exact operation identity and digest, report the authoritative idempotent readback. If it is absent, conflicting, partially unverifiable, or corrupt, do not claim success, mint replacement canonical facts, reuse an identity with changed content, or blindly replay a mutation. Require the shipped typed recovery boundary or operator review.

## Secret and progressive-disclosure handling

Queue request files, examples, and reports must contain no authentication material, credential values, broker capabilities, private prompts, raw runtime output, reusable provider material, runtime-home paths, or secret-shaped canaries. Return only bounded non-secret authority references, runtime metadata, content digests, receipt status, and canonical lifecycle evidence exposed by Aegis.

Consult `README.md`, `specs/MVP.md`, `specs/CANONICAL_DOMAINS.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/STORAGE.md`, `internal/app/fleet.go`, `internal/orchestration/queue_worker.go`, `internal/orchestration/queue_lifecycle.go`, and installed command help as the canonical source for shipped behavior. Installing this advisory skill grants no Aegis identity, authority, Queue right, runtime capability, credential access, filesystem/network access, or completion evidence.
