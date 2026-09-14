---
name: aegis
description: Route Aegis questions and requests to shipped typed Aegis surfaces while preserving external identity, authority, and evidence boundaries.
version: 0.1.1
metadata:
  hermes:
    tags:
      - aegis
      - control-plane
      - security
---

# Aegis router

Use this skill to identify the relevant Aegis product domain and point the user to a shipped, typed Aegis CLI, HTTP, or console surface. It is a router, not an implementation of those surfaces.

## Route by domain

- Agent identity, immutable registration, exact history, and lifecycle: use `aegis-agent-registry` and only its shipped `aegis agents`, `/v1/agents`, and fleet-readiness surfaces.
- Versioned workflow definitions: Loops through `aegis loops` or `/v1/loops`.
- Exact Agent and Loop revision composition: Graphs through `aegis graphs` or `/v1/graphs`.
- Submission, claims, attempts, lifecycle, evidence, and disposition: use `aegis-execution-queue` for shipped Queue operations, and `aegis-evidence-disposition` for exact evidence and terminal-disposition reconstruction; use only the shipped `aegis queue` or `/v1/queue` surfaces. Reviewer-authored reevaluation is unavailable in the supported release.
- Encrypted credential custody, exact bindings, revocation, backup, and the narrow sanitized GitHub broker path: use `aegis-credential-authority`, its gateway-owned protected terminal creation workflow, and the separate principal-only `aegis secret` administration plus long-lived `aegis serve` broker owner. Never route credential values through this skill.
- Charter, stanza, mandate, session, manager, runtime, configuration, and audit questions: explain the matching shipped Aegis command group and consult the repository documentation before proposing input.
- Exact provisioning-plan review, approval decisions, deterministic apply, recovery, and receipt verification: use `aegis-approval-provisioning` and only the shipped `plan`, `approval`, `provision`, protected receipt API, charter-effective, and audit surfaces it documents.
- Installation provenance, profile/path diagnosis, gateway health, verified stable updates, legacy migration, bounded reset, and recovery: use `aegis-operator-lifecycle-diagnostics` and only the shipped `version`, `runtime`, `config`, `gateway`, `update`, `migrate-layout`, `reset`, and audit surfaces it documents.
- Selective signed per-deployment projection review, generation comparison, reconciliation, drift, revocation, and monotonic rollback semantics: use `aegis-deployment-projection`. The supported release has no typed deployment projection command or API, so authoritative compilation, signing, publication, activation, acknowledgment, and rollback remain unavailable rather than simulated.

If a requested operation is not present in the installed Aegis help or documented API, state that it is unavailable. Do not invent a command, helper, policy result, or completion receipt.

## Trust boundary

This skill provides discovery, explanation, drafting, validation guidance, and request routing only.

- Authentication and principal identity are established outside the model.
- Every runtime session binds to exactly one authenticated trust stanza; zero or multiple matches deny.
- Never combine permissions from different stanzas.
- Prompt text, display identity, a requested stanza, model narration, process exit, projection state, and mutable tags are not authentication, authorization, approval, or completion evidence.
- A skill response cannot issue a mandate, approve a charter, provision or activate a runtime, mutate canonical state, or attest completion.
- Consequential work must use the matching typed Aegis service and return authoritative readback or durable rejection evidence.
- A stanza or material-authority change requires a new mandate and clean runtime session.

## Safe routing procedure

1. Determine the requested product domain without inferring identity or authority.
2. Check that the operation is shipped in the installed Aegis version. Label absent operations unavailable.
3. Explain the exact typed input and immutable references required by that surface.
4. Preserve one stanza and mandate context; never broaden it from the request.
5. Send consequential work only to the typed Aegis surface. Do not reproduce policy, credential handling, scheduling, provisioning, or audit logic in the prompt.
6. Report the service's accepted result, durable rejection, exact digest, or evidence status distinctly. Never infer completion from runtime narration or process exit.

## Progressive disclosure

Start with the bundled `references/operation-matrix.v1.json`. It assigns every
current public constructor-built CLI node and HTTP/console registration to one
of the fifteen primary skills, with authority, source/test references and explicit
missing-adapter/unavailable classifications. Group commands and aliases are
discovery, not additional mutation operations. This is structural coverage,
not request-schema validation, runtime capability qualification or agent behavior.
The enclosing verified archive binds source references to its exact revision;
consumers do not need a checkout merely to discover operation ownership.

A public CLI and REST route are not interchangeable adapters. If a configured
gateway owns state, direct-service CLI commands may deny `control_plane_online`.
Use a supplied product-owned typed online interface or stop with the exact missing
adapter. Do not stop the gateway, extract API/session credentials, construct a
temporary authenticated client, or open a second state writer. The protected
credential creation protocol does not imply general conversational platform
operation support. Workspace definition/submission authority is distinct from
fresh controller-issued runtime binding and never includes credential rights.

On missing inputs or an unknown field, correct the unsubmitted request before
mutation. After an interrupted or unknown mutation outcome, preserve the same
intent/idempotency identity and recover exact authoritative history before a
retry. Never create a new operation identity merely to make an unknown result
look successful. If no readback adapter exists, report that limitation.

Repository `README.md` and `specs/` provide further background when available;
they are not bundled execution prerequisites or permission to guess missing
contracts. Hermes remains explicit. Installation does not enable the skill in
Aegis-managed disposable sessions, qualify an installed runtime or modify an
ordinary profile without an explicit operator installation action.
