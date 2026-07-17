# Aegis: The Big Idea

## Executive summary

Aegis is an identity, trust, and session-control layer for agents running on explicit, existing AI runtimes.

Aegis does not replace Hermes, Codex, Claude Code, OpenClaw, or another runtime, and it does not pretend those runtimes are interchangeable. Instead, Aegis authenticates the caller, selects an authorized trust context, binds that context to a new session, and then starts the selected runtime with only the identity, memory, credentials, tools, and authority assigned to that context.

The central abstraction is:

> An authenticated, trust-stanza-bound session over an explicitly selected agent runtime.

A logical agent may define one or many trust stanzas. A session enters exactly one stanza. Stanzas are not personalities; they are security contexts.

## The problem

Current agent runtimes are generally configured around profiles, prompts, tools, credentials, and conversation sessions. Those mechanisms are useful, but they do not by themselves provide a complete answer to several basic questions:

- Who is speaking to the agent?
- Which relationship or trust context applies?
- What authority does that identity have in this session?
- Which memory, credentials, tools, and data may the session access?
- What may cross from one trust context to another?
- Which consequential actions require a principal's approval?
- Which underlying runtime is actually executing the session?

A single agent may need to interact differently with its principal, team members, other agents, customers, or the public. Encoding all of that in one system prompt gives the model guidance, but it does not create a dependable security boundary.

Aegis makes those boundaries explicit and enforceable outside the model.

## Core model

### Logical agent

A logical agent is the stable entity the user designs and names. It owns a charter and one or more trust stanzas. It is distinct from any individual runtime process or runtime-specific profile.

A logical agent may be implemented by Hermes today and another runtime later. That runtime choice remains visible and is part of every design and session record.

### Runtime

The runtime is the concrete agent system that performs reasoning and tool use. Examples include Hermes Agent, Codex, Claude Code, and OpenClaw.

Aegis never hides the runtime. The user should always be able to see:

- Runtime name and version
- Runtime adapter version
- Runtime-specific profile or home, if any
- Model and provider
- Session mode
- Effective trust stanza

Runtime adapters translate Aegis artifacts into runtime-specific configuration and lifecycle operations. They do not erase runtime differences.

### Principal

The principal is the root human authority for a logical agent. For the initial Aegis deployment, that principal is Matt.

The principal identity is established by an authentication mechanism outside the model. A prompt saying "I am Matt" is never authentication. Only the principal may create or expand foundational authority, approve sensitive provisioning, and authorize protected cross-stanza disclosure.

### Trust stanza

A trust stanza is one bounded interaction and authority context within a logical agent.

Examples include:

- `principal`
- `teamwide`
- `project-alpha`
- `customer-support`
- `public`

Each stanza declares:

- Who may authenticate into it
- Which runtime configurations may serve it
- Which tools and capabilities it receives
- Which memory namespace it uses
- Which credentials it may access
- Which data classifications it may read or emit
- Which actions require approval
- Its session lifetime and reauthentication rules
- Whether and how information may cross to another stanza

Every stanza requires identity provenance. A public stanza may authorize a broad class of identities, but "public" does not mean that messages lose their source or become principal-authorized.

A session selects exactly one stanza. If no stanza matches, access is denied. If more than one stanza matches ambiguously, access is denied. Aegis never unions permissions from multiple stanzas.

### Charter

The charter is the canonical, declarative specification for a logical agent. It contains the runtime selection, trust stanzas, identity rules, capability assignments, memory and credential scopes, approval rules, and lifecycle policy.

The charter—not a design conversation—is the source of truth. It is versioned, validated, diffable, and digestible. Provisioning and session startup refer to an exact charter revision.

### Mandate

A mandate is the short-lived authority issued to one authenticated session. It binds:

- Authenticated subject
- Logical agent
- Selected trust stanza
- Explicit runtime and runtime configuration
- Charter version and digest
- Effective capabilities
- Memory and credential scopes
- Issue and expiry times
- Revocation state

The model cannot create, alter, or extend its mandate.

### Session

A session is the actual execution of a runtime under a mandate. Its trust stanza remains fixed for its lifetime.

Changing stanza, runtime, principal, or material authority creates a new session. It must not carry over private transcript, loaded secrets, tool handles, or memory unless an explicit policy permits a controlled transfer.

## User experience

## Designing an agent

Aegis should provide a dedicated design session rather than injecting design authority into whichever ordinary agent profile happens to be active.

The intended flow is:

1. Authenticate Matt as the principal.
2. Resolve and visibly display the target runtime.
3. Start a dedicated design session using that runtime and its Aegis adapter.
4. Load runtime-neutral trust-design guidance plus runtime-specific expertise.
5. Discuss the agent's purpose, identities, stanzas, capabilities, memory, credentials, and approval boundaries.
6. Produce a canonical charter and runtime-specific provisioning plan.
7. Validate the design and show all defaults, warnings, requested credentials, files, profiles, and services.
8. Require explicit approval of the exact charter revision.
9. Provision deterministically.
10. Verify the resulting runtime artifacts before activation.

