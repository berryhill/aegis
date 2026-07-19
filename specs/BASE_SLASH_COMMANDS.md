# Base Slash Commands

## 1. Status and purpose

**Status:** Normative implementation specification

**Target:** The authenticated principal-facing Aegis manager terminal and the shared application services behind its base slash-command surface

**Parent specifications:**

- `specs/MVP.md`
- `specs/AEGIS_MANAGER.md`
- `specs/BASE_MANAGER_END_TO_END.md`
- `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`
- `specs/TERMINAL_EXPERIENCE_OPERATIONAL_IMPLEMENTATION.md`
- `specs/IDENTITY_AND_AUTHORIZATION.md`
- `specs/RUNTIME_AND_SESSIONS.md`
- `specs/AUDIT.md`
- `specs/CONTROL_PLANE_API.md`

**Supporting research:**

- `research/2026-07-19-aegis-security-slash-command-model.md`
- `research/2026-07-19-aegis-core-15-command-report.md`
- `research/2026-07-17-host-endpoint-monitoring-enforcement-response.md`
- `research/2026-07-17-mcp-agent-skills-secret-exposure-addendum.md`

This specification defines the stable base vocabulary, parsing, routing, authority, lifecycle, result, subagent, testing, and completion contracts for Aegis slash commands. It is intended for a long-running implementation loop.

Normative terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** have their conventional requirements meanings.

The defining invariant is:

> A recognized Aegis slash command is a deterministic local control request consumed before ordinary ingress and Hermes. It is parsed into a closed typed operation, checked against authoritative Aegis state, executed through a shared application service, and rendered from a typed result. Slash text is never authentication, authority, approval, or model instruction.

## 2. Authority and conflict resolution

The implementation MUST follow this authority order:

1. `AGENTS.md`;
2. `specs/MVP.md`;
3. identity, authorization, approval, runtime, session, audit, manager, and lifecycle specifications;
4. this focused specification;
5. supporting research;
6. implementation convenience.

If this specification conflicts with a stronger identity, one-stanza, mandate, credential, approval, clean-session, audit, or lifecycle invariant, the stronger requirement wins. The implementation MUST resolve the conflict explicitly rather than weaken security silently.

This specification does not authorize:

- endpoint sensor installation or activation;
- host-wide or fleet-wide collection merely because a slash command exists;
- host sandbox claims;
- modification of normal Hermes profiles;
- arbitrary shell, file, network, MCP, plugin, skill, cron, or gateway authority;
- cloud fallback or in-session model/runtime/stanza switching;
- external report publication;
- automatic response, containment, remediation, quarantine, credential rotation, or policy mutation;
- creation of remote issues, releases, or external resources;
- fabricated scan, watch, finding, report, audit, or test output.

## 3. Operational completion rule

### 3.1 Operational means connected

A base command is operational only when this path is real and exercised:

```text
real terminal composer
  -> bounded command-position detector
  -> exact typed parser and registry
  -> current lifecycle/readiness check
  -> authenticated identity and exactly one stanza/mandate
  -> shared Aegis application service or truthful typed unavailable result
  -> typed result/event
  -> terminal-safe authoritative rendering
  -> metadata-safe audit where required
  -> real cancellation/cleanup behavior
```

A renderer, switch case, help string, completion candidate, interface, fixture-only service, static result, model narration, or disconnected package is not an operational command.

A capability-dependent command MAY be operationally complete in an unavailable/degraded state only when:

- the command is parsed and authorized through the real path;
- the exact missing capability, source, service, or scope is identified with a stable reason;
- no success, coverage, watch, finding, or report is fabricated;
- help and completion represent the state truthfully;
- tests prove it becomes available through the same path when a real or hermetic production-interface implementation is supplied.

### 3.2 Implementation slices

The complete specification is intentionally broader than one patch. Implementation SHOULD proceed through these independently verifiable slices:

1. parser, registry, routing, help, completion, and literal-slash escape;
2. status, context, authority, limits, audit, cancel, clear, and exit;
3. Aegis-native core scan and operation/result storage;
4. findings and investigation records;
5. timeline and frozen local report generation;
6. leased watch manager over real Aegis-owned event sources;
7. optional scanner/event adapters and constrained subagent analysis.

