# Aegis Project Memory and Working Rules

## Project identity

Aegis is an open-source identity, trust, and session-control layer for agents running on explicit existing AI runtimes.

Core statement:

> Aegis starts authenticated, trust-stanza-bound sessions over explicitly selected agent runtimes.

Aegis must not abstract away or disguise the underlying runtime.

## Principal

- The initial principal is an explicitly configured operator.
- Principal authority must be established through authentication outside the model.
- A prompt, display name, CLI stanza flag, or model inference is never authentication.
- Only an authenticated principal may approve foundational authority or exact provisioning artifacts.

## Core concepts

- **Logical agent:** stable agent defined by a charter.
- **Runtime:** explicit underlying system such as Hermes Agent.
- **Runtime adapter:** runtime-specific discovery, design-session, provisioning, launch, and verification integration.
- **Charter:** canonical, versioned, validated, digestible specification for a logical agent.
- **Trust stanza:** one authenticated security context within a logical agent.
- **Mandate:** short-lived authority binding an authenticated identity, one stanza, one runtime, and one charter revision.
- **Session:** one runtime execution under one mandate.
- **Provisioner:** deterministic application component that applies an approved charter; it is not the design model.

## Trust-stanza invariants

- Each logical agent may have 1–N trust stanzas.
- Every stanza requires identity provenance and an authentication rule.
- Every session binds to exactly one stanza.
- Zero authorized stanza matches means deny.
- Multiple matches mean deny as ambiguous.
- Permissions from different stanzas are never unioned.
- Changing stanzas creates a clean new session.
- Prompt content cannot select or change a stanza.
- Stanzas independently scope tools, capabilities, memory, credentials, approvals, and session lifetime.

## Runtime decisions

- Hermes Agent is the first runtime adapter.
- Hermes must remain visible in the CLI, charter, session, logs, and receipts.
- A persistent Hermes profile is not required for an Aegis design session.
- Design should use an isolated/disposable Hermes execution context.
- Persistent Hermes profiles or other runtime artifacts may be provisioning results after approval.
- Hermes profiles isolate Hermes state but are not host filesystem sandboxes.
- Do not use Hermes one-shot/YOLO behavior for approval-sensitive design sessions.

## Design and provisioning boundary

- A dedicated principal-only design session helps the authenticated operator produce a charter.
- The design runtime may propose artifacts but must not provision them.
- Design sessions must not receive arbitrary shell, file-write, profile-management, plugin, MCP, credential, or provisioning authority.
- Aegis validates and renders the charter and runtime-specific plan.
- The authenticated principal approves the exact canonical charter digest.
- Any change invalidates approval.
- A separate deterministic provisioner applies the exact approved revision.
- The resulting runtime configuration must be verified before activation.

## User interaction rule

Discussion, ideation, and design requests are not authorization to modify Hermes profiles, start gateways, create cron jobs, provision agents, or change external systems.

Before consequential project actions:

- Distinguish clearly between discussion, artifact writing, provisioning, and activation.
- Do not provision or activate anything unless the authenticated principal explicitly requests it.
- Show the intended scope before applying runtime or system changes.
- Keep project artifacts inside the repository unless the user explicitly directs otherwise.
- Do not place retained Aegis research or reports in `/tmp`.

## Go engineering decisions

- Implementation language: Go.
- Preferred CLI library: Cobra.
- Preferred configuration library: Viper.
- Preferred HTTP framework: Echo v5.
- Use constructor-built Cobra command trees; no package-level mutable commands.
- Use `viper.New()`; no global Viper singleton.
- Decode configuration once into strict typed validated values.
- Keep operational configuration distinct from agent charters.
- Use `context` cancellation throughout lifecycle operations.
- Use injected `log/slog` structured loggers.
- Keep stdout for command results and stderr for diagnostics.
- Centralize error rendering and exit-code selection.
- Echo handlers and Cobra commands call shared application services.
- Run services in the foreground under external supervision.

## Initial security posture

