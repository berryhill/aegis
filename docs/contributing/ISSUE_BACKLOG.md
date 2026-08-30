# Early Contributor Issue Backlog

These are repository-local proposals, not remote GitHub issues.

## 1. Add mTLS certificate-to-subject mapping

Scope: add strict configured certificate identity mapping for TCP API callers without treating TLS or bearer labels as principal identity. Relevant files: `internal/config`, `internal/api`, `internal/app`. Acceptance: unknown/ambiguous certificates deny; 401/403 semantics and adversarial tests; Unix behavior unchanged. Security: never map a display name or arbitrary certificate field implicitly.

## 2. Add Hermes post-launch inspection when upstream supports it

Scope: research and implement a stable Hermes 0.18.x inspection protocol, or document upstream absence. Relevant files: `internal/runtime/hermes`, `research/HERMES_RUNTIME_RESEARCH.md`. Acceptance: compare reported toolsets to mandate and terminate on mismatch. Security: do not claim individual-tool attestation from launch arguments.

## 3. Harden provisioning paths with descriptor-relative filesystem operations

Scope: replace path-based publication with Linux `openat2`/descriptor-relative operations where available. Relevant files: `internal/store`, `internal/app`. Acceptance: race-oriented symlink tests, atomic create, fsync, rollback evidence. Security: preserve state-root containment and portability fallback denial.

## 4. Add externally retained audit checkpoint integration

Scope: define a narrow checkpoint sink and verification contract for separately protected retention. Relevant files: `internal/store`, `internal/config`, `docs/THREAT_MODEL.md`. Acceptance: replacement/truncation detected relative to retained head; no private key in runtime process. Dependency: operator-selected retention facility.

## 5. Produce and review the no-key terminal recording

Scope: run `docs/RECORDING.md`, sanitize, replay, and verify against current CLI. Acceptance: no secrets/personal paths, authentic provider failure, script and cast agree. Dependency: maintainer approval to publish generated recording.

## 6. Verify the Aegis-owned Hermes broker bridge

Scope: prove that pinned Hermes can register exactly one Aegis-owned `github.get_repository.v1` bridge from a disposable home without enabling inherited MCP, plugins, tokens, skills, or profiles. Relevant files: `internal/runtime/hermes`, `internal/credentials/broker`, `docs/CREDENTIAL_BROKER.md`. Acceptance: effective tool registration is verified before prompts; capability arrives through an inherited channel where supported; a stanza without the exact operation cannot call it; safe-mode invariants remain tested; no terminal/curl fallback. Dependency: a supported Hermes bridge-injection contract or an upstream change.

## 7. Complete cross-platform terminal interruption campaigns

Scope: port the Linux PTY coverage for SIGINT/SIGTERM, second-signal termination, EOF, ordinary/protected intake cancellation, and exact exit aliases to each supported non-Linux OS, including resize and forced child failure. Acceptance: cancellation-safe protected intake is either implemented and preflighted or fails closed before model activation; terminal echo is restored, capabilities are invalidated, children are gone, disposable state is removed, and one metadata-only receipt remains. Security: generated canaries must remain absent from captures, errors, audit, database metadata, and temporary files.

## 8. Prove descriptor-anchored layout migration on additional platforms

Scope: implement and race-test equivalents of Linux no-follow descriptor-relative migration/reset cleanup on each supported non-Linux filesystem API. Relevant files: `internal/migration`, `internal/safefs`, `docs/PATH_LAYOUT.md`. Acceptance: exact legacy defaults beneath a writable external parent cannot redirect copy or deletion; unsupported filesystems deny before mutation; same- and cross-filesystem source layouts preserve exact authority/certification bindings. Security: do not replace the current explicit unsupported-platform denial with pathname-only deletion.

## 9. Complete non-Linux rich-terminal PTY campaigns

Scope: port the production manager composer, resize, accessible/plain, approval cancellation, renderer-failure, and raw-mode restoration matrix to macOS and explicitly evaluate Windows console support. Relevant files: `internal/tui`, `internal/command`, `cmd/aegis`. Acceptance: every supported platform either passes real PTY/console subprocess tests or fails rich preflight before Hermes/model activation and retains the plain path; tmux/screen campaigns are included where available. Security: never substitute direct Hermes TTY attachment or an uninterruptible reader.

## 10. Add a production leased Aegis event-source manager

Scope: implement the currently unavailable `/watch` boundary over an existing authoritative Aegis lifecycle/control/audit event stream before considering endpoint adapters. Relevant files: `internal/slash`, `internal/app`, `internal/store`, `internal/command`. Acceptance: owner-bound IDs; exact scope/profile/rule/source-generation equivalence; bounded leases, buffers, retention, and event queries; explicit reconnect/gap/drop semantics; revocation/expiry/session cleanup; race and PTY command-path tests. Security: do not relabel polling, fixtures, or host-blind Aegis events as endpoint threat monitoring, and do not install sensors in tests.

## 11. Complete cross-platform pinentry compatibility campaigns

