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