No slice may advertise a later slice as implemented.

## 4. Canonical base vocabulary

Aegis MUST define exactly these 15 canonical base top-level commands:

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

These names form the stable base registry. Nested forms and flags do not add top-level commands.

The following aliases MAY exist and MUST canonicalize before authorization, dispatch, telemetry, and audit:

```text
/quit             -> /exit
/scan-secrets     -> /scan secrets
/scan-processes   -> /scan processes
/scan-network     -> /scan network
/scan-files       -> /scan files
```

Aliases MUST NOT have separate handlers, authority names, result schemas, or audit semantics.

Existing manager-specific `/secret` metadata commands MUST be preserved as compatibility/domain extensions during this implementation unless a separately approved migration replaces them. They are not part of the Core 15 and MUST NOT shadow a base command. Existing `/complete` behavior MAY remain as a compatibility/testing hook while completion moves to the registry, but it MUST NOT remain a second source of command truth.

## 5. Command-position and routing contract

### 5.1 Detection

After enforcing a raw input byte bound, Aegis MUST treat input as command input when the first non-whitespace character of the submitted logical input is `/`.

Leading whitespace MUST NOT cause a command-looking input to bypass local dispatch and reach Hermes.

Only user composer submission MAY enter command detection. The following MUST be architecturally unable to invoke the dispatcher:

- Hermes/model output;
- subagent output;
- tool output;
- sensor/integration text;
- report or audit content;
- transcript replay;
- pasted content not submitted by the user as composer input;
- terminal control sequences.

### 5.2 Literal slash

`//` at command position SHOULD escape one leading slash and route the resulting content through the ordinary bounded ingress guard and Hermes path when allowed.

The exact behavior MUST be documented and tested. An escaped slash is not a bypass around the ordinary secret/content guard.

### 5.3 Unknown commands

Unknown slash commands MUST fail locally. They MUST NOT:

- fall through to Hermes;
- fuzzy-execute;
- prefix-execute;
- invoke a similarly named alias automatically;
- trigger a network lookup or skill/plugin discovery;
- reveal unauthorized command targets.

Suggestions MAY be shown but MUST require a new explicit submission.

## 6. Parser and registry

### 6.1 Grammar

The initial grammar MUST be a documented, bounded, non-shell grammar:

```text
/<command> [<subcommand>] [<typed positional> ...] [--flag value | --flag=value] ...
```

It MAY support single- and double-quoted literal values. It MUST NOT perform:

- shell or command substitution;
- environment expansion;
- parser-owned glob expansion;
- pipes or redirects;
- semicolon chaining;
- background `&` behavior;
- arbitrary `!` shell execution;
- hidden model interpretation of trailing text.

Unknown flags, duplicate singleton flags, malformed values, trailing arguments, and oversized values MUST fail with stable usage errors.

### 6.2 Typed registry

One typed registry MUST be the source of truth for:

- canonical command and alias names;
- description and usage;
- argument/flag schema;
- examples;
- execution class;
- lifecycle availability;
- required capabilities/services;
- valid scopes;
- mutation and egress classification;
- approval class;
- cancellation semantics;
- canonical policy and audit operation name;
- result schema;
- help and completion;
- degraded/unavailable behavior.

Dispatch, help, completion, and authorization MUST NOT maintain independent command lists.

### 6.3 Matching

Command and subcommand matching MUST be lowercase and exact. Prefix and fuzzy matching MAY produce suggestions only. User-defined Hermes commands, skills, plugins, or configuration MUST NOT shadow the base registry or populate authoritative Aegis help.

### 6.4 Completion

Completion MUST be local, read-only, bounded, state-aware, and disclosure-aware. It MUST NOT start a command, model call, scan, network request, or broad filesystem walk.

Completion MUST show only safely disclosable targets and valid state transitions. Zero or multiple target matches MUST never be resolved by guessing.

## 7. Shared typed contracts

### 7.1 IDs

Long-running and durable objects MUST use stable opaque typed IDs, including as applicable:

- operation;
- scan;
- watch;
- finding;
- investigation;
- report;
- audit event;
- receipt.

