# Terminal Experience Operational Implementation

## 1. Status and purpose

**Status:** Normative implementation specification

**Target:** The principal-facing Aegis bootstrap and built-in manager terminal experience

**Parent specifications:**

- `specs/AEGIS_MANAGER.md`
- `specs/BASE_MANAGER_END_TO_END.md`
- `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`
- `specs/RUNTIME_AND_SESSIONS.md`

**Supporting research:**

- `research/2026-07-19-terminal-experience-best-of-hermes-openclaw-claude-code.md`

This specification converts the terminal-experience research into one operational implementation target suitable for a long-running `/loop` session. It is intentionally implementation-heavy. It does not authorize a mockup, a screenshot-only redesign, a disconnected component package, a theme demo, or a fixture that bypasses the real manager lifecycle.

The completed implementation MUST make this statement true:

> Bare interactive `aegis` owns one responsive, terminal-safe conversation interface that visibly binds the authenticated principal, exactly one trust stanza, one mandate, Hermes Agent, one exact local inference route, and one clean runtime session; real startup, turns, guard denials, protected intake, deterministic approvals, expiry, failures, and bounded cleanup all update that interface through typed authoritative state; accessible/plain and noninteractive contracts remain operational; and no untrusted runtime or model output can forge Aegis security chrome.

## 2. Authority and conflict resolution

The implementation MUST follow this authority order:

1. `AGENTS.md`
2. `specs/MVP.md`
3. `specs/AEGIS_MANAGER.md`
4. `specs/BASE_MANAGER_END_TO_END.md`
5. `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`
6. `specs/RUNTIME_AND_SESSIONS.md`
7. this specification
8. other focused specifications
9. retained research
10. implementation convenience

If this specification conflicts with a higher-authority security, lifecycle, identity, approval, audit, or runtime requirement, the stronger requirement wins. The implementation MUST resolve the conflict explicitly and preserve fail-closed behavior.

This specification does not authorize:

- modification of a normal Hermes profile;
- model download, copying, replacement, or implicit selection;
- global Ollama changes;
- provisioning or activation of a user-created logical agent;
- arbitrary shell/file/network/MCP/plugin capability in the built-in manager;
- credential creation outside protected intake and deterministic authorization;
- commits, tags, pushes, releases, remote issues, or external publication;
- fabricated recordings, screenshots, runtime output, certifications, checksums, or test evidence.

## 3. Operational completion rule

### 3.1 Operational means connected

A feature is operational only when it is connected to the real command path and exercised through that path.

For this specification, operational implementation requires all of the following:

```text
real bare-command/root dispatch
  -> real TTY and terminal-capability detection
  -> real authenticated principal preflight
  -> real manager lifecycle/orchestrator
  -> real Aegis-owned terminal application
  -> real guarded composer input
  -> real Hermes gateway turn or truthful degraded state
  -> real typed manager response/proposal path
  -> real Aegis approval/protected-intake path
  -> real application operation and authoritative audit
  -> real expiry/failure/exit handling
  -> real bounded cleanup and terminal restoration
```

No link may be represented only by:

- a static renderer;
- a sample model;
- a Storybook-like preview;
- a golden string with no command integration;
- an unused interface or event type;
- a fake event replay presented as a successful runtime;
- a spinner around the old blocking path;
- a direct Hermes TTY attachment;
- a second manager implementation that bypasses existing guards or lifecycle ownership.

### 3.2 Evidence outweighs appearance

Visual polish is required, but operational evidence has higher priority than screenshots or animation. A less elaborate component that correctly handles cancellation, authority, resize, sanitization, and cleanup is complete before a more elaborate component that does not.

The implementation MUST NOT be declared complete because:

- the welcome screen looks polished;
- unit snapshots pass while PTY behavior fails;
- fake events render correctly while real lifecycle events are not wired;
- the accessible/plain path is missing;
- terminal state is not restored after interruption;
- noninteractive JSON changes accidentally;
- real untrusted text bypasses sanitization;
- approval still occurs in an interleaved line prompt that the model can visually spoof;
- startup or cleanup can hang without visible bounded progress;
- any launch asset describes unimplemented behavior.

### 3.3 No hidden fallback around requirements

If the rich renderer cannot initialize safely, Aegis MAY enter an explicitly selected, tested accessible/plain interactive renderer. It MUST NOT silently:

- attach the terminal to Hermes;
- disable ingress scanning;
- skip the typed event boundary;
- downgrade exact approval;
- union or change authority;
- start cloud inference;
- bypass model certification;
- abandon cleanup ownership;
- claim the rich renderer is active.

If neither interactive renderer is safe, startup MUST fail before Hermes/model activation with an actionable stable reason.

## 4. Product outcome

The terminal should feel fast, coherent, and alive while making security context continuously understandable.

The primary repeated loop MUST be:

1. compose naturally;
2. submit through Aegis custody;
3. see that the turn is guarded and routed locally;
4. observe bounded live activity and elapsed time;
5. interrupt without corrupting session or terminal state;
6. distinguish user, Hermes/model, and authoritative Aegis output;
7. inspect details only when needed;
8. approve consequential operations through a separate deterministic boundary;
9. return immediately to the composer after completion;
10. exit through one bounded cleanup path.

Aegis MUST retain its own product identity. It should adopt proven interaction patterns from Hermes Agent, OpenClaw, and Claude Code, not mimic their brand, wording, art, or authority model.

## 5. Scope

### 5.1 Required surfaces

This implementation covers:

- first-run/bootstrap rendering and prompts;
- manager preflight and startup progress;
- active manager conversation;
- truthful degraded manager mode;
- persistent trust context;
- multiline composition;
- slash-command discovery and help;
- assistant streaming/display;
- guard denials;
- typed proposal display;
- authoritative approval;
- protected no-echo intake handoff;
- explicit authenticated exact-reference value retrieval;
- operation progress and result display;
- status inspection;
- expiry, revocation, runtime failure, signal, EOF, and exit;
- cleanup progress and final result;
- accessible/plain interactive mode;
- unchanged noninteractive/machine behavior.