- The model and runtime propose; they do not authorize.
- Identity, stanza selection, mandates, approvals, provisioning, and audit are controlled outside the model.
- Capability removal is stronger than prompt instructions.
- Default deny on missing, unknown, expired, or ambiguous control input.
- Cross-stanza information transfer is denied by default in the MVP.
- Audit events are emitted authoritatively by Aegis, not accepted from model narration.
- Do not claim complete zero trust, confinement, or formal least privilege before those properties are actually enforced and tested.

## MVP objective

Prove this vertical slice:

1. The configured principal authenticates.
2. Aegis visibly selects Hermes.
3. A dedicated design session defines one logical agent with 1–N stanzas.
4. The design session produces but cannot provision a canonical charter.
5. The configured principal approves the exact charter digest.
6. Aegis deterministically provisions and verifies the Hermes mapping.
7. Aegis starts clean principal and teamwide sessions with different effective authority.
8. Unauthorized escalation and cross-stanza state access fail closed.
9. Sessions expire or can be revoked.
10. Audit records reconstruct identity, charter, stanza, mandate, runtime, and provisioning provenance.

## Authoritative project reports

- `docs/product/BIG_IDEA.md` — product thesis, conceptual model, and long-term direction.
- `specs/MVP.md` — minimum viable feature set, invariants, and deferred scope.
- `specs/*.md` — normative, implementation-independent product and security specifications.
- `research/GO_RESEARCH.md` — consolidated Go, Cobra, Viper, Echo, and runtime-integration recommendations.
- `specs/DEPLOYMENT_PROJECTION.md` — selective per-deployment projection and fleet synchronization architecture.
- `research/2026-07-17-embedded-bbolt-credential-authority.md` — normative host-native bbolt credential authority, encryption, key-custody, broker, and Infisical migration specification.

Detailed retained research is under `research/`.

## Launch-asset synchronization rule

Every implementation change must include a launch-asset impact review. Before declaring the change complete, inspect every asset below, update each affected asset in the same change, and verify that every retained statement still matches the implemented and tested behavior:

- Clear root `README.md`.
- `LICENSE`.
- `SECURITY.md`.
- `CONTRIBUTING.md`.
- `CODE_OF_CONDUCT.md`.
- `CHANGELOG.md`.
- Threat model.
- Architecture diagram.
- Five-minute quickstart.
- No-key demonstration.
- Short terminal recording.
- GitHub release binaries and checksums.
- Several focused issues suitable for early contributors.

This review is required even when no asset needs an edit. Do not perform unrelated rewrites merely to touch every file; instead, record or report which assets changed and which were reviewed as unaffected. Documentation, diagrams, demonstrations, recordings, release artifacts, checksums, and contributor issues must describe the current code rather than planned or aspirational behavior.

Treat missing required assets, stale commands, unverified examples, inaccurate security claims, obsolete diagrams, recordings that no longer reproduce, and release checksums that do not match their binaries as launch blockers. Run every documented command or workflow that can be exercised locally. Never fabricate command output, demonstrations, recordings, release artifacts, checksums, issue links, or verification results. Creating GitHub releases or issues is an external action and requires the repository owner's explicit authorization; when authorization is absent, prepare accurate repository-local release/issue material and report the remaining external action clearly.

When behavior, command syntax, configuration, architecture, trust boundaries, dependencies, supported Hermes versions, installation, build, testing, security posture, or release packaging changes, update all corresponding launch assets as part of that implementation. Keep the root `README.md` concise and route detailed material to focused documents while preserving a genuine five-minute path to a successful no-key demonstration.

## Implemented MVP command surface

The working Go implementation is under `cmd/aegis` and `internal/`. Build with `go build -o aegis ./cmd/aegis`. The verified command groups are `runtime`, `config`, `design`, `charter`, `plan`, `approval`, `provision`, `session`, `audit`, and `serve`. See `README.md` and `examples/` for the executable workflow.

The Hermes adapter supports `>=0.18.0,<0.19.0`, uses safe mode and disposable homes, and treats Hermes toolsets as the MVP hard capability unit. Design uses Hermes's structured TUI-gateway stdio protocol through `--draft` or `--smoke`; it never uses one-shot mode. Provisioning is restricted to deterministic Aegis-owned artifacts under the configured state directory. These process/home controls are not a host sandbox.
