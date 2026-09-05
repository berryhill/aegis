# Control-Plane API Specification

Cobra and Echo are transports over the same application services. Policy and identity decisions MUST NOT be duplicated in handlers.

## CLI

Command results are JSON on stdout. Diagnostics and centrally rendered errors use stderr. Constructor-built Cobra trees and isolated typed configuration avoid package-level mutable state.

## HTTP API

Echo exposes liveness/readiness and protected workflow routes for runtime discovery, configuration inspection, charter design/import/validation/list/show, plan review, approval, provisioning/receipts, session preview/start/list/show/effective authority/termination, and audit inspection/verification. Principal-only audit-delivery routes expose aggregate status, bounded delivery, derived-projection verification, and explicit projection rebuild. `/readyz` is unauthenticated but returns only sanitized aggregate audit-delivery state and reports not-ready while delivery is pending, degraded, or unverifiable.

The fleet-control resource roots are `/v1/agents`, `/v1/loops`, `/v1/graphs`, and `/v1/queue`. List and detail operations expose exact immutable revision/digest references. Mutation routes call shared services that authenticate outside the model, validate the requested revision, bind exactly one authority context, and persist either a durable rejection or accepted result. Graph submission never accepts participant, Loop, stanza, mandate, or authority replacement from prompt content.

Fleet mutation services also accept the controller-derived registered-Agent workspace path where implemented. The transport supplies an authenticated subject and an exact sealed workspace reference; it cannot choose owner or capabilities. The service resolves the exact latest enabled Agent and permits only stable-owner Loop/Graph mutation, submission when that Agent revision is an exact Graph participant, and owner-matching Queue management. Fleet definition reads and references are shared across workspaces. Workspace submission records enter `awaiting-runtime`; a separate controller operation must bind fresh runtime authority before they become claimable. No workspace request may supply credentials, mint runtime authority, imply provisioning, or trigger automatic execution.

The authenticated manager turn route returns authoritative non-model results for complete credential count/list/search/exact-reference value reads and for generic or polite Agent-registration and credential-create intent. Credential reads execute through the same deterministic Aegis authority dispatcher as the in-process manager; an authoritative value result is explicitly marked sensitive on the principal-authenticated local manager transport, is accepted only as an authoritative `credential_value` result by the Aegis manager client, and is never model input. Agent intent names the deterministic `/agents` transaction; credential create reports no mutation because protected no-echo intake is unavailable through this endpoint. The route MUST NOT turn these boundaries into a generic 503, let model refusal veto an admitted principal operation, or forward credential-bearing text to the model. Educational, quoted, negated, multiline, and incomplete follow-up text remains non-authoritative. Conversational failures use stable `manager_runtime_unavailable`, `manager_authority_unavailable`, `manager_authority_invalid`, `manager_turn_timeout`, `manager_turn_protocol_error`, and `manager_turn_internal_error` envelopes with matching HTTP status. Clients MUST reject unknown or status-inconsistent categories and render bounded remediation rather than producer/model text.

Readiness is evaluated per attempted action, not as one global onboarding boolean. Typed service and transport results distinguish `ready`, `denied`, `unavailable`, and `degraded`; `empty` is valid only for a successfully read authoritative collection. Bounded repair guidance may be returned for degraded state, but a readiness check never performs the repair. `/readyz` remains deployment health and cannot substitute for Registry publication, Graph submission, queue claim, or runtime-effect admission.

A bearer token authenticates transport only. On Linux Unix sockets, `SO_PEERCRED` supplies caller UID and Aegis maps it to a subject. Bearer-only TCP requests cannot manufacture principal identity. Optional TCP TLS requires complete certificate/key configuration and TLS 1.2 or newer.

## Operational controls

The server uses bounded bodies and headers, explicit read/write/idle/shutdown timeouts, pre-auth source limits, post-auth subject limits, request IDs, panic recovery, safe error envelopes, structured logs, stable route-template telemetry, readiness transitions, and graceful in-flight draining.

Forwarded identity headers are not trusted by default. Errors, telemetry, and configuration output must not expose credentials or private runtime paths.