Unique-prefix lookup MAY be supported only with a documented minimum and MUST fail on zero or multiple matches. PID alone MUST NOT identify a process target.

### 7.2 Selectors

Shared typed selectors SHOULD include where applicable:

```text
--scope
--target
--profile
--since
--until
--severity
--state
--source
--limit
--format
```

Every command MUST reject unsupported selectors. Requested scope and effective authorized scope MUST be distinct typed values.

### 7.3 Execution classes

Commands MUST declare one of these classes per operation form:

- immediate read;
- bounded job;
- leased subscription;
- workflow mutation;
- lifecycle operation;
- export/egress operation.

Every job or subscription MUST return a stable ID and state. Interactive attachment MUST NOT be confused with operation ownership or persistence.

### 7.4 Result envelope

Every command result MUST contain or derive from one typed envelope with:

- schema version;
- canonical operation;
- request/operation ID where applicable;
- state and stable reason code;
- actor and active security-context references;
- requested and effective scope where applicable;
- timestamps;
- source/coverage/health metadata where applicable;
- bounded warnings;
- related object IDs;
- audit/receipt reference where required.

Terminal and machine renderers MUST consume the same semantic result. View code MUST NOT infer authority from prose.

### 7.5 Canonical security language

Aegis MUST distinguish:

- `observed`;
- `detected`;
- `denied by Aegis gateway`;
- `blocked by OS control`;
- `blocked by external control`;
- `mitigation requested`;
- `mitigation applied, unverified`;
- `mitigation confirmed`;
- `no findings in covered scope`.

“No findings” MUST NOT be rendered as safe, secure, clean, protected, or threat-free.

## 8. Global identity, authority, and lifecycle

Before service invocation, Aegis MUST verify:

- active session lifecycle permits the operation;
- authenticated subject is present and sufficiently fresh;
- exactly one trust stanza is active;
- mandate is unexpired and unrevoked;
- command/form is available;
- requested scope is authorized;
- arguments are typed and bounded;
- required service/source is healthy enough to attempt the operation.

Consequential workflow mutation or export MUST revalidate authoritative state immediately before the effect.

Slash commands MUST NOT:

- authenticate a subject;
- select or switch stanza;
- union capabilities;
- widen a mandate;
- grant delegation;
- approve a model/subagent proposal;
- change model, provider, runtime, or profile;
- bypass protected secret intake.

Once cleanup begins, new commands other than safe local status/exit handling as explicitly permitted MUST be rejected. Pending commands, jobs, subscriptions, approvals, and delegated work MUST converge on the one manager lifecycle.

## 9. `/help`

Bare `/help` MUST render state-aware base command and keyboard help from the registry.

Supported forms SHOULD include:

```text
/help
/help <command>
/help states
/help syntax
/help keyboard
/help aliases
```

Help MUST show canonical syntax, aliases, execution class, consequence marker, current availability, and safe reason for unavailability where disclosure permits. It MUST remain available in degraded mode and MUST NOT call Hermes or the network.

## 10. `/status`

Bare `/status` MUST answer what is happening now and whether the view is fresh enough to trust.

It MUST include, from authoritative state:

- identity/stanza/mandate summary;
- runtime and route health;
- source/sensor freshness and degraded state;
- foreground operation;
- active scans/watches/investigations/reports where implemented;
- finding counts by severity where implemented;
- pending approval or unverified outcomes;
- latest audit verification state where available.

Cached values MUST carry observation/staleness time. Unknown MUST remain distinct from stopped, healthy, or zero. Status MUST NOT mutate, repair, restart, provision, switch, or fall back.

## 11. `/context`

Bare `/context` MUST answer who and which immutable security context is active.

It MUST show:

- authenticated subject and provenance;
- logical agent;
- trust stanza;
- charter/policy revision and digest;
- mandate ID/issue/expiry/revocation state;
- Hermes runtime/adapter/session identity;
- current authorized default scan/watch scope;
- explicit isolation limitations.

`/context` MUST be read-only and MUST NOT switch identity, stanza, runtime, model, provider, deployment, or profile. Inconsistent authoritative context MUST stop consequential operations.

## 12. `/authority`

