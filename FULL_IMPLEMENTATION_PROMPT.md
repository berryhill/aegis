# Aegis Full MVP Implementation Prompt

You are implementing Aegis end to end. Work directly in:

the repository root

Your assignment is to FULLY IMPLEMENT the complete minimum viable feature set. Do not stop at research, planning, interfaces, skeletons, pseudocode, TODOs, mocks presented as production behavior, or a partially wired CLI. The deliverable is a working, tested vertical slice backed by real execution.

## Required reading

Read these files before designing or changing anything:

- `AGENTS.md`
- `BIG_IDEA.md`
- `MVP_FEATURE_SET.md`
- `GO_RESEARCH.md`
- `specs/README.md`
- Every Go file under `specs/`
- Every Markdown report under `research/`

Treat `AGENTS.md` and `MVP_FEATURE_SET.md` as authoritative. Use the research as supporting evidence. If the existing Go specification is incomplete or inconsistent with the authoritative MVP, correct it deliberately and document the reason.

## Important working rules

- The implementation language is Go.
- Use Cobra for the CLI.
- Use Viper only as a configuration-input adapter; create isolated Viper instances and decode once into strict typed configuration.
- Use Echo v5 for the control-plane API.
- Use context cancellation throughout.
- Use injected `log/slog` loggers.
- Keep stdout for command results and stderr for diagnostics.
- Keep the underlying runtime explicit at all times.
- Hermes Agent is the first real runtime adapter.
- Do not hide Hermes behind a generic abstraction.
- Do not modify the operator’s existing default Hermes profile.
- Do not create persistent Hermes profiles during design.
- Use disposable, isolated Hermes homes for design and integration tests.
- Do not use `hermes -z` for approval-sensitive design sessions because one-shot mode enables YOLO behavior.
- Do not start gateways, install system services, create cron jobs, or modify external systems unless the approved test fixture explicitly calls for it.
- Never put retained project artifacts or reports in `/tmp`. Temporary test directories are fine if they are automatically cleaned.
- Do not push or force-push unless explicitly instructed.
- Do not rewrite or discard existing research.
- Do not treat model output, prompt text, display names, or CLI stanza flags as authentication.
- Do not let a runtime or design agent self-approve or expand its own authority.
- Do not silently weaken an invariant to make a test pass.

## Subagent workstreams

Use subagents heavily and in parallel for independent workstreams, including:

1. CLI/config/application architecture
2. Charter, trust-stanza, mandate, and policy model
3. Authentication and identity
4. Approval/provisioning state machine
5. Hermes runtime integration
6. Echo API
7. Persistence and tamper-evident audit
8. Security and adversarial testing
9. End-to-end testing and documentation

Subagents may research and propose changes, but you remain responsible for reviewing, integrating, compiling, and testing their output. Do not blindly merge subagent work.

## Required feature implementation

### 1. Principal authentication

- Support one explicitly configured principal.
- Implement real local authentication based on the operating-system identity.
- Bind the configured principal to an explicit local UID/user identity.
- Design the authentication interface so stronger authenticators can be added later.
- Fail closed for missing, expired, mismatched, or ambiguous authentication.
- A prompt claiming to be the principal must have no effect.
- A CLI flag must not grant principal authority.
- Record authentication success and failure in the authoritative audit trail.
- Avoid placing reusable raw credentials in logs or model context.

### 2. Explicit runtime selection

- Implement a runtime registry and a real Hermes runtime adapter.
- Discover the Hermes executable and report:
  - Runtime name
  - Runtime version
  - Executable path
  - Installation details when available
  - Adapter version
  - Supported capabilities
- Resolve runtime selection from:
  1. Explicit CLI choice
  2. Charter setting
  3. Visible configured default
- Always display the resolved runtime and selection source.
- Reject unsupported Hermes versions rather than silently degrading.

### 3. Dedicated design session