The design model proposes. Aegis validates and provisions. The target agent does not grant itself authority or configure its own trust boundary.

A named runtime profile is not necessarily required during design. For Hermes, Aegis can run a disposable, isolated design worker and create actual Hermes profiles or other persistent artifacts only after approval.

## Starting an operational session

The operational flow is:

1. Select a logical agent.
2. Authenticate the caller.
3. Select or resolve one trust stanza.
4. Confirm that the identity is authorized for that stanza.
5. Resolve the explicit runtime and runtime-specific target.
6. Issue a short-lived mandate.
7. Load only that stanza's tools, memory, credentials, and policy.
8. Start a clean runtime session.
9. Record runtime, identity, stanza, mandate, and charter provenance.
10. Expire or revoke the session when required.

On Matt's private workstation, the default stanza may be `principal` only when the local environment has authenticated Matt. On shared, remote, fleet, or unauthenticated surfaces, Aegis should default to deny or to a lower-trust stanza—not silently to `principal`.

## Trust and enforcement

Aegis treats the model and runtime as proposal-generating workloads, not policy authorities.

The division of responsibility is:

- The model reasons and proposes.
- The runtime executes only the tools it is actually given.
- Aegis authenticates identities.
- Aegis selects the stanza.
- Aegis issues and revokes mandates.
- Aegis enforces provisioning and protected-action policy.
- Aegis records authoritative audit events.

Prompt instructions remain useful for behavior and explanation, but the following must not depend solely on prompts:

- Identity
- Stanza selection
- Capability availability
- Credential access
- Approval state
- Provisioning authority
- Cross-stanza disclosure
- Audit generation

The long-term architecture is therefore a harness around agent runtimes: a reference monitor, session authority issuer, and controlled information-flow boundary.

## Information flow

Trust stanzas should be isolated by default. Cross-stanza access is not direct memory or filesystem access. It is a controlled request and disclosure flow.

A private stanza may approve a sanitized fact for a teamwide stanza without releasing the private source data. A teamwide agent may submit a request upward, but that request carries no principal authority merely because it was delivered to the private context.

The eventual information-flow model should support:

- Typed cross-stanza requests
- Explicit disclosure policies
- Field-level minimization
- Exact-payload approval
- Expiring releases
- Provenance and audit linkage
- No implicit transitive trust

The initial MVP can remain stricter: no automatic cross-stanza data transfer at all.

## Runtime adapters

A runtime adapter is responsible for:

- Runtime discovery and version reporting
- Capability discovery
- Starting and stopping design and operational sessions
- Translating charter fields into runtime-specific settings
- Producing a reviewable provisioning plan
- Applying approved runtime artifacts deterministically
- Verifying effective runtime configuration
- Reporting limitations honestly

The first adapter should target Hermes Agent. Hermes offers profiles, toolsets, prompt/context controls, sessions, gateway authentication, MCP, plugins, and programmatic interfaces. Aegis must still distinguish hard runtime controls from prompt-only behavior, and it must not treat a Hermes profile as a host filesystem sandbox.

## Technical direction

Aegis will be implemented in Go.

Initial technology direction:

- Cobra for CLI command structure
- Viper as a configuration-input adapter, decoded once into strict typed configuration
- Echo for an optional local or remote control-plane API
- Standard `context` cancellation and structured `log/slog`
- A runtime-adapter boundary with Hermes first
- Deterministic charter validation and provisioning
- Durable, attributable session and audit records

The CLI and API are interfaces to the same application services and policy model. Neither should contain the security model itself.

## What Aegis is not

Aegis is not:

- A new foundation model
- A replacement for existing agent runtimes
- A universal runtime compatibility illusion
- A prompt template that asks models to obey roles
- A claim that profiles alone provide sandboxing
- A system where a CLI flag grants principal authority
- A system where agents self-approve or expand their own permissions
- A full zero-trust, federation, or formal information-flow platform in its first release

## Initial product claim

The defensible initial framing is:

> Aegis provides authenticated, reviewable, and attributable trust-stanza sessions over explicit agent runtimes.

Its long-term ambition is broader:

> Aegis becomes the identity, authority, disclosure, and session-control plane for human-directed agent systems.

## Project vocabulary

- **Aegis:** the overall system
- **Logical agent:** the stable agent designed by the user
- **Runtime:** the explicit underlying agent runtime
- **Adapter:** runtime-specific integration
- **Principal:** root human authority
- **Charter:** canonical logical-agent specification
- **Trust stanza:** one authenticated security context
- **Mandate:** short-lived session authority
- **Session:** one runtime execution bound to one stanza
- **Disclosure:** controlled transfer between stanzas
- **Provisioner:** deterministic component applying approved artifacts

## Research foundation

The detailed primary-source research supporting this direction is retained in the repository under `research/`, including:

- Go, Cobra, and Viper practices
- Echo control-plane engineering
- Agent security and trust-control architecture
- Hermes runtime integration
- Independent product-architecture critique
