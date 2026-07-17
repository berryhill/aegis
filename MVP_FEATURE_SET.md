# Aegis Minimum Viable Feature Set

## MVP objective

The first Aegis release should prove one coherent vertical slice:

> A configured principal can design one logical agent for an explicit Hermes runtime, define one or more authenticated trust stanzas, approve an exact charter, and start a clean Hermes session bound to exactly one authorized stanza without creating a persistent Hermes profile during design.

The MVP should not claim to solve all agent security. It should establish a trustworthy session boundary and a deterministic design-to-launch workflow.

## 1. Principal authentication

- Support one explicitly configured principal.
- Authenticate outside the model before a principal design or operational session starts.
- Initially support a local authentication mechanism tied to the principal's OS account, with a pluggable interface for stronger authentication.
- Never accept a CLI flag, display name, prompt statement, or model conclusion as authentication.
- Fail closed when principal authentication is absent, expired, or ambiguous.

## 2. Explicit runtime selection

- Support one runtime adapter: Hermes Agent.
- Discover and display the Hermes executable, version, installation, and adapter version.
- Select the runtime by explicit flag, charter setting, or visibly displayed configured default.
- Never hide Hermes behind a generic-agent label.
- Refuse unsupported runtime versions rather than silently degrading security behavior.

## 3. Dedicated design session

- Provide a principal-only agent-design command and session mode.
- Run the design assistant on the explicit Hermes runtime.
- Do not require creation of a named Hermes profile for design.
- Isolate design from the user's ordinary Hermes memory, sessions, plugins, MCP servers, and project instructions.
- Give the design runtime read-only design and capability-discovery tools only.
- Do not give the design runtime provisioning, arbitrary file, shell, plugin, MCP, credential, or profile-management authority.
- Clearly display `Design mode: no provisioning capability`.

## 4. Canonical agent charter

- Produce one versioned, machine-readable charter for each logical agent.
- Include:
  - Stable logical-agent ID and name
  - Explicit runtime and runtime constraints
  - One or more trust stanzas
  - Authentication rule for each stanza
  - Tools and capabilities for each stanza
  - Memory scope for each stanza
  - Credential scope for each stanza
  - Session lifetime for each stanza
  - Approval requirements
  - Runtime-specific Hermes mapping
- Use deterministic serialization.
- Compute and display a charter digest.
- Reject unknown fields, invalid combinations, unsafe implicit defaults, and ambiguous stanza rules.
- Keep the charter—not the conversation transcript—as the source of truth.

## 5. One-to-many trust stanzas

- Allow each logical agent to define 1–N trust stanzas.
- Require a stable ID and explicit authentication policy for every stanza.
- Support at least:
  - `principal`
  - `teamwide`
  - User-defined stanza names
- Keep tools, capabilities, memory, credentials, and session policy independently configurable per stanza.
- Treat stanza names as metadata, not authentication evidence.
- Do not implement transitive trust or stanza inheritance in the MVP.

## 6. Deterministic stanza selection

- Bind every operational session to exactly one stanza.
- Select the stanza from authenticated identity plus an explicit request or deterministic policy.
- Zero authorized matches means deny.
- More than one valid match means deny as ambiguous.
- Never union permissions from multiple stanzas.
- Never allow a model message to change the active stanza.
- Require a new session to change stanzas.

## 7. Authenticated session mandate

- Issue a short-lived mandate after identity and stanza authorization succeed.
- Bind the mandate to:
  - Authenticated subject
  - Logical-agent ID
  - Stanza ID
  - Charter version and digest
  - Hermes runtime identity and configuration
  - Effective capabilities
  - Memory and credential scopes
  - Issue and expiry times
- Prevent the runtime or model from modifying or extending the mandate.
- Support explicit session termination and revocation.
- Do not support mandate delegation in the MVP.

## 8. Clean per-stanza runtime launch

- Start a new Hermes process or isolated Hermes execution context for each session.
- Give it only the selected stanza's effective configuration.
- Do not carry principal transcript, memory, secrets, or tool handles into a teamwide session.
- Use separate runtime state directories where needed to prevent session-history and memory collision.
- Keep persistent runtime profiles optional; use a disposable Hermes home for design sessions.
- Make the distinction between Hermes state isolation and host sandboxing explicit.

## 9. Capability restriction

- Resolve a concrete tool list before starting Hermes.
- Expose only tools declared by the selected stanza and supported by the adapter.
- Never rely on a system prompt as the only tool restriction.
- Deny broad wildcard tool selection in the MVP.
- Disable ambient MCP servers and plugins unless they are explicitly represented in the approved charter.
- Do not give the runtime direct access to another stanza's credentials or memory.

## 10. Exact charter approval

