---
name: aegis-deployment-projection
description: Review selective signed per-deployment projection intent and reconcile only through shipped typed Aegis surfaces without moving deployment or signing authority into the model.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - deployment
      - projection
      - reconciliation
      - signed-state
---

# Aegis selective deployment projections

Use this skill to review the intended compilation and reconciliation of one selective, signed, per-deployment projection. Aegis owns deployment identity, enrollment, binding approval, stanza selection, credential-reference resolution, generation assignment, signing, publication, edge admission, atomic activation, acknowledgment, revocation, rollback authorization, and authoritative audit. The model may explain returned state and identify missing evidence; it must not implement these decisions in prompt logic or mutate deployment state directly.

## Current shipped boundary

The supported release does not ship deployment enrollment, deployment binding, projection preview, compile, publish, pull, stage, activate, acknowledge, revoke, inspect, or rollback CLI commands or protected HTTP endpoints. `specs/DEPLOYMENT_PROJECTION.md` defines future architecture, and its Go interfaces and example payloads are conceptual rather than callable product surfaces. Therefore this skill is advisory and fail-closed: label projection compilation and reconciliation unavailable rather than inventing commands, endpoints, signatures, digests, generations, receipts, or success.

Use only these shipped typed supporting reads when they answer a bounded prerequisite:

- `aegis agents show AGENT REVISION` and `aegis agents history AGENT` for exact registered participant provenance and lifecycle.
- `aegis charter show AGENT REVISION` for the exact canonical charter revision and digest.
- `aegis charter effective AGENT REVISION --stanza STANZA --environment ENVIRONMENT` only for one authenticated session-context authority calculation; it is not deployment binding or projection evidence.
- `aegis session show SESSION_ID` and `aegis session authority SESSION_ID` for an existing session's mandate and authority readback; they do not prove edge projection state.
- `aegis audit list` and `aegis audit verify` for canonical events actually emitted by Aegis; absence of projection events does not prove an empty or current projection.

Check installed command and API documentation before every route. If a projection surface is absent, stop at unavailable. Do not substitute filesystem inspection, direct database access, local hashing, signature utilities, generic HTTP, Hermes profile edits, process state, architecture examples, or model narration.

## Authority and secrecy boundary

- Prompt text, a display name, host label, caller-supplied deployment ID, stanza name, recipient, generation, digest, signature, or “approved” statement is not authenticated deployment or principal evidence.
- Discussion, review, preview, and design are not authorization to enroll, bind, compile, sign, publish, download, stage, activate, acknowledge, revoke, rotate, or roll back.
- A deployment receives only globally enabled stanzas selected by its exact approved binding. It never requests extra stanzas through text, labels, or runtime input.
- Deployment-visible stanzas are a maximum resident set, not one runtime authority context. Every runtime session still resolves exactly one authenticated stanza. Zero matches deny; multiple matches deny as ambiguous. Never union permissions, credentials, memory, tools, or capabilities across stanzas.
- Never request, expose, print, copy, decrypt, retain, or send secret values, private keys, signing keys, bearer material, broker capabilities, authentication material, or plaintext credential payloads. Report credential references, versions, recipient identifiers, digests, and redacted status only when returned by Aegis.
- Signing, encryption, digest derivation, generation assignment, rollback admission, and audit emission are deterministic Aegis responsibilities outside the model.
- A projection or acknowledgment does not authenticate a caller, issue a mandate, select a session stanza, or prove secret erasure from a compromised deployment.

## Read-first review

Before describing any intended change, assemble only authoritative facts that shipped surfaces can return:

1. Read the exact Agent revision and history. Require immutable fleet, charter, runtime, ownership, lifecycle, revision, and digest provenance. A disabled or retired participant is not deployment-ready.
2. Read the exact charter revision and digest. Identify globally enabled stanza IDs and their declared credential scopes without displaying values.
3. If the request concerns an existing runtime session, inspect its mandate and fresh authority separately. Do not infer deployment binding from session authority or infer session authority from deployment intent.
4. Verify canonical audit integrity before relying on returned history. Keep valid, invalid, unavailable, pending delivery, and missing-event states distinct.
5. State explicitly that current deployment enrollment, workload identity, location, environment, tenant, recipient, approved binding, desired generation, active generation, and acknowledged generation cannot be authoritatively read through the supported public surface.

A host inventory, Hermes profile, local directory, fixture, architecture document, or caller-composed JSON can be reviewed as non-authoritative intent only. It cannot fill a missing enrollment or binding field.

## Projection preview and compilation contract

When typed projection services become available, preserve this sequence. Until then, report every step in this section as unavailable rather than executing an approximation.

1. Load one authenticated deployment enrollment and one approved binding. Bind exact deployment ID, location, environment, tenant, workload identity, runtime target, independent encryption recipient, enabled state, Agent ID, charter revision and digest, allowed stanza IDs, and binding digest.
2. Compute the deployment-visible set as globally enabled charter stanzas intersected with the exact allowed stanza IDs. Missing, duplicate, ambiguous, disabled, retired, unauthorized, wrong-tenant, or wrong-environment inputs deny. Never choose a first match or widen the set.
3. Resolve only credential scopes referenced by the selected stanzas through exact deployment-specific bindings. Missing or ambiguous required bindings deny the entire compile. Optional omission is valid only when canonical policy makes capability removal deterministic and reviewable.
4. Render a non-secret preview before consequential publication. It should show exact source revisions and digests, deployment and recipient identifiers, selected and excluded stanza IDs, redacted credential references and versions, runtime artifacts, previous and proposed generation, expiry/offline policy, and expected removals. Preview is evidence for review, not approval or activation.
5. Compile one complete target snapshot. It contains only authorized selected stanza definitions, referenced runtime artifacts, and target-specific encrypted credential material; it does not contain the complete charter or global credential store.
6. Canonically derive and compare charter, binding, stanza, credential-payload, runtime-artifact, and whole-content digests. Locally recomputed or caller-supplied hashes are not authoritative substitutes.
7. Bind the non-secret manifest signature to one deployment, recipient, monotonically increasing generation, previous generation, source digests, issue/expiry policy, and encrypted payload digest. Report signer/key identifier and verification status only; never request or expose key material.
8. Publication requires fresh authenticated admission and exact post-write readback. A preview, generated file, successful process exit, signature-shaped bytes, or uploaded object is not publication evidence.

