# Aegis Architecture Critique

**Status:** Retained pre-implementation critique. Its open questions and illustrative recommendations are historical analysis, not current guidance; current requirements are defined by `AGENTS.md` and `specs/`.

## Overall assessment

Aegis has a promising core: separate **design-time intent** from **runtime authority**, make trust configuration explicit, and require review before provisioning. The concept is not yet precise enough to support strong security claims. Its key terms—agent, session, principal, trust stanza, authentication, and provisioning—need operational definitions and enforceable invariants.

## Critical ambiguities

### “Explicit agent runtime”

Clarify whether this means:

- a dedicated process, container, VM, or merely an application context;
- a stable runtime identity or an ephemeral session identity;
- an enforcement boundary or only an orchestration abstraction;
- who launches, updates, observes, and terminates it.

If the runtime shares credentials, filesystem, network namespace, or control plane with other agents, “explicit” does not imply isolation.

### “One logical agent”

A logical agent may still contain a model, planner, tools, subprocesses, delegated agents, retries, and concurrent sessions. Define:

- what shares identity, state, credentials, and audit history;
- whether multiple runtime replicas can represent one logical agent;
- whether delegated work inherits authority;
- whether session state can cross boundaries.

“One agent” is an accounting model, not automatically a security boundary.

### “1–N authenticated trust stanzas bound per session”

“Trust stanza” is underspecified. Each stanza should explicitly identify:

- issuer and subject;
- permitted resources and actions;
- constraints such as tenant, environment, time, network, and purpose;
- credential or proof type;
- issuance, expiry, rotation, and revocation semantics;
- conflict and composition rules;
- audit attribution.

The most important unresolved question is how multiple stanzas compose. Union semantics can silently escalate privilege; intersection semantics may be unusable. Precedence, denial behavior, and ambiguous overlaps must be deterministic.

“Bound per session” also requires a precise session identifier, anti-replay guarantees, maximum lifetime, reauthentication rules, and behavior after policy or principal changes.

### “Principal-only design session”

This phrase could mean:

1. only a human principal may participate;
2. only the principal’s identity is available;
3. no runtime credentials or side effects are permitted;
4. no agent or administrator can alter the design.

These are materially different guarantees. Also define who may review, approve, amend, and provision an artifact—and whether those roles must be distinct.

### “Reviewable artifacts before provisioning”

Specify the canonical artifact, not merely a rendered view. It should include:

- requested capabilities and trust stanzas;
- resource targets and environmental assumptions;
- generated runtime configuration;
- artifact and policy versions;
- immutable digest;
- approver identity and decision;
- provisioning result and resulting runtime identity.

Provisioning must consume exactly the reviewed artifact. Otherwise the review is vulnerable to drift or substitution.

## Likely security overclaims

Avoid claiming that Aegis is “secure,” “zero trust,” or “least privilege” merely because trust is explicit and reviewed.

- **Authentication is not authorization.** A valid stanza can still grant excessive access.
- **Session binding is not confinement.** A compromised runtime may exfiltrate data or misuse legitimate authority during the session.
- **Human review is not correctness.** Reviewers may miss dangerous combinations or approve misleading generated output.
- **Artifacts do not prevent drift.** Provisioners, external systems, or mutable defaults may diverge from the approved plan.
- **One logical agent does not ensure isolation.**
- **Principal-only design does not guarantee principal intent** if generated artifacts are opaque or defaults are hidden.
- **Audit logs are not tamper resistance** unless externally anchored and complete.
- **Revocation is not immediate** when downstream credentials, caches, or active operations survive.
- **Multiple authenticated stanzas can amplify authority** through composition or confused-deputy behavior.

A defensible initial claim is narrower: Aegis provides a reviewable, attributable workflow for declaring and issuing session-scoped runtime authority.

## Required invariants

1. Design sessions cannot access runtime secrets or perform provisioning side effects.
2. Every runtime session maps to one logical agent, one approved artifact digest, and one runtime identity.
3. No authority exists unless represented in the approved artifact.
4. Provisioning uses the exact approved artifact; any mutation invalidates approval.
5. Stanza composition is deterministic, visible, and fail-closed.
6. Credentials are scoped to the session, expire promptly, and are not reusable by another session.
7. Runtime actions are attributable to the agent, session, stanza, and artifact.
8. Revocation terminates or disables both active sessions and derived credentials, within a documented bound.
9. The runtime cannot self-approve or expand its own authority.

## Smallest coherent UX

A minimal user flow should be linear:

1. **Create agent**  
   Name it and assign a stable logical-agent identifier.

2. **Draft session authority**  
   Select resources and actions using structured stanzas. Show effective combined authority—not just individual entries.

3. **Preview artifact**  
   Present a canonical, diffable manifest with explicit defaults, expiry, composition behavior, and warnings.

4. **Approve**  
   The principal authenticates and approves the artifact digest. Any change requires reapproval.

5. **Provision and start**  
   A separate provisioner creates an ephemeral runtime identity and session-bound credentials from that digest.

6. **Observe and revoke**  
   Show runtime status, effective authority, expiry, actions, and a prominent revoke/terminate control.

Avoid graphical policy builders, collaborative editing, delegation chains, reusable templates, and automatic optimization in the first release.

## Smallest coherent MVP

Include only:

- one principal type;
- one logical agent per runtime session;
- one runtime implementation with a real isolation boundary;
- a small fixed stanza schema;
- additive grants with explicit conflict rejection—no complex precedence;
- session-scoped, short-lived credentials;
- canonical artifact serialization and digest-based approval;
- provisioning from immutable approved artifacts;
- append-only audit events;
- runtime termination and credential revocation;
- one end-to-end integration with a resource provider.

Defer:

- multi-party approval;
- agent delegation and subagents;
- cross-session memory or credentials;
- arbitrary policy languages;
- dynamic trust negotiation;
- long-lived sessions;
- multiple provisioners or clouds;
- automated artifact generation from natural language;
- claims of formal least privilege or comprehensive confinement.

## Recommended framing

Position Aegis initially as an **authority issuance and provenance system for agent sessions**, not a complete agent-security platform. Its value is making the chain from principal intent to reviewed artifact to provisioned runtime identity explicit, reproducible, and auditable. Broader security claims should wait until isolation, composition, revocation, and artifact-to-runtime equivalence are demonstrated.
