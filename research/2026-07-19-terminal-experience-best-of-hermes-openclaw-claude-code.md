# Aegis Terminal Experience: Best of Hermes Agent, OpenClaw, and Claude Code

**Research date:** 2026-07-19 UTC  
**Status:** Product and engineering recommendation; no runtime or profile provisioning authorized or performed

## Executive decision

Aegis should have a first-class full-screen terminal UI, but it should not be a generic clone of any agent terminal.

The recommended combination is:

- **Claude Code's interaction discipline:** excellent composer behavior, keyboard discoverability, interruption, progressive disclosure, narrow-layout handling, clear long-running activity, and accessibility fallbacks.
- **OpenClaw's TUI architecture:** explicit UI state, bounded chat components, structured tool/activity cards, overlays, responsive light/dark palettes, terminal-safe rendering, and strong PTY-focused tests.
- **Hermes Agent's personality and feedback:** visible runtime identity, streaming responses, compact tool summaries, rich status, memorable but optional motion, and data-driven presentation primitives.
- **Aegis's own security semantics:** identity, trust stanza, mandate expiry, runtime, route, model, and approval boundary must be persistent first-class interface state—not buried in `/status`, represented only by color, or delegated to model-generated text.

The target is a terminal that feels fast and alive while making authority unusually legible:

> Beautiful conversation, unmistakable authority, deterministic approvals.

## Sources inspected

Repository source was inspected at these revisions:

- OpenClaw: `openclaw/openclaw` at `d5cb708623fa2cd1baf424d169cd27bab11c6d3d`
- Hermes Agent: `NousResearch/hermes-agent` at `e598cef87465981fcea1c0339edfcf5d9716c917`
- Claude Code public repository: `anthropics/claude-code` at `015170d3fd84fb57ef4685a64b673fadd0690dc1`

Primary online sources:

- https://github.com/openclaw/openclaw
- https://docs.openclaw.ai
- https://github.com/NousResearch/hermes-agent
- https://hermes-agent.nousresearch.com/docs
- https://github.com/anthropics/claude-code
- https://code.claude.com/docs/en/overview
- https://code.claude.com/docs/en/interactive-mode
- https://code.claude.com/docs/en/terminal-config
- https://code.claude.com/docs/en/statusline
- https://code.claude.com/docs/en/permissions

The Claude Code implementation is not published in full in its public repository, so conclusions about it are based on official documentation, its public changelog, and observable documented behavior—not invented source internals.

## What each product gets right

### Claude Code

Claude Code's strongest contribution is not decoration. It is interaction polish accumulated across many terminal edge cases:

- A real multiline composer with universal fallbacks (`Ctrl+J` and backslash plus Enter), not only terminal-specific Shift+Enter behavior.
- Prompt history and reverse search.
- Slash-command discovery, contextual autocomplete, and an empty-input `?` help surface.
- Clear interruption semantics: stop work without destroying completed work, clear input separately, and require a deliberate exit gesture.
- External-editor handoff for large prompts.
- Progressive disclosure: compact tool summaries by default, detailed transcript on demand.
- Visible elapsed time and periodic heartbeats for long operations so the interface never appears frozen.
- Explicit permission modes and permission dialogs.
- Fullscreen and plain/screen-reader rendering modes.
- Careful narrow-terminal behavior for banners, dialogs, and diffs.
- Notifications when work finishes or input is needed.
- Theme adaptation and status information that remain useful during long sessions.

The lesson for Aegis: optimize the repeated loop—compose, submit, observe, interrupt, inspect, approve—not just the welcome banner.

### OpenClaw

OpenClaw provides the clearest inspectable TUI architecture of the three:

- A stateful component TUI rather than interleaved `fmt.Fprintln` output.
- Dedicated chat-log components for user, assistant, system, and tool events.
- Bounded retained components and explicit cleanup of side indexes.
- Live tool cards with pending/success/error states, curated arguments, partial output, and a 12-line collapsed preview.
- Overlay-based help, status, session, and selection surfaces with explicit focus restoration.
- A custom editor with layered shortcuts, autocomplete-aware submit behavior, AltGr handling, and terminal-specific key handling.
- Animated waiting state that combines activity text, elapsed time, and connection status.
- Light/dark palette selection with contrast-aware decisions.
- Output sanitization that strips ANSI/control characters, handles binary-like text, preserves copy-sensitive paths and URLs, and considers RTL isolation.
- A reusable terminal-core package for themes, safe text, tables, prompts, progress lines, restore behavior, and OSC progress.
- PTY and terminal-loss tests, not only string snapshots.

