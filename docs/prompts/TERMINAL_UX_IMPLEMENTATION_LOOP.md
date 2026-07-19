# Aegis Terminal Experience Implementation Loop

Implement a complete, production-quality Aegis terminal experience with two deliberately separate surfaces:

1. a standard-terminal bootstrap and reconfiguration wizard; and
2. a modern Aegis-owned TUI for the authenticated manager runtime.

This must be an original Aegis experience. It may learn from the usability principles demonstrated by Hermes Agent, but it must not clone Hermes branding, wording, visual layout, themes, mascots, source code, implementation structure, or setup wizard. Preserve Hermes as the explicit underlying runtime while making Aegis's identity, trust, authority, and session controls visually and operationally primary.

Read `AGENTS.md` first. Then inspect the normative specifications, current command tree, initialization flow, manager lifecycle, terminal input, Hermes structured-gateway adapter, Ollama route, credential authority, certification, audit services, tests, and documentation before designing or editing anything.

## Current working tree

The repository already contains uncommitted `aegis reset` work. Preserve, inspect, verify, and integrate with it. Do not overwrite, revert, discard, or broadly reformat it:

```text
M .gitignore
M CHANGELOG.md
M CONTRIBUTING.md
M README.md
M SECURITY.md
M docs/ARCHITECTURE.md
M docs/QUICKSTART.md
M docs/THREAT_MODEL.md
M internal/command/root.go
?? internal/command/reset.go
?? internal/command/reset_test.go
?? internal/reset/
```

Before editing, run fresh Git status and diff inspection because this list may have changed. Treat all existing modifications as user work.

## Core outcome

Running bare interactive `aegis` must provide one coherent journey:

```text
standard-terminal bootstrap
    -> verified ready state
    -> optional launch into the Aegis manager TUI
```

On an already-ready installation:

```text
bare interactive aegis
    -> Aegis manager TUI
```

On a valid intermediate installation:

```text
bare interactive aegis
    -> resumable standard-terminal bootstrap at the first incomplete stage
```

On a repair-required installation:

```text
bare interactive aegis
    -> standard-terminal diagnosis with deterministic remediation
```

In a non-TTY environment:

```text
bare aegis
    -> no prompts, no TUI, no mutation
    -> stable structured state, reason, next command, and exit status
```

Do not stop at a design, mockup, screen rendering, placeholder coordinator, or disconnected TUI. Implement the complete exercised path through the real CLI using isolated fixtures.

## Design identity

Create an original Aegis terminal language appropriate to identity and authority management.

It should feel:

- calm;
- exact;
- security-conscious;
- readable;
- restrained;
- operational rather than playful;
- transparent about state and side effects.

Use consistent semantics for status markers, headings, warnings, errors, progress, and security decisions. Do not copy Hermes's banner, caduceus, colors, mascots, prose, setup modes, or exact component arrangement.

Aegis terminology must remain accurate:

- principal;
- logical agent;
- trust stanza;
- mandate;
- session;
- charter revision;
- credential authority;
- Hermes Agent runtime;
- exact model artifact;
- certification;
- audit provenance.

A trust stanza is a security context, not a personality.

## Surface 1: standard-terminal bootstrap

The bootstrap must be an ordinary terminal wizard, not the modern runtime TUI.

It must work correctly when the executable was launched directly from a real terminal. It must also preserve terminal behavior and detect non-interactive execution accurately.

Implement an explicit artifact-derived onboarding state machine, using existing states where possible and extending them carefully where required:

```text
uninitialized
principal-configured
authority-configured
runtime-configured
model-present
model-certified
ready
repair-required
```

Do not persist one optimistic `onboarding_complete` boolean. Derive state from validated configuration, authority artifacts, runtime discovery, exact model identity, certification, and security checks.

### Required bootstrap stages

1. Installation inspection
   - Resolve the exact configuration and state paths.
   - Inspect existing artifacts without mutation.
   - Authenticate the local OS identity.
   - Determine the first incomplete verified stage.
   - Clearly report valid, incomplete, malformed, insecure, drifted, or ambiguous state.

