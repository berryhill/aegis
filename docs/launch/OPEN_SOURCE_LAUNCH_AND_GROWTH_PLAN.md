# Aegis Open-Source Launch and Organic Growth Plan

## Purpose

This document is the repository-local operating plan for the first public Aegis release and the community-building period that follows it. It separates implemented product claims from future architecture and treats a runnable, honest technical preview as the launch objective.

## Core principle

> Agent identity and authority must be established outside the model, and every runtime session should receive exactly one authenticated, reviewable trust context.

Short form:

> One authenticated identity. One trust stanza. One clean runtime session.

Supporting distinction:

> Trust stanzas are not personalities; they are security contexts.

## Launch objective

Publish a technically credible alpha that allows an external Linux or macOS user to understand the Aegis security contract, run the no-key demonstration, validate the core identity-to-runtime workflow, and identify the boundaries Aegis does not enforce.

The launch is successful when qualified users can accurately explain what Aegis enforces and what it does not enforce. Stars and announcement traffic are secondary.

## Initial audience

Primary:

- Hermes Agent operators who need reproducible sessions with explicit authority.
- Developers building tool-enabled agents who do not want prompts or profile names treated as authorization.
- Agent-security researchers and engineers reviewing identity, approval, runtime, and audit boundaries.
- Go infrastructure developers interested in fail-closed control-plane design.

Not the initial audience:

- Buyers expecting a production enterprise security platform.
- Users expecting a host, container, VM, or network sandbox.
- Teams requiring remote multi-tenant identity, fleet reconciliation, or cross-runtime support immediately.
- General AI users who only want a conversational agent interface.

## Positioning

Category:

> Identity and session control for explicit agent runtimes.

One-line description:

> Aegis authenticates a caller outside the model, selects exactly one approved trust stanza, and launches a clean Hermes Agent session under that authority.

Thirty-second explanation:

> Prompts and agent profiles can describe roles, but they do not authenticate callers. Aegis is an experimental Go control plane that maps authenticated identity to one reviewed trust stanza, binds authority to an exact charter and mandate, and launches Hermes Agent in a fresh runtime home with the approved configuration. It controls session authority and runtime materialization; it is not a host or network sandbox.

## Defensible alpha claim

The first release may claim that Aegis provides:

- Local operating-system principal authentication.
- Strict, versioned charters.
- Deterministic selection of exactly one trust stanza.
- Fail-closed zero-match and ambiguous-match behavior.
- Exact approval binding to canonical charter and provisioning-plan digests.
- Short-lived session mandates.
- Explicit Hermes Agent runtime discovery and version enforcement.
- Fresh disposable Hermes homes for operational sessions.
- Approved Hermes toolset launch-argument enforcement.
- Session inspection, revocation, and process termination.
- Local tamper-evident audit verification under the documented checkpoint assumptions.
- An administrative encrypted credential authority that is not yet a runtime broker.

The first release must not claim:

- Host filesystem, kernel, container, VM, or network confinement.
- Stable post-launch attestation of every individual Hermes tool.
- Protection after complete host compromise.
- Runtime use of secrets from the encrypted credential authority.
- Fleet deployment, selective projections, or enterprise secret-manager integration.
- General multi-runtime or multi-tenant production readiness.
- Complete prompt-injection prevention or formal information-flow security.

## Launch gates

A public release is a no-go until every required gate is either verified or explicitly identified as an owner-controlled external action.

### Repository and legal

- [ ] The release commit contains no local toolchain, generated root binary, archive, credential, personal state, or unrelated workstation artifact.
- [ ] Apache-2.0 license and copyright information are correct.
- [ ] Git status is understood and unrelated work is preserved.
- [ ] Root README, documentation index, and repository metadata describe the same product scope.
- [ ] GitHub description and topics are configured.

### Product and documentation

- [ ] Root README explains the problem, value, maturity, installation, five-minute path, and limitations.
- [ ] Quickstart succeeds from a clean checkout.
- [ ] No-key demonstration succeeds without provider credentials and does not simulate a model turn.
- [ ] Threat model distinguishes runtime-state isolation from host confinement.
- [ ] Architecture diagram matches current code and boundaries.
- [ ] Security policy has an owner-approved private reporting route.
- [ ] Contribution guide and code of conduct have owner-approved contact and enforcement details.
- [ ] Changelog matches the release commit.
- [ ] Historical and future architecture documents are clearly labeled.

### Engineering verification

- [ ] `gofmt` check passes.
- [ ] `go build ./cmd/aegis` passes.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes on supported CI runners.
- [ ] `go vet ./...` passes.
- [ ] A freshly installed, supported `govulncheck ./...` passes.
- [ ] Hermetic CLI end-to-end tests pass.
- [ ] The no-key demo leaves no Aegis or Hermes process running.
- [ ] Release builds succeed for every declared operating-system and architecture pair.
- [ ] Generated checksums verify against all release artifacts.
- [ ] The terminal recording is replayed and checked for secrets and personal paths.