The lesson for Aegis: rendering should consume typed state and typed events. Security decisions must not be inferred from already-rendered text.

### Hermes Agent

Hermes contributes the strongest sense that the runtime is active and present:

- A fixed input area, multiline editing, slash autocomplete, history, interrupt-and-redirect, and streaming output.
- Compact, recognizable tool activity with per-tool labels and previews.
- Rich status and context feedback.
- Presentation primitives separated from agent logic in several areas.
- `NO_COLOR`, `TERM=dumb`, and TTY-aware color behavior.
- Data-driven skins with semantic tokens for accent, success, warning, error, input, response, status, and selection.
- Distinct waiting/thinking feedback and configurable branding.
- Diff presentation and bounded previews.

The lesson for Aegis: polish benefits from personality, but Aegis should use motion and character sparingly around security-sensitive interactions. Trust semantics must remain stable across themes.

## Current Aegis terminal assessment

The present manager is truthful and security-explicit, but visually it is a linear debug transcript:

- Startup prints many `[startup]` lines and then a large metadata block.
- The composer is a single `"[composer] > "` line with no multiline editing, history search, or interactive completion.
- Activity is printed as a static route sentence on every turn.
- Assistant content is prefixed correctly as untrusted, but `streamSafeText` only chunks runes; it does not currently make terminal escape sequences safe.
- `/status` contains important state, but the principal, stanza, runtime, route, and expiry are not continuously visible.
- The authoritative approval prompt is semantically strong but visually dense.
- Bootstrap and manager interaction styles are not yet one coherent design system.
- `/clear` emits raw ANSI directly when `TERM` is not dumb instead of using a centralized terminal capability/rendering layer.

The most urgent issue is not aesthetics: untrusted model output needs a real terminal-safety boundary before rich rendering. A function named `streamSafeText` should strip or neutralize ANSI/OSC/C0/C1 controls and dangerous bidirectional formatting before any renderer sees the content.

## Proposed Aegis experience

### 1. Persistent trust bar

Reserve one stable row for authoritative session context. It should be rendered by Aegis only and never be populated from model prose.

Suggested compact content:

```text
AEGIS  principal:javi  stanza:secrets-manager  runtime:Hermes  route:local  expires:42m
```

Rules:

- Always name Hermes; never imply a hidden Aegis model runtime.
- Show stanza as a security context, not a persona.
- Show degraded, expiring, revoked, or disconnected states in words and symbols, never color alone.
- Collapse low-priority fields by width, but never hide principal, stanza, runtime, or authority state.
- At narrow widths, use a two-row layout rather than truncating security identifiers ambiguously.
- The trust bar is not user-scriptable. Claude Code's command-backed custom status line is useful for coding context but is inappropriate for Aegis's authoritative trust surface.

### 2. Compact session header

Use a restrained startup card instead of a wall of text:

```text
╭─ Aegis Manager ─────────────────────────────────────────────╮
│ ✓ Principal authenticated       javi / local OS identity    │
│ ✓ Security context              secrets-manager             │
│ ✓ Runtime                       Hermes Agent 0.18.x          │
│ ✓ Inference route                local Ollama / pinned digest │
│ ! Isolation                      runtime state, not sandbox   │
╰──────────────────────────────────────────────────────────────╯
```

Then move immediately to the composer. Full digests, paths, policy revision, and route identity remain available in `/status` and a status overlay.

### 3. Real composer

Adopt the best repeated-input behavior:

- Multiline editing.
- Enter submits; `Ctrl+J` always inserts newline; support Shift+Enter where distinguishable.
- Up/Down history navigation that respects multiline cursor position.
- `Ctrl+R` reverse history search.
- Slash-command autocomplete with descriptions.
- Empty-input `?` opens keyboard help.
- `Ctrl+L` redraws without changing session state.
- `Esc` or `Ctrl+C` interrupts an active model turn; when idle, it clears input before any exit behavior.
- `Ctrl+D` on empty input uses a deliberate double-press or confirmation before exit.
- Bracketed paste and burst coalescing.
- Large paste shown as a bounded placeholder with an inspect action before submission.
- No `!` shell mode in the built-in manager. The unavailable capability should not be teased by the UI.
- No arbitrary `@` file mention unless a future stanza explicitly grants a typed, guarded file capability.