- Implement an authenticated principal-only `aegis design` workflow.
- Run the design assistant on Hermes while keeping Hermes explicit.
- Do not create a persistent named Hermes profile for design.
- Use a disposable Hermes home that is separate from `~/.hermes`.
- Do not inherit normal Hermes memories, sessions, project prompts, user plugins, MCP servers, or ambient profile state.
- Give the design runtime only the capabilities required to discuss and produce an agent charter.
- Do not give it provisioning, arbitrary shell, arbitrary filesystem write, profile management, plugin installation, MCP installation, credential management, gateway, cron, or service-management authority.
- Display clearly that the session is design-only and cannot provision.
- Persist the resulting charter draft through Aegis, not by allowing the model to write arbitrary files.
- Clean up the disposable runtime state when the design session ends, subject to explicit retention policy.

Prefer the documented Hermes TUI gateway stdio integration if it is viable and stable. If another documented Hermes integration is more appropriate after inspecting the actual installed version, document the decision and preserve equivalent lifecycle and capability restrictions. Do not substitute a fake runtime for the real adapter.

### 4. Canonical agent charter

Implement a versioned machine-readable charter containing:

- Stable logical-agent ID
- Human-readable name
- Schema version
- Charter revision
- Explicit runtime adapter and runtime
- Runtime version constraints
- Runtime-specific target/mapping
- One or more trust stanzas
- Authentication policy per stanza
- Concrete tools and capabilities per stanza
- Memory scope per stanza
- Credential scope per stanza
- Session lifetime and reauthentication policy
- Approval policy
- Information-flow policy
- Runtime-specific Hermes configuration
- Creation identity and timestamp

Requirements:

- Strict decoding
- Unknown fields rejected
- Deterministic canonical serialization
- Stable SHA-256 digest
- Complete validation
- Unsafe implicit defaults rejected
- Duplicate stanza IDs rejected
- Wildcard capabilities/tools rejected
- Ambiguous selector configurations rejected where determinable
- The charter, not the transcript, is the source of truth
- Charter revisions are immutable once approved
- Any change creates a new revision and digest

Choose and document a durable on-disk representation. JSON or YAML input is acceptable, but approval must bind a deterministic canonical representation.

### 5. One-to-many trust stanzas

- Support 1–N trust stanzas per logical agent.
- Support at least:
  - `principal`
  - `teamwide`
  - Arbitrary user-defined stanza names
- Require a stable stanza ID.
- Require explicit authentication policy.
- Scope tools, capabilities, memory, credentials, session policy, approval policy, and runtime configuration independently.
- Do not implement transitive trust.
- Do not implement stanza inheritance in the MVP.
- Names are metadata and never authentication evidence.
- Cross-stanza information flow must default to deny.

### 6. Deterministic stanza selection

- Implement selection using authenticated identity, charter policy, environment, and an optional explicit requested stanza.
- Zero matches must return a typed denial.
- More than one match must return an ambiguity denial.
- Exactly one match binds the session to that stanza.
- Never union authority from multiple stanzas.
- Never allow prompt or model content to change the stanza.
- Changing stanza requires a clean new session.
- Provide a command/API operation that explains the trusted inputs and reason for allow/deny without relying on model narration.

### 7. Authenticated session mandates

- Issue a short-lived mandate only after successful authentication and stanza selection.
- Bind it to:
  - Authenticated subject
  - Principal mapping, when applicable
  - Logical-agent ID
  - Exactly one stanza ID
  - Charter revision and digest
  - Explicit runtime and runtime configuration
  - Concrete capabilities and tools
  - Memory and credential scopes
  - Issue and expiry times
  - Unique mandate ID
  - Revocation state
- Store mandates server-side or protect them cryptographically.
- Ensure the model/runtime cannot modify or extend them.
- Support validation, expiry, explicit revocation, and termination.
- Do not implement mandate delegation.
- Fail closed if the charter revision, runtime, stanza, or mandate is no longer valid.

### 8. Clean per-stanza Hermes launch

- Start a new Hermes process or properly isolated Hermes execution context for each operational session.
- Supply only the selected stanza’s effective configuration.
- Do not reuse transcript, memory, credentials, state directory, plugin state, MCP state, or tool handles across stanzas.
- Use separate state directories where necessary.
- Make persistent Hermes profiles optional.
- Clearly distinguish Hermes state isolation from host filesystem sandboxing.
- Ensure the implementation and CLI reveal the actual Hermes process/session/profile/home in use.
- Terminate the runtime when the Aegis session is revoked or expires.

