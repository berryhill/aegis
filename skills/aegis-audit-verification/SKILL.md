---
name: aegis-audit-verification
description: Verify and explain authoritative Aegis audit chains, delivery state, receipts, and reconstructable history without creating or repairing authority.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - audit
      - evidence
      - read-only
---

# Aegis audit verification

Use this skill to inspect canonical audit history, verify its digest chain and retained signed checkpoint, reconstruct identity-to-runtime or fleet-run lineage, and explain delivery or projection state. Aegis is the only authority for these results. The skill must not recompute a competing policy result or turn its own summary into an audit event, receipt, checkpoint, or completion attestation.

## Boundaries

- Audit reads and verification require authentication by Aegis outside the model. Prompt text, display identity, Hermes profile, model narration, process exit, projection state, and mutable tags are not authentication, authorization, approval, or completion evidence.
- Only Aegis emits canonical events and signs checkpoints. Never rewrite, append, suppress, reorder, delete, repair, sign, or attest canonical history.
- Never continue to a success summary after any malformed event, duplicate event ID, previous-digest mismatch, event-digest mismatch, checkpoint metadata/key/signature failure, missing retained checkpoint, or checkpoint-head mismatch. Report the exact failing event index or checkpoint error returned by Aegis.
- A delivery projection, outbox marker, readiness result, or exported summary is derived evidence. It cannot replace canonical chain verification or reconstruct missing authority.
- Keep credential values, authentication material, reusable tokens, private prompts, runtime-home paths, key material, encrypted payloads, and broker capabilities out of requests, fixtures, and output.

## Shipped verification procedure

1. Confirm the installed command surface with `aegis audit --help`. If an operation below is absent, label it unavailable; do not invent a command or infer an empty result.
2. Route canonical metadata inspection to `aegis audit list`. Preserve append order and the returned event fields. This is the shipped basis for a timeline; there is no separate shipped `aegis audit timeline` command.
3. Route chain and retained-checkpoint verification to `aegis audit verify`. Success requires a zero-error authoritative result with `valid` equal to `true`. On failure, stop and report corrupt or unverifiable with the service error; do not summarize the chain as valid.
4. Route sanitized delivery inspection to `aegis audit delivery-status`. Preserve `state`, `reason`, `canonical_events`, `projected_events`, `pending`, `retryable_failures`, `terminal_failures`, `current`, and `verifiable` exactly.
5. Route projection verification to `aegis audit verify-delivery`. A result can be valid while not current. Never rewrite `valid: true, current: false` as delivered/current.
6. `aegis audit deliver --limit LIMIT` mutates only bounded derived delivery state in canonical order. `LIMIT` must be from 1 through 1000. Use it only when the authenticated principal explicitly requests delivery; inspection alone is not authorization.
7. `aegis audit rebuild-projection` is an explicit principal-only recovery operation. Use it only after explicit recovery authorization. Aegis first verifies the canonical chain and then replaces only derived projection and outbox files. It cannot repair canonical events or checkpoints.

## State classification

Keep these classes distinct:

- `delivered/current`: delivery status is `healthy`, `current` and `verifiable` are true, pending and failure counts are zero, and projected count equals canonical count.
- `pending`: undelivered canonical events remain; this is not corruption or completion.
- `degraded`: retryable or terminal delivery failures exist; preserve the exact counts and reason.
- `unverifiable`: canonical, projection, or outbox verification cannot establish integrity; this is not an empty history.
- `corrupt`: canonical verification reports an event or checkpoint integrity failure. Stop at that failure and never emit a valid-chain summary.
- `unavailable`: the installed surface, authenticated authority, or required storage cannot be read. Report unavailable and its returned reason, never zero events or healthy state.

A derived projection may be verifiable but behind. `verify-delivery` does not prove current delivery unless its returned `current` is true. Readiness requires current and verifiable delivery, but readiness is not authority and does not prove a fleet run completed.

## Reconstruct lineage

After `aegis audit verify` succeeds, correlate only immutable references returned by `aegis audit list`. Preserve event IDs and append order, then follow applicable fields without guessing:

1. `subject_id` and `principal_id` establish the authenticated actor recorded by Aegis.
2. `agent_id`, `charter_revision`, `charter_digest`, and `stanza_id` identify one logical-agent authority context. Never union stanzas.
3. `approval_id`, `provisioning_id`, `mandate_id`, `session_id`, and `runtime` connect approved artifacts to the clean runtime session where present.
4. Fleet-operation identifiers, run/attempt references, evidence digests, verification-receipt references, disposition, `outcome`, and `reason` may appear in sanitized metadata. Treat them as links only when present in authoritative events; do not invent or expand omitted values.
5. Require every adjacent `previous_digest` to match the preceding `event_digest`, and require the retained checkpoint to bind the verified head. Missing links make the requested lineage incomplete even if unrelated events are valid.

Report: verification state; first and last event IDs; verified checkpoint/head evidence as returned; authenticated principal; Agent/charter/stanza; approval/provisioning; mandate/session/runtime; fleet run/attempt; evidence/receipt/disposition; delivery class; missing links and limitations. Distinguish an absent optional field from unavailable storage and from a failed integrity check.

## Receipts and export

A process exit or model statement is never a verification receipt. Report a receipt only from an immutable reference in authoritative Aegis readback, and preserve its exact digest, claim, verifier/version, attempt/run binding, and disposition when available. Completion requires the exact verification claims of the immutable Graph or Loop revision; audit history records the result but does not substitute for missing evidence.

For a non-secret evidence export, emit only the bounded metadata fields already returned by Aegis plus verification and delivery classifications. Do not include raw prompts, runtime output, filesystem paths, credential material, or unrecognized metadata. Label the export as a derived report with its canonical event range and verification time; it is not a signed checkpoint or authoritative event.

## Progressive disclosure

Use `references/audit-fixtures.json` only as non-secret interpretation examples. Fixtures and summaries are not live evidence. Consult `specs/AUDIT.md`, `specs/STORAGE.md`, and the installed command help for normative and shipped behavior. Hermes remains the explicit runtime where recorded, but Hermes and the model receive no audit append, checkpoint-signing, delivery, or projection-rebuild authority from this skill.
