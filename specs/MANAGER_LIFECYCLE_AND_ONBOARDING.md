# Manager Lifecycle, Graceful Shutdown, and Local Onboarding Remediation

## 1. Status and purpose

**Status:** Normative remediation specification

**Target:** Aegis built-in manager lifecycle, degraded mode, protected intake, and deterministic local-model onboarding

**Parent specifications:**

- `specs/AEGIS_MANAGER.md`
- `specs/BASE_MANAGER_END_TO_END.md`
- `specs/RUNTIME_AND_SESSIONS.md`

This specification defines the required remediation for failures observed in a real first-run manager session:

```text
$ aegis
...
Inference: Ollama local / not configured@not certified
...
Conversational local inference unavailable. Reason: manager_model_not_certified

aegis> hey
The local Aegis management model is unavailable.

aegis> ^C
aegis> exit
Aegis blocked the message: manager_scanner_failed
```

Repeated Ctrl-C did not terminate the process.

A temporary pseudo-terminal reproduction against the affected implementation established:

```text
ALIVE_AFTER_SIGINT=true
ALIVE_AFTER_PLAIN_EXIT=true
```

This document converts that failure into one bounded implementation target. The completed remediation MUST make this statement true:

> First Ctrl-C, SIGTERM, EOF, slash exit, plain exact exit, expiry, revocation, and runtime failure all converge on one bounded cleanup state machine; cancellation never becomes a scanner-policy error; protected intake always restores the terminal; degraded mode reports only usable operations; and an authenticated operator receives a deterministic path from no model to one exact locally installed and certified model without automatic download or cloud fallback.

## 2. Authority and conflict resolution

The implementation MUST follow this authority order:

1. `AGENTS.md`
2. `specs/MVP.md`
3. `specs/AEGIS_MANAGER.md`
4. `specs/BASE_MANAGER_END_TO_END.md`
5. this remediation specification
6. other focused specifications
7. retained research
8. implementation convenience

A conflict MUST be resolved explicitly in favor of the higher-authority requirement. An implementation session MUST NOT weaken a security boundary to improve terminal convenience.

This specification does not authorize:

- model download or update;
- global Ollama configuration changes;
- normal Hermes profile changes;
- agent provisioning or activation;
- credential creation outside explicit protected intake;
- Git commits, tags, pushes, releases, or remote issue creation;
- modification of external systems.

## 3. Confirmed root causes

### 3.1 Root context cancellation without REPL termination

The executable installs `signal.NotifyContext` around the root command. The first SIGINT cancels the root context. The manager REPL continues its synchronous scanner loop and does not treat `ctx.Done()` as a shutdown condition.

A later line is inspected with the already-canceled context. The guard correctly retains its fail-closed default and returns `manager_scanner_failed`. The manager incorrectly treats that result as a recoverable message-policy denial and continues.

Cancellation is therefore converted into a permanent scanner-failure loop.

### 3.2 Normal signal behavior remains intercepted

The signal context's stop function does not run until the root command returns. Because the manager remains alive, later SIGINTs remain intercepted rather than restoring normal process termination behavior.

### 3.3 Blocking terminal reads are not context-aware

The ordinary REPL blocks in `bufio.Scanner.Scan`. Protected intake blocks in terminal password input. Neither read is guaranteed to return merely because the root context was canceled.

### 3.4 Cleanup is not one bounded transaction

Manager cleanup is deferred with an unbounded background context, returned errors are discarded, and the end reason is collapsed into `terminal_closed`.

### 3.5 Runtime readiness is misclassified

An absent model is returned as `manager_model_not_certified`, even though absence and failed/missing certification are distinct states with different remediation.

### 3.6 Degraded mode overstates available operations

The fallback advertises slash-form credential mutations that its dispatcher rejects. It also advertises credential operations immediately after first run even though no credential authority exists.

### 3.7 Onboarding stops before a usable local manager route

First run correctly avoids automatic model download and authority creation, but Aegis does not provide a complete deterministic command path to discover, select, configure, and certify an already-installed approved local model.

