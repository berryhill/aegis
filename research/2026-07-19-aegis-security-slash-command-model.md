# Aegis security slash-command model

**Research date:** 2026-07-19 UTC  
**Status:** Product and architecture research; not an implementation specification  
**Authorization boundary:** This report does not authorize endpoint collection, privileged monitoring, Hermes profile changes, provisioning, activation, or response actions

## Executive decision

A slash command in Aegis should be a deterministic local control gesture, not a specially worded prompt.

The defining behavior is:

> If input is recognized as an Aegis slash command, Aegis consumes it before ingress to Hermes, parses it into a closed typed operation, checks current availability and authority, invokes an Aegis-owned service, and renders an origin-labeled result. Command text does not reach the model.

That gives Aegis a useful two-lane terminal:

1. **Conversation lane:** natural language is bounded and guarded, then may reach Hermes as untrusted model input.
2. **Control lane:** slash commands are parsed and executed locally through typed Aegis services and never gain authority from model interpretation.

The slash itself is only syntax. It is not authentication, authorization, a capability, an approval, or an enforcement boundary. Those remain Aegis responsibilities outside the model.

For security inspection, the recommended public shape is one composable command family with a few convenience aliases:

```text
/scan [profile]
/scan secrets [scope]
/scan processes [scope]
/scan network [scope]
/scan files [scope]
/scan persistence [scope]
/scan permissions [scope]
/scan runtime [scope]
/watch start [profile] [scope]
/watch status [watch-id]
/watch events [watch-id]
/watch stop <watch-id>
/findings [filter]
/findings <finding-id>
/investigate <finding-id|target>
```

`/scan-secrets`, `/scan-processes`, and similar forms may exist as discoverable aliases, but they should map to the same canonical typed operations. They should not become separately implemented code paths.

The primary product recommendation is to ship the semantic substrate before shipping a broad command list. A command such as `/watch threats` is dishonest unless Aegis can identify its sensors, coverage, event loss, policy generation, scope, start time, and degraded state. Likewise, “scan clean” must never imply that an endpoint is safe.

## Research basis

### Repository sources inspected

- `AGENTS.md`
- `docs/product/BIG_IDEA.md`
- `specs/MVP.md`
- `specs/BASE_MANAGER_END_TO_END.md`
- `specs/TERMINAL_EXPERIENCE_OPERATIONAL_IMPLEMENTATION.md`
- `research/2026-07-19-terminal-experience-best-of-hermes-openclaw-claude-code.md`
- `research/2026-07-17-host-endpoint-monitoring-enforcement-response.md`
- `internal/command/manager.go`
- `internal/command/manager_startup.go`
- existing manager command tests under `internal/command/`

The current Aegis manager already establishes an important invariant: local deterministic slash commands are consumed before Hermes. It currently supports local commands including `/help`, `/status`, `/audit verify`, `/clear`, `/complete`, `/quit`, `/exit`, and state-dependent credential metadata commands. Proposed scan and watch commands are not implemented.

### External sources consulted

- Claude Code interactive mode: https://code.claude.com/docs/en/interactive-mode
- OpenAI Codex CLI slash commands: https://developers.openai.com/codex/cli/slash-commands/
- Hermes Agent CLI guide: https://hermes-agent.nousresearch.com/docs/user-guide/cli/
- Gemini CLI commands: https://google-gemini.github.io/gemini-cli/docs/cli/commands.html
- NIST SP 800-61 Rev. 3, Incident Response Recommendations and Considerations for Cybersecurity Risk Management: https://csrc.nist.gov/pubs/sp/800/61/r3/final
- MITRE ATT&CK: https://attack.mitre.org/

The interactive-agent products consistently use slash commands for discoverable, low-friction session control and inspection. Their useful common patterns are command autocomplete, help, status/context inspection, explicit local handling, and progressive disclosure. Aegis must adapt those patterns to a stricter authority model: local command origin, typed operations, state-aware availability, exact approval, and truthful security coverage.

NIST incident-response guidance and MITRE ATT&CK inform the broader lifecycle and threat vocabulary, but neither should be treated as a ready-made Aegis command taxonomy. Aegis commands should express its own typed observations, findings, investigations, and response requests.

## 1. What a slash command innately is

A slash command has five distinct properties.

### 1.1 It is a lexical mode switch

At a documented command position, a leading `/` switches the composer from conversational parsing to deterministic local-command parsing.

This switch must occur before:

- the ordinary prompt ingress guard;
- Hermes gateway submission;
- model inference;
- model-generated proposal decoding.

The command parser, not the model, decides whether input is a command.

### 1.2 It is a local control-plane request

A recognized slash command names an Aegis operation. It does not ask Hermes to imitate that operation.

For example:

```text
/scan processes --scope host
```

should become a typed request resembling:

```text
operation: scan.processes
scope: host
mode: snapshot
```

It should not become the prompt “please scan the processes.”