### 5.2 Explicitly excluded from this slice

The implementation MUST NOT add merely to make the TUI look capable:

- arbitrary shell mode or a `!` command;
- arbitrary file mentions or `@` expansion;
- generic model tools;
- in-session stanza switching;
- model/provider/runtime switching;
- arbitrary user-scripted authoritative status lines;
- untrusted user themes that can collapse origin distinctions;
- model-initiated, ambiguous, fuzzy, or unauthenticated secret reveal;
- session resume that violates clean-session or mandate requirements;
- remote manager operation;
- browser or desktop UI;
- host sandbox claims.

### 5.3 Existing command compatibility

Existing Cobra subcommands and noninteractive output contracts MUST remain compatible. Interactive TUI work MUST NOT reformat JSON command output, add ANSI to redirected output, prompt in non-TTY mode, consume piped content as chat, or change stable exit-code rendering unintentionally.

## 6. Required architecture

### 6.1 One terminal owner

During interactive bootstrap or manager operation, exactly one Aegis presentation controller MUST own terminal rendering and ordinary input dispatch.

Background goroutines MUST NOT write directly to terminal stdout while that controller is active. Runtime, proxy, Ollama, lifecycle, guard, audit, and operation updates MUST enter the presentation controller as typed events. Diagnostics that must remain on stderr MUST be coordinated so they do not corrupt the active display; they SHOULD be represented in the terminal application and retained in metadata-safe logs where appropriate.

Protected intake MAY temporarily take exclusive terminal input ownership, but the handoff MUST be explicit, serialized, context-bound, and restorable.

### 6.2 Typed event boundary

The implementation MUST define a closed internal presentation event model. At minimum it MUST represent:

```text
bootstrap inspection started/completed
bootstrap stage started/completed/failed
principal authenticated/denied
trust context selected/denied
mandate issued/expiring/expired/revoked
runtime discovery started/completed/failed
model verification/load/unload started/completed/failed
proxy route opened/closed/denied
Hermes process/session started/stopped/failed
manager ready/degraded
input accepted/blocked/discarded
turn queued/started/progress/completed/failed/interrupted
assistant delta/completed/rejected
proposal received/validated/denied
approval requested/confirmed/cancelled/expired
protected intake requested/started/completed/cancelled/failed
operation started/completed/failed
cleanup requested/stage/completed/failed
terminal warning/resize/capability change
```

Each event MUST have a typed origin and trust classification. At minimum:

- `aegis-authoritative`;
- `aegis-diagnostic`;
- `runtime-hermes`;
- `model-untrusted`;
- `user-input`.

Model text MUST never be accepted as an authoritative event merely because it contains JSON or resembles Aegis wording. Existing strict manager envelopes and proposal validators remain the protocol boundary.

### 6.3 Pure state transition core

The terminal application SHOULD use a pure update/state/view architecture:

- services perform real work;
- adapters translate real outcomes to typed events;
- update logic changes presentation state;
- view logic renders that state;
- view logic performs no authorization or application mutation.

Tests MUST be able to drive the state model without a terminal, but this does not replace command-path and PTY integration tests.

### 6.4 No duplicate security state

The TUI MUST render principal, stanza, mandate, runtime, route, model, certification, and lifecycle state from the same typed authoritative values used by application/runtime decisions. It MUST NOT parse these values back from log lines or maintain an independently editable security copy.

If rendered state and authoritative state cannot be reconciled, the UI MUST show an explicit inconsistent/failed state and stop consequential operations. It MUST not guess.

## 7. Terminal capability model

### 7.1 Detection

Aegis MUST detect and retain a typed terminal capability snapshot, including where reliably available:

- stdin/stdout TTY state;
- terminal width and height;
- color level or no-color state;
- `NO_COLOR`;
- `TERM=dumb`;
- Unicode suitability or explicit ASCII fallback;
- extended-key support where observable;
- bracketed-paste availability;
- alternate-screen support;
- mouse support only if used;
- reduced-motion/accessibility selection;
- operating system and terminal limitations relevant to restoration.

Environment hints are advisory and MUST be bounded and sanitized. A repository or model response MUST NOT be allowed to inject terminal keybindings or presentation code.

### 7.2 Renderer profiles

Aegis MUST support these profiles:

1. **Rich interactive:** semantic color, Unicode, component updates, multiline composer, markdown, overlays/cards, bounded animation.
2. **Accessible/plain interactive:** no cursor-dependent animation, no color-only meaning, stable line-oriented announcements, complete keyboard operation, screen-reader-suitable updates.
3. **Machine/noninteractive:** existing structured output; no terminal application, prompts, cursor controls, animation, or model conversation.

The active profile MUST be inspectable. A profile change that requires terminal reinitialization MUST occur at a safe boundary and preserve session authority and transcript truthfully.

### 7.3 Terminal restoration

The presentation controller MUST capture and restore every terminal mode it changes. Restoration MUST occur after:

- normal exit;
- `/quit`, `/exit`, plain exact `quit`, and plain exact `exit`;
- Ctrl-C/Esc interruption as applicable;
- Ctrl-D/EOF;
- SIGINT/SIGTERM;
- session expiry or revocation;
- Hermes/proxy/Ollama failure;
- startup rollback;
- protected-intake success/failure/cancellation;
- renderer error or panic boundary;
- cleanup timeout;
- second-signal forced termination where process semantics permit a final restoration attempt.

Restoration MUST be PTY-tested, not inferred from defer statements.

## 8. Central terminal-safety boundary

### 8.1 Mandatory before rich model rendering

