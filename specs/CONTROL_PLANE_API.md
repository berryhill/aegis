# Control-Plane API Specification

Cobra and Echo are transports over the same application services. Policy and identity decisions MUST NOT be duplicated in handlers.

## CLI

Command results are JSON on stdout. Diagnostics and centrally rendered errors use stderr. Constructor-built Cobra trees and isolated typed configuration avoid package-level mutable state.

## HTTP API

Echo exposes liveness/readiness and protected workflow routes for runtime discovery, configuration inspection, charter design/import/validation/list/show, plan review, approval, provisioning/receipts, session preview/start/list/show/effective authority/termination, and audit inspection/verification. Principal-only audit-delivery routes expose aggregate status, bounded delivery, derived-projection verification, and explicit projection rebuild. `/readyz` is unauthenticated but returns only sanitized aggregate audit-delivery state and reports not-ready while delivery is pending, degraded, or unverifiable.

The fleet-control resource roots are `/v1/agents`, `/v1/loops`, `/v1/graphs`, and `/v1/queue`. List and detail operations expose exact immutable revision/digest references. Mutation routes call shared services that authenticate outside the model, validate the requested revision, bind exactly one authority context, and persist either a durable rejection or accepted result. Graph submission never accepts participant, Loop, stanza, mandate, or authority replacement from prompt content.

Readiness is evaluated per attempted action, not as one global onboarding boolean. Typed service and transport results distinguish `ready`, `denied`, `unavailable`, and `degraded`; `empty` is valid only for a successfully read authoritative collection. Bounded repair guidance may be returned for degraded state, but a readiness check never performs the repair. `/readyz` remains deployment health and cannot substitute for Registry publication, Graph submission, queue claim, or runtime-effect admission.

A bearer token authenticates transport only. On Linux Unix sockets, `SO_PEERCRED` supplies caller UID and Aegis maps it to a subject. Bearer-only TCP requests cannot manufacture principal identity. Optional TCP TLS requires complete certificate/key configuration and TLS 1.2 or newer.

## Operational controls

The server uses bounded bodies and headers, explicit read/write/idle/shutdown timeouts, pre-auth source limits, post-auth subject limits, request IDs, panic recovery, safe error envelopes, structured logs, stable route-template telemetry, readiness transitions, and graceful in-flight draining.

Forwarded identity headers are not trusted by default. Errors, telemetry, and configuration output must not expose credentials or private runtime paths.
