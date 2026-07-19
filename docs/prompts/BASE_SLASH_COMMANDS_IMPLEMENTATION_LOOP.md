# Base Slash Commands Implementation Loop

Implement and verify Aegis's complete Core 15 base slash-command surface through the real authenticated manager path. This is a production implementation task, not a design review, mockup, help-text patch, or disconnected package exercise.

Read `AGENTS.md` first. Then read, in authority order:

- `specs/MVP.md`;
- `specs/AEGIS_MANAGER.md`;
- `specs/BASE_MANAGER_END_TO_END.md`;
- `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`;
- `specs/TERMINAL_EXPERIENCE_OPERATIONAL_IMPLEMENTATION.md`;
- `specs/IDENTITY_AND_AUTHORIZATION.md`;
- `specs/RUNTIME_AND_SESSIONS.md`;
- `specs/AUDIT.md`;
- `specs/CONTROL_PLANE_API.md`;
- `specs/BASE_SLASH_COMMANDS.md`.

Use these as supporting research, not as authority over the specifications:

- `research/2026-07-19-aegis-security-slash-command-model.md`;
- `research/2026-07-19-aegis-core-15-command-report.md`;
- `research/2026-07-17-host-endpoint-monitoring-enforcement-response.md`;
- `research/2026-07-17-mcp-agent-skills-secret-exposure-addendum.md`.

## Working-tree safety

Before editing, run fresh `git status`, branch, and diff inspection. The repository contains substantial uncommitted manager/TUI/lifecycle/documentation work. Treat every existing modification and untracked file as user work. Preserve, understand, and integrate with it. Do not revert, overwrite, discard, commit, push, broadly reformat, or replace it with a parallel manager implementation.

Do not touch the operator's real:

- `~/.argis` or Aegis state;
- credential authority or credentials;
- Hermes profiles, homes, plugins, skills, MCP, gateways, sessions, or configuration;
- Ollama daemon or model store;
- host sensors, audit policy, firewall, process state, services, cron, or startup configuration;
- external reports, issues, releases, or messaging destinations.

Use isolated temporary HOME/config/state/audit/runtime fixtures and fake loopback services. Do not install or activate endpoint sensors in this loop.

## Required outcome

The real interactive manager must expose exactly this canonical base vocabulary through one typed registry:

```text
/help
/status
/context
/authority
/limits
/scan
/watch
/findings
/investigate
/timeline
/report
/audit
/cancel
/clear
/exit
```

The real path must be:

```text
composer submission
  -> bounded command-position detection
  -> exact typed parser and registry
  -> manager lifecycle/readiness
  -> authenticated identity + exactly one stanza/mandate
  -> shared application service or truthful typed unavailable result
  -> typed result/presentation event
  -> safe terminal rendering
  -> metadata-safe audit where required
  -> cancellation/cleanup
```

Recognized and unknown slash input must never reach Hermes. Model, subagent, tool, sensor, report, audit, or transcript output must never invoke local command dispatch.

Do not stop after adding command names, switch cases, interfaces, fixtures, TODOs, static output, or completion entries. Continue until every implemented claim is connected to the real manager path, exercised, documented, and verified.

## Implementation sequence

Work in the following sequence. Keep each slice buildable and tested before moving forward.

### Slice 1 — trace and preserve the current manager/TUI

Inspect and map:

- root/bare interactive dispatch;
- `internal/command/manager.go` and startup command handling;
- current `localDirective` and completion behavior;
- `internal/tui` controller, composer, events, state, rendering, sanitization, and tests;
- manager lifecycle, cancellation, runtime failure, and cleanup;
- identity, policy, mandate, readiness, audit, and application services;
- existing `/secret` compatibility/domain commands;
- existing noninteractive CLI/API contracts;
- active uncommitted diffs.

Write no code until you can identify the current terminal owner, command-routing point, authoritative state sources, and cleanup owner.

### Slice 2 — typed registry and parser

Replace the duplicated switch/help/completion command declarations with one constructor-built typed registry. It must describe canonical name, aliases, grammar, execution class, availability, capability/service prerequisites, scope, consequence class, cancellation, policy/audit operation, result schema, examples, and help text.