### 4. Typed conversation timeline

Render typed events rather than log prefixes:

- User messages: visually grouped, neutral background.
- Assistant messages: markdown, streamed in place, permanently marked `Hermes / untrusted model` in the component metadata.
- Aegis system notices: separate authoritative style and origin label.
- Guard blocks: concise warning card stating that content did not reach Hermes and was not retained.
- Runtime activity: one live component updated in place, not repeated output lines.
- Repeated identical system events: coalesce with a count.
- Keep native scrollback usable in the default mode; provide an optional fullscreen transcript mode later.

Do not allow markdown, ANSI, OSC links, or model text to forge Aegis chrome. Model markdown links should be visibly marked and sanitized before optional OSC 8 rendering.

### 5. Activity and progress

Use an updating single-line activity area:

```text
◐ Hermes reasoning · 8.4s · local route verified
```

For startup:

```text
✓ principal   ✓ authority   ✓ Hermes   ◐ model load   · session
```

Requirements:

- Always include elapsed time after a short threshold.
- Emit a periodic heartbeat for long calls.
- Replace animation with static text under `NO_COLOR`, `TERM=dumb`, reduced-motion mode, non-TTY output, or accessibility mode.
- Never use whimsical phrases for authentication, approval, revocation, guard blocks, or cleanup failures.
- A small amount of Hermes-like personality is acceptable only during ordinary waiting.
- Use OSC 9/4 progress only as optional enhancement; visible text remains authoritative.

### 6. Security-native approval dialog

Keep Aegis's exact-phrase authorization, but structure it as a focused modal/card:

```text
╭─ AUTHORITATIVE AEGIS APPROVAL ───────────────────────────────╮
│ Operation     rotate credential                              │
│ Target        github/team-bot                                │
│ Actor         principal:javi                                 │
│ Context       secrets-manager                                │
│ Persists      encrypted credential authority                 │
│ Expires       2026-07-19T05:20:00Z                            │
│                                                              │
│ Safe default: CANCEL                                         │
│ Type: approve 7e4a2c1d9b81f0aa                               │
╰──────────────────────────────────────────────────────────────╯
```

Requirements:

- Modal input goes to Aegis, never Hermes.
- Exact operation and target remain visible while typing.
- The safe default is explicit and selected by default.
- Approval cannot be represented by color or a single ambiguous `y`.
- Untrusted model prose cannot appear inside authoritative fields.
- Very long or bidi-containing values are sanitized, bounded, and inspectable without permitting visual reordering.
- Terminal loss, resize, cancellation, EOF, and signal handling restore terminal state and fail closed.

### 7. Status and help overlays

`/status` should open a scan-friendly panel with sections:

- Identity: principal and authentication provenance.
- Authority: stanza, policy revision/digest, mandate ID, expiry/revocation.
- Runtime: Hermes executable/version/session state/disposable-home status.
- Inference: Ollama ownership, exact model identity/digest, certification.
- Route: local endpoint class and capability state without token material.
- Isolation limits: explicit non-sandbox statement.
- Audit: last authoritative event and verification status.

`?` or `/help` should show only actions that are actually available in the current state. Degraded mode must not advertise unavailable credential mutations.

### 8. Responsive rendering modes

Provide three renderer profiles:

1. **Rich interactive:** component TUI, semantic color, motion, markdown, overlays.
2. **Accessible/plain interactive:** stable text, no animation, no background color dependence, screen-reader-friendly updates.
3. **Machine/non-interactive:** preserve existing JSON and deterministic Cobra output exactly; never start the TUI.

Terminal width tiers:

- `< 50`: stacked metadata, no side-by-side fields, bounded identifiers.
- `50–89`: compact single-column cards.
- `>= 90`: two-column status cards where safe.

Every essential state must remain legible at 40 columns and with color disabled.

## Visual language

Aegis should launch with one carefully controlled semantic theme, plus light, dark, ANSI, and no-color variants—not arbitrary security-chrome skins in the first release.

Suggested semantic tokens:

- `brand`: Aegis identity only.
- `authoritative`: Aegis-generated control-plane content.
- `runtime`: Hermes-origin/runtime activity.
- `untrusted`: model-origin content metadata.
- `success`, `warning`, `error`, `muted`.
- `approval`: consequential decision boundary.
- `principal`, `stanza`, `expiry`.
- `border`, `inputBorder`, `selection`.