### 1.3 It is not authority

Typing `/contain process ...` cannot authorize containment. At most, it creates a response request. Aegis must independently establish:

- authenticated actor;
- active, unexpired, unrevoked mandate;
- exactly one trust stanza;
- operation availability;
- target resolution;
- policy decision;
- required approval freshness;
- exact execution scope.

The command text is user intent, not proof of permission.

### 1.4 It is not necessarily synchronous

Commands fall into execution classes:

- **Immediate local:** help, status, context, command discovery.
- **Bounded query:** list findings, inspect one finding, run a small snapshot.
- **Long-running job:** full scan, dependency inventory, evidence correlation.
- **Subscription:** watch event streams or detection state.
- **Proposal:** prepare a consequential response for approval.
- **Lifecycle:** exit, cancel, stop a watch.

The UI must represent these classes differently. A long scan should return a stable operation ID, progress, cancellation behavior, and final status rather than blocking silently.

### 1.5 It has authoritative origin only for what Aegis actually knows

Aegis may authoritatively report that it accepted a command, invoked a specific service, observed a specific result, or failed to collect required telemetry. It may not transform model narration into an authoritative scan result.

An output can contain model-assisted explanation, but that section must remain separately labeled as untrusted analysis.

## 2. Recommended parsing contract

### 2.1 Command position

A slash command should be recognized when the first non-whitespace character of a submitted logical input is `/`.

This removes a surprising distinction between `/status` and `  /status`. The raw submitted input must still be bounded before trimming or parsing.

Only the first logical line may open command mode. Multiline command arguments should be rejected initially unless a specific command schema explicitly permits them.

### 2.2 Literal slash escape

A user needs a deterministic way to send natural language beginning with `/`. Recommended behavior:

```text
//scan is an example in this discussion
```

becomes conversational content beginning:

```text
/scan is an example in this discussion
```

The escaped content then follows the ordinary ingress guard and may reach Hermes. The UI should preview this behavior in help and autocomplete.

Unknown slash input must not fall through to Hermes. A typo such as `/scna` should produce a local error and suggestions, not become a model prompt.

### 2.3 Matching rules

Recommended matching behavior:

- command names are lowercase ASCII;
- exact command/subcommand matching;
- no implicit prefix execution;
- no fuzzy execution;
- fuzzy or prefix matches are suggestions only;
- aliases are explicit registry entries;
- deprecated aliases warn but resolve deterministically;
- no user-defined alias may shadow a built-in security command;
- unknown commands fail locally.

Executing the only autocomplete candidate after an abbreviation is convenient but unsafe for security operations. `/cont` should suggest `/context` and `/contain`; it should execute neither.

### 2.4 Argument grammar

Aegis should use a small purpose-built argument grammar, not shell evaluation.

Useful supported forms:

```text
/scan processes
/scan processes --scope host
/findings --severity high --since 1h
/findings finding_01J...
/scan files --path "/srv/application data"
```

The parser may support:

- whitespace-separated tokens;
- single and double quoted literal values;
- backslash escaping only where precisely documented;
- `--name value` and, if desired, `--name=value`;
- a literal `--` separator only when a command schema needs it.

It must not support:

- shell command substitution;
- environment expansion;
- glob expansion by the parser;
- pipes;
- redirects;
- background `&` semantics;
- semicolon command chaining;
- arbitrary `!` shell mode;
- executable interpolation;
- hidden model interpretation of extra arguments.

Every operation must reject unknown flags, duplicate singleton flags, malformed durations, unsupported scopes, trailing arguments, and oversized values.

### 2.5 Canonicalization

Parsing should produce a canonical request containing:

- schema version;
- canonical operation name;
- authenticated subject reference supplied by Aegis, not input;
- immutable session and stanza references supplied by Aegis;
- normalized bounded arguments;
- requested scope;
- request ID;
- creation time;
- optional idempotency key where appropriate.

Aliases must canonicalize before policy evaluation and audit. Audit should record `scan.secrets`, not create different semantics for `/scan-secrets` and `/scan secrets`.

## 3. Command registry as the source of truth

Aegis should define each slash command in a typed registry. The registry should drive parser dispatch, autocomplete, help, state-aware availability, policy lookup, tests, and audit names.

Each command definition should contain at least:

- canonical name and explicit aliases;
- one-line description;
- argument schema;
- examples;
- execution class;
- required service/capability;
- valid session lifecycle states;
- allowed scopes;
- data sensitivity;
- whether model assistance is permitted;
- whether the operation mutates state;
- approval class;
- cancellation semantics;
- audit policy;
- degraded-mode behavior;
- output/result schema version.

This prevents help, completion, authorization, and implementation from drifting into separate command lists. The current manager has separate switch and completion declarations; the operational implementation should converge on one registry rather than expand that duplication.

Availability is an evaluated property, not a static boolean. A command may be:

- available;
- unavailable because a service is absent;
- unavailable because telemetry is degraded;
- denied by the current trust stanza;
- unavailable during startup or cleanup;
- approval-gated;
- unsupported on the current platform;
- available only for a narrower scope.

Help should distinguish these conditions without revealing capabilities or targets outside the caller's disclosure policy.

## 4. Command taxonomy

### 4.1 Session and authority inspection

These commands expose Aegis-owned session facts:

```text
/help [topic]
/status
/context
/authority
/limits
/exit
```

Recommended distinctions:

- `/status`: operational health and concise active-state summary.
- `/context`: identity, logical agent, stanza, mandate, runtime, charter/policy provenance.
- `/authority`: effective allowed/denied operation classes and approval boundaries.
- `/limits`: blind spots, unavailable sensors, unsupported scopes, and non-sandbox limitations.

Command discovery belongs under `/help`. Connected capabilities are summarized by `/status`, `/authority`, and `/limits` rather than receiving additional top-level commands.

Important security context should remain persistently visible and not depend solely on these commands.

### 4.2 Snapshot scanning

Use `/scan` as a dispatcher over explicit scan profiles:

```text
/scan                         # documented safe default profile and scope
/scan quick                   # bounded high-signal profile
/scan full                    # broader profile; explicit cost and coverage
/scan secrets                 # credential-pattern exposure scan
/scan processes               # process identity, ancestry, executable provenance
/scan network                 # listeners, connections, destinations, policy mismatch
/scan files                   # selected paths, integrity, ownership, permissions, drift
/scan persistence             # services, timers, cron, startup and account mechanisms
/scan permissions             # effective Aegis/runtime authority and dangerous reach
/scan runtime                 # Hermes/runtime artifacts and effective configuration
/scan dependencies            # manifests, lockfiles, advisories, provenance where available
/scan configuration           # Aegis and integrated control configuration
/scan sensors                 # collection and enforcement health
```

Convenience aliases can include:

```text
/scan-secrets
/scan-processes
/scan-network
/scan-files
```

Aliases should remain thin mappings. The canonical family is easier to discover, document, filter, and extend.

### 4.3 Continuous monitoring

`/watch` should manage a real subscription or bounded monitoring job:

```text
/watch start threats --scope host
/watch start processes --scope agent-session
/watch start network --scope agent-session
/watch status
/watch status <watch-id>
/watch events <watch-id> --since 10m
/watch stop <watch-id>
```

Bare `/watch` should idempotently ensure one clearly documented, low-cost default observation watch and return its status. It must not silently enable broad host collection.

A watch result must identify:

- watch ID;
- profile and rule-set revision;
- scope;
- sensor set and health;
- start time and bounded retention;
- dropped-event counters where available;
- current state;
- stop/expiry condition;
- whether it is observation-only or connected to enforcement.

“Watching threats” is not a meaningful coverage claim without these details. Watching processes and network state periodically is also not equivalent to kernel-event collection, and collection is not prevention.

### 4.4 Findings and investigation

```text
/findings [filters]
/findings <finding-id>
/investigate <finding-id|typed-target>
/investigate evidence <finding-id>
/timeline [scope]
/investigate trace process <process-ref>
/investigate compare <snapshot-id> <snapshot-id>
/investigate explain <finding-id>
/report [scope]
```

`/findings <id>` should render deterministic finding data. `/investigate explain` may include model-assisted prose only when clearly origin-labeled and based on bounded referenced evidence.

A finding should include:

- stable ID;
- detector/rule ID and version;
- severity and confidence as separate fields;
- state and timestamps;
- typed target reference;
- scope;
- required sensors and their health;
- bounded evidence references;
- authority/session correlation where applicable;
- recommended next step;
- known uncertainty and coverage limitations.

### 4.5 Workflow operations

```text
/findings <finding-id> ack
/findings <finding-id> note <text>
/findings <finding-id> classify <classification>
/findings <finding-id> assign <authorized-subject>
/findings <finding-id> suppress --until <time> --reason <text>
```

These mutate security workflow state and therefore require typed authorization and authoritative audit. Suppression must not delete historical evidence or hide sensor failure.

### 4.6 Response requests

Potential future commands include:

```text
/contain process <process-ref>
/contain workload <workload-ref>
/revoke session <session-id>
/block destination <destination-ref>
/quarantine artifact <artifact-ref>
/stop service <service-ref>
/rotate credential <credential-ref>
/recover <incident-id> --playbook <playbook-id>
```

These are response requests, not direct model actions. They require deterministic target resolution, exact preview, current-state revalidation, policy evaluation, approval where required, bounded execution, postcondition verification, and a receipt.

Aegis should not ship generic `/kill <pid>`, `/rm`, `/shell`, or `/fix` operations. PID alone is reusable and insufficient process identity; deletion has poor recovery semantics; shell text is not a typed response; and “fix” has no bounded postcondition.

