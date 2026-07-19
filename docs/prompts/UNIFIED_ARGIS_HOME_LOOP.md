# Unified `~/.argis` Home and Safe Legacy Reset Implementation Loop

Implement a complete audit and production-quality correction of Aegis's local per-operator filesystem layout.

The canonical directory name is literally:

```text
~/.argis
```

This spelling is intentional. Do not change it to `~/.aegis`, do not treat it as a typo, and do not preserve XDG paths as the default local layout.

Read `AGENTS.md` first. Inspect the complete current Git status and diff before editing. Preserve all existing user and worker changes, including any in-progress terminal-onboarding, systemd-authority, reset, documentation, or test changes. Do not revert, overwrite, commit, or broadly reformat unrelated work.

## Reported production failure

The current released local layout and reset policy contradict each other:

```text
Config:      ~/.config/aegis/aegis.yaml
State:       ~/.local/state/aegis
Checkpoints: ~/.local/state/aegis-checkpoints or a child selected during initialization
```

On the operator's machine:

```text
/home/javi/.local/state mode=0775 owner=javi:javi
/home/javi/.local/state/aegis mode=0700 owner=javi:javi
```

`aegis reset` fails with:

```text
aegis: reset_denied: unsafe parent /home/javi/.local/state: artifact is writable by group or others
```

The current reset path validator applies artifact ownership and mode requirements to every path component from the operator home to the target. The group-writable external XDG parent is therefore rejected even though Aegis itself selected the child as its default state path.

Do not “fix” this by blindly removing parent, ownership, symlink, hard-link, race, repository, or scope checks. Correct the path model, deletion-authority model, legacy handling, and tests together.

## Required canonical local layout

Create one centralized typed resolver for the per-operator local Aegis layout. The default layout must be derived from the authenticated operator's home and must be:

```text
~/.argis/                                      mode 0700
~/.argis/aegis.yaml                           mode 0600
~/.argis/state/                               mode 0700
~/.argis/state/audit-checkpoints/
~/.argis/state/credentials/
~/.argis/state/credentials/authority.db
~/.argis/state/credentials/authority.kek
~/.argis/state/manager/certifications/
~/.argis/state/manager/ollama-models/
~/.argis/state/runtime/
~/.argis/state/charters/
~/.argis/state/plans/
~/.argis/state/approvals/
~/.argis/state/mandates/
~/.argis/state/sessions/
~/.argis/state/receipts/
~/.argis/state/provisioned/
```

Use only the directories actually required by implemented behavior; do not create empty directories speculatively. Every Aegis-owned local artifact must resolve under the canonical root unless an explicit supported deployment override is supplied.

The default bare local CLI must not place Aegis-owned configuration, state, checkpoints, credentials, temporary configuration files, manager certifications, managed Ollama artifacts, runtime homes, sockets, logs, receipts, or audit material under:

```text
~/.config/aegis
~/.local/state/aegis
~/.local/state/aegis-checkpoints
~/.cache/aegis
/tmp/aegis-*
```

Atomic temporary files must be created adjacent to their canonical destination or in a dedicated mode-0700 directory below `~/.argis/state/runtime` as appropriate.

Normal Hermes installations and profiles, operator-managed Ollama daemons and model stores, systemd-delivered credentials, external TLS material, the Aegis executable, source checkouts, and genuinely external systems are not Aegis-owned local data and must remain outside the canonical root and untouched.

Do not weaken or erase the distinction between:

- per-operator local CLI defaults under `~/.argis`;
- explicit administrator-selected paths;
- system deployments such as `/etc/aegis` and `/var/lib/aegis`;
- external operator-managed assets.

Explicit `--config`, `--state-dir`, environment overrides, and system-deployment paths may remain supported where already part of the product contract, but they must be treated as explicit deployment configuration, never as the local default. Validate them according to their deployment threat model. Do not silently redirect an explicit path.

## Single source of truth

Eliminate split path derivation. At present, configuration uses `os.UserConfigDir`, state is independently hardcoded under `~/.local/state`, checkpoint behavior differs between defaults and initialization, and tests routinely bypass the real defaults.

Introduce one focused package or cohesive API that returns a typed layout, including at minimum:

- canonical root;
- configuration file;
- state root;
- audit checkpoint root;
- credential-authority database;
- passphrase-encrypted local KEK;
- host-file KEK;
- manager certification root;
- managed model store;
- runtime root.

Requirements:

- derive the home through an injectable resolver for tests;
- return clean absolute paths;
- reject an empty, relative, root, malformed, symlinked, ambiguously owned, or unsafe home/layout;
- never rely on string-prefix containment;
- avoid package-level mutable state;
- keep config loading separate from layout resolution;
- ensure initialization, onboarding, reset, manager, model configuration, certification, runtime creation, authority setup, audit, examples, tests, and documentation all consume the same layout contract;
- use `~/.argis` only in user-facing prose; use resolved absolute paths for filesystem operations.

## Secure directory creation

For a fresh local installation:

- create `~/.argis` with mode 0700;
- verify ownership matches the authenticated effective operator;
- reject symlink components;
- reject pre-existing non-directory components;
- reject an existing canonical root writable by group or others;
- create children with mode 0700;
- write sensitive regular files with mode 0600;
- use atomic publication and directory syncing where required;
- revalidate identities immediately before consequential publication;
- restore or leave a valid resumable state after interruption.

The home directory itself need not have mode 0700, but it must be the authenticated operator's real owned home and must not be a symlink-substituted deletion authority. Define and test the exact trust rule rather than reusing an artifact error message for every ancestor.

## Legacy layout discovery

Existing installations must not be silently stranded when the default changes.

Recognize only the exact known legacy local defaults:

```text
~/.config/aegis/aegis.yaml
~/.local/state/aegis
~/.local/state/aegis-checkpoints
```

Also account for the initializer variant where checkpoints are below the configured legacy state root.

When no explicit config path is supplied, inspect canonical and recognized legacy locations before initialization.

Required states:

1. Neither canonical nor legacy exists
   - fresh bootstrap uses only `~/.argis`.

2. Canonical exists and legacy is absent
   - use and validate canonical state.

3. Legacy exists and canonical is absent
   - never begin a second fresh installation;
   - report `legacy-layout-detected`;
   - offer an authenticated deterministic migration in an interactive terminal;
   - allow a safe legacy reset without requiring migration first.

4. Canonical and legacy both exist
   - fail closed as ambiguous;
   - do not merge, union, overwrite, or pick the newest;
   - show exact inspection and recovery commands.

5. Explicit `--config` is supplied
   - operate only on the explicit deployment after strict validation;
   - do not infer deletion or migration authority from unrelated canonical or legacy files.

6. Environment-only configuration exists
   - preserve the existing rule that environment variables are not deletion authority;
   - report exact variables that must be explicitly handled without printing secrets.

Legacy detection must be artifact-derived and read-only. It must not parse malformed configuration as authority to delete arbitrary configured paths.

## Authenticated legacy migration

Implement a deterministic migration planner and applier for recognized legacy local layouts. If a migration command already exists, extend it; otherwise add a focused command such as:

```text
aegis migrate-layout
```

Do not overload ordinary discussion or startup inspection as authorization to mutate data.

Migration must:

- require a real terminal;
- authenticate the current OS principal;
- verify exact ownership and supported legacy paths;
- inventory recognized Aegis artifacts without following symlinks;
- reject unknown files rather than deleting or absorbing them;
- show exact source and destination paths;
- show which credentials, audit records, certifications, and state artifacts move;
- show external assets that remain untouched;
- compute a canonical digest over the exact plan;
- render `Apply this digest-bound migration plan? [Y/n]` with apply as the clearly displayed default;
- accept Enter alone as confirmation of the displayed default, and also accept an explicit `y` or `yes` without requiring the operator to retype paths or ceremonial phrases;
- treat `n`, `no`, EOF, Ctrl+C, non-TTY input, and every unrecognized answer as cancellation with no mutation;
- bind the Enter/default confirmation to the exact displayed plan digest and reject any plan, path, identity, or artifact change before application;
- reauthenticate and revalidate source/destination identities before application;
- create the canonical root securely;
- support cross-filesystem migration using verified copy, fsync, publication, and source cleanup rather than assuming rename works;
- never overwrite an existing canonical artifact;
- keep source data intact until the complete destination has been verified;
- preserve file modes and required metadata without preserving unsafe permissions;
- rewrite only Aegis-owned path fields to their canonical destinations;
- verify the migrated configuration and all security-critical artifacts before source deletion;
- preserve the exact model artifact binding and certification provenance;
- never copy, reveal, log, or render credential values;
- make interruption resumable or fail closed with an exact recovery path;
- emit authoritative metadata-only audit evidence where an operational audit authority exists;
- finish with canonical readiness inspection.

If safe automatic migration of a particular recognized artifact cannot be proven, stop before mutation and report a precise manual recovery procedure. Never weaken path or credential rules to force migration success.

## Safe reset for canonical and legacy layouts

`aegis reset` must work for the new canonical layout and for an exact recognized legacy installation.

Canonical reset:

- authorize only validated Aegis-owned artifacts below `~/.argis`;
- preserve managed Ollama model artifacts if that is the retained product contract, or explicitly change and document the contract after reviewing existing requirements;
- preserve all external assets;
- require explicit `yes` at a default-deny `[y/N]` terminal confirmation;
- retain plan digest, identity revalidation, no-follow behavior, bounded inventory, unknown-artifact denial, and postcondition verification;
- return the local installation to artifact-derived `uninitialized` state.

Legacy reset:

- must not require the operator to chmod or weaken security on `~/.local/state`;
- must not treat an external XDG parent as an Aegis-owned artifact;
- must not recursively delete an arbitrary configured directory;
- must remain safe if a group-writable ancestor can rename or substitute path entries;
- must use descriptor-anchored, no-follow operations and identity checks where pathname re-resolution would introduce a race;
- may securely empty a validated Aegis-owned legacy child while intentionally retaining that empty child directory if deleting the child entry from an unsafe external parent cannot be authorized safely;
- must remove the validated legacy configuration only through its securely validated parent;
- must verify that normal config discovery reports `uninitialized` afterward;
- must report any intentionally retained empty legacy directory truthfully.

Do not globally relax `identity()` or `validateScopedPath()` just to pass the reported case. Separate these concepts explicitly:

- trusted traversal component;
- Aegis-owned scope root;
- deletable artifact;
- external parent containing an Aegis-owned child;
- preserved external asset.

Use precise errors. Do not label an external parent as an “artifact” unless it is actually in the reset plan.

## Complete path audit

Search and inspect all code, tests, specifications, examples, scripts, release assets, and documentation for path derivation and retained Aegis data, including:

```text
.config/aegis
.local/state/aegis
.local/state/aegis-checkpoints
XDG_CONFIG_HOME
XDG_STATE_HOME
os.UserConfigDir
os.UserHomeDir
os.TempDir
MkdirTemp
CreateTemp
state_dir
checkpoint_dir
authority.db
authority.kek
authority-kek.json
certification
ollama-models
runtime
audit
receipts
mandates
sessions
charters
plans
approvals
provisioned
broker.sock
```

Classify each path as:

- canonical Aegis-owned local data;
- explicit deployment override;
- ephemeral Aegis-owned runtime data;
- preserved external dependency;
- test fixture only;
- obsolete or contradictory path.

Record the final inventory in focused architecture or path-layout documentation. Do not include secrets or inspect credential contents unnecessarily.

## Required tests

All tests must use isolated temporary homes. Never run migration or reset against `/home/javi`, the real `~/.argis`, the real legacy XDG paths, normal Hermes profiles, the operator's Ollama store, or real credentials.

Add tests for at least:

### Defaults and initialization

- literal default root is `$HOME/.argis`;
- config is `$HOME/.argis/aegis.yaml`;
- state and checkpoints are below `$HOME/.argis/state`;
- authority, certification, managed model, and runtime defaults are below canonical state;
- XDG variables do not scatter default local Aegis files;
- explicit supported overrides still work without changing unrelated defaults;
- fresh initialization creates correct ownership and modes;
- symlinked, wrong-owner, group-writable, other-writable, and malformed canonical roots fail closed;
- interruption leaves a resumable state;
- no Aegis-owned local artifact appears in legacy XDG or `/tmp` locations.

### Legacy detection and migration

- no layout;
- canonical only;
- legacy only;
- canonical plus legacy ambiguity;
- malformed legacy config;
- insecure legacy config;
- legacy child under a mode-0775 external XDG parent;
- unknown legacy files;
- symlink and hard-link attacks;
- same-filesystem migration;
- simulated cross-filesystem migration;
- decline, EOF, non-TTY, cancellation, and identity drift perform no destructive source mutation;
- Enter accepts the clearly displayed default migration action;
- explicit `y` and `yes` accept, while `n`, `no`, EOF, Ctrl+C, non-TTY input, and unrecognized input cancel without mutation;
- confirmation remains bound to the exact displayed plan digest, and drift is rejected;
- destination collision;
- interrupted migration recovery;
- migrated authority DB and KEK verify without exposing values;
- migrated certification remains bound to the exact model digest;
- external Hermes, Ollama, systemd, TLS, and credential assets remain byte-for-byte or identity unchanged as appropriate.

### Reset

- canonical complete reset and first-run replay;
- exact recognized legacy reset under a mode-0775 `~/.local/state` parent reproduces and fixes the reported failure;
- no chmod of the external parent is required or performed;
- unsafe external-parent race attempts cannot redirect deletion;
- retained empty legacy child behavior, if used, is verified and reported;
- unknown files, symlinks, hard links, malformed paths, repositories, roots, home itself, and arbitrary explicit external paths fail closed;
- default-deny `[y/N]` confirmation remains mandatory;
- plan and apply identity drift is detected;
- non-TTY, decline, EOF, and cancellation make no changes;
- post-reset default inspection reports `uninitialized` and next bare interactive invocation enters canonical `~/.argis` onboarding;
- normal Hermes profiles, operator Ollama artifacts, external credentials, systemd credentials, executable, and source tree are preserved.