Color is additive. Every semantic distinction also needs text, a symbol, placement, or border style.

Avoid:

- Giant ASCII art on every launch.
- Emoji in security outcomes.
- Rainbow logs.
- Spinners that overwrite prompts or approvals.
- User themes that can make `untrusted model` look identical to `authoritative Aegis`.
- A customizable shell-backed status line in the built-in secrets manager.
- Fullscreen alternate-screen mode as the only mode; native scrollback matters.

## Go implementation recommendation

Use a dedicated presentation layer with typed events and no authority decisions inside view code.

Recommended current Go stack to evaluate and pin:

- Bubble Tea v2 (`github.com/charmbracelet/bubbletea/v2`) for update/view lifecycle and terminal ownership.
- Lip Gloss v2 (`github.com/charmbracelet/lipgloss/v2`) for responsive semantic styling.
- Bubbles v2 (`github.com/charmbracelet/bubbles/v2`) for textarea, viewport, spinner, help, and list primitives.
- Glamour v2 (`github.com/charmbracelet/glamour/v2`) only behind Aegis sanitization for terminal markdown.
- Huh v2 may be useful for deterministic bootstrap forms, but avoid mixing two interaction models unless it shares the same theme, terminal lifecycle, and cancellation behavior.

Versions observed during research were Bubble Tea `v2.0.8`, Lip Gloss `v2.0.5`, Bubbles `v2.1.1`, Huh `v2.0.3`, and Glamour `v2.0.1`. Versions must be rechecked and deliberately pinned at implementation time.

Proposed package boundaries:

```text
internal/tui/
  app.go              terminal lifecycle and root model
  event.go            typed presentation events
  model.go            pure UI state
  update.go           state transitions
  view.go             responsive composition
  theme.go            semantic tokens and capabilities
  sanitize.go         untrusted terminal-text boundary
  composer.go         input/history/completion behavior
  transcript.go       user/model/system/activity components
  approval.go         authoritative approval modal
  status.go           trust bar and status overlay
  accessibility.go    plain/reduced-motion behavior
```

Application and manager services should emit typed events such as:

- `PrincipalAuthenticated`
- `StanzaSelected`
- `RuntimeDiscovered`
- `RuntimeStarted`
- `RouteVerified`
- `TurnStarted`
- `AssistantDelta`
- `GuardBlocked`
- `ApprovalRequested`
- `ApprovalResolved`
- `SessionExpiring`
- `CleanupStarted`
- `CleanupCompleted`

The view may render these events. It may not reinterpret them into authorization.

## Terminal-safety requirements

Before rich TUI work, implement and test one centralized sanitizer for all untrusted terminal text:

- Strip CSI, OSC, DCS, APC, PM, and other escape/control sequences.
- Remove unsafe C0/C1 controls while preserving permitted newline and tab behavior by context.
- Prevent carriage-return rewriting and cursor movement.
- Neutralize bidi overrides and isolates in security-sensitive fields; use explicit safe isolation for legitimate RTL prose.
- Bound line length, grapheme count, total rendered bytes, markdown nesting, table rows, and code-block size.
- Preserve copy-sensitive paths, URLs, IDs, and digests without inserting invisible or visible corruption.
- Treat model-provided OSC 8 links as untrusted; reconstruct links only from validated URLs.
- Never render model ANSI directly, even if it appears to contain attractive color.
- Sanitize before measurement and layout so hidden control bytes cannot affect width calculations.

This is a security boundary, not a cosmetic helper.

## Verification plan

### Unit and golden tests

- Pure update-state tests for every lifecycle and approval transition.
- Golden views at widths 40, 50, 80, 120, and 200.
- Light, dark, ANSI-16, no-color, and `TERM=dumb` snapshots.
- Sanitizer corpus covering ANSI/OSC/DCS, carriage returns, C0/C1 controls, bidi controls, malformed UTF-8, giant tokens, binary-like text, RTL, CJK, emoji, and combining graphemes.
- Approval fields cannot be populated from assistant events.
- Degraded mode exposes only available commands.
- Trust bar never loses principal, stanza, runtime, or authority status at supported widths.

### PTY tests

- Multiline input and history.
- Bracketed paste and submit bursts.
- Resize during stream, approval, and startup.
- Ctrl+C, Esc, Ctrl+D, EOF, SIGINT, and SIGTERM semantics.
- Terminal restoration after normal exit, panic, expiry, runtime crash, and cleanup timeout.
- No prompt corruption while progress updates arrive.
- tmux/screen behavior where available.
- Linux terminals first, followed by macOS and Windows campaigns required by the existing backlog.

