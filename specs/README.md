# Aegis MVP Go Specification

The normative Go contracts for every feature in `MVP_FEATURE_SET.md` live in `specs/`.

This is intentionally a specification package, not a working product implementation. It defines:

- Domain vocabulary and stable identifiers
- Service boundaries for authentication, runtime integration, design, approval, provisioning, sessions, audit, and inspection
- Exact trust-stanza, charter, mandate, and runtime-launch shapes
- Transport-neutral services shared by future Cobra and Echo layers
- Executable structural invariants and tests

## Feature mapping

| MVP feature set | Go contract |
|---|---|
| Principal authentication | `Authenticator`, `PrincipalAuthorizer` |
| Explicit runtime selection | `RuntimeRegistry`, `RuntimeAdapter`, `RuntimeSelection` |
| Dedicated design session | `Designer`, `DesignLaunchSpec`, `DesignSession` |
| Canonical agent charter | `Charter`, `CanonicalCharter`, `CharterCodec`, `CharterRepository` |
| One-to-many trust stanzas | `TrustStanza`, `AuthenticationPolicy`, `SessionPolicy` |
| Deterministic stanza selection | `StanzaSelector`, `StanzaSelectionRequest`, `StanzaDecision` |
| Authenticated session mandate | `Mandate`, `MandateIssuer` |
| Clean per-stanza runtime launch | `RuntimeLaunchSpec`, `RuntimeAdapter.Launch` |
| Capability restriction | `CapabilityResolver`, `EffectiveCapabilities` |
| Exact charter approval | `ApprovalService`, `ReviewArtifact`, `ApprovalConsumption` |
| Deterministic Hermes provisioning | `Provisioner`, `ProvisioningPlan`, `ProvisioningReceipt` |
| Session startup and visibility | `SessionService`, `SessionPreview`, `Session` |
| Basic audit trail | `AuditSink`, `AuditReader`, `AuditEvent` |
| Inspection and validation | `Inspector`, `CharterValidator`, invariant functions |
| Go application foundation | `Services`, transport-neutral interfaces |
| Security invariants | `ValidateCharter`, `ValidateDesignLaunch`, `ValidateMandate`, tests |

## Normative rules represented in the contracts

- Caller text and model output are not authentication evidence.
- Every session binds exactly one authenticated subject, logical agent, stanza, charter digest, and explicit runtime.
- Stanza selection fails closed for zero or multiple matches.
- Authority from multiple stanzas is never unioned.
- Runtime capabilities are concrete and wildcard-free before launch.
- Design sessions are read-only, non-provisioning, isolated, and free of ambient memory, plugins, and MCP.
- Approval binds an exact charter digest, runtime, and environment and is consumed atomically.
- Provisioning is separate from design and uses deterministic application logic.
- Session changes and stanza changes require clean runtime contexts.
- Cross-stanza information flow is denied in the MVP.
- Aegis, not the runtime or model, emits authoritative audit events.

## Conformance expectation

Concrete implementations should satisfy these interfaces and add adapter/storage contract suites. The current invariant tests cover only checks that can be enforced without choosing authentication, persistence, or runtime technology.

The specification deliberately does not contain:

- A fake authenticator
- A fake approval path
- A mock Hermes implementation presented as production behavior
- Cobra commands
- Viper configuration loading
- Echo routes
- Persistent storage
- Runtime provisioning side effects

Those belong to implementation phases after these contracts are reviewed.
