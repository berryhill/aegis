# Audit Specification

Aegis—not Hermes or the model—emits authoritative audit events.

## Event coverage

Events cover authentication success/failure, design creation/outcome, charter validation/import, authorization selection/denial, approval decisions, provisioning/recovery, mandate issuance, session start, expiry, revocation, termination, and failure.

Events use stable IDs and machine-readable reason codes and include applicable subject, principal, agent, stanza, mandate, session, runtime, charter revision/digest, approval, and provisioning identifiers. Credential values, API tokens, full private prompts, and runtime-home paths are excluded.

## Integrity

The local store serializes append operations across processes. Events form a digest chain. Ed25519-signed checkpoints bind the retained head and key identifier so verification detects modification, deletion, insertion, reordering, truncation, and replacement relative to the checkpoint.

Audit listing and verification require authenticated principal authority. Application services depend on a narrow append/read/verify authority interface that is never passed to Hermes.

## Deployment boundary

The default implementation is in-process and same-account local storage; it is not an external transparency service. Stronger append separation requires deploying the audit authority behind a separately supervised process or OS account and retaining checkpoints on independently protected storage.
