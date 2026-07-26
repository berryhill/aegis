# Plumbing Domain and Lifecycle Specification

## Purpose

Aegis plumbing is the participant-centric control-plane aggregate that preserves one authenticated, reviewable causal chain from ingress to terminal disposition:

> authenticated participant → trusted ingress fact → exactly-one trust-stanza decision → immutable mandate/authority context → dispatch and session attempts → typed operations and requests → artifacts and deliveries → verification evidence → terminal disposition

This aggregate is a product-stable Aegis domain. Runtime, transport, dashboard, queue, and persistence adapters may project their records into it, but those adapters do not define identity, authority, verification, or lifecycle truth. Model narration is never an authoritative plumbing fact.

## Aggregate ownership and identity

One aggregate has a stable ID, a positive monotonic revision, one owner, creation/update timestamps, and exactly one authenticated participant and ingress fact. Every retained fact has:

- a stable identifier unique inside the aggregate;
- the aggregate owner in its provenance;
- a producer from the closed Aegis-owned producer vocabulary (`aegis-control-plane`, `aegis-runtime-adapter`, or `aegis-verifier`);
- an opaque source or evidence reference;
- an authoritative recording timestamp.

References are causal identifiers, not display labels. Parents must already exist in the aggregate. A missing, duplicated, forward, cross-owner, or mismatched reference fails validation. Raw payloads, credentials, transcripts, and arbitrary artifact bodies are not aggregate fields. The aggregate retains bounded metadata, SHA-256 digests, and opaque references to separately controlled content.

## Authentication and ingress

A participant is a human, agent, service, device, team, or organization whose identity was authenticated outside the model. Authentication records bind issuer, method, evidence, claims digest, authentication time, and expiry.

The ingress fact binds that participant to trusted contact and channel observations. Contact IDs, channel IDs, channel kind, endpoint references, and provenance are facts established by the control plane or an authenticated adapter. Prompt content, names, bearer strings, requested stanza names, and model claims cannot create or alter them. Ingress outside the authenticated lifetime fails closed.

## Trust-stanza decision and authority context

A decision binds the participant and ingress fact to one logical agent and exact charter revision/digest. Its only valid outcomes are:

- zero matches: deny with no selected stanza;
- exactly one match: allow with that one selected stanza;
- multiple matches: deny as ambiguous with no selected stanza.

A denied decision cannot issue authority, dispatch work, start a session, execute an operation, create a request, artifact, or delivery, and must end in a denied disposition.

An allowed decision must produce exactly one immutable authority context. That context binds:

- mandate, decision, participant, agent, and selected stanza IDs;
- charter revision and digest;
- explicit runtime identity and version;
- sorted, duplicate-free capabilities, tools, memory scopes, and credential scopes;
- issue and expiry times;
- owner and Aegis-controlled provenance;
- a SHA-256 digest over the complete context except the digest field itself.

The context contains no alternate stanza and no inherited or unioned permission source. A stanza or material authority change requires a new mandate, context, dispatch, and clean session. The issued authority interval is half-open: `[IssuedAt, ExpiresAt)`. Revocation is append-only; it never rewrites the issued context. For structural validation, the authority cutoff is the earlier of `ExpiresAt` and the earliest retained revocation. Requests and starts, operations, request creation, artifact creation, delivery attempts, successful attempt completion, and successful delivery completion must all precede that cutoff. Equality with the cutoff is outside authority and fails closed.

Failure, denial, cancellation, or expiry recording may complete after the cutoff when it terminalizes work that began while authority was effective. Verification and terminal disposition may also record that cleanup; they do not create a new authority effect. A retry that can exercise authority must itself begin before the cutoff (or use a new authority context). Historical structural validation does not replace the application service's live check immediately before each authority-bearing effect: the service must evaluate the current time, query authoritative revocation state, and prevent time-of-check/time-of-use gaps before dispatching or publishing anything.

## Attempts and terminal state

