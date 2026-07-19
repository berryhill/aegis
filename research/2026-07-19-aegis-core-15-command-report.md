# Aegis Core 15 command research and product report

**Research date:** 2026-07-19 UTC  
**Status:** Product, security, and implementation research; not a normative implementation specification  
**Scope:** The complete proposed behavior of Aegis's 15 core terminal slash commands, including online primary-source research and independent subagent review  
**Authorization boundary:** This report does not authorize endpoint collection, sensor installation, profile changes, provisioning, activation, external reporting, or response actions

## Executive decision

Aegis should expose exactly 15 stable top-level slash commands:

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

These commands form one security workflow:

```text
orient
  /help /status /context /authority /limits

observe
  /scan /watch

triage and analyze
  /findings /investigate /timeline

communicate and verify
  /report /audit

control the terminal lifecycle
  /cancel /clear /exit
```

The two flagship commands are deliberately useful in bare form:

- Bare `/scan` starts or attaches to a bounded, read-only, versioned `core` posture scan in the current authorized default scope.
- Bare `/watch` idempotently ensures one leased, observation-only default watch exists for the current authorized default scope, then attaches to or displays it.

A slash command remains a deterministic local Aegis operation. It is consumed before Hermes, parsed exactly into a closed typed request, authorized outside the model, and rendered from typed results. The slash text is not authentication, authority, approval, or an instruction for the model to imitate.

The core 15 are a vocabulary, not 15 monolithic code paths. Each command fronts shared application services, typed selectors, operation IDs, lifecycle state, disclosure policy, and authoritative audit. Convenience aliases such as `/scan-secrets` may map to canonical operations such as `scan.secrets`, but aliases must not create separate implementations or policy names.

## 1. Research method

### 1.1 Repository research

The report builds on and cross-checks:

- `AGENTS.md`
- `docs/product/BIG_IDEA.md`
- `specs/MVP.md`
- `specs/BASE_MANAGER_END_TO_END.md`
- `specs/TERMINAL_EXPERIENCE_OPERATIONAL_IMPLEMENTATION.md`
- `research/2026-07-19-aegis-security-slash-command-model.md`
- `research/2026-07-19-terminal-experience-best-of-hermes-openclaw-claude-code.md`
- `research/2026-07-17-host-endpoint-monitoring-enforcement-response.md`
- `research/2026-07-17-mcp-agent-skills-secret-exposure-addendum.md`
- `internal/command/manager.go`
- `internal/command/manager_startup.go`
- relevant manager command tests under `internal/command/`

The current manager already consumes recognized local slash directives before ordinary ingress and Hermes. The proposed core 15 is not implemented. In particular, Aegis does not currently provide endpoint scan, watch, durable finding, investigation, or report services.

### 1.2 Online primary sources

The research directly retrieved and inspected official or upstream sources in these groups.

#### Interactive command design

- Claude Code interactive mode: https://code.claude.com/docs/en/interactive-mode
- OpenAI Codex developer/slash commands: https://developers.openai.com/codex/cli/slash-commands/
- Hermes Agent CLI guide: https://hermes-agent.nousresearch.com/docs/user-guide/cli/
- Gemini CLI commands: https://google-gemini.github.io/gemini-cli/docs/cli/commands.html
- Command Line Interface Guidelines: https://clig.dev/

Useful patterns include slash-command discovery, autocomplete, status inspection, multiline terminal interaction, interruption, progressive disclosure, and state-aware command surfaces.

Aegis should not copy several adjacent patterns:

- Hermes user-defined shell-backed quick commands must not shadow or populate Aegis's authoritative built-in command registry.
- Gemini's `!` shell mode is inappropriate for the built-in Aegis manager.
- Runtime/model/permission mode switching must not imply that slash text can alter an Aegis stanza or mandate.
- Dynamic skills may remain visible as Hermes features, but they are not automatically Aegis-authoritative commands.

#### Incident response and logs

- NIST SP 800-61 Rev. 3: https://csrc.nist.gov/pubs/sp/800/61/r3/final
- NIST SP 800-92: https://csrc.nist.gov/pubs/sp/800/92/final
- OWASP Logging Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html

These sources support a lifecycle that integrates preparation, detection, response, recovery, and improvement rather than treating scans as isolated tools. They also reinforce structured log management, event provenance, retention, access control, sanitization, and protection against sensitive-data leakage and log injection.

#### Detection, scanning, and telemetry

- Falco event-source documentation: https://falco.org/docs/concepts/event-sources/
- Gitleaks: https://github.com/gitleaks/gitleaks
- Trivy documentation: https://trivy.dev/docs/latest/
- OSV-Scanner documentation: https://google.github.io/osv-scanner/
- MITRE ATT&CK: https://attack.mitre.org/

The tools demonstrate that “scan” is not one technology:

- secret-pattern scanning;
- dependency vulnerability matching;
- filesystem and configuration inspection;
- runtime event collection;
- known-rule detection;
- behavioral or correlated detection.

Aegis therefore needs profiles and modules with explicit sources, coverage, versions, limits, and outcomes. It must never collapse all of these into an unsupported claim that a machine has been comprehensively scanned for all threats.

#### Findings and event schemas

- SARIF 2.1.0: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html
- Open Cybersecurity Schema Framework: https://schema.ocsf.io/
- OpenTelemetry Logs Data Model: https://opentelemetry.io/docs/specs/otel/logs/data-model/
- Elastic Common Schema event fields: https://www.elastic.co/guide/en/ecs/current/ecs-event.html

These sources support typed result identity, tool/rule provenance, timestamps, severity/confidence separation, event categories, source metadata, and interoperable finding classes. Aegis should use adapters at ingestion/export boundaries, not replace its authoritative internal model with any one external schema.

#### Agent and subagent behavior

- Claude Code subagents: https://docs.anthropic.com/en/docs/claude-code/sub-agents
- OpenAI Agents SDK handoffs: https://openai.github.io/openai-agents-python/handoffs/
- NIST AI Risk Management Framework: https://www.nist.gov/itl/ai-risk-management-framework
- OWASP GenAI Security Project: https://genai.owasp.org/

The retrieved Claude documentation describes subagents with separate context windows, specialized prompts, tool access, and permissions. OpenAI handoffs model transfer or delegation among agents. Those are useful orchestration concepts, but Aegis must reject implicit authority inheritance: a parent model's context, tools, credentials, approval, and stanza authority do not automatically become a child's.