Managed Ollama uses an Aegis-private model store, while documentation refers to already-installed local models without resolving whether those models are visible to that private store.

## 4. Security and lifecycle invariants

The remediation is incomplete if any invariant below is violated.

### 4.1 Cancellation

- Context cancellation MUST terminate or advance shutdown; it MUST NOT be interpreted as a content-policy decision.
- No user input accepted after shutdown begins may reach the guard, Hermes, proxy, Ollama, model context, or an application operation.
- A scanner timeout/panic/failure while the session is active MUST still fail closed.
- The stable scanner-failure reason MUST remain distinct from cancellation.

### 4.2 Signals

- First SIGINT MUST request graceful shutdown.
- SIGTERM MUST request graceful shutdown.
- Normal signal behavior MUST be restored when graceful shutdown begins.
- A second SIGINT during blocked cleanup MUST force termination or invoke an explicitly documented immediate-exit policy.
- Ordinary domain/application packages MUST NOT call `os.Exit`.

### 4.3 Protected intake

- Cancellation MUST stop pending secret intake within a bounded interval.
- Terminal echo/raw mode MUST be restored on every return path.
- Partial secret bytes MUST NOT be persisted or emitted.
- Cancellation MUST NOT place secret bytes in errors, logs, audit, receipts, stdout, or stderr.
- Aegis MUST not claim perfect process-memory zeroization.

### 4.4 Cleanup

- Every manager exit/failure path MUST invoke the same cleanup state machine.
- Cleanup MUST be bounded, idempotent, and race-safe.
- Cleanup MUST be safe after every partial startup stage.
- Cleanup failures MUST be visible through metadata-safe diagnostics and receipts.
- Cleanup MUST invalidate ephemeral route authority even when a child process fails to stop cleanly.

### 4.5 Runtime and model truthfulness

- No configured model and no certification are separate states.
- No cloud fallback or alternate model may be attempted.
- Degraded mode MUST advertise only implemented and currently available operations.
- No model may be downloaded, copied, or activated without explicit authenticated operator authorization.

## 5. Manager lifecycle state machine

The manager MUST own an explicit lifecycle state machine.

### 5.1 States

At minimum:

```text
created
preflighting
starting
active
closing
cleaning
closed
failed
```

State transitions MUST be monotonic. Once `closing` begins, no transition back to `active` is permitted.

### 5.2 End reasons

Use stable allowlisted end reasons at minimum:

```text
user_exit
terminal_eof
interrupt
termination
session_expired
session_revoked
runtime_failed
startup_failed
cleanup_failed
```

Arbitrary error text MUST NOT become an audit reason code.

### 5.3 Shutdown request arbitration

The first accepted shutdown request MUST select the authoritative end reason unless a higher-severity condition is required by policy. Later requests MAY accelerate cleanup but MUST NOT create duplicate receipts or repeated teardown.

### 5.4 Active-loop events

The active manager loop MUST select among:

- complete terminal input;
- context cancellation;
- SIGINT/SIGTERM lifecycle notification;
- session expiry;
- session revocation;
- Hermes/gateway failure;
- proxy failure;
- managed Ollama failure;
- explicit local exit.

It MUST NOT depend on a blocked synchronous scanner returning after cancellation.

## 6. Cancellation-aware terminal input

### 6.1 Ordinary lines

The terminal input abstraction MUST:

- return complete bounded lines;
- report EOF distinctly;
- report read failure distinctly;
- expose cancellation promptly;
- avoid input goroutine leaks after the command returns;
- avoid races with protected intake;
- preserve configured byte/rune limits;
- ensure only one component owns terminal reads at a time.

The exact implementation MAY be platform-specific, but its contract MUST be injectable and testable.

### 6.2 Post-cancellation behavior

After shutdown begins:

- no line may be sent to `Guard.Inspect`;
- no line may be treated as a secret finding;
- no line may be sent to Hermes;
- no directive may initiate a new operation;
- pending lines MAY be discarded without retention.

### 6.3 Local exit aliases

