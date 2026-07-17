# Changelog

This project follows a Keep a Changelog-style structure. It has no tagged release yet; development builds report version `dev`, while the release workflow injects the exact tag version.

## Unreleased

### Added

- Go/Cobra CLI and Echo v5 control-plane API over an explicit Hermes Agent adapter.
- Strict canonical charters, one-to-many trust stanzas, deterministic selection, mandates, exact single-use approvals, deterministic Aegis-owned provisioning, session lifecycle control, and hash-linked audit checkpoints.
- Disposable Hermes design and operational homes, toolset launch-argument verification, typed provider credential resolution, Unix peer-credential API identity, optional TCP TLS, pre/post-authentication rate limiting, and stable route telemetry abstraction.
- Hermetic CLI and complete Unix-socket API workflow tests, in-flight graceful-shutdown coverage, short sanitized no-key terminal recording, and bounded fuzz campaigns.
- Explicit review fields for all approval-relevant scope, complete stored-plan digest verification, injectable audit authority, and interrupted-provisioning recovery.

### Security

- Ambient provider credentials are excluded from Hermes launches.
- Unknown provisioning effects, wildcard authority, ambiguous stanza matches, any mutated stored plan field, replayed approvals, unsupported Hermes versions, interrupted publication, and bearer-only principal claims fail closed.

### Known limitations

- Hermes-home isolation is not host sandboxing.
- Hermes 0.18.x has no stable post-launch individual-tool enumeration used by Aegis.
- Audit append/checkpoint authority needs a separately protected deployment boundary for stronger tamper resistance.
- TCP TLS has no certificate-to-subject mapper; principal API operations require Unix peer credentials.
