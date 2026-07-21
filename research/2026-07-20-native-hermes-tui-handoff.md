# Native Hermes TUI handoff inside an Aegis-controlled session

- Status: Architecture research and recommendation
- Date: 2026-07-20
- Prepared for: Aegis
- Scope: terminal ownership, native Hermes TUI integration, session supervision, authority, credential brokering, approvals, lifecycle, and audit
- Does not authorize: implementation, provisioning, activation, modification of the operator's normal Hermes profile, or use of real credentials

## Executive decision

Aegis can authenticate the principal, establish exactly one security context, materialize a clean session, and then hand the terminal to the native Hermes TUI for the conversational portion of an operational session.

The recommended product boundary is:

> Aegis owns entry, identity, stanza selection, mandate, effective authority, credential use, lifecycle, verification, and audit. Hermes remains visible and owns the agent loop and native conversational experience.

This should not replace the Aegis-owned built-in manager interface. The two interfaces have different security needs:

- The **built-in manager** should continue to use an Aegis-owned terminal controller over Hermes's structured gateway because Aegis must guard ordinary input, protect credential intake, decode typed proposals, and render authoritative approvals without allowing Hermes or model output to own the terminal.
- A **user-created operational agent session** may offer an explicit native-Hermes mode after authentication and launch authorization are complete. Aegis remains the supervising parent while Hermes owns ordinary terminal interaction.
- **Noninteractive and remote clients** should use structured protocols rather than a terminal-byte tunnel.

The preferred native integration is a supervised child process with direct terminal attachment when no mid-session Aegis modal is required. A PTY relay is justified only when Aegis must suspend Hermes, regain terminal ownership, present an authoritative approval or protected-input surface, and then resume Hermes. In either design, terminal bytes are presentation only and must never become an authorization or audit protocol.

## Research question

Can Aegis be the mandatory entry point while, at a deliberate point in its runtime, presenting what is literally a normal Hermes terminal/TUI session?

This report answers:

1. Whether the current Aegis and Hermes implementations make native handoff technically possible.
2. Which process and terminal topology best preserves the native Hermes experience.
3. Which controls Aegis can retain while Hermes owns the screen and keyboard.
4. How credential brokering and protected intake interact with terminal ownership.
5. Why the built-in manager and operational-agent experience should not use the same presentation topology.
6. What must be prototyped and tested before this becomes supported behavior.

## Method and evidence labels

Repository source, retained specifications, the installed Hermes executable, and the installed Hermes 0.18.2 source were inspected on 2026-07-20. The following local commands were exercised without launching a session or changing runtime state:

- `hermes --version`
- `hermes --help`
- `hermes acp --help`
- `hermes serve --help`
- `hermes gateway --help`

The inspected executable reported Hermes Agent `0.18.2`. Its help confirms `--tui`, `--safe-mode`, `--toolsets`, provider/model overrides, session resume, ACP mode, and a headless JSON-RPC/WebSocket backend. The Aegis adapter currently supports Hermes `>=0.18.0,<0.19.0`.

Evidence labels used below:

- **Verified:** directly supported by inspected Aegis code, specifications, executable output, or installed Hermes source.
- **Recommendation:** proposed architecture or product behavior.
- **Unresolved:** requires a prototype, compatibility test, or product decision.

No normal Hermes profile, Aegis runtime configuration, credential authority, external account, or service was modified.

## Terminology

### Native terminal handoff

Aegis starts Hermes as a supervised child whose stdin, stdout, and stderr are attached to the principal's terminal. Hermes directly performs terminal mode changes, rendering, input handling, and resize handling.

### PTY tunnel

Aegis allocates a pseudoterminal. Hermes is attached to the PTY slave, while Aegis relays bytes between the real terminal and the PTY master. Aegis remains capable of pausing the relay and temporarily taking terminal ownership.

### Structured frontend

Aegis communicates with the Hermes TUI gateway using JSON-RPC events and renders its own interface. This uses the Hermes agent loop but is not the native Hermes TUI.

### Control plane and presentation plane