The existing `streamSafeText` behavior is not sufficient because chunking runes does not neutralize terminal control sequences. A centralized sanitizer MUST be implemented and tested before model/runtime text is passed to markdown or component renderers.

### 8.2 Required protections

The sanitizer MUST, by rendering context:

- strip or neutralize CSI, OSC, DCS, APC, PM, SOS, and related escape sequences;
- remove unsafe C0 and C1 controls;
- prevent carriage-return rewriting, cursor movement, title changes, clipboard writes, terminal queries, and fake progress sequences;
- reject or neutralize bidi overrides and dangerous invisible formatting in security-sensitive fields;
- apply explicit safe isolation for legitimate RTL prose where supported;
- preserve ordinary newlines and tabs only in contexts that permit them;
- handle malformed UTF-8 deterministically;
- bound total bytes, runes/graphemes, line count, line width, nesting, table rows, code blocks, and link count;
- avoid corrupting copy-sensitive paths, URLs, IDs, hashes, and commands;
- omit binary-like content with an explicit placeholder;
- sanitize before width measurement and layout;
- sanitize errors, runtime status, model output, user-visible metadata, and any external process text independently.

### 8.3 Links

Raw OSC 8 from untrusted content MUST never pass through. If clickable links are supported, Aegis MUST reconstruct them from a validated visible URL and ensure the displayed destination cannot disguise a different target. Security-sensitive approval fields SHOULD remain non-clickable.

### 8.4 Origin cannot be forged

Assistant markdown, borders, headings, code blocks, ANSI-like text, Unicode box drawing, and pasted text MUST remain inside a model/user component. They MUST not overwrite or visually merge with the trust bar, approval card, guard result, or Aegis lifecycle status.

The sanitizer and layout MUST be adversarially tested with output designed to imitate Aegis chrome.

## 9. Persistent trust context

### 9.1 Required fields

During an active or degraded manager session, the interface MUST continuously make these facts available without relying solely on `/status`:

- Aegis identity;
- authenticated principal identity;
- selected trust stanza/security context;
- authority/mandate state and remaining lifetime or expiry;
- explicit Hermes Agent runtime identity and state;
- local/degraded route state;
- no-fallback state where space permits or through a fixed status indicator.

The exact model identity/digest, policy digest, certification, route digest, and full expiry timestamp MAY move to a status overlay at constrained widths, but the compact surface MUST not create ambiguity.

### 9.2 Authoritative source

Every trust field MUST be supplied by Aegis application/runtime state. The model MUST not choose labels, colors, icons, ordering, or values for this surface.

### 9.3 Responsive behavior

At widths below 50 columns, security fields MUST stack or abbreviate using unambiguous labels. Principal, stanza, runtime, and authority status MUST not disappear. Truncated identifiers MUST include a safe detail path, and two different values MUST not render as the same ambiguous truncation where a decision is pending.

### 9.4 No color-only semantics

Authenticated, untrusted, warning, expired, revoked, degraded, and failed states MUST be represented in text and/or stable symbols in addition to color.

## 10. Startup and bootstrap operation

### 10.1 Real progress, not optimistic progress

Startup status MUST be driven by completion of actual preflight/orchestrator stages. A spinner or check mark MUST not advance before the underlying authoritative operation returns successfully.

At minimum, startup MUST visibly distinguish:

- principal authentication;
- credential authority validation/unlock;
- Hermes discovery/version verification;
- Ollama route verification;
- exact model artifact verification;
- certification validation;
- model load;
- Aegis proxy readiness;
- disposable Hermes process readiness;
- Hermes gateway session creation;
- authoritative session/audit completion;
- ready or degraded outcome.

### 10.2 Stage identity

Stage IDs and outcomes MUST be stable typed values. Human wording can evolve, but tests and audit correlation MUST not depend on parsing prose.

### 10.3 Cancellation and queueing

The existing ability to accept bounded startup input/queue behavior MUST be preserved only if it remains safe under the new composer. Input submitted before readiness MUST be visibly marked as queued, bounded, guardable, cancellable, and delivered at most once after readiness. It MUST not be submitted to Hermes before the route and session are ready.

### 10.4 Bootstrap mutations

Bootstrap previews, custody selection, passphrase intake, model route configuration, certification, and launch decisions remain deterministic Aegis operations. TUI forms MUST call the existing services and revalidation paths. They MUST NOT replace exact previews or authenticated confirmation with optimistic UI state.

## 11. Composer requirements

### 11.1 Input behavior

The rich composer MUST support:

- multiline editing;
- Enter to submit;
- `Ctrl+J` as a universal newline fallback;
- Shift+Enter or terminal-specific newline only when distinguishable safely;
- cursor movement across wrapped and logical lines;
- bounded undo/edit operations if the selected component supports them safely;
- Up/Down history navigation only at the first/last visual line as appropriate;
- `Ctrl+R` reverse history search;
- command autocomplete with descriptions;
- bracketed paste;
- submit-burst coalescing;
- explicit large-paste preview/placeholder behavior;
- deterministic byte/rune limits before guard submission;
- no hidden background submission.

### 11.2 Shortcuts

At minimum:

- `?` on empty input opens keyboard/help information;
- `Ctrl+L` redraws without clearing authority, transcript, or Hermes state;
- Esc or Ctrl-C interrupts active work according to the manager lifecycle contract;
- idle Ctrl-C clears nonempty input before requesting session exit, or uses another documented unambiguous policy;
- Ctrl-D on empty input requires a deliberate exit behavior and converges on bounded cleanup;
- slash exits remain locally consumed before guard/model submission.

Shortcut behavior MUST be documented in-product and PTY-tested. Unsupported terminal combinations MUST have visible fallbacks.

### 11.3 Capability honesty

Autocomplete and help MUST be generated from current implemented and available operations. The composer MUST NOT advertise:

- shell mode;
- arbitrary file mention;
- unavailable credential operations;
- model switching;
- stanza switching;
- runtime/profile/plugin/MCP management;
- any operation denied by current readiness or authority.

### 11.4 Input custody

Ordinary input MUST enter Aegis first. It MUST be bounded, source-classified, and guarded before Hermes. The TUI component MUST not write directly to Hermes stdin or the inference proxy.

## 12. Conversation timeline

### 12.1 Typed components

The timeline MUST distinguish:

- user message;
- assistant/model message;
- Aegis authoritative notice;
- Aegis diagnostic;
- runtime/Hermes activity;
- guard denial;
- proposal;
- approval result;
- operation result;
- cleanup/session result.

Each component MUST carry origin in state even when the visual profile suppresses color or decoration.

### 12.2 Assistant rendering

Assistant text MUST:

- remain labeled as Hermes/model-origin and untrusted;
- be sanitized before markdown parsing/rendering;
- stream through bounded coalesced updates;
- never directly authorize or claim an authoritative operation result;
- be rejected entirely if the manager response envelope/guard requires complete buffering and validation before release;
- follow the existing protocol requirement even when token-level streaming would look better.

Where the security protocol requires buffering the complete response, the UI MAY show activity but MUST NOT leak unvalidated partial assistant content.

The manager's message-only response path MAY incrementally release the `message` JSON string only after a bounded parser has matched the exact canonical schema/version, `kind:"message"`, and message-field prefix. It MUST retain a tail sufficient to avoid splitting JSON escapes, UTF-8, or surrogate pairs; sanitize each complete accumulated snapshot; require monotonic rendered snapshots; and validate the complete response normally before accepting the turn. Proposal and non-canonical envelopes MUST remain fully buffered. A streamed message whose completed envelope fails validation MUST be visibly rejected and MUST never produce an authoritative event or operation.

### 12.3 Bounded retention

In-memory presentation state MUST be bounded by component count and byte size. Pruning a visual component MUST also remove side indexes, pending-stream references, and retained untrusted payloads. Authoritative audit remains separate and metadata-only.

### 12.4 Native scrollback and transcript inspection

The initial operational renderer SHOULD preserve native terminal scrollback. An alternate-screen/fullscreen transcript MAY be added only with:

- reliable restoration;
- resize tests;
- accessible/plain alternative;
- an explicit method to export or write sanitized transcript content where policy permits;
- no loss of authoritative session receipts.

Fullscreen appearance MUST not delay the operational inline/component path.

## 13. Activity and progress

### 13.1 Live activity

Long-running work MUST never appear silently stuck. After a short bounded threshold, the interface MUST show:

- current typed stage/activity;
- elapsed time;
- runtime/route state where useful;
- how to interrupt or inspect when applicable.

A periodic heartbeat MUST update in place or through an accessible announcement without flooding scrollback.

### 13.2 Truthful completion

Activity indicators MUST stop on completion, failure, cancellation, expiry, or cleanup transition. A successful visual state MUST correspond to a successful authoritative outcome. Runtime narration cannot complete an Aegis progress stage.

### 13.3 Motion policy

Motion MUST be bounded and disabled for:

- `TERM=dumb`;
- `NO_COLOR` where animation relies on cursor/color behavior;
- accessible/plain mode;
- reduced-motion selection;
- noninteractive output;
- approval and protected-input typing;
- cleanup failure reporting.

Whimsical waiting phrases MAY appear only during ordinary non-security-critical waiting. They MUST NOT describe authentication, authorization, revocation, guard denial, credential intake, or cleanup failure.

## 14. Authoritative approval

### 14.1 Separate focused state

Consequential approval MUST enter a dedicated Aegis-owned state/modal. The composer and model turn submission MUST be suspended while approval is active.

The approval surface MUST visibly include:

- authoritative Aegis label;
- exact operation;
- exact target and bounded metadata;
- authenticated actor;
- trust stanza/security context;
- persistence/security consequence;
- authority expiry;
- allowed choices;
- safe default;
- exact required approval phrase.

### 14.2 Data provenance

Approval fields MUST be built from validated deterministic proposal/application data. Raw model prose MUST not occupy authoritative field positions. Optional model explanation must remain in a separately labeled untrusted section and SHOULD be hidden by default during the final decision.

### 14.3 Fail-closed interaction

Approval MUST cancel on:

- Esc/cancel choice;
- EOF;
- terminal loss;
- session expiry/revocation;
- route/runtime failure;
- proposal or state drift;
- resize/render failure that prevents exact scope from remaining visible;
- cleanup request;
- invalid phrase;
- context cancellation.

The implementation MUST revalidate authoritative state immediately before mutation as required by parent specifications.

### 14.4 Security-sensitive text

Operation, target, actor, stanza, and phrase rendering MUST neutralize bidi/invisible controls, bound widths, and provide a safe full-detail view. The user MUST not approve a visually truncated ambiguous target.

## 15. Protected intake

### 15.1 Explicit ownership handoff

Protected intake MUST be a distinct Aegis-owned terminal state. Ordinary composer and runtime event rendering MUST not consume secret keystrokes or print over the no-echo prompt.

### 15.2 No transcript path

Secret bytes MUST not enter:

- TUI model/state intended for transcript rendering;
- presentation events containing content;
- Hermes gateway;
- proxy/Ollama requests;
- logs, diagnostics, audit, receipts, panic text, metrics, or snapshots.

The TUI MAY retain only metadata-safe intake status such as started/completed/cancelled and operation ID.

### 15.3 Restoration and cancellation

All protected-intake guarantees in the lifecycle specification remain mandatory. The TUI integration MUST prove terminal echo restoration and reader ownership after success, mismatch, cancellation, expiry, signal, and failure.

### 15.4 Explicit authenticated value retrieval