#### Lifecycle and accessibility

- Go `context`: https://pkg.go.dev/context
- WCAG 2.2, Use of Color: https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html

These support propagated cancellation, bounded operation lifecycles, and terminal semantics that do not rely on color alone.

### 1.3 Independent subagent review

Three independent read-only Claude research workers were run in parallel with safe mode, no tools, no repository writes, no session persistence, and bounded budgets:

1. **Security architecture reviewer** — authority boundaries, degraded behavior, privacy, command ambiguities, and delegation risk.
2. **SOC/incident-response UX reviewer** — analyst workflow, status vocabulary, accessibility, findings/timeline/report integration.
3. **CLI/API language reviewer** — grammar, execution classes, selectors, result schemas, IDs, idempotency, and completion.

All three returned usable reports. No Hermes one-shot worker was used because Hermes one-shot mode auto-bypasses approvals and the project explicitly rejects that mode for approval-sensitive design work. The subagent outputs were advisory and were independently reconciled against Aegis invariants and primary sources.

Important disagreements were resolved as follows:

- `/clear` clears display only; it does not clear model context, evidence, findings, audit, or authority.
- `/exit` stops session-owned watches unless a separately authorized persistent service owns them; watches do not silently persist.
- Bare `/investigate` lists or shows active investigations; it does not silently pick the “most severe” target.
- Bare `/cancel` cancels only when exactly one unambiguous foreground operation exists; otherwise it lists cancellable operations and requires an ID.
- `/watch stop` remains a discoverable nested form even if it shares the same underlying cancellation service as `/cancel`.
- A report is a frozen, attributable artifact generated from records. It may include model-assisted narrative, but evidence and authoritative state are not model-generated.

## 2. Global command contract

### 2.1 Routing

```text
submitted input
  -> raw size and encoding bound
  -> command-position detection
     -> recognized or unknown slash input
        -> local exact parser
        -> typed registry lookup
        -> lifecycle/readiness check
        -> identity/stanza/mandate authorization
        -> shared Aegis application service
        -> typed result, job, subscription, or approval
        -> presentation event
        -> authoritative audit where required
     -> escaped slash or ordinary text
        -> normal Aegis ingress guard
        -> Hermes/model lane if allowed
```

Unknown slash input fails locally and never falls through to Hermes. Model output, sensor text, tool output, pasted transcript content, subagent output, and report content cannot invoke the slash dispatcher.

### 2.2 Grammar

Recommended initial grammar:

```text
/<command> [<subcommand>] [<typed positional> ...] [--flag value | --flag=value] ...
```

Rules:

- recognize `/` at the first non-whitespace character;
- lowercase exact command and subcommand matching;
- no prefix execution or fuzzy execution;
- suggestions never auto-execute;
- explicit aliases canonicalize before policy and audit;
- quoted literal arguments are supported through a documented non-shell parser;
- unknown and duplicate singleton flags fail;
- no environment expansion, glob expansion, command substitution, pipes, redirects, chaining, background shell semantics, or `!` shell mode;
- multiline commands are rejected initially unless a specific schema permits a protected editor/intake flow;
- `//text` is the recommended escape for conversational content beginning `/text`;
- scripts and machine clients should use the shared application API rather than scrape TUI text.

### 2.3 Shared selectors

The command registry should reuse typed selectors:

```text
--scope <agent-session|workspace|workload|host|deployment|fleet>
--target <typed-reference>
--profile <versioned-profile-id>
--since <RFC3339 timestamp|bounded duration>
--until <RFC3339 timestamp>
--severity <informational|low|medium|high|critical>
--state <typed-state>
--source <source-id>
--limit <bounded-count>
--format <terminal|json|sarif|ocsf|... when supported>
```

Not every selector applies to every command. Unsupported selectors fail instead of being ignored.

Scope names alone are insufficient. A resolved scope must include an immutable typed target set or selector digest, authorization decision, source policy, and disclosure policy.

### 2.4 Stable typed IDs

Recommended object classes:

```text
operation ID
scan ID
watch ID
finding ID
investigation ID
report ID
incident ID, when incidents exist
audit event ID
receipt ID
process/workload/artifact references
```

IDs should be opaque, stable, non-secret, and visibly typed. Prefix abbreviation may be accepted only after unique resolution; zero or multiple matches fail closed. A process reference is not a PID alone.

### 2.5 Execution classes

- **Immediate query:** help, status, context, authority, limits, finding lookup, timeline lookup, audit lookup, clear.
- **Bounded job:** scan, investigation analysis, report generation.
- **Subscription/leased resource:** watch.
- **Lifecycle operation:** cancel and exit.
- **Workflow mutation:** finding acknowledgement/classification/note/suppression and report export.
- **Future response proposal:** specific contextual action exposed through a finding or investigation; not a generic core command.

Every non-immediate operation returns an operation ID. Interactive mode may attach to it, but attachment and operation ownership are distinct.

### 2.6 Common result envelope

Every command should produce a typed internal result with at least:

```text
schema version
canonical operation
request/operation ID
result state
actor and active security-context references
requested and effective scope
started/updated/completed timestamps
source and coverage metadata
bounded warnings
stable reason codes
object references
receipt/audit reference where applicable
```

Machine output and terminal output render the same typed result. The terminal must not parse security state back from prose.

### 2.7 Canonical status vocabulary

Aegis should use precise terms consistently:

- **observed** — telemetry indicates an event or state.
- **detected** — an identified rule/correlation matched and created or updated a finding.
- **denied by Aegis gateway** — Aegis refused a synchronously mediated request.
- **blocked by OS control** — a validated operating-system mechanism confirmed prevention.
- **blocked by external control** — an integrated control confirmed prevention.
- **mitigation requested** — a response was requested; no successful change is claimed.
- **mitigation applied, unverified** — execution returned success but required postcondition verification is unavailable or failed.
- **mitigation confirmed** — the target was rechecked and the expected outcome was observed.
- **no findings in covered scope** — enabled detectors found nothing in the declared effective coverage.

Never substitute “safe,” “secure,” “clean host,” “protected,” or “contained” without the required evidence.

### 2.8 State-aware help and completion

Completion is generated from the typed registry and local authoritative indexes. It must not trigger network calls, scans, model turns, or filesystem traversal.

Examples:

- `/cancel <Tab>` shows only cancellable operation IDs.
- `/investigate <Tab>` shows only findings the caller may discover and investigate.
- `/watch stop <Tab>` shows only watches the caller may stop.
- `--scope <Tab>` shows only authorized, safely disclosable scopes.
- unavailable commands or forms show a stable reason where disclosure policy permits.

### 2.9 Subagent contract

Subagents may assist inside selected application operations, but they never receive slash-dispatch authority.

Every delegated task requires:

- parent operation ID;
- delegation ID and depth;
- explicit specialist purpose;
- attenuated data scope;
- explicit tool/capability manifest;
- explicit provider/model/region policy;
- no ambient credentials;
- no inherited approval;
- no recursive delegation unless separately allowed;
- retention and disclosure policy;
- cancellation and expiry propagation;
- structured result schema;
- complete delegation provenance.

Subagent output is untrusted analysis. It cannot create authoritative findings, mark mitigation successful, alter audit, or execute a slash command. Deterministic Aegis code validates and promotes accepted evidence or proposals into authoritative records.

## 3. Core workflow and object relationships

```text
/scan --------------------+
                           +--> observations --> detector --> findings
/watch -------------------+                         |
                                                     v
/findings --> /investigate --> /timeline --> /report
     |              |              |            |
     +--------------+--------------+------------+
                            |
                          /audit
```

Distinctions:

- A scan is a bounded snapshot job.
- A watch is a leased observation subscription.
- A finding is a durable typed detector/correlation result.
- An investigation is a bounded workspace and analysis lifecycle linked to findings and evidence.
- A timeline is a query/rendering of ordered events; it is not a second event store.
- A report is a frozen generated artifact with source digests and provenance.
- Audit records what Aegis and authenticated actors requested, decided, and did. It is not interchangeable with environmental telemetry.

## 4. `/help`

### Purpose

Make the implemented, currently available command surface discoverable without asking Hermes.

### Bare behavior

`/help` opens one state-aware index grouped as orientation, observation, analysis, communication, and lifecycle. It shows keyboard controls, literal slash escaping, command versus conversation behavior, and consequence markers.

### Forms

```text
/help
/help <command>
/help <command> <subcommand>
/help states
/help syntax
/help keyboard
/help aliases
```

### Required output

- canonical name and aliases;
- short purpose;
- exact usage and examples;
- execution class;
- current availability;
- required scope/capability;
- mutation/egress/approval marker;
- degraded-mode behavior;
- cancellation behavior;
- source/coverage semantics where applicable.

### Authority and privacy

Help does not grant capability. It should show the stable core vocabulary while avoiding unauthorized target enumeration. A command can appear as unavailable with a safe reason rather than disappearing in a way that makes documentation inconsistent.

### Subagent role

None. Help is registry-generated. A model may not invent available commands or syntax.

### Failure behavior

Help must remain available offline and during degraded runtime state. If registry state is internally inconsistent, show an Aegis diagnostic and do not advertise uncertain consequential forms.

### Non-behavior

- no model call;
- no network call;
- no command execution from examples;
- no dynamic shell-backed built-in commands;
- no user alias shadowing.

## 5. `/status`

### Purpose

Answer: **What is happening now, and can I trust the current view?**

### Bare behavior

Show a concise current posture:

- authenticated principal and stanza summary;
- mandate remaining lifetime/state;
- Hermes/runtime and local route health;
- source/sensor freshness and degraded state;
- foreground operation;
- active scans and watches;
- open findings by severity;
- active investigations;
- pending approvals or unverified outcomes;
- latest audit verification state.

Persistent trust context remains visible outside `/status`; the command provides detail rather than being the sole source.

### Forms

```text
/status
/status --details
/status <operation-id|scan-id|watch-id|investigation-id|report-id>
/status sources
/status operations
/status watches
```

### Output semantics

Freshness is mandatory. Cached fields carry `observed_at` and `stale_since`. “Unknown” is distinct from “stopped,” “healthy,” or “zero.”

### Authority and privacy

Default output is current-session/current-stanza scoped. Cross-session or fleet status requires explicit scope and disclosure authorization. Status must never include capabilities, tokens, raw prompts, secret values, or unrestricted process command lines.

### Subagent role

None in the authoritative path. Optional summaries may exist, but authoritative fields come from services and state stores.

### Failure behavior

Status remains useful in degraded mode. It reports exact unavailable components and never triggers repair, restart, fallback, provisioning, or model switching.

### Audit

Status reads can emit bounded metadata-only audit when required by policy. Repeated polling should be coalesced or sampled to avoid making the audit log unusable.

## 6. `/context`

### Purpose

Answer: **Who am I operating as, in which immutable security context, over what resolved scope and runtime?**

### Bare behavior

Display:

- authenticated subject and authentication provenance;
- logical agent;
- trust stanza/security context;
- charter/policy revision and digest;
- mandate ID, issue time, expiry, and revocation state;
- Hermes runtime/version/adapter/session identity;
- current authorized default scan/watch scope;
- active deployment/machine-policy references where endpoint support exists;
- explicit runtime-state-isolation, non-sandbox limitation.

### Forms

```text
/context
/context --details
/context identity
/context scope
/context runtime
/context policy
```

### Boundary with other commands

- `/status`: what is happening now.
- `/context`: who/where/which immutable security context.
- `/authority`: what operations that context may request.
- `/limits`: what cannot be seen or what budgets constrain it.

### Authority and privacy

Context is read-only. It cannot switch stanza, runtime, model, provider, deployment, or profile. Any material context change requires a new mandate and clean session.

### Subagent role

None. A subagent may receive an attenuated context manifest, but cannot populate the authoritative parent context display.

### Failure behavior

If active context cannot be reconciled with authoritative session state, show `inconsistent` and stop consequential operations. Do not guess from logs or model text.

## 7. `/authority`

### Purpose

Answer: **What may this authenticated security context observe, investigate, request, export, or stop?**

### Bare behavior

Show a table grouped by operation class:

```text
allowed now
approval required
unavailable by policy
unavailable by readiness
explicitly denied
```

Each entry includes source policy, scope, expiry, and whether delegation is allowed.

### Forms

```text
/authority
/authority <canonical-operation>
/authority scan
/authority watch
/authority investigate
/authority report
/authority delegation
```

### Security invariants

- strictly read-only;
- no `/authority grant`, `elevate`, `switch`, or `assume` form;
- display names and slash arguments are not authentication;
- mandate refresh cannot widen authority invisibly;
- exact operation authorization is rechecked at execution time;
- parent authority does not imply delegation authority.

