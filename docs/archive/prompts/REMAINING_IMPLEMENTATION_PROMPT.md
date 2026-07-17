# Aegis Remaining Implementation and Verification Prompt

> Historical implementation prompt, retained for provenance. The deficiency list below describes the repository before the completed MVP implementation pass and is not current status. Current behavior is documented in `README.md`, `MVP_FEATURE_SET.md`, `CHANGELOG.md`, `docs/ARCHITECTURE.md`, and `docs/THREAT_MODEL.md` and is enforced by the executable tests.

Work directly in:

the repository root

The repository contains a partially implemented Aegis MVP. Your assignment is to AUDIT, CORRECT, AND FINISH every remaining requirement in:

`FULL_IMPLEMENTATION_PROMPT.md`

Do not assume prior completion claims are accurate. Treat the implementation as untrusted until each feature and security invariant is verified through source inspection and real execution.

## Authoritative requirements

Read these files completely before changing code:

- `AGENTS.md`
- `BIG_IDEA.md`
- `MVP_FEATURE_SET.md`
- `GO_RESEARCH.md`
- `FULL_IMPLEMENTATION_PROMPT.md`
- `specs/README.md`
- Every Go file under `specs/`
- Every Markdown report under `research/`
- The current `README.md`
- Every existing implementation and test file under `cmd/` and `internal/`

Authority order:

1. `AGENTS.md`
2. `MVP_FEATURE_SET.md`
3. `FULL_IMPLEMENTATION_PROMPT.md`
4. Normative contracts under `specs/`
5. Research and existing documentation

If implementation contracts conflict with the authoritative MVP, fix them deliberately and document why.

## Existing implementation status

A partial implementation already exists. It includes some working pieces:

- Cobra command tree
- Strict Viper configuration
- Local OS identity lookup and configured-principal mapping
- Strict JSON charter decoding and canonical SHA-256 digests
- Stanza selection
- Mandates
- Exact single-use approvals
- Narrow deterministic provisioning
- Hermes discovery and process launch
- Disposable Hermes homes
- Session persistence and revocation
- Hash-linked local audit records
- A partial Echo v5 API
- Unit tests and fuzz targets

Do not rewrite correct code merely for style. Audit it, preserve sound behavior, and replace incomplete or unsafe behavior.

There may be uncommitted user work. Inspect Git status first. Do not discard, reset, overwrite, or rewrite unrelated existing changes.

## Known deficiencies that must be resolved

The following list is a starting point, not an exhaustive substitute for auditing every requirement.

### 1. Complete application and transport surface

Ensure the CLI and Echo API invoke the same application services for every required operation:

- List logical agents
- List charter revisions
- Show charter
- Validate charter without provisioning
- Show effective stanza capabilities
- Explain authorization
- Start and close design sessions
- Create and decide exact approval requests
- Preview provisioning
- Apply provisioning
- List/show plans
- List/show receipts
- Preview operational session
- Start operational session
- Inspect session
- List active and historical sessions
- Revoke session
- Terminate session
- List/filter audit events
- Verify audit chain
- List runtime adapters and versions
- Show effective redacted configuration

Do not duplicate policy in Cobra or Echo handlers.

Machine-readable API responses and JSON CLI output must use stable schemas. Keep diagnostics on stderr.

### 2. Real API caller identity

An API bearer token is transport authentication, not the principal’s identity.

Implement a real API authentication mechanism that maps authenticated evidence to an Aegis subject without treating prompt text, display names, request fields, or a bearer token label as principal identity.

A defensible MVP implementation may use one or more of:

- Unix-domain socket peer credentials for local API access
- mTLS with explicit certificate-to-subject mapping
- Another externally verifiable mechanism approved by the project rules

Requirements:

- Map the principal only from explicit configured evidence.
- Keep API transport authentication separate from Aegis subject authentication.
- Return `401` for absent or invalid authentication.
- Return `403` for authenticated but unauthorized callers.
- Do not infer the caller from the OS identity of the server process.
- Do not let an API token alone grant principal authority.
- Add adversarial tests.
- If a mutation cannot be safely authenticated through a configured API mode, fail closed and document the exact limitation.

### 3. Complete Hermes design protocol

The design workflow must be a real, authenticated, principal-only Hermes design session.

It must:

- Use a disposable `HERMES_HOME`
- Avoid `hermes -z`
- Avoid normal Hermes memories, sessions, profiles, project rules, user plugins, and ambient MCP
- Expose only design-safe capabilities
- Clearly state that it cannot provision
- Capture a structured charter proposal through a documented Hermes integration
- Return that proposal to Aegis
- Let Aegis strictly decode, validate, canonicalize, digest, and persist it
- Never allow Hermes to write arbitrary retained project artifacts
- Never permit design to invoke provisioning
- Clean up disposable state unless retention is explicitly configured
- Audit creation, proposal submission, validation, failure, and closure

