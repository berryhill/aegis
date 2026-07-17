# Aegis Launch Readiness Implementation Prompt

> Historical launch-work prompt, retained as the acceptance criteria used for the repository-local launch assets. The assets it requests now exist and are linked from `README.md`. Remote release publication, remote issue creation, owner-controlled private reporting configuration, and hardened deployment remain owner actions rather than missing repository artifacts.

Work directly in:

the repository root

Bring Aegis's minimum launch assets to an accurate, current, reproducible state. This is implementation work, not a request for placeholders or aspirational copy. Inspect the current code, tests, commands, security boundaries, release process, and existing documentation before editing anything. Preserve unrelated user work.

## Authority and required reading

Read these sources before making changes:

- `AGENTS.md`
- `BIG_IDEA.md`
- `MVP_FEATURE_SET.md`
- `GO_RESEARCH.md`
- `FULL_IMPLEMENTATION_PROMPT.md`
- `REMAINING_IMPLEMENTATION_PROMPT.md`
- `specs/README.md` and every Go contract under `specs/`
- The root `README.md`
- All implementation and test files under `cmd/` and `internal/`
- Existing architecture, deployment, security, release, and contributor material

Treat `AGENTS.md` as the highest-authority repository instruction. Do not weaken Aegis's identity, trust-stanza, mandate, approval, provisioning, runtime-visibility, isolation, or fail-closed invariants to simplify documentation or demonstrations.

## Required outcome

Create, correct, or refactor all of the following minimum launch assets:

1. A clear root `README.md` that explains what Aegis is, what it is not, its maturity, explicit Hermes dependency, supported environment, installation, five-minute path, security limitations, and links to detailed material.
2. An accurate `LICENSE` with internally consistent project and copyright information.
3. A `SECURITY.md` with supported versions, private vulnerability-reporting instructions, expected response process, disclosure guidance, and an honest statement of current security boundaries.
4. A `CONTRIBUTING.md` with prerequisites, setup, build, test, lint/vet, vulnerability scanning, focused-test, documentation, issue, and pull-request guidance.
5. A `CODE_OF_CONDUCT.md` with clear scope, expected behavior, enforcement responsibilities, and a real reporting route. Do not invent contact details; identify unresolved owner input explicitly if necessary.
6. A `CHANGELOG.md` following a consistent release format, with an Unreleased section derived from repository history and verified current behavior rather than guessed dates or features.
7. A focused threat model covering assets, actors, trust boundaries, entry points, abuse cases, mitigations, residual risks, explicit non-goals, and the distinction between Hermes state isolation and host sandboxing.
8. A maintainable architecture diagram, preferably source-controlled Mermaid unless the repository already standardizes another text-based format. It must show authentication, charter/design, approval, deterministic provisioning, stanza selection, mandates, Hermes launch, credentials/state boundaries, API/CLI, persistence, and audit flow.
9. A genuine five-minute quickstart from a clean checkout. Keep required commands minimal, identify prerequisites, and make success criteria obvious.
10. A no-key demonstration that exercises meaningful Aegis behavior without inference-provider credentials or mutation of the user's normal Hermes profile. It must fail honestly at any provider-required boundary and must not simulate a successful model turn.
11. A short reproducible terminal recording and its source script or recording recipe. The recording must use sanitized, deterministic fixtures, avoid secrets and personal paths where practical, and match the current CLI. If recording software is unavailable, add the reproducible script/recipe and report the unproduced recording as a blocker rather than fabricating it.
12. A reproducible GitHub release workflow that builds supported binaries, generates cryptographic checksums, and attaches both to a release. Pin action permissions narrowly, document provenance and platform scope, and verify the workflow or its underlying build/checksum commands locally where possible. Do not publish a release without explicit authorization.
13. Several focused early-contributor issues. Prefer repository issue forms/templates plus a clearly labeled local issue backlog containing bounded scope, context, acceptance criteria, relevant files, verification steps, dependencies, and security considerations. Do not create remote GitHub issues without explicit authorization.

## Synchronization requirement

For every code or behavior change made during this work, review all thirteen asset classes again. Update every affected asset in the same change. Do not touch unaffected files solely to create the appearance of compliance; record them as reviewed and unaffected.

All claims must match executable behavior. In particular, verify and keep synchronized:

- CLI command names, flags, output format, and exit behavior.
- Configuration fields, precedence, defaults, and redaction.
- Supported Go and Hermes versions.
- Authentication and authorization behavior.
- Design-session and provisioning boundaries.
- Toolset, credential, memory, and stanza isolation claims.
- API authentication, transport, and deployment limitations.
- Build, test, package, and release commands.
- Threat-model controls and residual risks.

Do not claim complete zero trust, sandboxing, formal least privilege, tamper-proof audit, exact individual-tool enforcement, or successful provider-backed execution unless the implementation and tests demonstrate it.

## Working method

1. Inspect Git status first and preserve all existing work.
2. Inventory the thirteen launch-asset classes as present, missing, stale, or externally blocked.
3. Trace every factual claim to code, tests, configuration, or real command output.
4. Implement the smallest coherent corrections; avoid unrelated code refactors.
5. Keep detailed documents focused and keep the root README concise.
6. Use repository-relative paths and portable commands where possible.
7. Never read, print, record, or commit secrets.
8. Keep all retained project artifacts inside the repository.
9. Do not modify the user's normal Hermes profile or activate profiles, gateways, services, plugins, MCP servers, or cron jobs.
10. Do not commit, push, publish releases, or create remote issues unless explicitly requested.

## Verification

Run and report real results for all applicable checks, including:

- Markdown links and formatting checks available in the repository.
- Every command in the five-minute quickstart.
- The complete no-key demonstration from a clean isolated state.
- The terminal-recording source script or recipe.
- Go formatting, tests, race tests, vet, vulnerability scanning, and build.
- Release builds for each declared target and checksum generation/verification.
- Diagram rendering or syntax validation when tooling is available.
- A secret scan of recording fixtures and generated public artifacts using available repository tooling.

Use disposable Aegis state and `HERMES_HOME` directories for demonstrations and tests. Confirm that no newly spawned Aegis or Hermes processes remain afterward. If a check depends on unavailable credentials, platform runners, signing infrastructure, release permissions, contact details, or GitHub authorization, report that limitation precisely and leave an actionable, repository-local artifact; never invent a successful result.

## Completion report

Finish with:

- Assets created or updated.
- Assets reviewed and unchanged, with the reason.
- Exact verification commands and outcomes.
- Remaining blockers or owner decisions.
- External actions still requiring the repository owner's authorization.

Do not call launch readiness complete while any required asset is missing, stale, unverified, fabricated, or silently deferred.