### Output

For one operation, explain:

- allow/deny/approval-required decision;
- actor and stanza;
- requested/effective scopes;
- controlling rule/policy revision;
- capability/readiness prerequisites;
- remaining budget;
- delegation permission and depth;
- stable reason code.

### Subagent role

None in policy decision. Subagents cannot interpret policy into authority. A child receives an Aegis-issued attenuated manifest after deterministic authorization.

### Failure behavior

Cached authority may be displayed as stale, but consequential commands fail closed if live authorization/revalidation is required and unavailable.

## 8. `/limits`

### Purpose

Answer: **What is Aegis unable to observe, establish, retain, correlate, delegate, or enforce, and which budgets are exhausted?**

### Bare behavior

Show current blind spots and quantitative constraints:

- unavailable or unhealthy sensors;
- unsupported platforms/scopes;
- source freshness and event loss;
- scan time/volume/concurrency budgets;
- watch count, lease, buffer, and retention limits;
- finding/evidence retention;
- report size/export restrictions;
- model/subagent provider and delegation restrictions;
- runtime-state-isolation versus host-sandbox limitations;
- unmediated shell/filesystem/network routes;
- audit/evidence sink health.

### Forms

```text
/limits
/limits scan
/limits watch
/limits findings
/limits investigate
/limits report
/limits delegation
```

### Boundary with authority

Authority describes what policy permits. Limits describe what implementation, sensors, budgets, health, and coverage can actually support. A permitted host scan can still be unavailable because no host sensor exists.

### Subagent role

None for authoritative limits. Model-generated caveats can supplement, never override, deterministic coverage limitations.

### Failure behavior

If limits cannot be established, that uncertainty is itself a limit and should block claims of complete coverage.

## 9. `/scan`

### Purpose

Run a bounded snapshot that collects declared observations and applies versioned deterministic detectors across an explicit authorized scope.

### Bare behavior

Bare `/scan` desugars to a versioned `core` scan over the current authorized default scope. It is read-only and bounded. It starts a new job or returns/attaches to an equivalent running job using a canonical idempotency key.

The key includes at least:

- authenticated operation owner;
- effective scope digest;
- profile ID and revision;
- source/policy generation;
- relevant input snapshot identity where available.

It must never attach to a scan with an old or different scope merely because both were invoked as `/scan`.

### Canonical forms

```text
/scan
/scan core
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

Convenience aliases canonicalize:

```text
/scan-secrets   -> /scan secrets
/scan-processes -> /scan processes
/scan-network   -> /scan network
/scan-files     -> /scan files
```

### `core` profile

The first useful aggregate should prioritize Aegis-native checks:

- runtime executable/version/process identity;
- charter, stanza, mandate, and effective capability consistency;
- tool, credential, memory, and broker scope;
- route and no-fallback state;
- configured versus effective runtime artifacts;
- sensor/control health;
- audit-chain health;
- available narrow workspace/session scans.

Host-wide modules join only when a real endpoint service exists. Omitted modules are listed.

### Module semantics

#### Secrets

Use deterministic detectors and approved scanners. Do not print candidates. Retain detector/rule, bounded location metadata, class, confidence, and safe deduplication metadata only under policy. Secret bytes never enter Hermes, subagent prompts, transcript, logs, audit, reports, history, or completion.

#### Processes

Use stable process references including deployment, PID, process start identity, executable identity, OS identity, and ancestry. Check runtime drift, unexpected children, privilege, deleted/replaced executables, service/cgroup/namespace relationships, and policy mismatch. PID alone is never a response target.

#### Network

Distinguish socket snapshot, event history, DNS/destination telemetry, gateway denial, OS block, and external-control confirmation. A snapshot cannot prove absence of transient activity.

#### Files and persistence

Operate over selected paths and declared mechanisms. Prefer metadata, identity, digest, package provenance, ownership, mode, approved baseline relation, and observing process. List unsupported persistence mechanisms and permission failures.

#### Dependencies

Use lockfile/package provenance and vulnerability databases through pinned adapters. Report database snapshot/version and ecosystem coverage. A vulnerability match is not proof of exploitability; an absent match is not proof of safety.

#### Runtime and permissions

Compare approved Aegis authority/runtime mappings with observed effective state. This is a primary differentiator and can exist before broad endpoint collection.

### Preflight

Broad or costly scans show:

- exact requested and effective scope;
- modules/sources;
- excluded data classes;
- observation-only state;
- expected bounds;
- retention;
- provider/subagent use;
- whether any evidence may leave the host.

### Results

Required outcomes:

```text
completed_with_findings
completed_no_findings
partial
degraded
failed
cancelled
expired
```

Required summary:

- scan ID;
- requested/effective scope;
- profile/rule/source revisions;
- coverage and named gaps;
- source health;
- start/end/duration;
- observations processed;
- findings created/updated;
- timeouts/permission denials/event loss;
- next safe actions.

### Authority

Read-only must be capability-enforced. Scope is pinned at start. Mandate narrowing/revocation aborts affected work; authority cannot widen mid-scan. Discovered assets do not automatically enter scope.

### Subagent role

Permitted uses:

- shard independent modules or authorized targets;
- summarize already sanitized evidence;
- correlate references under a bounded hypothesis;
- evaluate scanner-result quality.

Requirements:

- one attenuated task manifest per shard;
- no shell/network/credential inheritance by default;
- no scope expansion from discovered assets;
- structured result and source provenance;
- no raw secret candidate or unrestricted file content;
- deterministic promotion of results into findings;
- cancellation and mandate expiry propagation.

### Non-behavior

A scan does not install sensors, remediate, contain, mutate policy, approve itself, or certify that the endpoint is safe.

## 10. `/watch`

### Purpose

Maintain a leased, bounded observation subscription over declared event sources and detectors.

### Bare behavior

Bare `/watch` means:

> Ensure one default observation-only watch exists for the current authorized default scope and profile, then attach to or display it.

The equivalence/idempotency key includes owner, scope digest, profile/rule revision, source set, and lease class.

### Forms

```text
/watch
/watch start threats --scope agent-session
/watch start processes --scope agent-session
/watch start network --scope host --lease 1h
/watch start finding <finding-id> --lease 30m
/watch list
/watch status [watch-id]
/watch events [watch-id] --since 10m
/watch pause <watch-id>
/watch resume <watch-id>
/watch renew <watch-id> --lease 1h
/watch stop <watch-id>
```

Pause/resume/renew should be omitted from the first release unless their authority and event-gap semantics are fully specified. `/watch stop` and `/cancel <watch-id>` call one underlying terminal transition.

### Watch lifecycle

```text
created -> starting -> active -> degraded -> stopping -> stopped
                |          |          |           |
                +----------+----------+-> failed/cancelled/expired/revoked