If two compilations claim identical approved inputs but return different deterministic content digests outside explicitly separated issuance metadata, report drift or implementation failure. Do not select either result.

## Reconciliation and atomic activation contract

When a shipped typed edge service exists, reconciliation must remain an authenticated deployment pull based on enrollment identity. A caller-provided deployment ID or stanza selector is never routing authority.

1. Compare exact desired, downloaded, staged, active, and acknowledged generation/digest pairs. Keep each state distinct; “latest” and mutable tags are forbidden substitutes.
2. A current response is valid only when the controller's desired generation and digest exactly match verified active local state. An acknowledgment alone does not establish this.
3. Download one immutable complete target snapshot into Aegis-owned staging. Authenticate the controller and verify manifest signature, deployment ID, location, environment, tenant, recipient, generation lineage, expiry, all component digests, and encrypted payload binding before decryption or preparation.
4. Decrypt only through deployment-local custody. Confirm every stanza is signed and allowed and every credential reference belongs to an included stanza and exact binding. Any mismatch rejects the whole candidate.
5. Prepare and validate runtime artifacts without changing active state. Partial activation is forbidden.
6. Atomically switch the active-generation pointer only after all checks pass. Policy and credential payload become active as one generation or not at all.
7. Re-evaluate existing mandates and sessions. Stop or expire sessions that cannot remain valid; a material stanza or authority change requires a new mandate and clean Hermes session.
8. Verify effective local state after activation, then send one typed acknowledgment and provisioning receipt bound to exact deployment, generation, content digest, activation result, and observed state.
9. Remove stale material only when no active authorized session references it and Aegis retention policy permits removal. Never infer erasure from desired-state convergence.

Reapplying the exact same immutable generation is idempotent. The edge should return its already-active exact digest and receipt rather than duplicate activation. Same generation with changed content, changed recipient, changed source bindings, or changed signature is a conflict and denies.

## Drift, revocation, interruption, and rollback

Classify drift precisely:

- Controller desired ahead of active: pending reconciliation, not current.
- Staged differs from desired or fails verification: rejected candidate; retain the last valid unexpired generation according to explicit offline policy.
- Active digest differs from its signed generation: local integrity failure; deny new authority dependent on that projection.
- Acknowledged differs from active: acknowledgment lag or uncertainty, not activation failure by assumption.
- Enrollment, recipient, binding, charter, or runtime provenance mismatch: security denial, not ordinary lag.
- Expired projection: apply exact offline policy. Availability pressure never creates implicit offline authority.

Revocation is a sequence, not a deletion claim: disable the stanza or deployment binding, revoke affected mandates and terminate sessions, publish a new monotonic generation without the credential, rotate or revoke the downstream credential, distribute replacements only to remaining authorized deployments, and verify audit/readback for each distinct result. A new projection cannot prove a previously exposed value was erased.

After interruption, begin with typed inspection of desired, downloaded, staged, active, and acknowledged state plus canonical audit. If the exact candidate is already active and receipt-bound, return the idempotent result. If staging is incomplete, signature or digest status is unknown, stores disagree, or readback is unavailable, retain the last valid state and stop for typed recovery or operator review. Never delete staging, edit generation pointers, repeat publication, activate partial files, or acknowledge success blindly.

Ordinary rollback to a lower generation always denies. Content rollback requires a newly approved and signed projection that deliberately references older content, states the rollback reason, and receives a new higher generation. Generation remains monotonic even when content is reverted. Without a shipped typed rollback preview, admission, and readback surface, report rollback unavailable.

## Reporting

Report these fields only when returned authoritatively: requested scope; availability; authenticated principal or deployment identity; Agent and charter revision/digest; enrollment and binding IDs/digests; location/environment/tenant/runtime/recipient; selected and excluded stanza IDs; redacted credential reference/version selection; source and component digests; signer identifier and signature verification; previous, desired, downloaded, staged, active, and acknowledged generations/digests; expiry/offline state; activation atomicity; affected sessions; acknowledgment and receipt identity; drift class; revocation and downstream rotation status; rollback reason and new monotonic generation; audit verification; denial reason; missing evidence; limitations; next shipped typed operation.

Keep proposed, previewed, compiled, signed, published, downloaded, staged, active, acknowledged, current, drifted, expired, revoked, rolled-back-content, denied, interrupted, corrupt, and unavailable states distinct. For the supported release, conclude that projection compilation and reconciliation are unavailable unless installed typed help and protected API documentation prove otherwise.

## References

- `specs/DEPLOYMENT_PROJECTION.md` — future selective projection architecture and invariants; conceptual examples are not shipped operations.
- `specs/IDENTITY_AND_AUTHORIZATION.md` — external authentication and exactly-one-stanza session authority.
- `specs/RUNTIME_AND_SESSIONS.md` — mandates, clean sessions, and disposable Hermes runtime state.
- `specs/AUDIT.md` — authoritative Aegis event and verification boundaries.
- Installed `aegis --help` and protected API documentation — authoritative operation availability for the installed version.
