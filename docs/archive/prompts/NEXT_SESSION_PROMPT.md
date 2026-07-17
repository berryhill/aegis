# Aegis Next Session Prompt

> Historical handoff, retained for provenance. Its unfinished-work inventory is obsolete: credential resolution, API parity, production controls, atomic/recoverable provisioning, audit authorization, launch assets, packaging, and demonstrations have since been implemented. Do not use this file as current project status; use `MVP_FEATURE_SET.md`, the implementation, tests, and `CHANGELOG.md`.

You are continuing implementation work from the Aegis repository root.

Your assignment is to audit, correct, and finish the remaining requirements in:

- `AGENTS.md`
- `MVP_FEATURE_SET.md`
- `FULL_IMPLEMENTATION_PROMPT.md`
- `REMAINING_IMPLEMENTATION_PROMPT.md`
- The normative contracts under `specs/`

Treat `AGENTS.md` as highest authority. Do not assume prior completion claims are accurate.

## Current state

The repository is largely untracked and contains existing user work. Before doing anything:

1. Run `git status --short --branch`.
2. Do not reset, discard, overwrite, commit, or push existing work.
3. Read all authoritative project files and trace relevant symbols before editing.
4. Keep project artifacts inside the repository.
5. Do not modify the operator's normal `~/.hermes` profile.
6. Do not provision or activate external Hermes profiles, gateways, services, cron jobs, plugins, or MCP servers.
7. Never expose or print secrets.

Use Go 1.26 or newer from `PATH`. Install `govulncheck` outside the repository when it is not already available.

## Verified work from the previous session

The following files were changed:

- `internal/runtime/hermes/hermes.go`
- `internal/runtime/hermes/design_test.go`

The Hermes changes:

- Set `HERMES_SKIP_VERSION_CHECK=1` during discovery so `hermes --version` does not invoke update behavior.
- Set `HERMES_ENABLE_PROJECT_PLUGINS=false` during discovery.
- Set `PYTHONDONTWRITEBYTECODE=1` in disposable Hermes environments so gateway imports do not write bytecode into the installed Hermes tree.
- Added focused regression tests for these controls.

Fresh ad-hoc verification passed:

- `gofmt -d internal/runtime/hermes/hermes.go internal/runtime/hermes/design_test.go`
- `go test -count=1 ./internal/runtime/hermes`
- `go test -count=1 ./...`
- `go vet ./...`
- `go build ./cmd/aegis`

A temporary `/tmp/hermes-verify-*` script and temporary binary were removed.

## Broader verification previously completed

A final isolated verification run successfully completed:

- `gofmt`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `govulncheck ./...`
- Five-second bounded campaigns for every existing fuzz target
- Built-binary subprocess CLI workflow
- Unix-socket Echo integration over the currently implemented routes
- Real installed Hermes 0.18.2 discovery/start/revoke lifecycle using disposable state
- Process cleanup checks

The live provider-backed Hermes design turn reached the real gateway but failed authentically because no inference provider was configured:

`No inference provider configured. Run 'hermes model' to choose a provider and model, or set an API key ...`

Do not fabricate a successful live turn.

Do not terminate pre-existing Hermes processes merely to make cleanup checks pass. Detect newly created processes relative to a baseline.

The complete `~/.hermes` tree changed during verification because those pre-existing sessions update volatile state. A scoped snapshot excluding active-session, cache, log, database, and installation paths showed the normal profile/configuration remained unchanged. Be precise about this distinction.

## Known unfinished implementation

The complete prompt is not satisfied. Prioritize real implementation rather than another verification-only pass.

### 1. Typed credential-resolution boundary

- `internal/config/config.go` defines credential bindings, but they are not wired into operational or design launches.
- Every injected credential must be selected explicitly.
- Provider authentication must remain distinct from caller authentication.
- Reject unsupported credential types and missing selected credentials.
- Never expose credential values in logs, audit events, errors, receipts, CLI JSON, or model prompts.
- Add adversarial tests proving teamwide sessions cannot receive principal credentials.
- Avoid reading ambient provider credentials directly inside the Hermes adapter.

### 2. Complete CLI/API service parity

- The Echo API currently covers inspection, validation, authorization explanation, and session revocation, but lacks the complete workflow exposed by the CLI.
- Add API operations for required design, charter import, plan preview, approval request/decision, provisioning, session preview/start/terminate, effective capabilities, redacted configuration, and related list/show operations.
- Cobra and Echo must call the same application services.
- Do not duplicate policy in handlers.
- API identity must continue to come from Unix `SO_PEERCRED`; the bearer token is transport authentication only.
- Add a real full API workflow E2E test.

### 3. Hermes capability verification

- Current enforcement is honestly modeled primarily at Hermes toolset granularity.
- Inspect what Hermes 0.18.2 can report about the effective launched surface.
- Compare real effective toolsets with the approved mandate where supported.
- Terminate and fail closed on mismatch.
- Do not claim exact individual-tool enforcement unless demonstrated.

### 4. Remaining production/API requirements

- Trusted-proxy policy
- TLS configuration
- Pre-auth and post-auth rate limiting
- OpenTelemetry abstraction/instrumentation
- Middleware-order tests
- In-flight graceful-shutdown tests
- Stable route telemetry without secrets or high-cardinality attributes

### 5. Provisioning completeness

- Complete typed effect classification and explicit denial for unsupported effect classes.
- Improve exact review/diff behavior.
- Ensure atomic publication, durable failure receipts, and safe rollback for Aegis-owned artifacts.
- Preserve exact approval binding and crash-consistent evidence.

### 6. Remaining audit/deployment limitations

- Separate append authority requires a deployment process/user boundary beyond the in-process fixture.
- Externally anchored tamper resistance is not implemented; signed local checkpoints are only operator-retained evidence.
- Document these accurately rather than classifying internal unfinished code as an external blocker.

## Required working method

1. Inspect the current tree and read the relevant code before changing it.
2. Build a concise requirement-to-code/test inventory for the next coherent increment.
3. Start with the typed credential-resolution boundary unless inspection identifies a prerequisite.
4. Trace every changed symbol to all call sites and tests.
5. Make focused edits only; no drive-by refactors.
6. Add executable unit, adversarial, and integration tests.
7. Run focused tests after each increment.
8. After implementation, run:
   - `gofmt`
   - `go test ./...`
   - `go test -race ./...`
   - `go vet ./...`
   - `govulncheck ./...`
9. Run bounded fuzz campaigns for all fuzz targets.
10. Build and exercise the actual binary in an isolated workspace.
11. Exercise the complete Unix-socket API workflow.
12. Exercise real Hermes only with disposable `HERMES_HOME`.
13. Compare process baselines and ensure no newly spawned Aegis/Hermes processes remain.
14. Use a fresh OS-safe `/tmp/hermes-verify-*` script for final verification, capture its real output, then remove it and its workspace.
15. Do not call the complete prompt satisfied unless every internal requirement is implemented and tested. Report provider or deployment limitations separately and precisely.

Deliver working code and real verification output, not a plan or completion claim based only on source inspection.