### Release operations

- [ ] The version and tag policy are coherent across CLI, adapter, module, changelog, and release workflow.
- [ ] The release workflow has minimal permissions.
- [ ] The tag is created only after the exact release commit passes verification.
- [ ] Release notes include supported Hermes range, platforms, known limitations, and upgrade instructions.
- [ ] Maintainer availability is reserved for launch-day questions and initial issue triage.
- [ ] Rollback or withdrawal steps are documented if a release artifact is wrong or vulnerable.

## Minimum external validation before broad launch

Ask a small set of external reviewers to work without a live walkthrough:

- Two or more Hermes users.
- One agent-security reviewer.
- One Go or infrastructure reviewer.

Each reviewer should attempt to:

1. Explain Aegis after reading the first section of the README.
2. Install or build Aegis on a clean supported environment.
3. Complete the no-key demonstration.
4. Identify why a requested stanza is not identity evidence.
5. Explain why disposable `HERMES_HOME` is not host sandboxing.
6. Report the first confusing command, term, or failure.
7. State whether they would test Aegis again, contribute, or only follow the project.

Do not ask whether they generally like the idea. Ask for observed failures and misunderstood claims.

## Demonstration narrative

The primary demonstration should prove one security story:

1. A caller is authenticated outside Hermes.
2. A stanza request is evaluated as a request, not identity evidence.
3. An unauthorized or ambiguous selection fails closed.
4. A plan is bound to an exact charter digest.
5. Mutation invalidates approval.
6. A mandate binds one identity, stanza, charter revision, and runtime.
7. Hermes launches with a new disposable home and the approved toolset arguments.
8. Revocation removes Aegis authority and terminates the recorded process.
9. Audit verification reconstructs the authoritative chain.

Avoid leading with the design assistant or encrypted credential authority. They are important capabilities, but the identity-to-one-stanza runtime chain is the clearest initial wedge.

## Organic growth strategy

The growth loop is:

```text
narrow useful product
    -> fast first success
    -> public technical proof
    -> qualified user feedback
    -> responsive maintenance
    -> examples and integrations
    -> repeat users and contributors
```

### Method 1: Teach the problem

Publish technical material that is useful without requiring adoption:

- Why prompts are not authentication.
- Why agent profiles are not security principals.
- One session, one trust context.
- Why a model cannot approve its own authority.
- Exact-digest approval for agent configuration.
- Why safe mode and disposable state are not host sandboxing.
- What revocation means for a running agent process.
- Separating encrypted secret storage from runtime credential brokerage.

Each item should include one concrete failure mode, one Aegis design response, and one explicit residual risk.

### Method 2: Participate where users already are

Priority communities:

1. Hermes Agent community and maintainers.
2. Agent-security and AI-infrastructure discussions.
3. Go security and systems communities.
4. Self-hosted agent communities when local operation is relevant.
5. Hacker News or Lobsters only after the project is directly runnable.

Participation rule:

> Answer the underlying problem first; link Aegis only when it is a relevant implementation.

Do not cross-post identical promotional copy, ask for stars, coordinate votes, or use unsolicited bulk direct messages.

### Method 3: Create public proof

Turn security invariants into shareable artifacts:

- Terminal recording of the no-key path.
- Test showing prompt text cannot authenticate.
- Test showing ambiguous matches deny.
- Test showing plan mutation invalidates approval.
- Test showing revocation terminates the process group.
- Test showing audit tampering is detected under stated assumptions.
- Capability matrix separating enforced, partial, planned, and non-goal properties.

### Method 4: Build the contributor ladder

Offer contributions at multiple levels:

- Reproduce installation on another supported platform.
- Clarify documentation or terminology.
- Add a sanitized example charter.
- Add a regression test.
- Improve threat-model analysis.
- Harden filesystem operations.
- Research Hermes post-launch inspection.
- Design a narrow mandate-bound credential broker action.

Every early issue should define context, relevant files, acceptance criteria, verification, dependencies, and security constraints.

### Method 5: Release around substantive milestones

Valid repeated launch moments include:

- First alpha and runnable security demonstration.
- First external contributor.
- First external Hermes deployment report.
- Material improvement to runtime verification.
- First mandate-bound broker action.
- New supported authentication boundary.
- Independent threat-model or security review.

Do not treat routine patch releases as fresh major launches.

## Launch sequence

### Stage 1: Private technical preview

