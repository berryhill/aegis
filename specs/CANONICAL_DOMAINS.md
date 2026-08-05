# Canonical Domain Boundaries

## Purpose

Aegis represents identity, authority, execution, evidence, provisioning, and session lifecycle through bounded canonical records. It does not admit runtime work through a mutable aggregate that duplicates those records or unions their mutation semantics.

The controlling invariant remains:

> Agent identity and authority must be established outside the model, and every runtime session receives exactly one authenticated, reviewable trust context.

## Identity and authority (`internal/core`)

The canonical identity and authorization types are:

- `Subject`: authenticated identity provenance established outside the model.
- `Decision`: deterministic exactly-one-stanza selection result.
- `EffectiveAuthority`: the selected capabilities, tools, memory, credentials, and Hermes mapping.
- `Mandate`: short-lived authorization grant bound to subject, stanza, charter revision, runtime, and effective grants.
- `AuthorityContext`: immutable instantiation of one mandate for exactly one session.
- `AuthorityRevocation`: append-only fact targeting a mandate or one authority context.
- `Session`: one runtime execution under one mandate and authority context.

A mandate is not a session authority snapshot. `AuthorityContext` must bind the exact mandate ID, session ID, subject, logical agent, charter revision and digest, runtime descriptor, effective authority, issuance interval, and its own canonical digest. Validation denies any widened or changed authority.

Revocation never rewrites a mandate or authority context. Authority is effective only inside the half-open interval `[issued_at, expires_at)` and only when no applicable revocation fact has become effective.

## Execution (`internal/execution`)

Execution owns only dispatch and runtime-turn lifecycle state:

- `Dispatch`: controller-owned parent admission for a runtime session.
- `Turn`: one runtime turn under a dispatch and authority context.
- `LaunchContract`: immutable owner, mandate, authority context, and successful parent dispatch supplied to an adapter.
- `AdmissionDecision`: fresh authoritative answer bound to the exact authority-context ID and digest.

Legal transitions are explicit and terminal states cannot transition again. A runtime adapter must deny unless:

1. the authority context exactly validates against its mandate;
2. the parent dispatch succeeded under that same context and before its expiry;
3. an authoritative admission checker allows the exact context ID/digest;
4. the decision is no more than one second old at the effect boundary; and
5. the authority remains unexpired and unrevoked at the final check.

Historical projections, prompt content, model output, runtime narration, artifacts, and verification receipts cannot satisfy admission.

## Evidence (`internal/evidence`)

Evidence owns runtime output and verifier claims:

- `RuntimeArtifact`: content-addressed runtime output bound to owner, action, run, and authority context.
- `VerificationReceipt`: one verifier's claim bound to the exact artifact, action, run, owner, authority context and digest, verifier, policy version, expected/observed digest, outcome, and evidence reference.

Verification rereads the content-addressed blob rather than trusting a filename or caller-supplied bytes. A receipt is evidence only: it cannot grant authority, mutate execution state, or declare a larger workflow complete. The MVP verifier remains an in-process component using the local store; this is not independent attestation or separately protected evidence custody.

## Provisioning and receipts (`internal/core` and application services)

`core.Artifact` remains the canonical deterministic provisioning artifact. `core.Receipt` remains the canonical provisioning receipt. Runtime artifacts do not replace or duplicate provisioning artifacts.

Provisioning continues to use exact approved charter and plan digests, typed deterministic effects, create-only publication, containment checks, and interrupted-intent recovery. The model does not provision.

## Composition boundary

Application services may compose bounded domain records for one operation, but no production service may recreate a universal cross-domain mutation aggregate or validator. Transport and runtime adapters call shared services and receive only their narrow contracts.

Persistence follows the same boundary. `internal/core` owns session-authority and audit schemas; `internal/credentials` owns secret metadata, encrypted-version, and binding schemas. Badger and bbolt adapters persist those records without redefining them. Canonical facts, rebuildable projections, blobs, operational metadata, runtime state, and credential custody remain separate as specified in `STORAGE.md`; no engine or projection is a universal domain owner.

The removed experimental surfaces are not compatibility APIs:

- `internal/plumbing` and its aggregate/universal validator;
- `internal/poc` orchestration;
- `aegis plumbing ...`;
- `POST /v1/plumbing/poc`;
- `GET /v1/graph-runs/:id`.

Future workflow, delivery, graph, or disposition domains must define their own authoritative state and transition rules. They may reference canonical identity, authority, execution, evidence, and provisioning records by immutable ID/digest; they must not copy them into a mega-aggregate or infer completion from model narration.