Prefer the documented Hermes TUI gateway stdio protocol if stable in the installed Hermes version. Otherwise use another documented Hermes protocol and explain the decision in architecture documentation.

A workflow that merely starts Hermes and later imports an unrelated manually supplied file is insufficient.

### 4. Exact Hermes capability enforcement and verification

Before launch:

- Resolve concrete supported tools/toolsets.
- Reject unknown values and wildcards.
- Reject unsupported plugins and MCP servers.
- Ensure charter grants and runtime configuration cannot disagree.
- Ensure teamwide authority cannot include principal-only tools, memory, or credentials.

After launch:

- Query or inspect the real Hermes effective tool surface where supported.
- Compare it with the approved stanza.
- Fail and terminate the session on mismatch.
- Record the verified effective surface in the session and audit trail.

Do not claim exact individual-tool enforcement when only toolset-level enforcement exists. Model the actual Hermes control granularity honestly.

Add real adapter tests against the installed Hermes version and disposable homes.

### 5. Credential scopes

Replace the current narrow or implicit environment handling with a typed credential-resolution boundary.

Requirements:

- Every injected credential must be named in the selected stanza.
- Inject only selected credentials.
- Never inject another stanza’s credentials.
- Do not place reusable raw credentials in audit records, logs, receipts, CLI JSON, or model context.
- Provider authentication needed by Hermes must be explicitly represented and treated separately from caller authentication.
- Reject unsupported credential types rather than pretending they are enforced.
- Clean up ephemeral material.
- Add tests proving teamwide sessions cannot receive principal credentials.
- Add tests proving logs, audit, errors, and receipts do not expose values.

If secure injection requires an external provider facility unavailable in the environment, implement the narrowest real supported mechanism and report the exact external limitation.

### 6. Memory and state scopes

Implement and verify per-stanza state separation:

- Distinct Hermes home/state directory
- Distinct session history
- Distinct memory namespace
- No transcript carryover
- No plugin state carryover
- No MCP state carryover
- No tool-handle reuse
- No credential material reuse

Changing stanza must create a clean runtime context.

Persistent Hermes mappings may be optional provisioning artifacts, but the design session must never create or modify the operator’s default Hermes profile.

Add adversarial tests that plant principal-only sentinel data and prove it is absent from teamwide state and launch configuration.

### 7. Robust supervised session lifecycle

A runtime must terminate when its session or mandate:

- Expires
- Is revoked
- Is explicitly terminated
- Becomes invalid because its charter revision, stanza, runtime, or provisioning state is invalid

Do not rely solely on a later inspection command to notice expiry.

Implement a real foreground supervisor in the long-running control plane, or another durable lifecycle mechanism consistent with project rules.

Requirements:

- Context cancellation throughout
- Process-group or equivalent child-tree termination
- Protection against PID reuse
- No orphaned Hermes processes
- Bounded graceful termination followed by forced kill
- Durable state recovery after Aegis restart
- Reconciliation of persisted “running” sessions with actual processes
- Expiry scheduling
- Tests for cancellation, shutdown, restart reconciliation, revocation, and process-tree cleanup
- No goroutine leaks

Do not start a system service during tests.

### 8. Complete deterministic provisioning

Implement a typed effect engine covering classification for:

- Files created
- Files modified
- Hermes profile creation
- MCP configuration
- Plugin configuration
- Gateway start
- Service installation
- Cron creation
- External network action

The MVP may deny unsupported effect classes, but it must:

- Parse and classify them explicitly
- Show them in review
- Reject unapproved effects
- Bind the exact complete plan to approval
- Apply only supported effects through typed Go logic
- Never execute model-generated shell
- Stage and atomically publish owned artifacts where practical
- Verify effective configuration
- Emit durable receipts
- Report partial failure precisely
- Roll back Aegis-owned changes safely
- Audit preview, apply, verify, failure, and rollback
- Never touch the operator’s default Hermes profile in tests

Approval consumption and provisioning publication must have crash-consistent semantics. Do not consume approval and then silently lose all evidence of a failed operation.

### 9. Exact review and diff

Approval review must present:

- Human-readable summary
- True full diff against the previous charter revision or empty baseline
- Runtime name/version/path/adapter
- Target environment
- Complete planned effects
- Requested tools/toolsets
- Requested credential references
- Memory scopes
- Warnings
- Exact charter digest
- Exact plan digest
- Expiry and single-use semantics

Any mutation to protected fields must invalidate approval.

Add tests changing every security-significant field independently.

