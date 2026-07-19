# Base Manager Slash Commands

The authenticated interactive manager exposes exactly 15 canonical base names from one constructor-built typed registry:

`/help /status /context /authority /limits /scan /watch /findings /investigate /timeline /report /audit /cancel /clear /exit`

`/quit` is an alias for `/exit`. Compatibility extensions `/secret` and `/complete` are not base names; `/complete` delegates to the registry and exists for testing/compatibility. The legacy scan aliases canonicalize to typed `/scan` forms before policy and audit.

## Routing and syntax

Only composer submissions can invoke dispatch. Aegis detects command position after bounded leading whitespace, parses exact lowercase names and typed arguments locally, and consumes recognized, unknown, and malformed slash input before any Hermes path. Prefix and fuzzy matching are suggestions only. Model, runtime, tool, sensor, report, audit, and subagent text are typed untrusted presentation events and cannot invoke dispatch.

The parser supports bounded single- or double-quoted literal arguments. Inside a quoted literal only its quote character or a backslash can be escaped. It does not implement a shell: pipes, redirects, chaining, backgrounding, backticks, `$` expansion, `!`, or unquoted backslash escaping are rejected. `//` at command position removes one slash and then sends the resulting ordinary text through the normal ingress guard; it is not a bypass.

Help, completion, availability, policy/audit operation names, aliases, grammar, execution class, consequence, scope, result schema, and examples come from the same registry. Completion discloses only commands available for the current lifecycle and prerequisites.

## Implemented behavior

- `/help` renders registry metadata and remains available during startup/degraded operation.
- `/status`, `/context`, `/authority`, and `/limits` read authenticated manager context, readiness, policy, route, lifecycle, and real capability gaps. Unknown is never rendered as zero.
- `/scan` runs or attaches to a durable equivalent Aegis-native core scan. Its equivalence key binds owner, exact scope, profile/rule revision, policy/source generation, and input identity. It checks manager identity/authority consistency, Hermes discovery, effective manager tool/credential/memory/broker constraints, local-only/no-fallback policy, readiness, and audit verification. Results name included and omitted modules, health, observations, gaps, timing, IDs, and scope. An audit-chain verification failure creates a versioned authoritative finding; absence of findings is worded only as “no findings in covered scope.”
- Nested host process, network, file, persistence, dependency, secret, and sensor scans are typed unavailable because no production adapter is installed.
- `/findings` reads bounded owner/stanza-filtered durable open records. `/investigate` with no ID lists active records and never silently chooses a target; an exact visible finding ID creates or attaches a durable bounded investigation.
- `/timeline` queries recent authoritative audit/control records and names its anchor, range, append ordering, local-clock caveat, and missing external/host sources.
- `/report` requires an explicit or already attached visible investigation, creates a new immutable local revision with deterministic input/digest/evidence/audit provenance, and never exports or publishes. Bare `/report` with no attachment is unavailable.
- `/audit` lists bounded metadata-only records; `/audit verify` calls the existing audit verification authority.
- `/cancel` lists ambiguity rather than choosing among multiple operations and never claims rollback. `/clear` redraws presentation and explicitly preserves authority, runtime context, durable records, and audit. `/exit` and `/quit` request the manager’s one bounded cleanup and terminal-restoration path.

Every executed base command emits canonical metadata-only `manager_command` audit data (operation, outcome/reason, optional operation ID and scope digest); raw command text, arguments, protected values, evidence, and rendered output are excluded.

## Truthful unavailable boundary

`/watch` is recognized, authorized, audited, rendered, documented, and completed locally as `watch_source_manager_unavailable`. Aegis does not currently have a production leased event-source manager, so it creates no watch ID, lease, timer, buffer, event, or monitoring claim. No endpoint sensor was installed or contacted by this implementation.

## Blind spots and non-goals

The core scan is not host threat detection and does not inspect arbitrary processes, sockets, files, persistence, packages, model stores, or operator services. Disposable Hermes state is not a host sandbox; unmediated host filesystem/network/process routes may remain. No subagent or model participates in command parsing, authority, scan completion, finding creation, timeline/audit mutation, report evidence, or response authority. No command changes stanza, mandate, runtime, provider, model, profile, or foundational authority.
