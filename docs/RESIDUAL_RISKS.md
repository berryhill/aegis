# Residual Risk Ledger

This ledger distinguishes the control-plane behavior proved by the installed Aegis verification path from intentionally unproved or CLI-only operations. A passing test, process exit, model response, artifact, receipt, or dashboard rendering never expands authority.

## Installed console closure evidence

`scripts/verify-installed-mvi.sh` builds and extracts the native release candidate from one clean revision and invokes `scripts/verify-installed-console.sh` with explicit archive-extraction evidence. The installed console proof uses that one candidate binary to:

- validate and import one canonical charter;
- authenticate the configured principal in a real Chrome process;
- review and register one exact current-fleet Agent through `/console`;
- publish and activate one authority-bound Loop revision;
- publish one Graph revision and submit typed input;
- process the resulting Queue item through the bounded Hermes gateway adapter;
- inspect content-addressed output, verification receipt, and terminal disposition;
- stop and restart the candidate server; and
- reconstruct exact historical Agent, Loop, Graph, Queue, attempt, artifact, receipt, rejection, and disposition records after later Agent and Loop revisions.

The proof also exercises an invalid fleet-source denial, durable invalid-authority submission rejection, exact record links, reload and browser history, filtering, pagination, responsive dialog/drawer behavior, principal-password rotation, stale-session invalidation, CSP enforcement, and unexpected request/5xx detection. Credentials remain a separate non-gating journey.

## Open residual risks

| Residual risk | Current boundary | Readiness consequence | Required future evidence |
| --- | --- | --- | --- |
| Live inference-provider and model acceptance | The installed proof uses a protocol-faithful fake Hermes gateway and verifies the real bounded adapter, runtime identity, disposable home, output digest, and cleanup. It does not call a live provider or certify model semantics. | Does not block credential-independent Registry, Loop, Graph, Queue, evidence, or disposition readiness. It blocks claims of live-provider acceptance. | Opt-in provider-specific acceptance against a pinned Hermes/runtime/model combination, with exact credential custody, denial, timeout, output, and cleanup evidence. |
| General Graph execution | The worker supports the documented narrow registered single-node Graph path. | Blocks claims of general orchestration, multi-node scheduling, or distributed execution. | Multi-node dependency, retry, cancellation, recovery, and scheduler campaigns with immutable causal readback. |
| Automated Queue lifecycle | Retry, reclaim, cancellation, expiry, exhaustion, revocation, and processing are explicit principal-authenticated controller operations. | Blocks claims of an automated scheduler or autonomous lifecycle manager. | Bounded scheduler policy, restart recovery, race, lease, and terminal-outcome campaigns. |
| Dirty-store recovery and fleet-store maintenance | Fleet canonical and lifecycle stores are qualified only in the exact storage envelope documented in `specs/STORAGE.md`; projections never grant authority. | Blocks operation after unverified dirty-state evidence and blocks broader storage qualification. | Exact migration, backup, restore, dirty-reopen, projection-rebuild, and crash campaigns. |
| Credential initialization, unlock, reveal, restore, KEK administration, and arbitrary-path backup | These remain protected local host/CLI operations. The browser supports only reviewed create, rotate, revoke, exact binding, and server-selected ciphertext backup with metadata-only readback. | Does not block credential-independent fleet control. It blocks claims that the browser administers host custody or exposes secret values. | Separate custody-specific operator reviews and platform campaigns; these operations should remain outside the browser unless a new security contract is approved. |
| Provisioning, gateway/service lifecycle, reset, update, release publication, and host filesystem mutation | These remain deterministic authenticated CLI/host operations with exact plans and dedicated confirmation boundaries. Discussion or dashboard navigation is not authorization. | Does not block dashboard fleet-control readiness. It blocks claims that `/console` is a generic host administrator. | Separate installed host-operation proofs and explicit operator authorization for each external mutation class. |
| Host confinement | Hermes disposable homes and profiles isolate runtime state, not the host filesystem, process table, or network. | Blocks sandbox, container, VM, and complete least-privilege claims. | A separately designed and tested confinement boundary. |
| Audit independence | Local audit is hash-linked and checkpointed but remains under the local account unless retained across a separately protected boundary. | Blocks tamper-proof or independently witnessed audit claims. | External retention/verification with separately protected append and checkpoint custody. |
| Cross-platform persistence qualification | Release archives are built for Linux and macOS on amd64/arm64, but fleet and authority persistence qualification is narrower. | Blocks persistence-safety claims outside the exact qualified Linux envelope. | Platform/filesystem-specific durability and recovery campaigns listed in `specs/STORAGE.md`. |
| Public security-response operations | GitHub private vulnerability reporting is preferred when enabled; a separate private contact and staffed response-time commitment are not yet published. | Remains a public-launch governance blocker, not a fleet-runtime authority gate. | Repository-owner publication of the private route and response commitment. |

## Intentionally CLI-only boundaries

The dashboard is not a generic shell and must not become one. Credential custody initialization/unlock/reveal/restore/KEK administration, deterministic provisioning, gateway/systemd lifecycle, reset, update, release publication, storage maintenance, and arbitrary host-path operations remain outside `/console`. Moving any of them into the browser requires a dedicated domain contract, exact principal and authority admission, bounded typed inputs, review/execute separation where consequential, authoritative readback, negative tests, and an updated threat model.