## 5. Scan semantics

### 5.1 Every scan requires explicit scope

A scan scope should be one typed value such as:

- current Aegis session;
- current logical agent/session process tree;
- repository/workspace;
- selected filesystem root;
- enrolled workload;
- host;
- deployment group or fleet.

The default scope must be visible in autocomplete, preview, progress, result, and audit. A host scan must never be inferred merely because the current OS user can read host data.

Scope authorization and data disclosure are separate checks. A caller might be authorized to request a host scan but only receive a minimized finding summary.

### 5.2 Every scan requires declared coverage

A scan result should report:

- requested scope;
- effective scope;
- included detectors;
- excluded detectors;
- sensor/source versions;
- source health;
- policy/rule revisions;
- start and end times;
- completeness/degraded status;
- limits, timeouts, permission denials, and event loss;
- finding count by severity;
- stable scan ID.

“No findings” means only that enabled detectors found nothing in the effective covered scope. It must not be rendered as “safe,” “secure,” “clean machine,” or “no threats.”

Recommended terminal outcomes are:

- `completed_with_findings`;
- `completed_no_findings`;
- `partial`;
- `degraded`;
- `failed`;
- `cancelled`;
- `expired`.

### 5.3 Secret scanning

`/scan secrets` should detect exposures without printing candidate secret values.

Its evidence should favor:

- detector identifier;
- file or source reference subject to disclosure policy;
- bounded location metadata;
- secret class guess;
- confidence;
- fingerprint safe for deduplication only if designed not to enable offline guessing;
- remediation guidance;
- whether the candidate was retained, redacted, or discarded.

Secret bytes must not enter chat transcript, model context, logs, audit, command history, completion state, terminal snapshots, or finding previews. Scanning arbitrary host memory or private files is a separate high-sensitivity forensic capability and should not be implied by the base command.

### 5.4 Process scanning

`/scan processes` should reason over stable process references rather than PID alone. At minimum, a process reference should include deployment, PID, process start identity, executable identity, OS identity, and observed ancestry.

Potential checks include:

- unexpected executable or digest;
- unexpected OS identity;
- suspicious ancestry;
- policy-disallowed children;
- deleted/replaced executable backing;
- unexpected namespace/cgroup/service relationship;
- privileged execution;
- runtime process drift from the recorded Aegis session;
- sensor or inspection gaps.

A later response must re-resolve and revalidate the process immediately before acting.

### 5.5 Network scanning

`/scan network` should distinguish:

- snapshot of current listeners/connections;
- event history from a sensor;
- DNS or destination telemetry;
- policy mismatch;
- confirmed gateway denial;
- OS/external enforcement confirmation.

A socket-table snapshot cannot prove that no transient connection occurred. A detected connection does not mean it was blocked.

### 5.6 File and persistence scanning

File scanning needs explicit selected paths and policy. It should avoid unbounded content collection by default. Useful evidence includes identity, digest, package provenance, ownership, mode, timestamps, approved baseline relation, and observing process where available.

Persistence scanning should identify the operating-system mechanisms actually covered. On Linux this can include selected systemd units/timers, cron, startup files, SSH authorization, accounts/groups, package state, kernel modules, and other declared mechanisms. The result must list exclusions and permission failures.

### 5.7 Runtime and authority scanning

Aegis has a differentiated scan unavailable to generic endpoint products:

```text
/scan runtime
/scan permissions
```

These can compare:

- authenticated subject;
- charter revision and digest;
- selected stanza;
- mandate and expiry;
- expected Hermes runtime/version/configuration;
- actual runtime process identity;
- effective tools;
- credential and memory scopes;
- broker destinations;
- plugin/MCP state;
- unmediated host access limitations;
- current machine-policy generation where endpoint support exists.

This is primarily deterministic drift and exposure inspection. It should not claim host sandboxing.

## 6. Watch semantics

### 6.1 Watch is a managed resource

A watch is not just a screen that refreshes. It should have a lifecycle:

```text
created -> starting -> active -> degraded -> stopping -> stopped
                            |                    |
                            +-> failed/cancelled/expired
```

A watch should be bound to:

- authenticated requester;
- stanza/mandate or separately issued monitoring authority;
- exact scope;
- sensor and rule-set revisions;
- retention policy;
- lease/expiry;
- operation ID;
- audit provenance.

Session exit should either stop the watch or explicitly transfer it to a separately authorized persistent service. The TUI must never imply a watch continues after its owning process exits unless that persistence is real and visible.

### 6.2 Watch has backpressure and loss semantics

Continuous telemetry can exceed UI and storage capacity. The system must define:

- event coalescing;
- bounded buffers;
- retained metadata;
- dropped-event counters;
- reconnect behavior;
- cursor/checkpoint behavior;
- event ordering guarantees;
- clock and timestamp sources;
- behavior when the evidence sink is unavailable.

