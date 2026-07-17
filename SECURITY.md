# Security Policy

## Status and supported versions

Aegis is pre-release MVP software. Until the first tagged release, only the current `main` branch receives security fixes. No released version is presently supported for production use.

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public issue, discussion, pull request, recording, or chat log. Use GitHub private vulnerability reporting from this repository's **Security** tab when it is enabled. If that facility is unavailable, stop before sending exploit details or secrets: the repository owner must publish a private security contact. That owner decision is an unresolved launch blocker; this project does not invent a contact address.

Include the affected revision, impact, prerequisites, a minimal reproduction, and whether credentials or third-party systems may have been exposed. Never include live credentials.

## Response process

A maintainer should acknowledge a private report, reproduce and classify it, coordinate a fix and regression test, prepare an advisory, and agree on disclosure timing with the reporter. Response-time commitments will be published only after maintainers establish a staffed reporting route.

## Current security boundaries

Aegis authenticates the configured local principal from OS identity, or Linux Unix-socket callers from `SO_PEERCRED`. Bearer tokens authenticate API transport only. Exact charter and plan digests bind single-use approvals. Operational sessions use fresh Hermes processes and disposable homes, and receive only explicitly resolved provider credentials and toolset launch arguments.

These controls are not a host filesystem, network, container, or VM sandbox. Hermes profiles and homes isolate runtime state, not host authority. Local audit records are hash-linked and have signed checkpoints, but are not externally tamper-proof unless checkpoint retention and append authority are deployed across separately protected boundaries. Toolset launch-argument verification is not individual-tool runtime attestation. See `docs/THREAT_MODEL.md`.