### 9. Capability restriction

- Resolve a concrete tool list before launch.
- Intersect charter-requested tools with adapter-supported tools.
- Reject unknown tools and broad wildcards.
- Disable ambient MCP servers and plugins.
- Allow MCP/plugins only when explicitly represented in the approved charter and supported by the MVP policy.
- Do not rely on prompt instructions as the only capability control.
- Verify the effective Hermes tool surface after launch where Hermes exposes that information.
- Ensure a teamwide stanza cannot receive principal-only tools, memory, or credentials.

### 10. Exact charter approval

- Implement a review and approval workflow.
- Present:
  - Human-readable summary
  - Full charter diff
  - Runtime and version
  - Target environment
  - Planned effects
  - Requested tools and credentials
  - Warnings
  - Exact charter digest
- Require fresh principal authentication for approval.
- Bind approval to the exact canonical charter digest, runtime, environment, and planned effects.
- Any mutation invalidates approval.
- Approval must expire.
- Approval must be single-use.
- Approval consumption must be atomic.
- The design agent cannot approve itself.
- Approval cannot grant authority forbidden by the charter/stanza.
- Record request, decision, consumption, and failure events.

### 11. Deterministic Hermes provisioning

- Keep provisioning in a separate application service/component from design.
- Generate a deterministic provisioning preview before applying anything.
- Classify planned effects, including:
  - Files created or modified
  - Hermes profile creation
  - MCP configuration
  - Plugin configuration
  - Gateway start
  - Service installation
  - Cron creation
  - External network action
- Deny consequential effects not included in the approved plan.
- Apply only the exact approved charter revision.
- Do not execute model-generated shell commands as the provisioning mechanism.
- Use typed Go logic and narrow Hermes CLI/API operations.
- Prefer staging and atomic publication where practical.
- Verify effective Hermes configuration after provisioning.
- Return a durable receipt listing every artifact and verification result.
- Support failure reporting and safe rollback for changes Aegis owns.
- Never touch the operator’s default Hermes profile during automated tests.

### 12. Operational session startup and visibility

Implement CLI commands and matching application services to:

- Preview a session
- Start a session
- Inspect a session
- List sessions
- Revoke a session
- Terminate a session

Before launch display:

- Authenticated identity
- Logical agent
- Selected stanza
- Charter revision and digest
- Runtime and version
- Runtime-specific target
- Effective capabilities/tools
- Memory and credential scopes
- Expiration
- Warnings

Rules:

- Privilege escalation into principal requires fresh authentication.
- Downshifting creates a clean new session.
- No safe deterministic stanza means deny.
- Do not resume a privileged session after its authentication or mandate expires.

### 13. Basic durable tamper-evident audit trail

Implement an authoritative Aegis audit system recording:

- Authentication success/failure
- Design-session creation and closure
- Charter creation and validation
- Approval request, decision, and consumption
- Provisioning preview, apply, verify, failure, and rollback
- Mandate issue, validation failure, expiry, and revocation
- Session preview, start, termination, expiry, and revocation
- Identity, agent, stanza, runtime, charter revision/digest, approval, and provisioning linkage

Requirements:

- Aegis emits events; model/runtime narratives are not authoritative events.
- Runtime processes cannot update or delete committed audit events.
- Use append-only persistence.
- Hash-link records or batches.
- Verify the chain.
- Use stable event IDs and machine-readable reason codes.
- Minimize sensitive fields.
- Do not log raw secrets, tokens, full private prompts, or complete private tool outputs.
- Provide inspection and verification commands.

### 14. Inspection and validation commands

Implement Cobra commands, and corresponding application services, for:

- List logical agents
- List charter revisions
- Show a charter
- Validate a charter without provisioning
- Show effective stanza capabilities
- Explain authorization
- List active and historical sessions
- Show a session
- Revoke a session
- Show provisioning plans and receipts
- Show audit events
- Verify the audit chain
- Show runtime adapters and versions
- Show effective configuration with secrets redacted

Commands must support useful machine-readable output where appropriate.

### 15. Go application foundation