Bare `/authority` MUST answer what the current security context may observe, investigate, request, export, delegate, or stop.

It SHOULD group operations as:

- allowed now;
- approval required;
- unavailable by policy;
- unavailable by readiness;
- explicitly denied.

`/authority <operation>` MUST explain the controlling policy revision, scope, expiry, readiness, budget, delegation status, and stable reason. `/authority` MUST NOT have grant, elevate, switch, assume, or widening behavior.

## 13. `/limits`

Bare `/limits` MUST report known blind spots and implementation/resource constraints, including where applicable:

- missing/unhealthy sources;
- unsupported scopes/platforms;
- freshness/event loss;
- scan/watch bounds;
- retention limits;
- delegation/provider restrictions;
- report/export restrictions;
- audit/evidence-sink health;
- runtime-state-isolation and unmediated-route limitations.

Uncertain limits MUST be reported as uncertainty. Policy permission MUST NOT be presented as implementation capability.

## 14. `/scan`

### 14.1 Bare behavior

Bare `/scan` MUST be useful. It MUST run or attach to an equivalent running versioned `core` scan over the current authorized default scope.

The equivalence key MUST include owner, effective scope digest, profile/rule revision, source/policy generation, and relevant input snapshot identity. A different or stale scope MUST NOT deduplicate.

### 14.2 Required properties

A scan MUST be:

- read-only at the capability layer;
- bounded by time, volume, and concurrency;
- explicit about requested/effective scope;
- explicit about included/omitted modules;
- explicit about source/rule versions and health;
- cancellable;
- unable to expand scope from discovered assets;
- unable to install sensors, remediate, contain, mutate policy, or approve itself.

### 14.3 Core profile

The first operational `core` profile MUST use real Aegis-owned checks available in the implementation, prioritizing:

- runtime identity and effective configuration;
- stanza/mandate/authority consistency;
- tool, credential, memory, and broker scope;
- route/no-fallback state;
- configured versus effective artifacts;
- sensor/control readiness;
- audit-chain health.

Unavailable host modules MUST appear as coverage limits. They MUST NOT be simulated.

### 14.4 Nested forms

Forms MAY become available only with real adapters:

```text
/scan quick
/scan full --scope host
/scan secrets --scope workspace
/scan processes --scope agent-session
/scan network --scope agent-session
/scan files --scope workspace --path <selected-path>
/scan persistence --scope host
/scan permissions
/scan runtime
/scan dependencies --scope workspace
/scan configuration
/scan sensors
/scan status <scan-id>
/scan list
/scan diff <scan-id> <scan-id>
```

### 14.5 Outcomes

A scan MUST end as one of:

```text
completed_with_findings
completed_no_findings
partial
degraded
failed
cancelled
expired
```

Every result MUST report coverage, named gaps, source health, timing, observations processed, finding references, and collection failures. Secret candidates and unsafe raw evidence MUST NOT enter Hermes, transcript, audit, history, completion, or ordinary output.

## 15. `/watch`

### 15.1 Bare behavior

Bare `/watch` MUST idempotently ensure one default observation-only watch for the current authorized owner, scope, profile/rule revision, source set, and lease class, then display or attach to it.

A watch with a different scope or generation is not equivalent. Multiple equivalent matches MUST fail as inconsistent/ambiguous rather than silently choose.

### 15.2 Required properties

Every watch MUST have:

- stable watch ID;
- authenticated owner and stanza/mandate or separate service authority;
- exact effective scope;
- source/rule revisions;
- bounded lease and renewal policy;
- bounded buffers and retention;
- dropped-event/gap semantics;
- current source health and last-event time;
- explicit observation-only or enforcement mode;
- deterministic stop/expiry/revocation cleanup.

The base watch MUST be observation-only. It MUST NOT install sources, enable automatic response, or persist after session exit unless a separately authorized persistent service owns it and is shown explicitly.

### 15.3 Forms

```text
/watch
/watch start <profile> [selectors]
/watch list
/watch status [watch-id]
/watch events [watch-id] [time selectors]
/watch stop <watch-id>
```

Pause/resume/renew SHOULD be deferred until gap, lease, authority, and audit semantics are specified and tested.

