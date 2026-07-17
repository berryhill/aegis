# Approval and Provisioning Specification

## Review

Before provisioning, Aegis produces a complete typed plan and human-readable review containing:

- canonical charter and plan digests;
- full previous-revision charter diff;
- resolved Hermes runtime identity, version, executable, and adapter version;
- target environment;
- every deterministic effect and consequence;
- per-stanza toolsets, memory scopes, credential scopes, and approval semantics;
- warnings and explicit limitations.

The stored plan digest is recomputed before listing, approval, or use and binds every typed plan field.

## Approval

Only a freshly authenticated configured principal may decide an approval. Approval binds the exact charter digest, complete plan digest, runtime, environment, principal identity, issue/decision times, expiry, and single-use policy.

Changed, expired, rejected, already consumed, missing, corrupt, or mismatched authority fails closed. The design model cannot approve its proposal.

## Provisioning

Provisioning is deterministic application code, separate from Hermes design. The MVP permits only explicit create-file effects under Aegis-owned provisioned storage. Unknown effects and external, service, gateway, cron, plugin, MCP, profile, or network effects are denied.

Publication MUST reject traversal, symlink parents, replacement, and non-regular rollback targets. Artifacts are atomically created and verified against their approved digest. A receipt records status, plan and approval IDs, charter digest, artifact paths/digests, verification, timestamps, and safe failure reason.

Approval consumption and an in-progress receipt are persisted transactionally. On failure, newly published matching artifacts are rolled back. On startup, interrupted receipts become failures; matching Aegis-owned artifacts are removed, while non-matching artifacts are preserved for manual intervention.