- Complete all repository-controlled launch gates.
- Recruit external reviewers from the initial audience.
- Observe clean installation and demo completion.
- Convert recurring confusion into documentation or product changes.
- Confirm that reviewers can state the security boundary accurately.

### Stage 2: Hermes-community soft launch

- Publish the alpha release, checksums, concise notes, and terminal demo.
- Introduce Aegis through the identity/authority problem.
- Ask for review of the no-key flow and threat model.
- Respond to every substantive report.
- Record qualified feedback without collecting secrets or private prompts.

### Stage 3: Broader technical launch

Proceed only after soft-launch failures are fixed.

- Submit a runnable Show HN.
- Share a tailored post with relevant Go, security, and agent communities.
- Publish one deep technical article rather than many generic announcements.
- Remain available to answer questions and acknowledge limitations.

### Stage 4: Retention and contribution

- Publish a post-launch report with failures, fixes, and next scope.
- Credit issue reporters and contributors in release notes.
- Maintain focused issues and realistic response-time expectations.
- Convert real user configurations into sanitized examples.
- Prioritize repeat use over audience expansion.

## Initial content plan

Recommended order:

1. **Why prompts are not authentication** — establish the problem and Aegis principle.
2. **One authenticated identity, one trust stanza, one clean session** — demonstrate the complete authority chain.
3. **What disposable Hermes state does not protect** — establish credibility through limits.
4. **Exact approval for agent authority** — show mutation and replay resistance.
5. **Revoking a live agent session** — demonstrate lifecycle enforcement.
6. **Secret storage is not credential brokerage** — explain the encrypted authority boundary honestly.

Every post should link to a runnable artifact or exact source/test location.

## Metrics

### Discovery

- Repository and documentation visitors.
- Referring sources.
- Release-page and artifact visits.
- Qualified discussions mentioning the actual identity/authority problem.

### Activation

- Successful clean builds or installs.
- Completed no-key demonstrations.
- Successful charter validation and Hermes discovery.
- Users who can explain the enforced boundary accurately.

### Retention

- Repeat release downloads or upgrades.
- Returning issue participants.
- Users reporting a second experiment or real configuration.
- Repeat contributors.

### Contribution

- Actionable external bug reports.
- External documentation and example improvements.
- First-time and repeat pull requests.
- Independent integrations or write-ups.

### Maintainer health

- Time to acknowledge issues.
- Time to review pull requests.
- Unresolved security reports.
- Support load and recurring confusion.
- Scope pressure outside the stated alpha wedge.

Stars and forks are awareness signals, not the primary success criteria.

## Launch no-go conditions

Delay or withdraw the launch if:

- A clean external user cannot complete the documented path.
- The release contains an unreviewed credential, personal path, local binary, or toolchain artifact.
- Vulnerability scanning is broken or reports an applicable reachable vulnerability.
- Documentation materially overstates host, network, tool, credential, or audit enforcement.
- The supported Hermes range cannot be reproduced.
- Checksums do not match the published artifacts.
- The private security-reporting route is not owner-approved.
- Existing unrelated work would be overwritten or accidentally included.
- The maintainer cannot respond to early security or installation reports.

## Owner-controlled decisions and actions

The repository owner must explicitly decide or perform:

- Final release version and publication timing.
- Security-reporting contact or private GitHub reporting configuration.
- Code-of-conduct enforcement contact.
- GitHub repository description and topics.
- Creation of remote contributor issues from the local backlog.
- Tag signing and remote release publication.
- Community announcements and availability for follow-up.
- Whether macOS support is claimed based on real external verification.

## References

Repository sources:

- [`README.md`](../../README.md)
- [`SECURITY.md`](../../SECURITY.md)
- [`CONTRIBUTING.md`](../../CONTRIBUTING.md)
- [`CHANGELOG.md`](../../CHANGELOG.md)
- [`docs/QUICKSTART.md`](../QUICKSTART.md)
- [`docs/DEMO_NO_KEY.md`](../DEMO_NO_KEY.md)
- [`docs/THREAT_MODEL.md`](../THREAT_MODEL.md)
- [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md)
- [`docs/contributing/ISSUE_BACKLOG.md`](../contributing/ISSUE_BACKLOG.md)

External guidance used for the organic-growth approach:

- Open Source Guides, Finding Users: https://opensource.guide/finding-users/
- Open Source Guides, Building Welcoming Communities: https://opensource.guide/building-community/
- Open Source Guides, Best Practices for Maintainers: https://opensource.guide/best-practices/
- Open Source Guides, Metrics: https://opensource.guide/metrics/
- GitHub, Setting Up for Healthy Contributions: https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions
- GitHub, Repository Topics: https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics
- Hacker News, Show HN Guidelines: https://news.ycombinator.com/showhn.html