- The **control plane** contains authenticated identity, stanza selection, mandate state, projected tools, credential scopes, lifecycle decisions, broker requests, approvals, verification, and audit.
- The **presentation plane** contains terminal input bytes, rendering bytes, key sequences, resize events, and the visible Hermes interface.

Aegis must not infer control-plane facts from presentation-plane bytes.

## Current implementation facts

### Aegis already proves direct native attachment

`internal/runtime/hermes/hermes.go:355-379` implements `RunDesignForeground`. It:

- creates a disposable runtime home;
- discovers the exact Hermes executable;
- launches `hermes --safe-mode --tui --toolsets no_mcp`;
- applies Aegis's minimal environment;
- attaches the caller's input, output, and error streams directly to Hermes;
- waits for the foreground process;
- removes the disposable home unless retention was explicitly requested.

This is already a native terminal handoff. It proves that Aegis can remain the entry point while the user interacts with the real Hermes TUI.

It does not yet prove the complete operational design proposed here. The design path has no operational mandate, session credential broker, mid-session Aegis approval handoff, or native-session receipt contract.

### Operational launch is not currently an interactive tunnel

`internal/runtime/hermes/hermes.go:118-227` launches operational Hermes processes with approved toolsets and disposable state, but it obtains pipes for stdin, stdout, and stderr. For ordinary sessions, stdout is deliberately drained rather than presented at `internal/runtime/hermes/hermes.go:209-213`.

Therefore:

- the adapter already controls the operational process, environment, home, toolset, and lifecycle;
- the current operational launch path is not a usable native terminal session;
- native operational handoff requires a new foreground/supervised presentation path rather than merely documenting current behavior differently.

### The manager intentionally uses a structured gateway

`internal/manager/hermes_process.go:36-98` launches `python -m tui_gateway.entry` with a disposable home, safe-mode controls, an exact toolset, an Aegis inference proxy route, and JSON-RPC stdio pipes.

`internal/manager/gateway.go:42-89` validates bounded newline-framed JSON objects and waits for `gateway.ready`. `internal/manager/gateway.go:92-198` creates a session, submits prompts, receives typed message events, bounds responses, and poisons a session after an interrupted turn whose late events can no longer be safely correlated.

This arrangement gives Aegis structured custody of manager input and output. It is not a terminal tunnel and should not be presented as one.

### The manager security contract forbids direct Hermes attachment

`specs/BASE_MANAGER_END_TO_END.md:134-145` requires the built-in manager to use Hermes safe mode and the structured gateway and explicitly states that Hermes must not attach directly to the principal terminal.

`specs/TERMINAL_EXPERIENCE_OPERATIONAL_IMPLEMENTATION.md:22-24` requires one Aegis-owned interface with persistent authenticated trust context. At `:106-120`, it rejects silently attaching the terminal to Hermes as a fallback. At `:187-195`, it requires exactly one Aegis presentation controller during manager operation, with only explicit, serialized protected-input handoff.

A native Hermes mode is therefore compatible with the broader Aegis product direction only when it is a separate operational-session surface. It is not a permissible shortcut around the built-in manager requirements.

### Hermes itself separates its TUI from its structured gateway

The installed Hermes source shows that the native TUI is a frontend over `tui_gateway.entry`. The gateway transport is newline-framed JSON-RPC and can run over stdio or WebSocket. The inspected gateway emits `gateway.ready` and supports structured methods including `session.create`, `prompt.submit`, and `tools.show`.

This gives Aegis two legitimate integration levels:

1. launch the native Hermes frontend and supervise it as an opaque terminal application; or
2. use the gateway protocol and provide an Aegis frontend.

Trying to combine both by scraping rendered Hermes output would discard the advantages of the structured protocol without obtaining reliable control-plane facts.

## Product boundary

### Direct personal Hermes remains independent

The operator should retain an explicit distinction:

```text
Direct personal use:
operator -> hermes -> normal ~/.hermes state

Aegis-controlled native use:
authenticated operator -> Aegis -> approved session projection
                       -> supervised native Hermes TUI -> disposable state
```

A session started directly with `hermes` is not Aegis-controlled and must not be represented in Aegis audit or receipts as if it were. Conversely, an Aegis-controlled session must not inherit the normal profile merely because it runs under the same host account.

