# Go desktop, system-tray, and streaming-UI framework research

- Status: Exploratory technology report; not an implementation specification
- Date: 2026-07-17
- Prepared for: Aegis
- Scope: Cross-platform desktop shells, system-tray/menu-bar integration, Go interoperability, streamed and server-driven interfaces, project support, packaging, and security boundaries
- Current authority: `AGENTS.md` and `specs/` remain normative for implemented Aegis behavior
- Primary decision: Preserve the Go control plane as an independently authoritative service. For a production desktop client today, prefer Tauri v2 with a constrained web frontend and the Go service as a sidecar or separately supervised process. Use Electron instead when ecosystem maturity and debugging are more important than footprint. Re-evaluate Wails v3 after it reaches stable status. Use Fyne only for a deliberately small, operational, all-Go interface.

## Executive summary

Go can support a cross-platform desktop application with a system-tray icon, but the best architecture depends on whether “Go application” means the entire graphical process must be written in Go or whether Go remains the authoritative backend behind a thin desktop shell.

The central finding is that the desktop shell, UI toolkit, streaming transport, and trusted application service are separate choices:

```text
OS integration                 interface rendering
tray / menu / windows          React / Svelte / native widgets
        |                               |
        +------ desktop shell ----------+
                        |
             constrained local protocol
                        |
                authoritative Go service
```

For interfaces expected to change rapidly, display streamed model output, render audit timelines, show structured approvals, or adopt server-driven components, a web frontend has the strongest component, accessibility, testing, virtualization, Markdown, and incremental-rendering ecosystem. Go should remain responsible for domain rules, identity, authorization, validation, approvals, provisioning, credential mediation, and authoritative audit.

The practical shortlist is:

1. **Tauri v2 + Svelte or React + Go sidecar/service** — best current balance of lightweight packaging, mature tray support, frontend flexibility, streaming primitives, and capability scoping.
2. **Electron + Svelte or React + Go child/service** — lowest UI ecosystem risk and strongest debugging/packaging maturity, with a materially larger resource footprint and a larger renderer/runtime attack surface.
3. **Wails v3 + Svelte or React** — best conceptual Go-first fit and a strong tray API, but upstream still labels v3 alpha. It should not be selected under a stable-only support policy yet.
4. **Fyne + Go** — viable for a compact status/settings client, but weaker for a rich, evolving, content-heavy or generative interface.

For Aegis, the desktop UI must be an untrusted presentation client. A tray click, window state, renderer message, prompt, or displayed principal name is never authentication or authorization. Consequential operations must pass through authenticated, typed Go application services and preserve the existing exact-approval and audit invariants.

## 1. Research questions

This investigation addresses:

1. Can a cross-platform tray or menu-bar application be implemented around Go?
2. Which frameworks provide first-class Windows, macOS, and Linux tray support?
3. Which choices are stable, active, documented, and supported by substantial communities?
4. Which frameworks are suitable for token streams, logs, progress, audit events, and rapidly changing state?
5. Which frameworks can support constrained server-driven or generative UI?
6. What packaging, signing, accessibility, testing, update, and platform costs remain?
7. Which architecture preserves Aegis's control-plane and security boundaries?

## 2. Research method and evidence limits

Primary sources inspected include current official framework documentation, repositories, release metadata, examples, and issue trackers for Wails, Tauri, Electron, Fyne, Gio, Flutter, Qt, and lightweight Go webview/tray libraries.

Live repository metadata was inspected on 2026-07-17. At that time:

- Wails had approximately 35,000 GitHub stars and more than 100 contributors represented on the first contributors page.
- Fyne had approximately 28,000 stars and more than 100 contributors represented on the first page.
- Tauri had approximately 109,000 stars and more than 100 contributors represented on the first page.
- Electron had approximately 122,000 stars and more than 100 contributors represented on the first page.
- Gio's GitHub mirror had approximately 2,200 stars and 81 contributors returned by the repository API.

Stars, contributor counts, issue counts, and recent commits are adoption and activity signals, not proof of correctness, security, accessibility, or long-term maintenance. They should not replace an Aegis-owned prototype and release matrix.

The version observations in this report are time-bound:

- Wails v2.13.0 was the latest stable Wails release inspected.
- Wails v3.0.0-alpha2.117 was the latest v3 prerelease inspected.
- Fyne v2.8.0 was the latest stable release inspected.
- Tauri v2.11.5 was the latest stable release inspected.
- Electron v43.1.1 was the latest stable release inspected.

No complete sample application was built in this research task, so this report does not claim measured startup time, memory use, binary size, event throughput, accessibility conformance, or packaging success. Those are prototype acceptance gates rather than facts inferred from upstream marketing.

## 3. Terminology

### 3.1 Desktop shell

The component that owns:

- application lifecycle;
- windows and webviews;
- system tray or macOS menu-bar items;
- native menus and dialogs;
- notifications;
- global shortcuts;
- application packaging and platform metadata;
- signing, notarization, and update integration.