### 10. Selector overlap analysis

Improve charter validation and selection testing:

- Reject duplicate stanza IDs.
- Reject implicit wildcard selectors.
- Reject exact duplicate selectors.
- Detect determinable semantic overlap across selectors, including intersections across:
  - subject kind
  - subject ID
  - principal ID
  - issuer
  - claims
  - environment
  - authentication method
- If overlap cannot be proven safe statically, runtime selection must still deny multiple matches.
- Never union grants.
- Never use prompt/model content.
- Explain decisions using trusted inputs only.

Add table tests and fuzzing for overlapping selectors.

### 11. Approval transaction hardening

Approval must be:

- Fresh-principal-authenticated
- Bound to exact canonical charter digest
- Bound to exact plan digest
- Bound to runtime and runtime version
- Bound to environment
- Bound to all effects
- Expiring
- Single-use
- Atomically consumed
- Non-replayable across processes
- Crash-consistent with protected execution

Use durable locking/transaction semantics appropriate to the storage design. Test concurrent consumers in separate goroutines and, where practical, separate processes.

### 12. Audit integrity hardening

The current local hash chain is not sufficient if the same OS account can rewrite the entire log.

Implement the strongest practical MVP design:

- Append-only authoritative events
- Stable event IDs
- Machine-readable reason codes
- Hash-linked records
- Chain verification
- Sensitive-field minimization
- File locking and crash-safe append
- Signed checkpoints
- Explicit key identifier
- Checkpoint verification
- Separate checkpoint retention boundary configurable by the operator
- Detection of deletion, truncation, insertion, modification, reordering, and chain replacement after a retained checkpoint

Runtime processes must not receive ordinary update/delete authority over committed audit state. Use a separate process/user boundary, narrow audit service, or another enforceable mechanism. If the local development environment prevents a complete separate-user deployment, implement the service boundary and tests with isolated fixtures, and document the deployment requirement honestly.

Do not claim externally anchored tamper resistance when only a local hash chain exists.

### 13. Echo v5 production completion

Complete and test:

- Explicit server timeouts
- Request-body limits
- Request IDs
- Panic recovery
- Structured logging
- Authentication
- Per-resource authorization
- Pre-auth and post-auth rate limiting
- Safe error envelopes
- Health and readiness
- Bounded graceful shutdown
- Readiness transition before drain
- Trusted-proxy policy
- TLS configuration
- Optional mTLS identity mapping if selected for API authentication
- Actual OpenTelemetry instrumentation or a clearly wired no-op/provider abstraction
- Context propagation
- Stable route-template telemetry
- No secret/high-cardinality attributes
- Middleware order tests
- In-flight shutdown tests

Do not expose filesystem paths, stack traces, framework details, secrets, or internal errors.

### 14. Storage correctness

Audit all persistence for:

- Cross-process races
- Atomic writes
- Directory fsync where needed
- Strict decoding
- Immutable charter revisions
- Approval transaction correctness
- Durable receipts
- Corrupt/truncated file handling
- Safe startup recovery
- Permissions
- Symlink/path traversal attacks
- State-directory containment
- Concurrent command/API access

Add corruption, concurrency, and adversarial filesystem tests.

### 15. Complete tests

Implement real tests for every invariant in `FULL_IMPLEMENTATION_PROMPT.md`, including:

- Prompt cannot authenticate the principal
- Stanza flag cannot bypass authorization
- Unauthorized identity cannot enter principal
- Zero matches deny
- Multiple matches deny
- Grants never union
- Stanza change creates a clean context
- Teamwide cannot load principal memory, credentials, transcripts, or tools
- Design cannot provision or alter Hermes artifacts
- Unapproved charter cannot provision
- Charter mutation invalidates approval
- Consumed approval cannot replay
- Expired approval cannot be used
- Provisioning effects equal approved plan
- Launched Hermes configuration equals approved charter
- Revocation and expiry terminate authority and runtime
- Model/runtime output cannot alter audit identity, stanza, approval, or outcome
- Audit tampering is detected
- Secrets are redacted
- Cancellation and shutdown do not leak processes or goroutines

Add:

- Unit tests
- Store contract tests
- Cobra tests with a fresh command tree per test
- Echo `httptest` tests
- Middleware-order tests
- Real subprocess CLI end-to-end tests
- Real Hermes integration tests using disposable `HERMES_HOME`
- Process cleanup assertions
- Concurrency tests
- Adversarial tests

### 16. Fuzzing

Retain and improve fuzz targets for:

- Charter strict decoding
- Canonicalization
- Stanza selectors and overlap
- Approval payload binding
- Audit decoding and verification

Run each fuzz target for a meaningful bounded duration. A fuzz function that merely exists but has never been exercised is not verification.

