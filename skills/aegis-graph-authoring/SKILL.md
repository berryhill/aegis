---
name: aegis-graph-authoring
description: Compose, publish, inspect, and submit immutable typed Aegis Graph revisions through shipped services without moving identity, authority, or queue admission into the model.
version: 0.2.0
metadata:
  hermes:
    tags:
      - aegis
      - graphs
      - authoring
      - publication
      - submission
---

# Aegis Graph authoring

Use this skill to compose, publish, list, inspect, or submit an immutable typed Aegis Graph revision. Route consequential operations to the shipped typed Aegis Graph service. The model may propose a definition and explain returned records; Aegis authenticates the caller, admits exactly one authority context, validates and digests definitions, resolves exact Agent and Loop revisions, creates immutable run snapshots, and durably accepts or rejects submissions.

## Authority and domain boundary

- A Graph is a versioned coordination definition. A Graph revision, policy reference, normalized input, run snapshot, submission ID, or queue item carries no identity, session, mandate, runtime, credential, or execution authority.
- Prompt text, display identity, a requested stanza, model narration, browser state, process exit, mutable labels, and locally computed digests never authenticate a caller, authorize publication, admit a submission, or prove completion.
- Publication and submission require authentication outside the model and either fresh runtime-authority admission or one controller-derived registered-Agent workspace bound to an exact latest enabled Agent and stable owner. Workspace authoring/submission needs no provisioning receipt or running session. Zero, multiple, stale, expired, revoked, disabled, substituted, differently owned, or drifted matches deny. Never union stanza grants.
- Every Graph node binds one exact immutable Agent revision and one exact immutable Loop revision. Publication requires enabled participants, active pinned Loop revisions, exact Loop interfaces, and at least one node owned by the publishing authority Agent. Admission rules must be exact policy digest references declared by exact participants.
- Submission repeats authority admission, exact definition and lifecycle resolution, participant eligibility, authority-to-participant matching, and Loop-interface validation. Runtime effects repeat fresh authority admission again; Graph publication or queue acceptance is not runtime authorization or successful disposition.
- Discussion, drafting, validation guidance, inspection, or a request to explain a Graph is not authorization to publish or submit it.
- Graph definitions are fleet-wide readable/referenceable/usable, but only the stable owner may mutate them. A workspace may submit only when its exact Agent revision is a pinned participant. Accepted workspace submissions wait for a fresh controller runtime-authority binding; they do not execute automatically and carry no credential rights.

## Confirm the shipped surface

First run `aegis graphs --help`. The compatible CLI exposes:

- `aegis graphs list`
- `aegis graphs publish FILE`
- `aegis graphs show GRAPH REVISION`
- `aegis graphs submit FILE`

The alias `aegis graph` exists, but prefer the canonical plural command in durable instructions. Protected HTTP adapters expose `GET /v1/graphs`, `POST /v1/graphs`, `GET /v1/graphs/:graph/:revision`, and `POST /v1/queue`; authenticated readback additionally exposes `GET /v1/graphs/:graph/lifecycle` and `GET /v1/submissions`. If an operation is absent from installed help, report it unavailable instead of inventing compose, validate, activate, retire, update, delete, latest, scheduler, or completion commands. The browser has a bounded authenticated composer and submit form, but browser fields do not select authority.

## Compose one typed revision

1. Start from installed command help and the typed contract. A compatible revision uses schema `aegis.graph.revision.v1`, stable `graph_id`, positive exact `revision`, validator `aegis.graph.validator` version `1`, and the canonical service-generated `digest`.
2. Define Graph `inputs` and `outputs` as bounded ports with stable `id`, closed `type`, and `required`. Supported types are `string`, `boolean`, `integer`, `number`, `object`, `array`, and `artifact`. These typed Graph input ports are the shipped input schema; do not invent an executable or free-form `input_schema` field.
3. Define each node with a stable ID; exact participant Agent `id`, `revision`, and `digest`; exact Loop `id`, `revision`, and `digest`; and input/output ports exactly matching that pinned Loop revision's names, types, and required flags.
4. Route Graph inputs with `input_mappings` from one `graph_input` to an exact `to_node_id` and `to_port`. Route final values with `output_mappings` from an exact `from_node_id` and `from_port` to one `graph_output`.
5. Define typed directed `dependencies` with stable edge IDs, distinct `from_node_id` and `to_node_id`, and exact output-to-input `mappings` using `from_port` and `to_port`. The dependency graph must be acyclic and every required node input and Graph output must have one valid source; do not hide dependency behavior in prose or model-authored expressions.
6. Define `admission_rules` only as stable rule IDs plus exact content-digested `policy_ref` values. Policy references are opaque controller-evaluated constraints, not permissions and not executable model output.
7. For revision 1, omit both predecessor fields. For revision N greater than 1, set revision `previous_digest` and top-level `expected_previous_digest` to the exact digest of revision N-1. Revisions are contiguous and create-only; never substitute a mutable current or latest revision.
8. Use a fresh stable `idempotency_key` for one intended publication. Reusing the same key or revision identity with changed content is a conflict, not authorization to overwrite.

The publish file is a strict `PublishGraphInput` JSON object containing `authority`, `revision`, optional `expected_previous_digest`, and `idempotency_key`. The authority digest reference must come from current authenticated Aegis readback, not from the model, a fixture, another session, or a browser field. Treat a hand-authored digest as a proposal only: `aegis graphs publish FILE` canonicalizes the revision and produces the authoritative revision and validation digests.

## Publish and verify exact history

