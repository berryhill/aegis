# Aegis Specifications

This directory contains Aegis's normative, implementation-independent Markdown specifications.

## Normative specifications

- [MVP](MVP.md) — release scope, required vertical slice, and security invariants.
- [Charter](CHARTER.md) — canonical logical-agent and trust-stanza document.
- [Identity and authorization](IDENTITY_AND_AUTHORIZATION.md) — principal authentication and deterministic stanza selection.
- [Approval and provisioning](APPROVAL_AND_PROVISIONING.md) — exact review, approval, deterministic effects, receipts, and recovery.
- [Runtime and sessions](RUNTIME_AND_SESSIONS.md) — explicit Hermes integration, mandates, launch isolation, and lifecycle.
- [Built-in Aegis manager](AEGIS_MANAGER.md) — bare-command manager UX, secure prompt boundary, protected credential intake, local Ollama lifecycle, and implementation completion gates.
- [Base manager end-to-end local session](BASE_MANAGER_END_TO_END.md) — focused implementation profile for wiring the manager UI, Hermes gateway, Aegis inference proxy, pinned Ollama route, typed proposals, protected credential operations, and bounded cleanup.
- [Audit](AUDIT.md) — authoritative events, integrity, inspection, and deployment boundary.
- [Control-plane API](CONTROL_PLANE_API.md) — shared CLI/API behavior and transport security.
- [Deployment projection](DEPLOYMENT_PROJECTION.md) — post-MVP selective deployment and fleet synchronization architecture.

The production Go implementation lives under `internal/` and `cmd/aegis/`. Executable tests enforce these contracts. The pre-implementation Go contract package formerly under `specs/` is retained only for provenance under `docs/archive/go-contracts/` as non-compiled `.go.txt` files; it is not a second authoritative domain model.

When prose and code diverge, resolve the mismatch deliberately. Authority order is `AGENTS.md`, `specs/MVP.md`, the focused specifications above, and then supporting research.