### Aegis remains the entry point

“Entry point” should mean an enforceable control boundary, not permanent ownership of every rendered cell. Before Hermes receives terminal ownership, Aegis must complete all prerequisites that determine effective authority:

1. authenticate the subject outside the model;
2. select exactly one trust stanza or deny;
3. resolve the exact charter revision and mandate;
4. verify Hermes executable and supported version;
5. construct a closed session projection;
6. create a fresh disposable home;
7. materialize only approved configuration and bridges;
8. establish session-bound broker capabilities;
9. verify the active route and effective tools where observable;
10. show the operator an authoritative launch summary;
11. enter Hermes only after the launch decision succeeds.

Hermes should never be used as the UI for choosing its own stanza, expanding its own tools, selecting ambient credentials, approving its own configuration, or authorizing provisioning.

### Hermes remains visible

The transition should explicitly name Hermes and then display the real Hermes interface. Aegis should not skin Hermes and claim the result is native, nor should it hide Hermes behind a generic “agent runtime” label.

A possible transition is:

```text
Aegis session ready

Principal       javi / authenticated host identity
Agent           research-assistant
Trust stanza    principal
Runtime         Hermes Agent 0.18.2
Authority       web + Aegis broker
Credentials     GitHub read through Aegis; reusable value not delivered
Expires         14m 52s
Isolation       disposable runtime state; not a host sandbox

Enter Hermes
```

The exact production copy and interaction require design review, accessibility behavior, and narrow-terminal treatment. The important semantic point is that the launch summary is authoritative Aegis output and the following screen is visibly Hermes.

## Architecture options

### Option A: supervised direct attachment

```text
real terminal
    |
    +-- Aegis parent and session supervisor
            |
            +-- Hermes child, directly attached to terminal
            +-- Aegis credential broker
            +-- mandate expiry/revocation timer
            +-- authoritative audit writer
```

Aegis prepares the terminal, stops its own renderer, launches Hermes in a new process group, and waits. Hermes reads and writes the real terminal. On normal exit, signal, expiry, or revocation, Aegis regains control, restores terminal state, cleans up the projection, invalidates capabilities, and renders a final authoritative result.

Advantages:

- the experience is literally the native Hermes TUI;
- keyboard handling, streaming, tool cards, themes, resize behavior, and upstream improvements remain Hermes-owned;
- there is little terminal translation code;
- latency and rendering behavior are close to direct Hermes use;
- Hermes remains unmistakably visible.

Limitations:

- Aegis cannot safely draw over Hermes while Hermes is active;
- mid-session protected intake requires terminating, suspending, or otherwise coordinating with Hermes;
- Aegis cannot sanitize Hermes rendering before it reaches the terminal;
- an Aegis global escape key cannot be reserved without an input interception layer;
- direct terminal attachment provides lifecycle supervision but not a structured transcript or tool-event stream.

Recommendation: use this mode when all consequential authority is fixed before launch and no Aegis-owned modal is required during ordinary conversation.

### Option B: supervised PTY relay

```text
real terminal <-> Aegis relay/controller <-> PTY master
                                             |
                                          PTY slave
                                             |
                                        Hermes child

Aegis control plane <-> broker, mandate, approval, audit, lifecycle
```

Aegis creates a PTY, launches Hermes on the slave, puts the real terminal into the required mode, and relays input and output. Resize events are copied to the child PTY. Signals, EOF, terminal loss, and child exit are handled by one lifecycle owner.

Advantages:

- retains the native Hermes TUI;
- enables Aegis to pause byte forwarding and regain the terminal;
- permits a deliberately reserved Aegis escape sequence;
- allows an Aegis-owned approval or protected-input screen between Hermes interactions;
- improves terminal-loss and child-process supervision options;
- can support transcript capture if explicitly authorized and safely classified.

Costs and risks:

- terminal emulation and restoration are easy to get wrong;
- raw byte forwarding can split escape sequences and UTF-8;
- both applications must never believe they own terminal mode simultaneously;
- background output from Hermes can corrupt an Aegis modal unless Hermes is paused and output is drained or bounded correctly;
- reserved key handling can conflict with Hermes keybindings, bracketed paste, mouse mode, and terminal protocols;
- captured PTY output can contain model text, secrets typed into ordinary chat, paths, and terminal controls and therefore must not be casually logged;
- terminal output still cannot be treated as authoritative events.