- Present a human-readable charter summary and full diff before provisioning or activation.
- Bind approval to the exact canonical charter digest.
- Record principal identity, timestamp, target runtime, target environment, and digest.
- Any charter mutation invalidates approval.
- Approval is not a blanket authorization for later revisions.
- The design agent cannot approve its own charter.

## 11. Deterministic Hermes provisioning

- Keep provisioning separate from the design runtime.
- Generate a complete preview of Hermes artifacts and actions before applying them.
- Provision only an approved charter revision.
- Create runtime-specific artifacts through deterministic application code, not model-generated shell commands.
- Verify the effective Hermes configuration after provisioning.
- Return a provisioning receipt listing created or changed artifacts.
- Do not automatically start gateways, install services, create cron jobs, or contact external systems unless those effects are explicitly included and separately approved.

## 12. Session startup and CLI visibility

- Provide an operational session command that accepts a logical agent and optional stanza.
- Display before launch:
  - Authenticated identity
  - Logical agent
  - Selected stanza
  - Charter version
  - Runtime and version
  - Runtime-specific target
  - Session expiry
- Require fresh authentication for privilege escalation into `principal`.
- Allow downshifting only by creating a clean new session.
- Default to deny when a safe stanza cannot be determined.

## 13. Basic audit trail

- Record authoritative events from Aegis rather than relying on model narration.
- Record at least:
  - Authentication success or failure
  - Design session creation
  - Charter creation and validation
  - Approval or rejection
  - Provisioning result
  - Session issuance, start, expiry, and revocation
  - Identity, stanza, runtime, and charter digest
- Keep secrets and full private prompts out of audit records by default.
- Make audit records append-only to the runtime process.
- Include stable event IDs and machine-readable reason codes.

## 14. Inspection and validation commands

- List logical agents and charter revisions.
- Inspect a logical agent's runtime and trust stanzas.
- Validate a charter without provisioning.
- Show effective stanza capabilities and runtime mapping.
- Explain why an identity is or is not authorized for a stanza.
- Show active sessions and their expiry.
- Revoke an active session.
- Show provisioning and audit receipts.

## 15. Go application foundation

- Implement the CLI in Go with Cobra.
- Construct fresh command trees through constructors; avoid package-level commands and mutable globals.
- Use Viper only to gather configuration sources.
- Decode once into strict, typed, validated configuration.
- Use an explicit configuration precedence contract.
- Use `context` cancellation throughout session and runtime lifecycle.
- Use structured `log/slog` logging with secrets redacted by construction.
- Keep stdout for command results and stderr for diagnostics.
- Centralize error rendering and exit codes.
- Make the optional Echo API call the same application services as the CLI.

## 16. MVP security invariants

The release is not complete unless tests demonstrate:

- A prompt cannot authenticate the principal.
- A CLI stanza flag cannot bypass authorization.
- An unauthorized identity cannot enter `principal`.
- A session can bind to only one stanza.
- Multiple matching stanzas fail closed.
- Stanza capabilities are never unioned.
- Changing stanza creates a clean runtime session.
- Teamwide sessions cannot load principal memory or credentials.
- The design session cannot provision or modify Hermes artifacts.
- Provisioning rejects an unapproved or changed charter.
- The actual launched runtime and effective tool list match the approved charter.
- Revocation prevents a revoked session from continuing through Aegis.
- Audit events identify the authenticated subject, stanza, runtime, and charter revision.

## Explicitly deferred

The following are not required for the first release:

- Additional agent runtimes beyond Hermes
- OPA as a mandatory dependency
- SPIFFE/SPIRE deployment
- Cross-organization federation
- Multi-principal or multi-party approval
- General capability delegation
- Agent-to-agent transitive trust
- Formal information-flow labels or taint tracking
- Automatic cross-stanza disclosure
- Shared cross-stanza memory
- Dynamic policy learned by a model
- Arbitrary third-party MCP or plugin installation during design
- Runtime migration between adapters
- Web dashboard
- Multi-tenant SaaS operation
- Hardware-backed runtime attestation
- Public transparency logs

## MVP success demonstration

A successful MVP demonstration should show:

1. The configured principal authenticates and starts an Aegis design session.
2. Aegis visibly selects Hermes as the runtime.
3. The principal designs one logical agent with `principal` and `teamwide` stanzas.
4. The design session produces a validated charter but cannot modify Hermes.
5. The principal reviews and approves the exact charter digest.
6. Aegis provisions the approved Hermes mapping and verifies it.
7. The principal starts a `principal` session and receives principal-only capabilities.
8. A separately authenticated team identity starts a clean `teamwide` session.
9. The teamwide session cannot access principal memory, credentials, or tools.
10. An attempted stanza escalation is denied and recorded.
11. The principal revokes a session and Aegis terminates its authority.
12. The audit trail reconstructs the full identity-to-runtime-to-stanza chain.

That vertical slice proves the defining Aegis concept without pretending the first release is a complete agent-security platform.