The following exact trimmed values MUST be consumed locally before ingress scanning:

```text
/quit
/exit
quit
exit
```

Strings merely containing `quit` or `exit` MUST remain ordinary messages.

The aliases MUST work in active and degraded modes.

### 6.4 EOF

Terminal EOF MUST select `terminal_eof`, run bounded cleanup, and emit one concise shutdown result. It MUST not silently bypass cleanup.

## 7. Signal behavior

### 7.1 First signal

On the first SIGINT or SIGTERM, Aegis MUST:

1. record `interrupt` or `termination`;
2. mark the manager closing;
3. print one concise shutdown message;
4. stop accepting turns;
5. cancel active work;
6. restore normal signal behavior;
7. begin bounded cleanup.

The output MUST not repeat once per blocked read or cleanup stage.

### 7.2 Second SIGINT

If graceful cleanup has not completed, a second SIGINT MUST provide a reliable forced termination path. It MUST not remain swallowed indefinitely.

The implementation MUST define whether the second signal uses restored OS default behavior or an explicit executable-boundary hard-exit path. The behavior MUST be tested in a subprocess, not merely by unit-invoking a handler.

### 7.3 Exit status

Signal and graceful user exits MUST use documented stable exit semantics. Cleanup failure MUST be distinguishable from a successful user exit.

## 8. Protected intake cancellation

### 8.1 Context ownership

The manager intake callback MUST use its supplied operation/session context. It MUST NOT discard that context and call an uninterruptible helper.

### 8.2 Terminal state

For every secret entry and confirmation read:

- capture restorable terminal state before modification;
- disable echo only for the bounded intake window;
- restore state before returning;
- restore state after cancellation, EOF, signal, mismatch, oversized input, panic boundary, and read failure;
- prevent another manager reader from consuming bytes concurrently.

### 8.3 Cancellation result

Canceled intake MUST:

- return a cancellation-class result;
- wipe partial buffers best-effort;
- perform no authority mutation;
- emit no secret-bearing error;
- return control to lifecycle cleanup rather than the conversational loop.

### 8.4 Forced cleanup

Cleanup MUST not wait indefinitely for protected input. If the input implementation cannot be interrupted safely on a supported OS, that OS MUST fail preflight or use an OS-specific bounded implementation. Documentation MUST not claim unsupported guarantees.

## 9. Unified bounded cleanup

### 9.1 Cleanup context

Cleanup MUST use an independent context with a configured finite deadline. It MUST not reuse the already-canceled session context and MUST not use unbounded `context.Background()` for the entire operation.

### 9.2 Ordered cleanup

The logical order MUST be:

1. mark lifecycle closing;
2. reject new turns and operations;
3. cancel active gateway/model work;
4. cancel confirmation/protected intake;
5. restore terminal state;
6. stop Hermes gracefully;
7. force-kill the Hermes process group after its bounded deadline;
8. close the inference proxy;
9. invalidate and erase ephemeral capability material;
10. unload the exact model;
11. stop managed Ollama if Aegis started it;
12. remove disposable runtime state;
13. close credential-authority handles;
14. finalize one metadata-only receipt;
15. report cleanup result.

A different physical ordering is permitted only where required to unblock an earlier operation, and MUST preserve the same security outcome.

### 9.3 Idempotence and concurrency

Each component close operation and the aggregate close operation MUST be safe when:

- called twice;
- called concurrently;
- called before startup completes;
- called after a child has exited;
- called after proxy failure;
- called with a nearly expired cleanup context.

Plain mutable booleans used by concurrent close paths MUST be replaced or protected appropriately.

### 9.4 Error aggregation

Cleanup errors MUST be collected without stopping subsequent security-critical cleanup attempts.

Safe output/receipt fields MAY include:

- cleanup status;
- component identifier;
- stable reason code;
- graceful versus forced termination;
- deadline exceeded.

They MUST NOT include:

- secret values;
- blocked prompt content;
- bearer capabilities;
- provider bodies;
- model request/response bodies;
- sensitive environment values.

### 9.5 Receipt finalization