Recommendation: introduce a PTY only if a concrete requirement cannot be met by direct attachment or a structured frontend. Do not build a PTY proxy merely to make Aegis appear to own the session.

### Option C: Aegis frontend over Hermes gateway

```text
principal terminal
    |
Aegis terminal controller
    |
bounded JSON-RPC stdio/WebSocket
    |
Hermes structured gateway
```

Advantages:

- Aegis owns and guards every submission;
- model/runtime output can be decoded, bounded, sanitized, and labeled;
- approvals and protected intake remain in one coherent terminal controller;
- structured events can drive accurate status and lifecycle presentation;
- machine and remote clients can reuse the same control boundary.

Limitations:

- this is not the native Hermes TUI;
- Aegis must implement and maintain composer, streaming, tool cards, accessibility, resize, history, and terminal safety;
- Hermes frontend improvements do not arrive automatically;
- protocol compatibility becomes an adapter responsibility.

Recommendation: retain this mode for the built-in manager and other approval-sensitive control-plane experiences.

### Rejected option: scrape or filter Hermes terminal output

Aegis should not parse ANSI output, text labels, tool cards, or model prose to discover:

- which tool executed;
- whether an approval occurred;
- which credential was requested;
- whether a session changed model or authority;
- whether a command succeeded;
- what should enter authoritative audit.

Rendered text is untrusted, version-sensitive, ambiguous, and potentially adversarial. If Aegis needs a fact, it must obtain it from process state, its own broker, an authenticated side channel, or the structured Hermes gateway.

### Rejected option: replace Aegis with `exec(hermes)`

Replacing the Aegis process with Hermes would provide a clean native experience but discard the lifecycle owner. Aegis would no longer be able to guarantee bounded cleanup, terminate on mandate revocation, invalidate session capabilities, restore state, or write a final authoritative receipt.

Hermes should be a child process, normally in a dedicated process group. Aegis remains alive as supervisor until cleanup completes.

## Recommended topology

### Default operational native mode

Use a supervised child attached directly to the terminal when:

- the terminal is local and interactive;
- the stanza and mandate are fixed before launch;
- effective tools and routes are verified before entry;
- downstream credentials stay behind brokers;
- no mandatory per-operation Aegis modal is required;
- session expiry can terminate the process cleanly;
- structured terminal transcript capture is not required.

### Approval-sensitive native mode

Use a PTY only when an approved feature requires a temporary Aegis-owned interaction during the session. The PTY controller must have an explicit state machine, for example:

```text
preflight
  -> aegis_owns_terminal
  -> hermes_starting
  -> hermes_owns_terminal
  -> handoff_requested
  -> hermes_quiesced
  -> aegis_modal
  -> hermes_resuming
  -> hermes_owns_terminal
  -> terminating
  -> restoring_terminal
  -> complete
```

Illegal transitions must fail closed. In particular, Aegis must not enter an approval modal until Hermes input is blocked and Hermes output can no longer mutate the visible approval surface.

### Built-in manager mode

Continue using the current structured gateway and Aegis-owned interface. The manager handles credential administration and deterministic approvals, and its existing specifications explicitly require this stronger terminal boundary.

## Credential architecture implications

### Native TUI does not require giving credentials to Hermes

A native terminal does not inherently weaken credential custody. Presentation ownership and credential possession are separate questions.

Aegis can launch native Hermes with:

- no reusable downstream credential in argv;
- no reusable downstream credential in the generated home;
- no reusable downstream credential in environment variables;
- only exact Aegis-owned broker registrations;
- a short-lived capability bound to the exact session and runtime;
- typed operations whose destinations and result schemas are constrained by Aegis.

The current GitHub broker is evidence for this pattern, although it implements only `github.get_repository.v1`. The broker, not Hermes, resolves and applies the reusable credential.

### Environment injection remains a weaker compatibility mode

