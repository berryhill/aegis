# Security Policy

## Status and supported versions

Aegis is pre-release MVP software. Until the first tagged release, only the current `main` branch receives security fixes. No released version is presently supported for production use.

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public issue, discussion, pull request, recording, or chat log. Use GitHub private vulnerability reporting from this repository's **Security** tab when it is enabled. If that facility is unavailable, stop before sending exploit details or secrets: the repository owner must publish a private security contact. That owner decision is an unresolved launch blocker; this project does not invent a contact address.

Include the affected revision, impact, prerequisites, a minimal reproduction, and whether credentials or third-party systems may have been exposed. Never include live credentials.

## Response process

A maintainer should acknowledge a private report, reproduce and classify it, coordinate a fix and regression test, prepare an advisory, and agree on disclosure timing with the reporter. Response-time commitments will be published only after maintainers establish a staffed reporting route.

## Current security boundaries

Aegis authenticates the configured local principal from OS identity, or Linux Unix-socket callers from `SO_PEERCRED`. Bearer tokens authenticate API transport only. Exact charter and plan digests bind single-use approvals. Operational sessions use fresh Hermes processes and disposable homes, and receive only explicitly resolved environment-backed provider credentials and toolset launch arguments.

The optional local credential authority encrypts each immutable value before bbolt persistence with a fresh XChaCha20-Poly1305 DEK and wraps each DEK under a versioned KEK held outside the database. Administrative CLI operations require the configured OS principal, use no-echo or exact-stdin intake, return metadata only, enforce strict file ownership/modes and exact deployment bindings, and emit metadata-only audit events. The host-file KEK mode is a weaker development fallback. Production custody requires a separately configured encrypted systemd service credential and tested recovery material.

These controls are not a host filesystem, network, container, or VM sandbox. bbolt does not provide encryption or RBAC, Go does not guarantee plaintext-memory zeroization, logical revocation does not erase backups/free pages, and local root can inspect usable plaintext. The authority is not yet connected to a runtime broker or selective deployment projections; Hermes still uses environment-backed provider bindings. Hermes profiles and homes isolate runtime state, not host authority. Local audit records are hash-linked and have signed checkpoints, but are not externally tamper-proof unless checkpoint retention and append authority are deployed across separately protected boundaries. Toolset launch-argument verification is not individual-tool runtime attestation. See `docs/THREAT_MODEL.md`.

The self-updater trusts GitHub's release API and HTTPS delivery for this repository, then requires the downloaded archive to match that release's `SHA256SUMS` entry before an atomic same-directory replacement. A checksum delivered beside an archive is corruption and consistency protection, not an independent signature or transparency proof. Package-manager installations should be updated with their package manager.