### 3.2 UI toolkit

The rendering and component layer, such as React, Svelte, Vue, Fyne widgets, Flutter widgets, Qt Widgets, or Qt Quick.

A shell may allow several UI toolkits. Wails, Tauri, and Electron can host React, Svelte, Vue, or plain HTML. Selecting Wails or Tauri does not require selecting React.

### 3.3 Streaming data into a UI

Incremental delivery of known data types such as:

- model tokens or text deltas;
- operation progress;
- logs and audit events;
- status and health changes;
- tool-call lifecycle events;
- session updates;
- binary or structured telemetry.

### 3.4 Server-driven or generative UI

A backend sends a constrained description of interface state or operations, and the client maps that description to a fixed set of reviewed components. This is distinct from downloading and executing arbitrary source code.

### 3.5 Pixel streaming

Remote delivery of rendered frames through technologies such as WebRTC, VNC, or RDP. None of the evaluated desktop frameworks provides this as its ordinary UI model. Pixel streaming is a different architecture and is out of scope.

## 4. Decision criteria

A production choice should be evaluated against:

1. **Tray support:** menu-only mode, hidden windows, dynamic menus/icons, and native behavior.
2. **Platform coverage:** supported Windows, macOS, Linux distributions, display servers, and desktop environments.
3. **Streaming:** ordered delivery, backpressure, cancellation, bounded buffers, and efficient frontend updates.
4. **Frontend freedom:** component libraries, accessibility, Markdown, tables, charts, virtualization, and testing.
5. **Go integration:** in-process bindings, sidecar lifecycle, IPC ergonomics, type generation, and error propagation.
6. **Security:** renderer isolation, capability restrictions, CSP, local transport authentication, and update integrity.
7. **Operational maturity:** stable releases, documentation, issue response, packaging, CI, signing, and updates.
8. **Footprint:** installer size, memory, idle CPU, startup latency, and dependence on system webviews.
9. **Accessibility:** keyboard behavior, screen readers, focus, semantic controls, reduced motion, and contrast.
10. **Maintainability:** debugging tools, contributor familiarity, test automation, and upgrade burden.

No framework should be chosen solely from repository popularity or a “single binary” claim.

## 5. Comparative assessment

| Option | Go role | Tray support | Streamed UI | UI ecosystem | Stability | Overall fit |
|---|---|---|---|---|---|---|
| Tauri v2 | Sidecar or separate Go service; Rust shell | First-class | Strong events and channels; sidecar can use framed stdio/SSE/WebSocket | Full web ecosystem | Stable | Preferred lightweight production candidate |
| Electron | Child or separate Go service; JS/Node shell | First-class and mature | Excellent IPC, MessagePort, streams, SSE, and WebSocket options | Largest web desktop ecosystem | Stable | Preferred maturity-first candidate |
| Wails v3 | In-process Go backend | First-class and extensive | Go-to-frontend events; local SSE/WebSocket also possible | Full web ecosystem | Alpha | Best future Go-first candidate; prototype only today |
| Wails v2 | In-process Go backend | Not first-class in stable core | Runtime events | Full web ecosystem | Stable | Windowed apps are viable; tray requirement is a blocker |
| Fyne | Entire app in Go | Built in | Suitable for bounded state, logs, and progress | Go-native widget ecosystem | Stable | Good for deliberately small operational UI |
| Gio | Entire app in Go | Not a central first-class feature | Strong custom/immediate-mode rendering | Small, custom-oriented ecosystem | Active/niche | Specialist custom-rendering option |
| Flutter | Dart UI; Go through process or FFI bridge | Plugin-based | Strong reactive/stream UI | Large Flutter ecosystem | Stable | Good UI platform but weak Go-specific fit |
| Qt | C++/QML core; Go through third-party bindings or process boundary | Mature native API | Strong signal/slot and model/view facilities | Very mature native ecosystem | Stable upstream | Qt is mature; Go bindings are the risk |
| Raw webview + tray libraries | Go process with low-level native wrappers | Library-dependent | Application must design transport | Full web frontend possible | Fragmented | Too much integration ownership for Aegis |

## 6. Wails

### 6.1 Architecture

Wails packages a Go backend with a web frontend and uses native platform rendering engines rather than bundling a complete Chromium distribution. It provides generated JavaScript and TypeScript bindings for Go services, native dialogs and menus, and a unified event mechanism.

This is the most natural architecture when the product requirement is “Go owns the application and a web frontend renders it.”

### 6.2 Version split and tray support

The upstream repository explicitly listed two active versions at the time of inspection:

- v2: stable;
- v3: alpha.

Wails v3 contains a comprehensive cross-platform system-tray implementation and examples. The API supports:

- tray-only and tray-plus-window applications;
- hidden and frameless windows;
- attaching a window to a tray item;
- icon, dark-mode icon, template icon, label, and tooltip behavior;
- dynamic menus and menu rebuilding;
- left, right, and double click handling;
- mouse enter and leave where the platform provides it;
- macOS accessory activation policy;
- Windows taskbar hiding.

