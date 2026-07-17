# Aegis Go Engineering Research and Recommendations

## Executive summary

Aegis should be built as a small, explicit Go application whose CLI and optional HTTP API invoke the same application services.

Recommended foundation:

- Go 1.26 as the initial toolchain baseline
- Cobra for CLI structure
- Viper as a configuration-input adapter, never as global application state
- Echo v5 for an optional control-plane API
- `context` for cancellation and lifecycle
- `log/slog` for structured logging
- Constructor-based dependency injection
- Strict typed configuration and deterministic validation
- Separate runtime-adapter, charter, policy, session, provisioning, and audit packages

Current upstream releases verified during research on 2026-07-17:

- Go `1.26.5`
- Cobra `v1.10.2`
- Viper `v1.21.0`
- Echo `v5.3.0`

Echo v5.3.0 declares Go 1.25 as its module language baseline. Aegis can still choose Go 1.26 for a new project while documenting its supported Go policy.

## Research method

Five independent research scopes were assigned to Hermes subagents:

- Go, Cobra, and Viper engineering
- Echo API engineering
- Security control-plane architecture
- Hermes runtime integration
- Product-architecture critique

Four substantial research outputs and one successful retry were retained under `research/`. Primary sources were independently checked through official documentation and upstream repositories.

The detailed reports contain direct citations and deeper analysis. This document consolidates the engineering decisions relevant to the Go implementation.

## 1. Application shape

Aegis should initially ship as one foreground CLI binary with an optional long-running `serve` command, unless packaging later demonstrates a need for a separate daemon binary.

The binary should expose application capabilities such as:

- Design an agent charter
- Validate and inspect charters
- Approve a charter revision
- Provision runtime-specific artifacts
- Start an authenticated stanza-bound session
- List, inspect, and revoke sessions
- Run the optional API service
- Inspect runtime adapters and versions

The CLI and API must call shared application services. Cobra command handlers and Echo handlers should translate transport input, authenticate the caller, call an application operation, and render the result. They should not contain charter, trust, authorization, or runtime lifecycle policy.

Recommended package boundaries:

- `cmd/aegis`: minimal executable entry point
- `internal/app`: application orchestration and dependency assembly
- `internal/command`: Cobra commands and CLI rendering
- `internal/config`: Viper input, typed configuration, and validation
- `internal/charter`: canonical logical-agent and trust-stanza specification
- `internal/identity`: authenticated subjects and principal mapping
- `internal/policy`: deterministic stanza and authorization decisions
- `internal/session`: mandates, session lifecycle, expiry, and revocation
- `internal/runtime`: adapter interfaces and shared runtime contracts
- `internal/runtime/hermes`: Hermes-specific adapter
- `internal/provision`: approved-artifact application and verification
- `internal/audit`: authoritative event recording
- `internal/api`: Echo routes, middleware, and transport DTOs
- `internal/version`: build provenance

Keep implementation packages under `internal` until there is a demonstrated external Go API. Go's `internal` mechanism prevents accidental imports outside the module tree and is appropriate for a security-sensitive application that is still defining its contracts.

Primary source: https://go.dev/doc/modules/layout

## 2. Cobra practices

Cobra is appropriate for Aegis, provided the project avoids the global command pattern commonly shown in introductory examples.

Required practices:

- Construct a fresh root command through a function or constructor.
- Construct subcommands explicitly and add them to the root.
- Keep flag destination values local to command construction.
- Prefer `RunE` so errors propagate to one process boundary.
- Pass the process context through `ExecuteContext`.
- Read `cmd.Context()` in handlers and application calls.
- Inject stdin, stdout, stderr, logger, version data, and service factories.
- Set `SilenceErrors` and `SilenceUsage`, then render classified errors centrally.
- Build a new command tree for each test.
- Use Cobra's controlled arguments and I/O methods for in-process tests.
- Keep help, completion, and version paths free of config, database, or network initialization.

Avoid:

- Package-level `rootCmd`
- Command registration through `init()`
- Package-level mutable flags
- `cobra.CheckErr` in application code
- `os.Exit` below the executable boundary
- Direct writes to global stdout/stderr from handlers
- Heavy shared initialization in persistent pre-run hooks
- Reusing a mutated command tree between tests

The command layer should translate a request into an application call. It should not decide whether a caller is the configured principal, whether a stanza is authorized, or whether a charter approval is valid.

Primary sources:

- https://cobra.dev/
- https://github.com/spf13/cobra
- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md

## 3. Viper practices

Viper is useful for merging flags, environment variables, files, and defaults. It should not become the configuration API used throughout Aegis.

Aegis should publish one explicit precedence contract:

1. Command-line flags
2. Environment variables
3. Explicit or discovered configuration file
4. Compiled defaults

Required practices:

- Create an independent Viper instance with `viper.New()`.
- Never use Viper's package-level singleton.
- Define one environment prefix, such as `AEGIS`.
- Define a canonical mapping from nested keys to environment names.
- Bind only known flags and settings.
- Read a single documented configuration file in the MVP.
- Strictly decode into typed Go configuration.
- Reject unknown fields.
- Validate the fully merged typed value before creating services.
- Pass typed immutable configuration into constructors.
- Keep application packages unaware of Viper.
- Redact secrets from diagnostics and effective-config output.

Important Viper traps:

- A config or environment value bound through Viper does not populate a Go variable originally bound to a Cobra flag. Decode the merged result and use that result consistently.
- `Get*` returns zero values for absent keys unless presence is checked.
- Viper does not perform an intuitive deep merge for every complex value; an override may replace a nested object.
- Empty environment variables are treated as unset unless explicitly enabled.
- Viper instances are not safe for concurrent reads and writes without synchronization.
- Global Viper state contaminates tests and commands.
- Live reload can expose partial or invalid state when consumers read directly from Viper.

The MVP should use immutable startup configuration. If live reload is added later, parse and validate a complete replacement snapshot and atomically swap only fields explicitly declared reloadable.

Primary source: https://github.com/spf13/viper

## 4. Configuration model

Configuration must be distinct from agent charters.

- Aegis configuration controls the Aegis installation: listen addresses, storage, logging, runtime discovery, authentication providers, and operational limits.
- An agent charter defines a logical agent, runtime target, trust stanzas, capabilities, scopes, and approval rules.

Do not merge these concepts into one Viper document. Charters need canonical serialization, revisioning, digest approval, and deterministic validation. Operational config needs deployment-specific precedence and secret references.

Use three validation layers:

1. CLI shape validation
   - Arguments
   - Required flags
   - Mutually exclusive options

2. Structural configuration validation
   - Unknown fields
   - Types and formats
   - Durations, URLs, paths, enums, and ranges

3. Semantic validation
   - Cross-field dependencies
   - Runtime compatibility
   - Ambiguous stanza selectors
   - Credential and tool requirements
   - Unsafe defaults
   - Listen-address or storage conflicts

Return all independent validation errors together where practical. Configuration mistakes should be ordinary errors, not panics.

## 5. Context and lifecycle

Create a signal-aware root context at the executable boundary and pass it into Cobra. Every long-running operation should accept and honor context cancellation.

Required lifecycle rules:

- Use `signal.NotifyContext` for interrupt and termination signals.
- Pass context as the first argument to operations that can block.
- Do not use context as a general dependency bag.
- Do not store one process context in arbitrary service structs.
- Give every goroutine an owner and termination condition.
- Coordinate long-running components with explicit cancellation and error propagation.
- Bound graceful shutdown.
- Treat expected cancellation during operator-requested shutdown as normal.
- Ensure partially started services are stopped if later startup steps fail.
- Run in the foreground and let systemd, launchd, containers, or another supervisor own restart policy.

Do not implement self-daemonization, PID files, internal restart loops, or unbounded shutdown.

Primary sources:

- https://pkg.go.dev/context
- https://pkg.go.dev/os/signal#NotifyContext
- https://pkg.go.dev/golang.org/x/sync/errgroup

## 6. Errors and exit codes

Errors should be returned with operation context and rendered once at the outer boundary.

Initial exit-code contract:

- `0`: success
- `1`: runtime or operational failure
- `2`: invalid command usage or invalid user-provided configuration

Add more codes only for a concrete automation requirement.

Required practices:

- Wrap causes using `%w`.
- Classify with `errors.Is` and `errors.As`, not string matching.
- Separate machine-readable categories from human messages.
- Print command results to stdout.
- Print diagnostics and logs to stderr.
- Never mix structured JSON command output with logs.
- Show usage for syntax errors, not for runtime failures.
- Call `os.Exit` only after cleanup-capable code has returned.

Primary source: https://pkg.go.dev/os#Exit

## 7. Structured logging

Use the standard `log/slog` package and inject `*slog.Logger` into services.

Required fields should include stable identifiers where applicable:

- Component
- Operation
- Event ID
- Request ID
- Session ID
- Logical-agent ID
- Stanza ID
- Runtime adapter
- Charter digest or revision
- Duration
- Decision reason

Security rules:

- Do not log tokens, passwords, private keys, raw session credentials, or complete credential-bearing config.
- Do not log full prompts or sensitive tool results by default.
- Do not serialize arbitrary structs into logs without reviewing their fields.
- Log an error at the layer that owns handling it; avoid logging and returning it repeatedly.
- Keep authoritative audit events separate from ordinary diagnostic logs.