```

A watch binds:

- authenticated owner;
- stanza and mandate epoch or separate persistent authority;
- exact scope;
- source/rule revisions;
- lease and renewal policy;
- buffer/retention policy;
- watch ID and idempotency key;
- audit provenance.

### Output

```text
Watch: active
Watch ID: watch_...
Scope: current Aegis agent session
Mode: observation only
Sources: Aegis authority, broker, runtime process
Detectors: authority drift, broker denial burst, runtime identity drift
Unavailable: host persistence events, packet telemetry
Started: <timestamp>
Lease remaining: 42m
Last event: 4s ago
Dropped events: 0 reported
Findings created/updated: 2/5
```

“0 reported” is not equivalent to proof that zero events were dropped if the source cannot provide a loss counter.

### Backpressure and gaps

Specify:

- bounded input/output buffers;
- coalescing and deduplication;
- dropped-event accounting;
- reconnect cursor/checkpoint behavior;
- ordering guarantees;
- source clock confidence;
- behavior during evidence-sink failure;
- gap events that create visible degraded state.

Silence cannot be interpreted as “nothing happened.”

### Authority

Observation-only is capability-enforced. Bare watch cannot install kernel drivers, enable OS audit, create a system service, alter firewall rules, persist after exit, or enable automatic response.

Session-owned watches stop on cleanup. Persistence requires a separately authorized service, identity, policy, retention, and receipt. Mandate expiry or revocation stops or narrows the watch deterministically; it does not leave an orphaned collector.

### Subagent role

Subagents must not sit in the authoritative event-ingest or hot enforcement path. They may:

- summarize batches after deterministic filtering;
- correlate bounded event references;
- recommend a finding or investigation;
- explain an existing detector match.

Persistent subagent loops are not the watch. The watch is an Aegis-managed service resource. Child analysis receives no inherited credentials/approval, cannot renew its own lease, and returns structured untrusted analysis.

### Non-behavior

Watch is not prevention, containment, automatic response, comprehensive host coverage, or a promise to run forever.

## 11. `/findings`

### Purpose

Provide the durable triage queue of versioned detector/correlation results.

### Bare behavior

List open findings visible to the current stanza, sorted by severity and recency using a documented stable order.

### Forms

```text
/findings
/findings <finding-id>
/findings --severity high
/findings --state open --since 24h
/findings --source <source-id>
/findings --scan <scan-id>
/findings --watch <watch-id>
/findings <finding-id> acknowledge
/findings <finding-id> note <bounded-text>
/findings <finding-id> classify <classification>
/findings <finding-id> assign <subject-ref>
/findings <finding-id> suppress --until <time> --reason <bounded-text>
/findings <finding-id> close --reason <bounded-text>
```

Workflow mutations should be phased after read-only listing/detail. They require typed authorization and audit.

### Finding schema

At minimum:

- stable finding ID and schema version;
- finding class;
- title and bounded summary;
- detector/rule ID and version;
- severity and confidence as distinct values;
- workflow state;
- first/last observed and detector timestamps;
- typed target reference;
- source and effective scope;
- required sensors and source health;
- evidence references, not unrestricted evidence bodies;
- scan/watch/investigation links;
- identity/stanza/mandate correlation where relevant;
- uncertainty, coverage limits, and known bypasses;
- recommended next safe actions;
- complete state transition history.

OCSF/SARIF adapters may map applicable fields, but Aegis retains its own authority and provenance fields.

### State model

A conservative initial workflow:

```text
open -> acknowledged -> investigating -> resolved/closed
  |          |              |
  +----------+--------------+-> suppressed until bounded expiry