Exactly one receipt MUST be finalized. It MUST contain the selected end reason and `complete` or `incomplete` cleanup status. Receipt failure MUST be surfaced without causing duplicate receipt attempts that can create inconsistent audit state.

## 10. Runtime readiness reason codes

The manager MUST distinguish at least:

```text
manager_model_absent
manager_model_digest_mismatch
manager_model_not_certified
manager_ollama_unavailable
manager_runtime_unsupported
manager_context_unsupported
manager_gateway_protocol_error
manager_session_expired
manager_session_revoked
manager_scanner_failed
```

Rules:

- Empty model configuration MUST return `manager_model_absent`.
- A configured model missing from the exact Ollama route MUST return the artifact/model absence reason.
- Exact digest mismatch MUST return `manager_model_digest_mismatch`.
- Missing, stale, or mismatched certification MUST return `manager_model_not_certified` or a more exact existing certification reason.
- Context cancellation MUST never return `manager_scanner_failed`.
- Startup audit MUST retain one exact allowlisted reason rather than only `manager_startup_failed`.

## 11. Truthful degraded mode

### 11.1 State-aware banner

The degraded banner MUST identify independently:

- authenticated principal;
- credential authority readiness;
- manager model configuration;
- exact model availability;
- certification validity;
- Hermes support;
- conversational route status;
- cloud fallback disabled;
- model switching disabled.

### 11.2 Accurate command advertisement

The manager MUST advertise only operations implemented in its local dispatcher and available under current prerequisites.

If slash mutations are not implemented, it MUST NOT advertise:

```text
/secret put
/secret rotate
/secret revoke
```

as in-manager commands.

It MAY state separately that equivalent top-level commands can be run from another shell, using exact implemented syntax.

### 11.3 Credential authority absent

When authority is absent:

- `/secret list` and `/secret show` MUST not be presented as ready;
- help/status MUST state the missing authority prerequisite;
- the operator MUST receive an exact deterministic next command or documented setup path;
- no secret value may be requested until authority readiness is established.

### 11.4 Help and status

`/help` MUST be state-aware and syntactically accurate.

`/status` MUST report at least:

- principal authentication;
- authority configured/absent/invalid;
- model configured/absent;
- model installed/unavailable;
- certification valid/absent/stale;
- Hermes supported/unsupported;
- inference active/degraded;
- local-only route;
- no-fallback policy.

Neither command may expose credential values, capabilities, or sensitive environment data.

## 12. Deterministic local-model onboarding

### 12.1 Goal

An authenticated operator MUST be able to progress from `manager_model_absent` to an exact configured and certified already-installed approved local model without manual YAML surgery.

### 12.2 Required command capabilities

Aegis MUST provide deterministic commands to:

1. list official traceable manager candidates;
2. inspect candidate metadata and license/source provenance;
3. discover which candidate artifacts are visible at an explicitly selected local Ollama route;
4. retrieve exact artifact identity/digest from local Ollama;
5. preview managed versus external-local routing implications;
6. select one exact candidate/model/digest;
7. select a certification destination below Aegis state;
8. preview the exact configuration mutation;
9. require authenticated operator confirmation;
10. write configuration atomically with restrictive permissions;
11. run explicit live certification;
12. inspect certification status and drift;
13. display the exact next step for every state.

Live certification MUST place the configured `manager.hermes.turn_timeout` around every gateway turn and principal authority expiry around the complete run. The first timeout, cancellation, expiry, protocol/transport error, invalid response, or failed case MUST abort the corpus and cleanup the disposable Hermes/proxy/model resources. Since late Hermes events are not prompt-correlated, an interrupted session MUST NOT be reused. Failure output MUST include the exact case and a stable metadata-safe reason, MUST provide the exact retry command, and MUST NOT publish a partial certification.

Command names SHOULD fit the existing constructor-built Cobra hierarchy. Business logic MUST live in shared services rather than command handlers.

### 12.3 No automatic download

Normal initialization, manager startup, readiness inspection, and certification MUST NOT:

- pull a model;
- update a mutable model tag;
- copy a model into another store;
- access model registries;
- enable cloud inference.

If no approved candidate is installed, Aegis MUST report that fact and the official candidate identities. Installation remains a separate explicitly authorized operator action.

### 12.4 Atomic configuration

Model configuration MUST:

- start from a securely owned valid config file;
- preserve unrelated fields and comments where the chosen config writer contract permits;
- validate exact model, digest, mode, endpoint, and certification path;
- show the complete security-relevant diff/preview;
- require confirmation;
- write through a same-directory restrictive temporary file;
- sync file/directory as required;
- atomically rename;
- leave no accepted partial state after cancellation or failure.

Malformed or insecure existing configuration MUST never be overwritten.

## 13. Managed versus external-local model visibility

### 13.1 Required explicit contract

The implementation MUST resolve the current mismatch between:

- an Aegis-private model directory used by managed Ollama; and
- documentation referring to an already-installed local model.

The implementation MUST choose and document one or both explicit paths:

#### External-local certification

- Connect only to an exact configured loopback Ollama origin.
- Inspect one already-installed approved artifact.
- Pin its exact digest.
- Do not stop the external daemon on exit.
- Continue to route Hermes through the Aegis proxy.

#### Managed-store import/install

- Require a separate explicit authenticated operation.
- Preview source and destination.
- Verify exact artifact identity.
- Never occur during ordinary startup or certification.
- Never be inferred from conversational text.

This remediation MUST NOT perform an actual model import or download without explicit authorization.

### 13.2 No ambient weakening

Managed mode MUST NOT silently point at arbitrary ambient model state merely to find a candidate. External-local mode MUST remain explicitly configured and documented as a weaker daemon-ownership boundary.

## 14. Credential-authority readiness

First run MUST offer a working passphrase-encrypted local authority path in a real terminal. It MAY also defer authority creation when the operator declines or deliberately selects externally delivered service custody.

Aegis MUST nevertheless provide deterministic readiness and setup guidance covering:

- authority database path;
- deployment identity;
- custody mode;
- KEK source;
- ownership and permissions;
- startup check;
- explicit `[Y/n]` confirmation before creation, with plan digest and artifact identity revalidated before mutation;
- no-echo passphrase creation/unlock when passphrase-encrypted local custody is selected;
- deterministic recovery from an incomplete undelivered systemd selection without silently downgrading.

The manager MUST not claim credential administration is ready until these checks pass.

Authority initialization remains separate from model certification and MUST not be delegated to a model.

## 15. Required tests

### 15.1 Lifecycle unit tests

- monotonic lifecycle transitions;
- first end reason wins under defined policy;
- duplicate close is idempotent;
- concurrent close is race-safe;
- cancellation does not become scanner failure;
- cleanup errors aggregate safely;
- exactly one receipt finalizes;
- stable reason-code classification.

### 15.2 Pseudo-terminal tests

On supported Unix platforms, tests MUST use an isolated subprocess and PTY to cover:

- SIGINT while waiting at prompt;
- process exits after first SIGINT;
- no post-SIGINT `manager_scanner_failed`;
- second SIGINT forces termination during intentionally blocked cleanup;
- SIGTERM;
- Ctrl-D/EOF;
- `/quit`;
- `/exit`;
- exact plain `quit`;
- exact plain `exit`;
- phrases containing exit/quit do not terminate;
- terminal echo restored after each path;
- one concise shutdown message;
- stable exit status.

### 15.3 Protected-intake tests

- cancellation during first secret entry;
- cancellation during confirmation;
- mismatch;
- EOF;
- oversized value;
- signal during raw/no-echo state;
- terminal restoration;
- no authority mutation after cancellation;
- partial buffer wipe best-effort;
- bounded cleanup despite blocked intake.

### 15.4 Runtime cleanup tests

Using fake Hermes/Ollama/proxy processes and services:

- cancellation during every startup stage;
- cancellation during active turn;
- Hermes process-group graceful/forced stop;
- proxy close and capability invalidation;
- exact model unload attempt;
- managed Ollama stop;
- disposable-state removal;
- authority handle close;
- receipt finalization;
- cleanup deadline;
- partial/incomplete cleanup reporting.

### 15.5 Degraded-mode tests

- no model returns `manager_model_absent`;
- absent certification returns `manager_model_not_certified`;
- digest mismatch remains distinct;
- startup audit records exact stable reason;
- help advertises only implemented commands;
- authority-absent state does not claim secret readiness;
- exit aliases work without model/authority;
- no cloud or alternate route is attempted.

### 15.6 Onboarding tests

With fake loopback Ollama services and isolated config/state:

- official candidate listing;
- installed-candidate discovery;
- exact digest capture;
- unapproved model rejection;
- non-loopback endpoint rejection;
- preview/decline performs no writes;
- confirmed atomic restrictive write;
- interrupted write recovery;
- malformed/insecure config preservation;
- certification path confinement;
- certification status and drift;
- no network registry access;
- no automatic model download/copy.

### 15.7 Random-canary cancellation test

Generate a new credential-shaped random canary, begin protected intake, cancel before completion, and assert plaintext absence from:

- Hermes input/output;
- proxy/Ollama captures;
- model context;
- stdout/stderr;
- logs/errors;
- audit;
- receipts;
- argv/environment captures;
- state/database metadata scans;
- temporary files;
- disposable runtime homes.

Tests MUST never use real credentials.

## 16. Hermeticity

Default tests MUST NOT:

- read or modify normal Aegis config/state;
- replace the installed Aegis executable;
- access GitHub or cloud inference;
- download or copy models;
- modify normal Hermes profiles;
- modify normal Ollama state;
- use real credentials;
- use the operator's signing keys;
- create commits, tags, releases, or remote changes.

Tests MUST use temporary HOME/XDG/config/state directories and injected fake services.

## 17. Verification requirements

The implementation session MUST run:

- focused package tests during development;
- shell syntax and release regression tests;
- Go formatting;
- Go build;
- full Go tests;
- race tests;
- Go vet;
- configured vulnerability scanning;
- targeted PTY lifecycle tests;
- random-canary cancellation test;
- `git diff --check`.

If runtime-recognized ad-hoc verification is required, create an OS-safe `/tmp/hermes-verify-*` script, execute the targeted changed behavior, remove it, and report it as ad-hoc rather than canonical suite evidence.

A real model/Hermes result MUST be claimed only when actually executed. Missing installed models or unsupported upstream behavior MUST be reported as exact external blockers.

## 18. Launch-asset review

Before completion, inspect every asset required by `AGENTS.md`:

- root `README.md`;
- `LICENSE`;
- `SECURITY.md`;
- `CONTRIBUTING.md`;
- `CODE_OF_CONDUCT.md`;
- `CHANGELOG.md`;
- threat model;
- architecture diagram;
- five-minute quickstart;
- no-key demonstration;
- terminal recording;
- release workflow/binaries/checksums material;
- contributor issue material.

Update affected assets. Report unaffected assets. Do not touch unrelated files merely to claim review.

Do not fabricate output, recordings, model results, certifications, releases, checksums, workflow results, or issue links.

## 19. Implementation sequence

### L0 — Reproduce and preserve

- Inspect working tree and preserve unrelated changes.
- Reproduce SIGINT/exit failure in an isolated PTY fixture.
- Trace signal, input, guard, intake, cleanup, and receipt ownership.

Gate: the regression fails against the old behavior without using real state.

### L1 — Lifecycle and cancellation-aware input

- Add explicit lifecycle/end reasons.
- Add cancellation-aware terminal input.
- Consume local exit aliases before guarding.
- Route EOF/cancellation/runtime failure into shutdown.

Gate: SIGINT, EOF, slash exit, and plain exact exit leave the REPL without scanner-failure output.

### L2 — Bounded cleanup and signal escalation

- Add independent cleanup deadline.
- Make closes idempotent/race-safe.
- Aggregate errors and finalize one receipt.
- Restore default signal behavior and test second-signal force exit.

