# Pinentry Authority Passphrase Implementation Loop

Implement and verify production-quality pinentry-backed authority-passphrase intake throughout Aegis. This is an implementation task, not a design-only review. Continue until the real command paths work, all affected documentation is accurate, and the complete verification matrix passes.

Read `AGENTS.md` first. Then inspect fresh Git status and diffs, `internal/command/bootstrap.go`, `internal/command/secret.go`, `internal/command/manager.go`, `internal/command/root.go`, terminal/TUI ownership and synchronized-output code, onboarding state derivation, passphrase custody, credential-authority startup, cancellation helpers, PTY tests, and every affected launch asset. Preserve all existing user work. Do not revert, overwrite, commit, push, provision, activate, or touch the operator's real `~/.argis`, credential authority, GPG configuration, GPG agent, Hermes profiles, Ollama daemon, model store, desktop keyring, or credentials.

## Goal

Use the same class of protected desktop prompt familiar from GPG signing: Aegis invokes a compatible `pinentry` program and communicates with it over its documented Assuan-style stdin/stdout protocol. The authority passphrase is entered in pinentry UI, never in the Aegis terminal transcript, model conversation, command arguments, environment variables, ordinary TUI state, logs, audit records, configuration, or retained fixtures.

Apply this to both authority-passphrase operations:

1. **Create/set:** collect a new authority passphrase and confirm it through two protected pinentry requests before creating the encrypted KEK envelope.
2. **Unlock/verify:** collect the existing authority passphrase through pinentry whenever a configured `passphrase-file` authority must be opened or verified, including bootstrap resumption and principal-only credential administration.

Do not broaden this task silently into generic desktop intake for stored secret values, approval confirmations, provider keys, systemd credentials, or model input. Those paths have separate semantics and require a separate review.

## Current defect to remove

Do not merely hide the current error. Trace and fix its cause.

`runBootstrap` replaces Cobra stdout with `tui.SynchronizedWriter` before authority intake. `readAuthorityPassphrase` later requires `terminalPair(file, cmd.OutOrStdout())`, whose implementation requires concrete `*os.File` values for both streams. The wrapped stdout can therefore make protected intake fail with `a real terminal is required for no-echo authority passphrase intake` even when Aegis was launched from a real terminal.

Pinentry must make authority-passphrase intake possible without terminal-backed stdin/stdout when a usable protected desktop pinentry is available. A secure no-echo terminal path must remain as a fallback and must test terminal capabilities without depending incorrectly on a presentation wrapper's concrete Go type. Do not weaken real-terminal requirements for ordinary bootstrap menus, the manager TUI, reset, migration, or other terminal-owned workflows merely because authority secret intake can use pinentry.

## Required outcomes

### 1. One narrow protected-passphrase abstraction

Create a small injected abstraction for authority-passphrase acquisition rather than scattering process execution through Cobra handlers. Keep command handlers thin and make the implementation independently testable.

The abstraction must distinguish at least:

- create/confirm versus unlock/verify intent;
- success with sensitive bytes;
- explicit user cancellation;
- pinentry unavailable or unsupported;
- pinentry protocol/launch failure;
- timeout or context cancellation;
- policy failure such as too short, too long, or confirmation mismatch.

Use typed errors or stable predicates where needed. Do not branch on arbitrary localized pinentry prose. Keep executable/process construction injectable so tests never invoke the operator's real GUI.

Use one shared authority-passphrase entry point at every applicable call site. Audit all `readAuthorityPassphrase` callers and all `passphrase-file` custodian-loading paths; do not fix only first-run setup.

### 2. Pinentry discovery and selection

Use a compatible local `pinentry` executable. Define and document a deterministic selection policy.

At minimum:

- prefer an explicit, validated Aegis configuration or narrowly scoped process option if the project already has an appropriate operational configuration surface;
- otherwise resolve the conventional `pinentry` executable;
- do not edit or depend on the operator's `gpg-agent.conf`;
- do not invoke `gpg`, `gpg-agent`, `gpg-connect-agent`, shell scripts, or a shell command line merely to display the prompt;
- do not assume one desktop implementation such as GNOME, Qt, GTK, curses, or macOS pinentry;
- validate an explicitly configured executable as an executable file and reject ambiguous or malformed values;
- execute directly with `exec.CommandContext`, never through `sh -c`;
- pass no passphrase, key, authority path, or sensitive value in argv or environment;
- do not persist pinentry selection as credential material.

If adding configuration, keep it operational and strictly typed, define secure defaults and validation, update example configuration where applicable, and preserve compatibility for existing valid configurations. Do not add a dependency merely to parse the small line protocol.

### 3. Correct bounded pinentry protocol

Implement the required subset of the documented pinentry Assuan-style protocol directly and defensively.

The client must:

- start the child with dedicated stdin/stdout pipes and a separately handled bounded stderr path;
- require and parse the initial `OK` greeting before sending requests;
- set a clear Aegis title, description, prompt, and context-appropriate action text using protocol commands such as `SETTITLE`, `SETDESC`, `SETPROMPT`, and `SETOK` only where supported;
- use `GETPIN` for protected passphrase retrieval;
- encode command parameters correctly, including `%`, CR, LF, and other protocol-sensitive bytes;
- decode percent-escaped `D` data correctly and reject malformed escapes;
- support protocol-valid data framing without accepting unsolicited, duplicate, oversized, or out-of-order responses;
- enforce the existing authority passphrase bounds before returning sensitive bytes: minimum 12 bytes and the actual maximum enforced by the credential layer;
- bound every line, total response, stderr capture, number of protocol records, and process lifetime;
- reject NUL and unsafe control content where the protocol or credential format cannot represent it safely;
- send `BYE` or otherwise close cleanly when possible without allowing cleanup to hang;
- reap the child on every exit;
- honor context cancellation promptly and terminate the child process tree as appropriate for supported platforms;
- return metadata-safe errors that never contain `D` payloads, decoded passphrases, raw child output, environment values, or secret-derived fingerprints.

Do not hard-code one localized cancellation message. Correctly distinguish an explicit pinentry cancel/close result from executable absence and protocol failure using protocol status and process state. User cancellation is final for that operation and must not silently open a second terminal prompt.

### 4. Safe process environment and UI context

Give pinentry only the environment required to reach the operator's protected desktop/session and render correctly. Do not blindly inherit unrelated provider credentials, API tokens, Hermes variables, proxy variables, or secret-bearing application environment.

Review and justify an allowlist that may include platform/session values such as `PATH`, `HOME`, locale, `DISPLAY`, `WAYLAND_DISPLAY`, `XAUTHORITY`, `XDG_RUNTIME_DIR`, and the desktop/session bus address when genuinely required. Preserve only what tested pinentry implementations need. Do not log the resulting environment.

Where available, provide non-secret terminal/display metadata through supported protocol options without making a TTY mandatory for GUI pinentry. Do not let model/runtime text supply title, description, prompt, executable, display, or protocol fields. All pinentry chrome must be static or derived from authenticated Aegis state through bounded sanitization.

The UI must identify:

- Aegis as the requester;
- whether this is creating/confirming or unlocking the credential authority;
- the 12-byte minimum in understandable wording;
- that the passphrase is not persisted;
- for creation, that losing it makes the authority unrecoverable without separate recovery.

Do not display the passphrase, KEK, database contents, credential values, model output, or untrusted text.

### 5. Creation and confirmation

For a new passphrase-encrypted authority:

1. retain the exact authority plan and separate `[Y/n]` mutation confirmation;
2. after plan authorization, request the new passphrase through pinentry;
3. enforce the 12-byte minimum and actual maximum before confirmation;
4. request confirmation through a second fresh protected pinentry interaction;
5. compare exact bytes in constant-time where practical for this local check, wipe the confirmation buffer, and reject mismatch without mutation;
6. provide a bounded retry loop for recoverable policy failures or mismatch, with clear metadata-only feedback and an explicit cancel path;
7. never create the KEK envelope, database, config mutation, or success audit event until one complete matching pair passes;
8. retain digest revalidation, atomic publication, cleanup, and post-create sentinel verification.

A pinentry cancellation, timeout, crash, malformed response, or exhausted retry bound must perform no authority creation and must leave onboarding safely resumable. Never treat prompt display or successful first entry as mutation authorization.

### 6. Unlock and verification

For an existing `passphrase-file` authority:

- request the passphrase through pinentry whenever the authority must be loaded in the current process;
- verify it only by opening the configured encrypted KEK/custodian and validating the deployment-bound authority path already used by Aegis;
- do not invent or persist a passphrase hash, verifier, cache file, keyring record, or GPG secret;
- never claim unlock before the existing encrypted envelope and sentinel checks pass;
- on an authentication/decryption mismatch, wipe the failed value and offer a bounded retry through a new pinentry interaction;
- distinguish wrong passphrase from corrupt, insecure, drifted, missing, or structurally invalid authority artifacts; only wrong-passphrase outcomes are retryable;
- do not loop indefinitely on corruption, filesystem errors, policy denial, context cancellation, or pinentry protocol failure;
- keep a successfully unlocked passphrase or KEK only for the existing process-local lifetime, with existing best-effort wiping/close behavior;
- do not add a background daemon or cross-process passphrase cache in this task.

Ensure bootstrap resumption, `secret` administration, and manager-related authority opening use consistent behavior. Avoid repeated prompts inside one operation when one process-local successful unlock can be safely reused under the existing lifecycle.

### 7. Fallback and failure policy

Define this order explicitly:

1. Attempt configured/discovered protected pinentry when available.
2. If pinentry is genuinely unavailable or cannot initialize **before any user interaction**, fall back to the existing no-echo TTY path only when suitable terminal-backed input and diagnostic output are available.
3. If pinentry reports user cancellation, do not fall back.
4. If pinentry accepted or may have accepted secret input and then fails, fail closed rather than unexpectedly requesting the passphrase in another surface.
5. If neither protected mechanism is available, return an actionable error naming the requirement without suggesting argv, environment variables, chat, or an unprotected pipe.

Do not silently downgrade to echoed stdin. Do not add a plaintext `--passphrase`, environment-variable, clipboard, temporary-file, or model/TUI-composer path. Existing explicit protected-stdin support for generic secret values is outside this authority-passphrase change and must not become an authority unlock bypass.

Correct the TTY fallback so synchronized output wrappers do not create a false negative. Preserve echo disable/restore guarantees and do not write passphrase bytes through the synchronized presentation writer.

### 8. Non-TTY command semantics

Review command-level TTY gates separately from secret intake.

- `aegis init` and bare `aegis` still contain ordinary interactive selections and mutation confirmations, so pinentry alone does not make the entire bootstrap safe for arbitrary non-interactive execution.
- Principal-only subcommands that need only authority unlock plus deterministic non-secret output may use GUI pinentry without terminal stdin if their own semantics do not otherwise require terminal interaction.
- Commands requiring ordinary approval, manager composer ownership, reset confirmation, migration confirmation, or terminal state must retain their existing TTY requirements unless a complete separate protected/noninteractive interaction design is implemented and tested.
- Never reinterpret piped model/chat input as authority-passphrase bytes.

Make the resulting boundaries explicit in help and errors. The original reported real-terminal failure must be fixed, but do not use it as justification to weaken unrelated non-TTY default-deny behavior.

### 9. Secret lifetime and observability

Preserve or strengthen all existing secret handling:

- sensitive values use bounded `[]byte`, not immutable strings, wherever controllable;
- wipe first entry, confirmation, decoded protocol data, partial buffers, and temporary copies on every success and failure path;
- never include values in `fmt` errors, `%q`, structured logs, audit metadata, TUI events, traces, test names, snapshots, or panic output;
- never retain real passphrases in repository fixtures;
- avoid `CombinedOutput` for pinentry because stdout contains protocol secret data;
- keep stderr bounded and redact it to stable reason categories rather than returning arbitrary helper prose;
- disable or avoid core-dump claims unless actually enforced; document Go/runtime/OS zeroization limitations accurately;
- pinentry protects display/intake routing, not a compromised account, desktop session, pinentry executable, process memory, root, or kernel.

Do not claim that GUI pinentry is a hardware-backed key store, desktop keyring, GPG-agent cache, sandbox, or stronger cryptographic custody. It changes the protected input surface; Argon2id plus XChaCha20-Poly1305 custody remains unchanged.

## Required tests

Add hermetic tests with an injected fake pinentry executable/process. Never open the operator's real pinentry UI during automated tests. Use generated canaries and assert their absence from all captures.

At minimum prove:

1. successful greeting, command exchange, escaped `D` decoding, `GETPIN`, and clean child exit;
2. create mode performs two distinct protected requests and returns only an exact matching value;
3. unlock mode performs one request per attempt;
4. 11 ASCII bytes fail, 12 ASCII bytes pass, multibyte input follows the existing byte policy, empty and oversized values fail;
5. mismatch wipes both values, mutates nothing, and retries only within the configured bound;
6. wrong existing passphrase retries, while corrupt envelope, wrong deployment context, insecure mode, missing file, and malformed authority do not;
7. explicit pinentry cancel is non-mutating and never falls back to TTY;
8. executable-not-found and pre-interaction startup failure fall back only when a real no-echo TTY is available;
9. a post-interaction protocol failure fails closed without a second-surface prompt;
10. no pinentry plus no TTY returns one actionable metadata-safe error;
11. malformed greeting, unknown records, malformed percent escapes, duplicate/out-of-order data, oversized lines/data/stderr, early EOF, nonzero exit, and protocol injection are rejected;
12. context cancellation and timeout terminate/reap the helper without goroutine, descriptor, or child leaks;
13. environment allowlisting includes required fake display/session values but excludes generated provider/API/token canaries;
14. title/description/prompt fields cannot be influenced by model output or untrusted configuration text;
15. synchronized bootstrap output no longer causes false `terminalPair` rejection on the real PTY fallback;
16. terminal echo and complete terminal state are restored on success, short input, mismatch, EOF, cancellation, signal, and fallback failure;
17. bootstrap creation still requires exact plan confirmation and performs no config/KEK/database/audit mutation before matching passphrase confirmation;
18. successful creation writes only expected mode-`0600` encrypted authority artifacts under isolated state, verifies the sentinel, and never persists the passphrase;
19. bootstrap resumption and `secret` administration unlock through the shared path and verify the existing authority;
20. passphrase canaries are absent from stdout, stderr, returned errors, logs, audit, configuration, state metadata, TUI events, environment, process arguments, and retained test files;
21. ordinary bare non-TTY bootstrap, manager TUI, reset, and migration behavior remains fail-closed where still required;
22. race-enabled concurrent command tests do not mix protocol records, prompts, passphrase buffers, or synchronized presentation output.

Use temporary HOME/config/state/checkpoint/authority paths, fake pinentry programs, fake display/session variables, fake Hermes/Ollama where needed, and no real credentials. On Linux, add real PTY subprocess coverage for the terminal fallback and fake desktop-helper subprocess coverage for non-TTY pinentry. Make platform-specific behavior explicit with build tags or capability tests rather than pretending unsupported GUI environments work.

## Documentation and launch assets

Update every affected source of truth after implementation, not before behavior exists:

- root `README.md`;
- `SECURITY.md`;
- `CHANGELOG.md` under Unreleased;
- `docs/CREDENTIAL_AUTHORITY_SETUP.md`;
- `docs/QUICKSTART.md`;
- `docs/THREAT_MODEL.md` trust-boundary diagram and abuse-case table;
- `docs/ARCHITECTURE.md` diagram and credential-authority narrative;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md`;
- relevant specifications/research whose statement that intake is TTY-only becomes stale;
- example configuration if a pinentry option is added;
- contributor issue material if cross-platform pinentry support remains deferred.

Document:

- pinentry-first authority passphrase creation and unlock;
- secure no-echo terminal fallback;
- deterministic executable selection and troubleshooting;
- desktop-session prerequisites such as display/session-bus availability where applicable;
- cancellation and retry behavior;
- no persistence or GPG-agent integration;
- process-local unlock lifetime;
- residual local-account/root/process-memory risks;
- how headless/systemd deployments should use systemd credential custody rather than pretending GUI pinentry is available.

Perform and report the full launch-asset impact review required by `AGENTS.md`, including license, contributing/code-of-conduct, threat model, architecture diagram, five-minute quickstart, no-key demonstration, terminal recording, release binaries/checksums, and focused contributor issues. Review unaffected assets explicitly; do not touch them merely to claim review. Do not fabricate a GUI capture, release, checksum, issue, or verification result.

## Architecture constraints

- Identity, authority-plan confirmation, and mutation authorization remain outside pinentry. Pinentry collects only protected bytes.
- The model and Hermes never choose the helper, supply UI text, receive protocol traffic, or learn success beyond metadata-safe deterministic state.
- Keep Cobra constructor-built and dependencies injected; do not add package-level mutable command or pinentry state.
- Use `context` cancellation throughout and direct child execution with bounded pipes.
- Preserve strict configuration decode/validation, digest binding, atomic publication, file modes, deployment sentinel verification, and audit provenance.
- Do not add a daemon, desktop keyring, D-Bus secret-service dependency, GPG-agent dependency, passphrase cache, recovery mechanism, key rotation, or custody redesign as drive-by scope.
- Do not add shell, arbitrary executable, plugin, MCP, profile, network, model, or provisioning authority.
- Match existing Go style and avoid broad refactors or reformatting.

## Verification and stopping condition

After implementation:

- format changed Go files;
- run focused pinentry protocol, authority creation/unlock, bootstrap, secret-command, cancellation, and PTY tests;
- run the relevant tests with `-race`;
- run `go test ./...`;
- run `go test -race ./...` if the environment supports the repository's established full race run;
- run `go vet ./...`;
- build `./cmd/aegis`;
- run `git diff --check`;
- exercise a fake-pinentry create and unlock end to end through isolated real CLI subprocesses;
- exercise real no-echo PTY fallback with isolated temporary authority artifacts;
- verify non-TTY plus fake GUI pinentry where the command has no other interactive requirement;
- confirm generated passphrase canaries are absent from captured output and persisted artifacts;
- inspect final status and diff for unintended changes.

A real installed pinentry may be used only for a non-secret, isolated manual smoke test after explicit operator consent; automated verification must use a fake. Never initialize, unlock, probe, or modify the operator's actual `~/.argis` authority as part of this loop.

Do not stop at a plan, interface, protocol stub, fake-only unit test, or documentation claim. Continue until creation and verification/unlock work through real isolated command paths, fallback and cancellation are exercised, all applicable call sites use the shared implementation, documentation matches tested behavior, and the launch-asset review is complete. If blocked, report the exact blocker and evidence rather than weakening requirements or claiming success.

The final report must include the selection/fallback policy, protocol and process-safety decisions, exact files changed, tests/builds with real results, isolated end-to-end evidence, launch assets reviewed, residual platform/security limitations, and confirmation that no operator authority, GPG state, desktop keyring, Hermes profile, model store, or external system was modified.