Primary sources:

- https://pkg.go.dev/log/slog
- https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html

## 8. Dependency injection and testability

Use constructor injection rather than a global container or framework.

Inject natural boundaries:

- Logger
- Typed config
- Clock when deterministic time is needed
- Identity provider
- Charter repository
- Session repository
- Audit sink
- Runtime-adapter registry
- Provisioner
- Filesystem interface at meaningful file boundaries
- HTTP clients
- CLI I/O

Interfaces should be narrow and normally defined by the consuming package. Do not create interfaces for every concrete type preemptively, and do not pass a huge service locator through every layer.

The model/runtime integration should sit behind an explicit adapter contract. Runtime-specific facts must remain available in domain records rather than being flattened away.

## 9. Echo v5 control-plane API

Echo v5 is suitable for an optional Aegis API. The API should be secondary to the domain model, not a parallel implementation.

### Server configuration

Configure production server limits explicitly:

- `ReadHeaderTimeout`
- `ReadTimeout`
- `WriteTimeout`
- `IdleTimeout`
- `MaxHeaderBytes`
- Maximum request-body size
- Handler deadlines
- Graceful shutdown timeout

Echo's defaults are not a complete Aegis production policy. Streaming or SSE endpoints may need a separately tuned server because ordinary write timeouts can conflict with long-lived responses.

### Middleware order

A recommended outer-to-inner shape is:

1. Trusted proxy or canonicalization handling where required before routing
2. Request ID
3. OpenTelemetry
4. Structured request logging
5. Panic recovery
6. Body-size limit
7. Coarse unauthenticated rate limit
8. Authentication
9. Resource-aware authorization
10. Principal or tenant-aware rate limit
11. Handler

Ordering must be documented and tested so logs and traces observe authentication failures, rate-limit decisions, panics, and handler errors.

### Authentication and authorization

- Authenticate every non-public route.
- Authorize each requested operation and resource separately.
- Keep principal authentication separate from Hermes transport authentication.
- Return `401` for missing or invalid authentication and `403` for an authenticated identity lacking authority.
- Never treat a valid certificate, JWT, API key, or Hermes credential as blanket authorization.
- Validate token algorithm, signature, issuer, audience, lifetime, and required claims.
- Keep browser-facing authentication and service-to-service authentication distinct.

### Input handling

- Limit bodies before binding.
- Bind into transport DTOs, never directly into domain or storage types.
- Avoid multiple sources for security-sensitive fields.
- Reject unsupported content types.
- Reject unknown fields where compatibility permits.
- Perform structural and semantic validation.
- Derive identity, stanza, policy revision, approval status, and audit fields server-side.

### Errors

Use one central HTTP error handler with a stable envelope containing:

- Safe machine-readable code
- Safe user-facing message
- Request ID
- Optional non-sensitive validation details

Do not return stack traces, filesystem paths, SQL errors, panic details, framework versions, or secrets.

### TLS and mTLS

- Require HTTPS for external traffic.
- Prefer TLS 1.3.
- Permit TLS 1.2 only when compatibility policy requires it.
- Use mTLS where appropriate for service-to-service runtime identity.
- Map certificate identities to Aegis subjects explicitly.
- Continue authorization after successful certificate verification.
- If TLS terminates at a proxy, restrict direct application access and define trusted proxy headers precisely.

### Rate limiting

Use layered limits:

- Coarse source limit before authentication
- Login/account recovery limits by source and account
- Authenticated principal or tenant limits
- Route-cost limits for expensive operations
- Concurrency limits for scarce dependencies

Echo's in-memory limiter is per process and resets on restart. It is not a distributed quota system. Do not trust `X-Forwarded-For` without an explicit proxy trust model.

### Health and shutdown

Expose distinct health semantics:

- Liveness: process is making progress
- Readiness: instance should receive traffic
- Startup: slow initialization has completed, if needed

Fail readiness before draining traffic. Use bounded graceful shutdown and explicitly manage WebSockets, SSE, or hijacked connections because `http.Server.Shutdown` does not wait for them automatically.

Primary sources:

- https://echo.labstack.com/docs/
- https://github.com/labstack/echo
- https://pkg.go.dev/net/http
- https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html

## 10. OpenTelemetry

Instrument the API and runtime operations with OpenTelemetry, but keep telemetry failure from taking down request processing.

Required practices:

- Initialize the SDK and exporters explicitly.
- Set stable service name, version, and environment resource attributes.
- Use route templates rather than raw URLs for span names.
- Propagate request context into runtime adapters and downstream clients.
- Avoid high-cardinality user-controlled attributes.
- Correlate logs with trace and request IDs.
- Record errors and set span status deliberately.
- Flush tracer and meter providers during bounded shutdown.
- Filter or sample health traffic.
- Define a deliberate sampling policy.

Primary source: https://opentelemetry.io/docs/languages/go/

## 11. Runtime-adapter implications

The Hermes research identified the TUI gateway stdio protocol as the strongest local integration candidate and the authenticated Runs API as the strongest language-neutral remote candidate.

For a Go Aegis implementation:

- Prefer a Hermes subprocess with a private stdio protocol for local design and session control if the protocol contract is stable enough.
- Use a disposable `HERMES_HOME` for design sessions.
- Do not use `hermes -z` for approval-sensitive design because Hermes one-shot mode enables YOLO behavior.
- Keep Hermes identity and version visible.
- Give design sessions a narrow tool surface with no arbitrary file, terminal, MCP, plugin, or provisioning access.
- Treat Hermes profile creation as a possible provisioning result, not a design prerequisite.
- Use a separate deterministic Go provisioner after exact charter approval.
- Verify resolved Hermes tools and effective configuration after launch.

Detailed references are in `research/HERMES_RUNTIME_RESEARCH.md`.

## 12. Testing strategy

### Unit tests

- Charter canonicalization and digest stability
- Strict configuration decoding
- Configuration precedence
- Stanza matching and ambiguity denial
- Mandate issuance and expiry
- Approval invalidation after mutation
- Error classification
- Audit-event minimization

### Cobra tests

- Fresh command per test
- Controlled arguments and I/O
- Help and version without initialization
- stdout/stderr separation
- Usage versus runtime failures
- Context cancellation

### Echo tests

- Handler and middleware behavior using `httptest`
- Authentication and authorization failures
- Body limit boundaries
- Malformed and unknown input
- Middleware execution order
- Panic recovery
- Stable safe error bodies
- Trusted-proxy behavior
- Rate-limit behavior
- TLS and mTLS certificate cases
- Graceful shutdown under an in-flight request

### Runtime integration tests

- Hermes discovery and version negotiation
- Disposable design home
- No ambient profile memory or plugins
- Exact effective tool list
- Runtime termination on session revocation
- No principal state in teamwide session
- Provisioning from an approved digest only
- Effective configuration verification

### Security and quality baseline

Run at minimum:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `govulncheck ./...`
- Formatting checks
- Fuzz tests for parsers, canonicalization, and trust-boundary input

Primary source: https://go.dev/doc/security/best-practices

## 13. Release provenance

Expose a fast `--version` path containing:

- Aegis version
- Commit
- Build date
- Dirty status when available
- Go version
- Platform
- Runtime-adapter versions

Use Cobra's version support, linker-provided release values, and `runtime/debug.ReadBuildInfo` as a fallback for local or `go install` builds. Version output must not load configuration, databases, or network clients.

Primary source: https://pkg.go.dev/runtime/debug#ReadBuildInfo

## 14. Recommended first implementation sequence

1. Establish the Go module, command constructor, config loader, logger, version package, and test harness.
2. Define canonical charter, trust-stanza, identity, mandate, and audit data structures.
3. Implement strict validation and deterministic serialization.
4. Implement local principal authentication and exact stanza selection.
5. Implement session issuance, expiry, and revocation.
6. Implement the Hermes discovery and design-worker adapter.
7. Implement design-only capability restriction.
8. Implement exact charter approval.
9. Implement deterministic Hermes provisioning and verification.
10. Implement operational stanza-bound Hermes launch.
11. Add the optional Echo API over the same application services.
12. Add observability and lifecycle hardening.
13. Complete the MVP invariant and integration test suite.

## 15. Decisions to retain

- Go is the implementation language.
- Cobra, Viper, and Echo are preferred libraries.
- Cobra command trees are constructor-built, not global.
- Viper is an input adapter, not application state.
- Configuration is strict, typed, immutable after startup, and distinct from charters.
- Echo is an interface over shared services, not the policy engine.
- Hermes is the first explicit runtime adapter.
- Design sessions do not require a persistent Hermes profile.
- Runtime provisioning is deterministic and occurs only after exact charter approval.
- Every operational session binds one authenticated identity to exactly one trust stanza.
- Aegis does not union stanza authority.

## Retained detailed research

- `research/GO_CLI_VIPER_RESEARCH.md`
- `research/GO_ECHO_RESEARCH.md`
- `research/SECURITY_CONTROL_PLANE_RESEARCH.md`
- `research/HERMES_RUNTIME_RESEARCH.md`
- `research/PRODUCT_ARCHITECTURE_CRITIQUE.md`