An unambiguous exact-reference value request in the authenticated built-in manager MAY render the decrypted value as an Aegis-authoritative operation result. It MUST bypass Hermes/model processing, reject missing or revoked references, escape terminal controls, emit metadata-only audit, and remain only in session-scoped presentation state that is purged on close. The UI MUST warn that terminal scrollback and external recording are outside Aegis cleanup. This exception does not weaken the protected-intake no-transcript rule and does not authorize model-initiated, fuzzy, or arbitrary reveal.

## 16. Status, help, and details

### 16.1 Status view

The status surface MUST organize authoritative data into:

- identity and authentication provenance;
- trust stanza/security context;
- mandate/session ID and expiry/revocation;
- policy revision/digest;
- credential authority state;
- Hermes executable/version/process/session/disposable-home state;
- Ollama mode/version/local endpoint class;
- exact model identity/digest/context;
- certification identity/state;
- route/proxy state without capability material;
- no-cloud/no-fallback/no-switch policy;
- isolation limitations;
- audit verification/last safe status where available.

It MUST never display secrets, bearer capabilities, reusable tokens, raw prompts, or raw model responses.

### 16.2 Help view

Help MUST be state-aware, searchable or filterable in rich mode where practical, and fully available in plain mode. It MUST show keyboard fallbacks and distinguish local deterministic commands from conversational requests.

### 16.3 Progressive disclosure

Startup should remain compact, but every omitted exact digest/path/revision required for review MUST be reachable through status/details without restarting the session or asking the model.

## 17. Degraded mode

The same terminal application SHOULD present degraded mode so that interaction, exit, status, and deterministic remediation stay coherent.

Degraded mode MUST:

- identify the exact stable reason;
- state that no cloud fallback or alternate model was attempted;
- expose only actually usable deterministic operations;
- retain authenticated principal and trust context truthfully;
- avoid fake assistant responses;
- avoid active-model spinners;
- provide exact next steps;
- permit every local exit path and bounded cleanup;
- remain useful in accessible/plain mode.

## 18. Lifecycle and cleanup integration

### 18.1 One lifecycle

The TUI MUST integrate with the existing explicit manager lifecycle. It MUST NOT introduce a second lifecycle that can remain active after the manager begins closing.

Once closing begins:

- composer submission is disabled;
- pending input is discarded without Hermes submission;
- approvals and intake cancel;
- new operations are denied;
- activity changes to cleanup;
- cleanup stages update from real teardown outcomes;
- terminal restoration occurs exactly once through coordinated ownership.

### 18.2 Cleanup visibility

Cleanup SHOULD show real bounded stages without exposing sensitive process text:

```text
stopping Hermes
closing gateway
invalidating inference capability
closing proxy
unloading and verifying exact model removal (external-local mode)
stopping dedicated managed Ollama instead (managed mode)
removing disposable state
finalizing receipt
restoring terminal
```

A check mark MUST appear only after the corresponding operation succeeds. Timeout/failure MUST identify the stable metadata-safe failed stage and produce nonzero/defined exit semantics; arbitrary backend error text MUST NOT be copied into the terminal result.

### 18.3 Signals

First SIGINT/SIGTERM, second-signal escalation, EOF, expiry, revocation, and runtime failures MUST retain the behavior required by `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`. TUI key handling MUST not swallow or reinterpret those events indefinitely.

## 19. Go implementation constraints

### 19.1 Dependency decision

The implementation session MUST evaluate current compatible releases of:

- Bubble Tea v2;
- Lip Gloss v2;
- Bubbles v2;
- Glamour v2;
- Huh v2 only if needed for deterministic forms.

It MUST inspect licenses, Go-version compatibility, transitive dependencies, terminal behavior, maintenance status, and security advisories before changing `go.mod`. Dependencies MUST be pinned by normal Go module resolution and verified by the repository vulnerability workflow.

The research-observed versions are not automatic authorization to install them. Current versions and APIs MUST be checked at implementation time.

### 19.2 Package boundary

A likely structure is:

```text
internal/tui/
  app.go
  capabilities.go
  event.go
  model.go
  update.go
  view.go
  theme.go
  sanitize.go
  composer.go
  transcript.go
  approval.go
  status.go
  accessibility.go
```

The implementation MAY choose a different structure after tracing existing code, but MUST preserve:

- typed events;
- pure presentation state where practical;
- no authority decisions in view code;
- no package-level mutable Cobra/TUI commands;
- injected IO, clock/ticker, terminal capabilities, and services for tests;
- context cancellation;
- centralized terminal restoration;
- separation of interactive rendering from noninteractive command output.

### 19.3 Existing code integration

The implementation MUST trace and integrate, not bypass, at least:

- `internal/command/manager.go`;
- `internal/command/bootstrap.go`;
- `internal/command/terminal_input.go` and platform-specific terminal input;
- `internal/command/manager_runtime.go`;
- `internal/manager` lifecycle, session, guard, gateway, proxy, Hermes, and Ollama components;
- credential authority/protected intake;
- root command TTY dispatch;
- centralized error/exit handling;
- manager audit/receipt services.

Existing in-progress unrelated working-tree changes MUST be preserved.

## 20. Testing requirements

### 20.1 Unit tests

Required unit coverage includes:

- every presentation event and origin classification;
- valid/invalid lifecycle transitions;
- trust-bar field source and responsive reduction;
- no color-only semantic distinctions;
- terminal capability profile selection;
- command availability by readiness/authority state;
- composer limits, multiline behavior, history, and completion logic;
- approval state and fail-closed cancellation;
- accessible/plain rendering;
- bounded transcript/component retention;
- sanitizer corpus and property/fuzz tests;
- width/grapheme calculations after sanitization;
- progress completion only from matching authoritative events.

### 20.2 Golden rendering tests

Golden outputs MUST cover widths at least:

```text
40
49
50
79
80
89
90
120
200
```

Cover:

- dark/light/ANSI/no-color;
- Unicode and ASCII fallback;
- startup, active, degraded, approval, intake metadata state, runtime failure, expiry, and cleanup;
- long principal/stanza/model/path/digest values;
- RTL/CJK/emoji/combining text;
- forged-border/model-output attempts;
- narrow approval targets with no ambiguous authorization.

Golden tests are necessary but not sufficient.

### 20.3 PTY/subprocess tests

Required operational PTY tests include:

- real root dispatch into the new renderer;
- Enter submit and `Ctrl+J` multiline;
- supported Shift+Enter behavior and fallback messaging;
- Up/Down history and Ctrl-R;
- slash completion/help;
- bracketed paste and submit bursts;
- large-paste handling;
- resize during startup, stream/activity, approval, protected intake, and cleanup;
- Ctrl-C/Esc interruption while idle and active;
- Ctrl-D/EOF;
- SIGINT, SIGTERM, and second-signal behavior;
- terminal loss;
- runtime/proxy/Ollama child failure;
- expiry/revocation;
- renderer error/panic boundary;
- terminal echo/raw/cursor mode restoration;
- no prompt corruption from concurrent events;
- exactly one cleanup receipt.

Linux coverage is mandatory in the local implementation environment. Other supported platforms MUST be tested or fail closed/document exact blockers according to existing cross-platform requirements.

### 20.4 Fake operational stack

Default integration tests MUST use fake local Hermes/Ollama/process fixtures, but they MUST exercise the same production orchestrator, proxy, gateway client, event adapters, TUI controller, guards, proposal validation, operations, and cleanup path. A fixture that injects final view state directly is not end-to-end evidence.

### 20.5 Random-canary invariant

Generate fresh credential-shaped canaries and prove that ordinary blocked paste and protected intake maintain all parent-specification non-disclosure requirements. Add TUI-specific absence assertions for:

- event queues;
- UI model/transcript state;
- render snapshots;
- terminal captures;
- status/help/approval components;
- panic/recovery diagnostics;
- completion/history stores;
- paste placeholders;
- accessibility announcements.

Tests MUST never use real credentials.

### 20.6 Adversarial terminal corpus

At minimum test:

- ANSI SGR and cursor controls;
- OSC title, hyperlink, notification, and clipboard sequences;
- DCS/APC/PM/SOS;
- carriage return and backspace rewriting;
- C0/C1 controls;
- bidi overrides/isolates and zero-width characters;
- malformed UTF-8;
- huge unbroken strings;
- deeply nested/oversized markdown;
- giant tables/code blocks;
- Unicode box drawing that imitates Aegis approval chrome;
- model text containing fake principal/stanza/runtime/approval lines;
- terminal query/response confusion;
- pasted escape sequences;
- runtime stderr containing escape sequences.

## 21. Performance and reliability requirements

### 21.1 Responsiveness

- Input editing MUST remain responsive while activity and assistant events arrive.
- Token/delta updates MUST be coalesced; the complete transcript MUST not rerender for every byte/token.
- Event queues MUST be bounded and apply backpressure or safe coalescing.
- Critical lifecycle/approval/guard events MUST not be dropped behind cosmetic deltas.
- Long output MUST use bounded previews and explicit expansion.
- History and transcript retention MUST be bounded.

### 21.2 No silent hangs

Every bounded startup, turn, approval, intake, operation, and cleanup stage MUST have a deadline/cancellation path. Long-running active stages MUST display elapsed time/heartbeat. Timeout MUST become an exact safe failure, not an indefinitely spinning interface.

### 21.3 Race safety

Race tests MUST cover event delivery, interruption, resize, stream completion, runtime failure, approval cancellation, and cleanup convergence. View state MUST not be mutated concurrently outside the TUI update owner.

## 22. Accessibility requirements

The implementation is incomplete without an operational accessible/plain mode.

It MUST:

- work without color, Unicode box drawing, background fills, mouse, or animation;
- announce origin in text;
- announce permission/approval state changes;
- keep prompts and status updates in stable order;
- avoid repeatedly rewriting the same line for screen readers;
- expose all actions by keyboard;
- preserve terminal bell/notification choices where supported and safe;
- document universal multiline and help shortcuts;
- remain usable at 40 columns;
- receive PTY/snapshot coverage independent of rich mode.

## 23. Documentation and launch assets

Implementation changes will affect terminal output and therefore MUST trigger the full launch-asset review required by `AGENTS.md`.

At minimum expect updates to:

- root `README.md`;
- `CHANGELOG.md`;
- `SECURITY.md` terminal-output boundary;
- `docs/THREAT_MODEL.md`;
- `docs/ARCHITECTURE.md` and architecture diagram;
- `docs/QUICKSTART.md`;
- no-key demonstration;
- `docs/RECORDING.md`, recording script, and any retained cast;
- contributor issue backlog;
- release notes/artifact workflow material where output assumptions change.

Every documented interactive command that can run safely locally MUST be executed. Recordings must be regenerated from real behavior and inspected for secrets, personal paths, hostnames, and fabricated success. Remote publication still requires explicit owner authorization.

`LICENSE`, `CONTRIBUTING.md`, and `CODE_OF_CONDUCT.md` MUST still be reviewed. Dependency license and contributor instructions MUST be updated if affected.

## 24. Implementation work packages

Work MUST proceed in dependency order. Each gate requires real tests and command-path evidence before the next package is considered complete.

### T0 — Baseline, preservation, and operational map

Deliverables:

- read all authority/parent documents completely;
- inspect working tree, diffs, tests, and in-progress changes;
- trace root dispatch, bootstrap, terminal input, manager loop, lifecycle, runtime startup, protected intake, approvals, audit, and cleanup;
- reproduce current terminal behavior in isolated PTY tests;
- record exact existing noninteractive output contracts;
- identify all direct interactive stdout/stderr writes and terminal mode owners;
- record current Hermes/Ollama prerequisites without modifying them.

