---
name: aegis-evidence-disposition
description: Inspect exact Aegis execution evidence and route authenticated outcome disposition through shipped services without treating process state or model narration as proof.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - evidence
      - verification
      - disposition
      - queue
---

# Aegis evidence and disposition

Use this skill to inspect one authority-bound Execution Queue item's immutable lineage, explain whether exact evidence satisfies its pinned Graph and Loop revisions, and route only shipped authenticated lifecycle operations to Aegis. The model may organize returned records and identify missing or conflicting evidence. It cannot verify bytes, mint receipts, authenticate a reviewer, choose authority, or record a disposition itself.

## Authority and evidence boundary

- Evidence is immutable input to a separate controller-side decision. An artifact, receipt, queue projection, process exit, model response, prompt claim, display identity, mutable tag, or historical `COMPLETED` value grants no authority and is never a gold label.
- Aegis authenticates the caller outside the model and freshly admits exactly one current authority context at claim, runtime effect, evidence verification, and disposition boundaries. Zero or multiple matches deny. Never union trust stanzas.
- The skill never fabricates, edits, repairs, replaces, suppresses, or deletes queue, attempt, artifact, receipt, disposition, audit, Graph, or Loop records. A content digest computed by the model or copied from a prompt is not authoritative evidence.
- Process status and artifact acceptance are separate. Success requires independently reloaded content-addressed output and independently reloaded receipts matching every exact evidence claim in the pinned immutable Loop revision. A Graph or Loop definition changed later cannot rewrite the run snapshot.
- Discussion, inspection, explanation, or a requested outcome is not authorization to process, retry, cancel, expire, exhaust, revoke, or reevaluate work.

## Confirm the shipped surface

First run `aegis queue --help`. The compatible CLI exposes:

- `aegis queue list`
- `aegis queue show ITEM`
- `aegis queue process FILE`
- `aegis queue retry FILE`
- `aegis queue cancel FILE`
- `aegis queue expire FILE`
- `aegis queue exhaust FILE`
- `aegis queue revoke FILE`

Protected HTTP read surfaces expose `GET /v1/queue` and `GET /v1/queue/:item`. The current CLI has no separate `evidence`, `receipt`, `disposition`, `accept`, `reject`, `needs-review`, or `reevaluate` command. The shipped store permits one terminal disposition per Graph run; append-only reviewer reevaluation history and a reviewer-authored needs-review disposition are unavailable in this compatibility range. Report those operations as unavailable instead of fabricating a mutation, overwriting the terminal record, or presenting a fixture as live proof.

`aegis queue process FILE` is the shipped controller operation that claims eligible narrow work, invokes the registered runtime adapter, stores content-addressed output, verifies precommitted claims, and records the resulting terminal disposition. It is consequential execution, not a read-only evidence-review command.

## Gather one reconstructable execution view

1. Read `aegis queue show ITEM`. If the item ID is not exact, use authenticated `aegis queue list` only to find an immutable queue identity; never select by a mutable label or model guess.
2. Preserve the accepted submission and every immutable reference returned by queue readback: submission and idempotency identities, snapshot reference, runtime, mandate and authority references, maximum attempts, and causal Graph-run identity. The current public queue view does not return the Graph-run snapshot body or its normalized typed inputs. Do not claim those fields were reconstructed from `queue show`; report full snapshot inspection as unavailable on this surface.
3. Preserve queue state as a projection, then gather every transition, claim and lease, retry, cancellation, Loop execution, and Attempt in returned order. Match queue item digest, Graph run, Loop execution, attempt number, claim, and authority context throughout.
4. For each terminal attempt, gather the typed runtime artifact, its artifact and content digests, media type, producer Agent ID, action, run, attempt, authority context, and creation time. `content_ref` must equal the artifact digest. The artifact `owner_id` is the producing participant Agent ID; it is not registry ownership, reviewer identity, or authority.
5. Gather every verification receipt: receipt and artifact identities, attempt, action, run, owner, authority context, verifier, policy version, exact claim, media type, expected and observed digests, outcome, failure category, content-addressed evidence reference, and observation time.
6. Gather the terminal disposition: immutable disposition ID and digest, exact Graph run, queue digest reference, authority digest reference, terminal state, stable reason code, artifact and receipt IDs, and occurrence time. Require Loop execution and Attempt bindings for attempt-backed dispositions. A valid pre-claim lifecycle disposition can omit both.
7. Treat a missing required record, failed strict decode, unknown field, digest mismatch, duplicate causal identity, stale authority, cross-attempt or cross-run binding, definition substitution, unreadable blob, incomplete receipt set, or inconsistent transition as a denial of verified success. Do not fill gaps from narration.

## Verify exact claims without reimplementing Aegis

Use returned Aegis evidence to explain the authoritative result; do not independently promote it to a new fact.

