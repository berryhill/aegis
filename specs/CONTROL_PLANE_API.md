# Control-Plane API Specification

Cobra and Echo are transports over the same application services. Policy and identity decisions MUST NOT be duplicated in handlers.

## CLI

Command results are JSON on stdout. Diagnostics and centrally rendered errors use stderr. Constructor-built Cobra trees and isolated typed configuration avoid package-level mutable state.

## HTTP API

Echo exposes liveness/readiness and protected workflow routes for runtime discovery, configuration inspection, charter design/import/validation/list/show, plan review, approval, provisioning/receipts, session preview/start/list/show/effective authority/termination, and audit inspection/verification.

A bearer token authenticates transport only. On Linux Unix sockets, `SO_PEERCRED` supplies caller UID and Aegis maps it to a subject. Bearer-only TCP requests cannot manufacture principal identity. Optional TCP TLS requires complete certificate/key configuration and TLS 1.2 or newer.

## Operational controls

The server uses bounded bodies and headers, explicit read/write/idle/shutdown timeouts, pre-auth source limits, post-auth subject limits, request IDs, panic recovery, safe error envelopes, structured logs, stable route-template telemetry, readiness transitions, and graceful in-flight draining.

Forwarded identity headers are not trusted by default. Errors, telemetry, and configuration output must not expose credentials or private runtime paths.
