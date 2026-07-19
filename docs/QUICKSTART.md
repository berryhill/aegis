# Five-Minute Quickstart

## Prerequisites

- Linux
- Go 1.26.5+
- Hermes Agent `>=0.18.0,<0.19.0` on `PATH`

Install the latest tagged Aegis source with `go install github.com/berryhill/aegis/cmd/aegis@latest`, or continue below to build a checkout. Self-update requires a published, non-draft stable GitHub release with assets, not merely a local or remote Git tag; until publication completes it correctly reports the previous published stable version. `aegis --update` is the strict root-only alias for `aegis update`; both use the same checksum-verifying service.

## Build and configure

```sh
go build -o aegis ./cmd/aegis
cp examples/aegis.yaml .aegis.yaml
chmod 600 .aegis.yaml
uid=$(id -u)
user=$(id -un)
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/; s/REPLACE_WITH_LOCAL_USERNAME/$user/" .aegis.yaml
cp examples/office-charter.json .office-charter.json
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/g; s/REPLACE_WITH_LOCAL_USERNAME/$user/g" .office-charter.json
```

The copied files are local working files and should not be committed.

## Verify the no-key path

```sh
./aegis --config .aegis.yaml runtime
./aegis --config .aegis.yaml charter validate .office-charter.json
./aegis --config .aegis.yaml config
```

Success means Hermes is named and versioned explicitly, charter validation returns a canonical digest, and the API token is shown as `[REDACTED]`.

Alternatively, a genuinely new installation can run bare `./aegis` in a terminal. The literal local defaults are `~/.argis/aegis.yaml` and `~/.argis/state`; XDG variables do not change them. Review each displayed plan and press Enter to accept its `[Y/n]` default. Bare onboarding asks for and confirms an authority passphrase with echo disabled, generates a random KEK, persists only its Argon2id plus XChaCha20-Poly1305 encrypted envelope, creates and verifies the authority database, and continues to runtime/model/certification checks. It never sends the passphrase or KEK to Hermes, Ollama, or a model.

Verify the initialized non-interactive manager boundary without starting Hermes or Ollama:

```sh
printf 'not chat' | ./aegis --config .aegis.yaml
# exits with manager_requires_tty and names deterministic subcommands
```

Without configuration, the same non-TTY invocation instead emits structured `manager_not_initialized` output naming `aegis init` and exits 2 without prompting.

## Reset and replay onboarding

For development/testing, a principal can return a default local installation to first-run state:

```sh
aegis reset
# review every delete/preserve path and the irreversible credential/audit warning
# answer yes at the default-deny prompt
aegis
```

If exact legacy defaults are detected, bare startup reports `legacy-layout-detected` rather than creating `~/.argis`. In a real terminal, run `aegis migrate-layout`, review its digest-bound copy/preservation plan, and press Enter at `Apply this digest-bound migration plan? [Y/n]`; or run `aegis reset` to remove only the recognized legacy installation. Canonical plus legacy artifacts are ambiguous and are never merged or selected automatically. Linux migration copies, fsyncs, verifies, publishes, then cleans the source; reset can retain an empty legacy child beneath an unsafe external XDG parent without chmodding that parent. See [PATH_LAYOUT.md](PATH_LAYOUT.md).

Use `aegis --config "$HOME/path/to/aegis.yaml" reset` for a safely scoped custom configuration; reset intentionally rejects repository paths, the home directory itself, filesystem roots, paths outside the authenticated operator home, unsafe parents, symlinks, hard-linked files, unknown state content, and any path/inode change after preview. It never accepts a force flag; non-TTY, Enter, explicit `no`, EOF, cancellation, or input other than `y`/`yes` performs no writes.

The reset scope is the resolved configuration, recognized Aegis state-store objects (charters, plans, approvals, receipts, mandates, sessions, provisioning artifacts, audit/checkpoints), manager certifications and disposable homes, and recognized interrupted initialization/configuration/authority temporaries. A credential database and passphrase-encrypted or development host-file KEK are deleted only when configured below the Aegis state root and independently recognizable as Aegis artifacts. External authority/KEK paths and systemd credential custody are reported and preserved. Aegis also preserves its executable and source checkout, Hermes and normal Hermes profiles, Ollama and an operator-managed daemon, external credentials/systems, external Ollama model stores, and downloaded model data. This includes the Aegis managed model store if present.

Reset irreversibly destroys encrypted credentials and audit history in scope without separate backups. It is intended for development/testing unless the operator deliberately accepts that loss. On success it prints `state: uninitialized` and `next_command: aegis`; `config.Inspect` is absent, so the next bare interactive invocation re-enters onboarding. Bare non-TTY invocation instead remains fail-closed with `manager_not_initialized`.

Run `./aegis --config .aegis.yaml` in a real terminal to resume deterministic bootstrap or, once every derived readiness check passes, enter the built-in manager shell. The registry has no default model. When discovery finds exactly one approved installed candidate, bootstrap visibly selects it without presenting a meaningless one-item menu; multiple installed candidates still require an explicit selection with no default. `exit` or `use an already-present candidate` performs no network mutation. Choosing `pull` requires a closed-registry candidate and a complete network/disk/route preview; `[Y/n]` confirms the selected action while artifact and configuration digests—not copied prose—bind what Aegis applies. Progress remains visible, interruption is resumable, and Aegis rediscovery binds the resulting exact digest before separate certification. Until an exact local artifact has passed the conformance suite, the wizard reports the failed prerequisite and exact next action; no cloud fallback is attempted. In manager mode `/status`, `/audit verify`, `/help`, `/complete`, `/clear`, `/quit`, and plain `quit`/`exit` remain local. Ctrl-C, SIGTERM, EOF, or session expiry enter the same bounded cleanup path.

Do not edit manager model fields merely to suppress that denial. For an already-installed official Ollama candidate, use the deterministic no-download path:

```sh
./aegis --config .aegis.yaml manager model candidates
./aegis --config .aegis.yaml manager model route --mode external-local --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model discover --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model configure CANDIDATE_ID --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager certify CANDIDATE_ID
./aegis --config .aegis.yaml manager model status
```

Discovery uses local Ollama metadata only. Configure previews the exact digest-bound route and requires literal `yes`; decline and interruption perform no write. Certification loads and tests the artifact through disposable Hermes and the authenticated loopback proxy. Every case is bounded by `manager.hermes.turn_timeout`, while principal authority expiry bounds the complete run. A timeout, cancellation, expiry, protocol/transport failure, invalid envelope, or failed case aborts immediately, reports the exact case and stable metadata-safe reason, performs cleanup, and writes no certification; retry the printed `aegis manager certify CANDIDATE_ID` command after correcting the reason. Model installation/downloading remains an operator action outside this quickstart.

## Provider boundary

```sh
./aegis --config .aegis.yaml design --smoke
```

Without `credentials.design_provider` and its source credential, this must not be presented as a successful model turn. The command may reach Hermes and report its authentic provider-configuration failure. It uses disposable state and does not modify the normal Hermes profile.

Clean up this repository-local example with `rm -f aegis .aegis.yaml .office-charter.json && rm -rf .aegis`. `aegis reset` deliberately rejects repository paths and is not a replacement for that explicit example cleanup.
