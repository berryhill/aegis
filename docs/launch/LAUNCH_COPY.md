# Aegis Launch Copy

This file contains draft public copy for the first open-source alpha. Every version number, platform claim, Hermes range, and link must be checked against the exact release commit before publication.

## Core message

> Agent identity and authority must be established outside the model, and every runtime session should receive exactly one authenticated, reviewable trust context.

Short form:

> One authenticated identity. One trust stanza. One clean runtime session.

Key distinction:

> Trust stanzas are not personalities; they are security contexts.

## GitHub description

Identity and session control for authenticated, trust-stanza-bound Hermes Agent runtimes.

## Repository topics

```text
ai-agents
agent-security
hermes-agent
identity
authentication
authorization
policy-enforcement
golang
```

## One-line announcement

Aegis is an experimental Go control plane that authenticates callers outside the model and launches each Hermes Agent session under exactly one approved trust stanza.

## Short announcement

Prompts and agent profiles can describe roles, but they do not authenticate callers. Aegis authenticates a local operator, deterministically selects exactly one trust stanza, binds authority to an exact charter and short-lived mandate, and launches Hermes Agent in a fresh runtime home with the approved configuration. The first alpha is a technical preview: it controls session authority and runtime materialization, but it is not a host or network sandbox.

## Hermes-community post

### Title

Aegis: authenticated trust-stanza sessions for Hermes Agent

### Body

I have been working on Aegis, an experimental Go control plane for Hermes Agent.

The premise is simple: prompt text and profile names are not authentication. Aegis establishes identity outside Hermes, selects exactly one authorized trust stanza, binds the decision to an approved charter and mandate, and starts a fresh Hermes process with the corresponding runtime configuration.

The alpha demonstrates:

- local OS identity authentication;
- fail-closed trust-stanza selection;
- exact charter and provisioning-plan approval;
- disposable Hermes homes;
- explicit toolset launch arguments;
- session revocation and process termination; and
- authoritative audit verification.

It does not claim host filesystem or network sandboxing, post-launch attestation of every individual Hermes tool, or production multi-tenant security.

The most useful feedback would be:

1. Can you complete the no-key demonstration on a clean supported machine?
2. Is the distinction between a requested stanza and authenticated identity clear?
3. Does the threat model accurately describe the Hermes and host boundaries?
4. Which Hermes behavior should the adapter verify more strongly?

Repository: https://github.com/berryhill/aegis

Quickstart: https://github.com/berryhill/aegis/blob/main/docs/QUICKSTART.md

Threat model: https://github.com/berryhill/aegis/blob/main/docs/THREAT_MODEL.md

## Show HN draft

### Title

Show HN: Aegis – authenticated trust contexts for Hermes Agent sessions

### Body

Agent profiles and system prompts can describe roles, but they cannot authenticate callers. I built Aegis, an experimental Go control plane that maps a locally authenticated identity to exactly one approved trust stanza before launching a fresh Hermes Agent session.

A stanza is a security context rather than a personality. It declares authentication rules, Hermes toolsets, runtime settings, memory and credential scopes, session lifetime, and approval requirements. Zero matches deny, multiple matches deny as ambiguous, and authority from different stanzas is never combined.

Aegis binds provisioning approval to exact charter and plan digests, issues a short-lived mandate, launches Hermes in a disposable `HERMES_HOME`, records the runtime provenance, and can revoke the session and terminate the process. The repository includes a no-provider-key demonstration and a threat model.

This is an alpha, not a claim of complete agent security. Disposable Hermes state is not host, container, VM, or network confinement; broad Hermes toolsets remain broad host-facing authority. The optional Linux broker exposes only one model-visible typed GitHub repository-metadata action through an Aegis-generated bridge whose exact one-tool registration is checked against the live Hermes gateway.

I would especially value feedback on the identity-to-stanza model, the stated security boundaries, and whether the quickstart reproduces cleanly.

Repository: https://github.com/berryhill/aegis

## Lobsters or technical-forum draft

### Title

Aegis: establishing agent authority outside the model

### Body

Aegis explores a narrow agent-security principle: identity and authority should be established outside the model, and each runtime session should receive exactly one authenticated trust context.

The current Go implementation targets Hermes Agent. It authenticates a configured local principal, validates strict charters, denies zero or ambiguous stanza matches, binds approval to exact deterministic artifacts, issues a short-lived mandate, and launches a new Hermes process with disposable state and explicit toolset arguments.