Silence MUST NOT be represented as proof that nothing happened. Unknown loss MUST remain distinct from zero loss.

## 16. `/findings`

Bare `/findings` MUST list open findings visible to the current stanza using a documented stable sort and bounded pagination.

`/findings <id>` MUST show one typed finding containing:

- stable ID/schema/class;
- detector/rule identity and version;
- severity and confidence as distinct fields;
- workflow state/history;
- first/last observed times;
- typed target and source/scope;
- source health/coverage;
- bounded evidence references;
- scan/watch/investigation links;
- uncertainty and recommended safe actions.

Workflow forms MAY include acknowledge, note, classify, assign, suppress, and close only after typed authorization and append-preserving audit exist. Suppress/close MUST NOT delete finding or evidence history.

Attacker-influenced values MUST be structured, bounded, disclosure-filtered, and terminal-sanitized. Counts, completion, timing, and errors MUST NOT leak restricted findings.

## 17. `/investigate`

Bare `/investigate` MUST list active visible investigations or show concise usage when none exist. It MUST NOT silently select a finding or target.

`/investigate <finding-id>` MAY create or attach to a bounded investigation after exact target and authority resolution.

An investigation MUST bind:

- stable ID/state/owner;
- linked findings/targets;
- scope/time bounds;
- hypotheses and notes with provenance;
- evidence references;
- analysis/delegation tasks;
- open questions;
- timeline/report references;
- expiry/closure state.

Investigation is read-only over existing evidence by default. Active probing, new collection, cross-scope correlation, export, and response are separate typed operations.

## 18. `/timeline`

Bare `/timeline` MUST render a bounded recent view anchored to the explicitly attached investigation, otherwise the current session. The anchor and time range MUST be visible.

Timeline MUST be a query over authoritative event/control records, not a second mutable event store. Each entry MUST preserve event/observed/ingest time as available, source, ordering/clock caveat, target, provenance, and related IDs.

Source gaps, clock disagreement, event loss, stale ingestion, and disclosure omissions MUST remain visible. Model-assisted grouping MUST be labeled and MUST NOT alter authoritative timestamps or fields.

## 19. `/report`

Bare `/report` MUST create or preview a local draft for an explicitly attached investigation. If no eligible context exists, it MUST show eligible inputs rather than choose one. Bare `/report` MUST NOT export or publish.

A report MUST be a frozen versioned artifact with:

- exact input selectors/source digests;
- scope and coverage limits;
- findings and canonical states;
- deterministic evidence appendix;
- timeline/audit references;
- unresolved questions;
- redaction/disclosure policy;
- runtime/model/subagent provenance where used;
- integrity digest.

Later source changes MUST create a new report revision rather than rewrite the frozen artifact.

Model/subagent narrative MAY assist only from sanitized structured inputs. Authoritative fields and evidence appendix MUST remain deterministic. External export requires exact destination and payload policy, deterministic scanning/redaction, approval where required, and receipt; export is not authorized by this base implementation specification.

## 20. `/audit`

Bare `/audit` MUST show a bounded recent metadata-only view authorized for the current session/stanza. `/audit verify` MUST call the existing authoritative verification service.

Audit MUST remain distinct from timeline:

- timeline describes environmental and linked workflow events;
- audit describes authenticated requests, decisions, effects, verification, and receipts.

Audit rendering MUST preserve event IDs, actor, agent/stanza/mandate/policy/runtime references, canonical operation, target/scope digest, decision/reason, outcome, and delegation chain where applicable. It MUST exclude secrets, full prompts, capabilities, and unsafe raw external errors.

Model/subagents MUST NOT create or verify authoritative audit events.

## 21. `/cancel`

Bare `/cancel` MUST:

- request cancellation when exactly one unambiguous foreground cancellable operation exists;
- report no foreground operation and list safe candidates when none exists;
- list candidates and require an ID when multiple are plausible.

It MUST NOT use an implicit “most recent” guess.

Cancellation states MUST distinguish requested, confirmed, already terminal, failed, and unknown. Cancellation MUST NOT claim rollback, containment, revocation, evidence deletion, report recall, or watch-history deletion.