2. Principal
   - Explain the intended principal binding.
   - Show exact OS identity attributes used for authentication.
   - Preview the configuration before writing.
   - Require appropriate authenticated confirmation.
   - Write strict mode-0600 configuration atomically.
   - Reinspect and verify before advancing.

3. Credential authority
   - Do not require manual YAML editing.
   - Offer supported custody choices with truthful security descriptions:
     - passphrase-encrypted local KEK custody as the working bare-terminal default;
     - production systemd encrypted credential custody;
     - explicitly weaker development host-file custody.
   - Do not imply the two choices provide equivalent protection.
   - For host-file custody, derive safe Aegis-owned database and KEK paths.
   - Show deployment ID, paths, ownership, modes, backup warning, and local-root limitation before mutation.
   - Require exact authenticated confirmation.
   - Reuse the existing credential initialization service.
   - Verify database, schema, KEK, deployment-bound sentinel, ownership, and permissions.
   - For systemd custody, render exact external prerequisite instructions and resume once delivered credentials validate.
   - Fix absent-authority errors so an empty custody value is never misreported as a systemd selection.

4. Hermes runtime
   - Discover the exact Hermes executable and version.
   - Display Hermes Agent by name.
   - Verify the supported version range.
   - Explain that Aegis uses a disposable Hermes home, safe mode, minimal environment, and structured gateway.
   - State accurately that runtime-state isolation is not host sandboxing.
   - Never modify or attach to normal Hermes profiles.

5. Ollama deployment
   - Discover supported managed or explicit external-local mode.
   - Show executable, endpoint, version, ownership boundary, and process policy.
   - Preserve local-only routing.
   - Never add cloud fallback or model switching.
   - Never silently start, stop, replace, or take ownership of an operator-managed daemon.

6. Exact model artifact
   - Discover approved installed candidate artifacts using the existing closed registry.
   - Show candidate ID, exact Ollama name, publisher/source, license, terms URL, digest, artifact size, quantization, context, and required capabilities.
   - Do not silently choose a candidate.
   - Reject unsupported, modified, abliterated, mutable, ambiguous, or drifted artifacts.
   - If no approved candidate is installed, offer an exact download only during explicit onboarding and only after a separately authenticated, exact-plan-bound confirmation.
   - Show the network action, expected artifact, endpoint/store, approximate size, licensing information, and digest-pinning behavior before download.
   - Decline, EOF, cancellation, or non-TTY use must perform no download.
   - Never download a real model in automated tests.
   - Rediscover after download and bind configuration to the exact resulting digest.

7. Certification
   - Explain resource use before launch.
   - Require explicit confirmation.
   - Run the existing complete Hermes -> authenticated Aegis proxy -> Ollama conformance path.
   - Do not create a weaker wizard-only certification.
   - Show bounded progress by named conformance stage without exposing prompts, tokens, secret-shaped data, or model internals.
   - Persist certification only after every required test passes.
   - Bind certification to exact model digest, Hermes version, Ollama version, context, corpus digest, timestamp, and result set.
   - Unload and clean up the model and runtime on success, failure, or cancellation as required.
   - A failed certification leaves deterministic administration available but never reports readiness.

8. Final readiness
   - Reinspect every stage from authoritative artifacts.
   - Render a concise truthful summary containing:
     - authenticated principal;
     - credential-authority status;
     - Hermes path and version;
     - Ollama route;
     - exact model name and shortened digest with an inspection command for the full value;
     - certification status;
     - cloud-fallback state;
     - model-switching state;
     - runtime-state-isolation limitation.
   - Offer:
     - start the Aegis manager TUI;
     - exit cleanly.
   - Starting the runtime must use the same verified state and must not ask the user to restart or manually chain commands.

### Bootstrap interaction requirements