Gate:

- no implementation edit until the end-to-end ownership map is understood;
- baseline PTY captures/tests demonstrate current behavior and terminal restoration;
- unrelated changes are explicitly preserved.

### T1 — Terminal safety and capability foundation

Deliverables:

- centralized contextual sanitizer;
- ANSI/OSC/control/bidi adversarial corpus;
- terminal capability snapshot and renderer-profile selection;
- semantic theme with rich/plain/ASCII/no-color behavior;
- replace unsafe direct rendering of untrusted model/runtime text;
- remove ad hoc raw terminal controls from manager paths where centralized primitives are required.

Gate:

- real manager/model fixture output cannot alter cursor, title, clipboard, trust chrome, or approval rendering;
- sanitizer fuzz/property tests and `go test -race` for affected packages pass;
- noninteractive output remains byte/semantic compatible as required.

### T2 — Typed presentation event bridge

Deliverables:

- closed typed event/origin model;
- adapters from real bootstrap/manager/lifecycle/runtime outcomes;
- bounded event queue/coalescing policy;
- pure state update model;
- no background terminal writes while interactive controller owns display;
- authoritative security fields sourced from application state.

Gate:

- fake operational stack drives real startup, active, degraded, failure, and cleanup state through production adapters;
- no test injects final view state as substitute evidence;
- critical events cannot be dropped or reordered behind assistant deltas.

### T3 — Operational application shell and trust surface

Deliverables:

- rich and accessible/plain interactive controllers;
- compact startup card/progress;
- persistent trust context;
- responsive layout at required widths;
- status/help views;
- centralized redraw/resize/restoration;
- root/bootstrap/manager integration.

Gate:

- bare `aegis` enters the new real interface in an isolated PTY;
- principal, stanza, Hermes, route, authority, and expiry are truthful through startup/degraded/active/closing states;
- terminal restoration passes after normal and failed startup.

### T4 — Composer and local command operation

Deliverables:

- multiline editor;
- universal newline fallback;
- history and reverse search;
- slash autocomplete/help;
- bounded paste and burst handling;
- interruption/redraw/exit shortcuts;
- state-aware command availability;
- startup queue integration if retained.

Gate:

- PTY tests exercise every required key/input path through the real command;
- user input reaches guard/Hermes at most once and only after readiness;
- unavailable capabilities never appear in help/completion.

### T5 — Conversation, activity, and protocol integration

Deliverables:

- typed user/model/Aegis/runtime/guard/proposal/result components;
- bounded assistant display consistent with complete-response validation;
- live activity and elapsed-time heartbeat;
- bounded transcript and expansion/detail behavior;
- interruption and turn failure integration;
- accessible announcements.

Gate:

- a fake Hermes/Ollama operational turn runs through gateway, proxy, guard, strict response decoder, event bridge, and TUI;
- malformed/secret-bearing/forged output is denied or safely rendered according to protocol;
- runtime failure and interruption converge on real cleanup.

### T6 — Authoritative approval and protected intake integration

Deliverables:

- focused approval state/card;
- exact phrase entry and safe default;
- validated field provenance;
- revalidation before mutation;
- serialized protected-intake handoff;
- cancellation/expiry/resize/terminal-loss handling;
- metadata-only presentation events.

Gate:

- create/rotate/revoke paths run through real application services with fake model proposal and fresh random canary;
- secret plaintext is absent from all TUI/event/transcript/capture surfaces;
- terminal mode is restored and no mutation occurs on every cancellation/drift case.

### T7 — Lifecycle, cleanup, and failure campaigns

Deliverables:

- closing-state composer lockout;
- real cleanup stage events;
- first/second signal behavior;
- EOF/expiry/revocation/runtime/proxy/Ollama/renderer failure handling;
- bounded idempotent cleanup presentation;
- exactly one receipt/final outcome;
- no silent spinner after deadline.

Gate:

- subprocess/PTY matrix proves children/listeners/capabilities/disposable state are handled and terminal restored;
- cleanup failures remain visible, safe, bounded, and auditable;
- race tests pass.

### T8 — Performance, accessibility, and cross-terminal hardening

Deliverables:

- coalesced streaming benchmarks/tests;
- bounded memory/event/transcript behavior;
- narrow/wide resize campaign;
- accessible/plain operational campaign;
- tmux/screen and representative terminal checks where locally available;
- cross-platform implementation or exact fail-closed blockers.

Gate:

- input remains responsive during bounded event load;
- no critical event loss;
- 40-column no-color accessible path can complete status, help, approval cancellation, and exit;
- unsupported platform behavior fails before unsafe runtime activation.

### T9 — Full verification and launch assets

Deliverables:

- focused and complete tests;
- race, vet, vulnerability, build, formatting, release-regression, and diff checks;
- safe documented workflow execution;
- launch-asset updates and review;
- real no-key demonstration regeneration;
- exact report of any opt-in live runtime blocker;
- repository-local focused issue updates for remaining external/cross-platform work.

Gate:

- every locally actionable Definition of Done item has real output/evidence;
- every blocked item names the exact missing external prerequisite;
- no documentation claims behavior beyond what was run;
- no release, commit, push, remote issue, model download, or external mutation occurs without explicit authorization.

## 25. Verification commands

The implementation session MUST run, as applicable:

```text
gofmt check for cmd/internal
focused package tests during each work package
PTY/subprocess terminal tests
go build ./cmd/aegis
go test ./...
go test -race ./...
go vet ./...
configured govulncheck
shell/release regression tests
safe no-key demonstration/workflows
git diff --check
```

`make verify` MAY be used only after inspecting its effects and preserving the current working tree. Its `go mod tidy` requirement means dependency changes must be intentional and reviewed.