The terminal timeline is not the evidence store. It is a bounded view over typed events.

### 6.3 Watch cannot silently escalate into response

A watch profile must declare one mode:

- observation only;
- detection and alert;
- deterministic deny at an already approved enforcement point;
- response proposal;
- policy-authorized automatic narrow response.

Starting an observation watch must not silently install firewall rules, create services, enable kernel sensors, persist configuration, or activate automatic response. Those are separate provisioning or policy actions requiring explicit scope and authorization.

## 7. Authority and approval behavior

### 7.1 Dispatch-time checks

Before invoking a command service, Aegis should check:

- session active and not cleaning up;
- authenticated identity present and fresh enough;
- exactly one active stanza;
- mandate unexpired and unrevoked;
- command available in current platform/readiness state;
- requested scope authorized;
- required service healthy enough to attempt the operation;
- arguments valid and bounded.

### 7.2 Execution-time checks

Consequential operations must recheck immediately before mutation:

- identity and approval freshness;
- mandate and policy generation;
- target uniqueness and current identity;
- expected preconditions;
- blast-radius limit;
- idempotency/replay status;
- operation expiry;
- enforcement service identity and health.

### 7.3 Approval is separate from command submission

For approval-gated operations, the slash command should lead to an Aegis-owned preview containing exact typed fields. The model may explain the proposal in a separate untrusted area, but it cannot populate authoritative fields or approve.

Cancel, EOF, terminal loss, resize that obscures exact scope, session expiry, target drift, or service failure must fail closed.

### 7.4 No in-session stanza switch

No slash command should mutate the session's trust stanza or union authority. A request to operate under another stanza requires a new authenticated mandate and a clean runtime session.

## 8. Output and origin model

Every command result should be a typed event/result, not arbitrary interleaved text.

Recommended origin classes:

- `aegis.authoritative` — accepted request, policy decision, service result, receipt;
- `aegis.diagnostic` — bounded operational diagnostics;
- `sensor.observation` — attributed external or host observation;
- `integration.result` — attributed EDR/SIEM/tool response;
- `hermes.analysis` — untrusted model explanation;
- `user.input` — command/request or conversational message.

A result should expose:

- request/operation ID;
- operation kind and schema version;
- state;
- scope;
- authoritative source;
- timing;
- coverage/health;
- bounded summary;
- finding/evidence references;
- audit/receipt reference when applicable;
- next allowed actions.

Untrusted content must pass terminal sanitization before rendering. It must not forge the Aegis command-result chrome, trust bar, approvals, findings, or receipts.

## 9. Interaction behavior

### 9.1 Discovery

Typing `/` should open state-aware completion. Completion should show:

- command name;
- short description;
- immediate availability;
- execution class or consequence marker;
- aliases only when useful;
- reason when a normally relevant command is unavailable and disclosure is allowed.

`/help` and `?` should explain the distinction between local deterministic commands and conversational requests.

### 9.2 Preview

Read-only metadata commands need no confirmation. Expensive or broad scans should show a concise preflight when cost, privacy, or scope is material. Mutations always follow the appropriate exact approval contract.

A broad scan preflight can show:

```text
Operation: scan.full
Scope: host endpoint endpoint_...
Mode: observation only
Sensors: process, socket, selected-file, service
Excluded: memory, packet payload, arbitrary file content
Expected limit: 5m / 250 MiB metadata
Persistence: finding metadata under configured retention policy
```

### 9.3 Progress and cancellation

Long operations should show real stages, elapsed time, and a stable operation ID. `/cancel <operation-id>` should request cancellation of a cancellable operation and report whether cancellation was confirmed.

Cancellation must not be reported as rollback. A scan may have already collected or persisted bounded evidence before cancellation. A response may need a separately defined rollback.

### 9.4 Concurrency

The MVP should permit at most one foreground composer-owned operation while allowing explicitly managed background watches. Concurrency limits should be enforced by service and scope, not only by UI.

Duplicate command submission should not create duplicate response actions. Consequential requests require idempotency keys or equivalent replay defense.

### 9.5 History and privacy

Command history should retain command structure only under an explicit policy. Sensitive arguments, paths, indicators, incident notes, and target identifiers may themselves be confidential. Protected values must never be accepted as ordinary command arguments.

History search, autocomplete, crash output, debug logs, and terminal recordings require the same disclosure review as transcript rendering.

## 10. Error behavior

Errors should be local, typed, stable, and actionable.

Required classes include:

- unknown command;
- ambiguous suggestion, with no execution;
- invalid argument;
- unavailable capability;
- unsupported platform/scope;
- unauthorized scope;
- expired/revoked session;
- ambiguous target;
- sensor degraded or unavailable;
- partial collection;
- timeout;
- cancellation requested/confirmed/unconfirmed;
- approval required/declined/expired;
- execution failed;
- postcondition unverified;
- audit/receipt failure.