- Use bounded keyboard-selectable menus for closed choices where appropriate.
- Arrow keys and number shortcuts may be supported, but all behavior must remain understandable without color.
- Respect `NO_COLOR` and non-Unicode terminals.
- Preserve readable behavior on narrow terminals.
- Use masked/no-echo input only for protected values.
- Restore terminal state on success, failure, cancellation, panic boundary, EOF, and signal.
- Ctrl+C must cancel safely and leave a valid resumable state.
- Never interpret cancellation as a policy or scanner failure.
- Print exact effects before consequential mutation.
- Reauthenticate immediately before foundational or destructive actions.
- Detect path, configuration, and artifact changes between preview and application.
- Keep stdout for command results and stderr for diagnostics.
- Do not use a model to select onboarding choices, authorize actions, classify state, or produce audit facts.

### Bootstrap resumption

Running `aegis init` or bare interactive `aegis` against a valid intermediate installation must resume rather than reject it as already initialized.

Re-running onboarding must not unnecessarily:

- rotate keys;
- recreate authority data;
- redownload models;
- overwrite exact route configuration;
- rerun valid certification;
- modify normal Hermes state;
- duplicate audit artifacts.

Every skipped stage must first be revalidated.

## Surface 2: Aegis manager TUI

After readiness, bare interactive `aegis` must launch a modern Aegis-owned terminal UI.

The TUI is a presentation and interaction layer over deterministic Aegis services and the existing isolated Hermes structured-gateway integration. It must not become a new source of authority.

Build an original transcript-first runtime with:

- a responsive startup frame;
- authenticated session summary;
- scrollable conversation transcript;
- persistent composer;
- streaming assistant output;
- structured tool and proposal activity;
- bounded details expansion;
- queued-input display;
- status line;
- command completion;
- secure approval and protected-input overlays;
- clean interruption and exit;
- readable narrow-terminal behavior;
- mouse support only where it does not harm keyboard operation;
- complete keyboard-only operation.

### Runtime startup

Render the first frame before expensive runtime initialization completes.

Display truthful startup stages such as:

```text
authenticating principal
validating mandate
verifying stanza
discovering Hermes
starting disposable runtime
opening authenticated inference route
verifying exact model
ready
```

Allow the composer to accept input during startup, but do not send it until the session is fully authenticated and ready. Clearly mark queued messages. If startup fails, preserve the queued text locally for editing but do not send it through an incomplete route.

### Persistent security context

Keep a compact security summary visible or one keypress away:

- authenticated principal;
- exact trust stanza;
- logical agent;
- mandate identifier and status;
- charter revision and digest;
- Hermes Agent version;
- exact model identity;
- session expiry;
- cloud fallback disabled;
- model switching disabled.

Do not union permissions across stanzas. The TUI cannot change stanza inside an active session. Any material authority change requires a new mandate and clean runtime session.

### Conversation and tools

- Continue using Aegis's deterministic ingress guard before Hermes.
- Continue using the Aegis inference proxy as the only Ollama route.
- Continue using deterministic egress and proposal validation.
- Render typed Aegis manager proposals distinctly from ordinary model text.
- Never render model narration as authoritative success.
- Show tool and proposal status from authoritative Aegis and runtime events.
- Keep detailed payloads collapsed when noisy, but make safe metadata inspectable.
- Do not expose reusable credentials, protected input, proxy tokens, raw secret-shaped text, or hidden prompts.

### Approvals

Create clear modal or inline bounded approval surfaces for Aegis-controlled actions.

Each approval must show:

- operation;
- authenticated actor;
- exact target;
- scope;
- security consequences;
- expiration where applicable;
- whether the action affects only this session or persistent state;
- allowed choices;
- safe default.

Use conventional `[Y/n]` and `[y/N]` prompts for bootstrap choices; Enter accepts the clearly displayed default. Digest-bound integrity comes from displaying, binding, and revalidating the exact plan, not from making the operator copy generated prose. Keep an exact phrase only for genuinely destructive reset unless the product policy explicitly changes it.

### Protected credential intake