`/watch stop` and `/cancel <watch-id>` MUST call the same underlying terminal transition. Cancellation MUST propagate to delegated work and report child acknowledgement/timeouts through bounded metadata.

## 22. `/clear`

Bare `/clear` MUST clear/redraw only the local presentation and state explicitly that session authority, Hermes state, operations, watches, findings, investigations, reports, and audit are unchanged.

It MUST NOT claim to erase terminal-emulator scrollback it cannot control. It MUST NOT clear model context, transcript retention, evidence, authority, or audit. Plain/accessibility mode MUST use a line-oriented boundary rather than unsafe cursor controls.

## 23. `/exit`

`/exit` and `/quit` MUST enter the one manager cleanup path.

Cleanup MUST:

- reject new work;
- cancel foreground and delegated work;
- cancel approval/protected intake and restore terminal state;
- stop session-owned watches;
- close Hermes/gateway/proxy/inference resources;
- remove disposable state;
- invalidate capabilities;
- finalize metadata-only receipt/audit;
- restore terminal exactly once;
- report separately owned persistent resources truthfully.

Exit MUST NOT trap the operator because cleanup failed. Failures MUST produce a stable nonzero result and exact residual state.

## 24. Subagent and model boundary

Subagents MAY assist only inside a typed parent operation. Every delegation MUST include:

- parent operation and delegation IDs;
- explicit specialist purpose;
- bounded attenuated data scope;
- explicit tool/capability manifest;
- provider/model/region policy;
- retention/disclosure policy;
- maximum depth/fan-out;
- cancellation/expiry propagation;
- structured result schema;
- complete provenance.

Subagents MUST NOT inherit by default:

- parent tools;
- credentials or bearer capabilities;
- approval;
- slash dispatch;
- audit append/verify authority;
- response authority;
- recursive delegation;
- full transcript or raw evidence.

Subagent output is untrusted analysis. Deterministic Aegis code MUST validate any promoted claim, finding candidate, correlation, or report content. A subagent MUST NOT mark a scan complete, create an authoritative finding, alter a timeline, approve/export a report, or claim mitigation success.

No subagent is required for the initial operational base-command slice. Adding subagents before typed operation, data, authority, and provenance boundaries exist is prohibited.

## 25. Presentation, privacy, and accessibility

Command results MUST enter the typed presentation controller. Background services and delegated workers MUST NOT write directly to the terminal.

Untrusted values MUST be sanitized before measurement/rendering. The implementation MUST neutralize terminal control injection, carriage-return rewriting, unsafe bidi/invisible controls in security-sensitive fields, malformed text, giant tokens, and forged Aegis chrome.

Sensitive surfaces include:

- command history and reverse search;
- completion indexes;
- terminal scrollback and recordings;
- transcript state;
- logs/audit;
- crash output;
- findings/evidence;
- report artifacts;
- model/subagent context.

Protected values MUST never be ordinary command arguments.

All states and severity MUST be legible without color. Rich, plain/accessibility, and machine renderers MUST preserve semantic distinctions and complete keyboard operation.

## 26. Architecture requirements

The implementation SHOULD introduce focused packages/interfaces for:

```text
slash command registry/parser/dispatcher
operation lifecycle and cancellation
scan orchestrator and modules
watch manager and sources
finding store/service
investigation service
timeline query
report generation
```

Exact package names MAY differ after tracing current code. Requirements:

- no package-level mutable commands/registries;
- constructor-built dependencies;
- context cancellation throughout;
- injected clocks/ID sources/storage/adapters for tests;
- strict typed validation;
- shared application services across TUI/CLI/API where exposed;
- authoritative state outside view code;
- no invocation of Aegis's own CLI as a subprocess;
- no duplicate audit or policy engine;
- bounded stores and retention;
- migration/version handling for durable schemas.

The implementation MUST preserve current uncommitted TUI/lifecycle work and integrate with `internal/command/manager.go`, `internal/command/manager_startup.go`, `internal/tui`, manager lifecycle, audit, identity, policy, runtime, and application services rather than build a second manager.

## 27. Required tests

### 27.1 Registry and parser