Wails documentation warns that Linux tray support depends on the desktop environment and that some events vary by platform. Its documentation also requires explicit menu updates after dynamic menu state changes.

Stable Wails v2 does not provide the equivalent first-class tray API in released core. A v2 system-tray pull request remained open during inspection. Combining v2 with a separate tray library would create two lifecycle systems and additional integration risk.

### 6.3 Streaming behavior

Wails supports custom Go-to-frontend events and window-specific events. This is appropriate for text deltas, progress, state changes, and audit notifications.

Generated Wails bindings do not directly expose Go channels; upstream binding documentation lists `chan` as unsupported. A streaming operation should therefore use one of:

- a service method that starts an operation and returns an operation ID, followed by Wails events;
- a local bounded SSE or WebSocket stream;
- a framed application protocol over a separately owned local connection.

The UI should batch high-frequency token events rather than trigger a complete render for every token.

### 6.4 Strengths

- Go-first development and packaging.
- Full web component ecosystem.
- Native system webview rather than bundled Chromium.
- Generated TypeScript models and service bindings.
- Strong v3 tray/window design.
- Smaller conceptual process topology than a sidecar architecture.

### 6.5 Risks

- v3 is still alpha despite frequent releases and active development.
- Alpha APIs and packaging behavior can change.
- Current open issues include platform-specific tray regressions.
- Native webview differences remain across WebView2, WebKit, and Linux WebKitGTK.
- In-process UI bindings can tempt developers to expose too much backend authority to the renderer.

### 6.6 Recommendation

Track Wails v3 and build a time-boxed prototype if maintaining a predominantly Go stack is strategically important. Do not make it Aegis's release-critical desktop dependency until the project establishes a stable release and the Aegis platform matrix passes.

## 7. Tauri v2

### 7.1 Architecture

Tauri uses a Rust shell around the operating system's native webview. A frontend can use React, Svelte, Vue, or another web framework. Tauri can package external executables as sidecars, allowing the existing Go service to remain a distinct process.

For Aegis:

```text
Tauri shell
  - tray, windows, notifications, packaging
  - narrow command and capability surface
        |
        | authenticated framed local protocol
        v
Aegis Go process
  - principal authentication
  - policy and state machines
  - exact approvals
  - credential authority and broker
  - provisioning and authoritative audit
```

### 7.2 Tray and desktop support

Tauri v2 documents first-class system-tray construction, menu creation, dynamic icon changes, menu events, and tray events. It also provides plugins or APIs for notifications, autostart, global shortcuts, dialogs, filesystem scopes, and updater behavior.

Linux behavior still depends on the available status-notifier/AppIndicator implementation and desktop environment. No cross-platform shell eliminates that OS ecosystem constraint.

### 7.3 Streaming behavior

Tauri's documentation makes a useful distinction:

- events are for small streamed messages and multi-producer/multi-consumer patterns;
- channels are optimized for ordered, high-throughput streaming data;
- events are JSON-oriented and are not intended for low-latency or high-throughput bulk transfer.

Those primitives connect Rust and the frontend. A Go sidecar still needs an explicit Go-to-shell transport. Viable choices include:

- framed newline-delimited JSON over stdin/stdout;
- a private Unix-domain socket or Windows named pipe;
- authenticated loopback SSE for one-way event streams;
- authenticated WebSockets for bidirectional streams.

The Rust shell can translate sidecar messages into Tauri Channels, but that translation layer adds code and must enforce bounds and cancellation.

### 7.4 Strengths

- Stable v2 release line and very active ecosystem.
- First-class tray support.
- Native system webviews and generally lower distribution footprint than Electron.
- Explicit capability and permission model for shell APIs.
- Strong web UI ecosystem.
- Sidecar model preserves a clean authority boundary around Go.
- Documented distinction between ordinary events and high-throughput channels.

### 7.5 Risks

- Introduces Rust and Cargo into builds, CI, release engineering, and vulnerability management.
- Sidecar packaging and per-platform executable naming require care.
- Process supervision, startup readiness, crash recovery, and protocol compatibility become application responsibilities.
- The system webview differs across operating systems.
- A capability model does not automatically secure a local Go API.

### 7.6 Recommendation

Tauri v2 is the preferred production prototype. Its process boundary is an advantage for Aegis, not merely overhead: the renderer and desktop shell need not receive direct access to authoritative Go objects.

## 8. Electron

### 8.1 Architecture

Electron bundles Chromium and Node.js. It has a privileged main process, renderer processes, optional utility processes, mature IPC, native menus, and a long-established tray API. Go normally runs as a managed child process or a separate service.

### 8.2 Streaming behavior

Electron supports several efficient patterns:

- main/renderer IPC;
- MessageChannel and MessagePort;
- transferable data;
- Node streams;
- child-process stdin/stdout;
- SSE and WebSockets;
- utility processes for isolation.

It is the least restrictive choice for existing AI web UI libraries and browser developer tooling.

### 8.3 Strengths

- Most mature cross-platform web desktop ecosystem in the comparison.
- Strong tray, notification, window, updater, crash, and packaging support.
- Consistent bundled Chromium behavior across platforms.
- Excellent development tools and frontend library compatibility.
- Straightforward Go child-process integration.
- Large pool of experienced developers and examples.

### 8.4 Risks

- Larger installers and higher idle memory than native-webview alternatives.
- Bundled Chromium and Node expand patching and supply-chain responsibilities.
- Electron security requires disciplined context isolation, sandboxing, CSP, navigation restrictions, and IPC allowlists.
- Renderer compromise must be assumed in the architecture.
- A background tray utility may appear disproportionately heavy.

### 8.5 Recommendation

Choose Electron when delivery certainty, web compatibility, and diagnostics matter more than footprint. Keep Node integration out of renderers, expose only narrow typed IPC, and keep Aegis authority in the Go service.

## 9. Fyne

### 9.1 Architecture and tray support

Fyne is a Go-native cross-platform GUI toolkit inspired by Material Design. It has widgets, layouts, data binding, drawing, packaging support, and system-tray menus.

A complete small application can remain in one Go codebase and one GUI process.

### 9.2 Streaming behavior

Goroutines can receive backend updates while Fyne bindings or UI-thread callbacks update widgets. This is suitable for:

- status indicators;
- progress bars;
- bounded log views;
- health dashboards;
- forms and settings;
- simple notifications.

The application must avoid blocking the UI loop and must coalesce high-frequency updates.

### 9.3 Strengths

- Stable and active Go project.
- No JavaScript, Rust, or bundled browser runtime.
- Straightforward Go concurrency and domain integration.
- Built-in tray and packaging support.
- Good fit for compact operational tools.

### 9.4 Risks

- Smaller component and accessibility ecosystem than the web, Flutter, or Qt.
- Rich Markdown, code, virtualized timelines, advanced tables, and generative component surfaces require more custom implementation.
- Visual behavior is toolkit-defined rather than deeply platform-native.
- Open tray issues show that OS-specific lifecycle and refresh problems still exist.
- Tight in-process integration can expose authoritative operations too broadly to UI handlers unless application-service boundaries remain explicit.

### 9.5 Recommendation

Select Fyne only if the intended UI is deliberately constrained to tray status, settings, and a small number of operational screens. It is not the preferred foundation for Aegis's richer manager, approval, audit, and streamed-conversation direction.

## 10. Gio

Gio is an immediate-mode Go GUI library supporting desktop and mobile targets. It gives developers substantial control over rendering and can handle rapidly changing custom scenes efficiently.

Its trade-off is application infrastructure: conventional widgets, desktop integration, accessibility behavior, and tray lifecycle are less turnkey than in the leading alternatives. The GitHub repository is a mirror of its primary SourceHut repository, so GitHub statistics understate or distort some project activity.

Gio is a specialist option for a highly custom graphical surface. It is not the preferred framework for a conventional cross-platform Aegis tray client.

## 11. Flutter

Flutter has a mature reactive UI model, large widget ecosystem, desktop targets, testing tools, and strong animation/rendering behavior. Streamed data maps naturally into Dart streams and reactive state.

The mismatch is architectural: Go is neither Flutter's application language nor its native desktop shell. Aegis would need:

- a separately supervised Go service;
- process IPC;
- or an FFI bridge with lifecycle and memory-safety complexity.

System-tray behavior is generally supplied by plugins rather than the central desktop API. Flutter is viable if the organization has strong Dart/Flutter expertise or needs mobile and desktop from one UI codebase, but it offers no clear advantage for a Go-first Aegis desktop client over Tauri or Electron.

## 12. Qt and native platform shells

Qt has mature system-tray support through `QSystemTrayIcon`, excellent model/view facilities, Qt Quick/QML, accessibility, internationalization, and long-lived cross-platform packaging experience.

The concern is not Qt itself; it is the Go integration layer. Go bindings for Qt have historically been fragmented, incomplete, or sensitive to Qt version changes. A separate Qt/C++ or QML client talking to Go would be technically sound but adds a substantial language and build ecosystem.

Fully native shells are another valid option:

- Swift/AppKit on macOS;
- C#/.NET on Windows;
- GTK or Qt on Linux.

Native clients give the deepest platform integration but create three UI implementations and three release pipelines. This is justified only when deep native behavior outweighs cross-platform delivery cost.

## 13. Lightweight Go webview and tray libraries

Libraries such as `webview/webview_go`, `fyne-io/systray`, and `getlantern/systray` can assemble a small application from low-level parts.

This approach appears lightweight but transfers framework responsibilities to the product:

- event-loop coordination;
- main-thread requirements;
- window/tray lifecycle;
- web asset serving;
- IPC and type generation;
- crash recovery;
- packaging and signing;
- accessibility and testing;
- Linux desktop differences;
- updates and compatibility.

The maintained `fyne-io/systray` fork showed recent activity during inspection, while `getlantern/systray` had not been pushed since 2024. A low-level tray library can be appropriate for a tray-only utility with no rich window. It is not the preferred foundation for an evolving Aegis desktop product.

## 14. Streaming-UI architecture

### 14.1 Separate command and event planes

Do not represent a long operation as one unbounded request that returns only when complete. Use:

```text
command plane
  start_operation(request) -> operation_id
  cancel_operation(operation_id)
  acknowledge_prompt(operation_id, response)

event plane
  operation.started
  content.delta
  tool.proposed
  approval.required
  progress.changed
  operation.completed
  operation.failed
```

The command plane is authenticated and typed. The event plane is bounded, ordered where required, cancellable, and correlated by identifiers.

### 14.2 Backpressure and rendering

Model tokens and logs can arrive faster than a useful visual refresh rate. The transport and frontend should:

- bound message sizes and queue depth;
- coalesce text deltas for roughly one visual update per animation frame;
- use sequence numbers where order matters;
- detect gaps and request a state snapshot;
- virtualize long event and audit lists;
- cap retained in-memory history;
- support cancellation and deadline propagation;
- distinguish transport completion from authoritative operation completion.

The exact refresh interval should be measured rather than hard-coded from this report.

### 14.3 Snapshots plus deltas

A robust stream should not require replaying every event from process start. Use:

```text
initial authenticated snapshot
        +
ordered bounded deltas
        +
periodic or on-demand resynchronization
```

The frontend view is a projection. The Go service remains authoritative.

### 14.4 Error and lifecycle semantics

Every stream needs explicit handling for:

- client reconnect;
- duplicate events;
- missing sequence numbers;
- service restart;
- renderer reload;
- cancellation races;
- terminal success or failure;
- stale operation IDs;
- expired authentication;
- bounded shutdown.

A renderer must not infer success merely because a stream closed cleanly.

## 15. Constrained generative UI

Aegis should not execute model-generated JavaScript, install model-selected packages, or render arbitrary privileged HTML as a way to obtain “generative UI.”

Use a versioned, closed component protocol:

```text
UIEnvelope {
  schema_version
  operation_id
  sequence
  component_id
  component_type
  validated_properties
  permitted_actions
}
```

An initial allowlist could include:

- plain text;
- sanitized Markdown;
- status badge;
- progress indicator;
- key/value details;
- bounded table;
- audit event;
- session card;
- approval summary;
- deterministic confirmation form;
- error and recovery instruction.

Each component type must have:

- a strict schema and size bounds;
- reviewed rendering code;
- an accessibility contract;
- an explicit action allowlist;
- safe text and URL handling;
- tests for malformed and adversarial payloads;
- a fallback renderer for unknown versions.

The model may propose content or a component selection. It must not grant an action, authenticate a principal, approve a digest, choose a trust stanza outside authenticated policy, or change the service's authority.

## 16. Local transport choices

### 16.1 Framed standard input/output

Good for a directly supervised sidecar:

- no listening port;
- simple parent/child ownership;
- portable;
- easy to capture structured lifecycle errors.

Requirements include framing, maximum message sizes, separate diagnostic output, startup handshake, cancellation, crash detection, and prohibition on secret-bearing command-line arguments.

### 16.2 Unix-domain socket and Windows named pipe

Good for a separately supervised local service:

- supports reconnects and multiple clients;
- can use OS peer identity and filesystem/ACL controls;
- avoids exposing a TCP listener.

The implementation must bind protocol authentication to the actual peer and current session, not merely to a pathname or process-supplied identity.

### 16.3 Loopback HTTP with SSE or WebSockets

Good developer ergonomics and strong browser support, but loopback is not an authentication boundary. Requirements include:

- ephemeral unguessable session capability or stronger authenticated exchange;
- strict origin and host validation;
- no wildcard CORS;
- bounded request and stream sizes;
- protection against browser-based cross-origin attacks and local port discovery;
- short lifetime and explicit revocation;
- no reusable secret values in URLs, logs, or browser storage.

SSE is appropriate for one-way events. WebSockets are appropriate when bidirectional low-latency communication is actually needed. Ordinary authenticated commands can remain request/response.

## 17. Security analysis for Aegis

### 17.1 Trust boundary

The desktop shell and renderer are presentation components. They must not become the authority for:

- principal identity;
- trust-stanza selection;
- charter digest approval;
- mandate issuance;
- provisioning;
- credential access;
- audit append authority;
- runtime launch policy.

A displayed principal name, tray-menu selection, renderer local storage value, JavaScript message, URL parameter, or model-produced component is untrusted input.

### 17.2 Least renderer authority