Use Linux-specific tests for descriptor-anchored reset operations where required and provide explicit safe behavior on unsupported platforms. Do not claim race safety from pathname checks alone.

## Compatibility and release behavior

This is a default-layout change and must not be treated as a cosmetic patch.

- Decide and document the compatibility policy for existing release installations.
- Preserve explicit deployment configuration.
- Never silently initialize canonical state while recognized legacy state exists.
- Never silently move credential authority or audit history.
- Never silently select between canonical and legacy installations.
- Ensure `aegis config`, manager onboarding, model status/configuration/certification, secret commands, audit verification, reset, and bare startup all resolve the same effective layout.
- Update changelog and any versioned migration notes accurately.

## Documentation and launch-asset review

Update every affected source of truth, including:

- root `README.md`;
- `docs/QUICKSTART.md`;
- credential-authority setup;
- architecture and path-layout documentation;
- threat model;
- `SECURITY.md`;
- `CONTRIBUTING.md` test isolation rules;
- examples and no-key demo;
- reset documentation;
- onboarding documentation;
- changelog;
- terminal recording instructions;
- repository-local contributor issue material.

Remove stale claims that the default local layout uses XDG directories. Keep system deployment examples under `/etc/aegis` and `/var/lib/aegis` only where they truly describe a separate explicit daemon deployment.

Perform the complete launch-asset impact review required by `AGENTS.md`. Do not fabricate recordings, release artifacts, checksums, command output, issues, or migration results.

## Verification

Before completion:

1. Format all changed Go files.
2. Run focused config/layout tests.
3. Run initialization and onboarding tests.
4. Run migration tests, including PTY confirmation behavior.
5. Run reset tests, including the exact mode-0775 legacy-parent reproduction.
6. Run manager, credential-authority, certification, audit, and runtime tests affected by path changes.
7. Run relevant race tests.
8. Run `go test ./...`.
9. Run `go vet ./...`.
10. Build `./cmd/aegis`.
11. Run `git diff --check`.
12. Exercise a complete isolated flow:

```text
legacy default installation
    -> detect legacy
    -> migrate to literal ~/.argis
    -> verify ready or exact intermediate state
    -> reset
    -> verify uninitialized
    -> rerun bootstrap
    -> verify every newly created local Aegis artifact is below ~/.argis
```

13. Inspect the temporary home and assert no Aegis-owned files were created below `.config`, `.local`, `.cache`, or the system temp directory.
14. Inspect final Git status and diff for unintended changes.

Do not run destructive or migratory commands against the operator's real installation. Use only hermetic temporary homes and fake Hermes/Ollama/systemd fixtures.

## Scope and authority

Allowed:

- edit repository files under `/home/javi/code/aegis`;
- add narrowly justified dependencies if descriptor-safe filesystem operations require them;
- run builds, tests, linters, PTY tests, and isolated fake services;
- create and remove temporary verification fixtures;
- update repository-local documentation and diagrams.

Prohibited:

- modify, migrate, chmod, reset, or delete `/home/javi/.config/aegis`, `/home/javi/.local/state/aegis`, `/home/javi/.argis`, or their real parents;
- inspect or print real credential values;
- modify normal Hermes profiles or installation;
- modify operator-managed Ollama or downloaded models;
- alter systemd credentials or external services;
- commit, tag, push, publish, deploy, release, or create remote issues;
- discard existing uncommitted work;
- weaken reset safety to make one test pass.

Do not stop after an audit, plan, path helper, migration skeleton, or unit tests. Continue until the unified literal `~/.argis` defaults, safe legacy detection and migration, canonical and legacy reset behavior, documentation, and isolated end-to-end verification are implemented and exercised.

If a security property cannot be implemented safely within the current platform APIs, stop at that exact boundary and provide concrete evidence. Do not substitute an unsafe pathname implementation or claim success.

## Final report

Report:

- the exact root cause of the original reset failure;
- every prior path derivation found;
- the final canonical layout;
- legacy detection and migration behavior;
- canonical and legacy reset safety design;
- exact files changed;
- dependencies added and why;
- exact test, race, vet, build, and end-to-end commands with real results;
- launch assets changed and reviewed as unaffected;
- external assets proven preserved;
- remaining platform or security limitations;
- confirmation that no real operator state was modified;
- confirmation that no commit or external action was performed.