- Use a separate no-echo terminal path.
- Suspend conflicting composer input while protected intake is active.
- Preserve bounded mutable buffers where practical.
- Reject oversized and mismatched input.
- Restore terminal mode under every exit path.
- Never place the credential value in model history, UI transcript, logs, errors, audit records, receipts, or ordinary Go strings where avoidable.

### Runtime controls

At minimum support discoverable local deterministic controls for:

- help;
- status and security context;
- clear local display;
- audit verification;
- session exit;
- immediate stop;
- protected credential operations already supported by the manager.

Do not add commands that let prompt content select identity, stanza, runtime, provider, model, or authority.

### Lifecycle

Implement and test:

- startup cancellation;
- input queued during startup;
- normal turn completion;
- Ctrl+C interruption;
- repeated Ctrl+C;
- EOF;
- `/exit` and `/quit`;
- mandate expiry;
- revocation;
- Hermes failure;
- Ollama failure;
- certification drift detected at startup;
- protected-intake interruption;
- bounded graceful shutdown;
- process-group termination after deadline;
- model unload;
- inference-proxy closure;
- disposable-home removal;
- terminal restoration;
- final authoritative receipt and audit completion.

Use existing lifecycle services rather than creating a second runtime manager.

## Architecture

- Keep Cobra commands thin.
- Keep onboarding and runtime orchestration in focused application services.
- Separate terminal rendering and input from security decisions and state transitions.
- Use constructor injection for terminal capabilities, clocks, identity, filesystem, process launch, runtime gateway, Ollama client, and certification where needed.
- Use `context.Context` cancellation throughout.
- Use injected `log/slog` loggers.
- Decode configuration once into strict typed validated values.
- Prefer small focused packages over a monolithic setup or TUI file.
- Avoid package-level mutable command or UI state.
- Do not copy source from Hermes Agent.
- Evaluate mature Go terminal libraries against the required behavior and existing dependency and security posture before adding one. Add the minimum justified dependencies, pin them, document why, and do not introduce a framework merely to imitate Hermes.
- Preserve plain standard-terminal fallback where the runtime TUI cannot safely initialize.

## Non-cloning requirement

The implementation may adopt general usability principles:

- immediate visual response;
- progressive disclosure;
- explicit state;
- resumability;
- contextual help;
- keyboard-first interaction;
- safe approval defaults;
- concise completion summaries;
- setup-to-runtime continuity.

It must not reproduce:

- Hermes artwork or caduceus;
- Hermes colors or themes;
- Hermes mascots or playful status vocabulary;
- Hermes banner geometry;
- Hermes wording;
- Hermes setup-mode copy;
- Hermes component names or source structure;
- Hermes-specific personality or profile onboarding;
- screenshots or terminal recordings copied from Hermes.

Produce an original Aegis visual and interaction design grounded in its security purpose.

## Testing

Use hermetic tests with isolated `HOME`, `XDG_CONFIG_HOME`, state, audit, authority, model, certification, and disposable Hermes paths. Never operate on the developer's actual Aegis configuration, credential authority, Ollama store, Hermes profiles, or downloaded models.

Add focused tests covering at minimum:

### Bootstrap

- clean first run to ready;
- reset followed by complete onboarding replay;
- every valid intermediate-state resume;
- repair-required paths;
- reconfiguration and idempotency;
- host-file authority;
- systemd external prerequisite;
- absent, malformed, and insecure authority;
- installed approved model selection;
- multiple candidate selection with no silent default;
- declined, interrupted, failed, and digest-drifted model download;
- certification success, semantic failure, cancellation, and cleanup;
- no readiness from presence alone;
- non-TTY no-prompt and no-mutation behavior;
- Ctrl+C and EOF terminal restoration;
- exact preview and confirmation binding;
- no secret leakage;
- no normal Hermes profile mutation.

### Runtime TUI

- immediate startup frame;
- startup-stage transitions;
- queued input held until ready;
- startup failure does not send queued input;
- principal, stanza, mandate, and model visibility;
- narrow terminal rendering;
- no-color and keyboard-only operation;
- slash-command completion;
- approval safe defaults;
- protected intake isolation;
- ingress rejection before Hermes;
- egress and proposal rejection before display or reuse;
- streaming output and tool or proposal events;
- cancellation and repeated interruption;
- expiry and revocation;
- Hermes and Ollama failure;
- bounded cleanup;
- terminal restoration;
- no secret material in transcript, status, errors, logs, audit, or receipts.