The interesting boundary is what Aegis intentionally does not claim. A disposable runtime home avoids ambient Hermes state, but it does not confine host filesystem or network access. The runtime remains an untrusted workload, and a stanza granting broad tools intentionally grants broad authority.

The repository includes the implementation, threat model, architecture, specifications, no-key demo, and hermetic CLI tests. I am looking for criticism of the authority model and reproducibility rather than production adoption claims.

Repository: https://github.com/berryhill/aegis

## Release summary template

### Aegis VERSION

Aegis VERSION is the first open-source technical preview of authenticated, trust-stanza-bound Hermes Agent sessions.

#### What it proves

- Identity is established outside model conversation.
- Every operational session binds to exactly one trust stanza.
- Exact approval covers deterministic charter and provisioning artifacts.
- Hermes starts in a fresh runtime home with explicit approved toolset arguments.
- Aegis can revoke the mandate and terminate the recorded runtime process.
- Audit verification reconstructs the identity-to-runtime authority chain.

#### Supported environment

- Platforms: VERIFY AGAINST RELEASE WORKFLOW
- Go: VERIFY AGAINST `go.mod` AND README
- Hermes Agent: VERIFY SUPPORTED RANGE

#### Important limitations

- No host filesystem, container, VM, or network sandbox.
- No stable individual-tool attestation for ordinary Hermes toolsets; only the reserved Aegis bridge receives exact live one-tool registration verification.
- No production remote multi-tenant identity boundary.
- The model-visible broker bridge supports only typed GitHub repository metadata; provider credentials remain environment-backed.
- Audit tamper resistance depends on the documented checkpoint-retention boundary.

#### Try it

- Quickstart: LINK
- No-key demonstration: LINK
- Threat model: LINK
- Checksums: LINK

#### Feedback requested

- Clean installation and demo reports.
- Security-boundary misunderstandings.
- Hermes compatibility findings.
- Reproducible authorization, approval, lifecycle, or audit defects.

## Social post variants

### Technical

Prompts are not authentication. Aegis maps an authenticated local identity to exactly one approved trust stanza, then launches a clean Hermes Agent session under that mandate. The alpha includes a no-key demo and an explicit threat model: LINK

### Security-boundary focused

A disposable agent home prevents ambient runtime-state inheritance; it is not a host sandbox. Aegis is an open-source experiment in establishing identity, approval, and session authority outside the model. Threat model and runnable demo: LINK

### Contributor focused

Aegis is preparing its first open-source alpha for authenticated Hermes Agent sessions. We are looking for clean-install reports, threat-model review, Hermes inspection research, documentation improvements, and narrowly scoped security hardening—not broad feature proposals yet. LINK

## Maintainer first comment

Thanks for taking a look. The fastest way to evaluate Aegis is the no-key demonstration, which exercises the control plane without pretending that a provider-backed model turn succeeded.

Please include your OS, architecture, Go version, Hermes version, exact command, and redacted error when reporting a reproduction. Do not post credentials, prompts containing secrets, private charters, or personal state paths.

The project is intentionally narrow at this stage. Requests that weaken fail-closed identity, one-stanza-per-session, exact approval, explicit runtime visibility, or honest isolation boundaries will not be accepted as convenience features.

## Questions for interviews and feedback threads

Ask observed-behavior questions:

- What was the first sentence or term you did not understand?
- Could you complete the no-key demonstration without maintainer help?
- At what point did you expect Aegis to provide stronger sandboxing?
- Which current tool, profile, or script does Aegis replace for you?
- What identity evidence would your real deployment require?
- Which runtime input would you be most concerned about inheriting accidentally?
- Which denial or audit explanation is insufficient?
- Would you run Aegis again, contribute a fix/example, or only follow the project?

Avoid speculative questions such as “Would you use an AI security control plane?”

## Publication checklist

Before copying any draft from this file:

- [ ] Replace every placeholder.
- [ ] Verify version, platforms, Go requirement, and Hermes range.
- [ ] Run the linked quickstart and no-key demonstration from the release commit.
- [ ] Verify repository, documentation, and artifact links.
- [ ] Confirm security and code-of-conduct reporting routes.
- [ ] Confirm the maintainer can stay available for replies.
- [ ] Do not ask for stars, coordinate votes, or cross-post identical copy.
- [ ] Disclose maintainer affiliation plainly.
- [ ] Keep the host/network sandbox limitation in the launch copy.