The renderer should receive only the data needed for the current view. It should not receive:

- reusable credential plaintext;
- arbitrary filesystem access;
- arbitrary shell execution;
- generic local network access;
- an unrestricted Go object binding;
- raw audit-signing keys;
- profile, plugin, MCP, or provisioning authority.

Commands should be operation-specific, for example `GetSessionSummary`, `BeginApproval`, or `CancelOperation`, rather than generic `Execute`, `ReadFile`, or `CallBackend` primitives.

### 17.3 Secret handling

A desktop form is not automatically protected intake. Browser/webview state, devtools, accessibility APIs, clipboard managers, crash reports, logs, renderer memory, and screenshots can expose content.

Aegis's existing protected no-echo terminal path remains stronger for reusable credential intake until a desktop-specific secure-intake threat model and implementation are validated. A future desktop intake flow must preserve encryption, audit redaction, exact binding, and brokered-use requirements; it must not send plaintext through model or renderer event streams.

### 17.4 Update and supply-chain boundary

Desktop packaging adds:

- frontend package dependencies;
- Rust crates for Tauri or Node packages and Chromium for Electron;
- native webview/runtime prerequisites;
- platform signing identities;
- installer and update metadata;
- framework-specific build tools.

Updates must be signed and verified through a deterministic release process. Framework autoupdaters do not remove the need for Aegis-owned release provenance, rollback policy, and checksum/signature verification.

### 17.5 Renderer hardening

For web-based shells:

- use a restrictive Content Security Policy;
- prohibit remote navigation by default;
- disallow arbitrary remote scripts;
- sanitize Markdown and links;
- disable or tightly control devtools in release builds without treating that as a security boundary;
- expose a narrow allowlisted bridge;
- validate every payload again in Go;
- treat frontend dependencies and generated assets as supply-chain inputs.

Electron additionally requires context isolation, sandboxed renderers where feasible, no renderer Node integration, and strict IPC sender validation.

## 18. Platform and packaging realities

### 18.1 macOS

A production menu-bar application needs:

- a correctly structured `.app` bundle;
- menu-bar template icons and dark-mode behavior;
- optional accessory activation policy to suppress a Dock icon;
- code signing;
- hardened runtime and entitlements as applicable;
- notarization and stapling;
- update and quarantine testing.

### 18.2 Windows

A production tray application needs:

- executable icon resources;
- notification-area lifecycle handling, including Explorer restart;
- hidden-window/taskbar behavior;
- code signing and installer behavior;
- WebView2 availability or bundling policy for native-webview shells;
- startup registration and uninstall cleanup.

### 18.3 Linux

“Linux tray support” is not one uniform target. Validation must cover the declared combinations of:

- GNOME, KDE Plasma, and other supported desktops;
- AppIndicator/StatusNotifier availability;
- X11 and Wayland behavior;
- GTK/WebKitGTK versions for webview shells;
- `.deb`, RPM, Flatpak, Snap, or AppImage packaging choices;
- desktop files, icons, autostart, and sandbox portals.

Some GNOME environments require an extension for tray indicators. The application must degrade to a normal window or another documented control path rather than assuming an icon exists.

## 19. Recommended Aegis architecture

### 19.1 Production candidate

```text
Svelte or React frontend
        |
Tauri v2 desktop shell
  - tray/menu bar
  - one or more constrained windows
  - notifications
  - local shell capability policy
        |
  authenticated versioned IPC
        |
Aegis Go service
  - external principal authentication
  - shared application services
  - identity and stanza selection
  - exact charter approval
  - mandate and session lifecycle
  - credential authority and broker
  - deterministic provisioning
  - authoritative audit
        |
explicit Hermes runtime adapter
```

Svelte is a good default for a compact client with less framework machinery. React is the safer choice when maximum component-library availability and contributor familiarity are more important. This report does not make the frontend-library choice normative.

### 19.2 Process ownership

Two deployment modes should be compared:

1. **Sidecar mode:** the desktop shell launches one bounded Aegis child process for the logged-in session.
2. **Service-client mode:** a separately supervised Aegis service exists independently and the tray application authenticates as a local client.

Service-client mode is preferable if Aegis must continue operating without the tray UI, serve the CLI and UI concurrently, or run privileged enforcement components. Sidecar mode is simpler for an initial unprivileged personal prototype.

The UI closing must not silently terminate security-critical sessions unless that lifecycle is explicit and verified.

### 19.3 UI scope for an initial prototype

The first prototype should be read-mostly:

- tray icon showing coarse service health;
- Open Aegis action;
- active-session summary;
- runtime visibility, including explicit Hermes labeling;
- bounded streamed manager output;
- deterministic operation progress;
- audit/event timeline with virtualization;
- Quit UI versus Stop Service as distinct actions.

Do not begin with desktop secret intake, provisioning, autostart installation, system-service mutation, or automatic updates. Those are consequential capabilities requiring separate scope and approval.

## 20. Prototype and measurement plan