```

Detection truth and workflow state are distinct. Closing or suppressing does not delete the finding, evidence references, or audit history.

### Injection and privacy

Finding titles, paths, process names, destinations, external tool messages, and evidence are attacker-influenced. Store structured bounded values, sanitize terminal rendering, and do not place raw evidence into Hermes or subagent context automatically.

### Authority

All list/detail results are stanza- and disclosure-scoped. Zero or multiple target matches fail. Lower-trust contexts must not infer the existence of restricted findings through counts, completion, timing, or errors.

### Subagent role

None for authoritative finding creation. A subagent may recommend a candidate finding or classification, but deterministic rules/services validate evidence and create the authoritative record.

## 12. `/investigate`

### Purpose

Create or operate a bounded investigation workspace linked to one or more authorized findings, targets, hypotheses, and evidence references.

### Bare behavior

- If active investigations exist, list them and identify any current UI attachment.
- If none exist, show concise usage and eligible finding count.
- Never auto-select “the most severe” finding or allow the model to choose a target silently.

### Forms

```text
/investigate
/investigate <finding-id>
/investigate start <finding-id>
/investigate status <investigation-id>
/investigate evidence <investigation-id>
/investigate explain <finding-id|investigation-id>
/investigate trace process <process-ref>
/investigate compare <snapshot-id> <snapshot-id>
/investigate note <investigation-id> <bounded-text>
/investigate link <investigation-id> <finding-id>
/investigate hypothesis <investigation-id> <bounded-text>
/investigate cancel <investigation-id>
```

The first implementation should prefer one level of nesting. If deeper forms are retained, they must be registry-defined rather than model-parsed.

### Investigation schema

- investigation ID/revision/state;
- owner and authorized participants;
- linked findings and targets;
- scope and time bounds;
- explicit hypotheses;
- evidence references and provenance;
- analysis tasks and delegation chain;
- notes with actor/time;
- timeline selector;
- open questions;
- output/report references;
- expiry/closure state.

### Authority

Investigation is read-only over evidence by default. Active probing, new collection, cross-scope correlation, external lookups, or response requests are separate typed operations and authority decisions.

### Subagent role

Investigation is the highest-value and highest-risk subagent use case because evidence is attacker-controlled.

Permitted specialist roles:

- process ancestry analyst;
- network/destination analyst;
- secret-exposure classifier over sanitized metadata;
- dependency/vulnerability correlator;
- timeline hypothesis evaluator;
- report critic.

Controls:

- attenuated evidence references, not full parent transcript;
- read-only toolset by default;
- no slash dispatcher access;
- no parent credential or approval inheritance;
- no recursive delegation unless explicit;
- source data classification propagated;
- structured claims with evidence IDs and confidence;
- contradiction preserved rather than averaged away;
- deterministic validation before state changes;
- cancellation/expiry propagation.

### Failure behavior

A failed specialist does not fail the whole investigation unless its evidence class is required. The investigation reports incomplete tasks and does not convert missing analysis into exculpatory evidence.

## 13. `/timeline`

### Purpose

Render a bounded ordered view over environmental observations, finding transitions, investigation events, and Aegis control actions.

### Bare behavior

Show a recent timeline for the current investigation when the user has explicitly attached to one; otherwise show a bounded current-session timeline. The header states the anchor and time range.

### Forms

```text
/timeline
/timeline <finding-id|investigation-id|scan-id|watch-id>
/timeline --since 30m
/timeline --scope agent-session
/timeline --source <source-id>
/timeline --details
```

### Event fields

- event ID and schema version;
- event time and observed/ingest time;
- source clock and confidence/skew metadata;
- event category and canonical status vocabulary;
- actor/source;
- typed target;
- bounded description;
- provenance and related IDs;
- ordering caveat;
- data classification/disclosure state.

### Data model

Timeline is a deterministic query/view over event and control records. It does not create a second mutable chronology. OpenTelemetry, ECS, and OCSF adapters can normalize source events while preserving original source/provenance.

### Authority and privacy

A “timeline of everything” is not the default. Aggregation can reveal behavior across users, stanzas, workloads, and tenants. Every event is filtered by disclosure policy, and omitted intervals or sources are disclosed without leaking restricted identities.

### Subagent role

A subagent may propose correlations or narrative groupings, but cannot alter timestamps, source order, or authoritative event fields. Model-assisted groupings remain optional overlays with evidence links.

### Failure behavior

Clock disagreement, event loss, stale source, or partial ingestion appears as explicit gaps. A confident-looking but unsupported total order is prohibited.

## 14. `/report`

### Purpose

Generate a frozen, sanitized, attributable report from selected findings, investigations, timelines, and audit references.

### Bare behavior

Generate or preview a local draft for the currently attached investigation. If no investigation is attached, show eligible inputs rather than silently reporting on an arbitrary scope. Bare `/report` never sends or exports externally.

### Forms

```text
/report
/report preview <investigation-id>
/report finding <finding-id>
/report incident <incident-id>
/report generate <investigation-id> --format terminal
/report list
/report show <report-id>
/report export <report-id> --format json --destination <approved-destination>
```

### Report structure

- report ID/version/generation time;
- exact input selectors and source digests;
- scope and coverage limitations;
- executive summary;
- finding table with canonical states;
- observations and detections;
- mitigation requested/applied-unverified/confirmed distinctions;
- deterministic evidence appendix;
- timeline excerpt with source caveats;
- authority/audit references;
- unresolved questions;
- redaction/disclosure policy;
- generation/runtime/model/subagent provenance;
- integrity digest.

### Frozen artifact semantics

A report is a snapshot. Later finding changes do not rewrite it. A new generation produces a new revision/digest and links the predecessor.

### Model and subagent role

A drafting subagent may produce narrative from sanitized structured inputs. It receives no export capability. Every substantive narrative claim should cite finding/evidence IDs. A deterministic appendix and authoritative status fields remain model-untouchable.

A separate critic may check unsupported claims, contradictory evidence, missing caveats, and accidental sensitive content. Neither approves publication.

### Egress and approval

Local preview and external export are separate operations. Export requires:

- exact destination resolution;
- destination allowlist/policy;
- data-classification check;
- deterministic redaction;
- exact payload digest;
- authenticated approval where required;
- execution receipt;
- no model-held destination credential.

### Failure behavior

A failed export leaves the report local and records `export_failed`; it does not claim delivery. Partial report generation is labeled incomplete. Secret or disclosure scanner failure blocks export.

## 15. `/audit`

### Purpose

Inspect and verify authoritative Aegis control-plane events: who requested what, under which stanza/mandate/policy, what decision occurred, what execution/verification returned, and which receipt proves it.

### Bare behavior

Show recent metadata-only events for the current session/security context using a bounded time/count default.

### Forms

```text
/audit
/audit verify
/audit show <event-id>
/audit --since 1h
/audit --decision denied
/audit --operation scan
/audit <finding-id|investigation-id|report-id|operation-id>
```

### Audit versus timeline

- Timeline answers what was observed in the environment and how relevant state evolved.
- Audit answers what authenticated actors and Aegis requested, decided, executed, verified, or failed to do.

They may link each other but have distinct trust, retention, and disclosure semantics.

### Event requirements

- stable event ID/type/schema version;
- event time and sequence/integrity metadata;
- authenticated actor;
- agent/stanza/mandate/policy/runtime references;
- canonical operation;
- target/scope digest;
- allow/deny/approval decision and reason code;
- operation and receipt references;
- outcome/verification state;
- delegation chain where applicable;
- no secret values, full prompts, bearer capabilities, or unsafe external errors.

### Integrity

`/audit verify` verifies the implemented append/integrity mechanism and reports exact covered range, gaps, and last verified state. It must not claim cryptographic guarantees stronger than implemented custody permits.

### Injection and privacy

Audit metadata can contain attacker-influenced identifiers. It is sanitized before terminal rendering. Viewing audit is disclosure-controlled and may itself be audited. Repeated read events can be sampled/coalesced under policy.

### Subagent role

None for authoritative event creation or verification. A subagent may summarize a bounded verified range, with each claim linked to event IDs.

### Failure behavior

Audit verification failure is high visibility. Consequential operations follow declared fail-closed policy; the UI must not quietly continue while claiming authoritative receipts.

## 16. `/cancel`

### Purpose

Request cooperative cancellation of one active operation or leased resource without implying rollback.

### Bare behavior

- If exactly one foreground cancellable operation exists, preview/name it and request cancellation according to the operation's interaction policy.
- If zero exist, report none and list relevant active background operations.
- If multiple plausible foreground operations exist, list them and require an ID.

No implicit “most recent operation” rule.

### Forms

```text
/cancel
/cancel <operation-id|scan-id|watch-id|investigation-id|report-id>
/cancel --type scan
/cancel --all --type scan
```

Bulk cancellation should be deferred or require exact preview and confirmation. A generic `--force` should not ship until hard-termination and cleanup semantics are proven for each operation class.

### Semantics

Cancellation states:

```text
cancellation_requested
cancellation_confirmed
already_terminal
cancellation_failed
cancellation_unknown
```

Canceling a completed operation is idempotent and returns `already_terminal`. Canceling a scan does not delete findings already created. Canceling a watch stops future collection but does not erase retained events. Canceling report export does not recall a payload already delivered.

Cancellation is not rollback, containment, revocation, or evidence deletion.

### Authority

An owner should generally be able to stop work it was authorized to start, but cross-owner/persistent-service cancellation requires explicit policy. Session revocation may cancel independently of caller permission.

### Subagent role

None in decision. Cancellation propagates to child contexts/jobs and records which children acknowledged, timed out, or required force termination.

### Implementation note

Use propagated `context` cancellation plus operation-specific cleanup and bounded deadlines. The Go context signal alone does not prove an external process, sensor, or export stopped; postconditions remain necessary.

## 17. `/clear`

### Purpose

Clear the local terminal presentation while preserving all security and session state.

### Bare behavior

Clear/redraw the display and print a concise confirmation:

```text
Display cleared.
Session authority, Hermes state, watches, findings, investigations, and audit are unchanged.
```

### Forms

```text
/clear
```

No destructive variants in the core command.

### Security invariants

`/clear` does not:

- erase terminal emulator scrollback it cannot control;
- clear Hermes/model context;
- clear Aegis transcript retention;
- stop operations or watches;
- delete evidence/findings/reports/audit;
- revoke authority;
- hide prior activity from authoritative records.

The UI should avoid claiming stronger clearing than the terminal API can establish.

### Audit

A clear action may be recorded as bounded metadata, especially around active incidents, but it must not create excessive audit noise.

### Subagent role

None.

### Accessibility

Plain/screen-reader mode should emit a line-oriented boundary rather than cursor-control sequences. `TERM=dumb` and terminal capability detection govern whether physical clearing is attempted.

## 18. `/exit`

### Purpose

End the current Aegis manager session through one bounded cleanup path.

### Bare behavior

Begin cleanup immediately unless a focused approval/protected-intake state requires a safe cancel transition first. Display real cleanup stages and final outcome.

### Forms

```text
/exit
/quit   # explicit alias
```

An optional future `--detach` is inappropriate until persistent operation ownership is fully implemented.

### Cleanup behavior

At minimum:

1. mark session closing and reject new input;
2. cancel foreground model/operation work;
3. cancel approvals and protected intake and restore terminal mode;
4. stop session-owned watches;
5. stop/close Hermes and gateway resources;
6. close inference/proxy resources;
7. remove disposable state;
8. invalidate/release capabilities;
9. finalize one metadata-only receipt;
10. restore terminal exactly once;
11. report final outcome and any persistent separately owned resources.

### Persistent operations

Nothing silently persists. If a watch/job is owned by an authenticated service rather than the session, exit lists its ID, owner, scope, lease, and stop command. Session-owned work is cancelled/stopped.

### Authority and audit

Exit cannot be denied merely to trap the operator in a session. Cleanup may report failures and return nonzero status. Audit records end reason, operation disposition, capability invalidation, and cleanup result.

### Subagent role

None in lifecycle authority. Cancellation propagates to all delegated tasks, and cleanup reports stragglers without exposing their raw output.

## 19. Cross-command state machines

### 19.1 Operations

```text
accepted -> queued -> running -> completed
                     |    |       |
                     |    +------> failed
                     +-----------> cancellation_requested
                                      |
                                      +-> cancelled
                                      +-> cancellation_failed/unknown