Errors must not include secret bytes, raw environment values, bearer capabilities, arbitrary external stderr, full prompts, or unsafe terminal controls.

Unknown slash commands must never be sent to Hermes as a fallback. This is both a predictability and security requirement.

## 11. Current implementation assessment

The current manager has the right foundational ordering but should not be generalized by simply adding more `switch` cases.

Observed current behavior:

- local directives are consumed before the ordinary ingress guard and Hermes;
- unknown slash directives fail locally;
- help is partly state-aware for credential-authority readiness;
- startup consumes a smaller local command set and does not queue unknown slash input;
- completion is represented as a local `/complete PREFIX` command;
- local command parsing uses whitespace fields;
- the main loop enters local-command handling only when `/` is the first raw byte;
- dispatch, help, and completion command declarations are separate;
- local command output is direct text rather than typed presentation events;
- proposed scan/watch/finding operations and endpoint services do not exist.

Consequences:

1. Leading whitespace currently changes command recognition even though directive parsing itself trims fields.
2. `strings.Fields` cannot provide a robust quoted, typed, or privacy-aware argument grammar.
3. Expanding the switch would duplicate availability and completion logic.
4. Scan and watch commands require new application services, result schemas, sensors, lifecycle, and audit—not merely terminal syntax.
5. The current built-in manager is credential-focused; endpoint-security commands should not be advertised until the corresponding authenticated services exist.

## 12. Core 15 command decision

Aegis should keep the top-level vocabulary small. The recommended core is exactly 15 commands:

```text
 1. /help
 2. /status
 3. /context
 4. /authority
 5. /limits
 6. /scan
 7. /watch
 8. /findings
 9. /investigate
10. /timeline
11. /report
12. /audit
13. /cancel
14. /clear
15. /exit
```

This is the stable user vocabulary. Detail belongs under subcommands and flags rather than in dozens of top-level names. For example, `/scan secrets` is canonical and `/scan-secrets` may be an alias, while help and audit still identify the operation as `scan.secrets`.

### 12.1 Exact core behavior

| Command | Bare-command behavior | Important nested forms |
|---|---|---|
| `/help` | Open state-aware command and keyboard help | `/help scan`, `/help watch`, `/help findings` |
| `/status` | Show concise operational posture, active scan/watch work, and highest-priority findings | `/status --details` |
| `/context` | Show authenticated identity, logical agent, stanza, mandate, runtime, and policy provenance | `/context --details` |
| `/authority` | Show effective observation, investigation, and response-request authority | `/authority <operation>` |
| `/limits` | Show blind spots, unavailable sensors, unsupported scopes, event loss, and isolation limits | `/limits scan`, `/limits watch` |
| `/scan` | Run the bounded high-level default scan in the current authorized default scope | `/scan quick`, `/scan full`, `/scan secrets`, `/scan processes`, `/scan network`, `/scan files`, `/scan persistence`, `/scan permissions`, `/scan runtime`, `/scan sensors` |
| `/watch` | Ensure the bounded default observation watch is active; return its status if already active | `/watch start <profile>`, `/watch status`, `/watch events`, `/watch stop` |
| `/findings` | List current findings using a safe concise default filter | `/findings <id>`, `/findings --severity high`, `/findings --since 1h` |
| `/investigate` | Start or continue a bounded investigation for one finding or typed target | `/investigate <finding-id>`, `/investigate status <id>`, `/investigate cancel <id>` |
| `/timeline` | Show a correlated, bounded event timeline for the current scope | `/timeline <finding-id>`, `/timeline --since 30m` |
| `/report` | Produce a sanitized posture report preview | `/report finding <id>`, `/report incident <id>`, `/report export ...` when policy permits |
| `/audit` | Show recent authoritative Aegis security/control events | `/audit verify`, `/audit show <event-id>` |
| `/cancel` | Request cancellation of the current foreground operation | `/cancel <operation-id>` |
| `/clear` | Clear only the local display | no security state, transcript authority, finding, watch, or operation is changed |
| `/exit` | Begin bounded session cleanup | `/quit` may remain an explicit alias |

`/status`, `/context`, and `/authority` intentionally remain separate:

- status answers **what is happening now?**
- context answers **who and which security context am I operating as?**
- authority answers **what can this context actually request or do?**

`/limits` is equally important for security honesty. It answers **what can Aegis not currently see, establish, or enforce?**

### 12.2 Bare `/scan`

Bare `/scan` must be a real useful operation rather than another help alias. It runs one versioned default profile, tentatively named `core`, with these properties:

- read-only;
- bounded in time, collection volume, and concurrency;
- scoped to the current authorized default, never silently promoted to the whole host or fleet;
- composed from only currently connected scan modules;
- always reports included and omitted modules;
- always reports effective scope, source health, policy/rule revision, and coverage limits;
- returns a stable scan ID and one of the defined complete/partial/degraded/failure outcomes;
- does not send collected evidence to Hermes by default;
- does not perform containment, remediation, persistence, sensor installation, or policy mutation.