### Security tests

- Model output cannot forge the trust bar or approval border.
- Secret-like blocked input never appears in transcript state, render snapshots, logs, or queued events.
- Protected no-echo intake remains outside the TUI transcript and Hermes stream.
- Bidi and zero-width content cannot reorder operation, target, actor, or approval phrase.
- Terminal hyperlinks cannot execute commands and cannot disguise a different destination.
- Approval safely cancels on terminal loss or focus/lifecycle failure.

### Performance targets

- Keystrokes remain responsive while assistant and task events stream.
- Render updates are coalesced; do not redraw the whole transcript per token.
- Transcript and event retention are bounded.
- Long tool/model output uses collapsed previews and explicit expansion.
- No silent period longer than a few seconds during active long-running work; heartbeat text updates without flooding scrollback.

## Delivery sequence

### Phase 0: safety foundation

1. Replace the current misleading `streamSafeText` behavior with tested terminal sanitization.
2. Introduce terminal capability detection and semantic no-color styling.
3. Preserve existing non-interactive output contracts.

### Phase 1: high-impact polish without full alternate-screen dependence

1. Compact startup card.
2. Persistent trust/status row.
3. Updating startup and turn activity with elapsed time.
4. Structured authoritative approval card.
5. Better help and status views.

### Phase 2: composer and component transcript

1. Bubble Tea application shell.
2. Multiline composer, history, completion, paste handling, interruption.
3. Typed user/model/system/activity components.
4. Markdown rendering behind sanitizer.
5. Responsive layout and accessible/plain mode.

### Phase 3: advanced workflow

1. Overlays and transcript inspection.
2. Notifications and terminal progress integration.
3. Optional fullscreen mode while retaining a native-scrollback mode.
4. Session resume only when Aegis mandate and clean-session invariants permit it.

## Features to take, adapt, or reject

| Source | Take | Adapt for Aegis | Reject for built-in manager |
|---|---|---|---|
| Claude Code | Composer, history, help, interrupt, elapsed time, accessibility | Permission UI becomes exact Aegis authorization | Shell mode, command-backed authoritative status, casual permission-mode cycling |
| OpenClaw | Typed components, overlays, bounded transcript, safe rendering, PTY tests | Tool cards become Aegis operation/activity cards | Gateway/session switching that could imply in-session stanza switching |
| Hermes | Runtime visibility, streaming, concise activity, tasteful personality | Semantic theme locked around trust origins | Arbitrary skins that blur security provenance; direct Hermes TTY ownership |

## Launch-asset impact review

This research document changes no command syntax or implemented behavior, so no launch asset was edited.

For the eventual implementation, the following will be affected and must be updated and exercised together:

- Root `README.md`: screenshots/terminal examples and interactive behavior.
- `docs/QUICKSTART.md`: first-run and manager interaction.
- `docs/RECORDING.md` and recording source: all terminal output changes.
- `docs/THREAT_MODEL.md` and `SECURITY.md`: terminal-control injection and authoritative/untrusted rendering boundaries.
- Architecture diagram: Aegis-owned typed TUI and sanitizer boundary.
- No-key demonstration and short recording: regenerate and verify; never edit a cast to imply success.
- `CHANGELOG.md`: user-visible TUI behavior.
- Release binaries/checksums: rebuild only for an authorized release and verify checksums.
- Contributor issue backlog: split sanitizer, TUI shell, composer, approval modal, accessibility, and PTY campaigns into focused issues; external GitHub issue creation still requires owner authorization.

`LICENSE`, `CONTRIBUTING.md`, and `CODE_OF_CONDUCT.md` were reviewed as conceptually unaffected by this research-only addition. Dependency licenses must be re-reviewed when the Go TUI dependencies are actually added.

## Bottom line

Yes: the terminal should be exceptionally good. But Aegis wins by making security context feel native rather than bureaucratic.

The signature experience should be:

- Claude Code-level input quality.
- OpenClaw-level component structure and output safety.
- Hermes-level runtime presence and energy.
- Aegis-only persistent, non-forgeable trust context and deterministic approval UX.

That combination is differentiated, implementable in Go, and aligned with the project's central promise: one authenticated identity, one trust stanza, one clean runtime session.