Default verification MUST NOT access cloud inference, download models, read real credentials, alter real Hermes profiles, mutate normal Ollama state, replace installed binaries, publish releases, or change remote repository state.

A real Hermes/model result may be claimed only if actually run against explicitly authorized, already-installed prerequisites. Otherwise report the exact blocker.

## 26. Definition of Done

The feature is complete only when every locally actionable statement below is true.

### 26.1 Operational command path

- Bare interactive `aegis` starts the new Aegis-owned terminal controller.
- Bootstrap, active manager, degraded manager, and cleanup use one coherent presentation architecture.
- Real application/runtime outcomes drive typed UI state.
- No disconnected demo or old hidden manager loop remains as the claimed primary path.
- Bare non-TTY and existing machine contracts remain compatible and noninteractive.

### 26.2 Trust and authority

- Principal, stanza, mandate state/expiry, Hermes runtime, route, and authority state remain continuously inspectable.
- Every field comes from authoritative typed state.
- The model cannot forge, select, broaden, or restyle authoritative security context.
- Exactly one stanza is represented per session; no in-session switching or authority union exists.
- Approval remains exact, deterministic, revalidated, and fail-closed.

### 26.3 Terminal safety

- Untrusted model/runtime/user metadata passes through contextual sanitization before rendering.
- ANSI/OSC/control/bidi attacks cannot alter terminal state or forge Aegis chrome.
- Sanitization occurs before layout measurement.
- Rich markdown cannot bypass the safety boundary.
- Terminal state restores after every tested exit/failure path.

### 26.4 Composer and conversation

- Multiline input, history, reverse search, completion, paste handling, redraw, interrupt, and exit work through real PTY tests.
- Ordinary input reaches Aegis guard first and Hermes at most once.
- Help/completion advertise only implemented and available capabilities.
- Assistant content remains visibly untrusted and consistent with strict complete-response validation.
- Activity displays truthful elapsed progress without silent hangs or scrollback flooding.

### 26.5 Protected secrets

- Protected intake has exclusive, cancellable terminal ownership.
- Secret bytes never enter presentation events, UI state, transcript, history, captures, Hermes/Ollama/model paths, logs, audit, receipts, or errors.
- Random-canary tests cover TUI-specific surfaces.
- Terminal echo and input ownership restore after every intake outcome.

### 26.6 Lifecycle and reliability

- First signal, second signal, EOF, local exits, expiry, revocation, runtime failures, renderer failures, and cleanup timeout converge on defined behavior.
- No input or operation begins after closing.
- Cleanup stages reflect real outcomes and remain bounded/idempotent/race-safe.
- Exactly one receipt/final result is produced.
- Event queues, transcript, history, and rendering work are bounded.
- Input remains responsive under streaming/activity load.

### 26.7 Accessibility and responsiveness

- Rich and accessible/plain interactive modes are operational.
- No semantic state depends only on color, animation, Unicode, mouse, or background fill.
- Required flows remain usable at 40 columns.
- Resize does not hide or ambiguously truncate approval/security scope.
- Accessible/plain mode has independent tests and documentation.

### 26.8 Evidence

- Unit, golden, PTY, subprocess, integration, adversarial, random-canary, race, build, vet, vulnerability, and diff checks have real results.
- Fake services exercise production orchestration rather than directly injecting final UI state.
- Every safe locally runnable documented workflow is exercised.
- Any unavailable live Hermes/model/cross-platform evidence is identified by exact prerequisite, not replaced by a fixture claim.
- Launch assets describe only implemented and tested behavior.

## 27. `/loop` execution contract

A `/loop` implementation session using this specification MUST:

1. read this entire file before editing;
2. read `AGENTS.md`, `specs/MVP.md`, every parent specification listed in section 1, and the supporting terminal research;
3. inspect `git status`, current diffs, recent history, dependencies, implementation, tests, and launch assets before editing;
4. preserve all unrelated and in-progress user work;
5. trace symbols and usages before creating interfaces or dependencies;
6. begin with a failing baseline/regression or operational characterization test where behavior changes;
7. work through T0–T9 in dependency order and satisfy each gate with real evidence;
8. prioritize terminal safety, authority provenance, cancellation, and cleanup over decorative polish;
9. connect each component to the production command/orchestrator path before considering it delivered;
10. add tests with each behavior change rather than postponing verification;
11. keep default tests hermetic and use fake local Hermes/Ollama processes through production integration seams;
12. never treat a snapshot, mock event replay, unused package, spinner, TODO, or sample screen as operational completion;
13. preserve noninteractive output and stdout/stderr contracts;
14. preserve Aegis custody of input and never attach the principal TTY directly to Hermes;
15. preserve exact single-stanza, no-fallback, no-model-switching, protected-intake, and deterministic-approval invariants;
16. run focused tests continuously and full verification before completion;
17. fix root causes rather than suppressing symptoms or weakening tests;
18. continue until every locally actionable Definition of Done item has real evidence;
19. report external prerequisites and unsupported platform/runtime contracts exactly;
20. perform and report the complete launch-asset review;
21. not commit, tag, push, publish, release, create remote issues, download models, alter real Hermes/Ollama/profile state, or modify external systems without explicit operator authorization.

If a library, terminal primitive, operating system, or supported Hermes/Ollama contract blocks a requirement, the session MUST:

1. produce a minimal reproducible finding using isolated state;
2. preserve the stronger security/lifecycle boundary;
3. finish every unaffected work package;
4. document the exact blocker and required upstream/external change;
5. fail closed rather than substituting direct TTY pass-through, unbounded reads, cloud fallback, prompt-only security, fabricated output, or an untested fallback renderer.

The loop MUST NOT stop at analysis or a plan. Its deliverable is a working, exercised terminal implementation with real command output and tests, or an honest exact blocker after every unaffected requirement has been implemented and verified.
