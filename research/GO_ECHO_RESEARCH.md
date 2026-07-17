# Aegis Research Memo: Production Echo v5 Usage in Go

**Research date:** 2026-07-17 UTC  
**Current Echo v5 release reviewed:** `github.com/labstack/echo/v5` v5.3.0  
**Scope:** Production HTTP APIs using Echo v5, Go `net/http`, OWASP guidance, and OpenTelemetry.

## Executive recommendations

1. Use `echo.StartConfig` or an explicitly configured `http.Server`; never rely on zero-value production timeouts.
2. Separate transport timeouts from handler deadlines. Tune them to endpoint behavior rather than copying one timeout everywhere.
3. Shut down from a signal-derived context, fail readiness before draining, bound the drain period, and explicitly handle WebSockets or other hijacked connections.
4. Register middleware deliberately so request IDs, telemetry, and logs observe downstream authentication failures, rate-limit decisions, panics, and handler errors.
5. Authenticate every non-public route and perform authorization at the endpoint or resource level. JWT validation must verify the algorithm, signature, issuer, audience, lifetime, and relevant claims.
6. Limit request bodies before binding, bind only into transport DTOs, and perform both syntactic and business-semantic validation.
7. Return stable, generic client errors while preserving detailed internal errors for logs and traces.
8. Require HTTPS. Prefer TLS 1.3, permit TLS 1.2 only where compatibility requires it, and use mTLS for suitable service-to-service or high-value API environments.
9. Apply both coarse pre-authentication limits and identity-aware post-authentication limits. Do not trust forwarded client-IP headers without an explicit proxy trust model.
10. Export traces and metrics through the OpenTelemetry SDK, correlate them with structured logs, control cardinality, sample deliberately, and flush providers during shutdown.
11. Keep liveness cheap and local; let readiness represent ability to receive traffic. Test all failure paths, middleware ordering, TLS, body limits, and graceful shutdown.

---

## 1. Server and timeout policy

Echo v5.3.0’s `StartConfig` constructs an `http.Server` with a default `ReadTimeout` of 30 seconds. It leaves `WriteTimeout` unset and exposes `BeforeServeFunc` specifically for configuring server timeouts and limits:

- [Echo v5 `StartConfig` source](https://github.com/labstack/echo/blob/v5.3.0/server.go)
- [Go `http.Server`](https://pkg.go.dev/net/http#Server)

Configure these fields explicitly:

| Setting | Purpose | Aegis guidance |
|---|---|---|
| `ReadHeaderTimeout` | Time allowed to receive request headers | Set a short explicit value, commonly a few seconds, to resist slow-header attacks. |
| `ReadTimeout` | Maximum time to read the entire request, including the body | Bound uploads according to the largest legitimate request and minimum supported upload speed. |
| `WriteTimeout` | Maximum response-writing duration | Set for ordinary APIs. Streaming, SSE, and large downloads require a separate policy or server. |
| `IdleTimeout` | Keep-alive idle period | Set explicitly to release idle connections without defeating normal connection reuse. |
| `MaxHeaderBytes` | Request-header size ceiling | Keep bounded; Go’s default is 1 MiB, which may be larger than the application needs. |
| Handler context deadline | Time allowed for application work | Set below the upstream proxy/client deadline and propagate it to databases and downstream calls. |

Illustrative configuration—not universal sizing:

```go
sc := echo.StartConfig{
	Address:         ":8443",
	GracefulTimeout: 25 * time.Second,
	TLSConfig:       tlsConfig,
	BeforeServeFunc: func(s *http.Server) error {
		s.ReadHeaderTimeout = 5 * time.Second
		s.ReadTimeout = 15 * time.Second
		s.WriteTimeout = 30 * time.Second
		s.IdleTimeout = 60 * time.Second
		s.MaxHeaderBytes = 256 << 10
		return nil
	},
}

if err := sc.Start(signalCtx, e); err != nil {
	e.Logger.Error("server stopped", "error", err)
}
```

Important distinctions:

- A server timeout protects connection resources; it is not a substitute for cancellation-aware handlers.
- Echo’s `ContextTimeout` middleware adds a deadline to the request context. Handlers and downstream clients must observe `c.Request().Context()`.
- Echo’s timeout example explicitly selects on `Context.Done()`:
  [Echo timeout cookbook](https://echo.labstack.com/cookbook/timeout/)
- Go documents the exact semantics and fallback relationships among `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, and `IdleTimeout`:
  [Go `http.Server` fields](https://pkg.go.dev/net/http#Server)

For SSE or indefinite streaming, do not casually apply the ordinary API `WriteTimeout`. Isolate streaming routes on a separately tuned listener/server when possible.

---

## 2. Graceful shutdown

Use `signal.NotifyContext` for `SIGTERM` and interrupt handling, then let Echo’s `StartConfig` perform bounded shutdown or call `http.Server.Shutdown` yourself.

Recommended sequence:

1. Receive the termination signal.
2. Change `/readyz` to return failure so the instance stops receiving new traffic.
3. Allow enough time for the orchestrator or load balancer to observe the readiness change.
4. Call `Shutdown` with a fresh bounded context.
5. Wait for ordinary in-flight HTTP requests.
6. Explicitly notify and drain WebSockets, SSE sessions, or hijacked connections.
7. Close background workers and data stores in dependency order.
8. Shut down OpenTelemetry providers so buffered telemetry is exported.
9. Force-close only after the total termination budget expires.

Echo’s current graceful-shutdown pattern and `GracefulTimeout` behavior are documented here:

- [Echo graceful-shutdown cookbook](https://echo.labstack.com/cookbook/graceful-shutdown/)
- [Echo v5 `StartConfig` shutdown implementation](https://github.com/labstack/echo/blob/v5.3.0/server.go)

Go’s guarantees and limitations are important:

- `Shutdown` closes listeners, then idle connections, and waits indefinitely for active connections unless its context expires.
- Server methods return `http.ErrServerClosed` during normal shutdown.
- `Shutdown` does **not** close or wait for hijacked connections such as WebSockets.
- `RegisterOnShutdown` can initiate shutdown notifications for long-lived protocols, but registered callbacks should not themselves block waiting for completion.

Source: [Go `http.Server.Shutdown`](https://pkg.go.dev/net/http#Server.Shutdown)

---

## 3. Middleware ordering

Echo v5 applies middleware in registration order on the inbound path: the first registered middleware becomes the outermost wrapper. Response/error unwinding occurs in reverse order. `Pre` middleware runs before routing; `Use` middleware runs after Echo has matched a route.

Sources:

- [Echo v5 `Pre`, `Use`, and middleware-chain source](https://github.com/labstack/echo/blob/v5.3.0/echo.go)
- [Echo middleware cookbook](https://echo.labstack.com/cookbook/middleware/)

Recommended shape:

```go
e.Pre(canonicalizationOrTrustedProxyMiddleware)

e.Use(middleware.RequestID())
e.Use(echootel.NewMiddleware("aegis-api"))
e.Use(requestLogger)
e.Use(middleware.Recover())
e.Use(middleware.BodyLimit(maxBodyBytes))
e.Use(coarseIPRateLimiter)

api := e.Group("/v1")
api.Use(authentication)
api.Use(authorization)
api.Use(principalRateLimiter)
```

Rationale:

1. **Pre-routing:** reserve for operations that must affect routing, such as path rewriting. Do not put ordinary business middleware here unnecessarily.
2. **Request ID:** make a correlation identifier available to all later middleware.
3. **OpenTelemetry:** create the server span around the rest of request processing.
4. **Request logger:** observe downstream status, latency, and returned errors.
5. **Recovery:** convert downstream panics into errors that outer logging and tracing can observe.
6. **Body limit:** reject oversized bodies before binding or expensive work.
7. **Coarse rate limiter:** constrain unauthenticated floods.
8. **Authentication and authorization:** apply at group or route scope; authorization must remain resource-aware.
9. **Principal/tenant quota:** apply after authentication when the limit key is a user, client, tenant, or token.

Avoid enabling Echo request logger’s `HandleError` without understanding its side effect: it invokes the global error handler and commits the response, preventing outer middleware from changing it. See [Echo Request Logger](https://echo.labstack.com/middleware/logger/).

Echo recovery behavior is documented at [Echo Recover middleware](https://echo.labstack.com/middleware/recover/).

---

## 4. Authentication and authorization

Authentication and authorization are separate controls:

- Authentication establishes the caller’s identity.
- Authorization decides whether that identity may perform the requested action on the specific resource.
- Every non-public endpoint must enforce access control locally, even when an API gateway also authenticates traffic.

OWASP source: [REST Security Cheat Sheet — Access Control](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html#access-control)

### Basic authentication

Echo’s Basic Auth middleware supports constant-time credential comparison. Use Basic Auth only over TLS and normally only for tightly controlled administration or service integration—not as a default public-user authentication design.

Source: [Echo Basic Auth middleware](https://echo.labstack.com/middleware/basic-auth/)

Do not embed production credentials in source. Retrieve them from a secret manager, compare secrets in constant time, rotate them, and apply throttling.

### JWT or OIDC

Echo’s JWT integration is maintained separately as `github.com/labstack/echo-jwt/v5`:

- [Echo JWT middleware](https://echo.labstack.com/middleware/jwt/)
- [Echo JWT cookbook](https://echo.labstack.com/cookbook/jwt/)

For JWT validation:

- Accept tokens through `Authorization: Bearer`, not query parameters.
- Pin or explicitly permit expected signing algorithms.
- Verify cryptographic integrity.
- Validate `iss`, `aud`, `exp`, `nbf`, and any required subject/client/tenant claims.
- Prefer asymmetric signatures when independent services validate tokens.
- Support key rotation through a controlled JWKS/key-selection policy.
- Keep access tokens short-lived; define revocation or denylisting where immediate logout is required.
- Never interpret unverified claims for authorization.
- Return `401` for missing or invalid authentication and `403` for an authenticated principal lacking permission.
- Use generic login errors and rate-limit authentication attempts by both account and network source.

OWASP references:

- [REST Security Cheat Sheet — JWT](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html#jwt)
- [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)

---

## 5. Request-body limits, binding, and validation

Install Echo’s body-limit middleware before binding. It checks both the declared `Content-Length` and the bytes actually read, preventing a client from bypassing the limit with a false header. Oversized requests produce `413 Request Entity Too Large`.

Sources:

- [Echo Body Limit middleware](https://echo.labstack.com/middleware/body-limit/)
- [Echo v5 Body Limit source](https://github.com/labstack/echo/blob/v5.3.0/middleware/body_limit.go)
- [Go `http.MaxBytesReader`](https://pkg.go.dev/net/http#MaxBytesReader)

Use endpoint-specific limits where payload sizes vary. A global ceiling should still protect every route.

Binding is parsing, not validation. Echo’s default binding precedence is:

1. Path parameters
2. Query parameters for `GET` and `DELETE`
3. Request body

Later sources overwrite earlier values. Headers are not included in `c.Bind()` and require explicit header binding.

Source: [Echo binding guide](https://echo.labstack.com/guide/binding/)

Production rules:

- Bind into dedicated request DTOs, never directly into domain or persistence structs.
- Avoid multi-source tags for security-sensitive values. Bind from exactly one source when practical.
- Explicitly reject missing or unsupported `Content-Type`; use `415 Unsupported Media Type`.
- Reject unknown JSON fields where API compatibility permits.
- Check trailing data so a valid JSON object followed by junk is not silently accepted.
- Register an Echo `Validator` and call `c.Validate(&dto)` after binding.
- Perform syntactic checks—type, format, length, range, enum membership—and semantic checks such as ownership, cross-field relationships, allowed state transitions, and date ordering.
- Prefer allowlists to denylists.
- Log repeated validation failures as a security signal, without logging sensitive submitted values.

Echo’s `Context.Validate` requires an application-supplied validator:
[Echo v5 `Context.Validate`](https://github.com/labstack/echo/blob/v5.3.0/context.go#L482-L488)

OWASP source:
[Input Validation Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Input_Validation_Cheat_Sheet.html)

---

## 6. Error handling

Echo’s model is appropriate for production: handlers and middleware return errors, and one central HTTP error handler converts them into responses.

Source: [Echo error-handling guide](https://echo.labstack.com/guide/error-handling/)

Adopt a stable error envelope containing:

- A machine-readable application code
- A safe user-facing message
- The request/correlation ID
- Optional field-validation details that do not expose internals

Recommended mapping:

| Condition | Status |
|---|---:|
| Malformed request or binding failure | `400` |
| Missing or invalid credentials | `401` |
| Authenticated but forbidden | `403` |
| Resource absent | `404` |
| Conflict or invalid state transition | `409` |
| Body too large | `413` |
| Unsupported media type | `415` |
| Semantically invalid entity, if part of the API contract | `422` |
| Rate limit exceeded | `429` |
| Unexpected internal failure | `500` |
| Temporary dependency or capacity failure | `503` |

The global handler should:

- Preserve intentional `echo.HTTPError` status codes.
- Check whether the response has already been committed.
- Return generic responses for unexpected errors.
- Log the internal error, wrapped causes, route template, principal identifier where safe, trace ID, and request ID.
- Never return stack traces, SQL errors, filesystem paths, panic values, framework versions, or secrets.
- Treat write failures after commitment as logging/telemetry events rather than attempting a second response.

OWASP recommends centralized handling that logs details server-side while returning generic client responses:
[OWASP Error Handling Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Error_Handling_Cheat_Sheet.html)

---

## 7. TLS and mTLS

All externally reachable production APIs should use HTTPS. OWASP recommends defaulting to TLS 1.3 and allowing TLS 1.2 only where compatibility requires it; TLS 1.0 and 1.1 must be disabled.

Source: [OWASP Transport Layer Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Transport_Layer_Security_Cheat_Sheet.html)

Echo’s default TLS configuration, when it creates one, currently sets a minimum of TLS 1.2. Aegis should set the policy explicitly rather than relying on that default:

```go
tlsConfig := &tls.Config{
	MinVersion:   tls.VersionTLS13,
	Certificates: []tls.Certificate{serverCert},
}
```

For environments requiring TLS 1.2 compatibility, set `MinVersion: tls.VersionTLS12` and retain Go’s secure cipher defaults rather than maintaining an unnecessary custom cipher list.

Echo source: [Echo v5 TLS configuration](https://github.com/labstack/echo/blob/v5.3.0/server.go)  
Go source: [Go `tls.Config`](https://pkg.go.dev/crypto/tls#Config)

For mTLS:

```go
tlsConfig := &tls.Config{
	MinVersion:   tls.VersionTLS13,
	Certificates: []tls.Certificate{serverCert},
	ClientCAs:    trustedClientCAs,
	ClientAuth:   tls.RequireAndVerifyClientCert,
}
```

mTLS considerations:

- Maintain a dedicated trust pool for client identities.
- Define how certificate subject or SAN values map to application principals.
- Enforce authorization after certificate verification; a valid certificate is not blanket permission.
- Plan issuance, rotation, expiration monitoring, and revocation.
- Use mTLS particularly for service-to-service APIs, administrative interfaces, or high-value same-organization clients.
- If TLS terminates at a proxy, protect the proxy-to-application hop, restrict direct access to the application, and never trust client-certificate or identity headers from arbitrary peers.
- Apply HSTS at the public TLS termination point where browser clients are involved.

---

## 8. Rate limiting

Echo supplies a token-bucket rate limiter with a default in-memory store:

[Echo Rate Limiter middleware](https://echo.labstack.com/middleware/rate-limiter/)

Use multiple dimensions where appropriate:

- Coarse source-IP limits before authentication
- Account-plus-IP limits on login and credential recovery
- Principal, API-client, or tenant limits after authentication
- Per-route cost budgets for expensive searches, exports, or write operations
- Concurrency limits for scarce dependencies
- Global distributed quotas where aggregate multi-replica enforcement is required

Return `429 Too Many Requests`; include `Retry-After` when the retry time is known. OWASP’s REST guidance identifies `429` as the appropriate status for rate-limit or DoS rejection:
[OWASP REST Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html)

Do not assume Echo’s default in-memory store provides a cluster-wide quota. It is per-process and resets with the process. For distributed enforcement, use an external atomic store or enforce the aggregate limit at a gateway while retaining local protection.

The default identifier uses `Context.RealIP()`. That is safe only after configuring the application’s proxy trust model:

- With no proxy, use `echo.ExtractIPDirect()`.
- Behind proxies, trust only known proxy ranges.
- Strip untrusted forwarding headers at the edge.
- Never select the leftmost `X-Forwarded-For` value unconditionally.

Source: [Echo IP-address and trusted-proxy guide](https://echo.labstack.com/guide/ip-address/)

Decide explicitly whether limiter-store failure is fail-open or fail-closed. Authentication and high-cost operations may warrant stricter behavior than low-risk reads.

---

## 9. Observability

Echo’s OpenTelemetry middleware supplies HTTP traces and metrics and supports explicit tracer providers, meter providers, propagators, span options, and metric attributes:

[Echo OpenTelemetry middleware](https://echo.labstack.com/middleware/open-telemetry/)

Production setup should:

- Initialize the OpenTelemetry SDK, not only the API.
- Set a stable `service.name`, deployment environment, version, and resource attributes.
- Use batched OTLP exporters rather than console exporters.
- Configure W3C Trace Context and baggage propagation as required.
- Pass request contexts to all downstream HTTP, database, queue, and RPC operations.
- Shut down tracer and meter providers with a bounded context.
- Use route templates such as `/users/:id`, not raw URLs, for span names and metric dimensions.
- Filter or heavily sample health-check traffic.
- Avoid high-cardinality attributes such as user-controlled query strings, full IDs, tokens, or unbounded error text.
- Record errors on spans and explicitly set span status to error where appropriate; `RecordError` alone does not set status.
- Monitor request rate, errors, latency, in-flight requests, body-limit rejections, rate-limit decisions, authentication failures, panic recoveries, dependency latency, and shutdown duration.
- Correlate structured logs with trace ID, span ID, request ID, route template, status, latency, and safe principal/tenant identifiers.

OpenTelemetry references:

- [OpenTelemetry Go instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/)
- [Go `otelhttp` instrumentation](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp)
- [OpenTelemetry sampling](https://opentelemetry.io/docs/concepts/sampling/)

Use head sampling for predictable low-cost volume control. If preserving rare errors or high-latency traces is important at scale, consider collector-side tail sampling while accounting for its state and operational cost.

Logging rules should follow OWASP:

- Log authentication successes/failures, authorization failures, validation abuse, rate-limit events, and security-relevant state changes.
- Do not log passwords, access tokens, raw session identifiers, private keys, or sensitive personal data.
- Sanitize carriage returns, line feeds, and delimiters to prevent log injection.
- Protect logs against unauthorized access and tampering.

Source: [OWASP Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html)

---

## 10. Health checks

Expose separate endpoints with separate meanings:

- `/livez`: Confirms that the process is making progress. Keep it cheap and local; do not fail it merely because a downstream service is temporarily unavailable.
- `/readyz`: Confirms that the instance should receive traffic. Include required dependency state and shutdown/draining state, with very short dependency deadlines.
- `/startupz`: For slow initialization or migrations, indicates that startup has completed before liveness enforcement begins.

During graceful shutdown, readiness should fail before listener shutdown.

Do not expose build secrets, dependency credentials, internal topology, stack traces, or detailed failure causes in health responses. Restrict public access through network policy, firewall rules, or a management listener. Health endpoints normally should not generate high-volume traces or ordinary access logs, but failures and state transitions should remain observable.

Kubernetes defines the operational distinction clearly: liveness failures cause restart, while readiness failures stop traffic without necessarily restarting the process.

Source: [Kubernetes liveness, readiness, and startup probes](https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/)

---

## 11. Test plan

Echo handlers and middleware can be tested with standard `net/http/httptest`; Echo v5 also includes `echotest` helpers.

Sources:

- [Echo testing guide](https://echo.labstack.com/guide/testing/)
- [Go `net/http/httptest`](https://pkg.go.dev/net/http/httptest)
- [Echo v5 `echotest`](https://pkg.go.dev/github.com/labstack/echo/v5/echotest)

Minimum production test matrix:

### Handler and contract tests

- Valid requests and every documented status.
- Malformed JSON/XML/form data.
- Missing, incorrect, and unsupported `Content-Type`.
- Unknown or duplicate fields where relevant.
- Path/query/body precedence attacks.
- Syntactic and semantic validation failures.
- Stable error envelope and request ID.
- No internal details in `4xx` or `5xx` bodies.

### Security middleware tests

- Missing, malformed, expired, not-yet-valid, wrong-issuer, wrong-audience, wrong-algorithm, and invalid-signature JWTs.
- `401` versus `403` distinction.
- Per-resource authorization and cross-tenant access attempts.
- Body exactly at the limit and one byte over it, including chunked requests and falsified `Content-Length`.
- Rate-limit boundary, refill, burst behavior, key extraction, store failure, and `429`.
- Trusted and spoofed forwarding headers.
- Panic recovery producing a generic `500` while retaining a logged/traced internal error.

### Middleware-order tests

Use sentinel middleware to record inbound and outbound execution order. Verify that:

- Request ID exists in logs and spans.
- Logger and telemetry observe authentication, validation, rate-limit, and recovered-panic errors.
- Authentication does not run for explicitly public routes.
- Health-check filtering does not suppress failure-state observability.

### Integration and lifecycle tests

- Exercise the full Echo router through `httptest.NewServer`.
- Use `httptest.NewUnstartedServer` or `NewTLSServer` for TLS configuration.
- For mTLS, test trusted, untrusted, missing, expired, and wrong-usage client certificates.
- Start a real listener, begin a slow request, trigger cancellation, and verify it completes within the shutdown budget.
- Verify new traffic is rejected or removed after readiness changes.
- Verify shutdown timeout behavior and explicit WebSocket/stream draining.
- Confirm OpenTelemetry providers flush on normal termination.

### Robustness tests

- Run `go test -race ./...`.
- Fuzz binders, validators, authentication parsers, and custom error mapping.
- Load-test slow headers, slow bodies, keep-alive churn, oversized bodies, and expensive routes.
- Ensure deadline cancellation reaches database and downstream HTTP calls.
- Test observability backpressure or exporter failure so telemetry cannot take down request handling.

---

## Production acceptance checklist

- [ ] Explicit `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes`
- [ ] Endpoint-level context deadlines propagated downstream
- [ ] Signal-driven bounded graceful shutdown
- [ ] Readiness fails before draining
- [ ] Long-lived and hijacked connections have a separate shutdown path
- [ ] Middleware ordering is documented and tested
- [ ] Authentication and resource-level authorization cover every protected route
- [ ] JWT algorithms and claims are explicitly verified
- [ ] Global and route-specific body limits are installed
- [ ] Binding uses dedicated DTOs; validation is syntactic and semantic
- [ ] Central error handler returns generic, stable responses
- [ ] TLS 1.3 preferred; TLS 1.2 allowed only by policy
- [ ] mTLS identities are mapped to authorization principals where used
- [ ] Rate-limit keys use a trusted IP or authenticated identity
- [ ] Multi-replica quota behavior is explicitly designed
- [ ] OpenTelemetry SDK, resources, exporters, propagation, sampling, and shutdown are configured
- [ ] Logs exclude credentials, tokens, sessions, and sensitive payloads
- [ ] Separate liveness, readiness, and startup semantics are implemented
- [ ] Unit, middleware-order, TLS, shutdown, race, fuzz, and load tests pass