Implement:

- all 15 canonical names;
- exact lowercase matching;
- suggestion-only prefix/fuzzy behavior;
- aliases canonicalized before dispatch/policy/audit;
- command recognition after permitted leading whitespace;
- documented bounded non-shell quoting;
- `//` literal slash escape through the ordinary ingress guard;
- strict unknown/duplicate/malformed argument rejection;
- no shell expansion, pipes, redirects, chaining, `!`, environment expansion, or hidden model parsing;
- state-aware, disclosure-aware local completion;
- unknown slash input consumed locally.

Preserve existing `/secret` metadata behavior as a compatibility/domain extension. Preserve `/complete` only as a compatibility/testing hook if required; it must delegate to the registry and must not remain another command list. `/quit` is an alias for `/exit`.

Add parser/property/fuzz tests before expanding service behavior.

### Slice 3 — shared result and operation lifecycle

Introduce or reuse typed contracts for:

- canonical command request;
- requested/effective scope;
- immediate read, bounded job, leased subscription, workflow mutation, lifecycle, and export classes;
- operation/scan/watch/finding/investigation/report/audit/receipt IDs as needed;
- result envelope with schema, operation, state, reason, actor/context, scope, time, health/coverage, warnings, related IDs, and audit reference;
- operation registration, lookup, progress, cancellation, and terminal state;
- typed presentation events for command accepted/progress/result/unavailable/denied/cancelled.

Use injected clock/ID/storage/service dependencies for tests. Do not put authority decisions in parser, handler, or view code. Do not invoke the Aegis CLI as a subprocess.

### Slice 4 — operational orientation and lifecycle commands

Fully implement through authoritative sources:

```text
/help
/status
/context
/authority
/limits
/audit
/cancel
/clear
/exit
```

Requirements:

- `/help` comes entirely from the registry and remains usable in degraded mode.
- `/status` reports freshness and distinguishes unknown, stale, stopped, healthy, and zero.
- `/context` renders authenticated subject, stanza, mandate, policy, runtime, and isolation limits from authoritative state.
- `/authority` is read-only and explains current operation decisions without granting or switching anything.
- `/limits` reports real missing capabilities, source health, budgets, retention, and non-sandbox/unmediated-route limits.
- `/audit` preserves current listing/verification authority and metadata-only behavior; `/audit verify` calls the existing verification service.
- Bare `/cancel` never guesses among multiple operations and never claims rollback.
- `/clear` is presentation-only and states what remains unchanged.
- `/exit` and `/quit` converge on the existing bounded cleanup and terminal-restoration path.

Do not fabricate active jobs, sources, findings, or health.

### Slice 5 — real Aegis-native `/scan`

Implement a bounded read-only `core` scan using checks Aegis can authoritatively perform now. Prefer:

- runtime executable/version/process and effective configuration;
- stanza/mandate/authority consistency;
- tool, credential, memory, and broker scope;
- local route/no-fallback/model-switching state;
- configured versus effective artifacts;
- control/readiness health;
- audit-chain verification state.

Bare `/scan` must run or attach to an equivalent running core scan. The idempotency key must include owner, exact effective scope digest, profile/rule revision, source/policy generation, and relevant input identity. Different/stale scope must never deduplicate.

Implement real outcomes:

```text
completed_with_findings
completed_no_findings
partial
degraded
failed
cancelled
expired
```

Every result must report included/omitted modules, effective scope, source/rule versions, source health, timing, observations, findings, and named gaps. “No findings” must never become “safe” or “clean.”

Do not implement fake host scans. Nested secret/process/network/file/persistence/dependency forms must be state-aware unavailable unless a real adapter behind a production interface is added and hermetically tested. Do not install adapters or contact update services without explicit repository-owner authorization.

### Slice 6 — findings and investigations

Add versioned bounded durable storage and services only if they can be implemented without creating a second authority/audit system.

Implement:

- `/findings` bounded visible open list;
- `/findings <id>` typed detail;
- finding schema with rule/version, severity/confidence, workflow state/history, target, source/scope, health/coverage, evidence references, and related IDs;
- strict disclosure filtering that does not leak restricted counts/IDs through output, completion, timing, or errors;
- `/investigate` active-list/usage behavior with no silent target selection;
- `/investigate <finding-id>` create-or-attach after exact authorization;
- bounded investigation records, evidence references, notes/hypotheses, provenance, state, expiry, and links.

Workflow mutation forms may remain state-aware unavailable until exact authorization, append-preserving history, and audit exist. Do not let model narration create authoritative findings or investigation outcomes.

### Slice 7 — timeline and local report

Implement:

- `/timeline` as a bounded query over authoritative event/control records, not a second event store;
- explicit anchor/time range, source/clock/order caveats, event loss/gaps, provenance, and related IDs;
- `/report` as local preview/generation only for an explicitly attached investigation or selected finding;
- frozen versioned report artifact with input digests, coverage, findings, deterministic evidence appendix, timeline/audit references, unresolved questions, provenance, and digest;
- new report revision rather than mutation after source changes.

Bare `/report` must never export or publish. Do not add external destinations in this loop. Model-assisted narrative is optional and must not block deterministic report generation; if added, it receives sanitized structured references, cannot alter authoritative fields/evidence appendix, and remains labeled with provenance.

### Slice 8 — real leased `/watch` or truthful boundary

Implement `/watch` as active only if there is a real Aegis-owned event source manager and bounded event stream to observe. Acceptable initial sources are existing Aegis lifecycle/control/audit events that are already produced authoritatively; do not pretend a polling timer or fixture is host threat monitoring.

A real watch requires:

- watch ID and owner;
- exact scope/profile/rule/source equivalence key;
- bounded lease, buffer, and retention;
- source health, last-event time, reconnect/gap behavior;
- dropped-event semantics that distinguish unknown from zero;
- observation-only enforcement at the capability layer;
- deterministic stop, expiry, revocation, and session-cleanup behavior;
- `/watch`, start, list, status, events, and stop forms backed by one manager.

Bare `/watch` must ensure-and-attach idempotently. A different scope/generation is not equivalent. Session-owned watches stop on `/exit`.

If a real source manager cannot be completed safely in the current loop, finish a truthful typed unavailable `/watch` path with exact readiness, help, completion, tests, and documentation. Do not claim it is active.

### Slice 9 — subagent boundary only after typed services

Subagents are optional for this implementation and must not be added before the typed operation/data/authority boundaries exist.

If used, every delegated task must have parent/delegation IDs, attenuated data scope, explicit tools, provider/model/region policy, retention, depth/fan-out, cancellation/expiry propagation, structured result, and provenance.

Subagents must not inherit parent credentials, approval, tools, slash dispatch, audit authority, response authority, recursive delegation, full transcript, or raw unrestricted evidence. Their output remains untrusted and cannot mark scans complete, create authoritative findings, alter timeline/audit, export reports, or claim mitigation.

Do not use Hermes one-shot/YOLO mode to implement or verify subagents.

## Security and UX requirements

- One Aegis controller owns ordinary terminal input and rendering.
- Background services never write directly to the terminal.
- All command events have typed origin/trust classification.
- Untrusted text is sanitized before measurement and rendering.
- Model/sensor/subagent content cannot forge command results, trust context, findings, audit, or approvals.
- Protected values never appear in command arguments, history, completion, output, audit, reports, or model/subagent context.
- Severity and state are never color-only.
- Rich, plain/accessibility, and machine modes preserve semantics.
- No command changes stanza, mandate, runtime, provider, model, profile, or foundational authority.
- No generic `/respond`, `/fix`, `/kill`, shell mode, or arbitrary file mention.
- No success state is inferred from model narration.

Use canonical language:

```text
observed
detected
denied by Aegis gateway
blocked by OS control
blocked by external control
mitigation requested
mitigation applied, unverified
mitigation confirmed
no findings in covered scope
```

## Required testing

Add focused unit, property/fuzz, integration, PTY, race, and command-path tests proving at least:

1. exactly 15 canonical base names;
2. one registry drives help, completion, dispatch, availability, policy, and audit names;
3. aliases use one canonical handler and result;
4. leading whitespace cannot route a slash command to Hermes;
5. `//` goes through the ordinary ingress guard;
6. unknown slash and malformed commands never reach Hermes;
7. model/tool/subagent/sensor/report/audit text cannot invoke dispatch;
8. no shell expansion, chaining, pipes, redirects, or `!` behavior;
9. command availability across startup, degraded, active, expiring, revoked, and cleanup states;
10. identity/stanza/mandate checks and no authority union/switch;
11. status/context/authority/limits values come from authoritative sources;
12. scan bounds, coverage, idempotency, outcomes, cancellation, and no-finding wording;
13. watch equivalence, lease, gaps, loss, revocation, and cleanup if implemented;
14. finding disclosure and append-preserving state history;
15. no silent investigation target selection;
16. timeline gap/order/clock caveats;
17. frozen report revision/digest and no bare export;
18. audit verification and metadata-only output;
19. cancel is unambiguous and never rollback;
20. clear is presentation-only;
21. exit uses one cleanup path and restores terminal state exactly once;
22. no secret or protected evidence leaks into any output/retention surface;
23. narrow terminal, no-color, screen-reader/plain, resize, EOF, Ctrl-C, Esc, terminal loss, SIGINT, and SIGTERM behavior;
24. all production claims exercised through the real manager path with isolated fixtures.

Do not use snapshots alone as proof. Assert typed state, side effects, absence properties, lifecycle transitions, and actual command routing.

## Documentation and launch assets

For each implemented behavior, update and exercise every affected source of truth required by `AGENTS.md`, including:

- root `README.md`;
- command/help documentation;
- `docs/QUICKSTART.md`;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md` and recording source;
- `docs/ARCHITECTURE.md` and diagram;
- `docs/THREAT_MODEL.md`;
- `SECURITY.md`;
- `CHANGELOG.md`;
- contributor issue material.

Review `LICENSE`, `CONTRIBUTING.md`, and `CODE_OF_CONDUCT.md` and edit only if affected. Do not fabricate recordings, screenshots, releases, checksums, issue links, scan/watch events, or test output. Do not create external issues/releases without explicit authorization.

Documentation must distinguish:

- implemented and verified;
- implemented but degraded/unavailable by current readiness;
- future adapter behavior;
- explicit non-goals and blind spots.

Run every documented local command/workflow that the isolated environment can safely exercise.

## Verification loop

After each slice:

1. format changed Go files;
2. run focused package tests;
3. run parser fuzz/property tests for a bounded practical interval;
4. run relevant PTY and race tests;
5. exercise the real manager command path with isolated fixtures;
6. run `git diff --check`;
7. inspect fresh status/diff for accidental changes;
8. fix root causes before continuing.

Before completion, run:

```text
go test ./...
go test -race ./...
go vet ./...
go build -o <isolated-output> ./cmd/aegis
git diff --check
```

Also run the repository's documented vulnerability/security workflow if available and locally safe. Do not write the built binary over a tracked or user-owned artifact.

## Stopping condition

Do not stop at a plan, registry stub, interface, renderer, fixture, or partial command list. Continue until all locally implementable requirements are connected, tested, documented, and exercised.

A capability that genuinely depends on an absent sensor/service may stop at the normative truthful unavailable boundary only after the parser, authority, result, help, completion, tests, and documentation for that boundary are complete. Report it as unavailable; do not claim the command's successful operational behavior.

If blocked:

- report the exact requirement and blocker;
- include real command/test evidence;
- do not weaken the requirement;
- do not invent output;
- leave the tree buildable and the completed slices verified.

The final report must include:

- architecture and command-semantics decisions;
- exact files changed;
- which Core 15 forms are operational, degraded, or unavailable;
- tests, race, vet, build, diff, PTY, and isolated command-path results;
- scan/watch coverage and explicit blind spots;
- subagent status and delegation controls, if any;
- documentation workflows exercised;
- complete launch-asset impact review;
- residual limitations and next focused slice;
- confirmation that no real operator state, credential, runtime profile, model store, sensor, external system, issue, release, or report destination was modified.