Build the same narrow vertical slice in two candidates:

- Tauri v2 + Svelte + Go sidecar;
- Wails v3 + Svelte, only as a forward-looking comparison.

If avoiding alpha dependencies is mandatory, replace the Wails prototype with Electron + Svelte + Go child process.

The slice should:

1. start in the system tray;
2. open and hide a small window;
3. display an authenticated snapshot from a mock or non-authoritative Go service;
4. stream a large bounded sequence of text and progress events;
5. cancel an operation;
6. recover from a renderer reload;
7. detect a killed Go process;
8. restart or report failure according to policy;
9. reject malformed, oversized, duplicate, stale, and out-of-order messages;
10. package on all declared operating systems.

Measure rather than assume:

- clean and warm startup latency;
- idle and streaming memory;
- idle CPU and wakeups;
- packaged and installed size;
- event throughput and dropped-event behavior;
- time to first visible delta;
- UI responsiveness under burst load;
- shutdown and process-leak behavior;
- accessibility checks;
- installer, signing, and update complexity;
- cross-platform rendering and tray correctness.

## 21. Acceptance gates

A framework should not become a release dependency until:

1. Tray creation, dynamic updates, hide/show, and shutdown pass on every supported platform.
2. Linux behavior is documented for each supported desktop/display-server combination.
3. The Go service remains usable by the existing CLI without the desktop client.
4. Renderer compromise does not directly grant authoritative Go operations.
5. Every command and event payload is typed, versioned, bounded, and validated.
6. Streaming has cancellation, backpressure, gap detection, resynchronization, and terminal status.
7. Renderer reload and service restart behavior are deterministic.
8. No reusable credential enters frontend logs, URLs, local storage, crash reports, or model streams.
9. Accessibility checks cover keyboard, focus, screen readers, scaling, contrast, and reduced motion.
10. Signed packages can be built reproducibly enough for the declared release process.
11. Framework and frontend dependency vulnerability handling is documented.
12. Idle CPU, memory, size, startup, and event-latency budgets are measured and accepted.
13. Update rollback and failure behavior are exercised without weakening Aegis release verification.
14. A threat-model update records the shell, renderer, IPC, updater, and local-service boundaries.
15. README, architecture, quickstart, demonstration, recording, release assets, checksums, and contributor material are updated only when the feature becomes implemented and reproducible.

## 22. Decision matrix by product priority

### Priority: strongest production support and UI ecosystem

Choose **Electron**, with Go as a separate service or managed child.

### Priority: lower footprint with mature tray support

Choose **Tauri v2**, with Go as a sidecar or service.

### Priority: Go-first implementation and one integrated application

Track **Wails v3**, but wait for a stable release or accept and explicitly manage alpha risk for a prototype.

### Priority: no JavaScript or Rust and only a small operational UI

Choose **Fyne**.

### Priority: custom graphics rather than a conventional desktop application

Evaluate **Gio**, accepting greater application infrastructure work.

### Priority: mobile and desktop from one UI codebase

Evaluate **Flutter**, but treat Go as a separate service and accept Dart/plugin complexity.

## 23. Final recommendation

Aegis should not interpret the absence of a first-party Go GUI toolkit as a reason to move its control plane away from Go. The durable split is:

> Go owns identity, authority, policy, state transitions, credentials, runtime control, and audit. A replaceable desktop shell owns presentation and operating-system interaction.

Proceed in this order:

1. Preserve the current CLI and shared Go application services as the product authority.
2. Define a narrow, authenticated, versioned local desktop-client protocol.
3. Prototype Tauri v2 with Svelte against an unprivileged/read-mostly Go sidecar.
4. Compare Electron only if Tauri's Rust, native-webview variation, or tooling creates unacceptable delivery risk.
5. Track Wails v3 stability and repeat the prototype when v3 reaches a stable release.
6. Add mutating UI operations only after the renderer, IPC, approval, secret, and audit threat boundaries are specified and tested.
7. Keep a deterministic CLI path for every consequential operation and for recovery when the desktop client is unavailable.

This recommendation does not authorize provisioning a desktop application, installing autostart entries, changing system services, downloading framework toolchains, or activating external update infrastructure.

## 24. Open decisions

1. Is the first desktop target macOS, Windows, Ubuntu/GNOME, or an explicit matrix?
2. Must the client run when no principal is logged in, or only inside a desktop session?
3. Is the Go process a per-login sidecar or an independently supervised service?
4. Is an alpha framework categorically prohibited for prototypes or only for releases?
5. Is Rust acceptable in the release toolchain?
6. What idle memory, installed size, startup, and event-latency budgets apply?
7. Does the initial client need conversation rendering, or only tray status and session controls?
8. Which operations may be initiated from the desktop client, and which remain terminal-only?
9. Can reusable secret intake remain terminal-only for the first desktop release?
10. Which Linux desktop environments and package formats are supported?
11. Is mobile reuse a real requirement or an unnecessary architecture driver?
12. Which frontend library and component system meet Aegis accessibility and supply-chain requirements?
13. What is the update trust model for the desktop shell and its Go service?
14. How does a renderer authenticate to an already-running service without placing reusable credentials in frontend storage?
15. What UI schema, if any, is allowed to be model-influenced?

