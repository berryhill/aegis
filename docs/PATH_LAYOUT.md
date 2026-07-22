# Local Path Layout

## Per-operator production and development defaults

Stable release binaries use the literal production root `~/.argis`. Source-built binaries whose version is `dev` use `.aegis` in the Aegis repository root containing the executable. Aegis verifies the development executable directory using a real matching `go.mod`, a non-symlink `.git` worktree marker, and containment below the authenticated operator home; a copied development binary fails closed. A pre-rename `.aegis-dev` tree is detected and denied rather than silently merged, copied, or deleted. It resolves `~` for production to the authenticated effective operator's clean, absolute, owned home and never passes a tilde to filesystem APIs. `XDG_CONFIG_HOME` and `XDG_STATE_HOME` do not alter either profile.

```text
~/.argis/                                      0700
~/.argis/aegis.yaml                           0600
~/.argis/state/                               0700
~/.argis/state/audit-checkpoints/
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

<repository>/.aegis/                           0700, Git-ignored
<repository>/.aegis/aegis.yaml                0600
<repository>/.aegis/state/                    0700
# same state children as production, but a distinct authority,
# deployment identifier, audit chain, certification, and runtime
```

Directories are created only when implemented behavior needs them. Atomic configuration and manager-configuration files are created beside their destination. Disposable Hermes and managed Ollama homes are created below `state/runtime`. Store atomic files are created beside their state destination. Sensitive regular files are mode `0600`; Aegis-owned directories are mode `0700`.

The typed resolver in `internal/layout` is the source for both profile roots and their config, state, checkpoints, authority database, host KEK, certification, managed-model, and runtime defaults. Configuration loading remains separate. An executable refuses configuration or state beneath the opposing profile root, including values loaded indirectly from configuration. Explicit deployments outside both local profile roots retain their existing validation, but destructive reset is restricted to the executable's own exact profile layout.

## Path classification

| Path or source | Classification |
|---|---|
| `~/.argis/aegis.yaml` | canonical Aegis-owned local configuration |
| `~/.argis/state` and implemented children above | canonical Aegis-owned local state |
| `<repository>/.aegis/aegis.yaml` and `<repository>/.aegis/state` | isolated, Git-ignored development configuration and state for source-built `dev` binaries residing in the verified worktree root |
| adjacent `.aegis-*` temporaries | ephemeral Aegis-owned transaction data at the canonical destination |
| `state/runtime/design-*`, `stanza-*`, `manager-*`, `ollama-*` | ephemeral Aegis-owned runtime data |
| `state/manager/ollama-models` | Aegis-managed model data; preserved by reset |
| explicit config/state/checkpoint/authority/certification/socket paths | explicit deployment override; never inferred from unrelated defaults for deletion or migration |
| `/etc/aegis`, `/var/lib/aegis`, `/run/aegis` | explicit system deployment examples, not bare local defaults |
| normal Hermes installation/profiles, operator Ollama daemon/store, systemd credentials, external TLS/credentials, executable and checkout | preserved external dependencies |
| repository `.aegis` paths in `examples/`, demo scripts, and tests | explicit isolated fixture data, not local defaults |
| `os.MkdirTemp` below configured `state/runtime` | canonical or explicitly overridden ephemeral runtime data |
| `os.CreateTemp` beside config/state/update destinations | adjacent atomic transaction data; updater data belongs to the executable installation, not local state |
| `~/.config/aegis/aegis.yaml`, `~/.local/state/aegis`, `~/.local/state/aegis-checkpoints` | recognized legacy defaults only |
| `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `os.UserConfigDir` | obsolete as default path derivation; XDG variables are isolated in tests and ignored by canonical resolution |
| `/tmp/aegis-*`, `~/.cache/aegis` | forbidden for default Aegis-owned local data |

## Discovery and compatibility

For a release binary with no explicit `--config`, production discovery is artifact-derived and read-only. A development binary supplies its fixed development configuration path and never performs production/legacy discovery:

- no canonical installation or meaningful legacy artifacts: `uninitialized`, and bootstrap uses only `~/.argis`; an empty canonical root/state or a state tree containing only the deliberately preserved managed-model store is not an installation;
- canonical only: validate and use canonical state;
- legacy only: `legacy-layout-detected`; do not initialize a second installation;
- canonical plus legacy: fail closed as `canonical_and_legacy_layout_ambiguous`;
- empty retained legacy state/checkpoint children after a safe reset or migration do not count as installations;
- explicit `--config`: inspect only that deployment and do not infer migration/deletion authority from local defaults;
- environment values are configuration precedence, not deletion authority.

`aegis migrate-layout` is Linux-only because automatic cleanup requires descriptor-anchored no-follow operations. It authenticates the OS principal, accepts only exact legacy state/checkpoint defaults and a secure valid config, rejects unknown/symlinked/hard-linked artifacts and destination collisions, prints a digest-bound source/destination inventory, requires a real terminal and exact `migrate aegis to ~/.argis`, copies through a mode-`0700` staging root, fsyncs and verifies the destination, proves any configured bbolt authority opens with the same deployment identity and custody, publishes without overwrite, and only then cleans sources. Copy is used on both same- and cross-filesystem source layouts. Only structured Aegis-owned path fields are rewritten. On unsupported platforms, missing systemd custody, or authority-linkage failure it fails before mutation.

Migration does not copy or render credential values. Credential database/KEK and certification bytes move with state and retain their exact cryptographic/model bindings; systemd credentials and external assets remain outside the plan. A staging collision or post-publication cleanup failure reports an exact path and leaves data for inspection rather than weakening validation.

## Reset authority

Production reset authorizes only validated known artifacts below `~/.argis`; development reset authorizes only validated known artifacts below the exact verified `<repository>/.aegis`. Neither executable accepts an arbitrary explicit deployment as reset authority. The repository exception applies only to that ignored subtree; source files and every sibling repository path remain prohibited. Reset preserves the managed model store and all external dependencies and removes configuration last. If no preserved model data remains it also removes the empty canonical root; otherwise default discovery ignores the model-only retained tree so reset still returns `uninitialized`. Legacy reset accepts only exact recognized defaults. It does not require or perform `chmod` on external XDG parents. A mode-`0775` `~/.local/state` is traversal context, not an Aegis artifact or deletion root: every removal is opened relative to the validated Aegis child descriptor with no-follow and device/inode checks. Where deleting the child entry through that external parent is unsafe, reset truthfully retains the empty child. Empty retained roots are ignored by default discovery, so successful reset is `uninitialized`.

All reset paths retain bounded inventory, exact plan digest, real-TTY default-deny `[y/N]` confirmation, identity revalidation, unknown-artifact denial, hard-link/symlink denial, repository/root/home denial, and postcondition verification. Development reset intentionally skips authority-passphrase authentication. If a production reset would delete credential records or local encrypted KEK material, it authenticates the existing minimum-12-byte passphrase-file authority before confirmation and independently again after `yes`; missing, incorrect, malformed, or different custody denies before mutation. Pathname checks alone are not claimed as race safety.

For the verified development profile only, directories between the operator home and the verified repository-local `.aegis` root are traversal context and may be group-writable; they must remain real directories without symlink components. The `.aegis` root and every reset artifact beneath it retain strict ownership, mode, link, inventory, and revalidation requirements. Production grants no corresponding exception.