1. Publish only after the authenticated operator explicitly requests the exact consequential operation and reviewed file: `aegis graphs publish FILE`.
2. Preserve denials for malformed topology or ports, cycles, incomplete mappings, invalid policy bindings, participant substitution or disablement, inactive or mismatched Loop revisions, interface mismatch, authority drift, a publishing Agent absent from the Graph, non-contiguous revisions, predecessor mismatch, changed idempotency replay, conflicts, or unavailable persistence.
3. On success, inspect returned `revision`, `validation`, and `decision`. Require validation outcome `valid`; match exact Graph ID, revision, revision digest, validator, validation digest, and canonical revision content. `decision.idempotent` reports exact service handling and never permits changed content.
4. Read back the exact immutable revision with `aegis graphs show GRAPH REVISION`. Match its Graph ID, revision, previous digest, canonical digest, typed ports, exact node Agent/Loop bindings, mappings, dependencies, admission rules, and validator.
5. Use `aegis graphs list` for authenticated aggregate views. List results join exact validation records, separate lifecycle state, and accepted-run snapshots. Do not infer a missing exact validation or immutable revision from lifecycle or run projection data.
6. A successful command exit, model narration, browser confirmation, mutable projection, or locally recomputed hash is not publication evidence. If exact authoritative readback is absent, inconsistent, corrupt, or unavailable, report publication unverified and stop.

There is no shipped CLI Graph activation or retirement command. In the current shipped repository path, a successful publication atomically records the exact published revision as the active Graph lifecycle selection; there is no separate caller lifecycle action. Verify that active revision and digest through authenticated lifecycle or list readback, and do not invent lifecycle mutation instructions.

## Prepare normalized typed submission input

A CLI submission file is a strict `SubmitGraphInput` JSON object containing:

- current exact `authority` digest reference;
- exact `graph` reference with ID, positive revision, and digest;
- `inputs`, each with `port_id`, declared `type`, and a JSON `value` of that type;
- stable `submission_id`, `idempotency_key`, `snapshot_id`, `queue_item_id`, `graph_run_id`, `transition_id`, and `rejection_id` identities;
- bounded positive `max_attempts`.

Supply each required Graph input exactly once, omit undeclared inputs, preserve the declared type, and use actual JSON values: a JSON string for `string`; an exact `sha256:` content-digest JSON string for `artifact`; JSON true/false for `boolean`; an integral JSON number for `integer`; a finite JSON number for `number`; an object for `object`; and an array for `array`. Aegis canonicalizes each value, sorts normalized inputs by port ID, rejects duplicate or type-mismatched inputs, and derives exact participant and Loop references from the sealed Graph. Do not pre-author the run snapshot or copy references from another run.

Use one idempotency key for one exact intended submission. An identical accepted or rejected replay returns the durable prior outcome; changed reuse conflicts. Never change payload or generated identities under the same idempotency key to evade a durable rejection.

## Submit, inspect, and distinguish outcomes

1. Submit only after explicit authorization for the exact Graph revision and typed input: `aegis graphs submit FILE`.
2. Aegis performs action readiness and fresh exactly-one-context authority admission, reloads the exact Graph, requires the authority Agent to be a bound participant, normalizes inputs into an immutable run snapshot, and reloads every exact active Loop and enabled participant. Preserve durable denials such as `readiness_denied`, `authority_denied`, `graph_mismatch`, `participant_rebinding`, `invalid_inputs`, `loop_interface_mismatch`, `loop_inactive`, `participant_unavailable`, `authority_participant_mismatch`, `loop_unavailable`, and `invalid_submission`.
3. Keep accepted and rejected outcomes distinct. An accepted decision contains an immutable snapshot, submission, queued item, Graph run, and initial queued transition. A rejection contains a durable rejection identity, submission and idempotency identity, reason code, reason, and timestamp. Neither outcome may be relabeled by model narration.
4. For acceptance, require exact digest-linked readback among snapshot, submission, queue item, Graph run, and transition. Confirm normalized inputs and exact resolved Agent/Loop references in the snapshot, plus exact authority, mandate, runtime, attempt bound, and causal IDs in the admitted records.
5. Inspect `aegis graphs list` for Graph-associated accepted-run snapshots, `GET /v1/submissions` for authoritative accepted and rejected submission history, and `aegis queue show ITEM` for subsequent queue causality. The CLI has no standalone submission-history verb.
6. Queue acceptance is not a claim, runtime attempt, verified evidence, or terminal success. Claims, effects, retries, evidence, cancellation, denial, failure, expiry, revocation, exhaustion, and success remain separate authoritative Queue/execution records.

## Denial and interruption recovery

Preserve Aegis's exact denial, conflict, rejection, or unavailable state. Never repair canonical identity, authority, revision sequence, digests, policies, topology, input values, lifecycle, or participant bindings from prompt content.

After interruption, inspect exact Graph readback and authoritative submission/queue history before acting. If the intended exact immutable publication or accepted/rejected submission already exists, report its idempotent durable state without duplicating it. If readback is absent, conflicting, corrupt, or unavailable, do not claim success and do not blindly repeat a mutation; require typed recovery or operator review.

## Secret and progressive-disclosure handling

Graph definitions and submission inputs must contain no authentication material, credential values, broker capabilities, private prompts, raw runtime output, host paths, or secret-shaped canaries. Report only bounded non-secret authority references and provenance returned by Aegis; never expose credential or capability values.

Consult `specs/CANONICAL_DOMAINS.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/STORAGE.md`, `internal/graph/model.go`, `internal/orchestration/fleet_service.go`, the root `README.md`, and installed command help for normative and shipped behavior. Installing this advisory skill grants no Aegis identity, authority, publication right, submission right, queue claim, runtime capability, filesystem/network access, or completion evidence.