If Aegis injects a provider or downstream API key into the Hermes process environment, it prevents persistence in the normal profile but does not keep the value outside Hermes. Native terminal handoff neither improves nor worsens that process-level exposure.

The distinction must remain explicit:

- **brokered credential use:** Hermes receives a bounded operation and sanitized result, not the reusable credential;
- **process credential delivery:** Hermes possesses the credential for the process lifetime;
- **persistent profile credential:** explicitly outside the target Aegis-controlled design.

Receipts and UI must not describe process injection as broker isolation.

### Protected input cannot be typed into Hermes

Secret creation, rotation, recovery, or any other protected input must never occur in the native Hermes composer. If the user types a secret into ordinary Hermes input, it may enter model context, session history, terminal scrollback, tool arguments, or runtime logs.

Protected input requires one of two designs:

1. perform it before entering Hermes or after leaving Hermes; or
2. use a PTY handoff that quiesces Hermes, restores an Aegis-owned protected-input surface, collects the value without echo, completes the authorized operation outside Hermes, and then resumes the session without forwarding the secret.

A model request such as “paste your API key” must never be sufficient to trigger protected intake. A structured, policy-authorized broker event and an authenticated Aegis state transition are required.

### Broker results need a structured side channel

When Hermes invokes an Aegis-owned MCP or broker tool, the request already travels through a structured tool protocol. Aegis should authorize and audit from that request, not from anything shown in the TUI.

For operations requiring operator approval, the broker needs a bounded outcome such as:

- completed;
- denied by policy;
- approval required;
- approval expired;
- session expired or revoked;
- downstream failure with sanitized metadata.

Whether Hermes can pause one active tool call while Aegis performs a PTY modal without deadlocking the TUI is unresolved and must be prototyped against every supported Hermes version.

## Authority and lifecycle behavior

### Authority is immutable inside the native session

The Hermes TUI must not offer an effective path to broaden the approved session. Commands or configuration changes that would alter model, provider, fallback, toolsets, MCP servers, plugins, credentials, memory, hooks, gateway behavior, or persistence must be:

- absent by capability removal;
- rejected by the generated projection or broker;
- detected by runtime verification where observable; or
- treated as requiring a new mandate and clean session.

Prompt content and Hermes slash commands are not authentication or authorization.

### Expiry and revocation remain out of band

Aegis must maintain its own timer and revocation state while Hermes runs. It must not depend on Hermes noticing or narrating expiry.

On expiry or revocation, Aegis should:

1. mark the mandate inactive authoritatively;
2. reject new broker operations immediately;
3. request graceful Hermes termination;
4. terminate the process group after a bounded grace period;
5. invalidate session capabilities;
6. remove disposable state according to retention policy;
7. restore the terminal;
8. record the termination reason and cleanup outcome;
9. show an Aegis-owned final message after Hermes no longer controls the terminal.

### Terminal loss and signals

The supervisor must own:

- `SIGINT`, `SIGTERM`, and hangup behavior;
- process-group termination;
- child exit collection;
- window-size propagation in PTY mode;
- terminal mode restoration;
- bounded cleanup after startup failure;
- idempotent cleanup after concurrent exit and revocation.

Signal semantics must be explicit. For example, `Ctrl+C` may belong to Hermes during an active turn, while external `SIGTERM` must remain an Aegis lifecycle event. The exact mapping requires a prototype because forwarding behavior differs between direct foreground process groups and PTY-mediated children.

## Audit boundary

Aegis can authoritatively record:

- authenticated subject provenance;
- selected stanza and mandate;
- charter and projection digests;
- Hermes executable and observed version;
- generated artifact digests;
- expected and verified effective tools where observable;
- broker requests, decisions, destinations, and sanitized results;
- start, expiry, revocation, signal, exit, and cleanup outcomes.

Aegis cannot authoritatively infer from an opaque native TUI:

- every user message;
- every model response;
- every internal Hermes event;
- complete tool activity outside Aegis-owned brokers;
- whether rendered text truthfully describes an operation.

A native operational session therefore needs an explicit audit contract. Either:

1. session-level and Aegis-broker audit is sufficient, and the receipt says so; or
2. Hermes supplies a separate structured event stream whose identity and compatibility Aegis verifies.