The initial Aegis-native `core` profile should prefer checks Aegis can establish authoritatively:

```text
runtime identity and effective configuration
stanza, mandate, and authority consistency
effective tool and credential scopes
broker and route state
sensor/control health
audit-chain health
available narrow workspace/session checks
```

Host modules such as persistence or host-wide process inspection join the aggregate only when a real authorized endpoint service exists. Their absence must appear in scan coverage rather than disappear silently.

Examples:

```text
/scan
/scan --scope agent-session
/scan quick --scope workspace
/scan full --scope host
/scan secrets --scope workspace
/scan processes --scope agent-session
```

`/scan full --scope host` is explicit because both cost and privacy differ materially from the default. A broad preflight may be required even though the operation is read-only.

### 12.3 Bare `/watch`

Bare `/watch` should also be useful. Its semantics are:

> Ensure one default observation-only threat watch exists for the current authorized default scope, then display its current state.

This is idempotent:

- if no equivalent watch exists, Aegis starts one;
- if exactly one equivalent watch is active, Aegis returns its status and does not create a duplicate;
- if multiple candidates somehow exist, Aegis reports ambiguity rather than choosing silently;
- if the required service or authority is unavailable, Aegis reports the exact limitation and does not pretend to watch.

The default watch must be:

- observation-only;
- leased and bounded;
- attached to an explicit session or persistent-service owner;
- explicit about scope, sensors, rules, event loss, retention, and expiry;
- incapable of silently installing sensors or enabling automatic response;
- stopped during session cleanup unless separately authorized persistent ownership is real.

Examples:

```text
/watch
/watch status
/watch events --since 10m
/watch start processes --scope agent-session
/watch start threats --scope host --lease 1h
/watch stop <watch-id>
```

Bare `/watch` does not mean “watch the entire machine forever.” The UI should summarize it in concrete terms, for example:

```text
Watch: active
Scope: current Aegis agent session
Mode: observation only
Coverage: process identity, Aegis authority drift, broker denials
Unavailable: host persistence events, packet telemetry
Lease remaining: 42m
Dropped events: 0 reported
```

### 12.4 Why response is not one of the core 15 yet

There should not be a generic top-level `/respond`, `/fix`, or `/kill` command in the initial core. Those names conceal target, blast radius, reversibility, and enforcement semantics.

When response adapters exist, investigation and finding views should expose only the specific typed actions currently available, such as `revoke session`, `freeze process tree`, or `block destination`. A later stable `/response` family may be justified, but it should not be reserved as decorative syntax before deterministic enforcement, approval, verification, rollback, and receipts are implemented.

### 12.5 Aliases outside the core count

Aliases do not enlarge the conceptual command surface when they canonicalize to one core operation. Recommended compatibility/convenience aliases are:

```text
/quit             -> /exit
/scan-secrets     -> /scan secrets
/scan-processes   -> /scan processes
/scan-network     -> /scan network
/scan-files       -> /scan files
```

Aliases must appear as aliases in help, policy, telemetry, and deprecation messages. They must not have independent authorization or implementation paths.

## 13. Recommended command surface by phase

### Phase A — command substrate

Implement or consolidate:

```text
/help
/status
/context
/authority
/limits
/audit
/cancel <operation-id>
/clear
/exit
```

Command discovery is part of `/help`; connected capabilities are summarized by `/status`, `/authority`, and `/limits` rather than adding more top-level vocabulary.

Deliver:

- typed command registry;
- exact parser and literal-slash escape;
- state-aware help/completion;
- typed local result events;
- lifecycle and cancellation integration;
- PTY and parser fuzz tests;
- no model fallback for slash input.

### Phase B — Aegis-native inspection

Add only inspection Aegis can already authoritatively source:

```text
/scan runtime
/scan permissions
/scan configuration
/scan sensors
/findings
/findings <id>
/audit verify
```

This phase should focus on charter, stanza, mandate, Hermes mapping, broker, credential metadata, route, audit, and control-health drift.

### Phase C — bounded snapshot adapters

After selecting and validating sensors/adapters:

```text
/scan secrets --scope workspace
/scan processes --scope agent-session
/scan network --scope agent-session
/scan files --scope <approved-selection>
/scan persistence --scope host
```

Start with narrow scopes and explicit limitations. Do not imply fleet or host-wide coverage from session-only telemetry.

### Phase D — continuous watch

Add managed, leased watch resources with health and loss semantics:

```text
/watch start ...
/watch status ...
/watch events ...
/watch stop ...
```

Persistent watches require a separately authorized service lifecycle rather than dependence on the interactive TUI process.

### Phase E — investigation workflow

Add durable findings, evidence references, timeline, assignment, classification, suppression policy, and reporting.

### Phase F — response requests