1. Start from the exact Graph-run snapshot reference, not current or latest definitions. The controller internally reloads the snapshot and exact definitions during consequential admission. For public inspection, use only exact revision references actually returned by authenticated surfaces, such as Loop references in Loop executions, with `aegis loops show LOOP REVISION` and `aegis graphs show GRAPH REVISION`. Because `queue show` does not expose the snapshot body, do not claim complete public reconstruction of its normalized inputs or Graph reference when those identifiers are unavailable. Definition retirement or a newer revision does not invalidate intact historical evidence, and current definitions cannot substitute for pinned ones.
2. Require the artifact to validate and bind the same Attempt, Loop execution action, producer participant Agent ID, authority context, and run. Require `content_ref == digest`.
3. Aegis's verifier must freshly reload the blob addressed by `content_ref`, recompute its SHA-256 content reference, and compare it with both artifact digest and the immutable claim's expected digest. Runtime-returned bytes, a filename, projection, or caller-provided copy are insufficient.
4. Match required evidence by exact claim, producer action, verifier ID, policy version, media type, and expected digest from the pinned Loop. Require exactly one valid receipt for every required claim; reject missing, duplicate, additional-substitution, stale, mismatched, malformed, or unverifiable receipts.
5. Each receipt must independently reload from `evidence_ref`, match its canonical content address, and bind the same artifact, Attempt, action, run, owner, and authority context. A passing receipt requires `observed_digest == expected_digest == artifact.digest` and no failure category.
6. Final completion authorization must independently reload the artifact and every receipt again. Only then may the controller atomically persist a succeeded disposition. Store enforcement rechecks exact required evidence against the historical Loop revision.
7. Require exact post-write `aegis queue show ITEM` readback. Match disposition, transition, artifact, receipts, Attempt and pinned definitions. A zero exit from `queue process` alone is not completion evidence.

Graph revisions currently contribute exact historical Agent/Loop bindings and typed run inputs. Exact executable output claims are declared by the pinned Loop evidence contract. Do not invent Graph-level output-verifier claims that the shipped Graph schema does not contain.

## Explain distinct outcomes

Never collapse these classes:

- `succeeded`: every exact required claim passed after fresh artifact and receipt reload; shipped worker reason is `evidence_satisfied`.
- `failed verification`: a valid persisted failed receipt records that a required policy or exact claim did not pass; preserve `evidence_policy_missing` or `evidence_verification_failed`. Corrupt or unreadable content-addressed artifacts or receipts can prevent authoritative completion from being committed and must remain an uncommitted recovery/needs-review condition rather than an invented failed-verification disposition.
- `failed execution`: runtime effect or output/persistence failed; preserve its exact reason rather than calling it rejected evidence.
- `denied`: fresh readiness, authority, runtime binding, evidence admission, or disposition admission denied. A denial is not a failed verifier result.
- `cancelled`, `expired`, and `revoked`: authenticated queue lifecycle outcomes with distinct durable transitions and reasons. Retry exhaustion is persisted as terminal state `failed` with reason `retry_exhausted`; preserve that state/reason pair rather than inventing an `exhausted` state. None is successful or failed verification.
- submission `rejected`: durable admission rejection before queue execution. It is not a terminal execution disposition.
- `needs review`: an operator-facing diagnostic when evidence is absent, inconsistent, corrupt, unavailable, or an unsupported reevaluation is requested. It is not a shipped execution state or persisted disposition.
- artifact `accepted` or `rejected` by a human reviewer and append-only `reevaluation`: unavailable as reviewer-authored disposition operations in the supported release. Do not map them onto process success/failure or overwrite history.

## Route consequential operations

Only an explicitly authorized, authenticated request for the exact input may be routed:

- `aegis queue process FILE` starts one eligible attempt. The strict file binds current authority, exact queue item, worker, Loop execution, claim, Attempt, transition, disposition and artifact identities, plus a bounded lease.
- `aegis queue retry FILE` appends a bounded retry decision for an eligible retryable item. It does not rewrite an Attempt or terminal disposition and cannot exceed the immutable attempt bound.
- `aegis queue cancel FILE`, `expire FILE`, `exhaust FILE`, and `revoke FILE` route distinct authenticated lifecycle decisions. The command returns the cancellation record. Then use exact authenticated queue and audit readback to preserve the separately persisted transition, disposition, authority, and audit evidence; do not imply those records were returned by the mutation response.

Never invoke a lifecycle mutation merely to make evidence agree with a desired label. If the requested operation is reviewer acceptance, rejection, needs-review persistence, or reevaluation, state that it is unavailable and preserve the existing record.

## Interruption, replay, and recovery

After interruption, read the exact queue item before any action. If an exact attempt, transition, artifact, receipt, disposition, retry, or cancellation already exists, report that durable state and do not duplicate it. If the item remains claimed, evidence is incomplete, or stores are unavailable or inconsistent, report needs review without inventing completion. Changed request data under a reused identity or idempotency key is a conflict, not recovery.

A stale artifact or receipt from another attempt, run, authority context, definition revision, action, owner, verifier policy, or media type must be rejected even if its bytes or digest look plausible. Never repair replay by editing causal identifiers.

## Secret and progressive-disclosure handling

Consult `references/evidence-fixtures.json` only for non-secret structural examples. Fixtures are evaluation data, not live records, authority, reviewer identity, or proof. Never include authentication material, credential values, broker capabilities, private prompts, raw runtime output, host paths, runtime homes, or secret-shaped canaries in fixtures, reports, logs, commands, or model context.

Consult `specs/CANONICAL_DOMAINS.md`, `specs/MVP.md`, `specs/STORAGE.md`, `internal/evidence/model.go`, `internal/evidence/verifier.go`, `internal/disposition/model.go`, `internal/orchestration/queue_worker.go`, `internal/persistence/fleet/badger/completion.go`, the root `README.md`, and installed command help for normative and shipped behavior. Installing this advisory skill grants no identity, authority, queue claim, runtime capability, reviewer power, evidence custody, disposition right, filesystem/network access, or completion evidence.