- Create a constructor-built Cobra command tree.
- Do not use package-level mutable command variables.
- Use `RunE`.
- Use `ExecuteContext`.
- Create a fresh command tree in every test.
- Centralize error rendering and exit codes.
- Use:
  - `0` for success
  - `1` for operational failure
  - `2` for usage/configuration errors
- Only the executable boundary may call `os.Exit`.
- Use `viper.New()`, never the global singleton.
- Decode configuration once into strict typed values.
- Document precedence:
  1. CLI flags
  2. Environment
  3. Config file
  4. Defaults
- Separate operational Aegis config from agent charters.
- Use injected dependencies.
- Use `log/slog`.
- Keep CLI and Echo as transports over the same services.
- Run the API in the foreground.
- Implement bounded graceful shutdown.
- Set explicit Echo/HTTP timeouts and request limits.
- Implement request IDs, recovery, structured logging, authentication, authorization, rate limiting, safe errors, health/readiness, and OpenTelemetry-ready instrumentation.
- Do not expose internal errors, paths, stack traces, or secrets in API responses.

### 16. Security invariant tests

Implement unit, integration, adversarial, and end-to-end tests proving at least:

- Prompt text cannot authenticate the principal.
- A stanza CLI flag cannot bypass authorization.
- An unauthorized identity cannot enter principal.
- Zero stanza matches deny.
- Multiple stanza matches deny.
- Stanza capabilities are never unioned.
- Changing stanza creates a new clean runtime context.
- Teamwide sessions cannot load principal memory, credentials, transcript, or tools.
- Design sessions cannot provision or modify Hermes artifacts.
- Unapproved charters cannot provision.
- Mutated charters invalidate approval.
- Consumed approvals cannot be replayed.
- Expired approvals cannot be used.
- Provisioning effects match the approved plan.
- Launched Hermes runtime/tool configuration matches the approved charter.
- Revocation terminates further Aegis-mediated authority and the runtime process.
- Runtime/model output cannot alter audit identity, stanza, approval, or outcome fields.
- Audit-chain tampering is detected.
- Secrets are redacted from logs, output, receipts, and audit metadata.
- Context cancellation and shutdown do not leak runtime processes or goroutines.

## Testing and verification requirements

- Use real Go tests, not prose.
- Run:
  - `gofmt`
  - `go test ./...`
  - `go test -race ./...`
  - `go vet ./...`
  - `govulncheck ./...`
- Add fuzz tests for:
  - Charter decoding
  - Canonicalization
  - Stanza selectors
  - Approval payload binding
  - Audit decoding/verification
- Run real end-to-end CLI tests in isolated temporary workspaces.
- Run a real Hermes integration smoke test using a disposable `HERMES_HOME`.
- Never point tests at `~/.hermes`.
- Confirm all spawned Hermes processes terminate.
- If provider authentication prevents a live model turn, still test real Hermes discovery, startup, isolation, capabilities, session lifecycle, and expected authenticated failure. Report the exact blocker; do not fabricate success.
- Create a fresh final verification script under an OS-safe `/tmp/hermes-verify-*` path, execute it, report its actual output, and remove it afterward.
- Do not leave temporary verification artifacts behind.

## Implementation process

1. Inspect the existing repository and Git state.
2. Read all authoritative reports and specs.
3. Use subagents for independent implementation/review streams.
4. Produce a concise execution plan.
5. Implement the full vertical slice.
6. Continuously compile and test.
7. Perform an independent security review against every MVP invariant.
8. Run the complete verification suite.
9. Exercise the actual CLI end to end.
10. Update project documentation and `AGENTS.md` with verified behavior and commands.
11. Report exact files changed, real commands executed, real test output, limitations, and any remaining blockers.

## Definition of done

Do not stop until:

- Every MVP feature has a concrete implementation or an honestly reported external blocker.
- The CLI is usable end to end.
- The Hermes adapter is real.
- The API invokes the same services.
- Tests cover the security invariants.
- Formatting, tests, race tests, vet, vulnerability scan, and the final verification script pass.
- There are no unexplained TODOs, placeholders, dead code, fake success responses, or unverified claims.
