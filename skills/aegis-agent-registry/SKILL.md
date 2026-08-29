---
name: aegis-agent-registry
description: Register, inspect, and govern immutable Aegis Agent Registry revisions through shipped typed services without treating profiles or model content as identity or authority.
version: 0.1.0
metadata:
  hermes:
    tags:
      - aegis
      - agent-registry
      - identity
      - lifecycle
---

# Aegis Agent Registry

Use this skill for the shipped credential-independent Agent Registry workflow: register exactly one existing fleet participant, inspect current or historical immutable revisions, and request append-only enabled, disabled, or retired lifecycle revisions. Aegis authenticates, authorizes, validates, persists, and audits every operation outside the model.

## Boundaries

- A canonical Agent ID is stable Registry identity. A Hermes profile, display metadata, prompt identity, fixture, source ID, model narration, process exit, projection state, deployment reference, or mutable tag is not authentication, authorization, registration, ownership, approval, or completion evidence.
- A Hermes profile is a runtime/provisioning artifact or projection, never the canonical Agent Registry. Registration does not create, import, provision, launch, or certify a profile or runtime.
- The current-fleet fixture is a deterministic proposal only. It grants no authority and must contain no credentials, authentication material, private prompts, runtime-home paths, or secret-shaped values.
- Never infer owner or accountability identity. Preserve the exact `ownership.owner_id` and `ownership.accountability_id` returned by Aegis.
- Never rewrite an existing revision, substitute `latest` for an exact reference, combine trust stanzas, or treat capability declarations or policy references as effective runtime grants.
- Reads and writes require Aegis authentication outside the model. Lifecycle writes additionally require authenticated Aegis admission against the exact current Agent revision and digest.

## Confirm the shipped surface

Check `aegis agents --help` before routing work. The shipped CLI operations are:

- `aegis agents register FILE`
- `aegis agents list`
- `aegis agents show AGENT [REVISION]`
- `aegis agents history AGENT`
- `aegis agents enable AGENT FILE`
- `aegis agents disable AGENT FILE`
- `aegis agents retire AGENT FILE`

The corresponding protected HTTP operations are `POST /v1/agents`, `GET /v1/agents`, `GET /v1/agents/:agent?revision=REVISION`, `GET /v1/agents/:agent/revisions`, and `PUT /v1/agents/:agent/lifecycle`. Registry collection readiness is included in `GET /v1/fleet/readiness`; there is no shipped standalone `aegis agents readiness` CLI command. The authenticated manager has a separate bounded `/agents readiness|list|show|prepare|register` surface, but it does not expose lifecycle or history operations.

If an operation is absent from installed help, label it unavailable. There is no shipped Agent update, delete, unretire, deployment-management, profile-discovery, or arbitrary source-scanning operation.

## Register one existing participant

1. Require a canonical charter to have already been imported. Registration does not import it.
2. Prepare one strict JSON request file with exactly `fixture` and `identity`. `fixture` is an embedded `aegis.current-fleet.fixture.v1` object. `identity` contains exact `fleet_id`, `kind: "current-fleet"`, and `source_id`. Do not add caller identity or authority fields.
3. In the selected fixture candidate, preserve:
   - canonical `agent_id`;
   - fleet and source provenance;
   - `runtime.adapter`, `runtime.runtime`, and `runtime.target`;
   - `ownership.owner_id` and `ownership.accountability_id`;
   - exact charter ID, revision, and digest;
   - initial lifecycle;
   - sorted capability declarations and sorted exact policy digest references.
4. Route the request to `aegis agents register FILE` or `POST /v1/agents`. Aegis strictly decodes the request, resolves exactly one matching source, verifies the pre-imported charter binding, seals revision 1, and performs create-only persistence.
5. Require authoritative response readback. Preserve `created`, `registration.agent_id`, `registration.source`, `registration.initial_revision`, and every field of `revision`, including its exact digest.

An identical retry is idempotent and returns the existing canonical record with `created: false`. The same Agent ID or fleet-source identity with different content is a conflict, not an update. Missing, malformed, ambiguous, or non-canonical candidates deny; do not repair fields in the model.

## Inspect and reconstruct history

- Use `aegis agents list` or `GET /v1/agents` for latest registered views. An authenticated empty list is valid; unavailable storage or authority is not an empty Registry.
- Use `aegis agents show AGENT` for the latest revision only when the caller explicitly requests current state. Use `aegis agents show AGENT REVISION` or the HTTP `revision` query for exact historical readback.
- Use `aegis agents history AGENT` or `GET /v1/agents/:agent/revisions` for ordered immutable revision history.
- For each result, report canonical Agent ID; revision and digest; fleet/kind/source provenance; charter ID/revision/digest; runtime adapter/runtime/target; owner/accountability; lifecycle; capability declarations; policy references; and initial registration reference.
- The current schema does not expose a separate runtime-adapter version or deployment-reference field. Report those fields unavailable rather than deriving them from a target, Hermes profile, mutable tag, or surrounding system.
- CLI and HTTP are adapters over the same application service. When comparing them, require exact equality for Agent ID, revision, digest, lifecycle, source, charter, runtime, and ownership. A mismatch is a conflict or integrity concern, never harmless presentation drift.

Historical revisions remain readable after disablement or retirement. History reconstructs what Aegis recorded; it does not reactivate the participant or prove runtime health, deployment, mandate validity, queue completion, or current authority.

## Append a lifecycle revision

1. Read the exact current revision immediately before mutation.
2. Create a strict JSON file containing `expected` with exact `id`, positive `revision`, and `digest`. For HTTP, also provide the closed `lifecycle` value. The CLI `enable`, `disable`, or `retire` verb fixes that value and does not trust a conflicting file value.
3. Route to `aegis agents enable AGENT FILE`, `aegis agents disable AGENT FILE`, `aegis agents retire AGENT FILE`, or `PUT /v1/agents/:agent/lifecycle`.
4. Require the path Agent ID to equal `expected.id`. Aegis repeats principal authorization and compares the expected revision and digest with the latest canonical revision before appending.
5. Verify returned Agent ID, incremented revision, new digest, requested lifecycle, and unchanged source, charter, runtime, ownership, capabilities, and policies. An already-current identical lifecycle request returns the current revision without rewriting history.

A stale revision/digest or path mismatch conflicts. A retired Agent cannot be re-enabled or disabled. Disabled and retired participants remain registered and readable but deny new executable participation on authority-bound Loop publication and Graph/Queue admission surfaces; preserve the exact denial from the invoked typed service. Do not collapse stale revision, wrong authority, disabled participant, and retired participant into one generic success or absence state.

## Readiness and reporting

Use `GET /v1/fleet/readiness` for authenticated collection/action readiness and preserve its `state`, `collections`, `actions`, and reason codes. Registry collection readiness is not runtime health or execution authority. The manager's `/agents readiness` is its own bounded Registry view and reports an empty Registry as valid when configured.

Report with these headings: operation; authenticated Aegis surface; canonical Agent and source; exact revision and digest; charter binding; runtime binding; owner/accountability; lifecycle; declarations/references; created/idempotent state; readiness; history evidence; denial/conflict; unavailable fields; limitations.

## Progressive disclosure

Use `references/registry-fixtures.json` only as non-secret interpretation examples. Fixtures are not live Registry records or authority. Consult `specs/CANONICAL_DOMAINS.md`, `specs/MVP.md`, `internal/registry/model.go`, `internal/registry/source.go`, and installed command help for the current contract. Consequential actions must remain in typed Aegis services; this skill never reimplements authentication, policy, persistence, provisioning, scheduling, credential custody, or audit authority.
