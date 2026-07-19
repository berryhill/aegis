# Bootstrap UX Corrections Implementation Loop

Implement and verify a complete consistency pass over Aegis's standard-terminal bootstrap UX. This is an implementation task, not a design-only review.

Read `AGENTS.md` first. Then inspect fresh Git status and diffs, `internal/command/bootstrap.go`, terminal-input helpers, onboarding state derivation, credential custody and passphrase code, model discovery/configuration, existing tests, and every affected launch asset. Preserve all existing uncommitted work. Do not revert, overwrite, commit, push, provision, activate, or touch the operator's real `~/.argis`, credential authority, Hermes profiles, Ollama daemon, model store, or credentials.

## Required outcomes

### 1. Eliminate meaningless one-option menus everywhere

Audit every bootstrap menu and closed-choice prompt, not only model selection. Apply one consistent rule:

- Zero actionable choices: render the exact reason and safe next action; do not prompt for a selection.
- Exactly one actionable choice: render it without a numeric menu label, state visibly that it is the only applicable choice, and continue without consuming terminal input for a redundant selection.
- Two or more actionable choices: render a numbered menu, identify any safe default truthfully, and validate the selection strictly.
- Unavailable choices may be shown only when their disabled state and prerequisite are explicit; they must not be accepted as if usable.
- Cancellation and EOF must remain safe and must not mutate state.

Selection is not authorization. Removing a redundant selection prompt must never remove the separate confirmation for a consequential mutation, network action, certification run, destructive action, or authority change.

For installed models specifically:

- If exactly one approved installed candidate exists, show its complete provenance and artifact metadata without `[1]`, visibly auto-select it, and proceed directly to the exact digest-bound configuration preview and separate `[Y/n]` mutation confirmation.
- Do not consume input intended for the following confirmation.
- If multiple approved installed candidates exist, number them and require an explicit selection with no default.
- If none exist, retain the separately authorized closed-registry download path and its no-mutation decline behavior.

### 2. Make credential-custody selection truthful and concise

Keep the supported custody choices and security boundary accurate:

- passphrase-encrypted local KEK is the working interactive default;
- systemd service credential is advanced and selectable only when its prerequisite can be truthfully completed or persisted as an explicit resumable external prerequisite;
- plaintext host file is development-only and visibly weaker;
- exit/cancel performs no mutation.

Do not describe `~/.argis` as a typo: it is the canonical literal local home in the current implementation. Render exact resolved paths and configuration digest changes before mutation.

Do not add confirmations merely to acknowledge informational text. Keep one clear custody selection when multiple actionable custody modes exist, then one exact plan preview and one conventional confirmation for the actual authority/configuration mutation.

### 3. Explain and enforce the passphrase requirement before intake

Before the first no-echo passphrase prompt, state the actual policy in plain language:

- minimum: 12 bytes;
- for plain ASCII, 12 characters equal 12 bytes;
- non-ASCII characters may occupy multiple bytes;
- the passphrase is entered twice with echo disabled;
- it is never written to disk;
- losing it makes the encrypted authority unrecoverable without a separate recovery mechanism.

Enforce the existing 12-byte policy at the protected-input boundary. Reject a too-short value before asking for confirmation, return a concise actionable error, never print the value, and preserve terminal restoration and best-effort secret wiping on success, mismatch, short input, EOF, cancellation, signal, and failure. Do not silently raise or weaken the existing minimum.

### 4. Apply a coherent prompt grammar

Use consistent terminal wording:

- `[Y/n]`: Enter accepts the displayed default.
- `[y/N]`: Enter cancels.
- Numbered selection prompts only when at least two actionable choices exist.
- Clearly distinguish `selected`, `previewed`, `confirmed`, `applied`, and `verified`.
- Never imply that preview or selection authorized mutation.
- Keep output understandable without color and on narrow terminals.
- Keep stdout for command results and stderr for diagnostics according to existing command conventions.

Avoid generic password advice, duplicated warnings, generated copy/paste confirmation phrases, and unexplained byte/character terminology.

## Architecture constraints

- Keep Cobra commands thin and security decisions deterministic.
- Reuse the existing onboarding, credential initialization, model discovery, preview, apply, and audit services.
- Do not weaken authentication, digest binding, drift checks, exact artifact identity, atomic publication, file modes, default-deny behavior, or audit provenance.
- Do not introduce model authority, cloud fallback, model switching, real downloads in tests, or a second onboarding state machine.
- Prefer a small shared menu/choice helper only if it genuinely removes duplication without obscuring security-sensitive call sites.
- Match existing Go style; no drive-by refactors or broad reformatting.

## Required tests

Add or update focused hermetic tests proving:

1. zero, one, and multiple actionable-choice behavior;
2. a sole option has no numeric label and no selection prompt;
3. sole-option handling consumes no input intended for the next confirmation;
4. multiple options require strict explicit selection;
5. cancellation, blank input, invalid selection, EOF, and context cancellation are non-mutating;
6. custody options and disabled/prerequisite states are truthful;
7. the 12-byte requirement is displayed before protected intake;
8. 11 ASCII bytes fail before confirmation, 12 ASCII bytes pass to confirmation, multibyte input is measured in bytes according to the actual policy, and mismatches fail safely;
9. passphrases never appear in stdout, stderr, errors, audit output, configuration, or ordinary retained fixtures;
10. terminal echo/state restoration holds across all protected-input exits;
11. exact plan confirmation, digest revalidation, and atomic apply behavior remain intact;
12. model zero/one/multiple discovery paths retain correct no-download and exact-confirmation behavior.

Use isolated temporary HOME/config/state/checkpoint/authority paths, fake loopback Ollama, fake Hermes where needed, and no real credentials or model downloads. Assert semantic state and side effects, not snapshots alone.

## Documentation and launch assets

Update only affected sources of truth, including README, quickstart, credential-authority setup, security guidance, threat model, architecture, terminal UX implementation prompt, changelog, demo/recording instructions, and contributor issue material. Preserve the distinction between a no-default registry and visible automatic handling of one exact installed candidate.

Perform and report the complete launch-asset impact review required by `AGENTS.md`. Do not fabricate recordings, releases, checksums, issues, certification output, or command results.

## Verification and stopping condition

After implementation:

- format changed Go files;
- run focused bootstrap, protected-input, credential-authority, and model-selection tests;
- run relevant PTY/race tests;
- run `go test ./...`;
- run `go vet ./...`;
- build `./cmd/aegis`;
- run `git diff --check`;
- exercise the corrected zero/one/multiple menu behavior and passphrase boundary through isolated real command paths or PTY fixtures;
- inspect final status and diff for unintended changes.

If no canonical command covers a changed behavior, create an OS-safe temporary `/tmp/hermes-verify-*` script, run it, remove it, and label its result as targeted ad-hoc verification rather than suite-wide proof.

Do not stop at a plan, mockup, helper stub, or unit test. Continue until the behavior is implemented, exercised, documented, and verified. If blocked, report the exact blocker and evidence instead of weakening requirements or claiming success.

The final report must include UX decisions, exact files changed, tests and builds with real results, isolated end-to-end evidence, launch assets reviewed, residual limitations, and confirmation that no operator state or external system was modified.