```

### 19.2 Findings

```text
open -> acknowledged -> investigating -> resolved -> closed
  |          |              |
  +----------+--------------+-> suppressed(until)
```

Detection status remains preserved regardless of workflow state.

### 19.3 Investigations

```text
open -> active -> awaiting_input -> completed -> closed
          |              |
          +-> partial/failed/cancelled/expired
```

### 19.4 Reports

```text
generating -> draft -> frozen
                |       |
                |       +-> export_requested -> exported
                |                              -> export_failed
                +-> generation_failed/cancelled
```

### 19.5 Watches

The watch lifecycle is defined in `/watch`; event-source loss moves to degraded rather than silently remaining active/healthy.

## 20. Shared privacy and disclosure rules

All commands must account for:

- terminal scrollback;
- command history and reverse search;
- autocomplete caches;
- model and subagent prompts;
- logs and audit;
- crash/panic output;
- report artifacts;
- external integrations;
- clipboard and recording workflows;
- retained operation/event/finding stores.

Protected values are never ordinary command arguments. Paths, process names, usernames, destinations, finding titles, notes, indicators, and report metadata may also be sensitive.

Sanitization must neutralize terminal controls, carriage-return rewriting, unsafe bidi controls, malformed text, giant tokens, and forged Aegis chrome. Sanitization is not declassification; disclosure policy is separate.

## 21. Source adapter strategy

Aegis should define its internal types first, then implement adapters:

- SARIF import/export for applicable static-analysis findings;
- OCSF mapping for applicable security finding/event classes;
- OpenTelemetry/ECS mapping for event ingestion/export;
- Gitleaks/Trivy/OSV-style scanner adapters for selected profiles;
- Falco or other event-source adapters for watch inputs;
- existing EDR/SIEM integrations under authenticated deployment policy.

Every adapter reports:

- source product/version;
- rule/schema/database version;
- collection mode;
- effective coverage;
- loss/partial behavior;
- normalization decisions;
- unsupported fields;
- confidence and authority class.

External tool output does not become authoritative merely because the tool is popular or produces JSON.

## 22. Recommended implementation architecture

```text
internal/tui or terminal owner
  -> command position detector
  -> typed slash parser
  -> command registry
  -> application operation dispatcher
       -> identity/policy/mandate service
       -> scan orchestrator and adapters
       -> watch manager and source adapters
       -> finding store/service
       -> investigation service
       -> timeline query service
       -> report generator/export service
       -> audit service
       -> operation lifecycle/cancellation service
  -> typed presentation events