Scope: exercise the direct bounded Assuan client against maintained native pinentry implementations on Linux desktop variants, macOS, and supported Windows environments without adding GPG-agent or keyring coupling. Relevant files: `internal/command/pinentry*.go`, `docs/CREDENTIAL_AUTHORITY_SETUP.md`. Acceptance: fake-helper protocol campaigns remain hermetic; opt-in manual matrices cover discovery, static chrome, cancellation/close codes, display/session metadata, process-tree cancellation, and headless denial; every supported platform either passes or documents an explicit capability denial. Security: never automate the operator's real passphrase, inherit provider/token/proxy variables, accept secret argv/environment, or weaken the no-echo fallback.

## 12. Complete the remaining fleet-v1 worker lifecycle and production adapters

Scope: build on the implemented strict queue/execution/evidence/disposition records, authority-bound Loop publication and active/retired history, principal-authenticated public CLI/API vertical, reviewed dashboard Loop publication/lifecycle and Graph compose/run commands, atomic accepted-or-rejected submission, dependency-gated attempt-bounded claims, durable expired-lease retry/reclaim, queued-or-claimed cancellation, validated rebuildable queue projections, narrow single-node runtime router with bounded Hermes gateway execution, closed authenticated dashboard queue controls, and release-shaped installed fake-gateway proof. Extend the Hermes adapter beyond the current registered single-turn path, add live-provider acceptance, automated retry/reclaim/cancellation scheduling, dirty-store recovery, multi-node scheduling, Graph lifecycle application, migration/backup/recovery operations, and remaining console workflows. Relevant files: `internal/persistence/qualification`, `internal/persistence/fleet`, `internal/registry`, `internal/loop`, `internal/graph`, `internal/queue`, `internal/execution`, `internal/evidence`, `internal/disposition`, `internal/orchestration`, `internal/app`, `internal/command`, `internal/api`, and `internal/console`. Acceptance: exclusive writer-lock and reserve enforcement; clean/dirty reopen, lease recovery, cancellation, retry exhaustion, migration, backup/restore, projection-replay campaigns; exact Hermes runtime identity; and regression of the installed Registry → Loop → Graph → Queue → evidence-gated disposition proof. Security: preserve the separate session-authority database, repeat fresh admission at every runtime effect, deny alternate paths/schemas and partial readiness, never infer readiness from process liveness or persisted claims/evidence, and do not represent the installed fake-gateway Hermes proof as live-provider acceptance or complete crash-safe general Graph execution.

## 13. Route remaining online CLI resources through the authenticated daemon

Scope: replace the current deliberate `control_plane_online` denial for store-backed CLI resources with narrow typed Unix-transport clients, while retaining `aegis console` as the reference transport implementation. Relevant files: `internal/command`, `internal/api`, `internal/app`, and installed verification scripts. Acceptance: each online command uses the generated protected token file plus Unix `SO_PEERCRED`, preserves existing structured output/error classes, never opens an authoritative store in the client, and has exact daemon-unavailable and transport-substitution tests. Security: do not add a persistent Hermes gateway, generic RPC escape hatch, TCP bearer-to-principal mapping, or silent direct-store fallback.

## 14. Implement an explicitly approved gateway-owned built-in-Agent backfill

Scope: add a typed gateway-owned migration for installations whose already-running gateway predates canonical built-in `aegis` registration. Relevant files: `internal/api`, `internal/command`, `internal/orchestration`, `internal/registry`, and `internal/persistence/fleet`. Acceptance: no offline/second-writer mutation; authenticated explicit default-decline approval; exact canonical revision-1 create/readback; idempotent retry; collision denial; no gateway restart race; no persistent or ambient Hermes profile access. Security: the migration must not grant credential, stanza, mandate, queue, runtime, or model authority and must not infer approval from an existing gateway or authenticated transport alone.

## 15. Prove manager authoritative-intent and typed-failure compatibility across clients

Scope: add contract fixtures for generic/polite Agent-registration and credential-create model bypass, expertise-v2 digest visibility, unavailable protected intake, and every typed manager turn failure. Relevant files: `internal/managergateway`, `internal/api`, `internal/command`, and manager client documentation. Acceptance: recognized create values and high-confidence credential material do not reach the model; arbitrary-text detection is explicitly not claimed as complete DLP; no protected create is claimed through the turn endpoint; unknown/status-inconsistent failures deny; CLI remediation is stable; `/agents` remains discoverable in conversational and degraded modes. Security: fixtures use generated canaries only and never serialize producer diagnostics as authoritative guidance.

### Execution Queue lifecycle status

Manual principal-authenticated retry/reclaim/cancel/expire/exhaust/revoke operations, bounded lease/backoff/attempt controls, distinct terminal outcomes, and complete queue-history readback are implemented in the issue #129 candidate. The issue #152 candidate additionally exposes process, expired-lease reclaim, and terminal operations through closed authenticated dashboard forms with server-owned authority and identifiers plus distinct fail-closed denials. Remaining follow-on scope is automated lifecycle scheduling, dirty-store recovery, migration/backup recovery, distributed and multi-node scheduling, Graph lifecycle application, and live-provider acceptance. Do not re-open implemented manual controls as scheduler work, and do not describe absent automation as shipped.