### 17. Documentation accuracy

Update:

- `README.md`
- `AGENTS.md`
- Example config
- Example charter
- Architecture/security notes

Document:

- Exact supported Hermes versions
- Real runtime integration protocol
- Hard controls versus prompt guidance
- Configuration precedence
- Exit codes
- CLI/API workflow
- API authentication
- Tool/toolset granularity
- Credential behavior
- Session supervision
- Provisioning effect support and denial
- Audit checkpoint model
- Host-sandbox limitations
- External provider-authentication blockers

Remove or correct every overclaim.

## Implementation rules

- Go only
- Cobra CLI
- Constructor-built command trees
- No package-level mutable commands
- `RunE`
- `ExecuteContext`
- Only executable boundary calls `os.Exit`
- Isolated `viper.New()` instances
- Strict typed configuration decoded once
- Echo v5
- Shared application services
- Injected `log/slog`
- Context cancellation throughout
- stdout for results
- stderr for diagnostics
- No fake runtime success
- No model-generated provisioning shell
- No use of the operator’s normal Hermes home in tests
- No persistent design profile
- No `hermes -z`
- No gateways, services, cron, plugins, MCP installation, or external effects unless an approved isolated fixture explicitly requires them
- No retained project artifacts under `/tmp`
- Do not push
- Do not modify the operator’s default Hermes profile
- Do not silently weaken invariants
- Do not leave unexplained TODOs, placeholders, dead code, or stubs

## Required execution process

1. Inspect Git status and preserve existing work.
2. Read all required files.
3. Inventory every requirement in `FULL_IMPLEMENTATION_PROMPT.md`.
4. Build a traceability matrix:
   - Requirement
   - Implementation location
   - Tests
   - Verification command
   - Status/blocker
5. Audit the existing code against the matrix.
6. Produce a concise execution plan.
7. Implement missing functionality in coherent increments.
8. Run formatting and focused tests continuously.
9. Perform an independent security review against every invariant.
10. Exercise the actual CLI end to end.
11. Exercise the API through a real loopback or Unix-socket server.
12. Exercise the installed Hermes adapter with disposable homes.
13. Verify every spawned process terminates.
14. Update documentation only after behavior is verified.

## Mandatory final verification

Run all of the following and report exact results:

```sh
gofmt
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Also:

- Run bounded fuzz campaigns for every fuzz target.
- Build the actual binary.
- Run real subprocess CLI end-to-end tests in isolated workspaces.
- Run Echo API integration tests.
- Run the real Hermes discovery/startup/isolation/lifecycle smoke test with disposable `HERMES_HOME`.
- If model-provider authentication blocks a live turn, capture and report the exact authentic failure while still verifying discovery, startup, isolation, capability setup, and cleanup.
- Confirm no Hermes process remains.
- Confirm the operator’s normal `~/.hermes` content was not changed by tests.

Finally:

1. Create a fresh verification script under an OS-safe path matching:
   `/tmp/hermes-verify-*`
2. Have it create and clean its own isolated workspace.
3. Run the full final verification from that script.
4. Capture its real output.
5. Remove the script and all temporary artifacts.
6. Confirm removal.

## Definition of done

Do not report completion until:

- Every requirement in `FULL_IMPLEMENTATION_PROMPT.md` has an implementation and test, or a precisely identified unavoidable external blocker.
- Every known deficiency above is resolved or honestly classified as an external deployment/provider blocker.
- CLI and API use the same services.
- API caller identity does not derive from the server process or bearer-token labels.
- Hermes integration is real and explicit.
- Design returns a structured proposal through Aegis.
- Runtime capability verification compares actual effective Hermes state with the charter.
- Credentials and memory are isolated by stanza.
- Sessions are supervised and terminate on expiry/revocation.
- Approval consumption is exact, atomic, and crash-consistent.
- Provisioning is deterministic, verified, and rollback-safe.
- Audit tampering relative to retained signed checkpoints is detected.
- Every security invariant has executable tests.
- Formatting, tests, race tests, vet, vulnerability scan, fuzz campaigns, CLI E2E, API integration, Hermes smoke test, and final verification script pass.
- Documentation matches actual behavior without overclaiming.

## Final report format

Report:

1. Requirement traceability summary
2. Architecture decisions
3. Exact files changed
4. Security invariants and corresponding tests
5. Real Hermes integration behavior
6. Commands executed
7. Exact test/race/vet/vulnerability/fuzz/E2E results
8. Final verification-script output
9. Confirmation of process and temporary-file cleanup
10. Remaining external blockers, if any
11. Explicit statement of whether the complete prompt is satisfied

Do not call a partial implementation “fully complete.”
