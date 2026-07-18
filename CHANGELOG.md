# Changelog

This project follows a Keep a Changelog-style structure. Development builds report version `dev`, while the release workflow injects the exact tag version.

## Unreleased

### Fixed

- Release-tag CI now compares the built CLI and adapter versions directly instead of comparing a tagged child binary with the `dev` test-process version.

## [0.1.0] - 2026-07-17

### Added

- Go/Cobra CLI and Echo v5 control-plane API over an explicit Hermes Agent adapter.
- Strict canonical charters, one-to-many trust stanzas, deterministic selection, mandates, exact single-use approvals, deterministic Aegis-owned provisioning, session lifecycle control, and hash-linked audit checkpoints.
- Disposable Hermes design and operational homes, toolset launch-argument verification, typed provider credential resolution, Unix peer-credential API identity, optional TCP TLS, pre/post-authentication rate limiting, and stable route telemetry abstraction.
- Hermetic CLI and complete Unix-socket API workflow tests, in-flight graceful-shutdown coverage, short sanitized no-key terminal recording, and bounded fuzz campaigns.
- Explicit review fields for all approval-relevant scope, complete stored-plan digest verification, injectable audit authority, and interrupted-provisioning recovery.
- Stable Semantic Versioning release enforcement, module-version detection for `go install`, and a checksum-verifying atomic `aegis update` command for supported release platforms.
- Deterministic `make release` preparation from a dirty checkout via isolated committed-source verification, signed-tag publication, and capability-restricted advisory Hermes review; invoking the target is the explicit operator authorization.
- Deployment-bound embedded bbolt credential authority with per-version envelope encryption, versioned external KEK custody, strict codecs and startup checks, no-echo principal intake, exact credential bindings, rotation, logical revocation, metadata-only inspection/audit, and consistent ciphertext backups.
- Linux pathname-socket credential broker with pre-body `SO_PEERCRED`, digest-only session capabilities, bounded deadline/request-ID replay state, exact mandate/runtime/binding reauthorization, immediate lifecycle revocation, and one bounded `github.get_repository.v1` action that applies authentication internally and returns sanitized metadata.

### Security

- Release and development builds now require Go 1.26.5 or newer, avoiding reachable standard-library vulnerabilities present in the initial Go 1.26 patch releases.
- Ambient provider credentials are excluded from Hermes launches.
- Unknown provisioning effects, wildcard authority, ambiguous stanza matches, any mutated stored plan field, replayed approvals, unsupported Hermes versions, interrupted publication, and bearer-only principal claims fail closed.
- Credential ciphertext/context mutation, wrong KEKs, unsafe authority/key-file ownership or modes, duplicate exact bindings, wrong destinations, and revoked records/versions fail closed.
- Trust stanzas now require complete policy blocks plus issuer/environment-bound identity selectors; stored canonical policy and mandate authority are rechecked, effective inspection is authenticated, narrowing requests have safe reason codes, and CLI/API denials preserve the same shared decision.

### Known limitations

- Hermes-home isolation is not host sandboxing.
- Hermes 0.18.x has no stable post-launch individual-tool enumeration used by Aegis.
- Audit append/checkpoint authority needs a separately protected deployment boundary for stronger tamper resistance.
- TCP TLS has no certificate-to-subject mapper; principal API operations require Unix peer credentials.
- The broker is not yet exposed as a verified model-visible Hermes tool because Hermes 0.18.x safe-mode bridge registration remains unresolved. Production service/runtime user provisioning, systemd unit/TPM recovery, selective fleet projections, network confinement, and Infisical migration remain external work. Operational Hermes provider credentials remain environment-backed.
