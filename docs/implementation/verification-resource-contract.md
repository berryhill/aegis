# Release verification resource and termination contract

Objective: retain every release gate while bounding verification CPU, memory, process count, output and elapsed time; always clean up owned children and close authority storage after failure.

Acceptance: inherited conservative Go defaults with valid explicit overrides preserved; Linux cgroup-supervised verification with fail-closed unavailable supervision; bounded CDP framing/events/deadlines; process-tree timeout cleanup; bounded test helpers and PTY buffers; Sync-failure close regression with no CLEAN marker; exact-head CI and verified main merge.

Non-goals: no service/laptop configuration changes, release publication, weaker signing/authority/admission gates, or claim that these source risks caused the reported desktop freeze.

Proof policy: low concurrency, bounded cgroup execution, durable proof roots. Production storage tuning is not changed merely to make tests cheaper. Launch review and actual verification evidence follow in the delivery review.

## Launch-asset impact review

- Updated CONTRIBUTING.md: the complete bounded gate and Linux/systemd/cgroup-v2 prerequisites, explicit macOS verification denial, focused supervised command, inherited defaults and limits.
- Updated CHANGELOG.md: verification resource/termination and authority-close corrections.
- Updated CI and release workflows: retain all tests, race, vet, vulnerability, archive/checksum and installed gates behind supervision. No release is published by this change.
- Updated README.md with the full-verification prerequisite; reviewed LICENSE, SECURITY.md, CODE_OF_CONDUCT.md, docs/THREAT_MODEL.md, docs/ARCHITECTURE.md and docs/QUICKSTART.md: identity, runtime, installation, trust and product CLI contracts unchanged. Verification limits are not a security sandbox or supported-platform expansion.
- Reviewed docs/DEMO_NO_KEY.md, scripts/demo-no-key.sh, docs/RECORDING.md and retained docs/assets/aegis-no-key.{typescript,timing}: no demonstration command/output change; retained capture remains explicitly historical, not fresh provider or release evidence.
- Reviewed .github/workflows/release.yml, archive/checksum verifier and docs/contributing/ISSUE_BACKLOG.md: packaging formats and owner publication boundaries unchanged; backlog is explicitly repository-local. No GitHub release, recording publication or contributor issue was created.

The installed, full-suite, race and vulnerability gates must pass on the committed
candidate before merge. A dirty-tree local gate correctly stops at provenance;
that is not passing installed verification. Exact-head CI and independent parent
review remain mandatory; this document does not attest they have completed.