### End to end

- PTY test from genuinely absent state through standard-terminal onboarding into the manager TUI;
- fake supported Hermes structured gateway;
- fake loopback Ollama with an exact approved artifact;
- real deterministic Aegis proxy and guards;
- fake successful certification;
- ordinary message reaches Hermes and the model only after readiness;
- exact typed response appears in the TUI;
- clean exit removes disposable runtime state;
- external and operator-managed assets remain unchanged.

Do not use snapshot tests alone for security-critical behavior. Assert semantic state, events, side effects, and absence of forbidden output.

## Documentation and launch assets

Update every affected source of truth, including:

- README;
- quickstart;
- architecture;
- threat model;
- security guidance;
- credential-authority setup;
- manager lifecycle and onboarding specifications;
- manager runtime specifications;
- demo;
- recording instructions;
- changelog;
- contributor issue material.

Clearly distinguish implemented behavior from planned behavior.

Update architecture diagrams to show:

```text
standard-terminal onboarding
    -> deterministic readiness services
    -> Aegis manager TUI
    -> Aegis guards and proxy
    -> isolated Hermes structured gateway
    -> exact local Ollama artifact
```

Run every locally exercisable documented command. Do not fabricate screenshots, recordings, demonstrations, releases, checksums, issue links, certification results, or command output.

Perform the complete launch-asset impact review required by `AGENTS.md`. Report each affected asset changed and each reviewed unaffected asset.

## Verification

Before completion:

- format all changed Go and UI files;
- run focused onboarding tests;
- run focused manager and TUI tests;
- run reset tests to prove the existing work was preserved;
- run relevant race tests;
- run `go test ./...`;
- run `go vet ./...`;
- build `./cmd/aegis`;
- run any UI-specific typecheck, lint, test, and build introduced by the selected implementation;
- run `git diff --check`;
- exercise the complete bootstrap-to-runtime PTY path against isolated fake services;
- exercise documented reset-and-replay onboarding against isolated paths;
- inspect final Git status and diff for unintended changes.

If the environment lacks a canonical verifier, create a narrowly scoped OS-safe `/tmp/hermes-verify-*` script, run it, remove it, and label the result explicitly as ad-hoc verification. Do not replace available canonical tests with ad-hoc evidence.

## Scope and authority

Allowed:

- edit files within `/home/javi/code/aegis`;
- add justified pinned dependencies;
- run tests, builds, linters, local fake services, and isolated PTY verification;
- update repository-local documentation and diagrams.

Prohibited:

- reset or modify the operator's real Aegis installation;
- initialize or delete the real credential authority;
- download or remove real Ollama models;
- start, stop, or reconfigure the operator-managed Ollama daemon;
- modify normal Hermes profiles or installation;
- provision or activate external agents;
- create gateways, cron jobs, plugins, MCP servers, cloud resources, releases, or remote issues;
- use real credentials;
- commit, tag, push, merge, publish, or deploy;
- discard or overwrite existing uncommitted reset work;
- claim host sandboxing or complete zero trust.

Do not stop after planning, dependency selection, screen mockups, scaffolding, or isolated unit tests. Continue until the standard-terminal bootstrap, Aegis manager TUI, complete isolated bootstrap-to-runtime path, documentation, and verification are implemented and exercised. If a genuine security or authority blocker prevents completion, stop at that exact boundary and report the blocker with concrete evidence rather than weakening the requirement or inventing success.

In the final report include:

- UX and architecture decisions;
- exact files changed;
- dependencies added and justification;
- how existing reset work was preserved and integrated;
- exact verification commands and real results;
- end-to-end PTY evidence;
- launch assets reviewed;
- residual security limitations;
- external-only steps not performed;
- no unsupported claim of completion.