```

The TUI does not make authority decisions. Command handlers do not contain detector, workflow, or provisioning policy. CLI/API/TUI call shared services.

The registry is the source for:

- parsing;
- help;
- completion;
- aliases;
- availability;
- argument schemas;
- execution class;
- authorization operation name;
- audit operation name;
- result schema;
- tests.

## 23. Test requirements

### Command routing

- exact top-level and nested matching;
- unknown slash never reaches Hermes;
- model/subagent/sensor output cannot invoke commands;
- `//` literal escape enters ordinary guarded conversation;
- leading whitespace behavior;
- no shell expansion/chaining;
- aliases canonicalize before policy/audit.

### Registry consistency

- help/completion/dispatch/policy names generated from one registry;
- exactly 15 canonical top-level commands;
- every advertised form has a real service and state-aware availability;
- aliases cannot shadow canonical commands;
- no unavailable response command is advertised.

### Authority

- unauthenticated, expired, revoked, missing, and ambiguous contexts fail closed;
- no in-session stanza switch;
- scope widening fails;
- delegation requires separate permission;
- subagent gets attenuated manifest and no inherited approval/credential;
- approval bound to exact payload/target/digest.

### Scan

- bare profile desugaring and idempotency key include scope/revision;
- partial sources never produce complete result;
- no-findings never says safe;
- secret candidates absent from all retention/output surfaces;
- discovered assets do not expand scope;
- cancellation retains already-created finding history.

### Watch

- bare ensure-and-attach semantics;
- duplicate prevention scoped correctly;
- lease expiry/revocation/cleanup;
- dropped-event and disconnect visibility;
- no automatic response or sensor installation;
- persistent ownership claims backed by a real service;
- session exit stops session-owned watches.

### Findings/investigations/timeline/report

- disclosure-scoped list/count/completion/error behavior;
- attacker-controlled text cannot forge UI;
- finding state history is append-preserving;
- no silent investigation target selection;
- timestamps and gaps preserved;
- report is frozen and digestible;
- model narrative cannot modify authoritative appendix;
- export requires exact destination and payload policy;
- failed export never reports delivery.

### Audit

- metadata-only records;
- event links and delegation chain;
- integrity verification range/gap behavior;
- unsafe external values excluded/sanitized;
- audit failure influences consequential operation policy as specified.

### Terminal/accessibility

- widths 40/50/80/120/200;
- no-color and screen-reader/plain modes;
- no color-only severity/state;
- stable IDs readable in plain output;
- Ctrl-C/Esc/EOF/terminal loss semantics;
- one terminal restoration path;
- command history privacy;
- bounded output and pagination.

## 24. Delivery sequence

### Phase 0 — command substrate

- command-position detector and literal escape;
- typed registry/parser;
- typed results/presentation events;
- operation IDs and lifecycle;
- state-aware help/completion;
- terminal sanitization;
- authority and audit integration.

Initial core forms:

```text
/help /status /context /authority /limits /audit /cancel /clear /exit
```

### Phase 1 — Aegis-native scan

- `/scan core`, runtime, permissions, configuration, sensors;
- operation/finding schemas;
- coverage/limits semantics;
- no host-security overclaim.

### Phase 2 — bounded scanner adapters

- secrets, dependencies, selected files, session process/network snapshots;
- pinned adapters and databases;
- SARIF/OCSF mappings where useful.

### Phase 3 — findings and investigation

- durable finding store/workflow;
- investigation workspace;
- bounded subagent delegation;
- timeline query.

### Phase 4 — watch

- leased source manager;
- event normalization;
- rule/correlation lifecycle;
- loss/backpressure/gap handling;
- explicit session cleanup.

### Phase 5 — report

- frozen deterministic report artifact;
- model-assisted narrative behind evidence references;
- local preview;
- separately authorized export.

### Phase 6 — contextual response

Only after real enforcement adapters, policy, approval, idempotency, rollback, postcondition verification, and receipts exist. Do not add a generic `/fix`, `/kill`, or `/respond` shortcut first.

## 25. Resolved decisions and remaining decisions

### Resolved in this report

- exactly 15 canonical top-level commands;
- `/scan` is useful in bare form;
- `/watch` is useful and idempotent in bare form;
- `/clear` is display-only;
- `/exit` stops session-owned watches;
- `/investigate` never silently chooses a target;
- `/report` bare form never exports;
- unknown slash input never reaches Hermes;
- subagents never inherit authority implicitly;
- response remains contextual and typed rather than a generic core command.

### Remaining before normative specification

1. Exact version 1 `core` scan modules, bounds, and default scope.
2. Exact default watch sources, rules, lease, buffer, and retention.
3. Stable public ID format and abbreviation policy.
4. Final parser quoting and `//` escape details.
5. Finding/investigation/report persistence technology and retention policy.
6. First Linux sensor/adapter set and privilege separation.
7. Which scanner databases may update online and through which approved supply-chain path.
8. Exact subagent providers/models/regions allowed for each operation and data class.
9. First export destinations and approval policy.
10. Mapping boundaries for SARIF, OCSF, OpenTelemetry, and ECS.
11. Whether watch pause/resume/renew ship initially.
12. First narrow response action that is sufficiently reversible and verifiable.

## 26. Launch-asset impact review

This report changes no implementation, command syntax, dependency, runtime behavior, endpoint collection, or security enforcement claim. It is research only.

Reviewed as unaffected for current behavior:

- root `README.md`;
- `LICENSE`;
- `SECURITY.md`;
- `CONTRIBUTING.md`;
- `CODE_OF_CONDUCT.md`;
- `CHANGELOG.md`;
- `docs/THREAT_MODEL.md`;
- `docs/ARCHITECTURE.md` and architecture diagram;
- `docs/QUICKSTART.md`;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md` and recording assets;
- release binaries and checksums;
- repository-local contributor issue material.

Implementation will affect all command/help examples, quickstart, demo, recording, architecture, threat model, SECURITY, CHANGELOG, PTY evidence, release artifacts/checksums for an authorized release, and contributor issue material. External releases/issues require explicit owner authorization.

## Bottom line

The Core 15 is coherent and sufficient.

Its product spine is:

```text
/scan and /watch observe
/findings preserves detections
/investigate organizes bounded analysis
/timeline orders evidence and control events
/report freezes a reviewable account
/audit proves authenticated Aegis decisions and outcomes
```

The orientation commands make trust and limitations legible; lifecycle commands keep work cancellable and cleanup explicit. Subagents can provide specialization and parallelism only inside attenuated typed operations. They never become an authority source, receive inherited approval, or gain access to the slash dispatcher.

That combination gives Aegis a security-native terminal vocabulary without turning slash syntax or model behavior into a security boundary.
