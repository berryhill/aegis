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
