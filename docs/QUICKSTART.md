# Five-Minute Quickstart

## Prerequisites

- Linux
- Go 1.26.5+
- Hermes Agent `>=0.18.0,<0.19.0` on `PATH`
- A compatible `pinentry` in the operator's desktop session for protected authority prompts, or a real terminal for the no-echo fallback

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

Alternatively, a genuinely new installation can run a bare executable in a terminal. An installed tagged release `aegis` uses the production defaults `~/.argis/aegis.yaml` and `~/.argis/state`. A repository-built development `./aegis` reports `dev` and uses ignored repository-local defaults `.aegis/aegis.yaml` and `.aegis/state`. It must remain in the real Aegis module/worktree root, and it rejects production paths. Review each displayed plan and press Enter to accept its `[Y/n]` default. After plan authorization, bare onboarding asks for and confirms an authority passphrase in two fresh pinentry windows. It prefers an explicit absolute `--pinentry-executable`, otherwise conventional `pinentry`, and uses terminal-backed no-echo input only if pinentry is unavailable before interaction. It generates a random KEK, persists only its Argon2id plus XChaCha20-Poly1305 encrypted envelope, creates and verifies the authority database, and continues to runtime/model/certification checks. It never sends the passphrase or KEK to Hermes, Ollama, or a model. Pinentry cancellation does not fall back; headless services should use systemd credential custody.

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
# if credential authority material is present, authenticate with its existing 12+ byte passphrase
# answer yes at the default-deny prompt
aegis
```

If exact legacy defaults are detected, bare startup reports `legacy-layout-detected` rather than creating `~/.argis`. In a real terminal, run `aegis migrate-layout`, review its digest-bound copy/preservation plan, and press Enter at `Apply this digest-bound migration plan? [Y/n]`; or run `aegis reset` to remove only the recognized legacy installation. Canonical plus legacy artifacts are ambiguous and are never merged or selected automatically. Linux migration copies, fsyncs, verifies, publishes, then cleans the source; reset can retain an empty legacy child beneath an unsafe external XDG parent without chmodding that parent. See [PATH_LAYOUT.md](PATH_LAYOUT.md).

Reset is intentionally fixed to the selected profile rather than accepting a custom `--config`. Repository paths remain denied except for the exact ignored `.aegis` subtree authorized by a development binary; reset also rejects the home directory itself, filesystem roots, paths outside the authenticated operator home, unsafe parents, symlinks, hard-linked files, unknown state content, and any path/inode change after preview. It never accepts a force flag; non-TTY, Enter, explicit `no`, EOF, cancellation, or input other than `y`/`yes` performs no writes.

The reset scope is the resolved configuration, recognized Aegis state-store objects (charters, plans, approvals, receipts, mandates, sessions, provisioning artifacts, audit/checkpoints), manager certifications and disposable homes, and recognized interrupted initialization/configuration/authority temporaries. A credential database and passphrase-encrypted or development host-file KEK are deleted only when configured below the Aegis state root and independently recognizable as Aegis artifacts. External authority/KEK paths and systemd credential custody are reported and preserved. Aegis also preserves its executable and source checkout, Hermes and normal Hermes profiles, Ollama and an operator-managed daemon, external credentials/systems, external Ollama model stores, and downloaded model data. This includes the Aegis managed model store if present.

Reset irreversibly destroys encrypted credentials and audit history in scope without separate backups. It is intended for development/testing unless the operator deliberately accepts that loss. On success it prints `state: uninitialized` and `next_command: aegis`; `config.Inspect` is absent, so the next bare interactive invocation re-enters onboarding. Bare non-TTY invocation instead remains fail-closed with `manager_not_initialized`.

Run `./aegis --config .aegis.yaml` in a real terminal to resume deterministic bootstrap or, once every derived readiness check passes, enter the built-in manager shell. Ordinary canonical message responses stream as sanitized `Hermes model / untrusted` text; proposals remain buffered until complete validation. Rich terminals update one progress line in place, and accessible/plain terminals print at most one progress line per turn. The registry has no default model. When discovery finds exactly one approved installed candidate, bootstrap visibly selects it without presenting a meaningless one-item menu; multiple installed candidates still require an explicit selection with no default. `exit` or `use an already-present candidate` performs no network mutation. Choosing `pull` requires a closed-registry candidate and a complete network/disk/route preview; `[Y/n]` confirms the selected action while artifact and configuration digests—not copied prose—bind what Aegis applies. Progress remains visible, interruption is resumable, and Aegis rediscovery binds the resulting exact digest before separate certification. Until an exact local artifact has passed the conformance suite, the wizard reports the failed prerequisite and exact next action; no cloud fallback is attempted. In manager mode the exact Core 15 base names are `/help /status /context /authority /limits /scan /watch /findings /investigate /timeline /report /audit /cancel /clear /exit`; `/quit` aliases `/exit`, and compatibility `/complete` delegates to the same registry. `/watch` and host-expanding scan profiles report unavailable because this checkout installs no source manager or endpoint adapter. See [BASE_SLASH_COMMANDS.md](BASE_SLASH_COMMANDS.md). Enter submits, `Ctrl+J` inserts a universal newline, Up/Down recall history, `Ctrl+R` searches recent history, `?` on empty input shows help, and bracketed paste remains one bounded guarded submission. Use `AEGIS_ACCESSIBLE=1 ./aegis --config .aegis.yaml` for the no-animation, line-oriented accessible/plain renderer; `TERM=dumb` also selects it. Ctrl-C, SIGTERM, EOF, or session expiry enter the same bounded cleanup path and restore terminal raw/echo/canonical state.

Do not edit manager model fields merely to suppress that denial. For an already-installed official Ollama candidate, use the deterministic no-download path:

```sh
./aegis --config .aegis.yaml manager model candidates
./aegis --config .aegis.yaml manager model route --mode external-local --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model discover --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model configure CANDIDATE_ID --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager certify CANDIDATE_ID
./aegis --config .aegis.yaml manager model status
```

Discovery uses local Ollama metadata only. The generated configuration uses a 15-minute principal authority lifetime and five-minute manager turn/Ollama request deadlines so the complete CPU-bound certification corpus can finish. Configure previews the exact digest-bound route and requires literal `yes`; decline and interruption perform no write. Certification loads and tests the artifact through disposable Hermes and the authenticated loopback proxy, including an ordinary manager-specific conversational response that cannot pass as a generic acknowledgement. A schema-valid reply whose sole failure is omitted required conversational content enters a bounded three-execution loop using direct, case-specific wording rather than repeating an ambiguous request until authority expires; equivalent truthful descriptions of encrypted Aegis custody and the out-of-model boundary pass without one magic phrase. Other failures abort on the first result by default. Add `--continue-on-error` to run the remaining corpus for diagnostics; cancellation and authority expiry still stop immediately, every case must still pass, and no failed or partial certification is saved. Every execution is bounded by `manager.hermes.turn_timeout`, while principal authority expiry bounds the complete run. A timeout, cancellation, expiry, protocol/transport failure, invalid envelope, other failed case, or exhausted conversational check reports the exact case and stable metadata-safe reason and performs cleanup; instruction or corpus changes invalidate an earlier certification. Model installation/downloading remains an operator action outside this quickstart.

In the rich manager composer, terminal bracketed paste keeps multiline clipboard text in one guarded submission. Press Enter once after the paste summary; Aegis scans the complete normalized multiline input before sending any allowed text to Hermes.

To create a credential, type `new cred`; Aegis locally asks for the credential name, visibly normalizes a human name such as `Berryhill GHCR token` to `berryhill-ghcr-token`, and immediately enters protected no-echo value intake. It defaults disclosure to protected and does not ask the model to choose a kind or protection level. Complete forms such as `store a cred named demo with a value of synthetic-example-1234`, `make a cred named demo with a secret of synthetic-example-1234`, or paired shorthand `key: "demo" secret: "synthetic-example-1234"` also execute directly. That low-ambiguity imperative authorizes the exact parsed insert without another confirmation dialogue or model round trip, so it cannot poison or be vetoed by the Hermes conversation. On close, Aegis invalidates the route, removes Hermes state, verifies the external Ollama runner and avoids a redundant unload request when it is already absent, or terminates the dedicated managed daemon; it clears retained presentation/history and reports a stable failed teardown stage rather than claiming completion if cleanup fails.

If a natural create request omits the credential name, Aegis prompts locally for a name and shows the normalized exact reference before starting protected value intake. It never silently stores a generic placeholder reference.

At `Secret value:`, bracketed paste accepts a complete multiline credential document as one no-echo value. Paste the block, press Enter, then paste and submit the same block at confirmation. A mismatch flushes any queued protected lines before the composer returns. Do not paste credentials at the ordinary composer: Aegis blocks detected credential-bearing submissions locally before Hermes and asks you to restart protected intake.

Authenticated read-only questions execute immediately without model negotiation. `how many secrets do we have?` returns exact total, active, and record-level revoked counts from the authority; `list my credentials` returns metadata only. Neither request asks for confirmation or reaches the model.

An explicit request such as `what is the value for credential: "demo"` or `I need to see the demo cred value` decrypts and renders that exact reference directly in the authenticated terminal session without reaching the model. The value is terminal-escaped, audit remains metadata-only, retained TUI state is purged on close, and terminal scrollback is outside Aegis cleanup. Missing, ambiguous, or revoked references fail closed.

## Provider boundary

```sh
./aegis --config .aegis.yaml design --smoke
```

Without `credentials.design_provider` and its source credential, this must not be presented as a successful model turn. The command may reach Hermes and report its authentic provider-configuration failure. It uses disposable state and does not modify the normal Hermes profile.

Clean up this repository-local example with `rm -f aegis .aegis.yaml .office-charter.json && rm -rf .aegis`. `aegis reset` deliberately rejects repository paths and is not a replacement for that explicit example cleanup.