- exactly 15 canonical base names;
- aliases canonicalize to one handler/policy/audit name;
- help/completion/dispatch derive from one registry;
- command after allowed leading whitespace;
- `//` literal escape;
- exact matching and suggestion-only fuzzy/prefix behavior;
- quoted arguments and malformed input;
- no shell expansion/chaining/redirects;
- byte/rune/value bounds;
- unknown slash never reaches ingress/Hermes;
- model/tool/subagent/sensor output cannot invoke dispatch.

### 27.2 Availability and authority

- startup/degraded/active/closing command availability;
- unauthenticated/expired/revoked/ambiguous denial;
- no stanza or authority switching;
- requested/effective scope distinction;
- truthful unavailable capability;
- no unauthorized target/count/completion leakage;
- revalidation before workflow mutation/export;
- delegation denied unless separately allowed.

### 27.3 Commands

Add focused tests for every bare command and every implemented nested form, including:

- registry-derived help and status freshness;
- context/authority/limits source correctness;
- core scan coverage/outcomes/idempotency/cancellation;
- watch equivalence/lease/gap/cleanup when implemented;
- finding disclosure and state history;
- no silent investigation target;
- timeline ordering caveats;
- frozen report revision/digest and no bare export;
- audit verification;
- unambiguous cancellation and no rollback claim;
- display-only clear;
- one bounded exit/terminal restoration path.

### 27.4 Subagents

If subagents are implemented, test:

- attenuated manifests;
- no inherited credentials/tools/approval;
- depth/fan-out limits;
- cancellation/expiry propagation;
- structured untrusted results;
- no slash dispatch or authoritative event creation;
- full delegation provenance;
- secret/evidence absence from unauthorized contexts.

### 27.5 Integration and PTY

Tests MUST exercise the real manager command path with isolated HOME/config/state/audit/runtime fixtures. Required cases include narrow terminal, no color, accessible/plain mode, resize, Ctrl-C, Esc, EOF, SIGINT/SIGTERM, terminal loss, startup queue, degraded runtime, cleanup, and output sanitization.

No automated test may install a real sensor, modify the operator's Hermes/Ollama state, use real credentials, publish a report, or claim real host coverage from fixtures.

## 28. Documentation and launch assets

Every implemented slice MUST update and exercise affected sources of truth:

- root `README.md`;
- command/help reference;
- `docs/QUICKSTART.md`;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md` and recording source;
- `docs/ARCHITECTURE.md` and diagram;
- `docs/THREAT_MODEL.md`;
- `SECURITY.md`;
- `CHANGELOG.md`;
- contributor issue material;
- release binaries/checksums only for an explicitly authorized release.

Documentation MUST distinguish implemented, unavailable, degraded, and future adapter behavior. It MUST NOT present this specification or research as shipped functionality.

`LICENSE`, `CONTRIBUTING.md`, and `CODE_OF_CONDUCT.md` MUST be reviewed for impact. They MUST be edited only when actually affected.

## 29. Definition of Done

The base-command implementation is complete only when:

1. the real manager recognizes the Core 15 through one typed registry;
2. command-looking user input cannot bypass local dispatch to Hermes;
3. unknown commands fail locally and literal slash works through the ordinary guard;
4. help, completion, dispatch, availability, policy, and audit cannot drift into separate lists;
5. every bare command has the behavior specified here or a truthful typed unavailable result backed by real readiness checks;
6. `/scan` has a real bounded Aegis-native core profile before it claims success;
7. `/watch` claims active only when a real leased source manager exists;
8. no finding, investigation, timeline, or report is fabricated from model narration;
9. cancellation, clear, and exit semantics are unambiguous and PTY-tested;
10. subagents, if present, are attenuated, attributable, cancellable, and non-authoritative;
11. focused, integration, PTY, race, full Go test, vet, build, and diff checks pass;
12. locally exercisable documentation workflows are run successfully;
13. launch assets accurately describe only implemented behavior;
14. no operator state, external service, release, issue, profile, sensor, model store, or credential is modified without explicit authorization.

If a required service is not implementable in the current slice, the implementation MUST stop at a truthful unavailable boundary, retain tests for that boundary, and report the exact remaining dependency. It MUST not use a placeholder success or weaken the requirement.