Gate: every partial startup/active-stage cancellation completes or reports bounded incomplete cleanup.

### L3 — Protected-intake cancellation

- Pass context into intake.
- Implement bounded interruptible terminal read.
- Restore terminal and wipe partial values.

Gate: PTY canary cancellation proves terminal restoration and non-disclosure.

### L4 — Truthful degraded mode

- Correct reason codes.
- Make help/status state-aware.
- Remove unsupported command claims.
- Provide exact authority/model next steps.

Gate: a first-run principal can understand every unavailable state and exit normally without false capability claims.

### L5 — Deterministic model onboarding

- Add candidate/readiness/configuration/status commands.
- Resolve managed versus external-local visibility contract.
- Reuse explicit live certification.
- Preserve no-download behavior.

Gate: an already-installed fake approved artifact can be configured and certified end to end through deterministic commands and isolated tests.

### L6 — Full verification and launch assets

- Run tests and verification.
- Exercise documented safe workflows.
- Update affected launch assets.

Gate: every locally actionable Definition of Done item has real evidence.

## 20. Definition of Done

The remediation is complete only when:

### Shutdown

- First Ctrl-C begins and completes bounded graceful shutdown.
- Second Ctrl-C can force termination if cleanup hangs.
- SIGTERM uses the same bounded cleanup path.
- Ctrl-C never leaves the REPL alive with a canceled context.
- Cancellation never appears as `manager_scanner_failed`.
- EOF and every exact exit alias shut down cleanly.

### Protected intake

- Intake observes cancellation.
- Terminal mode is restored after every interruption.
- Partial canceled values are not stored or emitted.
- Cleanup does not wait indefinitely for intake.

### Cleanup

- One explicit state machine owns shutdown.
- Cleanup is bounded, idempotent, race-safe, and partial-startup-safe.
- Hermes, proxy, capability, model, managed Ollama, disposable state, authority handles, and receipt are all handled.
- Cleanup failures are reported safely.
- Exactly one receipt is finalized.

### Truthful state

- No model configured reports `manager_model_absent`.
- Missing/stale certification reports `manager_model_not_certified`.
- Degraded help/status advertise only usable operations.
- Authority absence is visible and not misrepresented as secret readiness.

### Onboarding

- An operator can inspect official candidates and already-installed local artifacts.
- Exact model/digest/mode/certification configuration can be previewed and written atomically.
- Managed versus external-local model visibility is explicit.
- Live certification remains explicit.
- No model is downloaded, copied, or activated automatically.
- No manual YAML surgery is required for the supported path.

### Evidence

- PTY signal/EOF/terminal-restoration tests pass.
- Protected-intake cancellation and random-canary tests pass.
- Runtime cleanup and race tests pass.
- Build, test, vet, vulnerability, and diff checks have real output.
- Documentation matches implemented behavior only.

## 21. `/loop` execution contract

A `/loop` session using this specification MUST:

1. run `cat specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md` and read the complete file;
2. read `AGENTS.md`, `specs/MVP.md`, and the parent manager/runtime specifications;
3. inspect status, diffs, history, implementation, and tests before editing;
4. preserve unrelated work;
5. work through L0–L6 in dependency order;
6. add regression tests before or with each behavior fix;
7. keep default tests hermetic;
8. preserve fail-closed scanner and secret boundaries;
9. avoid reducing the fix to one context check or an error-string change;
10. continue until every locally actionable Definition of Done requirement has real evidence;
11. report exact external prerequisites honestly;
12. perform the required launch-asset review;
13. not commit, tag, push, publish, run `make release`, download models, alter real Hermes/Ollama state, or change external systems without explicit operator authorization.

If an OS terminal primitive or supported Hermes/Ollama contract blocks a requirement, the session MUST produce a minimal reproducible finding, preserve the stronger boundary, document the exact blocker, and finish every unaffected requirement. It MUST NOT substitute unbounded reads, direct Hermes TTY attachment, cloud fallback, prompt-only restrictions, or fabricated results.
