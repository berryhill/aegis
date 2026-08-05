# Audit Specification

Aegis—not Hermes or the model—emits authoritative audit events.

## Event coverage

Events cover authentication success/failure, design creation/outcome, charter validation/import, authorization selection/denial, approval decisions, provisioning/recovery, mandate issuance, session start, expiry, revocation, termination, and failure.

Events use stable IDs and machine-readable reason codes and include applicable subject, principal, agent, stanza, mandate, session, runtime, charter revision/digest, approval, and provisioning identifiers. Credential values, API tokens, full private prompts, and runtime-home paths are excluded.

## Integrity

The local store serializes append operations across processes. Events form a digest chain. Ed25519-signed checkpoints bind the retained head and key identifier so verification detects modification, deletion, insertion, reordering, truncation, and replacement relative to the checkpoint.

Audit listing and verification require authenticated principal authority. Application services depend on a narrow append/read/verify authority interface that is never passed to Hermes.

## Delivery and derived projection

Each canonical event has one durable metadata-only outbox entry bound to its event ID and digest. Delivery is bounded, ordered by the canonical chain, and idempotent across interruption: projection publication precedes the delivered marker, and restart reconciles an already projected prefix without duplicating it. Status distinguishes healthy/current, pending, retryable failure, terminal/degraded failure, and unverifiable derived state using sanitized aggregate counts and stable reason codes.

The projection and outbox are derived local state, not a second source of audit authority or an external transparency service. Their verification first verifies the canonical chain, then requires the projection and outbox to be exact canonical prefixes. An explicit principal-only rebuild verifies the canonical chain before replacing only those derived files; it MUST NOT rewrite canonical events or checkpoints. Service readiness fails closed unless delivery is current and verifiable. Hermes and deployed runtimes receive no delivery, rebuild, or audit-authority capability.

## Deployment boundary

The default implementation is in-process and same-account local storage; it is not an external transparency service. Stronger append separation requires deploying the audit authority behind a separately supervised process or OS account and retaining checkpoints on independently protected storage.