## 25. Launch-asset impact review

This report is research only and changes no implemented behavior, command syntax, configuration, dependencies, packaging, runtime support, or security claim.

Reviewed launch assets:

- root `README.md` — unaffected;
- `LICENSE` — unaffected;
- `SECURITY.md` — unaffected;
- `CONTRIBUTING.md` — unaffected;
- `CODE_OF_CONDUCT.md` — unaffected;
- `CHANGELOG.md` — unaffected because no product behavior changed;
- `docs/THREAT_MODEL.md` — unaffected until a desktop implementation exists;
- `docs/ARCHITECTURE.md` — unaffected until a desktop implementation exists;
- `docs/QUICKSTART.md` — unaffected;
- `docs/DEMO_NO_KEY.md` — unaffected;
- `docs/RECORDING.md` — unaffected;
- GitHub release binaries and checksums — unaffected; none were created or changed;
- contributor issues — unaffected; no external issue was created.

`docs/README.md` is updated only to make this retained research discoverable. No implementation or release claim is added.

## Sources

All sources accessed 2026-07-17 unless otherwise stated.

### Wails

1. Wails repository and version/status table: https://github.com/wailsapp/wails
2. Wails v3 system-tray documentation: https://github.com/wailsapp/wails/blob/master/docs/src/content/docs/features/menus/systray.mdx
3. Wails v3 Events API: https://github.com/wailsapp/wails/blob/master/docs/src/content/docs/reference/events.mdx
4. Wails binding architecture and unsupported Go channel binding: https://github.com/wailsapp/wails/blob/master/docs/src/content/docs/contributing/architecture/bindings.mdx
5. Wails v3 basic tray example: https://github.com/wailsapp/wails/blob/master/v3/examples/systray-basic/main.go
6. Wails v2 system-tray pull request: https://github.com/wailsapp/wails/pull/4991
7. Wails releases: https://github.com/wailsapp/wails/releases

### Tauri

8. Tauri v2 introduction: https://v2.tauri.app/start/
9. Tauri system-tray guide: https://v2.tauri.app/learn/system-tray/
10. Calling the frontend, including events and streaming channels: https://v2.tauri.app/develop/calling-frontend/
11. Calling Rust from the frontend, including channels: https://v2.tauri.app/develop/calling-rust/
12. Embedding external binaries and sidecars: https://v2.tauri.app/develop/sidecar/
13. Tauri repository: https://github.com/tauri-apps/tauri
14. Tauri releases: https://github.com/tauri-apps/tauri/releases

### Electron

15. Electron process model: https://www.electronjs.org/docs/latest/tutorial/process-model
16. Electron Tray API: https://www.electronjs.org/docs/latest/api/tray
17. Electron MessagePortMain API: https://www.electronjs.org/docs/latest/api/message-port-main
18. Electron security guidance: https://www.electronjs.org/docs/latest/tutorial/security
19. Electron repository: https://github.com/electron/electron
20. Electron releases: https://github.com/electron/electron/releases

### Fyne and Gio

21. Fyne getting started: https://docs.fyne.io/started/
22. Fyne system-tray menu documentation: https://docs.fyne.io/explore/systray/
23. Fyne data binding: https://docs.fyne.io/explore/binding/
24. Fyne repository: https://github.com/fyne-io/fyne
25. Fyne releases: https://github.com/fyne-io/fyne/releases
26. Fyne systray repository: https://github.com/fyne-io/systray
27. Gio architecture: https://gioui.org/doc/architecture
28. Gio repository mirror: https://github.com/gioui/gio

### Other frameworks and low-level libraries

29. Tauri alternative Electron repository comparison input: https://github.com/electron/electron
30. Flutter desktop support: https://docs.flutter.dev/platform-integration/desktop
31. Flutter platform channels: https://docs.flutter.dev/platform-integration/platform-channels
32. Flutter repository: https://github.com/flutter/flutter
33. Qt `QSystemTrayIcon`: https://doc.qt.io/qt-6/qsystemtrayicon.html
34. Qt Base repository: https://github.com/qt/qtbase
35. Go bindings for webview: https://github.com/webview/webview_go
36. Maintained Fyne systray fork: https://github.com/fyne-io/systray
37. Lantern systray repository: https://github.com/getlantern/systray

### Platform guidance

38. Apple, Human Interface Guidelines, menu bar extras: https://developer.apple.com/design/human-interface-guidelines/the-menu-bar
39. Microsoft, notification-area application guidance: https://learn.microsoft.com/windows/win32/shell/notification-area
40. freedesktop.org StatusNotifierItem specification: https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/