Aegis should not silently capture raw PTY traffic. If transcript capture is ever added, it requires a distinct retention, redaction, disclosure, encryption, and authorization design.

## Security analysis

### Properties retained by either supervised native design

Provided implementation follows the proposed boundary, Aegis retains:

- authentication outside the model;
- exactly one selected stanza;
- digest-bound mandate and projection;
- disposable Hermes state;
- a minimal environment;
- exact broker capabilities;
- session-bound credential operations;
- out-of-band expiry and revocation;
- process-group termination;
- Aegis-authoritative broker and lifecycle audit.

### Properties reduced compared with the manager frontend

A native TUI gives Hermes direct presentation authority. Consequently, Aegis does not mediate every composer submission or sanitize every rendered byte. Compared with the manager:

- ordinary input can reach Hermes without Aegis's manager ingress guard;
- Hermes/model output can emit terminal control sequences according to Hermes's own safety behavior;
- Aegis cannot maintain unforgeable persistent security chrome while Hermes owns the full terminal;
- Aegis cannot derive typed proposals from the visible UI;
- Aegis cannot guarantee that the native interface distinguishes model claims from Aegis authority unless the transition boundary is very clear.

This is acceptable only for a deliberately different operational-agent surface. It is not acceptable for credential administration or foundational authority approval.

### Host isolation is unchanged

A native handoff does not create filesystem, network, container, VM, or operating-system confinement. A stanza granting Hermes terminal, file, web, browser, or arbitrary MCP authority still grants the corresponding host-facing capability unless separate enforcement constrains it.

Disposable homes prevent ambient Hermes-state inheritance; they are not host sandboxes.

### Threats specific to PTY mode

A PTY implementation must address:

- terminal escape injection and forged Aegis-like screens;
- incomplete UTF-8 and escape sequences across relay reads;
- terminal mode leakage after crashes;
- child output racing an Aegis approval modal;
- input leakage during ownership transitions;
- buffered keystrokes delivered to the wrong owner;
- bracketed-paste confusion;
- resize races and zero-sized terminals;
- child process escape from the expected process group;
- unbounded output and memory growth;
- hidden logging of terminal content;
- deadlock while a broker tool waits for approval and the TUI waits for the tool.

The safest visual rule is that Aegis authoritative output appears only after Hermes has been quiesced and the screen has been reset by Aegis. A persistent Aegis status bar composited over arbitrary Hermes output would create substantial terminal-emulation complexity and should not be the first implementation.

## Recommended user experience

### Entry

1. The operator runs `aegis` or an explicit session-entry command.
2. Aegis authenticates and resolves one stanza.
3. Aegis shows the exact runtime, projection, authority, credential mode, expiry, and isolation limit.
4. The operator explicitly enters the native Hermes session.
5. Aegis marks the visual ownership transition clearly.
6. Hermes starts with its native TUI and native runtime identity.

The transition should not require the operator to approve authority already approved by the charter and mandate, but it should make the effective context reviewable before terminal ownership changes.

### During the session

- Hermes owns ordinary conversation, native slash commands that remain within the projection, streaming, and tool presentation.
- Aegis supervises the process, mandate, brokers, and session capabilities out of band.
- The native TUI should not show an Aegis trust bar unless Hermes gains a structured, non-forgeable integration designed for that purpose.
- A reserved return-to-Aegis key is optional and requires PTY interception; an ordinary Hermes exit is sufficient for the direct-attachment MVP.

### Exit

1. Hermes exits normally, Aegis revokes the session, the mandate expires, or a signal initiates cleanup.
2. Aegis ensures Hermes no longer owns the terminal.
3. Aegis restores terminal state and invalidates capabilities.
4. Aegis cleans disposable state according to policy.
5. Aegis displays an authoritative summary containing reason, duration, runtime identity, mandate state, broker-operation count, cleanup status, and audit receipt ID without model text or secret material.

## Proposed implementation sequence

This report does not authorize implementation. If authorized later, the work should proceed in bounded stages.

### Stage 1: native foreground operational prototype

Add a separate adapter path rather than changing the existing background launch contract in place. The prototype should:

- reuse discovery, tool resolution, projection, minimal environment, credentials, and broker setup;
- launch Hermes as a child process, not through a shell and not by replacing Aegis;
- attach terminal streams directly;
- create a dedicated process group;
- enforce one short mandate expiry timer;
- terminate and clean up on expiry;
- restore the terminal and print a final Aegis result;
- use only fake provider/broker fixtures and disposable homes in automated tests.

This stage should not add mid-session approvals, PTY interception, transcript capture, persistent profiles, or real credentials.

### Stage 2: explicit session presentation contract

Define typed service results for:

- entry authorized or denied;
- runtime started;
- terminal ownership transferred;
- child exited;
- mandate expired or revoked;
- cleanup completed or failed;
- terminal restored or restoration uncertain.

Keep stdout for the terminal application and final command result, stderr for coordinated diagnostics, and authoritative audit separate from both.

### Stage 3: brokered native-session proof

Verify a native Hermes session with exactly one fake Aegis MCP operation and no reusable credential in Hermes. The proof must demonstrate:

- exact live tool registration;
- exact session binding;
- denial after expiry or revocation;
- sanitized result delivery;
- no canary in environment, argv, generated files, output, logs, audit, or receipts.

### Stage 4: PTY approval experiment

Only after direct handoff is stable, prototype one fake approval-required tool call. Determine whether Hermes can be safely quiesced while the broker waits and whether the terminal can be handed to an Aegis modal without output races or deadlock.

This experiment should be discarded if it requires scraping Hermes output, modifying the normal Hermes profile, or weakening the exact broker protocol.

### Stage 5: supported PTY mode, if justified

If the experiment succeeds, implement:

- an explicit terminal-ownership state machine;
- PTY allocation and resize propagation;
- bounded bidirectional relay;
- output draining while paused;
- protected modal entry and restoration;
- signal and terminal-loss handling;
- accessible fallback or honest denial;
- compatibility tests for every supported Hermes minor range.

## Verification plan

### Unit tests

- command construction never uses a shell;
- projected environment excludes ambient credentials and normal Hermes state;
- terminal-mode state transitions reject illegal ownership changes;
- expiry and revocation invalidate brokers before process cleanup completes;
- cleanup is bounded and idempotent;
- receipt fields contain no terminal transcript or secret value;
- direct-attachment mode is rejected for the built-in manager;
- non-TTY invocation never starts native mode or prompts.

### PTY integration tests

Use an isolated fake Hermes process before testing real Hermes. Cover:

- alternate-screen entry and exit;
- raw/canonical mode changes;
- fragmented UTF-8 and ANSI sequences;
- large and sustained output;
- bracketed paste;
- resize storms and narrow terminals;
- `Ctrl+C`, EOF, `SIGTERM`, hangup, and child crash;
- mandate expiry during idle, rendering, and an active turn;
- broker denial while Hermes is still alive;
- output arriving during an Aegis modal;
- terminal restoration after every failure point;
- rich and `AEGIS_ACCESSIBLE=1` behavior where Aegis owns the screen.

### Real Hermes compatibility tests

Against the exact supported Hermes versions, verify:

- native TUI starts in the generated disposable home;
- approved toolsets are effective;
- normal profiles, rules, memories, skills, plugins, MCP servers, provider pools, and gateway settings are not inherited;
- direct attachment behaves correctly on resize and interrupt;
- process-group termination removes all children;
- broker-enabled mode reports exactly the approved Aegis tool registrations;
- no unsupported Hermes slash command can materially broaden authority;
- the final Aegis receipt matches actual exit and cleanup behavior.

Tests must not target the developer's real Hermes home, Aegis installation, credential database, Ollama store, or external credentials.

### Security tests

- generated-canary credential non-disclosure across argv, environment, home, PTY capture, logs, audit, receipts, and errors;
- stale, missing, wrong-session, wrong-runtime, and revoked broker capability denial;
- malicious model output that visually imitates an Aegis approval;
- child process attempting to survive outside the expected process group;
- terminal output intended to overwrite prior authority text;
- buffered keystrokes at ownership-transition boundaries;
- approval timeout and terminal loss while a broker call is pending;
- runtime configuration mutation attempts after launch.

