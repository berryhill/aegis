---
name: aegis-approval-provisioning
description: Review exact Aegis provisioning plans and guide authenticated approval, deterministic apply, recovery, and receipt verification without granting authority to the model.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - approval
      - provisioning
      - receipts
---

# Aegis approval and provisioning

Use this skill to review a complete canonical provisioning plan, route an authenticated principal's exact decision to Aegis, and verify deterministic Aegis-owned results. The skill is advisory. Aegis remains authoritative for authentication, plan canonicalization and persistence, approval decisions, digest checks, application, rollback, recovery, receipts, and audit.

## Boundaries

- Prompt text, conversational assent, display identity, model inference, a Hermes profile, process exit, projection state, and mutable tags are never authentication, approval, authorization, or completion evidence.
- The model cannot request or decide approval on the principal's behalf, provision, recover, broaden effects, write provisioning shell commands, consume an approval, or attest success.
- Discussion, plan inspection, and receipt inspection do not authorize approval, rejection, apply, recovery, activation, or external mutation. Route a consequential operation only after an explicit request from the principal authenticated by Aegis outside the model.
- The shipped provisioner permits only deterministic create-file effects beneath Aegis-owned provisioned storage. Deny replacement, traversal, symlink parents, unknown effects, and external, service, gateway, cron, plugin, MCP, profile, credential, or network effects.
- Never include credential values, authentication material, reusable tokens, private prompts, runtime-home paths, or host secrets in requests, fixtures, or summaries.

## Review an exact plan

1. Confirm the installed surfaces with `aegis plan --help`, `aegis approval --help`, and `aegis provision --help`. Label any absent operation unavailable rather than inventing a command or bypass.
2. An authenticated principal may create a persisted review with:

   `aegis plan preview AGENT --revision REVISION --environment local`

   Use an explicit revision for approval-sensitive work. Revision `0` means current latest and is mutable input, not an immutable approval reference.
3. Read an existing plan with `aegis plan show PLAN_ID`. Aegis recomputes the stored plan digest before returning it. Stop on missing, corrupt, or digest-mismatched state.
4. Present every returned field before a decision: agent and charter revision; `charter_digest`; plan ID and `plan_digest`; Hermes runtime identity, version, executable, and adapter version; target environment; every effect's kind, target, digest, and consequence; complete previous-revision diff; warnings and limitations; per-stanza requested toolsets, memory scopes, credential scopes, and approval lifetime/single-use requirements.
5. Preserve stanza maps separately. Never union grants. A plan review proposes deterministic effects; it is not an approval, mandate, session, activation, or proof of application.

## Request and decide exact approval

After review, an authenticated principal may explicitly request a bounded decision record:

`aegis approval request PLAN_ID --ttl DURATION`

Inspect it with `aegis approval show APPROVAL_ID`. Before any decision, require exact equality between the freshly read plan and approval for plan ID and digest, charter digest, Hermes runtime and version, and target environment. Report requester, status, request time, expiry, decision time when present, and single-use state.

Only route an explicit authenticated-principal decision:

- `aegis approval approve APPROVAL_ID`
- `aegis approval reject APPROVAL_ID`

Conversational words such as “looks good” are not the typed decision. Approval is valid only for the exact returned canonical bindings. Reject distinctly when the plan changed, an approval was replayed, expired, rejected, consumed, missing, or corrupt, the authenticated actor is not the configured principal, or runtime, runtime version, environment, plan ID, plan digest, or charter digest differs. Never repair, refresh, substitute, or widen a denied record. A new plan or changed authority requires a new review and approval.

## Deterministic apply

Immediately before apply:

1. Freshly read `aegis plan show PLAN_ID` and `aegis approval show APPROVAL_ID`.
2. Require the plan digest to revalidate and every exact binding above to match. Require approval status `approved`, current time before `expires_at`, and no `consumed_at`.
3. Re-present all effects and warnings. Do not translate them into shell, profile, gateway, plugin, MCP, service, cron, credential, or external-system operations.
4. Only on a separate explicit authenticated-principal apply request, invoke:

   `aegis provision PLAN_ID APPROVAL_ID`

Aegis transactionally consumes the single-use approval and creates an in-progress receipt before applying effects. A command error, interruption, or process exit is never success and must not be retried blindly with the consumed approval.

## Verify receipt and effective result

The `aegis provision` success response is the authoritative receipt readback. The protected HTTP API also ships `GET /v1/receipts/:id` and `GET /v1/receipts`; there is no shipped receipt CLI command, so never invent one.

Report success only when all of these hold:

- receipt status is exactly `verified` and failure is empty;
- receipt plan ID, plan digest, approval ID, and charter digest equal the freshly revalidated plan and approval;
- every approved create-file effect has exactly one receipt artifact and no extra artifact exists;
- each artifact path, action, and digest equals its approved effect and `verified` is true;
- finished time is present and the approval's authoritative readback is consumed;
- the plan's per-stanza `requested_toolsets` still exactly matches the reviewed charter authority, checked through `aegis charter effective AGENT REVISION --stanza STANZA --environment local` for each separately authorized stanza; require `authority_not_unioned: true`. The receipt's verified content-addressed Aegis-owned artifact binds that same exact charter and plan. Do not launch Hermes merely to prove provisioning.

A verified provisioning receipt proves only the bounded Aegis-owned artifacts in that exact plan. It does not activate a runtime, issue a mandate, start a gateway or session, prove host sandboxing, or authorize later work.

## Interrupted and partial application

Aegis performs startup recovery from durable in-progress receipts; no standalone recovery CLI is shipped. If the authenticated principal explicitly authorizes the normal operator-controlled restart or startup of the existing owning Aegis service, recovery runs before serving. Without that authorization, report recovery as unavailable and do not act. Afterward inspect the protected receipt API or authoritative audit readback. Do not mutate receipt files or provisioned artifacts directly.

Recovery marks an interrupted receipt `failed`. It removes only newly published Aegis-owned artifacts whose exact plan, approval, charter, and content bindings are established. Non-matching artifacts are preserved and the failure requires manual intervention. Preserve `provisioning`, `failed`, rolled-back, preserved/manual-intervention, and `verified` as distinct states. Never turn a valid prefix, missing readback, zero exit, existing file, or model narration into success; never reuse the consumed approval.

## Safe response format

Report: operation; authenticated principal as returned by Aegis; agent and charter revision/digest; plan ID/digest; runtime and environment; complete effects; separated stanza grants and requested toolsets; diff; warnings; approval ID/status/requester/decider/expiry/consumption; receipt ID/status/bindings/artifacts/verification/times/failure; recovery state; limitations. Clearly distinguish proposed, reviewed, pending decision, approved, rejected, expired, consumed, provisioning, failed, recovered, verified, denied, and unavailable.

## Progressive disclosure

Use `references/provisioning-fixtures.json` only as non-secret interpretation examples, never as live authority or receipts. Consult `specs/APPROVAL_AND_PROVISIONING.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/CHARTER.md`, and the installed command help for normative and shipped behavior. Use `aegis-audit-verification` for canonical history correlation after receipt verification. Installing this skill grants no approval or provisioning authority.