Dispatch and session attempts are separate records. A session attempt requires an earlier successful dispatch attempt under the same authority context. Attempt state is monotonic:

- `requested` may become `started`, `denied`, `cancelled`, or `expired`;
- `started` may become `succeeded`, `failed`, `cancelled`, or `expired`;
- terminal states never transition.

Requested, started, and terminal timestamps must agree with state and remain ordered. Runtime attempt IDs are adapter references, not Aegis authority.

The aggregate may remain active without a disposition. Once present, terminal disposition is immutable and one of `succeeded`, `failed`, `denied`, `cancelled`, or `expired`. It has a machine-readable reason and may cite only verification evidence owned by the aggregate. A disposition requires every retained attempt and delivery to be terminal and must not predate their completion. Success requires a successfully completed session, at least one passed cited verification record, and a successful delivery for every retained artifact. Failed delivery records may coexist with a later successful retry. A non-success disposition must be supported by a corresponding failed, denied, cancelled, or expired terminal event (with a denied stanza decision itself supporting denial).

## Typed operations and requests

An operation is a closed, versioned Aegis operation under one active or successfully completed session attempt and that session's exact authority context. It binds a canonical typed-parameter digest and may carry an opaque controlled-storage reference. Free-form model output cannot become an operation.

A request belongs to one operation, has a stable replay identifier, a canonical payload digest, a bounded deadline, and optional causal parent. Parent requests must appear earlier. A request cannot broaden its operation, authority, destination, or deadline.

## Artifacts, deliveries, and verification

An artifact belongs to one request and records owner, kind, positive revision, media type, content digest, opaque content reference, creation time, and provenance. Artifact metadata does not authorize delivery.

A delivery binds one request, one artifact from that request, the exact authority context, and one explicit destination. Delivery is `pending` or terminal (`delivered`, `failed`, `denied`, or `cancelled`). Terminal delivery states never reopen.

Verification evidence binds an existing aggregate subject to a verifier from the closed Aegis-owned verifier vocabulary, pass/fail outcome, evidence digest, opaque evidence reference, observation time, and provenance. The initial artifact verifier is `aegis-artifact-verifier`. Runtime or model assertions may be evidence inputs but cannot populate authoritative producer/verifier identities, mark verification passed, or select the aggregate's disposition.

## Adapter and public-facade boundary

Adapters translate existing source reality into these records while preserving source IDs and provenance. They must not:

- copy dashboard-specific routes or persistence schemas into the public domain;
- infer authentication or authority from model/runtime text;
- merge authority from multiple stanzas or sessions;
- turn delivery success into verification success;
- mutate terminal records to hide retries or failures;
- retain credential plaintext or unrestricted payload bodies in plumbing metadata.

Retries create new attempt, request, delivery, or aggregate revisions as appropriate. Storage implementations must compare the expected prior revision before append/publication. The package validator checks the aggregate's retained structure: referential and timestamp causality, half-open historical authority boundaries, terminal consistency, digests, and provenance shape. Application services remain responsible for live wall-clock expiry and authoritative revocation lookup immediately before effects, durable optimistic concurrency, transition serialization, source freshness, adapter-specific verification, and enforcement against external systems. Passing structural validation is therefore necessary but never an authorization decision by itself.

## Implementation mapping

The transport-neutral Go vocabulary and fail-closed validator live in `internal/plumbing`. Existing `internal/core` charter, selection, mandate, and session records remain source reality for normal operational sessions. The principal-only `aegis plumbing poc` command and `POST /v1/plumbing/poc` endpoint now exercise one explicitly acknowledged, synthetic non-restrictive and non-production authority context through the plumbing facade. `aegis plumbing show GRAPH_RUN_ID` and `GET /v1/graph-runs/:id` return a run only after application readback validates one unique terminal aggregate, separately stored artifact bytes and verification evidence, delivery/disposition bindings, and authoritative audit evidence. This proof surface does not change normal runtime launch, provisioning, or activation behavior and must not be treated as a production authority policy.