Add only narrow typed responses backed by real enforcement adapters, exact authorization, idempotency, postcondition verification, rollback where possible, and receipts.

## 14. Required test matrix

### Parser tests

- command at first byte and after allowed leading whitespace;
- `//` literal slash escape;
- unknown commands never reach Hermes;
- exact matching and no abbreviation execution;
- explicit alias canonicalization;
- quoting and escaping;
- duplicate/unknown flags;
- malformed durations, scopes, IDs, and paths;
- oversized tokens and total input;
- Unicode and malformed UTF-8 behavior;
- no shell expansion, chaining, pipes, or redirects;
- multiline rejection or command-specific acceptance.

### Authority tests

- no authentication means deny;
- zero stanza matches means deny;
- multiple matches mean deny;
- stanza capabilities never union;
- slash text cannot change stanza or mandate;
- unavailable commands are not advertised as available;
- dispatch and pre-execution revalidation;
- target drift and PID reuse fail closed;
- expired approval and replay fail closed.

### Routing tests

- recognized commands never enter ingress/Hermes/model transcript;
- escaped slash content does enter ordinary guarded routing;
- command errors never fall through to the model;
- model cannot emit a slash command to invoke local dispatch;
- pasted assistant text cannot synthesize input events;
- startup/degraded/active/cleanup states expose the correct command set.

### Scan/watch tests

- effective scope and exclusions always reported;
- no-findings result never says “safe”;
- partial/degraded sensor state cannot produce complete status;
- secret candidates absent from all output and retention surfaces;
- dropped events and disconnects visible;
- watch lease expiry and session cleanup deterministic;
- cancellation state distinguished from rollback;
- persistent watch claims backed by a real persistent service;
- monitoring, detection, denial, and mitigation wording remains distinct.

### Terminal and output tests

- command completion at narrow widths and no color;
- screen-reader/plain mode;
- untrusted sensor/integration/model text sanitized;
- output cannot forge trust or approval surfaces;
- resize, EOF, terminal loss, Ctrl-C, and Ctrl-D behavior;
- bounded event/transcript retention;
- no command-history leakage of protected values.

## 15. Open product decisions

The following should be resolved before a normative implementation specification:

1. Is `//` the final literal-slash escape, and how is it represented in multiline composition?
2. Which command argument parser and quoting rules become stable user-facing syntax?
3. What exact modules, time bound, and collection bound define version 1 of the bare `/scan` `core` profile?
4. What exact scope, sensor set, lease, and retention define version 1 of bare `/watch`?
5. Which scopes exist in the first endpoint release?
6. Which services produce Aegis-native runtime/permission scans before host sensors exist?
7. Which endpoint sensor/adaptor combinations are selected for Linux-first snapshot and event coverage?
8. Where are durable scans, findings, watches, and evidence stored, and under which retention policy?
9. Which command metadata is safe to disclose to lower-trust stanzas?
10. Which narrow response is reversible and safe enough to prove the first end-to-end response flow?
11. Are aliases like `/scan-secrets` permanent API or transitional discoverability aids?
12. Which slash commands are available in the built-in secrets manager versus a future endpoint-focused logical agent?

## 16. Launch-asset impact review

This is a research-only addition. It changes no implemented command syntax, dependency, endpoint service, launch behavior, or security claim.

Reviewed as unaffected for current implementation:

- root `README.md`;
- `LICENSE`;
- `SECURITY.md`;
- `CONTRIBUTING.md`;
- `CODE_OF_CONDUCT.md`;
- `CHANGELOG.md`;
- `docs/THREAT_MODEL.md`;
- `docs/ARCHITECTURE.md` and its diagram;
- `docs/QUICKSTART.md`;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md` and recording assets;
- release binaries and checksums;
- repository-local contributor issue material.

When slash-command behavior is implemented, the affected launch assets will include at least README/help examples, quickstart, no-key demo, recording, architecture, threat model, SECURITY, CHANGELOG, command reference, PTY test evidence, binaries/checksums for an authorized release, and contributor issue material. External issue or release creation still requires explicit owner authorization.

## Bottom line

Aegis should make security slash commands feel immediate without making them casual.

The essential contract is:

```text
user input
  -> bounded command-position detection
     -> recognized slash command
        -> exact typed parse
        -> state/authority check
        -> Aegis service
        -> typed result / approval / operation lifecycle
        -> authoritative audit where required
     -> escaped or ordinary conversation
        -> ingress guard
        -> Hermes/model lane
```

Start with a command registry and exact semantics. Then add Aegis-native authority/runtime scans. Add host scans only with truthful sensor coverage. Add `/watch` only with a real managed lifecycle and event-loss semantics. Add response commands only when deterministic enforcement, approval, verification, and receipts exist.

That preserves Aegis's central promise: one authenticated identity, one trust stanza, one clean runtime session—while giving the terminal a powerful, discoverable security-control vocabulary.
