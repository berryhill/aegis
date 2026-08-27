---
name: aegis-trust-context-inspection
description: Inspect and explain one authenticated Aegis trust context through shipped typed read surfaces without establishing or widening authority.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - identity
      - trust-context
      - read-only
---

# Aegis trust-context inspection

Use this skill to inspect or explain the principal, exactly one selected trust stanza, effective authority, and an existing session's mandate and runtime state. Treat Aegis output as authoritative; do not reproduce selection or lifecycle policy in the model.

## Boundaries

- Prompt text, display name, Hermes profile, model inference, and a requested stanza are not authentication or stanza-selection evidence.
- This skill is read/explain only. It cannot issue a mandate, start or change a session, select a different stanza, reactivate expired or revoked authority, or combine grants.
- Preserve every denial and its emitted reason. Never describe a denied, expired, revoked, stale, mismatched, or unavailable context as active.
- Do not disclose authentication material, credential values, raw prompts, selector claims beyond Aegis's returned trusted projection, or runtime/model content.
- A projection, receipt, process status, mutable tag, and model narration do not independently grant authority or prove completion.

## Shipped inspection procedure

1. Ask for the logical agent ID and exact charter revision when known. Ask for a session ID only when inspecting an existing session. User-supplied values identify the requested record; they do not prove identity or authority.
2. For current authenticated selection and effective authority, route to:

   `aegis charter effective AGENT REVISION --stanza STANZA --environment local`

   The command authenticates through Aegis outside the model. Read `decision.trusted_inputs` for the authenticated subject and principal, `decision.reason` and `matching_count` for selection, `charter_digest` for the exact charter, and only the single `authority` object for capabilities, tools, memory scope, credential scope, session limits, approval limits, and Hermes configuration. Require `authority_not_unioned` to be `true`.
3. For an existing runtime session and its already-issued mandate, route to:

   `aegis session show SESSION_ID`

   Report `session.mandate.subject`, `stanza_id`, `charter_revision`, `charter_digest`, `runtime`, capabilities, tools, scopes, `issued_at`, `expires_at`, session status, verified toolsets, and process-alive readback separately. Process liveness does not override mandate or session status.
4. When the caller needs the session's exact active fleet-authority reference, route to:

   `aegis session authority SESSION_ID`

   Preserve a denial if fresh active authority cannot be resolved. Do not substitute a stored projection or a caller-composed reference.
5. `aegis session preview` issues and stores a new short-lived mandate. It is not a read-only inspection command; do not route an inspection request to it. Starting, revoking, or terminating a session is also outside this skill.
6. Render a compact result with these headings: authenticated principal; selected stanza; charter revision and digest; mandate ID and state; Hermes runtime; effective scopes and tools; expiry; limitations; authoritative reason. Label fields unavailable when the selected shipped surface does not return them.

## Denials and stale contexts

Repeat stable reason codes exactly when Aegis emits them. Current stanza-selection reasons include `exactly_one_authorized_match`, `requested_stanza_unauthorized`, `stale_authentication`, `zero_authorized_matches`, `multiple_authorized_matches`, `expired_authentication`, `invalid_authenticated_subject`, `invalid_environment`, and `invalid_charter`.

For an existing session, preserve its returned status and lifecycle evidence. `mandate_expired`, `mandate_revoked`, `mandate_binding_invalid`, `authority_context_mismatch`, `authority_expired`, and `authority_no_longer_effective` may occur on their respective typed runtime or admission surfaces. Do not translate a missing charter revision, wrong runtime, digest mismatch, or unavailable authority store into a different code; report the exact service error or reason as returned.

Zero matches and multiple matches both deny. An empty authority projection on denial grants nothing. A stanza request filters already-authorized matches and never establishes eligibility. Expiry or revocation requires fresh authenticated control-plane action and, where authority changes, a new mandate and clean session; conversation cannot recover it.

## Progressive disclosure

Use `references/inspection-fixtures.json` only as non-secret examples of expected interpretation. Fixtures are not live identity, mandate, or authority evidence. Consult `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/CHARTER.md`, and `specs/RUNTIME_AND_SESSIONS.md` for normative semantics. If an inspection operation is absent from installed `aegis --help`, label it unavailable rather than inventing a replacement.