## Product decisions still required

1. Is native Hermes the default operational-agent experience, or an explicit mode such as “Enter native Hermes”?
2. Are user-created operational sessions allowed to submit ordinary unguarded chat directly to Hermes, or must all Aegis-controlled input pass an ingress scanner?
3. Is session-level plus Aegis-broker audit sufficient, or is a complete structured Hermes event stream required?
4. Do any operational broker actions require per-call operator approval, or can mandates pre-authorize narrowly bounded operations?
5. If approval is required, may Aegis suspend Hermes, or should the operation fail with instructions to leave Hermes and approve from Aegis?
6. Is transcript retention forbidden by default, optional, or required for some deployments?
7. Should Aegis expose a reserved return key, and which terminals/key protocols must it support?
8. Which Hermes slash commands and configuration mutation surfaces remain reachable in native mode?
9. Must native sessions support resume, and if so how does resume preserve the clean-session and mandate rules?
10. Is direct terminal attachment sufficient for the first release, deferring PTY-mediated modal handoff?

## Recommended decisions for the first slice

For the smallest defensible implementation:

- native Hermes is explicit, not the built-in manager default;
- use supervised direct attachment rather than a PTY;
- perform all foundational approvals and protected credential intake before entry;
- allow only pre-authorized, narrowly typed broker operations during the session;
- require exit and a new Aegis interaction for any new approval or secret intake;
- do not capture a terminal transcript;
- audit session lifecycle and Aegis-owned broker operations only;
- do not support resume initially;
- terminate on expiry, revocation, runtime mismatch, or broker-binding invalidation;
- preserve the structured Aegis manager unchanged.

This slice proves the central experience—Aegis as the security entry point followed by a real Hermes session—without first building a terminal multiplexer or weakening the manager boundary.

## Launch-asset impact review

This report records proposed architecture only and changes no implementation, command syntax, configuration schema, runtime behavior, security claim, dependency, build, demonstration, release artifact, or supported-version range. Therefore no launch asset should be edited to describe native operational handoff as implemented.

Reviewed impact categories:

- root `README.md`: unaffected; must continue to describe only the current manager, design foreground, and operational launch behavior;
- `LICENSE`: unaffected;
- `SECURITY.md`: unaffected; no enforcement property changed;
- `CONTRIBUTING.md`: unaffected; existing disposable-state and PTY testing rules remain applicable;
- `CODE_OF_CONDUCT.md`: unaffected;
- `CHANGELOG.md`: unaffected because this is research, not shipped behavior;
- threat model: unaffected for current behavior; this report contains the future PTY/direct-attachment threat analysis;
- architecture diagram: unaffected until a native operational path is implemented;
- five-minute quickstart: unaffected and must not claim this future flow;
- no-key demonstration: unaffected;
- terminal recording: unaffected and must not imply a native operational handoff exists;
- release binaries and checksums: unaffected; no release was created or authorized;
- focused contributor issues: unaffected; no remote issue was created or authorized.

When implementation is authorized, the architecture diagram, threat model, README/runtime behavior, quickstart or focused native-session guide, terminal recording, changelog, tests, and contributor backlog must be reviewed together. Any recording must use fake credentials and must not imply that a fixture proves real provider or broker behavior.

## Final recommendation

Aegis should support a deliberate native Hermes operational-session handoff. It is both technically feasible and aligned with the product identity: Aegis is the authenticated security entry point, while Hermes remains the visible runtime.

The architecture should not be described as Aegis “wrapping terminal bytes for security.” Security comes from the controls established before and outside the TUI: authenticated stanza selection, immutable mandate, deterministic projection, minimal environment, exact tool and broker registration, credential non-delivery, out-of-band revocation, process supervision, and authoritative audit.

Start with supervised direct terminal attachment and no mid-session Aegis modal. Keep the built-in manager on the structured gateway. Introduce a PTY relay only after a concrete approval or protected-input requirement proves that terminal ownership must move back and forth during one Hermes process, and only after PTY, lifecycle, canary, and real-Hermes compatibility tests demonstrate that the handoff is safe and truthful.
